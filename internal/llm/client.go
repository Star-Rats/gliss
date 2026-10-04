package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Client 是 OpenAI 兼容 Chat Completions 客户端。
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client

	mu    sync.Mutex
	model string
}

// NewClient 创建客户端。BaseURL 形如 https://api.openai.com/v1。
func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		model:   model,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// SetModel 切换模型名，对之后的调用生效。并发安全。
func (c *Client) SetModel(model string) {
	c.mu.Lock()
	c.model = model
	c.mu.Unlock()
}

// ModelName 返回当前模型名。
func (c *Client) ModelName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.model
}

// AssistantReply 是一次完整 LLM 调用的结果。
type AssistantReply struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
}

// ChatStream 发起流式对话请求。events 通道在流结束或出错后关闭。
// 返回的 reply 在流结束后才完整可用。
func (c *Client) ChatStream(ctx context.Context, messages []Message, tools []ToolDef) (*AssistantReply, <-chan StreamEvent) {
	events := make(chan StreamEvent, 64)
	reply := &AssistantReply{}

	go func() {
		defer close(events)

		reqBody, err := json.Marshal(chatRequest{
			Model:    c.ModelName(),
			Messages: messages,
			Tools:    tools,
			Stream:   true,
		})
		if err != nil {
			emitErr(ctx, events, fmt.Errorf("序列化请求失败: %w", err))
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
		if err != nil {
			emitErr(ctx, events, fmt.Errorf("构造请求失败: %w", err))
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			emitErr(ctx, events, mapRequestError(ctx, err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			emitErr(ctx, events, c.apiError(resp))
			return
		}

		acc := &streamAccumulator{}
		err = scanSSE(resp.Body, func(data []byte) error {
			if string(data) == "[DONE]" {
				return errDone
			}
			var chunk streamChunk
			if err := json.Unmarshal(data, &chunk); err != nil {
				return fmt.Errorf("解析流分片失败: %w", err)
			}
			acc.add(&chunk)
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					// 消费端可能已退出（如 TUI 关闭），此时靠 ctx 解除阻塞。
					if !sendEvent(ctx, events, StreamEvent{Token: choice.Delta.Content}) {
						return ctx.Err()
					}
				}
			}
			return nil
		})
		if err != nil && err != errDone {
			emitErr(ctx, events, err)
			return
		}

		reply.Content = acc.content
		reply.ToolCalls = acc.toolCalls
		reply.Usage = acc.usage
		sendEvent(ctx, events, StreamEvent{Done: true})
	}()

	return reply, events
}

// apiError 读取非 200 响应体并转成带状态码的错误。
func (c *Client) apiError(resp *http.Response) error {
	body, _ := readLimited(resp.Body, 4096)
	if len(body) == 0 {
		return fmt.Errorf("API 返回状态码 %d", resp.StatusCode)
	}
	var e struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Message != "" {
		if e.Error.Type != "" {
			return fmt.Errorf("API 错误(%d, %s): %s", resp.StatusCode, e.Error.Type, e.Error.Message)
		}
		return fmt.Errorf("API 错误(%d): %s", resp.StatusCode, e.Error.Message)
	}
	return fmt.Errorf("API 返回状态码 %d: %s", resp.StatusCode, truncateRunes(string(body), 512))
}

// sendEvent 优先把事件送入通道；仅当通道满且消费端停止读取时才靠 ctx 解除阻塞，
// 避免 ctx 已取消时 select 随机丢弃错误事件。
func sendEvent(ctx context.Context, events chan<- StreamEvent, ev StreamEvent) bool {
	select {
	case events <- ev:
		return true
	default:
	}
	select {
	case events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func emitErr(ctx context.Context, events chan<- StreamEvent, err error) {
	sendEvent(ctx, events, StreamEvent{Err: err, Done: true})
}

// mapRequestError 区分用户主动取消与真实网络错误。
func mapRequestError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("请求 API 失败: %w", err)
}
