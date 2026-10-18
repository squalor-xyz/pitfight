// SPDX-License-Identifier: AGPL-3.0-or-later

// Package replay writes, reads and applies the pitfight replay file, version 1
// (docs/spec/replay.md): versioned JSON lines holding the match configuration, the
// loaded bots, the steps of every cycle (executed address, cell writes, spawns and
// deaths), and the result.
// The bytes depend only on the match, not on the Go version (C36).
package replay
