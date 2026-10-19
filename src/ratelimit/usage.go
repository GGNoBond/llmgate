package ratelimit

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/utils"
	"gopkg.in/natefinch/lumberjack.v2"
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
	w         io.WriteCloser
	ch        chan UsageRecord
	done      chan struct{}
	closeOnce sync.Once
	dropped   atomic.Int64
}

// RecorderOptions 控制用量落库文件的位置与轮转策略。
// 轮转字段零值时使用默认：单文件 100MB、保留 10 份、7 天、不压缩。
type RecorderOptions struct {
	Path       string
	MaxSize    int // 单文件最大 MB，超过则轮转
	MaxBackups int // 最多保留的旧文件数
	MaxAge     int // 旧文件最多保留天数
	Compress   bool
}

const (
	defaultUsageMaxSize    = 100 // MB
	defaultUsageMaxBackups = 10
	defaultUsageMaxAge     = 7 // 天
)

func (r *Recorder) Dropped() int64 {
	return r.dropped.Load()
}

// NewRecorder 用默认轮转策略在 path 落库（供测试与简单调用）。
func NewRecorder(path string) (*Recorder, error) {
	return NewRecorderWithOptions(RecorderOptions{Path: path})
}

// NewRecorderWithOptions 按给定轮转策略创建 Recorder：底层用 lumberjack
// 按大小自动切割，避免压测时 usage.jsonl 无限增长。
func NewRecorderWithOptions(opts RecorderOptions) (*Recorder, error) {
	path, err := resolveUsagePath(opts.Path)
	if err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	maxSize := opts.MaxSize
	if maxSize <= 0 {
		maxSize = defaultUsageMaxSize
	}
	maxBackups := opts.MaxBackups
	if maxBackups <= 0 {
		maxBackups = defaultUsageMaxBackups
	}
	maxAge := opts.MaxAge
	if maxAge <= 0 {
		maxAge = defaultUsageMaxAge
	}
	w := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   opts.Compress,
	}
	r := &Recorder{
		w:    w,
		ch:   make(chan UsageRecord, 4096),
		done: make(chan struct{}),
	}
	go r.run()
	tokenizerInit()
	return r, nil
}

// resolveUsagePath 处理空路径与相对路径：相对路径统一落到项目根 data/usage.jsonl。
func resolveUsagePath(path string) (string, error) {
	if path == "" {
		path = "data/usage.jsonl"
	}
	if !filepath.IsAbs(path) {
		root, err := utils.FindProjectRoot()
		if err != nil {
			return "", err
		}
		path = filepath.Join(root, "data", "usage.jsonl")
	}
	return path, nil
}

func (r *Recorder) run() {
	for u := range r.ch {
		body, err := json.Marshal(u)
		if err != nil {
			continue
		}
		_, _ = r.w.Write(append(body, '\n'))
	}
	_ = r.w.Close()
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
	if r == nil {
		return nil
	}
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
