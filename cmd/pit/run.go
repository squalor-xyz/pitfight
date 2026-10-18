// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/replay"
)

const runUsage = `usage: pit run [flags] [--] <bot.s> <bot.s> [<bot.s> ...]

Assembles the bots, runs one match and prints the result. Bots get the ids 1, 2,
... in the order given. Flags and files may come in any order; every argument
after -- is a file, even one that starts with a dash. A flag that takes a value takes
the next argument as it, even if that is --.

Flags:
  --seed N            placement seed, 0 to 2^64-1 (default 0)
  --core-size N       number of core cells, 1 to 1048576 (default 8000)
  --cycle-limit N     maximum number of cycles, 1 to 2147483647 (default 80000)
  --max-processes N   live processes allowed per bot, 1 to 1048576 (default 8000)
  --min-separation N  empty cells between neighbouring bots, 0 to 1048576 (default 100)
  --json              print the result as one line of JSON instead of text
  --replay FILE       also write the replay (docs/spec/replay.md) to FILE and print its
                      hash (written to a temporary file first, so a failed write
                      leaves no partial FILE)

Exit codes: 0 the match ran; 1 an error in a bot file or the configuration; 2 a usage error.
`

// runRun implements "pit run": assemble the bot files, run a match and print its
// result. args excludes "run".
func runRun(args []string, stdout, stderr io.Writer) int {
	cfg := arena.DefaultConfig()
	var asJSON bool
	var replayPath string
	fs := flag.NewFlagSet("pit run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Uint64Var(&cfg.Seed, "seed", cfg.Seed, "")
	fs.IntVar(&cfg.CoreSize, "core-size", cfg.CoreSize, "")
	fs.IntVar(&cfg.CycleLimit, "cycle-limit", cfg.CycleLimit, "")
	fs.IntVar(&cfg.MaxProcesses, "max-processes", cfg.MaxProcesses, "")
	fs.IntVar(&cfg.MinSeparation, "min-separation", cfg.MinSeparation, "")
	fs.BoolVar(&asJSON, "json", false, "")
	fs.StringVar(&replayPath, "replay", "", "")

	// Flags and files may come in any order, and "--" ends the flags.
	files, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, runUsage)
			return 0
		}
		fmt.Fprintf(stderr, "pit run: %v\n\n%s", err, runUsage)
		return 2
	}
	if len(files) == 0 {
		fmt.Fprint(stderr, runUsage)
		return 2
	}

	bots, ok := assembleFiles("pit run", files, stderr)
	if !ok {
		return 1
	}
	var (
		res        arena.Result
		replayHash string
	)
	if replayPath == "" {
		res, _, err = arena.Run(cfg, bots)
	} else {
		var rec *replay.Replay
		if rec, err = replay.Record(cfg, bots); err == nil {
			res = rec.Result
			data := rec.Bytes()
			replayHash = replay.Hash(data)
			err = writeFileAtomic(replayPath, 0o644, func(w io.Writer) error { _, err := w.Write(data); return err })
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "pit run: %v\n", err)
		return 1
	}
	var out []byte
	if asJSON {
		out, err = runJSON(cfg, files, bots, res, replayHash)
	} else {
		out = runText(cfg, files, bots, res, replayHash)
	}
	if err == nil {
		_, err = stdout.Write(out)
	}
	if err != nil {
		fmt.Fprintf(stderr, "pit run: %v\n", err)
		return 1
	}
	return 0
}

// assembleFiles reads and assembles the bot files, in order. On an error it writes the
// message to stderr (an assembler error as the assembler words it, with file, line and
// column; any other error prefixed with the command name) and reports false.
func assembleFiles(command string, files []string, stderr io.Writer) ([]arena.Bot, bool) {
	bots := make([]arena.Bot, len(files))
	for i, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", command, err)
			return nil, false
		}
		prog, err := asm.Assemble(path, src)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return nil, false
		}
		bots[i] = arena.Bot{Cells: prog.Cells, Entry: prog.Entry}
	}
	return bots, true
}

// entryAddress is the address of a bot's first process.
func entryAddress(cfg arena.Config, start, entry int) int {
	return (start + entry) % cfg.CoreSize
}

// runText formats the result as text: a configuration line, one line per bot, then
// cycles, survivors and the result, and the replay hash if a replay was written.
func runText(cfg arena.Config, files []string, bots []arena.Bot, res arena.Result, replayHash string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "match: %d bots, seed %d, core size %d, cycle limit %d, max processes %d, min separation %d\n",
		len(bots), cfg.Seed, cfg.CoreSize, cfg.CycleLimit, cfg.MaxProcesses, cfg.MinSeparation)
	for i, bot := range bots {
		start := res.Placement.Starts[i]
		fmt.Fprintf(&b, "bot %d: %s (length %d, start %d, entry_address %d)\n",
			i+1, files[i], len(bot.Cells), start, entryAddress(cfg, start, bot.Entry))
	}
	fmt.Fprintf(&b, "cycles: %d\n", res.Cycles)
	b.WriteString("survivors:")
	if len(res.Survivors) == 0 {
		b.WriteString(" none")
	}
	for _, id := range res.Survivors {
		b.WriteString(" " + strconv.FormatUint(uint64(id), 10))
	}
	b.WriteString("\n")
	if res.Outcome == arena.Win {
		fmt.Fprintf(&b, "result: win bot %d\n", res.Winner)
	} else {
		b.WriteString("result: tie between bots")
		for i, id := range res.Tied {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(" " + strconv.FormatUint(uint64(id), 10))
		}
		b.WriteString("\n")
	}
	if replayHash != "" {
		fmt.Fprintf(&b, "replay: %s\n", replayHash)
	}
	return b.Bytes()
}

// The JSON result is one line. Field order is fixed by these structs, and the
// lists are never null.
type jsonConfig struct {
	CoreSize      int    `json:"core_size"`
	CycleLimit    int    `json:"cycle_limit"`
	MaxProcesses  int    `json:"max_processes"`
	MinSeparation int    `json:"min_separation"`
	Seed          string `json:"seed"` // a decimal string: JavaScript numbers lose precision above 2^53
}

type jsonBot struct {
	ID           int    `json:"id"`
	File         string `json:"file"`
	Length       int    `json:"length"`
	Start        int    `json:"start"`
	EntryAddress int    `json:"entry_address"`
}

type jsonResult struct {
	Config    jsonConfig `json:"config"`
	Bots      []jsonBot  `json:"bots"`
	Cycles    int        `json:"cycles"`
	Survivors []uint32   `json:"survivors"`
	Outcome   string     `json:"outcome"`
	Winner    uint32     `json:"winner"`
	Tied      []uint32   `json:"tied"`
	// ReplayHash is present only when --replay was given.
	ReplayHash string `json:"replay_hash,omitempty"`
}

func runJSON(cfg arena.Config, files []string, bots []arena.Bot, res arena.Result, replayHash string) ([]byte, error) {
	out := jsonResult{
		Config: jsonConfig{cfg.CoreSize, cfg.CycleLimit, cfg.MaxProcesses, cfg.MinSeparation, strconv.FormatUint(cfg.Seed, 10)},
		Bots:   make([]jsonBot, len(bots)),
		Cycles: res.Cycles,
		// Empty lists, not null.
		Survivors:  append([]uint32{}, res.Survivors...),
		Outcome:    res.Outcome.String(),
		Winner:     res.Winner,
		Tied:       append([]uint32{}, res.Tied...),
		ReplayHash: replayHash,
	}
	for i, bot := range bots {
		start := res.Placement.Starts[i]
		out.Bots[i] = jsonBot{i + 1, files[i], len(bot.Cells), start, entryAddress(cfg, start, bot.Entry)}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
