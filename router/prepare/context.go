package prepare

// BlockHash is a cumulative hash of a prompt prefix up to and including one
// fixed-size token block. Comparable so it can key the Layer 9 block index.
type BlockHash [32]byte

// RequestContext holds per-request state produced by the Prepare pipeline.
type RequestContext struct {
	PromptText    string
	TokenEstimate float64
	// TokenBlocks holds approximate token chunks (blockSize tokens each).
	TokenBlocks []string
	BlockHashes []BlockHash

	// Layer 13: disaggregated prefill/decode routing decisions.
	PrefillURL    string
	DecodeURL     string
	PrefillScore  float64
	DecodeScore   float64
	PrefillHostPort string // host:port for x-prefiller-host-port header
}
