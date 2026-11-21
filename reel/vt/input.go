// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"getsteep.dev/steep/reel/vt/internal/wasmvt"
)

// SendInput queues p on [Terminal.Read], after any bytes already queued, as
// input for the program. The Encode methods return the bytes a key, mouse,
// paste, or focus change sends; a caller that records input sends those
// bytes here once it has recorded them. It fails once [Terminal.CloseReader]
// has been called.
func (t *Terminal) SendInput(p []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	if !t.replies.Write(p) {
		return errors.New("vt: input is closed")
	}
	return nil
}

// EncodeKey returns the bytes a press of key sends the program, given the
// keyboard modes the program has set, such as the kitty keyboard protocol.
// It does not send them; see [Terminal.SendInput].
//
// key.Code is a GhosttyKey manifest name ("ENTER", "A"). An unknown name
// panics in [wasmvt.KeyByName]. An empty code leaves the key at 0 and the
// encoder uses Text.
func (t *Terminal) EncodeKey(key Key) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return nil, errClosed
	}
	if err := wasmvt.Result("key encoder modes", t.mod.Xghostty_key_encoder_set_from_terminal(t.keys, t.term)); err != nil {
		return nil, err
	}
	action := wasmvt.KeyActionPress
	t.mod.Xghostty_key_event_set_action(t.keyEv, action)
	t.mod.Xghostty_key_event_set_mods(t.keyEv, int32(key.Mods))
	code := int32(0)
	if key.Code != "" {
		code = wasmvt.KeyByName(key.Code)
	}
	t.mod.Xghostty_key_event_set_key(t.keyEv, code)
	t.mod.Xghostty_key_event_set_unshifted_codepoint(t.keyEv, unshiftedCodepoint(key))
	consumed := int32(0)
	if key.Text != "" {
		consumed = int32(key.Mods & ModShift)
	}
	t.mod.Xghostty_key_event_set_consumed_mods(t.keyEv, consumed)
	// The text is a GhosttyString passed by value, so by pointer on wasm32.
	// The event borrows the bytes until its text is set again, which every
	// call does before it encodes.
	raw := []byte(key.Text)
	n := wasmvt.SizeString + len(raw)
	str, err := t.mod.Alloc(n)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(str, n)
	t.mod.Zero(str, wasmvt.SizeString)
	if len(raw) > 0 {
		text := str + int32(wasmvt.SizeString)
		t.mod.Put(text, raw)
		t.mod.SetI32(str, wasmvt.StringPtrOff, text)
		t.mod.SetU32(str, wasmvt.StringLenOff, wasmvt.Uint32(len(raw)))
	}
	err = wasmvt.Result("key text", t.mod.Xghostty_key_event_set_utf8(t.keyEv, str))
	if err != nil {
		return nil, err
	}
	return t.encode("key encode", 128, func(buf, capN, lenPtr int32) int32 {
		return t.mod.Xghostty_key_encoder_encode(t.keys, t.keyEv, buf, capN, lenPtr)
	})
}

// unshiftedCodepoint is the character key types without modifiers on a US
// layout. A key with no code takes it from its text. The kitty keyboard
// protocol, and ctrl and alt combinations, encode that character.
func unshiftedCodepoint(key Key) int32 {
	if chars, ok := usKeys[key.Code]; ok {
		return chars[0]
	}
	if r, size := utf8.DecodeRuneInString(key.Text); key.Code == "" && size == len(key.Text) && unicode.IsPrint(r) {
		return unicode.ToLower(r)
	}
	return 0
}

// encode runs an encoder export into a scratch buffer of capN bytes and
// returns a copy of what it wrote. The length out-parameter sits
// immediately after the buffer.
func (t *Terminal) encode(what string, capN int, fn func(buf, capN, lenPtr int32) int32) ([]byte, error) {
	buf, err := t.mod.Alloc(capN + 4)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(buf, capN+4)
	lenPtr := buf + wasmvt.Int32(capN)
	t.mod.Zero(lenPtr, 4)
	rc := fn(buf, wasmvt.Int32(capN), lenPtr)
	if rc == wasmvt.ResultOutOfSpace {
		return nil, fmt.Errorf("vt: %s needs %d bytes", what, t.mod.U32(lenPtr, 0))
	}
	if rerr := wasmvt.Result(what, rc); rerr != nil {
		return nil, rerr
	}
	n := int(t.mod.U32(lenPtr, 0))
	if n == 0 {
		return nil, nil
	}
	return append([]byte(nil), t.mod.Slice(buf, n)...), nil
}

// EncodeMouse returns the bytes a mouse event sends the program, given the
// mouse tracking and format modes the program has set. It is nil when no
// enabled mode reports the event. It does not send them; see
// [Terminal.SendInput].
//
// Ghostty's position is surface pixels, so the event's cell is scaled by
// the cell size stored on the terminal. The
// position pointer is two little-endian binary32 values. The C type
// GhosttySurfacePosition is two doubles. This build's mouse event
// setter consumes the 8-byte form written here.
func (t *Terminal) EncodeMouse(m MouseEvent) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return nil, errClosed
	}
	if err := t.configureMouse(); err != nil {
		return nil, err
	}
	t.mod.Xghostty_mouse_event_set_action(t.mouseEv, int32(m.Action))
	// The event outlives this call, so a button from the last one has to go.
	// Without one, motion is reported as hover.
	if m.Button == MouseUnknown {
		t.mod.Xghostty_mouse_event_clear_button(t.mouseEv)
	} else {
		t.mod.Xghostty_mouse_event_set_button(t.mouseEv, int32(m.Button))
	}
	pos, err := t.mod.Alloc(8)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(pos, 8)
	t.mod.SetF32(pos, 0, float32(m.X)*float32(t.cellW))
	t.mod.SetF32(pos, 4, float32(m.Y)*float32(t.cellH))
	t.mod.Xghostty_mouse_event_set_position(t.mouseEv, pos)
	return t.encode("mouse encode", 64, func(buf, capN, lenPtr int32) int32 {
		return t.mod.Xghostty_mouse_encoder_encode(t.mice, t.mouseEv, buf, capN, lenPtr)
	})
}

// EncodePaste returns the bytes pasting text sends the program, bracketed
// when the program has set mode 2004. It does not send them; see
// [Terminal.SendInput].
func (t *Terminal) EncodePaste(text string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return nil, errClosed
	}
	bracketed, err := t.modeLocked(Mode{Number: 2004, ANSI: false})
	if err != nil {
		return nil, err
	}
	raw := []byte(text)
	data, err := t.mod.Alloc(len(raw) + 1)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(data, len(raw)+1)
	t.mod.Put(data, raw)
	flag := int32(0)
	if bracketed {
		flag = 1
	}
	return t.encode("paste encode", len(raw)+16, func(buf, capN, lenPtr int32) int32 {
		return t.mod.Xghostty_paste_encode(data, wasmvt.Int32(len(raw)), flag, buf, capN, lenPtr)
	})
}

// EncodeFocus returns the focus-in or focus-out report a focus change sends
// the program. It is nil unless the program has set mode 1004. It does not
// send it; see [Terminal.SendInput].
func (t *Terminal) EncodeFocus(focused bool) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return nil, errClosed
	}
	on, err := t.modeLocked(Mode{Number: 1004, ANSI: false})
	if err != nil || !on {
		return nil, err
	}
	event := wasmvt.FocusEventLost
	if focused {
		event = wasmvt.FocusEventGained
	}
	return t.encode("focus encode", 16, func(buf, capN, lenPtr int32) int32 {
		return t.mod.Xghostty_focus_encode(event, buf, capN, lenPtr)
	})
}

// configureMouse maps DEC private modes onto GhosttyMouseEncoder
// options. Tracking comes from modes 9 and 1000-1003. Format comes
// from 1005, 1006, 1015, and 1016. The size option is a sized
// GhosttyMouseEncoderSize in pixels. Enum values are passed by
// pointer and freed before return. Ghostty copies them during set.
func (t *Terminal) configureMouse() error {
	tracking := wasmvt.MouseTrackingModeNone
	switch {
	case t.modeOn(1003):
		tracking = wasmvt.MouseTrackingModeAny
	case t.modeOn(1002):
		tracking = wasmvt.MouseTrackingModeButton
	case t.modeOn(1000):
		tracking = wasmvt.MouseTrackingModeNormal
	case t.modeOn(9):
		tracking = wasmvt.MouseTrackingModeX10
	}
	format := wasmvt.MouseFormatX10
	switch {
	case t.modeOn(1016):
		format = wasmvt.MouseFormatSgrPixels
	case t.modeOn(1006):
		format = wasmvt.MouseFormatSgr
	case t.modeOn(1015):
		format = wasmvt.MouseFormatUrxvt
	case t.modeOn(1005):
		format = wasmvt.MouseFormatUtf8
	}
	opt, err := t.mod.Alloc(4)
	if err != nil {
		return err
	}
	defer t.mod.Free(opt, 4)
	t.mod.SetI32(opt, 0, tracking)
	err = wasmvt.Result("mouse tracking", t.mod.Xghostty_mouse_encoder_set(t.mice, wasmvt.MouseEncoderOptionEvent, opt))
	if err != nil {
		return err
	}
	t.mod.SetI32(opt, 0, format)
	err = wasmvt.Result("mouse format", t.mod.Xghostty_mouse_encoder_set(t.mice, wasmvt.MouseEncoderOptionFormat, opt))
	if err != nil {
		return err
	}

	n := wasmvt.SizeMouseEncoderSize
	size, err := t.mod.Alloc(n)
	if err != nil {
		return err
	}
	defer t.mod.Free(size, n)
	t.mod.Zero(size, n)
	t.mod.SetU32(size, 0, uint32(n))
	t.mod.SetU32(size, wasmvt.MouseEncoderSizeScreenWidthOff, wasmvt.Uint32(t.cols)*t.cellW)
	t.mod.SetU32(size, wasmvt.MouseEncoderSizeScreenHeightOff, wasmvt.Uint32(t.rows)*t.cellH)
	t.mod.SetU32(size, wasmvt.MouseEncoderSizeCellWidthOff, t.cellW)
	t.mod.SetU32(size, wasmvt.MouseEncoderSizeCellHeightOff, t.cellH)
	return wasmvt.Result("mouse size", t.mod.Xghostty_mouse_encoder_set(t.mice, wasmvt.MouseEncoderOptionSize, size))
}

func (t *Terminal) modeOn(number int) bool {
	on, _ := t.modeLocked(Mode{Number: number, ANSI: false})
	return on
}
