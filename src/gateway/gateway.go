package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aoxin/llmgate/src/auth"
	"github.com/aoxin/llmgate/src/cache"
	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/metrics"
	"github.com/aoxin/llmgate/src/openai"
	"github.com/aoxin/llmgate/src/provider"
	"github.com/aoxin/llmgate/src/ratelimit"
	"github.com/aoxin/llmgate/src/router"
	"github.com/aoxin/llmgate/src/utils"
)

type Gateway struct {
	Router   *router.Router
	Limiter  ratelimit.Limiter
	Recorder *ratelimit.Recorder
	Store    *auth.Store
	Cache    *cache.Cache
}

func (gw *Gateway) recordErr(tenant *auth.Tenant, req *openai.ChatCompletionRequest, node *router.Node, statusCode int, isStream bool, err error) {
	if gw.Recorder == nil || tenant == nil {
		return
	}
	errMsg := "unknown"
	if err != nil {
		errMsg = err.Error()
	}
	upstream := ""
	if node != nil {
		upstream = node.Backend.ID
	}
	gw.Recorder.Record(ratelimit.UsageRecord{
		Timestamp:  time.Now(),
		TenantID:   tenant.ID,
		Model:      req.Model,
		Upstream:   upstream,
		Stream:     isStream,
		StatusCode: statusCode,
		Err:        errMsg,
	})
}

// deliverCached 重写 id/created 后，按 req.Stream 决定 JSON 还是 SSE 回放；
// 并记录一次命中（Upstream="cache"、TotalTokens=0）用于命中率/节省统计。
func (gw *Gateway) deliverCached(w http.ResponseWriter, r *http.Request, tenant *auth.Tenant, req *openai.ChatCompletionRequest, resp *openai.ChatCompletionResponse) {
	resp.ID = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	resp.Created = time.Now().Unix()

	if gw.Recorder != nil && tenant != nil {
		gw.Recorder.Record(ratelimit.UsageRecord{
			Timestamp:  time.Now(),
			TenantID:   tenant.ID,
			Model:      req.Model,
			Upstream:   "cache",
			Stream:     req.Stream,
			StatusCode: http.StatusOK,
		})
	}

	if !req.Stream {
		w.Header().Set("X-LLMGate-Cache", "HIT")
		utils.WriteJSON(w, http.StatusOK, resp)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.WriteJSON(w, http.StatusOK, resp)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-LLMGate-Cache", "HIT")
	w.WriteHeader(http.StatusOK)

	base := openai.ChatCompletionChunk{
		Id: resp.ID, Object: "chat.completion.chunk", Created: resp.Created, Model: resp.Model,
	}
	emit := func(c openai.ChatCompletionChunk) {
		data, _ := json.Marshal(c)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	roleChunk := base
	roleChunk.Choices = []openai.ChunkChoice{{Index: 0, Delta: openai.ChatMessage{Role: "assistant"}}}
	emit(roleChunk)

	content := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	runes := []rune(content)
	const piece = 8
	for i := 0; i < len(runes); i += piece {
		end := min(i+piece, len(runes))
		c := base
		c.Choices = []openai.ChunkChoice{{Index: 0, Delta: openai.ChatMessage{Content: string(runes[i:end])}}}
		emit(c)
	}

	stop := "stop"
	endChunk := base
	endChunk.Choices = []openai.ChunkChoice{{Index: 0, Delta: openai.ChatMessage{}, FinishReason: &stop}}
	emit(endChunk)

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
	logger.Ctx(r.Context()).Debug().Msgf("[cache] HIT model=%s tenant=%s stream=%v", req.Model, tenant.ID, req.Stream)
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

func writeFinalErr(w http.ResponseWriter, lastErr error) {
	if lastErr == nil {
		utils.WriteJSONErr(w, http.StatusServiceUnavailable, "no available upstream")
		return
	}
	if ue, ok := provider.IsRateLimit(lastErr); ok {
		if ue.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(ue.RetryAfter.Seconds())))
		}
		utils.WriteJSONErr(w, http.StatusTooManyRequests, "upstream rate limited: "+lastErr.Error())
		return
	}
	utils.WriteJSONErr(w, http.StatusBadGateway, "all upstream failed: "+lastErr.Error())

}
func (gw *Gateway) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	var req openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONErr(w, http.StatusBadRequest, "Invalid Body:"+err.Error())
		return
	}
	tenant, ok := r.Context().Value(auth.CtxAuthKey{}).(*auth.Tenant)
	if !ok || tenant == nil {
		utils.WriteJSONErr(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if !tenant.ModelAllowed(req.Model) {
		utils.WriteJSONErr(w, http.StatusForbidden, "Model not allowed")
		return
	}
	// 缓存命中：跳过限流/计费/上游调用，直接返回（跨租户共享）
	var cacheKey string
	if gw.Cache != nil {
		cacheKey = gw.Cache.Key(&req)
		if cached, ok := gw.Cache.Get(r.Context(), cacheKey); ok {
			metrics.CacheTotal.WithLabelValues("hit").Inc()
			gw.deliverCached(w, r, tenant, &req, cached)
			return
		}
		metrics.CacheTotal.WithLabelValues("miss").Inc()
	}
	// 余额检查，RPM、TPM检查
	prompt := ratelimit.CountMessages(req.Messages)
	logger.Ctx(r.Context()).Debug().Msgf("Received chat completion request: %d", prompt)

	bl, err := gw.Store.GetBalance(r.Context(), tenant.ID)
	if err == nil && bl < int64(prompt) {
		utils.WriteJSONErr(w, http.StatusPaymentRequired, "Insufficient balance")
		return
	}
	if gw.Limiter != nil {
		if tenant.RateLimitRPM > 0 {
			if ok, _ := gw.Limiter.Allow("rl:"+tenant.ID+":rpm",
				tenant.RateLimitRPM, float64(tenant.RateLimitRPM)/60.0, 1); !ok {
				metrics.RateLimitedTotal.WithLabelValues("rpm").Inc()
				utils.WriteJSONErr(w, http.StatusTooManyRequests, "rate limit exceeded (rpm)")
				return
			}
		}
		if tenant.RateLimitTPM > 0 {
			if ok, _ := gw.Limiter.Allow("rl:"+tenant.ID+":tpm",
				tenant.RateLimitTPM, float64(tenant.RateLimitTPM)/60.0, prompt); !ok {
				metrics.RateLimitedTotal.WithLabelValues("tpm").Inc()
				utils.WriteJSONErr(w, http.StatusTooManyRequests, "rate limit exceeded (tpm)")
				return
			}
		}
	}

	// SSE 响应
	if req.Stream {
		gw.HandleChatCompletionStream(w, r, &req)
		return
	}

	maxRetry := 3
	var lastErr error
	tried := map[string]bool{}
	for range maxRetry {
		node, err := gw.Router.Pick(req.Model, tried)
		if err != nil {
			if lastErr == nil {
				utils.WriteJSONErr(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			writeFinalErr(w, lastErr)
			return
		}
		tried[node.Backend.ID] = true
		start := time.Now()
		node.Backend.IncInflight()
		resp, err := node.Provider.Chat(r.Context(), &req)
		node.Backend.DecInflight()

		if err != nil {
			isInstErr, statusCode := provider.IsInstanceErr(err)
			logger.Ctx(r.Context()).Trace().Msgf("isInstErr: %v, statusCode: %v", isInstErr, statusCode)
			if isInstErr {
				lastErr = err
				node.CB.OnFailure()
				metrics.FailoverTotal.Inc()
				logger.Ctx(r.Context()).Error().Msgf("[FAILURE] %s 失败 (%v), 尝试切换实例", node.Backend.ID, err)
				continue
			}
			utils.WriteJSONErr(w, statusCode, err.Error())
			return
		}
		node.CB.OnSuccess()
		gw.Recorder.Record(ratelimit.UsageRecord{
			Timestamp:    time.Now(),
			TenantID:     tenant.ID,
			Model:        req.Model,
			Upstream:     node.Backend.ID,
			PromptTokens: resp.Usage.PromptTokens,
			CompTokens:   resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
			LatencyMs:    time.Since(start).Milliseconds(),
			Stream:       false,
			StatusCode:   http.StatusOK,
		})
		if gw.Limiter != nil && tenant.RateLimitTPM > 0 {
			if delta := resp.Usage.TotalTokens - prompt; delta > 0 {
				gw.Limiter.Deduct("rl:"+tenant.ID+":tpm", tenant.RateLimitTPM, float64(tenant.RateLimitTPM)/60.0, delta)
			}
		}
		gw.Store.DeductBalance(r.Context(), tenant.ID, int64(resp.Usage.TotalTokens))
		metrics.TokensTotal.WithLabelValues(req.Model, "prompt").Add(float64(resp.Usage.PromptTokens))
		metrics.TokensTotal.WithLabelValues(req.Model, "completion").Add(float64(resp.Usage.CompletionTokens))
		logger.Ctx(r.Context()).Debug().Msgf("[%s] model=%s time_cost=%v tokens=%d", node.Backend.ID, req.Model, time.Since(start), resp.Usage.TotalTokens)
		if gw.Cache != nil && cacheKey != "" {
			gw.Cache.Set(r.Context(), cacheKey, resp)
		}
		utils.WriteJSON(w, http.StatusOK, resp)
		return
	}
	gw.recordErr(tenant, &req, nil, http.StatusBadGateway, false, lastErr)
	writeFinalErr(w, lastErr)
}

func (gw *Gateway) HandleChatCompletionStream(w http.ResponseWriter, r *http.Request, req *openai.ChatCompletionRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.WriteJSONErr(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}
	tenant, ok := r.Context().Value(auth.CtxAuthKey{}).(*auth.Tenant)
	if !ok || tenant == nil {
		utils.WriteJSONErr(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	maxRetry := 3
	var lastErr error
	tried := map[string]bool{}
	for range maxRetry {
		node, err := gw.Router.Pick(req.Model, tried)
		if err != nil {
			if lastErr == nil {
				utils.WriteJSONErr(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			writeFinalErr(w, lastErr)
			return
		}
		tried[node.Backend.ID] = true
		start := time.Now()
		var firstTrunk time.Time
		n := 0
		var content strings.Builder
		var upstreamUsage *openai.Usage

		node.Backend.IncInflight()
		err = node.Provider.ChatStream(r.Context(), req, func(chunk *openai.ChatCompletionChunk) error {
			select {
			case <-r.Context().Done():
				logger.Ctx(r.Context()).Trace().Msgf("[%s] Context canceled", node.Backend.ID)
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
			if len(chunk.Choices) > 0 {
				content.WriteString(chunk.Choices[0].Delta.Content)
			}
			if chunk.Usage != nil {
				upstreamUsage = chunk.Usage
			}
			data, _ := json.Marshal(chunk)
			// if len(chunk.Choices) > 0 {
			// 	logger.Ctx(r.Context()).Trace().Msgf("[%s] Read chunk: %s", time.Now().Format("15:04:05.000"), chunk.Choices[0].Delta.Content)
			// }
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
				// token用量落库
				var pTok, cTok, tTok int
				if upstreamUsage != nil {
					pTok, cTok, tTok = upstreamUsage.PromptTokens, upstreamUsage.CompletionTokens, upstreamUsage.TotalTokens
				} else {
					pTok = ratelimit.CountMessages(req.Messages)
					cTok = ratelimit.CountText(content.String())
					tTok = pTok + cTok
				}
				tid := tenant.ID
				gw.Recorder.Record(ratelimit.UsageRecord{
					Timestamp:    time.Now(),
					TenantID:     tid,
					Model:        req.Model,
					Upstream:     node.Backend.ID,
					PromptTokens: pTok,
					CompTokens:   cTok,
					TotalTokens:  tTok,
					LatencyMs:    time.Since(start).Milliseconds(),
					Stream:       true,
					StatusCode:   http.StatusOK,
				})
				if gw.Limiter != nil && tenant.RateLimitTPM > 0 {
					if delta := tTok - pTok; delta > 0 {
						gw.Limiter.Deduct("rl:"+tid+":tpm", tenant.RateLimitTPM, float64(tenant.RateLimitTPM)/60.0, delta)
					}
				}
				gw.Store.DeductBalance(r.Context(), tenant.ID, int64(tTok))

				if gw.Cache != nil {
					gw.Cache.Set(r.Context(), gw.Cache.Key(req), &openai.ChatCompletionResponse{
						ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
						Object:  "chat.completion",
						Created: time.Now().Unix(),
						Model:   req.Model,
						Choices: []openai.Choice{{
							Index:        0,
							Message:      openai.ChatMessage{Role: "assistant", Content: content.String()},
							FinishReason: "stop",
						}},
						Usage: openai.Usage{PromptTokens: pTok, CompletionTokens: cTok, TotalTokens: tTok},
					})
				}

				metrics.TTFT.Observe(firstTrunk.Sub(start).Seconds())
				metrics.TokensTotal.WithLabelValues(req.Model, "prompt").Add(float64(pTok))
				metrics.TokensTotal.WithLabelValues(req.Model, "completion").Add(float64(cTok))
				logger.Ctx(r.Context()).Debug().Msgf("[%s] model=%s Stream done: %d chunks, TTFT:%v, TT:%v", node.Backend.ID, req.Model, n, firstTrunk.Sub(start), time.Since(start))
			} else {
				lastErr = fmt.Errorf("%s empty stream", node.Backend.ID)
				gw.recordErr(tenant, req, node, http.StatusBadGateway, true, lastErr)
				utils.WriteJSONErr(w, http.StatusBadGateway, "all upstream failed: "+lastErr.Error())
			}
			return
		}

		if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
			logger.Ctx(r.Context()).Trace().Msgf("[%s] Context canceled", node.Backend.ID)
			gw.recordErr(tenant, req, node, http.StatusTooManyRequests, true, err)
			return
		}

		isInstErr, statusCode := provider.IsInstanceErr(err)
		// 尚未发chunk
		if n == 0 {
			lastErr = err
			if isInstErr { // 实例级错误
				node.CB.OnFailure()
				metrics.FailoverTotal.Inc()
				logger.Ctx(r.Context()).Error().Msgf("[FAILURE] %s 失败 (%v), 尝试切换实例", node.Backend.ID, err)
				continue
			}
			utils.WriteJSONErr(w, statusCode, err.Error())
			gw.recordErr(tenant, req, node, statusCode, true, err)
			return
		}
		node.CB.OnFailure()
		gw.recordErr(tenant, req, node, http.StatusBadGateway, true, err)
		logger.Ctx(r.Context()).Error().Msgf("[%s] Stream disconnected(%v), %d chunks sent.", node.Backend.ID, err, n)
		fmt.Fprintf(w, "event: error\ndata: {\"error\": %q}\n\n", err.Error())
		flusher.Flush()
		return
	}
	gw.recordErr(tenant, req, nil, http.StatusBadGateway, true, lastErr)
	writeFinalErr(w, lastErr)
}
