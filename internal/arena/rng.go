// SPDX-License-Identifier: AGPL-3.0-or-later

// Package arena runs matches: it validates a configuration, places bots in a
// core with a seeded generator, schedules them round-robin over the pure VM core
// and reports the result (docs/spec/match.md).
package arena

// RNG is the SplitMix64 generator of docs/spec/match.md section 4.
type RNG struct{ s uint64 }

// NewRNG returns a generator seeded with seed; every seed is valid.
func NewRNG(seed uint64) *RNG { return &RNG{s: seed} }

// Next returns the next 64-bit output. Arithmetic is modulo 2^64.
func (r *RNG) Next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Draw returns an integer in [0, n) without modulo bias, by rejection. n must be
// at least 1. It consumes the outputs up to and including the first accepted one.
func (r *RNG) Draw(n uint64) uint64 {
	t := (0 - n) % n // (2^64 - n) mod n, computed in unsigned 64-bit
	for {
		if x := r.Next(); x >= t {
			return x % n
		}
	}
}
