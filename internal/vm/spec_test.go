// SPDX-License-Identifier: AGPL-3.0-or-later
package vm

import (
	"github.com/squalor-xyz/pitfight/internal/isa"
	"os"
	"slices"
	"strings"
	"testing"
)

// This parser deliberately follows the normative table headers (C50), rather
// than treating worked cases as an enumeration of the legal vocabulary.
func TestSpecEnumeration(t *testing.T) {
	data, err := os.ReadFile("../../docs/spec/isa.md")
	if err != nil {
		t.Fatal(err)
	}
	var tables [][]string
	var cases []string
	var current []string
	inTable := false
	inCases := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "| Mnemonic |") {
			if inTable {
				t.Fatal("nested table")
			}
			inTable = true
			current = nil
			continue
		}
		if strings.HasPrefix(line, "| Case |") {
			inCases = true
			continue
		}
		if !strings.HasPrefix(line, "|") {
			if inTable {
				tables = append(tables, current)
				inTable = false
			}
			inCases = false
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 3 {
			t.Fatal(line)
		}
		v := strings.TrimSpace(cols[1])
		if strings.HasPrefix(v, "---") {
			continue
		}
		if len(v) < 2 || v[0] != '`' || v[len(v)-1] != '`' {
			if inTable || inCases {
				t.Fatalf("bad mnemonic: %s", line)
			}
			continue
		}
		v = strings.Trim(v, "`")
		if inTable {
			current = append(current, v)
		}
		if inCases {
			cases = append(cases, v)
		}
	}
	if len(tables) != 3 {
		t.Fatalf("got %d vocabulary tables", len(tables))
	}
	var modes, mods, ops []string
	for _, v := range isa.Modes {
		modes = append(modes, v.String())
	}
	for _, v := range isa.Modifiers {
		mods = append(mods, v.String())
	}
	for _, v := range isa.Opcodes {
		ops = append(ops, v.String())
	}
	for i, want := range [][]string{modes, mods, ops} {
		if !slices.Equal(tables[i], want) {
			t.Errorf("table %d: %v want %v", i, tables[i], want)
		}
	}
	var names []string
	for _, tc := range stepCases {
		names = append(names, tc.name)
	}
	if !slices.Equal(cases, names) {
		t.Errorf("spec worked cases %v differ from test cases %v", cases, names)
	}
	for _, op := range ops {
		found := false
		for _, tc := range stepCases {
			if strings.HasPrefix(tc.name, op+".") {
				found = true
			}
		}
		if !found {
			t.Errorf("opcode %s lacks row", op)
		}
	}
	for _, mod := range mods {
		if !slices.Contains(names, "MOV."+mod) {
			t.Errorf("modifier %s lacks row", mod)
		}
	}
	for _, mode := range modes {
		if !slices.Contains(names, mode) {
			t.Errorf("mode %s lacks row", mode)
		}
	}
}
