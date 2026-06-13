"""Unit tests for report.py scalar formatters and renderers (Tasks 8.1–8.3)."""

from __future__ import annotations

import pytest

from llmsim.report import (
    format_money,
    format_int,
    format_percent,
    format_tflops,
    render_scenario_result,
    render_model_comparison,
    render_rag_result,
)
from llmsim.scenarios import default_scenarios, run_scenario, run_all_models_comparison
from llmsim.rag import default_rag_config, run_rag_simulation


# ---------------------------------------------------------------------------
# format_money (Req 13.1)
# ---------------------------------------------------------------------------

class TestFormatMoney:
    def test_large_amount(self):
        assert format_money(1234567.89) == "1,234,567.89"

    def test_small_amount(self):
        assert format_money(0.01) == "0.01"

    def test_zero(self):
        assert format_money(0.0) == "0.00"

    def test_thousands(self):
        assert format_money(1234.56) == "1,234.56"


# ---------------------------------------------------------------------------
# format_int (Req 13.2)
# ---------------------------------------------------------------------------

class TestFormatInt:
    def test_million(self):
        assert format_int(1_000_000) == "1,000,000"

    def test_zero(self):
        assert format_int(0) == "0"

    def test_thousands(self):
        assert format_int(1000) == "1,000"

    def test_no_separator_below_1000(self):
        assert format_int(999) == "999"


# ---------------------------------------------------------------------------
# format_percent (Req 13.3)
# ---------------------------------------------------------------------------

class TestFormatPercent:
    def test_normal(self):
        assert format_percent(92.3) == "92.3"

    def test_zero(self):
        assert format_percent(0.0) == "0.0"

    def test_hundred(self):
        assert format_percent(100.0) == "100.0"

    def test_rounding(self):
        # 92.35 rounds to one decimal
        assert format_percent(92.35) in ("92.3", "92.4")  # floating-point rounding


# ---------------------------------------------------------------------------
# format_tflops (Req 13.4–13.6)
# ---------------------------------------------------------------------------

class TestFormatTflops:
    def test_sub_one_uses_4_decimals(self):
        result = format_tflops(0.5)
        assert result.endswith("TFLOPs")
        assert result == "0.5000 TFLOPs"

    def test_exactly_one_uses_2_decimals(self):
        result = format_tflops(1.0)
        assert result.endswith("TFLOPs")
        assert result == "1.00 TFLOPs"

    def test_below_1000_uses_2_decimals(self):
        result = format_tflops(999.99)
        assert result.endswith("TFLOPs")
        assert result == "999.99 TFLOPs"

    def test_exactly_1000_uses_pflops(self):
        result = format_tflops(1000.0)
        assert result.endswith("PFLOPs")
        assert result == "1.0 PFLOPs"

    def test_2000_pflops(self):
        assert format_tflops(2000.0) == "2.0 PFLOPs"

    def test_sub_one_nonzero(self):
        result = format_tflops(0.1234)
        assert result == "0.1234 TFLOPs"

    def test_zero_uses_4_decimals(self):
        # 0.0 < 1, so uses 4-decimal TFLOPs format
        assert format_tflops(0.0) == "0.0000 TFLOPs"


# ---------------------------------------------------------------------------
# render_rag_result (Req 13.7)
# ---------------------------------------------------------------------------

class TestRenderRagResult:
    def test_contains_required_fields(self):
        """render_rag_result output must contain hit rate, Without, With, Monthly."""
        result = run_rag_simulation(default_rag_config())
        output = render_rag_result(result)

        assert "hit rate" in output.lower()
        assert "Without" in output
        assert "With" in output
        assert "MONTHLY" in output or "Monthly" in output

    def test_contains_knowledge_base_info(self):
        result = run_rag_simulation(default_rag_config())
        output = render_rag_result(result)
        # Knowledge base: 200 documents × 512 tokens
        assert "200" in output
        assert "512" in output

    def test_contains_model_name(self):
        result = run_rag_simulation(default_rag_config())
        output = render_rag_result(result)
        assert "Llama-3-70B" in output

    def test_contains_per_run_totals(self):
        result = run_rag_simulation(default_rag_config())
        output = render_rag_result(result)
        assert "PER-RUN TOTALS" in output

    def test_returns_string(self):
        result = run_rag_simulation(default_rag_config())
        output = render_rag_result(result)
        assert isinstance(output, str)
        assert len(output) > 100


# ---------------------------------------------------------------------------
# render_model_comparison (Req 13.8)
# ---------------------------------------------------------------------------

class TestRenderModelComparison:
    def test_six_model_rows(self):
        """Output must contain exactly 6 model name rows."""
        results = run_all_models_comparison(
            prefix_len=1024, num_requests=1000, reuse_rate=0.90
        )
        output = render_model_comparison(results)

        # Each of the 6 model names should appear in the output
        expected_models = [
            "Llama-3-8B",
            "Llama-3-70B",
            "Mistral-7B",
            "Mixtral-8x7B",
            "Qwen2.5-72B",
            "DeepSeek-R1-671B",
        ]
        for model_name in expected_models:
            assert model_name in output, f"Model {model_name!r} not found in comparison output"

    def test_each_row_contains_hit_rate(self):
        """Each model row must contain a hit rate percentage."""
        results = run_all_models_comparison(
            prefix_len=1024, num_requests=1000, reuse_rate=0.90
        )
        output = render_model_comparison(results)
        # The hit rate column header should appear
        assert "Hit Rate" in output

    def test_each_row_contains_reduction(self):
        """Reduction column must appear in the comparison output."""
        results = run_all_models_comparison(
            prefix_len=1024, num_requests=1000, reuse_rate=0.90
        )
        output = render_model_comparison(results)
        assert "Reduction" in output

    def test_returns_string(self):
        results = run_all_models_comparison(
            prefix_len=1024, num_requests=1000, reuse_rate=0.90
        )
        output = render_model_comparison(results)
        assert isinstance(output, str)

    def test_correct_number_of_result_rows(self):
        """Verify exactly 6 result rows (one per model) are in the output."""
        results = run_all_models_comparison(
            prefix_len=1024, num_requests=1000, reuse_rate=0.90
        )
        assert len(results) == 6
        output = render_model_comparison(results)
        # The separator and header lines are not model rows; count model entries
        model_lines = [
            line for line in output.splitlines()
            if any(name in line for name in [
                "Llama-3-8B", "Llama-3-70B", "Mistral-7B",
                "Mixtral-8x7B", "Qwen2.5-72B", "DeepSeek-R1-671B",
            ])
        ]
        assert len(model_lines) == 6


# ---------------------------------------------------------------------------
# render_scenario_result (smoke test)
# ---------------------------------------------------------------------------

class TestRenderScenarioResult:
    def test_returns_non_empty_string(self):
        scenarios = default_scenarios()
        result = run_scenario(scenarios[0])
        output = render_scenario_result(result)
        assert isinstance(output, str)
        assert len(output) > 50

    def test_contains_scenario_name(self):
        scenarios = default_scenarios()
        result = run_scenario(scenarios[0])
        output = render_scenario_result(result)
        assert scenarios[0].name in output
