// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"context"
	"errors"
	"image"
	"slices"
	"time"

	"getsteep.dev/steep/reel/render"
	"getsteep.dev/steep/reel/vt"
)

// Player steps through a timeline's events in a terminal, replaying them as
// [Export] does, so a viewer can look at the terminal after any event.
// Seeking forward applies the events in between; seeking back replays the
// timeline from its first event, or from a checkpoint [Player.Prepare]
// made. A Player is not safe for concurrent use. Close it when done.
type Player struct {
	ctx    context.Context
	header Header
	events []Event
	opts   []render.Option

	r    *replay
	rend *render.Renderer
	// img is drawn after drawn events, until SetOptions clears it.
	img   *image.NRGBA
	drawn int

	// checkpoints are the snapshots Prepare took, which Seek restores.
	// build replays toward the next missing one. skipped are the events
	// the terminal could not be snapshotted after.
	checkpoints []checkpoint
	build       *replay
	skipped     map[int]bool
}

// NewPlayer returns a Player of all of tl's events, before the first one is
// applied. Its images are drawn with a renderer of opts, after the palette
// of tl's header, as [Export] draws them.
func NewPlayer(ctx context.Context, tl *Timeline, opts ...render.Option) (*Player, error) {
	p := &Player{ctx: ctx, header: tl.header, events: tl.snapshot(), opts: renderOptions(tl.header, opts)}
	if err := p.rewind(); err != nil {
		return nil, err
	}
	return p, nil
}

// rewind replaces the terminal with a fresh one, closed first so the pool
// can reuse it. The player holds its terminal for as long as its caller
// chooses, so it opens it from the unlimited pool.
func (p *Player) rewind() error {
	if p.r != nil {
		_ = p.r.close()
		p.r = nil
	}
	r, err := newReplay(p.ctx, recordRuntime, p.header, 0, 0)
	if err != nil {
		return err
	}
	p.r = r
	return nil
}

// Len returns the number of events in the timeline.
func (p *Player) Len() int { return len(p.events) }

// Pos returns the number of events applied.
func (p *Player) Pos() int {
	if p.r == nil {
		return 0
	}
	return p.r.n
}

// Seek applies the timeline's first n events, clamped to [0, Len]. Seeking
// forward applies the events in between. Seeking back starts from the
// checkpoint nearest before n that [Player.Prepare] took, or the start. If it
// fails, the other methods must not be called until a Seek succeeds.
func (p *Player) Seek(n int) error {
	n = min(max(n, 0), len(p.events))
	if p.r == nil || n < p.r.n {
		// Seeking rewinds to the start when restoring a checkpoint fails.
		if cp := p.checkpointAt(n); p.r == nil || cp == nil || p.r.restore(cp) != nil {
			if err := p.rewind(); err != nil {
				return err
			}
		}
	}
	for p.r.n < n {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		if err := p.r.apply(p.events[p.r.n]); err != nil {
			return err
		}
	}
	return nil
}

// checkpointAt returns the checkpoint furthest along without passing n, or
// nil if none is.
func (p *Player) checkpointAt(n int) *checkpoint {
	var best *checkpoint
	for i := range p.checkpoints {
		if cp := &p.checkpoints[i]; cp.n <= n && (best == nil || cp.n > best.n) {
			best = cp
		}
	}
	return best
}

// maxCheckpointBytes is the most memory the snapshots [Player.Prepare]
// keeps may take. A Kitty image on screen is in every snapshot taken while
// it shows, so a few large ones would otherwise multiply.
const maxCheckpointBytes = 256 << 20

// checkpoint is a replay after its first n events, as [Player.Prepare] took
// it.
type checkpoint struct {
	n       int
	now     time.Duration
	focused bool
	snap    *vt.Snapshot
}

// Prepare works for up to budget toward taking limit checkpoints at evenly
// spaced events of the timeline, so a Seek replays from the nearest one at
// or before it rather than from the start. Each is a snapshot of the
// terminal (see [vt.Terminal.Snapshot]), a small part of the memory a
// terminal holds, and Seek can use it any number of times. It reports
// whether there is more to do, so it can be called again when the caller is
// idle. A point the terminal cannot be snapshotted at, such as on the
// alternate screen with prompt marks kept for the primary one, gets no
// checkpoint, and neither do those during synchronized output or past the
// memory the checkpoints may take together. A limit of zero drops them all.
func (p *Player) Prepare(limit int, budget time.Duration) (more bool, err error) {
	targets := checkpoints(len(p.events), limit)
	// Drop checkpoints anywhere the targets no longer are. That frees room,
	// so the targets skipped for want of it are tried again.
	kept := slices.DeleteFunc(p.checkpoints, func(cp checkpoint) bool {
		return !slices.Contains(targets, cp.n)
	})
	if len(kept) < len(p.checkpoints) {
		p.skipped = nil
	}
	p.checkpoints = kept
	deadline := time.Now().Add(budget)
	for _, target := range targets {
		if p.skipped[target] || slices.ContainsFunc(p.checkpoints, func(cp checkpoint) bool { return cp.n == target }) {
			continue
		}
		if p.build != nil && p.build.n > target {
			_ = p.build.close()
			p.build = nil
		}
		// Opening a builder can restore a checkpoint, which costs about
		// as much as taking one.
		if time.Now().After(deadline) {
			return true, nil
		}
		if p.build == nil {
			if p.build, err = newReplay(p.ctx, recordRuntime, p.header, 0, 0, vt.WithSnapshots()); err != nil {
				return false, err
			}
			// Start from the nearest checkpoint before the target.
			if cp := p.checkpointAt(target); cp != nil {
				if err = p.build.restore(cp); err != nil {
					_ = p.build.close()
					p.build = nil
					return false, err
				}
			}
		}
		for p.build.n < target {
			if time.Now().After(deadline) {
				return true, nil
			}
			if err = p.build.apply(p.events[p.build.n]); err != nil {
				_ = p.build.close()
				p.build = nil
				return false, err
			}
		}
		// A snapshot copies the whole terminal, so it waits for the next
		// call once the budget is spent.
		if time.Now().After(deadline) {
			return true, nil
		}
		if err = p.takeCheckpoint(); err != nil {
			_ = p.build.close()
			p.build = nil
			return false, err
		}
	}
	if p.build != nil {
		_ = p.build.close()
		p.build = nil
	}
	return false, nil
}

// takeCheckpoint snapshots the builder where it is, or marks that event
// skipped when it cannot or should not be snapshotted there.
func (p *Player) takeCheckpoint() error {
	n := p.build.n
	// A snapshot during synchronized output keeps the frozen frame, with
	// images the byte count does not see, so a hold gets none.
	if p.build.term.Held() {
		p.skip(n)
		return nil
	}
	snap, err := p.build.term.Snapshot()
	switch {
	case errors.Is(err, vt.ErrSnapshot) || err == nil && p.checkpointBytes()+snap.Len() > maxCheckpointBytes:
		p.skip(n)
	case err != nil:
		return err
	default:
		p.checkpoints = append(p.checkpoints, checkpoint{n: n, now: p.build.now, focused: p.build.focused, snap: snap})
	}
	return nil
}

// skip marks event count n as one Prepare takes no checkpoint at.
func (p *Player) skip(n int) {
	if p.skipped == nil {
		p.skipped = map[int]bool{}
	}
	p.skipped[n] = true
}

// checkpointBytes is the size of the checkpoints' snapshots.
func (p *Player) checkpointBytes() int {
	n := 0
	for _, cp := range p.checkpoints {
		n += cp.snap.Len()
	}
	return n
}

// checkpoints returns limit event counts spaced evenly through n events,
// leaving out the start, which needs none.
func checkpoints(n, limit int) []int {
	var out []int
	for k := 1; k <= limit; k++ {
		// Targets are ordered, so only the previous one can be a duplicate.
		if c := k * n / (limit + 1); c > 0 && (len(out) == 0 || out[len(out)-1] != c) {
			out = append(out, c)
		}
	}
	return out
}

// Frame returns the terminal's screen and state after the applied events,
// at the time of the last of them.
func (p *Player) Frame() CellFrame { return p.r.current(p.r.now) }

// Screen returns the terminal's screen after the applied events, as
// [Session.Screen] does.
func (p *Player) Screen() *Screen { return screenOf(p.r.term, p.r.focused) }

func (p *Player) renderer() (*render.Renderer, error) {
	if p.rend == nil {
		rend, err := render.New(p.opts...)
		if err != nil {
			return nil, err
		}
		p.rend = rend
	}
	return p.rend, nil
}

// SetOptions replaces the options images are drawn with, such as to turn
// window chrome or the scrollbar on, from the next [Player.Image]. The
// header's palette still applies first, as for [NewPlayer].
func (p *Player) SetOptions(opts ...render.Option) error {
	if p.rend != nil {
		if err := p.rend.Close(); err != nil {
			return err
		}
		p.rend = nil
	}
	p.opts = renderOptions(p.header, opts)
	p.img = nil
	return nil
}

// Rasterizer returns the rasterizer [Player.Image] draws with, such as for
// an effect's [Context]. SetOptions replaces it and Close closes it.
func (p *Player) Rasterizer() (Rasterizer, error) {
	rend, err := p.renderer()
	if err != nil {
		return nil, err
	}
	return rend, nil
}

// Layout returns where [Player.Image] places the screen after the applied
// events in its image.
func (p *Player) Layout() (render.Layout, error) {
	rend, err := p.renderer()
	if err != nil {
		return render.Layout{}, err
	}
	cols, rows := p.r.term.Size()
	return rend.Layout(cols, rows), nil
}

// Image draws the terminal after the applied events. The image is reused
// by the next call to Image, and must not be modified; until the next Seek
// or SetOptions, it is returned again without being drawn.
func (p *Player) Image() (*image.NRGBA, error) {
	if _, err := p.renderer(); err != nil {
		return nil, err
	}
	if p.img != nil && p.drawn == p.r.n {
		return p.img, nil
	}
	f := p.Frame()
	r := p.rend.Layout(f.Screen.Width(), f.Screen.Height()).Image
	if p.img == nil || p.img.Rect != r {
		p.img = image.NewNRGBA(r)
	}
	p.rend.Draw(p.img, f.Screen, f.State, f.At)
	p.drawn = p.r.n
	return p.img, nil
}

// Close releases the player's terminals and renderer.
func (p *Player) Close() error {
	var err error
	if p.r != nil {
		err = p.r.close()
	}
	p.checkpoints = nil
	if p.build != nil {
		err = errors.Join(err, p.build.close())
		p.build = nil
	}
	if p.rend != nil {
		err = errors.Join(err, p.rend.Close())
	}
	return err
}
