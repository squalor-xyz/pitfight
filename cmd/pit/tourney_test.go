// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tourneyPit runs pit with args and returns the exit code, stdout and stderr.
func tourneyPit(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// botDir writes the sources into a new temporary directory, in the order given, and
// returns the directory.
func botDir(t *testing.T, files map[string]string, order ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range order {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(files[name]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// starterDir copies the starter bots into a temporary directory, last name first.
func starterDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"scanner", "replicator", "imp", "dwarf"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "bots", name+".s"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".s"), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestTourneyCommand runs the starter bots twice and with different worker counts;
// the output is the same each time and ranks all four bots.
func TestTourneyCommand(t *testing.T) {
	dir := starterDir(t)
	// Files that are not bots are ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a bot"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.s"), 0o755); err != nil {
		t.Fatal(err)
	}

	code, first, errOut := tourneyPit("tourney", dir, "--rounds", "3", "--jobs", "2")
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q; want 0 and empty", code, errOut)
	}
	for _, args := range [][]string{
		{"tourney", dir, "--rounds", "3", "--jobs", "2"},
		{"tourney", dir, "--rounds", "3", "--jobs", "1"},
		{"tourney", "--rounds", "3", "--jobs", "8", dir},
		{"tourney", "--rounds=3", dir},
	} {
		code, again, errOut := tourneyPit(args...)
		if code != 0 || errOut != "" || again != first {
			t.Errorf("%v: exit code %d, stderr %q, stdout differs:\n%s\nwant:\n%s", args, code, errOut, again, first)
		}
	}

	lines := strings.Split(strings.TrimSuffix(first, "\n"), "\n")
	if len(lines) != 2+1+4 {
		t.Fatalf("%d lines, want 7:\n%s", len(lines), first)
	}
	wantHead := "tourney: 4 bots, 3 rounds, 36 matches, core size 8000, cycle limit 80000, max processes 8000, min separation 100"
	if lines[0] != wantHead || lines[1] != "scoring: win 3, tie 1, loss 0" {
		t.Errorf("header %q, %q", lines[0], lines[1])
	}
	if !strings.HasPrefix(lines[2], "rank  bot") {
		t.Errorf("table header %q", lines[2])
	}
	seen := make(map[string]bool)
	for _, l := range lines[3:] {
		f := strings.Fields(l)
		if len(f) != 8 || f[3] != "18" { // each bot plays 3 other bots x 3 rounds x 2 orders
			t.Errorf("row %q", l)
			continue
		}
		seen[f[1]] = true
	}
	for _, n := range []string{"dwarf", "imp", "replicator", "scanner"} {
		if !seen[n] {
			t.Errorf("no row for %s in:\n%s", n, first)
		}
	}
}

// TestTourneyOutput checks the exact output for three tiny bots, computed by hand:
// a sitter and its twin tie after the cycle limit, and each beats the suicide bot in
// cycle 1, in both orders in each of 2 rounds.
func TestTourneyOutput(t *testing.T) {
	files := map[string]string{
		"c_suicide.s": "DAT.F $0, $0\n",
		"a_sitter.s":  "loop: JMP.B loop, $0\n",
		"b_twin.s":    "loop: JMP.B loop, $0\n",
	}
	dir := botDir(t, files, "c_suicide.s", "a_sitter.s", "b_twin.s")
	args := []string{"tourney", dir, "--rounds", "2", "--cycle-limit", "50"}

	code, out, errOut := tourneyPit(args...)
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	want := "tourney: 3 bots, 2 rounds, 12 matches, core size 8000, cycle limit 50, max processes 8000, min separation 100\n" +
		"scoring: win 3, tie 1, loss 0\n" +
		"rank  bot        points  played  wins  ties  losses  mean_cycles\n" +
		"   1  a_sitter       16       8     4     4       0        25.50\n" +
		"   2  b_twin         16       8     4     4       0        25.50\n" +
		"   3  c_suicide       0       8     0     0       8         1.00\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}

	// --json prints only the scoreboard, one line per bot.
	code, out, errOut = tourneyPit(append(args, "--json")...)
	wantJSON := `{"rank":1,"bot":"a_sitter","points":16,"played":8,"wins":4,"ties":4,"losses":0,"cycles":204,"mean_cycles":25.5}` + "\n" +
		`{"rank":2,"bot":"b_twin","points":16,"played":8,"wins":4,"ties":4,"losses":0,"cycles":204,"mean_cycles":25.5}` + "\n" +
		`{"rank":3,"bot":"c_suicide","points":0,"played":8,"wins":0,"ties":0,"losses":8,"cycles":8,"mean_cycles":1}` + "\n"
	if code != 0 || errOut != "" || out != wantJSON {
		t.Errorf("--json: exit code %d, stderr %q, stdout:\n%s\nwant:\n%s", code, errOut, out, wantJSON)
	}

	// --matches writes one record per match to a file; stdout does not change.
	matches := filepath.Join(t.TempDir(), "matches.jsonl")
	code, out2, errOut := tourneyPit(append(args, "--matches", matches)...)
	if code != 0 || errOut != "" || out2 != want {
		t.Errorf("--matches: exit code %d, stderr %q, stdout:\n%s", code, errOut, out2)
	}
	data, err := os.ReadFile(matches)
	if err != nil {
		t.Fatal(err)
	}
	recs := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(recs) != 12 {
		t.Fatalf("%d match records, want 12:\n%s", len(recs), data)
	}
	wantFirst := `{"index":0,"round":1,"seed":"10451216379200822465","first":"a_sitter","second":"b_twin","outcome":"tie","winner":null,"cycles":50}`
	wantLast := `{"index":11,"round":2,"seed":"10905525725756348110","first":"c_suicide","second":"b_twin","outcome":"win","winner":"b_twin","cycles":1}`
	if recs[0] != wantFirst || recs[11] != wantLast {
		t.Errorf("first record %q, last %q", recs[0], recs[11])
	}
}

// TestTourneyDefaults checks the default of 100 rounds.
func TestTourneyDefaults(t *testing.T) {
	dir := botDir(t, map[string]string{"a.s": "loop: JMP.B loop, $0\n", "b.s": "DAT.F $0, $0\n"}, "a.s", "b.s")
	code, out, errOut := tourneyPit("tourney", dir, "--cycle-limit", "5")
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if want := "tourney: 2 bots, 100 rounds, 200 matches, "; !strings.HasPrefix(out, want) {
		t.Errorf("stdout %q does not start with %q", out, want)
	}
}

func TestTourneyErrors(t *testing.T) {
	good := map[string]string{"a.s": "loop: JMP.B loop, $0\n", "b.s": "DAT.F $0, $0\n"}
	two := botDir(t, good, "a.s", "b.s")
	one := botDir(t, good, "a.s")
	empty := botDir(t, good)
	bad := botDir(t, map[string]string{"a.s": "loop: JMP.B loop, $0\n", "b.s": "FOO\n"}, "a.s", "b.s")

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string // substring of stderr
		wantOut  string // substring of stdout
	}{
		{"no directory", []string{"tourney"}, 2, "usage: pit tourney", ""},
		{"two directories", []string{"tourney", two, two}, 2, "usage: pit tourney", ""},
		{"unknown flag", []string{"tourney", two, "--bogus"}, 2, "usage: pit tourney", ""},
		{"rounds zero", []string{"tourney", two, "--rounds", "0"}, 2, "--rounds", ""},
		{"jobs zero", []string{"tourney", two, "--jobs", "0"}, 2, "--jobs", ""},
		{"help", []string{"tourney", "--help"}, 0, "", "usage: pit tourney"},
		{"missing directory", []string{"tourney", filepath.Join(two, "nope")}, 1, "pit tourney:", ""},
		{"a file, not a directory", []string{"tourney", filepath.Join(two, "a.s")}, 1, "pit tourney:", ""},
		{"no bots", []string{"tourney", empty}, 1, "pit tourney: need at least 2 bots", ""},
		{"one bot", []string{"tourney", one}, 1, "pit tourney: need at least 2 bots", ""},
		{"assembler error", []string{"tourney", bad}, 1, filepath.Join(bad, "b.s") + ":1:1:", ""},
		{"placement impossible", []string{"tourney", two, "--rounds", "1", "--core-size", "10"}, 1, "placement impossible", ""},
		{"bad config", []string{"tourney", two, "--cycle-limit", "0"}, 1, "invalid config", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := tourneyPit(tt.args...)
			if code != tt.wantCode {
				t.Errorf("exit code %d, want %d (stderr %q)", code, tt.wantCode, errOut)
			}
			if !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("stderr %q does not contain %q", errOut, tt.wantErr)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("stdout %q does not contain %q", out, tt.wantOut)
			}
			if tt.wantCode != 0 && out != "" {
				t.Errorf("stdout = %q, want empty on failure", out)
			}
		})
	}
}

// TestTourneyDashDashFlagValue: a flag value that is the literal "--" is a value, not
// the end of the flags. Here "--matches --" takes "--" as its file name, so the later
// "--rounds 0" is still a flag and is refused as one (usage error, nothing written).
func TestTourneyDashDashFlagValue(t *testing.T) {
	dir := starterDir(t)
	code, out, errOut := tourneyPit("tourney", "--matches", "--", dir, "--rounds", "0")
	if code != 2 || out != "" || !strings.Contains(errOut, "--rounds must be at least 1, got 0") {
		t.Errorf("exit code %d, stdout %q, stderr %q; want 2 and the --rounds error", code, out, errOut)
	}
	if _, err := os.Stat("--"); err == nil {
		os.Remove("--")
		t.Error("a refused run wrote the file --")
	}
}
