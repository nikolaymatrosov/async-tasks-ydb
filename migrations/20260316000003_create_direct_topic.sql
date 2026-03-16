-- +goose Up
-- +goose StatementBegin
CREATE TOPIC `tasks/direct` WITH (
    min_active_partitions = 3,
    retention_period = Interval('PT24H')
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TOPIC `tasks/direct`;
-- +goose StatementEnd
