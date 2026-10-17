// SPDX-License-Identifier: AGPL-3.0-or-later

package vm

import "github.com/squalor-xyz/pitfight/internal/isa"

// Step executes the front process and appends its effects to events. A dead
// queue is a no-op. q must belong to this core, bot must be nonzero, and cells
// must contain valid ISA codes and normalized values (isa.md sections 1-3).
// Events are plain values; callers own the returned buffer.
func (c *Core) Step(q *Queue, bot uint32, events []Event) []Event {
	if q.Dead() {
		return events
	}
	pc := q.pop()
	ir := c.Read(int64(pc))
	emitWrite := func(addr int32) {
		events = append(events, Event{Kind: CellWrite, Bot: bot, PC: pc, Address: addr, Cell: c.Read(int64(addr))})
	}
	resolve := func(mode isa.Mode, value int32) Operand {
		op, _ := c.Resolve(pc, mode, value, bot)
		if mode == isa.ModeAPreDec || mode == isa.ModeBPreDec {
			emitWrite(c.Norm(int64(pc) + int64(value)))
		}
		return op
	}
	a := resolve(ir.AMode, ir.AValue)
	b := resolve(ir.BMode, ir.BValue)
	// Each modifier supplies source and destination field indices, A=0 and B=1.
	pairs := [2][2]int{{0, 0}, {1, 1}}
	count := 1
	switch ir.Modifier {
	case isa.ModB:
		pairs[0] = [2]int{1, 1}
	case isa.ModAB:
		pairs[0] = [2]int{0, 1}
	case isa.ModBA:
		pairs[0] = [2]int{1, 0}
	case isa.ModF, isa.ModI:
		count = 2
	case isa.ModX:
		pairs = [2][2]int{{0, 1}, {1, 0}}
		count = 2
	}
	av := [2]int32{a.Snapshot.AValue, a.Snapshot.BValue}
	bv := [2]int32{b.Snapshot.AValue, b.Snapshot.BValue}
	out := c.Read(int64(b.Target))
	values := [2]int32{out.AValue, out.BValue}
	next := c.Norm(int64(pc) + 1)
	dead, write, spawn := false, false, false
	equal, less, zero, divZero := true, true, true, false
	for _, p := range pairs[:count] {
		s, d := p[0], p[1]
		equal = equal && av[s] == bv[d]
		less = less && av[s] < bv[d]
		zero = zero && bv[d] == 0
		divZero = divZero || av[s] == 0
	}
	if ir.Modifier == isa.ModI && (ir.Opcode == isa.OpSEQ || ir.Opcode == isa.OpSNE) {
		aa, bb := a.Snapshot, b.Snapshot
		aa.Writer = 0
		bb.Writer = 0
		equal = aa == bb
	}
	switch ir.Opcode {
	case isa.OpDAT:
		dead = true
	case isa.OpMOV:
		write = true
		if ir.Modifier == isa.ModI {
			out = a.Snapshot
		} else {
			for _, p := range pairs[:count] {
				values[p[1]] = av[p[0]]
			}
		}
	case isa.OpADD, isa.OpSUB, isa.OpMUL, isa.OpDIV:
		if ir.Opcode == isa.OpDIV && divZero {
			dead = true
			break
		}
		write = true
		for _, p := range pairs[:count] {
			s, d := int64(av[p[0]]), int64(bv[p[1]])
			var v int64
			switch ir.Opcode {
			case isa.OpADD:
				v = d + s
			case isa.OpSUB:
				v = d - s
			case isa.OpMUL:
				v = d * s
			case isa.OpDIV:
				v = d / s
			}
			values[p[1]] = c.Norm(v)
		}
	case isa.OpJMP:
		next = a.Target
	case isa.OpJMZ:
		if zero {
			next = a.Target
		}
	case isa.OpJMN:
		if !zero {
			next = a.Target
		}
	case isa.OpDJN:
		write = true
		nonzero := false
		for _, p := range pairs[:count] {
			d := p[1]
			values[d] = c.Norm(int64(bv[d]) - 1)
			nonzero = nonzero || values[d] != 0
		}
		if nonzero {
			next = a.Target
		}
	case isa.OpSPL:
		spawn = q.count+1 < len(q.pcs)
	case isa.OpSEQ:
		if equal {
			next = c.Norm(int64(pc) + 2)
		}
	case isa.OpSNE:
		if !equal {
			next = c.Norm(int64(pc) + 2)
		}
	case isa.OpSLT:
		if less {
			next = c.Norm(int64(pc) + 2)
		}
	case isa.OpNOP:
	}
	if write {
		if !(ir.Opcode == isa.OpMOV && ir.Modifier == isa.ModI) {
			out.AValue, out.BValue = values[0], values[1]
		}
		c.Write(int64(b.Target), out, bot)
		emitWrite(b.Target)
	}
	for _, post := range []PostInc{a.Post, b.Post} {
		if post.Field != PostNone {
			c.ApplyPostInc(post, bot)
			emitWrite(post.Pointer)
		}
	}
	if dead {
		return append(events, Event{Kind: ProcessDeath, Bot: bot, PC: pc, Address: pc})
	}
	q.push(next)
	if spawn {
		q.push(a.Target)
		events = append(events, Event{Kind: ProcessSpawn, Bot: bot, PC: pc, Address: a.Target})
	}
	return events
}
