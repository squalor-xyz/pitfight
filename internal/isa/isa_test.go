// SPDX-License-Identifier: AGPL-3.0-or-later

package isa

import "testing"

func TestOpcodeList(t *testing.T) {
	want := []string{"DAT", "MOV", "ADD", "SUB", "MUL", "DIV", "JMP", "JMZ", "JMN", "DJN", "SPL", "SEQ", "SNE", "SLT", "NOP"}
	if len(Opcodes) != len(want) {
		t.Fatalf("len(Opcodes) = %d, want %d", len(Opcodes), len(want))
	}
	seen := make([]bool, 256)
	for i, op := range Opcodes {
		if seen[op] {
			t.Errorf("duplicate opcode %d", op)
		}
		seen[op] = true
		if int(op) != i {
			t.Errorf("Opcodes[%d] = %d, want code %d (spec order)", i, op, i)
		}
		if op.String() != want[i] {
			t.Errorf("Opcodes[%d].String() = %q, want %q", i, op.String(), want[i])
		}
	}
	if OpDAT != 0 || OpMOV != 1 || OpADD != 2 || OpSUB != 3 || OpMUL != 4 || OpDIV != 5 ||
		OpJMP != 6 || OpJMZ != 7 || OpJMN != 8 || OpDJN != 9 || OpSPL != 10 ||
		OpSEQ != 11 || OpSNE != 12 || OpSLT != 13 || OpNOP != 14 {
		t.Error("opcode constants are not in spec order")
	}
}

func TestModifierList(t *testing.T) {
	want := []string{"A", "B", "AB", "BA", "F", "X", "I"}
	if len(Modifiers) != len(want) {
		t.Fatalf("len(Modifiers) = %d, want %d", len(Modifiers), len(want))
	}
	seen := make([]bool, 256)
	for i, m := range Modifiers {
		if seen[m] {
			t.Errorf("duplicate modifier %d", m)
		}
		seen[m] = true
		if int(m) != i {
			t.Errorf("Modifiers[%d] = %d, want code %d (spec order)", i, m, i)
		}
		if m.String() != want[i] {
			t.Errorf("Modifiers[%d].String() = %q, want %q", i, m.String(), want[i])
		}
	}
	if ModA != 0 || ModB != 1 || ModAB != 2 || ModBA != 3 || ModF != 4 || ModX != 5 || ModI != 6 {
		t.Error("modifier constants are not in spec order")
	}
}

func TestModeList(t *testing.T) {
	want := []string{"#", "$", "*", "@", "{", "<", "}", ">"}
	if len(Modes) != len(want) {
		t.Fatalf("len(Modes) = %d, want %d", len(Modes), len(want))
	}
	seen := make([]bool, 256)
	for i, m := range Modes {
		if seen[m] {
			t.Errorf("duplicate mode %d", m)
		}
		seen[m] = true
		if int(m) != i {
			t.Errorf("Modes[%d] = %d, want code %d (spec order)", i, m, i)
		}
		if m.String() != want[i] {
			t.Errorf("Modes[%d].String() = %q, want %q", i, m.String(), want[i])
		}
	}
	if ModeImmediate != 0 || ModeDirect != 1 || ModeAIndirect != 2 || ModeBIndirect != 3 ||
		ModeAPreDec != 4 || ModeBPreDec != 5 || ModeAPostInc != 6 || ModeBPostInc != 7 {
		t.Error("mode constants are not in spec order")
	}
}

func TestOutOfRangeStrings(t *testing.T) {
	if got := Opcode(15).String(); got != "Opcode(15)" {
		t.Errorf("Opcode(15).String() = %q", got)
	}
	if got := Modifier(7).String(); got != "Modifier(7)" {
		t.Errorf("Modifier(7).String() = %q", got)
	}
	if got := Mode(8).String(); got != "Mode(8)" {
		t.Errorf("Mode(8).String() = %q", got)
	}
}

func TestEmptyCell(t *testing.T) {
	want := Cell{Opcode: OpDAT, Modifier: ModF, AMode: ModeDirect, BMode: ModeDirect}
	if EmptyCell != want {
		t.Errorf("EmptyCell = %+v, want DAT.F $0, $0 writer 0", EmptyCell)
	}
}
