// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"log/slog"
	"testing"
	"time"
)

// InputKind is the kind of an input action.
type InputKind string

const (
	// InputKindType is text typed as key presses ([Harness.Type]).
	InputKindType InputKind = "type"
	// InputKindKey is a key press ([Harness.Key]).
	InputKindKey InputKind = "key"
	// InputKindPaste is a paste ([Harness.Paste]).
	InputKindPaste InputKind = "paste"
	// InputKindMouse is a single mouse event ([Harness.Mouse]).
	InputKindMouse InputKind = "mouse"
	// InputKindClick is a press and release of a mouse button
	// ([Harness.MouseClick]).
	InputKindClick InputKind = "click"
	// InputKindDrag is a press of a mouse button, motion, and its release
	// ([Harness.MouseDrag]).
	InputKindDrag InputKind = "drag"
	// InputKindWheel is one or more turns of the mouse wheel
	// ([Harness.Wheel]).
	InputKindWheel InputKind = "wheel"
)

// InputMeta is the metadata of [HookInput] events.
type InputMeta struct {
	Kind InputKind `json:"action"`
	// Text is the text of [InputKindType] and [InputKindPaste].
	Text string `json:"text,omitzero"`
	// Key is the key of [InputKindKey], such as "enter" or "ctrl+c".
	Key string `json:"key,omitzero"`
	// Mouse is the mouse event of [InputKindMouse]: "press", "release", or
	// "motion".
	Mouse string `json:"mouse,omitzero"`
	// Button is the mouse button of [InputKindMouse], [InputKindClick],
	// [InputKindDrag], and [InputKindWheel]: "left", "right", "middle",
	// "wheel_up", "wheel_down", "wheel_left", "wheel_right", or "none" for
	// motion without one.
	Button string `json:"button,omitzero"`
	// Count is how many notches [InputKindWheel] turns the wheel.
	Count int `json:"count,omitzero"`
}

func (m InputMeta) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("kind", string(m.Kind))}
	for _, a := range []slog.Attr{
		slog.String("text", m.Text),
		slog.String("key", m.Key),
		slog.String("mouse", m.Mouse),
		slog.String("button", m.Button),
	} {
		if a.Value.String() != "" {
			attrs = append(attrs, a)
		}
	}
	if m.Count != 0 {
		attrs = append(attrs, slog.Int("count", m.Count))
	}
	return slog.GroupValue(attrs...)
}

// WaitKind is what a wait waits for.
type WaitKind string

const (
	// WaitKindText waits for text to be on the screen ([Harness.Wait]).
	WaitKindText WaitKind = "text"
	// WaitKindNotText waits for text not to be on the screen
	// ([Harness.WaitNot]).
	WaitKindNotText WaitKind = "not_text"
	// WaitKindCondition waits for a [reel.Condition] on the screen
	// ([Harness.WaitFor]).
	WaitKindCondition WaitKind = "condition"
	// WaitKindSettle waits for the screen to stay unchanged
	// ([Harness.WaitSettle]).
	WaitKindSettle WaitKind = "settle"
	// WaitKindFinished waits for the program to end
	// ([Harness.WaitFinished]).
	WaitKindFinished WaitKind = "finished"
	// WaitKindSleep waits for time to pass ([Harness.Sleep]).
	WaitKindSleep WaitKind = "sleep"
)

// WaitMeta is the metadata of [HookWait] events, and of the [HookClose]
// event of [Harness.WaitFinished].
type WaitMeta struct {
	Kind WaitKind `json:"wait"`
}

func (m WaitMeta) LogValue() slog.Value {
	return slog.GroupValue(slog.String("kind", string(m.Kind)))
}

// AssertKind is what an assertion asserts.
type AssertKind string

const (
	// AssertKindContains asserts that text is on the screen
	// ([Harness.Assert] and [Harness.Require]).
	AssertKindContains AssertKind = "contains"
	// AssertKindNotContains asserts that text is not on the screen
	// ([Harness.AssertNot] and [Harness.RequireNot]).
	AssertKindNotContains AssertKind = "not_contains"
	// AssertKindCondition asserts a [reel.Condition] on the screen
	// ([Harness.AssertFor] and [Harness.RequireFor]).
	AssertKindCondition AssertKind = "condition"
	// AssertKindWidth asserts the width of the screen's text
	// ([Harness.AssertWidth] and [Harness.RequireWidth]).
	AssertKindWidth AssertKind = "width"
	// AssertKindHeight asserts the height of the screen's text
	// ([Harness.AssertHeight] and [Harness.RequireHeight]).
	AssertKindHeight AssertKind = "height"
	// AssertKindDimensions asserts the width and height of the screen's
	// text ([Harness.AssertDimensions] and [Harness.RequireDimensions]).
	AssertKindDimensions AssertKind = "dimensions"
)

// AssertMeta is the metadata of [HookAssert] events.
type AssertMeta struct {
	Kind AssertKind `json:"assert"`
	// Required is true for a Require method, whose failure stops the test.
	Required bool `json:"required"`
}

func (m AssertMeta) LogValue() slog.Value {
	return slog.GroupValue(slog.String("kind", string(m.Kind)), slog.Bool("required", m.Required))
}

// CaptureKind is what a capture captures.
type CaptureKind string

const (
	// CaptureKindText compares the screen's text with a ".snap" file
	// ([Harness.AssertSnapshot] and [Harness.RequireSnapshot]).
	CaptureKindText CaptureKind = "text"
	// CaptureKindJSON compares the screen's cells and the terminal's state
	// with a ".json" file ([Harness.AssertJSON] and [Harness.RequireJSON]).
	CaptureKindJSON CaptureKind = "json"
	// CaptureKindImage renders the screen as an image ([Harness.Image] and
	// [Harness.ImageInto]). It records no annotation.
	CaptureKindImage CaptureKind = "image"
)

// CaptureMeta is the metadata of [HookCapture] events.
type CaptureMeta struct {
	Kind CaptureKind `json:"format"`
	// Required is true for a Require method, whose mismatch stops the test.
	Required bool `json:"required"`
}

func (m CaptureMeta) LogValue() slog.Value {
	return slog.GroupValue(slog.String("kind", string(m.Kind)), slog.Bool("required", m.Required))
}

// HookEvent identifies a harness action for hooks and mark labels (see
// [Harness.Mark]).
type HookEvent int

const (
	// HookInput is a keyboard, paste, or mouse action, with [InputMeta].
	HookInput HookEvent = iota
	// HookTerminal is a change to the terminal, such as a resize.
	HookTerminal
	// HookProgram is an action on the program, such as a message sent to it.
	HookProgram
	// HookWait is a wait, or a sleep, with [WaitMeta].
	HookWait
	// HookReady is the program's start.
	HookReady
	// HookClose is the program's end, and the harness's. The wait of
	// [Harness.WaitFinished] has [WaitMeta].
	HookClose
	// HookAssert is an assertion, with [AssertMeta].
	HookAssert
	// HookCapture is a snapshot comparison or image capture, with
	// [CaptureMeta].
	HookCapture
)

// String returns the event name used in mark labels and still file names.
func (e HookEvent) String() string {
	switch e {
	case HookInput:
		return "input"
	case HookTerminal:
		return "terminal"
	case HookProgram:
		return "program"
	case HookWait:
		return "wait"
	case HookReady:
		return "ready"
	case HookClose:
		return "close"
	case HookAssert:
		return "assert"
	case HookCapture:
		return "capture"
	default:
		return unknownName
	}
}

// unknownName names a value outside its type's known set, such as in
// annotations and hook event names.
const unknownName = "unknown"

// HookStage is whether a hook runs before or after the harness action.
type HookStage int

const (
	HookStageBefore HookStage = iota
	HookStageAfter
)

func (s HookStage) String() string {
	switch s {
	case HookStageBefore:
		return "before"
	case HookStageAfter:
		return "after"
	}
	return unknownName
}

// Hook observes harness actions (see [WithHooks]). Errors fail the test
// immediately.
//
// metadata is nil, or the action's [InputMeta], [WaitMeta], [AssertMeta], or
// [CaptureMeta], which marshal to JSON as the matching fields of the
// annotation the harness records for the action.
type Hook func(event HookEvent, stage HookStage, metadata any) error

// hook runs the Before hooks of event, and returns a function that marks it
// (see [Harness.markEvent]) and runs its After hooks. The marks of earlier
// actions waiting for the test's next step are placed before an action
// starts.
func (h *Harness) hook(event HookEvent, metadata any) func() {
	h.tb.Helper()
	if deferredMark(event) {
		h.placeMarks()
	}
	h.runHooks(event, HookStageBefore, metadata)
	return func() {
		if d := h.cfg.slow; d > 0 {
			time.Sleep(d)
		}
		// Close places its own mark as it starts.
		if event != HookClose || !h.closed.Load() {
			h.markEvent(event)
		}
		h.runHooks(event, HookStageAfter, metadata)
	}
}

func (h *Harness) runHooks(event HookEvent, stage HookStage, metadata any) {
	h.tb.Helper()
	for _, hook := range h.cfg.hooks {
		if hook == nil {
			continue
		}
		if err := hook(event, stage, metadata); err != nil {
			h.tb.Fatalf("hook %s %s: %v", event, stage.String(), err)
		}
	}
}

type testLogWriter struct{ tb testing.TB }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.tb.Helper()
	w.tb.Log(string(p))
	return len(p), nil
}

// LoggerHook returns a hook that logs events to logger, or with
// [testing.TB.Log] when logger is nil.
func LoggerHook(tb testing.TB, logger *slog.Logger) Hook {
	tb.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(testLogWriter{tb: tb}, nil))
	}
	return func(event HookEvent, stage HookStage, metadata any) error {
		args := []slog.Attr{
			slog.String("event", event.String()),
			slog.String("stage", stage.String()),
		}
		if metadata != nil {
			args = append(args, slog.Any("metadata", metadata))
		}
		logger.LogAttrs(tb.Context(), slog.LevelInfo, "steep", args...)
		return nil
	}
}
