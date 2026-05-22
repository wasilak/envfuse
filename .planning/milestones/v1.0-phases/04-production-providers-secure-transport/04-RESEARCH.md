# Phase 4: Production Providers & Secure Transport - Research

**Researched:** 2026-05-22
**Domain:** Go secrets backends — HashiCorp Vault KVv2, AWS Secrets Manager SDK v2
**Confidence:** HIGH

---

## Summary

Phase 4 adds two production secret backends behind the existing `provider.Provider` interface. The local provider pattern (`internal/provider/local/local.go`) is the template: a small struct that holds config, a `New(...)` constructor, and a `Fetch(ctx, resourceURI)` method that returns `map[string]any`. Both new providers follow this exact shape.

The Vault backend uses `github.com/hashicorp/vault/api` v1.23.0 — the official HashiCorp Go client. Its `KVv2` helper handles the `data.data` nesting automatically, returning a flat `secret.Data` map that maps directly to the `Provider` interface return type. Token auth is read from the `VAULT_TOKEN` env var automatically by `NewClient`; a config field `vault_token` can override it. TLS uses system CAs by default, and a custom CA cert path is supported via `api.TLSConfig{CACert: ...}`.

The AWS backend uses `github.com/aws/aws-sdk-go-v2/config` + `github.com/aws/aws-sdk-go-v2/service/secretsmanager` v1.41.7. All AWS SDK v2 calls are HTTPS-only by default — there is no TLS opt-out. Auth uses the standard credential chain (env vars → `~/.aws/credentials` → IAM instance profile). The `SecretString` field contains a JSON-encoded map, so the provider JSON-decodes it into `map[string]any`. For unit testing, the SDK v2 pattern is to define a narrow interface and pass a mock; no httptest needed for AWS.

For Vault unit tests, the idiomatic approach is `net/http/httptest.NewServer` — point `api.Config.Address` at the test server URL and return a hard-coded KVv2 JSON fixture. This avoids the heavyweight `vault/vault` test cluster import.

**Primary recommendation:** Implement `internal/provider/vault/vault.go` and `internal/provider/aws/aws.go`, wire them into the provider factory in `coordinator.go`, and add the required config fields to `config.go`.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Secret fetch (Vault KVv2) | API / Backend (Go process) | — | envfuse runs as PID 1; all secret fetches happen in-process |
| Secret fetch (AWS SM) | API / Backend (Go process) | — | SDK v2 makes HTTPS calls from within the same Go process |
| TLS enforcement | API / Backend (Go process) | — | Each provider owns its HTTP client config |
| Auth credential resolution | API / Backend (Go process) | — | Token from env or config; AWS credential chain is in-process |
| Provider selection | API / Backend (Go process) | — | Factory in `coordinator.go` reads `provider_type` from config |

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PROV-01 | Operator can configure Vault KVv2 as a secrets backend | Vault `api.KVv2.Get()` pattern documented; config fields identified |
| PROV-02 | Operator can configure AWS Secrets Manager as a secrets backend | AWS SDK v2 `GetSecretValue` pattern documented; config fields identified |
| PROV-04 | Operator can switch `provider_type` between backends without changing templates or app code | Provider factory extension in `coordinator.go`; all backends implement `provider.Provider` |
| SECU-02 | Runtime uses TLS for all remote provider communication | Vault: system CA default + no Insecure flag; AWS SDK v2: HTTPS-only by design |

</phase_requirements>

---

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/hashicorp/vault/api` | v1.23.0 | Vault Go client — KVv2 reads, TLS config, token auth | Official HashiCorp client; `KVv2` helper handles data nesting |
| `github.com/aws/aws-sdk-go-v2/config` | v1.32.17 | AWS config loader — credential chain, region, endpoint | Official AWS SDK v2 config package |
| `github.com/aws/aws-sdk-go-v2/service/secretsmanager` | v1.41.7 | AWS Secrets Manager client | Official AWS SDK v2 service package |

[VERIFIED: Go module proxy (proxy.golang.org), confirmed 2026-05-22]

### Supporting (transitive — go mod tidy will add)

| Library | Purpose |
|---------|---------|
| `github.com/aws/aws-sdk-go-v2` | Core SDK primitives (`aws.String`, `aws.Config`) |
| `github.com/aws/smithy-go` | AWS SDK transport layer (transitive) |

No alternatives needed — both client packages are the canonical official libraries.

**go.mod additions:**

```
require (
    github.com/hashicorp/vault/api                       v1.23.0
    github.com/aws/aws-sdk-go-v2/config                  v1.32.17
    github.com/aws/aws-sdk-go-v2/service/secretsmanager  v1.41.7
)
```

Run `go mod tidy` after adding — this resolves all transitive deps automatically.

---

## Package Legitimacy Audit

> slopcheck was unavailable at research time. Packages verified via Go module proxy and official source repositories.

| Package | Registry | Source Repo | Verified Via | Disposition |
|---------|----------|-------------|--------------|-------------|
| `github.com/hashicorp/vault/api` | Go module proxy | github.com/hashicorp/vault | Go module proxy + official HashiCorp docs | Approved [CITED: developer.hashicorp.com/vault] |
| `github.com/aws/aws-sdk-go-v2/config` | Go module proxy | github.com/aws/aws-sdk-go-v2 | Go module proxy + official AWS docs | Approved [CITED: docs.aws.amazon.com/sdk-for-go/v2] |
| `github.com/aws/aws-sdk-go-v2/service/secretsmanager` | Go module proxy | github.com/aws/aws-sdk-go-v2 | Go module proxy + official AWS docs | Approved [CITED: docs.aws.amazon.com] |

**Packages removed:** none
**Packages flagged:** none

---

## Architecture Patterns

### Recommended Project Structure

```
internal/provider/
├── provider.go          # Provider interface (existing)
├── local/
│   └── local.go         # Local JSON provider (existing)
├── vault/
│   └── vault.go         # Vault KVv2 provider (new)
└── aws/
    └── aws.go           # AWS Secrets Manager provider (new)
```

The provider factory stays in `internal/sync/coordinator.go` — extend the `if cfg.ProviderType != "local"` guard to a switch statement.

---

### Pattern 1: Vault KVv2 Provider

**What:** Thin wrapper around `api.Client.KVv2(mount).Get(ctx, path)`. Mount is configurable (default `"secret"`). Token comes from env `VAULT_TOKEN` automatically; config field overrides if provided.

**Key details:**
- `DefaultConfig()` calls `ReadEnvironment()` internally, picking up `VAULT_ADDR` and `VAULT_TOKEN`.
- `client.SetToken(token)` overrides the env var token when `vault_token` config field is set.
- `api.TLSConfig{CACert: path}` provides a custom CA. Leave `Insecure` as `false` (zero value) — never set it.
- `secret.Data` from `KVv2.Get()` is already `map[string]interface{}` — return it directly.

```go
// Source: pkg.go.dev/github.com/hashicorp/vault/api + developer.hashicorp.com/vault
package vault

import (
    "context"
    "fmt"

    "github.com/hashicorp/vault/api"
)

type Provider struct {
    client *api.Client
    mount  string
}

type Config struct {
    Address  string // e.g. "https://vault.example.com:8200"
    Token    string // optional; falls back to VAULT_TOKEN env var
    Mount    string // default "secret"
    CACert   string // optional path to PEM CA cert file
}

func New(cfg Config) (*Provider, error) {
    apiCfg := api.DefaultConfig() // reads VAULT_ADDR from env
    if cfg.Address != "" {
        apiCfg.Address = cfg.Address
    }
    if cfg.CACert != "" {
        if err := apiCfg.ConfigureTLS(&api.TLSConfig{CACert: cfg.CACert}); err != nil {
            return nil, fmt.Errorf("vault tls: %w", err)
        }
    }
    // Insecure is zero-value false — TLS is enforced (SECU-02)

    client, err := api.NewClient(apiCfg)
    if err != nil {
        return nil, fmt.Errorf("vault client: %w", err)
    }
    if cfg.Token != "" {
        client.SetToken(cfg.Token) // overrides VAULT_TOKEN env var
    }
    // If cfg.Token == "", NewClient already read VAULT_TOKEN via ReadEnvironment

    mount := cfg.Mount
    if mount == "" {
        mount = "secret"
    }
    return &Provider{client: client, mount: mount}, nil
}

func (p *Provider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
    secret, err := p.client.KVv2(p.mount).Get(ctx, resourceURI)
    if err != nil {
        return nil, fmt.Errorf("vault fetch %q: %w", resourceURI, err)
    }
    if secret == nil || secret.Data == nil {
        return nil, fmt.Errorf("vault: secret not found at %q", resourceURI)
    }
    return secret.Data, nil // already map[string]interface{}
}
```

**Vault KVv2 HTTP response structure** (for test fixtures):
```json
{
  "data": {
    "data": {
      "username": "admin",
      "password": "s3cr3t"
    },
    "metadata": {
      "version": 1,
      "created_time": "2024-01-01T00:00:00Z",
      "deletion_time": "",
      "destroyed": false
    }
  }
}
```

The `KVv2.Get()` helper unwraps the outer `data` wrapper and returns a `KVSecret` where `.Data` is the inner `data.data` map — callers never touch the raw HTTP response.

---

### Pattern 2: AWS Secrets Manager Provider

**What:** Calls `GetSecretValue`, reads `SecretString`, JSON-decodes into `map[string]any`. Uses an internal interface for testability.

**Key details:**
- `config.LoadDefaultConfig()` uses the credential chain: `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` env vars → `~/.aws/credentials` → IAM instance profile (container metadata endpoint).
- Region can be set via `AWS_REGION` env var or the config field.
- All calls use HTTPS — the SDK v2 has no plaintext fallback. [CITED: docs.aws.amazon.com/sdk-for-go/v2]
- `cfg.aws_endpoint` overrides the endpoint for LocalStack/testing; use `config.WithBaseEndpoint(url)`.
- The mock interface pattern is from the official AWS SDK v2 unit testing guide.

```go
// Source: docs.aws.amazon.com/sdk-for-go/v2/developer-guide/unit-testing.html
//         docs.aws.amazon.com/secretsmanager/latest/userguide/retrieving-secrets-go-sdk.html
package aws

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/aws/aws-sdk-go-v2/aws"
    awsconfig "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// secretsManagerAPI is the narrow interface used for testing.
type secretsManagerAPI interface {
    GetSecretValue(
        ctx context.Context,
        params *secretsmanager.GetSecretValueInput,
        optFns ...func(*secretsmanager.Options),
    ) (*secretsmanager.GetSecretValueOutput, error)
}

type Provider struct {
    client secretsManagerAPI
}

type Config struct {
    Region   string // optional; falls back to AWS_REGION env var
    Endpoint string // optional; for LocalStack or test override
}

func New(ctx context.Context, cfg Config) (*Provider, error) {
    opts := []func(*awsconfig.LoadOptions) error{}
    if cfg.Region != "" {
        opts = append(opts, awsconfig.WithRegion(cfg.Region))
    }
    if cfg.Endpoint != "" {
        opts = append(opts, awsconfig.WithBaseEndpoint(cfg.Endpoint))
    }

    awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
    if err != nil {
        return nil, fmt.Errorf("aws config: %w", err)
    }
    return &Provider{client: secretsmanager.NewFromConfig(awsCfg)}, nil
}

// NewWithClient injects a client — used in tests.
func NewWithClient(client secretsManagerAPI) *Provider {
    return &Provider{client: client}
}

func (p *Provider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
    out, err := p.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
        SecretId: aws.String(resourceURI),
    })
    if err != nil {
        return nil, fmt.Errorf("aws secretsmanager fetch %q: %w", resourceURI, err)
    }
    if out.SecretString == nil {
        return nil, fmt.Errorf("aws secretsmanager: secret %q has no string value (binary not supported)", resourceURI)
    }

    var result map[string]any
    if err := json.Unmarshal([]byte(*out.SecretString), &result); err != nil {
        return nil, fmt.Errorf("aws secretsmanager: decode secret %q: %w", resourceURI, err)
    }
    return result, nil
}
```

---

### Pattern 3: Provider Factory Extension

Extend `RunSingleCycleWithStoreFromConfig` in `coordinator.go` (and the non-store variant) with a switch:

```go
func buildProvider(cfg config.Config) (provider.Provider, error) {
    switch cfg.ProviderType {
    case "local":
        return localprovider.New(cfg.LocalFilePath), nil
    case "vault":
        return vaultprovider.New(vaultprovider.Config{
            Address: cfg.VaultAddress,
            Token:   cfg.VaultToken,
            Mount:   cfg.VaultMount,
            CACert:  cfg.VaultTLSCACert,
        })
    case "aws":
        return awsprovider.New(context.Background(), awsprovider.Config{
            Region:   cfg.AWSRegion,
            Endpoint: cfg.AWSEndpoint,
        })
    default:
        return nil, fmt.Errorf("unsupported provider_type: %q", cfg.ProviderType)
    }
}
```

The factory returns `(provider.Provider, error)` — error propagates as `CycleResult{Status: CycleStatusFailed, ErrorClass: "provider_init_failed"}`.

---

### Anti-Patterns to Avoid

- **Setting `Insecure: true` on Vault TLSConfig:** Bypasses certificate validation entirely. Never set this in production code paths. [SECU-02]
- **Importing `github.com/hashicorp/vault/vault` for unit tests:** This is the heavy internal Vault package that requires CGO and external dependencies. Use `httptest.NewServer` instead.
- **Passing `*secretsmanager.Client` directly as a function parameter:** Prevents mocking. Always depend on the narrow `secretsManagerAPI` interface.
- **Not handling `SecretString == nil`:** Binary secrets return `SecretBinary`, not `SecretString`. Fail fast with a clear error rather than panicking on nil deref.
- **Returning raw Vault `KVSecret.Data` when `secret == nil`:** `KVv2.Get()` returns `nil, nil` for a 404 (secret not found). Must guard against nil.

---

## Config Schema Fields

Add to `internal/config/config.go` `Config` struct (DisallowUnknownFields means all fields must be declared):

```go
// Vault provider fields (used when provider_type = "vault")
VaultAddress  string `json:"vault_address,omitempty"`
VaultToken    string `json:"vault_token,omitempty"`   // overrides VAULT_TOKEN env var
VaultMount    string `json:"vault_mount,omitempty"`   // default "secret"
VaultTLSCACert string `json:"vault_tls_ca_cert,omitempty"` // path to CA cert PEM file

// AWS provider fields (used when provider_type = "aws")
AWSRegion   string `json:"aws_region,omitempty"`   // overrides AWS_REGION env var
AWSEndpoint string `json:"aws_endpoint,omitempty"` // custom endpoint (LocalStack, test)
```

Add validation in `LoadConfig` after the existing switch:

```go
case "vault":
    // vault_address can be omitted if VAULT_ADDR env var is set
    // vault_token can be omitted if VAULT_TOKEN env var is set
    // no hard-fail here — missing creds surface as fetch errors at runtime
case "aws":
    // region can be omitted if AWS_REGION env var is set
```

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| KVv2 `data.data` unwrapping | Custom Vault HTTP client | `api.KVv2.Get()` | The nested JSON response structure (outer `data`, inner `data.data`) is error-prone to unwrap manually; the SDK helper handles it |
| AWS credential chain | Custom env-var reader | `config.LoadDefaultConfig()` | IAM role, ECS task role, EC2 instance profile, `~/.aws/credentials`, env vars — many edge cases |
| TLS certificate pool construction | `x509.CertPool` manual loading | `api.TLSConfig{CACert: ...}` | Platform-specific CA store locations, PEM parsing errors |
| AWS HTTPS enforcement | Custom transport with TLS config | SDK v2 default | AWS SDK v2 uses HTTPS unconditionally; adding a custom transport risks accidentally weakening it |

---

## Unit Testing Approach

### Vault Provider — httptest.NewServer

The Vault API client is configured via `api.Config.Address`. In tests, point this at an `httptest.NewServer`. The KVv2 read path is a `GET /v1/{mount}/data/{path}` request.

```go
// internal/provider/vault/vault_test.go
package vault_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestVaultProvider_Fetch(t *testing.T) {
    // KVv2 fixture — outer "data" wrapper, inner "data" is the actual secret
    fixture := map[string]any{
        "data": map[string]any{
            "data": map[string]any{
                "username": "admin",
                "password": "s3cr3t",
            },
            "metadata": map[string]any{
                "version": 1,
            },
        },
    }

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // KVv2 read path: GET /v1/{mount}/data/{path}
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(fixture)
    }))
    defer srv.Close()

    p, err := New(Config{Address: srv.URL, Token: "test-token", Mount: "secret"})
    if err != nil {
        t.Fatal(err)
    }

    data, err := p.Fetch(context.Background(), "myapp/db")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if data["username"] != "admin" {
        t.Errorf("expected admin, got %v", data["username"])
    }
}
```

Note: `httptest.NewServer` uses HTTP (not HTTPS). The Vault client accepts HTTP addresses — it only enforces TLS when the address scheme is `https`. This is correct for unit tests.

### AWS Provider — Interface Mock

Define `secretsManagerAPI` in the provider package (as shown above). Tests inject a mock function value:

```go
// Source: docs.aws.amazon.com/sdk-for-go/v2/developer-guide/unit-testing.html
// internal/provider/aws/aws_test.go
package aws_test

import (
    "context"
    "testing"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// mockSMClient satisfies secretsManagerAPI
type mockSMClient func(
    ctx context.Context,
    params *secretsmanager.GetSecretValueInput,
    optFns ...func(*secretsmanager.Options),
) (*secretsmanager.GetSecretValueOutput, error)

func (m mockSMClient) GetSecretValue(
    ctx context.Context,
    params *secretsmanager.GetSecretValueInput,
    optFns ...func(*secretsmanager.Options),
) (*secretsmanager.GetSecretValueOutput, error) {
    return m(ctx, params, optFns...)
}

func TestAWSProvider_Fetch(t *testing.T) {
    mock := mockSMClient(func(ctx context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
        return &secretsmanager.GetSecretValueOutput{
            SecretString: aws.String(`{"username":"admin","password":"s3cr3t"}`),
        }, nil
    })

    p := NewWithClient(mock)
    data, err := p.Fetch(context.Background(), "myapp/db")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if data["username"] != "admin" {
        t.Errorf("expected admin, got %v", data["username"])
    }
}
```

---

## TLS Enforcement (SECU-02)

### Vault

- `api.DefaultConfig()` defaults to TLS-enabled HTTPS.
- `api.TLSConfig.Insecure` must never be set to `true` in any code path.
- Custom CA: `apiCfg.ConfigureTLS(&api.TLSConfig{CACert: "/path/to/ca.pem"})`.
- System CAs are used when `CACert` and `CAPath` are both empty — standard Go `crypto/tls` behavior.
- The address scheme drives enforcement: if `vault_address` starts with `http://`, the client sends plaintext. The operator must configure `https://`. Document this in config validation error messages.
- Consider adding a validation rule in `LoadConfig`: if `provider_type == "vault"` and `vault_address` is set and doesn't start with `https://`, return an error. [SECU-02]

### AWS

- AWS SDK v2 uses HTTPS unconditionally for all `secretsmanager` API calls. [CITED: docs.aws.amazon.com/sdk-for-go/v2]
- `config.WithBaseEndpoint("http://localhost:4566")` for LocalStack uses HTTP — this is only acceptable in test/local environments. The production code path never sets `AWSEndpoint` for non-test configs.
- No additional TLS configuration is needed or possible for AWS — the SDK manages it.

---

## Common Pitfalls

### Pitfall 1: Vault Returns nil Secret (404)

**What goes wrong:** `KVv2.Get()` returns `nil, nil` when the secret path doesn't exist (HTTP 404). Accessing `secret.Data` panics.

**Why it happens:** The SDK treats 404 as a non-error (path simply doesn't exist). This differs from network errors.

**How to avoid:** Always check `if secret == nil || secret.Data == nil` before returning data. Return a descriptive error.

**Warning signs:** `nil pointer dereference` panic in `Fetch`.

---

### Pitfall 2: DisallowUnknownFields Config Breakage

**What goes wrong:** Adding new provider config fields to JSON but not to the `Config` struct causes `config.LoadConfig` to return a decode error, breaking all existing configs.

**Why it happens:** `json.Decoder.DisallowUnknownFields()` is set in `LoadConfig`.

**How to avoid:** Add all new fields to the `Config` struct with `omitempty` tags before adding them to any JSON config file. All new fields must be optional (pointer or zero-value) so existing configs with `provider_type: "local"` continue to load without change.

---

### Pitfall 3: AWS Binary Secrets

**What goes wrong:** If a secret is stored as binary (not JSON text), `SecretString` is nil and `SecretBinary` is populated. The provider panics or returns empty data.

**Why it happens:** AWS Secrets Manager supports both string and binary secret types. Not all secrets are JSON-encoded strings.

**How to avoid:** Check `out.SecretString == nil` before dereferencing. Return a clear error: `"binary secrets not supported"`. This is a deliberate scope limit.

---

### Pitfall 4: Vault Mount vs Path Confusion

**What goes wrong:** Operator configures `vault_mount: "secret"` and `secret_paths: ["secret/myapp"]`, causing the actual Vault path to be `secret/data/secret/myapp` (mount + path collision).

**Why it happens:** The `resourceURI` from `secret_paths` is used as the path argument to `KVv2.Get()`. The mount is prepended by the client. If the operator includes the mount in the path, it doubles.

**How to avoid:** Document clearly: `vault_mount` is the KVv2 engine mount point (e.g., `"secret"`), and `secret_paths` entries are paths *within* the mount (e.g., `"myapp/database"`). Add a config validation warning or error if a path starts with the configured mount value.

---

### Pitfall 5: Context Cancellation in Provider Factory

**What goes wrong:** `awsprovider.New(context.Background(), cfg)` is called in the provider factory inside `RunSingleCycleWithStoreFromConfig`, which already has a `ctx` in scope. Using `context.Background()` in the factory ignores cancellation during initialization.

**Why it happens:** `LoadDefaultConfig` is async and can make network calls (EC2 metadata endpoint for IMDSv2).

**How to avoid:** Pass the caller's `ctx` into `awsprovider.New(ctx, cfg)`. The factory function signature becomes `buildProvider(ctx context.Context, cfg config.Config)`.

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `github.com/hashicorp/vault/api` manual `Logical().Read()` | `client.KVv2(mount).Get(ctx, path)` | KVv2 helper added in api v1.8.0 | No need to manually unwrap `data.data` nesting |
| AWS SDK v1 (`github.com/aws/aws-sdk-go`) | AWS SDK v2 (`github.com/aws/aws-sdk-go-v2`) | 2020 | Interface-based clients, modules per service, no global state |
| `config.WithEndpointResolverWithOptions` | `config.WithBaseEndpoint` | SDK v2 ~1.25 | Simpler endpoint override for LocalStack |

**Deprecated/outdated:**
- `github.com/aws/aws-sdk-go` (v1): Deprecated in favor of v2. Do not add.
- `Logical().Read("secret/data/path")` raw path: Superseded by `KVv2.Get()` helper which handles mount + path construction.
- `api.Config.ReadEnvironment()` called explicitly: `DefaultConfig()` calls it internally. Calling it again is redundant.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `KVv2.Get()` returns `nil, nil` for a 404 (not an error) | Pitfall 1, Provider Pattern | If 404 returns an error instead, the nil guard is still safe but the error message may differ |
| A2 | `api.DefaultConfig()` + `NewClient()` automatically reads `VAULT_TOKEN` from env | Vault Pattern | If token must be set explicitly, all token-less configs fail silently |

[A2 is based on the WebFetch of the raw Vault client.go source which shows `NewClient` reads `os.Getenv(EnvVaultToken)` — HIGH confidence]
[A1 is based on multiple community sources and the KVv2 API docs — MEDIUM confidence, verify with one test case]

---

## Open Questions

1. **Vault AppRole / Kubernetes auth**
   - What we know: Token auth works for PROV-01; env var `VAULT_TOKEN` is the standard operator interface.
   - What's unclear: Whether operators will want AppRole or Kubernetes auth in Phase 4 or defer to Phase 5/v2.
   - Recommendation: Token auth only for Phase 4. AppRole is deferred (add a RUNX requirement if needed).

2. **AWS SecretString non-JSON format**
   - What we know: Some secrets are plain strings (not JSON). The provider fails with a decode error.
   - What's unclear: Whether envfuse operators will store non-JSON secrets.
   - Recommendation: Fail fast with a clear error for non-JSON secrets. V2 can add `SecretString` as a raw-value fallback.

---

## Environment Availability

| Dependency | Required By | Available | Fallback |
|------------|------------|-----------|----------|
| Go 1.25 | All compilation | Assumed present (project uses go 1.25.0 in go.mod) | — |
| Vault server | PROV-01 runtime | Not needed for unit tests (httptest mock) | httptest server in tests |
| AWS account / LocalStack | PROV-02 runtime | Not needed for unit tests (interface mock) | Mock in tests |
| Internet (module download) | `go mod tidy` | Required once | Go module cache after first run |

---

## Sources

### Primary (HIGH confidence — verified via official sources)

- Go module proxy `proxy.golang.org` — confirmed `github.com/hashicorp/vault/api v1.23.0` (2026-03-23), `aws-sdk-go-v2 config v1.32.17` (2026-04-29), `secretsmanager v1.41.7` (2026-04-29)
- `developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2` — KVv2 response format, GET path structure
- `pkg.go.dev/github.com/hashicorp/vault/api` — `KVv2`, `TLSConfig`, `Config`, `DefaultConfig`, `SetToken`
- `docs.aws.amazon.com/sdk-for-go/v2/developer-guide/unit-testing.html` — interface mock pattern for AWS SDK v2
- `docs.aws.amazon.com/secretsmanager/latest/userguide/retrieving-secrets-go-sdk.html` — `GetSecretValue` canonical example
- Raw `github.com/hashicorp/vault/blob/main/api/client.go` — `TLSConfig` struct fields, `VAULT_TOKEN` env var handling

### Secondary (MEDIUM confidence)

- `pkg.go.dev/github.com/aws/aws-sdk-go-v2/config` — `LoadDefaultConfig`, `WithBaseEndpoint`, `WithRegion`
- `github.com/hashicorp/vault-examples/blob/main/examples/_quick-start/go/example.go` — `KVv2.Get()` + `secret.Data` access pattern

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — versions verified via Go module proxy against official repos
- Architecture: HIGH — mirrors existing local provider pattern exactly
- Vault TLS: HIGH — TLSConfig fields verified from vault/api client.go source
- AWS mock pattern: HIGH — from official AWS SDK v2 developer guide
- Pitfalls: MEDIUM — A1 (nil on 404) is community-verified but not tested in this session

**Research date:** 2026-05-22
**Valid until:** 2026-08-22 (Vault API and AWS SDK v2 are stable; check for minor version bumps before go mod tidy)
