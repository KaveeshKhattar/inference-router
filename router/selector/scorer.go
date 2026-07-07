package selector

import "router/prepare"

// Candidate is one routable replica at decision time.
type Candidate struct {
	URL       string
	PodIndex  int
	Healthy   bool
	TokenLoad float64
}

// ScoringContext carries request state and the filtered candidate set.
type ScoringContext struct {
	Request    *prepare.RequestContext
	Candidates []Candidate
}

// Scorer assigns a higher-is-better score to each candidate.
type Scorer interface {
	Score(sc ScoringContext) []float64
}

// WeightedScorer combines multiple scorers with explicit weights.
type WeightedScorer struct {
	Parts   []Scorer
	Weights []float64
}

func NewWeightedScorer(parts []Scorer, weights []float64) *WeightedScorer {
	if len(parts) != len(weights) {
		panic("selector: WeightedScorer parts/weights length mismatch")
	}
	return &WeightedScorer{Parts: parts, Weights: weights}
}

func (w *WeightedScorer) Score(sc ScoringContext) []float64 {
	n := len(sc.Candidates)
	if n == 0 || len(w.Parts) == 0 {
		return nil
	}

	combined := make([]float64, n)
	totalWeight := 0.0
	for i, part := range w.Parts {
		weight := w.Weights[i]
		if weight <= 0 {
			continue
		}
		totalWeight += weight
		partScores := part.Score(sc)
		for j := range combined {
			combined[j] += weight * partScores[j]
		}
	}
	if totalWeight > 0 {
		for i := range combined {
			combined[i] /= totalWeight
		}
	}
	return combined
}

// MaxScorePick returns the candidate index with the highest score.
func MaxScorePick(scores []float64) int {
	best := -1
	bestScore := -1.0
	for i, s := range scores {
		if s > bestScore {
			bestScore = s
			best = i
		}
	}
	return best
}
