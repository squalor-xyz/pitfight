// SPDX-License-Identifier: AGPL-3.0-or-later

package acceptance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/isa"
	"github.com/squalor-xyz/pitfight/internal/replay"
	"github.com/squalor-xyz/pitfight/internal/tui"
	"github.com/squalor-xyz/pitfight/internal/vm"
)

// repoRoot is the module root, seen from this package's directory (the working
// directory of go test). The tests never change directory; the pit binary is run
// with this as its working directory.
const repoRoot = "../.."

// pitBin is the pit binary built by TestMain from ./cmd/pit.
var pitBin string

func TestMain(m *testing.M) { os.Exit(testMain(m)) }

func testMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "pit-acceptance-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	goTool, err := exec.LookPath("go")
	if err != nil {
		goTool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	pitBin = filepath.Join(dir, "pit")
	if runtime.GOOS == "windows" {
		pitBin += ".exe"
	}
	build := exec.Command(goTool, "build", "-o", pitBin, "./cmd/pit")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building pit: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// pitResult is one finished run of the pit binary.
type pitResult struct {
	code           int
	stdout, stderr string
}

// pit runs the built binary with the repository root as its working directory, so
// "bots/imp.s" names the starter bot.
func pit(t *testing.T, args ...string) pitResult {
	t.Helper()
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(pitBin, args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	res := pitResult{}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("pit %v: %v", args, err)
		}
		res.code = ee.ExitCode()
	}
	res.stdout, res.stderr = stdout.String(), stderr.String()
	return res
}

func readFile(t testing.TB, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func logMachine(t *testing.T) {
	t.Helper()
	t.Logf("machine: %s/%s, %s", runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// cell strips one pair of backticks from a spec table cell.
func cell(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1]
	}
	return s
}

// tableRows returns the rows of every table of doc whose header row starts with
// header (for example "| Case |"), each row split into its trimmed columns. One
// slice per table.
func tableRows(doc, header string) [][][]string {
	var tables [][][]string
	var cur [][]string
	in := false
	for _, line := range strings.Split(doc, "\n") {
		switch {
		case strings.HasPrefix(line, header):
			in, cur = true, nil
		case in && !strings.HasPrefix(line, "|"):
			tables = append(tables, cur)
			in = false
		case in:
			cols := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
			if strings.HasPrefix(strings.TrimSpace(cols[0]), "---") {
				continue
			}
			for i := range cols {
				cols[i] = strings.TrimSpace(cols[i])
			}
			cur = append(cur, cols)
		}
	}
	if in {
		tables = append(tables, cur)
	}
	return tables
}

// fencedBlocks returns the contents of the fenced blocks of doc whose info string is tag.
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

// ---- C15: the spec covers and tests every opcode, modifier and addressing mode ----

// specCase is one row of the worked cases of docs/spec/isa.md section 6.
type specCase struct{ id, setup, before, after, queue string }

// TestC15SpecCoverage parses the three vocabulary tables and the worked cases of
// docs/spec/isa.md the way S06's TestSpecEnumeration does (that parser lives in
// package vm's tests and cannot be imported), then checks that
//   - the tables list exactly the opcodes, modifiers and modes of internal/isa;
//   - every opcode, every modifier and every mode has a worked case;
//   - every worked case produces, on internal/vm, the writes and queue the spec
//     table says, so the spec is not merely present but true;
//   - internal/vm's own test table has a row for each worked case.
func TestC15SpecCoverage(t *testing.T) {
	doc := readFile(t, "docs/spec/isa.md")

	vocab := tableRows(doc, "| Mnemonic |")
	if len(vocab) != 3 {
		t.Fatalf("got %d vocabulary tables, want 3 (modes, modifiers, opcodes)", len(vocab))
	}
	var want [3][]string
	for _, v := range isa.Modes {
		want[0] = append(want[0], v.String())
	}
	for _, v := range isa.Modifiers {
		want[1] = append(want[1], v.String())
	}
	for _, v := range isa.Opcodes {
		want[2] = append(want[2], v.String())
	}
	var specNames [3][]string
	for i, rows := range vocab {
		for _, r := range rows {
			specNames[i] = append(specNames[i], cell(r[0]))
		}
		if !slices.Equal(specNames[i], want[i]) {
			t.Errorf("vocabulary table %d lists %v, internal/isa has %v", i, specNames[i], want[i])
		}
	}

	var cases []specCase
	for _, rows := range tableRows(doc, "| Case |") {
		for _, r := range rows {
			if len(r) != 5 {
				t.Fatalf("worked case row has %d columns: %v", len(r), r)
			}
			cases = append(cases, specCase{cell(r[0]), cell(r[1]), cell(r[2]), cell(r[3]), cell(r[4])})
		}
	}
	var ids []string
	for _, c := range cases {
		ids = append(ids, c.id)
	}

	for _, op := range specNames[2] {
		if !slices.ContainsFunc(ids, func(id string) bool { return strings.HasPrefix(id, op+".") }) {
			t.Errorf("opcode %s has no worked case", op)
		}
	}
	for _, mod := range specNames[1] {
		if !slices.Contains(ids, "MOV."+mod) {
			t.Errorf("modifier %s has no worked case (MOV.%s)", mod, mod)
		}
	}
	for _, mode := range specNames[0] {
		if !slices.Contains(ids, mode) {
			t.Errorf("addressing mode %s has no worked case", mode)
		}
	}

	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) { runSpecCase(t, c) })
	}

	// The case ids are the names of the rows of internal/vm's step table.
	src := readFile(t, "internal/vm/exec_test.go")
	var vmNames []string
	for _, m := range regexp.MustCompile(`(?m)^\t\{"([^"]+)", "N=`).FindAllStringSubmatch(src, -1) {
		vmNames = append(vmNames, m[1])
	}
	if !slices.Equal(vmNames, ids) {
		t.Errorf("internal/vm stepCases rows %v differ from the spec's worked cases %v", vmNames, ids)
	}
}

// runSpecCase sets up the core of a worked case, executes one step of bot 1 and
// compares the writes (as events, in order), the death and the queue with the
// table. Cell values in the table are stored values, so nothing is normalized.
func runSpecCase(t *testing.T, c specCase) {
	n, maxProcs := 8, 8
	for _, kv := range strings.Split(c.setup, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(kv), "=")
		x, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("setup %q: %v", c.setup, err)
		}
		switch k {
		case "N":
			n = x
		case "MAXPROCESSES":
			maxProcs = x
		default:
			t.Fatalf("setup %q: unknown key %q", c.setup, k)
		}
	}
	core, err := vm.NewCore(n)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range specWrites(t, c.before) {
		core.Write(w.addr, specCell(t, w.text), 0)
	}
	before := make([]isa.Cell, n)
	for i := range before {
		before[i] = core.Read(int64(i))
	}
	q, err := core.NewQueue(maxProcs, 0)
	if err != nil {
		t.Fatal(err)
	}

	events := core.Step(q, 1, nil)

	var wantWrites []vm.Event
	final := slices.Clone(before)
	for _, w := range specWrites(t, c.after) {
		cl := specCell(t, w.text)
		cl.Writer = 1
		wantWrites = append(wantWrites, vm.Event{Kind: vm.CellWrite, Bot: 1, PC: 0, Address: int32(w.addr), Cell: cl})
		final[w.addr] = cl
	}
	var gotWrites []vm.Event
	died := false
	for _, e := range events {
		switch e.Kind {
		case vm.CellWrite:
			gotWrites = append(gotWrites, e)
		case vm.ProcessDeath:
			died = true
		}
	}
	if !slices.Equal(gotWrites, wantWrites) {
		t.Errorf("writes\n got %+v\nwant %+v", gotWrites, wantWrites)
	}
	for i := range final {
		if got := core.Read(int64(i)); got != final[i] {
			t.Errorf("cell %d = %+v, want %+v", i, got, final[i])
		}
	}
	wantQueue := specQueue(t, c.queue)
	if got := q.Addresses(); !slices.Equal(got, wantQueue) {
		t.Errorf("queue %v, want %v", got, wantQueue)
	}
	if died != (len(wantQueue) == 0) {
		t.Errorf("process death event = %v, but the queue is %v", died, wantQueue)
	}
}

type specWrite struct {
	addr int64
	text string
}

// specWrites parses "a: CELL; b: CELL" or "-".
func specWrites(t *testing.T, s string) []specWrite {
	t.Helper()
	if s == "-" {
		return nil
	}
	var out []specWrite
	for _, part := range strings.Split(s, ";") {
		a, text, ok := strings.Cut(strings.TrimSpace(part), ": ")
		if !ok {
			t.Fatalf("bad write %q", part)
		}
		addr, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			t.Fatalf("bad address in %q", part)
		}
		out = append(out, specWrite{addr, text})
	}
	return out
}

func specQueue(t *testing.T, s string) []int32 {
	t.Helper()
	var out []int32
	for _, part := range strings.Split(strings.Trim(s, "[]"), ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		x, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			t.Fatalf("bad queue %q", s)
		}
		out = append(out, int32(x))
	}
	return out
}

// specCell parses "OPCODE.MODIFIER <mode><value>, <mode><value>".
func specCell(t *testing.T, s string) isa.Cell {
	t.Helper()
	head, rest, ok := strings.Cut(strings.TrimSpace(s), " ")
	opName, modName, ok2 := strings.Cut(head, ".")
	a, b, ok3 := strings.Cut(rest, ",")
	if !ok || !ok2 || !ok3 {
		t.Fatalf("bad cell %q", s)
	}
	var c isa.Cell
	opFound, modFound := false, false
	for _, op := range isa.Opcodes {
		if op.String() == opName {
			c.Opcode, opFound = op, true
		}
	}
	for _, m := range isa.Modifiers {
		if m.String() == modName {
			c.Modifier, modFound = m, true
		}
	}
	if !opFound || !modFound {
		t.Fatalf("bad opcode or modifier in %q", s)
	}
	operand := func(text string) (isa.Mode, int32) {
		text = strings.TrimSpace(text)
		for _, m := range isa.Modes {
			if strings.HasPrefix(text, m.String()) {
				v, err := strconv.ParseInt(text[len(m.String()):], 10, 32)
				if err != nil {
					t.Fatalf("bad operand %q in %q", text, s)
				}
				return m, int32(v)
			}
		}
		t.Fatalf("bad mode in operand %q of %q", text, s)
		return 0, 0
	}
	c.AMode, c.AValue = operand(a)
	c.BMode, c.BValue = operand(b)
	return c
}

// ---- C16: imp against dwarf at seed 1 gives the same result and replay hash ----

// TestC16ImpDwarfSeed1 runs `pit run bots/imp.s bots/dwarf.s --seed 1 --replay F`
// twice. Each run must print the golden result and the golden replay hash, write
// replay bytes whose own hash is that hash, and the two runs must agree byte for
// byte. The golden hash must also equal the hash pinned by internal/replay
// (fullHash in replay_test.go), so the two cannot drift apart.
func TestC16ImpDwarfSeed1(t *testing.T) {
	logMachine(t)
	goldenResult := readFile(t, "internal/acceptance/testdata/imp_dwarf_seed1.result.txt")
	goldenHash := strings.TrimSpace(readFile(t, "internal/acceptance/testdata/imp_dwarf_seed1.hash.txt"))

	m := regexp.MustCompile(`(?m)^const fullHash = "([0-9a-f]{64})"$`).FindStringSubmatch(readFile(t, "internal/replay/replay_test.go"))
	if m == nil {
		t.Fatal("internal/replay/replay_test.go has no fullHash constant")
	}
	if goldenHash != m[1] {
		t.Errorf("golden hash %s differs from internal/replay's pinned full-match hash %s", goldenHash, m[1])
	}

	dir := t.TempDir()
	var outs []string
	var replays [][]byte
	for run := 1; run <= 2; run++ {
		path := filepath.Join(dir, fmt.Sprintf("run%d.jsonl", run))
		res := pit(t, "run", "bots/imp.s", "bots/dwarf.s", "--seed", "1", "--replay", path)
		if res.code != 0 || res.stderr != "" {
			t.Fatalf("run %d: exit code %d, stderr %q", run, res.code, res.stderr)
		}
		result, hashLine, ok := strings.Cut(res.stdout, "replay: ")
		if !ok {
			t.Fatalf("run %d: no replay line in %q", run, res.stdout)
		}
		if result != goldenResult {
			t.Errorf("run %d result:\n%s\nwant golden:\n%s", run, result, goldenResult)
		}
		if got := strings.TrimSpace(hashLine); got != goldenHash {
			t.Errorf("run %d printed replay hash %s, want golden %s", run, got, goldenHash)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := replay.Hash(data); got != goldenHash {
			t.Errorf("run %d: the hash of the written replay file is %s, want golden %s", run, got, goldenHash)
		}
		outs = append(outs, res.stdout)
		replays = append(replays, data)
	}
	if outs[0] != outs[1] {
		t.Errorf("the two runs printed different output:\n%s\n%s", outs[0], outs[1])
	}
	if !bytes.Equal(replays[0], replays[1]) {
		t.Errorf("the two runs wrote different replay files (%d and %d bytes)", len(replays[0]), len(replays[1]))
	}
}

// ---- C17: assembler errors have line and column; asm, disasm, asm round-trips ----

type malformed struct {
	name string
	src  string
	line int
	col  int
	msg  string
}

// malformedCases has at least one program for every message of the table in
// docs/spec/asm.md section 7. Each position was worked out from the position rules
// of that section.
var malformedCases = []malformed{
	{"unexpected character", "DAT !\n", 1, 5, `unexpected character '!'`},
	{"unexpected byte", "DAT \xff\n", 1, 5, `unexpected byte 0xff`},
	{"malformed number", "DAT #12ab\n", 1, 6, `malformed number "12ab"`},
	{"expected opcode", "  5 DAT\n", 1, 3, `expected opcode, found "5"`},
	{"unknown opcode on line 2", "DAT 0\nFOO $1, $2\n", 2, 1, `unknown opcode "FOO"`},
	{"unknown modifier", "MOV.Q $1, $2\n", 1, 5, `unknown modifier "Q"`},
	{"reserved label", "mov: DAT 0\n", 1, 1, `label name "mov" is reserved`},
	{"duplicate label", "a: DAT 0\n\na: DAT 1\n", 3, 1, `duplicate label "a" (first defined at line 1)`},
	{"duplicate ORG", "ORG 0\nDAT 0\nORG 0\n", 3, 1, `duplicate ORG (first at line 1)`},
	{"space before the dot", "MOV .I 1\n", 1, 5, `unexpected space before "."`},
	{"space after the dot", "MOV. I 1\n", 1, 6, `unexpected space after "."`},
	{"missing modifier", "MOV.\n", 1, 5, `expected modifier, found end of line`},
	{"bad addressing mode", "MOV.I %1, $2\n", 1, 7, `bad addressing mode "%"`},
	{"missing B operand", "MOV 1,", 1, 7, `expected operand, found end of file`},
	{"missing expression", "MOV #, 1\n", 1, 6, `expected expression, found ","`},
	{"missing parenthesis", "DAT #(1+2, 2\n", 1, 10, `expected ")", found ","`},
	{"two numbers", "MOV 1 2\n", 1, 7, `expected "," or end of line, found "2"`},
	{"three operands", "MOV 1, 2, 3\n", 1, 9, `expected end of line, found ","`},
	{"number out of range", "DAT #2147483648\n", 1, 6, `value out of range`},
	{"result out of range", "DAT #2147483647 + 1\n", 1, 17, `value out of range`},
	{"expression too complex", "DAT #" + strings.Repeat("-", 257) + "1\n", 1, 5 + 257, `expression too complex`},
	{"program too long", strings.Repeat("DAT\n", 1048576+1), 1048577, 1, `program longer than 1048576 instructions`},
	{"undefined label", "\n  JMP nowhere\n", 2, 7, `undefined label "nowhere"`},
	{"division by zero", "DAT #1 / 0\n", 1, 8, `division by zero`},
	{"entry offset outside the program", "ORG 5\nDAT 0\n", 1, 5, `entry offset 5 outside 0..0`},
	{"no instructions", "; nothing\n", 2, 1, `no instructions`},
}

// specMessagePattern turns a message of the table of docs/spec/asm.md section 7
// (with its placeholders X, N, NN, E, M and the example 12ab) into a regexp.
func specMessagePattern(msg string) *regexp.Regexp {
	p := regexp.QuoteMeta(msg)
	for _, r := range [][2]string{
		{`'c'`, `'.+'`},
		{`0xNN`, `0x[0-9a-f]{2}`},
		{`"12ab"`, `"[^"]*"`},
		{`"X"`, `"[^"]*"`},
		{`found X`, `found .+`},
		{`line N`, `line [0-9]+`},
		{`offset E outside 0\.\.M`, `offset -?[0-9]+ outside 0\.\.[0-9]+`},
	} {
		p = strings.ReplaceAll(p, r[0], r[1])
	}
	return regexp.MustCompile("^" + p + "$")
}

// TestC17Assembler checks, through `pit asm`, that every malformed program is
// refused with exit code 1, nothing on stdout and `file:line:col: message` on
// stderr (and that asm.Assemble reports the same position in its Error), and that
// every message in the spec's table has a case. Then every starter bot and every
// asm example of the specs survives assemble, disassemble (`pit asm`), assemble.
func TestC17Assembler(t *testing.T) {
	logMachine(t)

	dir := t.TempDir()
	t.Run("malformed input", func(t *testing.T) {
		for i, tc := range malformedCases {
			path := filepath.Join(dir, fmt.Sprintf("bad%d.s", i))
			if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("%s:%d:%d: %s\n", path, tc.line, tc.col, tc.msg)
			res := pit(t, "asm", path)
			if res.code != 1 || res.stdout != "" || res.stderr != want {
				t.Errorf("%s: pit asm: exit %d, stdout %q, stderr %q; want exit 1, no stdout, stderr %q", tc.name, res.code, res.stdout, res.stderr, want)
			}
			_, err := asm.Assemble(path, []byte(tc.src))
			var ae *asm.Error
			if !errors.As(err, &ae) || ae.Line != tc.line || ae.Col != tc.col || ae.Msg != tc.msg {
				t.Errorf("%s: asm.Assemble error = %v, want %d:%d: %s", tc.name, err, tc.line, tc.col, tc.msg)
			}
		}
	})

	t.Run("every message of the spec has a case", func(t *testing.T) {
		var templates []string
		tables := tableRows(readFile(t, "docs/spec/asm.md"), "| Message |")
		if len(tables) != 1 {
			t.Fatalf("found %d message tables in asm.md, want 1", len(tables))
		}
		for _, r := range tables[0] {
			for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(r[0], -1) {
				templates = append(templates, m[1])
			}
		}
		if len(templates) < 20 {
			t.Fatalf("parsed only %d message templates: %v", len(templates), templates)
		}
		used := make([]bool, len(malformedCases))
		for _, tpl := range templates {
			re := specMessagePattern(tpl)
			found := false
			for i, tc := range malformedCases {
				if re.MatchString(tc.msg) {
					found, used[i] = true, true
				}
			}
			if !found {
				t.Errorf("no malformed-input case for the message %q", tpl)
			}
		}
		for i, tc := range malformedCases {
			if !used[i] {
				t.Errorf("case %q (%s) matches no message of the spec table", tc.name, tc.msg)
			}
		}
	})

	t.Run("round trip", func(t *testing.T) {
		type source struct{ name, text string }
		var sources []source
		starters, err := filepath.Glob(filepath.Join(repoRoot, "bots", "*.s"))
		if err != nil || len(starters) != 4 {
			t.Fatalf("starter bots %v, err %v; want 4", starters, err)
		}
		for _, p := range starters {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			sources = append(sources, source{"bots/" + filepath.Base(p), string(data)})
		}
		for _, doc := range []struct {
			path string
			min  int
		}{{"docs/spec/isa.md", 3}, {"docs/spec/asm.md", 4}} {
			blocks := fencedBlocks(readFile(t, doc.path), "asm")
			if len(blocks) < doc.min {
				t.Errorf("%s has %d asm examples, want at least %d", doc.path, len(blocks), doc.min)
			}
			for i, b := range blocks {
				sources = append(sources, source{fmt.Sprintf("%s example %d", doc.path, i+1), b})
			}
		}
		for i, s := range sources {
			orig := filepath.Join(dir, fmt.Sprintf("orig%d.s", i))
			if err := os.WriteFile(orig, []byte(s.text), 0o644); err != nil {
				t.Fatal(err)
			}
			prog, err := asm.Assemble(s.name, []byte(s.text))
			if err != nil {
				t.Errorf("%s: %v", s.name, err)
				continue
			}
			list := pit(t, "asm", orig)
			if list.code != 0 || list.stderr != "" {
				t.Errorf("%s: pit asm: exit %d, stderr %q", s.name, list.code, list.stderr)
				continue
			}
			back, err := asm.Assemble(s.name+" listing", []byte(list.stdout))
			if err != nil {
				t.Errorf("%s: the listing does not assemble: %v\n%s", s.name, err, list.stdout)
				continue
			}
			if back.Entry != prog.Entry || !reflect.DeepEqual(back.Cells, prog.Cells) {
				t.Errorf("%s: listing assembles to entry %d cells %+v, want entry %d cells %+v", s.name, back.Entry, back.Cells, prog.Entry, prog.Cells)
			}
			// The listing is a fixed point: listing the listing prints it again.
			lpath := filepath.Join(dir, fmt.Sprintf("list%d.s", i))
			if err := os.WriteFile(lpath, []byte(list.stdout), 0o644); err != nil {
				t.Fatal(err)
			}
			if again := pit(t, "asm", lpath); again.code != 0 || again.stdout != list.stdout {
				t.Errorf("%s: the listing of the listing differs:\n%s\nthen\n%s", s.name, list.stdout, again.stdout)
			}
		}
		if len(sources) < 4+3+4 {
			t.Errorf("only %d sources round-tripped", len(sources))
		}
	})
}

// ---- C18: the starter-bot tournament over 100 rounds prints a stable scoreboard ----

// TestC18Tournament runs `pit tourney bots` (the default 100 rounds) twice and
// requires identical output, four ranked bots, and a first row equal to the
// example scoreboard of docs/spec/tourney.md. It plays 2 x 1200 full matches, so
// -short skips it.
func TestC18Tournament(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 2400 matches; run without -short")
	}
	logMachine(t)
	first := pit(t, "tourney", "bots")
	second := pit(t, "tourney", "bots")
	for i, res := range []pitResult{first, second} {
		if res.code != 0 || res.stderr != "" {
			t.Fatalf("run %d: exit code %d, stderr %q", i+1, res.code, res.stderr)
		}
	}
	if first.stdout != second.stdout {
		t.Errorf("the two runs printed different scoreboards:\n%s\n%s", first.stdout, second.stdout)
	}
	lines := strings.Split(strings.TrimRight(first.stdout, "\n"), "\n")
	if len(lines) != 3+4 {
		t.Fatalf("scoreboard has %d lines, want 7:\n%s", len(lines), first.stdout)
	}
	if want := "tourney: 4 bots, 100 rounds, 1200 matches, core size 8000, cycle limit 80000, max processes 8000, min separation 100"; lines[0] != want {
		t.Errorf("first line %q, want %q", lines[0], want)
	}
	var names []string
	for i, row := range lines[3:] {
		f := strings.Fields(row)
		if len(f) != 8 || f[0] != strconv.Itoa(i+1) || f[3] != "600" {
			t.Errorf("row %d = %q, want rank %d, 8 columns and 600 matches played", i+1, row, i+1)
		}
		names = append(names, f[1])
	}
	if want := []string{"dwarf", "imp", "replicator", "scanner"}; !slices.Equal(slices.Sorted(slices.Values(names)), want) {
		t.Errorf("ranked bots %v, want %v", names, want)
	}

	// The example of the spec shows the first row of this very tournament.
	var example []string
	for _, b := range fencedBlocks(readFile(t, "docs/spec/tourney.md"), "") {
		if strings.HasPrefix(b, "tourney: 4 bots, 100 rounds") {
			example = strings.Split(strings.TrimRight(b, "\n"), "\n")
		}
	}
	if len(example) != 4 {
		t.Fatalf("docs/spec/tourney.md has no 4-line example scoreboard, found %q", example)
	}
	if !slices.Equal(strings.Fields(example[3]), strings.Fields(lines[3])) {
		t.Errorf("first row %q, but docs/spec/tourney.md shows %q", lines[3], example[3])
	}
}

// ---- C19: the terminal and web viewers play the same replay file the same way ----

// TestC19ViewerParity checks the terminal viewer's frame model against every
// expected-frames file in testdata/viewer, and that both viewers' own test suites
// read each of those files: internal/tui (TestFrames, Go) and web/test/model.test.js
// (node --test), so a file one viewer skips fails here. The web side runs in
// `node --test web/test/*.test.js`; only canvas drawing and the interactive viewer
// stay manual (see the note of S17).
func TestC19ViewerParity(t *testing.T) {
	frameFiles, err := filepath.Glob(filepath.Join(repoRoot, "testdata", "viewer", "*frames.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(frameFiles) < 2 {
		t.Fatalf("found %d frames files in testdata/viewer, want at least 2 (frames.json, spl_frames.json)", len(frameFiles))
	}
	var webTests, tuiTests strings.Builder
	webFiles, _ := filepath.Glob(filepath.Join(repoRoot, "web", "test", "*.js"))
	tuiFiles, _ := filepath.Glob(filepath.Join(repoRoot, "internal", "tui", "*_test.go"))
	if len(webFiles) == 0 || len(tuiFiles) == 0 {
		t.Fatalf("found %d web test files and %d tui test files", len(webFiles), len(tuiFiles))
	}
	for _, p := range webFiles {
		webTests.WriteString(readFile(t, strings.TrimPrefix(filepath.ToSlash(p), repoRoot+"/")))
	}
	for _, p := range tuiFiles {
		tuiTests.WriteString(readFile(t, strings.TrimPrefix(filepath.ToSlash(p), repoRoot+"/")))
	}

	for _, framesPath := range frameFiles {
		name := filepath.Base(framesPath)
		t.Run(name, func(t *testing.T) {
			var ff struct {
				Fixture  string      `json:"fixture"`
				CoreSize int         `json:"core_size"`
				Cycles   int         `json:"cycles"`
				Frames   []tui.Frame `json:"frames"`
			}
			if err := json.Unmarshal([]byte(readFile(t, "testdata/viewer/"+name)), &ff); err != nil {
				t.Fatal(err)
			}
			// A path in quotes, as the readers write it, not a mention in a test title.
			for _, file := range []string{name, ff.Fixture} {
				ref := regexp.MustCompile(`testdata/viewer/` + regexp.QuoteMeta(file) + `["']`)
				if !ref.MatchString(webTests.String()) {
					t.Errorf("no file in web/test reads testdata/viewer/%s", file)
				}
				if !ref.MatchString(tuiTests.String()) {
					t.Errorf("no file in internal/tui reads testdata/viewer/%s", file)
				}
			}
			f, err := os.Open(filepath.Join(repoRoot, "testdata", "viewer", ff.Fixture))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			r, err := replay.Read(f)
			if err != nil {
				t.Fatal(err)
			}
			m, err := tui.NewModel(r)
			if err != nil {
				t.Fatal(err)
			}
			if m.Cycles() != ff.Cycles || len(ff.Frames) != ff.Cycles+1 || r.Config.CoreSize != ff.CoreSize {
				t.Fatalf("model has %d cycles and core %d; %s has cycles %d, core %d, %d frames", m.Cycles(), r.Config.CoreSize, name, ff.Cycles, ff.CoreSize, len(ff.Frames))
			}
			for _, want := range ff.Frames {
				if err := m.Seek(want.Cycle); err != nil {
					t.Fatal(err)
				}
				if got := m.Frame(); !reflect.DeepEqual(got, want) {
					t.Errorf("cycle %d:\n got %+v\nwant %+v", want.Cycle, got, want)
				}
			}
		})
	}
}
