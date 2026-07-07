package index

import (
	"encoding/binary"
	"sync"

	"router/prepare"
)

const (
	// NumShards spreads the block map across independent locks.
	NumShards = 16
	// goldenRatio64 is 2^64 / φ, used for Fibonacci hashing.
	goldenRatio64 = uint64(11400714819323198485)
)

type shard struct {
	mu   sync.RWMutex
	data map[prepare.BlockHash]HostBitmap
}

func newShard() *shard {
	return &shard{data: make(map[prepare.BlockHash]HostBitmap)}
}

func shardFor(h prepare.BlockHash) int {
	v := binary.BigEndian.Uint64(h[:8])
	// Top 4 bits select one of 16 shards.
	return int((v * goldenRatio64) >> 60)
}
