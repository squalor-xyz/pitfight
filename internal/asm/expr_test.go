// SPDX-License-Identifier: AGPL-3.0-or-later

package asm

import "testing"

// TestExpr assembles "DAT #<expr>, #0" (an immediate operand keeps the value as
// written) and compares the A value with docs/spec/asm.md section 6.
func TestExpr(t *testing.T) {
	tests := []struct {
		expr string
		want int32
	}{
		{"0", 0},
		{"42", 42},
		{"007", 7},
		{"2147483647", 2147483647},
		// Precedence and associativity.
		{"1 + 2 * 3", 7},
		{"2 * 3 + 1", 7},
		{"(1 + 2) * 3", 9},
		{"10 - 4 - 3", 3},
		{"10 - (4 - 3)", 9},
		{"100 / 10 / 5", 2},
		{"100 / (10 / 5)", 50},
		{"2 * 3 % 4", 2},
		{"2 * (3 % 4)", 6},
		{"20 % 6 * 2", 4},
		{"1 + 7 % 4", 4},
		{"1 - 2 - 3 + 4", 0},
		{"2 * 3 * 4 / 6", 4},
		{"((((7))))", 7},
		// Negative values and unary operators.
		{"-1", -1},
		{"-2 * 3 + 4 - 1", -3},
		{"+5", 5},
		{"- (2 + 3)", -5},
		{"--3", 3},
		{"+-+3", -3},
		{"2 - -3", 5},
		{"2 * -3", -6},
		{"-3 * -3", 9},
		{"1 + -2 * 3", -5},
		{"-(1 + 2) * 3", -9},
		{"-2147483647 - 1", -2147483648},
		{"-2147483648", -2147483648},
		{"2147483648 - 1", 2147483647},
		{"-(2147483648)", -2147483648},
		// Division and modulo truncate toward zero; % has the dividend's sign.
		{"7 / 2", 3},
		{"-7 / 2", -3},
		{"7 / -2", -3},
		{"-7 / -2", 3},
		{"7 % 3", 1},
		{"-7 % 3", -1},
		{"7 % -3", 1},
		{"-7 % -3", -1},
		{"1 / 2", 0},
		// Whitespace and parentheses.
		{"  ( 1+2 )*3  ", 9},
		{"1+2*3", 7},
	}
	for _, tt := range tests {
		p := mustAssemble(t, "DAT #"+tt.expr+", #0")
		if got := p.Cells[0].AValue; got != tt.want {
			t.Errorf("%q = %d, want %d", tt.expr, got, tt.want)
		}
	}
}

// TestExprLabels checks label values in expressions: relative to the instruction
// holding the operand, so that a difference of labels is the plain distance.
func TestExprLabels(t *testing.T) {
	tests := []struct {
		name string
		src  string
		a, b int32 // operand values of the last instruction
	}{
		{"label is relative", "x: DAT 0\nDAT x, x + 1", -1, 0},
		{"forward label", "DAT end, 0\nend:", 1, 0},
		{"label plus offset", "a: DAT 0\nDAT a + 3, a - 3", 2, -4},
		{"label times constant", "a: DAT 0\nDAT 2 * a, 0", -2, 0},
		{"difference of labels is the distance", "a: DAT 0\nDAT 0\nb: DAT #b - a, #a - b", 2, -2},
		{"difference with a forward label", "a: DAT 0\nDAT 0\nDAT #b - a, #a - b\nb:", 3, -3},
		{"parenthesized label", "a: DAT 0\nDAT (a), -(a)", -1, 1},
		{"star as mode then multiplication", "x: DAT 0\nDAT *x*2, 0", -2, 0}, // an operator here would be an error
		{"star after an operand is multiplication", "DAT 2 * 3, 4 * 5", 6, 20},
	}
	for _, tt := range tests {
		p := mustAssemble(t, tt.src)
		last := p.Cells[len(p.Cells)-1]
		if last.AValue != tt.a || last.BValue != tt.b {
			t.Errorf("%s: last cell operands %d, %d; want %d, %d", tt.name, last.AValue, last.BValue, tt.a, tt.b)
		}
	}
}
