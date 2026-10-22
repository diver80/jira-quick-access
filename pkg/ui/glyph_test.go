package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/image/font/sfnt"
)

// TestDrawnTextHasGlyphs guards against characters the embedded Inter font lacks,
// which render as empty boxes (e.g. "✕", "⚙"). It checks every non-ASCII rune in
// string literals of the UI package against both font weights.
func TestDrawnTextHasGlyphs(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/gogpu/ui").Output()
	if err != nil {
		t.Skipf("cannot locate gogpu/ui: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "internal", "render", "fonts")
	var fonts []*sfnt.Font
	for _, name := range []string{"Inter-Regular.ttf", "Inter-Bold.ttf"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Skipf("font not in module cache: %v", err)
		}
		f, err := sfnt.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		fonts = append(fonts, f)
	}

	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	var buf sfnt.Buffer
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, r := range s {
				if r < 128 {
					continue
				}
				for _, font := range fonts {
					if i, _ := font.GlyphIndex(&buf, r); i == 0 {
						t.Errorf("%s: %q (U+%04X) is not in the UI font", fset.Position(lit.Pos()), r, r)
						break
					}
				}
			}
			return true
		})
	}
}
