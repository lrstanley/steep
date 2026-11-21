// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"errors"

	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/effects"
	"getsteep.dev/steep/reel/region"
)

// Caption shows text in a caption from here in the recording: a pill at the
// top or bottom of the terminal (see [effects.NewCaption]), for whoever
// watches the recording, such as to say what a demo does next. It shows
// until the next caption, [Harness.EndCaption], or the end of the
// recording, unless an option ends it sooner, such as [effects.CaptionFor].
// A callout does not end it.
//
// Exports draw captions with [effects.Captions], which the harness adds to
// a [WithRecording] profile of a test that placed any and has none of its
// own. It pauses only where a caption says to ([effects.CaptionPause]); for
// a demo that gives its viewers time to read each, add
// effects.Captions(effects.DefaultCaptionPause(effects.AutoPause)) to the
// profile.
//
// A caption starts where it is placed: after an action, such as
// [Harness.Press], once the program has painted its response (see
// [Harness.Mark]). Text that is empty once read as plain text, or options
// that conflict, are a test error.
func (h *Harness) Caption(text string, opts ...effects.CaptionOption) *Harness {
	h.tb.Helper()
	h.placeCaption(nil, text, opts)
	return h
}

// Callout shows text in a callout from here in the recording: a box with a
// caret pointing at the first cells anchor matches as it starts, such as
// [region.Text] of a button's label (see [effects.NewCallout]). It is
// otherwise a caption (see [Harness.Caption]), but for its own sort: it
// shows until the next callout or [Harness.EndCallout], and a caption does
// not end it. With [effects.Spotlight], it darkens all but its anchor while
// it shows.
//
// The anchor must be a region of package region, such as [region.Text]; a
// region of a caller's own is a test error.
func (h *Harness) Callout(anchor region.Region, text string, opts ...effects.CaptionOption) *Harness {
	h.tb.Helper()
	if anchor == nil {
		h.tb.Errorf("steep: callout: nil anchor")
		return h
	}
	h.placeCaption(anchor, text, opts)
	return h
}

// EndCaption ends the caption that shows, from here in the recording. It
// does nothing when none shows.
func (h *Harness) EndCaption() *Harness {
	h.tb.Helper()
	h.annotateCaption("end caption", effects.EndCaption())
	return h
}

// EndCallout ends the callout that shows, from here in the recording. It
// does nothing when none shows.
func (h *Harness) EndCallout() *Harness {
	h.tb.Helper()
	h.annotateCaption("end callout", effects.EndCallout())
	return h
}

// placeCaption places a caption, or a callout with an anchor.
func (h *Harness) placeCaption(anchor region.Region, text string, opts []effects.CaptionOption) {
	h.tb.Helper()
	sort := "caption"
	var a reel.Annotation
	var err error
	if anchor == nil {
		a, err = effects.NewCaption(text, opts...)
	} else {
		sort = "callout"
		a, err = effects.NewCallout(anchor, text, opts...)
	}
	if err != nil {
		h.tb.Errorf("steep: %v", err)
		return
	}
	h.annotateCaption(sort, a)
}

// annotateCaption places a, a caption's annotation, in the recording.
func (h *Harness) annotateCaption(what string, a reel.Annotation) {
	h.tb.Helper()
	if err := h.session.Annotate(a); err != nil && !errors.Is(err, reel.ErrClosed) {
		h.tb.Errorf("steep: %s: %v", what, err)
	}
}

// hasCaptions reports whether tl holds a caption or a callout.
func hasCaptions(tl *reel.Timeline) bool {
	for a := range tl.Annotations() {
		if a.Kind == effects.CaptionKind {
			return true
		}
	}
	return false
}
