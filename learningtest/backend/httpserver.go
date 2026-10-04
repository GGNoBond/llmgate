package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aoxin/llmgate/learningtest/tinytests"
)

func HTTPServerRun(cfg *BackendConfig) {
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           getRouter(),
		ReadHeaderTimeout: 3 * time.Second, // 防 Slowloris：限制读请求头的时间
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second, // keep-alive 连接的空闲上限
	}

	// 在独立 goroutine 中启动服务，主 goroutine 负责等待退出信号
	go func() {
		tinytests.Debug("Starting %s at port %d", cfg.Name, cfg.Port)
		// ListenAndServe 在 Shutdown 时会返回 http.ErrServerClosed，属正常退出，不算错误
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			tinytests.Fatal("HTTP Server Fatal: %v", err)
		}
	}()

	// 阻塞等待中断信号（Ctrl+C / kill）
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	tinytests.Debug("Received signal %v, shutting down...", sig)

	// 给在途请求最多 5 秒处理完，然后关闭监听
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		tinytests.Fatal("HTTP Server Shutdown Error: %v", err)
	}
	tinytests.Debug("Server exited gracefully")
}
