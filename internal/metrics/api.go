package metrics

import "github.com/prometheus/client_golang/prometheus"

// APIMetrics holds all Prometheus metrics for the API layer.
// Labels allow all 3 invalidation strategies to be compared on the same panel.
type APIMetrics struct {
	Requests        *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	CacheHits       *prometheus.CounterVec
	CacheMisses     *prometheus.CounterVec
	DBQueries       *prometheus.CounterVec
	InvalidationDur *prometheus.HistogramVec
}

// NewAPIMetrics creates and registers all API layer metrics.
func NewAPIMetrics(reg prometheus.Registerer) *APIMetrics {
	m := &APIMetrics{
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_api_requests_total",
			Help: "Total HTTP requests to the API layer.",
		}, []string{"endpoint", "method", "status_code"}),

		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dcg_api_request_duration_seconds",
			Help:    "API request latency in seconds (p99 derivable from histogram).",
			Buckets: prometheus.DefBuckets,
		}, []string{"endpoint", "method"}),

		CacheHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_api_cache_hits_total",
			Help: "Total cache hits at the API layer.",
		}, []string{"endpoint", "invalidation_strategy"}),

		CacheMisses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_api_cache_misses_total",
			Help: "Total cache misses at the API layer.",
		}, []string{"endpoint", "invalidation_strategy"}),

		DBQueries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_api_db_queries_total",
			Help: "Total database queries issued by the API layer.",
		}, []string{"query_type"}), // query_type: get | list | update

		InvalidationDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dcg_api_invalidation_duration_seconds",
			Help:    "Time spent on cache invalidation per strategy.",
			Buckets: prometheus.DefBuckets,
		}, []string{"strategy"}),
	}

	reg.MustRegister(
		m.Requests, m.RequestDuration,
		m.CacheHits, m.CacheMisses,
		m.DBQueries, m.InvalidationDur,
	)
	return m
}
