// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package play

import (
	"context"
	"errors"
	"fmt"
	"image"
	"regexp"
	"time"

	syntax "getsteep.dev/steep/cmd/reel/internal/tape"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/vt"
)

// target is the cell a mouse command points at: a cell of the screen, or
// where the first match of a condition starts, moved by an offset.
type target struct {
	// cell is the cell, or the offset from the match of find.
	cell image.Point
	// find, if set, is what to find on the screen, waiting for it for up
	// to timeout.
	find    reel.Condition
	timeout time.Duration
}

// resolve returns the cell t points at, waiting for what t finds to be on
// the screen. A cell off the screen is an error, as the program would see
// the event at the screen's edge, if at all. what names the event, such as
// "Click".
func (t target) resolve(ctx context.Context, s *reel.Session, what string) (image.Point, error) {
	p := t.cell
	if t.find != nil {
		wctx, cancel := context.WithTimeout(ctx, t.timeout)
		defer cancel()
		res, err := s.Wait(wctx, t.find)
		var werr *reel.WaitError
		if errors.As(err, &werr) && ctx.Err() == nil && errors.Is(werr.Err, context.DeadlineExceeded) {
			return p, fmt.Errorf("%s: %s is not on the screen after %v; the screen was:\n%s\n%w", what, t.find, t.timeout, TrimScreen(werr.Last), err)
		}
		if err != nil {
			return p, err
		}
		p = p.Add(image.Pt(res.Matches[0].Col, res.Matches[0].Row))
	}
	return p, OnScreen(s, what, p)
}

// OnScreen returns an error for the first of cells off s's screen, where
// the program would see a mouse event at the screen's edge, if at all. what
// names the event, such as "Click".
func OnScreen(s *reel.Session, what string, cells ...image.Point) error {
	cols, rows := s.Terminal().Size()
	for _, p := range cells {
		if !p.In(image.Rect(0, 0, cols, rows)) {
			return fmt.Errorf("%s at (%d, %d) is off the %dx%d screen", what, p.X, p.Y, cols, rows)
		}
	}
	return nil
}

// trimRows is a condition checked against the screen without the spaces at
// the end of each row, as Wait matches it, so /^Save$/ matches a row that
// holds only Save. Columns are counted from the start of each row, so no
// match moves.
type trimRows struct{ reel.Condition }

func (c trimRows) Check(screen reel.ScreenText) ([]reel.Match, bool) {
	screen.Text = trimLines(screen.Text)
	return c.Condition.Check(screen)
}

// clickButtons are the buttons Click and Drag take as +modifiers, and
// wheelButtons the directions Wheel does.
//
//nolint:goconst // Modifier names, as the key commands' are written.
var (
	clickButtons = map[string]vt.MouseButton{"Left": vt.MouseLeft, "Middle": vt.MouseMiddle, "Right": vt.MouseRight}
	wheelButtons = map[string]vt.MouseButton{
		"Up": vt.MouseWheelUp, "Down": vt.MouseWheelDown, "Left": vt.MouseWheelLeft, "Right": vt.MouseWheelRight,
	}
)

// button reads the command's one +modifier as a button of buttons, or
// returns def without one. A command whose def is [vt.MouseUnknown] must
// have one.
func (c *compiler) button(buttons map[string]vt.MouseButton, def vt.MouseButton, names string) (vt.MouseButton, error) {
	name := c.cmd.Name.Text
	switch {
	case len(c.cmd.Mods) > 1:
		return 0, c.errorf(c.cmd.Mods[1].Pos, "%s takes one of %s", name, names)
	case len(c.cmd.Mods) == 0 && def == vt.MouseUnknown:
		return 0, fmt.Errorf("%s expects one of %s", name, names)
	case len(c.cmd.Mods) == 0:
		return def, nil
	}
	mod := c.cmd.Mods[0]
	if b, ok := buttons[mod.Text]; ok && mod.Kind == syntax.Word {
		return b, nil
	}
	return 0, c.errorf(mod.Pos, "%s+ expects %s, not %q", name, names, mod.Text)
}

// target reads a TARGET: a cell as X Y, from 0, or a string or /regexp/ to
// find on the screen, with an optional offset from where its first match
// starts of a signed number of columns and then of rows, such as +6 or
// +1 -1.
func (c *compiler) target() (target, error) {
	name := c.cmd.Name
	t, ok := c.take()
	switch {
	case !ok:
		return target{}, c.errorf(name.Pos, "%s expects a cell X Y, a string, or a /regexp/", name.Text)
	case t.Kind == syntax.Number:
		x, err := t.Int()
		if err != nil {
			return target{}, c.errorf(t.Pos, "%s: %w", name.Text, err)
		}
		var y int
		err = c.intArg(syntax.Token{Text: name.Text + "'s row", Pos: t.Pos}, 0, &y)
		return target{cell: image.Pt(x, y)}, err
	}
	found := target{timeout: c.tape.settings.waitTimeout}
	switch {
	case t.Kind == syntax.Regex:
		re, err := regexp.Compile(t.Text)
		if err != nil {
			return target{}, c.errorf(t.Pos, "%s: %w", name.Text, err)
		}
		found.find = trimRows{reel.Matches(re)}
	case t.IsString() && t.Text != "":
		found.find = trimRows{reel.Contains(t.Text)}
	default:
		return target{}, c.errorf(t.Pos, "%s: %q is not a cell X Y, a string, or a /regexp/", name.Text, t.Text)
	}
	for i := range 2 {
		n, signed, err := c.signed()
		if err != nil || !signed {
			return found, err
		}
		if i == 0 {
			found.cell.X = n
		} else {
			found.cell.Y = n
		}
	}
	return found, nil
}

// signed reads a number with a sign before it, such as +6 or -1, if the
// next argument is a sign, which ok reports.
func (c *compiler) signed() (n int, ok bool, err error) {
	sign := c.peek()
	if sign.Kind != syntax.Punct || sign.Text != "+" && sign.Text != "-" {
		return 0, false, nil
	}
	c.take()
	t, more := c.take()
	if !more || t.Kind != syntax.Number {
		return 0, true, c.errorf(sign.Pos, "%s: %s expects a whole number after it", c.cmd.Name.Text, sign.Text)
	}
	n, err = t.Int()
	if err != nil {
		return 0, true, c.errorf(t.Pos, "%s: %w", c.cmd.Name.Text, err)
	}
	if sign.Text == "-" {
		n = -n
	}
	return n, true, nil
}

// click compiles Click[+Left|+Middle|+Right][@<time>] TARGET [count].
func (c *compiler) click(cmd syntax.Command) error {
	button, err := c.button(clickButtons, vt.MouseLeft, "Left, Middle, or Right")
	if err != nil {
		return err
	}
	delay, err := c.speed(0)
	if err != nil {
		return err
	}
	at, err := c.target()
	if err != nil {
		return err
	}
	n, err := c.count()
	if err != nil {
		return err
	}
	c.add(cmd.Name.Pos, clickStep{button: button, at: at, delay: delay, repeat: n})
	return nil
}

// hover compiles Hover TARGET.
func (c *compiler) hover(cmd syntax.Command) error {
	at, err := c.target()
	if err != nil {
		return err
	}
	c.add(cmd.Name.Pos, hoverStep{at: at})
	return nil
}

// drag compiles Drag[+Left|+Middle|+Right][@<time>] TARGET TARGET.
func (c *compiler) drag(cmd syntax.Command) error {
	button, err := c.button(clickButtons, vt.MouseLeft, "Left, Middle, or Right")
	if err != nil {
		return err
	}
	delay, err := c.speed(0)
	if err != nil {
		return err
	}
	from, err := c.target()
	if err != nil {
		return err
	}
	to, err := c.target()
	if err != nil {
		return err
	}
	c.add(cmd.Name.Pos, dragStep{button: button, from: from, to: to, delay: delay})
	return nil
}

// wheel compiles Wheel+Up|+Down|+Left|+Right[@<time>] TARGET [count].
func (c *compiler) wheel(cmd syntax.Command) error {
	button, err := c.button(wheelButtons, vt.MouseUnknown, "Up, Down, Left, or Right")
	if err != nil {
		return err
	}
	delay, err := c.speed(0)
	if err != nil {
		return err
	}
	at, err := c.target()
	if err != nil {
		return err
	}
	n, err := c.count()
	if err != nil {
		return err
	}
	c.add(cmd.Name.Pos, wheelStep{button: button, at: at, delay: delay, repeat: n})
	return nil
}

// clickStep clicks a target, repeat times.
type clickStep struct {
	button vt.MouseButton
	at     target
	delay  time.Duration
	repeat int
}

func (c clickStep) run(ctx context.Context, r *runner) error {
	p, err := c.at.resolve(ctx, r.s, "Click")
	if err != nil {
		return err
	}
	return repeat(ctx, c.repeat, c.delay, func() error { return r.s.Click(c.button, p.X, p.Y) })
}

// hoverStep moves the pointer to a target with no button held.
type hoverStep struct{ at target }

func (c hoverStep) run(ctx context.Context, r *runner) error {
	p, err := c.at.resolve(ctx, r.s, "Hover")
	if err != nil {
		return err
	}
	return r.s.Mouse(vt.MouseMotion, vt.MouseUnknown, p.X, p.Y)
}

// dragStep drags from one target to another, both found before the press.
type dragStep struct {
	button   vt.MouseButton
	from, to target
	delay    time.Duration
}

func (c dragStep) run(ctx context.Context, r *runner) error {
	from, err := c.from.resolve(ctx, r.s, "Drag")
	if err != nil {
		return err
	}
	to, err := c.to.resolve(ctx, r.s, "Drag")
	if err != nil {
		return err
	}
	return Drag(ctx, r.s, c.button, from, to, c.delay)
}

// Drag presses button at from, moves the pointer one cell at a time to to,
// as [reel.Session.Drag] does, pausing for delay after each step, and
// releases it there. When ctx ends first, it releases the button where the
// pointer got to, so the program does not see it held, and returns ctx's
// cause.
func Drag(ctx context.Context, s *reel.Session, button vt.MouseButton, from, to image.Point, delay time.Duration) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if err := s.Mouse(vt.MousePress, button, from.X, from.Y); err != nil {
		return err
	}
	at := from
	for _, p := range reel.DragPath(from, to) {
		err := context.Cause(ctx)
		if err == nil {
			err = s.Mouse(vt.MouseMotion, button, p.X, p.Y)
		}
		if err == nil {
			at = p
			err = sleep(ctx, delay)
		}
		if err != nil {
			return errors.Join(err, s.Mouse(vt.MouseRelease, button, at.X, at.Y))
		}
	}
	return s.Mouse(vt.MouseRelease, button, to.X, to.Y)
}

// wheelStep turns the wheel at a target, repeat notches.
type wheelStep struct {
	button vt.MouseButton
	at     target
	delay  time.Duration
	repeat int
}

func (c wheelStep) run(ctx context.Context, r *runner) error {
	p, err := c.at.resolve(ctx, r.s, "Wheel")
	if err != nil {
		return err
	}
	return Wheel(ctx, r.s, c.button, p.X, p.Y, c.repeat, c.delay)
}

// Wheel turns the mouse wheel toward button n notches at (x, y), pausing
// for delay after each, until ctx is done.
func Wheel(ctx context.Context, s *reel.Session, button vt.MouseButton, x, y, n int, delay time.Duration) error {
	return repeat(ctx, n, delay, func() error { return s.Wheel(button, x, y) })
}
