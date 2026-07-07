package selector

import (
	"errors"
	"testing"

	"router/discovery"
	"router/index"
	"router/prepare"
)

func TestLoadScorerPrefersLowLoad(t *testing.T) {
	sc := ScoringContext{
		Candidates: []Candidate{
			{URL: "a", TokenLoad: 100},
			{URL: "b", TokenLoad: 10},
			{URL: "c", TokenLoad: 50},
		},
	}
	scores := NewLoadScorer().Score(sc)
	if MaxScorePick(scores) != 1 {
		t.Fatalf("scores=%v, want pod b (index 1) to win", scores)
	}
}

func TestCacheAffinityScorerPrefersLongerPrefix(t *testing.T) {
	idx := index.NewBlockIndex()
	pipeline := prepare.NewDefaultPipeline()

	ctxA := &prepare.RequestContext{PromptText: "shared-prefix-" + string(make([]byte, 200)) + " tail-a"}
	ctxB := &prepare.RequestContext{PromptText: "shared-prefix-" + string(make([]byte, 200)) + " tail-b"}
	if err := pipeline.Prepare(ctxA); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Prepare(ctxB); err != nil {
		t.Fatal(err)
	}

	urlA := "http://pod-a:8000"
	urlB := "http://pod-b:8000"
	idx.RegisterBlocks(urlA, ctxA.BlockHashes)
	idx.RegisterBlocks(urlB, ctxB.BlockHashes[:len(ctxB.BlockHashes)/2])

	podA, _ := idx.Pods().Index(urlA)
	podB, _ := idx.Pods().Index(urlB)

	sc := ScoringContext{
		Request: ctxB,
		Candidates: []Candidate{
			{URL: urlA, PodIndex: podA, Healthy: true},
			{URL: urlB, PodIndex: podB, Healthy: true},
		},
	}

	scores := NewCacheAffinityScorer(idx).Score(sc)
	if scores[0] <= scores[1] {
		t.Fatalf("pod A should beat pod B on cache score: %v vs %v", scores[0], scores[1])
	}
}

func TestWeightedScorerCombinesSignals(t *testing.T) {
	idx := index.NewBlockIndex()
	pipeline := prepare.NewDefaultPipeline()
	ctx := &prepare.RequestContext{PromptText: "system prompt " + string(make([]byte, 300))}
	if err := pipeline.Prepare(ctx); err != nil {
		t.Fatal(err)
	}

	urlHot := "http://hot:8000"
	urlCold := "http://cold:8000"
	idx.RegisterBlocks(urlHot, ctx.BlockHashes)
	idx.Pods().Ensure(urlCold)

	podHot, _ := idx.Pods().Index(urlHot)
	podCold, _ := idx.Pods().Index(urlCold)

	scorer := NewWeightedScorer(
		[]Scorer{NewCacheAffinityScorer(idx), NewLoadScorer()},
		[]float64{0.9, 0.1},
	)

	// Cold pod has much lower load but no cache; hot pod has full cache but high load.
	sc := ScoringContext{
		Request: ctx,
		Candidates: []Candidate{
			{URL: urlHot, PodIndex: podHot, TokenLoad: 5000},
			{URL: urlCold, PodIndex: podCold, TokenLoad: 10},
		},
	}
	scores := scorer.Score(sc)
	if MaxScorePick(scores) != 0 {
		t.Fatalf("cache-heavy weight should pick hot pod: scores=%v", scores)
	}
}

func TestComposableSelectorPicksHealthy(t *testing.T) {
	idx := index.NewBlockIndex()
	sel := NewCacheAware(idx, 0.7, 0.3)
	sel.Update([]discovery.ReplicaHealth{
		{URL: "http://a:8000"},
		{URL: "http://b:8000", Error: errors.New("down")},
	})

	url, score := sel.Pick(&prepare.RequestContext{PromptText: "hello"})
	if url != "http://a:8000" || score < 0 {
		t.Fatalf("got url=%q score=%f", url, score)
	}
}
