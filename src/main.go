package main

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aoxin/llmgate/src/auth"
	"github.com/aoxin/llmgate/src/cache"
	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/gateway"
	"github.com/aoxin/llmgate/src/health"
	"github.com/aoxin/llmgate/src/lb"
	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/metrics"
	"github.com/aoxin/llmgate/src/middleware"
	"github.com/aoxin/llmgate/src/ratelimit"
	"github.com/aoxin/llmgate/src/router"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

func main() {
	configPath := os.Getenv("LLMGATE_CONFIG")
	if configPath == "" {
		configPath = "config/config.yaml"
	}
	cfg := config.GetConfig(configPath)
	logger.Init(&cfg.Logger)
	lgr := logger.Get()
	tr := gateway.NewTransport(&cfg.Transport)
	// lb := &lb.LeastConn{}
	lb := &lb.WeightedRR{}
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
	var recorder *ratelimit.Recorder
	if cfg.Server.UsageEnable == nil || *cfg.Server.UsageEnable {
		rec, err := ratelimit.NewRecorderWithOptions(ratelimit.RecorderOptions{
			Path:       cfg.Server.UsageConfigFile,
			MaxSize:    cfg.Server.UsageMaxSize,
			MaxBackups: cfg.Server.UsageMaxBackups,
			MaxAge:     cfg.Server.UsageMaxAge,
			Compress:   cfg.Server.UsageCompress,
		})
		if err != nil {
			lgr.Error().Msgf("init usage recorder failed: %v", err)
		}
		recorder = rec
	} else {
		lgr.Info().Msg("usage recording disabled")
	}
	var cc *cache.Cache
	if cfg.Cache.Enable {
		cc = cache.New(rdb, cfg.Cache.TTL)
		lgr.Info().Msgf("exact cache enabled, ttl=%v", cfg.Cache.TTL)
	}
	gw := &gateway.Gateway{
		Router:   r,
		Limiter:  ratelimit.NewRedisLimiter(rdb),
		Recorder: recorder,
		Store:    st,
		Cache:    cc,
	}

	metrics.Register()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/v1/chat/completions", middleware.Chain(
		http.HandlerFunc(gw.HandleChatCompletion),
		middleware.TraceID(),
		middleware.Metrics(),
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
