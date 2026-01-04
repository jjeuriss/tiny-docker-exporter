# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY main.go .

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -buildvcs=false -ldflags="-s -w" -o exporter .

# Runtime stage - Alpine base for minimal size
FROM alpine:3.19

# Aggressive GC to reduce memory footprint
ENV GOGC=20

COPY --from=builder /app/exporter /usr/local/bin/exporter

EXPOSE 8010

ENTRYPOINT ["/usr/local/bin/exporter"]
CMD ["8010"]
