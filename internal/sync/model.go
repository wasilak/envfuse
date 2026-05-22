package sync

type CycleStatus string

const (
	CycleStatusSuccess CycleStatus = "success"
	CycleStatusFailed  CycleStatus = "failed"
	CycleStatusAborted CycleStatus = "aborted"
)

type CycleResult struct {
	Status        CycleStatus
	FetchedPaths  []string
	ErrorClass    string
	AppliedEnv    []string
	RenderedFiles []string
	// Fingerprint is the SHA-256 digest of the committed candidate env+files.
	// Populated only when Status == CycleStatusSuccess (D-06: pre-commit candidate state).
	// Used by the supervisor to decide whether to restart the child (RELO-02).
	Fingerprint string
}
