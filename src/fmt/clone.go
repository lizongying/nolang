package fmt

// clone.go — 給「同一份原始碼已經 parse 過」的消費方一顆可安全改寫的工作副本。
//
// LSP 的 ParseDocument 每次緩衝區變動都會發布一棵 SkipLowering 糖 AST（見
// lsp/documents.go 的 parseSnapshot）。format-on-save 若要複用這棵樹，不能直接
// 拿索引樹來跑主動改寫：checker.FixStringConcatOperators 會就地改 AST 節點，
// 渲染器省略冗餘標註時也會短暫改寫 LetStatement.Type（stmt.go 會還原，但
// 併發的 hover / 補全讀到中途狀態仍有風險）。CloneProgram 正是為此存在。
//
// 實證（fmt 保真測試）：CloneProgram 保留 FmtTrivia（連 node-keyed 換鍵）、
// ExternFuncSigs / ExternMethodSigs / EnumVariants 與 VarTypes，格式化輸出與
// fresh parse 逐位元組一致；**但 ExternStructFields 會整張消失**，因為
// remapSem 走 SemanticContext.Merge，而 Merge 只搬函式 / 方法兩張簽名表。
// 這張表是名字鍵（struct 名 → 欄位名 → 型別字串），沒有節點指標，補回來
// 只需淺拷貝，不必動 parser 的 clone 語意（那會影響 astcache 與裝配期）。

import "github.com/lizongying/nolang/parser"

// CloneProgramForFormat 回傳 prog 的格式化工作副本：原樹及其可達節點不被
// 觸碰，副本可安全交給 checker / formatter 改寫。prog 為 nil 時回 nil。
// 與 parser.CloneProgram 的差別只補回 ExternStructFields。
func CloneProgramForFormat(prog *parser.Program) *parser.Program {
	out := parser.CloneProgram(prog)
	if out == nil || out.Sem == nil || prog.Sem == nil || len(prog.Sem.ExternStructFields) == 0 {
		return out
	}
	fields := make(map[string]map[string]string, len(prog.Sem.ExternStructFields))
	for name, fs := range prog.Sem.ExternStructFields {
		inner := make(map[string]string, len(fs))
		for k, v := range fs {
			inner[k] = v
		}
		fields[name] = inner
	}
	out.Sem.ExternStructFields = fields
	return out
}
