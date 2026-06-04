-- Migration 001: Create products table
-- This is the source of truth for the product catalog.
-- The cache sits in front of this table — all cached values originate here.

CREATE TABLE IF NOT EXISTS products (
    -- String ID (not auto-increment int) — more realistic for distributed systems.
    -- String IDs map directly to cache keys (e.g. "product:prod-123").
    id          TEXT PRIMARY KEY,

    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',

    -- NUMERIC(10,2) = exact decimal with up to 10 digits, 2 after the decimal point.
    -- NEVER use FLOAT for money — floats have rounding errors.
    -- e.g. 0.1 + 0.2 = 0.30000000000000004 in floating point.
    price_usd   NUMERIC(10, 2) NOT NULL DEFAULT 0.00,

    category    TEXT        NOT NULL DEFAULT '',

    -- TIMESTAMPTZ = timestamp WITH timezone.
    -- Always store timestamps with timezone to avoid ambiguity across regions.
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index on category for filtered list queries (e.g. GET /products?category=electronics).
-- Without this index, listing by category would require a full table scan.
CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);

-- Seed some sample products so the sandbox has data to work with immediately.
INSERT INTO products (id, name, description, price_usd, category) VALUES
    ('prod-001', 'Wireless Headphones',   'Noise-cancelling over-ear headphones', 79.99,  'electronics'),
    ('prod-002', 'Mechanical Keyboard',   'TKL layout with Cherry MX switches',   129.99, 'electronics'),
    ('prod-003', 'Standing Desk Mat',     'Anti-fatigue mat for standing desks',  49.99,  'office'),
    ('prod-004', 'USB-C Hub',             '7-in-1 USB-C hub with HDMI and PD',    39.99,  'electronics'),
    ('prod-005', 'Notebook (A5)',         'Dotted grid, 200 pages',               12.99,  'office'),
    ('prod-006', 'Laptop Backpack',       'Water-resistant 30L backpack',         89.99,  'bags'),
    ('prod-007', 'Monitor Light Bar',     'Screenbar clip-on monitor light',      59.99,  'electronics'),
    ('prod-008', 'Cable Management Box', 'Hide power strips and cables cleanly',  24.99,  'office'),
    ('prod-009', 'Webcam 1080p',          'Autofocus webcam with privacy cover',  69.99,  'electronics'),
    ('prod-010', 'Desk Plant (Small)',    'Low-maintenance succulent',             14.99,  'decor')
ON CONFLICT (id) DO NOTHING; -- idempotent: safe to run multiple times
