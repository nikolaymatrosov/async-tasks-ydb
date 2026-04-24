# Implementation Plan: Bastion VM Migrations via COI + SSH Provisioner

**Branch**: `012-bastion-vm-migrations` | **Date**: 2026-04-24 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/012-bastion-vm-migrations/spec.md`

## Summary

Replace the bastion VM's Ubuntu image with a Container-Optimized Image (COI), attach a dedicated `bastion` service account, and add a Terraform SSH provisioner that runs the migrations container on first VM creation. A new `cmd/migrate/main.go` CLI wraps goose with `yc.WithMetadataCredentials()` so the migration container authenticates to YDB using the VM's attached IAM identity — no static credentials required. `Dockerfile.migrations` is updated to build this binary instead of the standalone `goose` CLI.

## Technical Context

**Language/Version**: HCL (Terraform ≥ 1.5) + Go 1.26 (new `cmd/migrate` binary)
**Primary Dependencies**: `yandex-cloud/yandex` provider (existing); `github.com/ydb-platform/ydb-go-sdk/v3`, `ydb-go-yc`, `pressly/goose/v3` — all already in `go.mod`; no new direct dependencies
**Storage**: YDB Dedicated — existing `coordinated_tasks` and other tables; no schema changes
**Testing**: `terraform validate` + `terraform plan` against live folder; `go vet ./cmd/migrate/` build gate
**Target Platform**: Yandex Cloud COI VM (bastion), `linux/amd64` container
**Project Type**: Terraform infrastructure change + new Go migration CLI
**Performance Goals**: N/A — one-shot migration; must complete before Terraform signals success
**Constraints**: No SA key files on bastion; all YDB auth via metadata service; SSH private key path supplied as variable (never hardcoded)
**Scale/Scope**: 1 new Go file, 1 updated Dockerfile, 4 Terraform files modified, 1 Terraform file modified (iam.tf)

## Constitution Check

| Principle | Check |
| --------- | ----- |
| I. Self-Contained Examples — single `main.go`, own top-level dir, `go run ./<example>/` | N/A — `cmd/migrate` is a tool, not an SDK example |
| II. Lifecycle Completeness — startup, operation, shutdown via `signal.NotifyContext` | ✅ — `cmd/migrate` opens YDB, runs goose Up, defers Close; no long-running goroutines |
| III. Schema-Managed Persistence — all DDL in `migrations/` as goose Up+Down | ✅ — no new migrations; existing files unchanged |
| IV. Environment-Variable Configuration — `YDB_ENDPOINT`/`YDB_SA_KEY_FILE`, no hardcoded creds | ✅ — `YDB_ENDPOINT` required; `YDB_SA_KEY_FILE` optional; metadata auth if absent |
| V. Structured Logging — `log/slog` JSON handler, structured fields, plain stats block | ✅ — `cmd/migrate` uses `slog.NewJSONHandler`; logs each applied migration version |
| Tech Constraints — Go 1.26, YDB SDK, murmur3 for routing, goose for migrations | ✅ — goose v3 used; no new direct deps; no murmur3 needed (not a routing component) |

Post-design re-check: ✅ Design confirms no deviations from applicable principles.

## Project Structure

### Documentation (this feature)

```text
specs/012-bastion-vm-migrations/
├── plan.md         ← this file
├── research.md     ← Phase 0 output
├── data-model.md   ← Phase 1 output
└── tasks.md        ← Phase 2 output (/speckit-tasks command)
```

### Source Code Changes

```text
cmd/
└── migrate/
    └── main.go                           ← new: goose migration CLI with metadata auth

Dockerfile.migrations                     ← update: build cmd/migrate instead of standalone goose

terraform/
├── bastion.tf                            ← update: COI image, SA binding, SSH provisioner
├── variables.tf                          ← update: add ssh_private_key_path
└── modules/
    └── db/
        ├── iam.tf                        ← update: add bastion SA + two IAM bindings
        └── outputs.tf                    ← update: add bastion_service_account_id output
```

**Structure Decision**: `cmd/migrate/` follows the `cmd/` convention established by `04_coordinated_table/cmd/`. No new module is created; the bastion SA lives alongside other IAM resources in `modules/db/iam.tf`.

## Complexity Tracking

| Violation               | Why Needed                                                                                            | Simpler Alternative Rejected Because                                                  |
|-------------------------|-------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------|
| Principle I n/a         | `cmd/migrate` is a tool, not an SDK example; principle I is scoped to "runnable examples"             | Constitution cannot be amended per feature; documented deviation is the correct path  |

## Implementation Notes

### cmd/migrate/main.go (new)

```go
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
```

### Dockerfile.migrations (updated)

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/migrate ./cmd/migrate/

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /app/migrate /app/migrate
COPY migrations/ /migrations/
ENTRYPOINT ["/app/migrate"]
```

Note: `root.crt` copy is removed — `yc.WithInternalCA()` in the binary embeds the Yandex CA cert.

### terraform/modules/db/iam.tf additions

```hcl
resource "yandex_iam_service_account" "bastion" {
  name      = "async-tasks-bastion-sa"
  folder_id = var.folder_id
}

resource "yandex_resourcemanager_folder_iam_member" "bastion_ydb_editor" {
  folder_id = var.folder_id
  role      = "ydb.editor"
  member    = "serviceAccount:${yandex_iam_service_account.bastion.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "bastion_registry_puller" {
  folder_id = var.folder_id
  role      = "container-registry.images.puller"
  member    = "serviceAccount:${yandex_iam_service_account.bastion.id}"
}
```

### terraform/modules/db/outputs.tf addition

```hcl
output "bastion_service_account_id" {
  description = "ID of the bastion VM service account"
  value       = yandex_iam_service_account.bastion.id
}
```

### terraform/bastion.tf (full replacement)

```hcl
data "yandex_compute_image" "coi_bastion" {
  family = "container-optimized-image"
}

resource "yandex_compute_instance" "bastion" {
  name               = "async-tasks-bastion"
  folder_id          = var.folder_id
  zone               = var.zone
  platform_id        = "standard-v3"
  service_account_id = module.db.bastion_service_account_id

  resources {
    cores  = 2
    memory = 2
  }

  boot_disk {
    initialize_params {
      image_id = data.yandex_compute_image.coi_bastion.id
      size     = 10
    }
  }

  network_interface {
    subnet_id      = module.db.subnet_ids[0]
    nat            = true
    nat_ip_address = "111.88.240.80"
  }

  metadata = {
    ssh-keys = "yc-user:${var.ssh_public_key}"
  }

  connection {
    type        = "ssh"
    user        = "yc-user"
    host        = self.network_interface[0].nat_ip_address
    private_key = file(var.ssh_private_key_path)
  }

  provisioner "remote-exec" {
    inline = [
      "docker run --rm -e YDB_ENDPOINT='${module.db.ydb_endpoint}' ${module.workers.migrations_image}",
    ]
  }

  depends_on = [module.workers]
}
```

Key points:
- `depends_on = [module.workers]` ensures `null_resource.migrations_image` in the workers module completes (image built and pushed) before the bastion SSH provisioner runs.
- The `YDB_ENDPOINT` env var is the full `grpcs://...` endpoint — `cmd/migrate` uses it directly.
- User changed from `ubuntu` to `yc-user` (the default COI SSH user).
- The old `data.yandex_compute_image.ubuntu` block is removed entirely.

### terraform/variables.tf addition

```hcl
variable "ssh_private_key_path" {
  description = "Local path to the SSH private key used by the Terraform SSH provisioner on the bastion"
  type        = string
  default     = "~/.ssh/id_rsa"
}
```
