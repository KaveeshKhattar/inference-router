package index

import (
	"log"

	"router/prepare"
)

// BlockIndex maps cumulative block hashes to the pods that hold them.
// Sharded by Fibonacci hash of the block key to reduce lock contention.
type BlockIndex struct {
	shards []*shard
	pods   *PodRegistry
}

func NewBlockIndex() *BlockIndex {
	shards := make([]*shard, NumShards)
	for i := range shards {
		shards[i] = newShard()
	}
	return &BlockIndex{
		shards: shards,
		pods:   NewPodRegistry(),
	}
}

func (idx *BlockIndex) Pods() *PodRegistry {
	return idx.pods
}

// RegisterBlocks records that podURL holds every hash in the chain.
// Synthetic stand-in for real vLLM cache events until Layer 10 wiring.
func (idx *BlockIndex) RegisterBlocks(podURL string, hashes []prepare.BlockHash) {
	pod := idx.pods.Ensure(podURL)
	if pod < 0 {
		log.Printf("index: pod limit reached, skipping register for %s", podURL)
		return
	}
	for _, h := range hashes {
		idx.set(pod, h)
	}
}

func (idx *BlockIndex) set(pod int, h prepare.BlockHash) {
	s := idx.shards[shardFor(h)]
	s.mu.Lock()
	s.data[h] = s.data[h].Set(pod)
	s.mu.Unlock()
}

// Has reports whether pod holds the given cumulative block hash.
func (idx *BlockIndex) Has(pod int, h prepare.BlockHash) bool {
	s := idx.shards[shardFor(h)]
	s.mu.RLock()
	bm := s.data[h]
	s.mu.RUnlock()
	return bm.Has(pod)
}

// PrefixLenForPod returns how many leading blocks of query pod already holds.
// Uses binary search with O(1) checks: cumulative hash[i] implies prefix 0..i.
func (idx *BlockIndex) PrefixLenForPod(pod int, query []prepare.BlockHash) int {
	if len(query) == 0 {
		return 0
	}

	lo, hi := 0, len(query)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if idx.Has(pod, query[mid-1]) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// PrefixLens returns matched prefix length per registered pod index.
// Result[i] is the number of leading query blocks held by pod i.
func (idx *BlockIndex) PrefixLens(query []prepare.BlockHash) []int {
	n := idx.pods.Len()
	if n == 0 {
		return nil
	}
	out := make([]int, n)
	for pod := 0; pod < n; pod++ {
		out[pod] = idx.PrefixLenForPod(pod, query)
	}
	return out
}

// BestPod returns the pod index with the longest prefix match.
func (idx *BlockIndex) BestPod(query []prepare.BlockHash) (pod int, matched int) {
	lens := idx.PrefixLens(query)
	for i, n := range lens {
		if n > matched {
			matched = n
			pod = i
		}
	}
	return pod, matched
}
