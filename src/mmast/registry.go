package mmast

import (
	"fmt"
	"strings"
	"sync"

	pkgspec "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// 進程內的「預編 AST 包」註冊表：依賴鍵 → 一個包的多個模組（map[modulePath]*Program）。
//
// 這是消費側的接縫：`no add nogit@github.com/lizongying/notools` 之後，編譯器遇到
// `use nogit/remote`（直接取模組）或 `# nogit.greet`（按函式定位模組）時不再去
// disk 找原始碼，而是從這裡取回 <tool>-ast.zip 解出的對應 AST。
// 之所以放在 mmast（而不是 package/parser）：package 被 parser 依賴、parser 又需要
// package，只有 mmast 能同時握住兩者而不成環。
var (
	moduleASTs sync.Map // key -> *ASTZip（Modules: map[modulePath]*Program）
)

// RegisterModuleAST 註冊一個包的全部模組（key 為依賴鍵，同時可用工具名取用）。
func RegisterModuleAST(key string, set *ASTZip) {
	if set == nil || key == "" {
		return
	}
	moduleASTs.Store(key, set)
	if i := strings.LastIndex(key, "@"); i > 0 {
		moduleASTs.Store(key[:i], set) // "nogit@github.com/o/r" → 也註冊成 "nogit"
	}
}

// LookupModuleAST 依依賴鍵或工具名 + 模組路徑取回已註冊的 AST。
func LookupModuleAST(name, module string) (*parser.Program, bool) {
	if name == "" {
		return nil, false
	}
	v, ok := moduleASTs.Load(name)
	if !ok {
		return nil, false
	}
	set, _ := v.(*ASTZip)
	if set == nil {
		return nil, false
	}
	prog, ok := set.Modules[module]
	return prog, ok && prog != nil
}

// EnsurePackageAST 確保依賴鍵對應的**整包**已就緒：先查進程內註冊表，未命中
// 就抓 <tool>-ast.zip（含磁碟快取）並解碼所有模組。
// 對應 `# nogit` 這種「只寫包名、用 nogit.<module>.<fn> 取用」的導入形式。
func EnsurePackageAST(depKey, version string) (*ASTZip, error) {
	return ensureModuleSet(depKey, version)
}

// EnsureModuleAST 確保依賴鍵對應的**指定模組**已就緒：先查進程內註冊表，未命中
// 就抓 <tool>-ast.zip（含磁碟快取）並解碼。version 為空會報錯——依賴版本必須是具體的。
// module 為 "remote" 等，對應 `# nogit/remote`。
func EnsureModuleAST(depKey, version, module string) (*parser.Program, error) {
	set, err := ensureModuleSet(depKey, version)
	if err != nil {
		return nil, err
	}
	prog, ok := set.Modules[module]
	if !ok || prog == nil {
		return nil, fmt.Errorf("mmast: dependency %s has no module %q (available: %s)",
			depKey, module, strings.Join(set.ModulePaths(), ", "))
	}
	return prog, nil
}

// EnsureModuleASTForFunction 對應 `# nogit.greet`：<pkg> 只是包名，在包內所有
// 模組中找定義了函式/常量 fn 的模組。找不到或多個模組同名都明確報錯，
// 不回退到某個「入口模組」——容器裡根本沒有入口這個概念。
func EnsureModuleASTForFunction(depKey, version, fn string) (*parser.Program, error) {
	set, err := ensureModuleSet(depKey, version)
	if err != nil {
		return nil, err
	}
	prog, err := set.FindModuleByFunction(fn)
	if err != nil {
		return nil, fmt.Errorf("mmast: dependency %s: %w", depKey, err)
	}
	return prog, nil
}

// ensureModuleSet 查註冊表，未命中就抓包並註冊整包（一次下載註冊全部模組）。
func ensureModuleSet(depKey, version string) (*ASTZip, error) {
	if v, ok := moduleASTs.Load(depKey); ok {
		if set, _ := v.(*ASTZip); set != nil {
			return set, nil
		}
	}
	spec, err := pkgspec.ParseToolSpec(depKey)
	if err != nil {
		return nil, fmt.Errorf("mmast: bad dependency key %q: %w", depKey, err)
	}
	if version == "" || version == "*" {
		return nil, fmt.Errorf("mmast: dependency %s has no concrete version (got %q)", depKey, version)
	}
	set, err := FetchModuleSet(spec, version)
	if err != nil {
		return nil, fmt.Errorf("mmast: loading %s (%s): %w", depKey, version, err)
	}
	RegisterModuleAST(depKey, set)
	return set, nil
}
