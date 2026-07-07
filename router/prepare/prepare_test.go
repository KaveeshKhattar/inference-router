package prepare

import (
	"strings"
	"testing"
)

func TestBlockHashPrefixSharing(t *testing.T) {
	// One full 16-token block = 64 chars with the chars/4 heuristic.
	charsPerBlock := DefaultBlockSize * CharsPerToken
	shared := strings.Repeat("s", charsPerBlock)

	pipeline := NewDefaultPipeline()

	ctxA := &RequestContext{PromptText: shared + " Request A tail content here."}
	ctxB := &RequestContext{PromptText: shared + " Request B different tail."}

	if err := pipeline.Prepare(ctxA); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Prepare(ctxB); err != nil {
		t.Fatal(err)
	}

	if len(ctxA.BlockHashes) < 2 || len(ctxB.BlockHashes) < 2 {
		t.Fatalf("expected at least 2 blocks, got A=%d B=%d", len(ctxA.BlockHashes), len(ctxB.BlockHashes))
	}

	// First block is entirely within the shared prefix — hashes must match.
	if ctxA.BlockHashes[0] != ctxB.BlockHashes[0] {
		t.Errorf("block 0 diverged:\n  A=%s\n  B=%s",
			FormatBlockHash(ctxA.BlockHashes[0]), FormatBlockHash(ctxB.BlockHashes[0]))
	}

	// Second block spans the divergence point — hashes must differ.
	if ctxA.BlockHashes[1] == ctxB.BlockHashes[1] {
		t.Errorf("block 1 should diverge but both are %s", FormatBlockHash(ctxA.BlockHashes[1]))
	}
}

func TestBlockHashIdenticalPrompts(t *testing.T) {
	pipeline := NewDefaultPipeline()
	prompt := "Same prompt repeated for cache affinity testing purposes."

	ctx1 := &RequestContext{PromptText: prompt}
	ctx2 := &RequestContext{PromptText: prompt}

	if err := pipeline.Prepare(ctx1); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Prepare(ctx2); err != nil {
		t.Fatal(err)
	}

	if len(ctx1.BlockHashes) != len(ctx2.BlockHashes) {
		t.Fatalf("block count mismatch: %d vs %d", len(ctx1.BlockHashes), len(ctx2.BlockHashes))
	}
	for i := range ctx1.BlockHashes {
		if ctx1.BlockHashes[i] != ctx2.BlockHashes[i] {
			t.Errorf("block %d differs: %s vs %s", i,
				FormatBlockHash(ctx1.BlockHashes[i]), FormatBlockHash(ctx2.BlockHashes[i]))
		}
	}
}

func TestTokenizeBlockBoundaries(t *testing.T) {
	p := NewTokenizePreparer(16)
	ctx := &RequestContext{PromptText: "abcd"} // 4 chars, less than one 64-char block
	if err := p.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ctx.TokenBlocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(ctx.TokenBlocks))
	}
	if ctx.TokenBlocks[0] != "abcd" {
		t.Errorf("block content = %q, want %q", ctx.TokenBlocks[0], "abcd")
	}

	// Exactly one block worth of chars.
	ctx = &RequestContext{PromptText: string(make([]byte, 64))}
	if err := p.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ctx.TokenBlocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(ctx.TokenBlocks))
	}

	// One char past one block → two blocks.
	ctx = &RequestContext{PromptText: string(make([]byte, 65))}
	if err := p.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ctx.TokenBlocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(ctx.TokenBlocks))
	}
}

func TestCumulativeHashProperty(t *testing.T) {
	// Changing block 0 must change every subsequent hash in the chain.
	pipeline := NewDefaultPipeline()

	base := &RequestContext{PromptText: "alpha-beta-gamma-delta " + string(make([]byte, 40))}
	altered := &RequestContext{PromptText: "ALPHA-beta-gamma-delta " + string(make([]byte, 40))}

	if err := pipeline.Prepare(base); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Prepare(altered); err != nil {
		t.Fatal(err)
	}

	for i := range base.BlockHashes {
		if base.BlockHashes[i] == altered.BlockHashes[i] {
			t.Errorf("block %d hash should differ after prefix change", i)
		}
	}
}
