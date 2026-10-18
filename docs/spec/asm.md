# pitfight assembly language specification

This document defines the assembly syntax, the expression operators and their
precedence, the errors, and the output of `pit asm`. The assembler
(`internal/asm`) is built and tested against it. Instruction meanings are in
`docs/spec/isa.md`; how a bot is loaded into a match is in `docs/spec/match.md`
section 5. The syntax is original; it is Redcode-like but makes no compatibility
claim (isa.md section 7).

Fenced blocks tagged `asm` are complete programs that assemble without error
(`TestSpecExamples` checks this). Blocks tagged `text` are grammar, output or
fragments and are not assembled.

## 1. Source files

A source file is a sequence of bytes. The language is ASCII: any byte outside a
comment that is not part of a token in section 2 is an error. Bytes inside a comment
are ignored, whatever they are.

- A line ends at a line feed (`\n`). The last line needs no line feed.
- Space, tab and carriage return are whitespace, so `\r\n` line endings work.
- Line numbers start at 1. Column numbers start at 1 and count bytes from the start
  of the line (a tab is one column).

## 2. Lexical elements

- **Comment:** `;` to the end of the line.
- **Identifier:** a letter or `_`, then letters, digits and `_`. Opcode, modifier and
  directive names are matched ignoring case (`mov.i` is `MOV.I`); labels are
  case-sensitive.
- **Number:** one or more decimal digits. There are no other bases and no sign in the
  literal (a minus is the unary operator of section 6). A number directly followed by
  a letter or `_` (`12ab`) is malformed.
- **Punctuation:** `# $ * @ { < } > + - / % ( ) , . :`.

## 3. Statements

```text
line        = { label } [ statement ] [ comment ] newline
label       = identifier ":"
statement   = instruction | "ORG" expression
instruction = opcode [ "." modifier ] [ operand [ "," operand ] ]
operand     = [ mode ] expression
mode        = "#" | "$" | "*" | "@" | "{" | "<" | "}" | ">"
```

- Opcodes are the 15 mnemonics of isa.md section 3 and modifiers the 7 mnemonics of
  the same section. The `.` and the modifier follow the opcode with no space.
- A line may define any number of labels, then hold one statement. A line with
  labels only is allowed.
- Each instruction line produces exactly one cell. The cells are numbered from 0 in
  source order; the number is the instruction's *index*.
- A *label* names the index of the next instruction after it, which may be on a
  later line. A label after the last instruction names the index one past the end
  (the program length `L`).
- A label name must not be an opcode or `ORG`, ignoring case (`label name "MOV" is
  reserved`). Modifier names (`A`, `B`, `F`, ...) are allowed as labels. A label is
  defined at most once.

## 4. Operands

The mode is one of the eight tokens of isa.md section 3.

- An operand without a mode token has mode `$` (direct).
- An omitted operand is `$0`. So `DAT` alone is `DAT.F $0, $0` (the empty cell),
  `JMP loop` is `JMP.B $loop, $0`, and one operand is always the A operand.
- A mode token and the following expression may be separated by whitespace.
  `*` is both a mode and the multiplication operator: at the start of an operand it is
  the mode, elsewhere the operator. To start an operand with a unary sign or a
  parenthesis, omit the mode or write one first (`$-1`, `#(1+2)`).

### Default modifier

An instruction written without `.modifier` gets one from its opcode and the modes of
its operands after the omitted-operand rule above (the same table as Redcode '94,
without the opcodes pitfight lacks):

| Opcode | Modifier |
| --- | --- |
| `DAT`, `NOP` | `F` |
| `MOV`, `SEQ`, `SNE` | `AB` if the A mode is `#`; else `B` if the B mode is `#`; else `I` |
| `ADD`, `SUB`, `MUL`, `DIV` | `AB` if the A mode is `#`; else `B` if the B mode is `#`; else `F` |
| `SLT` | `AB` if the A mode is `#`; else `B` |
| `JMP`, `JMZ`, `JMN`, `DJN`, `SPL` | `B` |

The cell always carries an explicit modifier (isa.md section 2).

## 5. Entry offset: `ORG`

match.md section 5 starts a bot's first process at its entry offset `e`,
`0 <= e < L`. A source file names it with the directive `ORG expression`, which may
appear on any line, before or after the instructions, at most once.

- The expression is evaluated like any other (section 6), except that a label
  stands for its absolute index, not an offset. `ORG start` is the index of `start`;
  `ORG 3` is 3.
- Without `ORG`, `e = 0`.
- `e` must lie in `0 .. L-1`. A label after the last instruction has index `L` and
  is therefore not a valid entry.

## 6. Expressions

```text
expression = term { ( "+" | "-" ) term }
term       = unary { ( "*" | "/" | "%" ) unary }
unary      = ( "+" | "-" ) unary | primary
primary    = number | identifier | "(" expression ")"
```

Precedence from tightest to loosest: unary `+` and `-`; then `*`, `/` and `%`; then
binary `+` and `-`. Binary operators are left-associative. So `-2 * 3 + 4 - 1` is
`((-2) * 3) + 4 - 1 = -3`, and `1 + 2 * 3` is 7. Parentheses group.

- All arithmetic is on signed integers. `/` truncates toward zero and `%` has the
  sign of the dividend (as in Go), so `-7 / 2 = -3` and `-7 % 2 = -1`.
- An identifier in an expression is a label reference. In an operand its value is
  `index(label) - index(instruction containing the operand)`, the relative offset
  that the ISA's modes add to the program counter; in `ORG` it is `index(label)`.
  A label may be used before or after its definition. The difference of two labels
  is the same in both readings.
- Every operator result and the final value of an operand or `ORG` expression must be
  in `-2147483648 .. 2147483647` (the range of `int32`), otherwise the expression is
  an error. A number may be at most `2147483648`, so that `-2147483648` can be
  written; a bare `2147483648` is out of range.
- A divisor of zero is an error: `/ 0` and `% 0`.
- An expression may be at most 256 levels high (nesting of parentheses, unary
  operators and the chain length of a left-associative operator all count).

The value of an operand is stored in the cell as is, an `int32` that may be
negative. It is not reduced modulo `CORESIZE`: the assembler does not know the core
size, and the match stores `Norm(v)` when it loads the bot (match.md section 5,
isa.md section 1).

## 7. Programs and errors

A program has `1 .. 1048576` instructions (match.md validation check 6 uses the same
bound). An empty program is an error positioned at the end of the file.

Assembly stops at the first error. An error has a file name (as given to the
assembler), a line, a column and a message, and its text is

```text
file:line:col: message
```

Syntax errors are found while reading the file, in source order (a number above
`2147483648` is one of them); they come before every error of the second group. The
second group is found after the whole file has been read, in this order: the operands
of the instructions in source order (A before B), where an undefined label or a
failed evaluation (`/ 0`, a result out of range) is reported; then the empty
program; then the `ORG` expression and its range.

The column is the first byte of the offending token. An error about an operator
(`division by zero`, `value out of range` for a result) is positioned at the
operator. An error at the end of a line is positioned at its line feed, and one at
the end of the file just past the last byte.

The messages, with `X` the token as written:

| Message | Position |
| --- | --- |
| `unexpected character 'c'` | a valid UTF-8 character that is not part of a token (including NUL and form feed), shown as Go quotes it |
| `unexpected byte 0xNN` | a byte that is not valid UTF-8 |
| `malformed number "12ab"` | the number |
| `expected opcode, found X` | the token |
| `unknown opcode "X"`, `unknown modifier "X"` | the identifier |
| `label name "X" is reserved` | the label |
| `duplicate label "X" (first defined at line N)` | the second label |
| `duplicate ORG (first at line N)` | the second `ORG` |
| `unexpected space before "."`, `unexpected space after "."` | the `.`, or the token after it |
| `expected modifier, found X` | the token after `.` |
| `bad addressing mode "X"` | a punctuation token at the start of an operand that is not a mode, `+`, `-`, `(` or `,` |
| `expected operand, found X` | the token after `,` or the `,` of an empty first operand |
| `expected expression, found X` | the token |
| `expected ")", found X` | the token |
| `expected "," or end of line, found X` | the token after the first operand |
| `expected end of line, found X` | the token after the statement |
| `value out of range` | the number or operator |
| `expression too complex` | the operator or token that makes it too tall |
| `program longer than 1048576 instructions` | the 1048577th opcode |
| `undefined label "X"` | the identifier |
| `division by zero` | the `/` or `%` |
| `entry offset E outside 0..M` | the first token of the `ORG` expression |
| `no instructions` | the end of the file |

## 8. Go API

`internal/asm` has:

```text
type Program struct {
    Cells []isa.Cell // L >= 1 cells; Writer is 0; values are not normalized
    Entry int        // entry offset, 0 <= Entry < len(Cells)
}

func Assemble(name string, src []byte) (Program, error)
```

`name` is only used in errors. The error is `*asm.Error` (fields `File`, `Line`,
`Col`, `Msg`; `Error()` is the `file:line:col: message` text) and its `Line` and `Col`
are at least 1. On error the `Program` is the zero value.

## 9. `pit asm`

```text
pit asm <file>
```

reads the file, assembles it with the file name exactly as given on the command line,
and prints the assembled program as a *listing* on standard output. Exit code 0.

- The first line is `ORG <e>` with the entry offset.
- Then one line per cell: `OPCODE.MODIFIER <mode><value>, <mode><value>`, the
  modifier always explicit, the mode token directly followed by the signed decimal
  value, single spaces as shown, no labels and no comments.
- Every line, including the last, ends with a line feed.

A listing is itself a valid source file and assembles to the same cells and entry
offset. Example source and listing:

```asm
; the bomber of isa.md section 8
start:  ADD.AB #4, ptr      ; step the pointer in ptr's B field
        MOV.I  ptr, @ptr    ; copy the bomb to ptr + its B field
        JMP    start        ; loop
ptr:    DAT    #0, #0       ; pointer and bomb
```

```text
ORG 0
ADD.AB #4, $3
MOV.I $2, @2
JMP.B $-2, $0
DAT.F #0, #0
```

If the file cannot be read, or on an assembly error, nothing is written to standard
output, the message is written to standard error, and the exit code is 1. An
assembly error is printed as `file:line:col: message`. A wrong argument count or an
argument starting with `-` is a usage error: exit code 2 and
`usage: pit asm <file>` on standard error.

## 10. More examples

```asm
; the imp of isa.md section 8
        MOV.I $0, $1
```

```asm
; the replicator of isa.md section 8, entered at its first cell
        ORG   copy
copy:   MOV.A  #-5, ptr
        MOV.AB #95, ptr
loop:   MOV.I  }ptr, >ptr
        JMN.A  loop, ptr
        SPL.B  $96
ptr:    DAT.F  $0, $0
```

```asm
; labels, operators and a computed entry offset
        ORG   (2 * 3) % 4 - 1 + first
first:  DAT   #0, #size
        DAT   #-1, #(1 + 2) * 3
size:
```
