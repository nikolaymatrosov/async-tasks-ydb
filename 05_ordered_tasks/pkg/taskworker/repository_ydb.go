package taskworker

import (
	"context"
	"fmt"
	"time"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
)

type ydbTaskRepository struct {
	db *ydb.Driver
}

// NewYDBRepository constructs a TaskRepository backed by a YDB driver.
func NewYDBRepository(db *ydb.Driver) TaskRepository {
	return &ydbTaskRepository{db: db}
}

func (r *ydbTaskRepository) FetchEligibleHeads(ctx context.Context, partitionID uint16, k int) ([]Candidate, error) {
	if k <= 0 {
		k = 32
	}

	out := make([]Candidate, 0, k)

	err := r.db.Query().Do(ctx, func(ctx context.Context, s query.Session) error {
		out = out[:0]
		rs, err := s.Query(ctx,
			`DECLARE $partition_id AS Uint16;
DECLARE $k AS Uint64;
SELECT id, entity_id, entity_seq, payload, status, scheduled_at, locked_until, attempt_count
FROM ordered_tasks VIEW idx_partition_entity_seq
WHERE partition_id = $partition_id
  AND status IN ('pending', 'locked')
ORDER BY entity_id, entity_seq
LIMIT $k;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$partition_id").Uint16(partitionID).
					Param("$k").Uint64(uint64(k)).
					Build(),
			),
			query.WithTxControl(query.SnapshotReadOnlyTxControl()),
		)
		if err != nil {
			return fmt.Errorf("snapshot select heads: %w", err)
		}
		defer rs.Close(ctx) //nolint:errcheck

		resultSet, err := rs.NextResultSet(ctx)
		if err != nil {
			return fmt.Errorf("next result set: %w", err)
		}

		for {
			row, err := resultSet.NextRow(ctx)
			if err != nil {
				return nil
			}
			var (
				id           string
				entityID     string
				entitySeq    uint64
				payload      string
				status       string
				scheduledAt  *time.Time
				lockedUntil  *time.Time
				attemptCount uint32
			)
			if err := row.ScanNamed(
				query.Named("id", &id),
				query.Named("entity_id", &entityID),
				query.Named("entity_seq", &entitySeq),
				query.Named("payload", &payload),
				query.Named("status", &status),
				query.Named("scheduled_at", &scheduledAt),
				query.Named("locked_until", &lockedUntil),
				query.Named("attempt_count", &attemptCount),
			); err != nil {
				return fmt.Errorf("scan candidate row: %w", err)
			}
			out = append(out, Candidate{
				ID:           id,
				EntityID:     entityID,
				EntitySeq:    entitySeq,
				Payload:      payload,
				Status:       status,
				ScheduledAt:  scheduledAt,
				LockedUntil:  lockedUntil,
				AttemptCount: attemptCount,
			})
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ydbTaskRepository) ClaimTask(ctx context.Context, partitionID uint16, c Candidate, lockValue string, lockedUntil time.Time) (*ClaimedTask, error) {
	var claimed bool
	var attemptCount uint32

	err := r.db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		claimed = false

		rs, err := tx.Query(ctx,
			`DECLARE $partition_id AS Uint16;
DECLARE $id AS Utf8;
SELECT status, locked_until, attempt_count
FROM ordered_tasks
WHERE partition_id = $partition_id
  AND id = $id;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$partition_id").Uint16(partitionID).
					Param("$id").Text(c.ID).
					Build(),
			),
		)
		if err != nil {
			return fmt.Errorf("point select task: %w", err)
		}
		defer rs.Close(ctx) //nolint:errcheck

		resultSet, err := rs.NextResultSet(ctx)
		if err != nil {
			return fmt.Errorf("next result set: %w", err)
		}
		row, err := resultSet.NextRow(ctx)
		if err != nil {
			return nil
		}

		var status string
		var currentLockedUntil *time.Time
		if err := row.ScanNamed(
			query.Named("status", &status),
			query.Named("locked_until", &currentLockedUntil),
			query.Named("attempt_count", &attemptCount),
		); err != nil {
			return fmt.Errorf("scan status row: %w", err)
		}

		claimable := status == "pending" ||
			(status == "locked" && currentLockedUntil != nil && currentLockedUntil.Before(time.Now().UTC()))
		if !claimable {
			return nil
		}

		if err := tx.Exec(ctx,
			`DECLARE $id AS Utf8;
DECLARE $partition_id AS Uint16;
DECLARE $lock_value AS Utf8;
DECLARE $locked_until AS Timestamp;
UPDATE ordered_tasks
SET status = 'locked',
    lock_value = $lock_value,
    locked_until = $locked_until
WHERE partition_id = $partition_id
  AND id = $id;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$id").Text(c.ID).
					Param("$partition_id").Uint16(partitionID).
					Param("$lock_value").Text(lockValue).
					Param("$locked_until").Timestamp(lockedUntil).
					Build(),
			),
		); err != nil {
			return fmt.Errorf("update task lock: %w", err)
		}

		claimed = true
		return nil
	}, query.WithTxSettings(query.TxSettings(query.WithSerializableReadWrite())))

	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, nil
	}
	return &ClaimedTask{
		ID:           c.ID,
		PartitionID:  partitionID,
		EntityID:     c.EntityID,
		EntitySeq:    c.EntitySeq,
		Payload:      c.Payload,
		LockValue:    lockValue,
		LockedUntil:  lockedUntil,
		AttemptCount: attemptCount,
	}, nil
}

func (r *ydbTaskRepository) MarkCompleted(ctx context.Context, task ClaimedTask, doneAt time.Time) error {
	return r.db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		return tx.Exec(ctx,
			`DECLARE $id AS Utf8;
DECLARE $partition_id AS Uint16;
DECLARE $lock_value AS Utf8;
DECLARE $done_at AS Timestamp;
UPDATE ordered_tasks
SET status = 'completed',
    done_at = $done_at,
    lock_value = NULL,
    locked_until = NULL
WHERE partition_id = $partition_id
  AND id = $id
  AND status = 'locked'
  AND lock_value = $lock_value;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$id").Text(task.ID).
					Param("$partition_id").Uint16(task.PartitionID).
					Param("$lock_value").Text(task.LockValue).
					Param("$done_at").Timestamp(doneAt).
					Build(),
			),
		)
	}, query.WithTxSettings(query.TxSettings(query.WithSerializableReadWrite())))
}

func (r *ydbTaskRepository) MarkFailedWithBackoff(ctx context.Context, task ClaimedTask, retryAt time.Time, lastError string) error {
	return r.db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		return tx.Exec(ctx,
			`DECLARE $id AS Utf8;
DECLARE $partition_id AS Uint16;
DECLARE $lock_value AS Utf8;
DECLARE $retry_at AS Timestamp;
DECLARE $last_error AS Utf8;
UPDATE ordered_tasks
SET status = 'pending',
    lock_value = NULL,
    locked_until = NULL,
    scheduled_at = $retry_at,
    attempt_count = attempt_count + 1u,
    last_error = $last_error
WHERE partition_id = $partition_id
  AND id = $id
  AND status = 'locked'
  AND lock_value = $lock_value;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$id").Text(task.ID).
					Param("$partition_id").Uint16(task.PartitionID).
					Param("$lock_value").Text(task.LockValue).
					Param("$retry_at").Timestamp(retryAt).
					Param("$last_error").Text(lastError).
					Build(),
			),
		)
	}, query.WithTxSettings(query.TxSettings(query.WithSerializableReadWrite())))
}

func (r *ydbTaskRepository) MarkTerminallyFailed(ctx context.Context, task ClaimedTask, failedAt time.Time, lastError string) error {
	return r.db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		return tx.Exec(ctx,
			`DECLARE $id AS Utf8;
DECLARE $partition_id AS Uint16;
DECLARE $lock_value AS Utf8;
DECLARE $failed_at AS Timestamp;
DECLARE $last_error AS Utf8;
UPDATE ordered_tasks
SET status = 'failed',
    done_at = $failed_at,
    last_error = $last_error,
    lock_value = NULL,
    locked_until = NULL
WHERE partition_id = $partition_id
  AND id = $id
  AND status = 'locked'
  AND lock_value = $lock_value;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$id").Text(task.ID).
					Param("$partition_id").Uint16(task.PartitionID).
					Param("$lock_value").Text(task.LockValue).
					Param("$failed_at").Timestamp(failedAt).
					Param("$last_error").Text(lastError).
					Build(),
			),
		)
	}, query.WithTxSettings(query.TxSettings(query.WithSerializableReadWrite())))
}
