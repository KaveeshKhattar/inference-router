package workflow

import (
	"testing"

	"router/discovery"
	"router/index"
	"router/prepare"
	"router/selector"
)

func TestPDWorkflowSelect(t *testing.T) {
	idx := index.NewBlockIndex()
	prefill := selector.NewCacheAware(idx, 0.7, 0.3)
	decode := selector.NewCacheAware(idx, 0.7, 0.3)

	prefill.Update([]discovery.ReplicaHealth{{URL: "http://10.0.0.1:8000"}})
	decode.Update([]discovery.ReplicaHealth{{URL: "http://10.0.0.2:8000"}})

	wf := NewPDWorkflow(prefill, decode, idx, DefaultConfig())
	ctx := &prepare.RequestContext{PromptText: "hello world"}
	if err := wf.Select(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.PrefillURL != "http://10.0.0.1:8000" {
		t.Fatalf("prefill=%q", ctx.PrefillURL)
	}
	if ctx.DecodeURL != "http://10.0.0.2:8000" {
		t.Fatalf("decode=%q", ctx.DecodeURL)
	}
	if ctx.PrefillHostPort != "10.0.0.1:8000" {
		t.Fatalf("hostport=%q", ctx.PrefillHostPort)
	}
}
