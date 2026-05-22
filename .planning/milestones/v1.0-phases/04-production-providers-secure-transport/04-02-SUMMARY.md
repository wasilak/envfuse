---
phase: 04-production-providers-secure-transport
plan: "02"
subsystem: provider
tags: [aws, secrets-manager, aws-sdk-go-v2, provider-factory, tdd]

# Dependency graph
requires:
  - phase: 04-production-providers-secure-transport
    provides: vault provider with TLS, local provider, provider interface, buildProvider factory stub

provides:
  - AWS Secrets Manager provider with standard credential chain and JSON decoding
  - Completed buildProvider factory (all three backends wired)
  - AWSRegion and AWSEndpoint config fields
  - Provider switching integration smoke test (PROV-04)

affects:
  - Any future phase adding a fourth provider type
  - Integration tests relying on RunSingleCycleWithStoreFromConfig

# Tech tracking
tech-stack:
  added:
    - github.com/aws/aws-sdk-go-v2 v1.41.7
    - github.com/aws/aws-sdk-go-v2/config v1.32.17
    - github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.41.7
    - github.com/aws/smithy-go v1.25.1
    - Supporting AWS SDK v2 transitive dependencies
  patterns:
    - "Narrow interface (secretsManagerAPI) for SDK client testability — same pattern as vault HTTPClient injection"
    - "NewWithClient() exported constructor for test injection, New(ctx, cfg) for production"
    - "mockSMClient function type satisfying narrow interface for zero-boilerplate test mocks"
    - "httptest TLS server with CA cert written to temp file for config-driven vault integration tests"

key-files:
  created:
    - internal/provider/aws/aws.go
    - internal/provider/aws/aws_test.go
    - tests/integration/provider_switching_test.go
  modified:
    - internal/config/config.go (added AWSRegion, AWSEndpoint fields)
    - internal/sync/coordinator.go (buildProvider: aws case + ctx parameter restored)
    - internal/sync/coordinator_test.go (added TestCoordinator_ProviderFactory_AWS)
    - go.mod / go.sum (AWS SDK v2 dependencies)

key-decisions:
  - "secretsManagerAPI interface unexported (lowercase) — testability without leaking SDK dependency"
  - "NewWithClient exported for test injection; New(ctx, cfg) for production credential chain"
  - "Binary secrets (SecretString==nil) fail fast with explicit 'binary secrets not supported' message"
  - "Non-JSON secrets include the path name in the decode error for debuggability"
  - "buildProvider ctx parameter restored (was _ context.Context) — required for AWS IMDSv2 network calls"
  - "Integration test vault case uses CA cert file (vault_tls_ca_cert) rather than HTTP client injection — only config-driven path available from RunSingleCycleWithStoreFromConfig"
  - "No AWSRegion validation in LoadConfig — missing region surfaces as SDK error at runtime (same behavior as vault_address optional)"

patterns-established:
  - "Narrow interface + function-type mock: define unexported interface, export NewWithClient, use func type as mock"
  - "Config-driven integration tests write CA certs to temp files to avoid http:// SECU-02 rejection"

requirements-completed: [PROV-02, PROV-04, SECU-02]

# Metrics
duration: 12min
completed: 2026-05-22
---

# Phase 04 Plan 02: AWS Secrets Manager Provider Summary

**AWS Secrets Manager provider via standard credential chain with JSON decoding, completing the buildProvider factory for all three backends (local/vault/aws) and proving PROV-04 switchability in an integration test**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-05-22T08:10:49Z
- **Completed:** 2026-05-22T08:23:09Z
- **Tasks:** 3 (RED + GREEN + integration)
- **Files modified:** 8

## Accomplishments

- AWS Secrets Manager provider implemented with narrow interface for testability and no hardcoded credentials
- buildProvider factory fully wired for local, vault, and aws backends; ctx parameter restored for IMDSv2 compatibility
- AWSRegion and AWSEndpoint config fields added to Config struct (no extra validation — SDK surfaces errors at runtime)
- Provider switching integration test proves PROV-04: same secret_paths/env_mappings work across local and vault by changing only provider_type

## Task Commits

1. **Task 1: Failing tests for AWS provider (RED)** - `7dbff46` (test)
2. **Task 2: AWS provider implementation + config + factory (GREEN)** - `a96e8f6` (feat)
3. **Task 3: Provider switching integration smoke test** - `50ee1a8` (test)

## Files Created/Modified

- `internal/provider/aws/aws.go` - AWS Secrets Manager provider with secretsManagerAPI interface, New(), NewWithClient(), Fetch()
- `internal/provider/aws/aws_test.go` - Unit tests: happy path, binary secret rejection, non-JSON error with path name
- `internal/sync/coordinator.go` - buildProvider aws case wired; ctx restored from blank identifier
- `internal/sync/coordinator_test.go` - TestCoordinator_ProviderFactory_AWS added
- `internal/config/config.go` - AWSRegion and AWSEndpoint fields added
- `tests/integration/provider_switching_test.go` - TestProviderSwitching: local, vault (httptest TLS), unknown_type
- `go.mod` / `go.sum` - AWS SDK v2 dependencies

## Decisions Made

- secretsManagerAPI interface unexported to avoid leaking SDK dependency across package boundary
- NewWithClient exported for test injection; production always uses New(ctx, cfg) with credential chain
- Binary secrets fail fast with "binary secrets not supported" in error — no silent data loss
- buildProvider ctx was `_ context.Context` (unused); restored to named `ctx` since AWS needs it for IMDSv2
- Integration test vault case uses `vault_tls_ca_cert` config field + temp file rather than HTTP client injection (only config-driven path available)

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

The coordinator factory test `TestCoordinator_ProviderFactory_AWS` passed even in the RED state because `aws_region` was rejected by `DisallowUnknownFields` in `LoadConfig` (returning `config_invalid`, not `provider_init_failed`). The primary RED indicator was the aws_test.go compilation failure, which correctly represented the missing package. After adding `AWSRegion`/`AWSEndpoint` fields and the provider, both tests pass correctly in GREEN.

## Next Phase Readiness

- All three providers fully implemented and factory-wired
- Requirements PROV-02, PROV-04, SECU-02 satisfied
- Ready for remaining Phase 4 plans (e.g., end-to-end CLI testing with real-like configs)

---
*Phase: 04-production-providers-secure-transport*
*Completed: 2026-05-22*
