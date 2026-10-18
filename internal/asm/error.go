// SPDX-License-Identifier: AGPL-3.0-or-later

package asm

import "fmt"

// Error is an assembly error: where it is and what is wrong (docs/spec/asm.md
// section 7). Line and Col start at 1; Col counts bytes.
type Error struct {
	File string
	Line int
	Col  int
	Msg  string
}

// Error returns "file:line:col: message".
func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Msg)
}
