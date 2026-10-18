// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tourney runs round-robin tournaments: in every round each pair of bots
// plays twice, once in each order, on a seed derived from the round number. Matches
// run on a pool of workers, but results are kept by match index, so the scoreboard
// depends only on the bots, the configuration and the number of rounds.
package tourney

// Pair is two bots by index into the bot list, with A < B.
type Pair struct{ A, B int }

// Pairings returns every unordered pair of n bots, ordered by A and then B. For
// fewer than 2 bots there are none.
func Pairings(n int) []Pair {
	var pairs []Pair
	for a := 0; a < n; a++ {
		for b := a + 1; b < n; b++ {
			pairs = append(pairs, Pair{a, b})
		}
	}
	return pairs
}

// Match is one match of a tournament. Index is its position in the list Matches
// returns. First is the bot that gets match id 1 and Second the bot that gets id 2,
// both as indexes into the bot list. Match ids are not rotated (match.md section 7),
// so id 1 moves first in every cycle; playing each pair in both orders cancels that.
type Match struct {
	Index         int
	Round         int
	Seed          uint64
	First, Second int
}

// Matches returns the matches of a tournament of n bots over rounds rounds, in this
// fixed order: rounds from 1 up; within a round the pairs in Pairings order; for each
// pair first the order (A, B), then (B, A). Every match of a round has Seed(round).
func Matches(n, rounds int) []Match {
	pairs := Pairings(n)
	var matches []Match
	for round := 1; round <= rounds; round++ {
		seed := Seed(round)
		for _, p := range pairs {
			matches = append(matches,
				Match{Index: len(matches), Round: round, Seed: seed, First: p.A, Second: p.B},
				Match{Index: len(matches) + 1, Round: round, Seed: seed, First: p.B, Second: p.A})
		}
	}
	return matches
}
