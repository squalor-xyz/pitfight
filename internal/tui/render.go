// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Grid size limits. The defaults give a screen of at most 24 lines by 80 columns
// for a core of up to 8000 cells and two bots; the maximum bounds memory for a
// caller that passes a huge size.
const (
	DefaultCols = 72
	DefaultRows = 20
	maxCols     = 1000
	maxRows     = 1000

	// maxBotLines is how many bots get a line of their own below the header.
	maxBotLines = 8
)

// HelpLine is the last line of every drawing.
const HelpLine = "s [N] step  b [N] back  g N seek  p [N] play/pause  q quit"

// Options sets the size of the drawn grid in characters. A value below 1 means the
// default.
type Options struct{ Cols, Rows int }

// Screen is everything one drawing shows.
type Screen struct {
	Frame   Frame
	Total   int    // the last cycle of the replay
	Playing bool   // the player state
	Message string // a one-line notice drawn above the help line; empty for none
}

// glyphs are the characters of bot ids 1, 2, ...; ids beyond 35 repeat them.
const glyphs = "123456789abcdefghijklmnopqrstuvwxyz"

// palette is the ANSI foreground colours of bot ids 1, 2, ...; ids beyond 12 repeat.
var palette = [...]int{31, 32, 33, 34, 35, 36, 91, 92, 93, 94, 95, 96}

// glyph is the character of a cell owner: '.' for no owner.
func glyph(id uint32) byte {
	if id == 0 {
		return '.'
	}
	return glyphs[(id-1)%uint32(len(glyphs))]
}

// colour is the ANSI foreground code of a bot, 0 for no owner.
func colour(id uint32) int {
	if id == 0 {
		return 0
	}
	return palette[(id-1)%uint32(len(palette))]
}

// layout returns how many cells one character stands for and how many grid rows
// that takes, for a core of n cells on a grid of cols by rows characters (both at
// least 1). A core that fits gets one cell per character.
func layout(n, cols, rows int) (block, nrows int) {
	if n <= 0 {
		return 1, 0
	}
	block = (n + cols*rows - 1) / (cols * rows)
	nrows = (n + block*cols - 1) / (block * cols)
	return block, nrows
}

// Render draws the screen to w as ANSI text. Every drawing starts with the escape
// sequences that home the cursor and clear the screen (ESC[H ESC[2J), so drawings
// can follow one another on a terminal. The text is:
//
//	pitfight  cycle 3/12  core 8000 (1 char = 6 cells)  paused
//	1 bot 1  live 1  pc 4531          (one line per bot, at most 8)
//	... and 3 more bots               (only if there are more)
//	   0 ..11112...                   (the grid: address of the row, then characters)
//	<message>                         (only if there is one)
//	s [N] step  ...                   (the help line)
//
// A character shows the owner of a block of cells as a glyph and colour (see
// glyph and colour); in a block of several cells it is the owner with most cells
// among the written ones, the lowest id on a tie, '.' if none. A character that
// holds an executed address of the frame is drawn in reverse video. Addresses
// outside the core and bots without a line are ignored; nothing in a Frame can
// make Render fail except w.
func Render(w io.Writer, s Screen, o Options) error {
	cols, rows := o.Cols, o.Rows
	if cols < 1 {
		cols = DefaultCols
	}
	if rows < 1 {
		rows = DefaultRows
	}
	cols, rows = min(cols, maxCols), min(rows, maxRows)

	f := s.Frame
	n := len(f.Owners)
	block, nrows := layout(n, cols, rows)

	var b bytes.Buffer
	b.WriteString("\x1b[H\x1b[2J")
	state := "paused"
	if s.Playing {
		state = "playing"
	}
	note := ""
	if block > 1 {
		note = fmt.Sprintf(" (1 char = %d cells)", block)
	}
	fmt.Fprintf(&b, "pitfight  cycle %d/%d  core %d%s  %s\n", f.Cycle, s.Total, n, note, state)

	for i, live := range f.Live {
		if i == maxBotLines {
			fmt.Fprintf(&b, "... and %d more bots\n", len(f.Live)-maxBotLines)
			break
		}
		id := uint32(i + 1)
		var pcs []string
		for _, p := range f.Executed {
			if p.Bot == id {
				pcs = append(pcs, strconv.Itoa(p.PC))
			}
		}
		pc := "-"
		if len(pcs) > 0 {
			pc = strings.Join(pcs, ",")
		}
		fmt.Fprintf(&b, "\x1b[0;%dm%c\x1b[0m bot %d  live %d  pc %s\n", colour(id), glyph(id), id, live, pc)
	}

	if nrows > 0 {
		nblocks := (n + block - 1) / block
		marked := make([]bool, nblocks)
		for _, p := range f.Executed {
			if p.PC >= 0 && p.PC < n {
				marked[p.PC/block] = true
			}
		}
		labelWidth := len(strconv.Itoa(n - 1))
		var tmp []uint32
		for r := 0; r < nrows; r++ {
			fmt.Fprintf(&b, "%*d ", labelWidth, r*cols*block)
			cur := "" // the SGR parameters in effect; "" is the default style
			for j := r * cols; j < min((r+1)*cols, nblocks); j++ {
				cells := f.Owners[j*block : min((j+1)*block, n)]
				var owner uint32
				owner, tmp = blockOwner(cells, tmp)
				st := ""
				if owner != 0 || marked[j] {
					st = "0"
					if marked[j] {
						st += ";7"
					}
					if owner != 0 {
						st += ";" + strconv.Itoa(colour(owner))
					}
				}
				if st != cur {
					if st == "" {
						b.WriteString("\x1b[0m")
					} else {
						b.WriteString("\x1b[" + st + "m")
					}
					cur = st
				}
				b.WriteByte(glyph(owner))
			}
			if cur != "" {
				b.WriteString("\x1b[0m")
			}
			b.WriteByte('\n')
		}
	}

	if s.Message != "" {
		b.WriteString(s.Message + "\n")
	}
	b.WriteString(HelpLine + "\n")
	_, err := w.Write(b.Bytes())
	return err
}

// blockOwner returns the owner shown for a block of cells: the written owner with
// most cells, the lowest id on a tie, 0 if no cell is written. tmp is scratch space
// that is returned for reuse.
func blockOwner(cells []uint32, tmp []uint32) (uint32, []uint32) {
	if len(cells) == 1 {
		return cells[0], tmp
	}
	tmp = tmp[:0]
	for _, c := range cells {
		if c != 0 {
			tmp = append(tmp, c)
		}
	}
	slices.Sort(tmp)
	var best uint32
	bestN := 0
	for i := 0; i < len(tmp); {
		j := i
		for j < len(tmp) && tmp[j] == tmp[i] {
			j++
		}
		if j-i > bestN {
			best, bestN = tmp[i], j-i
		}
		i = j
	}
	return best, tmp
}
