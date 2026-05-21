package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

type Provider struct {
	filePath string
}

func New(filePath string) *Provider {
	return &Provider{filePath: filePath}
}

func (p *Provider) Fetch(_ context.Context, resourceURI string) (map[string]any, error) {
	b, err := os.ReadFile(p.filePath)
	if err != nil {
		return nil, fmt.Errorf("read local secrets file: %w", err)
	}

	store := map[string]map[string]any{}
	if err := json.Unmarshal(b, &store); err != nil {
		return nil, fmt.Errorf("decode local secrets file: %w", err)
	}

	v, ok := store[resourceURI]
	if !ok {
		return nil, fmt.Errorf("resource path not found: %s", resourceURI)
	}

	out := make(map[string]any, len(v))
	for k, val := range v {
		out[k] = val
	}

	return out, nil
}
