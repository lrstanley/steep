// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"fmt"
	"testing"
)

// TestBuild guards a pin bump: the module must be the release the constants
// name, built the way reel needs it, with every API package vt calls.
func TestBuild(t *testing.T) {
	t.Parallel()
	b := Build()
	if b.Version != LibraryVersion {
		t.Errorf("version = %q, want %q", b.Version, LibraryVersion)
	}
	if got := fmt.Sprintf("%d.%d.%d", b.Major, b.Minor, b.Patch); got != LibraryVersion {
		t.Errorf("version parts = %s, want %s", got, LibraryVersion)
	}
	if b.Pre != "" || b.Build != "" {
		t.Errorf("pre-release %q, build %q, want a release", b.Pre, b.Build)
	}
	if b.Commit != LibraryCommit || len(b.Commit) != 40 {
		t.Errorf("commit = %q", b.Commit)
	}
	if b.Optimize != "ReleaseSmall" {
		t.Errorf("optimize = %q, want ReleaseSmall", b.Optimize)
	}
	if b.SIMD {
		t.Error("SIMD is on: the translated module would need GOEXPERIMENT=simd")
	}
	if !b.KittyGraphics {
		t.Error("Kitty graphics are off: kitty-freestanding.patch did not apply")
	}
	want := BuildFeatures{
		Snapshot:          true,
		Formatter:         true,
		Selection:         true,
		Search:            true,
		RenderState:       true,
		InputEncode:       true,
		Color:             true,
		GridIntrospection: true,
		GlyphProtocol:     true,
	}
	if b.Features != want {
		t.Errorf("features = %+v, want %+v", b.Features, want)
	}
}
