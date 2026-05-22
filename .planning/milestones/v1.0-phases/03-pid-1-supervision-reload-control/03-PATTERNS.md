# Phase 03: PID 1 Supervision & Reload Control - Pattern Map

**Mapped:** 2026-05-21  
**Files analyzed:** 10  
**Analogs found:** 10 / 10

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `cmd/envfuse/main.go` (modify) | controller (entrypoint) | request-response + event-driven | `cmd/envfuse/main.go` | exact |
| `internal/config/config.go` (modify) | config | CRUD/validation | `internal/config/config.go` | exact |
| `internal/sync/model.go` (modify) | model | transform | `internal/sync/model.go` | exact |
| `internal/sync/coordinator.go` (modify) | service (orchestrator) | batch + transform | `internal/sync/coordinator.go` | exact |
| `internal/supervisor/supervisor.go` (new) | service | event-driven | `internal/exec/launcher.go` + `internal/sync/coordinator.go` | partial (composed) |
| `internal/supervisor/model.go` (new) | model | transform | `internal/sync/model.go` | role-match |
| `internal/fingerprint/fingerprint.go` (new) | utility | transform | `internal/exec/launcher.go` (`sort.Strings` deterministic ordering) + `internal/state/store.go` | partial (composed) |
| `internal/supervisor/supervisor_test.go` (new) | test | event-driven | `internal/sync/coordinator_test.go` | role-match |
| `internal/fingerprint/fingerprint_test.go` (new) | test | transform | `internal/exec/launcher_test.go` | role-match |
| `tests/integration/pid1_supervision_reload_test.go` (new) | test | request-response + event-driven | `tests/integration/dual_vector_delivery_test.go` | role-match |

## Pattern Assignments

### `cmd/envfuse/main.go` (controller, request-response + event-driven)

**Analog:** `cmd/envfuse/main.go`

**Imports pattern** (lines 3-12):
```go
import (
	"context"
	"flag"
	"fmt"
	"os"

	launcher "envfuse/internal/exec"
	"envfuse/internal/state"
	synccycle "envfuse/internal/sync"
)
```

**Core control-flow pattern** (lines 18-33):
```go
flag.StringVar(&configPath, "config", "./seon.json", "path to seon config file")
flag.BoolVar(&once, "once", false, "run exactly one sync cycle and exit")
flag.Parse()

if !once {
	fmt.Fprintln(os.Stderr, "only -once mode is supported in phase 01")
	os.Exit(1)
}

store := state.NewStore()
result := synccycle.RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)
fmt.Println(string(result.Status))

if result.Status != synccycle.CycleStatusSuccess {
	os.Exit(1)
}
```

**Child launch handoff pattern** (lines 35-40):
```go
if len(flag.Args()) > 0 {
	if err := launcher.LaunchWithEnv(flag.Args(), os.Environ(), store.LastAppliedEnv()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

---

### `internal/config/config.go` (config, CRUD/validation)

**Analog:** `internal/config/config.go`

**Imports + strict decode pattern** (lines 3-7, 31-44):
```go
import (
	"encoding/json"
	"fmt"
	"os"
)

f, err := os.Open(path)
...
dec := json.NewDecoder(f)
dec.DisallowUnknownFields()

var cfg Config
if err := dec.Decode(&cfg); err != nil {
	return Config{}, fmt.Errorf("decode config: %w", err)
}
```

**Field-level validation pattern** (lines 46-84):
```go
if cfg.ProviderType == "" {
	return Config{}, fmt.Errorf("provider_type is required")
}
...
for i, mapping := range cfg.EnvMappings {
	if mapping.Path == "" { ... }
	if mapping.Key == "" { ... }
	if mapping.EnvVar == "" { ... }
}
...
for i, tpl := range cfg.Templates {
	if tpl.OutputPath == "" { ... }
	hasInline := tpl.Content != ""
	hasFile := tpl.TemplatePath != ""
	if hasInline == hasFile {
		return Config{}, fmt.Errorf("templates[%d] must set exactly one of content or template_path", i)
	}
}
```

Use this same style for `shutdown_timeout` and `reload_cooldown`: strict JSON schema + explicit required/default/constraint checks.

---

### `internal/exec/launcher.go` (service, request-response)

**Analog:** `internal/exec/launcher.go`

**Imports pattern** (lines 3-10):
```go
import (
	"fmt"
	"os"
	osexec "os/exec"
	"sort"

	"envfuse/internal/state"
)
```

**Process setup pattern** (lines 17-25):
```go
cmd := osexec.Command(command[0], command[1:]...)
cmd.Env = mergeEnv(inherited, envPayload)
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
cmd.Stdin = os.Stdin

if err := cmd.Run(); err != nil {
	return fmt.Errorf("launch child: %w", err)
}
```

**Deterministic env merge pattern** (lines 37-67):
```go
merged := make(map[string]string, len(inherited)+len(envPayload))
...
for k, v := range envPayload {
	merged[k] = v
}

keys := make([]string, 0, len(merged))
for k := range merged {
	keys = append(keys, k)
}
sort.Strings(keys)

out := make([]string, 0, len(keys))
for _, k := range keys {
	out = append(out, fmt.Sprintf("%s=%s", k, merged[k]))
}
```

For Phase 3, preserve this exact env canonicalization while switching launch path from `Run()` to `Start()/Wait()` in supervisor-managed lifecycle.

---

### `internal/sync/model.go` (model, transform)

**Analog:** `internal/sync/model.go`

**Status + taxonomy model pattern** (lines 3-17):
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
	AppliedEnv   []string
	RenderedFiles []string
}
```

Mirror this style for supervision reason taxonomy (`config_changed`, `child_exited`, `startup_failed`, `forced_kill`) as typed constants.

---

### `internal/sync/coordinator.go` (service, batch + transform)

**Analog:** `internal/sync/coordinator.go`

**Imports + dependency injection pattern** (lines 3-21, 23-40):
```go
import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
...
	"golang.org/x/sync/errgroup"
)

type Coordinator struct {
	provider       provider.Provider
	store          *state.Store
	perPathTimeout time.Duration
	clock          clock.Clock
}
```

**Concurrency + fail-fast pattern** (lines 75-99):
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

**All-or-nothing commit gate pattern** (lines 141-153):
```go
// Stage and commit applied snapshot only after full-batch success, per SYNC-04.
c.store.StageCandidate(candidate, envPayload, renderedPayload)
c.store.CommitCandidate()

sort.Strings(fetched)
...
return CycleResult{Status: CycleStatusSuccess, FetchedPaths: fetched, AppliedEnv: appliedEnv, RenderedFiles: renderedFiles}
```

This is the integration anchor for restart gating: only evaluate restart on successful committed cycle.

---

### `internal/supervisor/supervisor.go` (new service, event-driven)

**Primary analogs:**
- Process lifecycle: `internal/exec/launcher.go` (lines 17-25)
- Success/commit gating: `internal/sync/coordinator.go` (lines 141-153)
- Entrypoint error/exit behavior: `cmd/envfuse/main.go` (lines 31-39)

**Copy process wiring skeleton from launcher**:
```go
cmd := osexec.Command(command[0], command[1:]...)
cmd.Env = mergeEnv(inherited, envPayload)
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
cmd.Stdin = os.Stdin
```

**Copy explicit result->exit gate from main**:
```go
if result.Status != synccycle.CycleStatusSuccess {
	os.Exit(1)
}
```

**Copy success-only transition principle from coordinator**:
```go
c.store.StageCandidate(candidate, envPayload, renderedPayload)
c.store.CommitCandidate()
```

Apply these together for state machine edges: start child -> watch signals/cycle -> stop with timeout -> kill on expiry -> restart once cooldown allows.

---

### `internal/supervisor/model.go` (new model, transform)

**Analog:** `internal/sync/model.go` (lines 3-17)

Use the same structure:
- `type Reason string`
- `const (...)`
- result struct with explicit fields (reason, exit code, forced kill bool, restart attempted bool).

Pattern to copy:
```go
type CycleStatus string
const (...)
type CycleResult struct { ... }
```

---

### `internal/fingerprint/fingerprint.go` (new utility, transform)

**Primary analogs:**
- Deterministic ordering in `internal/exec/launcher.go` (lines 57-66)
- Stable snapshot copies in `internal/state/store.go` (lines 67-95)
- Sorted result surfaces in `internal/sync/coordinator.go` (lines 145-150)

**Ordering pattern**:
```go
keys := make([]string, 0, len(merged))
for k := range merged { keys = append(keys, k) }
sort.Strings(keys)
```

**Immutable-copy mindset for hash input**:
```go
buf := make([]byte, len(v))
copy(buf, v)
out[k] = buf
```

Use this to build canonical bytes from:
1) sorted env keys + values,
2) sorted rendered output paths + bytes,
then hash (SHA-256 per research decisions).

---

### `internal/supervisor/supervisor_test.go` (new test, event-driven)

**Analog:** `internal/sync/coordinator_test.go`

**Parallel test + scripted stub pattern** (lines 50-87, 95-128):
```go
func TestRunCycle_FetchesDistinctPathsConcurrently(t *testing.T) {
	t.Parallel()
	...
}

type scriptedUnitProvider struct {
	mu      sync.Mutex
	results map[string][]scriptedUnitResult
}
```

**Error-class assertions pattern** (lines 205-210, 301-309):
```go
if result.Status != CycleStatusFailed { ... }
if result.ErrorClass != "env_mapping_unresolved" { ... }
```

Use this exact style for supervisor reason assertions (`forced_kill`, `child_exited`, etc.) and timeout behavior.

---

### `internal/fingerprint/fingerprint_test.go` (new test, transform)

**Analog:** `internal/exec/launcher_test.go`

**Tableless deterministic assertion style** (lines 12-35):
```go
inherited := []string{"HOME=/tmp/home", "APP_API_KEY=old", "UNCHANGED=yes"}
payload := map[string]string{"APP_API_KEY": "new", "DB_PASSWORD": "secret"}

merged := mergeEnv(inherited, payload)
...
if vals["APP_API_KEY"] != "new" { ... }
```

**State-handoff test style** (lines 41-55):
```go
store.StageCandidate(...)
store.CommitCandidate()
...
err := RestartWithCommittedEnv(...)
if err != nil { t.Fatalf(...) }
```

Use same style to assert hash stability (same logical state => same fingerprint) and sensitivity (effective changes => different fingerprint).

---

### `tests/integration/pid1_supervision_reload_test.go` (new integration test, request-response + event-driven)

**Analog:** `tests/integration/dual_vector_delivery_test.go`

**CLI integration harness pattern** (lines 332-337):
```go
cmd := exec.Command("go", "run", "./cmd/envfuse", "-config", configPath, "-once", "--", "/bin/sh", "-c", ...)
cmd.Dir = filepath.Join("..", "..")
output, err := cmd.CombinedOutput()
if err != nil {
	t.Fatalf("envfuse run failed: %v\noutput=%s", err, string(output))
}
```

**Fixture/config JSON pattern** (lines 316-323):
```go
configJSON := `{
	"provider_type": "local",
	"local_file_path": "...",
	"secret_paths": ["app/base"],
	"env_mappings": [...]
}`
```

**Last-known-good verification style** (lines 457-463):
```go
rendered, err := os.ReadFile(renderedPath)
...
if got := strings.TrimSpace(string(rendered)); got != "LAST_GOOD=v1" {
	t.Fatalf(...)
}
```

Adapt to verify: signal forwarding, graceful timeout then force kill, cooldown coalescing, and exit-code propagation.

## Shared Patterns

### Error classification and explicit outcome taxonomy
**Source:** `internal/sync/coordinator.go` + `internal/sync/model.go`  
**Apply to:** supervisor runtime + new models/tests

```go
return CycleResult{Status: CycleStatusFailed, ErrorClass: "fetch_failed"}
...
type CycleResult struct {
	Status       CycleStatus
	ErrorClass   string
}
```

### Deterministic ordering before externally visible decisions
**Source:** `internal/exec/launcher.go` (sort env), `internal/sync/coordinator.go` (sort outputs)  
**Apply to:** fingerprint canonicalization + restart gating

```go
sort.Strings(keys)
...
sort.Strings(fetched)
sort.Strings(renderedFiles)
```

### Success-only commit gate
**Source:** `internal/sync/coordinator.go` + `internal/state/store.go`  
**Apply to:** restart decisions should use committed/successful cycle transitions only

```go
c.store.StageCandidate(candidate, envPayload, renderedPayload)
c.store.CommitCandidate()
```

### Strict config validation with unknown-field rejection
**Source:** `internal/config/config.go`  
**Apply to:** `shutdown_timeout`, `reload_cooldown` additions

```go
dec := json.NewDecoder(f)
dec.DisallowUnknownFields()
```

## No Analog Found

All scoped files have at least a role-match analog. No completely unmatched file patterns were found.

## Metadata

**Analog search scope:** `cmd/envfuse`, `internal/config`, `internal/exec`, `internal/sync`, `internal/state`, `internal/fileio`, `tests/integration`  
**Files scanned:** 11 Go files (+ 2 phase docs + AGENTS)  
**Pattern extraction date:** 2026-05-21
