// SPDX-License-Identifier: AGPL-3.0-or-later

// Command pit is the pitfight command line: assemble bots, run matches, run
// tournaments and view replays. This file holds only usage handling; the
// subcommands arrive with later slices.
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: pit <command> [arguments]

pit assembles pitfight bots, runs matches and tournaments, and views replays.

Commands (planned; not implemented yet):
  asm      assemble a bot source file
  run      run a match between bots
  tourney  run a round-robin tournament
  view     play back a recorded replay
  help     print this text

Exit codes: 0 success; 2 a usage error.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one pit invocation and returns the process exit code. args
// excludes the program name.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "pit: unknown subcommand %q\n\n%s", args[0], usage)
		return 2
	}
}
