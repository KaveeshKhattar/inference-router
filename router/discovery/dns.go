package discovery

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
	"sync"
	"router/util"
)

var (
    endpointsMu sync.RWMutex
    endpoints   []string
)

func setEndpoints(e []string) {
    endpointsMu.Lock()
    defer endpointsMu.Unlock()
    endpoints = e
}

func GetCachedEndpoints() []string {
    endpointsMu.RLock()
    defer endpointsMu.RUnlock()
    return append([]string(nil), endpoints...) // safe copy
}

// ReplicaHealth is the scraped health snapshot for one replica.
type ReplicaHealth struct {
	URL               string
	QueueDepth        float64
	Running           float64
	KVCache           float64
	PromptTokensSum   float64
	PromptTokensCount float64
	AvgPromptTokens   float64
	EstimatedLoad     float64
	Error             error
}

// Updater is the narrow interface discovery needs from any selector.
type Updater interface {
	Update([]ReplicaHealth)
}

func GetEndpoints() []string {
	host := util.GetEnvString("VLLM_DISCOVERY_HOST", util.DefaultDiscoveryHost)
	port := util.GetEnvInt("VLLM_METRICS_PORT", util.DefaultMetricsPort)

	ips, err := net.LookupHost(host)
	if err != nil {
		log.Printf("DNS lookup failed for %s: %v", host, err)
		return nil
	}
	urls := make([]string, 0, len(ips))
	for _, ip := range ips {
		urls = append(urls, fmt.Sprintf("http://%s:%d", ip, port))
	}
	return urls
}

func StartBackgroundRefresh(client *http.Client, u Updater, interval time.Duration) {
	go func() {
		refresh := func() {
			replicas := GetEndpoints()
			if len(replicas) == 0 {
				return
			}
			setEndpoints(replicas)
			u.Update(ScrapeAll(client, replicas))
		}
		refresh()
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			refresh()
		}
	}()
}