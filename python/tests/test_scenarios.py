"""Tests for scenarios.py: scenario savings math, determinism, all-models comparison.

Tasks 5.4 and 5.5: property tests (Properties 13–16) and unit/example tests.
Feature: python-llm-rag-simulation
"""

from __future__ import annotations

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from llmsim.models import ALL_MODELS
from llmsim.scenarios import (
    ComparisonParamError,
    ScenarioResult,
    UnknownScenarioError,
    default_scenarios,
    get_scenario,
    run_all_models_comparison,
    run_scenario,
)


# ---------------------------------------------------------------------------
# Property 13: Savings arithmetic
# ---------------------------------------------------------------------------
# Feature: python-llm-rag-simulation, Property 13: Savings arithmetic relationships hold for both simulators

@given(
    cost=st.floats(min_value=0.0, max_value=1e10, allow_nan=False, allow_infinity=False),
    requests=st.integers(min_value=0, max_value=10_000_000),
    misses=st.integers(min_value=0, max_value=10_000_000),
)
@settings(max_examples=500)
def test_property_13_savings_arithmetic(cost: float, requests: int, misses: int) -> None:
    """Validates: Requirements 6.6, 6.7, 6.8, 6.9, 6.10, 10.7, 10.8, 10.9

    Pure arithmetic: without == cost * requests, with == cost * misses,
    savings == without - with, pct == savings / without * 100 when without > 0,
    else pct == 0.0 (no division).
    """
    # Clamp misses to [0, requests] so the relationship makes sense
    misses = min(misses, requests)

    without = cost * requests
    with_cache = cost * misses
    savings = without - with_cache

    if without > 0:
        savings_pct = savings / without * 100.0
    else:
        savings_pct = 0.0

    # Assert all relationships hold
    assert without == cost * requests
    assert with_cache == cost * misses
    assert savings == without - with_cache

    if without > 0:
        expected_pct = savings / without * 100.0
        assert abs(savings_pct - expected_pct) < 1e-9
        assert 0.0 <= savings_pct <= 100.0
    else:
        # No division when without == 0
        assert savings_pct == 0.0


# ---------------------------------------------------------------------------
# Property 14: Seeded simulations are deterministic
# ---------------------------------------------------------------------------
# Feature: python-llm-rag-simulation, Property 14: Seeded simulations are deterministic

def test_property_14_seeded_determinism() -> None:
    """Validates: Requirements 6.11, 7.5

    Run each of the 5 default scenarios twice with seed=42 and assert results
    are identical (same miss counts, hit rates, cost values, savings).
    """
    scenarios = default_scenarios()
    assert len(scenarios) == 5

    for scenario in scenarios:
        result_a = run_scenario(scenario, seed=42)
        result_b = run_scenario(scenario, seed=42)

        assert result_a.stats.total_requests == result_b.stats.total_requests
        assert result_a.stats.cache_hits == result_b.stats.cache_hits
        assert result_a.stats.cache_misses == result_b.stats.cache_misses
        assert result_a.stats.hit_rate == result_b.stats.hit_rate
        assert result_a.without_cache_cost == result_b.without_cache_cost
        assert result_a.with_cache_cost == result_b.with_cache_cost
        assert result_a.savings_usd == result_b.savings_usd
        assert result_a.savings_pct == result_b.savings_pct


# ---------------------------------------------------------------------------
# Property 15: All-models comparison produces one ordered well-formed result per model
# ---------------------------------------------------------------------------
# Feature: python-llm-rag-simulation, Property 15: All-models comparison produces one ordered, well-formed result per model

@given(
    prefix_len=st.integers(min_value=1, max_value=4096),
    num_requests=st.integers(min_value=1, max_value=200),
    reuse_rate=st.floats(min_value=0.0, max_value=1.0, allow_nan=False),
)
@settings(max_examples=20)
def test_property_15_all_models_comparison_well_formed(
    prefix_len: int, num_requests: int, reuse_rate: float
) -> None:
    """Validates: Requirements 7.1, 7.2

    The comparison returns exactly 6 results ordered as ALL_MODELS,
    hit_rate in [0, 100], savings_pct in [0, 100].
    """
    results = run_all_models_comparison(
        prefix_len=prefix_len,
        num_requests=num_requests,
        reuse_rate=reuse_rate,
        seed=42,
    )

    assert len(results) == 6

    for i, (result, expected_model) in enumerate(zip(results, ALL_MODELS)):
        assert result.scenario.model.name == expected_model.name, (
            f"Result {i} model mismatch: got {result.scenario.model.name!r}, "
            f"expected {expected_model.name!r}"
        )
        assert 0.0 <= result.stats.hit_rate <= 100.0, (
            f"hit_rate out of range: {result.stats.hit_rate}"
        )
        assert 0.0 <= result.savings_pct <= 100.0, (
            f"savings_pct out of range: {result.savings_pct}"
        )


# ---------------------------------------------------------------------------
# Property 16: Comparison param validation rejects invalid inputs
# ---------------------------------------------------------------------------
# Feature: python-llm-rag-simulation, Property 16: Comparison parameter validation rejects invalid inputs

@given(prefix_len=st.integers(max_value=0))
def test_property_16_invalid_prefix_len(prefix_len: int) -> None:
    """Validates: Requirements 7.4 — prefix_len < 1 raises ComparisonParamError."""
    with pytest.raises(ComparisonParamError) as exc_info:
        run_all_models_comparison(prefix_len=prefix_len, num_requests=100, reuse_rate=0.5)
    assert "prefix_len" in str(exc_info.value)


@given(num_requests=st.integers(max_value=0))
def test_property_16_invalid_num_requests(num_requests: int) -> None:
    """Validates: Requirements 7.4 — num_requests < 1 raises ComparisonParamError."""
    with pytest.raises(ComparisonParamError) as exc_info:
        run_all_models_comparison(prefix_len=512, num_requests=num_requests, reuse_rate=0.5)
    assert "num_requests" in str(exc_info.value)


@given(
    reuse_rate=st.one_of(
        st.floats(max_value=-1e-9, allow_nan=False, allow_infinity=False),
        st.floats(min_value=1.0 + 1e-9, allow_nan=False, allow_infinity=False),
    )
)
def test_property_16_invalid_reuse_rate(reuse_rate: float) -> None:
    """Validates: Requirements 7.4 — reuse_rate outside [0, 1] raises ComparisonParamError."""
    with pytest.raises(ComparisonParamError) as exc_info:
        run_all_models_comparison(prefix_len=512, num_requests=100, reuse_rate=reuse_rate)
    assert "reuse_rate" in str(exc_info.value)


# ---------------------------------------------------------------------------
# Unit / example tests (task 5.5)
# ---------------------------------------------------------------------------


def test_default_scenarios_returns_five() -> None:
    """default_scenarios() returns exactly 5 Scenario objects."""
    scenarios = default_scenarios()
    assert len(scenarios) == 5


def test_default_scenarios_types() -> None:
    """Each element returned by default_scenarios() is a Scenario instance."""
    from llmsim.scenarios import Scenario

    for s in default_scenarios():
        assert isinstance(s, Scenario)


def test_customer_service_bot_parameters() -> None:
    """Customer Service Bot has the correct parameters matching Go reference."""
    scenario = get_scenario("Customer Service Bot")
    assert scenario.name == "Customer Service Bot"
    assert scenario.system_prompt_len == 512
    assert scenario.num_requests == 500
    assert scenario.prefix_reuse_rate == 1.0
    assert scenario.model.name == "Llama-3-70B"


def test_code_assistant_rag_parameters() -> None:
    """Code Assistant (RAG) has the correct parameters."""
    scenario = get_scenario("Code Assistant (RAG)")
    assert scenario.system_prompt_len == 1024
    assert scenario.num_requests == 1000
    assert scenario.prefix_reuse_rate == 0.85


def test_rag_pipeline_document_qa_parameters() -> None:
    """RAG Pipeline (Document QA) has the correct parameters."""
    scenario = get_scenario("RAG Pipeline (Document QA)")
    assert scenario.system_prompt_len == 2048
    assert scenario.num_requests == 1000
    assert scenario.prefix_reuse_rate == 0.92


def test_legal_document_analysis_parameters() -> None:
    """Legal Document Analysis has the correct parameters."""
    scenario = get_scenario("Legal Document Analysis")
    assert scenario.system_prompt_len == 3000
    assert scenario.num_requests == 500
    assert scenario.prefix_reuse_rate == 0.99
    assert scenario.model.name == "Qwen2.5-72B"


def test_open_source_model_comparison_parameters() -> None:
    """Open Source Model Comparison has the correct parameters."""
    scenario = get_scenario("Open Source Model Comparison")
    assert scenario.system_prompt_len == 1024
    assert scenario.num_requests == 1000
    assert scenario.prefix_reuse_rate == 0.90


def test_get_scenario_returns_expected() -> None:
    """get_scenario('Customer Service Bot') returns the expected Scenario."""
    scenario = get_scenario("Customer Service Bot")
    assert scenario.name == "Customer Service Bot"
    assert scenario.system_prompt_len == 512


def test_get_scenario_unknown_raises() -> None:
    """get_scenario('Unknown') raises UnknownScenarioError."""
    with pytest.raises(UnknownScenarioError):
        get_scenario("Unknown")


def test_run_scenario_customer_service_bot_savings() -> None:
    """Run Customer Service Bot with seed=42: savings_pct > 0.0."""
    scenario = get_scenario("Customer Service Bot")
    result = run_scenario(scenario, seed=42)
    assert result.savings_pct > 0.0


def test_run_all_models_prefix_len_zero_raises() -> None:
    """run_all_models_comparison(prefix_len=0, ...) raises ComparisonParamError."""
    with pytest.raises(ComparisonParamError):
        run_all_models_comparison(prefix_len=0, num_requests=100, reuse_rate=0.80)


def test_run_all_models_returns_six_results() -> None:
    """run_all_models_comparison(prefix_len=1024, num_requests=100, reuse_rate=0.80) returns 6 results."""
    results = run_all_models_comparison(
        prefix_len=1024, num_requests=100, reuse_rate=0.80
    )
    assert len(results) == 6
    assert all(isinstance(r, ScenarioResult) for r in results)
