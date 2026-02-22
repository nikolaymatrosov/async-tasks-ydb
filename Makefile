.PHONY: migrate migrate-status migrate-up-one migrate-down migrate-reset migrate-redo

# Load environment variables from .env file
include .env
export

# Build the YDB connection string with goose parameters and IAM token
YDB_CONNECTION_STRING := $(YDB_ENDPOINT)&go_query_mode=scripting&go_fake_tx=scripting&go_query_bind=declare,numeric&token=`yc iam create-token`

# Apply all pending migrations
migrate:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" up

# Check migration status
migrate-status:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" status

# Apply one migration
migrate-up-one:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" up-by-one

# Rollback the last migration
migrate-down:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" down

# Rollback all migrations
migrate-reset:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" reset

# Re-apply the latest migration
migrate-redo:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" redo
