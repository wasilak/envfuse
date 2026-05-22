---
phase: 01-atomic-sync-foundation-local-first
reviewed: 2026-05-21T11:07:13Z
depth: standard
files_reviewed: 10
files_reviewed_list:
  - cmd/envfuse/main.go
  - internal/config/config.go
  - internal/provider/provider.go
  - internal/provider/local/local.go
  - internal/sync/model.go
  - internal/sync/coordinator.go
  - internal/sync/coordinator_test.go
  - internal/state/store.go
  - internal/clock/clock.go
  - tests/integration/atomic_sync_test.go
findings:
  critical: 1
  warning: 3
  info: 0
  total: 4
status: issues_found
---

# Phase 01: Code Review Report

**Reviewed:** 2026-05-21T11:07:13Z
**Depth:** standard
**Files Reviewed:** 10
**Status:** issues_found

## Summary

Reviewed source changes from plans 01-01 and 01-02 (docs excluded), focusing on correctness, security posture, and maintainability.

Main risk: cycle status classification is race-prone and can incorrectly report `aborted` instead of `failed` when a real fetch error occurs concurrently with cancellation. This breaks deterministic behavior required by SYNC-03/SYNC-04 semantics and can mislead operators/automation.

Additional warnings include configuration validation gaps and one test reliability defect (concurrent map access race in integration test stubs).

## Critical Issues

### CR-01: Non-deterministic error classification (`failed` vs `aborted`) under concurrent cancellation

**Classification:** BLOCKER  
**File:** `internal/sync/coordinator.go:71-75` (originating at `internal/sync/coordinator.go:59-62`)

**Issue:**
`RunCycle` decides final status from whichever error `errgroup.Wait()` returns first. If one worker has a real fetch error while sibling workers return `context.Canceled` after group cancellation, the first returned error can be cancellation, producing `CycleStatusAborted` instead of `CycleStatusFailed`.

This violates deterministic status semantics and can hide genuine provider failures.

**Fix:**
Track whether any non-context fetch error occurred across workers, and prioritize `failed` if true.

```go
var sawFetchError atomic.Bool

g.Go(func() error {
    pathCtx, cancel := context.WithTimeout(gctx, c.perPathTimeout)
    defer cancel()

    data, err := c.provider.Fetch(pathCtx, path)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
            return err
        }
        sawFetchError.Store(true)
        return fmt.Errorf("fetch path %s: %w", path, err)
    }
    // ...
    return nil
})

if err := g.Wait(); err != nil {
    if sawFetchError.Load() {
        return CycleResult{Status: CycleStatusFailed, ErrorClass: "fetch_failed"}
    }
    return CycleResult{Status: CycleStatusAborted, ErrorClass: "fetch_aborted"}
}
```

## Warnings

### WR-01: Missing `secret_paths` validation allows false-positive success on empty input

**Classification:** WARNING  
**File:** `internal/config/config.go:30-37`

**Issue:**
Config loader validates `provider_type` and `local_file_path`, but does not validate that `secret_paths` is non-empty and non-blank. This allows a misconfigured `seon.json` to pass validation and produce a successful no-op cycle.

**Fix:**
Reject empty/malformed secret path lists during config load.

```go
if len(cfg.SecretPaths) == 0 {
    return Config{}, fmt.Errorf("secret_paths must contain at least one entry")
}
for i, p := range cfg.SecretPaths {
    if strings.TrimSpace(p) == "" {
        return Config{}, fmt.Errorf("secret_paths[%d] must not be empty", i)
    }
}
```

### WR-02: Local provider ignores cancellation context

**Classification:** WARNING  
**File:** `internal/provider/local/local.go:18-27`

**Issue:**
`Fetch` accepts `context.Context` but discards it (`_ context.Context`). If context is already canceled, the provider still performs file read and JSON unmarshal work. This weakens timeout/cancellation responsiveness expected by per-path deadline orchestration.

**Fix:**
Honor context before and after expensive operations.

```go
func (p *Provider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
    if err := ctx.Err(); err != nil {
        return nil, err
    }

    b, err := os.ReadFile(p.filePath)
    if err != nil {
        return nil, fmt.Errorf("read local secrets file: %w", err)
    }

    if err := ctx.Err(); err != nil {
        return nil, err
    }
    // decode + lookup
}
```

### WR-03: Integration test stub has concurrent map access race (flaky under parallel fetch)

**Classification:** WARNING  
**File:** `tests/integration/atomic_sync_test.go:22-33`

**Issue:**
`scriptedProvider.results` is read/mutated without synchronization while coordinator fetches multiple paths concurrently. This can trigger data races and flaky tests (especially with `-race`), reducing test reliability.

**Fix:**
Protect shared stub state with a mutex (as already done in `internal/sync/coordinator_test.go`).

```go
type scriptedProvider struct {
    mu      sync.Mutex
    results map[string][]scriptedFetchResult
}

func (p *scriptedProvider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
    p.mu.Lock()
    seq, ok := p.results[resourceURI]
    if !ok || len(seq) == 0 {
        p.mu.Unlock()
        return nil, errors.New("missing scripted result")
    }
    current := seq[0]
    p.results[resourceURI] = seq[1:]
    p.mu.Unlock()
    // ...
}
```

---

_Reviewed: 2026-05-21T11:07:13Z_  
_Reviewer: gsd-code-reviewer (agent)_  
_Depth: standard_
