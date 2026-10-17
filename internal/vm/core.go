// SPDX-License-Identifier: AGPL-3.0-or-later

// Package vm implements the pitfight virtual machine core: the circular memory
// operand evaluation, opcode execution and process queues (docs/spec/isa.md). It is pure: no I/O, clock,
// goroutines or randomness, and no map iteration (C12, C36).
package vm

import (
	"errors"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

// Allowed range of CORESIZE (docs/spec/isa.md section 1).
const (
	MinCoreSize = 1
	MaxCoreSize = 1 << 20
)

// ErrCoreSize is returned by NewCore for a size outside [MinCoreSize, MaxCoreSize].
var ErrCoreSize = errors.New("vm: core size out of range")

// Core is a circular array of cells addressed 0 .. Size()-1. Addresses passed to
// its methods are int64 and wrap modulo the size. Use NewCore; the zero value is
// not usable (Norm would divide by zero).
type Core struct {
	cells []isa.Cell
}

// NewCore returns a core of size cells, each the empty cell.
func NewCore(size int) (*Core, error) {
	if size < MinCoreSize || size > MaxCoreSize {
		return nil, ErrCoreSize
	}
	cells := make([]isa.Cell, size)
	for i := range cells {
		cells[i] = isa.EmptyCell
	}
	return &Core{cells: cells}, nil
}

// Size returns CORESIZE.
func (c *Core) Size() int { return len(c.cells) }

// Norm is the spec's Norm(x) = ((x mod N) + N) mod N, the one normalization used
// for every address and stored value. x % N lies in (-N, N), so adding N cannot
// overflow int64 for any x; the result is in [0, N) and fits int32.
func (c *Core) Norm(x int64) int32 {
	n := int64(len(c.cells))
	return int32((x%n + n) % n)
}

// Read returns a copy of the cell at addr, which wraps modulo the size. It never
// changes the cell or its writer.
func (c *Core) Read(addr int64) isa.Cell {
	return c.cells[c.Norm(addr)]
}

// Write stores cell at addr, which wraps modulo the size, and sets the cell's
// writer to bot, ignoring cell.Writer. A write of an unchanged value still counts.
func (c *Core) Write(addr int64, cell isa.Cell, bot uint32) {
	cell.Writer = bot
	c.cells[c.Norm(addr)] = cell
}
