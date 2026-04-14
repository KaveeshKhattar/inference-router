package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"router/discovery"
	"router/metrics"
	"router/proxy"
	"router/selector"
	"router/util"
)

func main() {
	metrics.Register()

	client := &http.Client{
		Timeout: util.GetEnvDuration("ROUTER_SCRAPE_TIMEOUT", util.DefaultScrapeTimeout),
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// ── swap this one line to change strategy ──────────────────────────
	var s selector.Selector = selector.NewTokenAware()
	// ───────────────────────────────────────────────────────────────────

	discovery.StartBackgroundRefresh(
		client,
		s,
		util.GetEnvDuration("ROUTER_LOAD_REFRESH", util.DefaultLoadRecalc),
	)

	handler := proxy.NewHandler(s)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	mux.Handle("/", handler)

	addr := fmt.Sprintf(":%d", util.GetEnvInt("ROUTER_PORT", util.DefaultRouterPort))
	log.Printf("router listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}