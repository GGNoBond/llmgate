package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/middleware"
	"github.com/aoxin/llmgate/src/openai"
	"github.com/aoxin/llmgate/src/provider"
	"github.com/aoxin/llmgate/src/utils"
)

type Gateway struct {
	router *provider.Router
}

func newTransport(cfg *config.TransportConfig) *http.Transport {
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

func buildRouter(cfg *config.Config, tr *http.Transport) *provider.Router {
	r := provider.NewRouter()
	r.InitRouteTable(cfg.Upstream, tr)
	return r
}

func (gw *Gateway) pickProvider(ps []provider.Provider) (*provider.Provider, error) {
	// TODO::pick a provider
	if ps == nil || len(ps) == 0 {
		return nil, fmt.Errorf("no available provider")
	}
	return &ps[0], nil
}

func (gw *Gateway) handleChatCompletion(w http.ResponseWriter, r *http.Request) {
	var req openai.ChatCompletionRequest
	// Parse request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONErr(w, http.StatusBadRequest, "Invalid Body:"+err.Error())
		return
	}
	p, err := gw.router.Route(req.Model)
	if err != nil {
		utils.WriteJSONErr(w, http.StatusNotFound, err.Error())
		return
	}
	// SSE support
	if req.Stream {
		gw.handleChatCompletionStream(w, r, p, &req)
		return
	}

	start := time.Now()
	resp, err := p.Chat(r.Context(), &req)
	if err != nil {
		utils.WriteJSONErr(w, http.StatusBadGateway, err.Error())
		return
	}
	logger.Get().Debug().Msgf("[%s] model=%s time_cost=%v tokens=%d", p.Name(), req.Model, time.Since(start), resp.Usage.TotalTokens)
	utils.WriteJSON(w, http.StatusOK, resp)
}

func (gw *Gateway) handleChatCompletionStream(w http.ResponseWriter, r *http.Request, p provider.Provider, req *openai.ChatCompletionRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.WriteJSONErr(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	start := time.Now()
	var firstTrunk time.Time
	n := 0

	err := p.ChatStream(r.Context(), req, func(chunk *openai.ChatCompletionChunk) error {
		select {
		// context canceled
		case <-r.Context().Done():
			logger.Get().Error().Msgf("[%s] Context canceled", p.Name())
			return r.Context().Err()
		default:
		}
		if n == 0 {
			firstTrunk = time.Now()
		}
		n++
		data, _ := json.Marshal(chunk)
		logger.Get().Trace().Msgf("[%s] Read chunk: %s", time.Now().Format("15:04:05.000"), chunk.Choices[0].Delta.Content)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// context canceled
			logger.Get().Info().Msgf("[%s] Context canceled", p.Name())
		} else {
			// other error
			logger.Get().Error().Msgf("[%s] Error: %v", p.Name(), err)
		}
	} else {
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		logger.Get().Debug().Msgf("[%s] model=%s Stream done: %d chunks, TTFT:%v, TT:%v", p.Name(), req.Model, n, firstTrunk.Sub(start), time.Since(start))
	}
}

func main() {
	cfg := config.GetConfig("config/config.yaml")
	logger.Init(&cfg.Logger)
	lgr := logger.Get()
	tr := newTransport(&cfg.Transport)
	gw := &Gateway{router: buildRouter(cfg, tr)}

	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", middleware.Chain(
		http.HandlerFunc(gw.handleChatCompletion),
		middleware.Recovery(),
	))

	server := http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}
	server.RegisterOnShutdown(func() {
		lgr.Info().Msg("Server is shutting down")
	})

	go func() {
		lgr.Info().Msgf("Starting server at %s", cfg.Server.Addr)
		if err := server.ListenAndServe(); err != nil {
			lgr.Error().Msgf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	lgr.Info().Msgf("Shutting down server due to %s", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		lgr.Error().Msgf("Server shutdown error: %v", err)
	}
	lgr.Info().Msg("Server shutdown complete")

}
