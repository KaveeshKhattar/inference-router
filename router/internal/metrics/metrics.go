package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
)

var (
    RoutedRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
            Name: "router_requests_total",
            Help: "Total routed requests",
        },
        []string{"replica"},
    )
	RoutingLatency = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "router_decision_seconds",
            Help:    "Time taken to compute routing decision",
            Buckets: prometheus.DefBuckets, // Always include buckets for histograms
        },
    )
)

func Init() {
    prometheus.MustRegister(RoutedRequests)
    prometheus.MustRegister(RoutingLatency)
}