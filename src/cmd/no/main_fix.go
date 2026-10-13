package main

// 本檔案實作 `no fmt --fix=main`：把可省略的 `main = () { … }` 包裹去掉，
// 讓 main 區塊內的陳述直接升到頂層（Nolang 中 main 可省略，頂層陳述即程式入口主體）。
//
// 修復形態：
//
//	main = () {
//	    a()
//	    print('{a}')
//	}
//	main()
//	=>
//	a()
//	print('{a}')
//
// 與既有 fix 類同風格：以「磁碟檔案自身」為分析來源、手術式文字删除（只刪除
// `main = () {` 首行與其配對 `}` 末行，區塊內每一行原樣保留——註解、空行、縮排都不動），
// 刪除後再走一次正常 `no fmt` 渲染，讓縮排/空行自動歸位。重跑冪等：已升頂層的檔案
// 不再含可省略 main，第二次跑不會再改動。
//
// 顯式調用形：部分檔案寫成 `main = () {…}` 後在頂層再呼叫 `main()`（手動觸發入口）。
// 主體升頂層後會自動執行，該 `main()` 呼叫即冗餘——一併刪除。為安全起見，只有當
// 全檔 `main` 識別字出現次數恰為「(被升頂層的 main 定義數) + (可刪除的頂層 main() 呼叫數)」
// 時才動手；若 main 另有其它引用（被當值傳遞、帶參呼叫、方法調用等）即整體跳過，
// 絕不留下指向已消失定義的懸空呼叫。
//
// 保守閘門（任一不滿足即跳過該 main／檔案，不動檔案）：
//   - 頂層 FunctionDefinition、名字恰為 `main`、非方法定義；
//   - 無參數、無結果參數（`main = () (r i64) {…}` 這種有返回值的不可省）；
//   - 區塊形 `{ … }`（非行內單陳述），首行裁剪後為 `main = () {`、末行裁剪後為 `}`；
//   - main 主體內「直接」不含 return（嵌套函式字面量／函式定義內的 return 屬其自身，
//     不計入——那些節點升頂層後仍是嵌套節點，語義不變）。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	nfmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// fixMainFixes 收集單檔/目錄下所有 `--fix=main` 的修復結果（絕對檔名 -> 修復後源碼）。
func fixMainFixes(arg string) map[string]string {
	info, err := os.Stat(arg)
	if err != nil {
		return nil
	}
	files := []string{}
	if info.IsDir() {
		walkErr := filepath.Walk(arg, func(path string, fi os.FileInfo, werr error) error {
			if werr != nil || fi == nil || fi.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".no") {
				files = append(files, path)
			}
			return nil
		})
		if walkErr != nil {
			return nil
		}
	} else {
		files = append(files, arg)
	}
	sort.Strings(files)
	fixes := map[string]string{}
	for _, f := range files {
		fixed, ok := fixMainInFile(f)
		if !ok {
			continue
		}
		abs, aerr := filepath.Abs(f)
		if aerr != nil {
			abs = f
		}
		fixes[filepath.Clean(abs)] = fixed
	}
	return fixes
}

// fixMainInFile 讀入單檔（磁碟內容），把每個可省略的 `main = () {…}` 包裹去掉並重新
// 格式化；無可省 main 或改動後無法再解析時回傳 ok=false（不動檔案）。
func fixMainInFile(filename string) (string, bool) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", false
	}
	src := string(data)
	program := parseProgramForFmt(src)
	if program == nil {
		return "", false
	}

	lines := strings.Split(src, "\n")
	// (1) 收集可升頂層的 main 定義及其包裹行跨度（首行 `main = () {` + 配對 `}`）。
	type span struct{ start, end int }
	var hoisted []span
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || !isOmittableMain(fd) {
			continue
		}
		start := fd.Pos().Line
		end := fd.Body.RBrace.Line // 配對 `}` 所在行
		if start < 1 || end < start || end > len(lines) {
			continue
		}
		// 閘門：首行裁剪後必須恰為 `main = () {`、末行裁剪後必須恰為 `}`，
		// 否則說明写法非規範區塊形（行內、後同行有註解等），保守跳過。
		if strings.TrimSpace(lines[start-1]) != "main = () {" ||
			strings.TrimSpace(lines[end-1]) != "}" {
			continue
		}
		// 閘門：main 主體直接含 return → 升頂層後 return 非法，跳過。
		if blockHasDirectReturn(fd.Body) {
			continue
		}
		hoisted = append(hoisted, span{start, end})
	}
	if len(hoisted) == 0 {
		return "", false
	}

	// (2) 收集冗餘的頂層 `main()` 呼叫（顯式調用形：主體升頂層後自動執行，該呼叫多餘）。
	var calls []span
	for _, stmt := range program.Statements {
		if s, e, ok := topLevelMainCall(stmt); ok {
			if s >= 1 && e <= len(lines) && e >= s {
				calls = append(calls, span{s, e})
			}
		}
	}

	// (3) 安全閘門：全檔 `main` 識別字出現次數必須恰為「升頂層定義數 + 冗餘呼叫數」。
	//     每個被升頂層的 `main = () {` 貢獻一個 IDENT `main`、每個 `main()` 貢獻一個；
	//     字串/註解中的 `main` 不是 IDENT、不計入。若 actual 更大，代碼中另有不可刪除的
	//     main 引用（被當值傳、帶參呼叫、方法調用等），整體跳過——絕不留懸空呼叫。
	if countMainIdentTokens(src) != len(hoisted)+len(calls) {
		return "", false
	}

	// (4) 待刪除行號：包裹行 + 區塊內每一行（含註解/空行/縮排）原樣保留 + 冗餘呼叫行。
	drop := map[int]bool{}
	for _, sp := range hoisted {
		drop[sp.start] = true
		drop[sp.end] = true
	}
	for _, sp := range calls {
		for ln := sp.start; ln <= sp.end; ln++ {
			drop[ln] = true
		}
	}

	// 手術式删除：只丟掉包裹行與冗餘呼叫行，區塊內每一行原樣保留。
	kept := make([]string, 0, len(lines))
	for i, ln := range lines {
		if drop[i+1] {
			continue
		}
		kept = append(kept, ln)
	}
	reduced := strings.Join(kept, "\n")

	// 再走一次正常 `no fmt`：縮排與空行自動歸位；解析失敗則保守放棄（不改壞原檔）。
	out, perrs := nfmt.FormatFileWithErrors(reduced)
	if len(perrs) > 0 || out == src {
		return "", false
	}
	return out, true
}

// isOmittableMain 判斷 fd 是否為可省略的 `main`：頂層、名字為 main、非方法、
// 無參數、無結果參數、區塊形且末行 `}` 有位置。
func isOmittableMain(fd *parser.FunctionDefinition) bool {
	if fd == nil || fd.Name != "main" || fd.IsMethodDef || fd.Body == nil {
		return false
	}
	if len(fd.Parameters) != 0 || len(parser.DeclaredResults(fd)) != 0 {
		return false
	}
	if fd.Body.IsInline || fd.Body.RBrace.Line == 0 {
		return false
	}
	return true
}

// blockHasDirectReturn 遞迴走區塊語句樹，發現「屬於本層函式主體」的 return 即回傳 true。
// 遇到嵌套函式定義／函式字面量則不下鑽（其 return 屬嵌套函式自身）。
func blockHasDirectReturn(block *parser.BlockStatement) bool {
	if block == nil {
		return false
	}
	for _, s := range block.Statements {
		if stmtHasDirectReturn(s) {
			return true
		}
	}
	return false
}

func stmtHasDirectReturn(s parser.Statement) bool {
	switch v := s.(type) {
	case *parser.ReturnStatement:
		return true
	case *parser.BlockStatement:
		return blockHasDirectReturn(v)
	case *parser.ForStatement:
		if v.Init != nil && stmtHasDirectReturn(v.Init) {
			return true
		}
		if v.Update != nil && stmtHasDirectReturn(v.Update) {
			return true
		}
		return blockHasDirectReturn(v.Body)
	case *parser.ExpressionStatement:
		return exprHasDirectReturn(v.Expression)
	}
	return false
}

// exprHasDirectReturn 走 if/match 的分支區塊（Consequence / Alternative / DotValBody
// 都是 BlockStatement）。不進入 FunctionLiteral（其 body 的 return 屬該字面量自身）。
func exprHasDirectReturn(e parser.Expression) bool {
	ie, ok := e.(*parser.IfExpression)
	if !ok || ie == nil {
		return false
	}
	if blockHasDirectReturn(ie.Consequence) {
		return true
	}
	if blockHasDirectReturn(ie.Alternative) {
		return true
	}
	return blockHasDirectReturn(ie.DotValBody)
}

// topLevelMainCall 判斷頂層陳述是否為零參 `main()` 呼叫，是則回傳其所占行跨度
// [start, end]（1-based）與 true。只認 `main()` 這種「Identifier  callee + 零引數」
// 的直接呼叫；其它形如 `x.main()` / `main(a)` / 把 main 當值傳遞都不匹配（回 false）。
func topLevelMainCall(s parser.Statement) (int, int, bool) {
	es, ok := s.(*parser.ExpressionStatement)
	if !ok || es == nil {
		return 0, 0, false
	}
	call, ok := es.Expression.(*parser.CallExpression)
	if !ok || call == nil || len(call.Arguments) != 0 {
		return 0, 0, false
	}
	id, ok := call.Function.(*parser.Identifier)
	if !ok || id == nil || id.Value != "main" {
		return 0, 0, false
	}
	return es.Pos().Line, es.EndPos().Line, true
}

// countMainIdentTokens 以詞法元數全檔 `main` IDENT token 的出現次數。字串/註解
// 中的 `main` 不是 IDENT token，故不計入；`x.main()` 的 `main` 仍是 IDENT，會使計數
// 超出「定義數 + 呼叫數」而觸發上層安全閘門跳過。
func countMainIdentTokens(src string) int {
	lx := lexer.New(src)
	n := 0
	for {
		tok := lx.NextToken()
		if tok.Type == lexer.EOF {
			break
		}
		if tok.Type == lexer.IDENT && tok.Literal == "main" {
			n++
		}
	}
	return n
}
