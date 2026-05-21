package sync

import (
	"context"
	"errors"
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
	for k, v := range current.data {
		out[k] = v
	}
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
