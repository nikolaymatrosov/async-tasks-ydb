package taskworker

import (
	"context"
	"time"
)

// Candidate is a task fetched from the eligibility scan but not yet claimed by
// this worker. It carries the per-entity ordering metadata needed to dedup and
// gate dispatch.
type Candidate struct {
	ID           string
	EntityID     string
	EntitySeq    uint64
	Payload      string
	Status       string // "pending" | "locked"
	ScheduledAt  *time.Time
	LockedUntil  *time.Time
	AttemptCount uint32
}

// ClaimedTask is a Candidate that has been successfully transitioned to
// status='locked' under this worker's lock_value.
type ClaimedTask struct {
	ID           string
	PartitionID  uint16
	EntityID     string
	EntitySeq    uint64
	Payload      string
	LockValue    string
	LockedUntil  time.Time
	AttemptCount uint32
}

// TaskRepository abstracts all ordered_tasks-table access used by the worker
// loop. All transitions out of locked are conditioned on the lock_value to make
// at-least-once safe.
type TaskRepository interface {
	FetchEligibleHeads(ctx context.Context, partitionID uint16, k int) ([]Candidate, error)
	ClaimTask(ctx context.Context, partitionID uint16, c Candidate, lockValue string, lockedUntil time.Time) (*ClaimedTask, error)
	MarkCompleted(ctx context.Context, task ClaimedTask, doneAt time.Time) error
	MarkFailedWithBackoff(ctx context.Context, task ClaimedTask, retryAt time.Time, lastError string) error
	MarkTerminallyFailed(ctx context.Context, task ClaimedTask, failedAt time.Time, lastError string) error
}
