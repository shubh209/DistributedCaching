package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRegistry creates a fresh Prometheus registry.
// Using a custom registry (instead of the global default) means each
// service has its own isolated metric namespace — important for tests
// where multiple registries might be created in the same process.
func NewRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// Handler returns an HTTP handler that serves the Prometheus metrics
// for the given registry in the standard exposition format.
// Wire this to the /metrics route in each service.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: false, // standard Prometheus text format
	})
}
