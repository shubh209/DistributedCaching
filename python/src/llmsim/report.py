"""Report formatter: scalar formatters and string renderers for all simulation modes."""

from __future__ import annotations

from llmsim.scenarios import ScenarioResult
from llmsim.rag import RAGResult

__all__ = [
    "format_money",
    "format_int",
    "format_percent",
    "format_tflops",
    "render_scenario_result",
    "render_model_comparison",
    "render_rag_result",
]


# ---------------------------------------------------------------------------
# Scalar formatters (Req 13.1–13.6)
# ---------------------------------------------------------------------------

def format_money(value: float) -> str:
    """Return a comma-thousands, 2-decimal dollar string (Req 13.1).

    Examples: 1234567.89 → "1,234,567.89", 0.01 → "0.01"
    """
    return f"{value:,.2f}"


def format_int(value: int) -> str:
    """Return a comma-thousands integer string (Req 13.2).

    Examples: 1000000 → "1,000,000", 0 → "0"
    """
    return f"{value:,d}"


def format_percent(value: float) -> str:
    """Return a 1-decimal percentage string (Req 13.3).

    Examples: 92.3 → "92.3", 0.0 → "0.0"
    """
    return f"{value:.1f}"


def format_tflops(value: float) -> str:
    """Return a human-readable TFLOPs/PFLOPs string, mirroring Go's FormatTFLOPs.

    Tiers (Req 13.4–13.6):
      >= 1000 → "X.X PFLOPs"  (1 decimal)
      >= 1    → "X.XX TFLOPs" (2 decimals)
      < 1     → "X.XXXX TFLOPs" (4 decimals)
    """
    if value >= 1000:
        return f"{value / 1000:.1f} PFLOPs"
    if value >= 1:
        return f"{value:.2f} TFLOPs"
    return f"{value:.4f} TFLOPs"


# ---------------------------------------------------------------------------
# Renderers — all return strings (Req 13.7, 13.8)
# ---------------------------------------------------------------------------

_SEP = "─" * 70


def render_scenario_result(result: ScenarioResult) -> str:
    """Render a scenario simulation result as a human-readable string.

    Mirrors the Go PrintResults output style.
    """
    s = result.scenario
    stats = result.stats
    lines = [
        "",
        _SEP,
        f"  Scenario:   {s.name}",
        f"  Company:    {s.company}",
        f"  Model:      {s.model.name} ({s.model.params_billion:.0f}B params)",
        f"  Prefix:     {s.system_prompt_len} tokens | "
        f"Requests: {s.num_requests} | "
        f"Reuse rate: {s.prefix_reuse_rate * 100:.0f}%",
        _SEP,
        f"  Cache hit rate:      {format_percent(stats.hit_rate)}% "
        f"({stats.cache_hits}/{stats.total_requests} requests)",
        f"  Prefix cost/req:     ${s.model.cost_usd(s.system_prompt_len):.6f} "
        f"({format_tflops(s.model.attention_tflops(s.system_prompt_len))})",
        f"  Cost WITHOUT cache:  ${result.without_cache_cost:.4f}",
        f"  Cost WITH cache:     ${result.with_cache_cost:.4f}",
        f"  ✦ Compute saved:     {format_tflops(stats.total_saved_tflops)}",
        f"  ✦ Cost saved:        ${result.savings_usd:.4f} "
        f"({format_percent(result.savings_pct)}% reduction)",
    ]
    return "\n".join(lines)


def render_model_comparison(results: list[ScenarioResult]) -> str:
    """Render a multi-model comparison table (Req 13.8).

    Columns: Model | Hit Rate | Cost/Req | Saved Total | Reduction
    One row per ScenarioResult in the order provided.
    """
    header = f"\n{'Model':<20} {'Hit Rate':>8} {'Cost/Req':>12} {'Saved Total':>12} {'Reduction':>10}"
    sep = _SEP
    rows = [header, sep]
    for r in results:
        rows.append(
            f"{r.scenario.model.name:<20} "
            f"{r.stats.hit_rate:>7.1f}% "
            f"${r.scenario.model.cost_usd(r.scenario.system_prompt_len):>10.6f} "
            f"{format_tflops(r.stats.total_saved_tflops):>11} "
            f"{r.savings_pct:>9.1f}%"
        )
    return "\n".join(rows)


def render_rag_result(result: RAGResult) -> str:
    """Render a RAG simulation result as a human-readable string (Req 13.7).

    Includes: knowledge base size, retrieval top-K, prefix size, model,
    requests, cache hit rate, compute saved, cost per uncached prefix,
    per-run totals (without/with/savings), and monthly projection.
    """
    cfg = result.config
    stats = result.stats
    lines = [
        "",
        _SEP,
        "  RAG PIPELINE COST SIMULATION",
        _SEP,
        f"  Knowledge base:   {cfg.num_documents} documents × {cfg.tokens_per_doc} tokens",
        f"  Retrieval:        top-{cfg.top_k} documents per query (Zipf popularity)",
        f"  Prefix size:      {result.prefix_tokens} tokens "
        f"({cfg.top_k} docs + {cfg.query_tokens} query)",
        f"  Model:            {cfg.model.name} ({cfg.model.params_billion:.0f}B params)",
        f"  Requests:         {cfg.num_requests} simulated",
        _SEP,
        f"  Cache hit rate:   {format_percent(stats.hit_rate)}% "
        f"({stats.cache_hits} hits / {stats.total_requests} requests)",
        f"  Compute saved:    {format_tflops(stats.total_saved_tflops)}",
        f"  Cost per uncached prefix: ${result.cost_per_miss_usd:.6f}",
        _SEP,
        "  PER-RUN TOTALS",
        f"    Without caching:  ${result.without_cache_usd:.4f}",
        f"    With caching:     ${result.with_cache_usd:.4f}",
        f"    ✦ Saved:          ${result.savings_usd:.4f} "
        f"({format_percent(result.savings_pct)}% reduction)",
        _SEP,
        f"  MONTHLY PROJECTION (at {format_int(cfg.requests_per_day)} requests/day)",
        f"    Without caching:  ${format_money(result.monthly_without)} / month",
        f"    With caching:     ${format_money(result.monthly_with)} / month",
        f"    ✦ Saved:          ${format_money(result.monthly_savings)} / month",
        _SEP,
    ]
    return "\n".join(lines)
