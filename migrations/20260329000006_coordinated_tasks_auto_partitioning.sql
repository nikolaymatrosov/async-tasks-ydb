-- +goose Up
-- +goose StatementBegin
ALTER TABLE coordinated_tasks SET (
    AUTO_PARTITIONING_BY_LOAD = ENABLED
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE coordinated_tasks SET (
    AUTO_PARTITIONING_BY_LOAD = DISABLED
);
-- +goose StatementEnd
