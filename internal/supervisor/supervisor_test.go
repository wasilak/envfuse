package supervisor

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSupervisor_StartupFailed verifies that a command that cannot be started
// returns ReasonStartupFailed with a non-zero exit code (D-14).
func TestSupervisor_StartupFailed(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := run(ctx,
		[]string{"/nonexistent-binary-that-does-not-exist"},
		os.Environ(),
		5*time.Second,
	)

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := run(ctx, nil, os.Environ(), 5*time.Second)

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := run(ctx,
		[]string{"/bin/sh", "-c", "exit 3"},
		os.Environ(),
		5*time.Second,
	)

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := run(ctx,
		[]string{"/bin/sh", "-c", "exit 0"},
		os.Environ(),
		5*time.Second,
	)

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

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan Result, 1)
	go func() {
		// Child ignores SIGTERM and sleeps indefinitely.
		// Short shutdownTimeout (300ms) triggers force-kill after context is cancelled.
		done <- run(ctx,
			[]string{"/bin/sh", "-c", "trap '' TERM; while true; do sleep 1 & wait; done"},
			os.Environ(),
			300*time.Millisecond,
		)
	}()

	// Wait until the child is running (give it a short moment to start).
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()

	// Child exits immediately with non-zero code — if supervisor retried, the test
	// would take longer than the timeout window.
	result := run(ctx,
		[]string{"/bin/sh", "-c", "exit 7"},
		os.Environ(),
		5*time.Second,
	)

	elapsed := time.Since(start)

	// Must return quickly — no retry loop.
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
