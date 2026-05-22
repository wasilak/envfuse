---
phase: 03-pid-1-supervision-reload-control
plan: "01"
subsystem: supervisor
tags: [supervision, pid1, signal-forwarding, shutdown-timeout, reason-taxonomy]
dependency_graph:
  requires: [internal/config, internal/state, internal/sync, internal/exec]
  provides: [internal/supervisor]
  affects: [cmd/envfuse/main.go]
tech_stack:
  added:
    - "internal/supervisor package: child lifecycle state machine (os/exec Start/Wait, os/signal NotifyContext)"
  patterns:
    - "signal.NotifyContext for SIGTERM/SIGINT handling delegated to run(ctx) for testability"
    - "Typed Reason constants mirroring CycleStatus pattern from internal/sync/model.go"
    - "Internal run(ctx) function decoupled from OS signal subscription for unit test injection"
key_files:
  created:
    - internal/supervisor/model.go
    - internal/supervisor/supervisor.go
    - internal/supervisor/supervisor_test.go
    - tests/integration/pid1_supervision_reload_test.go
  modified:
    - internal/config/config.go
    - cmd/envfuse/main.go
decisions:
  - "Added shutdown_timeout as string field in Config with ParsedShutdownTimeout() method (D-01/D-02)"
  - "Decoupled OS signal subscription into Run() wrapper; internal run(ctx) accepts context for unit-testable timeout path"
  - "Tests build envfuse binary once via sync.Once to avoid go run process-tree signal delivery issues"
  - "Updated TestPID1_SignalForwarding to use wait-in-loop shell pattern for POSIX-compliant trap handling"
metrics:
  duration_minutes: 8
  completed_date: "2026-05-22"
  tasks_completed: 3
  files_changed: 6
---

# Phase 3 Plan 1: PID1 Supervision Lifecycle Summary

**One-liner:** Child process supervision with SIGTERM forwarding and bounded force-kill timeout via signal.NotifyContext and child-only SIGKILL escalation.

## What Was Built

Implemented the foundational PID 1 supervisor loop for envfuse, delivering SUPV-01/02/03 requirements with all D-01..D-04 and D-13..D-16 locked decisions enforced.

### internal/supervisor/model.go
Typed `Reason` constants matching D-16 taxonomy:
- `config_changed`, `child_exited`, `startup_failed`, `forced_kill`
- `Result` struct: Reason, ExitCode, Killed fields

### internal/supervisor/supervisor.go
- `Run()`: subscribes to OS SIGTERM/SIGINT via `signal.NotifyContext`, delegates to `run(ctx)`
- `run(ctx)`: child lifecycle state machine with Start/Wait, signal forwarding, timeout escalation
- On host signal: forwards SIGTERM to child process only (D-03), then waits up to `shutdownTimeout`
- On timeout: sends SIGKILL to child process only (D-03), returns `ReasonForcedKill` (D-04)
- On uncontrolled child exit: returns `ReasonChildExited` with child exit code (D-13)
- On failed start: returns `ReasonStartupFailed` with exit code 1 (D-14)
- No crash auto-retry (D-15)

### internal/config/config.go
- Added `ShutdownTimeout string` field with JSON tag `shutdown_timeout`
- Added `DefaultShutdownTimeout = 10s` constant (D-01)
- Added `ParsedShutdownTimeout()` method: returns default if empty, validates positive duration (D-02)
- Integrated validation into `LoadConfig`

### cmd/envfuse/main.go
- Removed phase-01 `-once` guard error message
- Added supervisor mode as default: loads config, calls `supervisor.Run()` with parsed timeout
- Preserved `-once` flag for legacy direct execution without supervision loop
- Reads `ReasonStartupFailed`, `ReasonChildExited`, `ReasonForcedKill` → maps to OS exit codes

### tests/integration/pid1_supervision_reload_test.go
Three integration tests proving SUPV-01/02/03:
1. `TestPID1_StartAndWait`: supervised child starts and env payload is delivered (SUPV-01)
2. `TestPID1_SignalForwarding`: SIGTERM forwarded to child, trap handler fires (SUPV-02)
3. `TestPID1_GraceTimeoutForceKill`: 1s timeout + SIGTERM-ignoring child → force-kill (SUPV-03)

### internal/supervisor/supervisor_test.go
Seven deterministic unit tests:
- `TestSupervisor_StartupFailed`: nonexistent binary → startup_failed (D-14)
- `TestSupervisor_StartupFailedEmptyCommand`: nil command → startup_failed (D-14)
- `TestSupervisor_ChildExitedCode`: exit 3 → child_exited with code 3 (D-13)
- `TestSupervisor_ChildExitedZeroCode`: exit 0 → child_exited with code 0 (D-13)
- `TestSupervisor_ForcedKillReason`: context cancel + 300ms timeout + SIGTERM-ignoring child → forced_kill, Killed=true (D-03/D-04/D-16)
- `TestSupervisor_NoAutoRetry`: failing child exits immediately, no retry (D-15)
- `TestSupervisor_ReasonTaxonomyExactStrings`: validates exact D-16 string constants

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Signal forwarding test required binary compilation**
- **Found during:** Task 2 GREEN verification
- **Issue:** `go run ./cmd/envfuse` creates a subprocess wrapper; SIGTERM sent to the `go run` process does not reach the actual envfuse binary, causing `TestPID1_SignalForwarding` and `TestPID1_GraceTimeoutForceKill` to hang
- **Fix:** Added `buildEnvfuseBinary()` helper using `sync.Once` to compile envfuse binary once per test run; all PID1 tests use the compiled binary directly
- **Files modified:** `tests/integration/pid1_supervision_reload_test.go`
- **Commit:** 172904c

**2. [Rule 1 - Bug] POSIX sh trap does not fire while foreground process running**
- **Found during:** Task 2 GREEN verification
- **Issue:** `sh -c 'trap handler TERM; sleep 30'` does not process SIGTERM while waiting for `sleep` in non-interactive shell; trap fires only after `sleep` exits or in background+wait mode
- **Fix:** Changed signal forwarding test script to `while true; do sleep 1 & wait; done` pattern, which returns control to sh periodically allowing trap delivery
- **Files modified:** `tests/integration/pid1_supervision_reload_test.go`
- **Commit:** 172904c

**3. [Rule 2 - Critical] run(ctx) internal function extracted for unit testability**
- **Found during:** Task 3 implementation
- **Issue:** Original `Run()` used OS signal context internally; unit tests cannot inject shutdown signal without killing the test process itself; `TestSupervisor_ForcedKillReason` blocked forever
- **Fix:** Extracted internal `run(shutdownCtx context.Context, ...)` function; `Run()` remains the OS-signal entry point; tests call `run(ctx)` with a cancellable context
- **Files modified:** `internal/supervisor/supervisor.go`, `internal/supervisor/supervisor_test.go`
- **Commit:** e511869

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| Decouple OS signal subscription from core logic via `run(ctx)` | Enables deterministic unit tests without spawning subprocesses or sending signals to test binary |
| Build binary once in integration tests via `sync.Once` | Avoids repeated compilation overhead; fixes signal delivery chain broken by `go run` process nesting |
| Use `while true; do sleep 1 & wait; done` in signal forwarding test | POSIX-compliant approach for signal trap delivery while long-running |

## Known Stubs

None — all supervision paths are fully implemented and exercised.

## Threat Flags

None — all plan threat model mitigations implemented:
- T-03-01: shutdown_timeout + child-only SIGKILL enforced
- T-03-02: only SIGINT/SIGTERM via NotifyContext accepted
- T-03-03: typed reasons emitted and asserted in tests
- T-03-04: Process.Kill() on child only, not process group

## Self-Check: PASSED

All created files verified present. All task commits verified in git log.
