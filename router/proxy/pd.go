package proxy

import (
	"log"
	"net/http"

	"router/admission"
	"router/metrics"
	"router/prepare"
	"router/selector"
)

func (h *Handler) serveDisaggregated(
	w http.ResponseWriter,
	r *http.Request,
	req ChatRequest,
	reqCtx *prepare.RequestContext,
	tokens float64,
) {
	if err := h.pd.Workflow.Select(reqCtx); err != nil {
		log.Printf("proxy: pd select failed: %v", err)
		http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
		return
	}

	pri := RequestPriority(r)
	log.Printf("proxy: pd prefill=%s (%.3f) decode=%s (%.3f) priority=%s",
		reqCtx.PrefillURL, reqCtx.PrefillScore, reqCtx.DecodeURL, reqCtx.DecodeScore, pri)

	prefillTokens := float64(len(reqCtx.PromptText)/4 + 1)

	releasePrefill, ok := h.tryAcquire(w, r, reqCtx.PrefillURL, pri)
	if !ok {
		return
	}
	h.trackStart(h.pd.Prefill, reqCtx.PrefillURL, prefillTokens)

	if err := h.pd.Executor.Prefill(r.Context(), req, reqCtx.PrefillURL); err != nil {
		h.trackFinish(h.pd.Prefill, reqCtx.PrefillURL, prefillTokens)
		releasePrefill()
		log.Printf("proxy: prefill failed: %v", err)
		http.Error(w, "prefill failed", http.StatusBadGateway)
		return
	}

	h.trackFinish(h.pd.Prefill, reqCtx.PrefillURL, prefillTokens)
	releasePrefill()

	if h.blockIndex != nil && len(reqCtx.BlockHashes) > 0 {
		h.blockIndex.RegisterBlocks(reqCtx.PrefillURL, reqCtx.BlockHashes)
	}

	releaseDecode, ok := h.tryAcquire(w, r, reqCtx.DecodeURL, pri)
	if !ok {
		return
	}
	h.trackStart(h.pd.Decode, reqCtx.DecodeURL, tokens)
	defer h.trackFinish(h.pd.Decode, reqCtx.DecodeURL, tokens)
	defer releaseDecode()

	if h.blockIndex != nil && len(reqCtx.BlockHashes) > 0 {
		h.blockIndex.RegisterBlocks(reqCtx.DecodeURL, reqCtx.BlockHashes)
	}

	metrics.RouterRequestsTotal.Inc()
	metrics.RouterTargetRequestsTotal.WithLabelValues(reqCtx.PrefillURL).Inc()
	metrics.RouterTargetRequestsTotal.WithLabelValues(reqCtx.DecodeURL).Inc()
	metrics.RouterInflightRequests.Inc()
	defer metrics.RouterInflightRequests.Dec()

	if err := h.pd.Executor.StreamDecode(r.Context(), w, req, reqCtx.DecodeURL, reqCtx.PrefillHostPort); err != nil {
		log.Printf("proxy: decode failed: %v", err)
	}
}

func (h *Handler) tryAcquire(w http.ResponseWriter, r *http.Request, podURL string, pri admission.Priority) (func(), bool) {
	if h.admission == nil || !h.admission.Enabled() {
		return func() {}, true
	}
	release, err := h.admission.Acquire(r.Context(), podURL, pri)
	if err != nil {
		log.Printf("proxy: admission rejected pod=%s priority=%s: %v", podURL, pri, err)
		http.Error(w, "service overloaded", http.StatusTooManyRequests)
		return nil, false
	}
	return release, true
}

func (h *Handler) trackStart(s selector.Selector, url string, tokens float64) {
	if s != nil {
		s.OnRequestStart(url, tokens)
	}
}

func (h *Handler) trackFinish(s selector.Selector, url string, tokens float64) {
	if s != nil {
		s.OnRequestFinish(url, tokens)
	}
}
