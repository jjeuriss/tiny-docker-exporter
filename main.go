package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Metrics holds parsed docker stats
type Metrics struct {
	mu                 sync.RWMutex
	containers         map[string]*ContainerMetrics
	previousStats      map[string]*types.StatsJSON // Cache previous stats for CPU delta calculation
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

var metrics = &Metrics{
	containers:    make(map[string]*ContainerMetrics, 32),
	previousStats: make(map[string]*types.StatsJSON, 32),
}

func init() {
	// Aggressive garbage collection to reduce memory footprint
	debug.SetGCPercent(20)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	port := ":8010"
	scrapeInterval := 10 * time.Second
	
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}
	
	if len(os.Args) > 2 {
		if interval, err := strconv.Atoi(os.Args[2]); err == nil && interval > 0 {
			scrapeInterval = time.Duration(interval) * time.Second
		}
	}

	// Start background collector
	go collectMetrics(scrapeInterval)

	// Expose metrics endpoint
	http.HandleFunc("/metrics", metricsHandler)
	http.HandleFunc("/health", healthHandler)

	log.Printf("Starting Prometheus exporter on %s with %d second scrape interval\n", port, int(scrapeInterval.Seconds()))
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func collectMetrics(interval time.Duration) {
	// Initial collection
	updateMetrics()
	
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		updateMetrics()
		// Hint to GC to free memory after collection
		runtime.GC()
	}
}

func updateMetrics() {
	log.Printf("Starting metrics collection cycle...")
	ctx, cancel := context.WithTimeout(context.Background(), 210*time.Second)
	defer cancel()
	
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("Error creating Docker client: %v", err)
		metrics.mu.Lock()
		metrics.lastCollectionTime = time.Time{}
		metrics.mu.Unlock()
		return
	}
	defer cli.Close()

	// List all containers
	containers, err := cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		log.Printf("Error listing containers: %v", err)
		metrics.mu.Lock()
		metrics.lastCollectionTime = time.Time{}
		metrics.mu.Unlock()
		return
	}

	newContainers := make(map[string]*ContainerMetrics, len(containers))
	
	// Use a channel to collect results from workers
	type statsResult struct {
		name   string
		metric *ContainerMetrics
	}
	
	// Worker pool for parallel stats collection
	// Use fewer workers with longer timeouts to avoid overwhelming the daemon
	numWorkers := 4
	if len(containers) < 4 {
		numWorkers = len(containers)
	}
	
	resultChan := make(chan statsResult, len(containers))
	containerChan := make(chan types.Container, len(containers))
	
	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cont := range containerChan {
				// Create a fresh client for each worker to avoid sharing state
				workerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
				if err != nil {
					log.Printf("Error creating worker client: %v", err)
					continue
				}
				
				// Respect parent context deadline to avoid timeout bypass
				// Set per-container timeout but don't exceed overall deadline
				containerCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
				stats, err := workerCli.ContainerStats(containerCtx, cont.ID, false)
				cancel()
				
				if err != nil {
					log.Printf("Error getting stats for container %s: %v", cont.Names[0], err)
					workerCli.Close()
					continue
				}

				// Parse stats from stream
				var statData types.StatsJSON
				if err := json.NewDecoder(stats.Body).Decode(&statData); err != nil {
					stats.Body.Close()
					log.Printf("Error parsing stats for container %s: %v", cont.Names[0], err)
					workerCli.Close()
					continue
				}
				stats.Body.Close()

				// Clean container name by removing leading slash
				cleanName := cont.Names[0]
				if len(cleanName) > 0 && cleanName[0] == '/' {
					cleanName = cleanName[1:]
				}

				cm := &ContainerMetrics{
					Name:      cleanName,
					MemUsage:  float64(statData.MemoryStats.Usage),
					MemLimit:  float64(statData.MemoryStats.Limit),
					NetIn:     calculateNetIn(&statData),
					NetOut:    calculateNetOut(&statData),
					BlockIn:   calculateBlockIn(&statData),
					BlockOut:  calculateBlockOut(&statData),
					PIDs:      float64(statData.PidsStats.Current),
				}

				// Calculate CPU percentage using cached previous stats (thread-safe)
				metrics.mu.RLock()
				cm.CPUPercent = calculateCPUPercent(&statData, cont.ID)
				metrics.mu.RUnlock()

				// Calculate memory percentage
				if cm.MemLimit > 0 {
					cm.MemPercent = (cm.MemUsage / cm.MemLimit) * 100
				}

				resultChan <- statsResult{name: cleanName, metric: cm}
				
				// Cache current stats for next collection cycle (thread-safe)
				statsCopy := statData // Shallow copy
				metrics.mu.Lock()
				metrics.previousStats[cont.ID] = &statsCopy
				metrics.mu.Unlock()
				
				workerCli.Close()
			}
		}()
	}
	
	// Send containers to workers (include sender goroutine in waitgroup to prevent leak)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, cont := range containers {
			containerChan <- cont
		}
		close(containerChan)
	}()
	
	// Wait for workers to finish
	wg.Wait()
	close(resultChan)
	
	// Collect results
	for result := range resultChan {
		newContainers[result.name] = result.metric
	}

	log.Printf("Successfully collected stats from %d containers", len(newContainers))
	if len(newContainers) == 0 {
		log.Printf("WARNING: No container stats collected!")
	}

	metrics.mu.Lock()
	metrics.containers = newContainers
	metrics.lastCollectionTime = time.Now()
	metrics.mu.Unlock()
}

func calculateCPUPercent(stat *types.StatsJSON, containerID string) float64 {
	cpuPercent := 0.0
	
	// Use cached previous stats for meaningful delta calculation
	prevStat, hasPrev := metrics.previousStats[containerID]
	if !hasPrev {
		// No previous stats yet, return 0
		return 0.0
	}
	
	cpuDelta := float64(stat.CPUStats.CPUUsage.TotalUsage - prevStat.CPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stat.CPUStats.SystemUsage - prevStat.CPUStats.SystemUsage)
	
	if systemDelta > 0.0 && cpuDelta >= 0.0 {
		// On some systems (particularly ARM), OnlineCPUs and PercpuUsage may be empty.
		// Use the number of CPUs from OnlineCPUs, fallback to the length of PercpuUsage.
		// If both are unavailable, the system likely has 1 CPU or stats aren't being reported.
		numCPUs := stat.CPUStats.OnlineCPUs
		if numCPUs == 0 {
			numCPUs = uint32(len(stat.CPUStats.CPUUsage.PercpuUsage))
		}
		if numCPUs == 0 {
			numCPUs = 1 // Fallback: assume 1 CPU
		}
		cpuPercent = (cpuDelta / systemDelta) * float64(numCPUs) * 100.0
	}
	
	return cpuPercent
}

func calculateNetIn(stat *types.StatsJSON) float64 {
	var sum uint64
	for _, net := range stat.Networks {
		sum += net.RxBytes
	}
	return float64(sum)
}

func calculateNetOut(stat *types.StatsJSON) float64 {
	var sum uint64
	for _, net := range stat.Networks {
		sum += net.TxBytes
	}
	return float64(sum)
}

func calculateBlockIn(stat *types.StatsJSON) float64 {
	if len(stat.BlkioStats.IoServiceBytesRecursive) == 0 {
		return 0
	}
	return float64(stat.BlkioStats.IoServiceBytesRecursive[0].Value)
}

func calculateBlockOut(stat *types.StatsJSON) float64 {
	if len(stat.BlkioStats.IoServiceBytesRecursive) < 2 {
		return 0
	}
	return float64(stat.BlkioStats.IoServiceBytesRecursive[1].Value)
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	metrics.mu.RLock()
	defer metrics.mu.RUnlock()

	// Single pass - write directly to response without intermediate buffer
	w.Write([]byte("# HELP docker_container_cpu_percent CPU percentage used by container\n# TYPE docker_container_cpu_percent gauge\n"))
	for name, cm := range metrics.containers {
		fmt.Fprintf(w, "docker_container_cpu_percent{container=\"%s\"} %f\n", name, cm.CPUPercent)
	}

	w.Write([]byte("\n# HELP docker_container_memory_usage_bytes Memory usage in bytes\n# TYPE docker_container_memory_usage_bytes gauge\n"))
	for name, cm := range metrics.containers {
		fmt.Fprintf(w, "docker_container_memory_usage_bytes{container=\"%s\"} %f\n", name, cm.MemUsage)
	}

	w.Write([]byte("# HELP docker_container_memory_limit_bytes Memory limit in bytes\n# TYPE docker_container_memory_limit_bytes gauge\n"))
	for name, cm := range metrics.containers {
		fmt.Fprintf(w, "docker_container_memory_limit_bytes{container=\"%s\"} %f\n", name, cm.MemLimit)
	}

	w.Write([]byte("# HELP docker_container_memory_percent Memory percentage\n# TYPE docker_container_memory_percent gauge\n"))
	for name, cm := range metrics.containers {
		fmt.Fprintf(w, "docker_container_memory_percent{container=\"%s\"} %f\n", name, cm.MemPercent)
	}

	w.Write([]byte("# HELP docker_container_network_input_bytes Network input in bytes\n# TYPE docker_container_network_input_bytes gauge\n"))
	for name, cm := range metrics.containers {
		fmt.Fprintf(w, "docker_container_network_input_bytes{container=\"%s\"} %f\n", name, cm.NetIn)
	}

	w.Write([]byte("# HELP docker_container_network_output_bytes Network output in bytes\n# TYPE docker_container_network_output_bytes gauge\n"))
	for name, cm := range metrics.containers {
		fmt.Fprintf(w, "docker_container_network_output_bytes{container=\"%s\"} %f\n", name, cm.NetOut)
	}

	w.Write([]byte("# HELP docker_container_block_input_bytes Block input in bytes\n# TYPE docker_container_block_input_bytes gauge\n"))
	for name, cm := range metrics.containers {
		fmt.Fprintf(w, "docker_container_block_input_bytes{container=\"%s\"} %f\n", name, cm.BlockIn)
	}

	w.Write([]byte("# HELP docker_container_block_output_bytes Block output in bytes\n# TYPE docker_container_block_output_bytes gauge\n"))
	for name, cm := range metrics.containers {
		fmt.Fprintf(w, "docker_container_block_output_bytes{container=\"%s\"} %f\n", name, cm.BlockOut)
	}

	w.Write([]byte("# HELP docker_container_pids Number of processes\n# TYPE docker_container_pids gauge\n"))
	for name, cm := range metrics.containers {
		fmt.Fprintf(w, "docker_container_pids{container=\"%s\"} %f\n", name, cm.PIDs)
	}
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
