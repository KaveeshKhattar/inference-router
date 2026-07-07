package selector

import "router/index"

// CacheAffinityScorer prefers replicas that hold longer prefixes of the query chain.
type CacheAffinityScorer struct {
	Index *index.BlockIndex
}

func NewCacheAffinityScorer(idx *index.BlockIndex) *CacheAffinityScorer {
	return &CacheAffinityScorer{Index: idx}
}

func (c *CacheAffinityScorer) Score(sc ScoringContext) []float64 {
	n := len(sc.Candidates)
	scores := make([]float64, n)
	if n == 0 || sc.Request == nil {
		return scores
	}

	query := sc.Request.BlockHashes
	if len(query) == 0 {
		return scores
	}

	denom := float64(len(query))
	for i, cand := range sc.Candidates {
		pod := cand.PodIndex
		if pod < 0 {
			if idx, ok := c.Index.Pods().Index(cand.URL); ok {
				pod = idx
			} else {
				continue
			}
		}
		matched := c.Index.PrefixLenForPod(pod, query)
		scores[i] = float64(matched) / denom
	}
	return scores
}
