# DNS Caching

> **Status**: Conceptual documentation only — not implemented as a running service.
> See the [layer status table in README.md](../README.md#layer-status) for the full picture.

---

## What is DNS?

Every time your browser visits `api.example.com`, it needs to find the IP address for that hostname. The **Domain Name System (DNS)** is the internet's phone book — it translates human-readable hostnames into IP addresses.

The lookup chain looks like this:

```
Browser → Local OS cache → Recursive resolver (ISP/Google/Cloudflare)
       → Root nameserver → TLD nameserver (.com)
       → Authoritative nameserver (example.com) → IP address
```

This full chain can take 50–200ms. DNS caching exists to short-circuit it.

---

## How TTL Works in DNS

Every DNS record has a **TTL (Time To Live)** — a number of seconds that tells resolvers how long to cache the answer.

```
api.example.com.  300  IN  A  203.0.113.42
                  ^^^
                  TTL = 300 seconds = 5 minutes
```

When a resolver gets this record, it caches the IP address for 300 seconds. For the next 5 minutes, any request for `api.example.com` returns the cached IP without going upstream. After 300 seconds, the cache expires and the resolver fetches a fresh answer.

---

## How Recursive Resolvers Cache Records

A **recursive resolver** (like `8.8.8.8` from Google or `1.1.1.1` from Cloudflare) does the work of walking the DNS tree on your behalf. It also caches answers aggressively:

1. **OS-level cache**: Your operating system caches DNS responses locally. `nscd` on Linux, the DNS cache on macOS, or the Windows DNS Client service.
2. **Resolver-level cache**: Your ISP's resolver or a public resolver like Google/Cloudflare caches records globally across all their users. If 1 million people query `google.com`, the resolver only needs to ask the authoritative server once per TTL window.
3. **Browser cache**: Chrome, Firefox, and Safari all maintain their own DNS caches independently of the OS.

Each layer has its own TTL tracking. The record expires from each cache independently when its TTL elapses.

---

## Trade-offs: Short vs Long DNS TTLs

### Short TTL (e.g. 30–60 seconds)

**Pros:**
- Changes propagate quickly — if you update an IP address, clients see the new IP within 60 seconds
- Useful for blue-green deployments, failover, and traffic shifting

**Cons:**
- More DNS queries — every 60 seconds, every resolver re-fetches the record
- Higher load on authoritative nameservers
- Slightly higher latency for the first request after expiry

**Use when:** You need rapid failover, are doing traffic migration, or run a geo-distributed service with frequent IP changes.

### Long TTL (e.g. 3600–86400 seconds)

**Pros:**
- Fewer DNS queries — records are cached for hours or days
- Lower load on nameservers
- Near-zero DNS lookup latency for most requests (always cached)

**Cons:**
- Changes take hours to propagate — if your server IP changes, old clients keep hitting the old IP until TTL expires
- "DNS propagation delay" is this phenomenon — it's a direct consequence of long TTLs

**Use when:** Your infrastructure is stable, IPs rarely change, and you want minimal DNS overhead.

### The practical sweet spot

Most production services use **300–900 seconds (5–15 minutes)**. This balances propagation speed with query volume. Services that need rapid failover (like Netflix, Cloudflare) use **30–60 seconds** with sophisticated health-checking to compensate for the higher query load.

---

## How DNS Caching Differs from Application-Level Caching

| Dimension | DNS Caching | Application Caching (this project) |
|---|---|---|
| What is cached | IP address → hostname mappings | Business data (products, users, etc.) |
| Who controls TTL | DNS record owner (infra team) | Application developer |
| Invalidation | TTL only — no manual purge possible | TTL + write-through + write-behind |
| Scope | Global (ISPs, CDNs, OS, browser) | Per-service (your cache cluster) |
| Consistency | Eventual (propagates over TTL window) | Configurable (strong with write-through) |
| Cache miss cost | ~50-200ms DNS round-trip | ~10-50ms DB query |

The key difference: DNS caching is **passive and TTL-only**. You cannot push an invalidation to all resolvers in the world — you can only set a short TTL and wait. Application caches like ours give you much finer control.

---

## DNS Caching in Our System

Our `docker-compose.yml` services communicate by hostname (`cache-node-1`, `api`, `postgres`, etc.). Docker's internal DNS resolves these names to container IP addresses. The TTL for Docker's internal DNS is very short (seconds), which is why service discovery works reliably even when containers restart and get new IPs.

This is DNS caching working at the infrastructure layer — invisible but essential.
