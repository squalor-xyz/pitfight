// SPDX-License-Identifier: AGPL-3.0-or-later

package asm

import "strconv"

// Operand values and every operator result are int32 values; a literal may also be
// 2^31 (see evaluator.value).
const (
	minValue = -1 << 31
	maxValue = 1<<31 - 1
)

// maxHeight bounds the nesting of an expression (docs/spec/asm.md section 6), so
// evaluation and parsing recurse a bounded number of levels whatever the input.
const maxHeight = 256

type nodeKind uint8

const (
	nodeNum    nodeKind = iota // val
	nodeLabel                  // name
	nodeUnary                  // op, l
	nodeBinary                 // op, l, r
)

// node is an expression tree node. line and col are the position of its token (the
// operator for unary and binary nodes). height is 0 for a leaf and 1 more than the
// tallest child otherwise.
type node struct {
	kind   nodeKind
	op     byte
	val    int64
	name   string
	l, r   *node
	line   int
	col    int
	height int
}

// evaluator evaluates expression trees. here is the index of the instruction that
// holds the expression: a label stands for index(label) - here.
type evaluator struct {
	file   string
	labels map[string]labelDef // lookup only; never iterated
	here   int
}

func (ev *evaluator) errorAt(n *node, msg string) error {
	return &Error{File: ev.file, Line: n.line, Col: n.col, Msg: msg}
}

// operand evaluates an operand expression; a nil tree is the omitted operand, 0.
func (ev *evaluator) operand(n *node) (int32, error) {
	if n == nil {
		return 0, nil
	}
	v, err := ev.value(n)
	return int32(v), err
}

// value evaluates a whole expression: every operator result and the final value
// must be in range. A literal may be 2147483648, so that -2147483648 can be
// written, but a bare one is out of range.
func (ev *evaluator) value(n *node) (int64, error) {
	v, err := ev.eval(n)
	if err != nil {
		return 0, err
	}
	return ev.checked(n, v)
}

func (ev *evaluator) eval(n *node) (int64, error) {
	switch n.kind {
	case nodeNum:
		return n.val, nil
	case nodeLabel:
		def, ok := ev.labels[n.name]
		if !ok {
			return 0, ev.errorAt(n, "undefined label "+strconv.Quote(n.name))
		}
		return int64(def.index) - int64(ev.here), nil
	case nodeUnary:
		x, err := ev.eval(n.l)
		if err != nil {
			return 0, err
		}
		if n.op == '-' {
			x = -x
		}
		return ev.checked(n, x)
	}
	x, err := ev.eval(n.l)
	if err != nil {
		return 0, err
	}
	y, err := ev.eval(n.r)
	if err != nil {
		return 0, err
	}
	switch n.op {
	case '+':
		x += y
	case '-':
		x -= y
	case '*':
		x *= y
	default: // '/' and '%'
		if y == 0 {
			return 0, ev.errorAt(n, "division by zero")
		}
		if n.op == '/' {
			x /= y
		} else {
			x %= y
		}
	}
	return ev.checked(n, x)
}

// checked returns v, or an error if it is outside the int32 range. The operands
// of a binary operation are at most 2^31 in absolute value (a literal may be 2^31,
// every other value is in range), so their sum, difference or product cannot
// overflow int64.
func (ev *evaluator) checked(n *node, v int64) (int64, error) {
	if v < minValue || v > maxValue {
		return 0, ev.errorAt(n, "value out of range")
	}
	return v, nil
}
