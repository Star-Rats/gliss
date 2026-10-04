package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newSSEServer 启动一个返回逐条 SSE 事件的测试服务器。
func newSSEServer(t *testing.T, events []string, usage string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, e := range events {
			_, _ = w.Write([]byte("data: " + e + "\n\n"))
			flusher.Flush()
		}
		if usage != "" {
			_, _ = w.Write([]byte("data: " + usage + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
}

func collectTokens(t *testing.T, events <-chan StreamEvent) (string, error) {
	t.Helper()
	var sb strings.Builder
	for ev := range events {
		if ev.Err != nil {
			return sb.String(), ev.Err
		}
		sb.WriteString(ev.Token)
	}
	return sb.String(), nil
}

func TestChatStream_TextDeltas(t *testing.T) {
	srv := newSSEServer(t, []string{
		`{"choices":[{"delta":{"content":"你好"}}]}`,
		`{"choices":[{"delta":{"content":"，世界"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}, `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)

	c := NewClient(srv.URL, "test-key", "test-model")
	reply, events := c.ChatStream(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)

	tokens, err := collectTokens(t, events)
	if err != nil {
		t.Fatalf("流出错: %v", err)
	}
	if tokens != "你好，世界" {
		t.Errorf("token 流 = %q, 期望 %q", tokens, "你好，世界")
	}
	if reply.Content != "你好，世界" {
		t.Errorf("reply.Content = %q", reply.Content)
	}
	if reply.Usage.PromptTokens != 10 || reply.Usage.CompletionTokens != 5 {
		t.Errorf("usage = %+v", reply.Usage)
	}
}

func TestChatStream_ToolCallFragments(t *testing.T) {
	// 模拟 tool_calls 分片：id 只在首片出现，name 与 arguments 分多片累积。
	srv := newSSEServer(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"_file","arguments":"{\"pa"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"main.go\"}"}}]}}]}`,
		`{"choices":[{"delta":{"content":"我来读取文件。"}}]}`,
	}, "")

	c := NewClient(srv.URL, "test-key", "test-model")
	reply, events := c.ChatStream(context.Background(), []Message{{Role: RoleUser, Content: "读 main.go"}}, nil)
	if _, err := collectTokens(t, events); err != nil {
		t.Fatalf("流出错: %v", err)
	}

	if reply.Content != "我来读取文件。" {
		t.Errorf("Content = %q", reply.Content)
	}
	if len(reply.ToolCalls) != 1 {
		t.Fatalf("toolCalls 数量 = %d, 期望 1", len(reply.ToolCalls))
	}
	tc := reply.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "read_file" {
		t.Errorf("toolCall = %+v", tc)
	}
	if tc.Function.Arguments != `{"path":"main.go"}` {
		t.Errorf("arguments = %q", tc.Function.Arguments)
	}
}

func TestChatStream_ParallelToolCalls(t *testing.T) {
	srv := newSSEServer(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"glob","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"grep","arguments":"{}"}}]}}]}`,
	}, "")

	c := NewClient(srv.URL, "test-key", "test-model")
	reply, events := c.ChatStream(context.Background(), []Message{{Role: RoleUser, Content: "搜"}}, nil)
	if _, err := collectTokens(t, events); err != nil {
		t.Fatalf("流出错: %v", err)
	}
	if len(reply.ToolCalls) != 2 || reply.ToolCalls[0].ID != "a" || reply.ToolCalls[1].ID != "b" {
		t.Fatalf("toolCalls = %+v", reply.ToolCalls)
	}
}

func TestChatStream_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "test-model")
	_, events := c.ChatStream(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	var last StreamEvent
	for ev := range events {
		last = ev
	}
	if last.Err == nil || !strings.Contains(last.Err.Error(), "bad request") {
		t.Errorf("期望包含 API 错误信息, got %v", last.Err)
	}
}

func TestChatStream_Cancel(t *testing.T) {
	srv := newSSEServer(t, []string{`{"choices":[{"delta":{"content":"x"}}]}`}, "")
	c := NewClient(srv.URL, "test-key", "test-model")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, events := c.ChatStream(ctx, []Message{{Role: RoleUser, Content: "hi"}}, nil)
	var last StreamEvent
	for ev := range events {
		last = ev
	}
	if last.Err == nil {
		t.Error("取消后应返回错误")
	}
}

func TestScanSSE(t *testing.T) {
	input := "data: {\"a\":1}\n\nevent: ping\ndata: \n\ndata: [DONE]\n\n"
	var got []string
	err := scanSSE(strings.NewReader(input), func(data []byte) error {
		if string(data) == "[DONE]" {
			return errDone
		}
		got = append(got, string(data))
		return nil
	})
	if err != nil {
		t.Fatalf("scanSSE: %v", err)
	}
	if len(got) != 1 || got[0] != `{"a":1}` {
		t.Errorf("got = %v", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	s := "你好世界"
	got := truncateRunes(s, 2)
	if got != "你好..." {
		t.Errorf("got = %q", got)
	}
	if truncateRunes(s, 10) != s {
		t.Errorf("短字符串不应被截断")
	}
}

func TestStreamDoesNotBlock(t *testing.T) {
	// 事件通道容量有限，确保消费端不读也不会死锁（收尾靠 close 前的 Done 事件）。
	srv := newSSEServer(t, []string{`{"choices":[{"delta":{"content":"a"}}]}`}, "")
	c := NewClient(srv.URL, "test-key", "test-model")
	reply, events := c.ChatStream(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	select {
	case _, ok := <-events:
		if !ok {
			t.Fatal("事件通道提前关闭")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待首个事件超时")
	}
	_ = reply
}
