# Contract: Parameterized Dockerfile

**Interface type**: Docker build interface
**Consumer**: Build script / Makefile / developer

## Build Arguments

| Arg | Required | Default | Description |
|-----|----------|---------|-------------|
| `EXAMPLE` | Yes | — | Name of the example directory (e.g., `01_db_producer`, `02_cdc_worker`, `03_topic`) |

## Build Command

```bash
docker build \
  --build-arg EXAMPLE=01_db_producer \
  -t cr.yandex/<registry_id>/01_db_producer:latest \
  .
```

## Image Characteristics

- **Base**: `gcr.io/distroless/static-debian12:nonroot`
- **User**: UID 65532 (nonroot)
- **Entrypoint**: `/app/<example_binary>` (vector form, no shell)
- **No shell**: Cannot `docker exec -it ... /bin/sh`
- **Size**: ~10–15 MB per image (Go binary + distroless base)

## Container Environment Variables

Each container expects these env vars at runtime (passed via Docker Compose on COI VM):

| Variable          | Required | Description                                                                                 |
|-------------------|----------|---------------------------------------------------------------------------------------------|
| `YDB_ENDPOINT`    | Yes      | Full gRPC connection string to YDB                                                          |
| `YDB_SA_KEY_FILE` | No       | Path to service account key file for local dev (if unset, VM metadata credentials are used) |

## Registry Push

```bash
# Authenticate to Yandex Container Registry
cat sa.json | docker login --username json_key --password-stdin cr.yandex

# Push image
docker push cr.yandex/<registry_id>/01_db_producer:latest
```
