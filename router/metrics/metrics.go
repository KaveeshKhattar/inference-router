package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	RouterRequestsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "router_requests_total",
		Help: "Total requests handled by the router",
	})
	RouterTargetRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "router_target_requests_total",
		Help: "Total requests routed to each target replica",
	}, []string{"replica"})
	RouterInflightRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "router_inflight_requests",
		Help: "Current in-flight requests proxied by the router",
	})
	RouterQueueLength = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "router_queue_length",
		Help: "Total upstream queue depth observed across healthy replicas",
	})
	RouterQueueLengthTokens = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "router_queue_length_tokens",
		Help: "Estimated token-weighted in-flight queue depth at the router",
	})
	RouterTargetQueueLengthTokens = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "router_target_queue_length_tokens",
		Help: "Estimated token-weighted in-flight queue depth by target replica",
	}, []string{"replica"})
	RoutingLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "router_decision_seconds",
		Help:    "Time taken to compute routing decision",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
	})
)

func Register() {
	prometheus.MustRegister(
		RouterRequestsTotal,
		RouterTargetRequestsTotal,
		RouterInflightRequests,
		RouterQueueLength,
		RouterQueueLengthTokens,
		RouterTargetQueueLengthTokens,
		RoutingLatency,
	)
}