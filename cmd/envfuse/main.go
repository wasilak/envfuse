package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"os"
	osexec "os/exec"
	"sort"
	"strings"

	"envfuse/internal/config"
	"envfuse/internal/state"
	synccycle "envfuse/internal/sync"
	"envfuse/internal/supervisor"
)

func initLogger() {
	levelVar := new(slog.LevelVar) // default INFO
	if raw := os.Getenv("ENVFUSE_LOG_LEVEL"); raw != "" {
		switch strings.ToUpper(raw) {
		case "DEBUG":
			levelVar.Set(slog.LevelDebug)
		case "WARN":
			levelVar.Set(slog.LevelWarn)
		case "ERROR":
			levelVar.Set(slog.LevelError)
		// INFO is the default; any unrecognized value stays at INFO
		}
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: levelVar})))
}

func main() {
	initLogger()

	var configPath string
	var once bool

	flag.StringVar(&configPath, "config", "./seon.json", "path to seon config file")
	flag.BoolVar(&once, "once", false, "run one sync cycle then exec child (no supervision loop)")
	flag.Parse()

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		slog.Error("load config failed", "err", err)
		os.Exit(1)
	}

	shutdownTimeout, err := cfg.ParsedShutdownTimeout()
	if err != nil {
		slog.Error("invalid shutdown_timeout", "err", err)
		os.Exit(1)
	}

	pollInterval, err := cfg.ParsedReloadPollInterval()
	if err != nil {
		slog.Error("invalid reload_poll_interval", "err", err)
		os.Exit(1)
	}

	reloadCooldown, err := cfg.ParsedReloadCooldown()
	if err != nil {
		slog.Error("invalid reload_cooldown", "err", err)
		os.Exit(1)
	}

	store := state.NewStore()
	result := synccycle.RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)

	if result.Status != synccycle.CycleStatusSuccess {
		slog.Error("initial sync failed", "status", string(result.Status), "error_class", result.ErrorClass)
		os.Exit(1)
	}

	// D-08 INFO startup summary
	fp := result.Fingerprint
	if len(fp) >= 8 {
		fp = fp[:8]
	}
	slog.Info("envfuse started",
		"provider_type", cfg.ProviderType,
		"paths_fetched", len(result.FetchedPaths),
		"env_vars_applied", len(result.AppliedEnv),
		"files_rendered", len(result.RenderedFiles),
		"fingerprint", fp,
	)

	// D-13 DEBUG env var names applied
	slog.Debug("env vars applied", "names", result.AppliedEnv)

	// D-14 DEBUG template render details
	slog.Debug("templates rendered", "output_paths", result.RenderedFiles)

	if len(flag.Args()) == 0 {
		return
	}

	childEnv := mergeEnvForChild(os.Environ(), store.LastAppliedEnv())

	if once {
		// Legacy -once mode: run child directly without a supervision loop.
		if err := runDirect(flag.Args(), childEnv); err != nil {
			slog.Error("child exec failed", "err", err)
			os.Exit(1)
		}
		return
	}

	// Long-running supervisor mode (PID 1): supervise child with signal forwarding
	// and configurable shutdown timeout (SUPV-01/02/03).
	supCfg := supervisor.Config{
		Command:            flag.Args(),
		Env:                childEnv,
		ShutdownTimeout:    shutdownTimeout,
		ConfigPath:         configPath,
		Store:              store,
		InitialFingerprint: result.Fingerprint,
		PollInterval:       pollInterval,
		ReloadCooldown:     reloadCooldown,
	}

	res := supervisor.Run(supCfg)

	switch res.Reason {
	case supervisor.ReasonStartupFailed:
		// D-14: startup failure exits non-zero.
		os.Exit(1)
	case supervisor.ReasonChildExited:
		// D-13: exit with child exit code on uncontrolled exit.
		os.Exit(res.ExitCode)
	case supervisor.ReasonForcedKill:
		// D-04: forced kill recorded; exit non-zero.
		os.Exit(1)
	}
}

// runDirect runs the child command synchronously without supervision (legacy -once mode).
func runDirect(command []string, env []string) error {
	cmd := osexec.Command(command[0], command[1:]...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch child: %w", err)
	}
	return nil
}

// mergeEnvForChild applies envPayload overrides on top of the inherited environment.
// Matches the same canonicalization as internal/exec/launcher.go.
func mergeEnvForChild(inherited []string, envPayload map[string]string) []string {
	merged := make(map[string]string, len(inherited)+len(envPayload))
	for _, item := range inherited {
		for i := 0; i < len(item); i++ {
			if item[i] == '=' {
				merged[item[:i]] = item[i+1:]
				break
			}
		}
	}
	maps.Copy(merged, envPayload)
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%s", k, merged[k]))
	}
	return out
}
