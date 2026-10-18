// SPDX-License-Identifier: AGPL-3.0-or-later

package asm

import (
	"errors"
	"os"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

// FuzzAssemble: arbitrary input never panics; an error is the positioned error
// type with line and column at least 1; a program has a valid entry offset and
// valid cells.
func FuzzAssemble(f *testing.F) {
	for _, s := range []string{
		"",
		"\n",
		"; only a comment",
		"MOV.I $0, $1",
		"mov 0, 1",
		"DAT",
		"start: ADD.AB #4, ptr\n MOV.I ptr, @ptr\n JMP start\nptr: DAT #0, #0\n",
		"ORG go\nDAT 0\ngo: DAT 0, 0\n",
		"loop: MOV.I }loop, >loop\r\nJMN.A loop, -(1+2)*3\r\n",
		"DAT #2147483647 + 1",
		"DAT #(((1",
		"DAT #1 / 0",
		"MOV.Q 1, 2",
		"MOV %1, 2",
		"a: a: DAT 0",
		"JMP nowhere",
		"DAT \xff",
		"DAT #12ab",
		"MOV. I 1",
		"ORG 9\nDAT 0",
		"x: y: ORG x - y\nDAT 0",
	} {
		f.Add([]byte(s))
	}
	if b, err := os.ReadFile("testdata/undefined_label.s"); err == nil {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		p, err := Assemble("fuzz.s", src)
		if err != nil {
			var ae *Error
			if !errors.As(err, &ae) {
				t.Fatalf("error %T is not *Error: %v", err, err)
			}
			if ae.File != "fuzz.s" || ae.Line < 1 || ae.Col < 1 || ae.Msg == "" {
				t.Fatalf("bad error position or text: %+v", *ae)
			}
			if p.Cells != nil || p.Entry != 0 {
				t.Fatalf("Program %+v on error", p)
			}
			return
		}
		if len(p.Cells) < 1 || p.Entry < 0 || p.Entry >= len(p.Cells) {
			t.Fatalf("entry %d with %d cells", p.Entry, len(p.Cells))
		}
		for i, c := range p.Cells {
			if int(c.Opcode) >= len(isa.Opcodes) || int(c.Modifier) >= len(isa.Modifiers) ||
				int(c.AMode) >= len(isa.Modes) || int(c.BMode) >= len(isa.Modes) || c.Writer != 0 {
				t.Fatalf("cell %d invalid: %+v", i, c)
			}
		}
		// Assembly is deterministic.
		q, err := Assemble("fuzz.s", src)
		if err != nil || q.Entry != p.Entry || len(q.Cells) != len(p.Cells) {
			t.Fatalf("second assembly differs: %v", err)
		}
		for i := range p.Cells {
			if p.Cells[i] != q.Cells[i] {
				t.Fatalf("second assembly differs at cell %d", i)
			}
		}
	})
}

// cellBytes is the size of one cell in the byte encoding that decodeCells reads.
const cellBytes = 13

// decodeCells turns arbitrary bytes into valid cells: opcode, modifier and the two
// modes reduced into range, two little-endian int32 operand values and a last-writer
// id. Trailing bytes short of a whole cell are ignored.
func decodeCells(data []byte) []isa.Cell {
	var cells []isa.Cell
	for ; len(data) >= cellBytes; data = data[cellBytes:] {
		le := func(b []byte) int32 {
			return int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
		}
		cells = append(cells, isa.Cell{
			Opcode:   isa.Opcodes[int(data[0])%len(isa.Opcodes)],
			Modifier: isa.Modifiers[int(data[1])%len(isa.Modifiers)],
			AMode:    isa.Modes[int(data[2])%len(isa.Modes)],
			BMode:    isa.Modes[int(data[3])%len(isa.Modes)],
			AValue:   le(data[4:8]),
			BValue:   le(data[8:12]),
			Writer:   uint32(data[12]),
		})
	}
	return cells
}

// FuzzRoundTrip: arbitrary bytes decoded into valid cells and an entry offset
// disassemble to source that assembles back to the same cells (except the
// last-writer id) and entry, and disassembling again gives the same text.
func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte{}, uint32(0))
	f.Add([]byte{1, 6, 1, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0}, uint32(0))                // imp
	f.Add([]byte{0, 4, 0, 7, 0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0x80, 9}, uint32(0)) // int32 extremes
	f.Add([]byte{14, 2, 3, 4, 1, 0, 0, 0, 2, 0, 0, 0, 1, 10, 3, 5, 6, 3, 0, 0, 0, 0xfc, 255, 255, 255, 0}, uint32(1))
	f.Fuzz(func(t *testing.T, data []byte, entry uint32) {
		cells := decodeCells(data)
		if len(cells) == 0 {
			return
		}
		p := Program{Cells: cells, Entry: int(entry % uint32(len(cells)))}
		src := Disassemble(p)
		back, err := Assemble("fuzz.s", src)
		if err != nil {
			t.Fatalf("reassembly failed: %v\n%s", err, src)
		}
		if back.Entry != p.Entry || len(back.Cells) != len(cells) {
			t.Fatalf("entry %d with %d cells, want %d with %d\n%s", back.Entry, len(back.Cells), p.Entry, len(cells), src)
		}
		for i, want := range cells {
			want.Writer = 0
			if back.Cells[i] != want {
				t.Fatalf("cell %d = %+v, want %+v\n%s", i, back.Cells[i], want, src)
			}
		}
		if again := Disassemble(back); string(again) != string(src) {
			t.Fatalf("second disassembly differs:\n%s\nthen\n%s", src, again)
		}
	})
}
