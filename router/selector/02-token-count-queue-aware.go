package selector

import (
	"math"
	"sync"

	"router/discovery"
)

type tokenReplica struct {
	url        string
	running    float64
	queueDepth float64
	tokenLoad  float64
	healthy    bool
}

type TokenAware struct {
	mu       sync.RWMutex
	replicas map[string]*tokenReplica
}

func NewTokenAware() *TokenAware {
	return &TokenAware{
		replicas: make(map[string]*tokenReplica),
	}
}

// Update refreshes health metrics from discovery.
func (t *TokenAware) Update(healths []discovery.ReplicaHealth) {
	t.mu.Lock()
	defer t.mu.Unlock()

	seen := make(map[string]bool)

	for _, h := range healths {
		seen[h.URL] = true

		r, ok := t.replicas[h.URL]
		if !ok {
			r = &tokenReplica{
				url: h.URL,
			}
			t.replicas[h.URL] = r
		}

		r.running = h.Running
		r.queueDepth = h.QueueDepth
		r.healthy = h.Error == nil
	}

	// remove replicas that disappeared
	for url := range t.replicas {
		if !seen[url] {
			delete(t.replicas, url)
		}
	}
}

// Pick chooses the replica with the lowest score.
func (t *TokenAware) Pick() (string, float64) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	bestURL := ""
	bestScore := math.MaxFloat64

	for _, r := range t.replicas {
		if !r.healthy {
			continue
		}

		score := r.tokenLoad

		if score < bestScore {
			bestScore = score
			bestURL = r.url
		}
	}

	if bestURL == "" {
		return "", 0
	}

	return bestURL, bestScore
}

// OnRequestStart increases predicted load for a replica.
func (t *TokenAware) OnRequestStart(url string, tokens float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if r, ok := t.replicas[url]; ok {
		r.tokenLoad += tokens
	}
}

// OnRequestFinish decreases predicted load.
func (t *TokenAware) OnRequestFinish(url string, tokens float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if r, ok := t.replicas[url]; ok {
		r.tokenLoad -= tokens
		if r.tokenLoad < 0 {
			r.tokenLoad = 0
		}
	}
}