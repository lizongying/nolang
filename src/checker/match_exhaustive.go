package checker

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// matchNonexTraceID is the diagnostic ID for non-exhaustive option matches.
const matchNonexTraceID = "match-nonex"

// optionMatchVariants are the option variants a match arm can test against.
var optionMatchVariants = map[string]bool{"ok": true, "nil": true, "err": true}

// ValidateNonExhaustiveMatch reports matches over option-typed values that handle
// neither the failure variants (nil/err) nor provide a catch-all `->` arm.
//
// nolang desugars `x: { pattern -> body }` into an if/else chain in
// parser/lowering.go, so a match carrying only `ok ->` becomes a plain
// `if x == ok { ... }` with no else: when the option is nil/err the body is
// silently skipped and execution continues with whatever default the surrounding
// code had. That is a frequent source of "the operation failed yet we carried
// on with a zero value" bugs. We therefore require option matches to be
// exhaustive.
//
// RELATIONSHIP TO THE PARSER'S [RAL] CHECK — this rule is COMPLEMENTARY, not
// redundant. The parser enforces "err, nil, ok" completeness at parse time
// (see "option match must handle all branches" in parser/expr.go and
// parser/match.go), but ONLY when matchedIsOption() can prove the subject is an
// option: it handles a bare *Identifier whose type is already recorded in
// p.sem.VarTypes with a "?" prefix. Matches whose subject is anything else —
// most importantly a CALL, e.g. `foo(): { ok -> ... }` — parse cleanly with no
// [RAL] diagnostic and are caught only here. Do NOT delete this rule on the
// assumption that [RAL] subsumes it; see TestNonExhaustiveMatchCallSubject.
//
// Only option matches are reported: bare guard blocks (`{ cond -> body }`) have
// no MatchedExpr, and matches over plain values (`x: { 1 -> ... }`) test no
// option variant, so neither is flagged — keeping the rule low-noise.
// NonExhaustiveMatch describes one option match that ValidateNonExhaustiveMatch
// reports: the desugared root arm plus the option variants it fails to handle.
//
// It exists so automated fixers (see `no fix`) can drive off the
// *same* detection the diagnostic uses instead of re-deriving arm structure from
// source text.
//
// SHAPES (S-d): after the S-d cleanup (2026-10-19) matches only ever appear as
// *parser.SurfaceMatch on the checker path — Lower() is never called in the
// pipeline (only LowerSurface runs during ParseProgram, and it does not lower
// matches), so Sugar is the sole shape and Root no longer exists. Pos / End /
// Subject are filled from the SurfaceMatch node (the closing `}` is at
// SurfaceMatch.RBracePos), which is what `no fix` drives off.
type NonExhaustiveMatch struct {
	Sugar   *parser.SurfaceMatch // 糖形态：match 节点；降糖形态为 nil
	Pos     lexer.Position       // 诊断锚点（match 起点），两形态共同填写
	End     lexer.Position       // match 收尾 `}` 的位置（fixer 插入点），两形态共同填写
	Subject string               // 可打印的主体名，两形态共同填写
	Missing []string             // subset of ok / nil / err, in that canonical order
	File    string               // 所在源文件（顶层 stmt 的 SourceFile，由 transpiler 合并前填入）。
	// 空表示未知：RunAllLints 在 merged 多文件模式下会据此丢弃（见 lint.go 的
	// `!singleFile && r.File == ""` 过滤），故务必填实。
}

// CollectNonExhaustiveMatches returns every option match in program that neither
// terminates in a catch-all arm nor handles all three option variants.
// ValidateNonExhaustiveMatch is a thin reporting wrapper over it.
//
// After S-d (2026-10-19) matches only ever appear as *parser.SurfaceMatch on the
// checker path (Lower() is never called in the pipeline; only LowerSurface runs
// during ParseProgram, and it does not lower matches), so a single sugar-shape
// collector handles every match.
func CollectNonExhaustiveMatches(program *parser.Program) []NonExhaustiveMatch {
	if program == nil {
		return nil
	}
	return collectSugarNonExhaustiveMatches(program)
}

func ValidateNonExhaustiveMatch(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult
	for _, m := range CollectNonExhaustiveMatches(program) {
		pos := m.Pos
		results = append(results, ValidateResult{
			TraceID: matchNonexTraceID,
			Line:    pos.Line,
			Column:  pos.Column,
			File:    m.File,
			Message: fmt.Sprintf(
				"non-exhaustive match on option '%s': missing arm(s): %s — add a `-> ` (else) arm, or handle every variant",
				m.Subject, strings.Join(m.Missing, ", ")),
		})
	}
	return results
}

// subjectIsDeclaredEnumExpr reports whether the matched subject's static type is a
// user-declared enum (e.g. `e-res`). Such a match ranges over that enum's own
// variants, so the option completeness rule (ok/nil/err) does not apply.
// Only a statically known type can be excluded — unknown types stay reported,
// which is what lets call subjects (`foo(): { ok -> ... }`) still be caught.
//
// The subject is looked up with FuncVarType(funcName, ...) so function-local
// types (parameters included) are found in the per-function map first, falling
// back to the global VarTypes for module-level matches.
//
// After S-d the only caller is the sugar-shape collector, which hands over the
// SurfaceMatch subject directly.
func subjectIsDeclaredEnumExpr(program *parser.Program, subj parser.Expression, funcName string) bool {
	if program == nil || program.Sem == nil || len(program.Sem.EnumVariants) == 0 {
		return false
	}
	id, ok := subj.(*parser.Identifier)
	if !ok {
		return false
	}
	t, ok := program.Sem.FuncVarType(funcName, id.Value)
	if !ok || t == "" || strings.HasPrefix(t, "?") {
		return false
	}
	_, isEnum := program.Sem.EnumVariants[t]
	return isEnum
}

// collectMatchRoots returns the outermost IfExpression of every match in the
// program, together with the name of the enclosing function ("" for module-level
// matches) and the source file of the enclosing top-level statement. Chained
// arms (an arm sitting in another arm's else slot) are excluded so each match is
// reported exactly once. The file map lets callers attribute each match to its
// originating module in the merged (multi-file) program used by `no vet`.
func collectMatchRoots(program *parser.Program) ([]*parser.IfExpression, map[*parser.IfExpression]string, map[*parser.IfExpression]string) {
	var all []*parser.IfExpression
	children := make(map[*parser.IfExpression]bool)
	rootFunc := make(map[*parser.IfExpression]string)
	rootFile := make(map[*parser.IfExpression]string)

	var walkExpr func(funcName, file string, e parser.Expression)
	var walkStmts func(funcName, file string, stmts []parser.Statement)

	walkStmts = func(funcName, file string, stmts []parser.Statement) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			// 陳述自身帶 SourceFile 時刷新當前檔（模組合併後由 transpiler
			// 填入；只有頂層陳述持有，其函式體內部陳述不繼承）。與
			// range_index_lint.go 的 curFile 下傳口徑一致。
			if f := parser.GetSourceFile(s); f != "" {
				file = f
			}
			switch st := s.(type) {
			case *parser.FunctionDefinition:
				fn := st.Name
				if st.Body != nil {
					walkStmts(fn, file, st.Body.Statements)
				}
			case *parser.BlockStatement:
				walkStmts(funcName, file, st.Statements)
			case *parser.LetStatement:
				walkExpr(funcName, file, st.Value)
			case *parser.MultiAssignStatement:
				for _, t := range st.Targets {
					walkExpr(funcName, file, t)
				}
				walkExpr(funcName, file, st.Value)
			case *parser.UnwrapAssignStatement:
				walkExpr(funcName, file, st.Value)
			case *parser.ReturnStatement:
				walkExpr(funcName, file, st.ReturnValue)
			case *parser.ExpressionStatement:
				walkExpr(funcName, file, st.Expression)
			case *parser.ForStatement:
				walkExpr(funcName, file, st.Condition)
				if st.Init != nil {
					walkStmts(funcName, file, []parser.Statement{st.Init})
				}
				if st.Update != nil {
					walkStmts(funcName, file, []parser.Statement{st.Update})
				}
				if st.Body != nil {
					walkStmts(funcName, file, st.Body.Statements)
				}
			}
		}
	}

	walkExpr = func(funcName, file string, e parser.Expression) {
		if e == nil {
			return
		}
		switch x := e.(type) {
		case *parser.IfExpression:
			// 實 if/else：match 在糖形態下是 SurfaceMatch（見 CollectNonExhaustiveMatches），不經此路徑。
			walkExpr(funcName, file, x.Condition)
			if x.Consequence != nil {
				walkStmts(funcName, file, x.Consequence.Statements)
			}
			if x.Alternative != nil {
				walkStmts(funcName, file, x.Alternative.Statements)
			}
		case *parser.InfixExpression:
			walkExpr(funcName, file, x.Left)
			walkExpr(funcName, file, x.Right)
		case *parser.PrefixExpression:
			walkExpr(funcName, file, x.Right)
		case *parser.CallExpression:
			walkExpr(funcName, file, x.Function)
			for _, a := range x.Arguments {
				walkExpr(funcName, file, a)
			}
		case *parser.AssignExpression:
			walkExpr(funcName, file, x.Left)
			walkExpr(funcName, file, x.Value)
		case *parser.IndexExpression:
			walkExpr(funcName, file, x.Left)
			walkExpr(funcName, file, x.Index)
		case *parser.ArrayLiteral:
			for _, el := range x.Elements {
				walkExpr(funcName, file, el)
			}
		case *parser.GroupedExpression:
			walkExpr(funcName, file, x.Expression)
		case *parser.AwaitExpression:
			walkExpr(funcName, file, x.Right)
		case *parser.DotExpression:
			walkExpr(funcName, file, x.Receiver)
		}
	}

	walkStmts("", "", program.Statements)

	roots := make([]*parser.IfExpression, 0, len(all))
	for _, ife := range all {
		if !children[ife] {
			roots = append(roots, ife)
		}
	}
	return roots, rootFunc, rootFile
}

// ────────────────────────────────────────────────────────────────────────────
// 糖形态的 match（S-d 后唯一形态）
//
// S-d（2026-10-19）删除了降糖路径：pipeline 中从不在 checker 之前把 match 降成
// if/else 链（只有 ParseProgram 的 LowerSurface，它不降 match），所以这里只保留
// 糖形态收集器 collectSugarNonExhaustiveMatches，直接走
// collectSugarMatchRoots / sugarMatchVariants / sugarMatchHasCatchAll /
// sugarMatchSubjectName（match_arm_reach.go）与 subjectIsDeclaredEnumExpr（本档）。
//
// 历史对照（已删除的降糖路径，仅作溯源）：降糖版曾用 collectMatchRoots 走
// if/else 链、matchVariants 沿 matchChainedChild 收变体、matchHasCatchAll 看链
// 末端 else；糖版用上面 sugar* 对应物代替，语义逐条等价（MatchedExpr ≠ nil ↔
// sm.Matched ≠ nil）。collectMatchRoots 现仅由 match_arm_reach_test.go 的
// desugar-scoped 回归测试使用，保留作 guard。
// ────────────────────────────────────────────────────────────────────────────

// collectSugarNonExhaustiveMatches 收集程式中所有「以空 option 变体 arm 结尾」或
// 「缺 ok/nil/err 中某些变体」的 *parser.SurfaceMatch（S-d 后 match 的唯一形态）。
// 语义等价于已删除的降糖版 collectLoweredNonExhaustiveMatches，但直接在糖形态上判断。
func collectSugarNonExhaustiveMatches(program *parser.Program) []NonExhaustiveMatch {
	var out []NonExhaustiveMatch
	roots, rootFunc, rootFile := collectSugarMatchRoots(program)
	for _, sm := range roots {
		// 裸 guard 區塊（`{ cond -> body }`）沒有主體，降糖形態下
		// collectMatchRoots 也以 MatchedExpr == nil 排除它。沒有這一條，
		// `{ ok -> body }`（條件恰好叫 ok）會被誤判成缺 nil/err 的 option
		// match——糖形態獨有的誤報。
		if sm.Matched == nil {
			continue
		}
		vs := sugarMatchVariants(sm)
		if len(vs) == 0 {
			continue
		}
		// 與降糖路徑的 MatchEndPos.Line == 0 對應。糖形態下 parser 不為合成
		// match 建 SurfaceMatch（`?=` / 安全索引的合成 match 只存在於
		// lowering），所以 RBracePos 為 0 只可能是異常 AST；保守跳過，與降糖
		// 路徑「使用者沒有可改的源碼區塊就不要求窮盡」同向。
		if sm.RBracePos.Line == 0 {
			continue
		}
		if subjectIsDeclaredEnumExpr(program, sm.Matched, rootFunc[sm]) {
			continue
		}
		if sugarMatchHasCatchAll(sm) || (vs["ok"] && vs["nil"] && vs["err"]) {
			continue
		}
		var missing []string
		for _, v := range []string{"ok", "nil", "err"} {
			if !vs[v] {
				missing = append(missing, v)
			}
		}
		out = append(out, NonExhaustiveMatch{
			Sugar:   sm,
			Pos:     sm.Pos(),
			End:     sm.RBracePos,
			Subject: sugarMatchSubjectName(sm),
			Missing: missing,
			File:    rootFile[sm],
		})
	}
	return out
}

// sugarMatchHasCatchAll 判断整条糖形态 match 的最后一條臂是否为裸 `->`（IsWildcard
// 且不是 `.->` / `ok ->` 這種 dot-val 分支）。语义等价于已删除的降糖版 matchHasCatchAll。
//
// 為何不能只看 IsWildcard：`ok ->` 與 `->` 兩者都帶 IsWildcard=true（parser
// 把「無條件的臂」一律標成 wildcard），差別在 IsDotVal。漏掉這個區分會把
// `v: { ok -> a }` 誤判成已有兜底臂 ⇒ 靜默漏報 nil/err。
func sugarMatchHasCatchAll(sm *parser.SurfaceMatch) bool {
	if sm == nil || len(sm.Arms) == 0 {
		return false
	}
	last := &sm.Arms[len(sm.Arms)-1]
	return last.IsWildcard && !last.IsDotVal
}
