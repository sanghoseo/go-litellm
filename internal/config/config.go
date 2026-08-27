package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const environmentReferencePrefix = "os.environ/"

type Config struct {
	MasterKey          string
	Models             []Model
	DatabaseURL        string
	RedisURL           string
	RequestTimeout     time.Duration
	NumRetries         int
	ModelAliases       map[string]string
	ResourceModel      string
	ForwardTraceparent bool
	Fallbacks          []map[string][]string
	MaxFallbacks       int
	ResponseCache      bool
	Unsupported        []string
}

type Model struct {
	Name          string
	Model         string
	APIKey        string
	APIBase       string
	Timeout       time.Duration
	StreamTimeout time.Duration
	NumRetries    int
	AWSRegion     string
	Weight        float64
}

type document struct {
	ModelList       []modelEntry      `yaml:"model_list"`
	GeneralSettings generalSettings   `yaml:"general_settings"`
	LiteLLMSettings litellmSettings   `yaml:"litellm_settings"`
	RouterSettings  routerSettings    `yaml:"router_settings"`
	Environment     map[string]string `yaml:"environment_variables"`
}

type modelEntry struct {
	ModelName     string      `yaml:"model_name"`
	LiteLLMParams modelParams `yaml:"litellm_params"`
}

type modelParams struct {
	Model         string  `yaml:"model"`
	APIKey        string  `yaml:"api_key"`
	APIBase       string  `yaml:"api_base"`
	Timeout       float64 `yaml:"timeout"`
	StreamTimeout float64 `yaml:"stream_timeout"`
	NumRetries    int     `yaml:"num_retries"`
	AWSRegion     string  `yaml:"aws_region_name"`
	Weight        float64 `yaml:"weight"`
}

type generalSettings struct {
	MasterKey          string `yaml:"master_key"`
	ResourceModel      string `yaml:"resource_model"`
	ForwardTraceparent bool   `yaml:"forward_traceparent_to_llm_provider"`
}

type litellmSettings struct {
	RequestTimeout float64 `yaml:"request_timeout"`
	NumRetries     int     `yaml:"num_retries"`
	Cache          bool    `yaml:"cache"`
}

type routerSettings struct {
	ModelGroupAlias map[string]string     `yaml:"model_group_alias"`
	Fallbacks       []map[string][]string `yaml:"fallbacks"`
	MaxFallbacks    int                   `yaml:"max_fallbacks"`
}

func Load(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	parsed := document{}
	if err := yaml.Unmarshal(contents, &parsed); err != nil {
		return Config{}, fmt.Errorf("parse YAML: %w", err)
	}
	unsupported, err := unsupportedKeys(contents)
	if err != nil {
		return Config{}, err
	}

	for name, value := range parsed.Environment {
		if _, exists := os.LookupEnv(name); !exists {
			if err := os.Setenv(name, value); err != nil {
				return Config{}, fmt.Errorf("set environment variable %q: %w", name, err)
			}
		}
	}

	models := make([]Model, 0, len(parsed.ModelList))
	for index, entry := range parsed.ModelList {
		model, err := parseModel(entry)
		if err != nil {
			return Config{}, fmt.Errorf("model_list[%d]: %w", index, err)
		}
		if model.NumRetries == 0 {
			model.NumRetries = parsed.LiteLLMSettings.NumRetries
		}
		if model.Timeout == 0 {
			model.Timeout = secondsDuration(parsed.LiteLLMSettings.RequestTimeout)
		}
		models = append(models, model)
	}

	masterKey, err := resolveEnvironmentReference(parsed.GeneralSettings.MasterKey)
	if err != nil {
		return Config{}, fmt.Errorf("resolve general_settings.master_key: %w", err)
	}
	if value, exists := os.LookupEnv("LITELLM_MASTER_KEY"); exists {
		masterKey = value
	}

	return Config{
		MasterKey:          masterKey,
		Models:             models,
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		RedisURL:           redisURLFromEnvironment(),
		RequestTimeout:     secondsDuration(parsed.LiteLLMSettings.RequestTimeout),
		NumRetries:         parsed.LiteLLMSettings.NumRetries,
		ModelAliases:       parsed.RouterSettings.ModelGroupAlias,
		ResourceModel:      parsed.GeneralSettings.ResourceModel,
		ForwardTraceparent: parsed.GeneralSettings.ForwardTraceparent,
		Fallbacks:          parsed.RouterSettings.Fallbacks,
		MaxFallbacks:       maxFallbacks(parsed.RouterSettings.MaxFallbacks),
		ResponseCache:      parsed.LiteLLMSettings.Cache,
		Unsupported:        unsupported,
	}, nil
}

func maxFallbacks(value int) int {
	if value <= 0 {
		return 5
	}
	return value
}

var supportedModelParams = map[string]struct{}{"model": {}, "api_key": {}, "api_base": {}, "timeout": {}, "stream_timeout": {}, "num_retries": {}, "aws_region_name": {}, "weight": {}}
var supportedGeneralSettings = map[string]struct{}{"master_key": {}, "resource_model": {}, "forward_traceparent_to_llm_provider": {}}
var supportedLiteLLMSettings = map[string]struct{}{"request_timeout": {}, "num_retries": {}, "cache": {}}
var supportedRouterSettings = map[string]struct{}{"model_group_alias": {}, "fallbacks": {}, "max_fallbacks": {}}
var supportedTopLevel = map[string]struct{}{"model_list": {}, "general_settings": {}, "litellm_settings": {}, "router_settings": {}, "environment_variables": {}}

func unsupportedKeys(contents []byte) ([]string, error) {
	tree := map[string]any{}
	if err := yaml.Unmarshal(contents, &tree); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	unsupported := []string{}
	seen := map[string]struct{}{}
	add := func(path string) {
		if _, found := seen[path]; !found {
			seen[path] = struct{}{}
			unsupported = append(unsupported, path)
		}
	}
	for key := range tree {
		if _, found := supportedTopLevel[key]; !found {
			add(key)
		}
	}
	entries, _ := tree["model_list"].([]any)
	for index, entry := range entries {
		record, _ := entry.(map[string]any)
		for key := range record {
			switch key {
			case "model_name", "litellm_params":
			default:
				add(fmt.Sprintf("model_list[%d].%s", index, key))
			}
		}
		params, _ := record["litellm_params"].(map[string]any)
		for key := range params {
			if _, found := supportedModelParams[key]; !found {
				add(fmt.Sprintf("model_list[%d].litellm_params.%s", index, key))
			}
		}
	}
	for section, supported := range map[string]map[string]struct{}{"general_settings": supportedGeneralSettings, "litellm_settings": supportedLiteLLMSettings, "router_settings": supportedRouterSettings} {
		record, _ := tree[section].(map[string]any)
		for key := range record {
			if _, found := supported[key]; !found {
				add(section + "." + key)
			}
		}
	}
	return unsupported, nil
}

func (config Config) WithRuntime(databaseURL string, redisURL string) Config {
	config.DatabaseURL = databaseURL
	config.RedisURL = redisURL
	return config
}

func parseModel(entry modelEntry) (Model, error) {
	name := strings.TrimSpace(entry.ModelName)
	if name == "" {
		return Model{}, errors.New("model_name is required")
	}

	modelName, err := resolveEnvironmentReference(entry.LiteLLMParams.Model)
	if err != nil {
		return Model{}, fmt.Errorf("resolve litellm_params.model: %w", err)
	}
	if strings.TrimSpace(modelName) == "" {
		return Model{}, errors.New("litellm_params.model is required")
	}

	apiKey, err := resolveEnvironmentReference(entry.LiteLLMParams.APIKey)
	if err != nil {
		return Model{}, fmt.Errorf("resolve litellm_params.api_key: %w", err)
	}
	apiBase, err := resolveEnvironmentReference(entry.LiteLLMParams.APIBase)
	if err != nil {
		return Model{}, fmt.Errorf("resolve litellm_params.api_base: %w", err)
	}

	return Model{Name: name, Model: modelName, APIKey: apiKey, APIBase: apiBase, Timeout: secondsDuration(entry.LiteLLMParams.Timeout), StreamTimeout: secondsDuration(entry.LiteLLMParams.StreamTimeout), NumRetries: entry.LiteLLMParams.NumRetries, AWSRegion: entry.LiteLLMParams.AWSRegion, Weight: entry.LiteLLMParams.Weight}, nil
}

func secondsDuration(value float64) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value * float64(time.Second))
}

func resolveEnvironmentReference(value string) (string, error) {
	if !strings.HasPrefix(value, environmentReferencePrefix) {
		return value, nil
	}

	name := strings.TrimPrefix(value, environmentReferencePrefix)
	if name == "" {
		return "", errors.New("environment variable name is empty")
	}

	return os.Getenv(name), nil
}

func redisURLFromEnvironment() string {
	if value := os.Getenv("REDIS_URL"); value != "" {
		return value
	}

	host := os.Getenv("REDIS_HOST")
	if host == "" {
		return ""
	}
	port := os.Getenv("REDIS_PORT")
	if port == "" {
		port = "6379"
	}
	return host + ":" + port
}

func LoadEnvFile(path string) error {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, value, found := strings.Cut(line, "=")
		if !found {
			return fmt.Errorf("invalid environment assignment %q", line)
		}
		name = strings.TrimSpace(name)
		value = strings.Trim(strings.TrimSpace(value), "\"")
		if name == "" {
			return fmt.Errorf("empty environment variable name in %q", line)
		}
		if _, exists := os.LookupEnv(name); !exists {
			if err := os.Setenv(name, value); err != nil {
				return fmt.Errorf("set environment variable %q: %w", name, err)
			}
		}
	}

	return scanner.Err()
}
