package main

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"

	"async-tasks-ydb/testhelper"
)

func TestStatsWorkload_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	endpoint := testhelper.StartYDB(t)
	testhelper.ApplyMigrations(t, endpoint, "../migrations")
	db := testhelper.OpenDB(t, endpoint)

	workload := statsWorkload(db, &atomic.Int64{})
	ctx := context.Background()

	t.Run("IncrementA", func(t *testing.T) {
		userID := uuid.New()
		if err := workload(ctx, BenchMessage{ID: uuid.New(), UserID: userID, Type: "A"}); err != nil {
			t.Fatalf("workload: %v", err)
		}
		a, b, c, err := queryStatsRow(ctx, db, userID)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if a != 1 || b != 0 || c != 0 {
			t.Errorf("got (a=%d, b=%d, c=%d), want (1, 0, 0)", a, b, c)
		}
	})

	t.Run("IncrementB", func(t *testing.T) {
		userID := uuid.New()
		if err := workload(ctx, BenchMessage{ID: uuid.New(), UserID: userID, Type: "B"}); err != nil {
			t.Fatalf("workload: %v", err)
		}
		a, b, c, err := queryStatsRow(ctx, db, userID)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if a != 0 || b != 1 || c != 0 {
			t.Errorf("got (a=%d, b=%d, c=%d), want (0, 1, 0)", a, b, c)
		}
	})

	t.Run("IncrementC", func(t *testing.T) {
		userID := uuid.New()
		if err := workload(ctx, BenchMessage{ID: uuid.New(), UserID: userID, Type: "C"}); err != nil {
			t.Fatalf("workload: %v", err)
		}
		a, b, c, err := queryStatsRow(ctx, db, userID)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if a != 0 || b != 0 || c != 1 {
			t.Errorf("got (a=%d, b=%d, c=%d), want (0, 0, 1)", a, b, c)
		}
	})

	t.Run("AccumulatesCountsForSameUser", func(t *testing.T) {
		userID := uuid.New()
		msgs := []BenchMessage{
			{ID: uuid.New(), UserID: userID, Type: "A"},
			{ID: uuid.New(), UserID: userID, Type: "A"},
			{ID: uuid.New(), UserID: userID, Type: "B"},
			{ID: uuid.New(), UserID: userID, Type: "C"},
			{ID: uuid.New(), UserID: userID, Type: "C"},
			{ID: uuid.New(), UserID: userID, Type: "C"},
		}
		for _, msg := range msgs {
			if err := workload(ctx, msg); err != nil {
				t.Fatalf("workload: %v", err)
			}
		}
		a, b, c, err := queryStatsRow(ctx, db, userID)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if a != 2 || b != 1 || c != 3 {
			t.Errorf("got (a=%d, b=%d, c=%d), want (2, 1, 3)", a, b, c)
		}
	})

	t.Run("IndependentUsersDoNotInterfere", func(t *testing.T) {
		userA, userB := uuid.New(), uuid.New()
		if err := workload(ctx, BenchMessage{ID: uuid.New(), UserID: userA, Type: "A"}); err != nil {
			t.Fatalf("workload userA: %v", err)
		}
		if err := workload(ctx, BenchMessage{ID: uuid.New(), UserID: userB, Type: "B"}); err != nil {
			t.Fatalf("workload userB: %v", err)
		}
		aA, bA, cA, err := queryStatsRow(ctx, db, userA)
		if err != nil {
			t.Fatalf("query userA: %v", err)
		}
		aB, bB, cB, err := queryStatsRow(ctx, db, userB)
		if err != nil {
			t.Fatalf("query userB: %v", err)
		}
		if aA != 1 || bA != 0 || cA != 0 {
			t.Errorf("userA: got (a=%d, b=%d, c=%d), want (1, 0, 0)", aA, bA, cA)
		}
		if aB != 0 || bB != 1 || cB != 0 {
			t.Errorf("userB: got (a=%d, b=%d, c=%d), want (0, 1, 0)", aB, bB, cB)
		}
	})

	t.Run("ConcurrentUpdatesAreSerialized", func(t *testing.T) {
		userID := uuid.New()
		const goroutines = 10
		const msgsPerGoroutine = 5

		errCh := make(chan error, goroutines)
		for range goroutines {
			go func() {
				for range msgsPerGoroutine {
					if err := workload(ctx, BenchMessage{ID: uuid.New(), UserID: userID, Type: "A"}); err != nil {
						errCh <- err
						return
					}
				}
				errCh <- nil
			}()
		}

		for range goroutines {
			if err := <-errCh; err != nil {
				t.Errorf("concurrent workload: %v", err)
			}
		}

		a, _, _, err := queryStatsRow(ctx, db, userID)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		want := int64(goroutines * msgsPerGoroutine)
		if a != want {
			t.Errorf("got a=%d, want %d (serializable RMW lost updates)", a, want)
		}
	})
}

// queryStatsRow fetches (a, b, c) for userID from the stats table.
func queryStatsRow(ctx context.Context, db *ydb.Driver, userID uuid.UUID) (a, b, c int64, err error) {
	row, err := db.Query().QueryRow(ctx,
		`SELECT a, b, c FROM stats WHERE user_id = $user_id`,
		query.WithParameters(
			ydb.ParamsBuilder().Param("$user_id").Uuid(userID).Build(),
		),
	)
	if err != nil {
		return 0, 0, 0, err
	}

	var pa, pb, pc *int64
	if err := row.ScanNamed(
		query.Named("a", &pa),
		query.Named("b", &pb),
		query.Named("c", &pc),
	); err != nil {
		return 0, 0, 0, err
	}

	if pa != nil {
		a = *pa
	}
	if pb != nil {
		b = *pb
	}
	if pc != nil {
		c = *pc
	}

	return a, b, c, nil
}
