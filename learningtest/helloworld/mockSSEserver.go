// learningtest/mockstream/mockSSEserver.go
// 模拟 Ollama /api/chat 流式接口（NDJSON 格式），用于测试上游 chunk 间隔超时
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ollamaMessage 对应 ollama 响应中的 message 字段
type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaStreamChunk 对应 ollama /api/chat 流式返回的单行 JSON
type ollamaStreamChunk struct {
	Model           string        `json:"model"`
	CreatedAt       time.Time     `json:"created_at"`
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	DoneReason      string        `json:"done_reason,omitempty"`
	PromptEvalCount *int          `json:"prompt_eval_count,omitempty"`
	EvalCount       *int          `json:"eval_count,omitempty"`
}

func writeChunk(w http.ResponseWriter, flusher http.Flusher, c ollamaStreamChunk) {
	data, _ := json.Marshal(c)
	// NDJSON：每行一个 JSON 对象，以 \n 分隔
	fmt.Fprintf(w, "%s\n", data)
	flusher.Flush()
}

func main() {
	http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		model := "mock-ollama"
		now := time.Now()

		// 逐字返回内容，前3个正常速度，第4个模拟上游卡死
		chunks := []string{"你", "好", "，", "世", "界", "！"}
		for i, c := range chunks {
			delay := 500 * time.Millisecond
			if i == 3 {
				delay = 15 * time.Second // 模拟上游卡死，触发 idle timeout
			}
			time.Sleep(delay)

			writeChunk(w, flusher, ollamaStreamChunk{
				Model:     model,
				CreatedAt: now,
				Message:   ollamaMessage{Role: "assistant", Content: c},
				Done:      false,
			})
		}

		// 最后一条：done=true，带 usage 统计
		promptEval, evalCount := 5, len(chunks)
		writeChunk(w, flusher, ollamaStreamChunk{
			Model:           model,
			CreatedAt:       now,
			Message:         ollamaMessage{Role: "assistant", Content: ""},
			Done:            true,
			DoneReason:      "stop",
			PromptEvalCount: &promptEval,
			EvalCount:       &evalCount,
		})
	})

	fmt.Println("Mock Ollama stream server on :9999")
	http.ListenAndServe(":9999", nil)
}

/* 模拟openai
// learningtest/mockstream/mockSSEserver.go
package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		chunks := []string{"你", "好", "，", "世", "界", "！"}
		for i, c := range chunks {
			// 前3个chunk正常速度，第4个开始模拟卡顿
			delay := 500 * time.Millisecond
			if i == 3 {
				delay = 15 * time.Second // 模拟上游卡死
			}
			time.Sleep(delay)

			fmt.Fprintf(w, `data: {"id":"chatcmpl-mock","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"%s"},"finish_reason":null}]}`+"\n\n", c)
			flusher.Flush()
		}
		fmt.Fprintf(w, `data: {"id":"chatcmpl-mock","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	fmt.Println("Mock stream server on :9999")
	http.ListenAndServe(":9999", nil)
}

*/
