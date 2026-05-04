# `n8nctl`

`n8nctl` is a Go CLI for validating, diffing, deploying, and inspecting n8n Cloud workflows from the terminal.

It provides:

- deterministic commands
- machine-readable JSON output
- explicit safety gates for production deploys
- repo-local configuration and local workflow files
- release automation for tagged binaries and team updates

## Team Rollout

For shared use, commit only example files and keep local operator config untracked:

- `.n8nctl.example.yaml`
- `.env.1password.example`

Each teammate should copy them to local files:

```bash
cp .n8nctl.example.yaml .n8nctl.yaml
cp .env.1password.example .env.1password
```

The local `.n8nctl.yaml` and `.env.1password` files are ignored by Git.
Alternatively, run `n8nctl init --with-1password --onepassword-vault <vault>` inside a project to generate `.n8nctl.yaml`, `.env.1password`, and `.n8nctl/load-1password-env.sh`.

## Quick Start

Create the repo-local config:

```bash
n8nctl init
```

Create config plus local 1Password env files for this project:

```bash
n8nctl init --with-1password --onepassword-vault Employee
source .n8nctl/load-1password-env.sh
```

Set your API key:

```bash
export N8N_PROD_API_KEY="your-api-key"
```

Or load it once per shell session from 1Password CLI:

```bash
source .n8nctl/load-1password-env.sh
n8nctl workflow list --env prod
```

Install the latest released binary for your platform:

```bash
./scripts/install.sh
```

If your shell does not already include the install directory on `PATH`, let the installer add it to your shell profile:

```bash
./scripts/install.sh --setup-path
```

Validate a workflow:

```bash
n8nctl workflow validate workflows/slack-alert.json
```

Diff against the remote environment:

```bash
n8nctl workflow diff workflows/slack-alert.json --env dev
```

Deploy to dev:

```bash
n8nctl workflow deploy workflows/slack-alert.json --env dev
```

Activate explicitly when you are ready:

```bash
n8nctl workflow activate slack-alert --env dev
```

Inspect executions:

```bash
n8nctl execution list --workflow slack-alert --env dev
```

## Codex MCP Server

`n8nctl` can run as a local stdio MCP server for Codex:

```bash
n8nctl mcp serve
```

Project-scoped Codex config example:

```toml
[mcp_servers.n8nctl]
command = "n8nctl"
args = ["mcp", "serve"]
cwd = "/path/to/n8nctl"
env_vars = ["N8N_DEV_API_KEY", "N8N_PROD_API_KEY"]
startup_timeout_sec = 10
tool_timeout_sec = 120
```

Replace `/path/to/n8nctl` with the local checkout path. Codex launches stdio MCP servers itself; do not keep `n8nctl mcp serve` running in a separate terminal. If you run it manually for debugging, it will appear silent because stdout is reserved for MCP JSON-RPC messages. If Codex cannot find `n8nctl` on `PATH`, set `command` to the absolute path of the installed `n8nctl` binary.

Read-only installs can restrict the exposed tool set in Codex config:

```toml
[mcp_servers.n8nctl]
command = "n8nctl"
args = ["mcp", "serve"]
enabled_tools = [
  "version",
  "env_list",
  "env_doctor",
  "project_list",
  "workflow_list",
  "workflow_get",
  "workflow_validate",
  "workflow_diff",
  "workflow_drift",
  "workflow_issues",
  "workflow_dependencies",
  "workflow_doctor",
  "execution_list",
  "execution_get",
  "execution_wait",
  "execution_failures",
  "execution_diagnose",
]
```

Alternatively, keep the full server available but disable write tools:

```toml
[mcp_servers.n8nctl]
command = "n8nctl"
args = ["mcp", "serve"]
disabled_tools = [
  "workflow_deploy",
  "workflow_create",
  "workflow_move",
  "workflow_clone",
  "workflow_run",
  "workflow_activate",
  "workflow_deactivate",
  "workflow_cleanup",
  "workflow_rebind_credential",
  "execution_retry",
]
```

MCP write tools default to dry-run. A real mutation requires `dry_run=false` and `confirm_mutation=true`; production mutations also require the `confirmation_phrase` returned by a matching prior dry run. Successful dry-run mutation responses include `nextCall`, a ready-to-use MCP tool call that preserves the matching arguments and adds the required confirmation fields for Codex to use after user approval. For MCP deploy/create/rebind mutations, `credential_preflight=fail` is the default, `credential_preflight=warn` can be explicitly confirmed, and `credential_preflight=skip` is only allowed for dry-runs. MCP file inputs and backup paths are constrained to the workspace, responses are sanitized, mutating calls are serialized, and `.n8nctl/audit/mcp.jsonl` records MCP actions without secrets.

## Config

`n8nctl` loads a repo-local `.n8nctl.yaml` from the current directory or its parents.

Example:

```yaml
default_env: prod

environments:
  dev:
    base_url: https://company-dev.app.n8n.cloud
    api_key_env: N8N_DEV_API_KEY
    api_key_env_aliases:
      - TEAM_N8N_DEV_API_KEY
    default_project: HR & Workplace Experience

  prod:
    base_url: https://company.app.n8n.cloud
    api_key_env: N8N_PROD_API_KEY
    default_project: HR & Workplace Experience

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
```

API keys are never stored in config. They are resolved from each environment’s `api_key_env`, then from optional `api_key_env_aliases`.

## Validation

`workflow validate`, `workflow issues`, `workflow deploy`, and `workflow doctor` emit normalized findings with stable codes, severity, node name/id, JSON path, source, and remediation text.

Local file validation runs the Go structural validator plus the configured `validation.engine`. The default `n8n-runtime` engine uses the embedded runtime validator and metadata-driven node rules to catch editor-style setup blockers such as missing required node parameters, missing node types, broken connections, malformed expressions, missing required credentials, and HTTP Request Google authentication caveats.

If the optional `tools/n8n-validator` Node dependencies are installed, `n8nctl` also verifies that the local `n8n` package version matches the embedded validator metadata version. If those dependencies are not installed, validation still runs with embedded metadata and emits a warning.

Google Drive and Google Sheets nodes may use either OAuth2 credentials or Google Service Account API credentials. For these nodes, `n8nctl` accepts both the OAuth2 credential keys (`googleDriveOAuth2Api`, `googleSheetsOAuth2Api`) and the service account key (`googleApi`) during local/runtime validation.

Remote `workflow issues`, `workflow doctor`, `workflow create`, and `workflow deploy` use public `/api/v1` workflow data plus credential/project preflight. They don't call private editor `/rest` endpoints. Credential preflight can run as `--credential-preflight=fail|warn|skip`; mutating commands default to `fail`, while diagnostic commands default to `warn`.

## API Key Scopes

If you are using enterprise-scoped n8n API keys, give `n8nctl` the minimum documented scopes it needs:

- `workflow:list`
- `workflow:read`
- `workflow:create`
- `workflow:update`
- `workflow:delete`
- `workflow:publish`
- `workflow:unpublish`
- `workflow:move`
- `credential:list`
- `credential:read`
- `project:list`
- `project:read`

These scopes cover the current command set:

- `workflow list|get|diff|issues|doctor|dependencies` and name-based workflow lookup
- `workflow create|clone|deploy` create and update operations
- `workflow cleanup` delete operations scoped by project and id or prefix
- `workflow move` and project-aware deploy transfer operations
- `workflow activate|deactivate`
- credential preflight and `workflow rebind-credential`
- project lookup through `--project` and `default_project`
- `execution list|get|wait|failures|retry` when resolving a workflow by name

Notes:

- Non-enterprise API keys are full-access in n8n and can't be narrowed by scope.
- `n8nctl` reads credential metadata to verify workflow references, but it doesn't read credential secret values.
- The current n8n permission reference doesn't document separate execution API scopes. Start with the scopes above and widen only if your instance enforces execution-specific permissions.
- `workflow run` uses only public `/api/v1` behavior. If your n8n instance doesn't expose a stable public run endpoint, the command returns `unsupported_endpoint` and doesn't call internal editor APIs.
- The user behind the API key still needs access to the target project and workflows.

References:

- [n8n public API](https://docs.n8n.io/api/)
- [n8n API authentication](https://docs.n8n.io/api/authentication/)
- [n8n custom project roles and scopes](https://docs.n8n.io/user-management/rbac/custom-roles/)

## 1Password CLI

For a new project, let `init` create the project-local env and shell helper:

```bash
n8nctl init --with-1password --onepassword-vault Employee
```

This creates:

- `.env.1password`, a Git-ignored map from `N8N_*_API_KEY` variables to 1Password `op://...` references.
- `.n8nctl/load-1password-env.sh`, a Git-ignored sourceable shell helper that resolves those references into the current shell.

If `.n8nctl.yaml` already exists, `n8nctl init --with-1password` leaves it alone and only creates the missing local auth files. Use `--force` only when you explicitly want to overwrite existing local files.

Use `.env.1password.example` as the committed template for 1Password CLI secret references:

```bash
N8N_PROD_API_KEY="op://Engineering/n8nctl-api-key-prod/credential"
```

Authentication model:

- `n8nctl` never calls 1Password directly.
- `n8nctl` only reads the real API key from the environment variable configured in `.n8nctl.yaml`.
- `.env.1password` may contain `op://...` references, but those references must be resolved before `n8nctl` starts.
- Do not set `N8N_PROD_API_KEY` to an `op://...` value directly; that is a pointer, not the API key.

For day-to-day development, resolve the secret once into your current shell:

```bash
source .n8nctl/load-1password-env.sh
n8nctl workflow list --env prod
n8nctl workflow deploy workflows/slack-alert.json --env prod --yes
```

This calls `op read` once per secret in `.env.1password` and exports the resolved values into the current terminal session. It does not write API keys to disk.
After sourcing, run `n8nctl` directly in that same shell. If you open a new terminal, source the script again.

For one-off commands, `op run` still works:

```bash
op run --env-file=.env.1password -- n8nctl workflow list --env prod
```

`op run` resolves secrets for that one child process only. If you wrap every `n8nctl` command in `op run`, 1Password may ask for access repeatedly.

Safe checks that do not print the secret:

```bash
test -n "$N8N_PROD_API_KEY" && echo N8N_PROD_API_KEY is loaded
case "$N8N_PROD_API_KEY" in op://*) echo unresolved 1Password reference ;; *) echo resolved value present ;; esac
```

If 1Password prompts or fails repeatedly, restart the 1Password desktop app and confirm CLI integration is enabled in 1Password settings.

For AI agents and automation:

- Start the agent from a shell where `source .n8nctl/load-1password-env.sh` has already run, or explicitly pass a resolved `N8N_PROD_API_KEY` into the process environment.
- The agent should not pass `op://...` as the API key value.
- The agent should not use `op run` for every command in a loop; resolve once into the shell/session to avoid repeated 1Password authorization prompts.

Clear the loaded key from your shell with:

```bash
unset N8N_PROD_API_KEY
```

If you create a separate development API key later, add an `N8N_DEV_API_KEY` reference to your local `.env.1password`.

## Command Surface

```bash
n8nctl init
n8nctl init --with-1password --onepassword-vault Employee
n8nctl version
n8nctl env list
n8nctl env doctor --all
n8nctl env load --loader 1password --format sh
n8nctl project list --env dev
n8nctl workflow list --env dev
n8nctl workflow list --env dev --project "HR & Workplace Experience"
n8nctl workflow get slack-alert --env dev
n8nctl workflow validate workflows/slack-alert.json
n8nctl workflow issues --id wf_123 --env dev --project "HR & Workplace Experience"
n8nctl workflow diff workflows/slack-alert.json --env dev --project "HR & Workplace Experience"
n8nctl workflow create workflows/slack-alert.json --env dev --project "HR & Workplace Experience"
n8nctl workflow deploy workflows/slack-alert.json --env dev
n8nctl workflow move slack-alert --env dev --project "HR & Workplace Experience"
n8nctl workflow clone slack-alert --env dev --project "HR & Workplace Experience" --name "slack-alert-copy"
n8nctl workflow run slack-alert --env dev --wait
n8nctl workflow dependencies workflows/slack-alert.json --local
n8nctl workflow dependencies --id wf_123 --remote --env dev
n8nctl workflow drift workflows/slack-alert.json --env dev --project "HR & Workplace Experience"
n8nctl workflow cleanup --env dev --project "HR & Workplace Experience" --prefix "tmp-"
n8nctl workflow doctor --id wf_123 --env dev --project "HR & Workplace Experience"
n8nctl workflow rebind-credential --id wf_123 --node Slack --credential "Slack API" --env dev
n8nctl workflow rebind-credential --id wf_123 --all-google-drive svc-talent-mapping --env dev
n8nctl workflow activate slack-alert --env dev
n8nctl workflow deactivate slack-alert --env dev
n8nctl execution list --workflow slack-alert --env dev
n8nctl execution get 123 --env dev --include-data
n8nctl execution wait 123 --env dev
n8nctl execution failures 123 --env dev
n8nctl execution diagnose 123 --env dev --json
n8nctl execution retry 123 --env dev --load-workflow --wait
n8nctl execution rerun-item 123 --node "HTTP Request" --item 0 --env dev
```

Every command accepts:

```bash
--json
--no-color
--yes
--dry-run
--ci
```

## Execution Troubleshooting

When a workflow fails, start with the diagnosis command:

```bash
n8nctl execution diagnose <execution-id> --env prod
```

It fetches execution data with `includeData=true` and prints:

- execution status, mode, workflow id, timestamps, and duration
- `lastNodeExecuted` and any top-level execution error
- failed node runs with node name, run index, item index, error name/message, and compact hints
- node-run log rows with status, start time, duration in ms, item count, and error message

For agent-friendly output:

```bash
n8nctl execution diagnose <execution-id> --env prod --json
```

Use this order for failed workflows:

```bash
n8nctl execution diagnose <id> --env prod --json
n8nctl execution failures <id> --env prod --json
n8nctl workflow doctor --id <workflow-id> --env prod --project "<project>"
```

Common hints are generated for credential/auth failures, missing resources, timeouts, rate limits, expression/input-shape errors, and Google OAuth scope issues. `execution get --include-data` is still available when you need the raw n8n execution payload.

`workflow run --wait`, `execution wait`, and `execution retry --wait` support `--diagnose-on-failure=auto|always|never`. The default `auto` adds diagnosis output when the final execution status is unsuccessful.

## Agent-Friendly JSON

Example:

```bash
n8nctl workflow deploy workflows/slack-alert.json --env dev --json --yes
```

Successful responses include a top-level `status`. Errors render as:

```json
{
  "status": "error",
  "code": "validation_failed",
  "message": "workflow validation failed",
  "details": {}
}
```

## Install And Update

For release-based installs:

```bash
./scripts/install.sh
./scripts/install.sh --setup-path
./scripts/install.sh v0.1.0
```

The installer prefers GitHub Releases via `gh`. If `gh` is unavailable, it falls back to `go install`.
When you run the installer from a local `n8nctl` checkout before the first release exists, it installs from that checkout instead of failing.
It installs to a writable directory already on `PATH` when possible, otherwise it falls back to `~/.local/bin`.
If the chosen directory is not on `PATH`, the script prints the exact export command to use. With `--setup-path`, it appends that export to `~/.zshrc` or `~/.bashrc`.

You can override the GitHub repository, Go module path, and install target:

```bash
N8NCTL_REPO=LogicMonitor-IT/n8nctl \
N8NCTL_MODULE=github.com/LogicMonitor-IT/n8nctl \
N8NCTL_INSTALL_DIR="$HOME/bin" \
./scripts/install.sh
```

After install, verify the binary:

```bash
n8nctl version
```

## Development

Run tests and compile checks:

```bash
go test ./...
go build ./...
```

Optionally install the pinned runtime-validator package:

```bash
cd tools/n8n-validator
npm install
```

## Release Process

- Pull requests and pushes to `main` run `.github/workflows/ci.yml`
- Tags matching `v*` run `.github/workflows/release.yml`
- Release archives are named `n8nctl_<version>_<os>_<arch>.tar.gz`, matching `scripts/install.sh`

Create a release with:

```bash
git tag v0.1.0
git push origin v0.1.0
```

If you later rename or fork this repo, update the module path in `go.mod` and the default `N8NCTL_REPO` / `N8NCTL_MODULE` values in `scripts/install.sh`.

## Safety Behavior

- production deploys fail closed unless `--yes` is provided
- production mutation failures print the target environment, target project, workflow, planned action, and exact `--yes` rerun guidance
- workflow updates can back up the current remote workflow to `.n8nctl/backups/<env>/`
- deploy backups include optional `--reason` labels and the current git SHA when available
- update, move, rebind, and cleanup commands can use `--backup-file` or `--backup-dir` for predictable backup placement
- deploy creates new workflows inactive by default
- if an existing remote workflow is active and deploy is not passed `--activate`, `n8nctl` deactivates the remote workflow before updating it
- `--dry-run` resolves the remote workflow, project, credentials, and planned actions before writing, and does not require `--yes`
- deploy/create/move verify final project placement after mutation and report `project_location_verified`, `project_location_unverified`, or `project_location_mismatch`
