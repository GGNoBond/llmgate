// learningtest/mock429/mock429server.go
// 模拟 OpenAI 兼容上游：对指定 api key 返回 429 + Retry-After，验证 llmgate 限流/熔断/换 key 链路
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	limited := strings.Split(os.Getenv("LIMITED_KEYS"), ",")
	retryAfter := os.Getenv("RETRY_AFTER")
	if retryAfter == "" {
		retryAfter = "20"
	}
	isLimited := func(key string) bool {
		for _, k := range limited {
			k = strings.TrimSpace(k)
			if k == "*" || (k != "" && strings.Contains(key, k)) {
				return true
			}
		}
		return false
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if isLimited(key) {
			fmt.Printf("[429 ] key=%s stream=%v\n", key, req.Stream)
			w.Header().Set("Retry-After", retryAfter)
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"message": "Rate limit reached for this api key", "type": "rate_limit_error"},
			})
			return
		}

		fmt.Printf("[200 ] key=%s stream=%v\n", key, req.Stream)
		if req.Stream {
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, c := range []string{"你", "好", "，", "我", "是", "mock"} {
				data, _ := json.Marshal(map[string]any{
					"id": "chatcmpl-mock429", "object": "chat.completion.chunk",
					"created": time.Now().Unix(), "model": req.Model,
					"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": c}}},
				})
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
				time.Sleep(100 * time.Millisecond)
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-mock429", "object": "chat.completion",
			"created": time.Now().Unix(), "model": req.Model,
			"choices": []any{map[string]any{"index": 0,
				"message":       map[string]string{"role": "assistant", "content": "mock ok, this key is not rate limited"},
				"finish_reason": "stop"}},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12},
		})
	})
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	fmt.Printf("Mock 429 upstream on :11435 (LIMITED_KEYS=%q, RETRY_AFTER=%s)\n", os.Getenv("LIMITED_KEYS"), retryAfter)
	http.ListenAndServe(":11435", mux)
}
