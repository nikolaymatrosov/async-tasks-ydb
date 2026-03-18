# CDC Worker

A change data capture (CDC) worker that processes task events from a YDB changefeed topic and marks them as complete.

## Overview

This application consumes events from a YDB changefeed topic (`tasks/cdc_tasks`), processes them with simulated work, and updates task records to mark completion. It demonstrates an asynchronous task processing pattern using YDB's publish-subscribe capabilities.

## Features

- **Changefeed consumption**: Reads CDC events from a YDB topic in real-time
- **Event filtering**: Processes INSERT/UPDATE events, skips DELETE events
- **Idempotent processing**: Uses UPSERT operations for safe reprocessing on failures
- **At-least-once delivery**: Commits offsets after batch processing
- **Real-time metrics**: Reports processed/skipped/error counts every second
- **Graceful shutdown**: Responds to SIGTERM and Ctrl-C signals

## Requirements

- Go 1.20+
- YDB database instance with credentials
- YDB changefeed configured on the `tasks` table with NEW_IMAGE mode
- A `tasks` table with the following schema:

  ```sql
  CREATE TABLE tasks (
    id UUID PRIMARY KEY,
    payload Bytes,
    created_at Timestamp,
    done_at Timestamp?
  )
  ```

## Configuration

### Environment Variables

- `YDB_ENDPOINT`: YDB connection endpoint (required)
  - Example: `grpcs://ydb.example.com:2135`
- `YDB_SA_KEY_FILE`: Path to YDB service account key file (optional)
  - JSON file with authentication credentials
  - If not set, VM metadata credentials are used automatically

### Command-line Flags

- `-work-delay`: Simulated processing delay per message (default: 100ms)
- `-topic`: Topic path relative to the database root (default: `tasks/cdc_tasks`)
  - The full topic path is: `db.Name() + "/" + topic`
- `-consumer`: Topic consumer name for tracking offset (default: `cdc-worker`)

## Usage

```bash
export YDB_ENDPOINT="grpcs://ydb.example.com:2135"
export YDB_SA_KEY_FILE="/path/to/sa_key.json"  # optional, omit on VM

go run main.go -work-delay 100ms -topic tasks/cdc_tasks -consumer cdc-worker
```

Stop the worker with Ctrl-C. The application will finish processing the current batch and shut down gracefully.

## Output

The application logs statistics every second:

```text
[stats] processed=45 skipped=2 errors=0 rate=47.0 msg/s
```

Upon shutdown, final statistics are displayed:

```text
--- Final Stats ---
Processed (INSERTs) : 1000
Skipped (non-INSERT): 5
Errors              : 0
```

## Message Processing Flow

1. **Read batch**: Retrieves a batch of CDC messages from the topic
2. **Parse events**: Deserializes JSON messages into CDC and task row structures
3. **Filter**: Skips DELETE events (empty newImage) and invalid messages
4. **Process**: Simulates work with configurable delay
5. **Update database**: Marks tasks as done via UPSERT, preserving `created_at`
6. **Commit**: Commits the batch offset to the consumer group

## Idempotency

The application uses UPSERT with explicit `created_at` values to ensure idempotent processing. If a message is reprocessed after failure, the same row is updated with the same `done_at` timestamp.

## Dependencies

- `github.com/google/uuid`: UUID parsing
- `github.com/ydb-platform/ydb-go-sdk/v3`: YDB SDK
- `github.com/ydb-platform/ydb-go-yc`: YDB authentication with service accounts
