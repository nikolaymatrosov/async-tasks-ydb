package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	"github.com/pressly/goose/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3"
	yc "github.com/ydb-platform/ydb-go-yc"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	endpoint := os.Getenv("YDB_ENDPOINT")
	if endpoint == "" {
		slog.Error("YDB_ENDPOINT is required")
		os.Exit(1)
	}

	opts := []ydb.Option{yc.WithInternalCA()}
	if keyFile := os.Getenv("YDB_SA_KEY_FILE"); keyFile != "" {
		opts = append(opts, yc.WithServiceAccountKeyFileCredentials(keyFile))
	} else {
		opts = append(opts, yc.WithMetadataCredentials())
	}

	ctx := context.Background()
	nativeDriver, err := ydb.Open(ctx, endpoint, opts...)
	if err != nil {
		slog.Error("ydb.Open failed", "err", err)
		os.Exit(1)
	}
	defer nativeDriver.Close(context.Background()) //nolint:errcheck

	connector, err := ydb.Connector(nativeDriver,
		ydb.WithDefaultQueryMode(ydb.ScriptingQueryMode),
		ydb.WithFakeTx(ydb.ScriptingQueryMode),
		ydb.WithAutoDeclare(),
		ydb.WithNumericArgs(),
	)
	if err != nil {
		slog.Error("ydb.Connector failed", "err", err)
		os.Exit(1)
	}

	sqlDB := sql.OpenDB(connector)
	defer sqlDB.Close() //nolint:errcheck

	fsys := os.DirFS("/migrations")
	provider, err := goose.NewProvider(goose.DialectYdB, sqlDB, fsys)
	if err != nil {
		slog.Error("goose.NewProvider failed", "err", err)
		os.Exit(1)
	}
	defer provider.Close() //nolint:errcheck

	results, err := provider.Up(ctx)
	if err != nil {
		slog.Error("goose Up failed", "err", err)
		os.Exit(1)
	}
	for _, r := range results {
		slog.Info("migration applied", "version", r.Source.Version, "duration_ms", r.Duration.Milliseconds())
	}
}
