package router

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/lb"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/provider"
)

type Node struct {
	Backend  *lb.Backend
	Provider provider.Provider
	CB       *lb.CircuitBreaker
}

type Router struct {
	mu       sync.RWMutex
	table    map[string][]*Node // model->实例组
	balancer lb.Balancer
}

func newRouter(balancer lb.Balancer) *Router {
	return &Router{
		table:    make(map[string][]*Node),
		balancer: balancer,
	}
}
func BuildRouter(upstreams []config.UpstreamConfig, tr *http.Transport, b lb.Balancer) (*Router, []*Node) {
	r := newRouter(b)
	var allNodes []*Node
	for _, up := range upstreams {
		var p provider.Provider
		switch up.Type {
		case "ollama":
			p = provider.NewOllamaProvider(&up, tr)
		default:
			p = provider.NewOpenAIProvider(&up, tr)
		}
		weight := up.Weight
		health := &lb.Health{
			Path:          up.Health.Path,
			Interval:      up.Health.Interval,
			Timeout:       up.Health.Timeout,
			FailThreshold: up.Health.FailThreshold,
			OkThreshold:   up.Health.OkThreshold,
		}
		node := &Node{
			Backend:  lb.NewBackend(up.Name, weight, health),
			Provider: p,
			CB:       lb.NewCircuitBreaker(3, 10*time.Second, 1*time.Second), // 熔断器参数设置了固定值
		}
		allNodes = append(allNodes, node)
		r.Register(up.Models, node)
	}
	return r, allNodes
}
func (r *Router) Register(models []string, node *Node) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, model := range models {
		r.table[model] = append(r.table[model], node)
	}
}

func (r *Router) Nodes(model string) []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.table[model]
}

func (r *Router) Pick(model string) (*Node, error) {
	nodes := r.Nodes(model)
	if len(nodes) == 0 {
		return nil, errors.New("no upstream for model: " + model)
	}
	var candidates []*lb.Backend
	backendToNode := make(map[string]*Node)
	for _, node := range nodes {
		if node.Backend.Healthy() && node.CB.Allow() {
			candidates = append(candidates, node.Backend)
			backendToNode[node.Backend.ID] = node
		}
	}
	b := r.balancer.Pick(candidates)
	if b == nil {
		return nil, errors.New("all upstreams are unhealthy or circuit-open for model: " + model)
	}
	logger.Get().Trace().Msgf("picked %s for model %s", b.ID, model)
	return backendToNode[b.ID], nil
}
