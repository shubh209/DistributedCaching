"""llmsim — LLM prefix-KV-cache and RAG-pipeline cost simulator."""

from llmsim.models import (
    ModelConfig,
    ALL_MODELS,
    get_model,
)
from llmsim.prefix_cache import (
    PrefixCache,
    CacheEntry,
    CacheStats,
    hash_prefix,
)
from llmsim.scenarios import (
    run_scenario,
    run_all_models_comparison,
)
from llmsim.rag import (
    run_rag_simulation,
    default_rag_config,
    RAGConfig,
)

__all__ = [
    "ModelConfig",
    "ALL_MODELS",
    "get_model",
    "PrefixCache",
    "CacheEntry",
    "CacheStats",
    "hash_prefix",
    "run_scenario",
    "run_all_models_comparison",
    "run_rag_simulation",
    "default_rag_config",
    "RAGConfig",
]
