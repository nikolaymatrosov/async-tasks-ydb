package ydbconn

import (
	"context"
	"os"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	yc "github.com/ydb-platform/ydb-go-yc"
)

// Open creates a YDB driver using credentials resolved from environment variables.
// Credential resolution order: YDB_SA_KEY_FILE → YDB_ANONYMOUS_CREDENTIALS=1 → metadata.
func Open(ctx context.Context, endpoint, database string) (*ydb.Driver, error) {
	var creds ydb.Option
	if saKeyFile := os.Getenv("YDB_SA_KEY_FILE"); saKeyFile != "" {
		creds = yc.WithServiceAccountKeyFileCredentials(saKeyFile)
	} else if os.Getenv("YDB_ANONYMOUS_CREDENTIALS") == "1" {
		creds = ydb.WithAnonymousCredentials()
	} else {
		creds = yc.WithMetadataCredentials()
	}

	return ydb.Open(ctx, endpoint,
		creds,
		yc.WithInternalCA(),
	)
}
