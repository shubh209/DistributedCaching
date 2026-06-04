package proxy

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// upstreamTimeout is how long the proxy waits for the upstream API layer.
	// Per requirement 7.5: serve stale or return 503 if upstream doesn't respond within 30s.
	upstreamTimeout = 30 * time.Second

	// defaultCacheTTL is used when the upstream response has no Cache-Control header.
	defaultCacheTTL = 5 * time.Minute
)

// Proxy is an HTTP reverse proxy that caches full responses by URL.
// It has no domain knowledge — it caches any response regardless of content.
type Proxy struct {
	store       *ResponseStore
	upstreamURL string       // base URL of the API layer, e.g. "http://api:8081"
	client      *http.Client // HTTP client with timeout
}

// NewProxy creates a Proxy that forwards cache misses to the given upstream URL.
func NewProxy(upstreamURL string) *Proxy {
	return &Proxy{
		store:       NewResponseStore(),
		upstreamURL: strings.TrimRight(upstreamURL, "/"),
		client:      &http.Client{Timeout: upstreamTimeout},
	}
}

// ServeHTTP implements http.Handler — this is the main proxy entry point.
//
// For every incoming request:
//  1. Build a cache key from the full URL including query string
//  2. Check the store — serve from cache on HIT
//  3. On MISS — forward to upstream, cache if allowed, return response
//  4. On upstream timeout — serve stale or return 503
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Cache key = path + query string (full URL uniquely identifies the response)
	cacheKey := r.URL.RequestURI() // e.g. "/products/prod-001?page=1"

	// Step 1: Check cache
	if cached, ok := p.store.Get(cacheKey); ok {
		p.serveCached(w, cached)
		return
	}

	// Step 2: Forward to upstream
	upstreamResp, err := p.forwardToUpstream(r)
	if err != nil {
		// Upstream unreachable — serve stale if available, otherwise 503
		if stale, ok := p.store.Get(cacheKey); ok {
			p.serveCached(w, stale)
			return
		}
		http.Error(w, "upstream unavailable and no cached response available", http.StatusServiceUnavailable)
		return
	}
	defer upstreamResp.Body.Close()

	// Step 3: Read full response body
	body, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	// Step 4: Determine if this response should be cached
	cacheControl := upstreamResp.Header.Get("Cache-Control")
	shouldCache, ttl := parseCacheControl(cacheControl)

	if shouldCache && upstreamResp.StatusCode < 500 {
		// Don't cache error responses from upstream — only cache successful ones
		cached := &CachedHTTPResponse{
			StatusCode: upstreamResp.StatusCode,
			Headers:    upstreamResp.Header.Clone(),
			Body:       body,
			CachedAt:   time.Now(),
		}
		if ttl > 0 {
			cached.ExpiresAt = time.Now().Add(ttl)
		}
		p.store.Set(cacheKey, cached)
	}

	// Step 5: Write response to client
	copyHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	w.Write(body)
}

// serveCached writes a cached response back to the client.
func (p *Proxy) serveCached(w http.ResponseWriter, cached *CachedHTTPResponse) {
	copyHeaders(w.Header(), cached.Headers)
	w.Header().Set("X-Proxy-Cache", "HIT")
	w.WriteHeader(cached.StatusCode)
	w.Write(cached.Body)
}

// forwardToUpstream sends the request to the upstream API layer.
func (p *Proxy) forwardToUpstream(r *http.Request) (*http.Response, error) {
	// Build upstream URL: proxy base URL + original path + query
	upstreamURL := p.upstreamURL + r.URL.RequestURI()

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
	if err != nil {
		return nil, fmt.Errorf("proxy: build upstream request: %w", err)
	}

	// Copy request headers (e.g. Content-Type, Authorization)
	for key, values := range r.Header {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}

	return p.client.Do(req)
}

// parseCacheControl parses the Cache-Control header and returns:
//   - shouldCache: false if no-store, no-cache, or private directive present
//   - ttl: duration from max-age directive; 0 if not set or max-age=0
//
// Per requirements 7.6, 7.7, 7.8.
func parseCacheControl(header string) (shouldCache bool, ttl time.Duration) {
	if header == "" {
		return true, defaultCacheTTL
	}

	// Directives that prevent caching
	lower := strings.ToLower(header)
	for _, directive := range []string{"no-store", "no-cache", "private"} {
		if strings.Contains(lower, directive) {
			return false, 0
		}
	}

	// Parse max-age=N
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "max-age=") {
			val := strings.TrimPrefix(strings.ToLower(part), "max-age=")
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil {
				continue
			}
			if n == 0 {
				// max-age=0: forward request, return response, do NOT cache
				return false, 0
			}
			return true, time.Duration(n) * time.Second
		}
	}

	// No max-age directive — use default TTL
	return true, defaultCacheTTL
}

// copyHeaders copies response headers from src to dst.
func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}

// Store returns the underlying ResponseStore (used by the /metrics handler).
func (p *Proxy) Store() *ResponseStore {
	return p.store
}

// Handler returns this proxy as an http.Handler for use with http.ListenAndServe.
func (p *Proxy) Handler() http.Handler {
	mux := http.NewServeMux()

	// All product catalog traffic passes through the proxy handler
	mux.Handle("/", p)

	// Health check — proxy itself is healthy if it can respond
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"healthy":true,"cached_entries":%d}`, p.store.Len())
	})

	// Metrics stub — wired to Prometheus in Task 15
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "# proxy metrics — prometheus instrumentation added in task 15\n")
		fmt.Fprintf(w, "proxy_cached_entries %d\n", p.store.Len())
	})

	return mux
}
