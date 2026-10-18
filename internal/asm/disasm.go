// SPDX-License-Identifier: AGPL-3.0-or-later

package asm

import (
	"bytes"
	"fmt"
	"strconv"
)

// Disassemble formats p as source in the syntax of docs/spec/asm.md, in the listing
// form of section 9: an ORG line with the entry offset, then one line per cell with
// an explicit modifier, both modes and signed decimal values. No labels, no
// comments, every line ends with a line feed. Assemble of the result gives back the
// same Cells (with Writer 0, as the last-writer id is not part of the source) and
// the same Entry.
//
// The cells must be valid (every opcode, modifier and mode within its isa list) and
// p.Entry within the cells; otherwise the output uses the fallback names of the isa
// String methods and does not assemble.
func Disassemble(p Program) []byte {
	var b bytes.Buffer
	b.WriteString("ORG " + strconv.Itoa(p.Entry) + "\n")
	for _, c := range p.Cells {
		fmt.Fprintf(&b, "%s.%s %s%d, %s%d\n", c.Opcode, c.Modifier, c.AMode, c.AValue, c.BMode, c.BValue)
	}
	return b.Bytes()
}
