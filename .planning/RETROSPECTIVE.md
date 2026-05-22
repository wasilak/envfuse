# Retrospective

## Milestone: v1.0 — MVP

**Shipped:** 2026-05-22
**Phases:** 5 | **Plans:** 12 | **Timeline:** 2 days

### What Was Built

1. Atomic batch sync with concurrent fetch and all-or-nothing apply semantics (Phase 1)
2. Dual-vector delivery — env mapping + file template rendering in one cycle (Phase 2)
3. PID 1 supervision with signal forwarding, shutdown timeout, and cooldown anti-loop (Phase 3)
4. Production providers — Vault KVv2 + AWS Secrets Manager with TLS enforcement (Phase 4)
5. Secret-safe structured logging with 8 observability events and SECU-01 redaction tests (Phase 5)

### What Worked

- **Wave-based parallel execution** — Plans within the same wave ran in isolated worktrees simultaneously, cutting total execution time significantly.
- **Narrow interfaces for testability** — `secretsManagerAPI` and `Fetch(ctx, path)` kept providers small and independently mockable. Test coverage was high without integration overhead.
- **StageCandidate/CommitCandidate gate** — The two-phase commit pattern for cycle results was the right abstraction; it made atomic semantics provable in unit tests.
- **TDD discipline on providers** — Writing failing tests first (RED commit) for Vault and AWS providers caught interface design issues before implementation, not after.
- **slog passive redaction** — Designing to emit `ErrorClass` codes (not raw errors) as the first-class logging primitive made SECU-01 a structural property, not an afterthought.

### What Was Inefficient

- **Phase 01 MVP goal format** — The verifier's User Story format guard caused a gap_found in Phase 01 verification. A gap-closure plan (02-03) was inserted to fix it in Phase 02. This overhead could have been avoided by matching goal syntax to the verifier's expectations upfront.
- **Config reload duplication** — `main.go` initially loaded config twice (once inside `RunSingleCycleWithStoreFromConfig`, once explicitly). Phase 05 refactored this into a single load, but the duplication existed through most of the milestone.
- **REQUIREMENTS.md not auto-updated on phase completion** — SECU-01 and OBSV-01 remained `Pending` in REQUIREMENTS.md after Phase 05 verification. Manual update was required at milestone close.

### Patterns Established

- **Worktree isolation per plan** — Each executor runs in a `worktree-agent-*` namespace; orchestrator merges via `--ff-only` after each wave. Clean separation prevents concurrent edit conflicts.
- **ErrorClass over error messages** — `fetch_failed`, `config_invalid`, `fetch_aborted` as structured codes, never raw error strings, in log output.
- **Narrow interface injection** — Production dependencies (AWS SDK, HTTP client) hidden behind single-method interfaces for test isolation without real infrastructure.
- **`truncate8` fingerprint helper** — Log-safe fingerprint display by truncating to 8 chars — enough for correlation, not enough to reconstruct state.

### Key Lessons

- Define MVP goal format consistent with verifier expectations at roadmap creation time — saves a gap-closure plan.
- Load external config exactly once at the binary entry point, then thread it through — avoids duplication and makes startup logging accurate.
- REQUIREMENTS.md should be updated as part of phase verification completion, not deferred to milestone close.
- Two-day execution from planning to all-green for a 5-phase, 12-plan project is achievable with parallel worktree execution and focused TDD.

### Cost Observations

- Parallel wave execution with worktree isolation kept orchestrator context lean (~15%) while subagents ran at full context capacity.
- No backtracking or re-execution needed — all 12 plans passed verification on first attempt.
- Sessions: 1 orchestrated milestone close session + 1 execution session (Phase 05).

---

## Cross-Milestone Trends

| Metric | v1.0 |
|--------|------|
| Phases | 5 |
| Plans | 12 |
| Tests | 71 |
| Backtracking | 0 plans re-executed |
| Deferred items at close | 1 (tooling gap) |
| Timeline | 2 days |
