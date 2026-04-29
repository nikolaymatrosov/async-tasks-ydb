-- +goose Up
-- +goose StatementBegin
CREATE TABLE ordered_tasks (
    id            Utf8         NOT NULL,
    partition_id  Uint16       NOT NULL,
    entity_id     Utf8         NOT NULL,
    entity_seq    Uint64       NOT NULL,
    status        Utf8         NOT NULL,
    payload       Utf8         NOT NULL,
    lock_value    Utf8,
    locked_until  Timestamp,
    scheduled_at  Timestamp,
    attempt_count Uint32       NOT NULL,
    last_error    Utf8,
    resolved_by   Utf8,
    resolved_at   Timestamp,
    created_at    Timestamp    NOT NULL,
    done_at       Timestamp,
    PRIMARY KEY (partition_id, id),
    INDEX idx_partition_entity_seq GLOBAL ON (partition_id, entity_id, entity_seq)
      COVER (id, payload, status, scheduled_at, locked_until, attempt_count)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE ordered_tasks;
-- +goose StatementEnd
