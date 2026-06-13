"""Prefix cache: CacheEntry, LRU cache, hashing, CacheStats."""

from __future__ import annotations

import collections
import hashlib
import json
from dataclasses import dataclass, field
from typing import Sequence

from llmsim.models import ModelConfig

__all__ = [
    "CacheEntry",
    "CacheStats",
    "PrefixCache",
    "hash_prefix",
    "new_entry_for_prefix",
    "format_tflops",
]


# ---------------------------------------------------------------------------
# Hashing
# ---------------------------------------------------------------------------

def hash_prefix(tokens: Sequence[int]) -> str:
    """Return a 32-character lowercase hex string identifying a token sequence.

    Serializes *tokens* to compact JSON (matching Go's json.Marshal byte layout),
    computes SHA-256, and hex-encodes the first 16 bytes → exactly 32 hex chars.

    An empty sequence hashes ``[]`` without error (Req 3.4).
    """
    data = json.dumps(list(tokens), separators=(",", ":"))
    digest = hashlib.sha256(data.encode()).digest()
    return digest[:16].hex()


# ---------------------------------------------------------------------------
# CacheEntry
# ---------------------------------------------------------------------------

@dataclass
class CacheEntry:
    """Metadata for a cached token prefix (Req 4, 5.5).

    Not frozen — hit_count is mutated on each cache hit.
    """

    prefix_hash: str
    model: str
    token_count: int
    tflops_cost: float
    cost_usd: float
    hit_count: int = 0

    def saved_tflops(self) -> float:
        """Compute saved by caching: tflops_cost * hit_count (Req 5.5)."""
        return self.tflops_cost * self.hit_count

    def saved_usd(self) -> float:
        """Dollar savings accumulated by caching: cost_usd * hit_count."""
        return self.cost_usd * self.hit_count


# ---------------------------------------------------------------------------
# CacheStats
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class CacheStats:
    """Aggregate cache statistics snapshot (Req 5.1–5.4)."""

    total_requests: int
    cache_hits: int
    cache_misses: int
    hit_rate: float          # percentage 0..100 (Req 5.2)
    entries_stored: int
    total_saved_tflops: float
    total_saved_usd: float


# ---------------------------------------------------------------------------
# PrefixCache — LRU via collections.OrderedDict
# ---------------------------------------------------------------------------

class PrefixCache:
    """In-memory LRU prefix cache keyed by SHA-256 prefix hash (Req 4.1–4.9).

    Capacity must be >= 1 (Req 4.8).  The cache uses a ``collections.OrderedDict``
    so ``move_to_end`` and ``popitem(last=False)`` give exact O(1) LRU semantics
    without timestamp scanning.
    """

    def __init__(self, capacity: int) -> None:
        if capacity < 1:
            raise ValueError(f"PrefixCache capacity must be >= 1, got {capacity}")
        self._capacity: int = capacity
        self._entries: collections.OrderedDict[str, CacheEntry] = collections.OrderedDict()
        self._total_requests: int = 0
        self._cache_hits: int = 0
        self._total_saved_tflops: float = 0.0
        self._total_saved_usd: float = 0.0

    # ------------------------------------------------------------------
    # lookup
    # ------------------------------------------------------------------

    def lookup(self, prefix_hash: str) -> CacheEntry | None:
        """Look up an entry by its prefix hash (Req 4.1–4.4).

        Always increments ``total_requests``.
        On hit: marks the entry most-recently-used, increments ``hit_count``,
        accumulates saved totals, and returns the entry.
        On miss: returns ``None`` leaving the cache unchanged.
        """
        self._total_requests += 1

        if prefix_hash not in self._entries:
            return None

        # Cache HIT
        entry = self._entries[prefix_hash]
        self._entries.move_to_end(prefix_hash)   # mark MRU
        entry.hit_count += 1
        self._cache_hits += 1
        self._total_saved_tflops += entry.tflops_cost
        self._total_saved_usd += entry.cost_usd
        return entry

    # ------------------------------------------------------------------
    # store
    # ------------------------------------------------------------------

    def store(self, entry: CacheEntry) -> None:
        """Insert or replace an entry, evicting LRU when at capacity (Req 4.5–4.7, 4.9).

        * If the hash already exists: replace the entry and move it to MRU;
          entries-stored count is unchanged (Req 4.9).
        * If the hash is new and the cache is at capacity: evict the
          least-recently-used entry via ``popitem(last=False)`` (Req 4.6).
        * Insert the new entry at the MRU end (Req 4.5, 4.7).
        """
        h = entry.prefix_hash

        if h in self._entries:
            # Replace existing entry; move to MRU; count unchanged
            self._entries[h] = entry
            self._entries.move_to_end(h)
            return

        # New hash — evict LRU if at capacity
        if len(self._entries) >= self._capacity:
            self._entries.popitem(last=False)  # evict LRU (oldest/front)

        # Insert as MRU
        self._entries[h] = entry
        self._entries.move_to_end(h)

    # ------------------------------------------------------------------
    # stats
    # ------------------------------------------------------------------

    def stats(self) -> CacheStats:
        """Return a snapshot of aggregate cache statistics (Req 5.1–5.4)."""
        if self._total_requests > 0:
            hit_rate = self._cache_hits / self._total_requests * 100.0
        else:
            hit_rate = 0.0

        cache_misses = self._total_requests - self._cache_hits
        entries_stored = len(self._entries)

        return CacheStats(
            total_requests=self._total_requests,
            cache_hits=self._cache_hits,
            cache_misses=cache_misses,
            hit_rate=hit_rate,
            entries_stored=entries_stored,
            total_saved_tflops=self._total_saved_tflops,
            total_saved_usd=self._total_saved_usd,
        )


# ---------------------------------------------------------------------------
# Module-level helpers
# ---------------------------------------------------------------------------

def new_entry_for_prefix(tokens: Sequence[int], model: ModelConfig) -> CacheEntry:
    """Build a ``CacheEntry`` for *tokens* on *model*.

    ``len(tokens) == 0`` is valid (yields 0.0 cost).
    """
    h = hash_prefix(tokens)
    n = len(tokens)
    tflops = model.attention_tflops(n)
    cost = model.cost_usd(n)
    return CacheEntry(
        prefix_hash=h,
        model=model.name,
        token_count=n,
        tflops_cost=tflops,
        cost_usd=cost,
    )


def format_tflops(tflops: float) -> str:
    """Human-readable TFLOPs / PFLOPs string, mirroring Go's FormatTFLOPs (Req 13.4–13.6)."""
    if tflops >= 1000:
        return f"{tflops / 1000:.1f} PFLOPs"
    if tflops >= 1:
        return f"{tflops:.2f} TFLOPs"
    return f"{tflops:.4f} TFLOPs"
