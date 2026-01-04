# Tiny Docker Exporter

An extremely lightweight Prometheus metrics exporter for Docker container statistics, written in Go.

## Features

- **Lightweight**: 15.2MB Docker image, 6-9MB memory usage at runtime
- **Fast**: <20ms response time for metrics requests
- **Comprehensive**: CPU, memory, network I/O, block I/O, and process counts per container
- **Prometheus-compatible**: Standard text format, works with Prometheus, Grafana, and other tools
- **Configurable**: Adjustable scrape interval (default 10s) and port for flexible deployments

## Metrics Exposed

| Metric | Description |
|--------|-------------|
| `docker_container_cpu_percent` | CPU percentage |
| `docker_container_memory_usage_bytes` | Current memory usage |
| `docker_container_memory_limit_bytes` | Memory limit |
| `docker_container_memory_percent` | Memory percentage |
| `docker_container_network_input_bytes` | Network input bytes |
| `docker_container_network_output_bytes` | Network output bytes |
| `docker_container_block_input_bytes` | Block device input bytes |
| `docker_container_block_output_bytes` | Block device output bytes |
| `docker_container_pids` | Number of processes |

## Grafana Dashboard

A pre-built Grafana dashboard is included for visualizing Docker container metrics. See [GRAFANA_SETUP.md](./GRAFANA_SETUP.md) for complete setup instructions.

![Grafana Dashboard Preview](./Grafana-dashboard.png)

**Dashboard features**:
- Running container count (stat panel)
- CPU usage pie charts (last $range average) and time series (real-time)
- Memory usage pie charts (last $range average) and time series (real-time)
- Network I/O graphs (input/output bytes with rate analysis)
- Block I/O graphs (input/output bytes with rate analysis)
- Process count trends (PIDs over time)
- Multi-container filtering via dropdown variable
- Adjustable time range analysis ($range variable)
- Shared tooltip on hover across all panels for correlation

## Running

### Add to Existing Prometheus/Grafana (Recommended)

If you already have Prometheus and Grafana running, add this service to your `docker-compose.yml`:

```yaml
services:
  tiny-docker-exporter:
    image: ghcr.io/jjeuriss/tiny-docker-exporter:latest
    container_name: tiny-docker-exporter
    restart: unless-stopped
    ports:
      - "8010:8010"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    mem_limit: 100m
```

Then add to your Prometheus `scrape_configs`:

```yaml
scrape_configs:
  - job_name: 'tiny-docker-exporter'
    static_configs:
      - targets: ['localhost:8010']
    scrape_interval: 10s
    scrape_timeout: 5s
```

> **Assumes**: Prometheus and Grafana are already installed and configured in your environment.

### Complete Stack (New Installation)

If you don't have Prometheus and Grafana yet, use the included `docker-compose.yml`:

```bash
git clone https://github.com/jjeuriss/tiny-docker-exporter.git
cd tiny-docker-exporter
docker-compose up -d
```

This includes:
- **tiny-docker-exporter** on port 8010
- **Prometheus** on port 9090
- **Grafana** on port 3000 (admin/admin)

### Docker Run

For manual deployment without docker-compose:

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -p 8010:8010 \
  ghcr.io/jjeuriss/tiny-docker-exporter:latest
```

Metrics are exposed at `http://localhost:8010/metrics`.


### Configuration

The exporter accepts two optional arguments:

1. **Port** (default: `8010`) - The port to listen on
2. **Scrape interval** (default: `10`) - Collection interval in seconds

```bash
# Run on port 9999 with 10 second scrape interval
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -p 9999:9999 \
  tiny-docker-exporter:latest 9999 10

# Run on port 8010 with 30 second scrape interval  
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -p 8010:8010 \
  tiny-docker-exporter:latest 8010 30
```

## Prometheus Configuration

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'tiny-docker-exporter'
    static_configs:
      - targets: ['localhost:8010']
```

## Health Check

A health endpoint is available at `http://localhost:8010/health`.


## Building from Source

```bash
git clone https://github.com/jjeuriss/tiny-docker-exporter.git
cd tiny-docker-exporter
docker build -t tiny-docker-exporter:latest .
```


## Building

```bash
docker build -t tiny-docker-exporter:latest .
```

## Testing

A comprehensive test suite is included to verify the exporter is working correctly:

```bash
# Run tests
./test.sh
```

The test suite verifies:
- Docker image exists and is accessible
- Container starts and runs successfully
- Health endpoint responds correctly
- Metrics endpoint returns properly formatted Prometheus metrics
- All 9 metric types are present
- Metric values are numeric and valid
- Memory usage is within acceptable range
- Response time is acceptable
- All running containers are discovered and monitored


## Contributing

Contributions are welcome! Please feel free to submit PRs or open issues.

## License

MIT
