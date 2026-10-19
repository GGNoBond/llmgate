// benchmock 是压测专用的假 OpenAI 上游：不跑模型，CPU 即可，
// 用于剥离真实推理延迟、单独测量网关自身开销，并可控制 5xx 比例来触发故障转移/熔断。
// 严格遵循 OpenAI /v1/chat/completions 与 SSE 规范（含 stream_options.include_usage）。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

type chatReq struct {
	Model         string `json:"model"`
	Stream        bool   `json:"stream"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

type cfg struct {
	addr             string
	model            string
	chunks           int
	firstDelay       time.Duration
	chunkInterval    time.Duration
	promptTokens     int
	completionTokens int
	think            time.Duration
	errorRate        float64
}

var C cfg

var allWords = []string{
	"mock", "upstream", "for", "gateway", "load", "test",
	"emitting", "deterministic", "sse", "chunks", "so", "the",
	"model", "is", "never", "the", "bottleneck", "and",
	"we", "can", "measure", "pure", "gateway", "overhead",
}

// contentWords 按 -chunks 截断，让流式块数与 token 数可预测
func contentWords() []string {
	n := C.chunks
	if n <= 0 {
		n = 1
	}
	if n > len(allWords) {
		n = len(allWords)
	}
	return allWords[:n]
}

func usage(c chatReq) (int, int) {
	pt := C.promptTokens
	ct := C.completionTokens
	if ct == 0 {
		ct = len(contentWords())
	}
	return pt, ct
}

func models(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"object":"list","data":[{"id":%q,"object":"model","created":%d,"owned_by":"benchmock"}]}`, C.model, time.Now().Unix())
}

func completions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var c chatReq
	_ = json.NewDecoder(r.Body).Decode(&c)

	// 可控故障：按 error-rate 概率返回 500，用来测故障转移 / 熔断 / 健康剔除
	if C.errorRate > 0 && rand.Float64() < C.errorRate {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"benchmock injected failure","type":"server_error"}}`)
		return
	}

	model := c.Model
	if model == "" {
		model = C.model
	}
	id := fmt.Sprintf("chatcmpl-benchmock-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	pt, ct := usage(c)
	words := contentWords()

	if !c.Stream {
		// 非流式：可配 think 人工延迟（默认 0 = 立即），带完整 usage
		if C.think > 0 {
			time.Sleep(C.think)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":%q,"object":"chat.completion","created":%d,"model":%q,`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}}`,
			id, created, model, strings.Join(words, " "), pt, ct, pt+ct)
		return
	}

	// 流式：SSE，firstDelay 控制 TTFT，chunkInterval 控制吐字间隔
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	time.Sleep(C.firstDelay) // TTFT
	for i, word := range words {
		var delta string
		if i == 0 {
			delta = fmt.Sprintf(`{"role":"assistant","content":%q}`, word)
		} else {
			delta = fmt.Sprintf(`{"content":%q}`, word)
		}
		fmt.Fprintf(w, "data: {\"id\":%q,\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]}\n\n",
			id, created, model, delta)
		flusher.Flush()
		time.Sleep(C.chunkInterval)
	}
	// 结束块：finish_reason=stop
	fmt.Fprintf(w, "data: {\"id\":%q,\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
		id, created, model)
	flusher.Flush()
	// 上游若请求 include_usage，则最后再补一个带 usage 的空 choices 块（OpenAI 规范）
	if c.StreamOptions.IncludeUsage {
		fmt.Fprintf(w, "data: {\"id\":%q,\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":%q,\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n",
			id, created, model, pt, ct, pt+ct)
		flusher.Flush()
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func main() {
	flag.StringVar(&C.addr, "addr", ":11499", "listen address")
	flag.StringVar(&C.model, "model", "mock-gpt", "default model name")
	flag.IntVar(&C.chunks, "chunks", 20, "number of stream content chunks")
	flag.DurationVar(&C.firstDelay, "first-delay", 5*time.Millisecond, "delay before first stream chunk (TTFT)")
	flag.DurationVar(&C.chunkInterval, "chunk-interval", 5*time.Millisecond, "gap between stream chunks")
	flag.IntVar(&C.promptTokens, "prompt-tokens", 24, "fixed prompt_tokens in usage")
	flag.IntVar(&C.completionTokens, "completion-tokens", 0, "fixed completion_tokens (0 = use chunk count)")
	flag.DurationVar(&C.think, "think", 0, "non-stream artificial latency")
	flag.Float64Var(&C.errorRate, "error-rate", 0, "probability [0..1) of returning 500")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", completions)
	mux.HandleFunc("/chat/completions", completions)
	mux.HandleFunc("/v1/models", models)
	mux.HandleFunc("/models", models)

	fmt.Printf("benchmock listening on %s model=%s chunks=%d first-delay=%s chunk-interval=%s error-rate=%.2f\n",
		C.addr, C.model, C.chunks, C.firstDelay, C.chunkInterval, C.errorRate)
	if err := http.ListenAndServe(C.addr, mux); err != nil {
		panic(err)
	}
}
