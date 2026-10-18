// SPDX-License-Identifier: AGPL-3.0-or-later

package asm

import (
	"strconv"
	"strings"

	"github.com/squalor-xyz/pitfight/internal/isa"
)

// maxInstructions is the longest program, match.md validation check 6.
const maxInstructions = 1048576

// instr is a parsed instruction whose operand expressions are not yet evaluated.
// A nil expression is the omitted operand.
type instr struct {
	op    isa.Opcode
	mod   isa.Modifier
	aMode isa.Mode
	bMode isa.Mode
	a, b  *node
}

type labelDef struct {
	index int // index of the next instruction after the label
	line  int
}

// parser holds the token stream and what has been read so far.
type parser struct {
	file   string
	toks   []token
	pos    int
	depth  int // current nesting of unary operators and parentheses
	instrs []instr
	labels map[string]labelDef // lookup only; never iterated

	org     *node // the ORG expression, or nil
	orgTok  token // the first token of the ORG expression
	orgLine int   // the line of the ORG directive
}

func (p *parser) peek() token { return p.toks[p.pos] }

// peekAt returns the token n ahead; past the end it is the EOF token.
func (p *parser) peekAt(n int) token {
	if p.pos+n >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.pos+n]
}

func (p *parser) errorAt(t token, msg string) error {
	return &Error{File: p.file, Line: t.line, Col: t.col, Msg: msg}
}

func describe(t token) string {
	switch t.kind {
	case tokNewline:
		return "end of line"
	case tokEOF:
		return "end of file"
	}
	return strconv.Quote(t.text)
}

// unexpected reports t where something else was expected. A lexical error is
// reported as such.
func (p *parser) unexpected(t token, expected string) error {
	if t.kind == tokIllegal {
		return p.errorAt(t, t.msg)
	}
	return p.errorAt(t, "expected "+expected+", found "+describe(t))
}

func isPunctTok(t token, s string) bool { return t.kind == tokPunct && t.text == s }

func isEnd(t token) bool { return t.kind == tokNewline || t.kind == tokEOF }

func (p *parser) parseFile() error {
	for p.peek().kind != tokEOF {
		if err := p.parseLine(); err != nil {
			return err
		}
	}
	return nil
}

func (p *parser) parseLine() error {
	for p.peek().kind == tokIdent && isPunctTok(p.peekAt(1), ":") {
		if err := p.defineLabel(p.peek()); err != nil {
			return err
		}
		p.pos += 2
	}
	t := p.peek()
	switch t.kind {
	case tokNewline:
		p.pos++
		return nil
	case tokEOF:
		return nil
	case tokIdent:
		var err error
		if strings.EqualFold(t.text, "ORG") {
			err = p.parseORG()
		} else {
			err = p.parseInstruction()
		}
		if err != nil {
			return err
		}
		switch end := p.peek(); end.kind {
		case tokNewline:
			p.pos++
		case tokEOF:
		default:
			return p.unexpected(end, "end of line")
		}
		return nil
	}
	return p.unexpected(t, "opcode")
}

func (p *parser) defineLabel(t token) error {
	if strings.EqualFold(t.text, "ORG") || lookupOpcode(t.text) != nil {
		return p.errorAt(t, "label name "+strconv.Quote(t.text)+" is reserved")
	}
	if first, ok := p.labels[t.text]; ok {
		return p.errorAt(t, "duplicate label "+strconv.Quote(t.text)+" (first defined at line "+strconv.Itoa(first.line)+")")
	}
	p.labels[t.text] = labelDef{index: len(p.instrs), line: t.line}
	return nil
}

func (p *parser) parseORG() error {
	org := p.toks[p.pos]
	p.pos++
	if p.org != nil {
		return p.errorAt(org, "duplicate ORG (first at line "+strconv.Itoa(p.orgLine)+")")
	}
	first := p.peek()
	e, err := p.parseExpr()
	if err != nil {
		return err
	}
	p.org, p.orgTok, p.orgLine = e, first, org.line
	return nil
}

func (p *parser) parseInstruction() error {
	opTok := p.toks[p.pos]
	p.pos++
	opp := lookupOpcode(opTok.text)
	if opp == nil {
		return p.errorAt(opTok, "unknown opcode "+strconv.Quote(opTok.text))
	}
	if len(p.instrs) >= maxInstructions {
		return p.errorAt(opTok, "program longer than "+strconv.Itoa(maxInstructions)+" instructions")
	}
	in := instr{op: *opp, aMode: isa.ModeDirect, bMode: isa.ModeDirect}

	var mod *isa.Modifier
	if dot := p.peek(); isPunctTok(dot, ".") {
		if dot.off != opTok.off+len(opTok.text) {
			return p.errorAt(dot, `unexpected space before "."`)
		}
		p.pos++
		m := p.peek()
		if m.kind != tokIdent {
			return p.unexpected(m, "modifier")
		}
		if m.off != dot.off+1 {
			return p.errorAt(m, `unexpected space after "."`)
		}
		if mod = lookupModifier(m.text); mod == nil {
			return p.errorAt(m, "unknown modifier "+strconv.Quote(m.text))
		}
		p.pos++
	}

	if !isEnd(p.peek()) {
		var err error
		if in.aMode, in.a, err = p.parseOperand(); err != nil {
			return err
		}
		if t := p.peek(); isPunctTok(t, ",") {
			p.pos++
			if in.bMode, in.b, err = p.parseOperand(); err != nil {
				return err
			}
		} else if !isEnd(t) {
			return p.unexpected(t, `"," or end of line`)
		}
	}
	if mod != nil {
		in.mod = *mod
	} else {
		in.mod = defaultModifier(in.op, in.aMode, in.bMode)
	}
	p.instrs = append(p.instrs, in)
	return nil
}

// parseOperand reads [mode] expression.
func (p *parser) parseOperand() (isa.Mode, *node, error) {
	mode := isa.ModeDirect
	t := p.peek()
	switch t.kind {
	case tokPunct:
		if m := lookupMode(t.text); m != nil {
			mode = *m
			p.pos++
		} else {
			switch t.text {
			case "+", "-", "(":
			case ",":
				return 0, nil, p.unexpected(t, "operand")
			default:
				return 0, nil, p.errorAt(t, "bad addressing mode "+strconv.Quote(t.text))
			}
		}
	case tokNewline, tokEOF:
		return 0, nil, p.unexpected(t, "operand")
	}
	e, err := p.parseExpr()
	return mode, e, err
}

func (p *parser) parseExpr() (*node, error) {
	return p.parseBinary(func(s string) bool { return s == "+" || s == "-" }, p.parseTerm)
}

func (p *parser) parseTerm() (*node, error) {
	return p.parseBinary(func(s string) bool { return s == "*" || s == "/" || s == "%" }, p.parseUnary)
}

// parseBinary reads operand { op operand } with a left-associative op for which
// isOp is true.
func (p *parser) parseBinary(isOp func(string) bool, operand func() (*node, error)) (*node, error) {
	l, err := operand()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind != tokPunct || !isOp(t.text) {
			return l, nil
		}
		p.pos++
		r, err := operand()
		if err != nil {
			return nil, err
		}
		n := &node{kind: nodeBinary, op: t.text[0], l: l, r: r, line: t.line, col: t.col, height: 1 + max(l.height, r.height)}
		if n.height > maxHeight {
			return nil, p.errorAt(t, "expression too complex")
		}
		l = n
	}
}

func (p *parser) parseUnary() (*node, error) {
	t := p.peek()
	if t.kind != tokPunct || (t.text != "+" && t.text != "-") {
		return p.parsePrimary()
	}
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxHeight {
		return nil, p.errorAt(t, "expression too complex")
	}
	p.pos++
	x, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	n := &node{kind: nodeUnary, op: t.text[0], l: x, line: t.line, col: t.col, height: 1 + x.height}
	if n.height > maxHeight {
		return nil, p.errorAt(t, "expression too complex")
	}
	return n, nil
}

func (p *parser) parsePrimary() (*node, error) {
	t := p.peek()
	switch {
	case t.kind == tokNumber:
		v, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil || v > -minValue {
			return nil, p.errorAt(t, "value out of range")
		}
		p.pos++
		return &node{kind: nodeNum, val: v, line: t.line, col: t.col}, nil
	case t.kind == tokIdent:
		p.pos++
		return &node{kind: nodeLabel, name: t.text, line: t.line, col: t.col}, nil
	case isPunctTok(t, "("):
		p.depth++
		defer func() { p.depth-- }()
		if p.depth > maxHeight {
			return nil, p.errorAt(t, "expression too complex")
		}
		p.pos++
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if c := p.peek(); !isPunctTok(c, ")") {
			return nil, p.unexpected(c, `")"`)
		}
		p.pos++
		return e, nil
	}
	return nil, p.unexpected(t, "expression")
}

// lookupOpcode returns the opcode with the given mnemonic, ignoring case, or nil.
func lookupOpcode(s string) *isa.Opcode {
	for _, op := range isa.Opcodes {
		if strings.EqualFold(op.String(), s) {
			return &op
		}
	}
	return nil
}

func lookupModifier(s string) *isa.Modifier {
	for _, m := range isa.Modifiers {
		if strings.EqualFold(m.String(), s) {
			return &m
		}
	}
	return nil
}

func lookupMode(s string) *isa.Mode {
	for _, m := range isa.Modes {
		if m.String() == s {
			return &m
		}
	}
	return nil
}

// defaultModifier is the modifier of an instruction written without one
// (docs/spec/asm.md section 4).
func defaultModifier(op isa.Opcode, a, b isa.Mode) isa.Modifier {
	switch op {
	case isa.OpMOV, isa.OpSEQ, isa.OpSNE:
		switch {
		case a == isa.ModeImmediate:
			return isa.ModAB
		case b == isa.ModeImmediate:
			return isa.ModB
		}
		return isa.ModI
	case isa.OpADD, isa.OpSUB, isa.OpMUL, isa.OpDIV:
		switch {
		case a == isa.ModeImmediate:
			return isa.ModAB
		case b == isa.ModeImmediate:
			return isa.ModB
		}
		return isa.ModF
	case isa.OpSLT:
		if a == isa.ModeImmediate {
			return isa.ModAB
		}
		return isa.ModB
	case isa.OpJMP, isa.OpJMZ, isa.OpJMN, isa.OpDJN, isa.OpSPL:
		return isa.ModB
	}
	return isa.ModF // DAT, NOP
}
