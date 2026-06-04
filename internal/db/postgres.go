package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a product doesn't exist in the database.
// The API layer maps this to HTTP 404.
var ErrNotFound = errors.New("db: product not found")

// Product is the domain model for a product catalog entry.
// It mirrors the products table schema exactly.
type Product struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceUSD    float64   `json:"price_usd"`
	Category    string    `json:"category"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProductUpdateRequest holds the fields that can be updated.
// All fields are pointers — nil means "don't change this field".
// This implements a partial update (PATCH semantics over PUT).
type ProductUpdateRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	PriceUSD    *float64 `json:"price_usd"`
	Category    *string  `json:"category"`
}

// DB wraps a pgx connection pool and exposes the queries the API layer needs.
// Using a pool means we never open a new connection per request — connections
// are reused, keeping latency low.
type DB struct {
	pool *pgxpool.Pool
}

// New creates a DB connected to the given PostgreSQL DSN.
// The DSN format is: postgres://user:pass@host:port/dbname?sslmode=disable
//
// We also run the schema migration here so the service is always ready
// when it starts — no separate migration step needed in Docker Compose.
func New(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}

	// Verify the connection is actually working
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	db := &DB{pool: pool}

	// Run schema migration on startup
	if err := db.migrate(ctx); err != nil {
		return nil, fmt.Errorf("db: migrate: %w", err)
	}

	return db, nil
}

// Close releases all connections in the pool.
// Should be called when the service shuts down.
func (db *DB) Close() {
	db.pool.Close()
}

// migrate runs the schema migration inline.
// In a production system you'd use a migration tool like golang-migrate.
// For this sandbox, embedding the SQL keeps things simple.
func (db *DB) migrate(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS products (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		price_usd   NUMERIC(10, 2) NOT NULL DEFAULT 0.00,
		category    TEXT NOT NULL DEFAULT '',
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);
	`
	_, err := db.pool.Exec(ctx, sql)
	return err
}

// GetProduct fetches a single product by ID.
// Returns ErrNotFound if the product doesn't exist — the API maps this to 404.
func (db *DB) GetProduct(ctx context.Context, id string) (*Product, error) {
	row := db.pool.QueryRow(ctx, `
		SELECT id, name, description, price_usd, category, created_at, updated_at
		FROM products
		WHERE id = $1
	`, id)

	p := &Product{}
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.PriceUSD, &p.Category, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get product %s: %w", id, err)
	}
	return p, nil
}

// ListProducts returns a paginated list of products ordered by created_at DESC.
// page is 1-indexed. pageSize is capped at 100 by the API layer before calling this.
// Returns the products for the requested page and the total count of all products.
func (db *DB) ListProducts(ctx context.Context, page, pageSize int) ([]Product, int, error) {
	// Get total count first (for pagination metadata)
	var total int
	err := db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM products`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("db: count products: %w", err)
	}

	// Calculate offset for pagination
	// page=1 → offset=0, page=2 → offset=pageSize, etc.
	offset := (page - 1) * pageSize

	rows, err := db.pool.Query(ctx, `
		SELECT id, name, description, price_usd, category, created_at, updated_at
		FROM products
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("db: list products: %w", err)
	}
	defer rows.Close()

	products := make([]Product, 0, pageSize)
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.PriceUSD, &p.Category, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("db: scan product: %w", err)
		}
		products = append(products, p)
	}

	return products, total, rows.Err()
}

// UpdateProduct partially updates a product — only non-nil fields are changed.
// Returns ErrNotFound if the product doesn't exist.
// Returns the updated product so the caller can populate the cache with fresh data.
func (db *DB) UpdateProduct(ctx context.Context, id string, req ProductUpdateRequest) (*Product, error) {
	// Build the SET clause dynamically based on which fields are provided.
	// This avoids overwriting fields the caller didn't intend to change.
	setClauses := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *req.Description)
		argIdx++
	}
	if req.PriceUSD != nil {
		setClauses = append(setClauses, fmt.Sprintf("price_usd = $%d", argIdx))
		args = append(args, *req.PriceUSD)
		argIdx++
	}
	if req.Category != nil {
		setClauses = append(setClauses, fmt.Sprintf("category = $%d", argIdx))
		args = append(args, *req.Category)
		argIdx++
	}

	// Add the id as the final argument for the WHERE clause
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE products
		SET %s
		WHERE id = $%d
		RETURNING id, name, description, price_usd, category, created_at, updated_at
	`, joinClauses(setClauses), argIdx)

	row := db.pool.QueryRow(ctx, query, args...)

	p := &Product{}
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.PriceUSD, &p.Category, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: update product %s: %w", id, err)
	}
	return p, nil
}

// joinClauses joins SET clause strings with ", ".
// e.g. ["name = $1", "price_usd = $2", "updated_at = NOW()"]
// → "name = $1, price_usd = $2, updated_at = NOW()"
func joinClauses(clauses []string) string {
	result := ""
	for i, c := range clauses {
		if i > 0 {
			result += ", "
		}
		result += c
	}
	return result
}
