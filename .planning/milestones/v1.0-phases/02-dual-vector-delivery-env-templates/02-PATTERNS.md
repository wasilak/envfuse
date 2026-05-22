# Phase 2: Dual-Vector Delivery (Env + Templates) - Pattern Map

**Mapped:** 2026-05-21  
**Files analyzed:** 11  
**Analogs found:** 11 / 11

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/config/config.go` | config | transform | `internal/config/config.go` | exact |
| `internal/sync/coordinator.go` | service | batch | `internal/sync/coordinator.go` | exact |
| `internal/sync/model.go` | model | transform | `internal/sync/model.go` | exact |
| `internal/state/store.go` | service | CRUD | `internal/state/store.go` | exact |
| `internal/inject/mapping.go` | service | transform | `internal/config/config.go` | role-match |
| `internal/inject/validation.go` | utility | transform | `internal/config/config.go` | dataflow-match |
| `internal/render/template.go` | service | transform | `internal/provider/local/local.go` | dataflow-match |
| `internal/fileio/atomic_writer.go` | utility | file-I/O | `internal/state/store.go` | partial |
| `internal/fileio/path_guard.go` | utility | transform | `internal/config/config.go` | dataflow-match |
| `internal/sync/coordinator_test.go` | test | batch | `internal/sync/coordinator_test.go` | exact |
| `tests/integration/dual_vector_delivery_test.go` | test | batch | `tests/integration/atomic_sync_test.go` | role-match |

## Pattern Assignments

### `internal/config/config.go` (config, transform)

**Analog:** `internal/config/config.go`

**Imports pattern** (lines 3-7):
```go
import (
	"encoding/json"
	"fmt"
	"os"
)
```

**Strict decode + validation pattern** (lines 22-35):
```go
dec := json.NewDecoder(f)
dec.DisallowUnknownFields()

var cfg Config
if err := dec.Decode(&cfg); err != nil {
	return Config{}, fmt.Errorf("decode config: %w", err)
}

if cfg.ProviderType == "" {
	return Config{}, fmt.Errorf("provider_type is required")
}
```

**How to copy for Phase 2:** keep `DisallowUnknownFields`, add new schema sections for env mappings and template specs, and keep explicit field-by-field validation errors.

---

### `internal/sync/coordinator.go` (service, batch)

**Analog:** `internal/sync/coordinator.go`

**Imports and dependency wiring pattern** (lines 3-17, 19-24):
```go
import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"envfuse/internal/clock"
	"envfuse/internal/config"
	"envfuse/internal/provider"
	"envfuse/internal/state"
	"golang.org/x/sync/errgroup"
)

type Coordinator struct {
	provider       provider.Provider
	store          *state.Store
	perPathTimeout time.Duration
	clock          clock.Clock
}
```

**Concurrent fetch + cancellation core pattern** (lines 52-76):
```go
g, gctx := errgroup.WithContext(ctx)
for _, path := range uniq {
	path := path
	g.Go(func() error {
		pathCtx, cancel := context.WithTimeout(gctx, c.perPathTimeout)
		defer cancel()

		data, err := c.provider.Fetch(pathCtx, path)
		if err != nil {
			return fmt.Errorf("fetch path %s: %w", path, err)
		}
		...
		return nil
	})
}

if err := g.Wait(); err != nil {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return CycleResult{Status: CycleStatusAborted, ErrorClass: "fetch_aborted"}
	}
	return CycleResult{Status: CycleStatusFailed, ErrorClass: "fetch_failed"}
}
```

**Commit gate pattern** (lines 78-83):
```go
c.store.StageCandidate(candidate)
c.store.CommitCandidate()

sort.Strings(fetched)
return CycleResult{Status: CycleStatusSuccess, FetchedPaths: fetched}
```

**How to copy for Phase 2:** keep this exact transaction shape, but stage dual outputs (`env plan` + `rendered files`) before a single commit gate.

---

### `internal/state/store.go` (service, CRUD)

**Analog:** `internal/state/store.go`

**Thread-safe stage/commit pattern** (lines 17-35):
```go
func (s *Store) StageCandidate(candidate map[string]map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidate = cloneSnapshot(candidate)
}

func (s *Store) CommitCandidate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.candidate == nil {
		return
	}

	s.lastKnownGood = cloneSnapshot(s.candidate)
	s.candidate = nil
}
```

**Snapshot copy safety pattern** (lines 43-52):
```go
func cloneSnapshot(in map[string]map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(in))
	for path, kv := range in {
		copyKV := make(map[string]any, len(kv))
		for k, v := range kv {
			copyKV[k] = v
		}
		out[path] = copyKV
	}
	return out
}
```

**How to copy for Phase 2:** extend store shape to hold both env payload and file payload in candidate/committed snapshots.

---

### `internal/inject/mapping.go` (service, transform)

**Analog:** `internal/config/config.go`

**Validation error style pattern** (lines 27, 31, 34):
```go
return Config{}, fmt.Errorf("decode config: %w", err)
return Config{}, fmt.Errorf("provider_type is required")
return Config{}, fmt.Errorf("local_file_path is required for local provider")
```

**How to copy for Phase 2:** implement mapping resolution errors with the same explicit, deterministic messages (e.g., missing `path:key`, missing target env var).

---

### `internal/render/template.go` (service, transform)

**Analog:** `internal/provider/local/local.go`

**Pure transformation + defensive copy pattern** (lines 24-39):
```go
store := map[string]map[string]any{}
if err := json.Unmarshal(b, &store); err != nil {
	return nil, fmt.Errorf("decode local secrets file: %w", err)
}

v, ok := store[resourceURI]
if !ok {
	return nil, fmt.Errorf("resource path not found: %s", resourceURI)
}

out := make(map[string]any, len(v))
for k, val := range v {
	out[k] = val
}
```

**How to copy for Phase 2:** keep transform pure (input snapshot -> output bytes), return wrapped errors, avoid mutating source map.

---

### `internal/fileio/atomic_writer.go` (utility, file-I/O)

**Analog:** `internal/state/store.go`

**Two-phase apply contract pattern** (lines 17-35):
```go
// Stage first
s.candidate = cloneSnapshot(candidate)

// Commit only when candidate is complete
if s.candidate == nil {
	return
}
s.lastKnownGood = cloneSnapshot(s.candidate)
s.candidate = nil
```

**How to copy for Phase 2:** keep the same stage/commit semantics in file writing (`tmp write -> sync -> rename`) so file outputs join the same commit boundary as env injection.

---

### `internal/fileio/path_guard.go` (utility, transform)

**Analog:** `internal/config/config.go`

**Guard clause pattern** (lines 30-35):
```go
if cfg.ProviderType == "" {
	return Config{}, fmt.Errorf("provider_type is required")
}
if cfg.ProviderType == "local" && cfg.LocalFilePath == "" {
	return Config{}, fmt.Errorf("local_file_path is required for local provider")
}
```

**How to copy for Phase 2:** implement explicit path guard checks as sequential guard clauses with immediate errors.

---

### `internal/sync/model.go` (model, transform)

**Analog:** `internal/sync/model.go`

**Typed status/result pattern** (lines 3-15):
```go
type CycleStatus string

const (
	CycleStatusSuccess CycleStatus = "success"
	CycleStatusFailed  CycleStatus = "failed"
	CycleStatusAborted CycleStatus = "aborted"
)

type CycleResult struct {
	Status       CycleStatus
	FetchedPaths []string
	ErrorClass   string
}
```

**How to copy for Phase 2:** keep string status enum + `ErrorClass`; add dual-vector metadata fields rather than changing status semantics.

---

### `internal/sync/coordinator_test.go` (test, batch)

**Analog:** `internal/sync/coordinator_test.go`

**Concurrency test structure pattern** (lines 47-84):
```go
resultCh := make(chan CycleResult, 1)
go func() {
	resultCh <- coord.RunCycle(ctx, []string{"app/base", "db/primary"})
}()

// Observe both workers start before release
for len(seen) < 2 { ... }
close(p.releaseC)
```

**Failure invariant pattern** (lines 145-151, 172-178):
```go
if result.Status != CycleStatusFailed { ... }
if len(store.LastKnownGood()) != 0 {
	t.Fatalf("expected no commit on failed cycle")
}
```

**How to copy for Phase 2:** preserve invariant assertions: any env/template validation failure must produce non-success status and no commit.

---

### `tests/integration/dual_vector_delivery_test.go` (test, batch)

**Analog:** `tests/integration/atomic_sync_test.go`

**Fixture + temp filesystem setup pattern** (lines 57-76 in analog):
```go
tmpDir := t.TempDir()
secretsPath := filepath.Join(tmpDir, "secrets.json")
configPath := filepath.Join(tmpDir, "seon.json")

if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil { ... }
if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil { ... }
```

**Cycle + invariant assertion pattern** (lines 139-157, 189-197 in analog):
```go
first := coordinator.RunCycle(context.Background(), []string{"app/base", "db/primary"})
if first.Status != sync.CycleStatusSuccess { ... }

second := coordinator.RunCycle(context.Background(), []string{"app/base", "db/primary"})
if second.Status != sync.CycleStatusFailed { ... }

snapshotB := store.LastKnownGood()
if snapshotB["app/base"]["VERSION"] != "v1" {
	t.Fatalf("expected last-known-good to remain v1 after failed cycle")
}
```

**How to copy for Phase 2:** use the same integration style to prove dual-vector behavior specifically:
- success path: env payload + rendered files both materialize from one cycle,
- failure path: any env/template/path failure yields non-success status and zero partial commit,
- regression invariant: last-known-good env/files remain unchanged after failed cycles.

## Shared Patterns

### Transaction Boundary (all-or-nothing)
**Source:** `internal/sync/coordinator.go` lines 78-83 + `internal/state/store.go` lines 17-35  
**Apply to:** `internal/sync/coordinator.go`, `internal/state/store.go`, `internal/fileio/atomic_writer.go`, `internal/inject/*`, `internal/render/*`
```go
// stage first
c.store.StageCandidate(candidate)
// commit once full cycle succeeds
c.store.CommitCandidate()
```

### Error Classification + Deterministic Result
**Source:** `internal/sync/coordinator.go` lines 71-76 + `internal/sync/model.go` lines 3-15  
**Apply to:** sync orchestration + render/inject validation paths
```go
if err := g.Wait(); err != nil {
	return CycleResult{Status: CycleStatusFailed, ErrorClass: "fetch_failed"}
}
```

### Strict Input Validation
**Source:** `internal/config/config.go` lines 22-35  
**Apply to:** config extension, mapping validation, path guard checks
```go
dec := json.NewDecoder(f)
dec.DisallowUnknownFields()
```

### Concurrent Fetch Orchestration
**Source:** `internal/sync/coordinator.go` lines 52-69  
**Apply to:** coordinator fetch stage before env/template planning
```go
g, gctx := errgroup.WithContext(ctx)
...
pathCtx, cancel := context.WithTimeout(gctx, c.perPathTimeout)
```

### Secret Handling Discipline
**Source:** `AGENTS.md` lines 14-16  
**Apply to:** all new/modified phase files
```text
Never apply partial updates on batch failure.
Secrets must remain in process memory or intended target files, not logs/global env.
```

## No Analog Found

Files with only partial analog coverage (planner should combine repo patterns + `02-RESEARCH.md`):

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `internal/render/template.go` | service | transform | No existing in-repo template rendering implementation yet (`text/template`, `missingkey=error` is new in this phase). |
| `internal/fileio/atomic_writer.go` | utility | file-I/O | No existing on-disk atomic writer helper exists yet; only in-memory stage/commit pattern exists in `state.Store`. |
| `internal/fileio/path_guard.go` | utility | transform | No existing path safety helper exists yet (`filepath.Clean`/`filepath.IsLocal` not yet implemented). |

## Metadata

**Analog search scope:** `/Users/piotrek/git/envfuse` (`cmd/`, `internal/`, `tests/`)  
**Files scanned:** 10 Go source/test files  
**Pattern extraction date:** 2026-05-21
