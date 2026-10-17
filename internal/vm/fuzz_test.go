// SPDX-License-Identifier: AGPL-3.0-or-later

package vm

import (
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
