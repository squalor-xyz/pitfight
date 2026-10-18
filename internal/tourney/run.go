// SPDX-License-Identifier: AGPL-3.0-or-later

package tourney

import (
	"sync"

	"github.com/squalor-xyz/pitfight/internal/arena"
)

// Bot is a named bot of a tournament.
type Bot struct {
	Name string
	Bot  arena.Bot
}

// MatchResult is the outcome of one match. Winner is the winning bot's index into
// the bot list, or -1 for a tie.
type MatchResult struct {
	Match
	Cycles int
	Winner int
}

// Run plays every match of Matches(len(bots), rounds) with the configuration cfg
// (its Seed is replaced by each match's seed) on jobs workers, at least one, and
// returns the results in match order. Each worker writes only the result slots of
// the matches it takes, so the order of the results does not depend on which worker
// finishes first. If any match is refused, Run returns the error of the lowest
// match index and no results.
func Run(cfg arena.Config, bots []Bot, rounds, jobs int) ([]MatchResult, error) {
	matches := Matches(len(bots), rounds)
	results := make([]MatchResult, len(matches))
	errs := make([]error, len(matches))
	jobs = max(1, min(jobs, len(matches)))

	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				results[i], errs[i] = playMatch(cfg, bots, matches[i])
			}
		}()
	}
	for i := range matches {
		next <- i
	}
	close(next)
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// playMatch plays one match; a test replaces it to observe the workers.
var playMatch = play

// play runs one match.
func play(cfg arena.Config, bots []Bot, m Match) (MatchResult, error) {
	cfg.Seed = m.Seed
	// A tournament scores the match and needs none of its events: with no observer
	// the arena keeps only the events of one cycle, not those of the whole match.
	res, err := arena.RunObserved(cfg, []arena.Bot{bots[m.First].Bot, bots[m.Second].Bot}, nil)
	if err != nil {
		return MatchResult{}, err
	}
	out := MatchResult{Match: m, Cycles: res.Cycles, Winner: -1}
	if res.Outcome == arena.Win {
		out.Winner = m.First
		if res.Winner == 2 {
			out.Winner = m.Second
		}
	}
	return out, nil
}
