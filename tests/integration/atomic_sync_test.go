package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"envfuse/internal/sync"
)

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
