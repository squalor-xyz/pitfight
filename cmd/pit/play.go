// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"time"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/replay"
	"github.com/squalor-xyz/pitfight/internal/tui"
)

const playUsage = `usage: pit play [flags] [--] <bot.s> <bot.s> [<bot.s> ...]

Assembles the bots, runs one match and shows it: the web viewer is served on this
machine only (127.0.0.1), the match is loaded into it, and the browser is opened
on it. Ctrl-C stops the server. With --terminal the match plays in the terminal
viewer instead, with the commands of "pit view". The replay stays in memory; only
--replay FILE writes it. Bots get the ids 1, 2, ... in the order given. Flags and
files may come in any order; every argument after -- is a file.

The seed is printed, so that a match you liked can be repeated with --seed N.
Without --seed a random one is chosen. With --terminal the seed is printed when the
viewer ends (q or end of input), not when you stop it with Ctrl-C: pass --seed N to
keep a match you may stop that way.

Flags:
  --seed N          placement seed, 0 to 2^64-1 (default: random)
  --cycle-limit N   maximum number of cycles, 1 to 2147483647 (default 80000)
  --port N          port to listen on, 0 to 65535 (default 0: a free port)
  --no-open         print the address but do not open the browser
  --terminal        play in the terminal viewer; no server, --port and --no-open
                    are ignored
  --replay FILE     also write the replay (docs/spec/replay.md) to FILE and print
                    its hash

If the browser cannot be opened (open on macOS, xdg-open on Linux) the address is
printed and the server keeps running.

Exit codes: 0 normal end; 1 an error in a bot file or the configuration, or in
starting the server; 2 a usage error.
`

// playReplayPath is where the viewer address of "pit play" finds the replay: the
// same /replays/ place that "pit serve --replays" uses, and loaded by the viewer
// with ?replay=.
const playReplayPath = "/replays/match.jsonl"

// playConfig is the part of "pit play" that tests replace. Zero values mean the real
// thing.
type playConfig struct {
	open    func(url string) error // opens the browser; nil: the platform opener
	newSeed func() (uint64, error) // the default seed; nil: random from crypto/rand
	view    viewConfig             // the terminal viewer; zero: the defaults of pit view
}

// runPlay implements "pit play". args excludes "play".
func runPlay(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return play(context.Background(), playConfig{}, args, stdin, stdout, stderr)
}

// play runs the match and shows it. For the web viewer it serves until ctx ends or
// the process is interrupted.
func play(ctx context.Context, pc playConfig, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg := arena.DefaultConfig()
	var (
		port       int
		noOpen     bool
		terminal   bool
		replayPath string
	)
	fs := flag.NewFlagSet("pit play", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Uint64Var(&cfg.Seed, "seed", 0, "")
	fs.IntVar(&cfg.CycleLimit, "cycle-limit", cfg.CycleLimit, "")
	fs.IntVar(&port, "port", 0, "")
	fs.BoolVar(&noOpen, "no-open", false, "")
	fs.BoolVar(&terminal, "terminal", false, "")
	fs.StringVar(&replayPath, "replay", "", "")

	files, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, playUsage)
			return 0
		}
		fmt.Fprintf(stderr, "pit play: %v\n\n%s", err, playUsage)
		return 2
	}
	if len(files) < 2 {
		fmt.Fprint(stderr, playUsage)
		return 2
	}
	if port < 0 || port > 65535 {
		fmt.Fprintf(stderr, "pit play: --port must be 0 to 65535\n\n%s", playUsage)
		return 2
	}
	seedGiven := false
	fs.Visit(func(f *flag.Flag) { seedGiven = seedGiven || f.Name == "seed" })

	bots, ok := assembleFiles("pit play", files, stderr)
	if !ok {
		return 1
	}
	if !seedGiven {
		newSeed := pc.newSeed
		if newSeed == nil {
			newSeed = randomSeed
		}
		if cfg.Seed, err = newSeed(); err != nil {
			fmt.Fprintf(stderr, "pit play: choosing a seed: %v\n", err)
			return 1
		}
	}
	rec, err := replay.Record(cfg, bots)
	if err != nil {
		fmt.Fprintf(stderr, "pit play: %v\n", err)
		return 1
	}
	data := rec.Bytes()

	// What the player needs to know after the match, printed before the server
	// starts, or after the terminal viewer ends (its first drawing clears the screen).
	announce := func() {
		fmt.Fprintf(stdout, "pit play: seed %d (repeat this match with --seed %d)\n", cfg.Seed, cfg.Seed)
		if replayPath != "" {
			fmt.Fprintf(stdout, "pit play: replay written to %s (hash %s)\n", replayPath, replay.Hash(data))
		}
	}
	if replayPath != "" {
		err := writeFileAtomic(replayPath, 0o644, func(w io.Writer) error { _, err := w.Write(data); return err })
		if err != nil {
			fmt.Fprintf(stderr, "pit play: %v\n", err)
			return 1
		}
	}

	if terminal {
		return playTerminal(pc, data, announce, stdin, stdout, stderr)
	}
	announce()
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		fmt.Fprintf(stderr, "pit play: %v\n", err)
		return 1
	}
	defer ln.Close()
	addr := "http://" + ln.Addr().String() + "/?replay=" + playReplayPath + "&autoplay=1"
	fmt.Fprintf(stdout, "pit play: the match is at %s (Ctrl-C to stop)\n", addr)
	if !noOpen {
		open := pc.open
		if open == nil {
			open = openBrowser
		}
		// Before the server takes requests, so that a request that was answered
		// means the opener has been called already (or not, with --no-open).
		if err := open(addr); err != nil {
			fmt.Fprintf(stderr, "pit play: could not open a browser (%v); open the address above yourself\n", err)
		}
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	if err := serveUntil(ctx, ln, newPlayHandler(data)); err != nil {
		fmt.Fprintf(stderr, "pit play: %v\n", err)
		return 1
	}
	return 0
}

// playTerminal plays the replay in the terminal viewer on stdin and stdout.
func playTerminal(pc playConfig, data []byte, announce func(), stdin io.Reader, stdout, stderr io.Writer) int {
	r, err := replay.Read(bytes.NewReader(data))
	if err != nil {
		fmt.Fprintf(stderr, "pit play: %v\n", err)
		return 1
	}
	m, err := tui.NewModel(r)
	if err != nil {
		fmt.Fprintf(stderr, "pit play: %v\n", err)
		return 1
	}
	vc := pc.view
	if vc.opts.Cols == 0 && vc.opts.Rows == 0 {
		vc.opts = tui.Options{Cols: tui.DefaultCols, Rows: tui.DefaultRows}
		vc.delay = 100 * time.Millisecond
	}
	if err := viewSession(stdin, stdout, tui.NewPlayer(m), vc); err != nil {
		fmt.Fprintf(stderr, "pit play: %v\n", err)
		return 1
	}
	announce()
	return 0
}

// newPlayHandler returns the handler of "pit play": the viewer and its API as "pit
// serve" serves them without directories (the start page then has nothing to list),
// and the replay at playReplayPath, held in memory. The Host check covers the replay
// as it covers the rest.
func newPlayHandler(data []byte) http.Handler {
	// No directory is opened, so there is no error and nothing to close.
	viewer, _, _ := newServeHandler("", "", "")
	mux := http.NewServeMux()
	mux.Handle("/", viewer)
	mux.Handle(playReplayPath, getOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setAPIHeaders(w, "application/x-ndjson")
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	})))
	return localOnly(mux)
}

// randomSeed draws a seed from the operating system's random source. The CLI may do
// this: determinism (C36) binds the VM and arena core, and the chosen seed is printed.
func randomSeed() (uint64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b[:]), nil
}

// openerCommand returns the program and arguments that open url in the default
// browser on goos: open on macOS, xdg-open on Linux and the BSDs.
func openerCommand(goos, url string) (name string, args []string, err error) {
	switch goos {
	case "darwin":
		return "open", []string{url}, nil
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		return "xdg-open", []string{url}, nil
	}
	return "", nil, fmt.Errorf("no browser opener for %s", goos)
}

// openBrowser starts the platform opener and does not wait for the browser.
func openBrowser(url string) error {
	name, args, err := openerCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap the child
	return nil
}
