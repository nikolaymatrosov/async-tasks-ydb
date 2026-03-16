# DB Producer

A high-performance task producer application that inserts records into a YDB database.

## Overview

This application generates random task records and inserts them into a YDB database table (`tasks`) at high throughput. It's designed to stress-test and measure the performance of database writes with configurable parallelism.

## Features

- **Concurrent writes**: Uses a worker pool to execute multiple concurrent INSERT operations
- **Random payload generation**: Creates random binary payloads for realistic data size testing
- **Performance metrics**: Tracks and reports rows/second, throughput (Mbps), and average insert latency
- **Graceful shutdown**: Responds to SIGTERM and Ctrl-C signals

## Requirements

- Go 1.20+
- YDB database instance with credentials
- Access to a `tasks` table with the following schema:

  ```sql
  CREATE TABLE tasks (
    id UUID PRIMARY KEY,
    payload Bytes,
    created_at Timestamp
  )
  ```

## Configuration

### Environment Variables

- `YDB_ENDPOINT`: YDB connection endpoint (required)
  - Example: `grpcs://ydb.example.com:2135`
- `YDB_SA_KEY_FILE`: Path to YDB service account key file (required)
  - JSON file with authentication credentials

### Command-line Flags

- `-payload-size`: Size of random payload per row in bytes (default: 1024)
- `-parallelism`: Number of concurrent worker goroutines (default: 10)

## Usage

```bash
export YDB_ENDPOINT="grpcs://ydb.example.com:2135"
export YDB_SA_KEY_FILE="/path/to/sa_key.json"

go run main.go -payload-size 1024 -parallelism 10
```

Stop the producer with Ctrl-C. The application will wait for in-flight operations to complete and display final statistics.

## Output

The application prints statistics upon shutdown:

```text
--- Stats ---
Rows inserted : 10000
Elapsed       : 45.32 s
Rows/sec      : 220.61
Throughput    : 1.8052 Mbps
Avg insert    : 4.55 ms
```

## Dependencies

- `github.com/alitto/pond/v2`: Worker pool implementation
- `github.com/google/uuid`: UUID generation
- `github.com/ydb-platform/ydb-go-sdk/v3`: YDB SDK
- `github.com/ydb-platform/ydb-go-yc`: YDB authentication with service accounts
