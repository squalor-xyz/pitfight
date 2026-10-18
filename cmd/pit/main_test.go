// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"
)

// usageLines are the usage text's command entries. Matching "\n  name " rather
// than the bare name keeps prose such as "runs matches", or another entry's
// description, from satisfying a test.
var usageLines = []string{"\n  asm ", "\n  run ", "\n  tourney ", "\n  view ", "\n  serve ", "\n  play ", "\n  help "}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout []string    // substrings that must be in stdout
		wantStderr []string    // substrings that must be in stderr
		wantOrder  [][2]string // pairs: the first must come before the second in stderr
		quietOut   bool        // stdout must be empty
		quietErr   bool        // stderr must be empty
	}{
		{
			name:       "no arguments",
			args:       nil,
			wantCode:   2,
			wantStderr: append([]string{"usage: pit"}, usageLines...),
			quietOut:   true,
		},
		{
			name:       "help",
			args:       []string{"help"},
			wantCode:   0,
			wantStdout: append([]string{"usage: pit"}, usageLines...),
			quietErr:   true,
		},
		{
			name:       "unknown subcommand",
			args:       []string{"bogus"},
			wantCode:   2,
			wantStderr: append([]string{`unknown subcommand "bogus"`, "usage: pit"}, usageLines...),
			wantOrder:  [][2]string{{`unknown subcommand "bogus"`, "usage: pit"}},
			quietOut:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			for _, s := range tt.wantStdout {
				if !strings.Contains(stdout.String(), s) {
					t.Errorf("stdout %q does not contain %q", stdout.String(), s)
				}
			}
			for _, s := range tt.wantStderr {
				if !strings.Contains(stderr.String(), s) {
					t.Errorf("stderr %q does not contain %q", stderr.String(), s)
				}
			}
			for _, p := range tt.wantOrder {
				i, j := strings.Index(stderr.String(), p[0]), strings.Index(stderr.String(), p[1])
				if i < 0 || j < 0 || i >= j {
					t.Errorf("stderr %q: %q must come before %q", stderr.String(), p[0], p[1])
				}
			}
			if tt.quietOut && stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if tt.quietErr && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}
