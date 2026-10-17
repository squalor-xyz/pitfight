// SPDX-License-Identifier: AGPL-3.0-or-later

package vm

import (
	"math"
	"math/big"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

func TestEmptyCore(t *testing.T) {
	for _, n := range []int{1, 2, 8, 8000, MaxCoreSize} {
		c, err := NewCore(n)
		if err != nil {
			t.Fatalf("NewCore(%d): %v", n, err)
		}
		if c.Size() != n {
			t.Errorf("Size() = %d, want %d", c.Size(), n)
		}
		for a := 0; a < n; a++ {
			if got := c.Read(int64(a)); got != isa.EmptyCell {
				t.Fatalf("N=%d cell %d = %+v, want empty cell", n, a, got)
			}
		}
	}
}

func TestNewCoreRange(t *testing.T) {
	for _, n := range []int{0, -1, -8000, MaxCoreSize + 1, math.MaxInt32} {
		if c, err := NewCore(n); err == nil || c != nil {
			t.Errorf("NewCore(%d) = %v, %v; want nil and an error", n, c, err)
		}
	}
	if MinCoreSize != 1 || MaxCoreSize != 1048576 {
		t.Errorf("range = [%d, %d], want [1, 1048576]", MinCoreSize, MaxCoreSize)
	}
}

func TestWrap(t *testing.T) {
	for _, n := range []int{1, 8, 8000, MaxCoreSize} {
		c, err := NewCore(n)
		if err != nil {
			t.Fatal(err)
		}
		N := int64(n)
		inputs := []int64{
			0, 1, N - 1, N, N + 1, 2 * N, -1, -2, -N, -N - 1, -N + 1, -2*N + 1,
			-1 << 20, -(1 << 40), 1 << 40, -1<<40 - 3, math.MaxInt64, math.MinInt64, math.MinInt64 + 1,
		}
		for _, x := range inputs {
			want := new(big.Int).Mod(big.NewInt(x), big.NewInt(N)).Int64()
			got := c.Norm(x)
			if int64(got) != want {
				t.Errorf("N=%d Norm(%d) = %d, want %d", n, x, got, want)
			}
		}
	}
}

func TestWrapSpecExamples(t *testing.T) {
	c, _ := NewCore(8)
	for _, tc := range []struct{ in, want int64 }{{-1, 7}, {8, 0}, {-9, 7}, {0, 0}, {7, 7}} {
		if got := c.Norm(tc.in); int64(got) != tc.want {
			t.Errorf("Norm(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestReadWriteWrapAddresses(t *testing.T) {
	c, _ := NewCore(8)
	cell := isa.Cell{Opcode: isa.OpMOV, Modifier: isa.ModI, AMode: isa.ModeDirect, AValue: 3, BMode: isa.ModeBPostInc, BValue: 5}
	c.Write(-1, cell, 4)
	want := cell
	want.Writer = 4
	if got := c.Read(7); got != want {
		t.Errorf("Read(7) after Write(-1) = %+v, want %+v", got, want)
	}
	if got := c.Read(15); got != want {
		t.Errorf("Read(15) = %+v, want %+v", got, want)
	}
	if got := c.Read(-9 - 8000); got != want {
		t.Errorf("Read(-8009) = %+v, want %+v", got, want)
	}
	for a := int64(0); a < 7; a++ {
		if got := c.Read(a); got != isa.EmptyCell {
			t.Errorf("cell %d changed: %+v", a, got)
		}
	}
}

func TestWriteRecordsWriter(t *testing.T) {
	c, _ := NewCore(8)
	cell := isa.Cell{Opcode: isa.OpADD, Modifier: isa.ModAB, AMode: isa.ModeImmediate, AValue: 1, BMode: isa.ModeImmediate, BValue: 2, Writer: 99}
	c.Write(3, cell, 2)
	if got := c.Read(3).Writer; got != 2 {
		t.Errorf("writer after Write by bot 2 = %d, want 2 (the argument, not the cell's own Writer field)", got)
	}
	for i := 0; i < 3; i++ {
		_ = c.Read(3)
		_ = c.Read(4)
	}
	if got := c.Read(3).Writer; got != 2 {
		t.Errorf("writer after reads = %d, want 2", got)
	}
	if got := c.Read(4).Writer; got != 0 {
		t.Errorf("writer of untouched cell after reads = %d, want 0", got)
	}
	c.Write(3, cell, 5)
	if got := c.Read(3).Writer; got != 5 {
		t.Errorf("writer after Write by bot 5 = %d, want 5", got)
	}
	// Writing an identical value is still a write and sets the writer (spec section 4).
	c.Write(3, c.Read(3), 7)
	if got := c.Read(3).Writer; got != 7 {
		t.Errorf("writer after same-value write = %d, want 7", got)
	}
}
