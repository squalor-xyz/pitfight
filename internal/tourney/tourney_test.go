// SPDX-License-Identifier: AGPL-3.0-or-later

package tourney

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// assemble turns source into a named bot.
func assemble(t *testing.T, name, src string) Bot {
	t.Helper()
	p, err := asm.Assemble(name, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return Bot{Name: name, Bot: arena.Bot{Cells: p.Cells, Entry: p.Entry}}
}

// starterBots assembles bots/*.s in name order.
func starterBots(t *testing.T) []Bot {
	t.Helper()
	var bots []Bot
	for _, name := range []string{"dwarf", "imp", "replicator", "scanner"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "bots", name+".s"))
		if err != nil {
			t.Fatal(err)
		}
		bots = append(bots, assemble(t, name, string(src)))
	}
	return bots
}

const (
	sitterSrc  = "loop: JMP.B loop, $0\n"
	suicideSrc = "DAT.F $0, $0\n"
)

func TestPairings(t *testing.T) {
	tests := []struct {
		n    int
		want []Pair
	}{
		{0, nil},
		{1, nil},
		{2, []Pair{{0, 1}}},
		{3, []Pair{{0, 1}, {0, 2}, {1, 2}}},
		{4, []Pair{{0, 1}, {0, 2}, {0, 3}, {1, 2}, {1, 3}, {2, 3}}},
		{5, []Pair{{0, 1}, {0, 2}, {0, 3}, {0, 4}, {1, 2}, {1, 3}, {1, 4}, {2, 3}, {2, 4}, {3, 4}}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			got := Pairings(tt.n)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Pairings(%d) = %v, want %v", tt.n, got, tt.want)
			}
			if want := tt.n * (tt.n - 1) / 2; tt.n >= 2 && len(got) != want {
				t.Errorf("len = %d, want %d", len(got), want)
			}
		})
	}
}

// TestMatches checks the match list: rounds in order, within a round every pair in
// pairing order, each pair first in the order (A, B) and then (B, A), indexes
// consecutive from 0 and every match of a round on the round's seed.
func TestMatches(t *testing.T) {
	got := Matches(3, 2)
	var want []Match
	for round := 1; round <= 2; round++ {
		for _, p := range Pairings(3) {
			for _, o := range [][2]int{{p.A, p.B}, {p.B, p.A}} {
				want = append(want, Match{Index: len(want), Round: round, Seed: Seed(round), First: o[0], Second: o[1]})
			}
		}
	}
	if len(want) != 12 || !slices.Equal(got, want) {
		t.Errorf("Matches(3, 2) =\n%v\nwant\n%v", got, want)
	}
	if m := Matches(1, 5); len(m) != 0 {
		t.Errorf("Matches(1, 5) = %v, want none", m)
	}
	if m := Matches(4, 0); len(m) != 0 {
		t.Errorf("Matches(4, 0) = %v, want none", m)
	}
}

// TestSeeds pins Seed(round) to the first output of the SplitMix64 generator of
// match.md section 4 seeded with the round number. The expected values come from an
// independent computation of that generator; those for rounds 0 and 1 are also in the
// spec's test vectors.
func TestSeeds(t *testing.T) {
	tests := []struct {
		round int
		want  uint64
	}{
		{0, 16294208416658607535},
		{1, 10451216379200822465},
		{2, 10905525725756348110},
		{3, 2092789425003139053},
		{100, 2532601429470541124},
	}
	for _, tt := range tests {
		if got := Seed(tt.round); got != tt.want {
			t.Errorf("Seed(%d) = %d, want %d", tt.round, got, tt.want)
		}
	}
	// Repeatable and distinct over a tournament-sized range.
	seen := make(map[uint64]int)
	for r := 1; r <= 1000; r++ {
		s := Seed(r)
		if s != Seed(r) {
			t.Fatalf("Seed(%d) is not repeatable", r)
		}
		if prev, dup := seen[s]; dup {
			t.Fatalf("Seed(%d) = Seed(%d) = %d", r, prev, s)
		}
		seen[s] = r
	}
}

// TestRunMatchesArena compares each result of Run with arena.Run on the same bots,
// seed and order.
func TestRunMatchesArena(t *testing.T) {
	// The starter bots, because their results depend on the placement seed.
	bots := starterBots(t)
	cfg := arena.DefaultConfig()
	cfg.CycleLimit = 5000
	results, err := Run(cfg, bots, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	matches := Matches(4, 3)
	if len(matches) != 36 || len(results) != len(matches) {
		t.Fatalf("%d matches, %d results, want 36 of each", len(matches), len(results))
	}
	for i, r := range results {
		if r.Match != matches[i] {
			t.Errorf("result %d is for match %+v, want %+v", i, r.Match, matches[i])
		}
		c := cfg
		c.Seed = r.Seed
		res, _, err := arena.Run(c, []arena.Bot{bots[r.First].Bot, bots[r.Second].Bot})
		if err != nil {
			t.Fatal(err)
		}
		wantWinner := -1
		if res.Outcome == arena.Win {
			wantWinner = []int{r.First, r.Second}[res.Winner-1]
		}
		if r.Cycles != res.Cycles || r.Winner != wantWinner {
			t.Errorf("match %d: cycles %d winner %d, arena.Run gives cycles %d winner %d", i, r.Cycles, r.Winner, res.Cycles, wantWinner)
		}
	}
}

// TestRunError checks that a configuration that cannot place the bots is an error
// and that the same error comes back whatever the worker count.
func TestRunError(t *testing.T) {
	bots := []Bot{assemble(t, "a", sitterSrc), assemble(t, "b", sitterSrc)}
	cfg := arena.DefaultConfig()
	cfg.CoreSize = 10
	for _, jobs := range []int{1, 4} {
		results, err := Run(cfg, bots, 3, jobs)
		if err == nil || results != nil {
			t.Fatalf("jobs %d: got %v, %v; want an error and no results", jobs, results, err)
		}
		if !strings.Contains(err.Error(), "placement impossible") {
			t.Errorf("jobs %d: error %q does not name the placement", jobs, err)
		}
	}
}

// TestRunErrorLowestIndex checks that the error reported is the one of the lowest
// failing match. Bots of 1, 2 and 50 cells in a core of 210 with the default
// separation of 100 place in pairs (1 + 2 + 200 <= 210) except those with the long
// bot. The first failing match is index 2, bot 0 against bot 2 (51 cells); the last
// failing match is a pair with 52 cells, so the two messages differ.
func TestRunErrorLowestIndex(t *testing.T) {
	bots := []Bot{
		assemble(t, "short", sitterSrc),
		assemble(t, "mid", "DAT.F $0, $0\nloop: JMP.B loop, $0\n"),
		assemble(t, "long", strings.Repeat("DAT.F $0, $0\n", 49)+"loop: JMP.B loop, $0\n"),
	}
	if len(bots[0].Bot.Cells) != 1 || len(bots[1].Bot.Cells) != 2 || len(bots[2].Bot.Cells) != 50 {
		t.Fatal("unexpected bot lengths")
	}
	cfg := arena.DefaultConfig()
	cfg.CoreSize = 210
	want := "placement impossible: 2 bots of total length 51 with min separation 100 need 251 cells, core size is 210"
	for _, jobs := range []int{1, 4} {
		results, err := Run(cfg, bots, 3, jobs)
		if results != nil || err == nil || err.Error() != want {
			t.Errorf("jobs %d: got %v, %v; want no results and error %q", jobs, results, err, want)
		}
	}
}

// threeBots are two bots that never die and one that dies at once. With the cycle
// limit 50 every a-b match is a tie after 50 cycles and every other match is won by
// the sitter in cycle 1 (the suicide steps once, in the same cycle).
func threeBots(t *testing.T) []Bot {
	return []Bot{assemble(t, "a_sitter", sitterSrc), assemble(t, "b_twin", sitterSrc), assemble(t, "c_suicide", suicideSrc)}
}

func TestScore(t *testing.T) {
	bots := threeBots(t)
	cfg := arena.DefaultConfig()
	cfg.CycleLimit = 50
	results, err := Run(cfg, bots, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	got := Score(bots, results)
	want := []Standing{
		{Name: "a_sitter", Points: 16, Played: 8, Wins: 4, Ties: 4, Losses: 0, Cycles: 204},
		{Name: "b_twin", Points: 16, Played: 8, Wins: 4, Ties: 4, Losses: 0, Cycles: 204},
		{Name: "c_suicide", Points: 0, Played: 8, Wins: 0, Ties: 0, Losses: 8, Cycles: 8},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Score =\n%+v\nwant\n%+v", got, want)
	}
}

// TestScoreRanking ranks by points, then wins, then name, using hand-made results.
func TestScoreRanking(t *testing.T) {
	res := func(first, second, winner int) MatchResult {
		return MatchResult{Match: Match{First: first, Second: second}, Cycles: 10, Winner: winner}
	}
	rank := func(names []string, results []MatchResult) []string {
		var bots []Bot
		for _, n := range names {
			bots = append(bots, Bot{Name: n})
		}
		var out []string
		for _, s := range Score(bots, results) {
			out = append(out, fmt.Sprintf("%s=%d", s.Name, s.Points))
		}
		return out
	}
	tests := []struct {
		name    string
		bots    []string
		results []MatchResult
		want    []string
	}{
		// More points beat an earlier name.
		{"points", []string{"z", "a"}, []MatchResult{res(0, 1, 0)}, []string{"z=3", "a=0"}},
		// b has 3 points from a win, a and c have 3 from three ties; b has the wins.
		{"wins", []string{"b", "a", "c"}, []MatchResult{res(0, 2, 0), res(1, 2, -1), res(1, 2, -1), res(2, 1, -1)}, []string{"b=3", "a=3", "c=3"}},
		// Equal points and wins: the name decides, not the position in the bot list.
		{"name", []string{"b", "a"}, []MatchResult{res(0, 1, -1)}, []string{"a=1", "b=1"}},
	}
	for _, tt := range tests {
		if got := rank(tt.bots, tt.results); !slices.Equal(got, tt.want) {
			t.Errorf("%s: ranking %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMeanCycles(t *testing.T) {
	tests := []struct {
		cycles int64
		played int
		want   float64
	}{
		{0, 0, 0},
		{10, 3, 3.33},
		{20, 3, 6.67},
		{204, 8, 25.5},
	}
	for _, tt := range tests {
		if got := (Standing{Cycles: tt.cycles, Played: tt.played}).MeanCycles(); got != tt.want {
			t.Errorf("MeanCycles(%d/%d) = %v, want %v", tt.cycles, tt.played, got, tt.want)
		}
	}
}

func TestFormatText(t *testing.T) {
	bots := threeBots(t)
	cfg := arena.DefaultConfig()
	cfg.CycleLimit = 50
	results, err := Run(cfg, bots, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := "rank  bot        points  played  wins  ties  losses  mean_cycles\n" +
		"   1  a_sitter       16       8     4     4       0        25.50\n" +
		"   2  b_twin         16       8     4     4       0        25.50\n" +
		"   3  c_suicide       0       8     0     0       8         1.00\n"
	if got := string(FormatText(Score(bots, results))); got != want {
		t.Errorf("text:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatJSON(t *testing.T) {
	bots := threeBots(t)
	cfg := arena.DefaultConfig()
	cfg.CycleLimit = 50
	results, err := Run(cfg, bots, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FormatJSON(Score(bots, results))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"rank":1,"bot":"a_sitter","points":16,"played":8,"wins":4,"ties":4,"losses":0,"cycles":204,"mean_cycles":25.5}` + "\n" +
		`{"rank":2,"bot":"b_twin","points":16,"played":8,"wins":4,"ties":4,"losses":0,"cycles":204,"mean_cycles":25.5}` + "\n" +
		`{"rank":3,"bot":"c_suicide","points":0,"played":8,"wins":0,"ties":0,"losses":8,"cycles":8,"mean_cycles":1}` + "\n"
	if string(got) != want {
		t.Errorf("json:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatMatches checks the match records for one round: seeds are decimal
// strings and a tie has a null winner.
func TestFormatMatches(t *testing.T) {
	bots := threeBots(t)
	cfg := arena.DefaultConfig()
	cfg.CycleLimit = 50
	results, err := Run(cfg, bots, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FormatMatches(bots, results)
	if err != nil {
		t.Fatal(err)
	}
	seed := "10451216379200822465" // Seed(1)
	rec := func(i int, first, second, outcome, winner string, cycles int) string {
		return fmt.Sprintf(`{"index":%d,"round":1,"seed":"%s","first":"%s","second":"%s","outcome":"%s","winner":%s,"cycles":%d}`+"\n",
			i, seed, first, second, outcome, winner, cycles)
	}
	want := rec(0, "a_sitter", "b_twin", "tie", "null", 50) +
		rec(1, "b_twin", "a_sitter", "tie", "null", 50) +
		rec(2, "a_sitter", "c_suicide", "win", `"a_sitter"`, 1) +
		rec(3, "c_suicide", "a_sitter", "win", `"a_sitter"`, 1) +
		rec(4, "b_twin", "c_suicide", "win", `"b_twin"`, 1) +
		rec(5, "c_suicide", "b_twin", "win", `"b_twin"`, 1)
	if string(got) != want {
		t.Errorf("matches:\n%s\nwant:\n%s", got, want)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(got), []byte("\n")) {
		if !json.Valid(line) {
			t.Errorf("not JSON: %s", line)
		}
	}
}

// scoreboardBytes renders everything the tournament prints for a set of results.
func scoreboardBytes(t *testing.T, bots []Bot, results []MatchResult) []byte {
	t.Helper()
	st := Score(bots, results)
	js, err := FormatJSON(st)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := FormatMatches(bots, results)
	if err != nil {
		t.Fatal(err)
	}
	return slices.Concat(FormatText(st), js, ms)
}

// TestScoreboardIndependentOfJobs runs the starter bots over 10 rounds with 1, 2 and
// 8 workers and then scores the results in reverse arrival order; every scoreboard
// is byte for byte the same.
func TestScoreboardIndependentOfJobs(t *testing.T) {
	bots := starterBots(t)
	cfg := arena.DefaultConfig()
	var base []MatchResult
	var want []byte
	for _, jobs := range []int{1, 2, 8} {
		results, err := Run(cfg, bots, 10, jobs)
		if err != nil {
			t.Fatal(err)
		}
		got := scoreboardBytes(t, bots, results)
		if jobs == 1 {
			base, want = results, got
			if len(results) != 120 {
				t.Fatalf("%d results, want 120", len(results))
			}
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("jobs %d: scoreboard differs from jobs 1:\n%s\nwant:\n%s", jobs, got, want)
		}
	}
	reversed := slices.Clone(base)
	slices.Reverse(reversed)
	if got := scoreboardBytes(t, bots, reversed); !bytes.Equal(got, want) {
		t.Errorf("reverse arrival: scoreboard differs:\n%s\nwant:\n%s", got, want)
	}
}

// totalAlloc returns the bytes allocated by f.
func totalAlloc(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestRunKeepsNoEvents: a tournament only scores the matches, so it must not collect
// the VM events of a match (S13 measured about 143 MB at the default workers). Two
// imps write a cell every cycle: 2 x 80000 events per tied match, a few MB if kept; a
// third bot that dies at once makes the other matches decisive. Every result (cycles,
// and the winner or the tie) is the one of the same match played with events
// (arena.Run), and the allocation is far below what keeping the events costs.
func TestRunKeepsNoEvents(t *testing.T) {
	imp := "MOV.I $0, $1\n"
	bots := []Bot{assemble(t, "imp1", imp), assemble(t, "imp2", imp), assemble(t, "suicide", suicideSrc)}
	cfg := arena.DefaultConfig() // cycle limit 80000
	var results []MatchResult
	got := totalAlloc(func() {
		var err error
		if results, err = Run(cfg, bots, 1, 1); err != nil {
			t.Fatal(err)
		}
	})

	// The same matches with events kept.
	var eventBytes uint64
	var wins, ties int
	for _, r := range results {
		c := cfg
		c.Seed = r.Seed
		res, events, err := arena.Run(c, []arena.Bot{bots[r.First].Bot, bots[r.Second].Bot})
		if err != nil {
			t.Fatal(err)
		}
		wantWinner := -1
		if res.Outcome == arena.Win {
			wantWinner = []int{r.First, r.Second}[res.Winner-1]
			wins++
		} else {
			ties++
		}
		if res.Cycles != r.Cycles || r.Winner != wantWinner {
			t.Errorf("match %+v: tournament cycles %d winner %d, arena.Run cycles %d winner %d",
				r.Match, r.Cycles, r.Winner, res.Cycles, wantWinner)
		}
		eventBytes += uint64(len(events)) * uint64(unsafe.Sizeof(vm.Event{}))
	}
	if wins == 0 || ties == 0 {
		t.Fatalf("%d wins and %d ties; the test needs both", wins, ties)
	}
	if eventBytes < 8<<20 {
		t.Fatalf("the test matches have %d bytes of events; too few for the check", eventBytes)
	}
	t.Logf("Run allocated %d bytes; the events of its matches are %d bytes", got, eventBytes)
	if got > eventBytes/4 {
		t.Errorf("Run allocated %d bytes for matches with %d bytes of events; it keeps events", got, eventBytes)
	}
}

// TestRunConcurrent proves that jobs matches run at the same time: the stand-in for a
// match waits until jobs of them have started, and fails after a timeout. With fewer
// workers than jobs the barrier never fills.
func TestRunConcurrent(t *testing.T) {
	const jobs = 4
	bots := []Bot{assemble(t, "a", sitterSrc), assemble(t, "b", sitterSrc), assemble(t, "c", sitterSrc)}
	var arrived atomic.Int32
	full := make(chan struct{})
	var timedOut atomic.Bool
	old := playMatch
	playMatch = func(_ arena.Config, _ []Bot, m Match) (MatchResult, error) {
		if arrived.Add(1) == jobs {
			close(full)
		}
		select {
		case <-full:
		case <-time.After(2 * time.Second):
			timedOut.Store(true)
		}
		return MatchResult{Match: m, Cycles: 1, Winner: -1}, nil
	}
	t.Cleanup(func() { playMatch = old })

	if m := Matches(len(bots), 2); len(m) < jobs {
		t.Fatalf("%d matches, need at least %d", len(m), jobs)
	}
	results, err := Run(arena.DefaultConfig(), bots, 2, jobs)
	if err != nil || len(results) != len(Matches(len(bots), 2)) {
		t.Fatalf("%d results, error %v", len(results), err)
	}
	if timedOut.Load() {
		t.Errorf("fewer than %d matches ran at the same time", jobs)
	}
}
