package checker

import (
	"fmt"

	"github.com/lizongying/nolang/parser"
)

// rngidxTraceID 是「手動按下標遍歷容器」的建議診斷碼（§3.4 末條 / §11.9）。
const rngidxTraceID = "rngidx"

// ValidateRangeIndexLoops 對 `for i <- [a..b) { … arr[i] … }` 這類「手動按下標
// 遍歷容器」的寫法發一條 HINT，建議改寫為 `for x <- arr { … x … }`。
//
// 為什麼是 HINT 而不是 ERROR：兩種寫法都正確，這裡改的只是**慣用法**與
// 複雜度 —— 手動下標遍歷把「不會越界」的責任交回給程式設計師，編譯器看不見
// 上界與容器長度的關係，只能照常產生 option（D6），使用者於是要麼寫防禦碼、
// 要麼承擔一個靜默的默認值。容器迭代（for x <- arr）根本沒有下標，
// 也就不存在這個問題。
//
// 定位（§8.1(a) 的修訂）：只在**編譯器證明不了安全**的地方提示。
// 靜態分析（bounds.go）能證明在界內的讀取已經被消除了 option 包裝與解包，
// 沒有代價可省，提示只會製造噪音；因此這裡用 ProvablyInBoundsIndexReads
// 把那些讀取整批剔除，與 codegen / vet / fmt 三方共用同一份判定。
//
// 不做的事：不自動改寫。兩種寫法語義並不等價 —— 手動下標遍歷需要下標本身
// （`arr[i] = v`、`print(i)`、跨陣列對齊），改寫會丟掉它，故交給人工決定。
func ValidateRangeIndexLoops(program *parser.Program, mainFile string) []ValidateResult {
	if program == nil {
		return nil
	}
	inBounds := parser.ProvablyInBoundsIndexReads(program)
	var results []ValidateResult
	seen := map[*parser.IndexExpression]bool{}

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
		}
		return out
	}

	var walk func(stmts []parser.Statement, file string)
	walk = func(stmts []parser.Statement, file string) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			if f := parser.GetSourceFile(s); f != "" {
				file = f
			}
			switch st := s.(type) {
			case *parser.FunctionDefinition:
				if st.Body != nil {
					walk(st.Body.Statements, file)
				}
			case *parser.BlockStatement:
				walk(st.Statements, file)
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
			case *parser.ForStatement:
				if st.IterRange != nil && st.IterRange.Range != nil && st.IterRange.Variable != "" && st.Body != nil {
					name := st.IterRange.Variable
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
							if seen[idx] || inBounds[idx] {
								continue
							}
							seen[idx] = true
							base := ""
							if b, ok := idx.Left.(*parser.Identifier); ok {
								base = b.Value
							}
							results = append(results, ValidateResult{
								Line:    idx.Pos().Line,
								Column:  idx.Pos().Column,
								File:    file,
								TraceID: rngidxTraceID,
								Message: fmt.Sprintf(
									"range-index loop: '%s[%s]' 在 `for %s <- [a..b)` 裡手動按下標讀取；若不需要下標本身，改用容器迭代 `x <- %s`（無下標即無越界，也無需解包 option）。",
									base, name, name, base),
							})
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
