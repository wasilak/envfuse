# Phase 1: Atomic Sync Foundation (Local-First) - Pattern Map

**Mapped:** 2026-05-21  
**Files analyzed:** 10  
**Analogs found:** 0 / 10

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `cmd/envfuse/main.go` | controller | request-response | none (greenfield repo) | no-analog |
| `internal/config/config.go` | config | transform | none (greenfield repo) | no-analog |
| `internal/provider/provider.go` | interface/utility | request-response | none (greenfield repo) | no-analog |
| `internal/provider/local/local.go` | service | file-I/O | none (greenfield repo) | no-analog |
| `internal/sync/coordinator.go` | service | batch | none (greenfield repo) | no-analog |
| `internal/sync/model.go` | model | transform | none (greenfield repo) | no-analog |
| `internal/state/store.go` | service | CRUD | none (greenfield repo) | no-analog |
| `internal/clock/clock.go` | utility | transform | none (greenfield repo) | no-analog |
| `internal/sync/coordinator_test.go` | test | batch | none (greenfield repo) | no-analog |
| `tests/integration/atomic_sync_test.go` | test | batch | none (greenfield repo) | no-analog |

## Pattern Assignments

> Repository status: no existing implementation files (`**/*.go` returned no matches), so there are no meaningful in-repo code analogs yet.  
> Bootstrap patterns below are taken from phase research artifacts and should be used to seed the first implementation pass.

### `cmd/envfuse/main.go` (controller, request-response)

**Analog:** none in repo

**Bootstrap wiring pattern** (`.planning/phases/01-atomic-sync-foundation-local-first/01-RESEARCH.md` lines 121-132):
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

### `internal/config/config.go` (config, transform)

**Analog:** none in repo

**Strict JSON decode pattern** (`01-RESEARCH.md` lines 175-182):
```go
dec := json.NewDecoder(r)
dec.DisallowUnknownFields()
var cfg Config
if err := dec.Decode(&cfg); err != nil {
    return fmt.Errorf("invalid config: %w", err)
}
```

### `internal/provider/provider.go` (interface/utility, request-response)

**Analog:** none in repo

**Provider contract pattern** (`AGENTS.md` line 28):
```go
Fetch(ctx, resourceURI) (map[string]any, error)
```

### `internal/provider/local/local.go` (service, file-I/O)

**Analog:** none in repo

**Local backend decode pattern** (`01-RESEARCH.md` lines 171-183):
```go
dec := json.NewDecoder(r)
dec.DisallowUnknownFields()
var cfg Config
if err := dec.Decode(&cfg); err != nil {
    return fmt.Errorf("invalid config: %w", err)
}
```

### `internal/sync/coordinator.go` (service, batch)

**Analog:** none in repo

**Concurrent fetch + fail-fast pattern** (`01-RESEARCH.md` lines 140-152):
```go
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

**Cycle transaction boundary pattern** (`01-RESEARCH.md` lines 27-31):
```text
fetch concurrently, apply serially, commit atomically
never mutate runtime state before full-batch success
```

### `internal/sync/model.go` (model, transform)

**Analog:** none in repo

**Deterministic cycle result status pattern** (`01-RESEARCH.md` lines 29 and 236-239):
```go
if err := g.Wait(); err != nil {
    return CycleResult{Status: "failed", Err: err}
}
return CycleResult{Status: "success"}
```

### `internal/state/store.go` (service, CRUD)

**Analog:** none in repo

**Stage-then-commit pattern** (`01-RESEARCH.md` lines 160-169):
```go
tmp := target + ".tmp"
if err := os.WriteFile(tmp, rendered, 0o600); err != nil { return err }
f, err := os.OpenFile(tmp, os.O_RDWR, 0)
if err != nil { return err }
if err := f.Sync(); err != nil { _ = f.Close(); return err }
_ = f.Close()
if err := os.Rename(tmp, target); err != nil { return err }
```

### `internal/clock/clock.go` (utility, transform)

**Analog:** none in repo

**Timeout control pattern** (`01-RESEARCH.md` lines 144-146):
```go
pctx, cancel := context.WithTimeout(gctx, perPathTimeout)
defer cancel()
_, err := provider.Fetch(pctx, p)
```

### `internal/sync/coordinator_test.go` (test, batch)

**Analog:** none in repo

**Deterministic outcome guidance** (`01-RESEARCH.md` lines 29 and 224-240):
```text
Assert explicit cycle status outcomes.
On first worker failure/timeout, cycle result is failed and no apply occurs.
```

### `tests/integration/atomic_sync_test.go` (test, batch)

**Analog:** none in repo

**Phase-level acceptance test guidance** (`.planning/ROADMAP.md` lines 28-33):
```text
Batch all configured paths per cycle.
Fetch distinct paths concurrently.
Reject cycle on any fetch failure/timeout.
Preserve last-known-good state when cycle fails.
```

## Shared Patterns

### Concurrent Fan-Out + Cancellation
**Source:** `01-RESEARCH.md` lines 140-152  
**Apply to:** `internal/sync/coordinator.go`
```go
g, gctx := errgroup.WithContext(ctx)
...
if err := g.Wait(); err != nil {
    return cycleFailed(err)
}
```

### Strict Input Validation
**Source:** `01-RESEARCH.md` lines 175-182  
**Apply to:** `internal/config/config.go`, `internal/provider/local/local.go`
```go
dec := json.NewDecoder(r)
dec.DisallowUnknownFields()
```

### Atomic Commit Boundary (No Partial Apply)
**Source:** `01-RESEARCH.md` lines 160-169 and AGENTS.md lines 14-16  
**Apply to:** `internal/state/store.go`, `internal/sync/coordinator.go`
```go
// stage to temp + fsync + rename; never write final target in-place
```

### Secret-Safe Logging Rule
**Source:** AGENTS.md lines 15-16  
**Apply to:** all runtime files
```text
Never log secret values; log path/status/error class only.
```

## No Analog Found

Repository is currently greenfield (no `.go` source files). Planner should treat this phase as bootstrap implementation and use research-backed patterns as temporary source-of-truth.

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `cmd/envfuse/main.go` | controller | request-response | No existing runtime entrypoint in repo |
| `internal/config/config.go` | config | transform | No existing config parser in repo |
| `internal/provider/provider.go` | interface/utility | request-response | No provider abstractions exist yet |
| `internal/provider/local/local.go` | service | file-I/O | No provider implementations exist yet |
| `internal/sync/coordinator.go` | service | batch | No sync orchestration layer exists yet |
| `internal/sync/model.go` | model | transform | No cycle/state model types exist yet |
| `internal/state/store.go` | service | CRUD | No last-known-good state store exists yet |
| `internal/clock/clock.go` | utility | transform | No time abstraction utilities exist yet |
| `internal/sync/coordinator_test.go` | test | batch | No test baseline in repo |
| `tests/integration/atomic_sync_test.go` | test | batch | No integration test suite in repo |

## Metadata

**Analog search scope:** repository root (`/Users/piotrek/git/envfuse`)  
**File discovery searched:** `**/*.go`, `**/*_test.go`, `.claude/skills/*`, `.agents/skills/*`  
**Files scanned for analogs:** 0 source files (greenfield)  
**Pattern extraction date:** 2026-05-21
