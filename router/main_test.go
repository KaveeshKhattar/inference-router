package main

// import (
// 	"errors"
// 	"testing"
// )

// func TestRoundRobinCyclesAcrossHealthyReplicas(t *testing.T) {
// 	selector := NewRoundRobinSelector()
// 	healths := []ReplicaHealth{
// 		{URL: "http://replica-a:8000"},
// 		{URL: "http://replica-b:8000"},
// 		{URL: "http://replica-c:8000"},
// 	}

// 	want := []string{
// 		"http://replica-a:8000",
// 		"http://replica-b:8000",
// 		"http://replica-c:8000",
// 		"http://replica-a:8000",
// 		"http://replica-b:8000",
// 	}

// 	for i, expected := range want {
// 		got := selector.Pick(healths)
// 		if got != expected {
// 			t.Fatalf("pick #%d mismatch: got=%s want=%s", i+1, got, expected)
// 		}
// 	}
// }

// func TestRoundRobinSkipsUnhealthyReplicas(t *testing.T) {
// 	selector := NewRoundRobinSelector()
// 	healths := []ReplicaHealth{
// 		{URL: "http://replica-a:8000", Error: errors.New("scrape failed")},
// 		{URL: "http://replica-b:8000"},
// 		{URL: "http://replica-c:8000", Error: errors.New("scrape failed")},
// 		{URL: "http://replica-d:8000"},
// 	}

// 	want := []string{
// 		"http://replica-b:8000",
// 		"http://replica-d:8000",
// 		"http://replica-b:8000",
// 		"http://replica-d:8000",
// 	}

// 	for i, expected := range want {
// 		got := selector.Pick(healths)
// 		if got != expected {
// 			t.Fatalf("pick #%d mismatch: got=%s want=%s", i+1, got, expected)
// 		}
// 	}
// }

// func TestRoundRobinReturnsEmptyWhenNoHealthyReplicas(t *testing.T) {
// 	selector := NewRoundRobinSelector()

// 	healths := []ReplicaHealth{
// 		{URL: "http://replica-a:8000", Error: errors.New("scrape failed")},
// 		{URL: "http://replica-b:8000", Error: errors.New("scrape failed")},
// 	}

// 	if got := selector.Pick(healths); got != "" {
// 		t.Fatalf("expected empty selection, got=%s", got)
// 	}
// }
