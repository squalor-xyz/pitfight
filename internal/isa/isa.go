// SPDX-License-Identifier: AGPL-3.0-or-later

package isa

import "strconv"

// Opcode is an instruction opcode. The constants are in the code order of
// docs/spec/isa.md section 3 (DAT = 0 .. NOP = 14).
type Opcode uint8

const (
	OpDAT Opcode = iota
	OpMOV
	OpADD
	OpSUB
	OpMUL
	OpDIV
	OpJMP
	OpJMZ
	OpJMN
	OpDJN
	OpSPL
	OpSEQ
	OpSNE
	OpSLT
	OpNOP
)

// Modifier selects which operand fields an instruction reads and writes. The
// constants are in the code order of docs/spec/isa.md section 3 (A = 0 .. I = 6).
type Modifier uint8

const (
	ModA Modifier = iota
	ModB
	ModAB
	ModBA
	ModF
	ModX
	ModI
)

// Mode is an addressing mode. The constants are in the code order of
// docs/spec/isa.md section 3 (immediate = 0 .. B-postincrement = 7).
type Mode uint8

const (
	ModeImmediate Mode = iota // #
	ModeDirect                // $
	ModeAIndirect             // *
	ModeBIndirect             // @
	ModeAPreDec               // {
	ModeBPreDec               // <
	ModeAPostInc              // }
	ModeBPostInc              // >
)

// Opcodes, Modifiers and Modes list every valid value in code order, so
// Opcodes[i] == Opcode(i) and likewise for the others. Callers must not modify them.
var (
	Opcodes = []Opcode{
		OpDAT, OpMOV, OpADD, OpSUB, OpMUL, OpDIV, OpJMP, OpJMZ,
		OpJMN, OpDJN, OpSPL, OpSEQ, OpSNE, OpSLT, OpNOP,
	}
	Modifiers = []Modifier{ModA, ModB, ModAB, ModBA, ModF, ModX, ModI}
	Modes     = []Mode{
		ModeImmediate, ModeDirect, ModeAIndirect, ModeBIndirect,
		ModeAPreDec, ModeBPreDec, ModeAPostInc, ModeBPostInc,
	}
)

var (
	opcodeNames   = [...]string{"DAT", "MOV", "ADD", "SUB", "MUL", "DIV", "JMP", "JMZ", "JMN", "DJN", "SPL", "SEQ", "SNE", "SLT", "NOP"}
	modifierNames = [...]string{"A", "B", "AB", "BA", "F", "X", "I"}
	modeNames     = [...]string{"#", "$", "*", "@", "{", "<", "}", ">"}
)

// String returns the assembler mnemonic, or "Opcode(n)" for an invalid code.
func (o Opcode) String() string {
	if int(o) < len(opcodeNames) {
		return opcodeNames[o]
	}
	return "Opcode(" + strconv.Itoa(int(o)) + ")"
}

// String returns the assembler mnemonic, or "Modifier(n)" for an invalid code.
func (m Modifier) String() string {
	if int(m) < len(modifierNames) {
		return modifierNames[m]
	}
	return "Modifier(" + strconv.Itoa(int(m)) + ")"
}

// String returns the assembler mnemonic token, or "Mode(n)" for an invalid code.
func (m Mode) String() string {
	if int(m) < len(modeNames) {
		return modeNames[m]
	}
	return "Mode(" + strconv.Itoa(int(m)) + ")"
}

// Cell is one core cell (docs/spec/isa.md section 2). AValue and BValue are
// stored values in [0, CORESIZE) for a cell in a core; the assembler emits raw
// signed values (C69) and the arena's loader reduces them with Norm when it places
// a bot. Writer is the id of the last bot that wrote the cell, 0 if none; it is
// metadata and not part of the instruction.
type Cell struct {
	Opcode   Opcode
	Modifier Modifier
	AMode    Mode
	AValue   int32
	BMode    Mode
	BValue   int32
	Writer   uint32
}

// EmptyCell is DAT.F $0, $0 with writer 0, the content of every cell of a fresh core.
var EmptyCell = Cell{Opcode: OpDAT, Modifier: ModF, AMode: ModeDirect, BMode: ModeDirect}
