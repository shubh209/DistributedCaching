"""Model catalog: ModelConfig, FLOP/cost math, NumPy vectorization."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Sequence

import numpy as np

__all__ = [
    "ModelConfig",
    "ALL_MODELS",
    "LLAMA3_8B",
    "LLAMA3_70B",
    "MISTRAL_7B",
    "MIXTRAL_8X7B",
    "QWEN2_5_72B",
    "DEEPSEEK_R1",
    "get_model",
    "UnknownModelError",
    "SeqLenRangeError",
    "validate_seq_len",
    "attention_tflops_vec",
    "cost_usd_vec",
    "MAX_SEQ_LEN",
    "H100_TFLOPS_PER_SEC",
    "H100_HOURLY_COST_USD",
]

# ---------------------------------------------------------------------------
# Module-level constants
# ---------------------------------------------------------------------------

MAX_SEQ_LEN: int = 1_048_576           # 2**20, inclusive upper bound (Req 2.1, 2.7)
H100_TFLOPS_PER_SEC: float = 312.0    # H100 SXM5 fp16 non-sparse peak
H100_HOURLY_COST_USD: float = 2.50    # on-demand $/hr estimate


# ---------------------------------------------------------------------------
# Custom error types
# ---------------------------------------------------------------------------

class UnknownModelError(ValueError):
    """Raised when a model name is not found in the catalog."""


class SeqLenRangeError(ValueError):
    """Raised when seq_len is invalid (non-int, bool, negative, or > MAX_SEQ_LEN)."""


# ---------------------------------------------------------------------------
# ModelConfig dataclass
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class ModelConfig:
    """Transformer architecture parameters for FLOP/cost computation.

    All values sourced from Hugging Face config.json (mirrors Go ModelConfig).
    """

    name: str
    num_layers: int
    num_kv_heads: int
    num_q_heads: int
    head_dim: int
    max_context_len: int
    params_billion: float

    # ------------------------------------------------------------------
    # Methods — stubs for tasks 2.2 and 2.3; raise NotImplementedError
    # ------------------------------------------------------------------

    def attention_flops(self, seq_len: int) -> float:
        """Return attention FLOPs for a sequence of seq_len tokens.

        Formula: num_layers * (4 * num_q_heads * head_dim * seq_len**2)
        Validates seq_len first; returns 0.0 when seq_len == 0 (Req 2.6).
        Raises SeqLenRangeError for invalid seq_len (Req 2.7).
        """
        validate_seq_len(seq_len)
        return float(self.num_layers) * (4.0 * self.num_q_heads * self.head_dim * seq_len * seq_len)

    def attention_tflops(self, seq_len: int) -> float:
        """Return attention_flops(seq_len) / 1e12."""
        return self.attention_flops(seq_len) / 1e12

    def cost_usd(self, seq_len: int) -> float:
        """Return H100 dollar cost for this attention computation.

        Formula: attention_tflops(seq_len) / 312.0 / 3600.0 * 2.50
        """
        return self.attention_tflops(seq_len) / 312.0 / 3600.0 * 2.50

    def kv_cache_size_per_token(self) -> int:
        """Return KV-cache bytes per token for this model.

        Formula: 2 * num_kv_heads * head_dim * 2 * num_layers
        """
        return 2 * self.num_kv_heads * self.head_dim * 2 * self.num_layers


# ---------------------------------------------------------------------------
# Six frozen ModelConfig module-level constants (Req 1.2–1.7, Go reference)
# ---------------------------------------------------------------------------

# Llama-3-8B: 32 layers, GQA with 8 KV heads, 32 Q heads, head_dim=128
# Source: NousResearch/Meta-Llama-3-8B-Instruct config.json
LLAMA3_8B = ModelConfig(
    name="Llama-3-8B",
    num_layers=32,
    num_kv_heads=8,
    num_q_heads=32,
    head_dim=128,
    max_context_len=8192,
    params_billion=8.0,
)

# Llama-3-70B: 80 layers, GQA with 8 KV heads, 64 Q heads, head_dim=128
# Source: NousResearch/Meta-Llama-3-70B-Instruct config.json
LLAMA3_70B = ModelConfig(
    name="Llama-3-70B",
    num_layers=80,
    num_kv_heads=8,
    num_q_heads=64,
    head_dim=128,
    max_context_len=8192,
    params_billion=70.0,
)

# Mistral-7B: 32 layers, GQA with 8 KV heads, 32 Q heads, head_dim=128
# Source: TheBloke/Mistral-7B-codealpaca-lora-GPTQ config.json
MISTRAL_7B = ModelConfig(
    name="Mistral-7B",
    num_layers=32,
    num_kv_heads=8,
    num_q_heads=32,
    head_dim=128,
    max_context_len=32768,
    params_billion=7.0,
)

# Mixtral-8x7B: 32 layers, GQA with 8 KV heads, 32 Q heads, head_dim=128
# MoE model — attention arch same as Mistral-7B, 8 expert FFN layers
# Source: TheBloke/Mixtral-8x7B-Instruct-v0.1-GPTQ config.json
MIXTRAL_8X7B = ModelConfig(
    name="Mixtral-8x7B",
    num_layers=32,
    num_kv_heads=8,
    num_q_heads=32,
    head_dim=128,
    max_context_len=32768,
    params_billion=46.7,
)

# Qwen2.5-72B: 80 layers, GQA with 8 KV heads, 64 Q heads, head_dim=128
# Source: Qwen/Qwen2.5-72B-Instruct config.json
QWEN2_5_72B = ModelConfig(
    name="Qwen2.5-72B",
    num_layers=80,
    num_kv_heads=8,
    num_q_heads=64,
    head_dim=128,
    max_context_len=131072,
    params_billion=72.0,
)

# DeepSeek-R1 (671B MoE): 61 layers, 128 KV heads, 128 Q heads, head_dim=128
# Source: deepseek-ai/DeepSeek-R1 config.json
DEEPSEEK_R1 = ModelConfig(
    name="DeepSeek-R1-671B",
    num_layers=61,
    num_kv_heads=128,
    num_q_heads=128,
    head_dim=128,
    max_context_len=131072,
    params_billion=671.0,
)

# ---------------------------------------------------------------------------
# ALL_MODELS: ordered tuple (Req 1.8): 8B → 70B → Mistral → Mixtral → Qwen → DeepSeek
# ---------------------------------------------------------------------------

ALL_MODELS: tuple[ModelConfig, ...] = (
    LLAMA3_8B,
    LLAMA3_70B,
    MISTRAL_7B,
    MIXTRAL_8X7B,
    QWEN2_5_72B,
    DEEPSEEK_R1,
)


# ---------------------------------------------------------------------------
# Module-level functions — stubs for tasks 2.2 and 2.3
# ---------------------------------------------------------------------------

def validate_seq_len(seq_len: int) -> None:
    """Validate seq_len: reject booleans, non-integers, negatives, and > MAX_SEQ_LEN.

    Raises SeqLenRangeError on any invalid input.
    0 is valid and returns without error (Req 2.6, 2.7).
    """
    if isinstance(seq_len, bool):
        raise SeqLenRangeError(
            f"seq_len must be an int, got bool: {seq_len!r}"
        )
    if not isinstance(seq_len, int):
        raise SeqLenRangeError(
            f"seq_len must be an int, got {type(seq_len).__name__}: {seq_len!r}"
        )
    if seq_len < 0:
        raise SeqLenRangeError(
            f"seq_len must be >= 0, got {seq_len}"
        )
    if seq_len > MAX_SEQ_LEN:
        raise SeqLenRangeError(
            f"seq_len must be <= {MAX_SEQ_LEN}, got {seq_len}"
        )


def get_model(name: str) -> ModelConfig:
    """Look up a ModelConfig by name; raises UnknownModelError on a miss (Req 1.9, 1.10)."""
    for m in ALL_MODELS:
        if m.name == name:
            return m
    raise UnknownModelError(
        f"Unknown model '{name}'. Valid names: {[m.name for m in ALL_MODELS]}"
    )


def attention_tflops_vec(
    models: Sequence[ModelConfig], seq_len: int
) -> np.ndarray:
    """Return a NumPy array of attention TFLOPs, one element per model (Req 2.5, 7.3).

    Calls validate_seq_len first.
    """
    validate_seq_len(seq_len)
    layers = np.array([m.num_layers for m in models], dtype=np.float64)
    q_heads = np.array([m.num_q_heads for m in models], dtype=np.float64)
    head_dim = np.array([m.head_dim for m in models], dtype=np.float64)
    flops = layers * (4.0 * q_heads * head_dim * float(seq_len) ** 2)
    return flops / 1e12


def cost_usd_vec(
    models: Sequence[ModelConfig], seq_len: int
) -> np.ndarray:
    """Return a NumPy array of H100 USD costs, one element per model (Req 2.5, 7.3).

    Vectorized formula: flops = layers * (4.0 * q_heads * head_dim * seq_len**2),
    tflops = flops / 1e12, usd = tflops / 312.0 / 3600.0 * 2.50.
    """
    validate_seq_len(seq_len)
    layers = np.array([m.num_layers for m in models], dtype=np.float64)
    q_heads = np.array([m.num_q_heads for m in models], dtype=np.float64)
    head_dim = np.array([m.head_dim for m in models], dtype=np.float64)
    flops = layers * (4.0 * q_heads * head_dim * float(seq_len) ** 2)
    tflops = flops / 1e12
    return tflops / 312.0 / 3600.0 * 2.50
