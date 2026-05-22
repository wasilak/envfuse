# Phase 3: PID 1 Supervision & Reload Control - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md - this log preserves the alternatives considered.

**Date:** 2026-05-21
**Phase:** 3-PID 1 Supervision & Reload Control
**Areas discussed:** Shutdown escalation, Fingerprint scope, Restart coalescing, Child exit policy

---

## Shutdown escalation

| Option | Description | Selected |
|--------|-------------|----------|
| 10s | Balanced default for container workloads; enough for normal SIGTERM handling without long hangs. | ✓ |
| 30s | Safer for slower shutdown paths, but increases rollout/termination latency. | |
| 5s | Fast failover, but higher risk of killing during valid graceful teardown. | |

**User's choice:** 10s default graceful timeout.
**Notes:** User aligned all shutdown decisions with recommended deterministic baseline.

| Option | Description | Selected |
|--------|-------------|----------|
| Single global timeout | One `shutdown_timeout` for all stop/restart paths; simplest and deterministic. | ✓ |
| Separate stop/restart timeouts | More control for operators, but extra config surface and edge cases. | |
| Fixed non-configurable | Minimal config, but too rigid for varied workloads. | |

**User's choice:** Single global timeout.
**Notes:** Keep MVP config surface minimal.

| Option | Description | Selected |
|--------|-------------|----------|
| SIGKILL child only | Target only supervised child process; preserves clear ownership and predictable exit path. | ✓ |
| Kill process group | Also kills child descendants; stronger cleanup but higher blast radius risk. | |
| Never force-kill | Avoids hard termination but can deadlock PID 1 and break orchestrator expectations. | |

**User's choice:** SIGKILL child only.
**Notes:** Avoid broad kill semantics in MVP.

| Option | Description | Selected |
|--------|-------------|----------|
| Continue loop | Keeps supervisor available; restart reason can record forced-kill event for observability. | ✓ |
| Exit fatally | Fail-fast posture, but can cause avoidable container restarts for transient hangs. | |
| Configurable mode | Flexible, but adds complexity in phase focused on core control behavior. | |

**User's choice:** Continue loop after forced kill.
**Notes:** Forced-kill is treated as recoverable supervisor event.

---

## Fingerprint scope

| Option | Description | Selected |
|--------|-------------|----------|
| Env + rendered file bytes | Captures full effective runtime state from Phase 2 dual-vector apply path. | ✓ |
| Env only | Misses template/file-only changes; could skip needed restarts. | |
| Rendered files only | Misses env-only changes; incomplete for dual-vector delivery. | |

**User's choice:** Include env plus rendered file bytes.
**Notes:** Must reflect full effective config state.

| Option | Description | Selected |
|--------|-------------|----------|
| Pre-commit candidate | Decide restart before mutating process lifecycle; aligns with atomic cycle gate. | ✓ |
| Post-commit applied state | Works, but ties restart decision to already-committed apply path and complicates rollback semantics. | |
| Both and compare | Extra complexity with little added value in MVP. | |

**User's choice:** Pre-commit candidate state.
**Notes:** Keeps restart gating explicit before process action.

| Option | Description | Selected |
|--------|-------------|----------|
| Canonical sort by path/key/output path | Stable ordering across runs; robust against Go map iteration randomness. | ✓ |
| Serialize as-is | Risky: non-deterministic map order can create false-positive restarts. | |
| Store insertion order metadata | Adds bookkeeping complexity not needed with canonical sorting. | |

**User's choice:** Canonical sorting.
**Notes:** Deterministic hash required to avoid restart noise.

| Option | Description | Selected |
|--------|-------------|----------|
| No, data payload only | Prevents churn from non-semantic runtime noise. | ✓ |
| Include selected metadata | Could capture operational context, but risks restart noise. | |
| Include all metadata | Maximal churn and likely restart loops; not suitable. | |

**User's choice:** Exclude metadata from fingerprint.
**Notes:** Hash should reflect semantic state only.

---

## Restart coalescing

| Option | Description | Selected |
|--------|-------------|----------|
| Cooldown window | After a restart, suppress additional restarts for a configured minimum interval. | ✓ |
| Debounce only | Wait for quiet period before restarting, but weaker against continuous churn. | |
| Hard restart cap only | Caps frequency but may still thrash within cap boundaries. | |

**User's choice:** Cooldown window as primary anti-loop control.
**Notes:** Clear and deterministic loop prevention.

| Option | Description | Selected |
|--------|-------------|----------|
| Mark pending, restart once after cooldown | Coalesces bursts into a single restart while still applying latest good state. | ✓ |
| Ignore until next poll after cooldown | Simpler but can delay restart longer and miss immediate catch-up. | |
| Break cooldown for every change | Responsive but defeats anti-loop protection. | |

**User's choice:** Mark pending and restart once after cooldown.
**Notes:** Burst-safe while preserving eventual convergence.

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, with safe default | Lets operators tune behavior per workload while preserving deterministic default. | ✓ |
| No, fixed duration | Minimal config but too rigid across different app shutdown/start profiles. | |
| Configurable only via env var | Works, but weaker config discoverability and traceability. | |

**User's choice:** Configurable cooldown in `seon.json`.
**Notes:** Keep safe default while allowing tuning.

| Option | Description | Selected |
|--------|-------------|----------|
| 10s | Pairs well with 10s graceful timeout; reduces thrash without long stale windows. | ✓ |
| 5s | More responsive but allows more restart pressure. | |
| 30s | Strong protection but may keep workload stale too long. | |

**User's choice:** 10s default cooldown.
**Notes:** Symmetric baseline with shutdown timeout.

---

## Child exit policy

| Option | Description | Selected |
|--------|-------------|----------|
| Exit with child code | Classic PID 1 wrapper behavior; lets orchestrator restart container predictably. | ✓ |
| Always restart child | Keeps process alive, but risks hiding crash loops in-process. | |
| Configurable policy | Flexible but adds branching complexity in MVP supervision core. | |

**User's choice:** Exit with child code.
**Notes:** Keep orchestrator-responsible crash handling model.

| Option | Description | Selected |
|--------|-------------|----------|
| Fail phase and exit non-zero | Surface hard failure clearly; avoids running stale/no-child state silently. | ✓ |
| Revert to previous child | Complex rollback of process state; risky for MVP. | |
| Retry internally forever | Can trap PID 1 in loop and delay orchestrator-level recovery. | |

**User's choice:** Exit non-zero on startup failure during controlled restart.
**Notes:** No hidden degraded mode.

| Option | Description | Selected |
|--------|-------------|----------|
| No retries in Phase 3 | Keep MVP deterministic and delegate restart policy to orchestrator for now. | ✓ |
| Yes, fixed retry count | Adds resilience but introduces extra state machine complexity immediately. | |
| Yes, configurable retries | Most flexible, highest complexity and testing surface. | |

**User's choice:** No in-process retries in Phase 3.
**Notes:** Retry policy deferred beyond MVP core.

| Option | Description | Selected |
|--------|-------------|----------|
| config_changed, child_exited, startup_failed, forced_kill | Enough reason classes to debug lifecycle decisions without adding full metrics work yet. | ✓ |
| config_changed only | Too little context for crash/shutdown diagnostics. | |
| Detailed 10+ reasons | Richer taxonomy but over-specifies before Phase 5 observability implementation. | |

**User's choice:** Minimal four-reason taxonomy.
**Notes:** Good bridge into Phase 5 observability scope.

---

## the agent's Discretion

None.

## Deferred Ideas

None.
