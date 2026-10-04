// Package agent 实现 coding agent 的核心会话循环：
// 接收用户输入，驱动 LLM 流式调用与工具执行，向 UI 发送事件。
package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gliss/internal/llm"
	"gliss/internal/tools"
)

// Agent 持有一段会话的全部状态，运行在独立 goroutine 中。
type Agent struct {
	client *llm.Client
	reg    *tools.Registry
	sys    string

	maxTurns int

	mu       sync.Mutex
	messages []llm.Message

	prompts   chan string
	events    chan Event
	done      chan struct{}
	closeOnce sync.Once

	// stateMu 保护 cancel 与 totalUsage。
	stateMu    sync.Mutex
	cancel     context.CancelFunc
	totalUsage UsageSummary
}

// New 创建 agent 并启动会话循环 goroutine。
func New(client *llm.Client, reg *tools.Registry, systemPrompt string, maxTurns int) *Agent {
	a := &Agent{
		client:   client,
		reg:      reg,
		sys:      systemPrompt,
		maxTurns: maxTurns,
		prompts:  make(chan string, 16),
		events:   make(chan Event, 256),
		done:     make(chan struct{}),
	}
	if a.maxTurns <= 0 {
		a.maxTurns = 40
	}
	a.messages = append(a.messages, llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	go a.run()
	return a
}

// Events 返回事件通道。agent 停止后通道关闭。
func (a *Agent) Events() <-chan Event { return a.events }

// Submit 提交一条用户输入。agent 忙碌时输入会排队。
func (a *Agent) Submit(prompt string) {
	select {
	case a.prompts <- prompt:
	case <-a.done:
	}
}

// Interrupt 中断当前回合（Esc 触发）。空闲时无效果。
func (a *Agent) Interrupt() {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// Stop 终止会话循环。中断进行中的回合并关闭 prompts；
// 事件通道由会话 goroutine 在退出时关闭。
func (a *Agent) Stop() {
	a.Interrupt()
	a.closeOnce.Do(func() { close(a.prompts) })
}

// Done 返回会话循环结束信号（用于测试或优雅退出等待）。
func (a *Agent) Done() <-chan struct{} { return a.done }

// TotalUsage 返回会话累计用量快照。
func (a *Agent) TotalUsage() UsageSummary {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.totalUsage
}

// Reset 清空会话历史与累计用量，仅保留系统提示词。忙碌时由 UI 负责拒绝调用。
func (a *Agent) Reset() {
	a.mu.Lock()
	a.messages = a.messages[:1] // system
	a.mu.Unlock()
	a.stateMu.Lock()
	a.totalUsage = UsageSummary{}
	a.stateMu.Unlock()
}

// SetModel 切换模型名，对下一次 LLM 调用生效。
func (a *Agent) SetModel(model string) {
	a.client.SetModel(model)
}

// CurrentModel 返回当前模型名。
func (a *Agent) CurrentModel() string {
	return a.client.ModelName()
}

// ToolSummaries 返回全部工具的 "名称: 描述" 列表。
func (a *Agent) ToolSummaries() []string {
	defs := a.reg.Definitions()
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Function.Name+": "+d.Function.Description)
	}
	return out
}

func (a *Agent) addUsage(u llm.Usage) {
	a.stateMu.Lock()
	a.totalUsage.Add(u)
	a.stateMu.Unlock()
}

// run 是会话主循环：等待输入 → 处理回合 → 回到等待。
func (a *Agent) run() {
	defer close(a.done)
	defer close(a.events)
	for prompt := range a.prompts {
		a.handleTurn(prompt)
		a.emit(Event{Kind: EventIdle})
	}
}

// handleTurn 处理一条用户输入：LLM 调用与工具循环，直到无工具调用或达上限。
func (a *Agent) handleTurn(prompt string) {
	ctx, cancel := context.WithCancel(context.Background())
	a.stateMu.Lock()
	a.cancel = cancel
	a.stateMu.Unlock()
	defer cancel()
	defer func() {
		a.stateMu.Lock()
		a.cancel = nil
		a.stateMu.Unlock()
	}()

	a.mu.Lock()
	a.messages = append(a.messages, llm.Message{Role: llm.RoleUser, Content: prompt})
	a.mu.Unlock()

	turnUsage := UsageSummary{}
	for turn := 0; turn < a.maxTurns; turn++ {
		if ctx.Err() != nil {
			break
		}
		a.emit(Event{Kind: EventTurnStart})

		a.mu.Lock()
		msgs := make([]llm.Message, len(a.messages))
		copy(msgs, a.messages)
		a.mu.Unlock()

		reply, events := a.client.ChatStream(ctx, msgs, a.reg.Definitions())
		var streamErr error
		for ev := range events {
			switch {
			case ev.Err != nil:
				streamErr = ev.Err
			case ev.Token != "":
				a.emit(Event{Kind: EventToken, Text: ev.Token})
			}
		}
		if streamErr != nil {
			// 中断（用户取消）与真实错误分开处理。
			if errors.Is(streamErr, context.Canceled) || ctx.Err() != nil {
				break
			}
			a.emit(Event{Kind: EventError, Err: streamErr})
			// 回滚本回合开头追加的 user 消息，让用户重试时历史保持干净。
			// 仅在尚未进入工具循环（最后一条仍是 user）时回滚。
			a.mu.Lock()
			if n := len(a.messages); n > 0 && a.messages[n-1].Role == llm.RoleUser {
				a.messages = a.messages[:n-1]
			}
			a.mu.Unlock()
			return
		}

		turnUsage.Add(reply.Usage)
		a.addUsage(reply.Usage)

		// 无工具调用：回合自然结束。
		if len(reply.ToolCalls) == 0 {
			a.emit(Event{Kind: EventTurnEnd, Usage: turnUsage})
			return
		}

		// 记录 assistant 的工具调用意图，再逐个执行并回填结果。
		a.mu.Lock()
		a.messages = append(a.messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   reply.Content,
			ToolCalls: append([]llm.ToolCall(nil), reply.ToolCalls...),
		})
		a.mu.Unlock()
		turnUsage.ToolCalls += len(reply.ToolCalls)

		if reply.Content != "" {
			// 有些模型在发起工具调用的同时也输出说明文本，补换行分隔。
			a.emit(Event{Kind: EventToken, Text: "\n"})
		}

		for _, tc := range reply.ToolCalls {
			a.emit(Event{Kind: EventToolStart, ToolCallID: tc.ID, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments})
			result := a.reg.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			isErr := strings.HasPrefix(result, "错误:")
			a.emit(Event{Kind: EventToolEnd, ToolCallID: tc.ID, ToolName: tc.Function.Name, ToolResult: result, ToolErr: isErr})
			a.mu.Lock()
			a.messages = append(a.messages, llm.Message{
				Role:       llm.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})
			a.mu.Unlock()

			if ctx.Err() != nil {
				break
			}
		}
		if ctx.Err() != nil {
			break
		}
	}

	if ctx.Err() != nil {
		a.emit(Event{Kind: EventInterrupted})
		return
	}
	// 达到轮数上限：硬停并告知 UI。
	a.emit(Event{Kind: EventError, Err: fmt.Errorf("已达单回合 %d 次 LLM 调用上限，已强制停止（会话历史保留，可继续对话）", a.maxTurns)})
	a.emit(Event{Kind: EventTurnEnd, Usage: turnUsage})
}

// emit 发送事件；agent 已停止时丢弃。
func (a *Agent) emit(ev Event) {
	select {
	case a.events <- ev:
	case <-a.done:
	}
}

// SystemPrompt 生成内置系统提示词。
func SystemPrompt(workDir string) string {
	var sb strings.Builder
	sb.WriteString(`你是 Gliss，一个运行在终端里的编程助手（coding agent）。你通过调用工具完成用户的编程任务。

工作准则：
- 工作目录是用户的当前目录。修改文件前先 read_file 了解现状；优先用 edit_file 做精确修改，新建文件才用 write_file。
- 搜索代码用 grep（按内容）与 glob（按文件名）；list_dir 了解项目结构。
- 可以用 run_bash 执行构建、测试、git 等命令。改动代码后，如项目有测试应尽量运行验证。
- 一次只做一件事：每个工具调用使用尽量小的范围（读相关片段而非整个大文件）。
- edit_file 的 old_string 必须与文件内容逐字符一致（含缩进），且在文件内唯一；不唯一时扩大上下文或用 replace_all。
- 路径都限制在工作目录内；工作目录之外的请求要向用户说明无法执行。
- 回答用简体中文，代码、标识符与命令保持原文。最终总结要简洁：改了什么、为什么、如何验证。
- 当前日期：`)
	sb.WriteString(time.Now().Format("2006-01-02"))
	fmt.Fprintf(&sb, "\n- 操作系统: %s (%s)\n- 工作目录: %s\n", runtime.GOOS, runtime.GOARCH, filepath.Clean(workDir))
	return sb.String()
}
