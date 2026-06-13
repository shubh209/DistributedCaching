"""Tests for prefix_cache.py: hashing, hit/miss, LRU eviction, stats.

Covers:
  - Property tests (Hypothesis) for Properties 5–12
  - Unit/example tests for hash stability, hit/miss, LRU eviction,
    capacity validation, and stats accounting

Feature: python-llm-rag-simulation
"""

from __future__ import annotations

import re
import copy

import pytest
from hypothesis import given, assume, settings
from hypothesis import strategies as st
from hypothesis.strategies import composite

from llmsim.prefix_cache import (
    CacheEntry,
    CacheStats,
    PrefixCache,
    hash_prefix,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

HEX32_RE = re.compile(r"^[0-9a-f]{32}$")


def make_entry(
    prefix_hash: str = "aabbccddeeff00112233445566778899",
    model: str = "test-model",
    token_count: int = 10,
    tflops_cost: float = 1.5,
    cost_usd: float = 0.01,
    hit_count: int = 0,
) -> CacheEntry:
    """Create a CacheEntry with sensible defaults."""
    return CacheEntry(
        prefix_hash=prefix_hash,
        model=model,
        token_count=token_count,
        tflops_cost=tflops_cost,
        cost_usd=cost_usd,
        hit_count=hit_count,
    )


# ---------------------------------------------------------------------------
# Property 5: Prefix hash is a stable 32-character lowercase hex string
# Feature: python-llm-rag-simulation, Property 5: Prefix hash is a stable
#   32-character lowercase hex string
# ---------------------------------------------------------------------------

@given(st.lists(st.integers()))
def test_property5_hash_format(tokens):
    """hash_prefix always returns a 32-char lowercase hex string."""
    # Feature: python-llm-rag-simulation, Property 5: Prefix hash is a stable 32-character lowercase hex string
    h = hash_prefix(tokens)
    assert HEX32_RE.fullmatch(h), (
        f"hash_prefix({tokens!r}) = {h!r} does not match ^[0-9a-f]{{32}}$"
    )


@given(st.lists(st.integers()))
def test_property5_hash_stability(tokens):
    """Same input called twice produces identical hash (deterministic)."""
    # Feature: python-llm-rag-simulation, Property 5: Prefix hash is a stable 32-character lowercase hex string
    assert hash_prefix(tokens) == hash_prefix(tokens)


# ---------------------------------------------------------------------------
# Property 6: Prefix hashing distinguishes different sequences
# Feature: python-llm-rag-simulation, Property 6: Prefix hashing distinguishes
#   different sequences
# ---------------------------------------------------------------------------

@given(st.lists(st.integers()), st.lists(st.integers()))
def test_property6_different_sequences_different_hashes(a, b):
    """Two token lists that are not identical must produce different hashes."""
    # Feature: python-llm-rag-simulation, Property 6: Prefix hashing distinguishes different sequences
    assume(a != b)
    assert hash_prefix(a) != hash_prefix(b), (
        f"hash_prefix collision: {a!r} and {b!r} both hash to {hash_prefix(a)!r}"
    )


@given(
    st.lists(st.integers(), min_size=1),
)
def test_property6_order_matters(tokens):
    """Reversing a non-palindromic list changes the hash."""
    # Feature: python-llm-rag-simulation, Property 6: Prefix hashing distinguishes different sequences
    reversed_tokens = list(reversed(tokens))
    assume(tokens != reversed_tokens)
    assert hash_prefix(tokens) != hash_prefix(reversed_tokens)


@given(st.lists(st.integers(), min_size=1))
def test_property6_extra_element_changes_hash(tokens):
    """Appending one element changes the hash."""
    # Feature: python-llm-rag-simulation, Property 6: Prefix hashing distinguishes different sequences
    extended = tokens + [tokens[-1] + 1]
    assert hash_prefix(tokens) != hash_prefix(extended)


# ---------------------------------------------------------------------------
# Property 7: Cache hit returns the entry, marks it MRU, and accumulates savings
# Feature: python-llm-rag-simulation, Property 7: Cache hit returns the entry,
#   marks it most-recently-used, and accumulates savings
# ---------------------------------------------------------------------------

@given(
    capacity=st.integers(min_value=1, max_value=20),
    tflops=st.floats(min_value=0.0, max_value=1e6, allow_nan=False, allow_infinity=False),
    usd=st.floats(min_value=0.0, max_value=1e6, allow_nan=False, allow_infinity=False),
    tokens=st.lists(st.integers(), min_size=1, max_size=50),
)
def test_property7_hit_returns_entry_and_accumulates(capacity, tflops, usd, tokens):
    """
    Cache hit: returns entry, increments hit_count, increments cache_hits,
    and adds tflops_cost / cost_usd to saved totals.

    Feature: python-llm-rag-simulation, Property 7: Cache hit returns the entry, marks it most-recently-used, and accumulates savings
    """
    cache = PrefixCache(capacity)
    h = hash_prefix(tokens)
    entry = make_entry(prefix_hash=h, tflops_cost=tflops, cost_usd=usd)
    cache.store(entry)

    result = cache.lookup(h)

    assert result is not None, "lookup should return the entry on a hit"
    assert result.hit_count == 1, "hit_count should be incremented to 1"

    stats = cache.stats()
    assert stats.cache_hits == 1
    assert stats.total_requests == 1
    assert abs(stats.total_saved_tflops - tflops) < 1e-9
    assert abs(stats.total_saved_usd - usd) < 1e-9


@given(
    capacity=st.integers(min_value=2, max_value=20),
    n_hits=st.integers(min_value=2, max_value=10),
    tokens=st.lists(st.integers(), min_size=1, max_size=30),
    tflops=st.floats(min_value=0.0, max_value=100.0, allow_nan=False, allow_infinity=False),
    usd=st.floats(min_value=0.0, max_value=100.0, allow_nan=False, allow_infinity=False),
)
def test_property7_hit_marks_mru(capacity, n_hits, tokens, tflops, usd):
    """
    After n hits on one entry, that entry remains in cache (is MRU),
    and hit_count equals n_hits.

    Feature: python-llm-rag-simulation, Property 7: Cache hit returns the entry, marks it most-recently-used, and accumulates savings
    """
    cache = PrefixCache(capacity)
    h = hash_prefix(tokens)
    entry = make_entry(prefix_hash=h, tflops_cost=tflops, cost_usd=usd)
    cache.store(entry)

    for _ in range(n_hits):
        result = cache.lookup(h)
        assert result is not None

    assert result.hit_count == n_hits
    stats = cache.stats()
    assert stats.cache_hits == n_hits
    assert abs(stats.total_saved_tflops - tflops * n_hits) < 1e-6
    assert abs(stats.total_saved_usd - usd * n_hits) < 1e-6


# ---------------------------------------------------------------------------
# Property 8: Cache miss returns nothing and leaves the cache unchanged
# Feature: python-llm-rag-simulation, Property 8: Cache miss returns nothing
#   and leaves the cache unchanged
# ---------------------------------------------------------------------------

@given(
    capacity=st.integers(min_value=1, max_value=20),
    stored_tokens=st.lists(st.integers(), min_size=1, max_size=30),
    miss_tokens=st.lists(st.integers(), min_size=1, max_size=30),
)
def test_property8_miss_returns_none(capacity, stored_tokens, miss_tokens):
    """
    Lookup of a hash not in the cache returns None and leaves stats unchanged
    except total_requests.

    Feature: python-llm-rag-simulation, Property 8: Cache miss returns nothing and leaves the cache unchanged
    """
    assume(hash_prefix(stored_tokens) != hash_prefix(miss_tokens))

    cache = PrefixCache(capacity)
    h_stored = hash_prefix(stored_tokens)
    entry = make_entry(prefix_hash=h_stored)
    cache.store(entry)

    entries_before = cache.stats().entries_stored

    result = cache.lookup(hash_prefix(miss_tokens))

    assert result is None
    stats = cache.stats()
    assert stats.cache_hits == 0
    assert stats.cache_misses == 1
    assert stats.entries_stored == entries_before
    assert stats.total_saved_tflops == 0.0
    assert stats.total_saved_usd == 0.0


@given(
    capacity=st.integers(min_value=1, max_value=20),
    miss_tokens=st.lists(st.integers(), max_size=30),
)
def test_property8_miss_empty_cache(capacity, miss_tokens):
    """Lookup on empty cache always returns None with cache_misses=1."""
    # Feature: python-llm-rag-simulation, Property 8: Cache miss returns nothing and leaves the cache unchanged
    cache = PrefixCache(capacity)
    result = cache.lookup(hash_prefix(miss_tokens))
    assert result is None
    stats = cache.stats()
    assert stats.cache_misses == 1
    assert stats.entries_stored == 0


# ---------------------------------------------------------------------------
# Property 9: Store inserts or replaces, keeping at most one entry per hash
# Feature: python-llm-rag-simulation, Property 9: Store inserts or replaces,
#   keeping at most one entry per hash
# ---------------------------------------------------------------------------

@given(
    capacity=st.integers(min_value=2, max_value=20),
    token_lists=st.lists(
        st.lists(st.integers(), min_size=1, max_size=20),
        min_size=2,
        max_size=15,
        unique_by=lambda x: tuple(x),
    ),
)
def test_property9_at_most_one_entry_per_hash(capacity, token_lists):
    """
    After storing many entries, entries_stored <= capacity, and a lookup for
    any stored hash returns at most one result.

    Feature: python-llm-rag-simulation, Property 9: Store inserts or replaces, keeping at most one entry per hash
    """
    cache = PrefixCache(capacity)
    for tl in token_lists:
        h = hash_prefix(tl)
        cache.store(make_entry(prefix_hash=h))

    stats = cache.stats()
    assert stats.entries_stored <= capacity


@given(
    capacity=st.integers(min_value=1, max_value=20),
    tokens=st.lists(st.integers(), min_size=1, max_size=30),
    tflops_v2=st.floats(min_value=0.1, max_value=100.0, allow_nan=False, allow_infinity=False),
)
def test_property9_replace_does_not_grow_count(capacity, tokens, tflops_v2):
    """
    Re-storing the same hash replaces the entry but entries_stored stays the same.

    Feature: python-llm-rag-simulation, Property 9: Store inserts or replaces, keeping at most one entry per hash
    """
    cache = PrefixCache(capacity)
    h = hash_prefix(tokens)
    cache.store(make_entry(prefix_hash=h, tflops_cost=1.0))
    count_after_first = cache.stats().entries_stored

    cache.store(make_entry(prefix_hash=h, tflops_cost=tflops_v2))
    count_after_replace = cache.stats().entries_stored

    assert count_after_first == count_after_replace


# ---------------------------------------------------------------------------
# Property 10: Storing at full capacity evicts exactly the LRU entry
# Feature: python-llm-rag-simulation, Property 10: Storing at full capacity
#   evicts exactly the least-recently-used entry
# ---------------------------------------------------------------------------

@composite
def cache_access_patterns(draw):
    """Generate a list of distinct token lists for filling a cache of known capacity."""
    capacity = draw(st.integers(min_value=2, max_value=8))
    # We need capacity + 1 distinct hashes to trigger eviction
    n = capacity + 1
    token_lists = draw(
        st.lists(
            st.lists(st.integers(min_value=0, max_value=1000), min_size=1, max_size=10),
            min_size=n,
            max_size=n,
            unique_by=lambda x: tuple(x),
        )
    )
    return capacity, token_lists


@given(cache_access_patterns())
def test_property10_lru_eviction_on_full(pattern):
    """
    Fill cache to capacity, then store a new entry; the LRU entry is evicted.

    Feature: python-llm-rag-simulation, Property 10: Storing at full capacity evicts exactly the least-recently-used entry
    """
    capacity, token_lists = pattern
    # Ensure all hashes are distinct (may overlap due to hash collisions — skip if so)
    hashes = [hash_prefix(tl) for tl in token_lists]
    assume(len(set(hashes)) == len(hashes))

    cache = PrefixCache(capacity)

    # Store first `capacity` entries in order → token_lists[0] is LRU
    for tl in token_lists[:capacity]:
        h = hash_prefix(tl)
        cache.store(make_entry(prefix_hash=h))

    lru_hash = hash_prefix(token_lists[0])
    new_hash = hash_prefix(token_lists[capacity])

    # Capacity is full — store one more new entry
    cache.store(make_entry(prefix_hash=new_hash))

    # LRU entry (first stored, never accessed since) must be gone
    assert cache.lookup(lru_hash) is None, "LRU entry should have been evicted"
    # New entry must be present
    assert cache.lookup(new_hash) is not None, "Newly stored entry must be present"


@given(cache_access_patterns())
def test_property10_access_prevents_eviction(pattern):
    """
    Accessing entry A after storing it (but before filling capacity) prevents A
    from being evicted when a new entry is inserted.

    Feature: python-llm-rag-simulation, Property 10: Storing at full capacity evicts exactly the least-recently-used entry
    """
    capacity, token_lists = pattern
    hashes = [hash_prefix(tl) for tl in token_lists]
    assume(len(set(hashes)) == len(hashes))

    cache = PrefixCache(capacity)

    # Store all capacity entries
    for tl in token_lists[:capacity]:
        cache.store(make_entry(prefix_hash=hash_prefix(tl)))

    # Access the first entry (making it MRU — no longer LRU)
    first_hash = hash_prefix(token_lists[0])
    cache.lookup(first_hash)

    # Store a new entry — should evict token_lists[1] (now the LRU)
    new_hash = hash_prefix(token_lists[capacity])
    cache.store(make_entry(prefix_hash=new_hash))

    # First entry (which was accessed) should still be present
    # (stats.total_requests is now 2 due to the lookup above + the eviction lookup below)
    still_there = cache.lookup(first_hash)
    assert still_there is not None, "Accessed entry should NOT have been evicted"


# ---------------------------------------------------------------------------
# Property 11: Cache accounting is consistent
# Feature: python-llm-rag-simulation, Property 11: Cache accounting is consistent
# ---------------------------------------------------------------------------

@composite
def lookup_sequence(draw):
    """Generate a (capacity, stored_hashes, lookup_sequence) triple."""
    capacity = draw(st.integers(min_value=1, max_value=10))
    n_entries = draw(st.integers(min_value=1, max_value=8))
    base_tokens = draw(
        st.lists(
            st.lists(st.integers(0, 500), min_size=1, max_size=8),
            min_size=n_entries,
            max_size=n_entries,
            unique_by=lambda x: tuple(x),
        )
    )
    # Build lookup sequence: mix of existing and non-existing hashes
    stored_hashes = [hash_prefix(tl) for tl in base_tokens]
    # Extra hashes that are definitely absent
    absent_tokens = draw(
        st.lists(
            st.lists(st.integers(10000, 20000), min_size=1, max_size=5),
            min_size=1,
            max_size=5,
            unique_by=lambda x: tuple(x),
        )
    )
    absent_hashes = [hash_prefix(tl) for tl in absent_tokens]
    all_absent = [h for h in absent_hashes if h not in stored_hashes]
    assume(len(all_absent) > 0)

    # Mix of hits and misses
    lookups = draw(
        st.lists(
            st.sampled_from(stored_hashes + all_absent),
            min_size=1,
            max_size=20,
        )
    )
    return capacity, stored_hashes, lookups


@given(lookup_sequence())
def test_property11_accounting_consistency(seq):
    """
    total_requests == number of lookups; hits + misses == total_requests;
    hit_rate == hits / total * 100 when total > 0.

    Feature: python-llm-rag-simulation, Property 11: Cache accounting is consistent
    """
    capacity, stored_hashes, lookups = seq

    cache = PrefixCache(capacity)
    for h in stored_hashes:
        cache.store(make_entry(prefix_hash=h))

    for h in lookups:
        cache.lookup(h)

    stats = cache.stats()
    n = len(lookups)

    assert stats.total_requests == n
    assert stats.cache_hits + stats.cache_misses == stats.total_requests
    assert 0 <= stats.hit_rate <= 100.0

    if n > 0:
        expected_rate = stats.cache_hits / n * 100.0
        assert abs(stats.hit_rate - expected_rate) < 1e-9


@given(st.integers(min_value=1, max_value=50))
def test_property11_zero_lookups_hit_rate_zero(capacity):
    """With no lookups, hit_rate is 0.0 and all counts are zero."""
    # Feature: python-llm-rag-simulation, Property 11: Cache accounting is consistent
    cache = PrefixCache(capacity)
    stats = cache.stats()
    assert stats.total_requests == 0
    assert stats.cache_hits == 0
    assert stats.cache_misses == 0
    assert stats.hit_rate == 0.0


# ---------------------------------------------------------------------------
# Property 12: Entry saved totals scale with hit count
# Feature: python-llm-rag-simulation, Property 12: Entry saved totals scale
#   with hit count
# ---------------------------------------------------------------------------

@given(
    tflops=st.floats(min_value=0.0, max_value=1e6, allow_nan=False, allow_infinity=False),
    usd=st.floats(min_value=0.0, max_value=1e6, allow_nan=False, allow_infinity=False),
    hit_count=st.integers(min_value=0, max_value=1000),
)
def test_property12_saved_totals_scale(tflops, usd, hit_count):
    """
    saved_tflops == tflops_cost * hit_count; saved_usd == cost_usd * hit_count;
    both 0.0 when hit_count is 0.

    Feature: python-llm-rag-simulation, Property 12: Entry saved totals scale with hit count
    """
    entry = make_entry(tflops_cost=tflops, cost_usd=usd, hit_count=hit_count)
    assert abs(entry.saved_tflops() - tflops * hit_count) < 1e-9
    assert abs(entry.saved_usd() - usd * hit_count) < 1e-9

    if hit_count == 0:
        assert entry.saved_tflops() == 0.0
        assert entry.saved_usd() == 0.0


@given(
    capacity=st.integers(min_value=1, max_value=10),
    tokens=st.lists(st.integers(), min_size=1, max_size=20),
    n_lookups=st.integers(min_value=1, max_value=20),
    tflops=st.floats(min_value=0.0, max_value=1000.0, allow_nan=False, allow_infinity=False),
    usd=st.floats(min_value=0.0, max_value=1000.0, allow_nan=False, allow_infinity=False),
)
def test_property12_cache_accumulates_saved_totals(capacity, tokens, n_lookups, tflops, usd):
    """
    After n_lookups hits, total_saved_tflops == tflops * n_lookups
    and total_saved_usd == usd * n_lookups.

    Feature: python-llm-rag-simulation, Property 12: Entry saved totals scale with hit count
    """
    cache = PrefixCache(capacity)
    h = hash_prefix(tokens)
    cache.store(make_entry(prefix_hash=h, tflops_cost=tflops, cost_usd=usd))

    for _ in range(n_lookups):
        cache.lookup(h)

    stats = cache.stats()
    assert abs(stats.total_saved_tflops - tflops * n_lookups) < 1e-6
    assert abs(stats.total_saved_usd - usd * n_lookups) < 1e-6


# ===========================================================================
# Unit / Example Tests
# ===========================================================================


class TestHashPrefix:
    """Unit tests for hash_prefix."""

    def test_returns_32_char_hex(self):
        h = hash_prefix([1, 2, 3])
        assert len(h) == 32
        assert HEX32_RE.fullmatch(h), f"Got {h!r}"

    def test_empty_list_does_not_raise(self):
        h = hash_prefix([])
        assert HEX32_RE.fullmatch(h), f"Empty list hash {h!r} is not 32-char hex"

    def test_same_input_same_output(self):
        assert hash_prefix([1, 2, 3]) == hash_prefix([1, 2, 3])

    def test_different_order_different_hash(self):
        assert hash_prefix([1, 2, 3]) != hash_prefix([3, 2, 1])

    def test_different_values_different_hash(self):
        assert hash_prefix([1, 2, 3]) != hash_prefix([1, 2, 4])

    def test_subset_different_hash(self):
        assert hash_prefix([1, 2]) != hash_prefix([1, 2, 3])

    def test_all_lowercase(self):
        h = hash_prefix([42, 999, -1])
        assert h == h.lower()


class TestPrefixCacheCapacity:
    """Unit tests for capacity validation."""

    def test_zero_capacity_raises(self):
        with pytest.raises(ValueError):
            PrefixCache(0)

    def test_negative_capacity_raises(self):
        with pytest.raises(ValueError):
            PrefixCache(-1)

    def test_capacity_one_does_not_raise(self):
        cache = PrefixCache(1)
        assert cache is not None

    def test_large_capacity_does_not_raise(self):
        cache = PrefixCache(100_000)
        assert cache is not None


class TestCacheHitMiss:
    """Unit tests for cache hit and miss behavior."""

    def _make_cache_with_entry(self, capacity=10):
        cache = PrefixCache(capacity)
        tokens = [1, 2, 3]
        h = hash_prefix(tokens)
        entry = make_entry(prefix_hash=h, tflops_cost=2.5, cost_usd=0.05)
        cache.store(entry)
        return cache, h

    def test_hit_returns_cache_entry(self):
        cache, h = self._make_cache_with_entry()
        result = cache.lookup(h)
        assert result is not None
        assert isinstance(result, CacheEntry)
        assert result.prefix_hash == h

    def test_hit_increments_cache_hits_stat(self):
        cache, h = self._make_cache_with_entry()
        cache.lookup(h)
        assert cache.stats().cache_hits == 1

    def test_miss_returns_none(self):
        cache, _ = self._make_cache_with_entry()
        result = cache.lookup(hash_prefix([9, 9, 9]))
        assert result is None

    def test_miss_increments_cache_misses_stat(self):
        cache, _ = self._make_cache_with_entry()
        cache.lookup(hash_prefix([9, 9, 9]))
        assert cache.stats().cache_misses == 1

    def test_multiple_lookups_total_requests(self):
        cache, h = self._make_cache_with_entry()
        cache.lookup(h)               # hit
        cache.lookup(hash_prefix([0]))  # miss
        cache.lookup(h)               # hit
        stats = cache.stats()
        assert stats.total_requests == 3
        assert stats.cache_hits == 2
        assert stats.cache_misses == 1

    def test_stats_zero_state(self):
        cache = PrefixCache(5)
        stats = cache.stats()
        assert stats.hit_rate == 0.0
        assert stats.cache_hits == 0
        assert stats.cache_misses == 0
        assert stats.total_requests == 0

    def test_stats_hit_rate_formula(self):
        cache = PrefixCache(10)
        tokens_a = [10, 20, 30]
        tokens_b = [40, 50, 60]
        ha = hash_prefix(tokens_a)
        hb = hash_prefix(tokens_b)
        cache.store(make_entry(prefix_hash=ha))
        cache.store(make_entry(prefix_hash=hb))

        cache.lookup(ha)   # hit
        cache.lookup(hb)   # hit
        cache.lookup(hash_prefix([99]))  # miss

        stats = cache.stats()
        assert stats.total_requests == 3
        assert stats.cache_hits == 2
        assert stats.cache_misses == 1
        expected_rate = 2 / 3 * 100
        assert abs(stats.hit_rate - expected_rate) < 1e-9


class TestLRUEviction:
    """Unit tests for LRU eviction behavior."""

    def test_lru_eviction_basic(self):
        """capacity=2: store A, store B, lookup A (A is MRU), store C → B evicted."""
        cache = PrefixCache(2)

        ha = hash_prefix([1])
        hb = hash_prefix([2])
        hc = hash_prefix([3])

        cache.store(make_entry(prefix_hash=ha))  # cache: [A]
        cache.store(make_entry(prefix_hash=hb))  # cache: [A, B]
        cache.lookup(ha)                          # cache: [B, A]  (A is MRU)
        cache.store(make_entry(prefix_hash=hc))  # cache: [A, C]  (B evicted)

        # B should be evicted
        assert cache.lookup(hb) is None, "B should have been evicted (LRU)"
        # A should still be present
        assert cache.lookup(ha) is not None, "A should still be in cache"
        # C should be present
        assert cache.lookup(hc) is not None, "C should be in cache"

    def test_eviction_respects_capacity(self):
        """Cache never exceeds capacity."""
        cap = 3
        cache = PrefixCache(cap)
        for i in range(10):
            h = hash_prefix([i])
            cache.store(make_entry(prefix_hash=h))
            assert cache.stats().entries_stored <= cap

    def test_store_replaces_without_growing(self):
        """Storing the same hash twice does not increase entries_stored."""
        cache = PrefixCache(5)
        h = hash_prefix([7, 8, 9])
        cache.store(make_entry(prefix_hash=h, tflops_cost=1.0))
        assert cache.stats().entries_stored == 1
        cache.store(make_entry(prefix_hash=h, tflops_cost=2.0))
        assert cache.stats().entries_stored == 1

    def test_capacity_one_always_evicts_previous(self):
        """A capacity-1 cache always holds only the most recently stored entry."""
        cache = PrefixCache(1)
        ha = hash_prefix([100])
        hb = hash_prefix([200])
        cache.store(make_entry(prefix_hash=ha))
        cache.store(make_entry(prefix_hash=hb))
        assert cache.lookup(ha) is None
        assert cache.lookup(hb) is not None


class TestCacheEntryMethods:
    """Unit tests for CacheEntry.saved_tflops and saved_usd."""

    def test_saved_tflops_zero_hits(self):
        e = make_entry(tflops_cost=5.0, hit_count=0)
        assert e.saved_tflops() == 0.0

    def test_saved_usd_zero_hits(self):
        e = make_entry(cost_usd=3.0, hit_count=0)
        assert e.saved_usd() == 0.0

    def test_saved_tflops_with_hits(self):
        e = make_entry(tflops_cost=4.0, hit_count=3)
        assert abs(e.saved_tflops() - 12.0) < 1e-9

    def test_saved_usd_with_hits(self):
        e = make_entry(cost_usd=0.10, hit_count=7)
        assert abs(e.saved_usd() - 0.70) < 1e-9
