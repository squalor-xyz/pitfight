// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/isa"
	"github.com/squalor-xyz/pitfight/internal/replay"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

const (
	fixturePath = "../../testdata/viewer/fixture.jsonl"
	framesPath  = "../../testdata/viewer/frames.json"
)

// loadFixture reads the shared viewer fixture.
func loadFixture(t testing.TB) *replay.Replay { return loadReplayFile(t, fixturePath) }

// loadReplayFile reads a replay file of testdata/viewer.
func loadReplayFile(t testing.TB, path string) *replay.Replay {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := replay.Read(f)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// framesFile is testdata/viewer/frames.json.
type framesFile struct {
	Fixture  string  `json:"fixture"`
	CoreSize int     `json:"core_size"`
	Cycles   int     `json:"cycles"`
	Frames   []Frame `json:"frames"`
}

func loadFrames(t testing.TB) framesFile { return loadFramesFile(t, framesPath) }

// loadFramesFile reads a frames file of testdata/viewer.
func loadFramesFile(t testing.TB, path string) framesFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ff framesFile
	if err := json.Unmarshal(data, &ff); err != nil {
		t.Fatal(err)
	}
	return ff
}

func newModel(t testing.TB, r *replay.Replay) *Model {
	t.Helper()
	m, err := NewModel(r)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestFrames checks the frame model of each shared fixture against its frames file,
// which was derived by hand from the bot sources, not from this code. The SPL
// fixture covers spawns and deaths, which the first one lacks.
func TestFrames(t *testing.T) {
	for _, tc := range []struct {
		name, fixture, frames string
		coreSize, cycles      int
	}{
		{"imp and dwarf", fixturePath, framesPath, 24, 12},
		{"SPL", "../../testdata/viewer/spl_fixture.jsonl", "../../testdata/viewer/spl_frames.json", 24, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ff := loadFramesFile(t, tc.frames)
			m := newModel(t, loadReplayFile(t, tc.fixture))
			if ff.CoreSize != tc.coreSize || ff.Cycles != tc.cycles || len(ff.Frames) != tc.cycles+1 || !strings.HasSuffix(tc.fixture, "/"+ff.Fixture) {
				t.Fatalf("frames file header: fixture %q core %d cycles %d, %d frames", ff.Fixture, ff.CoreSize, ff.Cycles, len(ff.Frames))
			}
			if m.Cycles() != ff.Cycles {
				t.Errorf("Cycles() = %d, want %d", m.Cycles(), ff.Cycles)
			}
			for _, want := range ff.Frames {
				if err := m.Seek(want.Cycle); err != nil {
					t.Fatal(err)
				}
				if got := m.Frame(); !reflect.DeepEqual(got, want) {
					t.Errorf("cycle %d:\n got %+v\nwant %+v", want.Cycle, got, want)
				}
			}
		})
	}
}

// TestSeekEqualsStep: seeking to cycle N gives the frame that N single steps give,
// from a fresh model and from a model that is past N (a backward seek).
func TestSeekEqualsStep(t *testing.T) {
	r := loadFixture(t)
	last := newModel(t, r).Cycles()
	stepper := NewPlayer(newModel(t, r))
	stepped := []Frame{stepper.Frame()}
	for i := 0; i < last; i++ {
		if err := stepper.Step(1); err != nil {
			t.Fatal(err)
		}
		stepped = append(stepped, stepper.Frame())
	}
	if len(stepped) != last+1 || last == 0 {
		t.Fatalf("%d stepped frames, want %d (last %d)", len(stepped), last+1, last)
	}
	for n := 0; n <= last; n++ {
		fresh := newModel(t, r)
		if err := fresh.Seek(n); err != nil {
			t.Fatal(err)
		}
		if got := fresh.Frame(); !reflect.DeepEqual(got, stepped[n]) {
			t.Errorf("fresh seek to %d: got %+v, want %+v", n, got, stepped[n])
		}
		back := newModel(t, r)
		if err := back.Seek(last); err != nil {
			t.Fatal(err)
		}
		if err := back.Seek(n); err != nil {
			t.Fatal(err)
		}
		if got := back.Frame(); !reflect.DeepEqual(got, stepped[n]) {
			t.Errorf("backward seek to %d: got %+v, want %+v", n, got, stepped[n])
		}
	}
}

// TestSeekClamps: a cycle before 0 or after the last one gives cycle 0 or the last.
func TestSeekClamps(t *testing.T) {
	m := newModel(t, loadFixture(t))
	for _, tt := range []struct{ seek, want int }{{-5, 0}, {7, 7}, {1000, 12}, {-1, 0}, {12, 12}} {
		if err := m.Seek(tt.seek); err != nil {
			t.Fatal(err)
		}
		if m.Cycle() != tt.want || m.Frame().Cycle != tt.want {
			t.Errorf("Seek(%d): Cycle() = %d, Frame().Cycle = %d, want %d", tt.seek, m.Cycle(), m.Frame().Cycle, tt.want)
		}
	}
}

// cellOf is a DAT.F #0, #0 cell, enough for a hand-built replay.
var cellOf = isa.Cell{Opcode: isa.OpDAT, Modifier: isa.ModF}

func write(bot uint32, pc, addr int32) vm.Event {
	return vm.Event{Kind: vm.CellWrite, Bot: bot, PC: pc, Address: addr, Cell: cellOf}
}

func spawn(bot uint32, pc, addr int32) vm.Event {
	return vm.Event{Kind: vm.ProcessSpawn, Bot: bot, PC: pc, Address: addr}
}

func death(bot uint32, pc int32) vm.Event {
	return vm.Event{Kind: vm.ProcessDeath, Bot: bot, PC: pc, Address: pc}
}

// TestMultiProcessExecuted: an x record names only the process that stepped, so
// after a spawn the frame counts two live processes but draws only the executed
// address of the cycle.
func TestMultiProcessExecuted(t *testing.T) {
	r := &replay.Replay{
		Config: arena.Config{CoreSize: 16},
		Bots:   []replay.Bot{{Start: 0, EntryAddress: 0, Cells: []isa.Cell{cellOf}}, {Start: 8, EntryAddress: 8, Cells: []isa.Cell{cellOf}}},
		Cycles: []replay.Cycle{
			{N: 1, Steps: []arena.Step{{Bot: 1, PC: 0, Events: []vm.Event{spawn(1, 0, 5)}}, {Bot: 2, PC: 8}}},
			{N: 2, Steps: []arena.Step{{Bot: 1, PC: 5}, {Bot: 2, PC: 9}}},
			{N: 3, Steps: []arena.Step{{Bot: 1, PC: 1, Events: []vm.Event{death(1, 1)}}, {Bot: 2, PC: 10}}},
		},
		Result: arena.Result{Cycles: 3},
	}
	m := newModel(t, r)
	want := []struct {
		live []int
		exec []Pointer
	}{
		{[]int{1, 1}, []Pointer{}},
		{[]int{2, 1}, []Pointer{{1, 0}, {2, 8}}},
		{[]int{2, 1}, []Pointer{{1, 5}, {2, 9}}},
		{[]int{1, 1}, []Pointer{{1, 1}, {2, 10}}},
	}
	for c, w := range want {
		if err := m.Seek(c); err != nil {
			t.Fatal(err)
		}
		f := m.Frame()
		if !reflect.DeepEqual(f.Live, w.live) || !reflect.DeepEqual(f.Executed, w.exec) {
			t.Errorf("cycle %d: live %v executed %v, want live %v executed %v", c, f.Live, f.Executed, w.live, w.exec)
		}
	}
}

// TestModelDegenerate feeds the model replays the reader accepts although the
// writer never produces them: overlapping bots, a bot longer than the core, an
// entry outside the bot, steps of an unknown bot, addresses out of range, a gap in
// the cycles, more deaths than processes, and a result longer than the recorded
// cycles. The model must not panic and must keep its shapes.
func TestModelDegenerate(t *testing.T) {
	long := make([]isa.Cell, 12) // longer than the core of 8
	for i := range long {
		long[i] = cellOf
	}
	r := &replay.Replay{
		Config: arena.Config{CoreSize: 8},
		Bots: []replay.Bot{
			{Start: 7, EntryAddress: 40, Cells: long},
			{Start: 6, EntryAddress: -3, Cells: []isa.Cell{cellOf, cellOf, cellOf, cellOf}},
		},
		Cycles: []replay.Cycle{
			{N: 1, Steps: []arena.Step{{Bot: 5, PC: 99}, {Bot: 1, PC: -1, Events: []vm.Event{write(1, -1, 100), death(1, -1), death(1, -1)}}}},
			{N: 3, Steps: []arena.Step{{Bot: 2, PC: 3}}},
		},
		Result: arena.Result{Cycles: 5},
	}
	m := newModel(t, r)
	if m.Cycles() != 5 {
		t.Fatalf("Cycles() = %d, want 5", m.Cycles())
	}
	for c := 0; c <= 5; c++ {
		if err := m.Seek(c); err != nil {
			t.Fatal(err)
		}
		f := m.Frame()
		if len(f.Owners) != 8 || len(f.Live) != 2 || f.Executed == nil || f.Cycle != c {
			t.Errorf("cycle %d: frame shape owners %d live %v executed %v cycle %d", c, len(f.Owners), f.Live, f.Executed, f.Cycle)
		}
		for _, n := range f.Live {
			if n < 0 {
				t.Errorf("cycle %d: negative live count in %v", c, f.Live)
			}
		}
	}
	// A cycle with no recorded steps has no executed addresses, not those of an earlier one.
	for _, c := range []int{2, 4, 5} {
		if err := m.Seek(c); err != nil {
			t.Fatal(err)
		}
		if got := m.Frame().Executed; len(got) != 0 {
			t.Errorf("cycle %d: executed %v, want none", c, got)
		}
	}
	if err := m.Seek(3); err != nil {
		t.Fatal(err)
	}
	if got, want := m.Frame().Executed, []Pointer{{2, 3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("cycle 3: executed %v, want %v", got, want)
	}
	// The unknown bot's step is kept as recorded; the renderer decides what to draw.
	if err := m.Seek(1); err != nil {
		t.Fatal(err)
	}
	if got, want := m.Frame().Executed, []Pointer{{5, 99}, {1, -1}}; !reflect.DeepEqual(got, want) {
		t.Errorf("cycle 1: executed %v, want %v", got, want)
	}
}

// TestNewModelRejectsBadCore: a core size the VM cannot build is an error.
func TestNewModelRejectsBadCore(t *testing.T) {
	if _, err := NewModel(&replay.Replay{Config: arena.Config{CoreSize: 0}}); err == nil {
		t.Error("NewModel with core size 0: want an error")
	}
}
