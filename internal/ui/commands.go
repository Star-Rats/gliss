package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"gliss/internal/agent"
)

// commandItem 是一个斜杠命令的展示条目。
type commandItem struct {
	name string // 命令名（不含 /）
	args string // 参数占位提示，可为空
	desc string // 一句话说明
}

// commandItems 是全部命令，同时用于菜单候选与命令识别。
var commandItems = []commandItem{
	{"help", "", "显示命令与快捷键帮助"},
	{"clear", "", "清空会话历史与用量统计（别名 /new）"},
	{"model", "[名称]", "查看或切换模型，如 /model glm-4.7"},
	{"usage", "", "查看会话累计 token 用量"},
	{"tools", "", "列出全部内置工具及说明"},
	{"exit", "", "退出 Gliss（别名 /quit）"},
}

// commandAliases 把别名映射到规范命令名。
var commandAliases = map[string]string{
	"new":  "clear",
	"quit": "exit",
}

// filterCommands 根据当前输入计算命令菜单候选。
// 仅当输入以 / 开头且不含空白（尚未开始输参数）时返回候选。
func filterCommands(input string) ([]commandItem, bool) {
	if !strings.HasPrefix(input, "/") || strings.ContainsAny(input, " \t\n") {
		return nil, false
	}
	q := strings.ToLower(strings.TrimPrefix(input, "/"))
	var out []commandItem
	for _, c := range commandItems {
		if q == "" || strings.HasPrefix(c.name, q) {
			out = append(out, c)
		}
	}
	return out, len(out) > 0
}

// matchCommand 判断输入是否为已知命令（含别名）。是则返回规范命令名与参数；
// 未知的 / 开头文本不算命令（便于询问含 / 的路径问题），按普通消息发送。
func matchCommand(text string) (cmd, args string, ok bool) {
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	fields := strings.Fields(text)
	name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if alias, isAlias := commandAliases[name]; isAlias {
		name = alias
	}
	for _, c := range commandItems {
		if c.name == name {
			return name, strings.TrimSpace(strings.Join(fields[1:], " ")), true
		}
	}
	return "", "", false
}

// execCommand 执行斜杠命令，返回需要 bubbletea 执行的 Cmd（如退出）。
func (m *Model) execCommand(cmd, args string) tea.Cmd {
	m.input.Reset()
	m.refreshCmdMenu()
	defer m.refreshContent(true)

	// 退出命令任何时候都可用。
	if cmd == "exit" || cmd == "quit" {
		m.quitting = true
		m.ag.Stop()
		return tea.Quit
	}

	if m.agentDead {
		m.addBlock(block{kind: blockInfo, text: "会话已结束，按 ctrl+c 退出。"})
		return nil
	}
	if m.busy {
		m.addBlock(block{kind: blockInfo, text: "agent 正在执行，Esc 中断后再试。"})
		return nil
	}

	switch cmd {
	case "help":
		m.addBlock(block{kind: blockInfo, text: commandHelp})

	case "clear", "new":
		m.ag.Reset()
		m.blocks = nil
		m.streaming.Reset()
		m.usage = agent.UsageSummary{}
		m.addBlock(block{kind: blockInfo, text: "会话已清空（历史与用量归零，系统提示词保留）。"})

	case "model":
		if args == "" {
			m.addBlock(block{kind: blockInfo, text: "当前模型：" + m.model})
			return nil
		}
		m.ag.SetModel(args)
		m.model = m.ag.CurrentModel()
		m.addBlock(block{kind: blockInfo, text: "模型已切换为 " + m.model})

	case "usage":
		u := m.ag.TotalUsage()
		text := "会话累计：" + usageLine(u)
		if u.LLMCalls == 0 {
			text = "会话累计：还没有对话记录。"
		}
		m.addBlock(block{kind: blockInfo, text: text})

	case "tools":
		m.addBlock(block{kind: blockInfo, text: "可用工具：\n  " + strings.Join(m.ag.ToolSummaries(), "\n  ")})
	}
	return nil
}

const commandHelp = `可用命令：
  /help            显示本帮助
  /clear           清空会话历史与用量统计（别名 /new）
  /model [名称]    查看或切换模型，如 /model glm-4.7
  /usage           查看会话累计 token 用量
  /tools           列出可用工具
  /exit            退出 Gliss（别名 /quit）

输入 / 时会弹出命令菜单：↑↓ 选择，tab 补全，enter 执行，esc 关闭。
快捷键：enter 发送 · shift+enter 换行 · esc 中断/清空 · ctrl+t 工具详情 · pgup/pgdn 滚动
未知的 / 开头输入会作为普通消息发送给模型。`
