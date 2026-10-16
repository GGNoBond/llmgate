package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aoxin/llmgate/src/metrics"
)

// statusRecorder 包一层 ResponseWriter 记录状态码。
// 必须实现 Flush()，否则下游流式 handler 的 w.(http.Flusher) 断言会失败、SSE 直接废掉。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Metrics 是横切中间件：统计每个请求的延迟、状态码计数、在途数。
// model/token 等业务维度不在此处（中间件拿不到解析后的 body），由 handler 内部打点。
func Metrics() MiddleWare {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metrics.Inflight.Inc()
			defer metrics.Inflight.Dec()
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			metrics.Latency.Observe(time.Since(start).Seconds())
			metrics.RequestsTotal.WithLabelValues(strconv.Itoa(rec.status)).Inc()
		})
	}
}
