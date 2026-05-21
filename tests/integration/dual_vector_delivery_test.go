package integration_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"envfuse/internal/config"
	"envfuse/internal/sync"
)

type scriptedEnvResult struct {
	data  map[string]any
	err   error
	delay time.Duration
}

type scriptedEnvProvider struct {
	results map[string][]scriptedEnvResult
}

func (p *scriptedEnvProvider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
	seq, ok := p.results[resourceURI]
	if !ok || len(seq) == 0 {
		return nil, errors.New("missing scripted result")
	}

	current := seq[0]
	p.results[resourceURI] = seq[1:]

	if current.delay > 0 {
		select {
		case <-time.After(current.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if current.err != nil {
		return nil, current.err
	}

	out := make(map[string]any, len(current.data))
	for k, v := range current.data {
		out[k] = v
	}

	return out, nil
}

func TestDualVector_EnvMappingSuccessMaterializesDeclaredChildEnv(t *testing.T) {
	t.Parallel()

	store := sync.NewStateStore()
	provider := &scriptedEnvProvider{
		results: map[string][]scriptedEnvResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "abc123", "EXTRA": "ignored"}},
			},
			"db/primary": {
				{data: map[string]any{"PASSWORD": "secret"}},
			},
		},
	}

	mappings := []config.EnvMapping{
		{Path: "app/base", Key: "API_KEY", EnvVar: "APP_API_KEY"},
		{Path: "db/primary", Key: "PASSWORD", EnvVar: "DB_PASSWORD"},
	}

	coordinator := sync.NewCoordinatorWithStoreAndTimeout(provider, store, 50*time.Millisecond)
	result := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base", "db/primary"}, mappings)

	if result.Status != sync.CycleStatusSuccess {
		t.Fatalf("expected success status, got %q", result.Status)
	}
	if result.ErrorClass != "" {
		t.Fatalf("expected empty error class, got %q", result.ErrorClass)
	}

	appliedEnv := store.LastAppliedEnv()
	if got := appliedEnv["APP_API_KEY"]; got != "abc123" {
		t.Fatalf("expected APP_API_KEY to be abc123, got %q", got)
	}
	if got := appliedEnv["DB_PASSWORD"]; got != "secret" {
		t.Fatalf("expected DB_PASSWORD to be secret, got %q", got)
	}
	if _, ok := appliedEnv["EXTRA"]; ok {
		t.Fatalf("expected undeclared env var EXTRA to be absent")
	}
	if _, ok := appliedEnv["HOME"]; ok {
		t.Fatalf("expected host env var HOME to never be injected implicitly")
	}
}

func TestDualVector_EnvMappingMissingKeyFailsDeterministically(t *testing.T) {
	t.Parallel()

	store := sync.NewStateStore()
	provider := &scriptedEnvProvider{
		results: map[string][]scriptedEnvResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "abc123"}},
			},
		},
	}

	mappings := []config.EnvMapping{
		{Path: "app/base", Key: "MISSING", EnvVar: "APP_API_KEY"},
	}

	coordinator := sync.NewCoordinatorWithStoreAndTimeout(provider, store, 50*time.Millisecond)
	result := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base"}, mappings)

	if result.Status != sync.CycleStatusFailed {
		t.Fatalf("expected failed status for missing env mapping key, got %q", result.Status)
	}
	if result.ErrorClass != "env_mapping_unresolved" {
		t.Fatalf("expected deterministic error class env_mapping_unresolved, got %q", result.ErrorClass)
	}
}

func TestDualVector_EnvMappingFailurePreservesLastKnownGoodAppliedEnv(t *testing.T) {
	t.Parallel()

	store := sync.NewStateStore()
	provider := &scriptedEnvProvider{
		results: map[string][]scriptedEnvResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "v1"}},
				{data: map[string]any{"API_KEY": "v2"}},
			},
		},
	}

	coordinator := sync.NewCoordinatorWithStoreAndTimeout(provider, store, 50*time.Millisecond)

	first := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "API_KEY",
		EnvVar: "APP_API_KEY",
	}})
	if first.Status != sync.CycleStatusSuccess {
		t.Fatalf("expected initial success, got %q", first.Status)
	}

	before := store.LastAppliedEnv()
	if before["APP_API_KEY"] != "v1" {
		t.Fatalf("expected initial last-known-good APP_API_KEY=v1, got %q", before["APP_API_KEY"])
	}

	second := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "MISSING",
		EnvVar: "APP_API_KEY",
	}})
	if second.Status != sync.CycleStatusFailed {
		t.Fatalf("expected failed status for unresolved mapping, got %q", second.Status)
	}
	if second.ErrorClass != "env_mapping_unresolved" {
		t.Fatalf("expected deterministic error class env_mapping_unresolved, got %q", second.ErrorClass)
	}

	after := store.LastAppliedEnv()
	if after["APP_API_KEY"] != "v1" {
		t.Fatalf("expected last-known-good APP_API_KEY to remain v1 after failed cycle, got %q", after["APP_API_KEY"])
	}
}

func TestDualVector_EnvMappingConfigAcceptsEnvMappingsInStrictSchema(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := tmpDir + "/seon.json"

	configJSON := `{
		"provider_type": "local",
		"local_file_path": "/tmp/secrets.json",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		]
	}`

	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	if _, err := config.LoadConfig(configPath); err != nil {
		t.Fatalf("expected strict schema to accept env_mappings, got error: %v", err)
	}
}
