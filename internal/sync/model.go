package sync

type CycleStatus string

const (
	CycleStatusSuccess CycleStatus = "success"
	CycleStatusFailed  CycleStatus = "failed"
	CycleStatusAborted CycleStatus = "aborted"
)

type CycleResult struct {
	Status       CycleStatus
	FetchedPaths []string
	ErrorClass   string
	AppliedEnv   []string
}
