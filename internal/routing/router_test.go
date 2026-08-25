package routing

import (
	"errors"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

func TestSelectRoundRobinsDeploymentsForSameModel(t *testing.T) {
	router := New([]config.Model{
		{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://one.example"},
		{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://two.example"},
	})

	first, err := router.Select("gateway-model")
	if err != nil {
		t.Fatalf("first select: %v", err)
	}
	second, err := router.Select("gateway-model")
	if err != nil {
		t.Fatalf("second select: %v", err)
	}
	if first.APIBase != "https://one.example" || second.APIBase != "https://two.example" {
		t.Fatalf("selected %q then %q, want round robin", first.APIBase, second.APIBase)
	}
}

func TestFallbackSkipsFailedDeployment(t *testing.T) {
	first := config.Model{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://one.example"}
	second := config.Model{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://two.example"}
	router := New([]config.Model{first, second})

	selected, err := router.Fallback("gateway-model", []config.Model{first})
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if selected.APIBase != second.APIBase {
		t.Fatalf("fallback = %q, want %q", selected.APIBase, second.APIBase)
	}
}

func TestSelectUnknownModel(t *testing.T) {
	_, err := New(nil).Select("unknown")
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("error = %v, want ErrModelNotFound", err)
	}
}
