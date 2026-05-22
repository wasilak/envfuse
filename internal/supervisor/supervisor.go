package supervisor

import (
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"envfuse/internal/state"
	synccycle "envfuse/internal/sync"
)

// Config holds the full set of parameters for the supervision loop.
type Config struct {
	// Command is the child command and arguments to supervise.
	Command []string
	// Env is the initial environment for the child process (merged from host + applied secrets).
	Env []string
	// ShutdownTimeout is the grace window before force-killing the child (D-01/D-02).
	ShutdownTimeout time.Duration
	// ConfigPath is the envfuse config file used for subsequent sync cycles.
	ConfigPath string
	// Store is the state store shared with the sync coordinator.
	Store *state.Store
	// InitialFingerprint is the fingerprint from the first committed cycle.
	// Used as baseline for change detection (RELO-02).
	InitialFingerprint string
	// PollInterval is the interval between subsequent sync cycles.
	PollInterval time.Duration
}

// child holds a running child process and its async wait channel.
type child struct {
	cmd    *osexec.Cmd
	waitCh chan error
}

// Run starts the child command and supervises it as PID 1, running sync cycles at
// PollInterval to detect config changes. When effective config changes, the child is
// stopped and restarted with the new environment (RELO-01/RELO-02).
//
// Subscribes to SIGTERM/SIGINT from the OS and forwards them to the child (T-03-02).
func Run(cfg Config) Result {
	sigCtx, stopSig := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSig()
	return runLoop(sigCtx, cfg)
}

// runLoop is the internal supervision loop, accepting a context for shutdown signaling
// so unit tests can inject cancellation without OS signals.
func runLoop(shutdownCtx context.Context, cfg Config) Result {
	lastFingerprint := cfg.InitialFingerprint
	childEnv := make([]string, len(cfg.Env))
	copy(childEnv, cfg.Env)

	for {
		c, startErr := spawnChild(cfg.Command, childEnv)
		if startErr != nil {
			// D-14: startup failure exits non-zero (no retry per D-15).
			return Result{Reason: ReasonStartupFailed, ExitCode: 1}
		}

		ticker := time.NewTicker(cfg.PollInterval)
		restart, result := watch(shutdownCtx, cfg, c, ticker, &lastFingerprint, &childEnv)
		ticker.Stop()

		if !restart {
			// Terminal condition: host shutdown or child self-exit.
			return result
		}
		// restart == true: config changed, child stopped; loop restarts with new env.
	}
}

// watch monitors a running child and drives config polling.
// Returns (restart=false, result) when the loop should terminate.
// Returns (restart=true, zero) when config changed and child should be restarted.
func watch(
	shutdownCtx context.Context,
	cfg Config,
	c *child,
	ticker *time.Ticker,
	lastFingerprint *string,
	childEnv *[]string,
) (restart bool, result Result) {
	for {
		select {
		case waitErr := <-c.waitCh:
			// Child exited on its own outside controlled shutdown (D-13).
			exitCode := exitCodeFromErr(waitErr)
			return false, Result{Reason: ReasonChildExited, ExitCode: exitCode}

		case <-shutdownCtx.Done():
			// Host SIGTERM/SIGINT received. Forward to child and wait (SUPV-02/03).
			sendSignal(c, syscall.SIGTERM)
			r := waitOrKill(c.waitCh, c.cmd, cfg.ShutdownTimeout)
			return false, r

		case <-ticker.C:
			// Run a sync cycle. Restart only on success + fingerprint change (RELO-02).
			cycleResult := synccycle.RunSingleCycleWithStoreFromConfig(
				shutdownCtx, cfg.ConfigPath, cfg.Store,
			)

			if cycleResult.Status != synccycle.CycleStatusSuccess {
				// Failed/aborted cycle: keep child running with last good config.
				continue
			}

			if cycleResult.Fingerprint == *lastFingerprint {
				// Effective config unchanged: no restart (RELO-02).
				continue
			}

			// Effective config changed: stop child gracefully, then restart (RELO-01).
			// config_changed reason (D-16) applies when restart occurs.
			sendSignal(c, syscall.SIGTERM)
			waitOrKill(c.waitCh, c.cmd, cfg.ShutdownTimeout)

			*lastFingerprint = cycleResult.Fingerprint
			*childEnv = mergeEnvForRestart(cfg.Env, cfg.Store.LastAppliedEnv())

			return true, Result{}
		}
	}
}

// spawnChild launches the child command asynchronously.
// Returns an error if the process cannot be started (D-14).
func spawnChild(command []string, env []string) (*child, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	cmd := osexec.Command(command[0], command[1:]...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "supervisor: start child: %v\n", err)
		return nil, err
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	return &child{cmd: cmd, waitCh: waitCh}, nil
}

// sendSignal delivers a signal to the child process only (D-03: not process group).
func sendSignal(c *child, sig syscall.Signal) {
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(sig)
	}
}

// waitOrKill waits for the child to exit within shutdownTimeout, then force-kills (D-03/D-04).
func waitOrKill(waitCh <-chan error, cmd *osexec.Cmd, shutdownTimeout time.Duration) Result {
	killTimer := time.NewTimer(shutdownTimeout)
	defer killTimer.Stop()

	select {
	case waitErr := <-waitCh:
		exitCode := exitCodeFromErr(waitErr)
		return Result{Reason: ReasonChildExited, ExitCode: exitCode}

	case <-killTimer.C:
		// Grace window expired; force-kill child process only (D-03).
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitCh
		return Result{Reason: ReasonForcedKill, Killed: true}
	}
}

// exitCodeFromErr extracts an exit code from a cmd.Wait() error.
func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*osexec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

// mergeEnvForRestart builds a new child env slice by applying newPayload overrides
// on top of the original inherited environment. Matches launcher canonicalization.
func mergeEnvForRestart(originalEnv []string, newPayload map[string]string) []string {
	merged := make(map[string]string, len(originalEnv)+len(newPayload))
	for _, item := range originalEnv {
		for i := 0; i < len(item); i++ {
			if item[i] == '=' {
				merged[item[:i]] = item[i+1:]
				break
			}
		}
	}
	for k, v := range newPayload {
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
