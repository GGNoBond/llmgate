package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aoxin/llmgate/learningtest/tinytests"
)

func ReverseProxyRun(proxyCfg *ProxyConfig) {
	if proxyCfg == nil || len(proxyCfg.ForwardPeers) == 0 {
		panic("Proxy config is nil or empty")
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	mux := http.NewServeMux()
	for _, peer := range proxyCfg.ForwardPeers {
		handler := &httputil.ReverseProxy{
			Transport: transport,
			Rewrite: func(req *httputil.ProxyRequest) {
				trimmed := strings.TrimPrefix(req.Out.URL.Path, peer.Prefix)
				newPath := tinytests.SingleJoinSlash(peer.Path, trimmed)
				req.SetURL(&url.URL{Scheme: peer.Protocol, Host: fmt.Sprintf("%s:%d", peer.Host, peer.Port)})
				req.Out.URL.Path = newPath
				req.Out.URL.RawPath = ""
				req.SetXForwarded()
				tinytests.Debug("Proxy %s %s -> %s", req.Out.Method, req.Out.URL.Path, req.Out.URL.String())
				// if dump, err := httputil.DumpRequestOut(req.Out, true); err == nil {
				// 	tinytests.Debug("==== upstream request(%s) ====\n%s", peer.Name, dump)
				// }
			},
			// ModifyResponse: func(resp *http.Response) error {
			// 	if dump, err := httputil.DumpResponse(resp, true); err == nil {
			// 		tinytests.Debug("==== downstream response(%s) ====\n%s", peer.Name, dump)
			// 	}
			// 	return nil
			// },
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				tinytests.Error("ReverseProxy error for %s: %v", r.URL.Path, err)
				http.Error(w, "Bad Gateway:"+err.Error(), http.StatusBadGateway)
			},
		}
		mux.Handle(peer.Prefix, handler)
		mux.Handle(peer.Prefix+"/", handler)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", proxyCfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	server.RegisterOnShutdown(transport.CloseIdleConnections)

	go func() {
		tinytests.Info("Starting proxy server on %d", proxyCfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			tinytests.Fatal("Failed to start proxy server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	tinytests.Debug("Received signal %v, shutting down...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		tinytests.Error("Failed to shutdown proxy server: %v", err)
	}
	tinytests.Info("Proxy server shutdown completed")
}
