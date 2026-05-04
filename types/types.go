package types

import "encoding/json"

// ─── Anthropic Request Types ──────────────────────────────────────────────────

type AnthropicRequest struct {
	Model         string     `json:"model"`
	MaxTokens     int        `json:"max_tokens"`
	Messages      []AMessage `json:"messages"`
	System        any        `json:"system,omitempty"`
	Tools         []ATool    `json:"tools,omitempty"`
	Stream        bool       `json:"stream"`
	Thinking      *AThinking `json:"thinking,omitempty"`
	Temperature   *float64   `json:"temperature,omitempty"`
	TopP          *float64   `json:"top_p,omitempty"`
	TopK          *int       `json:"top_k,omitempty"`
	StopSequences []string   `json:"stop_sequences,omitempty"`
}

type AMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type AContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
}

type ATool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type AThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// WebToolInput is the parsed JSON input for web_fetch and web_search tool calls.
type WebToolInput struct {
	URL   string `json:"url"`
	Query string `json:"query"`
}

// ─── OpenAI Request Types ─────────────────────────────────────────────────────

type OAIRequest struct {
	Model               string         `json:"model"`
	MaxTokens           int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	Messages            []OAIMessage   `json:"messages"`
	Tools               []OAITool      `json:"tools,omitempty"`
	Stream              bool           `json:"stream"`
	StreamOptions       *StreamOptions `json:"stream_options,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	TopP                *float64       `json:"top_p,omitempty"`
	Stop                []string       `json:"stop,omitempty"`
	// Thinking is forwarded to providers that support extended thinking (e.g. NVIDIA NIM).
	// Most providers silently ignore unknown fields.
	Thinking *AThinking `json:"thinking,omitempty"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type OAIMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content"`
	ToolCalls  []OAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
}

type OAIContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type OAITool struct {
	Type     string      `json:"type"`
	Function OAIFunction `json:"function"`
}

type OAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type OAIToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function OAIFuncCall `json:"function"`
}

type OAIFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ─── OpenAI SSE Response ──────────────────────────────────────────────────────

type OAIStreamChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []OAIChoice `json:"choices"`
	Usage   *OAIUsage   `json:"usage,omitempty"`
}

type OAIChoice struct {
	Index        int      `json:"index"`
	Delta        OAIDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type OAIDelta struct {
	Role             string             `json:"role,omitempty"`
	Content          *string            `json:"content"`
	ReasoningContent *string            `json:"reasoning_content,omitempty"`
	ToolCalls        []OAIToolCallDelta `json:"tool_calls,omitempty"`
}

type OAIToolCallDelta struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function OAIFuncDelta `json:"function"`
}

type OAIFuncDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type OAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OAIResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []OAIChoiceComplete `json:"choices"`
	Usage   OAIUsage            `json:"usage"`
}

type OAIChoiceComplete struct {
	Index        int        `json:"index"`
	Message      OAIMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

// ─── Anthropic Response Types ─────────────────────────────────────────────────

type AResponse struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Role         string          `json:"role"`
	Content      []AContentBlock `json:"content"`
	Model        string          `json:"model"`
	StopReason   *string         `json:"stop_reason"`
	StopSequence *string         `json:"stop_sequence"`
	Usage        AUsage          `json:"usage"`
}

type AUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ─── Anthropic SSE Event types ────────────────────────────────────────────────

type MessageStartEvent struct {
	Type    string    `json:"type"`
	Message AResponse `json:"message"`
}

type ContentBlockStartEvent struct {
	Type         string            `json:"type"`
	Index        int               `json:"index"`
	ContentBlock ContentBlockStart `json:"content_block"`
}

type ContentBlockStart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Input    any    `json:"input,omitempty"`
}

type ContentBlockDeltaEvent struct {
	Type  string       `json:"type"`
	Index int          `json:"index"`
	Delta ContentDelta `json:"delta"`
}

type ContentDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

type ContentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type MessageDeltaEvent struct {
	Type  string       `json:"type"`
	Delta MessageDelta `json:"delta"`
	Usage AUsage       `json:"usage"`
}

type MessageDelta struct {
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type MessageStopEvent struct {
	Type string `json:"type"`
}

// ─── Anthropic Error ──────────────────────────────────────────────────────────

type AnthropicError struct {
	Type  string      `json:"type"`
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ─── /v1/models response ─────────────────────────────────────────────────────

type ModelsResponse struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ─── Count Tokens ─────────────────────────────────────────────────────────────

type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// ─── Provider ─────────────────────────────────────────────────────────────────

type Provider struct {
	Name         string
	BaseURL      string
	APIKey       string
	ExtraHeaders map[string]string
	Proxy        string // optional HTTP/SOCKS5 proxy URL for this provider
}
