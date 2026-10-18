// SPDX-License-Identifier: AGPL-3.0-or-later

package asm

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

// roundTrip disassembles p, assembles the result and fails the test unless the
// cells (compared without the last-writer id, which is not part of the source) and
// the entry offset come back unchanged. It returns the disassembly.
func roundTrip(t *testing.T, label string, p Program) []byte {
	t.Helper()
	src := Disassemble(p)
	back, err := Assemble("disasm", src)
	if err != nil {
		t.Fatalf("%s: reassembling the disassembly failed: %v\n%s", label, err, src)
	}
	if back.Entry != p.Entry {
		t.Fatalf("%s: entry %d after the round trip, want %d\n%s", label, back.Entry, p.Entry, src)
	}
	if len(back.Cells) != len(p.Cells) {
		t.Fatalf("%s: %d cells after the round trip, want %d\n%s", label, len(back.Cells), len(p.Cells), src)
	}
	for i, want := range p.Cells {
		want.Writer = 0
		if back.Cells[i] != want {
			t.Fatalf("%s: cell %d = %+v after the round trip, want %+v\n%s", label, i, back.Cells[i], want, src)
		}
	}
	return src
}

// TestRoundTripCells: Assemble(Disassemble(cells)) == cells for every opcode,
// modifier and pair of addressing modes, with operand values of every sign and the
// int32 extremes, and for every entry offset.
func TestRoundTripCells(t *testing.T) {
	values := []int32{0, 1, -1, 2, -2, 95, -8000, 7999, 1 << 20, -(1 << 20), 2147483647, -2147483648}
	var cells []isa.Cell
	n := 0
	for _, op := range isa.Opcodes {
		for _, mod := range isa.Modifiers {
			for _, am := range isa.Modes {
				for _, bm := range isa.Modes {
					cells = append(cells, isa.Cell{Opcode: op, Modifier: mod,
						AMode: am, AValue: values[n%len(values)],
						BMode: bm, BValue: values[(n*5+3)%len(values)]})
					n++
				}
			}
		}
	}
	if want := len(isa.Opcodes) * len(isa.Modifiers) * len(isa.Modes) * len(isa.Modes); len(cells) != want {
		t.Fatalf("built %d cells, want %d", len(cells), want)
	}
	for _, entry := range []int{0, 1, len(cells) / 2, len(cells) - 1} {
		roundTrip(t, fmt.Sprintf("entry %d", entry), Program{Cells: cells, Entry: entry})
	}

	// Every value, with each extreme in each operand and mode.
	for _, v := range values {
		var vc []isa.Cell
		for _, m := range isa.Modes {
			vc = append(vc,
				isa.Cell{Opcode: isa.OpDAT, Modifier: isa.ModF, AMode: m, AValue: v, BMode: isa.ModeDirect},
				isa.Cell{Opcode: isa.OpDAT, Modifier: isa.ModF, AMode: isa.ModeDirect, BMode: m, BValue: v})
		}
		roundTrip(t, fmt.Sprintf("value %d", v), Program{Cells: vc})
	}

	// The empty cell, alone.
	roundTrip(t, "empty cell", Program{Cells: []isa.Cell{isa.EmptyCell}})
}

// TestRoundTripIgnoresWriter: the last-writer id is not source. It does not change
// the disassembly and is zero after the round trip.
func TestRoundTripIgnoresWriter(t *testing.T) {
	plain := []isa.Cell{
		{Opcode: isa.OpMOV, Modifier: isa.ModI, AMode: isa.ModeDirect, AValue: 0, BMode: isa.ModeDirect, BValue: 1},
		{Opcode: isa.OpSPL, Modifier: isa.ModB, AMode: isa.ModeDirect, AValue: -3, BMode: isa.ModeDirect},
	}
	written := slices.Clone(plain)
	written[0].Writer = 1
	written[1].Writer = 4294967295
	want := roundTrip(t, "plain", Program{Cells: plain, Entry: 1})
	got := roundTrip(t, "written", Program{Cells: written, Entry: 1})
	if string(got) != string(want) {
		t.Errorf("Writer changed the disassembly:\n%s\nwant\n%s", got, want)
	}
	back, err := Assemble("disasm", got)
	if err != nil {
		t.Fatal(err)
	}
	for i, cell := range back.Cells {
		if cell.Writer != 0 {
			t.Errorf("cell %d has Writer %d after the round trip, want 0", i, cell.Writer)
		}
	}
}

// TestDisassemble pins the exact output of docs/spec/asm.md section 9.
func TestDisassemble(t *testing.T) {
	tests := []struct {
		name string
		p    Program
		want string
	}{
		{"one cell", Program{Cells: []isa.Cell{isa.EmptyCell}}, "ORG 0\nDAT.F $0, $0\n"},
		{"entry, modes and signs", Program{Entry: 2, Cells: []isa.Cell{
			{Opcode: isa.OpMOV, Modifier: isa.ModX, AMode: isa.ModeImmediate, AValue: -1, BMode: isa.ModeAPreDec, BValue: 2},
			{Opcode: isa.OpSPL, Modifier: isa.ModBA, AMode: isa.ModeBPreDec, AValue: 3, BMode: isa.ModeBPostInc, BValue: -4},
			{Opcode: isa.OpDJN, Modifier: isa.ModI, AMode: isa.ModeBIndirect, AValue: 1, BMode: isa.ModeAIndirect, BValue: 2},
			{Opcode: isa.OpSEQ, Modifier: isa.ModA, AMode: isa.ModeAPostInc, AValue: 5, BMode: isa.ModeDirect, Writer: 9},
		}}, "ORG 2\nMOV.X #-1, {2\nSPL.BA <3, >-4\nDJN.I @1, *2\nSEQ.A }5, $0\n"},
		{"values are printed as they are", Program{Cells: []isa.Cell{
			{Opcode: isa.OpDAT, Modifier: isa.ModF, AMode: isa.ModeImmediate, AValue: -2147483648, BMode: isa.ModeImmediate, BValue: 2147483647},
		}}, "ORG 0\nDAT.F #-2147483648, #2147483647\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(Disassemble(tt.p)); got != tt.want {
				t.Errorf("Disassemble = %q, want %q", got, tt.want)
			}
			roundTrip(t, tt.name, tt.p)
		})
	}
}

// TestRoundTripSpecExamples: every program in a fenced asm block of docs/spec/isa.md
// and docs/spec/asm.md assembles, disassembles and reassembles to the same cells and
// entry offset, and the disassembly is a fixed point.
func TestRoundTripSpecExamples(t *testing.T) {
	for _, doc := range []struct {
		path string
		min  int // at least this many asm blocks
	}{
		{"../../docs/spec/isa.md", 3},
		{"../../docs/spec/asm.md", 4},
	} {
		data, err := os.ReadFile(doc.path)
		if err != nil {
			t.Fatal(err)
		}
		blocks := fencedBlocks(string(data), "asm")
		if len(blocks) < doc.min {
			t.Errorf("%s has %d asm blocks, want at least %d", doc.path, len(blocks), doc.min)
		}
		for i, b := range blocks {
			label := fmt.Sprintf("%s block %d", doc.path, i+1)
			p, err := Assemble(label, []byte(b))
			if err != nil {
				t.Errorf("%v\n%s", err, b)
				continue
			}
			src := roundTrip(t, label, p)
			again, err := Assemble(label, src)
			if err != nil {
				t.Fatal(err)
			}
			if string(Disassemble(again)) != string(src) {
				t.Errorf("%s: disassembly is not a fixed point:\n%s\nthen\n%s", label, src, Disassemble(again))
			}
		}
	}
}
