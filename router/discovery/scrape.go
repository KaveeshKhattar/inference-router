package discovery

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

func ScrapeAll(client *http.Client, replicas []string) []ReplicaHealth {
	results := make([]ReplicaHealth, len(replicas))
	var wg sync.WaitGroup
	for i, r := range replicas {
		wg.Add(1)
		go func(i int, r string) {
			defer wg.Done()
			h, _ := scrapeMetrics(client, r)
			results[i] = h
		}(i, r)
	}
	wg.Wait()
	return results
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
		var v float64
		if _, err := fmt.Sscanf(parts[1], "%f", &v); err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "vllm:num_requests_waiting{"):
			health.QueueDepth = v
		case strings.HasPrefix(line, "vllm:num_requests_running{"):
			health.Running = v
		case strings.HasPrefix(line, "vllm:kv_cache_usage_perc{"):
			health.KVCache = v
		case strings.HasPrefix(line, "vllm:request_prompt_tokens_sum{"):
			health.PromptTokensSum = v
		case strings.HasPrefix(line, "vllm:request_prompt_tokens_count{"):
			health.PromptTokensCount = v
		}
	}

	health.EstimatedLoad = (health.Running + health.QueueDepth*10)

	if err := scanner.Err(); err != nil {
		health.Error = err
		return health, err
	}
	return health, nil
}