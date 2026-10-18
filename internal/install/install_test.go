// SPDX-License-Identifier: AGPL-3.0-or-later

// Package install is test-only: it runs scripts/install.sh against a fake GitHub
// release served by net/http/httptest. Nothing here touches the network.
package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

const version = "v0.1.1"

var botNames = []string{"imp.s", "dwarf.s", "replicator.s", "scanner.s"}

var platforms = [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}

func scriptPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func botBody(name string) string { return "; stub bot " + name + "\n" }

func pitBody(ver, goos, goarch string) string {
	return fmt.Sprintf("#!/bin/sh\necho pit-stub %s %s %s\n", ver, goos, goarch)
}

// ---- fake release ----

type tarEntry struct {
	name string
	body string
	mode int64
	typ  byte // 0 means a regular file
	link string
}

func stdEntries(ver, goos, goarch string) []tarEntry {
	top := fmt.Sprintf("pit_%s_%s_%s", ver, goos, goarch)
	es := []tarEntry{
		{name: top + "/", typ: tar.TypeDir, mode: 0o755},
		{name: top + "/pit", body: pitBody(ver, goos, goarch), mode: 0o755},
		{name: top + "/bots/", typ: tar.TypeDir, mode: 0o755},
		{name: top + "/LICENSE", body: "license\n", mode: 0o644},
		{name: top + "/NOTICE", body: "notice\n", mode: 0o644},
		{name: top + "/README.md", body: "readme\n", mode: 0o644},
	}
	for _, b := range botNames {
		es = append(es, tarEntry{name: top + "/bots/" + b, body: botBody(b), mode: 0o644})
	}
	return es
}

func tarGz(t *testing.T, es []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range es {
		h := &tar.Header{Name: e.name, Mode: e.mode, Typeflag: e.typ, Linkname: e.link}
		if e.typ == 0 {
			h.Typeflag = tar.TypeReg
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func archiveName(ver, goos, goarch string) string {
	return fmt.Sprintf("pit_%s_%s_%s.tar.gz", ver, goos, goarch)
}

// release is one fake GitHub release: archives for the four platforms and the
// SHA256SUMS file as build-release.sh writes it (two spaces between hash and name).
type release struct {
	ver      string
	archives map[string][]byte
	sums     string
}

// newRelease builds a release. edit may change the entries of one archive.
func newRelease(t *testing.T, ver string, edit func(goos, goarch string, es []tarEntry) []tarEntry) release {
	t.Helper()
	r := release{ver: ver, archives: map[string][]byte{}}
	var names []string
	for _, p := range platforms {
		es := stdEntries(ver, p[0], p[1])
		if edit != nil {
			es = edit(p[0], p[1], es)
		}
		n := archiveName(ver, p[0], p[1])
		r.archives[n] = tarGz(t, es)
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r.sums += sum(r.archives[n]) + "  " + n + "\n"
	}
	return r
}

// routes returns the URL paths (under /releases) the release is served at.
func (r release) routes(routes map[string][]byte) map[string][]byte {
	for n, b := range r.archives {
		routes["/releases/download/"+r.ver+"/"+n] = b
	}
	routes["/releases/download/"+r.ver+"/SHA256SUMS"] = []byte(r.sums)
	return routes
}

type server struct {
	*httptest.Server
	mu   sync.Mutex
	hits []string
}

func newServer(t *testing.T, routes map[string][]byte, redirects map[string]string) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits = append(s.hits, r.URL.Path)
		s.mu.Unlock()
		if to, ok := redirects[r.URL.Path]; ok {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
		b, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) base() string { return s.URL + "/releases" }

func (s *server) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.hits...)
}

// ---- running the script ----

type opts struct {
	shell    string    // default "sh"
	version  string    // PITFIGHT_VERSION; "-" leaves it unset (latest)
	uname    [2]string // stub uname -s and -m; empty keeps the real uname
	path     string    // PATH behind the stub dirs; empty means the real PATH
	prefix   string    // default: a nonexistent dir under t.TempDir()
	env      []string  // extra KEY=VALUE
	stdin    bool      // feed the script on stdin (as curl | sh does)
	baseURL  string
	noPrefix bool   // leave PREFIX unset
	cwdFile  string // create this empty file in the working directory
}

type result struct {
	code           int
	out            string
	prefix         string
	home, tmp, cwd string
}

func shellFor(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not available", name)
	}
	return p
}

func run(t *testing.T, srv *server, o opts) result {
	t.Helper()
	if o.shell == "" {
		o.shell = "sh"
	}
	shell := shellFor(t, o.shell)
	base := t.TempDir()
	res := result{
		prefix: filepath.Join(base, "pre fix"), // a space on purpose
		home:   filepath.Join(base, "home"),
		tmp:    filepath.Join(base, "tmp"),
		cwd:    filepath.Join(base, "cwd"),
	}
	if o.prefix != "" {
		res.prefix = o.prefix
	}
	for _, d := range []string{res.home, res.tmp, res.cwd} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if o.cwdFile != "" {
		if err := os.WriteFile(filepath.Join(res.cwd, o.cwdFile), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var dirs []string
	if o.uname[0] != "" {
		d := t.TempDir()
		stub := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in -s) echo '%s';; -m) echo '%s';; *) exit 2;; esac\n", o.uname[0], o.uname[1])
		if err := os.WriteFile(filepath.Join(d, "uname"), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
	}
	path := o.path
	if path == "" {
		path = os.Getenv("PATH")
	}
	path = strings.Join(append(dirs, path), string(os.PathListSeparator))
	env := []string{"PATH=" + path, "HOME=" + res.home, "TMPDIR=" + res.tmp, "LANG=C"}
	if !o.noPrefix {
		env = append(env, "PREFIX="+res.prefix)
	}
	if srv != nil {
		env = append(env, "PITFIGHT_BASE_URL="+srv.base())
	}
	if o.baseURL != "" {
		env = append(env, "PITFIGHT_BASE_URL="+o.baseURL)
	}
	switch o.version {
	case "":
		env = append(env, "PITFIGHT_VERSION="+version)
	case "-":
	default:
		env = append(env, "PITFIGHT_VERSION="+o.version)
	}
	env = append(env, o.env...)
	var cmd *exec.Cmd
	if o.stdin {
		cmd = exec.Command(shell)
		f, err := os.Open(scriptPath(t))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		cmd.Stdin = f
	} else {
		cmd = exec.Command(shell, scriptPath(t))
	}
	cmd.Env = env
	cmd.Dir = res.cwd
	out, err := cmd.CombinedOutput()
	res.out = string(out)
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running script: %v", err)
		}
		res.code = ee.ExitCode()
	}
	return res
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustFail(t *testing.T, r result, want string) {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("exit 0, want failure; output:\n%s", r.out)
	}
	if want != "" && !strings.Contains(r.out, want) {
		t.Fatalf("output lacks %q:\n%s", want, r.out)
	}
}

func mustSucceed(t *testing.T, r result) {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("exit %d, want 0; output:\n%s", r.code, r.out)
	}
}

// assertNothingInstalled checks a failed run left no file under PREFIX, and
// nothing in HOME, TMPDIR or the working directory either.
func assertNothingInstalled(t *testing.T, r result) {
	t.Helper()
	if got := files(t, r.prefix); len(got) != 0 {
		t.Errorf("files under PREFIX after a failed install: %v", got)
	}
	assertClean(t, r)
}

func assertClean(t *testing.T, r result) {
	t.Helper()
	for name, dir := range map[string]string{"HOME": r.home, "TMPDIR": r.tmp, "cwd": r.cwd} {
		if got := files(t, dir); len(got) != 0 {
			t.Errorf("files left in %s: %v", name, got)
		}
		ents, _ := os.ReadDir(dir)
		if len(ents) != 0 {
			t.Errorf("entries left in %s: %v", name, ents)
		}
	}
}

func assertInstalled(t *testing.T, r result, ver, goos, goarch string) {
	t.Helper()
	pit := filepath.Join(r.prefix, "bin", "pit")
	st, err := os.Stat(pit)
	if err != nil {
		t.Fatalf("pit not installed: %v\noutput:\n%s", err, r.out)
	}
	if st.Mode()&0o111 == 0 {
		t.Errorf("pit mode %v is not executable", st.Mode())
	}
	b, _ := os.ReadFile(pit)
	if string(b) != pitBody(ver, goos, goarch) {
		t.Errorf("pit content %q, want the %s %s %s stub", b, ver, goos, goarch)
	}
	for _, n := range botNames {
		b, err := os.ReadFile(filepath.Join(r.prefix, "share", "pitfight", "bots", n))
		if err != nil {
			t.Errorf("bot %s not installed: %v", n, err)
			continue
		}
		if string(b) != botBody(n) {
			t.Errorf("bot %s content %q", n, b)
		}
	}
	got := files(t, r.prefix)
	if len(got) != 1+len(botNames) {
		t.Errorf("files under PREFIX = %v, want pit and %d bots only", got, len(botNames))
	}
}

func std(t *testing.T) (*server, release) {
	t.Helper()
	rel := newRelease(t, version, nil)
	return newServer(t, rel.routes(map[string][]byte{}), nil), rel
}

// ---- tests ----

func TestScriptParses(t *testing.T) {
	for _, sh := range []string{"sh", "dash"} {
		p := shellFor(t, sh)
		if out, err := exec.Command(p, "-n", scriptPath(t)).CombinedOutput(); err != nil {
			t.Errorf("%s -n: %v\n%s", sh, err, out)
		}
	}
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "#!/bin/sh\n") {
		t.Errorf("first line is not #!/bin/sh")
	}
	if !strings.Contains(strings.SplitN(s, "\n\n", 2)[0], "SPDX-License-Identifier: AGPL-3.0-or-later") {
		t.Errorf("no SPDX header")
	}
	for _, bad := range []string{"eval ", "sudo", "[[", "echo -e", "pipefail", "local "} {
		if strings.Contains(s, bad) {
			t.Errorf("script contains %q", bad)
		}
	}
	if !strings.Contains(s, "set -eu") {
		t.Errorf("script lacks set -eu")
	}
	for _, want := range []string{"--proto =https", "--proto-redir =https", "--https-only"} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q for the default URL", want)
		}
	}
	if !strings.Contains(s, "https://github.com/squalor-xyz/pitfight/releases") {
		t.Errorf("default base URL is not the https GitHub releases URL")
	}
}

func TestInstallsForEachPlatform(t *testing.T) {
	srv, _ := std(t)
	// The real platform, with the real uname.
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
			r := run(t, srv, opts{})
			mustSucceed(t, r)
			assertInstalled(t, r, version, runtime.GOOS, runtime.GOARCH)
			assertClean(t, r)
		}
	}
	cases := []struct {
		uname        [2]string
		goos, goarch string
	}{
		{[2]string{"Linux", "x86_64"}, "linux", "amd64"},
		{[2]string{"Linux", "amd64"}, "linux", "amd64"},
		{[2]string{"Linux", "aarch64"}, "linux", "arm64"},
		{[2]string{"Linux", "arm64"}, "linux", "arm64"},
		{[2]string{"Darwin", "x86_64"}, "darwin", "amd64"},
		{[2]string{"Darwin", "arm64"}, "darwin", "arm64"},
	}
	for _, c := range cases {
		t.Run(c.uname[0]+"_"+c.uname[1], func(t *testing.T) {
			r := run(t, srv, opts{uname: c.uname})
			mustSucceed(t, r)
			assertInstalled(t, r, version, c.goos, c.goarch)
			assertClean(t, r)
		})
	}
}

func TestInstallsUnderDash(t *testing.T) {
	srv, _ := std(t)
	r := run(t, srv, opts{shell: "dash", uname: [2]string{"Linux", "x86_64"}})
	mustSucceed(t, r)
	assertInstalled(t, r, version, "linux", "amd64")
	assertClean(t, r)
}

func TestRunsFromStdin(t *testing.T) {
	srv, _ := std(t)
	r := run(t, srv, opts{stdin: true, uname: [2]string{"Linux", "x86_64"}})
	mustSucceed(t, r)
	assertInstalled(t, r, version, "linux", "amd64")
	assertClean(t, r)
}

func TestDefaultPrefixIsHomeDotLocal(t *testing.T) {
	srv, _ := std(t)
	r := run(t, srv, opts{noPrefix: true, uname: [2]string{"Linux", "x86_64"}})
	mustSucceed(t, r)
	r.prefix = filepath.Join(r.home, ".local")
	assertInstalled(t, r, version, "linux", "amd64")
}

func TestChecksumMismatchInstallsNothing(t *testing.T) {
	rel := newRelease(t, version, nil)
	name := archiveName(version, "linux", "amd64")
	bad := strings.Replace(rel.sums, sum(rel.archives[name]), strings.Repeat("0", 64), 1)
	if bad == rel.sums {
		t.Fatal("test bug: sums unchanged")
	}
	rel.sums = bad
	srv := newServer(t, rel.routes(map[string][]byte{}), nil)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}})
	mustFail(t, r, "checksum")
	assertNothingInstalled(t, r)
}

func TestTamperedArchiveInstallsNothing(t *testing.T) {
	rel := newRelease(t, version, nil)
	name := archiveName(version, "linux", "amd64")
	routes := rel.routes(map[string][]byte{})
	// Same name, different bytes than SHA256SUMS lists.
	evil := stdEntries(version, "linux", "amd64")
	evil[1].body = "#!/bin/sh\necho evil\n"
	routes["/releases/download/"+version+"/"+name] = tarGz(t, evil)
	srv := newServer(t, routes, nil)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}})
	mustFail(t, r, "checksum")
	assertNothingInstalled(t, r)
}

func TestMissingChecksumEntryInstallsNothing(t *testing.T) {
	name := archiveName(version, "linux", "amd64")
	hashOf := func(rel release) string { return sum(rel.archives[name]) }
	cases := map[string]func(rel release) string{
		"entry removed": func(rel release) string {
			var keep []string
			for _, l := range strings.SplitAfter(rel.sums, "\n") {
				if !strings.Contains(l, name) {
					keep = append(keep, l)
				}
			}
			return strings.Join(keep, "")
		},
		"only a longer name": func(rel release) string {
			return hashOf(rel) + "  x" + name + "\n"
		},
		"only a name with a suffix": func(rel release) string {
			return hashOf(rel) + "  " + name + ".sig\n"
		},
		"hash only in a comment": func(rel release) string {
			return "# " + hashOf(rel) + "  " + name + "\n"
		},
		"empty": func(release) string { return "" },
	}
	for label, mk := range cases {
		t.Run(label, func(t *testing.T) {
			rel := newRelease(t, version, nil)
			rel.sums = mk(rel)
			srv := newServer(t, rel.routes(map[string][]byte{}), nil)
			r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}})
			mustFail(t, r, "SHA256SUMS")
			assertNothingInstalled(t, r)
		})
	}
}

func TestDuplicateChecksumEntryFails(t *testing.T) {
	rel := newRelease(t, version, nil)
	name := archiveName(version, "linux", "amd64")
	rel.sums += strings.Repeat("0", 64) + "  " + name + "\n"
	srv := newServer(t, rel.routes(map[string][]byte{}), nil)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}})
	mustFail(t, r, "")
	assertNothingInstalled(t, r)
}

// A hash field of * must not be expanded as a file name pattern: with a file in the
// working directory named like the real hash, a globbing script would take it as the
// SHA256SUMS entry and install.
func TestGlobInChecksumFieldFails(t *testing.T) {
	rel := newRelease(t, version, nil)
	name := archiveName(version, "linux", "amd64")
	rel.sums = "*  " + name + "\n"
	srv := newServer(t, rel.routes(map[string][]byte{}), nil)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}, cwdFile: sum(rel.archives[name])})
	mustFail(t, r, "SHA256SUMS")
	if got := files(t, r.prefix); len(got) != 0 {
		t.Errorf("files under PREFIX after a failed install: %v", got)
	}
	for name, dir := range map[string]string{"HOME": r.home, "TMPDIR": r.tmp} {
		if got := files(t, dir); len(got) != 0 {
			t.Errorf("files left in %s: %v", name, got)
		}
	}
}

func TestSumsDownloadFailureFailsClosed(t *testing.T) {
	rel := newRelease(t, version, nil)
	routes := rel.routes(map[string][]byte{})
	delete(routes, "/releases/download/"+version+"/SHA256SUMS")
	srv := newServer(t, routes, nil)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}})
	mustFail(t, r, "SHA256SUMS")
	assertNothingInstalled(t, r)
}

func TestArchiveDownloadFailureInstallsNothing(t *testing.T) {
	rel := newRelease(t, version, nil)
	routes := rel.routes(map[string][]byte{})
	delete(routes, "/releases/download/"+version+"/"+archiveName(version, "linux", "amd64"))
	srv := newServer(t, routes, nil)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}})
	mustFail(t, r, "")
	assertNothingInstalled(t, r)
}

func TestNoEnvSwitchSkipsChecksum(t *testing.T) {
	rel := newRelease(t, version, nil)
	rel.sums = strings.Repeat("0", 64) + "  " + archiveName(version, "linux", "amd64") + "\n"
	srv := newServer(t, rel.routes(map[string][]byte{}), nil)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}, env: []string{
		"PITFIGHT_SKIP_CHECKSUM=1", "PITFIGHT_INSECURE=1", "PITFIGHT_NO_VERIFY=1", "SKIP_CHECKSUM=1", "INSECURE=1",
		"PITFIGHT_SHA256=" + strings.Repeat("0", 64), "SHA256SUM=true", "SHASUM=true",
	}})
	mustFail(t, r, "checksum")
	assertNothingInstalled(t, r)
}

// minimalPath makes a directory of symlinks to the tools the script may use, and
// never to a hash tool, so a test can add one or leave it out.
func minimalPath(t *testing.T, withHash bool) string {
	t.Helper()
	d := t.TempDir()
	tools := []string{"uname", "mktemp", "curl", "wget", "tar", "gzip", "gunzip", "awk", "tr", "rm", "rmdir", "mkdir", "mv",
		"cp", "chmod", "find", "cat", "head", "sed", "grep", "sort", "wc", "dirname", "basename", "ls", "id", "printf", "test", "cut", "expr", "env", "pwd"}
	if withHash {
		tools = append(tools, "sha256sum", "shasum", "perl")
	}
	for _, n := range tools {
		p, err := exec.LookPath(n)
		if err != nil {
			continue
		}
		if err := os.Symlink(p, filepath.Join(d, n)); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestNoHashToolFailsClosed(t *testing.T) {
	srv, _ := std(t)
	u := [2]string{"Linux", "x86_64"}

	// Control: with the same tools plus a hash tool the install works, so the failure
	// below is due to the missing hash tool and nothing else.
	r := run(t, srv, opts{uname: u, path: minimalPath(t, true)})
	mustSucceed(t, r)

	r = run(t, srv, opts{uname: u, path: minimalPath(t, false)})
	mustFail(t, r, "sha256")
	assertNothingInstalled(t, r)
}

func TestUnsupportedPlatform(t *testing.T) {
	srv, _ := std(t)
	cases := []struct {
		uname [2]string
		want  string
	}{
		{[2]string{"FreeBSD", "amd64"}, "FreeBSD"},
		{[2]string{"MINGW64_NT-10.0", "x86_64"}, "MINGW64_NT-10.0"},
		{[2]string{"Linux", "riscv64"}, "riscv64"},
		{[2]string{"Linux", "i686"}, "i686"},
		{[2]string{"Darwin", "ppc"}, "ppc"},
	}
	for _, c := range cases {
		t.Run(c.uname[0]+"_"+c.uname[1], func(t *testing.T) {
			r := run(t, srv, opts{uname: c.uname})
			mustFail(t, r, c.want)
			if !strings.Contains(r.out, "unsupported") {
				t.Errorf("message does not say unsupported:\n%s", r.out)
			}
			assertNothingInstalled(t, r)
		})
	}
	if reqs := srv.requested(); len(reqs) != 0 {
		t.Errorf("requests made before the platform check: %v", reqs)
	}
}

func TestVersionSelectsArchive(t *testing.T) {
	routes := map[string][]byte{}
	newRelease(t, "v0.1.1", nil).routes(routes)
	newRelease(t, "v0.2.0-rc.1", nil).routes(routes)
	srv := newServer(t, routes, nil)
	u := [2]string{"Linux", "x86_64"}
	r := run(t, srv, opts{uname: u, version: "v0.2.0-rc.1"})
	mustSucceed(t, r)
	assertInstalled(t, r, "v0.2.0-rc.1", "linux", "amd64")
	for _, h := range srv.requested() {
		if strings.Contains(h, "v0.1.1") {
			t.Errorf("fetched %s while v0.2.0-rc.1 was requested", h)
		}
	}
	want := "/releases/download/v0.2.0-rc.1/pit_v0.2.0-rc.1_linux_amd64.tar.gz"
	found := false
	for _, h := range srv.requested() {
		if h == want {
			found = true
		}
	}
	if !found {
		t.Errorf("never requested %s; got %v", want, srv.requested())
	}
}

func TestLatestFollowsRedirect(t *testing.T) {
	routes := map[string][]byte{}
	newRelease(t, "v0.1.0", nil).routes(routes)
	newRelease(t, "v0.1.2", nil).routes(routes)
	redirects := map[string]string{"/releases/latest/download/SHA256SUMS": "/releases/download/v0.1.2/SHA256SUMS"}
	srv := newServer(t, routes, redirects)
	r := run(t, srv, opts{uname: [2]string{"Darwin", "arm64"}, version: "-"})
	mustSucceed(t, r)
	assertInstalled(t, r, "v0.1.2", "darwin", "arm64")
	assertClean(t, r)
	for _, h := range srv.requested() {
		if strings.Contains(h, "v0.1.0") {
			t.Errorf("fetched %s", h)
		}
	}
}

func TestLatestWithBadNameFails(t *testing.T) {
	// The latest SHA256SUMS names a version with characters that must not reach a URL.
	rel := newRelease(t, "v0.1.2", nil)
	rel.sums = strings.Repeat("a", 64) + "  pit_v1.0$(touch_x)_linux_amd64.tar.gz\n"
	routes := rel.routes(map[string][]byte{})
	routes["/releases/latest/download/SHA256SUMS"] = []byte(rel.sums)
	srv := newServer(t, routes, nil)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}, version: "-"})
	mustFail(t, r, "")
	assertNothingInstalled(t, r)
}

func TestInvalidVersionRejected(t *testing.T) {
	srv, _ := std(t)
	for _, v := range []string{"0.1.1", "v0.1.1/../../x", "../v0.1.1", "v0.1.1 x", "v0.1.1;touch x", "v$(id)", "latest"} {
		t.Run(v, func(t *testing.T) {
			r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}, version: v})
			mustFail(t, r, "PITFIGHT_VERSION")
			assertNothingInstalled(t, r)
		})
	}
	if reqs := srv.requested(); len(reqs) != 0 {
		t.Errorf("requests made for invalid versions: %v", reqs)
	}
}

func TestRelativePrefixRejected(t *testing.T) {
	srv, _ := std(t)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}, prefix: "relative/dir"})
	mustFail(t, r, "PREFIX")
	assertClean(t, r)
}

func TestBadArchiveLayoutInstallsNothing(t *testing.T) {
	top := func(goos, goarch string) string { return fmt.Sprintf("pit_%s_%s_%s", version, goos, goarch) }
	evilAbs := filepath.Join(t.TempDir(), "pitfight-evil")
	cases := map[string]func(goos, goarch string, es []tarEntry) []tarEntry{
		"path traversal": func(goos, goarch string, es []tarEntry) []tarEntry {
			return append(es, tarEntry{name: top(goos, goarch) + "/../../evil", body: "x", mode: 0o644})
		},
		"traversal first": func(goos, goarch string, es []tarEntry) []tarEntry {
			return append([]tarEntry{{name: "../evil", body: "x", mode: 0o644}}, es...)
		},
		"absolute path": func(goos, goarch string, es []tarEntry) []tarEntry {
			return append(es, tarEntry{name: evilAbs, body: "x", mode: 0o644})
		},
		"outside the top directory": func(goos, goarch string, es []tarEntry) []tarEntry {
			return append(es, tarEntry{name: "other/file", body: "x", mode: 0o644})
		},
		"symlink entry": func(goos, goarch string, es []tarEntry) []tarEntry {
			return append(es, tarEntry{name: top(goos, goarch) + "/bots/link.s", typ: tar.TypeSymlink, link: "/etc/passwd", mode: 0o777})
		},
		"pit is a symlink": func(goos, goarch string, es []tarEntry) []tarEntry {
			es[1] = tarEntry{name: top(goos, goarch) + "/pit", typ: tar.TypeSymlink, link: "/bin/sh", mode: 0o777}
			return es
		},
		"no pit": func(goos, goarch string, es []tarEntry) []tarEntry {
			return append(es[:1], es[2:]...)
		},
		"no bots": func(goos, goarch string, es []tarEntry) []tarEntry {
			return es[:2]
		},
	}
	for label, edit := range cases {
		t.Run(label, func(t *testing.T) {
			rel := newRelease(t, version, edit) // the checksum matches the bad archive
			srv := newServer(t, rel.routes(map[string][]byte{}), nil)
			r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}})
			want := "archive"
			switch label {
			case "path traversal", "traversal first", "absolute path", "outside the top directory":
				// The script's own listing check must refuse these, before the
				// system tar gets to unpack (or refuse) them.
				want = "unexpected path in archive"
			}
			mustFail(t, r, want)
			assertNothingInstalled(t, r)
			if _, err := os.Stat(evilAbs); err == nil {
				t.Errorf("%s was written", evilAbs)
			}
		})
	}
}

func TestInstallFailureMidwayLeavesNoInstall(t *testing.T) {
	srv, _ := std(t)
	prefix := filepath.Join(t.TempDir(), "p")
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// share is a file, so the bots directory cannot be created.
	if err := os.WriteFile(filepath.Join(prefix, "share"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}, prefix: prefix})
	mustFail(t, r, "")
	if got := files(t, prefix); len(got) != 1 || got[0] != "share" {
		t.Errorf("files under PREFIX = %v, want only the pre-existing share", got)
	}
	assertClean(t, r)
}

func TestMoveFailureCleansStaging(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("a read-only directory does not stop root")
	}
	srv, _ := std(t)
	prefix := filepath.Join(t.TempDir(), "p")
	bots := filepath.Join(prefix, "share", "pitfight", "bots")
	if err := os.MkdirAll(bots, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bots, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(bots, 0o755) })
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}, prefix: prefix})
	mustFail(t, r, "")
	if got := files(t, prefix); len(got) != 0 {
		t.Errorf("files under PREFIX after a failed install (staging left behind?): %v", got)
	}
	assertClean(t, r)
}

func TestPitMadeExecutable(t *testing.T) {
	rel := newRelease(t, version, func(goos, goarch string, es []tarEntry) []tarEntry {
		es[1].mode = 0o600
		return es
	})
	srv := newServer(t, rel.routes(map[string][]byte{}), nil)
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}})
	mustSucceed(t, r)
	st, err := os.Stat(filepath.Join(r.prefix, "bin", "pit"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Errorf("pit mode %v, want 0755", st.Mode().Perm())
	}
}

func TestReinstallReplacesAndKeepsUserBots(t *testing.T) {
	routes := map[string][]byte{}
	newRelease(t, "v0.1.1", nil).routes(routes)
	newRelease(t, "v0.1.2", nil).routes(routes)
	srv := newServer(t, routes, nil)
	u := [2]string{"Linux", "x86_64"}
	prefix := filepath.Join(t.TempDir(), "p")
	r := run(t, srv, opts{uname: u, prefix: prefix, version: "v0.1.1"})
	mustSucceed(t, r)
	mine := filepath.Join(prefix, "share", "pitfight", "bots", "mine.s")
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "share", "pitfight", "bots", "imp.s"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = run(t, srv, opts{uname: u, prefix: prefix, version: "v0.1.2"})
	mustSucceed(t, r)
	b, _ := os.ReadFile(filepath.Join(prefix, "bin", "pit"))
	if string(b) != pitBody("v0.1.2", "linux", "amd64") {
		t.Errorf("pit not replaced: %q", b)
	}
	b, _ = os.ReadFile(filepath.Join(prefix, "share", "pitfight", "bots", "imp.s"))
	if string(b) != botBody("imp.s") {
		t.Errorf("imp.s not replaced: %q", b)
	}
	if b, err := os.ReadFile(mine); err != nil || string(b) != "mine\n" {
		t.Errorf("user bot lost: %q %v", b, err)
	}
	if got := files(t, prefix); len(got) != 2+len(botNames) {
		t.Errorf("files = %v (leftover staging files?)", got)
	}
	assertClean(t, r)
}

func TestPathWarning(t *testing.T) {
	srv, _ := std(t)
	u := [2]string{"Linux", "x86_64"}
	prefix := filepath.Join(t.TempDir(), "my prefix")

	r := run(t, srv, opts{uname: u, prefix: prefix})
	mustSucceed(t, r)
	if !strings.Contains(r.out, "not on your PATH") || !strings.Contains(r.out, filepath.Join(prefix, "bin")) {
		t.Errorf("no PATH warning naming %s:\n%s", filepath.Join(prefix, "bin"), r.out)
	}

	p := filepath.Join(prefix, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")
	r = run(t, srv, opts{uname: u, prefix: prefix, path: p})
	mustSucceed(t, r)
	if strings.Contains(r.out, "not on your PATH") {
		t.Errorf("PATH warning although bin is on PATH:\n%s", r.out)
	}
}

// fakeWget implements the wget options the script may use by calling curl, and
// rejects any other option.
const fakeWget = `#!/bin/sh
out=; url=
while [ $# -gt 0 ]; do
  case $1 in
    -q|--quiet|--https-only) ;;
    -O) shift; out=$1 ;;
    -*) echo "fake wget: unsupported option $1" >&2; exit 2 ;;
    *) url=$1 ;;
  esac
  shift
done
[ -n "$out" ] && [ -n "$url" ] || exit 2
exec curl --fail --silent --location -o "$out" "$url"
`

func TestWgetFallback(t *testing.T) {
	srv, _ := std(t)
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl needed by the fake wget")
	}
	// A PATH with wget but without curl: the script must fall back to wget.
	bin := t.TempDir()
	stub := strings.Replace(fakeWget, "exec curl", "exec "+curl, 1)
	if err := os.WriteFile(filepath.Join(bin, "wget"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	mp := minimalPath(t, true)
	os.Remove(filepath.Join(mp, "curl"))
	os.Remove(filepath.Join(mp, "wget"))
	path := bin + string(os.PathListSeparator) + mp
	r := run(t, srv, opts{uname: [2]string{"Linux", "x86_64"}, path: path})
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s", r.code, r.out)
	}
	assertInstalled(t, r, version, "linux", "amd64")
}
