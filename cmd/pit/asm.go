// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/squalor-xyz/pitfight/internal/asm"
)

const asmUsage = "usage: pit asm <file>\n"

// runAsm implements "pit asm <file>": assemble the file and print the listing of
// docs/spec/asm.md section 9, which asm.Disassemble produces. args excludes "asm".
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
	if _, err := stdout.Write(asm.Disassemble(prog)); err != nil {
		fmt.Fprintf(stderr, "pit asm: %v\n", err)
		return 1
	}
	return 0
}
