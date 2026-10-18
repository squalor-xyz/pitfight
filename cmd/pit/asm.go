// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/squalor-xyz/pitfight/internal/asm"
)

const asmUsage = "usage: pit asm <file>\n"

// runAsm implements "pit asm <file>": assemble the file and print the listing of
// docs/spec/asm.md section 9. args excludes "asm".
func runAsm(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprint(stderr, asmUsage)
		return 2
	}
	path := args[0]
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "pit asm: %v\n", err)
		return 1
	}
	prog, err := asm.Assemble(path, src)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := stdout.Write(listing(prog)); err != nil {
		fmt.Fprintf(stderr, "pit asm: %v\n", err)
		return 1
	}
	return 0
}

// listing formats p as docs/spec/asm.md section 9 defines: an ORG line, then one
// line per cell. The result is valid source that assembles to p.
func listing(p asm.Program) []byte {
	var b bytes.Buffer
	b.WriteString("ORG " + strconv.Itoa(p.Entry) + "\n")
	for _, c := range p.Cells {
		fmt.Fprintf(&b, "%s.%s %s%d, %s%d\n", c.Opcode, c.Modifier, c.AMode, c.AValue, c.BMode, c.BValue)
	}
	return b.Bytes()
}
