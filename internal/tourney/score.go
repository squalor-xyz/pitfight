// SPDX-License-Identifier: AGPL-3.0-or-later

package tourney

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// The scoring rule: a win scores 3 points, each bot of a tied match 1 and a loss 0.
const (
	WinPoints  = 3
	TiePoints  = 1
	LossPoints = 0
)

// Standing is one bot's totals over the tournament. Cycles is the sum of the cycle
// counts of the Played matches.
type Standing struct {
	Name                               string
	Points, Played, Wins, Ties, Losses int
	Cycles                             int64
}

// MeanCycles is the mean number of cycles of the bot's matches, rounded to two
// decimals; 0 if it played none.
func (s Standing) MeanCycles() float64 {
	if s.Played == 0 {
		return 0
	}
	return math.Round(float64(s.Cycles)/float64(s.Played)*100) / 100
}

// Score totals the results per bot and returns the standings, best first: more
// points, then more wins, then the name in byte order. The totals are sums over the
// results, so they do not depend on the order of results.
func Score(bots []Bot, results []MatchResult) []Standing {
	st := make([]Standing, len(bots))
	for i, b := range bots {
		st[i].Name = b.Name
	}
	for _, r := range results {
		for _, i := range [2]int{r.First, r.Second} {
			s := &st[i]
			s.Played++
			s.Cycles += int64(r.Cycles)
			switch {
			case r.Winner < 0:
				s.Ties++
				s.Points += TiePoints
			case r.Winner == i:
				s.Wins++
				s.Points += WinPoints
			default:
				s.Losses++
				s.Points += LossPoints
			}
		}
	}
	slices.SortFunc(st, func(a, b Standing) int {
		if a.Points != b.Points {
			return b.Points - a.Points
		}
		if a.Wins != b.Wins {
			return b.Wins - a.Wins
		}
		return strings.Compare(a.Name, b.Name)
	})
	return st
}

// FormatText renders the standings as a table: a header line and one line per bot,
// with the bot name left-aligned and the numbers right-aligned.
func FormatText(st []Standing) []byte {
	head := []string{"rank", "bot", "points", "played", "wins", "ties", "losses", "mean_cycles"}
	rows := [][]string{head}
	for i, s := range st {
		rows = append(rows, []string{
			strconv.Itoa(i + 1), s.Name, strconv.Itoa(s.Points), strconv.Itoa(s.Played),
			strconv.Itoa(s.Wins), strconv.Itoa(s.Ties), strconv.Itoa(s.Losses),
			strconv.FormatFloat(s.MeanCycles(), 'f', 2, 64),
		})
	}
	widths := make([]int, len(head))
	for _, row := range rows {
		for c, cell := range row {
			widths[c] = max(widths[c], len(cell))
		}
	}
	var b bytes.Buffer
	for _, row := range rows {
		for c, cell := range row {
			if c > 0 {
				b.WriteString("  ")
			}
			if c == 1 { // the bot name
				cell += strings.Repeat(" ", widths[c]-len(cell))
			} else {
				cell = strings.Repeat(" ", widths[c]-len(cell)) + cell
			}
			b.WriteString(cell)
		}
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// standingRecord is one line of the JSON scoreboard; the field order is fixed.
type standingRecord struct {
	Rank       int     `json:"rank"`
	Bot        string  `json:"bot"`
	Points     int     `json:"points"`
	Played     int     `json:"played"`
	Wins       int     `json:"wins"`
	Ties       int     `json:"ties"`
	Losses     int     `json:"losses"`
	Cycles     int64   `json:"cycles"`
	MeanCycles float64 `json:"mean_cycles"`
}

// FormatJSON renders the standings as JSON lines, one object per bot, best first.
func FormatJSON(st []Standing) ([]byte, error) {
	recs := make([]any, len(st))
	for i, s := range st {
		recs[i] = standingRecord{i + 1, s.Name, s.Points, s.Played, s.Wins, s.Ties, s.Losses, s.Cycles, s.MeanCycles()}
	}
	return jsonLines(recs)
}

// matchRecord is one line of the match log. Seed is a decimal string, as elsewhere in
// the JSON output; Winner is null for a tie.
type matchRecord struct {
	Index   int     `json:"index"`
	Round   int     `json:"round"`
	Seed    string  `json:"seed"`
	First   string  `json:"first"`
	Second  string  `json:"second"`
	Outcome string  `json:"outcome"`
	Winner  *string `json:"winner"`
	Cycles  int     `json:"cycles"`
}

// FormatMatches renders one JSON line per match, in match index order whatever the
// order of results.
func FormatMatches(bots []Bot, results []MatchResult) ([]byte, error) {
	sorted := slices.Clone(results)
	slices.SortFunc(sorted, func(a, b MatchResult) int { return a.Index - b.Index })
	recs := make([]any, len(sorted))
	for i, r := range sorted {
		rec := matchRecord{
			Index: r.Index, Round: r.Round, Seed: strconv.FormatUint(r.Seed, 10),
			First: bots[r.First].Name, Second: bots[r.Second].Name, Outcome: "tie", Cycles: r.Cycles,
		}
		if r.Winner >= 0 {
			rec.Outcome, rec.Winner = "win", &bots[r.Winner].Name
		}
		recs[i] = rec
	}
	return jsonLines(recs)
}

// jsonLines marshals each value onto its own line.
func jsonLines(recs []any) ([]byte, error) {
	var b bytes.Buffer
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			return nil, fmt.Errorf("tourney: %w", err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}
