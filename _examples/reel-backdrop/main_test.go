// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	files, err := run(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if fi, statErr := os.Stat(f); statErr != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", f, statErr)
		}
	}

	// The title bar, which has the background's opacity, is translucent
	// over the transparent backdrop only.
	for name, translucent := range map[string]bool{"gradient": false, "wallpaper": false, "transparent": true} {
		f, openErr := os.Open(filepath.Join(dir, name+".png"))
		if openErr != nil {
			t.Fatal(openErr)
		}
		img, decodeErr := png.Decode(f)
		_ = f.Close()
		if decodeErr != nil {
			t.Fatalf("%s: %v", name, decodeErr)
		}
		// Past the margin, between the window buttons and the title.
		at := img.Bounds().Min.Add(image.Pt(margin+200, margin+6))
		if _, _, _, a := img.At(at.X, at.Y).RGBA(); (a < 0xffff) != translucent {
			t.Errorf("%s: the title bar has alpha %#x, want translucent %v", name, a, translucent)
		}
	}
}
