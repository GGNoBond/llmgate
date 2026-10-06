package provider

import (
	"context"
	"errors"
	"net/http"

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
