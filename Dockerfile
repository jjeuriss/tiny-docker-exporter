# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod .
COPY main.go .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o exporter .

# Extract docker CLI from official docker image
FROM docker:latest AS docker-extractor
# Docker CLI is already in the official docker image

# Runtime stage - Alpine base for minimal size
FROM alpine:3.19

# Copy docker CLI from docker image
COPY --from=docker-extractor /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker-extractor /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app/exporter /usr/local/bin/exporter

EXPOSE 8010

ENTRYPOINT ["/usr/local/bin/exporter"]
CMD ["8010"]
