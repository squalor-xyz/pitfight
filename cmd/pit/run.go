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
)

const runUsage = `usage: pit run [flags] [--] <bot.s> <bot.s> [<bot.s> ...]

Assembles the bots, runs one match and prints the result. Bots get the ids 1, 2,
... in the order given. Flags and files may come in any order; every argument
after -- is a file, even one that starts with a dash.

Flags:
  --seed N            placement seed, 0 to 2^64-1 (default 0)
  --core-size N       number of core cells, 1 to 1048576 (default 8000)
  --cycle-limit N     maximum number of cycles, 1 to 2147483647 (default 80000)
  --max-processes N   live processes allowed per bot, 1 to 1048576 (default 8000)
  --min-separation N  empty cells between neighbouring bots, 0 to 1048576 (default 100)
  --json              print the result as one line of JSON instead of text

Exit codes: 0 the match ran; 1 an error in a bot file or the configuration; 2 a usage error.
`

// runRun implements "pit run": assemble the bot files, run a match and print its
// result. args excludes "run".
func runRun(args []string, stdout, stderr io.Writer) int {
	cfg := arena.DefaultConfig()
	var asJSON bool
	fs := flag.NewFlagSet("pit run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Uint64Var(&cfg.Seed, "seed", cfg.Seed, "")
	fs.IntVar(&cfg.CoreSize, "core-size", cfg.CoreSize, "")
	fs.IntVar(&cfg.CycleLimit, "cycle-limit", cfg.CycleLimit, "")
	fs.IntVar(&cfg.MaxProcesses, "max-processes", cfg.MaxProcesses, "")
	fs.IntVar(&cfg.MinSeparation, "min-separation", cfg.MinSeparation, "")
	fs.BoolVar(&asJSON, "json", false, "")

	// The flag package stops at the first file name; parse again after each one so
	// that "pit run a.s b.s --seed 1" works.
	var files []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprint(stdout, runUsage)
				return 0
			}
			fmt.Fprintf(stderr, "pit run: %v\n\n%s", err, runUsage)
			return 2
		}
		rest := fs.Args()
		// Parse consumed a "--" if the last argument it took is one; a flag value
		// cannot be "--" because no flag accepts it. Everything after it is a file.
		if used := len(args) - len(rest); used > 0 && args[used-1] == "--" {
			files = append(files, rest...)
			break
		}
		if len(rest) == 0 {
			break
		}
		files = append(files, rest[0])
		args = rest[1:]
	}
	if len(files) == 0 {
		fmt.Fprint(stderr, runUsage)
		return 2
	}

	bots := make([]arena.Bot, len(files))
	for i, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "pit run: %v\n", err)
			return 1
		}
		prog, err := asm.Assemble(path, src)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		bots[i] = arena.Bot{Cells: prog.Cells, Entry: prog.Entry}
	}
	res, _, err := arena.Run(cfg, bots)
	if err != nil {
		fmt.Fprintf(stderr, "pit run: %v\n", err)
		return 1
	}
	var out []byte
	if asJSON {
		out, err = runJSON(cfg, files, bots, res)
	} else {
		out = runText(cfg, files, bots, res)
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

// entryAddress is the address of a bot's first process.
func entryAddress(cfg arena.Config, start, entry int) int {
	return (start + entry) % cfg.CoreSize
}

// runText formats the result as text: a configuration line, one line per bot, then
// cycles, survivors and the result.
func runText(cfg arena.Config, files []string, bots []arena.Bot, res arena.Result) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "match: %d bots, seed %d, core size %d, cycle limit %d, max processes %d, min separation %d\n",
		len(bots), cfg.Seed, cfg.CoreSize, cfg.CycleLimit, cfg.MaxProcesses, cfg.MinSeparation)
	for i, bot := range bots {
		start := res.Placement.Starts[i]
		fmt.Fprintf(&b, "bot %d: %s (length %d, start %d, entry %d)\n",
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
	return b.Bytes()
}

// The JSON result is one line. Field order is fixed by these structs, and the
// lists are never null.
type jsonConfig struct {
	CoreSize      int    `json:"core_size"`
	CycleLimit    int    `json:"cycle_limit"`
	MaxProcesses  int    `json:"max_processes"`
	MinSeparation int    `json:"min_separation"`
	Seed          uint64 `json:"seed"`
}

type jsonBot struct {
	ID     int    `json:"id"`
	File   string `json:"file"`
	Length int    `json:"length"`
	Start  int    `json:"start"`
	Entry  int    `json:"entry"`
}

type jsonResult struct {
	Config    jsonConfig `json:"config"`
	Bots      []jsonBot  `json:"bots"`
	Cycles    int        `json:"cycles"`
	Survivors []uint32   `json:"survivors"`
	Outcome   string     `json:"outcome"`
	Winner    uint32     `json:"winner"`
	Tied      []uint32   `json:"tied"`
}

func runJSON(cfg arena.Config, files []string, bots []arena.Bot, res arena.Result) ([]byte, error) {
	out := jsonResult{
		Config: jsonConfig{cfg.CoreSize, cfg.CycleLimit, cfg.MaxProcesses, cfg.MinSeparation, cfg.Seed},
		Bots:   make([]jsonBot, len(bots)),
		Cycles: res.Cycles,
		// Empty lists, not null.
		Survivors: append([]uint32{}, res.Survivors...),
		Outcome:   res.Outcome.String(),
		Winner:    res.Winner,
		Tied:      append([]uint32{}, res.Tied...),
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
