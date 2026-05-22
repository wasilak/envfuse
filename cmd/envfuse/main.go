package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	osexec "os/exec"
	"sort"

	"envfuse/internal/config"
	"envfuse/internal/state"
	synccycle "envfuse/internal/sync"
	"envfuse/internal/supervisor"
)

func main() {
	var configPath string
	var once bool

	flag.StringVar(&configPath, "config", "./seon.json", "path to seon config file")
	flag.BoolVar(&once, "once", false, "run one sync cycle then exec child (no supervision loop)")
	flag.Parse()

	store := state.NewStore()
	result := synccycle.RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)
	fmt.Println(string(result.Status))

	if result.Status != synccycle.CycleStatusSuccess {
		os.Exit(1)
	}

	if len(flag.Args()) == 0 {
		return
	}

	childEnv := mergeEnvForChild(os.Environ(), store.LastAppliedEnv())

	if once {
		// Legacy -once mode: run child directly without a supervision loop.
		if err := runDirect(flag.Args(), childEnv); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// Long-running supervisor mode (PID 1): supervise child with signal forwarding
	// and configurable shutdown timeout (SUPV-01/02/03).
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	shutdownTimeout, err := cfg.ParsedShutdownTimeout()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	res := supervisor.Run(flag.Args(), childEnv, shutdownTimeout)

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
	for k, v := range envPayload {
		merged[k] = v
	}
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
