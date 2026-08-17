package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoNewlinesInsideStyledRender guards spec §2.2. lipgloss pads every line
// of a multi-line block to the width of its widest line, so a newline inside
// Render() silently indents whatever is written next. Style the text; emit
// newlines outside.
func TestNoNewlinesInsideStyledRender(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Render" {
				return true
			}
			for _, arg := range call.Args {
				literal, ok := arg.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				if strings.Contains(value, "\n") {
					t.Errorf("%s: Render() argument contains a newline: %q\n"+
						"lipgloss pads every line of a block to its widest line, "+
						"which indents the next write. Style the text; emit newlines outside.",
						fset.Position(literal.Pos()), value)
				}
			}
			return true
		})
	}
}

func TestStylesAreDefined(t *testing.T) {
	if styleHeading.Render("x") == "" {
		t.Error("styleHeading must render")
	}
	if styleSelected.Render("x") == "" {
		t.Error("styleSelected must render")
	}
}
