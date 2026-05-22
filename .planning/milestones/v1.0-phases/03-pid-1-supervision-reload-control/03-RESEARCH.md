# Phase 03: PID 1 Supervision & Reload Control - Research

**Researched:** 2026-05-21 [VERIFIED: system date]  
**Domain:** Go PID 1 child supervision, signal forwarding, deterministic restart gating by effective state fingerprint [VERIFIED: .planning/ROADMAP.md][VERIFIED: .planning/REQUIREMENTS.md]  
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
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

### Deferred Ideas (OUT OF SCOPE)
None - discussion stayed within phase scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SUPV-01 | Runtime can run as PID 1 and start the configured child command | Use `exec.Cmd.Start` + `Wait` supervision loop (not `Run`) and hold child lifecycle in main process control. [CITED: https://pkg.go.dev/os/exec#Cmd.Start][CITED: https://pkg.go.dev/os/exec#Cmd.Wait][VERIFIED: cmd/envfuse/main.go] |
| SUPV-02 | Runtime forwards `SIGTERM` and `SIGINT` to child process | Use `signal.NotifyContext(..., os.Interrupt, syscall.SIGTERM)` and forward to `cmd.Process.Signal`. [CITED: https://pkg.go.dev/os/signal#NotifyContext][CITED: https://pkg.go.dev/os/signal#Notify][CITED: https://pkg.go.dev/os#Process.Signal] |
| SUPV-03 | Runtime enforces configurable graceful shutdown timeout, then force-kills if needed | Implement one timeout knob (`shutdown_timeout`) and timeout path that issues `SIGKILL` to child process only. [VERIFIED: .planning/phases/03-pid-1-supervision-reload-control/03-CONTEXT.md][CITED: https://pkg.go.dev/os#Process.Kill] |
| RELO-01 | Runtime computes deterministic state fingerprint for fetched/rendered config | Hash canonicalized env payload + rendered file bytes with sorted keys/paths using SHA-256. [VERIFIED: .planning/phases/03-pid-1-supervision-reload-control/03-CONTEXT.md][CITED: https://pkg.go.dev/crypto/sha256#Sum256][CITED: https://pkg.go.dev/sort#Strings] |
| RELO-02 | Runtime restarts child when fingerprint changes after successful apply | Compute candidate fingerprint from staged pre-commit state, compare to last applied fingerprint, restart only on success+change. [VERIFIED: .planning/phases/03-pid-1-supervision-reload-control/03-CONTEXT.md][VERIFIED: internal/state/store.go][VERIFIED: internal/sync/coordinator.go] |
| RELO-03 | Runtime avoids restart loops using cooldown or coalescing controls | Use cooldown window + pending-change bit to coalesce into one restart after cooldown. [VERIFIED: .planning/phases/03-pid-1-supervision-reload-control/03-CONTEXT.md] |
</phase_requirements>

## Summary

Phase 3 is mostly a control-plane phase: the repo already has deterministic sync, staged/committed state, and child launch helpers, but it does not yet have long-running PID 1 supervision or restart gating. [VERIFIED: cmd/envfuse/main.go][VERIFIED: internal/sync/coordinator.go][VERIFIED: internal/state/store.go][VERIFIED: internal/exec/launcher.go]  
Planning should center on introducing a dedicated supervisor loop that decouples child lifecycle (`Start/Wait`, signal forwarding, graceful-stop timeout) from sync cycle execution, then links them via deterministic fingerprint decisions. [CITED: https://pkg.go.dev/os/exec#Cmd.Start][CITED: https://pkg.go.dev/os/exec#Cmd.Wait][VERIFIED: .planning/ROADMAP.md]

The discuss-phase decisions are specific and remove most design ambiguity: single global shutdown timeout (default 10s), child-only force-kill, cooldown (default 10s), pre-commit fingerprint scope (env + rendered bytes), and explicit exit/restart reasons. [VERIFIED: .planning/phases/03-pid-1-supervision-reload-control/03-CONTEXT.md]  
So the planner should produce implementation plans that optimize wiring correctness and test coverage rather than debating alternatives. [VERIFIED: .planning/phases/03-pid-1-supervision-reload-control/03-CONTEXT.md]

**Primary recommendation:** Implement a `supervisor` package with: (1) child lifecycle state machine, (2) fingerprint/cooldown restart arbiter, (3) reason taxonomy propagation into cycle/supervision outcomes. [VERIFIED: .planning/ROADMAP.md][VERIFIED: .planning/phases/03-pid-1-supervision-reload-control/03-CONTEXT.md]

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Child process spawn + lifecycle ownership | API / Backend runtime | — | envfuse is the runtime process and must own Start/Wait/exit decisions as PID 1. [VERIFIED: .planning/ROADMAP.md][VERIFIED: cmd/envfuse/main.go] |
| Signal reception and forwarding | API / Backend runtime | OS / Kernel signal subsystem | App receives host/orchestrator signals and forwards to child process. [CITED: https://pkg.go.dev/os/signal][CITED: https://pkg.go.dev/os#Process.Signal] |
| Graceful shutdown escalation | API / Backend runtime | OS / Kernel process subsystem | Timeout + SIGKILL policy is runtime orchestration logic over OS process APIs. [VERIFIED: 03-CONTEXT.md][CITED: https://pkg.go.dev/os#Process.Kill] |
| Effective-state fingerprinting | API / Backend runtime | Filesystem (rendered output source bytes) | Fingerprint policy combines env payload and rendered file bytes before commit. [VERIFIED: 03-CONTEXT.md][VERIFIED: internal/state/store.go] |
| Restart cooldown/coalescing | API / Backend runtime | — | Anti-loop state and timers are supervisor policy, not provider/render layers. [VERIFIED: 03-CONTEXT.md] |

## Project Constraints (from AGENTS.md)

- Use Go and Linux process/signal model patterns for runtime control. [VERIFIED: AGENTS.md]  
- Preserve no-partial-apply semantics from earlier phases while adding supervision/reload logic. [VERIFIED: AGENTS.md][VERIFIED: internal/sync/coordinator.go]  
- Keep secrets scoped to child env and declared files only; avoid leakage paths while implementing reload logic. [VERIFIED: AGENTS.md][VERIFIED: .planning/REQUIREMENTS.md]  
- Keep runtime lightweight; prefer stdlib and existing internal modules over new frameworks. [VERIFIED: AGENTS.md]

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go toolchain | 1.26.3 (local) | Build/runtime baseline | Current local and pkg.go.dev docs baseline for this project session. [VERIFIED: `go version`][VERIFIED: https://pkg.go.dev/os/exec] |
| `os/exec` (stdlib) | go1.26.3 docs | Child process supervision (`Start`, `Wait`, `Process`, `ExitError`) | Canonical process control API in Go stdlib. [CITED: https://pkg.go.dev/os/exec] |
| `os/signal` (stdlib) | go1.26.3 docs | Signal subscription and cancellation context | Canonical signal handling API, includes `NotifyContext`. [CITED: https://pkg.go.dev/os/signal#NotifyContext] |
| `crypto/sha256` (stdlib) | go1.26.3 docs | Deterministic state fingerprint digest | Built-in hash primitive with simple byte-oriented API. [CITED: https://pkg.go.dev/crypto/sha256#Sum256] |
| `sort` (stdlib) | go1.26.3 docs | Canonical ordering before hashing | Deterministic ordering requirement matches `sort.Strings`. [CITED: https://pkg.go.dev/sort#Strings][VERIFIED: 03-CONTEXT.md D-07] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `golang.org/x/sync/errgroup` | v0.20.0 | Continue concurrent sync-cycle orchestration | Reuse existing sync fan-out path; no replacement needed in this phase. [VERIFIED: go.mod][VERIFIED: `go list -m -json golang.org/x/sync@latest`][VERIFIED: internal/sync/coordinator.go] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| In-process supervisor lifecycle | external init wrapper (`tini`, shell scripts) | Adds split lifecycle ownership and weakens deterministic restart policy integration with cycle fingerprinting. [VERIFIED: AGENTS.md][ASSUMED] |
| SHA-256 fingerprint | non-cryptographic hash | Faster but higher accidental-collision risk; unnecessary optimization at this phase scale. [ASSUMED] |

**Installation:**
```bash
# No new mandatory deps for Phase 3 core path
# Existing dependency retained:
go list -m golang.org/x/sync
```

**Version verification:**
```bash
go version
go list -m -json golang.org/x/sync@latest
```

## Architecture Patterns

### System Architecture Diagram

```text
                Host signals (SIGINT/SIGTERM)
                           |
                           v
                   [envfuse PID 1 main]
                           |
         +-----------------+------------------+
         |                                    |
         v                                    v
[Sync Cycle Engine]                    [Supervisor Loop]
  (fetch/map/render)                     (child state)
         |                                    |
         v                                    |
[Stage candidate env+files]                  |
         |                                    |
         v                                    |
[Compute candidate fingerprint]--------------+
         |                      compare vs last applied + cooldown
         v                                    |
   success? ----no----> keep child running    |
         | yes                                 v
         v                           [Graceful stop -> timeout? -> SIGKILL]
[Commit candidate state]                        |
         |                                      v
         +-----------------------------> [Start child with committed env]
                                                |
                                                v
                                       [Wait child / exit policy]
```

### Recommended Project Structure
```text
internal/
├── supervisor/         # new: state machine, signal handling, cooldown/restart logic
├── fingerprint/        # new or co-located: canonicalize + hash env/files
├── exec/               # reuse launcher primitives; adapt for Start/Wait paths
├── sync/               # emit cycle success + candidate/applied state hooks
└── config/             # add shutdown_timeout + reload_cooldown schema/validation
```

### Pattern 1: Start/Wait Supervision (not Run)
**What:** Start child asynchronously, keep explicit `Wait` handling and supervisor control loop.  
**When to use:** Long-running PID 1 mode for Phase 3.  
**Example:**
```go
// Source: https://pkg.go.dev/os/exec#Cmd.Start
if err := cmd.Start(); err != nil { return err }
waitErr := cmd.Wait() // Source: https://pkg.go.dev/os/exec#Cmd.Wait
```

### Pattern 2: Signal-aware shutdown via NotifyContext
**What:** Convert SIGINT/SIGTERM into context cancellation; forward signal to child, then enforce timeout path.  
**When to use:** Normal stop and controlled restart flows.  
**Example:**
```go
// Source: https://pkg.go.dev/os/signal#NotifyContext
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

### Pattern 3: Deterministic fingerprint from canonical byte stream
**What:** Sort all logical keys/paths, serialize deterministically, hash bytes with SHA-256.  
**When to use:** RELO-01/02 restart gating.  
**Example:**
```go
// Source: https://pkg.go.dev/crypto/sha256#Sum256
sum := sha256.Sum256(canonicalBytes)
```

### Anti-Patterns to Avoid
- **Using `cmd.Run()` for supervised mode:** loses explicit control points needed for signal-forwarding and restart state machine coordination. [CITED: https://pkg.go.dev/os/exec#Cmd.Run][CITED: https://pkg.go.dev/os/exec#Cmd.Start]  
- **Hashing unsorted maps/files:** produces non-deterministic fingerprints and restart flapping. [VERIFIED: 03-CONTEXT.md D-07]  
- **Killing process group by default:** conflicts with locked decision to kill child process only. [VERIFIED: 03-CONTEXT.md D-03]  
- **Restart on failed cycle:** violates “successful changed-state cycles only” intent. [VERIFIED: .planning/ROADMAP.md]

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Signal multiplexing infrastructure | custom OS signal plumbing layer | `os/signal` Notify/NotifyContext | Official API already handles registration/reset semantics correctly. [CITED: https://pkg.go.dev/os/signal] |
| Child lifecycle primitives | ad-hoc syscall wrappers | `os/exec` + `os.Process` APIs | Standardized behavior around start/wait/exit errors. [CITED: https://pkg.go.dev/os/exec] |
| Cryptographic digest implementation | custom hashing code | `crypto/sha256` | Standard, audited implementation; avoids subtle correctness issues. [CITED: https://pkg.go.dev/crypto/sha256] |

**Key insight:** The complexity is in state transitions (when to stop/restart/exit), not in low-level primitives; reuse stdlib primitives and focus planning on explicit supervisor state machine tests. [VERIFIED: .planning/REQUIREMENTS.md][ASSUMED]

## Common Pitfalls

### Pitfall 1: Child exits and supervisor silently continues
**What goes wrong:** envfuse remains alive without workload process. [ASSUMED]  
**Why it happens:** `Wait` result not mapped to explicit D-13 exit policy path. [VERIFIED: 03-CONTEXT.md D-13][ASSUMED]  
**How to avoid:** Treat uncontrolled child exit as terminal for supervisor and exit with child code. [VERIFIED: 03-CONTEXT.md D-13]  
**Warning signs:** Main process running while child PID missing. [ASSUMED]

### Pitfall 2: Restart storms from tiny non-effective differences
**What goes wrong:** repeated restarts despite no meaningful config change. [ASSUMED]  
**Why it happens:** fingerprint includes non-deterministic metadata or unsorted serialization. [VERIFIED: 03-CONTEXT.md D-08][VERIFIED: 03-CONTEXT.md D-07]  
**How to avoid:** canonical sort + metadata exclusion + cooldown coalescing. [VERIFIED: 03-CONTEXT.md D-07][VERIFIED: 03-CONTEXT.md D-08][VERIFIED: 03-CONTEXT.md D-09..D-12]  
**Warning signs:** alternating fingerprints with equal effective env/file content. [ASSUMED]

### Pitfall 3: Leaking resources on repeated restart cycles
**What goes wrong:** zombie processes/file descriptor buildup over time. [ASSUMED]  
**Why it happens:** missing `Wait` calls or concurrent restart paths. [CITED: https://pkg.go.dev/os/exec#Cmd.Wait][ASSUMED]  
**How to avoid:** single-threaded supervisor state transitions with guaranteed wait/cleanup on every stop/start edge. [ASSUMED]  
**Warning signs:** process table shows defunct children after reload tests. [ASSUMED]

## Code Examples

### Forwarding stop signal to child
```go
// Source: https://pkg.go.dev/os#Process.Signal
if cmd.Process != nil {
    _ = cmd.Process.Signal(syscall.SIGTERM)
}
```

### Force-kill fallback after timeout
```go
// Source: https://pkg.go.dev/os#Process.Kill
if cmd.Process != nil {
    _ = cmd.Process.Kill()
}
```

### Exit code extraction from child termination
```go
// Source: https://pkg.go.dev/os#ProcessState.ExitCode
code := cmd.ProcessState.ExitCode()
os.Exit(code)
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| One-shot bootstrap (`-once`) only | Continuous supervisor loop with PID 1 lifecycle ownership | Required by Phase 3 requirements | Enables controlled runtime behavior under orchestrator signals/reloads. [VERIFIED: cmd/envfuse/main.go][VERIFIED: .planning/REQUIREMENTS.md] |
| Restart decision by “cycle succeeded” | Restart decision by “cycle succeeded + effective state changed + cooldown policy” | Required by RELO requirements and discuss decisions | Prevents unnecessary restart churn and loops. [VERIFIED: .planning/REQUIREMENTS.md][VERIFIED: 03-CONTEXT.md] |

**Deprecated/outdated:**
- Continuing to print “only -once mode is supported in phase 01” in main path is outdated for planned Phase 3 behavior. [VERIFIED: cmd/envfuse/main.go]

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | External init wrappers are categorically worse for this phase | Standard Stack / Alternatives | Medium — could over-constrain deployment options if org standard requires tini. |
| A2 | SHA-256 is preferred over non-cryptographic hashes for restart fingerprinting here | Standard Stack / Alternatives | Low — perf tradeoff may differ at extreme scale. |
| A3 | Zombie/fd leak risk is material without strict Wait discipline in this codebase | Common Pitfalls | Medium — test plan emphasis may be mis-prioritized. |
| A4 | Single-threaded supervisor state transition model is the best fit | Common Pitfalls | Medium — implementation may choose equivalent lock-based model. |

## Open Questions (RESOLVED)

1. **Polling trigger source for reload checks in PID 1 mode — RESOLVED**
   - Resolution: Phase 3 uses an in-process supervisor-driven cycle loop (internal trigger), not an external trigger contract. Reload evaluation happens in that supervisor loop after each cycle result, with no additional external event interface introduced in this phase. [RESOLVED: aligned with 03-01/03-02/03-03 plan sequence]
   - Implementation consequence: keep trigger ownership in runtime supervision path and verify behavior through `tests/integration/pid1_supervision_reload_test.go` scenarios (startup, restart-on-change, cooldown/coalescing). [RESOLVED]

2. **Where fingerprint ownership should live (sync vs supervisor package) — RESOLVED**
   - Resolution: fingerprint computation lives in a dedicated `internal/fingerprint` package; supervisor owns restart-gate decisions by comparing candidate-vs-last-applied digests, while sync/coordinator remains responsible for successful stage/commit boundaries. [RESOLVED: aligned with D-06 and RELO-01/02]
   - Implementation consequence: use candidate env/rendered payload from the sync path as pre-commit fingerprint input, then perform restart gating in `internal/supervisor/supervisor.go` only after success-gated transitions. [RESOLVED]

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain (`go`) | Build/tests for Phase 3 | ✓ | go1.26.3 | — |
| POSIX shell (`/bin/sh`) | Existing integration test patterns | ✓ | present | — |
| Docker | Optional future integration coverage | ✓ | 29.5.0 | not required for core Phase 3 |

**Missing dependencies with no fallback:**
- None found in this phase baseline checks. [VERIFIED: local command probes]

**Missing dependencies with fallback:**
- None. [VERIFIED: local command probes]

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Phase 3 scope has no auth flow changes. [VERIFIED: .planning/REQUIREMENTS.md] |
| V3 Session Management | no | No session model in this phase. [VERIFIED: .planning/REQUIREMENTS.md] |
| V4 Access Control | yes | Enforce child-only process control and bounded signal/kill actions per policy. [VERIFIED: 03-CONTEXT.md][CITED: https://pkg.go.dev/os#Process.Signal] |
| V5 Input Validation | yes | Validate new config knobs (`shutdown_timeout`, cooldown) under strict config decode rules. [VERIFIED: internal/config/config.go][VERIFIED: 03-CONTEXT.md] |
| V6 Cryptography | yes | Use standard SHA-256 implementation for fingerprinting; no custom crypto. [CITED: https://pkg.go.dev/crypto/sha256] |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Restart-thrash as self-DoS | Denial of Service | Cooldown window + single pending restart coalescing. [VERIFIED: 03-CONTEXT.md D-09..D-12] |
| Unbounded graceful-wait hang | Denial of Service | Global timeout and force-kill fallback policy. [VERIFIED: 03-CONTEXT.md D-01..D-04] |
| Ambiguous failure telemetry | Repudiation | Fixed reason taxonomy (`config_changed`, `child_exited`, `startup_failed`, `forced_kill`). [VERIFIED: 03-CONTEXT.md D-16] |

## Sources

### Primary (HIGH confidence)
- `.planning/phases/03-pid-1-supervision-reload-control/03-CONTEXT.md` — locked decisions and scope.
- `.planning/REQUIREMENTS.md` — SUPV/RELO requirement definitions.
- `.planning/ROADMAP.md` — Phase 3 goal and success criteria.
- `cmd/envfuse/main.go`, `internal/exec/launcher.go`, `internal/sync/coordinator.go`, `internal/state/store.go`, `internal/config/config.go` — implementation anchors.
- https://pkg.go.dev/os/exec — command lifecycle semantics.
- https://pkg.go.dev/os/signal — signal subscription and NotifyContext behavior.
- https://pkg.go.dev/os#ProcessState.ExitCode — exit code propagation.
- https://pkg.go.dev/crypto/sha256 — hash API.
- https://pkg.go.dev/sort — deterministic key sorting helper.

### Secondary (MEDIUM confidence)
- Context7 `/golang/go` — API cross-check for os/exec, os/signal, sha256.
- Context7 `/websites/pkg_go_dev_golang_org_x_sync` — errgroup cancellation semantics reference.

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - all core recommendations are stdlib APIs and current repo dependencies with direct docs/code verification.
- Architecture: HIGH - directly constrained by locked decisions (D-01..D-16) and current code entry points.
- Pitfalls: MEDIUM - several operational failure modes are inferred from supervisor patterns and need implementation-time validation.

**Research date:** 2026-05-21  
**Valid until:** 2026-06-20 (30 days; stdlib/process model stable, but repo internals may change quickly during Phase 3)
