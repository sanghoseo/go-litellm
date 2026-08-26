package routing

import (
	"errors"
	"math/rand"
	"sync"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

var ErrModelNotFound = errors.New("model deployment not found")

type Router struct {
	deployments map[string][]config.Model
	fallbacks   []map[string][]string
	mu          sync.Mutex
	next        map[string]uint64
	aliases     map[string]string
	rng         *rand.Rand
}

func New(models []config.Model) *Router {
	return NewWithAliases(models, nil)
}

func NewWithAliases(models []config.Model, aliases map[string]string) *Router {
	return NewWithFallbacks(models, aliases, nil)
}

func NewWithFallbacks(models []config.Model, aliases map[string]string, fallbacks []map[string][]string) *Router {
	deployments := make(map[string][]config.Model)
	for _, model := range models {
		deployments[model.Name] = append(deployments[model.Name], model)
	}
	return &Router{deployments: deployments, fallbacks: fallbacks, next: make(map[string]uint64), aliases: aliases, rng: rand.New(rand.NewSource(rand.Int63()))}
}

func (router *Router) Select(modelName string) (config.Model, error) {
	return router.selectExcluding(modelName, nil)
}

func (router *Router) Fallback(modelName string, failedModels []config.Model) (config.Model, error) {
	failed := make(map[string]struct{}, len(failedModels))
	for _, model := range failedModels {
		failed[modelIdentity(model)] = struct{}{}
	}
	if selected, err := router.selectExcluding(modelName, failed); err == nil {
		return selected, nil
	}
	for _, target := range router.fallbackTargetsFor(router.resolve(modelName)) {
		if selected, err := router.selectExcluding(target, failed); err == nil {
			return selected, nil
		}
	}
	return config.Model{}, ErrModelNotFound
}

func (router *Router) selectExcluding(modelName string, excluded map[string]struct{}) (config.Model, error) {
	router.mu.Lock()
	defer router.mu.Unlock()

	modelName = router.resolve(modelName)
	models := router.deployments[modelName]
	if len(models) == 0 {
		return config.Model{}, ErrModelNotFound
	}
	if router.weighted(models) {
		return router.pickWeighted(models, excluded)
	}
	start := router.next[modelName] % uint64(len(models))
	router.next[modelName]++
	for offset := range models {
		candidate := models[(start+uint64(offset))%uint64(len(models))]
		if _, isExcluded := excluded[modelIdentity(candidate)]; !isExcluded {
			return candidate, nil
		}
	}
	return config.Model{}, ErrModelNotFound
}

func (router *Router) pickWeighted(models []config.Model, excluded map[string]struct{}) (config.Model, error) {
	weights := make([]float64, len(models))
	total := 0.0
	for index, model := range models {
		if _, isExcluded := excluded[modelIdentity(model)]; isExcluded {
			continue
		}
		weight := model.Weight
		if weight <= 0 {
			weight = 1
		}
		weights[index] = weight
		total += weight
	}
	if total <= 0 {
		return config.Model{}, ErrModelNotFound
	}
	threshold := router.rng.Float64() * total
	for index, weight := range weights {
		threshold -= weight
		if threshold < 0 {
			return models[index], nil
		}
	}
	for index := len(models) - 1; index >= 0; index-- {
		if weights[index] > 0 {
			return models[index], nil
		}
	}
	return config.Model{}, ErrModelNotFound
}

func (router *Router) weighted(models []config.Model) bool {
	for _, model := range models {
		if model.Weight > 0 {
			return true
		}
	}
	return false
}

func (router *Router) fallbackTargetsFor(modelGroup string) []string {
	for _, rule := range router.fallbacks {
		if targets, found := rule[modelGroup]; found {
			return targets
		}
	}
	if targets, found := router.genericFallbackTargets(); found {
		return targets
	}
	return nil
}

func (router *Router) genericFallbackTargets() ([]string, bool) {
	for _, rule := range router.fallbacks {
		if targets, found := rule["*"]; found {
			return targets, true
		}
	}
	return nil, false
}

func (router *Router) resolve(modelName string) string {
	if alias, found := router.aliases[modelName]; found {
		return alias
	}
	return modelName
}

func modelIdentity(model config.Model) string {
	return model.Name + "\x00" + model.Model + "\x00" + model.APIBase
}
