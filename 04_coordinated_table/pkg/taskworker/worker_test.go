package taskworker

import (
	"context"
	"errors"
	"testing"
	"time"

	"async-tasks-ydb/04_coordinated_table/pkg/metrics"
)

// fakeOutcome holds the scripted return values for one repository call.
type fakeOutcome struct {
	candidate *Candidate
	claimed   *ClaimedTask
	err       error
}

// fakeRepository implements TaskRepository via scripted slices. When a slice is
// exhausted the next call returns (nil, context.Canceled) so the partition loop
// exits cleanly without recording an error.
type fakeRepository struct {
	fetchOutcomes  []fakeOutcome
	claimOutcomes  []fakeOutcome
	completeErrors []error
	fetchIdx       int
	claimIdx       int
	completeIdx    int
	completedTasks []ClaimedTask
}

func (f *fakeRepository) FetchEligibleCandidate(_ context.Context, _ uint16) (*Candidate, error) {
	if f.fetchIdx >= len(f.fetchOutcomes) {
		return nil, context.Canceled
	}
	out := f.fetchOutcomes[f.fetchIdx]
	f.fetchIdx++
	return out.candidate, out.err
}

func (f *fakeRepository) ClaimTask(_ context.Context, _ uint16, _ Candidate, _ string, _ time.Time) (*ClaimedTask, error) {
	if f.claimIdx >= len(f.claimOutcomes) {
		return nil, context.Canceled
	}
	out := f.claimOutcomes[f.claimIdx]
	f.claimIdx++
	return out.claimed, out.err
}

func (f *fakeRepository) MarkCompleted(_ context.Context, task ClaimedTask, _ time.Time) error {
	if f.completeIdx >= len(f.completeErrors) {
		return context.Canceled
	}
	err := f.completeErrors[f.completeIdx]
	f.completeIdx++
	if err == nil {
		f.completedTasks = append(f.completedTasks, task)
	}
	return err
}

func newTestWorker(repo TaskRepository) (*Worker, *metrics.Stats) {
	stats := metrics.NewStats("test-worker")
	w := &Worker{
		Repo:         repo,
		WorkerID:     "test-worker",
		LockDuration: 5 * time.Second,
		BackoffMin:   10 * time.Millisecond,
		BackoffMax:   80 * time.Millisecond,
		Stats:        stats,
		SleepFn:      func(d time.Duration) {}, // no-op for fast tests
	}
	return w, stats
}

func runPartition(w *Worker) {
	ctx := context.Background()
	w.processPartition(ctx, ctx, 0)
}

// TestBackoffEscalatesOnEmptyPolls verifies that sleep durations grow from
// BackoffMin up toward BackoffMax when no candidate is found.
func TestBackoffEscalatesOnEmptyPolls(t *testing.T) {
	repo := &fakeRepository{
		fetchOutcomes: []fakeOutcome{
			{nil, nil, nil},
			{nil, nil, nil},
			{nil, nil, nil},
		},
	}

	var recorded []time.Duration
	processCalled := false

	w, _ := newTestWorker(repo)
	w.SleepFn = func(d time.Duration) { recorded = append(recorded, d) }
	w.ProcessTask = func(_ context.Context, _ string, _ string) error {
		processCalled = true
		return nil
	}

	runPartition(w)

	if processCalled {
		t.Fatal("ProcessTask must not be called when no candidate is found")
	}
	if len(recorded) != 3 {
		t.Fatalf("expected 3 sleep calls, got %d", len(recorded))
	}
	min := w.BackoffMin
	max := w.BackoffMax
	expected := []time.Duration{min, min * 2, min * 4}
	for i, want := range expected {
		got := recorded[i]
		if got < want || got > max {
			t.Errorf("sleep[%d]: got %v, want %v (capped at %v)", i, got, want, max)
		}
	}
}

// TestLostRaceNoOp verifies that when ClaimTask returns (nil, nil) the worker
// does not call ProcessTask or MarkCompleted and does not record an error.
func TestLostRaceNoOp(t *testing.T) {
	candidate := &Candidate{ID: "task-1", Priority: 5, Payload: `{}`}
	repo := &fakeRepository{
		fetchOutcomes: []fakeOutcome{{candidate: candidate}},
		claimOutcomes: []fakeOutcome{{}},
	}

	processCalled := false
	w, stats := newTestWorker(repo)
	w.ProcessTask = func(_ context.Context, _ string, _ string) error {
		processCalled = true
		return nil
	}

	runPartition(w)

	if processCalled {
		t.Fatal("ProcessTask must not be called after a lost race")
	}
	if len(repo.completedTasks) != 0 {
		t.Fatal("MarkCompleted must not be called after a lost race")
	}
	if metrics.ReadCounter(stats.Errors) != 0 {
		t.Fatalf("Stats.Errors: got %d, want 0", metrics.ReadCounter(stats.Errors))
	}
	if metrics.ReadCounter(stats.Locked) != 0 {
		t.Fatalf("Stats.Locked: got %d, want 0", metrics.ReadCounter(stats.Locked))
	}
}

// TestSuccessfulClaimProcessComplete verifies the happy path: fetch → claim →
// process → complete, with counters correctly incremented.
func TestSuccessfulClaimProcessComplete(t *testing.T) {
	candidate := &Candidate{ID: "task-2", Priority: 10, Payload: `{"url":"http://example.com"}`}
	claimed := &ClaimedTask{
		ID:          "task-2",
		PartitionID: 0,
		Priority:    10,
		Payload:     `{"url":"http://example.com"}`,
		LockValue:   "lock-abc",
		LockedUntil: time.Now().UTC().Add(5 * time.Second),
	}
	repo := &fakeRepository{
		fetchOutcomes:  []fakeOutcome{{candidate: candidate}},
		claimOutcomes:  []fakeOutcome{{claimed: claimed}},
		completeErrors: []error{nil},
	}

	w, stats := newTestWorker(repo)
	w.ProcessTask = func(_ context.Context, _ string, _ string) error { return nil }

	runPartition(w)

	if len(repo.completedTasks) != 1 {
		t.Fatalf("MarkCompleted call count: got %d, want 1", len(repo.completedTasks))
	}
	got := repo.completedTasks[0]
	if got.ID != claimed.ID {
		t.Errorf("completed task ID: got %q, want %q", got.ID, claimed.ID)
	}
	if got.LockValue != claimed.LockValue {
		t.Errorf("completed task LockValue: got %q, want %q", got.LockValue, claimed.LockValue)
	}
	if metrics.ReadCounter(stats.Processed) != 1 {
		t.Fatalf("Stats.Processed: got %d, want 1", metrics.ReadCounter(stats.Processed))
	}
	if metrics.ReadCounter(stats.Locked) != 1 {
		t.Fatalf("Stats.Locked: got %d, want 1", metrics.ReadCounter(stats.Locked))
	}
}

// TestProcessorErrorSkipsCompletion verifies that when ProcessTask returns an
// error, MarkCompleted is never called and backoff is not reset.
func TestProcessorErrorSkipsCompletion(t *testing.T) {
	candidate := &Candidate{ID: "task-3", Priority: 1, Payload: `{}`}
	claimed := &ClaimedTask{
		ID:          "task-3",
		PartitionID: 0,
		Priority:    1,
		Payload:     `{}`,
		LockValue:   "lock-xyz",
		LockedUntil: time.Now().UTC().Add(5 * time.Second),
	}
	// Two fetch outcomes: first returns the candidate (for claim+process attempt),
	// second returns nil so the worker sleeps before exiting via exhausted script.
	repo := &fakeRepository{
		fetchOutcomes: []fakeOutcome{{candidate: candidate}, {}},
		claimOutcomes: []fakeOutcome{{claimed: claimed}},
	}

	var recorded []time.Duration
	w, stats := newTestWorker(repo)
	w.SleepFn = func(d time.Duration) { recorded = append(recorded, d) }
	w.ProcessTask = func(_ context.Context, _ string, _ string) error {
		return errors.New("processing failed")
	}

	runPartition(w)

	if len(repo.completedTasks) != 0 {
		t.Fatal("MarkCompleted must not be called when ProcessTask fails")
	}
	if metrics.ReadCounter(stats.Errors) != 1 {
		t.Fatalf("Stats.Errors: got %d, want 1", metrics.ReadCounter(stats.Errors))
	}
	// After a processor error, backoff is not reset (reset happens only on
	// successful claim). The next nil-candidate iteration sleeps at BackoffMin.
	if len(recorded) == 0 {
		t.Fatal("expected a sleep from the nil-candidate iteration after processor error")
	}
	if recorded[0] < w.BackoffMin {
		t.Errorf("sleep after processor error: got %v, want >= %v", recorded[0], w.BackoffMin)
	}
}

// TestTransientRepositoryError verifies that a transient FetchEligibleCandidate
// error increments Stats.Errors, escalates backoff, and allows the loop to continue.
func TestTransientRepositoryError(t *testing.T) {
	transientErr := errors.New("ydb: transient error")
	repo := &fakeRepository{
		fetchOutcomes: []fakeOutcome{
			{nil, nil, transientErr},
		},
	}

	var recorded []time.Duration
	w, stats := newTestWorker(repo)
	w.SleepFn = func(d time.Duration) { recorded = append(recorded, d) }

	runPartition(w)

	if metrics.ReadCounter(stats.Errors) != 1 {
		t.Fatalf("Stats.Errors: got %d, want 1", metrics.ReadCounter(stats.Errors))
	}
	if len(recorded) == 0 {
		t.Fatal("expected a sleep after transient error")
	}
	if recorded[0] < w.BackoffMin {
		t.Errorf("backoff after transient error: got %v, want >= %v", recorded[0], w.BackoffMin)
	}
}

// TestLeaseCancellationExitsCleanly verifies that a cancelled lease context
// causes the partition loop to exit without recording an error.
func TestLeaseCancellationExitsCleanly(t *testing.T) {
	repo := &fakeRepository{}
	w, stats := newTestWorker(repo)

	leaseCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	w.processPartition(context.Background(), leaseCtx, 0)

	if metrics.ReadCounter(stats.Errors) != 0 {
		t.Fatalf("Stats.Errors: got %d, want 0 (lease cancellation is not an error)", metrics.ReadCounter(stats.Errors))
	}
}
