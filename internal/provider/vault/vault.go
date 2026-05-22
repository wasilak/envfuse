package vault

import (
	"context"
	"fmt"
	"net/http"

	vaultapi "github.com/hashicorp/vault/api"
)

// Config holds the configuration for the Vault provider.
type Config struct {
	// Address is the full URL of the Vault server (e.g. "https://vault.example.com").
	// When empty, the VAULT_ADDR environment variable is used.
	Address string

	// Token is the Vault token. When empty, VAULT_TOKEN is used.
	Token string

	// Mount is the KVv2 mount path (defaults to "secret").
	Mount string

	// TLSCACert is the path to a PEM-encoded CA certificate file.
	// When non-empty, the TLS client will verify the server certificate against this CA.
	TLSCACert string

	// HTTPClient is an optional custom HTTP client. Intended for tests only.
	// When nil, the vault API default client is used.
	HTTPClient *http.Client
}

// Provider fetches secrets from HashiCorp Vault using the KVv2 secrets engine.
type Provider struct {
	client *vaultapi.Client
	mount  string
}

// New creates a new Vault provider. Returns an error if TLS configuration fails.
// NEVER sets TLSConfig.Insecure = true.
func New(cfg Config) (*Provider, error) {
	apiCfg := vaultapi.DefaultConfig()

	if cfg.Address != "" {
		apiCfg.Address = cfg.Address
	}

	if cfg.HTTPClient != nil {
		apiCfg.HttpClient = cfg.HTTPClient
	}

	if cfg.TLSCACert != "" {
		tlsCfg := &vaultapi.TLSConfig{
			CACert: cfg.TLSCACert,
		}
		if err := apiCfg.ConfigureTLS(tlsCfg); err != nil {
			return nil, fmt.Errorf("vault: configure TLS: %w", err)
		}
	}

	client, err := vaultapi.NewClient(apiCfg)
	if err != nil {
		return nil, fmt.Errorf("vault: create client: %w", err)
	}

	if cfg.Token != "" {
		client.SetToken(cfg.Token)
	}

	mount := cfg.Mount
	if mount == "" {
		mount = "secret"
	}

	return &Provider{client: client, mount: mount}, nil
}

// Fetch retrieves a KVv2 secret at the given path and returns the inner data map.
func (p *Provider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
	secret, err := p.client.KVv2(p.mount).Get(ctx, resourceURI)
	if err != nil {
		return nil, fmt.Errorf("vault: fetch %q: %w", resourceURI, err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("vault: secret not found at path %q", resourceURI)
	}
	return secret.Data, nil
}
