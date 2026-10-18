// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import "testing"

// TestPlayer drives the state machine of the 12-cycle fixture through scripts of
// operations. After every operation the cycle and the playing flag are checked, and
// for "tick" also whether the frame changed.
func TestPlayer(t *testing.T) {
	type op struct {
		do      string // step, seek, play, pause, toggle, tick
		n       int
		cycle   int
		playing bool
		counted bool // a counted play (play N) is running
		changed bool // tick only: the cycle advanced
	}
	tests := []struct {
		name string
		ops  []op
	}{
		{"starts paused at 0", []op{{do: "pause", cycle: 0}}},
		{"step forward", []op{
			{do: "step", n: 1, cycle: 1},
			{do: "step", n: 1, cycle: 2},
			{do: "step", n: 3, cycle: 5},
		}},
		{"step back", []op{
			{do: "seek", n: 6, cycle: 6},
			{do: "step", n: -1, cycle: 5},
			{do: "step", n: -2, cycle: 3},
		}},
		{"step back before 0", []op{
			{do: "seek", n: 2, cycle: 2},
			{do: "step", n: -9, cycle: 0},
			{do: "step", n: -1, cycle: 0},
		}},
		{"step past the end", []op{
			{do: "seek", n: 10, cycle: 10},
			{do: "step", n: 100, cycle: 12},
			{do: "step", n: 1, cycle: 12},
		}},
		{"step zero stays", []op{
			{do: "seek", n: 4, cycle: 4},
			{do: "step", n: 0, cycle: 4},
		}},
		{"seek", []op{
			{do: "seek", n: 7, cycle: 7},
			{do: "seek", n: 3, cycle: 3},
			{do: "seek", n: 0, cycle: 0},
			{do: "seek", n: 12, cycle: 12},
		}},
		{"seek past the end", []op{
			{do: "seek", n: 13, cycle: 12},
			{do: "seek", n: 1 << 30, cycle: 12},
		}},
		{"seek before 0", []op{
			{do: "seek", n: 5, cycle: 5},
			{do: "seek", n: -1, cycle: 0},
			{do: "seek", n: -1 << 30, cycle: 0},
		}},
		{"play to the end", []op{
			{do: "play", cycle: 0, playing: true},
			{do: "tick", cycle: 1, playing: true, changed: true},
			{do: "seek", n: 10, cycle: 10}, // a seek pauses
			{do: "play", cycle: 10, playing: true},
			{do: "tick", cycle: 11, playing: true, changed: true},
			{do: "tick", cycle: 12, playing: false, changed: true},
			{do: "tick", cycle: 12, playing: false},
		}},
		{"play N cycles", []op{
			{do: "play", n: 2, cycle: 0, playing: true, counted: true},
			{do: "tick", cycle: 1, playing: true, counted: true, changed: true},
			{do: "tick", cycle: 2, playing: false, changed: true},
			{do: "tick", cycle: 2, playing: false},
		}},
		{"play N stops at the end", []op{
			{do: "seek", n: 11, cycle: 11},
			{do: "play", n: 5, cycle: 11, playing: true, counted: true},
			{do: "tick", cycle: 12, playing: false, changed: true},
		}},
		{"play at the end does nothing", []op{
			{do: "seek", n: 12, cycle: 12},
			{do: "play", cycle: 12},
			{do: "play", n: 3, cycle: 12},
			{do: "toggle", cycle: 12},
		}},
		{"pause", []op{
			{do: "play", cycle: 0, playing: true},
			{do: "tick", cycle: 1, playing: true, changed: true},
			{do: "pause", cycle: 1},
			{do: "tick", cycle: 1},
			{do: "pause", cycle: 1},
		}},
		{"toggle", []op{
			{do: "toggle", cycle: 0, playing: true},
			{do: "tick", cycle: 1, playing: true, changed: true},
			{do: "toggle", cycle: 1},
			{do: "toggle", cycle: 1, playing: true},
		}},
		{"play again while playing restarts the count", []op{
			{do: "play", n: 3, cycle: 0, playing: true, counted: true},
			{do: "tick", cycle: 1, playing: true, counted: true, changed: true},
			{do: "play", cycle: 1, playing: true},
			{do: "tick", cycle: 2, playing: true, changed: true},
			{do: "tick", cycle: 3, playing: true, changed: true},
		}},
		{"step pauses", []op{
			{do: "play", cycle: 0, playing: true},
			{do: "step", n: 1, cycle: 1},
			{do: "tick", cycle: 1},
		}},
		{"seek pauses", []op{
			{do: "play", n: 4, cycle: 0, playing: true, counted: true},
			{do: "seek", n: 6, cycle: 6},
			{do: "tick", cycle: 6},
		}},
		{"paused tick is a no-op", []op{
			{do: "tick", cycle: 0},
			{do: "step", n: 2, cycle: 2},
			{do: "tick", cycle: 2},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPlayer(newModel(t, loadFixture(t)))
			if p.Cycle() != 0 || p.Playing() || p.Cycles() != 12 {
				t.Fatalf("start: cycle %d playing %v cycles %d, want 0 false 12", p.Cycle(), p.Playing(), p.Cycles())
			}
			for i, o := range tt.ops {
				var changed bool
				var err error
				switch o.do {
				case "step":
					err = p.Step(o.n)
				case "seek":
					err = p.Seek(o.n)
				case "play":
					p.Play(o.n)
				case "pause":
					p.Pause()
				case "toggle":
					p.Toggle()
				case "tick":
					changed, err = p.Tick()
				}
				if err != nil {
					t.Fatalf("op %d (%s %d): %v", i, o.do, o.n, err)
				}
				if p.Cycle() != o.cycle || p.Playing() != o.playing || p.Counted() != o.counted {
					t.Errorf("op %d (%s %d): cycle %d playing %v counted %v, want %d %v %v",
						i, o.do, o.n, p.Cycle(), p.Playing(), p.Counted(), o.cycle, o.playing, o.counted)
				}
				if o.do == "tick" && changed != o.changed {
					t.Errorf("op %d (tick): changed = %v, want %v", i, changed, o.changed)
				}
				if f := p.Frame(); f.Cycle != p.Cycle() {
					t.Errorf("op %d (%s %d): Frame().Cycle = %d, Cycle() = %d", i, o.do, o.n, f.Cycle, p.Cycle())
				}
			}
		})
	}
}
