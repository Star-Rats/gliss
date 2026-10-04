package ui

import "github.com/charmbracelet/lipgloss"

// 主题色板（终端 256 色，兼容大多数配色方案）。
var (
	colorPrimary = lipgloss.Color("39")  // 蓝：模型名、标题
	colorUser    = lipgloss.Color("254") // 亮灰：用户输入
	colorDim     = lipgloss.Color("241") // 弱化：提示、分隔、工具摘要
	colorOK      = lipgloss.Color("70")  // 绿：成功
	colorErr     = lipgloss.Color("203") // 红：错误
	colorWarn    = lipgloss.Color("214") // 黄：中断、警告
	colorTool    = lipgloss.Color("140") // 紫：工具名
)

var (
	styleHeader = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleHeaderDim = lipgloss.NewStyle().
			Foreground(colorDim)

	styleUser = lipgloss.NewStyle().
			Foreground(colorUser)

	styleUserPrefix = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleAssistant = lipgloss.NewStyle()

	styleAssistantPrefix = lipgloss.NewStyle().
				Foreground(colorTool).
				Bold(true)

	styleTool = lipgloss.NewStyle().
			Foreground(colorDim)

	styleToolName = lipgloss.NewStyle().
			Foreground(colorTool).
			Bold(true)

	styleToolOK = lipgloss.NewStyle().
			Foreground(colorOK)

	styleMenuSel = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleOK = lipgloss.NewStyle().
		Foreground(colorOK)

	styleToolErr = lipgloss.NewStyle().
			Foreground(colorErr)

	styleError = lipgloss.NewStyle().
			Foreground(colorErr)

	styleWarn = lipgloss.NewStyle().
			Foreground(colorWarn)

	styleDim = lipgloss.NewStyle().
			Foreground(colorDim)

	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	stylePlaceholder = lipgloss.NewStyle().
				Foreground(lipgloss.Color("238"))

	// streamingWrap 在渲染时做软换行，保留 ANSI 转义。
	streamingWrap = lipgloss.NewStyle()
)
