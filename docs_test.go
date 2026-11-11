// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"getsteep.dev/steep/reel"
)

// docsText returns the text of the documentation's pages under dir.
func docsText(t *testing.T, dir string) string {
	t.Helper()
	var text strings.Builder
	err := filepath.WalkDir(filepath.Join("docs", "content", dir), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".mdx" {
			return err
		}
		b, err := os.ReadFile(path)
		text.Write(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return text.String()
}

// TestDocsCoverAPI fails when a harness option or method isn't in steep's
// documentation, or an environment variable isn't on its reference page, or
// when that page names one that no longer exists.
func TestDocsCoverAPI(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { //nolint:staticcheck // The package's own files, by name.
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	env := map[string]bool{}
	envName := regexp.MustCompile(`"(STEEP_[A-Z_]+)"`)
	for _, file := range pkgs["steep"].Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			isOption := fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "With")
			if isOption || receiver(fn) == "Harness" {
				names = append(names, fn.Name.Name)
			}
		}
		src, readErr := os.ReadFile(fset.Position(file.Pos()).Filename)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, m := range envName.FindAllStringSubmatch(string(src), -1) {
			env[m[1]] = true
		}
	}

	section := docsText(t, "steep")
	for _, name := range names {
		if !regexp.MustCompile(`\b` + name + `\b`).MatchString(section) {
			t.Errorf("%s isn't in docs/content/steep", name)
		}
	}
	page, err := os.ReadFile(filepath.Join("docs", "content", "steep", "environment.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	reference := string(page)
	for name := range env {
		if !strings.Contains(reference, "`"+name+"`") {
			t.Errorf("%s isn't in docs/content/steep/environment.mdx", name)
		}
	}
	for _, m := range regexp.MustCompile("`(STEEP_[A-Z_]+)[`=]").FindAllStringSubmatch(reference, -1) {
		if !env[m[1]] {
			t.Errorf("docs/content/steep/environment.mdx names %s, which steep doesn't read", m[1])
		}
	}
}

// receiver returns the name of the type a method is on.
func receiver(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// TestDocsExportFormats fails when the export configurator offers an output
// that reel doesn't accept.
func TestDocsExportFormats(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("docs", "src", "lib", "export-formats.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Formats []struct {
			Ext    string `json:"ext"`
			Stills bool   `json:"stills"`
			Params []struct {
				Name    string   `json:"name"`
				Options []string `json:"options"`
			} `json:"params"`
		} `json:"formats"`
	}
	if err = json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Formats) == 0 {
		t.Fatal("no formats")
	}
	for _, f := range data.Formats {
		outputs := []string{"out." + f.Ext}
		for _, p := range f.Params {
			for _, option := range p.Options {
				outputs = append(outputs, fmt.Sprintf("out.%s?%s=%s", f.Ext, p.Name, option))
			}
		}
		if f.Stills {
			outputs = append(outputs, fmt.Sprintf("out_{mark}.%s?match=input:*", f.Ext))
		}
		for _, output := range outputs {
			if _, err = reel.ParseFile(output); err != nil {
				t.Errorf("%s: %v", output, err)
			}
		}
	}
}
