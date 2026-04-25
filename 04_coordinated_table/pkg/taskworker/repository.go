package taskworker

import (
	"context"
	"time"
)

// Candidate is a task fetched but not yet claimed by this worker.
type Candidate struct {
	ID       string
	Priority uint8
	Payload  string
}

// ClaimedTask is a task that has been successfully transitioned to locked.
type ClaimedTask struct {
	ID          string
	PartitionID uint16
	Priority    uint8
	Payload     string
	LockValue   string
	LockedUntil time.Time
}

// TaskRepository abstracts all task-table access.
type TaskRepository interface {
	FetchEligibleCandidate(ctx context.Context, partitionID uint16) (*Candidate, error)
	ClaimTask(ctx context.Context, partitionID uint16, c Candidate, lockValue string, lockedUntil time.Time) (*ClaimedTask, error)
	MarkCompleted(ctx context.Context, task ClaimedTask, doneAt time.Time) error
}
