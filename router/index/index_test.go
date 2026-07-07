package index

import (
	"crypto/sha256"
	"testing"

	"router/prepare"
)

func makeHash(seed byte) prepare.BlockHash {
	return prepare.BlockHash(sha256.Sum256([]byte{seed}))
}

func makeChain(n int) []prepare.BlockHash {
	out := make([]prepare.BlockHash, n)
	var prev prepare.BlockHash
	for i := range out {
		d := sha256.New()
		if i > 0 {
			d.Write(prev[:])
		}
		d.Write([]byte{byte(i)})
		copy(out[i][:], d.Sum(nil))
		prev = out[i]
	}
	return out
}

func TestRegisterAndHas(t *testing.T) {
	idx := NewBlockIndex()
	h := makeHash(1)

	idx.RegisterBlocks("http://10.0.0.1:8000", []prepare.BlockHash{h})
	pod, ok := idx.Pods().Index("http://10.0.0.1:8000")
	if !ok || pod != 0 {
		t.Fatalf("pod index = %d ok=%v, want 0 true", pod, ok)
	}
	if !idx.Has(0, h) {
		t.Fatal("expected pod 0 to have block")
	}
	if idx.Has(1, h) {
		t.Fatal("pod 1 should not have block")
	}
}

func TestPrefixLenBinarySearch(t *testing.T) {
	idx := NewBlockIndex()
	chain := makeChain(8)

	// Pod 0 holds blocks 0..4 (5 blocks).
	idx.RegisterBlocks("http://pod-a:8000", chain[:5])
	// Pod 1 holds the full chain.
	idx.RegisterBlocks("http://pod-b:8000", chain)

	if got := idx.PrefixLenForPod(0, chain); got != 5 {
		t.Errorf("pod 0 prefix = %d, want 5", got)
	}
	if got := idx.PrefixLenForPod(1, chain); got != 8 {
		t.Errorf("pod 1 prefix = %d, want 8", got)
	}

	// Shared-prefix scenario: query extends chain with two new blocks.
	extended := append(chain, makeChain(2)...)
	extended[8] = makeHash(100)
	extended[9] = makeHash(101)

	if got := idx.PrefixLenForPod(1, extended); got != 8 {
		t.Errorf("pod 1 prefix on extended = %d, want 8", got)
	}
}

func TestPrefixLensAllPods(t *testing.T) {
	idx := NewBlockIndex()
	chain := makeChain(4)
	idx.RegisterBlocks("http://a:8000", chain[:2])
	idx.RegisterBlocks("http://b:8000", chain[:4])

	lens := idx.PrefixLens(chain)
	if len(lens) != 2 {
		t.Fatalf("got %d pods, want 2", len(lens))
	}
	if lens[0] != 2 || lens[1] != 4 {
		t.Errorf("lens = %v, want [2 4]", lens)
	}

	pod, matched := idx.BestPod(chain)
	if pod != 1 || matched != 4 {
		t.Errorf("best = pod %d matched %d, want pod 1 matched 4", pod, matched)
	}
}

func TestShardSpread(t *testing.T) {
	counts := make([]int, NumShards)
	for i := 0; i < 1000; i++ {
		h := makeHash(byte(i))
		counts[shardFor(h)]++
	}
	empty := 0
	for _, c := range counts {
		if c == 0 {
			empty++
		}
	}
	if empty > NumShards/2 {
		t.Errorf("poor shard spread: %d/%d shards empty", empty, NumShards)
	}
}

func TestHostBitmap(t *testing.T) {
	var bm HostBitmap
	if bm.Count() != 0 {
		t.Fatal("empty bitmap should count 0")
	}
	bm = bm.Set(0).Set(3).Set(63)
	if bm.Count() != 3 {
		t.Fatalf("count = %d, want 3", bm.Count())
	}
	if !bm.Has(3) || bm.Has(4) {
		t.Fatal("bit test failed")
	}
}

func TestIntegrationWithPreparePipeline(t *testing.T) {
	pipeline := prepare.NewDefaultPipeline()
	idx := NewBlockIndex()

	shared := "You are a helpful assistant. " + string(make([]byte, 64-30))
	reqA := &prepare.RequestContext{PromptText: shared + " tail A"}
	reqB := &prepare.RequestContext{PromptText: shared + " tail B"}

	if err := pipeline.Prepare(reqA); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Prepare(reqB); err != nil {
		t.Fatal(err)
	}

	idx.RegisterBlocks("http://pod-0:8000", reqA.BlockHashes)

	// Pod 0 should fully match A and partially match B (shared first block).
	lensA := idx.PrefixLenForPod(0, reqA.BlockHashes)
	lensB := idx.PrefixLenForPod(0, reqB.BlockHashes)
	if lensA != len(reqA.BlockHashes) {
		t.Errorf("full match A = %d, want %d", lensA, len(reqA.BlockHashes))
	}
	if lensB < 1 {
		t.Errorf("partial match B = %d, want at least 1 shared block", lensB)
	}
	if reqA.BlockHashes[0] == reqB.BlockHashes[0] && lensB < 1 {
		t.Fatal("shared first block should produce prefix >= 1")
	}
}
