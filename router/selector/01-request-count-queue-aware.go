package selector

import (
    "math"
    "sync"
    "router/discovery"
)

type QueueAware struct {
    mu       sync.RWMutex
    replicas []discovery.ReplicaHealth
}

func NewRequestCountQueueAware() *QueueAware {
    return &QueueAware{}
}

func (q *QueueAware) Update(healths []discovery.ReplicaHealth) {
    q.mu.Lock()
    q.replicas = healths
    q.mu.Unlock()
}

func (q *QueueAware) Pick() (string, float64) {
    q.mu.RLock()
    defer q.mu.RUnlock()

    best, bestScore := "", math.MaxFloat64
    for _, h := range q.replicas {
        if h.Error != nil {
            continue
        }
        score := h.Running + h.QueueDepth
        if score < bestScore {
            bestScore = score
            best = h.URL
        }
    }
    return best, bestScore
}