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

func TestSelectResolvesModelGroupAlias(t *testing.T) {
	router := NewWithAliases([]config.Model{{Name: "deployment", Model: "openai/gpt-5"}}, map[string]string{"public-name": "deployment"})
	selected, err := router.Select("public-name")
	if err != nil || selected.Name != "deployment" {
		t.Fatalf("selected = %#v, err = %v", selected, err)
	}
}

func TestSelectUsesWeightedDistribution(t *testing.T) {
	router := New([]config.Model{
		{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://one.example", Weight: 99},
		{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://two.example", Weight: 1},
	})
	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		selected, err := router.Select("gateway-model")
		if err != nil {
			t.Fatalf("select: %v", err)
		}
		counts[selected.APIBase]++
	}
	if counts["https://one.example"] < 900 || counts["https://two.example"] == 0 {
		t.Fatalf("counts = %v, want weighted distribution around 99/1", counts)
	}
}

func TestFallbackSwitchesToConfiguredModelGroup(t *testing.T) {
	primary := config.Model{Name: "primary", Model: "openai/gpt-5", APIBase: "https://one.example"}
	router := NewWithFallbacks([]config.Model{primary, {Name: "backup", Model: "anthropic/claude", APIBase: "https://backup.example"}}, nil, []map[string][]string{{"primary": {"backup"}}})

	selected, err := router.Fallback("primary", []config.Model{primary})
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if selected.Name != "backup" {
		t.Fatalf("fallback = %#v, want backup deployment", selected)
	}
}

func TestFallbackUsesCatchAllRule(t *testing.T) {
	primary := config.Model{Name: "primary", Model: "openai/gpt-5"}
	router := NewWithFallbacks([]config.Model{primary, {Name: "backup", Model: "anthropic/claude"}}, nil, []map[string][]string{{"*": {"backup"}}})

	selected, err := router.Fallback("primary", []config.Model{primary})
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if selected.Name != "backup" {
		t.Fatalf("fallback = %#v, want backup deployment", selected)
	}
}

func TestFallbackWithoutConfiguredTargetsReturnsNotFound(t *testing.T) {
	primary := config.Model{Name: "primary", Model: "openai/gpt-5"}
	router := NewWithFallbacks([]config.Model{primary}, nil, []map[string][]string{{"other": {"primary"}}})

	if _, err := router.Fallback("primary", []config.Model{primary}); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("error = %v, want ErrModelNotFound", err)
	}
}
