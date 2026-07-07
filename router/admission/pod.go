package admission

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrOverloaded = errors.New("admission: overloaded")
	ErrQueueFull  = errors.New("admission: queue full")
)

type waiter struct {
	priority   Priority
	enqueuedAt time.Time
	done       chan struct{}
	err        error
}

type podGate struct {
	cfg Config

	mu          sync.Mutex
	inFlight    int
	inFlightLow int
	queue       []*waiter

	queueAboveSince time.Time
	dropping        bool
}

func newPodGate(cfg Config) *podGate {
	return &podGate{cfg: cfg}
}

type gateCallbacks struct {
	debtBlocked  func() bool
	onHighQueued func(hasLowAhead bool, lowInFlight bool)
	onAdmitted   func(priority Priority)
}

func (p *podGate) acquire(ctx context.Context, priority Priority, cb gateCallbacks) (func(), error) {
	p.mu.Lock()
	if p.canAdmitDirectLocked(priority, cb.debtBlocked()) {
		p.startLocked(priority)
		p.mu.Unlock()
		cb.onAdmitted(priority)
		return func() { p.release(priority, cb.debtBlocked) }, nil
	}

	if len(p.queue) >= p.cfg.QueueCapacity {
		p.mu.Unlock()
		return nil, ErrQueueFull
	}

	w := &waiter{
		priority:   priority,
		enqueuedAt: time.Now(),
		done:       make(chan struct{}),
	}
	p.queue = append(p.queue, w)
	if priority == PriorityHigh {
		cb.onHighQueued(p.hasLowWaiterLocked(), p.inFlightLow > 0)
	}
	p.runCoDelLocked(time.Now())
	p.mu.Unlock()

	waitCtx, cancel := context.WithTimeout(ctx, p.cfg.MaxWait)
	defer cancel()

	select {
	case <-w.done:
		if w.err != nil {
			return nil, w.err
		}
		cb.onAdmitted(w.priority)
		return func() { p.release(w.priority, cb.debtBlocked) }, nil
	case <-waitCtx.Done():
		p.mu.Lock()
		p.cancelWaiterLocked(w)
		p.mu.Unlock()
		return nil, ErrOverloaded
	}
}

func (p *podGate) canAdmitDirectLocked(priority Priority, debtBlocked bool) bool {
	if p.inFlight >= p.cfg.PodCapacity {
		return false
	}
	if priority == PriorityLow && debtBlocked {
		return false
	}
	return true
}

func (p *podGate) startLocked(priority Priority) {
	p.inFlight++
	if priority == PriorityLow {
		p.inFlightLow++
	}
}

func (p *podGate) finishLocked(priority Priority) {
	if p.inFlight > 0 {
		p.inFlight--
	}
	if priority == PriorityLow && p.inFlightLow > 0 {
		p.inFlightLow--
	}
}

func (p *podGate) release(priority Priority, debtBlocked func() bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finishLocked(priority)
	p.dispatchNextLocked(debtBlocked)
}

func (p *podGate) dispatchNextLocked(debtBlocked func() bool) {
	for len(p.queue) > 0 && p.inFlight < p.cfg.PodCapacity {
		idx := p.pickWaiterIndexLocked(debtBlocked)
		if idx < 0 {
			return
		}
		w := p.queue[idx]
		p.queue = append(p.queue[:idx], p.queue[idx+1:]...)
		if len(p.queue) == 0 {
			p.queueAboveSince = time.Time{}
			p.dropping = false
		}
		p.startLocked(w.priority)
		w.err = nil
		close(w.done)
	}
}

func (p *podGate) pickWaiterIndexLocked(debtBlocked func() bool) int {
	if len(p.queue) == 0 {
		return -1
	}

	for i := 0; i < len(p.queue); i++ {
		idx := i
		if p.dropping {
			idx = len(p.queue) - 1 - i
		}
		w := p.queue[idx]
		if w.priority == PriorityLow && debtBlocked() {
			continue
		}
		return idx
	}
	return -1
}

func (p *podGate) runCoDelLocked(now time.Time) {
	if len(p.queue) == 0 {
		p.queueAboveSince = time.Time{}
		p.dropping = false
		return
	}

	sojourn := now.Sub(p.queue[0].enqueuedAt)
	if sojourn < p.cfg.TargetDelay {
		p.queueAboveSince = time.Time{}
		p.dropping = false
		return
	}

	if p.queueAboveSince.IsZero() {
		p.queueAboveSince = now
		return
	}
	if now.Sub(p.queueAboveSince) < p.cfg.Interval {
		return
	}

	p.dropping = true
	for len(p.queue) > 0 {
		head := p.queue[0]
		if now.Sub(head.enqueuedAt) < p.cfg.TargetDelay {
			break
		}
		p.queue = p.queue[1:]
		head.err = ErrOverloaded
		close(head.done)
	}
}

func (p *podGate) cancelWaiterLocked(w *waiter) {
	for i, q := range p.queue {
		if q == w {
			p.queue = append(p.queue[:i], p.queue[i+1:]...)
			w.err = ErrOverloaded
			close(w.done)
			return
		}
	}
}

func (p *podGate) hasLowWaiterLocked() bool {
	for _, w := range p.queue {
		if w.priority == PriorityLow {
			return true
		}
	}
	return false
}

func (p *podGate) stats() (inFlight, queued int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inFlight, len(p.queue)
}
