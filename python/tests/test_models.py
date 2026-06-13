"""Tests for models.py: FLOP/TFLOPs/KV/cost math, NumPy parity, boundaries.

Covers:
- Property tests (Hypothesis) for Properties 1, 2, 3, 4
- Unit/example tests for all 6 model catalog values, ALL_MODELS order,
  FLOP reference values, KV cache size, zero seq_len boundary, error boundaries
"""

from __future__ import annotations

import math

import numpy as np
import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from llmsim.models import (
    ALL_MODELS,
    DEEPSEEK_R1,
    LLAMA3_70B,
    LLAMA3_8B,
    MISTRAL_7B,
    MIXTRAL_8X7B,
    MAX_SEQ_LEN,
    QWEN2_5_72B,
    ModelConfig,
    SeqLenRangeError,
    UnknownModelError,
    attention_tflops_vec,
    cost_usd_vec,
    get_model,
    validate_seq_len,
)


# ===========================================================================
# Property Tests
# ===========================================================================

# Feature: python-llm-rag-simulation, Property 1: Analytic attention and cost math match the reference formulas
@given(
    model=st.sampled_from(ALL_MODELS),
    seq_len=st.integers(0, MAX_SEQ_LEN),
)
@settings(max_examples=200)
def test_property1_analytic_math_matches_reference_formulas(
    model: ModelConfig, seq_len: int
) -> None:
    """Property 1: attention_flops, tflops, cost_usd, kv_cache_size match formulas.

    Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.6
    """
    expected_flops = model.num_layers * (
        4 * model.num_q_heads * model.head_dim * seq_len ** 2
    )
    expected_tflops = expected_flops / 1e12
    expected_cost = expected_tflops / 312.0 / 3600.0 * 2.50
    expected_kv = 2 * model.num_kv_heads * model.head_dim * 2 * model.num_layers

    assert math.isclose(
        model.attention_flops(seq_len), expected_flops, rel_tol=1e-9
    ), f"attention_flops mismatch for {model.name}, seq_len={seq_len}"

    assert math.isclose(
        model.attention_tflops(seq_len), expected_tflops, rel_tol=1e-9
    ), f"attention_tflops mismatch for {model.name}, seq_len={seq_len}"

    assert math.isclose(
        model.cost_usd(seq_len), expected_cost, rel_tol=1e-9
    ), f"cost_usd mismatch for {model.name}, seq_len={seq_len}"

    assert model.kv_cache_size_per_token() == expected_kv, (
        f"kv_cache_size_per_token mismatch for {model.name}"
    )

    # At seq_len == 0 all cost/compute values are exactly 0.0
    if seq_len == 0:
        assert model.attention_flops(0) == 0.0
        assert model.attention_tflops(0) == 0.0
        assert model.cost_usd(0) == 0.0


# Feature: python-llm-rag-simulation, Property 2: NumPy vectorized cost equals the scalar cost within tolerance
@given(seq_len=st.integers(0, MAX_SEQ_LEN))
@settings(max_examples=200)
def test_property2_numpy_vectorized_cost_equals_scalar(seq_len: int) -> None:
    """Property 2: cost_usd_vec(ALL_MODELS, seq_len) matches scalar per-model cost_usd.

    Validates: Requirements 2.5, 7.3
    """
    scalar_costs = [m.cost_usd(seq_len) for m in ALL_MODELS]
    vec_costs = cost_usd_vec(ALL_MODELS, seq_len)

    assert np.allclose(vec_costs, scalar_costs, rtol=1e-9), (
        f"NumPy vectorized costs differ from scalar costs at seq_len={seq_len}:\n"
        f"  vec={vec_costs}\n  scalar={scalar_costs}"
    )

    # Also check attention_tflops_vec parity
    scalar_tflops = [m.attention_tflops(seq_len) for m in ALL_MODELS]
    vec_tflops = attention_tflops_vec(ALL_MODELS, seq_len)
    assert np.allclose(vec_tflops, scalar_tflops, rtol=1e-9), (
        f"NumPy vectorized tflops differ from scalar tflops at seq_len={seq_len}"
    )


# Feature: python-llm-rag-simulation, Property 3: Sequence-length validation rejects out-of-range and non-integer inputs
@given(seq_len=st.integers(max_value=-1))
def test_property3_negative_int_raises_seq_len_range_error(seq_len: int) -> None:
    """Property 3 (negative int): validate_seq_len raises SeqLenRangeError.

    Validates: Requirement 2.7
    """
    with pytest.raises(SeqLenRangeError):
        validate_seq_len(seq_len)


@given(seq_len=st.integers(min_value=MAX_SEQ_LEN + 1))
def test_property3_too_large_int_raises_seq_len_range_error(seq_len: int) -> None:
    """Property 3 (int > MAX_SEQ_LEN): validate_seq_len raises SeqLenRangeError.

    Validates: Requirement 2.7
    """
    with pytest.raises(SeqLenRangeError):
        validate_seq_len(seq_len)


@given(seq_len=st.floats(allow_nan=False, allow_infinity=False))
def test_property3_float_raises_seq_len_range_error(seq_len: float) -> None:
    """Property 3 (float): validate_seq_len raises SeqLenRangeError.

    Validates: Requirement 2.7
    """
    with pytest.raises(SeqLenRangeError):
        validate_seq_len(seq_len)  # type: ignore[arg-type]


@given(seq_len=st.booleans())
def test_property3_bool_raises_seq_len_range_error(seq_len: bool) -> None:
    """Property 3 (bool): validate_seq_len raises SeqLenRangeError.

    Booleans are int subclasses but must be rejected (Req 2.7).
    Validates: Requirement 2.7
    """
    with pytest.raises(SeqLenRangeError):
        validate_seq_len(seq_len)  # type: ignore[arg-type]


# Feature: python-llm-rag-simulation, Property 4: Unknown model and scenario lookups raise errors
@given(name=st.text())
def test_property4_unknown_model_raises_unknown_model_error(name: str) -> None:
    """Property 4: get_model raises UnknownModelError for any name not in ALL_MODELS.

    Validates: Requirements 1.10
    """
    valid_names = {m.name for m in ALL_MODELS}
    if name not in valid_names:
        with pytest.raises(UnknownModelError):
            get_model(name)
    else:
        # Valid name must not raise
        result = get_model(name)
        assert result.name == name


# ===========================================================================
# Unit / Example Tests — Model Catalog Values
# ===========================================================================

class TestModelCatalogValues:
    """Assert each of the 6 ModelConfig constants matches Requirements 1.2–1.7."""

    def test_llama3_8b_fields(self) -> None:
        """Req 1.2: Llama-3-8B — 32 layers, 8 KV heads, 32 Q heads, dim 128, 8.0B params."""
        assert LLAMA3_8B.name == "Llama-3-8B"
        assert LLAMA3_8B.num_layers == 32
        assert LLAMA3_8B.num_kv_heads == 8
        assert LLAMA3_8B.num_q_heads == 32
        assert LLAMA3_8B.head_dim == 128
        assert LLAMA3_8B.params_billion == 8.0

    def test_llama3_70b_fields(self) -> None:
        """Req 1.3: Llama-3-70B — 80 layers, 8 KV heads, 64 Q heads, dim 128, 70.0B params."""
        assert LLAMA3_70B.name == "Llama-3-70B"
        assert LLAMA3_70B.num_layers == 80
        assert LLAMA3_70B.num_kv_heads == 8
        assert LLAMA3_70B.num_q_heads == 64
        assert LLAMA3_70B.head_dim == 128
        assert LLAMA3_70B.params_billion == 70.0

    def test_mistral_7b_fields(self) -> None:
        """Req 1.4: Mistral-7B — 32 layers, 8 KV heads, 32 Q heads, dim 128, 7.0B params."""
        assert MISTRAL_7B.name == "Mistral-7B"
        assert MISTRAL_7B.num_layers == 32
        assert MISTRAL_7B.num_kv_heads == 8
        assert MISTRAL_7B.num_q_heads == 32
        assert MISTRAL_7B.head_dim == 128
        assert MISTRAL_7B.params_billion == 7.0

    def test_mixtral_8x7b_fields(self) -> None:
        """Req 1.5: Mixtral-8x7B — 32 layers, 8 KV heads, 32 Q heads, dim 128, 46.7B params."""
        assert MIXTRAL_8X7B.name == "Mixtral-8x7B"
        assert MIXTRAL_8X7B.num_layers == 32
        assert MIXTRAL_8X7B.num_kv_heads == 8
        assert MIXTRAL_8X7B.num_q_heads == 32
        assert MIXTRAL_8X7B.head_dim == 128
        assert MIXTRAL_8X7B.params_billion == 46.7

    def test_qwen2_5_72b_fields(self) -> None:
        """Req 1.6: Qwen2.5-72B — 80 layers, 8 KV heads, 64 Q heads, dim 128, 72.0B params."""
        assert QWEN2_5_72B.name == "Qwen2.5-72B"
        assert QWEN2_5_72B.num_layers == 80
        assert QWEN2_5_72B.num_kv_heads == 8
        assert QWEN2_5_72B.num_q_heads == 64
        assert QWEN2_5_72B.head_dim == 128
        assert QWEN2_5_72B.params_billion == 72.0

    def test_deepseek_r1_fields(self) -> None:
        """Req 1.7: DeepSeek-R1-671B — 61 layers, 128 KV heads, 128 Q heads, dim 128, 671.0B params."""
        assert DEEPSEEK_R1.name == "DeepSeek-R1-671B"
        assert DEEPSEEK_R1.num_layers == 61
        assert DEEPSEEK_R1.num_kv_heads == 128
        assert DEEPSEEK_R1.num_q_heads == 128
        assert DEEPSEEK_R1.head_dim == 128
        assert DEEPSEEK_R1.params_billion == 671.0


# ===========================================================================
# Unit / Example Tests — ALL_MODELS order and length
# ===========================================================================

class TestAllModels:
    """Assert ALL_MODELS has length 6 and the correct order (Req 1.8)."""

    def test_all_models_length(self) -> None:
        assert len(ALL_MODELS) == 6

    def test_all_models_order(self) -> None:
        """Req 1.8: 8B → 70B → Mistral → Mixtral → Qwen → DeepSeek."""
        assert ALL_MODELS[0] is LLAMA3_8B
        assert ALL_MODELS[1] is LLAMA3_70B
        assert ALL_MODELS[2] is MISTRAL_7B
        assert ALL_MODELS[3] is MIXTRAL_8X7B
        assert ALL_MODELS[4] is QWEN2_5_72B
        assert ALL_MODELS[5] is DEEPSEEK_R1


# ===========================================================================
# Unit / Example Tests — FLOP reference value
# ===========================================================================

class TestFlopReferenceValues:
    """Assert LLAMA3_8B.attention_flops(1024) matches the exact Go reference."""

    def test_llama3_8b_attention_flops_1024(self) -> None:
        """LLAMA3_8B: 32 layers, 32 Q heads, head_dim=128, seq_len=1024.

        Expected: 32 * (4 * 32 * 128 * 1024 * 1024) = 32 * 536_870_912 = 17_179_869_184
        """
        expected = 32 * (4 * 32 * 128 * 1024 * 1024)
        assert LLAMA3_8B.attention_flops(1024) == expected

    def test_llama3_8b_attention_tflops_1024(self) -> None:
        expected_flops = 32 * (4 * 32 * 128 * 1024 * 1024)
        expected_tflops = expected_flops / 1e12
        assert math.isclose(
            LLAMA3_8B.attention_tflops(1024), expected_tflops, rel_tol=1e-9
        )

    def test_llama3_8b_cost_usd_1024(self) -> None:
        expected_flops = 32 * (4 * 32 * 128 * 1024 * 1024)
        expected_cost = (expected_flops / 1e12) / 312.0 / 3600.0 * 2.50
        assert math.isclose(
            LLAMA3_8B.cost_usd(1024), expected_cost, rel_tol=1e-9
        )


# ===========================================================================
# Unit / Example Tests — KV cache size per token
# ===========================================================================

class TestKvCacheSize:
    """Assert kv_cache_size_per_token returns the expected byte count (Req 2.4)."""

    def test_llama3_8b_kv_cache_size(self) -> None:
        # 2 * 8 * 128 * 2 * 32 = 131_072
        assert LLAMA3_8B.kv_cache_size_per_token() == 131_072

    def test_llama3_70b_kv_cache_size(self) -> None:
        # 2 * 8 * 128 * 2 * 80 = 327_680
        assert LLAMA3_70B.kv_cache_size_per_token() == 327_680

    def test_mistral_7b_kv_cache_size(self) -> None:
        # 2 * 8 * 128 * 2 * 32 = 131_072
        assert MISTRAL_7B.kv_cache_size_per_token() == 131_072

    def test_mixtral_8x7b_kv_cache_size(self) -> None:
        # 2 * 8 * 128 * 2 * 32 = 131_072
        assert MIXTRAL_8X7B.kv_cache_size_per_token() == 131_072

    def test_qwen2_5_72b_kv_cache_size(self) -> None:
        # 2 * 8 * 128 * 2 * 80 = 327_680
        assert QWEN2_5_72B.kv_cache_size_per_token() == 327_680

    def test_deepseek_r1_kv_cache_size(self) -> None:
        # 2 * 128 * 128 * 2 * 61 = 3_997_696
        assert DEEPSEEK_R1.kv_cache_size_per_token() == 3_997_696


# ===========================================================================
# Unit / Example Tests — Zero seq_len boundary (Req 2.6)
# ===========================================================================

class TestZeroSeqLen:
    """Assert all cost/FLOP methods return 0.0 for seq_len=0 (Req 2.6)."""

    @pytest.mark.parametrize("model", list(ALL_MODELS), ids=lambda m: m.name)
    def test_attention_flops_zero(self, model: ModelConfig) -> None:
        assert model.attention_flops(0) == 0.0

    @pytest.mark.parametrize("model", list(ALL_MODELS), ids=lambda m: m.name)
    def test_attention_tflops_zero(self, model: ModelConfig) -> None:
        assert model.attention_tflops(0) == 0.0

    @pytest.mark.parametrize("model", list(ALL_MODELS), ids=lambda m: m.name)
    def test_cost_usd_zero(self, model: ModelConfig) -> None:
        assert model.cost_usd(0) == 0.0


# ===========================================================================
# Unit / Example Tests — validate_seq_len error boundaries (Req 2.7)
# ===========================================================================

class TestValidateSeqLen:
    """Test all error and valid boundary cases for validate_seq_len."""

    def test_bool_true_raises(self) -> None:
        """True is an int subclass but must be rejected."""
        with pytest.raises(SeqLenRangeError):
            validate_seq_len(True)  # type: ignore[arg-type]

    def test_bool_false_raises(self) -> None:
        """False is an int subclass but must be rejected."""
        with pytest.raises(SeqLenRangeError):
            validate_seq_len(False)  # type: ignore[arg-type]

    def test_float_raises(self) -> None:
        with pytest.raises(SeqLenRangeError):
            validate_seq_len(1.5)  # type: ignore[arg-type]

    def test_float_zero_raises(self) -> None:
        with pytest.raises(SeqLenRangeError):
            validate_seq_len(0.0)  # type: ignore[arg-type]

    def test_negative_one_raises(self) -> None:
        with pytest.raises(SeqLenRangeError):
            validate_seq_len(-1)

    def test_large_negative_raises(self) -> None:
        with pytest.raises(SeqLenRangeError):
            validate_seq_len(-1_000_000)

    def test_max_seq_len_plus_one_raises(self) -> None:
        with pytest.raises(SeqLenRangeError):
            validate_seq_len(MAX_SEQ_LEN + 1)

    def test_zero_is_valid(self) -> None:
        """seq_len=0 is valid and must not raise (Req 2.6)."""
        validate_seq_len(0)  # should not raise

    def test_max_seq_len_is_valid(self) -> None:
        """seq_len=MAX_SEQ_LEN is valid and must not raise."""
        validate_seq_len(MAX_SEQ_LEN)  # should not raise

    def test_one_is_valid(self) -> None:
        validate_seq_len(1)  # should not raise

    def test_midrange_is_valid(self) -> None:
        validate_seq_len(1024)  # should not raise

    def test_string_raises(self) -> None:
        with pytest.raises(SeqLenRangeError):
            validate_seq_len("512")  # type: ignore[arg-type]

    def test_none_raises(self) -> None:
        with pytest.raises(SeqLenRangeError):
            validate_seq_len(None)  # type: ignore[arg-type]


# ===========================================================================
# Unit / Example Tests — get_model lookup
# ===========================================================================

class TestGetModel:
    """Test get_model returns correct config or raises UnknownModelError."""

    @pytest.mark.parametrize("model", list(ALL_MODELS), ids=lambda m: m.name)
    def test_known_model_returns_config(self, model: ModelConfig) -> None:
        result = get_model(model.name)
        assert result is model

    def test_unknown_name_raises(self) -> None:
        with pytest.raises(UnknownModelError):
            get_model("GPT-4")

    def test_empty_string_raises(self) -> None:
        with pytest.raises(UnknownModelError):
            get_model("")

    def test_case_sensitive(self) -> None:
        """Model name lookup is case-sensitive."""
        with pytest.raises(UnknownModelError):
            get_model("llama-3-8b")

    def test_unknown_model_error_is_value_error(self) -> None:
        """UnknownModelError must subclass ValueError."""
        with pytest.raises(ValueError):
            get_model("unknown-model")


# ===========================================================================
# Unit / Example Tests — SeqLenRangeError type hierarchy
# ===========================================================================

class TestErrorTypes:
    """Assert custom error types subclass ValueError."""

    def test_seq_len_range_error_is_value_error(self) -> None:
        with pytest.raises(ValueError):
            validate_seq_len(-1)

    def test_unknown_model_error_is_value_error(self) -> None:
        with pytest.raises(ValueError):
            get_model("not-a-model")
