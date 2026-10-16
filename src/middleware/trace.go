package middleware

import (
	"net/http"

	"github.com/aoxin/llmgate/src/logger"
	"github.com/google/uuid"
)

// TraceHeader 是 traceID 的透传 header 名。
const TraceHeader = "X-Trace-Id"

// TraceID 是横切中间件：透传上游带来的 X-Trace-Id，没有则生成 UUID；
// 写回响应头 + 注入 ctx（并绑定到 zerolog），串起一次请求的全链路日志。
func TraceID() MiddleWare {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tid := r.Header.Get(TraceHeader)
			if tid == "" {
				tid = uuid.NewString()
			}
			w.Header().Set(TraceHeader, tid)
			ctx := logger.WithTraceID(r.Context(), tid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
