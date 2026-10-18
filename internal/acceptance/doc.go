// SPDX-License-Identifier: AGPL-3.0-or-later

// Package acceptance holds the v1 acceptance checks of PROJECT.md (catalog C15 to
// C19) as Go tests. It has no code of its own. The tests build the real pit binary
// and run it from the repository root, so they check what a user would run; golden
// files are in testdata/.
package acceptance
