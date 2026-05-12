package amp

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCompactTranscriptBytes(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "9.9.9")
	data, err := compactTranscriptBytes([]byte(testPreparedTranscriptJSON))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`"agent":"amp"`,
		`"cli_version":"9.9.9"`,
		`"type":"user"`,
		`"type":"assistant"`,
		`"name":"edit_file"`,
		`"result":{"output":"ok","status":"success"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compact transcript missing %q\n%s", want, got)
		}
	}
}

func TestCompactTranscriptResponse(t *testing.T) {
	agent := New()
	path := writePreparedTranscript(t)
	resp, err := agent.CompactTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), `"agent":"amp"`) {
		t.Fatalf("decoded = %s", decoded)
	}
}

func TestCompactTranscriptBytes_UnpreparedJSONL(t *testing.T) {
	_, err := compactTranscriptBytes([]byte(testTranscriptJSONL))
	if err == nil {
		t.Fatal("expected unprepared transcript error")
	}
}

func TestCompactTranscriptBytes_PreparedJSON(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "9.9.9")
	data, err := compactTranscriptBytes([]byte(testPreparedTranscriptJSON))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`"agent":"amp"`,
		`"cli_version":"9.9.9"`,
		`"type":"user"`,
		`"type":"assistant"`,
		`"name":"edit_file"`,
		`"result":{"output":"ok","status":"success"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compact transcript missing %q\n%s", want, got)
		}
	}
}
