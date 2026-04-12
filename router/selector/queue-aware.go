package selector

import (
	"log"
	"math"
	"sync"
	"sync/atomic"

	"router/discovery"
)

type replicaState struct {
	estimatedLoad float64
}

// QueueAware picks the replica with the lowest projected token-weighted load,
// combining scraped vLLM metrics with locally tracked in-flight pending tokens.
type QueueAware struct {
	weight float64 // weight applied to local pending-token counter

	mu     sync.RWMutex
	states map[string]replicaState

	pendingMu     sync.RWMutex
	pending        map[string]*atomic.Int64
	pendingTokensMu sync.RWMutex
	pendingTokens   map[string]*atomic.Int64
}

func NewQueueAware(localPendingWeight float64) *QueueAware {
	return &QueueAware{
		weight:        localPendingWeight,
		states:        make(map[string]replicaState),
		pending:       make(map[string]*atomic.Int64),
		pendingTokens: make(map[string]*atomic.Int64),
	}
}

func (q *QueueAware) Update(healths []discovery.ReplicaHealth) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, h := range healths {
		if h.Error != nil {
			delete(q.states, h.URL)
			continue
		}
		q.states[h.URL] = replicaState{estimatedLoad: h.EstimatedLoad}
	}
}

func (q *QueueAware) Pick(replicas []string, incomingTokens int64) (string, float64) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	best := ""
	bestScore := math.MaxFloat64

	for _, r := range replicas {
		state, ok := q.states[r]
		if !ok {
			log.Printf("selector: no health state for %s, skipping", r)
			continue
		}
		localPending := float64(q.getPendingTokens(r).Load())
		score := state.estimatedLoad + q.weight*localPending + float64(incomingTokens)
		if score < bestScore {
			bestScore = score
			best = r
		}
	}
	return best, bestScore
}

// GetPending / GetPendingTokens are exported so proxy.go can track in-flight state.
func (q *QueueAware) GetPending(replica string) *atomic.Int64 {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	if c, ok := q.pending[replica]; ok {
		return c
	}
	c := &atomic.Int64{}
	q.pending[replica] = c
	return c
}

func (q *QueueAware) getPendingTokens(replica string) *atomic.Int64 {
	q.pendingTokensMu.Lock()
	defer q.pendingTokensMu.Unlock()
	if c, ok := q.pendingTokens[replica]; ok {
		return c
	}
	c := &atomic.Int64{}
	q.pendingTokens[replica] = c
	return c
}

func (q *QueueAware) GetPendingTokens(replica string) *atomic.Int64 {
	return q.getPendingTokens(replica)
}