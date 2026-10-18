// SPDX-License-Identifier: AGPL-3.0-or-later

package asm

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

// c builds a cell with writer 0.
func c(op isa.Opcode, mod isa.Modifier, am isa.Mode, av int32, bm isa.Mode, bv int32) isa.Cell {
	return isa.Cell{Opcode: op, Modifier: mod, AMode: am, AValue: av, BMode: bm, BValue: bv}
}

const (
	imm = isa.ModeImmediate
	dir = isa.ModeDirect
)

// mustAssemble fails the test if src does not assemble.
func mustAssemble(t *testing.T, src string) Program {
	t.Helper()
	p, err := Assemble("t.s", []byte(src))
	if err != nil {
		t.Fatalf("Assemble(%q): %v", src, err)
	}
	return p
}

type assembleCase struct {
	name  string
	src   string
	want  []isa.Cell
	entry int
}

var assembleCases = []assembleCase{
	// One case per opcode, with a different modifier and operands each time.
	{name: "DAT", src: "DAT.F #1, #2", want: []isa.Cell{c(isa.OpDAT, isa.ModF, imm, 1, imm, 2)}},
	{name: "MOV", src: "MOV.I $0, $1", want: []isa.Cell{c(isa.OpMOV, isa.ModI, dir, 0, dir, 1)}},
	{name: "ADD", src: "ADD.AB #4, $3", want: []isa.Cell{c(isa.OpADD, isa.ModAB, imm, 4, dir, 3)}},
	{name: "SUB", src: "SUB.X $1, $2", want: []isa.Cell{c(isa.OpSUB, isa.ModX, dir, 1, dir, 2)}},
	{name: "MUL", src: "MUL.F $3, $2", want: []isa.Cell{c(isa.OpMUL, isa.ModF, dir, 3, dir, 2)}},
	{name: "DIV", src: "DIV.BA $1, $2", want: []isa.Cell{c(isa.OpDIV, isa.ModBA, dir, 1, dir, 2)}},
	{name: "JMP", src: "JMP.B $3, $0", want: []isa.Cell{c(isa.OpJMP, isa.ModB, dir, 3, dir, 0)}},
	{name: "JMZ", src: "JMZ.A $3, $1", want: []isa.Cell{c(isa.OpJMZ, isa.ModA, dir, 3, dir, 1)}},
	{name: "JMN", src: "JMN.F $3, $1", want: []isa.Cell{c(isa.OpJMN, isa.ModF, dir, 3, dir, 1)}},
	{name: "DJN", src: "DJN.B $3, $1", want: []isa.Cell{c(isa.OpDJN, isa.ModB, dir, 3, dir, 1)}},
	{name: "SPL", src: "SPL.B $3, $0", want: []isa.Cell{c(isa.OpSPL, isa.ModB, dir, 3, dir, 0)}},
	{name: "SEQ", src: "SEQ.I $1, $2", want: []isa.Cell{c(isa.OpSEQ, isa.ModI, dir, 1, dir, 2)}},
	{name: "SNE", src: "SNE.F $1, $2", want: []isa.Cell{c(isa.OpSNE, isa.ModF, dir, 1, dir, 2)}},
	{name: "SLT", src: "SLT.AB #2, $1", want: []isa.Cell{c(isa.OpSLT, isa.ModAB, imm, 2, dir, 1)}},
	{name: "NOP", src: "NOP.F $1, $2", want: []isa.Cell{c(isa.OpNOP, isa.ModF, dir, 1, dir, 2)}},
	// One case per modifier (the opcodes above cover A, B, AB, BA, F, X and I too;
	// these make each mnemonic's code explicit on one opcode).
	{name: "mod A", src: "MOV.A $1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModA, dir, 1, dir, 2)}},
	{name: "mod B", src: "MOV.B $1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModB, dir, 1, dir, 2)}},
	{name: "mod AB", src: "MOV.AB $1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModAB, dir, 1, dir, 2)}},
	{name: "mod BA", src: "MOV.BA $1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModBA, dir, 1, dir, 2)}},
	{name: "mod F", src: "MOV.F $1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModF, dir, 1, dir, 2)}},
	{name: "mod X", src: "MOV.X $1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModX, dir, 1, dir, 2)}},
	{name: "mod I", src: "MOV.I $1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, dir, 1, dir, 2)}},
	// One case per addressing mode, as the A operand and as the B operand.
	{name: "mode #", src: "MOV.I #1, #2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, isa.ModeImmediate, 1, isa.ModeImmediate, 2)}},
	{name: "mode $", src: "MOV.I $1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, isa.ModeDirect, 1, isa.ModeDirect, 2)}},
	{name: "mode *", src: "MOV.I *1, *2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, isa.ModeAIndirect, 1, isa.ModeAIndirect, 2)}},
	{name: "mode @", src: "MOV.I @1, @2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, isa.ModeBIndirect, 1, isa.ModeBIndirect, 2)}},
	{name: "mode {", src: "MOV.I {1, {2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, isa.ModeAPreDec, 1, isa.ModeAPreDec, 2)}},
	{name: "mode <", src: "MOV.I <1, <2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, isa.ModeBPreDec, 1, isa.ModeBPreDec, 2)}},
	{name: "mode }", src: "MOV.I }1, }2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, isa.ModeAPostInc, 1, isa.ModeAPostInc, 2)}},
	{name: "mode >", src: "MOV.I >1, >2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, isa.ModeBPostInc, 1, isa.ModeBPostInc, 2)}},
	// Syntax details.
	{name: "no mode is direct", src: "MOV.I 1, 2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, dir, 1, dir, 2)}},
	{name: "sign and parenthesis start an operand without a mode", src: "ADD.F -1, +2\nDAT.F (1 + 2), -(1)",
		want: []isa.Cell{c(isa.OpADD, isa.ModF, dir, -1, dir, 2), c(isa.OpDAT, isa.ModF, dir, 3, dir, -1)}},
	{name: "space after mode", src: "MOV.I # 1, @ 2", want: []isa.Cell{c(isa.OpMOV, isa.ModI, imm, 1, isa.ModeBIndirect, 2)}},
	{name: "negative operands are not normalized", src: "MOV.I $-1, #-8000",
		want: []isa.Cell{c(isa.OpMOV, isa.ModI, dir, -1, imm, -8000)}},
	{name: "lower case", src: "mov.ab #1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModAB, imm, 1, dir, 2)}},
	{name: "mixed case", src: "Mov.bA $1, $2", want: []isa.Cell{c(isa.OpMOV, isa.ModBA, dir, 1, dir, 2)}},
	{name: "omitted B is $0", src: "JMP.B 5", want: []isa.Cell{c(isa.OpJMP, isa.ModB, dir, 5, dir, 0)}},
	{name: "omitted both is $0, $0", src: "DAT.F", want: []isa.Cell{isa.EmptyCell}},
	{name: "comments blank lines and CRLF",
		src:  "; header\r\n\r\n   \t\r\nMOV.I $0, $1 ; copy\r\n; trailer",
		want: []isa.Cell{c(isa.OpMOV, isa.ModI, dir, 0, dir, 1)}},
	{name: "tabs and spacing", src: "\tADD.AB\t#4 ,\t$3\t", want: []isa.Cell{c(isa.OpADD, isa.ModAB, imm, 4, dir, 3)}},
	{name: "no space after comma", src: "ADD.AB #4,$3", want: []isa.Cell{c(isa.OpADD, isa.ModAB, imm, 4, dir, 3)}},
	{name: "non-ASCII in a comment", src: "DAT.F $0, $0 ; caf\xc3\xa9 \xff", want: []isa.Cell{isa.EmptyCell}},
	{name: "two instructions", src: "MOV.I $0, $1\nDAT.F #0, #1\n",
		want: []isa.Cell{c(isa.OpMOV, isa.ModI, dir, 0, dir, 1), c(isa.OpDAT, isa.ModF, imm, 0, imm, 1)}},
	// Entry offset.
	{name: "ORG number", src: "DAT.F $0, $0\nORG 1\nDAT.F $0, $0", entry: 1,
		want: []isa.Cell{isa.EmptyCell, isa.EmptyCell}},
	{name: "ORG before the instructions", src: "ORG 1\nDAT.F $0, $0\nDAT.F $0, $0", entry: 1,
		want: []isa.Cell{isa.EmptyCell, isa.EmptyCell}},
	{name: "ORG lower case", src: "org 1\nDAT.F $0, $0\nDAT.F $0, $0", entry: 1,
		want: []isa.Cell{isa.EmptyCell, isa.EmptyCell}},
	{name: "ORG label is absolute", src: "DAT.F $0, $0\ngo: DAT.F $0, $0\nORG go", entry: 1,
		want: []isa.Cell{isa.EmptyCell, isa.EmptyCell}},
	{name: "label and ORG on one line", src: "DAT.F $0, $0\ngo: ORG go + 0\nDAT.F $0, $0", entry: 1,
		want: []isa.Cell{isa.EmptyCell, isa.EmptyCell}},
}

func TestAssemble(t *testing.T) {
	var seenOp [64]bool
	var seenMod [64]bool
	var seenMode [64]bool
	for _, tc := range assembleCases {
		t.Run(tc.name, func(t *testing.T) {
			p := mustAssemble(t, tc.src)
			if !slices.Equal(p.Cells, tc.want) {
				t.Errorf("cells = %+v\nwant   %+v", p.Cells, tc.want)
			}
			if p.Entry != tc.entry {
				t.Errorf("entry = %d, want %d", p.Entry, tc.entry)
			}
		})
		for _, cell := range tc.want {
			seenOp[cell.Opcode] = true
			seenMod[cell.Modifier] = true
			seenMode[cell.AMode] = true
			seenMode[cell.BMode] = true
		}
	}
	// A new opcode, modifier or mode in internal/isa without a case here fails.
	for _, op := range isa.Opcodes {
		if !seenOp[op] {
			t.Errorf("no TestAssemble case assembles opcode %v", op)
		}
	}
	for _, m := range isa.Modifiers {
		if !seenMod[m] {
			t.Errorf("no TestAssemble case assembles modifier %v", m)
		}
	}
	for _, m := range isa.Modes {
		if !seenMode[m] {
			t.Errorf("no TestAssemble case assembles mode %v", m)
		}
	}
}

// TestAssembleAllCombinations assembles every opcode, modifier and mode pair
// written out from isa's own mnemonics.
func TestAssembleAllCombinations(t *testing.T) {
	n := 0
	for _, op := range isa.Opcodes {
		for _, mod := range isa.Modifiers {
			for _, am := range isa.Modes {
				for _, bm := range isa.Modes {
					src := fmt.Sprintf("%s.%s %s3, %s-4", op, mod, am, bm)
					p, err := Assemble("t.s", []byte(src))
					if err != nil {
						t.Fatalf("Assemble(%q): %v", src, err)
					}
					want := []isa.Cell{c(op, mod, am, 3, bm, -4)}
					if !slices.Equal(p.Cells, want) || p.Entry != 0 {
						t.Fatalf("Assemble(%q) = %+v, want %+v", src, p, want)
					}
					n++
				}
			}
		}
	}
	if want := len(isa.Opcodes) * len(isa.Modifiers) * len(isa.Modes) * len(isa.Modes); n != want {
		t.Errorf("ran %d combinations, want %d", n, want)
	}
}

// TestDefaultModifier covers the table in docs/spec/asm.md section 4.
func TestDefaultModifier(t *testing.T) {
	tests := []struct {
		src  string
		want isa.Modifier
	}{
		{"DAT 1, 2", isa.ModF},
		{"DAT #1, #2", isa.ModF},
		{"NOP", isa.ModF},
		{"NOP #1, #2", isa.ModF},
		{"MOV #1, $2", isa.ModAB},
		{"MOV #1, #2", isa.ModAB},
		{"MOV $1, #2", isa.ModB},
		{"MOV $1, $2", isa.ModI},
		{"MOV @1, <2", isa.ModI},
		{"SEQ #1, $2", isa.ModAB},
		{"SEQ $1, #2", isa.ModB},
		{"SEQ $1, $2", isa.ModI},
		{"SNE #1, $2", isa.ModAB},
		{"SNE $1, #2", isa.ModB},
		{"SNE $1, $2", isa.ModI},
		{"ADD #1, $2", isa.ModAB},
		{"ADD $1, #2", isa.ModB},
		{"ADD $1, $2", isa.ModF},
		{"SUB #1, $2", isa.ModAB},
		{"SUB $1, #2", isa.ModB},
		{"SUB $1, $2", isa.ModF},
		{"MUL #1, $2", isa.ModAB},
		{"MUL $1, #2", isa.ModB},
		{"MUL $1, $2", isa.ModF},
		{"DIV #1, $2", isa.ModAB},
		{"DIV $1, #2", isa.ModB},
		{"DIV $1, $2", isa.ModF},
		{"SLT #1, $2", isa.ModAB},
		{"SLT $1, #2", isa.ModB},
		{"SLT $1, $2", isa.ModB},
		{"JMP 1", isa.ModB},
		{"JMP #1, #2", isa.ModB},
		{"JMZ 1, 2", isa.ModB},
		{"JMN #1, 2", isa.ModB},
		{"DJN 1, #2", isa.ModB},
		{"SPL #1", isa.ModB},
		// The omitted operand is $0, a direct mode, so "MOV #1" has a direct B mode.
		{"MOV #1", isa.ModAB},
		{"MOV 1", isa.ModI},
		{"ADD", isa.ModF},
	}
	covered := make([]bool, len(isa.Opcodes))
	for _, tt := range tests {
		p := mustAssemble(t, tt.src)
		if len(p.Cells) != 1 {
			t.Fatalf("%q: %d cells", tt.src, len(p.Cells))
		}
		if got := p.Cells[0].Modifier; got != tt.want {
			t.Errorf("%q: modifier %v, want %v", tt.src, got, tt.want)
		}
		covered[p.Cells[0].Opcode] = true
	}
	for _, op := range isa.Opcodes {
		if !covered[op] {
			t.Errorf("no default-modifier case for opcode %v", op)
		}
	}
}

// TestStarterBots assembles the four strategies of isa.md section 8 and compares
// them with the cells listed there.
func TestStarterBots(t *testing.T) {
	const (
		B  = isa.ModeBIndirect
		Ap = isa.ModeAPostInc
		Bp = isa.ModeBPostInc
	)
	tests := []struct {
		name string
		src  string
		want []isa.Cell
	}{
		{"imp", "MOV.I $0, $1", []isa.Cell{c(isa.OpMOV, isa.ModI, dir, 0, dir, 1)}},
		{"bomber", `
start:  ADD.AB #4, ptr
        MOV.I  ptr, @ptr
        JMP    start
ptr:    DAT    $0, $0
`, []isa.Cell{
			c(isa.OpADD, isa.ModAB, imm, 4, dir, 3),
			c(isa.OpMOV, isa.ModI, dir, 2, B, 2),
			c(isa.OpJMP, isa.ModB, dir, -2, dir, 0),
			c(isa.OpDAT, isa.ModF, dir, 0, dir, 0),
		}},
		{"replicator", `
        MOV.A  #-5, ptr
        MOV.AB #95, ptr
loop:   MOV.I  }ptr, >ptr
        JMN.A  loop, ptr
        SPL.B  $96
ptr:    DAT.F  $0, $0
`, []isa.Cell{
			c(isa.OpMOV, isa.ModA, imm, -5, dir, 5),
			c(isa.OpMOV, isa.ModAB, imm, 95, dir, 4),
			c(isa.OpMOV, isa.ModI, Ap, 3, Bp, 3),
			c(isa.OpJMN, isa.ModA, dir, -1, dir, 2),
			c(isa.OpSPL, isa.ModB, dir, 96, dir, 0),
			c(isa.OpDAT, isa.ModF, dir, 0, dir, 0),
		}},
		{"scanner", `
loop:   ADD.AB #7, ptr
        SNE.I  @ptr, ref
        JMP    loop
        MOV.I  ref, @ptr
        JMP    loop
ptr:    DAT.F  $0, $0
ref:    DAT.F  $0, $0
`, []isa.Cell{
			c(isa.OpADD, isa.ModAB, imm, 7, dir, 5),
			c(isa.OpSNE, isa.ModI, B, 4, dir, 5),
			c(isa.OpJMP, isa.ModB, dir, -2, dir, 0),
			c(isa.OpMOV, isa.ModI, dir, 3, B, 2),
			c(isa.OpJMP, isa.ModB, dir, -4, dir, 0),
			c(isa.OpDAT, isa.ModF, dir, 0, dir, 0),
			c(isa.OpDAT, isa.ModF, dir, 0, dir, 0),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := mustAssemble(t, tt.src)
			if !slices.Equal(p.Cells, tt.want) {
				t.Errorf("cells = %+v\nwant   %+v", p.Cells, tt.want)
			}
		})
	}
}

func TestLabels(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		want  []isa.Cell
		entry int
	}{
		{"backward", "top: DAT.F $0, $0\nJMP.B top",
			[]isa.Cell{isa.EmptyCell, c(isa.OpJMP, isa.ModB, dir, -1, dir, 0)}, 0},
		{"forward", "JMP.B end\nDAT.F $0, $0\nend: DAT.F $0, $0",
			[]isa.Cell{c(isa.OpJMP, isa.ModB, dir, 2, dir, 0), isa.EmptyCell, isa.EmptyCell}, 0},
		{"self", "here: JMP.B here",
			[]isa.Cell{c(isa.OpJMP, isa.ModB, dir, 0, dir, 0)}, 0},
		{"label after the last instruction is L", "JMP.B end\nend:",
			[]isa.Cell{c(isa.OpJMP, isa.ModB, dir, 1, dir, 0)}, 0},
		{"label alone on its line names the next instruction", "JMP.B target\n\n  ; note\ntarget:\n\nDAT.F $0, $0",
			[]isa.Cell{c(isa.OpJMP, isa.ModB, dir, 1, dir, 0), isa.EmptyCell}, 0},
		{"several labels on one instruction", "a: b: c: JMP.B a\nDAT.F b, c",
			[]isa.Cell{c(isa.OpJMP, isa.ModB, dir, 0, dir, 0), c(isa.OpDAT, isa.ModF, dir, -1, dir, -1)}, 0},
		{"both operands", "x: DAT.F y, x\ny: DAT.F x, y",
			[]isa.Cell{c(isa.OpDAT, isa.ModF, dir, 1, dir, 0), c(isa.OpDAT, isa.ModF, dir, -1, dir, 0)}, 0},
		{"case sensitive", "a: DAT.F $0, $0\nA: DAT.F a, A",
			[]isa.Cell{isa.EmptyCell, c(isa.OpDAT, isa.ModF, dir, -1, dir, 0)}, 0},
		{"modifier names are valid labels", "A: B: F: X: I: AB: BA: DAT.F A, I",
			[]isa.Cell{c(isa.OpDAT, isa.ModF, dir, 0, dir, 0)}, 0},
		{"underscore and digits", "_loop_2: DAT.F _loop_2, 0",
			[]isa.Cell{isa.EmptyCell}, 0},
		{"colon with space", "a : DAT.F a, 0",
			[]isa.Cell{isa.EmptyCell}, 0},
		{"label in an indirect operand", "p: DAT.F 0, 0\nMOV.I @p, >p",
			[]isa.Cell{isa.EmptyCell, c(isa.OpMOV, isa.ModI, isa.ModeBIndirect, -1, isa.ModeBPostInc, -1)}, 0},
		{"ORG forward label", "ORG go\nDAT.F $0, $0\ngo: DAT.F $0, $0", []isa.Cell{isa.EmptyCell, isa.EmptyCell}, 1},
		{"ORG backward label", "first: DAT.F $0, $0\nDAT.F $0, $0\nORG first", []isa.Cell{isa.EmptyCell, isa.EmptyCell}, 0},
		{"ORG label arithmetic", "a: DAT.F $0, $0\nb: DAT.F $0, $0\nORG b - a", []isa.Cell{isa.EmptyCell, isa.EmptyCell}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := mustAssemble(t, tt.src)
			if !slices.Equal(p.Cells, tt.want) {
				t.Errorf("cells = %+v\nwant   %+v", p.Cells, tt.want)
			}
			if p.Entry != tt.entry {
				t.Errorf("entry = %d, want %d", p.Entry, tt.entry)
			}
		})
	}
}

type errCase struct {
	name string
	src  string
	line int
	col  int
	msg  string
}

var errCases = []errCase{
	// Required by the slice: unknown opcode, bad addressing mode, undefined
	// label, duplicate label, malformed expression.
	{"unknown opcode", "FOO $1, $2", 1, 1, `unknown opcode "FOO"`},
	{"unknown opcode after a label and on a later line", "\n\nstart: Bogus.I $1, $2", 3, 8, `unknown opcode "Bogus"`},
	{"unknown opcode for a label without a colon", "loop MOV.I $0, $1", 1, 1, `unknown opcode "loop"`},
	{"unknown modifier", "MOV.Q $1, $2", 1, 5, `unknown modifier "Q"`},
	{"bad addressing mode in A", "MOV.I %1, $2", 1, 7, `bad addressing mode "%"`},
	{"bad addressing mode in B", "MOV.I $1, /2", 1, 11, `bad addressing mode "/"`},
	{"bad addressing mode colon", "MOV 1, :x", 1, 8, `bad addressing mode ":"`},
	{"opcode before a colon is a reserved label name", "\tJMP :x", 1, 2, `label name "JMP" is reserved`},
	{"undefined label", "\n  JMP nowhere", 2, 7, `undefined label "nowhere"`},
	{"undefined label in B, later operand", "a: DAT.F a, b", 1, 13, `undefined label "b"`},
	{"undefined label in ORG", "DAT.F 0, 0\nORG missing", 2, 5, `undefined label "missing"`},
	{"label names are case sensitive", "a: DAT.F A, 0", 1, 10, `undefined label "A"`},
	{"duplicate label", "a: DAT 0\na: DAT 1", 2, 1, `duplicate label "a" (first defined at line 1)`},
	{"duplicate label on one line", "a: b: a: DAT 0", 1, 7, `duplicate label "a" (first defined at line 1)`},
	{"reserved label", "mov: DAT 0", 1, 1, `label name "mov" is reserved`},
	{"reserved label ORG", "x: Org: DAT 0", 1, 4, `label name "Org" is reserved`},
	{"malformed expression, missing operand", "DAT #1 +\n", 1, 9, `expected expression, found end of line`},
	{"malformed expression, operator then comma", "DAT #(1+, 2", 1, 9, `expected expression, found ","`},
	{"malformed expression, missing paren", "DAT #(1+2, 2", 1, 10, `expected ")", found ","`},
	{"malformed expression, missing paren at EOF", "DAT #(1+2", 1, 10, `expected ")", found end of file`},
	{"malformed expression, stray paren", "DAT 1), 2", 1, 6, `expected "," or end of line, found ")"`},
	{"malformed expression, empty parens", "DAT ()", 1, 6, `expected expression, found ")"`},
	{"malformed expression, mode without expression", "MOV #, 1", 1, 6, `expected expression, found ","`},
	{"malformed expression, two modes", "MOV ##1", 1, 6, `expected expression, found "#"`},
	{"malformed expression, adjacent numbers", "MOV 1 2", 1, 7, `expected "," or end of line, found "2"`},
	{"malformed expression, binary operator first", "DAT #*2", 1, 6, `expected expression, found "*"`},
	{"malformed number", "DAT #12ab", 1, 6, `malformed number "12ab"`},
	{"malformed number with underscore", "DAT 1_0", 1, 5, `malformed number "1_0"`},
	{"number out of range", "DAT #2147483648", 1, 6, `value out of range`},
	{"parenthesized number out of range", "DAT #(2147483648)", 1, 7, `value out of range`},
	{"negated number beyond the literal limit", "DAT #-2147483649", 1, 7, `value out of range`},
	{"number beyond the literal limit in a sum", "DAT 2147483649 - 1", 1, 5, `value out of range`},
	{"huge number", "DAT #99999999999999999999999", 1, 6, `value out of range`},
	{"result out of range", "DAT #65536 * 65536", 1, 12, `value out of range`},
	{"intermediate product out of range", "DAT #65536 * 65536 / 65536", 1, 12, `value out of range`},
	{"intermediate negation out of range", "DAT #-(-2147483648) - 1", 1, 6, `value out of range`},
	{"sum out of range", "DAT #2147483647 + 1", 1, 17, `value out of range`},
	{"negation of range limit result", "DAT -2147483647 - 2", 1, 17, `value out of range`},
	{"division by zero", "DAT #1 / 0", 1, 8, `division by zero`},
	{"modulo by zero", "DAT #1 %  (2 - 2)", 1, 8, `division by zero`},
	{"division by zero via label difference", "a: DAT.F (1 / (a - a)), 0", 1, 13, `division by zero`},
	// Lexical errors.
	{"unexpected character", "DAT !", 1, 5, `unexpected character '!'`},
	{"unexpected character mid-line", "DAT 1, 2 =", 1, 10, `unexpected character '='`},
	{"unexpected non-ASCII character", "\nDAT é", 2, 5, `unexpected character 'é'`},
	{"unexpected byte", "DAT \xff", 1, 5, `unexpected byte 0xff`},
	{"unexpected NUL", "DAT \x00", 1, 5, `unexpected character '\x00'`},
	// Statement shape.
	{"statement starts with a comma", ", DAT", 1, 1, `expected opcode, found ","`},
	{"statement starts with a number", "  5 DAT", 1, 3, `expected opcode, found "5"`},
	{"space before the dot", "MOV .I 1", 1, 5, `unexpected space before "."`},
	{"space after the dot", "MOV. I 1", 1, 6, `unexpected space after "."`},
	{"missing modifier", "MOV.\n", 1, 5, `expected modifier, found end of line`},
	{"modifier is a number", "MOV.1", 1, 5, `expected modifier, found "1"`},
	{"missing B operand", "MOV 1,", 1, 7, `expected operand, found end of file`},
	{"missing first operand", "MOV , 1", 1, 5, `expected operand, found ","`},
	{"three operands", "MOV 1, 2, 3", 1, 9, `expected end of line, found ","`},
	{"modifier twice", "MOV.A.B 1, 2", 1, 6, `bad addressing mode "."`},
	{"label after the instruction", "MOV 1, 2 x:", 1, 10, `expected end of line, found "x"`},
	{"ORG without expression", "ORG\nDAT", 1, 4, `expected expression, found end of line`},
	{"ORG with trailing token", "ORG 1 2\nDAT", 1, 7, `expected end of line, found "2"`},
	// ORG and program size.
	{"ORG past the end", "ORG 5\nDAT 0", 1, 5, `entry offset 5 outside 0..0`},
	{"ORG at L", "ORG 2\nDAT 0\nDAT 0", 1, 5, `entry offset 2 outside 0..1`},
	{"ORG negative", "DAT 0\nORG   -1", 2, 7, `entry offset -1 outside 0..0`},
	{"ORG label after the last instruction", "DAT 0\nend:\nORG end", 3, 5, `entry offset 1 outside 0..0`},
	{"duplicate ORG", "ORG 0\nORG 0\nDAT 0", 2, 1, `duplicate ORG (first at line 1)`},
	{"empty source", "", 1, 1, `no instructions`},
	{"only comments", "; nothing\n", 2, 1, `no instructions`},
	{"only labels and ORG", "ORG 0\nx:", 2, 3, `no instructions`},
	// Order: syntax errors come before evaluation errors.
	{"syntax error after an undefined label", "JMP nowhere\nFOO", 2, 1, `unknown opcode "FOO"`},
	{"syntax error after a division by zero", "DAT 1 / 0\nMOV 1 2", 2, 7, `expected "," or end of line, found "2"`},
	{"first undefined label wins", "DAT a, b", 1, 5, `undefined label "a"`},
	{"A operand is evaluated before B", "DAT 1 / 0, x", 1, 7, `division by zero`},
	{"operands are evaluated before the ORG expression", "ORG missing\nDAT x", 2, 5, `undefined label "x"`},
	{"empty program is reported before the ORG expression", "ORG missing", 1, 12, `no instructions`},
	{"earlier instruction first", "DAT x, 0\nDAT 1 / 0", 1, 5, `undefined label "x"`},
	// Limits.
	{"too many nested parentheses", "DAT #" + strings.Repeat("(", 257) + "1" + strings.Repeat(")", 257), 1, 5 + 257, `expression too complex`},
	{"too many unary operators", "DAT #" + strings.Repeat("-", 257) + "1", 1, 5 + 257, `expression too complex`},
	{"operator chain too long", "DAT #1" + strings.Repeat("+1", 257), 1, 7 + 2*256, `expression too complex`},
	{"too many instructions", strings.Repeat("DAT\n", 1048576+1), 1048577, 1, `program longer than 1048576 instructions`},
}

func TestErrors(t *testing.T) {
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Assemble("prog.s", []byte(tc.src))
			if err == nil {
				t.Fatalf("assembled to %d cells, want an error", len(p.Cells))
			}
			var ae *Error
			if !errors.As(err, &ae) {
				t.Fatalf("error %T %v is not *asm.Error", err, err)
			}
			if ae.File != "prog.s" || ae.Line != tc.line || ae.Col != tc.col || ae.Msg != tc.msg {
				t.Errorf("error = %s:%d:%d: %s\nwant   prog.s:%d:%d: %s", ae.File, ae.Line, ae.Col, ae.Msg, tc.line, tc.col, tc.msg)
			}
			if want := fmt.Sprintf("prog.s:%d:%d: %s", tc.line, tc.col, tc.msg); err.Error() != want {
				t.Errorf("Error() = %q, want %q", err.Error(), want)
			}
			if p.Cells != nil || p.Entry != 0 {
				t.Errorf("Program = %+v on error, want zero value", p)
			}
		})
	}
}

// TestErrorsBoundaries checks that the limits in TestErrors are exact: one step
// below each still assembles.
func TestErrorsBoundaries(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int32 // AValue, for the expression cases
	}{
		{"256 nested parentheses", "DAT #" + strings.Repeat("(", 256) + "1" + strings.Repeat(")", 256), 1},
		{"256 unary operators", "DAT #" + strings.Repeat("-", 256) + "1", 1},
		{"256 chained operators", "DAT #1" + strings.Repeat("+1", 256), 257},
		{"int32 maximum", "DAT #2147483647", 2147483647},
		{"int32 minimum", "DAT #-2147483647 - 1", -2147483648},
		{"int32 minimum literal", "DAT #-2147483648", -2147483648},
		{"result at the maximum", "DAT #2147483646 + 1", 2147483647},
	}
	for _, tt := range tests {
		p := mustAssemble(t, tt.src)
		if got := p.Cells[0].AValue; got != tt.want {
			t.Errorf("%s: AValue = %d, want %d", tt.name, got, tt.want)
		}
	}
	p := mustAssemble(t, strings.Repeat("DAT\n", 1048576))
	if len(p.Cells) != 1048576 {
		t.Errorf("1048576 instructions gave %d cells", len(p.Cells))
	}
}

// TestErrorsTestdata checks the malformed program that the CLI check uses.
func TestErrorsTestdata(t *testing.T) {
	const path = "testdata/undefined_label.s"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Assemble(path, src)
	want := path + `:3:16: undefined label "nowhere"`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %s", err, want)
	}
}

// TestSpecExamples assembles every fenced block tagged asm in docs/spec/asm.md.
func TestSpecExamples(t *testing.T) {
	data, err := os.ReadFile("../../docs/spec/asm.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := fencedBlocks(string(data), "asm")
	if len(blocks) < 3 {
		t.Fatalf("found %d asm blocks in asm.md, want at least 3", len(blocks))
	}
	for i, b := range blocks {
		if _, err := Assemble(fmt.Sprintf("asm.md block %d", i+1), []byte(b)); err != nil {
			t.Errorf("%v\n%s", err, b)
		}
	}
}

// fencedBlocks returns the contents of the fenced blocks whose info string is tag.
func fencedBlocks(doc, tag string) []string {
	var out []string
	var cur []string
	in, match := false, false
	for _, line := range strings.Split(doc, "\n") {
		switch {
		case !in && strings.HasPrefix(line, "```"):
			in, match, cur = true, strings.TrimSpace(strings.TrimPrefix(line, "```")) == tag, nil
		case in && strings.HasPrefix(line, "```"):
			if match {
				out = append(out, strings.Join(cur, "\n")+"\n")
			}
			in = false
		case in:
			cur = append(cur, line)
		}
	}
	return out
}
