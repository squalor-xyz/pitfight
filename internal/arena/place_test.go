// SPDX-License-Identifier: AGPL-3.0-or-later

package arena

import (
	"slices"
	"testing"
)

func placeConfig(seed uint64, n, s int) Config {
	cfg := DefaultConfig()
	cfg.Seed, cfg.CoreSize, cfg.MinSeparation = seed, n, s
	return cfg
}

// TestPlacement checks the worked placements P1-P6 and X2 of match.md section 6.
func TestPlacement(t *testing.T) {
	tests := []struct {
		name    string
		seed    uint64
		n, s    int
		lengths []int
		order   []int
		rot     int
		gaps    []int
		starts  []int
	}{
		{"P1", 1, 8000, 100, []int{100, 100}, []int{1, 2}, 4519, []int{6677, 923}, []int{4519, 3396}},
		{"P2", 2, 8000, 100, []int{100, 100}, []int{2, 1}, 4226, []int{6918, 682}, []int{3344, 4226}},
		{"P3", 0, 8000, 100, []int{5, 12, 30}, []int{3, 1, 2}, 1679, []int{1272, 2457, 3924}, []int{3081, 5643, 1679}},
		{"P4", 7, 100, 0, []int{10, 20, 30, 40}, []int{2, 3, 1, 4}, 3, []int{0, 0, 0, 0}, []int{53, 3, 23, 63}},
		{"P5", 1<<64 - 1, 64, 4, []int{8, 8, 8}, []int{1, 2, 3}, 41, []int{2, 6, 20}, []int{41, 55, 9}},
		{"P6", 42, 8, 0, []int{1, 1}, []int{1, 2}, 3, []int{0, 6}, []int{3, 4}},
		{"X2", 1, 200, 10, []int{90, 90}, []int{1, 2}, 119, []int{0, 0}, []int{119, 19}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := placeConfig(tt.seed, tt.n, tt.s)
			p, err := Place(cfg, tt.lengths)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(p.Order, tt.order) {
				t.Errorf("order = %v, want %v", p.Order, tt.order)
			}
			if p.Rotation != tt.rot {
				t.Errorf("rotation = %d, want %d", p.Rotation, tt.rot)
			}
			if !slices.Equal(p.Gaps, tt.gaps) {
				t.Errorf("gaps = %v, want %v", p.Gaps, tt.gaps)
			}
			if !slices.Equal(p.Starts, tt.starts) {
				t.Errorf("starts = %v, want %v", p.Starts, tt.starts)
			}
			checkBlocks(t, tt.n, tt.s, tt.lengths, p.Starts)
		})
	}
}

// checkBlocks verifies, independently of the algorithm, that the blocks do not
// overlap and that at least s empty cells follow the last cell of every block.
func checkBlocks(t *testing.T, n, s int, lengths, starts []int) {
	t.Helper()
	if len(starts) != len(lengths) {
		t.Fatalf("got %d starts, want %d", len(starts), len(lengths))
	}
	owner := make([]int, n)
	for i, a := range starts {
		for k := 0; k < lengths[i]; k++ {
			c := (a + k) % n
			if owner[c] != 0 {
				t.Fatalf("cell %d belongs to bots %d and %d", c, owner[c], i+1)
			}
			owner[c] = i + 1
		}
	}
	for i, a := range starts {
		empty := 0
		for k := lengths[i]; k < n && owner[(a+k)%n] == 0; k++ {
			empty++
		}
		if empty < s {
			t.Errorf("only %d empty cells follow bot %d, want at least %d", empty, i+1, s)
		}
	}
}

// TestPlacementProperties checks the placement constraints on many seeds.
func TestPlacementProperties(t *testing.T) {
	lengths := []int{7, 20, 3, 11}
	for seed := uint64(0); seed < 300; seed++ {
		cfg := placeConfig(seed, 150, 10)
		p, err := Place(cfg, lengths)
		if err != nil {
			t.Fatal(err)
		}
		checkBlocks(t, 150, 10, lengths, p.Starts)
	}
}

func TestPlacementImpossible(t *testing.T) {
	tests := []struct {
		name    string
		seed    uint64
		n, s    int
		lengths []int
		want    string
	}{
		{"X1", 1, 8000, 100, []int{4000, 3900}, "placement impossible: 2 bots of total length 7900 with min separation 100 need 8100 cells, core size is 8000"},
		{"X3", 1, 200, 10, []int{91, 90}, "placement impossible: 2 bots of total length 181 with min separation 10 need 201 cells, core size is 200"},
		{"X4", 1, 8, 100, []int{1, 1}, "placement impossible: 2 bots of total length 2 with min separation 100 need 202 cells, core size is 8"},
		{"X5", 1, 8, 0, []int{5, 4}, "placement impossible: 2 bots of total length 9 with min separation 0 need 9 cells, core size is 8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Place(placeConfig(tt.seed, tt.n, tt.s), tt.lengths)
			e, ok := err.(*Error)
			if !ok {
				t.Fatalf("err = %v, want *Error", err)
			}
			if e.Kind != PlacementImpossible {
				t.Errorf("kind = %d, want PlacementImpossible", e.Kind)
			}
			if e.Msg != tt.want {
				t.Errorf("message = %q, want %q", e.Msg, tt.want)
			}
		})
	}
}
