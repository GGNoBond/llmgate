package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aoxin/llmgate/src/auth"
	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/gateway"
	"github.com/aoxin/llmgate/src/health"
	"github.com/aoxin/llmgate/src/lb"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/middleware"
	"github.com/aoxin/llmgate/src/ratelimit"
	"github.com/aoxin/llmgate/src/router"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.GetConfig("config/config.yaml")
	logger.Init(&cfg.Logger)
	lgr := logger.Get()
	tr := gateway.NewTransport(&cfg.Transport)
	lb := &lb.LeastConn{}
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		lgr.Fatal().Msgf("Redis connection failed: %v", err)
	}
	st := auth.NewStore(rdb)
	st.InitStore(context.Background(), cfg.Tenants)
	r, nodeList := router.BuildRouter(cfg.Upstreams, tr, lb)
	recorder, err := ratelimit.NewRecorder(cfg.Server.UsageConfigFile)
	if err != nil {
		lgr.Error().Msgf("init usage recorder failed: %v", err)
	}
	gw := &gateway.Gateway{
		Router:   r,
		Limiter:  ratelimit.NewRedisLimiter(rdb),
		Recorder: recorder,
		Store:    st,
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", middleware.Chain(
		http.HandlerFunc(gw.HandleChatCompletion),
		middleware.Recovery(),
		auth.Auth(st),
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

	defer recorder.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		lgr.Error().Msgf("Server shutdown error: %v", err)
	}
	lgr.Info().Msg("Server shutdown complete")

}
