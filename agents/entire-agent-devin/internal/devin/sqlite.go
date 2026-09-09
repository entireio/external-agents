package devin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// sessionsDBPath returns the local Devin CLI SQLite session store.
func sessionsDBPath() (string, error) {
	dataDir, err := devinDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "cli", "sessions.db"), nil
}

type sessionRow struct {
	ID          string
	Model       string
	BackendType string
	AgentMode   string
	MainChainID sql.NullInt64
	CreatedAt   int64
	Metadata    string
}

type messageNode struct {
	NodeID       int
	ParentNodeID sql.NullInt64
	ChatMessage  string
	CreatedAt    int64
	Metadata     string
}

// chatMessage is the JSON shape stored in message_nodes.chat_message.
// It is deliberately permissive: content may be a string or an array of parts,
// and tool_calls may be OpenAI/Anthropic/ACP shaped.
type chatMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ToolCalls        []chatToolCall  `json:"tool_calls"`
	ToolCallID       string          `json:"tool_call_id"`
	Metadata         json.RawMessage `json:"metadata"`
	ReasoningDetails json.RawMessage `json:"reasoning_details"`
	Images           json.RawMessage `json:"images"`
	Model            string          `json:"model"`
	ModelName        string          `json:"model_name"`
}

type chatToolCall struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Function  json.RawMessage `json:"function"`
	Input     json.RawMessage `json:"input"`
	Arguments json.RawMessage `json:"arguments"`
}

type chatMetadata struct {
	IsUserInput bool         `json:"is_user_input"`
	NumTokens   int          `json:"num_tokens"`
	RequestID   string       `json:"request_id"`
	Metrics     *chatMetrics `json:"metrics"`
	FinishReason string      `json:"finish_reason"`
	Model       string       `json:"model"`
	ModelName   string       `json:"model_name"`
}

type chatMetrics struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	CachedTokens        int `json:"cached_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
	NumTokens           int `json:"num_tokens"`
}

// materializeLiveTranscript builds an ATIF transcript from Devin's live
// SQLite session store and writes it to the canonical transcript path. It is
// a best-effort fallback used when Devin has not yet flushed the ATIF file
// (which happens on session exit). If anything goes wrong it returns an
// error so the caller can fall back to a stub.
func (a *Agent) materializeLiveTranscript(sessionRef string) error {
	sessionID := strings.TrimSuffix(filepath.Base(sessionRef), ".json")
	if sessionID == "" {
		return errors.New("empty session id")
	}

	dbPath, err := sessionsDBPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return fmt.Errorf("devin sessions.db not found at %s", dbPath)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open sessions.db: %w", err)
	}
	defer db.Close()

	sess, err := loadSession(db, sessionID)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if sess == nil {
		return fmt.Errorf("session %q not found in sessions.db", sessionID)
	}

	nodes, err := loadMessageNodes(db, sessionID)
	if err != nil {
		return fmt.Errorf("load message_nodes: %w", err)
	}
	if len(nodes) == 0 {
		return fmt.Errorf("session %q has no message_nodes", sessionID)
	}

	steps, err := buildSteps(sess, nodes)
	if err != nil {
		return fmt.Errorf("build steps: %w", err)
	}

	transcript := ATIFTranscript{
		SchemaVersion: "ATIF-v1.7",
		SessionID:     sessionID,
		Agent:         buildAgentInfo(sess),
		Steps:         steps,
	}
	data, err := json.Marshal(transcript)
	if err != nil {
		return fmt.Errorf("marshal live transcript: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o750); err != nil {
		return fmt.Errorf("create transcript directory: %w", err)
	}
	return atomicWriteFile(sessionRef, data, 0o600)
}

func loadSession(db *sql.DB, sessionID string) (*sessionRow, error) {
	var s sessionRow
	err := db.QueryRow(`
		SELECT id, model, backend_type, agent_mode, main_chain_id, created_at, metadata
		FROM sessions WHERE id = ?
	`, sessionID).Scan(&s.ID, &s.Model, &s.BackendType, &s.AgentMode, &s.MainChainID, &s.CreatedAt, &s.Metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func loadMessageNodes(db *sql.DB, sessionID string) ([]messageNode, error) {
	rows, err := db.Query(`
		SELECT node_id, parent_node_id, chat_message, created_at, metadata
		FROM message_nodes WHERE session_id = ? ORDER BY node_id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []messageNode
	for rows.Next() {
		var n messageNode
		if err := rows.Scan(&n.NodeID, &n.ParentNodeID, &n.ChatMessage, &n.CreatedAt, &n.Metadata); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// buildSteps orders message nodes by the session's main chain, falling back
// to node_id order when no main_chain_id is recorded.
func buildSteps(sess *sessionRow, nodes []messageNode) ([]ATIFStep, error) {
	nodeMap := make(map[int]messageNode, len(nodes))
	for _, n := range nodes {
		nodeMap[n.NodeID] = n
	}

	ordered := make([]messageNode, 0, len(nodes))
	if sess.MainChainID.Valid {
		cur := int(sess.MainChainID.Int64)
		if _, ok := nodeMap[cur]; ok {
			chain := make([]int, 0, len(nodes))
			seen := make(map[int]bool, len(nodes))
			for {
				n, ok := nodeMap[cur]
				if !ok || seen[cur] {
					break
				}
				seen[cur] = true
				chain = append(chain, cur)
				if !n.ParentNodeID.Valid {
					break
				}
				cur = int(n.ParentNodeID.Int64)
			}
			for i := len(chain) - 1; i >= 0; i-- {
				ordered = append(ordered, nodeMap[chain[i]])
			}
		}
	}
	if len(ordered) == 0 {
		ordered = nodes
	}

	steps := make([]ATIFStep, 0, len(ordered))
	for _, n := range ordered {
		step, err := nodeToStep(sess, n)
		if err != nil {
			// Preserve the node as a fallback step rather than dropping it.
			step = fallbackStep(n, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func buildAgentInfo(sess *sessionRow) json.RawMessage {
	info := map[string]any{"name": "devin"}
	if sess.Model != "" {
		info["model_name"] = sess.Model
	}
	extra := map[string]any{}
	if sess.BackendType != "" {
		extra["backend_type"] = sess.BackendType
	}
	if sess.AgentMode != "" {
		extra["agent_mode"] = sess.AgentMode
	}
	if sess.Metadata != "" {
		var md map[string]any
		if json.Unmarshal([]byte(sess.Metadata), &md) == nil {
			extra["session_metadata"] = md
		}
	}
	if len(extra) > 0 {
		info["extra"] = extra
	}
	raw, _ := json.Marshal(info)
	return raw
}

func nodeToStep(sess *sessionRow, n messageNode) (json.RawMessage, error) {
	var chat chatMessage
	if err := json.Unmarshal([]byte(n.ChatMessage), &chat); err != nil {
		return nil, fmt.Errorf("unmarshal chat_message: %w", err)
	}

	nodeMD, msgMD := parseMetadata(json.RawMessage(n.Metadata), chat.Metadata)

	step := map[string]any{
		"step_id":   n.NodeID,
		"timestamp": time.UnixMilli(n.CreatedAt).UTC().Format(time.RFC3339),
		"source":    deriveSource(chat, nodeMD, msgMD),
		"message":   extractContent(chat.Content),
	}

	if modelName := deriveModelName(sess, chat, nodeMD, msgMD); modelName != "" {
		step["model_name"] = modelName
	}

	if reasoning := extractReasoning(chat.ReasoningDetails); reasoning != "" {
		step["reasoning_content"] = reasoning
	}

	toolCalls, err := convertToolCalls(chat.ToolCalls)
	if err != nil {
		return nil, err
	}
	if len(toolCalls) > 0 {
		step["tool_calls"] = toolCalls
	}

	if metrics := deriveMetrics(chat, nodeMD, msgMD); metrics != nil {
		step["metrics"] = metrics
	}

	extra := collectExtra(n, chat, nodeMD, msgMD)
	if len(extra) > 0 {
		step["extra"] = extra
	}

	return json.Marshal(step)
}

func fallbackStep(n messageNode, err error) json.RawMessage {
	step := map[string]any{
		"step_id":   n.NodeID,
		"timestamp": time.UnixMilli(n.CreatedAt).UTC().Format(time.RFC3339),
		"source":    "unknown",
		"message":   n.ChatMessage,
		"extra": map[string]any{
			"parse_error": err.Error(),
			"metadata":    n.Metadata,
		},
	}
	raw, _ := json.Marshal(step)
	return raw
}

func parseMetadata(nodeMD, msgMD json.RawMessage) (*chatMetadata, *chatMetadata) {
	var n, m *chatMetadata
	if len(nodeMD) > 0 {
		var parsed chatMetadata
		if err := json.Unmarshal(nodeMD, &parsed); err == nil {
			n = &parsed
		}
	}
	if len(msgMD) > 0 {
		var parsed chatMetadata
		if err := json.Unmarshal(msgMD, &parsed); err == nil {
			m = &parsed
		}
	}
	return n, m
}

func deriveSource(chat chatMessage, nodeMD, msgMD *chatMetadata) string {
	switch chat.Role {
	case "user":
		return "user"
	case "assistant":
		return "agent"
	case "system":
		return "system"
	case "tool":
		return "tool"
	}
	if msgMD != nil && msgMD.IsUserInput {
		return "user"
	}
	if nodeMD != nil && nodeMD.IsUserInput {
		return "user"
	}
	return "unknown"
}

func deriveModelName(sess *sessionRow, chat chatMessage, nodeMD, msgMD *chatMetadata) string {
	if msgMD != nil && msgMD.ModelName != "" {
		return msgMD.ModelName
	}
	if msgMD != nil && msgMD.Model != "" {
		return msgMD.Model
	}
	if nodeMD != nil && nodeMD.ModelName != "" {
		return nodeMD.ModelName
	}
	if nodeMD != nil && nodeMD.Model != "" {
		return nodeMD.Model
	}
	if chat.ModelName != "" {
		return chat.ModelName
	}
	if chat.Model != "" {
		return chat.Model
	}
	return sess.Model
}

func extractContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}

	var sb strings.Builder
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		var text string
		if err := json.Unmarshal(p, &text); err == nil {
			sb.WriteString(text)
			continue
		}
		var part struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(p, &part); err == nil {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

func extractReasoning(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}

	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "thinking" || p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

func convertToolCalls(calls []chatToolCall) ([]map[string]any, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	out := make([]map[string]any, 0, len(calls))
	for _, tc := range calls {
		item := map[string]any{}
		if tc.ID != "" {
			item["tool_call_id"] = tc.ID
		}

		fnName := tc.Name
		var args json.RawMessage

		if len(tc.Function) > 0 {
			var fn struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(tc.Function, &fn); err == nil {
				fnName = fn.Name
				args = fn.Arguments
			}
		}

		if args == nil && tc.Input != nil {
			args = tc.Input
		}
		if args == nil && tc.Arguments != nil {
			args = tc.Arguments
		}

		if len(args) > 0 {
			// OpenAI encodes function.arguments as a JSON string.
			var str string
			if err := json.Unmarshal(args, &str); err == nil {
				var obj any
				if err := json.Unmarshal([]byte(str), &obj); err == nil {
					b, err := json.Marshal(obj)
					if err == nil {
						args = b
					}
				}
			}
		}

		if fnName == "" {
			fnName = tc.Type
		}
		if fnName == "" {
			fnName = "<unknown>"
		}

		item["function_name"] = fnName
		item["arguments"] = args
		out = append(out, item)
	}
	return out, nil
}

func deriveMetrics(chat chatMessage, nodeMD, msgMD *chatMetadata) map[string]int {
	cm := coalesceMetrics(nodeMD, msgMD)
	if cm == nil {
		return nil
	}

	m := make(map[string]int)
	if cm.PromptTokens > 0 {
		m["prompt_tokens"] = cm.PromptTokens
	}
	if cm.CompletionTokens > 0 {
		m["completion_tokens"] = cm.CompletionTokens
	}
	if cm.CachedTokens > 0 {
		m["cached_tokens"] = cm.CachedTokens
	}

	if len(m) > 0 {
		return m
	}

	// Fallback: translate the top-level metadata token counts into the ATIF
	// shape when the nested metrics object is absent.
	role := chat.Role
	if msgMD != nil && msgMD.IsUserInput {
		role = "user"
	}
	if nodeMD != nil && nodeMD.IsUserInput {
		role = "user"
	}

	if cm.NumTokens > 0 {
		if role == "user" {
			m["prompt_tokens"] = cm.NumTokens
		} else {
			m["completion_tokens"] = cm.NumTokens
		}
	}
	if cm.CacheCreationTokens > 0 {
		m["cached_tokens"] = cm.CacheCreationTokens
	}

	if len(m) > 0 {
		return m
	}
	return nil
}

func coalesceMetrics(nodeMD, msgMD *chatMetadata) *chatMetrics {
	if msgMD != nil && msgMD.Metrics != nil {
		return msgMD.Metrics
	}
	if nodeMD != nil && nodeMD.Metrics != nil {
		return nodeMD.Metrics
	}
	return nil
}

func collectExtra(n messageNode, chat chatMessage, nodeMD, msgMD *chatMetadata) map[string]any {
	extra := make(map[string]any)

	if chat.ToolCallID != "" {
		extra["tool_call_id"] = chat.ToolCallID
	}
	if len(chat.Images) > 0 {
		extra["images"] = chat.Images
	}

	if n.Metadata != "" {
		var raw map[string]any
		if err := json.Unmarshal([]byte(n.Metadata), &raw); err == nil {
			for k, v := range raw {
				switch k {
				case "is_user_input", "num_tokens", "request_id", "metrics", "finish_reason", "model", "model_name":
					// surfaced elsewhere; skip
				default:
					extra["node_metadata_"+k] = v
				}
			}
		}
	}

	var chatRaw map[string]any
	if err := json.Unmarshal([]byte(n.ChatMessage), &chatRaw); err == nil {
		for k, v := range chatRaw {
			switch k {
			case "role", "content", "tool_calls", "tool_call_id", "reasoning_details", "images", "metadata", "model", "model_name":
				// surfaced elsewhere; skip
			default:
				extra[k] = v
			}
		}
		if md, ok := chatRaw["metadata"].(map[string]any); ok {
			for k, v := range md {
				switch k {
				case "is_user_input", "num_tokens", "request_id", "metrics", "finish_reason", "model", "model_name":
				default:
					extra["chat_metadata_"+k] = v
				}
			}
		}
	}

	return extra
}

// stepMetricsForTests exposes the metric calculation for tests that want to
// assert token accounting without building a full transcript.
func stepMetricsForTests(chat chatMessage, nodeMD, msgMD *chatMetadata) map[string]int {
	return deriveMetrics(chat, nodeMD, msgMD)
}
