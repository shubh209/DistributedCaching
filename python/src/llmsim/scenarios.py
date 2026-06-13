"""Scenario simulator: scenarios and all-models comparison."""

from __future__ import annotations

from dataclasses import dataclass

import numpy as np

from llmsim.models import (
    ModelConfig,
    ALL_MODELS,
    LLAMA3_8B,
    LLAMA3_70B,
    QWEN2_5_72B,
    cost_usd_vec,
)
from llmsim.prefix_cache import (
    PrefixCache,
    CacheStats,
    new_entry_for_prefix,
    hash_prefix,
)

__all__ = [
    "Scenario",
    "ScenarioResult",
    "UnknownScenarioError",
    "ComparisonParamError",
    "SEED",
    "default_scenarios",
    "get_scenario",
    "run_scenario",
    "run_all_models_comparison",
]

# Default RNG seed — matches the Go reference (seed 42).
SEED: int = 42


# ---------------------------------------------------------------------------
# Error types
# ---------------------------------------------------------------------------

class UnknownScenarioError(ValueError):
    """Raised when a scenario name is not found in the default catalog."""


class ComparisonParamError(ValueError):
    """Raised when run_all_models_comparison receives an invalid parameter."""


# ---------------------------------------------------------------------------
# Dataclasses
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class Scenario:
    """Description of a single simulation workload (Req 6.1, 6.2).

    All numeric ranges are validated at run time by run_scenario.
    Fields intentionally mirror the Go ``Scenario`` struct.
    """

    name: str
    description: str
    company: str
    system_prompt_len: int    # 1..1_000_000 tokens
    user_message_len: int     # 1..1_000_000 tokens
    num_requests: int         # 1..10_000_000
    prefix_reuse_rate: float  # 0.0..1.0
    model: ModelConfig


@dataclass(frozen=True)
class ScenarioResult:
    """Output of one scenario simulation run (Req 6.6–6.10, 7.2)."""

    scenario: Scenario
    stats: CacheStats
    without_cache_cost: float
    with_cache_cost: float
    savings_usd: float
    savings_pct: float


# ---------------------------------------------------------------------------
# Default scenario catalog — must match Go DefaultScenarios() exactly
# ---------------------------------------------------------------------------

def default_scenarios() -> list[Scenario]:
    """Return the five pre-configured simulation scenarios (Req 6.1).

    Scenario parameters match the Go ``DefaultScenarios()`` function in
    ``internal/llm/simulator.go``.
    """
    return [
        Scenario(
            name="Customer Service Bot",
            description=(
                "500 users share the same 512-token system prompt. "
                "User messages vary."
            ),
            company="SaaS companies (Salesforce, HubSpot, Zendesk)",
            system_prompt_len=512,
            user_message_len=64,
            num_requests=500,
            prefix_reuse_rate=1.0,
            model=LLAMA3_70B,
        ),
        Scenario(
            name="Code Assistant (RAG)",
            description=(
                "200 developers, same 1024-token codebase context prepended "
                "to each query."
            ),
            company="AI coding tools (Cursor, GitHub Copilot, Sourcegraph)",
            system_prompt_len=1024,
            user_message_len=128,
            num_requests=1000,
            prefix_reuse_rate=0.85,
            model=LLAMA3_70B,
        ),
        Scenario(
            name="RAG Pipeline (Document QA)",
            description=(
                "1000 queries over the same 2048-token retrieved document. "
                "Maximum prefix reuse."
            ),
            company="Enterprise AI (Glean, Notion AI, Perplexity Enterprise)",
            system_prompt_len=2048,
            user_message_len=96,
            num_requests=1000,
            prefix_reuse_rate=0.92,
            model=LLAMA3_70B,
        ),
        Scenario(
            name="Legal Document Analysis",
            description=(
                "500 queries over the same 3000-token legal context. "
                "Fixed domain knowledge prefix."
            ),
            company="Vertical AI (Harvey, Lexion, ContractPodAi)",
            system_prompt_len=3000,
            user_message_len=128,
            num_requests=500,
            prefix_reuse_rate=0.99,
            model=QWEN2_5_72B,
        ),
        Scenario(
            name="Open Source Model Comparison",
            description=(
                "1000 requests across all 6 open-source models with "
                "1024-token shared prefix."
            ),
            company="LLM API providers (Groq, Together.ai, Fireworks.ai)",
            system_prompt_len=1024,
            user_message_len=64,
            num_requests=1000,
            prefix_reuse_rate=0.90,
            model=LLAMA3_8B,
        ),
    ]


def get_scenario(name: str) -> Scenario:
    """Return the named scenario from the default catalog (Req 6.12).

    Raises:
        UnknownScenarioError: if *name* does not match any default scenario.
    """
    for s in default_scenarios():
        if s.name == name:
            return s
    valid = [s.name for s in default_scenarios()]
    raise UnknownScenarioError(
        f"Unknown scenario {name!r}. Valid names: {valid}"
    )


# ---------------------------------------------------------------------------
# run_scenario
# ---------------------------------------------------------------------------

def run_scenario(scenario: Scenario, *, seed: int = SEED) -> ScenarioResult:
    """Simulate *scenario* and return a ScenarioResult (Req 6.3–6.11).

    Algorithm:
    1. Build a shared prefix of ``system_prompt_len`` sequential token IDs
       (offset 1000, matching Go).
    2. Pre-populate a PrefixCache(10000) with the shared prefix.
    3. For each request draw ``rng.random() < prefix_reuse_rate``; on True use
       the shared prefix, on False generate a unique prefix with token IDs in
       [200000, 300000) (mirrors Go's ``rng.Intn(100000) + 200000``).
    4. Hash the prefix, lookup; on miss store a new entry.
    5. Compute cost math from stats.cache_misses.

    A fresh seeded ``numpy.random.default_rng(seed)`` per call guarantees
    determinism (Req 6.11).
    """
    model = scenario.model
    rng = np.random.default_rng(seed)

    # --- Build shared prefix (step 1) ---
    shared_prefix: list[int] = [1000 + i for i in range(scenario.system_prompt_len)]

    # --- Pre-populate cache (step 2) ---
    cache = PrefixCache(10000)
    cache.store(new_entry_for_prefix(shared_prefix, model))

    shared_hash = hash_prefix(shared_prefix)

    # --- Simulate requests (steps 3–4) ---
    for _ in range(scenario.num_requests):
        if rng.random() < scenario.prefix_reuse_rate:
            prefix_tokens = shared_prefix
            h = shared_hash
        else:
            # Unique prefix: token IDs uniform in [200000, 300000)
            prefix_tokens = rng.integers(
                200000, 300000, size=scenario.system_prompt_len
            ).tolist()
            h = hash_prefix(prefix_tokens)

        if cache.lookup(h) is None:
            # Cache miss — store for future requests
            cache.store(new_entry_for_prefix(prefix_tokens, model))

    # --- Cost math (step 5) ---
    stats = cache.stats()
    prefix_cost = model.cost_usd(scenario.system_prompt_len)
    without_cache = prefix_cost * scenario.num_requests
    with_cache = prefix_cost * stats.cache_misses
    savings_usd = without_cache - with_cache
    if without_cache > 0:
        savings_pct = savings_usd / without_cache * 100.0
    else:
        savings_pct = 0.0

    return ScenarioResult(
        scenario=scenario,
        stats=stats,
        without_cache_cost=without_cache,
        with_cache_cost=with_cache,
        savings_usd=savings_usd,
        savings_pct=savings_pct,
    )


# ---------------------------------------------------------------------------
# run_all_models_comparison
# ---------------------------------------------------------------------------

def run_all_models_comparison(
    prefix_len: int,
    num_requests: int,
    reuse_rate: float,
    *,
    seed: int = SEED,
) -> list[ScenarioResult]:
    """Run the same workload across every model in ALL_MODELS (Req 7.1–7.5).

    Parameters
    ----------
    prefix_len:
        System-prompt token count shared across all model runs (>= 1).
    num_requests:
        Number of simulated requests per model (>= 1).
    reuse_rate:
        Fraction of requests that re-use the shared prefix (0.0..1.0).
    seed:
        RNG seed for reproducibility (Req 7.5).

    Returns
    -------
    list[ScenarioResult]
        One result per model, in ALL_MODELS catalog order (Req 7.2).

    Raises
    ------
    ComparisonParamError:
        If any parameter is out of range (Req 7.4).  Raised **before** any
        simulation work.
    """
    # --- Input validation (Req 7.4) --- must happen before any computation ---
    if prefix_len < 1:
        raise ComparisonParamError(
            f"prefix_len must be >= 1, got {prefix_len}"
        )
    if num_requests < 1:
        raise ComparisonParamError(
            f"num_requests must be >= 1, got {num_requests}"
        )
    if not (0.0 <= reuse_rate <= 1.0):
        raise ComparisonParamError(
            f"reuse_rate must be in [0.0, 1.0], got {reuse_rate}"
        )

    # --- NumPy parity assertion (Req 7.3) ---
    vec = cost_usd_vec(ALL_MODELS, prefix_len)
    scalar = [m.cost_usd(prefix_len) for m in ALL_MODELS]
    assert np.allclose(vec, scalar, rtol=1e-9), (
        "NumPy vectorized cost deviates from scalar cost beyond rtol=1e-9"
    )

    # --- Run one scenario per model in ALL_MODELS order (Req 7.1, 7.2) ---
    results: list[ScenarioResult] = []
    for model in ALL_MODELS:
        scenario = Scenario(
            name=model.name,
            description="",
            company="",
            system_prompt_len=prefix_len,
            user_message_len=64,
            num_requests=num_requests,
            prefix_reuse_rate=reuse_rate,
            model=model,
        )
        results.append(run_scenario(scenario, seed=seed))

    return results
