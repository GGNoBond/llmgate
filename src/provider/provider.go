package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/openai"
)

// Provider 上游服务提供者接口
type Provider interface {
	Name() string
	Chat(ctx context.Context, req *openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error)
	ChatStream(ctx context.Context, req *openai.ChatCompletionRequest, h StreamHandler) error
}

type StreamHandler func(chunk *openai.ChatCompletionChunk) error

// Router 	上游服务路由表
type Router struct {
	table map[string][]Provider
}

func NewRouter() *Router {
	return &Router{
		table: make(map[string][]Provider),
	}
}

// Register 注册上游服务提供者
func (r *Router) Register(p Provider, models []string) {
	for _, model := range models {
		r.table[model] = append(r.table[model], p)
	}
}

// Route 路由
func (r *Router) Route(model string) (Provider, error) {
	ps, ok := r.table[model]
	if !ok || len(ps) == 0 {
		return nil, fmt.Errorf("no provider for model:%s", model)
	}
	return ps[0], nil
}

func (r *Router) InitRouteTable(upstreams []config.UpstreamConfig, tr *http.Transport) {
	for _, upstream := range upstreams {
		switch upstream.Type {
		case "ollama":
			r.Register(NewOllamaProvider(&upstream, tr), upstream.Models)
		default:
			r.Register(NewOpenAIProvider(&upstream, tr), upstream.Models)
		}
	}
}
