// Package rng provides the server-authoritative random source (design section 13).
// It is seedable so that runs and integration tests can be made deterministic by
// injecting a fixed seed; a zero seed falls back to a time-based seed.
package rng

import (
	"math/rand"
	"sync"
	"time"
)

// Rand is a concurrency-safe random source. All gameplay randomness (initial
// body metrics, work experience jitter, ...) must go through it so that a fixed
// seed reproduces the same world.
type Rand struct {
	mu sync.Mutex
	r  *rand.Rand
}

// New returns a Rand seeded with the given seed. A seed of 0 uses a time-based
// seed, making the source non-deterministic.
func New(seed int64) *Rand {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &Rand{r: rand.New(rand.NewSource(seed))}
}

// IntN returns a pseudo-random int in the half-open interval [0, n). It panics
// if n <= 0, matching math/rand.Intn.
func (g *Rand) IntN(n int) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.r.Intn(n)
}
