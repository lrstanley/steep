// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"time"

	"getsteep.dev/steep/reel/vt/internal/wasmvt"
)

// ControlStringMaxBytes is how much of each string [WithControlStrings]
// reports. Longer ones are cut and marked [ControlString.Truncated].
const ControlStringMaxBytes = 4096

// WithControlStrings calls fn with each APC, DCS, OSC, SOS, and PM string the
// program writes, implemented or not, as it was on the wire: its data, how it
// was introduced, and how it ended. A string split across writes is reported
// once its end arrives. fn runs inside [Terminal.Write] with the terminal
// locked, so it must not call the terminal's methods.
//
// The strings are found by the same parse that runs them, with
// ghostty_terminal_vt_write_until, so writes stop at each string's start and
// end. Without this option or [WithUnknownSequence], writes don't stop.
func WithControlStrings(fn func(ControlString)) Option {
	return func(cfg *openConfig) { cfg.controls = fn }
}

// ControlString is one APC, DCS, OSC, SOS, or PM string a program wrote. See
// [WithControlStrings].
type ControlString struct {
	// Kind is the kind of string.
	Kind SequenceKind
	// Data is everything between the introducer and the terminator. A DCS
	// starts with its parameters, intermediates, and final byte, and an OSC
	// with its number. It may hold binary data.
	Data string
	// Truncated is true when Data holds only the first
	// [ControlStringMaxBytes] of the string.
	Truncated bool
	// C1 is true when the string started with an 8-bit introducer, such as
	// 0x9D for OSC, rather than ESC and a byte.
	C1 bool
	// End is how the string ended.
	End StringEnd
	// At is when the string's end arrived, on the terminal's clock (see
	// [WithClock]).
	At time.Duration
}

// StringEnd is how a [ControlString] ended.
type StringEnd string

// String ends. Names match GhosttyTerminalStringEnd in
// include/ghostty/vt/terminal.h.
const (
	// EndST is the 7-bit string terminator, ESC \. A string ending in ESC
	// is reported once the byte after it arrives.
	EndST StringEnd = "st"
	// EndESC is ESC followed by a byte other than \, which ends the string
	// without a proper terminator.
	EndESC StringEnd = "esc"
	// EndSTC1 is the 8-bit string terminator, 0x9C.
	EndSTC1 StringEnd = "st-c1"
	// EndBEL is BEL, which only ends an OSC.
	EndBEL StringEnd = "bel"
	// EndCancelled is CAN or SUB. The terminal drops a cancelled APC or
	// OSC.
	EndCancelled StringEnd = "cancelled"
	// EndAborted is another 8-bit control byte cutting the string short.
	// The terminal drops an aborted APC.
	EndAborted StringEnd = "aborted"
)

// Boundaries a write stops at to find strings.
const (
	stringStarts = wasmvt.TerminalBoundaryApcStart | wasmvt.TerminalBoundaryDcsStart | wasmvt.TerminalBoundaryOscStart
	stringEnds   = wasmvt.TerminalBoundaryApcEnd | wasmvt.TerminalBoundaryDcsEnd | wasmvt.TerminalBoundaryOscEnd
	dcsBounds    = wasmvt.TerminalBoundaryDcsStart | wasmvt.TerminalBoundaryDcsEnd
)

// stringScan is the string a write is inside, while strings are watched.
type stringScan struct {
	// open is true between a string's start and end.
	open      bool
	kind      SequenceKind
	c1        bool
	data      []byte
	truncated bool
	// escAt is when the closed string ended with ESC, while escEnd says the
	// byte after it has not arrived. ESC \ is ST; any other byte after
	// ESC ends the string all the same.
	escEnd bool
	escAt  time.Duration
}

// add keeps p as more of the open string's data, up to the most either
// callback reports.
func (s *stringScan) add(p []byte) {
	if !s.open || len(p) == 0 {
		return
	}
	room := max(ControlStringMaxBytes, UnknownSequenceMaxBytes) - len(s.data)
	if len(p) > room {
		p, s.truncated = p[:max(room, 0)], true
	}
	s.data = append(s.data, p...)
}

// boundariesLocked are the boundaries writes stop at, or 0 when nothing
// watches strings and writes don't stop. Ghostty reports unknown APC and
// OSC strings itself, so [WithUnknownSequence] alone stops only at DCS.
// [WithSnapshots] watches every string, so a [Snapshot] knows the one it
// ends inside, and a restore onto a terminal [WithControlStrings] reports it.
func (t *Terminal) boundariesLocked() int32 {
	switch {
	case t.controls != nil || t.snapshots:
		return stringStarts | stringEnds
	case t.unknown != nil:
		return dcsBounds
	default:
		return 0
	}
}

// writeUntilLocked writes chunk, which is already in the feed buffer, and
// stops at the start and end of each string to collect it. Every byte is
// written in order, so the terminal ends up as one plain write would leave
// it.
func (t *Terminal) writeUntilLocked(chunk []byte, bounds int32) {
	stop, n := t.writeStop, wasmvt.SizeTerminalWriteStop
	for off := 0; off < len(chunk); {
		if t.scan.escEnd {
			// The byte after a string's closing ESC tells ST from the
			// start of something else. It is written as usual.
			t.scan.escEnd = false
			end := EndESC
			if chunk[off] == '\\' {
				end = EndST
			}
			t.reportStringLocked(end, t.scan.escAt)
		}
		t.mod.Zero(stop, n)
		t.mod.SetU32(stop, wasmvt.TerminalWriteStopSizeOff, uint32(n))
		rc := t.mod.Xghostty_terminal_vt_write_until(t.term, t.feed+wasmvt.Int32(off), wasmvt.Int32(len(chunk)-off), bounds, stop)
		if rc == wasmvt.ResultNoValue {
			t.scan.add(chunk[off:])
			return
		}
		if rc != 0 {
			// Not expected for valid arguments. The rest is written
			// without stops, so no byte is lost.
			t.mod.Xghostty_terminal_vt_write(t.term, t.feed+wasmvt.Int32(off), wasmvt.Int32(len(chunk)-off))
			return
		}
		end := off + int(t.mod.U32(stop, wasmvt.TerminalWriteStopConsumedOff))
		if end <= off {
			// A ground stop, which is never asked for. Write the rest.
			t.mod.Xghostty_terminal_vt_write(t.term, t.feed+wasmvt.Int32(off), wasmvt.Int32(len(chunk)-off))
			return
		}
		reached := wasmvt.AsI32(t.mod.U32(stop, wasmvt.TerminalWriteStopReachedOff))
		// One byte can end a string and start the next, so the end is
		// handled first. The stopping byte belongs to neither's data.
		if reached&stringEnds != 0 {
			t.scan.add(chunk[off : end-1])
			t.endStringLocked(t.mod.I32(stop, wasmvt.TerminalWriteStopEndOff))
		}
		if reached&stringStarts != 0 {
			t.startStringLocked(reached, t.mod.U8(stop, wasmvt.TerminalWriteStopByteOff),
				t.mod.I32(stop, wasmvt.TerminalWriteStopIntroducerOff) == wasmvt.TerminalStringIntroducerC1)
		}
		off = end
	}
}

// startStringLocked opens the string a stop started. b is its introducer's
// last byte, which tells SOS and PM from APC.
func (t *Terminal) startStringLocked(reached int32, b byte, c1 bool) {
	kind := SequenceAPC
	switch {
	case reached&wasmvt.TerminalBoundaryDcsStart != 0:
		kind = SequenceDCS
	case reached&wasmvt.TerminalBoundaryOscStart != 0:
		kind = SequenceOSC
	case b == 'X' || b == 0x98:
		kind = SequenceSOS
	case b == '^' || b == 0x9e:
		kind = SequencePM
	}
	t.scan = stringScan{open: true, kind: kind, c1: c1, data: t.scan.data[:0]}
}

// endStringLocked closes the open string and reports it, or for one that
// ended with ESC, waits for the byte after it. An end with no start seen,
// such as one a pooled terminal's last checkout started, is dropped.
func (t *Terminal) endStringLocked(how int32) {
	s := &t.scan
	if !s.open {
		return
	}
	s.open = false
	if how == wasmvt.TerminalStringEndStEsc {
		s.escEnd, s.escAt = true, t.clock()
		return
	}
	t.reportStringLocked(stringEnd(how), t.clock())
}

// reportStringLocked reports the closed string to the callbacks, which ended
// at at.
func (t *Terminal) reportStringLocked(end StringEnd, at time.Duration) {
	s := &t.scan
	if t.controls != nil {
		data, cut := s.data, s.truncated
		if len(data) > ControlStringMaxBytes {
			data, cut = data[:ControlStringMaxBytes], true
		}
		t.controls(ControlString{
			Kind: s.kind, Data: string(data), Truncated: cut,
			C1: s.c1, End: end, At: at,
		})
	}
	// The terminal drops a cancelled or aborted string without running it.
	if t.unknown != nil && s.kind == SequenceDCS && end != EndCancelled && end != EndAborted && !knownDCS(s.data) {
		data, cut := s.data, s.truncated
		if len(data) > UnknownSequenceMaxBytes {
			data, cut = data[:UnknownSequenceMaxBytes], true
		}
		t.unknown(UnknownSequence{Kind: SequenceDCS, Data: string(data), Truncated: cut, At: at})
	}
}

func stringEnd(how int32) StringEnd {
	switch how {
	case wasmvt.TerminalStringEndStC1:
		return EndSTC1
	case wasmvt.TerminalStringEndCancelled:
		return EndCancelled
	case wasmvt.TerminalStringEndAborted:
		return EndAborted
	default:
		return EndBEL
	}
}

// knownDCS reports whether libghostty-vt implements the DCS whose data is
// data: XTGETTCAP (DCS + q) and DECRQSS (DCS $ q). Like Ghostty, it looks
// only at the intermediate and final bytes of the header. DCS 1000 p, tmux
// control mode, is not built in.
func knownDCS(data []byte) bool {
	i := 0
	for i < len(data) && (data[i] >= '0' && data[i] <= '9' || data[i] == ';' || data[i] == ':') {
		i++
	}
	start := i
	for i < len(data) && data[i] >= 0x20 && data[i] <= 0x2f {
		i++
	}
	if i >= len(data) || i-start != 1 || data[i] != 'q' {
		return false
	}
	return data[start] == '+' || data[start] == '$'
}

// InSequence reports whether the bytes written so far end inside an escape
// sequence or a UTF-8 character, so the program has more to write before the
// last of it takes effect. A Kitty image sent in several writes, for one, is
// not stored until its last write.
func (t *Terminal) InSequence() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return false
	}
	return t.inSequenceLocked()
}

// inSequenceLocked asks the parser with a write of no bytes that stops at
// ground: at ground it stops at once, and otherwise there is nothing to
// write, so either way nothing changes.
func (t *Terminal) inSequenceLocked() bool {
	stop, n := t.writeStop, wasmvt.SizeTerminalWriteStop
	t.mod.Zero(stop, n)
	t.mod.SetU32(stop, wasmvt.TerminalWriteStopSizeOff, uint32(n))
	return t.mod.Xghostty_terminal_vt_write_until(t.term, 0, 0, wasmvt.TerminalBoundaryGround, stop) == wasmvt.ResultNoValue
}
