# Quickstart: 04_coordinated_table (pkg/cmd layout)

## Build

```bash
# From repo root
go build -o /dev/null ./04_coordinated_table/cmd/producer/
go build -o /dev/null ./04_coordinated_table/cmd/worker/
```

## Run producer

```bash
export YDB_ENDPOINT="grpcs://your-endpoint:2135"
export YDB_DATABASE="/your/database"
export YDB_SA_KEY_FILE="/path/to/sa-key.json"

go run ./04_coordinated_table/cmd/producer/ \
  --rate 200 \
  --batch-window 100ms \
  --report-interval 5s \
  --metrics-port 9090
```

## Run worker (in a separate terminal)

```bash
export YDB_ENDPOINT="grpcs://your-endpoint:2135"
export YDB_DATABASE="/your/database"
export YDB_SA_KEY_FILE="/path/to/sa-key.json"

go run ./04_coordinated_table/cmd/worker/ \
  --lock-duration 5s \
  --backoff-min 50ms \
  --backoff-max 5s \
  --metrics-port 9091
```

## Prometheus metrics

```bash
# Producer metrics
curl http://localhost:9090/metrics

# Worker metrics
curl http://localhost:9091/metrics
```

## Verify separation

Passing a producer-only flag to the worker (or vice versa) must exit with an error:

```bash
go run ./04_coordinated_table/cmd/worker/ --rate 100
# Expected: flag provided but not defined: -rate
```
