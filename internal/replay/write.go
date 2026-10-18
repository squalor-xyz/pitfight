// SPDX-License-Identifier: AGPL-3.0-or-later

package replay

import (
	"io"
	"strconv"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/isa"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// Version is the replay format version this package writes and reads.
const Version = 1

// Replay is a replay in memory: what the file of docs/spec/replay.md holds.
type Replay struct {
	Config arena.Config
	Bots   []Bot // bot i+1 is Bots[i]
	Cycles []Cycle
	// Result is the match result. When the replay was read from a file, Cycles,
	// Survivors, Outcome, Winner and Tied are as in the end line, and of Placement
	// only Starts is set, from the header; Order, Rotation and Gaps are not in the file.
	Result arena.Result
}

// Bot is a bot as the match loaded it.
type Bot struct {
	Hash         string     // BotHash of the assembled bot
	Start        int        // the placement start address
	EntryAddress int        // Norm(Start + entry offset), the first process
	Cells        []isa.Cell // normalized, written to Start, Start+1, ... with the bot's id as writer
}

// Cycle holds the steps of one cycle, in execution order (ascending bot id). Every
// cycle of the match has one, and at least one step. In an event of a step, Bot and
// PC are those of the step, Address of a ProcessDeath is PC, and for a CellWrite
// Cell.Writer equals Bot.
type Cycle struct {
	N     int
	Steps []arena.Step
}

// Record runs the match and returns its replay. It returns the error of
// arena.Run unchanged when the match is refused.
func Record(cfg arena.Config, bots []arena.Bot) (*Replay, error) {
	var cycles []Cycle
	res, err := arena.RunObserved(cfg, bots, func(cycle int, _ *vm.Core, steps []arena.Step) {
		if cycle == 0 {
			return
		}
		// Copy the steps and their events: arena reuses its buffers.
		n := 0
		for _, st := range steps {
			n += len(st.Events)
		}
		events := make([]vm.Event, 0, n) // never grows, so the subslices stay valid
		kept := make([]arena.Step, len(steps))
		for i, st := range steps {
			lo := len(events)
			events = append(events, st.Events...)
			kept[i] = arena.Step{Bot: st.Bot, PC: st.PC, Events: events[lo:len(events):len(events)]}
		}
		cycles = append(cycles, Cycle{N: cycle, Steps: kept})
	})
	if err != nil {
		return nil, err
	}
	core, err := vm.NewCore(cfg.CoreSize) // Run accepted cfg, so this cannot fail; it gives Norm
	if err != nil {
		return nil, err
	}
	r := &Replay{Config: cfg, Bots: make([]Bot, len(bots)), Cycles: cycles, Result: res}
	for i, b := range bots {
		id := uint32(i + 1)
		start := res.Placement.Starts[i]
		cells := make([]isa.Cell, len(b.Cells))
		for k, c := range b.Cells {
			c.AValue = core.Norm(int64(c.AValue))
			c.BValue = core.Norm(int64(c.BValue))
			c.Writer = id
			cells[k] = c
		}
		r.Bots[i] = Bot{
			Hash:         BotHash(b),
			Start:        start,
			EntryAddress: int(core.Norm(int64(start) + int64(b.Entry))),
			Cells:        cells,
		}
	}
	return r, nil
}

// Bytes returns the replay file: the one byte string docs/spec/replay.md defines for
// the values in r. Every line ends with a line feed.
func (r *Replay) Bytes() []byte {
	heads := make([]botHeader, len(r.Bots))
	for i, bot := range r.Bots {
		heads[i] = botHeader{bot.Hash, len(bot.Cells), bot.Start, bot.EntryAddress}
	}
	b := appendHeader(nil, r.Config, heads)
	for i, bot := range r.Bots {
		b = appendLoad(b, i+1, bot.Cells)
	}
	for _, c := range r.Cycles {
		b = appendCycle(b, c.N)
		for _, st := range c.Steps {
			b = appendStep(b, st.Bot, st.PC)
			for _, ev := range st.Events {
				b = appendEvent(b, ev)
			}
		}
	}
	return appendEnd(b, r.Result)
}

// Write writes Bytes to w.
func (r *Replay) Write(w io.Writer) error {
	_, err := w.Write(r.Bytes())
	return err
}

func appendInt(b []byte, v int64) []byte { return strconv.AppendInt(b, v, 10) }

func appendCell(b []byte, c isa.Cell) []byte {
	b = append(b, '[')
	b = appendInt(b, int64(c.Opcode))
	b = append(b, ',')
	b = appendInt(b, int64(c.Modifier))
	b = append(b, ',')
	b = appendInt(b, int64(c.AMode))
	b = append(b, ',')
	b = appendInt(b, int64(c.AValue))
	b = append(b, ',')
	b = appendInt(b, int64(c.BMode))
	b = append(b, ',')
	b = appendInt(b, int64(c.BValue))
	return append(b, ']')
}

func appendIDs(b []byte, ids []uint32) []byte {
	b = append(b, '[')
	for i, id := range ids {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendInt(b, int64(id))
	}
	return append(b, ']')
}

// botHeader is the header's entry for a bot.
type botHeader struct {
	hash                        string
	length, start, entryAddress int
}

func appendHeader(b []byte, cfg arena.Config, bots []botHeader) []byte {
	b = append(b, `{"t":"header","version":`...)
	b = appendInt(b, Version)
	b = append(b, `,"config":{"core_size":`...)
	b = appendInt(b, int64(cfg.CoreSize))
	b = append(b, `,"cycle_limit":`...)
	b = appendInt(b, int64(cfg.CycleLimit))
	b = append(b, `,"max_processes":`...)
	b = appendInt(b, int64(cfg.MaxProcesses))
	b = append(b, `,"min_separation":`...)
	b = appendInt(b, int64(cfg.MinSeparation))
	b = append(b, `,"seed":"`...)
	b = strconv.AppendUint(b, cfg.Seed, 10)
	b = append(b, `"},"bots":[`...)
	for i, bot := range bots {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, `{"id":`...)
		b = appendInt(b, int64(i+1))
		b = append(b, `,"hash":"`...)
		b = append(b, bot.hash...)
		b = append(b, `","length":`...)
		b = appendInt(b, int64(bot.length))
		b = append(b, `,"start":`...)
		b = appendInt(b, int64(bot.start))
		b = append(b, `,"entry_address":`...)
		b = appendInt(b, int64(bot.entryAddress))
		b = append(b, '}')
	}
	return append(b, "]}\n"...)
}

func appendLoad(b []byte, id int, cells []isa.Cell) []byte {
	b = append(b, `{"t":"load","bot":`...)
	b = appendInt(b, int64(id))
	b = append(b, `,"cells":[`...)
	for i, c := range cells {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendCell(b, c)
	}
	return append(b, "]}\n"...)
}

func appendCycle(b []byte, n int) []byte {
	b = append(b, `{"t":"c","n":`...)
	b = appendInt(b, int64(n))
	return append(b, "}\n"...)
}

func appendStep(b []byte, bot uint32, pc int32) []byte {
	b = append(b, `{"t":"x","b":`...)
	b = appendInt(b, int64(bot))
	b = append(b, `,"pc":`...)
	b = appendInt(b, int64(pc))
	return append(b, "}\n"...)
}

// appendEvent writes an event of a step. The step's line has the bot and pc, so a
// write has an address and a cell, a spawn an address and a death nothing.
func appendEvent(b []byte, ev vm.Event) []byte {
	switch ev.Kind {
	case vm.CellWrite:
		b = append(b, `{"t":"w","a":`...)
		b = appendInt(b, int64(ev.Address))
		b = append(b, `,"c":`...)
		b = appendCell(b, ev.Cell)
	case vm.ProcessSpawn:
		b = append(b, `{"t":"s","a":`...)
		b = appendInt(b, int64(ev.Address))
	default:
		b = append(b, `{"t":"d"`...)
	}
	return append(b, "}\n"...)
}

func appendEnd(b []byte, res arena.Result) []byte {
	b = append(b, `{"t":"end","cycles":`...)
	b = appendInt(b, int64(res.Cycles))
	b = append(b, `,"survivors":`...)
	b = appendIDs(b, res.Survivors)
	b = append(b, `,"outcome":"`...)
	b = append(b, res.Outcome.String()...)
	b = append(b, `","winner":`...)
	b = appendInt(b, int64(res.Winner))
	b = append(b, `,"tied":`...)
	b = appendIDs(b, res.Tied)
	return append(b, "}\n"...)
}
