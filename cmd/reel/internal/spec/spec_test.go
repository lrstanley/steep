// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package spec

import (
	"strings"
	"testing"

	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/effects"
	"getsteep.dev/steep/reel/region"
)

func TestEffect(t *testing.T) {
	tests := []struct {
		kind, spec, err string
	}{
		{kind: "highlight", spec: "rows=2-4;dim=0.4;outline=#f5c2e7:3;padding=4;between=a,b;ease=200ms"},
		{kind: "highlight", spec: "row=1;outline=#fff"},
		{kind: "zoom", spec: "re=ERROR.*;scale=2;anchor=0.5,0.5"},
		{kind: "zoom", spec: "cells=0,0,10,2"},
		{kind: "redact", spec: "env=TOKEN;style=blur"},
		{kind: "redact", spec: "text=secret;style=replace:*;between=,done"},
		{kind: "redact", spec: "text=x;style=block"},
		{kind: "blur", spec: "row=1", err: `unknown effect "blur"`},
		{kind: "zoom", spec: "row", err: `region "row" is not KIND=VALUE`},
		{kind: "highlight", spec: "row=-1"},
		{kind: "highlight", spec: "rows=-2-0"},
		{kind: "highlight", spec: "rows=1--1"},
		{kind: "zoom", spec: "rows=1", err: "want 2 numbers"},
		{kind: "zoom", spec: "rows=1-2-3", err: "want 2 numbers"},
		{kind: "zoom", spec: "rows=a-b", err: "want 2 numbers"},
		{kind: "zoom", spec: "rows=1-99999999999999999999", err: "out of range"},
		{kind: "zoom", spec: "cells=1,2,3", err: "want 4 numbers"},
		{kind: "zoom", spec: "re=(", err: "missing closing )"},
		{kind: "zoom", spec: "pixel=1", err: `unknown region "pixel"`},
		{kind: "zoom", spec: "row=1;scale", err: `option "scale" is not KEY=VALUE`},
		{kind: "zoom", spec: "row=1;scale=2;scale=3", err: `option "scale" is repeated`},
		{kind: "zoom", spec: "row=1;dim=2", err: `unknown option "dim"`},
		{kind: "zoom", spec: "row=1;between=a", err: `between: "a" is not FROM,TO`},
		{kind: "redact", spec: "row=1;style=fuzzy", err: `style "fuzzy" is not blur`},
		{kind: "redact", spec: "row=1;ease=1s", err: `unknown option "ease"`},
		{kind: "highlight", spec: "row=1;outline=red", err: "is not a #rrggbb color"},
	}
	for _, tt := range tests {
		t.Run(tt.kind+" "+tt.spec, func(t *testing.T) {
			e, err := Effect(tt.kind, tt.spec)
			if tt.err == "" {
				if err != nil || e == nil {
					t.Fatalf("Effect = %v, %v", e, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("error = %v, want one containing %q", err, tt.err)
			}
		})
	}
}

func TestCaption(t *testing.T) {
	t.Parallel()
	note := func(a reel.Annotation, err error) string {
		if err != nil {
			t.Fatal(err)
		}
		return string(a.Data)
	}
	for _, tc := range []struct {
		spec string
		want string
		err  string
	}{
		{spec: "", want: `{"text":"x"}`},
		{spec: "pause=auto;for=1500ms;until=done;side=top", want: `{"text":"x","pause_auto":true,"for_ms":1500,"until":"done","side":"top"}`},
		{spec: "pause=0s", want: `{"text":"x","pause_ms":0}`},
		{spec: "spotlight=false", want: `{"text":"x"}`},
		{spec: "pause=-1s", err: "pause -1s below zero"},
		{spec: "pause=6m", err: "pause 6m0s is longer than 5m0s"},
		{spec: "pause=soon", err: `pause "soon" is not a duration or auto`},
		{spec: "side=diagonal", err: `"diagonal" is not top, bottom, left, or right`},
		{spec: "until=", err: "no mark"},
		{spec: "for=2s;for=3s", err: `option "for" is repeated`},
		{spec: "wobble", err: `option "wobble" is not KEY=VALUE`},
	} {
		opts, err := Caption(tc.spec)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("Caption(%q) = %v, want %q", tc.spec, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Caption(%q): %v", tc.spec, err)
			continue
		}
		if got := note(effects.NewCaption("x", opts...)); got != tc.want {
			t.Errorf("Caption(%q) = %s, want %s", tc.spec, got, tc.want)
		}
	}

	region, opts, err := Callout("text=Save;spotlight;side=right")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text":"x","anchor":{"type":"text","text":"Save"},"side":"right","spotlight":true}`
	if got := note(effects.NewCallout(region, "x", opts...)); got != want {
		t.Errorf("Callout = %s, want %s", got, want)
	}
	if _, _, err = Callout("Save"); err == nil {
		t.Error("Callout without a region: no error")
	}
}

func TestRegionRows(t *testing.T) {
	t.Parallel()
	for spec, want := range map[string]string{
		"row=-1":     `{"type":"rows","from":-1,"to":0}`,
		"rows=2-4":   `{"type":"rows","from":2,"to":4}`,
		"rows=-2-0":  `{"type":"rows","from":-2,"to":0}`,
		"rows=1--1":  `{"type":"rows","from":1,"to":-1}`,
		"rows=-3--1": `{"type":"rows","from":-3,"to":-1}`,
		"rows=+1-+3": `{"type":"rows","from":1,"to":3}`,
	} {
		r, err := Region(spec)
		if err != nil {
			t.Fatalf("Region(%q): %v", spec, err)
		}
		got, err := region.Marshal(r)
		if err != nil || string(got) != want {
			t.Errorf("Region(%q) = %s, %v, want %s", spec, got, err, want)
		}
	}
}
