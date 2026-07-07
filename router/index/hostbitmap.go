package index

// MaxPods is the fixed width of HostBitmap (one bit per pod).
const MaxPods = 64

// HostBitmap is a fixed-width bit vector: bit i set means pod i holds the block.
type HostBitmap uint64

func (b HostBitmap) Has(pod int) bool {
	if pod < 0 || pod >= MaxPods {
		return false
	}
	return b&(1<<uint(pod)) != 0
}

func (b HostBitmap) Set(pod int) HostBitmap {
	if pod < 0 || pod >= MaxPods {
		return b
	}
	return b | (1 << uint(pod))
}

func (b HostBitmap) Count() int {
	n := 0
	for v := b; v != 0; v &= v - 1 {
		n++
	}
	return n
}
