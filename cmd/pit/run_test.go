// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/replay"
)

const (
	sitterSrc  = "loop: JMP.B loop, $0\n"
	suicideSrc = "DAT.F $0, $0\n"
)

// writeBots writes the sources into one temporary directory and returns the paths.
func writeBots(t *testing.T, files map[string]string, order ...string) []string {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for _, name := range order {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(files[name]), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

func runPit(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// TestRunCommand runs sitter against suicide with seed 1. The start addresses
// 4519 and 7975 were computed outside the Go code from the placement algorithm of
// docs/spec/match.md section 6.
func TestRunCommand(t *testing.T) {
	paths := writeBots(t, map[string]string{"sitter.s": sitterSrc, "suicide.s": suicideSrc}, "sitter.s", "suicide.s")
	args := []string{"run", paths[0], paths[1], "--seed", "1"}

	code, out, errOut := runPit(args...)
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q; want 0 and empty", code, errOut)
	}
	want := "match: 2 bots, seed 1, core size 8000, cycle limit 80000, max processes 8000, min separation 100\n" +
		"bot 1: " + paths[0] + " (length 1, start 4519, entry_address 4519)\n" +
		"bot 2: " + paths[1] + " (length 1, start 7975, entry_address 7975)\n" +
		"cycles: 1\n" +
		"survivors: 1\n" +
		"result: win bot 1\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}

	// The same command twice prints byte-identical output.
	code2, out2, errOut2 := runPit(args...)
	if code2 != 0 || errOut2 != "" || out2 != out {
		t.Errorf("second run differs: code %d, stderr %q, stdout %q", code2, errOut2, out2)
	}
}

func TestRunTie(t *testing.T) {
	paths := writeBots(t, map[string]string{"a.s": sitterSrc, "b.s": sitterSrc}, "a.s", "b.s")
	code, out, errOut := runPit("run", "--cycle-limit", "7", paths[0], paths[1])
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"seed 0,", "cycle limit 7,", "cycles: 7\n", "survivors: 1 2\n", "result: tie between bots 1, 2\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	paths = writeBots(t, map[string]string{"a.s": suicideSrc, "b.s": suicideSrc}, "a.s", "b.s")
	_, out, _ = runPit("run", paths[0], paths[1])
	for _, want := range []string{"survivors: none\n", "result: tie between bots 1, 2\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
}

func TestRunJSON(t *testing.T) {
	paths := writeBots(t, map[string]string{"sitter.s": sitterSrc, "suicide.s": suicideSrc}, "sitter.s", "suicide.s")
	code, out, errOut := runPit("run", "--json", "--seed", "1", paths[0], paths[1])
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	want := `{"config":{"core_size":8000,"cycle_limit":80000,"max_processes":8000,"min_separation":100,"seed":"1"},` +
		`"bots":[{"id":1,"file":` + jsonString(paths[0]) + `,"length":1,"start":4519,"entry_address":4519},` +
		`{"id":2,"file":` + jsonString(paths[1]) + `,"length":1,"start":7975,"entry_address":7975}],` +
		`"cycles":1,"survivors":[1],"outcome":"win","winner":1,"tied":[]}` + "\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	// A tie has winner 0 and no survivors is an empty list, not null.
	paths = writeBots(t, map[string]string{"a.s": suicideSrc, "b.s": suicideSrc}, "a.s", "b.s")
	_, out, _ = runPit("run", "--json", paths[0], paths[1])
	var got struct {
		Survivors []int  `json:"survivors"`
		Outcome   string `json:"outcome"`
		Winner    int    `json:"winner"`
		Tied      []int  `json:"tied"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "tie" || got.Winner != 0 || len(got.Tied) != 2 || len(got.Survivors) != 0 || !strings.Contains(out, `"survivors":[]`) {
		t.Errorf("unexpected result %+v in %s", got, out)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestRunFlagsAfterFiles checks that flags and files may be given in any order and
// that a seed above 2^63 is accepted.
func TestRunFlagsAfterFiles(t *testing.T) {
	paths := writeBots(t, map[string]string{"a.s": sitterSrc, "b.s": suicideSrc}, "a.s", "b.s")
	_, a, errA := runPit("run", paths[0], "--seed", "18446744073709551615", paths[1])
	_, b, errB := runPit("run", "--seed", "18446744073709551615", paths[0], paths[1])
	if errA != "" || errB != "" || a != b || !strings.Contains(a, "seed 18446744073709551615,") {
		t.Errorf("flag order changed the output or failed:\n%s%s\nstderr %q %q", a, b, errA, errB)
	}
	// Seed 2^64-1 puts bot 2 first around the circle (placement P5's seed, K=2).
	if !strings.Contains(a, "start 6508") || !strings.Contains(a, "start 969") {
		t.Errorf("unexpected placement:\n%s", a)
	}
}

func TestRunBadBot(t *testing.T) {
	src := "FOO $1, $2\n"
	paths := writeBots(t, map[string]string{"good.s": sitterSrc, "bad.s": src}, "good.s", "bad.s")
	_, wantErr := asm.Assemble(paths[1], []byte(src))
	if wantErr == nil {
		t.Fatal("test source assembles")
	}
	code, out, errOut := runPit("run", paths[0], paths[1], "--seed", "1")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if want := wantErr.Error() + "\n"; errOut != want || !strings.Contains(errOut, "bad.s:1:1:") {
		t.Errorf("stderr = %q, want %q", errOut, want)
	}
}

func TestRunErrors(t *testing.T) {
	paths := writeBots(t, map[string]string{"a.s": sitterSrc, "b.s": sitterSrc}, "a.s", "b.s")
	a, b := paths[0], paths[1]
	missing := filepath.Join(filepath.Dir(a), "missing.s")
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string // substring of stderr
	}{
		{"no bots", []string{"run"}, 2, "usage: pit run"},
		{"unknown flag", []string{"run", "--nope", a, b}, 2, "usage: pit run"},
		{"bad seed", []string{"run", "--seed", "-1", a, b}, 2, "usage: pit run"},
		{"seed too large", []string{"run", "--seed", "18446744073709551616", a, b}, 2, "usage: pit run"},
		{"one bot", []string{"run", a}, 1, "pit run: invalid bots: need 2..1048576 bots, got 1"},
		{"missing file", []string{"run", a, missing}, 1, "missing.s"},
		{"core size", []string{"run", "--core-size", "0", a, b}, 1, "pit run: invalid config: CORESIZE 0 out of range 1..1048576"},
		{"cycle limit", []string{"run", "--cycle-limit", "0", a, b}, 1, "pit run: invalid config: CYCLELIMIT 0 out of range 1..2147483647"},
		{"max processes", []string{"run", "--max-processes", "0", a, b}, 1, "pit run: invalid config: MAXPROCESSES 0 out of range 1..1048576"},
		{"min separation", []string{"run", "--min-separation", "-1", a, b}, 1, "pit run: invalid config: MINSEPARATION -1 out of range 0..1048576"},
		{"placement", []string{"run", "--core-size", "8", a, b}, 1, "pit run: placement impossible: 2 bots of total length 2 with min separation 100 need 202 cells, core size is 8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := runPit(tt.args...)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", errOut, tt.wantErr)
			}
		})
	}
}

// TestRunEntry checks the printed entry_address: start plus the ORG offset, wrapped
// around the core. Seed 1 in a core of 20 cells with no separation puts the
// three-cell bot at 19..1 (computed outside the Go code from match.md section 6).
func TestRunEntry(t *testing.T) {
	paths := writeBots(t, map[string]string{
		"a.s": "DAT.F $0, $0\nDAT.F $0, $0\ngo: JMP.B $0, $0\nORG go\n",
		"b.s": sitterSrc,
	}, "a.s", "b.s")
	code, out, errOut := runPit("run", "--seed", "1", "--core-size", "20", "--min-separation", "0", "--cycle-limit", "5", paths[0], paths[1])
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"(length 3, start 19, entry_address 1)\n", "(length 1, start 2, entry_address 2)\n", "cycles: 5\n", "survivors: 1 2\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
}

func TestRunHelp(t *testing.T) {
	code, out, errOut := runPit("run", "--help")
	if code != 0 || errOut != "" || !strings.Contains(out, "usage: pit run") || !strings.Contains(out, "--seed N") {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// TestRunDoubleDash checks that every argument after "--" is a bot file, even one
// that looks like a flag.
func TestRunDoubleDash(t *testing.T) {
	paths := writeBots(t, map[string]string{"a.s": sitterSrc, "b.s": suicideSrc}, "a.s", "b.s")

	// A flag-like operand is reported as a missing file, not parsed as a flag.
	code, out, errOut := runPit("run", paths[0], "--", "--seed", paths[1])
	if code != 1 || out != "" || !strings.Contains(errOut, "pit run: open --seed:") {
		t.Errorf("exit code %d, stdout %q, stderr %q; want 1, empty and an error opening --seed", code, out, errOut)
	}
	code, out, errOut = runPit("run", "--", paths[0], "--json", paths[1])
	if code != 1 || out != "" || !strings.Contains(errOut, "pit run: open --json:") {
		t.Errorf("exit code %d, stderr %q; want 1 and an error opening --json", code, errOut)
	}

	// Flags before "--" still apply and the files after it run.
	code, out, errOut = runPit("run", "--seed", "1", "--", paths[0], paths[1])
	if code != 0 || errOut != "" || !strings.Contains(out, "seed 1,") || !strings.Contains(out, "bot 2: "+paths[1]) {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, out, errOut)
	}

	if !strings.Contains(runUsage, "[--]") {
		t.Error("the usage text does not mention --")
	}
}

// replayTestBots writes two bots that run for a while: an imp, which writes a cell
// every cycle, and a sitter.
func replayTestBots(t *testing.T) []string {
	t.Helper()
	return writeBots(t, map[string]string{"imp.s": "MOV.I $0, $1\n", "sitter.s": sitterSrc}, "imp.s", "sitter.s")
}

// TestRunReplay: --replay FILE writes the replay of the match, the output gives its
// hash, and the file is the one internal/replay writes for the same match.
func TestRunReplay(t *testing.T) {
	paths := replayTestBots(t)
	file := filepath.Join(t.TempDir(), "match.jsonl")
	code, out, errOut := runPit("run", paths[0], paths[1], "--seed", "1", "--cycle-limit", "30", "--replay", file)
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	r, err := replay.Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the replay file does not parse: %v", err)
	}
	if r.Config.Seed != 1 || r.Config.CycleLimit != 30 || len(r.Cycles) != 30 || r.Result.Cycles != 30 {
		t.Errorf("config %+v, %d cycles with events, result %+v", r.Config, len(r.Cycles), r.Result)
	}
	for _, c := range r.Cycles {
		if len(c.Steps) != 2 || c.Steps[0].Bot != 1 || c.Steps[1].Bot != 2 {
			t.Fatalf("cycle %d steps %+v, want one step for each bot", c.N, c.Steps)
		}
	}
	// The replay is the replay of the same bots through the library.
	var bots []arena.Bot
	for _, p := range paths {
		src, _ := os.ReadFile(p)
		prog, err := asm.Assemble(p, src)
		if err != nil {
			t.Fatal(err)
		}
		bots = append(bots, arena.Bot{Cells: prog.Cells, Entry: prog.Entry})
	}
	want, err := replay.Record(r.Config, bots)
	if err != nil || !bytes.Equal(want.Bytes(), data) {
		t.Errorf("the file differs from replay.Record (err %v)", err)
	}
	hash := replay.Hash(data)
	if !strings.HasSuffix(out, "replay: "+hash+"\n") {
		t.Errorf("stdout does not end with the replay hash %s:\n%s", hash, out)
	}

	// The same command into a second file: identical bytes, identical output.
	file2 := filepath.Join(t.TempDir(), "again.jsonl")
	code, out2, _ := runPit("run", paths[0], paths[1], "--seed", "1", "--cycle-limit", "30", "--replay", file2)
	data2, err := os.ReadFile(file2)
	if err != nil || code != 0 || !bytes.Equal(data, data2) || out != out2 {
		t.Errorf("second run: code %d, err %v, files equal %v, outputs equal %v", code, err, bytes.Equal(data, data2), out == out2)
	}

	// Without the flag nothing is printed about a replay.
	_, plain, _ := runPit("run", paths[0], paths[1], "--seed", "1", "--cycle-limit", "30")
	if strings.Contains(plain, "replay") || !strings.HasPrefix(out, plain) {
		t.Errorf("output without --replay:\n%s\nwith it:\n%s", plain, out)
	}
}

// TestRunReplayJSON: with --json the hash is the last field, replay_hash.
func TestRunReplayJSON(t *testing.T) {
	paths := replayTestBots(t)
	file := filepath.Join(t.TempDir(), "match.jsonl")
	code, out, errOut := runPit("run", "--json", "--replay", file, "--seed", "18446744073709551615", "--cycle-limit", "10", paths[0], paths[1])
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"seed":"18446744073709551615"`) || !bytes.Contains(data, []byte(`"seed":"18446744073709551615"`)) {
		t.Errorf("seed is not a string:\n%s\n%s", out, data[:200])
	}
	if want := `,"tied":[1,2],"replay_hash":"` + replay.Hash(data) + `"}` + "\n"; !strings.HasSuffix(out, want) {
		t.Errorf("stdout does not end with %s:\n%s", want, out)
	}
	// Without the flag the field is absent.
	_, plain, _ := runPit("run", "--json", "--cycle-limit", "10", paths[0], paths[1])
	if strings.Contains(plain, "replay") {
		t.Errorf("replay_hash without --replay: %s", plain)
	}
}

func TestRunReplayErrors(t *testing.T) {
	paths := replayTestBots(t)
	// A directory that does not exist: the match ran, but the replay cannot be written.
	bad := filepath.Join(t.TempDir(), "missing", "match.jsonl")
	code, out, errOut := runPit("run", "--replay", bad, paths[0], paths[1])
	if code != 1 || out != "" || !strings.Contains(errOut, "pit run:") || !strings.Contains(errOut, "missing") {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, out, errOut)
	}
	// The flag needs a value.
	code, out, errOut = runPit("run", paths[0], paths[1], "--replay")
	if code != 2 || out != "" || !strings.Contains(errOut, "usage: pit run") {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, out, errOut)
	}
	// A refused match writes no file.
	file := filepath.Join(t.TempDir(), "match.jsonl")
	code, _, _ = runPit("run", "--core-size", "0", "--replay", file, paths[0], paths[1])
	if _, err := os.Stat(file); code != 1 || err == nil {
		t.Errorf("exit code %d, stat error %v; want 1 and no file", code, err)
	}
	if !strings.Contains(runUsage, "--replay FILE") {
		t.Error("the usage text does not list --replay")
	}
}
