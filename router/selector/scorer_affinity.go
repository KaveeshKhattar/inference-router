package selector

import (
	"math"
	"net/url"
	"strconv"
	"strings"
)

// NearPrefillScorer prefers decode pods close to the chosen prefill pod.
// Uses last IP octet distance as a simple locality heuristic on kind clusters.
type NearPrefillScorer struct {
	PrefillURL string
}

func NewNearPrefillScorer(prefillURL string) *NearPrefillScorer {
	return &NearPrefillScorer{PrefillURL: prefillURL}
}

func (n *NearPrefillScorer) Score(sc ScoringContext) []float64 {
	scores := make([]float64, len(sc.Candidates))
	pOct := lastOctet(n.PrefillURL)
	for i, c := range sc.Candidates {
		d := math.Abs(float64(lastOctet(c.URL) - pOct))
		scores[i] = 1.0 - d/255.0
	}
	return scores
}

func lastOctet(rawURL string) int {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	parts := strings.Split(u.Hostname(), ".")
	if len(parts) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(parts[len(parts)-1])
	return n
}
