package proxy

import (
	"time"
	"strings"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	

	"router/metrics"
	"router/selector"
)

// Handler forwards requests to the replica chosen by s.
// ReverseProxy instances are constructed once per replica and reused.
type Handler struct {
	s      selector.Selector
	mu     sync.RWMutex
	proxies map[string]*httputil.ReverseProxy
	requestTimeout time.Duration
}

func NewHandler(s selector.Selector, timeout time.Duration) *Handler {
	return &Handler{
		s:       s,
		proxies: make(map[string]*httputil.ReverseProxy),
		requestTimeout: timeout,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tokens := estimateRequestTokensAndRestoreBody(r)

	chosen, score := h.s.Pick()
	if chosen == "" {
		log.Printf("proxy: no healthy replicas available")
		http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
		return
	}

	log.Printf("proxy: routing to %s score=%.2f", chosen, score)

	metrics.RouterRequestsTotal.Inc()
	metrics.RouterTargetRequestsTotal.WithLabelValues(chosen).Inc()
	metrics.RouterInflightRequests.Inc()
	defer metrics.RouterInflightRequests.Dec()
	
	h.s.OnRequestStart(chosen, tokens)
	defer h.s.OnRequestFinish(chosen, tokens)

    h.proxyFor(chosen).ServeHTTP(w, r)
}

// proxyFor returns the cached ReverseProxy for a replica, creating it if needed.
func (h *Handler) proxyFor(replicaURL string) *httputil.ReverseProxy {
	h.mu.RLock()
	rp, ok := h.proxies[replicaURL]
	h.mu.RUnlock()
	if ok {
		return rp
	}

	target, err := url.Parse(replicaURL)
	if err != nil {
		// Should never happen — URLs come from DNS + scrape pipeline.
		log.Printf("proxy: invalid replica URL %q: %v", replicaURL, err)
		return httputil.NewSingleHostReverseProxy(target)
	}

	rp = httputil.NewSingleHostReverseProxy(target)

	if h.requestTimeout > 0 {
		rp.Transport = &http.Transport{
			ResponseHeaderTimeout: h.requestTimeout,
		}
	}

	rp.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		log.Printf("proxy: timeout/error for %s: %v", req.URL, err)
		if err != nil && strings.Contains(err.Error(), "timeout") {
			metrics.RouterTimeoutTotal.WithLabelValues(replicaURL).Inc()
			http.Error(rw, "Gateway Timeout", http.StatusGatewayTimeout)
			return
		}
		http.Error(rw, "Bad Gateway", http.StatusBadGateway)
	}

	h.mu.Lock()
	h.proxies[replicaURL] = rp
	h.mu.Unlock()

	return rp
}