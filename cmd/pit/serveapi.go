// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/squalor-xyz/pitfight/internal/arena"
	"github.com/squalor-xyz/pitfight/internal/asm"
	"github.com/squalor-xyz/pitfight/internal/replay"
)

// The two endpoints of the start page of "pit serve":
//
//	GET  /api/list   {"bots":["dwarf.s","imp.s"],"replays":["m.jsonl"]}
//	POST /api/match  {"bots":["imp.s","dwarf.s"],"seed":"42"} -> the replay
//
// The listing holds the names of the regular files ending in .s in the bots
// directory and in .jsonl in the replays directory, sorted, as base names only (the
// replay NAME is then served at /replays/NAME). A match names exactly two of the
// listed bots; the name is looked up in the listing and never joined into a path
// from the request. The seed is a decimal string of 0 to 2^64-1 (JavaScript numbers
// lose precision above 2^53), absent meaning 0 as in "pit run". The answer is the
// replay "pit run --replay" writes for those bots and seed with the default core
// configuration (docs/spec/replay.md), content type application/x-ndjson. An error is
// a JSON object {"error": text} with, for an error in a bot's source, also "bot",
// "line", "col" and "message".
//
// api does not know HTTP routing or the Host check: mount adds the two endpoints
// to a mux behind localOnly, and match is plain Go for a caller that is not a web
// page.

// apiLimits bounds the work and the data of one request.
type apiLimits struct {
	CycleLimit   int   // cycle limit of every match run
	MaxBodyBytes int64 // size of the body of a match request
	MaxBotBytes  int64 // size of one bot source file
	MaxListed    int   // names per list
}

// defaultAPILimits returns the limits of "pit serve": the default cycle limit of
// "pit run" (so a match ends after 80000 cycles at most) and sizes far above any
// real request or bot.
func defaultAPILimits() apiLimits {
	return apiLimits{CycleLimit: arena.DefaultConfig().CycleLimit, MaxBodyBytes: 4 << 10, MaxBotBytes: 64 << 10, MaxListed: 1000}
}

// apiError is a refused request: the HTTP status and the message, and for an error in
// a bot's source also the bot and the position.
type apiError struct {
	Status    int
	Msg       string // the whole message, "bot.s:2:3: unknown opcode "X"" for a source error
	Bot       string // the listed name of the bot with a source error
	Line, Col int    // its position, from 1
	Message   string // the assembler's message without the position
}

func (e *apiError) Error() string { return e.Msg }

func newAPIError(status int, format string, args ...any) *apiError {
	return &apiError{Status: status, Msg: fmt.Sprintf(format, args...)}
}

// api serves the bots of one directory and the replays of another. A nil file system
// is a directory that was not given, which lists as empty.
type api struct {
	bots, replays fs.FS
	lim           apiLimits
	slot          chan struct{} // one match at a time: a token in slot means a match is running
}

// newAPI returns the API over the two file systems. They are read through their
// names only; pass file systems that refuse a path that leaves them, such as an
// os.Root's.
func newAPI(bots, replays fs.FS, lim apiLimits) *api {
	return &api{bots: bots, replays: replays, lim: lim, slot: make(chan struct{}, 1)}
}

// mount adds /api/list and /api/match to mux. Wrap the mux in localOnly: the
// Host check is not done here.
func (a *api) mount(mux *http.ServeMux) {
	mux.HandleFunc("/api/list", a.handleList)
	mux.HandleFunc("/api/match", a.handleMatch)
}

// acquire takes the right to run a match, or returns nil if one is running. The caller
// calls the returned function when its match has ended.
func (a *api) acquire() func() {
	select {
	case a.slot <- struct{}{}:
		return func() { <-a.slot }
	default:
		return nil
	}
}

// list returns the names of the regular files of fsys that end in suffix, sorted.
// A symbolic link counts if fs.Stat through fsys shows a regular file, so one that
// leaves the root, a dangling one and one to a pipe do not. Nothing is opened: opening
// a named pipe would block. More than MaxListed names are cut and reported.
func (a *api) list(fsys fs.FS, suffix string) (names []string, truncated bool, err error) {
	names = []string{}
	if fsys == nil {
		return names, false, nil
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, false, err
	}
	for _, e := range entries {
		name := e.Name()
		if len(name) <= len(suffix) || !strings.HasSuffix(name, suffix) || !plainName(name) {
			continue
		}
		regular := e.Type().IsRegular()
		if e.Type()&fs.ModeSymlink != 0 {
			info, err := fs.Stat(fsys, name)
			regular = err == nil && info.Mode().IsRegular()
		}
		if !regular {
			continue
		}
		if len(names) == a.lim.MaxListed {
			return names, true, nil
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names, false, nil
}

// plainName reports whether a file name survives a round trip through JSON and a
// URL: valid UTF-8 without control characters.
func plainName(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// listing is the answer to GET /api/list.
type listing struct {
	Bots      []string `json:"bots"`
	Replays   []string `json:"replays"`
	Truncated bool     `json:"truncated,omitempty"`
}

func (a *api) listing() (listing, error) {
	var l listing
	var botsCut, replaysCut bool
	var err error
	if l.Bots, botsCut, err = a.list(a.bots, ".s"); err != nil {
		return listing{}, err
	}
	if l.Replays, replaysCut, err = a.list(a.replays, ".jsonl"); err != nil {
		return listing{}, err
	}
	l.Truncated = botsCut || replaysCut
	return l, nil
}

// readBot returns the source of a listed bot. A name that is not in the listing is
// 404, whatever it looks like.
func (a *api) readBot(name string) ([]byte, error) {
	names, _, err := a.list(a.bots, ".s")
	if err != nil {
		return nil, newAPIError(http.StatusInternalServerError, "cannot read the bots directory")
	}
	if _, found := slices.BinarySearch(names, name); !found {
		return nil, newAPIError(http.StatusNotFound, "no such bot in the list")
	}
	f, err := a.bots.Open(name)
	if err != nil {
		return nil, newAPIError(http.StatusNotFound, "no such bot in the list")
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, newAPIError(http.StatusNotFound, "no such bot in the list")
	}
	src, err := io.ReadAll(io.LimitReader(f, a.lim.MaxBotBytes+1))
	if err != nil {
		return nil, newAPIError(http.StatusInternalServerError, "cannot read bot %q", name)
	}
	if int64(len(src)) > a.lim.MaxBotBytes {
		return nil, newAPIError(http.StatusUnprocessableEntity, "bot %q is larger than %d bytes", name, a.lim.MaxBotBytes)
	}
	return src, nil
}

// match assembles the two listed bots, runs a match with the seed and the default
// configuration but the cycle limit of the limits, and returns the replay file. One
// match runs at a time; another is refused with status 503. The error is an *apiError.
func (a *api) match(first, second string, seed uint64) ([]byte, error) {
	var bots [2]arena.Bot
	for i, name := range [2]string{first, second} {
		src, err := a.readBot(name)
		if err != nil {
			return nil, err
		}
		prog, err := asm.Assemble(name, src)
		if err != nil {
			var ae *asm.Error
			if errors.As(err, &ae) {
				return nil, &apiError{Status: http.StatusUnprocessableEntity, Msg: ae.Error(), Bot: name, Line: ae.Line, Col: ae.Col, Message: ae.Msg}
			}
			return nil, newAPIError(http.StatusUnprocessableEntity, "%v", err)
		}
		bots[i] = arena.Bot{Cells: prog.Cells, Entry: prog.Entry}
	}
	cfg := arena.DefaultConfig()
	cfg.Seed = seed
	cfg.CycleLimit = a.lim.CycleLimit

	release := a.acquire()
	if release == nil {
		return nil, newAPIError(http.StatusServiceUnavailable, "a match is already running; try again in a moment")
	}
	defer release()
	rec, err := replay.Record(cfg, bots[:])
	if err != nil {
		var ae *arena.Error
		if errors.As(err, &ae) {
			return nil, newAPIError(http.StatusUnprocessableEntity, "%s", ae.Msg)
		}
		return nil, newAPIError(http.StatusInternalServerError, "the match failed")
	}
	return rec.Bytes(), nil
}

// errorBody is the JSON of an error answer.
type errorBody struct {
	Error   string `json:"error"`
	Bot     string `json:"bot,omitempty"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
	Message string `json:"message,omitempty"`
}

func setAPIHeaders(w http.ResponseWriter, contentType string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
}

func writeAPIError(w http.ResponseWriter, err *apiError) {
	body, _ := json.Marshal(errorBody{Error: err.Msg, Bot: err.Bot, Line: err.Line, Col: err.Col, Message: err.Message})
	setAPIHeaders(w, "application/json")
	if err.Status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(err.Status)
	w.Write(append(body, '\n'))
}

func (a *api) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeAPIError(w, newAPIError(http.StatusMethodNotAllowed, "use GET"))
		return
	}
	l, err := a.listing()
	if err != nil {
		writeAPIError(w, newAPIError(http.StatusInternalServerError, "cannot list the directories"))
		return
	}
	body, _ := json.Marshal(l)
	setAPIHeaders(w, "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
	if r.Method == http.MethodHead {
		return
	}
	w.Write(append(body, '\n'))
}

// matchRequest is the body of POST /api/match.
type matchRequest struct {
	Bots []string `json:"bots"`
	Seed *string  `json:"seed"`
}

func (a *api) handleMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeAPIError(w, newAPIError(http.StatusMethodNotAllowed, "use POST"))
		return
	}
	// A match is work, so only this server's own page may ask for one. A browser sends
	// Origin with every cross-site POST and Sec-Fetch-Site with every fetch; and a
	// body that is not JSON (what a plain form can send) is refused, while a page of
	// another site cannot send JSON without a preflight, which this server never allows.
	if !sameOrigin(r) {
		writeAPIError(w, newAPIError(http.StatusForbidden, "cross-origin request refused"))
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeAPIError(w, newAPIError(http.StatusUnsupportedMediaType, "send the request as application/json"))
		return
	}
	var req matchRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, a.lim.MaxBodyBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(&req)
	if err == nil {
		// Nothing but whitespace may follow the object.
		if _, err2 := dec.Token(); err2 != io.EOF {
			err = errors.New("unexpected data after the request")
			if err2 != nil {
				err = err2
			}
		}
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeAPIError(w, newAPIError(http.StatusRequestEntityTooLarge, "the request is larger than %d bytes", a.lim.MaxBodyBytes))
			return
		}
		writeAPIError(w, newAPIError(http.StatusBadRequest, "bad request: %v", err))
		return
	}
	if len(req.Bots) != 2 {
		writeAPIError(w, newAPIError(http.StatusBadRequest, `"bots" must name exactly two bots`))
		return
	}
	var seed uint64
	if req.Seed != nil {
		if seed, err = strconv.ParseUint(*req.Seed, 10, 64); err != nil {
			writeAPIError(w, newAPIError(http.StatusBadRequest, `"seed" must be a decimal string from 0 to 18446744073709551615`))
			return
		}
	}
	out, err := a.match(req.Bots[0], req.Bots[1], seed)
	if err != nil {
		var ae *apiError
		if !errors.As(err, &ae) {
			ae = newAPIError(http.StatusInternalServerError, "the match failed")
		}
		writeAPIError(w, ae)
		return
	}
	setAPIHeaders(w, "application/x-ndjson")
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	io.Copy(w, bytes.NewReader(out))
}

// sameOrigin reports whether a request comes from a page of this server, as far as
// the headers a browser adds can tell: Sec-Fetch-Site, if present, is same-origin (or
// none for a request the user typed), and Origin, if present, is http:// and the
// Host of the request. A request with neither is not from a web page.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "http" && u.Path == "" && u.Opaque == "" && u.User == nil && strings.EqualFold(u.Host, r.Host)
}

// defaultBotsDir returns the "bots" directory under base if there is one, and "" if
// there is not (the default of --bots).
func defaultBotsDir(base string) string {
	dir := filepath.Join(base, "bots")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}
