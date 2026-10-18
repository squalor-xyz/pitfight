// SPDX-License-Identifier: AGPL-3.0-or-later

package asm

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

type tokKind uint8

const (
	tokEOF     tokKind = iota
	tokNewline         // a line feed
	tokIdent           // opcode, modifier, directive, label or label reference
	tokNumber          // decimal digits
	tokPunct           // one of the characters in punctuation
	tokIllegal         // a lexical error; msg says which
)

// punctuation is the set of one-character tokens (docs/spec/asm.md section 2).
const punctuation = "#$*@{<}>+-/%(),.:"

// token is one lexical element with its position. line and col start at 1; col
// counts bytes. off is the byte offset in the source.
type token struct {
	kind tokKind
	text string
	msg  string // tokIllegal: the error message
	line int
	col  int
	off  int
}

func isLetter(b byte) bool { return b == '_' || (b|0x20 >= 'a' && b|0x20 <= 'z') }
func isDigit(b byte) bool  { return b >= '0' && b <= '9' }

// lex splits src into tokens. It never fails: a bad character becomes a tokIllegal
// token, so the parser reports it only when it reaches it. The last token is
// tokEOF.
func lex(src []byte) []token {
	var toks []token
	line, lineStart := 1, 0
	for i := 0; i < len(src); {
		b := src[i]
		pos := token{line: line, col: i - lineStart + 1, off: i}
		switch {
		case b == ' ' || b == '\t' || b == '\r':
			i++
		case b == '\n':
			pos.kind = tokNewline
			toks = append(toks, pos)
			i++
			line, lineStart = line+1, i
		case b == ';':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case isLetter(b):
			j := i + 1
			for j < len(src) && (isLetter(src[j]) || isDigit(src[j])) {
				j++
			}
			pos.kind, pos.text = tokIdent, string(src[i:j])
			toks = append(toks, pos)
			i = j
		case isDigit(b):
			j := i + 1
			for j < len(src) && (isLetter(src[j]) || isDigit(src[j])) {
				j++
			}
			pos.text = string(src[i:j])
			pos.kind = tokNumber
			for k := 0; k < len(pos.text); k++ {
				if !isDigit(pos.text[k]) {
					pos.kind = tokIllegal
					pos.msg = "malformed number " + strconv.Quote(pos.text)
					break
				}
			}
			toks = append(toks, pos)
			i = j
		case isPunct(b):
			pos.kind, pos.text = tokPunct, string(b)
			toks = append(toks, pos)
			i++
		default:
			r, size := utf8.DecodeRune(src[i:])
			pos.kind = tokIllegal
			if r == utf8.RuneError && size <= 1 {
				pos.msg = fmt.Sprintf("unexpected byte 0x%02x", b)
				size = 1
			} else {
				pos.msg = "unexpected character " + strconv.QuoteRune(r)
			}
			toks = append(toks, pos)
			i += size
		}
	}
	toks = append(toks, token{kind: tokEOF, line: line, col: len(src) - lineStart + 1, off: len(src)})
	return toks
}

func isPunct(b byte) bool {
	for i := 0; i < len(punctuation); i++ {
		if punctuation[i] == b {
			return true
		}
	}
	return false
}
