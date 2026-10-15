package mmast

import (
	"fmt"
	"strings"
	"sync"

	pkgspec "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// 進程內的「預編 AST 模組」註冊表：依賴鍵 / 模組名 → 已解出的 *Program。
//
// 這是消費側的接縫：`no add nogit@github.com/lizongying/notools` 之後，編譯器遇到
// `use nogit` 時不再去 disk 找原始碼，而是從這裡取回 <tool>.ast.zip 解出的 AST。
// 之所以放在 mmast（而不是 package/parser）：package 被 parser 依賴、parser 又需要
// package，只有 mmast 能同時握住兩者而不成環。
var (
	moduleASTs sync.Map // key -> *parser.Program
)

// RegisterModuleAST 註冊一個模組的 AST（key 為依賴鍵，同時可用模組名取用）。
func RegisterModuleAST(key string, prog *parser.Program) {
	if prog == nil || key == "" {
		return
	}
	moduleASTs.Store(key, prog)
	if i := strings.LastIndex(key, "@"); i > 0 {
		moduleASTs.Store(key[:i], prog) // "nogit@github.com/o/r" → 也註冊成 "nogit"
	}
}

// LookupModuleAST 依依賴鍵或模組名取回已註冊的 AST。
func LookupModuleAST(name string) (*parser.Program, bool) {
	if name == "" {
		return nil, false
	}
	v, ok := moduleASTs.Load(name)
	if !ok {
		return nil, false
	}
	prog, _ := v.(*parser.Program)
	return prog, prog != nil
}

// EnsureModuleAST 確保依賴鍵對應的 AST 已就緒：先查進程內註冊表，未命中就抓
// <tool>.ast.zip（含磁碟快取）並解碼。version 為空會報錯——依賴版本必須是具體的。
func EnsureModuleAST(depKey, version string) (*parser.Program, error) {
	if prog, ok := LookupModuleAST(depKey); ok {
		return prog, nil
	}
	spec, err := pkgspec.ParseToolSpec(depKey)
	if err != nil {
		return nil, fmt.Errorf("mmast: bad dependency key %q: %w", depKey, err)
	}
	if version == "" || version == "*" {
		return nil, fmt.Errorf("mmast: dependency %s has no concrete version (got %q)", depKey, version)
	}
	prog, _, err := FetchASTZip(spec, version)
	if err != nil {
		return nil, fmt.Errorf("mmast: loading %s (%s): %w", depKey, version, err)
	}
	RegisterModuleAST(depKey, prog)
	return prog, nil
}
