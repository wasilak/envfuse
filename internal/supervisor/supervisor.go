package supervisor

import (
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"os/signal"
	"syscall"
	"time"
)

// Run starts the child command with the given environment and supervises it as PID 1.
// It subscribes to SIGTERM/SIGINT from the OS and forwards them to the child.
//
// Behavior:
//   - SIGTERM and SIGINT received by envfuse are forwarded to the child process (SUPV-02).
//   - If child does not exit within shutdownTimeout after the signal, child is force-killed
//     via SIGKILL to the child process only (D-03, SUPV-03).
//   - If child exits on its own (outside controlled shutdown), returns ReasonChildExited
//     with the child's exit code (D-13).
//   - If child fails to start, returns ReasonStartupFailed (D-14).
//   - No crash auto-retry; single run only (D-15).
func Run(command []string, env []string, shutdownTimeout time.Duration) Result {
	// Subscribe to host signals. Accept only SIGTERM and SIGINT (T-03-02).
	sigCtx, stopSig := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSig()

	return run(sigCtx, command, env, shutdownTimeout)
}

// run is the internal implementation that accepts a context for shutdown signaling.
// This allows unit tests to inject a cancellable context instead of relying on OS signals.
func run(shutdownCtx context.Context, command []string, env []string, shutdownTimeout time.Duration) Result {
	if len(command) == 0 {
		return Result{Reason: ReasonStartupFailed, ExitCode: 1}
	}

	cmd := osexec.Command(command[0], command[1:]...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "supervisor: start child: %v\n", err)
		return Result{Reason: ReasonStartupFailed, ExitCode: 1}
	}

	// waitCh receives the child exit error asynchronously.
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case waitErr := <-waitCh:
		// Child exited on its own outside any controlled shutdown — D-13.
		exitCode := exitCodeFrom(waitErr, cmd)
		return Result{Reason: ReasonChildExited, ExitCode: exitCode}

	case <-shutdownCtx.Done():
		// Shutdown signal received (host SIGTERM/SIGINT or test context cancellation).
		// Forward the signal to the child process only (D-03 — not process group).
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}

		// Wait up to shutdownTimeout for the child to exit gracefully (D-01/D-02).
		killTimer := time.NewTimer(shutdownTimeout)
		defer killTimer.Stop()

		select {
		case waitErr := <-waitCh:
			// Child exited within grace window.
			exitCode := exitCodeFrom(waitErr, cmd)
			return Result{Reason: ReasonChildExited, ExitCode: exitCode}

		case <-killTimer.C:
			// Grace timeout expired — force-kill child process only (D-03).
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			// Wait for kill to complete; process must be reaped to avoid zombies.
			<-waitCh
			exitCode := 0
			if cmd.ProcessState != nil {
				exitCode = cmd.ProcessState.ExitCode()
			}
			// D-04: record forced_kill reason after deterministic stop.
			return Result{Reason: ReasonForcedKill, ExitCode: exitCode, Killed: true}
		}
	}
}

// exitCodeFrom extracts the process exit code from a Wait error, falling back to
// the ProcessState when available.
func exitCodeFrom(waitErr error, cmd *osexec.Cmd) int {
	if waitErr == nil {
		return 0
	}
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	return 1
}
