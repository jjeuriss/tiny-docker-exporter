package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Metrics holds parsed docker stats
type Metrics struct {
	mu                 sync.RWMutex
	containers         map[string]*ContainerMetrics
	lastCollectionTime time.Time
}

// ContainerMetrics holds metrics for a single container
type ContainerMetrics struct {
	Name       string
	CPUPercent float64
	MemUsage   float64
	MemLimit   float64
	MemPercent float64
	NetIn      float64
	NetOut     float64
	BlockIn    float64
	BlockOut   float64
	PIDs       float64
}

// DockerStatsJSON is the structure returned by docker stats --format json
type DockerStatsJSON struct {
	Container string `json:"Container"`
	Name      string `json:"Name"`
	CPUPerc   string `json:"CPUPerc"`
	MemUsage  string `json:"MemUsage"`
	MemLimit  string `json:"MemLimit"`
	MemPerc   string `json:"MemPerc"`
	NetIO     string `json:"NetIO"`
	BlockIO   string `json:"BlockIO"`
	PIDs      string `json:"PIDs"`
}

var metrics = &Metrics{
	containers: make(map[string]*ContainerMetrics),
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	port := ":8010"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}

	// Start background collector
	go collectMetrics()

	// Expose metrics endpoint
	http.HandleFunc("/metrics", metricsHandler)
	http.HandleFunc("/health", healthHandler)

	log.Printf("Starting Prometheus exporter on %s\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func collectMetrics() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		updateMetrics()
	}
}

func updateMetrics() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "json")
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		log.Printf("Error running docker stats: %v (stderr: %s)", err, stderr.String())
		metrics.mu.Lock()
		metrics.lastCollectionTime = time.Time{} // Clear last collection time on error
		metrics.mu.Unlock()
		return
	}

	newContainers := make(map[string]*ContainerMetrics)
	
	// Parse newline-delimited JSON
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		
		var stat DockerStatsJSON
		if err := json.Unmarshal(line, &stat); err != nil {
			log.Printf("Error parsing docker stats JSON: %v", err)
			continue
		}

		name := stat.Name
		cm := &ContainerMetrics{Name: name}

		cm.CPUPercent = parsePercent(stat.CPUPerc)
		
		// MemUsage format: "256MiB / 1GiB"
		memParts := strings.Split(stat.MemUsage, "/")
		if len(memParts) == 2 {
			cm.MemUsage = parseBytes(strings.TrimSpace(memParts[0]))
			cm.MemLimit = parseBytes(strings.TrimSpace(memParts[1]))
		}
		
		cm.MemPercent = parsePercent(stat.MemPerc)
		cm.NetIn, cm.NetOut = parseNetIO(stat.NetIO)
		cm.BlockIn, cm.BlockOut = parseBlockIO(stat.BlockIO)
		cm.PIDs = parseFloat(stat.PIDs)

		newContainers[name] = cm
	}

	metrics.mu.Lock()
	metrics.containers = newContainers
	metrics.mu.Unlock()
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	metrics.mu.RLock()
	defer metrics.mu.RUnlock()

	var output strings.Builder
	output.WriteString("# HELP docker_container_cpu_percent CPU percentage used by container\n")
	output.WriteString("# TYPE docker_container_cpu_percent gauge\n")
	for name, cm := range metrics.containers {
		output.WriteString(fmt.Sprintf("docker_container_cpu_percent{container=\"%s\"} %f\n", name, cm.CPUPercent))
	}

	output.WriteString("\n# HELP docker_container_memory_usage_bytes Memory usage in bytes\n")
	output.WriteString("# TYPE docker_container_memory_usage_bytes gauge\n")
	for name, cm := range metrics.containers {
		output.WriteString(fmt.Sprintf("docker_container_memory_usage_bytes{container=\"%s\"} %f\n", name, cm.MemUsage))
	}

	output.WriteString("\n# HELP docker_container_memory_limit_bytes Memory limit in bytes\n")
	output.WriteString("# TYPE docker_container_memory_limit_bytes gauge\n")
	for name, cm := range metrics.containers {
		output.WriteString(fmt.Sprintf("docker_container_memory_limit_bytes{container=\"%s\"} %f\n", name, cm.MemLimit))
	}

	output.WriteString("\n# HELP docker_container_memory_percent Memory percentage\n")
	output.WriteString("# TYPE docker_container_memory_percent gauge\n")
	for name, cm := range metrics.containers {
		output.WriteString(fmt.Sprintf("docker_container_memory_percent{container=\"%s\"} %f\n", name, cm.MemPercent))
	}

	output.WriteString("\n# HELP docker_container_network_input_bytes Network input in bytes\n")
	output.WriteString("# TYPE docker_container_network_input_bytes gauge\n")
	for name, cm := range metrics.containers {
		output.WriteString(fmt.Sprintf("docker_container_network_input_bytes{container=\"%s\"} %f\n", name, cm.NetIn))
	}

	output.WriteString("\n# HELP docker_container_network_output_bytes Network output in bytes\n")
	output.WriteString("# TYPE docker_container_network_output_bytes gauge\n")
	for name, cm := range metrics.containers {
		output.WriteString(fmt.Sprintf("docker_container_network_output_bytes{container=\"%s\"} %f\n", name, cm.NetOut))
	}

	output.WriteString("\n# HELP docker_container_block_input_bytes Block input in bytes\n")
	output.WriteString("# TYPE docker_container_block_input_bytes gauge\n")
	for name, cm := range metrics.containers {
		output.WriteString(fmt.Sprintf("docker_container_block_input_bytes{container=\"%s\"} %f\n", name, cm.BlockIn))
	}

	output.WriteString("\n# HELP docker_container_block_output_bytes Block output in bytes\n")
	output.WriteString("# TYPE docker_container_block_output_bytes gauge\n")
	for name, cm := range metrics.containers {
		output.WriteString(fmt.Sprintf("docker_container_block_output_bytes{container=\"%s\"} %f\n", name, cm.BlockOut))
	}

	output.WriteString("\n# HELP docker_container_pids Number of processes\n")
	output.WriteString("# TYPE docker_container_pids gauge\n")
	for name, cm := range metrics.containers {
		output.WriteString(fmt.Sprintf("docker_container_pids{container=\"%s\"} %f\n", name, cm.PIDs))
	}

	w.Write([]byte(output.String()))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	metrics.mu.RLock()
	hasMetrics := len(metrics.containers) > 0
	lastCollection := metrics.lastCollectionTime
	metrics.mu.RUnlock()
	
	if !hasMetrics || lastCollection.IsZero() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("No metrics collected"))
		return
	}
	
	// Warn if collection is stale (older than 10 seconds)
	if time.Since(lastCollection) > 10*time.Second {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Metrics stale"))
		return
	}
	
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// parsePercent extracts percentage from strings like "12.34%"
func parsePercent(s string) float64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return 0
}

// parseBytes converts byte strings like "256MiB", "1.5GB", "512kB" to bytes
func parseBytes(s string) float64 {
	s = strings.TrimSpace(s)
	
	units := map[string]float64{
		"b":   1,
		"kb":  1000,
		"mb":  1000 * 1000,
		"gb":  1000 * 1000 * 1000,
		"tb":  1000 * 1000 * 1000 * 1000,
		"kib": 1024,
		"mib": 1024 * 1024,
		"gib": 1024 * 1024 * 1024,
		"tib": 1024 * 1024 * 1024 * 1024,
	}

	// Find where the numeric part ends
	endIdx := 0
	for i, ch := range s {
		if (ch >= '0' && ch <= '9') || ch == '.' {
			endIdx = i + 1
		} else {
			break
		}
	}

	if endIdx == 0 {
		return 0
	}

	num, err := strconv.ParseFloat(s[:endIdx], 64)
	if err != nil {
		return 0
	}

	unit := strings.ToLower(strings.TrimSpace(s[endIdx:]))
	if multiplier, ok := units[unit]; ok {
		return num * multiplier
	}

	return 0
}

// parseNetIO parses "123.5MB / 456.7MB" format
func parseNetIO(s string) (in, out float64) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	in = parseBytes(strings.TrimSpace(parts[0]))
	out = parseBytes(strings.TrimSpace(parts[1]))
	return
}

// parseBlockIO parses "123.5MB / 456.7MB" format
func parseBlockIO(s string) (in, out float64) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	in = parseBytes(strings.TrimSpace(parts[0]))
	out = parseBytes(strings.TrimSpace(parts[1]))
	return
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
