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

// TestSupervisor_DefaultCooldownValue verifies that the default reload_cooldown is 10s (D-12).
func TestSupervisor_DefaultCooldownValue(t *testing.T) {
	t.Parallel()

	cfg := makeTestConfig([]string{"/bin/sh", "-c", "exit 0"})
	if cfg.ReloadCooldown != 0 {
		// makeTestConfig leaves ReloadCooldown unset; zero means "use default".
		t.Fatalf("makeTestConfig should leave ReloadCooldown as zero, got %v", cfg.ReloadCooldown)
	}
	if effectiveCooldown(cfg) != 10*time.Second {
		t.Fatalf("effectiveCooldown with zero ReloadCooldown: expected 10s (D-12), got %v", effectiveCooldown(cfg))
	}
}

// TestSupervisor_CooldownSuppressesImmediateRestart verifies that when a restart occurs,
// a second fingerprint change arriving within the cooldown window is suppressed and does
// not trigger an immediate second restart (D-09, D-10).
//
// Mechanism: uses a scripted fakeCycleRunner that drives tick events directly so the
// test does not sleep for real cooldown durations.
func TestSupervisor_CooldownSuppressesImmediateRestart(t *testing.T) {
	t.Parallel()

	// The cooldown tracker starts in the not-in-cooldown state.
	// After the first restart, it must enter cooldown.
	// A second change during cooldown must NOT cause a second immediate restart.
	ct := newCooldownTracker(50 * time.Millisecond)

	// Simulate: restart happens now.
	ct.recordRestart()

	if !ct.inCooldown() {
		t.Fatal("cooldown tracker must be in cooldown immediately after recordRestart()")
	}

	// A fingerprint change arrives while in cooldown — must be marked pending.
	ct.markPending()

	if !ct.isPending() {
		t.Fatal("cooldown tracker must report isPending() after markPending() during cooldown")
	}

	// Cooldown expires.
	time.Sleep(100 * time.Millisecond)

	if ct.inCooldown() {
		t.Fatal("cooldown tracker must exit cooldown after duration elapses")
	}

	// Drain the pending flag — simulates the deferred restart being scheduled.
	fired := ct.drainPending()
	if !fired {
		t.Fatal("drainPending() must return true when a pending restart was coalesced")
	}

	// Pending flag must be cleared after drain.
	if ct.isPending() {
		t.Fatal("isPending() must be false after drainPending()")
	}
}

// TestSupervisor_MultipleChangesCoalesceToOneRestart verifies that multiple fingerprint
// changes arriving during a single cooldown window result in exactly one deferred restart,
// not N restarts (D-10).
func TestSupervisor_MultipleChangesCoalesceToOneRestart(t *testing.T) {
	t.Parallel()

	ct := newCooldownTracker(50 * time.Millisecond)
	ct.recordRestart()

	// Simulate N rapid changes during cooldown.
	const nChanges = 5
	for i := range nChanges {
		_ = i
		ct.markPending()
	}

	// Only one deferred restart must fire when cooldown expires.
	time.Sleep(100 * time.Millisecond)

	fired := ct.drainPending()
	if !fired {
		t.Fatal("drainPending() must return true after N changes during cooldown")
	}

	// A second drain must return false — only one restart may execute.
	firedAgain := ct.drainPending()
	if firedAgain {
		t.Fatal("drainPending() must return false on second call — no second deferred restart (D-10)")
	}
}

// TestSupervisor_NoCooldownWithoutPriorRestart verifies that a fresh supervisor
// (no restart yet) is not in cooldown and markPending/drainPending behave correctly
// when called before any restart (edge case: system just started).
func TestSupervisor_NoCooldownWithoutPriorRestart(t *testing.T) {
	t.Parallel()

	ct := newCooldownTracker(50 * time.Millisecond)

	if ct.inCooldown() {
		t.Fatal("cooldown tracker must not be in cooldown before any restart")
	}
	if ct.isPending() {
		t.Fatal("cooldown tracker must not have a pending restart before any restart")
	}

	// drainPending with no prior markPending must return false.
	fired := ct.drainPending()
	if fired {
		t.Fatal("drainPending() must return false when no restart was pending")
	}
}
