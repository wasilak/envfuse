# Phase 5: Operational Safety & Observability - Context

**Gathered:** 2026-05-22
**Status:** Ready for planning

<domain>
## Phase Boundary

Finalize secret redaction guarantees (SECU-01) and introduce operator-facing runtime signals (OBSV-01): structured log events covering sync outcomes, cycle latency, fingerprint changes, restart reasons, and shutdown. The binary must be safe to operate in production with zero risk of secret values appearing in any log output.

</domain>

<decisions>
## Implementation Decisions

### Observability Surface
- **D-01:** Emit runtime signals to **stderr only** — no HTTP/metrics endpoint, no separate events file. Container runtimes (ECS, Kubernetes) collect stderr and forward to log aggregators.
- **D-02:** envfuse and child process **share stderr** (no separate stream). envfuse log lines are distinguished by structured JSON format and the `envfuse` source prefix.

### Log Format
- **D-03:** Use **stdlib `slog` with `JSONHandler`** — structured JSON to stderr. No new dependency (Go 1.21+). JSON plays well with CloudWatch, Datadog, and Loki.
- **D-04:** **`ENVFUSE_LOG_LEVEL` env var** controls verbosity. Default is `INFO`. Supports the full slog level set: `DEBUG`, `INFO`, `WARN`, `ERROR`.
- **D-05:** `slog.SetDefault(logger)` is called once in `main.go` at startup based on `ENVFUSE_LOG_LEVEL`. All packages call `slog.Info(...)` / `slog.Debug(...)` directly — **no logger injection or passing required**.

### Redaction Guarantee (SECU-01)
- **D-06:** **Passive approach** — log calls receive only `ErrorClass` codes, path names, counts, and durations. Secret values are never passed to any log call. This is already mostly true in the codebase; Phase 5 enforces it explicitly.
- **D-07:** Test coverage for redaction spans **all three high-risk paths**: provider fetch errors (Vault/AWS), template rendering errors, and config validation errors. Each test asserts that known secret values do not appear anywhere in log output when those error paths are triggered.

### Events at INFO Level
All four event categories are emitted at INFO:
- **D-08:** **Startup summary** — emitted after first successful sync cycle: `provider_type`, `paths_fetched` count, `env_vars_applied` count, `files_rendered` count, `fingerprint` (first 8 chars).
- **D-09:** **Cycle outcome** — emitted on every poll cycle: `status` (success/failed/aborted), `latency_ms`, `error_class` if failed/aborted.
- **D-10:** **Fingerprint change + restart** — emitted when effective config changes: `fingerprint_old` (8 chars), `fingerprint_new` (8 chars), `restart_reason` (typed Reason from supervisor model).
- **D-11:** **Shutdown + exit** — emitted on SIGTERM/SIGINT or child self-exit: `exit_reason`, `exit_code`.

### Events at DEBUG Level
All four debug event categories are emitted at DEBUG:
- **D-12:** **Per-path fetch results** — individual path latency and success/error within a concurrent fetch batch.
- **D-13:** **Env var names applied** — list of env var names (e.g., `["DATABASE_URL","API_KEY"]`) applied in a cycle. **Never values.**
- **D-14:** **Template render details** — which template specs rendered and to which output paths.
- **D-15:** **Cooldown state** — when a restart is deferred: `pending` flag set/cleared, cooldown window duration.

### Claude's Discretion
- Field naming and exact slog attribute keys (e.g., `latency_ms` vs `duration_ms`) — follow slog conventions and be consistent.
- Whether to emit a startup INFO line before the first sync cycle completes (e.g., "envfuse starting, provider=vault") — reasonable to add.
- Truncation length for fingerprints in log output (8 chars suggested above, adjust if needed for uniqueness).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements
- `.planning/REQUIREMENTS.md` — SECU-01 and OBSV-01 are the two requirements this phase delivers. Read the exact acceptance criteria.
- `.planning/ROADMAP.md` §Phase 5 — Success criteria: (1) operator cannot find secret values in logs, (2) operator can observe sync success/failure, latency, and restart reason.

### Existing Code
- `cmd/envfuse/main.go` — entry point where `slog.SetDefault(logger)` will be initialized. Current output via `fmt.Println`/`fmt.Fprintln` will be replaced with structured log calls.
- `internal/sync/coordinator.go` — `runCycleWithVectors` returns `CycleResult` with `Status`, `ErrorClass`, `FetchedPaths`, `AppliedEnv`, `RenderedFiles`, `Fingerprint`. These fields feed INFO and DEBUG events.
- `internal/sync/model.go` — `CycleResult` and `CycleStatus` types. No changes expected; log calls consume these.
- `internal/supervisor/supervisor.go` — `watch()` loop where cycle outcomes are acted on. Restart and shutdown events originate here.
- `internal/supervisor/model.go` — `Reason` type (`config_changed`, `child_exited`, `startup_failed`, `forced_kill`). Use these typed values in `restart_reason` log field.

### Standard Library
- `log/slog` (Go 1.21+) — stdlib structured logging. Use `slog.SetDefault`, `slog.JSONHandler`, `slog.NewLevelVar` for dynamic level control via `ENVFUSE_LOG_LEVEL`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `CycleResult.ErrorClass` — already a code string, not a raw error message. Safe to log directly at INFO on failure.
- `supervisor.Reason` — typed restart/exit reason taxonomy. Maps directly to `restart_reason` and `exit_reason` log fields.
- `CycleResult.Fingerprint` — SHA-256 hex string, safe to truncate to 8 chars for log output.

### Established Patterns
- **Passive redaction** is already the norm: error paths use `ErrorClass` codes and path names, not secret values. Phase 5 formalizes and tests this pattern.
- **Config-load validation** happens in `config.LoadConfig` — errors reference field names (e.g., `vault_address`) but not vault tokens or secret values. Keep this pattern intact.
- **`fmt.Fprintln(os.Stderr, err)`** in `main.go` for startup errors — replace with `slog.Error(...)` calls after logger is initialized. Note: logger init must happen before config parse to catch config errors, or config errors use a pre-init stderr write.

### Integration Points
- `main.go`: initialize `slog.SetDefault` before first sync cycle; replace all `fmt.Print*` calls with `slog.*` calls.
- `supervisor/supervisor.go`: add `slog.Info` calls for cycle outcome, fingerprint change, and shutdown events. The `watch()` function already has all the information needed.
- No changes to provider, inject, render, or fileio internals — they return errors upward; log calls stay at the top level.

</code_context>

<specifics>
## Specific Ideas

- Global slog default pattern: `slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: levelVar})))` where `levelVar` is parsed from `ENVFUSE_LOG_LEVEL`.
- Fingerprint truncation: `fingerprint[:8]` is sufficient for log readability. Full fingerprint stays in `CycleResult` for internal logic.
- The INFO startup summary should fire after `RunSingleCycleWithStoreFromConfig` returns success — at that point `result.FetchedPaths`, `result.AppliedEnv`, `result.RenderedFiles`, and `result.Fingerprint` are all populated.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 5-Operational Safety & Observability*
*Context gathered: 2026-05-22*
