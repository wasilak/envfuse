# Phase 2: Dual-Vector Delivery (Env + Templates) - Research

**Researched:** 2026-05-21 [VERIFIED: system date]  
**Domain:** Go runtime delivery of environment variables + rendered template files in one atomic sync cycle [VERIFIED: .planning/ROADMAP.md]  
**Confidence:** HIGH

## User Constraints (from CONTEXT.md)

No `*-CONTEXT.md` exists for this phase directory, so there are no locked discuss-phase decisions to copy yet. [VERIFIED: .planning/phases/02-dual-vector-delivery-env-templates/*-CONTEXT.md]

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INJT-01 | Operator can map provider keys to environment variables using path:key mapping syntax | Add explicit mapping schema (`path`, `key`, `env_var`) plus validation against fetched batch snapshot before apply. [VERIFIED: .planning/REQUIREMENTS.md][VERIFIED: internal/sync/coordinator.go] |
| INJT-02 | Operator can render template files from fetched secrets before child process start | Use `text/template` rendering with pre-parse + per-cycle execute, write to temp, fsync, rename to target file. [CITED: https://pkg.go.dev/text/template][CITED: https://pkg.go.dev/os#WriteFile][CITED: https://pkg.go.dev/os#Rename] |
| INJT-03 | Runtime can deliver both environment variables and rendered files in the same sync cycle | Extend cycle transaction to stage both env payload and file outputs, then commit both together after full validation. [VERIFIED: .planning/REQUIREMENTS.md][VERIFIED: .planning/ROADMAP.md] |
| INJT-04 | Runtime fails startup if required template keys are missing | Use `Template.Option("missingkey=error")` and fail startup cycle on template exec error. [CITED: https://pkg.go.dev/text/template#Template.Option] |
| SECU-03 | Runtime only injects secrets into child environment and declared target files | Enforce allowlist of env names and output file paths from config; do not write anywhere else. [VERIFIED: .planning/REQUIREMENTS.md][VERIFIED: AGENTS.md] |
</phase_requirements>

## Summary

Phase 2 should extend the existing Phase 1 batch engine into a **dual-output transaction**: one candidate environment payload plus one candidate file-render set, both derived from the same fetched secret snapshot, both validated before commit. [VERIFIED: internal/sync/coordinator.go][VERIFIED: .planning/ROADMAP.md]  
The planning-critical boundary is not “templating” alone; it is preserving Phase 1 no-partial-commit semantics while adding two output channels. [VERIFIED: internal/state/store.go][VERIFIED: AGENTS.md]

The standard Go stack for this phase remains stdlib-first: `text/template` for rendering with strict missing-key behavior, `os/exec` for child env handoff, and `filepath` lexical guards for target-path safety. [CITED: https://pkg.go.dev/text/template#Template.Option][CITED: https://pkg.go.dev/os/exec#Cmd][CITED: https://pkg.go.dev/path/filepath#IsLocal]  
Avoid sidecars/shell glue as implementation strategy in this phase; this repo’s constraints require a single in-process deterministic lifecycle. [VERIFIED: AGENTS.md][CITED: https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/process-supervisor]

**Primary recommendation:** Implement a `DualVectorApplyPlan` (env plan + file plan) built from one fetched snapshot, validated completely (mapping existence, template keys, path allowlist), then committed atomically as one cycle result. [VERIFIED: .planning/ROADMAP.md][VERIFIED: internal/sync/coordinator.go]

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `path:key -> env` mapping resolution | API / Backend runtime | — | Mapping resolution is runtime business logic, not shell/child responsibility. [VERIFIED: INJT-01 in .planning/REQUIREMENTS.md] |
| Template rendering from fetched secret snapshot | API / Backend runtime | Database / Storage (filesystem) | Rendering logic lives in runtime; output artifact persists as declared files. [VERIFIED: INJT-02 in .planning/REQUIREMENTS.md] |
| Combined env+file transaction gate | API / Backend runtime | — | Atomic all-or-nothing semantics are coordinator-owned. [VERIFIED: INJT-03 and SYNC-04 in .planning/REQUIREMENTS.md] |
| Startup fail-fast on missing template keys | API / Backend runtime | — | Validation decision belongs to runtime startup path before child launch. [VERIFIED: INJT-04 in .planning/REQUIREMENTS.md] |
| Secret materialization boundary enforcement | API / Backend runtime | Database / Storage (filesystem) | Runtime must constrain writes to declared env vars + declared files only. [VERIFIED: SECU-03 in .planning/REQUIREMENTS.md] |

## Project Constraints (from AGENTS.md)

- Implement in Go and align with Linux process/signal model. [VERIFIED: AGENTS.md]  
- Never apply partial updates on batch failure. [VERIFIED: AGENTS.md]  
- Keep secrets in process memory or intended target files only; do not leak to logs/global env. [VERIFIED: AGENTS.md]  
- Keep runtime lightweight and avoid unnecessary dependencies. [VERIFIED: AGENTS.md]  
- Preserve deterministic startup/config validation behavior. [VERIFIED: AGENTS.md]

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go toolchain | 1.26.3 | Runtime implementation baseline | Already in local environment and current stdlib docs reference this version stream. [VERIFIED: `go version`][CITED: https://pkg.go.dev/text/template] |
| `text/template` (stdlib) | go1.26.3 stdlib docs | Template rendering with strict missing-key mode | Native, audited stdlib; supports `missingkey=error` fail-fast semantics. [CITED: https://pkg.go.dev/text/template#Template.Option] |
| `os/exec` (stdlib) | go1.26.3 stdlib docs | Child process env payload delivery | `Cmd.Env` explicitly controls child environment and duplicate-key resolution. [CITED: https://pkg.go.dev/os/exec#Cmd] |
| `path/filepath` (stdlib) | go1.26.3 stdlib docs | Output-path lexical safety checks | `IsLocal`, `Clean`, `Join` reduce traversal and path-normalization bugs. [CITED: https://pkg.go.dev/path/filepath#IsLocal] |
| `golang.org/x/sync/errgroup` | v0.20.0 (published 2026-02-23) | Reuse Phase 1 concurrent fetch orchestration | Existing coordinator already uses it; no reason to replace for Phase 2. [VERIFIED: `go list -m -json golang.org/x/sync@latest`][VERIFIED: internal/sync/coordinator.go] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/stretchr/testify` | v1.11.1 (published 2025-08-27) | Assertions for mapping/template validation tests | Use for unit tests around INJT-01..04 error classes and edge conditions. [VERIFIED: `go list -m -json github.com/stretchr/testify@latest`] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| In-process dual-vector engine | Vault Agent process-supervisor mode | Vault Agent exec mode supports `env_template` but is incompatible with regular file `template` entries in that mode; does not satisfy env+file single-cycle goal directly. [CITED: https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/process-supervisor] |
| `text/template` strict mode | Custom template parser/renderer | Reinvents parser semantics and missing-key behavior with higher defect risk. [CITED: https://pkg.go.dev/text/template#Template.Option] |

**Installation (Go modules):**
```bash
go get golang.org/x/sync@v0.20.0
go get github.com/stretchr/testify@v1.11.1
```

**Version verification used in this research:**
```bash
go version
go list -m -json golang.org/x/sync@latest
go list -m -json github.com/stretchr/testify@latest
```

## Architecture Patterns

### System Architecture Diagram

```text
seon.json
  |  (env mappings + template targets + secret paths)
  v
[Config + Schema Validation]
  |
  v
[Fetch Batch (existing Phase 1 errgroup fan-out)]
  |
  v
[Unified Secret Snapshot]
  |-------------------------------|
  v                               v
[Env Mapping Resolver]      [Template Renderer]
  |                               |
  v                               v
[Candidate Child Env]       [Candidate File Outputs]
  |___________________   ____________________|
                      \ /
               [Dual-Vector Validation Gate]
          (missing keys, path allowlist, target declarations)
                         |
              success    |    fail
                 v       |       v
       [Atomic Commit: env+files]   [Reject cycle, keep last known good]
                         |
                         v
                  [Phase 3 supervisor consumes committed env/files]
```

### Recommended Project Structure
```text
internal/
├── config/              # extend schema: env mappings + template specs
├── sync/                # coordinator + dual-vector candidate planner
├── inject/              # env mapping resolution + validation
├── render/              # template parse/execute + output planning
├── state/               # last-known-good + staged candidate snapshots
└── fileio/              # temp-write/fsync/rename helpers + path guards
```

### Pattern 1: Strict Template Execution for Startup Fail-Fast
**What:** Parse templates once, execute with `missingkey=error`, treat any execution error as cycle failure.  
**When to use:** Every startup cycle and every reload cycle that re-renders templates.  
**Example:**
```go
// Source: https://pkg.go.dev/text/template#Template.Option
tpl, err := template.New("app-config").Option("missingkey=error").Parse(tplText)
if err != nil { return err }
if err := tpl.Execute(&buf, values); err != nil {
    return fmt.Errorf("template render failed: %w", err)
}
```

### Pattern 2: Child-Only Environment Injection Boundary
**What:** Build explicit env slice for child process and pass via `exec.Cmd.Env`; do not export to global host env.  
**When to use:** Any child launch/restart path in Phase 3+, but the env payload contract is defined in Phase 2.  
**Example:**
```go
// Source: https://pkg.go.dev/os/exec#Cmd
cmd := exec.Command(binary, args...)
cmd.Env = append(os.Environ(), "DB_PASSWORD=...", "API_TOKEN=...")
```

### Pattern 3: Declared-Target File Safety Guard
**What:** Validate each rendered target path against declared allowlist + lexical local-path constraints.  
**When to use:** Before any temp-file write in each cycle.  
**Example:**
```go
// Source: https://pkg.go.dev/path/filepath#IsLocal
clean := filepath.Clean(target)
if !filepath.IsLocal(clean) {
    return fmt.Errorf("disallowed target path: %s", target)
}
```

### Anti-Patterns to Avoid
- **Silent template key fallback (`<no value>`):** default mode can hide missing required data; use `missingkey=error`. [CITED: https://pkg.go.dev/text/template#Template.Option]  
- **Two-phase apply (env first, files second):** violates INJT-03 atomic dual-vector requirement. [VERIFIED: .planning/REQUIREMENTS.md]  
- **Writing outside declared target set:** violates SECU-03 boundary requirement. [VERIFIED: .planning/REQUIREMENTS.md]  
- **Shell-based env injection scripts:** undermines deterministic lifecycle and security controls expected by project constraints. [VERIFIED: AGENTS.md]

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Template parser and missing-key semantics | Custom DSL/parser | `text/template` + `Option("missingkey=error")` | Built-in, maintained behavior; predictable error semantics. [CITED: https://pkg.go.dev/text/template#Template.Option] |
| Child env delivery mechanism | Global `os.Setenv` mutation strategy | `exec.Cmd.Env` explicit per-process env | Keeps secrets scoped to child process launch path. [CITED: https://pkg.go.dev/os/exec#Cmd] |
| Path safety normalization | Manual string prefix checks | `filepath.Clean` + `filepath.IsLocal` | Avoids brittle path traversal checks. [CITED: https://pkg.go.dev/path/filepath#Clean][CITED: https://pkg.go.dev/path/filepath#IsLocal] |

**Key insight:** The hard part in this phase is transaction boundaries and validation ordering, not rendering syntax. Reuse stdlib for primitives and spend design effort on cycle-state correctness. [VERIFIED: .planning/REQUIREMENTS.md][VERIFIED: internal/sync/coordinator.go]

## Common Pitfalls

### Pitfall 1: Missing-key template failures discovered too late
**What goes wrong:** Runtime starts child with partially rendered/invalid file content. [ASSUMED]  
**Why it happens:** Default template missing-key behavior is non-failing (`<no value>`). [CITED: https://pkg.go.dev/text/template#Template.Option]  
**How to avoid:** Enforce `missingkey=error` and fail before apply/child start. [CITED: https://pkg.go.dev/text/template#Template.Option]  
**Warning signs:** Render output contains `<no value>` literals. [CITED: https://pkg.go.dev/text/template#Template.Option]

### Pitfall 2: Env and file paths validated against different snapshots
**What goes wrong:** Env reflects one fetch outcome while files reflect another. [ASSUMED]  
**Why it happens:** Separate fetch or transform pipelines for env vs template stages. [ASSUMED]  
**How to avoid:** Build both plans from one immutable per-cycle secret snapshot. [VERIFIED: INJT-03 in .planning/REQUIREMENTS.md]  
**Warning signs:** Same secret version mismatch across env and rendered files in one cycle. [ASSUMED]

### Pitfall 3: Path escape through output target config
**What goes wrong:** Secret file written to unintended location. [ASSUMED]  
**Why it happens:** Naive path concatenation without lexical checks. [ASSUMED]  
**How to avoid:** `filepath.Clean` + `filepath.IsLocal` + declared target allowlist check. [CITED: https://pkg.go.dev/path/filepath#Clean][CITED: https://pkg.go.dev/path/filepath#IsLocal][VERIFIED: SECU-03 in .planning/REQUIREMENTS.md]  
**Warning signs:** Targets containing `..` segments or absolute paths pass validation. [ASSUMED]

## Code Examples

Verified patterns from official sources:

### Strict missing-key template execution
```go
// Source: https://pkg.go.dev/text/template#Template.Option
tpl := template.Must(template.New("cfg").Option("missingkey=error").Parse(tplText))
if err := tpl.Execute(out, data); err != nil {
    return fmt.Errorf("render failed: %w", err)
}
```

### Child-scoped environment assignment
```go
// Source: https://pkg.go.dev/os/exec#Cmd
cmd := exec.Command("./my-app")
cmd.Env = append(os.Environ(), "FOO=actual_value")
```

### Lexical local-path guard
```go
// Source: https://pkg.go.dev/path/filepath#IsLocal
candidate := filepath.Clean(target)
if !filepath.IsLocal(candidate) {
    return fmt.Errorf("invalid output target: %s", target)
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Shell entrypoint composition (`envsubst`, ad-hoc scripts) | In-process deterministic render+inject lifecycle in Go runtime | Modern container hardening practice; reflected in project constraints and stack guidance. [VERIFIED: AGENTS.md] | Better determinism, fewer signal/env edge cases. [VERIFIED: AGENTS.md] |
| Vault Agent as direct env+file dual-delivery solution | Vault Agent process-supervisor mode focuses on env templates and is incompatible with regular file template entries in that mode | Documented in current Vault docs for process supervisor mode. [CITED: https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/process-supervisor] | Supports project decision to implement native dual-vector engine. [VERIFIED: AGENTS.md] |

**Deprecated/outdated:**
- Treating default template missing-key behavior as “strict” is incorrect; default is non-failing unless set to error mode. [CITED: https://pkg.go.dev/text/template#Template.Option]

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Separate env/file transform pipelines are a likely implementation risk in this codebase | Common Pitfalls 2 | Medium — planner may over-allocate validation tasks if implementation naturally avoids split snapshots. |
| A2 | Path-escape and mismatch warning signs listed will appear in logs/tests as described | Common Pitfalls 2-3 | Low — diagnostics wording may differ by implementation. |

## Open Questions (RESOLVED)

1. **Should template functions beyond stdlib defaults be allowed in v1? — RESOLVED: No (v1 disallows custom funcs).**
   - What we know: `text/template` supports custom function maps. [CITED: https://pkg.go.dev/text/template#Template.Funcs]
   - Resolution: Phase 2 implementation will use stdlib template behavior only and will not register custom `FuncMap` entries.
   - Why: This keeps the v1 execution surface minimal while still meeting INJT-02/INJT-04 requirements. [VERIFIED: .planning/REQUIREMENTS.md]
   - Revisit trigger: Only reopen if a future requirement explicitly requires custom template functions.

2. **Should output paths be restricted to a configured base directory or only to declared explicit targets? — RESOLVED: Declared explicit targets (with lexical safety) for this phase.**
   - What we know: SECU-03 requires declared target-file boundaries. [VERIFIED: .planning/REQUIREMENTS.md]
   - Resolution: Phase 2 enforces declared-target allowlist + lexical checks (`filepath.Clean`/`filepath.IsLocal`) and does not require an additional base-dir sandbox in this phase.
   - Why: This directly satisfies SECU-03 and aligns with the phase scope without adding a second path-policy model. [VERIFIED: .planning/ROADMAP.md][CITED: https://pkg.go.dev/path/filepath#IsLocal]
   - Revisit trigger: Add base-dir policy only if explicitly required by a later phase or locked user decision.

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No auth flow introduced in this phase scope. [VERIFIED: phase requirements list] |
| V3 Session Management | no | No session concept in this phase. [VERIFIED: phase requirements list] |
| V4 Access Control | yes | Restrict materialization to declared env keys and file targets only (SECU-03). [VERIFIED: .planning/REQUIREMENTS.md] |
| V5 Input Validation | yes | Validate mapping references and template keys; reject invalid target paths. [VERIFIED: INJT-01/INJT-04/SECU-03 in .planning/REQUIREMENTS.md][CITED: https://pkg.go.dev/path/filepath#IsLocal] |
| V6 Cryptography | no (phase-local) | No new cryptographic primitive required in this phase. [VERIFIED: .planning/ROADMAP.md] |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Output path traversal via template target config | Tampering | Lexical path checks (`Clean`/`IsLocal`) + declared-target allowlist. [CITED: https://pkg.go.dev/path/filepath#Clean][CITED: https://pkg.go.dev/path/filepath#IsLocal] |
| Over-injection of secrets into unintended env vars | Information Disclosure | Explicit mapping schema and child-scoped `Cmd.Env` assignment only. [CITED: https://pkg.go.dev/os/exec#Cmd][VERIFIED: SECU-03 in .planning/REQUIREMENTS.md] |
| Silent omission of required template fields | Tampering | `missingkey=error` and startup fail-fast. [CITED: https://pkg.go.dev/text/template#Template.Option][VERIFIED: INJT-04 in .planning/REQUIREMENTS.md] |

## Sources

### Primary (HIGH confidence)
- `.planning/REQUIREMENTS.md` — phase requirements and security boundary requirements.
- `.planning/ROADMAP.md` — Phase 2 scope and success criteria.
- `AGENTS.md` (project) — non-negotiable constraints (Go runtime, no partial apply, secret handling).
- `internal/sync/coordinator.go`, `internal/state/store.go`, `go.mod` — current implementation baseline and dependency reality.
- https://pkg.go.dev/text/template#Template.Option — missingkey option semantics.
- https://pkg.go.dev/os/exec#Cmd — child environment semantics.
- https://pkg.go.dev/path/filepath#IsLocal — local lexical path safety primitive.
- https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/process-supervisor — process-supervisor mode constraints.
- Local version verification: `go version`, `go list -m -json ...@latest`.

### Secondary (MEDIUM confidence)
- Context7 `/golang/go` — API cross-check snippets for `text/template` and `os/exec`.
- Context7 `/websites/developer_hashicorp_vault` — process-supervisor mode details mirrored in official docs.

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — verified with local module/toolchain checks and official pkg docs.
- Architecture: HIGH — directly constrained by explicit INJT/SECU requirements + existing Phase 1 transaction model.
- Pitfalls: MEDIUM — partly doc-verified, partly implementation-risk assumptions.

**Research date:** 2026-05-21  
**Valid until:** 2026-06-20 (30 days; stable domain, but Go/toolchain/module versions may move)
