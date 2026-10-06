package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/gateway"
	"github.com/aoxin/llmgate/src/health"
	"github.com/aoxin/llmgate/src/lb"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/middleware"
	"github.com/aoxin/llmgate/src/router"
)

func main() {
	cfg := config.GetConfig("config/config.yaml")
	logger.Init(&cfg.Logger)
	lgr := logger.Get()
	tr := gateway.NewTransport(&cfg.Transport)
	// bl := &lb.WeightedRR{}
	bl := &lb.LeastConn{}
	r, nodeList := router.BuildRouter(cfg.Upstreams, tr, bl)
	gw := &gateway.Gateway{Router: r}

	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", middleware.Chain(
		http.HandlerFunc(gw.HandleChatCompletion),
		middleware.Recovery(),
	))

	server := http.Server{
		Addr:        cfg.Server.Addr,
		Handler:     mux,
		ReadTimeout: cfg.Server.ReadTimeout,
		// WriteTimeout: cfg.Server.WriteTimeout,	// 用provider中的client.Timeout
	}
	server.RegisterOnShutdown(func() {
		lgr.Info().Msg("Server is shutting down")
	})

	go func() {
		lgr.Info().Msgf("Starting server at %s", cfg.Server.Addr)
		checker := health.NewChecker()
		checker.Start(nodeList)
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
