package selector

import "router/discovery"

// Selector picks a replica URL given the current live replica list
// and an optional incoming token estimate. Implementations must be
// safe for concurrent use.
type Selector interface {
	// Update refreshes internal state from the latest health scrape.
	Update(healths []discovery.ReplicaHealth)

	// Pick returns the chosen replica URL and an opaque score/load value
	// for logging. Returns ("", 0) when no healthy replica is available.
	Pick(replicas []string, incomingTokens int64) (chosen string, score float64)
}