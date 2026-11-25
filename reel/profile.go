// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"fmt"
	"image"
	"maps"
	"math"
	"time"

	"getsteep.dev/steep/reel/render"
	"getsteep.dev/steep/reel/sink"
)

// Profile configures how [Export] and [Record] turn a [Timeline] into files.
// Every field but Outputs has a usable zero value. A profile holds no state,
// so it can be shared by parallel recordings.
type Profile struct {
	// Outputs are the files to write; it must not be empty.
	Outputs []File
	// Render configures the renderer. A [render.WithPalette] palette
	// replaces the timeline's [Header.Palette].
	Render []render.Option
	// Fit, if set, fits frames within X by Y pixels by choosing the render
	// scale (see [render.FitScale]), so text stays sharp rather than the
	// image being resized; one direction may come out smaller. An X or Y of 0
	// sets no limit in that direction. A [Resize] to a larger terminal still
	// grows the frames. [render.WithScale] in Render, or a Rasterizer, is an
	// error.
	Fit image.Point
	// Rasterizer, if set, draws frames instead of a renderer built from
	// Render. It is not closed, and each recording uses it from one
	// goroutine.
	Rasterizer Rasterizer
	// Encode configures every frame output (see the sink package).
	Encode []sink.Option
	// Effects transform frames, by priority and then in the order listed
	// (see [Prioritizer]). See [Concealer] for the rules on hiding secrets
	// and [Describer] for how asciicast outputs record them.
	Effects []Effect
	// FPS is the frame rate, in frames a second, that recordings are timed
	// on: frames play whole periods of 1s/FPS. Zero means 20. Each recording
	// output may set its own ([File.FPS]); it must be at most the
	// [sink.MaxFrameRate] of each output that uses it, such as 50 for GIF
	// and 240 for MP4, or the recording fails rather than play slower. Frames
	// are not drawn at a fixed rate: each visible change is a frame ending
	// on the period nearest its timeline end, and of changes in one period
	// only the newest is shown.
	FPS int
	// Speed scales playback: 2 plays twice as fast. Zero means 1.
	Speed float64
	// Title names the recording, for effects such as a title card and for
	// asciicast outputs. Defaults to the timeline's [Header.Title].
	Title string
	// Meta is passed to effects in [Context.Meta]; reel never reads it.
	Meta any
	// Vars are extra path variables (see [Export]), by name without braces.
	Vars map[string]string
	// Limits bound how long the recording plays and how large its terminal
	// and frames get. Zero fields are the defaults, which are enough for
	// demos and tests, so only timelines you trust that need more should
	// raise them.
	Limits Limits
}

// File is one output a [Profile] writes.
//
// An asciicast (".cast") output is the timeline itself, written with
// [Timeline.WriteCast], with [Profile.Title] and the drawn palette in its
// header. It bypasses the effects, so it cannot be combined with a
// [Concealer] or [Cutter]. Instead, it records each [Describer] effect's description,
// in the order they run, as an [Annotation] at the start of the file. These
// replace the timeline's existing annotations of [EffectKind] and of the
// described kinds, so re-exporting a file with its own effects does not
// duplicate them. Effects that are not Describers, or whose Describe
// returns an error wrapping [errors.ErrUnsupported], are left out. Before
// anything is written, it is an error for Describe to fail otherwise or
// for a description to fail [AnnotationSchema]; see [MarkChecker] for
// effects that name marks.
//
// [ParseFile] reads a File written as a path with query parameters, such as
// for a command line or configuration file.
type File struct {
	// Path is where the file is written; it may contain variables (see
	// [Export]). The extension picks the format: ".cast" for asciicast,
	// ".snap" for the screen's text, ".json" for the screen as a [Screen],
	// otherwise any format [sink.File] supports, such as ".gif" or ".mp4".
	// Missing directories are created.
	//
	// A ".snap" or ".json" output is the screen at the end of the window,
	// or with Marks or MarkMatch at each mark, from the replay. Effects do
	// not change it, so it shows what a [Concealer] such as a redaction
	// hides. It takes no Options.
	Path string
	// Options are added to [Profile.Encode] for this output.
	Options []sink.Option
	// Marks, if set, makes the output one still per [Mark] label instead of
	// a recording; Path must contain {mark}, which is the label with each
	// character but "/" and "\" that Windows does not allow in file names
	// replaced by "-", so "input:1" is written as "input-1"; two labels
	// written the same, such as "step:1" and "step-1", are an error. A
	// repeated label uses its first mark in the window. A missing one
	// writes nothing, and is reported as a [NoMarkError] once the other
	// outputs are written. Stills run through only [Concealer] and
	// [StillEffect] effects.
	Marks []string
	// MarkMatch, like Marks, makes the output one still per mark whose
	// label matches one of these [path.Match] patterns, such as "input:*",
	// after those of Marks, oldest first. A pattern that matches no mark is
	// not an error. A matched label must be a file name, without slashes.
	// Only [Export] can match marks, since [Record] starts before them.
	MarkMatch []string
	// Cast is the asciicast version for a ".cast" Path; zero means
	// [CastV2]. Setting it on other paths is an error.
	Cast CastVersion
	// FPS, if set, replaces [Profile.FPS] for this output, so one profile
	// can write a GIF at 50 FPS and an MP4 at 120. Outputs of different
	// rates run the effects once each. It must be at most the format's
	// [sink.MaxFrameRate], and is an error on outputs that are not
	// recordings: stills, asciicast, and screen outputs.
	FPS int
}

// MustFiles returns a [File] for each output, parsed with [ParseFile], such
// as MustFiles("{name}.gif?loop=once", "{name}.cast"). It panics if one
// does not parse, so it is for outputs written in code; use ParseFile for
// those read at run time.
func MustFiles(outputs ...string) []File {
	files := make([]File, len(outputs))
	for i, raw := range outputs {
		f, err := ParseFile(raw)
		if err != nil {
			panic(err)
		}
		files[i] = f
	}
	return files
}

// Merge returns p with every non-zero field of o replacing p's. Slices are
// replaced, not appended to. Vars merge by name, with o winning, and so do
// the fields of Limits.
func (p Profile) Merge(o Profile) Profile {
	if o.Outputs != nil {
		p.Outputs = o.Outputs
	}
	if o.Render != nil {
		p.Render = o.Render
	}
	if o.Fit != (image.Point{}) {
		p.Fit = o.Fit
	}
	if o.Rasterizer != nil {
		p.Rasterizer = o.Rasterizer
	}
	if o.Encode != nil {
		p.Encode = o.Encode
	}
	if o.Effects != nil {
		p.Effects = o.Effects
	}
	if o.FPS != 0 {
		p.FPS = o.FPS
	}
	if o.Speed != 0 {
		p.Speed = o.Speed
	}
	if o.Title != "" {
		p.Title = o.Title
	}
	if o.Meta != nil {
		p.Meta = o.Meta
	}
	if o.Vars != nil {
		vars := maps.Clone(p.Vars)
		if vars == nil {
			vars = make(map[string]string, len(o.Vars))
		}
		maps.Copy(vars, o.Vars)
		p.Vars = vars
	}
	p.Limits = p.Limits.merge(o.Limits)
	return p
}

const defaultFPS = 20

// maxFPS is the highest [sink.MaxFrameRate] of any format, and so the highest
// [Profile.FPS].
const maxFPS = 240

// settings are a profile's timing with defaults applied.
type settings struct {
	// step is the period of the profile's frame rate; a recording output
	// of its own rate runs on a copy with it changed (see [settings.at]).
	step  time.Duration
	speed float64
	// limits are the profile's; zero fields are the defaults.
	limits Limits
}

// settings validates p's timing and applies defaults.
func (p Profile) settings() (settings, error) {
	var s settings
	fps := cmp.Or(p.FPS, defaultFPS)
	if fps < 1 || fps > maxFPS {
		return s, fmt.Errorf("reel: FPS %d out of range [1, %d]", p.FPS, maxFPS)
	}
	s = s.at(fps)
	s.limits = p.Limits
	s.speed = p.Speed
	if s.speed == 0 {
		s.speed = 1
	}
	if !(s.speed > 0) || math.IsInf(s.speed, 0) { // Also catches NaN.
		return s, fmt.Errorf("reel: speed %v must be positive and finite", p.Speed)
	}
	return s, nil
}

// at returns s timed at fps, with its period rounded to the nanosecond. The
// pipeline rounds frame ends, not delays, to whole periods, so the rounding
// moves an hour at 240 FPS by under half a millisecond.
func (s settings) at(fps int) settings {
	s.step = (time.Second + time.Duration(fps)/2) / time.Duration(fps)
	return s
}
