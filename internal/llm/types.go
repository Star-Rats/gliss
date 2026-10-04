// Package llm 实现一个精简的 OpenAI 兼容 Chat Completions 客户端，
// 支持 SSE 流式输出与 tool_calls 增量累积。
package llm

// Role 是消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall 是一次函数调用请求（assistant 消息内）。
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // 固定 "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON 字符串
	} `json:"function"`
}

// Message 是一条对话消息。ToolCallID 仅用于 role=tool 的回填消息。
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// FunctionDef 描述一个可调用函数的参数 schema。
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolDef 是发给 API 的工具定义。
type ToolDef struct {
	Type     string      `json:"type"` // 固定 "function"
	Function FunctionDef `json:"function"`
}

// Usage 是一次请求的 token 用量统计。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// ---- 请求体 ----

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

// ---- 流式响应的分片 ----

// streamChunk 是 SSE data 中一个 chat.completion.chunk 的映射。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			FinishReason *string `json:"finish_reason"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// streamAccumulator 把分片累积成一次完整的 assistant 回复。
type streamAccumulator struct {
	content   string
	toolCalls []ToolCall // 按 Index 有序
	usage     Usage
}

func (a *streamAccumulator) add(chunk *streamChunk) {
	if chunk.Usage != nil {
		a.usage = *chunk.Usage
	}
	for _, choice := range chunk.Choices {
		a.content += choice.Delta.Content
		for _, tc := range choice.Delta.ToolCalls {
			for len(a.toolCalls) <= tc.Index {
				a.toolCalls = append(a.toolCalls, ToolCall{Type: "function"})
			}
			dst := &a.toolCalls[tc.Index]
			if tc.ID != "" {
				dst.ID = tc.ID
			}
			if tc.Function.Name != "" {
				dst.Function.Name += tc.Function.Name
			}
			dst.Function.Arguments += tc.Function.Arguments
		}
	}
}

// StreamEvent 是流式过程中的单个增量事件。
type StreamEvent struct {
	// Token 是一段文本增量。
	Token string
	// Done 为 true 表示流结束。
	Done bool
	// Err 非 nil 表示流出错（此时 Done 也为 true）。
	Err error
}
