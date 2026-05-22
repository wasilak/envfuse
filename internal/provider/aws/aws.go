package aws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// Config holds the configuration for the AWS Secrets Manager provider.
type Config struct {
	// Region is the AWS region. When empty, the standard credential chain is used
	// (AWS_DEFAULT_REGION, ~/.aws/config, IMDSv2, etc.).
	Region string

	// Endpoint is an optional custom endpoint URL. Intended for LocalStack or test
	// overrides only. Leave empty in production.
	Endpoint string
}

// secretsManagerAPI is a narrow interface for testability.
type secretsManagerAPI interface {
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// Provider fetches secrets from AWS Secrets Manager using the standard credential chain.
type Provider struct {
	client secretsManagerAPI
}

// New creates a production AWS Secrets Manager provider using the standard credential chain.
// ctx is passed here because LoadDefaultConfig may make IMDSv2 network calls.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	clientOpts := []func(*secretsmanager.Options){}
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *secretsmanager.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}
	client := secretsmanager.NewFromConfig(awsCfg, clientOpts...)
	return &Provider{client: client}, nil
}

// NewWithClient creates an AWS provider with an injected client for testing.
func NewWithClient(client secretsManagerAPI) *Provider {
	return &Provider{client: client}
}

// Fetch retrieves a secret by ID and returns its JSON-decoded key-value pairs.
// Binary secrets (SecretString == nil) and non-JSON secrets are rejected.
func (p *Provider) Fetch(ctx context.Context, resourceURI string) (map[string]any, error) {
	out, err := p.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(resourceURI),
	})
	if err != nil {
		return nil, fmt.Errorf("get secret %q: %w", resourceURI, err)
	}
	if out.SecretString == nil {
		return nil, fmt.Errorf("secret %q is a binary secret; only JSON SecretString secrets are supported (binary secrets not supported)", resourceURI)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(*out.SecretString), &result); err != nil {
		return nil, fmt.Errorf("decode secret %q: %w", resourceURI, err)
	}
	return result, nil
}
