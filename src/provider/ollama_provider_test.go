package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/openai"
)

func newTestOllamaProvider(baseURL string) *OllamaProvider {
	return NewOllamaProvider(&config.UpstreamConfig{
		Name:               "ollama-test",
		BaseURL:            baseURL,
		PerResponseTimeout: 2 * time.Second,
		StreamChunkTimeout: 2 * time.Second,
	}, http.DefaultTransport.(*http.Transport).Clone())
}

// 非流式：Ollama 原生 JSON -> OpenAI chat.completion 的字段映射
func TestOllamaChat_ProtocolConversion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("期望路径 /api/chat, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"qwen2.5:1.5b","created_at":"2026-01-01T00:00:00Z","message":{"role":"assistant","content":"Hi there"},"done":true,"done_reason":"stop","prompt_eval_count":12,"eval_count":7}`))
	}))
	defer srv.Close()

	p := newTestOllamaProvider(srv.URL)
	resp, err := p.Chat(context.Background(), &openai.ChatCompletionRequest{
		Model:    "qwen2.5:1.5b",
		Messages: []openai.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat 出错: %v", err)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object 应为 chat.completion, got %s", resp.Object)
	}
	if resp.Model != "qwen2.5:1.5b" {
		t.Errorf("Model 映射错误: %s", resp.Model)
	}
	if resp.Choices[0].Message.Content != "Hi there" {
		t.Errorf("message.content 未映射到 choices[0].message: %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("done_reason 未映射到 finish_reason: %q", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 12 {
		t.Errorf("PromptTokens 应=12(prompt_eval_count), got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 7 {
		t.Errorf("CompletionTokens 应=7(eval_count), got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 19 {
		t.Errorf("TotalTokens 应=19, got %d", resp.Usage.TotalTokens)
	}
}

// 流式：Ollama NDJSON -> OpenAI SSE chunk（首块带 role，末块带 finish_reason+usage）
func TestOllamaChatStream_NDJSONToSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		lines := []string{
			`{"model":"qwen","message":{"role":"assistant","content":"He"},"done":false}`,
			`{"model":"qwen","message":{"role":"","content":"llo"},"done":false}`,
			`{"model":"qwen","message":{"role":"","content":""},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":5}`,
		}
		for _, l := range lines {
			_, _ = w.Write([]byte(l + "\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	p := newTestOllamaProvider(srv.URL)
	var got []openai.ChatCompletionChunk
	err := p.ChatStream(context.Background(),
		&openai.ChatCompletionRequest{Model: "qwen", Messages: []openai.ChatMessage{{Role: "user", Content: "hi"}}},
		func(c *openai.ChatCompletionChunk) error {
			got = append(got, *c)
			return nil
		})
	if err != nil {
		t.Fatalf("ChatStream 出错: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("应有 3 个 chunk, got %d", len(got))
	}
	if got[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("首块应带 role=assistant, got %q", got[0].Choices[0].Delta.Role)
	}
	var sb strings.Builder
	for _, c := range got {
		sb.WriteString(c.Choices[0].Delta.Content)
	}
	if sb.String() != "Hello" {
		t.Errorf("流式正文拼接应=Hello, got %q", sb.String())
	}
	last := got[len(got)-1]
	if last.Choices[0].FinishReason == nil || *last.Choices[0].FinishReason != "stop" {
		t.Errorf("末块 finish_reason 应为 stop")
	}
	if last.Usage == nil || last.Usage.PromptTokens != 3 || last.Usage.CompletionTokens != 5 || last.Usage.TotalTokens != 8 {
		t.Errorf("末块 usage 映射错误: %+v", last.Usage)
	}
}
