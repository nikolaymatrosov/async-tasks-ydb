FROM golang:1.26-alpine AS builder

ARG EXAMPLE

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/binary ./${EXAMPLE}/

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /app/binary /app/binary
ENTRYPOINT ["/app/binary"]
