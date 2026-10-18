// SPDX-License-Identifier: AGPL-3.0-or-later

package arena

import (
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// Outcome is the outcome field of a result (match.md section 8).
type Outcome uint8

const (
	Win Outcome = iota
	Tie
)

// String returns "win" or "tie".
func (o Outcome) String() string {
	if o == Win {
		return "win"
	}
	return "tie"
}

// Result is the result of a match (match.md section 8), plus the placement the
// match used. Survivors and Tied are in ascending bot id order; Tied is empty
// for a win and Winner is 0 for a tie.
type Result struct {
	Cycles    int
	Survivors []uint32
	Outcome   Outcome
	Winner    uint32
	Tied      []uint32
	Placement Placement
}

// Run runs one match (match.md sections 5 to 8) and returns its result and the
// VM events of every step, in execution order. The events do not include the
// loading of the bots. Run has no I/O, no clock and no map; the same bots and
// config always give the same result and events. It does not change bots.
func Run(cfg Config, bots []Bot) (Result, []vm.Event, error) {
	if err := Validate(cfg, bots); err != nil {
		return Result{}, nil, err
	}
	lengths := make([]int, len(bots))
	for i, b := range bots {
		lengths[i] = len(b.Cells)
	}
	placement, err := Place(cfg, lengths)
	if err != nil {
		return Result{}, nil, err
	}
	core, queues, err := load(cfg, bots, placement)
	if err != nil {
		return Result{}, nil, err
	}

	var events []vm.Event
	var liveAtStart, live []uint32
	cycle := 0
	for {
		cycle++
		liveAtStart = liveAtStart[:0]
		for i, q := range queues {
			if !q.Dead() {
				liveAtStart = append(liveAtStart, uint32(i+1))
			}
		}
		// A step changes only the core and the stepping bot's own queue, so the
		// bots live at the start of the cycle all step, in ascending id.
		for _, id := range liveAtStart {
			events = core.Step(queues[id-1], id, events)
		}
		live = live[:0]
		for _, id := range liveAtStart {
			if !queues[id-1].Dead() {
				live = append(live, id)
			}
		}
		if len(live) <= 1 || cycle == cfg.CycleLimit {
			break
		}
	}

	res := Result{Cycles: cycle, Survivors: slicesClone(live), Placement: placement}
	switch {
	case len(live) == 1:
		res.Outcome, res.Winner = Win, live[0]
	case len(live) > 1:
		res.Outcome, res.Tied = Tie, slicesClone(live)
	default:
		res.Outcome, res.Tied = Tie, slicesClone(liveAtStart)
	}
	return res, events, nil
}

// slicesClone copies s; an empty s gives nil.
func slicesClone(s []uint32) []uint32 {
	if len(s) == 0 {
		return nil
	}
	return append([]uint32(nil), s...)
}

// load builds the core and one queue per bot for a placement (match.md section 5).
// Each bot's cells go to a_i, a_i+1, ... with operand values reduced by Norm (the
// assembler emits raw signed values, C69) and the writer set to the bot id. The
// bot starts with one process at Norm(a_i + e_i). bots is not changed.
func load(cfg Config, bots []Bot, p Placement) (*vm.Core, []*vm.Queue, error) {
	core, err := vm.NewCore(cfg.CoreSize)
	if err != nil {
		return nil, nil, err
	}
	queues := make([]*vm.Queue, len(bots))
	for i, b := range bots {
		id := uint32(i + 1)
		start := int64(p.Starts[i])
		for k, c := range b.Cells {
			c.AValue = core.Norm(int64(c.AValue))
			c.BValue = core.Norm(int64(c.BValue))
			core.Write(start+int64(k), c, id)
		}
		q, err := core.NewQueue(cfg.MaxProcesses, core.Norm(start+int64(b.Entry)))
		if err != nil {
			return nil, nil, err
		}
		queues[i] = q
	}
	return core, queues, nil
}
