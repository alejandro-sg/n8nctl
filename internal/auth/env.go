package auth

import (
	"fmt"
	"strings"

	"github.com/LogicMonitor-IT/n8nctl/internal/config"
	clierrors "github.com/LogicMonitor-IT/n8nctl/internal/errors"
)

func ResolveAPIKey(getenv func(string) string, envName string, env config.Environment) (string, error) {
	if getenv == nil {
		return "", clierrors.New(clierrors.ExitInternal, clierrors.CodeInternalFailure, "getenv function is not configured", nil)
	}

	apiKeyEnv := strings.TrimSpace(env.APIKeyEnv)
	if apiKeyEnv == "" {
		return "", clierrors.New(clierrors.ExitUsage, clierrors.CodeConfigInvalid, fmt.Sprintf("environment %q is missing api_key_env", envName), nil)
	}

	names := append([]string{apiKeyEnv}, env.APIKeyEnvAliases...)
	var unresolvedName string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		apiKey := strings.TrimSpace(getenv(name))
		if apiKey == "" {
			continue
		}
		if strings.HasPrefix(apiKey, "op://") {
			unresolvedName = name
			continue
		}
		return apiKey, nil
	}

	if unresolvedName != "" {
		return "", clierrors.New(clierrors.ExitSafety, clierrors.CodeAPIKeyMissing, fmt.Sprintf("environment variable %s contains an unresolved 1Password reference", unresolvedName), missingAPIKeyDetails(envName, apiKeyEnv, env.APIKeyEnvAliases, unresolvedName, "op_reference_not_resolved"))
	}

	return "", clierrors.New(clierrors.ExitSafety, clierrors.CodeAPIKeyMissing, fmt.Sprintf("environment variable %s is not set", apiKeyEnv), missingAPIKeyDetails(envName, apiKeyEnv, env.APIKeyEnvAliases, apiKeyEnv, "missing"))
}

func missingAPIKeyDetails(envName, apiKeyEnv string, aliases []string, checkedName string, problem string) map[string]any {
	return map[string]any{
		"environment": envName,
		"envVar":      apiKeyEnv,
		"aliases":     cleanAliases(aliases),
		"checkedVar":  checkedName,
		"problem":     problem,
		"summary":     "n8nctl reads API keys from process environment variables only; it does not call 1Password directly.",
		"instructions": []string{
			"Use a real n8n API key value in the configured environment variable.",
			"For 1Password, run: n8nctl init --with-1password --onepassword-vault <vault>",
			"Then run: source .n8nctl/load-1password-env.sh",
			"After sourcing, run n8nctl commands directly in the same shell without op run.",
			"For one-off commands only, use: op run --env-file=.env.1password -- n8nctl ...",
			"If op prompts or fails repeatedly, restart 1Password Desktop and verify CLI integration is enabled.",
		},
		"agentInstructions": []string{
			"Before invoking n8nctl, ensure the child process environment contains the resolved API key value.",
			"Do not pass an op:// reference as the API key value.",
			"Prefer project-local .env.1password plus source .n8nctl/load-1password-env.sh for repeated commands.",
			"Do not wrap every n8nctl invocation in op run when repeated commands are expected; resolve once into the shell instead.",
		},
		"safeChecks": []string{
			"test -n \"$" + apiKeyEnv + "\" && echo " + apiKeyEnv + " is loaded",
			"case \"$" + apiKeyEnv + "\" in op://*) echo unresolved 1Password reference ;; *) echo resolved value present ;; esac",
		},
	}
}

func cleanAliases(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}
