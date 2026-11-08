// lowering.go — lowering pass：表層 AST（SurfaceMatch）→ 核心 AST（IfExpression 鏈）。
//
// parser 在解析 match 語法（裸 match `{ cond -> body }`、matched match `x: { ... }`、
// deprecated switch/~match）時只產出表層節點 SurfaceMatch（記錄 arms 與解析期語義
// 快照），不立即 desugar。ParseProgram 結束時呼叫 lowerProgram，由 lowering pass
// 自底向上將所有 SurfaceMatch 展開為 if/elif/else 鏈（核心 AST）。
// 下游（transpiler/LSP/formatter）看到的 AST 與舊「解析期立即 desugar」實作一致。
package parser

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/ty"
)

// ---- 表層 AST 節點 ----

// SurfaceMatch — match 語法的表層 AST 節點。
// parser 只負責收集 arms 與解析期語義快照；desugar 由 lowering pass 延後執行。
type SurfaceMatch struct {
	Token   lexer.Token // LBRACE（裸 match）或 matched match 的起始 token
	Matched Expression  // 被匹配的表達式；nil 表示裸 match（條件直接使用）
	Arms    []MatchArm

	// OpeningBraceComment 保留裸 match `{` 同行註釋，lowering 時轉移到 IfExpression。
	OpeningBraceComment *CommentGroup
	// RBracePos 保留裸 match 外層 `}` 的位置，由 SurfaceMatch.EndPos() 與
	// match_exhaustive（no fix 驅動）直接取用。
	RBracePos lexer.Position

	// PreChecked 標記本 match 的「解析期/降糖前」預檢（preRegisterEnumArmBindings +
	// rejectItInNonOkArm）是否已經跑過。S1 翻轉形態後，預檢從 lowerSurfaceMatch /
	// desugarSurfaceMatch 的展開入口搬到 lowerer.walk 的 defer 分支（每個 SugarMatch
	// 在預設路徑只走一次），以此欄位避免 js/wasm 展開路徑或 H0 desugar 二次重跑時
	// 重複報出 `it` 非法使用等診斷。SkipLowering 路徑下節點從未經過 walk，PreChecked
	// 為 false，desugarSurfaceMatch 仍會補跑預檢。
	PreChecked bool

	// 表層 match 節點自身的 `#{...}`（arm 級註解會複製到臂體陳述上，
	// 而 match 節點本身也可能被搬移註解）。
	AnnotatedNode
}

func (sm *SurfaceMatch) expressionNode()     {}
func (sm *SurfaceMatch) Pos() lexer.Position { return posFromToken(sm.Token) }
func (sm *SurfaceMatch) EndPos() lexer.Position {
	// RBracePos 記錄了外層 `}` 的位置，優先使用。
	if sm.RBracePos.Line > 0 {
		return sm.RBracePos
	}
	if n := len(sm.Arms); n > 0 && sm.Arms[n-1].Body != nil {
		return sm.Arms[n-1].Body.EndPos()
	}
	return posFromToken(sm.Token)
}

// newSurfaceMatch 建立表層 match 節點（不捕獲型別快照；類型推斷由 Resolver pass
// 寫入語義副表，lowering 時自 p.sem 讀取）。
func (p *Parser) newSurfaceMatch(tok lexer.Token, matched Expression, arms []MatchArm) *SurfaceMatch {
	return &SurfaceMatch{Token: tok, Matched: matched, Arms: arms}
}

// ---- lowering pass 驅動 ----

// LowerSurface 對整棵 AST 執行 lowering pass（兩階段；見 lowerProgram 與
// DesugarSurfaceMatches）：先展開除 *SurfaceMatch 外的糖節點，再把保留的
// *SurfaceMatch 展開為 IfExpression 鏈。由 ParseProgram 在解析結束、拷貝
// Warnings 之前呼叫。
// LowerSurface runs the surface-AST lowering pass in two phases: lowerProgram
// expands every sugar node except SurfaceMatch (left intact), then
// DesugarSurfaceMatches expands those deferred SurfaceMatch nodes into the core
// AST. The split lets S1 delete the match-desugaring phase once the checker /
// transpiler / H0 consume SurfaceMatch directly. It publishes the index side
// table and finalises the compound-assignment diagnostics.
//
// This is the **single** implementation of the lowering pass. ParseProgram calls
// it unless Parser.SkipLowering is set, so the deferred path (parse with
// SkipLowering → … → LowerSurface) and the default path cannot drift apart;
// src/parser/dump/lowering_seam_test.go proves they produce byte-identical trees
// over the whole tests/ corpus.
//
// It exists as a named entry point so that lowering can be *moved* out of
// ParseProgram without changing what it does — docs/design/nolang-parser-lowering-invariants.md
// migration step S1, which needs the checker to run on the sugar-bearing AST
// before this pass executes.
//
// It expects an UNLOWERED tree. Passing an already-lowered program is harmless
// but pointless: no sugar remains to expand.
func (p *Parser) LowerSurface(program *Program) {
	if program == nil {
		return
	}
	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		fmt.Fprintf(os.Stderr, "[debug-it] LowerSurface called: %d top-level statements, filename=%s\n",
			len(program.Statements), p.Filename)
	}

	p.lowerProgram(program)
	// S1 flip (2026-10-17): the default pipeline now KEEPS *SurfaceMatch nodes
	// intact. The match is no longer expanded here — instead:
	//   - the native / H0 path desugars it directly at ASTToHIR
	//     (parser/tohir.go case *SurfaceMatch), which routes through the IR-agnostic
	//     resolve layer + emitSubjectMatchHIR;
	//   - the experimental js / wasm backends call p.DesugarSurfaceMatchesPlain(program)
	//     explicitly from their builders (build/builder.go) before codegen.
	//
	// The parser-side normalisation (safe-index subject rebind to __match_subj_*,
	// D9 place-write, D4 empty-arm default, it bindings, enum destructuring) lives
	// in the shared resolveSubjectMatch / resolveBareMatchPlan prep, so it runs on
	// BOTH the H0 and js/wasm desugar paths. P7 (design doc §5) deleted the legacy
	// buildMatchDesugar / emitSubjectMatchAST / desugarSurfaceMatch route.
	//
	// NOTE (design doc §15.6): flipping the default form is a user-visible change
	// to `no vet` output. Measured 2026-10-18 against baseline ab157ea7:
	//   - tests/*.no   : 478 files, 0 files differ.
	//   - src/std/*.no : 110 of 115 files differ — ALL of them gain HINTs only
	//     (median +7 hints/file; net modules such as http2 +695 / http +684 /
	//     ws +684 / tls +675 are large because vetting them pulls in dependency
	//     modules). 0 new ERRORs, 0 new WARNINGs, and no diagnostic disappears.
	//   The extra hints are the S4 sugar blind spot closing: with SurfaceMatch
	//   surviving to the checker, lint walkers now see inside match arms and
	//   report findings (nolang-range-index / nolang-index / …) that the lowered
	//   tree used to hide. They are true positives, not noise.
	//   (An earlier note here said "19 programs"; that count is stale.)
	if os.Getenv("NOLANG_DEBUG_SELF") != "" && strings.Contains(p.Filename, "txt") {
		dbgLeakState("POST-lower", program)
	}

	// 將「安全索引專用」本地型別表（idxLocalTypes）掛到語義副表匯出，供
	// checker 的 ValidateUnhandledIndex 複現 isSafeIndexBase 的型別查詢（parser
	// 實例在 ParseProgram 返回後即被丟棄）。
	if program.Sem != nil {
		program.Sem.IdxLocalTypes = p.idxLocalTypes
	}

	// 函式外的頂層複合賦算（沒有任何 option 結果可上拋）在此收尾定案。
	p.flushCompoundAssigns(nil)

	// 降糖階段也會產生 warning（複合賦算的棄用提示等）。預設路徑下 ParseProgram
	// 會在呼叫本函式之後才拷貝 Warnings，所以那裡自然涵蓋；但延後降糖的路徑
	//（SkipLowering）在 ParseProgram 返回時就已拷貝過一次，必須在這裡補齊，
	// 否則會靜默丟掉降糖期的診斷。
	program.Warnings = append([]string{}, p.Warnings()...)
}

// prepareInBoundsMetadata computes proof results before lowering and stores them
// in Program.Sem. The analysis remains read-only over the syntax AST; both the
// default and SkipLowering ParseProgram paths prepare the same node side-table.
func (p *Parser) prepareInBoundsMetadata(program *Program) {
	if program == nil {
		return
	}
	p.precomputeIdxLocalTypes(program)
	if program.Sem == nil {
		return
	}
	program.Sem.IdxLocalTypes = p.idxLocalTypes
	for idx := range AnalyzeInBoundsIndex(program).Reads {
		program.Sem.SetIndexInBounds(idx, true)
	}
}

// prepareInferredTypes computes the parser-side inferred-type facts for every
// LetStatement (e.g. `x = a + b` → `?int`, `x = arr[i]` → `?elem`) and publishes
// them to Program.Sem. It is purely read-only on the AST and is invoked for both
// the default and SkipLowering paths so the AST→HIR / checker / formatter see the
// same ?T regardless of which pipeline produced the program.
//
// The SkipLowering path previously saw no inferred type at all (since the
// lowering pass never ran), causing 24/532 corpus files to diverge in HIR
// `t=?int` vs no-type for let-statements (see parser/dump/s3_breakdown_test.go
// typeSynth bucket). With this preparation, both paths populate the same
// side-table entry and ASTToHIR can produce identical Type IDs.
func (p *Parser) prepareInferredTypes(program *Program) {
	if program == nil || program.Sem == nil {
		return
	}
	// Reuse the same walker as lowering so function-scope state
	// (curFuncName / declaredLocals) and surface sugar states are mirrored
	// exactly. The walker only invokes the pure analyzers below; no AST field
	// is written.
	l := &lowerer{
		p:              p,
		visited:        map[uintptr]bool{},
		declaredLocals: map[string]bool{},
		analysisOnly:   true,
	}
	l.runInferredTypeAnalysis(program)
}

func (p *Parser) lowerProgram(prog *Program) {
	if prog == nil {
		return
	}
	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		fmt.Fprintf(os.Stderr, "[debug-it] lowerProgram called, %d top-level statements\n", len(prog.Statements))
	}
	// S3: skipSugarExpand=true 讓 walk 遇 UnwrapAssign 不再原地展開回寫 AST；
	// H0 (tohir.go case *UnwrapAssignStatement → lowerUnwrapAssignToHIR) 直接把
	// 糖節點降成基礎 HIR。checker 亦已糖感知（checker.go 多處 case *UnwrapAssignStatement）。
	// 側表推斷仍在 walk 其它分支進行。D9 下標寫入目標（a[i] ?= v，uas.Name == nil）
	// 仍走原展開（lowerUnwrapAssignToIndex）。
	l := &lowerer{p: p, visited: map[uintptr]bool{}, declaredLocals: map[string]bool{}, deferMatchDesugar: true, skipSugarExpand: true}
	l.walk(reflect.ValueOf(prog))
}

// DesugarSurfaceMatchesPlain is the js/wasm re-home entry point (design doc §5
// P7). It expands every deferred SurfaceMatch into the SAME core-AST IfExpression
// chain the main pipeline gets, but the chain carries NO match-specific fields
// (MatchedExpr / RangePattern / EqualityPattern / OptionPatterns / ValuePatterns /
// RawCond / DotValBody) and NO RTFlag — only Condition / Consequence /
// Alternative, exactly what the js/wasm backends consume. This decouples the
// experimental js/wasm backends from buildMatchDesugar / emitSubjectMatchAST, so
// P7 can delete the legacy desugar without touching them.
//
// Type-destructuring matches are routed through buildTypeMatchDesugar unchanged:
// it emits a nil-Condition IfExpression that only the MIR static-dispatch path can
// lower, so js/wasm already cannot handle them — re-homing them is out of scope.
func (p *Parser) DesugarSurfaceMatchesPlain(prog *Program) {
	if prog == nil {
		return
	}
	l := &lowerer{p: p, visited: map[uintptr]bool{}, declaredLocals: map[string]bool{}, skipCollectLocalTypes: true}
	l.walk(reflect.ValueOf(prog))
}

// desugarSurfaceMatchToIfExpr lowers a *SurfaceMatch to the core-AST IfExpression
// chain, consuming the IR-agnostic resolve layer + the field-less plain emitters.
// It is the SINGLE desugar path shared by the parser walk (lowerSurfaceMatch), the
// experimental js/wasm backends (DesugarSurfaceMatchesPlain), and every internal
// caller that synthesises a match (unwrap guards, capture propagation, index
// defaulting, …). P7 (design doc §5) deleted the legacy buildMatchDesugar /
// emitSubjectMatchAST / desugarSurfaceMatch that this now subsumes; the result
// carries only Condition/Consequence/Alternative, which is all the IfExpression
// consumers (ASTToHIR's case *IfExpression, the js/wasm backends) read.
func (p *Parser) desugarSurfaceMatchToIfExpr(sm *SurfaceMatch) Expression {
	if sm.Matched == nil {
		if plan := p.resolveBareMatchPlan(sm.Token, sm.Arms); plan != nil {
			return p.emitMatchIfPlain(sm.Token, plan)
		}
		return nil
	}
	if ident, ok := sm.Matched.(*Identifier); ok {
		t := p.matchSubjectUnionType(p.curFuncName, ident.Value)
		if !hasOptionMatchArm(sm.Arms) &&
			(p.isUnionType(t) || p.armsCarryTypePatterns(sm.Arms)) {
			return p.buildTypeMatchDesugar(sm, t)
		}
	}
	if prep := p.resolveSubjectMatch(sm); p.subjectMatchDirectHIR(prep) {
		return p.emitSubjectMatchPlain(prep)
	}
	return nil
}

type lowerer struct {
	p           *Parser
	visited     map[uintptr]bool    // 指標去重：AST 有共享節點（如 MatchedExpr），避免重複遍歷
	curFuncName string              // current function being lowered, for function-scoped VarType lookup
	curFuncDef  *FunctionDefinition // current function definition, for result param lookup
	optTmpSeq   int                 // 單調計數器：為 compound `?=` 的 __opt_N 暫存變數產生全函式唯一名
	capTmpSeq   int                 // 單調計數器：為捕獲賦值的 __cap_N 暫存變數產生全函式唯一名
	// declaredLocals 記錄「本函式內已經出現過的 LetStatement 目標名」，按 walk
	// 順序累積。用途有二：
	//  1. 判斷捕獲目標是否為首次宣告 —— 只有首次宣告才需要補 `a ?T = nil`
	//     預宣告；對已存在的變數再發一次預宣告會摧毀它的既有值（`a = a + b / c`
	//     的 RHS 在 ok 臂才求值，預宣告先把 a 清成 nil，RHS 就讀到 nil）。
	//  2. 語義表（VarTypes）不記錄由字面量推斷出的型別（`mi = 0.0`），單靠它
	//     判斷「是否已宣告」會漏判，故這裡自行記錄。
	declaredLocals map[string]bool
	// analysisOnly 為 true 時，walker 只跑推斷型別分析（推斷 ?T 並寫入
	// Program.Sem 的 InferredType side-table），不做任何結構性改寫
	//（不替換 SurfaceMatch / UnwrapAssign / IndexExpression / LetStatement）。
	// 由 prepareInferredTypes 用於 SkipLowering 路徑下補齊側表，使
	// ASTToHIR / checker / formatter 看到與降糖路徑同樣的 ?T 事實。
	analysisOnly bool
	// warnMatchArms 為 true 時，analysisOnly 的走訪會在每個 SurfaceMatch
	// 上多跑一次「`->` 臂不可達」判定（parser/match_warn.go）。診斷已從降糖
	// 迴圈搬出來，故它對兩種形態都成立——見該檔案頂部的說明。
	warnMatchArms bool
	// deferMatchDesugar 為 true 時，walk 遇到介面欄位中的 *SurfaceMatch 不立即
	// 展開為 IfExpression 鏈，而是遞迴走訪其子節點（讓 arms 內的其它糖節點
	// UnwrapAssign / IndexExpression / LetStatement 仍在本輪 lowering 展開），
	// 把 SurfaceMatch→IfExpression 的展開延後到獨立的 DesugarSurfaceMatches pass。
	// 這是 S1 遷移的一步：使 match 降糖成為一個可單獨移除的 pass（未來 checker /
	// transpiler / H0 直接消費 SurfaceMatch 時即可刪除），且對 build/transpiler
	// 的 ~20 個 walker 零改動（它們仍看到 IfExpression 樹）。
	deferMatchDesugar bool
	// skipCollectLocalTypes 為 true 時，walk 在進入 FunctionDefinition 時不重跑
	// collectLocalTypes（避免 idxLocalTypes 副表被寫入兩次、因 Go map 迭代順序
	// 非確定而與單趟 lowering 產生差異）。專為 DesugarSurfaceMatches 第二階段：
	// 第一階段 lowerProgram 已經跑過 collectLocalTypes 並填好 idxLocalTypes，第二
	// 階段只需要補上 match 降糖，不該再動這張副表。其它糖節點展開與語義推斷仍照常
	// 進行（buildMatchDesugar 需要 p.sem.FuncVarType 等副表，必須保留）。
	skipCollectLocalTypes bool
	// skipSugarExpand 為 true 時，walk 遇到 UnwrapAssign / index-out / 結構捕獲
	// 等糖節點不做「原地展開回寫 AST」(v.Set 替換成 BlockStatement / match /
	// __cap_N)，只遞迴走訪其子節點；展開改由下游負責：LLVM 路徑由 H0 (ASTToHIR)
	// 直接把糖節點降成基礎 HIR，js/wasm 路徑由 DesugarSurfaceMatchesPlain 的泛化
	// 兜底 pass 展開成核心 AST。這是 S3 的目標：消除 parser 端 AST 回寫成本，以及
	// 「展開後整棵 AST 再整體轉 HIR」的冗餘轉換。注意：側表推斷（?T / 捕獲 inner
	// 型別）仍照常進行——checker / H0 需要這些事實。
	skipSugarExpand bool
	// resultNameOverride 非空時，lowerUnwrapAssign 用它作為 option 結果參數名，
	// 不再從 curFuncDef.Results 探測。H0 直出（tohir.go 的 lowerUnwrapAssignToHIR）
	// 用它傳入 hirConv.curResultName（funcLike 從 sig.Results 提取的 ?T 結果名），
	// 因為 H0 侧沒有 curFuncDef。
	resultNameOverride string

	// blockMatchSubject 非空時，這個 lowerer 正在展開被陳述層級塊 match
	// `{ body }: { arms }` 捕捉的 `?=`。lowerUnwrapAssign 會把「上拋目標」
	// 改成這個合成 subject（而非函式的 ?T 結果參數，見「最近的塊 match 捕捉」
	// 語意），且 nil/err 臂只寫入 subject、**不 return** —— 停止塊主體改由
	// block_match.go 的 `if __bm_N == nil` 守衛負責。
	blockMatchSubject string
	// unwrapTmpOverride 覆寫 `__unwrap_<line>`：同一行有兩個 `?=` 時這個
	// 名字會撞名，塊 match 改由 parse-time Subject 派生唯一名。
	// （`__match_subj_<line>_<col>` 已含欄位，不需覆寫。）
	unwrapTmpOverride string
}

var surfaceMatchPtrType = reflect.TypeOf((*SurfaceMatch)(nil))
var unwrapAssignPtrType = reflect.TypeOf((*UnwrapAssignStatement)(nil))
var statementIfaceType = reflect.TypeOf((*Statement)(nil)).Elem()

// walk 遞迴遍歷任意 AST 值。替換點是「介面欄位/介面切片元素中裝的 *SurfaceMatch」——
// AST 中 Expression/Statement 均以介面持有，且所有節點實作皆為指標，
// 因此經由指標 Elem() 抵達的介面欄位一定可設置（CanSet）。
func (l *lowerer) walk(v reflect.Value) {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		ptr := v.Pointer()
		if l.visited[ptr] {
			return
		}
		l.visited[ptr] = true
		l.walk(v.Elem())
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		// 陳述層級塊 match `{ body }: { arms }`。
		//
		// LLVM 主管線（skipSugarExpand）：**絕不 descend 進 Body**。D9 下標目標
		// `a[i] ?= v`（uas.Name == nil）在下方的 :420 會被 v.Set 原地改寫，而
		// H0 讀的是同一份 *Program；降進去會污染糖 AST。展開全權交給 H0 的
		// case *BlockMatchStatement。
		//
		// js/wasm（DesugarSurfaceMatchesPlain）：在原地換成展開後的核心 AST，
		// 再走一次 walk，讓合成的 SurfaceMatch 也降成 IfExpression 鏈。
		if v.Elem().Type() == blockMatchPtrType {
			if l.skipSugarExpand {
				return
			}
			if !l.analysisOnly && v.CanSet() {
				bm := v.Interface().(*BlockMatchStatement)
				if expanded := expandBlockMatch(l.p, bm, l.resultNameOverride); expanded != nil {
					v.Set(reflect.ValueOf(expanded))
					l.walk(v)
				}
				return
			}
			// analysisOnly：只收集推斷事實，不做任何結構改寫。
			l.walk(v.Elem())
			return
		}
		// 運算符重載派發（post-order）：先遞迴把子節點（如巢狀 `a + b`、
		// `(a + b) * c`）降成 method-call，再嘗試把「本節點」的 `x OP y`
		// 重寫為 `x._op(y)`。僅 build 路徑（非 analysisOnly）改寫 AST；
		// fmt / LSP 的 analysisOnly 路徑跳過，保留 `a + b` 原樣輸出，
		// 故格式化源碼不受影響。
		if _, ok := v.Elem().Interface().(*InfixExpression); ok {
			l.walk(v.Elem()) // 先降子節點
			if !l.analysisOnly && v.CanSet() {
				if repl, ok := l.maybeDesugarInfix(v); ok {
					v.Set(reflect.ValueOf(repl))
				}
			}
			return
		}
		if v.Elem().Type() == surfaceMatchPtrType && v.CanSet() {
			if l.analysisOnly {
				// warnMatchArms 模式：由 warnMatchArmsPass 自行走訪子節點
				//（順序與 lowerSurfaceMatch 一致），並在最後做本層判定。
				if l.warnMatchArms {
					l.warnMatchArmsPass(v.Interface().(*SurfaceMatch))
					return
				}
				// 推斷模式：仍遞迴走訪 children 以便覆蓋其中的 LetStatement，
				// 但不替換 SurfaceMatch 為 IfExpression 鏈。
				l.walk(v.Elem())
				return
			}
			if l.deferMatchDesugar {
				// S1 flip (2026-10-17): the default pipeline now KEEPS the
				// SurfaceMatch node (H0 desugars it at ASTToHIR via
				// desugarSurfaceMatch). We still recurse into its children so nested
				// sugar (UnwrapAssign / IndexExpression / LetStatement) inside the
				// arms is lowered now; nested SurfaceMatch nodes are left for the
				// separate expand pass (js/wasm) or H0.
				//
				// We must ALSO run the parser-side pre-checks here (previously only
				// done inside lowerSurfaceMatch/desugarSurfaceMatch), so `it`-
				// validation and enum destructuring bindings still fire on the sugar
				// path. Guarded by sm.PreChecked to avoid double-reporting when the
				// same node is later expanded by js/wasm / H0.
				if sm, ok := v.Elem().Interface().(*SurfaceMatch); ok {
					if !sm.PreChecked {
						l.preRegisterEnumArmBindings(sm)
						l.rejectItInNonOkArm(sm)
						sm.PreChecked = true
					}
				}
				l.walk(v.Elem())
				return
			}
			sm := v.Interface().(*SurfaceMatch)
			if lowered := l.lowerSurfaceMatch(sm); lowered != nil {
				v.Set(reflect.ValueOf(lowered))
			} else {
				v.Set(reflect.Zero(v.Type()))
			}
			return
		}
		if v.Elem().Type() == unwrapAssignPtrType && v.CanSet() {
			uas := v.Interface().(*UnwrapAssignStatement)
			// UnwrapAssignStatement 一律展開為 BlockStatement（編譯器需要展開後的
			// IR）。formatter 現走糖 AST（SkipLowering），不經此路徑，故不再需要
			// SkipUnwrapLowering 跳過——該旗標已隨 §4.3 刪除。
			if l.analysisOnly {
				l.walk(v.Elem())
				return
			}
			// S3：LLVM 路徑（skipSugarExpand=true）不再原地展開——H0 (ASTToHIR 的
			// case *UnwrapAssignStatement → lowerUnwrapAssignToHIR) 直接把糖節點降成
			// 基礎 HIR，checker 亦已糖感知（checker.go 多處 case *UnwrapAssignStatement）。
			// 仍呼叫 lowerUnwrapAssign 執行校驗（resultName 檢查、rhsDefinitelyNonOption、
			// 型別副表推斷），但**丟棄**展開結果——不 v.Set 回寫 AST。這是 S3 的核心：
			// 側表推斷與診斷在 walk 進行，結構展開在 H0。
			// 限簡單變數目標（Name != nil）：下標/欄位目標（Target != nil，D9 寫入側
			// `a[i] ?= v`）H0 尚不支援，暫時仍走原展開。
			if l.skipSugarExpand && uas.Name != nil {
				l.walk(v.Elem())
				// 執行 lowerUnwrapAssign 以收集診斷與型別副表事實，但不回寫。
				l.lowerUnwrapAssign(uas)
				return
			}
			lowered := l.lowerUnwrapAssign(uas)
			if lowered != nil {
				// UnwrapAssignStatement 降級為 BlockStatement（Statement）。
				// 若此節點位於 Statement 介面欄位（如 BlockStatement.Statements
				// 元素），可直接替換；若位於 Expression 介面欄位（如 ?= 被當作
				// 值使用，屬非法用法），BlockStatement 無法賦回 Expression 介面，
				// 此時發出解析錯誤並清零，避免 reflect.Set panic。
				if v.Type() == statementIfaceType {
					v.Set(reflect.ValueOf(lowered))
				} else {
					l.p.saveErrorf("7t6loa4e", "line %d, column %d: `?=` is a statement operator and cannot be used as an expression",
						uas.Token.Line, uas.Token.Column)
					v.Set(reflect.Zero(v.Type()))
				}
			} else {
				v.Set(reflect.Zero(v.Type()))
			}
			return
		}
		l.walk(v.Elem())
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			// D11：`#{index-out=DEF}` 已移除，安全索引只剩 option 自動傳播：
			// option 回傳函式內的裸 `x = v[5]`（arr/vec）→ 視為 `x ?= v[5]`，
			// 越界時向上傳播錯誤。
			replaced := false
			if v.Index(i).CanSet() {
				stmt := v.Index(i).Interface()
				// carryFrom 保存「未展開」的外層陳述：行註解（`#{overflow=...}`）
				// 掛在最外層陳述上（side-table 條目或 OverflowMode 欄位），
				// desugar 產生取代節點後必須由它把模式帶過去，否則註解失效。
				var carryFrom Node
				if n, isNode := stmt.(Node); isNode {
					carryFrom = n
				}
				// 展開 ExpressionStatement → AssignExpression：索引寫入
				//（`k[i] = key[i]`、`obj.field = arr[i]`）在 AST 上為
				// ExpressionStatement 包裹 AssignExpression；取出內層節點並把外層
				// 攜帶的泛型註解（如 `#{overflow=...}`）一併轉掛，使下游 pass
				// 能在正確節點上讀到 side-table。
				if es, ok := stmt.(*ExpressionStatement); ok {
					if l.p.sem != nil {
						if anns := l.p.sem.RawAnnotationsOf(es); len(anns) > 0 {
							switch tgt := es.Expression.(type) {
							case *AssignExpression:
								// 一筆就夠：`SetRawAnnotations` 寫的是節點本身
								// （`AnnotatedNode.Annotations`），`AnnotationsOf` 與
								// `RawAnnotationsOf` 讀的是同一份存儲。Stage 3 時代
								// 還要補 `ns.Annotations`（因 ResolveProgram 只對解析期
								// 直接收過註解的節點回填，而內層 ae 是 lowering 才有的），
								// 現在不存在這個差異。
								l.p.sem.SetRawAnnotations(tgt, anns)
								stmt = tgt
							case *SurfaceMatch:
								// place-match 主體是內層 SurfaceMatch；外層
								// ExpressionStatement 攜帶的泛型註解轉掛到 sm，
								// 使 buildMatchDesugar / rewriteItPlaceWrites 能讀到。
								l.p.sem.SetRawAnnotations(tgt, anns)
							}
						}
					}
				}
				// D11：徹底移除 `#{index-out=DEF}` 註解（parseAnnotationBody 對任何
				// 殘留的 index-out 鍵直接報錯 idxourmv）；安全索引預設值降糖
				//（maybeIndexOutAssign / desugarIndexOutReads）與相關函式已刪除。
				// 下方捕獲 / 推斷 pass 保持不變。
				// analysisOnly 模式（prepareInferredTypes）：推斷分析由 inferOptionArith
				// + maybeCaptureOptionAssign 內的 synthesizeNullableType 完成。
				// 有號整數 `/` `%` 本身就會產生 option（除零/溢出時為 err），
				// 因此以普通 `=` 綁定其結果時，變數型別必然是 `?T`：無需用戶
				// 顯式標註，在此就地補上推斷出的註解（等同寫 `q ?int = a / b`），
				// 讓下游 checker（ovfhndld / ovf-int-default）與 MIR（typeHint →
				// 除零守衛）走已驗證的顯式路徑。
				// fmt 路徑跳過，formatter 永遠看不到合成註解。
				// analysisOnly 模式下**必須**執行：對 SkipLowering 路徑補齊
				// `?T` 推斷事實，使 ASTToHIR 產生與預設路徑一致的 Type ID。
				// SkipUnwrapLowering 旗標已隨 §4.3 刪除，option 算術推斷在此一律進行。
				arithRoot := false
				if ls, ok := stmt.(*LetStatement); ok {
					arithRoot = l.inferOptionArith(ls)
				}
				// 捕獲賦值：`a = b + c / d` 之類「普通 `=` 綁定含可錯源的運算式」
				// 就地改寫為捕獲（a 推斷為 ?T，nil/err 存進 a，流程繼續）。
				// 必須排在 inferOptionArith 之後：根為算術的情形由它處理。
				// analysisOnly 模式：仍呼叫以執行內部的 synthesizeNullableType
				// 推斷；該函式已被加上 `if l.analysisOnly { return nil }` 提早出口，
				// 不會建構取代節點。
				// SkipUnwrapLowering / SkipSafeIndexLowering 旗標已隨 §4.3 刪除，
				// 捕獲賦值在此一律進行。
				//
				// arithRoot 必須**在此處**由 inferOptionArith 的當次回傳值帶入，
				// 不能改成進副表再讀回：判據依賴 `signedIntOperandType` 當下可見的
				// 型別集合，而 inferOptionArith 自己會 setVarType 改動那份集合
				//（`x = x + 1` 自引用），重算會得到不同答案。
				if ls, ok := stmt.(*LetStatement); ok && !replaced {
					if repl := l.maybeCaptureOptionAssign(ls, arithRoot); !l.analysisOnly && repl != nil {
						l.carryOverflowAnnotation(carryFrom, repl)
						v.Index(i).Set(reflect.ValueOf(repl))
						replaced = true
					}
				}
				// 按 walk 順序登記使用者寫下的宣告名（必須在捕獲 pass 之後：
				// 捕獲 pass 要判斷的是「本陳述之前」是否已宣告同名變數，才能決定
				// 要不要補 `a ?T = nil` 預宣告。對已存在的變數補預宣告會先把它的
				// 值清成 nil，`a = a + b / c` 的 RHS 在 ok 臂才求值，就讀到 nil）。
				// 合成陳述（desugar 產物）不算使用者宣告。
				if ls, ok := stmt.(*LetStatement); ok && ls.Name != nil && !ls.IsSynthetic {
					l.declaredLocals[ls.Name.Value] = true
				}
				// 多返回值解構（`q, r = a.div-mod(b)`）：把每個目標按被呼叫者宣告的
				// 回傳型別順位登記進函數作用域型別表。缺這一步，之後 `q % b` 的
				// 運算符重載探測（resolveReceiverType → FuncVarType）查不到 `q`，
				// 便放棄改寫；InfixExpression 一路活到 MIR，對結構體 emit 原生
				// srem／add —— 輕則 LLVM operand-type 錯誤，重則 toolchain abort
				// （實測 tmp/bx/d1.no、d2.no；型別本身 checker 是知道的，
				// `q.abs-cmp(r)` 能過 vet，缺的只是這張派發表）。
				if mas, ok := stmt.(*MultiAssignStatement); ok {
					l.registerMultiAssignTargetTypes(mas)
				}
			}
			l.walk(v.Index(i))
			_ = replaced
		}
	case reflect.Struct:
		t := v.Type()
		// Detect FunctionDefinition to track current function name for
		// function-scoped variable type lookups during match desugaring.
		var savedFuncName string
		var savedFuncDef *FunctionDefinition
		var savedDeclared map[string]bool
		var savedPFuncName string
		isFuncDef := false
		if v.CanAddr() {
			if fd, ok := v.Addr().Interface().(*FunctionDefinition); ok {
				savedFuncName = l.curFuncName
				savedFuncDef = l.curFuncDef
				savedDeclared = l.declaredLocals
				// p.curFuncName 必須與 l.curFuncName 同步：isSafeIndexBase 等函式
				// 作用域查詢（idxLocalTypes / FuncVarType）以它為鍵。過去只有
				// 安全索引降糖 pass 會順手同步它，SkipLowering 形態
				//（prepareInferredTypes 的 analysisOnly 走查）整個函式體都在錯誤的
				//（或上一個函式殘留的）作用域裡做索引查詢——巢狀安全索引捕獲因此
				// 漏記錄（captureFactsLen=0）、checker 的 S3-c 護欄也隨之落空，
				// LSP 診斷與 build 不一致。在此入口統一同步、出口還原，
				// full / H0 / SkipLowering 三形態一致。
				savedPFuncName = l.p.curFuncName
				l.p.curFuncName = fd.Name
				l.curFuncName = fd.Name
				l.curFuncDef = fd
				// 已宣告名單同樣獨立作用域：參數先入帳，函式內同名變數才不會
				// 被誤判為首次宣告。
				l.declaredLocals = map[string]bool{}
				for _, prm := range fd.Parameters {
					if prm != nil && prm.Name != "" {
						l.declaredLocals[prm.Name] = true
					}
				}
				// 預掃描區域變數容器型別（如 `av = a.to-vec()`），使其被安全索引
				// 降級辨識（isSafeIndexBase / inferIndexElemType 依賴 FuncVarType）。
				if !l.skipCollectLocalTypes {
					l.collectLocalTypes(fd)
				}
				// Bounds proof metadata was prepared for the whole Program before
				// this lowering walk, after precomputeIdxLocalTypes populated the table.
				isFuncDef = true
			}
		}
		// 非導出欄位一律跳過（AST 子節點欄位均導出；MatchArm 由 lowerSurfaceMatch
		// 顯式處理）。欄位索引按型別緩存，避免每個節點重複 t.Field(i)。
		for _, i := range exportedFieldIndices(t) {
			l.walk(v.Field(i))
		}
		if isFuncDef {
			l.curFuncName = savedFuncName
			l.curFuncDef = savedFuncDef
			l.declaredLocals = savedDeclared
			l.p.curFuncName = savedPFuncName
		}
		// 標籤條件迴圈包裝（`#N cond: { body }`）：解析期只掛上 SurfaceMatch，
		// lowering 後補齊 Body = IfExpression.Consequence（與舊解析期行為一致）。
		// analysisOnly 模式跳過此結構修補。
		if !l.analysisOnly && v.CanAddr() {
			if fs, ok := v.Addr().Interface().(*ForStatement); ok {
				if fs.IsCondWrapper && fs.Body == nil {
					if ife, ok := fs.Condition.(*IfExpression); ok {
						fs.Body = ife.Consequence
					}
				}
			}
		}
	}
}

// runInferredTypeAnalysis walks the program invoking the same analyzer pipeline
// that the lowering pass uses for inferOptionArith / synthesizeNullableType
// (via maybeCaptureOptionAssign's root-typing branch) but in analysisOnly mode:
// no AST field is written, no sugar node is replaced. The inferred ?T is
// published to Program.Sem's type-facts table via publishInferredType so the
// SkipLowering path produces the same HIR / checker / formatter output as the
// default path.
func (l *lowerer) runInferredTypeAnalysis(program *Program) {
	if program == nil {
		return
	}
	l.walk(reflect.ValueOf(program))
}

// maybeDesugarInfix 嘗試把介面 v 持有的 InfixExpression 重寫為對應的運算符重載
// 方法呼叫。呼叫此函數前，v 的子節點（Left / Right 中的巢狀 InfixExpression）
// 必須已先降成 method-call（post-order），否則左操作數型別可能尚未就位而探測失敗。
// v 必須是持有 *InfixExpression 的介面。傳回取代節點與是否成功改寫。
func (l *lowerer) maybeDesugarInfix(v reflect.Value) (Node, bool) {
	inf, ok := v.Interface().(*InfixExpression)
	if !ok || inf == nil {
		return nil, false
	}
	// 非可重載運算符（比較、位運算等）直接跳過，保留原 InfixExpression。
	if _, ok := OperatorMethod(inf.Operator); !ok {
		return nil, false
	}
	// 探測左操作數靜態型別是否定義對應運算符重載方法；無則不改寫。
	rt := l.p.OperatorOverloadReturnType(inf)
	if rt == "" {
		return nil, false
	}
	method, _ := OperatorMethod(inf.Operator)
	repl := &CallExpression{
		Token: inf.Token,
		Function: &DotExpression{
			Token:    inf.Token,
			Receiver: inf.Left,
			Property: method,
		},
		Arguments: []Expression{inf.Right},
	}
	return repl, true
}

// registerMultiAssignTargetTypes 把 `q, r = call()` 的每個裸識別符目標，按被呼叫
// 者宣告的回傳型別順序登記進「函數作用域型別表」（sem.FuncVarTypes + VarTypes）。
//
// 為什麼要在這裡登記：運算符重載的派發（`q % b` → `q._mod(b)`）以接收者的**靜態
// 型別**為唯一判據，而它查的是 parser 自己的函數作用域表。那張表由 LetStatement
// 的宣告／推斷、match 臂綁定、`#{index-out}` 暫存、for-range 變數等各自寫入，唯獨
// 漏了多返回值解構 —— 於是一個剛被正规定型成 bigint 的變數，在派發眼裡等於沒有
// 型別，探測回 ""，改寫被跳過，中綴運算帶著結構體操作數一路走到 MIR。
//
// 三條「不亂寫」的邊界：
//   - 只認裸識別符（`a[i], n = ...` 的索引目標與 `_` 佔位直接跳過）；
//   - 目標數必須與查到的回傳型數完全相等，否則整條不登記（寧可漏報，不要把
//     第 k 個型別安到第 j 個名字上）；
//   - 函數作用域已有條目就不覆寫（參數同名、先前已綁定等用戶意圖優先）。
//     這裡刻意只看**本函數**那一層，不看 FuncVarType 的全域回退：全域有個同名
//     `q` 不構成本函數內 `q` 的型別。
func (l *lowerer) registerMultiAssignTargetTypes(mas *MultiAssignStatement) {
	if mas == nil || len(mas.Targets) < 2 || mas.Value == nil {
		return
	}
	call, ok := unwrapLowerExpr(mas.Value).(*CallExpression)
	if !ok || call == nil {
		return
	}
	rets := l.p.multiReturnTypesFromCallExpr(call)
	if len(rets) != len(mas.Targets) {
		return
	}
	saved := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	defer func() { l.p.curFuncName = saved }()
	local := l.p.sem.FuncVarTypes[l.curFuncName]
	for i, tgt := range mas.Targets {
		id, ok := tgt.(*Identifier)
		if !ok || id.Value == "" || id.Value == "_" {
			continue
		}
		if rets[i] == "" {
			continue
		}
		if _, exists := local[id.Value]; exists {
			continue
		}
		l.p.sem.SetFuncVarType(l.curFuncName, id.Value, rets[i])
		l.p.setVarType(id.Value, rets[i])
		l.declaredLocals[id.Value] = true
	}
}

// collectLocalTypes 預掃描函數體，將「未標型別、但可由 RHS 推導容器型別」的
// 區域變數（如 `av = a.to-vec()`）記入函數作用域 FuncVarTypes，使安全索引
// 降級（isSafeIndexBase / inferIndexElemType）能辨識這類 vec/slice 基底。
// 否則其型別在解析期未知 → 安全索引降級不觸發 → vec/slice 索引仍走硬
// bounds_check，越界時崩潰（違反「一定要安全」要求）。目前處理 to-vec()
// 轉換（stdsig：[n]t.to-vec -> []t），回傳 slice 型別。
func (l *lowerer) collectLocalTypes(fd *FunctionDefinition) {
	if fd == nil || fd.Body == nil {
		return
	}
	savedP := l.p.curFuncName
	savedL := l.curFuncName
	l.p.curFuncName = fd.Name
	l.curFuncName = fd.Name
	localTypes := map[string]string{}
	// 參數型別：明確標註的容器型別（arr [N]T / []T / vec[T]）同樣記入「安全索引
	// 專用」本地型別表，使 `key[i]`（key 為參數）也能被 isSafeIndexBase 辨識、
	// 走安全降級（否則降級在 ResolveProgram 之前執行，參數型別尚未進入
	// sem.FuncVarType，isSafeIndexBase 查不到 → 參數基底的越界索引無法被
	// `#{index-out}` 處理，與 checker 的判定（ResolveProgram 之後，含參數型別）
	// 不一致，導致標註失效、索引仍被報未處理）。
	for _, prm := range fd.Parameters {
		if prm.Type == nil {
			continue
		}
		nm := prm.Name
		if nm == "" {
			continue
		}
		lt := typeString(prm.Type)
		if lt != "" {
			localTypes[nm] = lt
			l.recordIndexLocalType(fd.Name, nm, lt)
		}
	}
	// 掃描「同一函數作用域內」所有 LetStatement（含巢狀區塊 / 迴圈體 / if/match 臂體，
	// 但不跨越巢狀函式定義）。原本只掃描函式體頂層陳述，導致巢狀區塊內宣告的定長陣列
	//（如 tls.no prf 的 `p-tmp [32]byte` 位於 `(out-off < out-len) { ... }` 條件區塊內）
	// 不會記入 idxLocalTypes → isSafeIndexBase 查不到其容器型別 → 該陣列的 in-bounds
	// 讀取無法被推斷、配套 `#{index-out}` 註解也無法被 `no fmt` 移除。仍按 fd.Name 分鍵、
	// 不跨越巢狀函式，故不會造成跨函數 / 跨模組型別污染。
	for _, ls := range gatherLetsInFuncScope(fd.Body) {
		if ls == nil || ls.Name == nil {
			continue
		}
		name := ls.Name.Value
		if name == "" || ls.IsSynthetic {
			continue
		}
		if _, exists := localTypes[name]; exists {
			continue
		}
		// 1) 顯式型別標註：arr [N]T / x []T / x vec[T]
		if ls.Type != nil {
			lt := typeString(ls.Type)
			if lt != "" {
				localTypes[name] = lt
				// 寫入「安全索引專用」的解析器本地型別表（idxLocalTypes），
				// 不污染 p.sem.FuncVarTypes / VarTypes（後兩者會被 codegen
				// 讀取，寫入 `a`/`av` 等常見名會竄改 std 函式如 str.to-i8 的
				// 區域變數型別，造成跨函數型別污染崩潰）。
				l.recordIndexLocalType(fd.Name, name, lt)
			}
			continue
		}
		// 2) RHS 為 to-vec()：接收者須為已知容器，元素型別取其元素。
		if elem := l.toVecElemType(ls.Value, localTypes); elem != "" {
			lt := "[]" + elem // to-vec 回傳 slice
			localTypes[name] = lt
			l.recordIndexLocalType(fd.Name, name, lt)
			continue
		}
		// 2b) RHS 為切片表達式 `base[a..b)` / `base[..]`：parseLetStatement 刻意
		// 不為 SliceExpression 推斷 SliceType（「切片表達式總是走 clone 路徑，
		// 型別由 varLLVMType 從 RHS 推導」），所以 h 在 sem.VarTypes /
		// FuncVarTypes / idxLocalTypes 三層查表全 miss。後果：
		// `h = IV[..8); h[i]: { ok -> it = 999 -> _ }` 的 isSafeIndexBase 回
		// false，resolveSubjectMatch 不做索引主體歸一化，整條 option 位置寫被
		// 靜默丟棄（tests/slice-option-write.no 4 個 FAIL，顯式標註 `h []u64`
		// 則正常）。這裡只補「安全索引專用」的 idxLocalTypes 條目，不改
		// stmt.Type / sem.VarTypes，故不影響 codegen 的 slice-view clone 路徑。
		// str/txt 切片回傳的是 str/txt（不是 []char），以 `[` / `vec[` 前綴
		// 限制在位置型容器，避免把它們誤登記成 []char。
		if se, isSlice := ls.Value.(*SliceExpression); isSlice {
			if base := dotReceiverName(se.Left); base != "" {
				src := localTypes[base]
				if src == "" && l.p.sem != nil {
					if t, ok := l.p.sem.FuncVarType(fd.Name, base); ok {
						src = t
					}
					if src == "" {
						src = l.p.sem.VarTypeRaw(base)
					}
				}
				baseRaw := ty.StripOptionRaw(src)
				if isPositionContainerRaw(baseRaw) {
					if elem := containerElemType(baseRaw); elem != "" {
						lt := "[]" + elem
						localTypes[name] = lt
						l.recordIndexLocalType(fd.Name, name, lt)
						continue
					}
				}
			}
		}
		// 3) RHS 為方法 receiver self（源碼 `parts = .`，parser 把獨立 `.` 解析成
		// Identifier("self")）：登记方法 receiver 的容器型別。方法名形如
		// `[]txt.join` / `str.upper`，最後一個 '.' 之前即 receiver 型別。
		// 不登记的後果：`parts[i]: { ok -> … }` 的安全索引歸一化
		// （isSafeIndexBase / inferIndexElemType 三層查表——idxLocalTypes /
		// sem.FuncVarType / 全域 VarTypes——全部 miss，self 的型別任何一層都
		// 推不出）→ match 退化、全臂失效（2026-11-03 match-index-option 的
		// []txt.join 失敗根因：std join 內 `parts = .` 後逐元素 match 拿到空串，
		// 輸出 "..."）。非容器 receiver（str 等）登记後 containerElemType 為空、
		// isSafeIndexBase 仍 false，無副作用。
		if id, isID := ls.Value.(*Identifier); isID && id.Value == "self" {
			// 方法 receiver 的真實型別以合成的 self 結果參數為準，不能從 fd.Name 反推：
			// 泛型方法經 monomorphizeGenerics 單態化後會被改名，例如 `[]txt.join`
			// → `_xtxt.join`，`fd.Name[:LastIndex(".")]` 得到 `_xtxt`，
			// containerElemType 回空 → 不登记 → 方法體內 `parts[i]: { ok -> … }`
			// 的索引 option 型別推不出，安全索引歸一化失效（2026-11-05
			// match-index-option.no 的 []txt.join 印出 "..." 而不是 "a.b.c.d"）。
			// self 參數由 decl.go 插入、substituteType 會把 `[]t` 換成 `[]txt`，
			// 對模板與單態化實例都給出同一個正確答案。
			recv := ""
			if fd.IsMethodDef && len(fd.Results) > 0 {
				if r := fd.Results[0]; r != nil && r.Name == "self" && r.Type != nil {
					recv = typeString(r.Type)
				}
			}
			if recv == "" {
				if ridx := strings.LastIndex(fd.Name, "."); ridx > 0 {
					recv = fd.Name[:ridx]
				}
			}
			if recv != "" && containerElemType(recv) != "" {
				localTypes[name] = recv
				l.recordIndexLocalType(fd.Name, name, recv)
			}
		}
	}
	l.p.curFuncName = savedP
	l.curFuncName = savedL
}

// gatherMultiAssignsInFuncScope 與 gatherLetsInFuncScope 同構，收集的是
// *MultiAssignStatement（`_, mb = f(...)` 這類多目標賦值）。分開一個函式是因為
// 兩者節點型別不同，而收集口徑（同作用域、遇巢狀函式停、按指標去重）必須一致。
func gatherMultiAssignsInFuncScope(body *BlockStatement) []*MultiAssignStatement {
	var out []*MultiAssignStatement
	if body == nil {
		return out
	}
	seen := map[uintptr]bool{}
	var walk func(rv reflect.Value)
	walk = func(rv reflect.Value) {
		switch rv.Kind() {
		case reflect.Ptr:
			if rv.IsNil() {
				return
			}
			ptr := rv.Pointer()
			if seen[ptr] {
				return
			}
			seen[ptr] = true
			if _, isFn := rv.Interface().(*FunctionDefinition); isFn {
				return
			}
			if ma, isMA := rv.Interface().(*MultiAssignStatement); isMA {
				out = append(out, ma)
			}
			walk(rv.Elem())
		case reflect.Interface:
			if rv.IsNil() {
				return
			}
			walk(rv.Elem())
		case reflect.Slice:
			for i := 0; i < rv.Len(); i++ {
				walk(rv.Index(i))
			}
		case reflect.Struct:
			t := rv.Type()
			for _, i := range exportedFieldIndices(t) {
				walk(rv.Field(i))
			}
		}
	}
	walk(reflect.ValueOf(body))
	return out
}

// gatherLetsInFuncScope 以反射走訪 body 子樹，依原始碼順序回傳「本函數作用域內」
// （含巢狀區塊 / 迴圈體 / if-match 臂體）的所有 *LetStatement；遇到巢狀
// FunctionDefinition（閉包 / 巢狀函式）即停止下钻——那是另一個作用域、另有自己的
// idxLocalTypes 鍵。反射僅讀取導出欄位，並按指標去重（AST 可能有共享節點）。
func gatherLetsInFuncScope(body *BlockStatement) []*LetStatement {
	var out []*LetStatement
	if body == nil {
		return out
	}
	seen := map[uintptr]bool{}
	var walk func(rv reflect.Value)
	walk = func(rv reflect.Value) {
		switch rv.Kind() {
		case reflect.Ptr:
			if rv.IsNil() {
				return
			}
			ptr := rv.Pointer()
			if seen[ptr] {
				return
			}
			seen[ptr] = true
			if _, isFn := rv.Interface().(*FunctionDefinition); isFn {
				return // 巢狀函式：獨立作用域，不下钻
			}
			if ls, isLet := rv.Interface().(*LetStatement); isLet {
				out = append(out, ls)
			}
			walk(rv.Elem())
		case reflect.Interface:
			if rv.IsNil() {
				return
			}
			walk(rv.Elem())
		case reflect.Slice:
			for i := 0; i < rv.Len(); i++ {
				walk(rv.Index(i))
			}
		case reflect.Struct:
			t := rv.Type()
			for _, i := range exportedFieldIndices(t) {
				walk(rv.Field(i))
			}
		}
	}
	walk(reflect.ValueOf(body))
	return out
}

// gatherFuncDefs 以反射走訪整棵程式樹，回傳其中所有的 *FunctionDefinition
// （含巢狀函式 / 閉包——它們各有自己的 idxLocalTypes 鍵）。只讀導出欄位、
// 按指標去重（AST 是 DAG，可能有共享節點）。
//
// 存在的理由：idxLocalTypes 的填寫（collectLocalTypes）原本只在降糖 pass 的
// reflection walk 裡發生。跳過降糖時那趟 walk 不跑，表就是空的，所有以它為鍵
// 的規則（idxwrite / idxhndld / rngidx）會**靜默失效**——不是報錯，是沉默。
// 這張表描述的是**原始碼的事實**（哪個區域變數是容器），不是降糖的產物，
// 因此它必須能在不降糖的前提下算出來。見設計稿 §15.2(1)。
func gatherFuncDefs(root interface{}) []*FunctionDefinition {
	var out []*FunctionDefinition
	if root == nil {
		return out
	}
	seen := map[uintptr]bool{}
	var walk func(rv reflect.Value)
	walk = func(rv reflect.Value) {
		switch rv.Kind() {
		case reflect.Ptr:
			if rv.IsNil() {
				return
			}
			ptr := rv.Pointer()
			if seen[ptr] {
				return
			}
			seen[ptr] = true
			if fd, isFn := rv.Interface().(*FunctionDefinition); isFn {
				out = append(out, fd)
			}
			walk(rv.Elem())
		case reflect.Interface:
			if rv.IsNil() {
				return
			}
			walk(rv.Elem())
		case reflect.Slice:
			for i := 0; i < rv.Len(); i++ {
				walk(rv.Index(i))
			}
		case reflect.Struct:
			t := rv.Type()
			for _, i := range exportedFieldIndices(t) {
				walk(rv.Field(i))
			}
		}
	}
	walk(reflect.ValueOf(root))
	return out
}

// precomputeIdxLocalTypes 在不執行降糖 pass 的前提下算出 idxLocalTypes。
//
// 為什麼需要獨立入口：collectLocalTypes 只**讀** AST（收集函式作用域內的容器
// 型別區域變數），卻只被 lowerer.walk 呼叫。於是「跳過降糖」與「有這張表」
// 被意外綁在一起——S1 要把 checker 移到降糖之前，就會踩到這個耦合。
//
// 行為等價性：預設路徑**不呼叫**本函式（它仍走 lowerProgram 內的原地呼叫），
// 所以降糖路徑的行為逐位元不變；本函式只在 SkipLowering 分支補跑同一批
// collectLocalTypes，順序與 lowerer.walk 的「先預掃描、再遞進欄位」一致
// （即兩者都是對**未降糖**的函式體做預掃描）。
func (p *Parser) precomputeIdxLocalTypes(prog *Program) {
	if prog == nil {
		return
	}
	l := &lowerer{p: p, visited: map[uintptr]bool{}, declaredLocals: map[string]bool{}}
	for _, fd := range gatherFuncDefs(prog) {
		l.collectLocalTypes(fd)
	}
}

// toVecElemType 從 `a.to-vec()` / `a.to-vec` 這類 RHS 推導容器元素型別。
// to-vec 在固定陣列 [n]t 上回傳 []t（stdsig_gen.go），故元素取接收者陣列/
// 切片/vec 的元素型別。
func (l *lowerer) toVecElemType(val Expression, localTypes map[string]string) string {
	var recvName string
	switch v := val.(type) {
	case *CallExpression:
		de, ok := v.Function.(*DotExpression)
		if !ok || de.Property != "to-vec" {
			return ""
		}
		recvName = dotReceiverName(de.Receiver)
	case *DotExpression:
		if v.Property != "to-vec" {
			return ""
		}
		recvName = dotReceiverName(v.Receiver)
	default:
		return ""
	}
	if recvName == "" {
		return ""
	}
	recvType := ""
	if t, ok := localTypes[recvName]; ok {
		recvType = t
	} else if t, ok := l.p.sem.FuncVarType(l.curFuncName, recvName); ok {
		recvType = t
	} else if t, ok := l.p.sem.VarTypes[recvName]; ok {
		recvType = t.Raw()
	}
	return containerElemType(ty.StripOptionRaw(recvType))
}

// dotReceiverName 返回 `a` / `a.field` 這類接收者表達式中最左識別符名稱。
func dotReceiverName(recv Expression) string {
	switch r := recv.(type) {
	case *Identifier:
		return r.Value
	case *DotExpression:
		return dotReceiverName(r.Receiver)
	}
	return ""
}

// recordIndexLocalType 將區域變數容器型別寫入「安全索引專用」的解析器本地
// 型別表 idxLocalTypes（按函數作用域）。此表僅供 isSafeIndexBase /
// inferIndexElemType 查詢，不寫入 sem.FuncVarTypes / VarTypes，故不會影響
// codegen 對 std 函式區域變數的型別推斷（避免跨函數型別污染崩潰）。
func (l *lowerer) recordIndexLocalType(funcName, name, lt string) {
	if l.p.idxLocalTypes == nil {
		l.p.idxLocalTypes = make(map[string]map[string]*ty.Type)
	}
	if l.p.idxLocalTypes[funcName] == nil {
		l.p.idxLocalTypes[funcName] = make(map[string]*ty.Type)
	}
	l.p.idxLocalTypes[funcName][name] = tyPool.Parse(lt)
}

// inferOptionArith — 有號整數 `+ - * / %`（D6）本身產生 option（溢位／除零為
// err），故普通 `=` 綁定其結果時變數型別必然是 `?T`：無需用戶顯式標註，
// 在 lowering 階段把推斷出的 `?T` 型別字串記入 Program.Sem 的推斷型別
// side-table，並註冊變數型別，使 checker（ovfhndld declaredOption 豁免、
// ovf-int-default lint）與 MIR（KLet typeHint → 溢位／除零守衛）完全復用顯式
// 路徑 `q ?int = a + b`。P1：不再就地寫 `s.Type`（即不再於 lowering pass 內
// 變更 AST 語法節點），所有事實留在語義副表，由 AST→HIR / checker / formatter
// 顯式讀取。推斷不出（運算元型別未知／非有號整數）時保守不改寫，維持原有行為。
//
// D1：`#{overflow=...}` 註解不再阻止本推斷 —— 註解只影響「失敗時怎麼取值」的
// 行為（wrap／clamp0／…），不再把結果型別降級成 plain int。註解本身由
// carryOverflowAnnotation 原樣保留，MIR 端以 Inst.OvfAnnotated 消費（結果仍是
// %option，只是溢位臂不再點亮 tag=2）。字串拼接的 `+` 由 signedIntOperandType
// 的型別判定自然排除（str 非有號整數）。
//
// 回傳值 = 「本陳述的根-算術推斷是否成立」。呼叫端（walker）把它原值轉交給
// `maybeCaptureOptionAssign` 作短路判據，取代 S3 時期的 `InferredByOptionArith`
// 持久位（見 maybeCaptureOptionAssign 的註釋）。
func (l *lowerer) inferOptionArith(s *LetStatement) bool {
	if s == nil || s.IsSynthetic || s.Name == nil || s.Value == nil {
		return false
	}
	// 顯式標註（含 ?T）一律不碰：用戶意圖優先，也保證冪等（重解析不重複改寫）。
	if s.Type != nil {
		return false
	}
	// 目標已宣告為非 option（`x i64 = 0` 之後的 `x = a + b`、純量出參 `r i64`）
	// → 不改寫。合成 `?T` 會就地改掉一個已存在變數的型別：它先前的所有普通值
	// 使用會突然拿到 option，且出參的 option 會被靜默取 payload 回傳（錯誤被丟
	// 棄）。交由既有診斷處理，維持與 maybeCaptureOptionAssign 同一條規則
	//（captureTargetAllowsOption）。
	if !l.captureTargetAllowsOption(s) {
		return false
	}
	inf, ok := unwrapLowerExpr(s.Value).(*InfixExpression)
	if !ok || !isOptionArithOperator(inf.Operator) {
		return false
	}
	lt, okLT := l.signedIntOperandType(inf.Left)
	rt, okRT := l.signedIntOperandType(inf.Right)
	if !okLT || !okRT {
		return false
	}
	base := lt
	if len(rt) > len(base) {
		base = rt
	}
	// 推斷結果改走語義副表（Program.Sem 的 typeFacts），不再寫 AST。
	l.publishInferredType(s, "?"+base)
	return true
}

// synthesizeNullableType 把推斷出的 `?inner` 型別字串記入 Program.Sem，
// 不再就地寫 AST `s.Type`。下游 checker（ovfhndld/idxhndld 的 declaredOption
// 豁免）與 MIR（KLet typeHint）由 AST→HIR 透過語義副表讀到同一事實，
// 完全復用「用戶顯式寫 ?T」的已驗證路徑。
//
// 與 `inferOptionArith` 的差別只剩「誰寫的」：本函式在**結構捕獲期間**對
// 索引根／混合運算元根合成型別，該路徑仍需要結構 desugar（構造 `__cap_N`
// 暫存），故呼叫端不會把它當成根-算術短路依據 —— 短路判據由 walker 從
// `inferOptionArith` 的回傳值傳遞，與本函式無關。
func (l *lowerer) synthesizeNullableType(s *LetStatement, inner string) {
	if s == nil || s.Name == nil || inner == "" {
		return
	}
	l.publishInferredType(s, "?"+inner)
}

// publishInferredType 為 s 推斷 ?T 型別字串並同時登記到語義副表 + VarTypes。
// 這是把原本 `s.Type = &NullableType{IsInferred: true}; setVarType(...)` 的
// 「AST 寫」/「副表寫」兩步合成為單一副表操作；前者已被 P1 禁止。
//
// 2026-10-26：本函式不再區分寫入來源。原本的 `byArith` 參數只為了拉起
// `InferredByOptionArith` 這個持久位，而該位的唯一消費者是
// `maybeCaptureOptionAssign` 的根-算術短路 —— 那條短路現在由 walker 把
// `inferOptionArith` 的回傳值直接傳參，不再經副表。故副表只剩「推斷出什麼」，
// 不再記錄「誰推斷的」。
func (l *lowerer) publishInferredType(s *LetStatement, typ string) {
	if s == nil || s.Name == nil || typ == "" || l == nil || l.p == nil {
		return
	}
	if l.p.sem != nil {
		l.p.sem.SetInferredType(s, typ)
	}
	l.p.setVarType(s.Name.Value, typ)
}

// captureNode 描述一個「可錯源」子表達式：它在運行期可能產生 nil/err。
type captureNode struct {
	expr  Expression // 原始子表達式，會被綁定到 __cap_N
	inner string     // option 內部型別 T
	name  string     // 綁定用的臨時變數名 __cap_N
}

// captureInfixInner 回報 `a + b` / `a - b` / `a * b` / `a / b` / `a % b` 這類
// 自身即為可錯源的 Infix 的內部型別（提升後的有號整型）。非 D6 涵蓋的算術運算子、
// 或運算元型別不可判定時回傳 ""。
//
// D6（2026-10-12）：運算子集合由 `/ %` 放寬為 `+ - * / %` —— 有號整數的這五個運算
// 一律可能溢位（`/ %` 另有除零），故自身即為 option 來源。原本的註解寫著「純算術
// 本身不是可錯源，否則所有算術都會變成 option」，正是本次要取消的舊規則。
// 無號整數、浮點、`str` 拼接、`char` 算術仍由 signedIntOperandType 排除。
func (l *lowerer) captureInfixInner(inf *InfixExpression) string {
	if inf == nil || !isOptionArithOperator(inf.Operator) {
		return ""
	}
	lt, okLT := l.signedIntOperandType(inf.Left)
	rt, okRT := l.signedIntOperandType(inf.Right)
	if !okLT || !okRT {
		return ""
	}
	if len(rt) > len(lt) {
		return rt
	}
	return lt
}

// callOptionInner 回報回傳 option 的呼叫的內部型別；非 option 回傳 ""。
func (l *lowerer) callOptionInner(c *CallExpression) string {
	if c == nil || l.p == nil {
		return ""
	}
	inferred := l.p.inferTypeFromCallExpr(c)
	if inferred != "" && ty.IsOptionRaw(inferred) {
		return inferred[1:]
	}
	return ""
}

// commonCaptureInner 回傳捕獲節點們一致的內部型別；為空、型別不一致
// （混合整/浮點等，理論上被 checker 擋）或無節點時回傳 ""，讓呼叫端保留
// 舊的預設（算術根 = i64）。呼叫端（maybeCaptureOptionAssign 的算術根
// inner 補正）只對結果型別 ≠ i64 的算術（浮點、str 拼接）採用它。
func commonCaptureInner(nodes []captureNode) string {
	common := ""
	for _, n := range nodes {
		if n.inner == "" {
			return ""
		}
		if common == "" {
			common = n.inner
		} else if common != n.inner {
			return ""
		}
	}
	return common
}

// intOperandTypeName 回傳運算元的整數型別名（任意寬度，含 byte/u8 等無號型別）。
// 與 signedIntOperandType 的差別：不限定有號、索引讀取按其元素型別解析
// （`hx[0][1]` 於 [][]byte 上回 "byte"）。型別不可判定時回 ""（呼叫端保留
// i64 慣例）。這是混型算術「結果取第一個運算元型別」規則的型別查詢端。
func (l *lowerer) intOperandTypeName(e Expression) string {
	switch x := unwrapLowerExpr(e).(type) {
	case *IntegerLiteral:
		return "i64"
	case *Identifier:
		name := x.Value
		if name == "self" || name == "." {
			if t, ok := l.lowerSelfType(); ok && t != "" {
				return ty.StripOptionRaw(t)
			}
			return ""
		}
		if l.p.sem != nil {
			if t, has := l.p.sem.FuncVarType(l.curFuncName, name); has && t != "" {
				return ty.StripOptionRaw(t)
			}
		}
		return ""
	case *IndexExpression:
		// indexContainerType 回傳的正是「這個讀取表達式本身的型別」
		// （IndexExpression 情形已剝過一層容器）：`hx[0][0]` 於 [][]byte 上
		// 直接回 "byte"。
		return l.indexContainerType(x)
	case *InfixExpression:
		// 巢狀算術運算元：其值為 option 的內部 payload，型別遞迴取其左運算元
		// （與本規則的根層判定一致）。
		if !isOptionArithOperator(x.Operator) {
			return ""
		}
		return l.intOperandTypeName(x.Left)
	}
	return ""
}

// indexContainerType 回傳「對 e 做下標讀取時的容器型別」，支援嵌套索引鏈：
// `hx`（[][]byte）→ "[][]byte"；`hx[0]` → 其元素型別 "[]byte"，恰為內層
// 讀取的容器。Identifier 走函數作用域語義表；IndexExpression 遞迴；點運算
// 解 struct 欄位。不可判定回 ""。
func (l *lowerer) indexContainerType(e Expression) string {
	switch b := unwrapLowerExpr(e).(type) {
	case *Identifier:
		if l.p.sem != nil {
			if t, has := l.p.sem.FuncVarType(l.curFuncName, b.Value); has && t != "" {
				return ty.StripOptionRaw(t)
			}
		}
	case *IndexExpression:
		if cont := l.indexContainerType(b.Left); cont != "" {
			return containerElemType(cont)
		}
	case *DotExpression:
		if rt := l.p.resolveReceiverType(b.Receiver); rt != "" {
			if fields, ok := l.p.structFieldsOf(rt); ok {
				if ft, ok := fields[b.Property]; ok {
					return strings.TrimPrefix(ft, "?")
				}
			}
		}
	}
	return ""
}

// chainOuterType 推断「读取一个表达式所得的值」的型别，用于链式 `?=` 的最外层
// 型别覆写。与 indexContainerType（只回答「e 作为容器的型别」）不同，这里对
// IndexExpression 返回其元素型别、对 DotExpression 返回字段型别，因此
// `.pool.nodes[idx].first` 会得到 i64，而不是链根元素 dom-node。
// 不可判定时返回空字符串。
func (l *lowerer) chainOuterType(e Expression) string {
	switch b := unwrapLowerExpr(e).(type) {
	case *Identifier:
		if l.p.sem != nil {
			if t, has := l.p.sem.FuncVarType(l.curFuncName, b.Value); has && t != "" {
				return ty.StripOptionRaw(t)
			}
		}
	case *IndexExpression:
		if cont := l.indexContainerType(b.Left); cont != "" {
			return ty.StripOptionRaw(containerElemType(cont))
		}
	case *DotExpression:
		if rt := l.chainOuterType(b.Receiver); rt != "" {
			if fields, ok := l.p.structFieldsOf(rt); ok {
				if ft, ok := fields[b.Property]; ok {
					return ty.StripOptionRaw(ft)
				}
			}
		}
	case *CallExpression:
		if t := l.p.inferTypeFromCallExpr(b); t != "" {
			return ty.StripOptionRaw(t)
		}
	}
	return ""
}

// addCaptureNode 把 e 登記為捕獲節點（分配 __cap_N、註冊型別），回傳替代 e 的識別符。
func (l *lowerer) addCaptureNode(e Expression, inner string, out *[]captureNode, tok lexer.Token) Expression {
	l.capTmpSeq++
	name := fmt.Sprintf("__cap_%d", l.capTmpSeq)
	l.p.setVarType(name, "?"+inner)
	*out = append(*out, captureNode{expr: e, inner: inner, name: name})
	return &Identifier{Token: tok, Value: name}
}

// collectCaptureNodes 按求值順序收集可錯源節點，並就地把它們替換成對應的
// __cap_N 識別符，回傳改寫後的表達式。
//
// inArithOperand 表示 e 目前位於「算術運算元」位置：只有在此位置才直接捕獲
// option 變數與回傳 option 的呼叫（作為呼叫引數的 option 交給被呼叫方，與
// guardInCallArg 的既有註釋一致）。
//
// analysisOnly=true（prepareInferredTypes 旗標）：讀 *out 但不改寫 AST——
// 仍依規則找到可錯源並把它記入 *out，但 `v.Left = ...`／`v.Arguments[i] = ...`
// 等就地改寫一律略過、回傳 nil。語義副表的 `publishInferredType` 由呼叫端
// （maybeCaptureOptionAssign 的 synthesizeNullableType 走線）負責，與結構
// pass 完全相同；只是不寫 AST 樹。修補 §3.2 之前 prepareInferredTypes 曾因
// 不小心寫入 `v.Left = __cap_1` 永久破壞 `ls.Value`，讓後續 structural pass
// 第二次 collectCaptureNodes 看不到原始的 b/c 樹——一個「sem 分析 pass」踩
// 到 AST 的 P1 紅線。
func (l *lowerer) collectCaptureNodes(e Expression, out *[]captureNode, tok lexer.Token, inArithOperand bool) Expression {
	switch v := e.(type) {
	case *GroupedExpression:
		if v.Expression != nil {
			if l.analysisOnly {
				l.collectCaptureNodes(v.Expression, out, tok, inArithOperand)
				return nil
			}
			v.Expression = l.collectCaptureNodes(v.Expression, out, tok, inArithOperand)
		}
		return v
	case *InfixExpression:
		// `/` `%` 自身即為可錯源，且只取最外層：巢狀 `(b / c) / d` 只捕獲外層，
		// 內部行為與 `?=` 現狀完全一致。
		if inner := l.captureInfixInner(v); inner != "" {
			return l.addCaptureNode(v, inner, out, tok)
		}
		if l.analysisOnly {
			l.collectCaptureNodes(v.Left, out, tok, true)
			l.collectCaptureNodes(v.Right, out, tok, true)
			return nil
		}
		v.Left = l.collectCaptureNodes(v.Left, out, tok, true)
		v.Right = l.collectCaptureNodes(v.Right, out, tok, true)
		return v
	case *IndexExpression:
		if l.analysisOnly {
			if v.Left != nil {
				l.collectCaptureNodes(v.Left, out, tok, false)
			}
			if v.Index != nil {
				l.collectCaptureNodes(v.Index, out, tok, false)
			}
			if l.p.isSafeIndexBase(v) {
				if elem := l.p.inferIndexElemType(v); elem != "" {
					l.addCaptureNode(v, elem, out, tok)
				}
			}
			return nil
		}
		if v.Left != nil {
			v.Left = l.collectCaptureNodes(v.Left, out, tok, false)
		}
		if v.Index != nil {
			v.Index = l.collectCaptureNodes(v.Index, out, tok, false)
		}
		// D7/D6：安全索引的捕獲不再以「所在函式有 ?T 結果參數」為閘門 ——
		// `x = arr[i]` 在任何函式（含頂層指令稿）裡都產生 `?elem`，與
		// `x = a + b` 的待遇一致（§3.2）。原本的閘門會讓非 ?T 函式裡的
		// `x = arr[i]` 保持 plain，只在 checker 端留下一條 idxhndld HINT，
		// 於是同一種寫法在不同函式裡語意不同。
		if l.p.isSafeIndexBase(v) {
			if elem := l.p.inferIndexElemType(v); elem != "" {
				return l.addCaptureNode(v, elem, out, tok)
			}
		}
		return v
	case *CallExpression:
		if l.analysisOnly {
			for _, arg := range v.Arguments {
				l.collectCaptureNodes(arg, out, tok, false)
			}
			if inArithOperand {
				if inner := l.callOptionInner(v); inner != "" {
					l.addCaptureNode(v, inner, out, tok)
				}
			}
			return nil
		}
		for i := range v.Arguments {
			v.Arguments[i] = l.collectCaptureNodes(v.Arguments[i], out, tok, false)
		}
		if inArithOperand {
			if inner := l.callOptionInner(v); inner != "" {
				return l.addCaptureNode(v, inner, out, tok)
			}
		}
		return v
	case *Identifier:
		if inArithOperand && v.Value != "self" {
			if inner := l.optionInnerOfVar(v.Value); inner != "" {
				return l.addCaptureNode(v, inner, out, tok)
			}
		}
		return v
	}
	return e
}

// maybeCaptureOptionAssign 偵測「普通 `=` 綁定含可錯源的運算式」並就地改寫為捕獲：
//
//	a = b + c / d
//	→ __cap_1 ?i64 = c / d
//	  a ?i64 = nil
//	  match __cap_1 { nil -> a = __cap_1
//	                  err -> a = __cap_1
//	                  ok  -> a = b + __cap_1 }
//
// 錯誤（nil/err）就地存進 a，流程繼續；之後用 `a: { ok -> ... }` 消費。
// 多個可錯源時綁定放進上一層的 ok 臂，保持由左至右的求值順序。
//
// 保守跳過的情形（維持今日行為，交由 checker 或既有路徑處理）：
//   - 顯式標註非 option 型別、或變數已確定為非 option（無法把 option 存進 plain）
//   - 根為算術且 inferOptionArith 已合成 `?T`、根為 option 變數（賦值即拷貝）
//   - 根為安全索引（僅合成 ?elem 註解，無結構 desugar）
//   - 根為呼叫但回傳型別未知、或回傳本身已是 option（整體拷貝語意）
//   - 根為其他型別（字面量、欄位存取…）語意未定
//   - 掃描後沒有任何可錯源節點（`len(nodes) == 0`）
//
// 參數 `byOptionArith` 是 walker 剛呼叫 `inferOptionArith` 的回傳值，語意為
// 「本陳述的根-算術推斷剛剛成立」。它取代了 S3 時期的 `InferredByOptionArith`
// 持久位（`TypeFact.ByOptionArith`）：那位在 S3 是為了不寫 AST 才搬進副表，
// 但它其實是**同一次 walk 內的局部判定結果**，不是可重建的程式事實 —— 存起來
// 既讓 blob 多一欄，又讓「只存 Prog + 重走重建」在 6 個語料上失效（重走時
// `signedIntOperandType` 的可見型別集合與首次 walk 不同）。改為值傳遞後，
// 判定與消費發生在同一時刻、同一份狀態上，與 walk 順序無關。
func (l *lowerer) maybeCaptureOptionAssign(stmt interface{}, byOptionArith bool) Statement {
	ls, ok := stmt.(*LetStatement)
	if !ok || ls == nil || ls.IsSynthetic || ls.Name == nil || ls.Value == nil {
		return nil
	}
	if !l.captureTargetAllowsOption(ls) {
		return nil
	}
	// place-write / match-payload 重綁定：`it = X` 屬 ok 臂語義，由
	// buildMatchDesugar 的 rewriteItPlaceWrites 統一處理元素寫回。若在此走
	// 通用捕獲 pass，會把 `it = rotr64(v[d]^v[a],32)` 改寫成 capture SurfaceMatch，
	// 使 itAssignParts 認不到、RHS 读取未被處理（capture 產物仍被 checker 視為
	// ?u64）→ fxxoptarg。直接跳過，交給 place-write lowering 處理。
	//
	// ⚠️ 編譯期降糖模式跳過：此時 D9 place-write desugar 會接管 `it`。糖形態
	// （no fmt / LSP，SkipLowering=true）不走此路徑，`it` 是普通暫存變數（std
	// 裡大量使用），必須走通用捕獲邏輯。SkipSafeIndexLowering 旗標已隨 §4.3 刪除，
	// 此處統一在編譯期（非 analysisOnly）跳過 `it` 捕獲。
	if ls.Name.Value == "it" {
		return nil
	}
	// D1：`#{overflow=...}` / `#{index-out=...}` 註解不再豁免捕獲 —— 註解只影響
	// 「失敗時怎麼取值」的行為，不再把結果型別降級成 plain int（也不再讓子樹免於
	// 檢查）。已標註的陳述同樣是「未消費的 option」，正是本次遷移的主體。
	// 顯式標註非 option 型別 → 用戶強制 plain，交 checker 報錯。
	//
	// 例外：parse 期替 `v = arr[i]` 推斷出的元素型別（IsInferred，見
	// parseLetStatement 的 `case *IndexExpression`）不是用戶意圖，捕獲 pass
	// 有權把它改寫成 `?elem`。少了這個例外，`v = arr[i]` 在 lowering 前就被
	// 標成 plain i64，根索引的捕獲分支永遠走不到（越界錯誤只能靠 checker 報）。
	if ls.Type != nil && !ty.IsOptionRaw(typeString(ls.Type)) && !typeIsInferred(ls.Type) {
		return nil
	}
	// 變數已確定為非 option → 交 checker 報錯（訊息建議 ?= / ?T / match）。
	//
	// 僅在陳述本身沒有型別節點時才據此拒絕：有型別節點代表這是（重新）宣告，
	// sem 中的型別正是本陳述剛登記的（如上面的 parse 期索引推斷），用它來否決
	// 自己是循環論證。真正「先宣告 plain、再賦值」的情形（`v i64 = 0` 之後的
	// `v = arr[i]`）其 ls.Type 為 nil，仍會被這一關擋下。
	//
	// 判定必須用 localDefinitelyNonOption（函式作用域）而非 isDefinitelyNonOptionVar
	// —— 後者經 FuncVarType 回退到全域 VarTypes，會被其他函式的同名參數污染。
	if !l.captureTargetAllowsOption(ls) {
		return nil
	}
	root := unwrapLowerExpr(ls.Value)
	// 布林結果的 infix（比較 == != < <= > >=、邏輯 && ||）不是「可錯算術」：
	// 比較在 emitCmp 直接對 ?T 取 payload 比較並得到 bool，邏輯運算同理，兩者
	// 都不傳遞 nil/err。若讓它們走本 pass 的 option 捕獲，下方 switch 會把
	// inner 硬編碼成 "i64"，使 `b = lb == 10` 的 b 合成為 ?i64 並生成
	// ok/err/nil 捕獲鏈；後續 `!b` 便把 %option 交給 `xor i1`，LLVM 驗證
	// 直接失敗（'%lv.N' defined with type '%option' but expected 'i1'）。
	// 安全索引越界的 nil 由 emitCmp 的 payload 比較自然給出 false（見
	// mir/index_compare_test.go 的語言契約），不需要、也不應該往外傳播 option。
	if inf, isInf := root.(*InfixExpression); isInf && isBoolResultInfixOperator(inf.Operator) {
		return nil
	}
	// 根為算術（`+ - * / %`）且已帶型別節點（inferOptionArith 剛合成的推斷型別，
	// 或用戶顯式寫的 ?T）→ 本 pass 不介入。
	//
	// 合成的 `?T` 即整體捕獲語意：巢狀的可錯子運算式（`a + b / c`、`a + b * c`）
	// 由 codegen 逐層包裹，不需結構 desugar（實測 m1／n1–n3 皆正確）。用戶顯式
	// `?T` 同理，且必須原樣保留（否則顯式標註會被改寫成捕獲，破壞冪等）。
	//
	// 只有「型別節點仍為空」才讓路：此時 inferOptionArith 推斷失敗，繼續走下方
	// 捕獲路徑，讓巢狀的 option 運算元（`a + b / c` 中 b 為 ?T）／安全索引仍被就地
	// 捕獲，不比今日更漏。
	//
	// S3（2026-10-16.4）：inferOptionArith 不再就地寫 `s.Type`；推斷結果改放
	// `sem.InferredType` side-table。`a = b + c` 算術根且雙運算元為有號整數時由
	// inferOptionArith 推斷出 `?i64`，walker 把該事實以 `byOptionArith` 傳入，
	// 本短路生效，跳過結構 desugar；`a = x + 1`（x 為 option）則 inferOptionArith
	// 推不出，`byOptionArith` 為 false，捕獲照走，建構 `__cap_N` 暫存。
	//
	// 兩條短路合併成一個條件是安全的：`ls.Type != nil` 時 inferOptionArith 必定
	// 提前返回 false（顯式標註不碰），故二者互斥且並集即「根-算術已被別處收手」。
	if inf, isInf := root.(*InfixExpression); isInf && isOptionArithOperator(inf.Operator) && (ls.Type != nil || byOptionArith) {
		return nil
	}
	// 根為 option 變數：賦值本身即整體拷貝，現有行為已正確。
	if id, isID := root.(*Identifier); isID && l.optionInnerOfVar(id.Value) != "" {
		return nil
	}
	// 根為安全索引（無內部可錯子表達式）：只合成 ?elem，越界 → nil 即捕獲。
	//
	// D7/D6：不再要求「所在函式有 ?T 結果參數」（原 `enclosingFuncHasOptResult`
	// 閘門已刪）—— 索引捕獲全函式生效，`x = arr[i]` 一律得到 `?elem`。
	if idx, isIdx := root.(*IndexExpression); isIdx && l.p.isSafeIndexBase(idx) {
		if elem := l.p.inferIndexElemType(idx); elem != "" {
			l.synthesizeNullableType(ls, elem)
		}
		return nil
	}
	// 根型別決定 option 內部型別 T（方案 §1「T 推斷」）：
	//   - 算術 Infix（`+ - * <<` …）：T = i64 —— option 內部資料一律 i64，
	//     見 lowerUnwrapAssign 對 ?T 內部型別的說明。
	//   - 呼叫：T = 回傳型別去 `?`。`a = g(b / c)` 的捕獲點在引數內，結果型別
	//     仍是 g 的回傳型別；回傳本身已是 option（`?T`）時整個呼叫即為 option，
	//     屬既有「賦值即整體拷貝」語意，不由此 pass 處理（保守跳過）。
	//   - 其餘（字面量、欄位存取…）語意未定，保守不改寫。
	var inner string
	switch r := root.(type) {
	case *InfixExpression:
		inner = "i64"
	case *CallExpression:
		rt := l.p.returnTypeFromCallExpr(r)
		if rt == "" || ty.IsOptionRaw(rt) {
			return nil
		}
		inner = rt
	default:
		return nil
	}
	var nodes []captureNode
	// S3-b 雙軌：開關打開時 collectCaptureNodes **必須**走「不改寫 AST」的分支
	//（即 analysisOnly 那一路，見該函式的 `if l.analysisOnly` 各段）。
	//
	// 不這麼做的實測後果（本函式會被 walker 多次走到）：第一次就地把 `o` 換成
	// `__cap_1`，而 addCaptureNode 已用 setVarType 給它登記了 `?i64` ⇒ 後續
	// pass 把合成標識當成**新的可錯源**再捕獲一次，Binds 裡存的是 `__cap_1`
	// 本身而非原始表達式，副表直接失去意義。
	//
	// rewritten 在 HIR 軌不使用（回傳 nil）：替換成 `__cap_N` 的動作改由
	// tohir 依副表即席完成，不產生也不改寫任何 AST。
	savedAnalysisOnly := l.analysisOnly
	if HIRCaptureEnabled {
		l.analysisOnly = true
	}
	rewritten := l.collectCaptureNodes(ls.Value, &nodes, ls.Token, false)
	l.analysisOnly = savedAnalysisOnly
	if len(nodes) == 0 {
		return nil
	}
	// 算術根的 inner 推導補正：上方 `case *InfixExpression: inner = "i64"`
	// 只對「整數算術」成立——codegen 的 option payload 慣例是整數算術（任意
	// 寬度，含 byte/u32/char）的結果一律 zext 至 i64（窄型別已 zext／符號擴展，
	// 見 lowerUnwrapAssign 對 ?T 內部型別與 ?= 暫存的說明），故硬編碼 i64 對
	// 它們正確。需要覆寫的只有「結果型別 ≠ i64」的兩種：
	//   - 浮點算術：結果是運算元的浮點型別（`a * b`，a/b 皆 ?f64 ⇒ ?f64；
	//     實測 tests/arr-test-tmp.no，?i64 會讓 checker 誤報格式串 'g'）。
	//   - str 拼接（`+`）：結果是 str。
	// 其餘（整數、型別不一致、未知）保留 i64。⚠️ 實測教訓：把 byte 移位組合
	// （aes.no 的 `(ek[x] << 24) | …`）覆寫成 ?byte 會讓 codegen 的 option
	// wrap 發出 `trunc i64 %c614 to i8` 而 LLVM 驗證失敗（'%c614' defined
	// with type 'i8' but expected 'i64'，tests/aes-vectors.no）。
	//
	// 2026-11-03（混型寬度規則）：option-arith 根的整數結果型別改為**左運算元
	// 的型別**（語言規則：`byte + i64` 的結果是 byte、值必須落在其範圍內，
	// 溢出 ⇒ err；回繞需顯式 `#{overflow=wrap}`）。這同時對齊了 MIR 側：
	// KInfix 的 Dst 型別本來就取左運算元（hir2mir typeOfNode），此前 parser
	// 給 ?i64、MIR 給 byte，兩邊不一致只能靠 option-wrap 的 type-pun 硬湊，
	// 疊上 emitConst 缺 i8（折疊 byte 常量恆 0）就是混型加法順序敏感 bug 的
	// 全鏈。移位/位運根（`<<`、`|` 等）不在 isOptionArithOperator 內，維持
	// i64 慣例——aes.no 的教訓場景不受影響。
	if inf, isInf := root.(*InfixExpression); isInf {
		if common := commonCaptureInner(nodes); common == "f32" || common == "f64" ||
			(common == "str" && inf.Operator == "+") {
			inner = common
		} else if isOptionArithOperator(inf.Operator) {
			ltName := l.intOperandTypeName(inf.Left)
			if ltName != "" {
				inner = ltName
			}
		}
	}
	// 顯式標註 ?T 時補內部守衛（推斷內部型別以顯式為準）。
	if ls.Type != nil {
		if explicit := ty.StripOptionRaw(typeString(ls.Type)); explicit != "" {
			inner = explicit
		}
	}
	// S3-b 第一步：把捕獲判定結果**物化**進副表（當下只寫不讀，無消費方）。
	//
	// 走到這裡代表「本陳述確定要捕獲」已判定完畢：inner 是最終內層型別，
	// nodes 是由內至外的捕獲綁定。日後 tohir 可直接讀本表產出 `KLet` + `KIf`
	// 鏈，捕獲就不再需要在 AST 層注入 `__cap_N` 合成節點（V4 違規，S3 backlog）。
	//
	// ⚠️ 本段**不改任何行為**：下方的結構改寫（預宣告 / 巢狀 match / 取代節點）
	// 照舊執行，副表純屬旁路記錄。
	if l.p != nil && l.p.sem != nil {
		binds := make([]CaptureBind, 0, len(nodes))
		for _, cn := range nodes {
			binds = append(binds, CaptureBind{Expr: cn.expr, Inner: cn.inner, Name: cn.name})
		}
		// WasDeclared 必須在預宣告判斷之前取值，且隨表帶出：它活在 lowerer 的
		// 走查期作用域（declaredLocals）裡，tohir 拿不到。取值時機見下方同名
		// 註解——declaredLocals 在本陳述的捕獲跑完之後才登記本陳述的名字。
		wasDeclared := l.declaredLocals[ls.Name.Value]
		l.p.sem.SetCaptureFact(ls, &CaptureFact{Inner: inner, Binds: binds, WasDeclared: wasDeclared})
	}
	// S3-b 雙軌：開關打開時**只記錄、不改寫 AST** —— 巢狀 match / 預宣告 /
	// 取代節點的結構改寫全部略過，交由 H0（tohir）讀副表產出等價的
	// `KLet` + `KIf` 鏈。型別推斷（synthesizeNullableType → sem.InferredType）
	// 仍須執行：它是 H0 產出 `?T` 型別的唯一來源。
	//
	// ⚠️ 此刻 H0 側產出尚未實作 ⇒ 開關為 true 時捕獲語義**缺失**，僅供副表
	// 記錄面與 AST 凍結的驗證，不得用於端到端編譯（見 HIRCaptureEnabled）。
	if HIRCaptureEnabled {
		l.synthesizeNullableType(ls, inner)
		return nil
	}
	// 目標是否在原始碼中已宣告（供下面的預宣告判斷）。
	//
	// 判定只能用 declaredLocals（walk 順序的宣告名單，參數在函式入口入帳）：
	// 它在本陳述的捕獲跑完之後才登記本陳述的名字（見 walker 的
	// `l.declaredLocals[ls.Name.Value] = true`），所以這裡看到的名字必然是
	// 「先前陳述／參數」宣告的，正是預宣告要區分的兩種情形。
	//
	// 曾經還有一條 `localDeclaredType` 回退，已於 2026-10-22 刪除：它把
	// **本陳述自己**在解析期登記的型別也算成「已宣告」，而捕獲會把本陳述連同
	// 它的型別節點一起替換掉，預宣告才是唯一能把 `?T` 型別建立起來的地方——
	// 漏掉它，MIR 只能從第一條臂（nil 臂的 `a = __cap_N` option 傳播）推導
	// 目標型別，於是 `xs = shs-sub(ln, i + 1, rb)`（callee 回傳 str，
	// filterInferableType 保留 str）捕獲後 xs 被錯定型別，`xs.len-bytes()`
	// 在 emitBuiltinLen 報 "unsupported receiver type i64"。解析期會登記的
	// 兩種來源（呼叫回傳推斷的 str/[]T/struct、本陳述顯式 `?T` 標註）都落在
	// 這條回退裡，故一併刪除。
	//
	// 先前的 `?=` 目標／match 臂綁定不走 declaredLocals，但它們登记的型別是
	// plain（?= 记的是解箱後的內層型別），會被上面 captureTargetAllowsOption
	// 的 localDefinitelyNonOption 先擋下，走不到這裡；本陳述是它們的首次
	// let 時，預宣告只是把型別重新建立一次，安全。
	wasDeclared := l.declaredLocals[ls.Name.Value]
	l.synthesizeNullableType(ls, inner)

	// analysisOnly 模式（prepareInferredTypes）：分析到此結束，後續建巢
	// match / 預宣告 / 取代節點的結構改寫對側表已是多餘。直接返回 nil 即可。
	if l.analysisOnly {
		return nil
	}

	block := l.buildCaptureChain(ls, inner, nodes, wasDeclared, rewritten)
	// S3-c：AST 軌把被捕獲的 LetStatement 就地替換成 buildCaptureChain 產出的
	// BlockStatement，原節點隨即從 AST 消失；其 `#{...}` 註解（住在原節點上）
	// 若不相隨搬運，就會在進 HIR 前丟失。H0 軌看凍結形態、不經此替換，
	// 故保留註解（見 parser/dump/hir_equivalence_test.go 的 hirCaptureKnownDivergence
	// 與 tests/arr-test-tmp.no 的 `#{index-out=0.0}`）。這是 AST 軌的既有缺陷；此處把
	// 原節點的註解 remap 到取代它的 BlockStatement，使兩軌註解都進 HIR（落在同一個
	// KBlock 上）、S3-c 切換零行為變化。
	//
	// 只在 AST 軌執行：本路徑（maybeCaptureOptionAssign 對 buildCaptureChain 的呼叫）
	// 僅在 !HIRCaptureEnabled 時抵達（H0 軌在上方 1304 行早退，改由 tohir 的
	// lowerCaptureToHIR 建鏈並直接 annotate 原語句）。若在 buildCaptureChain 內無條件
	// remap，H0 軌會经 c.stmt(block) 的 annotate(block) 与外层 annotate(ls) 重複掛兩次。
	if bs, ok := block.(*BlockStatement); ok && l.p != nil && l.p.sem != nil {
		if anns := l.p.sem.RawAnnotationsOf(ls); len(anns) > 0 {
			l.p.sem.SetRawAnnotations(bs, anns)
		}
	}
	return block
}

// buildCaptureChain 由「已判定要捕獲」的素材建出巢狀 match 結構（預宣告 +
// 綁定 + 三臂），回傳取代原陳述的 BlockStatement。
//
// S3-b：本函式是**兩條軌道的唯一實作**——
//   - AST 軌（默認）：由 maybeCaptureOptionAssign 就地改寫 AST 前呼叫；
//   - H0 軌（HIRCaptureEnabled）：由 tohir 的 lowerCaptureToHIR 以副表
//     （CaptureFact）重建的素材呼叫，產物**不寫回**用戶的 AST。
//
// 共用同一個建構函式是「HIR 逐字節一致」的根本保證：兩軌餵進來的是同一套
// 素材、走的是同一段 code、也同樣經 c.stmt / case *IfExpression 折疊，
// 差別只在**合成發生在哪裡**（parser 走查 vs H0）。
//
// 參數 rewritten 是最終賦值（子表達式已換成 `__cap_N`）；AST 軌由
// collectCaptureNodes 就地改寫取得，H0 軌由 substCaptureExprs 非破壞性取得。
func (l *lowerer) buildCaptureChain(ls *LetStatement, inner string, nodes []captureNode, wasDeclared bool, rewritten Expression) Statement {
	tok := ls.Token
	// 由最內層節點往外建巢狀 match；最內層 body 即最終賦值。
	body := Statement(&LetStatement{
		Token:       tok,
		Name:        ls.Name,
		Value:       rewritten,
		IsSynthetic: true,
		// OverflowMode 繼承：原陳述的 `#{overflow=wrap}` 行注解經 setStmtOverflowMode
		// 寫在 ls.OverflowMode 欄位上，但捕獲鏈把原陳述替換成嵌套 match 後，
		// 真正的整數運算落在最內層 ok 臂的這條合成賦值裡。tohir 只從
		// LetStatement.OverflowMode 欄位編碼 HIR overflow flags（KBlock 不編碼、
		// S3-c 的側表 remap 進不了 Flags），漏掉它，內層 add 在 codegen 退回
		// checked `llvm.*.with.overflow`——即使 u64 鏈上加法真溢出也產生 err
		// option 並沿資料流污染後續計算（sha512 調度 w29 首個真溢出即觸發，
		// 2026-10-30 crypto 回歸根因）。
		OverflowMode: ls.OverflowMode,
	})
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		capIdent := &Identifier{Token: tok, Value: n.name}
		// nil/err 臂：把整個 option 結構拷貝進目標（IsPropagation 鏡像 ?= 的
		// 傳播賦值，避免 codegen 對 arm 內哨兵 it 綁定的處理把這次 store 吞掉）。
		copyArm := func() *BlockStatement {
			return &BlockStatement{Token: tok, Statements: []Statement{
				&LetStatement{Token: tok, Name: ls.Name, Value: capIdent, IsSynthetic: true, IsPropagation: true},
			}}
		}
		arms := []MatchArm{
			{Condition: &Identifier{Token: tok, Value: "nil"}, Body: copyArm(), IsBlockBody: true, Pos: posFromToken(tok), SkipItBinding: true},
			{Condition: &Identifier{Token: tok, Value: "err"}, Body: copyArm(), IsBlockBody: true, Pos: posFromToken(tok), SkipItBinding: true},
			{Condition: &Identifier{Token: tok, Value: "ok"}, Body: &BlockStatement{Token: tok, Statements: []Statement{body}}, IsBlockBody: true, Pos: posFromToken(tok), SkipItBinding: true},
		}
		sm := &SurfaceMatch{Token: tok, Matched: capIdent, Arms: arms}
		savedFuncName := l.p.curFuncName
		l.p.curFuncName = l.curFuncName
		lowered := l.p.desugarSurfaceMatchToIfExpr(sm)
		l.p.curFuncName = savedFuncName
		if lowered == nil {
			return nil
		}
		bind := &LetStatement{
			Token:       tok,
			Name:        &Identifier{Token: tok, Value: n.name},
			IsSynthetic: true,
			// IsInferred：這個 `?inner` 是 lowering 合成的，原始碼裡沒有。
			// 少了它，checker 的冗餘型別標註 lint（tcpoxtfd）會把它當成用戶寫的
			// 顯式標註並建議刪除——指向一個用戶根本看不到的臨時變數
			//（`__cap_1 ?i64 = …`）。
			Type: &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: n.inner}, IsInferred: true},
			// OverflowMode 同上繼承：bind 的值運算式（n.expr）也可能含整數運算。
			OverflowMode: ls.OverflowMode,
			Value:        n.expr,
		}
		body = &BlockStatement{Token: tok, Statements: []Statement{
			bind,
			&ExpressionStatement{Token: tok, Expression: lowered},
		}}
	}
	var stmts []Statement
	// 目標尚未宣告時預先 `a ?T = nil` 宣告：所有臂都會寫入 a，nil 槽無堆指標，
	// 後續覆寫安全。已宣告為 ?T 時只需臂內重賦值。
	if !wasDeclared {
		stmts = append(stmts, &LetStatement{
			Token:       tok,
			Name:        ls.Name,
			IsSynthetic: true,
			// IsInferred：同上方 bind 的說明——合成的預宣告 `a ?T = nil` 不是
			// 用戶寫的標註，不得被 tcpoxtfd 建議刪除。
			Type:  &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: inner}, IsInferred: true},
			Value: &NilLiteral{Token: tok},
		})
	}
	stmts = append(stmts, body)
	return &BlockStatement{Token: tok, Statements: stmts, CommentedNode: ls.CommentedNode}
}

// signedIntOperandType 回傳運算元的有號整數型別名（int/i64/...）。僅在型別
//
//	statically 可知且有號時回傳 ok：整數字面量 → i64；識別符 → 語義表查詢
//
// （self 用方法 receiver 型別）；括號展開。型別未知或非整數回 ok=false，
// 保守跳過推斷（i128 除外：codegen 對其退化為 wrap，不產生 option）。
func (l *lowerer) signedIntOperandType(e Expression) (string, bool) {
	switch x := unwrapLowerExpr(e).(type) {
	case *IntegerLiteral:
		return "i64", true
	case *Identifier:
		name := x.Value
		if name == "self" || name == "." {
			return l.lowerSelfType()
		}
		typ := ""
		if l.p.sem != nil {
			if t, has := l.p.sem.FuncVarType(l.curFuncName, name); has {
				typ = t
			}
		}
		return lowerSignedIntName(typ)
	case *InfixExpression:
		// 巢狀算術運算元（`a + b * c` 的右運算元 `b * c`）：其值為 option 的
		// 內部 payload，型別即兩側較寬的有號整數型別。這是 D6 統一規則的必要
		// 條件 —— 否則根運算元判定不出型別，整個綁定就推不出 `?T`。
		if !isOptionArithOperator(x.Operator) {
			return "", false
		}
		lt, okLT := l.signedIntOperandType(x.Left)
		rt, okRT := l.signedIntOperandType(x.Right)
		if !okLT || !okRT {
			return "", false
		}
		if len(rt) > len(lt) {
			return rt, true
		}
		return lt, true
	default:
		return "", false
	}
}

// isOptionArithOperator 回報運算子是否屬「統一 option 規則」（D6）涵蓋的整數
// 算術運算：`+ - * / %`。字串拼接的 `+`、浮點／char／無號運算皆由
// signedIntOperandType 的型別判定排除，不在此另作區分。
func isOptionArithOperator(op string) bool {
	switch op {
	case "+", "-", "*", "/", "%":
		return true
	}
	return false
}

// isBoolResultInfixOperator reports infix operators whose result is a plain
// bool (comparisons and logical connectives). Unlike the D6 arithmetic family,
// these CONSUME their option operands: emitCmp compares the ok-payload, and a
// nil/err operand simply makes the comparison false. They therefore yield a
// bool, not a ?T, and must not be rewritten into an option capture chain by
// maybeCaptureOptionAssign. See the comment at that call site.
func isBoolResultInfixOperator(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
		return true
	}
	return false
}

// lowerSelfType 回傳當前方法 receiver（self）的有號整數型別；非方法或
// 非整數接收者回 ok=false。
func (l *lowerer) lowerSelfType() (string, bool) {
	fd := l.curFuncDef
	if fd == nil || !fd.IsMethodDef || len(fd.Results) == 0 {
		return "", false
	}
	if r := fd.Results[0]; r != nil && r.Name == "self" && r.Type != nil {
		return lowerSignedIntName(typeString(r.Type))
	}
	return "", false
}

// unwrapLowerExpr 剝除括號節點，回傳內層表達式。
func unwrapLowerExpr(e Expression) Expression {
	for {
		g, ok := e.(*GroupedExpression)
		if !ok || g.Expression == nil {
			return e
		}
		e = g.Expression
	}
}

// lowerSignedIntName 判定型別字串是否為可產生 option 的有號整數型別：
// 接受 int/i8/i16/i32/i64（含 number.int 等限定名與 *int 指標形式），
// 拒絕無號（u8..u128/byte）、i128（codegen 退化 wrap）、f32/f64、str 等
// 已宣告型別、以及已為 option（?T）的型別。
func lowerSignedIntName(typ string) (string, bool) {
	typ = strings.TrimSpace(typ)
	if typ == "" || ty.IsOptionRaw(typ) {
		return "", false
	}
	typ = ty.StripPtrAllRaw(typ)
	if i := strings.LastIndex(typ, "."); i >= 0 {
		typ = typ[i+1:]
	}
	switch typ {
	case "int", "i8", "i16", "i32", "i64":
		return typ, true
	}
	return "", false
}

// carryOverflowAnnotation 把被安全索引 desugar 取代的原陳述之 `#{overflow = ...}`
// 條目轉掛到取代節點上。
//
// 安全索引 desugar 會以全新的 match/Block 取代原陳述（`arr[idx] = v` →
// `__tmp = arr[idx]` + match），而 `#{overflow = wrap}` 這類**行註解**是綁在原陳述
// 節點上的。若不轉移，取代節點就變成「未標註」：
//   - lint（checker.ValidateIntOverflow，跑在 lowering 之後的程式上）會把
//     索引算式 `arr[base + i]` 裡真正的整數運算當成未處理而繼續報告
//     （ovf-int-default），即使原始碼上方明明寫了 `#{overflow=wrap}`；
//   - 型別校驗（checker.overflowModeFromSem）也讀不到模式。
//
// 這正是「加了註解卻仍被誤報」的根因，故在此把 overflow 條目一併帶到取代節點
// （只帶 overflow，不帶 index-out：index-out 的語意已由 desugar 本身實現）。
//
// 註解來源有兩個：側表條目（Ident 開頭的陳述由 parser 直接附加），以及
// OverflowMode 欄位（`.field[i] = v` / `return ...` 這類無法直接附加的陳述，
// 行注解只寫欄位而不進側表，以免 formatter 把同一行註解印兩次）。
func (l *lowerer) carryOverflowAnnotation(from Node, to Statement) {
	if l.p.sem == nil || to == nil || isNil(from) {
		return
	}
	entries := l.p.overflowEntries(l.p.sem.RawAnnotationsOf(from))
	if len(entries) == 0 {
		entries = l.p.overflowEntries(l.p.sem.AnnotationsOf(from))
	}
	if len(entries) == 0 {
		if mode := overflowModeFieldOf(from); mode != "" {
			entries = []*AnnotationEntry{{
				Key:   "overflow",
				Value: &AnnotationIdentValue{Value: mode},
			}}
		}
	}
	if len(entries) == 0 {
		return
	}
	l.p.sem.SetRawAnnotations(to, entries)
}

// stmtHasOverflowAnnotation 回報陳述是否帶有 `#{overflow=...}` 行註解（側表條目
// 或 OverflowMode 欄位）。捕獲 pass 用它判斷「本陳述／外層陳述已由用戶顯式處理
// 溢出語義」而整棵子樹跳過改寫。
func (l *lowerer) stmtHasOverflowAnnotation(n Node) bool {
	if isNil(n) || l.p.sem == nil {
		return false
	}
	if len(l.p.overflowEntries(l.p.sem.RawAnnotationsOf(n))) > 0 {
		return true
	}
	if len(l.p.overflowEntries(l.p.sem.AnnotationsOf(n))) > 0 {
		return true
	}
	return overflowModeFieldOf(n) != ""
}

// overflowModeFieldOf 讀取陳述節點的 OverflowMode 欄位（行注解在無法直接附加
// 註解的陳述上只寫此欄位）。欄位集合與 setStmtOverflowMode 保持一致。
func overflowModeFieldOf(n Node) string {
	switch s := n.(type) {
	case *ForStatement:
		return s.OverflowMode
	case *ExpressionStatement:
		return s.OverflowMode
	case *LetStatement:
		return s.OverflowMode
	case *ReturnStatement:
		return s.OverflowMode
	case *MultiAssignStatement:
		return s.OverflowMode
	}
	return ""
}

// isContainerOrStructElem 報告元素型別 elem 是否為非标量（容器或結構體）。
// 容器以 [ 開頭（[]T / [N]T / [K]V）；其餘非标量識別字型別（如結構體）視為結構體。
func isContainerOrStructElem(elem string) bool {
	if elem == "" {
		return false
	}
	if strings.HasPrefix(elem, "[") {
		return true
	}
	switch elem {
	case "f64", "f32", "bool", "str", "txt", "byte", "char",
		"i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128":
		return false
	}
	return true
}

// compositeElemKind 描述元素型別屬於哪一種複合型別。
const (
	compositeSlice  = iota // []T
	compositeArray         // [N]T
	compositeMap           // [K]V
	compositeStruct        // 具名結構體
)

// splitBracketType 拆分以 '[' 開頭的複合型別字串，取第一對方括號內的內容與其後
// 剩餘部分。例：[]i64 → ("", "i64")；[3]i64 → ("3", "i64")；[str]i64 → ("str","i64")。
func splitBracketType(elem string) (inner string, rest string, ok bool) {
	if !strings.HasPrefix(elem, "[") {
		return "", "", false
	}
	depth := 0
	for i := 0; i < len(elem); i++ {
		switch elem[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return elem[1:i], elem[i+1:], true
			}
		}
	}
	return "", "", false
}

// classifyCompositeElem 分類複合元素型別。
// elemType 是容器內的元素型別（映射則為值型別）；size 只在定長陣列時有意義；
// keyType 只在映射時有意義。
func classifyCompositeElem(elem string) (kind int, elemType string, size int64, keyType string) {
	if inner, rest, ok := splitBracketType(elem); ok {
		if inner == "" {
			return compositeSlice, rest, 0, ""
		}
		if n, err := strconv.ParseInt(inner, 10, 64); err == nil {
			return compositeArray, rest, n, ""
		}
		return compositeMap, rest, 0, inner
	}
	return compositeStruct, "", 0, ""
}

// zeroValueLiteralFor 依元素型別 elem 產生越界預設值字面量：
// 切片 → 空 ArrayLiteral（WasSliceLiteral）；定長陣列 → N 個零元素；
// 映射 → 空 MapLiteral；結構體 → 零 StructLiteral。
func zeroValueLiteralFor(tok lexer.Token, elem string) Expression {
	kind, elemType, size, _ := classifyCompositeElem(elem)
	switch kind {
	case compositeMap:
		return &MapLiteral{Token: tok, Pairs: nil}
	case compositeArray:
		elems := []Expression{}
		for i := int64(0); i < size; i++ {
			elems = append(elems, scalarZeroLiteral(tok, elemType))
		}
		return &ArrayLiteral{
			Token:    tok,
			Size:     &IntegerLiteral{Token: tok, Value: size, Raw: strconv.FormatInt(size, 10)},
			Elements: elems,
		}
	case compositeSlice:
		return &ArrayLiteral{Token: tok, Elements: []Expression{}, WasSliceLiteral: true}
	default:
		return &StructLiteral{Token: tok, Type: elem, Fields: nil}
	}
}

// scalarZeroLiteral 產生型別 typeName 的零值字面量；複合型別遞迴展開。
func scalarZeroLiteral(tok lexer.Token, typeName string) Expression {
	switch typeName {
	case "f64", "f32":
		return &FloatLiteral{Token: tok, Value: 0, Raw: "0"}
	case "bool":
		return &BooleanLiteral{Token: tok, Value: false}
	case "str", "txt":
		return &StringLiteral{Token: tok, Value: "", Raw: "''"}
	default:
		// 未列出的識別字型別可能是結構體 / 容器，遞迴取複合零值，
		// 其餘（整數族）一律 i64 0。typeName 在遞迴中嚴格變短，必然終止。
		if isContainerOrStructElem(typeName) {
			return zeroValueLiteralFor(tok, typeName)
		}
		return &IntegerLiteral{Token: tok, Value: 0, Raw: "0"}
	}
}

// preRegisterEnumArmBindings 在展開 match 之前，先依「被匹配變數的靜態型別」把
// 帶載荷變體析構臂（`some(v) ->` / `rect(w, h) ->`）的綁定名與其欄位型別登記進
// 函式作用域符號表。lowerSurfaceMatch 是先自底向上 walk arm 體（其中可能含有以該
// 綁定變數為主體的嵌套 match），之後才 buildMatchDesugar；若不預先登記，內層 match
// 在解析期查不到綁定變數的型別，就會把它自己的變體臂 `a(v)` 當成普通呼叫，導致
// `v` 未定義。此處登記後，內層 match 便能正確解析自身變體並繼續綁定載荷。
func (l *lowerer) preRegisterEnumArmBindings(sm *SurfaceMatch) {
	if sm == nil || len(sm.Arms) == 0 {
		return
	}
	ident, ok := sm.Matched.(*Identifier)
	if !ok {
		return
	}
	p := l.p
	mt, ok := p.sem.FuncVarType(l.curFuncName, ident.Value)
	if !ok || mt == "" {
		return
	}
	if _, isEnum := p.sem.EnumVariantsOf(mt); !isEnum {
		return
	}
	saved := p.curFuncName
	p.curFuncName = l.curFuncName
	defer func() { p.curFuncName = saved }()
	for i := range sm.Arms {
		a := &sm.Arms[i]
		call, ok := a.Condition.(*CallExpression)
		if !ok || len(call.Arguments) == 0 {
			continue
		}
		fn, ok := call.Function.(*Identifier)
		if !ok {
			continue
		}
		names := make([]string, 0, len(call.Arguments))
		allBare := true
		for _, arg := range call.Arguments {
			id, isID := arg.(*Identifier)
			if !isID {
				allBare = false
				break
			}
			names = append(names, id.Value)
		}
		if !allBare {
			continue
		}
		fts := p.enumVariantFieldTypes(mt, fn.Value)
		if len(fts) == 0 || len(fts) != len(names) {
			continue
		}
		if p.sem.DeclaredVars == nil {
			p.sem.DeclaredVars = make(map[string]bool)
		}
		for k, nm := range names {
			p.sem.DeclaredVars[nm] = true
			if fts[k] != "" {
				p.sem.SetFuncVarType(l.curFuncName, nm, fts[k])
				p.setVarType(nm, fts[k])
			}
		}
	}
}

// matchArmCovers reports which of the option's three cases (nil / err / ok) an
// arm claims. A combined arm (`nil || err ->`) claims both.
func matchArmCovers(a *MatchArm) (nilOK, errOK, okOK bool) {
	for _, pat := range a.MultiOptionPatterns {
		switch pat {
		case "nil":
			nilOK = true
		case "err":
			errOK = true
		case "ok":
			okOK = true
		}
	}
	switch c := a.Condition.(type) {
	case *NilLiteral:
		nilOK = true
	case *Identifier:
		switch c.Value {
		case "nil":
			nilOK = true
		case "err":
			errOK = true
		case "ok":
			okOK = true
		}
	}
	// `ok ->` is the dotVal arm; `ok(cond) ->` carries a raw boolean condition.
	if a.IsDotVal || a.IsRawCond {
		okOK = true
	}
	return nilOK, errOK, okOK
}

// rejectItInNonOkArm rejects `it` inside a catch-all `->` arm that is not
// provably the ok arm.
//
// `it` is meaningful only where the matched option is known to hold a value:
// an explicit `ok ->` arm, or a catch-all `->` that runs only after BOTH `nil`
// and `err` have been claimed by earlier arms. A bare `->` with no such arms
// still receives nil/err, so `it` there is uninitialized payload — the
// compiler must say so instead of silently emitting a deref.
func (l *lowerer) rejectItInNonOkArm(sm *SurfaceMatch) {
	if sm == nil {
		return
	}
	// 只對 option 型別的 match 生效。enum / 純值 match 的 catch-all 臂裡 `it`
	// 就是被匹配的值本身（不存在 nil / err 的可能），不能報錯。
	if ident, ok := sm.Matched.(*Identifier); ok {
		if t, ok := l.p.sem.FuncVarType(l.curFuncName, ident.Value); ok {
			if _, isEnum := l.p.sem.EnumVariantsOf(t); isEnum {
				return
			}
			if !ty.IsOptionRaw(t) && !strings.Contains(t, "|") {
				return
			}
		}
	} else {
		// 非識別符主體：無從判定是否為 option，不報。裸 match（`{ cond -> ... }`）
		// 沒有主體，`it` 指的是外層 match 綁的那個值，也不該由這裡判定。
		return
	}
	var nilCovered, errCovered, okCovered bool
	for i := range sm.Arms {
		n, e, o := matchArmCovers(&sm.Arms[i])
		nilCovered = nilCovered || n
		errCovered = errCovered || e
		okCovered = okCovered || o
	}
	// The catch-all arm gets every case the explicit arms did NOT claim. `it`
	// has a single well-defined meaning only when exactly one case is left:
	//   nil+err claimed -> `->` is ok  (it = the value)
	//   ok+nil  claimed -> `->` is err (it = the error message)
	//   ok+err  claimed -> `->` is nil (it = nil)
	// Anything else means `it` may be any of several unrelated things.
	remaining := 0
	if !nilCovered {
		remaining++
	}
	if !errCovered {
		remaining++
	}
	if !okCovered {
		remaining++
	}
	if remaining <= 1 {
		return
	}
	for i := range sm.Arms {
		a := &sm.Arms[i]
		// Only the catch-all `->` arm is ambiguous. `ok ->` (isDotVal) and
		// explicit pattern arms are fine.
		if !a.IsWildcard || a.IsDotVal || a.SkipItBinding {
			continue
		}
		if !armUsesIt(a.Body) {
			continue
		}
		line, col := a.Pos.Line, a.Pos.Column
		if line == 0 && a.Body != nil && len(a.Body.Statements) > 0 {
			line = a.Body.Statements[0].Pos().Line
			col = a.Body.Statements[0].Pos().Column
		}
		var missing []string
		if !nilCovered {
			missing = append(missing, "nil")
		}
		if !errCovered {
			missing = append(missing, "err")
		}
		if !okCovered {
			missing = append(missing, "ok")
		}
		l.p.saveErrorf("rgzkvjhx", "line %d, column %d: `it` is not valid in this arm: the catch-all `->` arm may receive %s; claim them with explicit arms (e.g. `nil ->` / `err ->`) or use `ok ->` before reading `it`",
			line, col, strings.Join(missing, " / "))
	}
}

// armUsesIt reports whether an arm body references the implicit `it` binding.
// Nested matches are NOT scanned except for their subject: their arms bind
// their own `it`, which belongs to the inner match, not this one.
func armUsesIt(body *BlockStatement) bool {
	if body == nil {
		return false
	}
	seen := make(map[uintptr]bool)
	var scan func(v reflect.Value) bool
	scan = func(v reflect.Value) bool {
		switch v.Kind() {
		case reflect.Ptr:
			if v.IsNil() {
				return false
			}
			p := v.Pointer()
			if seen[p] {
				return false
			}
			seen[p] = true
			switch n := v.Interface().(type) {
			case *Identifier:
				return n.Value == "it"
			case *SurfaceMatch:
				return n.Matched != nil && scan(reflect.ValueOf(n.Matched))
			}
			return scan(v.Elem())
		case reflect.Interface:
			if v.IsNil() {
				return false
			}
			if v.Elem().Type() == surfaceMatchPtrType {
				sm, ok := v.Elem().Interface().(*SurfaceMatch)
				if ok {
					return sm.Matched != nil && scan(reflect.ValueOf(sm.Matched))
				}
			}
			return scan(v.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				if scan(v.Index(i)) {
					return true
				}
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if scan(v.Field(i)) {
					return true
				}
			}
		}
		return false
	}
	return scan(reflect.ValueOf(body))
}

// lowerSurfaceMatch 將單個表層 match 節點展開為核心 AST。
// 先自底向上處理 matched 與各 arm 內部（嵌套 match），再建 if 鏈。
func (l *lowerer) lowerSurfaceMatch(sm *SurfaceMatch) Expression {
	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		if ident, ok := sm.Matched.(*Identifier); ok {
			fmt.Fprintf(os.Stderr, "[debug-it] lowerSurfaceMatch: matched=%q curFuncName=%q\n", ident.Value, l.curFuncName)
		} else if sm.Matched == nil {
			fmt.Fprintf(os.Stderr, "[debug-it] lowerSurfaceMatch: matched=nil (bare match) curFuncName=%q\n", l.curFuncName)
		} else {
			fmt.Fprintf(os.Stderr, "[debug-it] lowerSurfaceMatch: matched=%T curFuncName=%q\n", sm.Matched, l.curFuncName)
		}
	}
	// 先把析構綁定名/型別登記進符號表，內層 match 才能解析其主體型別。
	// PreChecked 由 lowerer.walk 的 defer 分支在預設路徑上已設（S1 翻轉後），
	// 此處跳過以免 js/wasm 展開路徑二次重報 `it` 非法使用等診斷。
	if !sm.PreChecked {
		l.preRegisterEnumArmBindings(sm)
		// `it` 只在「可證明是 ok」的臂裡有效：catch-all `->` 臂若前面沒有把 nil 與
		// err 都處理掉，走到這裡時值仍可能是 nil 或 err，讀 `it` 沒有意義（且會
		// 讀到未初始化的載荷）。此時若臂體用到 `it`，直接報錯——必須改用 `ok ->`
		// 或補上 `nil ->` / `err ->`。檢查必須在 walk 之前做：walk 會把內層 match
		// 展開成 if/else 並插入它自己的合成 `let it`，那之後就分不清 `it` 屬於哪層。
		l.rejectItInNonOkArm(sm)
		sm.PreChecked = true
	}
	l.walk(reflect.ValueOf(&sm.Matched))
	for i := range sm.Arms {
		a := &sm.Arms[i]
		l.walk(reflect.ValueOf(&a.Condition))
		for j := range a.MultiValuePatterns {
			l.walk(reflect.ValueOf(&a.MultiValuePatterns[j]))
		}
		if a.Body != nil {
			l.walk(reflect.ValueOf(a.Body))
		}
	}

	if sm.Matched == nil {
		// Sync curFuncName to parser for function-scoped VarType lookups.
		savedFuncName := l.p.curFuncName
		l.p.curFuncName = l.curFuncName
		var result Expression
		if plan := l.p.resolveBareMatchPlan(sm.Token, sm.Arms); plan != nil {
			result = l.p.emitMatchIfPlain(sm.Token, plan)
		}
		l.p.curFuncName = savedFuncName
		if ifExpr, ok := result.(*IfExpression); ok && ifExpr != nil {
			l.p.fmtTrivia.SetOpeningBraceComment(ifExpr, sm.OpeningBraceComment)
		}
		return result
	}
	// Sync curFuncName to parser for function-scoped VarType lookups.
	savedFuncName := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	// 類型解構 match 偵測：被匹配值靜態型別是 union（如 num）時，按型別臂靜態
	// 單態化派發，完全繞開 match 的 `matched == X` 運行期條件生成。
	//
	// 臂形態是第二條（S1 翻轉後必需的）判據：union 單態化複本的主體已是具體成員
	// 型別、且語義表沒有以複本名為鍵的條目，單靠「主體是 union」永遠判不出來
	// ——見 armsCarryTypePatterns 的註解（tests/type-match.no 迴歸案）。
	if ident, ok := sm.Matched.(*Identifier); ok {
		// 臂形前置守衛：含 option 臂（ok/nil/err）的 match 絕不可能是靜態型別
		// 派發 match，即使 subject 型別被（跨模組污染的）全域回退解析成 union。
		t := l.p.matchSubjectUnionType(l.curFuncName, ident.Value)
		if !hasOptionMatchArm(sm.Arms) &&
			(l.p.isUnionType(t) || l.p.armsCarryTypePatterns(sm.Arms)) {
			result := l.p.buildTypeMatchDesugar(sm, t)
			l.p.curFuncName = savedFuncName
			return result
		}
	}
	var result Expression
	// P7 (design doc §5): the SINGLE desugar path is the IR-agnostic resolve layer
	// + the field-less plain emitters; the legacy buildMatchDesugar / emitSubjectMatchAST
	// route has been deleted, and js/wasm already went through this path via
	// DesugarSurfaceMatchesPlain (plainMatch=true).
	if prep := l.p.resolveSubjectMatch(sm); l.p.subjectMatchDirectHIR(prep) {
		result = l.p.emitSubjectMatchPlain(prep)
	}
	l.p.curFuncName = savedFuncName
	return result
}

// armPreChecks 跑展開 match 前的兩道預檢：先登記析構綁定型別（供嵌套 match 解析
// 自身主體），再檢查 catch-all 臂裡的 `it` 用法。兩者本來都是 *lowerer 方法，
// 但都只讀 sm 與函式作用域符號表、只寫診斷與作用域內的綁定型別表 —— 不碰 AST，
// 所以可以掛在一隻暫存 lowerer 上給非變更的 desugarSurfaceMatch 用。
func (p *Parser) armPreChecks(sm *SurfaceMatch) {
	l := &lowerer{
		p:              p,
		curFuncName:    p.curFuncName,
		visited:        map[uintptr]bool{},
		declaredLocals: map[string]bool{},
	}
	if !sm.PreChecked {
		l.preRegisterEnumArmBindings(sm)
		l.rejectItInNonOkArm(sm)
		sm.PreChecked = true
	}
}

// findFuncDef returns the top-level FunctionDefinition named name in prog, or nil.
func findFuncDef(prog *Program, name string) *FunctionDefinition {
	if prog == nil || name == "" {
		return nil
	}
	for _, st := range prog.Statements {
		if fd, ok := st.(*FunctionDefinition); ok && fd != nil && fd.Name == name {
			return fd
		}
	}
	return nil
}

// astLocalVarType derives the static type of a function-local variable directly
// from the source AST, used as the S1-flip fallback when
// sem.FuncVarType(curFunc, subject) misses.
//
// Why this is needed: after the flip, std modules arrive from the baked blob in
// SURFACE form and are desugared at H0 (AST→HIR). Their local-let types were
// never collected into FuncVarTypes during baking (the match desugar that would
// register them was deferred past the bake), so the H0 dispatch lookup misses
// and would otherwise fall back to the GLOBAL, unscoped VarTypes — which can pick
// up a SAME-NAMED variable from a DIFFERENT module and emit wrong codegen.
// Deriving the type from the source declaration is sound and module-scoped.
//
// Resolution order mirrors how the variable is declared:
//  1. a function parameter with an explicit type,
//  2. a `let x : T = …` with an explicit annotation (typeString),
//  3. a `let x = call(…)` whose return type is known (returnTypeFromCallExpr),
//  4. a `let x = other` chain that resolves to one of the above.
//
// The parser must carry the Program (Parser.prog, set on the H0 scratch parser);
// if it is nil we return "" and the caller keeps its existing (weaker) fallback.
func (p *Parser) astLocalVarType(funcName, varName string) string {
	if p == nil || p.prog == nil || varName == "" || funcName == "" {
		return ""
	}
	// 在途守衛：本函式為「RHS 是呼叫」的 let 會問 returnTypeFromCallExpr，
	// 而它解析接收者型別時又會回到這裡（`x = f(x)`、`a = b` / `b = a`）。
	// 環上再問一次只會同義反覆，直接回空讓呼叫端用它的弱回退。
	if p.astVarTypeBusy == nil {
		p.astVarTypeBusy = make(map[string]bool)
	}
	busyKey := funcName + "\x00" + varName
	if p.astVarTypeBusy[busyKey] {
		return ""
	}
	// 結果快取：本函式每次都要 gatherLetsInFuncScope 走完整個函式體，而 H0
	// 降糖期 resolveReceiverType 會對同一個 (函式, 變數) 反覆發問（每個臂、每個
	// 巢狀 match 各一次）。沒有快取時 genstdsig 直接跑不完（O(N·M)，被 OOM/超時
	// 砍掉）。H0 期 AST 已凍結，快取安全；快取掛在 *Parser 上，不同 Program
	// 用不同 parser，不會互相污染。
	if v, ok := p.astVarTypeCache[busyKey]; ok {
		return v
	}
	p.astVarTypeBusy[busyKey] = true
	defer delete(p.astVarTypeBusy, busyKey)
	t := p.computeAstLocalVarType(funcName, varName)
	if p.astVarTypeCache == nil {
		p.astVarTypeCache = make(map[string]string)
	}
	p.astVarTypeCache[busyKey] = t
	return t
}

// computeAstLocalVarType is the uncached body of astLocalVarType.
func (p *Parser) computeAstLocalVarType(funcName, varName string) string {
	fd := findFuncDef(p.prog, funcName)
	if fd == nil {
		// Monomorphized / suffixed copies keep the original declaration under a
		// base name; strip a trailing "@…" suffix and retry.
		if idx := strings.IndexByte(funcName, '@'); idx > 0 {
			fd = findFuncDef(p.prog, funcName[:idx])
		}
	}
	if fd == nil || fd.Body == nil {
		return ""
	}
	types := map[string]string{}
	for _, prm := range fd.Parameters {
		if prm != nil && prm.Name != "" && prm.Type != nil {
			if t := typeString(prm.Type); t != "" {
				types[prm.Name] = t
			}
		}
	}
	// First pass: explicit annotations + call-RHS return types.
	for _, ls := range gatherLetsInFuncScope(fd.Body) {
		if ls == nil || ls.Name == nil || ls.Name.Value == "" || ls.IsSynthetic {
			continue
		}
		name := ls.Name.Value
		if _, exists := types[name]; exists {
			continue
		}
		if ls.Type != nil {
			if t := typeString(ls.Type); t != "" {
				types[name] = t
				continue
			}
		}
		if call, ok := ls.Value.(*CallExpression); ok {
			if rt := p.returnTypeFromCallExpr(call); rt != "" {
				types[name] = rt
			}
		}
	}
	// Second pass: follow identifier RHS chains (`x = otherVar`).
	for _, ls := range gatherLetsInFuncScope(fd.Body) {
		if ls == nil || ls.Name == nil || ls.Name.Value == "" || ls.IsSynthetic {
			continue
		}
		name := ls.Name.Value
		if _, ok := types[name]; ok {
			continue
		}
		if id, ok := ls.Value.(*Identifier); ok {
			if t, ok := types[id.Value]; ok {
				types[name] = t
			}
		}
	}
	// Third pass: 多重賦值 `_, mb = f(...)` —— 目標型別按位置取 RHS 呼叫的多值
	// 返回值簽名。缺這遍時 `mb`（src/std/str.no 的 str-to-f64：
	// `_, mb = mw.div-mod(two52)`）兩邊都查不到——不在參數、不是 let（多目標
	// 賦值是 MultiAssignStatement），於是 `mb.to-i64()` 的接收者型別為空、回傳
	// 型別推不出、值位 match 主語不歸一化、臂內 `it` 沒有綁定，最後在 codegen
	// 炸成 `unknown callee byte.to-f64`。
	for _, ma := range gatherMultiAssignsInFuncScope(fd.Body) {
		if ma == nil || len(ma.Targets) == 0 {
			continue
		}
		call, ok := ma.Value.(*CallExpression)
		if !ok {
			continue
		}
		rets := p.multiReturnTypesFromCallExpr(call)
		if len(rets) == 0 {
			continue
		}
		for i, tgt := range ma.Targets {
			if i >= len(rets) {
				break
			}
			id, ok := tgt.(*Identifier)
			if !ok || id.Value == "" || id.Value == "_" {
				continue
			}
			if _, exists := types[id.Value]; exists {
				continue
			}
			if rets[i] != "" {
				types[id.Value] = rets[i]
			}
		}
	}
	return types[varName]
}

// prepareSurfaceMatch clones the match, carries annotations onto the clone,
// and runs the arm pre-checks. Every step here has side effects that later
// lowering depends on:
//
//   - the clone is what the emitters mutate (arm bodies get the narrowing let
//     prepended), so the caller's SurfaceMatch stays untouched — required, since
//     the same tree is walked more than once (checker, sugar diagnostics);
//   - annotations are keyed by node pointer, so a bare clone loses them and
//     the arm-body rewrites (rewriteItPlaceWrites) silently stop seeing them;
//   - armPreChecks registers destructuring bindings so an INNER match can
//     resolve its own subject type, and validates `it` usage while that is
//     still unambiguous (before any synthetic `let it` exists).
//
// Both the AST chain and the streaming HIR emitter call it (S-a).
func (p *Parser) prepareSurfaceMatch(sm *SurfaceMatch) *SurfaceMatch {
	arms := make([]MatchArm, len(sm.Arms))
	copy(arms, sm.Arms)
	clone := *sm
	clone.Arms = arms
	if anns := p.sem.RawAnnotationsOf(sm); len(anns) > 0 {
		p.sem.SetRawAnnotations(&clone, anns)
	}
	p.armPreChecks(&clone)
	return &clone
}

// matchSubjectUnionType 解析「類型解構 match 主體」的靜態型別，且刻意繞開
// SemanticContext.FuncVarType 對全域 VarTypes 的回退。單態化複本／模板（名字含
// "__"）在語義表裡沒有以自己函數名為鍵的條目，若回退到全域 VarTypes，主體參數名
// 可能被 OTHER 模組的同名全域變數污染（例：bigint._add 的運算元 `b` 抓到
// tests/type-match.no 模組級 `b: num`），把臂拿去對錯誤 union 校驗而誤報
// "arm pattern X is not a member of union num"（lowering.go 頂部已記錄此類跨模組
// 污染案）。複本的正解是 matchedVarType==""（跳過成員集校驗，改由臂形態 + MIR 具體
// 型別靜態選臂），故對 "__" 函數名不回退全域；原始函數仍走 per-func→全域→AST-local。
func (p *Parser) matchSubjectUnionType(funcName, varName string) string {
	if p.sem != nil && p.sem.FuncVarTypes != nil {
		if vars, ok := p.sem.FuncVarTypes[funcName]; ok {
			if t, ok := vars[varName]; ok && t.Raw() != "" {
				return t.Raw()
			}
		}
	}
	if strings.Contains(funcName, "__") {
		// 單態化複本／模板：不回退全域，交由臂形態判據 + 具體型別選臂。
		return ""
	}
	if t, ok := p.sem.VarTypes[varName]; ok && t.Raw() != "" {
		return t.Raw()
	}
	if at := p.astLocalVarType(funcName, varName); at != "" {
		return at
	}
	return ""
}

// typeDestructuringSubjectType reports whether sm is a type-destructuring match
// and returns the subject type to dispatch on, with the union alias recovered
// for union-monomorphized clones (see recoverUnionFromMember).
//
// A union-monomorphized clone carries a CONCRETE member type (e.g. i32) as its
// subject instead of the union (num), so agreeing on "is this a type match" is
// not enough — the recovered type has to agree too, or the two emitters would
// validate different member sets.
func (p *Parser) typeDestructuringSubjectType(sm *SurfaceMatch) (string, bool) {
	if sm == nil || sm.Matched == nil || len(sm.Arms) == 0 {
		return "", false
	}
	ident, ok := sm.Matched.(*Identifier)
	if !ok {
		return "", false
	}
	t := p.matchSubjectUnionType(p.curFuncName, ident.Value)
	var armPatterns []string
	for i := range sm.Arms {
		a := &sm.Arms[i]
		if a.IsTypeArm {
			armPatterns = append(armPatterns, a.TypePattern)
		} else if id, ok := a.Condition.(*Identifier); ok && p.isUnionMemberName(id.Value) {
			armPatterns = append(armPatterns, id.Value)
		}
	}
	if !hasOptionMatchArm(sm.Arms) && p.armsCarryTypePatterns(sm.Arms) {
		return p.recoverUnionFromMember(t, armPatterns), true
	}
	return "", false
}

// isOptionMatchArm reports whether a match arm is an option arm (ok/nil/err)
// rather than a type-pattern arm. hasOptionMatchArm is the slice-level form.
//
// Why this exists (measured, not assumed): SemanticContext.FuncVarType falls back
// to a GLOBAL VarTypes table that has NO file/module scoping
// (parser/resolver.go:617-628). After the S1 flip, std modules arrive from the
// baked blob in SURFACE form and are desugared at H0, where their per-function
// type table was never collected. A match on a LOCAL therefore resolves through
// the global fallback and can pick up a SAME-NAMED GLOBAL from a different
// module. Measured failure: building tests/type-match.no desugared fs.no's
// `fs.file.read-str` match on local `b` as a *type* match, because VarTypes["b"]
// held type-match.no's module-level `b` (type `num`, a union) — producing
// `type match arm pattern "err" is not a member of union "num"` against arms that
// are actually ok/nil/default.
//
// Arm shape is the sound discriminator: option arms and type-pattern arms are
// mutually exclusive by construction, so a match carrying any option arm is never
// a static type-dispatch match — whatever the subject's (possibly mis-resolved)
// type says. Guarding on this avoids having to make VarTypes module-scoped.
func isOptionMatchArm(a *MatchArm) bool {
	if a == nil {
		return false
	}
	switch c := a.Condition.(type) {
	case *NilLiteral:
		return true
	case *CallExpression:
		if id, ok := c.Function.(*Identifier); ok {
			switch id.Value {
			case "ok", "err":
				return true
			}
		}
	case *Identifier:
		switch c.Value {
		case "ok", "nil", "err":
			return true
		}
	}
	return a.IsDotVal
}

func hasOptionMatchArm(arms []MatchArm) bool {
	for i := range arms {
		if isOptionMatchArm(&arms[i]) {
			return true
		}
	}
	return false
}

// isUnionMemberName reports whether name is a member of ANY known union alias
// (e.g. i64 is a member of `num = i32 | i64 | f64`). Only DIRECT members are
// recorded (parser/type.go:728-741), so a member reached through a nested
// alias (`mixed = num | str` ⇒ i64) is found because i64 is ALSO a direct
// member of num.
func (p *Parser) isUnionMemberName(name string) bool {
	if name == "" || p.unionMembers == nil {
		return false
	}
	// 前綴化具體型別（`bigint.bigint`）與裸名成員（`bigint`）末段一致即同一型別；
	// 內建純量末段==原名，比對退化为精確匹配，无行为变化。
	nameTail := matchTypeTail(name)
	for _, members := range p.unionMembers {
		for _, m := range members {
			if m == name || matchTypeTail(m) == nameTail {
				return true
			}
		}
	}
	return false
}

// armsCarryTypePatterns reports whether any arm is a type-destructuring
// pattern: the explicit `v T ->` form (parser sets IsTypeArm,
// parser/expr.go:1152-1158) or a BARE `T ->` arm whose condition names a
// union member type.
//
// Why the arm shape must be able to drive the dispatch on its own: both
// dispatch sites (lowerSurfaceMatch / desugarSurfaceMatch) keyed on the
// SUBJECT's static type being a union. After union monomorphization
// (build/transpiler_monomorph.go cloneUnionVariant) the subject is the
// CONCRETE member (i64) and the clone carries no
// SemanticContext.FuncVarTypes entry under its own name, so the union test
// can never fire for a clone. Measured regressions from the S1 flip (which
// moved desugaring to H0, i.e. AFTER monomorphization):
//
//	tests/type-match.no — `v i64 -> v.to-str()` desugared as the runtime
//	  value comparison `x == i64` against an undefined identifier ⇒
//	  "unknown callee v.to-str in func classify__i64" (compile error).
//	a bare-arm program (`r = x: { i32 -> lo, i64 -> hi, -> x }`) — same
//	  mis-desugar, but it compiles and TRAPS at runtime (SIGTRAP, exit 133).
//
// Before the flip this could not happen: the parser desugared the match
// BEFORE monomorphization cloned the function, so the clone carried an
// already-desugared if-chain. The arm shape is the only signal that survives
// cloning, and it is sound: option arms are excluded by hasOptionMatchArm,
// and a value match never names a union member as its pattern.
func (p *Parser) armsCarryTypePatterns(arms []MatchArm) bool {
	for i := range arms {
		a := &arms[i]
		if a.IsTypeArm {
			return true
		}
		if id, ok := a.Condition.(*Identifier); ok && p.isUnionMemberName(id.Value) {
			return true
		}
	}
	return false
}

// buildTypeMatchDesugar 建立「類型解構 match」的核心 AST（union / type-set
// 靜態單態化派發）。
//
// 被匹配值靜態型別是 union（如 num = i32 | i64 | f64）；arm 模式為型別名
// （i32 ->）、帶綁定（v i32 ->）或預設臂（->）。不產生 `matched == X` 運行期
// 條件：選臂在 MIR 階段按單態化後的具體型別完成（靜態派發，零執行期開銷、
// 零 type tag）。每個型別臂內注入 `it = matched`（或 `v = matched`）並收窄為
// 該成員型別，供 checker 型別推斷與臂內操作使用。
func (p *Parser) buildTypeMatchDesugar(sm *SurfaceMatch, matchedVarType string) Expression {
	tok, arms := sm.Token, sm.Arms
	if len(arms) == 0 {
		return nil
	}
	resolved := p.resolveTypeMatchArms(sm, matchedVarType)
	if len(resolved) == 0 {
		return nil
	}
	// 由內而外建 if/elif/else 鏈；MIR 按單態化後的具體型別選臂（不依賴此鏈結構）。
	var ifExpr *IfExpression
	for i := len(resolved) - 1; i >= 0; i-- {
		ra := resolved[i]
		node := &IfExpression{
			Token:       tok,
			Condition:   nil,
			Consequence: ra.body,
		}
		if ifExpr != nil {
			node.Alternative = &BlockStatement{
				Token:      tok,
				Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
			}
		}
		ifExpr = node
	}
	return ifExpr
}

// typeMatchArm is the IR-agnostic result of resolving one arm of a
// type-destructuring match: the member type it selects, the optional binding
// name, whether it is the default arm, and its body with the narrowing let
// (`it = matched` / `v = matched`) already prepended.
//
// S-a (design doc §5.2): this is what keeps the two emitters from drifting.
// buildTypeMatchDesugar turns it into the legacy IfExpression chain, while
// lowerTypeMatchToHIR streams KTypeArm nodes straight out of it. Both run the
// SAME per-arm decisions and side effects because both call
// resolveTypeMatchArms.
type typeMatchArm struct {
	typePattern string
	bindingName string
	isDefault   bool
	body        *BlockStatement
}

// resolveTypeMatchArms validates every arm against the union's member set and
// injects the narrowing binding. It is deliberately IR-agnostic: it returns
// resolved arms rather than AST nodes.
func (p *Parser) resolveTypeMatchArms(sm *SurfaceMatch, matchedVarType string) []typeMatchArm {
	tok, matched, arms := sm.Token, sm.Matched, sm.Arms
	if len(arms) == 0 {
		return nil
	}
	// 成員表用於「臂模式是否屬於該 union」的靜態校驗。主體型別未知時
	// （matchedVarType == ""，例如 union 單態化複本：cloneUnionVariant 只改
	// 函數簽名，語義表沒有以複本名為鍵的條目）跳過校驗——flattenUnion 對
	// 未知型別會回傳 [""] 這個葉子佔位，若拿它當成員集，每個型別臂都會被
	// 誤判成 "not a member of union \"\""（S1 翻轉後單態化複本在 H0 降糖的
	// 必經之路，見 armsCarryTypePatterns 註釋）。
	members := p.flattenUnion(matchedVarType, nil)
	if matchedVarType == "" {
		members = nil
	}
	memberSet := make(map[string]bool, len(members))
	for _, m := range members {
		memberSet[m] = true
	}

	resolved := make([]typeMatchArm, len(arms))
	for i := range arms {
		a := &arms[i]
		var ra typeMatchArm
		ra.body = a.Body
		if a.IsTypeArm {
			ra.typePattern = a.TypePattern
			ra.bindingName = a.TypeBindingName
			if len(members) > 0 && !memberSet[a.TypePattern] {
				p.saveErrorf("qkce8wnr", "line %d, column %d: type match arm pattern %q is not a member of union %q",
					a.Pos.Line, a.Pos.Column, a.TypePattern, matchedVarType)
			}
		} else if a.IsWildcard {
			ra.isDefault = true
		} else if ident, ok := a.Condition.(*Identifier); ok {
			ra.typePattern = ident.Value
			if len(members) > 0 && !memberSet[ident.Value] {
				p.saveErrorf("plsdygti", "line %d, column %d: type match arm pattern %q is not a member of union %q",
					a.Pos.Line, a.Pos.Column, ident.Value, matchedVarType)
			}
		} else {
			p.saveErrorf("75yiyise", "line %d, column %d: type match on union %q requires type patterns (e.g. %q ->) or a default arm (->)",
				a.Pos.Line, a.Pos.Column, matchedVarType, firstMember(members))
			ra.isDefault = true
		}
		// 注入收窄綁定：型別臂內 `it`（或 v）按成員型別可見。
		if !ra.isDefault && ra.typePattern != "" {
			bindName := ra.bindingName
			if bindName == "" {
				bindName = "it"
			}
			if bindName != "it" {
				if p.sem.DeclaredVars == nil {
					p.sem.DeclaredVars = make(map[string]bool)
				}
				p.sem.DeclaredVars[bindName] = true
				p.sem.SetFuncVarType(p.curFuncName, bindName, ra.typePattern)
				p.setVarType(bindName, ra.typePattern)
			}
			bind := &LetStatement{
				Token:        tok,
				Name:         &Identifier{Token: tok, Value: bindName},
				Value:        matched,
				Type:         &NamedType{Value: ra.typePattern},
				IsSynthetic:  true,
				IsArmBinding: true,
			}
			ra.body = p.prependStmt(ra.body, bind)
		}
		resolved[i] = ra
	}
	return resolved
}

// firstMember 回傳 union 成員清單的第一個（用於錯誤訊息示例），空則回傳 "i32"。
func firstMember(members []string) string {
	if len(members) > 0 {
		return members[0]
	}
	return "i32"
}

// ---- desugar 構建器（原 desugar.go，現屬 lowering 層） ----

// buildBareMatchDesugar 建立 if/elif/else 鏈（無 matched expression，條件直接使用）
//
// 當所有 arm 都是非 wildcard 且數量大於 1 時，使用 independent-guard 語義：
// 每個條件獨立判定，所有命中的 arm 都會執行（而非 elif 只走第一個 true 臂）。
// 這對應 uuid.no 之类的 guard 模式：
//
//	{ i+1 < 16 -> { ... }  i+2 < 16 -> { ... }  i+3 < 16 -> { ... } }
//
// 三個條件可同時成立，elif 會丟棄第 2/3 個字節寫入。
// 結構化為 `if(1){ if(c1){B1}; if(c2){B2}; ... }`，hir2mir lowerBlock
// 逐條 lowerStmt 使每個 KIf 獨立求值。
//
// 含有 wildcard 臂（→ default）或僅一個臂時沿用既有 elif 鏈語義。
// matchIfPlan is the IR-agnostic shape of an if/elif/else chain lowered from a
// match — bare (no subject) and subject matches alike. One decision function per
// match kind fills the plan, and both emitters consume it, so they cannot drift:
//
//   - resolveBareMatchPlan      -> buildBareMatchDesugar -> emitMatchIfAST;
//   - resolveSubjectMatchPlan   -> emitSubjectMatchAST / emitSubjectMatchHIR (S-c1).
//
// alt vs altBody matters for byte-identical HIR: an else branch that is a nested
// guard is wrapped in BlockStatement{ExpressionStatement{...}} (which lowers to
// KBlock -> KExprStmt -> KIf), while a default arm's body is Alternative
// directly (which lowers to that body's own KBlock).
//
// The subject-only fields (matched / optionPatterns / valuePatterns / rawCond)
// are nil for a bare match, so the bare emitter ignores them; they exist because
// a subject match's KIf carries nine slots, not six (see emitMatchIfHIR).
type matchIfPlan struct {
	// isWrapper marks the `if(1){ ... }` form used by the bare-match
	// independent-guard pattern and, later, by the subject-match tail wrappers.
	isWrapper      bool
	cond           Expression
	eqPattern      Expression
	rangePattern   *RangeExpression
	dotValBody     *BlockStatement
	body           *BlockStatement
	alt            *matchIfPlan
	altBody        *BlockStatement
	stmts          []*matchIfPlan
	rtFlags        RTFlag
	matched        Expression
	optionPatterns []string
	valuePatterns  []Expression
	rawCond        Expression
}

// resolveBareMatchPlan builds the bare-match plan. Faithful port of the original
// in-buildBareMatchDesugar logic: same fast path condition, same dotVal /
// wildcard / defaultBody rules, same arm order.
func (p *Parser) resolveBareMatchPlan(tok lexer.Token, arms []MatchArm) *matchIfPlan {
	if len(arms) == 0 {
		return nil
	}

	// ---- independent-guard fast path ----
	allNonWildcard := true
	for _, arm := range arms {
		if arm.IsWildcard {
			allNonWildcard = false
			break
		}
	}
	if allNonWildcard && len(arms) > 1 {
		w := &matchIfPlan{
			isWrapper: true,
			cond:      &IntegerLiteral{Token: tok, Value: 1},
		}
		for i := range arms {
			sub := &matchIfPlan{
				cond:    arms[i].Condition,
				body:    arms[i].Body,
				rtFlags: RTBareMatch,
			}
			if rng, isRange := arms[i].Condition.(*RangeExpression); isRange {
				sub.rangePattern = rng
			} else {
				sub.eqPattern = arms[i].Condition
			}
			w.stmts = append(w.stmts, sub)
		}
		return w
	}

	// ---- elif chain (original logic) ----
	var cur *matchIfPlan
	var defaultBody *BlockStatement
	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]
		if arm.IsWildcard {
			if cur == nil {
				switch {
				case defaultBody != nil && arm.IsDotVal:
					// dotVal arm (ok->) after a regular -> wildcard (else):
					// treat as conditional arm: if ok { body } else { defaultBody }
					cur = &matchIfPlan{
						cond:       &Identifier{Token: tok, Value: "ok"},
						body:       arm.Body,
						altBody:    defaultBody,
						eqPattern:  &Identifier{Token: tok, Value: "ok"},
						dotValBody: arm.Body,
						rtFlags:    RTBareMatch | RTMatchWildcard,
					}
				case arm.IsDotVal:
					// dotVal arm without prior default — standalone ok condition
					cur = &matchIfPlan{
						cond:       &Identifier{Token: tok, Value: "ok"},
						body:       arm.Body,
						eqPattern:  &Identifier{Token: tok, Value: "ok"},
						dotValBody: arm.Body,
						rtFlags:    RTBareMatch | RTMatchWildcard,
					}
				default:
					// Regular wildcard (->): store as defaultBody for next arm's else
					cur = &matchIfPlan{
						cond:    &IntegerLiteral{Token: tok, Value: 1},
						body:    arm.Body,
						rtFlags: RTBareMatch | RTMatchWildcard,
					}
				}
			} else {
				if arm.IsDotVal {
					// dotVal arm (ok->) when other arms already processed: wrap as outer condition
					cur = &matchIfPlan{
						cond:       &Identifier{Token: tok, Value: "ok"},
						body:       arm.Body,
						alt:        cur,
						eqPattern:  &Identifier{Token: tok, Value: "ok"},
						dotValBody: arm.Body,
						rtFlags:    RTBareMatch | RTMatchWildcard,
					}
				} else {
					if cur.alt == nil && cur.altBody == nil {
						cur.altBody = arm.Body
					}
				}
			}
			if !arm.IsDotVal && defaultBody == nil {
				defaultBody = arm.Body
			}
		} else {
			sub := &matchIfPlan{
				cond:    arm.Condition,
				body:    arm.Body,
				rtFlags: RTBareMatch,
			}
			if rng, isRange := arm.Condition.(*RangeExpression); isRange {
				sub.rangePattern = rng
			} else {
				sub.eqPattern = arm.Condition
			}
			if cur != nil {
				sub.alt = cur
			}
			cur = sub
		}
	}
	return cur
}

// emitMatchIfPlain is the js/wasm re-home counterpart of emitMatchIfAST (design
// doc §5 P7). It builds the SAME if/elif/else chain structure — identical
// Condition / Consequence / Alternative nesting — but writes NONE of the
// match-specific IfExpression fields (EqualityPattern / RangePattern / DotValBody)
// and no RTFlag. The js/wasm backends consume only the if-chain shape (they never
// read those fields), so the emitted JS/WASM is byte-identical to the legacy
// route while keeping js/wasm off buildMatchDesugar's dependency chain.
func (p *Parser) emitMatchIfPlain(tok lexer.Token, plan *matchIfPlan) Expression {
	if plan.isWrapper {
		stmts := make([]Statement, 0, len(plan.stmts))
		for _, s := range plan.stmts {
			stmts = append(stmts, &ExpressionStatement{
				Token:      tok,
				Expression: p.emitMatchIfPlain(tok, s),
			})
		}
		return &IfExpression{
			Token:     tok,
			Condition: plan.cond,
			Consequence: &BlockStatement{
				Token:      tok,
				Statements: stmts,
			},
		}
	}
	node := &IfExpression{
		Token:       tok,
		Condition:   plan.cond,
		Consequence: plan.body,
	}
	if plan.alt != nil {
		node.Alternative = &BlockStatement{
			Token:      tok,
			Statements: []Statement{&ExpressionStatement{Token: tok, Expression: p.emitMatchIfPlain(tok, plan.alt)}},
		}
	} else if plan.altBody != nil {
		node.Alternative = plan.altBody
	}
	return node
}

// subjectMatchPrep is the IR-agnostic state buildMatchDesugar computes BEFORE
// it starts emitting arms (design doc §5.2.9, segments A+B+C). Everything here is
// either a decision (which subject variable to match on, what `it` type each arm
// binds, which variant a wildcard else arm stands for) or a side effect already
// applied to the tree (arm bodies rewritten for the zero-default / place-write /
// destructuring-binding cases).
//
// The split exists so the S-c HIR emitter can share the preparation exactly the
// way S-a/S-b do: resolveSubjectMatch runs the side effects once, then
// emitSubjectMatchAST and (S-c1) emitSubjectMatchHIR consume the same prep. Two
// emitters over one prep is what keeps the AST and HIR routes from drifting.
//
// ⚠️ placeWriteUsed is MUTATED by the emitter (rewriteItPlaceWrites reports back
// through it) and then READ by the tail wrappers, so it lives on the prep rather
// than being a local of either phase.
type subjectMatchPrep struct {
	tok                lexer.Token
	matched            Expression
	arms               []MatchArm
	origMatched        Expression
	subjName           string
	subjElem           string
	origIdxExpr        Expression
	idxTempName        string
	// baseLeft 是寫回的容器基底**表達式**（不是名字）：`a[i]` 的 `a`、`.limit[i]`
	// 的 `.limit`、`a.b.f[i]` 的 `a.b.f`。早期只存 string 名字，欄位基底因此
	// 拿不到、place-write 靜默不寫回（lowering.go 舊 baseName == "" 早退）。
	baseLeft           Expression
	placeWriteUsed     bool
	anyArmValueCtx     bool
	matchedVarType     string
	isEnumType         bool
	enumVariants       []string
	elemType           string
	itStmt             *LetStatement
	hasRawCond         bool
	hasExplicitOk      bool
	hasExplicitErr     bool
	hasExplicitNil     bool
	enumListedVariants map[string]bool
	matchedIsEnum      bool
}

// matchSubjectTypeRaw is the declared type of a synthesised `__match_subj`
// binding, given the ELEMENT type of the indexed container.
//
// It is `?<element>` for an ordinary container, but a container OF OPTIONS
// (`[]?T`) has an element that is already `?T`, and the index read yields it
// directly — `??T` is not a type any read can produce. All three places that
// need this string (the semantic side table, emitSubjectMatchAST and
// emitSubjectMatchHIR) go through here so they cannot drift: when the AST/HIR
// node said `??str` while the side table said `?str`, the arm's `it`
// (correctly `str`) was read out of a doubly-wrapped slot and `it + '-'` fed a
// `%option` to @str_concat.
func matchSubjectTypeRaw(elem string) string {
	if ty.IsOptionRaw(elem) {
		return elem
	}
	return "?" + elem
}

// subjTypeRaw is the prep-level spelling of matchSubjectTypeRaw.
func (p *subjectMatchPrep) subjTypeRaw() string { return matchSubjectTypeRaw(p.subjElem) }

// resolveSubjectMatch runs every side effect buildMatchDesugar performs on the
// tree and on the parser side tables, and returns the resulting plan. It is
// deliberately free of any emission: no IfExpression is built here.
//
// The side effects, in order (§5.2.9 segments A-C):
//
//	A. subject normalisation — an index-expression subject of an option match is
//	   replaced by a synthesised `__match_subj_L_C ?elem` identifier (registered
//	   in FuncVarType/VarTypes), and the D9 place-write scratch names are chosen;
//	B. type inference + empty-arm defaults — matchedVarType / enumVariants /
//	   elemType come from the semantic side tables (with the astLocalVarType
//	   fallback), and value-context empty arms get their zero-value body;
//	C. `it` binding + enum destructuring — the shared binding is built, hasRawCond
//	   is probed, and `some(v) ->` payload-destructuring arms are normalised while
//	   the explicit-variant bookkeeping (hasExplicit*, enumListedVariants) is
//	   filled in.

func (p *Parser) resolveSubjectMatch(sm *SurfaceMatch) *subjectMatchPrep {
	prep := &subjectMatchPrep{}
	tok, matched, arms := sm.Token, sm.Matched, sm.Arms
	// NOTE: prep.matched is deliberately NOT captured here. When the subject is a
	// safe-index expression in an option match (e.g. `a[i]: { ok -> ... }`), the
	// block below normalises `matched` to a synthetic `__match_subj` option
	// identifier so the inner match reuses the identifier option-match path
	// (tag comparison + correct `it` binding). prep.matched must reflect that
	// *normalised* value, otherwise the chain conditions and `it` binding still
	// reference the raw `a[i]` index expression and degrade into a value
	// comparison (`a[i] == 0`) instead of a `__match_subj == ok` tag check.
	// prep.origMatched intentionally keeps the raw index for the wrapper that
	// builds `__match_subj ?elem = <origMatched>`.
	prep.tok, prep.arms, prep.origMatched = tok, arms, matched
	if len(arms) == 0 {
		return nil
	}

	// 索引表達式主體（如 `parts[i]`）的安全索引回傳 Option（?elem）。當 match
	// 是 option match（arm 含 ok/nil/err 或 `ok ->`）時，把主體歸一化為合成的
	// ?elem 區域變數，複用識別符 option-match desugar 路徑：
	//
	//   `parts[i]: { ok -> ... -> ... }`  →
	//   `if 1 { __match_subj_L_C ?elem = parts[i]; __match_subj_L_C: { ok -> ... -> ... } }`
	//
	// 這樣 `it` 正確綁定 payload、條件走 tag 比較，而非退化成
	// `streq(parts[i], '')` 字串比較（那會讓 `s = it` 變成 void，codegen 後
	// 報 "receiver has no slot"）。歸一化只在 option match 上觸發：值 match
	// （如 `arr[i]: { 0 -> }`）保持原本的裸元素比對，不受影響。
	origMatched := matched
	subjName := ""
	subjElem := ""
	// 「這是不是 option match」——臂裡出現 `ok ->`（IsDotVal）或 ok/nil/err 模式
	// 即成立。索引與呼叫兩種主語的歸一化共用此判定。
	isOptMatch := false
	for _, a := range arms {
		if a.IsDotVal {
			isOptMatch = true
			break
		}
		if ident, ok := a.Condition.(*Identifier); ok {
			if ident.Value == "ok" || ident.Value == "nil" || ident.Value == "err" {
				isOptMatch = true
				break
			}
		} else if _, ok := a.Condition.(*NilLiteral); ok {
			isOptMatch = true
			break
		}
	}
	if isOptMatch {
		subjValueElem := ""
		if idx, ok := matched.(*IndexExpression); ok && p.isSafeIndexBase(idx) {
			subjValueElem = p.inferIndexElemType(idx)
		} else if call, ok := matched.(*CallExpression); ok {
			// 呼叫主語（`v = mb.to-i64(): { ok -> it.to-f64()  -> 0.0 }`）：回傳
			// `?T` 時同樣歸一化。不歸一化會同時壞兩件事——
			//   ① buildItBinding 只認 Identifier / IndexExpression，主語是呼叫時
			//      `it` 根本沒有綁定，臂內 `it.<method>()` 拿接收者殘型別猜
			//      （yaml 的 `unknown callee byte.to-f64`）；
			//   ② `it = <call>` 若真的生成，呼叫會在每個臂裡重求值一次，而條件鏈
			//      又求值一次 —— 有副作用的呼叫會多跑。
			// 歸一化走既有的 `if 1 { let __match_subj ?T = <orig>; <chain> }`
			// wrapper（下方 emitSubjectMatchPlain 4028 段），呼叫恰好求值一次。
			// 只認 option 回傳；union（`i64 | err`）主語維持原樣，不在本次擴張。
			if rt := p.returnTypeFromCallExpr(call); rt != "" && ty.IsOptionRaw(rt) {
				subjValueElem = ty.StripOptionRaw(rt)
			}
		}
		if subjValueElem != "" {
			subjElem = subjValueElem
			subjName = fmt.Sprintf("__match_subj_%d_%d", tok.Line, tok.Column)
			subjType := matchSubjectTypeRaw(subjElem)
			p.sem.SetFuncVarType(p.curFuncName, subjName, subjType)
			p.setVarType(subjName, subjType)
			matched = &Identifier{Token: tok, Value: subjName}
		}
	}
	// Capture the (possibly normalised) subject now that the index→`__match_subj`
	// rewrite above has run. For an option match on a safe-index subject this is
	// the synthetic `__match_subj` identifier; for every other subject it is the
	// original expression. The chain conditions and `it` binding in both emitters
	// read prep.matched, so this single assignment is what makes the normalised
	// identifier path actually used.
	prep.matched = matched

	// D9 place-write 準備：statement 語境的安全索引 match（`a[i]: { ok -> it = v,
	// -> _ }`）的 ok 臂裡，`it` 指代**元素位置**而非載荷副本。為了讓「option 讀取」
	// 與「寫回 store」共用同一次下標求值（下標表達式可能有副作用，只能求值一次），
	// 把原下標表達式綁定到合成變數 __idx_L_C，讀取與寫回都透過它索引。只有臂裡
	// 真的出現 `it = v` 時才產生這個綁定（placeWriteUsed）——純讀取 match 零額外
	// 開銷。origIdxExpr 必須在任何改寫之前捕獲（寫回前讀取的 Index 會被替換成
	// 暫存識別符）。
	var origIdxExpr Expression
	idxTempName := ""
	var baseLeft Expression
	if subjName != "" {
		if idx, ok := origMatched.(*IndexExpression); ok {
			origIdxExpr = idx.Index
			idxTempName = fmt.Sprintf("__idx_%d_%d", tok.Line, tok.Column)
			// 基底可以是表達式：`a[i]` 的識別符、`.limit[i]`／`self.f[i]`／
			// `a.b.f[i]` 的 DotExpression（含元素作接收者的 `a[i].f[j]`）。
			// 深拷貝避免寫回節點與原下標共享同一棵子樹（同時被走訪兩次）。
			baseLeft = cloneIndexBase(idx.Left)
		}
	}
	placeWriteUsed := false
	anyArmValueCtx := false
	for _, a := range arms {
		if a.ValueCtx {
			anyArmValueCtx = true
		}
	}

	// D11：`#{index-out=DEF}` 註解已徹底移除，不再有「越界取 DEF」的臂內 RHS
	// 降糖，故不再需要取出其預設值（原 idxOutDef 計算鏈已刪除）。

	// 類型推斷結果取自語義副表（Resolver pass 寫入 p.sem），不再依賴解析期快照。
	// Use function-scoped lookup (curFuncName) to avoid cross-function type
	// pollution when same-named locals exist in different functions.
	matchedVarType := ""
	isEnumType := false
	var enumVariants []string
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := p.sem.FuncVarType(p.curFuncName, ident.Value); ok {
			matchedVarType = t
			if vs, ok := p.sem.EnumVariantsOf(t); ok {
				isEnumType = true
				enumVariants = vs
			}
			if os.Getenv("NOLANG_DEBUG_IT") != "" {
				fmt.Fprintf(os.Stderr, "[debug-it] buildMatchDesugar: matched=%q matchedVarType=%q\n", ident.Value, matchedVarType)
			}
		} else {
			// S1 flip fallback: FuncVarType can miss for blob-loaded std
			// functions whose local-let types were never collected into the
			// side table (the match desugar that would register them was
			// deferred past the bake). Derive the subject type directly from the
			// source AST (let annotation / call return type / identifier chain)
			// instead of the unscoped global VarTypes fallback.
			if at := p.astLocalVarType(p.curFuncName, ident.Value); at != "" {
				matchedVarType = at
				if vs, ok := p.sem.EnumVariantsOf(at); ok {
					isEnumType = true
					enumVariants = vs
				}
			}
			if os.Getenv("NOLANG_DEBUG_IT") != "" {
				if matchedVarType != "" {
					fmt.Fprintf(os.Stderr, "[debug-it] buildMatchDesugar: matched=%q AST-derived matchedVarType=%q\n", ident.Value, matchedVarType)
				} else {
					fmt.Fprintf(os.Stderr, "[debug-it] buildMatchDesugar: matched=%q FuncVarType NOT FOUND curFunc=%q\n", ident.Value, p.curFuncName)
					// Dump FuncVarTypes for curFuncName to see what's registered
					if vars, ok := p.sem.FuncVarTypes[p.curFuncName]; ok {
						for k, v := range vars {
							fmt.Fprintf(os.Stderr, "[debug-it]   FuncVarTypes[%q][%q]=%q\n", p.curFuncName, k, v)
						}
					} else {
						fmt.Fprintf(os.Stderr, "[debug-it]   FuncVarTypes[%q] map not found\n", p.curFuncName)
					}
				}
			}
		}
	}

	// 模組合併期（build.prefixModuleStatements Phase 4）只改寫 AST 上的型別節點，
	// 解析期寫入語義副表的型別字串仍是模組內的裸名。多個 std 模組定義同名型別時
	// （net/server.no 與 net/tls.no 都有 server-conn），裸名在 HIR 解析不到帶前綴
	// 的方法（`unknown callee server-conn.handshake`）。AST 註解已帶模組限定，
	// 據此升級副表型別。
	if ident, ok := matched.(*Identifier); ok && bareModuleType(matchedVarType) {
		if at := p.astLocalVarType(p.curFuncName, ident.Value); typeQualifiedOver(at, matchedVarType) {
			matchedVarType = at
		}
	}

	// 主語是呼叫表達式時的型別推斷。option 回傳（`?T`）的情形已被上方的
	// `__match_subj` 歸一化接管——matched 那時已換成識別符，走第一條路徑。
	// 此處只剩**非 option** 回傳的兜底：union（`T | err`）主語不歸一化，
	// 但 elemType 仍要能算出來，否則值語境的空臂（`-> 0.0`）取不到零值。
	if matchedVarType == "" {
		if call, ok := matched.(*CallExpression); ok {
			matchedVarType = p.returnTypeFromCallExpr(call)
		}
	}

	// Determine element type from option type for per-arm `it` type inference.
	// For ?i64, elemType = "i64"; for result unions (i64 | err), extract the payload type.
	elemType := ""
	if matchedVarType != "" {
		if ty.IsOptionRaw(matchedVarType) {
			elemType = ty.StripOptionRaw(matchedVarType)
		} else if strings.Contains(matchedVarType, "|") {
			// 聯合型別（如 i64 | err）：提取 payload 型別（i64）
			elemType = unionElemType(matchedVarType)
		} else if isEnumType {
			// For enum match, set elemType to trigger per-arm it binding path
			elemType = matchedVarType
		}
	}
	if elemType == "" && subjElem != "" {
		// 索引主體歸一化（上方 subjName）已確定內部型別時，以其為準。
		elemType = subjElem
	}

	// D4：值-match 的空臂（`->` 後不寫任何東西）= 被匹配 option 內部型別的預設零值。
	// 僅對**值語境**的臂生效（MatchArm.valueCtx，由 validateMatchArmReturns 寫入）；
	// 陳述語境的空臂是 no-op（既有行為，如 `parsed: { ok -> {...}  nil ->  err -> }`）。
	// 值語境但型別推不出來時報錯（不猜）。裸 match（buildBareMatchDesugar）不走本路徑。
	for i := range arms {
		if !arms[i].ValueCtx {
			continue
		}
		if arms[i].Body != nil && len(arms[i].Body.Statements) > 0 {
			continue
		}
		if elemType == "" {
			p.saveErrorf("hcdpwnpp", "line %d, column %d: empty match arm cannot determine a value (matched expression type is unknown)",
				tok.Line, tok.Column)
			continue
		}
		zb := &BlockStatement{
			Token: tok,
			Statements: []Statement{
				&ExpressionStatement{Token: tok, Expression: scalarZeroLiteral(tok, elemType)},
			},
		}
		// 標記合成語句本身（而非 body 區塊）：desugar 可能把 body 重新包成新的
		// BlockStatement，區塊指標不保證存活到 formatter；語句指標則一定存活。
		// Formatter 據此把該語句濾掉、還原成空臂輸出（否則 `->` 會被改寫成 `-> 0`，
		// 破壞 `no fmt` 冪等）。
		p.fmtTrivia.SetRTFlag(zb.Statements[0], RTEmptyArmDefault)
		if arms[i].Body != nil {
			zb.IsInline = arms[i].Body.IsInline
			zb.TrailingComments = arms[i].Body.TrailingComments
			zb.ClosingBraceComment = arms[i].Body.ClosingBraceComment
			zb.RBrace = arms[i].Body.RBrace
		}
		arms[i].Body = zb
	}

	// Shared it binding (used for hasRawCond or fallback)
	itStmt := p.buildItBinding(tok, matched)

	// For hasRawCond, set the element type on the shared binding for LSP inference
	if itStmt != nil && elemType != "" {
		itStmt.Type = &NamedType{Value: elemType}
	}

	// Check if any arm uses ok(cond) — if so, `it` must be bound BEFORE the if-chain
	// (not inside arm bodies) because the condition references `it`.
	hasRawCond := false
	for _, arm := range arms {
		if arm.IsRawCond {
			hasRawCond = true
			break
		}
	}

	// Check which variants are explicitly listed (for computing else arm complement)
	hasExplicitOk, hasExplicitErr, hasExplicitNil := false, false, false
	// For enum types, track which enum variant identifiers are listed
	enumListedVariants := make(map[string]bool)
	matchedIsEnum := isEnumType

	// 標籤列舉析構綁定泛化：`some(v) -> ...`（不限於 ok）。
	// 解析期無法判定（尚不知被匹配變數的靜態型別），故在此依 matchedVarType 決定：
	// 僅當 X 是本枚舉的「帶載荷變體」且括號內為單一裸識別符時，才視為析構綁定——
	// 把 arm.condition 規範化為 Identifier{X}，載荷綁定到 v。其餘 IDENT(expr) 形態
	// （如 ok(it > 127) 這類條件臂）保持原樣，不受影響。
	if matchedIsEnum {
		for i := range arms {
			a := &arms[i]
			if a.BindingName != "" || len(a.BindingNames) > 0 {
				continue
			}
			call, ok := a.Condition.(*CallExpression)
			if !ok || len(call.Arguments) == 0 {
				continue
			}
			fn, ok := call.Function.(*Identifier)
			if !ok {
				continue
			}
			// 括號內必須全為裸識別符（綁定名）。
			names := make([]string, 0, len(call.Arguments))
			allBare := true
			for _, arg := range call.Arguments {
				id, isId := arg.(*Identifier)
				if !isId {
					allBare = false
					break
				}
				names = append(names, id.Value)
			}
			if !allBare {
				continue
			}
			fieldTypes := p.enumVariantFieldTypes(matchedVarType, fn.Value)
			if len(fieldTypes) == 0 || len(fieldTypes) != len(names) {
				continue
			}
			if len(names) == 1 {
				a.BindingName = names[0]
			} else {
				a.BindingNames = names
				a.BindingFields = fieldTypes
			}
			a.BindingVariant = fn.Value
			a.Condition = &Identifier{Token: fn.Token, Value: fn.Value}
		}
	}

	for _, a := range arms {
		if len(a.MultiOptionPatterns) > 0 {
			// Combined option patterns: mark all as explicit
			for _, pat := range a.MultiOptionPatterns {
				if pat == "err" {
					hasExplicitErr = true
				} else if pat == "nil" {
					hasExplicitNil = true
				} else if pat == "ok" {
					hasExplicitOk = true
				}
			}
		} else if a.IsDotVal {
			hasExplicitOk = true
		} else if a.Condition != nil {
			if ident, ok := a.Condition.(*Identifier); ok {
				if ident.Value == "err" {
					hasExplicitErr = true
				} else if ident.Value == "nil" {
					hasExplicitNil = true
				} else if ident.Value == "ok" {
					hasExplicitOk = true
				} else if matchedIsEnum {
					enumListedVariants[ident.Value] = true
				}
			} else if _, ok := a.Condition.(*NilLiteral); ok {
				hasExplicitNil = true
			}
		}
	}

	prep.subjName = subjName
	prep.subjElem = subjElem
	prep.origIdxExpr = origIdxExpr
	prep.idxTempName = idxTempName
	prep.baseLeft = baseLeft
	prep.placeWriteUsed = placeWriteUsed
	prep.anyArmValueCtx = anyArmValueCtx
	prep.matchedVarType = matchedVarType
	prep.isEnumType = isEnumType
	prep.enumVariants = enumVariants
	prep.elemType = elemType
	prep.itStmt = itStmt
	prep.hasRawCond = hasRawCond
	prep.hasExplicitOk = hasExplicitOk
	prep.hasExplicitErr = hasExplicitErr
	prep.hasExplicitNil = hasExplicitNil
	prep.enumListedVariants = enumListedVariants
	prep.matchedIsEnum = matchedIsEnum
	return prep
}

// emitSubjectMatchAST builds the IfExpression chain from a resolved prep. This is
// the legacy route (and what the no-flip control path keeps using); the streaming
// HIR emitter is the S-c counterpart.

// rewriteSubjectArm is the per-arm body rewrite half of the D segment (§5.2.9):
// it computes the arm's variant type, injects the `it` (or `v`) binding, applies
// the D9 place-write rewrite, and fills in the value-context zero default. It
// mutates arm.Body in place and registers names in the semantic side tables.
//
// It is shared by BOTH emitters (emitSubjectMatchAST and emitSubjectMatchHIR) so
// the two routes cannot disagree about what an arm's body looks like. It has no
// cross-arm dependency: armType, the injected statements and the side-table
// registrations are all per-arm, and the only shared variable it touches is
// prep.placeWriteUsed, which is a monotonic OR-accumulation. That is what lets
// the emitters run this as a separate pass over the arms before assembling the
// chain.
func (p *Parser) rewriteSubjectArm(prep *subjectMatchPrep, arm *MatchArm) {
	// Guard from the original in-loop code (see the comment block below on why
	// the per-arm path is skipped when the shared binding is absent or an
	// ok(cond) arm will bind `it` before the chain).
	if prep.itStmt == nil || prep.hasRawCond {
		return
	}
	tok := prep.tok
	matched := prep.matched
	itStmt := prep.itStmt
	matchedIsEnum := prep.matchedIsEnum
	matchedVarType := prep.matchedVarType
	enumVariants := prep.enumVariants
	enumListedVariants := prep.enumListedVariants
	hasExplicitOk := prep.hasExplicitOk
	hasExplicitErr := prep.hasExplicitErr
	hasExplicitNil := prep.hasExplicitNil
	elemType := prep.elemType
	isEnumType := prep.isEnumType
	idxTempName := prep.idxTempName
	anyArmValueCtx := prep.anyArmValueCtx
	origMatched := prep.origMatched
	baseLeft := prep.baseLeft
	placeWriteUsed := prep.placeWriteUsed

	var armType string
	// 標籤列舉帶載荷的變體臂：armType/綁定型別取「載荷型別」而非變體名，
	// 使 `some(v) ->` 的 v 與隱含 `it` 得到正確型別（如 i64/f64/str）。
	armBindingType := ""
	skipItBinding := false
	if arm.SkipItBinding {
		// Generated sentinel arms (e.g. ?= / #{index-out} err/nil arms)
		// never reference `it`; opt out so a sentinel arm's %str-long/nil
		// `it` type can't clobber the ok arm's element-type `it`.
		skipItBinding = true
	}
	if len(arm.MultiOptionPatterns) > 0 {
		// Combined option patterns: compute armType from pattern set
		hasOk, hasErr, hasNil := false, false, false
		for _, pat := range arm.MultiOptionPatterns {
			switch pat {
			case "ok":
				hasOk = true
			case "err":
				hasErr = true
			case "nil":
				hasNil = true
			}
		}
		if hasOk && hasErr && hasNil {
			armType = "ok_err_nil"
		} else if hasOk && hasErr {
			armType = "ok_err"
		} else if hasOk && hasNil {
			armType = "ok_nil"
		} else if hasErr && hasNil {
			armType = "else" // err | nil
		} else if hasOk {
			armType = "ok"
		} else if hasErr {
			armType = "err"
		} else if hasNil {
			armType = "nil"
		}
	} else if arm.IsWildcard {
		if arm.IsDotVal {
			armType = "ok" // ok-> is explicit ok case
			// 標籤列舉：`ok(v) ->` 這類走 dotVal 路徑的析構綁定，其綁定型別
			// 同樣取載荷型別（由 bindingVariant 在本枚舉內解析）。
			if matchedIsEnum && arm.BindingVariant != "" {
				if pt := p.enumVariantPayload(matchedVarType, arm.BindingVariant); pt != "" {
					armType = pt
					armBindingType = pt
				}
			}
		} else {
			// Compute complement: which variants remain for -> else arm
			if matchedIsEnum {
				// For enum types, compute complement dynamically
				var remaining []string
				for _, v := range enumVariants {
					if !enumListedVariants[v] {
						remaining = append(remaining, v)
					}
				}
				if len(remaining) == 0 {
					// All enum variants listed — the else arm is dead code
					//
					// 診斷已搬到解析期（parser/match_warn.go 的
					// warnUnreachableArms）：這條提示原本在此發出，等於
					// 它是「降糖的副產物」——SkipLowering 下整段不執行，
					// 消費方翻到糖形態就靜默丟掉它（設計稿 §17.1）。
					skipItBinding = true
				} else {
					armType = strings.Join(remaining, " | ")
				}
			} else {
				// Three variants: ok(elemType), err, nil
				okListed, errListed, nilListed := hasExplicitOk, hasExplicitErr, hasExplicitNil
				if okListed && errListed && nilListed {
					// All three option variants listed — the else arm is
					// dead code. 診斷同列舉分支：已搬到解析期
					// （parser/match_warn.go），這裡只保留結構性的
					// skipItBinding。
					skipItBinding = true
				} else if okListed && !errListed && nilListed {
					armType = "err" // only err remains
				} else if okListed && errListed && !nilListed {
					armType = "nil" // only nil remains
				} else if okListed && !errListed && !nilListed {
					armType = "else" // err | nil
				} else if !okListed && errListed && nilListed {
					armType = "ok" // only i64 remains
				} else if !okListed && !errListed && nilListed {
					armType = "ok_err" // i64 | err
				} else if !okListed && errListed && !nilListed {
					armType = "ok_nil" // i64 | nil
				} else if !okListed && !errListed && !nilListed {
					armType = "ok_err_nil" // i64 | err | nil
				}
			}
		}
	} else if ident, ok := arm.Condition.(*Identifier); ok {
		if ident.Value == "err" || ident.Value == "nil" {
			armType = ident.Value
		} else if ident.Value == "ok" {
			armType = "ok"
		} else if matchedIsEnum {
			armType = ident.Value // Use variant name as arm type for it binding
			// 帶載荷變體（some(v) ->）：綁定型別 = 載荷型別。
			// 由被匹配變數的靜態型別（matchedVarType）在解析期補全查得。
			if pt := p.enumVariantPayload(matchedVarType, ident.Value); pt != "" {
				armType = pt
				armBindingType = pt
			}
		}
	} else if _, ok := arm.Condition.(*NilLiteral); ok {
		armType = "nil"
	} else if arm.IsDotVal {
		// ok-> arm (dotVal wildcard): it should be the unwrapped elemType
		armType = "ok"
	}
	// D9 place-write：statement 語境 + 安全索引主體 + ok 路徑臂 ⇒ 臂內使用者
	// 寫的 `it = v`（let / assign 形態）改寫為「求值一次 → 寫回元素 → 重綁 it」。
	// 值語境（anyArmValueCtx）的 match 保持讀語義：it 是載荷副本，`it = v` 只
	// 重綁局部，與今天一致。nil/err 臂沒有元素位置可寫，不改寫。
	// SkipSafeIndexLowering 旗標已隨 §4.3 刪除：編譯期一律做 place-write 寫回；
	// 糖形態（SkipLowering=true）不走此路徑，formatter 直接渲染 surface 語法。
	if idxTempName != "" && !anyArmValueCtx && isOKPathArmType(armType) {
		writeInBounds := false
		if idx, ok := origMatched.(*IndexExpression); ok {
			writeInBounds = p.sem.IndexIsInBounds(idx)
		}
		arm.Body = p.rewriteItPlaceWrites(arm.Body, tok, baseLeft, idxTempName, elemType, &placeWriteUsed, writeInBounds)
	}
	if armType != "" {
		// 析構綁定 `ok(v) -> ...`：把載荷綁到 v 而非隱含的 `it`。
		// 嵌套 match 時這能保住外層的 `it`（否則內層會把它覆蓋掉）。
		bindName := arm.BindingName
		if bindName == "" {
			bindName = "it"
		}
		// Use per-arm position so walker/index can distinguish synthetic bindings
		var armTok lexer.Token
		if arm.Condition != nil {
			pos := arm.Condition.Pos()
			armTok = lexer.Token{Type: lexer.IDENT, Literal: bindName, Line: pos.Line, Column: pos.Column}
		} else if len(arm.Body.Statements) > 0 {
			pos := arm.Body.Statements[0].Pos()
			armTok = lexer.Token{Type: lexer.IDENT, Literal: bindName, Line: pos.Line, Column: pos.Column}
		} else {
			armTok = tok
		}
		if arm.BindingName != "" {
			// 讓 checker / codegen 知道 v 的型別（等同 `it` 在 ok 臂的 elemType）。
			// DeclaredVars 必須註冊，否則 ValidateUndefinedVars 會報
			// "'v' is not defined"（`it` 是 checker 裡寫死的關鍵字，自訂名沒有這待遇）。
			if p.sem.DeclaredVars == nil {
				p.sem.DeclaredVars = make(map[string]bool)
			}
			p.sem.DeclaredVars[arm.BindingName] = true
			bt := elemType
			if armBindingType != "" {
				bt = armBindingType
			} else if armType == "err" && !matchedIsEnum {
				// 內建 option 的 err 載荷恆為 str
				//（option { ok(v t), nil, err(e str) }），與 codegen 端
				// hir2mir 的 err-marker 剝皮語義一致；不可誤用 ok 載荷型別。
				bt = "str"
			}
			if bt != "" {
				p.sem.SetFuncVarType(p.curFuncName, arm.BindingName, bt)
				p.setVarType(arm.BindingName, bt)
			}
		}
		if armIt := p.buildItBindingForArm(armTok, matched, armType, elemType, matchedVarType, isEnumType, bindName); armIt != nil {
			// Set the synthetic end position to cover the arm body
			bodyEnd := arm.Body.EndPos()
			if bodyEnd.Line == 0 && bodyEnd.Column == 0 && len(arm.Body.Statements) > 0 {
				bodyEnd = arm.Body.Statements[len(arm.Body.Statements)-1].EndPos()
			}
			armIt.SyntheticEnd = bodyEnd
			// 多欄位析構綁定（`rect(w, h) -> ...`）：按位置把 it.f0 / it.f1
			// 綁到各欄位名。逆序 prepend，使最終語句順序為 `it; w; h; body`。
			if len(arm.BindingNames) > 0 {
				for j := len(arm.BindingNames) - 1; j >= 0; j-- {
					fname := arm.BindingNames[j]
					ftype := ""
					if j < len(arm.BindingFields) {
						ftype = arm.BindingFields[j]
					}
					if p.sem.DeclaredVars == nil {
						p.sem.DeclaredVars = make(map[string]bool)
					}
					p.sem.DeclaredVars[fname] = true
					if ftype != "" {
						p.sem.SetFuncVarType(p.curFuncName, fname, ftype)
						p.setVarType(fname, ftype)
					}
					extract := &LetStatement{
						Token: tok,
						Name:  &Identifier{Token: tok, Value: fname},
						Value: &DotExpression{
							Token:    tok,
							Receiver: &Identifier{Token: tok, Value: bindName},
							Property: fmt.Sprintf("f%d", j),
						},
						IsSynthetic:  true,
						IsArmBinding: true,
					}
					arm.Body = p.prependStmt(arm.Body, extract)
				}
			}
			arm.Body = p.prependStmt(arm.Body, armIt)
		} else if !skipItBinding && !isSentinelArmType(armType) {
			// ok/else arm whose matched type is unknown at parse time:
			// fall back to the shared `it` binding (best-effort). Safe,
			// because `it` is only meaningful in the ok arm where the
			// option is non-nil, so the struct-deref is guarded by the
			// live variant. Sentinel (nil/err) arms must NOT receive any
			// `it` binding here — doing so would emit an unconditional
			// deref of the option's data field (null when nil/err).
			arm.Body = p.prependStmt(arm.Body, p.namedItBinding(itStmt, bindName))
		}
		// Sentinel arms with unknown matched type: intentionally bind nothing.
	} else if !skipItBinding && !arm.IsWildcard {
		// Non-wildcard arm: safe to bind shared `it`.
		// Wildcard arms (->) with unknown matched type must NOT receive
		// shared `it` binding: when the matched var is an option and the
		// arm is the else/catch-all, the option may be nil (data=0),
		// and the shared binding unconditionally dereferences the data
		// field, causing null pointer dereference / segfault.
		arm.Body = p.prependStmt(arm.Body, itStmt)
	}
	prep.placeWriteUsed = placeWriteUsed
}

// armCondPlan is the outcome of resolveArmCond: the if-condition plus the six
// match-specific operands case *IfExpression turns into KIf slots. Keeping them
// in one struct is what lets the AST emitter (which fills IfExpression fields)
// and the HIR emitter (which fills KIf slots directly) share the classification
// instead of drifting on which pattern kind an arm is.
type armCondPlan struct {
	cond            Expression
	rangePattern    *RangeExpression
	equalityPattern Expression
	optionPatterns  []string
	valuePatterns   []Expression
	rawCond         Expression
}

// resolveArmCond classifies a NON-wildcard arm's condition (§5.2.9, slice S-c1's
// shared core). Six mutually exclusive shapes, in the original order:
//
//	MultiOptionPatterns  -> (matched == p1) || (matched == p2) || ...
//	MultiValuePatterns   -> (matched == v1) || (matched == v2) || ...
//	IsRawCond            -> (matched == ok) && <arm cond>
//	RangeExpression      -> matched >= a && matched <= b (desugarRangeCondition)
//	otherwise            -> matched == <arm cond>
//
// The condition tree is built left-folded, so the FIRST pattern is the outermost
// left operand and later patterns nest on the right; both emitters depend on that
// shape, and it is why this is one function rather than two.
func (p *Parser) resolveArmCond(tok lexer.Token, matched Expression, arm MatchArm) armCondPlan {
	var plan armCondPlan
	var cond Expression
	var rangePattern *RangeExpression
	var equalityPattern Expression
	var optionPatterns []string
	var valuePatterns []Expression
	var rawCond Expression
	if len(arm.MultiOptionPatterns) > 0 {
		// Combined option patterns: (matched == p1) || (matched == p2) || ...
		optionPatterns = arm.MultiOptionPatterns
		for i, pat := range arm.MultiOptionPatterns {
			patCond := &InfixExpression{
				Token:    tok,
				Left:     matched,
				Operator: "==",
				Right:    &Identifier{Token: tok, Value: pat},
			}
			if i == 0 {
				cond = patCond
			} else {
				cond = &InfixExpression{
					Token:    tok,
					Left:     cond,
					Operator: "||",
					Right:    patCond,
				}
			}
		}
	} else if len(arm.MultiValuePatterns) > 0 {
		// Combined value patterns: 1 || 3 || 5 → (matched == 1) || (matched == 3) || (matched == 5)
		valuePatterns = arm.MultiValuePatterns
		for i, pat := range arm.MultiValuePatterns {
			patCond := &InfixExpression{
				Token:    tok,
				Left:     matched,
				Operator: "==",
				Right:    pat,
			}
			if i == 0 {
				cond = patCond
			} else {
				cond = &InfixExpression{
					Token:    tok,
					Left:     cond,
					Operator: "||",
					Right:    patCond,
				}
			}
		}
	} else if arm.IsRawCond {
		// ok(cond) arm: condition is (matched == ok) && cond
		rawCond = arm.Condition
		cond = &InfixExpression{
			Token: tok,
			Left: &InfixExpression{
				Token:    tok,
				Left:     matched,
				Operator: "==",
				Right:    &Identifier{Token: tok, Value: "ok"},
			},
			Operator: "&&",
			Right:    arm.Condition,
		}
	} else if rng, isRange := arm.Condition.(*RangeExpression); isRange {
		// Range condition: [a..b] → matched >= a && matched <= b
		rangePattern = rng
		cond = p.desugarRangeCondition(tok, matched, rng)
	} else {
		// matched == condition
		// 對 option 變數，condition 為 err/nil 時由 transpiler 生成 tag 比較
		equalityPattern = arm.Condition
		cond = &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: "==",
			Right:    arm.Condition,
		}
	}
	plan.cond = cond
	plan.rangePattern = rangePattern
	plan.equalityPattern = equalityPattern
	plan.optionPatterns = optionPatterns
	plan.valuePatterns = valuePatterns
	plan.rawCond = rawCond
	return plan
}

// emitSubjectMatchPlain is the js/wasm re-home counterpart of emitSubjectMatchAST
// (design doc §5 P7). It builds the SAME if/elif/else chain — identical
// Condition / Consequence / Alternative nesting and the identical `if 1 { ... }`
// wrappers for the ok(cond) and index-subject-normalisation cases — but writes
// NONE of the match-specific IfExpression fields (MatchedExpr / RangePattern /
// EqualityPattern / OptionPatterns / ValuePatterns / RawCond / DotValBody) and no
// RTFlag. The js/wasm backends consume only the if-chain shape, so the emitted
// JS/WASM is byte-identical to the legacy route while keeping js/wasm off
// buildMatchDesugar's dependency chain.
func (p *Parser) emitSubjectMatchPlain(prep *subjectMatchPrep) Expression {
	tok := prep.tok
	matched := prep.matched
	arms := prep.arms
	origMatched := prep.origMatched
	subjName := prep.subjName
	subjElem := prep.subjElem
	origIdxExpr := prep.origIdxExpr
	idxTempName := prep.idxTempName
	placeWriteUsed := prep.placeWriteUsed
	itStmt := prep.itStmt
	hasRawCond := prep.hasRawCond

	var ifExpr *IfExpression
	var defaultBody *BlockStatement
	var defaultDotValBody *BlockStatement

	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]

		p.rewriteSubjectArm(prep, &arm)

		if arm.IsWildcard {
			if ifExpr == nil {
				if defaultBody != nil && arm.IsDotVal {
					cond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					}
					newIf := &IfExpression{
						Token:       tok,
						Condition:   cond,
						Consequence: arm.Body,
						Alternative: defaultBody,
					}
					ifExpr = newIf
				} else {
					defaultBody = arm.Body
					if arm.IsDotVal {
						defaultDotValBody = arm.Body
					}
				}
			} else {
				if arm.IsDotVal {
					cond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					}
					newIf := &IfExpression{
						Token:       tok,
						Condition:   cond,
						Consequence: arm.Body,
						Alternative: &BlockStatement{
							Token:      tok,
							Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
						},
					}
					ifExpr = newIf
				} else {
					ifExpr.Alternative = arm.Body
				}
			}
		} else {
			plan := p.resolveArmCond(tok, matched, arm)
			newIf := &IfExpression{
				Token:       tok,
				Condition:   plan.cond,
				Consequence: arm.Body,
				Alternative: nil,
			}
			if ifExpr != nil {
				newIf.Alternative = &BlockStatement{
					Token:      tok,
					Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
				}
			} else if defaultBody != nil {
				newIf.Alternative = defaultBody
			}
			ifExpr = newIf
		}
	}

	if ifExpr == nil {
		if defaultBody != nil {
			if defaultDotValBody == defaultBody {
				ifExpr = &IfExpression{
					Token: tok,
					Condition: &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					},
					Consequence: defaultBody,
				}
			} else {
				ifExpr = &IfExpression{
					Token:       tok,
					Condition:   &IntegerLiteral{Token: tok, Value: 1},
					Consequence: defaultBody,
				}
			}
		} else {
			return nil
		}
	}

	// ok(cond): `if 1 { it = matched; <chain> }` — must mirror emitSubjectMatchAST.
	//
	// placeWriteUsed 的取值時點在這裡很關鍵：它在上面那圈 rewriteSubjectArm 裡才被
	// 累積出來（rewriteItPlaceWrites 透過 *bool 回報），函式開頭抓到的是**進入時**
	// 的值。早期版本整段直接用開頭那個 snapshot，於是「臂裡確實有 `it = v`」的
	// match 仍然拿不到 `let __idx = <orig>`、讀取也不改寫成 `__idx`，而寫回 store
	// 卻照樣用 `__idx_L_C` —— 產生一個**從未宣告的索引暫存**，MIR 靜默讀到 0，
	// 用戶可見症狀就是 `?=` 寫入整批消失（`?= ` 由 lowerUnwrapAssignToIndex 直接
	// 呼叫本函式，必經此路徑；用戶手寫的 `ok -> it = v` 走 walk 版 buildMatchDesugar，
	// 那邊每個臂都即時更新，故只有 `?=` 形態壞掉）。
	// H0（tohir.go 的 emitSubjectMatchHIR）本來就讀 prep.placeWriteUsed 的即時值，
	// 兩條路徑必須一致。
	placeWriteUsed = prep.placeWriteUsed
	if hasRawCond && itStmt != nil {
		ifExpr = &IfExpression{
			Token:     tok,
			Condition: &IntegerLiteral{Token: tok, Value: 1},
			Consequence: &BlockStatement{
				Token: tok,
				Statements: []Statement{
					itStmt,
					&ExpressionStatement{Token: tok, Expression: ifExpr},
				},
			},
		}
	}

	// Index-subject normalisation: `if 1 { [let __idx = <orig>;] let __match_subj ?elem = <orig>; <chain> }`.
	if subjName != "" && subjElem != "" && ifExpr != nil {
		var preStmts []Statement
		subjValue := origMatched
		if placeWriteUsed && origIdxExpr != nil {
			preStmts = append(preStmts, &LetStatement{
				Token:       tok,
				Name:        &Identifier{Token: tok, Value: idxTempName},
				IsSynthetic: true,
				Value:       origIdxExpr,
			})
			// 讀取索引改寫成 __idx 暫存 —— **建新節點，絕不就地改寫
			// origMatched**（與 tohir.go 的 emitSubjectMatchHIR 同口徑）。
			//
			// 曾經這裡寫的是 `idx.Index = &Identifier{…idxTempName}`，直接改掉
			// 用戶 AST 上那顆 *IndexExpression。同一份 AST 只要被降糖兩趟
			//（`?=` 的目標索引節點在 block-match 展開與 H0 直出之間是共享的），
			// 第二趟 prepareSurfaceMatch 取到的 origIdxExpr 就已經是 `__idx_L_C`，
			// 於是宣告變成自我引用的 `let __idx_L_C = __idx_L_C`：讀取那個從未
			// 初始化的暫存、寫回也落在同一個 0 索引 ⇒ `a[i] ?= v` 靜默無效
			//（x8 探針：期望 99，實際 0；HIR dump 可見 `let s=__idx_4_14` 的初值
			// 是 `ident s=__idx_4_14`）。
			if idx, ok := origMatched.(*IndexExpression); ok {
				fresh := &IndexExpression{
					Token: idx.Token,
					Left:  idx.Left,
					Index: &Identifier{Token: tok, Value: idxTempName},
				}
				// 借用原節點（bounds 分析已證明）的 InBounds 事實，並標成寫入
				// 主體：可增長容器要走 grow-write 下標（`i >= 0` 即擴容），而非
				// 讀語義的 `0 <= i < len`。與 emitSubjectMatchHIR 逐條對應，
				// 少了它兩軌在 MIR 上分叉（mir/d9_write_bce_test.go 會紅）。
				if p.sem != nil {
					if p.sem.IndexIsInBounds(origMatched) {
						p.sem.SetIndexInBounds(fresh, true)
					}
					p.sem.SetGrowWrite(fresh, true)
				}
				subjValue = fresh
			}
		}
		// The type NODE must be the same string the semantic table was given
		// above — see subjTypeRaw for why `[]?T` must not be wrapped twice.
		subjAssign := &LetStatement{
			Token:       tok,
			Name:        &Identifier{Token: tok, Value: subjName},
			IsSynthetic: true,
			Type:        buildType(prep.subjTypeRaw(), tok),
			Value:       subjValue,
		}
		wrapperStmts := make([]Statement, 0, len(preStmts)+2)
		wrapperStmts = append(wrapperStmts, preStmts...)
		wrapperStmts = append(wrapperStmts, subjAssign, &ExpressionStatement{Token: tok, Expression: ifExpr})
		ifExpr = &IfExpression{
			Token:     tok,
			Condition: &IntegerLiteral{Token: tok, Value: 1},
			Consequence: &BlockStatement{
				Token:      tok,
				Statements: wrapperStmts,
			},
		}
	}

	return ifExpr
}

// resolveSubjectMatchPlan assembles the if/elif/else chain of a subject match as
// a matchIfPlan tree — the plan counterpart of emitSubjectMatchAST's D segment
// plus its all-wildcard tail.
//
// ⚠️ Why a tree and not a direct mirror: D's plain-wildcard branch executes
// `ifExpr.Alternative = arm.Body`, i.e. it patches the ELSE SLOT of a node that
// was already built. On the HIR side Builder.Add copies its argument and
// b.nodes is unreachable from this package (AppendChild can only append at the
// END of the child chain, while `else` is KIf's third child), so an emitted KIf
// can never gain that slot. Building the tree first and emitting it afterwards
// is the only way to keep the slot order identical.
//
// Every decision here is shared with the AST route: the per-arm body rewrites go
// through rewriteSubjectArm and the condition classification through
// resolveArmCond, so the two routes cannot disagree about an arm's shape.
func (p *Parser) resolveSubjectMatchPlan(prep *subjectMatchPrep) *matchIfPlan {
	tok, matched, arms := prep.tok, prep.matched, prep.arms

	// Chain state, exactly as in emitSubjectMatchAST.
	var head *matchIfPlan
	var defaultBody *BlockStatement       // 最內層 wildcard body（直接作為 else，避免 if 1 {} 包裝）
	var defaultDotValBody *BlockStatement // track val branch body separately

	okCond := func() Expression {
		return &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: "==",
			Right:    &Identifier{Token: tok, Value: "ok"},
		}
	}
	okIdent := func() Expression { return &Identifier{Token: tok, Value: "ok"} }

	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]
		p.rewriteSubjectArm(prep, &arm)
		if arm.IsWildcard {
			if head == nil {
				if defaultBody != nil && arm.IsDotVal {
					// dotVal arm (ok->) after a regular -> wildcard (else):
					// treat as conditional arm: if matched == ok { body } else { defaultBody }
					head = &matchIfPlan{
						cond:       okCond(),
						body:       arm.Body,
						altBody:    defaultBody,
						matched:    matched,
						eqPattern:  okIdent(),
						dotValBody: arm.Body,
						rtFlags:    RTBareMatch | RTMatchWildcard,
					}
				} else {
					// 最內層 wildcard：儲存 body 作為下一個條件 arm 的 else
					defaultBody = arm.Body
					if arm.IsDotVal {
						defaultDotValBody = arm.Body
					}
				}
			} else {
				if arm.IsDotVal {
					// ok-> when other arms already processed (e.g., {ok ->, nil ->, ->}):
					// wrap as outer condition instead of overwriting the alternative
					head = &matchIfPlan{
						cond:       okCond(),
						body:       arm.Body,
						alt:        head,
						matched:    matched,
						eqPattern:  okIdent(),
						dotValBody: arm.Body,
						rtFlags:    RTBareMatch | RTMatchWildcard,
					}
				} else {
					// The late `ifExpr.Alternative = arm.Body` from the AST route.
					// On a plan tree this is a plain overwrite — and it is a real
					// overwrite: an earlier wildcard's body is discarded, which is
					// the behaviour the AST route has too.
					head.altBody = arm.Body
				}
			}
		} else {
			acp := p.resolveArmCond(tok, matched, arm)
			n := &matchIfPlan{
				cond:           acp.cond,
				body:           arm.Body,
				matched:        matched,
				rangePattern:   acp.rangePattern,
				eqPattern:      acp.equalityPattern,
				optionPatterns: acp.optionPatterns,
				valuePatterns:  acp.valuePatterns,
				rawCond:        acp.rawCond,
				rtFlags:        RTBareMatch,
			}
			if head != nil {
				n.alt = head
			} else if defaultBody != nil {
				// 直接使用 wildcard body 作為 else，避免 if 1 {} 包裝
				n.altBody = defaultBody
				if defaultDotValBody == defaultBody {
					n.dotValBody = defaultBody
				}
			}
			head = n
		}
	}

	// 若所有 arm 都是 wildcard，或只有 wildcard 而無條件 arm
	if head == nil {
		if defaultBody == nil {
			return nil
		}
		// 唯一 arm 是 wildcard。對 ok-> (dotVal) arm 生成 `matched == ok` 條件而非
		// 硬編碼 true——否則 option 為 nil 時仍進入 ok 分支，解引用空指針導致段錯誤。
		if defaultDotValBody == defaultBody {
			head = &matchIfPlan{
				cond:       okCond(),
				body:       defaultBody,
				matched:    matched,
				eqPattern:  okIdent(),
				dotValBody: defaultBody,
				rtFlags:    RTBareMatch | RTMatchWildcard,
			}
		} else {
			// 非 dotVal wildcard：用 if 1 {} 包裝（無法避免）
			head = &matchIfPlan{
				cond:    &IntegerLiteral{Token: tok, Value: 1},
				body:    defaultBody,
				matched: matched,
				rtFlags: RTBareMatch | RTMatchWildcard,
			}
		}
	}
	return head
}

// subjectMatchDirectHIR reports whether this subject match can be streamed to HIR
// by emitSubjectMatchHIR. It is the S-c coverage gate: false sends the match
// down the legacy two-hop route, so widening coverage can never change the
// behaviour of a form this slice does not emit yet.
//
// Allowed forms (S-c1 + S-c2 + S-c3 + index-subject normalisation, all landed
// and verified by the equivalence gate):
//   - plain-identifier subject (the S-c1 case);
//   - ok(cond) matches (hasRawCond): emitSubjectMatchWrappersHIR wraps the chain
//     in `if 1 { it = matched; <chain> }`, with no index-subject interaction, so
//     the InBounds side-table proof is unaffected;
//   - matchedIsEnum (enum-complement / payload-destructuring matches): the per-arm
//     binding type and the wildcard-else complement are computed in the SHARED
//     rewriteSubjectArm + resolveArmCond, which both emitters call, so the HIR
//     chain is byte-identical to the legacy IfExpression chain;
//   - subjName != "" (index-subject normalisation, D9 place-write): the H0 direct
//     path lowers a *shallow* clone of the SurfaceMatch (prepareSurfaceMatch), so
//     prep.origMatched is the SAME *IndexExpression the bounds analyzer proved
//     in-bounds. emitSubjectMatchWrappersHIR rewrites the read to a fresh
//     `base[__idx]` node and transfers the InBounds proof onto it (c.sem ==
//     prog.Sem == p.sem), so lowerSafeIndex keeps the provable-in-bounds fast
//     path and mir/d9_write_bce_test.go stays green.
//
// The gate is now fully open: every subject match that reaches here (type-
// destructuring and bare matches are handled earlier by S-a / S-b and never reach
// this point) is streamed to HIR. The equivalence gate remains the byte-equality
// backstop.
func (p *Parser) subjectMatchDirectHIR(prep *subjectMatchPrep) bool {
	if prep == nil {
		return false
	}
	return true
}

// desugarRangeCondition 將 match arm 中的 RangeExpression 條件 desugar 為布爾表達式。
// 有界（4 種）：
//
//	[a..b] → matched >= a && matched <= b
//	[a..b) → matched >= a && matched < b
//	(a..b] → matched > a && matched <= b
//	(a..b) → matched > a && matched < b
//
// 無上限（4 種，End=nil）：
//
//	[a..)  → matched >= a
//	[a..]  → matched >= a
//	(a..)  → matched > a
//	(a..]  → matched > a
//
// 無下限（4 種，Start=nil）：
//
//	[..b]  → matched <= b
//	[..b)  → matched < b
//	(..b]  → matched <= b
//	(..b)  → matched < b
//
// 完全無界（4 種，Start=nil 且 End=nil）：
//
//	[..]   → true (匹配所有值)
//	[..)   → true
//	(..]   → true
//	(..)   → true
func (p *Parser) desugarRangeCondition(tok lexer.Token, matched Expression, rng *RangeExpression) Expression {
	var leftOp string
	if rng.LeftInc {
		leftOp = ">="
	} else {
		leftOp = ">"
	}
	var rightOp string
	if rng.RightInc {
		rightOp = "<="
	} else {
		rightOp = "<"
	}

	if rng.Start != nil && rng.End != nil {
		leftCond := &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: leftOp,
			Right:    rng.Start,
		}
		rightCond := &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: rightOp,
			Right:    rng.End,
		}
		return &InfixExpression{
			Token:    tok,
			Left:     leftCond,
			Operator: "&&",
			Right:    rightCond,
		}
	}
	if rng.Start != nil {
		return &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: leftOp,
			Right:    rng.Start,
		}
	}
	if rng.End != nil {
		return &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: rightOp,
			Right:    rng.End,
		}
	}
	// 完全無界 [..], [..), (..], (..) — 永真，匹配所有值
	return &IntegerLiteral{Token: tok, Value: 1}
}

// bareModuleType reports whether t is an option of a bare (unqualified)
// non-builtin type name — the only shape a module-prefix rewrite can upgrade.
// Gate for the AST fallback below, so the FuncVarTypes hit path stays O(1) for
// the common `?i64` / `?str` cases.
func bareModuleType(t string) bool {
	if !ty.IsOptionRaw(t) {
		return false
	}
	inner := ty.StripOptionRaw(t)
	return inner != "" && !strings.Contains(inner, ".") && !isBuiltinTypeName(inner)
}

// typeQualifiedOver reports whether option type a is the module-qualified form
// of option type b (e.g. "?tls.server-conn" over "?server-conn"), i.e. the same
// payload type with a module prefix that b lacks. Used to prefer the
// post-merge AST annotation over the parse-time semantic side table.
func typeQualifiedOver(a, b string) bool {
	if a == b || !ty.IsOptionRaw(a) || !ty.IsOptionRaw(b) {
		return false
	}
	ai, bi := ty.StripOptionRaw(a), ty.StripOptionRaw(b)
	if ai == bi || bi == "" {
		return false
	}
	return strings.HasSuffix(ai, "."+bi)
}

// buildItBinding creates `it = matched` LetStatement when matched is an Identifier.
// Returns nil if matched is not an Identifier.
//
// When the variable has a known parse-time type (in varDeclTypes), the binding
// is created normally. When the type is unknown at parse time (e.g. the variable
// was assigned from a generic method call whose return type is only resolved
// after monomorphization, like v = m.get('a') where get returns ?v), a fallback
// binding with Type = nil is created. The codegen determines the type from
// g.varTypes at generation time.
func (p *Parser) buildItBinding(tok lexer.Token, matched Expression) *LetStatement {
	// 識別符與索引表達式（如 `parts[i]`）都可作為 option-match 主體：索引主體
	// 回傳的選項 rvalue 需綁定到 `it`，後續臂體 `s = it` 才能取到 payload。
	switch matched.(type) {
	case *Identifier, *IndexExpression:
	default:
		return nil
	}
	// Create the binding regardless of whether the type is known at parse time.
	// When varDeclTypes doesn't have the variable, Type is left nil so codegen
	// can infer it from g.varTypes (e.g. %option for option-returning calls).
	return &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: "it"},
		Value:       matched,
		IsSynthetic: true,
	}
}

// isOKPathArmType reports whether an arm's computed type includes the ok
// variant — i.e. the arm body executes on the ok path where the element place
// exists. D9 place-write only rewrites `it = v` in such arms; nil/err arms
// have no element to write through.
func isOKPathArmType(t string) bool {
	switch t {
	case "ok", "ok_err", "ok_nil", "ok_err_nil":
		return true
	}
	return false
}

// itAssignParts recognises a user-written `it = v` binding in a match arm
// body. Two surface shapes reach lowering: a LetStatement (the common arm
// form — `it = 2` parses as a redeclaration) and an AssignExpression wrapped
// in an ExpressionStatement (when `it` is already declared in an enclosing
// scope). Compiler-generated bindings (IsSynthetic) are never rewritten.
func itAssignParts(s Statement) (Expression, bool) {
	switch n := s.(type) {
	case *LetStatement:
		if n.IsSynthetic || n.Name == nil || n.Name.Value != "it" || n.Value == nil {
			return nil, false
		}
		return n.Value, true
	case *ExpressionStatement:
		if as, ok := n.Expression.(*AssignExpression); ok && !as.IsSynthetic {
			if id, ok := as.Left.(*Identifier); ok && id.Value == "it" && as.Value != nil {
				return as.Value, true
			}
		}
	}
	return nil, false
}

// cloneIndexBase 淺拷貝索引基底的 AST 節點。
//
// place-write 把基底塞進寫回的 IndexExpression（`<base>[__idx] = it`），而原下標
// 節點仍留在樹上供讀取與診斷使用。共享同一指標會讓同一棵子樹被走訪兩次（H0
// 降糖、bounds 分析、fmt 都會踩到），故必須獨立一份。
func cloneIndexBase(e Expression) Expression {
	switch x := e.(type) {
	case *Identifier:
		if x == nil {
			return nil
		}
		c := *x
		return &c
	case *DotExpression:
		if x == nil {
			return nil
		}
		c := *x
		c.Receiver = cloneIndexBase(x.Receiver)
		return &c
	case *GroupedExpression:
		if x == nil {
			return nil
		}
		c := *x
		c.Expression = cloneIndexBase(x.Expression)
		return &c
	}
	return e
}

// rewriteItPlaceWrites rewrites user-written `it = v` bindings inside a
// place-match ok arm into a write-through sequence (D9):
//
//	it = v   ⇒   it = v
//	             a[__idx_L_C] = it
//
// The rebind comes FIRST and writes straight into the arm's own `it`: `it` is
// the element VALUE the rest of the arm reads, and it has to be a plain
// assignment (see the inline comment — a second `let it` shadows the arm's
// binding, and a __place temp is consumed by the store's option wrap when the
// container is a `[]?T`). The index store is marked IsSynthetic so the idxwrite
// validator does not reject the compiler's own output, and both the read
// (__match_subj) and this store index through the shared __idx_L_C temp, so a
// side-effecting index expression is evaluated exactly once.
//
// Nested matches — already lowered into RTBareMatch-flagged IfExpressions by
// the time this runs — are skipped: their `it` belongs to the inner match.
// User-written if/for blocks are descended into.
func (p *Parser) rewriteItPlaceWrites(body *BlockStatement, tok lexer.Token, baseLeft Expression, idxTempName, elemType string, used *bool, writeInBounds bool) *BlockStatement {
	if body == nil || baseLeft == nil || idxTempName == "" {
		return body
	}
	stmts, changed := p.rewriteItPlaceWritesStmts(body.Statements, tok, baseLeft, idxTempName, elemType, used, writeInBounds)
	if !changed {
		return body // nothing rewritten
	}
	nb := &BlockStatement{Token: body.Token, Statements: stmts, IsInline: body.IsInline}
	nb.TrailingComments = body.TrailingComments
	nb.ClosingBraceComment = body.ClosingBraceComment
	nb.RBrace = body.RBrace
	return nb
}

func (p *Parser) rewriteItPlaceWritesStmts(stmts []Statement, tok lexer.Token, baseLeft Expression, idxTempName, elemType string, used *bool, writeInBounds bool) ([]Statement, bool) {
	changed := false
	out := make([]Statement, 0, len(stmts)+2)
	for _, s := range stmts {
		if val, ok := itAssignParts(s); ok {
			// D11：`#{index-out=DEF}` 註解已移除，臂內 RHS 不再做「越界取 DEF」
			// 降糖（desugarIndexOutReads 已刪除）。newVal 直接取原值，後續的
			// __place_val_ 提升修復與寫回保持不變。
			newVal := val
			// D11: `it = <value match>`（例如 `ok -> it = a[9]: { ok(it1) -> it1
			// / -> 0 }`）不能把 SurfaceMatch 直接留在合成 AssignExpression 的
			// RHS 上。H0 在 expression 位置降糖这种值 match 时會產出 cond/then
			// 皆空的退化 KIf，導致整條 `it = …` 指派消失，隨後的寫回 store 仍
			// 讀到 match 之前綁定的原始元素值（用戶可見症狀：寫回靜默失效）。
			//
			// 改成已驗證可用的「提升」形狀：先用合成 LetStatement 求值
			// （`__place_val_L_C = <match>` 與源碼 `t = a[i]: { … }` 同構，
			// H0 會正確展開），再讓 `it` 從該暫存重綁。暫存名帶陳述座標，
			// 同一函式內多處 `it = <match>` 不會撞名；`__` 前綴只禁用於
			// 使用者源碼，編譯器合成名本就沿用 `__idx_` / `__match_subj_`。
			if _, isValueMatch := newVal.(*SurfaceMatch); isValueMatch {
				pos := s.Pos()
				tmpName := fmt.Sprintf("__place_val_%d_%d", pos.Line, pos.Column)
				out = append(out, &LetStatement{
					Token:       tok,
					Name:        &Identifier{Token: tok, Value: tmpName},
					Value:       newVal,
					IsSynthetic: true,
				})
				newVal = &Identifier{Token: tok, Value: tmpName}
			}
			writeIndex := &IndexExpression{
				Token: tok,
				Left:  cloneIndexBase(baseLeft),
				Index: &Identifier{Token: tok, Value: idxTempName},
			}
			if writeInBounds {
				p.sem.SetIndexInBounds(writeIndex, true)
			}
			out = append(out,
				// Rebind `it` FIRST, with a plain assignment — never a second
				// `let it = ...`, and never through a separate __place temp.
				//
				// `let it = ...` declares a NEW local `it` for the whole
				// enclosing block, so every read of `it` in the arm — including
				// the RHS of this very statement — resolved to that fresh,
				// still-unassigned slot. `ok -> it = it + 'x'` therefore read
				// `it` as the empty string (and `it = it + 1` as 0), while a
				// read AFTER the write looked right, which is what hid it. The
				// iteration form (`x <- a: { x: { ok -> it = it + 'x' } }`) never
				// had the bug: it goes through mir/elemplace.go, where the arm's
				// `it` aliases the element place instead of being re-declared.
				//
				// The temp is gone for a reason of its own. The store below has
				// to WRAP the value into the element when the container is a
				// `[]?T` (`a[__idx] = ok(<value>)`), and a wrap TAKES OWNERSHIP
				// of its operand. With `__place = v; a[__idx] = __place;
				// it = __place` the wrap consumed __place and the rebind then
				// read it back after the move — a use-after-free that showed up
				// as `signal: trace/BPT trap` (double free) on
				// `[]?str` + `ok -> it = it + '-'`. Writing through `it`
				// instead means the store reads a local that is still live
				// afterwards, which is the shape the move analysis already
				// handles (it promotes the wrap to a clone).
				&ExpressionStatement{Token: tok, Expression: &AssignExpression{
					Token:       tok,
					Left:        &Identifier{Token: tok, Value: "it"},
					Value:       newVal,
					IsSynthetic: true,
				}},
				&ExpressionStatement{Token: tok, Expression: &AssignExpression{
					Token:       tok,
					Left:        writeIndex,
					Value:       &Identifier{Token: tok, Value: "it"},
					IsSynthetic: true,
				}},
			)
			*used = true
			changed = true
			continue
		}
		// 下降：使用者寫的區塊 / if / for 裡可能也有 `it = v`（指的是本層 it）。
		// 已降糖的巢狀 match（RTBareMatch 旗標）整棵跳過——它的 it 屬於內層。
		switch n := s.(type) {
		case *BlockStatement:
			if ns, ch := p.rewriteItPlaceWritesStmts(n.Statements, tok, baseLeft, idxTempName, elemType, used, writeInBounds); ch {
				n.Statements = ns
				changed = true
			}
		case *ExpressionStatement:
			if ie, ok := n.Expression.(*IfExpression); ok && !p.fmtTrivia.HasRTFlag(ie, RTBareMatch) {
				if ie.Consequence != nil {
					if ns, ch := p.rewriteItPlaceWritesStmts(ie.Consequence.Statements, tok, baseLeft, idxTempName, elemType, used, writeInBounds); ch {
						ie.Consequence.Statements = ns
						changed = true
					}
				}
				if ie.Alternative != nil {
					if ns, ch := p.rewriteItPlaceWritesStmts(ie.Alternative.Statements, tok, baseLeft, idxTempName, elemType, used, writeInBounds); ch {
						ie.Alternative.Statements = ns
						changed = true
					}
				}
			}
		case *ForStatement:
			if n.Body != nil {
				if ns, ch := p.rewriteItPlaceWritesStmts(n.Body.Statements, tok, baseLeft, idxTempName, elemType, used, writeInBounds); ch {
					n.Body.Statements = ns
					changed = true
				}
			}
		}
		out = append(out, s)
	}
	return out, changed
}

// unionElemType 從聯合型別字串中提取 payload（非 err/nil）型別。
// "?i64" → "i64"；"i64 | err" → "i64"；"i64 | err | nil" → "i64"。
// 若無法唯一確定（如多個非變體型別）則回傳空字串。
func unionElemType(t string) string {
	if t == "" {
		return ""
	}
	if ty.IsOptionRaw(t) {
		return ty.StripOptionRaw(t)
	}
	parts := strings.Split(t, "|")
	var elem []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "err" || p == "nil" {
			continue
		}
		elem = append(elem, p)
	}
	if len(elem) == 1 {
		return elem[0]
	}
	return ""
}

// enumVariantPayload 查詢枚舉變體的載荷型別，回傳 "" 表示單元變體或不存在。
// 同時嘗試原樣鍵與去模組前綴的裸名鍵（解析期以裸名登記，模組前綴化後可能不一致）。
func (p *Parser) enumVariantPayload(enumType, variant string) string {
	if p.sem == nil || enumType == "" {
		return ""
	}
	if pt, ok := p.sem.EnumVariantPayloadOf(enumType, variant); ok {
		return pt
	}
	if idx := strings.LastIndex(enumType, "."); idx > 0 {
		if pt, ok := p.sem.EnumVariantPayloadOf(enumType[idx+1:], variant); ok {
			return pt
		}
	}
	return ""
}

// enumVariantFieldTypes 查詢枚舉變體的載荷欄位型別列表（單元變體回傳 nil）。
// 同時嘗試原樣鍵與去模組前綴的裸名鍵。
func (p *Parser) enumVariantFieldTypes(enumType, variant string) []string {
	if p.sem == nil || enumType == "" {
		return nil
	}
	if fs := p.sem.EnumVariantFieldsOf(enumType, variant); len(fs) > 0 {
		return fs
	}
	if idx := strings.LastIndex(enumType, "."); idx > 0 {
		if fs := p.sem.EnumVariantFieldsOf(enumType[idx+1:], variant); len(fs) > 0 {
			return fs
		}
	}
	return nil
}

// isSentinelArmType reports whether armType denotes only the nil/err variants
// (no payload), e.g. "nil", "err", or "err | nil". For these arms `it` is a
// placeholder and must never trigger the struct-deref codegen path
// (stmt.go:6339), which would load the option's data field unconditionally —
// including when the option is nil/err (data == 0 → null deref → segfault).
func isSentinelArmType(armType string) bool {
	if armType == "" {
		return false
	}
	// "else" denotes the complement of ok (i.e., err | nil) — both are
	// sentinels with no payload, so it must be treated as sentinel.
	if strings.TrimSpace(armType) == "else" {
		return true
	}
	for _, part := range ty.UnionMembersOrSelfRaw(armType) {
		part = strings.TrimSpace(part)
		if part == "err" || part == "nil" || part == "" {
			continue
		}
		return false
	}
	return true
}

// buildItBindingForArm creates `it = matched` LetStatement with the correct type
// for the specific match arm. For option types (e.g., ?i64):
//
//	err arm -> it: err (variant type)
//	nil arm -> it: nil (variant type)
//	ok arm  -> it: elemType (e.g., i64 for ?i64)
//
// 型別資訊取自 sm 的解析期快照（MatchedVarType/IsEnumType）。
func (p *Parser) buildItBindingForArm(tok lexer.Token, matched Expression, armType string, elemType string, matchedVarType string, isEnumType bool, name string) *LetStatement {
	if name == "" {
		name = "it"
	}
	_, ok := matched.(*Identifier)
	if !ok {
		return nil
	}
	t := matchedVarType

	// When matchedVarType is unknown at parse time (e.g., variable assigned from
	// a method call whose return type is resolved later), we still need to create
	// `it` bindings for err and nil arms so the codegen can unwrap the error
	// message at runtime. Without this, the err arm body (e.g. `content = err(it)`)
	// would reference `it` from the ok arm's collectVarDecls, causing a type
	// mismatch (e.g., %vec instead of %str-long).
	// For ok arms (armType "ok", "else", "ok_err", etc.) with unknown type,
	// return nil so the caller falls back to the shared `it` binding (safe,
	// because the option is non-nil in ok arms, so the struct-deref is guarded
	// by the live variant).
	if t == "" {
		switch armType {
		case "err":
			return &LetStatement{
				Token:        tok,
				Name:         &Identifier{Token: tok, Value: name},
				Value:        matched,
				Type:         &NamedType{Value: "err"},
				IsSynthetic:  true,
				IsArmBinding: true,
			}
		case "nil":
			return &LetStatement{
				Token:        tok,
				Name:         &Identifier{Token: tok, Value: name},
				Value:        matched,
				Type:         &NamedType{Value: "nil"},
				IsSynthetic:  true,
				IsArmBinding: true,
			}
		default:
			return nil
		}
	}

	var typeStr string
	if ty.IsOptionRaw(t) && elemType != "" {
		// Option type: map armType to specific type string
		switch armType {
		case "err":
			typeStr = "err"
		case "nil":
			typeStr = "nil"
		case "ok":
			typeStr = elemType
		case "else":
			typeStr = "err | nil"
		case "ok_err", "ok_nil", "ok_err_nil":
			// For codegen, `it` in ok-containing arms is the payload type.
			// The union type (e.g. "file | nil") is only meaningful for the
			// type checker; using it in codegen causes method dispatch to
			// generate invalid function names with `|` characters.
			typeStr = elemType
		default:
			return nil
		}
	} else if !isEnumType && elemType != "" && (strings.Contains(t, "err") || strings.Contains(t, "nil")) {
		// 結果聯合型別（如 i64 | err / i64 | err | nil）：按 armType 收窄 it。
		// 與 ?X 選項型別類似，但變體為 err/nil（而非 ok/err/nil）。
		var remaining []string
		for _, v := range []string{"err", "nil"} {
			if strings.Contains(t, v) {
				remaining = append(remaining, v)
			}
		}
		remStr := strings.Join(remaining, " | ")
		switch armType {
		case "err":
			typeStr = "err"
		case "nil":
			typeStr = "nil"
		case "ok":
			typeStr = elemType
		case "else":
			typeStr = remStr
		case "ok_err", "ok_nil", "ok_err_nil":
			// For codegen, `it` in ok-containing arms is the payload type.
			typeStr = elemType
		default:
			return nil
		}
	} else if isEnumType {
		// Enum type: armType is the variant name or union expression (e.g., "status1" or "status2 | status3")
		typeStr = armType
	} else {
		return nil
	}

	return &LetStatement{
		Token:        tok,
		Name:         &Identifier{Token: tok, Value: name},
		Value:        matched,
		Type:         &NamedType{Value: typeStr},
		IsSynthetic:  true,
		IsArmBinding: true,
	}
}

// namedItBinding 把共享的 `it = matched` 綁定改名為 `name = matched`
// （析構綁定 `ok(v) -> ...` 用）。name 為空或 "it" 時直接回傳原節點。
func (p *Parser) namedItBinding(it *LetStatement, name string) *LetStatement {
	if it == nil || name == "" || name == "it" {
		return it
	}
	nb := *it
	nb.Name = &Identifier{Token: it.Token, Value: name}
	nb.IsArmBinding = true
	return &nb
}

// prependStmt prepends a statement to a BlockStatement, returning a new BlockStatement.
func (p *Parser) prependStmt(body *BlockStatement, stmt Statement) *BlockStatement {
	if body == nil {
		return &BlockStatement{Statements: []Statement{stmt}}
	}
	stmts := make([]Statement, 0, len(body.Statements)+1)
	stmts = append(stmts, stmt)
	stmts = append(stmts, body.Statements...)
	nb := &BlockStatement{Token: body.Token, Statements: stmts, IsInline: body.IsInline}
	// Preserve the original body block's comments when the desugar injects an
	// `it` binding. The arm-body block is REPLACED by nb, so without this the
	// comments that live inside the block (trailing comments before `}`, the
	// closing-brace comment and the `{`-line comment) are silently dropped.
	// Symptom: option-match arms such as `ok -> { s ; tail }` lost `; tail` on
	// format (notools/nonpm tests). Bare-match arms never call prependStmt,
	// which is why only `v: { ok -> { ... } }` regressed.
	nb.TrailingComments = body.TrailingComments
	nb.ClosingBraceComment = body.ClosingBraceComment
	nb.RBrace = body.RBrace
	if obc := p.fmtTrivia.OpeningBraceCommentOf(body); obc != nil {
		p.fmtTrivia.SetOpeningBraceComment(nb, obc)
	}
	// 保留原 body 區塊的 #{overflow = ...} 等註解：裸配對臂的溢出模式掛在
	// bodyBlock 上，desugar 插入 `it` 綁定時若丟棄註解，generateIfExpression
	// 便無法從 Consequence/Alternative 區塊讀到模式，導致臂體條件內的整數
	// 運算（如 `n >= 0 - 128` 的 `0 - 128`）退回預設 option 模式。
	if an := p.sem.AnnotationsOf(body); len(an) > 0 {
		p.sem.SetRawAnnotations(nb, an)
	}
	return nb
}

// SplitIndexReceiverChain 拆解「以安全索引為接收者的方法／欄位呼叫鏈」：
//
//	.names[i].to-lower()   → root = .names[i]
//	a[i].f().g(x)          → root = a[i]
//	obj.f[i].field         → root = obj.f[i]
//
// 回傳鏈根的 *IndexExpression 與一個「把鏈根換成別的表達式」的重建函式。
// 重建一律複製節點、共享未變子樹（H0 直出不得改寫使用者 AST）。
// 非此形狀回傳 (nil, nil)；鏈根恰好就是 e 本身時回傳 identity 重建。
//
// 關鍵：只沿 CallExpression.Function / DotExpression.Receiver 下探，**不**進入
// Arguments —— `f(a[i])` 的索引是呼叫引數，不是接收者，不能當成 `?=` 的傳播點。
// checker 亦以此函式判定 `?=` RHS 鏈根的裸索引已被顯式消費（idxbruse 豁免）。
func SplitIndexReceiverChain(e Expression) (*IndexExpression, func(Expression) Expression) {
	switch v := e.(type) {
	case *IndexExpression:
		return v, func(r Expression) Expression { return r }
	case *DotExpression:
		if v.Receiver == nil {
			return nil, nil
		}
		root, rebuild := SplitIndexReceiverChain(v.Receiver)
		if root == nil {
			return nil, nil
		}
		return root, func(r Expression) Expression {
			nd := *v
			nd.Receiver = rebuild(r)
			return &nd
		}
	case *CallExpression:
		if v.Function == nil {
			return nil, nil
		}
		root, rebuild := SplitIndexReceiverChain(v.Function)
		if root == nil {
			return nil, nil
		}
		return root, func(r Expression) Expression {
			nc := *v
			nc.Function = rebuild(r)
			return &nc
		}
	}
	return nil, nil
}

// lowerUnwrapAssign desugars `v ?= expr` into a match/if chain:
//
//	__unwrap_N = expr
//	__unwrap_N: {
//	    nil || err -> {
//	        result = __unwrap_N
//	        return
//	    }
//	    -> v = it
//	}
//
// The result param name is found from the current function definition's Results.
// If no option-typed result param is found, the desugar produces a parse error
// (the checker will report it).
func (l *lowerer) lowerUnwrapAssign(uas *UnwrapAssignStatement) Statement {
	tok := uas.Token

	// ok 臂要寫給 LHS 的值。一般情形是解箱後的 `it`；當 RHS 是「以安全索引
	// 為接收者的方法鏈」（如 `h ?= .names[i].to-lower()`）時改成 `it.to-lower()`
	// —— option 由鏈根索引產生，方法只作用在解箱後的值上。
	itIdent := &Identifier{Token: tok, Value: "it"}
	okValue := Expression(itIdent)
	// 鏈式 RHS 的最外層回傳型別（可能不同於鏈根元素型別，例如
	// `h ?= .names[i].len()` 的 elem=str / result=i64）。空值代表沿用 elemType。
	var lhsTypeOverride string

	// `.field[i]` 的基底解析（indexBaseContainerRaw → indexBaseReceiverType →
	// resolveReceiverType）要靠 `Parser.methodStructStack` 把隱含的 `self`
	// 解成 struct 名。解析期由 decl.go 在 parseFunctionBody 前後 push/pop，
	// 而 lowering / H0 走查發生在 parse **之後**，那時棧早已清空。
	//
	// `?=` 的**兩側**都可能是結構欄位下標：寫側目標 `a[i] ?= v` 的
	// uas.Target，以及讀側 RHS `v ?= self.f[i]`。故補推必須覆蓋整次降糖，
	// 不能只放在寫側分支裡。補推層級維持「單一 `?=` 呼叫」：若改成在走查
	// 進入 FunctionDefinition 時就 push，會改變所有 `.field` 接收者解析的既有
	// 結果，使 BuildSemViews 的重建路徑（不走走查、只回放事實）與 fresh parse
	// 分叉，semviews_test.go 的 TestBuildSemViewsMatchesFreshParse 會在
	// std/bigint.no 上報 `?bigint||-` vs `||-`。此處進來就 push、函式返回前
	// pop（defer），影響面僅限本次 `?=` 降糖。與 tohir.go:1493 同口徑：
	// H0 直出由 scratchParser 以 c.curSelfType 補齊，這裡 savedLen != 0 時
	// 不重複推。
	if savedLen := len(l.p.methodStructStack); savedLen == 0 {
		if st := functionSelfType(l.curFuncDef); st != "" {
			l.p.methodStructStack = append(l.p.methodStructStack, st)
			defer func() { l.p.methodStructStack = l.p.methodStructStack[:savedLen] }()
		}
	}

	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		nm := ""
		if uas.Name != nil {
			nm = uas.Name.Value
		}
		fmt.Fprintf(os.Stderr, "[debug-it] lowerUnwrapAssign: name=%q line=%d curFunc=%q\n", nm, tok.Line, l.curFuncName)
	}

	// First, walk into the Value expression to lower any nested SurfaceMatch.
	l.walk(reflect.ValueOf(&uas.Value))

	// Find the option-typed result param name from the current function def.
	// H0 直出（lowerUnwrapAssignToHIR）透過 resultNameOverride 傳入（funcLike
	// 已從 sig.Results 提取），此處僅在 walk 路徑（curFuncDef 可用）時探測。
	resultName := l.resultNameOverride
	if resultName == "" && l.curFuncDef != nil {
		for _, res := range l.curFuncDef.Results {
			if res.Type != nil {
				ts := typeString(res.Type)
				if ty.IsOptionRaw(ts) {
					resultName = res.Name
					break
				}
			}
		}
	}
	// 最近的塊 match 永遠捕捉：即使所在函式本身有 ?T 結果參數，也改用
	// 合成 subject 作為上拋目標（塊 match 語意 #4）。
	if l.blockMatchSubject != "" {
		resultName = l.blockMatchSubject
	}

	// --- D9 寫入側 ?=：`a[i] ?= v` ---
	// 寫入目標為安全索引時，option 性來自「下標讀取」（a[i] 越界回傳 nil），
	// **不是**右側值 v。現有 generic 檢查（`rhsDefinitelyNonOption(uas.Value)`
	// 會把字面量 v 當非 option 報錯）在此是錯的，必須走專用分支：構造安全索引
	// match，ok 臂把 v 經 place 寫回元素位置、nil/err 臂向上傳播錯誤。
	// 僅當目標是「安全索引基底」（直接 arr/vec/slice 變數）時接管；struct-field
	// 基底 / map / 未知型別落在 wave 1 豁免區，交回既有 ?= 路徑（其報錯會在 P2
	// idxwrite 啟用後被更精確的寫入側指引取代）。
	// map 基底必须在 isSafeIndexBase 之前拦下（D9 §12.2.1）：map 的写是 put，
	// 任何键恒有定义，没有可传播的 nil；而 ?= 的 ok 臂是「读出 a[i] 再写回元素
	// **位置**」，map 读出的 ?V 只是 get 的 out-param 副本，写回它不会回到 map。
	// 放任它落到 isSafeIndexBase 分支（containerElemType 把 [str]i64 当数组，
	// 故该分支确实会命中），降糖出的控制流会让同一张 map 在多条路径上各 drop
	// 一次 —— pristine HEAD 是编译期 `lencap struct: len/cap unsupported on
	// struct hashmap-str-i64`；MIR 补上 map 下标降糖后变成**编译成功、运行期
	// SIGBUS** 的三重 free。故此处必须是硬错误。
	if uas.Name == nil {
		// 寫入側 `?=`：methodStructStack 已在函式開頭補推（見上），此處直接
		// 用 isSafeIndexBase / mapIndexBaseType 判定 `.field[i]` 基底。
		if idx, ok := uas.Target.(*IndexExpression); ok {
			if mt := l.p.mapIndexBaseType(idx); mt != "" {
				l.p.saveErrorf("pvma2wsn", "line %d, column %d: `?=` on a map subscript is not supported (base type `%s`); write `%s = v` directly — a map key has no out-of-range case, so the write is a put and cannot fail",
					tok.Line, tok.Column, mt, exprToString(uas.Target))
				return nil
			}
			if l.p.isSafeIndexBase(idx) {
				return l.lowerUnwrapAssignToIndex(uas, idx, resultName)
			}
		}
	}

	// --- 複合 / 非 option ?= 檢查與 option 運算元守衛注入 ---
	// 解析期能確定的「明確非 option」右側（字面量、已知非 option 區域變數）直接使用
	// ?= 無意義，報錯提示改用 `=`。其餘（呼叫回傳型別、未知型別變數）在解析期尚不可判
	// 定，交由檢查器處理，避免誤報（見 v ?= bar() 之類的 option 回傳呼叫）。
	// 若右側含 option 運算元（如 a ?= b + c + d、a ?= f(b + c)），為每個 option 運算元
	// 綁定臨時變數並注入傳播守衛（nil/err → result = __opt_N; return）。
	var guards []Statement
	rhs := uas.Value
	if resultName != "" {
		if l.rhsDefinitelyNonOption(uas.Value) {
			l.p.saveErrorf("5l952ztl", "line %d, column %d: `?=` RHS `%s` is not an option — use `=` instead",
				tok.Line, tok.Column, uas.String())
			return nil
		}
		// `h ?= .names[i].to-lower()`：option 來自**鏈根的安全索引**，不是最外層
		// 呼叫的回傳值。把 RHS 換成鏈根索引，讓下面 `case *IndexExpression`
		// 正確標註 __unwrap_N 為 `?elem`；ok 臂再把 `it` 放回鏈根呼叫方法。
		// 這樣寫法與 `h = .names[i]: { ok -> it.to-lower() -> '' }` 等價，但
		// 由 `?=` 一肩上拋 nil/err，不需額外宣告 `h str = ''`。
		if root, rebuild := SplitIndexReceiverChain(uas.Value); root != nil {
			rhs = root
			okValue = rebuild(itIdent)
			if call, ok := uas.Value.(*CallExpression); ok {
				if t := l.p.inferTypeFromCallExpr(call); t != "" {
					lhsTypeOverride = ty.StripOptionRaw(t)
				}
			} else if t := l.chainOuterType(uas.Value); t != "" {
				// 純字段鏈（`.nodes[i].first`）：最外層是 DotExpression，
				// 需要取該字段的宣告型別；否則 LHS 會拿到鏈根的
				// 元素型別（整個 struct），下游把它當純量用會炸。
				lhsTypeOverride = t
			}
		} else {
			rhs = l.guardOptionOperands(uas.Value, &guards, resultName, tok)
		}
	}

	// Create a unique temporary variable name for the option value.
	tmpName := fmt.Sprintf("__unwrap_%d", tok.Line)
	if l.unwrapTmpOverride != "" {
		tmpName = l.unwrapTmpOverride
	}
	tmpIdent := &Identifier{Token: tok, Value: tmpName}

	// Infer the type of the temp variable from the RHS expression.
	// This allows buildMatchDesugar to resolve elemType for `it` bindings.
	savedFuncName2 := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	if call, ok := rhs.(*CallExpression); ok {
		if inferred := l.p.inferTypeFromCallExpr(call); inferred != "" {
			l.p.setVarType(tmpName, inferred)
		}
	}
	l.p.curFuncName = savedFuncName2

	// 對於算術 RHS（如 `a ?= b - c`），?= 需要 option 接收端才能傳播溢出錯誤。
	// 將暫存變數標註為 ?i64：overflow 預設回傳的 option 內部資料一律為 i64
	//（窄型別已 zext / 符號擴展至 i64），故 option 結構 %option 與內部型別無關。
	// 必須在 LetStatement 上顯式掛 NullableType 節點，否則 checker 會從 RHS
	//（未標註減法預設 wrap）推斷成純 i64，導致後續 `r = __unwrap_N` 報
	// i64 -> ?i64 型別錯誤。
	var tmpType Type
	var elemType string // 安全索引的元素型別（v[i] 的 i）；用於標註 LHS 區域變數型別
	switch v := rhs.(type) {
	case *InfixExpression:
		l.p.setVarType(tmpName, "?i64")
		tmpType = &NullableType{
			Token: tok,
			Type:  &NamedType{Token: tok, Value: "i64"},
		}
		// 解箱後內部值型別：overflow 預設 option 內部資料一律為 i64
		//（窄型別已 zext / 符號擴展至 i64），故解箱值恆為 i64。標註 LHS
		// 區域變數型別，避免 codegen 退回被全域 VarTypes 同名污染（std 常見
		// a:str → %str-long）的型別 → `%a.val defined with type '%str-long'
		// but expected 'i64'（見 q_probe_c / q_probe_ab A）。
		elemType = "i64"
		if uas.Name != nil && uas.Name.Value != resultName {
			l.p.sem.SetFuncVarType(l.curFuncName, uas.Name.Value, "i64")
		}
	case *CallExpression:
		// 函式呼叫 RHS（如 a ?= f(n)）：從呼叫回傳型別推導解箱後內部值型別，
		// 標註 LHS 區域變數，避免 codegen 退回被全域 VarTypes 同名污染（std
		// 常見 a:str → %str-long）的型別 → `%a.val defined with type '%str-long'
		// but expected 'i64'（見 q_probe_ab A）。LHS 為結果參數時不標註（結果
		// 參數本身已是 ?T，走既有 wrap 路徑，保持 res ?= f(21) 既有行為）。
		if inferred := l.p.inferTypeFromCallExpr(v); inferred != "" {
			inner := ty.StripOptionRaw(inferred)
			elemType = inner
			l.p.setVarType(tmpName, inferred)
			// 必須同時在 LetStatement 上掛 NullableType 節點：H0 直出（tohir.go
			// 的 lowerUnwrapAssignToHIR）只從 s.Type 取型別 ID，讀不到 FuncVarTypes。
			// 漏設時 `let __unwrap_N` 的 HIR Type=0（無型別），MIR 取不到 ?T 的
			// payload ⇒ ok 臂 `let it = __unwrap_N` 無值 ⇒ 目標變數恆 undef
			//（fs.no 的 `size ?= fs.fstat-size(.fd)` 即為 CallExpression 分支）。
			// Infix / Index 兩分支早已如此，此處為對齊補齊。
			if ty.IsOptionRaw(inferred) {
				tmpType = &NullableType{
					Token: tok,
					Type:  &NamedType{Token: tok, Value: inner},
				}
			}
			if uas.Name != nil && uas.Name.Value != resultName {
				l.p.sem.SetFuncVarType(l.curFuncName, uas.Name.Value, inner)
			}
		}
	case *IndexExpression:
		// 安全索引：`v[i]` 越界回傳 err（D11 起 nil 只代表值確實不存在）；
		// `?=` 解箱並向上傳播。基底可以是直接變數（`a[i]`）或結構欄位下標
		// （`.field[i]` / `obj.field[i]` / `a[i].f[j]`）——兩者都由
		// indexBaseContainerRaw 統一判定，且 codegen / MIR 的安全索引降糖
		// 早已涵蓋 DotExpression 基底（isSafeIndexBase 對它回傳 true）。
		// 舊版在此以 `v.Left.(*Identifier)` 硬擋結構欄位（gcvwum3g），
		// 與其餘索引語意不一致，D11 後移除：讀側 `v ?= .field[i]` 與寫側
		// `.field[i] ?= v` 現在共用同一套基底解析。
		// 推斷元素型別以便讓 match desugar 的 it 綁定拿到正確型別。
		if elem := l.p.inferIndexElemType(v); elem != "" {
			// `[N]?T` / `[]?T` 的下標讀取本身**就是** `?T`（三態 ok/nil/err
			// 全在同一個 option 的 tag 上），不是 `??T`。containerElemType
			// 回傳的是宣告的元素型別原文 `?T`，直接用會把 __unwrap_N 標成
			// `??T`、把 LHS 標成 `?T`，ok 臂再把整個 option 結構當成純量
			// 讀出 → 執行期出現指標大小的垃圾值。故這裡剝掉一層 option：
			// 暫存變數 `?T`、it / LHS 都是 `T`。
			elemType = ty.StripOptionRaw(elem)
			l.p.setVarType(tmpName, "?"+elemType)
			tmpType = &NullableType{
				Token: tok,
				Type:  &NamedType{Token: tok, Value: elemType},
			}
			// 記錄 LHS 區域變數（如未標型別的 x）的元素型別到「函數作用域」
			// FuncVarTypes，避免 codegen 退回被全域 VarTypes 同名污染
			// （std 常見 x:str → %str-long）而把 x 誤當字串，導致
			// `%x.val defined with type '%str-long' but expected 'i64'`
			// （見 safe-index-utl.no 的 safe-utl）。寫入當前函數作用域
			// （非全域）確保不跨函數污染 std。
			if uas.Name != nil {
				l.p.sem.SetFuncVarType(l.curFuncName, uas.Name.Value, elemType)
			}
		}
	}

	// 後備推斷：若 elemType/tmpType 仍未設定（例如 RHS 是直接識別符 `inner ?= res`，
	// 而非 Infix/Index 表達式），從 RHS 變數的選項型別推導元素型別。
	// 否則 codegen 會退回被全域 VarTypes 同名污染（std 常見 inner/x/v:str → %str-long）
	// 的型別，導致 inner 被誤配為 %str-long、且 __unwrap_N 被誤配為 i64（8 位元組）
	// 卻以 %option（16 位元組）存取 → 記憶體損壞與崩潰（見 test-iso-q.no）。
	if elemType == "" {
		if ident, ok := rhs.(*Identifier); ok {
			t := ""
			if tt, ok2 := l.p.sem.FuncVarType(l.curFuncName, ident.Value); ok2 {
				t = tt
			} else if tt, ok2 := l.p.sem.VarTypes[ident.Value]; ok2 {
				t = tt.Raw()
			}
			if ty.IsOptionRaw(t) {
				elemType = ty.StripOptionRaw(t)
				l.p.setVarType(tmpName, "?"+elemType)
				tmpType = &NullableType{
					Token: tok,
					Type:  &NamedType{Token: tok, Value: elemType},
				}
				if uas.Name != nil {
					l.p.sem.SetFuncVarType(l.curFuncName, uas.Name.Value, elemType)
				}
			}
		}
	}

	// LHS 的型別標註：一般與 __unwrap_N 的內部型別相同；鏈式 RHS 則以最外層
	// 呼叫的回傳型別為準（`h ?= .names[i].len()` 的 h 是 i64，不是 str）。
	lhsType := elemType
	if lhsTypeOverride != "" {
		lhsType = lhsTypeOverride
		if uas.Name != nil && uas.Name.Value != resultName {
			l.p.sem.SetFuncVarType(l.curFuncName, uas.Name.Value, lhsType)
		}
	}

	// Build the temporary variable assignment: __unwrap_N = expr
	tmpAssign := &LetStatement{
		Token: tok,
		Name:  tmpIdent,
		Type:  tmpType,
		Value: rhs,
	}

	// Build match arms.
	var arms []MatchArm

	var preStmts []Statement
	// 傳播賦值目標是否「就是」option 結果參數本身（`r ?= ...`）。
	// 僅此情形才能把 `r = __unwrap_N` 提前到 match 之前：此時 r 與 __unwrap_N
	// 型別一致（同為 ?T），整體 option 結構淺拷貝即語意正確。
	// 若目標是其他局部變數（如 `size ?= fstat-size(.fd)`，size:i64 而結果參數
	// content:?[]byte），提前拷貝會把 ?i64 的整個 option 結構（ok 時 data=原始
	// i64）塞進 ?[]byte 的結果槽 —— 之後 codegen 對 content 的 freeOldHeapValue
	// /clone 會把該 i64 當 box 指標解引用 → segfault（見 fs.no file.read-bytes）。
	// 此時改為在 nil/err arm 內才拷貝（僅傳播 err/nil 結果），ok 路徑不污染
	// 結果槽，由用戶程式碼自行賦值。
	targetIsResult := uas.Name != nil && uas.Name.Value == resultName
	// 塊 match 的 subject 是合成名，使用者不可能寫出 `?=` 目標剛好等於它；
	// 即使寫了也必須走「僅在 nil/err 臂寫入」的路徑，否則 match 前就先把
	// 整個 option（含 ok）塞進 subject，成功路徑會讓 arms 看到 ok。
	if l.blockMatchSubject != "" {
		targetIsResult = false
	}
	if resultName != "" && targetIsResult {
		// 將 `result = __unwrap_N` 提前到 match 之前：三個 arm 都不再寫回 r，
		// 因為 match 前的這次賦值已把 opt 結果（ok/err/nil）寫入 r，arm 內
		// 只需 return（nil/err）或提取內部值（ok 臂的 v = it）。
		// 關鍵：若把 `r = __unwrap_N` 放在 err/nil arm 內，該賦值會因 arm 內的
		// sentinel `it` 綁定而未被 codegen 發出（codegen 僅對 ok 臂發出 store
		// %option 到 %r），導致 r 停留初始 nil、呼叫方匹配到 nil 而非 err。
		// 提前可徹底規避此 codegen 坑。
		preStmts = append(preStmts, &LetStatement{
			Token: tok,
			Name:  &Identifier{Token: tok, Value: resultName},
			Value: tmpIdent,
		})
		// nil -> return；err -> return（r 已在 match 前寫入 __unwrap_N）。
		// 分別展開為兩個獨立 arm（而非 multiOptionPatterns 的
		// `matched == nil || matched == err` 組合條件），因為組合 OR 條件會
		// 觸發 option 變數的「值比較」代碼路徑（對 err/nil 關鍵字做 load，
		// 產生未定義的 %err / 錯誤的 %nil 比較）；獨立 arm 走 tag 比較路徑。
		arms = append(arms, MatchArm{
			Condition:     &Identifier{Token: tok, Value: "nil"},
			Body:          &BlockStatement{Token: tok, Statements: []Statement{&ReturnStatement{Token: tok}}},
			IsBlockBody:   true,
			Pos:           posFromToken(tok),
			SkipItBinding: true,
		})
		arms = append(arms, MatchArm{
			Condition:     &Identifier{Token: tok, Value: "err"},
			Body:          &BlockStatement{Token: tok, Statements: []Statement{&ReturnStatement{Token: tok}}},
			IsBlockBody:   true,
			Pos:           posFromToken(tok),
			SkipItBinding: true,
		})
	} else if resultName != "" {
		// 非結果參數目標（`v ?= ...` 且 v != 結果參數）：把 option 結構的傳播
		// 拷貝放進 nil/err arm 內，ok 路徑完全不動結果參數。
		propagate := func() *BlockStatement {
			// whole-option 拷貝（IsPropagation）保留 err.err 的 msg/code/file/
			// line/col 原樣；塊 match 不 return，由 `if __bm_N == nil` 守衛
			// 停止塊主體後讓 arms 以 err 跑一次。
			stmts := []Statement{
				&LetStatement{
					Token:         tok,
					Name:          &Identifier{Token: tok, Value: resultName},
					Value:         tmpIdent,
					IsPropagation: true,
				},
			}
			stmts = append(stmts, blockMatchTail(l.blockMatchSubject, tok)...)
			return &BlockStatement{Token: tok, Statements: stmts}
		}
		arms = append(arms, MatchArm{
			Condition:     &Identifier{Token: tok, Value: "nil"},
			Body:          propagate(),
			IsBlockBody:   true,
			Pos:           posFromToken(tok),
			SkipItBinding: true,
		})
		arms = append(arms, MatchArm{
			Condition:     &Identifier{Token: tok, Value: "err"},
			Body:          propagate(),
			IsBlockBody:   true,
			Pos:           posFromToken(tok),
			SkipItBinding: true,
		})
	} else {
		// No option result param — emit a parse error placeholder body.
		//
		// 複合賦算降糖來的（DeprecatedOp != ""）不重複報 generic 錯誤：parser 在
		// 函式定義解析完時已就「所在函式沒有 option 結果參數」發過一條更具體的
		// ERROR（含 `a + 1: { ok -> a = it, -> }` 這兩種可寫法，見
		// flushCompoundAssigns）。同一行再補一句「`?=` can only be used …」只會
		// 讓用戶以為解決方案是去掉 `?=`，而去掉之後仍是條死路。
		if uas.DeprecatedOp == "" && l.blockMatchSubject == "" {
			l.p.saveErrorf("l86f91me", "line %d, column %d: `?=` can only be used inside a function with an option-typed result param", tok.Line, tok.Column)
		}
		arms = append(arms, MatchArm{
			Condition:     &Identifier{Token: tok, Value: "nil"},
			Body:          &BlockStatement{Token: tok, Statements: []Statement{&ReturnStatement{Token: tok}}},
			IsBlockBody:   true,
			Pos:           posFromToken(tok),
			SkipItBinding: true,
		})
		arms = append(arms, MatchArm{
			Condition:     &Identifier{Token: tok, Value: "err"},
			Body:          &BlockStatement{Token: tok, Statements: []Statement{&ReturnStatement{Token: tok}}},
			IsBlockBody:   true,
			Pos:           posFromToken(tok),
			SkipItBinding: true,
		})
	}

	// -> { v = okValue }（wildcard / ok 臂：r 已在 match 前寫入 __unwrap_N，
	// 此處僅把內部值解箱給 v）。一般 okValue 就是 `it`；鏈式 RHS 則是
	// `it.method(...)`。欄位/索引目標改用 AssignExpression（target = okValue）。
	var okArm Statement
	if uas.Target != nil {
		okArm = &ExpressionStatement{
			Token: tok,
			Expression: &AssignExpression{
				Token: tok,
				Left:  uas.Target,
				Value: okValue,
			},
		}
	} else if uas.Name != nil && uas.Name.Value != resultName {
		// 非結果參數目標（v != result）：v 已在 match 前預宣告（見下方 stmts 組裝），
		// 此處僅賦值 `v = okValue`（AssignExpression 寫入預宣告的 slot）。若用 LetStatement
		// 會把 v 定義成臂內局部，臂外 `r = v` 讀不到 ⇒ MIR "move src slot"（bug #85 鄰域）。
		okArm = &ExpressionStatement{
			Token: tok,
			Expression: &AssignExpression{
				Token: tok,
				Left:  uas.Name,
				Value: okValue,
			},
		}
	} else {
		// 結果參數目標（r ?= …）：r 是 out-param，本就有 slot，LetStatement 直接賦值。
		la := &LetStatement{
			Token: tok,
			Name:  uas.Name,
			Value: okValue,
		}
		// 標註 LHS 區域變數的元素型別（安全索引 v[i] 的解箱值），使 generateLet
		// 以正確型別配置 x 的 alloca，而非退回被全域 VarTypes 同名污染（std 常見
		// x:str → %str-long）的型別——否則 `%x.val defined with type '%str-long'
		// but expected 'i64'。節點級標註，零跨函數污染風險。
		if lhsType != "" {
			la.Type = &NamedType{Token: tok, Value: lhsType}
		}
		okArm = la
	}
	okStmts := []Statement{okArm}
	if resultName == "" {
		// 無 result param（理論上不會發生，lowerUnwrapAssign 已報錯）：
		// 僅解箱給 v。
		okStmts = []Statement{
			&LetStatement{
				Token: tok,
				Name:  uas.Name,
				Value: itIdent,
			},
		}
	}
	okBody := &BlockStatement{
		Token:      tok,
		Statements: okStmts,
	}
	arms = append(arms, MatchArm{
		IsWildcard:  true,
		Body:        okBody,
		IsBlockBody: true,
		Pos:         posFromToken(tok),
	})

	// Build SurfaceMatch and desugar it.
	sm := &SurfaceMatch{
		Token:   tok,
		Matched: tmpIdent,
		Arms:    arms,
	}

	// Set the var type for the temp variable so desugarSurfaceMatchToIfExpr can infer elemType.
	// Try to infer from the Value expression (e.g. function call returning ?T).
	savedFuncName := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	lowered := l.p.desugarSurfaceMatchToIfExpr(sm)
	l.p.curFuncName = savedFuncName

	if lowered == nil {
		return nil
	}

	// Wrap in a block: { [__opt_N = operand; match {nil/err -> propagate}]; __unwrap_N = expr; [result = __unwrap_N]; <lowered match> }
	// guards（option 運算元守衛）必須在 __unwrap_N 指派與 match 之前，確保任一
	// option 運算元為 nil/err 時先上拋；preStmts（result = __unwrap_N）亦須在
	// match 之前，確保 r 在 arm 內的 return 前已被賦值為 opt 結果（ok/err/nil）。
	stmts := make([]Statement, 0, len(guards)+len(preStmts)+3)
	// 非結果參數目標（v != result）：需要一條 `v <elemType> = <zero>` 預宣告，使 v
	// 在臂外可見且有 slot；ok 臂以 AssignExpression 寫入此預宣告的 slot，避免臂內
	// LetStatement 把 v 定義成臂內局部（MIR "move src slot"）。
	//
	// ⚠️ 位置是語意攸關的：它必須排在 tmpAssign（`__unwrap_N = <RHS>`）**之後**。
	// 這條預宣告本質上是「把 v 清零」，而 RHS 可以引用 v 本身——最典型的是自我
	// 引用的解箱 `p ?= lf[p]`（BWT 反變換的 LF 鏈、鏈式 next 走訪）。放在前面時，
	// 求值 RHS 用的是剛被清零的 v，索引恆為 0：p 每輪都讀 lf[0]，鏈條原地打轉。
	//   p = 2
	//   p ?= lf[p]  ; 舊： preDecl(p=0) → 讀 lf[0] → p = 1（永遠）
	//               ; 新： 讀 lf[p]（p 的既有值）→ preDecl(p=0) → match 覆寫 p
	// 自我索引的 `?=` 解箱（`p ?= lf[p]`）與 `=` 型寫法同受此位置約束。
	// 移到之後仍滿足原目的：match 的兩條臂都會寫 v，預宣告的零值從不被讀到，
	// 它只負責「建立 slot / 讓臂外可見」這一件事。
	var preDecls []Statement
	if uas.Name != nil && uas.Name.Value != resultName && lhsType != "" {
		if zero := scalarZeroLiteral(tok, lhsType); zero != nil {
			preDecls = append(preDecls, &LetStatement{
				Token: tok,
				Name:  uas.Name,
				Type:  &NamedType{Token: tok, Value: lhsType},
				Value: zero,
			})
		}
	}
	tail := []Statement{tmpAssign}
	tail = append(tail, preDecls...)
	tail = append(tail, preStmts...)
	tail = append(tail, &ExpressionStatement{Token: tok, Expression: lowered})
	if l.blockMatchSubject != "" && len(guards) > 0 {
		// guardOptionOperands 的失敗臂會寫入 subject；塊 match 下它不 return，
		// 故 tmpAssign 與 match 必須一併關進 `if __bm_N == nil`，否則守衛失敗
		// 後仍會繼續求值 RHS 並跑一次 match。
		tail = []Statement{&ExpressionStatement{
			Token: tok,
			Expression: &IfExpression{
				Token:       tok,
				Condition:   blockMatchNilGuard(tok, l.blockMatchSubject),
				Consequence: &BlockStatement{Token: tok, Statements: tail},
			},
		}}
	}
	stmts = append(stmts, guards...)
	stmts = append(stmts, tail...)
	return &BlockStatement{
		Token:      tok,
		Statements: stmts,
	}
}

// lowerUnwrapAssignToIndex desugars the D9 write-side `?=`:
//
//	a[i] ?= v
//
// into a safe-index match whose option-ness comes from the INDEX READ
// (a[i] returns ?elem, nil on out-of-bounds) — NOT from the RHS value v:
//
//	a[i]: {
//	    nil -> { result = __match_subj_L_C; return }   // OOB → 向上傳播
//	    err -> { result = __match_subj_L_C; return }
//	    -> { it = v }                                    // ok 臂：經 place 寫回元素位置
//	}
//
// The ok arm's non-synthetic `it = v` triggers the place-write rewrite already
// implemented in buildMatchDesugar (D9), turning it into
//
//	__place = v
//	a[__idx] = __place   (IsSynthetic store — idxwrite validator 不誤傷)
//	it = __place
//
// The nil/err propagation arms reference the same `__match_subj_L_C` name that
// buildMatchDesugar synthesizes (it normalizes the index subject to a `?elem`
// temp of exactly that name), so no separate temp is introduced here.
func (l *lowerer) lowerUnwrapAssignToIndex(uas *UnwrapAssignStatement, idx *IndexExpression, resultName string) Statement {
	tok := uas.Token
	// 塊 match 捕捉時改用合成 subject 作為上拋目標，壓掉 jib2tpel 且不 return nil。
	if l.blockMatchSubject != "" {
		resultName = l.blockMatchSubject
	}

	// 若所在函式沒有 option 結果參數，無法向上傳播越界錯誤 ⇒ 報既有口径錯誤。
	// （D9 的 idxwrite ERROR 會在 P2 提供更具體的寫入側指引；此處保持 ?= 既有語義。）
	if resultName == "" {
		if uas.DeprecatedOp == "" {
			l.p.saveErrorf("jib2tpel", "line %d, column %d: `?=` can only be used inside a function with an option-typed result param", tok.Line, tok.Column)
		}
		return nil
	}

	// 推不出元素型別（基底型別未知）⇒ 無法 place-write，報錯。
	if l.p.inferIndexElemType(idx) == "" {
		l.p.saveErrorf("g1v1b6qi", "line %d, column %d: cannot determine element type of index target `%s` for `?=` write",
			tok.Line, tok.Column, exprToString(uas.Target))
		return nil
	}

	// `a[i] ?= b[j]` / `a[i] ?= s.byte(j)`：RHS 本身也是 option。若直接放進 ok
	// 臂的 `it = <RHS>`，`?T -> T` 的隱式解包會把 nil/err 靜默丟掉 —— 寫入不
	// 發生、錯誤也不上拋（探針 `dst[0] ?= src[9]` 會得到 ok(0) 而非 err）。
	// D11 的「越界即 err、`?=` 負責上拋」要求這裡先用讀側 `?=` 把 RHS 解箱到
	// 純值暫存變數（同一個 resultName / blockMatchSubject 傳播），再把純值寫回
	// place。兩段都復用既有降糖，不新增控制流形狀。
	if inner := ty.StripOptionRaw(l.exprOptionInnerType(uas.Value)); inner != "" && resultName != "" {
		valName := fmt.Sprintf("__place_val_%d_%d", tok.Line, tok.Column)
		valIdent := &Identifier{Token: tok, Value: valName}
		first := &UnwrapAssignStatement{
			Token: tok,
			Name:  valIdent,
			Value: uas.Value,
		}
		// 同一行可能已有別的 `?=` 用到 `__unwrap_<line>`，給讀側一個獨立暫存名。
		savedOverride := l.unwrapTmpOverride
		l.unwrapTmpOverride = fmt.Sprintf("__unwrap_val_%d_%d", tok.Line, tok.Column)
		loweredFirst := l.lowerUnwrapAssign(first)
		l.unwrapTmpOverride = savedOverride
		if loweredFirst != nil {
			second := &UnwrapAssignStatement{
				Token:  tok,
				Target: idx,
				Value:  valIdent,
			}
			if loweredSecond := l.lowerUnwrapAssignToIndex(second, idx, resultName); loweredSecond != nil {
				return &BlockStatement{
					Token:      tok,
					Statements: []Statement{loweredFirst, loweredSecond},
				}
			}
		}
	}

	// subjName 與 buildMatchDesugar 內部歸一化命名一致（__match_subj_L_C），
	// 供傳播臂引用越界 option。
	// 注意：subjName 必須與 resolveSubjectMatch 為索引主體歸一化的名字一致
	//（__match_subj_<line>_<col>），不可覆寫；line+col 已保證同行唯一。
	subjName := fmt.Sprintf("__match_subj_%d_%d", tok.Line, tok.Column)

	// nil / err 傳播臂：result = __match_subj; return（塊 match 下不 return）。
	propagate := func() *BlockStatement {
		stmts := []Statement{
			&LetStatement{
				Token:         tok,
				Name:          &Identifier{Token: tok, Value: resultName},
				Value:         &Identifier{Token: tok, Value: subjName},
				IsPropagation: true,
			},
		}
		stmts = append(stmts, blockMatchTail(l.blockMatchSubject, tok)...)
		return &BlockStatement{Token: tok, Statements: stmts}
	}
	arms := []MatchArm{
		{
			Condition:     &Identifier{Token: tok, Value: "nil"},
			Body:          propagate(),
			IsBlockBody:   true,
			Pos:           posFromToken(tok),
			SkipItBinding: true,
		},
		{
			Condition:     &Identifier{Token: tok, Value: "err"},
			Body:          propagate(),
			IsBlockBody:   true,
			Pos:           posFromToken(tok),
			SkipItBinding: true,
		},
		{
			IsWildcard:  true,
			IsBlockBody: true,
			Pos:         posFromToken(tok),
			Body: &BlockStatement{
				Token: tok,
				Statements: []Statement{
					&LetStatement{
						Token: tok,
						Name:  &Identifier{Token: tok, Value: "it"},
						Value: uas.Value, // 非合成：觸發 place-write 改寫
					},
				},
			},
		},
	}

	sm := &SurfaceMatch{
		Token:   tok,
		Matched: idx,
		Arms:    arms,
	}
	return &ExpressionStatement{Token: tok, Expression: l.p.desugarSurfaceMatchToIfExpr(sm)}
}

// optionInnerOfVar 回傳變數 name 的 option 元素型別（? 後的 T），非 option 回傳 ""。
// 優先查函數作用域（避免跨函數同名區域變數污染），回退查全域 VarTypes。
func (l *lowerer) optionInnerOfVar(name string) string {
	// FuncVarTypeRef already falls back to the global table, so one lookup covers
	// both scopes; the answer is structural, so `?` is never re-scanned by hand.
	if t, ok := l.p.sem.FuncVarTypeRef(l.curFuncName, name); ok && t.IsOption() {
		return t.OptionInner().Raw()
	}
	return ""
}

// exprOptionInnerType 若表達式 e 為 option 值（option 變數、option 回傳呼叫、
// 安全索引），回傳其元素型別（?T 的 T）；否則回傳 ""。
// 算術 Infix 一律視為 option 值（溢位回傳 ?i64），以相容既有溢位傳播語意。
func (l *lowerer) exprOptionInnerType(e Expression) string {
	switch v := e.(type) {
	case *Identifier:
		return l.optionInnerOfVar(v.Value)
	case *CallExpression:
		if inferred := l.p.inferTypeFromCallExpr(v); inferred != "" && ty.IsOptionRaw(inferred) {
			return ty.StripOptionRaw(inferred)
		}
		return ""
	case *IndexExpression:
		if elem := l.p.inferIndexElemType(v); elem != "" {
			return elem
		}
		return ""
	case *InfixExpression:
		return "i64" // 算術溢位 → ?i64
	}
	return ""
}

// isDefinitelyNonOptionVar 回報變數 name 是否「確定為非 option」：僅當其型別在
// 解析期已知且不以 ? 開頭時成立。型別未知（未宣告 / 尚未推斷）或非 option 回傳
// 的呼叫則回傳 false —— 解析期無法判定，交由檢查器處理，避免誤報。
func (l *lowerer) isDefinitelyNonOptionVar(name string) bool {
	if t, ok := l.p.sem.FuncVarTypeRef(l.curFuncName, name); ok {
		return !t.IsOption()
	}
	return false
}

// localDeclaredType 回報 name 在「當前函式作用域」已知的型別：優先函式參數
// （含方法 self），其次 FuncVarTypes 的本函式條目；頂層（curFuncName == ""）才查
// 全域 VarTypes。
//
// 刻意**不做** FuncVarType 的全域回退：全域 VarTypes 會被其他函式的同名參數／
// 區域變數覆寫（std 常見 `c i64` 參數），回退會讓本函式的判斷被別人的型別否決。
// 實測：`f = (b i64, c i64, d i64) { a = b + c / d }` 之後，
// `h = (m i64) { c = y + 1 }` 的捕獲被靜默跳過（`FuncVarType("h","c")` 回退到 f
// 的參數型別 i64），連帶使 `c` 不再是 option、溢出檢查消失、後續 match 走錯臂。
func (l *lowerer) localDeclaredType(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if l.curFuncDef != nil {
		for _, prm := range l.curFuncDef.Parameters {
			if prm != nil && prm.Name == name && prm.Type != nil {
				return typeString(prm.Type), true
			}
		}
	}
	if l.curFuncName != "" {
		if l.p.sem != nil {
			if vars, ok := l.p.sem.FuncVarTypes[l.curFuncName]; ok {
				if t, ok := vars[name]; ok {
					return t.Raw(), true
				}
			}
		}
		return "", false
	}
	if l.p.sem != nil {
		if t, ok := l.p.sem.VarTypes[name]; ok {
			return t.Raw(), true
		}
	}
	return "", false
}

// localDefinitelyNonOption 回報 name 在當前作用域是否「已宣告且非 option」。
func (l *lowerer) localDefinitelyNonOption(name string) bool {
	t, ok := l.localDeclaredType(name)
	return ok && !ty.IsOptionRaw(t)
}

// captureTargetAllowsOption 回報捕獲 pass 是否可以把 s 的目標改寫成 option 變數。
//
// 規則是「已存在的變數不改變其型別」：
//   - 目標在本函式內已出現過（declaredLocals，含參數）→ 只有當它本來就是 option
//     才允許；否則改寫會讓它之前的所有使用（普通值語境）突然拿到 option，
//     且無法把 option 存進已宣告的純量變數（方案 §1：「變數已宣告為非 option
//     型別 → 不碰，交 checker 報」）。
//   - 目標是首次宣告 → 只要型別表沒有明確的非 option 型別（如函式參數）就允許。
//
// 判定刻意不直接讀全域 VarTypes：它會被其他函式的同名參數污染（f 的 `c i64`
// 讓 h 的區域 `c` 被當成已宣告），且方法體在解析期的 curFuncName 與 lowering 期
// 不一致，`v i64 = 0` 的型別可能只落在全域表 —— 兩種情況都會讓判定失準。
func (l *lowerer) captureTargetAllowsOption(s *LetStatement) bool {
	if s == nil || s.Name == nil {
		return false
	}
	name := s.Name.Value
	if l.declaredLocals[name] {
		t, ok := l.localDeclaredType(name)
		return ok && ty.IsOptionRaw(t)
	}
	if s.Type == nil && l.localDefinitelyNonOption(name) {
		return false
	}
	return true
}

// rhsDefinitelyNonOption 判斷 ?= 右側是否「明確非 option」且解析期可判定：
// 字面量、或已知非 option 的區域變數。此類右側使用 ?= 無意義，應報錯提示改用 `=`。
// 算術（可溢位）、安全索引、option 變數/呼叫、以及型別未知者皆回傳 false，
// 交由後續（檢查器或既有 desugar）處理。
func (l *lowerer) rhsDefinitelyNonOption(e Expression) bool {
	switch v := e.(type) {
	case *IntegerLiteral, *FloatLiteral, *StringLiteral, *BooleanLiteral:
		return true
	case *Identifier:
		return l.isDefinitelyNonOptionVar(v.Value)
	case *IndexExpression:
		return false // 安全索引回傳 option
	case *InfixExpression:
		// 若整個算術運算「明確為 plain」（所有葉節點皆為字面量或已知非 option
		// 區域變數、且不含任何呼叫），則結果必為非 option（如 10+20、x+1 且
		// x 為 i64），使用 ?= 無意義，應報錯提示改用 `=`。只要任一運算元是 option
		// 變數、未知型別變數或呼叫（可能回傳 option），即視為「可能 option」，
		// 算術 ?= 的右側（如 `a ?= b - c`）：?= 要求以 option（溢出）模式求值 RHS，
		// 溢出時向上傳播錯誤；故即使運算元皆為已知非 option 變數（b/c 為 i64），也算
		// 「可能為 option」（運算本身可能溢出 → 回傳 %option），不應在此報錯。
		// 僅「純字面」算術（如 `v ?= 10 + 20`，編譯期常量永遠不溢出、?= 無意義）
		// 才保留報錯提示改用 `=`。
		if l.infixAllLiterals(v) {
			return true
		}
		return false
	case *CallExpression:
		return false // 回傳型別解析期不可判定，交由檢查器
	}
	return false
}

// infixDefinitelyPlain 遞迴判斷算術運算是否「明確為非 option」：僅由字面量與
// 已知非 option 區域變數組成、且不含任何呼叫 / option 運算元時成立。只要出現
// 未知型別變數、option 變數或函式呼叫（可能回傳 option），即回傳 false。
func (l *lowerer) infixDefinitelyPlain(e Expression) bool {
	switch v := e.(type) {
	case *IntegerLiteral, *FloatLiteral, *StringLiteral, *BooleanLiteral:
		return true
	case *Identifier:
		return l.isDefinitelyNonOptionVar(v.Value)
	case *InfixExpression:
		return l.infixDefinitelyPlain(v.Left) && l.infixDefinitelyPlain(v.Right)
	case *CallExpression:
		return false // 呼叫可能回傳 option
	}
	return false
}

// infixAllLiterals 遞迴判斷算術運算是否「全為字面量」（不含任何識別符或呼叫）。
// 純字面算術（如 10 + 20、1 * 3 - 2）為編譯期常量，永不溢出，故 `?=` 對其無意義；
// 一旦出現任何變數或呼叫（運算可能溢出），即應放行 ?= 以啟用溢出傳播。
func (l *lowerer) infixAllLiterals(e Expression) bool {
	switch v := e.(type) {
	case *IntegerLiteral, *FloatLiteral, *StringLiteral, *BooleanLiteral:
		return true
	case *InfixExpression:
		return l.infixAllLiterals(v.Left) && l.infixAllLiterals(v.Right)
	}
	return false
}

// guardOptionOperands 遍歷 ?= 右側表達式，為其中的 option 運算元注入傳播守衛。
// 僅在「算術運算元」位置直接解包 option 運算元；呼叫引數位置則只遞迴下探
// （如 f(b+c) 的 b+c），不直接解包作為引數的 option 變數（交給被呼叫方或 ?= 豁免）。
// 守衛語句會被附加到 *guards。
//
// C6（H0 不動 AST）：回傳**新的**表達式樹（不變的子樹共享原節點），不原地改寫
// 傳入的 e —— H0 直出（lowerUnwrapAssignToHIR）呼叫時不得污染原 AST。walk 路徑
// 用回傳值替換 uas.Value 即可，行為一致。
func (l *lowerer) guardOptionOperands(e Expression, guards *[]Statement, resultName string, tok lexer.Token) Expression {
	switch v := e.(type) {
	case *InfixExpression:
		left := l.guardInArithmetic(v.Left, guards, resultName, tok)
		right := l.guardInArithmetic(v.Right, guards, resultName, tok)
		if left == v.Left && right == v.Right {
			return v
		}
		return &InfixExpression{Token: v.Token, Left: left, Operator: v.Operator, Right: right}
	case *CallExpression:
		changed := false
		args := make([]Expression, len(v.Arguments))
		for i := range v.Arguments {
			args[i] = l.guardInCallArg(v.Arguments[i], guards, resultName, tok)
			if args[i] != v.Arguments[i] {
				changed = true
			}
		}
		if !changed {
			return v
		}
		return &CallExpression{Token: v.Token, Function: v.Function, Arguments: args}
	}
	return e
}

func (l *lowerer) guardInArithmetic(e Expression, guards *[]Statement, resultName string, tok lexer.Token) Expression {
	if inner := l.exprOptionInnerType(e); inner != "" {
		return l.bindAndGuard(e, inner, guards, resultName, tok)
	}
	switch v := e.(type) {
	case *InfixExpression:
		left := l.guardInArithmetic(v.Left, guards, resultName, tok)
		right := l.guardInArithmetic(v.Right, guards, resultName, tok)
		if left == v.Left && right == v.Right {
			return v
		}
		return &InfixExpression{Token: v.Token, Left: left, Operator: v.Operator, Right: right}
	case *CallExpression:
		changed := false
		args := make([]Expression, len(v.Arguments))
		for i := range v.Arguments {
			args[i] = l.guardInCallArg(v.Arguments[i], guards, resultName, tok)
			if args[i] != v.Arguments[i] {
				changed = true
			}
		}
		if !changed {
			return v
		}
		return &CallExpression{Token: v.Token, Function: v.Function, Arguments: args}
	}
	return e
}

func (l *lowerer) guardInCallArg(e Expression, guards *[]Statement, resultName string, tok lexer.Token) Expression {
	switch v := e.(type) {
	case *InfixExpression:
		left := l.guardInArithmetic(v.Left, guards, resultName, tok)
		right := l.guardInArithmetic(v.Right, guards, resultName, tok)
		if left == v.Left && right == v.Right {
			return v
		}
		return &InfixExpression{Token: v.Token, Left: left, Operator: v.Operator, Right: right}
	case *CallExpression:
		changed := false
		args := make([]Expression, len(v.Arguments))
		for i := range v.Arguments {
			args[i] = l.guardInCallArg(v.Arguments[i], guards, resultName, tok)
			if args[i] != v.Arguments[i] {
				changed = true
			}
		}
		if !changed {
			return v
		}
		return &CallExpression{Token: v.Token, Function: v.Function, Arguments: args}
	}
	return e
}

// bindAndGuard 將 option 運算元 e 綁定到新臨時變數 __opt_N，並注入傳播守衛
// （nil/err → result = __opt_N; return），回傳替代原運算元的臨時變數識別符。
func (l *lowerer) bindAndGuard(e Expression, inner string, guards *[]Statement, resultName string, tok lexer.Token) Expression {
	l.optTmpSeq++
	name := fmt.Sprintf("__opt_%d", l.optTmpSeq)
	tmpIdent := &Identifier{Token: tok, Value: name}
	l.p.setVarType(name, "?"+inner)
	// __opt_N = e
	*guards = append(*guards, &LetStatement{
		Token: tok,
		Name:  tmpIdent,
		Type:  &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: inner}},
		Value: e,
	})
	// match __opt_N { nil/err -> result = __opt_N; return }
	*guards = append(*guards, l.makePropagateGuard(name, resultName, tok))
	return tmpIdent
}

// makePropagateGuard 產生 `match __opt_N { nil/err -> result = __opt_N; return }`
// 的 Statement（ok 情況落空、繼續執行後續語句）。
func (l *lowerer) makePropagateGuard(optName, resultName string, tok lexer.Token) Statement {
	tmpIdent := &Identifier{Token: tok, Value: optName}
	mkBody := func() *BlockStatement {
		// 這是編譯器合成的「整個 option 結構」上拋拷貝：nil/err 的 tag 與
		// err.err 載荷必須原樣轉送，內部型別 T 與結果參數的 T 可以不同
		// （`dst[i] ?= s.byte(k)` 在 `?message` 函式裡，守衛的 __opt_N 是
		// ?i64、結果是 ?message）。與 lowerUnwrapAssign / lowerUnwrapAssignToIndex
		// 的傳播臂同口徑，一律標 IsPropagation，讓 checker 跳過 15w45dqk
		// 的型別比對；過去只在塊 match 下標記，會讓一般函式裡的 option
		// 運算元守衛誤報「cannot assign ?i64 value to ?message variable」。
		assignRes := &LetStatement{
			Token:         tok,
			Name:          &Identifier{Token: tok, Value: resultName},
			Value:         tmpIdent,
			IsPropagation: true,
		}
		stmts := []Statement{assignRes}
		stmts = append(stmts, blockMatchTail(l.blockMatchSubject, tok)...)
		return &BlockStatement{Token: tok, Statements: stmts}
	}
	nilArm := MatchArm{
		Condition:     &Identifier{Token: tok, Value: "nil"},
		Body:          mkBody(),
		IsBlockBody:   true,
		Pos:           posFromToken(tok),
		SkipItBinding: true,
	}
	errArm := MatchArm{
		Condition:     &Identifier{Token: tok, Value: "err"},
		Body:          mkBody(),
		IsBlockBody:   true,
		Pos:           posFromToken(tok),
		SkipItBinding: true,
	}
	sm := &SurfaceMatch{Token: tok, Matched: tmpIdent, Arms: []MatchArm{nilArm, errArm}}
	saved := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	desugared := l.p.desugarSurfaceMatchToIfExpr(sm)
	l.p.curFuncName = saved
	if desugared == nil {
		return &BlockStatement{Token: tok, Statements: mkBody().Statements}
	}
	return &ExpressionStatement{Token: tok, Expression: desugared}
}
