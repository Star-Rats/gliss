// Package ui 实现 Gliss 的 Bubble Tea 终端界面。
package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gliss/internal/agent"
	"gliss/internal/config"
)

// blockKind 区分聊天历史中的块类型。
type blockKind int

const (
	blockUser blockKind = iota
	blockAssistant
	blockTool
	blockError
	blockInfo
	blockUsage
)

// block 是历史区中一个可独立渲染的块。
type block struct {
	kind blockKind

	// 文本（user / assistant / error / info）
	text string

	// 工具块
	toolName   string
	toolArgs   string
	toolResult string
	toolErr    bool
	toolDone   bool

	// 渲染缓存：内容不再变化的块只渲染一次。
	cached    string
	cacheHit  bool
	cacheWide int // 生成缓存时的可用宽度
}

// Model 是 Bubble Tea 根模型。
type Model struct {
	ag  *agent.Agent
	cfg *config.Config

	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model
	md       *markdownRenderer

	blocks      []block
	streaming   strings.Builder // 正在流式输出的 assistant 文本
	busy        bool            // agent 是否在处理
	expandTools bool            // ctrl+t 展开/折叠工具详情
	usage       agent.UsageSummary
	model       string // 当前模型名（/model 可切换）

	// 斜杠命令菜单状态
	cmdMenuOpen bool
	cmdSelected int
	cmdMatches  []commandItem

	width, height int
	ready         bool
	quitting      bool
	agentDead     bool // 事件通道已关闭
}

// New 构造初始模型。
func New(ag *agent.Agent, cfg *config.Config) Model {
	ta := textarea.New()
	ta.Placeholder = "输入任务，/help 查看命令，Enter 发送…"
	ta.Prompt = "> " // Claude Code 风格的输入提示符
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	// 去掉默认的光标行背景色块与占位文字背景，仅保留暗色前景。
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = stylePlaceholder
	ta.BlurredStyle.Placeholder = stylePlaceholder
	// 提示符与历史区用户消息的 ❯ 同色，标记"这是你的输入"。
	ta.FocusedStyle.Prompt = styleUserPrefix
	ta.BlurredStyle.Prompt = styleUserPrefix
	ta.Cursor.SetMode(cursor.CursorStatic) // 白块光标，常亮不闪烁
	ta.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(styleDim))

	vp := viewport.New(80, 20)

	return Model{
		ag:       ag,
		cfg:      cfg,
		input:    ta,
		viewport: vp,
		spinner:  sp,
		md:       newMarkdownRenderer(),
		model:    ag.CurrentModel(),
	}
}

// Init 启动事件泵与 spinner。
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, waitForAgentEvent(m.ag))
}

// agentEventMsg 包装从 agent 事件通道读到的事件。
type agentEventMsg struct{ ev agent.Event }

// waitForAgentEvent 阻塞读取一个 agent 事件（Bubble Tea 事件泵标准模式）。
func waitForAgentEvent(ag *agent.Agent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ag.Events()
		if !ok {
			return agentClosedMsg{}
		}
		return agentEventMsg{ev: ev}
	}
}

// agentClosedMsg 表示 agent 会话已终止（事件通道关闭）。
type agentClosedMsg struct{}
