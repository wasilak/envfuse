package supervisor

import (
	"time"
)

// cooldownTracker implements the anti-loop coalescing state machine (D-09, D-10, D-11).
//
// After each restart the tracker enters a cooldown window. Any fingerprint change that
// arrives while the tracker is in cooldown is recorded as a pending restart flag (not
// executed immediately). When the cooldown expires, drainPending() returns true exactly
// once, causing the supervisor to execute exactly one deferred restart regardless of
// how many changes arrived during the window (D-10).
type cooldownTracker struct {
	duration  time.Duration
	coolUntil time.Time
	pending   bool
}

// newCooldownTracker creates a tracker with the given cooldown duration.
func newCooldownTracker(duration time.Duration) *cooldownTracker {
	return &cooldownTracker{duration: duration}
}

// recordRestart starts the cooldown window. Must be called after each restart.
func (ct *cooldownTracker) recordRestart() {
	ct.coolUntil = time.Now().Add(ct.duration)
}

// inCooldown reports whether the cooldown window is still active.
func (ct *cooldownTracker) inCooldown() bool {
	return time.Now().Before(ct.coolUntil)
}

// markPending records that a fingerprint change arrived during cooldown.
// Calling markPending outside of cooldown is valid but has no special effect;
// the caller is responsible for gating on inCooldown() before calling markPending.
func (ct *cooldownTracker) markPending() {
	ct.pending = true
}

// isPending reports whether a deferred restart is waiting to fire.
func (ct *cooldownTracker) isPending() bool {
	return ct.pending
}

// drainPending atomically reads and clears the pending flag.
// Returns true if a deferred restart should execute (exactly once per coalescing window).
// Returns false if no restart was pending.
func (ct *cooldownTracker) drainPending() bool {
	if !ct.pending {
		return false
	}
	ct.pending = false
	return true
}
