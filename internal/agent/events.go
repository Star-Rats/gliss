package agent

import "gliss/internal/llm"

// EventKind 标识事件类型。
type EventKind int

const (
	EventToken       EventKind = iota // assistant 文本增量
	EventTurnStart                    // 一次 LLM 调用开始（新一轮工具循环）
	EventToolStart                    // 工具开始执行
	EventToolEnd                      // 工具执行结束
	EventTurnEnd                      // 整个用户回合结束（含 usage 汇总）
	EventInterrupted                  // 用户中断
	EventError                        // 错误
	EventIdle                         // agent 空闲，等待新输入
)

// Event 是 agent 发给 UI 的事件。
type Event struct {
	Kind EventKind

	// Token 字段（EventToken）
	Text string

	// 工具字段（EventToolStart / EventToolEnd）
	ToolCallID string
	ToolName   string
	ToolArgs   string // 原始参数 JSON
	ToolResult string // 执行结果文本
	ToolErr    bool   // 结果是否为错误文本

	// 汇总字段（EventTurnEnd）
	Usage UsageSummary

	// 错误信息（EventError）
	Err error
}

// UsageSummary 是一个用户回合的累计用量。
type UsageSummary struct {
	PromptTokens     int
	CompletionTokens int
	LLMCalls         int
	ToolCalls        int
}

// Add 累加一次 LLM 调用的用量。
func (u *UsageSummary) Add(other llm.Usage) {
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
	u.LLMCalls++
}
