// Package srcscan 找出原始碼裡的中文字串,並分辨哪些已經接上語系目錄。
//
// 掃描邏輯只有這一份:tools/i18nscan 用它產生改寫計畫,internal/i18n
// 的測試用它檢查目錄裡有沒有原始碼已經不存在的 key。兩邊各寫一份的話,
// 「工具說接上了、測試說沒有」這種矛盾遲早會出現,而那時沒有人知道
// 該信哪一邊。
package srcscan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// Hit 是一條中文字串字面值。
type Hit struct {
	Pos  token.Position
	End  int
	Text string
	// Done 表示它已經被 i18n.T / Sprintf / Errorf 包起來。
	Done bool
	// Kind 是改寫分類:auto 能自動包,skip:* 要人工看。
	Kind string
}

// SkipDirs 是掃描時不進去的目錄:產物、快取、原版素材與測試資料
// 都不是這個程式的介面文字。
var SkipDirs = map[string]bool{
	".git": true, "dist": true, "dist-all": true, ".cache": true,
	"original": true, "android": true, "testdata": true,
}

// Walk 掃一個目錄樹。解不開的檔案跳過 —— 掃描不該因為一個檔壞掉而停。
func Walk(root string) ([]Hit, error) {
	fset := token.NewFileSet()
	var out []Hit
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if SkipDirs[fi.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return nil
		}
		out = append(out, scan(fset, f)...)
		return nil
	})
	return out, err
}

// wrapped 是把字串送進語系目錄的那幾個函式。
var wrapped = map[string]bool{"T": true, "Sprintf": true, "Errorf": true}

type hit struct {
	pos  token.Position
	text string
	done bool
	// kind 是改寫分類:auto 能自動包,skip:* 要人工看(理由見上面的說明)。
	kind string
	end  int
}

func scan(fset *token.FileSet, f *ast.File) []Hit {
	var out []Hit
	// 先記下所有「被包起來的字串字面值」,再走一次全部字面值 ——
	// 兩趟比在一趟裡維護 parent 堆疊簡單,而且不會漏掉巢狀的情況。
	done := map[ast.Node]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		name := ""
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok && x.Name == "i18n" {
				name = fn.Sel.Name
			}
		case *ast.Ident:
			name = fn.Name
		}
		if wrapped[name] {
			done[call.Args[0]] = true
		}
		return true
	})
	// 第二趟維護一個祖先堆疊,才判斷得出這條字串長在什麼位置上。
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		defer func() { stack = append(stack, n) }()
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil || !hasCJK(s) {
			return true
		}
		out = append(out, Hit{
			Pos: fset.Position(lit.Pos()), End: fset.Position(lit.End()).Offset,
			Text: s, Done: done[ast.Node(lit)], Kind: classify(stack, lit),
		})
		return true
	})
	return out
}

// classify 看這條字串長在什麼位置上,決定能不能自動包。
func classify(stack []ast.Node, lit *ast.BasicLit) string {
	for i := len(stack) - 1; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.CaseClause:
			for _, e := range p.List {
				if containsLit(e, lit) {
					return "skip:case"
				}
			}
		case *ast.KeyValueExpr:
			if p.Key == ast.Expr(lit) {
				return "skip:key"
			}
		case *ast.GenDecl:
			if p.Tok == token.CONST {
				return "skip:const"
			}
			if p.Tok == token.VAR {
				// 函式內的 var 每次進函式都重新求值,沒有凍住的問題;
				// 套件層級的才有。看堆疊上有沒有 FuncDecl 就分得出來。
				for _, n := range stack {
					if _, ok := n.(*ast.FuncDecl); ok {
						return "auto"
					}
				}
				return "skip:var"
			}
		}
	}
	return "auto"
}

func containsLit(e ast.Expr, lit *ast.BasicLit) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if n == ast.Node(lit) {
			found = true
		}
		return !found
	})
	return found
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) {
			return true
		}
	}
	return false
}
