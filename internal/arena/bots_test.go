// SPDX-License-Identifier: AGPL-3.0-or-later

package arena

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/isa"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// starterBots lists the starter bots of bots/, in the order the golden table uses.
var starterBots = []string{"imp", "dwarf", "replicator", "scanner"}

// starterProgram assembles bots/<name>.s.
func starterProgram(t *testing.T, name string) asm.Program {
	t.Helper()
	path := filepath.Join("..", "..", "bots", name+".s")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := asm.Assemble(path, src)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// starterBot assembles bots/<name>.s into a bot.
func starterBot(t *testing.T, name string) Bot {
	t.Helper()
	p := starterProgram(t, name)
	return Bot{Cells: p.Cells, Entry: p.Entry}
}

// loaded returns cell as the match loads it into a core of n cells: operand values
// reduced by Norm and the writer set to the bot id.
func loaded(c isa.Cell, n int, id uint32) isa.Cell {
	norm := func(v int32) int32 { return int32((int64(v)%int64(n) + int64(n)) % int64(n)) }
	c.AValue, c.BValue, c.Writer = norm(c.AValue), norm(c.BValue), id
	return c
}

// writesBy returns the cell-write events of bot id executed at address pc.
func writesBy(events []vm.Event, id uint32, pc int32) []vm.Event {
	var out []vm.Event
	for _, e := range events {
		if e.Kind == vm.CellWrite && e.Bot == id && e.PC == pc {
			out = append(out, e)
		}
	}
	return out
}

// TestStarterBotsAssemble checks that bots/ holds exactly the four starter bots and
// that each assembles and round-trips through the disassembler.
func TestStarterBotsAssemble(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "bots", "*.s"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	slices.Sort(names)
	want := []string{"dwarf.s", "imp.s", "replicator.s", "scanner.s"}
	if !slices.Equal(names, want) {
		t.Fatalf("bots/*.s = %v, want %v", names, want)
	}
	for _, name := range starterBots {
		t.Run(name, func(t *testing.T) {
			p := starterProgram(t, name)
			again, err := asm.Assemble(name+".listing", asm.Disassemble(p))
			if err != nil {
				t.Fatalf("listing does not assemble: %v", err)
			}
			if !slices.Equal(again.Cells, p.Cells) || again.Entry != p.Entry {
				t.Errorf("round trip changed the program:\n got %v entry %d\nwant %v entry %d",
					again.Cells, again.Entry, p.Cells, p.Entry)
			}
		})
	}
}

// TestImp runs the imp beside a sitter that is too far away to matter in 80
// cycles (the nearest other bot starts at least MINSEPARATION = 100 cells ahead).
// The imp survives to the cycle limit and every step copies the imp onto the next
// cell.
func TestImp(t *testing.T) {
	const limit = 80
	cfg := DefaultConfig()
	cfg.Seed, cfg.CycleLimit = 1, limit
	imp := starterBot(t, "imp")
	res, events, err := Run(cfg, []Bot{imp, botFile(t, "sitter.s")})
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, wantResult{limit, []uint32{1, 2}, Tie, 0, []uint32{1, 2}})
	if len(events) != limit {
		t.Fatalf("%d events, want %d (one write per step, no deaths)", len(events), limit)
	}
	start := int32(res.Placement.Starts[0])
	want := loaded(imp.Cells[0], cfg.CoreSize, 1)
	for i, e := range events {
		wantEv := vm.Event{Kind: vm.CellWrite, Bot: 1, PC: start + int32(i), Address: start + int32(i) + 1, Cell: want}
		if e != wantEv {
			t.Fatalf("event %d = %+v, want %+v", i, e, wantEv)
		}
	}
}

// TestDwarf checks the dwarf's fixed stride. The dwarf's MOV (index 1) writes one
// bomb per loop of 3 cycles, 4 cells further each time, starting 4 cells past its
// pointer cell (index 3). 60 cycles are 20 bombs, which end at index 83, before the
// nearest other bot (index 105 at the earliest).
func TestDwarf(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Seed, cfg.CycleLimit = 1, 60
	dwarf := starterBot(t, "dwarf")
	res, events, err := Run(cfg, []Bot{dwarf, botFile(t, "sitter.s")})
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, wantResult{60, []uint32{1, 2}, Tie, 0, []uint32{1, 2}})
	start := int32(res.Placement.Starts[0])
	bombs := writesBy(events, 1, start+1)
	if len(bombs) != 20 {
		t.Fatalf("%d bombs, want 20", len(bombs))
	}
	bomb := isa.Cell{Opcode: isa.OpDAT, Modifier: isa.ModF, AMode: isa.ModeImmediate, BMode: isa.ModeImmediate, Writer: 1}
	for k, e := range bombs {
		wantAddr := start + 3 + 4*int32(k+1)
		if e.Address != wantAddr || e.Cell != bomb {
			t.Errorf("bomb %d: address %d cell %+v, want address %d cell %+v", k, e.Address, e.Cell, wantAddr, bomb)
		}
	}
	// The pointer in the B field of cell 3 is stepped by 4 before each bomb.
	steps := writesBy(events, 1, start)
	if len(steps) != 20 {
		t.Fatalf("%d pointer steps, want 20", len(steps))
	}
	for k, e := range steps {
		if e.Address != start+3 || e.Cell.BValue != 4*int32(k+1) {
			t.Errorf("pointer step %d: address %d B %d, want address %d B %d", k, e.Address, e.Cell.BValue, start+3, 4*(k+1))
		}
	}
}

// TestReplicator checks that the replicator copies its eight code cells (indexes 0
// to 7) 100 cells ahead, splits to the copy, and that the original loops back to
// copy again 200 cells ahead, so that the number of processes grows.
func TestReplicator(t *testing.T) {
	const limit = 100
	cfg := DefaultConfig()
	cfg.Seed, cfg.CycleLimit = 1, limit
	rep := starterBot(t, "replicator")
	res, events, err := Run(cfg, []Bot{rep, botFile(t, "sitter.s")})
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, wantResult{limit, []uint32{1, 2}, Tie, 0, []uint32{1, 2}})
	start := int32(res.Placement.Starts[0])

	// The copies are the writes of the MOV.I at index 2 of the original, except
	// for its post-increments, which write the pointer cell (index 8).
	var copies []vm.Event
	for _, e := range writesBy(events, 1, start+2) {
		if e.Address != start+8 {
			copies = append(copies, e)
		}
	}
	if len(copies) < 16 {
		t.Fatalf("%d copy writes by the original, want at least 16", len(copies))
	}
	// The first pass copies to 100 cells ahead, the second to 200.
	for pass, base := range []int32{100, 200} {
		for j := int32(0); j < 8; j++ {
			want := loaded(rep.Cells[j], cfg.CoreSize, 1)
			if e := copies[pass*8+int(j)]; e.Address != start+base+j || e.Cell != want {
				t.Errorf("pass %d copy %d: address %d cell %+v, want address %d cell %+v",
					pass, j, e.Address, e.Cell, start+base+j, want)
			}
		}
	}

	// Spawns: the original at index 5 (twice, so it did loop back) and the first
	// copy at its own index 5, which is 100 cells on.
	type spawn struct{ pc, addr int32 }
	var spawns []spawn
	for _, e := range events {
		if e.Kind == vm.ProcessSpawn && e.Bot == 1 {
			spawns = append(spawns, spawn{e.PC, e.Address})
		}
	}
	want := []spawn{{start + 5, start + 100}, {start + 5, start + 200}, {start + 105, start + 200}}
	for _, w := range want {
		if !slices.Contains(spawns, w) {
			t.Errorf("spawns = %v, missing %v", spawns, w)
		}
	}
	if len(spawns) == 0 || spawns[0] != want[0] {
		t.Errorf("spawns = %v, want the first to be %v", spawns, want[0])
	}
	for _, e := range events {
		if e.Kind == vm.ProcessDeath && e.Bot == 1 {
			t.Errorf("replicator process died: %+v", e)
		}
	}
}

// TestScanner places the scanner against a four-cell target at the offset the seed
// gives, predicts from that offset which cell the scanner hits first, and checks
// the first bomb, the kill and the cycle in which it happens.
//
// The scanner (cell 7 is its pointer) examines cell 7 + 3j in its j-th pass of 3
// cycles. It bombs the first examined cell that is not empty, in the third cycle of
// that pass (an empty cell makes SEQ skip the bomb and the pass takes 3 cycles). The target (NOP, NOP, NOP, JMP back) then dies within 4 cycles.
func TestScanner(t *testing.T) {
	const target = "NOP.F $0, $0\nNOP.F $0, $0\nNOP.F $0, $0\nJMP.B $-3, $0"
	scanner := starterBot(t, "scanner")
	for _, seed := range []uint64{1, 2, 3, 4, 5} {
		cfg := DefaultConfig()
		cfg.Seed = seed
		res, events, err := Run(cfg, []Bot{scanner, botFrom(t, target)})
		if err != nil {
			t.Fatal(err)
		}
		start, enemy := res.Placement.Starts[0], res.Placement.Starts[1]
		n := cfg.CoreSize
		ptr := start + 7
		j := 1
		for ; j <= 2600; j++ {
			if off := ((ptr+3*j-enemy)%n + n) % n; off < 4 {
				break
			}
		}
		if j > 2600 {
			t.Fatalf("seed %d: the target is not on the scanner's lap (start %d, enemy %d)", seed, start, enemy)
		}
		wantAddr := int32((ptr + 3*j) % n)

		bombs := writesBy(events, 1, int32(start)+2)
		if len(bombs) == 0 {
			t.Fatalf("seed %d: the scanner never bombed", seed)
		}
		if e := bombs[0]; e.Address != wantAddr || e.Cell != loaded(scanner.Cells[9], n, 1) {
			t.Errorf("seed %d: first bomb at %d cell %+v, want address %d cell %+v",
				seed, e.Address, e.Cell, wantAddr, loaded(scanner.Cells[9], n, 1))
		}
		if res.Outcome != Win || res.Winner != 1 {
			t.Errorf("seed %d: result %v winner %d, want a win for bot 1", seed, res.Outcome, res.Winner)
		}
		if res.Cycles < 3*j || res.Cycles > 3*j+4 {
			t.Errorf("seed %d: ended in cycle %d, want %d..%d", seed, res.Cycles, 3*j, 3*j+4)
		}
	}
}
