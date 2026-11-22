// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/internal/plaintext"
	"getsteep.dev/steep/reel/region"
	"github.com/charmbracelet/x/ansi"
)

// CaptionKind is the kind of the [reel.Annotation] of a caption or a callout
// ([NewCaption], [NewCallout]), which [Captions] draws.
const CaptionKind = "reel.caption"

// CaptionEndKind is the kind of the [reel.Annotation] that ends the caption
// or the callout that shows ([EndCaption], [EndCallout]).
const CaptionEndKind = "reel.caption.end"

// AutoPause is a pause for a reading time worked out from a caption's words:
// 700ms and 260ms for each word, at least 1400ms and at most 5 minutes.
// It is a pause of a caption ([CaptionPause]) or the default pause of
// [Captions] ([DefaultCaptionPause]).
const AutoPause = time.Duration(-1)

// captionMaxLines is the most lines of text a caption shows. Text past them
// is cut, ending in "…".
const captionMaxLines = 6

// Side is where a caption shows (see [CaptionSide]).
type Side int

// The sides a caption can show on.
const (
	// SideAuto is the side that covers the fewest cells with text,
	// preferring the bottom.
	SideAuto Side = iota
	SideTop
	SideBottom
	SideLeft
	SideRight
)

// sideNames are the names of the sides in a caption's annotation.
var sideNames = map[Side]string{SideTop: "top", SideBottom: "bottom", SideLeft: "left", SideRight: "right"} //nolint:exhaustive // SideAuto is left out of annotations.

func (s Side) String() string {
	if s == SideAuto {
		return "auto"
	}
	if name, ok := sideNames[s]; ok {
		return name
	}
	return fmt.Sprintf("Side(%d)", int(s))
}

// CaptionOption configures a caption or a callout ([NewCaption],
// [NewCallout]).
type CaptionOption func(*captionData)

// CaptionFor ends the caption d into the recording after it starts. Time
// its pause holds the frame does not count, so a d of zero shows it only
// while its pause lasts. A d below zero is an error.
func CaptionFor(d time.Duration) CaptionOption {
	return func(c *captionData) {
		ms := millis(d)
		if d < 0 {
			ms = -1
		}
		c.ForMS = &ms
	}
}

// CaptionUntil ends the caption at the first mark labeled label at or after
// its start.
func CaptionUntil(label string) CaptionOption {
	return func(d *captionData) { d.Until = label }
}

// CaptionPause pauses playback for d as the caption starts, holding the
// frame still while it shows, or for a reading time with [AutoPause]. Zero
// is no pause, even where [Captions] has a default ([DefaultCaptionPause]).
// Any other d below zero is an error. Without CaptionPause, the caption
// pauses as Captions' default says, which is not at all unless set.
func CaptionPause(d time.Duration) CaptionOption {
	return func(c *captionData) {
		if d == AutoPause {
			c.PauseAuto, c.PauseMS = true, nil
			return
		}
		ms := millis(d)
		if d < 0 {
			ms = -1
		}
		c.PauseAuto, c.PauseMS = false, &ms
	}
}

// CaptionSide puts a caption at the top or bottom of the terminal, or a
// callout on a side of its anchor. A callout that does not fit on that side
// takes another. A caption on the left or right is an error. The default,
// [SideAuto], is the side that covers the fewest cells with text,
// preferring the bottom.
func CaptionSide(s Side) CaptionOption {
	return func(d *captionData) {
		d.Side = ""
		if s != SideAuto {
			d.Side = s.String()
		}
	}
}

// Spotlight darkens everything but a callout's anchor while the callout
// shows, its pause included, and rings the anchor in the accent color (see
// [WithCaptionAccent]). On a caption, it is an error.
func Spotlight() CaptionOption {
	return func(d *captionData) { d.Spotlight = true }
}

// NewCaption returns the annotation of a caption: text in a pill, a rounded
// box at the top or bottom of the terminal, from where the annotation is
// placed in a timeline, such as with [reel.Session.Annotate]. [Captions]
// draws it.
//
// The text is read as plain text: a line feed starts a new line, other
// control characters and escape sequences are dropped, and long lines wrap
// at spaces. A caption shows until the first of its own end ([CaptionFor],
// [CaptionUntil]), an [EndCaption], the start of the next caption (a callout
// does not end a caption, nor a caption a callout), and the end of the
// recording.
//
// Text that is empty once read as plain text, or options that conflict,
// are an error.
func NewCaption(text string, opts ...CaptionOption) (reel.Annotation, error) {
	return newCaption(nil, text, opts)
}

// NewCallout returns the annotation of a callout: text in a box with a caret
// that points at the first cells anchor matches, in reading order, on the
// frame where the callout starts. It stays where it was placed, on the side
// of the anchor that covers the fewest cells with text (see [CaptionSide]),
// moved along that side to stay in the frame. It is otherwise a caption (see
// [NewCaption]), and shows as one where anchor matches nothing.
//
// The anchor must be one of this package's regions, which an annotation can
// describe (see the package documentation); a region of a caller's own is
// an error wrapping [errors.ErrUnsupported].
func NewCallout(anchor region.Region, text string, opts ...CaptionOption) (reel.Annotation, error) {
	if anchor == nil {
		return reel.Annotation{}, errors.New("effects: callout: nil anchor")
	}
	return newCaption(anchor, text, opts)
}

// EndCaption returns the annotation that ends the caption that shows where
// it is placed in a timeline, if one does. A callout shows on.
func EndCaption() reel.Annotation {
	return reel.Annotation{Kind: CaptionEndKind}
}

// EndCallout returns the annotation that ends the callout that shows where
// it is placed in a timeline, if one does. A caption shows on.
func EndCallout() reel.Annotation {
	return reel.Annotation{Kind: CaptionEndKind, Data: jsontext.Value(`{"callout":true}`)}
}

// captionData is the data of a caption's annotation.
type captionData struct {
	Text      string         `json:"text"`
	Anchor    jsontext.Value `json:"anchor,omitzero"`
	PauseMS   *int64         `json:"pause_ms,omitzero"`
	PauseAuto bool           `json:"pause_auto,omitzero"`
	ForMS     *int64         `json:"for_ms,omitzero"`
	Until     string         `json:"until,omitzero"`
	Side      string         `json:"side,omitzero"`
	Spotlight bool           `json:"spotlight,omitzero"`
}

type captionEndData struct {
	Callout bool `json:"callout,omitzero"`
}

func newCaption(anchor region.Region, text string, opts []CaptionOption) (reel.Annotation, error) {
	what := "caption"
	if anchor != nil {
		what = "callout"
	}
	var trunc bool
	d := captionData{Text: reel.ClipText(text, &trunc)}
	for _, o := range opts {
		if o != nil {
			o(&d)
		}
	}
	if anchor != nil {
		var err error
		if d.Anchor, err = region.Marshal(anchor); err != nil {
			return reel.Annotation{}, fmt.Errorf("effects: callout anchor: %w", err)
		}
	}
	if _, err := d.caption(); err != nil {
		return reel.Annotation{}, fmt.Errorf("effects: %w", err)
	}
	a, err := reel.NewAnnotation(CaptionKind, d)
	if err != nil {
		return reel.Annotation{}, fmt.Errorf("effects: %s: %w", what, err)
	}
	return a, nil
}

// caption is a caption as [Captions] draws it, read from its annotation.
type caption struct {
	lines  []string
	anchor region.Region // nil for a pill
	// pause is the caption's own pause, AutoPause, or -2 to take the
	// default.
	pause     time.Duration
	end       time.Duration // for its for_ms, or -1 for none
	until     string
	side      Side
	spotlight bool
}

// pauseDefault is [caption.pause] for a caption that takes [Captions]'
// default.
const pauseDefault = time.Duration(-2)

// callout reports whether c is a callout.
func (c *caption) callout() bool { return c.anchor != nil }

// caption checks d and returns the caption it describes.
func (d captionData) caption() (*caption, error) {
	what := "caption"
	if d.Anchor != nil {
		what = "callout"
	}
	c := &caption{pause: pauseDefault, end: -1, until: d.Until, spotlight: d.Spotlight}
	c.lines = captionLines(d.Text)
	if len(c.lines) == 0 {
		return nil, fmt.Errorf("%s: no text", what)
	}
	if d.Anchor != nil {
		var err error
		if c.anchor, err = region.Unmarshal(d.Anchor); err != nil {
			return nil, fmt.Errorf("callout anchor: %w", err)
		}
	}
	switch {
	case d.PauseAuto && d.PauseMS != nil:
		return nil, fmt.Errorf("%s: both pause_auto and pause_ms", what)
	case d.PauseAuto:
		c.pause = AutoPause
	case d.PauseMS != nil:
		if *d.PauseMS < 0 {
			return nil, fmt.Errorf("%s: pause %dms below zero", what, *d.PauseMS)
		}
		var err error
		if c.pause, err = fromMillis(*d.PauseMS); err != nil {
			return nil, fmt.Errorf("%s: pause: %w", what, err)
		}
	}
	if d.ForMS != nil {
		if *d.ForMS < 0 {
			return nil, fmt.Errorf("%s: for %dms below zero", what, *d.ForMS)
		}
		var err error
		if c.end, err = fromMillis(*d.ForMS); err != nil {
			return nil, fmt.Errorf("%s: for: %w", what, err)
		}
	}
	if d.Side != "" {
		for s, name := range sideNames {
			if name == d.Side {
				c.side = s
			}
		}
		if c.side == SideAuto {
			return nil, fmt.Errorf("%s: unknown side %q", what, d.Side)
		}
	}
	if !c.callout() && (c.side == SideLeft || c.side == SideRight) {
		return nil, fmt.Errorf("caption: side %v: a caption shows at the top or bottom", c.side)
	}
	if !c.callout() && c.spotlight {
		return nil, errors.New("caption: a spotlight is for a callout")
	}
	return c, nil
}

// captionLines is text read as plain text, one line per line feed, without
// escape sequences, cut to captionMaxLines. Blank lines at the start and end
// are dropped.
func captionLines(text string) []string {
	var lines []string
	for l := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		lines = append(lines, strings.TrimSpace(plaintext.Line(ansi.Strip(l), reel.AnnotationTextMax)))
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > captionMaxLines {
		lines = lines[:captionMaxLines]
		lines[captionMaxLines-1] += " …"
	}
	return lines
}

// readCaption returns the caption of a's data. Members the caption does not
// know are left alone, as a newer tool may write them.
func readCaption(a reel.Annotation) (*caption, error) {
	var d captionData
	if err := json.Unmarshal(a.Data, &d); err != nil {
		return nil, fmt.Errorf("effects: caption: %w", err)
	}
	c, err := d.caption()
	if err != nil {
		return nil, fmt.Errorf("effects: %w", err)
	}
	return c, nil
}

// readCaptionEnd reports whether a ends the callout, rather than the
// caption, that shows.
func readCaptionEnd(a reel.Annotation) (callout bool, err error) {
	var d captionEndData
	if a.Data != nil {
		if err = json.Unmarshal(a.Data, &d); err != nil {
			return false, fmt.Errorf("effects: caption end: %w", err)
		}
	}
	return d.Callout, nil
}

// maxReadingTime is the longest pause [AutoPause] gives.
const maxReadingTime = 5 * time.Minute

// readingTime is the pause [AutoPause] gives lines of text, at most
// [maxReadingTime].
func readingTime(lines []string) time.Duration {
	var words int
	for _, l := range lines {
		words += len(strings.Fields(l))
	}
	return min(maxReadingTime, max(1400*time.Millisecond, 700*time.Millisecond+time.Duration(words)*260*time.Millisecond))
}
