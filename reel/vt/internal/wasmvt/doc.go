// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package wasmvt is the host side of the checked-in libghostty-vt module.
//
// vt.generated.go is that module translated by wasm2go
// (github.com/ncruces/wasm2go). The translator emits one Go method per
// Wasm export and does not know the C ABI those exports implement. This
// package is the hand-written half: linear memory, the type manifest,
// opaque constructors, and GhosttyResult. Terminal behavior stays in
// package vt.
//
// # Updating Ghostty
//
// The Wasm module is built from one libghostty-vt release, GHOSTTY_TAG in
// vt/build/Dockerfile. [GhosttyRev] is the commit that tag points at and
// must match GHOSTTY_REV there. Rebuild from the module root
// with `task vt:generate` after changing the pin. That rebuilds the
// module and regenerates abi.gen.go from ghostty_type_json. Struct
// layouts are not stable across commits. Use the generated constants.
//
// WebAssembly utilities (alloc, free, opaque slots, memory growth):
//
//	https://github.com/ghostty-org/ghostty/blob/108cacb17ed8990840cd14abfce43d8ed8503ca1/include/ghostty/vt/wasm.h
//
// Implementation of those exports:
//
//	https://github.com/ghostty-org/ghostty/blob/108cacb17ed8990840cd14abfce43d8ed8503ca1/src/lib/allocator/wasm.zig
//
// Shared types, GhosttyResult, GhosttyString, and ghostty_type_json:
//
//	https://github.com/ghostty-org/ghostty/blob/108cacb17ed8990840cd14abfce43d8ed8503ca1/include/ghostty/vt/types.h
//
// JSON schema for that manifest:
//
//	https://github.com/ghostty-org/ghostty/blob/108cacb17ed8990840cd14abfce43d8ed8503ca1/src/terminal/c/types.schema.json
//
// Terminal options and queries (ghostty_terminal_set, ghostty_terminal_get):
//
//	https://github.com/ghostty-org/ghostty/blob/108cacb17ed8990840cd14abfce43d8ed8503ca1/include/ghostty/vt/terminal.h
//
// # Memory
//
// One [Module] is one Wasm instance and one concurrency domain. Its
// methods are not safe for concurrent use. [Module.Alloc] and most
// exports can grow linear memory. Numeric pointers and handles stay
// valid, but a slice from [Module.Memory] or [Module.Slice] does not.
// Re-read memory immediately before the next host access. wasm.h
// ("Memory growth") is the upstream description of that rule.
//
// # ABI
//
// This host assumes the pinned wasm32-freestanding build: little-endian,
// 4-byte pointers, and 4-byte size_t. cmd/genabi refuses to emit constants
// for any other target. A rebuild for another target needs new load and
// store code as well as a generator change.
//
// Sized C structs (anything initialized with GHOSTTY_INIT_SIZED) store
// their byte size in the first field. Callers that pass one by pointer
// write that size before the call. The manifest field list is authoritative
// for the offset. On this target it is 0.
package wasmvt

// GhosttyRev is the Ghostty commit (libghostty-vt 1.0.1) these helpers and
// the doc links were written against. It must match GHOSTTY_REV in vt/build/Dockerfile.
const GhosttyRev = "108cacb17ed8990840cd14abfce43d8ed8503ca1"
