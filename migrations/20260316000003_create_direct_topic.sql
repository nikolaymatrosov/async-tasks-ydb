-- +goose Up
-- +goose StatementBegin
CREATE TOPIC `task_topics/direct` WITH (
    min_active_partitions = 3,
    retention_period = Interval('PT24H')
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TOPIC `task_topics/direct`;
-- +goose StatementEnd
