package ratelimit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// 并发投递 N 条 -> Close 排空 -> 读回文件逐行校验：数量对、每行合法、无丢弃
func TestRecorder_AsyncWriteAndDrain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatalf("NewRecorder 出错: %v", err)
	}

	const (
		workers = 50
		per     = 20 // 共 1000 条，远小于 4096 缓冲，不应丢
	)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range per {
				r.Record(UsageRecord{
					TenantID: "t1", Model: "qwen", TotalTokens: 42, StatusCode: 200,
				})
			}
		})
	}
	wg.Wait()

	if err := r.Close(); err != nil {
		t.Fatalf("Close 出错: %v", err)
	}
	if d := r.Dropped(); d != 0 {
		t.Fatalf("缓冲充足却丢弃了 %d 条", d)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开落库文件失败: %v", err)
	}
	defer f.Close()

	lines := 0
	var sumTokens int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var u UsageRecord
		if err := json.Unmarshal(line, &u); err != nil {
			t.Fatalf("第%d行不是合法 JSON: %v (%s)", lines+1, err, line)
		}
		lines++
		sumTokens += u.TotalTokens
	}
	if lines != workers*per {
		t.Fatalf("应有 %d 条记录, 实际 %d", workers*per, lines)
	}
	if sumTokens != workers*per*42 {
		t.Fatalf("token 累加不符, got %d want %d", sumTokens, workers*per*42)
	}
}

// Close 必须幂等：重复调用不 panic、不重复关文件
func TestRecorder_CloseIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatalf("NewRecorder 出错: %v", err)
	}
	r.Record(UsageRecord{TenantID: "t1", TotalTokens: 1})
	if err := r.Close(); err != nil {
		t.Fatalf("第一次 Close 出错: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("第二次 Close 应幂等返回 nil, got %v", err)
	}
}

// nil recorder / 空路径兜底：Record 不应 panic
func TestRecorder_NilSafeRecord(t *testing.T) {
	var r *Recorder
	r.Record(UsageRecord{TenantID: "t"}) // 不应 panic
}
