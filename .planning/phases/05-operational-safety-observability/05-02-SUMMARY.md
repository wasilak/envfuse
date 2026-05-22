# Phase 5 Plan 02: Supervisor observability events + SECU-01 redaction tests

## Status: Complete

## What was built

Wired four remaining observability events (D-09 cycle outcome, D-10 fingerprint change/restart, D-11 shutdown/child-exit/startup-failed, D-15 cooldown state) into `internal/supervisor/supervisor.go` using structured slog calls. Added per-path fetch result DEBUG events (D-12) to `internal/sync/coordinator.go`. Wrote three table-driven redaction tests in `internal/sync/redaction_test.go` that capture slog JSON output and assert known fake secret values do not appear anywhere in the log buffer, locking the SECU-01 passive-redaction contract.

## Tasks completed

- Task 1: Added D-09/D-10/D-11/D-12/D-15 slog INFO/DEBUG/WARN events to supervisor.go and coordinator.go; added truncate8 helper for safe fingerprint prefix logging
- Task 2: Wrote TestRedaction_ProviderFetchError, TestRedaction_TemplateRenderError, TestRedaction_ConfigValidationError with captureLog helper and test provider stubs

## Commits

- a299ae5: feat(05-02): add supervisor INFO/DEBUG observability events (D-09/D-10/D-11/D-12/D-15)
- 37f4749: test(05-02): add SECU-01 redaction tests for provider fetch, template render, and config errors

## Acceptance criteria verified

1. `go build ./...` succeeds — PASS
2. `go test ./...` passes (71 tests across 16 packages) — PASS
3. supervisor.go emits structured JSON log lines for sync cycle, config changed restart, shutdown, child exited, cooldown deferred/drained events — PASS (code review verified)
4. TestRedaction_ProviderFetchError passes: s.fake-vault-token-abc123 absent from log buffer — PASS
5. TestRedaction_TemplateRenderError passes: s.fake-template-secret-xyz789 absent from log buffer — PASS
6. TestRedaction_ConfigValidationError passes: s.fake-config-token-def456 absent from log buffer — PASS
7. `rg 'fmt\.Fprintf\(os\.Stderr' internal/supervisor/supervisor.go` returns no matches — PASS
8. `rg 'fmt\.Print' cmd/envfuse/main.go` returns no matches — PASS

## Deviations from Plan

None — plan executed exactly as written, with one minor adaptation: the plan's pseudocode used `Source`/`TargetPath` for TemplateSpec but the actual struct uses `Content`/`OutputPath`; tests used the correct field names as found in the codebase.

## Self-Check: PASSED

- internal/supervisor/supervisor.go: present and modified
- internal/sync/coordinator.go: present and modified
- internal/sync/redaction_test.go: present and created
- commits a299ae5 and 37f4749: confirmed in git log
