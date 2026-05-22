package integration_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"envfuse/internal/sync"
)

// TestProviderSwitching proves PROV-04: switching providers requires only changing
// provider_type (and provider-specific fields) — secret_paths, env_mappings, and
// templates are provider-agnostic.
func TestProviderSwitching(t *testing.T) {
	t.Parallel()

	// Shared secret content used across all providers.
	const secretPath = "app/config"
	secretData := map[string]any{"API_KEY": "abc123"}

	tmpDir := t.TempDir()

	// Case 1: local provider.
	t.Run("local_provider_succeeds", func(t *testing.T) {
		t.Parallel()

		secretsPath := filepath.Join(tmpDir, "local-secrets.json")
		secretsContent := map[string]any{secretPath: secretData}
		secretsJSON, err := json.Marshal(secretsContent)
		if err != nil {
			t.Fatalf("marshal secrets: %v", err)
		}
		if err := os.WriteFile(secretsPath, secretsJSON, 0o600); err != nil {
			t.Fatalf("write secrets: %v", err)
		}

		configPath := filepath.Join(tmpDir, "local-config.json")
		configJSON := `{
			"provider_type": "local",
			"local_file_path": "` + secretsPath + `",
			"secret_paths": ["` + secretPath + `"]
		}`
		if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		store := sync.NewStateStore()
		result := sync.RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)

		if result.Status != sync.CycleStatusSuccess {
			t.Fatalf("expected CycleStatusSuccess for local provider, got %q (errorClass=%q)", result.Status, result.ErrorClass)
		}
	})

	// Case 2: vault provider via httptest TLS server.
	t.Run("vault_provider_succeeds", func(t *testing.T) {
		t.Parallel()

		vaultHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fixture := map[string]any{
				"data": map[string]any{
					"data":     secretData,
					"metadata": map[string]any{"version": 1},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(fixture)
		})

		srv := httptest.NewTLSServer(vaultHandler)
		t.Cleanup(srv.Close)

		// Write the test server's CA certificate to a temp file so the vault
		// provider can verify the TLS connection via vault_tls_ca_cert.
		caCertPath := filepath.Join(tmpDir, "vault-ca.pem")
		if err := writeTLSServerCACert(t, srv, caCertPath); err != nil {
			t.Fatalf("write CA cert: %v", err)
		}

		configPath := filepath.Join(tmpDir, "vault-config.json")
		configJSON := `{
			"provider_type": "vault",
			"vault_address": "` + srv.URL + `",
			"vault_token": "test-token",
			"vault_mount": "secret",
			"vault_tls_ca_cert": "` + caCertPath + `",
			"secret_paths": ["` + secretPath + `"]
		}`
		if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		store := sync.NewStateStore()
		result := sync.RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)

		if result.Status != sync.CycleStatusSuccess {
			t.Fatalf("expected CycleStatusSuccess for vault provider, got %q (errorClass=%q)", result.Status, result.ErrorClass)
		}
	})

	// Case 3: unknown provider_type returns provider_init_failed.
	t.Run("unknown_provider_fails", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(tmpDir, "unknown-config.json")
		configJSON := `{
			"provider_type": "unknown_type",
			"secret_paths": ["` + secretPath + `"]
		}`
		if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		store := sync.NewStateStore()
		result := sync.RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)

		if result.Status != sync.CycleStatusFailed {
			t.Fatalf("expected CycleStatusFailed for unknown provider, got %q", result.Status)
		}
		if result.ErrorClass != "provider_init_failed" {
			t.Fatalf("expected ErrorClass=provider_init_failed, got %q", result.ErrorClass)
		}
	})
}

// writeTLSServerCACert writes the TLS CA certificate from the given httptest.Server
// to path in PEM format.
func writeTLSServerCACert(t *testing.T, srv *httptest.Server, path string) error {
	t.Helper()
	if srv.TLS == nil || len(srv.TLS.Certificates) == 0 {
		t.Fatal("httptest server has no TLS certificates")
	}
	cert := srv.TLS.Certificates[0]
	if len(cert.Certificate) == 0 {
		t.Fatal("TLS certificate chain is empty")
	}
	// Parse the leaf certificate.
	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return err
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: x509Cert.Raw,
	})
	return os.WriteFile(path, pemBlock, 0o600)
}
