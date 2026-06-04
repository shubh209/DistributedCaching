package api

import (
	"time"
)

// Product is the API-layer representation of a catalog product.
// It is returned in all GET responses and after successful PUT updates.
type Product struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceUSD    float64   `json:"price_usd"`
	Category    string    `json:"category"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProductUpdateRequest is the request body for PUT /products/{id}.
// All fields are pointers — nil means "don't change this field".
// This gives us partial update semantics: only provided fields are written.
type ProductUpdateRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	PriceUSD    *float64 `json:"price_usd"`
	Category    *string  `json:"category"`
}

// ProductListResponse is the response body for GET /products.
// Includes pagination metadata alongside the product slice.
type ProductListResponse struct {
	Products   []Product `json:"products"`
	Page       int       `json:"page"`
	PageSize   int       `json:"page_size"`
	TotalCount int       `json:"total_count"`
}
