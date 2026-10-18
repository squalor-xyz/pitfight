// SPDX-License-Identifier: AGPL-3.0-or-later

package arena

import (
	"fmt"
	"testing"
)

// goldenRow pins the result of one match of two starter bots, run under the
// default configuration (core 8000, 80000 cycles, 8000 processes, separation 100)
// with the given placement seed. The first bot is bot 1, so it moves first in every
// cycle. winner is 0 for a tie. cycles is the number of the last cycle run.
type goldenRow struct {
	a, b   string
	seed   uint64
	cycles int
	winner uint32
}

// golden is the table of pinned results: every pair of the four starter bots (in
// the order of starterBots, the lower-numbered bot first) at seeds 1, 2 and 3.
//
// Every value was reviewed by hand before it was pinned (the full reasoning is in
// the note of slice S10). In short, the dwarf's bomb number k lands in cycle 3k-1
// on its pointer cell (start + 3) + 4k:
//
//   - imp vs dwarf, seeds 1 and 2: the imp executes the cell it has just written,
//     so only a bomb on that very cell kills it. With the imp moving first, the
//     bomb meets the imp's head at k = (imp start - dwarf start - 4) mod 8000
//     (seed 1: k = 1960, cycle 5879, so the imp dies in cycle 5880; seed 2:
//     k = 1202, cycle 3605, dies in cycle 3606). Seed 3: the imp reaches the dwarf
//     first (cycle 3140), overwrites its code with imps, and neither dies.
//   - imp vs replicator and imp vs scanner: no imp process ever executes a DAT,
//     so the imp survives; the other bot keeps a process too: tie at the limit.
//   - dwarf vs scanner: the dwarf's stride-4 bomb either hits the scanner's cell 0
//     (seed 1: cycle 2957, the scanner runs the DAT in cycle 2959) or its bomb
//     cell 2 early (seed 2, hit in cycle 3002, run in cycle 3990 when the scanner
//     finds the dwarf), or the scanner finds the dwarf first (seed 3: bombs the
//     dwarf's cell 1 in cycle 2136, the dwarf runs it in cycle 2138).
//   - dwarf vs replicator: copies sit 100 cells apart, so the dwarf's stride-4 bombs
//     hit the same two cells of every copy, cell c = (dwarf start + 3 - replicator
//     start) mod 4 and c + 4: cells 2 and 6 at seeds 1 and 2, cells 3 and 7 at seed
//     3 (dwarf 7561, replicator 4373). The match ends when the last replicator
//     process runs a bomb.
//   - replicator vs scanner: the replicator out-multiplies the scanner's bombs and
//     its copy loops finally overwrite a scanner cell with an empty cell.
var golden = []goldenRow{
	{"imp", "dwarf", 1, 5880, 2},
	{"imp", "dwarf", 2, 3606, 2},
	{"imp", "dwarf", 3, 80000, 0},
	{"imp", "replicator", 1, 80000, 0},
	{"imp", "replicator", 2, 80000, 0},
	{"imp", "replicator", 3, 80000, 0},
	{"imp", "scanner", 1, 80000, 0},
	{"imp", "scanner", 2, 80000, 0},
	{"imp", "scanner", 3, 80000, 0},
	{"dwarf", "replicator", 1, 2334, 1},
	{"dwarf", "replicator", 2, 2896, 1},
	{"dwarf", "replicator", 3, 5425, 1},
	{"dwarf", "scanner", 1, 2959, 1},
	{"dwarf", "scanner", 2, 3990, 1},
	{"dwarf", "scanner", 3, 2138, 2},
	{"replicator", "scanner", 1, 12099, 1},
	{"replicator", "scanner", 2, 1046, 1},
	{"replicator", "scanner", 3, 14208, 1},
}

// TestGolden runs every pairing at seeds 1, 2 and 3 and compares the pinned winner
// and cycles run. It first checks that the table covers every pairing and seed.
func TestGolden(t *testing.T) {
	seeds := []uint64{1, 2, 3}
	type key struct {
		a, b string
		seed uint64
	}
	rows := map[key]goldenRow{}
	for _, r := range golden {
		k := key{r.a, r.b, r.seed}
		if _, dup := rows[k]; dup {
			t.Errorf("duplicate golden row %v", k)
		}
		rows[k] = r
	}
	for i, a := range starterBots {
		for _, b := range starterBots[i+1:] {
			for _, seed := range seeds {
				k := key{a, b, seed}
				row, ok := rows[k]
				if !ok {
					t.Errorf("no golden row for %s vs %s at seed %d", a, b, seed)
					continue
				}
				t.Run(fmt.Sprintf("%s_vs_%s_seed%d", a, b, seed), func(t *testing.T) {
					cfg := DefaultConfig()
					cfg.Seed = seed
					res, _, err := Run(cfg, []Bot{starterBot(t, a), starterBot(t, b)})
					if err != nil {
						t.Fatal(err)
					}
					if res.Cycles != row.cycles || res.Winner != row.winner {
						t.Errorf("cycles %d winner %d, want cycles %d winner %d", res.Cycles, res.Winner, row.cycles, row.winner)
					}
				})
			}
		}
	}
	if want := 6 * len(seeds); len(golden) != want {
		t.Errorf("%d golden rows, want %d", len(golden), want)
	}
}
