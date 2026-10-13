package ratelimit

import (
	"testing"
	"time"
)

func TestMemBucket_BurstThenReject(t *testing.T) {
	m := NewMemBucketLimiter()
	// 容量 3、速率 0：连发 3 次应放行，第 4 次应被拒（桶空且无回填）
	for i := range 3 {
		if ok, rem := m.Allow("k", 3, 0, 1); !ok {
			t.Fatalf("第%d次应放行, remaining=%d", i+1, rem)
		}
	}
	if ok, _ := m.Allow("k", 3, 0, 1); ok {
		t.Fatal("第4次应被拒")
	}
}

func TestMemBucket_Refill(t *testing.T) {
	m := NewMemBucketLimiter()
	if ok, _ := m.Allow("k", 2, 100, 2); !ok { // 一次耗尽, 速率100/s
		t.Fatal("初始应放行 2 个")
	}
	if ok, _ := m.Allow("k", 2, 100, 1); ok {
		t.Fatal("刚耗尽应立即被拒")
	}
	time.Sleep(30 * time.Millisecond) // 约回填 3 个(封顶 2)
	if ok, _ := m.Allow("k", 2, 100, 1); !ok {
		t.Fatal("30ms 后应已回填并放行")
	}
}

func TestMemBucket_CapNoOverAccumulate(t *testing.T) {
	m := NewMemBucketLimiter()
	m.Allow("k", 2, 100, 1) // 建立桶
	time.Sleep(50 * time.Millisecond)
	n := 0
	for range 5 {
		if ok, _ := m.Allow("k", 2, 100, 1); ok {
			n++
		}
	}
	if n > 2 {
		t.Fatalf("回填后不应超过容量 2, 实际放行 %d 次", n)
	}
}

func TestMemBucket_DeductIsForce(t *testing.T) {
	m := NewMemBucketLimiter()
	m.Allow("k", 1, 0, 1)  // 用掉容量, 桶=0
	m.Deduct("k", 1, 0, 2) // Deduct=force, 即使不足也扣(可致负), 用于事后补差
	if ok, _ := m.Allow("k", 1, 0, 1); ok {
		t.Fatal("补扣为负后, 普通请求应被拒")
	}
}
