package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	clierrors "github.com/alejandro-sg/n8nctl/internal/errors"
)

const FileName = ".n8nctl.yaml"

type Config struct {
	DefaultEnv   string                 `mapstructure:"default_env"`
	Environments map[string]Environment `mapstructure:"environments"`
	Workflows    WorkflowConfig         `mapstructure:"workflows"`
	Validation   ValidationConfig       `mapstructure:"validation"`
	Secrets      SecretsConfig          `mapstructure:"secrets"`
	Safety       SafetyConfig           `mapstructure:"safety"`
}

type Environment struct {
	BaseURL          string   `mapstructure:"base_url"`
	APIKeyEnv        string   `mapstructure:"api_key_env"`
	APIKeyEnvAliases []string `mapstructure:"api_key_env_aliases"`
	DefaultProject   string   `mapstructure:"default_project"`
}

type WorkflowConfig struct {
	Path         string `mapstructure:"path"`
	NameStrategy string `mapstructure:"name_strategy"`
}

type SafetyConfig struct {
	RequireConfirmForProd bool `mapstructure:"require_confirm_for_prod"`
	BackupBeforeUpdate    bool `mapstructure:"backup_before_update"`
	DeployInactiveDefault bool `mapstructure:"deploy_inactive_by_default"`
}

type ValidationConfig struct {
	Engine               string `mapstructure:"engine"`
	N8NVersion           string `mapstructure:"n8n_version"`
	RequireRemoteContext bool   `mapstructure:"require_remote_context"`
	CredentialPreflight  string `mapstructure:"credential_preflight"`
}

type SecretsConfig struct {
	Loader             string `mapstructure:"loader"`
	OnePasswordEnvFile string `mapstructure:"onepassword_env_file"`
}

func LoadFromDir(startDir string) (*Config, string, error) {
	path, err := FindConfig(startDir)
	if err != nil {
		return nil, "", err
	}
	cfg, err := Load(path)
	if err != nil {
		return nil, "", err
	}
	return cfg, path, nil
}

func FindConfig(startDir string) (string, error) {
	if strings.TrimSpace(startDir) == "" {
		return "", clierrors.New(
			clierrors.ExitUsage,
			clierrors.CodeConfigMissing,
			"working directory is required to locate .n8nctl.yaml",
			nil,
		)
	}

	current := startDir
	for {
		candidate := filepath.Join(current, FileName)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate, nil
		}
		next := filepath.Dir(current)
		if next == current {
			break
		}
		current = next
	}

	return "", clierrors.New(
		clierrors.ExitUsage,
		clierrors.CodeConfigMissing,
		"could not find .n8nctl.yaml in the current directory or its parents",
		nil,
	)
}

func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetDefault("workflows.path", "workflows")
	v.SetDefault("workflows.name_strategy", "file_or_json_name")
	v.SetDefault("validation.engine", "n8n-runtime")
	v.SetDefault("validation.n8n_version", "2.17.5")
	v.SetDefault("validation.require_remote_context", false)
	v.SetDefault("validation.credential_preflight", "")
	v.SetDefault("secrets.loader", "none")
	v.SetDefault("secrets.onepassword_env_file", ".env.1password")
	v.SetDefault("safety.require_confirm_for_prod", true)
	v.SetDefault("safety.backup_before_update", true)
	v.SetDefault("safety.deploy_inactive_by_default", true)

	if err := v.ReadInConfig(); err != nil {
		return nil, clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeConfigInvalid, "failed to read .n8nctl.yaml", map[string]any{
			"path": path,
		})
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeConfigInvalid, "failed to parse .n8nctl.yaml", map[string]any{
			"path": path,
		})
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if len(c.Environments) == 0 {
		return clierrors.New(clierrors.ExitUsage, clierrors.CodeConfigInvalid, "config must define at least one environment", nil)
	}
	for name, env := range c.Environments {
		if strings.TrimSpace(env.BaseURL) == "" {
			return clierrors.New(clierrors.ExitUsage, clierrors.CodeConfigInvalid, fmt.Sprintf("environment %q is missing base_url", name), nil)
		}
		if strings.TrimSpace(env.APIKeyEnv) == "" {
			return clierrors.New(clierrors.ExitUsage, clierrors.CodeConfigInvalid, fmt.Sprintf("environment %q is missing api_key_env", name), nil)
		}
	}
	if c.DefaultEnv != "" {
		if _, ok := c.Environments[c.DefaultEnv]; !ok {
			return clierrors.New(clierrors.ExitUsage, clierrors.CodeConfigInvalid, "default_env does not exist in environments", map[string]any{
				"defaultEnv": c.DefaultEnv,
			})
		}
	}
	if mode := strings.TrimSpace(c.Validation.CredentialPreflight); mode != "" && !validCredentialPreflightMode(mode) {
		return clierrors.New(clierrors.ExitUsage, clierrors.CodeConfigInvalid, "validation.credential_preflight must be fail, warn, or skip", map[string]any{
			"value": c.Validation.CredentialPreflight,
		})
	}
	if loader := strings.TrimSpace(c.Secrets.Loader); loader != "" && loader != "none" && loader != "1password" {
		return clierrors.New(clierrors.ExitUsage, clierrors.CodeConfigInvalid, "secrets.loader must be none or 1password", map[string]any{
			"value": c.Secrets.Loader,
		})
	}
	return nil
}

func (c *Config) ResolveEnvironment(requested string) (string, Environment, error) {
	name := strings.TrimSpace(requested)
	if name == "" {
		name = strings.TrimSpace(c.DefaultEnv)
	}
	if name == "" {
		return "", Environment{}, clierrors.New(
			clierrors.ExitSafety,
			clierrors.CodeEnvironmentMissing,
			"no environment selected; pass --env or set default_env in .n8nctl.yaml",
			nil,
		)
	}

	env, ok := c.Environments[name]
	if !ok {
		return "", Environment{}, clierrors.New(
			clierrors.ExitUsage,
			clierrors.CodeEnvironmentMissing,
			fmt.Sprintf("environment %q is not defined in .n8nctl.yaml", name),
			nil,
		)
	}
	return name, env, nil
}

func (c *Config) ConfigDir(configPath string) string {
	return filepath.Dir(configPath)
}

func (c *Config) BackupDir(configPath, envName string) string {
	return filepath.Join(c.ConfigDir(configPath), ".n8nctl", "backups", envName)
}

func (c *Config) IsProductionEnv(name string) bool {
	return strings.Contains(strings.ToLower(name), "prod")
}

func (c *Config) ProductionHosts(currentEnv string) []string {
	hosts := make([]string, 0, len(c.Environments))
	for name, env := range c.Environments {
		if strings.EqualFold(name, currentEnv) {
			continue
		}
		if !c.IsProductionEnv(name) {
			continue
		}
		baseURL := strings.TrimSpace(env.BaseURL)
		if baseURL == "" {
			continue
		}
		hosts = append(hosts, baseURL)
	}
	return hosts
}

func WriteDefault(path string, force bool) (string, error) {
	if _, err := os.Stat(path); err == nil && !force {
		return "", clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, ".n8nctl.yaml already exists; rerun with --force to overwrite", map[string]any{
			"path": path,
		})
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeConfigInvalid, "failed to inspect .n8nctl.yaml", map[string]any{
			"path": path,
		})
	}

	if err := os.WriteFile(path, []byte(DefaultTemplate()), 0o644); err != nil {
		return "", clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to write .n8nctl.yaml", map[string]any{
			"path": path,
		})
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return absPath, nil
}

func DefaultTemplate() string {
	return `default_env: prod

environments:
  dev:
    base_url: https://company-dev.app.n8n.cloud
    api_key_env: N8N_DEV_API_KEY
    default_project: Development

  prod:
    base_url: https://company.app.n8n.cloud
    api_key_env: N8N_PROD_API_KEY
    default_project: Production

workflows:
  path: workflows
  name_strategy: file_or_json_name

validation:
  engine: n8n-runtime
  n8n_version: 2.17.5
  require_remote_context: false
  credential_preflight: ""

secrets:
  loader: none
  onepassword_env_file: .env.1password

safety:
  require_confirm_for_prod: true
  backup_before_update: true
  deploy_inactive_by_default: true
`
}

func (c *Config) applyDefaults() {
	if c.Workflows.Path == "" {
		c.Workflows.Path = "workflows"
	}
	if c.Workflows.NameStrategy == "" {
		c.Workflows.NameStrategy = "file_or_json_name"
	}
	if c.Validation.Engine == "" {
		c.Validation.Engine = "n8n-runtime"
	}
	if c.Validation.N8NVersion == "" {
		c.Validation.N8NVersion = "2.17.5"
	}
	if c.Secrets.Loader == "" {
		c.Secrets.Loader = "none"
	}
	if c.Secrets.OnePasswordEnvFile == "" {
		c.Secrets.OnePasswordEnvFile = ".env.1password"
	}
}

func validCredentialPreflightMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "fail", "warn", "skip":
		return true
	default:
		return false
	}
}
