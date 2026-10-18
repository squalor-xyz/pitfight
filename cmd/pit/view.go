// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/squalor-xyz/pitfight/internal/replay"
	"github.com/squalor-xyz/pitfight/internal/tui"
)

const viewUsage = `usage: pit view [flags] [--] <replay.jsonl>

Shows a replay (written by "pit run --replay FILE") in the terminal: the core as a
grid of coloured characters, one colour and character per bot, the cells written by
a bot in its colour, and the executed cells of the current cycle in reverse video.
A core too big for the grid shows one character per block of cells.

Commands are read from standard input, one per line (press Enter after each):
  s [N]   step forward N cycles (default 1)
  b [N]   step back N cycles (default 1)
  g N     go to cycle N (0 is the loaded core; out of range means the nearest end)
  p       play to the last cycle; p again, or any line, pauses
  p N     play N cycles, then pause
  q       quit
  (empty line: step 1, or pause while playing)
A line of more than 65536 bytes is ignored with a message, and the next lines still
run.
A play shows one cycle per --delay and stops at the last cycle. At the end of input
the command exits; it first finishes a play started with a count (p N), but stops a
play to the end at once, so piped input uses "p N".

Flags:
  --cols N    grid width in characters (default 72)
  --rows N    grid height in lines (default 20)
  --delay MS  milliseconds between cycles while playing (default 100)

Exit codes: 0 normal end; 1 an error in the replay file or reading input; 2 a usage error.
`

// maxViewLine is the longest command line, without its line break, that pit view
// runs. A longer line is skipped with a message and the session goes on.
const maxViewLine = 64 * 1024

// viewLine is one line of input. TooLong is set, and Text empty, for a line of more
// than maxViewLine bytes.
type viewLine struct {
	Text    string
	TooLong bool
}

// readViewLine reads the next line of r, a reader with a buffer of at least
// maxViewLine+2 bytes, without its "\n" or "\r\n". A line that is too long is read to
// its end and given as such. At the end of input it returns io.EOF, after the last
// line if that has no line break.
func readViewLine(r *bufio.Reader) (viewLine, error) {
	b, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = r.ReadSlice('\n')
		}
		if err == io.EOF {
			err = nil // the next read reports the end of input
		}
		return viewLine{TooLong: true}, err
	}
	if err == io.EOF && len(b) > 0 {
		err = nil
	}
	if err != nil {
		return viewLine{}, err
	}
	text := strings.TrimSuffix(string(b), "\n")
	text = strings.TrimSuffix(text, "\r")
	if len(text) > maxViewLine {
		return viewLine{TooLong: true}, nil
	}
	return viewLine{Text: text}, nil
}

// viewConfig is the part of a view session that tests replace.
type viewConfig struct {
	opts  tui.Options
	delay time.Duration
	// after returns a channel that delivers once the delay has passed; nil means
	// time.After. A test passes its own to control the clock.
	after func(time.Duration) <-chan time.Time
}

// runView implements "pit view": read the replay and run the session on stdin and
// stdout. args excludes "view".
func runView(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var cfg viewConfig
	cols, rows, delayMS := tui.DefaultCols, tui.DefaultRows, 100
	fs := flag.NewFlagSet("pit view", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&cols, "cols", cols, "")
	fs.IntVar(&rows, "rows", rows, "")
	fs.IntVar(&delayMS, "delay", delayMS, "")

	// Flags and the file may come in any order, and "--" ends the flags.
	files, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, viewUsage)
			return 0
		}
		fmt.Fprintf(stderr, "pit view: %v\n\n%s", err, viewUsage)
		return 2
	}
	if len(files) != 1 {
		fmt.Fprint(stderr, viewUsage)
		return 2
	}
	for _, c := range []struct {
		name string
		v    int
	}{{"--cols", cols}, {"--rows", rows}} {
		if c.v < 1 {
			fmt.Fprintf(stderr, "pit view: %s must be at least 1\n\n%s", c.name, viewUsage)
			return 2
		}
	}
	if delayMS < 0 {
		fmt.Fprintf(stderr, "pit view: --delay must not be negative\n\n%s", viewUsage)
		return 2
	}
	cfg.opts = tui.Options{Cols: cols, Rows: rows}
	cfg.delay = time.Duration(delayMS) * time.Millisecond

	f, err := os.Open(files[0])
	if err != nil {
		fmt.Fprintf(stderr, "pit view: %v\n", err)
		return 1
	}
	r, err := replay.Read(f)
	f.Close()
	if err != nil {
		fmt.Fprintf(stderr, "pit view: %s: %v\n", files[0], err)
		return 1
	}
	m, err := tui.NewModel(r)
	if err != nil {
		fmt.Fprintf(stderr, "pit view: %s: %v\n", files[0], err)
		return 1
	}
	if err := viewSession(stdin, stdout, tui.NewPlayer(m), cfg); err != nil {
		fmt.Fprintf(stderr, "pit view: %v\n", err)
		return 1
	}
	return 0
}

// viewSession draws the player's frame, then reads commands from in until q or the
// end of input. While the player is playing, every cfg.delay it ticks the player
// and draws; a line of input pauses the play (an empty line or "p" only pauses) and
// otherwise runs as a command. At the end of input a counted play finishes first;
// any other play stops. It returns an error only if drawing or reading fails.
func viewSession(in io.Reader, out io.Writer, pl *tui.Player, cfg viewConfig) error {
	after := cfg.after
	if after == nil {
		after = time.After
	}
	lines := make(chan viewLine)
	stop := make(chan struct{})
	defer close(stop)
	var readErr error // set before lines is closed
	go func() {
		defer close(lines)
		r := bufio.NewReaderSize(in, maxViewLine+2)
		for {
			l, err := readViewLine(r)
			if err != nil {
				if err != io.EOF {
					readErr = err
				}
				return
			}
			select {
			case lines <- l:
			case <-stop:
				return
			}
		}
	}()

	var message string
	draw := func() error {
		return tui.Render(out, tui.Screen{Frame: pl.Frame(), Total: pl.Cycles(), Playing: pl.Playing(), Message: message}, cfg.opts)
	}
	if err := draw(); err != nil {
		return err
	}
	input := lines // set to nil at the end of input so that select ignores it
	for {
		var line viewLine
		if pl.Playing() {
			select {
			case l, ok := <-input:
				if !ok {
					if !pl.Counted() {
						return readErr
					}
					input = nil
					continue
				}
				line = l
			case <-after(cfg.delay):
				if _, err := pl.Tick(); err != nil {
					return err
				}
				if err := draw(); err != nil {
					return err
				}
				continue
			}
		} else {
			if input == nil {
				return readErr
			}
			l, ok := <-input
			if !ok {
				return readErr
			}
			line = l
		}

		wasPlaying := pl.Playing()
		fields := strings.Fields(line.Text)
		if wasPlaying {
			pl.Pause()
		}
		switch {
		case line.TooLong:
			message = fmt.Sprintf("line too long (more than %d bytes), ignored", maxViewLine)
		case len(fields) == 0 && wasPlaying, len(fields) == 1 && fields[0] == "p" && wasPlaying:
			// Only pauses.
		case len(fields) == 0:
			fields = []string{"s"}
			fallthrough
		default:
			quit, msg, err := viewCommand(pl, fields)
			if err != nil {
				return err
			}
			if quit {
				return nil
			}
			message = msg
		}
		if err := draw(); err != nil {
			return err
		}
	}
}

// viewCommand runs one command. It returns whether to quit and a notice for the
// screen; err is only a failure of the player.
func viewCommand(pl *tui.Player, fields []string) (quit bool, message string, err error) {
	cmd, rest := fields[0], fields[1:]
	count := func(def int) (int, bool) {
		if len(rest) == 0 {
			return def, true
		}
		n, err := strconv.Atoi(rest[0])
		return n, len(rest) == 1 && err == nil && n > 0
	}
	switch cmd {
	case "q":
		return true, "", nil
	case "s", "b":
		n, ok := count(1)
		if !ok {
			return false, cmd + " needs a positive number", nil
		}
		if cmd == "b" {
			n = -n
		}
		return false, "", pl.Step(n)
	case "g":
		if len(rest) != 1 {
			return false, "g needs a cycle number", nil
		}
		n, convErr := strconv.Atoi(rest[0])
		if convErr != nil {
			return false, "g needs a cycle number", nil
		}
		return false, "", pl.Seek(n)
	case "p":
		n, ok := count(0)
		if !ok {
			return false, "p needs a positive number", nil
		}
		pl.Play(n)
		return false, "", nil
	}
	return false, fmt.Sprintf("unknown command %q", cmd), nil
}
