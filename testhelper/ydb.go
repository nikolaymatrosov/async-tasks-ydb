// Package testhelper provides shared test infrastructure for async-tasks-ydb integration tests.
package testhelper

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/balancers"
)

const YDBImage = "ydbplatform/local-ydb:25.4"

// StartYDB starts a local YDB container and returns the grpc endpoint.
// The container is terminated via t.Cleanup.
func StartYDB(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	t.Log("Going to start YDB")
	req := testcontainers.ContainerRequest{
		Image: YDBImage,
		Env: map[string]string{
			"GRPC_PORT": "2136",
			"MON_PORT":  "8765",
		},
		ExposedPorts: []string{"2136/tcp", "8765/tcp"},
		WaitingFor: wait.ForHTTP("/healthcheck").
			WithPort("8765/tcp").
			WithResponseMatcher(func(body io.Reader) bool {
				var resp struct {
					SelfCheckResult string `json:"self_check_result"`
				}
				return json.NewDecoder(body).Decode(&resp) == nil && resp.SelfCheckResult == "GOOD"
			}).
			WithStartupTimeout(2 * time.Minute),
		HostConfigModifier: func(hostConfig *container.HostConfig) {
			hostConfig.AutoRemove = true
			
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start YDB container: %v", err)
	}
	t.Log("started")

	t.Cleanup(func() {
		container.Terminate(context.Background()) //nolint:errcheck
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "2136")
	if err != nil {
		t.Fatalf("get mapped port: %v", err)
	}

	return fmt.Sprintf("grpc://%s:%s/local", host, port.Port())
}

// ApplyMigrations runs all goose migrations from migrationsDir against the given YDB endpoint.
func ApplyMigrations(t *testing.T, endpoint, migrationsDir string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use SingleConn balancer so the SDK never follows YDB's node discovery to
	// the container-internal hostname (e.g. the container short ID), which is
	// not resolvable from the host machine.
	nativeDriver, err := ydb.Open(ctx, endpoint, ydb.WithBalancer(balancers.SingleConn()))
	if err != nil {
		t.Fatalf("ydb.Open for goose: %v", err)
	}
	defer nativeDriver.Close(context.Background()) //nolint:errcheck

	// Build a database/sql connector with scripting mode required for DDL.
	connector, err := ydb.Connector(nativeDriver,
		ydb.WithDefaultQueryMode(ydb.ScriptingQueryMode),
		ydb.WithFakeTx(ydb.ScriptingQueryMode),
		ydb.WithAutoDeclare(),
		ydb.WithNumericArgs(),
	)
	if err != nil {
		t.Fatalf("ydb.Connector: %v", err)
	}

	sqlDB := sql.OpenDB(connector)
	defer sqlDB.Close() //nolint:errcheck

	fsys := os.DirFS(migrationsDir)

	provider, err := goose.NewProvider(goose.DialectYdB, sqlDB, fsys)
	if err != nil {
		t.Fatalf("goose.NewProvider: %v", err)
	}
	defer provider.Close() //nolint:errcheck

	results, err := provider.Up(ctx)
	if err != nil {
		t.Fatalf("goose Up: %v", err)
	}
	for _, r := range results {
		t.Logf("migration %d applied in %s", r.Source.Version, r.Duration)
	}
}

// OpenDB opens a YDB SDK driver against the given endpoint (no TLS, no auth).
// The driver is closed via t.Cleanup.
func OpenDB(t *testing.T, endpoint string) *ydb.Driver {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := ydb.Open(ctx, endpoint, ydb.WithBalancer(balancers.SingleConn()))
	if err != nil {
		t.Fatalf("ydb.Open: %v", err)
	}

	t.Cleanup(func() {
		db.Close(context.Background()) //nolint:errcheck
	})

	return db
}
