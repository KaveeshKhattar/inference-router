package selector

import (
	"router/discovery"
	"router/prepare"
)

// Selector picks a replica for each incoming request.
// Implementations must be safe for concurrent use.
type Selector interface {
	// Update is called by the background scraper on every refresh tick.
	Update(healths []discovery.ReplicaHealth)

	// Pick returns the chosen replica URL and a score for logging.
	// ctx carries prepared block hashes; nil is valid for load-only strategies.
	Pick(ctx *prepare.RequestContext) (url string, score float64)

	OnRequestStart(replica string, tokens float64)
	OnRequestFinish(replica string, tokens float64)
}