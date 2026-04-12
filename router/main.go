package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"router/discovery"
	"router/metrics"
	"router/proxy"
	"router/selector"
	"router/util"
)

func main() {
	scrapeTimeout  := util.GetEnvDuration("ROUTER_SCRAPE_TIMEOUT", util.DefaultScrapeTimeout)
	routerPort     := util.GetEnvInt("ROUTER_PORT", util.DefaultRouterPort)
	refreshInterval := util.GetEnvDuration("ROUTER_LOAD_REFRESH", util.DefaultLoadRecalc)
	localWeight    := util.GetEnvFloat("ROUTER_LOCAL_PENDING_WEIGHT", util.DefaultLocalWeight)

	client := &http.Client{Timeout: scrapeTimeout}

	// ── swap this one line to change strategy ──────────────────────────
	var s selector.Selector = selector.NewQueueAware(localWeight)
	// ───────────────────────────────────────────────────────────────────

	discovery.StartBackgroundRefresh(client, s, refreshInterval)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		proxy.Handle(s, w, r)
	})

	addr := fmt.Sprintf(":%d", routerPort)
	log.Printf("router listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func init() { metrics.Register() }