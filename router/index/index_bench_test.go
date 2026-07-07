package index

import (
	"fmt"
	"testing"

	"router/prepare"
)

func populateIndex(pods, blocksPerPod int) (*BlockIndex, []prepare.BlockHash) {
	idx := NewBlockIndex()
	query := makeChain(32)

	for p := 0; p < pods; p++ {
		// Each pod holds a random-ish prefix length: pod p holds (p*blocksPerPod/pods) blocks.
		held := (p + 1) * len(query) / pods
		if held < 1 {
			held = 1
		}
		if held > len(query) {
			held = len(query)
		}
		url := fmt.Sprintf("http://10.0.0.%d:8000", p+1)
		idx.RegisterBlocks(url, query[:held])
	}
	return idx, query
}

func BenchmarkPrefixLenSinglePod(b *testing.B) {
	idx, query := populateIndex(64, 32)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.PrefixLenForPod(31, query)
	}
}

func BenchmarkPrefixLensAllPods(b *testing.B) {
	idx, query := populateIndex(64, 32)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.PrefixLens(query)
	}
}

func BenchmarkHasLookup(b *testing.B) {
	idx, query := populateIndex(64, 32)
	h := query[16]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Has(31, h)
	}
}

// BenchmarkPrefixQuery64x32 is the headline number for the KV-cache talk:
// 64 pods, 32-block query chain, full prefix resolution.
func BenchmarkPrefixQuery64x32(b *testing.B) {
	idx, query := populateIndex(64, 32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.PrefixLens(query)
	}
}
