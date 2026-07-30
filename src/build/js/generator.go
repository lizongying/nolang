// Package js 提供 Nolang 的 JavaScript 後端：直接從 *parser.Program 發射
// JavaScript 原始碼字串，並在生成階段執行類型擦除（type erasure）。
//
// 因為 JS 為動態型別，所有 Nolang 的型別標註（int/str/bool/vec[T]/[N]T/?T 等）
// 在 JS 輸出中完全不保留，僅生成執行時行為。
//
// 設計參考：
//   - src/build/no/generator.go：同為「AST → 文字」的 codegen-only Generator
//   - src/build/wasm/generator.go：Direct WASM 後端的 SetTargetPlatform 機制
//   - src/build/llvm/generator.go：FilterByPlatform 平台過濾機制
package js

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// Generator 從 Nolang AST 發射 JavaScript 原始碼。
// 生成過程執行類型擦除——所有 Nolang 型別標註在 JS 輸出中不保留。
type Generator struct {
	// targetGoos / targetGoarch 用於平台變體過濾（#{linux-amd64}、#{js} 等），
	// 與 llvm.Generator 的對應欄位語義一致。
	// JS 後端預設為 ("js", "")。
	targetGoos   string
	targetGoarch string

	// targetEnv 控制執行環境："node"（預設）或 "browser"。
	// "browser" 時注入 console 重導向 prelude 並改用 shim 取代 process.*。
	targetEnv string

	// 縮排層級（每層一個 tab）
	indentLevel int

	// declaredVars 記錄目前作用域已宣告的變數名，用於決定 let vs 純賦值。
	declaredVars map[string]bool

	// inFunctionBody 標記是否在函式主體內（影響 return 處理）
	inFunctionBody bool

	// currentResults 記錄當前函式/方法的具名 out-params（Results），
	// 供 bare return 與函式結尾自動 return 使用。
	currentResults []*parser.Parameter

	// methodsByReceiver 收集每個 struct 的方法定義（key = type name, part before dot in fd.Name）。
	// 在 Generate phase 1 前填充，供 generateStructDefinition 在 class body 內發射方法。
	methodsByReceiver map[string][]*parser.FunctionDefinition

	// globalVars 記錄頂層宣告的全域變數名（原始 Nolang 名稱），
	// 用於在函式內部避免以 let 重新宣告全域變數。
	globalVars map[string]bool

	// out 為輸出緩衝；Generate 入口會重置
	out *strings.Builder
}

// NewGenerator 建立一個預設目標為 JS 平台的 Generator。
func NewGenerator() *Generator {
	return &Generator{
		targetGoos:        "js",
		targetGoarch:      "",
		targetEnv:         "node",
		declaredVars:      make(map[string]bool),
		methodsByReceiver: make(map[string][]*parser.FunctionDefinition),
		globalVars:        make(map[string]bool),
	}
}

// SetTargetPlatform 設定編譯目標平台，用於平台變體過濾。
// JS 後端通常為 ("js", "")；呼叫者可傳入其他值以進行交叉過濾。
func (g *Generator) SetTargetPlatform(goos, goarch string) {
	g.targetGoos = goos
	g.targetGoarch = goarch
}

// SetTargetEnv 設定執行環境目標："node"（預設）或 "browser"。
// "browser" 時同時將平台設為 ("js", "browser")，使 #{js-browser} 註解被保留。
func (g *Generator) SetTargetEnv(env string) {
	g.targetEnv = env
	if env == "browser" {
		g.targetGoos = "js"
		g.targetGoarch = "browser"
	}
}

// indent 回傳目前縮排層級對應的字串（tab）。
func (g *Generator) indent() string {
	return strings.Repeat("\t", g.indentLevel)
}

// writeLine 寫入一行（含縮排與換行）。
func (g *Generator) writeLine(s string) {
	g.out.WriteString(g.indent())
	g.out.WriteString(s)
	g.out.WriteByte('\n')
}

// writeRaw 寫入原始字串（不加縮排與換行）。
func (g *Generator) writeRaw(s string) {
	g.out.WriteString(s)
}

// Generate 是 JS 後端入口：從 *parser.Program 發射 JS 原始碼字串。
//
// 流程：
//  1. 平台過濾——依 targetGoos/targetGoarch 過濾 #{linux-amd64} 等標註
//  2. 先輸出所有 struct 定義（→ JS class）
//  3. 再輸出所有 function 定義（→ JS function）
//  4. 最後輸出頂層語句（直接執行，不包裝 main）
func (g *Generator) Generate(program *parser.Program) (string, error) {
	if program == nil {
		return "", fmt.Errorf("js.Generator.Generate: program is nil")
	}

	g.out = &strings.Builder{}
	g.declaredVars = make(map[string]bool)
	g.globalVars = make(map[string]bool)
	g.indentLevel = 0
	g.inFunctionBody = false
	g.currentResults = nil
	g.methodsByReceiver = make(map[string][]*parser.FunctionDefinition)

	// 0. runtime helpers：在所有輸出之前注入 Nolang 運行時輔助函數。
	g.writeRaw(runtimeHelpers)

	// 0b. browser prelude：在 browser 模式下注入 console 重導向至 #nolang-output。
	if g.targetEnv == "browser" {
		g.writeRaw(browserPrelude)
	}

	// 1. 平台過濾
	stmts := filterByPlatform(program.Sem, program.Statements, g.targetGoos, g.targetGoarch)

	// 1b. 收集方法定義（key = receiver type name），供 generateStructDefinition 在 class body 內發射。
	for _, stmt := range stmts {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || !fd.IsMethodDef {
			continue
		}
		dotIdx := strings.Index(fd.Name, ".")
		if dotIdx < 0 {
			continue
		}
		typeName := fd.Name[:dotIdx]
		g.methodsByReceiver[typeName] = append(g.methodsByReceiver[typeName], fd)
	}

	// 1c. 收集全域變數名（頂層 let 敘述），供函式內部避免以 let 重新宣告。
	for _, stmt := range stmts {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			g.globalVars[ls.Name.Value] = true
		}
	}

	// 2. 輸出所有 struct 定義（→ JS class）
	for _, stmt := range stmts {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			g.generateStructDefinition(sd)
			g.writeRaw("\n")
		}
	}

	// 3. 輸出所有 function 定義（→ JS function）
	for _, stmt := range stmts {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			g.generateFunctionDefinition(fd)
			g.writeRaw("\n")
		}
	}

	// 4. 輸出頂層語句（直接執行）
	for _, stmt := range stmts {
		switch stmt.(type) {
		case *parser.FunctionDefinition, *parser.StructDefinition,
			*parser.InterfaceDefinition, *parser.EnumDefinition,
			*parser.UseStatement, *parser.ExportStatement,
			*parser.ExternStatement, *parser.AnnotationStatement,
			*parser.TypeAlias, *parser.TaggedEnumDefinition:
			continue
		}
		g.generateStatement(stmt)
	}

	return g.out.String(), nil
}

// ---- 平台過濾（自包含實作，避免 js → llvm 的循環依賴） ----

// platformKeys maps flattened platform annotation keys to (goos, goarch) pairs.
// 與 llvm.platformKeys 同步——此處為副本以避免循環依賴。
var platformKeys = map[string]struct{ goos, goarch string }{
	"linux-amd64": {"linux", "amd64"},
	"linux-arm64": {"linux", "arm64"},
	"win-amd64":   {"windows", "amd64"},
	"win-arm64":   {"windows", "arm64"},
	"mac-amd64":   {"darwin", "amd64"},
	"mac-arm64":   {"darwin", "arm64"},
	"wasi-wasm32": {"wasi", "wasm32"},
	"js":          {"js", ""},
	"js-browser":  {"js", "browser"},
}

// runtimeHelpers 提供 Nolang → JS 編譯所需的運行時輔助函數。
// 這些函數在所有輸出之前注入，無論目標環境是 node 還是 browser。
const runtimeHelpers = `// Nolang runtime helpers
function __nsub(a, b) {
    if (typeof a === 'number' && typeof b === 'number') return a - b;
    return String(a) + String(b);
}
`

// browserPrelude 在 browser 模式下注入於所有輸出之前，將 console.log/error
// 重導向至 id="nolang-output" 的 DOM 元素。
const browserPrelude = `// Nolang browser prelude
(function() {
    const output = document.getElementById('nolang-output');
    if (output) {
        const origLog = console.log;
        const origErr = console.error;
        console.log = function(...args) {
            origLog.apply(console, args);
            const line = document.createElement('div');
            line.textContent = args.map(a => typeof a === 'object' ? JSON.stringify(a) : String(a)).join(' ');
            output.appendChild(line);
        };
        console.error = function(...args) {
            origErr.apply(console, args);
            const line = document.createElement('div');
            line.style.color = 'red';
            line.textContent = args.map(a => String(a)).join(' ');
            output.appendChild(line);
        };
    }
})();
`

// stmtAnnotations 從敘述中抽取平台註解（經由 side-table）。
func stmtAnnotations(sem *parser.SemanticContext, stmt parser.Statement) []*parser.AnnotationEntry {
	switch stmt.(type) {
	case *parser.LetStatement, *parser.FunctionDefinition, *parser.StructDefinition, *parser.ExpressionStatement:
		return sem.AnnotationsOf(stmt)
	}
	return nil
}

// matchesPlatform 回傳 true 若任一平台註解 key 匹配目標 (goos, goarch)。
// 無平台註解 → 一律保留。
// matcher.goarch 為空時（如 "js"）→ 僅比對 goos。
func matchesPlatform(annotations []*parser.AnnotationEntry, goos, goarch string) bool {
	if len(annotations) == 0 {
		return true
	}
	hasPlatform := false
	for _, entry := range annotations {
		if entry.Value != nil {
			continue
		}
		matcher, isPlatform := platformKeys[entry.Key]
		if !isPlatform {
			continue
		}
		hasPlatform = true
		if goos != matcher.goos {
			continue
		}
		// goarch 為空時（如 "js"）僅比對 goos；否則兩者都須匹配
		if matcher.goarch == "" || goarch == matcher.goarch {
			return true
		}
	}
	return !hasPlatform
}

// filterByPlatform 移除平台註解不匹配目標的敘述。
func filterByPlatform(sem *parser.SemanticContext, stmts []parser.Statement, goos, goarch string) []parser.Statement {
	filtered := make([]parser.Statement, 0, len(stmts))
	for _, stmt := range stmts {
		if matchesPlatform(stmtAnnotations(sem, stmt), goos, goarch) {
			filtered = append(filtered, stmt)
		}
	}
	return filtered
}
