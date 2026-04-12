package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"router/discovery"
	"router/metrics"
	"router/selector"
)

// pendingTracker is the narrow interface proxy needs from QueueAware
// (or any selector that wants to track in-flight state).
type pendingTracker interface {
	GetPending(replica string) interface{ Add(int64) int64; Load() int64 }
	GetPendingTokens(replica string) interface{ Add(int64) int64; Load() int64 }
}

func Handle(s selector.Selector, w http.ResponseWriter, r *http.Request) {
	replicas := discovery.GetCachedEndpoints()
	if len(replicas) == 0 {
		log.Printf("discovery returned 0 replicas")
		http.Error(w, "no replicas discovered via DNS", http.StatusServiceUnavailable)
		return
	}

	tokens := estimateRequestTokensAndRestoreBody(r)

	start := time.Now()
	chosen, score := s.Pick(replicas, tokens)
	metrics.RoutingLatency.Observe(time.Since(start).Seconds())

	if chosen == "" {
		log.Printf("no healthy replica among: %v", replicas)
		http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
		return
	}

	log.Printf("routing to %s tokens=%d score=%.2f", chosen, tokens, score)
	proxyTo(w, r, chosen, tokens, s)
}

func proxyTo(w http.ResponseWriter, r *http.Request, replica string, tokens int64, s selector.Selector) {
	metrics.RouterRequestsTotal.Inc()
	metrics.RouterTargetRequestsTotal.WithLabelValues(replica).Inc()

	target, err := url.Parse(replica)
	if err != nil {
		http.Error(w, "invalid upstream target", http.StatusInternalServerError)
		return
	}

	// Track in-flight state only if the selector supports it.
	if pt, ok := s.(pendingTracker); ok {
		p := pt.GetPending(replica)
		p.Add(1)
		defer p.Add(-1)

		pt2 := pt.GetPendingTokens(replica)
		pt2.Add(tokens)
		defer pt2.Add(-tokens)
	}

	metrics.RouterInflightRequests.Inc()
	defer metrics.RouterInflightRequests.Dec()
	metrics.RouterQueueLengthTokens.Add(float64(tokens))
	defer metrics.RouterQueueLengthTokens.Sub(float64(tokens))
	metrics.RouterTargetQueueLengthTokens.WithLabelValues(replica).Add(float64(tokens))
	defer metrics.RouterTargetQueueLengthTokens.WithLabelValues(replica).Sub(float64(tokens))
	

	httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
}