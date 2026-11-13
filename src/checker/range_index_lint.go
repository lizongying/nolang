package checker

import (
	"github.com/lizongying/nolang/i18n"

	"github.com/lizongying/nolang/parser"
)

// rngidxTraceID 是「手動按下標遍歷容器」的建議診斷碼（§3.4 末條 / §11.9）。
const rngidxTraceID = "m2fh7rfg"

// ValidateRangeIndexLoops 對 `for i <- [a..b) { … arr[i] … }` 這類「手動按下標
// 遍歷容器」的寫法發一條 HINT，建議改寫為 `for x <- arr { … x … }`。
//
// 為什麼是 HINT 而不是 ERROR：兩種寫法都正確，這裡改的只是**慣用法**與
// 複雜度 —— 手動下標遍歷把「不會越界」的責任交回給程式設計師，編譯器看不見
// 上界與容器長度的關係，只能照常產生 option（D6），使用者於是要麼寫防禦碼、
// 要麼承擔一個靜默的默認值。容器迭代（for x <- arr）根本沒有下標，
// 也就不存在這個問題。
//
// 定位（D7 兩層分離，2026-10-15）：**不論**編譯器能否證明在界內，用戶側都需寫
// option 並解包（in-bounds 只是 codegen 內部消除，不改變用戶負擔），故這裡對
// in-bounds 讀取**照常提示**——容器迭代消除的正是這份用戶側解包負擔。此前依
// ProvablyInBoundsIndexReads 剔除 in-bounds 是 D7-R1（用戶側豁免）的舊口徑，已廢。
//
// 只在「改寫等價」時提示（否則提示就是一張兌現不了的支票）：下標在迴圈體裡
// 只充當同一個容器的下標、沒被當值使用、沒有 `base[i] = v` 直寫、基底是可迭代
// 的裸變數、且區間可證明恰好覆蓋整個容器。判定見 range_index_convert.go。
//
// 提示詞按「讀 / 寫」兩態分別舉例（2026-11-13）：容器迭代的迴圈變數是**元素的
// place**、型別是裸值 `T`，因此讀（無需解包）與寫（`x = v` 直接落回元素）都用
// 同一個 `x`。舊文案只給了一種 option 解包形（`x: { ok -> it = 0 }`），那是把
// 「按下標讀到 `?T`」的形狀硬套到迭代元素上 —— 照抄編不過，而真正要清零的迴圈
// （`out[i]: { ok -> it = 0 }`）反而正是該看「寫」那一態的場景。
//
// 不做的事：不自動改寫。是否真的想換寫法仍交給人工決定。
func ValidateRangeIndexLoops(program *parser.Program, mainFile string) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult
	seen := map[*parser.IndexExpression]bool{}
	// curScope 是當前函式（或頂層）的同一層作用域，量容器宣告長度用。
	curScope := rngidxNewScope(program.Statements, nil)

	// indexReadsByName 收集 e 中以 name 為下標、且基底是直接變數的索引讀取。
	var indexReadsByName func(e parser.Expression, name string) []*parser.IndexExpression
	indexReadsByName = func(e parser.Expression, name string) []*parser.IndexExpression {
		if e == nil {
			return nil
		}
		var out []*parser.IndexExpression
		switch x := e.(type) {
		case *parser.IndexExpression:
			if id, ok := x.Index.(*parser.Identifier); ok && id.Value == name {
				if b, ok := x.Left.(*parser.Identifier); ok && b.Value != name {
					out = append(out, x)
				}
			}
			out = append(out, indexReadsByName(x.Left, name)...)
			out = append(out, indexReadsByName(x.Index, name)...)
		case *parser.InfixExpression:
			out = append(out, indexReadsByName(x.Left, name)...)
			out = append(out, indexReadsByName(x.Right, name)...)
		case *parser.PrefixExpression:
			out = append(out, indexReadsByName(x.Right, name)...)
		case *parser.GroupedExpression:
			out = append(out, indexReadsByName(x.Expression, name)...)
		case *parser.CallExpression:
			out = append(out, indexReadsByName(x.Function, name)...)
			for _, a := range x.Arguments {
				out = append(out, indexReadsByName(a, name)...)
			}
		case *parser.DotExpression:
			out = append(out, indexReadsByName(x.Receiver, name)...)
		case *parser.SurfaceMatch:
			// S1：糖形態下臂體仍是語句容器，臂內的 `arr[i]` 讀取也要算進來。
			// 見 sugar_walk.go。
			out = append(out, indexReadsByName(x.Matched, name)...)
			for i := range x.Arms {
				arm := &x.Arms[i]
				out = append(out, indexReadsByName(arm.Condition, name)...)
				for _, p := range arm.MultiValuePatterns {
					out = append(out, indexReadsByName(p, name)...)
				}
				if arm.Body != nil {
					for _, bs := range arm.Body.Statements {
						switch b := bs.(type) {
						case *parser.LetStatement:
							out = append(out, indexReadsByName(b.Value, name)...)
						case *parser.ExpressionStatement:
							out = append(out, indexReadsByName(b.Expression, name)...)
						}
					}
				}
			}
		}
		return out
	}

	var walk func(stmts []parser.Statement, file string)
	walk = func(stmts []parser.Statement, file string) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			if f := program.Sem.SourceFileOf(s); f != "" {
				file = f
			}
			switch st := s.(type) {
			case *parser.FunctionDefinition:
				if st.Body != nil {
					outer := curScope
					// 輸入與輸出參數都可能是被迭代的容器（`f = (a [3]i64) (b [8]byte)`）。
					params := append(append([]*parser.Parameter{}, st.Parameters...), st.Results...)
					curScope = rngidxNewScope(st.Body.Statements, params)
					walk(st.Body.Statements, file)
					curScope = outer
				}
			case *parser.BlockStatement:
				walk(st.Statements, file)
			case *parser.BlockMatchStatement:
				// 塊 match 的主體 / 臂體與一般區塊同口徑下鑽。
				walk(blockMatchChildStatements(st), file)
			case *parser.ExpressionStatement:
				// `cond -> { … }` guard 語句：兩個分支都要走。
				if ie, ok := st.Expression.(*parser.IfExpression); ok {
					if ie.Consequence != nil {
						walk(ie.Consequence.Statements, file)
					}
					if ie.Alternative != nil {
						walk(ie.Alternative.Statements, file)
					}
				}
				// S1：糖形態下 match 臂體也是語句容器（`for` 迴圈本身可能就長在臂裡）。
				// 見 sugar_walk.go。
				if sm, ok := st.Expression.(*parser.SurfaceMatch); ok {
					walkSurfaceMatchChildren(sm, nil, nil, func(sub parser.Statement) {
						walk([]parser.Statement{sub}, file)
					})
				}
			case *parser.ForStatement:
				if st.IterRange != nil && st.IterRange.Range != nil && st.IterRange.Variable != "" && st.Body != nil {
					name := st.IterRange.Variable
					// 只在「改寫成容器迭代等價」時提示：下標另有其職 / 覆蓋範圍
					// 對不上容器長度時，改寫會丟掉行為，不能發這張支票
					// （判定見 range_index_convert.go）。
					iterable, convertible := rngidxConvertTarget(st, curScope)
					if convertible {
						for _, bs := range st.Body.Statements {
							if bs == nil {
								continue
							}
							// 該陳述裡所有以 name 為下標的容器讀取。
							var reads []*parser.IndexExpression
							switch b := bs.(type) {
							case *parser.LetStatement:
								reads = indexReadsByName(b.Value, name)
							case *parser.ExpressionStatement:
								reads = indexReadsByName(b.Expression, name)
							}
							for _, idx := range reads {
								if seen[idx] {
									continue
								}
								id, ok := idx.Left.(*parser.Identifier)
								if !ok || id.Value != iterable {
									continue
								}
								seen[idx] = true
								base := id.Value
								results = append(results, ValidateResult{
									Line:    idx.Pos().Line,
									Column:  idx.Pos().Column,
									File:    file,
									TraceID: "m2fh7rfg",
									Message: i18n.T("m2fh7rfg", "range-index loop: '%s[%s]' is accessed by index by hand inside `%s <- [a..b):`; if you do not need the index itself, iterate the container with `x <- %s` — no index means no bounds check and no option to unwrap. The loop variable is the element's place, so one form covers both directions:\n     read:  x <- %s: {\n         print(x)      ; x is a bare element, not `?T`\n     }\n     write: x <- %s: {\n         x = 0x00      ; the store lands back in the element\n     }\n     fill:  %s.fill(0x00)      ; every element the same value, no loop",
										base, name, name, base, base, base, base),
								})
							}
						}
					}
				}
				if st.Body != nil {
					walk(st.Body.Statements, file)
				}
			}
		}
	}
	walk(program.Statements, mainFile)
	return results
}
