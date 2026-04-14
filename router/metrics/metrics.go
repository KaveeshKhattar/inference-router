package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	RouterRequestsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "router_requests_total",
		Help: "Total requests handled by the router",
	})

	RouterTargetRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "router_target_requests_total",
		Help: "Requests routed to each replica",
	}, []string{"replica"})

	RouterInflightRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "router_inflight_requests",
		Help: "Current in-flight requests",
	})

	RoutingLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "router_decision_seconds",
		Help:    "Time to compute routing decision",
		Buckets: []float64{.0001, .0005, .001, .005, .01},
	})
)

func Register() {
	prometheus.MustRegister(
		RouterRequestsTotal,
		RouterTargetRequestsTotal,
		RouterInflightRequests,
		RoutingLatency,
	)
}