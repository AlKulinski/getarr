# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o getarr ./cmd/getarr

# Runtime stage
FROM alpine:3.19
RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /src/getarr /app/getarr

EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/app/getarr"]
CMD ["-data", "/data"]
