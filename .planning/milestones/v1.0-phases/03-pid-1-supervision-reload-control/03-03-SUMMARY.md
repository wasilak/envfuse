---
phase: 03-pid-1-supervision-reload-control
plan: "03"
subsystem: supervisor, config
tags: [cooldown, anti-loop, coalescing, relo-03, d-09, d-10, d-11, d-12, supervisor-lifecycle]
dependency_graph:
  requires: [internal/config, internal/supervisor, internal/fingerprint]
  provides: [cooldown-state-machine, anti-loop-coalescing]
  affects: [internal/supervisor/supervisor.go, internal/supervisor/cooldown.go, internal/config/config.go, cmd/envfuse/main.go, tests/integration/pid1_supervision_reload_test.go]
tech_stack:
  added:
    - "internal/supervisor/cooldown.go: cooldownTracker struct with recordRestart/inCooldown/markPending/isPending/drainPending"
    - "effectiveCooldown(cfg Config) helper returns configured or default 10s (D-12)"
    - "reload_cooldown config field with DefaultReloadCooldown=10s and ParsedReloadCooldown() method"
  patterns:
    - "Cooldown tracker is a value type with monotonic time comparison (time.Now().Before(coolUntil))"
    - "drainPending() atomically reads+clears pending flag — caller executes exactly one deferred restart"
    - "Pending drain checked at tick boundary before running new cycle — handles case where no new change arrives after cooldown"
    - "Fingerprint and env updated eagerly on change detection regardless of cooldown state — deferred restart uses current state"
key_files:
  created:
    - internal/supervisor/cooldown.go
  modified:
    - internal/supervisor/supervisor.go
    - internal/supervisor/supervisor_test.go
    - internal/config/config.go
    - cmd/envfuse/main.go
    - tests/integration/pid1_supervision_reload_test.go
decisions:
  - "cooldownTracker in separate file to isolate state machine from supervision loop logic"
  - "drainPending() atomic read+clear prevents double-restart on consecutive tick checks"
  - "Fingerprint updated eagerly (not deferred) so the deferred restart uses latest effective config"
  - "Pending drain checked at the top of each tick before cycle run — ensures deferred restart fires even if no subsequent change arrives"
metrics:
  duration_minutes: 4
  completed_date: "2026-05-22"
  tasks_completed: 3
  files_changed: 6
---

# Phase 3 Plan 3: Cooldown-Based Anti-Loop Restart Coalescing Summary

**One-liner:** Cooldown state machine (D-09..D-12) enforces a configurable post-restart hold window where multiple rapid fingerprint changes are coalesced into exactly one deferred restart, preventing restart storms (RELO-03).

## What Was Built

### internal/supervisor/cooldown.go (new)

`cooldownTracker` struct implementing the anti-loop coalescing state machine:

- `newCooldownTracker(duration time.Duration)` — creates tracker with configured cooldown
- `recordRestart()` — starts the cooldown window by setting `coolUntil = now + duration`
- `inCooldown() bool` — reports whether cooldown window is active via monotonic time comparison
- `markPending()` — sets the pending flag when a change arrives during cooldown (D-10)
- `isPending() bool` — reports whether a deferred restart is waiting
- `drainPending() bool` — atomically reads and clears the pending flag; returns true exactly once per coalesced window

### internal/supervisor/supervisor.go

Added `ReloadCooldown time.Duration` field to `Config` struct (D-11) and `effectiveCooldown(cfg Config) time.Duration` returning configured value or 10s default (D-12).

Changed `runLoop` to:
- Create a `cooldownTracker` once (shared across restart iterations)
- Call `ct.recordRestart()` after each restart to start the cooldown window

Changed `watch` to accept `ct *cooldownTracker` and apply two-stage cooldown logic:
1. At each tick: check `!ct.inCooldown() && ct.drainPending()` — if true, execute the coalesced deferred restart immediately without running a new cycle
2. After a successful cycle with fingerprint change: if `ct.inCooldown()`, call `ct.markPending()` and continue (D-09/D-10); otherwise, execute restart immediately

Fingerprint and env are updated eagerly on any detected change (including during cooldown) so the deferred restart always uses the latest effective config.

### internal/config/config.go

Added:
- `ReloadCooldown string` field with JSON tag `reload_cooldown`
- `DefaultReloadCooldown = 10 * time.Second` constant (D-12)
- `ParsedReloadCooldown() (time.Duration, error)` method matching existing `ParsedShutdownTimeout()` pattern
- Validation call in `LoadConfig` (invalid/non-positive duration rejected)

### internal/supervisor/supervisor_test.go

New unit tests:
- `TestSupervisor_DefaultCooldownValue`: verifies `effectiveCooldown()` returns 10s when `ReloadCooldown` is zero (D-12)
- `TestSupervisor_CooldownSuppressesImmediateRestart`: tests full cooldown lifecycle — `recordRestart` → `inCooldown` → `markPending` → expire → `drainPending` returns true once
- `TestSupervisor_MultipleChangesCoalesceToOneRestart`: 5 `markPending` calls during cooldown → `drainPending` returns true once, then false (D-10)
- `TestSupervisor_NoCooldownWithoutPriorRestart`: edge case — fresh tracker has no cooldown and no pending restart

### tests/integration/pid1_supervision_reload_test.go

New integration tests and helper:
- `TestPID1_CooldownCoalescesRestarts`: triggers initial restart, then 2 rapid changes during cooldown; asserts restart count == 3 (initial + first + one coalesced deferred restart, not 4+)
- `TestPID1_NoRestartLoopUnderRapidChanges`: 80ms change rate for 2s with 500ms cooldown; asserts restart count <= 6 (vs ~40 without cooldown)
- `waitForFile(t, path, timeout, desc)`: shared helper replacing repetitive polling loops

### cmd/envfuse/main.go

Added `cfg.ParsedReloadCooldown()` call and passes `ReloadCooldown` to `supervisor.Config`.

## Deviations from Plan

None — plan executed exactly as written. The cooldown tracker unit tests (Task 1 RED) directly referenced the future API (`newCooldownTracker`, `effectiveCooldown`, `ct.recordRestart`, etc.) which drove the exact interface shape for Task 2 GREEN.

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| cooldownTracker in separate file | Isolates 40-line state machine from 200+ line supervision loop; easier to reason about independently |
| drainPending() atomically clears flag | Prevents double-restart if drainPending() is called again on the same tick due to code path changes |
| Fingerprint updated eagerly during cooldown | Deferred restart must apply latest config, not the config at cooldown-start; eager update is correct |
| Pending drain checked before cycle on each tick | Handles the case where no new change arrives after cooldown expires — deferred restart still fires on next poll tick |

## Known Stubs

None — all anti-loop paths are exercised by both unit and integration tests.

## Threat Flags

None — all plan threat model mitigations implemented:
- T-03-09: Cooldown + pending coalescing enforced; bounded restart count verified by integration test
- T-03-10: reload_cooldown validated in LoadConfig; invalid/non-positive values rejected; default 10s applied when absent
- T-03-11: config_changed restart path follows reason taxonomy (D-16); forced_kill emitted on timeout escalation (existing D-04 path)

## Self-Check: PASSED

All created/modified files verified present. All task commits verified in git log.
- bc8d765: test(03-03): add failing tests for cooldown window and deferred restart coalescing
- fc71b06: feat(03-03): implement reload cooldown state machine with pending-change coalescing
