package checker

// 本檔案提供 RunAllLints 共享入口，集中所有 Validate* 校驗器的調用
// 與嚴重性映射。被以下三條路徑共用：
//   1. build/transpiler.go vetMode 分支（no vet CLI 命令）
//   2. lsp/vet.go VetFile（nolang-lsp vet CLI 命令）
//   3. lsp/server.go publishDiagnostics（編輯器實時診斷）
//
// 新增 lint 時只需在 lintSteps 加一條，三條路徑同時生效，避免漂移。
//
// Stage 6A 機械重構：RunAllLints 的 39 步串接拆為 runLintXxx 函數 +
// lintSteps 表，並提供三個入口：
//   - RunAllLints:       全部步（原行為，順序不變）
//   - RunSurfaceLints:   surface=true 步（S2 糖形態/命名/宣告/注解）
//   - runTHIRStageChecks: surface=false 步（THIR 型別軌，暫未導出）
// 共享尾巴（File 歸因 / Strict 升級 / std 過濾 / 排序）由 finalizeLintResults
// 統一執行。多重集不變式：RunAllLints ≡ RunSurfaceLints ⊎ runTHIRStageChecks。

import (
	"fmt"
	"github.com/lizongying/nolang/i18n"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// LintSeverity 表示 lint 結果的嚴重程度。
type LintSeverity string

const (
	LintError   LintSeverity = "error"
	LintWarning LintSeverity = "warning"
	LintHint    LintSeverity = "hint"
)

// LintResult 是一條 lint 診斷結果，包含位置、嚴重性、來源和消息。
type LintResult struct {
	Line      int
	Column    int
	EndColumn int
	Severity  LintSeverity
	Source    string
	Message   string
	TraceID   string
	// File 是該診斷所屬的源碼檔案路徑。在 vet 合併模式下，merged 程式
	// 包含來自多個模組的語句，File 用於區分每條 lint 歸屬哪個檔案。
	// 為空時由調用者使用默認路徑（如輸入檔案路徑）。
	File string
}

// LintOptions 控制 RunAllLints 的行為。
type LintOptions struct {
	// SourcePath 是當前編譯/驗證的源碼檔案路徑，
	// 用於 ValidateEmbedAnnotations 和 ValidateExportSymbols。
	SourcePath string
	// RootDir 是源碼所在目錄，用於 ValidateUndefinedVars、
	// ValidateDependencyImports 和 ValidateFuncArgs 的路徑解析。
	RootDir string
	// Strict 為 true 時，所有 warning/hint 升級為 error。
	Strict bool
	// LightweightMode 為 true 時，額外執行 CheckUnresolvedModuleCalls
	// （輕量版未解析模組函數呼叫檢查）。LSP 路徑（不做模組合併）
	// 應設為 true；no vet 路徑已有 build/module_check.go 的完整版，
	// 應設為 false（預設）。
	LightweightMode bool
	// MainFileVarNames 記錄主程式（非導入模組）的頂層變數名集合。
	// 用於 ValidateUnusedVars：當 program 是合併了導入模組的 merged
	// program 時（no vet 路徑），只檢查主程式的變數是否未使用，
	// 跳過導入模組的常量（如 std/log 的 LEVEL-ERROR）。
	// 為 nil 時（LSP 路徑），program 未合併模組，檢查所有頂層變數。
	MainFileVarNames map[string]bool
	// SkipTypeChecks 為 true 時跳過 ValidateTypes 和 ValidateFuncArgs。
	// 用於 no vet 路徑：這兩項檢查已在模組合併前作為 hard error 執行過
	// （transpiler.go line 1641/1647），合併後對 merged program 重複執行
	// 會因標準庫模組函數體內的局部型別推斷不完整而產生誤報（如
	// rand(kp-state) 被誤判為 []byte 而非 i64）。
	// LSP 路徑不做模組合併，program 是原始的，需要執行這兩項檢查。
	SkipTypeChecks bool
	// thirView 是 THIR 軌 lint 步驟的型別副表視圖（6D 切換）。
	// 由 runLintPipeline 在有待跑 THIR 軌步時經 BuildTHIRView 統一構建
	// （含 H0 副作用隔離），不導出——外部調用者無需關心。
	thirView *THIRView
	// StmtFileMap 已徹底移除：語句的來源檔歸因現在存於
	// Program.Sem side-table（SemanticContext.SourceFileOf，S5），
	// RunAllLints 直接以 program.Sem.SourceFileOf(stmt) 讀取。
}

// LintIneffectiveOverflow 報告「無效」的 #{overflow = ...} 註解：其管轄陳述不含
// 整數溢出運算（例如整行僅字串拼接，中劃線是拼接而非減法），註解對溢出毫無作用，
// 應建議刪除。採 WARNING 層級（不強制），與「未標註整數運算」的 ERROR 區分。
//
// 採用 OverflowAnnotationRelevance 的保守（超集）語意：relevant 集合中的陳述未必
// 真的需要註解，但不在其中的陳述註解必定無效——因此本 lint 只會對「確定無效」的
// 註解發出，不會誤報有效的 overflow 註解。
//
// sourcePath 為當前被 vet 的主檔路徑。兩重作用：
//  1. 只檢查主檔自己的語句：vet 合併模式下 program 含導入模組語句的副本，
//     這些副本在合併上下文中的型別推斷與檔案自身 vet 時不一致（如
//     number.no 的 f64 運算在本檔被推為整數運算、在別檔合併副本被推為
//     浮點），會產生自相矛盾的誤報；目錄模式下每個檔案都會作為主檔被
//     vet，導入副本直接跳過即可，不丟失覆蓋。
//  2. File 回退歸因：結果必須攜帶 File（語句自帶的 SourceFile），否則
//     下方案號範圍回退歸因會把導入模組的行號對到主檔某函式的行距範圍，
//     造成診斷被誤歸因到被 vet 的檔案。
func LintIneffectiveOverflow(program *parser.Program, sourcePath string) []LintResult {
	if program == nil {
		return nil
	}
	relevant, governed := OverflowAnnotationRelevance(program)
	if relevant == nil {
		return nil
	}
	// isImportedCopy 報告語句是否來自導入模組的合併副本（非主檔自身）。
	// SourceFile 為空表示主檔語句（未合併路徑，如 LSP）；與 sourcePath
	// 同檔的副本也需檢查（合併程序可能同時帶入主檔自身的模組副本）。
	isImportedCopy := func(file string) bool {
		if file == "" {
			return false
		}
		if sourcePath == "" {
			return true
		}
		absFile, err1 := filepath.Abs(file)
		absMain, err2 := filepath.Abs(sourcePath)
		if err1 != nil || err2 != nil {
			return file != sourcePath
		}
		return absFile != absMain
	}
	msg := i18n.T("0cccc2ei", "the statement governed by this #{overflow=...} annotation contains no integer arithmetic (e.g. the line only concatenates strings), so it can never overflow; consider removing the annotation.")
	var results []LintResult
	// 獨立成行註解節點：governed 直接給出管轄陳述。
	for as, gov := range governed {
		if gov == nil || !relevant[gov] {
			// 來源檔優先取註解節點自身；回退到被管轄陳述，再回退到主檔。
			file := program.Sem.SourceFileOf(as)
			if file == "" && gov != nil {
				file = program.Sem.SourceFileOf(gov)
			}
			if isImportedCopy(file) {
				continue
			}
			if file == "" {
				file = sourcePath
			}
			results = append(results, LintResult{
				Line:     as.Pos().Line,
				Column:   as.Pos().Column,
				Severity: LintWarning,
				Source:   "nolang-overflow-ineffective",
				Message:  msg,
				File:     file,
			})
		}
	}
	// 附加（IDENT 起始）註解：遍歷所有陳述，檢查其 side-table 中的 overflow 條目。
	var collect func(stmts []parser.Statement)
	var collectIf func(ie *parser.IfExpression)
	var collectSurfaceMatch func(sm *parser.SurfaceMatch)
	collect = func(stmts []parser.Statement) {
		for _, stmt := range stmts {
			if stmt == nil {
				continue
			}
			if program.Sem != nil {
				for _, e := range program.Sem.AnnotationsOf(stmt) {
					if e != nil && e.Key == "overflow" && !e.Trailing {
						if !relevant[stmt] {
							file := program.Sem.SourceFileOf(stmt)
							if isImportedCopy(file) {
								continue
							}
							if file == "" {
								file = sourcePath
							}
							results = append(results, LintResult{
								Line:     e.Pos().Line,
								Column:   e.Pos().Column,
								Severity: LintWarning,
								Source:   "nolang-overflow-ineffective",
								Message:  msg,
								File:     file,
							})
						}
					}
				}
			}
			switch v := stmt.(type) {
			case *parser.FunctionDefinition:
				if v.Body != nil {
					collect(v.Body.Statements)
				}
			case *parser.BlockStatement:
				collect(v.Statements)
			case *parser.ForStatement:
				if v.Body != nil {
					collect(v.Body.Statements)
				}
			case *parser.ExpressionStatement:
				// if/match 臂体同样是行注解的作用域，必须下钻（与
				// OverflowAnnotationRelevance.collect 保持一致）。
				if ie, ok := v.Expression.(*parser.IfExpression); ok {
					collectIf(ie)
				}
				// S1：糖形態 match 是 SurfaceMatch；臂體亦為行註解作用域。
				if sm, ok := v.Expression.(*parser.SurfaceMatch); ok {
					collectSurfaceMatch(sm)
				}
			case *parser.LetStatement:
				if ie, ok := v.Value.(*parser.IfExpression); ok {
					collectIf(ie)
				}
				if sm, ok := v.Value.(*parser.SurfaceMatch); ok {
					collectSurfaceMatch(sm)
				}
			}
		}
	}
	collectIf = func(ie *parser.IfExpression) {
		if ie == nil {
			return
		}
		if ie.Consequence != nil {
			collect(ie.Consequence.Statements)
		}
		if ie.Alternative != nil {
			collect(ie.Alternative.Statements)
		}
	}
	// collectSurfaceMatch 下鑽糖 match 的臂體（與 checker.OverflowAnnotationRelevance
	// 及 range_index_lint 保持一致：臂體是行註解的作用域）。
	collectSurfaceMatch = func(sm *parser.SurfaceMatch) {
		if sm == nil {
			return
		}
		walkSurfaceMatchArmBodies(sm, nil, func(stmts []parser.Statement) { collect(stmts) })
	}
	collect(program.Statements)
	return results
}

// ──────────────────────────────────────────────────────────────────────────────
// Stage 6A: lintSteps 表 + runLintXxx 步驟函數
// ──────────────────────────────────────────────────────────────────────────────

// lintStep 描述一條 lint 檢查步驟。
type lintStep struct {
	// name 是步驟的唯一名稱（kebab-case），用於診斷與等價測試。
	name string
	// surface 標記軌道：true = RunSurfaceLints（S2 糖形態/命名/宣告/注解），
	// false = runTHIRStageChecks（THIR 型別軌）。
	surface bool
	// run 執行該步驟，返回其診斷。不得修改 program（純只讀）。
	run func(program *parser.Program, opts LintOptions) []LintResult
}

// lintSteps 是全部 lint 檢查步驟，順序即 RunAllLints 的串接順序（不可重排）。
// surface=true 的步驟構成 RunSurfaceLints；surface=false 構成 runTHIRStageChecks。
// 切分判據：需要操作數型別推斷（inferExprType / TypeFacts）⇒ THIR 軌；
// 只需糖形態/命名/宣告/注解/作用域形態 ⇒ Surface 軌。
var lintSteps = []lintStep{
	{"types", false, runLintTypes},
	{"naming", true, runLintNaming},
	{"async-naming", true, runLintAsyncNaming},
	{"unused-vars", true, runLintUnusedVars},
	{"undefined-vars", true, runLintUndefinedVars},
	{"uninit-output-params", true, runLintUninitOutputParams},
	{"multi-option-results", true, runLintMultiOptionResults},
	{"unassigned-returns", true, runLintUnassignedReturns},
	{"embed-annotations", true, runLintEmbedAnnotations},
	{"interface-impl", false, runLintInterfaceImpl},
	{"interface-merge", true, runLintInterfaceMerge},
	{"use-keyword", true, runLintUseKeyword},
	{"use-alias", true, runLintUseAlias},
	{"redundant-type-annot", true, runLintRedundantTypeAnnot},
	{"duplicate-vars", true, runLintDuplicateVars},
	{"dependency-imports", true, runLintDependencyImports},
	{"export-symbols", true, runLintExportSymbols},
	{"string-concat", false, runLintStringConcat},
	{"str-index-complexity", false, runLintStrIndexComplexity},
	{"list-dir-unfiltered", true, runLintListDirUnfiltered},
	{"deprecated-calls", true, runLintDeprecatedCalls},
	{"hex-case", true, runLintHexCase},
	{"literal-conversion", true, runLintLiteralConversion},
	{"func-args", false, runLintFuncArgs},
	{"non-exhaustive-match", true, runLintNonExhaustiveMatch},
	{"option-comparison", false, runLintOptionComparison},
	{"str-ordering", false, runLintStrOrdering},
	{"char-arith", false, runLintCharArith},
	{"print-format", true, runLintPrintFormat},
	{"cross-module-type-refs", true, runLintCrossModuleTypeRefs},
	{"unresolved-module-calls", true, runLintUnresolvedModuleCalls},
	{"int-overflow", false, runLintIntOverflow},
	{"div-by-zero", false, runLintDivByZero},
	{"ineffective-overflow", true, runLintIneffectiveOverflow},
	{"unhandled-overflow", false, runLintUnhandledOverflow},
	{"unhandled-index", false, runLintUnhandledIndex},
	{"catch-all-arms", true, runLintCatchAllArms},
	{"range-index-loops", true, runLintRangeIndexLoops},
	{"range-self-ellipsis", true, runLintRangeSelfEllipsis},
	{"index-write", false, runLintIndexWrite},
	{"parser-warnings", true, runLintParserWarnings},
}

// ── THIR 軌（surface=false）─────────────────────────────────────────────────────

// runLintTypes — 1. 型別錯誤（no vet 路徑已在合併前執行，跳過以避免 merged program 誤報）
func runLintTypes(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	if !opts.SkipTypeChecks {
		for _, e := range ValidateTypesTHIR(program, opts.thirView) {
			results = append(results, LintResult{
				Line: e.Line, Column: e.Column,
				Severity: LintError, Source: "nolang-type-checker",
				Message: e.Message, TraceID: e.TraceID,
			})
		}
	}
	return results
}

// runLintInterfaceImpl — 9. 接口實現
func runLintInterfaceImpl(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateInterfaceImplementationTHIR(program, opts.thirView) {
		endCol := u.Column
		if u.EndColumn > 0 {
			endCol = u.EndColumn
		}
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, EndColumn: endCol, File: u.File,
			Severity: LintWarning, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintInterfaceMerge — 9b. 接口合併（interface merging）
//
// 合併項寫在接口體內（`serialize { json⏎ noml }`）。本步把
// checker.ResolveInterfaces 的診斷轉成 lint 結果：未知父接口 / 循環 /
// 合併後簽名衝突 ⇒ ERROR；仍用舊的「前置列表」寫法 ⇒ WARNING（提示改用體內
// 形，`no fmt` 會自動改寫）。
//
// 結果帶 File，符合 std 診断歸因過濾（finalizeLintResults）。
func runLintInterfaceMerge(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	if program == nil || program.Sem == nil {
		return nil
	}
	_, diags := ResolveInterfaces(program)
	for _, d := range diags {
		endCol := d.Column
		if d.EndColumn > 0 {
			endCol = d.EndColumn
		}
		results = append(results, LintResult{
			Line: d.Line, Column: d.Column, EndColumn: endCol, File: d.File,
			Severity: LintError, Source: "nolang-lint",
			Message: d.Message, TraceID: d.TraceID,
		})
	}
	// 前置列表兼容寫法：`db enter, leave { ... }` 仍能解析，但規範形是體內。
	for _, stmt := range program.Statements {
		id, ok := stmt.(*parser.InterfaceDefinition)
		if !ok || len(id.Implements) == 0 {
			continue
		}
		if !interfaceUsesLeadingList(program, id, opts) {
			continue
		}
		results = append(results, LintResult{
			Line: id.Token.Line, Column: id.Token.Column,
			EndColumn: id.Token.Column + len(id.Name),
			File:      program.Sem.SourceFileOf(id),
			Severity:  LintWarning, Source: "nolang-lint",
			Message: i18n.T("r0360g4t", "interface '%s': parent interfaces written as a leading list; write one name per line inside the body instead", id.Name),
			TraceID: "r0360g4t",
		})
	}
	return results
}

// interfaceUsesLeadingList 報告接口是否採用了舊的「前置列表」寫法。
//
// AST 不記錄合併項的位置——複用 Implements 正是為了不改 gob / `std_ast.bin`
// 格式——因此在這裡回讀來源檔的那幾行：前置列表必然寫在「接口名」與 `{`
// 之間（`db enter, leave {`），而體內形的 `{` 緊跟接口名。
// 讀不到檔案時靜默跳過：這只是收斂寫法的建議，不值得為它引入新的 AST 狀態。
func interfaceUsesLeadingList(program *parser.Program, id *parser.InterfaceDefinition, opts LintOptions) bool {
	if id == nil || id.Token.Line <= 0 {
		return false
	}
	path := program.Sem.SourceFileOf(id)
	if path == "" {
		path = opts.SourcePath
	}
	if path == "" {
		return false
	}
	candidates := []string{path}
	if !filepath.IsAbs(path) && opts.RootDir != "" {
		candidates = append(candidates, filepath.Join(opts.RootDir, path))
	}
	var data []byte
	for _, c := range candidates {
		if b, err := os.ReadFile(c); err == nil {
			data = b
			break
		}
	}
	if data == nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	start := id.Token.Line - 1
	// 從接口名所在行起，直到遇見 `{`（多行前置列表：`db enter,` / `leave {`）。
	for i := start; i >= 0 && i < len(lines) && i <= start+8; i++ {
		line := lines[i]
		if c := strings.IndexByte(line, ';'); c >= 0 {
			line = line[:c] // `;` 是行註解
		}
		if i == start && id.Token.Column > 0 && id.Token.Column-1 < len(line) {
			line = line[id.Token.Column-1:]
			if strings.HasPrefix(line, id.Name) {
				line = line[len(id.Name):]
			}
		}
		brace := strings.IndexByte(line, '{')
		if brace < 0 {
			if strings.TrimSpace(line) != "" {
				return true // 接口名與 `{` 之間出現了其他內容
			}
			continue
		}
		return strings.TrimSpace(line[:brace]) != ""
	}
	return false
}

// runLintStringConcat — 16. 字串拼接提示（建議用 - 代替 +）
func runLintStringConcat(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateStringConcatTHIR(program, opts.thirView) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintStrIndexComplexity — 16b. str/txt 的 s[i] 码点下标 O(n) 性能告警
func runLintStrIndexComplexity(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateStrIndexComplexityTHIR(program, opts.thirView) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintWarning, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintFuncArgs — 18. 函數引數型別校驗（no vet 路徑已在合併前執行，跳過以避免 merged program 誤報）
func runLintFuncArgs(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	if !opts.SkipTypeChecks {
		for _, u := range ValidateFuncArgsTHIR(program, opts.RootDir, opts.thirView) {
			results = append(results, LintResult{
				Line: u.Line, Column: u.Column,
				Severity: LintError, Source: "nolang-type-checker",
				Message: u.Message, TraceID: u.TraceID,
			})
		}
	}
	return results
}

// runLintOptionComparison — 19c. option 值直接比較
func runLintOptionComparison(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateOptionComparisonTHIR(program, opts.thirView) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-type-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintStrOrdering — 19d. 關係運算子（< <= > >=）的多字元字串運算元
func runLintStrOrdering(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateStrOrderingTHIR(program, opts.thirView) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintError, Source: "nolang-type-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintCharArith — 19e. char 與非數值運算元（bool/f64/結構體/容器）的算術
func runLintCharArith(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateCharArithTHIR(program, opts.thirView) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintError, Source: "nolang-type-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintIntOverflow — 20c. 整數四則運算溢出提示
func runLintIntOverflow(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateIntOverflowTHIR(program, opts.thirView) {
		results = append(results, LintResult{
			File: u.File, // 節點級來源檔；空則由下方行號範圍回退歸因
			Line: u.Line, Column: u.Column,
			Severity: LintHint, Source: "nolang-overflow",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintDivByZero — 20c-bis. 整數除法/取模的字面零除數（編譯硬錯誤）
func runLintDivByZero(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, d := range ValidateDivByZeroTHIR(program, opts.thirView) {
		results = append(results, LintResult{
			File: d.File,
			Line: d.Line, Column: d.Column,
			Severity: LintError, Source: "nolang-div-zero",
			Message: d.Message, TraceID: d.TraceID,
		})
	}
	return results
}

// runLintUnhandledOverflow — 20d. option 生命週期（D2）——產生處 HINT、未消費 ERROR。
// 嚴重度逐條依 TraceID 決定（ovfhndld→HINT，optunuse→ERROR）。
func runLintUnhandledOverflow(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateUnhandledOverflowTHIR(program, opts.SourcePath, opts.thirView) {
		// 攜帶 ValidateResult.File（陳述所屬模組），避免合併 std vet 時
		// 誤歸因。File 為空則回退到 opts.SourcePath。
		file := u.File
		if file == "" {
			file = opts.SourcePath
		}
		sev := LintHint
		if u.TraceID == optUnuseTraceID {
			sev = LintError
		}
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: sev, Source: "nolang-overflow",
			File:    file,
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintUnhandledIndex — 20e. 未處理的越界索引（D2：產生處 HINT）
func runLintUnhandledIndex(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateUnhandledIndexTHIR(program, opts.SourcePath, opts.thirView) {
		file := u.File
		if file == "" {
			file = opts.SourcePath
		}
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintHint, Source: "nolang-index",
			File:    file,
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintIndexWrite — 20h. 下標直寫 `a[i] = v`（D9）→ ERROR `idxwrite`
func runLintIndexWrite(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateIndexWriteTHIR(program, opts.SourcePath, opts.thirView) {
		file := u.File
		if file == "" {
			file = opts.SourcePath
		}
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-index-write",
			File:    file,
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// ── Surface 軌（surface=true）──────────────────────────────────────────────────

// runLintNaming — 2. 命名規範
// 局部变量/函数名必须只含小写字母、数字与中划线（大写首字母仅允许用于
// 模块级全局常量/变量）。这是强制语言规则，故升级为 ERROR。
func runLintNaming(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, w := range ValidateNaming(program) {
		results = append(results, LintResult{
			Line: w.Line, Column: w.Column, File: w.File,
			Severity: LintError, Source: "nolang-lint",
			Message: w.Message, TraceID: w.TraceID,
		})
	}
	return results
}

// runLintAsyncNaming — 3. async 命名規範
func runLintAsyncNaming(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, w := range ValidateAsyncNaming(program) {
		results = append(results, LintResult{
			Line: w.Line, Column: w.Column, File: w.File,
			Severity: LintWarning, Source: "nolang-lint",
			Message: w.Message, TraceID: w.TraceID,
		})
	}
	return results
}

// runLintUnusedVars — 4. 未使用變數
func runLintUnusedVars(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateUnusedVars(program, opts.MainFileVarNames) {
		endCol := u.Column
		if u.EndColumn > 0 {
			endCol = u.EndColumn
		}
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, EndColumn: endCol, File: u.File,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintUndefinedVars — 5. 未定義變數
func runLintUndefinedVars(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateUndefinedVars(program, opts.RootDir, opts.SourcePath) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintUninitOutputParams — 6. 未初始化的 ?T 輸出參數
func runLintUninitOutputParams(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateUninitOutputParams(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintError, Source: "nolang-type-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintMultiOptionResults — 6.5 一個函式最多只能有一個 ?T 返回參數。
// 已接在 ValidateTypes 尾端（hard error）；這裡只在 SkipTypeChecks 為 true
// 時補跑（no vet CLI 合併路徑），LSP 不會重複回報。
func runLintMultiOptionResults(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	if opts.SkipTypeChecks {
		for _, e := range ValidateMultiOptionResults(program) {
			results = append(results, LintResult{
				Line: e.Line, Column: e.Column, File: e.File,
				Severity: LintError, Source: "nolang-type-checker",
				Message: e.Message, TraceID: e.TraceID,
			})
		}
	}
	return results
}

// runLintUnassignedReturns — 7. 未賦值的命名返回參數
func runLintUnassignedReturns(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, w := range ValidateUnassignedReturns(program) {
		results = append(results, LintResult{
			Line: w.Line, Column: w.Column, File: w.File,
			Severity: LintWarning, Source: "nolang-type-checker",
			Message: w.Message, TraceID: w.TraceID,
		})
	}
	return results
}

// runLintEmbedAnnotations — 8. embed 註解校驗
func runLintEmbedAnnotations(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, e := range ValidateEmbedAnnotations(program, opts.SourcePath) {
		results = append(results, LintResult{
			Line: e.Line, Column: e.Column,
			Severity: LintError, Source: "nolang-lint",
			Message: e.Message, TraceID: e.TraceID,
		})
	}
	return results
}

// runLintUseKeyword — 10. use 關鍵字提示
func runLintUseKeyword(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateUseKeyword(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintUseAlias — 11. use as 別名提示
func runLintUseAlias(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateUseAlias(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintRedundantTypeAnnot — 12. 冗餘型別標註
func runLintRedundantTypeAnnot(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateRedundantTypeAnnotation(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintDuplicateVars — 13. 重複變數
func runLintDuplicateVars(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateDuplicateVars(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintError, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintDependencyImports — 14. 依賴導入校驗
func runLintDependencyImports(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateDependencyImports(program, opts.RootDir) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintError, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintExportSymbols — 15. 導出符號校驗
func runLintExportSymbols(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateExportSymbols(program, opts.SourcePath) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintListDirUnfiltered — 16c. fs.list-dir 遞歸/刪除未過濾 "."/".."
func runLintListDirUnfiltered(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateListDirUnfiltered(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintWarning, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintDeprecatedCalls — 16d. 棄用函數呼叫提示。
// 複合賦算運算符（`+=` 一族）按用戶定案是 WARNING；棄用函數是 HINT。
func runLintDeprecatedCalls(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateDeprecatedCalls(program) {
		endCol := u.Column
		if u.EndColumn > 0 {
			endCol = u.EndColumn
		}
		// 複合賦算運算符（`+=` 一族）按用戶定案是 WARNING；棄用函數是 HINT。
		sev := LintHint
		if u.TraceID == deprecatedAssignTraceID {
			sev = LintWarning
		}
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, EndColumn: endCol, File: u.File,
			Severity: sev, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintHexCase — 17. hex 字面量大寫提示
func runLintHexCase(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateHexCase(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintLiteralConversion — 17b. 数字字面量直接转换提示
func runLintLiteralConversion(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateLiteralConversion(program) {
		endCol := u.Column
		if u.EndColumn > 0 {
			endCol = u.EndColumn
		}
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, EndColumn: endCol, File: u.File,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintNonExhaustiveMatch — 19b. option match 穷尽性
func runLintNonExhaustiveMatch(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateNonExhaustiveMatch(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintError, Source: "nolang-type-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintPrintFormat — 19. print 格式字串校驗
func runLintPrintFormat(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidatePrintFormat(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-format-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintCrossModuleTypeRefs — 20. 跨模組型別前綴校驗
func runLintCrossModuleTypeRefs(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, u := range ValidateCrossModuleTypeRefs(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, File: u.File,
			Severity: LintError, Source: "nolang-type-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}
	return results
}

// runLintUnresolvedModuleCalls — 20b. 輕量版未解析模組函數呼叫檢查（僅 LSP 路徑）
func runLintUnresolvedModuleCalls(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	if opts.LightweightMode {
		for _, u := range CheckUnresolvedModuleCalls(program) {
			results = append(results, LintResult{
				Line: u.Line, Column: u.Column,
				Severity: LintError, Source: "nolang-lint",
				Message: u.Message, TraceID: u.TraceID,
			})
		}
	}
	return results
}

// runLintIneffectiveOverflow — 20c-ter. 無效 overflow 註解（WARNING）。
// 結果自帶 File（語句 SourceFile 或主檔回退），不参与行號範圍回退歸因。
func runLintIneffectiveOverflow(program *parser.Program, opts LintOptions) []LintResult {
	return LintIneffectiveOverflow(program, opts.SourcePath)
}

// runLintCatchAllArms — 20f. option match 的空 `->` 兜底臂（D9）。
// 嚴重度逐條依 TraceID 決定：arm-unreach→HINT，arm-should→WARNING。
func runLintCatchAllArms(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, a := range ValidateCatchAllArms(program) {
		sev := LintWarning
		if a.TraceID == armUnreachTraceID {
			sev = LintHint
		}
		results = append(results, LintResult{
			Line: a.Line, Column: a.Column,
			Severity: sev, Source: "nolang-match-arm",
			Message: a.Message, TraceID: a.TraceID,
		})
	}
	return results
}

// runLintRangeIndexLoops — 20g. 手動按下標遍歷容器 → HINT `rngidx`
func runLintRangeIndexLoops(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, r := range ValidateRangeIndexLoops(program, opts.SourcePath) {
		file := r.File
		if file == "" {
			file = opts.SourcePath
		}
		results = append(results, LintResult{
			Line: r.Line, Column: r.Column,
			Severity: LintHint, Source: "nolang-range-index",
			File:    file,
			Message: r.Message, TraceID: r.TraceID,
		})
	}
	return results
}

// runLintRangeSelfEllipsis — 20g-bis. 區間上界以隱式 self 的 `.field` 開頭 → HINT `rng-ellipsis`
func runLintRangeSelfEllipsis(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, r := range ValidateRangeSelfEllipsis(program, opts.SourcePath) {
		file := r.File
		if file == "" {
			file = opts.SourcePath
		}
		results = append(results, LintResult{
			Line: r.Line, Column: r.Column,
			Severity: LintHint, Source: "nolang-range",
			File:    file,
			Message: r.Message, TraceID: r.TraceID,
		})
	}
	return results
}

// runLintParserWarnings — 21. parser 警告
func runLintParserWarnings(program *parser.Program, opts LintOptions) []LintResult {
	var results []LintResult
	for _, warnMsg := range program.Warnings {
		var line, col int
		// Strip optional "filename:" prefix so Sscanf can match "line N, column M:"
		stripped := warnMsg
		if idx := strings.Index(stripped, "line "); idx > 0 {
			stripped = stripped[idx:]
		}
		fmt.Sscanf(stripped, "line %d, column %d:", &line, &col)
		// Extract message after "line N, column M: " prefix
		msg := stripped
		if prefix := fmt.Sprintf("line %d, column %d:", line, col); strings.HasPrefix(msg, prefix) {
			msg = strings.TrimPrefix(msg, prefix)
			msg = strings.TrimPrefix(msg, " ")
		}
		// Strip "[W_CODE] " tag prefix for cleaner display
		if strings.HasPrefix(msg, "[W_") {
			if endBracket := strings.Index(msg, "] "); endBracket >= 0 {
				msg = msg[endBracket+2:]
			}
		}
		results = append(results, LintResult{
			Line: line, Column: col,
			Severity: LintHint, Source: "nolang-parser",
			Message: msg,
		})
	}
	return results
}

// ──────────────────────────────────────────────────────────────────────────────
// 共享尾巴 + 管線 + 入口
// ──────────────────────────────────────────────────────────────────────────────

// finalizeLintResults 完成 File 歸因、Strict 升級、std 過濾與排序。
// 尾巴對每條診斷獨立作用（多重集可分配），故 RunAllLints ≡
// finalize(surface) ⊎ finalize(thir) 在多重集意義下成立。
func finalizeLintResults(results []LintResult, program *parser.Program, opts LintOptions) []LintResult {
	// 用語句的 SourceFile 欄位填充每條結果的 File 欄位。
	// 構建行號範圍 → 檔案路徑的映射：對 FunctionDefinition 記錄
	// [startLine, endLine]；對其他語句記錄單行 startLine。
	//
	// ⚠️ 僅在「單一檔案」程式（LSP 路徑，所有語句屬同一個 SourceFile 或都無
	// SourceFile）下才做行號範圍回退歸因。合併多模組（no vet 主檔）程式裡，不同
	// 檔案的行號座標會重疊：一條空 File 的診斷若只憑行號去比對各模組的 [start,end]
	// 範圍，極易比到「恰好同行號的另一個模組函式」，被誤歸因到錯誤檔案（常見為
	// 報在「函式定義行」或「無關陳述」上，正是 option-unified.md 記錄的
	// 「位置不可靠」症狀）。多模組程式下，每條診斷必須自帶正確的 File（由各
	// Validate* 從節點 SourceFile 填入）；行號回退只會幫倒忙，故直接跳過。
	singleFile := true
	{
		type lineRange struct {
			startLine int
			endLine   int
			file      string
		}
		var ranges []lineRange
		var refFile string
		for _, stmt := range program.Statements {
			file := program.Sem.SourceFileOf(stmt)
			if file == "" {
				continue
			}
			if refFile == "" {
				refFile = file
			} else if file != refFile {
				singleFile = false
			}
			startLine := stmt.Pos().Line
			endLine := stmt.EndPos().Line
			if endLine < startLine {
				endLine = startLine
			}
			ranges = append(ranges, lineRange{startLine, endLine, file})
		}
		if singleFile && len(ranges) > 0 {
			for i := range results {
				if results[i].File != "" {
					continue
				}
				line := results[i].Line
				// 在行號範圍中查找匹配的檔案
				for _, r := range ranges {
					if line >= r.startLine && line <= r.endLine {
						results[i].File = r.file
						break
					}
				}
			}
		}
	}

	// --strict：升級 warning/hint 為 error
	if opts.Strict {
		for i := range results {
			if results[i].Severity != LintError {
				results[i].Severity = LintError
			}
		}
	}

	// 檢查目標不是 std 檔時，丟棄歸屬到 std 的診斷：
	// std 有獨立閘門（`no vet src/std`，目前 0 error），而 merged 模式下 std 自帶的
	// `#{overflow}` 註解會因為 lowering 重建 AST 節點、導致註解掛在已被替換掉的
	// 舊節點上（註解隨節點走，見 parser/annotations.go）查不到而產生大量假警報——實測 src/std/arr.no:68 已存在
	// `#{overflow=wrap}`，merged vet 仍報 :69 未處理溢位；同一份 std 單獨 vet 卻是
	// 乾淨的。把 std 內部診斷算進使用者/測試檔的 vet 結果只會誤導，故在此過濾。
	// merged 模式（多檔程式）下跨檔行號無法歸因（singleFile=false → File 為空）
	// 的診斷也一併過濾——這些幾乎都是 std 內部洩漏。單檔程式（LSP/測試）不走此分支。
	if !isStdPath(opts.SourcePath) {
		filtered := results[:0]
		for _, r := range results {
			if isStdPath(r.File) {
				continue
			}
			if !singleFile && r.File == "" {
				continue
			}
			filtered = append(filtered, r)
		}
		results = filtered
	} else if !singleFile {
		// std 目标的 merged 程式：vet src/std/X 时，依赖闭包里的其它 std 档
		// （std/err.no、std/bigint.no…）会随合并被重检一遍，其自带诊断若并入
		// 目标，就形成「串档假 ERROR」——同一行号被贴到不同档名、且远超目标 EOF。
		// 每个 std 档都在 `no vet src/std` 遍历中有自己的独立闸门，跨档诊断不该
		// 重复计入。故仅保留归属「目标自身 std 档」的诊断，丢弃其它 std 档与
		// 无法归因（File 为空）的泄漏诊断（对齐 cross_module_lint_position 不变量 3）。
		selfRel := stdRelPath(opts.SourcePath)
		filtered := results[:0]
		for _, r := range results {
			if r.File == "" {
				continue
			}
			if isStdPath(r.File) && stdRelPath(r.File) != selfRel {
				continue
			}
			filtered = append(filtered, r)
		}
		results = filtered
	}

	// 按 file, line, column 排序，方便閱讀
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].File != results[j].File {
			return results[i].File < results[j].File
		}
		if results[i].Line != results[j].Line {
			return results[i].Line < results[j].Line
		}
		return results[i].Column < results[j].Column
	})

	return results
}

// runLintPipeline 按 filter 執行 lintSteps 並 finalize。
// filter 為 nil 時執行全部步；否則只執行 filter(step)==true 的步。
// 步驟執行順序恒為 lintSteps 的宣告順序。
// 6D 切換：有待跑 THIR 軌步（!step.surface）時，先經 BuildTHIRView 構建
// THIR 型別副表視圖（H0 副作用已隔離——lint 步驟看到的 program 狀態與
// legacy 路徑完全一致），注入 opts.thirView 供 THIR 变体使用。
func runLintPipeline(program *parser.Program, opts LintOptions, filter func(lintStep) bool) []LintResult {
	if program == nil {
		return nil
	}
	// Lazy-build THIR view: only when at least one THIR-track step will run.
	// BuildTHIRView isolates H0 side effects (h0Diags + Sem name-map writes),
	// so every lint step sees the same program state as the legacy path.
	needView := false
	for _, step := range lintSteps {
		if filter != nil && !filter(step) {
			continue
		}
		if !step.surface {
			needView = true
			break
		}
	}
	if needView && opts.thirView == nil {
		opts.thirView = BuildTHIRView(program)
	}
	var results []LintResult
	for _, step := range lintSteps {
		if filter != nil && !filter(step) {
			continue
		}
		results = append(results, step.run(program, opts)...)
	}
	return finalizeLintResults(results, program, opts)
}

// RunAllLints 對 program 執行全部 lint 校驗，返回匯總結果。
// 調用者可根據 LintOptions.Strict 決定是否將 warning/hint 視為錯誤。
//
// 注意：ValidateTypes、ValidateFuncArgs、ValidateUninitOutputParams 在
// build/transpiler.go Compile 流程中已被調用並可能作為 hard error 阻止編譯，
// 但在 vet 模式（不走 Compile 的 LSP 路徑）下需要在此重新執行。
// 為保證三條路徑一致，這裡不遺漏地全部執行——同一 program 重複調用
// Validate* 是安全的（純只讀，不改 AST）。
func RunAllLints(program *parser.Program, opts LintOptions) []LintResult {
	return runLintPipeline(program, opts, nil)
}

// RunSurfaceLints 只執行 surface=true 的步（S2 糖形態/命名/宣告/注解/作用域形態）。
// 目標掛載點：H0 降糖之前（L0 Surface AST 階段）。設計意圖見
// docs/design/nolang-parser-lowering-invariants.md §3.5 裁決 3。
func RunSurfaceLints(program *parser.Program, opts LintOptions) []LintResult {
	return runLintPipeline(program, opts, func(s lintStep) bool { return s.surface })
}

// runTHIRStageChecks 只執行 surface=false 的步（THIR 型別軌：inferExprType /
// TypeFacts 依賴項）。目標掛載點：H0 之後、Checker-on-THIR 階段。
// 暫未導出——6C 影子雙跑期間供內部對拍使用。
func runTHIRStageChecks(program *parser.Program, opts LintOptions) []LintResult {
	return runLintPipeline(program, opts, func(s lintStep) bool { return !s.surface })
}

// isStdPath 報告路徑是否屬於標準函式庫（std）。std 可能以絕對路徑
// （/…/src/std/X.no）、相對路徑（src/std/X.no）或正規化後的模組形式（std/X.no）
// 出現，三種都要認得，否則過濾會漏。
func isStdPath(p string) bool {
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "std/") {
		return true
	}
	return strings.Contains(p, "/std/")
}

// stdRelPath 把 std 路徑正規化為可比較的相对形式「std/<rest>」，讓絕對路徑
// （/…/src/std/net/http.no）、相對路徑（src/std/net/http.no）與邏輯模組形式
// （std/net/http.no）指向同一檔時比對相等。非 std 路徑原樣返回。
func stdRelPath(p string) string {
	if p == "" {
		return p
	}
	q := filepath.ToSlash(p)
	if i := strings.Index(q, "/std/"); i >= 0 {
		return q[i+1:]
	}
	return q
}

// CountBySeverity 統計指定嚴重性的結果數量。
func CountBySeverity(results []LintResult, sev LintSeverity) int {
	count := 0
	for _, r := range results {
		if r.Severity == sev {
			count++
		}
	}
	return count
}

// HasErrors 判斷結果中是否存在 error 級別的診斷。
func HasErrors(results []LintResult) bool {
	for _, r := range results {
		if r.Severity == LintError {
			return true
		}
	}
	return false
}
