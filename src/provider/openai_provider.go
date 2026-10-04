package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/openai"
)

type OpenAIProvider struct {
	name               string
	baseURL            string
	apiKey             string
	streamChunkTimeout time.Duration
	client             *http.Client
	streamClient       *http.Client
}

func NewOpenAIProvider(upstreamCfg *config.UpstreamConfig, tr *http.Transport) *OpenAIProvider {
	return &OpenAIProvider{
		name:               upstreamCfg.Name,
		baseURL:            upstreamCfg.BaseURL,
		apiKey:             upstreamCfg.APIKey,
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	{
		// dump request
		reqDump, err := httputil.DumpRequest(httpReq, true)
		if err != nil {
			return nil, err
		}
		logger.Get().Trace().Msgf("build httpReq:\n%s", reqDump)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	{
		// dump response
		respDump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			return nil, err
		}
		logger.Get().Trace().Msgf("Response:\n%s", respDump)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response status code: %d, status: %s", resp.StatusCode, resp.Status)
	}

	data, _ := io.ReadAll(resp.Body)
	var out openai.ChatCompletionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req *openai.ChatCompletionRequest, h StreamHandler) error {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.streamClient.Do(httpReq)
	if err != nil {
		return err
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
				break
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
			return fmt.Errorf("SSE chunk timeout")
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
