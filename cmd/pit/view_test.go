// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/pitfight/internal/replay"
	"github.com/squalor-xyz/pitfight/internal/tui"
)

const viewFixture = "../../testdata/viewer/fixture.jsonl"

const clearScreen = "\x1b[H\x1b[2J"

var headerRe = regexp.MustCompile(`^pitfight  cycle (\d+)/(\d+)  core \d+(?: \([^)]*\))?  (\w+)$`)

// states returns one "cycle state" string per drawing in the output, taken from the
// header line of each ("3 paused", "5 playing").
func states(t *testing.T, out string) []string {
	t.Helper()
	var got []string
	for _, part := range strings.Split(out, clearScreen)[1:] {
		line, _, _ := strings.Cut(part, "\n")
		m := headerRe.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("drawing without a header line: %q", line)
		}
		got = append(got, m[1]+" "+m[3])
	}
	return got
}

func pitView(t *testing.T, input string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = runIn(append([]string{"view"}, args...), strings.NewReader(input), &out, &errb)
	return code, out.String(), errb.String()
}

// TestViewCommand drives the player of the shared fixture (12 cycles) with commands
// from a reader. Every drawing starts with the clear sequence, and the command exits
// 0 at the end of input or on q.
func TestViewCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"no input", "", []string{"0 paused"}},
		{"step and seek", "s\ns\ng 7\nb 2\n", []string{"0 paused", "1 paused", "2 paused", "7 paused", "5 paused"}},
		{"counts and enter", "s 3\nb\n\n", []string{"0 paused", "3 paused", "2 paused", "3 paused"}},
		{"seek out of range", "g 99\ng -4\n", []string{"0 paused", "12 paused", "0 paused"}},
		{"quit stops reading", "g 4\nq\ns\ns\n", []string{"0 paused", "4 paused"}},
		{"spaces and carriage returns", "  g   6 \r\n s\r\n", []string{"0 paused", "6 paused", "7 paused"}},
		{"play N cycles", "p 3\n", []string{"0 paused", "0 playing", "1 playing", "2 playing", "3 paused"}},
		{"play N stops at the end", "g 10\np 9\n", []string{"0 paused", "10 paused", "10 playing", "11 playing", "12 paused"}},
		{"play at the end", "g 12\np 4\n", []string{"0 paused", "12 paused", "12 paused"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := pitView(t, tt.input, viewFixture, "--delay", "0", "--cols", "8", "--rows", "4")
			if code != 0 || errOut != "" {
				t.Fatalf("exit code %d, stderr %q; want 0 and empty", code, errOut)
			}
			got := states(t, out)
			if strings.Join(got, ", ") != strings.Join(tt.want, ", ") {
				t.Errorf("drawings = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestViewCommandDraws checks one full drawing: the last frame after "g 12" is the
// frame the renderer produces for cycle 12 of the fixture.
func TestViewCommandDraws(t *testing.T) {
	_, out, _ := pitView(t, "g 12\n", viewFixture, "--cols", "8", "--rows", "4")
	i := strings.LastIndex(out, clearScreen)
	if i < 0 {
		t.Fatalf("no drawing in %q", out)
	}
	last := out[i:]
	for _, want := range []string{
		"pitfight  cycle 12/12  core 24  paused\n",
		" bot 1  live 0  pc 14\n",
		" bot 2  live 1  pc 21\n",
		"s [N] step",
	} {
		if !strings.Contains(plainText(last), want) {
			t.Errorf("last drawing lacks %q:\n%q", want, last)
		}
	}
}

func plainText(s string) string {
	return regexp.MustCompile("\x1b\\[[0-9;]*[mHJ]").ReplaceAllString(s, "")
}

// TestViewCommandMessages: a bad command or argument is reported on the screen and
// does not end the command.
func TestViewCommandMessages(t *testing.T) {
	tests := []struct{ input, want string }{
		{"x\n", `unknown command "x"`},
		{"g\n", "g needs a cycle number"},
		{"g abc\n", "g needs a cycle number"},
		{"g 1 2\n", "g needs a cycle number"},
		{"s 0\n", "s needs a positive number"},
		{"s x\n", "s needs a positive number"},
		{"b -2\n", "b needs a positive number"},
		{"p 0\n", "p needs a positive number"},
		{"s 1 2\n", "s needs a positive number"},
	}
	for _, tt := range tests {
		code, out, _ := pitView(t, tt.input+"s\n", viewFixture, "--delay", "0")
		if code != 0 {
			t.Errorf("%q: exit code %d, want 0", tt.input, code)
		}
		parts := strings.Split(out, clearScreen)
		if len(parts) != 4 { // empty, initial, the bad command, the step after it
			t.Errorf("%q: %d drawings, want 3", tt.input, len(parts)-1)
			continue
		}
		if !strings.Contains(plainText(parts[2]), tt.want+"\n") {
			t.Errorf("%q: drawing lacks %q:\n%s", tt.input, tt.want, plainText(parts[2]))
		}
		if strings.Contains(plainText(parts[3]), tt.want) {
			t.Errorf("%q: the message stays on the next drawing", tt.input)
		}
		if states(t, out)[1] != "0 paused" || states(t, out)[2] != "1 paused" {
			t.Errorf("%q: states %q", tt.input, states(t, out))
		}
	}
}

func TestViewCommandErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte("not a replay\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string // substring of stdout
		wantErr  string // substring of stderr
	}{
		{"no file", nil, 2, "", "usage: pit view"},
		{"two files", []string{viewFixture, viewFixture}, 2, "", "usage: pit view"},
		{"unknown flag", []string{"--bogus", viewFixture}, 2, "", "usage: pit view"},
		{"zero columns", []string{"--cols", "0", viewFixture}, 2, "", "--cols"},
		{"zero rows", []string{"--rows", "0", viewFixture}, 2, "", "--rows"},
		{"negative delay", []string{"--delay", "-1", viewFixture}, 2, "", "--delay"},
		{"help", []string{"--help"}, 0, "usage: pit view", ""},
		{"missing file", []string{filepath.Join(dir, "none.jsonl")}, 1, "", "pit view:"},
		{"not a replay", []string{bad}, 1, "", "pit view:"},
		{"flags after the file", []string{viewFixture, "--cols", "8"}, 0, "cycle 0/12", ""},
		{"after double dash", []string{"--", viewFixture}, 0, "cycle 0/12", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := pitView(t, "", tt.args...)
			if code != tt.wantCode || !strings.Contains(out, tt.wantOut) || !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("code %d stdout %q stderr %q; want code %d, stdout with %q, stderr with %q",
					code, out, errOut, tt.wantCode, tt.wantOut, tt.wantErr)
			}
			if tt.wantCode != 0 && out != "" {
				t.Errorf("stdout = %q, want empty on failure", out)
			}
		})
	}
}

// TestViewUsage: the main usage text no longer calls view unimplemented, and the
// view usage names every command.
func TestViewUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"help"}, &out, &errb); code != 0 {
		t.Fatalf("help: code %d", code)
	}
	if strings.Contains(out.String(), "not implemented") {
		t.Errorf("usage still says view is not implemented:\n%s", out.String())
	}
	_, help, _ := pitView(t, "", "--help")
	for _, w := range []string{"s [N]", "b [N]", "g N", "p N", "q", "--cols", "--rows", "--delay"} {
		if !strings.Contains(help, w) {
			t.Errorf("view usage lacks %q:\n%s", w, help)
		}
	}
}

// syncBuffer is a bytes.Buffer that a test can wait on while the session writes.
type syncBuffer struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  bytes.Buffer
}

func newSyncBuffer() *syncBuffer {
	b := &syncBuffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	b.cond.Broadcast()
	return n, err
}

// waitDrawings blocks until the output holds n drawings, and returns the output.
func (b *syncBuffer) waitDrawings(t *testing.T, n int) string {
	t.Helper()
	timer := time.AfterFunc(10*time.Second, func() { b.cond.Broadcast() })
	defer timer.Stop()
	deadline := time.Now().Add(10 * time.Second)
	b.mu.Lock()
	defer b.mu.Unlock()
	for strings.Count(b.buf.String(), clearScreen) < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d drawings; have %d", n, strings.Count(b.buf.String(), clearScreen))
		}
		b.cond.Wait()
	}
	return b.buf.String()
}

func newSession(t *testing.T) (pl *tui.Player, ticks chan time.Time, in *io.PipeWriter, out *syncBuffer, done chan error) {
	t.Helper()
	f, err := os.Open(viewFixture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := replay.Read(f)
	if err != nil {
		t.Fatal(err)
	}
	m, err := tui.NewModel(r)
	if err != nil {
		t.Fatal(err)
	}
	pl = tui.NewPlayer(m)
	ticks = make(chan time.Time)
	pr, pw := io.Pipe()
	out = newSyncBuffer()
	done = make(chan error, 1)
	cfg := viewConfig{
		opts:  tui.Options{Cols: 8, Rows: 4},
		after: func(time.Duration) <-chan time.Time { return ticks },
	}
	go func() { done <- viewSession(pr, out, pl, cfg) }()
	return pl, ticks, pw, out, done
}

func send(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := io.WriteString(w, s); err != nil {
		t.Fatal(err)
	}
}

func finish(t *testing.T, in *io.PipeWriter, done chan error) {
	t.Helper()
	in.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("session error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end at the end of input")
	}
}

// TestViewPause plays, pauses and steps with the clock under the test's control:
// each tick is sent by the test, so the sequence is deterministic.
func TestViewPause(t *testing.T) {
	_, ticks, in, out, done := newSession(t)
	out.waitDrawings(t, 1)
	send(t, in, "p\n")
	out.waitDrawings(t, 2)
	ticks <- time.Time{}
	out.waitDrawings(t, 3)
	ticks <- time.Time{}
	out.waitDrawings(t, 4)
	send(t, in, "p\n") // pause
	out.waitDrawings(t, 5)
	send(t, in, "s\n")
	got := out.waitDrawings(t, 6)
	finish(t, in, done)
	want := "0 paused, 0 playing, 1 playing, 2 playing, 2 paused, 3 paused"
	if s := strings.Join(states(t, got), ", "); s != want {
		t.Errorf("drawings = %s, want %s", s, want)
	}
}

// TestViewPlayToEnd: an open-ended play runs to the last cycle and stops there.
func TestViewPlayToEnd(t *testing.T) {
	_, ticks, in, out, done := newSession(t)
	send(t, in, "p\n")
	out.waitDrawings(t, 2)
	for i := 1; i <= 12; i++ {
		ticks <- time.Time{}
		out.waitDrawings(t, 2+i)
	}
	got := states(t, out.waitDrawings(t, 14))
	if last := got[len(got)-1]; last != "12 paused" {
		t.Errorf("last drawing = %q, want \"12 paused\" (all: %q)", last, got)
	}
	finish(t, in, done)
}

// TestViewEOFStopsPlay: the end of input ends an open-ended play at once.
func TestViewEOFStopsPlay(t *testing.T) {
	pl, _, in, out, done := newSession(t)
	send(t, in, "p\n")
	out.waitDrawings(t, 2)
	finish(t, in, done)
	if pl.Cycle() != 0 {
		t.Errorf("cycle = %d after EOF in play, want 0 (no tick was sent)", pl.Cycle())
	}
}

// TestViewInputPausesPlay: a line typed during play pauses it (an empty line too).
func TestViewInputPausesPlay(t *testing.T) {
	pl, ticks, in, out, done := newSession(t)
	send(t, in, "p\n")
	out.waitDrawings(t, 2)
	ticks <- time.Time{}
	out.waitDrawings(t, 3)
	send(t, in, "\n")
	got := out.waitDrawings(t, 4)
	finish(t, in, done)
	if want := "0 paused, 0 playing, 1 playing, 1 paused"; strings.Join(states(t, got), ", ") != want {
		t.Errorf("drawings = %q, want %s", states(t, got), want)
	}
	if pl.Playing() || pl.Cycle() != 1 {
		t.Errorf("player: playing %v cycle %d, want paused at 1", pl.Playing(), pl.Cycle())
	}
}

// TestFixtureIsPitRunOutput: testdata/viewer/fixture.jsonl, shared with the web
// viewer, is what pit run writes for the starter bots imp and dwarf on a core of 24.
func TestFixtureIsPitRunOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.jsonl")
	code, _, errOut := runPit("run", "../../bots/imp.s", "../../bots/dwarf.s",
		"--seed", "24", "--core-size", "24", "--min-separation", "3", "--cycle-limit", "40", "--replay", path)
	if code != 0 {
		t.Fatalf("pit run: code %d, stderr %q", code, errOut)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(viewFixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("pit run output differs from %s", viewFixture)
	}
}
