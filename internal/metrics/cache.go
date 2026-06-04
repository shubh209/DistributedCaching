package metrics

import "github.com/prometheus/client_golang/prometheus"

// CacheMetrics holds all Prometheus metrics for a cache node.
// Labels allow all 4 eviction policies to be compared on the same Grafana panel.
type CacheMetrics struct {
	Hits      *prometheus.CounterVec
	Misses    *prometheus.CounterVec
	Evictions *prometheus.CounterVec
	Entries   *prometheus.GaugeVec
	Routed    *prometheus.CounterVec
	OpDuration *prometheus.HistogramVec
}

// NewCacheMetrics creates and registers all cache node metrics.
// nodeID identifies the specific node (e.g. "node1").
func NewCacheMetrics(reg prometheus.Registerer) *CacheMetrics {
	m := &CacheMetrics{
		Hits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_cache_hits_total",
			Help: "Total number of cache hits.",
		}, []string{"node_id", "eviction_policy"}),

		Misses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_cache_misses_total",
			Help: "Total number of cache misses.",
		}, []string{"node_id", "eviction_policy"}),

		Evictions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_cache_evictions_total",
			Help: "Total number of cache evictions.",
		}, []string{"node_id", "eviction_policy"}),

		Entries: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dcg_cache_entries",
			Help: "Current number of entries in the cache.",
		}, []string{"node_id", "eviction_policy"}),

		Routed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dcg_cache_routed_requests_total",
			Help: "Total requests routed to this node by the coordinator.",
		}, []string{"node_id"}),

		OpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dcg_cache_operation_duration_seconds",
			Help:    "Cache operation latency in seconds.",
			Buckets: prometheus.DefBuckets, // .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10
		}, []string{"node_id", "operation"}), // operation: get | set | delete
	}

	reg.MustRegister(m.Hits, m.Misses, m.Evictions, m.Entries, m.Routed, m.OpDuration)
	return m
}
