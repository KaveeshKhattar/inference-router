package workflow

import (
	"errors"
	"net/url"

	"router/index"
	"router/prepare"
	"router/selector"
)

var ErrNoPrefillPod = errors.New("workflow: no prefill pod available")
var ErrNoDecodePod = errors.New("workflow: no decode pod available")

// Config tunes decode-pass scoring weights.
type Config struct {
	CacheWeight    float64
	LoadWeight     float64
	AffinityWeight float64
}

func DefaultConfig() Config {
	return Config{
		CacheWeight:    0.5,
		LoadWeight:     0.3,
		AffinityWeight: 0.2,
	}
}

// PDWorkflow runs prefill then decode selection against a shared RequestContext.
type PDWorkflow struct {
	prefill *selector.ComposableSelector
	decode  *selector.ComposableSelector
	index   *index.BlockIndex
	cfg     Config
}

func NewPDWorkflow(
	prefill, decode *selector.ComposableSelector,
	idx *index.BlockIndex,
	cfg Config,
) *PDWorkflow {
	return &PDWorkflow{
		prefill: prefill,
		decode:  decode,
		index:   idx,
		cfg:     cfg,
	}
}

// Select fills PrefillURL and DecodeURL on ctx.
func (w *PDWorkflow) Select(ctx *prepare.RequestContext) error {
	prefillURL, prefillScore := w.prefill.Pick(ctx)
	if prefillURL == "" {
		return ErrNoPrefillPod
	}
	ctx.PrefillURL = prefillURL
	ctx.PrefillScore = prefillScore
	ctx.PrefillHostPort = hostPort(prefillURL)

	decodeScorer := selector.NewWeightedScorer(
		[]selector.Scorer{
			selector.NewCacheAffinityScorer(w.index),
			selector.NewLoadScorer(),
			selector.NewNearPrefillScorer(prefillURL),
		},
		[]float64{w.cfg.CacheWeight, w.cfg.LoadWeight, w.cfg.AffinityWeight},
	)

	decodeURL, decodeScore := w.decode.PickWithScorer(ctx, decodeScorer)
	if decodeURL == "" {
		return ErrNoDecodePod
	}
	ctx.DecodeURL = decodeURL
	ctx.DecodeScore = decodeScore
	return nil
}

func hostPort(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Host
}
