package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadResolvesEnvironmentReferences(t *testing.T) {
	t.Setenv("TEST_OPENAI_KEY", "test-key")
	t.Setenv("LITELLM_MASTER_KEY", "environment-master-key")
	configPath := writeConfig(t, `
model_list:
  - model_name: gpt-test
    litellm_params:
      model: openai/gpt-test
      api_key: os.environ/TEST_OPENAI_KEY
general_settings:
  master_key: configured-master-key
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.MasterKey != "environment-master-key" {
		t.Fatalf("MasterKey = %q, want environment-master-key", loaded.MasterKey)
	}
	if len(loaded.Models) != 1 {
		t.Fatalf("len(Models) = %d, want 1", len(loaded.Models))
	}
	if loaded.Models[0].APIKey != "test-key" {
		t.Fatalf("APIKey = %q, want test-key", loaded.Models[0].APIKey)
	}
}

func TestLoadPreservesRouterAndRetrySettings(t *testing.T) {
	configPath := writeConfig(t, `
model_list:
  - model_name: deployment
    litellm_params:
      model: openai/gpt-test
      timeout: 2.5
      stream_timeout: 3
      num_retries: 4
      aws_region_name: us-east-1
litellm_settings:
  request_timeout: 10
  num_retries: 2
router_settings:
  model_group_alias:
    public-name: deployment
`)
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RequestTimeout != 10*time.Second || loaded.NumRetries != 2 || loaded.ModelAliases["public-name"] != "deployment" {
		t.Fatalf("config = %#v", loaded)
	}
	if loaded.Models[0].Timeout != 2500*time.Millisecond || loaded.Models[0].StreamTimeout != 3*time.Second || loaded.Models[0].NumRetries != 4 || loaded.Models[0].AWSRegion != "us-east-1" {
		t.Fatalf("model = %#v", loaded.Models[0])
	}
}

func TestLoadRejectsModelWithoutProviderModel(t *testing.T) {
	configPath := writeConfig(t, `
model_list:
  - model_name: gpt-test
    litellm_params: {}
`)

	if _, err := Load(configPath); err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
}

func TestLoadEnvFileDoesNotOverrideExistingEnvironment(t *testing.T) {
	t.Setenv("LITELLM_TEST_VALUE", "existing")
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("LITELLM_TEST_VALUE=from-file\n"), 0o600); err != nil {
		t.Fatalf("write environment file: %v", err)
	}

	if err := LoadEnvFile(envPath); err != nil {
		t.Fatalf("LoadEnvFile() error = %v", err)
	}
	if value := os.Getenv("LITELLM_TEST_VALUE"); value != "existing" {
		t.Fatalf("environment value = %q, want existing", value)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
