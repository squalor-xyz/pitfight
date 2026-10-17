// SPDX-License-Identifier: AGPL-3.0-or-later
package vm

import (
	"github.com/squalor-xyz/pitfight/internal/isa"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type stepCase struct{ name, setup, before, after, queue string }

var stepCases = []stepCase{
	{"MOV.A", "N=8", "0: MOV.A $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6", "2: SLT.A $1, $6", "[1]"},
	{"MOV.B", "N=8", "0: MOV.B $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6", "2: SLT.A $5, $2", "[1]"},
	{"MOV.AB", "N=8", "0: MOV.AB $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6", "2: SLT.A $5, $1", "[1]"},
	{"MOV.BA", "N=8", "0: MOV.BA $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6", "2: SLT.A $2, $6", "[1]"},
	{"MOV.F", "N=8", "0: MOV.F $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6", "2: SLT.A $1, $2", "[1]"},
	{"MOV.X", "N=8", "0: MOV.X $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6", "2: SLT.A $2, $1", "[1]"},
	{"MOV.I", "N=8", "0: MOV.I $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6", "2: ADD.AB #1, #2", "[1]"},
	{"DAT.F", "N=8", "0: DAT.F $3, $4", "-", "[]"},
	{"DAT.F/empty", "N=8", "-", "-", "[]"},
	{"DAT.F/predec", "N=8", "0: DAT.F <1, $0; 1: DAT.F $0, $3", "1: DAT.F $0, $2", "[]"},
	{"DAT.F/postinc", "N=8", "0: DAT.F }1, $0; 1: DAT.F $2, $0", "1: DAT.F $3, $0", "[]"},
	{"ADD.AB", "N=8", "0: ADD.AB #3, $1; 1: DAT.F $1, $2", "1: DAT.F $1, $5", "[1]"},
	{"ADD.F", "N=8", "0: ADD.F $1, $2; 1: DAT.F $2, $3; 2: DAT.F $4, $5", "2: DAT.F $6, $0", "[1]"},
	{"SUB.X", "N=8", "0: SUB.X $1, $2; 1: DAT.F $2, $3; 2: DAT.F $1, $1", "2: DAT.F $6, $7", "[1]"},
	{"MUL.I", "N=8", "0: MUL.I $1, $2; 1: DAT.F $3, $2; 2: DAT.F $5, $4", "2: DAT.F $7, $0", "[1]"},
	{"DIV.BA", "N=8", "0: DIV.BA $1, $2; 1: DAT.F $1, $2; 2: DAT.F $7, $5", "2: DAT.F $3, $5", "[1]"},
	{"DIV.B/zero", "N=8", "0: DIV.B $1, $2; 1: DAT.F $1, $0; 2: DAT.F $4, $4", "-", "[]"},
	{"DIV.F/zero", "N=8", "0: DIV.F $1, $2; 1: DAT.F $2, $0; 2: DAT.F $6, $6", "-", "[]"},
	{"JMP.B", "N=8", "0: JMP.B $3, $0", "-", "[3]"},
	{"JMZ.B", "N=8", "0: JMZ.B $3, $1; 1: DAT.F $5, $0", "-", "[3]"},
	{"JMZ.B/no", "N=8", "0: JMZ.B $3, $1; 1: DAT.F $5, $1", "-", "[1]"},
	{"JMZ.F", "N=8", "0: JMZ.F $3, $1; 1: DAT.F $0, $2", "-", "[1]"},
	{"JMN.B", "N=8", "0: JMN.B $3, $1; 1: DAT.F $0, $2", "-", "[3]"},
	{"JMN.F", "N=8", "0: JMN.F $3, $1; 1: DAT.F $0, $2", "-", "[3]"},
	{"DJN.B", "N=8", "0: DJN.B $3, $1; 1: DAT.F $0, $2", "1: DAT.F $0, $1", "[3]"},
	{"DJN.B/exit", "N=8", "0: DJN.B $3, $1; 1: DAT.F $0, $1", "1: DAT.F $0, $0", "[1]"},
	{"DJN.B/wrap", "N=8", "0: DJN.B $3, $1; 1: DAT.F $0, $0", "1: DAT.F $0, $7", "[3]"},
	{"DJN.F", "N=8", "0: DJN.F $3, $1; 1: DAT.F $1, $3", "1: DAT.F $0, $2", "[3]"},
	{"SPL.B", "N=8", "0: SPL.B $3, $0", "-", "[1, 3]"},
	{"SPL.B/cap", "N=8, MAXPROCESSES=1", "0: SPL.B $3, $0", "-", "[1]"},
	{"SEQ.I", "N=8", "0: SEQ.I $1, $2; 1: ADD.AB #1, #2; 2: ADD.AB #1, #2", "-", "[2]"},
	{"SEQ.I/mode", "N=8", "0: SEQ.I $1, $2; 1: ADD.AB #1, #2; 2: ADD.AB $1, #2", "-", "[1]"},
	{"SEQ.A", "N=8", "0: SEQ.A $1, $2; 1: DAT.F $4, $1; 2: DAT.F $4, $2", "-", "[2]"},
	{"SNE.B", "N=8", "0: SNE.B $1, $2; 1: DAT.F $0, $1; 2: DAT.F $0, $2", "-", "[2]"},
	{"SNE.F", "N=8", "0: SNE.F $1, $2; 1: DAT.F $1, $2; 2: DAT.F $1, $3", "-", "[2]"},
	{"SNE.I/equal", "N=8", "0: SNE.I $1, $2; 1: ADD.AB #1, #2; 2: ADD.AB #1, #2", "-", "[1]"},
	{"SLT.AB", "N=8", "0: SLT.AB #2, $1; 1: DAT.F $0, $5", "-", "[2]"},
	{"SLT.AB/no", "N=8", "0: SLT.AB #2, $1; 1: DAT.F $0, $2", "-", "[1]"},
	{"SLT.F", "N=8", "0: SLT.F $1, $2; 1: DAT.F $1, $5; 2: DAT.F $2, $5", "-", "[1]"},
	{"SLT.X", "N=8", "0: SLT.X $1, $2; 1: DAT.F $1, $2; 2: DAT.F $3, $2", "-", "[2]"},
	{"NOP.F", "N=8", "0: NOP.F $1, $2", "-", "[1]"},
	{"NOP.F/side", "N=8", "0: NOP.F >1, $0; 1: DAT.F $0, $2", "1: DAT.F $0, $3", "[1]"},
	{"#", "N=8", "0: MOV.AB #4, $7", "7: DAT.F $0, $4", "[1]"},
	{"$", "N=8", "0: MOV.I $3, $7; 3: ADD.AB #1, #2", "7: ADD.AB #1, #2", "[1]"},
	{"*", "N=8", "0: MOV.I *1, $7; 1: DAT.F $2, $0; 3: ADD.AB #1, #2", "7: ADD.AB #1, #2", "[1]"},
	{"@", "N=8", "0: MOV.I @1, $7; 1: DAT.F $0, $2; 3: ADD.AB #1, #2", "7: ADD.AB #1, #2", "[1]"},
	{"{", "N=8", "0: MOV.I {1, $7; 1: DAT.F $3, $0; 3: ADD.AB #1, #2", "1: DAT.F $2, $0; 7: ADD.AB #1, #2", "[1]"},
	{"<", "N=8", "0: MOV.I <1, $7; 1: DAT.F $0, $3; 3: ADD.AB #1, #2", "1: DAT.F $0, $2; 7: ADD.AB #1, #2", "[1]"},
	{"}", "N=8", "0: MOV.I }1, $7; 1: DAT.F $2, $0; 3: ADD.AB #1, #2", "7: ADD.AB #1, #2; 1: DAT.F $3, $0", "[1]"},
	{">", "N=8", "0: MOV.I >1, $7; 1: DAT.F $0, $2; 3: ADD.AB #1, #2", "7: ADD.AB #1, #2; 1: DAT.F $0, $3", "[1]"},
	{"#/B", "N=8", "0: MOV.A $1, #0; 1: DAT.F $6, $0", "0: MOV.A $6, #0", "[1]"},
	{"@/wrap", "N=8", "0: MOV.I @1, $7; 1: DAT.F $0, $7", "7: MOV.I @1, $7", "[1]"},
	{"</wrap", "N=8", "0: MOV.I <1, $7; 1: DAT.F $0, $0", "1: DAT.F $0, $7; 7: MOV.I <1, $7", "[1]"},
	{"</order", "N=8", "0: MOV.I <1, <1; 1: DAT.F $0, $4; 3: ADD.AB #1, #2; 4: SUB.F $1, $1", "1: DAT.F $0, $3; 1: DAT.F $0, $2; 3: SUB.F $1, $1", "[1]"},
	{">/order", "N=8", "0: MOV.AB >1, >1; 1: DAT.F $0, $2; 3: DAT.F $5, $6", "3: DAT.F $5, $5; 1: DAT.F $0, $3; 1: DAT.F $0, $4", "[1]"},
	{"$/size1", "N=1", "0: MOV.AB #0, $0", "0: MOV.AB #0, $0", "[0]"},
}

func setupStep(t *testing.T, tc stepCase) (*Core, *Queue, []isa.Cell) {
	t.Helper()
	n, max := 8, 8
	for _, v := range strings.Split(tc.setup, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(v), "=")
		x, e := strconv.Atoi(v)
		if e != nil {
			t.Fatal(e)
		}
		if k == "N" {
			n = x
		} else if k == "MAXPROCESSES" {
			max = x
		} else {
			t.Fatal(k)
		}
	}
	c, _ := NewCore(n)
	for _, v := range parseWrites(t, tc.before) {
		c.Write(v.addr, parseCell(t, n, v.text), 0)
	}
	q, e := c.NewQueue(max, 0)
	if e != nil {
		t.Fatal(e)
	}
	return c, q, snapshotCore(c)
}
func parseWrites(t *testing.T, s string) []cellAt {
	t.Helper()
	if s == "-" {
		return nil
	}
	var out []cellAt
	for _, v := range strings.Split(s, ";") {
		a, b, ok := strings.Cut(strings.TrimSpace(v), ": ")
		if !ok {
			t.Fatal(v)
		}
		x, e := strconv.ParseInt(a, 10, 64)
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, cellAt{x, b})
	}
	return out
}
func wantQueue(t *testing.T, s string) []int32 {
	t.Helper()
	var out []int32
	for _, v := range strings.Split(strings.Trim(s, "[]"), ",") {
		if strings.TrimSpace(v) == "" {
			continue
		}
		x, e := strconv.Atoi(strings.TrimSpace(v))
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, int32(x))
	}
	return out
}
func TestOpcodes(t *testing.T) {
	for _, tc := range stepCases {
		t.Run(tc.name, func(t *testing.T) {
			c, q, init := setupStep(t, tc)
			c.Step(q, 1, nil)
			checkCore(t, "step", c, init, c.Size(), parseWrites(t, tc.after))
			if got, want := q.Addresses(), wantQueue(t, tc.queue); !slices.Equal(got, want) {
				t.Fatalf("queue %v want %v", got, want)
			}
		})
	}
}
func TestEvents(t *testing.T) {
	for _, tc := range stepCases {
		t.Run(tc.name, func(t *testing.T) {
			c, q, _ := setupStep(t, tc)
			prefix := Event{Kind: ProcessSpawn, Bot: 99, PC: 7, Address: 7}
			got := c.Step(q, 1, []Event{prefix})
			want := []Event{prefix}
			for _, v := range parseWrites(t, tc.after) {
				cell := parseCell(t, c.Size(), v.text)
				cell.Writer = 1
				want = append(want, Event{Kind: CellWrite, Bot: 1, PC: 0, Address: int32(v.addr), Cell: cell})
			}
			if tc.queue == "[]" {
				want = append(want, Event{Kind: ProcessDeath, Bot: 1, PC: 0, Address: 0})
			} else if strings.HasPrefix(tc.name, "SPL.") && tc.name != "SPL.B/cap" {
				want = append(want, Event{Kind: ProcessSpawn, Bot: 1, PC: 0, Address: 3})
			}
			if !slices.Equal(got, want) {
				t.Fatalf("events\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

// Explicit destinations for asymmetric fields distinguish every pair selection.
func TestArithmeticModifiers(t *testing.T) {
	for _, tc := range []struct {
		op   string
		want [7][2]int32
	}{
		{"ADD", [7][2]int32{{9, 5}, {7, 8}, {7, 7}, {10, 5}, {9, 8}, {10, 7}, {9, 8}}},
		{"SUB", [7][2]int32{{5, 5}, {7, 2}, {7, 3}, {4, 5}, {5, 2}, {4, 3}, {5, 2}}},
		{"MUL", [7][2]int32{{14, 5}, {7, 15}, {7, 10}, {21, 5}, {14, 15}, {21, 10}, {14, 15}}},
		{"DIV", [7][2]int32{{3, 5}, {7, 1}, {7, 2}, {2, 5}, {3, 1}, {2, 2}, {3, 1}}},
	} {
		for i, m := range isa.Modifiers {
			t.Run(tc.op+"."+m.String(), func(t *testing.T) {
				c, _ := NewCore(32)
				c.Write(0, parseCell(t, 32, tc.op+"."+m.String()+" $1, $2"), 0)
				c.Write(1, parseCell(t, 32, "DAT.F $2, $3"), 9)
				c.Write(2, parseCell(t, 32, "SLT.X @7, *5"), 8)
				q, _ := c.NewQueue(1, 0)
				events := c.Step(q, 4, nil)
				want := parseCell(t, 32, "SLT.X @7, *5")
				want.AValue, want.BValue = tc.want[i][0], tc.want[i][1]
				want.Writer = 4
				if got := c.Read(2); got != want {
					t.Fatalf("got %+v want %+v", got, want)
				}
				if len(events) != 1 || events[0].Cell != want || !slices.Equal(q.Addresses(), []int32{1}) {
					t.Fatalf("events %v queue %v", events, q.Addresses())
				}
			})
		}
	}
}

func TestStepEdges(t *testing.T) {
	for _, tc := range []stepCase{
		{"fetched operands survive self decrement", "N=8", "0: MOV.AB <0, $2; 1: DAT.F $5, $6", "0: MOV.AB <0, $1; 2: DAT.F $0, $5", "[1]"},
		{"destination preserves predecrement", "N=8", "0: MOV.AB $2, <1; 1: DAT.F $7, $1; 2: DAT.F $4, $5", "1: DAT.F $7, $0; 1: DAT.F $7, $4", "[1]"},
		{"distinct post order", "N=8", "0: MOV.AB }1, >2; 1: DAT.F $2, $0; 2: DAT.F $0, $2; 3: DAT.F $5, $6", "4: DAT.F $0, $5; 1: DAT.F $3, $0; 2: DAT.F $0, $3", "[1]"},
		{"post uses opcode write", "N=8", "0: MOV.B $2, >1; 1: DAT.F $0, $0; 2: DAT.F $0, $6", "1: DAT.F $0, $6; 1: DAT.F $0, $7", "[1]"},
		{"division death after posts", "N=8", "0: DIV.F }1, >1; 1: DAT.F $2, $2; 3: DAT.F $1, $0", "1: DAT.F $3, $2; 1: DAT.F $3, $3", "[]"},
		{"skip wraps", "N=8", "0: JMP.F $7, $0; 7: SEQ.I $0, $0", "-", "[7]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, q, init := setupStep(t, tc)
			events := c.Step(q, 1, nil)
			checkCore(t, "step", c, init, c.Size(), parseWrites(t, tc.after))
			if !slices.Equal(q.Addresses(), wantQueue(t, tc.queue)) {
				t.Fatal(q.Addresses())
			}
			var writes []Event
			for _, e := range events {
				if e.Kind == CellWrite {
					writes = append(writes, e)
				}
			}
			want := parseWrites(t, tc.after)
			if len(writes) != len(want) {
				t.Fatal(events)
			}
			for i, v := range want {
				cell := parseCell(t, c.Size(), v.text)
				cell.Writer = 1
				if writes[i].Address != int32(v.addr) || writes[i].Cell != cell {
					t.Fatal(events)
				}
			}
			if tc.name == "skip wraps" {
				c.Step(q, 1, nil)
				if !slices.Equal(q.Addresses(), []int32{1}) {
					t.Fatal(q.Addresses())
				}
			}
		})
	}
}

func TestInstructionEqualityIgnoresWriter(t *testing.T) {
	for _, op := range []string{"SEQ", "SNE"} {
		c, _ := NewCore(8)
		c.Write(0, parseCell(t, 8, op+".I $1, $2"), 0)
		cell := parseCell(t, 8, "ADD.X @3, *4")
		c.Write(1, cell, 7)
		c.Write(2, cell, 8)
		q, _ := c.NewQueue(1, 0)
		c.Step(q, 1, nil)
		want := int32(2)
		if op == "SNE" {
			want = 1
		}
		if !slices.Equal(q.Addresses(), []int32{want}) {
			t.Fatal(op, q.Addresses())
		}
	}
}

func TestDivisionAtomic(t *testing.T) {
	for _, source := range []string{"DAT.F $0, $2", "DAT.F $2, $0"} {
		c, _ := NewCore(8)
		c.Write(0, parseCell(t, 8, "DIV.F $1, $2"), 0)
		c.Write(1, parseCell(t, 8, source), 0)
		c.Write(2, parseCell(t, 8, "DAT.F $6, $6"), 9)
		before := c.Read(2)
		q, _ := c.NewQueue(1, 0)
		events := c.Step(q, 1, nil)
		if !q.Dead() || c.Read(2) != before || len(events) != 1 || events[0].Kind != ProcessDeath {
			t.Fatalf("%s: cell %+v queue %v events %v", source, c.Read(2), q.Addresses(), events)
		}
	}
}

func TestMultiplyWide(t *testing.T) {
	n := int32(MaxCoreSize - 1) // A non-power-of-two modulus exposes int32 overflow.
	c, _ := NewCore(int(n))
	ir := isa.EmptyCell
	ir.Opcode = isa.OpMUL
	ir.AValue = 1
	ir.BValue = 2
	c.Write(0, ir, 0)
	cell := isa.EmptyCell
	cell.AValue = n - 1
	cell.BValue = n - 2
	c.Write(1, cell, 0)
	c.Write(2, cell, 0)
	q, _ := c.NewQueue(1, 0)
	c.Step(q, 1, nil)
	if got := c.Read(2); got.AValue != 1 || got.BValue != 4 {
		t.Fatal(got)
	}
}
