# Changelog

## Unreleased

### Added

- Added example `.n8nctl.example.yaml` and `.env.1password.example` files for team rollout.
- Added GitHub Actions CI and tagged release workflows.
- Added fixture-backed API tests for workflow update payload stripping, activation, and transfer failure handling.

### Improved

- Runtime validation now uses embedded node metadata instead of hard-coded node checks and reports optional `n8n` package version mismatches when `tools/n8n-validator` dependencies are installed.

## v0.3.4

### Fixed

- Workflow deploy now strips n8n read-only workflow fields from create/update payloads, including activation state, IDs, timestamps, version metadata, sharing/tag data, pin data, static data, and other server-managed fields.
- JSON API failures now include the n8n response message/body plus request method and URL details.

## v0.3.3

### Fixed

- Runtime workflow validation now accepts Google Service Account API credentials (`googleApi`) for Google Drive and Google Sheets nodes.

## v0.3.2

### Added

- Added credential preflight modes with `--credential-preflight=fail|warn|skip`.
- Added `env doctor`, `env load`, local workflow dependency inspection, workflow drift detection, and scoped workflow cleanup.
- Added `--ci` stable exit codes for validation, credential preflight, drift, and execution failures.
- Added `--diagnose-on-failure` for waited workflow runs, execution waits, and execution retries.
- Added explicit backup destinations with `--backup-file` and `--backup-dir`.

### Improved

- Credential preflight now reports per-reference classifications, accepted types, actual types, and project-sharing status.
- Deploy/create/move operations now verify final project placement after mutation.

## v0.3.0

### Added

- Added `execution diagnose` for compact troubleshooting of failed workflow runs.
- Added n8n-aware execution failure parsing from `resultData.runData`.
- Added structured JSON diagnosis output for agents.
- Added human and agent guidance when API keys are missing or still set to unresolved `op://` references.

### Improved

- `execution failures` now reports failed node runs, run indexes, item indexes, error names/messages, and troubleshooting hints.
- Documentation now includes a recommended workflow for diagnosing failed executions.

## v0.2.0

### Added

- Added editor-style workflow validation checks through the runtime validator helper.
- Added `workflow issues`, `workflow doctor`, `workflow dependencies`, and credential preflight.
- Added project-aware workflow commands: `create`, `move`, `clone`, and project-aware deploy behavior.
- Added execution commands: `get`, `wait`, `failures`, `retry`, and `rerun-item` unsupported-endpoint guidance.
- Added project and credential API clients plus typed models.

### Improved

- Improved deploy safety output with target environment, project, workflow, planned actions, and credential preflight status.
- Improved workflow diff output with grouped node, credential, and connection changes.
- Added backup labels using deploy reason and git metadata.
- Updated installer behavior and API-scope documentation.

## v0.1.0

### Added

- Initial `n8nctl` Go CLI.
- Added workflow list, get, validate, diff, deploy, activate, and deactivate commands.
- Added execution listing.
- Added repo-local `.n8nctl.yaml` config and environment-based API-key resolution.
- Added JSON output, production safety gates, local backups, tests, release automation, and installer scripts.
