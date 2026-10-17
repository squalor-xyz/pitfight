// SPDX-License-Identifier: AGPL-3.0-or-later

package vm

import (
	"slices"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

// FuzzResolve: for any core size in [1, MaxCoreSize], any pc, operand value,
// pointer-cell contents and mode, Resolve does not panic, the target and the
// post-increment pointer are in [0, N), and the snapshot is the target cell.
func FuzzResolve(f *testing.F) {
	// size is the core size minus 1 (n = 1 + size%MaxCoreSize).
	for _, size := range []uint32{0, 1, 7, 7999, MaxCoreSize - 1} {
		for mode := uint8(0); mode <= 8; mode++ {
			f.Add(size, int32(0), int32(1), int32(2), int32(3), mode)
			f.Add(size, int32(-1), int32(-9), int32(-1<<31), int32(1<<31-1), mode)
			f.Add(size, int32(1<<31-1), int32(1<<31-1), int32(1<<31-1), int32(1<<31-1), mode)
		}
	}
	f.Fuzz(func(t *testing.T, size uint32, pc, value, fa, fb int32, mode uint8) {
		n := int(1 + size%MaxCoreSize)
		c, err := NewCore(n)
		if err != nil {
			t.Fatalf("NewCore(%d): %v", n, err)
		}
		// Place stored (normalized) field values in the cell the pointer names.
		ptr := c.Norm(int64(pc) + int64(value))
		c.Write(int64(ptr), isa.Cell{Opcode: isa.OpDAT, Modifier: isa.ModF,
			AMode: isa.ModeDirect, AValue: c.Norm(int64(fa)),
			BMode: isa.ModeDirect, BValue: c.Norm(int64(fb))}, 0)

		op, err := c.Resolve(pc, isa.Mode(mode), value, 1)
		if mode >= uint8(len(isa.Modes)) {
			if err == nil {
				t.Fatalf("mode %d: expected an error", mode)
			}
			return
		}
		if err != nil {
			t.Fatalf("mode %d: %v", mode, err)
		}
		if op.Target < 0 || int(op.Target) >= n {
			t.Fatalf("N=%d: target %d outside [0, N)", n, op.Target)
		}
		if got := c.Read(int64(op.Target)); got != op.Snapshot {
			t.Fatalf("snapshot %+v is not the target cell %+v", op.Snapshot, got)
		}
		if op.Post.Field != PostNone && (op.Post.Pointer < 0 || int(op.Post.Pointer) >= n) {
			t.Fatalf("N=%d: post-increment pointer %d outside [0, N)", n, op.Post.Pointer)
		}
		c.ApplyPostInc(op.Post, 1)
		for _, a := range []int32{ptr, op.Target} {
			cell := c.Read(int64(a))
			if cell.AValue < 0 || int(cell.AValue) >= n || cell.BValue < 0 || int(cell.BValue) >= n {
				t.Fatalf("N=%d: cell %d holds out-of-range values %+v", n, a, cell)
			}
		}
	})
}

// FuzzStep decodes arbitrary bytes into legal cells, then checks bounded steps
// for determinism, event addresses, stored values and the exact process cap.
func FuzzStep(f *testing.F) {
	f.Add([]byte{0}, uint8(0), uint8(0))
	f.Add([]byte{10, 1, 1, 0, 1, 0, 0}, uint8(0), uint8(0))
	f.Add([]byte{10, 6, 7, 1, 7, 1, 1, 6, 0, 3, 1, 2}, uint8(7), uint8(7))
	f.Add([]byte{5, 4, 4, 1, 5, 1, 0, 4, 1, 0, 1, 0}, uint8(31), uint8(3))
	f.Fuzz(func(t *testing.T, data []byte, size, capByte uint8) {
		n, max := 1+int(size), 1+int(capByte%32)
		c, _ := NewCore(n)
		clone, _ := NewCore(n)
		at := func(i int) byte {
			if len(data) == 0 {
				return 0
			}
			return data[i%len(data)]
		}
		for i := 0; i < n; i++ {
			j := i * 7
			cell := isa.Cell{Opcode: isa.Opcode(at(j) % 15), Modifier: isa.Modifier(at(j+1) % 7), AMode: isa.Mode(at(j+2) % 8), AValue: int32(int(at(j+3)) % n), BMode: isa.Mode(at(j+4) % 8), BValue: int32(int(at(j+5)) % n)}
			c.Write(int64(i), cell, uint32(at(j+6)))
			clone.Write(int64(i), cell, uint32(at(j+6)))
		}
		q, _ := c.NewQueue(max, int32(at(0)))
		r, _ := clone.NewQueue(max, int32(at(0)))
		for step := 0; step < 64; step++ {
			events, other := c.Step(q, 3, nil), clone.Step(r, 3, nil)
			if !slices.Equal(events, other) || !slices.Equal(q.Addresses(), r.Addresses()) {
				t.Fatal("nondeterministic step")
			}
			if q.Len() > max {
				t.Fatalf("queue %d exceeds %d", q.Len(), max)
			}
			for _, a := range q.Addresses() {
				if a < 0 || int(a) >= n {
					t.Fatalf("queue address %d", a)
				}
			}
			for _, e := range events {
				if e.PC < 0 || int(e.PC) >= n || e.Address < 0 || int(e.Address) >= n {
					t.Fatalf("event %+v", e)
				}
				if e.Kind == CellWrite && e.Cell.Writer != 3 {
					t.Fatal("wrong writer")
				}
			}
			for i := 0; i < n; i++ {
				cell := c.Read(int64(i))
				if cell != clone.Read(int64(i)) {
					t.Fatal("different core")
				}
				if cell.AValue < 0 || int(cell.AValue) >= n || cell.BValue < 0 || int(cell.BValue) >= n {
					t.Fatal("out of range fields")
				}
			}
		}
	})
}
