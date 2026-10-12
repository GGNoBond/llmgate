package ratelimit

import (
	"sync"
	"time"
)

type Limiter interface {
	Allow(key string, capacity int, perSecond float64, n int) (bool, int)
	Deduct(key string, capacity int, perSecond float64, n int)
}

type bucket struct {
	tokens   float64
	capacity float64
	rate     float64
	last     time.Time
}

func (b *bucket) allow(now time.Time, n float64, force bool) (bool, float64) {
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
	}
	b.last = now
	if force || b.tokens >= n {
		b.tokens -= n
		return true, b.tokens
	}
	return false, b.tokens
}

type MemBucketLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

func NewMemBucketLimiter() *MemBucketLimiter {
	return &MemBucketLimiter{buckets: make(map[string]*bucket)}
}

func (m *MemBucketLimiter) Allow(key string, capacity int, perSecond float64, n int) (bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{
			tokens:   float64(capacity),
			capacity: float64(capacity),
			rate:     perSecond,
			last:     now,
		}
		m.buckets[key] = b
	}
	allowed, remaining := b.allow(now, float64(n), false)
	return allowed, int(remaining)
}

func (m *MemBucketLimiter) Deduct(key string, capacity int, perSecond float64, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{
			tokens:   float64(capacity),
			capacity: float64(capacity),
			rate:     perSecond,
			last:     now,
		}
		m.buckets[key] = b
	}
	b.allow(now, float64(n), true)
}
