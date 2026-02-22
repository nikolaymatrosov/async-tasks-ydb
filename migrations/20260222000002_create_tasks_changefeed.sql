-- +goose Up
-- +goose StatementBegin
ALTER TABLE tasks ADD CHANGEFEED cdc_tasks WITH (
    FORMAT = 'JSON',
    MODE = 'NEW_IMAGE'
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tasks DROP CHANGEFEED cdc_tasks;
-- +goose StatementEnd
