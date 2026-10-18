// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

// Player is the step, play, pause and seek state machine over a Model. It has no
// clock: whoever owns the clock calls Tick once per frame while Playing, so a test
// or a script can drive it deterministically.
//
// States: paused or playing. A play is either open-ended (to the last cycle) or
// counted (a number of cycles, or the last cycle if that comes first). Step and
// Seek pause. Play at the last cycle does nothing. Tick advances one cycle while
// playing and pauses at the end of the play.
type Player struct {
	m         *Model
	playing   bool
	remaining int // cycles left in a counted play; 0 when open-ended
}

// NewPlayer returns a paused player at the model's cycle.
func NewPlayer(m *Model) *Player { return &Player{m: m} }

// Cycle returns the current cycle.
func (p *Player) Cycle() int { return p.m.Cycle() }

// Cycles returns the last cycle of the replay.
func (p *Player) Cycles() int { return p.m.Cycles() }

// Playing reports whether the player is playing.
func (p *Player) Playing() bool { return p.playing }

// Counted reports whether the play in progress is counted (Play with n > 0).
func (p *Player) Counted() bool { return p.playing && p.remaining > 0 }

// Frame returns the frame at the current cycle.
func (p *Player) Frame() Frame { return p.m.Frame() }

// Step pauses and moves n cycles (back if n is negative), staying within 0 and the
// last cycle.
func (p *Player) Step(n int) error {
	p.Pause()
	return p.m.Seek(p.m.Cycle() + n)
}

// Seek pauses and moves to a cycle, clamped to 0 and the last cycle.
func (p *Player) Seek(cycle int) error {
	p.Pause()
	return p.m.Seek(cycle)
}

// Play starts playing: for n cycles if n > 0, otherwise to the last cycle. Playing
// again while playing replaces the play in progress. At the last cycle there is
// nothing to play and the player stays paused.
func (p *Player) Play(n int) {
	p.playing = p.m.Cycle() < p.m.Cycles()
	p.remaining = 0
	if p.playing && n > 0 {
		p.remaining = n
	}
}

// Pause stops playing.
func (p *Player) Pause() { p.playing = false }

// Toggle pauses if playing and plays to the end otherwise.
func (p *Player) Toggle() {
	if p.playing {
		p.Pause()
		return
	}
	p.Play(0)
}

// Tick advances one cycle if the player is playing and reports whether it did. The
// play ends, and the player pauses, when the last cycle is reached or the counted
// cycles are used up.
func (p *Player) Tick() (bool, error) {
	if !p.playing {
		return false, nil
	}
	if err := p.m.Seek(p.m.Cycle() + 1); err != nil {
		p.Pause()
		return false, err
	}
	if p.remaining > 0 {
		p.remaining--
		if p.remaining == 0 {
			p.playing = false
		}
	}
	if p.m.Cycle() >= p.m.Cycles() {
		p.Pause()
	}
	return true, nil
}
