// SPDX-License-Identifier: AGPL-3.0-or-later

// Package asm assembles pitfight bot source (docs/spec/asm.md) into cells.
package asm

import (
	"strconv"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

// Program is an assembled bot: its cells and its entry offset (match.md section 5).
// Cell values are as written in the source, not reduced modulo CORESIZE.
type Program struct {
	Cells []isa.Cell
	Entry int // 0 <= Entry < len(Cells)
}

// Assemble assembles src. name is only used in errors. The error, if any, is an
// *Error. On error the Program is the zero value.
func Assemble(name string, src []byte) (Program, error) {
	p := &parser{file: name, toks: lex(src), labels: make(map[string]labelDef)}
	if err := p.parseFile(); err != nil {
		return Program{}, err
	}

	// The labels are known now. Evaluate the operands of the instructions in source
	// order (docs/spec/asm.md section 7).
	ev := &evaluator{file: name, labels: p.labels}
	cells := make([]isa.Cell, len(p.instrs))
	for i, in := range p.instrs {
		ev.here = i
		a, err := ev.operand(in.a)
		if err != nil {
			return Program{}, err
		}
		b, err := ev.operand(in.b)
		if err != nil {
			return Program{}, err
		}
		cells[i] = isa.Cell{Opcode: in.op, Modifier: in.mod, AMode: in.aMode, AValue: a, BMode: in.bMode, BValue: b}
	}
	if len(cells) == 0 {
		eof := p.toks[len(p.toks)-1]
		return Program{}, p.errorAt(eof, "no instructions")
	}

	entry := int64(0)
	if p.org != nil {
		ev.here = 0
		var err error
		if entry, err = ev.value(p.org); err != nil {
			return Program{}, err
		}
		if entry < 0 || entry >= int64(len(cells)) {
			return Program{}, p.errorAt(p.orgTok, "entry offset "+strconv.FormatInt(entry, 10)+" outside 0.."+strconv.Itoa(len(cells)-1))
		}
	}
	return Program{Cells: cells, Entry: int(entry)}, nil
}
