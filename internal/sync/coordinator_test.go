package sync

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"envfuse/internal/config"
	"envfuse/internal/clock"
	"envfuse/internal/state"
)

type stubClock struct{}

func (stubClock) Now() time.Time { return time.Unix(0, 0) }

type blockingProvider struct {
	mu       sync.Mutex
	started  map[string]struct{}
	startCh  chan string
	releaseC chan struct{}
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{
		started:  map[string]struct{}{},
		startCh:  make(chan string, 2),
		releaseC: make(chan struct{}),
	}
}

func (p *blockingProvider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
	p.mu.Lock()
	p.started[resourceURI] = struct{}{}
	p.mu.Unlock()
	p.startCh <- resourceURI

	select {
	case <-p.releaseC:
		return map[string]any{"ok": true}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRunCycle_FetchesDistinctPathsConcurrently(t *testing.T) {
	t.Parallel()

	p := newBlockingProvider()
	coord := NewCoordinator(p)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan CycleResult, 1)
	go func() {
		resultCh <- coord.RunCycle(ctx, []string{"app/base", "db/primary"})
	}()

	seen := map[string]struct{}{}
	for len(seen) < 2 {
		select {
		case path := <-p.startCh:
			seen[path] = struct{}{}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("expected both paths to start fetch concurrently, got starts: %v", seen)
		}
	}

	close(p.releaseC)

	select {
	case res := <-resultCh:
		if res.Status != CycleStatusSuccess {
			t.Fatalf("expected success status, got %q", res.Status)
		}
		if len(res.FetchedPaths) != 2 {
			t.Fatalf("expected 2 fetched paths, got %d", len(res.FetchedPaths))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("cycle did not complete after releasing provider")
	}
}

type scriptedUnitResult struct {
	data  map[string]any
	err   error
	delay time.Duration
}

type scriptedUnitProvider struct {
	mu      sync.Mutex
	results map[string][]scriptedUnitResult
}

func (p *scriptedUnitProvider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
	p.mu.Lock()
	seq := p.results[resourceURI]
	if len(seq) == 0 {
		p.mu.Unlock()
		return nil, errors.New("missing scripted result")
	}
	current := seq[0]
	p.results[resourceURI] = seq[1:]
	p.mu.Unlock()

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
	maps.Copy(out, current.data)
	return out, nil
}

func TestRunCycle_FirstErrorFailsCycle(t *testing.T) {
	t.Parallel()

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{err: errors.New("boom")},
			},
			"db/primary": {
				{data: map[string]any{"PASSWORD": "v2"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 100*time.Millisecond, stubClock{})
	result := coordinator.RunCycle(context.Background(), []string{"app/base", "db/primary"})

	if result.Status != CycleStatusFailed {
		t.Fatalf("expected failed cycle status, got %q", result.Status)
	}

	if len(store.LastKnownGood()) != 0 {
		t.Fatalf("expected no commit on failed cycle")
	}
}

func TestRunCycle_TimeoutAbortsCycle(t *testing.T) {
	t.Parallel()

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{data: map[string]any{"VERSION": "v2"}},
			},
			"db/primary": {
				{delay: 100 * time.Millisecond, data: map[string]any{"PASSWORD": "late"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 10*time.Millisecond, stubClock{})
	result := coordinator.RunCycle(context.Background(), []string{"app/base", "db/primary"})

	if result.Status != CycleStatusAborted {
		t.Fatalf("expected aborted cycle status, got %q", result.Status)
	}

	if len(store.LastKnownGood()) != 0 {
		t.Fatalf("expected no commit on aborted cycle")
	}
}

var _ clock.Clock = stubClock{}

func TestRunCycle_EnvMappingMissingKeyClassified(t *testing.T) {
	t.Parallel()

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "abc"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 100*time.Millisecond, stubClock{})
	result := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "MISSING",
		EnvVar: "APP_API_KEY",
	}})

	if result.Status != CycleStatusFailed {
		t.Fatalf("expected failed status, got %q", result.Status)
	}
	if result.ErrorClass != "env_mapping_unresolved" {
		t.Fatalf("expected env_mapping_unresolved, got %q", result.ErrorClass)
	}
}

func TestRunCycle_EnvMappingMissingPathClassified(t *testing.T) {
	t.Parallel()

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "abc"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 100*time.Millisecond, stubClock{})
	result := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "missing/path",
		Key:    "API_KEY",
		EnvVar: "APP_API_KEY",
	}})

	if result.Status != CycleStatusFailed {
		t.Fatalf("expected failed status, got %q", result.Status)
	}
	if result.ErrorClass != "env_mapping_unresolved" {
		t.Fatalf("expected env_mapping_unresolved, got %q", result.ErrorClass)
	}
}

func TestRunCycle_NoCommitOnEnvValidationFailure(t *testing.T) {
	t.Parallel()

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "v1"}},
				{data: map[string]any{"API_KEY": "v2"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 100*time.Millisecond, stubClock{})
	first := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "API_KEY",
		EnvVar: "APP_API_KEY",
	}})
	if first.Status != CycleStatusSuccess {
		t.Fatalf("expected initial success, got %q", first.Status)
	}
	if got := store.LastAppliedEnv()["APP_API_KEY"]; got != "v1" {
		t.Fatalf("expected APP_API_KEY=v1 after first cycle, got %q", got)
	}

	second := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "MISSING",
		EnvVar: "APP_API_KEY",
	}})
	if second.Status != CycleStatusFailed {
		t.Fatalf("expected failed status, got %q", second.Status)
	}
	if second.ErrorClass != "env_mapping_unresolved" {
		t.Fatalf("expected env_mapping_unresolved, got %q", second.ErrorClass)
	}

	if got := store.LastAppliedEnv()["APP_API_KEY"]; got != "v1" {
		t.Fatalf("expected APP_API_KEY to remain v1 after failed cycle, got %q", got)
	}
}

func TestRunCycle_EnvMappingDuplicateEnvVarRejected(t *testing.T) {
	t.Parallel()

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "abc"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 100*time.Millisecond, stubClock{})
	result := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base"}, []config.EnvMapping{
		{Path: "app/base", Key: "API_KEY", EnvVar: "APP_API_KEY"},
		{Path: "app/base", Key: "API_KEY", EnvVar: "APP_API_KEY"},
	})

	if result.Status != CycleStatusFailed {
		t.Fatalf("expected failed status for duplicate env var mapping, got %q", result.Status)
	}
	if result.ErrorClass != "env_mapping_invalid" {
		t.Fatalf("expected env_mapping_invalid, got %q", result.ErrorClass)
	}
	if len(store.LastAppliedEnv()) != 0 {
		t.Fatalf("expected no committed env payload on validation failure")
	}
}

func TestRunCycle_EnvMappingCommitsOnlyDeclaredEnvVars(t *testing.T) {
	t.Parallel()

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "abc", "EXTRA": "ignored"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 100*time.Millisecond, stubClock{})
	result := coordinator.RunCycleWithEnvMappings(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "API_KEY",
		EnvVar: "APP_API_KEY",
	}})

	if result.Status != CycleStatusSuccess {
		t.Fatalf("expected success status, got %q", result.Status)
	}

	applied := store.LastAppliedEnv()
	if got := applied["APP_API_KEY"]; got != "abc" {
		t.Fatalf("expected APP_API_KEY=abc, got %q", got)
	}
	if _, ok := applied["EXTRA"]; ok {
		t.Fatalf("expected undeclared key EXTRA to be absent")
	}
	if _, ok := applied["HOME"]; ok {
		t.Fatalf("expected host key HOME to never be implicitly injected")
	}
}

func TestRunCycle_TemplateMissingKey(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	renderedPath := filepath.Join(tmpDir, "app.env")

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "abc"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 100*time.Millisecond, stubClock{})
	result := coordinator.RunCycleWithVectors(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "API_KEY",
		EnvVar: "APP_API_KEY",
	}}, []config.TemplateSpec{{
		Name:       "env",
		Content:    "APP_API_KEY={{ index (index . \"app/base\") \"MISSING\" }}\\n",
		OutputPath: renderedPath,
	}})

	if result.Status != CycleStatusFailed {
		t.Fatalf("expected failed status, got %q", result.Status)
	}
	if result.ErrorClass != "template_missing_key" {
		t.Fatalf("expected template_missing_key, got %q", result.ErrorClass)
	}
	if len(store.LastKnownGood()) != 0 {
		t.Fatalf("expected no commit on template missing key")
	}
	if len(store.LastAppliedEnv()) != 0 {
		t.Fatalf("expected no env payload commit on template missing key")
	}
}

func TestRunCycle_DisallowedTarget(t *testing.T) {
	t.Parallel()

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "abc"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 100*time.Millisecond, stubClock{})
	result := coordinator.RunCycleWithVectors(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "API_KEY",
		EnvVar: "APP_API_KEY",
	}}, []config.TemplateSpec{{
		Name:       "env",
		Content:    "APP_API_KEY={{ index (index . \"app/base\") \"API_KEY\" }}\\n",
		OutputPath: "../outside.env",
	}})

	if result.Status != CycleStatusFailed {
		t.Fatalf("expected failed status, got %q", result.Status)
	}
	if result.ErrorClass != "path_disallowed" {
		t.Fatalf("expected path_disallowed, got %q", result.ErrorClass)
	}
	if len(store.LastKnownGood()) != 0 {
		t.Fatalf("expected no commit on disallowed target")
	}
	if len(store.LastAppliedEnv()) != 0 {
		t.Fatalf("expected no env payload commit on disallowed target")
	}
}

func TestRunCycle_DualVectorCommitGate(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	renderedPath := filepath.Join(tmpDir, "app.env")

	store := state.NewStore()
	provider := &scriptedUnitProvider{
		results: map[string][]scriptedUnitResult{
			"app/base": {
				{data: map[string]any{"API_KEY": "v1"}},
				{data: map[string]any{"API_KEY": "v2"}},
			},
		},
	}

	coordinator := newCoordinatorWithClock(provider, store, 100*time.Millisecond, stubClock{})
	first := coordinator.RunCycleWithVectors(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "API_KEY",
		EnvVar: "APP_API_KEY",
	}}, []config.TemplateSpec{{
		Name:       "env",
		Content:    "APP_API_KEY={{ index (index . \"app/base\") \"API_KEY\" }}\\n",
		OutputPath: renderedPath,
	}})
	if first.Status != CycleStatusSuccess {
		t.Fatalf("expected first cycle success, got %q", first.Status)
	}
	if got := store.LastAppliedEnv()["APP_API_KEY"]; got != "v1" {
		t.Fatalf("expected APP_API_KEY=v1 after first cycle, got %q", got)
	}
	if got := string(store.LastRenderedFiles()[renderedPath]); got != "APP_API_KEY=v1\\n" {
		t.Fatalf("expected rendered payload APP_API_KEY=v1\\n, got %q", got)
	}

	second := coordinator.RunCycleWithVectors(context.Background(), []string{"app/base"}, []config.EnvMapping{{
		Path:   "app/base",
		Key:    "API_KEY",
		EnvVar: "APP_API_KEY",
	}}, []config.TemplateSpec{{
		Name:       "env",
		Content:    "APP_API_KEY={{ index (index . \"app/base\") \"MISSING\" }}\\n",
		OutputPath: renderedPath,
	}})
	if second.Status != CycleStatusFailed {
		t.Fatalf("expected failed status, got %q", second.Status)
	}
	if second.ErrorClass != "template_missing_key" {
		t.Fatalf("expected template_missing_key, got %q", second.ErrorClass)
	}

	if got := store.LastAppliedEnv()["APP_API_KEY"]; got != "v1" {
		t.Fatalf("expected APP_API_KEY to remain v1 after failed cycle, got %q", got)
	}
	if got := string(store.LastRenderedFiles()[renderedPath]); got != "APP_API_KEY=v1\\n" {
		t.Fatalf("expected rendered payload to remain APP_API_KEY=v1\\n after failed cycle, got %q", got)
	}

	rendered, err := os.ReadFile(renderedPath)
	if err != nil {
		t.Fatalf("expected committed rendered file to exist, read failed: %v", err)
	}
	if got := string(rendered); got != "APP_API_KEY=v1\\n" {
		t.Fatalf("expected on-disk rendered file to remain APP_API_KEY=v1\\n, got %q", got)
	}
}

func TestCoordinator_ProviderFactory_VaultHTTPRejected(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := tmpDir + "/seon.json"
	configJSON := `{
		"provider_type": "vault",
		"vault_address": "http://vault.example.com",
		"vault_token": "root",
		"secret_paths": ["app/config"]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := state.NewStore()
	result := RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)

	if result.Status != CycleStatusFailed {
		t.Fatalf("expected CycleStatusFailed, got %q", result.Status)
	}
	// http:// vault_address must be rejected — either at config load (config_invalid)
	// or provider init (provider_init_failed)
	if result.ErrorClass != "config_invalid" && result.ErrorClass != "provider_init_failed" {
		t.Fatalf("expected config_invalid or provider_init_failed, got %q", result.ErrorClass)
	}
}

func TestCoordinator_ProviderFactory_AWS(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := tmpDir + "/seon.json"
	configJSON := `{
		"provider_type": "aws",
		"aws_region": "us-east-1",
		"secret_paths": ["myapp/config"]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := state.NewStore()
	result := RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)

	// The AWS provider initializes successfully (no real AWS calls needed for factory test),
	// but the fetch will fail because there are no real AWS credentials in CI.
	// We just verify the provider was constructed (no "not yet implemented" error).
	if result.ErrorClass == "provider_init_failed" {
		t.Fatalf("expected AWS provider to initialize, got provider_init_failed — stub may still be in place")
	}
}

func TestCoordinator_ProviderFactory_UnknownProvider(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := tmpDir + "/seon.json"
	configJSON := `{
		"provider_type": "unknown_provider_xyz",
		"secret_paths": ["app/config"]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := state.NewStore()
	result := RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)

	if result.Status != CycleStatusFailed {
		t.Fatalf("expected CycleStatusFailed, got %q", result.Status)
	}
	if result.ErrorClass != "provider_init_failed" {
		t.Fatalf("expected provider_init_failed, got %q", result.ErrorClass)
	}
}
