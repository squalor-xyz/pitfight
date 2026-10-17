// SPDX-License-Identifier: AGPL-3.0-or-later

package vm

import "github.com/squalor-xyz/pitfight/internal/isa"

// EventKind is an integer tag, suitable for deterministic replay serialization.
type EventKind uint8

const (
	CellWrite EventKind = iota
	ProcessSpawn
	ProcessDeath
)

// Event reports one effect, in execution order. Bot is the executing bot and PC
// is the executed instruction address. Address names the written cell, spawned
// process, or dying process. Cell is the post-write contents (including writer)
// for CellWrite and zero for process events. Process events follow all writes;
// normal process continuations produce no event.
type Event struct {
	Kind    EventKind
	Bot     uint32
	PC      int32
	Address int32
	Cell    isa.Cell
}
