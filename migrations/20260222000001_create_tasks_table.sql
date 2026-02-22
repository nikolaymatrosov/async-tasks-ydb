-- +goose Up
-- +goose StatementBegin
CREATE TABLE tasks (
    id UUID NOT NULL,
    payload String NOT NULL,
    created_at Timestamp NOT NULL,
    done_at Timestamp,
    PRIMARY KEY (id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE tasks;
-- +goose StatementEnd
