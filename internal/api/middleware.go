package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/user/distributed-caching-go/internal/db"
	"github.com/user/distributed-caching-go/internal/shared"
)

const (
	// defaultProductTTL is how long a product stays in cache before expiring.
	// 5 minutes is a reasonable balance: fresh enough for a product catalog,
	// long enough to absorb repeated reads of the same product.
	defaultProductTTL = 5 * time.Minute

	// xCacheHeader is the response header name that indicates cache status.
	// Standard convention used by Nginx, Varnish, and CDNs.
	xCacheHeader = "X-Cache"

	// cacheKeyPrefix is prepended to all product cache keys.
	// Using a prefix prevents key collisions if other data is ever cached.
	// e.g. "product:prod-001"
	cacheKeyPrefix = "product:"
)

// CacheAside wraps the cache-aside logic for product lookups.
// It is used by the HTTP handlers to check the cache before hitting the DB,
// and to populate the cache on misses.
type CacheAside struct {
	cache    shared.Cache
	db       *db.DB
	strategy shared.InvalidationStrategy
}

// NewCacheAside creates a CacheAside with the given cache, db, and strategy.
func NewCacheAside(cache shared.Cache, database *db.DB, strategy shared.InvalidationStrategy) *CacheAside {
	return &CacheAside{
		cache:    cache,
		db:       database,
		strategy: strategy,
	}
}

// GetProduct implements the cache-aside read path:
//  1. Check cache — return immediately on HIT
//  2. On MISS — fetch from DB, populate cache, return result
//
// Returns:
//   - (product, "HIT", nil)  — served from cache
//   - (product, "MISS", nil) — served from DB, cache now populated
//   - (nil, "", ErrNotFound) — product doesn't exist in DB → HTTP 404
//   - (nil, "", err)         — DB unavailable → HTTP 503
func (ca *CacheAside) GetProduct(ctx context.Context, id string) (*Product, string, error) {
	key := cacheKeyPrefix + id

	// Step 1: Check cache
	value, found, err := ca.cache.Get(ctx, key)
	if err != nil {
		// Cache error is non-fatal — fall through to DB
		// (e.g. coordinator unavailable — treat as miss)
		found = false
	}

	if found {
		// Cache HIT — deserialise and return
		var p Product
		if err := json.Unmarshal(value, &p); err != nil {
			// Corrupt cache entry — treat as miss
			_ = ca.cache.Delete(ctx, key)
			found = false
		} else {
			return &p, "HIT", nil
		}
	}

	// Step 2: Cache MISS — fetch from database
	dbProduct, err := ca.db.GetProduct(ctx, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, "", db.ErrNotFound // caller returns HTTP 404
	}
	if err != nil {
		return nil, "", fmt.Errorf("api: db unavailable: %w", err) // caller returns HTTP 503
	}

	// Convert db.Product → api.Product
	product := dbToAPIProduct(dbProduct)

	// Step 3: Populate cache — serialise and store with TTL
	// Per requirement 8.7: do NOT populate cache if DB returned an error.
	// We only reach here on a successful DB fetch.
	serialised, err := json.Marshal(product)
	if err == nil {
		// Cache Set errors are non-fatal — the response still goes out
		_ = ca.cache.Set(ctx, key, serialised, defaultProductTTL)
	}

	return product, "MISS", nil
}

// UpdateProduct implements the cache-aside write path:
//  1. Write to DB (source of truth first)
//  2. Invoke the active InvalidationStrategy to handle the cache
//
// The strategy decides what happens to the cache entry:
//   - TTL:          no-op, stale data served until TTL expires
//   - Write-through: synchronously update cache with new value
//   - Write-behind:  async cache invalidation within 5 seconds
//
// Per requirement 6.2: even if the cache update fails, the DB write
// is already committed and we return the updated product.
func (ca *CacheAside) UpdateProduct(ctx context.Context, id string, req ProductUpdateRequest) (*Product, error) {
	// Convert api.ProductUpdateRequest → db.ProductUpdateRequest
	dbReq := db.ProductUpdateRequest{
		Name:        req.Name,
		Description: req.Description,
		PriceUSD:    req.PriceUSD,
		Category:    req.Category,
	}

	// Step 1: Write to source of truth
	dbProduct, err := ca.db.UpdateProduct(ctx, id, dbReq)
	if errors.Is(err, db.ErrNotFound) {
		return nil, db.ErrNotFound // caller returns HTTP 404
	}
	if err != nil {
		return nil, fmt.Errorf("api: db update failed: %w", err)
	}

	product := dbToAPIProduct(dbProduct)

	// Step 2: Invoke invalidation strategy
	key := cacheKeyPrefix + id
	serialised, _ := json.Marshal(product)
	// Strategy errors are non-fatal — DB write already committed
	_ = ca.strategy.OnWrite(ctx, ca.cache, key, serialised)

	return product, nil
}

// dbToAPIProduct converts a db.Product to an api.Product.
// Keeps the db and api packages decoupled — neither imports the other's types.
func dbToAPIProduct(p *db.Product) *Product {
	return &Product{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		PriceUSD:    p.PriceUSD,
		Category:    p.Category,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}
