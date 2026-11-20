package kilo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
)

// restoreTimeout bounds the `kilo export` probe and `kilo import` that put a
// restored session back into Kilo's store.
const restoreTimeout = 2 * time.Minute

// importRestoredSession puts the session `entire resume` just restored back
// into Kilo's own store, so the `kilo run --session <id>` it prints finds it.
// Kilo never reads the transcript under .entire/tmp/kilo; it looks sessions up
// in its global database, which is empty for this session after `kilo session
// delete`, on a fresh clone, or on another machine.
//
// It leaves Kilo alone when it already holds the session: `kilo import` never
// replaces existing messages, but it would merge the checkpoint's messages
// into a session that has since moved on, and move the session to this
// directory. Transcripts with no envelope (written before this change) and
// machines without kilo on PATH are skipped too, which is the old behavior:
// the transcript file is written and nothing else happens.
func (a *Agent) importRestoredSession(session protocol.AgentSessionJSON) error {
	export, ok, err := exportFromTranscript(session.NativeData)
	if err != nil || !ok {
		return err
	}
	var envelope struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(export.Info, &envelope); err != nil {
		return fmt.Errorf("read kilo session envelope: %w", err)
	}
	if envelope.ID == "" {
		return errors.New("kilo session envelope has no id")
	}
	// The id goes to kilo as an argument; one starting with "-" would read as
	// an option. Entire's own session ids never start with one.
	if strings.HasPrefix(envelope.ID, "-") {
		return fmt.Errorf("kilo session envelope has an invalid id %q", envelope.ID)
	}
	if session.SessionID != "" && envelope.ID != session.SessionID {
		return fmt.Errorf("kilo transcript belongs to session %s, not %s", envelope.ID, session.SessionID)
	}

	dir := session.RepoPath
	if dir == "" {
		dir = protocol.RepoRoot()
	}
	runner := a.CommandRunner
	if runner == nil {
		runner = &DefaultCommandRunner{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), restoreTimeout)
	defer cancel()

	present, err := runner.HasSession(ctx, dir, envelope.ID)
	if errors.Is(err, exec.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check kilo for session %s: %w", envelope.ID, err)
	}
	if present {
		return nil
	}

	blob, err := marshalNoHTMLEscape(export)
	if err != nil {
		return err
	}
	exportPath, cleanup, err := writeImportFile(filepath.Dir(session.SessionRef), blob)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := runner.ImportSession(ctx, dir, exportPath); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("import kilo session %s: %w", envelope.ID, err)
	}
	return nil
}

// writeImportFile writes the export `kilo import` reads next to the restored
// transcript, readable only by the user, and returns its absolute path and a
// func that removes it. The path is absolute because Kilo runs in the
// repository, which need not be this process's working directory.
func writeImportFile(dir string, blob []byte) (string, func(), error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, fmt.Errorf("resolve kilo import directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".kilo-import-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create kilo import file: %w", err)
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return "", nil, errors.Join(err, tmp.Close())
	}
	if _, err := tmp.Write(blob); err != nil {
		cleanup()
		return "", nil, errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return name, cleanup, nil
}

// kiloSessionNotFound is how `kilo export` reports an id its store does not
// hold, on stderr with a non-zero exit ("Error: Session not found: <id>").
const kiloSessionNotFound = "Session not found"

// HasSession reports whether Kilo's store holds sessionID, by asking `kilo
// export` for it. Kilo's "Session not found" reads as absent; so does output
// that is not that session's export, and an export with no messages: a `kilo
// import` that fails partway leaves the session row behind without them, and a
// retry must still import. Any other failed export is an error, since it
// cannot tell whether Kilo holds the session; so are kilo failing to start
// (exec.ErrNotFound when it is not on PATH) and the timeout.
func (r *DefaultCommandRunner) HasSession(ctx context.Context, dir, sessionID string) (bool, error) {
	cmd := exec.CommandContext(ctx, "kilo", "export", sessionID)
	cmd.Dir = dir
	cmd.Env = pureKiloEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if cmd.ProcessState == nil {
		return false, fmt.Errorf("kilo export: %w", runErr)
	}
	if !cmd.ProcessState.Success() {
		if ctx.Err() != nil {
			return false, fmt.Errorf("kilo export: %w", ctx.Err())
		}
		if bytes.Contains(ansiEscape.ReplaceAll(stderr.Bytes(), nil), []byte(kiloSessionNotFound)) {
			return false, nil
		}
		return false, kiloFailure("kilo export", runErr, stderr.Bytes())
	}
	var export struct {
		Info struct {
			ID string `json:"id"`
		} `json:"info"`
		Messages []json.RawMessage `json:"messages"`
	}
	parsed := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &export) == nil
	return parsed && export.Info.ID == sessionID && len(export.Messages) > 0, nil
}

// ImportSession runs `kilo import <exportPath>` from dir. Kilo files the
// session under the project and directory it runs in, so dir is the
// repository the session is restored into.
func (r *DefaultCommandRunner) ImportSession(ctx context.Context, dir, exportPath string) error {
	cmd := exec.CommandContext(ctx, "kilo", "import", exportPath)
	cmd.Dir = dir
	cmd.Env = pureKiloEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return kiloFailure("kilo import", err, stderr.Bytes())
	}
	return nil
}

// kiloFailure is the error for a failed kilo command, with Kilo's own message
// from stderr when it printed one.
func kiloFailure(command string, err error, stderr []byte) error {
	if detail := kiloErrorDetail(stderr); detail != "" {
		return fmt.Errorf("%s: %w: %s", command, err, detail)
	}
	return fmt.Errorf("%s: %w", command, err)
}

// pureKiloEnv is kiloEnv with KILO_PURE=1, which stops Kilo loading plugins.
// The export probe and the import then never start the Entire plugin, so they
// cannot fire Entire hooks for a session that is only being restored.
func pureKiloEnv() []string {
	return append(kiloEnv(), "KILO_PURE=1")
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// maxKiloErrorDetail caps how much of Kilo's stderr an error carries.
const maxKiloErrorDetail = 512

// kiloErrorDetail turns Kilo's stderr into one line for an error message:
// colors stripped, its INFO log lines dropped, capped in length.
func kiloErrorDetail(stderr []byte) string {
	var lines []string
	for line := range strings.SplitSeq(ansiEscape.ReplaceAllString(string(stderr), ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "INFO ") {
			continue
		}
		lines = append(lines, line)
	}
	detail := strings.Join(lines, " ")
	if len(detail) > maxKiloErrorDetail {
		detail = detail[:maxKiloErrorDetail] + "..."
	}
	return detail
}
