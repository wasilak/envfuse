package vault_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	vault "envfuse/internal/provider/vault"
)

// newTestServer creates an httptest TLS server returning the given handler.
// The returned client transport trusts the test server's certificate.
func newTestVaultServer(t *testing.T, handler http.Handler) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

func TestVaultProvider_Fetch_HappyPath(t *testing.T) {
	t.Parallel()

	srv, client := newTestVaultServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// KVv2 read path: /v1/<mount>/data/<path>
		fixture := map[string]any{
			"data": map[string]any{
				"data":     map[string]any{"key": "value"},
				"metadata": map[string]any{"version": 1},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fixture)
	}))

	p, err := vault.New(vault.Config{
		Address:    srv.URL,
		Token:      "test-token",
		Mount:      "secret",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	got, err := p.Fetch(context.Background(), "myapp/config")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if v, ok := got["key"]; !ok || v != "value" {
		t.Fatalf("expected got[\"key\"]==\"value\", got %v", got)
	}
}

func TestVaultProvider_Fetch_NilSecret(t *testing.T) {
	t.Parallel()

	srv, client := newTestVaultServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[]}`))
	}))

	p, err := vault.New(vault.Config{
		Address:    srv.URL,
		Token:      "test-token",
		Mount:      "secret",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	_, err = p.Fetch(context.Background(), "myapp/missing")
	if err == nil {
		t.Fatal("expected error for 404/nil secret, got nil")
	}
}

func TestVaultProvider_Fetch_404_DescriptiveError(t *testing.T) {
	t.Parallel()

	srv, client := newTestVaultServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[]}`))
	}))

	p, err := vault.New(vault.Config{
		Address:    srv.URL,
		Token:      "test-token",
		Mount:      "secret",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	_, err = p.Fetch(context.Background(), "myapp/missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(err.Error()) == 0 {
		t.Fatal("expected descriptive error message, got empty string")
	}
}

func TestVaultProvider_New_MissingCAFile(t *testing.T) {
	t.Parallel()

	_, err := vault.New(vault.Config{
		Address:   "https://vault.example.com",
		Token:     "test-token",
		TLSCACert: "/nonexistent/ca.crt",
	})
	if err == nil {
		t.Fatal("expected error for non-existent CA cert file, got nil")
	}
}
