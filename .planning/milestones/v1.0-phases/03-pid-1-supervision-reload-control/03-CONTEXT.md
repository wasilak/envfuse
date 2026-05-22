# Phase 3: PID 1 Supervision & Reload Control - Context

**Gathered:** 2026-05-21
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 3 delivers PID 1 child-process supervision with deterministic restart control tied to effective config changes only, while preserving Phase 1/2 atomic sync guarantees.

</domain>

<decisions>
## Implementation Decisions

### Shutdown escalation
- **D-01:** Default graceful shutdown timeout is `10s` before force-kill.
- **D-02:** Use one global configurable timeout (`shutdown_timeout`) for all stop/restart paths.
- **D-03:** On timeout expiry, send `SIGKILL` to the supervised child process only (not full process group).
- **D-04:** After forced kill during restart handling, continue normal supervision loop and record the reason.

### Fingerprint scope
- **D-05:** Fingerprint input includes both effective env payload and rendered file bytes.
- **D-06:** Compute fingerprint from pre-commit candidate state for restart gating.
- **D-07:** Enforce determinism with canonical sorting by path/key/output path before hashing.
- **D-08:** Exclude runtime metadata (timestamps, latency, fetch order) from fingerprint.

### Restart coalescing
- **D-09:** Primary anti-loop mechanism is a cooldown window.
- **D-10:** If fingerprint changes during cooldown, mark pending and perform one restart after cooldown.
- **D-11:** Cooldown is configurable in `seon.json` with a safe default.
- **D-12:** Default cooldown is `10s`.

### Child exit policy
- **D-13:** If child exits on its own outside controlled reload, supervisor exits with the child exit code.
- **D-14:** If child start fails during controlled restart flow, envfuse exits non-zero (no in-process rollback child handoff).
- **D-15:** No in-process crash auto-retry policy in Phase 3; rely on orchestrator restart behavior.
- **D-16:** Minimum restart/exit reason taxonomy to capture now: `config_changed`, `child_exited`, `startup_failed`, `forced_kill`.

### the agent's Discretion
No discretionary areas were delegated in this discussion.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope and requirements
- `.planning/ROADMAP.md` - Phase 3 goal, dependencies, and success criteria boundary.
- `.planning/REQUIREMENTS.md` - SUPV-01..03 and RELO-01..03 requirement definitions.
- `.planning/PROJECT.md` - product constraints and atomic lifecycle intent.
- `.planning/STATE.md` - current sequencing context (Phase 2 complete, Phase 3 next).

### Existing implementation anchors
- `cmd/envfuse/main.go` - current `-once` bootstrap and child launch entry behavior.
- `internal/exec/launcher.go` - existing child execution path and env merge behavior.
- `internal/sync/coordinator.go` - successful-cycle commit point and state handoff boundaries.
- `internal/sync/model.go` - cycle result model used for control-flow outcomes.
- `internal/config/config.go` - current config schema and validation style to extend.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/exec/launcher.go` `LaunchWithEnv` / `RestartWithCommittedEnv`: reusable child-process/env handoff baseline for supervision loop wiring.
- `internal/state/store.go` (used by coordinator): last-applied env/file state already available for fingerprint input and restart gating.
- `internal/config/config.go`: strict `DisallowUnknownFields` config parsing and field validation pattern for new supervision/reload knobs.

### Established Patterns
- Coordinator uses all-or-nothing cycle semantics (`StageCandidate` then `CommitCandidate` only on full success) in `internal/sync/coordinator.go`.
- Failure classification is explicit (`ErrorClass` values in `CycleResult`) and should be mirrored for restart/exit reasons.
- Current process launch is synchronous `cmd.Run()` in launcher; Phase 3 will need `Start/Wait` plus signal-aware lifecycle control.

### Integration Points
- Replace phase-1-only `-once` gate in `cmd/envfuse/main.go` with long-running supervisor mode.
- Connect cycle success + fingerprint decision to controlled restart in sync/supervision boundary (likely coordinator output to exec layer contract).
- Extend `seon.json` schema in `internal/config/config.go` for shutdown timeout and cooldown settings.

</code_context>

<specifics>
## Specific Ideas

- Keep Phase 3 deterministic and minimal: one global timeout, one cooldown control, and orchestrator-owned crash retries.
- Keep reason taxonomy explicit now so Phase 5 observability can map directly without refactoring semantics.

</specifics>

<deferred>
## Deferred Ideas

None - discussion stayed within phase scope.

</deferred>

---

*Phase: 3-PID 1 Supervision & Reload Control*
*Context gathered: 2026-05-21*
