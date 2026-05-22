# envfuse

[![CI](https://github.com/piotrek-b/envfuse/actions/workflows/ci.yml/badge.svg)](https://github.com/piotrek-b/envfuse/actions/workflows/ci.yml)
[![Docker](https://github.com/piotrek-b/envfuse/actions/workflows/docker.yml/badge.svg)](https://github.com/piotrek-b/envfuse/actions/workflows/docker.yml)
[![CodeQL](https://github.com/piotrek-b/envfuse/actions/workflows/codeql.yml/badge.svg)](https://github.com/piotrek-b/envfuse/actions/workflows/codeql.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/piotrek-b/envfuse)](https://goreportcard.com/report/github.com/piotrek-b/envfuse)

**envfuse** is a lightweight Go process supervisor for containers. It fetches secrets from Vault, AWS Secrets Manager, or a local file, renders config templates, injects environment variables, and then runs your application as PID 1 — all in one atomic lifecycle transaction before the first byte of your app code executes.

```
seon.json → fetch secrets → render files → inject env → exec your app
                              ↑ poll for changes → restart on rotation ↑
```

## Why

Vault Agent `exec` mode, init containers, and entrypoint scripts all share the same flaw: they treat secret delivery and app startup as separate steps, which creates partial-state risk on failure and operational complexity on rotation. envfuse collapses both into a single runtime with deterministic all-or-nothing semantics.

- **Atomic** — if any secret path fails, nothing is applied and the container exits cleanly
- **Dual-vector** — env vars and rendered template files in the same cycle, not separate steps
- **Rotation-aware** — polls on a configurable interval; restarts the child only when the effective config fingerprint changes
- **Secret-safe logs** — structured JSON output; secret values never appear in stdout/stderr

## Installation

### Copy binary into your image (recommended)

```dockerfile
FROM ghcr.io/piotrek-b/envfuse:latest AS envfuse

FROM your-base-image
COPY --from=envfuse /envfuse /usr/local/bin/envfuse

COPY seon.json /etc/seon.json
ENTRYPOINT ["envfuse", "-config", "/etc/seon.json"]
CMD ["/app/your-service"]
```

### Download binary

Grab a release from [GitHub Releases](https://github.com/piotrek-b/envfuse/releases) — available for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.

### Build from source

```bash
go build -o envfuse ./cmd/envfuse/
```

## Quick start

Create `seon.json` next to your binary:

```json
{
  "provider_type": "local",
  "local_file_path": "./secrets.json",
  "secret_paths": ["app/config"],
  "env_mappings": [
    { "path": "app/config", "key": "db_password", "env_var": "DB_PASSWORD" },
    { "path": "app/config", "key": "api_key",     "env_var": "API_KEY" }
  ]
}
```

```json
// secrets.json
{
  "app/config": {
    "db_password": "hunter2",
    "api_key": "secret-123"
  }
}
```

```bash
envfuse -config seon.json -- ./my-app --port 8080
```

Your app starts with `DB_PASSWORD` and `API_KEY` in its environment. envfuse supervises it, polls every 30 seconds, and restarts it if the secrets change.

## Configuration reference

```jsonc
{
  // Required. One of: "local", "vault", "aws"
  "provider_type": "vault",

  // ── Paths ──────────────────────────────────────────────────────────────
  // Paths to fetch from the backend. All paths are fetched concurrently
  // in each sync cycle. The cycle is aborted if any path fails.
  "secret_paths": ["secret/data/app", "secret/data/db"],

  // ── Env injection ──────────────────────────────────────────────────────
  // Map individual secret keys to environment variable names.
  "env_mappings": [
    { "path": "secret/data/app", "key": "api_key",     "env_var": "API_KEY" },
    { "path": "secret/data/db",  "key": "password",    "env_var": "DB_PASS" }
  ],

  // ── File templates ─────────────────────────────────────────────────────
  // Render Go text/template files from fetched secrets before child start.
  // Use either inline "content" or a "template_path" — not both.
  "templates": [
    {
      "name": "db-config",
      "template_path": "/etc/templates/db.conf.tmpl",
      "output_path": "/run/config/db.conf",
      "required_paths": ["secret/data/db"]
    }
  ],

  // ── Supervisor timing ──────────────────────────────────────────────────
  "shutdown_timeout":    "15s",   // default: 10s — grace period before SIGKILL
  "reload_poll_interval": "60s",  // default: 30s — how often to check for changes
  "reload_cooldown":     "30s",   // default: 10s — coalesce rapid rotations into one restart

  // ── Provider: vault ────────────────────────────────────────────────────
  "vault_address":    "https://vault.example.com",  // or VAULT_ADDR env var
  "vault_token":      "",                           // or VAULT_TOKEN env var (recommended)
  "vault_mount":      "secret",                     // KVv2 mount path
  "vault_tls_ca_cert": "/etc/ssl/vault-ca.pem",     // optional custom CA

  // ── Provider: aws ──────────────────────────────────────────────────────
  "aws_region": "eu-west-1",  // optional — falls back to standard credential chain
  "aws_endpoint": "",         // optional — LocalStack or custom endpoint

  // ── Provider: local ────────────────────────────────────────────────────
  "local_file_path": "./secrets.json"  // JSON file: { "path": { "key": "value" } }
}
```

## Providers

### Local file

For development and CI. No credentials required.

```json
{
  "provider_type": "local",
  "local_file_path": "./secrets.json",
  "secret_paths": ["app/config"]
}
```

`secrets.json` format:

```json
{
  "app/config": {
    "db_password": "dev-password",
    "feature_flag": "true"
  }
}
```

### HashiCorp Vault (KVv2)

`vault_address` must use `https://`. The token is read from `VAULT_TOKEN` env var or the `vault_token` config field. Vault Addr also falls back to `VAULT_ADDR`.

```json
{
  "provider_type": "vault",
  "vault_address":  "https://vault.prod.example.com",
  "vault_mount":    "secret",
  "secret_paths":   ["secret/data/myapp/prod"]
}
```

```bash
VAULT_TOKEN=s.xxxxx envfuse -config seon.json -- ./server
```

Custom CA for internal PKI:

```json
{
  "vault_tls_ca_cert": "/etc/ssl/certs/internal-ca.pem"
}
```

### AWS Secrets Manager

Uses the standard AWS credential chain (env vars → `~/.aws/config` → EC2 instance profile / ECS task role). Secrets must be JSON strings.

```json
{
  "provider_type": "aws",
  "aws_region":    "us-east-1",
  "secret_paths":  ["prod/myapp/config", "prod/myapp/db"]
}
```

For LocalStack:

```json
{
  "provider_type":  "aws",
  "aws_region":     "us-east-1",
  "aws_endpoint":   "http://localhost:4566"
}
```

## File templates

Templates use Go's `text/template` syntax. The data context for each template is a flat map of all fetched secret keys from its `required_paths`.

```
# /etc/templates/db.conf.tmpl
[database]
host     = {{ .db_host }}
port     = {{ .db_port }}
password = {{ .db_password }}
```

Inline templates (no file needed):

```json
{
  "templates": [
    {
      "name": "app-config",
      "content": "APP_SECRET={{ .secret_value }}\nDEBUG=false\n",
      "output_path": "/run/config/app.env",
      "required_paths": ["secret/data/app"]
    }
  ]
}
```

The template is rendered atomically to a temp file and renamed into place. If any required key is missing, the entire cycle is aborted.

## Modes

### Supervisor mode (default)

envfuse runs as PID 1, supervises the child process, and polls for secret changes.

```bash
envfuse -config seon.json -- ./server --port 8080
```

- Forwards `SIGTERM` and `SIGINT` to the child
- On graceful shutdown timeout: `SIGKILL`
- On secret rotation: deterministic fingerprint comparison → restart only on actual change
- Rapid rotations within the cooldown window are coalesced into a single restart

### One-shot mode (`-once`)

Sync secrets, inject env, exec the child without a supervision loop. Useful for jobs and init containers.

```bash
envfuse -config seon.json -once -- ./migrate
```

### Sync-only mode (no child command)

Run one sync cycle without launching a child — useful for pre-populating files in init containers.

```bash
envfuse -config seon.json
# exits 0 after writing template files; no child started
```

## Observability

envfuse emits structured JSON logs to stderr. Set `ENVFUSE_LOG_LEVEL` to control verbosity.

| Level | Events included |
|-------|----------------|
| `ERROR` | Startup failures, provider errors, child exec failures |
| `WARN` | Force-kills after shutdown timeout |
| `INFO` (default) | Startup summary, sync cycle outcomes, restart/shutdown events |
| `DEBUG` | Per-path fetch results, env var names applied, template output paths |

**Startup:**
```json
{"time":"...","level":"INFO","msg":"envfuse started","provider_type":"vault","paths_fetched":2,"env_vars_applied":4,"files_rendered":1,"fingerprint":"a3f2b1c8"}
```

**Sync cycle (every poll tick):**
```json
{"time":"...","level":"INFO","msg":"sync cycle","status":"success","latency_ms":43}
{"time":"...","level":"INFO","msg":"sync cycle","status":"fetch_failed","latency_ms":12,"error_class":"fetch_failed"}
```

**Restart on rotation:**
```json
{"time":"...","level":"INFO","msg":"config changed, restarting child","fingerprint_old":"a3f2b1c8","fingerprint_new":"9d1e4f72","restart_reason":"config_changed"}
```

**Shutdown:**
```json
{"time":"...","level":"INFO","msg":"shutdown signal received, stopping child","exit_reason":"child_exited"}
```

Secret values are **never** logged. The log contract is verified by automated redaction tests on every CI run.

## Kubernetes example

```yaml
# Deployment snippet
containers:
  - name: app
    image: your-app:latest
    command: ["/usr/local/bin/envfuse"]
    args: ["-config", "/etc/seon/seon.json", "--", "/app/server"]
    env:
      - name: VAULT_TOKEN
        valueFrom:
          secretKeyRef:
            name: vault-token
            key: token
      - name: ENVFUSE_LOG_LEVEL
        value: INFO
    volumeMounts:
      - name: seon-config
        mountPath: /etc/seon
      - name: rendered-config
        mountPath: /run/config
volumes:
  - name: seon-config
    configMap:
      name: seon-config
  - name: rendered-config
    emptyDir: {}
```

## ECS example

```json
{
  "containerDefinitions": [
    {
      "name": "app",
      "image": "your-app:latest",
      "entryPoint": ["/usr/local/bin/envfuse", "-config", "/etc/seon.json", "--"],
      "command": ["/app/server"],
      "environment": [
        { "name": "ENVFUSE_LOG_LEVEL", "value": "INFO" }
      ]
    }
  ]
}
```

ECS task roles are picked up automatically by the AWS provider — no credentials to manage.

## Building

```bash
go build ./...
go test ./...
```

## License

MIT
