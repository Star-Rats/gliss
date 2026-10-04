package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gliss/internal/agent"
	"gliss/internal/config"
	"gliss/internal/llm"
	"gliss/internal/tools"
)

// newScriptedAgent 启动返回固定脚本响应的 LLM 服务器与 agent。
func newScriptedAgent(t *testing.T, responses [][]string) (*agent.Agent, *config.Config) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		idx := int(calls.Add(1)) - 1
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, e := range responses[min(idx, len(responses)-1)] {
			_, _ = w.Write([]byte("data: " + e + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	cfg := &config.Config{BaseURL: srv.URL, APIKey: "k", Model: "test-model", WorkDir: root}
	client := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	reg := tools.NewRegistry(root, 3000)
	ag := agent.New(client, reg, "system", 5)
	t.Cleanup(ag.Stop)
	return ag, cfg
}

func textSSE(text string) string {
	b, _ := json.Marshal(text)
	return `{"choices":[{"delta":{"content":` + string(b) + `}}]}`
}

func toolCallSSE(id, name, args string) string {
	argsLiteral, _ := json.Marshal(args)
	return `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"` + id + `","function":{"name":"` + name + `","arguments":` + string(argsLiteral) + `}}]}}]}`
}

// pump 执行 waitForAgentEvent 命令并把事件喂给模型，直到遇到终止条件或超时。
func pump(t *testing.T, m Model, until func(View string) bool, timeout time.Duration) (Model, string) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		view := m.View()
		if until(view) {
			return m, view
		}
		cmd := waitForAgentEvent(m.ag)
		var msg tea.Msg
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		select {
		case msg = <-done:
		case <-deadline:
			t.Fatalf("泵事件超时，当前视图:\n%s", view)
		}
		var cm tea.Model
		cm, _ = m.Update(msg)
		m = cm.(Model)
	}
}

func TestUIFullFlow(t *testing.T) {
	ag, cfg := newScriptedAgent(t, [][]string{
		{textSSE("我来创建文件。"), toolCallSSE("c1", "write_file", `{"path":"demo.txt","content":"hello"}`)},
		{textSSE("完成。")},
	})

	m := New(ag, cfg)
	var cm tea.Model = m
	cm, _ = cm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = cm.(Model)

	if v := m.View(); !strings.Contains(v, "gliss") || !strings.Contains(v, "test-model") {
		t.Fatalf("头部渲染异常:\n%s", v)
	}
	if !strings.Contains(m.View(), "就绪") {
		t.Fatalf("初始应为就绪状态:\n%s", m.View())
	}

	// 空输入提交应无效果
	m.input.SetValue("")
	cm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = cm.(Model)
	if len(m.blocks) != 0 {
		t.Fatalf("空输入不应产生块: %+v", m.blocks)
	}

	// 提交任务
	m.input.SetValue("创建 demo.txt")
	cm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = cm.(Model)
	if len(m.blocks) != 1 || m.blocks[0].kind != blockUser {
		t.Fatalf("应产生用户块: %+v", m.blocks)
	}
	if !m.busy {
		t.Fatal("提交后应为忙碌状态")
	}

	// 泵事件直到回合结束
	m, view := pump(t, m, func(v string) bool { return strings.Contains(v, "回合结束") }, 5*time.Second)

	for _, want := range []string{"❯ 创建 demo.txt", "✓ write_file", "demo.txt", "完成。", "回合结束"} {
		if !strings.Contains(view, want) {
			t.Errorf("视图缺少 %q:\n%s", want, view)
		}
	}
	if m.busy {
		t.Error("回合结束后应为空闲")
	}
	if v := m.View(); !strings.Contains(v, "就绪") {
		t.Errorf("结束后状态应为就绪:\n%s", v)
	}

	// ctrl+t 展开工具详情
	cm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = cm.(Model)
	if v := m.View(); !strings.Contains(v, "参数:") || !strings.Contains(v, "结果:") {
		t.Errorf("展开后应显示参数与结果:\n%s", v)
	}

	// esc 清空输入
	m.input.SetValue("draft")
	cm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = cm.(Model)
	if m.input.Value() != "" {
		t.Error("esc 应清空空闲状态的输入")
	}
}

func TestUIInterruptAndError(t *testing.T) {
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer fail.Close()

	root := t.TempDir()
	cfg := &config.Config{BaseURL: fail.URL, APIKey: "k", Model: "m", WorkDir: root}
	client := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	ag := agent.New(client, tools.NewRegistry(root, 3000), "system", 5)
	defer ag.Stop()

	m := New(ag, cfg)
	var cm tea.Model = m
	cm, _ = cm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = cm.(Model)

	m.input.SetValue("hi")
	cm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = cm.(Model)

	m, view := pump(t, m, func(v string) bool { return strings.Contains(v, "✗") }, 5*time.Second)
	if !strings.Contains(view, "boom") {
		t.Errorf("应显示错误信息:\n%s", view)
	}
	if m.busy {
		t.Error("出错后应为空闲")
	}
}

func TestUICommands(t *testing.T) {
	ag, cfg := newScriptedAgent(t, [][]string{{textSSE("ok")}})

	m := New(ag, cfg)
	var cm tea.Model = m
	cm, _ = cm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = cm.(Model)

	typeResp := func(m Model, text string) Model {
		m.input.SetValue(text)
		cm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return cm.(Model)
	}

	// /help
	m = typeResp(m, "/help")
	if v := m.View(); !strings.Contains(v, "可用命令") || !strings.Contains(v, "/clear") {
		t.Errorf("/help 应显示帮助:\n%s", v)
	}

	// 未知的 / 开头输入不算命令，应作为普通文本发送（产生 user 块并忙碌）
	m = typeResp(m, "/etc/hosts 是什么")
	if len(m.blocks) != 2 || m.blocks[1].kind != blockUser || !m.busy {
		t.Fatalf("未知 / 输入应作为普通消息发送: %+v", m.blocks)
	}
	m, _ = pump(t, m, func(v string) bool { return strings.Contains(v, "回合结束") }, 5*time.Second)

	// /model 查看与切换
	m = typeResp(m, "/model")
	if v := m.View(); !strings.Contains(v, "当前模型：test-model") {
		t.Errorf("/model 应显示当前模型:\n%s", v)
	}
	m = typeResp(m, "/model other-model")
	if v := m.View(); !strings.Contains(v, "other-model") {
		t.Errorf("切换后头部应显示新模型:\n%s", v)
	}
	if ag.CurrentModel() != "other-model" {
		t.Errorf("agent 模型未切换: %s", ag.CurrentModel())
	}

	// /tools
	m = typeResp(m, "/tools")
	if v := m.View(); !strings.Contains(v, "read_file") || !strings.Contains(v, "run_bash") {
		t.Errorf("/tools 应列出工具:\n%s", v)
	}

	// /usage
	m = typeResp(m, "/usage")
	if v := m.View(); !strings.Contains(v, "会话累计") {
		t.Errorf("/usage 应显示用量:\n%s", v)
	}

	// /clear：清空历史块与 agent 消息
	m = typeResp(m, "/clear")
	if len(m.blocks) != 1 {
		t.Errorf("/clear 后应只剩提示块: %d", len(m.blocks))
	}
	if v := m.View(); !strings.Contains(v, "会话已清空") {
		t.Errorf("/clear 应显示提示:\n%s", v)
	}

	// /exit 返回退出命令
	m = typeResp(m, "/exit")
	if !m.quitting {
		t.Error("/exit 应进入退出状态")
	}
}

func TestMatchCommand(t *testing.T) {
	if c, a, ok := matchCommand("/model glm-4.7"); !ok || c != "model" || a != "glm-4.7" {
		t.Errorf("got %q %q %v", c, a, ok)
	}
	if c, _, ok := matchCommand("/CLEAR"); !ok || c != "clear" {
		t.Errorf("命令应大小写不敏感: %q %v", c, ok)
	}
	if _, _, ok := matchCommand("/etc/hosts 是什么"); ok {
		t.Error("未知 / 开头文本不应识别为命令")
	}
	if _, _, ok := matchCommand("普通消息"); ok {
		t.Error("普通消息不应识别为命令")
	}
	if _, _, ok := matchCommand("/model"); !ok {
		t.Error("无参数命令也应识别")
	}
}
