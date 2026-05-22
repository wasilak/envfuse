# Phase 5: Operational Safety & Observability - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-22
**Phase:** 05-operational-safety-observability
**Areas discussed:** Observability surface, Log format, Redaction guarantee, Event granularity

---

## Observability Surface

| Option | Description | Selected |
|--------|-------------|----------|
| Stderr log lines only | Container runtimes collect stderr and forward to log aggregators. Zero new surface area. | ✓ |
| Stderr + /metrics HTTP endpoint | Prometheus-compatible endpoint for cycle latency and restart counters. Adds HTTP listener and port config. | |
| Stderr + structured events file | Machine-readable NDJSON events file. Avoids HTTP but introduces file lifecycle management. | |

**User's choice:** Stderr log lines only

| Option | Description | Selected |
|--------|-------------|----------|
| Shared stderr — envfuse and child mix on the same stream | Simple. Container log collectors handle mixed output. envfuse lines can be structured to distinguish them. | ✓ |
| Separate streams — envfuse writes to a dedicated fd or file | Cleaner log separation but introduces complexity for a PID 1. | |

**User's choice:** Shared stderr

---

## Log Format

| Option | Description | Selected |
|--------|-------------|----------|
| Structured JSON via stdlib slog (JSONHandler) | slog.JSONHandler writes JSON to stderr. No new dependency, plays well with CloudWatch/Datadog/Loki. | ✓ |
| Plain text via stdlib slog (TextHandler) | Human-readable key=value pairs. Log aggregators prefer JSON. | |
| Raw fmt.Fprintf to stderr | No library. Simple prefix. Hard to query by field in aggregators. | |

**User's choice:** Structured JSON via stdlib slog (JSONHandler)

| Option | Description | Selected |
|--------|-------------|----------|
| Single INFO level — no debug mode | All cycle events at INFO. Keeps config surface small. | |
| Configurable level via env var (ENVFUSE_LOG_LEVEL) | Operators set DEBUG for verbose fetch details. Must also redact secrets at DEBUG. | ✓ |
| Configurable level via seon.json log_level field | Config-driven verbosity. Consistent with existing seon.json approach but adds a config field. | |

**User's choice:** Configurable level via env var (ENVFUSE_LOG_LEVEL)

| Option | Description | Selected |
|--------|-------------|----------|
| DEBUG / INFO (two levels) | Covers the common troubleshooting case. | |
| DEBUG / INFO / WARN / ERROR (full slog levels) | Full slog level set. More granular but more surface to define in v1. | ✓ |

**User's choice:** DEBUG / INFO / WARN / ERROR (full slog levels)

---

## Redaction Guarantee

| Option | Description | Selected |
|--------|-------------|----------|
| Passive only — code discipline + tests | Log calls only receive ErrorClass, path names, counts, durations. Tests verify no secret values in log output. | ✓ |
| Active scrubber — wrap logger output through a redactor | Intercepts all log output and strips known secret values. Complex; scrubbing can produce misleading output. | |
| Type-safe wrapper — SecretString type that refuses to String() | Wrap secret values in a Go type returning [REDACTED]. Compile-time-ish guarantee but threads through all data paths. | |

**User's choice:** Passive only — code discipline + tests

| Option | Description | Selected |
|--------|-------------|----------|
| Provider fetch errors | Vault and AWS errors most likely to wrap secret values from API responses. | |
| Template rendering errors | Missing-key errors from text/template could include partial values. | |
| Config validation errors | Config errors reference field values like vault_token. | |
| All of the above | Test all three paths explicitly. Higher confidence, more test code. | ✓ |

**User's choice:** All of the above — test provider errors, render errors, and config errors.

---

## Event Granularity

**INFO-level events** (all four selected):
| Event | Selected |
|-------|----------|
| Startup summary (provider_type, path counts, env_var count, fingerprint) | ✓ |
| Cycle outcome (status, latency_ms, error_class) | ✓ |
| Fingerprint change + restart (old/new fingerprint, restart_reason) | ✓ |
| Shutdown + exit reason (exit_reason, exit_code) | ✓ |

**DEBUG-level events** (all four selected):
| Event | Selected |
|-------|----------|
| Individual path fetch results | ✓ |
| Env var names applied (not values) | ✓ |
| Template render details | ✓ |
| Cooldown state | ✓ |

**Logger architecture:**

| Option | Description | Selected |
|--------|-------------|----------|
| main.go + supervisor only — keep internal packages log-free | Internal packages return structured results/errors. main.go and supervisor translate to log events. | |
| Pass logger into coordinator and supervisor | More granular events but couples internals to a logger interface. | |
| Package-level logger in each package | Easy to add but breaks testability and creates log coupling. | |
| Global slog.SetDefault in main.go | slog.SetDefault called once; all packages use slog.* directly. No injection required. | ✓ |

**User's choice (freeform):** "somewhere top level by declaring global slog options, so later every package can directly use slog.* funcs without need to pass logger around"

---

## Claude's Discretion

- Exact slog attribute key names (e.g., `latency_ms` vs `duration_ms`) — follow slog conventions, be consistent.
- Whether to emit a pre-first-cycle startup INFO line (e.g., "envfuse starting, provider=vault").
- Fingerprint truncation length for log output (8 chars suggested).

## Deferred Ideas

None — discussion stayed within phase scope.
