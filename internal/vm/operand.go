// SPDX-License-Identifier: AGPL-3.0-or-later

package vm

import (
	"errors"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

// ErrBadMode is returned by Resolve for a mode code outside the eight modes.
var ErrBadMode = errors.New("vm: invalid addressing mode")

// PostField names the field of the pointer cell a post-increment applies to.
type PostField uint8

const (
	PostNone PostField = iota // no post-increment
	PostA                     // A field of the pointer cell
	PostB                     // B field of the pointer cell
)

// PostInc is a post-increment noted by Resolve and applied later with
// ApplyPostInc (docs/spec/isa.md section 4, step 5).
type PostInc struct {
	Field   PostField
	Pointer int32 // address of the pointer cell, in [0, CORESIZE)
}

// Operand is an evaluated operand: the target address, the snapshot of the target
// cell taken at evaluation time, and the pending post-increment, if any.
type Operand struct {
	Target   int32
	Snapshot isa.Cell
	Post     PostInc
}

// Resolve evaluates one operand (docs/spec/isa.md sections 3 and 4) for the cell
// executing at pc, with operand mode and value taken from the fetched copy of
// that cell. A predecrement is written to the core now, as bot; a post-increment
// is only returned in Operand.Post. Every address is the Norm of a sum computed in
// int64, so pc and value may be any int32. An invalid mode
// returns ErrBadMode and changes nothing.
func (c *Core) Resolve(pc int32, mode isa.Mode, value int32, bot uint32) (Operand, error) {
	pcN := int64(c.Norm(int64(pc)))
	v := int64(value)
	if mode == isa.ModeImmediate {
		return Operand{Target: int32(pcN), Snapshot: c.Read(pcN)}, nil
	}
	if mode == isa.ModeDirect {
		t := c.Norm(pcN + v)
		return Operand{Target: t, Snapshot: c.Read(int64(t))}, nil
	}
	if mode > isa.ModeBPostInc {
		return Operand{}, ErrBadMode
	}

	ptr := c.Norm(pcN + v)
	cell := c.Read(int64(ptr))
	useA := mode == isa.ModeAIndirect || mode == isa.ModeAPreDec || mode == isa.ModeAPostInc
	field := func() *int32 {
		if useA {
			return &cell.AValue
		}
		return &cell.BValue
	}

	var post PostInc
	switch mode {
	case isa.ModeAPreDec, isa.ModeBPreDec:
		f := field()
		*f = c.Norm(int64(*f) - 1)
		c.Write(int64(ptr), cell, bot)
	case isa.ModeAPostInc:
		post = PostInc{Field: PostA, Pointer: ptr}
	case isa.ModeBPostInc:
		post = PostInc{Field: PostB, Pointer: ptr}
	}
	t := c.Norm(pcN + v + int64(*field()))
	return Operand{Target: t, Snapshot: c.Read(int64(t)), Post: post}, nil
}

// ApplyPostInc adds 1 (with Norm) to the pointer cell's field as it is in the core
// now, as a write by bot. A PostInc with Field PostNone, or any invalid field, does nothing.
func (c *Core) ApplyPostInc(p PostInc, bot uint32) {
	cell := c.Read(int64(p.Pointer))
	if p.Field == PostA {
		cell.AValue = c.Norm(int64(cell.AValue) + 1)
	} else if p.Field == PostB {
		cell.BValue = c.Norm(int64(cell.BValue) + 1)
	} else {
		return // PostNone or an invalid field
	}
	c.Write(int64(p.Pointer), cell, bot)
}
