package sarvam

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func jsonStringOf(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func splitLines(s string) []string {
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func unmarshalStrict(t *testing.T, data string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), v); err != nil {
		t.Fatalf("unmarshal %s: %v", preview(data), err)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func preview(s string) string {
	if len(s) > 80 {
		return fmt.Sprintf("%.80s…", s)
	}
	return s
}
