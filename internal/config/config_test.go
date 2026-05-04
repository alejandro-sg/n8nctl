package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromDirResolvesParentConfig(t *testing.T) {
	t.Helper()

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(root, FileName)
	configBody := `default_env: dev
environments:
  dev:
    base_url: https://dev.example.com
    api_key_env: N8N_DEV_API_KEY
    api_key_env_aliases:
      - TEAM_N8N_DEV_KEY
  prod:
    base_url: https://prod.example.com
    api_key_env: N8N_PROD_API_KEY
safety:
  require_confirm_for_prod: false
  backup_before_update: false
  deploy_inactive_by_default: false
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, foundPath, err := LoadFromDir(nested)
	if err != nil {
		t.Fatalf("LoadFromDir() error = %v", err)
	}
	if foundPath != configPath {
		t.Fatalf("LoadFromDir() path = %q, want %q", foundPath, configPath)
	}
	if cfg.DefaultEnv != "dev" {
		t.Fatalf("DefaultEnv = %q, want dev", cfg.DefaultEnv)
	}
	if cfg.Safety.RequireConfirmForProd {
		t.Fatalf("RequireConfirmForProd = true, want false")
	}
	if cfg.Safety.BackupBeforeUpdate {
		t.Fatalf("BackupBeforeUpdate = true, want false")
	}
	if cfg.Safety.DeployInactiveDefault {
		t.Fatalf("DeployInactiveDefault = true, want false")
	}
	if got := cfg.Environments["dev"].APIKeyEnvAliases; len(got) != 1 || got[0] != "TEAM_N8N_DEV_KEY" {
		t.Fatalf("APIKeyEnvAliases = %#v", got)
	}
	if cfg.Secrets.Loader != "none" {
		t.Fatalf("Secrets.Loader = %q, want none", cfg.Secrets.Loader)
	}
}

func TestResolveEnvironmentUsesDefault(t *testing.T) {
	cfg := &Config{
		DefaultEnv: "prod",
		Environments: map[string]Environment{
			"prod": {
				BaseURL:   "https://prod.example.com",
				APIKeyEnv: "N8N_PROD_API_KEY",
			},
		},
	}

	name, env, err := cfg.ResolveEnvironment("")
	if err != nil {
		t.Fatalf("ResolveEnvironment() error = %v", err)
	}
	if name != "prod" {
		t.Fatalf("name = %q, want prod", name)
	}
	if env.BaseURL != "https://prod.example.com" {
		t.Fatalf("BaseURL = %q", env.BaseURL)
	}
}
