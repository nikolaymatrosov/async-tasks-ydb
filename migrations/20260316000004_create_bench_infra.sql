-- +goose Up
-- +goose StatementBegin
CREATE TOPIC `tasks/by_user` (
    CONSUMER `bench-byuser-stats`,
    CONSUMER `bench-byuser-processed`
) WITH (
    min_active_partitions = 10,
    retention_period = Interval('PT24H')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TOPIC `tasks/by_message_id` (
    CONSUMER `bench-bymsgid-stats`,
    CONSUMER `bench-bymsgid-processed`
) WITH (
    min_active_partitions = 10,
    retention_period = Interval('PT24H')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE stats (
    user_id UUID NOT NULL,
    a Int64,
    b Int64,
    c Int64,
    PRIMARY KEY (user_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE processed (
    id UUID NOT NULL,
    PRIMARY KEY (id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE processed;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE stats;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TOPIC `tasks/by_message_id`;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TOPIC `tasks/by_user`;
-- +goose StatementEnd
