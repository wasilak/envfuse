package sync

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"envfuse/internal/config"
	"envfuse/internal/state"
)

// captureLog redirects slog output to a buffer for the duration of the test.
// The returned restore function must be called (via defer) to reset the default logger.
func captureLog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	return buf, func() { slog.SetDefault(orig) }
}

// errorProvider always returns a fixed error from Fetch.
type errorProvider struct{ err error }

func (p *errorProvider) Fetch(_ context.Context, _ string) (map[string]any, error) {
	return nil, p.err
}

// staticProvider returns a fixed dataset keyed by path.
type staticProvider struct{ data map[string]map[string]any }

func (p *staticProvider) Fetch(_ context.Context, path string) (map[string]any, error) {
	if d, ok := p.data[path]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("path not found: %s", path)
}

// TestRedaction_ProviderFetchError asserts that a secret value embedded in a provider
// error message does not appear anywhere in captured log output (SECU-01, D-07).
//
// The coordinator returns only ErrorClass codes ("fetch_failed") — not raw error text —
// so even when the upstream error contains a sensitive token, logs must stay clean.
func TestRedaction_ProviderFetchError(t *testing.T) {
	t.Parallel()

	const fakeSecret = "s.fake-vault-token-abc123"

	// Provider whose error message contains the fake secret (simulates a badly
	// formatted upstream error, e.g. Vault returning a token in its error body).
	p := &errorProvider{err: fmt.Errorf("request failed with token %s", fakeSecret)}

	store := state.NewStore()
	c := NewCoordinatorWithStore(p, store, 100*time.Millisecond)

	buf, restore := captureLog(t)
	defer restore()

	result := c.RunCycle(context.Background(), []string{"secret/app"})

	if result.Status == CycleStatusSuccess {
		t.Fatalf("expected non-success status, got %q", result.Status)
	}

	if strings.Contains(buf.String(), fakeSecret) {
		t.Errorf("log output must not contain fake secret value\nlog output: %s", buf.String())
	}
}

// TestRedaction_TemplateRenderError asserts that secret values fetched from the provider
// do not appear in log output when a template render fails (SECU-01, D-07).
//
// The coordinator logs only ErrorClass codes, not template content or fetched values,
// so the secret in the fetched data must never surface in logs.
func TestRedaction_TemplateRenderError(t *testing.T) {
	t.Parallel()

	const fakeSecret = "s.fake-template-secret-xyz789"

	p := &staticProvider{data: map[string]map[string]any{
		"secret/app": {"password": fakeSecret},
	}}

	store := state.NewStore()
	c := NewCoordinatorWithStore(p, store, 100*time.Millisecond)

	tmpDir := t.TempDir()
	// Template references an undefined top-level key, which causes missingkey=error to fire.
	templates := []config.TemplateSpec{{
		Name:       "bad-template",
		Content:    `{{ .undefined_top_level_key }}`,
		OutputPath: filepath.Join(tmpDir, "out.txt"),
	}}

	buf, restore := captureLog(t)
	defer restore()

	result := c.RunCycleWithVectors(context.Background(), []string{"secret/app"}, nil, templates)

	if result.Status == CycleStatusSuccess {
		t.Fatalf("expected non-success status, got %q", result.Status)
	}

	if strings.Contains(buf.String(), fakeSecret) {
		t.Errorf("log output must not contain fake secret value\nlog output: %s", buf.String())
	}
}

// TestRedaction_ConfigValidationError asserts that a vault token embedded in a config
// file does not appear in log output when config loading fails (SECU-01, D-07).
//
// An unknown JSON field triggers DisallowUnknownFields, producing a config_invalid
// error before any provider is constructed or any network call is made.
func TestRedaction_ConfigValidationError(t *testing.T) {
	t.Parallel()

	const fakeSecret = "s.fake-config-token-def456"

	// Config has an unknown field to trigger DisallowUnknownFields decode error.
	// vault_token contains the fake secret to verify it never appears in logs.
	cfgContent := fmt.Sprintf(`{
		"provider_type": "vault",
		"vault_address": "https://vault.example.com",
		"vault_token": %q,
		"vault_mount": "secret",
		"secret_paths": ["secret/app"],
		"unknown_field_triggers_invalid": true
	}`, fakeSecret)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "seon.json")
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	buf, restore := captureLog(t)
	defer restore()

	result := RunSingleCycleWithStoreFromConfig(context.Background(), cfgPath, state.NewStore())

	if result.Status == CycleStatusSuccess {
		t.Fatalf("expected non-success status, got %q", result.Status)
	}
	if result.ErrorClass != "config_invalid" {
		t.Fatalf("expected error_class=config_invalid, got %q", result.ErrorClass)
	}

	if strings.Contains(buf.String(), fakeSecret) {
		t.Errorf("log output must not contain vault token value even on config validation error\nlog output: %s", buf.String())
	}
}
