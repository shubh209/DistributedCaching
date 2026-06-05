package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/user/distributed-caching-go/internal/db"
)

// Handler holds all dependencies needed by the HTTP handlers.
type Handler struct {
	cacheAside *CacheAside
}

// NewHandler creates a Handler with the given CacheAside.
func NewHandler(ca *CacheAside) *Handler {
	return &Handler{cacheAside: ca}
}

// GetProduct handles GET /products/{id}
//
// Response headers:
//   - X-Cache: HIT  — product served from cache
//   - X-Cache: MISS — product served from database
//
// Status codes:
//   - 200 — product found and returned
//   - 404 — product not found
//   - 500 — X-Cache header could not be set (per requirement 5.6)
//   - 503 — database unavailable on cache miss
func (h *Handler) GetProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id") // Go 1.22+ path value extraction

	product, cacheStatus, err := h.cacheAside.GetProduct(r.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	// Set X-Cache header — per requirement 5.6, if this fails we must
	// fail the request rather than serve data without the header.
	w.Header().Set(xCacheHeader, cacheStatus)
	if w.Header().Get(xCacheHeader) != cacheStatus {
		writeError(w, http.StatusInternalServerError, "failed to set cache status header")
		return
	}

	writeJSON(w, http.StatusOK, product)
}

// ListProducts handles GET /products
//
// Query parameters:
//   - page:     1-indexed page number (default: 1)
//   - pageSize: number of results per page (default: 20, max: 100)
func (h *Handler) ListProducts(w http.ResponseWriter, r *http.Request) {
	page := queryInt(r, "page", 1)
	pageSize := queryInt(r, "pageSize", 20)

	// Validate and cap pageSize per requirement 5.2
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if page < 1 {
		page = 1
	}

	products, total, err := h.cacheAside.db.ListProducts(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	// Convert []db.Product → []api.Product
	apiProducts := make([]Product, len(products))
	for i, p := range products {
		apiProducts[i] = *dbToAPIProduct(&p)
	}

	writeJSON(w, http.StatusOK, ProductListResponse{
		Products:   apiProducts,
		Page:       page,
		PageSize:   pageSize,
		TotalCount: total,
	})
}

// CreateProduct handles POST /products
//
// Status codes:
//   - 201 — product created and returned
//   - 400 — invalid request body or missing required fields
//   - 409 — product with this ID already exists
//   - 503 — database unavailable
func (h *Handler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var p Product
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if p.ID == "" || p.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}

	dbProduct, err := h.cacheAside.db.CreateProduct(r.Context(), db.Product{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		PriceUSD:    p.PriceUSD,
		Category:    p.Category,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "product with this ID already exists")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	writeJSON(w, http.StatusCreated, dbToAPIProduct(dbProduct))
}

// isUniqueViolation returns true if the error is a PostgreSQL unique constraint violation.
func isUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "unique"))
}


//
// Status codes:
//   - 200 — product updated and returned
//   - 400 — invalid request body
//   - 404 — product not found
//   - 503 — database unavailable
func (h *Handler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req ProductUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	product, err := h.cacheAside.UpdateProduct(r.Context(), id, req)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	writeJSON(w, http.StatusOK, product)
}

// --- response helpers ---

// writeJSON serialises v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// queryInt reads an integer query parameter with a default value.
func queryInt(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return n
}
