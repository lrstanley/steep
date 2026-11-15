// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package native is a [sink.Driver] that encodes animated WebP without
// ffmpeg or cgo. It is its own module, so programs that only use the
// default ffmpeg driver don't pull in its encoder.
//
// It encodes WebP only, so list it beside [sink.FFmpeg], which encodes MP4
// and WebM. With ffmpeg first, WebP uses ffmpeg where it is installed and
// this driver where it isn't:
//
//	sink.WithDrivers(sink.FFmpeg(""), native.Driver())
//
// Animated WebP is encoded as frames arrive, so memory stays flat however
// long the recording is. Each frame stores only the rectangle that changed
// since the previous one, and a frame identical to the previous one extends
// its duration instead. Frames are lossless by default and at
// [sink.MaxQuality], and lossy at a lower [sink.WithQuality].
//
// The encoder is libwebp, through github.com/gen2brain/webp, which runs it
// as Go code translated from WebAssembly. It uses a system libwebp instead
// when it finds one, unless built with the nodynamic tag; output then
// depends on that library's version.
package native
