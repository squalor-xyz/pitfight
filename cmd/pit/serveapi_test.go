// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/replay"
)

const (
	apiImpSrc   = "MOV.I $0, $1\n"
	apiBadSrc   = "MOV.I $0, $1\n  BOGUS.F $0, $1\n"
	apiHost     = "127.0.0.1:8080"
	apiMatchURL = "/api/match"
)

// apiRoots makes a bots directory and a replays directory, next to a secret outside
// both, and returns their paths. The bots are imp.s, dwarf.s (copies of the starter
// bots), bad.s (an assembler error) and notes.txt (not a bot).
func apiRoots(t *testing.T) (parent, botsDir, replaysDir string) {
	t.Helper()
	parent = t.TempDir()
	botsDir = filepath.Join(parent, "bots")
	replaysDir = filepath.Join(parent, "replays")
	for _, d := range []string{botsDir, replaysDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dwarf, err := os.ReadFile(filepath.Join("..", "..", "bots", "dwarf.s"))
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		filepath.Join(botsDir, "imp.s"):          []byte(apiImpSrc),
		filepath.Join(botsDir, "dwarf.s"):        dwarf,
		filepath.Join(botsDir, "bad.s"):          []byte(apiBadSrc),
		filepath.Join(botsDir, "notes.txt"):      []byte("not a bot"),
		filepath.Join(replaysDir, "z.jsonl"):     []byte("z"),
		filepath.Join(replaysDir, "m.jsonl"):     []byte("m"),
		filepath.Join(replaysDir, "other.txt"):   []byte("o"),
		filepath.Join(parent, "secret.s"):        []byte("DAT.F $0, $0 ; TOP SECRET\n"),
		filepath.Join(parent, "secret.jsonl"):    []byte("TOP SECRET"),
		filepath.Join(botsDir, "huge.s"):         []byte("DAT.F $0, $0\n" + strings.Repeat("; "+strings.Repeat("x", 100)+"\n", 700)),
		filepath.Join(botsDir, "also-imp.s"):     []byte(apiImpSrc),
		filepath.Join(replaysDir, "UPPER.JSONL"): []byte("u"),
	} {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(botsDir, "dir.s"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(botsDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(botsDir, "sub", "inner.s"), []byte(apiImpSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	return parent, botsDir, replaysDir
}

func apiHandler(t *testing.T, botsDir, replaysDir string) http.Handler {
	t.Helper()
	h, closeRoots, err := newServeHandler("", replaysDir, botsDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeRoots)
	return h
}

// postMatch sends a POST /api/match with the given headers (Host and Content-Type
// default to a local browser's) and body.
func postMatch(h http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, apiMatchURL, strings.NewReader(body))
	req.Host = apiHost
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type apiListing struct {
	Bots    []string `json:"bots"`
	Replays []string `json:"replays"`
}

func getList(t *testing.T, h http.Handler) apiListing {
	t.Helper()
	rec := do(h, http.MethodGet, apiHost, "/api/list")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/list = %d %q, want 200", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET /api/list Content-Type %q, want application/json", ct)
	}
	var l apiListing
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		t.Fatalf("GET /api/list: %v", err)
	}
	return l
}

// TestAPIList: the listing holds the .s files of --bots and the .jsonl files of
// --replays, sorted, and not directories, other extensions or anything else.
func TestAPIList(t *testing.T) {
	_, botsDir, replaysDir := apiRoots(t)
	h := apiHandler(t, botsDir, replaysDir)
	got := getList(t, h)
	want := apiListing{
		Bots:    []string{"also-imp.s", "bad.s", "dwarf.s", "huge.s", "imp.s"},
		Replays: []string{"m.jsonl", "z.jsonl"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listing = %+v, want %+v", got, want)
	}
	rec := do(h, http.MethodGet, apiHost, "/api/list")
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", rec.Header().Get("Cache-Control"))
	}
	if rec := do(h, http.MethodHead, apiHost, "/api/list"); rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD /api/list = %d with %d body bytes, want 200 and none", rec.Code, rec.Body.Len())
	}
}

// TestAPIListEmpty: without --bots or --replays the lists are empty arrays, never null.
func TestAPIListEmpty(t *testing.T) {
	h := apiHandler(t, "", "")
	rec := do(h, http.MethodGet, apiHost, "/api/list")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"bots":[],"replays":[]}` {
		t.Errorf("GET /api/list = %d %q, want 200 and empty lists", rec.Code, rec.Body.String())
	}
}

// TestAPIListConfined: a symlink that leaves a root, by a relative or an absolute
// target, is not listed, nor is an absolute link that points back inside; a relative
// link that stays inside is. The listing takes no parameters, so ".." in a query
// changes nothing, and it holds no absolute path.
func TestAPIListConfined(t *testing.T) {
	parent, botsDir, replaysDir := apiRoots(t)
	links := map[string]string{
		filepath.Join(botsDir, "out-rel.s"):         filepath.Join("..", "secret.s"),
		filepath.Join(botsDir, "out-abs.s"):         filepath.Join(parent, "secret.s"),
		filepath.Join(botsDir, "in-abs.s"):          filepath.Join(botsDir, "imp.s"),
		filepath.Join(botsDir, "in-rel.s"):          "imp.s",
		filepath.Join(replaysDir, "out-rel.jsonl"):  filepath.Join("..", "secret.jsonl"),
		filepath.Join(replaysDir, "out-abs.jsonl"):  filepath.Join(parent, "secret.jsonl"),
		filepath.Join(replaysDir, "dangling.jsonl"): "nowhere.jsonl",
	}
	for path, target := range links {
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
	}
	h := apiHandler(t, botsDir, replaysDir)
	got := getList(t, h)
	want := apiListing{
		Bots:    []string{"also-imp.s", "bad.s", "dwarf.s", "huge.s", "imp.s", "in-rel.s"},
		Replays: []string{"m.jsonl", "z.jsonl"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listing = %+v, want %+v", got, want)
	}
	for _, path := range []string{"/api/list?dir=..", "/api/list?bots=../", "/api/list/../../secret.s"} {
		rec := do(h, http.MethodGet, apiHost, path)
		if strings.Contains(rec.Body.String(), "TOP SECRET") || strings.Contains(rec.Body.String(), parent) {
			t.Errorf("GET %s leaked %q", path, rec.Body.String())
		}
	}
	if body := do(h, http.MethodGet, apiHost, "/api/list").Body.String(); strings.Contains(body, parent) || strings.Contains(body, "/") {
		t.Errorf("listing %q holds a path", body)
	}
}

// TestAPIMatchHash: a match of two listed bots with a seed returns the replay that
// "pit run --replay" writes for the same bots and seed, byte for byte, so the hashes
// are equal. Without a seed the seed is 0, as in pit run.
func TestAPIMatchHash(t *testing.T) {
	_, botsDir, replaysDir := apiRoots(t)
	h := apiHandler(t, botsDir, replaysDir)
	for _, tt := range []struct {
		name, body string
		seedArgs   []string
	}{
		{"seed", `{"bots":["imp.s","dwarf.s"],"seed":"18446744073709551615"}`, []string{"--seed", "18446744073709551615"}},
		{"seed 7, reversed", `{"bots":["dwarf.s","imp.s"],"seed":"7"}`, []string{"--seed", "7"}},
		{"same bot twice", `{"bots":["imp.s","imp.s"],"seed":"1"}`, []string{"--seed", "1"}},
		{"no seed", `{"bots":["imp.s","dwarf.s"]}`, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var req struct{ Bots []string }
			if err := json.Unmarshal([]byte(tt.body), &req); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "r.jsonl")
			args := append([]string{"run", filepath.Join(botsDir, req.Bots[0]), filepath.Join(botsDir, req.Bots[1]), "--replay", out}, tt.seedArgs...)
			code, stdout, stderr := runPit(args...)
			if code != 0 {
				t.Fatalf("pit run = %d, stderr %q", code, stderr)
			}
			want, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			rec := postMatch(h, tt.body, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("POST %s = %d %q, want 200", apiMatchURL, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/x-ndjson") {
				t.Errorf("Content-Type %q, want application/x-ndjson", ct)
			}
			got := rec.Body.Bytes()
			wantHash := replay.Hash(want)
			if !strings.Contains(stdout, "replay: "+wantHash) {
				t.Fatalf("pit run output %q lacks the hash %s", stdout, wantHash)
			}
			if gotHash := replay.Hash(got); gotHash != wantHash {
				t.Errorf("replay hash %s, want %s (pit run)", gotHash, wantHash)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("replay bytes differ from pit run (%d and %d bytes)", len(got), len(want))
			}
			// Reading a full-length replay back takes seconds, so one case does.
			if tt.name == "seed" {
				if _, err := replay.Read(bytes.NewReader(got)); err != nil {
					t.Errorf("the response is not a valid replay: %v", err)
				}
			}
		})
	}
}

// TestAPIMatchRefusals: the match endpoint answers a 4xx with a JSON error for a
// request it cannot run, and runs no match.
func TestAPIMatchRefusals(t *testing.T) {
	parent, botsDir, replaysDir := apiRoots(t)
	if err := os.Symlink(filepath.Join(parent, "secret.s"), filepath.Join(botsDir, "out-abs.s")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "secret.s"), filepath.Join(botsDir, "out-rel.s")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	h := apiHandler(t, botsDir, replaysDir)
	tests := []struct {
		name, body string
		want       int
	}{
		{"unlisted name", `{"bots":["imp.s","nope.s"],"seed":"1"}`, http.StatusNotFound},
		{"not a bot file", `{"bots":["imp.s","notes.txt"],"seed":"1"}`, http.StatusNotFound},
		{"directory", `{"bots":["imp.s","dir.s"],"seed":"1"}`, http.StatusNotFound},
		{"parent path", `{"bots":["imp.s","../secret.s"],"seed":"1"}`, http.StatusNotFound},
		{"parent path, encoded", `{"bots":["imp.s","..%2fsecret.s"],"seed":"1"}`, http.StatusNotFound},
		{"absolute path", `{"bots":["imp.s","` + filepath.Join(parent, "secret.s") + `"],"seed":"1"}`, http.StatusNotFound},
		{"dot path", `{"bots":["imp.s","./imp.s"],"seed":"1"}`, http.StatusNotFound},
		{"file in a subdirectory", `{"bots":["imp.s","sub/inner.s"],"seed":"1"}`, http.StatusNotFound},
		{"backslash path", `{"bots":["imp.s","..\\secret.s"],"seed":"1"}`, http.StatusNotFound},
		{"symlink out, relative", `{"bots":["imp.s","out-rel.s"],"seed":"1"}`, http.StatusNotFound},
		{"symlink out, absolute", `{"bots":["out-abs.s","imp.s"],"seed":"1"}`, http.StatusNotFound},
		{"one bot", `{"bots":["imp.s"],"seed":"1"}`, http.StatusBadRequest},
		{"three bots", `{"bots":["imp.s","imp.s","imp.s"],"seed":"1"}`, http.StatusBadRequest},
		{"no bots", `{"seed":"1"}`, http.StatusBadRequest},
		{"seed negative", `{"bots":["imp.s","imp.s"],"seed":"-1"}`, http.StatusBadRequest},
		{"seed not a number", `{"bots":["imp.s","imp.s"],"seed":"abc"}`, http.StatusBadRequest},
		{"seed too big", `{"bots":["imp.s","imp.s"],"seed":"18446744073709551616"}`, http.StatusBadRequest},
		{"seed hexadecimal", `{"bots":["imp.s","imp.s"],"seed":"0x10"}`, http.StatusBadRequest},
		{"seed empty", `{"bots":["imp.s","imp.s"],"seed":""}`, http.StatusBadRequest},
		{"seed a JSON number", `{"bots":["imp.s","imp.s"],"seed":1}`, http.StatusBadRequest},
		{"unknown field", `{"bots":["imp.s","imp.s"],"seed":"1","cycle_limit":1000000}`, http.StatusBadRequest},
		{"not JSON", `bots=imp.s`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
		{"trailing data", `{"bots":["imp.s","imp.s"]} {}`, http.StatusBadRequest},
		{"body too large", `{"bots":["imp.s","imp.s"],"pad":"` + strings.Repeat("x", 1<<16) + `"}`, http.StatusRequestEntityTooLarge},
		{"bot file too large", `{"bots":["imp.s","huge.s"],"seed":"1"}`, http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postMatch(h, tt.body, nil)
			if rec.Code != tt.want {
				t.Errorf("status %d, want %d (body %.200q)", rec.Code, tt.want, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type %q, want application/json", ct)
			}
			var e struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error == "" {
				t.Errorf("body %.200q is not a JSON error: %v", rec.Body.String(), err)
			}
			if strings.Contains(rec.Body.String(), "TOP SECRET") || strings.Contains(rec.Body.String(), parent) {
				t.Errorf("body %.200q leaks the secret or a path", rec.Body.String())
			}
		})
	}
}

// TestAPIMatchAssemblerError: an error in a bot is a 422 whose JSON carries the bot's
// listed name, the line and the column, and a message that names the file as listed
// (no directory).
func TestAPIMatchAssemblerError(t *testing.T) {
	_, botsDir, replaysDir := apiRoots(t)
	h := apiHandler(t, botsDir, replaysDir)
	rec := postMatch(h, `{"bots":["imp.s","bad.s"],"seed":"1"}`, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422 (body %q)", rec.Code, rec.Body.String())
	}
	var e struct {
		Error   string `json:"error"`
		Bot     string `json:"bot"`
		Line    int    `json:"line"`
		Col     int    `json:"col"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Bot != "bad.s" || e.Line != 2 || e.Col != 3 || e.Message != `unknown opcode "BOGUS"` || e.Error != `bad.s:2:3: unknown opcode "BOGUS"` {
		t.Errorf("error = %+v, want bot bad.s, line 2, col 3, message %q, error %q", e, `unknown opcode "BOGUS"`, `bad.s:2:3: unknown opcode "BOGUS"`)
	}
}

// TestAPIMatchRefusedMatch: a match the arena refuses (here a bot longer than the
// core fits) is a 422 with the arena's message.
func TestAPIMatchRefusedMatch(t *testing.T) {
	_, botsDir, replaysDir := apiRoots(t)
	// Two bots of 4500 cells do not fit the core of 8000 cells with the separation
	// between them. Each file is under the size cap.
	long := strings.Repeat("DAT.F 0,0\n", 4500)
	if err := os.WriteFile(filepath.Join(botsDir, "long.s"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	h, closeRoots, err := newServeHandler("", replaysDir, botsDir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoots()
	rec := postMatch(h, `{"bots":["long.s","long.s"],"seed":"1"}`, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422 (body %.200q)", rec.Code, rec.Body.String())
	}
}

// TestAPIMatchGuards: the match endpoint is for a page of this server only. A
// foreign Host, a foreign or opaque Origin, a cross-site fetch, a form content type
// and a wrong method each get the stated refusal with the valid body, and run no
// match (the response holds no replay).
func TestAPIMatchGuards(t *testing.T) {
	_, botsDir, replaysDir := apiRoots(t)
	h := apiHandler(t, botsDir, replaysDir)
	const body = `{"bots":["imp.s","dwarf.s"],"seed":"1"}`
	tests := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"plain", nil, http.StatusOK},
		{"same origin", map[string]string{"Origin": "http://" + apiHost}, http.StatusOK},
		{"json with charset", map[string]string{"Content-Type": "application/json; charset=utf-8"}, http.StatusOK},
		{"same-origin fetch", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"origin with a path", map[string]string{"Origin": "http://" + apiHost + "/x"}, http.StatusForbidden},
		{"same host, other scheme", map[string]string{"Origin": "https://" + apiHost}, http.StatusForbidden},
		{"foreign origin", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"foreign origin on a local port", map[string]string{"Origin": "http://127.0.0.1:9999"}, http.StatusForbidden},
		{"other local name", map[string]string{"Origin": "http://localhost:8080"}, http.StatusForbidden},
		{"opaque origin", map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"cross-site fetch", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"same-site fetch", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"form content type", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
		{"text/plain", map[string]string{"Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		{"multipart", map[string]string{"Content-Type": "multipart/form-data; boundary=x"}, http.StatusUnsupportedMediaType},
		{"no content type", map[string]string{"Content-Type": ""}, http.StatusUnsupportedMediaType},
		{"json lookalike", map[string]string{"Content-Type": "application/jsonx"}, http.StatusUnsupportedMediaType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postMatch(h, body, tt.headers)
			if rec.Code != tt.want {
				t.Errorf("status %d, want %d (body %.100q)", rec.Code, tt.want, rec.Body.String())
			}
			if tt.want != http.StatusOK && strings.Contains(rec.Body.String(), `"cycle"`) {
				t.Errorf("a refused request got a replay")
			}
		})
	}

	// A foreign Host is refused with no body, before anything else.
	for _, host := range []string{"evil.example", "evil.example:8080", "10.0.0.5:8080", ""} {
		req := httptest.NewRequest(http.MethodPost, apiMatchURL, strings.NewReader(body))
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || rec.Body.Len() != 0 {
			t.Errorf("POST with Host %q = %d with %d body bytes, want 403 and none", host, rec.Code, rec.Body.Len())
		}
		req = httptest.NewRequest(http.MethodGet, "/api/list", nil)
		req.Host = host
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || rec.Body.Len() != 0 {
			t.Errorf("GET /api/list with Host %q = %d with %d body bytes, want 403 and none", host, rec.Code, rec.Body.Len())
		}
	}

	// Methods: the endpoint takes POST only, the listing GET and HEAD only.
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		rec := do(h, method, apiHost, apiMatchURL)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
			t.Errorf("%s %s = %d, Allow %q, want 405 and POST", method, apiMatchURL, rec.Code, rec.Header().Get("Allow"))
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		rec := do(h, method, apiHost, "/api/list")
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s /api/list = %d, Allow %q, want 405 and %q", method, rec.Code, rec.Header().Get("Allow"), "GET, HEAD")
		}
	}
	// POST is still refused everywhere else.
	for _, path := range []string{"/replays/m.jsonl", "/", "/js/main.js"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Host = apiHost
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed || rr.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("POST %s = %d, Allow %q, want 405 and %q", path, rr.Code, rr.Header().Get("Allow"), "GET, HEAD")
		}
	}
}

// TestAPIMatchOneAtATime: while a match runs, another is refused 503 with Retry-After
// at once, and it is accepted again when the first has ended.
func TestAPIMatchOneAtATime(t *testing.T) {
	_, botsDir, replaysDir := apiRoots(t)
	botsFS, closeBots := openTestFS(t, botsDir)
	defer closeBots()
	replaysFS, closeReplays := openTestFS(t, replaysDir)
	defer closeReplays()
	a := newAPI(botsFS, replaysFS, defaultAPILimits())
	mux := http.NewServeMux()
	a.mount(mux)
	h := localOnly(mux)

	release := a.acquire()
	if release == nil {
		t.Fatal("the first acquire was refused")
	}
	if a.acquire() != nil {
		t.Error("a second acquire succeeded while the first is held")
	}
	rec := postMatch(h, `{"bots":["imp.s","dwarf.s"],"seed":"1"}`, nil)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Errorf("busy: status %d, Retry-After %q, want 503 with a Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	release()
	if rec := postMatch(h, `{"bots":["imp.s","dwarf.s"],"seed":"1"}`, nil); rec.Code != http.StatusOK {
		t.Errorf("after release: status %d, want 200", rec.Code)
	}
}

// TestAPIMatchLimits: the cycle limit of a served match is the API's limit, 80000
// (the pit run default) unless the limits say otherwise.
func TestAPIMatchLimits(t *testing.T) {
	if got, want := defaultAPILimits().CycleLimit, arena.DefaultConfig().CycleLimit; got != want {
		t.Errorf("default cycle limit %d, want %d", got, want)
	}
	_, botsDir, replaysDir := apiRoots(t)
	botsFS, closeBots := openTestFS(t, botsDir)
	defer closeBots()
	lim := defaultAPILimits()
	lim.CycleLimit = 50
	a := newAPI(botsFS, nil, lim)
	out, err := a.match("imp.s", "imp.s", 1)
	if err != nil {
		t.Fatal(err)
	}
	if limit, cycles := replayLimitAndCycles(t, out); limit != 50 || cycles != 50 {
		t.Errorf("cycle limit %d, match ran %d cycles, want 50 and 50", limit, cycles)
	}
	// The default limits run the full default match.
	h := apiHandler(t, botsDir, replaysDir)
	rec := postMatch(h, `{"bots":["imp.s","imp.s"],"seed":"1"}`, nil)
	if limit, cycles := replayLimitAndCycles(t, rec.Body.Bytes()); limit != 80000 || cycles != 80000 {
		t.Errorf("cycle limit %d, match ran %d cycles, want 80000 and 80000", limit, cycles)
	}
}

// TestAPIMatchError: the Go entry point returns an *apiError with the status, so a
// caller that is not HTTP (pit play) can use it.
func TestAPIMatchError(t *testing.T) {
	_, botsDir, _ := apiRoots(t)
	botsFS, closeBots := openTestFS(t, botsDir)
	defer closeBots()
	a := newAPI(botsFS, nil, defaultAPILimits())
	_, err := a.match("imp.s", "nope.s", 1)
	if e, ok := err.(*apiError); !ok || e.Status != http.StatusNotFound {
		t.Errorf("unlisted bot: error %v, want an *apiError with status 404", err)
	}
	_, err = a.match("imp.s", "bad.s", 1)
	if e, ok := err.(*apiError); !ok || e.Status != http.StatusUnprocessableEntity || e.Line != 2 || e.Col != 3 || e.Bot != "bad.s" {
		t.Errorf("assembler error: error %+v, want 422, bad.s, line 2, col 3", err)
	}
}

// TestDefaultBotsDir: the default for --bots is "bots" under the working directory
// if that is a directory, and nothing otherwise.
func TestDefaultBotsDir(t *testing.T) {
	base := t.TempDir()
	if got := defaultBotsDir(base); got != "" {
		t.Errorf("no bots directory: %q, want none", got)
	}
	if err := os.WriteFile(filepath.Join(base, "bots"), []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := defaultBotsDir(base); got != "" {
		t.Errorf("bots is a file: %q, want none", got)
	}
	if err := os.Remove(filepath.Join(base, "bots")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "bots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := defaultBotsDir(base), filepath.Join(base, "bots"); got != want {
		t.Errorf("bots directory: %q, want %q", got, want)
	}
}

// TestServeBotsFlag: a missing --bots directory is a command error, and the command
// serves the listing and a match over a real connection.
func TestServeBotsFlag(t *testing.T) {
	var out, errb bytes.Buffer
	missing := filepath.Join(t.TempDir(), "nope")
	// The context ends a server that wrongly starts, so that the test fails instead of hanging.
	short, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if code := serve(short, []string{"--bots", missing}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "pit serve:") {
		t.Errorf("missing --bots = %d, stderr %q, want 1 and a message", code, errb.String())
	}

	_, botsDir, replaysDir := apiRoots(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout, stderr := newSyncBuffer(), newSyncBuffer()
	done := make(chan int, 1)
	go func() {
		done <- serve(ctx, []string{"--port", "0", "--bots", botsDir, "--replays", replaysDir}, stdout, stderr)
	}()
	var url string
	for deadline := time.Now().Add(5 * time.Second); url == "" && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		url = serveURLRe.FindString(contents(stdout))
	}
	if url == "" {
		cancel()
		t.Fatalf("no URL on stdout %q (stderr %q)", contents(stdout), contents(stderr))
	}
	resp, err := http.Get(url + "api/list")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var l apiListing
	err = json.NewDecoder(resp.Body).Decode(&l)
	resp.Body.Close()
	if err != nil || !reflect.DeepEqual(l.Replays, []string{"m.jsonl", "z.jsonl"}) || len(l.Bots) != 5 {
		t.Errorf("listing over the wire = %+v, %v", l, err)
	}
	resp, err = http.Post(url+"api/match", "application/json", strings.NewReader(`{"bots":["imp.s","dwarf.s"],"seed":"5"}`))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(data, []byte(`{"t":"header","version":1,`)) {
		t.Errorf("match over the wire = %d %.80q, want 200 and a replay", resp.StatusCode, data)
	}
	// A browser page of another site sends its Origin.
	req, _ := http.NewRequest(http.MethodPost, url+"api/match", strings.NewReader(`{"bots":["imp.s","dwarf.s"],"seed":"5"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("match from a foreign Origin over the wire = %d, want 403", resp.StatusCode)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code %d, want 0 (stderr %q)", code, contents(stderr))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its context ended")
	}
}

// openTestFS opens dir as the file system the API reads, the way newServeHandler does.
func openTestFS(t *testing.T, dir string) (fsys rootFS, closeFn func()) {
	t.Helper()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return rootFS{r.FS()}, func() { r.Close() }
}

// replayLimitAndCycles reads the cycle limit of a replay's header and the number of
// cycles of its end line without parsing the whole replay.
func replayLimitAndCycles(t *testing.T, data []byte) (limit, cycles int) {
	t.Helper()
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	var head struct {
		Config struct {
			CycleLimit int `json:"cycle_limit"`
		} `json:"config"`
	}
	var end struct {
		Cycles int `json:"cycles"`
	}
	if err := json.Unmarshal(lines[0], &head); err != nil {
		t.Fatalf("header line: %v", err)
	}
	if err := json.Unmarshal(lines[len(lines)-1], &end); err != nil {
		t.Fatalf("end line: %v", err)
	}
	return head.Config.CycleLimit, end.Cycles
}

// TestAPIListLimitsAndNames: a list is cut at MaxListed names and says so, and a name
// that cannot survive JSON and a URL (control characters, invalid UTF-8) is not listed.
func TestAPIListLimitsAndNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.s", "b.s", "c.s", "bad\x01name.s", "bad\xffname.s", ".s"} {
		// A file system may refuse a name (macOS refuses invalid UTF-8); then it cannot be listed either.
		if err := os.WriteFile(filepath.Join(dir, name), []byte(apiImpSrc), 0o644); err != nil {
			t.Logf("cannot create %q here: %v", name, err)
		}
	}
	fsys, closeFS := openTestFS(t, dir)
	defer closeFS()
	lim := defaultAPILimits()
	l, err := newAPI(fsys, nil, lim).listing()
	if err != nil || !reflect.DeepEqual(l.Bots, []string{"a.s", "b.s", "c.s"}) || l.Truncated {
		t.Errorf("listing = %+v, %v, want a.s b.s c.s and not truncated", l, err)
	}
	lim.MaxListed = 2
	l, err = newAPI(fsys, nil, lim).listing()
	if err != nil || !reflect.DeepEqual(l.Bots, []string{"a.s", "b.s"}) || !l.Truncated {
		t.Errorf("limited listing = %+v, %v, want a.s b.s and truncated", l, err)
	}
	// Exactly MaxListed names are not a cut.
	lim.MaxListed = 3
	if l, _ = newAPI(fsys, nil, lim).listing(); l.Truncated || len(l.Bots) != 3 {
		t.Errorf("listing at the limit = %+v, want 3 names and not truncated", l)
	}
}

// TestServeUsageBots: the usage text documents --bots.
func TestServeUsageBots(t *testing.T) {
	var out, errb bytes.Buffer
	if code := serve(context.Background(), []string{"--help"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "--bots DIR") {
		t.Errorf("serve --help = %d, stdout %q: want --bots DIR documented", code, out.String())
	}
}

// TestPlainName: names that survive JSON and a URL, and those that do not.
func TestPlainName(t *testing.T) {
	for name, want := range map[string]bool{
		"imp.s":       true,
		"my bot.s":    true,
		"bot-é.s":     true,
		"a#b?c%d.s":   true,
		"bad\x01.s":   false,
		"bad\n.s":     false,
		"bad\x7f.s":   false,
		"bad\xff.s":   false,
		"bad\xc0\x80": false,
	} {
		if got := plainName(name); got != want {
			t.Errorf("plainName(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestAPIFifo: a named pipe called x.s (or x.jsonl), or a symbolic link to one, is not
// a file: it is not listed, a match naming it is a 404, and /replays/ answers 404 for
// it, all at once. Opening a pipe would block until some process writes to it.
func TestAPIFifo(t *testing.T) {
	_, botsDir, replaysDir := apiRoots(t)
	var pipes []string
	for _, p := range []string{filepath.Join(botsDir, "x.s"), filepath.Join(replaysDir, "x.jsonl")} {
		if out, err := exec.Command("mkfifo", p).CombinedOutput(); err != nil {
			t.Skipf("no named pipes here: %v %s", err, out)
		}
		pipes = append(pipes, p)
	}
	// Whatever the code under test does, release a reader blocked on a pipe at the end.
	t.Cleanup(func() {
		for _, p := range pipes {
			if f, err := os.OpenFile(p, os.O_RDWR, 0); err == nil {
				f.Close()
			}
		}
	})
	if err := os.Symlink("x.s", filepath.Join(botsDir, "link.s")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := os.Symlink(filepath.Join(botsDir, "x.s"), filepath.Join(botsDir, "link-abs.s")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	h := apiHandler(t, botsDir, replaysDir)

	type result struct {
		list       apiListing
		listCode   int
		matchCode  int
		linkCode   int
		replayCode int
	}
	done := make(chan result, 1)
	go func() {
		var r result
		rec := do(h, http.MethodGet, apiHost, "/api/list")
		r.listCode = rec.Code
		json.Unmarshal(rec.Body.Bytes(), &r.list)
		r.matchCode = postMatch(h, `{"bots":["imp.s","x.s"],"seed":"1"}`, nil).Code
		r.linkCode = postMatch(h, `{"bots":["link.s","imp.s"],"seed":"1"}`, nil).Code
		r.replayCode = do(h, http.MethodGet, apiHost, "/replays/x.jsonl").Code
		done <- r
	}()
	select {
	case r := <-done:
		want := apiListing{
			Bots:    []string{"also-imp.s", "bad.s", "dwarf.s", "huge.s", "imp.s"},
			Replays: []string{"m.jsonl", "z.jsonl"},
		}
		if r.listCode != http.StatusOK || !reflect.DeepEqual(r.list, want) {
			t.Errorf("listing = %d %+v, want 200 %+v", r.listCode, r.list, want)
		}
		if r.matchCode != http.StatusNotFound || r.linkCode != http.StatusNotFound {
			t.Errorf("match with a pipe = %d, with a link to a pipe = %d, want 404 and 404", r.matchCode, r.linkCode)
		}
		if r.replayCode != http.StatusNotFound {
			t.Errorf("GET /replays/x.jsonl (a pipe) = %d, want 404", r.replayCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a request blocked on a named pipe")
	}
}

// notRegularFS answers every Open with a file that Stat reports as a named pipe: a
// file that was a regular one when the directory was listed and was swapped since.
type notRegularFS struct{ fs.FS }

func (n notRegularFS) Open(name string) (fs.File, error) {
	f, err := n.FS.Open(name)
	if err != nil || name == "." {
		return f, err
	}
	return pipeFile{f}, nil
}

type pipeFile struct{ fs.File }

func (p pipeFile) Stat() (fs.FileInfo, error) {
	info, err := p.File.Stat()
	return pipeInfo{info}, err
}

type pipeInfo struct{ fs.FileInfo }

func (pipeInfo) Mode() fs.FileMode { return fs.ModeNamedPipe | 0o644 }

// TestReadBotChecksTheOpenedFile: a bot that is not a regular file once opened is
// refused, whatever the listing said.
func TestReadBotChecksTheOpenedFile(t *testing.T) {
	bots := notRegularFS{fstest.MapFS{"x.s": {Data: []byte(apiImpSrc)}, "imp.s": {Data: []byte(apiImpSrc)}}}
	a := newAPI(bots, nil, defaultAPILimits())
	_, err := a.match("imp.s", "x.s", 1)
	if e, ok := err.(*apiError); !ok || e.Status != http.StatusNotFound {
		t.Errorf("match with a bot that opens as a pipe: error %v, want an *apiError with status 404", err)
	}
}
