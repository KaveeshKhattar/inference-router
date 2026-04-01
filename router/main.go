package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultMetricsPort            = 8000
	defaultRouterPort             = 9000
	defaultDiscoveryHost          = "vllm-headless.default.svc.cluster.local"
	defaultWeightRecalcInterval   = 2 * time.Second
	defaultWeightSnapshotInterval = 5 * time.Minute
	defaultScrapeTimeout          = 1500 * time.Millisecond
	defaultSnapshotConfigMap      = "router-weight-snapshot"
	defaultSnapshotDataKey        = "weights.json"
	defaultSnapshotSchema         = "v1"
	defaultServiceAccountDir      = "/var/run/secrets/kubernetes.io/serviceaccount"
	minDynamicWeight              = 0.5
	maxDynamicWeight              = 2.0
)

var (
	routedRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "router_requests_total",
			Help: "Total routed requests",
		},
		[]string{"replica"},
	)

	routingLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "router_decision_seconds",
			Help:    "Time taken to compute routing decision",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
	)

	replicaWeight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "router_replica_weight",
			Help: "Current dynamic routing weight for each replica",
		},
		[]string{"replica"},
	)

	weightRecomputeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "router_weight_recompute_total",
			Help: "Number of dynamic weight recompute loops by status",
		},
		[]string{"status"},
	)

	weightSnapshotTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "router_weight_snapshot_total",
			Help: "Number of weight snapshot read/write attempts by status",
		},
		[]string{"operation", "status"},
	)
)

func init() {
	prometheus.MustRegister(routedRequests)
	prometheus.MustRegister(routingLatency)
	prometheus.MustRegister(replicaWeight)
	prometheus.MustRegister(weightRecomputeTotal)
	prometheus.MustRegister(weightSnapshotTotal)
}

type ReplicaHealth struct {
	URL        string
	QueueDepth float64
	Running    float64
	KVCache    float64
	Error      error
}

func (r ReplicaHealth) Score(pendingCount int64) float64 {
	return (r.QueueDepth * 10.0) + (r.Running * 1.0) + (float64(pendingCount) * 8.0) + (r.KVCache * 5.0)
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

var (
	replicaWeightsMu sync.RWMutex
	replicaWeights   = map[string]float64{}
)

func getReplicaWeight(replicaURL string) float64 {
	replicaWeightsMu.RLock()
	w, ok := replicaWeights[replicaURL]
	replicaWeightsMu.RUnlock()
	if !ok {
		return 1.0
	}
	return w
}

func setReplicaWeights(weights map[string]float64) {
	replicaWeightsMu.Lock()
	old := replicaWeights
	replicaWeights = weights
	replicaWeightsMu.Unlock()

	for replica, weight := range weights {
		replicaWeight.WithLabelValues(replica).Set(weight)
	}
	for replica := range old {
		if _, ok := weights[replica]; !ok {
			replicaWeight.DeleteLabelValues(replica)
		}
	}
}

func cloneReplicaWeights() map[string]float64 {
	replicaWeightsMu.RLock()
	defer replicaWeightsMu.RUnlock()
	out := make(map[string]float64, len(replicaWeights))
	for replica, weight := range replicaWeights {
		out[replica] = weight
	}
	return out
}

func getEnvDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		log.Printf("invalid duration for %s=%q, using default %s", name, value, fallback)
		return fallback
	}
	return d
}

func getEnvInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		log.Printf("invalid integer for %s=%q, using default %d", name, value, fallback)
		return fallback
	}
	return n
}

func getEnvBool(name string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		log.Printf("invalid boolean for %s=%q, using default %t", name, value, fallback)
		return fallback
	}
}

func getEndpoints() []string {
	discoveryHost := strings.TrimSpace(os.Getenv("VLLM_DISCOVERY_HOST"))
	if discoveryHost == "" {
		discoveryHost = defaultDiscoveryHost
	}

	metricsPort := getEnvInt("VLLM_METRICS_PORT", defaultMetricsPort)
	ips, err := net.LookupHost(discoveryHost)
	if err != nil {
		log.Printf("DNS lookup failed for %s: %v", discoveryHost, err)
		return nil
	}

	urls := make([]string, 0, len(ips))
	for _, ip := range ips {
		urls = append(urls, fmt.Sprintf("http://%s:%d", ip, metricsPort))
	}
	return urls
}

func scrapeMetrics(client *http.Client, replicaURL string) (ReplicaHealth, error) {
	resp, err := client.Get(replicaURL + "/metrics")
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

		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		var value float64
		if _, err := fmt.Sscanf(parts[1], "%f", &value); err != nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "vllm:num_requests_waiting{"):
			health.QueueDepth = value
		case strings.HasPrefix(line, "vllm:num_requests_running{"):
			health.Running = value
		case strings.HasPrefix(line, "vllm:kv_cache_usage_perc{"):
			health.KVCache = value
		}
	}

	if err := scanner.Err(); err != nil {
		health.Error = err
		return health, err
	}
	return health, nil
}

func scrapeAll(client *http.Client, replicas []string) []ReplicaHealth {
	results := make([]ReplicaHealth, len(replicas))
	var wg sync.WaitGroup

	for i, replicaURL := range replicas {
		wg.Add(1)
		go func(i int, replicaURL string) {
			defer wg.Done()
			h, _ := scrapeMetrics(client, replicaURL)
			results[i] = h
		}(i, replicaURL)
	}

	wg.Wait()
	return results
}

func computeDynamicWeights(healths []ReplicaHealth) map[string]float64 {
	type scoredReplica struct {
		url      string
		pressure float64
	}

	scored := make([]scoredReplica, 0, len(healths))
	for _, h := range healths {
		if h.Error != nil {
			continue
		}
		pressure := (h.QueueDepth * 10.0) + (h.Running * 1.0) + (h.KVCache * 5.0)
		scored = append(scored, scoredReplica{url: h.URL, pressure: pressure})
	}
	if len(scored) == 0 {
		return map[string]float64{}
	}

	minPressure := math.MaxFloat64
	maxPressure := -math.MaxFloat64
	for _, item := range scored {
		if item.pressure < minPressure {
			minPressure = item.pressure
		}
		if item.pressure > maxPressure {
			maxPressure = item.pressure
		}
	}

	weights := make(map[string]float64, len(scored))
	span := maxPressure - minPressure
	if span <= 0 {
		for _, item := range scored {
			weights[item.url] = 1.0
		}
		return weights
	}

	for _, item := range scored {
		normalized := (item.pressure - minPressure) / span
		weights[item.url] = minDynamicWeight + normalized*(maxDynamicWeight-minDynamicWeight)
	}
	return weights
}

func pickBest(healths []ReplicaHealth) string {
	bestReplica := ""
	bestScore := math.MaxFloat64

	for _, h := range healths {
		if h.Error != nil {
			continue
		}

		pendingCount := getPending(h.URL).Load()
		baseScore := h.Score(pendingCount)
		weightedScore := baseScore * getReplicaWeight(h.URL)

		if weightedScore < bestScore {
			bestScore = weightedScore
			bestReplica = h.URL
		}
	}
	return bestReplica
}

func recomputeWeights(client *http.Client) {
	activeReplicas := getEndpoints()
	if len(activeReplicas) == 0 {
		weightRecomputeTotal.WithLabelValues("no_replicas").Inc()
		return
	}

	healths := scrapeAll(client, activeReplicas)
	weights := computeDynamicWeights(healths)
	if len(weights) == 0 {
		weightRecomputeTotal.WithLabelValues("no_healthy_replicas").Inc()
		return
	}

	setReplicaWeights(weights)
	weightRecomputeTotal.WithLabelValues("success").Inc()
}

func runWeightControlLoop(client *http.Client, interval time.Duration) {
	if interval <= 0 {
		interval = defaultWeightRecalcInterval
	}

	recomputeWeights(client)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		recomputeWeights(client)
	}
}

type weightSnapshotPayload struct {
	SchemaVersion string             `json:"schemaVersion"`
	UpdatedAt     string             `json:"updatedAt"`
	Weights       map[string]float64 `json:"weights"`
}

type configMapDocument struct {
	APIVersion string            `json:"apiVersion,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
}

type weightSnapshotStore struct {
	baseURL       string
	namespace     string
	name          string
	dataKey       string
	token         string
	kubeAPIClient *http.Client
}

func buildWeightSnapshotJSON(weights map[string]float64, now time.Time) ([]byte, error) {
	payload := weightSnapshotPayload{
		SchemaVersion: defaultSnapshotSchema,
		UpdatedAt:     now.UTC().Format(time.RFC3339),
		Weights:       weights,
	}
	return json.Marshal(payload)
}

func parseWeightSnapshotJSON(raw string) (map[string]float64, error) {
	var payload weightSnapshotPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	if payload.Weights == nil {
		return map[string]float64{}, nil
	}
	return payload.Weights, nil
}

func newSnapshotStoreFromEnv() (*weightSnapshotStore, error) {
	namespace := strings.TrimSpace(os.Getenv("ROUTER_WEIGHT_SNAPSHOT_NAMESPACE"))
	if namespace == "" {
		namespace = strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	}
	if namespace == "" {
		namespace = "default"
	}

	name := strings.TrimSpace(os.Getenv("ROUTER_WEIGHT_SNAPSHOT_CONFIGMAP"))
	if name == "" {
		name = defaultSnapshotConfigMap
	}

	dataKey := strings.TrimSpace(os.Getenv("ROUTER_WEIGHT_SNAPSHOT_DATA_KEY"))
	if dataKey == "" {
		dataKey = defaultSnapshotDataKey
	}

	tokenRaw, err := os.ReadFile(path.Join(defaultServiceAccountDir, "token"))
	if err != nil {
		return nil, fmt.Errorf("read service account token: %w", err)
	}
	token := strings.TrimSpace(string(tokenRaw))
	if token == "" {
		return nil, fmt.Errorf("service account token is empty")
	}

	caRaw, err := os.ReadFile(path.Join(defaultServiceAccountDir, "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("read service account CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caRaw) {
		return nil, fmt.Errorf("failed to parse service account CA cert")
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}

	return &weightSnapshotStore{
		baseURL:       "https://kubernetes.default.svc",
		namespace:     namespace,
		name:          name,
		dataKey:       dataKey,
		token:         token,
		kubeAPIClient: &http.Client{Timeout: 5 * time.Second, Transport: transport},
	}, nil
}

func (s *weightSnapshotStore) configMapURL() string {
	return fmt.Sprintf("%s/api/v1/namespaces/%s/configmaps/%s", s.baseURL, url.PathEscape(s.namespace), url.PathEscape(s.name))
}

func (s *weightSnapshotStore) configMapCollectionURL() string {
	return fmt.Sprintf("%s/api/v1/namespaces/%s/configmaps", s.baseURL, url.PathEscape(s.namespace))
}

func (s *weightSnapshotStore) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Accept", "application/json")
	return s.kubeAPIClient.Do(req)
}

func (s *weightSnapshotStore) load() (map[string]float64, error) {
	req, err := http.NewRequest(http.MethodGet, s.configMapURL(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("load snapshot: unexpected status %d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var cm configMapDocument
	if err := json.NewDecoder(resp.Body).Decode(&cm); err != nil {
		return nil, err
	}
	if cm.Data == nil {
		return nil, nil
	}

	raw := cm.Data[s.dataKey]
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	return parseWeightSnapshotJSON(raw)
}

func (s *weightSnapshotStore) save(weights map[string]float64) error {
	snapshotJSON, err := buildWeightSnapshotJSON(weights, time.Now())
	if err != nil {
		return err
	}

	// First attempt create; if already exists, update with resourceVersion.
	createDoc := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      s.name,
			"namespace": s.namespace,
		},
		"data": map[string]string{
			s.dataKey: string(snapshotJSON),
		},
	}
	createBody, err := json.Marshal(createDoc)
	if err != nil {
		return err
	}

	createReq, err := http.NewRequest(http.MethodPost, s.configMapCollectionURL(), bytes.NewReader(createBody))
	if err != nil {
		return err
	}
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := s.do(createReq)
	if err != nil {
		return err
	}
	defer createResp.Body.Close()
	if createResp.StatusCode == http.StatusCreated {
		return nil
	}
	if createResp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(io.LimitReader(createResp.Body, 4096))
		return fmt.Errorf("create snapshot configmap failed: status=%d body=%q", createResp.StatusCode, strings.TrimSpace(string(body)))
	}

	getReq, err := http.NewRequest(http.MethodGet, s.configMapURL(), nil)
	if err != nil {
		return err
	}
	getResp, err := s.do(getReq)
	if err != nil {
		return err
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(getResp.Body, 4096))
		return fmt.Errorf("get configmap for update failed: status=%d body=%q", getResp.StatusCode, strings.TrimSpace(string(body)))
	}
	var existing struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&existing); err != nil {
		return err
	}
	if existing.Metadata.ResourceVersion == "" {
		return fmt.Errorf("existing configmap resourceVersion missing")
	}

	updateDoc := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":            s.name,
			"namespace":       s.namespace,
			"resourceVersion": existing.Metadata.ResourceVersion,
		},
		"data": map[string]string{
			s.dataKey: string(snapshotJSON),
		},
	}
	updateBody, err := json.Marshal(updateDoc)
	if err != nil {
		return err
	}

	updateReq, err := http.NewRequest(http.MethodPut, s.configMapURL(), bytes.NewReader(updateBody))
	if err != nil {
		return err
	}
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp, err := s.do(updateReq)
	if err != nil {
		return err
	}
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(updateResp.Body, 4096))
		return fmt.Errorf("update snapshot configmap failed: status=%d body=%q", updateResp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func runSnapshotLoop(store *weightSnapshotStore, interval time.Duration) {
	if store == nil {
		return
	}
	if interval <= 0 {
		interval = defaultWeightSnapshotInterval
	}

	save := func() {
		if err := store.save(cloneReplicaWeights()); err != nil {
			weightSnapshotTotal.WithLabelValues("save", "error").Inc()
			log.Printf("weight snapshot save failed: %v", err)
			return
		}
		weightSnapshotTotal.WithLabelValues("save", "success").Inc()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		save()
	}
}

func handler(client *http.Client, w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	activeReplicas := getEndpoints()
	if len(activeReplicas) == 0 {
		log.Printf("discovery returned 0 replicas from DNS")
		http.Error(w, "no replicas discovered via DNS", http.StatusServiceUnavailable)
		return
	}

	healths := scrapeAll(client, activeReplicas)
	chosen := pickBest(healths)
	routingLatency.Observe(time.Since(start).Seconds())

	if chosen == "" {
		log.Printf("health check failed for all discovered IPs: %v", activeReplicas)
		http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
		return
	}

	routedRequests.WithLabelValues(chosen).Inc()
	log.Printf("routing to %s (weight=%.3f)", chosen, getReplicaWeight(chosen))

	target, err := url.Parse(chosen)
	if err != nil {
		http.Error(w, "invalid upstream target", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	counter := getPending(chosen)
	counter.Add(1)
	defer counter.Add(-1)

	proxy.ServeHTTP(w, r)
}

func main() {
	scrapeTimeout := getEnvDuration("ROUTER_SCRAPE_TIMEOUT", defaultScrapeTimeout)
	weightInterval := getEnvDuration("ROUTER_WEIGHT_RECALC_INTERVAL", defaultWeightRecalcInterval)
	snapshotInterval := getEnvDuration("ROUTER_WEIGHT_SNAPSHOT_INTERVAL", defaultWeightSnapshotInterval)
	snapshotEnabled := getEnvBool("ROUTER_WEIGHT_SNAPSHOT_ENABLED", false)
	routerPort := getEnvInt("ROUTER_PORT", defaultRouterPort)

	client := &http.Client{Timeout: scrapeTimeout}

	if snapshotEnabled {
		store, err := newSnapshotStoreFromEnv()
		if err != nil {
			weightSnapshotTotal.WithLabelValues("load", "error").Inc()
			log.Printf("weight snapshot store init failed; continuing without persistence: %v", err)
		} else {
			loaded, err := store.load()
			if err != nil {
				weightSnapshotTotal.WithLabelValues("load", "error").Inc()
				log.Printf("weight snapshot load failed; continuing with empty weights: %v", err)
			} else if len(loaded) > 0 {
				setReplicaWeights(loaded)
				weightSnapshotTotal.WithLabelValues("load", "success").Inc()
				log.Printf("restored %d replica weights from snapshot", len(loaded))
			} else {
				weightSnapshotTotal.WithLabelValues("load", "empty").Inc()
			}
			go runSnapshotLoop(store, snapshotInterval)
		}
	}

	go runWeightControlLoop(client, weightInterval)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handler(client, w, r)
	})

	addr := fmt.Sprintf(":%d", routerPort)
	log.Printf("router listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
