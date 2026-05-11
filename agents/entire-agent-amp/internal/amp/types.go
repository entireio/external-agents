package amp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ThreadUnixMillis is an epoch timestamp encoded as milliseconds.
type ThreadUnixMillis int64

func (m ThreadUnixMillis) Time() time.Time {
	if m == 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(m)).UTC()
}

// Thread is the top-level Amp thread transcript document.
type Thread struct {
	Version         int                    `json:"v"`
	ID              string                 `json:"id"`
	Env             ThreadEnvironment      `json:"env"`
	Meta            ThreadMeta             `json:"meta"`
	Title           string                 `json:"title"`
	Created         ThreadUnixMillis       `json:"created"`
	Messages        []ThreadMessage        `json:"messages"`
	AgentMode       string                 `json:"agentMode"`
	UpdatedAt       time.Time              `json:"updatedAt"`
	CreatorUserID   string                 `json:"creatorUserID"`
	NextMessageID   int                    `json:"nextMessageId"`
	ActivatedSkills []ThreadActivatedSkill `json:"activatedSkills,omitempty"`
}

// ThreadEnvironment describes the editor, workspace, and platform state captured for
// the transcript.
type ThreadEnvironment struct {
	Initial ThreadInitialEnvironment `json:"initial"`
}

type ThreadInitialEnvironment struct {
	Tags     []string              `json:"tags,omitempty"`
	Trees    []ThreadWorkspaceTree `json:"trees,omitempty"`
	Platform ThreadPlatform        `json:"platform"`
}

type ThreadWorkspaceTree struct {
	URI         string           `json:"uri"`
	Repository  ThreadRepository `json:"repository"`
	DisplayName string           `json:"displayName"`
}

type ThreadRepository struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

type ThreadPlatform struct {
	OS                string `json:"os"`
	Client            string `json:"client"`
	OSVersion         string `json:"osVersion"`
	ClientType        string `json:"clientType"`
	WebBrowser        bool   `json:"webBrowser"`
	ClientVersion     string `json:"clientVersion"`
	InstallationID    string `json:"installationID"`
	CPUArchitecture   string `json:"cpuArchitecture"`
	DeviceFingerprint string `json:"deviceFingerprint"`
}

// ThreadMeta contains transcript-level metadata and execution traces.
type ThreadMeta struct {
	Traces          []ThreadTrace `json:"traces,omitempty"`
	Deleted         bool          `json:"deleted"`
	UsesDtw         bool          `json:"usesDtw"`
	AgentMode       string        `json:"agentMode"`
	ProjectID       *string       `json:"projectID"`
	Visibility      string        `json:"visibility"`
	WorkspaceID     *string       `json:"workspaceID"`
	SharedGroupIDs  []string      `json:"sharedGroupIDs,omitempty"`
	CreatedOnServer bool          `json:"createdOnServer"`
	LastUserMessage time.Time     `json:"lastUserMessageAt"`
}

type ThreadTrace struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Label      string                 `json:"label,omitempty"`
	Parent     string                 `json:"parent,omitempty"`
	Context    ThreadTraceContext     `json:"context"`
	StartTime  time.Time              `json:"startTime"`
	EndTime    time.Time              `json:"endTime"`
	Events     []ThreadTraceEvent     `json:"events,omitempty"`
	Attributes *ThreadTraceAttributes `json:"attributes,omitempty"`
}

type ThreadTraceContext struct {
	MessageID *ThreadMessageID `json:"messageId,omitempty"`
}

type ThreadTraceEvent struct {
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type ThreadTraceAttributes struct {
	Plugin *ThreadPluginTraceAttributes `json:"plugin,omitempty"`
}

type ThreadPluginTraceAttributes struct {
	Hook       string `json:"hook"`
	PluginName string `json:"pluginName"`
}

type ThreadActivatedSkill struct {
	Name string `json:"name"`
}

type ThreadMessageID string

func (id *ThreadMessageID) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*id = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*id = ThreadMessageID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*id = ThreadMessageID(n.String())
		return nil
	}
	return fmt.Errorf("invalid thread message id: %s", trimmed)
}

func (id ThreadMessageID) MarshalJSON() ([]byte, error) {
	if id == "" {
		return []byte("null"), nil
	}
	if _, err := strconv.Atoi(string(id)); err == nil {
		return []byte(string(id)), nil
	}
	return json.Marshal(string(id))
}

// ThreadMessageRole is an Amp transcript message role.
type ThreadMessageRole string

const (
	ThreadMessageRoleAssistant ThreadMessageRole = "assistant"
	ThreadMessageRoleInfo      ThreadMessageRole = "info"
	ThreadMessageRoleUser      ThreadMessageRole = "user"
)

// ThreadMessage is a user, assistant, or informational entry in the transcript.
type ThreadMessage struct {
	Meta                 *ThreadMessageMeta         `json:"meta,omitempty"`
	Role                 ThreadMessageRole          `json:"role"`
	Content              []ThreadContentBlock       `json:"content,omitempty"`
	AgentMode            string                     `json:"agentMode,omitempty"`
	MessageID            ThreadMessageID            `json:"messageId"`
	UserState            *ThreadUserState           `json:"userState,omitempty"`
	FileMentions         *ThreadFileMentions        `json:"fileMentions,omitempty"`
	State                *ThreadAssistantState      `json:"state,omitempty"`
	Usage                *ThreadUsage               `json:"usage,omitempty"`
	TurnElapsedMillis    int64                      `json:"turnElapsedMs,omitempty"`
	Interrupted          *bool                      `json:"interrupted,omitempty"`
	NativeMessage        json.RawMessage            `json:"nativeMessage,omitempty"`
	OriginalToolUseInput map[string]ThreadToolInput `json:"originalToolUseInput,omitempty"`
}

type ThreadMessageMeta struct {
	SentAt ThreadUnixMillis `json:"sentAt"`
}

type ThreadUserState struct {
	ActiveEditor          string                `json:"activeEditor,omitempty"`
	CursorLocation        *ThreadCursorLocation `json:"cursorLocation,omitempty"`
	CursorLocationLine    string                `json:"cursorLocationLine,omitempty"`
	CurrentlyVisibleFiles []string              `json:"currentlyVisibleFiles,omitempty"`
	SelectionRange        *ThreadSelectionRange `json:"selectionRange,omitempty"`
}

type ThreadCursorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type ThreadFileMentions struct {
	Files    []ThreadMentionedFile `json:"files,omitempty"`
	Mentions []ThreadFileMention   `json:"mentions,omitempty"`
}

type ThreadMentionedFile struct {
	URI     string `json:"uri"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content"`
	IsImage bool   `json:"isImage"`
}

type ThreadFileMention struct {
	URI  string `json:"uri,omitempty"`
	Path string `json:"path,omitempty"`
}

type ThreadSelectionRange struct {
	Content string             `json:"content,omitempty"`
	Start   ThreadTextPosition `json:"start"`
	End     ThreadTextPosition `json:"end"`
}

type ThreadTextPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type ThreadAssistantState struct {
	Type       string `json:"type"`
	StopReason string `json:"stopReason"`
}

type ThreadUsage struct {
	Model                    string    `json:"model"`
	Timestamp                time.Time `json:"timestamp"`
	InputTokens              int       `json:"inputTokens"`
	OutputTokens             int       `json:"outputTokens"`
	MaxInputTokens           int       `json:"maxInputTokens"`
	TotalInputTokens         int       `json:"totalInputTokens"`
	CacheReadInputTokens     int       `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int       `json:"cacheCreationInputTokens"`
	Credits                  float64   `json:"credits,omitempty"`
	CreditsSinceLastUser     float64   `json:"creditsSinceLastUserMessage,omitempty"`
}

// ThreadContentType is the discriminator for ThreadMessage.Content items.
type ThreadContentType string

const (
	ThreadContentText             ThreadContentType = "text"
	ThreadContentThinking         ThreadContentType = "thinking"
	ThreadContentImage            ThreadContentType = "image"
	ThreadContentToolUse          ThreadContentType = "tool_use"
	ThreadContentToolResult       ThreadContentType = "tool_result"
	ThreadContentServerToolUse    ThreadContentType = "server_tool_use"
	ThreadContentToolSearchResult ThreadContentType = "tool_search_tool_result"
)

// ThreadContentBlock represents one item in a message content array.
//
// Text and thinking blocks use Text/Thinking. Tool-use blocks use ID, Name,
// Input, Complete, StartTime, and FinalTime. Tool-result blocks use ToolUseID
// and Run. Input is raw because its schema depends on the tool name.
type ThreadContentBlock struct {
	Type      ThreadContentType `json:"type"`
	Text      string            `json:"text,omitempty"`
	Provider  string            `json:"provider,omitempty"`
	Thinking  string            `json:"thinking,omitempty"`
	Signature string            `json:"signature,omitempty"`
	Source    *ThreadSource     `json:"source,omitempty"`
	UserInput *ThreadUserInput  `json:"userInput,omitempty"`

	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Input    ThreadToolInput `json:"input,omitempty"`
	Complete *bool           `json:"complete,omitempty"`

	ToolUseID string         `json:"toolUseID,omitempty"`
	Run       *ThreadToolRun `json:"run,omitempty"`

	StartTime ThreadUnixMillis `json:"startTime,omitempty"`
	FinalTime ThreadUnixMillis `json:"finalTime,omitempty"`
}

type ThreadSource struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

type ThreadUserInput struct {
	Accepted bool `json:"accepted"`
}

// ThreadToolInput is a tool-specific input object keyed by argument name.
type ThreadToolInput map[string]json.RawMessage

type ThreadToolRun struct {
	Status     string              `json:"status"`
	Result     json.RawMessage     `json:"result,omitempty"`
	Error      *ThreadToolRunError `json:"error,omitempty"`
	Progress   json.RawMessage     `json:"progress,omitempty"`
	TrackFiles []string            `json:"trackFiles,omitempty"`
	Debug      json.RawMessage     `json:"~debug,omitempty"`
}

type ThreadToolRunError struct {
	Message string `json:"message"`
}

// ThreadCommonToolResult captures fields shared by many Amp tool results. Use
// ThreadToolRun.DecodeResult for stricter tool-specific structs.
type ThreadCommonToolResult struct {
	Output           string                  `json:"output,omitempty"`
	ExitCode         int                     `json:"exitCode,omitempty"`
	Diff             string                  `json:"diff,omitempty"`
	LineRange        []int                   `json:"lineRange,omitempty"`
	AbsolutePath     string                  `json:"absolutePath,omitempty"`
	DirectoryEntries []string                `json:"directoryEntries,omitempty"`
	IsDirectory      *bool                   `json:"isDirectory,omitempty"`
	Content          []ThreadEmbeddedContent `json:"content,omitempty"`
}

type ThreadEmbeddedContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
