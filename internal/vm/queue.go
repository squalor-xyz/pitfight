// SPDX-License-Identifier: AGPL-3.0-or-later

package vm

import "errors"

// ErrQueue is returned for an invalid process cap or too many initial processes.
var ErrQueue = errors.New("vm: invalid process queue")

// Queue is one bot's slice-backed FIFO. Its zero value is a dead queue.
// Use Core.NewQueue and pass it only to that same core. Do not copy a live Queue.
// A circular slice avoids shifting the remaining processes on every step.
type Queue struct {
	pcs         []int32
	head, count int
}

// NewQueue constructs a FIFO with MAXPROCESSES in [1, 2^20] (match.md
// section 1). Initial addresses are normalized; the caller retains its inputs.
func (c *Core) NewQueue(max int, pcs ...int32) (*Queue, error) {
	if max < 1 || max > MaxCoreSize || len(pcs) > max {
		return nil, ErrQueue
	}
	q := &Queue{pcs: make([]int32, max), count: len(pcs)}
	for i, pc := range pcs {
		q.pcs[i] = c.Norm(int64(pc))
	}
	return q, nil
}

// Len returns the number of live processes.
func (q *Queue) Len() int { return q.count }

// Dead reports whether the bot has no live processes.
func (q *Queue) Dead() bool { return q.count == 0 }

// Addresses returns a caller-owned snapshot in execution order.
func (q *Queue) Addresses() []int32 {
	out := make([]int32, q.count)
	for i := range out {
		out[i] = q.pcs[(q.head+i)%len(q.pcs)]
	}
	return out
}

// Front returns the address of the process that steps next. The queue must not be
// dead.
func (q *Queue) Front() int32 { return q.pcs[q.head] }

func (q *Queue) pop() int32 {
	pc := q.pcs[q.head]
	q.head = (q.head + 1) % len(q.pcs)
	q.count--
	return pc
}
func (q *Queue) push(pc int32) { q.pcs[(q.head+q.count)%len(q.pcs)] = pc; q.count++ }
