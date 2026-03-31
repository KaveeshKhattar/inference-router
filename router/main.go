package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"net"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// --- Prometheus Metrics ---

var (
	routedRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "router_requests_total",
			Help: "Total routed requests",
		},
		[]string{"replica"}, // This allows us to see load distribution
	)

	routingLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "router_decision_seconds",
			Help:    "Time taken to compute routing decision",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1}, // Custom buckets for ms-level precision
		},
	)
)

func init() {
	// Metrics must be registered before use
	prometheus.MustRegister(routedRequests)
	prometheus.MustRegister(routingLatency)
}

// --- Your Existing Logic ---

type ReplicaHealth struct {
	URL        string
	QueueDepth float64
	Running    float64
	KVCache    float64
	Error      error
}

var (
	pendingMu sync.RWMutex
	pending   = map[string]*atomic.Int64{}
)

func getPending(url string) *atomic.Int64 {
	pendingMu.RLock()
	c, ok := pending[url]
	pendingMu.RUnlock()
	if ok {
		return c
	}
	pendingMu.Lock()
	defer pendingMu.Unlock()
	c = &atomic.Int64{}
	pending[url] = c
	return c
}

func (r ReplicaHealth) Score(pendingCount int64) float64 {
	return (r.QueueDepth * 10.0) + (r.Running * 1.0) + (float64(pendingCount) * 8.0) + (r.KVCache * 5.0)
}

func scrapeMetrics(replicaURL string) (ReplicaHealth, error) {
	resp, err := http.Get(replicaURL + "/metrics")
	if err != nil {
		return ReplicaHealth{URL: replicaURL, Error: err}, err
	}
	defer resp.Body.Close()

	health := ReplicaHealth{URL: replicaURL}
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		var value float64
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		fmt.Sscanf(parts[1], "%f", &value)

		switch {
		case strings.HasPrefix(line, "vllm:num_requests_waiting{"):
			health.QueueDepth = value
		case strings.HasPrefix(line, "vllm:num_requests_running{"):
			health.Running = value
		case strings.HasPrefix(line, "vllm:kv_cache_usage_perc{"):
			health.KVCache = value
		}
	}
	return health, nil
}

func scrapeAll(replicas []string) []ReplicaHealth {
	results := make([]ReplicaHealth, len(replicas))
	var wg sync.WaitGroup
	for i, url := range replicas {
		wg.Add(1)
		go func(i int, url string) {
			defer wg.Done()
			h, _ := scrapeMetrics(url)
			results[i] = h
		}(i, url)
	}
	wg.Wait()
	return results
}

func pickBest(healths []ReplicaHealth) string {
	best := ""
	bestScore := float64(1 << 53)

	for _, h := range healths {
		if h.Error != nil {
			continue
		}
		pendingCount := getPending(h.URL).Load()
		score := h.Score(pendingCount)

		if score < bestScore {
			bestScore = score
			best = h.URL
		}
	}
	return best
}

var REPLICAS = []string{
	"http://vllm-headless:8000",
}

func getEndpoints() []string {
    // This looks up the Headless Service and returns all individual Pod IPs
    ips, err := net.LookupHost("vllm-headless.default.svc.cluster.local")
    if err != nil {
        log.Printf("DNS lookup failed: %v", err)
        return nil
    }

    var urls []string
    for _, ip := range ips {
        urls = append(urls, fmt.Sprintf("http://%s:8000", ip))
    }
    return urls
}

func handler(w http.ResponseWriter, r *http.Request) {
	// 1. Start timer for decision latency
	start := time.Now()

	activeReplicas := getEndpoints()

	if len(activeReplicas) == 0 {
        log.Printf("Discovery returned 0 replicas from DNS")
        http.Error(w, "no replicas discovered via DNS", http.StatusServiceUnavailable)
        return
    }

	healths := scrapeAll(activeReplicas)
	chosen := pickBest(healths)

	// 2. Record decision latency
	routingLatency.Observe(time.Since(start).Seconds())

	if chosen == "" {
		log.Printf("Health check failed for all discovered IPs: %v", activeReplicas)
		http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
		return
	}

	// 3. Increment request counter with the replica label
	routedRequests.WithLabelValues(chosen).Inc()

	log.Printf("routing to %s", chosen)

	target, _ := url.Parse(chosen)
	proxy := httputil.NewSingleHostReverseProxy(target)
	
	counter := getPending(chosen)
	counter.Add(1)
	defer counter.Add(-1)
	proxy.ServeHTTP(w, r)
}

func main() {
	// Register the /metrics endpoint for Prometheus to scrape
	http.Handle("/metrics", promhttp.Handler())

	// Your router handler
	http.HandleFunc("/", handler)

	log.Println("router listening on :9000")
	log.Fatal(http.ListenAndServe(":9000", nil))
}