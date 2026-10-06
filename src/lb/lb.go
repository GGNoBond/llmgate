package lb

import (
	"math"
	"sync/atomic"
	"time"
)

// Backend 维护一个上游的连接状态
type Backend struct {
	ID        string
	Weight    int
	HealthCfg *Health
	healthy   atomic.Bool
	inflight  atomic.Int64
	failCnt   atomic.Int64
	OKCnt     atomic.Int64
}
type Health struct {
	Path          string
	Interval      time.Duration
	Timeout       time.Duration
	FailThreshold int64
	OkThreshold   int64
}

func NewBackend(id string, weight int, health *Health) *Backend {
	b := &Backend{ID: id, Weight: weight, HealthCfg: health}
	b.healthy.Store(true)
	return b
}

func (b *Backend) Healthy() bool           { return b.healthy.Load() }
func (b *Backend) SetHealthy(healthy bool) { b.healthy.Store(healthy) }
func (b *Backend) Inflight() int64         { return b.inflight.Load() }
func (b *Backend) IncInflight()            { b.inflight.Add(1) }
func (b *Backend) DecInflight()            { b.inflight.Add(-1) }
func (b *Backend) FailCount() int64        { return b.failCnt.Load() }
func (b *Backend) RecordFail() bool        { return b.failCnt.Add(1) >= b.HealthCfg.FailThreshold }
func (b *Backend) ResetFail()              { b.failCnt.Store(0) }
func (b *Backend) OKCount() int64          { return b.OKCnt.Load() }
func (b *Backend) RecordOK() bool          { return b.OKCnt.Add(1) >= b.HealthCfg.OkThreshold }
func (b *Backend) ResetOK()                { b.OKCnt.Store(0) }

// Balancer 负载均衡器接口
type Balancer interface {
	Pick(backends []*Backend) *Backend
}

// RoundRobin 轮询
type RoundRobin struct {
	counter atomic.Uint64
}

func (b *RoundRobin) Pick(backends []*Backend) *Backend {
	if len(backends) == 0 {
		return nil
	}
	idx := b.counter.Add(1) % uint64(len(backends))
	return backends[idx]
}

// WeightedRR 加权轮询
type WeightedRR struct {
	counter atomic.Uint64
}

// Pick 按照权重展开到pool+轮询
func (b *WeightedRR) Pick(backends []*Backend) *Backend {
	var pool []*Backend
	for _, b := range backends {
		if !b.Healthy() {
			continue
		}
		weight := b.Weight
		if weight <= 0 {
			weight = 1
		}
		for range weight {
			pool = append(pool, b)
		}
	}
	if len(pool) == 0 {
		return nil
	}
	idx := b.counter.Add(1) % uint64(len(pool))
	return pool[idx]
}

// LeastConn 最少连接均衡器
type LeastConn struct{}

func (b *LeastConn) Pick(backends []*Backend) *Backend {
	var least *Backend
	minConn := int64(math.MaxInt64)
	for _, b := range backends {
		if !b.Healthy() {
			continue
		}
		if b.Inflight() < minConn {
			least = b
			minConn = b.Inflight()
		}
	}
	return least
}
