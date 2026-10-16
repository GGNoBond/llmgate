package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// 标签基数控制：只用 model / tenant / status / result / dim / type 这类低基数维度，
// 绝不把 traceID、请求 ID 之类高基数值当 label（会撑爆 Prometheus 时间线）。
var (
	// RequestsTotal 按最终 HTTP 状态码计数，用于 QPS 和错误率。
	// 由 Metrics 中间件统一打点，覆盖所有返回路径（含各类错误）。
	RequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "llmgate_requests_total",
		Help: "Total requests by final HTTP status",
	}, []string{"status"})

	// Latency 请求整体延迟分布，用于算 P50/P99。中间件打点，覆盖全路径。
	Latency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "llmgate_request_duration_seconds",
		Help:    "Request latency in seconds",
		Buckets: prometheus.DefBuckets,
	})

	// TTFT 流式首包延迟（time to first token），简历里 "流式首包 P95" 的数据来源。
	TTFT = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "llmgate_stream_ttft_seconds",
		Help:    "Stream time-to-first-token in seconds",
		Buckets: []float64{0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10},
	})

	// TokensTotal 真实上游消耗的 token，按模型和类型（prompt/completion）累加。
	TokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "llmgate_tokens_total",
		Help: "Tokens consumed from upstream",
	}, []string{"model", "type"})

	// CacheTotal 精确缓存命中/未命中，命中率 = hit / (hit+miss)。
	CacheTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "llmgate_cache_total",
		Help: "Exact cache hit/miss",
	}, []string{"result"})

	// Inflight 网关当前在途请求数（Gauge）。
	Inflight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "llmgate_inflight_requests",
		Help: "In-flight requests",
	})

	// FailoverTotal 因实例级错误切换到下一个上游的次数。
	FailoverTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "llmgate_failover_total",
		Help: "Upstream failover count",
	})

	// RateLimitedTotal 被限流拒绝的请求数，按维度 rpm/tpm 区分。
	RateLimitedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "llmgate_rate_limited_total",
		Help: "Requests rejected by rate limit",
	}, []string{"dim"})
)

var registerOnce sync.Once

// Register 注册所有指标到默认 registry；用 sync.Once 保证多次调用（含测试）不 panic。
func Register() {
	registerOnce.Do(func() {
		prometheus.MustRegister(
			RequestsTotal, Latency, TTFT, TokensTotal,
			CacheTotal, Inflight, FailoverTotal, RateLimitedTotal,
		)
	})
}
