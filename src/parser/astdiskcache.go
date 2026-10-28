package parser

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ASTVersion is the version of the *parse output*, not of the compiler. It is
// folded into every on-disk cache key, so bumping it invalidates all stale
// entries at once without scanning the directory.
//
// # C9 policy (S6, 2026-10-21)
//
// Lowering no longer runs inside ParseProgram (S1 flip, design §22), so a
// cached blob is a pure function of (source text, parse-time inputs). That makes
// the bump rule two-sided, and the two sides are NOT symmetric:
//
//	MUST bump — anything that changes what ParseProgram RETURNS, or how that
//	tree is serialized:
//	  1. surface AST shape: node types, node fields, and any field value that
//	     comes from the source text or from parse-time inference;
//	  2. the gob wire layout of CachedProgram / its deterministic shadow
//	     (astcache_det.go), including a new node-keyed fact table that is
//	     added without a detU32* mirror — the blob then decodes with that
//	     fact silently missing;
//	  3. a parse-time side table that HIR consumes: annotations, platform keys,
//	     embed data, generic params, IndexInBounds, InferredType, ModuleConst,
//	     SourceFile, ModuleOwner, ...
//
//	MUST NOT bump — anything strictly downstream of the cache:
//	  lowering (H0 sugar elimination, match representation), monomorphisation,
//	  assembly renaming / module prefixing / call rewriting, clone isolation.
//	  These change HIR, never the AST blob. They are versioned by
//	  hir.HIRVersion instead (documented only: there is no HIR disk cache yet).
//
// The old rule was "bump for any change to the lowering pass". It was correct
// while lowering ran inside ParseProgram, and it is wrong now in both
// directions: it throws the whole cache away for HIR-only changes, and it
// obscures the invariant that actually matters — a decoded blob must lower to
// the same HIR as a fresh parse. That invariant is what G7 measures
// (TestCachedASTLowersToIdenticalHIR); the bump rule is only the mechanism that
// keeps it true across compiler builds.
//
// The entries below are history. Everything before 2026-10-21.2 was written
// under the old rule, so several of them record lowering changes that would not
// require a bump today; they are kept verbatim because they document what the
// stale-blob failure looked like each time.

// 2026-10-11.1 — walkProgramNodes switched from per-unique-node to
// per-occurrence indexing (see the comment on walkProgramNodes): the old
// encoding attached nodeSem entries to the wrong nodes after a gob round-trip.
// The uint32 keys keep their type, so old entries would still DECODE — they
// must be invalidated by this bump or a stale blob would restore a wrong
// mapping instead of failing.
// 2026-10-11.2 — CachedProgram now implements GobEncoder/GobDecoder, so its
// serialization changed from "gob of the struct" to "gob of a deterministic
// shadow with key-sorted maps" (see astcache_det.go). Old entries decode into
// a different wire shape and must be invalidated.
// 2026-10-12.1 — the unified option rule (D6/D1) changed what ParseProgram
// PRODUCES without changing the wire shape: `x = a + b` (and `a - b`, `a * b`,
// and annotated arithmetic) now gets a synthesized `?T` type annotation from
// the lowering pass, and `#{overflow=…}` no longer downgrades it to a plain
// int. The added `matchArm.valueCtx` field is unexported, so gob ignores it and
// old blobs still decode — but they decode into the PRE-CHANGE lowering result.
// A stale entry therefore silently resurrects the old semantics (measured: a
// `no vet` false-positive `ovf-int-default` on `main` as the sole function with
// a 2-statement body, reproducible only on a cache HIT and gone under
// NOLANG_AST_CACHE_OFF=1). Bump so every stale entry is invalidated.
//
// 2026-10-12.3：D7-R1 反轉 —— 重新載入 bounds.go 的「可證明越界安全」豁免
// （p.inBoundsIdx / isSafeIndexBase 早退 / AnalyzInBoundsForFunc 合併 /
// checker + fmt 三方位接）。這會改變 lowering 的輸出形狀（無數取不再被降級成
// option、不再插入 __cap_N / match 區塊），屬必 bump 的 lowering 變更。
//
// 2026-10-13.2：15c72ccd 之後的提交（c721fe6c 裸 match 降級 + */** 速寫、
// 762ea045/33055cc5 解析期推斷區域變數與函式回傳型別）都改變了 ParseProgram
// 的**輸出形狀**卻沒有 bump——按上方 rule of thumb 這就該 bump。影響面只到
// Tier 1（入口檔 + 本地模組，見 build.parseFile / CompileTarget）：開發期設了
// NOLANG_STD_SRC 時，std 模組走 parseEmbeddedProgram，既不讀也不寫 Tier 1；
// Tier 0 blob 由 make 的 STDSIG_GEN 依賴（含 src/parser/*.go）自動重烤，加上
// ExternSignatureFingerprint 折入了 ASTVersion，雙重自洽，無需靠本次 bump。
// （更正：先前把 `no test test/std/ed25519.no` 的簽名 S 全 0 歸因於陳舊 Tier 1
// blob，為誤判——該症狀是 std 模組的 MIR 降級缺陷：lowerIf 在 arm body 走安全
// 索引後未替當前 block 接上 terminator，已由 src/mir/hir2mir.go 修正。當時 scratch
// 檔的內容是新增的，Tier 1 必然 miss；sc-muladd 所在的 std 又不經過 Tier 1，所以
// NOLANG_AST_CACHE_OFF 兩側看到的其實是不同世代的 bin/no（平行 session 在中間
// 重建過），那個 A/B 不成立。）
//
// 2026-10-14.1：補齊 parse 期區域變數型別推斷的兩個缺口，改變 ParseProgram 的
// 輸出（更多 LetStatement 帶上推斷型別，進而使 inferOptionArith 為更多算術綁定
// 合成 `?T`）：
//   - 裸標量字面量（`j = 1` / `f = 1.5` / `s = 'x'` / `b = true` / `c = "a"`）
//     的推斷型別原本只寫進 stmt.Type（IsInferred）而未登記語義表，導致
//     signedIntOperandType 對「推斷出的有號整數區域變數」查不到型別；
//   - 識別符賦值 `x = y` 未傳播 y 的已知型別，`i = 0; j = i` 這條鏈斷在 j。
//
// 兩者疊加使 `p-opt = j + 1` 推不出 `?i64`，ok arm 永不命中而落到 fallback。
// 實測 nojq dec-valid 的科學記數分支：`1e3`/`1E3`/`1.5e2`/`1e0` 被誤判 invalid，
// 且只在 Tier 1 cache HIT 時復現、`NOLANG_AST_CACHE_OFF=1` 即消失——正是本檔
// 開頭 rule of thumb 描述的陳舊 blob 症狀。按該規則 bump 以失效所有舊條目。
//
// 2026-10-15.1：D7 最終定案（兩層分離）—— isSafeIndexBase 不再依據 p.inBoundsIdx
// 對「可證明越界安全」的讀取早退，in-bounds 讀取照常降級為 option（用戶側必須
// 解包；內部消除改由 codegen/MIR 負責）。這改變 lowering 輸出形狀（更多 __cap_N
// / match 區塊），屬必 bump 的 lowering 變更。
// 2026-10-15.3：D9 place-match 寫入（多行形式；match 臂以换行分隔）—— lowering
// 在 statement 語境的安全索引 match ok 臂內把 `it = v` 改寫為寫回序列，並新增
// AssignExpression.IsSynthetic 欄位（gob 編碼形狀改變，舊緩存缺該欄位會讓
// idxwrite 檢查漏跳合成節點，故必須 bump）。
//
// 2026-10-16.2：`matchArm` → 導出 `MatchArm`，且**全部 18 個欄位改為導出**
// （含原本刻意保持未導出的 `valueCtx`）。動機：`SurfaceMatch.Arms` 的載荷在別的包
// （checker / formatter / `no ast`）完全不可見——這是「checker 前移」的根因，
// 見 docs/design/nolang-parser-lowering-invariants.md §15。
//
// **本次刻意不 bump**（維持 2026-10-16.1）。理由：本檔的 bump 判準是「改變
// ParseProgram 的**產出**」（rule of thumb：「a semantics-only change would
// otherwise be masked by cached blobs」），而本次是純 Go 可見性改名——
// ParseProgram 的 AST 逐位元組不變（由 532 檔語料門禁
// TestSkipLoweringMatchesDefaultPath 與 parser/hir/mir 全套綠燈證明）。
// gob/mm-ast 的線形確實變了（gob 只編碼導出欄位），但 `MatchArm` 值只存在於
// `SurfaceMatch` 內，而 SurfaceMatch 在 ParseProgram 內必被 lowering 展開、
// 永遠不會進 blob（見 MatchArm 的型別註解與 :750 的歷史註解），故沒有任何
// 既有 blob 會含 MatchArm 資料、沒有任何快取會解碼成不同結構。
// ⚠️ **若 S1 讓 SurfaceMatch 存活到序列化，那時必須 bump。**
// 另：把 `parseMatchExpression` 內的既有同名區域型別改名為 `deprecatedMatchArm`，
// 以免在該函式作用域內遮蔽包級 `MatchArm`。
// 2026-10-16.3：InBounds 從 IndexExpression 移至 NodeSemantics side-table，且 bounds
// analysis 在 ParseProgram 前置準備、只讀 AST。這改變 AST/gob shape 與 cached semantic
// metadata；舊 Tier-1 blob 既沒有 side-table proof，又可能把 AST InBounds 欄位解碼丟棄，
// 必須失效，避免 cache HIT 令 HIR 靜默漏掉 FlagInBounds。
//
// 2026-10-16.4：S3 修復——`inferOptionArith` / `synthesizeNullableType` 不再就地寫
// `s.Type = &NullableType{IsInferred: true}`，改把推斷 `?T` 字串記入
// `NodeSemantics.InferredType` side-table。AST 不再帶這個語義事實，舊 blob 若
// 反序列化會得到 `s.Type == nil` + 空的 InferredType，使 ASTToHIR 的 Type ID 變成
// NoID（不再輸出 `?i64`）；必須 bump，否則 cache HIT 會在 24/532 corpus 檔產生
// 與預設路徑不一致的 HIR（parser/dump/s3_breakdown_test.go 的 typeSynth 桶）。
// 2026-10-18.1：S1 翻轉——`ParseProgram()` 的默認路徑**保留 SurfaceMatch**
// （`deferMatchDesugar` 從「展開 match」改為「保留並只跑預檢」），
// `LowerSurface` 不再展開 match，降糖改由 `ASTToHIR` 的 `case *SurfaceMatch`
// 負責。ParseProgram 的**產出**因此改變：match 語句從降糖後的 IfExpression 鏈
// 變成 SurfaceMatch 節點。緩存鍵 = 源碼哈希 + ASTVersion + salt，**不含文件路徑、
// 不含二進制版本**（見 UserASTCache 註釋），所以不 bump 的話：舊二進制（翻轉前）
// 解析並寫入的已降糖 AST blob 會被新二進制命中，`no build` 拿到一棵沒有
// SurfaceMatch 的樹——S1 的行為被靜默退回翻轉前，且這種「假綠」只在緩存命中時
// 出現（NOLANG_AST_CACHE_OFF=1 復測即暴露）。2026-10-16.2 的註釋已預言此場景
// （「若 S1 讓 SurfaceMatch 存活到序列化，那時必須 bump」）。
// 2026-10-21.1：S5 stage 2–4 —— `LetStatement.IsModuleConst`、
// `CommentedNode.SourceFile`/`ModuleOwner`、`FuncSignature.VariadicUnion`/
// `GenericUnion` 共 5 個 AST 欄位刪除，事實遷入 SemanticContext side-table
// （nodeSem）。gob 版面改變：舊 blob 的欄位流會被新 decoder 靜默丟棄，
// 若不 bump，快取命中會讓這些事實「憑空消失」（nodeSem 鏡像雖已補齊，
// 但舊 blob 的 nodeSem 本就沒有這些鍵）。⚠️ stage 2/3 落地時漏了 bump，
// 本次一併作廢全部舊 blob。
// 2026-10-21.2：S5 stage 5 —— `LetStatement.Type` 的裝配期覆寫遷入
// `NodeSemantics.TypeOverride`（detNodeSem 鏡像同步）。嚴格說這次是**新增**
// 欄位（gob 向前相容、零值省略，且快取存的是解析產出、永遠不含 TypeOverride
// 條目），不 bump 也未必出事——但 stale-cache 故障只在 HIT 且難復現，
// bump 的代價是一次性作廢，故按慣例保守 bump。
//
// **2026-10-21 S6：本次刻意不 bump（維持 2026-10-21.2）** —— 這是新政策
// （上方 C9）的第一個適用者。S6 只做三件事：把 bump 規則改寫成兩向清單、
// 新增 `hir.HIRVersion` 承擔降糖/裝配形狀版本、補 G7 門禁
// （`TestCachedASTLowersToIdenticalHIR`）。三件都不改變 ParseProgram 的產出、
// 也不改變 gob 版面，按新規則不 bump。若這裡反而 bump，就等於用行動否定剛寫下
// 的政策——HIR 側的形狀變更永遠不該作廢 AST blob。
// 2026-10-22.2：同日的兩處 lowering 變更叠加（皆改變 ParseProgram 的輸出形狀）：
//  1. 捕獲 pass 的 `wasDeclared` 刪除 `localDeclaredType` 回退（見 2026-10-22.1
//     段）：解析期推斷型別不再冒充「已宣告」，捕獲目標現在一律補 `a ?T = nil`
//     預宣告。
//  2. 算術根的 inner 推導補正（lowering.go maybeCaptureOptionAssign）：結果型別
//     ≠ i64 的算術捕獲（浮點 `a * b` 兩側皆 ?f64 ⇒ ?f64；str 拼接 ⇒ ?str）不再
//     硬編碼 i64——舊行為會把預宣告弄成錯誤的 `c ?i64 = nil`（實測
//     tests/arr-test-tmp.no：checker 據此誤報格式串 'g' vs '?i64'）。整數算術
//     （任意寬度）結果 payload 一律 i64 的慣例不變（實測 aes-vectors：覆寫成
//     ?byte 會讓 codegen 的 option wrap 發出 trunc i64→i8 而 LLVM 驗證失敗）。
//
// 2026-10-22.3：移除 `#{intrinsic}` 註解——連帶刪除 `FunctionDefinition.Intrinsic`
// 這個 AST 欄位（parser/annotation.go 不再寫入、`no ast` 的 dump 欄位同步消失、
// checker 的 i3k422u3 抑制改由 `#{buildin}` 獨力承擔）。gob 版面改變：舊 blob 的
// Intrinsic 欄位會被新 decoder 靜默丟棄（該旗標只影響 checker 的返回值校驗、
// 不進 HIR），但按 C9 規則「刪除節點欄位」即屬必 bump（surface AST shape 改變），
// 2026-10-23.1：THIR Stage 2 —— RTFlags / OpeningBraceComment 從
// `NodeSemantics` 遷到獨立的 `FmtTrivia` 表（`Program.FmtTrivia`）。gob 版面
// 改變：`detNodeSem` 少了兩個欄位，舊 blob 解出的 NodeSemantics 會帶著已被
// 廢棄的位元；且新表**刻意不入 detProgram**（formatter-only trivia，fmt/LSP
// 一律 fresh parse 而從不讀快取），故解碼後 `Program.FmtTrivia` 為 nil。按
// C9 規則「detNodeSem 欄位增刪」即屬必 bump，作廢全部舊 blob。
//
// 2026-10-23.2：THIR Stage 3 —— `RawAnnotations` / `Annotations` /
// `PlatformKeys` / `GenericParams` 從 `NodeSemantics` 遷到獨立的 `AnnTable`
// （`SemanticContext.anns`）。gob 版面改變：`detNodeSem` 少四個欄位，註解改由
// `detCached.Anns`（occurrence 編號鍵）承載。🔴 這裡**必須** bump 且是雙向的：
// 舊 blob 解出的註解表為空，而註解驅動平台過濾 / 溢位處理 / 索引守衛——丟失
// 不報錯，只會靜默生成錯誤的程式。作廢全部舊 blob。
//
// 2026-10-23.3：THIR Stage 4 —— `InferredType` / `InferredByOptionArith` /
// `TypeOverride` 從 `NodeSemantics` 遷到獨立的 `TypeFacts` 表
// （`SemanticContext.typeFacts`）。gob 版面改變：`detNodeSem` 少三個欄位，
// 型別事實改由 `detCached.TypeFacts`（occurrence 編號鍵）承載。
// 🔴 必 bump（`astdiskcache.go` 的 MUST-bump 清單點名 `InferredType` 屬
// 「parse-time side table that HIR consumes」）：舊 blob 解出的型別事實表為空，
// `?T` 推斷消失 → KLet 的 HIR Type 變空、checker 四處 option 豁免失效；
// 裝配期有效型別覆寫丟失 → 退回宣告型別。全部靜默，不報錯。
//
// 2026-10-23.4：THIR Stage 5 —— 剩餘 9 個欄位（EmbedData / EmbedFiles /
// Callee / IndexInBounds / ModuleConst / SourceFile / ModuleOwner /
// VariadicUnion / GenericUnion）從 `NodeSemantics` 遷到獨立的 `NodeFacts` 表
// （`SemanticContext.nodeFacts`），`NodeSemantics` 自此成為空殼。gob 版面改變：
// `detNodeSem` 清空，事實改由 `detCached.NodeFacts`（occurrence 編號鍵）承載。
// 🔴 必 bump（MUST-bump 清單點名 `embed data` / `IndexInBounds` /
// `ModuleConst` / `SourceFile` / `ModuleOwner` 五項）：舊 blob 解出的節點事實表
// 為空，診斷歸因靜默回退成主檔路徑、常量傳播與索引守衛豁免靜默失效、
// union 單態化標記消失（Tier-0 std blob 路徑不重跑 monomorphizeUnions，
// 無法自行補回）。作廢全部舊 blob。
//
// 2026-10-23.5：THIR Stage 7 —— 刪除 `NodeSemantics` 空殼與整條 nodeSem 通道：
// `SemanticContext.nodeSem` / `ensure` / `HasSemantics`、`CachedProgram.NodeSem`、
// `SerializeNodeSem` / `RestoreNodeSem`、`detNodeSem` / `detU32Sem` /
// `detCached.NodeSem`。**無任何欄位搬運**：Stage 2–5 已把 18 個欄位全部遷到
// `FmtTrivia`（不進 blob）/ `AnnTable` / `TypeFacts` / `NodeFacts`，本輪是純機械
// 刪除；`SerializeNodeSem` 自 Stage 5 起恆回 nil、`RestoreNodeSem` 恆為 no-op，
// 故資料面零變化。
//
// gob 版面改變：`detCached` 少一個欄位。舊 blob **仍能**解碼（gob 忽略流裡多餘
// 的欄位，且 `NodeSem` 恆為空、解不出任何東西），但依 C9 MUST-bump 第 2 條
// 「gob 版面改變」**仍須 bump** —— 版本號是「blob 與本版編譯器匹配」的唯一憑證，
// 解碼容忍不等於語義正確。作廢全部舊 blob。
//
// 2026-10-26.1：註解回歸 AST —— 刪除 `AnnTable` 整條通道（`SemanticContext.anns`
// / `NewAnnTable` / `Set` / `Of` / `Len` / `Clone` / `Merge`、`CachedProgram.Anns`、
// `SerializeAnns` / `RestoreAnns`、`detCached.Anns` / `detU32Anns` /
// `detFromAnnsMap` / `detToAnnsMap`），`#{...}` 條目改為住在節點上
// （`AnnotatedNode`，嵌入 `CommentedNode` / `StructField` / `EnumValue` /
// `TaggedEnumVariant` / `AssignExpression` / `SurfaceMatch`）。
//
// 🔴 必 bump，理由有三層，每一層單獨就足夠：
//  1. surface AST 形狀變了（MUST-bump 第 1 條）：六類節點各多一個
//     `Annotations []*AnnotationEntry` 欄位，gob 會把它們一起編碼；
//  2. gob 版面變了（第 2 條）：`detCached` 少一個欄位 `Anns`；
//  3. 註解是 HIR 消費的 parse-time side table（第 3 條）：`#{index-out}` 驅動
//     索引守衛、`#{overflow}` 驅動算術處理、`#{platform}` 驅動平台過濾。
//
// 舊 blob **不能**靠 gob 的「忽略多餘欄位」容錯：舊 blob 的註解只活在流裡的
// `detCached.Anns` 中，而新版解碼器已無該欄位 ⇒ 解出的樹上零註解，全部
// 註解驅動行為靜默失效（不報錯，只生成錯誤的程式）。這正是本輪改動要消滅的
// 那種「靜默丟失」形態，所以**必須**作廢全部舊 blob。
//
// 2026-10-26.2：`TypeFact` 刪除 `ByOptionArith` 欄位。它是「`InferredType` 由誰
// 寫入」的一 bit 標記，唯一消費者是 `maybeCaptureOptionAssign` 的根-算術短路，
// 而該短路其實是 `inferOptionArith` 在**同一次 `LowerSurface` walk 內**的局部
// 判定結果 ⇒ 改為 walker 把回傳值直接傳參，不再進副表。
//
// 🔴 必 bump（第 2 條「gob 版面改變」）：`TypeFact` 是 `detCached.TypeFacts` 的
// 線值，少一個欄位即版面變更。舊 blob 仍能解碼（gob 丟棄多餘欄位），但此欄位的
// 存在本身就證明「blob 與本版編譯器匹配」不成立；且此輪同時改了
// `maybeCaptureOptionAssign` 的簽名與短路條件（屬 parse-time 產出形狀），
// 舊 blob 是舊 walk 的產物，語義上不可重用。
// 2026-10-26.3：`CachedProgram` 收斂為**只有 `Prog`** —— `TypeFacts` / `NodeFacts`
// 兩個欄位連同它們的 `detCached` 鏡像（`detU32Facts` / `detU32NodeFacts` /
// `detNodeFact` 及四個轉換函式）全部刪除；解碼後改由 `BuildSemViews`
// （semviews.go）重走整棵樹重建。
//
// 動機不是體積（實測 gzip 只差 0.09%），是**邊界**：這個 blob 是「結構化的原始碼」，
// 未來可能換序列化格式、且要作為**分發格式**，所以不得夾帶任何編譯器推導結果 ——
// 推導規則會隨編譯器版本變化，而快取鍵（內容哈希 + ASTVersion + salt）看不到
// 編譯器建置。同一件事有兩個真相來源（樹 vs 副表）本身也會漂移。
//
// 🔴 必 bump（第 2 條「gob 版面改變」，且是雙向的）：
//   - 舊 blob 裡多出來的 `TypeFacts` / `NodeFacts` 欄位會被新 decoder 靜默丟棄
//     （gob 忽略流裡多餘的欄位），所以「能解碼」不等於「語義正確」；
//   - 若沿用舊版本號，新二進位會 HIT 到舊 blob 並**看起來一切正常**，實際上
//     `?T` 推斷與索引證明全部缺失 —— 正是本輪要消滅的那類靜默失效。
//
// `TypeFacts` / `NodeFacts` 型別本身仍保留 `GobEncode`/`GobDecode` 空方法：它們
// 沒有導出字段，直接 gob 編碼 `*parser.Program` 的路徑（mmast_test.go 模擬舊格式）
// 仍會碰到，缺了會報 `no exported fields`。
const ASTVersion = "2026-10-26.3"

// UserASTCache is Tier 1 of the incremental-build AST cache: an on-disk,
// content-addressed store of parsed user-module Programs. It is independent
// of process and workspace, so a fresh `no build`/`no test` reuses the AST of
// every unchanged user file and only re-lexes+re-parses what actually changed.
//
// The cache key is (source content hash + ASTVersion + an external-signature
// salt). The salt covers exactly the inputs that ParseProgram consumes beyond
// the raw source text — injected cross-file function/struct signatures, the
// anonymous-fn-type flag, and the std signature key — so a restored Program is
// always identical to a fresh parse under the same inputs and is never used
// where it would be wrong. AST only: MIR/LLVM IR is never stored, so there is
// no target/opt-level/bounds-check dimension to the key.
type UserASTCache struct {
	dir    string
	max    int
	mu     sync.Mutex
	approx int  // in-process approximate on-disk entry count
	primed bool // approx has been synced with the real directory at least once
}

// NewUserASTCache creates a cache rooted at dir with the given max entry count.
// The directory is created if missing.
func NewUserASTCache(dir string, max int) (*UserASTCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if max <= 0 {
		max = 4096
	}
	return &UserASTCache{dir: dir, max: max}, nil
}

var (
	userASTCacheOnce sync.Once
	userASTCacheInst *UserASTCache
	userASTCacheErr  error
)

// DefaultUserASTCache returns the process-global on-disk user AST cache rooted
// at $NOLANG_AST_CACHE_DIR (default ~/.cache/nolang/ast). A nil, non-error
// result means caching is disabled (dir unwritable or NOLANG_AST_CACHE_OFF set);
// callers skip the cache silently. The instance is created at most once per
// process.
func DefaultUserASTCache() (*UserASTCache, error) {
	userASTCacheOnce.Do(func() {
		if os.Getenv("NOLANG_AST_CACHE_OFF") != "" {
			userASTCacheInst = nil
			return
		}
		dir := os.Getenv("NOLANG_AST_CACHE_DIR")
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				userASTCacheInst = nil
				return
			}
			dir = filepath.Join(home, ".cache", "nolang", "ast")
		}
		max := 4096
		if v := os.Getenv("NOLANG_AST_CACHE_MAX"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				max = n
			}
		}
		c, err := NewUserASTCache(dir, max)
		if err != nil {
			userASTCacheInst = nil
			userASTCacheErr = err
			return
		}
		userASTCacheInst = c
	})
	return userASTCacheInst, userASTCacheErr
}

// KeyFor derives the content-addressed cache key for a source blob under a
// given external-signature salt. The salt must capture every ParseProgram
// input other than the source text itself (see the UserASTCache doc comment).
func (c *UserASTCache) KeyFor(content, sigSalt string) string {
	return cacheKey(content, sigSalt, ASTVersion)
}

// cacheKey is KeyFor with the version axis exposed. It exists so the C9 split
// can be asserted rather than assumed: a test can show that the key moves when
// the AST version moves, and that hir.HIRVersion is not an input at all
// (see TestASTCacheKeyCoversParseVersionOnly).
func cacheKey(content, sigSalt, astVersion string) string {
	h := sha256.New()
	h.Write([]byte(astVersion))
	h.Write([]byte{0})
	h.Write([]byte(sigSalt))
	h.Write([]byte{0})
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *UserASTCache) pathFor(key string) string {
	return filepath.Join(c.dir, key+".gob.gz")
}

func (c *UserASTCache) tmpPathFor(key string) string {
	return filepath.Join(c.dir, key+"."+strconv.Itoa(os.Getpid())+".tmp")
}

// Get decodes and returns the cached Program for key, or (nil, false). The
// returned Program is freshly decoded (not shared across calls), preserving
// the "parse fresh per load" semantics the codegen pipeline relies on.
//
// cfg MUST describe the parse inputs the tree was produced under (see
// SemRebuildConfig). The blob carries the syntax tree only, so the derived
// semantic views are rebuilt here by BuildSemViews. Requiring cfg as a
// parameter is deliberate: a decode that skips the rebuild yields a Program
// whose fact tables are empty, which is silently wrong (missing `?T`, missing
// index proofs) rather than loudly broken.
func (c *UserASTCache) Get(key string, cfg SemRebuildConfig) (*Program, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := os.ReadFile(c.pathFor(key))
	if err != nil {
		return nil, false
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	defer zr.Close()
	var cp CachedProgram
	if err := gob.NewDecoder(zr).Decode(&cp); err != nil || cp.Prog == nil {
		return nil, false
	}
	BuildSemViews(cp.Prog, cfg)
	return cp.Prog, true
}

// Put serializes prog and writes it to the cache under key, evicting the
// oldest entries when over capacity. It never returns an error that should
// abort a build; failures are silent.
//
// Only the syntax tree is written: CachedProgram has a single Prog field, and
// everything the compiler derives from it (TypeFacts / NodeFacts / index
// proofs) is recomputed by Get. See the semviews.go header for why.
func (c *UserASTCache) Put(key string, prog *Program) {
	if c == nil || prog == nil {
		return
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	cp := &CachedProgram{Prog: prog}
	if err := gob.NewEncoder(zw).Encode(cp); err != nil {
		return
	}
	if err := zw.Close(); err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.primed {
		if es, err := os.ReadDir(c.dir); err == nil {
			c.approx = len(es)
		}
		c.primed = true
	}
	final := c.pathFor(key)
	tmp := c.tmpPathFor(key)
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return
	}
	c.approx++
	if c.approx > c.max {
		c.evict()
	}
}

// evict trims the oldest entries (by mtime) until the on-disk count is back at
// or below c.max. It recomputes the real count first so cross-process growth
// is also bounded.
func (c *UserASTCache) evict() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type fe struct {
		name string
		mt   time.Time
	}
	files := make([]fe, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fe{e.Name(), info.ModTime()})
	}
	c.approx = len(files)
	over := c.approx - c.max
	if over <= 0 {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mt.Before(files[j].mt) })
	for i := 0; i < len(files) && over > 0; i++ {
		if strings.HasSuffix(files[i].name, ".tmp") {
			continue
		}
		if os.Remove(filepath.Join(c.dir, files[i].name)) == nil {
			over--
		}
	}
	c.approx = c.max
}
