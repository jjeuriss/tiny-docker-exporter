# Grafana Dashboard Setup Guide

This guide explains how to import the tiny-docker-exporter dashboard into Grafana and configure Prometheus to scrape metrics.

![Grafana Dashboard Preview](./Grafana-dashboard.png)

## Contents

1. [Prerequisites](#prerequisites)
2. [Prometheus Configuration](#prometheus-configuration)
3. [Grafana Dashboard Import](#grafana-dashboard-import)
4. [Dashboard Features](#dashboard-features)
5. [Troubleshooting](#troubleshooting)

## Prerequisites

Before setting up the dashboard, ensure you have:

- **Prometheus**: Running and configured to scrape metrics
- **Grafana**: Version 8.0 or higher
- **tiny-docker-exporter**: Running on port 8010 (or your configured port)
- Network connectivity between Prometheus and the exporter

## Prometheus Configuration

### 1. Update Your Prometheus Configuration

Add the following scrape job to your `prometheus.yml`:

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  # ... existing configs ...

  - job_name: 'docker-exporter'
    scrape_interval: 10s
    scrape_timeout: 5s
    static_configs:
      - targets: ['localhost:8010']
        # Use the actual hostname or IP if Prometheus is on a different machine
        # Example: ['192.168.1.150:8010']
```

### 2. Reload Prometheus

Restart Prometheus to apply the new configuration:

```bash
# If running via Docker
docker restart prometheus-container-name

# If running via systemd
sudo systemctl restart prometheus

# If running as a standalone process (send SIGHUP)
kill -HUP $(pidof prometheus)
```

### 3. Verify Scraping

Visit Prometheus web interface and check:

1. Go to: `http://localhost:9090/targets`
2. Look for the `docker-exporter` job
3. Status should show "UP" in green

If you see "DOWN" or errors, check:

- The exporter is running: `docker ps | grep tiny-docker-exporter`
- The exporter is healthy: `curl http://localhost:8010/health`
- Network connectivity: `curl http://localhost:8010/metrics`
- Firewall rules allow communication between Prometheus and exporter

## Grafana Dashboard Import

### Method 1: Import from JSON File (Recommended)

1. **Access Grafana**
   - Open: `http://localhost:3000` (default)
   - Login with admin credentials

2. **Navigate to Dashboard Import**
   - Click the "+" menu in the left sidebar
   - Select "Import"
   - Or: Go to Dashboards → Manage → Import

3. **Upload the Dashboard**
   - Click "Upload JSON file"
   - Select: `grafana-dashboard.json` from the tiny-docker-exporter repository
   - Or paste the JSON content directly into the text area

4. **Configure the Dashboard**
   - Dashboard Name: `Docker Container Metrics` (or your preferred name)
   - Folder: Choose or create a folder (e.g., "Docker", "Infrastructure")
   - Select your Prometheus datasource
   - Click "Import"

### Method 2: Manual Dashboard Creation

If you prefer to create the dashboard manually:

1. Create a new dashboard in Grafana
2. Add panels with the following queries:
   - **CPU Usage**: `docker_container_cpu_percent{container=~"$container"}`
   - **Memory Usage**: `docker_container_memory_usage_bytes{container=~"$container"}`
   - **Network RX**: `rate(docker_container_network_receive_bytes_total{container=~"$container"}[1m])`
   - **Network TX**: `rate(docker_container_network_transmit_bytes_total{container=~"$container"}[1m])`
   - **Block I/O Read**: `rate(docker_container_block_io_read_bytes_total{container=~"$container"}[1m])`
   - **Block I/O Write**: `rate(docker_container_block_io_write_bytes_total{container=~"$container"}[1m])`
   - **Process Count**: `docker_container_pids{container=~"$container"}`

## Dashboard Features

### Panels Overview

1. **Container Alive Status** (State Timeline)
   - Visual timeline showing when containers are running
   - Displays status for all monitored containers

2. **Running Containers** (Stat Panel)
   - Shows total count of active containers being monitored
   - Updates in real-time as containers start/stop

3. **Last $range CPU Usage** (Pie Chart)
   - Average CPU usage across selected containers
   - Broken down by container for easy comparison
   - Time range selector available

4. **Last $range Memory Usage** (Pie Chart)
   - Average memory consumption across selected containers
   - Helps identify memory-intensive containers
   - Shows both absolute values and percentages

5. **CPU Usage Percent** (Time Series)
   - Historical CPU usage over time
   - Shows max and mean statistics in legend
   - Useful for identifying CPU spikes

6. **Memory Usage** (Time Series)
   - Historical memory consumption over time
   - Displays max, mean, and last values
   - Helps track memory growth trends

7. **Network Read / Write Bytes** (Time Series)
   - Incoming (RX) and outgoing (TX) network traffic
   - Shows rates (bytes per second)
   - TX values shown as negative for visual separation

8. **Storage Read / Write Bytes** (Time Series)
   - Block I/O read and write operations
   - Shows disk activity rates
   - Write values shown as negative for visual separation

9. **Process Count (PIDs)** (Time Series)
   - Number of processes running in each container
   - Helps identify resource leaks (increasing PIDs)
   - Across all time ranges

### Variables

The dashboard includes two template variables:

1. **Container** (Multi-select)
   - Choose which containers to display
   - Default: All containers
   - Use regex patterns to filter specific containers

2. **Time Range** (Interval)
   - Aggregate time windows for calculations
   - Options: 1m, 5m, 10m, 30m, 1h, 6h, 12h, 1d
   - Auto mode calculates optimal range based on dashboard time range

## Using the Dashboard

### Container Selection

1. Click the "Container" dropdown at the top
2. Select specific containers or keep "All" selected
3. Dashboard updates automatically

### Time Range Adjustment

1. Use the time picker at the top right
2. Select predefined ranges (Last 1h, Last 6h, etc.) or custom range
3. All panels refresh with new data

### Exporting Metrics

To export metrics for external analysis:

1. Click a chart's menu (three dots)
2. Select "Inspect" → "Data"
3. Choose export format (CSV, JSON)

## Troubleshooting

### "No Data" in All Panels

**Symptoms**: All dashboard panels appear blank or show "No data" message

**Quick Diagnostics**:

```bash
# 1. Verify exporter is running and healthy
curl http://192.168.1.150:8010/health

# 2. Verify Prometheus is scraping
curl "http://192.168.1.150:9090/api/v1/query?query=docker_container_cpu_percent" | jq '.data.result | length'
# Should return your container count (e.g., 21)

# 3. Check Prometheus targets
curl http://192.168.1.150:9090/api/v1/targets | jq '.data.activeTargets[] | select(.job=="tiny-docker-exporter")'
# Status should be "UP" (green)
```

**Fix Steps**:

1. **Check Grafana datasource**:
   - Configuration → Data Sources → Prometheus
   - Click "Save & Test" button
   - Should show "Data source is working"

2. **Re-import the dashboard**:
   - Dashboards → Manage
   - Delete "Docker Container Metrics" (if exists)
   - Click "+" → "Import"
   - Upload `grafana-dashboard.json`
   - Select "Prometheus" as datasource
   - Click "Import"

3. **Hard refresh Grafana**:
   - Press `Ctrl+Shift+R` to clear browser cache
   - Wait 10-15 seconds for fresh data

4. **If still no data, restart services**:
   ```bash
   docker compose -f /opt/docker-compose.yaml restart tiny-docker-exporter prometheus
   # Wait 30 seconds then refresh Grafana
   ```

### Query Errors ("Invalid Label" etc.)

**Symptoms**: Panel shows "Query error" in red text

**Cause**: Incorrect metric or label names

**Solution**:
- tiny-docker-exporter uses label: `container` (not `container_name`)
- Correct metric names:
  - `docker_container_cpu_percent`
  - `docker_container_memory_usage_bytes`, `docker_container_memory_limit_bytes`, `docker_container_memory_percent`
  - `docker_container_network_input_bytes`, `docker_container_network_output_bytes`
  - `docker_container_block_input_bytes`, `docker_container_block_output_bytes`
  - `docker_container_pids`
- To fix:
  1. Click the affected panel
  2. Click "Edit" → "Queries" tab
  3. Verify metric and label names match the list above
  4. Test query in Prometheus UI: http://192.168.1.150:9090 → "Graph" tab

### Stale or Missing Data

**Symptoms**: Dashboard doesn't update, shows data from hours ago

**Solutions**:
1. Hard refresh Grafana: `Ctrl+Shift+R`
2. Verify exporter is running: `docker ps | grep tiny-docker-exporter`
3. Check Prometheus scrape interval: `curl http://192.168.1.150:9090/targets`
4. Restart if needed:
   ```bash
   docker compose -f /opt/docker-compose.yaml restart tiny-docker-exporter prometheus
   ```

### Dashboard Features

Once working, you'll see 8 panels (shared tooltip on hover):

1. **Running Containers** - Count of active containers
2. **Last $range CPU Usage** - Average CPU per container (pie chart)
3. **Last $range Memory Usage** - Average memory per container (pie chart)
4. **CPU Usage Percent** - Real-time CPU over time (timeseries)
5. **Memory Usage** - Real-time memory in bytes over time (timeseries)
6. **Network Read/Write Bytes** - Network I/O per container (timeseries)
7. **Storage Read/Write Bytes** - Disk I/O per container (timeseries)
8. **Process Count PIDs** - Number of processes per container (timeseries)

All panels support filtering by container name using the "Container" dropdown variable at the top.

**Hover Feature**: Hover over any graph to see a shared red vertical line across all panels for easier debugging and correlation.

## Advanced Configuration

### Custom Refresh Intervals

Edit the dashboard JSON to change refresh rates:

```json
"refresh": "10s"  // Change from "10s" to desired interval
```

Common values:
- `"5s"` - Real-time (high load)
- `"10s"` - Balanced (default)
- `"30s"` - Low load
- `"1m"` - Very low load

### Alert Rules (Optional)

Create Prometheus alerts based on container metrics:

```yaml
groups:
  - name: docker_containers
    rules:
      - alert: HighCPUUsage
        expr: docker_container_cpu_percent > 80
        for: 5m
        annotations:
          summary: "High CPU usage detected"

      - alert: HighMemoryUsage
        expr: docker_container_memory_usage_bytes / docker_container_memory_limit_bytes > 0.9
        for: 5m
        annotations:
          summary: "High memory usage detected"
```

## Support

For issues or questions:

1. Check the [tiny-docker-exporter README](./README.md)
2. Review Prometheus targets: `http://192.168.1.150:9090/targets`
3. Inspect raw metrics: `http://192.168.1.150:8010/metrics`
4. Check container health: `curl http://192.168.1.150:8010/health`

---

**Last Updated**: 2024
**Dashboard Version**: 1.1
**Grafana Compatibility**: 8.0+
**Prometheus Compatibility**: 2.0+
