package keypool

import (
	"sync"
	"sync/atomic"
	"time"
)

type UpstreamKey struct {
	Key       string
	weight    int
	coolUntil atomic.Int64
	failCnt   atomic.Int64
}
type Pool struct {
	mu      sync.RWMutex
	keys    []*UpstreamKey
	counter atomic.Uint64
}

func NewPool(keys []string) *Pool {
	p := &Pool{}
	for _, key := range keys {
		p.keys = append(p.keys, &UpstreamKey{Key: key, weight: 1})
	}
	return p
}
func (p *Pool) Next() (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := len(p.keys)
	if n == 0 {
		return "", false
	}
	now := time.Now().UnixNano()
	for range n {
		idx := int(p.counter.Add(1)) % n
		k := p.keys[idx]
		if k.coolUntil.Load() <= now {
			return k.Key, true
		}
	}
	return "", false
}
func (p *Pool) MarkRateLimited(key string, coolDown time.Duration) {
	const defaultCoolDown = 30 * time.Second
	if coolDown <= 0 {
		coolDown = defaultCoolDown
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, k := range p.keys {
		if k.Key == key {
			k.coolUntil.Store(time.Now().Add(coolDown).UnixNano())
			k.failCnt.Add(1)
		}
	}
}
func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.keys)
}
