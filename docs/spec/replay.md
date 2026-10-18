# pitfight replay format, version 1

A replay is the complete record of one match: its configuration, the bots as they
were loaded, every change the match made to the core and to the process lists, and
the result. The terminal and web viewers play a replay without running the match
again (catalog C07, C19). The format is versioned JSON lines (C29). `internal/replay`
writes, reads and applies it, and is tested against this document.

Out of scope here: the viewers, a binary format, and the match rules themselves
(`docs/spec/match.md`, `docs/spec/isa.md`).

## 1. Principles

- **Bytes are the format.** The writer produces exactly one byte string for a match.
  A reader accepts only that byte string (section 8), so the file hash (section 6)
  identifies the match, and reading then writing a replay gives the same bytes.
- **Integers and fixed strings only.** No floating point, no maps, no insignificant
  whitespace, no escapes. Every string in a replay is a fixed word (`header`,
  `win`, ...), a decimal integer (the seed) or lowercase hexadecimal (a hash), so
  nothing in the bytes depends on a JSON library or the Go version (C36). The
  writer is hand-written (`strconv` appends); it does not use `encoding/json`.
- **JavaScript-safe numbers.** Every JSON number is an integer between 0 and
  2^31 - 1, far inside 2^53, so a viewer can read the file with `JSON.parse` and
  plain numbers. The seed, an unsigned 64-bit integer, is a string of decimal digits
  (owner amendment of C77, 2026-10-18).
- **Line-oriented.** One JSON object per line, each ending with a line feed (`\n`,
  byte 10), the last line included. The file is ASCII; a carriage return is not
  allowed.

## 2. Line order

A replay is, in this order:

1. one `header` line;
2. one `load` line per bot, in bot id order (`K` lines);
3. one group per cycle, `1 .. cycles` without gaps, each one `c` line followed by one
   or more steps; a step is one `x` line followed by zero or more event lines (`w`,
   `s`, `d`);
4. one `end` line, the last line.

Nothing else is allowed. `N` below is `core_size`, `K` the number of bots.

## 3. Cells

A cell is written as an array of six integers, the fields of `docs/spec/isa.md`
section 2 as codes (the code tables are in section 3 there):

```
[opcode, modifier, a_mode, a_value, b_mode, b_value]
```

Opcode `0..14` (`DAT`=0 .. `NOP`=14), modifier `0..6` (`A`=0 .. `I`=6), mode `0..7`
(`#`=0 .. `>`=7), values `0 .. N-1` (stored values, `Norm` already applied). The
writer id of a cell is not in the array: in a `load` line it is the line's bot, in a
`w` line it is the bot of the step (a write always sets the writer to the executing
bot, isa.md section 2).

## 4. Line types

Field names and their order are fixed; a line with another field, order or spelling
is not valid.

### `header`

```
{"t":"header","version":1,"config":{"core_size":8000,"cycle_limit":80000,"max_processes":8000,"min_separation":100,"seed":"1"},"bots":[{"id":1,"hash":"<64 hex>","length":1,"start":4519,"entry_address":4519},{"id":2,...}]}
```

| Field | Meaning |
| --- | --- |
| `version` | the integer 1 (section 7) |
| `config` | the five match fields of match.md section 1, snake case; `seed` is a decimal string without leading zeros, `0 .. 2^64 - 1` |
| `bots[i].id` | the bot id `i + 1`, ascending from 1, `2 .. 1048576` bots |
| `bots[i].hash` | the bot hash (section 6) |
| `bots[i].length` | `L_i`, the number of cells, `1 .. N` |
| `bots[i].start` | the start address `a_i` of the placement, `0 .. N-1` |
| `bots[i].entry_address` | the absolute address `Norm(a_i + e_i)` of the bot's first process, `0 .. N-1` (not the entry offset `e_i` of match.md section 5) |

The ranges of `config` are those of match.md section 1.

### `load`

```
{"t":"load","bot":1,"cells":[[...],[...]]}
```

The bot's cells as loaded (match.md section 5): `cells[k]` is the cell at address
`Norm(start + k)` with writer `bot`. `cells` has exactly `length` entries. The empty
core of every other address is `DAT.F $0, $0` with writer 0, so after the `load`
lines the viewer knows the whole initial core (cycle 0). Each bot starts with one
process, at `entry_address`.

### `c` (cycle)

```
{"t":"c","n":1}
```

Opens cycle `n`. The cycles are numbered `1, 2, ...` and every cycle of the match has
its line, so `n` is one more than the `n` of the previous `c` line and the first is 1.
A cycle has at least one step: the match runs only while two bots are live, so a
cycle without a live process cannot occur.

### `x` (execute)

```
{"t":"x","b":1,"pc":4519}
```

One step: bot `b` (`1 .. K`) executed the instruction at address `pc` (`0 .. N-1`),
the front process of its queue (match.md section 7). There is one `x` line for every
step, also for a step that changes nothing (a `JMP`, a `NOP`, a test that skips), so
the `pc` values of a cycle are exactly the executed addresses: the execution pointers
a viewer draws. Within a cycle the steps are in ascending bot id, and a bot has at
most one step per cycle. A bot has a step in a cycle only while it has a live process
(see `s` and `d`). The event lines that follow an `x` line, up to the next `x`, `c` or
`end` line, belong to that step.

### `w` (write)

```
{"t":"w","a":4520,"c":[1,6,1,0,1,1]}
```

A cell write in the step above it: address `a` (`0 .. N-1`) now holds the cell `c`,
with the step's bot as writer. Predecrements, the destination write and
post-increments each write, in the order of isa.md section 4; a write of an unchanged
value is still a write (vm event rule, C61).

### `s` (spawn) and `d` (death)

```
{"t":"s","a":12}
{"t":"d"}
```

`s`: the `SPL` of the step added a process of the step's bot at address `a`
(`0 .. N-1`). `d`: the process that executed the step died (`DAT`, or a division by
zero), so a death needs no address, it is the `pc` of the step. A step has at most one
`s` or `d`, and it is the step's last line, after its writes. Normal continuation of a
process is not recorded. A viewer counts the live processes of bot `b` as 1 at the
start, plus its `s` lines, minus its `d` lines; the bot is live while the count is
above 0. Where each process goes next is not recorded, but the next `x` line of the
bot names the address its next process executes.

### `end`

```
{"t":"end","cycles":80000,"survivors":[1,2],"outcome":"tie","winner":0,"tied":[1,2]}
```

The result of match.md section 8: `cycles` (`1 .. cycle_limit`, equal to the `n` of
the last `c` line), `survivors` and `tied` (ascending ids, possibly empty), `outcome`
(`win` or `tie`) and `winner` (0 for a tie). A `win` has `survivors` equal to
`[winner]` and an empty `tied`; a `tie` has `winner` 0 and a non-empty `tied`.

## 5. Playing a replay

State at cycle 0: the empty core with the `load` cells written. State at cycle `c`:
that state with the events of every cycle up to and including cycle `c` applied, in
file order; a `w` writes cell `c` with the step's bot as writer at `a`. This is
exactly the core of the live match after cycle `c`, and the `x` lines of cycle `c` are
exactly the live steps of that cycle, bot and address (`TestReplayEquivalence`).

There are no snapshot lines in version 1 (C85): a viewer that wants to seek keeps
its own checkpoints while reading, and adding snapshot lines would be a new version
(section 7).

## 6. Hashes

Both hashes are SHA-256 (FIPS 180-4) printed as 64 lowercase hexadecimal characters.

- **Replay hash**: SHA-256 of the bytes of the whole file, from the first byte of the
  `header` line to the line feed that ends the `end` line.
- **Bot hash**: SHA-256 of this byte string: the entry offset `e` as 4 bytes (big
  endian, unsigned), then for each cell of the bot as the assembler produced it (raw
  signed values, not reduced with `Norm`, catalog C69) 12 bytes: opcode (1), modifier
  (1), a_mode (1), a_value (4, big endian two's complement), b_mode (1), b_value (4,
  big endian two's complement). The writer field is not part of it. The hash does
  not depend on the core size or the placement. Readers cannot recompute it from
  the file (the `load` cells are normalized and carry no raw values), so they treat
  it as an opaque identifier.

## 7. Versions

The first line of every version of the format starts `{"t":"header","version":` and
the version is a decimal integer, so a reader can find it before interpreting
anything else. A reader rejects a version it does not implement, with an error that
names the version (`replay: unsupported version 2 (this reader reads version 1)`).
Any change to the line types, field names, order, number ranges, the meaning of a
field or the hash encodings is a new version number; there are no minor versions
and no optional fields.

## 8. Strictness

A reader validates the whole file and returns an error, never a partial replay:
the order of section 2, the ranges of section 4 (every address `0 .. N-1`, every bot
id `1 .. K`, every code a valid code, `cells` as long as `length`), the id
sequence, consecutive cycles, ascending bots within a cycle, a bot stepping only while
its process count (from `s` and `d`) is above 0, at most one `s` or `d` per step and
as its last line, the result's consistency, and that the line is byte for
byte the line the writer would produce for the values it holds (so no other
spacing, field order, number spelling or duplicate and unknown fields). A file that
does not end with a line feed, or has bytes after the `end` line, is not valid.

## 9. Reference output

`internal/replay/testdata/imp_dwarf_seed1.jsonl` is the replay of the starter bots
`bots/imp.s` and `bots/dwarf.s` (the dwarf has five cells) with `--seed 1
--cycle-limit 40`; the other settings are the defaults of match.md section 1. A
reduced cycle limit keeps the fixture small (5.9 KB). Its replay hash is pinned in
`internal/replay/replay_test.go` as `goldenHash`
(`24f20dbf52fa877d9c4c92cfcf109593a45540ebe2d4fcbc52389f06c4ecc26e`).

At the default limit of 80000 the same match ends at cycle 5880 with a win for the
dwarf, because the imp's process is bombed. Its replay is 782675 bytes, about 0.8 MB;
the size is roughly 130 bytes per cycle for two bots (a `c` line, two `x` lines and
the writes), so a match that runs to the default limit with two bots is about 10 MB.
The hash of the default-limit replay is pinned as `fullHash`
(`0c7024334a7c20fe7f74b39b97d0c9693980d41f0351f3422846c3e2caeac831`); it is the C16
check, which `pit run bots/imp.s bots/dwarf.s --seed 1 --replay FILE` reproduces
(twice, with identical files). `pit run bots/imp.s bots/dwarf.s --seed 1
--cycle-limit 40 --replay FILE` writes the fixture again, byte for byte.

How the pinned values were checked. A short Python stepper, written from isa.md,
match.md and this document, implements placement and the few opcodes and modes the
two bots use. It reads the assembled cells from the output of `pit asm`. It produced
the fixture and both hashes, and they were compared with the Go writer's output before
they were pinned. The stepper is not in the repository (it covers only these
opcodes); the pinned bytes and hashes are the record of its output. The first cycles
of the fixture were also checked by hand against isa.md: in cycle 1 the imp at 4519
copies itself to 4520 and the dwarf at 2555 adds 4 to the B field of its pointer cell
(2558), and in cycle 2 the dwarf copies its bomb cell (2559) to 2558 + 4 = 2562.

## 10. Decisions

Implementer decisions under catalog C35 (slice S11), each recorded in the catalog
with its reason:

| Catalog | Decision | Section |
| --- | --- | --- |
| C83 | hand-written canonical lines; the reader accepts only the writer's bytes | 1, 8 |
| C84 | line types and field names, a `c` line for every cycle, an `x` line for every step | 2, 4 |
| C85 | no snapshot lines in version 1 | 5 |
| C86 | replay hash and bot hash encodings | 6 |
| C87 | `internal/replay` API (`Record`, `Read`, `Bytes`, `Hash`, `BotHash`, `Player`) | - |
| C88 | `arena.RunObserved`, the per-cycle hook with the steps (bot, executed address, events) the writer uses | - |
| C89 | `pit run --replay FILE` and the printed hash | - |

The owner's amendments of 2026-10-18 to C76 and C77 (seed as a decimal string,
`entry_address` for the absolute address of a bot's first process) are in the header
and in `pit run --json`.
