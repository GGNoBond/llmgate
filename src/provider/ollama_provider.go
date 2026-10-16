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
	"time"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/openai"
	"github.com/aoxin/llmgate/src/utils"
)

type OllamaProvider struct {
	name               string
	baseURL            string
	streamChunkTimeout time.Duration
	client             *http.Client
	streamClient       *http.Client
}

func (p *OllamaProvider) BaseURL() string {
	return p.baseURL
}

func NewOllamaProvider(upstreamCfg *config.UpstreamConfig, tr *http.Transport) *OllamaProvider {
	return &OllamaProvider{
		name:               upstreamCfg.Name,
		baseURL:            upstreamCfg.BaseURL,
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
func (p *OllamaProvider) Name() string {
	return p.name
}

// ollama /api/chat Response struct
type (
	ollamaOptions struct {
		Temperature *float64 `json:"temperature,omitempty"`
		NumPredict  *int     `json:"num_predict,omitempty"`
	}
	ollamaRequest struct {
		Model    string               `json:"model"`
		Messages []openai.ChatMessage `json:"messages"`
		Stream   bool                 `json:"stream"`
		Options  ollamaOptions        `json:"options"`
	}
	ollamaResponse struct {
		Model                 string             `json:"model"`
		CreatedAt             time.Time          `json:"created_at"`
		Message               openai.ChatMessage `json:"message"`
		Done                  bool               `json:"done"`
		DoneReason            string             `json:"done_reason"`
		TotalDuration         *int               `json:"total_duration,omitempty"`
		LoadDuration          *int               `json:"load_duration,omitempty"`
		PromptEvalCount       *int               `json:"prompt_eval_count,omitempty"`
		PromptEvalCachedCount *int               `json:"prompt_eval_cached_count,omitempty"`
		PromptEvalDuration    *int               `json:"prompt_eval_duration,omitempty"`
		EvalCount             *int               `json:"eval_count,omitempty"`
		EvalDuration          *int               `json:"eval_duration,omitempty"`
	}
)

func (p *OllamaProvider) Chat(ctx context.Context, req *openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error) {
	or := ollamaRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   req.Stream,
		Options: ollamaOptions{
			Temperature: req.Temperature,
			NumPredict:  req.MaxCompletionTokens,
		},
	}
	body, _ := json.Marshal(or)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, &UpstreamError{Err: err, StatusCode: 0}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &UpstreamError{
			Err:        fmt.Errorf("upstream error: %s", resp.Status),
			StatusCode: resp.StatusCode,
		}
	}
	data, _ := io.ReadAll(resp.Body)
	var ollamaResp ollamaResponse
	if err := json.Unmarshal(data, &ollamaResp); err != nil {
		return nil, err
	}

	// convert ollama response to openai response
	now := time.Now()
	return &openai.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%s", now.Format("20060102150405")),
		Object:  "chat.completion",
		Created: ollamaResp.CreatedAt.Unix(),
		Model:   ollamaResp.Model,
		Choices: []openai.Choice{
			{
				Index:        0,
				Message:      ollamaResp.Message,
				FinishReason: ollamaResp.DoneReason,
			},
		},
		Usage: openai.Usage{
			CompletionTokens: utils.Defer(ollamaResp.EvalCount, 0),
			PromptTokens:     utils.Defer(ollamaResp.PromptEvalCount, 0),
			TotalTokens:      utils.Defer(ollamaResp.EvalCount, 0) + utils.Defer(ollamaResp.PromptEvalCount, 0),
		},
	}, nil
}

func (p *OllamaProvider) ChatStream(ctx context.Context, req *openai.ChatCompletionRequest, h StreamHandler) error {
	or := ollamaRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   true,
		Options: ollamaOptions{
			Temperature: req.Temperature,
			NumPredict:  req.MaxCompletionTokens,
		},
	}
	body, _ := json.Marshal(or)
	streamCtx, cancel := context.WithCancel(ctx)
	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		cancel()
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.streamClient.Do(httpReq)
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return err
		}
		return &UpstreamError{Err: err, StatusCode: 0}
	}
	defer func() {
		cancel()
		resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &UpstreamError{
			Err:        fmt.Errorf("upstream error: %s", resp.Status),
			StatusCode: resp.StatusCode,
		}
	}
	id := fmt.Sprintf("chatcmpl-%d", time.Now().Unix())
	first := true

	// 类似于 openai_provider.go 的实现
	lines := make(chan []byte)
	errCh := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			select {
			case lines <- bytes.TrimSpace(scanner.Bytes()):
			case <-streamCtx.Done():
				errCh <- ctx.Err()
				return
			}
		}
		errCh <- scanner.Err()
	}()

	sseTimeout := p.streamChunkTimeout
	sseTimer := time.NewTimer(sseTimeout)
	defer sseTimer.Stop()

	for {
		select {
		case line := <-lines:
			sseTimer.Reset(sseTimeout)
			if len(line) == 0 {
				continue
			}
			var or ollamaResponse
			if err := json.Unmarshal(line, &or); err != nil {
				logger.Get().Error().Msgf("json.Unmarshal error: %v, line: %s", err, line)
				continue
			}
			chunk := &openai.ChatCompletionChunk{
				Id:                id,
				Object:            "chat.completion.chunk",
				Created:           time.Now().Unix(),
				Model:             or.Model,
				SystemFingerprint: "fp_ollama",
				Choices: []openai.ChunkChoice{
					{
						Index: 0,
						Delta: openai.ChatMessage{
							Content: or.Message.Content,
						},
					},
				},
			}
			if first {
				chunk.Choices[0].Delta.Role = or.Message.Role
				first = false
			}
			if or.Done {
				chunk.Choices[0].FinishReason = &or.DoneReason
				chunk.Usage = &openai.Usage{
					CompletionTokens: utils.Defer(or.EvalCount, 0),
					PromptTokens:     utils.Defer(or.PromptEvalCount, 0),
					TotalTokens:      utils.Defer(or.EvalCount, 0) + utils.Defer(or.PromptEvalCount, 0),
				}
			}
			if err := h(chunk); err != nil {
				return err
			}
			if or.Done {
				return nil
			}
		case <-sseTimer.C:
			return errors.New("SSE chunk timeout")
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
