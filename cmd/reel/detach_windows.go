// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detached returns the ways to start a process without its parent's
// console, so it outlives the console it was started from, to try in
// order: out of its parent's job too, so that closing the job does not end
// it, then in the job, for one that does not let processes leave it (such
// as the one PowerShell's Start-Process -Wait waits on, so that waits for
// the session to end).
//
// It is not a new process group, which would ignore ctrl+c, and pass that
// on to the programs it starts.
func detached() []*syscall.SysProcAttr {
	return []*syscall.SysProcAttr{
		{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_BREAKAWAY_FROM_JOB},
		{CreationFlags: windows.DETACHED_PROCESS},
	}
}

var setConsoleCtrlHandler = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")

// handleCtrlC undoes the ignoring of ctrl+c this process may have from a
// parent in a new process group, so the programs it starts, which inherit
// it, take ctrl+c from their terminal.
func handleCtrlC() { _, _, _ = setConsoleCtrlHandler.Call(0, 0) }
