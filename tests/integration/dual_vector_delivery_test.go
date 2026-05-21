package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestDualVector_Templates(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	renderedPath := filepath.Join(tmpDir, "app.env")
	configPath := filepath.Join(tmpDir, "seon.json")

	secretsJSON := `{
		"app/base": {"API_KEY": "abc123"}
	}`
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		],
		"templates": [
			{
				"name": "app-env",
				"content": "APP_API_KEY={{ index (index . \"app/base\") \"API_KEY\" }}\\n",
				"output_path": "` + renderedPath + `",
				"required_paths": ["app/base"]
			}
		]
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	res := sync.RunSingleCycleFromConfig(context.Background(), configPath)
	if res.Status != sync.CycleStatusSuccess {
		t.Fatalf("expected success status for template + env cycle, got %q (%s)", res.Status, res.ErrorClass)
	}

	rendered, err := os.ReadFile(renderedPath)
	if err != nil {
		t.Fatalf("expected rendered file to exist, read failed: %v", err)
	}
	if got := strings.TrimSpace(string(rendered)); got != "APP_API_KEY=abc123" {
		t.Fatalf("expected rendered file content APP_API_KEY=abc123, got %q", got)
	}
}

func TestDualVector_Atomic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	renderedPath := filepath.Join(tmpDir, "app.env")
	configPath := filepath.Join(tmpDir, "seon.json")

	if err := os.WriteFile(renderedPath, []byte("LAST_GOOD=v1\n"), 0o600); err != nil {
		t.Fatalf("seed last-known-good rendered file: %v", err)
	}

	secretsJSON := `{
		"app/base": {"API_KEY": "abc123"}
	}`
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		],
		"templates": [
			{
				"name": "app-env",
				"content": "APP_API_KEY={{ index (index . \"app/base\") \"MISSING\" }}\\n",
				"output_path": "` + renderedPath + `",
				"required_paths": ["app/base"]
			}
		]
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	res := sync.RunSingleCycleFromConfig(context.Background(), configPath)
	if res.Status != sync.CycleStatusFailed {
		t.Fatalf("expected failed status when template branch fails, got %q", res.Status)
	}
	if res.ErrorClass != "template_missing_key" {
		t.Fatalf("expected error class template_missing_key, got %q", res.ErrorClass)
	}

	rendered, err := os.ReadFile(renderedPath)
	if err != nil {
		t.Fatalf("expected last-known-good rendered file to still exist, read failed: %v", err)
	}
	if got := strings.TrimSpace(string(rendered)); got != "LAST_GOOD=v1" {
		t.Fatalf("expected rendered file to stay on last-known-good value, got %q", got)
	}
}

func TestDualVector_ChildStartEnv(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	configPath := filepath.Join(tmpDir, "seon.json")
	childOutPath := filepath.Join(tmpDir, "child-start.txt")

	secretsJSON := `{
		"app/base": {"API_KEY": "abc123"}
	}`
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		]
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	cmd := exec.Command("go", "run", "./cmd/envfuse", "-config", configPath, "-once", "--", "/bin/sh", "-c", "printf '%s' \"$APP_API_KEY\" > '"+childOutPath+"'")
	cmd.Dir = filepath.Join("..", "..")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("envfuse run failed: %v\noutput=%s", err, string(output))
	}

	got, err := os.ReadFile(childOutPath)
	if err != nil {
		t.Fatalf("expected child output file to exist, read failed: %v", err)
	}
	if string(got) != "abc123" {
		t.Fatalf("expected child to receive APP_API_KEY=abc123, got %q", string(got))
	}
}

func TestDualVector_ChildRestartEnv(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	configPath := filepath.Join(tmpDir, "seon.json")
	childOutPath := filepath.Join(tmpDir, "child-restart.txt")

	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	writeSecrets := func(v string) {
		t.Helper()
		payload, err := json.Marshal(map[string]map[string]string{"app/base": {"API_KEY": v}})
		if err != nil {
			t.Fatalf("marshal secrets: %v", err)
		}
		if err := os.WriteFile(secretsPath, payload, 0o600); err != nil {
			t.Fatalf("write secrets: %v", err)
		}
	}

	runChild := func() {
		t.Helper()
		cmd := exec.Command("go", "run", "./cmd/envfuse", "-config", configPath, "-once", "--", "/bin/sh", "-c", "printf '%s' \"$APP_API_KEY\" > '"+childOutPath+"'")
		cmd.Dir = filepath.Join("..", "..")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("envfuse run failed: %v\noutput=%s", err, string(output))
		}
	}

	writeSecrets("v1")
	runChild()
	first, err := os.ReadFile(childOutPath)
	if err != nil {
		t.Fatalf("read first child output: %v", err)
	}
	if string(first) != "v1" {
		t.Fatalf("expected first launch env to be v1, got %q", string(first))
	}

	writeSecrets("v2")
	runChild()
	second, err := os.ReadFile(childOutPath)
	if err != nil {
		t.Fatalf("read second child output: %v", err)
	}
	if string(second) != "v2" {
		t.Fatalf("expected restart launch env to be v2, got %q", string(second))
	}
}

func TestDualVector_MissingKey(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	renderedPath := filepath.Join(tmpDir, "app.env")
	configPath := filepath.Join(tmpDir, "seon.json")

	if err := os.WriteFile(renderedPath, []byte("LAST_GOOD=v1\n"), 0o600); err != nil {
		t.Fatalf("seed rendered file: %v", err)
	}

	secretsJSON := `{
		"app/base": {"API_KEY": "abc123"}
	}`
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		],
		"templates": [
			{
				"name": "app-env",
				"content": "APP_API_KEY={{ index (index . \"app/base\") \"MISSING\" }}\\n",
				"output_path": "` + renderedPath + `",
				"required_paths": ["app/base"]
			}
		]
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	res := sync.RunSingleCycleFromConfig(context.Background(), configPath)
	if res.Status != sync.CycleStatusFailed {
		t.Fatalf("expected failed status when required template key is missing, got %q", res.Status)
	}
	if res.ErrorClass != "template_missing_key" {
		t.Fatalf("expected error class template_missing_key, got %q", res.ErrorClass)
	}

	rendered, err := os.ReadFile(renderedPath)
	if err != nil {
		t.Fatalf("expected previous rendered file to remain after failure, read failed: %v", err)
	}
	if got := strings.TrimSpace(string(rendered)); got != "LAST_GOOD=v1" {
		t.Fatalf("expected last-known-good rendered file to remain unchanged, got %q", got)
	}
}

func TestDualVector_PathGuard(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	renderedPath := filepath.Join(tmpDir, "app.env")
	configPath := filepath.Join(tmpDir, "seon.json")

	if err := os.WriteFile(renderedPath, []byte("LAST_GOOD=v1\n"), 0o600); err != nil {
		t.Fatalf("seed rendered file: %v", err)
	}

	secretsJSON := `{
		"app/base": {"API_KEY": "abc123"}
	}`
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		],
		"templates": [
			{
				"name": "app-env",
				"content": "APP_API_KEY={{ index (index . \"app/base\") \"API_KEY\" }}\\n",
				"output_path": "../outside.env",
				"required_paths": ["app/base"]
			}
		]
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	res := sync.RunSingleCycleFromConfig(context.Background(), configPath)
	if res.Status != sync.CycleStatusFailed {
		t.Fatalf("expected failed status for unsafe output path, got %q", res.Status)
	}
	if res.ErrorClass != "path_disallowed" {
		t.Fatalf("expected error class path_disallowed, got %q", res.ErrorClass)
	}

	rendered, err := os.ReadFile(renderedPath)
	if err != nil {
		t.Fatalf("expected previous rendered file to remain after path failure, read failed: %v", err)
	}
	if got := strings.TrimSpace(string(rendered)); got != "LAST_GOOD=v1" {
		t.Fatalf("expected last-known-good rendered file to remain unchanged, got %q", got)
	}
}
