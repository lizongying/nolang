# Nolang 自舉（Self-Hosting）技術方案：`compiler` 子項目

> **文檔定位**：這是一份**前向設計文檔**（forward design），不是實施記錄。`compiler/` 子項目目前**不存在**，本文描述的是它的目標形態、分階段路線圖與驗收門禁。所有既有資產的行號／計數取自 2026-10-09 工作樹（`HEAD = 4feaaf70`）。
>
> **一句話目標**：在 `compiler/` 子項目中用 **Nolang 自身**實現一個完整編譯器 `noc`，使它能夠**編譯自己**，並以「S0 → S1 → S2 → S3 定點」的方式證明自舉成立；同時用**五級差分門禁**保證它與現有 Go 實作 `no` 的行為一致。
>
> **命名**：`noc` = **No**lang **C**ompiler。它是 `compiler/` 包構建出的可執行檔，與 Go 實作的 `no` **並存**、互不干擾。`no` 在本文中一律指 **S0（當前 Go 實作）**，`noc-s1/s2/s3` 指自舉的三個階段產物。

---

## 0. 決策快照（Executive Summary）

| #        | 決策        | 結論                                                                                                                                                           | 主要論據                                                                                                                          |
| -------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| **DR-1** | 「自舉」的定義   | **三階段 + 定點**：S0（Go）編譯 `compiler/` → `noc-s1`；`noc-s1` 編譯同一份源碼 → `noc-s2`；`noc-s2` → `noc-s3`。要求 **s2 與 s3 在全語料上 `(rc, sha256(stdout))` 完全一致**，並盡量要求二進制逐位元組一致 | 這是唯一能排除「編譯器靠 S0 的某個隱含假設才跑得起來」的判據；單純「能用 nolang 寫個編譯器」不叫自舉                                                                      |
| **DR-2** | 後端吃什麼、吐什麼 | **吃自己的 MIR，吐 LLVM IR 文本**，再調用 `clang` 完成匯編 + 鏈接。**不依賴 `opt` / `llc`**（`clang` 可直接吃 `.ll`），**不實現機器碼生成器、不實現鏈接器**                                               | 沿用 `no` 今日已驗證的 MIR→IR 架構；跨平台（macOS arm64 / Linux x64 / Windows x64）由 `clang` 保證；把最大的不確定性（ISel/RA/obj writer/linker）整體排除在自舉範圍外 |
| **DR-3** | 堆記憶體策略    | **M4 階段一律用引用計數（RC）**：沿用 `no` 已有的 16 字節頭 ABI（`@nolang_rc_alloc` / `@nolang_rc_release`，`data = base + 16`）語義。**精確的所有權分析（`insertDrops` 那一整套）延後到 M7**           | 見 §5.6：這一步把 `src/mir` 43k LOC 中**最難、最容易出 UAF/double-free 的那一半**從「自舉必需」降級為「生產化優化」。RC 的缺陷（循環洩漏）對一個**短命編譯器進程**是可接受的              |
| **DR-4** | 自舉子集（SHS） | 明確**禁止**：一等函式值、`*` 指針、動態介面分派、匿名函式型別、`any`/`obj`、`#{c}` FFI。**強制使用**：標籤枚舉 + `match` 分派、arena（`[]T` + `i64` 索引）、`?T` + `?=`                                    | §2.2 有實測：fn 值在**除最平凡形狀外全部崩潰或 IR 驗證失敗**；arena+索引同時解決「循環引用無法用所有權表達」與「`[]T` 擴容使元素位址失效」兩個問題                                       |
| **DR-5** | 產物隔離      | `compiler/` 是獨立包，構建出 `noc`；**不改動** `no` 的任何行為；`-native`（nocg/nold，另一份設計文檔）不在本方案範圍                                                                            | 自舉期間 `no` 必須始終是一把**可信、可回歸的尺子**；讓 S0 同時當「被測物」會毀掉差分門禁                                                                           |
| **DR-6** | 驗證門禁      | **五級差分** L0 單元 / L1 詞法 / L2 AST / L3 診斷 / L4 行為 / L5 定點，全部**複用既有基建**（`no ast`、`no vet`、`scripts/mir_golden.sh`、`tests/golden/mir-baseline.tsv`）              | §7：這套基建已在 repo 中被反覆驗證過（AST 全語料 593/594 一致；golden 488 條指紋）                                                                     |
| **DR-7** | 前置改動      | 4 項 P0 **原訂**先於 M1 落地：① `package.jsonc` 包級 `overflow` 默認；② `no tokens` 子命令；③ `no ast --annotations`；④ 一等函式值修復。**M1 實測：② 已被繞過**——L1 的 oracle 改由 **repo 外**的只讀 dumper 提供（§7.1.1），**不動 `src/`**；② 仍對「`noc` 自己當 oracle」有價值                                                | §8：① 不做的話編譯器源碼每一行算術都要寫 `#{overflow=wrap}`；②③ 不做就沒有**內建**的詞法／註解差分 oracle（② 已由 §7.1.1 的外部 oracle 繞過）；④ 見 §2.2.1                                                           |
| **DR-8** | 信任鏈       | 反 Thompson 攻擊：**雙路交叉編譯**——除 `noc` 自身外，另用 JS 後端（`emit: "js"`）與 `no` 的 MIR 路徑各產一份，三方對拍                                                                         | §3.2：S0 是 TCB（trusted computing base），若不交叉驗證，一個被植入 S0 的後門可以自我複製進 S2/S3 且**永遠不會被差分測試發現**                                       |

---

## 1. 現狀盤點

### 1.1 規模（2026-10-09 實測）

| 組件                        | 檔案數 | 非測試 LOC      | 測試 LOC  | 自舉是否必需         |
| ------------------------- | --- | ------------ | ------- | -------------- |
| `src/lexer`               | 3   | 1,350        | 502     | ✅ M1           |
| `src/parser`（含 `dump.go`） | 21  | 22,313       | 7,584   | ✅ M2           |
| `src/checker`             | 14  | 22,739       | 7,942   | ✅ M3           |
| `src/hir`                 | 3   | 1,950        | 1,373   | ✅ M4           |
| `src/mir`                 | 20  | **43,177**   | 11,913  | ✅ M4（**最大單體**） |
| `src/build`               | 26  | 17,964       | 4,378   | ✅ M5           |
| `src/builtin`             | 17  | 2,423        | 203     | ✅ M4           |
| `src/package`             | 8   | 2,223        | 1,103   | ✅ M5           |
| `src/cmd/no`              | 5   | 4,607        | 641     | ✅ M5           |
| `src/cache`               | 1   | 106          | 82      | ✅ M5           |
| **核心流水線小計**               | —   | **≈118,850** | ≈39,700 | —              |
| `src/fmt`                 | 6   | 3,109        | 5,449   | ❌ 非必需（可後置）     |
| `src/lsp`                 | 18  | 6,424        | 5,531   | ❌ 非必需（可後置）     |
| `src/tools`               | 13  | 2,690        | 0       | ❌ 非必需          |
| **`src/` 合計**             | —   | **131,218**  | 46,701  | —              |

| 語言側資產                           | 數量                                    | 說明                                        |
| ------------------------------- | ------------------------------------- | ----------------------------------------- |
| `src/std/**/*.no`               | **118 個模組 / 75,154 LOC**              | 自舉編譯器**必須能編譯整個 std**（因為它要編譯自己，而自己會用到 std） |
| `tests/**/*.no`                 | **527 個檔案**                           | 行為差分語料                                    |
| `tests/golden/mir-baseline.tsv` | **488 條指紋**（`rc sha256(stdout) path`） | L4 門禁的基準                                  |
| `src/lexer/token.go`            | **92 個 TokenType**                    | M1 的完整目標                                  |
| `src/parser/*.go`               | **78 個 AST struct**（其中 **71** 個實作 `Node`；節點定義**全部**在 `ast.go`） | M2 的完整目標                                  |
| `src/mir/mir.go`                | **67 個 MIR op**                       | M4 的完整目標                                  |
| `src/checker`                   | **37 個 `Validate*` 函式**               | M3 的完整目標                                  |
| `src/std`                       | **158 處 `#{buildin}` 樁**              | 見 §5.4 的三條載入路徑陷阱                          |
| `src/checker/stdsig_gen.go`     | 7,179 行（`embeddedStdSigVersion = 2`）  | std 簽名表，**必須搬進 `compiler/`**              |

> **結論**：這是一個 **12 萬行級** 的移植工程，不是「寫個玩具編譯器」。方案的全部設計目標就是**把 12 萬行拆成 8 個各自可獨立驗收的里程碑**，並讓每一步都有可執行的差分門禁。

### 1.2 可直接複用的既有資產

| 資產                  | 位置                                                                           | 對自舉的用途                                                                                                                  |
| ------------------- | ---------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| **行為基準指紋**          | `tests/golden/mir-baseline.tsv` + `scripts/mir_golden.sh`                    | L4 門禁：全語料 `(rc, sha256(stdout))` 對比，已內建 517+ 檔案的並行 harness、逾時歸一化、bucket 分類（SAME/DIVERGE/REGRESS/IMPROVED/BOTH_FAIL/NEW） |
| **AST oracle**      | `no ast <file>` → `parser/dump.go`                                           | L2 門禁：repo 已用此法證明過「全語料 593/594 AST 逐位元組一致」                                                                              |
| **診斷 oracle**       | `no vet <file>`                                                              | L3 門禁：`<file>:<line>:<col>: [LEVEL] <source>: <msg> [<code>]`；⚠️ 歸一化後才可比（§7.3）                                          |
| **IR 檢視**           | `no build -v -o OUT F.no`（保留 `.ll`）；`NOLANG_MIR_DUMP_LL=1`（**驗證失敗時唯一窗口**）    | M4 調試                                                                                                                   |
| **runtime prelude** | `src/mir/codegen.go` 的 `windowsShimsIR()` / `%task` prelude / `@nolang_rc_*` | M4：**IR 文本可以直接照抄**，不需要重新設計 runtime                                                                                      |
| **標準庫**             | `src/std`（118 模組）                                                            | `noc` 自身的工具層（`fs` / `os` / `process` / `path` / `json` / `sort` / `str`）                                                |
| **測試框架**            | `test/runner.no`（`runner.eq` / `runner.test` / `runner.summary`）             | L0 門禁；⚠️ **斷言失敗不會自動改變 rc**，測試檔必須自己 `f-count > 0 -> exit(1)`                                                          |
| **平台常量表**           | `src/mir/platform.go`、`src/package/platform.go`                              | M5：`targetGOOS()/targetGOARCH()` 是唯一真值來源；**絕不可用 `runtime.GOOS`，也絕不可寫在 package 級 `var`**                                 |

### 1.3 外部工具鏈接口（M5 的目標）

`no build` 今日的完整路徑（`src/build/builder.go`）：

```
main.no
  ↓ lexer → parser → checker              （M1–M3）
  ↓ hir.LowerHIR() / ASTToHIR             （M4）
  ↓ mir.LowerHIR()                        （M4）
  ↓ mir.OptimizeMIR()                     （預設 OFF）
  ↓ transpiler.emitMIR() → mod.EmitLLVM() （M4：MIR → LLVM IR 文本）
  ↓ verifyMIRIRViaOpt()                   （M4：可選，用 opt -passes=verify）
  ↓ WriteFile(<tmp>/<name>.ll)
  ↓ exec "opt"  -O2 -S                    （M4：可選）
  ↓ exec "llc"  -mtriple=…                （★ 自舉方案：跳過，交給 clang）
  ↓ exec clang/zig cc <out>               （★ 自舉方案：`clang -x ir` 一步到位）
  ↓ os.Chmod(out, 0755)
```

`noc` 的目標路徑（**只有兩處外部調用**）：

```
compiler/main.no
  ↓ noc 前端 + MIR + IR 文本（全部在 nolang 內）
  ↓ WriteFile(<tmp>/<name>.ll)
  ↓ process.start-with-opts("clang", ["-x","ir","-O2","-o",out,ll,"-lm"], "", "", true)
  ↓ chmod
```

`std/process.no` 已有 `process.start-with-opts(program str, args []str, dir str, input str, merge-err bool) (ok bool)`，`std/fs.no` 已有 `read-bytes` / `write-bytes` / `list-dir` / `exists`——**驅動層所需的系統調用全部已具備**，無需新增 FFI。

---

## 2. 語言能力評估（全部為實測，非推論）

### 2.1 已驗證**可用**的慣用法

全部以 `./bin/no run <probe>` 實測通過（探針原文見附錄 B）：

| 慣用法                                                            | 結論                          | 實測輸出                                                             |
| -------------------------------------------------------------- | --------------------------- | ---------------------------------------------------------------- |
| **遞迴型別**（`next node #{inline=false}`）                          | ✅ 可用                        | 鏈式建構 + 讀取正確                                                      |
| **自引用切片欄位**（`kids []node`）                                     | ✅ 可用                        | `a.kids.push(c)` 後 `a.kids[0].kind == 2`                         |
| **arena 模式**（`[]node` + `i64` 索引）                              | ✅ 可用                        | 索引／欄位讀取正確                                                        |
| **方法 + out-param**（`arena.add = (k i64, v i64) (idx i64) {…}`） | ✅ 可用                        | 返回值正確                                                            |
| **map 作為符號表**（`m [str]i64`）                                    | ✅ 可用（`get` 回 `?V`，見 §2.2.6） | `put` / `get` / `len` 正確                                         |
| **`[]byte` 緩衝 + 手工字串構建**                                       | ✅ 可用                        | `b.push('hello ')` + `to-bytes-str()` 正確（完整 `sb` 樣板見 §5.1.2，已驗證） |
| **標籤枚舉 + `match`**                                             | ✅ 可用                        | 全語料大量使用                                                          |
| **`?T` + `?=` 錯誤上拋**                                           | ✅ 可用                        | std 全庫依賴                                                         |
| **具名函式型別 + 頂層函式作為參數**                                          | ⚠️ **僅最平凡形狀**               | `apply(inc, 41)` → `42`                                          |
| **結構體字面量作為呼叫引數**                                               | ✅ **必須多行**                  | 單行會 parse error（語言規則，非 bug）                                      |
| **結構體名 ≥ 2 字元**                                                | ⚠️ **硬約束**                  | 單字元結構體名 ⇒ `EmitLLVM: unsupported value type %x`                  |
| **`map.get` 回 `?V` + 三臂窮盡 match**                              | ⚠️ **必須處理 `err`**           | 少一臂 ⇒ `option match must handle all branches`                    |
| **`# /path/module.symbol [alias]` 導入**                         | ✅ 可用                        | 路徑**相對於 workspace 根**，不是檔案系統根                                    |
| **`exit` / `process.start-with-opts` / `fs.read-bytes`**    | ✅ 可用                        | 驅動層完備                                                            |

#### 2.1.1 語法陷阱（M1 實作時全部踩過；每一條都產生**誤導性**的錯誤訊息）

| 陷阱                                                                                             | 症狀（實際觀測）                                                                                                                                        | 正確寫法                                                                                |
| ---------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| 🔴 **`;` 是行註解，不是敘述分隔符**                                                                         | `c == X -> { typ = A; return }` 寫成**一行** ⇒ `; return }` 整段被註解掉 ⇒ 該區塊**永遠不閉合**。33 個未閉合區塊會吞掉其後所有定義，而錯誤報在**很後面**的呼叫端：`'lx-emit' is not defined`（指向 200 行外的呼叫點） | 每個 `-> { … }` 臂**多行展開**；`;` 只允許出現在字串字面量裡                                                  |
| 🔴 **`x -> { … }` 不是迴圈**                                                                        | 以為 `go -> { … }` 是 while ⇒ 迴圈體**只執行一次**（它是 match 臂）。症狀：「只吐出第一個 token 就停」                                                                            | `(cond) { … }`（前置式）或 `{ … } (cond)`（後綴式）；計數用 `n * { }`；範圍用 `i <- [a..b): { }`               |
| 🔴 **`err` 是保留標識符**                                                                            | 用它當參數名或局部變數名 ⇒ **整個函式不被註冊**，錯誤卻指向**呼叫端**（`'lx-emit' is not defined`）                                                                              | 改名（`lx.no` 用 `emsg`）                                                                 |
| 🔴 **導入形式有兩種，用錯的訊息不指向根因**                                                                       | `# /path/mod.Type` = **型別**導入（配 `<Type>.<method>`）；`# /path/mod` = **模組**導入（配 `<mod>.<定義名>`）。用錯 ⇒ `unknown callee lx.dump` 或 `unknown function 'lx.dump': module 'lx' has no top-level function 'dump'` | `lx` 無結構體 ⇒ 模組形式；`sb` 是結構體 ⇒ 型別形式                                                     |
| ⚠️ **跨模組呼叫用的是「定義名」而非「前綴名」**                                                                     | 函式定義名 `lx-dump` ⇒ 呼叫端只能寫 `lx.lx-dump(...)`                                                                                                      | 對外進入點命名為 `dump` ⇒ `lx.dump(src)`                                                      |
| ⚠️ **`#{overflow=wrap}` 只作用於緊跟的「那一條敘述」**                                                       | `cond -> v = a + b` 寫成一行 ⇒ 標註無效 ⇒ `[ovfhndld]`                                                                                                    | 展開成多行區塊，把標註放在**真正含算術**的那一行之前                                                          |
| ⚠️ **`fs.read-str` 非逐位元組無損**                                                                   | 非 ASCII 位元組被二次編碼（5 → 15 位元組），**`rc` 測不出來**                                                                                                        | `fs.read-bytes` + `to-bytes-str()`；見 §2.2.9                                            |
| ⚠️ **跨模組常量只有「入口檔直接 import」時才正確**                                                                | 只被間接依賴時**靜默變成 0**，作為運算式運算元時**硬編譯失敗**                                                                                                            | 入口檔直接列出該模組（**未使用也要列**）；見 §2.2.8                                                        |
| 🔴 **(M2) arena kid 槽位必須對齊 spec 的 CHILD 計數**                                                       | LetStatement 缺 Type 時只 push 1 個 kid ⇒ dumper 會讀到**後續節點的 kid** ⇒ 樹錯位／成環 ⇒ `noc ast` **掛死**（rc 測不出，需超時包裝）            | 無推斷型別時也要 `kid-add(t, idx, 0)` 佔位，再無條件 push Value 槽；見 §6.3                                       |
| 🔴 **(M2) `name = (` 有兩種句法**                                                                       | 只看首個 `LPAREN` 就判函式定義 ⇒ `a = (x)` 被誤判為 FunctionLiteral，`ps-parse-block` 苦等 `RBRACE` 到 EOF ⇒ **掛死**              |  lookahead 到配平的 `)` 後第一個非 trivia token 是 `{` 才算函式（`ps-is-func`）；否則走 GroupedExpression；見 §6.3                    |
| ⚠️ **(M2) 複合標註是合法的**                                                                                | 逐條堆疊 `#{index-out=zero}` `#{overflow=wrap}` 很囉嗦                                                                                          | 同一行用逗號合併：`#{index-out=zero, overflow=wrap}`（先例：`src/std/crypto/x509.no`）                                       |
| 🔴 **(M2) `{ … cond -> { 大區塊 } … -> 運算式 }` 的尾臂不互斥**                                                    | 區塊內前面 `cond -> { idx = A }` 已把 `idx` 設好，但尾部 `-> idx = B` **會作為無條件尾隨陳述再執行一次** ⇒ 覆寫前面結果（實際症狀：`x = 1` dump 出 `Program{}`，`idx` 由 ASSIGN 臂設的 3 被覆寫為 ps-parse-expr-stmt 的 0）         | 用 `handled` 旗標：前面每個臂末尾寫 `handled = 1`，尾臂改寫為 `handled == 0 -> idx = …`；**原 `-> {}`**（空區塊）恰好因為 no-op 才讓舊版僥倖正確（`ps.no::ps-parse-stmt` 的 IDENT 分支）                                       |
| 🔴 **(M2) 迴圈內 ≥3 個區塊臂的 match 跳臂迭代誤編**                                                                       | `(pgo) { A -> {blk} B -> {blk} C -> {blk} -> {pgo=false} }`：兩臂時正常，加到第三個區塊臂後，某個**跨臂接連迭代**會提前退出迴圈（實測 `f(x)[2]`／`m[0][1]`／`a.b(1)` 三探針必其一在首臂後就 `pgo=false`，後續 `[]`/`()` 被當成独立陳述）；調整臂序只是把壞的轉移換一個，換預設臂為區塊形也無效 | 同上——用 `handled` 旗標把終止條件剝離 match：每臂尾 `handled = 1`，迴圈末尾 `handled == 0 -> pgo = false`（`ps.no` 後綴迴圈）                                       |
| 🔴 **(M2) match 各臂區塊的區域變數提升到同一函式作用域**                                                                     | 同一 `cond -> {…}` 多臂去糖成的 if/else 鏈，各臂區塊內宣告的變數**並非區塊作用域**而是共用外層函式作用域；若新臂沿用兄弟臂已佔用的名字但**型別不同**（如 `ps-parse-stmt` 內 `tn` 在「明示型別標註」臂是 `tt.lit`=str、在「多重指派」臂卻當 i64），型別 checker/codegen 合併成 str ⇒ `tn > 0` 觸發 MIR「ordering comparison gt with a string operand」、`targets.push(tn)` 觸發「cannot push 'str' into '[]i64'」⇒ **build 直接失敗**（非 L2 dump 錯，故 fixture diff 看不到，只有 `no build` 會爆） | 同一個 match 的各臂，**區域變數名要全域唯一**：新臂所有地變數加語義前綴（`maTargets/maT0/maTn/maVal/maK`）；`idx`/`handled` 等跨臂同型別者可共用                                                          |

#### 2.1.2 位元組遍歷約定：**先 `str.to-bytes()`，再走 `[]byte`**

> 實測結論（2026-10-21）：`str.byte(i)` **本身**是 O(1)；壞掉的是**用結構體字段當接收者**。

`emitBuiltinRawByteAt`（`src/mir/codegen.go:15115`）把 `s.byte(i)` 展開為
`GEP + load + zext`，純 O(1)——對**區域變數／參數**接收者完全沒有問題。
真正的陷阱是另一件事：

> 🔴 **讀取 `str` 結構體字段是一次「借用」⇒ 整塊深拷貝 ⇒ O(N)。**
> 一旦這種求值被放進逐位元組的掃描迴圈，每次迭代都拷一份，總量退化成 **O(N²)**。

分離實驗（探針 `/tmp/fieldprobe3.no`；同一工具鏈、同一迴圈形狀，**只差字段的型別**）：

| 接收者 | N=40K | N=80K | N=200K | 性質 |
|--------|-------|-------|--------|------|
| A：`bx.s` 為 `str` + `.byte(i)` | 0.040s | 0.158s | **0.852s** | **O(N²)** |
| B：同上但長度**先快取**到局部 | 0.043s | 0.138s | **0.836s** | **仍 O(N²)** |
| C：`bx.b` 為 `[]byte` + `[i]` | 0.009s | 0.010s | **0.010s** | **O(N)** |

B 仍二次 ⇒ 二次項**不在** `len-bytes()`，而在**字段借用本身**；
而 `[]byte` 的字段借用只拷描述符（3×i64，O(1)）並共享緩衝，故平坦。

**約定**：需要在位元組層面遍歷字串時，**在入口處 `str.to-bytes()` 轉一次**，
此後一律用 `b[i]` 索引、`b.len()` 取長度：

- 省去「單獨快取長度」的欄位（如 `lxst.n`），消除長度與實際緩衝漂移的風險；
- 不用 `len-bytes()`；
- 轉出來的 `[]byte` 可直接用於後續遍歷。

🔴 **硬約束：`str.to-bytes()` 【本身】是一次 O(N) 拷貝**，不是零成本視圖。
看實作就明白（`src/std/str.no:464`）：

```nolang
str.to-bytes = () (val []byte) {
    val = with-len(.len-bytes())   ; 新配置一塊緩衝
    i <- [0...len-bytes()): {
        val[i] = .byte(i)          ; 逐位元組複製
    }
}
```

所以它**只能在「入口／邊界」呼叫一次**。若放進逐位元組迴圈、或每次
`push` 都轉一次，就會把 §2.1.2 另一條不變量（`vec.push` 攤還 O(1)）
重新打回 **O(N²)**。

> 由此也得到一條**看似反直覺、其實正確**的結論：
> `sb.push(s str)` 內部用 `s.byte(i)` 逐位元組追加是**對的**——
> 因為 `s` 是**參數（區域變數）**，`.byte(i)` 是 O(1)，且位元組終究要
> 拷進 `.buf`。若改成 `sb.push(s []byte)` 並要求呼叫端先 `to-bytes()`，
> 只是多加一次 O(N) 拷貝與一次配置，沒有任何好處。

**取捨通則**（決定「要不要先轉 `[]byte`」）：

| 情境 | 怎麼寫 | 理由 |
|------|--------|------|
| 接收者是**結構體字段**，且放在逐位元組迴圈裡 | ✅ **務必**在入口轉一次 `[]byte` | 否則每次迭代整塊拷貝 ⇒ O(N²) |
| 接收者是**參數／區域變數**，只做 O(1) 索引 | 直接用 `.byte(i)` 即可 | 轉 `[]byte` 反而**多一次 O(N) 拷貝** |
| 需要**反覆遍歷**同一份資料多次 | 轉一次 `[]byte` 留存 | 一趟拷貝換後續每趟省一次拷貝 |

> ⚠️ 已知代價：`lx-esc`／`lx-contains-dot` 改簽名為 `[]byte` 後，呼叫端
> `t.lit.to-bytes()` 會比原本 `lz-esc(t.lit)` **多一次拷貝**（字段借用 1 次
> ＋ `to-bytes()` 1 次）。因為是**逐 token** 而非逐位元組、且字面量短，
> 實測仍在噪聲內（236K：0.054s vs 0.049s）。這是為了「寫法統一、
> 去掉長度快取」付的常數代價，屬於**可接受**；但不要據此把 `to-bytes()`
> 套進熱迴圈。

⚠️ 配套事實：`str.len()` 回的是**碼點數**，不是位元組數（`src/std/str.no:41-44`，
實作為 `n = .cp-index(.len-bytes())`）；`[]byte.len()` 才是位元組數——
這也是 `[]byte` 更適合當位元組遍歷載體的原因之一。

⚠️ 方法真名是 **`str.to-bytes()`**（`src/std/str.no:464`），**沒有** `str.bytes()` 這個名字。

---

### 2.2 實測**不可用 / 受限**——本方案的關鍵約束

> 這一節是本方案最重要的部分。**每一條都改寫了架構決策**，全部有可重跑探針。

#### 🔴 2.2.1 一等函式值：除最平凡形狀外**全部失敗**

| 形狀                                                 | 結果                                           |
| -------------------------------------------------- | -------------------------------------------- |
| 頂層函式作為引數傳入，在**被調用方直接調用**                           | ✅ `42`                                       |
| 存入區域變數後調用（`f cb = inc` + `f(41)`）                  | ❌ `signal: trace/BPT trap`                   |
| 從函式**返回**（`pick() (r cb) { r = inc }`）             | ❌ `opt-verify: use of undefined value '@r'`  |
| **再轉傳**一層（`twice(f, v) { apply(f, apply(f, v)) }`） | ❌ `opt-verify: use of undefined value '@f'`  |
| 存入**結構體欄位**後調用                                     | ❌ `EmitLLVM: unknown callee node.fn`         |
| 存入 **map** 後調用                                     | ❌ MIR 降級失敗（`consumer reads value 0 (NoVal)`） |
| **匿名**函式字面量作為引數                                    | ❌ parse error                                |
| 區域函式定義 / 閉包捕獲                                      | ❌ `'f' is not defined; call it as 'des.f'`   |

**語料事實**：`src/std` **0 處**使用具名函式型別；`tests/` 僅 **5 個檔案**（`named-fn-type.no`、`fn-type-params.no`、`fn-type-return.no`、`fn-type-result.no`、`mem-safety/ffi-str-return.no`）使用，且全部只用「最平凡形狀」。

**架構後果（強制）**：

1. **SHS 禁止 fn 值**。編譯器的分派一律用 **`kind i64` + `match`**，或 arena 索引 + `match`。
2. 這一條**不阻塞自舉**——因為 std 不用它，編譯器自己也不用它。
3. 但 **§8 P0-④ 必須修**：`noc` 作為一個「完整編譯器」，遲早要編譯使用 fn 值的**使用者程式**（語料裡就有 5 個）。修復前的替代方案是「SHS 檢查器 + 明確診斷」。

#### 🔴 2.2.2 整數運算默認返回 `option<int>`，且**只能逐敘述標註**

```no
f = () (r i64) {
    #{overflow=wrap}      ; ← 只作用於「緊跟其後的那一條敘述」
    a = 1
    b = a + 2             ; ← 這裡仍然報錯 [ovfhndld]
    c = b * 3             ; ← 這裡也報錯
    r = c - 1
}
```

實測：區塊級標註**無效**；每一條含算術的敘述都要單獨寫 `#{overflow=wrap}`。`package.jsonc` 的 `compiler` 區塊**目前沒有** `overflow` 默認鍵。

**後果**：一個 12 萬行、以整數/索引運算為主的編譯器，會需要**數萬條** `#{overflow=wrap}`。這在可讀性與維護性上都不可接受 ⇒ **§8 P0-①**。

#### 🔴 2.2.3 深度遞迴的棧預算未測

`no` 的解析器是遞迴下降的，`noc` 也必然是。已知 `src/mir` 有一條硬性約束：**每個 `alloca` 必須在 entry block**（否則迴圈裡每輪成長一次棧）。這意味著**每個遞迴框架的棧佔用是「函式全部區域變數之和」**，不會隨分支收縮。

**後果**：需要 **M2 的一個明確驗收項**——量測語料中嵌套最深的檔案所需棧深，並決定是否需要（a）把 `parseExpression` 的若干層迭代化，或（b）在 `noc` 的 `main` 中調用 `setrlimit`/換大棧。**不要等到 M6 才發現**。

#### 🔴 2.2.4 `str` 讀寫索引語義不對稱

`s[i]` **讀**是**碼點**索引，`s[i] = b` **寫**是**位元組**索引。編譯器源碼會大量做位元組級掃描（詞法分析、UTF-8 解碼、hex 解析）。

**後果（寫進 SHS 規則）**：**一切位元組級讀取一律用 `s.byte(i)`**（回無號數值），邊界一律用 `s.len-bytes()`；`.len()` 是**碼點數**。這條規則在 repo 的歷史上曾造成過 109 處靜默錯值（`src/std/net/*` 的 TLS 記錄長度 `0x94` 被讀成 `1`）。

#### 🔴 2.2.5 結構體名**必須 ≥ 2 個字元**（單字元名會炸 codegen）

實測：`a` / `b` / `c` … `z`（**24 個單字元名全部**）作為結構體名時，一律：

```
Error: compilation error: MIR codegen could not handle /tmp/pp.no:
  EmitLLVM: unsupported value type %h in func main
```

同一個程式把 `h` 改成 `hh` 或 `holder` 就正常（`5`）。**這不是文件化的限制**，但它是硬事實。

**後果**：SHS 必須加一條「結構體／枚舉／介面名長度 ≥ 2」，並由 `shs-check.no` 檢查。**不要**用 `t` / `n` / `v` / `k` / `s` 這類名字——它們同時也是泛型的型別變數名。

#### 🔴 2.2.6 `map.get` 回傳 `?V`（不是裸值），且 option match **必須處理 `err`**

`[K]V` 的 `get` 簽名是 `get = (key K) (result ?V)`（`src/std/collection/map.no`）。實測：**選項 match 必須窮盡 `ok` / `nil` / `err` 三臂**，只寫兩臂是**編譯錯誤**：

```
parser errors: option match must handle all branches: err, nil, ok (missing: err)
```

同理，`fs.read-bytes` 回 `?[]byte`、`fs.write-bytes` 回 `?bool`。**`noc` 源碼裡的每一次 `get` / `read` / `write` 都是一個必須顯式處理的分支。**

#### 🔴 2.2.7 跨模組調用**強制** `ShortName.` 前綴


`module.md` 明確：調用其他 `.no` 檔案定義的函式/常量，**必須**寫 `ShortName.`。而「隱式同目錄合併」**不存在**（實測：同目錄另一檔案的 `greet` 直接不可見）。

**後果**：編譯器拆成 40 個檔案時，**每一處跨檔調用**都要前綴。這不是 bug，但直接影響**檔案切分策略**：

- 模組名要**短**（`lx` / `ps` / `ck` / `mr` / `cg`，而不是 `lexer` / `parser` / `checker` / `mir` / `codegen`）；
- 或者把**高度耦合的階段放在同一個檔案**（例如 parser + AST 定義同檔），只在階段邊界處跨模組；
- 兩種做法都要在 M0 就定下來——中途改切分等於全庫重寫。

#### 🔴 2.2.8 跨模組常量：只有**入口檔直接 import** 的模組才會生成其頂層綁定

**最小重現（3 檔，全部可重跑）**

```no
; tmp/probeZ/lib.no
K = 42

; tmp/probeZ/dep.no
# /tmp/probeZ/lib
a = () (r i64) { r = lib.K }

; tmp/probeZ/mn.no   ← 入口檔（頂層語句自動合成 main）
# /tmp/probeZ/dep
print(dep.a())
```

```
$ ./bin/no build -o /tmp/pD tmp/probeZ/mn.no && /tmp/pD
0          ← 不是 42
```

同一份程式，只要在**入口檔**多加一行**從未被使用**的 `# /tmp/probeZ/lib`：

```
$ printf '# /tmp/probeZ/lib\n# /tmp/probeZ/dep\n\nprint(dep.a())\n' > tmp/probeZ/n2.no
$ ./bin/no build -o /tmp/pD tmp/probeZ/n2.no && /tmp/pD
42
```

**判準（全部實測）**

| 入口檔的 import                                        | `dep` 內讀 `lib.K` |
| -------------------------------------------------- | ---------------- |
| 只 `# dep`                                           | ❌ `0`            |
| `# lib` + `# dep`（`lib` **未被使用**也算）                 | ✅ `42`           |
| 只 `# lib`（在 `main` 直接讀）                            | ✅ `42`           |
| `main → mid → dep → lib`，`mid` 有 `# lib`（**中間模組無效**） | ❌ `0`            |

- 「入口檔」= **含 `main`（或頂層語句自動合成 `main`）的那個檔案**。中間模組直接 import `lib` **無效**。
- import 的**順序無關**。
- 同一個「沒被生成的綁定」被當作**運算式的運算元**時，會升級為**硬編譯失敗**（`bin/no` 的 bug #85 guard 抓到它）：

```
Error: compilation error: MIR codegen could not handle …/main.no:
  EmitLLVM: MIR->LLVM: func lx_regex_start: consumer reads value 0 (NoVal),
  which no instruction produced — a lowering failure previously emitted as
  `undef` (bug #85); the instruction feeding this operand has no Dst
```

實測對照（同一個**被 import** 的模組 `lxm.no`；`# /compiler/src/tok` 提供 `tok.X`）：

| 寫法                                     | 結果                          |
| -------------------------------------- | --------------------------- |
| `r = tok.X`（常數是**整個 RHS**）            | 編譯過，值為 **0**（靜默錯值）          |
| `p == tok.X -> …`（常數是 `==` 的**運算元**）  | ❌ 硬編譯失敗                     |
| `r = tok.X + 1`                        | ❌ 硬編譯失敗                     |
| `m(p == tok.X)`（**呼叫引數**位置）            | ❌ 硬編譯失敗                     |
| `{ … } (p == tok.X)`（**迴圈條件**）         | ❌ 硬編譯失敗                     |

**語料可達性**：全語料 1584 個 `.no`，符合「被 import 的模組內、以跨模組常量作運算式運算元」的檔案數 = **0**（`compiler/` 除外）⇒ 這是**新暴露**的潛在缺陷，不是既有回歸。

**架構後果（強制）**

1. `compiler/main.no` 必須**直接**列出**每一個**「其常量/全局會被任何模組讀取」的模組——即使 `main` 從不使用它們。`noc` 目前的做法是在 `main.no` 直接寫 `# /compiler/src/tok` 與 `# /compiler/src/util/sb.sb`，並附「為什麼」的註釋。
2. 這是**反組合（non-composable）**的約束：新增一個讀取新模組常量的檔案時，**必須同時**回到入口檔加一行。⇒ 只能靠 **L1 全語料差分**兜底（錯值必然表現為 DIFF，不會表現為 `rc`）。
3. 正解見 §8 **P0-⑤**（讓 codegen 依「模組是否被引用」而非「是否被入口檔直接 import」來生成綁定）。

#### 🔴 2.2.9 `fs.read-str` **不是逐位元組無損**——詞法分析器必須走 `read-bytes`

`fs.read-str` → `file.read-str` → `[]byte.to-str()`，而 `[]byte.to-str()` 自 **2026-09-06** 起是「**byte → 碼點 → UTF-8 重編碼**」（`src/std/byte.no` 的註釋明說這是應需求方要求的語義變更；1:1 拷貝語義保留在 `[]byte.to-bytes-str()`）。

實測：

```
$ printf '; \xe7\xbb\xbc\n' > /tmp/u1.txt          ; 5 位元組
$ xxd /tmp/u1.txt
3b20 e7bb bc0a
$ ./bin/no run /tmp/rb.no                          ; rb.no: print(fs.read-str(...).len-bytes())
15                                                 ; ← 不是 5
```

每個 `≥ 0x80` 的位元組變成 **4 位元組**（`0xE7 → F0 80 83 A7`）。`bin/no` 自己（Go 實作）也是這個行為 ⇒ **不是 `noc` 的移植錯誤**。

**對策**：`fs.read-bytes(p)` + `it.to-bytes-str()`（`src/std/byte.no` 的 1:1 逐位元組拷貝）。

**後果**：編譯器的**每一次**原始碼讀取都必須用 `read-bytes` + `to-bytes-str`。用 `fs.read-str` 會讓所有非 ASCII 註解／字串字面量被二次編碼，而 **`rc` 測不出來**——只有逐位元組差分（L1）能抓到。見 §8 **P0-⑥**。

#### 🔴 2.2.10 `fs.read-bytes` 對**空檔案**回 `nil`（不是 `ok` + 空切片）

`fs.read-bytes(p)` 的 `?[]byte` 三態實測：

| 檔案狀態      | 結果                    |
| --------- | --------------------- |
| 存在且有內容    | `ok`（`len()` 正確）      |
| **存在但 0 位元組** | **`nil`**             |
| 路徑不存在     | `err`                 |

⇒ 「讀到空」與「讀失敗」**必須分開處理**。若把 `nil` 當錯誤，`noc tokens <空檔>` 會 **rc=1** 而 oracle 是 **rc=0**（oracle 對空檔吐單一 `1:1 EOF`）⇒ **L1 假 DIFF**。

```
$ : > /tmp/empty.no
$ ./bin/no run <probe>      ; c = fs.read-bytes('/tmp/empty.no'); c: { ok->… nil->… err->… }
empty-file -> nil
$ ./bin/no tokens /tmp/empty.no        ; SUT（修復前）
noc: cannot read /tmp/empty.no         ; rc=1
$ /tmp/notok/notok tokens /tmp/empty.no
1:1 EOF - - - -                        ; rc=0
```

**語料中目前沒有 0 位元組的 `.no`**（674 檔全過 L1）⇒ 此缺陷是**潛伏**的，不是當前 DIFF 的來源。`compiler/src/drv.no` 已把 `nil` 當成「合法的空來源」處理（見該檔註釋）。見 §8 **P1-⑤**。

### 2.3 自舉子集（SHS, Self-Hosting Subset）

**SHS 是 `compiler/` 源碼允許使用的語言特性白名單**，由 M0 的一個守護腳本強制（見 §7.6）。

| 允許                                              | 禁止                                             | 理由                      |
| ----------------------------------------------- | ---------------------------------------------- | ----------------------- |
| 結構體 / 標籤枚舉 / `match`                            | 一等函式值                                          | §2.2.1                  |
| `[]T`（arena）/ `[N]T` / `str` / `txt` / `[]byte` | `*T` 指針（FFI 專用）                                | 語言規則                    |
| `[K]V` map                                      | `any` / `obj`（std 專用）                          | 語言規則                    |
| 方法 + out-param                                  | 動態介面分派（nolang 的 `iface` 是**單態化靜態分派**，非 vtable） | 架構選擇：用 `kind` + `match` |
| `?T` / `?=` / `err`                             | 匿名函式型別（需 `anonymous-fn-type: true`）            | 配置默認值                   |
| 泛型（`[n]t` / `[]t`）                              | `#{c}` FFI 宣告                                  | 保持可移植性                  |
| 模組（`# /path.mod.sym`）                           | 全局可變狀態（模組級 `let` 有特殊釋放語義）                      | 見 §5.1.4                |
| `#{overflow=wrap}`                              | `#{}` 位置前綴（parse error）                        | 語言規則                    |
| `co` / 協程組（**僅在驅動層並行調用 clang 時**）               | `run` / `awy` 手寫                               | 已廢棄手寫                   |

**SHS 命名硬規則**（`shs-check.no` 強制）：

| 規則                                                                                  | 理由        |
| ----------------------------------------------------------------------------------- | --------- |
| 結構體 / 枚舉 / 介面名 **≥ 2 字元**                                                           | §2.2.5 實測 |
| 型別名**不得**用 `t` / `n` / `v` / `k` / `s`                                              | 與泛型型別變數衝突 |
| 每個 `get` / `read-*` / `write-*` 的 `?T` 結果**必須顯式 match**，且三臂（`ok`/`nil`/`err`）**齊全** | §2.2.6 實測 |

---

## 3. 自舉策略

### 3.1 三階段與定點

```
        ┌──────────────────────────────────────────────┐
        │  S0 = bin/no （Go 實作，v0.3.19+，鎖定 SHA）  │  ← 唯一可信的「尺子」
        └───────────────┬──────────────────────────────┘
                        │  no build -o noc-s1 compiler
                        ▼
   ┌────────────────────────────────────────────────────┐
   │  S1 = noc-s1 ：用 S0 編譯 compiler/ 源碼得到        │  ← 自舉的第一步
   └───────────────┬────────────────────────────────────┘
                   │  noc-s1 build -o noc-s2 compiler
                   ▼
   ┌────────────────────────────────────────────────────┐
   │  S2 = noc-s2 ：完全由 nolang 編譯器產出             │
   └───────────────┬────────────────────────────────────┘
                   │  noc-s2 build -o noc-s3 compiler
                   ▼
   ┌────────────────────────────────────────────────────┐
   │  S3 = noc-s3                                        │
   └────────────────────────────────────────────────────┘

   定點判據（三者缺一不可）：
     ① 行為定點：∀f ∈ 語料， (rc, sha256(stdout))(noc-s2, f) == (rc, sha256(stdout))(noc-s3, f)
     ② 自舉定點：noc-s3 build compiler == noc-s2 build compiler（逐位元組）
     ③ 無回歸：noc-s3 的 L1–L4 門禁全部與 S0 一致
```

**為什麼需要 S3 而不止於 S2？** S2 是「由 S1 產出」，而 S1 是「由 S0 產出」。若 `noc` 的某個行為**恰好**依賴 S0 的某個隱含假設（例如某個未定義行為被 LLVM 以特定方式折疊），S2 可能「碰巧正確」。S2→S3 是**用同一個編譯器編譯它自己**，任何這類假設都會在這一輪暴露成 s2/s3 的差異。

**為什麼要求「逐位元組一致」而不是只要求「行為一致」？** 行為一致是**必要**條件；逐位元組一致是**充分**證據。若兩者只能取其一，**必須先做到行為一致**（這是門禁的硬性部分），逐位元組一致作為 M6 的**加分驗收項**（若後端含 Go map 迭代等非確定性來源，逐位元組一致可能不可達——見 §9 R6）。

### 3.2 信任鏈與反 Thompson 攻擊

S0 是 TCB。**一個被植入 S0 的後門，可以讓 S0 在編譯 `compiler/` 時注入惡意邏輯，而該邏輯會在 S2/S3 中自我複製——且任何差分測試都測不出來**（因為 s2 與 s3 都是「自洽地錯」）。這是 Thompson 的 "Reflections on Trusting Trust"。

**本方案的三重反制**（M6 必須全部執行）：

| 反制                 | 做法                                                                                                                          | 能抓到什麼                  |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| **① 三路交叉編譯**       | `compiler/` 源碼分別經 (a) S0 的 MIR/LLVM 路徑、(b) S0 的 **JS 後端**（`compiler.emit: "js"`）、(c) `noc` 路徑編譯，三份產物在同一語料上比對 `(rc, stdout)` | 任一後端的注入                |
| **② 非自產編譯器的 S2**   | 用 **S0 直接編譯 `noc` 的「第二實現」**（見下）                                                                                             | S0 對 `compiler/` 的定向注入 |
| **③ 源碼級審計 + 差分審計** | 對 `compiler/` 的**每一個** M6 里程碑版本做 `no fmt -d` + AST dump 的雙人複核，並要求 **S2 與 S3 的 `.ll` 輸出在排序後 diff 為空**                        | 輸出層注入                  |

> 反制 ② 的「第二實現」成本很高（等於寫兩遍）。**務實的降級方案**：M6 只要求 ① 與 ③，並在文檔中明確記錄「S0 是 TCB，其完整性由 repo 的 Git 歷史與 CI 保證，不由自舉流程保證」。**不要假裝自舉本身解決了信任問題。**

### 3.3 為什麼不選「用 nolang 重寫整個 Go 編譯器（含 LSP/fmt/tools）」

| 選項                                                  | 工作量            | 對自舉的價值                                  |
| --------------------------------------------------- | -------------- | --------------------------------------- |
| 只做**核心流水線**（lexer→parser→checker→hir→mir→build→cmd） | ≈118.8k LOC Go | ✅ 這是自舉的**充分**條件：能編譯自己                   |
| 加上 `fmt`（3.1k）+ `lsp`（6.4k）+ `tools`（2.7k）          | +12.2k LOC     | ❌ 與「能否編譯自己」無關；`fmt` 可用 `no fmt`（S0）外部完成 |

**結論**：M0–M6 的範圍**嚴格限定在核心流水線**。`fmt` / `lsp` / `tools` 列為 **M8（後置）**，不阻塞自舉定點。

---

## 4. 子項目佈局

### 4.1 目錄結構

```
compiler/                          ← 新子項目（workspace 註冊為 "compiler"）
├── package.jsonc                  ; name: "compiler"，compiler.overflow: "wrap"（P0-① 落地後）
├── main.no                        ; 入口：no build -o noc compiler
├── README.md                      ; 構建與自舉步驟（S0→S1→S2→S3）
│
├── src/
│   ├── diag.no                    ; 診斷模型（位置、級別、碼、渲染）
│   │
│   │   ── 前端 ──────────────────────────────────────────────
│   ├── tok.no                     ; 92 個 TokenType + 關鍵字表（對齊 lexer/token.go）
│   ├── lx.no                      ; 詞法分析（對齊 lexer/lexer.go，1,350 LOC）
│   ├── ast.no                     ; 71 個節點型別（ast.go 共 78 struct）+ arena 容器 + dump（對齊 parser/ast.go + dump.go）
│   ├── ps.no                      ; 解析器（對齊 parser/*.go，22,313 LOC —— 最大前端單體）
│   ├── ann.no                     ; 註解系統 #{...}（對齊 parser/annotation.go）
│   ├── ty.no                      ; 型別表示與解析（對齊 parser/type.go）
│   │
│   │   ── 語義 ──────────────────────────────────────────────
│   ├── ck.no                      ; 型別檢查主體（對齊 checker/checker.go）
│   ├── ck-args.no                 ; 呼叫引數檢查 + stdargtyp（對齊 checker/funcargs.go）
│   ├── ck-lint.no                 ; 37 個 Validate*（對齊 checker/lint.go）
│   ├── ck-sig.no                  ; std 簽名表（**由 stdsig_gen.go 生成的 .no 檔案**）
│   │
│   │   ── 中端 ──────────────────────────────────────────────
│   ├── hir.no                     ; HIR 樹（對齊 hir/*.go，1,950 LOC）
│   ├── hir2mir.no                 ; HIR → MIR 降級（對齊 mir/hir2mir.go）
│   ├── mir.no                     ; MIR 表示 + 67 個 op（對齊 mir/mir.go）
│   ├── mir-opt.no                 ; 可選：MIR 優化（對齊 mir/opt.go）
│   │
│   │   ── 後端 ──────────────────────────────────────────────
│   ├── cg.no                      ; MIR → LLVM IR 文本（對齊 mir/codegen.go）
│   ├── cg-builtin.no              ; 內建函式降級（對齊 mir/builtins.go + builtin_call.go）
│   ├── cg-runtime.no              ; runtime prelude IR 文本（**照抄** codegen.go 中的 raw string）
│   ├── cg-rc.no                   ; RC 頭 ABI：@nolang_rc_alloc / @nolang_rc_release
│   ├── cg-win.no                  ; Windows shim（對齊 mir/builtin_win_shims.go，可後置到 M7）
│   │
│   │   ── 驅動 ──────────────────────────────────────────────
│   ├── ws.no                      ; workspace.jsonc / package.jsonc 解析（用 std/json）
│   ├── mod.no                     ; `#` 導入解析 + 模組合併（對齊 build/transpiler.go 的 merge 部分）
│   ├── drv.no                     ; CLI：build / run / ast / vet / tokens
│   └── link.no                    ; 調用 clang（process.start-with-opts）
│
├── src/util/
│   ├── sb.no                      ; 字串構建器（std 目前沒有！見 §5.1.2）
│   ├── arena.no                   ; 通用 arena：push / get / len / reserve
│   ├── intern.no                  ; 識別符號／字串駐留（符號表、map key 去重）
│   ├── idxmap.no                  ; [str]i64 的薄封裝 + 缺失語義
│   └── pathx.no                   ; 路徑拼接／規範化（基於 std/path）
│
├── tests/
│   ├── lx-test.no                 ; 詞法單元測試（對齊 src/lexer/lexer_test.go）
│   ├── ps_test.no                 ; 語法單元測試（對齊 src/parser/*_test.go）
│   ├── ck_test.no                 ; 語義單元測試
│   ├── cg_test.no                 ; 代碼生成單元測試（斷言 IR 片段）
│   └── e2e_test.no                ; 端到端：編譯 → 執行 → 比對
│
└── tools/
    ├── gen_stdsig/                ; 從 src/std 生成 src/ck-sig.no（Go 實作，一次性）
    └── shs-check.no               ; SHS 白名單守護（§7.6）
```

### 4.2 workspace / package 接入

**`workspace.jsonc`（倉庫根）**：新增一行

```jsonc
{
    "test1": "/example/test1",
    "test2": "/example/test2",
    "test": "/test",
    "compiler": "/compiler",     // ← 新增
}
```

**`compiler/package.jsonc`**：

```jsonc
{
  "name": "compiler",
  "mirrors": [],
  "dependencies": {},
  "compiler": {
    "version": "v0.3.19",
    "link-libs": [],
    "anonymous-fn-type": false,
    // ↓ P0-① 落地後才有這個鍵；落地前必須逐敘述寫 #{overflow=wrap}
    "overflow": "wrap",
  },
}
```

**構建與使用**：

```bash
make no                                    # 構建 S0（唯一可信的尺子）
./bin/no build -o /tmp/noc-s1 compiler     # S1：S0 編譯 compiler/
/tmp/noc-s1 build -o /tmp/noc-s2 compiler  # S2：S1 編譯 compiler/
/tmp/noc-s2 build -o /tmp/noc-s3 compiler  # S3：S2 編譯 compiler/
```

⚠️ **`no build` 的 flag 必須在檔案前**（`-o OUT FILE`）。寫反會**靜默不產出、rc 仍為 0**。

⚠️ **不要**把 `noc` 安裝成 `no`（`/usr/local/bin/no`）——那會讓 S0 與被測物互相污染，並破壞所有既有 golden 流程。

### 4.3 模塊劃分與依賴方向

依賴必須是**單向**的（無循環）。這在 nolang 裡尤其重要，因為**跨模組調用強制前綴**、且循環導入沒有隱式合併可以兜底。

```
util/{sb,arena,intern,idxmap,pathx}
        ▲
        │
      diag
        ▲
        │
   tok ──► lx ──► ast ──► ps ──► ck ──► hir ──► hir2mir ──► mir ──► cg ──► link
                    │       │                            │
                    └───► ann / ty                    cg-builtin / cg-runtime / cg-rc
                                                        ▲
                                              ws ──► mod ──► drv ──► main.no
```

**規則**：

- 上游模組**絕不** import 下游（`ast` 不知道 `ps` 存在；`mir` 不知道 `cg` 存在）。
- 共用資料結構（`ast.node`、`mir.inst`）**只在一處定義**，下游 import 上游。
- `main.no` 只做 CLI 分派，不含邏輯。

### 4.4 命名與前綴約定（直接影響全庫可讀性）

| 約定                                                          | 理由                      |
| ----------------------------------------------------------- | ----------------------- |
| 模組名 ≤ 3 字元（`lx` / `ps` / `ck` / `cg` / `drv`）               | §2.2.5：每處跨模組調用都要前綴      |
| 函式名**不重複模組名**（`lx.next-token` 而非 `lx.lexer-next-token`）     | repo 的 `module.md` 明文規定 |
| 避開關鍵字：`run` / `match` / `in` / `for` / `co` / `it` / `self` | 語言規則                    |
| 型別名用大寫開頭；區域變數小寫                                             | repo 的 `code-style.md`  |
| **禁止**頂層 `let` 作為可變狀態（模組全局釋放語義特殊）                           | 見 §5.1.4                |

---

## 5. 架構設計

### 5.1 資料結構總則

#### 5.1.1 Arena + 索引：解決「循環引用」與「擴容失效」

**問題**：編譯器的 AST / 符號表 / 型別圖本質上是**有向圖**（可能含環：型別互相引用、符號互相引用）。Nolang 的所有權模型**無法表達環**——環意味著兩個值互相擁有，兩邊都釋放就是 double free。

**解法**：**一切圖狀結構都用 arena + `i64` 索引**。

```no
; 節點資料與「指向節點的引用」分離
nodedata {
    kind i64
    a i64          ; 子索引 / 值 / 位置
    b i64
    s str          ; 詞素 / 名稱
}

ast {
    nodes []nodedata
    n i64
}

ast.push = (k i64, a i64, b i64, s str) (idx i64) {
    d = nodedata {
        kind: k
        a: a
        b: b
        s: s
    }
    .nodes.push(d)
    .n = .n + 1
    idx = .n - 1
}
```

**為什麼必須是索引而不是「結構體欄位裡放 `node`」**：

1. `[]T` **擴容會重新分配**，任何指向舊緩衝的位址立即失效。索引永遠有效。
2. 環可以自由表達（`nodes[i].a = j` 且 `nodes[j].a = i`）。
3. `[]T` 的元素在 `push` 時若含擁有堆的欄位會被**深拷貝**；純 POD 的 `nodedata` 拷貝成本是固定的 `memcpy`。

⚠️ **`nodedata` 必須是 POD**（只放 `i64` 與 `str`；`str` 欄位在 `push` 時仍會深拷貝，若詞素很長需改為「索引到 intern 表」）。這條要在 M0 的 `arena.no` 裡寫成註釋約定，並由 `shs-check.no` 檢查。

#### 5.1.2 字串構建器（`util/sb.no`）——**std 目前沒有**

`str` 的 `+` / `-` 是 `@str_concat`，每次都是 O(n) 拷貝。若用它拼 12 萬行的 IR 文本，是 O(n²)。


```no
sb {
    buf []byte
    n i64
}

sb.init = (cap i64) (r sb) {
    r = sb { buf: with-cap(cap) n: 0 }
}

sb.push = (s str) {
    base = .buf.len()
    .buf.truncate(base)          ; 確保 len 反映實際寫入
    i <- [0..s.len-bytes()): {
        .buf.push(s.byte(i))
    }
}

sb.push-byte = (b i64) {
    .buf.push(b)
}

sb.str = () (r str) {
    r = .buf.to-bytes-str()
}

sb.len = () (n i64) {
    n = .buf.len()
}
```

**驗收**：`sb` 在 M0 就要有單元測試（含 1 MiB 級別的 push 與 `len()` 校驗），並在 M4 用它產出 IR，量測峰值 RSS 是否為**平線**（用 `/usr/bin/time -l <binary>`，⚠️ macOS 的數字**在前面**，`awk '{print $1}'`）。

#### 5.1.3 識別符號駐留（`util/intern.no`）

編譯器會反覆比較識別符號（關鍵字查找、符號表、map key）。`str` 比較是 O(len)。**駐留**把每個識別符號映射到一個 `i64`，之後比較是 O(1)。

```no
intern {
    table [str]i64
    pool []str
    n i64
}

intern.get = (s str) (id i64) {
    v ?i64 = .table.get(s)
    v: {
        ok(x) -> id = x
        nil -> {
            .pool.push(s)
            .n = .n + 1
            .table.put(s, .n)
            id = .n
        }
        err(e) -> id = 0
    }
}
```

⚠️ **三臂必須齊全**（`ok` / `nil` / `err`），少一臂是編譯錯誤（§2.2.6 實測）。上面的程式已驗證：`get('a')` 首次回 `1`、再次回 `1`、`get('b')` 回 `2`。

⚠️ 注意 `map.get` 的**缺失語義**是 `nil`（`?V`），不是 0。M0 要為此寫一個明確的封裝（`util/idxmap.no`），**不要**在業務程式碼裡散落 `get` 的缺失判斷。

#### 5.1.4 全局狀態：**禁止**

編譯器需要「當前診斷緩衝」「當前檔案」「當前作用域」這類上下文。`memory.md` 的「模組級變數釋放」一節說明模組全局有特殊語義，且 `module.md` 建議「模組中避免使用全局變量」。

**解法**：**一切可變狀態都放在一個顯式傳遞的 context 結構體裡**，並且因為「Nolang 所有函式參數預設為引用型別」，傳遞 context **不產生拷貝**。

```no
ctx {
    diags []diag
    file  str
    src   str
    pos   i64
}

ck.check = (c ctx, a ast, root i64) {
    ; c 是引用語義：c.diags.push(...) 直接作用於呼叫方的結構體
}
```

⚠️ **這條是 SHS 的硬規則**，`shs-check.no` 必須檢查：`compiler/src/**` 中不得出現模組級 `let`（`tok.no` 的關鍵字表等**只讀常量**除外，且必須在檔案頭以註釋聲明）。

### 5.2 詞法分析（M1）

**目標**：完整復刻 92 個 `TokenType` 的掃描行為，逐 token 一致。

**關鍵復刻點**（全部來自 `src/lexer/lexer.go`，每一條都曾在 repo 歷史上造成過 bug）：

| 點                  | 規則                                                                         |
| ------------------ | -------------------------------------------------------------------------- |
| **三種註釋**           | `;` 單行；`//` 單行；`;;` 後**緊跟換行**才進入塊註釋，結束的 `;;` 也必須後跟換行/EOF                   |
| **`/` 上下文敏感**      | `//` 是註釋，單個 `/` 是**正則字面量**還是**除法**取決於 `prevTokenType`（`isRegexStart`，仿 JS） |
| **`#{`**           | 註解指令的開始（`HASH_LBRACE`）                                                     |
| **`#1` `#2`**      | 標籤（`LABEL`），僅在後跟數字時                                                        |
| **`"x"` vs `'x'`** | 雙引號是 `CHAR`（單一碼點，底層 `i32`）；單引號是 `STRING`                                   |
| **反引號**            | 原始字串（可多行）                                                                  |
| **UTF-8**          | 識別符號與字串的碼點解碼必須與 Go 一致                                                      |

**Oracle**：**不是** `no tokens`（`no` 沒有這個子命令，§8 P0-②）——而是一個 **repo 之外**的只讀 dumper，介面 `<l1-ctl> tokens <file>`，它 `import` 的正是 `src/lexer`（即 P0-② 若實作也會走同一個包）。這條路**完全不改動 `src/`**，接線方式與守衛見 **§7.1.1**。

**驗收（已達成）**：`scripts/selfhost_diff.sh --levels L1 --l1-ctl <dumper>` 對**全語料 674 檔**（`tests` + `src` + `compiler` + `docs`）的 token 流**逐位元組一致**：

```
$ ./scripts/selfhost_diff.sh --sut /tmp/noc-l1c --levels L1 \
      --l1-ctl /tmp/notok/notok --corpus "tests src compiler docs"
  files=674  ctl_ok=674  ctl_empty=0  sut_ok=674  sut_empty=0
  SAME=674  DIFF=0
  L1  PASS
```

另有 L0 的 `compiler/tests/tok-test.no` **62/62** 單元斷言。

⚠️ **兩個會讓「一致」變成假的陷阱**（都已寫進 harness）：
1. **oracle 必須真的有鑑別力**。實測：把 `./bin/no` 當 L1 的 oracle 時，`no tokens <f>` 對**任何**輸入都印同一份 usage 且 **rc=0** ⇒ 差分器看到「控制組輸出非空、SUT 與之不同」而**誤報 FAIL**（把「oracle 不存在」記成 SUT 的錯）。harness 的 **G7** 守衛即為此：控制組在 ≥3 檔上只吐 **1 種** stdout 雜湊 ⇒ 報 **UNAVAILABLE**（§7.1.1）。
2. **讀檔必須逐位元組無損**（§2.2.9）。用 `fs.read-str` 會把 ≥0x80 的位元組二次編碼，**`rc` 不變**、只有 L1 逐位元組差分抓得到。

⚠️ **不要把「詞法一致」等同於「AST 一致」**。詞法差分能抓到 AST 差分抓不到的錯誤（例如註釋與空白的處理、位置資訊），反之亦然。**兩級都要做。**

### 5.3 語法分析（M2）

**目標**：完整復刻 **71 個 AST 節點**（`ast.go` 共 78 個 struct，其中 71 個實作 `Node`）與 `parser/dump.go` 的輸出格式。完整的節點清單、欄位、建構函式與錯誤訊息目錄見 **[`nolang-ast-grammar.md`](nolang-ast-grammar.md)**（機械抽取，可重跑）。

**已知的 parser 陷阱**（每一條都有 repo 內的實證，全部要在 `noc` 中復刻**相同的**判定）：

| 陷阱                                                                | 位置                             | 症狀                                                                              |
| ----------------------------------------------------------------- | ------------------------------ | ------------------------------------------------------------------------------- |
| `classifyBlock` 用**塊內第一個非換行 token** 判定塊的種類                        | `parser/parser.go`             | 新型語法（以 `#{`、`&T`、`*T` 開頭）會被誤判為語句塊 ⇒ `'x' is not defined`                        |
| `isStatementBoundary` 是**白名單**                                    | `parser/stmt.go`               | 漏一個 token ⇒ 該敘述連同其換行被吞掉；症狀是「靜默少一條敘述」                                            |
| 語句位置的裸 `{` **永遠不是宣告**                                             | `parseStatement` 的 `LBRACE` 分支 | 誤判成 enum/iface ⇒ 塊被吃掉，後續敘述**洩漏一層**（`print(1); {print(2)}; print(3)` 印出 `3,1,2`） |
| `->` 是**短路管道**，臂以**換行**分隔                                         | `parser/stmt.go`               | 內聯臂體裡的第二個 `->` 曾被當成新臂，靜默丟掉後面所有內容                                                |
| `(v) { … }` 是 **while-do 迴圈**（條件），**不是 match**；match 是 `v: { … }` | `parser/stmt.go`               | 誤用 ⇒ 失控迴圈                                                                       |
| 註解只有**兩個**合法位置：獨立上一行 / 同行尾隨                                       | `parser/annotation.go`         | 同行**前綴**是 parse error                                                           |
| 結構體字面量**必須多行**                                                    | —                              | 單行 ⇒ parse error（§2.1 實測）                                                       |
| 頂層綁定的**大小寫**決定全局/局部                                               | `hir2mir.go`                   | 語義差異                                                                            |

**Oracle**：`no ast [-annotations] <file>`。驗收 = 全語料 **AST dump 逐位元組一致**。`noc` 必須**照抄** `parser/dump.go` 的輸出格式（含 `<unknown *parser.X>` 這類 fallback 字樣）。

✅ **2026-10-10：oracle 已補齊（P0-③ 完成）**。原本 `dumpNode` 只覆蓋 40/71 個節點型別，其餘塌成 `<unknown>` 且**整個子樹不可見**。現在：

- **71/71 個節點型別全部有 case**（含 9 種 Type 節點、4 種 Statement、10 種 Expression、12 種輔助節點）；
- **每個 `Node` 欄位與每個 `Node` 切片元素都被遞迴**（型別、成員、變體、參數、註解值）；「只印殼」的節點已清零；
- 新增 **`-annotations`**：把 `#{...}` 註解 side-table 依**樹序**印出（預設不含，見 §5.3.1）；
- 有**回歸釘**：`src/parser/dump/dump_completeness_test.go` 走全語料，任何節點塌成 `<unknown>` 即 FAIL。

⚠️ **dump 的穩定性**：`no ast` 的輸出必須是**確定性**的。**已實測**：三張 node-keyed 表（`anns` / `typeFacts` / `nodeFacts`；Stage 7 前是 `SemanticContext.nodeSem` 的 `map[Node]*NodeSemantics`）**絕不可 range**（Go 隨機化 map 疊代序）；dumper 一律以**樹序**走訪並對每個節點做**點查詢**。`TestDumpIsDeterministic` 對全語料連跑兩次逐位元組比對。

#### 5.3.1 `no ast` 的可見範圍：**已補齊**（2026-10-10）

> **狀態：修復前的盲點已全部關閉。** 本節同時保留修復前的實測數據（作為「為什麼必須補齊」的證據）
> 與**修復後的格式契約**（`noc` 必須照抄的那一份）。

**為什麼這件事是 M2 驗收判據的大前提。** `src/parser/dump/dump.go` 的 `dumpNode` 原本是一個**封閉的型別 switch**（331 行 / 40 個 case）。任何**不在 switch 裡**的節點型別落到 `default` 分支，只印一行 `<unknown *parser.X>`，**子樹完全不印**——不是「印得少」，而是**整個子樹不可見**。

##### 修復前的實測（歷史記錄，語料 = `tests/**/*.no`，528 檔 / 84,301 行 dump）

| `<unknown *parser.X>` | 行數      | 意義                      |
| --------------------- | ------- | ----------------------- |
| `MultiAssignStatement` | 154     | 多重賦值 `a, b = f()`       |
| `CoExpression`         | 47      | `co { … }`              |
| `AwaitExpression`      | 43      | `awy`                   |
| `RunExpression`        | 36      | `run`                   |
| `MapLiteral`           | 29      | map 字面量 `[K]V{…}`       |
| `TypeAlias`            | 6       | `type x = y`            |
| `RegexLiteral`         | 2       | `/re/`                  |
| `ExternStatement`      | 1       | `extern`                |
| `AnnotationStatement`  | 1       | `#{…}` 敘述               |
| **合計**                | **319** | **9 種節點型別完全不可見**（佔 0.38%） |

另有「進得了 switch 但只印殼」的節點（子節點不 dump）：

| 節點                                                              | 印出                  | **沒有** dump        |
| --------------------------------------------------------------- | ------------------- | ----------------- |
| `StructDefinition` / `EnumDefinition` / `TaggedEnumDefinition`  | 只有 `name`           | **所有欄位／成員／變體**    |
| `StructLiteral`                                                 | 只有 `type`           | **所有欄位初始化**       |
| `InterfaceDefinition`                                           | name + 方法名          | 方法簽名              |
| `ArrayLiteral` / `RangeExpression`                              | `nodeString(size/start/end)` | 子運算式              |

⚠️ 當時的**最壞後果**：一個把 map 字面量、多重賦值、`co`/`run`/`awy` 全部解析錯的 `noc`，只要**錯在同樣的地方**（都塌成一行 `<unknown>`），L2 就報「完全一致」。而且語料中 145 處 struct literal 的欄位**從未被差分**。

##### 修復後的狀態（實測）

| 項目 | 修復前 | 修復後 |
| --- | --- | --- |
| `dumpNode` 的 case 數 | 40 | **71** |
| 未被覆蓋的 `Node` 型別 | 31 | **0** |
| 語料中 `<unknown>` 行數（528 檔） | 319（9 種） | **0** |
| 語料 dump 總行數 | 84,301 | 91,293（+8.3%） |
| `#{...}` 註解覆蓋 | 0 | **287 條 / 88 檔**（`-annotations`） |

**節點盤點（機械抽取，可重跑）**：`ast.go` 共 **78** 個 struct，其中 **71** 個實作 `Node`（`Statement` 18、`Expression` 31、`Type` 9、無 marker 的純 `Node` 13），另 **7** 個是非 `Node` 的輔助 struct（`Comment` / `CommentGroup` / `CommentedNode` / `FuncSignature` / `AnnotationStructField` / `TaggedEnumField` / `InterfaceMethod`）。完整清單、欄位、建構函式與**錯誤訊息目錄（168 個站點）**見 [`nolang-ast-grammar.md`](nolang-ast-grammar.md)。

##### 格式契約（`noc` 必須逐位元組復刻）

1. **一行一個節點**，每層縮排 4 個空格。
2. 節點自身的純量欄位寫成 `TypeName{field: value, …}`；**大括號恆存在**（無欄位 ⇒ `TypeName{}`）。
3. 純量欄位分兩類：
   - **來源層資訊**（名稱、字面值 + `raw`、運算子、型別摘要、標籤種類、範圍開閉 `leftInc`/`rightInc`、`slice`/`readonly`/`sealed`、`explicit`、`variadic` …）**一律印出**；
   - **parser 內部 desugar 狀態**（`synthetic`、`armBinding`、`propagation`、`moduleConst`、`inferred`、`wasSliceLiteral`、`condWrapper`、`propagated` …）**僅在為真／非空／非零時印出**，使常見情形保持乾淨而例外狀態仍可見、可比。
4. **每個 `Node` 欄位與每個 `Node` 切片的元素都遞迴**，順序即欄位宣告序。型別節點（`NamedType` / `ArrayType` / `SliceType` / `MapType` / `FunctionType` / `UnionType`）**也在其中**——這是 `noc` 必須建出**同一棵型別樹**、而非只印出同一個 `Type.String()` 摘要的原因。
5. 父節點的 `{…}` 標頭可含**摘要**（`value: MapLiteral` 之類），摘要只是提示；**權威內容是子節點行**。`nodeString` 的 `default` 回傳去掉 `*parser.` 前綴的型別名。
6. `default` 分支印 `<unknown %T>`：這是**絆線**，正常情況下**永遠不該出現**。
7. 註解：`Doc: //…` / `Comment: //…`。`#{...}` 註解 side-table **預設不印**，加 `-annotations` 才印（見下）。
8. ⚠️ **旗標必須在檔名之前**（Go 的 flag 套件遇到第一個位置參數就停止解析）：`no ast -annotations a.no`。

**修復後的最小重現（可重跑）**

```
$ ./bin/no ast tests/map.no
    LetStatement{name: m, type: [str]i64, value: MapLiteral}
        MapType{}
            NamedType{name: str}
            NamedType{name: i64}
        MapLiteral{pairs: 2}
            MapPair
                StringLiteral{value: a, raw: 'a'}
                IntegerLiteral{value: 0, raw: 0}
            …

$ ./bin/no ast tests/async-group.no | grep -A3 CoExpression
        CoExpression
            CallExpression{fn: …, args: N}
            …
```

##### `-annotations`：註解系統進入差分覆蓋

`#{...}` 註解住在 **AST 節點自己身上**（`AnnotatedNode.Annotations`，嵌入 `CommentedNode` / `StructField` / `EnumValue` / `TaggedEnumVariant` / `AssignExpression` / `SurfaceMatch`），不在 `Program.Sem` 的 side-table 裡。歷史：Stage 7 之前整族住在 `map[Node]*NodeSemantics`；Stage 3 遷進 `AnnTable`（`SemanticContext.anns`，node-keyed）；2026-10-26 整條 `AnnTable` 刪除——因為「尾隨註解」（`stmt #{...}`）只存在於表裡、AST 完全不承載，任何「從 AST 重建註解」的路徑都會靜默丟掉它們。

🔴 **絕不可 range 一張註解 map**：Go 隨機化 map 疊代序，dump 會變成不可重現。實作**依樹序走訪、對每個節點做點查詢**（`Sem.AnnotationsOf(n)`，現為純查詢、直接讀節點），因此輸出順序恆定。`TestDumpIsDeterministic` 對全語料連跑兩次逐位元組比對以釘住這一點。

**預設行為不變**：不加旗標時，dump 中**不得出現任何 `AnnotationEntry{` / `Annotation*Value{`**（`TestNoAnnotationsByDefault` 釘住）。結構性的 `AnnotationStatement{entries: N}` 是**普通 AST 節點**，照常印出——它與「註解內容」是兩回事。

##### 回歸釘（讓「已補齊」這句話可被證偽）

`src/parser/dump/dump_completeness_test.go` 走全語料，四個測試：

| 測試 | 釘住什麼 |
| --- | --- |
| `TestCorpusHasNoUnknownNodes` | 任何節點塌成 `<unknown>` ⇒ FAIL（預設 + `-annotations` 兩種模式） |
| `TestDumpIsDeterministic` | 同一 program 連 dump 兩次必須逐位元組相同 |
| `TestAnnotationsFlagIsWired` | `-annotations` **不是 no-op**：實測 **88 檔 / 287 條** |
| `TestNoAnnotationsByDefault` | 預設不得洩漏註解內容（P0-③ 的驗收判據） |

**已證偽**：把 `dumpNode` 的 `case *parser.MapLiteral:` 整塊刪掉後，`TestCorpusHasNoUnknownNodes` 立刻 FAIL，報 **24 檔 / 58 處 `<unknown *parser.MapLiteral>`**。⇒ 這條釘子**會壞**，不是擺設。

##### 仍然**不在** dump 裡的東西（刻意如此）

- **位置資訊**（`Token` / `lexer.Position`）——印位置會讓 dump 對「同一語義、不同排版」過度敏感，且 L2 不負責格式。
- `Program.Sem`（語義副表本體）、`Program.BuiltinFuncNames`（`map[string]bool`，**不可 range**）。
- 註解以外的 desugar 內部表（`VarTypes` / `EnumVariants` / `IfaceImpls` …）——那些屬於 M3 的 `no vet` 差分，不屬於 L2。

#### 🔴 5.3.2 dump 曝露了「**哪條建構路徑**造出這個節點」——M2 必須複製的是**路徑**，不只是語義

（2026-10-10 實測，M2 開工前發現）

`-annotations` 與「補齊節點型別」讓 L2 覆蓋變完整，但也讓 oracle **比語義更嚴格**。最清楚的一例是字串字面量的 `raw` 欄位：

```
$ printf "b = 'x'\nm [str]i64 = { 'k': 1 }\n" > /tmp/t2.no
$ ./bin/no ast /tmp/t2.no | grep -E 'Literal'
            LetStatement{name: b, type: str, value: x}
                StringLiteral{value: x}              ← 沒有 raw
                MapLiteral{pairs: 1}
                        StringLiteral{value: k}      ← 沒有 raw
```

但語料中**也存在** `StringLiteral{value: , raw: ''}`（空字串字面量）。差異不是語義，是**路徑**：

| 建構點 | 設定 `Raw`？ | 來源 |
| --- | --- | --- |
| `parser/expr.go:753` `parseStringLiteral`（**直接解析**） | ❌ 不設 | 原始碼裡的 `'x'` |
| `parser/lowering.go:1244/1384`（**lowering 合成**） | ✅ `Raw: "'" + value + "'"` | 註解推導出的預設值等 |
| `parser/lowering.go:1247/1363`（空字串） | ✅ `Raw: "''"` | 同上 |

`IntegerLiteral` / `FloatLiteral` 則**一律**設 `Raw`（`lowering.go:1254/1224`、`type.go:799/1255`）。

**⇒ 三個必須記住的結論**

1. **「`noc` 的 AST 語義等價」不等於「L2 逐位元組一致」。** `raw`、`synthetic`、`armBinding`、`propagation`、`moduleConst`、`inferred`、`wasSliceLiteral`、`condWrapper`、`propagated` 這些旗標，記錄的都是 **parser/lowering 的內部路徑**。`noc` 要綠燈，就必須在**同一批路徑上**建構節點——亦即 **M2 的範圍其實包含 `lowering.go` 的 desugar 邏輯**，不只是「讀語法」。
2. **因此 `ast.no` 的節點模型必須能表達「合成」節點**（例如帶 `synthetic`/`propagation` 的 `LetStatement`），不能只建模原始碼語法。這也是為什麼 §5.1.1 的 arena 模型要在 M2 就位。
3. **這是「嚴格但正確」還是「過度嚴格」需要證據來判斷。** 若某個旗標被證明無法在 `noc` 複製（例如它依賴 Go 的某個偶然順序），**登記為缺陷並附證據**，不要靜默放寬 oracle——但也不要假裝它不存在。

⚠️ **`FloatLiteral.Value` 用 Go 的 `%v` 印**（最短可往返表示）。語料中只有 10 處（首 120 檔），值全是 `0` / `1` / `1.5` / `24` / `30` 這類簡單數 ⇒ 目前可人工處理，但**這是一個未解的演算法風險**（一般情形需要 Ryu/Grisu 級演算法）。登記於 §9 風險。

#### 5.3.3 match 表示分歧 + `compiler/` 構建破損（2026-10-19 S7 調查；構建破損**已修** 2026-10-20）

**背景**：S1「match 表示統一」收官後（詳見 `nolang-parser-lowering-invariants.md` §22），bin/no 的 parser **保留 `SurfaceMatch`** 不展開（翻轉 `b918dba9`、刪除舊降糖路徑 `d6965622`），降糖移到 H0 lowering pass 執行。`noc`（§6.3 t23 片）仍在 **parse 期**把 match 展開成 if/else 鏈——這在 2026-10-19 之前是兩邊的共同事實，現在成為**單邊分歧**。

**實測（2026-10-19；`bin/no` = `make bin/no` @ HEAD `4c3dfbf7` + 併發 WIP）**：

| 項目 | 實測值 |
| --- | --- |
| L2 預設語料（`find tests src/std -name '*.no' -type f`） | **653 檔** |
| 其中 `no ast` 產出含 `SurfaceMatch{`（⇒ noc 產 if/else 鏈，**必然 DIFF**） | **264 檔** |
| 探針 `tests/option.no`（2 陳述層 + 1 表達式層 + 1 裸 match） | bin/no 產 **4 個 `SurfaceMatch{arms: 3}`**、零 IfExpression 鏈；noc 仍產 if/else 鏈 |

⇒ **L2 在 264/653 檔上必然偏離**；S7 的「跑滿五級」（G6）在分歧裁定前不可能全綠。

**全量實測（2026-10-20，SUT＝構建修復後的 `noc-s0`）**：653 檔 DIFF=315 / SAME=338，另有 3 檔 sut 獨有 rc=124 挂起（**該三檔已於同日修復歸零，見下文「三檔挂起已修（s58/s59）」**）。**跨 noc 對照歸因**：同一 CTL 下，`762ea045` 之前的 noc（`b805ed06`）DIFF=355 / sut 獨有失敗 8 檔；修後 `noc-s0` DIFF=315 / 3 檔，且 DIFF 集 ⊆ 舊集（唯一例外 `tests/1-test.no`——`762ea045` 推斷移植的過度推斷，因構建破损潛伏、被修復激活，非本次修法引入），3 档挂起在舊 noc 上同樣复现。⇒ 本次修法**零新增分歧/挂起**；`762ea045` 的移植（經修復激活）淨減 40 檔分歧。歸因詳見 `nolang-parser-lowering-invariants.md` §11.1 第 4 條。

**✅ 裁定①（2026-10-21，使用者拍板）**：`ps.no` 改為產出 `SurfaceMatch`（parser 忠實，即 P1），降糖留待 M4 的 `hir.no` 重新實作。落地：`PS-SURFACE-MATCH` 預設翻 `1`（`compiler/src/ps.no`，開關保留為 A/B 回歸基線，翻 0 重編即得舊降糖形）；`ps-test.no` test 10 期望改為 oracle 的 `SurfaceMatch` 形（逐位元組對齊 `bin/no ast`）。選①的理由：`asp.no` 的 `K_SURFACE_MATCH` 條目早已就位（§6.3/§18.3）、surface 形已實測零回歸（§5.3.4.x：+45 檔 L2 收斂 / 0 回歸），而選②會讓自舉編譯器永久偏離 P1 並讓 L2 語料長期背豁免清單。**裁定後 L2 結論**：match 表示分歧不再是登記項；剩餘 DIFF 歸正交 track（`?T` unwrap / type-inference），G6 五級全綠仍待這些 track 收斂。

**🔴→✅ 前置發現：`compiler/` 曾自 2026-10-13 起無法構建（與上述分歧獨立，但曾阻塞 S7 全部五級驗收）——已於 2026-10-20 修復**：

| 事實 | 證據 |
| --- | --- |
| `./bin/no build -o /tmp/noc-s0 compiler` 曾失敗 | `EmitLLVM: MIR->LLVM: func ps_parse_func` / `func ps_parse_method_func`: `consumer reads value 0 (NoVal), which no instruction produced … (bug #85)`；**HEAD `4c3dfbf7` 乾淨工作樹（`git worktree`）復現**，非併發 WIP 所致 |
| 破壞者 commit | **`762ea045`**（2026-10-13 12:45「refactor(parser): infer types of local variables and function return values during parsing」）——手動二分實測鏈：`c721fe6c`（bare match 降糖）OK → `b805ed06` OK → **`762ea045` FAIL**（初現 8 處 NoVal，後續 commit 增至 12 處，同一對函式、同一錯誤形態） |
| 根因（修時證實） | `762ea045` 給 ps.no 加的型別登記循環把索引讀（`params[rp]` 等）巢置於 call 引數內 ⇒ option 捕獲合成：nil/err 臂把賦值目標（如 `pd`）定型為 `?i64`，ok 臂寫 struct，合併點後續欄位讀 `pd.kid0` 按 option 解析 ⇒ MIR NoVal（HIR/MIR dump 實證：`params[rp]` → `__cap_20 t=?i64` → 三臂 match） |
| 修法（語言層，不動 Go 編譯器） | 索引讀提取為獨立陳述 + `#{index-out=zero}`（該注解只覆蓋陳述內直接讀、**不覆蓋巢置於 call 引數內的讀**——v1→v4 最小復現階梯實證；與 ps.no 自身 `wildA[z]` 慣用法一致）。共 **8 處**：`ps-parse-func` / `ps-parse-method-func` 各 4（params/outs 型別登記循環 + params/outs 泛型偵測循環） |
| 修復實證 | `./bin/no build -o /tmp/noc-s0 compiler` **成功**（`noc v0.0.1-m0`）；L2 全語料歸因：修後 `noc-s0` 與 `762ea045` 之前的 noc（`c721fe6c`/`b805ed06`）在挂起檔上行為一致，**未引入新分歧**（詳見 `nolang-parser-lowering-invariants.md` §11.1 第 4 條） |
| 無人發現的時長 | **6 天**：CI 無 `no build compiler` 門禁（`pr-verify` 此前只跑 Go 單測 / std 測試 / 語料冒煙） |
| 影響面 | 曾阻塞 L0–L4（全都需要可用的 `noc-s0`）與 DR-1 自舉三階段（每一步都以「`no` 能編譯 `compiler/`」為前提） |
| 防復發 | CI 已補閘：`pr-verify` 新增 `./bin/no build -o /tmp/noc-ci compiler` 步驟（2026-10-20，`.github/workflows/build.yml`） |

**✅ 三檔挂起已修（2026-10-20；兩個獨立根因，s58/s59）**：三檔 sut 獨有 rc=124 挂起全部归零。此前二分一度误判为「单方法不触发、需 struct + 多方法上下文」，实为**两个**独立缺陷：

| 根因 | 位置与实据 | 修法 |
| --- | --- | --- |
| **A. 陣列 let 分支** | `ps-parse-stmt` 明式元素型別臂（`name [N]T = expr`）無條件呼叫 `ps-parse-arr-lit`；RHS 非 `[` 開頭（`net.no:571` 的 `iv [12]byte = .gen-iv()`）時 `ps-expect(LBRACKET)` 靜默失敗、元素循環把後續全部陳述當元素吞到 EOF | 復刻 Go `stmt.go:1333-1356`：RHS 先 `ps-skip-trivia`，為 `[` 才走 `ps-parse-arr-lit`，否則 `ps-parse-expr` |
| **B. IN 上下文關鍵字** | `ps-parse-expr-prec` primary 缺 IN 臂——Go `expr.go:96` 是 `case IDENT, IN, MATCH: parseIdentifier()`，表達式位的 `in` 是合法識別字。`aes-cbc.no:222` / `tls.no:695` 的 `(in[i]) != pad-len -> { … }` ⇒ 空殼 GroupedExpression + pos 卡住 ⇒ 一行被撕成三個陳述 | primary 臂改 `tk.typ == tok.IDENT \|\| tk.typ == tok.IN`（MATCH 同類但語料無用例，未補） |

- **故障形態（根因 B，三種）**：① `it = in[i]` ⇒ let RHS 丟失、`[i]` 拆成獨立 `SliceLiteral` 陳述；② `enc-block(in, ek)` ⇒ 引數 2→1（**靜默丟參**，比報錯危險）；③ 陳述層括號索引 + 比較 ⇒ 41+ 深 GroupedExpression 鏈 ⇒ dump 縮排 O(depth/行)、總量 O(N²) ⇒ GB 級輸出 ⇒ 表現為挂起。
- **挂起的兩個必要條件**（缺一不挂）：① 病理解剖出的超深節點鏈；② dump 的 `indent()` 二次方縮排。定位手法：`ast.no` 的 `node()` 加 `depth > 40` 截斷守衛 ⇒ 「挂起」變成「可完成 + 標記」，暴露 `ArrayLiteral{size: 12, elements: 33}`／深 GroupedExpression 鏈。
- **歸因（A/B 同一 harness、同一 CTL、同一 653 檔）**：verdict DIFF 309→311 / SAME 338→338 / 真挂起 2→0（+2 DIFF 係 aes-cbc、tls 由「挂起」轉為「可解析但仍帶 match 分歧」，非回歸）；**SAME→DIFF 真回歸 0 檔**。逐檔差異行數 **210 971 → 210 111（-860），7 檔改善、0 檔惡化**（`tdes` -372、`aes-ctr` -278、`rc4` -70、`aes` -56、`des` -38、`ecdsa` -26、`base32` -20）。
- **回歸測試**：新增 `compiler/tests/ps-test.no`（`./bin/no run compiler/tests/ps-test.no`，5/5 通過）釘死四種 `in` 形態 + 一組無 `in` 對照；期望字串全取自 oracle 實測。⚠️ **反空洞**：還原 IN 臂後該測試**挂起**（不是僅報錯）——正是缺陷本身。
- ⚠️ **新發現的既有缺陷（未修，已登記）**：`no build` / `no run` 一個導入 `compiler/src/ps` 的獨立檔案時，若入口檔**未直接導入** `tok`（lx.no 的 `tok.NEWLINE` 等跨模組常量所依賴的模組），這些常量解析不到 ⇒ MIR 降成 `const void` + `eq NoVal` ⇒ bug #85 類 `EmitLLVM` 失敗。**包內構建（`no build compiler`）不受影響**——故自舉構建與 CI 閘皆不受波及。繞開方式：測試檔導入清單補齊傳遞依賴（ps-test.no 已如此）。

**✅ 陣列／切片型別方法定義已修（2026-10-20；s64/s65，三小步）**：接續挂起归零後的 L2 非 match 分歧收斂。對齊 Go `parseArrayTypeMethodDefinition`（`decl.go:192-575`）——它其實是**一個函式三種接收者**（`[n]t` / `[]t` / `[?]t`），noc 此前**一種都沒有**。

| 步 | 缺口 | 實據（oracle vs noc） | 修法 |
| --- | --- | --- | --- |
| **s64** | 泛型陣列接收者 `[N]T.method = (…) { … }` | `[n]t.len` ⇒ `[]` 建成**幽靈 SliceLiteral 陳述**（並把 `Doc` 註解搶走），`t.len` 另起為普通方法 ⇒ 名稱丟 `[n]` 前綴、self 降成 `NamedType{t}`、泛型 `n` 缺席 | `ps-parse-stmt` 新增分派臂（守衛 `[ (IDENT\|INT) ] IDENT . IDENT = (`），建 ArrayType 後傳給 `ps-parse-method-func`（新增 `recvNode` 形參） |
| **s65a** | 參數位陣列型別的尺寸只收 INT | `[n]t.clone = () (dst [n]t)` 的 `(dst [n]t)` 退化成 `dst` / `n` / `t` **三個參數**（`ps-parse-type` 的 ArrayType 臂守衛寫死 `peek-at(1) == INT`）；對齊 Go `decl.go:301-303` 的 `INT \|\| IDENT` | 守衛改 `(INT \|\| IDENT)` |
| **s65b** | 切片接收者 `[]T.method` | 同 s64 形，語料分布最廣：`vec.no` 22 處、`byte.no` 12、`str.no`、`char.no`、`sort.no` 2 | 新增 `[] IDENT . IDENT = (` 臂，建 SliceType |

- ⚠️ **s65c（修 s64 自己引入的偏差）**：隱式泛型的**播種範圍**。Go 只在 `[n]t` 分支呼叫 `addImplicitGeneric(size/elem)`（`decl.go:242-243`），`[]t` / `[?]t` 分支**不**呼叫；且三條方法路徑（`decl.go:569` / `94` / `2037`）都在 `detectImplicitGeneric`（`516-521` / `1750-1753`）**之後**才把 `selfParam` 插進 `Results` ⇒ **self 的型別永遠不被掃描**。noc 原先順序是 `params → self → outs` 且對任何 `recvNode` 都播種 ⇒ 兩處偏差：① `[n]t.arr-stack-push` 產 `t, n`（應為 `n, t`）；② `[]t.len = () (out i64)` 憑 self 的 `SliceType{t}` 多出 `GenericParams: t`（oracle **無**）。修：播種限 `kind == K_ARRAY_TYPE` 的接收者，並把 self 從探測序列移除。
- **歸因（A/B 同一 harness、同一 oracle、同一 653 檔，三步各自量測）**：

  | 步驟 | 逐檔差異行數總計 | 改善／惡化 |
  | --- | --- | --- |
  | 基線（`a7160791`，s58+s59） | 223 232 | — |
  | +s64 | 222 248（−984） | 6 改善 / **0 惡化** |
  | +s65a/b | 221 648（−600） | 9 改善 / **0 惡化** |
  | +s65c | 221 626（−22） | 1 改善 / **0 惡化** |

  主要收斂：`arr.no` 565→153、`vec.no` 1241→964、`link.no` 1168→819、`byte.no` 546→341、`queue.no` 265→173、`arr-stack.no` 94→**0**、`tests/std-lib.no` 44→**0**、`sort.no` 99→25、`str.no` 6749→6735、`char.no` 1129→1114。
- **verdict 集合**：基線 DIFF 309 / SAME 338 / 逾時 6 → 終局 **DIFF 313 / SAME 340 / 逾時 0**；**SAME→非 SAME 真回歸 0 檔**；2 檔轉為**逐位元組一致**（`collection/arr-stack.no`、`tests/std-lib.no`）。基線 6 檔逾時實為 8 workers × 20 s 的**假逾時**（`sha256` 14 s / `sha512` 17 s / `x509` 19 s / `yaml` 21 s 等），90 s 逾時重測全部完成。
- **回歸測試**：`compiler/tests/ps-test.no` 由 5 個擴到 **9 個**（9/9 通過），新增：`[n]t.len`、`(dst [n]t)`、`[]t.len`（**無** `GenericParams` —— s65c 的反回歸錨點）、`[]t.eq`（泛型來自 `b []t`）。期望字串全取自 oracle 實測。其餘 compiler 測試同綠：`ast` 47 / `lx` 45 / `tok` 62 / `util` 27。

歸因細節見 `nolang-parser-lowering-invariants.md` §11.1 第 5、6 條。

### 5.3.4 S7 match 表示分歧——形狀歸因與立项規格（2026-10-20 實測；「98%」更正見本節 L2 收斂量化）

機械性缺口已挖盡，**剩餘分歧的『形狀』壓倒性地是單一項**：oracle 輸出含 `SurfaceMatch` 的檔，其差異行數佔總差異行的 98%。全量 653 檔（tests 535 + src/std 118）、313 檔 DIFF 的歸因：

| 桶 | 檔数（佔 DIFF 檔） | 差異行數 | 佔 DIFF 行 |
| --- | --- | --- | --- |
| oracle 輸出含 `SurfaceMatch` | 264（84%） | 218 227 | **98%（按行）** |
| 不含 | 49（16%） | 3 993 | 2%（按行） |

⚠️ **「98%」是『分歧形狀』歸因，不是『第②件收斂量』**（二者不可混淆，實測見本節 L2 收斂量化小節）：第②件只翻轉 45 檔（DIFF 313→268，總 DIFF 僅降 **14.4%**）；剩餘 268 檔中仍有 184 含 match 區塊，主因是 ② 範圍內的 **raw-cond 臂缺口**（s66d 已補，再收斂 1 檔 → 267）與**正交的** option/result unwrap、type-inference 註解分歧（獨立 track）。

抽查最大差異檔 `src/std/crypto/x25519.no`（15 583 行）前兩個 hunk **100% 是 match 表示**：oracle
`ExpressionStatement{SurfaceMatch}` / `SurfaceMatch{arms: 2}` / `MatchArm{wildcard: true, dotVal: true, binding: , bindingVariant: }`，
noc 是 `ExpressionStatement{IndexExpression}` + `IfExpression` 鏈。**結論：match 表示是剩餘分歧的最大單一『形狀』**；但解決它表示層（第②件）只直接對齊核心臂（ok(v)/err(e)/nil/_/binding/dotVal/blockBody），raw-cond 臂與 `?T` unwrap、type-inference 需另立項。（附帶更正：`InterfaceDefinition` 只出現在 4 檔 6 個節點，價值極低，不值得為它動 `asp.no` 的 id 表。）

**目標結構**（`src/parser/dump/dump.go:778` 的 `case *parser.SurfaceMatch`，由 `gen_ast_spec.py` 抽出的規格）：

```
HDR      SurfaceMatch{arms: <len(Arms)>}
COMMENTS
CHILD    Matched          （guarded，nil 則不印）
HELPER   Arms -> matchArm （逐臂）
```

⚠️ **`MatchArm` 不是 Node**（`dump.go:876-877`），沒有 `dumpNode` case，只經 SurfaceMatch 的
HELPER 到達 ⇒ **`asp.no` 不需要新增 kind，`K_SURFACE_MATCH = 65` 已足**。實測
`scripts/gen_ast_spec.py` 重生成後 `compiler/src/asp.no` **逐位元組不變**（同步正常）。

**語料中 `MatchArm` 欄位普查（6 735 個臂，`kv` 恆印、`kvb`/`kvs`/`kvc` 僅非預設值才印）**：

| 欄位 | 出現次數 | 型別 |
| --- | --- | --- |
| `binding` / `bindingVariant` | 6 735（恆印，多為空） | `kv` 字串 |
| `wildcard` | 4 218 | `kvb` |
| `dotVal` | 1 832 | `kvb` |
| `blockBody` | 1 401 | `kvb` |
| `valueCtx` | 89 | `kvb` |
| `typeArm` / `typePattern` / `typeBinding` | 10 / 10 / 10 | `kvb` / `kvs` / `kvs` |
| `rawCond` | 6 | `kvb` |
| `skipIt` / `multiOption` / `multiValue` / `bindingNames` / `bindingFields` | **0（語料完全不出現）** | — |

⇒ 最小切片只需 **6 個 bool + 4 個字串 + 2 個子節點（Condition、Body）**；`MultiValuePatterns` /
`BindingNames` / `BindingFields` / `skipIt` 可先不接線（語料零命中，不影響 L2 對拍）。

**落地拆分（三件，各自可獨立提交）**：

1. **`compiler/src/ast.no` 的 `helper` 接線 `matchArm`**（目前只接了 `taggedEnumField`，其餘記入
   `t.unsup`）。⚠️ 需要擴充 helper 元素的 aux 編碼：現制是每元素 2 個值
   （`aux[hbase+2j]=name-id`、`aux[hbase+2j+1]=tnode`），matchArm 需要 6 bool + 4 字串 + 2 節點。
   因只有 SurfaceMatch 用 matchArm，可按 kind 判別採用寬元素編碼；`d.hcount` / `d.hbase` 的
   預掃（`ast.no:857` / `967`）需同步改為按 kind 取元素寬度。
2. **`compiler/src/ps.no` 的 `ps-parse-match` 改發 `SurfaceMatch`**，env 開關（對齊專案既有的
   `NOLANG_*` 開關慣例，**預設關**）。⚠️ s55/s59/s60 的語義工作（关键字臂綁定、變體臂補集、
   armDoc 收集）**不能丟**：它們的結果要從「合成 `it` let 陳述」改寫進 `MatchArm` 的
   `binding` / `bindingVariant` / `wildcard` / `dotVal` 槽位。
3. **回歸測試 + A/B 量測**：`compiler/tests/ps-test.no` 加 surface 形用例；跑全量 sweep 確認
   「開關關＝零回歸、開關開＝差異大幅收斂」。

⚠️ **新發現的既有缺陷 A（生成器丟碼）**：`scripts/gen_ast_spec.py` **不會**把 `matchArm` 的
helper case 寫進 `src/parser/dump/spec_gen_test.go`——該檔裡那 39 行是**手寫補進去的**。
實測：重跑生成器後 `spec_gen_test.go` **靜默少 39 行**（`asp.no` 卻不變，極易被忽略）。
今後重生成必須 `git diff src/parser/dump/spec_gen_test.go` 複核，或把 matchArm case 挪出生成器覆蓋範圍。

⚠️ **新發現的既有缺陷 B（未接線）**：`ast.no` 的 `helper` 只接 `taggedEnumField`；
`matchArm` 與 `interfaceMethod` 都記入 `t.unsup`（`ast.no:1221` 注釋已記錄）。

**✅ s66（2026-10-20）：落地三件中的第 ① 件——`ast.no` 的 `helper` 接線 `matchArm`**

- 元素編碼採**寬元素**：每臂 **12 個 aux 值**（4 字串 + 6 bool + Condition/Body 兩個節點索引），
  與 `taggedEnumField` 的 2 值編碼按 `hname` 分派互不衝突。因 `HELPER` 是 SurfaceMatch 的
  **最後一條**指令，`cur += hcount` 的過度前進不影響任何輸出 ⇒ **`hcount` / `hbase` 的
  預掃（`ast.no:857` / `967`）不必改動**。
- bool 以字串 `'true'` / `''`（str id 0）表示——nolang 無位元運算，走字串最簡。
- 欄位順序逐項對齊 `dump.go:888-895` 的 `fields(...)`；`binding` / `bindingVariant` 是 `kv`
  （**恆印**，空值也印成 `binding: `），其餘為 `kvb` / `kvs`（僅非預設值才印）。
- **回歸測試**：`compiler/tests/ast-test.no` 新增 `build-surface-match` + `test-match-arm-helper`
  （48/48 通過）。它手工組出 arena，對 oracle 最小復現檔
  `printf 'h[0]: {\n    ok -> it = 0\n    -> _\n}\n'` 的輸出**逐位元組比對**。
  ⚠️ 這是 matchArm 支路**唯一**的可達路徑——ps.no 尚未產 SurfaceMatch，語料 653 檔全部走不到。
- **零回歸實證**：全量 verdict sweep 與 s65c **完全相同**（DIFF 313 / SAME 340 / 逾時 0）；
  ast 48 / lx 45 / tok 62 / util 27 / ps 9 全綠。

⚠️ **新發現的 arena 構造陷阱（無子節點 + CHILD 指令 ⇒ 自別名無限遞迴）**：
`ast.add` 把 `kid0` 設成「當下的 `kids.len()`」。一個**無子節點卻帶 CHILD 指令**的節點
（例：`ExpressionStatement{nil}`）稍後被 `kid-add` 進父節點時，**恰好落在 `kids[kid0]`** ⇒
dumper 的預掃把「自己」讀成首個子節點 ⇒ `ast.node` 無限遞迴（實測 **挂起，rc=137**）。
既有的「建父節點後立刻 kid-add 其子」守則對這類節點**不夠**。
**修法（構造側）**：先 `ast.kid-add(t, n, 0)` 佔住那一格。asm 實測：加這一行即從挂起變正常。
⚠️ 這對 s66 的第 ② 件是**前置約束**：ps.no 一旦開始產 `SurfaceMatch`，臂體裡的
`ExpressionStatement{nil}` 就必須這樣建，否則自舉 binary 直接挂死。

### 第 ② 件的第一次嘗試：已實測否決「一次編輯就能翻」的假設

把「組 if/else 鏈」那段換成「發 SurfaceMatch + MatchArm」後翻開關實測（`z: { 1 -> a = 1 / _ -> a = 2 }`）：

- 臂數 **6 ≠ 2**：臂迴圈的推送次數與 oracle 的臂數不同步；
- 臂體被塞進 `LetStatement{name: it, value: z, synthetic: true}` + `ExpressionStatement{nil}`
  ——這是 ps.no 降糖用的**合成 `it` 綁定**，surface 形**不該有**；
- 主體 `a = 1` 被切成 `ExpressionStatement{a}` 等碎片。

⇒ 證實第 ② 件**不是**末端組裝的局部改寫，而是要在臂迴圈裡做
「**關掉合成 `it` 注入** + 把 s55/s59/s60 的既有語義（关键字臂綁定、變體臂補集、armDoc）
改寫進 `MatchArm` 的 `binding` / `bindingVariant` / `wildcard` / `dotVal` 槽位」。
本次已把 ps.no **完整還原到提交態**（md5 核驗），未留下未驗證的死碼。

⚠️ **開關形式的實測結論（別再踩）**：不能用 env。`# /std/os` 會讓 `no build compiler`
去找 `<root>/std/os.no` 而失敗（實測），而修好它要動 `compiler/package.jsonc` 的
`dependencies` —— 為一個量測開關去改自舉編譯器的依賴圖不划算。
可行替代：**模組級常數**（`PS-SURFACE-MATCH`，翻值 + 重編即可 A/B），或把旗標放進
`psst` 由 `drv.no` 依 CLI 參數填入。

### 第 ② 件還缺的前置：主體形態覆蓋率

`ps-parse-expr-prec` 的 `COLON + LBRACE` 分派臂目前**只認裸 IDENT 主體**
（`ps.no:1945` 的 `ast.kind-at(t, idx) == asp.K_IDENTIFIER`）。但語料 2 975 個
`SurfaceMatch` 的 `Matched` 分布是：

| Matched | 數量 | 佔比 |
| --- | --- | --- |
| `IndexExpression` | 1 523 | 51% |
| **無 Matched**（`Matched == nil`） | 942 | 32% |
| `Identifier` | 509 | 17% |
| `DotExpression` | 1 | ~0% |

⇒ 只做裸 IDENT 主體，天花板約 **17%**；要吃到 51% 必須連 `IndexExpression` 主體一起放行
（並把主體**節點**（而非名稱字串）傳進 `ps-parse-match`）。另 32%「無 Matched」對應裸 match
大括號形（`ps-parse-bare-match`），`Matched` 須留空——`SurfaceMatch` 的 `CHILD Matched`
是 guarded，留空即不印。

**⚠️ 實測：51% 那一桶目前連 `ps-parse-match` 都進不去。**對語料主力形
`h[0]: { ok -> it = 0 / -> _ }`，noc（`1ecb3c81` 後的 binary）根本**不把它當 match**：
因為 `ps.no:1945` 的分派要求主體是 `K_IDENTIFIER`，而 `h[0]` 是 `IndexExpression`
⇒ 落成 `ExpressionStatement{IndexExpression}` + 一個裸 `BlockStatement`（臂被當普通 if 解析）。

**⚠️ 實測：連裸 IDENT 主體的臂迴圈本身也是壞的**（既有缺陷，與本次改動無關）。
`z: { 1 -> a = 1 / _ -> a = 2 }`（oracle 2 臂）在 noc 上產出 **4 臂**規模的結構：
多出 `z == <nil>` 這種臂，且主體 `a = 1` 被切成 `ExpressionStatement{a}`、
`a = 2` 的位置也錯位。⇒ 第 ② 件**不能**假設「臂迴圈是對的、只差末端組裝」。

**第 ② 件的正確拆解（依上述實測重排）**：
1. **放行非 IDENT 主體**：`ps.no:1945` 的分派改為接受 IDENT / IndexExpression / DotExpression，
   並把主體**節點**傳進 `ps-parse-match`（新增形參；`subj` 字串仍供降糖路用）。（覆蓋率 17%→68%）
2. **修臂迴圈**：臂數與臂體歸屬（實測 2 臂被讀成 4 臂、主體被切碎）。這一步可獨立驗證——
   開關關著時，看 `z: { … }` 的 if/else 鏈是否變正確即可。
3. **surface 形關掉合成 `it` 注入**（`ps.no:6234` 的 `kids.push(itl)`）。
4. **把 s55/s59/s60 語義搬進 MatchArm 槽位**（关键字臂綁定 / 變體臂補集 / armDoc）。
5. 裸大括號形（32%）的 `Matched` 留空。

### ✅ 第 ② 件落地（2026-10-20）：`ps-parse-match` 改發 `SurfaceMatch`

依「正確拆解」5 步落地。開關 `PS-SURFACE-MATCH`（`compiler/src/ps.no:110` 模組級常數）
**預設關（0）**保零回歸，翻 1 即走 surface 形；`ast.no` 的 `matchArm` helper（第 ① 件）已就位。

**關鍵修改（均位於 `ps-parse-match`）：**
1. **放行非 IDENT 主體**（步①）：`COLON+LBRACE` 分派放寬為 IDENT / IndexExpression / DotExpression，
   主體**節點**經新形參 `subjNode` 傳入（覆蓋率 17%→68%）。
2. **修臂迴圈**（步②）：`ps-parse-expr-stmt` 頂端新增裸 `_` 陳述處理（產 `ExpressionStatement{nil}` 佔槽），
   修掉 `-> _` 臂的幻影 wildcard 臂（claim B）；內聯臂體改以 `ps-parse-stmt` 解析，主體不再切碎。
3. **surface 形關掉合成 `it` 注入**（步③）：surface 收集區另建 `blkS`（不含 `itl`），與降糖 `blk` 平行。
4. **s55/s59/s60 語義搬進 `MatchArm` 槽位**（步④）：新增 `swA`/`dotValA`/`bindA`/`bvarA`/`condA`/`blkSA`/`bbA`
   陣列，`binding`/`bindingVariant`/`wildcard`/`dotVal`/`Condition`/`blockBody` 逐臂填入。
5. **裸大括號形 `Matched` 留空**（步⑤）：surface 發射分支 `ast.kid-add(t, sm, subjNode)`——主體為 0 時
   `Matched` 因 guarded 自然不印。

**err/nil wildcard 語義修正（本次收口重點）：**
原 `kvar != '' -> sw = 1` 錯把 `err`/`nil` 當 wildcard；對齊 oracle 改為 **`sw = wild; kvar == 'ok' -> sw = 1`**
——`_`/bare→ 與 `ok`/`ok(v)` 為 wildcard，`err`/`nil`（含 `err(e)`/`nil`）一律**非** wildcard 且保留
`Condition` 型樣節點；`bindingVariant` 僅解構綁定名存在時填（`ok(v)`⇒`ok`、`err(e)`⇒`err`）。emit 端
slot 2 由恆 `0` 改讀 `bvarA[i]`。

**實證（A/B 對 oracle `bin/no ast`）：**
| 案例 | 主體 | surface 輸出 | oracle |
| --- | --- | --- | --- |
| `z = 1\nz: { 1 -> a=1 / _ -> a=2 }` | Identifier | 2 臂，pattern 1 + wildcard _ | 一致 |
| `h[0]: { ok -> it=0 / -> _ }` | IndexExpression | 2 臂，ok=bare wildcard+dotVal，_=wildcard，`ExpressionStatement{nil}` | 一致 |
| `x i64=3\nx: { 1 -> {a=1 b=2} / _ -> a=9 }` | Identifier | 2 臂，`blockBody:true` + 非 inline `BlockStatement{}` | 一致 |
| `x i64=0\nx: { ok(v)->y=v / err(e)->y=e / nil->y=0 / _->y=1 }` | Identifier | ok(v)=wildcard+dotVal+binding:v+bindingVariant:ok；err(e)=binding:e+bindingVariant:err+Condition=Identifier{err}；nil=Condition=NilLiteral | 一致（非 ?T 主體亦吻合） |

**零回歸實證：** `PS-SURFACE-MATCH=0` 下 `ps-test.no` 10/10、`ast-test.no` 48/48 全綠；
`=1` 下 9/10（test 10 預期降糖 if/else，屬預期失敗，因 SurfaceMatch 已取代）。
**⚠️ 自舉 harness 兩個別再踩的坑：** ① nolang 字串字面量 `{…}` 被當**字串插值**⇒ 測試來源字串必須
**先存進變數再傳** `parse-dump(src)`，不可內嵌 `{`/`}`（否則 MIR codegen `interp not lowered` 報錯或靜默吐空）；
② **「自舉 `ps.parse` 對 `?i64` 會掛起」是誤判**（2026-10-20 複查更正）：此前觀察到的掛起實為①的字串插值吞源陷阱，
套用①的「先存變數」修法後，`?T` 主體（`x ?i64 = …`）在自舉 `noc ast` 下也能正常解析、不再掛起；
`ok`/`err`/`nil` 的逐位元組對拍既可走 `bin/no ast`（Go parser oracle），也可走自舉 `noc ast`（subject 為 `?T` 亦同構有效）。

### 5.3.4.x L2 收斂量化（第②件 + s66d raw-cond 擴展，2026-10-20 實測）

**方法**：`compiler/src/ps.no` 在 `PS-SURFACE-MATCH=0`（降糖基線）與 `=1`（SurfaceMatch）下，分別
`bin/no build -o /tmp/noc-s0-off compiler` / `bin/no build -o /tmp/noc-s0 compiler` 構建自舉編譯器；
`scripts/selfhost_diff.sh --levels L2 --corpus "tests src/std"` 比對 SUT `no ast` 與 oracle `bin/no ast`
（Go parser 產 SurfaceMatch），語料 = 653 個 `.no`。

**結果（headline）**

| 模式 | SAME | DIFF | 翻轉（DIFF→SAME） | 回歸（SAME→DIFF） |
| --- | --- | --- | --- | --- |
| flag-OFF（降糖基線） | 340 | 313 | — | — |
| flag-ON（SurfaceMatch，核心臂） | 385 | 268 | **45**（100% 含 match 區塊） | **0** |
| flag-ON + s66d（補 raw-cond 臂） | 386 | 267 | **+1**（`tests/minimal-ok.no`） | **0** |

- 收斂：DIFF 313 → 267（**−46**），SAME 340 → 386（**+46**），全程 **0 回歸**。
- 翻轉的 45 檔 100% 含 match 區塊 ⇒ 第②件精準消除「match 表示分歧」且未波及任何其他表示。

**s66d raw-cond 臂擴展（第②件範圍內補洞）**：SurfaceMatch 發射路徑原只處理值綁定臂
`ok(v)`/`ok`/`err(e)`/`nil`/`_`，對裸條件臂 `ok(<expr>)`（如 `ok(it >= 90)`）仍產 `CallExpression{fn: ok}`。
s66d 在 `ps-parse-match` 補：
- 臂首識別 `ok(<expr>)` ⇒ `rawCond: true`，`Condition` = 內部布林表達式（`InfixExpression`），**不綁定、非 wildcard、非 dotVal**；
- `valueCtx` 與 raw-cond **無關**，由**值語境**驅動（見本節 s67）：`rawCond: true` 的臂在陳述語境下 `valueCtx` 仍為 false。s66d 原「`valueCtx` 由 match 層 `matchHasRaw` 驅動」係**錯誤代理**（見 s67 根因），本版已棄用 `matchHasRaw` 驅動 valueCtx，僅留作 rawCond 標記。
- 降糖（if 鏈）末端：raw-cond 臂直接用 `Condition`（不包 `subj ==`）。

**獨立實證（非全量 sweep 推論）**：
- `tests/minimal-ok.no` 在 s66d 前屬 ON DIFF 集，s66d 後 `bin/no ast` 與 `/tmp/noc-s0 ast` **逐位元組一致** ⇒ 淨收斂 +1 檔的邏輯證明（非推論）。
- `tests/match-raw-cond.no` s66d 後 `rawCond`/`valueCtx` 相關 diff 行數 = **0**；剩餘 75 行全為正交的 `?T` unwrap / type-inference 分歧。

**結論 / 更正**：
- 「收斂 98% L2 表示分歧」**未被證實**：第②件 + s66d 總共只降總 DIFF 的 **14.7%**（46/313）。
- 殘餘 267 DIFF 主因是**正交的** option/result `?T` unwrap lowering 與 type-inference 註解分歧（獨立 parser 收斂 track），非 match 表示本身。
- 第②件與 s66d **零回歸**（0 個 SAME→DIFF），`PS-SURFACE-MATCH` 維持預設 `0` 安全。

### 5.3.4.z s67 valueCtx 值語境修正（2026-10-20，本輪）

**根因（推翻 s66d 假設）**：s66d 用 match 層 `matchHasRaw`（「含任一 raw-cond 臂」）驅動 `valueCtx`，
實為**錯誤代理**。實測 oracle（`bin/no ast`，top-level 陳述）證實 `valueCtx` 與 raw-cond **無關**：
- `r = n: { -> 1 }`（**無** raw-cond 臂，單純賦值右值）⇒ oracle 印 `valueCtx: true`；
- `n: { -> 1 }` 作**裸陳述** ⇒ oracle **不**印 `valueCtx`；
- `x = f(n: { -> 1 })` / `x = [ n: { -> 1 } ]` / `x = S{ y: n: { -> 1 } }`（巢狀 call-arg / 陣列·結構元素）⇒ 同為 `valueCtx: true`（黏性繼承）。

對齊 Go `src/parser/desugar.go:146` `validateMatchArmReturns`：該函數**只在值語境（CTX_EXPR）被呼叫**，
據此把每臂標 `valueCtx = true`（`src/parser/match.go:844`）。`CTX_EXPR` 是 Go `parseLetStatement` /
`parseUnwrapAssignStatement` / `parseCompoundAssignStatement` 三處 `p.ctx.push(CTX_EXPR)` 閘門，且
**對齊堆疊具黏性**——賦值 RHS 內的巢狀 call-arg / 陣列·結構字面量元素 / 二元運算元皆繼承值語境。
（return / call-arg 頂層 / if·for 條件**不** push CTX_EXPR ⇒ 那些語境的 match `valueCtx` 為 false，與本實作一致。）

**修法（compiler/src/ps.no）**：不 thread 形參（避免動 `ps-parse-expr-prec` 38 處遞迴呼叫點），改用 `psst.valCtx` 欄位：
- `new` 結構體加 `valCtx: 0`；`ps-parse-stmt` 開頭重置 `p.valCtx = 0`（每條陳述隔離）。
- 賦值 RHS 的 7 個解析點（`ps-parse-let` 主路 + 明示型別 / 定長陣列 / 推論陣列 / 切片 / 可空型別 / 多重指派標註）
  在 `ps-parse-expr` 前 `vcSave = p.valCtx; p.valCtx = 1`，後 `p.valCtx = vcSave`。
- 內聯 `obj.x = expr`（`K_ASSIGN_EXPRESSION` 路徑）**不**設值語境——對齊 Go `parseExpression` 不在該路徑 push CTX_EXPR。
- `ps-parse-match` 發射端第 valueCtx 槽改讀 `p.valCtx`（棄用 `matchHasRaw` 代理）；`matchHasRaw` 僅留作 rawCond 標記。

**收斂預期與狀態（已複測，2026-10-20 末）**：
- ⚠️ **本節舊版「toolchain 解鎖後補」歸因已迴轉**：`bin/no build compiler` 報 `EmitLLVM: setfield field`
  **不是** toolchain 漂移 / pre-existing 回歸，而是 **s67 自己引入**——`3c0df878` 在 `psst` 字面量與 7 處賦值
  用了 `valCtx`，卻**漏在型別定義宣告欄位** ⇒ codegen 找不到 field offset。修法 `7015c7ef`（補 `valCtx i64`）
  ⇒ 自舉構建 **Build successful**。全 39 commits（s67 + 修 + `?T` unwrap track 設計 + 並發 JS 後端）已推
  `origin/main`（`a58a907c`），`git ls-remote` 核實一致。
- **L2 全量複測**（乾淨 worktree，CTL=Go `no`@HEAD → `/tmp/noc-ctl`；SUT-ON=ps.no `PS-SURFACE-MATCH=1`
  → `/tmp/noc-s67fix`；SUT-OFF=`=0` → `/tmp/noc-s67off`；語料 `tests`+`src/std` 653 檔）：

  | 模式 | SAME | DIFF |
  | --- | --- | --- |
  | OFF（降糖基線） | 340 | 313 |
  | ON（SurfaceMatch + s67 valueCtx） | 385 | 268 |

  - 表示切換效應（同 CTL，ON vs OFF）：**+45 SAME / −45 DIFF**（45 檔 100% 含 match 區塊 `: {`）。
  - **s67 valueCtx 修正的 L2 效應 ≈ 0**：ON 檔數與 s66d 舊表（386/267）幾乎相同 ⇒ s67 是**語意正確性**修正，
    非收斂量修正。
  - **剩餘 DIFF 中 valueCtx 失配：0 / 268**（對 268 DIFF 檔逐檔 `diff <(CTL ast) <(SUT ast)` 抓 `valueCtx`）
    ⇒ s67 **已徹底消除 valueCtx 表示分歧**；剩餘 268 全為正交分歧（`?T` unwrap / raw-cond 臂缺口 / type-inference）。
- 「收斂 98% L2 表示分歧」說法**仍未被證實**：ON 僅比 OFF 少 14.4% DIFF（45/313），總 DIFF 仍 268/653=41% 未收斂。
- 兩輪 sweep 均過 G1/G2/G3/G6/G7（ctl_ok=653、oracle 71/71 節點全覆蓋、binary 無 drift）；`RESULT=FAIL` 僅因未全收斂。

### 5.3.4.w S7 裁定①續——match 表示缺口 A→H 全鏈修（2026-10-21 本輪）

裁定①（SurfaceMatch 預設開）落地後 L2 全量 SAME=385/DIFF=268。本輪以「MatchArm 多重集差分」
逐缺口歸因並修至 **SAME=510/DIFF=143**，match-表示殘差 **193 檔 → 6 檔**。

**歸因方法**：對 DIFF 檔抓 MatchArm 頭欄位多重集——count_gap（臂數不符）/ flag_gap（臂數齊但旗標異）/
clean（MatchArm 全同，分歧在他處）三類。三輪實測：

| 里程碑 | 修法 | SAME/DIFF | match 殘差 |
| --- | --- | --- | --- |
| 裁定①基線 | PS-SURFACE-MATCH=1 | 385/268 | 193（count 169 + flag 24） |
| A+B+C1 | 裸 match surface 發射 + valCtx 快照 + blockMatch 內層裸 match | 488/165 | 40 |
| +C+D | 型別臂 `v T ->` 三槽 + 變體臂保持 CallExpression | 497/156 | 29 |
| +E+F | NEWLINE 臂體形/空臂體 + range 型樣 `[a..b)` | 505/148 | 9 |
| +G+H* | `?T` 裸宣告 + blockMatch 回退提取 + 分類器修正 | **510/143** | **6**（count 5 + flag 1） |

**缺口清單（本輪全部實測釘死）**：
- **A 裸 match surface 發射**（~2401 臂）：`ps-parse-bare-match` 原走 desugar 組裝、無 SurfaceMatch
  發射。補 surface 發射段 + `ok->` 的 wildcard/dotVal + Matched 占位 `ast.kid-add(t, sm, 0)`
  （防 kid0 別名 → dump 無限遞迴 SIGSEGV，見 §5.3.4.z 同源教訓）。
- **B valCtx 讀點**（85 臂）：`ps-parse-stmt` 每陳述重置 `p.valCtx` ⇒ 發射尾讀必 0。入函快照 `mv`。
- **C 型別臂 `v T ->`**（12 臂）：generic expr 臂吞 `v T` 兩 IDENT。新 pattern-dispatch case
  ⇒ typeArm/typePattern/typeBinding 三槽（expr.go:1152-1160）。
- **D 變體臂 `circle(r) ->`**（~14 臂）：parse-time 歸一與 oracle（lowering 期歸一）相反。
  `PS-SURFACE-MATCH==1` 跳過 caseB ⇒ 保持 CallExpression 型樣、binding 空。
- **E NEWLINE 臂體形**：`->` 後 `ps-skip-trivia` 吞換行 ⇒ expr.go:1251-1376 第二支永不可見。
  補 NEWLINE 形（blockBody=true **且** inline=true；空臂體合法 `err ->\n\nnil ->`）+
  `ps-is-opt-pat-start`（Go isOptionPatternStart）收束判據 + inline 形空臂體 fallthrough。
- **F range 型樣 `[a..b)`**：LBRACKET primary 一律建 SliceLiteral ⇒ 誤吞成 SliceLiteral{4}。
  鏡像 Go parseSliceLiteral range 分支（ELLIPSIS 於首元素前後 ⇒ RangeExpression）。
- **G `name ?T` 裸宣告**：`ps-expect(ASSIGN)` 失敗不退 ⇒ 後隨陳述被吃成 LetStatement.value
  且 valCtx=1 污染 MatchArm.valueCtx（match-test.no）。鏡像 ps-parse-typedecl 雙形口徑。
- **H/H2/H3/H4 blockMatch 回退提取 + 分類器**：明示臂 LBRACE 體在 classify==blockMatch 但內層
  裸 match 失敗時，oracle 落 parseStatement 的 parseBlockStatement 回退並【提取】陳述——
  IsBlockBody 保持 **false**、IsInline=true（err-binding.no `err(m) -> { b: {…} … }`）。
  連帶修正 `ps-bm-scan` 兩處與 oracle classifyBlockAtCurrent 的分歧：
  - **H2** `IDENT : {`（labelled block/match）⇒ blockMatch（舊 excl 誤擋為結構）；
  - **H3** `IDENT =` / `IDENT ,` 起頭塊 ⇒ Go blockEnum≠blockMatch（語句起頭，箭頭掃描誤觸 10 檔回歸）；
  - **H4** 箭頭掃描【同行】止步（NEWLINE/SEMICOLON 即停，鏡像 Go scanForArrowAtDepth0）。
  - 另補 `bodyDone = 1`：LBRACE 體收畢未置旗 ⇒ NEWLINE 形分支誤觸發把 hd2 抬回 1。

**坑（新記錄）**：
- ⚠️ `ps-bm-scan` 的「有 depth-0 `->` ⇒ blockMatch」啟發式與 Go 的**形狀分類**根本不等價——
  語句起頭塊（`a = …` / `a, b = …` / `f(x)…`）內的後續獨立 if `->` 會誤觸。H3/H4 修兩類，
  `IDENT LPAREN`（Go blockIface）與 `IDENT NEWLINE`（Go blockUnknown）兩形仍待補（語料未見，
  見 §11.1 記帳）。
- ⚠️ `scanned/bodyDone` 雙旗標缺一即互踩：LBRACE 路徑收畢體後**必須** `bodyDone = 1`，
  否則下方 `hd2 == 0 && bodyDone == 0` 的 NEWLINE/inline 分支重入。
- 回歸放大教訓：gap H 首版（無 H3/H4）修 5 檔砸 10 檔——**掃描式分類器的改動必須全量 L2 驗**，
  單點冒煙不足。

**殘差 143 的結構（2026-10-21 末）**：match-表示 **6 檔**（count_gap 5：bzip2/http2/str/txt/yaml
——複雜主體 `expr[l]: {…}` match 陳述被吞、str/txt 反向多臂；flag_gap 1：build-mo）；
clean 137 全為正交軌（`?T` unwrap、type-inference、GenericParams 渲染、warnings 計數、Doc 註解歸位）。
bzip2 抽驗：3 個 `tmp-base[l]: { ok -> …; -> _ }`（IndexExpression 主體）整組消失——
「複雜主體 match 陳述」是下一缺口類（I）。

**回歸測試**：ps-test.no 13→19（test 11-19：bare-match surface / ok-dotval / valueCtx 快照 /
typeArm / 變體型樣 / NEWLINE 空臂體 / range 型樣 / `?T` 裸宣告 / blockMatch 回退提取）。
五套件全綠：ps 19 / ast 48 / lx 45 / tok 62 / util 27。


### 5.3.4.y S7 裁定①續二——缺口 I 收尾 + 迴圈行內體根因（2026-10-21 本輪）

**s7j 常態化結果（ps-arm-blk-match 臂體門）**：SAME=513 / DIFF=140 與 s7i 同，
但 MatchArm 多重集歸因翻轉——**count_gap 4→1**（str 97/97 組、txt 35/35 組、bzip2 全清零；
僅 yaml 殘：SM 27→24、MA 103→98）。其餘 139 檔 MatchArm/SurfaceMatch 多重集恆等（正交）。
match-表示殘差 = **yaml + build-mo（valueCtx: true 缺旗標）2 檔**。

**yaml 下計數根因——不是 match 門，是「迴圈行內體」偷括號**：

1. 症狀面：yaml 整檔 dump 發散 13,831 行，3 個裸 SurfaceMatch 組（chomp 3 臂組 +
   兩個 `content.len-bytes() > 0` 單臂組）+ 5 個 MatchArm 整組消失；SUT 將其降級為
   `BlockStatement`+IfExpression 鏈，且最內層 wildcard 臂 `-> {…}` 被吸附成內層 if 的 else。
2. **結構面（真正根因）**：FunctionDefinition 縮排普查顯示 SUT 從 `parse-plain` 起全部函式
   +2 層、從 `parse-block-scalar` 起 +9 層（後續函式被吞進前函式 for 迴圈體）。
   最小複現 **c2**（純函式級 `k <- [0..pending): content = content - '\n'`）：
   行內體被 parse 成**開區間區塊**，吞掉後隨兩條陳述並偷走函式收束 `}`，
   連頂層 `r = f(0)` 都被吞進函式體。
3. 根因鏈（ps.no 兩缺陷複合）：
   - `ps-expect` 對不符 token **靜默放行**（`-> {}`）；
   - `ps-parse-block` 是**開區間**（`(peek != RBRACE && peek != EOF)` 迴圈吃到 `}`/EOF）。
   ⇒ `for v <- range: stmt`（Go parseLoopBody「Single-statement body」形）在 ps.no
   落入塊模式後一路吞到下一個 `}`——外層收束被偷、括號全面錯位、裸 match 在錯位上下文中
   被逼回 parseBlockStatement 回退形（chomp 組因此整組丟失）。
4. **修法**：新增 `ps-loop-body`（鏡像 Go stmt.go:2386-2411 parseLoopBody）——
   `{ … }` 塊體照舊；否則**只 parse 一條陳述**包成 `BlockStatement`+slot0='true'
   （dump `inline: true`）。三處迴圈本體位（ps-parse-for 裸形 / ps-parse-for-kw
   isRange 形 / 條件形）全改走它。⚠️ 附帶修正：條件形 deprecated 警告**有冒號時不發**
   （Go stmt.go:2586-2612 `!hasColon` 閘；c5 探針 SUT 多報 warnings: 1）。
   ⚠️ **新坑**：nolang 中**兩條獨立 `cond -> …` 語句的條件各自即時求值**——若首條分支
   改變 parser 狀態，次條重讀 peek 已不同。`ps-loop-body` 首版用
   `peek==LBRACE -> {…}` / `peek!=LBRACE -> {…}` 兩條語句被此坑咬（c3 塊體形回歸），
   改「先擷取 isBlk 旗標再分支」（同 `hd` 慣例）後全綠。

**實據（探針 a–c7 + yaml 全檔）**：
- 行內體七形（a 裸 match 全形 / b more==false 形 / c1-c2 迴圈行內體 / c3 塊體 /
  c5-c6 關鍵字形 / c7 單陳述邊界）修後全 SAME；c4（`cond: stmt` 非迴圈形）殘 2 行——
  Go 在陳述層吐 `ExpressionStatement{nil}`，SUT 漏吐（獨立小缺口，記帳）。
- **yaml 全檔：13,831 行發散 → 1 行**；SurfaceMatch 27/27、MatchArm 103/103
  （3 組 + 5 臂全數回歸）；FunctionDefinition 縮排全檔歸 4（級聯消除）。
  殘 1 行為正交（`type: ?i64` 註解渲染 + 多一個 `AnnotationStatement{entries: 1}`）。
- 判別力實測：s7j 舊碼對 test 21 語料產 20 行發散（舊碼必 FAIL）、新碼 0 行。

**回歸測試**：ps-test.no 20→**21**（test 21：`for v <- range: stmt` 行內體只綁一條陳述 +
`inline: true` + 不偷外層收束）。五套件全綠：ps 21 / ast 48 / lx 45 / tok 62 / util 27。
L2 全量（noc-s7k）掃描見本輪末記帳。

**s7k 歸因翻轉（循環體修後）**：SAME=514/DIFF=139；match-表示殘差 **2→1 檔**
（yaml 3 組+5 臂全數回歸、滑入正交集；僅存 build-mo valueCtx）。

**build-mo（最後一檔）根因——RHS 位裸 match 運算式缺失**：
語料形 `result = { x == 0 -> 1; x == x -> 0 }`。Go expr.go:369-386 primary 位
LBRACE 臂：blockStruct → 匿名 struct literal／blockMatch → `parseBareMatchExpr()`／
其他 → 吃 `{` 回 nil。ps.parse-expr-prec **primary 位無 LBRACE 臂** ⇒ RHS 回空
（`LetStatement{value: ∅}`）、裸 match 落回陳述位重解析（valCtx 已被 869 行陳述起始
重置歸零）⇒ 臂 valueCtx 全丟。**修法**：補 LBRACE primary 臂——
`ps-is-struct-lit` → `ps-parse-struct-lit(t, p, '')`（匿名形 type 空）／
`ps-bm-scan` → `ps-parse-bare-match`（入函 `mv` 快照驅動 valueCtx，5251 輔助位）／
否則 bump 回 nil。⚠️ 同 ps-loop-body：**閘先擷旗標再分支**（解析改 p.pos 續列重評必錯位）。
實據：build-mo 13 行發散→**0**；匿名 struct literal primary 同修後亦 0；
ps-test 21→**22**（test 22：RHS 裸 match 掛 value + valueCtx: true，判別力 13 行）。
match-表示殘差 **清零**（138 檔 MatchArm/SurfaceMatch 多重集恆等全正交）。

**本輪 L2 收官（noc-s7m 雙修後全量）**：SAME=**515** / DIFF=**138**（輪初 513/140），
ctl_ok=sut_ok=653。多重集歸因：**match-表示殘差 0 檔**——138 檔 DIFF 的
MatchArm/SurfaceMatch 多重集全部恆等，純正交軌（`?T` unwrap 合成 desugar、
type-inference 註解渲染、AnnotationStatement 歸位、warnings 計數、Doc 歸位、
`cond: stmt` 陳述層 `ExpressionStatement{nil}` 小缺口等）。
S7 match-表示收斂線至此**收官**；後續轉入正交軌（G6 五級門禁擇機開）。


### 5.3.4.aa 正交軌首件——IndexExpression 元素型別推斷 + `tn` 同名靜默誤編（2026-10-21 本輪）

**正交軌開工**（S7 match 表示收官後）：138 檔 DIFF 按 dump marker 差異分類——
inferred-only 29 / synth+armBind+nullableT 28 / cap+synth+prop+armBind+inferred+nullableT 25 /
無 marker 差 19 / 其餘小類（annot 3、warnings 2、generic 3）。首件攻 inferred-only 類的
IndexExpression 元素型別推斷（Go stmt.go:1462-1494）。

**缺口**：`v = [1,2,3]`（全域）後 `e0 = v[0]`，Go 從**全域**型別表讀 receiver 型別推元素
型別並建 `NamedType{i64, inferred}`；ps.no 無此臂 ⇒ `LetStatement{value: IndexExpression}`
裸奔（無 type 行）。with-cap.no 的 9 行正交缺口（s7m 時代即有）正是此形
（`e0/e1/e2 = arr[i]`）。

**實作**（三件，鏡像 Go 口徑）：
- `ps-g-var-type`（ps.no:2302）：**全域表只讀**——Go setVarType 在函式內只寫
  FuncVarTypes（parser.go:1113-1129），IndexExpression 臂直接索引全域 `p.sem.VarTypes`
  ⇒ 函式內區域 receiver 讀不到就不推斷（與 Go 同口徑；p-idx-b 實測 0 行）。
- `ps-idx-elem-of`（ps.no:3392）：`[]X`→剝前綴；`[N]X`→第一個 `]` 之後；非陣列/切片形
  （含長度不足）⇒ `''`。⚠️ 算術一律預算（內聯算術作實參會污染呼叫 ABI，gap I 同款教訓）。
- ps-parse-let 的 IndexExpression 臂（ps.no:2936）：僅在 LHS 尚無型別時設定
  （FuncVarType 語義 = ps-var-type 前置查），並回寫型別表供後續推斷鏈。

**🔴 加碼即遇災難性回歸——`tn` 同名靜默誤編（本輪最大收穫，編譯級 bug）**：

症狀：`x = 5` 竟輸出 `type: 3` + `NamedType{name: 3, inferred: true}`——i64/f64/str→`3`、
char/bool/byte→`4`（數值＝**型別名長度**！），所有 let 推論型別全污染；而同構的切片字面量臂
（ps-elem-type-of）卻完好。bisect 鏈（/tmp 變體構建法見 `MEMORY.md` 鐵律節）：
- E2（只刪臂、助手全留）/ E3（+刪 ps-idx-elem-of）/ E4（全刪＝s7j 源）：三者 p-types/r3
  **全部恢復 SAME** ⇒ 助手無辜，**觸發點是「臂的存在」本身**——連
  `&& ps-idx-elem-of('') == 'never'` 永不真的守衛版也照壞。
- E5（臂保留、臂內 `tn`→`tnx`）：**即修**。

根因：**同一 match 的不同臂宣告同名局部變數（本臂 i64 `tn` vs catch-all 的 str `tn`）
⇒ codegen 把兩個 `tn` 串成同一槽位並統一型別**——catch-all 的
`tn = ps-infer-type(...)` 回傳值被按 i64 截斷，剛好落在字串描述的長度欄
（'i64'→"3"、'char'→"4"）。**checker 完全不報、編譯通過、執行不崩**——最危險的一類
靜默誤編。ps.no:2936 臂內已立註釋警示；寫新臂的節點變數一律加唯一後綴（如 `tnx`）。

**實據（noc-s7l 全探針）**：p-types / p-r3 / p-slice / p-idx-a（全域 []i64→i64）/
p-idx-b（函式內 receiver 不推）/ p-idx-c / p-idx-t（[]char→char）全 SAME；
**with-cap 9 行正交缺口順手關閉**；src/std/yaml.no 與 E4（無本修）輸出 **0 行差**
（217 行殘差為 s7m 時代既有基線，與本修無關）；tests/yaml.no 0 行。
ps-test 22→**23**（test-let-idx-elem-infer：全域 []i64→i64 / []char→char / 函式內
receiver 不推三形態一網打盡；判別力：s7j 舊碼對 e0/e2 兩形態缺 type 行）。
五套件全綠：ps 23 / ast 48 / lx 45 / tok 62 / util 27。


### 5.3.4.ab L2 現狀複測與 WIP mass-noop（2026-10-24 本輪）

**複測（CTL=`bin/no`、SUT=`/tmp/noc-se`、語料 `tests`+`src/std` 653 檔、`no ast`）**：

```
files=653  ctl_ok=653  ctl_empty=0  sut_ok=653  sut_empty=0
SAME=533  DIFF=120     失敗集合 ctl=0 sut=0
oracle 覆蓋：0 行 <unknown>（71/71 節點型別全覆蓋）
```

- 相較 s7m 收官的 515/138 **又前進 18 檔**（533/120），精確歸因見
  `docs/design/nolang-parser-lowering-invariants.md` §13.2。
- **match-表示殘差 = 0 檔**：120 檔 DIFF 全部 `MatchArm`/`SurfaceMatch` 多重集恆等
  （count_gap 0 / flag_gap 0 / clean 120）⇒ **gap I 已關閉**，S7 match 收斂線確認收官。
- 120 檔正交軌按 marker 簽名分 12 類（最大類 `nomarker` 35、
  `synth+armBind+nullableT+idxout` 32）。**含 `synth` 71 檔（59%）、含 `cap` 33 檔（27.5%）**
  ⇒ **S3-c（`__cap_N` 不再注入 AST）是自舉收斂的最大單點槓桿**，落地即自動消掉 33 檔。
  此前 S3（V4 合規）與 S7（自舉收斂）被當兩件事分開跟蹤，是漏看的協同。

**🔴 從當前工作樹構建的 SUT 是 mass-noop（本輪發現）**：

以工作樹 `compiler/` 構建 `/tmp/noc-now` 跑同一條 L2 ⇒ **SAME=0 / DIFF=653**，
且 SUT 的 stdout 雜湊對 653 檔**全部相同**（`4fde700016d5`）。直查：
**對任何輸入只吐 `Program{}`（10 bytes）**；`noc-se`/`noc-s7l` 同檔吐 2148 bytes。

**⚠️ 歸因更正（同輪晚些實測推翻初判）**：初判歸給 `compiler/` 的 1465 行 WIP，**錯了**。
二分實測：WIP `bin/no` + **HEAD 乾淨 `compiler/`**（`git worktree` 導出）⇒ **同樣 mass-noop**。
⇒ 換掉自舉側原始碼毫無改變，根因在 **Go 側 `src/` 的 106 個未提交 `.go`**
（parser 40 / checker 28 / build 14 / mir 10 / hir 5 / fmt 4 / mmast 2 / builtin 1）。

**根因鏈（探針逐層釘死）**：`fs.read-bytes` → `nil`（應為 ok）；`fs.open` → `ok`（開檔正常）；
`it.read-bytes()` → **`nil`**（← 失敗點）；而 `f = () (r ?i64) { r = 42 }` 解包 → `OPT=ok`
（option 機制本身正常）。⇒ 失敗點是 `src/std/fs.no:607` `file.read-bytes()` 的
`size == 0`（:615）／`total == 0`（:639）分支 ⇒ `fstat-size` 或 `read` 在 WIP 的 MIR 後端下取不到值。
**✅ 已判定為 WIP 新引入的回歸**（補做正確對照：HEAD 全棧 `BRANCH=ok` vs WIP `BRANCH=nil`）。
上一輪寫的「對照不成立」是**實驗缺陷**——複製 WIP 的 `std_ast.bin` 去配 HEAD 的 `src/std` 導致
`unknown callee`；正確做法是在 worktree 內就地 `go run ./checker/genstdsig` 生成該版自己的 `std_ast.bin`。
**下鑽結果**：`open` → ok、`fstat-size` → ok 且 `SIZE=NONZERO` ⇒ 排除 `size == 0`（fs.no:615），
命中 `read(.fd, buf[total..], …)` 回 ≤0（fs.no:631）⇒ `total == 0` ⇒ nil。
`src/builtin/os.go` 未被 WIP 改動 ⇒ 問題在 MIR 側對該 CLibCall 的發射路徑（`src/mir/codegen.go` +221）。

**自舉側次生缺陷（獨立、值得修）**：`compiler/src/drv.no:116` `drv.cmd-ast` 對 `read-bytes`
的 `nil` 分支設 `got = true` 卻不報錯 ⇒ 「檔案為空」被吞成「成功讀到空檔」，`cmd-tokens` 同形。
**正是這個靜默把一次 I/O 回歸放大成 mass-noop**。

兩點教訓：
1. **L2 是手動門禁，不在 CI 內** ⇒ 此 mass-noop 自 2026-10-22 起無人發現。
   `selfhost_diff.sh` 的 G7（「控制組只吐一種 stdout ⇒ oracle 無鑑別力」）**只查 CTL 側**。
   **已補 G8（2026-10-24）**：SUT 在整批語料只產生 1 種 stdout ⇒ `MASS-NOOP`，
   SUMMARY 視為 FAIL 並額外說明「不是 N 個獨立差異」。與 G2（SUT 全掛 rc≠0）分工：
   G8 抓「每檔都跑得動、卻吐同一份東西」——更危險，因為它看起來像有訊號。
   驗證：mass-noop SUT ⇒ 觸發 ✅；`noc-se` ⇒ 不觸發（SAME=10/DIFF=10）✅；
   `--self-test` ⇒ 不觸發（SAME=20 PASS）✅。
2. **任何 S7 收斂數字都必須先確認 SUT 不是 mass-noop**（`sut_ok=653` 但雜湊單一 ⇒ 有訊號是假的）。

**建議排期**：先修主編譯器 WIP 的檔案 I/O 回歸（`file.read-bytes` 回 `nil`，**不是**自舉側）
→ 再做 S3-c（收 33 檔）→ 再推安全索引降糖同類遷移（再收 ~26 檔）。

> 附帶：§5.3.4.y 引用的「§11.1 記帳」在本文件**不存在**（無 §11 章節）⇒ 引用斷裂，待補。

### 5.3.4.ac s8q pipeline 同位差——`let` 右側 `->` 鏈包成 value（2026-10-27 本輪）

**背景**：str.to-f64 MIR 回歸由平行 session 修復後，工作樹 `compiler/` 首次可建。
複測 L2（CTL=`bin/no`、SUT=`/tmp/noc-s64`、語料 `tests`+`src/std` **654 檔**、`no ast`）：
**SAME=640 / DIFF=14**（較 §5.3.4.ab 的 120 已因多條正交軌收斂大幅下降）。14 檔中
**同位差家族 = 2 檔**（`tests/pipeline-value.no`、`tests/stmt-boundary.no`）——為本輪攻擊目標。

**同位差形狀（探針 `/tmp/pipe-probe.no` 實測钉死）**：`x = A -> B` 中【整條鏈】是 let 的
value。Go（`src/parser/stmt.go:1242-1260`，`parseLetStatement`）在 `stmt.Value = parseExpression`
後，若【同位】緊接 `RARROW`（且非 `CTX_MATCH_ARM`/`CTX_FOR_COND`）就把 value 包成
`IfExpression{Condition: value, Consequence: 獨立 body, Alternative: 同列 else}`，並掛
`RTPipelineValue` 旗標；鏈 `a -> 8 -> 9` 右接為 `if a { if 8 { 9 } }`。舊 `noc` 缺這一步：
`ps-parse-let` 讓 let 只吃掉 `A`，殘留的 `-> B` 落到 `ps-parse-expr-stmt` (A) 被當成
**wildcard 獨立陳述**（`ExpressionStatement{IfExpression{cond: IntegerLiteral{1}}}`）——
既錯位（多一個頂層陳述）、又錯 cond（wildcard 標記 1 而非 `A`），且推論臂把 `p = a -> 7`
的 `a` 誤傳播成 `type: i64`。

**修法（`compiler/src/ps.no`，兩處同位規則）**：
1. `ps-parse-let`（推論型 `name = expr`）：`val = ps-parse-expr` 之後、型別推斷之前，加
   `val > 0 && p.inMatch == 0 && ps-peek(p) == tok.RARROW -> val = ps-mk-standalone-if(t, p, val)`。
   復用既有 `ps-mk-standalone-if`（已含 wrap-chain 遞迴 + 換行 `-> else` 續接，語義與 Go 一致）。
2. `ps-parse-stmt` 明示型別臂（`name T = expr`）：同一行守衛——對齊 Go 統一的
   `parseLetStatement`（Go 只有一個函式、型別有無皆走同段 pipeline 包裹）。語料無此形，純為規則完備。

包覆後 `val` 變 `IF_EXPRESSION` ⇒ 下游型別推斷各臂（Slice/Struct/Index/IDENT/Call）自然不命中，
`p = a -> 7` 不再誤推 `type: i64`（對齊 oracle）。⚠️ 守衛用【活】`ps-peek`（不跳 trivia）——
`a \n -> b`（arrow 換行）不構成 pipeline let，另起 wildcard 陳述（已由單元反向看守）。

**歸因（同一 harness、同一 CTL、654 檔，A/B `noc-s64`→`noc-s66`）**：**DIFF 14→12 / SAME 640→642**；
`pipeline-value.no`、`stmt-boundary.no` **轉 SAME**；**SAME→DIFF 真回歸 0 檔**；其餘 12 檔
（bigint/byte/sql/dom/http2/number/toml/vec/yaml/2/match-test/vec-elem-ownership）皆正交家族
（InterfaceDefinition 解析、`[?]byte` 參數型別、warnings 計數、decode-bit 巢狀等），本輪不動。

**門禁**：
- L0 單元：`compiler/tests/ps-test.no` 新增 `test-let-pipeline-value`（推論/鏈式/明示型別三形）
  與 `test-let-pipeline-no-newline`（換行反向看守），want 逐位元組取自 `bin/no ast`。
  `no run -cc zig compiler/tests/ps-test.no` ⇒ **25/25 通過**。
- fixture：`tests/pipeline-value.no`（本家族語料）L2 轉 SAME。
- vet：`no vet compiler/src` 殘留 19 ERROR 全為平行 session 的 WIP（`ps-is-builtin-type`
  重定義 @2854/6241、index-write lint @5210-5218/7676-7684），**本輪改動區（@3378、@1290）0 新增**。

**🔴 工具鏈附註（本輪構建必經）**：工作樹 `bin/no` 用 `/usr/bin/ld`（clang 17）連結時炸
`ld: tapi error: malformed file`——`MacOSX27.0.sdk` 的 `libSystem.tbd` 含 `arm64e.x1-macos`
架構記號，現存所有 `ld`/Xcode ld-1230 皆無法解析（SDKROOT/DEVELOPER_DIR/isysroot 覆寫無效）。
**解法：改用 `zig` 作連結器**（自帶可用 lld）：
```
export PATH="/opt/homebrew/opt/llvm/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export ZIG_GLOBAL_CACHE_DIR=/tmp/zig-cache ZIG_LOCAL_CACHE_DIR=/tmp/zig-cache  # 沙箱禁寫 ~/.cache/zig
./bin/no build -cc zig -o /tmp/noc-sNN compiler
```

### 5.3.4.ad s8r module-path 接收者的呼叫回傳型別推斷（2026-10-27 本輪）

**背景**：§5.3.4.ac 收官後 L2 剩 **12 檔 DIFF**。按 root cause 歸因分組，本輪攻「呼叫回傳型別推斷」家族 A（dom/toml）。複測基線：SUT=`/tmp/noc-s66`、654 檔、DIFF=12。

**缺口形狀**：`let x = <Mod>.<fn>()`（同檔以限定名定義的方法，如 toml.no:55 `toml.new = () (doc toml-doc)`）的【回傳型別推斷】。Go `returnTypeFromCallExpr`（type.go）對 DotExpression 呼叫有兩段：①`resolveReceiverType(recv)` 非空 → 查 `methodSignatures[recvTy.method]`；②recv 是 IDENT 但【非變數】→ **module-path 分支**，以全名 `recv.method` 查簽名表。noc 的 s58 臂【只實了①】（`rty != ''` 守衛），②整段缺失 ⇒ `x = <Mod>.<fn>()` 的 let 不帶型別，oracle 帶。

**修法（`compiler/src/ps.no`，s58 dot 臂新增 `rty == ''` 分支，標記 s8r）**：接收者 IDENT 且 `ps-var-type(recv)==''` 時，把它當套件限定名，組 `mq = recv + '.' + method`，`ps-msig-find(p, mq)`（`toml.new` 於 ps-parse-method-func 已註冊為 `ms:toml.new`），命中即克隆型別起點重跑 `ps-parse-type` + `ps-filter-ok` + 型別表登記（完全鏡像 rty!='' 臂與 Go 尾語意）。⚠️ 本臂區域變數一律 **q 後綴**，避免與 rty!='' 臂的 tn3/ts3/keep2 同名串型別（s7l 教訓）。裸名表 ps-sig-find 的 s8j 後綴掃描【不】用於此（其鍵為字面 `toml.new`，非 `.new` 結尾）。

**歸因（同一 harness、同一 CTL、654 檔，A/B `noc-s66`→`noc-s67`）**：**DIFF 12→10 / SAME 642→644**。轉 SAME 兩檔：`src/std/toml.no`（`doc = toml.new()` → `type: toml-doc`）、`src/std/yaml.no`（`s = <Mod>.<fn>()` → `type: str`，同根因）。**SAME→DIFF 真回歸 0 檔**。**⚠️ 家族 A 未全然收官**：`src/std/dom.no` 的 `out = self.pool.collect-tag(...)` 是【鏈式接收者】（rc 為 DotExpression 非 IDENT），需 `resolveReceiverType(self.pool)`＝struct `dom` 欄位 `pool`→型別 `dom-pool` 的**結構體欄位型別解析**，是 noc 尚未具備的獨立機制（登記為後續 A2）。

**門禁**：L0 `ps-test.no` 25/25；`no vet compiler/src` 殘留 19 ERROR 皆平行 session WIP（本輪改動區 0 新增）。

### 5.3.4.ae s8s `[?]T` 回傳參數型別（ArrayType size=nil）（2026-10-27 本輪）

**背景**：§5.3.4.ad 收官後 L2 剩 **10 檔 DIFF**。本輪攻「`[?]T` 參數型別誤拆」家族 C（number/vec）。複測基線：SUT=`/tmp/noc-s67`、DIFF=10。

**缺口形狀**：`(out [?]byte)`（number.no:1250/1273）與 `(out [?]t)`（vec.no:182）。Go 參數型別走 `parseParamTypeAfterName`（type.go:1090-1092）：`]` 前遇 `?` → `paramType="[?]"`，拼元素名後 `buildType("[?]T")` 產 **`ArrayType{Size:nil, Elem:NamedType{T}}`**（dump 為 `ArrayType{size: <nil>}`、僅一個可見子節點）。既非 `SliceType`、亦非 `parseTypeExpression`(type.go:826-833) 那條 `SliceType{NullableType}` 路徑。noc `ps-parse-type` 的 `[` 三臂（`[]T`/`[K]V`/`[N]T`）都以 +1==RBRACKET/INT/IDENT 為特徵，`[?]`（+1==QUESTION）無臂命中 ⇒ 回 0 且不消費 token ⇒ 參數迴圈逐 token 退化：`out` 落成【無型別】Parameter、`byte`/`t` 被當【下一個參數名】。

**修法（`compiler/src/ps.no`，ps-parse-type 新增 `[?]T` 臂，標記 s8s，置於 `[N]T` 臂前）**：`tk==LBRACKET && peek-at(1)==QUESTION && peek-at(2)==RBRACKET && peek-at(3)∈{IDENT,LBRACKET,QUESTION}` → bump `[ ? ]` 三者、`elem=ps-parse-type`（遞迴）、建 `K_ARRAY_TYPE`：slot0='false'（inferred 旗）、**kid0 push 0（Size=nil）**、kid1=elem。⚠️ `type-string`（ast.no:740-744）讀 kid0==0 印 `?`、kid0+1 讀 elem，結構 dump 由 asp spec（鏡像 Go dump.go，已支援 Size==nil）處理，零改 dumper。

**連帶收 GenericParams**：修好 `(out [?]t)` → `NamedType{t}` 後，s63 隱式泛型走訪（ps.no:4426/4594，先 Parameters 後 Results、走訪型別節點單字母 a-z）自回傳參數【取得】`t` ⇒ `[]t.to-arr` 補回 `GenericParams: t`（`[]t` 接收者本不播種，泛型 solely 來自此 `[?]t`）。

**歸因（同一 harness、同一 CTL，A/B `noc-s67`→`noc-s68`）**：**DIFF 10→9 / SAME +1**。轉 SAME：`src/std/number.no`（兩處 `(out [?]byte)` 完整對齊，全檔 SAME）。`src/std/vec.no`：`[?]t`/GenericParams 段已收斂，惟仍殘【無關】的 `j`/`sum` 識別符型別推斷過度/不足（既有、非本家族），故 vec 仍 DIFF。**SAME→DIFF 真回歸 0 檔**（本輪 DIFF 集為上輪真子集，僅移出 number）。

**門禁**：L0 `ps-test.no` **26/26**（新增 `test-param-nullable-size-array`，golden 取於 oracle：`ArrayType{size: <nil>}` + `GenericParams: t`）；`no vet compiler/src` 殘留 19 ERROR（本輪改動區 0 新增）；`no vet src/std` 的 `str.no:1388/1393` index-write ERROR 屬 HEAD 既有（最後由 `67646cbe` str.to-f64 提交觸及、非本 session、src/ 側範圍外），且 `no vet src/std` 跑 Go oracle vet，與 self-host ps.no 改動無涉。

### 5.3.4.af s8t match-test 死碼臂告警計數＋裸可空宣告 Doc 附着（2026-10-27 本輪）

**背景**：§5.3.4.ae（s8s）收官後 L2 剩 **9 檔 DIFF**。本輪攻家族 F。原估 F＝「byte.no＋match-test.no，Program{warnings} 計數＋Doc 附着，小」；複測分岔後**重新定性**：`src/std/byte.no` 的 2 條警告是 W_CHAIN_IF（UTF-8 編碼器 `cp<0x80 -> -> cp<0x800 -> -> …` 鏈式獨立 if），其 316 行結構差是同一鏈式降糖的分歧 ⇒ **歸家族 G（重）**，本輪擱置。真 F 只剩 `tests/match-test.no`（**中等**：須移植 parse 期語義警告）。基線：SUT=`/tmp/noc-s68`、DIFF=9。

**缺口形狀**（`diff <(oracle ast) <(noc ast)` 共 3 行）：
1. 首行 `Program{}` ↔ `Program{warnings: 2}`；
2. 缺 1 行 `Doc: // 直接->是else`（match-test.no:11 行前註解，須附著到 `x: { … }` 首陳述）。

**(A) 警告移植（`warnUnreachableArms`，Go match_warn.go:68-199）**：Go 已把「'->' 臂不可達」診斷從降糖（`buildMatchDesugar`/`skipItBinding`）搬到 parse 期 S4（match_warn.go:12-13 註明全語料僅 match-test.no 19:9／79:9 兩處命中）。noc `ps-parse-match` **早已算出同條件的 `dead` 旗標**（option：`ol==1 && oe==1 && onil==1` ⇒ 1，鏡 Go skipItBinding；enum：`bt==''` 全變體已列 ⇒ 1），僅供跳過 it 綁定。**修法**：於函式頂宣告 `deadTotal = 0`；兩處 `dead = 1` 改為 `{ dead = 1 deadTotal = deadTotal + 1 }`；臂迴圈走完后、組 if/else 鏈前發射 `matchHasRaw == 0 -> p.warn = p.warn + deadTotal`（鏡 Go：任一臂 IsRawCond ⇒ 整段 per-arm 判定 return ⇒ 不告警，故 raw-cond 閘控）。

**(B) Doc 修复（`ps-parse-stmt` QUESTION 裸可空臂，~1479）**：舊行為在解析完 `x ?i64` 的型別後【無條件】`ps-skip-trivia(p)`——destructive 吞掉緊接的換行＋行前註解，令 `; 直接->是else` 無法經區塊迴圈的 `ps-skip-trivia-doc` 成為下一陳述 Doc。**隔離實證**（dp2/dp4/dp5/dp6）：僅裸 `x ?i64`（QUESTION 臂）會丟 Doc，同形帶值 `x ?i64 = v`、型別宣告 `s status` 皆正常。**修法**：刪該 `ps-skip-trivia`，鏡 `ps-parse-typedecl`（6460-6517）含值/裸雙形口徑——直接【非破壞性】`ps-peek(p)` 判 `=`（同列 `name ?T = v` 時 `=` 緊接型別、ps-peek 即見 ASSIGN；裸形見 NEWLINE ⇒ 不進含值支）。

**歸因（同一 harness、同一 CTL，A/B `noc-s68`→`noc-s69`）**：**DIFF 9→8 / SAME +1**。轉 SAME：`tests/match-test.no`（warnings 計數＋Doc 雙缺皆收）。**SAME→DIFF 真回歸 0 檔**（本輪 DIFF 集為上輪真子集，僅移出 match-test）。

**門禁**：L0 `ps-test.no` **29/29**（新增 3 條：`test-match-unreachable-warn-option`／`test-match-unreachable-warn-enum`／`test-bare-nullable-doc-attach`，golden 皆取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（`fl[]`／`out[]` index-write 與 duplicate-fn，皆在本輪改動區【外】、HEAD 既有，0 新增）；`no vet src/std` 的 `str.no` index-write ERROR 屬 src/ 側既有（非本 session、範圍外）。

### 5.3.4.ag s8u map 標註 let 複合值型別 → MapLiteral（2026-10-27 本輪）

**背景**：§5.3.4.af（s8t）收官後 L2 剩 **8 檔 DIFF**。本輪攻 `tests/vec-elem-ownership.no`（8 行差）。基線：SUT=`/tmp/noc-s69`、DIFF=8。

**缺口形狀**：`m [str][]i64 = { }`——宣告型別為 MapType（`[str][]i64`），Go parseLetStatement（stmt.go:1221）對 `Type.(*MapType) && 當前=={` 走 `parseMapLiteral` 產 **MapLiteral**；noc 卻產 **StructLiteral{type: }**。

**根因**：noc `ps-parse-stmt` 的 map 標註臂（s8u 前 1406）以【固定偏移】手解析 key/value：守衛 `+4==IDENT && +5==ASSIGN`——只認【單一 IDENT】值型別。`[]i64`（+4==LBRACKET）不符守衛 ⇒ 本臂跳過 ⇒ 落到通用 catch-all `ps-parse-typedecl`（1624，用 `ps-parse-type` 建型別、`ps-parse-expr` 解 RHS）：型別仍建對（MapType），但 `{ }` 被 `ps-is-struct-lit` 誤判成 StructLiteral。

**修法（`compiler/src/ps.no`）**：
1. map 臂值型別改由 `vty = ps-parse-type(t, p)`【遞迴】解析（鏡 ps-parse-type 的 `[K]V` 臂 6453、Go type.go:835-849，V 可為 SliceType/NullableType/ArrayType/巢狀 MapType）；
2. 守衛由固定偏移改寫成非破壞性前探 `ps-is-map-assign-let(p)`：`[ K ]` 且 K 為內建型別名（同 map 鑑別），再從 +4 掃描型別語法（`[]`/`*T`/`&T`/`[K]V` 前綴群）至 base IDENT，其後必須為 `=` ⇒ 回 1；
3. 型別導向值解析（鏡 Go）：`ps-peek=={` ⇒ `ps-parse-map-lit`；否則 `ps-parse-expr`（`m [K]V = f()`）。

**⚠️ 踩坑（本輪真因、隱蔽）**：`ps-is-map-assign-let` 掃描迴圈最初寫成 `tkj==LBRACKET -> {…}`／`tkj==IDENT -> {…}`／`-> scan = 0`（意圖 catch-all）。但 **ps.no 塊內各 `cond -> stmt` 是互相獨立的 bare-if，裸 `-> X` 只掛在最【末】個 bare-if 作其 else**——於是 `-> scan=0` 對任何非 IDENT token（含 `[`）都觸發，掃描在 j=4 即中止 ⇒ 複合 V 回 0 ⇒ 本臂【根本不進】⇒ 仍由 typedecl 產 StructLiteral（單 IDENT 的 `n [str]i64` 因第一步就遇 IDENT 而倖免，故只 m 壞、n 好）。修：catch-all 改為顯式【已處理 token 補集】`tkj!=EOF && tkj!=MUL && … && tkj!=IDENT -> scan=0`。同坑亦見「裸 `->` 誤當 if/else」——欲得真 if/else 須將兩臂包進 `{ cond -> {…} -> {…} }`（ps-parse-map-lit 用此形）。另：初版泛化【丟了 `=` 判別】，令裸 map 宣告 `m1 [str]i64`（無 `=`）誤入本臂、吞後隨陳述 ⇒ `tests/mem-safety/nested-container-clone.no` 真回歸；`ps-is-map-assign-let` 的 `=` 前探即為補回。

**連帶踩坑（工具鏈）**：`ps-is-map-assign-let` 迴圈旗標命名 `run`——`run` 是 Nolang 保留的異步前綴運算子（run/co/awy），作識別字 ⇒ `expected ')' got INT`。改 `scan`。另 `build -cc zig` 若反覆报同一陈旧行，先排 AST 快取：`NOLANG_AST_CACHE_OFF=1`（本次實為真實語法錯，非快取）。

**歸因（同一 harness、同一 CTL，A/B `noc-s69`→`noc-s70`）**：**DIFF 8→7 / SAME +1**。轉 SAME：`tests/vec-elem-ownership.no`。`nested-container-clone.no` 先回歸後經 `=` 前探修復回 SAME。**最終 SAME→DIFF 真回歸 0 檔**。

**門禁**：L0 `ps-test.no` **30/30**（新增 `test-map-annotated-composite-value-literal`：複合 `[]i64` 空 map ＋ 單 ident 帶 pair map，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（`out[]`@5312-5320／`fl[]`@7799-7807／duplicate-fn@6343，僅因插行而重新編號、與 s8t 同集合，本輪改動區 0 新增）；`no vet src/std` ERROR 屬 src/ 側既有（非本 session、範圍外）。

### 5.3.4.ah s8v let-RHS 型別推斷家族：切片方法參數不傳播＋鏈式欄位回傳型（2026-10-27 本輪）

**背景**：§5.3.4.ag（s8u）收官後 L2 剩 **7 檔 DIFF**。本輪攻「let-RHS 型別推斷」家族兩檔——`src/std/vec.no`（6 行差）與 `src/std/dom.no`（4 行差）。基線：SUT=`/tmp/noc-s70`、DIFF=7。

**形狀一（vec.no）——`[]T`/`[N]T` 方法體內 `x = <識別符>` 的傳播邊界**：
- `[]t.remove = (i i64) …` 內 `j = i`：oracle【無型別】，SUT 卻推 `i64`（過推）；
- `[]t.sum = () (sum t)` 內 `sum = acc`（`acc = 0`）：oracle 推 `i64, inferred`，SUT【無型別】（漏推）。
兩者同為 s66 識別符傳播（stmt.go:1410-1433：`x = y` 僅傳播【有號整數】base，讀 `FuncVarType(curFunc, y)`，且僅當 x 尚無型別）。

**根因（Go 路徑分流）**：Go 有兩條方法定義解析路徑——普通方法 `t.foo`／結構方法走 `parseMethodDefinition`→`parseFunctionBody`（decl.go:1568/1766-1770），會把【參數與回傳值 setVarType 進 FuncVarTypes】；而【切片/陣列接收者】`[]T.m`／`[N]T.m` 走 `parseArrayTypeMethodDefinition`（decl.go:191），它只設 curFuncName＋parseBlockStatement、【完全不登參數/回傳型別】。故 `[]t.remove` 的參數 `i` 不在 FuncVarTypes ⇒ `j = i` 查不到 base ⇒ 不推；結果參數 `sum`（型別 t）亦未預登 ⇒ `sum = acc` 時 existing=="" ⇒ 本地 let `acc`（parseLetStatement 自登 i64）照常傳播。noc 舊碼 `ps-parse-method-func` 對【所有】方法無差別 `ps-set-var-type` 參數/回傳 ⇒ 與此相反（i 被登、sum 被預登成 t）。

**修法（`compiler/src/ps.no` `ps-parse-method-func`）**：以 `selfnt`（接收者型別節點）是否 `K_SLICE_TYPE`/`K_ARRAY_TYPE` 判定 array-method 路徑（鏡 Go 分流）；此路徑下參數/回傳型別改登進【s7v 命名空間 `vt7:`】（`ps-set-var-type-s7v`）而非 `vt:`。妙處正在於既有雙查表語意：`ps-var-type-strict`（推斷閘門，鏡 Go `FuncVarType`）【不讀】`vt7:` ⇒ `j=i` 不推、`sum=acc` 補推；`ps-var-type`（非嚴格，降糖端 `ps-apply-index-out` 讀取）【仍讀】`vt7:` ⇒ `self[cur] = …` 安全索引降糖的參數型別線索不丟。**⚠️ 首版教訓**：最初直接【跳過】params/outs 登記 ⇒ vec.no 內一個 `[]T` 方法的安全索引降糖（需讀參數 `cur` 型別）退化成裸 AssignExpression、真回歸一處；改走 `vt7:` 後兩全。

**形狀二（dom.no）——鏈式接收者 `self.field.method(...)` 回傳型推斷**：`out = .pool.collect-tag(.root, want, star)`，oracle 推 `[]i64`（`dom.pool` 欄位型別 dom-pool ⇒ `dom-pool.collect-tag` 回 `[]i64`），SUT【無型別】。根因：Go `returnTypeFromCallExpr`（type.go:173）先 `resolveReceiverType(dot.Receiver)`（type.go:45-54 的 DotExpression 臂——`self.field` 由 `structFields[當前結構][欄位]` 解析出欄位型別），再以 `欄位型別.方法` 查 `methodSignatures`。noc 的 DotExpression 呼叫臂（s58）只認 `rc` 為【裸 IDENT】變數；`self.pool` 是巢狀 DotExpression ⇒ 不觸發。

**修法（`compiler/src/ps.no`）**：
1. 新增結構欄位登記表 `ps-reg-struct-field`/`ps-struct-field-type`（鍵 `'sf:' sname ' ' fname`，鏡 Go `p.structFields`），`ps-parse-struct-def` 逐欄位登記 `type-string(ty)`；
2. DotExpression 呼叫臂增補【rc==K_DOT_EXPRESSION】子臂：其 receiver 為 `self` IDENT 時，`snm = ps-var-type('self')`（方法接收者型別，ps-parse-method-func 預登）→ `fty = ps-struct-field-type(snm, field)` → 以 `fty + '.' + method` 查 `ps-msig-find`，克隆 s58 的「跳 token 重解析回傳型別＋filter-ok＋宣告閘門＋mark-inferred」流程。純增量（僅在原本推不出型別時才補），節點變數 c 後綴避免串型別。

**歸因（同一 harness、同一 CTL，A/B `noc-s70`→`noc-s71`）**：**DIFF 7→5 / SAME +2**。轉 SAME：`src/std/vec.no`、`src/std/dom.no`。**SAME→DIFF 真回歸 0 檔**（中途 vec.no 索引降糖回歸經 `vt7:` 承接已消）。

**門禁**：L0 `ps-test.no` **32/32**（新增 `test-slice-method-param-no-propagate-local-yes` 釘 j=i 無型別/m=k i64；`test-chained-self-field-method-return-infer` 釘 structFields＋msig 鏈式推 []i64，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（`out[]`／`fl[]`／duplicate-fn 同 s8u 集合，本輪改動區 0 新增）；`no vet src/std` 145 ERROR 屬 src/ 側既有（oracle 執行、非本 session 範圍）。工具鏈：`no build -cc zig`；跑 L0 需 `no run -cc zig`（預設 clang 在 MacOSX27 SDK 連結 libSystem.tbd 報 unknown architecture）。

### 5.3.4.ai s8w 鏈式裸 if 尾隨 else 折疊＋切片方法參數補登記（2026-10-27）

**背景**：§5.3.4.ah（s8v）收官後 L2 剩 **5 檔 DIFF**（byte/bigint/database-sql/net-http2/tests-2）。本輪攻「chained-if 降糖」家族兩檔——`src/std/net/http2.no`（87 行差）與 `src/std/byte.no`（322 行差）。基線：SUT=`/tmp/noc-s71`、DIFF=5。

**形狀一（http2.no／byte.no 結構）——鏈式獨立 if 的尾隨 else 綁定**：`cond1 -> cond2 -> {B} -> {E}`（裸 if 鏈 + 尾 else）。SUT 舊碼把 `{E}` 綁到【最外 cond1】得 `if(c1){if(c2){B}}else{E}`。根因：`ps-mk-standalone-if` 的 else 分支在【鏈吃光之後】才取 `-> {E}`，誤掛最外層。

**⚠️ oracle 並非直覺的 `if(c1){if(c2){B}else{E}}`**：實測 `a -> b -> {B} -> {E}` 的 dump 是——最內層 if `if(b){B}` 充當**條件**、再包一層 `IfExpression{cond=if(b){B}, consequence=nil, alternative={E}}`（`dump.go:486-492`：IfExpression 恆印 cond、then/else【非 nil 才印】⇒ 外層只顯示 cond=內層 if ＋ 一個 else 區塊）。**修法（`ps-wrap-chain`）**：當本層 body 為區塊（`nextTail==0`、鏈到此終止）且其後【同列】有 `->` 時，就地吃掉 `-> {E}`、建 `chained = IfExpr{tail, nextBlk, 0}` 後再以 `wrapper = IfExpr{chained, 0, altW}` 包覆（`esChild = wrapper`）。http2.no 結構差全消。

**形狀二（warnings 計數）——Go wrapStandaloneChainDepth 的 for 迭代語意**：`a->b->c->{B}->{E}` oracle `Program{warnings: 2}`，SUT 0；`a->{B}->b->{D}->c->{F}->{G}`（byte.no UTF-8 編碼分支）亦 2。根因：Go 用 **for 迴圈**在同一 `linkIdx` 折疊多個 `->`（每迭代各報一次 WarnChainedIf），而 noc 用**純遞迴**（每個 `->` 落到 linkIdx+1 的新 frame），尾 else 折疊更曾被【重置 linkIdx=1】⇒ 從不达 2 ⇒ 不告警。**修法（`ps-wrap-chain` 尾折疊支）**：尾 else 是【同 frame 的下一次 for 迭代】⇒ `linkIdx == 2` 時【也要】`p.warn + 1`；其 else-body 遞迴改用 `linkIdx + 1`（鏡 Go `wrapStandaloneChainDepth(nextBody, linkIdx+1)`）。實測 e3(2)、byte-style(2)、純鏈 w3(1)/w4(1)、e2(0)、單 if/else(0) 全部對齊。

**形狀三（byte.no 型別）——切片方法參數標量重指派的補登記**：`[]byte.index-from = (… from i64)` 內 `from = 0`（標量字面量重指派參數）之後 `i = from`，oracle 推 `i64`、SUT【無型別】。根因：Go `parseLetStatement` 尾部 `stmt.go:1599-1606` 把（推斷的）標量型別【補登記】進嚴格 `FuncVarType`——其 existing 查詢讀嚴格表，而切片方法參數經 s8v 只登 `vt7:`（嚴格表恆空）⇒ 【一定補登記】⇒ 其後 `x = param` 識別符傳播查得到。noc 補登記（`ps-parse-let`）的 existing 查詢曾誤用【非嚴格】`ps-var-type`（看到 `vt7:` 的參數型別而誤判「已有型別」⇒ 跳過補登記 ⇒ 參數永不進嚴格表 ⇒ 漏推）。**修法**：existing 查詢改走 `ps-var-type-strict`。连带修正 `pos = i` 的既有型別閘門（已登 i64 的 pos 不再被覆寫推斷節點）。

**⚠️ 提交衛生事故（本輪發現並修正）**：`put-doc-str`（空字串首條註解回 -1、鏡 oracle `commentGroupInline` 無條件印 `//`＋文字＋換行）的【呼叫端】早在 **s8q（a761ab11）** 就寫進 ps.no（`ps-collect-tail` 等 7 處），但【定義】從未提交進 `ast.no`——即 `HEAD` 自 s8q 起【根本無法獨立 build】，歷輪全靠未提交的工作區 `ast.no` 建置。本輪把這份缺失的 `ast.no` 依賴（`put-doc-str` 定義、`comments` 對 -1 的 `//`＋換行渲染、`interfaceMethod` 非 Node 匯出）一併納入 s8w 提交，恢復 `HEAD` 可建置性。L2 全量（652 SAME）證明其輸出與 oracle 一致。

**歸因（同一 harness、同一 CTL，A/B `noc-s71`→`noc-s76`）**：**DIFF 5→3 / SAME +2**。轉 SAME：`src/std/net/http2.no`、`src/std/byte.no`。**SAME→DIFF 真回歸 0 檔**。剩 3 檔：`bigint.no`（src/ 側平行 WIP、本 session 禁改）、`database/sql.no`（多返回值 tuple、已判不可行）、`tests/2.no`（多值指派家族）。

**門禁**：L0 `ps-test.no` **34/34**（新增 `test-chain-bare-if-else-fold-and-warn` 釘 wrapper 結構＋warnings:2；`test-slice-method-param-reassign-propagate` 釘補登記→傳播，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（同 s8v 集合，本輪改動區 0 新增）。工具鏈：`no build -cc zig`／`no run -cc zig`。

### 5.3.4.aj s8x 多重指派逗號後目標專用解析器（2026-10-27）

**背景**：§5.3.4.ai（s8w）收官後 L2 剩 **3 檔 DIFF**。本輪攻 `tests/2.no`（48 行差，多值指派家族）。基線：SUT=`/tmp/noc-s77`。

**形狀——`bit, probs[m] = decode-bit(probs[m])` 末目標吞掉 `= RHS`**：noc 的 multi-assign 循環（`ps-parse-stmt` 的 `ps-is-multi-assign-start` 分支）對【逗號後目標】用通用 `ps-parse-expr-prec(t, p, 0)`。`=` 雖非中綴運算子（中綴迴圈停在 ASSIGN），但 `ps-parse-expr-prec` 尾部鏡像 Go `parseExpression`（expr.go:598-637）：左值為 Dot/Index 時吸收 `= value` 建 AssignExpression ⇒ 整個 `= decode-bit(probs[m])` 被折進目標 2，`ps-expect(ASSIGN)` 落空後越界吃掉下一行的 `decode-bit`（多出一個 `Identifier{decode-bit}` 當 value），其後【函式定義】被撕成 ExpressionStatement 碎片（`(prob i64)`、`i64`、裸 `BlockStatement{}` 等 40+ 行錯位）。實測觸發條件極窄：ident 目標（不吸收）、索引目標居首/居中段（後隨 COMMA 不吸收）皆 SAME；唯【末目標為索引】翻車。

**Go 權威**：逗號後目標走专用 `parseAssignTarget`（stmt.go:834-847）——讀 IDENT、可選【恰好一個】`[` LOWEST-expr `]`（`]` 缺席不報錯也不消費）、`]` 後即止【絕不碰 `=`】；逗號後非 IDENT 直接 `fatalf E_EXPECTED_IDENT`（parseMultiAssignStatement:801 / parseExpressionStatement:1942）。首目標則不同：`stmt.go:379` 分派（IDENT+peek COMMA）用 parseAssignTarget、`parseExpressionStatement:1919` 分派（索引/呼叫開頭）用完整 `firstExpr = parseExpression(LOWEST)`——對本守衛可達的 IDENT／IDENT[..]＋COMMA 兩形，通用解析與专用解析同果，故 noc `maT0` 保留 `ps-parse-expr-prec`（同時覆蓋 `m[i][j], x = f(1)` 嵌套索引首目標——Go firstExpr 支援、parseAssignTarget 反不支援）。

**修法（`ps.no`）**：新增 `ps-parse-assign-target`（鏡 parseAssignTarget：IDENT→Identifier，可選單個 `[` `ps-parse-expr` `]`→IndexExpression），multi-assign 循環逗號後改調之。`tests/2.no` 的 `;` 行前註解转 Doc（`FunctionDefinition{Doc: // bit, tmp = …}`）為既有能力，邊界修正後自然對齊。附带釘住多值 RHS 語意：`y, z[w] = 1, 2` Go 僅取單一 value（`1`），`, 2` 另成 `ExpressionStatement{2}`——noc 原行為即同，無需改動。

**歸因（同一 harness，A/B `noc-s77`→`noc-s78`）**：**DIFF 3→2 / SAME +1**：`tests/2.no` 轉逐字元一致。**SAME→DIFF 真回歸 0 檔**（全量 655 檔 sweep 逐檔比對差集為空）。剩 2 檔：`bigint.no`（src/ 側平行 WIP、本 session 禁改）、`database/sql.no`（多返回值 tuple、已判不可行）。

**門禁**：L0 `ps-test.no` **35/35**（新增 `test-multi-assign-index-target-no-swallow` 釘三形：末索引目標吞號修復、索引目標居首、多值 RHS 尾值另成陳述，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（同 s8w 集合，本輪改動區 0 新增）；`no vet src/std` 148 ERROR 屬 src/ 側平行 WIP（oracle 行為，非本 session 觸及）。工具鏈：`no build -cc zig`／`no run -cc zig`。

### 5.3.4.ak s8y 收官雙檔——裸型名陳述＋運算符重載剥 `?`＋介面繼承清單（2026-10-27 本輪）

**背景**：§5.3.4.aj（s8x）收官後 L2 剩 **2 檔 DIFF**：`src/std/bigint.no`（101 行，曾標「禁改/平行 WIP」）與 `src/std/database/sql.no`（207 行，曾標「不可行」）。本輪復核兩案：bigint.no 的兩族差異全部落在 `parseStatement`/`returnTypeFromCallExpr` 的**穩定** Go 路徑（平行 WIP 未觸及 stmt.go 相關區段，oracle 目標不動）；sql.no 的 207 行差實為**介面繼承清單解析缺口**（非多返回值 tuple）。兩檔均改判**可行**，本輪一併攻克 ⇒ **L2 DIFF 2→0 / 語料 655 檔全數逐位元組一致**。

**規則 A——裸型名陳述（`bigint.no` 家族一）**：後綴 cast `av u64 = (.limbs[i] & MASK) u64` 中，Go `parseExpression` 在 `)` 後即止，尾部 `u64` **獨行成句**；`parseStatement` IDENT 分派（stmt.go:346-356）見 `peekToken ∈ {NEWLINE, SEMICOLON}` 且 `isTypeName(literal)`（type.go:404-413 **閉集 15 名**，不含 any/txt/char/bigint）⇒ `parseLetStatement`：型別 token **回落至名 token 本身**（stmt.go:1153-1155）⇒ `LetStatement{name: u64, type: NamedType(u64 非 inferred), value: nil}`（dump `value: ` 佔位），裸分支 setVarType 後 early return ⇒ **跳過 SetFuncDeclared**。oracle 實測 g-a 釘住：`x = (x & 255) u64` 之 value 為 `GroupedExpression`（cast 未被吸收），前後各出一條 `LetStatement{name: u64, type: u64, value: }`。noc 新增 `ps-is-type-name-literal`（逐字鏡閉集；⚠️ 不複用 `ps-is-builtin-type`——它多含 `'any'`）＋ `ps-parse-stmt` 分派臂（置於 expr-stmt 兜底前）。

**規則 B——運算符重載回傳剥 `?`（`bigint.no` 家族二）**：`c = a._mul(b)` oracle 推 `bigint`，noc 推 `?bigint`。Go 權威兩處：① `returnTypeFromCallExpr` 的 **dot 分支**以 `dot.Property` 為鍵 `stripOperatorOverloadReturn`（type.go:192/199/229；`TrimPrefix "?"`），閉集 `{_add,_sub,_mul,_div,_mod}`（operator_overload.go:40-63，`OperatorOverloadBaseName` 取末 `.` 段）；**裸名 Identifier 分支不剥**（type.go:151-170）。② 結果參數登記（decl.go:1766-1790）：定義名末段為重載名時 `setVarType(resName, TrimPrefix ts)`。此規則同時解釋級聯差 `g = x.copy()` no-type：`x = x._mod(y)` 舊推 `?bigint` 污染 varType ⇒ 接收者鍵 `?bigint.copy` miss；剥 `?` 後級聯自解。noc 新增 `ps-is-op-overload-name`＋`ps-unwrap-op-overload-type`（節點級 NullableType 剥一层），落點：呼叫點推論**三條 dot 查找臂**（recv-var `mkey`／模組限定 `mq`／鏈式 `mkeyc`，`ps-parse-type` 重跑後、`ts` 導出前剥）＋**兩處結果登記循環**（`ps-parse-func-def` 以 `name`、`ps-parse-method-func` 以 `fullName`，含 regp 雙臂）。

**規則 C——介面繼承清單（`database/sql.no` 全檔）**：`db enter, leave { fs.close() (ok bool) … }`。Go 分派 stmt.go:65-113：`peek==IDENT` ⇒ 以 `look(k)`（= current 後第 **k+2** 個**非註解** token，⚠️ 掃描起點是清單**第二**元素、首元素由 `peek==IDENT` 守衛本身覆蓋）掃 DOT/COMMA 交錯介面名列表；終點恰 `LBRACE` ⇒ 內容首 token `IDENT+( /.)` 回 `parseInterfaceDefinition`（欄位形回 struct-with-implements——**全語料無此形**，s79 掃圖僅 sql.no，故守衛不放行、零回退）。解析端 decl.go:874-885：while current IDENT 收集實作名、跳逗號，再無條件跳 `LBRACE`。dump：`InterfaceDefinition{name, methods}` 後印 `Implements: [enter leave]`（dump.go:396-397，`%v` 空格分隔）。noc 側 tape kind40 **已含 OP-IMPLEMENTS**（規格表機械生成、dumper 已接線），僅缺解析：新增 `ps-is-iface-inherits` 守衛＋分派臂（置於 typedecl 前——Go 中 implements 掃描先於 `parseLetStatement`，否則 `db enter` 誤解為 `name TYPE` 宣告）＋`ps-parse-iface-def` 繼承清單循環；aux 規約［trail→clos→impl］⇒ `aux-begin`（置 imps 字串）＋`set-aux-counts(-1, -1, nimp)` **必須先於** `helper-begin`（hbase 取當時 aux 尾，順序顛倒則 impl 段與 helper 段錯位）。generic-receiver `fs.close()` 為既有能力。無繼承清單時 `nimp` 預設 0 ⇒ 零外溢。

**歸因（同一 harness，A/B）**：`noc-s78`→`noc-s79`（規則 A+B）：`bigint.no` 101→**0**、`tests/2.no`/其餘不动 ⇒ DIFF 2→1；`noc-s79`→`noc-s80`（規則 C）：`sql.no` 207→**0** ⇒ **DIFF 1→0 / SAME 655/655**，全量 655 檔逐檔差集為空、**真回歸 0 檔**。重現檔：`tmp/s8y/t2/t3/t8.no`（規則 A/B 最小重現）、`tmp/s8y/g-a/g-b/g-c.no`（三規則 golden 釘檔）。

**門禁**：L0 `ps-test.no` **38/38**（新增 `test-lone-type-name-statement`、`test-op-overload-return-strip`、`test-iface-inherits-list`，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（同 s8x 集合，本輪改動區 0 新增）；`no vet src/std` 145 ERROR 屬 src/ 側平行 WIP（oracle 行為，較 s8x 記帳 148 減 3，非本 session 觸及）。工具鏈：`no build -cc zig`／`no run -cc zig`。

**🏁 L2 門禁現狀**：語料 `tests src/std`（655 檔）`no ast` 逐位元組一致，**DIFF=0**。§5.3.4.ac 所列「剩餘家族」（InterfaceDefinition 繼承清單、decode-bit 巢狀、`[?]byte` 參數型別、warnings 計數）已於本輪全部結清。

### 5.3.4.al s8z——M2 驗收收官＋ast-test 缺線哨兵遷移（2026-10-27 本輪）

**背景**：s8y 後跑 M2 正式驗收，發現 L0 `ast-test.no` 47/48——`test-unsupported-is-live` 紅。根因：該測試以「dump `InterfaceDefinition` 必觸發 `interfaceMethod` helper 缺線計數（unsup==1）」作為「計數器非假斷言」的哨兵，而 `interfaceMethod` 在 ast.no helper 分派中**已接線**（implements 迴圈於 default 臂之前；s8y 後語料可逐位元組复現 `database/sql.no` 亦側證）⇒ 哨兵失效，非回歸、乃測試期望過期（本次才首跑 ast-test 該檔，故延至今日才暴露）。

**處置**：哨兵遷移至**仍未接線**的 `annotationStructField`——`AnnotationStructValue`（kind 9）tape（instrs 21..24：HDR/SCALAR×2/CHILD(HELPER a=21'Fields' c=22'annotationStructField')）帶該 OP-HELPER，落 helper 分派 default 臂 ⇒ `ast.dump(add(t, K_ANNOTATION_STRUCT_VALUE))` 必計 unsup==1，感測器等價且更省（無需設槽）。同步修 ast.no 兩處過期覆蓋面註釋（檔頭「未接線清單」與 helper 段：interfaceMethod 移入已接線列）。

**M2 驗收（本輪）**：全量 L2 複測（`/tmp/noc-s81`＝s8y+本輪註釋修訂現建，串行掃描 `tests src/std` 655 檔）⇒ **DIFF 清單為空**；抽驗 `bigint.no`(175 KB)/`sql.no`/`tests/2.no` 兩側 sha256 一致且非空。L0 五檔全綠 **27/62/45/48/38=220 斷言**；`no vet compiler/src` 19 ERROR（基線）。§6 里程碑表 M2 轉 ✅，並新增 §6.4「M3 啟動盤點」。**M2 里程碑自此驗收收官。**

### 5.3.4.am s9a——M3-a：`drv.cmd-vet` 骨架＋`diag.render` 行形補全（2026-10-27 本輪）

**交付**：① `compiler/src/drv.no` 新增 `drv.cmd-vet`（dispatch `vet`、usage 行），輸出契約逐條錨定 oracle authority `printLintResults`（`src/cmd/no/main.go`）：無診斷 ⇒ stdout `No issues found.` rc=0；空檔（read-bytes 回 nil）是合法源碼同樣走此分支；路徑不存在 ⇒ stderr `Error: stat <path>: no such file or directory` rc=1（探針實測流別與文案）；缺檔案參數 ⇒ rc=2（⚠️ 刻意偏離：oracle 無參數 vet 會掃整個 workspace，不在 L3 契約內）。cmd-vet 跑完整 parse 路徑（與 cmd-ast 同源），為 M3-b/M3-c 的診斷收集器預留掛點。② `compiler/src/diag.no` `render` 補 **line==0 檔案級行形**（authority 的 else 分支：`<file>: [LEVEL] <src>: <msg> [<code>]`，不得出現 `:0:0:`）。③ 新增 L0 `compiler/tests/diag-test.no`（7 斷言：三個級別名、有位置/檔案級/warning 三種 render 行形、render-mid 中間串形；期望字串全部由 oracle 實測釘死）。

**新 quirk（實測釘死，Go 側既有行為，本輪不修）**：struct 字面量**字段值**寫模組限定常量（`level: diag.LEVEL-ERROR`）被**靜默置 0**；整數字面量（`level: 2`）與構造後賦值（`d.level = diag.LEVEL-ERROR`）均正常（`/tmp/diag-probe2.no` 三例 0/2/2）。diag-test 因此一律構造後賦值；自舉 parser 的對應規則（`#`/字段值位置的限定名解析）將來必須復現——記此為 M2 遺產的新待復現命題。

**L3 誠實基線（骨架階段）**：`scripts/selfhost_diff.sh --sut /tmp/noc-s82 --levels L3 --limit 40` ⇒ **G8 MASS-NOOP**（SUT 對 40 檔吐同一 stdout）——骨架尚無診斷來源，本層整體退化被防假通過護欄如實標出，**這不是 PASS**。oracle 側盤點（同 40 檔）：僅 **2 檔 clean、38 檔有診斷**；severity 分布 **ERROR 594 / HINT 10001 / WARNING 296**；code 族 top：`ovf-int-default` 4107、`ovfhndld` 3034、`rngidx` 1106、`rng-ellipsis` 749、`tcpoxtfd` 629、`idxwrite` 592、`idxhndld` 359、`arm-should` 296、`optunuse` 2 ⇒ M3-c 的復刻面比 §5.4「37 個 Validate*」的計數更寬（HINT 族佔九成），`[ERROR]` 硬判據全語料外推萬行級。

**基線漂移記錄**：`no vet src/std` 現報 **145 ERROR**（其中 67 條 `nolang-index-write`）——該 lint 已在 HEAD 的 `src/checker/lint.go`（Go 側 parser-lowering-invariants 軌引入、對 std 存量代碼「長了牙」），**非本增量引入**（本輪只動 compiler/src 兩個 .no，且 `no vet compiler/src` 仍 19 ERROR、diag.no/drv.no 零 ERROR）。nolang-vet skill「src/std 必須 0 ERROR」的門禁對 HEAD 已失效，屬 Go 側 session 的處置範圍（遷移 std 呼叫點或規則降級），自舉側 M3-c 目標是**復現**這 145 條而非令其消失。

**門禁**：L0 六檔全綠 **27/62/45/48/38/7=227 斷言**；L2 回歸 `/tmp/noc-s82`（含本輪 drv/diag 改動）全量 655 檔 **DIFF=0**，抽驗 `tests/2.no`/`bigint.no`/`set.no` 兩側 sha256 一致；`no vet compiler/src` 19 ERROR（=基線，零新增）。


### 5.4 語義檢查（M3）

**目標**：復刻 37 個 `Validate*` 與型別推斷，使 `no vet` 的診斷集合一致。

**關鍵復刻點**：

1. **診斷的三條通路**（一條規則要接三個地方，漏一個就會「編輯器看得到、`no build` 看不到」或反之）：
   | 通路                                     | 入口                                                                                       |
   | -------------------------------------- | ---------------------------------------------------------------------------------------- |
   | `no build` / `no run` / `no test`（硬錯誤） | `checker.ValidateTypes` **加上** `src/build/transpiler.go` 的 `allValidateErrs` 明確列表        |
   | `no vet`（CLI）                          | `Transpiler.Compile` 先跑硬錯誤列表，再對**合併後**的程式跑 `checker.RunAllLints`（`SkipTypeChecks: true`） |
   | 編輯器                                    | `lsp/server.go` → `checker.RunAllLints` 對**原始未合併**的程式                                    |
2. **std 簽名表**：`src/checker/stdsig_gen.go`（7,179 行，`embeddedStdSigVersion = 2`，859 funcSigs / 1,983 methodSigs）**必須**搬進 `compiler/src/ck-sig.no`。做法：寫一個一次性的 `compiler/tools/gen_stdsig/`（Go 或 nolang）讀 `src/std` 生成 `.no` 源碼。**不要**在 `noc` 啟動時重新收集（那會讓每次啟動都變慢，且與 `no` 的時序不同）。
3. **`#{buildin}` 樁的三條載入路徑**（**158 處**）：一個空的 `#{buildin}` 函式體是「由 Go 內建表實作」的**承諾**。若任一路徑把它當真實函式生成，呼叫點就會綁到空符號 ⇒ **靜默錯值**（`f64.sqrt` 回堆疊垃圾、`math.clamp` 回 0、`os.get-env` 回 `''`）。三條路徑與守衛：
   | 路徑                                                                           | 位置                                         | 守衛                                                                                       |
   | ---------------------------------------------------------------------------- | ------------------------------------------ | ---------------------------------------------------------------------------------------- |
   | std 自動載入                                                                     | `checker.loadStdModuleBodyForce`           | 跳過樁                                                                                      |
   | 剝除                                                                           | `stripBuiltinStubs`（`build/transpiler.go`） | 剝除樁                                                                                      |
   | **顯式 `# std/x` 導入**                                                          | `processUseAndMerge`                       | ⚠️ **曾漏掉**；修法是 `if fd.BuiltinStub { merged.BuiltinFuncNames[fd.Name] = true; continue }` |
   | **驗收**：`rc` **不能**偵測這個 bug（Nolang 測試 runner 斷言失敗仍 rc=0）⇒ **必須 hash stdout**。 |                                            |                                                                                          |
4. **診斷輸出的兩種形狀**（不要發明第三種）：
   - 面向使用者：`<file>:<line>:<col>: [ERROR|WARNING|HINT] <source>: <message> [<code>]`
   - 中間編譯錯誤串：`<channel>: [<file>: ]line L, column C: <message> [<code>]`，多條以 `; ` 連接；`<channel>` ∈ `validation errors: ` / `parser errors: ` / `check error: `  
     ⚠️ **切分只能切「後面跟著位置前綴的 `; `」**——訊息文字本身可能含分號。

**Oracle**：`no vet <file>`。驗收 = 全語料**歸一化後**逐行一致（§7.3）。

### 5.5 中端：HIR / MIR（M4）

**目標**：HIR 樹 + 67 個 MIR op + 降級規則。

**M4 的範圍裁剪（關鍵決策）**：

| 子系統                                     | Go 側規模 | M4 是否必需          | 說明                                |
| --------------------------------------- | ------ | ---------------- | --------------------------------- |
| `hir2mir.go` 降級主體                       | 大      | ✅                | 語句/表達式 → MIR                      |
| `codegen.go` MIR→IR                     | 最大     | ✅                | 逐 op 發射 IR 文本                     |
| `builtin_call.go` / `builtins.go`       | 中      | ✅                | 內建函式降級                            |
| **`analysis.go` 所有權分析 + `insertDrops`** | 大      | ❌ **M4 用 RC 取代** | 見 §5.6                            |
| `alias_forward.go` 別名轉發優化               | 中      | ❌                | 純優化                               |
| `spawn_graph.go` / async                | 中      | ⚠️ M6 之後         | `noc` 自身不需要 async（除驅動層並行調用 clang） |
| `tier.go` 所有權層級推斷                       | 中      | ❌                | 報告用                               |
| `opt.go` MIR 優化                         | 中      | ❌                | 交給 LLVM `-O2`                     |

> **這一張表是本方案最大的槓桿**：它把 `src/mir` 的 43,177 LOC 中**約 40%** 從自舉必需降級為可選。

### 5.6 代碼生成（M4）：RC 優先的堆策略

#### 5.6.1 為什麼用 RC 而不是精確所有權

Nolang 今日的堆管理是**編譯期精確所有權分析**：`insertDrops` 決定每個值的 drop 位置，`moveSrc`/`moveDst` 決定 move 的所有權轉移，四個述詞（`moveStructSharesHeap` / `moveStrSharesHeap` / `moveSliceSharesHeap` / `moveTransfersOwnership`）的**併集**決定來源是否仍擁有堆。

這套機制的正確性極難達到，repo 的歷史就是一部「又一次 double free」的編年史（`txt` 佈局的 13 個站點、`%vec` 元素寬度、option 的 box、投影與克隆、別名轉發的順序……）。**在自舉階段重寫它是最高風險的選擇。**

**替代方案：引用計數（RC）**。`no` 已經有完整的 RC 基礎設施（P4/P5 of the ownership model）：

```
%nolang_hdr = type { i64, i64 }   ; rc, flags;  data = base + 16
```

- `@nolang_rc_alloc(size)` — 分配 `size + 16`，寫頭，`rc = 1`
- `@nolang_rc_retain(p)` — `rc++`
- `@nolang_rc_release(p)` — `rc--`，到 0 時 `free(base)`
- `rc == 0` 是**借用哨兵**（FFI 指針、字串常量、`cap == 0` 的 `str→[]byte` 視圖），兩個 helper 對它**立即返回**

**RC 的發射規則（比所有權分析簡單得多）**：

| 事件                                                                            | 發射                                  |
| ----------------------------------------------------------------------------- | ----------------------------------- |
| 建立一個擁有堆的值（`str_concat` / `vec_new` / `map_put` / struct literal 含 owned leaf） | `rc_alloc`（rc=1）                    |
| `b = a`（值拷貝，兩邊都活）                                                             | `retain(a)`                         |
| 離開作用域（每個 owned 局部）                                                            | `release(v)`                        |
| 傳入函式參數                                                                        | 因為**參數是引用語義**，呼叫方保留所有權，**不 retain** |
| 存入容器 / 結構體欄位                                                                  | `retain`（容器成為第二個擁有者）                |
| 從容器/欄位取出                                                                      | `retain`                            |

**RC 的代價與接受理由**：

- ❌ **循環洩漏**：環永遠不會歸零。→ **接受**：編譯器是短命進程，記憶體上界 = O(輸入大小 × 常數)。
- ❌ **原子操作開銷**：`noc` 是單執行緒（除驅動層的 `co`），**用非原子計數**。
- ❌ **retain/release 指令開銷**：比精確 drop 多，但 LLVM `-O2` 能消掉相當一部分。
- ✅ **正確性論證是局部的**：每一處 retain/release 的正確性只依賴它自己的上下文，不需要全程序定點分析。
- ✅ **不動 `noc` 自身的語義**：`noc` 源碼由 S0 編譯時，用的是 S0 的精確所有權；`noc` 產出的程式才用 RC。**兩者互不干擾**。

> ⚠️ **RC 的實作有一條必須遵守的鐵律**：**「每個別名都必須攜帶自己的計數」**。漏一個 retain 是 **UAF**（不是洩漏）。因此 M4 必須有 `check-rc-balance` 驗證器（沿 `handleAliasSet` 的同一套別名判定），並且**只檢查「欠計數」方向**——「過計數」是 §1.3.1 明確接受的洩漏，對它報錯會產生 false positive ⇒ 硬編譯失敗。

#### 5.6.2 為什麼發射 LLVM IR 文本而不是 C / 機器碼

| 選項                     | 前端工作量                  | 需要新寫的 runtime                              | 跨平台     | 編譯速度 | 選擇           |
| ---------------------- | ---------------------- | ------------------------------------------ | ------- | ---- | ------------ |
| **LLVM IR 文本 → clang** | 中（照抄 `codegen.go` 的結構） | **0**（照抄既有 prelude）                        | ✅ clang | 中    | ✅ **DR-2**   |
| C 源碼 → cc              | 低（無 SSA/phi）           | **需要一整套 C runtime**（str/vec/map/option/RC） | ✅       | 慢    | ❌            |
| 自研機器碼 + 鏈接器（nocg/nold） | **極高**                 | 0                                          | ❌ 需三端口  | 快    | ❌ 另一份設計文檔的範圍 |

**LLVM IR 文本的關鍵優勢**：`src/mir/codegen.go` 裡的 runtime prelude 是**字串常量**（`windowsShimsConst`、`%task` prelude、RC helper），可以**逐字節照抄**到 `compiler/src/cg-runtime.no`，不需要重新設計 ABI。

⚠️ **照抄 IR 文本時的一條硬規則**：**Go raw string（反引號）內部不得出現反引號**。若在 IR 註釋裡用反引號強調識別符號，會**終止 Go 字串字面量**，報錯是 Go 語法級的（`expected ';', found _truncate`），不是 IR 錯誤。在 nolang 裡對應的規則同樣成立（`'...'` 是 str，\`\`\` 是原始字串）——**IR 註釋裡一律用普通雙引號引用識別符號**。

#### 5.6.3 IR 生成的驗收

M4 的驗收**不能只靠「程式跑得對」**，因為：

- LLVM 的 UB 在 `-O0` 正確、`-O2` 錯誤（佈局變化會把潛在的越界讀變成 UB）；
- 一個「沉默的正確」可能只是棧佈局的巧合。

**三層驗收**：

1. **行為**：全語料 `(rc, sha256(stdout))` 與 golden 一致（L4）。
2. **IR 形狀**：對**排序後**的 `.ll` 做 diff（宣告順序非確定，直接 diff 必有噪聲）。要求關鍵函式（`@str_concat`、`@nolang_rc_*`、`@nolang_free`、每個 `@__nolang_drop_*`）的**存在性**一致，而不是逐位元組一致。
3. **`opt` 驗證**：對每個產出的 `.ll` 跑 `opt -passes=verify`，**0 失敗**。⚠️ 驗證失敗時 `.ll` **不會被持久化**——必須用 `NOLANG_MIR_DUMP_LL=1`（該開關在驗證**之前**寫檔）才能看到出錯的 IR。

### 5.7 驅動層（M5）

**目標**：`noc build/run/ast/vet/tokens`，含 workspace/package 解析、`#` 導入、模組合併、調用 clang。

| 子項               | 復刻點                                                                                                                                                          |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **workspace 查找** | 向上查找 `workspace.jsonc`（`pkg.FindWorkspaceRoot`）；找不到 ⇒ 相對路徑解析會錯成 `<root>/<絕對路徑>`（§2.1 實測）                                                                     |
| **`#` 導入**       | `# /path/module.symbol [alias]`，路徑**相對於 workspace 根**；`std/*` **自動載入**無需顯式導入；跨模組調用**必須** `ShortName.` 前綴                                                     |
| **模組合併**         | 同名常量**按名字去重、取第一個**（`mergedGlobalConsts`）；這條規則曾導致「第二個模組的函式被鎖在自己的常量之外」                                                                                         |
| **target 解析**    | `parseTargetPlatform()`（`build/builder.go`）是唯一既有實作；**禁止** `runtime.GOOS`；**禁止**把平台常量寫在 package 級 `var`（會在驅動解析 `-target` 之前凍結成 HOST 的值）                       |
| **外部工具**         | `process.start-with-opts("clang", ["-x","ir","-O2","-o",out,ll], "", "", true)`；⚠️ Windows 上的 libc 差異（無 `realpath`/`utimensat`/`sync`/`lstat`，**無裸 `stat`**） |
| **快取**           | `src/cache`（106 LOC）：內容雜湊 + `stdsig` key。M5 可以先不做快取（每次都全量編譯），但**必須**保留 `stdsig` key 的語義，否則 §5.4 的簽名表會與 `no` 不同步                                              |

### 5.8 診斷模型（`diag.no`）

```no
diag {
    file str
    line i64
    col  i64
    level i64     ; 0=HINT 1=WARNING 2=ERROR
    src  str      ; "nolang-parser" / "nolang-lint" / ...
    msg  str
    code str      ; 8 字元 base36 TraceID
}
```

⚠️ **TraceID 的來源**：`no` 的 TraceID 由 `scripts/stamp_traceid.sh` 在 `make` 時用 perl 把 `TraceID: "PLACEHOLDER"` 替換成隨機 8 字元 base36。**`noc` 必須使用與 `no` 完全相同的 TraceID 表**（否則 L3 診斷差分全線不同）。做法：把 `no` 的 TraceID 表**導出為一個 `.no` 常量表**（`compiler/src/diag-id.no`，由 `tools/gen_stdsig/` 一併生成）。


## 6. 里程碑與進度

> 本節是**唯一**的「實際進度」來源：§0 是決策、§5 是架構、§7 是門禁。**每完成一個里程碑就更新這裡的狀態與證據**（證據必須是可重跑命令 + 觀測值）。

| 里程碑    | 範圍                                                                              | 狀態      | 證據（可重跑）                                                                                |
| ------ | ------------------------------------------------------------------------------- | ------- | -------------------------------------------------------------------------------------- |
| **M0** | 子項目骨架：`compiler/` 佈局、`package.jsonc`、`main.no` 分派、`util/sb.no`、`tools/shs-check.no`、`scripts/selfhost_diff.sh` | ✅ **完成** | `no run compiler/tools/shs-check.no compiler/` ⇒ **10 檔 / 0 違規 / OK**；`selfhost_diff.sh --self-test` ⇒ **無 FAIL** |
| **M1** | 詞法：`src/tok.no`（92 個 `TokenType`）+ `src/lx.no`（`scanToken()` 的忠實移植）              | ✅ **完成** | `no run compiler/tests/tok-test.no` ⇒ **62/62**；`lx-test.no` ⇒ **45/45**；L1 全語料 **675/675 SAME, 0 DIFF**（§7.1.1） |
| **M2** | 語法：71 個 AST 節點 + 復刻 `parser/dump.go` 的輸出格式                                     | ✅ **完成（2026-10-27，s8y `261ab91c`）** | **L2 已可執行**：`noc` 已實作 `ast` 子命令（`drv.cmd-ast`），`selfhost_diff.sh --self-test --levels L0,L2 --sut /tmp/noc-s0`（2026-10-11）⇒ **L0 4/4 SAME、L2 646/646 SAME、0 DIFF**、語料 `<unknown>` 0 行（71/71 節點全覆蓋）。真實 noc↔oracle 逐位元組相同：**+1（独立 `AnnotationStatement{entries: N}` 節點增量，解锁 collection/*）、0 回歸**——絕對數方法論敏感（parallelism × 逾時上限）：以 uniform 120s-alarm `-P 3` 同法量測 s31=137（AssignExpression）→s32=145（ReturnStatement）→s33=158（Annotation）→s34=159（UseStatement）→s35=202（ArrayLiteral 推論元素型別，**+43**，為 aes/zlib 等 `[N] = [int-literals]` 表檔解锁）→s36=203（self-dot `.field` 前綴 receiver，新匹配 `src/std/io.no`）→s37=204（独立 `AnnotationStatement{entries: N}` 節點——不可附著之語句級 `#{...}`，新匹配 `src/std/collection/stack.no`）→s38=213（**ForStatement bang/cond-loop 簇**——陳述位 `!! { }`（恆真無限）/`! { }`（恆假）/`(cond) { }`（前置條件），**+9**，新匹配 `src/std/bufio.no`、`collection/hash-set.no`、`collection/str-set.no`、`env.no`、`tests/dowhile.no`、`loop-test.no`、`split-loop.no`、`str-loop-assign.no`、`mem-safety/bug15-read-dowhile-copyfile.no`）→s39=222（**字面量型別推斷補齊**——`CharLiteral`→`char`、十六進位 `0x/0X` `IntegerLiteral`→`byte`（十進位仍 i64），同時補 `ps-infer-type`（裸 RHS）與 `ps-elem-type-of`（陣列/切片首元素），**+9**，新匹配 `tests/char-*`（literal/new-method/new-syntax/old-syntax2/simple/step4/vet））→s40=253（**無值明示型別裸宣告 `name TYPE`**——陳述位 `result []byte`/`z [16]byte`/`x person`/`i8 i8` 此前被誤解析為 IndexExpression，**+31**，覆盖 crypto/archive 大量 `[]byte`/`[32]byte` 暫存宣告）→s41=262（**複合型別名**——`ps-parse-type` 補限定名 `a.b`（NamedType 續吃 `.IDENT`，對齊 Go stmt.go:1034）與巢狀元素 `[][]T`/`[N][]T`（SliceType/ArrayType 臂改為【遞迴】 ps-parse-type，元素 ∈ {IDENT,[,?}），同把 `ps-parse-typedecl-noval` 升級為通用 `ps-parse-typedecl`（新增 `= value` 分支），**+9**，新匹配 tls/tls-part3/http2/hpack/fs-error-{final,info}/fs-open-err/nested-vec-test/push-narrow）→s42=273（**無色 async 前綴運算子 `run`/`co`/`awy`**——`ps-parse-expr-prec` 缺此三前綴臂⇒`h = run f()` 的值被解析為空、`f()` 漏成獨立 ExpressionStatement；補上後 `run e`→RunExpression{Call:e}、`co e`→CoExpression{Call:e}、`awy e`→AwaitExpression{Right:e}（三節點 dumper 早已支援、纯 parser 缺口），**+11**，新匹配 async-{cancel,coop,rc,yield,handle-alias}/module-async/slot-rebind-unsafe/mem-safety/async-*）→s43=284（**區塊/程式尾註解 TrailingComments**——`ast.no` dumper 早已實作 OP-TRAILING（讀 ntrail/ct0/aux 印 `TrailingComments`＋每行 `//<text>`），唯 `ps.no` 的 `parse`（Program）與 `ps-parse-block`（BlockStatement）把 `}`/EOF 前的獨立尾註解群餵進 `ps-skip-trivia-doc` 後**丟棄**⇒語料中「最後一條陳述之後、`}`/EOF 之前」的 standalone 註解從未進入 TrailingComments；新增三 helper（`ps-trail-region` 前瞻（不消費 token）判斷下一個非 trivia 是否 ∈ {RBRACE,EOF}、`ps-collect-tail` 消費 NEWLINE/COMMENT 收集群——同條陳述行尾的首個行內註解仍走 set-cmt、其餘全部入 trail、`ps-attach-trailing` 走 aux-begin/aux-add/set-aux-counts(ntrail=len)），並接進 Program 與 BlockStatement 兩處。**零回歸結構保證**：ntrail 預設 -1⇒renderer 跳過、trail 空則不呼叫 attach⇒原匹配檔輸出位元組不變。**+11**，新匹配 src/std/types.no、tests/{arr,duplicate,for2,i,str,range-test}.no、mem-safety/{deep-free-nested-vec,deep-free-str,deep-free-struct-vec,minimal-str-map2}.no）。→s44=283（**結構體欄位型別拓展 + 欄位尾註解跳過（ps-is-struct-def）**——裸結構定義 `name { field TYPE }` 之前仅在栏位 TYPE ∈ {IDENT, []IDENT, [INT]IDENT} 且无尾注时被识；限定名栏位 `conns [32]net.conn`（`]` 后 `net` 再 `.conn`）、嵌套切片 `rows [][]str`、栏位尾 `#{inline=…}` 注均使旧手筛扫描器回 0⇒整个结构退回 ExpressionStatement（而 ps-parse-struct-def 主体早已走 ps-parse-type（s41 已支援限定/嵌套）⇒**仅 gate 限**）。修复：新增 ps-skip-type-idx（索引前视、不消费 token）完整镜像 ps-parse-type 四形（`a.b`/`?T`/`[]T`/`[N]T`、元素递归∈{IDENT,[,?}）取代 gate 内的手写栏位型扫描；另新增 ps-skip-annot-idx（gate）/ps-skip-annot（body）跳过栏位尾 `#{...}`——`no ast`（无 -annotations）完全不渲染栏位注⇒跳过即可。**零回归结构保证**：含限定/嵌套/注栏位的结构此前必已 NO（gate 拒→ExpressionStatement≠oracle StructDefinition），新能力只改善。**+3**，新匹配 src/std/net/client.no、tests/field-{inline-annotation,tag}.no）。→s45=284（**枚举/标签枚举定义 `name { red, green }` / `name { ok(v T), fail }`**——陈述位 IDENT+LBRACE 此前仅认 struct 形；Go parser.go:273 classifyBlock 把 blockEnum/blockTaggedEnum 与 blockStruct/blockInterface 分流（decl.go:749 parseEnumDefinition、decl.go:1077 parseTaggedEnumDefinition），而 asp.no dumper（EnumDefinition=24/EnumValue=25/TaggedEnumDefinition=65/TaggedEnumVariant=66 + taggedEnumField HELPER 对）早已支援⇒纯 parser 缺口。新增两函数：`ps-is-enum-def`（索引前视 gate，不消费 token）——成员 ∈ {IDENT/NIL 裸变体、`名(字段…)` 括号载荷、`名 = INT` 显式值、`#{…}` 注、NEWLINE/COMMA 分隔}，**保守分流**：括号+裸并存→2（标签）、仅裸/显式→1（枚举）、全括号无裸→0（与接口歧义）、`) (` 返回组或 `t.` 限定→0（接口形）、任何非预期 token→0——gate 拒者原本必走 ExpressionStatement≠oracle⇒结构上零回归；`ps-parse-enum-def`（body）——计数语义对齐 Go：显式 `V = INT`【不】推进 nextVal（decl.go:807–821）、标签变体 index 逐变体+1；变体字段以 helper-begin/aux-add（name|type 成对）挂 taggedEnumField；Type kid 以首字段型别占位（Go 同口径 Fields[0].Type）。⚠️ `#{…}` 变体注不渲染（默认无 -annotations）⇒跳过即可。**+1**，新匹配 tests/chain-copy.no；t49-enum-defs fixture byte-identical。⚠️ 语料其余 9 档首分歧即枚举（TaggedEnum 6+Enum 4+err.no 等）现结构已对，但随即在**下游 armBinding 裸 match desugar 主簇**再分歧（LetStatement 桶 107 档中 44 档为该簇）——该簇已确认为下一个最高价值增量。⚠️ **方法学事故记录**：本增量中 t31-standalone-if 一度显示回归——隔离实验证明源于 `~/.cache/nolang/ast` 被并行会话（Go 侧 concurrent edits + bin/no 重建）污染的**非确定性构建**（同一 HEAD 源码两建不同行为），清缓存重建后 s44/s45f 行为一致、arm 零回归；差分对照必须用**同一工具链代次**重建的新旧 noc。⚠️ 其余 4 fixture（t37/t4/t7/t8）与 live oracle 的差异为 **noc-s0 时代既有**（fixture 门历史基线并非 live-oracle 对照），非本增量引入。→s46=302（**明示 match 的 armBinding desugar 补齐**——三缺口：(a) err/nil 关键字臂（型样为裸关键字且紧接 `->`）→ 新 `ps-mk-it-let-arm` 建 `LetStatement{name:it, type:<变体>, value:subj, synthetic, armBinding:true}`（Go lowering.go buildItBindingForArm：被匹配型别未知时【仅 err/nil】臂带型别，ok/else 回落裸 synthetic——同文件 `ps-mk-it-let`；dumper 早已支持 armBinding 槽，纯 parser 缺口）；(b) 裸尾臂 `-> expr`＝else：peek 见 RARROW 直接视同 wildcard（不消费 token），其区块作前一 IfExpression 的 alternative，不再伪造 `subj == <nil>` 条件臂；(c) kvar 检测用 `ps-peek-at(p,1)==RARROW` 区分关键字臂与同名普通变量。fixture `t50-match-arm-binding.no` 于晋级后 noc-s0 与 oracle 逐位元组相同。**同代 A/B**（同一好代次工具链分别编译 HEAD ps.no＝ctl 与 s46 ps.no）：corpdiff(ctl→s46)＝ +20 档、回归 0；**最终语料门禁** corpdiff(noc-s0[s45f]→noc-s46f)（live oracle 02:18 代）＝ **+19 档（match-*/option-*/vec-*/open-*/arr-* 等 armBinding 主簇）、回归 0**、283→302（旧基线 284→283 系 02:18 oracle 代对个别档预期漂移所致、与本增量无关）；fixture 门 47/51（仅余既有 4 档 t37/t4/t7/t8）；单元 4 测试全 PASS；vet 错误集与 HEAD 完全同（仅行移 3309→3348）。⚠️ **晋级阻塞——外部工具链回归**：当代 Go 源码（deac8377 已提交＋并行会话未提交 WIP）编译【与 noc-r1/r2/r3 时代逐位元组相同】的 HEAD ps.no，产出的 noc 在 t31-standalone-if 与 tests/mkdtemp-debug.no 丢失 standalone-if 链的 inline else（`BlockStatement{inline:true}` 消失，签名一致；284→283）；三代工具链 parse ps.no 自身 AST 逐位元组一致⇒纯 lowering/codegen【静默误编译】；且昨日 20:12 的好状态只存在于【未提交】WIP（fd3f2931 提交信息描述的 parser 工作并未含 Go 文件入仓），不可复现。⚠️ **工具链事故升级与解法（s46 期间）**：主树当代 Go（deac8377+未提交 WIP、bin/no@02:18）以【cache-off】编译 HEAD ps.no 仍丢失 standalone-if inline else（t31+mkdtemp 同签名）——属当代 lowering/codegen 真回归、待 Go 侧修复；但 **纯 346d69ae（v0.4.4）与纯 c872721a 两代工具链 cache-off 均 GOOD**⇒破坏面锁定在 deac8377 及其后 WIP 区间。处置：晋级二进制改用 **git 钉住的纯 v44 工具链 + NOLANG_AST_CACHE_OFF=1 构建**（/tmp/noc-s0＝noc-s46f、sha 51de820a），旧基线存 /tmp/noc-s0-s45.bak。⚠️ **二分期中踩到的缓存新坑**：ASTVersion 只在 commit 时 bump，相邻两代未 bump 的 WIP 共享同 key（content, ASTVersion, StdSigKey）⇒cache-on 的文件级二分结果全是假象（本会话曾被 b3「GOOD」误导数轮）；**任何差分/二分实验必须 NOLANG_AST_CACHE_OFF=1 或隔离 HOME**（该开关只保 user-module AST 盘缓存层）。）→s47=308（**后辍圆括号 range 切片 `f(a..b)`/`f(a..b]`**——语料 7 个 SIGBUS 档的元凶：Go expr.go LPAREN 臂有 isRange 探视（`(` 后跟 ELLIPSIS、或 INT/IDENT 紧接 ELLIPSIS ⇒ parseSliceExpression，leftInc=false、`]` 闭合 rightInc=true；否则普通 CALL），而 ps.no 后辍链把 LPAREN 一律当 CallExpression——`a(1..4]` 落入 call 参数循环的防呆步进＋外层 pgo 重循环⇒死循环/SIGBUS。新增偏移 1/2 token 探视分支（镜像 Go saveState 探视）；并补 LBRACKET 臂两处 en 条件缺 `!= RPAREN`（Go 双闭合）。fixture `t51-range-call.no` 与 oracle 逐位元组相同。门禁：fixture 48/52（仅余既有 t37/t4/t7/t8）；单元 util/tok/lx/ast 4/4 PASS；vet 错误集与基线完全同集（行号全 +40 位移）；语料门 corpdiff(noc-s0[s46]→noc-s47)＝ **+6 档、0 回归**（arr/str/txt/vec-slice、slice-open-inclusive、mem-safety/reverse-slice-clone 六个原 BUS 档全匹配）、302→308。⚠️ **方法学坑——超时误报第三机制**：首跑 -P 4/30s 并发扫描把 sha256/yaml 报成「新增 SUT-FAIL 回归」——实因该两档 noc 解析耗时 ~15-25s 处于临界，并发负载压爆 alarm；串行 -P 2/90s 复测 s0/s47 行为完全一致⇒**语料扫描须 -P ≤2 + alarm ≥90s**，否则慢档被误判回归（与缓存假象、跨代误编并列第三独立机制）。晋级同 s46：git 钉纯 v44 工具链 + NOLANG_AST_CACHE_OFF=1 构建，/tmp/noc-s0＝noc-s47、sha1 fc0216b4，旧基线存 /tmp/noc-s0-s46.bak；余 SUT-FAIL 9 档（aes-cbc/net/fmt-heuristic/fn-type-{result,return}/map-key-leak/str-order 挂死、tls SEGV、https-server BUS——皆 noc-s0 时代既有，非本增量引入））→s48=370（**陈述位 `{ … }` 区块：BlockStatement wrapper + 裸 match desugar**——首分歧调查最大桶：抽样 200 档中 ≈48 档卡 `BlockStatement{}` wrapper 缺失、≈22 档卡裸 match 合成 `IntegerLiteral{value:1}`。此前 `ps-parse-stmt` 无 LBRACE 臂⇒防呆步进吃掉 `{`、语句外泄一层；裸 match `{ cond -> body … -> else }` 完全未实现。新增 `ps-parse-brace-stmt`（镜像 Go stmt.go:402 saveState/restoreState：`ps-bm-scan` 探深度 0 `->`（排除 `{ IDENT : {` 结构化形）⇒先试 `ps-parse-bare-match`、失败还 p.pos 回落普通 `ps-parse-block`+消费 `}`）；`ps-parse-bare-match` 镜像 match.go:134 三形臂体（`{…}` 区块（先试嵌套裸 match）、NEWLINE 块形（statements 至 isArmStart/`}`，inline:true）、同行单语句）+ lowering.go:1727 buildBareMatchDesugar 两装配路：全臂非 wildcard 且 n>1⇒fast path `IfExpression{IntegerLiteral{1}(合成 ps-mk-true), Block{各臂兄弟 ExpressionStatement{IfExpression}}}`，否则倒序 elif 链（尾 wildcard `-> body`＝`IfExpression{IntegerLiteral 1, body}`）；中途 wildcard／臂首 `#{`／`ok->` 保守 bail 还原。**⚠️ match 非排他/有状态守卫三新坑**：(1) wildcard 臂消费箭头后重评 `ps-peek!=RARROW` 因状态已变而误 fail⇒`ad` 快照旗标；(2) 倒序链 `w==1&&res>0→bail` 被本臂刚建的 res 再触发⇒`hdw` 快照；(3) 嵌套裸 match 尝试失败时 p.pos 已越过 `{`、回退路再 bump⇒双消费⇒`j <- [0..N): {` 被吞成 SliceLiteral 整档挂死（http/server 180s rc=142）⇒尝试前 `nsave=p.pos`、失败还原。门禁：fixture 49/53（仅余既有 t37/t4/t7/t8）、单元 4/4 PASS（⚠️ 当代 oracle 新增命名规范检查拒 `_test.no` 下划线路径⇒复制到 tmp/zz-s48gate/*-test.no 运行）、vet 10 既有 ERROR 集不变；语料门（-P 2/90s 全 647 档）＝DIFF 330→268、**新增匹配 62 档、0 回归**（SUT-FAIL 集与 s47 完全同 9 档；http/server 由挂死转 rc=0 且与 oracle 差异行数 6435→1394 / 3089→433，仍属既有 DIFF 档）。晋级：纯 v44 工具链 cache-off，/tmp/noc-s0＝noc-s48 sha1 c53a5641，旧基线存 /tmp/noc-s0-s47.bak）→s49=381（**语句位 `*`/`**` 速写 + `#N` 标签循环**——s48 后首分歧调查最大可解桶：oracle `BlockStatement{}`/`BreakStatement` 侧 ≈48 档中相分成两簇，其中 index-out desugar 簇（≈39 档，Go lowering.go maybeIndexOutAssign）需 sem 函数作域型别表（FuncVarType/idxLocalTypes）+ bounds.go 可证明界内豁免（inBoundsIdx）才可逐位元组复现，超出 parse-local 范畴留待后续；本增量取 `*`→break／`**`→continue 速写簇：Go parseStatement 对语句起始 MUL 走 parseStarBreakOrExpr（stmt.go:552/2913，peek(1) ∈ {NEWLINE,EOF,SEMI,RBRACE,LABEL,IDENT} 才成立、可跟 `#N`/IDENT 标签）、STAR_STAR 走 parseStarStarContinue（stmt.go:558/2937，无閘门）；ps.no 陈述位无 MUL/STAR_STAR 臂⇒`cond -> *` 被防呆拆成 `InfixExpression{op:*}[<nil>,next]`（http.no 首分歧即此）；新增 ps-parse-star-break/ps-parse-star-star-cont/ps-star-label/ps-mk-break-cont 四函数+两 dispatch 臂；另补 `#N` 标签前缀臂（parseLabeledStatement 保守版：消费 LABEL 后递回 ps-parse-stmt，若得 ForStatement 则改写 slot0 label）。⚠️ 多返回值坑：`(label, kind)` 裸组尾式非法语法、`x,y = f()` 解构仅适用已命名结果惯用法——改用 `[]str` out 参数传 label/kind。fixture `t53-star-break.no`（裸 match 臂体 `*`/`**`、`{ }` 内裸 `*`、`* #1`/`** #1` 标签、standalone-if 链）与 oracle 逐位元组相同。门禁：fixture 50/54（仅余既有 t37/t4/t7/t8）、单元 4/4、vet 10 既有；语料门（-P 2/90s 全 647）＝DIFF 268→258、**新增匹配 11 档、0 回归**（tests/https-server.no 由 SUT-FAIL rc=138 SIGBUS 转 rc=0 DIFF、属改善；http.no 与 oracle 差异 1394→185 行）。晋级：/tmp/noc-s0＝noc-s49 sha1 bb07e0ed，旧基线存 /tmp/noc-s0-s48.bak）→s50=383（**区块内换行后 `-> else` 续接（standalone-if else-attach）+ 工具链纪律再确认**——oracle 当日三代重建（08:40→09:56→10:30），10:30 代把 wrapStandaloneChainDepth 定为递归形（stmt.go:1768 注释引 str.replace-n 事故）后， promoted noc-s49（当时误用【主树 bin/no@08:40】编译，违反 s46 钉死纪律）静默丢失链式 `a -> b -> f` 最内层 consequence 而被漂移照出；本增量起一切 noc 构建一律改用 /tmp/nolang-v44 自带 `./bin/no`（纯 v0.4.4 钉死 + cache-off）。新特性：复刻 Go parseBlockStatement 迴圈开头 stmt.go:2168–2186——区块内上一条 standalone-if（RTStandalone、Alternative==nil）后换行的 `-> body` 挂为其 Alternative；实现弃用「建节点后原地覆写 kids」方案（ast.no kid-set），改在 ps-mk-standalone-if 组装前【前探】：跳 NEWLINE/SEMICOLON 后恰为 `->` 即就地吃掉并把 body 写进 alt 槽（节点一次成型、零覆写）；psst 加 inBlk/inMatch 上下文位：program 顶层不挂（t31 系顶层 `d -> e -> f`+`-> g` 分离形）、臂解析中抑制（裸 match/match 自行消化 wildcard 臂），ps-parse-block 进入时 inMatch 清零（臂 body `{ }` 内续接合法）。fixture t54-newline-else.no（j1 形/块形 else/（A）wildcard 三连挂/顶层不挂）与 oracle 逐位元组相同。门禁：fixture 51/55（仅余既有 t37/t4/t7/t8）、单元 util/tok/lx/ast 4/4、vet 10 既有 ERROR。语料门（-P 2/90s 全 647，oracle 10:30 代）＝SAME 383 / DIFF 256 / SUT-FAIL 8（全 rc=142 慢档、与 s49 同集）；vs s49 名单：新增 tests/{float-ne-nan,set}.no、丢 0 档（漂移日净匹配受 oracle 三代重建主导，attach 本身解锁的是语料内裸 match 回退区块形如 json.no 内层块）。晋级：/tmp/noc-s0＝noc-s50g sha1 c766715f，旧基线存 /tmp/noc-s0-s49.bak。⚠️ **事故记录（归因错误）**：s50 期间一度把链式丢 consequence 误判为 ast.no 新增 kid-set（i64 vec 字段元素写入）「污染全模组 codegen」——单变量实验 s50e 因同时混入了错误工具链（主树 bin/no）构建而给出假象；真因＝工具链代次误用，kid-set 无罪（后因设计更优而保留弃用）。差分对照一律同工具链代次重建；晋级前必须用 v44 `./bin/no` 重建并跑 fixture 门。新基线首分歧分桶（256 档）：ORACLE+ LetStatement 104／BlockStatement 48（index-out 簇 ≈39 + 裸 match mid-wildcard bail 簇如 t54 旧 g/h 形）／FunctionDefinition 16／NOC+ ExpressionStatement 11）→s51=413（**同檔裸名呼叫之 parse 期回傳型別推斷**——s50 後最大桶：256 檔首分歧 LetStatement 家族 132（裸 63＋nullable 34＋合成 35），其中 44 檔直接分歧於 `value: CallExpression` 的型別蓋印。復刻 Go parseLetStatement 的 CallExpression 推斷分支（stmt.go:1502–1533）＋ type.go 的 returnTypeFromCallExpr／filterInferableType／markInferred：RHS 為 `CallExpression{fn: 裸 Identifier}` 且簽名表有【單值】回傳 ⇒ 過濾（`?T` 恆留；排除精確 i64/i32/i16/i8/u64/u32/u16/u8/f64/f32/bool/byte/char/fd；含 `|` 排除——union 排除本增量未實作，風險已註於碼）再走三閘門：未宣告→蓋 Type＋setVarType；已宣告且回傳 `?T`→僅 setVarType 不蓋 Type；已宣告非 option 且既有 varType ∈ {'', 'i64'}→蓋 Type。順序註冊（ps-parse-func 於 body 解析完後、僅 outs.len()==1 才登記 sigN/sigP）⇒前向引用與遞迴不推斷（oracle 探針 inf2/inf3 釘死）；方法定義不進裸名表；形參/回傳 varType 註冊提前至 body 前（mirror decl.go:1756）。psst 增 12 欄（fn/lastTp/sigN/sigP/dclF/dclV/gclN/fvF/fvV/fvT/gvN/gvT）實現 FuncVarType（fn→全域 fallback）與 IsFuncDeclared（tracked 函式僅查 pair）。**克隆方案＝token 位置重入**：簽名存回傳型別起始 token 下標（ps-parse-param 捕 p.lastTp），使用處跳 p.pos 過去重跑 ps-parse-type 生成新節點（零既有節點突變）、回pos；ps-mark-inferred 逐 kind 遞迴寫 'true'（NULLABLE 只蓋子節點——dumper 標頭不印 inferred）；被拒克隆成無 kid 引用孤兒⇒零回歸。ps-parse-stmt 六明示型別分支＋typedecl 兩臂補 ps-reg-let 登記（復刻 Go parseLet 尾部 DeclaredVars+SetFuncDeclared）。fixture t55-call-infer.no（首個 `?str` 蓋印／重指派抑制／i64 過濾排除／`[]str` SliceType 推斷／字面量恆蓋／後定義函式不推斷）與 oracle 逐位元組相同。門禁：fixture 52/56（僅余既有 t37/t4/t7/t8）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR 集不變；語料門（-P 2/90s 全 647）＝**SAME 384→413（+29）、0 回歸**（SUT-FAIL 8 檔 rc=142 與基線同集慢檔），新匹配 x25519-* 全系列、mem-safety/struct-* 簇、move/slice/view 諸檔；args.no 差異行數 103→91（剩餘皆 lowering D6 `?char`/`?i64` 合成偽影，超出 parse-local 範疇）。⚠️ oracle 當日 12:26 因平行會話「remove explicit main」重構重建，基線探針全組復核 SAME 後才动工。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s51 sha1 33fd562b，舊基線存 /tmp/noc-s0-s50.bak）→s52=417（**前置 `#{buildin}`/`#{intrinsic}` 註解標記 ⇒ 節點旗標**——s51 後 226 檔首分歧重分桶：LetStatement 79（it-let 束縛 20／D6 option-index 23／CallExpression 残留 10 皆 lowering 产物）、BlockStatement 48（index-out 簇）、**builtinStub 10 檔**為最大單標記 parse-local 簇。語料 17 檔 152 處 `#{buildin}`（裸形＋`#{buildin, intrinsic}` 組合形）、`#{intrinsic}`（含 `#{win-amd64, …, intrinsic}` 平台組合形）。鏡像 Go attachAnnotations（annotation.go:349–378）：FunctionDefinition 上 'intrinsic'⇒Intrinsic（slot5 'true'）、'buildin'⇒BuiltinStub（slot6）＋可選 Value⇒BuiltinName（slot7，語料暫無 `=NAME` 形、預留）；TaggedEnumDefinition 上 'buildin'⇒Builtin（slot1，global.no option 即此）。槽序由 dump.go:272–279 渲染欄序列導出（name0 colonSyntax1 methodDef2 skipNamingCheck3 overflow4 builtinStub5 builtinName6 builtinGroup7；`#{intrinsic}` 已於 2026-10-22 從 Go 側移除，原 intrinsic5 槽消失、其後各槽（builtinStub/builtinName/builtinGroup）整體前移一格）——dumper（asp.no strs 表）早已就緒⇒纯 ps.no 增量（新 helper ps-apply-builtin＋註解迴圈收集 bs/intr/bn＋尾端套用一點）；非函式/非列舉目標忽略旗標（`#{buildin} e = 1` 探針釘死）、不可附著形仍走獨立 AnnotationStatement（Go 同口徑）。多載繼承（後續同名定义 BuiltinGroup）與尾隨同行形語料暫無、未實装。fixture t56-buildin-annot.no 與 oracle 逐位元組相同。門禁：fixture 53/57（僅余既有 t37/t4/t7/t8）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR 集不變；語料門（-P 2/90s 全 647）＝**SAME 413→417（+4）、0 回歸**（SUT-FAIL 8 檔同集），新匹配 global/os/bool/net/unix；async.no 殘差 5 行為 `*i8` PointerType 參數（新缺口）、time/fs/process/number 仍卡下游簇。⚠️ oracle 當日多次重建（12:26→13:24→13:51），掃描後對新 oracle 複驗解鎖檔＋全探針 SAME。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s52 sha1 b71b1210，舊基線存 /tmp/noc-s0-s51.bak）→s53=423（**頂層型別別名——union / 單一 / 函式型別三形**——s52 後重分桶選 TypeAlias 簇：`float = i8|f64|…` 系首分歧 Go 為 `TypeAlias{…Union…}` 而 noc 拆成 LetStatement+InfixExpression。復刻 Go stmt.go:151-170 分派序（isFnDef→isFnTypeAlias→looksLikeEqAlias→let）：type.go:554 looksLikeEqualsTypeAlias（IDENT+OR→scanIsUnionTypeAlias 深度 0 掃描（NEWLINE/EOF/;/閉合符→真；含 QUESTION 在內二元運算子→假；LPAREN 內容限 {IDENT,IN,RPAREN}）；IDENT+陳述邊界→內建 16 型名或 **typeAliasNames 註冊表**；`[]T`/`[N]T`/`?T` 形）、type.go:688 parseTypeAlias（`|` 平鋪收集成 UnionType{types:N}）、type.go:406/433/512 fn 別名形（首 `(...)` 後須陳述邊界或第二 `(...)`）。**三項 ps.no 機械發現**：（a）kvc 計數頭部（`types:`/`params:`/`results:`）由 dumper 讀**字符串槽**渲染、子節點遍歷讀 c 槽——builder 須 set-slot(.to-str()) 與 set-count 兩設（沿 CallExpression 既有範式）；（b）同塊多臂 match 中**裸 `->` 尾臂會被當尾隨無條件陳述執行**覆寫 idx——別名分派全臂加 `aliased == 0 &&` 守衛、無裸臂；（c）Go fn 型別參數規則：`(...)` 內單 IDENT 為**無名具型參數** `Parameter{name: , type: NamedType}`（不同於 FD 形參）——新 helper ps-parse-ftype-param；且結果組換行探測須 `sp = p.pos` 保存、未命中回滾，否則吃掉終結 NEWLINE 使下行文註錯掛為上陳述 Comment。psst 增 `alN []str` 註冊表。⚠️ 記錄偏離：別名閘門用 `p.fn == ''`（僅頂層）替代 Go 的 saveState 試解析——語料函式體内 `x = (expr)` 形皆被 Go 內容掃描否決，頂層閘門安全。fixture t57-type-alias.no（union/單一/註冊表引用/cb0 空參/fn 雙組/負形 `or-value = 1 | 2`、`x = (or-value)`）與 oracle 逐位元組相同。門禁：fixture 54/58（僅余既有 t37/t4/t7/t8）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR。語料門（-P 2/90s 全 647）＝**SAME 417→423（+6）、0 回歸**（SUT-FAIL 8→6 恰為 fn-type-result/return 轉 SAME 脫離慢檔集），新匹配 src/std/{float,num}.no、tests/{fn-type-params,fn-type-result,fn-type-return,named-fn-type}.no；int.no 殘差 8 行=`?int` NullableType 推斷 let 形（新簇出列）。⚠️ /tmp/nolang-v44 被 macOS /tmp 清理蒸發，重建：git clone + checkout v0.4.4 + make no。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s53 sha1 86907d51，舊基線存 /tmp/noc-s0-s52.bak）→s54=427（**D6 有號整數算術 option 推斷——inferOptionArith**——s53 後 218 DIFF 重分桶：LetStatement 86／BlockStatement 49，選中「`type: ?i64/?int, value: InfixExpression`」簇 16 檔（std 8＋tests 8）。復刻 lowering.go:493 inferOptionArith（parse 完成後 walk 逐 LetStatement）：無明示型別、非合成、值剝括號後為 op ∈ {+ - * / %} 的 InfixExpression 且兩側皆「靜態可知有號整數」（IntegerLiteral→i64；Identifier→FuncVarType→lowerSignedIntName 僅收 {int,i8,i16,i32,i64}、限定名取最後 '.' 段、剝前導 *；self/`.`→接收者型別；巢狀算術遞迴取較寬者、平手取左）⇒就地合成 NullableType{NamedType{base}}（dumper 對 NullableType 頭不印 inferred）＋setVarType(name,'?base')。D1：`#{overflow=...}` 不豁免（d6f/sha3/time 探針釘死仍蓋印）。ps.no 增量：五 helper（ps-unwrap-grouped／ps-is-arith-op／ps-lower-signed-int-name／ps-signed-int-operand／ps-mk-opt-arith-type）＋ps-parse-let 尾一鉤點＋ps-parse-method-func 預登記 self=接收者型別（鏡像 lowerSelfType 讀 Results[0]——裸 `.` noc 渲為 Identifier{self} 需在 body 前可查）。⚠️ 偏離：單趟查表 vs parse 完後 walk——前向引用運算元不蓋印（語料未見）。**閘門教訓（首掃曾丟 3 檔回歸）**：captureTargetAllowsOption 不是「查 varType 空即放行」！Go 以 declaredLocals（walk 序出現名單：函式作用域每次進入 FunctionDefinition 重置、形參先入帳、字面量 let 只入名單不入型別表）＋localDeclaredType（**刻意不做全域回退**，lowering.go:3377 事故註釋：他函式 `c i64` 參數污染）雙表判定——「本作用域已出現且型別表查無 entry」也拒，保護 `r = 0`→`r = 10/3` 重指派形與純量出參；新增 ps-walk-declared／ps-local-type＋psst.dclT（僅頂層名單）重造閘門後 net/ip、sha256-simplified、number-div-mod-clamp 回 SAME。⚠️ 誤編譯事故：臂內寫 `-> { r = 'int'; return }`——`;` 是行註解吞掉 ` return }`、括號失衡使後續整檔併入前函式，報錯 `EmitLLVM: unknown callee ps-parse-if in func ps-parse-stmt` 距真因極遠；改互斥條件單表達式形。fixture t58-d6-arith-option.no（正形：函式參數／剝括號／巢狀／方法 `.`；負形：u8、純字面量變數運算元、明示純量重指派、出參、union 別名接收者、D1 overflow 仍蓋、先字面量 let 後重指派拒蓋）與 oracle 逐位元組相同。門禁：fixture 58/59（僅余既有 t7-group-nil-float）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR（無一落於新碼區）。語料門（-P 2/90s 全 647）＝**SAME 423→427（+4）、0 回歸**（SUT-FAIL 6 檔 rc=142 同集），新匹配 src/std/{int,net/hpack}.no、tests/{1-test,basic}.no；簇內其余 12 檔殘差屬 D7 安全索引 `?char`／armBinding 型別／Comment 附著等其他簇。⚠️ oracle 當日漂移 15:02→16:16，晉級後對新 oracle 復核 25 探針＋抽樣 40 原 SAME 檔全 SAME。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s54 sha1 dedb47ef，舊基線存 /tmp/noc-s0-s53.bak）→s55=441（**armBinding it 合成綁定型別蓋印——buildItBindingForArm**——s54 後 214 DIFF 重分桶選中「`LetStatement{name: it, type: T, …, armBinding: true}`」it-let 簇 21 檔。復刻 lowering.go:2809 buildItBindingForArm ＋ per-arm 補集表 :2176–2235：主題靜態可知 `?T` ⇒ err 臂→err、nil 臂→nil、ok 臂→T、wildcard 臂取補集（全列 ok/err/nil ⇒ Go skipItBinding 死臂**不建任何綁定**；列 ok+nil→err、列 ok+err→nil、僅列 ok→`err | nil`、未列 ok→T）；型別未知（t==''）維持 s46 兼容形——僅 err/nil 關鍵字臂帶變體名、ok/wildcard 落回裸 shared 綁定。ps.no 三處增量：（a）`ps-mk-it-let-arm` 泛化為 `ps-mk-it-let-t(t, subj, bt, bname)`（bt 非空⇒NamedType{bt}+s1 synthetic+s2 armBinding；bt 空⇒同 ps-mk-it-let 裸形）；（b）`ps-match-listed` 深度追蹤 token 預掃（不消費、不建節點）收集「緊接 `->` 的 ok/err/nil 關鍵字臂」名單，鏡像 Go parse 後 hasExplicitOk/Err/Nil（嵌套 {…}/(…)/[…] 增深度防内層臂污染補集判定）；（c）ps-parse-match 接線——listed 預掃、`ok` 關鍵字臂 kvar 識別、bt 計算、dead 旗標（全列跳過整條 it 綁定）、it varType 逐臂登記與還原（鏡像 collectVarTypesFromBody 作用域掃描，保巢狀 match 内層 it 不污染外層——match-it-type-scope 回归守）。⚠️ 偏離：`Program{warnings:N}`（死臂 unreachable 警告計數）與未知型別主題 wildcard 裸綁定 vs 無綁定之別屬警告計數／下游簇，本增量未觸及（探針 e55a 剔除此二形後與 oracle 逐位元組相同）。fixture t59-arm-binding.no（n ?i64：err/nil/wildcard、ok/wildcard、僅 err/僅 nil/純 wildcard；m ?str：ok/nil/wildcard 補集 err；巢狀 match 作用域還原）與 oracle 逐位元組相同。門禁：fixture 59/60（僅余既有 t7-group-nil-float）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR（無一落於新碼區）。語料門（-P 2/90s 全 647）＝**SAME 427→441（+14）、0 回歸**（SUT-FAIL 6 檔 rc=142 同集、ORB-FAIL 0），新匹配 tests/{fs-error-complete,fs-error-simple,minimal-it,minimal-it2,opt-match-all,opt-match2,opt-match3,option-nil-predeclare,self-write-str,struct-field-leaf-clone,test-div-mod-option}.no、mem-safety/{option-cmp2,option-match-basic,option-match-direct}.no；簇內其余 7 檔（match-it-type-scope/opt-box-drop/option-capture/tagged-enum/test-div-zero/str-field-set-clone）殘差屬複合指派／enum 載荷析構／D7 安全索引等其他簇。⚠️ oracle 當日漂移 16:16→17:09，首掃 LOST=0 已對新 oracle 復核成立，晉級後復驗 14 新檔＋抽樣 39 原 SAME 檔全 SAME。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s55 sha1 054f3851，舊基線存 /tmp/noc-s0-s54.bak）→s56=445（**W_SEMI_EAT 行尾分號註釋計數——Program{warnings:N}**——s55 後 200 DIFF 首分歧分桶：LetStatement 62／BlockStatement 54／Program 9，選中 warnings 桶：4 檔唯一差異即 `Program{}` vs `Program{warnings:N}` 頭行。復刻 Go buffer.go::warnSemiSwallow 啟發式（advanceCollect 對每個符合的 COMMENT token 發一條 SeverityWarning，parser.go::Warnings 將全部 warning 匯總進 Program.warnings）：① Marker == ';'（單分號行註釋；`;;`/`;;block` 系永不警告）② 註釋文字含 `{` 或 `}` ③ 前一非 COMMENT token 為同一行代碼 token（行尾而非行首）。啟發式純詞法 ⇒ ps.no 以 `ps-semi-eat-count` 對 toks 前向預掃（last 指針跟蹤前一非註釋 token），`parse` 寫 Program 槽前疊加進 p.warn 總和——Go 的 warnedSemiEat[idx] 回溯去重在預掃下天然無需。探針 tmp/p1-p3 釘死：`;;` 行首/同行→0、同一行兩組大括號→僅 1（同屬一條 COMMENT token）。rc4/move-eligibility-improved/range 三檔首行也是 warnings，但 noc 過度計數的根因是 `[1,2,3]:` 陣列字面量被誤析為 RangeExpression 的 body 大 diff（他桶）。fixture t60-semi-eat.no（正形三行含括號／負形行首 `;{}`、`// {}`、`;; {}`、無括號 `;`／同行雙括號串）與 oracle 逐位元組相同。門禁：fixture 60/61（僅余既有 t7-group-nil-float）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR。語料門（-P 2/90s 全 647）＝**SAME 441→445（+4）、0 回歸**（SUT-FAIL 6 檔 rc=142 同集、ORB-FAIL 0），新匹配 tests/{map-to-str,struct-to-str,scratch-fe,print-eprint-to-str}.no。⚠️ oracle 當日漂移 17:09→18:16，首掃 LOST=0 對新 oracle 復核成立，晉級後復驗 4 新檔＋探針＋抽樣 40 原 SAME 檔全 SAME。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s56（構建 sha1 ceaf5862，晉級 codesign 後 1e872f5d），舊基線存 /tmp/noc-s0-s55.bak）→s57=448（**for 族語法補齊：range-for 匿名切片字面量源 + 計數循環雙形**——s56 後 196 DIFF 首分歧分桶：LetStatement 62／BlockStatement 54／ForStatement 6／Program 5，選 for 兩簇：（a）range 源 `i <- [1,2,3]:` 被 ps-for-source 一律當 RangeExpression 誤析（s56 副發現：rc4/move-elg/range 三檔过度 warnings 之真因即此）、语料切片字面量源 4 檔；（b）計數循環 `N * { }`／`{ } * N` 完全缺臂（for3.no 前置形 8 行差）。**鏡像 Go parseForRange LBRACKET 分類**（stmt.go:2582）：saveState→跳 `[`→peek ELLIPSIS 先行⇒kind1；否则 parseExpression(LOWEST) 後 ELLIPSIS⇒kind1（[a..b]/[a..]）、COMMA|RBRACKET⇒kind2（匿名切片字面量 [a,b]/[a]）；restoreState；kind2 整段交 ps-parse-expr 之 LBRACKET 前綴臂建 SliceLiteral 填 IterationExpr.RangeExpr(kid1)（非 range 非切片之 kind0⇒保持舊無分類行為，Go saveError 形無以復現）。分類期投機 parseExpression 所建節點不被任何 kid 引用即孤兒（s40/s51 先例）⇒零回歸。**計數循環雙門+雙 builder**：s57-count-prefix-p 鏡像 isCountLoopPrefix（k=1 用 peekToken、k≥2 用 look(k-2)；`*` 後必緊接 `{` 与乘法區分）於陳述分派 INT/SUB 臂前；s57-count-blockfirst-p 鏡像 isCountedLoopBlockFirst（平衡大括號掃描+`#{` 群組跳過、匹配 `}` 後 MUL [SUB] INT 且 INT 後必為 NEWLINE/EOF/SEMI/RBRACE 語句結束以免 `} * n` 乘法歧義）於 ps-parse-brace-stmt 入口優於裸 match/普通區塊；二者皆建 ForStatement{label:''} 6 guarded CHILD 僅 CountExpr/Body 非零，CountExpr=IntegerLiteral{value:字面量}（無 raw⇒slot1 空串不印，ps-mk-true 先例；Go ParseInt base10 拒 hex 故 hex 安全落回運算式形）。負號形把字面量前綴 '-'（Go intToken.Literal = "-"+lit）；標籤形 `#1 N * { }` 由既有 LABEL 臂遞回+改 slot0 天然覆蓋（e1 探針釘死）。⚠️ **新坑（MIR 層靜默誤讀）**：同一函式體內多次 ps-peek-at（含變量偏移）會讀到陳舊寄存器——探針实計：同體 pa1/pa2/pa3 局部正确，但嵌套臂內 `ps-peek-at(p, ko)`（ko==2）返回 ko==1 時的舊結果，而直接索引 `p.toks[p.pos+ko].typ` 恆正確；vet 的 fxxoptarg 錯位（ps.no:4401/4404/4415 等 6 處既有 `p.pos+1` 傳參）側证此族降格路径确有幉角⇒**新閘門一律改 ps-bm-scan 式直接索引掃描（index-out=zero 守衛）**。fixture t61-for-shapes.no（切片源/範圍源對照/前置計數/後置計數/負計數/標籤前置形）與 oracle 逐位元組相同。門禁：fixture 61/62（僅余既有 t7-group-nil-float）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR（無一落於新碼區）。語料門（-P 2/90s 全 647）＝**SAME 445→448（+3）、0 回歸**（SUT-FAIL 6 檔 rc=142 同集、ORB-FAIL 0），新匹配 tests/{for3,range,call-heavy}.no（call-heavy 為分類修正的級聯紅利）。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s57（構建 sha1 b5c4ac9e，晉級 codesign 後 25a178bb），舊基線存 /tmp/noc-s0-s56.bak）→s58=451（**參數/型別特殊形狀 + 方法回傳型別推斷**——s57 後 193 DIFF 首分歧分桶：LetStatement 62／BlockStatement 55／Identifier 9／Parameter 5，選 B 簇「型別形狀」：（a）參數/回傳位 `*T`/`**T`/`T*`/`&T`/`?&T` 全缺臂（async.no `task *i8`、view-basic `(v &view-point)`）；（b）參數名位缺 IN token（`in []byte` 被丟名）。鏡像 Go parseParamTypeAfterName（type.go:997）：前綴 `?`/`&` 循環＋`*` 循環＋STAR_STAR＋base＋後綴 `*`/`**` 正規化為 `? & * base` 再 buildType（type.go:1206）逐層包節點——noc 單趟直接節點化等價：ps-parse-type 新增 MUL→PointerType、STAR_STAR→雙 PointerType、AND→ViewType 三臂（遞迴先建內層 base 再包外層，子先於父）；IDENT 臂尾加後綴 `*`/`**` 包裹迴圈（`i8*`⇒`*i8`）；ps-parse-param 型別門擴 {MUL,STAR_STAR,AND}、名門容 IN。⚠️ 權威面探針釘死：oracle 函式字面量**參數位**不接 `&T`（isFunctionDefinition 掃描集無 AND，`(b &i64)` 落回 GroupedExpression）但 **results 位**接（掃描無條件跳過）——語料只用 results 位，noc 偵測比 Go 寬的偏差只在語料外形（e2 探針既有之偏離、非本增量引入）；頂部明示 `x &str = 1` oracle 也落回運算式（let 型別門不碰 AND，未改）。**級聯子簇：方法回傳型別推斷（s51 同族 DotExpression 臂，type.go:96 returnTypeFromCallExpr）**——view-basic `q = p.self-view()` 之 `type: &view-point` 為 parse 期推斷非明示：新增 psst.msN/msP 方法簽名表（鍵 'recv.method'，body 解析完才註冊、outs==1 限定、opos[0] 起點 token，鏡像 Go decl.go 尾註冊 methodSignatures 語意）＋ps-msig-find 反掃；ps-parse-let 推斷區加 DOT 臂：接收者 IDENT→ps-var-type 解接收者型別→查表→跳 token 重跑 ps-parse-type 克隆（未採用即孤兒零回歸，s51 法）＋filter-ok/declared 雙閘同裸名臂；StructLiteral let 臂僅登 varType 不蓋 stmt.Type（Go case *StructLiteral 語意，!exists 守衛）。K_POINTER_TYPE=51/K_VIEW_TYPE=71 asp.no 早已註冊、type-string ast.no:632/634 已渲 `*`/`&` 前綴、dumper kind-name 通用⇒纯 parser 增量。fixture t62-type-shapes.no（*T/**T/T* 後綴正規化/&T results/?&T+推斷/[]byte+IN 名對照/負形 `3 & 5`、`6 * 7` 保持 InfixExpression）與 oracle 逐位元組相同。門禁：fixture 62/63（僅余既有 t7-group-nil-float）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR（無一落於新碼區）。語料門（-P 2/90s 全 647）＝**SAME 448→451（+3）、0 回歸**（SUT-FAIL 6 檔 rc=142 同集、ORB-FAIL 0），新匹配 src/std/async.no、tests/{view-basic,view-optional}.no；Parameter 桶另兩檔 aes-ctr/tdes 首分歧已前移至 D7 安全索引簇（275/718 行殘差）。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s58（構建 sha1 cf4ee19f，晉級 codesign 後 e5dcc20d），舊基線存 /tmp/noc-s0-s57.bak）→s59=464（**match 臂区／區塊尾 註釋附着——armDoc 協議 + 區塊內部協議 + if 尾非破壞性預看**——s58 後 190 DIFF 首分歧分桶：Doc 7／Comment 7／TrailingComments 1（每檔僅 1–3 行差、皆缺註釋附着欄位），選中此註釋附着簇。鏡像 Go match.go 權威：臂循環頂 RBRACE/EOF 檢查先於 collectDocComments（:167-174）、`armDoc := p.collectDocComments()` 於型樣前收集（:208）、套用規則（:440-452）inline 體→`setDoc(bodyStmts[0], armDoc)`、區塊體→TrailingComments（空則直設、非空則 armDoc 併入最前）；區塊臂內部走 parseBlockStatement 委託（:265-431）：leading standalone→【其後】陳述 Doc、行尾→【前行】Comment、`}` 前→TrailingComments。**探針 p4 釘死 bare≠explicit 不對稱**：裸 match `{ }` 區塊臂 armDoc→TrailingComments 最前且保留內層首陳述 Doc；明示 `subj: { }` 區塊臂 armDoc **覆寫**首條真陳述之 Doc（內層首 doc 被棄）；兩 form 之 inline 臂皆 armDoc→體首 ExpressionStatement.Doc + 同線行尾註釋→Comment（attachInlineComment）。實作五批：① `ps-parse-if` else 預看由破壞性 `ps-skip-trivia` 改直接索引非破壞掃描（`p.toks[k].typ`、index-out=zero 守衛，遵 s57 MIR 舊寄存器閘門），查無 ELSE 則 pos 不動⇒if 區塊後函式尾註釋不再被吃掉（root cause：move-clone-liveness 等函式尾 TrailingComments 遺失）；② `ps-parse-match` 臂循環重構（armDoc 收集、區塊臂全協議 btrail/bprev/bfirst/bdoc 逐陳述 set-doc＋行尾 set-cmt、循環後 armDoc 覆寫 bfirst.Doc 或入 btrail、`ps-attach-trailing`）；③ inline 臂 armDoc→set-doc + 同線 COMMENT→set-cmt；④ `ps-parse-bare-match` 三體形（LBRACE 區塊／NEWLINE 區塊／同線單陳述）同協議；⑤ 新 helper `ps-premerge-trailing`——aux 區僅可附尾，prepend 需把舊區整段拷進新 aux 段（舊區孤兒無害：dumper 跟 ct0/ntrail 走訪）。fixture t63-comment-attach.no（裸 match 臂首 doc+行尾 tail、明示 match ok 區塊臂內部 doc/tail/trail、err inline 臂 tail、wildcard 區塊臂、頂層 standalone-if 行尾、負形中段 standalone 屬其後陳述 Doc）與 oracle 逐位元組相同。門禁：fixture 63/64（僅余既有 t7-group-nil-float）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR（無一落於新碼區）。語料門（-P 2/90s 全 647）＝**SAME 451→464（+13）、0 回歸**，新匹配 tests/{fs-struct,json-get-clone,map,map-generics,match,option,std-new,strconv,test-div-zero,yaml}.no、mem-safety/{clone-reset-is-moved,if-branch-move-leak,move-clone-liveness}.no；**SUT-FAIL 6→4**——fmt-heuristic/str-order 因 ① 解掛升為 DIFF（結構仍分歧但可解析）。殘差：ws.no（11 行）、vec-push-leak.no（52 行）同簇檔被 D7 安全索引／`?T` 無值裸宣告 downstream 阻塞。⚠️ 已接受偏離（語料未證形）：match 區塊 `}` 前 standalone 於臂循環頭被吃掉且其後無臂時丟棄（Go 留 buffer 給外層區塊）；`->` 與區塊體 `{` 之間註釋仍為跳過；嵌套裸臂 body 的 armDoc 未併入。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s59（構建 sha1 7c403081，晉級 codesign 後 70be6e56），舊基線存 /tmp/noc-s0-s58.bak）→s60=470（**tagged-enum／option 變體臂析構綁定**——s59 掃描後 oracle 漂移（3c2fa429，重基線 463/181/3），首分歧分桶 LetStatement 61／BlockStatement 56／Identifier 9，選 Identifier 簇 9 檔＝`ok(v)`／`err(e)`／`some(v)`／`rect(w,h)` 變體臂 decomp。鏡像 Go 權威：expr.go:1021-1065 parse 期 `ok`／`err` + `( 裸識別符 )` 規範化為析構（condition 換 Identifier{ok|err}＋bindingName）；lowering.go:2049-2090 泛化改寫（主體為枚舉且呼叫臂全裸識別符參數、元數==欄位數 ⇒ bindingNames、condition 換 Identifier{X}）；枚舉登記表（decl.go:1196-1224＋858：payload 單欄位=欄型別、多欄位='enum.variant' 合成結構名、單元=''，裸枚舉 kind==1 亦登記 payload 恒 ''）；typeStr 規則（:2900-2902 枚舉⇒typeStr=armType 直取）；armType 表（載荷替換、單元=變體名、wildcard 補集=登記序−已列、' | ' join、空⇒死臂 skipItBinding）；namedItBinding 回退（:2919-2927 未知主體 ok(v) 具名無型別仍 armBinding）；多欄位提欄（:2307-2334 `a = it.f0` DotExpression 逆序 prepend⇒it;a;b;body）；varType 登記（:2276-2296 elemType→armBindingType→err且非枚舉='str'）。實作五批：① psst 新增 enN/evN/evP/evC/efE/efV/efT 平行陣列登記表＋六 helper（ps-enum-{row,has,payload,ncols,ftype,variants}，限定名剝落以 byte 尾掃回退）；② ps-parse-enum-def 每變體尾填表（ast.type-string）；③ ps-parse-match 型樣塊加 case A（ok(v)/err(e) 消費 4 token→Identifier）、case B（枚舉呼叫臂掃描→Identifier＋參數名收集）、裸變體臂 kvarE；④ bt 計算加 enum armType 補集塊、reg 登記、綁定名 bn plumbing（ps-mk-it-let-t armBinding 改 bt≠''∨具名）、多欄位提欄（新 builder ps-mk-ext-let）；⑤ ps-match-listed 擴 cN/cA 元數候補＋ok(v)/err(e) 形 fl 置位（Go 改寫後掃描之預掃近似，首臂盲區以 caseA 就地置位補）。fixture t64-tagged-enum-arms.no（已知 option ok/err/nil 析構、未知主體回退、單載荷/單元/雙變體補集、多欄位提欄、巢狀同名、裸枚舉變體臂）與 oracle byte-identical。門禁：fixture 64（僅余既有 t7-group-nil-float、與基線既有紅同形）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR（無一落於新碼區）。語料門（-P 2/90s 全 647）＝**SAME 463→470（+7）、0 回歸**（SUT-FAIL 3 檔 rc=142 同集、ORB-FAIL 0），新匹配 tests/{enum-annotation,option-match-silent-wrong,tagged-enum-{alias,cross-fn,payload-ownership,two-match}}.no＋**意外收復 view-basic.no**（s60 Step1 之 LOST 源於平行會污源檔、已自行修復）。殘差：err-binding（8 行）／match-arm-binding-scope（28 行）被**區塊級裸 match 臂續接合併**族阻塞（區塊臂體 `stmt; cond -> X; -> Y`：Go 將臂區合併為單一 if/else 且區塊 inline:true、noc 現拆為 standalone-if＋if(1) 包裹——下一候選增量）；tagged-enum-zero-match（5 行）殘 `_ = o10` discard 形（oracle 建 LetStatement{name: _, value}）。⚠️ 已接受偏離：枚舉全列死臂之 unreachable warnings 計數（Program{warnings:N}、s55 先例）；多行型樣 `X(\n a, b)` 不相鄰形不規範化。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s60（構建 sha1 daca5d8e，晉級 codesign 後 4cf6b9bd），舊基線存 /tmp/noc-s0-s59.bak（70be6e56）→s61=474（**區塊級裸 match 臂續接合併 + 明示臂體 inline**——s60 殘差族：err-binding（原 8 行）／match-arm-binding-scope（原 28 行）之 `cond -> { stmt; cond2 -> X` 換行 `-> Y }` 區塊臂體。鏡像 Go 權威雙機制：① 換行後續接（stmt.go:2168）——`ps-mk-standalone-if` 已存「換行後 `->` 併入前 if 的 Alternative」路徑，於明示臂 `{ }` 體解析期間置 `inBlk=1/inMatch=0`，使 `stmt; cond2 -> X` 後之裸 `-> Y` 併入前 if 而非另起 `if(1)` wrapper；② 臂體 inline 標記（expr.go:1263「Inline 語句形」）——新增 `ps-bm-block-shape`（鏡像 classifyBlockAtCurrent parser.go:704-945，掃描起點 p.pos+1 跳過 LBRACE 本身）判別 `{ }` 首非 trivia token 形：回 blockMatch(1) ⇔ Go 走臂體提取路 ⇒ dump `BlockStatement{inline: true}`（具名區塊臂 `b: {`、`_ ->`、INT/STR/CHAR 型樣臂、`cond ==`/`&&`/`||` 比較臂 Go:943 直接 blockMatch）；回 0 ⇔ 普通區塊路（結構欄 `name: TYPE\n`、`name: 字面值\n`、`name: ident(`、`name: ident{`、`name: -INT`、`name: .x`、`name: Enum.Var`、`name: <value><binop>`、`IDENT NEWLINE`、`IDENT ,`、`IDENT }`、`IDENT (`）。⚠️ `IDENT .` 首臂改用新 helper `ps-arrow-scan-line` 前視深度 0 `->`（鏡像 parser.go:912-942）——`it.v = 99` 無臂 ⇒ blockUnknown(0)，修回 view-optional.no 之誤標 inline。⚠️ `bshape` 每臂重置（防上臂 blockMatch 洩漏至下臂）。fixture t65-block-arm-continuation.no（具名區塊臂 `b: {`→inline、區塊內 `cond -> X`＋換行 `-> Y` 續接、嵌套 err(m) 不污染外層、補集 else 與 wildcard 尾臂共存）與 oracle byte-identical。門禁：fixture 全 SAME（僅余既有 t7-group-nil-float 基線紅、與 s61 零差分）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR（無一落於新碼區）。語料門（-P 2/90s 全 647）＝**SAME 470→474（+4）、0 回歸（LOST=0）**，新匹配 tests/err-binding.no、tests/match-arm-binding-scope.no、tests/mem-safety/{json-nested-match,json-parse-option}.no（SUT-FAIL 3 檔 rc=142 同基線集：aes-cbc/net/tls、ORB-FAIL 0）。殘差：tagged-enum-zero-match（3 行）殘 `_ = o10` discard 形（oracle 建 LetStatement{name: _, value}，與 s60 同因）；`b:`＋換行（無大括號）之具名裸塊嵌套 match（r1/r2 探針）屬另一更深族，本增量未觸。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s61（構建 sha1 31901dcb，晉級 codesign 後 7dfdc91a），舊基線存 /tmp/noc-s0-s60.bak（4cf6b9bd）→s62=481（**`_ = expr` 空白標識符捨棄陳述 ⇒ `LetStatement{name: _}`**——s61 殘留候選族（tagged-enum-zero-match `_ = o10`）擴至全語料 7 檔首分歧。鏡像 Go parseStatement stmt.go:49（case 含 UNDERSCORE）→ :149（peek==ASSIGN）→ :172 parseLetStatement——`_` 走與 IDENT 同一 let 路、name 存字面 `'_'`。此前 noc UNDERSCORE 非 IDENT、陳述分派無臂⇒落預設 expr-stmt⇒`_`/`=` 被逐步防呆丟棄、只剩右值成裸 `ExpressionStatement{RHS}`（正戳中 Go stmt.go:2075-2079 註解所述「`_` 必須是語句邊界否則 `_ = expr` 靜默退化」之 noc 對偶面）。**實作最小兩批**：① ps-parse-stmt 主分派新增臂 `tk.typ==UNDERSCORE && ps-peek-at(p,1)==ASSIGN -> { ps-bump _; ps-bump =; idx = ps-parse-let(t,p,'_') }`（裸 `_`（無 `=`）不落本臂、仍走原路＝零回歸，oracle 裸 `_`⇒`ExpressionStatement{nil}` 屬另族未觸）；② ps-parse-let 內 s54 option-算術合成塊加 `name != '_'` 守衛。**關鍵對稱性（探針 a3/a4/b1/b2/b3 釘死）**：`_` 上【字面量/slice/呼叫回傳】型別推斷照常觸發（`_ = true`→bool、`_ = [1,2,3]`→[]i64、首個 `_ = g()`→?str），惟【option-算術推斷（inferOptionArith）豁免】（`_ = zz*2`⇒無型別）——因 Go captureTargetAllowsOption 拒 blank 目標；ps-mark-dcl('_') 自然令後續 `_ =` 落入「已宣告」閘（`_ = g()` 二次出現不再蓋 ?str，與 oracle 一致）。fixture t66-blank-discard.no（ident/bool/slice/call/option-算術豁免/多次丟棄/緊接他陳述後 全覆蓋）與 oracle byte-identical。門禁：fixture 全 SAME（僅余既有 t7-group-nil-float 基線紅）、單元 util/tok/lx/ast 4/4、vet 10 既有 ERROR（無一落於新碼區 ~985/~2769）。語料門（-P 2/90s 全 647、oracle 漂移 634a0bfa）＝**SAME 474→481（+7）、0 回歸（LOST=0）**，新匹配 tests/{fs-error-final,fs-error-info,fs-open-err,option-copy,uninit-output,tagged-enum-zero-match}.no、tests/mem-safety/option-heap-leak.no（SUT-FAIL 3 檔 rc=142 同基線集：aes-cbc/net/tls、ORB-FAIL 0）。殘留：裸 `_`（無 `=`）⇒`ExpressionStatement{nil}` 另族；`found = true` 於 match 臂體內裸 ident 賦值誤析為 `ExpressionStatement{found}`（collection/* 5 檔首分歧、涉臂體×賦值路由、回歸面較大、暫緩）。晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s62（構建 sha1 fb51a0d4，晉級 codesign 後 c6095b8e），舊基線存 /tmp/noc-s0-s61.bak（0e1fa13c）⚠️ 其余 6 档（json/net-{http2,pool,sse,ws}/str-vec-push）结构已正确、但仍于**下游 standalone-if desugar / index-out desugar / 裸 match 区块**再分歧（即下一个主簇，也即 LetStatement/IntegerLiteral/BlockStatement 三大桶）。✅ **AnnotationStatement 主鑰匙已打通**（self-dot 揭示者）：語句級 `#{...}` 現依「緊後首 token ∈ {IDENT,IN,RARROW}」分流——可附著→寫 overflow 槽（t37）、不可附著→發 `AnnotationStatement{entries:去重鍵名數}` 節點（t41）。✅ **bang/cond-loop ForStatement 簇已打通**（AnnotationStatement 揭示之下游主簇）：`!! { }`/`! { }` 僅當 bang【緊接】LBRACE 才成立（否則退回 `!x` 取反運算式陳述）；`(cond) { }` 需平衡 `)` 後【立即】是 `{` 且括號內非參數列（含 `,` 或「恰好兩 IDENT」⇒ 匿名函式字面量，退回 `(expr)` 運算式陳述）。三者皆 `ForStatement{label:''}` + `Condition`（BooleanLiteral/expr）+ 6 guarded CHILD 以 0 佔位，且【不】saveWarning（新式語法）。✅ **字面量型別推斷補齊**（Go stmt.go:1227–1268 / 1274–1300）：推斷表既有 INT→i64/FLOAT→f64/STRING→str/BOOL→bool，本增量補上 CHAR→char 與 hex(0x)→byte——雙函數（`ps-infer-type`、`ps-elem-type-of`）同口徑修正。✅ **無值明示型別裸宣告已打通**（Go parseLetStatement no-value 分支 stmt.go:1051–1060）：陳述位 `name TYPE`（TYPE∈{NamedType, SliceType, ArrayType, NullableType}）型別解析完後若緊接【非】`=` 而是陳述邊界（NEWLINE/RBRACE/EOF/;/行內註解）⇒ 建 `LetStatement{name, type, value: }`（Value guarded CHILD 以 0 佔位）。復用既有 `ps-parse-type`（子先於父）；非裸宣告（有 `=`、或索引 `x[i]`）則還原 p.pos——所建孤兒節點不被任何 kid 引用、dumper 自 root 沿 kid 走訪永不觸及（ast.dump 語義）⇒ 零回歸。✅ **複合型別名已打通**（接續 s40 揭示之同族）：`ps-parse-type` 先前只认單 IDENT 型別名，本增量補齊——（a）限定名 `a.b.c`：IDENT 臂建完 NamedType 前以迴圈續吃 `DOT IDENT`，整條字串存進 name 槽（type-string 直接讀 s0，不另建節點，對齊 Go stmt.go:1034）；（b）巢狀元素：SliceType/ArrayType 臂不再硬性要求元素為單 IDENT，改為【遞迴】 `ps-parse-type`（元素起 ∈ {IDENT, LBRACKET, QUESTION}），故 `[][]i64` 建為 SliceType{SliceType{NamedType{i64}}}、`?fs.file` 建為 NullableType{NamedType{fs.file}}（type-string ast.no:610/630 遞迴渲染）。各臂改用 `handled` 旗標防 match 非排他之跨臂重評。級聯效益：`ps-parse-param`（`w io.writer`）、可為空含值臂（`f ?fs.file = call`）無需改動即自動受益；另把 `ps-parse-typedecl-noval` 升級為通用 `ps-parse-typedecl`（新增 `= value` 分支）以接住限定/巢狀【含值】形（如 `d tls.conn = call-tls()`）。⚠️ **未涵蓋**：`?T`【無值】裸宣告（如 `x1 ?str`）仍由可為空臂（需 `=`）誤吞——為避免觸碰众多 option-* 檔的回歸風險，留待下一個專門增量（需把可為空臂改為「先解型別再判斷邊界 vs `=`」）。✅ **無色 async 前綴已打通**（接續 RunExpression/CoExpression 揭示之 async 簇）：Go expr.go:305–357——`run e`/`co e`/`awy e` 皆為前綴運算子包一層guarded-CHILD 節點（子先於父）。`run`/`co` 需歧義消解：若 `peek-at(1)==DOT` 則不是 async 關鍵字而是模組名（`run.list(...)`）⇒建普通 Identifier 停於 DOT 交後綴迴圈；`awy` 無此歧義。三節點（K_RUN_EXPRESSION=57 / K_CO_EXPRESSION=20 / K_AWAIT_EXPRESSION=13）asp.no 早已註冊⇒纯 parser 缺臂。⚠️ 用預先算好的 `dotAfter` 旗標防 match 非排他。⚠️ **下一個主簇**：heap.no/path.no/zlib.no 等仍於 **裸 match 區塊 `{ cond -> body … }` desugar**（首分歧位於 LetStatement/IntegerLiteral/BlockStatement 三大桶，oracle 包一層 `IfExpression{IntegerLiteral{value:1}(synthetic), BlockStatement{各臂}}`）再分歧；另有 `GenericParams`（泛型參數渲染）。⚠️ USE 增量清掉 11 個 use-bucket 檔案的首分歧，但它們随即在**下游 match-block/standalone-if/BlockStatement 簇**（已确認為下一個高價值增量）或 **FloatLiteral value 精度**上再分歧，故僅 sha3.no 逹到全匹配——USE 本身結構正確（t38 byte-identical）。⚠️ `tests/str-order.no` 於 s31/s32 **皆挂死**（既有 hang，非本增量回歸）。單元證據：`ast_test`、`tok_test`、`lx_test`、`util_test` 皆 PASS；`no vet compiler/src` ERROR **0**（硬門檻滿足）、WARNING **0**（既有 `overflow-ineffective` 已由當前 bin/no 降為 HINT，見下方 vet 現況）。覆蓋進度見 §6.3（t0–t51） |
| **M3** | 語義：37 個 `Validate*` + `stdsig` 表（§5.4）                                           | 🟡 **M3-a 骨架落地（s9a，2026-10-27，§5.3.4.am/§6.4）** | L3 誠實基線：`noc vet` 已可用但無診斷源 ⇒ harness 報 **G8 MASS-NOOP**（非 PASS） |
| **M4** | 中端 + 代碼生成：HIR / MIR + RC 堆策略 + LLVM IR 文本                                       | ⬜ 未開始   | —                                                                                      |
| **M5** | 驅動層：`build` / `run` / `vet` 的 CLI 與 clang 調用                                     | ⬜ 未開始   | —                                                                                      |
| **M6** | 定點：S0→S1→S2→S3 + 三路交叉編譯（§3.1 / §3.2）                                            | ⬜ 未開始   | —                                                                                      |
| **M7** | 精確所有權分析（`insertDrops` 那一整套）                                                       | ⬜ 未開始   | —                                                                                      |
| **M8** | 後置：`fmt` / `lsp` / `tools`（不阻塞定點）                                                | ⬜ 未開始   | —                                                                                      |

**M2 收官（2026-10-27，s8y `261ab91c` 落地後驗收）**：

- **判據**（§7.1 L2）：全語料 `no ast` dump 逐位元組一致。
- **實測**：語料 `tests src/std`（**655 檔**，含 mem-safety/golden 子目錄）以 `/tmp/noc-s81`（s8y 後代碼現建）對 `./bin/no`（當代 HEAD）串行掃描 ⇒ **DIFF 清單為空，655/655 SAME、0 回歸**。抽驗防橡皮圖章：`bigint.no`（175 KB dump）、`database/sql.no`、`tests/2.no` 三檔 `(rc, sha256(stdout))` 兩側一致且 SUT 輸出非空。
- **L0 全綠**：`no run compiler/tests/{util,tok,lx,ast,ps}-test.no` ⇒ **27/62/45/48/38 = 220 斷言 0 失敗**。⚠️ 其中 ast-test 的 `test-unsupported-is-live` 哨兵本輪遷移：s8y 把 `interfaceMethod` helper 接線後，舊哨兵（dump `InterfaceDefinition` 必計 1 unsup）失效——改用仍未接線的 `annotationStructField`（`AnnotationStructValue` kind 9 tape 帶 `OP-HELPER`）作缺線計數感測器，`ast.unsupported` 機制的「非假斷言」證明得以延續。
- **vet 門檻**：`no vet compiler/src` ⇒ 19 ERROR（既有基線，無一新增）。
- **收斂軌跡**（2026-10-27 當日連續增量）：s8q（pipeline 同位差）→s8r（module-path 接收者回傳型）→s8s（`[?]T` 回傳參數）→s8t（死碼臂 warnings＋裸可空宣告 Doc）→s8u（MapLiteral 複合標註）→s8v（let-RHS 推斷家族）→s8w（鏈式裸 if 尾隨 else＋切片參數補登記）→s8x（多重指派逗號後目標專用解析器，`tests/2.no`）→**s8y（裸型名陳述＋運算符重載剥 `?`＋介面繼承清單，`bigint.no`＋`database/sql.no`）⇒ DIFF=0**。逐輪記錄見 §5.3.4.ac–ak。
- **晉級**：`./bin/no build -cc zig -o /tmp/noc-s81 compiler`（含 ast.no 收官註釋修訂，位元組级行為與 noc-s80 同源）。

### 6.1 M1 的實作要點（與 §5.2 互補）

- **`lx.no` 是 `src/lexer/lexer.go::scanToken()` 的逐分支移植**；公開入口是 **`dump(src str) (out str)`**（`noc` 未來的 `tokens` 子命令與 L1 的 oracle 都走同一個包／同一份判準）。`dump` 的輸出**刻意不帶尾端換行**（`print` 會自己加一個）。
- **SHS 合規**：無一等函式值、無指針、無 `#{c}` FFI、無模組級可變 `let`；所有可變狀態顯式傳遞（`lx lxst` 結構體，§5.1.4）。
- ⚠️ **本檔踩過、並寫進檔頭註釋的三個坑**（完整表見 §2.1.1）：
  1. **`;` 是行註解**，**不能**當敘述分隔符（初版因此產生 33 個「未閉合塊」）；
  2. **`x -> { … }` 是 match 臂**（最多執行一次），**不是** while 迴圈 —— 迴圈要寫 `(cond) { … }` 或 `{ … } (cond)`；
  3. **跨模組常量在間接依賴下靜默為 0**，作運算式運算元時**硬編譯失敗**（§2.2.8）。
- **loop 重寫**：初版把 15 處 `go -> { … }` 當迴圈用 ⇒ **每個掃描迴圈都只跑一次**（SUT 對任何輸入只吐 1 個 token）。全部改寫為前綴 while 形式後才對。
- **regex flags 迴圈**曾**無限迴圈**（`tests/regex-literal.no` 逾時）：原因是 `hit` 同時當「迴圈條件」與「迴圈內重置」，遇到非字母時迴圈體既不前進也不改條件 ⇒ 改為在迴圈**體內**設 `stop` 旗標（對應 Go 的 `else { break }`）。
- **讀檔必須逐位元組無損**：`drv.cmd-tokens` 用 `fs.read-bytes` + `to-bytes-str()`，**不是** `fs.read-str`（§2.2.9）；否則所有非 ASCII 註解被二次編碼，且 **`rc` 測不出來**。

### 6.2 M2 的進度與**前置**（截至 2026-10-11）

**已完成的前置**（屬 M1 的詞法 API，但只為 M2 而做）：

- **`lx.scan-all(src) (r []tk)`**：把整份原始碼掃成結構化 token 陣列（含結尾 `EOF`）。
  動機：M1 時 `lx-scan-token` 是「掃描 + 渲染成字串」，只有 `dump` 一個消費者。
  若為解析器另寫一份掃描，兩份必然漂移，而 **L1 只保護其中一份**。⇒ 重構為
  **掃描只填 `tk` 結構**；`dump`（經 `lx-fmt`）與 `scan-all` 都是它的下游。
  重構後 **L1 675/675 不變**——這就是「重構沒改行為」的證據。
- **`compiler/tests/lx-test.no`（45 斷言）**：其中 `test-no-alias` 專門釘住
  「`push` 是**深拷貝**」。若 `[]tk` 的 push 退化成別名，`scan-all` 會全錯而
  `dump` 全對 ⇒ **L1 照不到**。

**進度（2026-10-11 更新）**：`ast.no`（節點 + arena，1423 行）與 `ps.no`（解析器，2859 行）**已開工並持續增量推進**（見 §6.3）；`ty.no`（型別）、`ann.no`（註解）尚未開始。

**開工前必讀 §5.3.1**：該節現在是**格式契約**（`noc` 必須逐位元組復刻的那一份），不再是「盲點警告」。

**原「已知的驗收缺口」——兩項均已關閉（2026-10-10）**：

| # | 缺口 | 處置 | 證據 |
| --- | --- | --- | --- |
| 1 | `no ast` **不印註解**（P0-③） | ✅ 新增 **`-annotations`**，依樹序點查 side-table（**絕不 range** 那張 map） | 語料實測 **287 條 / 88 檔**；`TestAnnotationsFlagIsWired`、`TestNoAnnotationsByDefault` |
| 2 | **節點覆蓋不全**（9 種塌成 `<unknown>` + 4 類只印殼） | ✅ `dumpNode` 由 40 → **71** 個 case；所有 `Node` 欄位/切片元素都遞迴 | 語料 `<unknown>` **319 → 0**；`TestCorpusHasNoUnknownNodes`（已證偽：刪一個 case ⇒ 24 檔 FAIL） |

✅ **已關閉（2026-10-10）**：`noc` 已實作 `ast` 子命令 ⇒ **L2 從 UNAVAILABLE 轉為可執行**，且 `--sut /tmp/noc-s0` 全語料 **30/30 SAME / 0 DIFF**（見 §6 表 M2 行與 §6.3）。

**節點盤點與錯誤訊息目錄**：見 [`nolang-ast-grammar.md`](nolang-ast-grammar.md)（由 `scripts/ast_inventory.py` 從 Go 原始碼機械抽取；含 71 個節點的欄位／建構函式，以及 **168 個錯誤訊息站點**——這是 M2/M3 要逐字重現的清單）。

### 6.3 M2 解析器覆蓋進度（逐增量，截至 2026-10-11）

`ps.no` 已支援的句法（每項都有 byte-identical fixture 釘住）：

| 增量 | 內容 | fixture |
| --- | --- | --- |
| 基礎 | Program / ExpressionStatement / IntegerLiteral / Identifier / 行尾無換行 | `t0`、`t1` |
| let 語句 | `x = expr`（含**僅字面量才合成** NamedType 推斷，見下） | `t2` |
| 函式定義 | `name = (params) { body }`（現經 `ps-is-func` 判定） | `t3` |
| 中綴優先級 | `ps-op-prec` 逐值復刻 Go `precedences` 表（含 quirks：`SHL/SHR/AND`→PRODUCT、`OR/XOR`→SUM）；左結合用嚴格 `>` | `t4`（`1 + 2 * 3`）、`t8`（跨行 `1 +\n2`） |
| 前綴 | `-` / `!`（`-3` **不摺疊**，仍是 PrefixExpression） | `t5` |
| 顯式型別標註 | `w i64 = 5`（NamedType **不帶** inferred） | `t6` |
| 括號 / nil / float | GroupedExpression、NilLiteral、FloatLiteral（value 走 `to-f64().to-str()` 對齊 Go `%v`，raw 保留原文） | `t7` |
| **呼叫運算式** | `f(a, b, …)` ⇒ `CallExpression{fn, args}`，slot0=args 字串渲染 (NON-ZERO)，count slot0=迭代次數；Function kid + 每個 Argument kid | `t9`（`f(1, 2)`） |
| **成員運算式** | `a.b` ⇒ `DotExpression{receiver, property}`，slot0=property 名（OF-FIELD），一個 kid=Receiver | `t10`（`a.b`）、`t11`（`y = a.b(1)` ⇒ Call 包 Dot） |
| **if / if-else** | `if cond { … } [else { … }]` ⇒ `IfExpression` **三 kid**（Condition/Consequence/Alternative），缺 alt 也要 push 0 占位；Header 為裸 `IfExpression`（無 `{}`） | `t12`（`if true { x = 1 } else { y = 2 }`）、`t13`（函式體內 `if r == 1 { … }`） |
| **Program.warnings 計數** | `psst` 新增 `warn i64` 欄位；`ps-parse-if` 每遇到一個 `if` 就 `p.warn = p.warn + 1`（與 Go `expr.go::parseIfExpression` 中 `saveWarning` 同構）；`parse` 將 `p.warn.to-str()` 寫入 slot0，使得 `Program{warnings: N}` 能如實呈現 | `t12`、`t13` |
| **切片字面量** | `[e1, e2, …]` ⇒ `SliceLiteral{elements: N}`，slot0=元素數渲染 (NON-ZERO)、count slot0=迭代次數、每個元素一個 kid；`ps-parse-let` 偵測到 SliceLiteral RHS 時合成 `SliceType{inferred}`（包一層 `NamedType{elem, inferred}`），元素型別由首個元素 kind 推斷（INT→i64/FLOAT→f64/STRING→str/BOOL→bool/空或巢狀→預設 i64），復刻 Go `stmt.go` 的推断路徑 | `t14`（`[1,2,3]` / `['a','b']` / `[]`） |
| **索引運算式** | `left[index]` ⇒ `IndexExpression{Left, Index}`（裸標頭無 `{}`、兩個 kid、**無槽位**）；在後綴迴圈中與呼叫/成員一同分派（一律绑定、不比較 prec）；index 用 LOWEST(0) 優先級解析。鏈式 `m[0][1]`（左嵌套）、`f(x)[2]`（Call 被索引）皆自然由後綴迴圈迭代。舆 `a[1..3]` 範圍切片（SliceExpression）**共用同一 LBRACKET 臂**，由 `..` 分流（見下行）。 | `t15`（`a[0]` / `a[i+1]` / `m[0][1]` / `f(x)[2]`） |
| **定長陣列** | `name [N]T = [e1, …, eN]` ⇒ `LetStatement{type: ArrayType, value: ArrayLiteral}`。`ArrayType{size: N}`：slot0=inferred（明示→'false'不印）、child0=Size、child1=Elem（NamedType）；`ArrayLiteral{size, elements, wasSliceLiteral: true}`：slot0=elements 字串(OF-LEN)、slot1='true'(OF-FIELD bool)、count0=元素數(KIDS 迭代)、kid0=Size（沿用型別側 sz 節點）+ 元素 kids。`size` 欄位是 OF-NODE-STRING（由 child 推得、**不占字串槽**）。復刻 Go `stmt.go:1210-1218` 的 SliceLiteral→ArrayLiteral 轉換（`WasSliceLiteral=true`）。分派守衛 `+3==RBRACKET && +5==ASSIGN` 专认 `] IDENT =`，自然排除 `a[0] = x`（那是 `] =`） | `t16`（`arr1 [3]i64 = [1,2,3]` / `arr2 [2]i64 = [10,20]`） |
| **範圍切片** | `a[lo..hi]` ⇒ `SliceExpression{Left, Range}`（裸標頭、兩 kid：Left、RangeExpression）；`RangeExpression{start, end, leftInc, rightInc}`：**固定兩 kid [Start, End]**，缺席端以 0 占位（`node-string(0)='<nil>'` ⇒ 標頭印 `start: <nil>`，guarded CHILD 跳過不印）；slot0=leftInc（開 `[`→true）、slot1=rightInc（閉 `]`→true / `)`→false）；start/end 皆 OF-NODE-STRING不占槽。與 IndexExpression **共用後綴 LBRACKET 臂**，靠 `hasStart` 快照 + `..`(ELLIPSIS) 判定分流。覆盤 Go `parseSliceExpression`；`a[1..3)`、`a[..3]`、`a[1..]`、`a[..]` 全同 | `t17`（五種範圍形） |
| **Map 字面量** | `name [K]V = { k1: v1, … }` ⇒ `LetStatement{type: MapType, value: MapLiteral}`。`MapType{}`（裸空標頭，inferred=false 不印）：slot0=inferred、kid0=Key(NamedType)、kid1=Value(NamedType)；`MapLiteral{pairs: N}`：slot0=pairs 字串(OF-LEN)、count0=KIDS 迭代數、**kid 序與 ArrayLiteral 相反**——dump 源碼序是先 `for Pairs`（KIDS）後 `node(MapType)`（CHILD），spec_gen 保留源碼序 ⇒ kid0..N-1=MapPair、**最末 kid=MapType**；`MapPair`（裸標頭）：kid0=Key、kid1=Value（皆 guarded CHILD）。`[K]V` 舆 `[N]T` 共用 `name [` 前綴，靠 +2 令牌型別分流（INT→陣列 / IDENT→map）。K/V 為單 IDENT 型名形（`[str]i64`）；巢狀值型 `[str][]i64`  deferred。⚠️ **Nolang 字面量引號反转**：`"a"`=CharLiteral（僅單字元）、`'text'`=StringLiteral；map key 可為 char / identifier / int，皆走 ps-parse-expr-prec | `t18`（`{"a":1,"b":2}` / `{}` / `{k1:10,k2:20}`） |
| **結構體字面量** | `Ident{ field: val, … }` ⇒ `StructLiteral{type, fields}`（type 是**字串**型别名、非子節點）。slot0=型别名、slot1=fields 數(OF-LEN)、count0=KIDS 迭代數；kid 序=各 StructField（僅 KIDS、無 CHILD）。`StructField{}`：slot0=name、slot1=arraySize('0'→kvn NON-ZERO 略過；未設會印空 `arraySize: `!)、slice/readonly/sealed 留''(P-TRUE 略過)；kid0=Type(0 占位 guarded)、kid1=Value（皆 CHILD）。後綴 LBRACE 臂新增，由 `ps-is-struct-lit`（跳 trivia 後首非 trivia 為 `IDENT COLON` 或空 `{}`）守衛，避免误捕 match（`->`）/enum（`,`/`=`）；型别名由 `ps-dotted-name` 重組：Identifier→slot0 名（`Point`）、DotExpression→`ps-dotted-name(kid0 Receiver) + '.' + slot0 property` 递归（`net.Conn` / `a.b.C` / `pkg.Sub.K`）、其他回退 node-string。空 `{}` / 巢狀 `Outer{inner: Point{x:1}}` / char值 `Box{v:"a"}` 皆同 | `t19`（`Point{x:1,y:2}` / `Empty{}` / `Box{v:"a"}` / `Outer{inner:Point{x:1}}`） |
| **明示切片型別標註** | `name []T = [e1, …]` ⇒ `LetStatement{type: SliceType, value: SliceLiteral}`。`SliceType{}`（裸空標頭、inferred=false 不印）：slot0=inferred、kid0=Elem（NamedType）；RHS `[..]` 經 primary 自然建 SliceLiteral（複用 t14 路徑，含空 `[]`→elements 0 不印）。舆 `[N]T`（+2 INT）/`[K]V`（+2 IDENT）**共用 `name [` 前綴**，靠 +2 令牌分流——此臂要求 +2 為 RBRACKET（空括 `[]`）、+3 IDENT elem、+4 ASSIGN。元素引號反轉同 map（`"a"`=CharLiteral）。巢狀 `[][]T` / `[]net.Conn` deferred | `t20`（`[]i64=[1,2]` / `[]str=["a","b"]` / `[]i64=[]`） |
| **限定名 struct literal** | `net.Conn{…}` / `a.b.C{…}` 等**點分限定型名**的 StructLiteral：型别名不再是 kind-name `DotExpression`而是重組後的點分字串。新增 `ps-dotted-name(t, node)` 递归：`Identifier`→slot0名、`DotExpression`→`dotted(kid0 Receiver) + '.' + slot0 property`（slot0=property、kid0=Receiver，同後綴 DOT 臂的建法）。LBRACE 臂由 `node-string(idx)` 改调 `ps-dotted-name(idx)`；非 struct 的 dot 链（`a.b.f()` / `x.y.z`）不受影响 | `t21`（`net.Conn{host:"x"}` / `a.b.C{x:1}` / `net.Conn{}` / `pkg.Sub.K{x:1,y:2}`） |
| **函式字面量（带型別參數）修正** | 復刻 `d.signature` 的子節點深度與 Parameter 的 kid 區。`name = (a T, b T) { body }` ⇒ `FunctionDefinition{name}` 下依序挂 `Parameter`（各带 `NamedType`）+ `BlockStatement`。**兩個真实 bug**：（一）`ast.no` OP-SIG 把 `signature` 误以 `depth`（非 `depth+1`）呼叫 ⇒ 0 參數不可見、≥1 參數時 `Parameter` 塌成 Program 同層兄胏（修正：dump.go FunctionDefinition:281 / FunctionLiteral:409 皆 `d.signature(sig, depth+1)`，ast.no 改 `depth+1`）；（二）`ps-parse-param` 只 push Type 一個 kid，但 Parameter spec 有 **2 個 guarded CHILD**（Type、DefaultExpr），dumper 位置式讀 kid0/kid0+1 ⇒ kid0+1 读到下一個全域 kid（body 的 Identifier）⇒ 多一个幽靈子節點（修正：恒 push 兩槽，Type 缺席用 0、DefaultExpr 恒 0） | `t22`（`(x i64){…}` / `(a i64,b i64){…}` / `(){…}`） |
| **match 明示形 desugar** | `subj: { pat -> body … }` ⇒ **parse 期** desugar 成 if/else 鏈（⚠️ **2026-10-19 分歧記錄**：此為 **noc 現態**；bin/no 的 parser 現已【保留 `SurfaceMatch`】不展開——S1 收官（翻轉 `b918dba9`、刪舊降糖路徑 `d6965622`，詳見 `nolang-parser-lowering-invariants.md` §22），故 `DumpWithOptions` 對 bin/no 會印出 `SurfaceMatch{arms: N}` 而非 IfExpression 鏈。本分歧使 L2 oracle 在含 match 的語料上偏離（實測 264/653 檔），裁定與收尾見 §5.3.3）。規則：①非 wildcard 臂 `pat` ⇒ `IfExpression{cond: InfixExpression{==}[Ident subj, pat], consequence: 臂區塊, alternative}`；②**每個臂區塊先塞合成 `LetStatement{name: it, value: subj, synthetic: true}`**（slot0='it'、slot1='true'、kid[0]=Type 缺席以 0 佔位、kid[1]=Ident）；③運算式臂 ⇒ `BlockStatement{inline: true}[it, ExpressionStatement{e}]`、區塊臂 ⇒ `BlockStatement{}[it, stmts…]`（inline 未設）；④相鄰非 wildcard 臂 ⇒ 後臂包成非內聯 `BlockStatement{}[ExpressionStatement{IfExpression}]` 作 alternative；末臂無 else 時 alt=0；⑤wildcard `_`（`tok.UNDERSCORE`=88，**非** IDENT）⇒ 其臂區塊**直接**作前一 if 的 alternative；若最外層只是 wildcard ⇒ 包成 `IfExpression{IntegerLiteral{value:1}（ps-mk-true，無 raw）, blk, 0}`。**範圍**：識別元主體 + 字面量/識別元/wildcard 型樣 + 運算式/區塊臂；**延後**守衛、option/enum 完整型樣、複雜主體（呼叫/索引）。**坑**：相鄰兩條 `cond -> {blk}` 陳述若條件用**活的** `ps-peek(p)` 會在跨臂迭代時**随位置重評**誤觸（同 §2.1.1 LBRACE 臂教訓）⇒ 改快照 `at = ps-peek(p)` 後 `{ at == UNDERSCORE -> {…}  at != UNDERSCORE -> {…} }` 互斥雙臂。另：區塊臂（`{ y = 5 }`）在 let-RHS（CTX_EXPR）為 oracle parse error「must end with expression」，只准在**陳述層** match 出現 | `t23`（`n:{1->10}` / `k=n:{1->10;2->20}` / `w=n:{1->10;_->0}` / `b=n:{true->1}` / 陳述層區塊臂） |

| **多重指派（MultiAssignStatement）** | `a, b, … = expr` ⇒ `MultiAssignStatement{targets: N}`（RHS 常為多返回值呼叫）。slot0=targets 數（kvc NON-ZERO，未設會印空 `targets: `）、overflow slot1 留''（kvs 空略過）；**kid 序依 dump 源碼序**：先 N 個 targets（KIDS、`set-count(0,N)`）、後 1 個 value（guarded CHILD，缺席以 0 佔位）。**父後建**：先把所有 target 與 value 子樹解析完，再 `add(parent)`、依序 `kid-add`（共享 kids 游標）。分派守衛在 `ps-parse-stmt` 的 IDENT 分支：`nxt_typ == tok.COMMA`（首 token 為**裸識別元**且緊接逗號）；因 `ps-op-prec(COMMA)=0`，`ps-parse-expr-prec(t,p,0)` 自然在逗號處停下、不會把 `a, b` 吞成單一運算式。**範圍**：僅**識別元目標**（`a, b = f()`、`m, ok = fs.read()` 含點分方法呼叫 RHS）；**延後**索引/點分作目標（`x[0], y = f()`，首 token 非裸 IDENT、gate 漏判）與巢狀 tuple（`a, b = 1, 2` 的 `, 2` 依 oracle 現況本就被拆成獨立 ExpressionStatement，noc 復刻一致）。**坑（見 §2.1.1）**：match 去糖成的 if/else 鏈把各臂區塊的區域變數**提升到同一函式作用域**——若新臂用了兄弟臂已佔用的變數名（如 `tn` 在「明示型別標註」臂是 `tt.lit`=**str**，新臂卻當 **i64** 用），型別 checker/codegen 會合併成 str ⇒ `t0 > 0` 變成「字串做 gt 比較」、`push` 變成「str 塞進 []i64」的 MIR codegen 錯。修法：新臂所有區域變數加唯一前綴（`maTargets/maT0/maTn/maVal/maK`） | `t24`（`a, b = foo()` / `p, q, r = bar(1)` / `x, y = get-pair()` / `m, ok = fs.read()`） |

| **可為空型別（NullableType）** | `?T` 型別標註 ⇒ `NullableType`（**裸標頭無任何欄位**（連 IsInferred 也不印）、**1 個 guarded CHILD=Type**）。新增共用 `ps-parse-type(t, p)` 遞迴型別解析器（子先於父）：`IDENT`→NamedType（明示→不設 inferred）、`? T`→NullableType[內層]、`[] IDENT`→SliceType[NamedType]（elem 僅單 IDENT 形）。接線兩處：①`ps-parse-stmt` IDENT 分支新增 `nxt_typ == tok.QUESTION -> { name; ps-parse-type; expect ASSIGN; expr }` 臂⇒ `LetStatement{type: NullableType, value}`（`IDENT ?` 於陳述層只可能是可為空標註——傳導 `?=` 是 `tok.QUESTION_ASSIGN`(77) 不同 token）；②`ps-parse-param` 型別段改用 `ps-parse-type`（原只認裸 IDENT），使輸入參數 `(x ?i64)` 正確建 Parameter{type:NullableType}（否則 `?` 不被消費→防呆 bump→`i64` 被當第二個參數→幽靈節點，部分形還可能掛死）。dump 端 ast.no 早已有 `K_NULLABLE_TYPE -> '?' - type-string(kid0)`（ast.no:630），故本片**纯 parser 側**。⚠️ **重大發現**：NullableType 在真實語料幾乎**只出現在回傳元組** `f = (params) (out ?T) { }`，而當時 `ps-is-func` 只認 `(...) {`——遇到 `(...) (...) {` 直接不判為函式、整段 misparse。**該回傳元組缺口已由下一片（t26）補足**，本片先釘住可單獨驗證的 `?T` let 標註 + nullable 輸入參數 | `t25`（`a ?i64 = 1` / `b ?[]i64 = [1,2]` / `c ?str = 'x'` / `f = (x ?i64) { y = 1 }`） |

| **回傳元組函式定義（FunctionDefinition results）** | `name = (params) (results) { body }` ⇒ `FunctionDefinition`，**回傳值也建為 Parameter**、於 `signature()` 接在輸入參數之後。Nolang 無獨立 return 節點：Go `d.signature` 先 `for Parameters` 再 `for Results` 平鋪渲染 ⇒ arena kid 區序為 `[generics…][inputs…][results…]` 連續，再掛 `body`（guarded CHILD）。做法：`set-sig(t, idx, 0, inputs.len(), results.len())`（c3=泛型/c4=輸入/c5=回傳，ast.no:235、signature() 由 base 依此三數切片，ast.no:1155）。①**`ps-is-func`** 原只在首群組閉合後看 `nt`：`{`⇒函式、否則 bail ⇒ 遇到 `(...) (` 直接判非函式。改成三分岔快照臂：`nt==LBRACE`→r=1 停、`nt==LPAREN`→**不中斷**（go 保持 true，續掃下一群組直到它閉合）、其他→停（r=0）。②**`ps-parse-func`** 於消費首 `(...)` 後加可選第二群組：`ps-peek(p)==LPAREN` 則再跑一次參數迴圈收进 `outs`，最後 `set-sig(0, params.len(), outs.len())` 並依序 kid-add inputs→outs→body。**父後建**：所有 param（含 NullableType/SliceType 內層）與 body 子樹先建完再 `add(FD)`。坑沿用：`nt` 用快照非活 `ps-peek`、臂内不復用兄弟臂變數名（`outs`/`ro`/`m` 皆新名）。`ps-is-func` 泛化後 `c = add(1)(2)`（鏈式呼叫）仍正確判非函式（第二群組閉合後 `nt` 非 `{`）。**延後**：泛型參數 `[T]`（GenericParams，需 `set-sig` c3>0 + 另一條 `ps-is-func` 前置掃描）、variadic、colon-syntax 方法定義 | `t26`（`(key str) (val i64, ok bool)` / `() (out ?i64)` / `(a i64, b i64, c []i64) (total i64)`） |

| **兵列迭代（ForStatement + IterationExpr）** | bare range-for：`var <- range-or-array [:] { body }`（**不使用 `for` 關鍵字**、crc-64 等 std 主流形）。新增 `ps-parse-for(t, p, vname)`，分派守衛在 `ps-parse-stmt` IDENT 分支 `nxt_typ == tok.ARROW`（`IDENT <-` 於述層即迭代）。**兩個節點层位**：`ForStatement{label: }`（slot0=label、`kv` 恆印；condWrapper/overflow 預設不印）有 **6 個 guarded CHILD**（dump 源碼序：Init,Condition,Update,CountExpr,IterRange,Body），迭代形仅後两者非零 ⇒ kid-add `0,0,0,0,ie,body`；`IterationExpr{var: i}`（slot0=var 名、`kvs`）有 **2 個 guarded CHILD**（Range、RangeExpr）——区间形填 kid0（RangeExpression）、陣列形填 kid1（運算式），另一方以 0 占位。区间解析复用 t17 的 RangeExpression 建法（leftInc=開 `[`→true/`(`→false、rightInc=閉 `]`→true/`)`→false、两端可缺）。⚠️ **两个坑**：（一）**冒號决定警告**——Go parseForStatement 在区间后：有 `:` 則消费（無警告）、無 `:` 則 `saveWarning`（deprecated）。故 `p.warn = p.warn + 1` 需同步（同 ps-parse-if），否则 Program 头 `warnings: N` 对不上；冒號检查用快照 `hd = ps-peek(p)`（活 `ps-peek` 在 ps-bump 消费 `:` 后会被下一臂重评⇒跨臂误加警告）。（二）**陣列裸 IDENT 源与明示-match 相撞**——ps-parse-expr 后缀有 `IDENT COLON LBRACE`⇒match 臂（ps.no:833），`i <- a: { }` 的裸 ident 源会被该臂吃掉 `: {body}` ⇒SIGBUS。修法：非区间源若 `IDENT` 且 `+1==COLON` 则**直接建 Identifier 节点**（不走 ps-parse-expr）；索引/呼叫形（`i <- a[0]:`、idx 非裸 IDENT）不触发该臂、仍走 ps-parse-expr。**未验证但已知剩余缺口**：本增量解锁 crc-64.no 的 range-for（不再 SIGBUS），但该文件仍因 **`Doc:` 行註解**（noc 不发 comments）与 **u64 大常量的带符号值**（`18446744073709551615`→应 `-1`、noc 误印 `20`）而 DIFF——**u64 常量部分已由 t28 `ps-int-lit-value` 解決**、仅剩 `Doc:` 行註解為獨立增量。**延后**：labelled `outer i <- r:`、无大括號 inline 本体、string 遍历（`i <- 'abc'`→RangeStr）；`for` 关键字形（`for i <- r { }`）已由 t29 解锁（见下方新列）。 | `t27`（`[0..8):` 含冒号 / `[0..a.len())` 无冒号→warning / `k <- a:` 陣列迭代） |

| **整數字面量值（IntegerLiteral value）** | 復刻 Go `parseIntegerLiteral`（expr.go:652-688）的 **value 計算**——`IntegerLiteral{value, raw}` slot0=`%d`(int64)、slot1=`Raw`（原始文字）。原 `tk.lit.to-i64().to-str()` 對 **hex/u64/i128 全錯**（`0xFF`→`35`、`18446744073709551615`→`20`、`10^22`→`20`）。新增 `ps-int-lit-value(raw str) str`：①`0x/0X` 前綴⇒ base16、否則 base10（Go 僅 `0x` 走 base0 自動偵測，十進位恒 base10 ⇒ `010`=`10` 非八進位）；②剝前綴+前導 0 後折疊低 64 位、外層 `#{overflow=wrap}` ⇒ 自動得 `int64(uval)` 兩倍補數（`2^64-1`⇒`-1`、`2^63`⇒i64-min）；③**超出 u64⇒回 `0`**（Go `isBareIntToken` 分支：保留原始文字給 HIR S2）——hex 有效位 >16 即超、十進位 >20 即超、`==20` 逐位比 `'18446744073709551615'`。探針覆蓋 `0x7fffffffffffffff`/`0xFFFFFFFFFFFFFFFF`/`18446744073709551616`/`9223372036854775808` 全 SAME。⚠️ 純 dumper 側（ast.no 早渲染 slot0），**無須動 ast.no/asp.no**。⚠️ **macOS 提升陷阱**（本增量踩到）：`cp noc-sN /tmp/noc-s0` 就地覆蓋 Mach-O 會令核心快取的代碼簽章失效 ⇒ 執行即 `killed: 9`（rc 137）、輸出全空、誤判「29 fixtures 全 DIFF」——實為簽章問題非代碼問題（同一 bytes 的 noc-sN 仍 OK）。修法：**先 `rm` 再 `cp`**，或 `codesign -f -s - /tmp/noc-s0` 重簽。另 **未納入**：hex 字面量的**型別推斷為 `byte`**（≤255 時 oracle 推 byte、noc 推 i64——屬 LetStatement 合成型別路徑，非 IntegerLiteral value 欄位）、`1_000` 詞法（`_` 為合法識別元起始字元、非分隔符⇒ oracle 拆成 `1` + `_000` ident，本就不在單 INT token 內、無需處理） | `t28`（`0xff`/`0X1A`/`0xFFFFFFFFFFFFFFFF`/`007`/`010`/`18446744073709551615`/`9223372036854775808`） |

| **`for`/`while` 關鍵字迴圈（ForStatement 關鍵字形）** | Go `parseForStatement` 的 FOR 分支（stmt.go:2283+）。`for` 與 `while` 在詞法層同 `tok.FOR`（靠 token `lit` 區分）。新增 `ps-parse-for-kw(t, p)`，分派在 `ps-parse-stmt` 加 `tk.typ == tok.FOR ->`；共用 `ps-for-source(t, p)(src, srcExpr)`（從 bare `ps-parse-for` 抽出，僅回兩個 i64⇒**無結構體、安全多回傳**）。**三種帶 `{ }` 本體形體**：①`for { }` 無限⇒`Condition=BooleanLiteral{true}`（kid1）、`warn+1`（2297）；②`for v <- range [:] { }`⇒`Condition=Identifier{v}`（kid1）+`IterRange=IterationExpr`（kid4）、**無警告**（ARROW 分支跳過 2407；colon 可選、有無都不警告）；③`for expr { }`/`while expr { }`⇒`Condition=expr`（kid1）、`warn+1`（2407 for／2402 while）。ForStatement kid-add `0,cond,0,0,ie,body`。**關鍵區别**：bare 形（t27）**不設 Condition**（僅 IterRange+Body），關鍵字 range 形**多一個 Condition=Identifier**——這是兩者的 dump 差異來源。⚠️ **不實作 C-style `for a; b; c`**：`;` 在 Nolang 是**行註解**⇒該形天然退化（`;` 註解掉後面全部），探針證實無合法 AST。⚠️ **警告只計數**：`Program.warnings` 只是總數（slot0），個體順序不影響 dump⇒`p.warn=p.warn+warn` 放函式尾亦正確。**延后**：labelled、無大括號 inline 本體、比較運算子退路（`for i < n:` 裸 ident 手動拼接形）、C-style `,`/`;`、`in` 關鍵字（僅實作 `<-`） | `t29`（`for {}` / `for n>0 {}` / `for i<-a {}` / `for j<-a: {}` / `while n>0 {}` / `for k<-[0..3] {}`） |

| **`break` / `continue`（BreakStatement / ContinueStatement）** | Go `parseBreakStatement` / `parseContinueStatement`（stmt.go:1550/1570）。`tok.BREAK`=19、`tok.CONTINUE`=20。新增 `ps-parse-break-continue(t, p, brk)`，分派在 `ps-parse-stmt` 加 `tk.typ == tok.BREAK/CONTINUE ->`。**節點無 CHILD**，僅兩個字串 slot（均 `kv` 恆印）：slot0=label、slot1=kind（`labelKindName` ⇒ 'none'/'text'/'numeric'）。⚠️ **kind 必须显式设**：`kv` 政策 P-ALWAYS（unset 也印）⇒ 裸 break 必须 `set-slot(1,'none')`，不能留空（否则印 `kind: `）。⚠️ **標籤侦测不跳 trivia**：Go 消费关键字后直接看**紧邻** token——LABEL(`#N`)⇒numeric（literal 已剔 `#`）、IDENT⇒text；否则（NEWLINE/RBRACE）⇒裸。ps.no 的 NEWLINE **在 raw token 流里保留**（`ps-skip-trivia` 才跳），故 `ps-peek` 直接拿紧邻 token即可区分同列标签 vs 换行后的下一条陈述。⚠️ **未覆盖（已由 t31 独立 if-then 增量解决）**：独立 `cond -> break` 箭頭形（parseStandaloneBody ⇒ IfExpression + **inline** BlockStatement 包裹）——break 節點本身已正确产出，但外围 standalone-箭頭形式待 t31。labelled `outer: for`（oracle 拆成 `ExpressionStatement{outer}`+`ExpressionStatement{nil}` 退化形）亦待后续 | `t30`（裸 break/continue / `break #1` numeric / `continue #2` / if 内嵌裸 break） |

| **獨立 if-then desugar（`cond -> body` / wildcard `-> body`）** | Go `parseExpressionStatement` 的 standalone 分支（stmt.go:1695–1882）+ `parseStandaloneBody`（1593）+ `wrapStandaloneChainDepth`（1653）。**這是 M2 目前解锁语料面最大的一片**（survey 显示 `ExpressionStatement{IfExpression}` 是 74 档的首个分歧节点）。**无外层花括号的 `cond -> body` 在语句层被 desugar 成 `IfExpression`**（不是二元运算符——`ps-op-prec(RARROW)=0`，`ps-parse-expr` 天然停在 `->`）。**形状（全部经 oracle 探针钉死 byte-identical）**：`cond -> expr` ⇒ `ExpressionStatement{IfExpression{cond, BlockStatement{inline:true}[ExpressionStatement{expr}], 0}}`；`cond -> { stmts }` ⇒ **非内联** `BlockStatement{}`（无 inline 标记）；`cond -> break/continue/return` 及 `cond -> IDENT = …`（let）⇒ 内联块**直接**包 `parseStatement` 产物（非 ExprStmt ⇒ 不可链）；行首裸 `-> body` ⇒ wildcard `IntegerLiteral{value:1}`（无 raw，复用 `ps-mk-true`）作 cond。**链式** `a -> b -> c` 递归 nest 成 `if(a){ inline[ExprStmt{ if(b){ inline[c] } }] }`（每层的 body 表达式成为下层 cond）。**rare else**：`cond -> {blk} -> alt` 当 wrap 吃光同列 `->` 后仍有 `->` ⇒ 作 alternative。⚠️ **警告规则**：链式 lint（`WarnChainedIf`）仅在 `wrapStandaloneChainDepth` 进入时 `linkIdx==2`（即整条链已达第 3 个条件）触发**一次**，与链长无关——`a -> b`（2 条件）、`a -> b -> c`（2 条件）⇒ warnings:0；`a -> b -> c -> d`（3 条件）⇒ warnings:1。**实现**：新增 `ps-mk-standalone-if(t,p,cond)`（前置：当前 token 即 cond 后的第一个 `->`）、`ps-standalone-body(t,p)(blk,tail)`（回两 i64、无结构体⇒安全多回传；blk=本体块、tail=可链的单元表达式）、`ps-wrap-chain(t,p,blk,tail,linkIdx)`（递归，本层只需一次判定因递归已吃光后续 `->`）、`ps-rarrow-same-line(p)`（`ps-peek==RARROW` 且 `p.toks[p.pos-1].typ != NEWLINE`，对齐 Go `prevToken.Type != NEWLINE`）。`ps-parse-expr-stmt` 在 (A) 行首 `->`→wildcard、(B) `expr` 后紧跟 `->`→standalone 两处 dispatch，**并把 standalone IfExpression 包一层 ExpressionStatement**（Go 回的是 `ExpressionStatement{IfExpression}`）。⚠️ **本增量不触碰明示-match `subj: { arms }`**：探针证实 subject-match 输出 s26==s27 逐字节相同（standalone-if 只在无冒号处触发，不泄漏进 `ps-parse-match`）。⚠️ **差分假象**：全语料 217 档输出变化，`diff` 行数指标对已严重分歧的大档（md5/ecdsa 数千行差）会因 diff 配对错位而虚报「变差」——真实回归检测须看「s26 曾完全匹配 oracle 的档 s27 是否仍匹配」（结果：0 真回归；view-optional.no 的 IfExpression 数 s26=18→s27=20=oracle，实证改善）。**延后**：块状 bare-match `{ cond -> body }` 分类器（`classifyBlockAtCurrent`⇒blockMatch 与 struct-literal/普通块/`{name:{}}`/混合内容回退的高误分类风险）、`(cond) { }` 前缀条件循环、`x = A -> B` 的 let-管道值形（RTPipelineValue） | `t31`（`a -> b` / `c -> { x = 1 }` / `d -> e -> f`（2 条件无警告） / `-> g` wildcard / `h -> i = 9`（let 本体） / `j -> { y = 2 } -> k`（else）） |
| **註解歸位（`Doc:` 行前註解 + `Comment:` 行尾註解）** | Go `collectDocComments` + `setDoc` + `attachInlineComment`（stmt.go:2085 陳述迴圈、parser.go:1354 setDoc、dump.go:850 comments()）。**這是 M2 解鎖語料面最大的一片**（survey：`Doc:` 是 381/621 檔的首個分歧節點——幾乎每檔都有行前註解，而 noc 完全不發 comments）。**noc 端 dumper 早已就緒**（`ast.no` 有 `doc`/`cmt` 欄位 + `set-doc`/`set-cmt` setter、`comments()` 渲染器經 spec tape `OP-COMMENTS` 掛到對的節點），故本增量**純 ps.no 解析器改動**（ast.no/asp.no 不動）。**語義（全部經 oracle 探針 d1–d7 釘死 byte-identical）**：①**行前註解**（獨立成行、位於某陳述之上）⇒ 該陳述的 `Doc:`；同一群組可跨空行（空行不打斷群組），但 `commentGroupInline` **只渲染群組首條**（`List[0]`）。②**行尾註解**（緊接前一陳述的同一行）⇒ 前一節點的 `Comment:`。③`;` 與 `//` 兩種標記源碼等價，渲染一律正規化成 `//`（`.lit` 存標記之後的文字、含前導空白）。④附著於**陳述節點本身**（`LetStatement`/`ExpressionStatement`/`ForStatement`/定義節點皆收 Doc/Comment，非其內層表達式）。**實現**：新增 `ps-skip-trivia-doc(t, p, prevIdx)`（跳 NEWLINE/COMMENT 的**同時**歸位：行前取首條回傳 docId、行尾 `set-cmt` 到 prevIdx）與 `ps-cur-lit(p)`（`.copy()` 讀當前 token 的 lit，切斷與 toks 向量的共享——見檔頭釋放守則）。改寫 `parse` 頂層迴圈與 `ps-parse-block`（兩者同形），以 `prev = s`（最後一個已建兄弟節點）驅動行尾附著、`docId != 0 -> set-doc(t, s, docId)` 驅動行前附著。⚠️ **換行偵測靠保留在 token 串中的 NEWLINE token**（本解析器不預先丟棄換行）：run 內尚未見 NEWLINE 且 `prevIdx>0` ⇒ 行尾，否則行前。⚠️ **不重構 `ps-parse-stmt`**：迴圈先在呼叫前用 `ps-skip-trivia-doc` 掃淨 trivia，`ps-parse-stmt` 入口的 `ps-skip-trivia` 即成 no-op，不會重複吞註解；其內部其他 `ps-skip-trivia`（型別標註與值之間）均在子運算式**之前**、非陳述尾，故行尾註解留在 `p.pos` 供迴圈捕捉。**效果**：全語料逐位元組相同檔 **25 → 122（+97）**，`Doc:` 首分歧 381→4；差分回歸 **0**（s27 曾匹配 oracle 的 25 檔在 s28 全部仍匹配）。**延後**：區塊收尾 `}` 前自成一行之註解（Go 歸 `block.TrailingComments`/`FreeComments`，本增量直接丟棄——僅 3–9 檔因此剩差）、陳述中途（運算式 token 之間）的註解、`#{...}` side-table。 | `t32`（行前多條含空行 / 行尾 Comment / `//` 標記 / 運算式陳述 Doc / for Doc / 函式內層 Doc+Comment） |
| **方法定義（`recvType.method = (params) [ (results) ] { body }`）** | Go `parseMethodDefinition`（decl.go:11–99，分派於 stmt.go:292–301）。修復 `noc` 對 `str.to-i64 = () (val i64) { … }` 的 **busy-spin**（此前 `IDENT DOT IDENT =` 不匹配任何陳述臂→退回運算式→在 `=` `(` `{` 處誤吞、`{` 永不閉合）。**語義（oracle 探針 me1/me4/me5/me6 + dump-ast.no 釘死 byte-identical）**：desugar 成 `FunctionDefinition{name: 'recvType.method', methodDef: true}`，`self` 合成為【首個輸出參數】（Results、非 inputs）`Parameter{name: self, type: NamedType{recvType}}`——`Results = prepend(selfParam)`，故 kid 區序 `[inputs…][self][results…][body]`、`set-sig` results 數 = 1 + 宣告回傳數。⚠️ **methodDef = FunctionDefinition 第 3 個純量槽（slot2）**：該節點恰有 8 個 dump 純量欄（name/colonSyntax/methodDef/skipNamingCheck/overflow/builtinStub/builtinName/builtinGroup = s0..s7），`kvb` 需顯式 `set-slot(2,'true')`。⚠️ **分派守衛** `IDENT DOT IDENT =` 且 `+4==LPAREN` 且 `ps-is-func==1`：`obj.field = value`（+4 非 LPAREN）或 `x = (expr)`（ps-is-func==0）一律 `p.pos = savePos` **還原**、退回運算式陳述——非函式形的點號賦值絕不誤吞。**實現**：新增 `ps-lit-at(p,k)`（向前看 k 讀 `.lit.copy()`）與 `ps-parse-method-func(t,p,fullName,recvType)`（進入時 pos 在輸入 LPAREN，重用 ps-parse-param/ps-parse-block，自建 self NamedType+Parameter 前置到 outs）。**效果**：`tests/dump-ast.no` 由 busy-spin 轉為逐位元組相同；方法呼叫形 `s.to-i64()` 早已匹配。**延後**：泛型方法 `IDENT DOT IDENT < …`（LESS 形）、陣列/切片型別方法 `[n]t.method`（isArrayTypeMethodDefinition）、限定接收者 `a.b.c.method`、`obj.field = value` 的 AssignExpression 形（屬獨立缺口）。 | `t33`（`point.zero = () (self point, ok i64) {…}` 明示 self 於 results / `vec.push = (v i64) {…}` 無回傳 / `grid.cell = (r i64, c i64) (idx i64) {…}` 輸入+回傳） |
| **結構體定義（陳述位 `name { field TYPE ... }`）** | Go `parseStructDefinition`（decl.go:1257–1505，分派於 stmt.go:49–115 的 `IDENT LBRACE` + `classifyBlock`⇒blockStruct 守衛，parser.go:267–513）。**注意與 t19 結構體【字面量】分流**：`Point{...}`（運算式右側）早已匹配；本片解鎖的是【陳述位置】的 `reader { fd i64 / buf []byte / pos i64 }` 型別宣告——此前 `noc` 把 `reader` 當成 `ExpressionStatement{reader}`、把區塊內容拆成獨立陳述（survey：`StructDefinition` 是 80 檔首分歧節點）。**語義（oracle 探針 s1/s2/s3 + bufio/tar 釘死 byte-identical）**：`StructDefinition` 恰 3 個純量槽——slot0=`name`、slot1=`implements` 數（`kvc` NON-ZERO：設 '0' 即省略）、slot2=`fields` 數（`kvc` OF-LEN，設 `flds.len().to-str()`）；另需 `set-count(idx, 0, flds.len())` 驱动 KIDS 迭代；kid 序=各 `StructField`。`StructField` 槽位：slot0=`name`、（`type:` 為 OF-TYPE，由 dumper 自 kid0 【合成】、**不佔槽**）、slot1=`arraySize`（`kvn` NON-ZERO：'0'→省略）、slot2=`slice`（`kvb`：'true'）、slot3=`readonly`、slot4=`sealed`；kid0=Type 節子（子先於父）、kid1=Value（定義恆 0 佔位）。**欄位型別形**（本檔前 25 struct 檔實測分布：NamedType ~84、`[]T` ~40、`[N]T` ~50 為主流）：`type: i64`→NamedType、`[]byte`→SliceType（slice:'true'）、`[8]i64`→ArrayType（arraySize:'8'）；`type:` 文字由 `ast.type-string`（ast.no:601）自 kid0 渲染（NamedType→name、SliceType→`[]`+elem、ArrayType→`[`+size+`]`+elem）。**實現**：①擴充 `ps-parse-type` 加 `[N]T`⇒ArrayType 分支（復用 t13 陣列標註臂建法：消費 `[`→`ps-parse-expr` 建 size IntegerLiteral→`ps-expect RBRACKET`→建 elem NamedType→ArrayType set-slot(0,'false')→kid-add sz、elem）；②新增 `ps-is-struct-def(p)`【不消費 token】的保守分派守衛——要求 `IDENT LBRACE`，掃描每欄需 `IDENT (IDENT | []IDENT | [INT]IDENT)` 且後接 NEWLINE/RBRACE/COMMA/EOF，任何偏離（枚舉 `a, b`、介面 `m()`、實作清單 `name impl {`、限定型別 `a.b`、可為空 `?T`、`#{...}` 註解）一律回 0→交回原運算式陳述路徑（零回歸）；③新增 `ps-parse-struct-def(t,p,name)` 建 StructField（子先）再建 StructDefinition 父；④`ps-parse-stmt` IDENT 块加 dispatch 臂 `nxt_typ == tok.LBRACE && ps-is-struct-def(p) == 1 -> ps-parse-struct-def`（置 method-def 臂後、`handled == 0` 前）。⚠️ **踩到兩次同類坑（已入 §2.1.1 尾臂坑類）**：`ps-is-struct-def` 的 `okType == 1` 體與 `ps-parse-struct-def` 的 `tt` 型別分支鏈，皆因裸 `-> { default }` 未包在 `{ }` match 块內→**作為無條件尾隨陳述屢次執行**，前者把 `r` 强制覆寫為 0（struct 路徑永不觸發、仍吐 ExpressionStatement）、後者在 slice 分支後又重跑 `ty = ps-parse-type` 把 `ty` 清為 0（`[]byte` 吐 `slice: true` 卻空型別）。修法：把多臂 match 分派包進顯式 `{ }` 块。⚠️ **場域**：本增量主要價值是把 bufio/tar/zip 等檔的首分歧【推後】——這些檔仍因下游 `obj.field = value`（AssignExpression）與型別推斷（`inferred: true`）缺口而 DIFF，故 net 逐位元組相同僅 +1（`tests/arr-field-lit.no`），但為後兩片增量解鎖了更多可達節點。**延後**：implements 清單、enum/tagged-enum/iface 形、限定型別 `a.b` 欄位、可為空 `?T` 欄位、`#{...}` 欄位註解（Go 走 side-table 不 dump）。 | `t34`（`point { x i64 / y i64 }` NamedType 欄 / `buffer { data []byte / pos i64 / table [8]i64 }` SliceType+NamedType+ArrayType 欄） |
| **lvalue 賦值（`obj.field = v` / `arr[i] = v` / `mp[k] = v` ⇒ AssignExpression）** | Go `parseExpression` 尾部（expr.go:598–637，`=` 之後）。**這是 M2 解鎖語料面最大的一片**（survey：`ExpressionStatement{AssignExpression}` 是 72 檔首分歧節點——幾乎每個有狀態物件/集合操作的 std 模組都用 `obj.x = v`、`m[k] = v`）。此前 `noc` 把 `obj.x = 1` 拆成 `ExpressionStatement{DotExpression}` + `ExpressionStatement{1}`（`=` 被丢）。**純解析器改動**：`AssignExpression`（K=12）**無純量槽**、兩個 unguarded CHILD（Left、Value），dumper 早已就緒（`ExpressionStatement{...}` 標籤由 kid 的 nodeString 派生，`node_of: Expression`）。**語義**：`=` 非中綴運算子（優先級 0）⇒ 中綴迴圈天然停在 ASSIGN；若此時左值（`ast.kind-at`）是 Dot/Index ⇒ 消費 `=`、以 LOWEST 解右值、建 `AssignExpression{Left: target, Value: val}`（右結合：`a.b = c.d = 7` → `AssignExpr(a.b, AssignExpr(c.d, 7))`）。**實現**：在 `ps-parse-expr-prec` 尾部（中綴迴圈後、`return idx` 前）加一段检查，用【活】`ps-peek(p) == tok.ASSIGN`（`=` 前若換行則 currentToken=NEWLINE、不觸發，對齊 Go）；`isLval` 由兩個獨立 standalone-if（`k0 == DOT -> isLval = 1` / `k0 == INDEX -> isLval = 1`）累加（非同一 match 的臂，故不踩裸 `->` 坑）。**與既有路徑不冲突**：`x = 1`（裸 IDENT ASSIGN）在 `ps-parse-stmt` 層已由 LetStatement 臂截走，不会到此；`obj.field = v`（IDENT DOT IDENT =）因 method-def 臂要求 `+4==LPAREN`、`f()=v` 因左值是 CallExpression（非 Dot/Index）而不誤触。**效果**：全語料逐位元組相同 **122 → 137（+15）**，差分回歸 **0**（含 `tests/struct.no`、`src/std/crypto/rand.no`、`tests/oob-slice-assign.no`、`tests/vec-assign*.no` 等）。**延後**：`?=` → UnwrapAssignStatement（另一節點）、`f() = v` 的 `ExpressionStatement{nil}` 杂乱 `=` 错误恢复形（非法代碼、語料未見、str-order.no 既有 hang 亦属另一缺口）。 | `t35`（`obj.x = 1` DotExpression 目标 / `obj.y = obj.z` / `arr[0] = 9` `arr[i] = arr[j]` IndexExpression 目标 / `a.b = c.d = 7` 右结合链） |
| **裸 `return`（ReturnStatement）** | Go `parseReturnStatement`（stmt.go:1424–1441）。**`return` 是 Nolang 常见陈述**（survey：`ReturnStatement{}` 是 21 檔首分歧節點——函式提前返回都先 `out = …; return`），此前 `noc` 完全丢弃它（ps-parse-stmt 無 RETURN 臂→走 ps-parse-expr-stmt→RETURN 無 primary 处理器→idx=0）。**语义**：Nolang 以具名 out-param 傳值，`return` 恒裸（`ReturnValue` 永远 nil）——Go 对 `return <val>` 直接 `saveError`（parse error、语料不会出现）。**节点（/tmp/ast_spec.json + dump.go:235–241）**：`ReturnStatement`（K=56）**无纯量槽**、`node_of: ReturnValue`（`braces: true`、`absent: ""`）、CHILD(ReturnValue, guard=true)、COMMENTS——ReturnValue 缺席 ⇒ 渲染 `ReturnStatement{}`。**实现**：在 `ps-parse-stmt` 顶层 match 块（BREAK/CONTINUE 臂旁）加 `tk.typ == tok.RETURN -> { ps-bump(p); idx = ast.add(t, K_RETURN_STATEMENT) }`（仅消费 RETURN、不消费终止符，同 Go）。⚠️ **guarded-缺席仍须 `kid-add(idx,0)` 占位**（新坑、同 §2.1.1 arena kid 对齐类）：不占位时 header pre-scan `childs.push(kid-at(cur))` 会读到**后续节点的 kid**⇒`node_of` 标签错为下一个陈述的 nodeString（实测 `ReturnStatement{LetStatement}` + 多带一个 kid）；补 `kid-add(idx, 0)` 后 guard 读到 0 ⇒ `{}` 且无 kid。同时修复 `cond -> return`（ps-standalone-body 已将 tok.RETURN 归 isStmt=1 转 ps-parse-stmt，现该臂接上）。**效果**：全语料逐位元组相同 **137 → 145（+8）**（`tests/getline.no`、`tests/str-repeat.no`、`tests/to-bytes.no`、`tests/u128-*.no` 等），差分回归 **0**。`tests/env.no` 首分歧（return）已清→剩余为 `(cond){}` bare-block-loop⇒ForStatement 另一缺口。**延后**：`return <val>` 错误恢复形（非法代码）、ForStatement 前缀循环。 | `t36`（`{ out = x; return }` 陈述尾裸 return / `flag == 1 -> { out = 9; return }` standalone 内联 / `return` 后接陈述的提前返回） |
| **语句级 `#{...}` 注解（overflow 槽）** | Go `parseStatement` 的 `HASH_LBRACE` 分支（`parseAnnotationStatement`）+ `attachAnnotations`→`setStmtOverflowMode`（annotation.go:315–342）。**这是当前最大可治纯解析缺口**（survey：258 档 LetStatement 首分歧中 **83 档**是 `#{...}` 泄漏成假 `LetStatement{name: index-out/overflow, ...}`）。`#{` 为单 token `HASH_LBRACE`（lx.no:1310），此前 ps.no 无处理⇒ Interior `overflow=wrap` 走 IDENT+ASSIGN→假 LetStatement、且紧后陈述丢 `overflow` 槽。**语义**：注解仅 `overflow=<mode>` 会 dump（写进语句节点的 `overflow` 纯量槽）；`index-out`/`platform`/`embed` 等只入语义副表、**不影响 AST dump**⇒ 本增量只需「整段吞掉不外泄」+ 抽 overflow。`NormalizeOverflowMode` 合法值 `wrap/clamp0/min/max/saturate`（语料中 4164 处几乎全为 `wrap`）。**节点槽位（/tmp/ast_spec.json）**：非 child-derived 纯量序：`LetStatement`=s6、`ForStatement`=s2、`MultiAssignStatement`=s1、`FunctionDefinition`=s4（`ExpressionStatement`/`ReturnStatement` 有 OverflowMode 字段但 dump 不渲染⇒略过）。**实现**：新增 `ps-parse-annotation`（深度计数吃平衡 `#{...}`、顶层捕捉 `overflow=<ident|string>`）、`ps-normalize-overflow`、`ps-apply-overflow`；在 `ps-parse-stmt` 顶（`ps-skip-trivia` 后）连续消费前置 `#{...}`、解析紧后陈述、末尾按节点 kind 写对应槽。因所有语句集合循环（parse/block/standalone/match 臂）皆走 `ps-parse-stmt`，单点拦截覆盖全上下文。无 `#{` 时循环不执行⇒**零回归结构性保证**（已匹配的 145 档不含注解）。**延后**：尾随同行注解 `stmt #{...}`（目前会被当作下一条的前置注解、错标目标；语料以首行前置为主故本增量未触）；`i64-max` 型别前缀形式归一。 | `t37`（`#{overflow=wrap}` 前置 / `#{index-out=zero}` 仅吞不 dump / `#{index-out=zero, overflow=wrap}` 合并取 overflow） |
| **`#` 匯入（UseStatement）** | Go `parseUseStatement`（stmt.go:45、`case lexer.USE`）。`#`（非 `#{`）詞法回 `tok.USE`（lx.no:1328、lit='#'）；語料形式恆為 `# std/number` / `# /example/...` 纯路径（11 檔 use-bucket 實測：無 `.fn` / `as`）。此前 ps.no 無 USE 臂→走 expr-stmt⇒把 `std/number` 拆成 `InfixExpression{op: /}` 除法鏈。**節點（ast_spec.json + dump.go:180）**：`UseStatement`（K=70）純量槽 path=s0、function=s1、alias=s2（三者 `kv` P-ALWAYS、即使空也渲染 `function: , alias: `）、as=s3（`kvb` policy=true、false 則省略）；無 CHILD、COMMENTS（doc 由既有 ps-skip-trivia-doc 附上）。**實現**：新增 `ps-parse-use`（消費 `#`→可選前導 `/`→以 `/`、`.` 分隔的 IDENT 段用 `+` 串接成 path（joinPathParts））+ ps-parse-stmt 加 `tk.typ == tok.USE -> ps-parse-use`。因任何含 `#` 匯入的檔此前必已 NO，故新增 USE 臂嚴格只改善→**零回歸結構保證**。**效果**：全語料 **158 → 159（+1，`tests/sha3.no`）**。USE 只清首分歧——其餘 10 檔随即在下游 match-block/standalone-if/BlockStatement 簇（`ExpressionStatement{IfExpression}`/`BlockStatement{}`）或 FloatLiteral value 精度（`1.41421` vs 全精度）再分歧，故僅 sha3 全匹配。本增量價值在打通 import 前置、為 match-block 主簇铺路。**延後**：`.fn` 函式名分隔符 + `as alias`（語料未見）。 | `t38`（`# std/number` / `# /test/runner` 前導斜線 / `# std/net/http` 多段） |
| **推論元素型別的定長陣列 `name [N] = [..]`（ArrayLiteral）** | Go `parseLetStatement` 的 `[N]` 分支（`stmt.go:947–959`）+ SliceLiteral→ArrayLiteral 轉換（`stmt.go:1210–1218`）。survey：ArrayLiteral typed-let 是 LetStatement 桶最大子因（~47 檔首分歧）。明示形 `name [N]T = [..]` 早已支援（+4==IDENT/+5==ASSIGN 臂），**但省略元素型別的 `name [N] = [..]`**（+4==ASSIGN/+5==LBRACKET）此前無臂→ `name [N]` 被誤解析為 IndexExpression、`= [..]` 成 AssignExpression。**關鍵路徑語義（探針實測、非推論）**：① 元素型別【恆為 `i64`】——Go 對省略元素型別的 `[N]` 一律補 `NamedType{Value:"i64", IsInferred:true}`（stmt.go:957），【不】依首元素 kind 推論（bool/float/str 陣列在 oracle 也出 `[N]i64`）；② `ArrayType.IsInferred=false`（源碼顯式寫了 `[N]`→formatter 必須印回 `[N]`）；③ 僅 `Size>0`（IntegerLiteral value>0）才把 RHS SliceLiteral 轉 ArrayLiteral（設 `wasSliceLiteral:true`）——`[0] = [..]` 保留 SliceLiteral（探針 `e [0] = []` ⇒ `value: SliceLiteral{}`）。**實現**：在 `ps-parse-stmt` 明示陣列臂後新增推論臂（守衛 `+2==INT && +3==RBRACKET && +4==ASSIGN && +5==LBRACKET`），復用既有 `ps-parse-arr-lit`；讀 `sz_lit`，`!='0'` 走 ArrayLiteral、`=='0'` 走 `ps-parse-expr`（SliceLiteral）；建 `NamedType{i64, inferred:true}` → `ArrayType{size, elem}` → `LetStatement{name, type, value}`。**零回歸結構保證**：任何含此形的檔此前必已被誤解析為 Index+Assign（已 NO），新臂只改善。**效果**：全語料 **159 → 202（+43）**、差分回歸 **0**（uniform 120s-alarm `-P 3`：s34=159→s35=202）。為 aes/zlib 等大量 `[N] = [int-literals]` 表檔解锁。 | `t39`（`a [3] = [10,20,30]` 推論 i64 / `b [4] = [1,2,3,4]` / `e [0] = []` Size==0 保留 SliceLiteral） |
| **方法體 `.field` 前導點（self 速記）** | Go `parsePrefixExpression` 的 `case lexer.DOT`（expr.go:230–245）。survey：`DotExpression{receiver: …}` 是 25 檔首分歧節點（方法體大量使用 `.pos`/`.buf[i]`/`.fill()` 速記）。`.field` ⇒ `DotExpression{Receiver: Identifier{"self"}, Property}`；裸 `.` ⇒ `Identifier{"self"}`。**此前無前綴 DOT 臂** ⇒ `.field` 落到 ps-parse-expr-prec 預設臂（idx=0）再經**後綴** DOT 迴圈用 idx=0 作 receiver ⇒ dump `DotExpression{receiver: <nil>}`（oracle 為 `receiver: self`）。**實現**：在 ps-parse-expr-prec 前綴 match 塊（ SliceLiteral 臂後、預設臂前）新增 `tk.typ == tok.DOT` 臂：消費 `.`、若緊接 IDENT 則**先建 self Identifier（子）再建 DotExpression（父）**（slot0=property、kid0=self Identifier）、否則裸 `.` 建 `Identifier{"self"}`。`self` 名硬編碼（與 Go `&Identifier{Value:"self"}` 同）。**零回歸結構保證**（含 `.field` 的檔此前必已 NO）。**效果**：全語料 **202 → 203（+1，`src/std/io.no`）**、差分回歸 **0**。⚠️ **關鍵發現（下一個高價值增量）**：self-dot 清掉 25 檔首分歧後，其餘 24 檔立即在 **`AnnotationStatement{entries: N}` 節點**（collection/*・bufio・regexp・txt・yaml 等）或 **ForStatement `!! { }`/`(cond) { }` 迴圈簇**再分歧。尤其 `AnnotationStatement`：增量 R 把陳述級 `#{...}` 全吞掉，但 oracle 對**無法附著的** `#{...}`（紧跟 ExpressionStatement/AssignExpression、無 overflow 可渲染槽者）會**保留一個 `AnnotationStatement{entries: N}` 普通節點**（紧跟 LetStatement/ForStatement 等可附著者則無節點、只設 overflow 槽—t37 已驗證）。故 ** emitting AnnotationStatement 節點** 為解锁 collection/* 的主鑰匙。 | `t40`（`.fill()` 呼叫 / `.buf[.pos]` 索引 rvalue / `.pos = .avail` lvalue+rvalue） |
| **独立 `AnnotationStatement{entries: N}` 節點（不可附著之語句級注解）** | 即上一列揭示的 collection/* 主鑰匙——已於本增量實現。Go `parseAnnotationStatement`（annotation.go:188–309）+ `attachAnnotations`。**關鍵規則**：語句級 `#{...}` 只在「緊後陳述首 token ∈ {IDENT, IN, RARROW}」時【附著】於該陳述（Go 247 分支：回傳 `parseStatement` 結果、**不產生独立節點**；overflow 寫入陳述純量槽＝增量 R/t37 路徑）；**否則（首 token 為 DOT `.field=`、LPAREN、LBRACE 等不可附著形）oracle 保留一個 `AnnotationStatement{entries: N}` 普通節點**作為兄弟陳述。增量 R 全吞 `#{...}`、從未發節點⇒ 此類檔永遠缺這一行。**entries 計數（dump.go:306 + parseAnnotationBody:990 + dedupeAnnotationEntries:853）**：N = 合併連續 `#{...}` 群組後【去重的頂層鍵名數】（`kvc`：`0` 則省略）；鍵 IDENT 只在 `#{` 後或頂層 `,` 後（expect_key），值內 ident（如 `wrap`）/ 括號不計；同鍵名跨群組去重只計一次。語料實測 404×`entries:1`、25×`entries:2`（無更高）。⚠️ 鍵名可含連字號（`index-out`/`win-amd64`）—— Go（lexer.go:959）與 noc（lx.no:175）皆以 `-` 為識別元內字元⇒ `index-out` 為**單一 IDENT**（僅 leading `-` 走運算子）。**節點（ast_spec.json）**：`AnnotationStatement`（K=7）仅一顆 `entries` 純量（of:len/policy:count/render:int）經【槽字串】渲染（非 c0！`set-slot(0, keys.len().to-str())`），另 COMMENTS；ANNOTS 子行只在 `--annotations` 旗標下渲染（預設 `no ast` 不印）。**實現**：將 `ps-parse-annotation`（舊只回 overflow）**內聯進 `ps-parse-stmt` 前置註解迴圈**，同段掃描同時產出 `ovf` 與去重 `keys []str`；以深度計數吃平衡（`#{ [ ( {`→+1、`} ) ]`→−1），頂層鍵去重入 keys、`overflow=` 取 `ps-normalize-overflow` 值。**末尾新增分派**：`keys.len()>0 && 緊後首 token ∉ {IDENT, IN, RARROW}` ⇒ `ast.add(K_ANNOTATION_STATEMENT)` + `set-slot(0, …to-str())` 並【就此 return】（緊後陳述留待下輪 ps-parse-stmt，兩者成兄弟）。**零回歸結構保證**：keys 空（無註解）時整段跳過；且任何含不可附著 `#{...}` 的檔此前必已 NO。**效果**：全語料 **203 → 204（+1，`src/std/collection/stack.no`）**、差分回歸 **0**。⚠️ heap.no 等仍未完配——本節點修正後它們立即在**下游 `GenericParams`（泛型）與 `(cond) { }`/`!! { }` ForStatement 簇**再分歧（即下一個主簇）。 | `t41`（`.pos=` 前 `#{overflow=wrap}`→entries:1 / 雙鍵 `#{index-out=zero, overflow=wrap}`→entries:2 / 連群組 `#{index-out=zero}`+`#{overflow=wrap}`→entries:2 / 同鍵兩群組→entries:1 / IDENT 前附著→無節點） |
| **`!! { }` / `! { }` / `(cond) { }` 陳述位循環（ForStatement bang/cond-loop 簇）** | 即上一列揭示的下游主簇——已於本增量實現。Go 三條入口：`parseBangLoop`（stmt.go:528–540 dispatch → 2577）——`case NOT`/`case BANG_BANG` 僅當 `peekToken==LBRACE` 才成循環，否則 `parseExpressionStatement`（`!x` 取反）；`case LPAREN`（stmt.go:381 `isCondLoopPrefix`→2686 `parseCondLoopPrefix`）。**bang-loop**：`Condition=BooleanLiteral{loop}`（`!!`→true 無限、`!`→false 不執行）、`Body=parseBlockStatement`。**cond-loop 判別（isCondLoopPrefix:2615）**：(1) 掃至平衡 `)`；(2) 其【立即】下一 token 須為 `{`（`tokAt(k+1)`，換行後の `{` 屬後綴式 `{ } (cond)`、非本形）；(3) 括號內容含 `,` 或「恰好兩個 IDENT」（參數名+型別）⇒ 匿名函式字面量、非循環；其餘（`()`、單 IDENT `(flag)`）⇒ 循環。`()` ⇒ `BooleanLiteral{false}`；`(expr)` ⇒ `Condition=parseExpression`。**節點（ast_spec.json `ForStatement`）**：3 純量——`label`(s0,kv 恆印)、`condWrapper`(s1,policy:true 僅 IsCondWrapper 印、本三形均 false 故不印)、`overflow`(s2,nonempty)；6【guarded】CHILD `Init,Condition,Update,CountExpr,IterRange,Body`——缺席者以 `kid-add(…,0)` 佔位（dumper 略過 0）。⚠️ 三形皆【不】saveWarning（新式語法）⇒ 不累加 `p.warn`（否則 Program `warnings: N` 錯）。**實現**：新增三個 helper `ps-parse-bang-loop`/`ps-is-cond-loop-prefix`（仿 `ps-is-func` 平衡括號掃描）/`ps-parse-cond-loop`，並在 `ps-parse-stmt` 主分派新增三臂（BANG_BANG/NOT 需 `peek-at(1)==LBRACE`、LPAREN 需 `ps-is-cond-loop-prefix==1`，否則落回既有 expr-stmt ⇒ 零回歸）。⚠️ **踩坑**：`ps-parse-cond-loop` 原在兩臂各呼叫 `ps-peek(p)`，首臂消費 `)` 後次臂重取看到 `{` ⇒ `()` 雙臂皆執行、cond 被覆寫成 0（`BooleanLiteral{false}` 消失）；改【預先取 `hd=ps-peek(p)` once】（同 `ps-parse-for-kw` 守則）後修復。**效果**：全語料 **204 → 213（+9：`src/std/bufio.no`、`collection/{hash-set,str-set}.no`、`env.no`、`tests/{dowhile,loop-test,split-loop,str-loop-assign}.no`、`mem-safety/bug15-read-dowhile-copyfile.no`）**、差分回歸 **0**（`!!`/`(` 此前於 noc 【挂死】——無分派臂→expr-stmt 內圈不前進）。⚠️ heap.no 修正後僅在 `GenericParams` 再分歧（即下一個主簇）。 | `t42`（`!! { }`→BooleanLiteral{true} / `! { }`→{false} / `(.i<3) { }`→InfixExpression / `() { }`→{false} / `(.flag) { }`→DotExpression 條件） |
| **字面量型別推斷補齊（`CharLiteral`→`char`、十六進位 `IntegerLiteral`→`byte`）** | 即 bang-loop 揭示後 path.no/char-* 檔的第一分歧子因（探針實測：`SEP = "/"` oracle 出 `LetStatement{name: SEP, type: char, value: /}` + `NamedType{name: char, inferred: true}`，noc 缺此 NamedType）。Go 推斷在 `parseLetStatement` 的 `stmt.Type == nil` 分支（stmt.go:1227–1268），【僅對字面量 RHS】合成 `NamedType{IsInferred:true}`。既有實作已覆 INT→i64/FLOAT→f64/STRING→str/BOOL→bool，**漏了兩項**：① `case *CharLiteral: Value:"char"`（stmt.go:1263）——雙引號 char 字面量未推；② `case *IntegerLiteral` 的十六進位分支（stmt.go:1229–1235）——`raw` 以 `0x/0X` 開頭且 len>2 ⇒ `byte`，否則 `i64`。同一口徑亦見於 SliceLiteral 首元素推斷（stmt.go:1274–1300：Char→char、hex-INT→byte）。**實現**：新增 helper `ps-intlit-hex(raw)`（复用 `ps-int-lit-value` 已有的 base 偵測同寫法：`raw.len-bytes()>2 && raw.byte(0)==48 && (raw.byte(1)==120||raw.byte(1)==88)`）；`ps-infer-type` 舆 `ps-elem-type-of` 兩處同步：`K_INTEGER_LITERAL` 臂改為先 `raw = ast.str-at(t, ast.nd-s(d, 1))`（IntegerLiteral slot1=raw 源碼文字）、`ps-intlit-hex(raw)==1 -> 'byte'` 否則 `'i64'`；新立 `K_CHAR_LITERAL -> 'char'` 臂。`char` 名硬編碼（對應 Go `ValueTypeChar.String()`）。**零回歸結構保證**：僅新錯位【之前缺失】的 inferred NamedType（原回 ''不推⇒無子節點），不觸碰已有型別標註路徑。**效果**：全語料 **213 → 222（+9：皆 `tests/char-*`——`char-literal`/`char-new-method`/`char-new-syntax{,2,3}`/`char-old-syntax2`/`char-simple`/`char-step4`/`char-vet`）**、差分回歸 **0**。⚠️ path.no 的 char 推斷已修（首分歧從 line 2 推到 line 423）、但仍於**下游裸 match desugar**再分歧（即下一個主簇）。 | `t43`（`SEP="/"`/`DOT="."`→char / `H=0x41`、`HBIG=0Xff`→byte / `DEC=65`、`ZRO=0x0`→i64・byte 邊界 / `ARRHEX=[0x50,0x4b,0]`→[]byte / `ARRDEC`→[]i64 / `ARRCHAR=["a","b"]`→[]char） |
| **無值明示型別裸宣告 `name TYPE`（陳述位 LetStatement）** | 即上一列揭示後 crypto/archive 檔的第一分歧主因（探針實測：`result []byte` oracle 出 `LetStatement{name: result, type: []byte, value: }` + `SliceType{}`/`NamedType{byte}`，noc 把 `result [` 误解析為 `IndexExpression`、`byte` 成單獨 `ExpressionStatement`）。survey：LetStatement 桶 173 檔，其中 **73 檔首分歧即此形**（oracle 行含 `value: }` 空值），跨 `[]byte`(17)/`[32]byte`(10)/`[]str`(7)/`[16]byte`(4)/str(4)/i64(4)/… 皆同。**Go 规则（stmt.go:170–249 + 1051–1060）**：`IDENT LBRACKET` 於陳述位以【`[` 前是否有空格】区分標註 `name [N]T`（有空格）vs 索引 `name[i]`（无空格，行 181 column 比较）；`parseLetStatement` 解完型別後，若 current/peek 為 NEWLINE/RBRACE/EOF ⇒ **仅型別宣告、无赋值**，回傳 `Value=nil` 的 LetStatement。`IDENT IDENT`（无 `=`）同理只能為裸宣告（Nolang 无「两 ident 无运算子」的合法運算式）。**实现**：新增 helper `ps-parse-typedecl-noval(t,p,name)`——`ps-bump` 消费 name IDENT → 復用既有 `ps-parse-type`（已支援 NamedType/NullableType/SliceType/ArrayType 全形、子先於父）→ 预取 `hd=ps-peek(p)`，仅当 `hd ∈ {NEWLINE,RBRACE,EOF,SEMICOLON,COMMENT}` 且 `hd != ASSIGN` 时建 `LetStatement{name, type, value:0}`（Value guarded CHILD 以 0 占位⇒`ast.wanted` 的 OF-LOCAL/R-NODE-STRING 遇 `child<=0` 回 `''`⇒dump `value: `）；否则**还原 p.pos**。**分派**：`ps-parse-stmt` 主分派末尾（各含值臂之后、`expr-stmt` 之前）新增一臂 `handled==0 && (nxt_typ==LBRACKET||nxt_typ==IDENT)`——含值臂皆要求 `=`（未命中⇒handled==0）才会进本臂；命中后设 handled=1。**零回归结构保证**：（①`ps-parse-type` 对索引 `x[i]`（`[` 后 IDENT非`[`/INT`）、`x[0]=v`（`]` 后 ASSIGN非 IDENT）均回 0⇒还原；②还原时所建的孤兒節點不被任何 kid 引用，dumper 自 root 沿 kid0/nkid 走訪（`ast.dump`→`ast.node`）永不触及，仅耗 arena；③nxt_typ 為固定快照，仅 LBRACKET/IDENT 两值进本臂，后续 COMMA/ARROW/DOT/LBRACE/QUESTION 臂的固定 nxt_typ 判定不匹配。）⚠️ **未涵盖**：限定型别名 `name a.b`（`ps-parse-type` IDENT 臂不吞 DOT）舆嵌套 `[][]i64`（slice 臂需 `[]` 后立即 IDENT）——此两类 ps-parse-type 回 0⇒还原、行为同前（无回归），留待后续。**效果**：全语料 **222 → 253（+31）**、差分回归 **0**（uniform 120s-alarm `-P 3`：新匹配 crypto/{sha256,hmac,aes-*,tls*}、archive、tests/{u128,vec-push-clear,words-test,…}）。⚠️ zlib.no 修复首段后仍于**下游 `data.len` 独立 if 反化 / 裸 match desugar**再分歧（即下一个主簇）。⚠️ 当时未涵盖的限定名/嵌套型已于下一列（s41）补齐。 | `t44`（`result []byte`/`z [16]byte`/`items []str`/`x box`/`i8 i8` 裸宣告 / `a []i64 = [1,2,3]` 含值对照 / `x.n=a.len()` 非宣告） |
| **複合型別名：限定名 `a.b` + 嵌套元素 `[][]T`/`[N][]T`（ps-parse-type 扩展）** | 即上一列揭示的「未涵盖」同族——已於本增量实现。探针实測三类同根因（`ps-parse-type` 不认限定/嵌套）：`c tls.conn`（限定无值）oracle 出 `LetStatement{name: c, type: tls.conn, value: }` + `NamedType{name: tls.conn}`，noc 拆为 `ExpressionStatement{c}` + `DotExpression{tls, conn}`；`a [][]i64`（嵌套无值）oracle 出 `SliceType{ SliceType{ NamedType{i64} } }`，noc 拆为嵌套 `IndexExpression`；`f ?fs.file = call`（限定含值可空）oracle 出 `NullableType{ NamedType{fs.file} }`，noc 误为 `type: ?fs` + 剩 `.file` 泄入 AssignExpression。survey：**12 档首分歧即此类**（src/std/log.no、net/proxy.no、fs-error-{complete,final,info,simple}、fs-open-err、hpack、http2、mem-safety/{deep-free-nested-vec,nested-container-clone}、nested-vec-test、net-client、push-narrow、std-new、tls{,-part3}、vec-elem-ownership）。**Go 规则**：限定名 stmt.go:1034 `for cur==DOT { typeName += "."; next; if IDENT { typeName += lit; next } }`（整条存进单一 NamedType.Value，不另建节点）；嵌套型 stmt.go:937–946 `parseTypeExpression` 递归（`[N][M]T`、`[][]T`）。**实现**（均仅改 `ps-parse-type`）：① IDENT 臂建 NamedType 前加 `go` 迴圈：`ps-peek==DOT && ps-peek-at(1)==IDENT` 则消费 `.段`、`nm = nm + '.' + seg`；② SliceType/ArrayType 臂的元素不再硬性要求单 IDENT，而是守卫「元素起 ∈ {IDENT, LBRACKET, QUESTION}」后【递归】 `elem = ps-parse-type(t,p)`（子先於父）；③ 各臂改用 `handled` 旗标（非互斥条件）防 match 非排他之跨臂重评。**级联**：`ps-parse-param`（`w io.writer`）、可為空含值臂 `nxt_typ==QUESTION`（`f ?fs.file = call`）均走 `ps-parse-type`→无需改动即自动受益。**含值复合形**：将 `ps-parse-typedecl-noval` 升级为通用 `ps-parse-typedecl`（新增 `hd==ASSIGN → 消费 `=`+`ps-parse-expr` → `LetStatement{name,type,value}` 分支），分派守卫仍限 `nxt_typ∈{IDENT,LBRACKET}`（QUESTION 交由可為空臂），因此只接住各特化含值臂遗漏的 `d tls.conn = call-tls()`、`outer [][]i64 = with-len(1)` 等形。**零回归结构保证**：限定/嵌套型此前 ps-parse-type 回 0⇒还原⇒这些档均已 NO，新能力只改善；孤兒节点不被 kid 引用⇒dumper 不触及。**效果**：全语料 **253 → 262（+9：tls/tls-part3/http2/hpack/fs-error-{final,info}/fs-open-err/nested-vec-test/push-narrow）**、差分回归 **0**。⚠️ 其余 3 档（fs-error-simple/std-new/net-client）复合类型已正确、但仍于**下游裸 match desugar / 空 StringLiteral / `{}` 结构字面量**再分歧（即下一个主簇）。⚠️ **未涵盖**：`?T`【无值】裸宣告（`x1 ?str`）仍由可為空臂（需 `=`）误吞——为避免触碰众多 option-* 档回归风险，留待下一个专门增量。 | `t45`（`c tls.conn`/`a [][]i64`/`z [16][]byte` 裸复合 / `f ?fs.file = call-fs()` 限定可空含值 / `d tls.conn = call-tls()` 限定含值 / 参數 `w io.writer, m [][]i64`、`k hpack.tables`） |
| **無色 async 前綴運算子 `run`/`co`/`awy`（RunExpression/CoExpression/AwaitExpression）** | 即 survey 揭示的 async 簇（10 檔首分歧即 `LetStatement{name: h, value: RunExpression}` 等）。探针实測：`h = run worker-async(100)` oracle 出 `LetStatement{name: h, value: RunExpression}` + `RunExpression` + `CallExpression{...}`；noc 因 `ps-parse-expr-prec` 无 RUN/CO/AWY 前缀臂⇒值解析為空（`value: `）、内层 `worker-async(100)` 漏成独立 `ExpressionStatement{CallExpression}`。同理 `r = awy h` ⇒ AwaitExpression{Identifier h}、`c = co child(n)` ⇒ CoExpression。**Go 规则（expr.go:305–357）**：三个前缀 case均在 `parseExpression` 的 prefix 分发——`run e`/`co e`：若 `peekToken==DOT` 则**不是** async 关键字而是模組名（`run.list(...)`）⇒建 `Identifier{lit}`、nextToken 停在 DOT 交由后缀 DOT 路径；否则消费关键字→`parseExpression(LOWEST)` 作内层→包 RunExpression{Call}/CoExpression{Call}。`awy e`：无 DOT 歧义，直接消费 awy→`parseExpression(LOWEST)`→AwaitExpression{Right}。**節點（ast_spec.json）**：RunExpression{Call}、CoExpression{Call}、AwaitExpression{Right}——均无标量、单一 guarded CHILD；K_RUN_EXPRESSION=57 / K_CO_EXPRESSION=20 / K_AWAIT_EXPRESSION=13 均已於 asp.no 注册⇒**纯 parser 缺臂**（无需改 ast.no/asp.no）。**实现**：在 `ps-parse-expr-prec` prefix match 块（NOT 臂后、LBRACKET 臂前）新增三臂；子先於父（先 `child = ps-parse-expr-prec(t,p,0)` 再 `ast.add` 父 + `kid-add`）。⚠️ `run`/`co` 的 DOT 歧义用**预先算好的 `dotAfter = ps-peek-at(p,1)==tok.DOT` 旗标**分两臂（`dotAfter==1` Identifier / `dotAfter==0` 包装），而非互补条件各自重取 ps-peek-at（match 非排他、首臂消费 token 会使后臂误触）。**零回归结构保证**：含 run/co/awy 的档此前必已 NO（值缺失）。**效果**：全语料 **262 → 273（+11：async-{cancel,coop,rc,yield,handle-alias}、module-async、slot-rebind-unsafe、mem-safety/{async-alloca-escape,async-module-awy,async-str-result,async-str-stress}）**、差分回归 **0**。⚠️ go-async.no 的 CoExpression 已正确、但仍于**下游行内註解附著（`b = co worker(20); // 註解`）/嵌套 BlockStatement**再分歧（即下一个主簇）。 | `t46`（`run worker(3)`/`co worker(4)`/`awy h` 三包节点 / `co.pool()` 模組名歧义→DotExpression{receiver: co} / 普通 `worker(5)` 不受影响） |
| **区块／程式尾注解 TrailingComments（BlockStatement + Program）** | 即 survey 揭示之 OTHER 桶——TrailingComments 20 档／TrailingComments 首分歧 11 档可纯 parser 翻转。探针实測：`for2.no`（仅两行注解、无语句）oracle 出 `Program{}` + `TrailingComments` + 两行 `// <text>`；`types.no` 文件头多行注解群同理。noc 因 `ps.no` 的 `parse`（Program）与 `ps-parse-block`（BlockStatement）把这些 standalone 注解喂给 `ps-skip-trivia-doc` 后丢弃⇒从不产生 TrailingComments。**Go 规则（stmt.go:2141）**：区块语句解完后 `block.TrailingComments = p.collectDocComments()`——取回全部剩余缓冲注解（再以 openBraceLine≤line≤closeBraceLine 过滤属于本区块者）；多注解群**全部**渲染（不同于 leading Doc 只印首条）。**节点（ast_spec.json）**：BlockStatement/Program 的 `TrailingComments *CommentGroup`——dumper 早已用 OP-TRAILING（`ast.no:1058-1081`，读 `d.ntrail`/`d.ct0`/`ast.aux`）完整支援⇒**纯 parser 缺口**（无需改 ast.no/asp.no）。**实现**（均仅改 `ps.no`）：新增三 helper——（a）`ps-trail-region(t,p)` 前瞻（不消费 token）判断跳完所有 NEWLINE/COMMENT 后首个实 token 是否 ∈ {RBRACE,EOF}（=处于尾注解区）；（b）`ps-collect-tail(t,p,prev,trail)` 消费尾区的 NEWLINE/COMMENT：同条陈述行尾的首个行内注解（`sawNL==0 && prev>0 && !cmtTaken`）仍走 `set-cmt`、其余全部 `trail.push(put-str)`；（c）`ps-attach-trailing(t,idx,trail)` 在 `trail.len()>0` 时走 `aux-begin`+逐个 `aux-add`+`set-aux-counts(ntrail=len, nclos=-1, nimp=0)`。在 `parse` 与 `ps-parse-block` 的语句循环【开头】与【尾】各做一次区域判断：region==0→照常 leading-doc（`docId`），region==1→`ps-collect-tail`；节点建完后 `ps-attach-trailing`。**零回归结构保证**：ntrail 默认 -1⇒`trailing-comments` renderer `d.ntrail>=0` 不成立而跳过；trail 空⇒不呼叫 attach⇒原匹配档输出字节不变。**效果**：全语料 **273 → 284（+11：src/std/types.no、tests/{arr,duplicate,for2,i,str,range-test}.no、mem-safety/{deep-free-nested-vec,deep-free-str,deep-free-struct-vec,minimal-str-map2}.no）**、差分回归 **0**（`corpdiff` M->N 列表为空）。⚠️ ClosingBraceComment（`}` 同行右的 `nclos`）尚未实现——本语料无此形，留待后续。 | `t47`（函式区块内陈述后的两条 standalone 尾注解→BlockStatement TrailingComments / 根层级最后陈述后的尾注解→Program TrailingComments） |
| **结构体栏位型别拓展 + 栏位尾注解跳过（ps-is-struct-def gate）** | 即 survey 揭示之 StructDefinition 桶（9 档首分歧）。探针实測：`matrix { rows [][]str; name str }`（嵌套切片栏位）与 `pool { conns [32]net.conn; ... }`（限定名阵列栏位）与 `node { next node #{inline=false} }`（栏位尾注解）——oracle 均出 `StructDefinition{name, fields: N}` + `StructField{name, type, arraySize/slice}`（且 `no ast` 无 -annotations **从不渲染栏位注解**）；noc 因 `ps-is-struct-def` gate 内手写的栏位型扫描器仅认 {IDENT, `[]IDENT`, `[INT]IDENT} 且不允许尾注⇒遇限定/嵌套/注解即回 0⇒整个结构退回 `ExpressionStatement{name}` + 内部被当作独立陈述。**关键**：body（`ps-parse-struct-def`）早已走 `ps-parse-type`（s41 已支援限定/嵌套）⇒**仅 gate 限**。**实现**（均仅改 `ps.no`）：（a）新增 `ps-skip-type-idx(p, i)`——以【索引前视】（不消费 token）完整镜像 ps-parse-type 四形（`a.b`（IDENT (.IDENT)*）、`?T`、`[]T`、`[N]T`（N=单 INT），元素递归∈{IDENT, [, ?}），合法回新索引、非法回 −1；用它取代 gate 内的 `okType` 手写臂。（b）新增 `ps-skip-annot-idx`（gate，索引前视）/`ps-skip-annot`（body，消费），以深度计数（`#{ [ ( {`→+1、`} ) ]`→−1）跳过栏位尾 `#{...}` 群。**零回归结构保证**：含限定/嵌套/注解栏位的结构此前必已 NO（gate 拒→ExpressionStatement ≠ oracle StructDefinition），新能力只改善；gate 与 body 共用同一型别语法⇒一致。**效果**：全语料 **280 → 283（+3：src/std/net/client.no、tests/field-{inline-annotation,tag}.no）**、差分回归 **0**（`corpdiff` M->N 列表为空）。⚠️ 其余 6 档（json/net-{http2,pool,sse,ws}/str-vec-push）结构已正确、但仍于**下游 standalone-if desugar / index-out desugar / 裸 match 区块**再分歧（即下一个主簇，LetStatement/IntegerLiteral/BlockStatement 三大桶）。 | `t48`（`next node #{inline=false}` 注解跳过 / `rows [][]str` 嵌套切片 / `conns [32]net.conn` 限定阵列 / `in-use [32]bool`） |

**路徑復刻（§5.3.2）的實測要點**（全部經 oracle 探針驗證，非推論）：

1. **型別推斷只對字面量合成 NamedType**：`x = 1 + 2`、`y = x` 的 LetStatement **沒有** Type 子節點；只有 INT→`i64`（但十六進位 `0x/0X` 形→`byte`）、FLOAT→`f64`、STRING→`str`、BOOL→`bool`、CHAR（雙引號）→`char` 字面量直接 RHS 才推。`x = f()` RHS 是 CallExpression 也 **不推**。
2. **kid 槽位佔位規則**（新坑，已入 §2.1.1 表）：LetStatement 的 spec CHILD 是 (Type, Value) 兩槽，缺 Type 時必須 `kid-add(t, idx, 0)` 佔位；IfExpression 同理三槽 (Condition, Consequence, Alternative)，缺 alt 時也要 push 0。
3. **`a = (…)` 歧義規則**（新坑，已入 §2.1.1 表）：`ps-is-func` 從當前 LPAREN 掃到配平的 RPAREN，再看下一個非 trivia token：`{`⇒函式、`(`⇒**回傳元組**（t26：不中斷、續掃下一群組直到它閉合再看是否 `{`）、其他⇒非函式。否則 `a = (x)` 會被誤判為函式定義並掛死；`c = add(1)(2)`（鏈式呼叫）因第二群組閉合後非 `{`仍正確判非函式。
4. **GroupedExpression / NilLiteral 的 dump 不帶括號／花括號**（探針實測）。
5. **CallExpression 的 `fn:` 使用 nodeString**（dump.go:904）：Identifier 時取 name，其他節點取 kind-name（例 `a.b(1)` ⇒ `CallExpression{fn: DotExpression, args: 1}`，因为 fn 的 nodeString 預設回 kind name）。
6. **args: 0 時 dump 不印**（kvc 政策 NON-ZERO）：slot0 存 `args.len().to-str()`，count slot0 同步 `args.len()`。
7. **`return 42` 在 Nolang 中是 parse error**（out-param 風气），不納入 M2 復刻目標。
8. **match 臂尾臂不互斥**（新坑，已入 §2.1.1）：同块 `cond -> { idx = A }  …  -> idx = B` 中，B 臂會**作為無條件尾隨陳述**再執行一次，覆寫 A 臂的 idx。`ps-parse-stmt` 的 IDENT 分支靠 `handled` 旗標隔開。
9. **單行 `-> { a; b }` 吃掉閉合 `}`**（§2.1.1 首行的實測復發）：SliceLiteral 元素迴圈若寫 `ps-peek(p) == tok.COMMA -> { ps-bump(p); ps-skip-trivia(p) }`（單行），`;` 會把 `ps-skip-trivia(p) }` 整段註解掉 ⇒ `{` 永不閉合 ⇒ 向後吞掉 `ps-parse-if` 等**所有頂層定義**，報錯卻指向遠端呼叫點：`EmitLLVM: unknown callee ps-parse-if in func ps-parse-stmt`。修正＝展開成多行（與既有的 call-args 迴圈同形）。
10. **ArrayLiteral / `[N]` 型別標註（已復刻）**：探針 `x [3]i64 = [1, 2, 3]`（明示）與 `x [3] = [1, 2, 3]`（推論）皆經 oracle 驗證 ⇒ `LetStatement{type: ArrayType{size:N}}` + `ArrayLiteral{size, elements, wasSliceLiteral: true}`。**兩关键路徑要點**（t39）：明示 `[N]T` 用 `NamedType{T}`（不帶 inferred）；**推論 `[N]`（無元素型別）時 Go 一律補 `NamedType{"i64", inferred:true}`、【不】依首元素 kind 推論**（bool/float/str 也出 `[N]i64`，stmt.go:957）；且 `ArrayType.IsInferred=false`（formatter 印回 `[N]`）；僅 `Size>0` 才轉 ArrayLiteral、`[0] = [..]` 保留 SliceLiteral（stmt.go:1210）。
11. **迴圈內多臂 match 的跳臂迭代不可靠**（新坑，已入 §2.1.1）：`ps-parse-expr-prec` 的後綴迴圈 `(pgo) { nt == A -> {…} nt == B -> {…} nt == C -> {…} -> {pgo=false} }`——只有兩個區塊臂時（LPAREN/DOT）跳臂迭代正常（t11 `a.b(1)` 同過），但**新增第三個區塊臂 LBRACKET 後**，某對跳臂轉移會誤編（表現在 `f(x)[2]`、`m[0][1]`、`a.b(1)` 三者中總有一個在第一臂後就退出迴圈，把後續 `[..]`/`(..)` 當成独立陳述），且**調整臂序只是把壞的轉移換了一個**。穩定修法：用 `handled` 旗標——每臂末尾 `handled = 1`，終止改寫為顯式陳述 `handled == 0 -> pgo = false`（與 §2.1.1尾臂坑的處理同構）。
12. **dump 標頭的 `size` 類欄位不占字串槽**（OF-NODE-STRING 機制）：`ast.no::scalar` 對 OF-TYPE / OF-NODE-STRING / OF-LOCAL(render=nodeString) 的純量，**從 child 现算** nodeString/typeString（ref 是 CHILD 序號）；只有 OF-FIELD/OF-LEN/… 才读字串槽 s0..。⇒ ArrayType/ArrayLiteral 的 `size` 由 child0（Size 節點）推得，**不要**為它 `set-slot`；只需為 bool/len 類欄位（inferred / elements / wasSliceLiteral）依序佔用字串槽。
13. **僅宣告無初始值 `e [3]i64`（無 `= …`）尚未復刻**（待辦）：oracle 對 `e [3]i64`（无 RHS）出 `LetStatement{name: e, type: [3]i64, value: }`（value 空）。當前分派守衛硬性要求 `+5==ASSIGN`，故此形落入 expr-stmt 誤解析為 IndexExpression。需放寬守衛（`] IDENT` 後無 ASSIGN 也走陣列宣告、value=0 佔位）——本增量**只釘住有初始值的 `[N]T = [..]`**。

**vet 現況（誠實記錄）**：`no vet compiler/src` ⇒ **0 ERROR / 0 WARNING / 123 hint**。ERROR=0 滿足 nolang-vet 硬門檻。⚠️ **基線遷移（2026-10-11）**：早期版本此處記錄的「11 WARNING」全為 `nolang-overflow-ineffective`（提示「此 `#{overflow=...}` 對應陳述不含可證 i64 整數運算，建議刪除」，針對 `x = x + intField` 類欄位算術——checker 無法證明 `intField` 為 i64（option/指標可能性）而誤報，非真實缺陷；舊分佈：`ast.no` 7、`drv.no` 2、`ps.no` 2）。**當前的 `bin/no` 已把該檢查由 WARNING 降為 HINT**，故現在計入 123 hint 之中、WARNING 歸 0——此為 oracle 端嚴重性重分類，**非本增量造成**（本增量只動 `ps.no`）。註解歸位增量新增的 `#{overflow=wrap}` / `#{index-out=zero}` 均對應真實 i64 算術或數組讀取，不新增 ineffective 項。

> ⚠️ 歷史上此處的 WARNING 計數會隨 oracle `bin/no` 對 `overflow-ineffective` 的嚴重性調整而變（曾為 0→未加入檢查、後 11→降為 WARNING、現 0→降為 HINT）；關鍵硬門檻始終是 **ERROR=0**。

⚠️ **有意保留的偏離**：`*_test.no`、`shs-check.no` 的檔名含下劃線，違反 `fn-rule` 檔名 lint——但這些檔名由本設計文檔（§6 M0/M1 驗收證據）直接指定，屬文檔與 lint 的衝突，以文檔為準。

**可重跑驗證（本增量的全部證據）**：

```sh
./bin/no build -o /tmp/noc-s0 compiler                     # Build successful
for f in compiler/tests/fixtures/*.no; do                  # t0–t48 全部逐位元組相同（原既有失敗 t32（`(expr) { }` cond-loop⇒ForStatement）已由 s38 bang/cond-loop 增量修復）
  diff <(./bin/no ast "$f") <(/tmp/noc-s0 ast "$f") && echo "SAME $f"
done
./scripts/selfhost_diff.sh --self-test --levels L0,L2 --limit 30 --sut /tmp/noc-s0   # L0 4/4・L2 30/30・PASS
./bin/no run compiler/tests/{ast,lx,tok,util}_test.no       # 44・45・62・27 全 PASS
./bin/no vet compiler/src                                   # 0 ERROR・0 WARNING
```

⚠️ **超時包裝的坑**（本節命令若改寫成自製 harness 時必讀）：用 perl `fork`+`waitpid(WNOHANG)` 包裝時，被 SIGKILL 的進程 `$? >> 8` 是 **0**，會把掛死誤報成成功；必須用顯式 `$to` 旗標（或 `perl -e 'alarm N; exec @ARGV'`）回報 124。

### 6.4 M3 啟動盤點（2026-10-27）

M2 收官（見 §6 表下方「M2 收官」段）後下一里程碑為 **M3 語義檢查**（§5.4）。本節記錄啟動摸底，作為 M3 首增量的選型依據：

- **SUT 現狀**：`drv.no` 僅有 `version` / `tokens` / `ast` 三個子命令，**無 `vet`**；`compiler/src/` 無 checker 模組（`ck*.no` 不存在）、無 `ck-sig.no`（stdsig 表）。`/tmp/noc-s81 vet <file>` 落 usage 分支 ⇒ **L3 誠實基線 = SUT 整體缺席（UNAVAILABLE/全 FAIL），不是 PASS**（§7.5 G7 同源教訓）。
- **門禁基建已就緒**：`scripts/selfhost_diff.sh` 的 L3 段（`run_diff_level L3 vet` + 歸一化 + `[ERROR]` 集合硬判據 + G5 oracle 穩定性守衛）早已接線（§7.3），M3 的每一步都能立刻上差分。
- **vet 基線漂移註記**：§6.3 舊證據块的「0 ERROR」是各增量大当期快照；以當代 `bin/no` 量測，`no vet compiler/src` 現為 **19 ERROR**（皆 `nolang-index-write` 族，集中在 ps.no 既有 `out[i] = …` 形），為 M3 之前的既有基線、非 L2 軌引入，留待 M3 期間一併清償或標記 grandfather。
- **§5.4 四大復刻點**（工作量主體）：
  1. **診斷三通路**（`no build` 硬錯誤列表 / `no vet` 的 `RunAllLints`+`SkipTypeChecks` / LSP 原始程式）——noc 側需先立 `diag.no`（§5.8 診斷模型），再按通路接線；
  2. **stdsig 表遷移**：`src/checker/stdsig_gen.go`（859 funcSigs / 1,983 methodSigs）→ `compiler/src/ck-sig.no`，用一次性生成工具（`compiler/tools/`）讀 std 生成，**不在 `noc` 啟動時重收集**；
  3. **158 處 `#{buildin}` 樁的三條載入路徑**——s52 已在 AST 面復刻 `BuiltinStub`/`BuiltinName` 旗標（slot5/6/7），M3 只需消費端跳過樁，是三條路徑守衛的既有地基；
  4. **診斷輸出兩種形狀**（`<file>:<line>:<col>: [ERROR|…]` 與 `<channel>: line L, column C: …` 串），§7.3 歸一化依賴此契約，**不得發明第三種**。
- **建議分期**（沿 M2 的增量方法論）：M3-a＝`diag.no`+`drv.cmd-vet` 骨架（連詞法/語法錯誤也走診斷形狀，讓 L3 先能跑、誠實報 FAIL）→ M3-b＝stdsig 生成器＋簽名表 → M3-c＝`Validate*` 按語料觸發頻度逐族增量（L3 門禁 = `[ERROR]` 集合 diff 收斂 + 0 回歸）。
- **M3-a 已完成（s9a，§5.3.4.am）**：`vet` 子命令落地，但範圍較原計劃有一處誠實縮減——ps.no 目前**不產錯**，故「詞法/語法錯誤也走診斷形狀」未隨骨架交付，現狀是「契約齊全、診斷源為空」：L3 harness 如預期報 **G8 MASS-NOOP**（護欄生效，非 PASS）。同輪 oracle 側盤點給出 M3-c 的真實工作面（樣本 40 檔：38 檔有診斷，ERROR/HINT/WARNING=594/10001/296，code 族 9 種），比 §5.4 的「37 個 Validate*」計數更寬。下一增量建議 M3-b（stdsig 生成器），或先行給 ps.no 接「第一條真診斷」（語法錯誤收集器），二者皆可立刻上 L3 差分。

---

## 7. 驗證策略（差分矩陣）

### 7.1 五級門禁總表

| 級      | 對象                    | Oracle                          | 判據                                        | 成本           |
| ------ | --------------------- | ------------------------------- | ----------------------------------------- | ------------ |
| **L0** | `compiler/tests/*_test.no` | `test/runner.no`（測試檔自檢） | 全綠；⚠️ 測試檔必須自己 `f-count > 0 -> exit(1)`。⚠️ 執行方式必須是 **`no run <file>`**：`no <file>` 只印 usage 且 rc=0 ⇒ 每個測試檔的 stdout 都相同 ⇒ **G7 判 oracle 無鑑別力** | 秒            |
| **L1** | 詞法                    | `<l1-ctl> tokens`（**repo 外只讀 dumper**，§7.1.1） | 全語料 token 流逐位元組一致                         | 分鐘           |
| **L2** | 語法                    | `no ast`                        | 全語料 AST dump 逐位元組一致                       | 分鐘           |
| **L3** | 語義                    | `no vet`                        | 歸一化後逐行一致 + `[ERROR]` 集合 diff 為空           | 分鐘           |
| **L4** | 行為                    | `tests/golden/mir-baseline.tsv` | `REGRESS = 0`；所有 DIVERGE 逐一歸因             | **~7–17 分鐘** |
| **L5** | 自舉                    | s2 vs s3                        | 行為定點 + （加分）逐位元組定點                         | 分鐘           |

#### 7.1.1 L1 的 oracle：**repo 外的只讀 dumper**（繞過 P0-②）

**問題**：L1 要一把能吐出「規範化 token 流」的尺子。文檔原訂靠 **P0-②（新增 `no tokens` 子命令）**，但那需要改 `src/`。而自舉的 M0/M1 階段有一條硬約束：**`src/` 不得改動**（`no` 必須始終是一把可信、可回歸的尺子，DR-5）。

**繞法**：把 oracle 放在 **repo 之外**（`/tmp/notok/`），它是一個 **Go** 程式，用 `replace` 指令 `import` 同一個 `src/lexer` 包——也就是說，**P0-② 若實作也會走同一個包**，兩者的判準天然一致。它提供與 SUT 相同的 CLI 形狀：

```
notok tokens <file>          # `tokens` 被忽略，只為了與 SUT 共用同一組 argv
```

輸出每行一個 token：`<line>:<col> <TYPE> <esc(literal)> <esc(raw)> <esc(marker)> <esc(err)>`，其中 `esc()` 把 `\`→`\\`、`\n`→`\n`、`\t`→`\t`、`\r`→`\r`、位元組 `<0x20` 或 `≥0x7f` → `\xHH`（小寫），空字串 → `-`。**必須轉義**：不轉義的話，含換行的 raw string 會讓「一個 token」跨成「多行」，行數就對不上。

**接線**：`scripts/selfhost_diff.sh --l1-ctl <path>`。**沒給、或路徑不可執行** ⇒ L1 誠實報 **UNAVAILABLE**（退出碼 4），**不是 PASS**。

**兩個必須有的守衛**（否則 L1 會靜默變成橡皮圖章）：

| 守衛          | 觸發條件                                            | 為什麼需要                                                                                                          |
| ----------- | ----------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| **缺 oracle** | `--l1-ctl` 未給 / 不可執行                              | `no` **沒有** `tokens` 子命令 ⇒ 不能拿 `bin/no` 當尺子。                                                    |
| **G7（無鑑別力）** | 控制組在 **≥3** 個檔上只吐 **1 種** stdout 雜湊               | 實測踩過：`bin/no tokens <f>` 對**任何**輸入都印同一份 usage，**rc=0**、stdout 非空 ⇒ G1/G2/G3 **全都攔不住**，會被記成「SUT 與控制組不同」的 **FAIL**（把責任推給 SUT）。G7 改記 **UNAVAILABLE**。 |

**自檢契約的修正**：`--self-test` 在 L1 這一格必須把 `SUT` 設成 **`$L1_CTL`**（L1 的尺子），而不是 `bin/no`——用兩把**不同的**尺子量同一批檔當然會「不同」，那會在自檢裡誤報 FAIL。

**P0-② 的處置**：仍然要做，但**不再阻塞 M1**。它的價值在於「`noc` 能自己當 oracle」（自舉後 `noc` 必須能產出 `no` 的那份 token 流，不能永遠依賴一個 repo 外的 Go 程式），以及 §8 的「同一 binary 跑 3 次雜湊一致」確定性驗收。

### 7.2 L4 的正確用法（repo 已有基建，直接複用）

```bash
# 一次 sweep（⚠️ 絕不並行跑兩次：$WORKDIR 是硬編碼的 /tmp/mir_golden）
GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default bash scripts/mir_golden.sh > /tmp/g.log 2>&1
```

**開跑前的 sanity check（每次必做）**：

```bash
./bin/no run tests/2.no; echo $?     # 必須是 0
pgrep -fl mir_golden                 # 必須為空
stat -f %Sm bin/no                   # 記錄 mtime：若 sweep 期間被別的 session 重建，本次 sweep 作廢
```

**收尾後的自檢**：`SAME + DIVERGE + REGRESS + IMPROVED + BOTH_FAIL + NEW == 語料檔案數`，且任一 bucket 內不得出現重複路徑。

**最強的 A/B 形式**（不要 `diff fp.txt other.txt`，行序非確定）：

```python
load = lambda p: {l.split(' ', 2)[2].strip(): tuple(l.split(' ', 2)[:2]) for l in open(p)}
a, b = load('/tmp/fp_ctl.txt'), load('/tmp/fp_noc.txt')
diff = [k for k in sorted(set(a) | set(b)) if a.get(k) != b.get(k)]
print(len(a), len(b), len(diff))
```

### 7.3 L3 的歸一化（`no vet` 的**路徑前綴不一致**）

> **訂正（2026-10-09，`HEAD=f1cc56d3` 重測）**：本文早期版本聲稱 `no vet` 對導入 std 的檔案會打印 ~290 條 HINT、且**歸屬逐次不同**（「8 次跑出 5 個 stdout 雜湊」）。**該現象在當前樹上未複現**——同一個 binary、同一個輸入，`no vet` 是**確定**的：

```
$ for i in 1 2 3 4; do ./bin/no vet src/std 2>/dev/null | shasum -a 256; done
92a6a19a0b6d3588…      ← 4/4 逐位元組相同（147 條 HINT）
```

HINT 總數也已由 ~290 變為 **147**（std 已變動）。**不要把「未複現」寫成「已修好」**：它可能只是當年的輸入不同。但至少在當前樹上，L3 **不需要**「因非確定性而排序」這一步。

**仍然必須歸一化**——但理由是**路徑前綴不一致**（不是非確定性）：同一份輸出裡，目錄**頂層檔**報告為**相對路徑**，而被當作**依賴載入**的模組報告為**絕對路徑**。實測（`no vet src/std`，147 行）：

| 前綴                                   | 行數  | 來源                                    |
| ------------------------------------ | --- | ------------------------------------- |
| `src/std/…`                          | 143 | `src/std` 下**直接被 vet** 的檔             |
| `/Users/…/no/src/std/str.no`         | 4   | `str.no` —— 以**依賴**身分被載入（故為絕對路徑）      |

⇒ 兩支 binary（或兩台機器、兩個 checkout）只要對同一模組走了不同的載入路徑，就會產生**假 DIFF**。歸一化（保留 `line:col: message`，丟掉檔名與目錄）：

```bash
norm() { sed -E 's#^[^ ]*/([^/ ]+):([0-9]+):([0-9]+): #\2:\3: #'; }
```

歸一化後 `line:col: message` 穩定，且**跨 binary / 跨 checkout 可比**。**並且**：A/B 的兩支 binary 必須以**相同方式**構建（`make no` vs `make no`），否則 `main.version` 的差異會讓**每個檔案**都吐一行 `warning: compiler version mismatch`，造成 **100% 假 DIFF 而 stdout 逐位元組全同**。

### 7.4 L1 / L2 的「失敗集合」也要比

只比「成功的檔案是否一致」是不夠的。必須**同時**比：

- **解析失敗的檔案集合**（兩邊必須完全相同）；
- **失敗訊息的位置**（line/col）。

這是唯一能抓出「`noc` 對某個語法過度寬鬆」的方法。

⚠️ **還是要印出 oracle 的 `<unknown>` 計數**（§5.3.1）：`no ast` 已補齊，**基準值是 0**。因此這行現在的角色**反轉**了——它從「警告盲點」變成**回歸偵測**：

> 語料 528 檔的 dump 中出現 **任何** `<unknown>` ⇒ **有節點型別沒被 dump**（通常是 parser 新增了節點而 `dump.go` 沒跟上）⇒ oracle 在**那一個子樹上**已退化，L2 綠燈會靜默地比實際更強。

`dump_completeness_test.go` 會在 `go test` 時就擋住這種退化；harness 的這一計數是第二道（且涵蓋 harness 自己的語料範圍）。

### 7.5 差分的假通過偵測（**每一個自製 harness 都必須有**）

歷史教訓：一個 `PATH` 少了 `llvm`/`clang` 的 harness 會讓**每個檔案** `rc=1` + 空 stdout，然後「0 differ」看起來像完美結果（實測 97/97「完全一致」而實際上什麼都沒跑）。

**每個 harness 的結尾必須打印**：

1. **控制組 `rc==0` 的檔案數**——≈0 ⇒ harness 壞了，整個比較作廢；
2. **`sha256(stdout) == e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`（空字串雜湊）的檔案數**——偏高 ⇒ 什麼都沒執行；
3. **控制組與被測組各自的非空輸出數**。

**實例（2026-10-10，G7 抓到的真 bug）**：L0 的呼叫一度寫成「直接跑 `<bin> <file>`」（子命令用 `"-"` 佔位）。但 `no <file>` **不是**「執行」——它印出 usage 並 **rc=0**。於是三個測試檔的 stdout 完全相同（同一份 usage），差分器看到的是「三檔一致、rc=0」，**看起來完美**。

```
$ ./bin/no compiler/tests/tok-test.no | shasum -a 256     # 錯的呼叫
febadc57a53b79d8a913838c9afed6d30f16736dfcc6ec3d1e20f8264442b718
$ ./bin/no compiler/tests/lx-test.no | shasum -a 256      # 同一個雜湊！
febadc57a53b79d8a913838c9afed6d30f16736dfcc6ec3d1e20f8264442b718
```

G7（控制組在 ≥3 檔上只吐 1 種 stdout 雜湊 ⇒ UNAVAILABLE）把這輪擋下來了。**若沒有 G7，L0 會一直是綠燈而什麼都沒測。** 修法：子命令改成 `run`。

**G8（本節新增）：診斷訊息本身也可能是「靜默的謊」。**（2026-10-10 實測）

harness 在 L1 無 oracle 時要印一段說明，其中三行原本寫成**雙引號內含反引號**：

```bash
echo "  用法：--l1-ctl /path/to/dumper   （必須支援 `<dumper> tokens <file>`）"
```

雙引號內的反引號是 **命令替換**，於是這行不是「印出說明」，而是**去執行** `` `<dumper> tokens <file>` ``——`<` `>` 被當成重導向 ⇒ bash 報 `syntax error near unexpected token 'newline'`，該行輸出變成殘缺的 `（必須支援 ）`。另兩行的反引號則會**真的執行 `no`** 與 `bin/no tokens <f>`。

**教訓**：診斷訊息是 harness 的**輸出**，它自己也可能壞掉，而壞掉的方式恰好是「看起來像正常輸出」——與 G7 同一類。⇒ 修法＝這三行改用**單引號**；並在 `bash -n` 之外，**實際跑一次 UNAVAILABLE 路徑**（`--levels L1` 不給 `--l1-ctl`）確認訊息完整。

⚠️ 注意 `bash -n` **抓不到**這個錯誤（它只檢查語法，而雙引號＋反引號本身語法合法）；只有**執行**到那一行才會炸。⇒ 對「只在失敗路徑才會印」的訊息，必須**主動走一遍失敗路徑**。

### 7.6 SHS 守護（`tools/shs-check.no`）

對 `compiler/src/**/*.no` 做**編譯器自身的解析**（用 `noc ast`，不是正則掃描），檢查：

| 檢查                 | 說明                                           |
| ------------------ | -------------------------------------------- |
| 無一等函式值             | 不得出現 `name = (params) (results)` 形式的具名函式型別定義 |
| 無指針 / FFI          | 不得出現 `*T` 型別、`#{c}` 註解                       |
| 無匿名函式型別            | 不得出現 `cb ()()` 形式                            |
| 無模組級可變 `let`       | 白名單（只讀常量表）由檔案頭註釋聲明                           |
| 無 `any` / `obj`    | —                                            |
| 所有位元組讀取用 `.byte()` | 啟發式：`s[...]` 出現在以 `.len-bytes()` 為界的迴圈中 ⇒ 警告 |

⚠️ **不要用 Python/正則寫這個掃描器**。Nolang 有**三種註釋形式**（`;` / `//` / `;;…;;`），且 `/` 是上下文敏感的（正則字面量 vs 除法）。一個只認 `//` 與 `/* */` 的掃描器會把 `;` 註釋裡的中文當成程式碼（實測：442 個檔案報出 17 個假陽性，而真正的編譯器報 0）。

---

## 8. 前置改動清單

> 這些是**對現有 `no` 的改動**，全部走 `nolang-compiler-change` 技能的流程（控制組、A/B、golden）。它們不屬於自舉本身，但**不做的話自舉會寸步難行**。

| ID       | 改動                                                                                               | 優先        | 影響                                                                                                                             | 驗收                                                             |
| -------- | ------------------------------------------------------------------------------------------------ | --------- | ------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------- |
| **P0-①** | `package.jsonc` 的 `compiler` 區塊新增 `"overflow": "wrap"`，作為**包級默認**（現有的 `#{overflow=…}` 敘述級註解仍然優先） | 🔴 **最高** | 否則編譯器源碼每行算術都要註解（§2.2.2）                                                                                                        | 新鍵生效 ⇒ 一個含 10 條算術的函式在無註解下編譯通過；**且**現有語料的 `(rc, stdout)` 不變（L4） |
| **P0-②** | 新增 `no tokens <file>` 子命令                                                                        | 🔴 高      | M1 的 oracle。⚠️ **已由 §7.1.1 的 repo 外只讀 dumper 繞過**（M1 因此不必先改 `src/`）；② 的剩餘價值＝**自舉後 `noc` 能自己當 oracle**，不再依賴外部 Go 程式                                                                                                                    | 對語料輸出確定性 token 流；同一 binary 跑 3 次雜湊一致                           |
| **P0-③** | `no ast --annotations`（或 dump 中加入註解節點）                                                           | ✅ **已完成** | M2 的註解覆蓋                                                                                                                       | ✅ 語料 **287 條 / 88 檔**註解進入覆蓋；預設行為不變（`TestNoAnnotationsByDefault`）；`dumpNode` 同時由 40 → **71** 個 case，語料 `<unknown>` **319 → 0**（§5.3.1） |
| **P0-④** | **一等函式值修復**（§2.2.1 的 6 種崩潰形狀）                                                                    | 🟠 中高     | M7 的語料完整覆蓋；`noc` 作為「完整編譯器」的必要條件                                                                                                | 6 種形狀全部正確；`tests/fn-type-*.no` 全綠；全語料 L4 無 REGRESS             |
| **P0-⑤** | **模組頂層綁定的生成條件**：改為「模組被引用即生成」，而非「被**入口檔直接** import 才生成」（§2.2.8）                                  | 🔴 **最高** | 否則跨模組常量在間接依賴下**靜默為 0**；作為運算式運算元時**硬編譯失敗**。這是自舉期間**唯一**必須靠人工記住「回入口檔補一行」的反組合約束 | 最小重現（§2.2.8）三種配置全部輸出 `42`；`compiler/main.no` 移除那兩行未使用 import 後仍正確；全語料 L4 不變 |
| **P0-⑥** | `fs.read-str` 的**逐位元組無損**讀取（新增 `fs.read-str-bytes` 或讓呼叫端統一走 `read-bytes` + `to-bytes-str`）（§2.2.9）    | 🔴 高      | 詞法分析器必須看到**原始位元組**；否則所有非 ASCII 註解／字面量被二次編碼，且 `rc` 測不出來                                                                          | 讀 5 位元組檔 `len-bytes()` 回 5；全語料 L1 無 DIFF                                 |
| **P0-⑦** | `src/lexer/lexer.go` 的 `readRawString` **回捲是空操作**（`LexerState` 未存字元位置）⇒ 每個續行吃掉前導空白**再加一個字元**                  | 🟡 低      | 目前語料**不可達**（1584 檔中 0 個真正的 raw string 字面量）；但 `noc` 的 L1 必須與 `no` **逐位元組**相同 ⇒ 必須二選一：修 `no` 並同步 `noc`，或**照抄缺陷**  | 決定後寫進 `compiler/src/lx.no` 檔頭；附可重跑重現；全語料 L1 無 DIFF                          |
| **P1-①** | 解析器棧深的實測與（必要時）迭代化                                                                                | 🟠 中      | M2 的健壯性                                                                                                                        | 量測報告 + 極限嵌套測試                                                  |
| **P1-②** | `std` 新增字串構建器（或 `compiler/util/sb.no` 先做，M7 再上收）                                                 | 🟡 低      | M4 效能                                                                                                                          | 1 MiB push 的 RSS 平線                                            |
| **P1-③** | `no ast` / `no tokens` 的輸出**確定性**保證（arena 順序而非指標順序）                                              | 🟠 中      | L2 的可比性                                                                                                                        | 同一 binary 跑 3 次逐位元組一致                                          |
| **P1-④** | `std` 補上 `chmod`（`std/os.no` 或 `std/fs.no`）                                                      | 🟡 低      | M5 驅動層：`no` 用 `os.Chmod(out, 0755)`，但 **nolang std 目前沒有 chmod**（`grep -rn chmod src/std/` 為空）。多數情況下 clang 產物自帶執行權限，但快取/複製路徑會丟失 | `fs.chmod(path, mode)` 可用；全語料 L4 不變                            |
| **P1-⑤** | `fs.read-bytes` 對 **0 位元組檔**回 `nil`（應回 `ok` + 空切片，或另加 `fs.read-bytes-or-empty`）（§2.2.10）             | 🟡 低      | 「讀到空」與「讀失敗」無法用回傳值區分（`nil` vs `err`）⇒ 每個呼叫端都要自己記住這條反直覺規則；漏了就是 rc 層級的假 DIFF（`noc tokens <空檔>`）         | 0 位元組檔回 `ok` 且 `len()==0`；不存在的路徑仍回 `err`；全語料 L1 不變                     |
| **P2-①** | `noc` 的 `-native` 對接（本方案不做，留給 nocg/nold 設計）                                                      | ⚪ 低       | 去 LLVM 依賴                                                                                                                      | —                                                              |

---

## 9. 風險登記

| ID      | 風險                                                                                                                          | 機率 | 影響 | 緩解                                                                                          |
| ------- | --------------------------------------------------------------------------------------------------------------------------- | -- | -- | ------------------------------------------------------------------------------------------- |
| **R1**  | **規模失控**：12 萬行 Go 移植，實際 nolang 行數可能更多                                                                                       | 高  | 極高 | 8 個里程碑各自可獨立驗收；M4 用 RC 砍掉 ~40% 的 `src/mir`；M8 明確排除 fmt/lsp/tools                             |
| **R2**  | **效能不可接受**：`noc` 比 `no` 慢 5–10×，導致開發循環崩潰                                                                                    | 中高 | 高  | 開發期**始終**用 S0；`noc` 只在門禁與自舉時跑；M7 才做效能；先量測再優化（**≥3 次取樣**，repo 的歷史顯示單次取樣無法分辨 1.5 s 的效應）       |
| **R3**  | **RC 的 UAF**：漏一個 retain 就是 UAF（不是洩漏）                                                                                        | 中  | 極高 | `check-rc-balance` 驗證器**只檢查欠計數方向**；**必須證明它會壞**（人為刪 retain ⇒ 它必須報錯）；每個里程碑跑全語料                |
| **R4**  | **把「非確定輸出」誤判為無害**：實測過一次——`tests/slice-heavy.no` 的輸出逐次不同，曾被記為「ASLR 位址，不可歸因」，實際上它**讀了已釋放的緩衝**（輸出裡赫然是 `@nolang_rc_alloc` 的頭魔數） | 中  | 高  | 差異檔案**必須讀輸出**；「逐次不同」只證明「非確定」，**不證明「無害」**。判據：看起來像位址/魔數/可變長度 ⇒ **記憶體安全缺陷**，去找它                |
| **R5**  | **並行 session 污染**：repo 常有其他 session 的未提交改動                                                                                  | 高  | 中  | 動手前**鎖 SHA**；A/B **兩邊都從同一份 `rsync` 快照構建**（相隔幾分鐘各建一支不保證配對）；**未經確認不得 add/commit/tag/push**    |
| **R6**  | **逐位元組定點不可達**：Go 的 map 迭代順序非確定，`codegen` 的清理釋放順序隨之變化（repo 已實測：同 binary 5 次跑出 5 個不同的 dump 雜湊，而**程式的 stdout 逐位元組穩定**）         | 中  | 低  | 行為定點是**硬性**要求；逐位元組定點是**加分**項。**永遠不要 hash MIR dump 做 A/B**；要比就比 `clone` 指令數或程式的 stdout       |
| **R7**  | **棧溢位**：`alloca` 必須在 entry block ⇒ 每個遞迴框架佔用「函式全部區域變數之和」                                                                     | 中  | 中  | M2 內實測；必要時迭代化最深的遞迴（表達式的括號/後綴鏈）                                                              |
| **R8**  | **`#{buildin}` 樁的三條路徑**：漏一條 ⇒ 靜默錯值，且 `rc` 測不出來                                                                              | 中  | 高  | M3 的明確驗收項；**必須 hash stdout**；掃描樁時**先剝 `;` 行註解**（否則 `database/sql.no:19` 的註釋會把模組數從 15 算成 16） |
| **R9**  | **跨模組前綴的維護成本**：每次跨檔調用都要 `ShortName.`                                                                                        | 高  | 低  | 模組名 ≤3 字元；高度耦合的階段同檔；M0 定死切分                                                                 |
| **R10** | **信任鏈（Thompson）**                                                                                                           | 低  | 極高 | §3.2 的三重反制；文檔中**明確記錄**「S0 是 TCB」                                                            |

---

## 10. 開放問題（需拍板）

| #      | 問題                                           | 選項                                     | 建議                                                               |
| ------ | -------------------------------------------- | -------------------------------------- | ---------------------------------------------------------------- |
| **Q1** | S1 是否允許「部分借用 S0」？例如 `noc` 只做前端，IR 生成仍調用 `no` | (a) 完全不借用 (b) 允許 M1–M3 只做前端、用 `no` 出後端 | **(b)** 作為 M1–M3 的臨時驗證手段：讓 `noc ast` 先可用，提早拿到 L2 門禁；但 M4 必須自產 IR |
| **Q2** | RC 還是精確所有權作為 M4 的目標？                         | (a) RC 先行 (b) 直接移植 `analysis.go`       | **(a)**：見 §5.6.1                                                 |
| **Q3** | 是否移植 `src/fmt`？                              | (a) 移植 (b) 用 S0 的 `no fmt` 外部完成        | **(b)**；`fmt` 與自舉無關                                              |
| **Q4** | 語料範圍：全 527 tests 還是「SHS 相容的子集」？              | (a) 全語料 (b) 分階段擴大                      | **(b)**：M1–M3 全語料（便宜）；M4 先 SHS+std，再逐步擴大                         |
| **Q5** | 逐位元組定點（R6）是否作為 M6 的硬性驗收？                     | (a) 硬性 (b) 加分                          | **(b)**                                                          |
| **Q6** | `noc` 是否要暴露 `no nocg` / `no nold` 這類調試子命令？   | —                                      | 與 `nolang-native-backend.md` 的 Q5 合併討論                           |
| **Q7** | S0 的版本鎖定策略                                   | (a) 鎖 `HEAD` SHA (b) 鎖 tag（`v0.3.19`）  | **(a)** 用於門禁，(b) 用於對外發布                                          |

---

## 附錄 A：實測證據與可重跑命令

> 以下每一條都在 2026-10-09、`HEAD = 4feaaf70`、`bin/no`（`make no` 產物）上實測。**重跑前先 `export PATH`**——Bash 預設**沒有 `go`**，且 `no` 需要 LLVM/clang 在 PATH 上。

```bash
export PATH="/opt/homebrew/bin:/opt/homebrew/opt/llvm/bin:/Library/Developer/CommandLineTools/usr/bin:/usr/bin:$PATH"
```

### A.1 規模計數


```bash
# Go 各包 LOC（非測試）
for d in src/*/; do
  l=$(find "$d" -name '*.go' -not -name '*_test.go' -exec cat {} + 2>/dev/null | wc -l)
  printf "%-16s %s\n" "$(basename $d)" "$l"
done
# → lexer 1350 / parser 22313 / checker 22739 / hir 1950 / mir 43177 /
#   build 17964 / builtin 2423 / package 2223 / cmd 4607 / cache 106 / fmt 3109 / lsp 6424 / tools 2690

find src/std -name '*.no' | wc -l                 # 118
find src/std -name '*.no' -exec cat {} + | wc -l  # 75154
find tests -name '*.no' | wc -l                   # 527
wc -l < tests/golden/mir-baseline.tsv             # 488

# 92 個 TokenType
python3 -c "import re;s=open('src/lexer/token.go').read();m=re.search(r'const \((.*?)\n\)',s,re.S);print(len(re.findall(r'^\t([A-Z][A-Z0-9_]*)\b',m.group(1),re.M)))"
# parser 套件「所有」struct = 96（⚠️ 含 Parser / lowerer / SemanticContext / Diagnostic 等
# 內部機械，**不是** AST 節點數。早期版本誤把 96 當成節點數，使 M2 的範圍被高估約 35%）
grep -rhoE '^type [A-Za-z]+ struct' src/parser/*.go | sort -u | wc -l

# 真正的 AST 節點定義全在 ast.go：78 個 struct
grep -cE '^type [A-Za-z]+ struct' src/parser/ast.go
# 其中 71 個實作 Node（Pos+EndPos）：18 Statement + 31 Expression + 9 Type + 13 無 marker
python3 scripts/ast_inventory.py   # 完整清單見 docs/design/nolang-ast-grammar.md
# 67 個 MIR op
awk '/OpInvalid|OpNop/,0' src/mir/mir.go | grep -oE 'Op[A-Z][A-Za-z]+' | sort -u | wc -l
# 37 個 Validate*
grep -rhoE '^func Validate[A-Za-z]+' src/checker/*.go | sort -u | wc -l
# 158 處 #{buildin}
grep -rhoE '#\{buildin\}' src/std/ | wc -l
```

### A.2 編譯耗時（含 clang 鏈接）

```bash
time ./bin/no build -o /tmp/t1 tests/str-test.no   # 706 行 → 4.12s user / 3.97s wall
time ./bin/no build -o /tmp/t2 tests/yaml.no       # 517 行 → 5.67s user / 5.52s wall
```

### A.3 語言能力探針（全部是**可重跑**的最小重現）

```bash
run(){ cat > /tmp/p.no; ./bin/no run /tmp/p.no 2>&1 | head -3; }
```

| #     | 探針                                              | 結果                                                                      |
| ----- | ----------------------------------------------- | ----------------------------------------------------------------------- |
| A3-1  | arena + 遞迴型別 + map + sb                         | ✅ 全部正確                                                                  |
| A3-2  | `f cb = inc` + `f(41)`                          | ❌ `signal: trace/BPT trap`                                              |
| A3-3  | `pick() (r cb) { r = inc }`                     | ❌ `opt-verify: use of undefined value '@r'`                             |
| A3-4  | `twice(f,v){ apply(f, apply(f,v)) }`            | ❌ `opt-verify: use of undefined value '@f'`                             |
| A3-5  | fn 值存入結構體欄位後調用                                  | ❌ `EmitLLVM: unknown callee node.fn`                                    |
| A3-6  | fn 值存入 map 後調用                                  | ❌ MIR 降級失敗                                                              |
| A3-7  | 匿名函式字面量作為引數                                     | ❌ parse error                                                           |
| A3-8  | 區域函式定義 / 閉包捕獲                                   | ❌ `'f' is not defined; call it as 'des.f'`                              |
| A3-9  | `apply(inc, 41)`（最平凡形狀）                         | ✅ `42`                                                                  |
| A3-10 | 區塊級 `#{overflow=wrap}`                          | ❌ 只作用於緊跟的一條敘述                                                           |
| A3-11 | 同目錄另一檔案的函式，無 `#` 導入                             | ❌ `'greet' is not defined`                                              |
| A3-12 | `# /src/util.greet greet`（workspace 根相對）        | ✅ `hi world`                                                            |
| A3-13 | 結構體名 `h`（單字元）                                   | ❌ `EmitLLVM: unsupported value type %h`（24 個單字元名全數失敗；`hh` / `holder` ✅） |
| A3-14 | `[str]i64` 的 `get` 缺失鍵                          | 回 `nil`（`?V`），**不是** 0                                                  |
| A3-15 | option match 只寫 `ok` / `nil`                    | ❌ `option match must handle all branches: err, nil, ok (missing: err)`  |
| A3-16 | `sb`（`[]byte` 構建器）+ `intern`（`[str]i64` 駐留）完整樣板 | ✅ `hello world` / `11` / `1`                                            |

### A.4 已知的觀測陷阱（**寫進 harness 的註釋**）

| 陷阱                              | 表現                                                                     |
| ------------------------------- | ---------------------------------------------------------------------- |
| `no build FILE -o OUT`（flag 在後） | **靜默不產出，rc 仍 0**                                                       |
| `no vet` 輸出                     | 逐次不同（HINT 歸屬漂移），排序無法修正                                                 |
| `make no` vs 裸 `go build`       | `main.version` 不同 ⇒ **每檔一行** `compiler version mismatch` ⇒ 100% 假 DIFF |
| `no ast`                        | **不打印註解**                                                              |
| MIR dump                        | `drop` 順序非確定（Go map 迭代）⇒ **永不 hash dump**                              |
| `/usr/bin/time -l`              | macOS 的 RSS 數字在**前面**（`awk '{print $1}'`；`$NF` 會得到字串 `"size"`）         |
| `no run` 的 RSS                  | 量到的是**編譯器**（~170 MB），不是程式；必須 `no build -o` 後量二進制                       |
| `grep`                          | 本環境是 ripgrep：BRE `\|` **靜默回空**，必須 `grep -E 'a\|b'`                     |
| zsh                             | **不 word-split 未加引號的變數**；`rm -f a b c *.nomatch` 整條命令**不執行**           |
| `;`                             | 是**行註解**，不是敘述分隔符（`x=1; print(x)` 只執行 `x=1`）                            |

---

## 附錄 B：慣用法樣板（`compiler/` 的骨架代碼）

### B.1 `compiler/package.jsonc`

```jsonc
{
  "name": "compiler",
  "mirrors": [],
  "dependencies": {},
  "compiler": {
    "version": "v0.3.19",
    "link-libs": [],
    "anonymous-fn-type": false,
    "overflow": "wrap",       // ← P0-① 落地後啟用；落地前逐敘述寫 #{overflow=wrap}
  },
}
```

### B.2 `compiler/main.no`（M0 骨架）

```no
# /compiler/src/drv.dispatch

drv.dispatch()
```

### B.3 `compiler/src/util/sb.no`（字串構建器）

見 §5.1.2。

### B.4 `compiler/src/ast.no`（arena 化的 AST）

```no
; 節點種類（與 parser/ast.go 的 71 個 Node 一一對應；此處僅示範）
KIND-LET = 1
KIND-IDENT = 2
KIND-INFIX = 3
; …

nodedata {
    kind i64
    a i64        ; 子節點索引 / 位置
    b i64        ; 子節點索引 / 長度
    s i64        ; 詞素索引（指向 intern 表）
}

ast {
    nodes []nodedata
    n i64
    pool intern
}

ast.push = (k i64, a i64, b i64, s i64) (idx i64) {
    d = nodedata {
        kind: k
        a: a
        b: b
        s: s
    }
    .nodes.push(d)
    ; ⚠️ 逐敘述標註：區塊級 #{overflow=wrap} 無效（§2.2.2）
    #{overflow=wrap}
    .n = .n + 1
    #{overflow=wrap}
    idx = .n - 1
}

ast.get = (i i64) (r nodedata) {
    r = .nodes[i]
}
```

**已驗證**：以 `push(1,2,3,4)` / `push(5,6,7,8)` 呼叫後，`get(1).kind == 5`、`get(1).b == 7`（輸出 `0 1 5 7`）。

> 注意 `pool intern` 是**內聯結構體欄位**（默認佈局是 by-value）。若 `intern` 內含容器（這裡是 `[str]i64` + `[]str`），整體會很大；若 `ast` 要被大量複製，應改為 `pool-idx i64`（索引到外部 arena）或給欄位加 `#{inline=false}`。**M0 要在 `ast.no` 的頭部註釋裡把這個決策寫死。**

### B.5 `compiler/src/cg-runtime.no`（照抄的 runtime prelude 片段）

```no
; ⚠️ 反引號禁令：IR 註釋內一律用雙引號引用識別符號，不得用反引號
rc-prelude = '
%nolang_hdr = type { i64, i64 }

define i8* @nolang_rc_alloc(i64 %n) {
entry:
  %sz = add i64 %n, 16
  %base = call i8* @malloc(i64 %sz)
  %rcp = getelementptr inbounds i8, i8* %base, i64 0
  store i64 1, i64* %rcp
  %data = getelementptr inbounds i8, i8* %base, i64 16
  ret i8* %data
}
'
```

⚠️ **`%nolang_hdr` 的 `rc` 是第一個欄位**（即 `data - 16`），retain/release 從 `data` 反向索引（`getelementptr inbounds i8, i8* %p, i64 -16`），**不命名 header 結構**。`rc == 0` 是**借用哨兵**（FFI 指針、字串常量、`cap == 0` 的 `str→[]byte` 視圖），兩個 helper 對它**立即返回**。

### B.6 `compiler/src/link.no`（調用 clang）

```no
# /compiler/src/diag.diag
; 把 LLVM IR 文本寫檔並調用 clang 產出可執行檔
; ⚠️ fs.write-bytes 回 ?bool，必須顯式處理（§2.2.6）
; ⚠️ std 目前沒有 chmod；clang 產出的檔案自帶執行權限，故此處不 chmod
;    （若確有需要，見 §8 P1-④：為 std 補上 chmod）
link.compile = (ll str, out str) (ok bool) {
    llpath str = out + '.ll'
    w ?bool = fs.write-bytes(llpath, ll.to-bytes())
    w: {
        ok(y) -> { }
        nil -> {
            eprint('cannot write {llpath}')
            ok = false
        }
        err(e) -> {
            eprint('cannot write {llpath}: {e}')
            ok = false
        }
    }
    {
        ok == false -> { }
        -> {
            args []str = []
            args.push('-x')
            args.push('ir')
            args.push('-O2')
            args.push('-o')
            args.push(out)
            args.push(llpath)
            ok = process.start-with-opts('clang', args, '', '', true)
        }
    }
}
```

⚠️ `ok` 的 out-param 初值必須先寫成 `false`（本範例為簡潔省略了初始化；實際代碼要在函式開頭 `ok = false`，否則 `ok` 是未定義值——Nolang 的 out-param 不保證零初始化）。

---

## 附錄 C：與其他設計文檔的關係

| 文檔                                                  | 關係                                                                                                           |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `docs/design/nolang-native-backend.md`（nocg / nold） | **互補**。它解決「去掉 LLVM 工具鏈依賴」；本方案解決「去掉 Go 實作依賴」。兩者可以疊加：`noc` + `nocg/nold` = 完全不依賴外部工具鏈。但**不要同時做**——先自舉，再 native |
| `docs/design/tagged-enum-payload-ownership.md`      | `noc` 的 M4/M7 必須完整理解它；RC 策略（§5.6）是它的**簡化替代**，M7 收斂時要對齊                                                       |
| `NOLANG-OWNERSHIP-MODEL.md`（repo 根）                 | 所有權模型的權威描述；§5.6 的 RC 規則直接引用其中的 P4/P5                                                                         |
| `docs/docs/lang/*.md`                               | SHS 的語言規則來源                                                                                                  |

---

**文檔結束。** 當前進度見 **§6**：**M0、M1 已完成**（L1 全語料 675/675 一致）；**M2 的前置已全部就緒**——結構化 token 串（`lx.scan-all`）與**補齊後的 L2 oracle**（71/71 節點、語料 `<unknown>` 0 行、`-annotations` 覆蓋 287 條/88 檔，見 §5.3.1）都已完成，且 oracle 有 4 個已證偽會壞的回歸釘。下一步＝**M2 本體**（`ast.no` / `ps.no` / `ty.no` / `ann.no`）；`noc` 自身成為 L1 oracle（§8 **P0-②**）不再阻塞，但仍是自舉的必要一步。
