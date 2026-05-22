# Roadmap: envfuse

## Overview

This roadmap delivers envfuse as a deterministic container runtime that first proves atomic sync correctness, then adds dual-vector secret delivery, PID 1 supervision/reload behavior, production backends, and final operational hardening for safe adoption.

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: Atomic Sync Foundation (Local-First)** - Establish config-driven local runtime with transactional sync semantics. (completed 2026-05-21)
- [x] **Phase 2: Dual-Vector Delivery (Env + Templates)** - Materialize secrets into env and files in one validated cycle. (completed 2026-05-21)
- [x] **Phase 3: PID 1 Supervision & Reload Control** - Run and manage child lifecycle with safe restart behavior. (completed 2026-05-22)
- [ ] **Phase 4: Production Providers & Secure Transport** - Add Vault/AWS providers and remote TLS-backed fetch paths.
- [ ] **Phase 5: Operational Safety & Observability** - Finalize redaction guarantees and operator-facing runtime signals.

## Phase Details

### Phase 1: Atomic Sync Foundation (Local-First)

**Goal**: Teams can run envfuse from `seon.json` with a local backend and trust batch-level all-or-nothing sync behavior.
**Mode:** mvp
**Depends on**: Nothing (first phase)
**Requirements**: PROV-03, SYNC-01, SYNC-02, SYNC-03, SYNC-04
**Success Criteria** (what must be TRUE):

  1. Developer can start envfuse with local JSON secrets for offline testing without changing app code.
  2. Runtime processes configured secret paths as a single sync batch each cycle.
  3. Runtime performs concurrent fetches for distinct paths and surfaces cycle completion/failure deterministically.
  4. If any fetch fails or times out, the cycle is rejected and previously applied env/files remain unchanged.

**Plans**: 3 plans

Plans:
**Wave 1**

- [x] 01-01-PLAN.md — Walking skeleton happy-path vertical slice (config → local provider → concurrent batch cycle).

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 01-02-PLAN.md — Failure-path vertical slice (timeout/fetch failure abort + last-known-good preservation).

### Phase 2: Dual-Vector Delivery (Env + Templates)

**Goal**: As a platform operator, I want to have envfuse materialize secrets into both child environment variables and rendered template files within one validated sync cycle, so that workloads start with complete and deterministic configuration without partial secret state.
**Mode:** mvp
**Depends on**: Phase 1
**Requirements**: INJT-01, INJT-02, INJT-03, INJT-04, SECU-03
**Success Criteria** (what must be TRUE):

  1. Operator can declare path:key-to-env mappings and child process receives those values on start/restart.
  2. Operator can define templates that render to target files before child process launch.
  3. Env injection and file rendering are applied together in the same successful cycle (not as separate partial steps).
  4. Startup fails fast when required template keys are missing.
  5. Runtime only materializes secrets into the supervised child environment and declared output files.

**Plans**: 2 plans

Plans:
**Wave 1**

- [x] 02-01-PLAN.md — Env-mapping vertical slice with strict schema validation and atomic env payload commit.

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 02-02-PLAN.md — Templates + file rendering wired into unified dual-vector atomic commit with fail-fast missing-key/path guards.

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 02-03-PLAN.md — Gap-closure plan: fix Phase 2 MVP goal to valid user-story format and rerun verification to clear blocker.

### Phase 3: PID 1 Supervision & Reload Control

**Goal**: envfuse can supervise a workload as PID 1 and restart it safely only when effective config changes.
**Mode:** mvp
**Depends on**: Phase 2
**Requirements**: SUPV-01, SUPV-02, SUPV-03, RELO-01, RELO-02, RELO-03
**Success Criteria** (what must be TRUE):

  1. Runtime can execute the configured child command as PID 1 and keep process lifecycle under supervisor control.
  2. Child process receives forwarded `SIGTERM`/`SIGINT` and exits gracefully when possible.
  3. If graceful shutdown exceeds configured timeout, runtime force-terminates the child and returns control predictably.
  4. Runtime computes a deterministic fingerprint of fetched/rendered effective state.
  5. Runtime restarts the child only on successful changed-state cycles, with cooldown/coalescing to prevent restart loops.

**Plans**: 3 plans

Plans:
**Wave 1**

- [x] 03-01-PLAN.md — PID1 supervision vertical slice (start/wait lifecycle, signal forwarding, graceful timeout + forced kill).

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 03-02-PLAN.md — Deterministic fingerprint + restart-on-change vertical slice (candidate-state hash gating).

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 03-03-PLAN.md — Cooldown/coalescing anti-loop vertical slice (single deferred restart during rapid churn).

### Phase 4: Production Providers & Secure Transport

**Goal**: Operators can use the same config model with Vault or AWS Secrets Manager over secure remote communication.
**Mode:** mvp
**Depends on**: Phase 3
**Requirements**: PROV-01, PROV-02, PROV-04, SECU-02
**Success Criteria** (what must be TRUE):

  1. Operator can configure Vault KVv2 as backend and fetch secrets successfully in normal sync cycles.
  2. Operator can configure AWS Secrets Manager as backend and fetch secrets successfully in normal sync cycles.
  3. Operator can switch `provider_type` between supported backends without changing template definitions or application code.
  4. All remote provider communication uses TLS by default and rejects insecure transport paths.

**Plans**: TBD

### Phase 5: Operational Safety & Observability

**Goal**: Runtime behavior is safe to operate in production with secret-safe logs and clear sync/restart visibility.
**Mode:** mvp
**Depends on**: Phase 4
**Requirements**: SECU-01, OBSV-01
**Success Criteria** (what must be TRUE):

  1. Operator cannot find secret values in stdout/stderr or structured runtime logs during normal and failure flows.
  2. Operator can observe sync success/failure, cycle latency, and explicit restart reasons for troubleshooting.

**Plans**: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 2 → 2.1 → 2.2 → 3 → 3.1 → 4

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Atomic Sync Foundation (Local-First) | 2/2 | Complete   | 2026-05-21 |
| 2. Dual-Vector Delivery (Env + Templates) | 3/3 | Complete    | 2026-05-21 |
| 3. PID 1 Supervision & Reload Control | 3/3 | Complete   | 2026-05-22 |
| 4. Production Providers & Secure Transport | 0/TBD | Not started | - |
| 5. Operational Safety & Observability | 0/TBD | Not started | - |
