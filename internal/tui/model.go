// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tui is the terminal viewer: a pure frame model of a replay (this file),
// the step, play, pause and seek state machine over it (player.go) and an ANSI
// renderer (render.go). It uses the standard library only; the model does not
// depend on the renderer and is the reference for the web viewer (C19), which must
// produce the same frames for testdata/viewer/fixture.jsonl as
// testdata/viewer/frames.json.
package tui

import (
	"slices"

	"github.com/squalor-xyz/pitfight/internal/replay"
)

// Pointer is one executed address of a cycle: bot Bot executed the instruction at
// address PC. A replay's x record names only the process that stepped, so a bot with
// several processes has one pointer per cycle, not one per process.
type Pointer struct {
	Bot uint32 `json:"bot"`
	PC  int    `json:"pc"`
}

// Frame is the state of the match at the end of one cycle. The JSON tags are the
// field names of testdata/viewer/frames.json.
//
//   - Cycle: 0 is the loaded core before any step; c is the core after cycle c.
//   - Owners: one entry per core address, the id of the bot that last wrote the
//     cell, 0 for a cell nobody wrote. Bot cells count as written by their bot.
//   - Executed: the steps of cycle Cycle in execution order (ascending bot id): the
//     bot and the address it executed. Empty (never null) at cycle 0 and for a cycle
//     the replay has no steps for.
//   - Live: live process count per bot, index i for bot i+1: 1 at the start, plus
//     spawns, minus deaths, not below 0.
type Frame struct {
	Cycle    int       `json:"cycle"`
	Owners   []uint32  `json:"owners"`
	Executed []Pointer `json:"executed"`
	Live     []int     `json:"live"`
}

// Model is a replay positioned at a cycle. Seeking forward applies the events in
// between; seeking back rebuilds the core from cycle 0 (replay.Player moves forward
// only). It does not assume the invariants the replay writer keeps but the reader
// does not check (docs/spec/replay.md section 8 leaves placement and per-cycle
// completeness open): it never panics on such a replay and shows what is recorded.
type Model struct {
	r *replay.Replay
	p *replay.Player
}

// NewModel returns a model of r at cycle 0. It fails only if the core size is not
// one the VM can build.
func NewModel(r *replay.Replay) (*Model, error) {
	p, err := replay.NewPlayer(r)
	if err != nil {
		return nil, err
	}
	return &Model{r: r, p: p}, nil
}

// Cycles returns the last cycle of the replay (the cycle count of its result).
func (m *Model) Cycles() int { return max(0, m.r.Result.Cycles) }

// Cycle returns the current cycle.
func (m *Model) Cycle() int { return m.p.Cycle() }

// Seek moves the model to a cycle; a cycle before 0 means 0 and one after the last
// means the last.
func (m *Model) Seek(cycle int) error {
	cycle = min(max(cycle, 0), m.Cycles())
	if cycle < m.p.Cycle() {
		p, err := replay.NewPlayer(m.r)
		if err != nil {
			return err
		}
		m.p = p
	}
	return m.p.AdvanceTo(cycle)
}

// Frame returns the frame at the current cycle. It shares nothing with the model.
func (m *Model) Frame() Frame {
	core := m.p.Core()
	f := Frame{
		Cycle:    m.p.Cycle(),
		Owners:   make([]uint32, core.Size()),
		Executed: []Pointer{},
		Live:     make([]int, len(m.r.Bots)),
	}
	for a := range f.Owners {
		f.Owners[a] = core.Read(int64(a)).Writer
	}
	for i := range f.Live {
		f.Live[i] = max(0, m.p.Live(uint32(i+1)))
	}
	// The steps of the current cycle: look the cycle up by its number, because a
	// replay with a gap in its cycles must not show the steps of an earlier one.
	if c := f.Cycle; c > 0 {
		i, ok := slices.BinarySearchFunc(m.r.Cycles, c, func(cy replay.Cycle, n int) int { return cy.N - n })
		if ok {
			for _, st := range m.r.Cycles[i].Steps {
				f.Executed = append(f.Executed, Pointer{Bot: st.Bot, PC: int(st.PC)})
			}
		}
	}
	return f
}
