---
phase: 02-dual-vector-delivery-env-templates
reviewed: 2026-05-21T16:47:59Z
depth: standard
files_reviewed: 14
files_reviewed_list:
  - cmd/envfuse/main.go
  - internal/config/config.go
  - internal/exec/launcher.go
  - internal/exec/launcher_test.go
  - internal/fileio/atomic_writer.go
  - internal/fileio/path_guard.go
  - internal/inject/mapping.go
  - internal/inject/validation.go
  - internal/render/template.go
  - internal/state/store.go
  - internal/sync/coordinator.go
  - internal/sync/coordinator_test.go
  - internal/sync/model.go
  - tests/integration/dual_vector_delivery_test.go
findings:
  critical: 2
  warning: 5
  info: 0
  total: 7
status: issues_found
---

# Phase 02: Code Review Report

**Reviewed:** 2026-05-21T16:47:59Z
**Depth:** standard
**Files Reviewed:** 14
**Status:** issues_found

## Summary

Reviewed all Phase 02 changed source files (Go runtime + supporting tests) with focus on correctness, security, and maintainability. The implementation is close, but there are two correctness blockers in atomic commit and nil-store handling, plus robustness issues in config parsing and validation.

> Advisory note: this report is non-blocking in workflow behavior, but findings are still severity-classified for triage.

## Critical Issues

### CR-01: Dual-file write path can leave partial on-disk state

**Classification:** BLOCKER  
**File:** `internal/fileio/atomic_writer.go:53-59`  
**Issue:** `WriteAtomic` renames files one-by-one. If rename for file N fails, files `0..N-1` are already committed while the rest are not. This violates the phase’s atomic dual-vector guarantee and can leave mixed configuration state on disk.

**Fix:** Use a true transaction-like strategy for multi-file materialization (e.g., stage into a versioned directory and atomically switch a symlink, or implement rollback of already-renamed targets using backups).

```go
// sketch: stage all outputs under a new version dir
stageDir := filepath.Join(baseDir, ".envfuse-stage-<id>")
// write + fsync all files in stageDir
// atomically swap pointer (symlink/rename) from current -> stageDir
```

### CR-02: Coordinator can panic when store is nil

**Classification:** BLOCKER  
**File:** `internal/sync/coordinator.go:34-40,142-143,170-183`  
**Issue:** `NewCoordinatorWithStore` / `RunSingleCycleWithStoreFromConfig` accept `*state.Store` without nil guard. `runCycleWithVectors` unconditionally calls `c.store.StageCandidate(...)`, which panics if caller passed nil.

**Fix:** Validate inputs and fail fast with explicit error/result class instead of panicking.

```go
func NewCoordinatorWithStore(p provider.Provider, s *state.Store, perPathTimeout time.Duration) *Coordinator {
    if s == nil {
        s = state.NewStore()
    }
    return newCoordinatorWithClock(p, s, perPathTimeout, clock.RealClock{})
}
```

## Warnings

### WR-01: Template renderer can reject valid output containing literal `<no value>`

**Classification:** WARNING  
**File:** `internal/render/template.go:35-37`  
**Issue:** After using `missingkey=error`, code additionally scans rendered output for `"<no value>"`. This creates false failures when template content legitimately includes that string.

**Fix:** Remove the post-render string scan and rely on `missingkey=error` execution failures.

### WR-02: Path traversal check is both over-restrictive and ineffective

**Classification:** WARNING  
**File:** `internal/fileio/path_guard.go:18-20`  
**Issue:** `strings.Contains(clean, "..")` may reject valid absolute paths containing `..` in a segment name (e.g., `..data`), while not being a sound traversal defense after `filepath.Clean`.

**Fix:** Remove string-substring traversal check and enforce only canonical absolute paths + explicit allowlist comparison.

### WR-03: Config loader accepts trailing JSON tokens

**Classification:** WARNING  
**File:** `internal/config/config.go:42-44`  
**Issue:** Decoder reads one JSON object but does not verify EOF, so trailing non-whitespace JSON can be silently accepted.

**Fix:** After first decode, ensure no extra tokens remain.

```go
if err := dec.Decode(&cfg); err != nil {
    return Config{}, fmt.Errorf("decode config: %w", err)
}
var extra any
if err := dec.Decode(&extra); err != io.EOF {
    return Config{}, fmt.Errorf("decode config: trailing content")
}
```

### WR-04: `env_var` format is not validated

**Classification:** WARNING  
**File:** `internal/config/config.go:61-67`, `internal/inject/validation.go:12-19`  
**Issue:** `env_var` only checked for non-empty/duplication. Invalid names (`"A=B"`, leading digit, control chars) can produce malformed environment entries and undefined child behavior.

**Fix:** Validate against portable env var pattern (e.g., `^[A-Za-z_][A-Za-z0-9_]*$`) in config and/or inject validation.

### WR-05: Secrets can leak through inherited process environment by default

**Classification:** WARNING  
**File:** `cmd/envfuse/main.go:36`, `internal/exec/launcher.go:18-19`  
**Issue:** Child launch always merges `os.Environ()` with mapped secret payload. This may propagate unrelated sensitive parent env vars into the child, conflicting with the “declared-only/scoped” delivery intent.

**Fix:** Add allowlist-based inheritance (or opt-in) and default to minimal inherited env.

## Info

No info-only findings.

---

_Reviewed: 2026-05-21T16:47:59Z_  
_Reviewer: the agent (gsd-code-reviewer)_  
_Depth: standard_
