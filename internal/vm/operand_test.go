// SPDX-License-Identifier: AGPL-3.0-or-later

package vm

import (
	"strconv"
	"strings"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

// parseCell reads the notation of docs/spec/isa.md section 2, for example
// "MOV.I @1, $-1". Negative values are normalized by the caller's core size n.
func parseCell(t *testing.T, n int, s string) isa.Cell {
	t.Helper()
	opMod, rest, ok := strings.Cut(strings.TrimSpace(s), " ")
	if !ok {
		t.Fatalf("bad cell %q", s)
	}
	opName, modName, ok := strings.Cut(opMod, ".")
	if !ok {
		t.Fatalf("bad cell %q: no modifier", s)
	}
	var cell isa.Cell
	found := 0
	for _, op := range isa.Opcodes {
		if op.String() == opName {
			cell.Opcode = op
			found++
		}
	}
	for _, m := range isa.Modifiers {
		if m.String() == modName {
			cell.Modifier = m
			found++
		}
	}
	if found != 2 {
		t.Fatalf("bad opcode or modifier in %q", s)
	}
	a, b, ok := strings.Cut(rest, ",")
	if !ok {
		t.Fatalf("bad cell %q: need two operands", s)
	}
	parseOperand := func(op string) (isa.Mode, int32) {
		op = strings.TrimSpace(op)
		for _, m := range isa.Modes {
			if strings.HasPrefix(op, m.String()) {
				v, err := strconv.ParseInt(op[len(m.String()):], 10, 64)
				if err != nil {
					t.Fatalf("bad operand %q in %q", op, s)
				}
				c, _ := NewCore(n)
				return m, c.Norm(v)
			}
		}
		t.Fatalf("bad mode in operand %q of %q", op, s)
		return 0, 0
	}
	cell.AMode, cell.AValue = parseOperand(a)
	cell.BMode, cell.BValue = parseOperand(b)
	return cell
}

type cellAt struct {
	addr int64
	text string
}

// modeCase is one addressing-mode case from docs/spec/isa.md section 6. The
// executing cell is at address 0 (PC = 0). The MOV write itself belongs to S06;
// here we check what operand evaluation yields: tA, tB, the snapshots and the
// writes to the pointer cells.
type modeCase struct {
	name       string
	n          int
	before     []cellAt
	tA, tB     int32
	snapA      string
	snapB      string
	afterA     []cellAt // if non-nil: pointer-cell writes after evaluating A, before B
	mid        []cellAt // pointer-cell writes after evaluating A then B (pre-decrements)
	afterPostA []cellAt // if non-nil: pointer-cell writes after A's post-increment, before B's
	final      []cellAt // pointer-cell writes after the post-increments too
	finalNil   bool     // true when final equals mid
}

const empty = "DAT.F $0, $0"

var modeCases = []modeCase{
	{name: "#", n: 8, before: []cellAt{{0, "MOV.AB #4, $7"}},
		tA: 0, tB: 7, snapA: "MOV.AB #4, $7", snapB: empty, finalNil: true},
	{name: "$", n: 8, before: []cellAt{{0, "MOV.I $3, $7"}, {3, "ADD.AB #1, #2"}},
		tA: 3, tB: 7, snapA: "ADD.AB #1, #2", snapB: empty, finalNil: true},
	{name: "*", n: 8, before: []cellAt{{0, "MOV.I *1, $7"}, {1, "DAT.F $2, $0"}, {3, "ADD.AB #1, #2"}},
		tA: 3, tB: 7, snapA: "ADD.AB #1, #2", snapB: empty, finalNil: true},
	{name: "@", n: 8, before: []cellAt{{0, "MOV.I @1, $7"}, {1, "DAT.F $0, $2"}, {3, "ADD.AB #1, #2"}},
		tA: 3, tB: 7, snapA: "ADD.AB #1, #2", snapB: empty, finalNil: true},
	{name: "{", n: 8, before: []cellAt{{0, "MOV.I {1, $7"}, {1, "DAT.F $3, $0"}, {3, "ADD.AB #1, #2"}},
		tA: 3, tB: 7, snapA: "ADD.AB #1, #2", snapB: empty,
		mid: []cellAt{{1, "DAT.F $2, $0"}}, finalNil: true},
	{name: "<", n: 8, before: []cellAt{{0, "MOV.I <1, $7"}, {1, "DAT.F $0, $3"}, {3, "ADD.AB #1, #2"}},
		tA: 3, tB: 7, snapA: "ADD.AB #1, #2", snapB: empty,
		mid: []cellAt{{1, "DAT.F $0, $2"}}, finalNil: true},
	{name: "}", n: 8, before: []cellAt{{0, "MOV.I }1, $7"}, {1, "DAT.F $2, $0"}, {3, "ADD.AB #1, #2"}},
		tA: 3, tB: 7, snapA: "ADD.AB #1, #2", snapB: empty,
		final: []cellAt{{1, "DAT.F $3, $0"}}},
	{name: ">", n: 8, before: []cellAt{{0, "MOV.I >1, $7"}, {1, "DAT.F $0, $2"}, {3, "ADD.AB #1, #2"}},
		tA: 3, tB: 7, snapA: "ADD.AB #1, #2", snapB: empty,
		final: []cellAt{{1, "DAT.F $0, $3"}}},

	{name: "#/B", n: 8, before: []cellAt{{0, "MOV.A $1, #0"}, {1, "DAT.F $6, $0"}},
		tA: 1, tB: 0, snapA: "DAT.F $6, $0", snapB: "MOV.A $1, #0", finalNil: true},
	{name: "@/wrap", n: 8, before: []cellAt{{0, "MOV.I @1, $7"}, {1, "DAT.F $0, $7"}},
		tA: 0, tB: 7, snapA: "MOV.I @1, $7", snapB: empty, finalNil: true},
	{name: "</wrap", n: 8, before: []cellAt{{0, "MOV.I <1, $7"}, {1, "DAT.F $0, $0"}},
		tA: 0, tB: 7, snapA: "MOV.I <1, $7", snapB: empty,
		mid: []cellAt{{1, "DAT.F $0, $7"}}, finalNil: true},
	{name: "</order", n: 8,
		before: []cellAt{{0, "MOV.I <1, <1"}, {1, "DAT.F $0, $4"}, {3, "ADD.AB #1, #2"}, {4, "SUB.F $1, $1"}},
		tA:     4, tB: 3, snapA: "SUB.F $1, $1", snapB: "ADD.AB #1, #2",
		afterA: []cellAt{{1, "DAT.F $0, $3"}},
		mid:    []cellAt{{1, "DAT.F $0, $2"}}, finalNil: true},
	{name: ">/order", n: 8,
		before: []cellAt{{0, "MOV.AB >1, >1"}, {1, "DAT.F $0, $2"}, {3, "DAT.F $5, $6"}},
		tA:     3, tB: 3, snapA: "DAT.F $5, $6", snapB: "DAT.F $5, $6",
		afterPostA: []cellAt{{1, "DAT.F $0, $3"}},
		final:      []cellAt{{1, "DAT.F $0, $4"}}},
	{name: "</self", n: 8, before: []cellAt{{0, "MOV.I <1, $7"}, {1, "DAT.F $0, $1"}},
		tA: 1, tB: 7, snapA: "DAT.F $0, $0", snapB: empty, // the snapshot sees the predecrement
		mid: []cellAt{{1, "DAT.F $0, $0"}}, finalNil: true},
	{name: "$/size1", n: 1, before: []cellAt{{0, "MOV.AB #0, $0"}},
		tA: 0, tB: 0, snapA: "MOV.AB #0, $0", snapB: "MOV.AB #0, $0", finalNil: true},
}

// runModeCase evaluates operand A then B of the cell at PC 0, applies the
// post-increments and checks every cell of the core against the expectation.
func runModeCase(t *testing.T, tc modeCase) {
	t.Helper()
	c, err := NewCore(tc.n)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range tc.before {
		c.Write(b.addr, parseCell(t, tc.n, b.text), 0) // setup writes carry writer 0
	}
	init := snapshotCore(c)

	ir := c.Read(0)
	a, err := c.Resolve(0, ir.AMode, ir.AValue, 1)
	if err != nil {
		t.Fatalf("Resolve A: %v", err)
	}
	if tc.afterA != nil {
		checkCore(t, "after evaluating A", c, init, tc.n, tc.afterA)
	}
	b, err := c.Resolve(0, ir.BMode, ir.BValue, 1)
	if err != nil {
		t.Fatalf("Resolve B: %v", err)
	}
	if a.Target != tc.tA {
		t.Errorf("tA = %d, want %d", a.Target, tc.tA)
	}
	if b.Target != tc.tB {
		t.Errorf("tB = %d, want %d", b.Target, tc.tB)
	}
	// Snapshots may carry the writer id of the target; compare instruction fields only.
	if got, want := withoutWriter(a.Snapshot), parseCell(t, tc.n, tc.snapA); got != want {
		t.Errorf("A* = %+v, want %+v", got, want)
	}
	if got, want := withoutWriter(b.Snapshot), parseCell(t, tc.n, tc.snapB); got != want {
		t.Errorf("B* = %+v, want %+v", got, want)
	}
	checkCore(t, "after evaluating A and B", c, init, tc.n, tc.mid)

	c.ApplyPostInc(a.Post, 1)
	if tc.afterPostA != nil {
		checkCore(t, "after A's post-increment", c, init, tc.n, tc.afterPostA)
	}
	c.ApplyPostInc(b.Post, 1)
	final := tc.final
	if tc.finalNil {
		final = tc.mid
	}
	checkCore(t, "after post-increments", c, init, tc.n, final)
}

func withoutWriter(c isa.Cell) isa.Cell {
	c.Writer = 0
	return c
}

func snapshotCore(c *Core) []isa.Cell {
	out := make([]isa.Cell, c.Size())
	for i := range out {
		out[i] = c.Read(int64(i))
	}
	return out
}

// checkCore verifies that the cells listed in changed hold the given contents
// with writer 1, and that every other cell equals init exactly.
func checkCore(t *testing.T, when string, c *Core, init []isa.Cell, n int, changed []cellAt) {
	t.Helper()
	for i := range init {
		got := c.Read(int64(i))
		want := init[i]
		for _, ch := range changed {
			if ch.addr == int64(i) {
				want = parseCell(t, n, ch.text)
				want.Writer = 1
			}
		}
		if got != want {
			t.Errorf("%s: cell %d = %+v, want %+v", when, i, got, want)
		}
	}
}

// TestAddressingModes has one row per addressing mode, named by mnemonic, from
// the unsuffixed cases of docs/spec/isa.md section 6.
func TestAddressingModes(t *testing.T) {
	for _, tc := range modeCases[:8] {
		t.Run(tc.name, func(t *testing.T) { runModeCase(t, tc) })
	}
}

// TestAddressingModeEdges runs the suffixed cases: immediate B, wrap, evaluation
// order and N = 1.
func TestAddressingModeEdges(t *testing.T) {
	for _, tc := range modeCases[8:] {
		t.Run(tc.name, func(t *testing.T) { runModeCase(t, tc) })
	}
}

func TestAddressingModeRowsCoverEveryMode(t *testing.T) {
	for i, m := range isa.Modes {
		if modeCases[i].name != m.String() {
			t.Errorf("modeCases[%d] is %q, want mode %q", i, modeCases[i].name, m.String())
		}
	}
}

func TestResolveWrapsPC(t *testing.T) {
	c, _ := NewCore(8)
	c.Write(7, parseCell(t, 8, "DAT.F $5, $6"), 0)
	for _, tc := range []struct {
		name  string
		pc    int32
		mode  isa.Mode
		value int32
		want  int32
	}{
		{"direct wraps past the end", 7, isa.ModeDirect, 3, 2},
		{"direct reaches cell 0", 7, isa.ModeDirect, 1, 0},
		{"negative value wraps", 1, isa.ModeDirect, -3, 6},
		{"large value wraps", 1, isa.ModeDirect, 1<<31 - 1, 0}, // 1 + 2147483647 = 2^31 = 0 mod 8
		{"immediate is PC", 5, isa.ModeImmediate, 6, 5},
		{"A-indirect wraps", 6, isa.ModeAIndirect, 1, 4}, // pointer 7, A = 5: 6+1+5 = 12 -> 4
		{"B-indirect wraps", 6, isa.ModeBIndirect, 1, 5}, // pointer 7, B = 6: 6+1+6 = 13 -> 5
		{"A-postinc uses old A", 6, isa.ModeAPostInc, 1, 4},
		{"B-postinc uses old B", 6, isa.ModeBPostInc, 1, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cc, _ := NewCore(8)
			cc.Write(7, c.Read(7), 0)
			op, err := cc.Resolve(tc.pc, tc.mode, tc.value, 1)
			if err != nil {
				t.Fatal(err)
			}
			if op.Target != tc.want {
				t.Errorf("Target = %d, want %d", op.Target, tc.want)
			}
		})
	}
}

func TestResolveWritesAsExecutingBot(t *testing.T) {
	c, _ := NewCore(8)
	c.Write(1, parseCell(t, 8, "DAT.F $3, $3"), 0)
	if _, err := c.Resolve(0, isa.ModeAPreDec, 1, 5); err != nil {
		t.Fatal(err)
	}
	if got := c.Read(1); got.Writer != 5 || got.AValue != 2 || got.BValue != 3 {
		t.Errorf("after predecrement by bot 5: %+v", got)
	}
	// A write that stores the old value is still a write: N = 1.
	c1, _ := NewCore(1)
	if _, err := c1.Resolve(0, isa.ModeBPreDec, 0, 3); err != nil {
		t.Fatal(err)
	}
	if got := c1.Read(0).Writer; got != 3 {
		t.Errorf("N=1 predecrement writer = %d, want 3", got)
	}
	c1b, _ := NewCore(1)
	op, err := c1b.Resolve(0, isa.ModeAPostInc, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := c1b.Read(0).Writer; got != 0 {
		t.Errorf("Resolve applied the post-increment early: writer %d", got)
	}
	c1b.ApplyPostInc(op.Post, 3)
	if got := c1b.Read(0).Writer; got != 3 {
		t.Errorf("N=1 postincrement writer = %d, want 3", got)
	}
}

func TestPostIncUsesCurrentField(t *testing.T) {
	// Spec section 4 step 5: the increment applies to the field as it is in the core now.
	c, _ := NewCore(8)
	c.Write(1, parseCell(t, 8, "DAT.F $0, $2"), 0)
	op, err := c.Resolve(0, isa.ModeBPostInc, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	c.Write(1, parseCell(t, 8, "DAT.F $4, $6"), 2) // the opcode overwrites the pointer cell
	c.ApplyPostInc(op.Post, 1)
	want := parseCell(t, 8, "DAT.F $4, $7")
	want.Writer = 1
	if got := c.Read(1); got != want {
		t.Errorf("cell 1 = %+v, want %+v", got, want)
	}
}

func TestPostIncWraps(t *testing.T) {
	c, _ := NewCore(8)
	c.Write(1, parseCell(t, 8, "DAT.F $7, $7"), 0)
	opA, _ := c.Resolve(0, isa.ModeAPostInc, 1, 1)
	c.ApplyPostInc(opA.Post, 1)
	opB, _ := c.Resolve(0, isa.ModeBPostInc, 1, 1)
	c.ApplyPostInc(opB.Post, 1)
	if got := c.Read(1); got.AValue != 0 || got.BValue != 0 {
		t.Errorf("cell 1 = %+v, want both fields 0", got)
	}
	c.ApplyPostInc(PostInc{}, 1) // no post-increment: nothing happens
}

func TestApplyPostIncIgnoresBadField(t *testing.T) {
	c, _ := NewCore(8)
	c.Write(1, parseCell(t, 8, "DAT.F $3, $4"), 0)
	before := snapshotCore(c)
	c.ApplyPostInc(PostInc{Field: PostField(3), Pointer: 1}, 1)
	c.ApplyPostInc(PostInc{Field: PostField(255), Pointer: 1}, 1)
	for i, cell := range snapshotCore(c) {
		if cell != before[i] {
			t.Errorf("cell %d changed by a PostInc with an invalid field: %+v", i, cell)
		}
	}
}

func TestResolveRejectsBadMode(t *testing.T) {
	c, _ := NewCore(8)
	before := snapshotCore(c)
	for _, m := range []isa.Mode{8, 9, 255} {
		if _, err := c.Resolve(0, m, 1, 1); err == nil {
			t.Errorf("Resolve with mode %d returned no error", m)
		}
	}
	for i, cell := range snapshotCore(c) {
		if cell != before[i] {
			t.Errorf("cell %d changed by a rejected Resolve", i)
		}
	}
}
