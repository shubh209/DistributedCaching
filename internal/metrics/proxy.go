package metrics

import "github.com/prometheus/client_golang/prometheus"

// ProxyMetrics holds all Prometheus metrics for the reverse proxy.
type ProxyMetrics struct {
	Hits           *prometheus.CounterVec
	Misses         *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	CachedEntries  prometheus.Gauge
	UpstreamErrors *prometheus.CounterVec
}

// NewProxyMetrics creates and registers all reverse proxy metrics.
func NewProxyMetrics(reg prometheus.Registerer) *ProxyMetrics {
	m := &ProxyMetrics{
		Hits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_proxy_hits_total",
			Help: "Total proxy cache hits (requests served without calling upstream).",
		}, []string{}),

		Misses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_proxy_misses_total",
			Help: "Total proxy cache misses (requests forwarded to upstream).",
		}, []string{}),

		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dcg_proxy_request_duration_seconds",
			Help:    "Proxied request latency in seconds (p99 derivable from histogram).",
			Buckets: prometheus.DefBuckets,
		}, []string{"status_code"}),

		CachedEntries: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dcg_proxy_cached_entries",
			Help: "Current number of cached HTTP responses in the proxy store.",
		}),

		UpstreamErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_proxy_upstream_errors_total",
			Help: "Total upstream errors encountered by the proxy.",
		}, []string{"error_type"}), // error_type: timeout | 5xx | connect_error
	}

	reg.MustRegister(
		m.Hits, m.Misses,
		m.RequestDuration,
		m.CachedEntries,
		m.UpstreamErrors,
	)
	return m
}
