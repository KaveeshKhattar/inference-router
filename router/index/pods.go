package index

import "sync"

// PodRegistry assigns stable 0..MaxPods-1 indices to replica URLs.
type PodRegistry struct {
	mu         sync.RWMutex
	urlToIndex map[string]int
}

func NewPodRegistry() *PodRegistry {
	return &PodRegistry{urlToIndex: make(map[string]int)}
}

// Ensure returns the pod index for url, assigning the next free slot on first sight.
// Returns -1 when MaxPods replicas are already registered.
func (p *PodRegistry) Ensure(url string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	if idx, ok := p.urlToIndex[url]; ok {
		return idx
	}
	if len(p.urlToIndex) >= MaxPods {
		return -1
	}
	idx := len(p.urlToIndex)
	p.urlToIndex[url] = idx
	return idx
}

func (p *PodRegistry) Index(url string) (int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	idx, ok := p.urlToIndex[url]
	return idx, ok
}

func (p *PodRegistry) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.urlToIndex)
}
