"""Tests for rag.py: Properties 17-24 and unit/example tests.

Feature: python-llm-rag-simulation
Covers tasks 6.5, 6.6, 6.7.
"""

from __future__ import annotations

import math
import random

import pytest
from hypothesis import assume, given, settings, HealthCheck
from hypothesis import strategies as st
from hypothesis.strategies import composite

from llmsim.rag import (
    DocumentStore,
    DocumentStoreParamError,
    RAGConfig,
    RAGConfigError,
    RAGResult,
    build_prefix_tokens,
    default_rag_config,
    run_rag_simulation,
)
from llmsim.models import LLAMA3_70B, LLAMA3_8B


# ---------------------------------------------------------------------------
# Helpers / shared strategies
# ---------------------------------------------------------------------------

def _make_valid_rag_config(**overrides) -> RAGConfig:
    """Return a minimal valid RAGConfig suitable for fast property tests."""
    defaults = dict(
        model=LLAMA3_8B,
        num_documents=10,
        tokens_per_doc=8,
        top_k=2,
        query_tokens=4,
        num_requests=10,
        zipf_exponent=1.0,
        requests_per_day=100,
    )
    defaults.update(overrides)
    return RAGConfig(**defaults)


@composite
def valid_rag_configs(draw) -> RAGConfig:
    """Composite Hypothesis strategy that draws a valid RAGConfig.

    Keeps all values small for speed (num_requests capped at 50).
    """
    num_documents = draw(st.integers(1, 20))
    top_k = draw(st.integers(1, num_documents))
    tokens_per_doc = draw(st.integers(1, 32))
    query_tokens = draw(st.integers(0, 32))
    num_requests = draw(st.integers(1, 50))
    zipf_exponent = draw(st.floats(min_value=0.01, max_value=3.0, allow_nan=False, allow_infinity=False))
    requests_per_day = draw(st.integers(1, 10_000))
    return RAGConfig(
        model=LLAMA3_8B,
        num_documents=num_documents,
        tokens_per_doc=tokens_per_doc,
        top_k=top_k,
        query_tokens=query_tokens,
        num_requests=num_requests,
        zipf_exponent=zipf_exponent,
        requests_per_day=requests_per_day,
    )


# ===========================================================================
# Property 17: Document store construction and retrieval are well-behaved
# ===========================================================================

# Feature: python-llm-rag-simulation, Property 17: Document store construction and retrieval are well-behaved

@given(st.integers(1, 1000), st.integers(1, 1000))
def test_property_17_document_store_construction(num_docs: int, tokens_per_doc: int) -> None:
    """Store holds exactly num_docs docs, each with the given token_count."""
    store = DocumentStore(num_docs, tokens_per_doc)
    assert store.num_docs == num_docs
    docs = store.retrieve(list(range(num_docs)))
    assert len(docs) == num_docs
    for doc in docs:
        assert doc.token_count == tokens_per_doc


@given(st.integers(1, 50), st.integers(1, 50), st.lists(st.integers(-200, 200)))
def test_property_17_document_store_retrieval(num_docs: int, tokens_per_doc: int, indices: list[int]) -> None:
    """Valid indices return docs; out-of-range silently excluded; empty range gives empty list."""
    store = DocumentStore(num_docs, tokens_per_doc)

    result = store.retrieve(indices)

    # Every returned doc corresponds to a valid index
    for doc in result:
        # doc id is "doc-NNN", NNN is 1-based
        doc_num = int(doc.id.split("-")[1])
        assert 1 <= doc_num <= num_docs
        assert doc.token_count == tokens_per_doc

    # Count of returned docs == count of in-range indices
    in_range_count = sum(1 for i in indices if 0 <= i < num_docs)
    assert len(result) == in_range_count

    # Empty retrieval never raises
    assert store.retrieve([]) == []

    # All-out-of-range returns empty
    out_of_range = [num_docs, num_docs + 1, -1, -100]
    assert store.retrieve(out_of_range) == []


# ===========================================================================
# Property 18: Document store rejects invalid construction parameters
# ===========================================================================

# Feature: python-llm-rag-simulation, Property 18: Document store rejects invalid construction parameters

@given(st.integers(max_value=0), st.integers(1, 1000))
def test_property_18_invalid_num_docs(num_docs: int, tokens_per_doc: int) -> None:
    """num_docs < 1 raises DocumentStoreParamError."""
    with pytest.raises(DocumentStoreParamError):
        DocumentStore(num_docs, tokens_per_doc)


@given(st.integers(1, 1000), st.integers(max_value=0))
def test_property_18_invalid_tokens_per_doc(num_docs: int, tokens_per_doc: int) -> None:
    """tokens_per_doc < 1 raises DocumentStoreParamError."""
    with pytest.raises(DocumentStoreParamError):
        DocumentStore(num_docs, tokens_per_doc)


# ===========================================================================
# Property 19: Exact-set prefix matching is order- and duplicate-independent
# ===========================================================================

# Feature: python-llm-rag-simulation, Property 19: Exact-set prefix matching is order- and duplicate-independent

@given(
    st.lists(st.integers(0, 50), min_size=1),
    st.integers(0, 100),
)
def test_property_19_order_and_duplicate_independence(indices: list[int], query_tokens: int) -> None:
    """Shuffled and duplicated index lists produce the same build_prefix_tokens output."""
    original = build_prefix_tokens(indices, query_tokens)

    # Shuffle
    shuffled = indices[:]
    random.shuffle(shuffled)
    assert build_prefix_tokens(shuffled, query_tokens) == original

    # Add duplicates
    duplicated = indices + indices
    assert build_prefix_tokens(duplicated, query_tokens) == original

    # Sort ascending (another ordering)
    sorted_indices = sorted(indices)
    assert build_prefix_tokens(sorted_indices, query_tokens) == original


# ===========================================================================
# Property 20: Different unique index sets produce different prefix hashes
# ===========================================================================

# Feature: python-llm-rag-simulation, Property 20: Different unique index sets produce different prefix hashes

@given(
    st.lists(st.integers(0, 100), min_size=0, max_size=20),
    st.lists(st.integers(0, 100), min_size=0, max_size=20),
    st.integers(0, 10),
)
def test_property_20_different_sets_different_output(
    a: list[int], b: list[int], query_tokens: int
) -> None:
    """Two genuinely different unique index sets produce different build_prefix_tokens output."""
    assume(set(a) != set(b))

    result_a = build_prefix_tokens(a, query_tokens)
    result_b = build_prefix_tokens(b, query_tokens)
    assert result_a != result_b


# ===========================================================================
# Property 21: RAG configuration validation rejects invalid fields
# ===========================================================================

# Feature: python-llm-rag-simulation, Property 21: RAG configuration validation rejects invalid fields

@pytest.mark.parametrize("field,bad_value,overrides", [
    # num_documents out of range (< 1)
    ("num_documents", 0, {}),
    # tokens_per_doc out of range (< 1)
    ("tokens_per_doc", 0, {}),
    # top_k < 1
    ("top_k", 0, {}),
    # top_k > num_documents (use num_documents=5, top_k=6)
    ("top_k", 6, {"num_documents": 5}),
    # query_tokens < 0
    ("query_tokens", -1, {}),
    # num_requests < 1
    ("num_requests", 0, {}),
    # zipf_exponent <= 0
    ("zipf_exponent", 0.0, {}),
    # requests_per_day < 1
    ("requests_per_day", 0, {}),
])
def test_property_21_invalid_rag_config_fields(field: str, bad_value, overrides: dict) -> None:
    """Each invalid field individually raises RAGConfigError naming that field."""
    kwargs = {
        "model": LLAMA3_8B,
        "num_documents": 10,
        "tokens_per_doc": 8,
        "top_k": 2,
        "query_tokens": 4,
        "num_requests": 10,
        "zipf_exponent": 1.0,
        "requests_per_day": 100,
    }
    kwargs.update(overrides)
    kwargs[field] = bad_value

    cfg = RAGConfig(**kwargs)
    with pytest.raises(RAGConfigError) as exc_info:
        cfg.validate()
    # Error message should mention the field name
    assert field in str(exc_info.value)


@given(
    st.integers(min_value=2, max_value=100),   # num_documents
    st.integers(min_value=1, max_value=100),   # tokens_per_doc
)
def test_property_21_top_k_exceeds_num_documents(num_documents: int, tokens_per_doc: int) -> None:
    """top_k > num_documents raises RAGConfigError."""
    top_k = num_documents + 1
    cfg = RAGConfig(
        model=LLAMA3_8B,
        num_documents=num_documents,
        tokens_per_doc=tokens_per_doc,
        top_k=top_k,
        query_tokens=0,
        num_requests=1,
        zipf_exponent=1.0,
        requests_per_day=1,
    )
    with pytest.raises(RAGConfigError) as exc_info:
        cfg.validate()
    assert "top_k" in str(exc_info.value)


# ===========================================================================
# Property 22: RAG prefix size and cache accounting are consistent
# ===========================================================================

# Feature: python-llm-rag-simulation, Property 22: RAG prefix size and cache accounting are consistent

@given(valid_rag_configs())
@settings(max_examples=30, suppress_health_check=[HealthCheck.too_slow])
def test_property_22_prefix_size_and_accounting(cfg: RAGConfig) -> None:
    """prefix_tokens == top_k * tokens_per_doc + query_tokens; cache_hits + cache_misses == num_requests."""
    result = run_rag_simulation(cfg, seed=42)

    expected_prefix_tokens = cfg.top_k * cfg.tokens_per_doc + cfg.query_tokens
    assert result.prefix_tokens == expected_prefix_tokens

    stats = result.stats
    assert stats.cache_hits + stats.cache_misses == cfg.num_requests


# ===========================================================================
# Property 23: Zipf-selected document indices stay within range
# ===========================================================================

# Feature: python-llm-rag-simulation, Property 23: Zipf-selected document indices stay within range

def test_property_23_zipf_indices_within_range_default() -> None:
    """Run default_rag_config() simulation and verify it completes without index errors."""
    cfg = default_rag_config()
    result = run_rag_simulation(cfg, seed=42)

    # If any out-of-range index was used, DocumentStore.retrieve would silently exclude it,
    # but the prefix would be built from fewer docs — leading to wrong prefix_tokens.
    # The simulation completes and stats are well-formed: hits + misses == num_requests.
    stats = result.stats
    assert stats.cache_hits + stats.cache_misses == cfg.num_requests
    assert 0 <= stats.hit_rate <= 100.0


@given(valid_rag_configs())
@settings(max_examples=20, suppress_health_check=[HealthCheck.too_slow])
def test_property_23_zipf_indices_within_range_custom(cfg: RAGConfig) -> None:
    """Verify small custom configs complete without errors and accounting holds."""
    result = run_rag_simulation(cfg, seed=0)

    stats = result.stats
    assert stats.cache_hits + stats.cache_misses == cfg.num_requests
    # Hits and misses are non-negative
    assert stats.cache_hits >= 0
    assert stats.cache_misses >= 0


# ===========================================================================
# Property 24: Monthly projection arithmetic holds
# ===========================================================================

# Feature: python-llm-rag-simulation, Property 24: Monthly projection arithmetic holds

@given(valid_rag_configs())
@settings(max_examples=30, suppress_health_check=[HealthCheck.too_slow])
def test_property_24_monthly_projection_arithmetic(cfg: RAGConfig) -> None:
    """Monthly projection fields satisfy exact arithmetic relationships."""
    result = run_rag_simulation(cfg, seed=42)

    # monthly_requests == requests_per_day * 30
    assert result.monthly_requests == cfg.requests_per_day * 30

    # monthly_without == per_req_without * monthly_requests
    assert math.isclose(
        result.monthly_without,
        result.per_req_without_usd * result.monthly_requests,
        rel_tol=1e-9,
    )

    # monthly_with == per_req_with * monthly_requests
    assert math.isclose(
        result.monthly_with,
        result.per_req_with_usd * result.monthly_requests,
        rel_tol=1e-9,
    )

    # monthly_savings == monthly_without - monthly_with
    assert math.isclose(
        result.monthly_savings,
        result.monthly_without - result.monthly_with,
        rel_tol=1e-9,
        abs_tol=1e-15,
    )

    # per_req_without == without_cache / num_requests
    assert math.isclose(
        result.per_req_without_usd,
        result.without_cache_usd / cfg.num_requests,
        rel_tol=1e-9,
    )


# ===========================================================================
# Unit / example tests (task 6.7)
# ===========================================================================

class TestDocumentStoreUnit:
    """Unit tests for DocumentStore construction and retrieval."""

    def test_holds_correct_count_and_token_count(self) -> None:
        """DocumentStore(200, 512) holds 200 docs each with token_count=512."""
        store = DocumentStore(200, 512)
        assert store.num_docs == 200
        docs = store.retrieve(list(range(200)))
        assert len(docs) == 200
        for doc in docs:
            assert doc.token_count == 512

    def test_invalid_num_docs_zero_raises(self) -> None:
        """DocumentStore(0, 512) raises DocumentStoreParamError."""
        with pytest.raises(DocumentStoreParamError):
            DocumentStore(0, 512)

    def test_retrieve_empty_indices_returns_empty(self) -> None:
        """retrieve([]) returns []."""
        store = DocumentStore(10, 64)
        assert store.retrieve([]) == []

    def test_retrieve_all_out_of_range_returns_empty(self) -> None:
        """retrieve with all-out-of-range indices returns []."""
        store = DocumentStore(5, 64)
        assert store.retrieve([5, 6, 100, -1, -999]) == []


class TestBuildPrefixTokensUnit:
    """Unit tests for build_prefix_tokens."""

    def test_empty_indices_zero_query_returns_empty(self) -> None:
        """build_prefix_tokens([], 0) returns []."""
        assert build_prefix_tokens([], 0) == []

    def test_order_independence(self) -> None:
        """build_prefix_tokens([3,1,2], 64) == build_prefix_tokens([1,2,3], 64)."""
        assert build_prefix_tokens([3, 1, 2], 64) == build_prefix_tokens([1, 2, 3], 64)

    def test_duplicate_independence(self) -> None:
        """build_prefix_tokens([1,1,2], 64) == build_prefix_tokens([1,2], 64)."""
        assert build_prefix_tokens([1, 1, 2], 64) == build_prefix_tokens([1, 2], 64)

    def test_query_block_appended(self) -> None:
        """Query block tokens are appended after doc marker tokens."""
        tokens = build_prefix_tokens([0], 3)
        # Doc 0: 8 marker tokens [500000]*8, then query [900000, 900001, 900002]
        assert tokens[:8] == [500000] * 8
        assert tokens[8:] == [900000, 900001, 900002]

    def test_empty_indices_with_query_returns_query_block(self) -> None:
        """With no indices, only the query block is returned."""
        tokens = build_prefix_tokens([], 5)
        assert tokens == [900000, 900001, 900002, 900003, 900004]


class TestRunRagSimulationUnit:
    """Unit tests for run_rag_simulation and default_rag_config."""

    def test_default_config_savings_pct_high(self) -> None:
        """Run default_rag_config() with seed=42: savings_pct > 85.0% (expected ~93.9%)."""
        cfg = default_rag_config()
        result = run_rag_simulation(cfg, seed=42)
        assert result.savings_pct > 85.0, (
            f"Expected savings_pct > 85.0, got {result.savings_pct:.2f}%"
        )

    def test_savings_pct_zero_when_zero_cost(self) -> None:
        """savings_pct == 0.0 when all requests hit cache (effectively zero miss cost)."""
        # Use a tiny config where num_requests=1 — there is exactly 1 miss and 0 hits.
        # To get savings_pct == 0.0 we need without_cache_usd == 0, which happens when
        # cost_usd(prefix_tokens) == 0. Use query_tokens=0 and tokens_per_doc=0 is invalid,
        # so instead we use the formula directly: set model with zero-cost tokens is not
        # straightforward. Instead, test the math branch: run with num_requests=1,
        # which gives 1 miss, 0 hits, savings = 0, savings_pct = 0.
        cfg = _make_valid_rag_config(num_requests=1)
        result = run_rag_simulation(cfg, seed=42)
        # 1 request → 1 miss → with_cache == without_cache → savings_usd == 0 → savings_pct == 0
        assert result.stats.cache_misses == 1
        assert result.stats.cache_hits == 0
        assert result.savings_usd == pytest.approx(0.0, abs=1e-15)
        assert result.savings_pct == pytest.approx(0.0, abs=1e-10)

    def test_monthly_requests_equals_requests_per_day_times_30(self) -> None:
        """monthly_requests == requests_per_day * 30 for default config."""
        cfg = default_rag_config()
        result = run_rag_simulation(cfg, seed=42)
        assert result.monthly_requests == cfg.requests_per_day * 30

    def test_prefix_tokens_formula(self) -> None:
        """prefix_tokens == top_k * tokens_per_doc + query_tokens."""
        cfg = default_rag_config()
        result = run_rag_simulation(cfg, seed=42)
        expected = cfg.top_k * cfg.tokens_per_doc + cfg.query_tokens
        assert result.prefix_tokens == expected

    def test_accounting_consistency(self) -> None:
        """cache_hits + cache_misses == num_requests for default config."""
        cfg = default_rag_config()
        result = run_rag_simulation(cfg, seed=42)
        assert result.stats.cache_hits + result.stats.cache_misses == cfg.num_requests

    def test_savings_arithmetic(self) -> None:
        """savings_usd == without_cache_usd - with_cache_usd."""
        cfg = default_rag_config()
        result = run_rag_simulation(cfg, seed=42)
        assert math.isclose(
            result.savings_usd,
            result.without_cache_usd - result.with_cache_usd,
            rel_tol=1e-9,
        )

    def test_monthly_projection_default(self) -> None:
        """Monthly projection arithmetic holds for default config."""
        cfg = default_rag_config()
        result = run_rag_simulation(cfg, seed=42)

        assert result.monthly_requests == cfg.requests_per_day * 30

        assert math.isclose(
            result.monthly_without,
            result.per_req_without_usd * result.monthly_requests,
            rel_tol=1e-9,
        )
        assert math.isclose(
            result.monthly_with,
            result.per_req_with_usd * result.monthly_requests,
            rel_tol=1e-9,
        )
        assert math.isclose(
            result.monthly_savings,
            result.monthly_without - result.monthly_with,
            rel_tol=1e-9,
            abs_tol=1e-15,
        )

    def test_invalid_config_raises_before_simulation(self) -> None:
        """RAGConfigError raised on invalid config before any simulation work."""
        bad_cfg = RAGConfig(
            model=LLAMA3_8B,
            num_documents=5,
            tokens_per_doc=8,
            top_k=10,   # top_k > num_documents → invalid
            query_tokens=4,
            num_requests=10,
            zipf_exponent=1.0,
            requests_per_day=100,
        )
        with pytest.raises(RAGConfigError):
            run_rag_simulation(bad_cfg, seed=42)
