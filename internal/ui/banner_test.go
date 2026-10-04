package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// stripANSI 去掉 ANSI 转义序列，便于断言纯文本布局。
func stripANSI(s string) string {
	re := regexp.MustCompile("\x1b\\[[0-9;]*m")
	return re.ReplaceAllString(s, "")
}

func TestBannerLayout(t *testing.T) {
	ag, cfg := newScriptedAgent(t, [][]string{{textSSE("ok")}})
	m := New(ag, cfg)
	var cm tea.Model = m
	// 用较宽的窗口，避免测试环境的超长临时目录路径被宽度截断
	cm, _ = cm.Update(tea.WindowSizeMsg{Width: 150, Height: 30})
	m = cm.(Model)

	view := stripANSI(m.View())
	lines := strings.Split(view, "\n")
	if len(lines) < 4 {
		t.Fatalf("视图行数不足: %q", view)
	}

	// 横幅三行：标志逐行与信息栏同行出现。
	wantPerLine := []struct {
		art  string
		info string
	}{
		{"█▀▀▀ █ █ █▀▀ █▀▀", "◆ gliss"},
		{"█▄▄▀ █ █ ▀▀█ ▀▀█", "test-model"},
		{"▀▄▄▀ ▀ ▀ ▀▀▀ ▀▀▀", ""},
	}
	for i, w := range wantPerLine {
		if !strings.Contains(lines[i], w.art) {
			t.Errorf("横幅第 %d 行缺少标志 %q:\n%s", i+1, w.art, lines[i])
		}
		if w.info != "" && !strings.Contains(lines[i], w.info) {
			t.Errorf("横幅第 %d 行缺少信息 %q:\n%s", i+1, w.info, lines[i])
		}
	}
	// 状态徽章在第二行。
	if !strings.Contains(lines[1], "就绪") {
		t.Errorf("横幅第 2 行应包含状态徽章:\n%s", lines[1])
	}
	// 第三行包含工作目录与 token 统计（测试目录为绝对路径，无 ~ 缩写）。
	if !strings.Contains(lines[2], "/") || !strings.Contains(lines[2], "↑0 ↓0") {
		t.Errorf("横幅第 3 行应包含目录与 token 统计:\n%s", lines[2])
	}
}

func TestCompactHeaderFallback(t *testing.T) {
	ag, cfg := newScriptedAgent(t, [][]string{{textSSE("ok")}})
	m := New(ag, cfg)
	var cm tea.Model = m
	// 高度不足时应退回单行头部。
	cm, _ = cm.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	m = cm.(Model)

	view := stripANSI(m.View())
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "◆ gliss") || !strings.Contains(lines[0], "test-model") {
		t.Errorf("紧凑头部应含品牌与模型:\n%s", lines[0])
	}
	if strings.Contains(view, "█▀▀▀") {
		t.Error("矮窗口不应渲染图形横幅")
	}
}

func TestInputStyleClean(t *testing.T) {
	ag, cfg := newScriptedAgent(t, [][]string{{textSSE("ok")}})
	m := New(ag, cfg)
	var cm tea.Model = m
	cm, _ = cm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = cm.(Model)

	view := m.View()
	plain := stripANSI(view)
	// 边框竖线之后不应再紧跟提示符竖线（"││" 或 "│ │" 模式）
	if strings.Contains(plain, "││") || strings.Contains(plain, "│ │") {
		t.Error("输入框不应包含竖线前缀提示符")
	}
	// 全界面不使用背景色（48; 为 ANSI 背景色码前缀）
	if strings.Contains(view, "48;5;") || strings.Contains(view, "48;2;") {
		t.Error("界面不应出现背景色")
	}
	// 光标应为白块常亮（不闪烁）
	if m.input.Cursor.Mode() != cursor.CursorStatic {
		t.Errorf("光标模式 = %v, 期望 CursorStatic", m.input.Cursor.Mode())
	}
}

// assertLineWidths 校验视图每一行的显示宽度都不超过窗口宽度。
func assertLineWidths(t *testing.T, view string, width int) {
	t.Helper()
	for i, l := range strings.Split(view, "\n") {
		if w := lipgloss.Width(l); w > width {
			t.Errorf("第 %d 行显示宽度 %d 超过窗口宽 %d: %q", i+1, w, width, stripANSI(l))
		}
	}
}

func TestWindowResizeAdaptive(t *testing.T) {
	ag, cfg := newScriptedAgent(t, [][]string{{textSSE("ok")}})
	m := New(ag, cfg)
	var cm tea.Model = m
	cm, _ = cm.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	m = cm.(Model)

	// 打开命令菜单增加布局元素
	for _, r := range "/" {
		cm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = cm.(Model)
	}
	if !m.cmdMenuOpen {
		t.Fatal("菜单应打开")
	}

	// 缩小窗口：所有行不得超宽
	cm, _ = m.Update(tea.WindowSizeMsg{Width: 58, Height: 18})
	m = cm.(Model)
	assertLineWidths(t, m.View(), 58)

	// 再缩到最小可用宽度
	cm, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 14})
	m = cm.(Model)
	assertLineWidths(t, m.View(), 30)

	// 放大回原尺寸：布局恢复，不残留旧宽度
	cm, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	m = cm.(Model)
	assertLineWidths(t, m.View(), 110)
	if v := stripANSI(m.View()); !strings.Contains(v, "█▀▀▀ █ █ █▀▀ █▀▀") {
		t.Errorf("放大后应恢复图形横幅:\n%s", v)
	}
	if m.viewport.Width != 110 {
		t.Errorf("viewport 宽度 = %d, 期望 110", m.viewport.Width)
	}
}
