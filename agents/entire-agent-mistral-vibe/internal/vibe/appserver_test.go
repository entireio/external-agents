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
//
// Its history paging copies Vibe 3.1.2 (vibe/app_server/_state.py,
// history_page): with no cursor every direction returns the NEWEST page; a
// backward cursor names the entry to stop before; a forward cursor names the
// entry to start after. nextCursor continues in the requested direction.
type fakeAppServer struct {
	entries  int // history entries, ids "e0000".."eNNNN" oldest first
	failRead bool
	requests []map[string]any
}

func entryID(index int) string { return fmt.Sprintf("e%04d", index) }

func (s *fakeAppServer) historyPage(page map[string]any) map[string]any {
	limit := 200
	if value, ok := page["limit"].(float64); ok {
		limit = int(value)
	}
	cursor, _ := page["cursor"].(string)
	backward := page["direction"] != "forward"
	indexOf := func(id string) int {
		for i := range s.entries {
			if entryID(i) == id {
				return i
			}
		}
		return -1
	}

	var start, end int
	switch {
	case cursor != "" && !backward:
		start = indexOf(cursor) + 1
		if start == 0 {
			start = s.entries
		}
		end = min(start+limit, s.entries)
	case cursor != "" && backward:
		end = max(indexOf(cursor), 0)
		start = max(end-limit, 0)
	default:
		start, end = max(s.entries-limit, 0), s.entries
	}

	items := []map[string]string{}
	for i := start; i < end; i++ {
		items = append(items, map[string]string{"id": entryID(i), "type": "message"})
	}
	var next any
	switch {
	case len(items) == 0:
	case backward && start > 0:
		next = entryID(start)
	case !backward && end < s.entries:
		next = entryID(end - 1)
	}
	return map[string]any{"items": items, "nextCursor": next}
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
			result = s.historyPage(pageOf(request))
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

func fetchedIDs(t *testing.T, snapshot *Snapshot) []string {
	t.Helper()
	var ids []string
	for _, raw := range snapshot.Entries {
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, probe.ID)
	}
	return ids
}

func TestReadSessionReturnsWholeHistoryOldestFirst(t *testing.T) {
	// 1 entry, exactly one page, one over, and several pages.
	for _, total := range []int{0, 1, historyPageLimit, historyPageLimit + 1, 3*historyPageLimit + 7} {
		t.Run(fmt.Sprintf("%d entries", total), func(t *testing.T) {
			server := &fakeAppServer{entries: total}
			snapshot, err := readSession(startFakeServer(t, server), testSessionID)
			if err != nil {
				t.Fatal(err)
			}
			ids := fetchedIDs(t, snapshot)
			if len(ids) != total {
				t.Fatalf("got %d entries, want %d", len(ids), total)
			}
			for i, id := range ids {
				if id != entryID(i) {
					t.Fatalf("entry %d is %s, want %s: history is out of order or incomplete", i, id, entryID(i))
				}
			}
		})
	}
}

func TestReadSessionHandshakeAndSessionHeader(t *testing.T) {
	server := &fakeAppServer{entries: 3}
	snapshot, err := readSession(startFakeServer(t, server), testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Model != "mistral-medium-3.5" || snapshot.Usage == nil || snapshot.Usage.InputTokens != 10 {
		t.Errorf("snapshot = %+v", snapshot)
	}
	var methods []string
	for _, request := range server.requests {
		method, _ := request["method"].(string)
		methods = append(methods, method)
	}
	want := "initialize initialized session/read session/history/list"
	if got := strings.Join(methods, " "); got != want {
		t.Errorf("methods = %q\nwant     %q", got, want)
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

	// A relative value could name a file in the repository; it is not used.
	t.Setenv(appServerBinEnv, "vibe-app-server")
	if _, err := appServerPath(); err == nil {
		t.Error("a relative VIBE_APP_SERVER_BIN must not be executed")
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
