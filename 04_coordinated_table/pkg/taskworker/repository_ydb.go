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

func (r *ydbTaskRepository) FetchEligibleCandidate(ctx context.Context, partitionID uint16) (*Candidate, error) {
	var (
		taskID   string
		priority uint8
		payload  string
		found    bool
	)

	err := r.db.Query().Do(ctx, func(ctx context.Context, s query.Session) error {
		found = false
		rs, err := s.Query(ctx,
			`DECLARE $partition_id AS Uint16;
SELECT id, priority, payload
FROM coordinated_tasks
WHERE partition_id = $partition_id
  AND (
      status = 'pending'
      OR (status = 'locked' AND locked_until < CurrentUtcTimestamp())
  )
  AND (scheduled_at IS NULL OR scheduled_at <= CurrentUtcTimestamp())
ORDER BY priority DESC
LIMIT 1;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$partition_id").Uint16(partitionID).
					Build(),
			),
			query.WithTxControl(query.SnapshotReadOnlyTxControl()),
		)
		if err != nil {
			return fmt.Errorf("snapshot select task: %w", err)
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
		if err := row.ScanNamed(
			query.Named("id", &taskID),
			query.Named("priority", &priority),
			query.Named("payload", &payload),
		); err != nil {
			return fmt.Errorf("scan task row: %w", err)
		}
		found = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &Candidate{ID: taskID, Priority: priority, Payload: payload}, nil
}

func (r *ydbTaskRepository) ClaimTask(ctx context.Context, partitionID uint16, c Candidate, lockValue string, lockedUntil time.Time) (*ClaimedTask, error) {
	var claimed bool

	err := r.db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		claimed = false

		rs, err := tx.Query(ctx,
			`DECLARE $partition_id AS Uint16;
DECLARE $priority AS Uint8;
DECLARE $id AS Utf8;
SELECT status, locked_until
FROM coordinated_tasks
WHERE partition_id = $partition_id
  AND priority = $priority
  AND id = $id;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$partition_id").Uint16(partitionID).
					Param("$priority").Uint8(c.Priority).
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
DECLARE $priority AS Uint8;
DECLARE $lock_value AS Utf8;
DECLARE $locked_until AS Timestamp;
UPDATE coordinated_tasks
SET status = 'locked',
    lock_value = $lock_value,
    locked_until = $locked_until
WHERE partition_id = $partition_id
  AND priority = $priority
  AND id = $id;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$id").Text(c.ID).
					Param("$partition_id").Uint16(partitionID).
					Param("$priority").Uint8(c.Priority).
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
		ID:          c.ID,
		PartitionID: partitionID,
		Priority:    c.Priority,
		Payload:     c.Payload,
		LockValue:   lockValue,
		LockedUntil: lockedUntil,
	}, nil
}

func (r *ydbTaskRepository) MarkCompleted(ctx context.Context, task ClaimedTask, doneAt time.Time) error {
	return r.db.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		return tx.Exec(ctx,
			`DECLARE $id AS Utf8;
DECLARE $partition_id AS Uint16;
DECLARE $priority AS Uint8;
DECLARE $done_at AS Timestamp;
UPDATE coordinated_tasks
SET status = 'completed',
    done_at = $done_at
WHERE partition_id = $partition_id
  AND priority = $priority
  AND id = $id;`,
			query.WithParameters(
				ydb.ParamsBuilder().
					Param("$id").Text(task.ID).
					Param("$partition_id").Uint16(task.PartitionID).
					Param("$priority").Uint8(task.Priority).
					Param("$done_at").Timestamp(doneAt).
					Build(),
			),
		)
	}, query.WithTxSettings(query.TxSettings(query.WithSerializableReadWrite())))
}
