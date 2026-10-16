# pitfight match specification

This document defines how one match runs: the configuration and its defaults, the
seeded random number generator, the placement of bots in the core, the order in
which bots take turns, when a match ends, and its result with the tie rule. The
arena (`internal/arena`) is built and tested against it.

The instruction semantics are not repeated here; `docs/spec/isa.md` is normative
for them. This document relies on these parts of it: section 1 (the circular core of
`N = CORESIZE` cells, `Norm`, stored values), section 2 (the cell, the writer field,
bot ids from 1, the empty cell), section 4 (a step executes the front process of one
bot and changes only the core and that bot's own queue) and section 5 (`DAT` and `DIV` by zero, the only ways a process dies; `SPL` with
`MAXPROCESSES`, the only way one is added).

Out of scope here: tournament scoring, the replay format, the assembler (which
produces a bot's cells and entry offset) and the implementation.

## 1. Configuration

A match is a configuration plus a list of bots (section 5). The configuration has
five fields. All numbers are integers; `N` below is `CORESIZE`.

| Field | Meaning | Type | Default | Valid range |
| --- | --- | --- | --- | --- |
| `CORESIZE` | number of cells in the core | integer | 8000 | `1 .. 1048576` (2^20) |
| `CYCLELIMIT` | maximum number of cycles (section 7) | integer | 80000 | `1 .. 2147483647` (2^31 - 1) |
| `MAXPROCESSES` | live processes allowed per bot (isa.md section 5, `SPL`) | integer | 8000 | `1 .. 1048576` (2^20) |
| `MINSEPARATION` | empty cells guaranteed between neighbouring bots (section 6) | integer | 100 | `0 .. 1048576` (2^20) |
| `SEED` | seed of the placement generator (section 4) | unsigned 64-bit | 0 | `0 .. 2^64 - 1`, every value valid |

`CORESIZE` has the range of isa.md section 1. `CYCLELIMIT` and `MAXPROCESSES` have
upper bounds that keep a cycle count in a signed 32-bit integer and the queue of a
bot no longer than the largest core. `MINSEPARATION` may exceed what a given core can
hold; that is a placement error (section 6), not a range error. The defaults of the
first three fields come from the Redcode '94 hill norms and are verified in
section 2. The default `MINSEPARATION` and `SEED` are decisions of this document.

### Validation

A match is refused before anything runs when one of these checks fails. The checks
run in this order and the first failure is the error. `<v>` is the offending value in
decimal.

| Order | Check | Error message |
| --- | --- | --- |
| 1 | `CORESIZE` in range | `invalid config: CORESIZE <v> out of range 1..1048576` |
| 2 | `CYCLELIMIT` in range | `invalid config: CYCLELIMIT <v> out of range 1..2147483647` |
| 3 | `MAXPROCESSES` in range | `invalid config: MAXPROCESSES <v> out of range 1..1048576` |
| 4 | `MINSEPARATION` in range | `invalid config: MINSEPARATION <v> out of range 0..1048576` |
| 5 | number of bots `K` in `2 .. 1048576` | `invalid bots: need 2..1048576 bots, got <K>` |
| 6 | for each bot in id order, first its length `L` in `1 .. 1048576` | `invalid bots: bot <id> length <L> out of range 1..1048576` |
| 7 | for each bot in id order, then its entry offset `e` in `[0, L)` | `invalid bots: bot <id> entry offset <e> outside 0..<L-1>` |
| 8 | placement possible | `placement impossible: ...` (section 6) |

Checks 6 and 7 run bot by bot: bot 1's length, bot 1's entry offset, bot 2's length,
and so on. The error kinds are named `InvalidConfig` (checks 1 to 4), `InvalidBots`
(5 to 7) and `PlacementImpossible` (8).

## 2. Default parameters: verification of C30

Catalog decision C30 accepted core 8000, cycle limit 80000 and max processes 8000,
marked unverified. They were checked on 2026-10-15 against public documentation,
read-only. Quoted strings are the text returned for each page; runs of whitespace in
the quotes from the hill page are collapsed to one space.

| Value | Source | What it says |
| --- | --- | --- |
| core size 8000 | [ICWS'94 draft, "Predefined variables"](https://corewar.info/lexicon/94draft.htm) | `CORESIZE`: "value of -s parameter (default: 8000)" |
| | [94 Standard Hill](http://www.corewar.info/hills/94.htm) | "Core size: 8000" |
| | [pMARS manual](https://manpages.ubuntu.com/manpages/noble/man6/pmars.6.html) | `-s`: "It defaults to 8000." |
| cycle limit 80000 | [ICWS'94 draft, "Predefined variables"](https://corewar.info/lexicon/94draft.htm) | `MAXCYCLES`: "value of -c parameter (default: 80000)" |
| | [94 Standard Hill](http://www.corewar.info/hills/94.htm) | "Max cycles: 80000" |
| | [pMARS manual](https://manpages.ubuntu.com/manpages/noble/man6/pmars.6.html) | `-c` "sets the maximum number of cycles per round"; the manual gives no default for it |
| max processes 8000 | [ICWS'94 draft, "Predefined variables"](https://corewar.info/lexicon/94draft.htm) | `MAXPROCESSES`: "value of -p parameter (default: 8000)" |
| | [94 Standard Hill](http://www.corewar.info/hills/94.htm) | "Max processes: 8000" |
| | [pMARS manual](https://manpages.ubuntu.com/manpages/noble/man6/pmars.6.html) | `-p`: "The default is 8000." |

All three values are confirmed: each is stated by the draft's predefined variables
and by the Standard Hill page. The pMARS manual confirms two of them and states no
default for the cycle limit. The same draft page lists `MINDISTANCE` (default 100)
and `MAXLENGTH` (default 100). `MAXLENGTH` is not a pitfight parameter. The pMARS manual
describes its `-d` option, whose default is 100, as "the minimum distance between the
first instruction of each warrior", and says it "cannot be smaller than the maximum
length (-l option)". So pMARS measures from the first instruction of one warrior to
the first instruction of the next. Pitfight deliberately counts the empty cells between
blocks instead (`MINSEPARATION`, section 6): the placement test is then an exact sum
that does not depend on the length of any bot, and a gap of `S` empty cells puts the
start addresses at least `L + S` apart, which is stricter than a start-to-start
distance of 100 for every bot length. Only the number 100 is taken from pMARS.

The pMARS manual also defines a cycle as "one execution of each warrior", a round as
ending "when either a single warrior remains executing or when the maximum number of
cycles has elapsed", and a tie as a round in which "both warriors lasted until
time-out". Sections 7 and 8 follow these ideas; where they differ or add detail, the
text says so.

## 3. Integer widths

Every quantity is an exact integer, never floating point.

- `SEED`, the generator state, generator outputs and the argument `n` of `Draw` are
  unsigned 64-bit integers. Addition and multiplication on them are modulo 2^64
  (keep the low 64 bits). Right shifts are logical (zero fill).
- Positions, lengths, gaps and sums in section 6 are signed 64-bit integers. After
  the checks of section 1 that run before placement, `K <= 2^20`, each
  `L_i <= 2^20` and `S <= 2^20`, so `T + K * S` is at most `2^40 + 2^40` and cannot
  overflow.
- A cycle count is a signed 32-bit integer (at most `CYCLELIMIT <= 2^31 - 1`).

## 4. The seeded generator

Randomness enters a match only here, only for placement (section 6). The VM draws
nothing. The generator is in-repo and fixed (catalog decision C36); no generator from
the Go standard library is used, so results survive Go upgrades.

### Algorithm: SplitMix64

State: one unsigned 64-bit integer `s`. Seeding: `s = SEED`. Every seed, including 0,
is valid. `Next()`:

```
s = s + 0x9E3779B97F4A7C15            (mod 2^64)
z = s
z = (z xor (z >> 30)) * 0xBF58476D1CE4E5B9    (mod 2^64)
z = (z xor (z >> 27)) * 0x94D049BB133111EB    (mod 2^64)
return z xor (z >> 31)
```

This is the fixed-increment SplitMix64 generator as published by Sebastiano Vigna
([splitmix64.c](https://prng.di.unimi.it/splitmix64.c), public domain), a fixed
increment version of Java 8's SplittableRandom; the constants above are those of that
file. It uses only integer addition, multiplication, xor and shifts.

### Bounded draw

`Draw(n)` returns an integer in `[0, n)` for an unsigned 64-bit `n >= 1`, without
modulo bias, by rejection:

```
t = (2^64 - n) mod n          computed in unsigned 64-bit as (0 - n) mod n
loop:
    x = Next()
    if x >= t: return x mod n
```

The accepted values `x >= t` number `2^64 - t`, a multiple of `n`, so each result in
`[0, n)` is equally likely. `Draw` always consumes at least one output of `Next()`,
including `Draw(1)` (where `t = 0` and the result is 0), and consumes exactly the
outputs up to and including the first accepted one. Placement uses `n <= 2^20 + 1`,
for which a rejection has probability below 2^-43 per output, so it practically never
rejects; the rejection path is defined for every `n` and tested with a large `n`
below.

### Test vectors

Computed with the algorithm above (see the note at the end of section 4). `Next()`
outputs, first to tenth, for three seeds.

Seed 0:

| # | Hex | Decimal |
| --- | --- | --- |
| 1 | `0xE220A8397B1DCDAF` | 16294208416658607535 |
| 2 | `0x6E789E6AA1B965F4` | 7960286522194355700 |
| 3 | `0x06C45D188009454F` | 487617019471545679 |
| 4 | `0xF88BB8A8724C81EC` | 17909611376780542444 |
| 5 | `0x1B39896A51A8749B` | 1961750202426094747 |
| 6 | `0x53CB9F0C747EA2EA` | 6038094601263162090 |
| 7 | `0x2C829ABE1F4532E1` | 3207296026000306913 |
| 8 | `0xC584133AC916AB3C` | 14232521865600346940 |
| 9 | `0x3EE5789041C98AC3` | 4532161160992623299 |
| 10 | `0xF3B8488C368CB0A6` | 17561866513979060390 |

Seed 1:

| # | Hex | Decimal |
| --- | --- | --- |
| 1 | `0x910A2DEC89025CC1` | 10451216379200822465 |
| 2 | `0xBEEB8DA1658EEC67` | 13757245211066428519 |
| 3 | `0xF893A2EEFB32555E` | 17911839290282890590 |
| 4 | `0x71C18690EE42C90B` | 8196980753821780235 |
| 5 | `0x71BB54D8D101B5B9` | 8195237237126968761 |
| 6 | `0xC34D0BFF90150280` | 14072917602864530048 |
| 7 | `0xE099EC6CD7363CA5` | 16184226688143867045 |
| 8 | `0x85E7BB0F12278575` | 9648886400068060533 |
| 9 | `0x491718DE357E3DA8` | 5266705631892356520 |
| 10 | `0xCB435C8E74616796` | 14646652180046636950 |

Seed 18446744073709551615 (2^64 - 1, the largest seed; the first step wraps):

| # | Hex | Decimal |
| --- | --- | --- |
| 1 | `0xE4D971771B652C20` | 16490336266968443936 |
| 2 | `0xE99FF867DBF682C9` | 16834447057089888969 |
| 3 | `0x382FF84CB27281E9` | 4048727598324417001 |
| 4 | `0x6D1DB36CCBA982D2` | 7862637804313477842 |
| 5 | `0xB4A0472E578069AE` | 13015481187462834606 |
| 6 | `0xD31DADBDA438BB33` | 15212506146343009075 |
| 7 | `0xF14F2CF802083FA5` | 17388166129998380965 |
| 8 | `0x405DA438A39E8064` | 4638043754431676516 |
| 9 | `0xC4FEA708156E0C84` | 14194966728679492740 |
| 10 | `0x031E50FE7BBD6E1C` | 224706085343030812 |

`Draw` vectors: the generator is freshly seeded with `seed`, then `Draw(n)` is called
the stated number of times in a row. Each result is followed by the number of
`Next()` outputs that call consumed.

| Seed | `n` | `t` | Results (outputs consumed) |
| --- | --- | --- | --- |
| 0 | 10 | 6 | 5 (1), 0 (1), 9 (1), 4 (1), 7 (1), 0 (1) |
| 1 | 8000 | 7616 | 6465 (1), 4519 (1), 2590 (1), 4235 (1), 761 (1), 2048 (1) |
| 2 | 1 | 0 | 0 (1), 0 (1), 0 (1) |
| 0 | 9223372036854775809 | 9223372036854775807 | 7070836379803831726 (1), **8686239339925766635 (3)**, **5009149828745571131 (4)**, **8338494477124284581 (2)** |
| 1 | 9223372036854775809 | 9223372036854775807 | 1227844342346046656 (1), 4533873174211652710 (1), 8688467253428114781 (1), **4849545566009754239 (3)** |
| 3 | 9223372036854775809 | 9223372036854775807 | **3694763184872335752 (2)**, 2084015055746161920 (1), **2512858195355979526 (3)**, **7170589470788784661 (2)** |

In the last three rows `n = 2^63 + 1`, so `t = 2^63 - 1` and about half of all
outputs are rejected. A result marked in bold consumed more than one output, so a
rejection really happened. For seed 0, the second call
consumed outputs 2 and 3 of the seed 0 list above (`0x6E789E6AA1B965F4` and
`0x06C45D188009454F`), both below `t` and so rejected, and accepted output 4
(`0xF88BB8A8724C81EC`), giving
`17909611376780542444 mod (2^63 + 1) = 8686239339925766635`. The first call consumed
output 1 (`0xE220A8397B1DCDAF`, 16294208416658607535), which is `>= t` and gives
`16294208416658607535 mod (2^63 + 1) = 7070836379803831726`.

**How the vectors were computed.** Two independent programs, one in Go and one in
Python 3, each written from this section's text, implemented `Next`, `Draw` and the
placement of section 6. They printed identical output for every vector in this
document. The worked matches of section 8 were checked with a third, small Python stepper that
implements section 7 for the three opcodes they use. The programs were kept under
the git-ignored `.cache/` directory and are not committed; S09 turns the vectors
into Go tests.

## 5. Bots

A bot is given to the match as:

- a list of `L >= 1` cells, its *length* being `L`. Each cell is a complete cell in
  the sense of isa.md section 2 (opcode, modifier, modes and values), with operand
  values stored as `Norm(v)` for this match's `N` (isa.md section 1), so a program
  does not depend on `CORESIZE`;
- an *entry offset* `e` with `0 <= e < L`. The assembler decides how a source file
  names `e`; the match only needs the number.

The bots are numbered `1 .. K` in the order given; this is the bot id of isa.md
section 2. `K >= 2`. Their lengths are `L_1 .. L_K`; write `T` for their sum.

At the start of a match the core is empty (isa.md section 2). Placement (section 6)
gives each bot a start address `a_i`. The bot's cells are copied to the addresses
`a_i, a_i + 1, ... , a_i + L_i - 1`, each reduced with `Norm`, and the writer field
of each of these `L_i` cells is set to `i`, so the visualizer shows the loaded code
as owned by its bot. The bot starts with exactly one process, at address
`Norm(a_i + e_i)`. A bot is *live* while it has at least one process; at the start
every bot is live. (`MAXPROCESSES >= 1`, so the first process is always allowed.)

## 6. Placement

### Meaning of `MINSEPARATION`

`S = MINSEPARATION`. The core is circular. Bot `i` occupies the *block* of `L_i`
consecutive addresses starting at `a_i`. Placement is valid when the blocks do not
overlap and, going around the circle, between the last cell of each block and the
first cell of the next block (in address order, the last block wrapping to the
first) there are at least `S` addresses that belong to no block. The measure is the
count of empty cells between blocks, not a distance between start addresses as in the
pMARS `-d` option (section 2).

### When placement is impossible

Around the circle there are `K` gaps (one after each block), each at least `S`. A
valid placement exists if and only if

```
T + K * S <= N
```

(this also covers a bot longer than the core, `L_i > N`). The test is
exact and uses no search or retry. When it fails the error is
`PlacementImpossible` with this message, where `K`, `T`, `S` and `N` are decimal and
`need` is `T + K * S`:

```
placement impossible: <K> bots of total length <T> with min separation <S> need <need> cells, core size is <N>
```

Note that `K = 2` bots of length 1 with the default `MINSEPARATION = 100` need
202 cells, so a core smaller than that needs an explicit smaller separation.

### Algorithm

Let `F = N - (T + K * S)`, the slack (`F >= 0`). Create the generator with
`s = SEED` and run these steps in this order, with no other use of the generator:

1. **Order.** Let `p` be the list `[1, 2, ..., K]` (0-based positions `0 .. K-1`).
   For `i` from `K - 1` down to `1`: `j = Draw(i + 1)`; swap `p[i]` and `p[j]`.
   (`K - 1` draws.) `p[0], p[1], ...` is the order of the bots around the circle.
2. **Rotation.** `r = Draw(N)`. (One draw.)
3. **Gaps.** `R = F`. For `k` from `0` to `K - 2`: `g[k] = Draw(R + 1)`;
   `R = R - g[k]`. Then `g[K-1] = R`. (`K - 1` draws; the sum of `g` is `F`.)
   Every draw is made even when `R = 0`.
4. **Start addresses.** `pos = r`. For `k` from `0` to `K - 1`: `a[p[k]] = pos`;
   `pos = (pos + L[p[k]] + S + g[k]) mod N`.

In total `2K - 1` draws are made. The gap after the block of bot `p[k]` is
`S + g[k]` empty cells, so the constraints of the previous subsection hold, and
the final `pos` equals `r` again because the lengths, separations and `F` add to `N`.
The distribution of the gaps is not uniform (earlier gaps tend to be larger); the
order of the bots is a uniform permutation and the rotation is uniform, and the
rules are fixed so that two implementations agree.

The generator is not used after placement.

### Worked placements

Each row gives a complete input and the result of the algorithm. `L` lists
`L_1, L_2, ...`; `N`, `S`, `SEED` as in section 1. `order` is `p`, `rot` is `r`,
`gaps` is `g`, `starts` is `a_1, a_2, ...` for bots 1, 2, ...

| Case | `SEED` | `N` | `S` | `L` | `order` | `rot` | `gaps` | `starts` |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| P1 (default config) | 1 | 8000 | 100 | 100, 100 | 1, 2 | 4519 | 6677, 923 | 4519, 3396 |
| P2 (default config) | 2 | 8000 | 100 | 100, 100 | 2, 1 | 4226 | 6918, 682 | 3344, 4226 |
| P3 (default config) | 0 | 8000 | 100 | 5, 12, 30 | 3, 1, 2 | 1679 | 1272, 2457, 3924 | 3081, 5643, 1679 |
| P4 (no slack; bot 4 wraps) | 7 | 100 | 0 | 10, 20, 30, 40 | 2, 3, 1, 4 | 3 | 0, 0, 0, 0 | 53, 3, 23, 63 |
| P5 (largest seed) | 18446744073709551615 | 64 | 4 | 8, 8, 8 | 1, 2, 3 | 41 | 2, 6, 20 | 41, 55, 9 |
| P6 (tiny core) | 42 | 8 | 0 | 1, 1 | 1, 2 | 3 | 0, 6 | 3, 4 |

Checks by hand. P1: `F = 8000 - 200 - 200 = 7600`; the draws are `Draw(2)`
(order), `Draw(8000)` (rotation, 4519), `Draw(7601)` (first gap 6677), so the last
gap is `7600 - 6677 = 923`. Bot 1 occupies 4519..4618; bot 2 starts at
`(4519 + 100 + 100 + 6677) mod 8000 = 3396`. P4: bot 2 occupies 3..22, bot 3
23..52, bot 1 53..62, bot 4 63..99 and 0..2 (wrapping), with no empty cells since
`F = 0` and `S = 0`. P5: bot 1 41..48, bot 2 starts at `41 + 8 + 4 + 2 = 55`, bot 3
at `(55 + 8 + 4 + 6) mod 64 = 9` and the last gap of `4 + 20 = 24` cells leads back to 41.

Impossible placements, with the exact error:

| Case | `SEED` | `N` | `S` | `L` | Result |
| --- | --- | --- | --- | --- | --- |
| X1 | 1 | 8000 | 100 | 4000, 3900 | `placement impossible: 2 bots of total length 7900 with min separation 100 need 8100 cells, core size is 8000` |
| X3 | 1 | 200 | 10 | 91, 90 | `placement impossible: 2 bots of total length 181 with min separation 10 need 201 cells, core size is 200` |
| X4 | 1 | 8 | 100 | 1, 1 | `placement impossible: 2 bots of total length 2 with min separation 100 need 202 cells, core size is 8` |
| X5 | 1 | 8 | 0 | 5, 4 | `placement impossible: 2 bots of total length 9 with min separation 0 need 9 cells, core size is 8` |

The boundary case X2 (`SEED` 1, `N` 200, `S` 10, `L` 90, 90) needs exactly 200
cells and is possible: order 1, 2, rotation 119, gaps 0, 0, starts 119 and 19.
X3 differs from X2 only in `L_1 = 91` and is impossible. X5 fails because
`T + K * S = 9 > 8`, although `S = 0`.

## 7. Running a match

### Cycle and turn order

A match is a sequence of *cycles* numbered `1, 2, 3, ...`. In cycle `c`, the bots
that are live at the start of the cycle take one step each, in ascending bot id.
A *step* is the step of isa.md section 4 for the process at the front of that bot's
queue; it takes the front process, executes its cell and appends the addresses the
opcode names (none if the process dies). A bot takes exactly one step per cycle
regardless of how many processes it has; its processes take turns in queue order over
successive cycles. One cycle counts as one unit of `CYCLELIMIT` however many bots
are live.

Because a step changes only the core and the stepping bot's own queue
(isa.md section 4), a bot's liveness can only change in its own step. So every bot
that is live at the start of a cycle takes its step in that cycle, even if a bot
before it in the order died earlier in the same cycle, and the order of the bots
within the cycle (ascending id) is fixed for the whole match: bot ids are not
rotated between cycles. The ids are assigned by the order in which the bots are given
to the match, so the match itself gives the lower ids the first move in every cycle. Removing
that effect, for example by playing a pair of bots in both orders, is a matter for
the tournament runner, not for the match.

The process cap `MAXPROCESSES` is applied by `SPL` as in isa.md section 5: a bot
with `MAXPROCESSES` live processes cannot add one, and nothing else limits the
number of processes. A bot's queue therefore never holds more than `MAXPROCESSES`
addresses.

### End of the match

After each cycle `c` the match checks, in this order:

1. If at most one bot is live, the match ends.
2. Otherwise, if `c = CYCLELIMIT`, the match ends.

Otherwise cycle `c + 1` follows. The check is made only at the end of a whole
cycle, so every bot that was live at the start of the last cycle took its step in it
even if another bot had already died in that cycle. The match always ends, after at
most `CYCLELIMIT` cycles. At least one cycle is always run, since there are at least
two live bots at the start.

## 8. Result and tie rule

A result has these fields:

| Field | Meaning |
| --- | --- |
| `cycles` | the number `c` of the last cycle run (`1 .. CYCLELIMIT`) |
| `survivors` | the ids of the bots live when the match ended, in ascending order (possibly empty) |
| `outcome` | `win` or `tie` |
| `winner` | the id of the winning bot when `outcome` is `win`; `0` when `outcome` is `tie` |
| `tied` | empty when `outcome` is `win`; when `outcome` is `tie`, the ids of the tied bots, ascending |

### Tie rule

- Exactly one survivor: `outcome = win`, `winner` is that bot. This holds whether the
  match ended because the other bots died or because the cycle limit was reached in
  the same cycle in which the others died.
- Otherwise `outcome = tie`:
  - two or more survivors (the cycle limit was reached with several bots live):
    `tied = survivors`;
  - no survivor (all remaining bots died in the last cycle): `tied` is the set of
    bots that were live at the start of the last cycle, the bots that died together
    in it.

Bots that died in an earlier cycle are neither a winner nor tied. There is no
tiebreak by process count, number of cells owned or any other measure.

Reason for this shape. A win needs the opponents to be gone, which is what
"a single warrior remains executing" means in the pMARS manual quoted in
section 2; surviving to the limit alongside another bot is the "both lasted until
time-out" tie of the same manual. The no-survivor case, which that wording does not
cover, is also a tie among the last live bots, because they died in the same cycle
and a complete cycle gives each of them the same number of steps.

### Worked matches

These matches use cells from isa.md and need no knowledge of the start addresses
(the bots never touch each other's cells), so any valid placement gives the same
result. Use `CORESIZE = 16`, `MINSEPARATION = 0`, `MAXPROCESSES = 8` and any `SEED`. Cells
are in the notation of isa.md section 2; entry offset is 0 for every bot.
`JMP.B $0, $0` jumps to itself forever (the A target of `$0` is its own address).
`DAT.F $0, $0` kills the process; `NOP.F $0, $0` continues at `PC + 1`.

| Case | `CYCLELIMIT` | Bot 1 | Bot 2 | Bot 3 | `cycles` | `survivors` | `outcome` | `winner` | `tied` |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| M1 | 10 | `DAT.F $0, $0` | `JMP.B $0, $0` | - | 1 | 2 | `win` | 2 | (none) |
| M2 | 10 | `DAT.F $0, $0` | `DAT.F $0, $0` | - | 1 | (none) | `tie` | 0 | 1, 2 |
| M3 | 5 | `JMP.B $0, $0` | `JMP.B $0, $0` | - | 5 | 1, 2 | `tie` | 0 | 1, 2 |
| M4 | 10 | `JMP.B $0, $0` | `NOP.F $0, $0` then `DAT.F $0, $0` | - | 2 | 1 | `win` | 1 | (none) |
| M5 | 3 | `JMP.B $0, $0` | `NOP.F $0, $0`, `NOP.F $0, $0`, `DAT.F $0, $0` | - | 3 | 1 | `win` | 1 | (none) |
| M6 | 10 | `DAT.F $0, $0` | `NOP.F $0, $0` then `DAT.F $0, $0` | `NOP.F $0, $0` then `DAT.F $0, $0` | 2 | (none) | `tie` | 0 | 2, 3 |

How they follow. M1: in cycle 1 bot 1 executes `DAT` and dies, bot 2 steps and
stays live; one bot is live at the end of the cycle, so the match ends. M2: both
die in cycle 1 and none survives, so `tied` is the two bots live at the start of
cycle 1. M3: both loop to the limit. M4: bot 2 executes `NOP` in cycle 1 and `DAT`
in cycle 2, so the match ends after cycle 2 with bot 1 the only survivor. M5: bot 2
dies in cycle 3, which is also the cycle limit; one survivor, so bot 1 wins with
`cycles = 3`. M6 has three bots, bot 3 being identical to bot 2: bot 1
dies in cycle 1 and two bots are still live, so the match goes on; bots 2 and 3 die
in cycle 2, no one survives and `tied` is 2, 3, the bots live at the start of cycle
2. Bot 1 died in an earlier cycle and is not tied.

## 9. Decisions

The values and rules above are implementer decisions under catalog decisions C35 and
C38, each recorded in the catalog with a one-line reason. Where they use a source
(section 2) or an algorithm (section 4) it is cited there.

| Catalog | Decision | Section |
| --- | --- | --- |
| C51 | defaults confirmed against sources (C30 verified), ranges, default seed | 1, 2 |
| C52 | SplitMix64 and `Draw` | 4 |
| C53 | tie rule | 8 |
| C54 | default `MINSEPARATION` and its meaning | 1, 6 |
| C55 | placement algorithm | 6 |
| C56 | turn order, cycle, end of match, loading, bot count, result fields | 5, 7, 8 |
