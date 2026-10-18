// SPDX-License-Identifier: AGPL-3.0-or-later

package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/isa"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// VersionError is the error for a replay whose version this package does not read.
type VersionError struct {
	Version int64
}

func (e *VersionError) Error() string {
	return fmt.Sprintf("replay: unsupported version %d (this reader reads version %d)", e.Version, Version)
}

// The decoded forms of the lines. The field names are those of the spec; the
// order here is not significant, because Read compares every line with what the
// writer would produce (canonical).
type headerLine struct {
	T       string `json:"t"`
	Version int64  `json:"version"`
	Config  struct {
		CoreSize      int64  `json:"core_size"`
		CycleLimit    int64  `json:"cycle_limit"`
		MaxProcesses  int64  `json:"max_processes"`
		MinSeparation int64  `json:"min_separation"`
		Seed          string `json:"seed"`
	} `json:"config"`
	Bots []struct {
		ID           int64  `json:"id"`
		Hash         string `json:"hash"`
		Length       int64  `json:"length"`
		Start        int64  `json:"start"`
		EntryAddress int64  `json:"entry_address"`
	} `json:"bots"`
}

type loadLine struct {
	T     string     `json:"t"`
	Bot   int64      `json:"bot"`
	Cells [][6]int64 `json:"cells"`
}

type cycleLine struct {
	T string `json:"t"`
	N int64  `json:"n"`
}

type stepLine struct {
	T  string `json:"t"`
	B  int64  `json:"b"`
	PC int64  `json:"pc"`
}

type eventLine struct {
	T string    `json:"t"`
	A int64     `json:"a"`
	C *[6]int64 `json:"c"`
}

type endLine struct {
	T         string  `json:"t"`
	Cycles    int64   `json:"cycles"`
	Survivors []int64 `json:"survivors"`
	Outcome   string  `json:"outcome"`
	Winner    int64   `json:"winner"`
	Tied      []int64 `json:"tied"`
}

// Read reads and validates a whole replay (docs/spec/replay.md section 8). On any
// error it returns a nil replay. A version other than Version gives a
// *VersionError.
func Read(rd io.Reader) (*Replay, error) {
	data, err := io.ReadAll(rd)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("replay: empty file")
	}
	if data[len(data)-1] != '\n' {
		return nil, errors.New("replay: the last line does not end with a newline (line feed)")
	}
	p := &parser{r: &Replay{}}
	for n := 1; len(data) > 0; n++ {
		i := bytes.IndexByte(data, '\n')
		line := data[:i+1]
		data = data[i+1:]
		if err := p.line(n, line); err != nil {
			return nil, err
		}
	}
	if p.stage != stageDone {
		return nil, errors.New("replay: no end line")
	}
	return p.r, nil
}

const (
	stageHeader = iota
	stageLoad
	stageBody
	stageDone
)

// parser is the state of Read.
type parser struct {
	r       *Replay
	stage   int
	lengths []int // cells per bot, from the header
	loaded  int   // bots whose load line was read
	open    bool  // the last cycle line is open: r.Cycles[len-1]
	lastN   int   // n of the last cycle line, 0 before the first
	lastBot int64 // bot of the last step of the open cycle, 0 before the first
	inStep  bool  // the open cycle has a step
	ended   bool  // the last step had its spawn or death
	live    []int // live processes per bot, from the header and the s and d lines
}

func (p *parser) line(n int, line []byte) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("replay: line %d: %s", n, fmt.Sprintf(format, args...))
	}
	var probe struct {
		T       string `json:"t"`
		Version *int64 `json:"version"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return fail("%v", err)
	}
	switch p.stage {
	case stageHeader:
		if probe.T != "header" {
			return fail("expected the header line")
		}
		if probe.Version == nil {
			return fail("the header has no version")
		}
		if *probe.Version != Version {
			return &VersionError{*probe.Version}
		}
		return p.header(n, line)
	case stageLoad:
		if probe.T != "load" {
			return fail("expected the load line of bot %d", p.loaded+1)
		}
		return p.load(n, line)
	case stageDone:
		return fail("content after the end line")
	}
	switch probe.T {
	case "c":
		return p.cycle(n, line)
	case "x":
		return p.execute(n, line)
	case "w", "s", "d":
		return p.event(n, line)
	case "end":
		return p.end(n, line)
	}
	return fail("unknown line type %q", probe.T)
}

// decode parses one line. It takes any field order, spelling of a number or
// unknown field; canonical then rejects everything but the writer's own bytes.
func decode(line []byte, v any) error { return json.Unmarshal(line, v) }

// canonical checks that line is exactly want, the line the writer produces.
func canonical(n int, line, want []byte) error {
	if !bytes.Equal(line, want) {
		return fmt.Errorf("replay: line %d: not in the canonical form of docs/spec/replay.md", n)
	}
	return nil
}

func rangeErr(n int, name string, v, lo, hi int64) error {
	return fmt.Errorf("replay: line %d: %s %d out of range %d..%d", n, name, v, lo, hi)
}

func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (p *parser) header(n int, line []byte) error {
	var h headerLine
	if err := decode(line, &h); err != nil {
		return fmt.Errorf("replay: line %d: %v", n, err)
	}
	c := h.Config
	for _, f := range []struct {
		name   string
		v      int64
		lo, hi int64
	}{
		{"core_size", c.CoreSize, vm.MinCoreSize, arena.MaxCoreSize},
		{"cycle_limit", c.CycleLimit, 1, arena.MaxCycleLimit},
		{"max_processes", c.MaxProcesses, 1, arena.MaxMaxProcesses},
		{"min_separation", c.MinSeparation, 0, arena.MaxMinSeparation},
	} {
		if f.v < f.lo || f.v > f.hi {
			return rangeErr(n, f.name, f.v, f.lo, f.hi)
		}
	}
	seed, err := strconv.ParseUint(c.Seed, 10, 64)
	if err != nil {
		return fmt.Errorf("replay: line %d: seed %q is not an unsigned 64-bit decimal", n, c.Seed)
	}
	if len(h.Bots) < 2 || len(h.Bots) > arena.MaxBots {
		return fmt.Errorf("replay: line %d: need 2..%d bots, got %d", n, arena.MaxBots, len(h.Bots))
	}
	cfg := arena.Config{
		CoreSize: int(c.CoreSize), CycleLimit: int(c.CycleLimit), MaxProcesses: int(c.MaxProcesses),
		MinSeparation: int(c.MinSeparation), Seed: seed,
	}
	size := c.CoreSize
	bots := make([]Bot, len(h.Bots))
	heads := make([]botHeader, len(h.Bots))
	starts := make([]int, len(h.Bots))
	p.lengths = make([]int, len(h.Bots))
	for i, b := range h.Bots {
		switch {
		case !isHash(b.Hash):
			return fmt.Errorf("replay: line %d: bot %d hash %q is not 64 lowercase hexadecimal characters", n, i+1, b.Hash)
		case b.Length < 1 || b.Length > size:
			return rangeErr(n, "length", b.Length, 1, size)
		case b.Start < 0 || b.Start >= size:
			return rangeErr(n, "start", b.Start, 0, size-1)
		case b.EntryAddress < 0 || b.EntryAddress >= size:
			return rangeErr(n, "entry_address", b.EntryAddress, 0, size-1)
		}
		bots[i] = Bot{Hash: b.Hash, Start: int(b.Start), EntryAddress: int(b.EntryAddress)}
		heads[i] = botHeader{b.Hash, int(b.Length), int(b.Start), int(b.EntryAddress)}
		starts[i] = int(b.Start)
		p.lengths[i] = int(b.Length)
	}
	if err := canonical(n, line, appendHeader(nil, cfg, heads)); err != nil {
		return err
	}
	p.r.Config, p.r.Bots = cfg, bots
	p.r.Result.Placement.Starts = starts
	p.live = make([]int, len(bots))
	for i := range p.live {
		p.live[i] = 1
	}
	p.stage = stageLoad
	return nil
}

// cell validates the six integers of a cell and returns it with the given writer.
func (p *parser) cell(n int, a [6]int64, writer uint32) (isa.Cell, error) {
	size := int64(p.r.Config.CoreSize)
	for _, f := range []struct {
		name   string
		v      int64
		lo, hi int64
	}{
		{"opcode", a[0], 0, int64(len(isa.Opcodes)) - 1},
		{"modifier", a[1], 0, int64(len(isa.Modifiers)) - 1},
		{"a_mode", a[2], 0, int64(len(isa.Modes)) - 1},
		{"a_value", a[3], 0, size - 1},
		{"b_mode", a[4], 0, int64(len(isa.Modes)) - 1},
		{"b_value", a[5], 0, size - 1},
	} {
		if f.v < f.lo || f.v > f.hi {
			return isa.Cell{}, rangeErr(n, f.name, f.v, f.lo, f.hi)
		}
	}
	return isa.Cell{
		Opcode: isa.Opcode(a[0]), Modifier: isa.Modifier(a[1]),
		AMode: isa.Mode(a[2]), AValue: int32(a[3]),
		BMode: isa.Mode(a[4]), BValue: int32(a[5]),
		Writer: writer,
	}, nil
}

func (p *parser) load(n int, line []byte) error {
	var l loadLine
	if err := decode(line, &l); err != nil {
		return fmt.Errorf("replay: line %d: %v", n, err)
	}
	id := p.loaded + 1
	if l.Bot != int64(id) {
		return fmt.Errorf("replay: line %d: load line for bot %d, want bot %d", n, l.Bot, id)
	}
	if len(l.Cells) != p.lengths[p.loaded] {
		return fmt.Errorf("replay: line %d: bot %d has %d cells, the header says %d", n, id, len(l.Cells), p.lengths[p.loaded])
	}
	cells := p.r.Bots[p.loaded].Cells
	for _, a := range l.Cells {
		c, err := p.cell(n, a, uint32(id))
		if err != nil {
			return err
		}
		cells = append(cells, c)
	}
	if err := canonical(n, line, appendLoad(nil, id, cells)); err != nil {
		return err
	}
	p.r.Bots[p.loaded].Cells = cells
	p.loaded++
	if p.loaded == len(p.r.Bots) {
		p.stage = stageBody
	}
	return nil
}

// closeCycle checks that the open cycle, if any, has steps.
func (p *parser) closeCycle(n int) error {
	if p.open && len(p.r.Cycles[len(p.r.Cycles)-1].Steps) == 0 {
		return fmt.Errorf("replay: line %d: cycle %d has no steps", n, p.lastN)
	}
	return nil
}

func (p *parser) cycle(n int, line []byte) error {
	var l cycleLine
	if err := decode(line, &l); err != nil {
		return fmt.Errorf("replay: line %d: %v", n, err)
	}
	if err := p.closeCycle(n); err != nil {
		return err
	}
	if l.N != int64(p.lastN)+1 {
		return fmt.Errorf("replay: line %d: cycle %d, want %d", n, l.N, p.lastN+1)
	}
	if l.N > int64(p.r.Config.CycleLimit) {
		return fmt.Errorf("replay: line %d: cycle %d is past cycle_limit %d", n, l.N, p.r.Config.CycleLimit)
	}
	if err := canonical(n, line, appendCycle(nil, int(l.N))); err != nil {
		return err
	}
	p.lastN = int(l.N)
	p.r.Cycles = append(p.r.Cycles, Cycle{N: p.lastN})
	p.open, p.lastBot, p.inStep = true, 0, false
	return nil
}

// execute reads an x line: a step of the open cycle.
func (p *parser) execute(n int, line []byte) error {
	var l stepLine
	if err := decode(line, &l); err != nil {
		return fmt.Errorf("replay: line %d: %v", n, err)
	}
	if !p.open {
		return fmt.Errorf("replay: line %d: step before the first cycle line", n)
	}
	size := int64(p.r.Config.CoreSize)
	switch {
	case l.B < 1 || l.B > int64(len(p.r.Bots)):
		return rangeErr(n, "bot", l.B, 1, int64(len(p.r.Bots)))
	case l.PC < 0 || l.PC >= size:
		return rangeErr(n, "pc", l.PC, 0, size-1)
	case l.B <= p.lastBot:
		return fmt.Errorf("replay: line %d: bot %d steps after bot %d in cycle %d", n, l.B, p.lastBot, p.lastN)
	case p.live[l.B-1] <= 0:
		return fmt.Errorf("replay: line %d: bot %d steps with no live process", n, l.B)
	}
	if err := canonical(n, line, appendStep(nil, uint32(l.B), int32(l.PC))); err != nil {
		return err
	}
	cur := &p.r.Cycles[len(p.r.Cycles)-1]
	cur.Steps = append(cur.Steps, arena.Step{Bot: uint32(l.B), PC: int32(l.PC)})
	p.inStep, p.lastBot, p.ended = true, l.B, false
	return nil
}

func (p *parser) event(n int, line []byte) error {
	var l eventLine
	if err := decode(line, &l); err != nil {
		return fmt.Errorf("replay: line %d: %v", n, err)
	}
	if !p.inStep {
		return fmt.Errorf("replay: line %d: event before a step", n)
	}
	if p.ended {
		return fmt.Errorf("replay: line %d: event after the spawn or death of its step", n)
	}
	size := int64(p.r.Config.CoreSize)
	last := &p.r.Cycles[len(p.r.Cycles)-1].Steps[len(p.r.Cycles[len(p.r.Cycles)-1].Steps)-1]
	ev := vm.Event{Bot: last.Bot, PC: last.PC}
	switch l.T {
	case "w", "s":
		if l.A < 0 || l.A >= size {
			return rangeErr(n, "address", l.A, 0, size-1)
		}
		ev.Address = int32(l.A)
	}
	switch l.T {
	case "w":
		ev.Kind = vm.CellWrite
		if l.C == nil {
			return fmt.Errorf("replay: line %d: write without a cell", n)
		}
		c, err := p.cell(n, *l.C, ev.Bot)
		if err != nil {
			return err
		}
		ev.Cell = c
	case "s":
		ev.Kind = vm.ProcessSpawn
	default:
		ev.Kind = vm.ProcessDeath
		ev.Address = ev.PC
	}
	if err := canonical(n, line, appendEvent(nil, ev)); err != nil {
		return err
	}
	last.Events = append(last.Events, ev)
	switch ev.Kind {
	case vm.ProcessSpawn:
		p.live[ev.Bot-1]++
		p.ended = true
	case vm.ProcessDeath:
		p.live[ev.Bot-1]--
		p.ended = true
	}
	return nil
}

// ids validates an ascending list of bot ids.
func (p *parser) ids(n int, name string, v []int64) ([]uint32, error) {
	var out []uint32
	prev := int64(0)
	for _, id := range v {
		if id <= prev || id > int64(len(p.r.Bots)) {
			return nil, fmt.Errorf("replay: line %d: %s %v are not ascending bot ids 1..%d", n, name, v, len(p.r.Bots))
		}
		out = append(out, uint32(id))
		prev = id
	}
	return out, nil
}

func (p *parser) end(n int, line []byte) error {
	var l endLine
	if err := decode(line, &l); err != nil {
		return fmt.Errorf("replay: line %d: %v", n, err)
	}
	if err := p.closeCycle(n); err != nil {
		return err
	}
	limit := int64(p.r.Config.CycleLimit)
	if l.Cycles < 1 || l.Cycles > limit {
		return rangeErr(n, "cycles", l.Cycles, 1, limit)
	}
	if l.Cycles != int64(p.lastN) {
		return fmt.Errorf("replay: line %d: cycles %d, but the last cycle is %d", n, l.Cycles, p.lastN)
	}
	survivors, err := p.ids(n, "survivors", l.Survivors)
	if err != nil {
		return err
	}
	tied, err := p.ids(n, "tied", l.Tied)
	if err != nil {
		return err
	}
	res := arena.Result{Cycles: int(l.Cycles), Survivors: survivors, Winner: uint32(l.Winner), Tied: tied, Placement: p.r.Result.Placement}
	switch l.Outcome {
	case "win":
		res.Outcome = arena.Win
		if len(survivors) != 1 || l.Winner != int64(survivors[0]) || len(tied) != 0 {
			return fmt.Errorf("replay: line %d: outcome win needs one survivor who is the winner and no tied bots", n)
		}
	case "tie":
		res.Outcome = arena.Tie
		if l.Winner != 0 || len(tied) == 0 || len(survivors) == 1 || (len(survivors) > 0 && !slices.Equal(survivors, tied)) {
			return fmt.Errorf("replay: line %d: outcome tie needs winner 0, tied bots, and no survivors or several, which are the tied bots", n)
		}
	default:
		return fmt.Errorf("replay: line %d: outcome %q is not win or tie", n, l.Outcome)
	}
	var alive []uint32
	for i, c := range p.live {
		if c > 0 {
			alive = append(alive, uint32(i+1))
		}
	}
	if !slices.Equal(alive, survivors) {
		return fmt.Errorf("replay: line %d: survivors %v do not match the live processes %v", n, survivors, alive)
	}
	if err := canonical(n, line, appendEnd(nil, res)); err != nil {
		return err
	}
	p.r.Result = res
	p.stage = stageDone
	return nil
}
