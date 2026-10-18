// SPDX-License-Identifier: AGPL-3.0-or-later

package arena

import "fmt"

// Placement is the outcome of the placement algorithm (match.md section 6).
// Order lists the bot ids around the circle, Rotation is r, Gaps is g (indexed by
// position around the circle) and Starts[i] is the start address of bot i+1.
type Placement struct {
	Order    []int
	Rotation int
	Gaps     []int
	Starts   []int
}

// checkPlacement is check 8 of match.md section 1: a valid placement exists if and
// only if T + K*S <= N. All sums are int64; the earlier checks keep them from
// overflowing (match.md section 3).
func checkPlacement(cfg Config, lengths []int) error {
	var total int64
	for _, l := range lengths {
		total += int64(l)
	}
	k, s, n := int64(len(lengths)), int64(cfg.MinSeparation), int64(cfg.CoreSize)
	if need := total + k*s; need > n {
		return &Error{PlacementImpossible, fmt.Sprintf(
			"placement impossible: %d bots of total length %d with min separation %d need %d cells, core size is %d",
			k, total, s, need, n)}
	}
	return nil
}

// Place places bots of the given lengths (bot i+1 has length lengths[i]) with the
// algorithm of match.md section 6. Only the placement check (match.md section 1,
// check 8) is made here; Validate makes the earlier ones, so cfg and lengths must
// already be valid. In particular Place requires len(lengths) >= 1 and panics
// otherwise; Run guarantees at least 2 bots.
func Place(cfg Config, lengths []int) (Placement, error) {
	if err := checkPlacement(cfg, lengths); err != nil {
		return Placement{}, err
	}
	k, n, s := len(lengths), int64(cfg.CoreSize), int64(cfg.MinSeparation)
	var total int64
	for _, l := range lengths {
		total += int64(l)
	}
	rng := NewRNG(cfg.Seed)

	// 1. Order: a Fisher-Yates shuffle of 1..K.
	order := make([]int, k)
	for i := range order {
		order[i] = i + 1
	}
	for i := k - 1; i >= 1; i-- {
		j := int(rng.Draw(uint64(i + 1)))
		order[i], order[j] = order[j], order[i]
	}

	// 2. Rotation.
	rot := int64(rng.Draw(uint64(n)))

	// 3. Gaps: the slack F is spread over the K gaps.
	rest := n - (total + int64(k)*s)
	gaps := make([]int, k)
	for i := 0; i < k-1; i++ {
		g := int64(rng.Draw(uint64(rest + 1)))
		gaps[i] = int(g)
		rest -= g
	}
	gaps[k-1] = int(rest)

	// 4. Start addresses.
	starts := make([]int, k)
	pos := rot
	for i, id := range order {
		starts[id-1] = int(pos)
		pos = (pos + int64(lengths[id-1]) + s + int64(gaps[i])) % n
	}
	return Placement{Order: order, Rotation: int(rot), Gaps: gaps, Starts: starts}, nil
}
