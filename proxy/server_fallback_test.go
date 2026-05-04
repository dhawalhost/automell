package proxy

import (
	"testing"

	"github.com/dhawalhost/automell/config"
)

func TestCandidateProviderModels_PrimaryThenGlobalFallback(t *testing.T) {
	cfg := &config.Config{
		ModelOpus:   "open_router/opus-model",
		ModelSonnet: "nvidia_nim/sonnet-model",
		ModelHaiku:  "deepseek/haiku-model",
		Model:       "ollama/default-model",
	}

	got := candidateProviderModels(cfg, "claude-sonnet-4-5")
	want := []string{
		"nvidia_nim/sonnet-model", // primary
		"ollama/default-model",    // global fallback MODEL
		"deepseek/haiku-model",
		"open_router/opus-model",
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected candidate count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected candidate at %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestCandidateProviderModels_DeduplicatesPrimaryAndFallbacks(t *testing.T) {
	cfg := &config.Config{
		ModelOpus:   "nvidia_nim/sonnet-model", // duplicate of primary
		ModelSonnet: "nvidia_nim/sonnet-model", // duplicate of primary
		ModelHaiku:  "nvidia_nim/sonnet-model", // duplicate of primary
		Model:       "nvidia_nim/sonnet-model", // duplicate of primary
	}

	got := candidateProviderModels(cfg, "claude-sonnet-4-5")
	want := []string{"nvidia_nim/sonnet-model"}
	if len(got) != len(want) {
		t.Fatalf("unexpected candidate count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected candidate at %d: got %q want %q", i, got[i], want[i])
		}
	}
}
