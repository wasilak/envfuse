package sync

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"envfuse/internal/clock"
	"envfuse/internal/config"
	"envfuse/internal/inject"
	"envfuse/internal/provider"
	localprovider "envfuse/internal/provider/local"
	"envfuse/internal/state"
	"golang.org/x/sync/errgroup"
)

type Coordinator struct {
	provider       provider.Provider
	store          *state.Store
	perPathTimeout time.Duration
	clock          clock.Clock
}

func NewCoordinator(p provider.Provider) *Coordinator {
	return NewCoordinatorWithStore(p, state.NewStore(), 2*time.Second)
}

func NewCoordinatorWithStore(p provider.Provider, s *state.Store, perPathTimeout time.Duration) *Coordinator {
	return newCoordinatorWithClock(p, s, perPathTimeout, clock.RealClock{})
}

func newCoordinatorWithClock(p provider.Provider, s *state.Store, perPathTimeout time.Duration, clk clock.Clock) *Coordinator {
	return &Coordinator{provider: p, store: s, perPathTimeout: perPathTimeout, clock: clk}
}

func NewStateStore() *state.Store {
	return state.NewStore()
}

func NewCoordinatorWithStoreAndTimeout(p provider.Provider, s *state.Store, perPathTimeout time.Duration) *Coordinator {
	return NewCoordinatorWithStore(p, s, perPathTimeout)
}

func (c *Coordinator) RunCycle(ctx context.Context, secretPaths []string) CycleResult {
	return c.RunCycleWithEnvMappings(ctx, secretPaths, nil)
}

func (c *Coordinator) RunCycleWithEnvMappings(ctx context.Context, secretPaths []string, envMappings []config.EnvMapping) CycleResult {
	if err := inject.ValidateEnvMappings(envMappings); err != nil {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "env_mapping_invalid"}
	}

	uniq := dedupe(secretPaths)
	fetched := make([]string, 0, len(uniq))
	candidate := make(map[string]map[string]any, len(uniq))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	for _, path := range uniq {
		path := path
		g.Go(func() error {
			pathCtx, cancel := context.WithTimeout(gctx, c.perPathTimeout) // per SYNC-03
			defer cancel()

			data, err := c.provider.Fetch(pathCtx, path)
			if err != nil {
				return fmt.Errorf("fetch path %s: %w", path, err)
			}
			mu.Lock()
			fetched = append(fetched, path)
			candidate[path] = copyKV(data)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return CycleResult{Status: CycleStatusAborted, ErrorClass: "fetch_aborted"}
		}
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "fetch_failed"}
	}

	envPayload, err := inject.ResolveEnvMappings(candidate, envMappings)
	if err != nil {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "env_mapping_unresolved"}
	}

	appliedEnv := make([]string, 0, len(envPayload))
	for envVar := range envPayload {
		appliedEnv = append(appliedEnv, envVar)
	}
	sort.Strings(appliedEnv)

	// Stage and commit applied snapshot only after full-batch success, per SYNC-04.
	c.store.StageCandidate(candidate, envPayload)
	c.store.CommitCandidate()

	sort.Strings(fetched)
	return CycleResult{Status: CycleStatusSuccess, FetchedPaths: fetched, AppliedEnv: appliedEnv}
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
	return coordinator.RunCycleWithEnvMappings(ctx, cfg.SecretPaths, cfg.EnvMappings)
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

func copyKV(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
