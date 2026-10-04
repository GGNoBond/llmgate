package middleware

import (
	"net/http"
	"slices"

	"github.com/aoxin/llmgate/src/logger"
)

type MiddleWare func(http.Handler) http.Handler

// Chain 将多个中间件串联起来
func Chain(h http.Handler, mws ...MiddleWare) http.Handler {
	for _, mw := range slices.Backward(mws) {
		h = mw(h)
	}
	return h
}

// Recovery 中间件，用于捕获 panic 并返回 500 错误
func Recovery() MiddleWare {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Get().Panic().Msgf("panic recovered: %v", err)
					http.Error(w, `{"error": "Internal Server Error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
