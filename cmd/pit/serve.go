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

Serves the web replay viewer on this machine only (127.0.0.1), because a browser
does not let a page read files from file://. Open the printed address, then choose
a replay file with the file picker, or add ?replay=/replays/NAME to the address to
load a file from the --replays directory.

Flags:
  --port N        port to listen on (default 8080; 0 picks a free port)
  --replays DIR   also serve the files under DIR at /replays/
  --web DIR       serve the viewer from DIR (for example web/) instead of the
                  copy built into pit, to work on it without rebuilding

Files outside the --replays and --web directories are never served, also not
through .. or a symbolic link. Stop the server with Ctrl-C.

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
	var webDir, replayDir string
	flags := flag.NewFlagSet("pit serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&port, "port", port, "")
	flags.StringVar(&webDir, "web", "", "")
	flags.StringVar(&replayDir, "replays", "", "")
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

	h, closeRoots, err := newServeHandler(webDir, replayDir)
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
// or the copy built into pit if it is empty) and, if replayDir is not empty, the
// files of that directory at /replays/. Directories are opened as os.Root, so a
// path cannot leave them through .. or a symbolic link. closeRoots releases them.
func newServeHandler(webDir, replayDir string) (h http.Handler, closeRoots func(), err error) {
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
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServerFS(viewer))
	if replayDir != "" {
		files, err := open(replayDir)
		if err != nil {
			return nil, nil, err
		}
		mux.Handle("/replays/", http.StripPrefix("/replays", http.FileServerFS(files)))
	}
	return localOnly(mux), closeAll, nil
}

// localOnly answers 403 to a request whose Host is not this machine (127.0.0.0/8,
// ::1 or localhost, any port) and 405 to any method but GET and HEAD, both with an
// empty body. The listener is on 127.0.0.1 already; the Host check stops a web page
// on another site from reading replays through a DNS name that it points at
// 127.0.0.1 (DNS rebinding).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLocalHost reports whether a Host header (host with an optional port) names a
// loopback address or localhost.
func isLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
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
type rootFS struct{ fs.FS }

func (r rootFS) Open(name string) (fs.File, error) {
	f, err := r.FS.Open(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
		err = &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return f, err
}
