// SPDX-License-Identifier: AGPL-3.0-or-later

package arena

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/isa"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// botFrom assembles src into a bot.
func botFrom(t *testing.T, src string) Bot {
	t.Helper()
	p, err := asm.Assemble("test.s", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return Bot{Cells: p.Cells, Entry: p.Entry}
}

// botFile assembles a file of testdata/.
func botFile(t *testing.T, name string) Bot {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	p, err := asm.Assemble(name, src)
	if err != nil {
		t.Fatal(err)
	}
	return Bot{Cells: p.Cells, Entry: p.Entry}
}

// workedConfig is the configuration of the worked matches of match.md section 8.
func workedConfig(cycleLimit int) Config {
	return Config{CoreSize: 16, CycleLimit: cycleLimit, MaxProcesses: 8, MinSeparation: 0, Seed: 0}
}

type wantResult struct {
	cycles    int
	survivors []uint32
	outcome   Outcome
	winner    uint32
	tied      []uint32
}

func checkResult(t *testing.T, got Result, want wantResult) {
	t.Helper()
	if got.Cycles != want.cycles {
		t.Errorf("cycles = %d, want %d", got.Cycles, want.cycles)
	}
	if !slices.Equal(got.Survivors, want.survivors) {
		t.Errorf("survivors = %v, want %v", got.Survivors, want.survivors)
	}
	if got.Outcome != want.outcome {
		t.Errorf("outcome = %v, want %v", got.Outcome, want.outcome)
	}
	if got.Winner != want.winner {
		t.Errorf("winner = %d, want %d", got.Winner, want.winner)
	}
	if !slices.Equal(got.Tied, want.tied) {
		t.Errorf("tied = %v, want %v", got.Tied, want.tied)
	}
}

const (
	srcDat    = "DAT.F $0, $0"
	srcJmp    = "JMP.B $0, $0"
	srcNopDat = "NOP.F $0, $0\nDAT.F $0, $0"
)

func TestResults(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		bots  []string
		want  wantResult
	}{
		{"M1", 10, []string{srcDat, srcJmp}, wantResult{1, []uint32{2}, Win, 2, nil}},
		{"M2", 10, []string{srcDat, srcDat}, wantResult{1, nil, Tie, 0, []uint32{1, 2}}},
		{"M3", 5, []string{srcJmp, srcJmp}, wantResult{5, []uint32{1, 2}, Tie, 0, []uint32{1, 2}}},
		{"M4", 10, []string{srcJmp, srcNopDat}, wantResult{2, []uint32{1}, Win, 1, nil}},
		{"M5", 3, []string{srcJmp, "NOP.F $0, $0\nNOP.F $0, $0\nDAT.F $0, $0"}, wantResult{3, []uint32{1}, Win, 1, nil}},
		{"M6", 10, []string{srcDat, srcNopDat, srcNopDat}, wantResult{2, nil, Tie, 0, []uint32{2, 3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bots []Bot
			for _, s := range tt.bots {
				bots = append(bots, botFrom(t, s))
			}
			res, _, err := Run(workedConfig(tt.limit), bots)
			if err != nil {
				t.Fatal(err)
			}
			checkResult(t, res, tt.want)
		})
	}
}

// TestResultsTestdata runs the testdata bots under the default configuration.
func TestResultsTestdata(t *testing.T) {
	sitter, suicide := botFile(t, "sitter.s"), botFile(t, "suicide.s")
	res, _, err := Run(DefaultConfig(), []Bot{sitter, suicide})
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, wantResult{1, []uint32{1}, Win, 1, nil})
	res, _, err = Run(DefaultConfig(), []Bot{suicide, sitter})
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, wantResult{1, []uint32{2}, Win, 2, nil})
}

func TestCycleLimit(t *testing.T) {
	sitter := botFile(t, "sitter.s")
	for _, limit := range []int{1, 7, 80000} {
		cfg := DefaultConfig()
		cfg.CycleLimit = limit
		res, events, err := Run(cfg, []Bot{sitter, sitter})
		if err != nil {
			t.Fatal(err)
		}
		checkResult(t, res, wantResult{limit, []uint32{1, 2}, Tie, 0, []uint32{1, 2}})
		if len(events) != 0 {
			t.Errorf("limit %d: %d events, want none (a jump to self writes nothing)", limit, len(events))
		}
	}
}

type ev struct {
	kind    vm.EventKind
	bot     uint32
	pc, adr int32
}

func TestTurnOrder(t *testing.T) {
	const imp = "MOV.I $0, $1"
	tests := []struct {
		name  string
		limit int
		bots  []string
		// want builds the expected events from the start addresses of the bots.
		want func(a []int32) []ev
	}{
		{"alternate by id", 3, []string{imp, imp}, func(a []int32) []ev {
			var out []ev
			for c := int32(0); c < 3; c++ {
				out = append(out,
					ev{vm.CellWrite, 1, a[0] + c, a[0] + c + 1},
					ev{vm.CellWrite, 2, a[1] + c, a[1] + c + 1})
			}
			return out
		}},
		{"bot that died earlier in the cycle does not skip later bots", 10, []string{srcDat, imp}, func(a []int32) []ev {
			return []ev{
				{vm.ProcessDeath, 1, a[0], a[0]},
				{vm.CellWrite, 2, a[1], a[1] + 1},
			}
		}},
		{"one step per bot per cycle whatever the process count", 3, []string{"SPL.B $1, $0\n" + imp, imp}, func(a []int32) []ev {
			return []ev{
				{vm.ProcessSpawn, 1, a[0], a[0] + 1},
				{vm.CellWrite, 2, a[1], a[1] + 1},
				{vm.CellWrite, 1, a[0] + 1, a[0] + 2},
				{vm.CellWrite, 2, a[1] + 1, a[1] + 2},
				{vm.CellWrite, 1, a[0] + 1, a[0] + 2},
				{vm.CellWrite, 2, a[1] + 2, a[1] + 3},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bots []Bot
			for _, s := range tt.bots {
				bots = append(bots, botFrom(t, s))
			}
			cfg := DefaultConfig()
			cfg.CycleLimit = tt.limit
			res, events, err := Run(cfg, bots)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Placement.Starts) != len(bots) {
				t.Fatalf("placement has %d starts, want %d", len(res.Placement.Starts), len(bots))
			}
			var starts []int32
			for i, s := range res.Placement.Starts {
				starts = append(starts, int32(s)+int32(bots[i].Entry))
			}
			var got []ev
			for _, e := range events {
				got = append(got, ev{e.Kind, e.Bot, e.PC, e.Address})
			}
			want := tt.want(starts)
			if !slices.Equal(got, want) {
				t.Errorf("events = %v\nwant     %v", got, want)
			}
		})
	}
}

func TestDeterministic(t *testing.T) {
	bots := []Bot{
		botFrom(t, "start: ADD.AB #4, ptr\nMOV.I ptr, @ptr\nJMP start\nptr: DAT #0, #0"),
		botFrom(t, "MOV.I $0, $1"),
		botFrom(t, "SPL.B $0, $0"),
	}
	cfg := DefaultConfig()
	cfg.CycleLimit = 2000
	cfg.Seed = 5
	r1, e1, err1 := Run(cfg, bots)
	r2, e2, err2 := Run(cfg, bots)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if len(e1) == 0 {
		t.Fatal("no events; the test would prove nothing")
	}
	if !reflect.DeepEqual(r1, r2) || !reflect.DeepEqual(e1, e2) {
		t.Error("two runs of the same bots, seed and config differ")
	}
	cfg.Seed = 6
	r3, _, err := Run(cfg, bots)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(r1.Placement.Starts, r3.Placement.Starts) {
		t.Errorf("seeds 5 and 6 placed the bots identically: %v", r1.Placement.Starts)
	}
}

func TestValidate(t *testing.T) {
	ok := Bot{Cells: []isa.Cell{isa.EmptyCell, isa.EmptyCell}, Entry: 1}
	two := []Bot{ok, ok}
	with := func(f func(*Config)) Config {
		c := DefaultConfig()
		f(&c)
		return c
	}
	tests := []struct {
		name string
		cfg  Config
		bots []Bot
		kind ErrorKind
		want string
	}{
		{"core size low", with(func(c *Config) { c.CoreSize = 0 }), two, InvalidConfig, "invalid config: CORESIZE 0 out of range 1..1048576"},
		{"core size high", with(func(c *Config) { c.CoreSize = 1<<20 + 1 }), two, InvalidConfig, "invalid config: CORESIZE 1048577 out of range 1..1048576"},
		{"cycle limit", with(func(c *Config) { c.CycleLimit = 0 }), two, InvalidConfig, "invalid config: CYCLELIMIT 0 out of range 1..2147483647"},
		{"max processes", with(func(c *Config) { c.MaxProcesses = 1<<20 + 1 }), two, InvalidConfig, "invalid config: MAXPROCESSES 1048577 out of range 1..1048576"},
		{"min separation", with(func(c *Config) { c.MinSeparation = -1 }), two, InvalidConfig, "invalid config: MINSEPARATION -1 out of range 0..1048576"},
		{"config order", with(func(c *Config) { c.MinSeparation, c.MaxProcesses, c.CycleLimit = -1, 0, 0 }), two, InvalidConfig, "invalid config: CYCLELIMIT 0 out of range 1..2147483647"},
		{"core size before cycle limit", with(func(c *Config) { c.CoreSize, c.CycleLimit = 0, 0 }), two, InvalidConfig, "invalid config: CORESIZE 0 out of range 1..1048576"},
		{"max processes before min separation", with(func(c *Config) { c.MaxProcesses, c.MinSeparation = 0, -1 }), two, InvalidConfig, "invalid config: MAXPROCESSES 0 out of range 1..1048576"},
		{"config before bots", with(func(c *Config) { c.CoreSize = 0 }), nil, InvalidConfig, "invalid config: CORESIZE 0 out of range 1..1048576"},
		{"no bots", DefaultConfig(), nil, InvalidBots, "invalid bots: need 2..1048576 bots, got 0"},
		{"one bot", DefaultConfig(), []Bot{ok}, InvalidBots, "invalid bots: need 2..1048576 bots, got 1"},
		{"too many bots", DefaultConfig(), make([]Bot, 1<<20+1), InvalidBots, "invalid bots: need 2..1048576 bots, got 1048577"},
		{"bot length", DefaultConfig(), []Bot{ok, {}}, InvalidBots, "invalid bots: bot 2 length 0 out of range 1..1048576"},
		{"entry high", DefaultConfig(), []Bot{ok, {Cells: ok.Cells, Entry: 2}}, InvalidBots, "invalid bots: bot 2 entry offset 2 outside 0..1"},
		{"entry negative", DefaultConfig(), []Bot{{Cells: ok.Cells, Entry: -1}, ok}, InvalidBots, "invalid bots: bot 1 entry offset -1 outside 0..1"},
		{"bot by bot", DefaultConfig(), []Bot{{Cells: ok.Cells, Entry: 5}, {}}, InvalidBots, "invalid bots: bot 1 entry offset 5 outside 0..1"},
		{"length before entry of the next bot", DefaultConfig(), []Bot{ok, {Entry: 3}, {Cells: ok.Cells, Entry: 9}}, InvalidBots, "invalid bots: bot 2 length 0 out of range 1..1048576"},
		{"bots before placement", with(func(c *Config) { c.CoreSize = 1 }), []Bot{ok, {}}, InvalidBots, "invalid bots: bot 2 length 0 out of range 1..1048576"},
		{"placement", with(func(c *Config) { c.CoreSize = 8 }), two, PlacementImpossible, "placement impossible: 2 bots of total length 4 with min separation 100 need 204 cells, core size is 8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, fn := range map[string]func() error{
				"Validate": func() error { return Validate(tt.cfg, tt.bots) },
				"Run":      func() error { _, _, err := Run(tt.cfg, tt.bots); return err },
			} {
				e, ok := fn().(*Error)
				if !ok {
					t.Fatalf("%s: err is not an *Error", name)
				}
				if e.Kind != tt.kind || e.Msg != tt.want {
					t.Errorf("%s: got kind %d %q, want kind %d %q", name, e.Kind, e.Msg, tt.kind, tt.want)
				}
			}
		})
	}
	if err := Validate(DefaultConfig(), two); err != nil {
		t.Errorf("valid match: %v", err)
	}
}

func TestDefaultConfig(t *testing.T) {
	want := Config{CoreSize: 8000, CycleLimit: 80000, MaxProcesses: 8000, MinSeparation: 100, Seed: 0}
	if got := DefaultConfig(); got != want {
		t.Errorf("DefaultConfig() = %+v, want %+v", got, want)
	}
}

func TestOutcomeString(t *testing.T) {
	if Win.String() != "win" || Tie.String() != "tie" {
		t.Errorf("got %q and %q", Win, Tie)
	}
}

// TestLoad checks the loading rules of match.md section 5: operand values are
// reduced with Norm (C69: the assembler emits raw signed values), every loaded cell
// carries its bot id as writer, the cells wrap around the end of the core and the
// bot starts with one process at Norm(a + e).
func TestLoad(t *testing.T) {
	cfg := Config{CoreSize: 100, CycleLimit: 10, MaxProcesses: 4, MinSeparation: 0, Seed: 7}
	lengths := []int{10, 20, 30, 40}
	entries := []int{0, 5, 29, 39}
	var bots []Bot
	for i, l := range lengths {
		var cells []isa.Cell
		for k := 0; k < l; k++ {
			cells = append(cells, isa.Cell{Opcode: isa.OpMOV, Modifier: isa.ModI,
				AMode: isa.ModeDirect, AValue: int32(-(k + 1)), BMode: isa.ModeBIndirect, BValue: int32(150 + i*1000 + k), Writer: 9})
		}
		bots = append(bots, Bot{Cells: cells, Entry: entries[i]})
	}
	orig := make([]Bot, len(bots))
	for i, b := range bots {
		orig[i] = Bot{Cells: slices.Clone(b.Cells), Entry: b.Entry}
	}
	p, err := Place(cfg, lengths)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.Starts, []int{53, 3, 23, 63}) {
		t.Fatalf("starts = %v; the test relies on placement P4", p.Starts)
	}
	core, queues, err := load(cfg, bots, p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bots, orig) {
		t.Error("load changed the bots it was given")
	}
	owned := make([]uint32, cfg.CoreSize)
	for i, b := range bots {
		for k, c := range b.Cells {
			addr := (p.Starts[i] + k) % cfg.CoreSize
			want := c
			want.AValue = int32((int(c.AValue)%100 + 100) % 100)
			want.BValue = int32((int(c.BValue)%100 + 100) % 100)
			want.Writer = uint32(i + 1)
			if got := core.Read(int64(addr)); got != want {
				t.Errorf("bot %d cell %d at %d = %+v, want %+v", i+1, k, addr, got, want)
			}
			owned[addr] = uint32(i + 1)
		}
	}
	for a, w := range owned {
		if w == 0 && core.Read(int64(a)) != isa.EmptyCell {
			t.Errorf("cell %d is not empty", a)
		}
	}
	if len(queues) != 4 {
		t.Fatalf("%d queues, want 4", len(queues))
	}
	for i, q := range queues {
		want := int32((p.Starts[i] + entries[i]) % cfg.CoreSize)
		if got := q.Addresses(); !slices.Equal(got, []int32{want}) {
			t.Errorf("bot %d queue = %v, want [%d]", i+1, got, want)
		}
	}
}

// TestNegativeOperandRuns runs a bot with a negative assembled operand through a
// whole match.
func TestNegativeOperandRuns(t *testing.T) {
	// SEQ.AB compares the A value of its own cell (-1 as assembled, N-1 = 15 once
	// loaded) with the B value of cell 3 (15). Equal skips the DAT, so the bot lives.
	b1 := botFrom(t, "SEQ.AB #-1, $3\nDAT.F $0, $0\nJMP.B $0, $0\nDAT.F #0, #15")
	b2 := botFrom(t, srcJmp)
	res, _, err := Run(workedConfig(5), []Bot{b1, b2})
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, wantResult{5, []uint32{1, 2}, Tie, 0, []uint32{1, 2}})
}

// TestMaxProcesses checks that SPL stops adding processes at MAXPROCESSES: a bot
// that spawns at every opportunity ends with MAXPROCESSES-1 spawns.
func TestMaxProcesses(t *testing.T) {
	spawner := botFrom(t, "loop: SPL.B loop, $0\nJMP.B loop, $0")
	for _, max := range []int{1, 3, 6} {
		cfg := DefaultConfig()
		cfg.MaxProcesses, cfg.CycleLimit = max, 200
		res, events, err := Run(cfg, []Bot{spawner, botFile(t, "sitter.s")})
		if err != nil {
			t.Fatal(err)
		}
		spawns := 0
		for _, e := range events {
			if e.Kind == vm.ProcessSpawn && e.Bot == 1 {
				spawns++
			}
		}
		if spawns != max-1 {
			t.Errorf("MAXPROCESSES %d: %d spawns, want %d", max, spawns, max-1)
		}
		checkResult(t, res, wantResult{200, []uint32{1, 2}, Tie, 0, []uint32{1, 2}})
	}
}
