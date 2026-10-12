package checker

import (
	"fmt"
	"math"

	"github.com/lizongying/nolang/parser"
)

// D9（2026-10-12 用户定案）：match 的空 `->` 兜底臂需要区分两种状态。
//
// 统一 option 规则（D6）下 `x = a + b` / `x = arr[i]` 一律产出 ?T，用户为了拿到值
// 必须写 match；而 match 的穷尽性要求（D8）又强制写出 `->` 兜底臂。于是「看得见不会
// 失败」的表达式也会被套上一个空兜底臂，看起来很多余：
//
//	ao = 1 + 2
//	a  = ao: {
//	    ok -> it
//	    ->      ; ← 主要影响这里
//	}
//
// 用户定案这个兜底臂**不能移除**（match 必须所有分支），**也不改语法**（保留还有一层
// 意义：未来可能出错、也可能正确，语法不用动）。编译器该做的是把状态告诉用户：
//
//   - 可证明恒为 ok（字面量算术不会溢出/除零、索引可证明在界内…）→ **HINT**
//     `arm-unreach`：这个分支不会走到。
//   - 无法证明 → **WARNING** `arm-should`：这个分支可能会走到，请决定它该做什么
//     （例如 `log('...')` / `print('...')`），而不是静默落入空臂的默认零值。
//
// 两者都只提示、不阻断：空兜底臂本身合法（D4：空臂 = 默认零值兼兜底臂）。
//
// 与 match_exhaustive.go 的分工：那边管「缺臂」（non-exhaustive），这边管「兜底臂
// 是否死代码 / 是否该写点东西」。复用同一批 match 走訪工具（collectMatchRoots /
// matchChainedChild / matchSubjectName），避免两套 AST 走法漂移。
const (
	armUnreachTraceID = "arm-unreach"
	armShouldTraceID  = "arm-should"
)

// maxSubjectResolveDepth 限制「標識符 → 其初始器」的追蹤深度。區域變數的初始器
// 又是另一個區域變數時（`a = 1 + 2` / `b = a`）需遞迴；深度上限防止異常 AST
// （自我引用 / 環）造成無限展開。
const maxSubjectResolveDepth = 8

// CatchAllArm 描述一条 option match 的**空** `->` 兜底臂。
type CatchAllArm struct {
	Root     *parser.IfExpression // match 链的根（位置即整条 match 的位置）
	Body     []parser.Statement   // 终端兜底臂的语句体（已知为空）
	AlwaysOk bool                 // true = 编译器可证明主体恒为 ok（死臂）
	Subject  string               // 可打印的主体名（诊断信息用）
	FuncName string               // 所在函数（匹配 collectMatchRoots 的口径）
}

// CollectEmptyCatchAllArms 回傳程式中所有「以空 `->` 兜底臂結尾」的 option match，
// 並判定该兜底臂是否可证明不会被执行。
//
// 只收錄兜底臂「什麼都沒做」（只產生 D4 默認零值）的 match —— 见
// catchAllArmIsDefaulting。用戶已经寫了動作的兜底臂（`-> log('x')`）代表他已经
// 决定失败时做什么，无需再提示。
func CollectEmptyCatchAllArms(program *parser.Program) []CatchAllArm {
	if program == nil {
		return nil
	}
	inb := parser.ProvablyInBoundsIndexReads(program)
	inits := localInitializers(program)

	var out []CatchAllArm
	roots, rootFunc := collectMatchRoots(program)
	for _, root := range roots {
		// 只要 option match（有臂测试 ok/nil/err）。值 match、bare guard 不在范围。
		if len(matchVariants(root)) == 0 {
			continue
		}
		// lowering 合成的 match（`?=` / 安全索引）没有用户可改的源码头臂。
		if root.MatchEndPos.Line == 0 {
			continue
		}
		// 末端兜底臂：match 链最后一级的 else 体。
		body := catchAllTailBody(root)
		if body == nil || !catchAllArmIsDefaulting(body) {
			continue
		}
		fn := rootFunc[root]
		subj := root.MatchedExpr
		out = append(out, CatchAllArm{
			Root:     root,
			Body:     body,
			AlwaysOk: optionSubjectAlwaysOk(subj, fn, inb, inits, 0),
			Subject:  matchSubjectName(root),
			FuncName: fn,
		})
	}
	return out
}

// ValidateCatchAllArms 是 CollectEmptyCatchAllArms 的上报包装（D9）。
func ValidateCatchAllArms(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult
	for _, a := range CollectEmptyCatchAllArms(program) {
		pos := a.Root.Pos()
		if a.AlwaysOk {
			results = append(results, ValidateResult{
				Line:      pos.Line,
				Column:    pos.Column,
				EndColumn: a.Root.EndPos().Column,
				TraceID:   armUnreachTraceID,
				Message: fmt.Sprintf(
					"空的 `->` 兜底臂不会被执行：编译器可证明 '%s' 恒为 ok（字面量算术不会溢出/除零，或索引可证明在界内）。该臂仍须保留以满足 match 穷尽性；保留也便于日后语义变化时无需改语法。",
					a.Subject),
			})
			continue
		}
		results = append(results, ValidateResult{
			Line:      pos.Line,
			Column:    pos.Column,
			EndColumn: a.Root.EndPos().Column,
			TraceID:   armShouldTraceID,
			Message: fmt.Sprintf(
				"空的 `->` 兜底臂可能会被执行（无法证明 '%s' 恒为 ok）。请决定失败时应该做什么，例如 `-> log('...')` / `-> print('...')`；留空则会静默取默认零值。",
				a.Subject),
		})
	}
	return results
}

// catchAllTailBody 沿 match 的 if/else 链走到末端，回傳终端兜底臂（else）的语句体。
// 回傳 nil 表示这条链没有兜底臂（那种情形由 CollectNonExhaustiveMatches 负责上报）。
func catchAllTailBody(root *parser.IfExpression) []parser.Statement {
	node := root
	seen := make(map[*parser.IfExpression]bool)
	for node != nil && !seen[node] {
		seen[node] = true
		if child := matchChainedChild(node); child != nil {
			node = child
			continue
		}
		if node.Alternative == nil {
			return nil // 链末端没有 else：非穷尽，不是这里的职责
		}
		return node.Alternative.Statements
	}
	return nil
}

// catchAllArmIsDefaulting 報告终端兜底臂是否「什麼都沒做」——只產生 D4 的默認零值。
//
// 🔴 為何不能簡單看「else 塊有幾條陳述」：`x = ao: { ok -> it, -> }` 降級後的 else
// 塊恆為 2 條（`it = <零值>` 合成陳述 + 零值表達式本身），用戶寫 `-> 0` 與寫 `->`
// 的 AST **完全無法區分**（D4：空臂語義就是默認零值）。因此這裡改為辨識「臂的值
// 是不是默認零值」，而不是辨識「臂是否為空」。
//
// 寫了實際動作的臂（`-> log('bad')`、`-> 7`、多條陳述、區塊體）一律回 false ——
// 那代表用戶已經決定失敗時做什麼，無需提示。
func catchAllArmIsDefaulting(body []parser.Statement) bool {
	if len(body) == 0 {
		// else 塊真的為空：同樣什麼都沒做。
		return true
	}
	// 形態 A：[零值表達式]
	// 形態 B：[it = <零值>（合成）, 零值表達式]  ← D4 空臂降級後的標準形態
	i := 0
	if ls, ok := body[0].(*parser.LetStatement); ok && ls.IsSynthetic {
		i = 1
	}
	if i >= len(body) {
		return true
	}
	// 剩下的必須恰好一條「零值表達式」陳述。
	if len(body)-i != 1 {
		return false
	}
	es, ok := body[i].(*parser.ExpressionStatement)
	if !ok {
		return false
	}
	return isDefaultZeroValueExpr(es.Expression)
}

// isDefaultZeroValueExpr 判斷 e 是否為 D4 的默認零值（與 parser 的
// zeroValueLiteralFor / scalarZeroLiteral 產生的形態對齊）。
func isDefaultZeroValueExpr(e parser.Expression) bool {
	switch v := e.(type) {
	case *parser.IntegerLiteral:
		return v.Value == 0
	case *parser.FloatLiteral:
		return v.Value == 0
	case *parser.StringLiteral:
		return v.Value == ""
	case *parser.CharLiteral:
		return v.Value == "" || v.Value == "\x00"
	case *parser.BooleanLiteral:
		return !v.Value
	case *parser.NilLiteral:
		return true
	case *parser.ArrayLiteral:
		return len(v.Elements) == 0
	case *parser.MapLiteral:
		return len(v.Pairs) == 0
	case *parser.StructLiteral:
		return len(v.Fields) == 0
	case *parser.GroupedExpression:
		return isDefaultZeroValueExpr(v.Expression)
	}
	return false
}

// optionSubjectAlwaysOk 判斷 match 主體是否「可证明恒为 ok」。
//
// 可证明的來源（與 D7-R1 的 bounds.go 同一套静態分析，保持口径一致）：
//   - 索引讀取 `a[i]`：可证明在界内（定長陣列 / with-len(K) 切片 + 界内下標）；
//   - 整數算術 `a OP b`：可常量折疊且不失敗（`+ - *` 不溢位；`/` `%` 的除數為非零
//     字面量且不是 (MIN, -1) 這對危險組合）；
//   - 標識符：追蹤到它唯一的初始器後遞迴判斷。
//
// 其餘一律回 false（→ WARNING）：宁可多提示，不可把「其實会失败」的臂說成死臂。
func optionSubjectAlwaysOk(subj parser.Expression, funcName string,
	inb map[*parser.IndexExpression]bool,
	inits map[string]map[string]parser.Expression, depth int) bool {
	if depth > maxSubjectResolveDepth {
		return false
	}
	switch s := subj.(type) {
	case *parser.GroupedExpression:
		return optionSubjectAlwaysOk(s.Expression, funcName, inb, inits, depth+1)
	case *parser.IndexExpression:
		return inb[s]
	case *parser.InfixExpression:
		return alwaysOkArith(s, depth)
	case *parser.Identifier:
		inner, ok := inits[funcName][s.Value]
		if !ok {
			return false
		}
		return optionSubjectAlwaysOk(inner, funcName, inb, inits, depth+1)
	}
	return false
}

// alwaysOkArith 判斷一棵算術樹是否「可证明不會失敗」（不溢位 / 不除零）。與
// foldIntArith 的分工：foldIntArith 只能處理「整棵都是字面量」的情形；這裡額外
// 收錄用戶最常見的 **除數為非零字面量**（`x / 2`、`n % 8`）——被除數是變數也無妨，
// 因為除零與 (MIN,-1) 溢位都只取決於除數。
func alwaysOkArith(inf *parser.InfixExpression, depth int) bool {
	if depth > maxSubjectResolveDepth || inf == nil {
		return false
	}
	switch inf.Operator {
	case "/", "%":
		// 除數必須是非零字面量，且不是 -1（`MIN / -1` 會溢位；|除數| >= 2 或
		// 除數 == 1 都安全）。`x / 2` 因此可證明安全，`x / y` 不可。
		if !nonOverflowingDivisorLiteral(inf.Right) {
			return false
		}
		// 被除數自身也可能是算術（`(a + b) / 2`），必須同樣可證明安全。
		return alwaysOkOperand(inf.Left, depth+1)
	case "+", "-", "*":
		// 加減乘的溢位取決於兩側，只有整棵可常量折疊時才可證明。
		_, ok := foldIntArith(inf, depth)
		return ok
	}
	return false
}

// alwaysOkOperand 判斷運算元是否「自身不會再引入失敗」。
//
// 🔴 只有除式需要這層檢查：`(a + b) / 2` 的除零已經由除數消掉，但 `a + b` 自己可能
// 溢位，所以被除數若是算術就必須遞迴。反之 `x / 2` 的被除數是個普通變數 —— 變數
// （與字面量）不可能在求值時「失敗」，可以直接放行；這正是用戶舉的 `x / 2` 場景。
// 呼叫 / 索引 / 屬性存取的結果本身可能就是 option，一律保守判為不可證明。
func alwaysOkOperand(e parser.Expression, depth int) bool {
	if depth > maxSubjectResolveDepth || e == nil {
		return false
	}
	switch v := e.(type) {
	case *parser.Identifier, *parser.IntegerLiteral, *parser.FloatLiteral,
		*parser.StringLiteral, *parser.CharLiteral, *parser.BooleanLiteral:
		return true
	case *parser.GroupedExpression:
		return alwaysOkOperand(v.Expression, depth+1)
	case *parser.InfixExpression:
		return alwaysOkArith(v, depth+1)
	}
	return false
}

// nonOverflowingDivisorLiteral 報告 e 是否為「不會造成除零 / 除式溢位」的字面量
// 除數：非零，且不是 -1（`INT64_MIN / -1` 溢位）。
// 注意前綴 `-`：有效除數是 **-il.Value**，所以危險的那個是 il.Value == 1。
func nonOverflowingDivisorLiteral(e parser.Expression) bool {
	switch v := e.(type) {
	case *parser.IntegerLiteral:
		return v.Value != 0 && v.Value != -1
	case *parser.PrefixExpression:
		if v.Operator != "-" {
			return false
		}
		il, ok := v.Right.(*parser.IntegerLiteral)
		if !ok {
			return false
		}
		return il.Value != 0 && il.Value != 1
	}
	return false
}

// foldIntArith 折疊一棵「葉子全是整數字面量」的算術樹，回傳折疊值。
// ok=false 表示無法證明安全：有非字面量葉子、會溢位、除數為 0、或除式為
// (INT64_MIN, -1) 這對會溢位的危險組合。呼叫方只關心 ok，不關心值。
func foldIntArith(e parser.Expression, depth int) (int64, bool) {
	if depth > maxSubjectResolveDepth {
		return 0, false
	}
	switch v := e.(type) {
	case *parser.IntegerLiteral:
		return v.Value, true
	case *parser.GroupedExpression:
		return foldIntArith(v.Expression, depth+1)
	case *parser.PrefixExpression:
		if v.Operator != "-" {
			return 0, false
		}
		x, ok := foldIntArith(v.Right, depth+1)
		if !ok || x == math.MinInt64 {
			return 0, false
		}
		return -x, true
	case *parser.InfixExpression:
		l, okL := foldIntArith(v.Left, depth+1)
		r, okR := foldIntArith(v.Right, depth+1)
		if !okL || !okR {
			return 0, false
		}
		switch v.Operator {
		case "+":
			if (l > 0 && r > 0 && l > math.MaxInt64-r) || (l < 0 && r < 0 && l < math.MinInt64-r) {
				return 0, false
			}
			return l + r, true
		case "-":
			if (l >= 0 && r < 0 && l > math.MaxInt64+r) || (l < 0 && r > 0 && l < math.MinInt64+r) {
				return 0, false
			}
			return l - r, true
		case "*":
			if l == 0 || r == 0 {
				return 0, true
			}
			if (l > 0) == (r > 0) {
				// 同號：正乘積必 > 0，負乘積必 < 0；反向即溢位。
				if l > math.MaxInt64/r || l < math.MinInt64/r {
					return 0, false
				}
			} else if l < math.MinInt64/r {
				return 0, false
			}
			return l * r, true
		case "/", "%":
			if r == 0 {
				return 0, false // 除零
			}
			if l == math.MinInt64 && r == -1 {
				return 0, false // (MIN, -1) 溢位
			}
			if v.Operator == "/" {
				return l / r, true
			}
			return l % r, true
		}
	}
	return 0, false
}

// localInitializers 回傳「函數名 → 變數名 → 初始器」，只收錄在該函數作用域內**僅
// 被綁定一次**的區域變數。綁定多次（遮蔽 / 重新宣告 / 多重賦值）的一律不收 ——
// 追錯初始器會把「其實会失败」的臂誤判成死臂，屬不可接受方向，故保守剔除。
func localInitializers(program *parser.Program) map[string]map[string]parser.Expression {
	out := map[string]map[string]parser.Expression{}
	if program == nil {
		return out
	}
	counts := map[string]map[string]int{}

	var walkStmts func(funcName string, stmts []parser.Statement)
	walkStmts = func(funcName string, stmts []parser.Statement) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			switch st := s.(type) {
			case *parser.FunctionDefinition:
				if st.Body != nil {
					walkStmts(st.Name, st.Body.Statements)
				}
			case *parser.BlockStatement:
				walkStmts(funcName, st.Statements)
			case *parser.LetStatement:
				if st.Name == nil || st.Value == nil || st.IsSynthetic {
					continue
				}
				// 合成陳述（lowering 產物）不算用戶綁定；具名 tuple/多重
				// 宣告結構複雜，同樣保守略過。
				if counts[funcName] == nil {
					counts[funcName] = map[string]int{}
				}
				counts[funcName][st.Name.Value]++
				if counts[funcName][st.Name.Value] == 1 {
					if out[funcName] == nil {
						out[funcName] = map[string]parser.Expression{}
					}
					out[funcName][st.Name.Value] = st.Value
				}
			}
		}
	}
	walkStmts("", program.Statements)

	// 第二趟：剔除綁定次數 > 1 的變數。
	for fn, per := range counts {
		for name, n := range per {
			if n > 1 {
				delete(out[fn], name)
			}
		}
	}
	return out
}
