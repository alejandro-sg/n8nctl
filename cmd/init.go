package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/alejandro-sg/n8nctl/internal/config"
	clierrors "github.com/alejandro-sg/n8nctl/internal/errors"
)

func newInitCmd(a *app) *cobra.Command {
	var force bool
	var withOnePassword bool
	var onePasswordVault string
	var onePasswordItemPrefix string
	var onePasswordField string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create repo-local n8nctl config and optional 1Password env files",
		RunE: func(cmd *cobra.Command, args []string) error {
			result := map[string]any{
				"status": "created",
			}
			created := make([]string, 0, 3)
			skipped := make([]string, 0, 1)
			nextSteps := make([]string, 0, 3)

			configPath := filepath.Join(a.deps.WorkingDir, config.FileName)
			writtenPath, err := config.WriteDefault(configPath, force)
			if err != nil {
				if withOnePassword && clierrors.Code(err) == clierrors.CodeUsageError {
					skipped = append(skipped, configPath)
				} else {
					return err
				}
			} else {
				created = append(created, writtenPath)
				result["path"] = writtenPath
			}

			if withOnePassword {
				envPath, loaderPath, err := writeOnePasswordProjectFiles(a.deps.WorkingDir, force, onePasswordVault, onePasswordItemPrefix, onePasswordField)
				if err != nil {
					return err
				}
				created = append(created, envPath, loaderPath)
				result["onePasswordEnvPath"] = envPath
				result["onePasswordLoaderPath"] = loaderPath
				nextSteps = append(nextSteps,
					"Edit .env.1password if the 1Password vault/item names differ.",
					"Run: source .n8nctl/load-1password-env.sh",
					"Then run n8nctl commands directly in the same shell.",
				)
			}
			result["created"] = created
			if len(skipped) > 0 {
				result["skipped"] = skipped
			}
			if len(nextSteps) > 0 {
				result["nextSteps"] = nextSteps
			}

			if a.opts.JSON {
				return a.printJSON(result)
			}

			for _, path := range created {
				fmt.Fprintf(a.deps.Streams.Out, "Created %s\n", path)
			}
			for _, path := range skipped {
				fmt.Fprintf(a.deps.Streams.Out, "Skipped existing %s\n", path)
			}
			if len(nextSteps) > 0 {
				fmt.Fprintln(a.deps.Streams.Out, "\nNext steps:")
				for _, step := range nextSteps {
					fmt.Fprintf(a.deps.Streams.Out, "- %s\n", step)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing .n8nctl.yaml")
	cmd.Flags().BoolVar(&withOnePassword, "with-1password", false, "create local .env.1password and sourceable shell loader")
	cmd.Flags().StringVar(&onePasswordVault, "onepassword-vault", "Engineering", "1Password vault name for generated op:// references")
	cmd.Flags().StringVar(&onePasswordItemPrefix, "onepassword-item-prefix", "n8nctl-api-key", "1Password item name prefix for generated op:// references")
	cmd.Flags().StringVar(&onePasswordField, "onepassword-field", "credential", "1Password field name containing the n8n API key")
	return cmd
}

func writeOnePasswordProjectFiles(root string, force bool, vault string, itemPrefix string, field string) (string, string, error) {
	envPath := filepath.Join(root, ".env.1password")
	loaderDir := filepath.Join(root, ".n8nctl")
	loaderPath := filepath.Join(loaderDir, "load-1password-env.sh")

	if err := writeFileIfAllowed(envPath, []byte(onePasswordEnvTemplate(vault, itemPrefix, field)), 0o600, force); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(loaderDir, 0o755); err != nil {
		return "", "", clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to create .n8nctl directory", map[string]any{
			"dir": loaderDir,
		})
	}
	if err := writeFileIfAllowed(loaderPath, []byte(onePasswordLoaderScript()), 0o755, force); err != nil {
		return "", "", err
	}

	absEnvPath, err := filepath.Abs(envPath)
	if err == nil {
		envPath = absEnvPath
	}
	absLoaderPath, err := filepath.Abs(loaderPath)
	if err == nil {
		loaderPath = absLoaderPath
	}
	return envPath, loaderPath, nil
}

func writeFileIfAllowed(path string, contents []byte, mode os.FileMode, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, fmt.Sprintf("%s already exists; rerun with --force to overwrite", filepath.Base(path)), map[string]any{
			"path": path,
		})
	} else if err != nil && !os.IsNotExist(err) {
		return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeConfigInvalid, "failed to inspect local init file", map[string]any{
			"path": path,
		})
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		return clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to write local init file", map[string]any{
			"path": path,
		})
	}
	return nil
}

func onePasswordEnvTemplate(vault string, itemPrefix string, field string) string {
	return fmt.Sprintf(`# Local n8nctl 1Password references. This file is ignored by Git.
# Resolve these once per shell with:
#   source .n8nctl/load-1password-env.sh
#
# n8nctl expects resolved API key values in N8N_*_API_KEY.
# Do not export op:// references directly before running n8nctl.

N8N_PROD_API_KEY="op://%s/%s-prod/%s"

# Add a separate development item and reference if needed:
# N8N_DEV_API_KEY="op://%s/%s-dev/%s"
`, vault, itemPrefix, field, vault, itemPrefix, field)
}

func onePasswordLoaderScript() string {
	return `#!/usr/bin/env sh

# Source this file to resolve local .env.1password references into this shell.
# Usage: source .n8nctl/load-1password-env.sh [env-file]

is_sourced() {
  if [ -n "${ZSH_VERSION:-}" ]; then
    case "${ZSH_EVAL_CONTEXT:-}" in
      *:file:*) return 0 ;;
    esac
    return 1
  fi

  if [ -n "${BASH_VERSION:-}" ]; then
    [ "${BASH_SOURCE:-}" != "$0" ]
    return $?
  fi

  return 1
}

trim() {
  printf '%s' "$1" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
}

unquote() {
  value="$1"
  case "$value" in
    \"*\")
      value="${value#\"}"
      value="${value%\"}"
      ;;
    \'*\')
      value="${value#\'}"
      value="${value%\'}"
      ;;
  esac
  printf '%s' "$value"
}

find_env_file() {
  if [ -n "${1:-}" ]; then
    printf '%s\n' "$1"
    return 0
  fi

  dir="$PWD"
  while [ "$dir" != "/" ]; do
    if [ -f "$dir/.env.1password" ]; then
      printf '%s\n' "$dir/.env.1password"
      return 0
    fi
    dir="$(dirname "$dir")"
  done

  printf '%s\n' ".env.1password"
}

fail() {
  printf 'load-1password-env: %s\n' "$1" >&2
  return 1
}

if ! is_sourced; then
  printf 'load-1password-env: this script must be sourced so it can export variables into your current shell\n' >&2
  printf 'usage: source .n8nctl/load-1password-env.sh [env-file]\n' >&2
  exit 1
fi

env_file="$(find_env_file "${1:-}")"

if [ ! -f "$env_file" ]; then
  fail "env file not found: $env_file"
  return 1
fi

if ! command -v op >/dev/null 2>&1; then
  fail "1Password CLI not found on PATH"
  return 1
fi

loaded=0

while IFS= read -r raw_line || [ -n "$raw_line" ]; do
  line="$(trim "$raw_line")"

  case "$line" in
    ''|\#*) continue ;;
  esac

  case "$line" in
    *=*) ;;
    *)
      fail "invalid line in $env_file: $raw_line"
      return 1
      ;;
  esac

  name="$(trim "${line%%=*}")"
  value="$(trim "${line#*=}")"
  value="$(unquote "$value")"

  if ! printf '%s' "$name" | grep -Eq '^[A-Za-z_][A-Za-z0-9_]*$'; then
    fail "invalid environment variable name: $name"
    return 1
  fi

  case "$value" in
    op://*) ;;
    *)
      fail "$name must use an op:// secret reference"
      return 1
      ;;
  esac

  secret="$(op read "$value")" || {
    fail "failed to read 1Password secret for $name"
    return 1
  }

  export "$name=$secret"
  loaded=$((loaded + 1))
done < "$env_file"

printf 'load-1password-env: loaded %s secret(s) from %s into this shell\n' "$loaded" "$env_file"
`
}
