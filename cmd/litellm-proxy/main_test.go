package main

import "testing"

func TestDefaultProviderRegistryIncludesOpenAICompatibleProviders(t *testing.T) {
	registry := newProviderRegistry()
	for _, provider := range []string{"openai", "ollama", "vllm", "xai", "fireworks_ai", "sambanova", "nvidia_nim", "anyscale", "databricks"} {
		if !registry.HasProvider(provider) {
			t.Fatalf("provider %q is not configured", provider)
		}
	}
}
