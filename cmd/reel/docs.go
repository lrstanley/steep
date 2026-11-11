// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"strings"

	"github.com/alecthomas/kong"
)

// docsURL is where the documentation is published.
const docsURL = "https://getsteep.dev"

// commandDocs returns the docs paths, under docsURL, for cmd: the guide its
// top-level command's guide tag names, if any, then its reference page,
// /cli/commands/<command>, or for a subcommand, its section there.
func commandDocs(cmd *kong.Node) []string {
	top := cmd
	for top.Parent != nil && top.Parent.Type == kong.CommandNode {
		top = top.Parent
	}
	var docs []string
	if guide := top.Tag.Get("guide"); guide != "" {
		docs = append(docs, guide)
	}
	ref := "/cli/commands/" + top.Name
	if cmd != top {
		ref += "#" + cmd.Name
	}
	return append(docs, ref)
}

// withDocsLinks adds each command's documentation links to its --help.
func withDocsLinks() kong.Option {
	return kong.PostBuild(func(k *kong.Kong) error {
		var walk func(node *kong.Node)
		walk = func(node *kong.Node) {
			for _, child := range node.Children {
				if child.Type != kong.CommandNode || child.Hidden {
					continue
				}
				child.Detail = docsDetail(child.Detail, commandDocs(child))
				walk(child)
			}
		}
		walk(k.Model.Node)
		return nil
	})
}

// docsDetail appends a block of docs links to a command's detailed help.
// The links are indented so that kong prints them as they are, unwrapped.
func docsDetail(detail string, paths []string) string {
	var b strings.Builder
	if detail != "" {
		b.WriteString(detail)
		b.WriteString("\n\n")
	}
	b.WriteString("Documentation:\n\n")
	for _, p := range paths {
		b.WriteString("    " + docsURL + p + "\n")
	}
	return b.String()
}
