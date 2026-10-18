# pitfight instruction set (ISA) specification

This document defines the cell format and the exact behaviour of every opcode,
modifier and addressing mode. The VM (`internal/vm`), the instruction types
(`internal/isa`) and the assembler are built and tested against it. The ISA is an
original design strongly inspired by Redcode semantics (C27); it is not Redcode
and v1 makes no compatibility claim (see [Relation to Redcode](#relation-to-redcode)).

Out of scope here: match configuration, placement, scheduling, cycle limit and tie
rule (`docs/spec/match.md`); assembly syntax; the replay format. Where this text
needs a match parameter (`MAXPROCESSES`) it says so and leaves the value to the
match spec.

## 1. Core, values and arithmetic

- The core is a circular array of `CORESIZE` cells, addressed `0 .. CORESIZE-1`.
  Write `N` for `CORESIZE`. Allowed range: `1 <= N <= 1048576` (2^20).
- `Norm(x) = ((x mod N) + N) mod N`, the mathematical remainder in `[0, N)`.
  Every address and every stored value is the result of `Norm`. Negative numbers
  wrap: with `N = 8`, `Norm(-1) = 7`, `Norm(8) = 0`, `Norm(-9) = 7`.
- Go mapping. A stored operand value is an `int32` in `[0, N)`. Compute in
  `int64`, then `Norm`, then narrow to `int32`: `x % N` lies in `(-N, N)`, so
  `x % N + N` cannot overflow `int64` for any `int64` `x`, and every sum, difference
  or product of two stored values (at most `(2^20)^2 = 2^40`) fits in `int64`.
  Code must not do these operations in `int32`.
- A value written in a program (an operand number) is any integer; it is stored as
  `Norm(v)`. Bounds on the number a source file may contain belong to the assembler.
- Stored values are therefore non-negative. All comparisons (`SLT`) and divisions
  (`DIV`) work on these values; integer division truncates (no negatives occur).

## 2. Cell

A cell has, in this order:

| Field | Type | Meaning |
| --- | --- | --- |
| opcode | code 0..14 | one of the opcodes in section 3 |
| modifier | code 0..6 | one of the modifiers in section 3 |
| A mode, A value | code 0..7, `int32` in `[0,N)` | the A operand |
| B mode, B value | code 0..7, `int32` in `[0,N)` | the B operand |
| writer | non-negative integer | id of the last bot that wrote the cell; `0` means no bot has |

The writer is metadata for the visualizer. It is not part of the instruction: it is
never read by an instruction, and `SEQ.I`/`SNE.I` do not compare it. Bot ids are
`1` and up; `0` is reserved for "never written".

**Empty cell:** `DAT.F $0, $0` with writer `0`. Every cell of a fresh core is empty.
Executing an empty cell kills the process.

**Notation.** Cells are written `OPCODE.MODIFIER Xv, Yw`, with mode tokens `X`, `Y`
and values `v`, `w` (`MOV.I $0, $1`). A negative number stands for its `Norm`
(`$-1` is `$7` when `N = 8`). This notation is for this document; the assembler
syntax is defined separately. Every cell carries an explicit modifier; filling in a
default modifier when a source line omits it is an assembler matter.

## 3. Tables

Three tables define the vocabulary. S06 parses them: the header row begins with
`Mnemonic`, the first column holds the exact assembler token in backticks, and the
rows are in code order starting at 0 (the order of the Go constants in
`internal/isa`). No other table in this document has `Mnemonic` as its first header.

### Addressing modes

Terms: `PC` is the address of the executing cell, `v` the operand value in the
fetched copy of the cell (section 4). The *pointer cell* is `Norm(PC + v)`; `f` is
its A field for `*`, `{`, `}` and its B field for `@`, `<`, `>`. The *target* is the
cell the operand names; the operand's *snapshot* is a copy of the target taken at
evaluation time.

| Mnemonic | Code | Name | Target address | Side effect on the pointer cell |
| --- | --- | --- | --- | --- |
| `#` | 0 | immediate | `PC` (the executing cell itself) | none |
| `$` | 1 | direct | `Norm(PC + v)` | none |
| `*` | 2 | A-indirect | `Norm(PC + v + f)`, `f` = A field of the pointer cell | none |
| `@` | 3 | B-indirect | `Norm(PC + v + f)`, `f` = B field of the pointer cell | none |
| `{` | 4 | A-predecrement | as `*`, using `f` after the decrement | before reading `f`: A field = `Norm(A - 1)` |
| `<` | 5 | B-predecrement | as `@`, using `f` after the decrement | before reading `f`: B field = `Norm(B - 1)` |
| `}` | 6 | A-postincrement | as `*`, using `f` before the increment | after the instruction: A field = `Norm(A + 1)` |
| `>` | 7 | B-postincrement | as `@`, using `f` before the increment | after the instruction: B field = `Norm(B + 1)` |

A decrement or increment is a write to the pointer cell by the executing bot
(writer updated). Immediate means the operand's value lives in the executing cell:
an immediate A operand reads the executing cell's fields, and an immediate B operand
is written into the executing cell.

### Modifiers

A modifier selects which fields an instruction reads and writes. Let `A*` be the A
snapshot and `B*` the B snapshot; `a` and `b` name the A and B value fields. Each
modifier defines a list of *pairs* `s -> d`: field `s` of `A*` is the source and
field `d` of `B*` (and of the B target cell) is the destination.

| Mnemonic | Code | Pairs (`A*.s -> B.d`) |
| --- | --- | --- |
| `A` | 0 | `a -> a` |
| `B` | 1 | `b -> b` |
| `AB` | 2 | `a -> b` |
| `BA` | 3 | `b -> a` |
| `F` | 4 | `a -> a` and `b -> b` |
| `X` | 5 | `a -> b` and `b -> a` |
| `I` | 6 | whole cell for `MOV`, `SEQ`, `SNE` (opcode, modifier, both modes, both values); for every other opcode that uses pairs, the same as `F` |

`DAT`, `JMP`, `SPL` and `NOP` ignore the modifier. For `JMZ`, `JMN` and `DJN` the
*tested fields* are the destination fields `d` of the pairs (`A`, `BA`: `a`;
`B`, `AB`: `b`; `F`, `X`, `I`: both); the source fields are unused.

### Opcodes

| Mnemonic | Code | Operands used | Process effect |
| --- | --- | --- | --- |
| `DAT` | 0 | none | the process dies |
| `MOV` | 1 | A source, B target | continues at `PC+1` |
| `ADD` | 2 | A source, B target | continues at `PC+1` |
| `SUB` | 3 | A source, B target | continues at `PC+1` |
| `MUL` | 4 | A source, B target | continues at `PC+1` |
| `DIV` | 5 | A divisor, B target | continues at `PC+1`; dies if a divisor is 0 |
| `JMP` | 6 | A target address | continues at the A target |
| `JMZ` | 7 | A target address, B tested | continues at the A target if tested fields are all 0, else `PC+1` |
| `JMN` | 8 | A target address, B tested | continues at the A target if any tested field is nonzero, else `PC+1` |
| `DJN` | 9 | A target address, B counter | decrements, then as `JMN` on the new values |
| `SPL` | 10 | A target address | continues at `PC+1` and spawns a process at the A target |
| `SEQ` | 11 | A, B compared | continues at `PC+2` if equal, else `PC+1` |
| `SNE` | 12 | A, B compared | continues at `PC+2` if not equal, else `PC+1` |
| `SLT` | 13 | A, B compared | continues at `PC+2` if every pair has `A*.s < B*.d`, else `PC+1` |
| `NOP` | 14 | none | continues at `PC+1` |

All 7 modifiers and 8 modes are valid with all 15 opcodes; no cell content is
illegal.

## 4. Executing one instruction

A bot's process queue is a FIFO of addresses. A *step* takes the process at the
front, executes the cell at its address `PC` (the executing bot's id is the writer of
every write below) and appends zero, one or two addresses to the back. A bot with an
empty queue is dead. A step runs these phases in order:

1. **Fetch.** `IR` = a copy of the cell at `PC`. The opcode, modifier, modes and the
   operand values `v` all come from `IR`, so a write to the executing cell during the
   step does not change them. The pointer fields `f` are not taken from `IR`: they are
   read from the core cell at the pointer address, after any pre-decrement.
2. **Evaluate A.** Apply the A mode to `IR`'s A value as in the mode table, applying
   its pre-decrement (a write) if any. This yields the target address `tA` and the
   snapshot `A*` = the core cell at `tA` now. A post-increment is only noted.
3. **Evaluate B.** The same for the B mode and `IR`'s B value, yielding `tB` and `B*`.
   B is evaluated after A's pre-decrement writes, and both operands are always
   evaluated, whatever the opcode.
4. **Execute** the opcode (section 5), writing at most one cell.
5. **Post-increments.** Apply A's then B's post-increment, if any, by adding 1 (with
   `Norm`) to the pointer cell's field as it is in the core now; each is a write. They
   happen even if the process dies in this step.
6. **Process effect.** Append the addresses the opcode names (each reduced with
   `Norm`). Dying appends nothing.

Writes are reported in the order they happen. One cell write counts once even when
it changes two fields. A write that stores the old value is still a write and sets
the writer. All addresses `PC+1`, `PC+2` and `Norm(...)` wrap modulo `N`.

## 5. Opcode semantics

Unless stated, "write `d`" sets field `d` of the cell at `tB` and leaves its other
fields, opcode, modifier and modes unchanged.

- **DAT.** Nothing is written; the process dies. (Operands were still evaluated, so
  predecrements and postincrements take place.)
- **MOV.** `I`: copy all of `A*` (opcode, modifier, modes, values) into the cell at
  `tB`. Otherwise for each pair, write `d = A*.s`.
- **ADD, SUB, MUL.** For each pair, write `d = Norm(B*.d + A*.s)`, `Norm(B*.d - A*.s)`,
  `Norm(B*.d * A*.s)` respectively (in `int64`, section 1).
- **DIV.** For each pair, `d = B*.d / A*.s` (integer division). If any `A*.s` of the
  pairs is 0, nothing is written and the process dies; otherwise all pairs are
  written. Division by zero is the only fault besides `DAT`.
- **JMP.** Continue at `tA`.
- **JMZ.** Continue at `tA` if every tested field of `B*` is 0, else at `PC+1`.
- **JMN.** Continue at `tA` if any tested field of `B*` is nonzero, else at `PC+1`.
  (Exactly the complement of `JMZ`.)
- **DJN.** For each tested field `d`, write `d = Norm(B*.d - 1)`. Continue at `tA` if
  any new tested value is nonzero, else at `PC+1`.
- **SPL.** Append `PC+1`, then `tA` as a new process, provided the bot has fewer than
  `MAXPROCESSES` live processes (the executing process counts, so a bot with
  `MAXPROCESSES` processes cannot spawn). If it is at the limit, only `PC+1` is
  appended and no error occurs. `MAXPROCESSES` is a match parameter (`match.md`).
- **SEQ / SNE.** Equal means: for `I`, `A*` and `B*` are identical in opcode, modifier,
  both modes and both values (not writer); otherwise `A*.s == B*.d` for every pair.
  `SEQ` skips (`PC+2`) when equal; `SNE` skips when not equal, i.e. when any pair
  differs or the cells differ.
- **SLT.** Skip (`PC+2`) if `A*.s < B*.d` for every pair, else `PC+1`.
- **NOP.** Nothing is written; continue at `PC+1`.

## 6. Worked cases

Each case is a self-contained test for S05 and S06. Common setup: bot id 1 runs a
single process at `PC = 0`, queue `[0]`; `MAXPROCESSES = 8` unless `Setup` says
otherwise. *Before* lists `address: cell` separated by `;`; every unlisted cell is
the empty cell, and `-` means no cell is set. *After* lists the writes in order as
`address: cell contents after that write` (writer 1); `-` means no write. *Queue* is
the bot's queue after the step: the addresses appended (the executed process has left
the front), `[]` meaning the bot is dead. Cell values below are stored values (in
`[0, N)`). The first column is the case id: `OPCODE.MODIFIER` (optionally
`/suffix`) for opcode and modifier cases, a mode mnemonic (optionally `/suffix`) for
mode cases. Tables in this section start with `Case`, not `Mnemonic`.

### Modifier cases

Each runs `MOV` between a source cell 1 `ADD.AB #1, #2` and a destination cell 2
`SLT.A $5, $6`.

| Case | Setup | Before | After | Queue |
| --- | --- | --- | --- | --- |
| `MOV.A` | N=8 | `0: MOV.A $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6` | `2: SLT.A $1, $6` | `[1]` |
| `MOV.B` | N=8 | `0: MOV.B $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6` | `2: SLT.A $5, $2` | `[1]` |
| `MOV.AB` | N=8 | `0: MOV.AB $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6` | `2: SLT.A $5, $1` | `[1]` |
| `MOV.BA` | N=8 | `0: MOV.BA $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6` | `2: SLT.A $2, $6` | `[1]` |
| `MOV.F` | N=8 | `0: MOV.F $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6` | `2: SLT.A $1, $2` | `[1]` |
| `MOV.X` | N=8 | `0: MOV.X $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6` | `2: SLT.A $2, $1` | `[1]` |
| `MOV.I` | N=8 | `0: MOV.I $1, $2; 1: ADD.AB #1, #2; 2: SLT.A $5, $6` | `2: ADD.AB #1, #2` | `[1]` |

### Opcode cases

| Case | Setup | Before | After | Queue |
| --- | --- | --- | --- | --- |
| `DAT.F` | N=8 | `0: DAT.F $3, $4` | `-` | `[]` |
| `DAT.F/empty` | N=8 | `-` | `-` | `[]` |
| `DAT.F/predec` | N=8 | `0: DAT.F <1, $0; 1: DAT.F $0, $3` | `1: DAT.F $0, $2` | `[]` |
| `DAT.F/postinc` | N=8 | `0: DAT.F }1, $0; 1: DAT.F $2, $0` | `1: DAT.F $3, $0` | `[]` |
| `ADD.AB` | N=8 | `0: ADD.AB #3, $1; 1: DAT.F $1, $2` | `1: DAT.F $1, $5` | `[1]` |
| `ADD.F` | N=8 | `0: ADD.F $1, $2; 1: DAT.F $2, $3; 2: DAT.F $4, $5` | `2: DAT.F $6, $0` | `[1]` |
| `SUB.X` | N=8 | `0: SUB.X $1, $2; 1: DAT.F $2, $3; 2: DAT.F $1, $1` | `2: DAT.F $6, $7` | `[1]` |
| `MUL.I` | N=8 | `0: MUL.I $1, $2; 1: DAT.F $3, $2; 2: DAT.F $5, $4` | `2: DAT.F $7, $0` | `[1]` |
| `DIV.BA` | N=8 | `0: DIV.BA $1, $2; 1: DAT.F $1, $2; 2: DAT.F $7, $5` | `2: DAT.F $3, $5` | `[1]` |
| `DIV.B/zero` | N=8 | `0: DIV.B $1, $2; 1: DAT.F $1, $0; 2: DAT.F $4, $4` | `-` | `[]` |
| `DIV.F/zero` | N=8 | `0: DIV.F $1, $2; 1: DAT.F $2, $0; 2: DAT.F $6, $6` | `-` | `[]` |
| `JMP.B` | N=8 | `0: JMP.B $3, $0` | `-` | `[3]` |
| `JMZ.B` | N=8 | `0: JMZ.B $3, $1; 1: DAT.F $5, $0` | `-` | `[3]` |
| `JMZ.B/no` | N=8 | `0: JMZ.B $3, $1; 1: DAT.F $5, $1` | `-` | `[1]` |
| `JMZ.F` | N=8 | `0: JMZ.F $3, $1; 1: DAT.F $0, $2` | `-` | `[1]` |
| `JMN.B` | N=8 | `0: JMN.B $3, $1; 1: DAT.F $0, $2` | `-` | `[3]` |
| `JMN.F` | N=8 | `0: JMN.F $3, $1; 1: DAT.F $0, $2` | `-` | `[3]` |
| `DJN.B` | N=8 | `0: DJN.B $3, $1; 1: DAT.F $0, $2` | `1: DAT.F $0, $1` | `[3]` |
| `DJN.B/exit` | N=8 | `0: DJN.B $3, $1; 1: DAT.F $0, $1` | `1: DAT.F $0, $0` | `[1]` |
| `DJN.B/wrap` | N=8 | `0: DJN.B $3, $1; 1: DAT.F $0, $0` | `1: DAT.F $0, $7` | `[3]` |
| `DJN.F` | N=8 | `0: DJN.F $3, $1; 1: DAT.F $1, $3` | `1: DAT.F $0, $2` | `[3]` |
| `SPL.B` | N=8 | `0: SPL.B $3, $0` | `-` | `[1, 3]` |
| `SPL.B/cap` | N=8, MAXPROCESSES=1 | `0: SPL.B $3, $0` | `-` | `[1]` |
| `SEQ.I` | N=8 | `0: SEQ.I $1, $2; 1: ADD.AB #1, #2; 2: ADD.AB #1, #2` | `-` | `[2]` |
| `SEQ.I/mode` | N=8 | `0: SEQ.I $1, $2; 1: ADD.AB #1, #2; 2: ADD.AB $1, #2` | `-` | `[1]` |
| `SEQ.A` | N=8 | `0: SEQ.A $1, $2; 1: DAT.F $4, $1; 2: DAT.F $4, $2` | `-` | `[2]` |
| `SNE.B` | N=8 | `0: SNE.B $1, $2; 1: DAT.F $0, $1; 2: DAT.F $0, $2` | `-` | `[2]` |
| `SNE.F` | N=8 | `0: SNE.F $1, $2; 1: DAT.F $1, $2; 2: DAT.F $1, $3` | `-` | `[2]` |
| `SNE.I/equal` | N=8 | `0: SNE.I $1, $2; 1: ADD.AB #1, #2; 2: ADD.AB #1, #2` | `-` | `[1]` |
| `SLT.AB` | N=8 | `0: SLT.AB #2, $1; 1: DAT.F $0, $5` | `-` | `[2]` |
| `SLT.AB/no` | N=8 | `0: SLT.AB #2, $1; 1: DAT.F $0, $2` | `-` | `[1]` |
| `SLT.F` | N=8 | `0: SLT.F $1, $2; 1: DAT.F $1, $5; 2: DAT.F $2, $5` | `-` | `[1]` |
| `SLT.X` | N=8 | `0: SLT.X $1, $2; 1: DAT.F $1, $2; 2: DAT.F $3, $2` | `-` | `[2]` |
| `NOP.F` | N=8 | `0: NOP.F $1, $2` | `-` | `[1]` |
| `NOP.F/side` | N=8 | `0: NOP.F >1, $0; 1: DAT.F $0, $2` | `1: DAT.F $0, $3` | `[1]` |

### Addressing-mode cases

The eight unsuffixed cases show which cell each mode targets: `#` copies the
executing cell's A value into cell 7, the others copy the target cell into cell 7
with `MOV.I`, where `ADD.AB #1, #2` at cell 3 is the intended target and the pointer
cell, when there is one, is cell 1. The suffixed cases cover an immediate B operand,
wrap, evaluation order, and `N = 1` (where a write of the same value is still a
write).

| Case | Setup | Before | After | Queue |
| --- | --- | --- | --- | --- |
| `#` | N=8 | `0: MOV.AB #4, $7` | `7: DAT.F $0, $4` | `[1]` |
| `$` | N=8 | `0: MOV.I $3, $7; 3: ADD.AB #1, #2` | `7: ADD.AB #1, #2` | `[1]` |
| `*` | N=8 | `0: MOV.I *1, $7; 1: DAT.F $2, $0; 3: ADD.AB #1, #2` | `7: ADD.AB #1, #2` | `[1]` |
| `@` | N=8 | `0: MOV.I @1, $7; 1: DAT.F $0, $2; 3: ADD.AB #1, #2` | `7: ADD.AB #1, #2` | `[1]` |
| `{` | N=8 | `0: MOV.I {1, $7; 1: DAT.F $3, $0; 3: ADD.AB #1, #2` | `1: DAT.F $2, $0; 7: ADD.AB #1, #2` | `[1]` |
| `<` | N=8 | `0: MOV.I <1, $7; 1: DAT.F $0, $3; 3: ADD.AB #1, #2` | `1: DAT.F $0, $2; 7: ADD.AB #1, #2` | `[1]` |
| `}` | N=8 | `0: MOV.I }1, $7; 1: DAT.F $2, $0; 3: ADD.AB #1, #2` | `7: ADD.AB #1, #2; 1: DAT.F $3, $0` | `[1]` |
| `>` | N=8 | `0: MOV.I >1, $7; 1: DAT.F $0, $2; 3: ADD.AB #1, #2` | `7: ADD.AB #1, #2; 1: DAT.F $0, $3` | `[1]` |
| `#/B` | N=8 | `0: MOV.A $1, #0; 1: DAT.F $6, $0` | `0: MOV.A $6, #0` | `[1]` |
| `@/wrap` | N=8 | `0: MOV.I @1, $7; 1: DAT.F $0, $7` | `7: MOV.I @1, $7` | `[1]` |
| `</wrap` | N=8 | `0: MOV.I <1, $7; 1: DAT.F $0, $0` | `1: DAT.F $0, $7; 7: MOV.I <1, $7` | `[1]` |
| `</order` | N=8 | `0: MOV.I <1, <1; 1: DAT.F $0, $4; 3: ADD.AB #1, #2; 4: SUB.F $1, $1` | `1: DAT.F $0, $3; 1: DAT.F $0, $2; 3: SUB.F $1, $1` | `[1]` |
| `>/order` | N=8 | `0: MOV.AB >1, >1; 1: DAT.F $0, $2; 3: DAT.F $5, $6` | `3: DAT.F $5, $5; 1: DAT.F $0, $3; 1: DAT.F $0, $4` | `[1]` |
| `$/size1` | N=1 | `0: MOV.AB #0, $0` | `0: MOV.AB #0, $0` | `[0]` |

## 7. Relation to Redcode

This comparison is from the authors' recollection of the ICWS'94 Redcode standard.
No standard text is in the repository and none was checked; this document, not
Redcode, is normative.

Kept (same idea and same names): a circular core with modulo addressing; an
instruction of opcode, modifier and two operands each with a mode; the eight
addressing modes and seven modifiers and their field pairing; evaluation of A then B
with snapshots; an immediate operand naming the executing cell; per-bot FIFO process
queues; `SPL` queueing the continuation before the new process; `DAT` and division
by zero killing the process; the empty cell `DAT.F $0, $0`; `SEQ`, `SNE`, `SLT`
skips.

Different or absent:

- 15 opcodes. No `MOD`, `LDP`, `STP` (no P-space), no `CMP` alias (`SEQ` only).
- No read or write limits: any cell may be read or written from anywhere.
- No default modifier in a cell, and no assembler pseudo-ops; both belong to the
  assembler.
- `DIV` with several pairs writes nothing at all if any divisor is 0.
- The last-writer id exists in every cell.
- Post-increment timing (after execution), the `SPL` behaviour at the process limit
  and the order of simultaneous side effects are defined here and may differ from
  Redcode. A Redcode compatibility mode, if added later, must check each against the
  standard text.

## 8. The four starter strategies

The ISA must express the starter bots (imp, bomber, replicator, scanner). Listings
use the notation of section 2 with relative numeric offsets, one line per
consecutive cell starting at offset 0; the number after each `;` is the cell's
offset. They are programs in the assembly syntax of `docs/spec/asm.md`. They are
sketches to show the ISA reaches each strategy; they are not balance results.

**Imp.** One cell, a moving process: `MOV.I $0, $1` copies itself onto the next cell
each step, then continues into the copy.

**Bomber** (stepping pointer in cell 3's B field, bomb is cell 3 itself):

```asm
ADD.AB #4, $3        ; 0: step the pointer in cell 3
MOV.I  $2, @2        ; 1: copy cell 3 to cell 3 + (its B field)
JMP.B  $-2, $0       ; 2: loop to 0
DAT.F  $0, $0        ; 3: pointer and bomb
```

**Replicator** (copies cells 0..4 to offset 100, spawns the copy, then the original
falls into cell 5 and dies; the copy re-initialises its own pointers):

```asm
MOV.A  #-5, $5       ; 0: pointer cell 5: A = source offset (cell 0)
MOV.AB #95, $4       ; 1: pointer cell 5: B = destination offset (cell 100)
MOV.I  }3, >3        ; 2: copy one cell, then step both pointers
JMN.A  $-1, $2       ; 3: loop to 2 until the source pointer reaches 0
SPL.B  $96, $0       ; 4: spawn the copy at offset 100
DAT.F  $0, $0        ; 5: pointer cell; the original dies here
```

**Scanner** (steps a pointer by 7, compares the scanned cell with the empty
reference, bombs the first different cell):

```asm
ADD.AB #7, $5        ; 0: step the pointer in cell 5
SNE.I  @4, $5        ; 1: scanned cell (via cell 5) vs reference cell 6: skip if different
JMP.B  $-2, $0       ; 2: nothing found: loop to 0
MOV.I  $3, @2        ; 3: found: copy the bomb (cell 6) onto the scanned cell
JMP.B  $-4, $0       ; 4: loop to 0
DAT.F  $0, $0        ; 5: pointer: B = scan offset from this cell
DAT.F  $0, $0        ; 6: reference and bomb
```

Known limit of these sketches: the scanner and bomber can reach their own cells after
the pointer wraps around the core; a real bot would avoid that.
