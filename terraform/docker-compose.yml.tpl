version: "3"
services:
  coordinator:
    image: ${coordinator_image}
    environment:
      YDB_ENDPOINT: ${ydb_endpoint}
      YDB_DATABASE: ${ydb_database}
    command:
      - "--mode"
      - "worker"
      - "--rate"
      - "${worker_rate}"
      - "--metrics-port"
      - "9090"
    ports:
      - "127.0.0.1:9090:9090"
    restart: unless-stopped

  unified-agent:
    image: cr.yandex/yc/unified-agent:latest
    network_mode: host
    volumes:
      - /proc:/ua_proc:ro
      - /etc/yandex-unified-agent/config.yml:/etc/yandex-unified-agent/config.yml:ro
    restart: unless-stopped
