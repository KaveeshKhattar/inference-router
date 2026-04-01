package main

import (
	"math"
	"testing"
	"time"
)

func TestComputeDynamicWeightsOrdersByPressure(t *testing.T) {
	healths := []ReplicaHealth{
		{URL: "http://replica-a:8000", QueueDepth: 0, Running: 1, KVCache: 0.1},
		{URL: "http://replica-b:8000", QueueDepth: 8, Running: 8, KVCache: 0.9},
	}

	weights := computeDynamicWeights(healths)
	wa := weights["http://replica-a:8000"]
	wb := weights["http://replica-b:8000"]

	if wa >= wb {
		t.Fatalf("expected lower-pressure replica to receive lower weight, got a=%f b=%f", wa, wb)
	}
	if wa < minDynamicWeight || wa > maxDynamicWeight {
		t.Fatalf("weight out of bounds for replica-a: %f", wa)
	}
	if wb < minDynamicWeight || wb > maxDynamicWeight {
		t.Fatalf("weight out of bounds for replica-b: %f", wb)
	}
}

func TestComputeDynamicWeightsEqualPressureDefaultsToOne(t *testing.T) {
	healths := []ReplicaHealth{
		{URL: "http://replica-a:8000", QueueDepth: 1, Running: 1, KVCache: 0.2},
		{URL: "http://replica-b:8000", QueueDepth: 1, Running: 1, KVCache: 0.2},
	}

	weights := computeDynamicWeights(healths)
	for replica, w := range weights {
		if math.Abs(w-1.0) > 1e-9 {
			t.Fatalf("expected weight=1 for equal pressure, replica=%s got=%f", replica, w)
		}
	}
}

func TestPickBestUsesDynamicWeight(t *testing.T) {
	setReplicaWeights(map[string]float64{
		"http://replica-a:8000": 2.0,
		"http://replica-b:8000": 0.5,
	})
	defer setReplicaWeights(map[string]float64{})

	healths := []ReplicaHealth{
		{URL: "http://replica-a:8000", QueueDepth: 0, Running: 2, KVCache: 0},
		{URL: "http://replica-b:8000", QueueDepth: 0, Running: 2, KVCache: 0},
	}

	chosen := pickBest(healths)
	if chosen != "http://replica-b:8000" {
		t.Fatalf("expected weighted picker to choose replica-b, got %s", chosen)
	}
}

func TestBuildAndParseWeightSnapshotJSONRoundTrip(t *testing.T) {
	weights := map[string]float64{
		"http://replica-a:8000": 0.8,
		"http://replica-b:8000": 1.4,
	}

	raw, err := buildWeightSnapshotJSON(weights, time.Unix(1710000000, 0))
	if err != nil {
		t.Fatalf("buildWeightSnapshotJSON failed: %v", err)
	}

	parsed, err := parseWeightSnapshotJSON(string(raw))
	if err != nil {
		t.Fatalf("parseWeightSnapshotJSON failed: %v", err)
	}
	if len(parsed) != len(weights) {
		t.Fatalf("unexpected parsed map size: got=%d want=%d", len(parsed), len(weights))
	}

	for replica, want := range weights {
		got, ok := parsed[replica]
		if !ok {
			t.Fatalf("missing replica %s in parsed weights", replica)
		}
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("weight mismatch for %s: got=%f want=%f", replica, got, want)
		}
	}
}

func TestParseWeightSnapshotJSONRejectsInvalidJSON(t *testing.T) {
	if _, err := parseWeightSnapshotJSON("{this-is-not-json"); err == nil {
		t.Fatalf("expected parse error for invalid snapshot json")
	}
}
