// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package look

import (
	"fmt"
	"strings"
	"testing"
)

func TestMerge(t *testing.T) {
	tape := Look{Title: new("tape"), FontSize: new(22.0), Bell: new(true)}
	flags := Look{Title: new("flag"), Bell: new(false)}
	got := tape.Merge(flags)
	if *got.Title != "flag" || *got.FontSize != 22 || *got.Bell {
		t.Errorf("merged = title %q, font size %v, bell %v; want the flags' over the tape's", *got.Title, *got.FontSize, *got.Bell)
	}
}

func TestValidate(t *testing.T) {
	for l, want := range map[*Look]string{
		{FontSize: new(0.0)}:                 "font size must be above 0",
		{Speed: new(-1.0)}:                   "speed must be above 0",
		{FPS: new(0)}:                        "fps must be at least 1",
		{Padding: new(-1)}:                   "padding must not be negative",
		{MarginFill: new("red")}:             "margin fill",
		{Theme: new("no-such-x")}:            "unknown theme",
		{LineHeight: new(0.0)}:               "line height must be above 0",
		{Theme: new("Dracula")}:              "",
		{MarginFill: new("#fff")}:            "",
		{MarginFill: new("45deg #fff #000")}: "",
		{MarginFill: new("45 #fff")}:         "margin fill",
	} {
		err := l.Validate()
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("Validate(%+v) = %v, want %q", *l, err, want)
		}
	}
}

func TestRender(t *testing.T) {
	l := Look{Theme: new("Dracula"), Padding: new(10), Margin: new(4), WindowBar: new(true), LetterSpacing: new(1.0), FontFamily: new("No Such Font")}
	opts, warnings, err := l.Render(18)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) == 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "No Such Font") {
		t.Errorf("Render = %d options, warnings %q", len(opts), warnings)
	}
}

func TestParseMarginFill(t *testing.T) {
	for in, want := range map[string]string{
		"#000":             "180 1 <nil>",
		"135deg #000 #fff": "135 2 <nil>",
		"90deg":            `0 0 "90deg" has no colors`,
		"rightdeg #000":    `0 0 "rightdeg" is not an angle like 135deg`,
		"#000 red":         `0 0 "red" is not a #rrggbb color`,
	} {
		angle, colors, err := ParseMarginFill(in)
		if got := fmt.Sprint(angle, " ", len(colors), " ", err); got != want {
			t.Errorf("ParseMarginFill(%q) = %s, want %s", in, got, want)
		}
	}
}
