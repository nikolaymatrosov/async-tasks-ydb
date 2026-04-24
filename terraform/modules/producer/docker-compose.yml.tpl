version: "3"
services:
  coordinator:
    image: ${coordinator_image}
    environment:
      YDB_ENDPOINT: ${ydb_endpoint}
      YDB_DATABASE: ${ydb_database}
      APIGW_URL: ${apigw_url}
    command:
      - "--rate"
      - "${producer_rate}"
      - "--metrics-port"
      - "9090"
    ports:
      - "127.0.0.1:9090:9090"
    restart: unless-stopped
  unified-agent:
    image: cr.yandex/yc/unified-agent:latest
    network_mode: host
    entrypoint: ""
    environment:
      PROC_DIRECTORY: /ua_proc
      FOLDER_ID: ${folder_id}
    volumes:
      - /proc:/ua_proc:ro
      - /home/yc-user/ua-config.yml:/etc/yandex/unified_agent/config.yml
    restart: unless-stopped
