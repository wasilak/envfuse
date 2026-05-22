package supervisor

import (
	"context"
	"os"
	"testing"
	"time"
)

// noCyclePollInterval is long enough that the cycle ticker never fires during unit tests.
// Unit tests exercise the child lifecycle paths, not the config-change polling path.
const noCyclePollInterval = 24 * time.Hour

// makeTestConfig returns a minimal supervisor Config for unit tests.
// The poll interval is set to noCyclePollInterval so the cycle ticker
// never fires; tests exercise only the child lifecycle paths.
func makeTestConfig(command []string) Config {
	return Config{
		Command:         command,
		Env:             os.Environ(),
		ShutdownTimeout: 5 * time.Second,
		// ConfigPath and Store are unused when PollInterval is too long to fire.
		ConfigPath:   "",
		Store:        nil,
		PollInterval: noCyclePollInterval,
	}
}

// TestSupervisor_StartupFailed verifies that a command that cannot be started
// returns ReasonStartupFailed with a non-zero exit code (D-14).
func TestSupervisor_StartupFailed(t *testing.T) {
	t.Parallel()

	result := runLoop(t.Context(), makeTestConfig(
		[]string{"/nonexistent-binary-that-does-not-exist"},
	))

	if result.Reason != ReasonStartupFailed {
		t.Fatalf("expected reason %q, got %q", ReasonStartupFailed, result.Reason)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for startup_failed, got 0")
	}
}

// TestSupervisor_StartupFailedEmptyCommand verifies that an empty command slice
// returns ReasonStartupFailed immediately without panicking (D-14).
func TestSupervisor_StartupFailedEmptyCommand(t *testing.T) {
	t.Parallel()

	result := runLoop(t.Context(), makeTestConfig(nil))

	if result.Reason != ReasonStartupFailed {
		t.Fatalf("expected reason %q for empty command, got %q", ReasonStartupFailed, result.Reason)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for empty command, got 0")
	}
}

// TestSupervisor_ChildExitedCode verifies that when a child exits on its own,
// the supervisor returns ReasonChildExited with the exact child exit code (D-13).
func TestSupervisor_ChildExitedCode(t *testing.T) {
	t.Parallel()

	result := runLoop(t.Context(), makeTestConfig(
		[]string{"/bin/sh", "-c", "exit 3"},
	))

	if result.Reason != ReasonChildExited {
		t.Fatalf("expected reason %q, got %q", ReasonChildExited, result.Reason)
	}
	if result.ExitCode != 3 {
		t.Fatalf("expected child exit code 3, got %d", result.ExitCode)
	}
}

// TestSupervisor_ChildExitedZeroCode verifies that a child exiting 0 is also
// classified as ReasonChildExited with code 0 (D-13).
func TestSupervisor_ChildExitedZeroCode(t *testing.T) {
	t.Parallel()

	result := runLoop(t.Context(), makeTestConfig(
		[]string{"/bin/sh", "-c", "exit 0"},
	))

	if result.Reason != ReasonChildExited {
		t.Fatalf("expected reason %q, got %q", ReasonChildExited, result.Reason)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected child exit code 0, got %d", result.ExitCode)
	}
}

// TestSupervisor_ForcedKillReason verifies that when a child ignores SIGTERM and
// does not exit within the shutdown timeout, the supervisor force-kills the child
// and returns ReasonForcedKill with Killed=true (D-03, D-04, D-16).
//
// Uses a cancellable context to inject the shutdown signal deterministically.
func TestSupervisor_ForcedKillReason(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	cfg := Config{
		Command:         []string{"/bin/sh", "-c", "trap '' TERM; while true; do sleep 1 & wait; done"},
		Env:             os.Environ(),
		ShutdownTimeout: 300 * time.Millisecond,
		PollInterval:    noCyclePollInterval,
	}

	done := make(chan Result, 1)
	go func() {
		done <- runLoop(ctx, cfg)
	}()

	// Wait until the child is running.
	time.Sleep(200 * time.Millisecond)

	// Cancel context to simulate host SIGTERM arriving at envfuse.
	cancel()

	select {
	case result := <-done:
		if result.Reason != ReasonForcedKill {
			t.Fatalf("expected reason %q (child ignored SIGTERM), got %q", ReasonForcedKill, result.Reason)
		}
		if !result.Killed {
			t.Fatalf("expected Killed=true for forced_kill result, got false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not return after context cancel + forced kill within timeout")
	}
}

// TestSupervisor_NoAutoRetry verifies that when a child exits with a non-zero code,
// the supervisor does NOT restart it — it returns immediately with child's exit code (D-15).
func TestSupervisor_NoAutoRetry(t *testing.T) {
	t.Parallel()

	start := time.Now()

	result := runLoop(t.Context(), makeTestConfig(
		[]string{"/bin/sh", "-c", "exit 7"},
	))

	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("supervisor took %v; expected near-instant return without retry (D-15)", elapsed)
	}

	if result.Reason != ReasonChildExited {
		t.Fatalf("expected reason %q (no retry), got %q", ReasonChildExited, result.Reason)
	}
	if result.ExitCode != 7 {
		t.Fatalf("expected child exit code 7, got %d", result.ExitCode)
	}
}

// TestSupervisor_ReasonTaxonomyExactStrings verifies that reason constants use
// the exact string values mandated by D-16.
func TestSupervisor_ReasonTaxonomyExactStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason   Reason
		expected string
	}{
		{ReasonConfigChanged, "config_changed"},
		{ReasonChildExited, "child_exited"},
		{ReasonStartupFailed, "startup_failed"},
		{ReasonForcedKill, "forced_kill"},
	}

	for _, tc := range tests {
		if string(tc.reason) != tc.expected {
			t.Errorf("reason constant %q: expected string %q, got %q", tc.reason, tc.expected, string(tc.reason))
		}
	}
}
