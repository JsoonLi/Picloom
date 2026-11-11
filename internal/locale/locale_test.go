package locale

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

// TestEveryTextIsTranslated fails when code says a text through T or Error
// that has no English translation, so no screen ever mixes languages.
func TestEveryTextIsTranslated(t *testing.T) {
	root := filepath.Join("..", "..")
	var files []string
	for _, dir := range []string{".", "internal"} {
		filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && path != filepath.Join(root, dir) && (dir == "." || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				files = append(files, path)
			}
			return nil
		})
	}
	used := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "T" && sel.Sel.Name != "Error" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "locale" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, _ := strconv.Unquote(lit.Value)
			used[text] = true
			if _, ok := english[text]; !ok {
				t.Errorf("%s: no English for %q", fset.Position(lit.Pos()), text)
			}
			return true
		})
	}
	if len(used) < 300 {
		t.Fatalf("found only %d texts; the scan missed the sources", len(used))
	}
	for text := range english {
		if !used[text] {
			t.Errorf("unused English entry %q", text)
		}
	}
}

// TestTranslationsKeepFormatVerbs keeps every %d, %s and %w of a text in
// its translation, in the same order.
func TestTranslationsKeepFormatVerbs(t *testing.T) {
	verbs := func(s string) string {
		var out []string
		for i := 0; i < len(s); i++ {
			if s[i] != '%' || i+1 >= len(s) {
				continue
			}
			j := i + 1
			for j < len(s) && strings.IndexByte("[]0123456789.", s[j]) >= 0 {
				j++
			}
			if j < len(s) {
				out = append(out, s[i:j+1])
			}
			i = j
		}
		return strings.Join(out, " ")
	}
	for source, translated := range english {
		if verbs(source) != verbs(translated) {
			t.Errorf("format verbs differ: %q → %q", source, translated)
		}
	}
}

func TestLanguageFollowsSetting(t *testing.T) {
	t.Cleanup(func() { Set(Chinese) })
	Set("en-GB")
	if Current() != English || T("设置") != "Settings" || Error("数据存储已关闭。").Error() != "Storage is closed." {
		t.Fatal("English tags must say English")
	}
	if T("未翻译的文字") != "未翻译的文字" {
		t.Fatal("a missing translation must fall back to the source")
	}
	Set("zh-Hant-TW")
	if Current() != Chinese || T("设置") != "设置" {
		t.Fatal("other languages must say the Chinese source")
	}
}
