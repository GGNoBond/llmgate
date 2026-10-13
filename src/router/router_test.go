package router

import (
	"context"
	"testing"
	"time"

	"github.com/aoxin/llmgate/src/lb"
	"github.com/aoxin/llmgate/src/openai"
	"github.com/aoxin/llmgate/src/provider"
)

// fakeProvider 只满足接口，不发真实请求
type fakeProvider struct{ name string }

func (f *fakeProvider) Name() string    { return f.name }
func (f *fakeProvider) BaseURL() string { return "" }
func (f *fakeProvider) Chat(ctx context.Context, req *openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error) {
	return &openai.ChatCompletionResponse{}, nil
}
func (f *fakeProvider) ChatStream(ctx context.Context, req *openai.ChatCompletionRequest, h provider.StreamHandler) error {
	return nil
}

func newNode(id string) *Node {
	return &Node{
		Backend:  lb.NewBackend(id, 1, &lb.Health{}),
		Provider: &fakeProvider{name: id},
		CB:       lb.NewCircuitBreaker(3, 10*time.Second, time.Second),
	}
}

func TestPick_Exclude(t *testing.T) {
	r := newRouter(&lb.WeightedRR{})
	a, b := newNode("a"), newNode("b")
	r.Register([]string{"m"}, a)
	r.Register([]string{"m"}, b)

	n, err := r.Pick("m", map[string]bool{"a": true}) // 排除 a
	if err != nil || n.Backend.ID != "b" {
		t.Errorf("排除 a 后应选 b, got %v err=%v", n, err)
	}
}

func TestPick_SkipUnhealthy(t *testing.T) {
	r := newRouter(&lb.WeightedRR{})
	a, b := newNode("a"), newNode("b")
	a.Backend.SetHealthy(false) // 摘掉 a
	r.Register([]string{"m"}, a)
	r.Register([]string{"m"}, b)

	n, err := r.Pick("m", nil)
	if err != nil || n.Backend.ID != "b" {
		t.Errorf("a 不健康应选 b, got %v err=%v", n, err)
	}
}

func TestPick_AllUnhealthy(t *testing.T) {
	r := newRouter(&lb.WeightedRR{})
	a := newNode("a")
	a.Backend.SetHealthy(false)
	r.Register([]string{"m"}, a)
	if _, err := r.Pick("m", nil); err == nil {
		t.Error("全不健康应返回 error")
	}
}

func TestPick_UnknownModel(t *testing.T) {
	r := newRouter(&lb.WeightedRR{})
	if _, err := r.Pick("nope", nil); err == nil {
		t.Error("未知模型应返回 error")
	}
}
