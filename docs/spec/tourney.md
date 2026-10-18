# Tournaments

`pit tourney DIR` runs a round-robin tournament of the bots in a directory and prints
a scoreboard. This document is the specification of what it plays, how it scores and
what it prints. A match is as defined in [match.md](match.md); the implementation is
`internal/tourney` and `cmd/pit/tourney.go`. There is no persistent state: results
are files (catalog decision C39).

## 1. Bots

The bots are the `*.s` files directly inside `DIR` (not in subdirectories, and not a
directory that happens to be named `x.s`), sorted by file name in byte order. A bot is
named after its file without `.s`. The bot list, in that order, fixes the bot indexes
`0 .. n-1` used below. At least 2 bots are needed. An assembly error in any file ends
the tournament with that error (exit code 1).

## 2. Rounds, pairings and seeds

A tournament has `R` rounds, numbered from 1 (default 100).

- **Pairings.** The pairings of `n` bots are the unordered pairs `(a, b)` with
  `a < b`, ordered by `a` and then `b`: `n(n-1)/2` pairs.
- **Both orders.** Match ids are not rotated and the lower id steps first in every
  cycle (match.md section 7). So every pair plays twice per round: first with `a` as
  bot id 1 and `b` as bot id 2, then with `b` as id 1 and `a` as id 2.
- **Match list.** Round 1 to `R`; within a round the pairs in pairing order; for each
  pair the order `(a, b)` and then `(b, a)`. The match index counts from 0 along this
  list. There are `R * n(n-1)` matches.
- **Seed.** The seed of round `r` is `Seed(r)`, the first output `Next()` of the
  SplitMix64 generator of match.md section 4 seeded with `r`. For example
  `Seed(1) = 10451216379200822465` (the first output for seed 1 in the test vectors)
  and `Seed(2) = 10905525725756348110`. Every match of a round uses the round's seed
  as the placement seed; the other configuration fields come from the flags.

## 3. Scoring and ranking

A win scores 3 points, each bot of a tied match 1 point and a loss 0. A match is
tied when match.md section 8 says so. For each bot the scoreboard totals over all its
matches:

| Column | Meaning |
| --- | --- |
| `points` | total points |
| `played` | matches played: `2 * R * (n-1)` |
| `wins`, `ties`, `losses` | matches by result for this bot |
| `cycles` | sum of the cycle counts of its matches (JSON only) |
| `mean_cycles` | `cycles / played`, rounded to 2 decimals |

Bots are ranked by more points, then more wins, then the name in byte order. Rank
counts from 1 and is never shared.

## 4. Parallel execution and determinism

Matches run on `--jobs` workers (goroutines). Each result is stored in the slot of its
match index, and everything printed is computed from the results in match order, so
the output does not depend on the worker count or on the order in which matches finish.
Printing never iterates a map. The same bots, rounds and configuration give the same
bytes. A tournament only scores the matches, so it keeps no VM events, only the
result of each match.

## 5. Output

Text (default):

```
tourney: 4 bots, 100 rounds, 1200 matches, core size 8000, cycle limit 80000, max processes 8000, min separation 100
scoring: win 3, tie 1, loss 0
rank  bot         points  played  wins  ties  losses  mean_cycles
   1  dwarf          944     600   250   194     156     28860.73
```

The bot name is left-aligned and the other columns right-aligned, separated by two
spaces. The worker count is not printed.

`--json` prints only the scoreboard, one JSON object per bot, best first, with the
fields `rank`, `bot`, `points`, `played`, `wins`, `ties`, `losses`, `cycles` and
`mean_cycles` in that order.

`--matches FILE` also writes one JSON object per match in match index order, with the
fields `index`, `round`, `seed`, `first`, `second`, `outcome` (`win` or `tie`),
`winner` (the bot's name, or `null` for a tie) and `cycles`. `first` is the bot with
id 1. Seeds are decimal strings (amended C77), so no reader rounds a 64-bit
seed. Both JSON forms are JSON lines with the same keys on every line,
so a tool such as DuckDB reads them directly (`read_json('matches.jsonl')`).

## 6. Errors

Exit code 0: the tournament ran. 1: the directory cannot be read, has fewer than 2
bots, a bot does not assemble, the configuration is refused (the messages of match.md
section 1; placement depends on the lengths of each pair, so some matches may fail and
others not, and the error of the lowest-numbered failing match is reported) or `FILE` cannot be written. 2: a usage error, including `--rounds` or
`--jobs` below 1.
