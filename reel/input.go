// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"fmt"
	"image"
	"strings"
	"time"

	"getsteep.dev/steep/reel/vt"
)

// InputOption configures one call to [Session.Type], [Session.Key],
// [Session.Click], [Session.Drag], or [Session.Wheel]. [NewInputConfig]
// reads what a call's options set.
type InputOption func(*InputConfig)

// InputConfig is what a call's [InputOption] values set, as
// [NewInputConfig] reads them, such as for a caller that records how many
// times its input is sent.
type InputConfig struct {
	// Delay is the pause after each key press, click, drag step, or wheel
	// turn, when DelaySet. Zero or less is no pause.
	Delay time.Duration
	// DelaySet is whether an option set Delay. Without one, a call pauses
	// for its default: the [WithTypingDelay] delay for keys, and none for
	// the mouse.
	DelaySet bool
	// Repeat is how many times the call sends its input, at least 1.
	Repeat int
}

// NewInputConfig returns what opts set, applied in order to no delay and one
// repeat, so the last of each wins. A Repeat of less than 1 is 1, as a call
// sends its input at least once.
func NewInputConfig(opts ...InputOption) InputConfig {
	cfg := InputConfig{Repeat: 1}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	cfg.Repeat = max(cfg.Repeat, 1)
	return cfg
}

// Delay pauses for d after each key press, click, drag step, or wheel turn of
// the call, instead of the [WithTypingDelay] delay (for keys) or none (for
// the mouse). Zero or less sends the input with no pause. The pause is a
// [time.Sleep].
func Delay(d time.Duration) InputOption {
	return func(cfg *InputConfig) { cfg.Delay, cfg.DelaySet = d, true }
}

// Repeat sends the call's input n times, such as a key pressed 5 times, a
// double click, or 10 turns of the wheel, each followed by the call's delay.
// It does not apply to [Session.Drag]. Less than 1 sends it once.
func Repeat(n int) InputOption {
	return func(cfg *InputConfig) { cfg.Repeat = n }
}

// newInputConfig returns what opts set, with a delay of d when none of them
// sets one.
func newInputConfig(d time.Duration, opts []InputOption) InputConfig {
	cfg := NewInputConfig(opts...)
	if !cfg.DelaySet {
		cfg.Delay = d
	}
	return cfg
}

// pause sleeps for the call's delay, if any.
func (cfg InputConfig) pause() {
	if cfg.Delay > 0 {
		time.Sleep(cfg.Delay)
	}
}

// send sends the program encode's bytes and appends them as an [Input]
// event. Nothing happens when encode returns no bytes. With snap, a
// scrolled viewport first goes back to the bottom (unless
// [WithScrollOnInput] is off), appending a [Scroll] event.
func (s *Session) send(encode func() ([]byte, error), snap bool) error {
	return s.change(func() (Event, error) {
		data, err := encode()
		if err != nil || len(data) == 0 {
			return nil, err
		}
		if snap && s.scrollOnInput && s.term.ScrollbackOffset() != 0 {
			err = s.term.ScrollToBottom()
			if err != nil {
				return nil, err
			}
			s.timeline.append(Scroll{})
		}
		return Input{Data: data}, s.term.SendInput(data)
	})
}

// Type sends text as one key press per character. Characters are pressed as
// on a US layout, with shift for upper case and shifted symbols; others,
// including control characters such as "\n", are sent as is. For keys with
// modifiers, use [Session.Key]. A scrolled viewport goes back to the bottom
// first (see [WithScrollOnInput]). [Repeat] types text that many times.
//
// Without a delay ([WithTypingDelay], [Delay]), the text is one [Input]
// event, sent only if every key encodes. With one, each key press is its own
// [Input] event followed by the pause, and the keys before one that fails to
// encode have been sent.
func (s *Session) Type(text string, opts ...InputOption) error {
	cfg := newInputConfig(s.typingDelay, opts)
	text = strings.Repeat(text, cfg.Repeat)
	if cfg.Delay <= 0 {
		return s.send(func() ([]byte, error) { return s.encodeText(text) }, true)
	}
	for _, r := range text {
		if err := s.send(func() ([]byte, error) { return s.encodeText(string(r)) }, true); err != nil {
			return err
		}
		cfg.pause()
	}
	return nil
}

// encodeText encodes each character of text as the key press that types it.
func (s *Session) encodeText(text string) ([]byte, error) {
	var data []byte
	for _, r := range text {
		b, err := s.term.EncodeKey(vt.RuneKey(r))
		if err != nil {
			return nil, err
		}
		data = append(data, b...)
	}
	return data, nil
}

// Key sends one key press, named as [vt.ParseKey] accepts, such as "enter",
// "ctrl+c", "shift+tab", "f5", or "a". An unknown name is an error.
// [Repeat] presses it that many times, each its own [Input] event.
//
// The key is encoded per the program's keyboard mode, such as the kitty
// keyboard protocol. A combination the mode cannot encode (such as
// ctrl+shift+a without kitty) is an error and sends nothing. A scrolled
// viewport goes back to the bottom first (see [WithScrollOnInput]). Key
// pauses after each press for the [WithTypingDelay] or [Delay] delay.
func (s *Session) Key(name string, opts ...InputOption) error {
	key, err := vt.ParseKey(name)
	if err != nil {
		return err
	}
	cfg := newInputConfig(s.typingDelay, opts)
	for range cfg.Repeat {
		err = s.send(func() ([]byte, error) {
			data, eerr := s.term.EncodeKey(key)
			if eerr == nil && len(data) == 0 {
				eerr = fmt.Errorf("reel: key %q has no encoding in the program's keyboard mode", name)
			}
			return data, eerr
		}, true)
		if err != nil {
			return err
		}
		cfg.pause()
	}
	return nil
}

// Paste sends text as a paste, bracketed when the program has enabled
// bracketed paste mode. A scrolled viewport goes back to the bottom first (see
// [WithScrollOnInput]).
func (s *Session) Paste(text string) error {
	return s.send(func() ([]byte, error) { return s.term.EncodePaste(text) }, true)
}

// Mouse sends a mouse event at zero-based cell (x, y). Nothing is sent
// unless the program enabled a mouse tracking mode that reports the event.
// Unlike keys, it leaves a scrolled viewport where it is.
func (s *Session) Mouse(action vt.MouseAction, button vt.MouseButton, x, y int) error {
	return s.send(func() ([]byte, error) {
		return s.term.EncodeMouse(vt.MouseEvent{Action: action, Button: button, X: x, Y: y})
	}, false)
}

// Click sends a press and a release of button at zero-based cell (x, y),
// as [Session.Mouse] does, then pauses for the call's [Delay]. [Repeat]
// clicks that many times, such as 2 for a double click.
func (s *Session) Click(button vt.MouseButton, x, y int, opts ...InputOption) error {
	cfg := newInputConfig(0, opts)
	for range cfg.Repeat {
		if err := s.Mouse(vt.MousePress, button, x, y); err != nil {
			return err
		}
		if err := s.Mouse(vt.MouseRelease, button, x, y); err != nil {
			return err
		}
		cfg.pause()
	}
	return nil
}

// Drag presses button at cell from, moves the pointer one cell at a time
// along the straight line to to, and releases it there, as [Session.Mouse]
// does. Each step of the motion is its own [Input] event, followed by the
// call's [Delay]. Cells are not square, so the line is straight in cells,
// not on screen.
func (s *Session) Drag(button vt.MouseButton, from, to image.Point, opts ...InputOption) error {
	cfg := newInputConfig(0, opts)
	if err := s.Mouse(vt.MousePress, button, from.X, from.Y); err != nil {
		return err
	}
	for _, p := range DragPath(from, to) {
		if err := s.Mouse(vt.MouseMotion, button, p.X, p.Y); err != nil {
			return err
		}
		cfg.pause()
	}
	return s.Mouse(vt.MouseRelease, button, to.X, to.Y)
}

// Wheel turns the mouse wheel one notch at zero-based cell (x, y), as a
// press of button, as [Session.Mouse] does. button must be
// [vt.MouseWheelUp], [vt.MouseWheelDown], [vt.MouseWheelLeft], or
// [vt.MouseWheelRight], or Wheel is an error and sends nothing. [Repeat]
// turns it that many notches, each its own [Input] event followed by the
// call's [Delay].
//
// Wheel scrolls whatever the program scrolls, and leaves the terminal's own
// viewport where it is. To scroll that, see [Session.ScrollUp].
func (s *Session) Wheel(button vt.MouseButton, x, y int, opts ...InputOption) error {
	// The wheel's buttons are 4 through 7 (see [vt.MouseButton]).
	if button < vt.MouseWheelUp || button > vt.MouseWheelRight {
		return fmt.Errorf("reel: mouse button %d is not a wheel button", button)
	}
	cfg := newInputConfig(0, opts)
	for range cfg.Repeat {
		if err := s.Mouse(vt.MousePress, button, x, y); err != nil {
			return err
		}
		cfg.pause()
	}
	return nil
}

// DragPath returns the cells [Session.Drag] moves the pointer through from
// from to to: the straight line between them in cells, after from and
// ending at to, by Bresenham's algorithm, so each step moves to one of the 8
// neighboring cells. It is empty when from is to.
func DragPath(from, to image.Point) []image.Point {
	dx, dy := abs(to.X-from.X), -abs(to.Y-from.Y)
	sx, sy := cmp.Compare(to.X, from.X), cmp.Compare(to.Y, from.Y)
	path := make([]image.Point, 0, max(dx, -dy))
	e := dx + dy
	for p := from; p != to; {
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			p.X += sx
		}
		if e2 <= dx {
			e += dx
			p.Y += sy
		}
		path = append(path, p)
	}
	return path
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
