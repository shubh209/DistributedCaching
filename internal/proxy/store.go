package proxy

import (
	"net/http"
	"sync"
	"time"
)

// CachedHTTPResponse holds a complete HTTP response captured from the upstream.
// The full response — status, headers, and body — is stored so it can be
// replayed to future clients without contacting upstream again.
type CachedHTTPResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	CachedAt   time.Time
	ExpiresAt  time.Time // derived from Cache-Control max-age; zero = no expiry set
}

// IsExpired returns true if this cached response has passed its max-age.
func (r *CachedHTTPResponse) IsExpired(now time.Time) bool {
	return !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt)
}

// ResponseStore is a thread-safe in-memory store for cached HTTP responses.
// Cache key = full request URL including query string.
// e.g. "/products/prod-001" and "/products/prod-001?format=json" are separate entries.
type ResponseStore struct {
	mu      sync.RWMutex
	entries map[string]*CachedHTTPResponse
}

// NewResponseStore creates an empty ResponseStore.
func NewResponseStore() *ResponseStore {
	return &ResponseStore{
		entries: make(map[string]*CachedHTTPResponse),
	}
}

// Get retrieves a cached response by URL key.
// Returns (nil, false) on a miss or if the entry has expired (lazy expiry).
func (s *ResponseStore) Get(urlKey string) (*CachedHTTPResponse, bool) {
	s.mu.RLock()
	entry, ok := s.entries[urlKey]
	s.mu.RUnlock()

	if !ok {
		return nil, false
	}

	// Lazy expiry — check TTL on access, not on a background timer
	if entry.IsExpired(time.Now()) {
		s.mu.Lock()
		// Re-check under write lock (another goroutine may have already deleted)
		if e, exists := s.entries[urlKey]; exists && e.IsExpired(time.Now()) {
			delete(s.entries, urlKey)
		}
		s.mu.Unlock()
		return nil, false
	}

	return entry, true
}

// Set stores a cached response for the given URL key.
// Overwrites any existing entry for the same key.
func (s *ResponseStore) Set(urlKey string, resp *CachedHTTPResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[urlKey] = resp
}

// Delete removes a cached response for the given URL key.
func (s *ResponseStore) Delete(urlKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, urlKey)
}

// Len returns the current number of cached entries (including expired ones
// not yet lazily cleaned up).
func (s *ResponseStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}
