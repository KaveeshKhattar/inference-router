package selector

// LoadScorer prefers replicas with lower in-flight token load.
// Scores are normalized to [0,1] across the candidate set.
type LoadScorer struct{}

func NewLoadScorer() *LoadScorer {
	return &LoadScorer{}
}

func (l *LoadScorer) Score(sc ScoringContext) []float64 {
	n := len(sc.Candidates)
	scores := make([]float64, n)
	if n == 0 {
		return scores
	}

	maxLoad := 0.0
	for _, c := range sc.Candidates {
		if c.TokenLoad > maxLoad {
			maxLoad = c.TokenLoad
		}
	}

	for i, c := range sc.Candidates {
		if maxLoad == 0 {
			scores[i] = 1.0
			continue
		}
		scores[i] = 1.0 - (c.TokenLoad / maxLoad)
	}
	return scores
}
