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
