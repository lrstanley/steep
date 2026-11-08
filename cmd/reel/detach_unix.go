// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package main

import "syscall"

// detached returns the ways to start a process in a session of its own, so
// it outlives the terminal it was started from, to try in order.
func detached() []*syscall.SysProcAttr {
	return []*syscall.SysProcAttr{{Setsid: true}}
}

// handleCtrlC does nothing: ctrl+c is the terminal's to signal.
func handleCtrlC() {}
