package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"router/admission"
	"router/execute"
	"router/index"
	"router/metrics"
	"router/prepare"
	"router/selector"
	"router/util"
	"router/workflow"
)

// Handler forwards requests to the replica chosen by s.
// ReverseProxy instances are constructed once per replica and reused.
type Handler struct {
	s              selector.Selector
	blockIndex     *index.BlockIndex
	admission      *admission.Controller
	pd             *PDOptions
	mu             sync.RWMutex
	proxies        map[string]*httputil.ReverseProxy
	requestTimeout time.Duration
	prepare        *prepare.Pipeline
	logEvery       uint64
	indexLogEvery  uint64
	reqCount       atomic.Uint64
}

// PDOptions wires Layer 13 disaggregated prefill/decode execution.
type PDOptions struct {
	Workflow *workflow.PDWorkflow
	Executor *execute.Executor
	Prefill  selector.Selector
	Decode   selector.Selector
}

func NewHandler(s selector.Selector, blockIndex *index.BlockIndex, adm *admission.Controller, pd *PDOptions, timeout time.Duration) *Handler {
	return &Handler{
		s:              s,
		blockIndex:     blockIndex,
		admission:      adm,
		pd:             pd,
		proxies:        make(map[string]*httputil.ReverseProxy),
		requestTimeout: timeout,
		prepare:        prepare.NewDefaultPipeline(),
		logEvery:       uint64(util.GetEnvInt("ROUTER_PREPARE_LOG_EVERY", 1)),
		indexLogEvery:  uint64(util.GetEnvInt("ROUTER_INDEX_LOG_EVERY", 0)),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, tokens := ParseChatRequest(r)

	reqCtx := &prepare.RequestContext{
		PromptText:    PromptText(req),
		TokenEstimate: tokens,
	}
	if err := h.prepare.Prepare(reqCtx); err != nil {
		log.Printf("proxy: prepare failed: %v", err)
	}
	h.maybeLogBlockHashes(reqCtx)

	if h.pd != nil {
		h.serveDisaggregated(w, r, req, reqCtx, tokens)
		return
	}

	chosen, score := h.s.Pick(reqCtx)
	if chosen == "" {
		log.Printf("proxy: no healthy replicas available")
		http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
		return
	}

	log.Printf("proxy: routing to %s score=%.2f priority=%s", chosen, score, RequestPriority(r))

	if h.admission != nil && h.admission.Enabled() {
		release, ok := h.tryAcquire(w, r, chosen, RequestPriority(r))
		if !ok {
			return
		}
		defer release()
	}

	if h.blockIndex != nil && len(reqCtx.BlockHashes) > 0 {
		h.blockIndex.RegisterBlocks(chosen, reqCtx.BlockHashes)
		h.maybeLogIndexMatch(reqCtx)
	}

	metrics.RouterRequestsTotal.Inc()
	metrics.RouterTargetRequestsTotal.WithLabelValues(chosen).Inc()
	metrics.RouterInflightRequests.Inc()
	defer metrics.RouterInflightRequests.Dec()
	
	h.s.OnRequestStart(chosen, tokens)
	defer h.s.OnRequestFinish(chosen, tokens)

	h.proxyFor(chosen).ServeHTTP(w, r)
}

func (h *Handler) maybeLogBlockHashes(ctx *prepare.RequestContext) {
	if h.logEvery == 0 {
		return
	}
	n := h.reqCount.Add(1)
	if n%h.logEvery != 0 {
		return
	}
	log.Printf("prepare: blocks=%d chain=%s prompt_len=%d",
		len(ctx.BlockHashes), prepare.FormatBlockHashChain(ctx.BlockHashes), len(ctx.PromptText))
}

func (h *Handler) maybeLogIndexMatch(ctx *prepare.RequestContext) {
	if h.indexLogEvery == 0 || h.blockIndex == nil {
		return
	}
	n := h.reqCount.Load()
	if n%h.indexLogEvery != 0 {
		return
	}
	pod, matched := h.blockIndex.BestPod(ctx.BlockHashes)
	log.Printf("index: best_pod=%d matched_blocks=%d/%d query_chain=%s",
		pod, matched, len(ctx.BlockHashes), prepare.FormatBlockHashChain(ctx.BlockHashes))
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