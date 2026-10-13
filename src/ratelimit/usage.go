package ratelimit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/utils"
)

type UsageRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	TenantID     string    `json:"tenant_id"`
	Model        string    `json:"model"`
	Upstream     string    `json:"upstream"`
	PromptTokens int       `json:"prompt_tokens"`
	CompTokens   int       `json:"comp_tokens"`
	TotalTokens  int       `json:"total_tokens"`
	LatencyMs    int64     `json:"latency_ms"`
	Stream       bool      `json:"stream"`
	StatusCode   int       `json:"status_code"`
	Err          string    `json:"err,omitempty"`
}

type Recorder struct {
	f         *os.File
	ch        chan UsageRecord
	done      chan struct{}
	closeOnce sync.Once
	dropped   atomic.Int64
}

func (r *Recorder) Dropped() int64 {
	return r.dropped.Load()
}

func NewRecorder(path string) (*Recorder, error) {
	if path == "" {
		path = "data/usage.jsonl"
	}
	if !filepath.IsAbs(path) {
		root, err := utils.FindProjectRoot()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(root, "data", "usage.jsonl")
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	r := &Recorder{
		f:    f,
		ch:   make(chan UsageRecord, 4096),
		done: make(chan struct{}),
	}
	go r.run()
	tokenizerInit()
	return r, nil
}

func (r *Recorder) run() {
	for u := range r.ch {
		body, err := json.Marshal(u)
		if err != nil {
			continue
		}
		_, _ = r.f.Write(append(body, '\n'))
	}
	_ = r.f.Close()
	close(r.done)
}

func (r *Recorder) Record(u UsageRecord) {
	if r == nil {
		return
	}
	select {
	case r.ch <- u:
	default:
		r.dropped.Add(1)
	}
}

func (r *Recorder) Close() error {
	var err error
	r.closeOnce.Do(func() {
		close(r.ch)
		select {
		case <-r.done:
		case <-time.After(3 * time.Second):
			err = errors.New("recorder: flush timed out on shutdown")
		}
	})
	if n := r.dropped.Load(); n > 0 {
		logger.Get().Info().Msgf("Dropped %d usage records", n)
	}
	return err
}
