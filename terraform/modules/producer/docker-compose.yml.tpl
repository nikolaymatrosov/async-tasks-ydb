version: "3"
services:
  coordinator:
    image: ${coordinator_image}
    environment:
      YDB_ENDPOINT: ${ydb_endpoint}
      YDB_DATABASE: ${ydb_database}
    command:
      - "--rate"
      - "${producer_rate}"
    restart: unless-stopped
