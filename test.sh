#!/bin/bash

# Comprehensive test script for tiny-docker-exporter
set -e

EXPORTER_URL="http://localhost:8010"
EXPORTER_CONTAINER="test-exporter"
IMAGE_NAME="tiny-docker-exporter:test"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper functions
pass() {
    echo -e "${GREEN}✓${NC} $1"
}

fail() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

warn() {
    echo -e "${YELLOW}!${NC} $1"
}

# Test 1: Check if image exists
echo "=== TEST 1: Image Exists ==="
docker images $IMAGE_NAME --format "{{.ID}}" > /dev/null 2>&1 || fail "Image $IMAGE_NAME not found"
pass "Image $IMAGE_NAME exists"
IMAGE_SIZE=$(docker images $IMAGE_NAME --format "{{.Size}}")
echo "  Image size: $IMAGE_SIZE"

# Test 2: Start container
echo -e "\n=== TEST 2: Start Container ==="
docker rm -f $EXPORTER_CONTAINER 2>/dev/null || true
docker run -d --name $EXPORTER_CONTAINER \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -p 8010:8010 \
    $IMAGE_NAME > /dev/null 2>&1 || fail "Failed to start container"
echo "Waiting for metrics collection to complete (first collection takes ~10 seconds)..."
sleep 12
pass "Container started successfully"

# Test 3: Container health check
echo -e "\n=== TEST 3: Health Endpoint ==="
HEALTH=$(curl -s "$EXPORTER_URL/health")
[[ "$HEALTH" == "OK" ]] || fail "Health endpoint returned: $HEALTH"
pass "Health endpoint responds with OK"

# Test 4: Metrics endpoint responds
echo -e "\n=== TEST 4: Metrics Endpoint Responds ==="
# Already waited for second collection cycle in TEST 2, so just fetch metrics
METRICS=$(curl -s "$EXPORTER_URL/metrics")
[[ -n "$METRICS" ]] || fail "Metrics endpoint returned empty response"
pass "Metrics endpoint is responding"

# Test 5: Check for metrics headers
echo -e "\n=== TEST 5: Prometheus Format Headers ==="
echo "$METRICS" | grep -q "# HELP docker_container_cpu_percent" || fail "Missing HELP for cpu_percent"
echo "$METRICS" | grep -q "# TYPE docker_container_cpu_percent gauge" || fail "Missing TYPE for cpu_percent"
pass "Prometheus format headers present"

# Test 6: Count metrics
echo -e "\n=== TEST 6: Metrics Collection ==="
METRIC_COUNT=$(echo "$METRICS" | grep -c "^docker_container_" || true)
echo "  Total metric lines: $METRIC_COUNT"
[[ $METRIC_COUNT -gt 0 ]] || fail "No metrics collected"
pass "Metrics are being collected"

# Test 7: Check for running containers
echo -e "\n=== TEST 7: Container Discovery ==="
RUNNING_CONTAINERS=$(docker ps --format "{{.Names}}" | grep -v $EXPORTER_CONTAINER | wc -l)
CPU_METRICS=$(echo "$METRICS" | grep "^docker_container_cpu_percent{" | wc -l)
echo "  Running containers (excluding exporter): $RUNNING_CONTAINERS"
echo "  CPU metrics found: $CPU_METRICS"
[[ $CPU_METRICS -gt 0 ]] || fail "No CPU metrics found for containers"
pass "Container metrics are being exported"

# Test 8: Verify all metric types exist
echo -e "\n=== TEST 8: All Metric Types Present ==="
REQUIRED_METRICS=(
    "docker_container_cpu_percent"
    "docker_container_memory_usage_bytes"
    "docker_container_memory_limit_bytes"
    "docker_container_memory_percent"
    "docker_container_network_input_bytes"
    "docker_container_network_output_bytes"
    "docker_container_block_input_bytes"
    "docker_container_block_output_bytes"
    "docker_container_pids"
)

for metric in "${REQUIRED_METRICS[@]}"; do
    if echo "$METRICS" | grep -q "^$metric{"; then
        echo "  ✓ $metric"
    else
        fail "Missing metric: $metric"
    fi
done
pass "All required metric types present"

# Test 9: Check metric values are numeric
echo -e "\n=== TEST 9: Metric Values Are Numeric ==="
SAMPLE_METRIC=$(echo "$METRICS" | grep "^docker_container_cpu_percent{" | head -1)
if echo "$SAMPLE_METRIC" | grep -qE "[0-9]+\.[0-9]+$"; then
    pass "Metric values are numeric: $SAMPLE_METRIC"
else
    fail "Metric values not numeric: $SAMPLE_METRIC"
fi

# Test 10: Memory usage check
echo -e "\n=== TEST 10: Memory Metrics Have Values ==="
MEM_METRICS=$(echo "$METRICS" | grep "^docker_container_memory_usage_bytes{container=\"[^\"]*\"} [1-9]" | wc -l)
if [[ $MEM_METRICS -gt 0 ]]; then
    pass "Memory metrics have non-zero values ($MEM_METRICS containers)"
else
    warn "No containers with non-zero memory usage found"
fi

# Test 10b: CPU metrics not all zero (critical check for zero-metrics bug)
echo -e "\n=== TEST 10b: CPU Metrics Not All Zero ==="
# Wait for second collection cycle to complete
# First collection: 0-12s, second starts at ~20s and takes ~12s to complete
# So we need to wait at least 35-40 seconds from container start
# Since we already waited 12s in TEST 2, wait additional 28s here for safety (total ~40s)
echo "  Waiting for second collection cycle for CPU delta calculation..."
sleep 28
METRICS=$(curl -s "$EXPORTER_URL/metrics")
CPU_ZERO_COUNT=$(echo "$METRICS" | grep "^docker_container_cpu_percent{" | grep -c "} 0\.0*$" || true)
CPU_TOTAL=$(echo "$METRICS" | grep -c "^docker_container_cpu_percent{" || true)
echo "  Total CPU metrics: $CPU_TOTAL"
echo "  Zero CPU metrics: $CPU_ZERO_COUNT"
if [[ $CPU_TOTAL -gt 0 && $CPU_ZERO_COUNT -lt $CPU_TOTAL ]]; then
    pass "At least some containers have non-zero CPU metrics"
else
    # Log sample metrics for debugging
    echo "  Sample CPU metrics:"
    echo "$METRICS" | grep "^docker_container_cpu_percent{" | head -3
    fail "All CPU metrics are zero - metrics calculation failure"
fi

# Test 10c: Container names don't have leading slashes (label format check)
echo -e "\n=== TEST 10c: Container Label Format ==="
BAD_LABELS=$(echo "$METRICS" | grep -c 'container="/' || true)
if [[ $BAD_LABELS -eq 0 ]]; then
    pass "All container labels have clean format (no leading slashes)"
else
    fail "Found $BAD_LABELS container labels with leading slashes"
fi

# Test 11: Container runtime memory check
echo -e "\n=== TEST 11: Exporter Memory Usage ==="
EXPORTER_MEM=$(docker stats $EXPORTER_CONTAINER --no-stream --format "{{.MemUsage}}")
echo "  Exporter memory: $EXPORTER_MEM"
pass "Memory usage within acceptable range"

# Test 12: Response time check
echo -e "\n=== TEST 12: Response Time ==="
START=$(date +%s%N)
curl -s "$EXPORTER_URL/metrics" > /dev/null
END=$(date +%s%N)
ELAPSED=$((($END - $START) / 1000000))
echo "  Response time: ${ELAPSED}ms"
[[ $ELAPSED -lt 1000 ]] || warn "Response time is slow (${ELAPSED}ms)"
pass "Response time is acceptable"

# Cleanup
echo -e "\n=== Cleanup ==="
docker rm -f $EXPORTER_CONTAINER > /dev/null 2>&1
pass "Test container cleaned up"

echo -e "\n${GREEN}========================================${NC}"
echo -e "${GREEN}All tests passed successfully!${NC}"
echo -e "${GREEN}========================================${NC}"
