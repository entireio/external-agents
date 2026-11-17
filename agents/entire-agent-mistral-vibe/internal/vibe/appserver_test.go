package vibe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// fakeAppServer answers JSON-RPC requests over a pipe the way vibe-app-server
// does, and records what it was asked.
type fakeAppServer struct {
	pages    [][]string // history entry ids, one slice per page
	failRead bool
	requests []map[string]any
}

func (s *fakeAppServer) serve(t *testing.T, requests io.Reader, responses io.WriteCloser) {
	t.Helper()
	defer responses.Close()
	scanner := bufio.NewScanner(requests)
	for scanner.Scan() {
		var request map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			t.Errorf("server got invalid JSON: %s", scanner.Text())
			return
		}
		s.requests = append(s.requests, request)
		id, isRequest := request["id"]
		if !isRequest {
			continue
		}
		// A real server interleaves notifications with responses.
		fmt.Fprintln(responses, `{"jsonrpc":"2.0","method":"session/updated","params":{}}`)

		var result any
		switch request["method"] {
		case "initialize":
			result = map[string]any{"serverInfo": map[string]string{"name": "vibe-app-server"}}
		case "session/read":
			if s.failRead {
				fmt.Fprintf(responses, `{"jsonrpc":"2.0","id":%v,"error":{"code":-32000,"message":"session not found"}}`+"\n", id)
				continue
			}
			result = map[string]any{"state": map[string]any{"session": map[string]any{
				"model":      "mistral-medium-3.5",
				"tokenUsage": map[string]int{"inputTokens": 10, "outputTokens": 2},
			}}}
		case "session/history/list":
			index := 0
			if cursor, _ := pageOf(request)["cursor"].(string); cursor != "" {
				if _, err := fmt.Sscanf(cursor, "page-%d", &index); err != nil {
					t.Errorf("unexpected cursor %q", cursor)
				}
			}
			items := []map[string]string{}
			for _, entryID := range s.pages[index] {
				items = append(items, map[string]string{"id": entryID, "type": "message"})
			}
			list := map[string]any{"items": items, "nextCursor": nil}
			if index+1 < len(s.pages) {
				list["nextCursor"] = fmt.Sprintf("page-%d", index+1)
			}
			result = list
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		fmt.Fprintln(responses, string(data))
	}
}

// pageOf returns the "page" object of a session/history/list request.
func pageOf(request map[string]any) map[string]any {
	params, _ := request["params"].(map[string]any)
	page, _ := params["page"].(map[string]any)
	return page
}

func startFakeServer(t *testing.T, server *fakeAppServer) *rpcClient {
	t.Helper()
	requestReader, requestWriter := io.Pipe()
	responseReader, responseWriter := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.serve(t, requestReader, responseWriter)
	}()
	t.Cleanup(func() {
		requestWriter.Close()
		<-done
	})
	return &rpcClient{w: requestWriter, r: bufio.NewReader(responseReader)}
}

func TestReadSessionFollowsHistoryPages(t *testing.T) {
	server := &fakeAppServer{pages: [][]string{{"a", "b"}, {"c"}, {"d", "e"}}}
	snapshot, err := readSession(startFakeServer(t, server), testSessionID)
	if err != nil {
		t.Fatal(err)
	}

	var ids []string
	for _, raw := range snapshot.Entries {
		var probe struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &probe)
		ids = append(ids, probe.ID)
	}
	if got := strings.Join(ids, ""); got != "abcde" {
		t.Errorf("entries = %q, want abcde in order", got)
	}
	if snapshot.Model != "mistral-medium-3.5" || snapshot.Usage == nil || snapshot.Usage.InputTokens != 10 {
		t.Errorf("snapshot = %+v", snapshot)
	}

	var methods []string
	for _, request := range server.requests {
		method, _ := request["method"].(string)
		methods = append(methods, method)
	}
	want := "initialize initialized session/read session/history/list session/history/list session/history/list"
	if got := strings.Join(methods, " "); got != want {
		t.Errorf("methods = %q\nwant     %q", got, want)
	}
	// History is read oldest first so line order matches conversation order.
	page := pageOf(server.requests[3])
	if page["direction"] != "forward" {
		t.Errorf("first page request = %v", page)
	}
}

func TestReadSessionReportsServerError(t *testing.T) {
	server := &fakeAppServer{failRead: true}
	_, err := readSession(startFakeServer(t, server), testSessionID)
	if err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadSessionFailsWhenServerExits(t *testing.T) {
	requestReader, requestWriter := io.Pipe()
	responseReader, responseWriter := io.Pipe()
	go func() {
		_, _ = io.Copy(io.Discard, requestReader)
	}()
	responseWriter.Close()
	t.Cleanup(func() { requestWriter.Close() })

	client := &rpcClient{w: requestWriter, r: bufio.NewReader(responseReader)}
	if _, err := readSession(client, testSessionID); err == nil {
		t.Fatal("want an error when the server closes its output")
	}
}

func TestAppServerPathPrefersVibeEnvironment(t *testing.T) {
	dir := t.TempDir()
	bin := dir + "/vibe-app-server"
	writeExecutable(t, bin)

	t.Setenv("PATH", "")
	t.Setenv(appServerBinEnv, bin)
	if got, err := appServerPath(); err != nil || got != bin {
		t.Errorf("got %q, %v", got, err)
	}

	// A stale value falls back to PATH rather than failing outright.
	t.Setenv(appServerBinEnv, dir+"/gone")
	t.Setenv("PATH", dir)
	if got, err := appServerPath(); err != nil || got != bin {
		t.Errorf("fallback: got %q, %v", got, err)
	}

	t.Setenv("PATH", "")
	if _, err := appServerPath(); err == nil {
		t.Error("want an error when the app server is not installed")
	}
}

func TestFetchSessionRequiresSessionID(t *testing.T) {
	if _, err := (&AppServerFetcher{}).FetchSession(t.Context(), " ", t.TempDir()); err == nil {
		t.Fatal("want an error")
	}
}
