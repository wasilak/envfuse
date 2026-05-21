package sync

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"envfuse/internal/config"
	"envfuse/internal/provider"
	localprovider "envfuse/internal/provider/local"
	"golang.org/x/sync/errgroup"
)

type Coordinator struct {
	provider provider.Provider
}

func NewCoordinator(p provider.Provider) *Coordinator {
	return &Coordinator{provider: p}
}

func (c *Coordinator) RunCycle(ctx context.Context, secretPaths []string) CycleResult {
	uniq := dedupe(secretPaths)
	fetched := make([]string, 0, len(uniq))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	for _, path := range uniq {
		path := path
		g.Go(func() error {
			_, err := c.provider.Fetch(gctx, path)
			if err != nil {
				return fmt.Errorf("fetch path %s: %w", path, err)
			}
			mu.Lock()
			fetched = append(fetched, path)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "fetch_failed"}
	}

	sort.Strings(fetched)
	return CycleResult{Status: CycleStatusSuccess, FetchedPaths: fetched}
}

func RunSingleCycleFromConfig(ctx context.Context, configPath string) CycleResult {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "config_invalid"}
	}

	if cfg.ProviderType != "local" {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "provider_unsupported"}
	}

	p := localprovider.New(cfg.LocalFilePath)
	coordinator := NewCoordinator(p)
	return coordinator.RunCycle(ctx, cfg.SecretPaths)
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
