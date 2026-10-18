// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/squalor-xyz/pitfight/web"
)

const serveUsage = `usage: pit serve [flags]

Serves the web viewer on this machine only (127.0.0.1), because a browser does not
let a page read files from file://. Open the printed address. The start page lists
the bots (*.s) of the --bots directory and the replays (*.jsonl) of the --replays
directory: pick two bots and press Fight to run a match with a seed and watch it,
or click a replay. A replay file can still be chosen with the file picker, and
?replay=/replays/NAME in the address loads a file from the --replays directory.

Flags:
  --port N        port to listen on (default 8080; 0 picks a free port)
  --bots DIR      list the bots of DIR on the start page and run matches of them
                  (default: bots, if that directory exists here)
  --replays DIR   list the replays of DIR and serve its files at /replays/
  --web DIR       serve the viewer from DIR (for example web/) instead of the
                  copy built into pit, to work on it without rebuilding

A request cannot reach files outside the --bots, --replays and --web directories,
by .. or by a symbolic link. A symbolic link inside one of them is followed only if
its target is relative and stays inside that directory; any other link is answered
404 (not found), and a bot or replay behind one is not listed. A match runs the
default configuration of pit run (cycle limit 80000), one match at a time, and only
for a page of this server. Stop the server with Ctrl-C.

Exit codes: 0 normal end; 1 an error starting or running the server; 2 a usage error.
`

// runServe implements "pit serve" until the process is interrupted. args excludes
// "serve".
func runServe(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return serve(ctx, args, stdout, stderr)
}

// serve runs the server until ctx ends.
func serve(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	port := 8080
	var webDir, replayDir, botsDir string
	flags := flag.NewFlagSet("pit serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&port, "port", port, "")
	flags.StringVar(&webDir, "web", "", "")
	flags.StringVar(&replayDir, "replays", "", "")
	flags.StringVar(&botsDir, "bots", "", "")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, serveUsage)
			return 0
		}
		fmt.Fprintf(stderr, "pit serve: %v\n\n%s", err, serveUsage)
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "pit serve: unexpected argument %q\n\n%s", flags.Arg(0), serveUsage)
		return 2
	}
	if port < 0 || port > 65535 {
		fmt.Fprintf(stderr, "pit serve: --port must be 0 to 65535\n\n%s", serveUsage)
		return 2
	}

	if botsDir == "" {
		botsDir = defaultBotsDir(".")
	}
	h, closeRoots, err := newServeHandler(webDir, replayDir, botsDir)
	if err != nil {
		fmt.Fprintf(stderr, "pit serve: %v\n", err)
		return 1
	}
	defer closeRoots()
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		fmt.Fprintf(stderr, "pit serve: %v\n", err)
		return 1
	}
	url := "http://" + ln.Addr().String() + "/"
	fmt.Fprintf(stdout, "pit serve: the viewer is at %s (Ctrl-C to stop)\n", url)
	if botsDir != "" {
		fmt.Fprintf(stdout, "pit serve: bots of %s are on the start page\n", botsDir)
	}
	if replayDir != "" {
		fmt.Fprintf(stdout, "pit serve: files of %s are at %sreplays/\n", replayDir, url)
	}

	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		fmt.Fprintf(stderr, "pit serve: %v\n", err)
		return 1
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		srv.Close()
	}
	return 0
}

// newServeHandler returns the handler of "pit serve": the viewer at / (from webDir,
// or the copy built into pit if it is empty), if replayDir is not empty the files of
// that directory at /replays/, and the API of the start page at /api/ (serveapi.go)
// over botsDir and replayDir, either of which may be empty. Directories are opened as
// os.Root, so a path cannot leave them through .. or a symbolic link. closeRoots
// releases them.
func newServeHandler(webDir, replayDir, botsDir string) (h http.Handler, closeRoots func(), err error) {
	var roots []*os.Root
	closeAll := func() {
		for _, r := range roots {
			r.Close()
		}
	}
	defer func() {
		if err != nil {
			closeAll()
		}
	}()
	open := func(dir string) (fs.FS, error) {
		r, err := os.OpenRoot(dir)
		if err != nil {
			return nil, err
		}
		roots = append(roots, r)
		return rootFS{r.FS()}, nil
	}

	var viewer fs.FS = web.Files
	if webDir != "" {
		if viewer, err = open(webDir); err != nil {
			return nil, nil, err
		}
	}
	var botFiles, replayFiles fs.FS // nil when the directory is not given
	if botsDir != "" {
		if botFiles, err = open(botsDir); err != nil {
			return nil, nil, err
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/", getOnly(http.FileServerFS(viewer)))
	if replayDir != "" {
		if replayFiles, err = open(replayDir); err != nil {
			return nil, nil, err
		}
		mux.Handle("/replays/", getOnly(http.StripPrefix("/replays", http.FileServerFS(replayFiles))))
	}
	newAPI(botFiles, replayFiles, defaultAPILimits()).mount(mux)
	return localOnly(mux), closeAll, nil
}

// localOnly answers 403 with an empty body to a request whose Host is not this
// machine (127.0.0.0/8, ::1 or localhost or localhost., any port). The listener is on
// 127.0.0.1 already; the Host check stops a web page on another site from reading
// replays through a DNS name that it points at 127.0.0.1 (DNS rebinding).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// getOnly answers 405 with an empty body and an Allow header to any method but GET
// and HEAD. The files are read-only; only the endpoints of the API take a POST.
func getOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLocalHost reports whether a Host header (host with an optional port) names a
// loopback address or localhost (also as "localhost.", with the root dot).
func isLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	// "localhost." with the root dot is the same name as "localhost" (RFC 1034).
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// rootFS is an os.Root file system that answers every open error that is not
// already "not found" or "permission denied" as not found. An os.Root refuses a
// path that leaves it, also through a symbolic link that points to an absolute path
// (even one inside the root), with an error that http.FileServerFS would report as
// 500; a refused path is the same as no file to a client.
//
// Only regular files and directories open. Opening a named pipe blocks until some
// process writes to it, which would hang the request, so anything else (a pipe, a
// socket, a device, or a symbolic link to one) is not found. Stat is forwarded, so
// that fs.Stat does not open the file.
type rootFS struct{ fs.FS }

func refused(op, name string, err error) error {
	if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
		err = &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	return err
}

func (r rootFS) Stat(name string) (fs.FileInfo, error) {
	info, err := fs.Stat(r.FS, name)
	if err != nil {
		return nil, refused("stat", name, err)
	}
	return info, nil
}

func (r rootFS) Open(name string) (fs.File, error) {
	info, err := r.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	f, err := r.FS.Open(name)
	if err != nil {
		return nil, refused("open", name, err)
	}
	return f, nil
}
