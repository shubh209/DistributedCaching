# llmsim — LLM Prefix-KV-Cache & RAG Pipeline Cost Simulator

A Python port of the Go LLM simulation module. It analytically computes transformer attention
FLOP costs and simulates an in-memory LRU prefix cache to quantify compute and dollar savings
that prefix caching provides for LLM inference and RAG pipelines.

No real models are run — every number comes from closed-form FLOP arithmetic plus a simulated cache.

## Installation

```bash
pip install -e .
```

## Usage

```bash
# Default mode: run five pre-built business scenarios
python -m llmsim --mode=prefix-cache

# Run one shared workload across all six supported models
python -m llmsim --mode=all-models

# Run the RAG pipeline simulation with Zipf document popularity
python -m llmsim --mode=rag
```

## Modes

| Mode | Description |
|------|-------------|
| `prefix-cache` | Five pre-built business scenarios (Customer Service Bot, Code Assistant, etc.) |
| `all-models`   | One workload (prefix_len=1024, 1000 requests, 90% reuse) across all six models |
| `rag`          | RAG pipeline: 200 documents, Zipf sampling, monthly cost projection |

## Supported Models

- Llama-3-8B
- Llama-3-70B
- Mistral-7B
- Mixtral-8x7B
- Qwen2.5-72B
- DeepSeek-R1

## Development

```bash
pip install -e ".[dev]"
pytest
```
