package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCmdMenuFilter(t *testing.T) {
	cases := []struct {
		input  string
		open   bool
		expect []string // 期望的候选命令名，nil 表示不检查
	}{
		{"/", true, []string{"help", "clear", "model", "usage", "tools", "exit"}},
		{"/m", true, []string{"model"}},
		{"/MO", true, []string{"model"}},
		{"/c", true, []string{"clear"}},
		{"/x", false, nil},
		{"/model glm", false, nil}, // 已进入参数输入，不弹菜单
		{"/etc/hosts 是什么", false, nil},
		{"普通消息", false, nil},
		{"", false, nil},
	}
	for _, c := range cases {
		got, open := filterCommands(c.input)
		if open != c.open {
			t.Errorf("filterCommands(%q) open = %v, 期望 %v", c.input, open, c.open)
		}
		if c.expect == nil {
			continue
		}
		if len(got) != len(c.expect) {
			t.Errorf("filterCommands(%q) = %v, 期望 %v", c.input, names(got), c.expect)
			continue
		}
		for i := range got {
			if got[i].name != c.expect[i] {
				t.Errorf("filterCommands(%q)[%d] = %s, 期望 %s", c.input, i, got[i].name, c.expect[i])
			}
		}
	}
}

func names(items []commandItem) []string {
	out := make([]string, len(items))
	for i, c := range items {
		out[i] = c.name
	}
	return out
}

func TestCmdMenuInteraction(t *testing.T) {
	ag, cfg := newScriptedAgent(t, [][]string{{textSSE("ok")}})
	m := New(ag, cfg)
	var cm tea.Model = m
	cm, _ = cm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = cm.(Model)

	typeKey := func(m Model, key tea.KeyMsg) Model {
		cm, _ := m.Update(key)
		return cm.(Model)
	}
	typeText := func(m Model, s string) Model {
		for _, r := range s {
			m = typeKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		return m
	}

	// 输入 / 弹出全部候选
	m = typeText(m, "/")
	if !m.cmdMenuOpen || len(m.cmdMatches) != 6 {
		t.Fatalf("输入 / 后菜单应打开且含 6 个候选: open=%v n=%d", m.cmdMenuOpen, len(m.cmdMatches))
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "▸ /help") || !strings.Contains(v, "显示命令与快捷键帮助") {
		t.Errorf("菜单应高亮第一项并显示说明:\n%s", v)
	}

	// ↓ 移动到第二项
	m = typeKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cmdSelected != 1 {
		t.Fatalf("down 后选中项 = %d, 期望 1", m.cmdSelected)
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "▸ /clear") {
		t.Errorf("down 后应高亮 /clear:\n%s", v)
	}

	// ↑ 回到第一项并 wrap 到最后一项
	m = typeKey(m, tea.KeyMsg{Type: tea.KeyUp})
	m = typeKey(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.cmdSelected != 5 {
		t.Fatalf("向上 wrap 后选中项 = %d, 期望 5", m.cmdSelected)
	}

	// tab 补全到输入框，菜单因参数空格关闭
	m.input.Reset()
	m = typeText(m, "/cle") // 唯一候选，选中项归零
	if !m.cmdMenuOpen || len(m.cmdMatches) != 1 {
		t.Fatalf("/cle 应只匹配 clear: open=%v n=%d", m.cmdMenuOpen, len(m.cmdMatches))
	}
	m = typeKey(m, tea.KeyMsg{Type: tea.KeyTab})
	if got := m.input.Value(); got != "/clear " {
		t.Fatalf("tab 补全后输入 = %q, 期望 %q", got, "/clear ")
	}
	if m.cmdMenuOpen {
		t.Fatal("补全后输入含空格，菜单应关闭")
	}

	// esc 关闭菜单但保留输入
	m.input.Reset()
	m = typeText(m, "/cl")
	if !m.cmdMenuOpen {
		t.Fatal("/cl 应打开菜单")
	}
	m = typeKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.cmdMenuOpen || m.input.Value() != "/cl" {
		t.Fatalf("esc 应仅关闭菜单并保留输入: open=%v input=%q", m.cmdMenuOpen, m.input.Value())
	}

	// 继续输入重新打开菜单，enter 执行选中命令
	m = typeText(m, "e") // /cle
	if !m.cmdMenuOpen {
		t.Fatal("/cle 应打开菜单")
	}
	m = typeKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if v := stripANSI(m.View()); !strings.Contains(v, "会话已清空") {
		t.Errorf("enter 应执行选中命令:\n%s", v)
	}
	if m.input.Value() != "" {
		t.Errorf("执行后输入框应清空: %q", m.input.Value())
	}

	// /mo + enter 直接执行 /model（无参数查看当前模型）
	m = typeText(m, "/mo")
	m = typeKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if v := stripANSI(m.View()); !strings.Contains(v, "当前模型：test-model") {
		t.Errorf("/mo + enter 应执行 /model:\n%s", v)
	}
}

func TestCmdMenuEnterFallsThroughToSubmit(t *testing.T) {
	// 无候选时（未知 / 命令），enter 应走普通发送路径
	ag, cfg := newScriptedAgent(t, [][]string{{textSSE("ok")}})
	m := New(ag, cfg)
	var cm tea.Model = m
	cm, _ = cm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = cm.(Model)

	for _, r := range "/xyz" {
		cm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = cm.(Model)
	}
	if m.cmdMenuOpen {
		t.Fatal("无候选时菜单应关闭")
	}
	cm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = cm.(Model)
	if len(m.blocks) != 1 || m.blocks[0].kind != blockUser || !m.busy {
		t.Fatalf("未知 / 命令应作为普通消息发送: %+v", m.blocks)
	}
}
