# Load Balancer Caching

> **Status**: Conceptual documentation only — not implemented as a running service.
> See the [layer status table in README.md](../README.md#layer-status) for the full picture.

---

## What is a Load Balancer?

A **load balancer** distributes incoming traffic across multiple backend servers. When you have 10 API server instances behind `api.example.com`, the load balancer decides which instance handles each request — round-robin, least connections, IP hash, etc.

```
Client → Load Balancer → API Instance 1
                       → API Instance 2
                       → API Instance 3
```

There are two kinds:

- **Layer 4 (L4) LB** — operates at the TCP/IP level. Sees source IP, destination port, but not HTTP content. Fast but dumb — it just routes packets.
- **Layer 7 (L7) LB** — operates at the HTTP level. Sees URLs, headers, cookies, and response bodies. Can make intelligent routing decisions — and can cache responses.

---

## How Layer-7 Load Balancers Can Cache Responses

An L7 LB like **Nginx**, **HAProxy**, **AWS ALB**, or **Envoy** can inspect HTTP responses and cache them — exactly like our reverse proxy. The difference is *where* this caching happens: at the infrastructure boundary, before the request even reaches your application servers.

Example Nginx L7 caching config:
```nginx
proxy_cache_path /tmp/nginx_cache levels=1:2 keys_zone=my_cache:10m;

server {
    location /products/ {
        proxy_cache my_cache;
        proxy_cache_valid 200 5m;   # cache 200 responses for 5 minutes
        proxy_pass http://api_backends;
    }
}
```

With this config, a request for `GET /products/prod-001` is served from Nginx's cache after the first hit. The API servers never see the request.

---

## Connection-Level vs Response-Level Caching

These are two fundamentally different things that both happen at the LB layer:

### Connection-Level Caching (Keep-Alive / Connection Pooling)
- The LB maintains **persistent TCP connections** to backend servers instead of opening a new connection per request
- Opening a TCP connection costs ~1-3ms (3-way handshake). A pool of 100 persistent connections saves this cost for every request
- This is not about caching *responses* — it's about caching *connections*
- All modern LBs do this by default (`Connection: keep-alive` HTTP header)

### Response-Level Caching
- The LB stores a copy of the **HTTP response body** and serves it to future requests for the same URL
- This is what we implemented in our reverse proxy (Task 14)
- Only L7 LBs can do this — L4 LBs don't understand HTTP and can't inspect response bodies
- Dramatically reduces load on backend servers for cacheable responses

---

## Comparison: Our Reverse Proxy vs L7 Load Balancer Caching

| Dimension | Our Reverse Proxy | L7 Load Balancer |
|---|---|---|
| Implementation | Go HTTP server (Task 14) | Nginx/HAProxy/Envoy/ALB |
| Cache storage | In-memory (Go map) | Disk or in-memory |
| Scale | Single instance | Distributed across LB nodes |
| Domain knowledge | None (caches any response) | None (caches any response) |
| Cache-Control support | Yes (implemented in Task 14) | Yes (native support) |
| Routing | Forwards to one upstream | Load-balances across N upstreams |
| Production use | Learning sandbox | Real production systems |

**The core behaviour is identical**: both cache full HTTP responses keyed by URL and respect `Cache-Control` headers. The difference is scale, reliability, and deployment context.

---

## Where Each Caching Layer Sits in a Real Production System

```
Internet
    ↓
CDN (Cloudflare / CloudFront)     ← Response caching at edge, globally
    ↓
L7 Load Balancer (Nginx / ALB)    ← Response caching + connection pooling
    ↓
[Our Reverse Proxy]               ← Response caching (Task 14, this project)
    ↓
API Layer                         ← Cache-aside with in-memory cache (Tasks 11-12)
    ↓
Cache Cluster (Redis / our impl)  ← In-memory key-value store (Tasks 5-8)
    ↓
Database (PostgreSQL)             ← Source of truth (Task 10)
```

Each layer protects the one below it. A request that hits the CDN never reaches the LB. A request that hits the LB cache never reaches the API. And so on down the stack.

---

## Why We Didn't Implement an L7 LB in This Project

An L7 LB requires multiple backend instances to be meaningful — you need something to balance *between*. In our sandbox, we have one API instance. Adding Nginx in front of it would add infrastructure complexity without teaching you anything new about caching — our reverse proxy (Task 14) already demonstrates the same HTTP response caching pattern at the conceptual level.

If you extend this project to run multiple API instances, dropping Nginx in front with `proxy_cache` enabled is the natural next step.
