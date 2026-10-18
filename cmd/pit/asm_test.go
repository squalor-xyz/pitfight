// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/asm"
)

// bomberSrc and bomberListing are the example of docs/spec/asm.md section 9.
const bomberSrc = `; the bomber
start:  ADD.AB #4, ptr
        MOV.I  ptr, @ptr
        JMP    start
ptr:    DAT    #0, #0
`

const bomberListing = `ORG 0
ADD.AB #4, $3
MOV.I $2, @2
JMP.B $-2, $0
DAT.F #0, #0
`

func writeSrc(t *testing.T, name, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAsmCommand(t *testing.T) {
	path := writeSrc(t, "bomber.s", bomberSrc)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"asm", path}, &stdout, &stderr); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stdout.String() != bomberListing {
		t.Errorf("stdout = %q, want %q", stdout.String(), bomberListing)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestAsmCommandListing(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"entry offset, every sign and several modes", `
        ORG b
        NOP
b:      MOV.X  #-1, {2
        SPL.BA <3, >-4
        DJN.I  @1, *2
        SEQ.A  }5, $0
`, `ORG 1
NOP.F $0, $0
MOV.X #-1, {2
SPL.BA <3, >-4
DJN.I @1, *2
SEQ.A }5, $0
`},
		{"values are not normalized", "DAT.F #-8000, #2147483647\n", "ORG 0\nDAT.F #-8000, #2147483647\n"},
		{"default modifier is printed", "MOV 0, 1\n", "ORG 0\nMOV.I $0, $1\n"},
		{"source without a final line feed", "DAT", "ORG 0\nDAT.F $0, $0\n"},
	}
	for _, tt := range tests {
		path := writeSrc(t, "x.s", tt.src)
		var stdout, stderr bytes.Buffer
		if code := run([]string{"asm", path}, &stdout, &stderr); code != 0 {
			t.Errorf("%s: exit code = %d, want 0 (stderr %q)", tt.name, code, stderr.String())
		}
		if stdout.String() != tt.want {
			t.Errorf("%s: stdout = %q, want %q", tt.name, stdout.String(), tt.want)
		}
	}
}

func TestAsmCommandErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.s")
	if err := os.WriteFile(bad, []byte("MOV.I $0, $1\nJMP nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A path that is not in its clean form must be printed exactly as given.
	unclean := dir + "/./sub/../bad.s"
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string // exact, or a prefix when wantPrefix
		wantPrefix bool
	}{
		{"malformed file", []string{"asm", bad}, 1, bad + ":2:5: undefined label \"nowhere\"\n", false},
		{"path is printed as given", []string{"asm", unclean}, 1, unclean + ":2:5: undefined label \"nowhere\"\n", false},
		{"missing file", []string{"asm", filepath.Join(dir, "missing.s")}, 1, "pit asm: open " + filepath.Join(dir, "missing.s") + ":", true},
		{"no file", []string{"asm"}, 2, "usage: pit asm <file>\n", false},
		{"two files", []string{"asm", bad, bad}, 2, "usage: pit asm <file>\n", false},
		{"option", []string{"asm", "-x"}, 2, "usage: pit asm <file>\n", false},
		{"help option", []string{"asm", "--help"}, 2, "usage: pit asm <file>\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			got := stderr.String()
			if tt.wantPrefix {
				if !strings.HasPrefix(got, tt.wantStderr) {
					t.Errorf("stderr = %q, want prefix %q", got, tt.wantStderr)
				}
			} else if got != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", got, tt.wantStderr)
			}
		})
	}
}

// TestAsmCommandTestdata runs the malformed program used by the slice's check.
func TestAsmCommandTestdata(t *testing.T) {
	const path = "../../internal/asm/testdata/undefined_label.s"
	var stdout, stderr bytes.Buffer
	code := run([]string{"asm", path}, &stdout, &stderr)
	want := path + ":3:16: undefined label \"nowhere\"\n"
	if code != 1 || stdout.Len() != 0 || stderr.String() != want {
		t.Errorf("code %d, stdout %q, stderr %q; want 1, empty, %q", code, stdout.String(), stderr.String(), want)
	}
}

// TestAsmCommandRoundTrip: pit asm is the disassembly entry point
// (docs/spec/asm.md section 9). Its output on a file is exactly asm.Disassemble of
// the assembled program, and assembles back to the same cells and entry offset.
func TestAsmCommandRoundTrip(t *testing.T) {
	srcs := map[string]string{
		"bomber": bomberSrc,
		"entry and every mode": `
        ORG b
        NOP
b:      MOV.X  #-1, {2
        SPL.BA <3, >-4
        DJN.I  @1, *2
        SEQ.A  }5, $0
        DAT    #-2147483648, #2147483647
`,
	}
	for name, src := range srcs {
		t.Run(name, func(t *testing.T) {
			path := writeSrc(t, "bot.s", src)
			prog, err := asm.Assemble(path, []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := run([]string{"asm", path}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit code = %d, stderr %q", code, stderr.String())
			}
			if want := asm.Disassemble(prog); !bytes.Equal(stdout.Bytes(), want) {
				t.Errorf("stdout = %q, want asm.Disassemble output %q", stdout.String(), want)
			}
			// The printed source, written to a file, reassembles to the same program.
			back, err := asm.Assemble("listing", stdout.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if back.Entry != prog.Entry || !slices.Equal(back.Cells, prog.Cells) {
				t.Errorf("reassembled %+v entry %d, want %+v entry %d", back.Cells, back.Entry, prog.Cells, prog.Entry)
			}
		})
	}
}

// TestSpecListing: the asm block of docs/spec/asm.md section 9 is followed by a
// text block with the listing that pit asm prints for it.
func TestSpecListing(t *testing.T) {
	data, err := os.ReadFile("../../docs/spec/asm.md")
	if err != nil {
		t.Fatal(err)
	}
	asmBlocks := fencedBlocks(string(data), "asm")
	if len(asmBlocks) == 0 {
		t.Fatal("no asm block in asm.md")
	}
	var found bool
	for _, b := range asmBlocks {
		if strings.HasPrefix(b, "; the bomber of isa.md section 8\n") {
			found = true
			path := writeSrc(t, "doc.s", b)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"asm", path}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit %d: %s", code, stderr.String())
			}
			listings := fencedBlocks(string(data), "text")
			if !slices.Contains(listings, stdout.String()) {
				t.Errorf("asm.md has no text block equal to the listing %q", stdout.String())
			}
		}
	}
	if !found {
		t.Error("the bomber example is missing from asm.md")
	}
}

func TestUsageListsAsm(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	for _, want := range []string{
		"\n  asm      assemble a bot source file\n",
		"\n  run      run a match between bots (not implemented yet)\n",
		"1 an error in a command",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("usage does not contain %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "planned") {
		t.Errorf("usage still says planned:\n%s", stdout.String())
	}
}

// fencedBlocks returns the contents of the fenced blocks whose info string is tag.
func fencedBlocks(doc, tag string) []string {
	var out, cur []string
	in, match := false, false
	for _, line := range strings.Split(doc, "\n") {
		switch {
		case !in && strings.HasPrefix(line, "```"):
			in, match, cur = true, strings.TrimSpace(strings.TrimPrefix(line, "```")) == tag, nil
		case in && strings.HasPrefix(line, "```"):
			if match {
				out = append(out, strings.Join(cur, "\n")+"\n")
			}
			in = false
		case in:
			cur = append(cur, line)
		}
	}
	return out
}
