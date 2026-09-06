package protocol

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
)

type sessionDirResolver interface{ GetSessionDir(string) (string, error) }
type sessionFileResolver interface{ ResolveSessionFile(string, string) string }
type sessionIDProvider interface{ GetSessionID(*HookInputJSON) string }
type sessionReader interface{ ReadSession(*HookInputJSON) (AgentSessionJSON, error) }
type sessionWriter interface{ WriteSession(AgentSessionJSON) error }
type transcriptReader interface{ ReadTranscript(string) ([]byte, error) }
type transcriptChunker interface { ChunkTranscript([]byte, int) ([][]byte, error); ReassembleTranscript([][]byte) ([]byte, error) }
type resumeFormatter interface{ FormatResumeCommand(string) string }

func WriteJSON(w io.Writer, v any) error { enc := json.NewEncoder(w); enc.SetEscapeHTML(false); return enc.Encode(v) }
func ReadJSON[T any](r io.Reader) (*T, error) { var value T; if err := json.NewDecoder(r).Decode(&value); err != nil { return nil, err }; return &value, nil }
func RepoRoot() string { if root := os.Getenv("ENTIRE_REPO_ROOT"); root != "" { return root }; root, _ := os.Getwd(); return root }

func HandleGetSessionID(in io.Reader, out io.Writer, a sessionIDProvider) error { value, err := ReadJSON[HookInputJSON](in); if err != nil { return err }; return WriteJSON(out, SessionIDResponse{SessionID: a.GetSessionID(value)}) }
func HandleGetSessionDir(args []string, out io.Writer, a sessionDirResolver) error { fs := newFlagSet("get-session-dir"); repo := fs.String("repo-path", "", "repo path"); if err := fs.Parse(args); err != nil { return err }; value, err := a.GetSessionDir(*repo); if err != nil { return err }; return WriteJSON(out, SessionDirResponse{SessionDir: value}) }
func HandleResolveSessionFile(args []string, out io.Writer, a sessionFileResolver) error { fs := newFlagSet("resolve-session-file"); dir := fs.String("session-dir", "", "session dir"); id := fs.String("session-id", "", "session id"); if err := fs.Parse(args); err != nil { return err }; return WriteJSON(out, SessionFileResponse{SessionFile: a.ResolveSessionFile(*dir, *id)}) }
func HandleReadSession(in io.Reader, out io.Writer, a sessionReader) error { value, err := ReadJSON[HookInputJSON](in); if err != nil { return err }; session, err := a.ReadSession(value); if err != nil { return err }; return WriteJSON(out, session) }
func HandleWriteSession(in io.Reader, a sessionWriter) error { value, err := ReadJSON[AgentSessionJSON](in); if err != nil { return err }; return a.WriteSession(*value) }
func HandleReadTranscript(args []string, out io.Writer, a transcriptReader) error { fs := newFlagSet("read-transcript"); ref := fs.String("session-ref", "", "session ref"); if err := fs.Parse(args); err != nil { return err }; if *ref == "" { return errors.New("session-ref is required") }; data, err := a.ReadTranscript(*ref); if err != nil { return err }; _, err = out.Write(data); return err }
func HandleChunkTranscript(args []string, in io.Reader, out io.Writer, a transcriptChunker) error { fs := newFlagSet("chunk-transcript"); size := fs.Int("max-size", 0, "maximum chunk size"); if err := fs.Parse(args); err != nil { return err }; data, err := io.ReadAll(in); if err != nil { return err }; chunks, err := a.ChunkTranscript(data, *size); if err != nil { return err }; return WriteJSON(out, ChunkResponse{Chunks: chunks}) }
func HandleReassembleTranscript(in io.Reader, out io.Writer, a transcriptChunker) error { value, err := ReadJSON[ChunkResponse](in); if err != nil { return err }; data, err := a.ReassembleTranscript(value.Chunks); if err != nil { return err }; _, err = out.Write(data); return err }
func HandleFormatResumeCommand(args []string, out io.Writer, a resumeFormatter) error { fs := newFlagSet("format-resume-command"); id := fs.String("session-id", "", "session id"); if err := fs.Parse(args); err != nil { return err }; return WriteJSON(out, ResumeCommandResponse{Command: a.FormatResumeCommand(*id)}) }
func DefaultSessionDir(repo string) string { return filepath.Join(repo, ".entire", "tmp") }
func ResolveSessionFile(dir, id string) string { return filepath.Join(dir, id+".json") }
func newFlagSet(name string) *flag.FlagSet { fs := flag.NewFlagSet(name, flag.ContinueOnError); fs.SetOutput(io.Discard); return fs }
