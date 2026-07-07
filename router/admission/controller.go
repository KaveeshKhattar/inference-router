package admission

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Controller applies PGKeeper-style admission per replica pod.
type Controller struct {
	cfg Config

	mu   sync.Mutex
	pods map[string]*podGate
	debt int
	noop bool
}

func NewController(cfg Config) *Controller {
	if !cfg.Enabled {
		return &Controller{noop: true, cfg: cfg}
	}
	return &Controller{
		cfg:  cfg,
		pods: make(map[string]*podGate),
	}
}

func (c *Controller) Enabled() bool {
	return c.cfg.Enabled && !c.noop
}

// Acquire blocks until the request may proceed to the chosen pod, or returns an error.
func (c *Controller) Acquire(ctx context.Context, podURL string, priority Priority) (func(), error) {
	if c.noop {
		return func() {}, nil
	}

	gate := c.podFor(podURL)
	waitStart := time.Now()

	release, err := gate.acquire(ctx, priority, gateCallbacks{
		debtBlocked: c.debtBlocked,
		onHighQueued: func(hasLowAhead, lowInFlight bool) {
			if hasLowAhead || lowInFlight {
				c.addDebt(1)
			}
		},
		onAdmitted: func(p Priority) {
			if p == PriorityHigh {
				c.payDebt(1)
			}
			AdmissionWaitSeconds.Observe(time.Since(waitStart).Seconds())
		},
	})
	if err != nil {
		if err == ErrOverloaded {
			AdmissionRejectedTotal.WithLabelValues("overloaded").Inc()
		} else if err == ErrQueueFull {
			AdmissionRejectedTotal.WithLabelValues("queue_full").Inc()
		}
		return nil, err
	}

	return func() {
		release()
		c.kick(podURL)
	}, nil
}

func (c *Controller) debtBlocked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.debt > 0
}

func (c *Controller) kick(podURL string) {
	gate := c.podFor(podURL)
	gate.mu.Lock()
	gate.dispatchNextLocked(c.debtBlocked)
	queued := len(gate.queue)
	gate.mu.Unlock()
	AdmissionQueueDepth.Set(float64(queued))
}

func (c *Controller) podFor(url string) *podGate {
	c.mu.Lock()
	defer c.mu.Unlock()
	g, ok := c.pods[url]
	if !ok {
		g = newPodGate(c.cfg)
		c.pods[url] = g
	}
	return g
}

func (c *Controller) addDebt(n int) {
	c.mu.Lock()
	c.debt += n
	d := c.debt
	c.mu.Unlock()
	AdmissionDebt.Set(float64(d))
}

func (c *Controller) payDebt(n int) {
	c.mu.Lock()
	c.debt -= n
	if c.debt < 0 {
		c.debt = 0
	}
	d := c.debt
	c.mu.Unlock()
	AdmissionDebt.Set(float64(d))
}

func (c *Controller) Debt() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.debt
}

func (c *Controller) TotalQueued() int {
	if c.noop {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	total := 0
	for _, g := range c.pods {
		_, q := g.stats()
		total += q
	}
	return total
}

var (
	AdmissionRejectedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "router_admission_rejected_total",
		Help: "Requests rejected by admission control",
	}, []string{"reason"})

	AdmissionQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "router_admission_queue_depth",
		Help: "Queued requests waiting for pod capacity",
	})

	AdmissionDebt = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "router_admission_low_priority_debt",
		Help: "Outstanding debt owed by low-priority traffic to high-priority",
	})

	AdmissionWaitSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "router_admission_wait_seconds",
		Help:    "Time spent waiting for admission",
		Buckets: []float64{.001, .01, .05, .1, .25, .5, 1, 2, 5},
	})
)

func RegisterMetrics() {
	prometheus.MustRegister(
		AdmissionRejectedTotal,
		AdmissionQueueDepth,
		AdmissionDebt,
		AdmissionWaitSeconds,
	)
}
