package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/openai"
	"github.com/aoxin/llmgate/src/provider"
	"github.com/aoxin/llmgate/src/router"
	"github.com/aoxin/llmgate/src/utils"
)

type Gateway struct {
	Router *router.Router
}

func NewTransport(cfg *config.TransportConfig) *http.Transport {
	return &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		DialContext: (&net.Dialer{
			Timeout: cfg.DialContextTimeout,
		}).DialContext,
		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
	}
}

func (gw *Gateway) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	var req openai.ChatCompletionRequest
	// Parse request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONErr(w, http.StatusBadRequest, "Invalid Body:"+err.Error())
		return
	}
	// SSE support
	if req.Stream {
		gw.HandleChatCompletionStream(w, r, &req)
		return
	}

	maxRetry := 3
	var lastErr error
	tried := map[string]bool{}
	for range maxRetry {
		node, err := gw.Router.Pick(req.Model)
		if err != nil {
			if lastErr != nil {
				utils.WriteJSONErr(w, http.StatusBadGateway, "all upstream failed: "+lastErr.Error())
			} else {
				utils.WriteJSONErr(w, http.StatusServiceUnavailable, err.Error())
			}
			return
		}
		if tried[node.Backend.ID] {
			continue
		}
		tried[node.Backend.ID] = true
		start := time.Now()
		node.Backend.IncInflight()
		resp, err := node.Provider.Chat(r.Context(), &req)
		node.Backend.DecInflight()

		if err != nil {
			isInstErr, statusCode := provider.IsInstanceErr(err)
			logger.Get().Trace().Msgf("isInstErr: %v, statusCode: %v", isInstErr, statusCode)
			// TODO: 如果statusCode=429, 要根据其含义做处理"
			//  1. api_key限流：需要key冷却，更换key
			//  2. 实例过载：可以走熔断"
			if isInstErr {
				lastErr = err
				node.CB.OnFailure()
				logger.Get().Error().Msgf("[FAILURE] %s 失败 (%v), 尝试切换实例", node.Backend.ID, err)
				continue
			}
			utils.WriteJSONErr(w, statusCode, err.Error())
			return
		}
		node.CB.OnSuccess()
		logger.Get().Debug().Msgf("[%s] model=%s time_cost=%v tokens=%d", node.Backend.ID, req.Model, time.Since(start), resp.Usage.TotalTokens)
		utils.WriteJSON(w, http.StatusOK, resp)
		return
	}
	if lastErr != nil {
		utils.WriteJSONErr(w, http.StatusBadGateway, "all upstream failed: "+lastErr.Error())
	} else {
		utils.WriteJSONErr(w, http.StatusServiceUnavailable, "no available upstream")
	}
}

func (gw *Gateway) HandleChatCompletionStream(w http.ResponseWriter, r *http.Request, req *openai.ChatCompletionRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.WriteJSONErr(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	maxRetry := 3
	tried := map[string]bool{}
	var lastErr error
	for range maxRetry {
		node, err := gw.Router.Pick(req.Model)
		if err != nil {
			if lastErr != nil {
				utils.WriteJSONErr(w, http.StatusBadGateway, "all upstream failed: "+lastErr.Error())
			} else {
				utils.WriteJSONErr(w, http.StatusServiceUnavailable, err.Error())
			}
			return
		}
		if tried[node.Backend.ID] {
			continue
		}
		tried[node.Backend.ID] = true
		start := time.Now()
		var firstTrunk time.Time
		n := 0

		node.Backend.IncInflight()
		err = node.Provider.ChatStream(r.Context(), req, func(chunk *openai.ChatCompletionChunk) error {
			select {
			case <-r.Context().Done():
				logger.Get().Trace().Msgf("[%s] Context canceled", node.Backend.ID)
				return r.Context().Err()
			default:
			}
			if n == 0 {
				firstTrunk = time.Now()
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.WriteHeader(http.StatusOK)
			}
			n++
			data, _ := json.Marshal(chunk)
			// logger.Get().Trace().Msgf("[%s] Read chunk: %s", time.Now().Format("15:04:05.000"), chunk.Choices[0].Delta.Content)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return nil
		})
		node.Backend.DecInflight()

		if err == nil {
			node.CB.OnSuccess()
			if n > 0 {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				logger.Get().Debug().Msgf("[%s] model=%s Stream done: %d chunks, TTFT:%v, TT:%v", node.Backend.ID, req.Model, n, firstTrunk.Sub(start), time.Since(start))
			} else {
				lastErr = fmt.Errorf("%s empty stream", node.Backend.ID)
				utils.WriteJSONErr(w, http.StatusBadGateway, "all upstream failed: "+lastErr.Error())
			}
			return
		}

		if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
			logger.Get().Trace().Msgf("[%s] Context canceled", node.Backend.ID)
			return
		}

		isInstErr, statusCode := provider.IsInstanceErr(err)
		// 尚未发chunk
		if n == 0 {
			lastErr = err
			if isInstErr { // 实例级错误
				node.CB.OnFailure()
				logger.Get().Error().Msgf("[FAILURE] %s 失败 (%v), 尝试切换实例", node.Backend.ID, err)
				continue
			}
			utils.WriteJSONErr(w, statusCode, err.Error())
			return
		}
		node.CB.OnFailure()
		logger.Get().Error().Msgf("[%s] Stream disconnected(%v), %d chunks sent.", node.Backend.ID, err, n)
		fmt.Fprintf(w, "event: error\ndata: {\"error\": %q}\n\n", err.Error())
		flusher.Flush()
		return
	}
	if lastErr != nil {
		utils.WriteJSONErr(w, http.StatusBadGateway, "all upstream failed: "+lastErr.Error())
	} else {
		utils.WriteJSONErr(w, http.StatusServiceUnavailable, "no available upstream")
	}
}
