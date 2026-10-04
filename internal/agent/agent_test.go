package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gliss/internal/llm"
	"gliss/internal/tools"
)

// scriptedServer 按顺序返回预设的 SSE 响应；把收到的 messages 记录下来供断言。
type scriptedServer struct {
	responses [][]string // 每个元素是一轮的 SSE data 列表
	mu        sync.Mutex
	calls     int
	lastBody  []chatRequestEcho
}

type chatRequestEcho struct {
	Messages []llm.Message `json:"messages"`
}

func (s *scriptedServer) snapshot() (int, []llm.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == 0 || len(s.lastBody) == 0 {
		return s.calls, nil
	}
	return s.calls, s.lastBody[len(s.lastBody)-1].Messages
}

func (s *scriptedServer) allBodies() []chatRequestEcho {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]chatRequestEcho(nil), s.lastBody...)
}

func (s *scriptedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var echo chatRequestEcho
	_ = json.Unmarshal(body, &echo)

	s.mu.Lock()
	callIdx := s.calls
	s.lastBody = append(s.lastBody, echo)
	s.calls++
	resp := s.responses[min(callIdx, len(s.responses)-1)]
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	flusher := w.(http.Flusher)
	for _, e := range resp {
		_, _ = w.Write([]byte("data: " + e + "\n\n"))
		flusher.Flush()
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

func toolCallSSE(id, name, args string) string {
	// API 规范中 arguments 是「JSON 字符串」而非内嵌对象，需再编码一层。
	argsLiteral, _ := json.Marshal(args)
	return `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"` + id + `","function":{"name":"` + name + `","arguments":` + string(argsLiteral) + `}}]}}]}`
}

func textSSE(text string) string {
	b, _ := json.Marshal(text)
	return `{"choices":[{"delta":{"content":` + string(b) + `}}]}`
}

// collectEvents 持续收集事件直到 kind 出现或超时。
func collectEvents(t *testing.T, events <-chan Event, until EventKind, timeout time.Duration) []Event {
	t.Helper()
	deadline := time.After(timeout)
	var got []Event
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return got
			}
			got = append(got, ev)
			if ev.Kind == until {
				return got
			}
		case <-deadline:
			t.Fatalf("等待事件 %d 超时, 已收到: %+v", until, summarize(got))
		}
	}
}

func summarize(evs []Event) []string {
	var out []string
	for _, e := range evs {
		msg := e.Kind.String() + "(" + e.Text + "/" + e.ToolName + ")"
		if e.Err != nil {
			msg += "[" + e.Err.Error() + "]"
		}
		out = append(out, msg)
	}
	return out
}

func (k EventKind) String() string {
	switch k {
	case EventToken:
		return "Token"
	case EventTurnStart:
		return "TurnStart"
	case EventToolStart:
		return "ToolStart"
	case EventToolEnd:
		return "ToolEnd"
	case EventTurnEnd:
		return "TurnEnd"
	case EventInterrupted:
		return "Interrupted"
	case EventError:
		return "Error"
	case EventIdle:
		return "Idle"
	}
	return "?"
}

func newTestAgent(t *testing.T, url, root string) *Agent {
	t.Helper()
	client := llm.NewClient(url, "k", "m")
	reg := tools.NewRegistry(root, 3000)
	return New(client, reg, "system", 5)
}

func TestAgentToolLoop(t *testing.T) {
	root := t.TempDir()
	srv := &scriptedServer{
		responses: [][]string{
			// 第一轮：先说话再调工具
			{textSSE("我先写文件。"), toolCallSSE("c1", "write_file", `{"path":"hello.txt","content":"hi"}`)},
			// 第二轮：纯文本收尾（同一流内的两个分片）
			{textSSE("已完成，文件内容是 "), textSSE("hi")},
		},
	}
	server := httptest.NewServer(srv)
	defer server.Close()

	a := newTestAgent(t, server.URL, root)
	defer a.Stop()
	a.Submit("创建 hello.txt")

	evs := collectEvents(t, a.Events(), EventTurnEnd, 5*time.Second)

	// 事件序列断言
	var tokens strings.Builder
	var toolNames []string
	sawToolStart, sawToolEnd, sawTurnStart := false, false, false
	for _, ev := range evs {
		switch ev.Kind {
		case EventToken:
			tokens.WriteString(ev.Text)
		case EventTurnStart:
			sawTurnStart = true
		case EventToolStart:
			sawToolStart = true
		case EventToolEnd:
			sawToolEnd = true
			toolNames = append(toolNames, ev.ToolName)
		}
	}
	if got := tokens.String(); got != "我先写文件。\n已完成，文件内容是 hi" {
		t.Errorf("token 流 = %q", got)
	}
	if !sawTurnStart || !sawToolStart || !sawToolEnd {
		t.Errorf("事件缺失: turnStart=%v toolStart=%v toolEnd=%v", sawTurnStart, sawToolStart, sawToolEnd)
	}
	if len(toolNames) != 1 || toolNames[0] != "write_file" {
		t.Errorf("工具调用 = %v", toolNames)
	}

	// 副作用断言
	if data, err := os.ReadFile(filepath.Join(root, "hello.txt")); err != nil || string(data) != "hi" {
		t.Errorf("文件未正确写入: %q, %v", data, err)
	}

	// 消息历史断言：第二轮请求应包含 tool 结果
	bodies := srv.allBodies()
	if len(bodies) != 2 {
		t.Fatalf("LLM 调用次数 = %d", len(bodies))
	}
	second := bodies[1].Messages
	var roles []string
	for _, m := range second {
		roles = append(roles, string(m.Role))
	}
	if strings.Join(roles, ",") != "system,user,assistant,tool" {
		t.Errorf("第二轮角色序列 = %v", roles)
	}
	if second[len(second)-1].ToolCallID != "c1" || !strings.Contains(second[len(second)-1].Content, "已写入") {
		t.Errorf("tool 回填消息异常: %+v", second[len(second)-1])
	}
}

func TestAgentInterrupt(t *testing.T) {
	root := t.TempDir()
	// 慢速服务器：流式输出但长时间不发完。
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: " + textSSE("开始") + "\n\n"))
		flusher.Flush()
		<-r.Context().Done() // 等客户端断开
	}))
	defer slow.Close()

	client := llm.NewClient(slow.URL, "k", "m")
	reg := tools.NewRegistry(root, 3000)
	a := New(client, reg, "system", 5)
	defer a.Stop()

	a.Submit("慢点")
	collectEvents(t, a.Events(), EventToken, 3*time.Second)
	a.Interrupt()
	evs := collectEvents(t, a.Events(), EventInterrupted, 3*time.Second)
	if len(evs) == 0 {
		t.Fatal("应收到中断事件")
	}
}

func TestAgentAPIError(t *testing.T) {
	root := t.TempDir()
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key","type":"auth"}}`))
	}))
	defer fail.Close()

	client := llm.NewClient(fail.URL, "k", "m")
	reg := tools.NewRegistry(root, 3000)
	a := New(client, reg, "system", 5)
	defer a.Stop()

	a.Submit("hi")
	evs := collectEvents(t, a.Events(), EventError, 3*time.Second)
	if evs[len(evs)-1].Err == nil || !strings.Contains(evs[len(evs)-1].Err.Error(), "invalid key") {
		t.Errorf("应收到 API 错误: %+v", evs)
	}
}

func TestSystemPrompt(t *testing.T) {
	p := SystemPrompt("/tmp/project")
	for _, want := range []string{"coding agent", "edit_file", "/tmp/project"} {
		if !strings.Contains(p, want) {
			t.Errorf("系统提示词缺少 %q", want)
		}
	}
}

func TestAgentResetAndTools(t *testing.T) {
	root := t.TempDir()
	srv := &scriptedServer{responses: [][]string{{textSSE("ok")}}}
	server := httptest.NewServer(srv)
	defer server.Close()

	a := newTestAgent(t, server.URL, root)
	defer a.Stop()
	a.Submit("hi")
	collectEvents(t, a.Events(), EventTurnEnd, 3*time.Second)

	if u := a.TotalUsage(); u.LLMCalls != 1 {
		t.Errorf("回合后累计 LLM 调用 = %d", u.LLMCalls)
	}
	a.Reset()
	if len(a.messages) != 1 || a.messages[0].Role != "system" {
		t.Errorf("Reset 后应只剩 system 消息: %d 条", len(a.messages))
	}
	if u := a.TotalUsage(); u.LLMCalls != 0 || u.PromptTokens != 0 {
		t.Errorf("Reset 后用量应归零: %+v", u)
	}

	summaries := a.ToolSummaries()
	if len(summaries) != 7 || !strings.Contains(summaries[0], "read_file") {
		t.Errorf("ToolSummaries 异常: %v", summaries)
	}

	a.SetModel("another")
	if a.CurrentModel() != "another" {
		t.Errorf("SetModel 未生效: %s", a.CurrentModel())
	}
}

func TestAgentStopClosesEvents(t *testing.T) {
	root := t.TempDir()
	srv := &scriptedServer{responses: [][]string{{textSSE("ok")}}}
	server := httptest.NewServer(srv)
	defer server.Close()

	a := newTestAgent(t, server.URL, root)
	a.Submit("hi")
	collectEvents(t, a.Events(), EventIdle, 3*time.Second)
	a.Stop()
	select {
	case _, ok := <-a.Events():
		if ok {
			t.Error("Stop 后事件通道不应再产出事件")
		}
	case <-time.After(2 * time.Second):
		t.Error("Stop 后事件通道应被关闭")
	}
	select {
	case <-a.Done():
	case <-time.After(2 * time.Second):
		t.Error("会话 goroutine 应退出")
	}
}
