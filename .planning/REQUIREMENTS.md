# Requirements: envfuse

**Defined:** 2026-05-21
**Core Value:** Ship one reliable runtime that atomically syncs secrets into both files and environment before app start, and safely hot-reloads on secret rotation without partial state.

## v1 Requirements

Requirements for initial release. Each maps to roadmap phases.

### Provider Backends

- [ ] **PROV-01**: Operator can configure Vault KVv2 as a secrets backend
- [ ] **PROV-02**: Operator can configure AWS Secrets Manager as a secrets backend
- [x] **PROV-03**: Developer can run envfuse with a local JSON file backend for offline testing
- [ ] **PROV-04**: Operator can switch backend by changing `provider_type` in config without changing templates or app code

### Injection and Rendering

- [x] **INJT-01**: Operator can map provider keys to environment variables using path:key mapping syntax
- [x] **INJT-02**: Operator can render template files from fetched secrets before child process start
- [x] **INJT-03**: Runtime can deliver both environment variables and rendered files in the same sync cycle
- [x] **INJT-04**: Runtime fails startup if required template keys are missing

### Atomic Synchronization

- [x] **SYNC-01**: Runtime fetches all configured secret paths as one batch per cycle
- [x] **SYNC-02**: Runtime fetches distinct provider paths concurrently for cycle performance
- [x] **SYNC-03**: Runtime aborts the cycle if any path fetch fails or times out
- [x] **SYNC-04**: Runtime preserves last known good env/files if a cycle fails

### Supervision and Reload

- [x] **SUPV-01**: Runtime can run as PID 1 and start the configured child command
- [x] **SUPV-02**: Runtime forwards `SIGTERM` and `SIGINT` to the child process
- [x] **SUPV-03**: Runtime enforces configurable graceful shutdown timeout, then force-kills if needed
- [ ] **RELO-01**: Runtime computes a deterministic state fingerprint for fetched/rendered config
- [ ] **RELO-02**: Runtime restarts child process when fingerprint changes after successful apply
- [ ] **RELO-03**: Runtime avoids restart loops using restart cooldown or coalescing controls

### Security and Observability

- [ ] **SECU-01**: Runtime never logs secret values in stdout/stderr or structured logs
- [ ] **SECU-02**: Runtime uses TLS for all remote provider communication
- [x] **SECU-03**: Runtime only injects secrets into child environment and declared target files
- [ ] **OBSV-01**: Operator can observe sync success/failure, cycle latency, and restart reason

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### Runtime Enhancements

- **RUNX-01**: Operator can classify secret changes by restart policy (restart-required vs non-restart)
- **RUNX-02**: Operator can run dry-run/validate mode in CI to detect mapping/template errors before deploy

### Platform Expansion

- **PLAT-01**: Operator can use envfuse on non-container runtimes (VM/bare metal)
- **PLAT-02**: Operator can use providers beyond Vault and AWS based on adoption demand

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Secret write-back APIs | Expands threat model and complexity; v1 is read/sync/supervise only |
| Arbitrary shell hooks on sync events | Reintroduces lifecycle races and nondeterministic behavior |
| Kubernetes Secret object sync as primary mode | Increases secret persistence footprint; v1 prioritizes process env + target files |
| Full parity with all Vault Agent modes | Product focus is deterministic dual-vector runtime in container context |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| PROV-01 | Phase 4 | Pending |
| PROV-02 | Phase 4 | Pending |
| PROV-03 | Phase 1 | Complete |
| PROV-04 | Phase 4 | Pending |
| INJT-01 | Phase 2 | Complete |
| INJT-02 | Phase 2 | Complete |
| INJT-03 | Phase 2 | Complete |
| INJT-04 | Phase 2 | Complete |
| SYNC-01 | Phase 1 | Complete |
| SYNC-02 | Phase 1 | Complete |
| SYNC-03 | Phase 1 | Complete |
| SYNC-04 | Phase 1 | Complete |
| SUPV-01 | Phase 3 | Complete |
| SUPV-02 | Phase 3 | Complete |
| SUPV-03 | Phase 3 | Complete |
| RELO-01 | Phase 3 | Pending |
| RELO-02 | Phase 3 | Pending |
| RELO-03 | Phase 3 | Pending |
| SECU-01 | Phase 5 | Pending |
| SECU-02 | Phase 4 | Pending |
| SECU-03 | Phase 2 | Complete |
| OBSV-01 | Phase 5 | Pending |

**Coverage:**
- v1 requirements: 22 total
- Mapped to phases: 22
- Unmapped: 0

---
*Requirements defined: 2026-05-21*
*Last updated: 2026-05-21 after roadmap mapping*
