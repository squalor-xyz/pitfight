// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// do sends one request with the given method and Host header to h.
func do(h http.Handler, method, host, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// get requests path from h as a browser on the local machine would (Host is
// 127.0.0.1) and returns the status, content type and body.
func get(t *testing.T, h http.Handler, path string) (int, string, string) {
	t.Helper()
	rec := do(h, http.MethodGet, "127.0.0.1:8080", path)
	return rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()
}

// serveRoots makes a replay root with one file and a secret file next to it, outside
// the root.
func serveRoots(t *testing.T) (parent, root string) {
	t.Helper()
	parent = t.TempDir()
	root = filepath.Join(parent, "replays")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		filepath.Join(root, "m.jsonl"):   "replay bytes",
		filepath.Join(parent, "secret"):  "TOP SECRET",
		filepath.Join(root, "sub-a.txt"): "a",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return parent, root
}

// TestServeFile: a file under the replay root is served at /replays/.
func TestServeFile(t *testing.T) {
	_, root := serveRoots(t)
	h, closeRoots, err := newServeHandler("", root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoots()
	code, _, body := get(t, h, "/replays/m.jsonl")
	if code != http.StatusOK || body != "replay bytes" {
		t.Errorf("GET /replays/m.jsonl = %d %q, want 200 %q", code, body, "replay bytes")
	}
	if code, _, _ := get(t, h, "/replays/missing.jsonl"); code != http.StatusNotFound {
		t.Errorf("missing file: status %d, want 404", code)
	}
}

// TestServeRefusesOutsideRoot: paths that climb out of the replay root, in plain,
// encoded and backslash spellings, and a symlink that points out of it, never
// deliver the file outside.
func TestServeRefusesOutsideRoot(t *testing.T) {
	parent, root := serveRoots(t)
	if err := os.Symlink(filepath.Join(parent, "secret"), filepath.Join(root, "link")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	h, closeRoots, err := newServeHandler("", root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoots()
	for _, path := range []string{
		"/replays/../secret",
		"/replays/%2e%2e/secret",
		"/replays/..%2fsecret",
		"/replays/..%5csecret",
		"/replays/sub/../../secret",
		"/replays/link",
		"/secret",
	} {
		code, _, body := get(t, h, path)
		if strings.Contains(body, "TOP SECRET") || code == http.StatusOK {
			t.Errorf("GET %s = %d %q: the file outside the root was served", path, code, body)
		}
	}
}

// TestServeSymlinks: a symlink that leaves the root, by a relative or an absolute
// target, and an absolute symlink that points back inside it are all answered 404:
// not served, and not a server error.
func TestServeSymlinks(t *testing.T) {
	parent, root := serveRoots(t)
	for name, target := range map[string]string{
		"out-rel": filepath.Join("..", "secret"),
		"out-abs": filepath.Join(parent, "secret"),
		"in-abs":  filepath.Join(root, "m.jsonl"),
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
	}
	h, closeRoots, err := newServeHandler("", root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoots()
	for _, name := range []string{"out-rel", "out-abs", "in-abs"} {
		code, _, body := get(t, h, "/replays/"+name)
		if code != http.StatusNotFound || strings.Contains(body, "TOP SECRET") || strings.Contains(body, "replay bytes") {
			t.Errorf("GET /replays/%s = %d %q, want 404 and no file content", name, code, body)
		}
	}
}

// TestServeHostCheck: only requests whose Host names this machine are answered, so
// a web page on another site cannot read replays through a DNS-rebinding name.
func TestServeHostCheck(t *testing.T) {
	_, root := serveRoots(t)
	h, closeRoots, err := newServeHandler("", root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoots()
	tests := []struct {
		host string
		want int
	}{
		{"127.0.0.1:8080", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"127.1.2.3:80", http.StatusOK},
		{"localhost:8080", http.StatusOK},
		{"LocalHost", http.StatusOK},
		{"[::1]:8080", http.StatusOK},
		{"[::1]", http.StatusOK},
		{"evil.example:8080", http.StatusForbidden},
		{"evil.example", http.StatusForbidden},
		{"127.0.0.1.evil.example:8080", http.StatusForbidden},
		{"localhost.evil.example", http.StatusForbidden},
		{"10.0.0.5:8080", http.StatusForbidden},
		{"[::]:8080", http.StatusForbidden},
		{"", http.StatusForbidden},
	}
	for _, tt := range tests {
		for _, path := range []string{"/", "/replays/m.jsonl"} {
			rec := do(h, http.MethodGet, tt.host, path)
			if rec.Code != tt.want {
				t.Errorf("Host %q GET %s = %d, want %d", tt.host, path, rec.Code, tt.want)
			}
			if tt.want != http.StatusOK && rec.Body.Len() != 0 {
				t.Errorf("Host %q GET %s: refused with a body %q", tt.host, path, rec.Body.String())
			}
		}
	}
}

// TestServeMethods: GET and HEAD are served; anything else is 405 with an Allow header.
func TestServeMethods(t *testing.T) {
	_, root := serveRoots(t)
	h, closeRoots, err := newServeHandler("", root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoots()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := do(h, method, "127.0.0.1:8080", "/replays/m.jsonl")
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", method, rec.Code)
		}
		if method == http.MethodHead && rec.Body.Len() != 0 {
			t.Errorf("HEAD returned a body %q", rec.Body.String())
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		rec := do(h, method, "127.0.0.1:8080", "/replays/m.jsonl")
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s = %d, Allow %q, want 405 and %q", method, rec.Code, rec.Header().Get("Allow"), "GET, HEAD")
		}
	}
	// A wrong host is refused before the method is looked at.
	if rec := do(h, http.MethodPost, "evil.example", "/"); rec.Code != http.StatusForbidden {
		t.Errorf("POST with a foreign Host = %d, want 403", rec.Code)
	}
}

// TestServeViewer: the viewer files built into pit are served at / with the types a
// browser needs for ES modules, and --web replaces them with a directory.
func TestServeViewer(t *testing.T) {
	h, closeRoots, err := newServeHandler("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoots()
	code, ct, body := get(t, h, "/")
	if code != http.StatusOK || !strings.Contains(ct, "text/html") || !strings.Contains(body, "pitfight") {
		t.Errorf("GET / = %d %q %.60q, want 200 text/html with the viewer page", code, ct, body)
	}
	for _, p := range []string{"/js/main.js", "/js/replay.js", "/js/model.js", "/js/player.js", "/js/render.js"} {
		code, ct, _ := get(t, h, p)
		if code != http.StatusOK || !strings.Contains(ct, "javascript") {
			t.Errorf("GET %s = %d %q, want 200 with a javascript type", p, code, ct)
		}
	}
	if code, _, _ := get(t, h, "/style.css"); code != http.StatusOK {
		t.Errorf("GET /style.css = %d, want 200", code)
	}
	// Test files are not part of the built-in viewer.
	if code, _, _ := get(t, h, "/test/support.js"); code == http.StatusOK {
		t.Errorf("GET /test/support.js = %d, want it not served", code)
	}
	// No replay root, no /replays/.
	if code, _, _ := get(t, h, "/replays/m.jsonl"); code != http.StatusNotFound {
		t.Errorf("GET /replays/m.jsonl without a root = %d, want 404", code)
	}

	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("custom page"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2, close2, err := newServeHandler(web, "")
	if err != nil {
		t.Fatal(err)
	}
	defer close2()
	if code, _, body := get(t, h2, "/"); code != http.StatusOK || body != "custom page" {
		t.Errorf("GET / with --web = %d %q, want 200 %q", code, body, "custom page")
	}
}

// TestServeBadRoots: a missing directory is an error, not a server that serves nothing.
func TestServeBadRoots(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if _, _, err := newServeHandler(missing, ""); err == nil {
		t.Error("missing --web: want an error")
	}
	if _, _, err := newServeHandler("", missing); err == nil {
		t.Error("missing --replays: want an error")
	}
}

// contents returns what has been written to b so far.
func contents(b *syncBuffer) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var serveURLRe = regexp.MustCompile(`http://127\.0\.0\.1:\d+/`)

// TestServeCommand starts the command on a free port, fetches the page over a real
// connection and stops it with the context.
func TestServeCommand(t *testing.T) {
	_, root := serveRoots(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout, stderr := newSyncBuffer(), newSyncBuffer()
	done := make(chan int, 1)
	go func() { done <- serve(ctx, []string{"--port", "0", "--replays", root}, stdout, stderr) }()

	var url string
	for deadline := time.Now().Add(5 * time.Second); url == "" && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		url = serveURLRe.FindString(contents(stdout))
	}
	if url == "" {
		cancel()
		t.Fatalf("no URL on stdout %q (stderr %q)", contents(stdout), contents(stderr))
	}
	for _, tt := range []struct{ path, want string }{{"", "pitfight"}, {"replays/m.jsonl", "replay bytes"}} {
		resp, err := http.Get(url + tt.path)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), tt.want) {
			t.Errorf("GET %s%s = %d %.60q, want 200 containing %q", url, tt.path, resp.StatusCode, body, tt.want)
		}
	}
	// The server keeps running until its context ends.
	select {
	case code := <-done:
		t.Fatalf("serve returned %d before its context ended", code)
	case <-time.After(300 * time.Millisecond):
	}
	if resp, err := http.Get(url); err != nil {
		t.Errorf("GET %s after a pause: %v", url, err)
	} else {
		resp.Body.Close()
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code %d after the context ended, want 0 (stderr %q)", code, contents(stderr))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context ended")
	}
}

// TestServeUsage: flag errors are usage errors (2), a missing directory is a
// command error (1), and --help prints the usage on stdout.
func TestServeUsage(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"help", []string{"--help"}, 0, "usage: pit serve", ""},
		{"unknown flag", []string{"--bogus"}, 2, "", "usage: pit serve"},
		{"stray argument", []string{"extra"}, 2, "", "usage: pit serve"},
		{"port too big", []string{"--port", "70000"}, 2, "", "--port"},
		{"port negative", []string{"--port", "-1"}, 2, "", "--port"},
		{"missing replays dir", []string{"--replays", missing}, 1, "", "pit serve:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := serve(context.Background(), tt.args, &out, &errb)
			if code != tt.wantCode || !strings.Contains(out.String(), tt.wantOut) || !strings.Contains(errb.String(), tt.wantErr) {
				t.Errorf("serve %v = %d, stdout %q, stderr %q; want %d, %q, %q", tt.args, code, out.String(), errb.String(), tt.wantCode, tt.wantOut, tt.wantErr)
			}
		})
	}
}

// TestServeInUsage: pit's own usage lists the command and dispatch reaches it.
func TestServeInUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"help"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "\n  serve ") {
		t.Errorf("help = %d, stdout %q: want the serve command listed", code, out.String())
	}
	out.Reset()
	errb.Reset()
	if code := run([]string{"serve", "--help"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "usage: pit serve") {
		t.Errorf("serve --help via run = %d, stdout %q", code, out.String())
	}
}
