// SPDX-License-Identifier: AGPL-3.0-or-later

package tourney

import "github.com/squalor-xyz/pitfight/internal/arena"

// Seed returns the placement seed of a round. Rounds count from 1. The seed is the
// first output of the SplitMix64 generator of match.md section 4 seeded with the
// round number, so it is a fixed function of the round: no clock, no math/rand
// (C36). Mixing the round number spreads consecutive rounds over the whole seed
// range.
func Seed(round int) uint64 {
	return arena.NewRNG(uint64(round)).Next()
}
