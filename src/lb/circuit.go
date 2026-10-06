package lb

import (
	"sync/atomic"
	"time"

	"github.com/aoxin/llmgate/src/logger"
)

const (
	StateClosed int32 = 0 // 关闭
	StateOpen   int32 = 1 // 打开
)

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	state         atomic.Int32
	failures      atomic.Int32  // 失败次数累计
	openedAt      atomic.Int64  // 上次熔断时间
	lastProbe     atomic.Int64  // 上次探测时间
	threshold     int32         // 关闭状态失败次数阈值
	cooldown      time.Duration // 熔断冷却
	probeInterval time.Duration // 探测间隔
}

func NewCircuitBreaker(threshold int, cooldown time.Duration, probeInterval time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:     int32(threshold),
		cooldown:      cooldown,
		probeInterval: probeInterval,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	if cb.state.Load() == StateClosed {
		return true
	}
	now := time.Now().UnixNano()
	if time.Duration(now-cb.openedAt.Load()) < cb.cooldown { // 熔断冷却期内
		return false
	}
	last := cb.lastProbe.Load()
	if time.Duration(now-last) < cb.probeInterval { // 探测间隔内
		return false
	}
	// 并发情况下只会返回一个true
	return cb.lastProbe.CompareAndSwap(last, now)
}

func (cb *CircuitBreaker) OnSuccess() {
	if cb.failures.Load() > 0 {
		logger.Get().Trace().Msgf("[SUCCESS] %d 成功，关闭熔断器", cb.state.Load())
	}
	cb.failures.Store(0)
	cb.state.Store(StateClosed)
}

func (cb *CircuitBreaker) OnFailure() {
	if cb.failures.Add(1) >= cb.threshold {
		logger.Get().Trace().Msgf("[OPEN] %d 失败，达到阈值 %d，打开熔断器", cb.failures.Load(), cb.threshold)
		cb.state.Store(StateOpen)
		cb.openedAt.Store(time.Now().UnixNano())
	}
}
