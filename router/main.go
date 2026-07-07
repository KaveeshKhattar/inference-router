package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"router/admission"
	"router/discovery"
	"router/execute"
	"router/index"
	"router/metrics"
	"router/proxy"
	"router/selector"
	"router/util"
	"router/workflow"
)

func pdEnabled() bool {
	v := util.GetEnvString("ROUTER_PD_ENABLED", "")
	return v == "1" || v == "true"
}

func main() {
	metrics.Register()
	admission.RegisterMetrics()

	client := &http.Client{
		Timeout: util.GetEnvDuration("ROUTER_SCRAPE_TIMEOUT", util.DefaultScrapeTimeout),
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	blockIndex := index.NewBlockIndex()
	cacheWeight := util.GetEnvFloat("ROUTER_SCORE_CACHE_WEIGHT", 0.7)
	loadWeight := util.GetEnvFloat("ROUTER_SCORE_LOAD_WEIGHT", 0.3)
	refresh := util.GetEnvDuration("ROUTER_LOAD_REFRESH", util.DefaultLoadRecalc)

	admCfg := admission.ConfigFromEnv()
	if admCfg.Enabled {
		log.Printf("admission enabled pod_capacity=%d queue=%d codel_target=%s",
			admCfg.PodCapacity, admCfg.QueueCapacity, admCfg.TargetDelay)
	}
	adm := admission.NewController(admCfg)

	var (
		s       selector.Selector
		pdOpts  *proxy.PDOptions
		timeout = 3 * time.Minute // P/D needs longer for prefill + decode
	)

	if pdEnabled() {
		prefillSel := selector.NewCacheAware(blockIndex, cacheWeight, loadWeight)
		decodeSel := selector.NewCacheAware(blockIndex, cacheWeight, loadWeight)
		discovery.StartDualPoolRefresh(client, prefillSel, decodeSel, refresh)

		wfCfg := workflow.DefaultConfig()
		wfCfg.CacheWeight = util.GetEnvFloat("ROUTER_PD_DECODE_CACHE_WEIGHT", wfCfg.CacheWeight)
		wfCfg.LoadWeight = util.GetEnvFloat("ROUTER_PD_DECODE_LOAD_WEIGHT", wfCfg.LoadWeight)
		wfCfg.AffinityWeight = util.GetEnvFloat("ROUTER_PD_DECODE_AFFINITY_WEIGHT", wfCfg.AffinityWeight)

		pdOpts = &proxy.PDOptions{
			Workflow: workflow.NewPDWorkflow(prefillSel, decodeSel, blockIndex, wfCfg),
			Executor: execute.NewExecutor(timeout),
			Prefill:  prefillSel,
			Decode:   decodeSel,
		}
		s = prefillSel // fallback selector for health; proxy uses pd path
		log.Printf("router mode=disaggregated-prefill-decode decode_weights cache=%.2f load=%.2f affinity=%.2f",
			wfCfg.CacheWeight, wfCfg.LoadWeight, wfCfg.AffinityWeight)
	} else {
		strategy := util.GetEnvString("ROUTER_STRATEGY", "cache-aware")
		switch strategy {
		case "round-robin":
			s = selector.NewRoundRobin()
		case "queue-aware":
			s = selector.NewTokenAware()
		case "cache-aware":
			s = selector.NewCacheAware(blockIndex, cacheWeight, loadWeight)
		default:
			log.Printf("unknown ROUTER_STRATEGY=%q, using cache-aware", strategy)
			s = selector.NewCacheAware(blockIndex, cacheWeight, loadWeight)
			strategy = "cache-aware"
		}
		log.Printf("router strategy=%s cache_weight=%.2f load_weight=%.2f", strategy, cacheWeight, loadWeight)
		discovery.StartBackgroundRefresh(client, s, refresh)
		timeout = 3 * time.Second
	}

	handler := proxy.NewHandler(s, blockIndex, adm, pdOpts, timeout)

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
