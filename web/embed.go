// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web holds the web viewer's files so that "pit serve" can serve them from
// the binary. The JavaScript tests in test/ are not embedded.
package web

import "embed"

// Files is the viewer: index.html, style.css and the ES modules in js/.
//
//go:embed index.html style.css js
var Files embed.FS
