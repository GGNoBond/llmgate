package ratelimit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	mu sync.Mutex
	f  *os.File
}

func NewRecorder(path string) (*Recorder, error) {
	if path == "" {
		root, err := utils.FindProjectRoot()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(root, "data", "usage.json")
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	tokenizerInit()
	return &Recorder{f: f}, nil
}

func (r *Recorder) Record(u UsageRecord) {
	if r == nil || r.f == nil {
		return
	}
	b, err := json.Marshal(u)
	if err != nil {
		return
	}
	b = append(b, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.f.Write(b)
}

func (r *Recorder) Close() {
	if r != nil && r.f != nil {
		_ = r.f.Close()
	}
}
