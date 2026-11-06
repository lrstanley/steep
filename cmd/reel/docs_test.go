// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

// TestDocsCoverCommands fails when a command or flag isn't in the
// documentation's CLI section. Flags inherited from a shared group are
// documented once, on the features and reference pages.
func TestDocsCoverCommands(t *testing.T) {
	var cli struct {
		Flags `embed:""`
	}
	p, err := kong.New(&cli, kong.Vars{"spec": specHelp}, kong.Exit(func(int) { t.Fatal("exited") }))
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Join("..", "..", "docs", "content", "cli")
	var all strings.Builder
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".mdx" {
			return err
		}
		b, err := os.ReadFile(path)
		all.Write(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	text := all.String()

	var walk func(prefix string, node *kong.Node)
	walk = func(prefix string, node *kong.Node) {
		for _, flag := range node.Flags {
			if flag.Hidden || flag.Name == "help" {
				continue
			}
			if !regexp.MustCompile(`--(\[no-\])?` + flag.Name + `([^-a-z]|$)`).MatchString(text) {
				t.Errorf("%s --%s isn't in docs/content/cli", prefix, flag.Name)
			}
		}
		for _, child := range node.Children {
			if child.Hidden {
				continue
			}
			name := strings.TrimSpace(prefix + " " + child.Name)
			page := filepath.Join(root, "commands", strings.Fields(name)[0]+".mdx")
			b, readErr := os.ReadFile(page)
			if readErr != nil {
				t.Errorf("%s has no page: %v", name, readErr)
			} else if !strings.Contains(string(b), child.Name) {
				t.Errorf("%s isn't in %s", name, page)
			}
			walk(name, child)
		}
	}
	walk("", p.Model.Node)
}
