package vibe

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	appServerBinary  = "vibe-app-server"
	appServerBinEnv  = "VIBE_APP_SERVER_BIN"
	historyPageLimit = 500
	// maxHistoryPages bounds a fetch against a server that never ends paging.
	maxHistoryPages = 400
	maxRPCLine      = 64 * 1024 * 1024
)

// Snapshot is a session as Vibe's app server reports it.
type Snapshot struct {
	// Entries are the raw history entries, oldest first.
	Entries []json.RawMessage
	Usage   *TokenUsage
	Model   string
}

// SessionFetcher reads a stored or live session from Vibe.
type SessionFetcher interface {
	FetchSession(ctx context.Context, sessionID, workDir string) (*Snapshot, error)
}

// AppServerFetcher talks to `vibe-app-server` over stdio: newline-delimited
// JSON-RPC 2.0. Reading a session needs no API key and does not resume it, so
// it is safe while Vibe still has the session open.
//
// The session store itself is a private recovery format, so this is the only
// supported way to get the conversation.
type AppServerFetcher struct{}

// appServerPath prefers the binary Vibe names for its hooks, so the reader
// matches the running install, and falls back to PATH for calls made outside a
// hook (for example from a git hook). The path only ever comes from the
// process environment or PATH, never from a file in the repository.
func appServerPath() (string, error) {
	if fromEnv := strings.TrimSpace(os.Getenv(appServerBinEnv)); fromEnv != "" {
		// Vibe always gives an absolute path. A relative one would be resolved
		// against the repository this process runs in, so it is refused.
		if !filepath.IsAbs(fromEnv) {
			return "", fmt.Errorf("%s must be an absolute path, got %q", appServerBinEnv, fromEnv)
		}
		if info, err := os.Stat(fromEnv); err == nil && !info.IsDir() {
			return fromEnv, nil
		}
	}
	path, err := exec.LookPath(appServerBinary)
	if err != nil {
		return "", fmt.Errorf("%s not found: %w", appServerBinary, err)
	}
	return path, nil
}

func (f *AppServerFetcher) FetchSession(ctx context.Context, sessionID, workDir string) (*Snapshot, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("session id is required")
	}
	bin, err := appServerPath()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = workDir
	// If a descendant keeps the pipes open after the server is killed, stop
	// waiting for them rather than hanging.
	cmd.WaitDelay = 2 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", appServerBinary, err)
	}
	// Closing stdin ends the server; Wait reaps it. On timeout the context
	// kills the process, which also unblocks any pending read.
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	client := &rpcClient{w: stdin, r: bufio.NewReaderSize(stdout, 64*1024)}
	snapshot, err := readSession(client, sessionID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%s: %w", appServerBinary, ctxErr)
		}
		return nil, err
	}
	return snapshot, nil
}

// readSession runs the handshake, then reads the session header and pages
// through its history.
func readSession(client *rpcClient, sessionID string) (*Snapshot, error) {
	if _, err := client.call("initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "entire-agent-" + agentName, "version": "1"},
		"capabilities": map[string]any{},
	}); err != nil {
		return nil, err
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		return nil, err
	}

	// history and turns are null: the history comes from the paged call below.
	raw, err := client.call("session/read", map[string]any{
		"sessionId": sessionID,
		"history":   nil,
		"turns":     nil,
	})
	if err != nil {
		return nil, err
	}
	var read struct {
		State struct {
			Session struct {
				Model      string      `json:"model"`
				TokenUsage *TokenUsage `json:"tokenUsage"`
			} `json:"session"`
		} `json:"state"`
	}
	if err := json.Unmarshal(raw, &read); err != nil {
		return nil, fmt.Errorf("parse session/read: %w", err)
	}
	snapshot := &Snapshot{Usage: read.State.Session.TokenUsage, Model: read.State.Session.Model}

	// History is paged from the newest entry backward. With no cursor Vibe
	// returns the newest page whatever direction is asked for, and only a
	// backward request then says how to reach the older ones; asking "forward"
	// first returns the last page and no cursor, which silently drops the
	// start of a long session.
	var pages [][]json.RawMessage
	cursor := ""
	for range maxHistoryPages {
		pageReq := map[string]any{"direction": "backward", "limit": historyPageLimit}
		if cursor != "" {
			pageReq["cursor"] = cursor
		}
		raw, err := client.call("session/history/list", map[string]any{"sessionId": sessionID, "page": pageReq})
		if err != nil {
			return nil, err
		}
		var list struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor string            `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("parse session/history/list: %w", err)
		}
		pages = append(pages, list.Items)
		if list.NextCursor == "" || list.NextCursor == cursor || len(list.Items) == 0 {
			// Each page is oldest-first; the pages themselves arrived newest-first.
			slices.Reverse(pages)
			snapshot.Entries = slices.Concat(pages...)
			return snapshot, nil
		}
		cursor = list.NextCursor
	}
	return nil, errors.New("session history did not end within the page limit")
}

type rpcClient struct {
	w      io.Writer
	r      *bufio.Reader
	nextID int
}

type rpcResponse struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *rpcClient) send(message map[string]any) error {
	message["jsonrpc"] = "2.0"
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = c.w.Write(append(data, '\n'))
	return err
}

func (c *rpcClient) notify(method string, params any) error {
	return c.send(map[string]any{"method": method, "params": params})
}

// call sends a request and returns its result, skipping the notifications and
// server requests that can arrive in between.
func (c *rpcClient) call(method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	for {
		line, err := readLine(c.r)
		if err != nil {
			return nil, fmt.Errorf("%s: read response: %w", method, err)
		}
		var resp rpcResponse
		if json.Unmarshal(line, &resp) != nil || resp.ID == nil || *resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// readLine reads one newline-terminated message, refusing oversized ones.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, chunk...)
		if len(line) > maxRPCLine {
			return nil, errors.New("response line too large")
		}
		if !isPrefix {
			return line, nil
		}
	}
}
