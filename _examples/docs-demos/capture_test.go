// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !docsfailures

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCaptureFailures runs each test of failures_test.go in a "go test" of its
// own, and keeps its output for the failure pages, with paths made relative
// or stable and the times removed, so regenerating it changes nothing.
func TestCaptureFailures(t *testing.T) {
	if !exporting() {
		t.Skip("exporting is off")
	}
	root, rootErr := filepath.Abs("../..")
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	times := regexp.MustCompile(`\(\d+\.\d+s\)|\t\d+\.\d+s$`)
	temps := regexp.MustCompile(`/tmp/Test\w+/\d+`)
	for _, name := range []string{
		"TestFailAssert", "TestFailRequire", "TestFailWait", "TestFailSettle", "TestFailSnapshot",
		"TestFailSnapshotMissing", "TestFailExecProgram", "TestFailLocate",
		"TestFailBadExport", "TestExportRemoved",
	} {
		args := []string{"test", "-tags", "docsfailures", "-count", "1", "-run", "^" + name + "$", "."}
		if name == "TestExportRemoved" {
			args = append(args, "-v")
		}
		cmd := exec.Command("go", args...)
		cmd.Env = append(os.Environ(), "NO_COLOR=1")
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()

		// Failing is the point of most of them, but it must be the test that
		// fails, rather than the build, so a broken run can't replace a page's
		// output.
		var exit *exec.ExitError
		switch {
		case name == "TestExportRemoved":
			if err != nil {
				t.Fatalf("%s: %v\n%s", name, err, out.String())
			}
		case !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(out.String(), "--- FAIL: "+name):
			t.Fatalf("%s should fail: %v\n%s", name, err, out.String())
		}

		var lines []string
		for line := range strings.Lines(out.String()) {
			line = strings.TrimRight(line, "\n")
			line = strings.TrimRight(times.ReplaceAllString(line, ""), " \t")
			line = temps.ReplaceAllString(line, "/tmp/go-test")
			line = strings.ReplaceAll(line, root+string(filepath.Separator), "")
			lines = append(lines, line)
		}
		path := filepath.Join(fixtureDir, name+".output")
		if err = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil { //nolint:gosec // Part of the site.
			t.Fatal(fmt.Errorf("%s: %w", name, err))
		}
	}
}
