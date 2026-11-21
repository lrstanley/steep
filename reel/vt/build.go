// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"sync"

	"getsteep.dev/steep/reel/vt/internal/wasmvt"
)

// LibraryVersion is the libghostty-vt release the package embeds, such as
// "1.0.1". It is fixed when the module is built, so it needs no emulator
// instance. [Build] reads the same value from the module itself.
const LibraryVersion = wasmvt.ABILibraryVersion

// LibraryCommit is the Ghostty commit the embedded libghostty-vt is built
// from, before reel's patches.
const LibraryCommit = wasmvt.GhosttyRev

// BuildInfo is how the embedded libghostty-vt was built, as the module
// reports it with ghostty_build_info.
type BuildInfo struct {
	// Version is the full version string, such as "1.0.1".
	Version string
	// Major, Minor, and Patch are the parts of Version.
	Major, Minor, Patch int
	// Pre and Build are Version's pre-release and build metadata, empty
	// for a release.
	Pre, Build string
	// Commit is [LibraryCommit].
	Commit string
	// Optimize is the Zig optimize mode: "Debug", "ReleaseSafe",
	// "ReleaseSmall", or "ReleaseFast".
	Optimize string
	// SIMD is true when the module uses SIMD. reel's build keeps it off,
	// so the translated Go needs no experiment.
	SIMD bool
	// KittyGraphics is true when the Kitty graphics protocol is compiled in.
	KittyGraphics bool
	// TmuxControlMode is true when tmux control mode is compiled in.
	TmuxControlMode bool
	// Features are the optional C APIs the module includes.
	Features BuildFeatures
}

// BuildFeatures are the optional parts of the libghostty-vt C API, each true
// when the embedded module includes it.
type BuildFeatures struct {
	Snapshot          bool
	Formatter         bool
	Selection         bool
	Search            bool
	RenderState       bool
	InputEncode       bool
	Color             bool
	GridIntrospection bool
	GlyphProtocol     bool
}

// buildInfo reads [BuildInfo] once, from an instance of its own that is
// dropped afterwards.
var buildInfo = sync.OnceValue(func() BuildInfo {
	return readBuildInfo(wasmvt.New())
})

// Build reports how the embedded libghostty-vt was built. The first call
// starts an emulator instance to ask, and later calls return the same value.
func Build() BuildInfo {
	return buildInfo()
}

func readBuildInfo(m *wasmvt.Module) BuildInfo {
	out, err := m.Alloc(wasmvt.SizeString)
	if err != nil {
		return BuildInfo{Version: LibraryVersion, Commit: LibraryCommit}
	}
	defer m.Free(out, wasmvt.SizeString)
	get := func(data int32) bool {
		m.Zero(out, wasmvt.SizeString)
		return m.Xghostty_build_info(data, out) == 0
	}
	flag := func(data int32) bool { return get(data) && m.U8(out, 0) != 0 }
	str := func(data int32) string {
		if !get(data) {
			return ""
		}
		return m.String(out, 0)
	}
	num := func(data int32) int {
		if !get(data) {
			return 0
		}
		return int(m.U32(out, 0))
	}
	info := BuildInfo{
		Version:         str(wasmvt.BuildInfoVersionString),
		Major:           num(wasmvt.BuildInfoVersionMajor),
		Minor:           num(wasmvt.BuildInfoVersionMinor),
		Patch:           num(wasmvt.BuildInfoVersionPatch),
		Pre:             str(wasmvt.BuildInfoVersionPre),
		Build:           str(wasmvt.BuildInfoVersionBuild),
		Commit:          LibraryCommit,
		SIMD:            flag(wasmvt.BuildInfoSimd),
		KittyGraphics:   flag(wasmvt.BuildInfoKittyGraphics),
		TmuxControlMode: flag(wasmvt.BuildInfoTmuxControlMode),
		Features: BuildFeatures{
			Snapshot:          flag(wasmvt.BuildInfoFeatureSnapshot),
			Formatter:         flag(wasmvt.BuildInfoFeatureFormatter),
			Selection:         flag(wasmvt.BuildInfoFeatureSelection),
			Search:            flag(wasmvt.BuildInfoFeatureSearch),
			RenderState:       flag(wasmvt.BuildInfoFeatureRenderState),
			InputEncode:       flag(wasmvt.BuildInfoFeatureInputEncode),
			Color:             flag(wasmvt.BuildInfoFeatureColor),
			GridIntrospection: flag(wasmvt.BuildInfoFeatureGridIntrospection),
			GlyphProtocol:     flag(wasmvt.BuildInfoFeatureGlyphProtocol),
		},
	}
	if get(wasmvt.BuildInfoOptimize) {
		info.Optimize = optimizeMode(m.I32(out, 0))
	}
	return info
}

func optimizeMode(mode int32) string {
	switch mode {
	case wasmvt.OptimizeModeDebug:
		return "Debug"
	case wasmvt.OptimizeModeReleaseSafe:
		return "ReleaseSafe"
	case wasmvt.OptimizeModeReleaseSmall:
		return "ReleaseSmall"
	case wasmvt.OptimizeModeReleaseFast:
		return "ReleaseFast"
	default:
		return ""
	}
}
