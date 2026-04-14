package selector

import (
	"sort"
	"sync"
	"sync/atomic"

	"router/discovery"
)

// RoundRobin cycles through healthy replicas ignoring load.
// Healthy set is rebuilt on every Update call.
type RoundRobin struct {
	mu         sync.RWMutex
	candidates []string // sorted, healthy replica URLs
	counter    atomic.Uint64
}

func NewRoundRobin() *RoundRobin {
	return &RoundRobin{}
}

// Update rebuilds the healthy replica list from the latest scrape.
func (rr *RoundRobin) Update(healths []discovery.ReplicaHealth) {
	next := make([]string, 0, len(healths))
	for _, h := range healths {
		if h.Error == nil {
			next = append(next, h.URL)
		}
	}
	sort.Strings(next) // stable order so counter wraps predictably

	rr.mu.Lock()
	rr.candidates = next
	rr.mu.Unlock()
}

// Pick returns the next replica in round-robin order.
func (rr *RoundRobin) Pick() (string, float64) {
	rr.mu.RLock()
	c := rr.candidates
	rr.mu.RUnlock()

	if len(c) == 0 {
		return "", 0
	}

	idx := rr.counter.Add(1) - 1
	return c[idx%uint64(len(c))], 0
}