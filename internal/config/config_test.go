package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"envfuse/internal/config"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "seon.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadConfig_VaultHTTPRejected(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
		"provider_type": "vault",
		"vault_address": "http://vault.example.com",
		"vault_token": "root",
		"secret_paths": ["app/config"]
	}`)

	_, err := config.LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for http:// vault_address, got nil")
	}
}

func TestLoadConfig_VaultEmptyAddress(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
		"provider_type": "vault",
		"vault_token": "root",
		"secret_paths": ["app/config"]
	}`)

	// Empty vault_address is valid — VAULT_ADDR env var is used by the provider.
	_, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("expected no error for empty vault_address, got: %v", err)
	}
}

func TestLoadConfig_VaultHTTPSAddressAccepted(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
		"provider_type": "vault",
		"vault_address": "https://vault.example.com",
		"vault_token": "root",
		"secret_paths": ["app/config"]
	}`)

	_, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("expected https:// vault_address to be accepted, got: %v", err)
	}
}
