package provider

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aoxin/llmgate/src/openai"
)

// Provider 上游服务提供者接口
type Provider interface {
	Name() string
	BaseURL() string
	Chat(ctx context.Context, req *openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error)
	ChatStream(ctx context.Context, req *openai.ChatCompletionRequest, h StreamHandler) error
}

// UpstreamError 打包http响应码和错误，用于判断是否是实例错误
type UpstreamError struct {
	StatusCode int
	RetryAfter time.Duration
	Err        error
}

func (e *UpstreamError) Error() string {
	return e.Err.Error()
}
func (e *UpstreamError) Unwrap() error {
	return e.Err
}

// IsInstanceErr 判断是否是实例错误
func IsInstanceErr(err error) (bool, int) {
	ue, ok := errors.AsType[*UpstreamError](err)
	// 非实例级错误也携带响应码，如果无或者为0 则默认为502
	resCode := http.StatusBadGateway
	if !ok {
		return false, resCode
	}
	instErr := ue.StatusCode == 0 || ue.StatusCode >= 500 || ue.StatusCode == 429
	resCode = ue.StatusCode
	return instErr, resCode
}

type StreamHandler func(chunk *openai.ChatCompletionChunk) error

func ParseRetryAfter(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs > 0 {
			return time.Duration(secs) * time.Second
		}
		return 0
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
func IsRateLimit(err error) (*UpstreamError, bool) {
	var ue *UpstreamError
	if errors.As(err, &ue) && ue.StatusCode == http.StatusTooManyRequests {
		return ue, true
	}
	return nil, false
}
