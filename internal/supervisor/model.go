package supervisor

// Reason classifies why a supervised child stopped or why the supervisor exited.
// D-16: typed reason taxonomy for supervision outcomes.
type Reason string

const (
	// ReasonConfigChanged indicates the child was stopped because effective config changed.
	ReasonConfigChanged Reason = "config_changed"

	// ReasonChildExited indicates the child exited on its own outside controlled reload.
	// D-13: supervisor exits with child exit code in this case.
	ReasonChildExited Reason = "child_exited"

	// ReasonStartupFailed indicates the child process could not be started.
	// D-14: supervisor exits non-zero in this case.
	ReasonStartupFailed Reason = "startup_failed"

	// ReasonForcedKill indicates the child did not exit within shutdown_timeout and
	// was force-killed via SIGKILL to the child process only (D-03).
	ReasonForcedKill Reason = "forced_kill"
)

// Result holds the outcome of a supervisor run.
type Result struct {
	Reason   Reason
	ExitCode int
	Killed   bool
}
