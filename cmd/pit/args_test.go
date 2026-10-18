// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"flag"
	"io"
	"slices"
	"testing"
)

// TestParseInterleaved: flags and positional arguments in any order; "--" ends the
// flags only where a flag could start, and is a value where a flag takes a value.
func TestParseInterleaved(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		want   []string // positional arguments
		replay string
		seed   int
		json   bool
	}{
		{"none", nil, nil, "", 0, false},
		{"flags first", []string{"--seed", "3", "a", "b"}, []string{"a", "b"}, "", 3, false},
		{"flags last", []string{"a", "b", "--seed", "3", "--json"}, []string{"a", "b"}, "", 3, true},
		{"flags between", []string{"a", "--seed=4", "b", "-json"}, []string{"a", "b"}, "", 4, true},
		{"single dash is positional", []string{"-", "a"}, []string{"-", "a"}, "", 0, false},
		{"dashdash ends the flags", []string{"--seed", "1", "--", "--json", "a"}, []string{"--json", "a"}, "", 1, false},
		{"dashdash after a positional", []string{"a", "--", "--seed", "b"}, []string{"a", "--seed", "b"}, "", 0, false},
		{"second dashdash is positional", []string{"a", "--", "--", "b"}, []string{"a", "--", "b"}, "", 0, false},
		{"dashdash is a value", []string{"--replay", "--", "a", "--seed", "2"}, []string{"a"}, "--", 2, false},
		{"dashdash is a value after a positional", []string{"a", "--replay", "--", "b", "--json"}, []string{"a", "b"}, "--", 0, true},
		{"dashdash is an attached value", []string{"--replay=--", "a", "--seed", "2"}, []string{"a"}, "--", 2, false},
		{"dashdash value then a real terminator", []string{"--replay", "--", "--", "--seed"}, []string{"--seed"}, "--", 0, false},
		{"value that looks like a flag", []string{"--replay", "--json", "a"}, []string{"a"}, "--json", 0, false},
		{"bool flag takes no value", []string{"--json", "--", "a"}, []string{"a"}, "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var replay string
			var seed int
			var asJSON bool
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			fs.StringVar(&replay, "replay", "", "")
			fs.IntVar(&seed, "seed", 0, "")
			fs.BoolVar(&asJSON, "json", false, "")
			got, err := parseInterleaved(fs, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) || replay != tt.replay || seed != tt.seed || asJSON != tt.json {
				t.Errorf("positional %q, replay %q, seed %d, json %v; want %q, %q, %d, %v",
					got, replay, seed, asJSON, tt.want, tt.replay, tt.seed, tt.json)
			}
		})
	}
}

func TestParseInterleavedErrors(t *testing.T) {
	newSet := func() *flag.FlagSet {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fs.String("replay", "", "")
		fs.Int("seed", 0, "")
		return fs
	}
	for _, args := range [][]string{
		{"a", "--replay"},     // a flag at the end needs its value
		{"--nope", "a"},       // unknown flag
		{"a", "--seed", "x"},  // bad value
		{"a", "--seed", "--"}, // "--" is the value, and not a number
	} {
		if _, err := parseInterleaved(newSet(), args); err == nil {
			t.Errorf("%q: no error", args)
		}
	}
	for _, h := range []string{"-h", "--help", "-help"} {
		if _, err := parseInterleaved(newSet(), []string{"a", h}); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("%s: error %v, want flag.ErrHelp", h, err)
		}
	}
}
