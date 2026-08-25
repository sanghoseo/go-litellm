package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const environmentReferencePrefix = "os.environ/"

type Config struct {
	MasterKey   string
	Models      []Model
	DatabaseURL string
	RedisURL    string
}

type Model struct {
	Name    string
	Model   string
	APIKey  string
	APIBase string
}

type document struct {
	ModelList       []modelEntry      `yaml:"model_list"`
	GeneralSettings generalSettings   `yaml:"general_settings"`
	Environment     map[string]string `yaml:"environment_variables"`
}

type modelEntry struct {
	ModelName     string      `yaml:"model_name"`
	LiteLLMParams modelParams `yaml:"litellm_params"`
}

type modelParams struct {
	Model   string `yaml:"model"`
	APIKey  string `yaml:"api_key"`
	APIBase string `yaml:"api_base"`
}

type generalSettings struct {
	MasterKey string `yaml:"master_key"`
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
		MasterKey:   masterKey,
		Models:      models,
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RedisURL:    redisURLFromEnvironment(),
	}, nil
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

	return Model{Name: name, Model: modelName, APIKey: apiKey, APIBase: apiBase}, nil
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
