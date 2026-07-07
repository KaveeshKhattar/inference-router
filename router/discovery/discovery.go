package discovery

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"router/util"
)

// ReplicaHealth is the scraped snapshot for one replica.
type ReplicaHealth struct {
	URL        string
	QueueDepth float64
	Running    float64

	// Histogram for declared token budget across all requests seen.
	// _sum / _count gives avg max tokens per request (lifetime).
	// TokenAware selector computes delta between scrapes for recency.
	MaxTokensSum   float64 // vllm:request_params_max_tokens_sum
	MaxTokensCount float64 // vllm:request_params_max_tokens_count

	// Exact prompt tokens for requests currently in the RUNNING phase.
	PromptTokensSum float64 // vllm:request_prompt_tokens_sum

	Error error
}

// Updater is the narrow interface the background refresh needs from any selector.
type Updater interface {
	Update([]ReplicaHealth)
}

// StartBackgroundRefresh resolves replicas via DNS and scrapes their metrics
// on every tick, calling u.Update with the results.
func StartBackgroundRefresh(client *http.Client, u Updater, interval time.Duration) {
	host := util.GetEnvString("VLLM_DISCOVERY_HOST", util.DefaultDiscoveryHost)
	startPoolRefresh(client, host, u, interval)
}

// StartDualPoolRefresh scrapes separate prefill and decode pools (Layer 13).
func StartDualPoolRefresh(client *http.Client, prefill, decode Updater, interval time.Duration) {
	prefillHost := util.GetEnvString("VLLM_PREFILL_DISCOVERY_HOST", util.DefaultPrefillDiscoveryHost)
	decodeHost := util.GetEnvString("VLLM_DECODE_DISCOVERY_HOST", util.DefaultDecodeDiscoveryHost)

	go func() {
		refresh := func() {
			if urls := lookupReplicasAt(prefillHost); len(urls) > 0 {
				prefill.Update(scrapeAll(client, urls))
			}
			if urls := lookupReplicasAt(decodeHost); len(urls) > 0 {
				decode.Update(scrapeAll(client, urls))
			}
		}
		refresh()
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			refresh()
		}
	}()
}

func startPoolRefresh(client *http.Client, host string, u Updater, interval time.Duration) {
	go func() {
		refresh := func() {
			replicas := lookupReplicasAt(host)
			if len(replicas) == 0 {
				return
			}
			u.Update(scrapeAll(client, replicas))
		}

		refresh()

		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			refresh()
		}
	}()
}

func lookupReplicas() []string {
	host := util.GetEnvString("VLLM_DISCOVERY_HOST", util.DefaultDiscoveryHost)
	return lookupReplicasAt(host)
}

func lookupReplicasAt(host string) []string {
	port := util.GetEnvInt("VLLM_METRICS_PORT", util.DefaultMetricsPort)

	ips, err := net.LookupHost(host)
	if err != nil {
		log.Printf("discovery: DNS lookup failed for %s: %v", host, err)
		return nil
	}

	urls := make([]string, 0, len(ips))
	for _, ip := range ips {
		urls = append(urls, fmt.Sprintf("http://%s:%d", ip, port))
	}
	return urls
}

// scrapeAll hits /metrics on every replica concurrently.
// Always returns one entry per replica — failed scrapes have Error set.
func scrapeAll(client *http.Client, replicas []string) []ReplicaHealth {
	results := make([]ReplicaHealth, len(replicas))
	var wg sync.WaitGroup

	for i, r := range replicas {
		i, r := i, r
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = scrapeOne(client, r)
		}()
	}

	wg.Wait()
	return results
}

// scrapeOne fetches /metrics from a single replica and parses the fields we need.
func scrapeOne(client *http.Client, baseURL string) ReplicaHealth {
	h := ReplicaHealth{URL: baseURL}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/metrics", nil)
	if err != nil {
		h.Error = fmt.Errorf("build request: %w", err)
		return h
	}

	resp, err := client.Do(req)
	if err != nil {
		h.Error = fmt.Errorf("GET /metrics: %w", err)
		return h
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.Error = fmt.Errorf("GET /metrics: status %d", resp.StatusCode)
		return h
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line[0] == '#' {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		var v float64
		if _, err := fmt.Sscanf(parts[1], "%f", &v); err != nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "vllm:num_requests_waiting"):
			h.QueueDepth = v
		case strings.HasPrefix(line, "vllm:num_requests_running"):
			h.Running = v
		case strings.HasPrefix(line, "vllm:request_params_max_tokens_sum"):
			h.MaxTokensSum = v
		case strings.HasPrefix(line, "vllm:request_params_max_tokens_count"):
			h.MaxTokensCount = v
		case strings.HasPrefix(line, "vllm:request_prompt_tokens_sum"):
			h.PromptTokensSum = v
		}
	}

	if err := scanner.Err(); err != nil {
		h.Error = fmt.Errorf("scan: %w", err)
	}

	return h
}