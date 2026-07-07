package admission

import (
	"context"
	"sync"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{
		Enabled:       true,
		PodCapacity:   2,
		QueueCapacity: 8,
		TargetDelay:   20 * time.Millisecond,
		Interval:      50 * time.Millisecond,
		MaxWait:       2 * time.Second,
	}
}

func TestDirectAdmitWithinCapacity(t *testing.T) {
	c := NewController(testConfig())
	release, err := c.Acquire(context.Background(), "http://pod-a:8000", PriorityHigh)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestBlocksWhenPodFull(t *testing.T) {
	c := NewController(testConfig())
	pod := "http://pod-a:8000"

	r1, _ := c.Acquire(context.Background(), pod, PriorityLow)
	r2, _ := c.Acquire(context.Background(), pod, PriorityLow)

	done := make(chan error, 1)
	go func() {
		_, err := c.Acquire(context.Background(), pod, PriorityLow)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("third acquire should block, got err=%v", err)
	case <-time.After(30 * time.Millisecond):
	}

	r1()
	<-done
	r2()
}

func TestDebtBlocksLowPriority(t *testing.T) {
	c := NewController(testConfig())
	pod := "http://pod-a:8000"

	// Fill pod with low-priority work.
	r1, _ := c.Acquire(context.Background(), pod, PriorityLow)
	r2, _ := c.Acquire(context.Background(), pod, PriorityLow)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := c.Acquire(context.Background(), pod, PriorityHigh)
		if err != nil {
			t.Errorf("high priority should eventually succeed: %v", err)
		}
	}()

	time.Sleep(20 * time.Millisecond)
	if c.Debt() == 0 {
		t.Fatal("expected debt after high priority queued behind low traffic")
	}

	r1()
	wg.Wait()
	r2()

	if c.Debt() != 0 {
		t.Fatalf("debt after high served = %d, want 0", c.Debt())
	}
}

func TestCoDelShedsSlowWaiters(t *testing.T) {
	cfg := testConfig()
	cfg.TargetDelay = 5 * time.Millisecond
	cfg.Interval = 10 * time.Millisecond
	cfg.MaxWait = 500 * time.Millisecond
	c := NewController(cfg)
	pod := "http://pod-a:8000"

	r1, _ := c.Acquire(context.Background(), pod, PriorityLow)
	r2, _ := c.Acquire(context.Background(), pod, PriorityLow)

	_, err := c.Acquire(context.Background(), pod, PriorityLow)
	if err == nil {
		t.Fatal("expected CoDel to shed queued request")
	}

	r1()
	r2()
}

func TestDisabledControllerNoOps(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	c := NewController(cfg)
	release, err := c.Acquire(context.Background(), "pod", PriorityLow)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestParsePriority(t *testing.T) {
	if ParsePriority("high") != PriorityHigh {
		t.Fatal("expected high")
	}
	if ParsePriority("unknown") != PriorityLow {
		t.Fatal("expected low default")
	}
}
