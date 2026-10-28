package goose

import (
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

// nestedUsageExport is a trimmed `goose session export --format json` document
// in the shape goose has written since 1.39.0, where the flat
// accumulated_*_tokens fields were replaced by nested Usage objects. The
// accumulated_usage figures are the ones reported against goose 1.46.0 in
// upstream issue 74; usage (the current context window) is deliberately smaller
// so the tests can tell which object was read.
//
// goose documents Usage.input_tokens as all prompt tokens, cache reads and
// writes included (crates/goose-provider-types/src/conversation/token_usage.rs),
// so fresh input here is 124611 - 112820 - 11777 = 14, and
// 14 + 112820 + 11777 + 394 = 125005 = total_tokens.
const nestedUsageExport = `{
  "id": "20260819_1",
  "working_dir": "/tmp/workspace",
  "name": "Nested usage session",
  "created_at": "2026-08-19T10:00:00Z",
  "updated_at": "2026-08-19T10:05:00Z",
  "usage": {
    "input_tokens": 60000,
    "output_tokens": 100,
    "total_tokens": 60100,
    "cache_read_input_tokens": 50000,
    "cache_write_input_tokens": 9000
  },
  "accumulated_usage": {
    "input_tokens": 124611,
    "output_tokens": 394,
    "total_tokens": 125005,
    "cache_read_input_tokens": 112820,
    "cache_write_input_tokens": 11777
  },
  "accumulated_cost": null,
  "message_count": 2,
  "provider_name": "anthropic",
  "model_config": {"model_name": "claude-sonnet-4-5"},
  "conversation": [
    {"id": "m1", "role": "user", "created": 1787133600, "content": [{"type": "text", "text": "hi"}]},
    {"id": "m2", "role": "assistant", "created": 1787133601, "content": [{"type": "text", "text": "hello"}]}
  ]
}`

const nestedConversation = `"conversation": [
    {"id": "m1", "role": "user", "created": 1787133600, "content": [{"type": "text", "text": "hi"}]},
    {"id": "m2", "role": "assistant", "created": 1787133601, "content": [{"type": "text", "text": "hello"}]}
  ]`

func tokenExport(usageFields string) string {
	return `{"id": "20260819_1", "name": "s", ` + usageFields + `, ` + nestedConversation + `}`
}

func TestCalculateTokensExportShapes(t *testing.T) {
	tests := []struct {
		name   string
		export string
		want   protocol.TokenUsageResponse
	}{
		{
			name:   "accumulated_usage is preferred over usage",
			export: nestedUsageExport,
			want: protocol.TokenUsageResponse{
				InputTokens: 14, OutputTokens: 394,
				CacheReadTokens: 112820, CacheCreationTokens: 11777,
				APICallCount: 1,
			},
		},
		{
			name: "usage is used when accumulated_usage is all null",
			export: tokenExport(`
  "usage": {"input_tokens": 1500, "output_tokens": 40, "total_tokens": 1540, "cache_read_input_tokens": 1000, "cache_write_input_tokens": 200},
  "accumulated_usage": {"input_tokens": null, "output_tokens": null, "total_tokens": null, "cache_read_input_tokens": null, "cache_write_input_tokens": null}`),
			want: protocol.TokenUsageResponse{
				InputTokens: 300, OutputTokens: 40,
				CacheReadTokens: 1000, CacheCreationTokens: 200,
				APICallCount: 1,
			},
		},
		{
			name:   "usage is used when accumulated_usage is absent",
			export: tokenExport(`"usage": {"input_tokens": 70, "output_tokens": 7}`),
			want:   protocol.TokenUsageResponse{InputTokens: 70, OutputTokens: 7, APICallCount: 1},
		},
		{
			name:   "pre-1.39 flat accumulated fields still read",
			export: exportFixture,
			want:   protocol.TokenUsageResponse{InputTokens: 5650, OutputTokens: 124, APICallCount: 2},
		},
		{
			name: "accumulated_usage without input or output counts falls back to usage",
			export: tokenExport(`
  "usage": {"input_tokens": 300, "output_tokens": 200, "total_tokens": 500, "cache_read_input_tokens": null, "cache_write_input_tokens": null},
  "accumulated_usage": {"input_tokens": null, "output_tokens": null, "total_tokens": 500, "cache_read_input_tokens": null, "cache_write_input_tokens": null}`),
			want: protocol.TokenUsageResponse{InputTokens: 300, OutputTokens: 200, APICallCount: 1},
		},
		{
			name: "null cache fields count as no cache",
			export: tokenExport(`
  "accumulated_usage": {"input_tokens": 900, "output_tokens": 30, "total_tokens": 930, "cache_read_input_tokens": null, "cache_write_input_tokens": null}`),
			want: protocol.TokenUsageResponse{InputTokens: 900, OutputTokens: 30, APICallCount: 1},
		},
		{
			name: "legitimately zero accumulated_usage stays zero",
			export: tokenExport(`
  "usage": {"input_tokens": 0, "output_tokens": 0, "total_tokens": 0, "cache_read_input_tokens": 0, "cache_write_input_tokens": 0},
  "accumulated_usage": {"input_tokens": 0, "output_tokens": 0, "total_tokens": 0, "cache_read_input_tokens": 0, "cache_write_input_tokens": 0}`),
			want: protocol.TokenUsageResponse{APICallCount: 1},
		},
		{
			name:   "no token fields at all reports zero without error",
			export: tokenExport(`"accumulated_cost": null`),
			want:   protocol.TokenUsageResponse{APICallCount: 1},
		},
		{
			name: "cache larger than input never yields negative fresh input",
			export: tokenExport(`
  "accumulated_usage": {"input_tokens": 10, "output_tokens": 1, "cache_read_input_tokens": 25}`),
			want: protocol.TokenUsageResponse{OutputTokens: 1, CacheReadTokens: 25, APICallCount: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&Agent{}).CalculateTokens([]byte(tt.export), 0)
			if err != nil {
				t.Fatalf("CalculateTokens: %v", err)
			}
			if got != tt.want {
				t.Fatalf("tokens = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Entire calls calculate-tokens on the STORED transcript, not the native
// export, so the nested totals must survive materialization and stay readable
// from a scoped slice that has lost the first lines.
func TestCalculateTokensNestedUsageSurvivesStoredTranscript(t *testing.T) {
	stored, ok := materializeExport([]byte(nestedUsageExport))
	if !ok {
		t.Fatal("nestedUsageExport did not materialize")
	}
	want := protocol.TokenUsageResponse{
		InputTokens: 14, OutputTokens: 394,
		CacheReadTokens: 112820, CacheCreationTokens: 11777,
	}
	for _, tc := range []struct {
		name     string
		data     []byte
		apiCalls int
	}{
		{"full stored transcript", stored, 1},
		{"scoped slice", sliceFromLine(stored, 1), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (&Agent{}).CalculateTokens(tc.data, 0)
			if err != nil {
				t.Fatalf("CalculateTokens: %v", err)
			}
			want.APICallCount = tc.apiCalls
			if got != want {
				t.Fatalf("tokens = %+v, want %+v", got, want)
			}
		})
	}
}
