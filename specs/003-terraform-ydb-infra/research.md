# Research: Terraform Infrastructure for YDB Cluster and Container-Optimized VMs

**Feature Branch**: `003-terraform-ydb-infra` | **Date**: 2026-03-17

## R-001: Yandex Cloud Terraform Provider

**Decision**: Use the `yandex-cloud/yandex` Terraform provider for all infrastructure provisioning.

**Rationale**: This is the official and only maintained provider for Yandex Cloud. It covers all required resource types: YDB, Compute, Container Registry, VPC, IAM.

**Alternatives considered**:

- Manual CLI provisioning via `yc` — rejected because it's not declarative/idempotent (violates FR-008).
- Pulumi or other IaC — rejected because Terraform is the de facto standard for Yandex Cloud with first-party support.

**Key details**:

- Provider source: `yandex-cloud/yandex`
- Authentication: `service_account_key_file` variable pointing to SA JSON key (same `sa.json` already in repo root)
- Required variables: `cloud_id`, `folder_id`, `zone`, `sa_key_file`

## R-002: YDB Database Resource Type

**Decision**: Use `yandex_ydb_database_serverless` for the managed YDB instance.

**Rationale**: The existing `.env` endpoint (`grpcs://lb.etndn1mfvf8mtl9qslvn.ydb.mdb.yandexcloud.net:2135`) points to a managed YDB. Serverless mode requires no capacity planning, fits the experimental/example nature of this project, and has no minimum cost when idle.

**Alternatives considered**:

- `yandex_ydb_database_dedicated` — rejected because dedicated clusters require specifying compute resources and have a baseline cost even when idle. Overkill for an example/learning repository.

**Key resource arguments**:

- `name`: database name
- `folder_id`: from variable
- `location_id`: regional location (e.g., `ru-central1`)

## R-003: Container-Optimized Image (COI) VM

**Decision**: Use `yandex_compute_instance` with `data.yandex_compute_image` family `container-optimized-image` and a Docker Compose specification in metadata.

**Rationale**: COI is Yandex Cloud's purpose-built VM image for running Docker containers. It includes Ubuntu LTS + Docker + a daemon that reads container declarations from VM metadata and auto-starts containers on boot.

**Alternatives considered**:

- Plain Ubuntu VM + manual Docker install — rejected because COI handles container lifecycle natively with less configuration.
- Kubernetes (Managed K8s) — rejected because it's massive overkill for running 3 example containers.

**Key details**:

- Image family: `container-optimized-image` (fetched via `data "yandex_compute_image"`)
- Container declaration: passed via `metadata.docker-compose` key as a Docker Compose YAML file
- Two metadata modes exist: `docker-container-declaration` (single container, K8s pod spec) vs `docker-compose` (multi-container, Docker Compose format). **Must use `docker-compose`** since we have 3 example apps.
- Cannot use both keys simultaneously — the daemon errors if both are present.

**COI specification format (Docker Compose)**:

```yaml
version: '3.7'
services:
  example-01:
    container_name: db-producer
    image: cr.yandex/<registry_id>/01_db_producer:latest
    restart: always
    environment:
      - YDB_ENDPOINT=grpcs://<ydb_endpoint>
      - YDB_SA_KEY_FILE=/secrets/sa.json
    volumes:
      - /etc/secrets:/secrets:ro
```

## R-004: Distroless Go Container Images

**Decision**: Use multi-stage Dockerfile with `golang:1.26-alpine` builder and `gcr.io/distroless/static-debian12:nonroot` runtime.

**Rationale**: Distroless static is the smallest possible base (~2 MiB) for statically-compiled Go binaries. The `:nonroot` tag runs as UID 65532 for security. Go binaries compiled with `CGO_ENABLED=0` are fully self-contained and need no OS libraries.

**Alternatives considered**:

- `scratch` — rejected because it lacks `/etc/passwd`, timezone data, and CA certificates that Go programs may need.
- `alpine` — rejected because it includes a shell and package manager (unnecessary attack surface, violates SC-005 "no unnecessary OS components").

**Dockerfile pattern**:

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/binary ./<example>/

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /app/binary /app/binary
ENTRYPOINT ["/app/binary"]
```

**Key constraints**:

- `CGO_ENABLED=0` is mandatory — distroless has no libc
- `-ldflags="-w -s"` strips debug symbols for smaller images
- `ENTRYPOINT` must be vector form (no shell in distroless)
- One Dockerfile per example, or a parameterized Dockerfile with build args

## R-005: Private Container Registry Authentication

**Decision**: Use VM service account with `container-registry.images.puller` IAM role for automatic registry authentication. No explicit credentials in Docker Compose.

**Rationale**: COI VMs automatically authenticate to Yandex Container Registry via the VM's attached service account when it has the appropriate role. This avoids storing registry credentials in metadata.

**Alternatives considered**:

- OAuth token in Docker Compose `imagePullSecrets` — rejected because it requires credential rotation and storage.
- Public registry — rejected because it exposes images publicly.

## R-006: VM-to-YDB Authentication

**Decision**: Use the metadata service (VM service account) for YDB authentication from containers. The VM's service account gets the `ydb.editor` role.

**Rationale**: The existing examples use `ydb-go-yc` with `yc.WithServiceAccountKeyFileCredentials` reading from `YDB_SA_KEY_FILE`. On a COI VM, we can either:

1. Mount the SA key file into containers and use the existing code path, or
2. Use the metadata service (instance identity) which `ydb-go-yc` supports via `yc.WithMetadataCredentials`.

Option 1 is simpler because it requires zero changes to existing example code. The SA key file is mounted as a read-only volume from the host.

**Alternatives considered**:

- Metadata-based auth (`yc.WithMetadataCredentials`) — would require modifying all example code. Could be a future improvement but out of scope.

## R-007: Networking

**Decision**: Create a VPC network and subnet in the same availability zone. YDB serverless is accessed via its public gRPC endpoint (already TLS-encrypted). The VM needs a public IP (NAT) for pulling container images from `cr.yandex`.

**Rationale**: YDB Serverless databases in Yandex Cloud expose a public gRPC endpoint by default. The VM accesses it over the internet (TLS-secured). A private endpoint would require a more complex VPC setup that's unnecessary for an example project.

**Alternatives considered**:

- Private Service Connect to YDB — unnecessary complexity for example use case.
- No public IP on VM — would require NAT gateway for pulling images, adding cost and complexity.

## R-008: Terraform Project Structure

**Decision**: Place all Terraform files in a `terraform/` directory at the repository root. Dockerfiles go in the repo root (one per example or a single parameterized one).

**Rationale**: Keeps infrastructure code separate from application code. Standard Terraform project layout.

**File layout**:

```
terraform/
├── main.tf              # Provider, data sources
├── variables.tf         # Input variables
├── outputs.tf           # Output values (VM IP, YDB endpoint, etc.)
├── ydb.tf               # YDB database resource
├── registry.tf          # Container registry
├── network.tf           # VPC network + subnet
├── iam.tf               # Service account + role bindings
├── compute.tf           # COI VM instance
├── docker-compose.yaml  # Container declaration for COI metadata
└── terraform.tfvars.example  # Example variable values
```

**Dockerfiles**:

```
Dockerfile.01_db_producer
Dockerfile.02_cdc_worker
Dockerfile.03_topic
```

Or a single parameterized `Dockerfile` with `--build-arg EXAMPLE=01_db_producer`.
