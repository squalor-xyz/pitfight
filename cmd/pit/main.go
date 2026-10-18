// SPDX-License-Identifier: AGPL-3.0-or-later

// Command pit is the pitfight command line: assemble bots, run matches, run
// tournaments and view replays. This file holds usage handling and dispatch; the
// subcommands live in their own files and arrive one slice at a time.
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: pit <command> [arguments]

pit assembles pitfight bots, runs matches and tournaments, and views replays.

Commands:
  asm      assemble a bot source file
  run      run a match between bots
  tourney  run a round-robin tournament (not implemented yet)
  view     play back a recorded replay (not implemented yet)
  help     print this text

Exit codes: 0 success; 1 an error in a command; 2 a usage error.
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
	case "asm":
		return runAsm(args[1:], stdout, stderr)
	case "run":
		return runRun(args[1:], stdout, stderr)
	case "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "pit: unknown subcommand %q\n\n%s", args[0], usage)
		return 2
	}
}
