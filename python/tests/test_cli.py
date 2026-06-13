"""Unit tests for cli.py mode dispatch and exit codes (Task 9.2)."""

from __future__ import annotations

import pytest

from llmsim.cli import main
from llmsim.models import ALL_MODELS


# ---------------------------------------------------------------------------
# Default mode (prefix-cache)
# ---------------------------------------------------------------------------

class TestDefaultMode:
    def test_no_args_returns_zero(self, capsys):
        """main([]) uses default mode 'prefix-cache' and returns 0."""
        code = main([])
        assert code == 0

    def test_no_args_produces_output(self, capsys):
        """Default mode produces non-empty stdout."""
        main([])
        captured = capsys.readouterr()
        assert len(captured.out) > 0

    def test_prefix_cache_mode_explicit(self, capsys):
        """--mode=prefix-cache returns 0."""
        code = main(["--mode=prefix-cache"])
        assert code == 0


# ---------------------------------------------------------------------------
# all-models mode
# ---------------------------------------------------------------------------

class TestAllModelsMode:
    def test_returns_zero(self, capsys):
        code = main(["--mode=all-models"])
        assert code == 0

    def test_output_contains_all_six_models(self, capsys):
        """Output must contain all 6 model names."""
        main(["--mode=all-models"])
        captured = capsys.readouterr()
        for model in ALL_MODELS:
            assert model.name in captured.out, (
                f"Model {model.name!r} not found in all-models output"
            )


# ---------------------------------------------------------------------------
# rag mode
# ---------------------------------------------------------------------------

class TestRagMode:
    def test_returns_zero(self, capsys):
        code = main(["--mode=rag"])
        assert code == 0

    def test_output_contains_monthly_projection(self, capsys):
        """RAG output must contain monthly projection text (Req 13.7)."""
        main(["--mode=rag"])
        captured = capsys.readouterr()
        # Accept any capitalisation variant
        assert "MONTHLY" in captured.out or "Monthly" in captured.out or "monthly" in captured.out


# ---------------------------------------------------------------------------
# Invalid mode (Req 12.4)
# ---------------------------------------------------------------------------

class TestInvalidMode:
    def test_invalid_mode_returns_nonzero(self, capsys):
        """Unrecognized --mode must return a non-zero exit code."""
        code = main(["--mode=invalid"])
        assert code != 0

    def test_invalid_mode_error_to_stderr(self, capsys):
        """Error message must go to stderr and mention 'invalid'."""
        main(["--mode=invalid"])
        captured = capsys.readouterr()
        assert "invalid" in captured.err.lower()

    def test_invalid_mode_lists_valid_modes(self, capsys):
        """Error message must list the valid modes."""
        main(["--mode=bogus"])
        captured = capsys.readouterr()
        # All three valid modes must appear in the error output
        assert "prefix-cache" in captured.err
        assert "all-models" in captured.err
        assert "rag" in captured.err

    def test_invalid_mode_no_stdout(self, capsys):
        """No simulation output should be produced for an invalid mode."""
        main(["--mode=totally-wrong"])
        captured = capsys.readouterr()
        # stdout should be empty — the error is on stderr
        assert captured.out == ""
