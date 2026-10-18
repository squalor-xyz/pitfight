// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/tourney"
)

const tourneyUsage = `usage: pit tourney [flags] [--] <bot-directory>

Runs a round-robin tournament of the bots in a directory (every *.s file, sorted by
name) and prints a scoreboard. In every round each pair of bots plays twice, once in
each order, on a seed derived from the round number. A win scores 3 points, each bot
of a tied match 1 and a loss 0. The scoreboard is the same for the same bots, rounds
and configuration, whatever --jobs is. Flags and the directory may come in any order.

Flags:
  --rounds N          number of rounds, at least 1 (default 100)
  --jobs N            matches played at once, at least 1 (default: the number of CPUs)
  --core-size N       number of core cells, 1 to 1048576 (default 8000)
  --cycle-limit N     maximum number of cycles, 1 to 2147483647 (default 80000)
  --max-processes N   live processes allowed per bot, 1 to 1048576 (default 8000)
  --min-separation N  empty cells between neighbouring bots, 0 to 1048576 (default 100)
  --json              print the scoreboard as JSON lines, one object per bot
  --matches FILE      also write one JSON line per match to FILE

Exit codes: 0 the tournament ran; 1 an error in a bot file, the directory, the
configuration or FILE; 2 a usage error.
`

// runTourney implements "pit tourney": assemble the bots of a directory, play the
// tournament and print the scoreboard. args excludes "tourney".
func runTourney(args []string, stdout, stderr io.Writer) int {
	cfg := arena.DefaultConfig()
	rounds, jobs := 100, runtime.NumCPU()
	var asJSON bool
	var matchesPath string
	fs := flag.NewFlagSet("pit tourney", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&rounds, "rounds", rounds, "")
	fs.IntVar(&jobs, "jobs", jobs, "")
	fs.IntVar(&cfg.CoreSize, "core-size", cfg.CoreSize, "")
	fs.IntVar(&cfg.CycleLimit, "cycle-limit", cfg.CycleLimit, "")
	fs.IntVar(&cfg.MaxProcesses, "max-processes", cfg.MaxProcesses, "")
	fs.IntVar(&cfg.MinSeparation, "min-separation", cfg.MinSeparation, "")
	fs.BoolVar(&asJSON, "json", false, "")
	fs.StringVar(&matchesPath, "matches", "", "")

	// The flag package stops at the first non-flag argument; parse again after each
	// one so that "pit tourney bots/ --rounds 10" works.
	var dirs []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprint(stdout, tourneyUsage)
				return 0
			}
			fmt.Fprintf(stderr, "pit tourney: %v\n\n%s", err, tourneyUsage)
			return 2
		}
		rest := fs.Args()
		// Parse consumed a "--" if the last argument it took is one; everything after
		// it is a directory.
		if used := len(args) - len(rest); used > 0 && args[used-1] == "--" {
			dirs = append(dirs, rest...)
			break
		}
		if len(rest) == 0 {
			break
		}
		dirs = append(dirs, rest[0])
		args = rest[1:]
	}
	if len(dirs) != 1 {
		fmt.Fprint(stderr, tourneyUsage)
		return 2
	}
	if rounds < 1 || jobs < 1 {
		name, v := "--rounds", rounds
		if rounds >= 1 {
			name, v = "--jobs", jobs
		}
		fmt.Fprintf(stderr, "pit tourney: %s must be at least 1, got %d\n\n%s", name, v, tourneyUsage)
		return 2
	}

	bots, err := loadBots(dirs[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	results, err := tourney.Run(cfg, bots, rounds, jobs)
	if err != nil {
		fmt.Fprintf(stderr, "pit tourney: %v\n", err)
		return 1
	}
	standings := tourney.Score(bots, results)

	var out []byte
	if asJSON {
		out, err = tourney.FormatJSON(standings)
	} else {
		out = tourneyText(cfg, len(bots), rounds, len(results), standings)
	}
	if err == nil && matchesPath != "" {
		var recs []byte
		if recs, err = tourney.FormatMatches(bots, results); err == nil {
			err = os.WriteFile(matchesPath, recs, 0o644)
		}
	}
	if err == nil {
		_, err = stdout.Write(out)
	}
	if err != nil {
		fmt.Fprintf(stderr, "pit tourney: %v\n", err)
		return 1
	}
	return 0
}

// loadBots assembles every *.s file of dir, sorted by name. A bot is named after its
// file without the .s. Subdirectories and other files are ignored.
func loadBots(dir string) ([]tourney.Bot, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("pit tourney: %w", err)
	}
	// ReadDir returns the entries sorted by file name, and that order is the bot
	// order of the tournament.
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".s") && len(e.Name()) > len(".s") {
			names = append(names, e.Name())
		}
	}
	if len(names) < 2 {
		return nil, fmt.Errorf("pit tourney: need at least 2 bots (*.s files) in %s, found %d", dir, len(names))
	}
	bots := make([]tourney.Bot, len(names))
	for i, name := range names {
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("pit tourney: %w", err)
		}
		prog, err := asm.Assemble(path, src)
		if err != nil {
			return nil, err
		}
		bots[i] = tourney.Bot{Name: strings.TrimSuffix(name, ".s"), Bot: arena.Bot{Cells: prog.Cells, Entry: prog.Entry}}
	}
	return bots, nil
}

// tourneyText formats the header lines and the scoreboard table.
func tourneyText(cfg arena.Config, bots, rounds, matches int, st []tourney.Standing) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "tourney: %d bots, %d rounds, %d matches, core size %d, cycle limit %d, max processes %d, min separation %d\n",
		bots, rounds, matches, cfg.CoreSize, cfg.CycleLimit, cfg.MaxProcesses, cfg.MinSeparation)
	fmt.Fprintf(&b, "scoring: win %d, tie %d, loss %d\n", tourney.WinPoints, tourney.TiePoints, tourney.LossPoints)
	b.Write(tourney.FormatText(st))
	return b.Bytes()
}
