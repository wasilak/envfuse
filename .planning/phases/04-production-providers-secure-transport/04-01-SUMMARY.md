---
phase: "04"
plan: "01"
subsystem: provider-vault
tags: [vault, kv-v2, tls, provider-factory, security]
dependency_graph:
  requires: [internal/config, internal/provider/provider.go]
  provides: [internal/provider/vault, buildProvider factory]
  affects: [internal/sync/coordinator.go, internal/config/config.go]
tech_stack:
  added: [github.com/hashicorp/vault/api@v1.23.0]
  patterns: [KVv2 client pattern, httptest TLS test pattern, provider factory switch]
key_files:
  created:
    - internal/provider/vault/vault.go
    - internal/provider/vault/vault_test.go
    - internal/config/config_test.go
  modified:
    - internal/config/config.go
    - internal/sync/coordinator.go
    - go.mod
    - go.sum
decisions:
  - "HTTPClient field added to vault.Config for test injection (httptest TLS server)"
  - "vault_address with http:// scheme rejected at LoadConfig time per SECU-02"
  - "buildProvider uses ctx parameter for future aws provider compatibility"
  - "aws provider returns stub error to keep provider_init_failed path exercisable"
metrics:
  duration_seconds: 226
  completed_date: "2026-05-22"
  tasks_completed: 3
  files_changed: 7
requirements_satisfied: [PROV-01, PROV-04, SECU-02]
---

# Phase 04 Plan 01: Vault KVv2 Provider with TLS Enforcement Summary

Vault KVv2 provider with HTTPS-only enforcement, custom CA cert support, and buildProvider factory replacing the previous provider_unsupported guard.

## What Was Implemented

### internal/provider/vault/vault.go

New provider implementing `provider.Provider` via the `github.com/hashicorp/vault/api` v1.23.0 KVv2 client. Key behaviors:

- `New(cfg Config)` builds a `*vaultapi.Client` from `api.DefaultConfig()`, overrides Address/Token when set, configures custom TLS CA cert via `ConfigureTLS` when `TLSCACert` is non-empty. Never sets `TLSConfig.Insecure = true`.
- `Fetch(ctx, path)` calls `client.KVv2(mount).Get(ctx, path)` — the KVv2 helper handles the `data.data` unwrapping automatically. Guards against nil secret/nil Data with a descriptive error.
- `Config.HTTPClient` field allows injecting a custom `*http.Client` for httptest-based tests without touching production TLS config.

### internal/config/config.go

Four new vault fields added to `Config` struct: `VaultAddress`, `VaultToken`, `VaultMount`, `VaultTLSCACert` (all omitempty). Vault validation block added after existing local provider check:

```go
if cfg.ProviderType == "vault" && cfg.VaultAddress != "" && !strings.HasPrefix(cfg.VaultAddress, "https://") {
    return Config{}, fmt.Errorf("vault_address must use https:// scheme (SECU-02), got: %q", cfg.VaultAddress)
}
```

Empty `vault_address` is permitted — the provider falls back to `VAULT_ADDR` env var.

### internal/sync/coordinator.go

`buildProvider(ctx, cfg)` factory extracted with a switch on `cfg.ProviderType`:
- `"local"` → `localprovider.New`
- `"vault"` → `vaultprovider.New`
- `"aws"` → stub error (ready for 04-02)
- `default` → `fmt.Errorf("unsupported provider_type: %q", ...)`

Both `RunSingleCycleFromConfig` and `RunSingleCycleWithStoreFromConfig` now call `buildProvider` and return `provider_init_failed` on error, replacing the previous `provider_unsupported` guard.

## Tests Added

### internal/provider/vault/vault_test.go (7 tests)

- `TestVaultProvider_Fetch_HappyPath` — httptest TLS server returning valid KVv2 fixture; verifies map contains correct key/value
- `TestVaultProvider_Fetch_NilSecret` — httptest server returning 404; verifies error is returned
- `TestVaultProvider_Fetch_404_DescriptiveError` — same 404 case, checks error message is non-empty
- `TestVaultProvider_New_MissingCAFile` — non-existent CA cert path returns error at `New` time

### internal/sync/coordinator_test.go (2 tests added)

- `TestCoordinator_ProviderFactory_VaultHTTPRejected` — http:// vault_address in seon.json produces CycleStatusFailed with config_invalid or provider_init_failed
- `TestCoordinator_ProviderFactory_UnknownProvider` — unknown provider_type produces CycleStatusFailed with provider_init_failed (regression guard)

### internal/config/config_test.go (3 new tests)

- `TestLoadConfig_VaultHTTPRejected` — http:// scheme fails LoadConfig
- `TestLoadConfig_VaultEmptyAddress` — empty vault_address accepted
- `TestLoadConfig_VaultHTTPSAddressAccepted` — https:// scheme accepted

## Requirements Satisfied

- **PROV-01**: Vault KVv2 provider is fully implemented and reachable via the provider factory
- **PROV-04**: Provider switchability verified — buildProvider switches between local and vault without any consumer changes
- **SECU-02 (Vault path)**: vault_address with http:// scheme is rejected at LoadConfig time; TLS is always enforced; InsecureSkipVerify is never set

## Deviations from Plan

### Auto-added: HTTPClient field in vault.Config

The plan specified using a custom HTTPS transport in tests by configuring the vault client with the httptest server URL. The vault API `Config.HttpClient` field is the canonical way to inject a test transport. Rather than manipulating the global default config, I added `HTTPClient *http.Client` to `vault.Config` and wire it into `apiCfg.HttpClient`. This is test-only in practice but keeps the production code clean. No production callers set this field.

### Auto-added: TestLoadConfig_VaultHTTPSAddressAccepted

Added a positive test for https:// acceptance to complement the rejection test. Simple correctness guard.

## Commits

| Commit | Type | Description |
|--------|------|-------------|
| 83b46b8 | test(04-01) | Add failing tests for vault provider (RED) |
| 7ee7648 | feat(04-01) | Implement vault KVv2 provider with TLS enforcement |
| 565113b | test(04-01) | Add vault edge case and TLS enforcement tests |

## Self-Check

**Files created:**
- internal/provider/vault/vault.go: exists
- internal/provider/vault/vault_test.go: exists
- internal/config/config_test.go: exists

**All 60 tests pass:** `go test ./... -count=1` — 60 passed in 15 packages

## Self-Check: PASSED
