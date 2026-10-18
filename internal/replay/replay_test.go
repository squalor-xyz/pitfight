// SPDX-License-Identifier: AGPL-3.0-or-later

package replay

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/isa"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// Inline bots for the cases that need small or particular programs; the golden and
// full-match tests use the starter bots of bots/ (starter).
const (
	impSrc = "MOV.I $0, $1\n"
	// bomberSrc is the four-cell bomber of docs/spec/isa.md section 8.
	bomberSrc = "ADD.AB #4, $3\nMOV.I $2, @2\nJMP.B $-2, $0\nDAT.F $0, $0\n"
	// replicator and scanner of isa.md section 8.
	replicatorSrc = "MOV.A #-5, $5\nMOV.AB #95, $4\nMOV.I }3, >3\nJMN.A $-1, $2\nSPL.B $96, $0\nDAT.F $0, $0\n"
	scannerSrc    = "ADD.AB #7, $5\nSNE.I @4, $5\nJMP.B $-2, $0\nMOV.I $3, @2\nJMP.B $-4, $0\nDAT.F $0, $0\nDAT.F $0, $0\n"
	// splitterSrc spawns a process that dies at once, then loops.
	splitterSrc = "SPL.B $2, $0\nJMP.B $0, $0\nDAT.F $0, $0\n"
	// predecSrc uses a predecrement and a post-increment.
	predecSrc = "MOV.I $2, <1\nMOV.I $1, >1\nDAT.F $3, $5\nJMP.B $-3, $0\n"
	// nopDatSrc and sitterSrc are bots M4 of match.md section 8.
	sitterSrc = "JMP.B $0, $0\n"
	nopDatSrc = "NOP.F $0, $0\nDAT.F $0, $0\n"
)

// goldenHash is the replay hash of testdata/imp_dwarf_seed1.jsonl, the starter bots
// imp and dwarf at seed 1 with a cycle limit of 40. The fixture and the hash were
// produced by a Python stepper written apart from the Go code
// (docs/spec/replay.md section 9).
const goldenHash = "24f20dbf52fa877d9c4c92cfcf109593a45540ebe2d4fcbc52389f06c4ecc26e"

// smallHash is the replay hash of testdata/win_death.jsonl.
const smallHash = "a873bbed14b1dfa66343725f004024b0ff4ced141de5e931e54c8d02ecd1f4f3"

// starter returns the source of the starter bot bots/<name>.s.
func starter(t testing.TB, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "bots", name+".s"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func bot(t testing.TB, src string) arena.Bot {
	t.Helper()
	p, err := asm.Assemble("test.s", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return arena.Bot{Cells: p.Cells, Entry: p.Entry}
}

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func goldenConfig() arena.Config {
	cfg := arena.DefaultConfig()
	cfg.Seed = 1
	cfg.CycleLimit = 40
	return cfg
}

func smallConfig() arena.Config {
	return arena.Config{CoreSize: 16, CycleLimit: 5, MaxProcesses: 8, MinSeparation: 0, Seed: 1}
}

func recordBytes(t testing.TB, cfg arena.Config, srcs ...string) []byte {
	t.Helper()
	bots := make([]arena.Bot, len(srcs))
	for i, s := range srcs {
		bots[i] = bot(t, s)
	}
	r, err := Record(cfg, bots)
	if err != nil {
		t.Fatal(err)
	}
	return r.Bytes()
}

// TestGoldenReplay: imp against dwarf at seed 1 with a cycle limit of 40 writes the
// bytes of the fixture, and the replay hash is the pinned one. The fixture and the
// hash come from a separate implementation (docs/spec/replay.md section 9).
func TestGoldenReplay(t *testing.T) {
	want := fixture(t, "imp_dwarf_seed1.jsonl")
	imp, dwarf := starter(t, "imp"), starter(t, "dwarf")
	got := recordBytes(t, goldenConfig(), imp, dwarf)
	if !bytes.Equal(got, want) {
		t.Errorf("replay differs from the fixture:\n%s\nwant:\n%s", got, want)
	}
	if h := Hash(got); h != goldenHash {
		t.Errorf("hash = %s, want %s", h, goldenHash)
	}
	if h := Hash(want); h != goldenHash {
		t.Errorf("hash of the fixture = %s, want %s", h, goldenHash)
	}

	// The same through Write.
	var buf bytes.Buffer
	r, err := Record(goldenConfig(), []arena.Bot{bot(t, imp), bot(t, dwarf)})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Write(&buf); err != nil || !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("Write: err %v, equal %v", err, bytes.Equal(buf.Bytes(), want))
	}
}

// fullHash is the replay hash of the starter bots imp and dwarf at seed 1 with the
// default configuration: the match ends at cycle 5880 with a win for the dwarf and
// the replay is 782675 bytes. The Python stepper that wrote the fixture computed it
// (docs/spec/replay.md section 9); there is no file.
const fullHash = "0c7024334a7c20fe7f74b39b97d0c9693980d41f0351f3422846c3e2caeac831"

// TestFullMatchHash pins the C16 command, `pit run bots/imp.s bots/dwarf.s --seed 1
// --replay F`, whose replay is too long to keep as a fixture.
func TestFullMatchHash(t *testing.T) {
	cfg := arena.DefaultConfig()
	cfg.Seed = 1
	data := recordBytes(t, cfg, starter(t, "imp"), starter(t, "dwarf"))
	if h := Hash(data); h != fullHash {
		t.Errorf("hash = %s, want %s (%d bytes)", h, fullHash, len(data))
	}
	if !bytes.HasSuffix(data, []byte(`{"t":"end","cycles":5880,"survivors":[2],"outcome":"win","winner":2,"tied":[]}`+"\n")) {
		t.Errorf("unexpected end of the replay: %s", data[len(data)-120:])
	}
}

// TestSmallReplay checks every line type, a death and a win against a fixture
// written by the Python stepper: M4 of match.md section 8 on a core of 16.
func TestSmallReplay(t *testing.T) {
	want := fixture(t, "win_death.jsonl")
	got := recordBytes(t, smallConfig(), sitterSrc, nopDatSrc)
	if !bytes.Equal(got, want) {
		t.Errorf("replay:\n%s\nwant:\n%s", got, want)
	}
	if h := Hash(got); h != smallHash {
		t.Errorf("hash = %s, want %s", h, smallHash)
	}
}

// TestBotHash pins the bot hash encoding of docs/spec/replay.md section 6 with
// values computed by the Python stepper, and checks what the hash ignores and
// what it covers.
func TestBotHash(t *testing.T) {
	imp := bot(t, starter(t, "imp"))
	if got, want := BotHash(imp), "219f569daf493aa9eef85007239a2ecce8c9147cadd2fcc66c267de6b5dc0f8f"; got != want {
		t.Errorf("imp hash = %s, want %s", got, want)
	}
	dwarf := bot(t, starter(t, "dwarf"))
	if got, want := BotHash(dwarf), "a282a0bde2c6ce055a95ed13b21bed1375d47af54b0a69a18eed0a18b95ab6b8"; got != want {
		t.Errorf("dwarf hash = %s, want %s", got, want)
	}
	// The writer is not part of the hash; the entry offset and the raw values are.
	w := arena.Bot{Cells: slices.Clone(dwarf.Cells), Entry: dwarf.Entry}
	w.Cells[1].Writer = 7
	if BotHash(w) != BotHash(dwarf) {
		t.Error("the writer changed the hash")
	}
	e := arena.Bot{Cells: slices.Clone(dwarf.Cells), Entry: 1}
	if BotHash(e) == BotHash(dwarf) {
		t.Error("the entry offset did not change the hash")
	}
	n := arena.Bot{Cells: slices.Clone(dwarf.Cells), Entry: dwarf.Entry}
	n.Cells[2].AValue += 8000 // same cell once normalized, different bot as assembled
	if BotHash(n) == BotHash(dwarf) {
		t.Error("a raw value did not change the hash")
	}
}

// TestDeterministicFile: the same match written twice to files gives identical
// bytes and hashes, also from two separate recordings.
func TestDeterministicFile(t *testing.T) {
	cfg := goldenConfig()
	cfg.CycleLimit = 500
	bots := []arena.Bot{bot(t, starter(t, "imp")), bot(t, starter(t, "dwarf"))}
	dir := t.TempDir()
	var datas [2][]byte
	for i := range datas {
		r, err := Record(cfg, bots)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "replay"+string(rune('a'+i))+".jsonl")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Write(f); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if datas[i], err = os.ReadFile(path); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(datas[0], datas[1]) {
		t.Error("two files of the same match differ")
	}
	if Hash(datas[0]) != Hash(datas[1]) || len(Hash(datas[0])) != 64 {
		t.Errorf("hashes %q and %q", Hash(datas[0]), Hash(datas[1]))
	}
	if len(datas[0]) < 10000 {
		t.Errorf("unexpectedly short replay: %d bytes", len(datas[0]))
	}
}

type matchCase struct {
	name string
	cfg  arena.Config
	srcs []string
}

func matchCases(t testing.TB) []matchCase {
	return []matchCase{
		// The starter bots to the end of the match (cycle 5880).
		{"starter-imp-dwarf", arena.Config{CoreSize: 8000, CycleLimit: 80000, MaxProcesses: 8000, MinSeparation: 100, Seed: 1}, []string{starter(t, "imp"), starter(t, "dwarf")}},
		{"starter-replicator-scanner", arena.Config{CoreSize: 8000, CycleLimit: 800, MaxProcesses: 8000, MinSeparation: 100, Seed: 2}, []string{starter(t, "replicator"), starter(t, "scanner")}},
		{"imp-bomber", arena.Config{CoreSize: 8000, CycleLimit: 300, MaxProcesses: 8000, MinSeparation: 100, Seed: 1}, []string{impSrc, bomberSrc}},
		{"replicator-scanner", arena.Config{CoreSize: 2000, CycleLimit: 600, MaxProcesses: 50, MinSeparation: 0, Seed: 3}, []string{replicatorSrc, scannerSrc}},
		{"spawn-and-death", arena.Config{CoreSize: 64, CycleLimit: 10, MaxProcesses: 2, MinSeparation: 0, Seed: 5}, []string{splitterSrc, impSrc}},
		{"predec-wrap-three-bots", arena.Config{CoreSize: 50, CycleLimit: 120, MaxProcesses: 8, MinSeparation: 0, Seed: 7}, []string{predecSrc, impSrc, bomberSrc}},
		{"first-bot-dies", smallConfig(), []string{nopDatSrc, sitterSrc}},
	}
}

// sameSteps reports whether two step lists hold the same bots, addresses and events.
func sameSteps(a, b []arena.Step) bool {
	return slices.EqualFunc(a, b, func(x, y arena.Step) bool {
		return x.Bot == y.Bot && x.PC == y.PC && slices.Equal(x.Events, y.Events)
	})
}

// TestReplayEquivalence: read a replay back from its bytes and apply it; the core
// is the live simulation's core after every cycle, the steps of every cycle (bot,
// executed address and events, silent steps included) are the live steps, and the
// live process counts end where the result says.
func TestReplayEquivalence(t *testing.T) {
	kinds := map[vm.EventKind]bool{}
	silentTotal := 0
	for _, mc := range matchCases(t) {
		t.Run(mc.name, func(t *testing.T) {
			bots := make([]arena.Bot, len(mc.srcs))
			for i, s := range mc.srcs {
				bots[i] = bot(t, s)
			}
			rec, err := Record(mc.cfg, bots)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Read(bytes.NewReader(rec.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			p, err := NewPlayer(got)
			if err != nil {
				t.Fatal(err)
			}

			// Live: every cycle, including cycle 0 (the loaded core).
			cycles, silent := 0, 0
			res, err := arena.RunObserved(mc.cfg, bots, func(cycle int, core *vm.Core, steps []arena.Step) {
				if cycle != cycles {
					t.Fatalf("hook called for cycle %d, want %d", cycle, cycles)
				}
				cycles++
				if err := p.AdvanceTo(cycle); err != nil {
					t.Fatal(err)
				}
				if p.Cycle() != cycle {
					t.Fatalf("player at cycle %d, want %d", p.Cycle(), cycle)
				}
				if !sameSteps(p.Steps(), steps) {
					t.Fatalf("cycle %d: live steps %+v, replayed steps %+v", cycle, steps, p.Steps())
				}
				if cycle > 0 && len(steps) == 0 {
					t.Fatalf("cycle %d has no steps", cycle)
				}
				silent += countSilent(steps)
				// The pc of a step is the pc the VM itself puts in the step's events.
				for _, st := range steps {
					for _, ev := range st.Events {
						if ev.Bot != st.Bot || ev.PC != st.PC {
							t.Fatalf("cycle %d: step of bot %d at %d has the event %+v", cycle, st.Bot, st.PC, ev)
						}
					}
				}
				for a := 0; a < mc.cfg.CoreSize; a++ {
					if live, replayed := core.Read(int64(a)), p.Core().Read(int64(a)); live != replayed {
						t.Fatalf("cycle %d address %d: live %+v, replayed %+v", cycle, a, live, replayed)
					}
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if cycles != res.Cycles+1 {
				t.Errorf("hook ran %d times, want %d", cycles, res.Cycles+1)
			}

			// The events are the events of arena.Run, grouped by cycle.
			_, flat, err := arena.Run(mc.cfg, bots)
			if err != nil {
				t.Fatal(err)
			}
			var fromReplay []vm.Event
			for i, c := range got.Cycles {
				if c.N != i+1 || len(c.Steps) == 0 {
					t.Errorf("cycle entry %d is cycle %d with %d steps", i, c.N, len(c.Steps))
				}
				for _, st := range c.Steps {
					fromReplay = append(fromReplay, st.Events...)
				}
			}
			if len(got.Cycles) != res.Cycles {
				t.Errorf("%d cycles in the replay, the match ran %d", len(got.Cycles), res.Cycles)
			}
			if !slices.Equal(fromReplay, flat) {
				t.Errorf("replayed events differ from arena.Run: %d against %d", len(fromReplay), len(flat))
			}
			for _, ev := range flat {
				kinds[ev.Kind] = true
			}
			silentTotal += silent

			// The result and the placement.
			if got.Result.Cycles != res.Cycles || !slices.Equal(got.Result.Survivors, res.Survivors) ||
				got.Result.Outcome != res.Outcome || got.Result.Winner != res.Winner || !slices.Equal(got.Result.Tied, res.Tied) {
				t.Errorf("result %+v, want %+v", got.Result, res)
			}
			if !slices.Equal(got.Result.Placement.Starts, res.Placement.Starts) {
				t.Errorf("starts %v, want %v", got.Result.Placement.Starts, res.Placement.Starts)
			}
			if err := p.AdvanceTo(res.Cycles); err != nil {
				t.Fatal(err)
			}
			for id := uint32(1); id <= uint32(len(bots)); id++ {
				if live, survivor := p.Live(id) > 0, slices.Contains(res.Survivors, id); live != survivor {
					t.Errorf("bot %d: live %v (%d processes), survivor %v", id, live, p.Live(id), survivor)
				}
			}
			if got.Config != mc.cfg {
				t.Errorf("config %+v, want %+v", got.Config, mc.cfg)
			}
		})
	}
	// The cases must exercise every event kind and silent steps, or the test proves little.
	if silentTotal == 0 {
		t.Error("no case had a step without events")
	}
	for _, k := range []vm.EventKind{vm.CellWrite, vm.ProcessSpawn, vm.ProcessDeath} {
		if !kinds[k] {
			t.Errorf("no case produced event kind %d", k)
		}
	}
}

// countSilent counts the steps without events.
func countSilent(steps []arena.Step) int {
	n := 0
	for _, st := range steps {
		if len(st.Events) == 0 {
			n++
		}
	}
	return n
}

// TestPlayerLiveCounts: process counts follow the spawn and death events.
func TestPlayerLiveCounts(t *testing.T) {
	// splitter: cycle 1 spawns a second process, which runs after the first one
	// in cycle 3 and dies.
	cfg := arena.Config{CoreSize: 64, CycleLimit: 4, MaxProcesses: 4, MinSeparation: 0, Seed: 5}
	r, err := Record(cfg, []arena.Bot{bot(t, splitterSrc), bot(t, impSrc)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPlayer(r)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 2, 1, 1} // after cycle 0 .. 4: spawn in cycle 1, the child dies in cycle 3
	for c, w := range want {
		if err := p.AdvanceTo(c); err != nil {
			t.Fatal(err)
		}
		if got := p.Live(1); got != w {
			t.Errorf("cycle %d: bot 1 has %d processes, want %d", c, got, w)
		}
		if got := p.Live(2); got != 1 {
			t.Errorf("cycle %d: bot 2 has %d processes, want 1", c, got)
		}
	}
	if p.Live(0) != 0 || p.Live(3) != 0 {
		t.Error("an unknown bot has processes")
	}
	if err := p.AdvanceTo(2); err == nil {
		t.Error("AdvanceTo went backwards without an error")
	}
}

// TestRoundTrip: reading the golden fixture and writing it again gives the same
// bytes, and the parsed values are the match's.
func TestRoundTrip(t *testing.T) {
	data := fixture(t, "imp_dwarf_seed1.jsonl")
	r, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r.Bytes(), data) {
		t.Error("Read then Bytes changed the file")
	}
	if r.Config.Seed != 1 || r.Config.CycleLimit != 40 || r.Config.CoreSize != 8000 || len(r.Bots) != 2 {
		t.Errorf("config %+v, %d bots", r.Config, len(r.Bots))
	}
	if r.Bots[1].Start != 2555 || r.Bots[1].EntryAddress != 2555 || len(r.Bots[1].Cells) != 5 {
		t.Errorf("bot 2 %+v", r.Bots[1])
	}
	if r.Result.Cycles != 40 || r.Result.Outcome != arena.Tie || !slices.Equal(r.Result.Tied, []uint32{1, 2}) {
		t.Errorf("result %+v", r.Result)
	}
}

// TestSeedIsString: a seed above 2^53 is a decimal string in the header and
// survives the round trip.
func TestSeedIsString(t *testing.T) {
	cfg := smallConfig()
	cfg.Seed = 18446744073709551615
	data := recordBytes(t, cfg, sitterSrc, nopDatSrc)
	if !bytes.Contains(data, []byte(`"seed":"18446744073709551615"`)) {
		t.Fatalf("header lacks the seed string:\n%s", data)
	}
	r, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if r.Config.Seed != cfg.Seed {
		t.Errorf("seed %d, want %d", r.Config.Seed, cfg.Seed)
	}
}

func TestUnknownVersion(t *testing.T) {
	for _, version := range []string{"2", "0", "99", "-1"} {
		data := `{"t":"header","version":` + version + `,"anything":["else"]}` + "\n"
		_, err := Read(strings.NewReader(data))
		var ve *VersionError
		if !errors.As(err, &ve) {
			t.Errorf("version %s: error %v is not a *VersionError", version, err)
			continue
		}
		if !strings.Contains(err.Error(), "version "+version) || !strings.Contains(err.Error(), "version 1") {
			t.Errorf("version %s: error %q does not name the version and the supported one", version, err)
		}
	}
	// A version that is not an integer is an error, but not a version error.
	for _, bad := range []string{`"1"`, `1.0`, `null`, `1e0`} {
		_, err := Read(strings.NewReader(`{"t":"header","version":` + bad + "}\n"))
		var ve *VersionError
		if err == nil || errors.As(err, &ve) {
			t.Errorf("version %s: error %v", bad, err)
		}
	}
}

// TestReadErrors damages the small fixture one way at a time; each must be refused.
func TestReadErrors(t *testing.T) {
	small := string(fixture(t, "win_death.jsonl"))
	// header, load 1, load 2, c1, x1, x2, c2, x1, x2, d, end, "".
	lines := strings.SplitAfter(small, "\n")
	if len(lines) != 12 {
		t.Fatalf("fixture has %d lines", len(lines)-1)
	}
	join := func(idx ...int) string {
		var b strings.Builder
		for _, i := range idx {
			b.WriteString(lines[i])
		}
		return b.String()
	}
	replace := func(old, new string) string {
		if strings.Count(small, old) != 1 {
			t.Fatalf("%q occurs %d times in the fixture", old, strings.Count(small, old))
		}
		return strings.Replace(small, old, new, 1)
	}
	// withEnd gives the fixture with another end line, from "survivors" on.
	withEnd := func(fields string) string {
		return strings.Replace(small, `"survivors":[1],"outcome":"win","winner":1,"tied":[]`, fields, 1)
	}
	const (
		winEnd = `{"t":"end","cycles":%d,"survivors":[1],"outcome":"win","winner":1,"tied":[]}` + "\n"
	)
	head := join(0, 1, 2)
	// bot 2 spawns in cycle 1 and dies in cycles 2 and 3: it has no process in cycle 4.
	twoLives := head +
		"{\"t\":\"c\",\"n\":1}\n{\"t\":\"x\",\"b\":1,\"pc\":7}\n{\"t\":\"x\",\"b\":2,\"pc\":0}\n{\"t\":\"s\",\"a\":5}\n" +
		"{\"t\":\"c\",\"n\":2}\n{\"t\":\"x\",\"b\":1,\"pc\":7}\n{\"t\":\"x\",\"b\":2,\"pc\":1}\n{\"t\":\"d\"}\n" +
		"{\"t\":\"c\",\"n\":3}\n{\"t\":\"x\",\"b\":1,\"pc\":7}\n{\"t\":\"x\",\"b\":2,\"pc\":5}\n{\"t\":\"d\"}\n"
	cycle4 := "{\"t\":\"c\",\"n\":4}\n{\"t\":\"x\",\"b\":1,\"pc\":7}\n"
	tests := []struct {
		name string
		data string
		want string // substring of the error
	}{
		{"empty", "", "empty"},
		{"no final newline", strings.TrimSuffix(small, "\n"), "newline"},
		{"carriage return", replace(`"tied":[]}`+"\n", `"tied":[]}`+"\r\n"), "line 11"},
		{"blank line", lines[0] + "\n" + join(1, 2, 3, 4, 5, 6, 7, 8, 9, 10), "line 2"},
		{"no load lines", join(0, 10), "line 2"},
		{"load before header", join(1, 0), "line 1"},
		{"missing end", join(0, 1, 2, 3, 4, 5, 6, 7, 8, 9), "end"},
		{"line after end", small + lines[3], "after"},
		{"two headers", lines[0] + small, "line 2"},
		{"unknown line type", replace(`{"t":"c","n":2}`, `{"t":"q","n":2}`), "line 7"},
		{"unknown field", replace(`"winner":1,`, `"winner":1,"extra":0,`), "line 11"},
		{"header wrong type", replace(`{"t":"header","version":1,`, `{"t":"x","version":1,`), "expected the header"},
		{"space", replace(`"n":2}`, `"n": 2}`), "line 7"},
		{"field order", replace(`{"t":"x","b":2,"pc":1}`, `{"t":"x","pc":1,"b":2}`), "line 9"},
		{"leading zeros", replace(`"pc":1}`, `"pc":01}`), "line 9"},
		{"seed number", replace(`"seed":"1"`, `"seed":1`), "line 1"},
		{"seed leading zero", replace(`"seed":"1"`, `"seed":"01"`), "line 1"},
		{"seed too large", replace(`"seed":"1"`, `"seed":"18446744073709551616"`), "line 1"},
		{"core size zero", replace(`"core_size":16`, `"core_size":0`), "core_size 0 out of range 1..1048576"},
		{"core size huge", replace(`"core_size":16`, `"core_size":1048577`), "core_size 1048577 out of range 1..1048576"},
		{"cycle limit zero", replace(`"cycle_limit":5`, `"cycle_limit":0`), "cycle_limit 0 out of range 1..2147483647"},
		{"max processes zero", replace(`"max_processes":8`, `"max_processes":0`), "max_processes 0 out of range 1..1048576"},
		{"min separation negative", replace(`"min_separation":0`, `"min_separation":-1`), "min_separation -1 out of range 0..1048576"},
		{"bot id", replace(`"id":2,`, `"id":3,`), "line 1"},
		{"bot hash not hex", strings.Replace(small, `"hash":"2915`, `"hash":"2G15`, 1), "hash"},
		{"bot hash short", replace(`"hash":"2915d3c360b2ce34c9adb3b60eccfd1b946d4055327776eeda1d20410d70ff2b"`, `"hash":"2915"`), "hash"},
		{"start out of range", replace(`"start":7,`, `"start":16,`), "start 16 out of range 0..15"},
		{"start negative", replace(`"start":7,`, `"start":-1,`), "start -1 out of range 0..15"},
		{"entry_address negative", replace(`"entry_address":7}`, `"entry_address":-1}`), "entry_address -1 out of range 0..15"},
		{"entry_address too large", replace(`"entry_address":7}`, `"entry_address":16}`), "entry_address 16 out of range 0..15"},
		{"length zero", replace(`"length":1,`, `"length":0,`), "length 0 out of range 1..16"},
		{"length longer than the core", strings.Replace(replace(`"length":1,`, `"length":17,`), `[[6,1,1,0,1,0]]`, "["+strings.Repeat(`[6,1,1,0,1,0],`, 16)+`[6,1,1,0,1,0]]`, 1), "length 17 out of range 1..16"},
		{"one bot", `{"t":"header","version":1,"config":{"core_size":16,"cycle_limit":5,"max_processes":8,"min_separation":0,"seed":"1"},"bots":[` +
			strings.SplitN(strings.SplitN(lines[0], `"bots":[`, 2)[1], "},{", 2)[0] + "}]}\n" + join(1, 3, 4, 5, 6, 7, 8, 9, 10), "bots"},
		{"load bot id", replace(`{"t":"load","bot":2,`, `{"t":"load","bot":1,`), "load line for bot 1, want bot 2"},
		{"load cell count", replace(`"cells":[[14,4,1,0,1,0],[0,4,1,0,1,0]]`, `"cells":[[14,4,1,0,1,0]]`), "line 3"},
		{"cell opcode", replace(`[14,4,1,0,1,0]`, `[15,4,1,0,1,0]`), "opcode"},
		{"cell modifier", replace(`[14,4,1,0,1,0]`, `[14,7,1,0,1,0]`), "modifier"},
		{"cell mode", replace(`[14,4,1,0,1,0]`, `[14,4,8,0,1,0]`), "mode"},
		{"cell value", replace(`[14,4,1,0,1,0]`, `[14,4,1,16,1,0]`), "value"},
		{"cell value negative", replace(`[14,4,1,0,1,0]`, `[14,4,1,0,1,-1]`), "value"},
		{"cell too short", replace(`[14,4,1,0,1,0]`, `[14,4,1,0,1]`), "line 3"},
		{"cell too long", replace(`[14,4,1,0,1,0]`, `[14,4,1,0,1,0,0]`), "line 3"},

		{"cycle zero", replace(`{"t":"c","n":1}`, `{"t":"c","n":0}`), "cycle 0, want 1"},
		{"first cycle is 2", replace(`{"t":"c","n":1}`, `{"t":"c","n":2}`), "cycle 2, want 1"},
		{"cycle skipped", replace(`{"t":"c","n":2}`, `{"t":"c","n":3}`), "cycle 3, want 2"},
		{"cycle repeated", join(0, 1, 2, 3, 4, 5, 3, 4, 5, 10), "cycle 1, want 2"},
		{"cycle past limit", replace(`"cycle_limit":5`, `"cycle_limit":1`), "cycle 2 is past cycle_limit 1"},
		{"cycle without steps", join(0, 1, 2, 3, 6, 7, 8, 9, 10), "cycle 1 has no steps"},
		{"last cycle without steps", join(0, 1, 2, 3, 4, 5, 6, 10), "cycle 2 has no steps"},
		{"no cycles", join(0, 1, 2, 10), "cycles 2, but the last cycle is 0"},
		{"step before the first cycle", join(0, 1, 2, 4, 5, 10), "step before the first cycle"},
		{"event before a step", join(0, 1, 2, 3, 9, 10), "event before a step"},
		{"event right after a later cycle line", join(0, 1, 2, 3, 4, 5, 6, 9, 10), "line 8: event before a step"},
		{"step bot zero", replace(`{"t":"x","b":2,"pc":0}`, `{"t":"x","b":0,"pc":0}`), "bot 0 out of range 1..2"},
		{"step bot too big", replace(`{"t":"x","b":2,"pc":0}`, `{"t":"x","b":3,"pc":0}`), "bot 3 out of range 1..2"},
		{"step pc out of range", replace(`{"t":"x","b":2,"pc":0}`, `{"t":"x","b":2,"pc":16}`), "pc 16 out of range 0..15"},
		{"step pc negative", replace(`{"t":"x","b":2,"pc":0}`, `{"t":"x","b":2,"pc":-1}`), "pc -1 out of range 0..15"},
		{"steps in descending bot order", join(0, 1, 2, 3, 5, 4, 6, 7, 8, 9, 10), "bot 1 steps after bot 2"},
		{"bot steps twice in a cycle", replace(`{"t":"x","b":2,"pc":0}`, `{"t":"x","b":1,"pc":0}`), "bot 1 steps after bot 1"},
		{"step with a stray field", replace(`{"t":"x","b":1,"pc":7}`+"\n"+`{"t":"x","b":2,"pc":0}`, `{"t":"x","b":1,"pc":7,"a":1}`+"\n"+`{"t":"x","b":2,"pc":0}`), "line 5"},
		{"dead bot steps", join(0, 1, 2, 3, 4, 5, 6, 7, 8, 9) + "{\"t\":\"c\",\"n\":3}\n{\"t\":\"x\",\"b\":1,\"pc\":7}\n{\"t\":\"x\",\"b\":2,\"pc\":1}\n" + fmtEnd(winEnd, 3), "bot 2 steps with no live process"},
		{"spawn address out of range", replace(`{"t":"d"}`, `{"t":"s","a":16}`), "address 16 out of range"},
		{"spawn without address", replace(`{"t":"d"}`, `{"t":"s"}`), "line 10"},
		{"write address out of range", replace(`{"t":"d"}`, `{"t":"w","a":16,"c":[0,4,1,0,1,0]}`), "address 16 out of range"},
		{"write address negative", replace(`{"t":"d"}`, `{"t":"w","a":-1,"c":[0,4,1,0,1,0]}`), "address -1 out of range"},
		{"write without cell", replace(`{"t":"d"}`, `{"t":"w","a":1}`), "write without a cell"},
		{"death with address", replace(`{"t":"d"}`, `{"t":"d","a":1}`), "line 10"},
		{"two deaths in a step", replace(`{"t":"d"}`+"\n", `{"t":"d"}`+"\n"+`{"t":"d"}`+"\n"), "after the spawn or death"},
		{"write after a death", replace(`{"t":"d"}`+"\n", `{"t":"d"}`+"\n"+`{"t":"w","a":1,"c":[0,4,1,0,1,0]}`+"\n"), "after the spawn or death"},
		{"death after a spawn", replace(`{"t":"d"}`+"\n", `{"t":"s","a":1}`+"\n"+`{"t":"d"}`+"\n"), "after the spawn or death"},
		{"spawned bot stepping after its deaths", twoLives + cycle4 + "{\"t\":\"x\",\"b\":2,\"pc\":1}\n" + fmtEnd(winEnd, 4), "bot 2 steps with no live process"},
		{"end cycles zero", replace(`"cycles":2,`, `"cycles":0,`), "cycles 0 out of range 1..5"},
		{"end cycles past limit", replace(`"cycles":2,`, `"cycles":6,`), "cycles 6 out of range 1..5"},
		{"end cycles before last cycle", replace(`"cycles":2,`, `"cycles":1,`), "cycles 1, but the last cycle is 2"},
		{"end outcome", replace(`"outcome":"win"`, `"outcome":"draw"`), "outcome"},
		{"win with tie fields", replace(`"winner":1,"tied":[]`, `"winner":0,"tied":[1]`), "outcome"},
		{"win with the wrong winner", withEnd(`"survivors":[1],"outcome":"win","winner":2,"tied":[]`), "outcome win"},
		{"win with tied bots", withEnd(`"survivors":[1],"outcome":"win","winner":1,"tied":[1]`), "outcome win"},
		{"win without survivors", withEnd(`"survivors":[],"outcome":"win","winner":0,"tied":[]`), "outcome win"},
		{"tie with a winner", withEnd(`"survivors":[1,2],"outcome":"tie","winner":1,"tied":[1,2]`), "outcome tie"},
		{"tie without tied bots", withEnd(`"survivors":[],"outcome":"tie","winner":0,"tied":[]`), "outcome tie"},
		{"tie with one survivor", withEnd(`"survivors":[1],"outcome":"tie","winner":0,"tied":[1]`), "outcome tie"},
		{"tie of other bots than the survivors", withEnd(`"survivors":[1,2],"outcome":"tie","winner":0,"tied":[1]`), "outcome tie"},
		{"survivors that still have processes", withEnd(`"survivors":[1,2],"outcome":"tie","winner":0,"tied":[1,2]`), "survivors [1 2] do not match the live processes"},
		{"survivor without processes", withEnd(`"survivors":[2],"outcome":"win","winner":2,"tied":[]`), "survivors [2] do not match the live processes"},
		{"survivor unknown bot", replace(`"survivors":[1]`, `"survivors":[3]`), "survivors [3] are not ascending bot ids 1..2"},
		{"survivor zero", replace(`"survivors":[1]`, `"survivors":[0]`), "survivors [0] are not ascending"},
		{"survivors unsorted", withEnd(`"survivors":[2,1],"outcome":"tie","winner":0,"tied":[1,2]`), "survivors [2 1] are not ascending"},
		{"survivors repeated", withEnd(`"survivors":[1,1],"outcome":"tie","winner":0,"tied":[1,1]`), "survivors [1 1] are not ascending"},
		{"tied unsorted", withEnd(`"survivors":[],"outcome":"tie","winner":0,"tied":[2,1]`), "tied [2 1] are not ascending"},
		{"not json", "not json\n", "line 1"},
		{"number overflow", replace(`"n":2`, `"n":99999999999999999999`), "line 7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Read(strings.NewReader(tt.data))
			if err == nil {
				t.Fatalf("accepted, parsed %d bots", len(r.Bots))
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q lacks %q", err, tt.want)
			}
			if r != nil {
				t.Error("a partial replay was returned with the error")
			}
		})
	}
	// The undamaged fixture is accepted, and so is a bot that spawns, dies twice and
	// stops stepping.
	if _, err := Read(strings.NewReader(small)); err != nil {
		t.Fatal(err)
	}
	r, err := Read(strings.NewReader(twoLives + cycle4 + fmtEnd(winEnd, 4)))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Cycles) != 4 || len(r.Cycles[0].Steps[1].Events) != 1 {
		t.Errorf("cycles %+v", r.Cycles)
	}
}

func fmtEnd(format string, cycles int) string { return fmt.Sprintf(format, cycles) }

// TestRecordRefusesBadMatch: Record returns the arena's error unchanged.
func TestRecordRefusesBadMatch(t *testing.T) {
	_, err := Record(arena.Config{CoreSize: 0, CycleLimit: 1, MaxProcesses: 1}, []arena.Bot{bot(t, impSrc), bot(t, impSrc)})
	var ae *arena.Error
	if !errors.As(err, &ae) || ae.Kind != arena.InvalidConfig {
		t.Errorf("error %v, want an arena InvalidConfig", err)
	}
	_, err = Record(smallConfig(), []arena.Bot{bot(t, impSrc)})
	if !errors.As(err, &ae) || ae.Kind != arena.InvalidBots {
		t.Errorf("error %v, want an arena InvalidBots", err)
	}
}

// TestSilentSteps: a match in which nothing changes still has a cycle line for every
// cycle and an execute line for every step, and the player reports the executed
// addresses.
func TestSilentSteps(t *testing.T) {
	cfg := smallConfig()
	cfg.CycleLimit = 9
	data := recordBytes(t, cfg, sitterSrc, sitterSrc)
	if n := bytes.Count(data, []byte(`{"t":"c","n":`)); n != 9 {
		t.Errorf("%d cycle lines, want 9:\n%s", n, data)
	}
	if n := bytes.Count(data, []byte(`{"t":"x",`)); n != 18 {
		t.Errorf("%d execute lines, want 18", n)
	}
	for _, kind := range []string{"w", "s", "d"} {
		if bytes.Contains(data, []byte(`{"t":"`+kind+`"`)) {
			t.Errorf("event line %q in a match without events", kind)
		}
	}
	r, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPlayer(r)
	if err != nil {
		t.Fatal(err)
	}
	if p.Steps() != nil {
		t.Errorf("steps at cycle 0: %+v", p.Steps())
	}
	if err := p.AdvanceTo(100); err != nil || p.Cycle() != 9 {
		t.Errorf("AdvanceTo(100): err %v, cycle %d; want 9", err, p.Cycle())
	}
	// Each sitter executes the address it was loaded at, every cycle.
	steps := p.Steps()
	if len(steps) != 2 || steps[0].Bot != 1 || steps[1].Bot != 2 || len(steps[0].Events) != 0 || len(steps[1].Events) != 0 {
		t.Fatalf("steps at cycle 9: %+v", steps)
	}
	for i, st := range steps {
		if int(st.PC) != r.Bots[i].EntryAddress {
			t.Errorf("bot %d executes %d, want its entry address %d", i+1, st.PC, r.Bots[i].EntryAddress)
		}
	}
}

// TestEntryAddressWraps: a bot that wraps around the end of the core, with an entry
// offset after the wrap. Seed 1 in a core of 20 cells without separation puts the
// three-cell bot at 19, 0, 1 (the case of cmd/pit TestRunEntry); its ORG is cell 2.
func TestEntryAddressWraps(t *testing.T) {
	cfg := arena.Config{CoreSize: 20, CycleLimit: 5, MaxProcesses: 8, MinSeparation: 0, Seed: 1}
	r, err := Record(cfg, []arena.Bot{bot(t, "DAT.F $0, $0\nDAT.F $0, $0\ngo: JMP.B $0, $0\nORG go\n"), bot(t, sitterSrc)})
	if err != nil {
		t.Fatal(err)
	}
	if b := r.Bots[0]; b.Start != 19 || b.EntryAddress != 1 {
		t.Errorf("bot 1 start %d, entry_address %d; want 19 and 1", b.Start, b.EntryAddress)
	}
	if !bytes.Contains(r.Bytes(), []byte(`"length":3,"start":19,"entry_address":1}`)) {
		t.Errorf("header:\n%s", r.Bytes())
	}
	// The loaded cells wrap too: the jump is at address 1, the sitter's at 2.
	p, err := NewPlayer(r)
	if err != nil {
		t.Fatal(err)
	}
	for addr, want := range map[int64]isa.Opcode{19: isa.OpDAT, 0: isa.OpDAT, 1: isa.OpJMP, 2: isa.OpJMP} {
		if got := p.Core().Read(addr).Opcode; got != want {
			t.Errorf("address %d holds %v, want %v", addr, got, want)
		}
	}
}
