# Walking Skeleton — envfuse (Seon)

**Phase:** 1
**Generated:** 2026-05-21

## Capability Proven End-to-End

A developer can run `envfuse -config ./seon.json -once` against local `secrets.json`, execute one concurrent batch sync cycle, and receive deterministic success/failure/aborted outcomes while preserving last-known-good state on failed cycles.

## Architectural Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Framework | Go single-binary CLI runtime (`cmd/envfuse`) | Matches project constraint for lightweight container runtime and deterministic process control. |
| Data layer | In-memory cycle state + local JSON file backend | Phase 1 scope is local-first proof (PROV-03) without remote provider complexity. |
| Auth | None in Phase 1 skeleton | No user/session surface in this phase; security focus is secret-safe handling and atomic state boundaries. |
| Deployment target | Local full-stack run command (`go run ./cmd/envfuse -config ./seon.json -once`) | Walking skeleton requires runnable end-to-end proof; local deterministic command is the minimal valid target for this phase. |
| Directory layout | `cmd/envfuse` + `internal/{config,provider/local,sync,state,clock}` + `tests/integration` | Mirrors phase research architecture map and keeps runtime tiers isolated by responsibility. |

## Stack Touched in Phase 1

- [x] Project scaffold (Go module runtime layout + test entrypoints)
- [x] Routing — CLI route/entrypoint with `-config` and `-once` controls
- [x] Database — replaced for this runtime by local provider read + in-memory write/commit boundary (one read and one state commit path)
- [x] UI — replaced for this runtime by interactive CLI invocation (`envfuse ...`) wired to runtime cycle API
- [x] Deployment — documented local full-stack run command exercising full slice

## Out of Scope (Deferred to Later Slices)

- Env var injection mappings and template rendering materialization (Phase 2)
- PID 1 child-process supervision and restart logic (Phase 3)
- Vault/AWS providers and TLS transport enforcement (Phase 4)
- Metrics/logging hardening and observability outputs (Phase 5)

## Subsequent Slice Plan

Each later phase adds one vertical slice on top of this skeleton without altering its architectural decisions:

- Phase 2: Deliver dual-vector materialization (env + template files) in one successful cycle.
- Phase 3: Add PID 1 child supervision and safe restart controls.
- Phase 4: Add Vault/AWS remote provider implementations behind shared provider contract.
- Phase 5: Add production-safe log redaction guarantees and operator observability signals.
