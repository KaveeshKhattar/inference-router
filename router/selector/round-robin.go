package selector

import (
	"sync"
	"sync/atomic"
	"sort"

	"router/discovery"
)

// RoundRobin cycles through healthy replicas ignoring load.
type RoundRobin struct {
	mu      sync.RWMutex // guards healthy set
	healthy map[string]struct{}
	counter atomic.Uint64
}

func NewRoundRobin() *RoundRobin {
	return &RoundRobin{healthy: make(map[string]struct{})}
}

func (rr *RoundRobin) Update(healths []discovery.ReplicaHealth) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.healthy = make(map[string]struct{}, len(healths))
	for _, h := range healths {
		if h.Error == nil {
			rr.healthy[h.URL] = struct{}{}
		}
	}
}

func (rr *RoundRobin) Pick(replicas []string, _ int64) (string, float64) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()

	var candidates []string
	for _, r := range replicas {
		if _, ok := rr.healthy[r]; ok {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return "", 0
	}
	sort.Strings(candidates)
	idx := rr.counter.Add(1) - 1
	return candidates[idx%uint64(len(candidates))], 0
}