package aws_test

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	awsprovider "envfuse/internal/provider/aws"
)

// mockSMClient is a function type that satisfies the narrow secretsManagerAPI interface.
type mockSMClient func(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)

func (m mockSMClient) GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return m(ctx, params, optFns...)
}

func TestAWSProvider_Fetch_HappyPath(t *testing.T) {
	t.Parallel()

	mock := mockSMClient(func(_ context.Context, params *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
		return &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"key":"value"}`),
		}, nil
	})

	p := awsprovider.NewWithClient(mock)
	got, err := p.Fetch(context.Background(), "myapp/config")
	if err != nil {
		t.Fatalf("Fetch: unexpected error: %v", err)
	}
	if v, ok := got["key"]; !ok || v != "value" {
		t.Fatalf("expected got[\"key\"]==\"value\", got %v", got)
	}
}

func TestAWSProvider_Fetch_BinarySecret(t *testing.T) {
	t.Parallel()

	mock := mockSMClient(func(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
		return &secretsmanager.GetSecretValueOutput{
			SecretString: nil,
		}, nil
	})

	p := awsprovider.NewWithClient(mock)
	_, err := p.Fetch(context.Background(), "myapp/binary")
	if err == nil {
		t.Fatal("expected error for binary secret, got nil")
	}
	if !contains(err.Error(), "binary secrets not supported") {
		t.Fatalf("expected error to contain \"binary secrets not supported\", got %q", err.Error())
	}
}

func TestAWSProvider_Fetch_NonJSONSecret(t *testing.T) {
	t.Parallel()

	const secretPath = "myapp/nonjson"
	mock := mockSMClient(func(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
		return &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String("not-json"),
		}, nil
	})

	p := awsprovider.NewWithClient(mock)
	_, err := p.Fetch(context.Background(), secretPath)
	if err == nil {
		t.Fatal("expected error for non-JSON secret, got nil")
	}
	if !contains(err.Error(), secretPath) {
		t.Fatalf("expected error to contain path %q, got %q", secretPath, err.Error())
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
