package sync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"envfuse/internal/clock"
	"envfuse/internal/config"
	"envfuse/internal/fileio"
	"envfuse/internal/fingerprint"
	"envfuse/internal/inject"
	"envfuse/internal/provider"
	localprovider "envfuse/internal/provider/local"
	"envfuse/internal/render"
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
	return c.runCycleWithVectors(ctx, secretPaths, envMappings, nil)
}

func (c *Coordinator) RunCycleWithVectors(ctx context.Context, secretPaths []string, envMappings []config.EnvMapping, templates []config.TemplateSpec) CycleResult {
	return c.runCycleWithVectors(ctx, secretPaths, envMappings, templates)
}

func (c *Coordinator) runCycleWithVectors(ctx context.Context, secretPaths []string, envMappings []config.EnvMapping, templates []config.TemplateSpec) CycleResult {
	if err := inject.ValidateEnvMappings(envMappings); err != nil {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "env_mapping_invalid"}
	}
	if err := fileio.ValidateTemplateTargets(templates); err != nil {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "path_disallowed"}
	}

	uniq := dedupe(secretPaths)
	fetched := make([]string, 0, len(uniq))
	candidate := make(map[string]map[string]any, len(uniq))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	for _, path := range uniq {
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

	for _, spec := range templates {
		for _, path := range spec.RequiredPaths {
			if _, ok := candidate[path]; !ok {
				return CycleResult{Status: CycleStatusFailed, ErrorClass: "template_missing_key"}
			}
		}
	}

	rendered, err := render.RenderTemplates(candidate, templates)
	if err != nil {
		if strings.Contains(err.Error(), "map has no entry for key") || strings.Contains(err.Error(), "missing key") {
			return CycleResult{Status: CycleStatusFailed, ErrorClass: "template_missing_key"}
		}
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "template_render_failed"}
	}

	renderedPayload := make(map[string][]byte, len(rendered))
	filePlan := make([]fileio.FilePayload, 0, len(rendered))
	for _, out := range rendered {
		buf := make([]byte, len(out.Content))
		copy(buf, out.Content)
		renderedPayload[out.Path] = buf
		filePlan = append(filePlan, fileio.FilePayload{Path: out.Path, Content: buf})
	}

	if err := fileio.WriteAtomic(filePlan); err != nil {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "file_write_failed"}
	}

	appliedEnv := make([]string, 0, len(envPayload))
	for envVar := range envPayload {
		appliedEnv = append(appliedEnv, envVar)
	}
	sort.Strings(appliedEnv)

	// Compute fingerprint from pre-commit candidate state (D-06).
	// Only effective env payload + rendered file bytes are included (D-05).
	// Runtime metadata (timestamps, fetch order) is excluded (D-08).
	candidateFingerprint := fingerprint.Compute(envPayload, renderedPayload)

	// Stage and commit applied snapshot only after full-batch success, per SYNC-04.
	c.store.StageCandidate(candidate, envPayload, renderedPayload)
	c.store.CommitCandidate()

	sort.Strings(fetched)
	renderedFiles := make([]string, 0, len(renderedPayload))
	for path := range renderedPayload {
		renderedFiles = append(renderedFiles, path)
	}
	sort.Strings(renderedFiles)

	return CycleResult{
		Status:        CycleStatusSuccess,
		FetchedPaths:  fetched,
		AppliedEnv:    appliedEnv,
		RenderedFiles: renderedFiles,
		Fingerprint:   candidateFingerprint,
	}
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
	return coordinator.RunCycleWithVectors(ctx, cfg.SecretPaths, cfg.EnvMappings, cfg.Templates)
}

func RunSingleCycleWithStoreFromConfig(ctx context.Context, configPath string, store *state.Store) CycleResult {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "config_invalid"}
	}

	if cfg.ProviderType != "local" {
		return CycleResult{Status: CycleStatusFailed, ErrorClass: "provider_unsupported"}
	}

	p := localprovider.New(cfg.LocalFilePath)
	coordinator := NewCoordinatorWithStoreAndTimeout(p, store, 2*time.Second)
	return coordinator.RunCycleWithVectors(ctx, cfg.SecretPaths, cfg.EnvMappings, cfg.Templates)
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
	maps.Copy(out, in)
	return out
}
