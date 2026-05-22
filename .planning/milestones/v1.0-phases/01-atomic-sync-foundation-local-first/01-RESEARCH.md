# Phase 1: Atomic Sync Foundation (Local-First) - Research

**Researched:** 2026-05-21 [VERIFIED: system date]
**Domain:** Go local-file provider + atomic batch sync orchestration [VERIFIED: .planning/ROADMAP.md]
**Confidence:** HIGH

## User Constraints

No `*-CONTEXT.md` exists for this phase yet, so there are no locked discuss-phase decisions to copy verbatim. [VERIFIED: glob .planning/phases/01-atomic-sync-foundation-local-first/*-CONTEXT.md]

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PROV-03 | Developer can run envfuse with a local JSON file backend for offline testing | Local provider contract + strict JSON decode pattern + deterministic error reporting in sync cycle design. [VERIFIED: .planning/REQUIREMENTS.md][CITED: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields] |
| SYNC-01 | Runtime fetches all configured secret paths as one batch per cycle | Single cycle coordinator pattern (`collect -> validate -> apply`) with commit gate only after all fetches succeed. [VERIFIED: .planning/REQUIREMENTS.md][CITED: https://pkg.go.dev/golang.org/x/sync/errgroup#Group.Wait] |
| SYNC-02 | Runtime fetches distinct provider paths concurrently | `errgroup.WithContext` fan-out across unique paths, bounded with `SetLimit` when needed. [VERIFIED: .planning/REQUIREMENTS.md][CITED: https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext] |
| SYNC-03 | Runtime aborts cycle if any fetch fails or times out | Per-path `context.WithTimeout`; first error cancels sibling fetches via group context. [VERIFIED: .planning/REQUIREMENTS.md][CITED: https://pkg.go.dev/context#WithTimeout][CITED: https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext] |
| SYNC-04 | Runtime preserves last known good env/files if cycle fails | Stage outputs in memory/temp files, commit via atomic rename only on full success; no partial apply path. [VERIFIED: .planning/REQUIREMENTS.md][CITED: https://pkg.go.dev/os#WriteFile][CITED: https://pkg.go.dev/os#Rename] |
</phase_requirements>

## Summary

Phase 1 should establish a **transactional sync engine** before provider breadth or PID 1 work. The phase scope is local-first (`seon.json` + local JSON backend), but the design must already enforce batch semantics that remain valid for Vault/AWS later. [VERIFIED: .planning/ROADMAP.md][VERIFIED: .planning/REQUIREMENTS.md]

The core implementation choice is: **fetch concurrently, apply serially, commit atomically**. Concurrent fetches improve cycle latency, but writes must only occur after a full batch passes validation. This is mandatory because Go’s `os.WriteFile` can leave partially written files on failure, so direct-in-place writes violate SYNC-04. [CITED: https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext][CITED: https://pkg.go.dev/os#WriteFile]

For local developer experience (PROV-03), use strict config decoding (`DisallowUnknownFields`) and deterministic cycle result statuses (`success`, `failed`, `aborted`) so offline testing behavior is predictable and debuggable. [CITED: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields][VERIFIED: .planning/ROADMAP.md]

**Primary recommendation:** Implement a `SyncCoordinator` that performs `(1) path dedup + concurrent fetch with timeout, (2) in-memory staging + validation, (3) atomic commit gate` and never mutates runtime state before full-batch success. [CITED: https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext][CITED: https://pkg.go.dev/os#Rename]

## Project Constraints (from AGENTS.md)

- Use Go and Linux process/signal model as baseline architecture. [VERIFIED: AGENTS.md]
- Never apply partial updates on batch failure. [VERIFIED: AGENTS.md]
- Secrets must not leak to logs or global environment targets. [VERIFIED: AGENTS.md]
- Keep runtime lightweight; avoid unnecessary runtime dependencies. [VERIFIED: AGENTS.md]
- Work is in GSD planning flow (`/gsd-plan-phase` already active by context). [VERIFIED: AGENTS.md]

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Parse `seon.json` and validate local provider config | API / Backend runtime (envfuse process) | — | This is bootstrap/runtime control-plane logic, not app code. [VERIFIED: .planning/ROADMAP.md] |
| Fetch secrets from local JSON backend | API / Backend runtime | Filesystem | Fetch logic is process-internal; source of truth is local file path(s). [VERIFIED: PROV-03 in .planning/REQUIREMENTS.md] |
| Batch orchestration across secret paths | API / Backend runtime | — | Sync semantics and cycle state belong to runtime coordinator. [VERIFIED: SYNC-01..04 in .planning/REQUIREMENTS.md] |
| Concurrent execution and timeout cancellation | API / Backend runtime | — | Concurrency primitives (`errgroup`, context cancellation) are runtime responsibilities. [CITED: https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext][CITED: https://pkg.go.dev/context#WithTimeout] |
| Atomic state commit / rollback behavior | API / Backend runtime | Filesystem | Commit boundary is runtime-owned; filesystem provides rename/sync primitives. [CITED: https://pkg.go.dev/os#Rename][CITED: https://pkg.go.dev/os#File.Sync] |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go toolchain | 1.26.3 | Main runtime implementation | Current local environment and stable stdlib APIs for context/fs/process primitives. [VERIFIED: `go version`] |
| `golang.org/x/sync/errgroup` | v0.20.0 (2026-02-23) | Concurrent fetch fan-out + error propagation | First-error cancellation and group wait semantics directly match SYNC-02/03. [VERIFIED: `go list -m -json golang.org/x/sync@latest`][CITED: https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext] |
| `encoding/json` (stdlib) | go1.26.3 | Strict local backend/config decode | `Decoder.DisallowUnknownFields()` prevents silent config drift. [CITED: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields] |
| `os` / `context` (stdlib) | go1.26.3 | Atomic-ish file commit primitives + timeout control | `WriteFile` is non-transactional, so staged write + rename pattern is required. [CITED: https://pkg.go.dev/os#WriteFile][CITED: https://pkg.go.dev/os#Rename][CITED: https://pkg.go.dev/context#WithTimeout] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/stretchr/testify` | v1.11.1 (2025-08-27) | Test assertions for cycle outcomes | Use for table tests around success/fail/timeout semantics. [VERIFIED: `go list -m -json github.com/stretchr/testify@latest`] |
| `github.com/google/go-cmp` | v0.7.0 (2025-01-14) | Deterministic state diff in tests | Use when comparing staged vs applied state snapshots. [VERIFIED: `go list -m -json github.com/google/go-cmp@latest`] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `errgroup.WithContext` | raw `sync.WaitGroup` + custom error channel | More boilerplate; easier to get cancellation/error race handling wrong. [CITED: https://pkg.go.dev/golang.org/x/sync/errgroup] |
| strict struct decode | decode to `map[string]any` | Faster prototyping, but weaker schema guarantees and more runtime key/type errors. [CITED: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields] |

**Installation (Go modules):**
```bash
go get golang.org/x/sync@v0.20.0
go get github.com/stretchr/testify@v1.11.1
go get github.com/google/go-cmp@v0.7.0
```

**Version verification used in this research:**
```bash
go list -m -json golang.org/x/sync@latest
go list -m -json github.com/stretchr/testify@latest
go list -m -json github.com/google/go-cmp@latest
go version
```

## Architecture Patterns

### System Architecture Diagram

```text
seon.json + local secrets.json
          |
          v
   [Config Loader]
          |
          v
 [Path Set Builder / Dedup]
          |
          v
 [Sync Cycle Coordinator] -----> [Cycle Timeout Controller]
          |                                |
          | fan-out (concurrent)           |
          v                                |
  [Fetch Worker per distinct path] <-------+
          |
          v
 [Batch Result Aggregator]
    | success(all)         | any error/timeout
    v                      v
[Stage Candidate State]  [Reject Cycle]
    |                      |
    v                      v
[Atomic Commit Gate]   [Keep Last Known Good]
    |
    v
[Applied Env/File State + Deterministic cycle status]
```

### Recommended Project Structure
```text
internal/
├── config/          # seon.json schema + strict decode
├── provider/local/  # local JSON backend implementation
├── sync/            # coordinator, batch model, cycle result states
├── state/           # last-known-good snapshot + commit boundary
└── clock/           # timeout/retry testability abstractions

cmd/envfuse/         # startup wiring and CLI entrypoint
tests/               # integration tests for phase semantics
```

### Pattern 1: Concurrent Fetch + Fail-Fast Cancellation
**What:** Use `errgroup.WithContext` to fetch each distinct provider path concurrently; first failure cancels siblings.
**When to use:** Every sync cycle with 2+ independent paths.
**Example:**
```go
// Source: https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext
g, gctx := errgroup.WithContext(ctx)
for _, p := range distinctPaths {
    p := p
    g.Go(func() error {
        pctx, cancel := context.WithTimeout(gctx, perPathTimeout)
        defer cancel()
        _, err := provider.Fetch(pctx, p)
        return err
    })
}
if err := g.Wait(); err != nil {
    return cycleFailed(err)
}
```

### Pattern 2: Stage-Then-Commit (No In-Place Mutation)
**What:** Build candidate env/file outputs in staging, then apply only after full-batch success.
**When to use:** Always (startup + every poll cycle).
**Example:**
```go
// Source: https://pkg.go.dev/os#WriteFile and https://pkg.go.dev/os#Rename
// NOTE: WriteFile can leave partial writes on failure, so do not write final target directly.
tmp := target + ".tmp"
if err := os.WriteFile(tmp, rendered, 0o600); err != nil { return err }
f, err := os.OpenFile(tmp, os.O_RDWR, 0)
if err != nil { return err }
if err := f.Sync(); err != nil { _ = f.Close(); return err }
_ = f.Close()
if err := os.Rename(tmp, target); err != nil { return err }
```

### Pattern 3: Strict JSON Decode for Local Backend
**What:** Decode into typed structs and reject unknown fields.
**When to use:** Parsing `seon.json` and local secrets file.
**Example:**
```go
// Source: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields
dec := json.NewDecoder(r)
dec.DisallowUnknownFields()
var cfg Config
if err := dec.Decode(&cfg); err != nil {
    return fmt.Errorf("invalid config: %w", err)
}
```

### Anti-Patterns to Avoid
- **Custom goroutine orchestration with ad-hoc error channels:** easy to leak goroutines or return nondeterministic first-error behavior; use errgroup. [CITED: https://pkg.go.dev/golang.org/x/sync/errgroup]
- **Direct `os.WriteFile` to final targets during cycle:** can violate SYNC-04 on mid-write failures. [CITED: https://pkg.go.dev/os#WriteFile]
- **Lenient map-based config parsing:** silently ignores unknown keys unless explicitly checked. [CITED: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields]

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Fan-out/fan-in with cancellation | Custom waitgroup + error mux | `errgroup.WithContext` | Canonical Go pattern, simpler and less error-prone for first-failure cancellation. [CITED: https://pkg.go.dev/golang.org/x/sync/errgroup] |
| Timeout tree management | Manual timer plumbing in each worker | `context.WithTimeout` per fetch | Standardized cancellation propagation and resource release semantics. [CITED: https://pkg.go.dev/context#WithTimeout] |
| JSON schema enforcement | Homegrown unknown-key validation | `json.Decoder.DisallowUnknownFields` | Built-in strictness with minimal code. [CITED: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields] |

**Key insight:** Most failures in this phase come from **state transition mistakes**, not provider parsing. Prefer well-known stdlib/x packages for concurrency, timeout, and decode controls, then focus custom code on transactional boundaries. [VERIFIED: phase requirements emphasize atomic semantics]

## Common Pitfalls

### Pitfall 1: Dedup after spawning workers
**What goes wrong:** same path fetched multiple times in one cycle; may produce inconsistent status aggregation. [ASSUMED]
**Why it happens:** dedup done at merge time instead of pre-fan-out. [ASSUMED]
**How to avoid:** construct distinct path set before launching goroutines. [ASSUMED]
**Warning signs:** duplicate fetch logs for identical path in one cycle. [ASSUMED]

### Pitfall 2: Partial apply during failed cycle
**What goes wrong:** one file/env updated before another path fails; last-known-good is corrupted. [VERIFIED: SYNC-04 semantics]
**Why it happens:** direct writes to live targets before full-batch success. [CITED: https://pkg.go.dev/os#WriteFile]
**How to avoid:** stage all outputs, then single commit gate. [VERIFIED: SYNC-04 in .planning/REQUIREMENTS.md]
**Warning signs:** failed cycle followed by changed runtime-visible config. [ASSUMED]

### Pitfall 3: Timeout without cancellation propagation
**What goes wrong:** one timed-out worker returns, others continue and mutate shared state late. [ASSUMED]
**Why it happens:** independent contexts not derived from shared group context. [CITED: https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext]
**How to avoid:** derive per-path timeout from errgroup context; never from background context. [CITED: https://pkg.go.dev/context#WithTimeout]
**Warning signs:** cycle marked failed but late worker logs appear after decision. [ASSUMED]

## Code Examples

Verified patterns from official sources:

### Concurrent batch with deterministic error result
```go
// Source: https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext
g, ctx := errgroup.WithContext(parent)
for _, path := range paths {
    path := path
    g.Go(func() error {
        // provider.Fetch must be side-effect free
        _, err := provider.Fetch(ctx, path)
        return err
    })
}
if err := g.Wait(); err != nil {
    return CycleResult{Status: "failed", Err: err}
}
return CycleResult{Status: "success"}
```

### Strict local config decode
```go
// Source: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields
func decodeConfig(r io.Reader) (Config, error) {
    dec := json.NewDecoder(r)
    dec.DisallowUnknownFields()
    var cfg Config
    if err := dec.Decode(&cfg); err != nil {
        return Config{}, err
    }
    return cfg, nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Serial path fetch with best-effort apply | Concurrent fetch + fail-fast cancellation + atomic commit gate | Matured in Go ecosystem with `errgroup` usage patterns; current module latest is v0.20.0 (2026-02-23) [VERIFIED] | Better cycle latency and clearer all-or-nothing semantics for SYNC requirements. [VERIFIED: requirements intent] |
| Direct file overwrite writes | Staged temp write + sync + rename commit | Current `os.WriteFile` docs explicitly warn about partial-write failure behavior [CITED] | Prevents partial state on write failure paths. [CITED: https://pkg.go.dev/os#WriteFile] |

**Deprecated/outdated:**
- Treating `os.Rename` as universally atomic across all platforms is outdated; docs explicitly call out non-Unix caveat. [CITED: https://pkg.go.dev/os#Rename]

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Duplicate-path fetches are likely in naive implementations | Common Pitfalls 1 | Medium — planner might under-prioritize path dedup if code naturally avoids this. |
| A2 | Late worker logs after cycle failure indicate cancellation bug | Common Pitfalls 3 | Low — diagnostic heuristic may vary by logger buffering. |

## Open Questions (RESOLVED)

1. **Should commit include directory fsync for stronger durability semantics? — RESOLVED**
   - What we know: `File.Sync()` semantics are documented for file content flush. [CITED: https://pkg.go.dev/os#File.Sync]
   - Decision (Phase 1): Do **not** require directory fsync in this phase; enforce atomic semantic correctness (SYNC-04) with staged write + file sync + rename.
   - Rationale: Phase 1 success criteria require all-or-nothing cycle behavior, not power-loss crash-consistency hardening. [VERIFIED: .planning/ROADMAP.md success criteria]
   - Follow-up: directory fsync hardening is explicitly deferred to later operational hardening scope.

2. **Do we need bounded concurrency (`SetLimit`) in Phase 1 MVP? — RESOLVED**
   - What we know: `errgroup.SetLimit` exists and blocks when limit reached. [CITED: https://pkg.go.dev/golang.org/x/sync/errgroup#Group.SetLimit]
   - Decision (Phase 1): Keep concurrent fetch fan-out unbounded by default for local-first MVP; no config knob in this phase.
   - Rationale: Phase 1 is offline/local validation with small path sets; additional concurrency tuning adds config surface without requirement pressure from PROV-03/SYNC-01..04.
   - Follow-up: revisit `SetLimit` when production providers (Phase 4) introduce higher path counts and remote backend contention.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | Build/run/tests | ✓ | go1.26.3 | — [VERIFIED: `go version`] |
| Git | Source workflow and reproducibility | ✓ | available | — [VERIFIED: `command -v git`] |
| Docker | Optional containerized manual validation | ✓ | available | Native local run [VERIFIED: `command -v docker`] |

**Missing dependencies with no fallback:**
- None identified. [VERIFIED: environment probes above]

**Missing dependencies with fallback:**
- None identified. [VERIFIED]

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (Phase 1 scope) | N/A for local file backend MVP. [VERIFIED: Phase 1 requirements] |
| V3 Session Management | no (Phase 1 scope) | N/A for local runtime sync semantics. [VERIFIED: Phase 1 requirements] |
| V4 Access Control | yes (file target boundaries) | Restrict apply targets to declared config outputs only. [VERIFIED: project constraint + SECU-03 roadmap mapping] |
| V5 Input Validation | yes | Strict typed JSON decode with unknown-field rejection. [CITED: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields] |
| V6 Cryptography | no (local backend) | No remote transport in Phase 1 local-first mode. [VERIFIED: PROV-03 scope] |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Malformed/poisoned local JSON config | Tampering | Strict schema decode + fail cycle before apply. [CITED: https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields] |
| Partial secret materialization on failure | Tampering / Information Disclosure | Stage candidate state and only commit on full success. [VERIFIED: SYNC-03/SYNC-04] |
| Secret leakage via diagnostics | Information Disclosure | Never log secret values; log path/status only. [VERIFIED: AGENTS.md security constraint] |

## Sources

### Primary (HIGH confidence)
- `.planning/REQUIREMENTS.md` — authoritative requirement semantics for PROV-03 and SYNC-01..04.
- `.planning/ROADMAP.md` — phase scope and success criteria.
- `AGENTS.md` (project) — non-negotiable architecture/safety/security constraints.
- https://pkg.go.dev/golang.org/x/sync/errgroup — group cancellation/error semantics.
- https://pkg.go.dev/context#WithTimeout — timeout context semantics and cancellation guidance.
- https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields — strict decode behavior.
- https://pkg.go.dev/os#WriteFile — partial-write caveat.
- https://pkg.go.dev/os#Rename — replacement + non-Unix atomicity caveat.
- https://pkg.go.dev/os#File.Sync — stable-storage flush semantics.
- Local verification commands: `go version`, `go list -m -json ...@latest`, `command -v ...`.

### Secondary (MEDIUM confidence)
- Context7 `/golang/go` snippets for filesystem primitives (used as cross-check against pkg.go.dev).
- Context7 `/websites/pkg_go_dev_golang_org_x_sync` snippets for errgroup behavior.

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — versions and APIs validated from local toolchain/module index + official docs.
- Architecture: HIGH — directly derived from explicit SYNC/PROV requirements and project constraints.
- Pitfalls: MEDIUM — partly verified from docs, partly operational assumptions logged in Assumptions Log.

**Research date:** 2026-05-21
**Valid until:** 2026-06-20 (30 days; stable domain, but module/toolchain versions can move)
