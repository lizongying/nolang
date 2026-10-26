package parser

import (
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// semviews.go — 從 AST 單獨重建「編譯器會消費的衍生事實」。
//
// 背景（2026-10-26 架構決定）：AST 快取（Tier-0 內嵌 std blob + Tier-1 使用者
// 檔案快取）**只存 `Program`**，不存任何衍生事實。理由不是體積，是邊界：
//
//   - 快取本質上是「結構化的原始碼」。衍生事實（推斷型別、索引證明、簽名表）
//     是**某個版本的編譯器**對這段原始碼的結論；把它們烘進 blob 等於把編譯器
//     版本也烘進去，而快取鍵（內容哈希 + `ASTVersion` + salt）看不到編譯器
//     建置。任何一條只在編譯器內部改變的推導規則，都會讓舊 blob 帶著舊結論
//     被新二進位「命中」，且**不報錯**。
//   - 這個 blob 未來要作為**分發格式**：分發物必須是原始碼的等價物，不能夾帶
//     某次編譯的副產品。這也要求它不綁死 `encoding/gob`。
//   - 同一件事有兩個真相來源（樹 vs 副表）本身就會漂移；能從樹算出來的東西
//     不該有第二份。
//
// 因此 `CachedProgram` 只剩 `Prog`，解碼後由 `BuildSemViews` 重走一遍把事實
// 補回來。**這條路徑必須與「新鮮解析」逐位元組等價**，門禁見
// `semviews_test.go` 的 `TestBuildSemViewsMatchesFreshParse`（600 檔語料）。
//
// 三個已踩過的坑（每一個都是靜默失效，`no ast` 看不出來）：
//
//  1. **寫入目標**：`ParseProgram` 設 `program.Sem = p.sem`，所有推導都經
//     `l.p.sem` 寫出。重建時必須把 parser 的 `sem` 指回**解碼後 program 的
//     Sem**，否則事實全寫進另一張表，重建看起來「成功」但樹上什麼都沒有。
//     `SetExternSignatures` 也會往 `p.sem.ExternFuncSigs` 同步快照，所以
//     重指必須在 `cfg.Apply` **之前**。
//  2. **nil 副表**：`SemanticContext.typeFacts` / `nodeFacts` 未導出，gob 不搬，
//     解碼後是 nil；而所有 setter 對 nil 表**靜默 no-op**。必須先顯式安裝。
//  3. **同檔簽名表**：捕獲 pass 用 `returnTypeFromCallExpr`，它讀 parser 實例的
//     `funcSignatures` / `methodSignatures`。重建必須按 `decl.go` 的登記規則重填
//     —— 包括「`#{buildin}` 樁照樣登記」、「`=`-形式方法落在 `funcSignatures`
//     全名鍵（裸名鍵被改名刪除）」這兩條反直覺規則，詳見
//     `rebuildLocalSignatures` 的註釋。

// SemRebuildConfig 是一次解析中**原始碼以外**的全部輸入。
//
// 它存在的唯一目的是讓「新鮮解析」與「從 blob 重建」共用同一份配置：兩邊都
// 只呼叫 `cfg.Apply(p)`，因此不可能一邊加了新旗標而另一邊忘了跟。這不是理論
// 風險 —— Tier-0 就曾因為生成器用裸 `parser.New` 烤 blob、建置路徑卻注入跨模組
// 簽名表，而烤出一份建置路徑永遠不會產生的 AST（`std/txt.no` 的
// `txt.replace-n` 因此返回 "1one" 而非 "1 two one"，見 checker/stdast.go）。
type SemRebuildConfig struct {
	// Filename 是 `filepath.Base` 之後的檔名（診斷用，也餵 currentModuleName）。
	Filename string
	// AllowAnonymousFnType 對應 `(*Parser).AllowAnonymousFnType`。
	AllowAnonymousFnType bool
	// FuncSigs / MethodSigs 是跨檔案與內建的**結果型別**表（parser 只吃結果）。
	FuncSigs   map[string][]string
	MethodSigs map[string][]string
	// StructFields 是 struct 名 → 欄位名 → 型別字串。
	StructFields map[string]map[string]string
	// EnumVariants 是跨模組枚舉變體表（match desugar 需要）。
	EnumVariants map[string][]string
}

// Apply 把配置裝到 p 上。**新鮮解析與重建都必須走這裡**，不要各自複製一份。
//
// 守衛條件與原本 build.parseFile 的寫法逐字對應（`externFuncSigs != nil ||
// externStructFields != nil` 才注入），因為「注入了空表」與「完全沒注入」在
// `ExternSignatureFingerprint` 與 Tier-1 salt 上是兩種不同狀態，不能合併。
func (c SemRebuildConfig) Apply(p *Parser) {
	if p == nil {
		return
	}
	p.Filename = c.Filename
	p.AllowAnonymousFnType = c.AllowAnonymousFnType
	if c.FuncSigs != nil || c.MethodSigs != nil || c.StructFields != nil {
		p.SetExternSignatures(c.FuncSigs, c.MethodSigs, c.StructFields)
	}
	if len(c.EnumVariants) > 0 {
		p.SetExternEnumVariants(c.EnumVariants)
	}
}

// BuildSemViews 從 prog 的語法樹重建所有衍生事實（`TypeFacts` / `NodeFacts` /
// `Sem.IdxLocalTypes`）。
//
// 呼叫方只需交出「當初產生這棵樹的配置」。本函式自建一個不帶原始碼的 parser
// 來跑既有分析 pass，**不碰呼叫方的 parser**，也不呼叫 ParseProgram。
//
// ⚠️ 已知缺口（刻意公開，不要靜默）：本函式只重建「有現成純後置 pass 可跑」的
// 事實。`Sem` 裡其餘的符號表（`VarTypes` / `FuncVarTypes` / `EnumVariants` /
// `EnumVariantPayload` / `UnionMembers` / …）是**解析宣告時**寫下的，仍隨 blob
// 一起傳輸 —— 它們不是 `TypeFacts`/`NodeFacts` 那類「樹上推不出第二種答案」的
// 事實，而是解析器自己的符號表，重算等於把宣告 pass 再實現一遍。要讓 blob
// 徹底只剩語法，得先讓宣告 pass 可獨立重跑，那是另一件事。
func BuildSemViews(prog *Program, cfg SemRebuildConfig) {
	if prog == nil {
		return
	}
	if prog.Sem == nil {
		prog.Sem = NewSemanticContext()
	}
	// 坑 2：nil 副表上的 setter 全是靜默 no-op。
	if prog.Sem.typeFacts == nil {
		prog.Sem.typeFacts = NewTypeFacts()
	}
	if prog.Sem.nodeFacts == nil {
		prog.Sem.nodeFacts = NewNodeFacts()
	}

	// 走樹不需要原始碼：New 只會從空 lexer 取一個 EOF token 把 ring buffer
	// 的游標初始化好，之後所有分析都是純 AST 走訪。
	p := New(lexer.New(""))
	// 坑 1：先重指 sem，再 Apply —— SetExternSignatures 會往 p.sem 寫快照。
	p.sem = prog.Sem
	cfg.Apply(p)

	// 坑 3：同檔簽名表。
	p.rebuildLocalSignatures(prog)

	// 索引證明與「安全索引專用」局部型別表（`Sem.IdxLocalTypes` +
	// `NodeFacts.IndexInBounds`）。與 ParseProgram 走同一入口，故兩條路徑的
	// 結果一致；`precomputeIdxLocalTypes` 只讀樹、寫 p.idxLocalTypes。
	p.prepareInBoundsMetadata(prog)

	// `?T` 推斷（`TypeFacts.InferredType` + `FuncVarTypes`）。與 ParseProgram
	// 的 `prepareInferredTypes` 是同一個 analysisOnly 走查。
	p.prepareInferredTypes(prog)
}

// rebuildLocalSignatures 依 `decl.go` 的登記規則，從樹上的函式定義重填
// 本檔的 `funcSignatures` / `methodSignatures`。
//
// 規則是從實測（std/fs.no、std/txt.no 逐函式對照「是否登記 × 是否 `#{buildin}`
// 樁 × 方法形式」）導出的，要點：
//   - **`#{buildin}` 樁照樣登記** —— decl.go 的登記點（:1820-1838）不查
//     BuiltinStub。fs.no 的 `read` 樁最終不在表裡，不是因為它是樁，而是因為
//     同檔後面的 `file.read` 方法在註冊當下用的裸名也是 "read"，**覆寫**了樁
//     條目，隨後 ：56-58 的改名又把裸名鍵刪除——淨效果是樁條目被沖掉。跳過樁
//     的話 `txt.byte`（樁方法，實測有登記）就會漏。
//   - **運算子方法（Name 以 `[` 開頭，如 `[]txt.join`）不登記** —— 解析期不走
//     parseFunctionDefinition 的登記點（實測未登記）。
//   - **三種登記去向**：
//     1. `:`-形式方法（`ColonSyntax`，decl.go:2013）：建構時 `IsMethodDef` 已
//     true、`Name` 已是全名 → `methodSignatures[fullName]`（:1831）。
//     2. `=`-形式方法（`txt.byte = (self txt, …)`，decl.go:13）：註冊點執行時
//     `IsMethodDef` **尚未**置 true（:52 才設）→ 進了 `funcSignatures[裸名]`，
//     隨後 :56-58 刪除裸名、改名為全名。淨效果 = 刪裸名鍵 + 落
//     `funcSignatures[fullName]`。只查最終的 `IsMethodDef` 把它路由進
//     `methodSignatures` 的話，`.byte(...)` 這類隱式 self 呼叫的回傳型別
//     就查不到（實測 std/txt.no 三個 `c = .byte(i+k) - target.byte(k)` 的
//     `?i64` 推斷全部消失）。
//     3. 普通函式 → `funcSignatures[Name]`（:1836）。
//
// ⚠️ 沖刷語意依賴**文件順序**（`file.read` 必須在其後刪掉 `read` 樁條目），
// walkNodes 按語句順序走訪頂層宣告，與解析順序一致。
func (p *Parser) rebuildLocalSignatures(prog *Program) {
	if p == nil || prog == nil {
		return
	}
	walkNodes(prog, func(v interface{}) {
		fd, ok := v.(*FunctionDefinition)
		if !ok || fd == nil || fd.Name == "" {
			return
		}
		// 運算子方法：解析期無登記點。
		if fd.Name[0] == '[' {
			return
		}
		results := DeclaredResults(fd)
		if len(results) == 0 {
			return
		}
		rets := make([]string, 0, len(results))
		for _, r := range results {
			rets = append(rets, typeString(r.Type))
		}
		if fd.IsMethodDef {
			if fd.ColonSyntax {
				if p.methodSignatures == nil {
					p.methodSignatures = make(map[string][]string)
				}
				p.methodSignatures[fd.Name] = rets
				return
			}
			// `=`-形式方法：模仿「先以裸名登記 → 改名」的淨效果。裸名 =
			// 全名第一個點之後的部分（struct 名不含點）。
			if _, bare, ok := strings.Cut(fd.Name, "."); ok && p.funcSignatures != nil {
				delete(p.funcSignatures, bare)
			}
		}
		if p.funcSignatures == nil {
			p.funcSignatures = make(map[string][]string)
		}
		p.funcSignatures[fd.Name] = rets
	})
}
