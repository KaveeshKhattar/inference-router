package selector

import (
	"fmt"
	"log"
	"sync"
	"time"

	"router/discovery"
	"router/index"
	"router/prepare"
)

type replicaState struct {
	url        string
	podIndex   int
	running    float64
	queueDepth float64
	tokenLoad  float64
	healthy    bool
}

// ComposableSelector implements Filter → Score → Pick with pluggable scorers.
type ComposableSelector struct {
	mu       sync.RWMutex
	replicas map[string]*replicaState
	scorer   Scorer
	index    *index.BlockIndex
}

func NewComposable(idx *index.BlockIndex, scorer Scorer) *ComposableSelector {
	return &ComposableSelector{
		replicas: make(map[string]*replicaState),
		scorer:   scorer,
		index:    idx,
	}
}

// NewCacheAware builds the default Layer 10 router: weighted cache + load scoring.
func NewCacheAware(idx *index.BlockIndex, cacheWeight, loadWeight float64) *ComposableSelector {
	scorer := NewWeightedScorer(
		[]Scorer{NewCacheAffinityScorer(idx), NewLoadScorer()},
		[]float64{cacheWeight, loadWeight},
	)
	return NewComposable(idx, scorer)
}

func (c *ComposableSelector) Update(healths []discovery.ReplicaHealth) {
	c.mu.Lock()
	defer c.mu.Unlock()

	seen := make(map[string]bool)
	for _, h := range healths {
		seen[h.URL] = true

		r, ok := c.replicas[h.URL]
		if !ok {
			podIndex := -1
			if c.index != nil && h.Error == nil {
				podIndex = c.index.Pods().Ensure(h.URL)
			}
			r = &replicaState{url: h.URL, podIndex: podIndex}
			c.replicas[h.URL] = r
		}

		r.running = h.Running
		r.queueDepth = h.QueueDepth
		r.healthy = h.Error == nil
		if r.healthy && r.podIndex < 0 && c.index != nil {
			r.podIndex = c.index.Pods().Ensure(h.URL)
		}
	}

	for url := range c.replicas {
		if !seen[url] {
			delete(c.replicas, url)
		}
	}
}

func (c *ComposableSelector) Pick(ctx *prepare.RequestContext) (string, float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	candidates := c.filterHealthy()
	if len(candidates) == 0 {
		log.Println("Pick: no healthy replicas")
		return "", 0
	}

	start := time.Now()
	sc := ScoringContext{Request: ctx, Candidates: candidates}
	scores := c.scorer.Score(sc)
	bestIdx := MaxScorePick(scores)
	if bestIdx < 0 {
		return "", 0
	}

	chosen := candidates[bestIdx]
	elapsed := time.Since(start)

	var detail string
	for i, cand := range candidates {
		marker := " "
		if i == bestIdx {
			marker = "*"
		}
		detail += fmt.Sprintf("%s%s=load:%.1f,score:%.3f ", marker, cand.URL, cand.TokenLoad, scores[i])
	}
	log.Printf("Pick: [%s] selected %s combined=%.3f | took %s", detail, chosen.URL, scores[bestIdx], elapsed)

	return chosen.URL, scores[bestIdx]
}

// PickWithScorer runs Filter → Score → Pick using a one-off scorer (Layer 13 decode pass).
func (c *ComposableSelector) PickWithScorer(ctx *prepare.RequestContext, scorer Scorer) (string, float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	candidates := c.filterHealthy()
	if len(candidates) == 0 {
		log.Println("Pick: no healthy replicas")
		return "", 0
	}

	start := time.Now()
	sc := ScoringContext{Request: ctx, Candidates: candidates}
	scores := scorer.Score(sc)
	bestIdx := MaxScorePick(scores)
	if bestIdx < 0 {
		return "", 0
	}

	chosen := candidates[bestIdx]
	elapsed := time.Since(start)
	log.Printf("Pick(custom): selected %s score=%.3f candidates=%d took %s",
		chosen.URL, scores[bestIdx], len(candidates), elapsed)
	return chosen.URL, scores[bestIdx]
}

func (c *ComposableSelector) filterHealthy() []Candidate {
	out := make([]Candidate, 0, len(c.replicas))
	for _, r := range c.replicas {
		if !r.healthy {
			continue
		}
		out = append(out, Candidate{
			URL:       r.url,
			PodIndex:  r.podIndex,
			Healthy:   true,
			TokenLoad: r.tokenLoad,
		})
	}
	return out
}

func (c *ComposableSelector) OnRequestStart(url string, tokens float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.replicas[url]; ok {
		r.tokenLoad += tokens
	}
}

func (c *ComposableSelector) OnRequestFinish(url string, tokens float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.replicas[url]; ok {
		r.tokenLoad -= tokens
		if r.tokenLoad < 0 {
			r.tokenLoad = 0
		}
	}
}

// String describes the active scoring composition for startup logs.
func (c *ComposableSelector) String() string {
	if ws, ok := c.scorer.(*WeightedScorer); ok {
		return fmt.Sprintf("composable(%d scorers)", len(ws.Parts))
	}
	return "composable"
}
