# Use case: `n8nctl` — n8n Cloud Workflow Dev CLI

## Problem

Codex or another AI agent generates n8n workflow JSON, but engineers must manually upload it into n8n Cloud, test it, adjust it, and repeat. This slows local development, troubleshooting, and workflow promotion.

n8n Cloud supports the public REST API, and API calls authenticate with the `X-N8N-API-KEY` header. The API is not available during free trials. ([n8n Docs][1])

## Goal

Create a modular Go CLI that lets humans and AI agents manage n8n Cloud workflows from the terminal.

```bash
n8nctl workflow validate workflows/slack-alert.json
n8nctl workflow deploy workflows/slack-alert.json --env dev
n8nctl workflow list --env dev
n8nctl workflow diff workflows/slack-alert.json --env dev
n8nctl workflow activate slack-alert --env dev
```

## Primary user story

As a developer, I want to generate an n8n workflow JSON with Codex, validate it, deploy it to n8n Cloud dev, test it, and promote it without manually uploading JSON through the n8n UI.

## AI-agent user story

As an AI coding agent, I need deterministic commands, machine-readable output, and clear exit codes so I can validate and deploy workflows without understanding the n8n UI.

---

# CLI name

Recommended:

```bash
n8nctl
```

Alternative internal name:

```bash
lm-n8n
```

I would use `n8nctl` unless there is a naming concern.

---

# Command design

## Human-friendly commands

```bash
n8nctl init
n8nctl env list
n8nctl workflow list --env dev
n8nctl workflow get slack-alert --env dev
n8nctl workflow validate workflows/slack-alert.json
n8nctl workflow deploy workflows/slack-alert.json --env dev
n8nctl workflow diff workflows/slack-alert.json --env dev
n8nctl workflow activate slack-alert --env dev
n8nctl workflow deactivate slack-alert --env dev
n8nctl execution list --workflow slack-alert --env dev
```

## AI-friendly commands

Every command should support:

```bash
--json
--no-color
--yes
--dry-run
```

Example:

```bash
n8nctl workflow deploy workflows/slack-alert.json \
  --env dev \
  --json \
  --yes
```

Example machine-readable output:

```json
{
  "status": "updated",
  "workflowName": "slack-alert",
  "workflowId": "abc123",
  "environment": "dev",
  "active": false
}
```

---

# Core workflow

```text
Codex generates workflow JSON
        ↓
n8nctl workflow validate
        ↓
n8nctl workflow diff --env dev
        ↓
n8nctl workflow deploy --env dev
        ↓
n8nctl workflow activate --env dev
        ↓
n8nctl execution list --workflow name
```

---

# Config file

Use a repo-local config file:

```yaml
# .n8nctl.yaml
default_env: dev

environments:
  dev:
    base_url: https://company-dev.app.n8n.cloud
    api_key_env: N8N_DEV_API_KEY

  prod:
    base_url: https://company.app.n8n.cloud
    api_key_env: N8N_PROD_API_KEY

workflows:
  path: workflows
  name_strategy: file_or_json_name

safety:
  require_confirm_for_prod: true
  backup_before_update: true
  deploy_inactive_by_default: true
```

No API keys should be stored in the config file.

Use environment variables:

```bash
export N8N_DEV_API_KEY="..."
export N8N_PROD_API_KEY="..."
```

n8n API keys are created in **Settings → n8n API**, and enterprise instances can scope keys. Non-enterprise keys have broad access, so treat them as sensitive. ([n8n Docs][2])

---

# Recommended MVP

Build these first:

```bash
n8nctl init
n8nctl workflow validate <file>
n8nctl workflow list --env dev
n8nctl workflow deploy <file> --env dev
n8nctl workflow activate <name-or-id> --env dev
```

That is enough to remove the manual upload bottleneck.

---

# Go project structure

```text
n8nctl/
  cmd/
    root.go
    init.go
    workflow.go
    execution.go
    env.go

  internal/
    api/
      client.go
      workflows.go
      executions.go

    config/
      config.go

    workflow/
      validate.go
      normalize.go
      diff.go

    output/
      table.go
      json.go

    auth/
      env.go

    errors/
      errors.go

  pkg/
    n8n/
      types.go

  testdata/
    workflows/
      valid.json
      invalid.json

  go.mod
  README.md
```

Use:

```text
cobra       → CLI framework
viper       → config loading
go-cmp      → diffing normalized workflows
jsonschema  → optional schema validation
```

---

# API client behavior

The n8n public API is OpenAPI-documented and includes resources such as workflows, executions, credentials, tags, variables, projects, and data tables. ([GitHub][3])

The Go API client should centralize:

```go
type Client struct {
    BaseURL string
    APIKey  string
    HTTP    *http.Client
}
```

Every request should add:

```http
X-N8N-API-KEY: <key>
Accept: application/json
Content-Type: application/json
```

---

# Deploy behavior

`deploy` should be create-or-update:

```bash
n8nctl workflow deploy workflows/slack-alert.json --env dev
```

Logic:

```text
1. Read local JSON
2. Validate basic n8n workflow shape
3. Extract workflow name
4. Search cloud workflows by name
5. If no match:
     create workflow
6. If one match:
     update workflow
7. If multiple matches:
     fail unless --id is provided
8. Keep inactive by default unless --activate is passed
9. Print result
```

Safe default:

```bash
n8nctl workflow deploy workflows/foo.json --env prod
```

Should require confirmation unless:

```bash
--yes
```

---

# Validation rules

Start simple. Validate that workflow JSON has:

```text
name
nodes[]
connections
settings
```

Then add deeper checks:

```text
No hardcoded secrets
No production webhook URLs in dev workflows
No missing credential references
No duplicate node names
No unknown environment placeholders
No active=true unless explicitly allowed
```

Example:

```bash
n8nctl workflow validate workflows/foo.json
```

Output:

```text
OK workflows/foo.json
name: Slack Alert
nodes: 8
connections: 7
credentials: 2 references
```

---

# Example README section

````md
## Quick start

Create config:

```bash
n8nctl init
````

Set API key:

```bash
export N8N_DEV_API_KEY="your-api-key"
```

Validate a workflow:

```bash
n8nctl workflow validate workflows/slack-alert.json
```

Deploy to dev:

```bash
n8nctl workflow deploy workflows/slack-alert.json --env dev
```

Activate:

```bash
n8nctl workflow activate slack-alert --env dev
```

````

---

# Why Go is a good fit

Go works well here because the final tool can be shipped as a single binary:

```bash
n8nctl-darwin-arm64
n8nctl-linux-amd64
n8nctl-windows-amd64.exe
````

No Python virtualenv, no Node dependency chain, easier CI usage, and easier installation for both humans and agents.

---

# Final recommendation

Build `n8nctl` as a **thin, safe, API-backed deployment CLI** for n8n Cloud.

Do **not** try to reproduce all n8n UI functionality. The first valuable version should only do:

```text
validate
list
diff
deploy
activate/deactivate
execution status
```

That gives you the development speedup without creating an overbuilt platform.

[1]: https://docs.n8n.io/api/ "n8n public REST API Documentation and Guides | n8n Docs "
[2]: https://docs.n8n.io/api/authentication/ "Authentication | n8n Docs  "
[3]: https://github.com/n8n-io/n8n-docs/blob/main/docs/api/v1/openapi.yml "n8n-docs/docs/api/v1/openapi.yml at main · n8n-io/n8n-docs · GitHub"
