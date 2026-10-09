package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/keypool"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/openai"
)

type OpenAIProvider struct {
	name               string
	baseURL            string
	pool               *keypool.Pool
	streamChunkTimeout time.Duration
	client             *http.Client
	streamClient       *http.Client
}

func (p *OpenAIProvider) BaseURL() string {
	return p.baseURL
}

func NewOpenAIProvider(upstreamCfg *config.UpstreamConfig, tr *http.Transport) *OpenAIProvider {
	return &OpenAIProvider{
		name:               upstreamCfg.Name,
		baseURL:            upstreamCfg.BaseURL,
		pool:               keypool.NewPool(upstreamCfg.KeyPool),
		streamChunkTimeout: upstreamCfg.StreamChunkTimeout,
		client: &http.Client{
			Transport: tr,
			Timeout:   upstreamCfg.PerResponseTimeout,
		},
		streamClient: &http.Client{
			Transport: tr,
		},
	}
}

func (p *OpenAIProvider) Name() string {
	return p.name
}

func (p *OpenAIProvider) Chat(ctx context.Context, req *openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error) {
	body, _ := json.Marshal(req)

	maxKeyTries := p.pool.Size()
	if maxKeyTries <= 0 {
		maxKeyTries = 1
	}
	var lastErr error
	for range maxKeyTries {
		apiKey, ok := p.pool.Next()
		if !ok {
			return nil, &UpstreamError{
				Err:        errors.New("all api keys rate limited"),
				StatusCode: http.StatusTooManyRequests,
			}
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			return nil, &UpstreamError{Err: err, StatusCode: 0}
		}
		// 429 key限流, 换key
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := ParseRetryAfter(resp.Header)
			p.pool.MarkRateLimited(apiKey, retryAfter)
			resp.Body.Close()
			lastErr = &UpstreamError{
				Err:        errors.New("upstream error: too many requests"),
				StatusCode: resp.StatusCode,
				RetryAfter: retryAfter,
			}
			logger.Get().Warn().Msgf("[%s] key %s 429, cooling down for %v", p.name, apiKey, retryAfter)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, &UpstreamError{
				Err:        fmt.Errorf("upstream error: %s", resp.Status),
				StatusCode: resp.StatusCode,
			}
		}

		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var out openai.ChatCompletionResponse
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return &out, nil

	}
	return nil, lastErr
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req *openai.ChatCompletionRequest, h StreamHandler) error {
	body, _ := json.Marshal(req)

	maxKeyTries := p.pool.Size()
	if maxKeyTries <= 0 {
		maxKeyTries = 1
	}
	var resp *http.Response
	var lastErr error
	for range maxKeyTries {
		apiKey, ok := p.pool.Next()
		if !ok {
			return &UpstreamError{
				Err:        errors.New("all api keys rate limited"),
				StatusCode: http.StatusTooManyRequests,
			}
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)

		r, err := p.streamClient.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			return &UpstreamError{Err: err, StatusCode: 0}
		}
		if r.StatusCode == http.StatusTooManyRequests {
			retryAfter := ParseRetryAfter(r.Header)
			p.pool.MarkRateLimited(apiKey, retryAfter)
			r.Body.Close()
			lastErr = &UpstreamError{
				Err:        errors.New("upstream error: too many requests"),
				StatusCode: r.StatusCode,
				RetryAfter: retryAfter,
			}
			logger.Get().Warn().Msgf("[%s] key %s 429, cooling down for %v", p.name, apiKey, retryAfter)
			continue
		}
		if r.StatusCode < 200 || r.StatusCode >= 300 {
			return &UpstreamError{
				Err:        fmt.Errorf("upstream error: %s", r.Status),
				StatusCode: r.StatusCode,
			}
		}
		resp = r
		break
	}
	if resp == nil {
		return lastErr
	}
	defer resp.Body.Close()
	// 用streamChunkTimeout 来控制每个chunk的超时
	// 起单独goroutine负责resp.Body上的阻塞读
	// lines传递读到的行，errCh传递scanner错误码
	// main goroutine创建timer，并等待timer和lines数据
	// 处理line时重置timer, 如果timer先于lines数据到期，则返回ctx deadline error
	lines := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
		errCh <- scanner.Err()
	}()

	sseTimeout := p.streamChunkTimeout
	sseChunkTimer := time.NewTimer(sseTimeout)
	defer sseChunkTimer.Stop()

	for {
		select {
		case line := <-lines:
			sseChunkTimer.Reset(sseTimeout)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				return nil
			}
			var chunk openai.ChatCompletionChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				logger.Get().Error().Msgf("json.Unmarshal error: %v, payload: %s", err, payload)
				continue
			}

			if err := h(&chunk); err != nil {
				return err
			}
		case <-sseChunkTimer.C:
			return errors.New("SSE chunk timeout")
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
