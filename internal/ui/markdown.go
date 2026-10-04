package ui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// markdownRenderer 线程安全地包装 glamour（其 TermRenderer 非并发安全）。
type markdownRenderer struct {
	mu  sync.Mutex
	r   *glamour.TermRenderer
	wid int
}

func newMarkdownRenderer() *markdownRenderer {
	return &markdownRenderer{wid: -1}
}

// Render 渲染 markdown 文本；宽度变化时重建 renderer。
// 渲染失败时返回原文（不致命）。
func (m *markdownRenderer) Render(text string, width int) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if width < 40 {
		width = 40
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.r == nil || m.wid != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(width),
			glamour.WithEmoji(),
		)
		if err != nil {
			return text
		}
		m.r = r
		m.wid = width
	}
	out, err := m.r.Render(text)
	if err != nil {
		return text
	}
	// glamour 输出自带首尾空行，去掉以免块间距过大。
	return strings.Trim(out, "\n")
}

// wrapText 软换行并按样式着色。
func wrapText(s string, width int, style lipgloss.Style) string {
	if width < 10 {
		width = 10
	}
	return style.Width(width).Render(s)
}
