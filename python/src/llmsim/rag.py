"""RAG simulator: DocumentStore, RAGConfig, RAG pipeline."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Sequence

import numpy as np

from llmsim.models import ModelConfig, LLAMA3_70B
from llmsim.prefix_cache import (
    PrefixCache,
    CacheEntry,
    CacheStats,
    hash_prefix,
    new_entry_for_prefix,
)

__all__ = [
    "Document",
    "DocumentStore",
    "DocumentStoreParamError",
    "RAGConfig",
    "RAGConfigError",
    "RAGResult",
    "default_rag_config",
    "run_rag_simulation",
    "build_prefix_tokens",
    "new_entry_for_token_count",
]


# ---------------------------------------------------------------------------
# Document and DocumentStore
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class Document:
    """A single retrievable document in the knowledge base."""
    id: str
    token_count: int


class DocumentStoreParamError(ValueError):
    """Raised when DocumentStore is constructed with invalid parameters."""


class DocumentStore:
    """Simulated knowledge base for a RAG pipeline.

    Retrieval is index-based (deterministic), not semantic.
    """

    def __init__(self, num_docs: int, tokens_per_doc: int) -> None:
        if num_docs < 1:
            raise DocumentStoreParamError(
                f"num_docs must be >= 1, got {num_docs!r}"
            )
        if tokens_per_doc < 1:
            raise DocumentStoreParamError(
                f"tokens_per_doc must be >= 1, got {tokens_per_doc!r}"
            )
        self._docs: list[Document] = [
            Document(id=f"doc-{i + 1:03d}", token_count=tokens_per_doc)
            for i in range(num_docs)
        ]

    @property
    def num_docs(self) -> int:
        """Return the number of documents in the store."""
        return len(self._docs)

    def retrieve(self, doc_indices: Sequence[int]) -> list[Document]:
        """Return documents at valid (in-range) indices; silently skip out-of-range.

        Returns an empty list if no index is in range.
        """
        result: list[Document] = []
        for idx in doc_indices:
            if 0 <= idx < len(self._docs):
                result.append(self._docs[idx])
        return result


# ---------------------------------------------------------------------------
# RAGConfig, RAGConfigError, RAGResult
# ---------------------------------------------------------------------------

class RAGConfigError(ValueError):
    """Raised when a RAGConfig field is out of its accepted range."""


@dataclass(frozen=True)
class RAGConfig:
    """Controls the RAG simulation workload."""

    model: ModelConfig
    num_documents: int    # 1..1_000_000
    tokens_per_doc: int   # 1..1_000_000
    top_k: int            # 1..num_documents
    query_tokens: int     # 0..1_000_000
    num_requests: int     # 1..10_000_000
    zipf_exponent: float  # > 0.0
    requests_per_day: int # 1..1_000_000_000

    def validate(self) -> None:
        """Validate all fields; raise RAGConfigError naming the first invalid field."""
        if not (1 <= self.num_documents <= 1_000_000):
            raise RAGConfigError(
                f"num_documents must be in [1, 1_000_000], got {self.num_documents!r}"
            )
        if not (1 <= self.tokens_per_doc <= 1_000_000):
            raise RAGConfigError(
                f"tokens_per_doc must be in [1, 1_000_000], got {self.tokens_per_doc!r}"
            )
        if not (1 <= self.top_k <= self.num_documents):
            raise RAGConfigError(
                f"top_k must be in [1, num_documents={self.num_documents}], "
                f"got {self.top_k!r}"
            )
        if not (0 <= self.query_tokens <= 1_000_000):
            raise RAGConfigError(
                f"query_tokens must be in [0, 1_000_000], got {self.query_tokens!r}"
            )
        if not (1 <= self.num_requests <= 10_000_000):
            raise RAGConfigError(
                f"num_requests must be in [1, 10_000_000], got {self.num_requests!r}"
            )
        if not (self.zipf_exponent > 0.0):
            raise RAGConfigError(
                f"zipf_exponent must be > 0.0, got {self.zipf_exponent!r}"
            )
        if not (1 <= self.requests_per_day <= 1_000_000_000):
            raise RAGConfigError(
                f"requests_per_day must be in [1, 1_000_000_000], "
                f"got {self.requests_per_day!r}"
            )


@dataclass(frozen=True)
class RAGResult:
    """Outcome of a RAG simulation run."""

    config: RAGConfig
    stats: CacheStats
    prefix_tokens: int
    cost_per_miss_usd: float
    without_cache_usd: float
    with_cache_usd: float
    savings_usd: float
    savings_pct: float
    per_req_without_usd: float
    per_req_with_usd: float
    monthly_requests: int
    monthly_without: float
    monthly_with: float
    monthly_savings: float


# ---------------------------------------------------------------------------
# build_prefix_tokens and new_entry_for_token_count
# ---------------------------------------------------------------------------

def build_prefix_tokens(doc_indices: Sequence[int], query_tokens: int) -> list[int]:
    """Encode a retrieved document set into a deterministic token-ID sequence.

    1. De-duplicate and sort ascending.
    2. For each index, append 8 marker tokens: [500000 + idx] * 8.
    3. Append query block: [900000 + j for j in range(query_tokens)].
    4. With no indices, return only the query block.
    """
    unique_sorted = sorted(set(doc_indices))
    tokens: list[int] = []
    for idx in unique_sorted:
        tokens.extend([500000 + idx] * 8)
    tokens.extend(900000 + j for j in range(query_tokens))
    return tokens


def new_entry_for_token_count(
    prefix_hash: str, token_count: int, model: ModelConfig
) -> CacheEntry:
    """Build a CacheEntry whose cost reflects token_count tokens on model.

    Uses model.attention_tflops(token_count) and model.cost_usd(token_count).
    """
    return CacheEntry(
        prefix_hash=prefix_hash,
        model=model.name,
        token_count=token_count,
        tflops_cost=model.attention_tflops(token_count),
        cost_usd=model.cost_usd(token_count),
    )


# ---------------------------------------------------------------------------
# run_rag_simulation
# ---------------------------------------------------------------------------

def run_rag_simulation(cfg: RAGConfig, *, seed: int = 42) -> RAGResult:
    """Simulate a RAG pipeline with Zipf document popularity and prefix caching.

    Algorithm:
    1. Validate cfg.
    2. Build DocumentStore and PrefixCache(100_000).
    3. For each request: sample top_k indices via Zipf rejection sampling,
       build prefix, hash, lookup; on miss store entry.
    4. Compute cost and monthly projection math.
    """
    cfg.validate()

    store = DocumentStore(cfg.num_documents, cfg.tokens_per_doc)
    cache = PrefixCache(100_000)

    prefix_tokens = cfg.top_k * cfg.tokens_per_doc + cfg.query_tokens
    rng = np.random.default_rng(seed)
    batch_size = cfg.top_k * 4  # oversample to reduce rejection waste

    for _ in range(cfg.num_requests):
        # Sample top_k document indices via Zipf rejection sampling
        indices: list[int] = []
        while len(indices) < cfg.top_k:
            raw = rng.zipf(cfg.zipf_exponent + 1, size=batch_size)  # values >= 1
            zero_based = raw - 1  # convert to 0-based
            valid = zero_based[zero_based < cfg.num_documents]  # reject out-of-range
            indices.extend(valid.tolist())
        doc_indices = indices[: cfg.top_k]

        # Build prefix token sequence (exact-set matching: dedup + sort inside)
        prefix = build_prefix_tokens(doc_indices, cfg.query_tokens)

        # Hash, lookup; on miss store entry
        h = hash_prefix(prefix)
        if cache.lookup(h) is None:
            cache.store(new_entry_for_token_count(h, prefix_tokens, cfg.model))

    stats = cache.stats()

    # Cost math
    cost_per_miss = cfg.model.cost_usd(prefix_tokens)
    without_cache = cost_per_miss * cfg.num_requests
    with_cache = cost_per_miss * stats.cache_misses
    savings_usd = without_cache - with_cache
    savings_pct = (savings_usd / without_cache * 100) if without_cache > 0 else 0.0
    per_req_without = without_cache / cfg.num_requests  # num_requests >= 1 after validate
    per_req_with = with_cache / cfg.num_requests

    # Monthly projection
    monthly_requests = cfg.requests_per_day * 30
    monthly_without = per_req_without * monthly_requests
    monthly_with = per_req_with * monthly_requests
    monthly_savings = monthly_without - monthly_with

    return RAGResult(
        config=cfg,
        stats=stats,
        prefix_tokens=prefix_tokens,
        cost_per_miss_usd=cost_per_miss,
        without_cache_usd=without_cache,
        with_cache_usd=with_cache,
        savings_usd=savings_usd,
        savings_pct=savings_pct,
        per_req_without_usd=per_req_without,
        per_req_with_usd=per_req_with,
        monthly_requests=monthly_requests,
        monthly_without=monthly_without,
        monthly_with=monthly_with,
        monthly_savings=monthly_savings,
    )


# ---------------------------------------------------------------------------
# default_rag_config
# ---------------------------------------------------------------------------

def default_rag_config() -> RAGConfig:
    """Return the default RAG config matching the Go reference (rag.go).

    enterprise document-QA assistant over a 200-document knowledge base.
    """
    return RAGConfig(
        model=LLAMA3_70B,
        num_documents=200,
        tokens_per_doc=512,
        top_k=3,
        query_tokens=64,
        num_requests=5000,
        zipf_exponent=1.2,
        requests_per_day=1_000_000,
    )
