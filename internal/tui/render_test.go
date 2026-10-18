// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
)

var sgr = regexp.MustCompile("\x1b\\[[0-9;]*[mHJ]")

// plain removes the escape sequences of a drawing.
func plain(s string) string { return sgr.ReplaceAllString(s, "") }

func render(t testing.TB, s Screen, o Options) string {
	t.Helper()
	var b bytes.Buffer
	if err := Render(&b, s, o); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestRender draws cycle 12 of the fixture (from frames.json) on a grid of 8 by 4
// and compares the bytes with a golden file composed by hand from the layout rules
// in render.go: the header, one line per bot, the grid with address labels, the
// help line; colours by owner, reverse video on the executed cells 14 and 21.
func TestRender(t *testing.T) {
	ff := loadFrames(t)
	got := render(t, Screen{Frame: ff.Frames[12], Total: 12}, Options{Cols: 8, Rows: 4})
	want, err := os.ReadFile("testdata/render_cycle12.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("render differs from testdata/render_cycle12.golden\n got %q\nwant %q", got, want)
	}
}

func TestRenderStatus(t *testing.T) {
	ff := loadFrames(t)
	got := render(t, Screen{Frame: ff.Frames[5], Total: 12, Playing: true, Message: "unknown command \"x\""}, Options{Cols: 8, Rows: 4})
	lines := strings.Split(plain(got), "\n")
	if lines[0] != "pitfight  cycle 5/12  core 24  playing" {
		t.Errorf("header = %q", lines[0])
	}
	if len(lines) < 7 {
		t.Fatalf("only %d lines: %q", len(lines), lines)
	}
	if lines[1] != "1 bot 1  live 1  pc 7" || lines[2] != "2 bot 2  live 1  pc 20" {
		t.Errorf("bot lines = %q, %q", lines[1], lines[2])
	}
	n := len(lines)
	if lines[n-3] != "unknown command \"x\"" || !strings.HasPrefix(lines[n-2], "s [N] step") || lines[n-1] != "" {
		t.Errorf("last lines = %q", lines[n-3:])
	}
	// A bot without a step in the cycle shows "pc -".
	f := ff.Frames[0]
	got = plain(render(t, Screen{Frame: f, Total: 12}, Options{Cols: 8, Rows: 4}))
	if !strings.Contains(got, "1 bot 1  live 1  pc -\n") {
		t.Errorf("cycle 0 render lacks %q:\n%s", "pc -", got)
	}
}

func TestLayout(t *testing.T) {
	tests := []struct{ n, cols, rows, block, nrows int }{
		{8000, 72, 20, 6, 19},
		{1440, 72, 20, 1, 20},
		{1441, 72, 20, 2, 11},
		{24, 8, 4, 1, 3},
		{30, 5, 2, 3, 2},
		{1, 72, 20, 1, 1},
		{0, 72, 20, 1, 0},
		{1 << 20, 72, 20, 729, 20},
	}
	for _, tt := range tests {
		block, nrows := layout(tt.n, tt.cols, tt.rows)
		if block != tt.block || nrows != tt.nrows {
			t.Errorf("layout(%d, %d, %d) = %d, %d; want %d, %d", tt.n, tt.cols, tt.rows, block, nrows, tt.block, tt.nrows)
		}
	}
}

func TestGlyphAndColour(t *testing.T) {
	for id, want := range map[uint32]byte{0: '.', 1: '1', 9: '9', 10: 'a', 35: 'z', 36: '1', 37: '2'} {
		if got := glyph(id); got != want {
			t.Errorf("glyph(%d) = %q, want %q", id, got, want)
		}
	}
	for id, want := range map[uint32]int{1: 31, 6: 36, 7: 91, 12: 96, 13: 31} {
		if got := colour(id); got != want {
			t.Errorf("colour(%d) = %d, want %d", id, got, want)
		}
	}
}

// TestRenderBlocks: when the core is larger than the grid, a character stands for
// a block of cells. N = 30 on 5 columns and 2 rows gives blocks of 3 cells. The
// block shows the owner with most cells, an owner beating empty cells even as a
// minority, the lowest id on a tie; a block holding an executed address is reversed.
func TestRenderBlocks(t *testing.T) {
	owners := []uint32{
		0, 0, 0, // '.'
		2, 0, 0, // '2': a minority beats empty
		1, 2, 2, // '2': majority
		1, 2, 3, // '1': tie, lowest id
		3, 3, 0, // '3'
		0, 0, 0, // '.'
		1, 1, 1, // '1'
		0, 0, 0, // '.' with the pointer
		0, 4, 0, // '4'
		5, 0, 0, // '5'
	}
	f := Frame{Cycle: 3, Owners: owners, Executed: []Pointer{{Bot: 1, PC: 22}}, Live: []int{1}}
	got := render(t, Screen{Frame: f, Total: 9}, Options{Cols: 5, Rows: 2})
	lines := strings.Split(plain(got), "\n")
	if lines[0] != "pitfight  cycle 3/9  core 30 (1 char = 3 cells)  paused" {
		t.Errorf("header = %q", lines[0])
	}
	if len(lines) < 4 {
		t.Fatalf("only %d lines: %q", len(lines), lines)
	}
	if lines[2] != " 0 .2213" || lines[3] != "15 .1.45" {
		t.Errorf("grid = %q, %q", lines[2], lines[3])
	}
	const row1 = "15 .\x1b[0;31m1\x1b[0;7m.\x1b[0;34m4\x1b[0;35m5\x1b[0m\n"
	if !strings.Contains(got, row1) {
		t.Errorf("render lacks the styled row %q:\n%q", row1, got)
	}
}

// TestRenderLabels: row labels are as wide as the last address, right-aligned.
func TestRenderLabels(t *testing.T) {
	f := Frame{Owners: make([]uint32, 100)}
	lines := strings.Split(plain(render(t, Screen{Frame: f}, Options{Cols: 10, Rows: 10})), "\n")
	if len(lines) < 12 || lines[1] != " 0 .........." || lines[2] != "10 .........." || lines[10] != "90 .........." {
		t.Errorf("grid lines = %q", lines)
	}
}

// TestRenderDefaults: with no options a core of 8000 cells takes 19 lines of 72
// characters, 6 cells each, so a drawing with two bots fits 80 by 24.
func TestRenderDefaults(t *testing.T) {
	f := Frame{Owners: make([]uint32, 8000), Live: []int{1, 1}}
	out := plain(render(t, Screen{Frame: f, Total: 5}, Options{}))
	lines := strings.Split(out, "\n")
	if lines[0] != "pitfight  cycle 0/5  core 8000 (1 char = 6 cells)  paused" {
		t.Errorf("header = %q", lines[0])
	}
	if len(lines) != 1+2+19+1+1 { // header, bots, grid, help, and the empty piece after the last newline
		t.Errorf("%d lines, want 24 with the empty last piece", len(lines))
	}
	if want := "   0 " + strings.Repeat(".", 72); lines[3] != want {
		t.Errorf("first grid line = %q, want %q", lines[3], want)
	}
	if want := "7776 " + strings.Repeat(".", 38); lines[21] != want {
		t.Errorf("last grid line = %q, want %q", lines[21], want)
	}
	// The grid holds 72 * 20 = 1440 cells: a core of 1440 needs no blocks, 1441 does.
	for n, want := range map[int]string{1440: "pitfight  cycle 0/5  core 1440  paused", 1441: "pitfight  cycle 0/5  core 1441 (1 char = 2 cells)  paused"} {
		got := plain(render(t, Screen{Frame: Frame{Owners: make([]uint32, n)}, Total: 5}, Options{}))
		if first, _, _ := strings.Cut(got, "\n"); first != want {
			t.Errorf("core %d: header = %q, want %q", n, first, want)
		}
	}
}

// TestRenderDegenerate: nothing a frame can hold stops the renderer.
func TestRenderDegenerate(t *testing.T) {
	f := Frame{
		Cycle:    4,
		Owners:   []uint32{0, 70000, 3, 0, 0, 1 << 31, 1, 0},
		Executed: []Pointer{{Bot: 9, PC: 99}, {Bot: 1, PC: -1}, {Bot: 1, PC: 3}, {Bot: 2, PC: 8}},
		Live:     make([]int, 20),
	}
	got := plain(render(t, Screen{Frame: f, Total: 2}, Options{})) // zero options: defaults
	if !strings.Contains(got, "... and 12 more bots\n") {
		t.Errorf("no more-bots line in\n%s", got)
	}
	if !strings.Contains(got, "1 bot 1  live 0  pc -1,3\n") {
		t.Errorf("bot 1 line missing in\n%s", got)
	}
	if strings.Contains(got, "bot 9 ") {
		t.Errorf("a bot beyond Live has a line:\n%s", got)
	}
	big := render(t, Screen{Frame: f, Total: 2}, Options{Cols: 1 << 40, Rows: 1 << 40}) // clamped, not allocated
	if !strings.Contains(plain(big), "\n0 .") {
		t.Errorf("huge options: no grid in\n%s", plain(big))
	}
	render(t, Screen{}, Options{})                                                    // empty frame
	render(t, Screen{Frame: Frame{Owners: []uint32{1}}}, Options{Cols: -1, Rows: -1}) // negative sizes
}

func TestRenderWriteError(t *testing.T) {
	if err := Render(failWriter{}, Screen{Frame: Frame{Owners: []uint32{1}}}, Options{}); err == nil {
		t.Error("Render to a failing writer: want an error")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }
