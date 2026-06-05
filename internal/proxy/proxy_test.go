package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newMockUpstream creates a test HTTP server that returns a configurable response.
// Used to simulate the API layer in proxy tests.
func newMockUpstream(statusCode int, body string, headers map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(statusCode)
		w.Write([]byte(body))
	}))
}

// --- ResponseStore tests ---

func TestResponseStore_SetAndGet_ReturnsEntry(t *testing.T) {
	s := NewResponseStore()
	resp := &CachedHTTPResponse{
		StatusCode: 200,
		Body:       []byte(`{"id":"1"}`),
		CachedAt:   time.Now(),
		ExpiresAt:  time.Now().Add(5 * time.Minute),
	}
	s.Set("/products/1", resp)

	got, ok := s.Get("/products/1")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if string(got.Body) != string(resp.Body) {
		t.Errorf("expected body %s, got %s", resp.Body, got.Body)
	}
}

func TestResponseStore_Get_MissingKey_ReturnsFalse(t *testing.T) {
	s := NewResponseStore()
	_, ok := s.Get("/products/nonexistent")
	if ok {
		t.Error("expected ok=false for missing key")
	}
}

func TestResponseStore_Get_ExpiredEntry_ReturnsFalse(t *testing.T) {
	s := NewResponseStore()
	resp := &CachedHTTPResponse{
		StatusCode: 200,
		Body:       []byte("data"),
		CachedAt:   time.Now().Add(-10 * time.Minute),
		ExpiresAt:  time.Now().Add(-1 * time.Millisecond), // already expired
	}
	s.Set("/products/expired", resp)

	_, ok := s.Get("/products/expired")
	if ok {
		t.Error("expected ok=false for expired entry")
	}
}

func TestResponseStore_Delete_RemovesEntry(t *testing.T) {
	s := NewResponseStore()
	s.Set("/k", &CachedHTTPResponse{Body: []byte("v"), ExpiresAt: time.Now().Add(time.Minute)})
	s.Delete("/k")
	_, ok := s.Get("/k")
	if ok {
		t.Error("expected ok=false after delete")
	}
}

// --- Proxy tests ---

func TestProxy_CacheMiss_ForwardsToUpstream(t *testing.T) {
	upstream := newMockUpstream(200, `{"id":"1"}`, map[string]string{
		"Content-Type":  "application/json",
		"Cache-Control": "max-age=300",
	})
	defer upstream.Close()

	p := NewProxy(upstream.URL)

	// First request — should be a cache miss, forwarded to upstream
	req := httptest.NewRequest("GET", "/products/1", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"id":"1"}` {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestProxy_CacheHit_DoesNotCallUpstream(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Cache-Control", "max-age=300")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"1"}`))
	}))
	defer upstream.Close()

	p := NewProxy(upstream.URL)

	// First request — populates cache
	req1 := httptest.NewRequest("GET", "/products/1", nil)
	p.ServeHTTP(httptest.NewRecorder(), req1)

	// Second request — should hit cache, NOT call upstream
	req2 := httptest.NewRequest("GET", "/products/1", nil)
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)

	if callCount != 1 {
		t.Errorf("upstream should be called once (cache miss), got %d calls", callCount)
	}
	if w2.Header().Get("X-Proxy-Cache") != "HIT" {
		t.Error("expected X-Proxy-Cache: HIT on second request")
	}
}

func TestProxy_NoStore_NotCached(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"1"}`))
	}))
	defer upstream.Close()

	p := NewProxy(upstream.URL)

	// Both requests should hit upstream — no-store means never cache
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/products/1", nil))
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/products/1", nil))

	if callCount != 2 {
		t.Errorf("expected 2 upstream calls with no-store, got %d", callCount)
	}
}

func TestProxy_NoCache_NotCached(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(200)
		w.Write([]byte("data"))
	}))
	defer upstream.Close()

	p := NewProxy(upstream.URL)
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	if callCount != 2 {
		t.Errorf("expected 2 upstream calls with no-cache, got %d", callCount)
	}
}

func TestProxy_MaxAgeZero_NotCached(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Cache-Control", "max-age=0")
		w.WriteHeader(200)
		w.Write([]byte("data"))
	}))
	defer upstream.Close()

	p := NewProxy(upstream.URL)
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	if callCount != 2 {
		t.Errorf("expected 2 upstream calls with max-age=0, got %d", callCount)
	}
}

func TestProxy_DifferentQueryStrings_SeparateCacheEntries(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Cache-Control", "max-age=300")
		w.WriteHeader(200)
		w.Write([]byte("data"))
	}))
	defer upstream.Close()

	p := NewProxy(upstream.URL)

	// Two requests with different query strings — different cache entries
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/products?page=1", nil))
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/products?page=2", nil))

	if callCount != 2 {
		t.Errorf("different query strings should be separate cache entries, got %d calls", callCount)
	}
}

// --- parseCacheControl tests ---

func TestProxy_404Response_NotCached(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Cache-Control", "max-age=300")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer upstream.Close()

	p := NewProxy(upstream.URL)

	// Both requests should hit upstream — 404s must never be cached
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/products/missing", nil))
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/products/missing", nil))

	if callCount != 2 {
		t.Errorf("404 responses must not be cached — expected 2 upstream calls, got %d", callCount)
	}
}

func TestParseCacheControl_MaxAge_ReturnsTTL(t *testing.T) {
	shouldCache, ttl := parseCacheControl("max-age=60")
	if !shouldCache {
		t.Error("expected shouldCache=true for max-age=60")
	}
	if ttl != 60*time.Second {
		t.Errorf("expected 60s TTL, got %v", ttl)
	}
}

func TestParseCacheControl_NoStore_ReturnsFalse(t *testing.T) {
	shouldCache, _ := parseCacheControl("no-store")
	if shouldCache {
		t.Error("expected shouldCache=false for no-store")
	}
}

func TestParseCacheControl_Empty_ReturnsDefaultTTL(t *testing.T) {
	shouldCache, ttl := parseCacheControl("")
	if !shouldCache {
		t.Error("expected shouldCache=true for empty header")
	}
	if ttl != defaultCacheTTL {
		t.Errorf("expected default TTL, got %v", ttl)
	}
}
