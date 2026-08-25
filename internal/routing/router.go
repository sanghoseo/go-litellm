package routing

import (
	"errors"
	"sync"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

var ErrModelNotFound = errors.New("model deployment not found")

type Router struct {
	deployments map[string][]config.Model
	mu          sync.Mutex
	next        map[string]uint64
}

func New(models []config.Model) *Router {
	deployments := make(map[string][]config.Model)
	for _, model := range models {
		deployments[model.Name] = append(deployments[model.Name], model)
	}
	return &Router{deployments: deployments, next: make(map[string]uint64)}
}

func (router *Router) Select(modelName string) (config.Model, error) {
	return router.selectExcluding(modelName, nil)
}

func (router *Router) Fallback(modelName string, failedModels []config.Model) (config.Model, error) {
	failed := make(map[string]struct{}, len(failedModels))
	for _, model := range failedModels {
		failed[modelIdentity(model)] = struct{}{}
	}
	return router.selectExcluding(modelName, failed)
}

func (router *Router) selectExcluding(modelName string, excluded map[string]struct{}) (config.Model, error) {
	router.mu.Lock()
	defer router.mu.Unlock()

	models := router.deployments[modelName]
	if len(models) == 0 {
		return config.Model{}, ErrModelNotFound
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

func modelIdentity(model config.Model) string {
	return model.Name + "\x00" + model.Model + "\x00" + model.APIBase
}
