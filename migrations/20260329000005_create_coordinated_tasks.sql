-- +goose Up
-- +goose StatementBegin
CREATE TABLE coordinated_tasks (
    id          Utf8            NOT NULL,
    hash        Int64           NOT NULL,
    partition_id Uint16         NOT NULL,
    priority    Uint8           NOT NULL,
    status      Utf8            NOT NULL,
    payload     Utf8            NOT NULL,
    lock_value  Utf8,
    locked_until Timestamp,
    scheduled_at Timestamp,
    created_at  Timestamp       NOT NULL,
    done_at     Timestamp,
    PRIMARY KEY (partition_id, priority, id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE coordinated_tasks;
-- +goose StatementEnd
