package providers

import (
	"fmt"
	"strings"

	"github.com/dhawalhost/automell/config"
	"github.com/dhawalhost/automell/types"
)

// Resolve resolves a model string to a Provider
// Model strings use the format: <provider_prefix>/<model_name>
// The model_name may itself contain slashes (e.g. nvidia_nim/meta/llama-3.1-70b-instruct).
func Resolve(model string) (types.Provider, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) < 2 || parts[1] == "" {
		return types.Provider{}, fmt.Errorf("invalid model format, expected <provider>/<model>, got: %s", model)
	}

	prefix := strings.ToLower(parts[0])
	_ = parts[1] // model path used downstream by caller

	cfg := config.Get()

	switch prefix {
	case "nvidia_nim":
		if cfg.NvidiaNimAPIKey == "" {
			return types.Provider{}, fmt.Errorf("NVIDIA_NIM_API_KEY not set")
		}
		return types.Provider{
			Name:    "NVIDIA NIM",
			BaseURL: "https://integrate.api.nvidia.com/v1/chat/completions",
			APIKey:  cfg.NvidiaNimAPIKey,
			Proxy:   cfg.NvidiaNimProxy,
		}, nil

	case "open_router":
		if cfg.OpenRouterAPIKey == "" {
			return types.Provider{}, fmt.Errorf("OPENROUTER_API_KEY not set")
		}
		return types.Provider{
			Name:    "OpenRouter",
			BaseURL: "https://openrouter.ai/api/v1/chat/completions",
			APIKey:  cfg.OpenRouterAPIKey,
			Proxy:   cfg.OpenRouterProxy,
		}, nil

	case "deepseek":
		if cfg.DeepSeekAPIKey == "" {
			return types.Provider{}, fmt.Errorf("DEEPSEEK_API_KEY not set")
		}
		return types.Provider{
			Name:    "DeepSeek",
			BaseURL: "https://api.deepseek.com/v1/chat/completions",
			APIKey:  cfg.DeepSeekAPIKey,
		}, nil

	case "lmstudio":
		baseURL := strings.TrimRight(cfg.LMStudioBaseURL, "/") + "/chat/completions"
		return types.Provider{
			Name:    "LM Studio",
			BaseURL: baseURL,
			APIKey:  "",
			Proxy:   cfg.LMStudioProxy,
		}, nil

	case "llamacpp":
		baseURL := strings.TrimRight(cfg.LlamaCppBaseURL, "/") + "/chat/completions"
		return types.Provider{
			Name:    "llama.cpp",
			BaseURL: baseURL,
			APIKey:  "",
			Proxy:   cfg.LlamaCppProxy,
		}, nil

	case "ollama":
		// Ollama uses the OpenAI-compatible /v1/chat/completions endpoint
		baseURL := strings.TrimRight(cfg.OllamaBaseURL, "/") + "/v1/chat/completions"
		return types.Provider{
			Name:    "Ollama",
			BaseURL: baseURL,
			APIKey:  "",
			Proxy:   cfg.OllamaProxy,
		}, nil

	default:
		return types.Provider{}, fmt.Errorf("unknown provider prefix: %s", prefix)
	}
}

// ConfiguredProviders returns the set of provider prefixes that have credentials/
// base URLs configured and can accept requests right now.
func ConfiguredProviders(cfg *config.Config) []string {
	var out []string
	if cfg.NvidiaNimAPIKey != "" {
		out = append(out, "nvidia_nim")
	}
	if cfg.OpenRouterAPIKey != "" {
		out = append(out, "open_router")
	}
	if cfg.DeepSeekAPIKey != "" {
		out = append(out, "deepseek")
	}
	// Local providers are always "configured" when their base URL is set (non-empty).
	if cfg.LMStudioBaseURL != "" {
		out = append(out, "lmstudio")
	}
	if cfg.LlamaCppBaseURL != "" {
		out = append(out, "llamacpp")
	}
	if cfg.OllamaBaseURL != "" {
		out = append(out, "ollama")
	}
	return out
}
