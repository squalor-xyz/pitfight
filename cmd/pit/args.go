// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"flag"
	"strings"
)

// parseInterleaved parses args into fs, allowing flags and positional arguments in
// any order, and returns the positional arguments in order. The flag package stops at
// the first positional argument, so this splits args first, with the rules of that
// package: an argument that starts with "-" and is longer than "-" is a flag; a flag
// that is not a bool and has no "=value" takes the next argument as its value, even
// if that is "--" or starts with a dash; the first "--" in a flag position ends the
// flags, and every argument after it is positional. A "--" taken as a flag's value
// does not end the flags. Unknown flags and flags without a value are left for
// fs.Parse to report.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case len(a) < 2 || a[0] != '-':
			positional = append(positional, a)
		default:
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if !strings.Contains(a, "=") && takesValue(fs, name) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		}
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return positional, nil
}

// takesValue reports whether the flag called name is defined and is not a bool flag,
// so that it takes the next argument as its value.
func takesValue(fs *flag.FlagSet, name string) bool {
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return !ok || !b.IsBoolFlag()
}
