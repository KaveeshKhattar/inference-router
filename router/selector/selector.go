package selector

import "router/discovery"

// Selector picks a replica for each incoming request.
// Implementations must be safe for concurrent use.
type Selector interface {
	// Update is called by the background scraper on every refresh tick.
	// The selector owns replica state after this call.
	Update(healths []discovery.ReplicaHealth)

	// Pick returns the chosen replica URL and a load score for logging.
	// Returns ("", 0) when no healthy replica is available.
	Pick() (url string, score float64)
	
	OnRequestStart(replica string, tokens float64)
	OnRequestFinish(replica string, tokens float64)
}