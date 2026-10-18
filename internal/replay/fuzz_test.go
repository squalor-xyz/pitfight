// SPDX-License-Identifier: AGPL-3.0-or-later

package replay

import (
	"bytes"
	"testing"
)

// FuzzRead: arbitrary input never panics. It is either refused with an error and
// no replay, or accepted, and then it is exactly the file the writer would produce
// for the parsed values, can be played to its end, and reads back the same.
func FuzzRead(f *testing.F) {
	for _, name := range []string{"imp_dwarf_seed1.jsonl", "win_death.jsonl"} {
		f.Add(fixture(f, name))
	}
	f.Add([]byte{})
	f.Add([]byte("\n"))
	f.Add([]byte(`{"t":"header","version":2}` + "\n"))
	f.Add([]byte(`{"t":"header","version":1}` + "\n"))
	f.Add([]byte(`{"t":"header","version":1,"config":{"core_size":16,"cycle_limit":5,"max_processes":8,"min_separation":0,"seed":"1"},"bots":[]}` + "\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := Read(bytes.NewReader(data))
		if err != nil {
			if r != nil {
				t.Fatal("an error came with a replay")
			}
			return
		}
		if !bytes.Equal(r.Bytes(), data) {
			t.Fatalf("accepted input differs from what the writer produces:\n%q\n%q", data, r.Bytes())
		}
		p, err := NewPlayer(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.AdvanceTo(r.Result.Cycles); err != nil {
			t.Fatal(err)
		}
	})
}
