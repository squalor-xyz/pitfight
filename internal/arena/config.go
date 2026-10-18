// SPDX-License-Identifier: AGPL-3.0-or-later

package arena

import (
	"fmt"

	"github.com/squalor-xyz/pitfight/internal/isa"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// Limits of match.md section 1. They are exported so that internal/replay holds a
// replay header to the same limits (docs/spec/replay.md section 3) instead of keeping
// copies; MaxCoreSize is the limit of the VM core.
const (
	MaxCoreSize      = vm.MaxCoreSize
	MaxCycleLimit    = 1<<31 - 1
	MaxMaxProcesses  = 1 << 20
	MaxMinSeparation = 1 << 20
	MaxBots          = 1 << 20
	MaxBotLength     = 1 << 20
)

// Config is a match configuration (docs/spec/match.md section 1).
type Config struct {
	CoreSize      int
	CycleLimit    int
	MaxProcesses  int
	MinSeparation int
	Seed          uint64
}

// DefaultConfig returns the spec defaults.
func DefaultConfig() Config {
	return Config{CoreSize: 8000, CycleLimit: 80000, MaxProcesses: 8000, MinSeparation: 100, Seed: 0}
}

// Bot is a bot as the match receives it (match.md section 5). Cell operand values
// may be raw signed values; Run reduces them with Norm when it loads the bot.
type Bot struct {
	Cells []isa.Cell
	Entry int
}

// ErrorKind names the three error kinds of match.md section 1.
type ErrorKind uint8

const (
	InvalidConfig ErrorKind = iota
	InvalidBots
	PlacementImpossible
)

// Error is a refused match. Msg is the exact message of match.md section 1.
type Error struct {
	Kind ErrorKind
	Msg  string
}

// Error returns the message.
func (e *Error) Error() string { return e.Msg }

// Validate runs the checks of match.md section 1 in order and returns the first
// failure as an *Error, or nil.
func Validate(cfg Config, bots []Bot) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}
	if err := validateBots(bots); err != nil {
		return err
	}
	lengths := make([]int, len(bots))
	for i, b := range bots {
		lengths[i] = len(b.Cells)
	}
	return checkPlacement(cfg, lengths)
}

func validateConfig(cfg Config) *Error {
	checks := [...]struct {
		name     string
		v        int
		min, max int
	}{
		{"CORESIZE", cfg.CoreSize, 1, MaxCoreSize},
		{"CYCLELIMIT", cfg.CycleLimit, 1, MaxCycleLimit},
		{"MAXPROCESSES", cfg.MaxProcesses, 1, MaxMaxProcesses},
		{"MINSEPARATION", cfg.MinSeparation, 0, MaxMinSeparation},
	}
	for _, c := range checks {
		if c.v < c.min || c.v > c.max {
			return &Error{InvalidConfig, fmt.Sprintf("invalid config: %s %d out of range %d..%d", c.name, c.v, c.min, c.max)}
		}
	}
	return nil
}

func validateBots(bots []Bot) *Error {
	if len(bots) < 2 || len(bots) > MaxBots {
		return &Error{InvalidBots, fmt.Sprintf("invalid bots: need 2..%d bots, got %d", MaxBots, len(bots))}
	}
	for i, b := range bots {
		l := len(b.Cells)
		if l < 1 || l > MaxBotLength {
			return &Error{InvalidBots, fmt.Sprintf("invalid bots: bot %d length %d out of range 1..%d", i+1, l, MaxBotLength)}
		}
		if b.Entry < 0 || b.Entry >= l {
			return &Error{InvalidBots, fmt.Sprintf("invalid bots: bot %d entry offset %d outside 0..%d", i+1, b.Entry, l-1)}
		}
	}
	return nil
}
