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
| **測試框架**            | `test/runner.no`（`runner.eq` / `runner.test` / `runner.summary`）             | L0 門禁；⚠️ **斷言失敗不會自動改變 rc**，測試檔必須自己 `f-count > 0 -> os.exit(1)`                                                          |
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
| **`os.exit` / `process.start-with-opts` / `fs.read-bytes`**    | ✅ 可用                        | 驅動層完備                                                            |

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

**後果**：SHS 必須加一條「結構體／枚舉／介面名長度 ≥ 2」，並由 `shs_check.no` 檢查。**不要**用 `t` / `n` / `v` / `k` / `s` 這類名字——它們同時也是泛型的型別變數名。

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

; tmp/probeZ/mn.no   ← 入口檔（含 main）
# /tmp/probeZ/dep
main = () { print(dep.a()) }
```

```
$ ./bin/no build -o /tmp/pD tmp/probeZ/mn.no && /tmp/pD
0          ← 不是 42
```

同一份程式，只要在**入口檔**多加一行**從未被使用**的 `# /tmp/probeZ/lib`：

```
$ printf '# /tmp/probeZ/lib\n# /tmp/probeZ/dep\n\nmain = () {\n    print(dep.a())\n}\n' > tmp/probeZ/n2.no
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

- 「入口檔」= **含 `main` 的那個檔案**。中間模組直接 import `lib` **無效**。
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

**SHS 命名硬規則**（`shs_check.no` 強制）：

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
│   ├── lx_test.no                 ; 詞法單元測試（對齊 src/lexer/lexer_test.go）
│   ├── ps_test.no                 ; 語法單元測試（對齊 src/parser/*_test.go）
│   ├── ck_test.no                 ; 語義單元測試
│   ├── cg_test.no                 ; 代碼生成單元測試（斷言 IR 片段）
│   └── e2e_test.no                ; 端到端：編譯 → 執行 → 比對
│
└── tools/
    ├── gen_stdsig/                ; 從 src/std 生成 src/ck-sig.no（Go 實作，一次性）
    └── shs_check.no               ; SHS 白名單守護（§7.6）
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

⚠️ **`nodedata` 必須是 POD**（只放 `i64` 與 `str`；`str` 欄位在 `push` 時仍會深拷貝，若詞素很長需改為「索引到 intern 表」）。這條要在 M0 的 `arena.no` 裡寫成註釋約定，並由 `shs_check.no` 檢查。

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

⚠️ **這條是 SHS 的硬規則**，`shs_check.no` 必須檢查：`compiler/src/**` 中不得出現模組級 `let`（`tok.no` 的關鍵字表等**只讀常量**除外，且必須在檔案頭以註釋聲明）。

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

另有 L0 的 `compiler/tests/tok_test.no` **62/62** 單元斷言。

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

⚠️ **dump 的穩定性**：`no ast` 的輸出必須是**確定性**的。**已實測**：`SemanticContext.nodeSem` 是 `map[Node]*NodeSemantics`，**絕不可 range**（Go 隨機化 map 疊代序）；dumper 一律以**樹序**走訪並對每個節點做**點查詢**。`TestDumpIsDeterministic` 對全語料連跑兩次逐位元組比對。

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

`#{...}` 註解**不在節點欄位裡**，而在 `Program.Sem`（`SemanticContext`）的 side-table 中，且該表是 **`map[Node]*NodeSemantics`**。

🔴 **絕不可 range 這張 map**：Go 隨機化 map 疊代序，dump 會變成不可重現。實作改為**依樹序走訪、對每個節點做點查詢**（`Sem.AnnotationsOf(n)`），因此輸出順序恆定。`TestDumpIsDeterministic` 對全語料連跑兩次逐位元組比對以釘住這一點。

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
$ printf "main = () {\n    b = 'x'\n    m [str]i64 = { 'k': 1 }\n}\n" > /tmp/t2.no
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
| **M0** | 子項目骨架：`compiler/` 佈局、`package.jsonc`、`main.no` 分派、`util/sb.no`、`tools/shs_check.no`、`scripts/selfhost_diff.sh` | ✅ **完成** | `no run compiler/tools/shs_check.no compiler/` ⇒ **10 檔 / 0 違規 / OK**；`selfhost_diff.sh --self-test` ⇒ **無 FAIL** |
| **M1** | 詞法：`src/tok.no`（92 個 `TokenType`）+ `src/lx.no`（`scanToken()` 的忠實移植）              | ✅ **完成** | `no run compiler/tests/tok_test.no` ⇒ **62/62**；`lx_test.no` ⇒ **45/45**；L1 全語料 **675/675 SAME, 0 DIFF**（§7.1.1） |
| **M2** | 語法：71 個 AST 節點 + 復刻 `parser/dump.go` 的輸出格式                                     | 🔵 **進行中** | **L2 已可執行**：`noc` 已實作 `ast` 子命令（`drv.cmd-ast`），`selfhost_diff.sh --self-test --levels L0,L2 --limit 30 --sut /tmp/noc-s0`（2026-10-10）⇒ **L0 4/4 SAME、L2 30/30 SAME、0 DIFF**、語料 `<unknown>` 0 行（71/71 節點全覆蓋）。單元證據：`ast_test` 44/44、`tok_test` 62/62、`lx_test` 45/45、`util_test` 27/27；`no vet compiler/src` ERROR **0**、WARNING **0**。覆盖进度见 §6.3（t0–t13） |
| **M3** | 語義：37 個 `Validate*` + `stdsig` 表（§5.4）                                           | ⬜ 未開始   | —                                                                                      |
| **M4** | 中端 + 代碼生成：HIR / MIR + RC 堆策略 + LLVM IR 文本                                       | ⬜ 未開始   | —                                                                                      |
| **M5** | 驅動層：`build` / `run` / `vet` 的 CLI 與 clang 調用                                     | ⬜ 未開始   | —                                                                                      |
| **M6** | 定點：S0→S1→S2→S3 + 三路交叉編譯（§3.1 / §3.2）                                            | ⬜ 未開始   | —                                                                                      |
| **M7** | 精確所有權分析（`insertDrops` 那一整套）                                                       | ⬜ 未開始   | —                                                                                      |
| **M8** | 後置：`fmt` / `lsp` / `tools`（不阻塞定點）                                                | ⬜ 未開始   | —                                                                                      |

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

### 6.2 M2 的進度與**前置**（截至 2026-10-10）

**已完成的前置**（屬 M1 的詞法 API，但只為 M2 而做）：

- **`lx.scan-all(src) (r []tk)`**：把整份原始碼掃成結構化 token 陣列（含結尾 `EOF`）。
  動機：M1 時 `lx-scan-token` 是「掃描 + 渲染成字串」，只有 `dump` 一個消費者。
  若為解析器另寫一份掃描，兩份必然漂移，而 **L1 只保護其中一份**。⇒ 重構為
  **掃描只填 `tk` 結構**；`dump`（經 `lx-fmt`）與 `scan-all` 都是它的下游。
  重構後 **L1 675/675 不變**——這就是「重構沒改行為」的證據。
- **`compiler/tests/lx_test.no`（45 斷言）**：其中 `test-no-alias` 專門釘住
  「`push` 是**深拷貝**」。若 `[]tk` 的 push 退化成別名，`scan-all` 會全錯而
  `dump` 全對 ⇒ **L1 照不到**。

**進度（2026-10-10 更新）**：`ast.no`（節點 + arena，1417 行）與 `ps.no`（解析器，816 行）**已開工並持續增量推進**（見 §6.3）；`ty.no`（型別）、`ann.no`（註解）尚未開始。

**開工前必讀 §5.3.1**：該節現在是**格式契約**（`noc` 必須逐位元組復刻的那一份），不再是「盲點警告」。

**原「已知的驗收缺口」——兩項均已關閉（2026-10-10）**：

| # | 缺口 | 處置 | 證據 |
| --- | --- | --- | --- |
| 1 | `no ast` **不印註解**（P0-③） | ✅ 新增 **`-annotations`**，依樹序點查 side-table（**絕不 range** 那張 map） | 語料實測 **287 條 / 88 檔**；`TestAnnotationsFlagIsWired`、`TestNoAnnotationsByDefault` |
| 2 | **節點覆蓋不全**（9 種塌成 `<unknown>` + 4 類只印殼） | ✅ `dumpNode` 由 40 → **71** 個 case；所有 `Node` 欄位/切片元素都遞迴 | 語料 `<unknown>` **319 → 0**；`TestCorpusHasNoUnknownNodes`（已證偽：刪一個 case ⇒ 24 檔 FAIL） |

✅ **已關閉（2026-10-10）**：`noc` 已實作 `ast` 子命令 ⇒ **L2 從 UNAVAILABLE 轉為可執行**，且 `--sut /tmp/noc-s0` 全語料 **30/30 SAME / 0 DIFF**（見 §6 表 M2 行與 §6.3）。

**節點盤點與錯誤訊息目錄**：見 [`nolang-ast-grammar.md`](nolang-ast-grammar.md)（由 `scripts/ast_inventory.py` 從 Go 原始碼機械抽取；含 71 個節點的欄位／建構函式，以及 **168 個錯誤訊息站點**——這是 M2/M3 要逐字重現的清單）。

### 6.3 M2 解析器覆蓋進度（逐增量，截至 2026-10-10）

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

**路徑復刻（§5.3.2）的實測要點**（全部經 oracle 探針驗證，非推論）：

1. **型別推斷只對字面量合成 NamedType**：`x = 1 + 2`、`y = x` 的 LetStatement **沒有** Type 子節點；只有 INT→`i64`、FLOAT→`f64`、STRING→`str`、BOOL→`bool` 字面量直接 RHS 才推。`x = f()` RHS 是 CallExpression 也 **不推**。
2. **kid 槽位佔位規則**（新坑，已入 §2.1.1 表）：LetStatement 的 spec CHILD 是 (Type, Value) 兩槽，缺 Type 時必須 `kid-add(t, idx, 0)` 佔位；IfExpression 同理三槽 (Condition, Consequence, Alternative)，缺 alt 時也要 push 0。
3. **`a = (…)` 歧義規則**（新坑，已入 §2.1.1 表）：`ps-is-func` 從當前 LPAREN 掃到配平的 RPAREN，再看下一個非 trivia token 是否 LBRACE；否則 `a = (x)` 會被誤判為函式定義並掛死。
4. **GroupedExpression / NilLiteral 的 dump 不帶括號／花括號**（探針實測）。
5. **CallExpression 的 `fn:` 使用 nodeString**（dump.go:904）：Identifier 時取 name，其他節點取 kind-name（例 `a.b(1)` ⇒ `CallExpression{fn: DotExpression, args: 1}`，因为 fn 的 nodeString 預設回 kind name）。
6. **args: 0 時 dump 不印**（kvc 政策 NON-ZERO）：slot0 存 `args.len().to-str()`，count slot0 同步 `args.len()`。
7. **`return 42` 在 Nolang 中是 parse error**（out-param 風气），不納入 M2 復刻目標。
8. **match 臂尾臂不互斥**（新坑，已入 §2.1.1）：同块 `cond -> { idx = A }  …  -> idx = B` 中，B 臂會**作為無條件尾隨陳述**再執行一次，覆寫 A 臂的 idx。`ps-parse-stmt` 的 IDENT 分支靠 `handled` 旗標隔開。
9. **單行 `-> { a; b }` 吃掉閉合 `}`**（§2.1.1 首行的實測復發）：SliceLiteral 元素迴圈若寫 `ps-peek(p) == tok.COMMA -> { ps-bump(p); ps-skip-trivia(p) }`（單行），`;` 會把 `ps-skip-trivia(p) }` 整段註解掉 ⇒ `{` 永不閉合 ⇒ 向後吞掉 `ps-parse-if` 等**所有頂層定義**，報錯卻指向遠端呼叫點：`EmitLLVM: unknown callee ps-parse-if in func ps-parse-stmt`。修正＝展開成多行（與既有的 call-args 迴圈同形）。
10. **ArrayLiteral / 明示 `[N]T` 型別標註尚未復刻**（待辦增量）：探針 `x [3]i64 = [1, 2, 3]` 顯示 oracle 走 `LetStatement{type: ArrayType{size:3}}` + `ArrayLiteral{size, elements, wasSliceLiteral: true}`——需要在標註位置解析 `[N]T` 語法、並把 SliceLiteral RHS 轉為 ArrayLiteral（設 `wasSliceLiteral`），與 SliceLiteral 是不同的複雜度，本增量**只釘住 SliceLiteral**。

**vet 清理（同次增量）**：`ast.no` 39 處 `[ovf-int-default]` ERROR 已全部以語句級 `#{overflow=wrap}` 清零（單行 match 臂如 `op.eq('+') -> r = a + b` 必須展開成多行區塊，標註才生效，見 §2.1.1）；`shs_check.no`／`util_test.no` 的剩餘站點同步處理。現狀：`no vet compiler/src` ⇒ **0 ERROR / 0 WARNING**。

⚠️ **有意保留的偏離**：`*_test.no`、`shs_check.no` 的檔名含下劃線，違反 `fn-rule` 檔名 lint——但這些檔名由本設計文檔（§6 M0/M1 驗收證據）直接指定，屬文檔與 lint 的衝突，以文檔為準。

**可重跑驗證（本增量的全部證據）**：

```sh
./bin/no build -o /tmp/noc-s0 compiler                     # Build successful
for f in compiler/tests/fixtures/*.no; do                  # t0–t14 全部逐位元組相同
  diff <(./bin/no ast "$f") <(/tmp/noc-s0 ast "$f") && echo "SAME $f"
done
./scripts/selfhost_diff.sh --self-test --levels L0,L2 --limit 30 --sut /tmp/noc-s0   # L0 4/4・L2 30/30・PASS
./bin/no run compiler/tests/{ast,lx,tok,util}_test.no       # 44・45・62・27 全 PASS
./bin/no vet compiler/src                                   # 0 ERROR・0 WARNING
```

⚠️ **超時包裝的坑**（本節命令若改寫成自製 harness 時必讀）：用 perl `fork`+`waitpid(WNOHANG)` 包裝時，被 SIGKILL 的進程 `$? >> 8` 是 **0**，會把掛死誤報成成功；必須用顯式 `$to` 旗標（或 `perl -e 'alarm N; exec @ARGV'`）回報 124。

---

## 7. 驗證策略（差分矩陣）

### 7.1 五級門禁總表

| 級      | 對象                    | Oracle                          | 判據                                        | 成本           |
| ------ | --------------------- | ------------------------------- | ----------------------------------------- | ------------ |
| **L0** | `compiler/tests/*_test.no` | `test/runner.no`（測試檔自檢） | 全綠；⚠️ 測試檔必須自己 `f-count > 0 -> os.exit(1)`。⚠️ 執行方式必須是 **`no run <file>`**：`no <file>` 只印 usage 且 rc=0 ⇒ 每個測試檔的 stdout 都相同 ⇒ **G7 判 oracle 無鑑別力** | 秒            |
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
$ ./bin/no compiler/tests/tok_test.no | shasum -a 256     # 錯的呼叫
febadc57a53b79d8a913838c9afed6d30f16736dfcc6ec3d1e20f8264442b718
$ ./bin/no compiler/tests/lx_test.no | shasum -a 256      # 同一個雜湊！
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

### 7.6 SHS 守護（`tools/shs_check.no`）

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

main = () {
    drv.dispatch()
}
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
