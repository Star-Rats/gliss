package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// View 渲染整屏：横幅 / 历史区 / 命令菜单 / 输入框 / 快捷键提示。
func (m Model) View() string {
	if !m.ready {
		return "正在启动 Gliss…"
	}
	if m.quitting {
		return ""
	}

	parts := []string{m.renderHeader(), m.viewport.View(), ""}
	if menu := m.renderCmdMenu(); menu != "" {
		parts = append(parts, menu)
	}
	parts = append(parts, m.renderInput(), m.renderFooter())
	// 最外层按当前窗口宽度钳制每一行，保证任何组件都不会横向溢出。
	return truncateLines(lipgloss.JoinVertical(lipgloss.Left, parts...), m.width)
}

// bannerArt 是 gliss 的三行 ASCII 标志（半块字符拼出小写 g-l-i-s-s）。
var bannerArt = []string{
	"█▀▀▀ █ █ █▀▀ █▀▀",
	"█▄▄▀ █ █ ▀▀█ ▀▀█",
	"▀▄▄▀ ▀ ▀ ▀▀▀ ▀▀▀",
}

// bannerColors 让标志从青到蓝到紫纵向渐变。
var bannerColors = []lipgloss.Color{"51", "39", "99"}

// useBanner 决定是否渲染图形横幅；窗口太小则退回单行头部。
func (m *Model) useBanner() bool {
	return m.width >= 46 && m.height >= 16
}

// relayout 依据窗口尺寸与输入框行数重排组件。
func (m *Model) relayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	inputW := m.width - 4 // 两侧边框各占 1，输入区无前缀
	if inputW < 8 {
		inputW = 8
	}
	m.input.SetWidth(inputW)

	lines := strings.Count(m.input.Value(), "\n") + 1
	if lines > 5 {
		lines = 5
	}
	if lines < 1 {
		lines = 1
	}
	m.input.SetHeight(lines)

	headerH := 1
	if m.useBanner() {
		headerH = len(bannerArt) // 标志三行
	}
	const (
		gapH    = 1 // 历史区与输入框（或菜单）之间的空行
		footerH = 1
	)
	menuH := 0
	if m.cmdMenuOpen {
		menuH = len(m.cmdMatches)
	}
	// 精确计算：header + gap + viewport + [menu] + 输入框(内容+上下边框) + footer = 窗口高
	vpH := m.height - headerH - gapH - (lines + 2) - footerH - menuH
	if vpH < 3 {
		vpH = 3
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpH
}

func (m *Model) renderHeader() string {
	if m.useBanner() {
		return m.renderBanner()
	}
	return m.renderCompactHeader()
}

// renderBanner 渲染渐变标志 + 右侧信息栏（品牌 / 模型与状态 / 目录与用量），
// 信息栏与标志同为 3 行，逐行对齐；信息按可用宽度自适应。
func (m *Model) renderBanner() string {
	art := make([]string, len(bannerArt))
	for i, line := range bannerArt {
		art[i] = lipgloss.NewStyle().
			Foreground(bannerColors[i]).
			Bold(true).
			Render(line)
	}
	logo := lipgloss.JoinVertical(lipgloss.Left, art...)
	logoW := lipgloss.Width(art[0])
	budget := m.width - logoW - 4 // 标志 + 间距后的信息栏可用宽度
	if budget < 8 {
		budget = 8
	}

	session := m.ag.TotalUsage()
	tokens := fmt.Sprintf("↑%d ↓%d", session.PromptTokens, session.CompletionTokens)

	// 第三行：目录（尾部截断保留末级路径）+ token 统计
	cwdBudget := budget - lipgloss.Width(tokens) - 2
	var line3 string
	if cwdBudget >= 8 {
		line3 = styleHeaderDim.Render(tailPath(shortenHome(m.cfg.WorkDir), cwdBudget)) +
			styleDim.Render("  "+tokens)
	} else {
		line3 = styleDim.Render(tokens)
	}

	infoLines := []string{
		ansi.Truncate(styleHeader.Render("◆ gliss"), budget, "…"),
		ansi.Truncate(styleHeaderDim.Render(m.model)+" "+m.statusBadge(), budget, "…"),
		ansi.Truncate(line3, budget, "…"),
	}
	info := lipgloss.JoinVertical(lipgloss.Left, infoLines...)
	out := lipgloss.JoinHorizontal(lipgloss.Center, logo, "    ", info)
	return truncateLines(out, m.width)
}

// renderCompactHeader 是窄窗口/矮窗口下的单行头部。
func (m *Model) renderCompactHeader() string {
	session := m.ag.TotalUsage()
	right := fmt.Sprintf("↑%d ↓%d  %s",
		session.PromptTokens, session.CompletionTokens, m.statusBadge())

	left := styleHeader.Render("◆ gliss") + styleHeaderDim.Render(" "+m.model)
	// 剩余空间足够时才展示目录（尾部截断）
	avail := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 6
	if avail >= 8 {
		left += styleHeaderDim.Render(" · " + tailPath(shortenHome(m.cfg.WorkDir), avail))
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, m.width, "")
}

// tailPath 从路径尾部保留最多 max 列，超出时以 … 前缀截断头部。
func tailPath(p string, max int) string {
	if max < 4 {
		return ""
	}
	if lipgloss.Width(p) <= max {
		return p
	}
	budget := max - 1 // 为 … 留 1 列
	runes := []rune(p)
	w := 0
	start := len(runes)
	for start > 0 {
		rw := 1
		if runes[start-1] > 0x2E80 {
			rw = 2
		}
		if w+rw > budget {
			break
		}
		w += rw
		start--
	}
	return "…" + string(runes[start:])
}

// truncateLines 把多行文本逐行截断到 width 显示宽度。
func truncateLines(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "")
	}
	return strings.Join(lines, "\n")
}

// shortenHome 把主目录前缀缩写为 ~。
func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func (m *Model) statusBadge() string {
	switch {
	case m.agentDead:
		return styleWarn.Render("● 已结束")
	case m.busy:
		return m.spinner.View() + styleHeaderDim.Render("思考中…")
	default:
		return styleOK.Render("● 就绪")
	}
}

func (m *Model) renderInput() string {
	return styleBorder.
		Width(m.width - 2).
		Render(m.input.View())
}

// renderCmdMenu 渲染输入框上方的命令候选菜单。
func (m *Model) renderCmdMenu() string {
	if !m.cmdMenuOpen || len(m.cmdMatches) == 0 {
		return ""
	}
	lines := make([]string, 0, len(m.cmdMatches))
	for i, c := range m.cmdMatches {
		label := "/" + c.name
		if c.args != "" {
			label += " " + c.args
		}
		if i == m.cmdSelected {
			lines = append(lines, styleMenuSel.Render("▸ "+label)+styleHeaderDim.Render("  "+c.desc))
		} else {
			lines = append(lines, styleDim.Render("  "+label+"  "+c.desc))
		}
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderFooter() string {
	return styleDim.Render(footerHints(m.width, m.expandTools))
}

// footerHints 按窗口宽度分级选择快捷键提示，保证完整显示不被截断。
func footerHints(width int, expandTools bool) string {
	full := "enter 发送 · shift+enter 换行 · esc 中断/清空 · ctrl+t 工具详情 · pgup/pgdn 滚动 · /help 命令 · ctrl+c 退出"
	medium := "enter 发送 · shift+enter 换行 · esc 中断 · ctrl+t 工具详情 · /help · ctrl+c 退出"
	short := "enter 发送 · esc 中断 · /help · ctrl+c 退出"
	tiny := "enter 发送 · /help · ctrl+c"

	hints := tiny
	switch {
	case width >= 110:
		hints = full
	case width >= 84:
		hints = medium
	case width >= 47:
		hints = short
	}
	if expandTools {
		hints = strings.Replace(hints, "ctrl+t 工具详情", "ctrl+t 收起工具", 1)
	}
	return hints
}

// refreshContent 重建历史区内容；位于底部时保持自动跟随。
func (m *Model) refreshContent(forceBottom bool) {
	var parts []string
	contentWidth := m.width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	for i := range m.blocks {
		b := &m.blocks[i]
		// 运行中的工具块不缓存（状态会变化）；其余块内容不可变，缓存渲染结果。
		if b.cacheHit && b.cacheWide == contentWidth && !(b.kind == blockTool && !b.toolDone) {
			parts = append(parts, b.cached)
			continue
		}
		rendered := m.renderBlock(b, contentWidth)
		b.cached = rendered
		b.cacheWide = contentWidth
		if !(b.kind == blockTool && !b.toolDone) {
			b.cacheHit = true
		}
		parts = append(parts, rendered)
	}

	// 流式中的 assistant 文本：软换行直出（完成后才做 markdown 渲染）。
	if m.streaming.Len() > 0 {
		text := strings.TrimRight(m.streaming.String(), "\n")
		prefix := styleAssistantPrefix.Render("◆ ")
		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Top,
			prefix, wrapText(text, contentWidth-2, styleAssistant)))
	}

	m.viewport.SetContent(strings.Join(parts, "\n\n"))
	if forceBottom || m.viewport.AtBottom() {
		m.viewport.GotoBottom()
	}
}

// renderBlock 渲染单个历史块。
func (m *Model) renderBlock(b *block, width int) string {
	switch b.kind {
	case blockUser:
		prefix := styleUserPrefix.Render("❯ ")
		return lipgloss.JoinHorizontal(lipgloss.Top,
			prefix, wrapText(b.text, width-2, styleUser))

	case blockAssistant:
		// glamour 的 document 样式自带 2 格左 margin，前缀不再加空格以对齐。
		prefix := styleAssistantPrefix.Render("◆")
		body := m.md.Render(b.text, width-2)
		return lipgloss.JoinHorizontal(lipgloss.Top, prefix, body)

	case blockTool:
		return m.renderToolBlock(b, width)

	case blockError:
		return wrapText("✗ "+b.text, width, styleError)

	case blockInfo:
		return wrapText("· "+b.text, width, styleWarn)

	case blockUsage:
		return styleDim.Render("── " + b.text + " ──")
	}
	return b.text
}

// renderToolBlock 渲染工具调用块：默认单行摘要，expandTools 时展开参数与结果。
func (m *Model) renderToolBlock(b *block, width int) string {
	icon, statusStyle := "●", styleDim
	switch {
	case !b.toolDone:
		icon = m.spinner.View()
	case b.toolErr:
		icon, statusStyle = "✗", styleToolErr
	default:
		icon, statusStyle = "✓", styleToolOK
	}

	name := styleToolName.Render(b.toolName)
	argSummary := styleDim.Render(compactArgs(b.toolArgs))

	var head string
	if b.toolDone {
		head = fmt.Sprintf("%s %s %s %s",
			statusStyle.Render(icon), name, argSummary,
			statusStyle.Render("→ "+firstLine(b.toolResult)))
	} else {
		head = icon + " " + name + " " + argSummary
	}
	head = ansi.Truncate(head, width, "…")

	if !m.expandTools || !b.toolDone {
		return head
	}

	var sb strings.Builder
	sb.WriteString(head)
	sb.WriteString("\n")
	sb.WriteString(styleDim.Render("  参数: ") + wrapText(prettyJSON(b.toolArgs), width-6, styleTool))
	sb.WriteString("\n")
	sb.WriteString(styleDim.Render("  结果: ") + wrapText(truncateRunesFor(b.toolResult, 2000), width-6, styleTool))
	return sb.String()
}

// compactArgs 生成工具参数的单行摘要。
func compactArgs(argsJSON string) string {
	if strings.TrimSpace(argsJSON) == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return argsJSON
	}

	keys := []string{"path", "pattern", "command", "include"}
	var parts []string
	for _, k := range keys {
		if v, ok := m[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	for k, v := range m {
		switch k {
		case "path", "pattern", "command", "include":
			// 已按优先顺序输出
		case "content":
			if s, ok := v.(string); ok {
				parts = append(parts, fmt.Sprintf("%s=<%d 字符>", k, len([]rune(s))))
			}
		case "old_string", "new_string":
			if s, ok := v.(string); ok {
				parts = append(parts, fmt.Sprintf("%s=<%d 字符>", k, len([]rune(s))))
			}
		default:
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	return strings.Join(parts, " ")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len([]rune(s)) > 120 {
		s = string([]rune(s)[:120]) + "…"
	}
	return s
}

func prettyJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

func truncateRunesFor(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "\n…（已截断）"
}
