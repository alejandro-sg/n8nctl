package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/LogicMonitor-IT/n8nctl/internal/errors"
	"github.com/LogicMonitor-IT/n8nctl/internal/output"
)

func newEnvCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Inspect configured n8n environments",
	}
	cmd.AddCommand(newEnvListCmd(a), newEnvDoctorCmd(a), newEnvLoadCmd(a))
	return cmd
}

func newEnvListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig()
			if err != nil {
				return err
			}

			names := make([]string, 0, len(cfg.Environments))
			for name := range cfg.Environments {
				names = append(names, name)
			}
			sort.Strings(names)

			type environmentRow struct {
				Name      string `json:"name"`
				Default   bool   `json:"default"`
				BaseURL   string `json:"baseUrl"`
				APIKeyEnv string `json:"apiKeyEnv"`
			}

			rows := make([]environmentRow, 0, len(names))
			for _, name := range names {
				env := cfg.Environments[name]
				rows = append(rows, environmentRow{
					Name:      name,
					Default:   name == cfg.DefaultEnv,
					BaseURL:   env.BaseURL,
					APIKeyEnv: env.APIKeyEnv,
				})
			}

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":       "ok",
					"defaultEnv":   cfg.DefaultEnv,
					"environments": rows,
				})
			}

			tableRows := make([][]string, 0, len(rows))
			for _, row := range rows {
				defaultMarker := ""
				if row.Default {
					defaultMarker = "*"
				}
				tableRows = append(tableRows, []string{defaultMarker, row.Name, row.BaseURL, row.APIKeyEnv})
			}

			if err := output.WriteTable(a.deps.Streams.Out, []string{"DEFAULT", "NAME", "BASE_URL", "API_KEY_ENV"}, tableRows); err != nil {
				return err
			}
			_, err = fmt.Fprintln(a.deps.Streams.Out, "\n* marks the default environment")
			return err
		},
	}
}

func newEnvDoctorCmd(a *app) *cobra.Command {
	var envName string
	var all bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check configured environment variables and unresolved secret references",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig()
			if err != nil {
				return err
			}
			names := make([]string, 0)
			if all {
				for name := range cfg.Environments {
					names = append(names, name)
				}
				sort.Strings(names)
			} else {
				name, _, err := cfg.ResolveEnvironment(envName)
				if err != nil {
					return err
				}
				names = append(names, name)
			}

			type doctorRow struct {
				Environment string   `json:"environment"`
				BaseURL     string   `json:"baseUrl"`
				APIKeyEnv   string   `json:"apiKeyEnv"`
				Aliases     []string `json:"aliases,omitempty"`
				ResolvedVar string   `json:"resolvedVar,omitempty"`
				Status      string   `json:"status"`
				Problem     string   `json:"problem,omitempty"`
			}
			rows := make([]doctorRow, 0, len(names))
			for _, name := range names {
				env := cfg.Environments[name]
				row := doctorRow{
					Environment: name,
					BaseURL:     env.BaseURL,
					APIKeyEnv:   env.APIKeyEnv,
					Aliases:     env.APIKeyEnvAliases,
					Status:      "missing",
					Problem:     "no configured API key environment variable is set",
				}
				for _, candidate := range append([]string{env.APIKeyEnv}, env.APIKeyEnvAliases...) {
					candidate = strings.TrimSpace(candidate)
					if candidate == "" {
						continue
					}
					value := strings.TrimSpace(a.deps.Getenv(candidate))
					if value == "" {
						continue
					}
					row.ResolvedVar = candidate
					if strings.HasPrefix(value, "op://") {
						row.Status = "unresolved_secret"
						row.Problem = "environment variable contains an unresolved 1Password reference"
						continue
					}
					row.Status = "ok"
					row.Problem = ""
					break
				}
				rows = append(rows, row)
			}

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":       "ok",
					"environments": rows,
				})
			}
			tableRows := make([][]string, 0, len(rows))
			for _, row := range rows {
				tableRows = append(tableRows, []string{row.Environment, row.BaseURL, row.APIKeyEnv, strings.Join(row.Aliases, ","), row.ResolvedVar, row.Status, row.Problem})
			}
			return output.WriteTable(a.deps.Streams.Out, []string{"ENV", "BASE_URL", "API_KEY_ENV", "ALIASES", "RESOLVED_VAR", "STATUS", "PROBLEM"}, tableRows)
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "environment name to check")
	cmd.Flags().BoolVar(&all, "all", false, "check all configured environments")
	return cmd
}

func newEnvLoadCmd(a *app) *cobra.Command {
	var loader string
	var format string
	var envFile string

	cmd := &cobra.Command{
		Use:   "load",
		Short: "Render secret-manager environment loading commands",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, configPath, err := a.loadConfig()
			if err == nil {
				if loader == "" {
					loader = cfg.Secrets.Loader
				}
				if envFile == "" {
					envFile = cfg.Secrets.OnePasswordEnvFile
				}
				if !filepath.IsAbs(envFile) {
					envFile = filepath.Join(cfg.ConfigDir(configPath), envFile)
				}
			} else {
				if loader == "" {
					loader = "1password"
				}
				if envFile == "" {
					envFile = filepath.Join(a.deps.WorkingDir, ".env.1password")
				}
			}
			if loader == "" || loader == "none" {
				loader = "1password"
			}
			if loader != "1password" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "env load currently supports --loader 1password", map[string]any{"loader": loader})
			}
			if format == "" {
				format = "sh"
			}
			entries, err := readEnvReferenceFile(envFile)
			if err != nil {
				return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeConfigInvalid, "failed to read env reference file", map[string]any{"path": envFile})
			}
			switch format {
			case "json":
				return a.printJSON(map[string]any{
					"status":  "ok",
					"loader":  loader,
					"envFile": envFile,
					"entries": entries,
				})
			case "sh":
				names := make([]string, 0, len(entries))
				for name := range entries {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					value := entries[name]
					if strings.HasPrefix(value, "op://") {
						fmt.Fprintf(a.deps.Streams.Out, "export %s=\"$(op read %s)\"\n", name, shellQuote(value))
					} else {
						fmt.Fprintf(a.deps.Streams.Out, "export %s=%s\n", name, shellQuote(value))
					}
				}
				return nil
			default:
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "--format must be sh or json", map[string]any{"format": format})
			}
		},
	}
	cmd.Flags().StringVar(&loader, "loader", "", "secret loader to use; currently 1password")
	cmd.Flags().StringVar(&format, "format", "sh", "output format: sh or json")
	cmd.Flags().StringVar(&envFile, "env-file", "", "env reference file to read")
	return cmd
}

func readEnvReferenceFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if name != "" {
			entries[name] = value
		}
	}
	return entries, scanner.Err()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
