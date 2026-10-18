// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/replay"
)

// playLimit keeps the matches of these tests short.
const playLimit = "200"

var playURLRe = regexp.MustCompile(`http://127\.0\.0\.1:\d+/\?replay=\S+`)
var runHashRe = regexp.MustCompile(`(?m)^replay: ([0-9a-f]{64})$`)

// pitRunReplay runs "pit run" for the bots with the seed and the test cycle limit and
// returns the replay file it writes and the hash it prints.
func pitRunReplay(t *testing.T, paths []string, seed string) (data []byte, hash string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "run.jsonl")
	args := append([]string{"run"}, paths...)
	args = append(args, "--seed", seed, "--cycle-limit", playLimit, "--replay", file)
	code, out, errOut := runPit(args...)
	if code != 0 {
		t.Fatalf("pit run = %d, stderr %q", code, errOut)
	}
	m := runHashRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no replay hash in %q", out)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return data, m[1]
}

// startedPlay is a running "pit play" under a test's control.
type startedPlay struct {
	stdout, stderr *syncBuffer
	done           chan int
	cancel         context.CancelFunc
}

// startPlay runs play in the background with no input.
func startPlay(t *testing.T, pc playConfig, args ...string) *startedPlay {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	p := &startedPlay{stdout: newSyncBuffer(), stderr: newSyncBuffer(), done: make(chan int, 1), cancel: cancel}
	go func() { p.done <- play(ctx, pc, args, strings.NewReader(""), p.stdout, p.stderr) }()
	t.Cleanup(cancel)
	return p
}

// url waits for the viewer address on stdout.
func (p *startedPlay) url(t *testing.T) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if u := playURLRe.FindString(contents(p.stdout)); u != "" {
			return u
		}
		select {
		case code := <-p.done:
			t.Fatalf("play ended with %d before it printed an address; stdout %q, stderr %q", code, contents(p.stdout), contents(p.stderr))
		default:
		}
	}
	t.Fatalf("no address on stdout %q (stderr %q)", contents(p.stdout), contents(p.stderr))
	return ""
}

// stop ends the context (Ctrl-C) and returns the exit code.
func (p *startedPlay) stop(t *testing.T) int {
	t.Helper()
	p.cancel()
	select {
	case code := <-p.done:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("play did not return after its context ended")
		return -1
	}
}

// fetch returns the body of a GET of the address; ref is resolved against base.
func fetch(t *testing.T, base, ref string) (int, []byte) {
	t.Helper()
	b, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	r, err := b.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(r.String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// preloaded returns the replay address that the page of the address loads.
func preloaded(t *testing.T, addr string) string {
	t.Helper()
	u, err := url.Parse(addr)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("replay")
}

// playBounded runs play for a call that must end by itself (bad input, a failed seed
// source, the terminal viewer). If it has not ended after ten seconds the context ends
// it, and the test fails: a regression that reached the server would otherwise block
// until the test timeout.
func playBounded(t *testing.T, pc playConfig, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code := play(ctx, pc, args, stdin, stdout, stderr)
	if ctx.Err() != nil {
		t.Fatalf("play %v was still running after 10 s (it served instead of ending); exit %d", args, code)
	}
	return code
}

// opened records the addresses that the stub opener gets.
type opened struct{ urls chan string }

func newOpened() *opened { return &opened{urls: make(chan string, 8)} }

func (o *opened) open(u string) error { o.urls <- u; return nil }

// TestPlayServesTheReplayOfPitRun: the viewer address names a replay that the server
// serves, and that replay (and its hash) is the one "pit run" gives for the same
// bots and seed.
func TestPlayServesTheReplayOfPitRun(t *testing.T) {
	paths := replayTestBots(t)
	want, wantHash := pitRunReplay(t, paths, "7")

	p := startPlay(t, playConfig{open: newOpened().open}, paths[0], paths[1], "--seed", "7", "--cycle-limit", playLimit, "--port", "0")
	addr := p.url(t)
	ref := preloaded(t, addr)
	if ref == "" {
		t.Fatalf("the address %q does not name a replay in ?replay=", addr)
	}
	code, got := fetch(t, addr, ref)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", ref, code)
	}
	if h := replay.Hash(got); h != wantHash {
		t.Errorf("served replay hash %s, want %s (the hash pit run prints)", h, wantHash)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("served replay differs from the file of pit run")
	}
	// The address is the viewer page, which holds the viewer.
	if code, page := fetch(t, addr, "/"); code != http.StatusOK || !strings.Contains(string(page), "pitfight") {
		t.Errorf("GET / = %d %.60q, want the viewer page", code, page)
	}
	if c := p.stop(t); c != 0 {
		t.Errorf("exit code %d after the context ended, want 0 (stderr %q)", c, contents(p.stderr))
	}
}

// TestPlayOpensTheBrowser: the opener gets the address that the command prints, with
// the replay preloaded; --no-open does not call it and still prints the address.
func TestPlayOpensTheBrowser(t *testing.T) {
	paths := replayTestBots(t)
	t.Run("opens", func(t *testing.T) {
		o := newOpened()
		p := startPlay(t, playConfig{open: o.open}, paths[0], paths[1], "--seed", "7", "--cycle-limit", playLimit, "--port", "0")
		printed := p.url(t)
		select {
		case got := <-o.urls:
			if got != printed {
				t.Errorf("opener got %q, the command printed %q", got, printed)
			}
			if u, err := url.Parse(got); err != nil || u.Query().Get("autoplay") != "1" {
				t.Errorf("the opened address %q lacks autoplay=1, so the page would show the match paused", got)
			}
			if code, body := fetch(t, got, preloaded(t, got)); code != http.StatusOK || !strings.Contains(string(body), `{"t":"header"`) {
				t.Errorf("the replay of the opened address = %d %.60q, want the replay", code, body)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the opener was not called")
		}
		p.stop(t)
	})
	t.Run("no-open", func(t *testing.T) {
		o := newOpened()
		p := startPlay(t, playConfig{open: o.open}, paths[0], paths[1], "--seed", "7", "--cycle-limit", playLimit, "--port", "0", "--no-open")
		addr := p.url(t)
		// The opener is called before the server takes requests, so once this
		// request is answered a call would have happened.
		if code, _ := fetch(t, addr, preloaded(t, addr)); code != http.StatusOK {
			t.Errorf("replay status %d, want 200", code)
		}
		select {
		case got := <-o.urls:
			t.Errorf("--no-open called the opener with %q", got)
		default:
		}
		p.stop(t)
	})
}

// TestPlayOpenerFailure: if the browser cannot be opened the command says so, still
// prints the address and keeps serving.
func TestPlayOpenerFailure(t *testing.T) {
	paths := replayTestBots(t)
	boom := errors.New("no such program")
	p := startPlay(t, playConfig{open: func(string) error { return boom }}, paths[0], paths[1], "--seed", "7", "--cycle-limit", playLimit, "--port", "0")
	addr := p.url(t)
	if code, _ := fetch(t, addr, preloaded(t, addr)); code != http.StatusOK {
		t.Errorf("replay status %d after a failed open, want 200", code)
	}
	if !strings.Contains(contents(p.stderr), boom.Error()) {
		t.Errorf("stderr %q does not give the opener's error %q", contents(p.stderr), boom)
	}
	if c := p.stop(t); c != 0 {
		t.Errorf("exit code %d, want 0", c)
	}
}

// TestPlaySeed: without --seed the seed comes from the seed source, is printed in a
// form that repeats the match, and the match is the one pit run gives for that seed.
// With --seed the source is not asked.
func TestPlaySeed(t *testing.T) {
	paths := replayTestBots(t)
	want, _ := pitRunReplay(t, paths, "99")
	pc := playConfig{open: newOpened().open, newSeed: func() (uint64, error) { return 99, nil }}
	p := startPlay(t, pc, paths[0], paths[1], "--cycle-limit", playLimit, "--port", "0")
	addr := p.url(t)
	if out := contents(p.stdout); !strings.Contains(out, "seed 99") || !strings.Contains(out, "--seed 99") {
		t.Errorf("stdout %q does not print the seed 99 and how to repeat it", out)
	}
	if _, got := fetch(t, addr, preloaded(t, addr)); !bytes.Equal(got, want) {
		t.Errorf("replay of the drawn seed differs from pit run --seed 99")
	}
	p.stop(t)

	pc.newSeed = func() (uint64, error) { t.Error("the seed source was asked although --seed was given"); return 0, nil }
	p = startPlay(t, pc, paths[0], paths[1], "--seed", "99", "--cycle-limit", playLimit, "--port", "0")
	addr = p.url(t)
	if _, got := fetch(t, addr, preloaded(t, addr)); !bytes.Equal(got, want) {
		t.Errorf("replay of --seed 99 differs from pit run --seed 99")
	}
	p.stop(t)

	pc.newSeed = func() (uint64, error) { return 0, errors.New("no entropy") }
	var out, errb bytes.Buffer
	if code := playBounded(t, pc, []string{paths[0], paths[1]}, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), "no entropy") {
		t.Errorf("failing seed source: exit %d, stderr %q; want 1 and the error", code, errb.String())
	}
}

// TestRandomSeed: the default seed source gives different seeds.
func TestRandomSeed(t *testing.T) {
	a, err := randomSeed()
	if err != nil {
		t.Fatal(err)
	}
	b, err := randomSeed()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("two draws gave %d", a)
	}
}

// TestPlayReplayFlag: --replay FILE writes the replay that is served, and prints its hash.
func TestPlayReplayFlag(t *testing.T) {
	paths := replayTestBots(t)
	want, wantHash := pitRunReplay(t, paths, "7")
	file := filepath.Join(t.TempDir(), "kept.jsonl")
	p := startPlay(t, playConfig{open: newOpened().open}, paths[0], paths[1], "--seed", "7", "--cycle-limit", playLimit, "--port", "0", "--replay", file)
	addr := p.url(t)
	_, served := fetch(t, addr, preloaded(t, addr))
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || !bytes.Equal(served, want) {
		t.Errorf("--replay file or served replay differs from the file of pit run")
	}
	if out := contents(p.stdout); !strings.Contains(out, wantHash) || !strings.Contains(out, file) {
		t.Errorf("stdout %q does not name the file and the hash %s", out, wantHash)
	}
	p.stop(t)
}

// TestPlayTerminal: --terminal runs the match and plays it in the terminal viewer on
// the given input: the same drawings as "pit view" on the replay of pit run, then the
// seed. No server is started and the browser is not opened.
func TestPlayTerminal(t *testing.T) {
	paths := replayTestBots(t)
	data, _ := pitRunReplay(t, paths, "7")
	file := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	input := "g 5\ns\nq\n"
	_, viewOut, _ := pitView(t, input, file)

	o := newOpened()
	var out, errb bytes.Buffer
	code := playBounded(t, playConfig{open: o.open}, []string{paths[0], paths[1], "--terminal", "--seed", "7", "--cycle-limit", playLimit}, strings.NewReader(input), &out, &errb)
	if code != 0 || errb.Len() != 0 {
		t.Fatalf("exit %d, stderr %q; want 0 and empty", code, errb.String())
	}
	if want := "pit play: seed 7"; !strings.Contains(out.String(), want) {
		t.Errorf("stdout does not say %q", want)
	}
	drawn, _, _ := strings.Cut(out.String(), "pit play:")
	if drawn != viewOut {
		t.Errorf("drawings differ from pit view on the replay of pit run:\n%q\nvs\n%q", drawn, viewOut)
	}
	if got := states(t, drawn); strings.Join(got, ",") != "0 paused,5 paused,6 paused" {
		t.Errorf("states %v, want the loaded core, then cycle 5, then cycle 6", got)
	}
	if strings.Contains(out.String(), "http://") {
		t.Errorf("--terminal printed an address: %q", out.String())
	}
	select {
	case u := <-o.urls:
		t.Errorf("--terminal opened %q", u)
	default:
	}
}

// TestPlayErrors: bad input exits with the codes of pit run, and starts nothing.
func TestPlayErrors(t *testing.T) {
	const badSrc = "MOV.I $0, $1\nFOO $1, $2\n"
	paths := writeBots(t, map[string]string{"good.s": sitterSrc, "bad.s": badSrc}, "good.s", "bad.s")
	_, asmErr := asm.Assemble(paths[1], []byte(badSrc))
	if asmErr == nil {
		t.Fatal("test source assembles")
	}
	missing := filepath.Join(t.TempDir(), "missing.s")
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"assembler error", []string{paths[0], paths[1]}, 1, asmErr.Error() + "\n"},
		{"missing file", []string{paths[0], missing}, 1, "pit play: "},
		{"bad cycle limit", []string{paths[0], paths[0], "--cycle-limit", "0"}, 1, "pit play: "},
		{"no files", nil, 2, "usage: pit play"},
		{"one file", []string{paths[0]}, 2, "usage: pit play"},
		{"unknown flag", []string{paths[0], paths[0], "--bogus"}, 2, "usage: pit play"},
		{"bad seed", []string{paths[0], paths[0], "--seed", "-1"}, 2, "usage: pit play"},
		{"port too big", []string{paths[0], paths[0], "--port", "70000"}, 2, "--port"},
		{"port negative", []string{paths[0], paths[0], "--port", "-1"}, 2, "--port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newOpened()
			var out, errb bytes.Buffer
			code := playBounded(t, playConfig{open: o.open, newSeed: func() (uint64, error) { return 1, nil }}, tt.args, strings.NewReader(""), &out, &errb)
			if code != tt.wantCode || !strings.Contains(errb.String(), tt.wantErr) {
				t.Errorf("play %v = %d, stderr %q; want %d containing %q", tt.args, code, errb.String(), tt.wantCode, tt.wantErr)
			}
			if tt.name == "assembler error" && (errb.String() != tt.wantErr || !strings.Contains(errb.String(), "bad.s:2:1:")) {
				t.Errorf("stderr %q, want exactly %q with line and column", errb.String(), tt.wantErr)
			}
			if strings.Contains(out.String(), "http://") {
				t.Errorf("an address was printed for bad input: %q", out.String())
			}
			select {
			case u := <-o.urls:
				t.Errorf("the browser was opened (%q) for bad input", u)
			default:
			}
		})
	}
}

// TestPlayHelp: --help prints the usage on stdout, and pit's own usage lists play.
func TestPlayHelp(t *testing.T) {
	code, out, errOut := runPit("play", "--help")
	if code != 0 || !strings.Contains(out, "usage: pit play") || errOut != "" {
		t.Errorf("play --help = %d, stdout %.60q, stderr %q", code, out, errOut)
	}
	if code, out, _ := runPit("help"); code != 0 || !strings.Contains(out, "\n  play ") {
		t.Errorf("help = %d, stdout %q: want the play command listed", code, out)
	}
}

// TestPlayHandlerHostCheck: the replay is served to this machine only, like the rest
// of pit serve.
func TestPlayHandlerHostCheck(t *testing.T) {
	h := newPlayHandler([]byte("replay bytes"))
	if rec := do(h, http.MethodGet, "127.0.0.1:8080", playReplayPath); rec.Code != http.StatusOK || rec.Body.String() != "replay bytes" {
		t.Errorf("local GET = %d %q, want 200 and the replay", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "127.0.0.1:8080", playReplayPath); rec.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Errorf("replay content type %q, want application/x-ndjson", rec.Header().Get("Content-Type"))
	}
	if rec := do(h, http.MethodGet, "evil.example:8080", playReplayPath); rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "replay bytes") {
		t.Errorf("foreign Host GET = %d %q, want 403 without the replay", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "127.0.0.1:8080", playReplayPath); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", rec.Code)
	}
}

// TestOpenerCommand: the platform opener is open on macOS and xdg-open on Linux, with
// the address as the one argument; elsewhere there is none.
func TestOpenerCommand(t *testing.T) {
	const addr = "http://127.0.0.1:1234/?replay=/replays/match.jsonl"
	for _, tt := range []struct {
		goos, name string
		ok         bool
	}{{"darwin", "open", true}, {"linux", "xdg-open", true}, {"plan9", "", false}} {
		name, args, err := openerCommand(tt.goos, addr)
		if (err == nil) != tt.ok || name != tt.name || (tt.ok && (len(args) != 1 || args[0] != addr)) {
			t.Errorf("openerCommand(%q) = %q %q, %v; want %q with the address, ok=%v", tt.goos, name, args, err, tt.name, tt.ok)
		}
	}
}
