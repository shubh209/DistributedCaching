package llm

// ModelConfig holds the transformer architecture parameters needed to compute
// attention FLOP cost. All values sourced directly from Hugging Face config.json.
type ModelConfig struct {
	Name           string
	NumLayers      int     // num_hidden_layers
	NumKVHeads     int     // num_key_value_heads (GQA)
	NumQHeads      int     // num_attention_heads
	HeadDim        int     // hidden_size / num_attention_heads
	MaxContextLen  int     // max position embeddings
	ParamsBillion  float64 // approximate parameter count for context
}

// KVCacheSizePerToken returns the KV cache memory in bytes per token for this model.
// KV cache = 2 (K+V) × num_kv_heads × head_dim × 2 bytes (fp16) × num_layers
func (m *ModelConfig) KVCacheSizePerToken() int64 {
	return int64(2 * m.NumKVHeads * m.HeadDim * 2 * m.NumLayers)
}

// AttentionFLOPs computes the number of floating point operations for the
// attention mechanism over a sequence of seqLen tokens.
//
// Real transformer math (per layer, per token being generated):
//   Q·K^T matmul:  2 × num_q_heads × head_dim × seq_len
//   softmax(A)·V:  2 × num_kv_heads × head_dim × seq_len
//   Summed over all layers.
//
// For prefill (processing the entire prefix at once):
//   Total = num_layers × (4 × num_heads × head_dim × seq_len²)
//   (quadratic in seq_len — this is why long prompts are expensive)
func (m *ModelConfig) AttentionFLOPs(seqLen int) float64 {
	flopsPerLayer := 4.0 * float64(m.NumQHeads) * float64(m.HeadDim) * float64(seqLen) * float64(seqLen)
	return flopsPerLayer * float64(m.NumLayers)
}

// AttentionTFLOPs returns AttentionFLOPs in teraflops (1e12).
func (m *ModelConfig) AttentionTFLOPs(seqLen int) float64 {
	return m.AttentionFLOPs(seqLen) / 1e12
}

// CostUSD estimates the dollar cost of running the attention computation on an H100.
// H100 SXM5: ~312 TFLOPS (fp16 non-sparse), ~$2.50/hr on-demand.
// Cost = TFLOPs / (312 TFLOPS × 3600 sec) × $2.50
func (m *ModelConfig) CostUSD(seqLen int) float64 {
	h100TFLOPsPerSec := 312.0
	h100HourlyCost := 2.50
	tflops := m.AttentionTFLOPs(seqLen)
	secondsCost := tflops / h100TFLOPsPerSec
	hourlyCost := secondsCost / 3600.0
	return hourlyCost * h100HourlyCost
}

// --- Supported open-source models (specs from Hugging Face config.json) ---

var (
	// Llama-3-8B: 32 layers, GQA with 8 KV heads, 32 Q heads, head_dim=128
	// Source: NousResearch/Meta-Llama-3-8B-Instruct config.json
	Llama3_8B = ModelConfig{
		Name:          "Llama-3-8B",
		NumLayers:     32,
		NumKVHeads:    8,
		NumQHeads:     32,
		HeadDim:       128,
		MaxContextLen: 8192,
		ParamsBillion: 8.0,
	}

	// Llama-3-70B: 80 layers, GQA with 8 KV heads, 64 Q heads, head_dim=128
	// Source: NousResearch/Meta-Llama-3-70B-Instruct config.json
	Llama3_70B = ModelConfig{
		Name:          "Llama-3-70B",
		NumLayers:     80,
		NumKVHeads:    8,
		NumQHeads:     64,
		HeadDim:       128,
		MaxContextLen: 8192,
		ParamsBillion: 70.0,
	}

	// Mistral-7B: 32 layers, GQA with 8 KV heads, 32 Q heads, head_dim=128
	// Source: TheBloke/Mistral-7B-codealpaca-lora-GPTQ config.json
	Mistral7B = ModelConfig{
		Name:          "Mistral-7B",
		NumLayers:     32,
		NumKVHeads:    8,
		NumQHeads:     32,
		HeadDim:       128,
		MaxContextLen: 32768,
		ParamsBillion: 7.0,
	}

	// Mixtral-8x7B: 32 layers, GQA with 8 KV heads, 32 Q heads, head_dim=128
	// MoE model — attention architecture same as Mistral-7B, 8 expert FFN layers
	// Source: TheBloke/Mixtral-8x7B-Instruct-v0.1-GPTQ config.json
	Mixtral8x7B = ModelConfig{
		Name:          "Mixtral-8x7B",
		NumLayers:     32,
		NumKVHeads:    8,
		NumQHeads:     32,
		HeadDim:       128,
		MaxContextLen: 32768,
		ParamsBillion: 46.7, // active params ~12B, total ~46.7B
	}

	// Qwen2.5-72B: 80 layers, GQA with 8 KV heads, 64 Q heads, head_dim=128
	// Source: Qwen/Qwen2.5-72B-Instruct config.json
	Qwen2_5_72B = ModelConfig{
		Name:          "Qwen2.5-72B",
		NumLayers:     80,
		NumKVHeads:    8,
		NumQHeads:     64,
		HeadDim:       128,
		MaxContextLen: 131072,
		ParamsBillion: 72.0,
	}

	// DeepSeek-R1 (671B MoE): 61 layers, 128 KV heads, 128 Q heads, head_dim=128
	// Source: deepseek-ai/DeepSeek-R1 config.json
	DeepSeekR1 = ModelConfig{
		Name:          "DeepSeek-R1-671B",
		NumLayers:     61,
		NumKVHeads:    128,
		NumQHeads:     128,
		HeadDim:       128,
		MaxContextLen: 131072,
		ParamsBillion: 671.0,
	}

	// AllModels is the full list used in simulations and the dashboard.
	AllModels = []ModelConfig{
		Llama3_8B, Llama3_70B, Mistral7B, Mixtral8x7B, Qwen2_5_72B, DeepSeekR1,
	}
)
