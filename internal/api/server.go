package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/user/distributed-caching-go/internal/db"
	"github.com/user/distributed-caching-go/internal/shared"
)

// Server is the HTTP API server for the product catalog.
type Server struct {
	handler *Handler
	httpSrv *http.Server
}

// NewServer creates and configures the API HTTP server.
// It wires together the cache, database, and invalidation strategy,
// then registers all routes.
func NewServer(port int, cache shared.Cache, database *db.DB, strategy shared.InvalidationStrategy) *Server {
	ca := NewCacheAside(cache, database, strategy)
	h := NewHandler(ca)

	mux := http.NewServeMux()

	// Product catalog routes (Go 1.22+ pattern matching)
	mux.HandleFunc("GET /products/{id}", h.GetProduct)
	mux.HandleFunc("GET /products", h.ListProducts)
	mux.HandleFunc("PUT /products/{id}", h.UpdateProduct)

	// Health and metrics endpoints
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"healthy":true,"service":"api"}`)
	})

	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		// Prometheus metrics — wired in Task 15
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "# api metrics — prometheus instrumentation added in task 15\n")
	})

	return &Server{
		handler: h,
		httpSrv: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
	}
}

// Start begins serving HTTP requests. Blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.httpSrv.Shutdown(context.Background())
	}
}
