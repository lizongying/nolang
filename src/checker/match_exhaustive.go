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
// SHAPES (S1): the rule runs on two AST shapes. Under the default lowering a
// match is an if/else chain and Root is its outermost link; Root.MatchEndPos
// carries the source position of the match's closing `}`. Under SkipLowering a
// match is a single *parser.SurfaceMatch and Sugar holds it; the closing `}` is
// at SurfaceMatch.RBracePos. Pos / End / Subject are therefore filled by BOTH
// paths (the same normalisation CatchAllArm uses, see match_arm_reach.go) — a
// consumer that reads only Root silently sees nothing under sugar, which is how
// `--fix=match` lost its insertion point before the flip.
type NonExhaustiveMatch struct {
	Root    *parser.IfExpression // 降糖形态：match 链的根；糖形态为 nil
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
// Both shapes are collected because they are disjoint by construction: lowering
// rewrites every SurfaceMatch into an if-chain (measured — a default-shaped
// program contains 0 SurfaceMatch nodes, see
// TestNonExhaustiveMatchShapesAreDisjoint), so neither path can double-report
// the other's matches.
func CollectNonExhaustiveMatches(program *parser.Program) []NonExhaustiveMatch {
	if program == nil {
		return nil
	}
	out := collectLoweredNonExhaustiveMatches(program)
	return append(out, collectSugarNonExhaustiveMatches(program)...)
}

func collectLoweredNonExhaustiveMatches(program *parser.Program) []NonExhaustiveMatch {
	var out []NonExhaustiveMatch
	roots, rootFunc, rootFile := collectMatchRoots(program)
	for _, root := range roots {
		vs := matchVariants(root)
		// Not an option match: no arm tests ok/nil/err (bare guard blocks and
		// matches over plain values land here and stay unreported).
		if len(vs) == 0 {
			continue
		}
		// Matches synthesised by the `?=` / safe-index lowerings have no source
		// block of their own (MatchEndPos stays zero): the user cannot add arms
		// to them, so demanding completeness is both unfixable and wrong.
		if root.MatchEndPos.Line == 0 {
			continue
		}
		// Tagged-enum matches are not option matches: `q: { ok(v) -> ... fail -> ... }`
		// matches the enum's OWN variants, and `ok` is simply a variant name that
		// collides with the option vocabulary. Demanding nil/err arms there is a
		// false positive — the match is already exhaustive over its enum.
		if subjectIsDeclaredEnum(program, root, rootFunc[root]) {
			continue
		}
		// Exhaustive: terminates in a catch-all `-> ` arm, or handles every
		// option variant (ok/nil/err). err is a real variant of any ?T (std's
		// read-stdin-str returns ?str and matches err ->), so all three are
		// required — mirroring the parser's [RAL] completeness rule.
		if matchHasCatchAll(root) || (vs["ok"] && vs["nil"] && vs["err"]) {
			continue
		}
		var missing []string
		for _, v := range []string{"ok", "nil", "err"} {
			if !vs[v] {
				missing = append(missing, v)
			}
		}
		out = append(out, NonExhaustiveMatch{
			Root:    root,
			Pos:     root.Pos(),
			End:     root.MatchEndPos,
			Subject: matchSubjectName(root),
			Missing: missing,
			File:    rootFile[root],
		})
	}
	return out
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

// subjectIsDeclaredEnum reports whether the matched subject's static type is a
// user-declared enum (e.g. `e-res`). Such a match ranges over that enum's own
// variants, so the option completeness rule (ok/nil/err) does not apply.
// Only a statically known type can be excluded — unknown types stay reported,
// which is what lets call subjects (`foo(): { ok -> ... }`) still be caught.
//
// The subject is looked up with FuncVarType(funcName, ...) so function-local
// types (parameters included) are found in the per-function map first, falling
// back to the global VarTypes for module-level matches — rather than reading
// the global snapshot directly, which misses parameters that now live only in
// the per-function map.
func subjectIsDeclaredEnum(program *parser.Program, root *parser.IfExpression, funcName string) bool {
	if root == nil {
		return false
	}
	return subjectIsDeclaredEnumExpr(program, root.MatchedExpr, funcName)
}

// subjectIsDeclaredEnumExpr is the shape-independent core of
// subjectIsDeclaredEnum: both shapes hand over their own subject expression
// (IfExpression.MatchedExpr / SurfaceMatch.Matched) so the enum lookup cannot
// drift between them.
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

// matchSubjectName returns a printable name for the matched expression.
func matchSubjectName(ife *parser.IfExpression) string {
	if ife == nil || ife.MatchedExpr == nil {
		return "?"
	}
	switch m := ife.MatchedExpr.(type) {
	case *parser.Identifier:
		return m.Value
	case *parser.DotExpression:
		return m.Property
	case *parser.CallExpression:
		// e.g. `foo(): { ok -> ... }` — show the callee so the diagnostic is
		// actionable (this shape is where the parser's [RAL] check cannot
		// fire, since it only recognises plain identifiers).
		switch fn := m.Function.(type) {
		case *parser.Identifier:
			return fn.Value + "()"
		case *parser.DotExpression:
			return fn.Property + "()"
		}
	}
	return "?"
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
			if x.MatchedExpr != nil {
				all = append(all, x)
				rootFunc[x] = funcName
				rootFile[x] = file
				if c := matchChainedChild(x); c != nil {
					children[c] = true
				}
			}
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

// matchChainedChild returns the next arm of the if/else chain, i.e. the match
// arm that sits alone in this arm's else block. It returns nil when the else
// block is a real catch-all body (or absent).
func matchChainedChild(ife *parser.IfExpression) *parser.IfExpression {
	if ife == nil || ife.Alternative == nil || len(ife.Alternative.Statements) != 1 {
		return nil
	}
	es, ok := ife.Alternative.Statements[0].(*parser.ExpressionStatement)
	if !ok {
		return nil
	}
	inner, ok := es.Expression.(*parser.IfExpression)
	if !ok || inner.MatchedExpr == nil {
		return nil
	}
	return inner
}

// matchHasCatchAll reports whether the match chain terminates in an else body
// (a `-> ` wildcard arm, or any arm placed in the default slot).
func matchHasCatchAll(root *parser.IfExpression) bool {
	seen := make(map[*parser.IfExpression]bool)
	for node := root; node != nil; {
		if seen[node] {
			return true // defensive: malformed cycle
		}
		seen[node] = true
		if node.Alternative == nil {
			return false // chain ends without an else
		}
		if child := matchChainedChild(node); child != nil {
			node = child
			continue
		}
		return true // else block holds a real body
	}
	return true
}

// matchVariants collects the option variants (ok / nil / err) tested by the
// arms of a match chain. An empty result means the match is not over an option.
func matchVariants(root *parser.IfExpression) map[string]bool {
	vs := make(map[string]bool)
	seen := make(map[*parser.IfExpression]bool)
	for node := root; node != nil; {
		if seen[node] {
			break
		}
		seen[node] = true
		for _, v := range armVariantNames(node) {
			vs[v] = true
		}
		node = matchChainedChild(node)
	}
	return vs
}

// armVariantNames returns every option variant ("ok" / "nil" / "err") a single
// desugared arm claims. A combined arm (`nil || err ->`) records BOTH variants
// in OptionPatterns, so collect ALL of them: returning only the first would make
// the exhaust checker treat `nil || err` as covering just `nil` and falsely
// report the `err` arm as missing. The parser's own [RAL] completeness check
// already treats a combined pattern as covering every listed variant (see
// parser/expr.go hasErrArm), so this keeps the checker consistent with it. An
// empty result means the arm tests something other than an option variant.
func armVariantNames(ife *parser.IfExpression) []string {
	if ife == nil {
		return nil
	}
	// Combined option patterns (`nil || err`): claim every listed variant.
	if len(ife.OptionPatterns) > 0 {
		var out []string
		for _, p := range ife.OptionPatterns {
			if optionMatchVariants[p] {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if v := armVariantName(ife); v != "" {
		return []string{v}
	}
	return nil
}

// armVariantName returns the single option variant a non-combined desugared arm
// tests ("ok" / "nil" / "err"), or "" when the arm tests something else.
func armVariantName(ife *parser.IfExpression) string {
	if ife == nil {
		return ""
	}
	if id, ok := ife.EqualityPattern.(*parser.Identifier); ok && optionMatchVariants[id.Value] {
		return id.Value
	}
	// `nil ->` carries a NilLiteral pattern, not an identifier.
	if _, ok := ife.EqualityPattern.(*parser.NilLiteral); ok {
		return "nil"
	}
	// ok-> / .-> val branch: only meaningful for options.
	if ife.DotValBody != nil {
		return "ok"
	}
	// Fallback: inspect the desugared condition `matched == ok` / `matched == nil`.
	if inf, ok := ife.Condition.(*parser.InfixExpression); ok && inf.Operator == "==" {
		if id, ok := inf.Right.(*parser.Identifier); ok && optionMatchVariants[id.Value] {
			return id.Value
		}
		if _, ok := inf.Right.(*parser.NilLiteral); ok {
			return "nil"
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// S1：糖形态的 match（Parser.SkipLowering 下 match 是 *parser.SurfaceMatch）
//
// 上面的整条链路都建在**降糖后的形状**上：collectMatchRoots 走 if/else 链，
// matchVariants 沿 matchChainedChild 逐链节收变体，matchHasCatchAll 看链末端
// 有没有 else。糖形态下这些一条都找不到 match ⇒ match-nonex 静默失效（不报错，
// 是沉默），`--fix=match` 也因此补不出 arm。
//
// 做法与 match_arm_reach.go 的 collectSugarCatchAllArms 对齐：**并行收集器 +
// 共用语义助手**，而不是让 collectMatchRoots 也回传 SurfaceMatch（它的回传型别
// 是 []*parser.IfExpression）。共用的助手：collectSugarMatchRoots /
// sugarMatchVariants / sugarMatchSubjectName（match_arm_reach.go）与
// subjectIsDeclaredEnumExpr（本档）。
//
// 逐条对应降糖路径的判定：
//
//	MatchedExpr != nil                      ↔ sm.Matched != nil
//	matchVariants(root) 非空                 ↔ sugarMatchVariants(sm) 非空
//	root.MatchEndPos.Line == 0 跳过          ↔ sm.RBracePos.Line == 0 跳过
//	subjectIsDeclaredEnum(...)               ↔ subjectIsDeclaredEnumExpr(...)
//	matchHasCatchAll(root)                  ↔ sugarMatchHasCatchAll(sm)
//	root.Pos() / root.MatchEndPos           ↔ sm.Pos() / sm.RBracePos
//
// 位置等价性已实测（checker/zz_posprobe_test.go 的 TestZZPosProbe）：同一条
// match 在两种形态下 Pos / EndPos / 收尾大括号位置**逐字段相同**——糖形态的
// RBracePos 正是 lowering 转移到 IfExpression.MatchEndPos 的那一个位置。
// ─────────────────────────────────────────────────────────────────────────────

// collectSugarNonExhaustiveMatches 是 collectLoweredNonExhaustiveMatches 的
// 糖形态分支。
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

// sugarMatchHasCatchAll 是 matchHasCatchAll 的糖形态对应物：整条 match 的最后一
// 條臂是裸 `->`（IsWildcard 且不是 `.->` / `ok ->` 這種 dot-val 分支）。
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
