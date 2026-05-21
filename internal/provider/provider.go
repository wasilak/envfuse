package provider

import "context"

type Provider interface {
	Fetch(ctx context.Context, resourceURI string) (map[string]any, error)
}
