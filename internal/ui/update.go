package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"gliss/internal/agent"
)

// Update 处理键盘、窗口与 agent 事件。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.relayout()
		m.refreshContent(true)
		// 缩放后强制整屏重绘，清除旧尺寸渲染的右侧残留。
		return m, tea.ClearScreen

	case tea.KeyMsg:
		// 命令菜单打开时，导航键优先被菜单消费。
		if m.cmdMenuOpen && len(m.cmdMatches) > 0 {
			n := len(m.cmdMatches)
			switch msg.String() {
			case "up":
				m.cmdSelected = (m.cmdSelected - 1 + n) % n
				return m, nil
			case "down":
				m.cmdSelected = (m.cmdSelected + 1) % n
				return m, nil
			case "tab":
				c := m.cmdMatches[m.cmdSelected]
				m.input.SetValue("/" + c.name + " ")
				m.refreshCmdMenu()
				return m, nil
			case "esc":
				m.cmdMenuOpen = false
				return m, nil
			case "enter":
				c := m.cmdMatches[m.cmdSelected]
				return m, m.execCommand(c.name, "")
			}
		}

		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			if m.busy {
				m.ag.Interrupt()
				m.addBlock(block{kind: blockInfo, text: "正在中断…"})
				m.refreshContent(true)
				return m, nil
			}
			if m.input.Value() != "" {
				m.input.Reset()
				m.refreshCmdMenu()
				return m, nil
			}
		case "enter":
			return m, m.submit()
		case "shift+enter":
			m.input.InsertString("\n")
			m.relayout()
			return m, nil
		case "ctrl+t":
			m.expandTools = !m.expandTools
			m.invalidateToolCache()
			m.refreshContent(true)
			return m, nil
		case "pgup":
			m.viewport.HalfPageUp()
		case "pgdown":
			m.viewport.HalfPageDown()
		case "home":
			m.viewport.GotoTop()
		case "end":
			m.viewport.GotoBottom()
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case agentClosedMsg:
		m.agentDead = true
		m.addBlock(block{kind: blockInfo, text: "会话已结束。按 ctrl+c 退出。"})
		m.refreshContent(true)
		return m, nil

	case agentEventMsg:
		m.applyAgentEvent(msg.ev)
		cmds = append(cmds, waitForAgentEvent(m.ag))
	}

	// 其余按键交给输入框，并按新内容刷新命令菜单。
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	m.refreshCmdMenu()
	return m, tea.Batch(cmds...)
}

// refreshCmdMenu 依据输入框内容重算命令菜单候选与高度。
func (m *Model) refreshCmdMenu() {
	m.cmdMatches, m.cmdMenuOpen = filterCommands(m.input.Value())
	if m.cmdSelected >= len(m.cmdMatches) {
		m.cmdSelected = 0
	}
	m.relayout()
}

// submit 发送输入框内容；/ 开头的已知命令走命令通道。
func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	if cmd, args, ok := matchCommand(text); ok {
		return m.execCommand(cmd, args)
	}
	if m.busy || m.agentDead {
		return nil
	}
	m.input.Reset()
	m.refreshCmdMenu()
	m.addBlock(block{kind: blockUser, text: text})
	m.busy = true
	m.streaming.Reset()
	m.ag.Submit(text)
	m.refreshContent(true)
	return nil
}

// applyAgentEvent 把 agent 事件落到界面状态。
func (m *Model) applyAgentEvent(ev agent.Event) {
	switch ev.Kind {
	case agent.EventToken:
		m.streaming.WriteString(ev.Text)

	case agent.EventTurnStart:
		// 新一轮 LLM 调用：把此前累积的流式文本落成一个 assistant 块。
		m.flushStreaming()

	case agent.EventToolStart:
		m.flushStreaming()
		m.blocks = append(m.blocks, block{
			kind:     blockTool,
			toolName: ev.ToolName,
			toolArgs: ev.ToolArgs,
		})

	case agent.EventToolEnd:
		// 找到对应的 pending 工具块（最后一个未完成块），完成后失效其渲染缓存。
		for i := len(m.blocks) - 1; i >= 0; i-- {
			b := &m.blocks[i]
			if b.kind == blockTool && !b.toolDone {
				b.toolDone = true
				b.toolResult = ev.ToolResult
				b.toolErr = ev.ToolErr
				b.cacheHit = false
				break
			}
		}

	case agent.EventTurnEnd:
		m.flushStreaming()
		m.usage = ev.Usage
		m.busy = false
		m.addBlock(block{kind: blockUsage, text: usageLine(ev.Usage)})

	case agent.EventInterrupted:
		m.flushStreaming()
		m.busy = false
		m.addBlock(block{kind: blockInfo, text: "已中断。"})

	case agent.EventError:
		m.flushStreaming()
		m.busy = false
		m.addBlock(block{kind: blockError, text: ev.Err.Error()})

	case agent.EventIdle:
		// 忙碌状态由 TurnEnd/Error/Interrupted 复位，Idle 仅做兜底。
		m.busy = false
	}

	if ev.Kind == agent.EventToken {
		// 流式 token 高频到达：只追加尾部内容，避免整段重排的抖动。
		m.refreshContent(false)
	} else {
		m.refreshContent(true)
	}
}

// flushStreaming 把流式缓冲落成一个 assistant 块。
func (m *Model) flushStreaming() {
	if m.streaming.Len() == 0 {
		return
	}
	m.addBlock(block{kind: blockAssistant, text: m.streaming.String()})
	m.streaming.Reset()
}

// addBlock 追加块并保持自动滚底。
func (m *Model) addBlock(b block) {
	m.blocks = append(m.blocks, b)
}

// invalidateToolCache 在展开状态切换后重建工具块缓存。
func (m *Model) invalidateToolCache() {
	for i := range m.blocks {
		if m.blocks[i].kind == blockTool {
			m.blocks[i].cacheHit = false
		}
	}
}

func usageLine(u agent.UsageSummary) string {
	parts := []string{fmt.Sprintf("%d 次 LLM 调用", u.LLMCalls)}
	if u.ToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("%d 次工具调用", u.ToolCalls))
	}
	if u.PromptTokens > 0 || u.CompletionTokens > 0 {
		parts = append(parts, fmt.Sprintf("tokens ↑%d ↓%d", u.PromptTokens, u.CompletionTokens))
	}
	return "回合结束：" + strings.Join(parts, "，")
}
