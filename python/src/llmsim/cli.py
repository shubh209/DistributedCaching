"""CLI argument parsing and mode dispatch for llmsim."""

from __future__ import annotations

import argparse
import sys

from llmsim.scenarios import default_scenarios, run_scenario, run_all_models_comparison
from llmsim.rag import run_rag_simulation, default_rag_config
from llmsim.report import render_scenario_result, render_model_comparison, render_rag_result

__all__ = ["build_parser", "main", "VALID_MODES", "DEFAULT_MODE"]

VALID_MODES = {"prefix-cache", "all-models", "rag"}
DEFAULT_MODE = "prefix-cache"


def build_parser() -> argparse.ArgumentParser:
    """Build and return the argument parser for llmsim CLI."""
    parser = argparse.ArgumentParser(
        prog="llmsim",
        description=(
            "LLM prefix-KV-cache cost simulation. "
            "Analytically computes compute and dollar savings from prefix caching."
        ),
    )
    parser.add_argument(
        "--mode",
        default=DEFAULT_MODE,
        metavar="MODE",
        help=(
            f"Simulation mode. One of: {sorted(VALID_MODES)}. "
            f"Default: {DEFAULT_MODE!r}."
        ),
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    """Parse argv and dispatch to the appropriate simulation mode.

    Parameters
    ----------
    argv:
        Argument list (default: None, which uses sys.argv[1:] via argparse).

    Returns
    -------
    int
        0 on success, non-zero on error (Req 12.3–12.5).
    """
    parser = build_parser()
    args = parser.parse_args(argv)
    mode = args.mode

    if mode not in VALID_MODES:
        print(
            f"Error: unrecognized mode {mode!r}. "
            f"Valid modes: {sorted(VALID_MODES)}",
            file=sys.stderr,
        )
        return 1

    if mode == "prefix-cache":
        for s in default_scenarios():
            result = run_scenario(s)
            print(render_scenario_result(result))
        return 0

    if mode == "all-models":
        results = run_all_models_comparison(
            prefix_len=1024, num_requests=1000, reuse_rate=0.90
        )
        print(render_model_comparison(results))
        return 0

    if mode == "rag":
        result = run_rag_simulation(default_rag_config())
        print(render_rag_result(result))
        return 0

    # Unreachable — VALID_MODES guard above catches all unrecognized modes
    return 1  # pragma: no cover
