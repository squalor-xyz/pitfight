// SPDX-License-Identifier: AGPL-3.0-or-later

package replay

import (
	"fmt"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// Player rebuilds the core of a replay by applying its events, without running the
// match (docs/spec/replay.md section 5). It moves forward only.
type Player struct {
	r     *Replay
	core  *vm.Core
	cycle int
	next  int   // index in r.Cycles of the first cycle not applied yet
	live  []int // live process count per bot, from the spawn and death events
}

// NewPlayer returns a player at cycle 0: the empty core with the bots loaded.
func NewPlayer(r *Replay) (*Player, error) {
	core, err := vm.NewCore(r.Config.CoreSize)
	if err != nil {
		return nil, err
	}
	p := &Player{r: r, core: core, live: make([]int, len(r.Bots))}
	for i, b := range r.Bots {
		for k, c := range b.Cells {
			core.Write(int64(b.Start)+int64(k), c, uint32(i+1))
		}
		p.live[i] = 1
	}
	return p, nil
}

// Core returns the player's core. The caller must not write to it.
func (p *Player) Core() *vm.Core { return p.core }

// Steps returns the steps of the cycle the player is at, in execution order: for
// each, the bot, the address it executed and the events (none for a silent step).
// It is nil at cycle 0. The caller must not modify it.
func (p *Player) Steps() []arena.Step {
	if p.next > 0 {
		return p.r.Cycles[p.next-1].Steps
	}
	return nil
}

// Cycle returns the cycle the core is at.
func (p *Player) Cycle() int { return p.cycle }

// AdvanceTo applies the events of every cycle up to and including cycle. A cycle
// after the last one of the match means the last one. It is an error to go back.
func (p *Player) AdvanceTo(cycle int) error {
	cycle = min(cycle, p.r.Result.Cycles)
	if cycle < p.cycle {
		return fmt.Errorf("replay: cannot go back from cycle %d to cycle %d", p.cycle, cycle)
	}
	for ; p.next < len(p.r.Cycles) && p.r.Cycles[p.next].N <= cycle; p.next++ {
		for _, st := range p.r.Cycles[p.next].Steps {
			for _, ev := range st.Events {
				p.apply(ev)
			}
		}
	}
	p.cycle = cycle
	return nil
}

func (p *Player) apply(ev vm.Event) {
	switch ev.Kind {
	case vm.CellWrite:
		p.core.Write(int64(ev.Address), ev.Cell, ev.Bot)
	case vm.ProcessSpawn:
		p.add(ev.Bot, 1)
	case vm.ProcessDeath:
		p.add(ev.Bot, -1)
	}
}

func (p *Player) add(bot uint32, d int) {
	if bot >= 1 && int(bot) <= len(p.live) {
		p.live[bot-1] += d
	}
}

// Live returns the number of live processes of a bot: 1 at the start, plus its
// spawns, minus its deaths. It is 0 for an id that is not a bot.
func (p *Player) Live(bot uint32) int {
	if bot < 1 || int(bot) > len(p.live) {
		return 0
	}
	return p.live[bot-1]
}
