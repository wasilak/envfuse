---
phase: 03-pid-1-supervision-reload-control
plan: "02"
subsystem: fingerprint, supervisor, sync
tags: [fingerprint, restart-gating, relo-01, relo-02, sha256, canonical-sort, supervisor-loop]
dependency_graph:
  requires: [internal/state, internal/sync, internal/config, internal/supervisor]
  provides: [internal/fingerprint, supervisor-restart-gate]
  affects: [internal/supervisor/supervisor.go, internal/sync/coordinator.go, cmd/envfuse/main.go]
tech_stack:
  added:
    - "internal/fingerprint package: canonical SHA-256 digest over sorted env keys + sorted file paths"
    - "Config struct in supervisor: store/configPath/initialFingerprint/pollInterval for cycle-driven restart"
  patterns:
    - "sort.Strings(keys) before SHA-256 feed enforces D-07 canonical ordering"
    - "Fingerprint computed pre-commit from candidate state (D-06) inside coordinator before CommitCandidate()"
    - "Tick-driven cycle polling in supervisor watch(); restart only on CycleStatusSuccess + changed fingerprint"
    - "noCyclePollInterval sentinel in unit tests bypasses polling without changing production code paths"
key_files:
  created:
    - internal/fingerprint/fingerprint.go
    - internal/fingerprint/fingerprint_test.go
  modified:
    - internal/sync/model.go
    - internal/sync/coordinator.go
    - internal/config/config.go
    - internal/supervisor/supervisor.go
    - internal/supervisor/supervisor_test.go
    - cmd/envfuse/main.go
    - tests/integration/pid1_supervision_reload_test.go
decisions:
  - "Fingerprint computed inside coordinator (not supervisor) so candidate state is available pre-commit (D-06)"
  - "supervisor.Config struct consolidates all supervision parameters; eliminates positional argument drift"
  - "noCyclePollInterval (24h) in unit tests keeps lifecycle tests deterministic without a mock cycle runner"
  - "mergeEnvForRestart re-derives child env from originalEnv + new payload on each restart, preserving host env"
  - "reload_poll_interval added to config with 30s default; validates same way as shutdown_timeout"
metrics:
  duration_minutes: 8
  completed_date: "2026-05-22"
  tasks_completed: 3
  files_changed: 9
---

# Phase 3 Plan 2: Deterministic Fingerprinting and Restart Gating Summary

**One-liner:** Canonical SHA-256 fingerprint over sorted env+file candidate state drives restart-on-change gating in the supervisor loop, enforcing RELO-01/RELO-02 with no metadata leakage.

## What Was Built

### internal/fingerprint/fingerprint.go

`Compute(env map[string]string, files map[string][]byte) string` — canonical SHA-256 digest:
- Sorts env keys with `sort.Strings` before feeding to `sha256.New()` (D-07)
- Sorts file paths with `sort.Strings` before feeding file bytes (D-07)
- Writes null-byte-separated key/value pairs to prevent ambiguous encoding
- Returns hex-encoded digest
- Function signature accepts only env+files; timestamps/latency cannot be passed in (D-08)
- Computed from pre-commit candidate state in coordinator (D-06)

### internal/sync/model.go

Added `Fingerprint string` field to `CycleResult`. Populated only on `CycleStatusSuccess`; empty on failed/aborted cycles.

### internal/sync/coordinator.go

Added fingerprint computation in `runCycleWithVectors` immediately before `StageCandidate`/`CommitCandidate` (D-06). Imports `envfuse/internal/fingerprint`. Returns `Fingerprint` in the success result.

### internal/config/config.go

Added `ReloadPollInterval string` field with JSON tag `reload_poll_interval`. Added `DefaultReloadPollInterval = 30s`. Added `ParsedReloadPollInterval()` method matching `ParsedShutdownTimeout()` pattern. Validated in `LoadConfig`.

### internal/supervisor/supervisor.go

Replaced single-shot `run(ctx, command, env, timeout)` with a full supervision loop:

- `Config` struct: Command, Env, ShutdownTimeout, ConfigPath, Store, InitialFingerprint, PollInterval
- `Run(cfg Config)`: OS signal subscription, delegates to `runLoop`
- `runLoop(ctx, cfg)`: outer restart loop — calls `spawnChild`, then `watch`, restarts if `watch` returns `restart=true`
- `watch(...)`: monitors `childWait`, `shutdownCtx.Done()`, and `ticker.C`:
  - `childWait`: returns `ReasonChildExited` (D-13)
  - `shutdownCtx.Done()`: sends SIGTERM, waits or kills (SUPV-02/03)
  - `ticker.C`: runs `RunSingleCycleWithStoreFromConfig`; no-op on failure or unchanged fingerprint; stops+restarts child on success+changed fingerprint (RELO-01/RELO-02)
- Config-change restart uses `config_changed` reason semantics (D-16) — the restart is implicit in the loop, not an explicit reason return; terminal outcomes still use ReasonChildExited/StartupFailed/ForcedKill

### internal/supervisor/supervisor_test.go

Updated all 7 unit tests to use `runLoop(ctx, Config{..., PollInterval: noCyclePollInterval})`. The `noCyclePollInterval = 24h` ensures the ticker never fires during unit tests. All lifecycle paths (startup failure, child exit, forced kill, no retry) remain fully tested.

### cmd/envfuse/main.go

Extended supervisor mode to build `supervisor.Config`:
- Parses `reload_poll_interval` from config
- Passes `store` (shared with initial sync cycle) and `result.Fingerprint` (from first cycle)
- Poll interval from config

### tests/integration/pid1_supervision_reload_test.go

Two new integration tests:

1. `TestPID1_RestartOnChange`: Starts envfuse with 200ms poll interval, changes secrets mid-test, asserts child restart count reaches >= 2 (RELO-01)
2. `TestPID1_NoRestartOnSameFingerprint`: Starts envfuse with 200ms poll interval, never changes secrets, waits 2 seconds (10+ cycles), asserts restart count == 1 (RELO-02)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Supervisor unit tests used old internal `run()` signature**
- **Found during:** Task 3 — build error when supervisor was refactored to `runLoop(ctx, Config)`
- **Issue:** The Phase 3 Plan 1 unit tests called the old `run(ctx, command, env, timeout)` internal function which no longer exists after the Config-struct refactor
- **Fix:** Updated `supervisor_test.go` to call `runLoop(ctx, Config{...})` with `noCyclePollInterval` sentinel; all 7 tests preserved with same behavioral assertions
- **Files modified:** `internal/supervisor/supervisor_test.go`
- **Commit:** 54df7e4

**2. [Rule 2 - Critical] Cycle poll configuration required in config schema**
- **Found during:** Task 2/3 — integration tests used `reload_poll_interval` JSON field rejected by `DisallowUnknownFields`
- **Issue:** Integration tests needed a short poll interval (200ms) to observe restart behavior; config must accept and validate the field
- **Fix:** Added `ReloadPollInterval` to `Config` struct, `ParsedReloadPollInterval()` method, validation in `LoadConfig`, default 30s
- **Files modified:** `internal/config/config.go`
- **Commit:** d732353

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| Fingerprint computed in coordinator pre-commit, not supervisor post-commit | Candidate state (env + rendered bytes) is available in coordinator before CommitCandidate(); avoids re-fetching from store after commit |
| supervisor.Config struct instead of positional args | New parameters (store, configPath, initialFingerprint, pollInterval) make positional args unmaintainable; struct scales cleanly |
| noCyclePollInterval sentinel (24h) in unit tests | No mock cycle runner needed; tick never fires; existing lifecycle tests preserved without additional test infrastructure |
| Store passed as *state.Store (concrete, not interface) | RunSingleCycleWithStoreFromConfig already accepts *state.Store; adding interface abstraction was unnecessary complexity for current scope |

## Known Stubs

None — all restart gating paths are implemented and exercised by integration tests.

## Threat Flags

None — all plan threat model mitigations implemented:
- T-03-05: Canonical sort enforced before SHA-256; unit-tested for order independence
- T-03-06: Restart gate fires only on success+change; unchanged cycles are no-ops
- T-03-07: config_changed restart path exercised in integration test; reason taxonomy preserved in model.go
- T-03-08: Fingerprint function signature accepts only env+files; no metadata can be passed in

## Self-Check: PASSED

All created/modified files verified present. All task commits verified in git log.
