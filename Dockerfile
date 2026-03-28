# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS builder

ARG EXAMPLE

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/binary ./${EXAMPLE}/

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /app/binary /app/binary
ENTRYPOINT ["/app/binary"]
