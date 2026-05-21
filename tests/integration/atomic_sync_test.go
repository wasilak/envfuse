package integration_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"envfuse/internal/sync"
)

type scriptedFetchResult struct {
	data  map[string]any
	err   error
	delay time.Duration
}

type scriptedProvider struct {
	results map[string][]scriptedFetchResult
}

func (p *scriptedProvider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
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

func TestAtomicSync_HappyPathBatchSuccess(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	configPath := filepath.Join(tmpDir, "seon.json")

	secretsJSON := `{
		"app/base": {"API_KEY": "abc123"},
		"db/primary": {"PASSWORD": "secret"}
	}`
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base", "db/primary"]
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	result := sync.RunSingleCycleFromConfig(context.Background(), configPath)

	if result.Status != sync.CycleStatusSuccess {
		t.Fatalf("expected cycle status %q, got %q", sync.CycleStatusSuccess, result.Status)
	}
	if got := len(result.FetchedPaths); got != 2 {
		t.Fatalf("expected one batch result with 2 fetched paths, got %d", got)
	}
}

func TestAtomicSync_CLIOnceSuccess(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	configPath := filepath.Join(tmpDir, "seon.json")

	secretsJSON := `{
		"app/base": {"API_KEY": "abc123"},
		"db/primary": {"PASSWORD": "secret"}
	}`
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base", "db/primary"]
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	cmd := exec.Command("go", "run", "./cmd/envfuse", "-config", configPath, "-once")
	cmd.Dir = filepath.Join("..", "..")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected CLI once run to succeed, err=%v, output=%s", err, string(output))
	}
}

func TestAtomicSync_FailedCyclePreservesLastKnownGood(t *testing.T) {
	t.Parallel()

	store := sync.NewStateStore()
	provider := &scriptedProvider{
		results: map[string][]scriptedFetchResult{
			"app/base": {
				{data: map[string]any{"VERSION": "v1"}},
				{data: map[string]any{"VERSION": "v2"}},
			},
			"db/primary": {
				{data: map[string]any{"PASSWORD": "ok"}},
				{err: errors.New("fetch failed")},
			},
		},
	}

	coordinator := sync.NewCoordinatorWithStoreAndTimeout(provider, store, 50*time.Millisecond)

	first := coordinator.RunCycle(context.Background(), []string{"app/base", "db/primary"})
	if first.Status != sync.CycleStatusSuccess {
		t.Fatalf("expected first cycle success, got %q", first.Status)
	}

	snapshotA := store.LastKnownGood()
	if snapshotA["app/base"]["VERSION"] != "v1" {
		t.Fatalf("expected first cycle to apply v1, got %v", snapshotA["app/base"]["VERSION"])
	}

	second := coordinator.RunCycle(context.Background(), []string{"app/base", "db/primary"})
	if second.Status != sync.CycleStatusFailed {
		t.Fatalf("expected failed status for fetch error, got %q", second.Status)
	}

	snapshotB := store.LastKnownGood()
	if snapshotB["app/base"]["VERSION"] != "v1" {
		t.Fatalf("expected last-known-good to remain v1 after failed cycle, got %v", snapshotB["app/base"]["VERSION"])
	}
}

func TestAtomicSync_TimeoutCyclePreservesLastKnownGood(t *testing.T) {
	t.Parallel()

	store := sync.NewStateStore()
	provider := &scriptedProvider{
		results: map[string][]scriptedFetchResult{
			"app/base": {
				{data: map[string]any{"VERSION": "v1"}},
				{data: map[string]any{"VERSION": "v2"}},
			},
			"db/primary": {
				{data: map[string]any{"PASSWORD": "ok"}},
				{delay: 100 * time.Millisecond, data: map[string]any{"PASSWORD": "late"}},
			},
		},
	}

	coordinator := sync.NewCoordinatorWithStoreAndTimeout(provider, store, 10*time.Millisecond)

	first := coordinator.RunCycle(context.Background(), []string{"app/base", "db/primary"})
	if first.Status != sync.CycleStatusSuccess {
		t.Fatalf("expected first cycle success, got %q", first.Status)
	}

	snapshotA := store.LastKnownGood()
	if snapshotA["app/base"]["VERSION"] != "v1" {
		t.Fatalf("expected first cycle to apply v1, got %v", snapshotA["app/base"]["VERSION"])
	}

	second := coordinator.RunCycle(context.Background(), []string{"app/base", "db/primary"})
	if second.Status != sync.CycleStatusAborted {
		t.Fatalf("expected aborted status for timeout cancellation, got %q", second.Status)
	}

	snapshotB := store.LastKnownGood()
	if snapshotB["app/base"]["VERSION"] != "v1" {
		t.Fatalf("expected last-known-good to remain v1 after timeout cycle, got %v", snapshotB["app/base"]["VERSION"])
	}
}
