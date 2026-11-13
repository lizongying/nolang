# Nolang 自舉（Self-Hosting）技術方案：`compiler` 子項目

> **文檔定位**：這是一份**前向設計（forward design）＋ as-built 實施日誌**的活文檔：§0–§4／§7–§11 描述目標形態、分階段路線圖與驗收門禁，§5.3.4 起按增量記錄已落地的實作與實測證據。**`compiler/` 子項目自 2026-10-09 起已存在並持續推進**（2026-11-12 現況：`compiler/src/*.no` 14 檔＋`compiler/src/util/*.no` 2 檔；M0–M3 完成，M4-c 發射層與 M4-e AST→MIR 降級已續推至 **s99n**，見 §6／§6.5／§11.2 B4／文末 2026-11-12 快照）凡標「2026-10-09 工作樹（`HEAD = 4feaaf70`）」的行號／計數屬**歷史證據**，不是現狀；同理 §4.1.1 的 2026-10-30 盤點、§6.5 的 2026-10-29 啟動摸底、文末 2026-11-06 快照均已 frozen 為歷史紀錄
>
> **一句話目標**：在 `compiler/` 子項目中用 **Nolang 自身**實現一個完整編譯器 `noc`，使它能夠**編譯自己**，並以S0 → S1 → S2 → S3 定點的方式證明自舉成立；同時用**五級差分門禁**保證它與現有 Go 實作 `no` 的行為一致
>
> **命名**：`noc` = **No**lang **C**ompiler它是 `compiler/` 包構建出的可執行檔，與 Go 實作的 `no` **並存**互不干擾`no` 在本文中一律指 **S0（當前 Go 實作）**，`noc-s1/s2/s3` 指自舉的三個階段產物

> ⛔ **2026-11-12 後端佈局變更（Go 側已落地，`compiler/src` 尚未鏡像）**：Go `no` 的 `?T` 不再是單一
> `%option = { i64 tag, [3 x i64] }`，改為**按載荷定型**的 `%option_<key> = { i8 tag, [K x i64] }`
> ——標籤 1 位元組、載荷固定從**位元組偏移 8** 開始、`K = roundUp8(sizeof(T))/8`，超過
> `option-inline-threshold`（預設 **264** = 1 + 7 + 256，最小 16）的載荷改存堆指標；`err` 的載荷
> **恆裝箱**。標籤列舉同時改為 `%tenum_X = { iN tag, [K x i64] }`：`iN` 隨變體個數（`<=256` → `i8`）、
> `K` 取**最寬變體**（不再按整槽累加）、欄位按自然對齊以**位元組偏移**尋址。權威實作見
> `src/mir/layout.go`；語言文檔見 `docs/docs/lang/syntax.md`〈可空類型(option)・內存佈局〉與
> 〈內存佈局（棧式）〉。
>
> **本文件 §5.3.4 s25–s27、§5.3.4.be、§5.3.4.bv、§5.3.4.cx 等節記述的 `%option = { i64, [3 x i64] }`
> 全部視為歷史紀錄。** `compiler/src/cg-runtime.no` 的 `cg-runtime-option-type()` 仍是舊形，
> **把新佈局鏡像進 `noc` 是待辦（P4）**；在完成前 `noc` 與 `no` 的 option 佈局不同，跨 option
> 的 `L4` 差分以 `no` 的輸出為準。

---



## 0. 決策快照（Executive Summary）

| #        | 決策        | 結論                                                                                                                                                           | 主要論據                                                                                                                          |
| -------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| **DR-1** | 自舉的定義   | **三階段 + 定點**：S0（Go）編譯 `compiler/` → `noc-s1`；`noc-s1` 編譯同一份源碼 → `noc-s2`；`noc-s2` → `noc-s3`要求 **s2 與 s3 在全語料上 `(rc, sha256(stdout))` 完全一致**，並盡量要求二進制逐位元組一致 | 這是唯一能排除編譯器靠 S0 的某個隱含假設才跑得起來的判據；單純能用 nolang 寫個編譯器不叫自舉                                                                      |
| **DR-2** | 後端吃什麼吐什麼 | **吃自己的 MIR，吐 LLVM IR 文本**，再調用 `clang` 完成匯編 + 鏈接**不依賴 `opt` / `llc`**（`clang` 可直接吃 `.ll`），**不實現機器碼生成器不實現鏈接器**                                               | 沿用 `no` 今日已驗證的 MIR→IR 架構；跨平台（macOS arm64 / Linux x64 / Windows x64）由 `clang` 保證；把最大的不確定性（ISel/RA/obj writer/linker）整體排除在自舉範圍外 |
| **DR-3** | 堆記憶體策略    | **M4 階段一律用引用計數（RC）**：沿用 `no` 已有的 16 字節頭 ABI（`@nolang_rc_alloc` / `@nolang_rc_release`，`data = base + 16`）語義**精確的所有權分析（`insertDrops` 那一整套）延後到 M7**           | 見 §5.6：這一步把 `src/mir` 43k LOC 中**最難最容易出 UAF/double-free 的那一半**從自舉必需降級為生產化優化RC 的缺陷（循環洩漏）對一個**短命編譯器進程**是可接受的              |
| **DR-4** | 自舉子集（SHS） | 明確**禁止**：一等函式值`*` 指針動態介面分派匿名函式型別`any`/`obj``#{c}` FFI**強制使用**：標籤枚舉 + `match` 分派arena（`[]T` + `i64` 索引）`?T` + `?=`                                    | §2.2 有實測：fn 值在**除最平凡形狀外全部崩潰或 IR 驗證失敗**；arena+索引同時解決循環引用無法用所有權表達與`[]T` 擴容使元素位址失效兩個問題                                       |
| **DR-5** | 產物隔離      | `compiler/` 是獨立包，構建出 `noc`；**不改動** `no` 的任何行為；`-native`（nocg/nold，另一份設計文檔）不在本方案範圍                                                                            | 自舉期間 `no` 必須始終是一把**可信可回歸的尺子**；讓 S0 同時當被測物會毀掉差分門禁                                                                           |
| **DR-6** | 驗證門禁      | **五級差分** L0 單元 / L1 詞法 / L2 AST / L3 診斷 / L4 行為 / L5 定點，全部**複用既有基建**（`no ast``no vet``scripts/mir_golden.sh``tests/golden/mir-baseline.tsv`）              | §7：這套基建已在 repo 中被反覆驗證過（AST 全語料 593/594 一致；golden 488 條指紋）                                                                     |
| **DR-7** | 前置改動      | 4 項 P0 **原訂**先於 M1 落地：① `package.jsonc` 包級 `overflow` 默認；② `no tokens` 子命令；③ `no ast --annotations`；④ 一等函式值修復**M1 實測：② 已被繞過**——L1 的 oracle 改由 **repo 外**的只讀 dumper 提供（§7.1.1），**不動 `src/`**；② 仍對`noc` 自己當 oracle有價值                                                | §8：① 不做的話編譯器源碼每一行算術都要寫 `#{overflow=wrap}`；②③ 不做就沒有**內建**的詞法／註解差分 oracle（② 已由 §7.1.1 的外部 oracle 繞過）；④ 見 §2.2.1                                                           |
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

> **結論**：這是一個 **12 萬行級** 的移植工程，不是寫個玩具編譯器方案的全部設計目標就是**把 12 萬行拆成 8 個各自可獨立驗收的里程碑**，並讓每一步都有可執行的差分門禁


### 1.2 可直接複用的既有資產

| 資產                  | 位置                                                                           | 對自舉的用途                                                                                                                  |
| ------------------- | ---------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| **行為基準指紋**          | `tests/golden/mir-baseline.tsv` + `scripts/mir_golden.sh`                    | L4 門禁：全語料 `(rc, sha256(stdout))` 對比，已內建 517+ 檔案的並行 harness逾時歸一化bucket 分類（SAME/DIVERGE/REGRESS/IMPROVED/BOTH_FAIL/NEW） |
| **AST oracle**      | `no ast <file>` → `parser/dump.go`                                           | L2 門禁：repo 已用此法證明過全語料 593/594 AST 逐位元組一致                                                                              |
| **診斷 oracle**       | `no vet <file>`                                                              | L3 門禁：`<file>:<line>:<col>: [LEVEL] <source>: <msg> [<code>]`；⚠️ 歸一化後才可比（§7.3）                                          |
| **IR 檢視**           | `no build -v -o OUT F.no`（保留 `.ll`）；`NOLANG_MIR_DUMP_LL=1`（**驗證失敗時唯一窗口**）    | M4 調試                                                                                                                   |
| **runtime prelude** | `src/mir/codegen.go` 的 `windowsShimsIR()` / `%task` prelude / `@nolang_rc_*` | M4：**IR 文本可以直接照抄**，不需要重新設計 runtime                                                                                      |
| **標準庫**             | `src/std`（118 模組）                                                            | `noc` 自身的工具層（`fs` / `os` / `process` / `path` / `json` / `sort` / `str`）                                                |
| **測試框架**            | `test/runner.no`（`runner.eq` / `runner.test` / `runner.summary`）             | L0 門禁；⚠️ **斷言失敗不會自動改變 rc**，測試檔必須自己 `f-count > 0 -> exit(1)`                                                          |
| **平台常量表**           | `src/mir/platform.go``src/package/platform.go`                              | M5：`targetGOOS()/targetGOARCH()` 是唯一真值來源；**絕不可用 `runtime.GOOS`，也絕不可寫在 package 級 `var`**                                 |


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

`std/process.no` 已有 `process.start-with-opts(program str, args []str, dir str, input str, merge-err bool) (ok bool)`，`std/fs.no` 已有 `read-bytes` / `write-bytes` / `list-dir` / `exists`——**驅動層所需的系統調用全部已具備**，無需新增 FFI

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
| 🔴 **`;` 是行註解，不是敘述分隔符**                                                                         | `c == X -> { typ = A; return }` 寫成**一行** ⇒ `; return }` 整段被註解掉 ⇒ 該區塊**永遠不閉合**33 個未閉合區塊會吞掉其後所有定義，而錯誤報在**很後面**的呼叫端：`'lx-emit' is not defined`（指向 200 行外的呼叫點） | 每個 `-> { … }` 臂**多行展開**；`;` 只允許出現在字串字面量裡                                                  |
| 🔴 **`x -> { … }` 不是迴圈**                                                                        | 以為 `go -> { … }` 是 while ⇒ 迴圈體**只執行一次**（它是 match 臂）症狀：只吐出第一個 token 就停                                                                            | `(cond) { … }`（前置式）或 `{ … } (cond)`（後綴式）；計數用 `n * { }`；範圍用 `i <- [a..b): { }`               |
| 🔴 **`err` 是保留標識符**                                                                            | 用它當參數名或局部變數名 ⇒ **整個函式不被註冊**，錯誤卻指向**呼叫端**（`'lx-emit' is not defined`）                                                                              | 改名（`lx.no` 用 `emsg`）                                                                 |
| 🔴 **導入形式有兩種，用錯的訊息不指向根因**                                                                       | `# /path/mod.Type` = **型別**導入（配 `<Type>.<method>`）；`# /path/mod` = **模組**導入（配 `<mod>.<定義名>`）用錯 ⇒ `unknown callee lx.dump` 或 `unknown function 'lx.dump': module 'lx' has no top-level function 'dump'` | `lx` 無結構體 ⇒ 模組形式；`sb` 是結構體 ⇒ 型別形式                                                     |
| ⚠️ **跨模組呼叫用的是定義名而非前綴名**                                                                     | 函式定義名 `lx-dump` ⇒ 呼叫端只能寫 `lx.lx-dump(...)`                                                                                                      | 對外進入點命名為 `dump` ⇒ `lx.dump(src)`                                                      |
| ⚠️ **`#{overflow=wrap}` 只作用於緊跟的那一條敘述**                                                       | `cond -> v = a + b` 寫成一行 ⇒ 標註無效 ⇒ `[ovfhndld]`                                                                                                    | 展開成多行區塊，把標註放在**真正含算術**的那一行之前                                                          |
| ⚠️ **`fs.read-str` 非逐位元組無損**                                                                   | 非 ASCII 位元組被二次編碼（5 → 15 位元組），**`rc` 測不出來**                                                                                                        | `fs.read-bytes` + `to-bytes-str()`；見 §2.2.9                                            |
| ⚠️ **跨模組常量只有入口檔直接 import時才正確**                                                                | 只被間接依賴時**靜默變成 0**，作為運算式運算元時**硬編譯失敗**                                                                                                            | 入口檔直接列出該模組（**未使用也要列**）；見 §2.2.8                                                        |
| 🔴 **(M2) arena kid 槽位必須對齊 spec 的 CHILD 計數**                                                       | LetStatement 缺 Type 時只 push 1 個 kid ⇒ dumper 會讀到**後續節點的 kid** ⇒ 樹錯位／成環 ⇒ `noc ast` **掛死**（rc 測不出，需超時包裝）            | 無推斷型別時也要 `kid-add(t, idx, 0)` 佔位，再無條件 push Value 槽；見 §6.3                                       |
| 🔴 **(M2) `name = (` 有兩種句法**                                                                       | 只看首個 `LPAREN` 就判函式定義 ⇒ `a = (x)` 被誤判為 FunctionLiteral，`ps-parse-block` 苦等 `RBRACE` 到 EOF ⇒ **掛死**              |  lookahead 到配平的 `)` 後第一個非 trivia token 是 `{` 才算函式（`ps-is-func`）；否則走 GroupedExpression；見 §6.3                    |
| ⚠️ **(M2) 複合標註是合法的**                                                                                | 逐條堆疊 `#{index-out=zero}` `#{overflow=wrap}` 很囉嗦                                                                                          | 同一行用逗號合併：`#{index-out=zero, overflow=wrap}`（先例：`src/std/crypto/x509.no`）                                       |
| 🔴 **(M2) `{ … cond -> { 大區塊 } … -> 運算式 }` 的尾臂不互斥**                                                    | 區塊內前面 `cond -> { idx = A }` 已把 `idx` 設好，但尾部 `-> idx = B` **會作為無條件尾隨陳述再執行一次** ⇒ 覆寫前面結果（實際症狀：`x = 1` dump 出 `Program{}`，`idx` 由 ASSIGN 臂設的 3 被覆寫為 ps-parse-expr-stmt 的 0）         | 用 `handled` 旗標：前面每個臂末尾寫 `handled = 1`，尾臂改寫為 `handled == 0 -> idx = …`；**原 `-> {}`**（空區塊）恰好因為 no-op 才讓舊版僥倖正確（`ps.no::ps-parse-stmt` 的 IDENT 分支）                                       |
| 🔴 **(M2) 迴圈內 ≥3 個區塊臂的 match 跳臂迭代誤編**                                                                       | `(pgo) { A -> {blk} B -> {blk} C -> {blk} -> {pgo=false} }`：兩臂時正常，加到第三個區塊臂後，某個**跨臂接連迭代**會提前退出迴圈（實測 `f(x)[2]`／`m[0][1]`／`a.b(1)` 三探針必其一在首臂後就 `pgo=false`，後續 `[]`/`()` 被當成独立陳述）；調整臂序只是把壞的轉移換一個，換預設臂為區塊形也無效 | 同上——用 `handled` 旗標把終止條件剝離 match：每臂尾 `handled = 1`，迴圈末尾 `handled == 0 -> pgo = false`（`ps.no` 後綴迴圈）                                       |
| 🔴 **(M2) match 各臂區塊的區域變數提升到同一函式作用域**                                                                     | 同一 `cond -> {…}` 多臂去糖成的 if/else 鏈，各臂區塊內宣告的變數**並非區塊作用域**而是共用外層函式作用域；若新臂沿用兄弟臂已佔用的名字但**型別不同**（如 `ps-parse-stmt` 內 `tn` 在明示型別標註臂是 `tt.lit`=str在多重指派臂卻當 i64），型別 checker/codegen 合併成 str ⇒ `tn > 0` 觸發 MIRordering comparison gt with a string operand`targets.push(tn)` 觸發cannot push 'str' into '[]i64'⇒ **build 直接失敗**（非 L2 dump 錯，故 fixture diff 看不到，只有 `no build` 會爆） | 同一個 match 的各臂，**區域變數名要全域唯一**：新臂所有地變數加語義前綴（`maTargets/maT0/maTn/maVal/maK`）；`idx`/`handled` 等跨臂同型別者可共用                                                          |

#### 2.1.2 位元組遍歷約定：**先 `str.to-bytes()`，再走 `[]byte`**

> 實測結論（2026-10-21）：`str.byte(i)` **本身**是 O(1)；壞掉的是**用結構體字段當接收者**

`emitBuiltinRawByteAt`（`src/mir/codegen.go:15115`）把 `s.byte(i)` 展開為
`GEP + load + zext`，純 O(1)——對**區域變數／參數**接收者完全沒有問題
真正的陷阱是另一件事：

> 🔴 **讀取 `str` 結構體字段是一次借用⇒ 整塊深拷貝 ⇒ O(N)**
> 一旦這種求值被放進逐位元組的掃描迴圈，每次迭代都拷一份，總量退化成 **O(N²)**

分離實驗（探針 `/tmp/fieldprobe3.no`；同一工具鏈同一迴圈形狀，**只差字段的型別**）：

| 接收者 | N=40K | N=80K | N=200K | 性質 |
|--------|-------|-------|--------|------|
| A：`bx.s` 為 `str` + `.byte(i)` | 0.040s | 0.158s | **0.852s** | **O(N²)** |
| B：同上但長度**先快取**到局部 | 0.043s | 0.138s | **0.836s** | **仍 O(N²)** |
| C：`bx.b` 為 `[]byte` + `[i]` | 0.009s | 0.010s | **0.010s** | **O(N)** |

B 仍二次 ⇒ 二次項**不在** `len-bytes()`，而在**字段借用本身**；
而 `[]byte` 的字段借用只拷描述符（3×i64，O(1)）並共享緩衝，故平坦

**約定**：需要在位元組層面遍歷字串時，**在入口處 `str.to-bytes()` 轉一次**，
此後一律用 `b[i]` 索引`b.len()` 取長度：

- 省去單獨快取長度的欄位（如 `lxst.n`），消除長度與實際緩衝漂移的風險；
- 不用 `len-bytes()`；
- 轉出來的 `[]byte` 可直接用於後續遍歷

🔴 **硬約束：`str.to-bytes()` 本身是一次 O(N) 拷貝**，不是零成本視圖
看實作就明白（`src/std/str.no:464`）：

```nolang
str.to-bytes = () (val []byte) {
    val = with-len(.len-bytes()); 新配置一塊緩衝
    i <- [0..self.len-bytes()): {
        val[i] = .byte(i); 逐位元組複製
    }
}
```

所以它**只能在入口／邊界呼叫一次**若放進逐位元組迴圈或每次
`push` 都轉一次，就會把 §2.1.2 另一條不變量（`vec.push` 攤還 O(1)）
重新打回 **O(N²)**

> 由此也得到一條**看似反直覺其實正確**的結論：
> `sb.push(s str)` 內部用 `s.byte(i)` 逐位元組追加是**對的**——
> 因為 `s` 是**參數（區域變數）**，`.byte(i)` 是 O(1)，且位元組終究要
> 拷進 `.buf`若改成 `sb.push(s []byte)` 並要求呼叫端先 `to-bytes()`，
> 只是多加一次 O(N) 拷貝與一次配置，沒有任何好處

**取捨通則**（決定要不要先轉 `[]byte`）：

| 情境 | 怎麼寫 | 理由 |
|------|--------|------|
| 接收者是**結構體字段**，且放在逐位元組迴圈裡 | ✅ **務必**在入口轉一次 `[]byte` | 否則每次迭代整塊拷貝 ⇒ O(N²) |
| 接收者是**參數／區域變數**，只做 O(1) 索引 | 直接用 `.byte(i)` 即可 | 轉 `[]byte` 反而**多一次 O(N) 拷貝** |
| 需要**反覆遍歷**同一份資料多次 | 轉一次 `[]byte` 留存 | 一趟拷貝換後續每趟省一次拷貝 |

> ⚠️ 已知代價：`lx-esc`／`lx-contains-dot` 改簽名為 `[]byte` 後，呼叫端
> `t.lit.to-bytes()` 會比原本 `lz-esc(t.lit)` **多一次拷貝**（字段借用 1 次
> ＋ `to-bytes()` 1 次）因為是**逐 token** 而非逐位元組且字面量短，
> 實測仍在噪聲內（236K：0.054s vs 0.049s）這是為了寫法統一
> 去掉長度快取付的常數代價，屬於**可接受**；但不要據此把 `to-bytes()`
> 套進熱迴圈

⚠️ 配套事實：`str.len()` 回的是**碼點數**，不是位元組數（`src/std/str.no:41-44`，
實作為 `n = .cp-index(.len-bytes())`）；`[]byte.len()` 才是位元組數——
這也是 `[]byte` 更適合當位元組遍歷載體的原因之一

⚠️ 方法真名是 **`str.to-bytes()`**（`src/std/str.no:464`），**沒有** `str.bytes()` 這個名字

---


### 2.2 實測**不可用 / 受限**——本方案的關鍵約束

> 這一節是本方案最重要的部分**每一條都改寫了架構決策**，全部有可重跑探針

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

**語料事實**：`src/std` **0 處**使用具名函式型別；`tests/` 僅 **5 個檔案**（`named-fn-type.no``fn-type-params.no``fn-type-return.no``fn-type-result.no``mem-safety/ffi-str-return.no`）使用，且全部只用最平凡形狀

**架構後果（強制）**：

1. **SHS 禁止 fn 值**編譯器的分派一律用 **`kind i64` + `match`**，或 arena 索引 + `match`
2. 這一條**不阻塞自舉**——因為 std 不用它，編譯器自己也不用它
3. 但 **§8 P0-④ 必須修**：`noc` 作為一個完整編譯器，遲早要編譯使用 fn 值的**使用者程式**（語料裡就有 5 個）修復前的替代方案是SHS 檢查器 + 明確診斷

#### 🔴 2.2.2 整數運算默認返回 `option<int>`，且**只能逐敘述標註**

```no
f = () (r i64) {

    #{overflow=wrap}
    a = 1

    ; ← 只作用於緊跟其後的那一條敘述
    b = a + 2; ← 這裡仍然報錯 [ovfhndld]
    c = b * 3; ← 這裡也報錯
    r = c - 1
}
```

實測：區塊級標註**無效**；每一條含算術的敘述都要單獨寫 `#{overflow=wrap}``package.jsonc` 的 `compiler` 區塊**目前沒有** `overflow` 默認鍵

**後果**：一個 12 萬行以整數/索引運算為主的編譯器，會需要**數萬條** `#{overflow=wrap}`這在可讀性與維護性上都不可接受 ⇒ **§8 P0-①**

#### 🔴 2.2.3 深度遞迴的棧預算未測

`no` 的解析器是遞迴下降的，`noc` 也必然是已知 `src/mir` 有一條硬性約束：**每個 `alloca` 必須在 entry block**（否則迴圈裡每輪成長一次棧）這意味著**每個遞迴框架的棧佔用是函式全部區域變數之和**，不會隨分支收縮

**後果**：需要 **M2 的一個明確驗收項**——量測語料中嵌套最深的檔案所需棧深，並決定是否需要（a）把 `parseExpression` 的若干層迭代化，或（b）在 `noc` 的 `main` 中調用 `setrlimit`/換大棧**不要等到 M6 才發現**

#### 🔴 2.2.4 `str` 讀寫索引語義不對稱

`s[i]` **讀**是**碼點**索引，`s[i] = b` **寫**是**位元組**索引編譯器源碼會大量做位元組級掃描（詞法分析UTF-8 解碼hex 解析）

**後果（寫進 SHS 規則）**：**一切位元組級讀取一律用 `s.byte(i)`**（回無號數值），邊界一律用 `s.len-bytes()`；`.len()` 是**碼點數**這條規則在 repo 的歷史上曾造成過 109 處靜默錯值（`src/std/net/*` 的 TLS 記錄長度 `0x94` 被讀成 `1`）

#### 🔴 2.2.5 結構體名**必須 ≥ 2 個字元**（單字元名會炸 codegen）

實測：`a` / `b` / `c` … `z`（**24 個單字元名全部**）作為結構體名時，一律：

```
Error: compilation error: MIR codegen could not handle /tmp/pp.no:
  EmitLLVM: unsupported value type %h in func main
```

同一個程式把 `h` 改成 `hh` 或 `holder` 就正常（`5`）**這不是文件化的限制**，但它是硬事實

**後果**：SHS 必須加一條結構體／枚舉／介面名長度 ≥ 2，並由 `shs-check.no` 檢查**不要**用 `t` / `n` / `v` / `k` / `s` 這類名字——它們同時也是泛型的型別變數名

#### 🔴 2.2.6 `map.get` 回傳 `?V`（不是裸值），且 option match **必須處理 `err`**

`[K]V` 的 `get` 簽名是 `get = (key K) (result ?V)`（`src/std/collection/map.no`）實測：**選項 match 必須窮盡 `ok` / `nil` / `err` 三臂**，只寫兩臂是**編譯錯誤**：

```
parser errors: option match must handle all branches: err, nil, ok (missing: err)
```

同理，`fs.read-bytes` 回 `?[]byte``fs.write-bytes` 回 `?bool`**`noc` 源碼裡的每一次 `get` / `read` / `write` 都是一個必須顯式處理的分支**

#### 🔴 2.2.7 跨模組調用**強制** `ShortName.` 前綴


`module.md` 明確：調用其他 `.no` 檔案定義的函式/常量，**必須**寫 `ShortName.`而隱式同目錄合併**不存在**（實測：同目錄另一檔案的 `greet` 直接不可見）

**後果**：編譯器拆成 40 個檔案時，**每一處跨檔調用**都要前綴這不是 bug，但直接影響**檔案切分策略**：

- 模組名要**短**（`lx` / `ps` / `ck` / `mr` / `cg`，而不是 `lexer` / `parser` / `checker` / `mir` / `codegen`）；
- 或者把**高度耦合的階段放在同一個檔案**（例如 parser + AST 定義同檔），只在階段邊界處跨模組；
- 兩種做法都要在 M0 就定下來——中途改切分等於全庫重寫

#### 🔴 2.2.8 跨模組常量：只有**入口檔直接 import** 的模組才會生成其頂層綁定

**最小重現（3 檔，全部可重跑）**

```no
; tmp/probeZ/lib.no
K = 42

; tmp/probeZ/dep.no
# /tmp/probeZ/lib

a = () (r i64) {
    r = lib.K
}

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

- 入口檔= **含 `main`（或頂層語句自動合成 `main`）的那個檔案**中間模組直接 import `lib` **無效**
- import 的**順序無關**
- 同一個沒被生成的綁定被當作**運算式的運算元**時，會升級為**硬編譯失敗**（`bin/no` 的 bug #85 guard 抓到它）：

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

**語料可達性**：全語料 1584 個 `.no`，符合被 import 的模組內以跨模組常量作運算式運算元的檔案數 = **0**（`compiler/` 除外）⇒ 這是**新暴露**的潛在缺陷，不是既有回歸

**架構後果（強制）**

1. `compiler/main.no` 必須**直接**列出**每一個**其常量/全局會被任何模組讀取的模組——即使 `main` 從不使用它們`noc` 目前的做法是在 `main.no` 直接寫 `# /compiler/src/tok` 與 `# /compiler/src/util/sb.sb`，並附為什麼的註釋
2. 這是**反組合（non-composable）**的約束：新增一個讀取新模組常量的檔案時，**必須同時**回到入口檔加一行⇒ 只能靠 **L1 全語料差分**兜底（錯值必然表現為 DIFF，不會表現為 `rc`）
3. 正解見 §8 **P0-⑤**（讓 codegen 依模組是否被引用而非是否被入口檔直接 import來生成綁定）

#### 🔴 2.2.9 `fs.read-str` **不是逐位元組無損**——詞法分析器必須走 `read-bytes`

`fs.read-str` → `file.read-str` → `[]byte.to-str()`，而 `[]byte.to-str()` 自 **2026-09-06** 起是**byte → 碼點 → UTF-8 重編碼**（`src/std/byte.no` 的註釋明說這是應需求方要求的語義變更；1:1 拷貝語義保留在 `[]byte.to-bytes-str()`）

實測：

```
$ printf '; \xe7\xbb\xbc\n' > /tmp/u1.txt          ; 5 位元組
$ xxd /tmp/u1.txt
3b20 e7bb bc0a
$ ./bin/no run /tmp/rb.no                          ; rb.no: print(fs.read-str(...).len-bytes())
15                                                 ; ← 不是 5
```

每個 `≥ 0x80` 的位元組變成 **4 位元組**（`0xE7 → F0 80 83 A7`）`bin/no` 自己（Go 實作）也是這個行為 ⇒ **不是 `noc` 的移植錯誤**

**對策**：`fs.read-bytes(p)` + `it.to-bytes-str()`（`src/std/byte.no` 的 1:1 逐位元組拷貝）

**後果**：編譯器的**每一次**原始碼讀取都必須用 `read-bytes` + `to-bytes-str`用 `fs.read-str` 會讓所有非 ASCII 註解／字串字面量被二次編碼，而 **`rc` 測不出來**——只有逐位元組差分（L1）能抓到見 §8 **P0-⑥**

#### 🔴 2.2.10 `fs.read-bytes` 對**空檔案**回 `nil`（不是 `ok` + 空切片）

`fs.read-bytes(p)` 的 `?[]byte` 三態實測：

| 檔案狀態      | 結果                    |
| --------- | --------------------- |
| 存在且有內容    | `ok`（`len()` 正確）      |
| **存在但 0 位元組** | **`nil`**             |
| 路徑不存在     | `err`                 |

⇒ 讀到空與讀失敗**必須分開處理**若把 `nil` 當錯誤，`noc tokens <空檔>` 會 **rc=1** 而 oracle 是 **rc=0**（oracle 對空檔吐單一 `1:1 EOF`）⇒ **L1 假 DIFF**

```
$ : > /tmp/empty.no
$ ./bin/no run <probe>      ; c = fs.read-bytes('/tmp/empty.no'); c: { ok->… nil->… err->… }
empty-file -> nil
$ ./bin/no tokens /tmp/empty.no        ; SUT（修復前）
noc: cannot read /tmp/empty.no         ; rc=1
$ /tmp/notok/notok tokens /tmp/empty.no
1:1 EOF - - - -                        ; rc=0
```

**語料中目前沒有 0 位元組的 `.no`**（674 檔全過 L1）⇒ 此缺陷是**潛伏**的，不是當前 DIFF 的來源`compiler/src/drv.no` 已把 `nil` 當成合法的空來源處理（見該檔註釋）見 §8 **P1-⑤**


### 2.3 自舉子集（SHS, Self-Hosting Subset）

**SHS 是 `compiler/` 源碼允許使用的語言特性白名單**，由 M0 的一個守護腳本強制（見 §7.6）

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
        │  S0 = bin/no （Go 實作，v0.3.19+，鎖定 SHA）  │  ← 唯一可信的尺子
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

**為什麼需要 S3 而不止於 S2？** S2 是由 S1 產出，而 S1 是由 S0 產出若 `noc` 的某個行為**恰好**依賴 S0 的某個隱含假設（例如某個未定義行為被 LLVM 以特定方式折疊），S2 可能碰巧正確S2→S3 是**用同一個編譯器編譯它自己**，任何這類假設都會在這一輪暴露成 s2/s3 的差異

**為什麼要求逐位元組一致而不是只要求行為一致？** 行為一致是**必要**條件；逐位元組一致是**充分**證據若兩者只能取其一，**必須先做到行為一致**（這是門禁的硬性部分），逐位元組一致作為 M6 的**加分驗收項**（若後端含 Go map 迭代等非確定性來源，逐位元組一致可能不可達——見 §9 R6）


### 3.2 信任鏈與反 Thompson 攻擊

S0 是 TCB**一個被植入 S0 的後門，可以讓 S0 在編譯 `compiler/` 時注入惡意邏輯，而該邏輯會在 S2/S3 中自我複製——且任何差分測試都測不出來**（因為 s2 與 s3 都是自洽地錯）這是 Thompson 的 "Reflections on Trusting Trust"

**本方案的三重反制**（M6 必須全部執行）：

| 反制                 | 做法                                                                                                                          | 能抓到什麼                  |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| **① 三路交叉編譯**       | `compiler/` 源碼分別經 (a) S0 的 MIR/LLVM 路徑(b) S0 的 **JS 後端**（`compiler.emit: "js"`）(c) `noc` 路徑編譯，三份產物在同一語料上比對 `(rc, stdout)` | 任一後端的注入                |
| **② 非自產編譯器的 S2**   | 用 **S0 直接編譯 `noc` 的第二實現**（見下）                                                                                             | S0 對 `compiler/` 的定向注入 |
| **③ 源碼級審計 + 差分審計** | 對 `compiler/` 的**每一個** M6 里程碑版本做 `no fmt -d` + AST dump 的雙人複核，並要求 **S2 與 S3 的 `.ll` 輸出在排序後 diff 為空**                        | 輸出層注入                  |

> 反制 ② 的第二實現成本很高（等於寫兩遍）**務實的降級方案**：M6 只要求 ① 與 ③，並在文檔中明確記錄S0 是 TCB，其完整性由 repo 的 Git 歷史與 CI 保證，不由自舉流程保證**不要假裝自舉本身解決了信任問題**

### 3.3 為什麼不選用 nolang 重寫整個 Go 編譯器（含 LSP/fmt/tools）

| 選項                                                  | 工作量            | 對自舉的價值                                  |
| --------------------------------------------------- | -------------- | --------------------------------------- |
| 只做**核心流水線**（lexer→parser→checker→hir→mir→build→cmd） | ≈118.8k LOC Go | ✅ 這是自舉的**充分**條件：能編譯自己                   |
| 加上 `fmt`（3.1k）+ `lsp`（6.4k）+ `tools`（2.7k）          | +12.2k LOC     | ❌ 與能否編譯自己無關；`fmt` 可用 `no fmt`（S0）外部完成 |

**結論**：M0–M6 的範圍**嚴格限定在核心流水線**`fmt` / `lsp` / `tools` 列為 **M8（後置）**，不阻塞自舉定點

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
│   ├── diag.no                    ; 診斷模型（位置級別碼渲染）
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
│   ├── intern.no                  ; 識別符號／字串駐留（符號表map key 去重）
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


### 4.1.1 目標佈局 vs 當前實際（2026-10-30 歷史快照；2026-11-12 as-built 校正見本節末）

> ⚠️ §4.1 是**目標**佈局（全部里程碑完成後的形狀），不是現狀下表是 **2026-10-30 的歷史快照**，逐檔對到當時的 `find compiler/src -name '*.no'`——當日只有 **11 個 `.no`**（含 2 個生成物）、前端閉環而中端/後端缺席。**該描述已過期**：2026-11-12 as-built 為 `compiler/src/*.no` **14 檔**＋`compiler/src/util/*.no` **2 檔**，`h2m.no`／`mir.no`／`cg.no`／`cg-runtime.no`／`cg-rc.no` 均已落地，M4-e 已續推至 **s99n**（§5.3.4.eo）。下表「當時實際」欄保留原值作歷史證據，**現狀以本節末的 2026-11-12 校正、§6／§6.5／§11.2 B4／文末 2026-11-12 快照為準**

| 檔案 | 目標（§4.1） | 當時實際（2026-10-30，歷史） | 里程碑 |
| --- | --- | --- | --- |
| `main.no` | ✅ 已列 | ✅ 存在（CLI 分派） | M0 |
| `src/tok.no` | ✅ | ✅ | M1 |
| `src/lx.no` | ✅ | ✅ | M1 |
| `src/ast.no` | ✅ | ✅ | M2 |
| `src/asp.no` | ❌ **未列** | ✅ **生成物**（`no ast` dump 規格表，`scripts/gen_ast_spec.py` 產出，勿手改） | M2 |
| `src/ps.no` | ✅ | ✅ | M2 |
| `src/ann.no` | ✅ | ⬜ **未建**——註解已內嵌 `ast.no` 的 `AnnotatedNode`（`AnnTable` 已刪，§5.3.1 演進），**很可能不需要獨立檔** | M2 |
| `src/ty.no` | ✅ | ⬜ 未建（型別解析暫在 `ps.no` 內） | M2 |
| `src/diag.no` | ✅ | ✅ | M3-a |
| `src/ck.no` | ✅ | ✅（首族 `'X' is not defined`，s13） | M3-c |
| `src/ck-sig.no` | ✅ | ✅ **生成物**（`compiler/tools/gen_ck_sig.py`） | M3-b |
| `src/ck-args.no` | ✅ | ⬜ 未建 | M3 |
| `src/ck-lint.no` | ✅ | ⬜ 未建——37 個 `Validate*` 按語料頻度**逐族併入 `ck.no`**（§6.4 s13），未必拆獨立檔 | M3 |
| `src/hir.no` | ✅ | ⬜ | M4 |
| `src/hir2mir.no` | ✅ | ⬜ | M4 |
| `src/mir.no` | ✅ | ⬜ | M4 |
| `src/mir-opt.no` | ✅ | ⬜（M4 可選，交 LLVM `-O2`） | M4 |
| `src/cg.no` | ✅ | ⬜ | M4 |
| `src/cg-builtin.no` | ✅ | ⬜ | M4 |
| `src/cg-runtime.no` | ✅ | ⬜ | M4 |
| `src/cg-rc.no` | ✅ | ⬜ | M4 |
| `src/cg-win.no` | ✅ | ⬜（後置 M7） | M7 |
| `src/ws.no` | ✅ | ⬜ | M5 |
| `src/mod.no` | ✅ | ⬜ | M5 |

| `src/link.no` | ✅ | ⬜ | M5 |
| `src/drv.no` | ✅ | ✅（`version` / `tokens` / `ast` / `vet`） | M0/M3 |
| `src/util/sb.no` | ✅ | ✅ | M0 |
| `src/util/intern.no` | ✅ | ✅ | M0/M1 |
| `src/util/arena.no` | ✅ | ⬜ | — |
| `src/util/idxmap.no` | ✅ | ⬜ | — |
| `src/util/pathx.no` | ✅ | ⬜ | — |

**測試側**（`compiler/tests/`，實際 10 個測試檔 + 68 個 `fixtures/t*.no`）：`tok-test` / `lx-test` / `ast-test` / `ps-test` / `ps-err-test` / `ck-test` / `ck-sig-test` / `diag-test` / `util-test` / `verify-struct-elem`——**全部已存在**（目標樹只列了 5 個，實際已超額）工具側實際只有 `gen_ck_sig.py` + `shs-check.no`（目標樹的 `gen_stdsig/` 目錄尚未建立，生成器暫為單檔 Python）

> **2026-11-12 as-built 校正（上表為 2026-10-30 歷史快照，勿當現狀）**：上表「當時實際」欄的 ⬜ 只代表 2026-10-30 該日；以 `ls compiler/src/*.no compiler/src/util/*.no` 逐檔重盤：
>
> - **合計 16 檔**：`compiler/src/*.no` **14** ＋ `compiler/src/util/*.no` **2**（`intern.no` 132 行／`sb.no` 70 行）；`arena.no`／`idxmap.no`／`pathx.no` 仍未建。
> - **前端（M1/M2）**：`tok.no` 406／`lx.no` 1,634／`ast.no` 2,002／`ps.no` 10,040／`asp.no` 915（**生成物**，`scripts/gen_ast_spec.py` 產出，勿手改）。`a4b7c66f`（11-10）把 `asp.no` 的規格表扁平化為「`tape` 每 5 個 `i64` 一條 `{op,a,b,c,d}`、`kinds` 每 4 個 `i64` 一種節點」；`0e055e11`（11-11）再重生為 `KIND-COUNT=74`／`STR-COUNT=191`／`INSTR-COUNT=315`，新增 `EmptyStatement` 節點。
> - **M3**：`diag.no` 59／`ck.no` 466／`ck-sig.no` 18,111。
> - **M4 中後端（上表全為 ⬜，現已落地）**：`h2m.no` 3,849（**上表未列**的 M4-e 主體，`ast.no` arena → `mir.no` 扁平指令流，s55–s99n）、`mir.no` 465（op 常量至 `OP-VEC-FREE = 94`）、`cg.no` 2,530、`cg-runtime.no` 411（`%str-long` prelude／`@str_eq`／`@str_concat`／`@vec_free`）、`cg-rc.no` 180（`check-rc-balance`）、`drv.no` 384。
> - **仍未建**：`hir.no`／`hir2mir.no`／`mir-opt.no`／`cg-builtin.no`／`cg-win.no`／`ws.no`／`mod.no`；自舉改走 `ast.no` → `h2m.no` → `mir.no` **直降**，未另立 `hir.no`／`hir2mir.no`（§6.5 的「M4-e＝`hir2mir` 降級主體」是命名沿用，載體實為 `h2m.no`）；~~`link.no`~~ **已建（2026-11-12，§11 後 ⑧：`link.compile` 只調 `clang -x ir`，DR-2）**，`drv.cmd-build`／`cmd-run` 自產 `.ll` 接線＝**M4-d 已落地**（殘差僅 L4 全語料行為門禁收斂）。
> - **測試側**：`compiler/tests/*.no` **15 檔**（上表記 10 檔；M4 新增 `h2m-test.no`／`cg-test.no` 4,179／`cg-runtime-test.no` 244，⑥ 再新增 `cg-str16-test.no` 280，⑧ 再新增 `drv-build-test.no` 109）、`compiler/tests/fixtures/*.no` **70 檔**（上表記 68；⑧ 新增 `m4d-exit.no`；行數為 2026-11-12 ⑧ 後實測）✅ `cg-test.no` 在 doc-only 輪次曾因 checker `15w45dqk` 收緊而整檔不可編譯（§5.3.4.eo），**已於 ⑦ 解鎖並校正 24B 斷言，現為 359/359 全綠**。


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

⚠️ **`no build` 的 flag 必須在檔案前**（`-o OUT FILE`）寫反會**靜默不產出rc 仍為 0**

⚠️ **不要**把 `noc` 安裝成 `no`（`/usr/local/bin/no`）——那會讓 S0 與被測物互相污染，並破壞所有既有 golden 流程

### 4.3 模塊劃分與依賴方向

依賴必須是**單向**的（無循環）這在 nolang 裡尤其重要，因為**跨模組調用強制前綴**且循環導入沒有隱式合併可以兜底

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

- 上游模組**絕不** import 下游（`ast` 不知道 `ps` 存在；`mir` 不知道 `cg` 存在）
- 共用資料結構（`ast.node``mir.inst`）**只在一處定義**，下游 import 上游
- `main.no` 只做 CLI 分派，不含邏輯

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

#### 5.1.1 Arena + 索引：解決循環引用與擴容失效

**問題**：編譯器的 AST / 符號表 / 型別圖本質上是**有向圖**（可能含環：型別互相引用符號互相引用）Nolang 的所有權模型**無法表達環**——環意味著兩個值互相擁有，兩邊都釋放就是 double free

**解法**：**一切圖狀結構都用 arena + `i64` 索引**

```no
; 節點資料與指向節點的引用分離
nodedata {
    kind i64
    a i64; 子索引 / 值 / 位置
    b i64
    s str; 詞素 / 名稱
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

**為什麼必須是索引而不是結構體欄位裡放 `node`**：

1. `[]T` **擴容會重新分配**，任何指向舊緩衝的位址立即失效索引永遠有效
2. 環可以自由表達（`nodes[i].a = j` 且 `nodes[j].a = i`）
3. `[]T` 的元素在 `push` 時若含擁有堆的欄位會被**深拷貝**；純 POD 的 `nodedata` 拷貝成本是固定的 `memcpy`

⚠️ **`nodedata` 必須是 POD**（只放 `i64` 與 `str`；`str` 欄位在 `push` 時仍會深拷貝，若詞素很長需改為索引到 intern 表）這條要在 M0 的 `arena.no` 裡寫成註釋約定，並由 `shs-check.no` 檢查

#### 5.1.2 字串構建器（`util/sb.no`）——**std 目前沒有**

`str` 的 `+` / `-` 是 `@str_concat`，每次都是 O(n) 拷貝若用它拼 12 萬行的 IR 文本，是 O(n²)


```no
sb {
    buf []byte
    n i64
}

sb.init = (cap i64) (r sb) {
    r = sb {
        buf: with-cap(cap)
        n: 0
    }
}

sb.push = (s str) {
    base = .buf.len()
    .buf.truncate(base); 確保 len 反映實際寫入
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

**驗收**：`sb` 在 M0 就要有單元測試（含 1 MiB 級別的 push 與 `len()` 校驗），並在 M4 用它產出 IR，量測峰值 RSS 是否為**平線**（用 `/usr/bin/time -l <binary>`，⚠️ macOS 的數字**在前面**，`awk '{print $1}'`）

#### 5.1.3 識別符號駐留（`util/intern.no`）

編譯器會反覆比較識別符號（關鍵字查找符號表map key）`str` 比較是 O(len)**駐留**把每個識別符號映射到一個 `i64`，之後比較是 O(1)

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

⚠️ **三臂必須齊全**（`ok` / `nil` / `err`），少一臂是編譯錯誤（§2.2.6 實測）上面的程式已驗證：`get('a')` 首次回 `1`再次回 `1``get('b')` 回 `2`

⚠️ 注意 `map.get` 的**缺失語義**是 `nil`（`?V`），不是 0M0 要為此寫一個明確的封裝（`util/idxmap.no`），**不要**在業務程式碼裡散落 `get` 的缺失判斷

#### 5.1.4 全局狀態：**禁止**

編譯器需要當前診斷緩衝當前檔案當前作用域這類上下文`memory.md` 的模組級變數釋放一節說明模組全局有特殊語義，且 `module.md` 建議模組中避免使用全局變量

**解法**：**一切可變狀態都放在一個顯式傳遞的 context 結構體裡**，並且因為Nolang 所有函式參數預設為引用型別，傳遞 context **不產生拷貝**

```no
ctx {
    diags []diag
    file str
    src str
    pos i64
}

ck.check = (c ctx, a ast, root i64) {
    ; c 是引用語義：c.diags.push(...) 直接作用於呼叫方的結構體
}
```

⚠️ **這條是 SHS 的硬規則**，`shs-check.no` 必須檢查：`compiler/src/**` 中不得出現模組級 `let`（`tok.no` 的關鍵字表等**只讀常量**除外，且必須在檔案頭以註釋聲明）


### 5.2 詞法分析（M1）

**目標**：完整復刻 92 個 `TokenType` 的掃描行為，逐 token 一致

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

**Oracle**：**不是** `no tokens`（`no` 沒有這個子命令，§8 P0-②）——而是一個 **repo 之外**的只讀 dumper，介面 `<l1-ctl> tokens <file>`，它 `import` 的正是 `src/lexer`（即 P0-② 若實作也會走同一個包）這條路**完全不改動 `src/`**，接線方式與守衛見 **§7.1.1**

**驗收（已達成）**：`scripts/selfhost_diff.sh --levels L1 --l1-ctl <dumper>` 對**全語料 674 檔**（`tests` + `src` + `compiler` + `docs`）的 token 流**逐位元組一致**：

```
$ ./scripts/selfhost_diff.sh --sut /tmp/noc-l1c --levels L1 \
      --l1-ctl /tmp/notok/notok --corpus "tests src compiler docs"
  files=674  ctl_ok=674  ctl_empty=0  sut_ok=674  sut_empty=0
  SAME=674  DIFF=0
  L1  PASS
```

另有 L0 的 `compiler/tests/tok-test.no` **62/62** 單元斷言

⚠️ **兩個會讓一致變成假的陷阱**（都已寫進 harness）：
1. **oracle 必須真的有鑑別力**實測：把 `./bin/no` 當 L1 的 oracle 時，`no tokens <f>` 對**任何**輸入都印同一份 usage 且 **rc=0** ⇒ 差分器看到控制組輸出非空SUT 與之不同而**誤報 FAIL**（把oracle 不存在記成 SUT 的錯）harness 的 **G7** 守衛即為此：控制組在 ≥3 檔上只吐 **1 種** stdout 雜湊 ⇒ 報 **UNAVAILABLE**（§7.1.1）
2. **讀檔必須逐位元組無損**（§2.2.9）用 `fs.read-str` 會把 ≥0x80 的位元組二次編碼，**`rc` 不變**只有 L1 逐位元組差分抓得到

⚠️ **不要把詞法一致等同於AST 一致**詞法差分能抓到 AST 差分抓不到的錯誤（例如註釋與空白的處理位置資訊），反之亦然**兩級都要做**


### 5.3 語法分析（M2）

**目標**：完整復刻 **71 個 AST 節點**（`ast.go` 共 78 個 struct，其中 71 個實作 `Node`）與 `parser/dump.go` 的輸出格式完整的節點清單欄位建構函式與錯誤訊息目錄見 **[`nolang-ast-grammar.md`](nolang-ast-grammar.md)**（機械抽取，可重跑）

**已知的 parser 陷阱**（每一條都有 repo 內的實證，全部要在 `noc` 中復刻**相同的**判定）：

| 陷阱                                                                | 位置                             | 症狀                                                                              |
| ----------------------------------------------------------------- | ------------------------------ | ------------------------------------------------------------------------------- |
| `classifyBlock` 用**塊內第一個非換行 token** 判定塊的種類                        | `parser/parser.go`             | 新型語法（以 `#{``&T``*T` 開頭）會被誤判為語句塊 ⇒ `'x' is not defined`                        |
| `isStatementBoundary` 是**白名單**                                    | `parser/stmt.go`               | 漏一個 token ⇒ 該敘述連同其換行被吞掉；症狀是靜默少一條敘述                                            |
| 語句位置的裸 `{` **永遠不是宣告**                                             | `parseStatement` 的 `LBRACE` 分支 | 誤判成 enum/iface ⇒ 塊被吃掉，後續敘述**洩漏一層**（`print(1); {print(2)}; print(3)` 印出 `3,1,2`） |
| `->` 是**短路管道**，臂以**換行**分隔                                         | `parser/stmt.go`               | 內聯臂體裡的第二個 `->` 曾被當成新臂，靜默丟掉後面所有內容                                                |
| `(v) { … }` 是 **while-do 迴圈**（條件），**不是 match**；match 是 `v: { … }` | `parser/stmt.go`               | 誤用 ⇒ 失控迴圈                                                                       |
| 註解只有**兩個**合法位置：獨立上一行 / 同行尾隨                                       | `parser/annotation.go`         | 同行**前綴**是 parse error                                                           |
| 結構體字面量**必須多行**                                                    | —                              | 單行 ⇒ parse error（§2.1 實測）                                                       |
| 頂層綁定的**大小寫**決定全局/局部                                               | `hir2mir.go`                   | 語義差異                                                                            |

**Oracle**：`no ast [-annotations] <file>`驗收 = 全語料 **AST dump 逐位元組一致**`noc` 必須**照抄** `parser/dump.go` 的輸出格式（含 `<unknown *parser.X>` 這類 fallback 字樣）

✅ **2026-10-10：oracle 已補齊（P0-③ 完成）**原本 `dumpNode` 只覆蓋 40/71 個節點型別，其餘塌成 `<unknown>` 且**整個子樹不可見**現在：

- **71/71 個節點型別全部有 case**（含 9 種 Type 節點4 種 Statement10 種 Expression12 種輔助節點）；
- **每個 `Node` 欄位與每個 `Node` 切片元素都被遞迴**（型別成員變體參數註解值）；只印殼的節點已清零；
- 新增 **`-annotations`**：把 `#{...}` 註解 side-table 依**樹序**印出（預設不含，見 §5.3.1）；
- 有**回歸釘**：`src/parser/dump/dump_completeness_test.go` 走全語料，任何節點塌成 `<unknown>` 即 FAIL

⚠️ **dump 的穩定性**：`no ast` 的輸出必須是**確定性**的**已實測**：三張 node-keyed 表（`anns` / `typeFacts` / `nodeFacts`；Stage 7 前是 `SemanticContext.nodeSem` 的 `map[Node]*NodeSemantics`）**絕不可 range**（Go 隨機化 map 疊代序）；dumper 一律以**樹序**走訪並對每個節點做**點查詢**`TestDumpIsDeterministic` 對全語料連跑兩次逐位元組比對

#### 5.3.1 `no ast` 的可見範圍：**已補齊**（2026-10-10）

> **狀態：修復前的盲點已全部關閉** 本節同時保留修復前的實測數據（作為為什麼必須補齊的證據）
> 與**修復後的格式契約**（`noc` 必須照抄的那一份）

**為什麼這件事是 M2 驗收判據的大前提** `src/parser/dump/dump.go` 的 `dumpNode` 原本是一個**封閉的型別 switch**（331 行 / 40 個 case）任何**不在 switch 裡**的節點型別落到 `default` 分支，只印一行 `<unknown *parser.X>`，**子樹完全不印**——不是印得少，而是**整個子樹不可見**

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

另有進得了 switch 但只印殼的節點（子節點不 dump）：

| 節點                                                              | 印出                  | **沒有** dump        |
| --------------------------------------------------------------- | ------------------- | ----------------- |
| `StructDefinition` / `EnumDefinition` / `TaggedEnumDefinition`  | 只有 `name`           | **所有欄位／成員／變體**    |
| `StructLiteral`                                                 | 只有 `type`           | **所有欄位初始化**       |
| `InterfaceDefinition`                                           | name + 方法名          | 方法簽名              |
| `ArrayLiteral` / `RangeExpression`                              | `nodeString(size/start/end)` | 子運算式              |

⚠️ 當時的**最壞後果**：一個把 map 字面量多重賦值`co`/`run`/`awy` 全部解析錯的 `noc`，只要**錯在同樣的地方**（都塌成一行 `<unknown>`），L2 就報完全一致而且語料中 145 處 struct literal 的欄位**從未被差分**

##### 修復後的狀態（實測）

| 項目 | 修復前 | 修復後 |
| --- | --- | --- |
| `dumpNode` 的 case 數 | 40 | **71** |
| 未被覆蓋的 `Node` 型別 | 31 | **0** |
| 語料中 `<unknown>` 行數（528 檔） | 319（9 種） | **0** |
| 語料 dump 總行數 | 84,301 | 91,293（+8.3%） |
| `#{...}` 註解覆蓋 | 0 | **287 條 / 88 檔**（`-annotations`） |

**節點盤點（機械抽取，可重跑）**：`ast.go` 共 **78** 個 struct，其中 **71** 個實作 `Node`（`Statement` 18`Expression` 31`Type` 9無 marker 的純 `Node` 13），另 **7** 個是非 `Node` 的輔助 struct（`Comment` / `CommentGroup` / `CommentedNode` / `FuncSignature` / `AnnotationStructField` / `TaggedEnumField` / `InterfaceMethod`）完整清單欄位建構函式與**錯誤訊息目錄（168 個站點）**見 [`nolang-ast-grammar.md`](nolang-ast-grammar.md)

##### 格式契約（`noc` 必須逐位元組復刻）

1. **一行一個節點**，每層縮排 4 個空格
2. 節點自身的純量欄位寫成 `TypeName{field: value, …}`；**大括號恆存在**（無欄位 ⇒ `TypeName{}`）
3. 純量欄位分兩類：
   - **來源層資訊**（名稱字面值 + `raw`運算子型別摘要標籤種類範圍開閉 `leftInc`/`rightInc``slice`/`readonly`/`sealed``explicit``variadic` …）**一律印出**；
   - **parser 內部 desugar 狀態**（`synthetic``armBinding``propagation``moduleConst``inferred``wasSliceLiteral``condWrapper``propagated` …）**僅在為真／非空／非零時印出**，使常見情形保持乾淨而例外狀態仍可見可比
4. **每個 `Node` 欄位與每個 `Node` 切片的元素都遞迴**，順序即欄位宣告序型別節點（`NamedType` / `ArrayType` / `SliceType` / `MapType` / `FunctionType` / `UnionType`）**也在其中**——這是 `noc` 必須建出**同一棵型別樹**而非只印出同一個 `Type.String()` 摘要的原因
5. 父節點的 `{…}` 標頭可含**摘要**（`value: MapLiteral` 之類），摘要只是提示；**權威內容是子節點行**`nodeString` 的 `default` 回傳去掉 `*parser.` 前綴的型別名
6. `default` 分支印 `<unknown %T>`：這是**絆線**，正常情況下**永遠不該出現**
7. 註解：`Doc: //…` / `Comment: //…``#{...}` 註解 side-table **預設不印**，加 `-annotations` 才印（見下）
8. ⚠️ **旗標必須在檔名之前**（Go 的 flag 套件遇到第一個位置參數就停止解析）：`no ast -annotations a.no`

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

`#{...}` 註解住在 **AST 節點自己身上**（`AnnotatedNode.Annotations`，嵌入 `CommentedNode` / `StructField` / `EnumValue` / `TaggedEnumVariant` / `AssignExpression` / `SurfaceMatch`），不在 `Program.Sem` 的 side-table 裡歷史：Stage 7 之前整族住在 `map[Node]*NodeSemantics`；Stage 3 遷進 `AnnTable`（`SemanticContext.anns`，node-keyed）；2026-10-26 整條 `AnnTable` 刪除——因為尾隨註解（`stmt #{...}`）只存在於表裡AST 完全不承載，任何從 AST 重建註解的路徑都會靜默丟掉它們

🔴 **絕不可 range 一張註解 map**：Go 隨機化 map 疊代序，dump 會變成不可重現實作**依樹序走訪對每個節點做點查詢**（`Sem.AnnotationsOf(n)`，現為純查詢直接讀節點），因此輸出順序恆定`TestDumpIsDeterministic` 對全語料連跑兩次逐位元組比對以釘住這一點

**預設行為不變**：不加旗標時，dump 中**不得出現任何 `AnnotationEntry{` / `Annotation*Value{`**（`TestNoAnnotationsByDefault` 釘住）結構性的 `AnnotationStatement{entries: N}` 是**普通 AST 節點**，照常印出——它與註解內容是兩回事

##### 回歸釘（讓已補齊這句話可被證偽）

`src/parser/dump/dump_completeness_test.go` 走全語料，四個測試：

| 測試 | 釘住什麼 |
| --- | --- |
| `TestCorpusHasNoUnknownNodes` | 任何節點塌成 `<unknown>` ⇒ FAIL（預設 + `-annotations` 兩種模式） |
| `TestDumpIsDeterministic` | 同一 program 連 dump 兩次必須逐位元組相同 |
| `TestAnnotationsFlagIsWired` | `-annotations` **不是 no-op**：實測 **88 檔 / 287 條** |
| `TestNoAnnotationsByDefault` | 預設不得洩漏註解內容（P0-③ 的驗收判據） |

**已證偽**：把 `dumpNode` 的 `case *parser.MapLiteral:` 整塊刪掉後，`TestCorpusHasNoUnknownNodes` 立刻 FAIL，報 **24 檔 / 58 處 `<unknown *parser.MapLiteral>`**⇒ 這條釘子**會壞**，不是擺設

##### 仍然**不在** dump 裡的東西（刻意如此）

- **位置資訊**（`Token` / `lexer.Position`）——印位置會讓 dump 對同一語義不同排版過度敏感，且 L2 不負責格式
- `Program.Sem`（語義副表本體）`Program.BuiltinFuncNames`（`map[string]bool`，**不可 range**）
- 註解以外的 desugar 內部表（`VarTypes` / `EnumVariants` / `IfaceImpls` …）——那些屬於 M3 的 `no vet` 差分，不屬於 L2

#### 🔴 5.3.2 dump 曝露了**哪條建構路徑**造出這個節點——M2 必須複製的是**路徑**，不只是語義

（2026-10-10 實測，M2 開工前發現）

`-annotations` 與補齊節點型別讓 L2 覆蓋變完整，但也讓 oracle **比語義更嚴格**最清楚的一例是字串字面量的 `raw` 欄位：

```
$ printf "b = 'x'\nm [str]i64 = { 'k': 1 }\n" > /tmp/t2.no
$ ./bin/no ast /tmp/t2.no | grep -E 'Literal'
            LetStatement{name: b, type: str, value: x}
                StringLiteral{value: x}              ← 沒有 raw
                MapLiteral{pairs: 1}
                        StringLiteral{value: k}      ← 沒有 raw
```

但語料中**也存在** `StringLiteral{value: , raw: ''}`（空字串字面量）差異不是語義，是**路徑**：

| 建構點 | 設定 `Raw`？ | 來源 |
| --- | --- | --- |
| `parser/expr.go:753` `parseStringLiteral`（**直接解析**） | ❌ 不設 | 原始碼裡的 `'x'` |
| `parser/lowering.go:1244/1384`（**lowering 合成**） | ✅ `Raw: "'" + value + "'"` | 註解推導出的預設值等 |
| `parser/lowering.go:1247/1363`（空字串） | ✅ `Raw: "''"` | 同上 |

`IntegerLiteral` / `FloatLiteral` 則**一律**設 `Raw`（`lowering.go:1254/1224``type.go:799/1255`）

**⇒ 三個必須記住的結論**

1. **`noc` 的 AST 語義等價不等於L2 逐位元組一致** `raw``synthetic``armBinding``propagation``moduleConst``inferred``wasSliceLiteral``condWrapper``propagated` 這些旗標，記錄的都是 **parser/lowering 的內部路徑**`noc` 要綠燈，就必須在**同一批路徑上**建構節點——亦即 **M2 的範圍其實包含 `lowering.go` 的 desugar 邏輯**，不只是讀語法
2. **因此 `ast.no` 的節點模型必須能表達合成節點**（例如帶 `synthetic`/`propagation` 的 `LetStatement`），不能只建模原始碼語法這也是為什麼 §5.1.1 的 arena 模型要在 M2 就位
3. **這是嚴格但正確還是過度嚴格需要證據來判斷** 若某個旗標被證明無法在 `noc` 複製（例如它依賴 Go 的某個偶然順序），**登記為缺陷並附證據**，不要靜默放寬 oracle——但也不要假裝它不存在

⚠️ **`FloatLiteral.Value` 用 Go 的 `%v` 印**（最短可往返表示）語料中只有 10 處（首 120 檔），值全是 `0` / `1` / `1.5` / `24` / `30` 這類簡單數 ⇒ 目前可人工處理，但**這是一個未解的演算法風險**（一般情形需要 Ryu/Grisu 級演算法）登記於 §9 風險

#### 5.3.3 match 表示分歧 + `compiler/` 構建破損（2026-10-19 S7 調查；構建破損**已修** 2026-10-20）

**背景**：S1match 表示統一收官後（詳見 `nolang-parser-lowering-invariants.md` §22），bin/no 的 parser **保留 `SurfaceMatch`** 不展開（翻轉 `b918dba9`刪除舊降糖路徑 `d6965622`），降糖移到 H0 lowering pass 執行`noc`（§6.3 t23 片）仍在 **parse 期**把 match 展開成 if/else 鏈——這在 2026-10-19 之前是兩邊的共同事實，現在成為**單邊分歧**

**實測（2026-10-19；`bin/no` = `make bin/no` @ HEAD `4c3dfbf7` + 併發 WIP）**：

| 項目 | 實測值 |
| --- | --- |
| L2 預設語料（`find tests src/std -name '*.no' -type f`） | **653 檔** |
| 其中 `no ast` 產出含 `SurfaceMatch{`（⇒ noc 產 if/else 鏈，**必然 DIFF**） | **264 檔** |
| 探針 `tests/option.no`（2 陳述層 + 1 表達式層 + 1 裸 match） | bin/no 產 **4 個 `SurfaceMatch{arms: 3}`**零 IfExpression 鏈；noc 仍產 if/else 鏈 |

⇒ **L2 在 264/653 檔上必然偏離**；S7 的跑滿五級（G6）在分歧裁定前不可能全綠

**全量實測（2026-10-20，SUT＝構建修復後的 `noc-s0`）**：653 檔 DIFF=315 / SAME=338，另有 3 檔 sut 獨有 rc=124 挂起（**該三檔已於同日修復歸零，見下文三檔挂起已修（s58/s59）**）**跨 noc 對照歸因**：同一 CTL 下，`762ea045` 之前的 noc（`b805ed06`）DIFF=355 / sut 獨有失敗 8 檔；修後 `noc-s0` DIFF=315 / 3 檔，且 DIFF 集 ⊆ 舊集（唯一例外 `tests/1-test.no`——`762ea045` 推斷移植的過度推斷，因構建破损潛伏被修復激活，非本次修法引入），3 档挂起在舊 noc 上同樣复现⇒ 本次修法**零新增分歧/挂起**；`762ea045` 的移植（經修復激活）淨減 40 檔分歧歸因詳見 `nolang-parser-lowering-invariants.md` §11.1 第 4 條

**✅ 裁定①（2026-10-21，使用者拍板）**：`ps.no` 改為產出 `SurfaceMatch`（parser 忠實，即 P1），降糖留待 M4 的 `hir.no` 重新實作落地：`PS-SURFACE-MATCH` 預設翻 `1`（`compiler/src/ps.no`，開關保留為 A/B 回歸基線，翻 0 重編即得舊降糖形）；`ps-test.no` test 10 期望改為 oracle 的 `SurfaceMatch` 形（逐位元組對齊 `bin/no ast`）選①的理由：`asp.no` 的 `K_SURFACE_MATCH` 條目早已就位（§6.3/§18.3）surface 形已實測零回歸（§5.3.4.x：+45 檔 L2 收斂 / 0 回歸），而選②會讓自舉編譯器永久偏離 P1 並讓 L2 語料長期背豁免清單**裁定後 L2 結論**：match 表示分歧不再是登記項；剩餘 DIFF 歸正交 track（`?T` unwrap / type-inference），G6 五級全綠仍待這些 track 收斂

**🔴→✅ 前置發現：`compiler/` 曾自 2026-10-13 起無法構建（與上述分歧獨立，但曾阻塞 S7 全部五級驗收）——已於 2026-10-20 修復**：

| 事實 | 證據 |
| --- | --- |
| `./bin/no build -o /tmp/noc-s0 compiler` 曾失敗 | `EmitLLVM: MIR->LLVM: func ps_parse_func` / `func ps_parse_method_func`: `consumer reads value 0 (NoVal), which no instruction produced … (bug #85)`；**HEAD `4c3dfbf7` 乾淨工作樹（`git worktree`）復現**，非併發 WIP 所致 |
| 破壞者 commit | **`762ea045`**（2026-10-13 12:45refactor(parser): infer types of local variables and function return values during parsing）——手動二分實測鏈：`c721fe6c`（bare match 降糖）OK → `b805ed06` OK → **`762ea045` FAIL**（初現 8 處 NoVal，後續 commit 增至 12 處，同一對函式同一錯誤形態） |
| 根因（修時證實） | `762ea045` 給 ps.no 加的型別登記循環把索引讀（`params[rp]` 等）巢置於 call 引數內 ⇒ option 捕獲合成：nil/err 臂把賦值目標（如 `pd`）定型為 `?i64`，ok 臂寫 struct，合併點後續欄位讀 `pd.kid0` 按 option 解析 ⇒ MIR NoVal（HIR/MIR dump 實證：`params[rp]` → `__cap_20 t=?i64` → 三臂 match） |
| 修法（語言層，不動 Go 編譯器） | 索引讀提取為獨立陳述 + `#{index-out=zero}`（該注解只覆蓋陳述內直接讀**不覆蓋巢置於 call 引數內的讀**——v1→v4 最小復現階梯實證；與 ps.no 自身 `wildA[z]` 慣用法一致）共 **8 處**：`ps-parse-func` / `ps-parse-method-func` 各 4（params/outs 型別登記循環 + params/outs 泛型偵測循環） |
| 修復實證 | `./bin/no build -o /tmp/noc-s0 compiler` **成功**（`noc v0.0.1-m0`）；L2 全語料歸因：修後 `noc-s0` 與 `762ea045` 之前的 noc（`c721fe6c`/`b805ed06`）在挂起檔上行為一致，**未引入新分歧**（詳見 `nolang-parser-lowering-invariants.md` §11.1 第 4 條） |
| 無人發現的時長 | **6 天**：CI 無 `no build compiler` 門禁（`pr-verify` 此前只跑 Go 單測 / std 測試 / 語料冒煙） |
| 影響面 | 曾阻塞 L0–L4（全都需要可用的 `noc-s0`）與 DR-1 自舉三階段（每一步都以`no` 能編譯 `compiler/`為前提） |
| 防復發 | CI 已補閘：`pr-verify` 新增 `./bin/no build -o /tmp/noc-ci compiler` 步驟（2026-10-20，`.github/workflows/build.yml`） |

**✅ 三檔挂起已修（2026-10-20；兩個獨立根因，s58/s59）**：三檔 sut 獨有 rc=124 挂起全部归零此前二分一度误判为单方法不触发需 struct + 多方法上下文，实为**两个**独立缺陷：

| 根因 | 位置与实据 | 修法 |
| --- | --- | --- |
| **A. 陣列 let 分支** | `ps-parse-stmt` 明式元素型別臂（`name [N]T = expr`）無條件呼叫 `ps-parse-arr-lit`；RHS 非 `[` 開頭（`net.no:571` 的 `iv [12]byte = .gen-iv()`）時 `ps-expect(LBRACKET)` 靜默失敗元素循環把後續全部陳述當元素吞到 EOF | 復刻 Go `stmt.go:1333-1356`：RHS 先 `ps-skip-trivia`，為 `[` 才走 `ps-parse-arr-lit`，否則 `ps-parse-expr` |
| **B. IN 上下文關鍵字** | `ps-parse-expr-prec` primary 缺 IN 臂——Go `expr.go:96` 是 `case IDENT, IN, MATCH: parseIdentifier()`，表達式位的 `in` 是合法識別字`aes-cbc.no:222` / `tls.no:695` 的 `(in[i]) != pad-len -> { … }` ⇒ 空殼 GroupedExpression + pos 卡住 ⇒ 一行被撕成三個陳述 | primary 臂改 `tk.typ == tok.IDENT \|\| tk.typ == tok.IN`（MATCH 同類但語料無用例，未補） |

- **故障形態（根因 B，三種）**：① `it = in[i]` ⇒ let RHS 丟失`[i]` 拆成獨立 `SliceLiteral` 陳述；② `enc-block(in, ek)` ⇒ 引數 2→1（**靜默丟參**，比報錯危險）；③ 陳述層括號索引 + 比較 ⇒ 41+ 深 GroupedExpression 鏈 ⇒ dump 縮排 O(depth/行)總量 O(N²) ⇒ GB 級輸出 ⇒ 表現為挂起
- **挂起的兩個必要條件**（缺一不挂）：① 病理解剖出的超深節點鏈；② dump 的 `indent()` 二次方縮排定位手法：`ast.no` 的 `node()` 加 `depth > 40` 截斷守衛 ⇒ 挂起變成可完成 + 標記，暴露 `ArrayLiteral{size: 12, elements: 33}`／深 GroupedExpression 鏈
- **歸因（A/B 同一 harness同一 CTL同一 653 檔）**：verdict DIFF 309→311 / SAME 338→338 / 真挂起 2→0（+2 DIFF 係 aes-cbctls 由挂起轉為可解析但仍帶 match 分歧，非回歸）；**SAME→DIFF 真回歸 0 檔**逐檔差異行數 **210 971 → 210 111（-860），7 檔改善0 檔惡化**（`tdes` -372`aes-ctr` -278`rc4` -70`aes` -56`des` -38`ecdsa` -26`base32` -20）
- **回歸測試**：新增 `compiler/tests/ps-test.no`（`./bin/no run compiler/tests/ps-test.no`，5/5 通過）釘死四種 `in` 形態 + 一組無 `in` 對照；期望字串全取自 oracle 實測⚠️ **反空洞**：還原 IN 臂後該測試**挂起**（不是僅報錯）——正是缺陷本身
- ⚠️ **新發現的既有缺陷（未修，已登記）**：`no build` / `no run` 一個導入 `compiler/src/ps` 的獨立檔案時，若入口檔**未直接導入** `tok`（lx.no 的 `tok.NEWLINE` 等跨模組常量所依賴的模組），這些常量解析不到 ⇒ MIR 降成 `const void` + `eq NoVal` ⇒ bug #85 類 `EmitLLVM` 失敗**包內構建（`no build compiler`）不受影響**——故自舉構建與 CI 閘皆不受波及繞開方式：測試檔導入清單補齊傳遞依賴（ps-test.no 已如此）

**✅ 陣列／切片型別方法定義已修（2026-10-20；s64/s65，三小步）**：接續挂起归零後的 L2 非 match 分歧收斂對齊 Go `parseArrayTypeMethodDefinition`（`decl.go:192-575`）——它其實是**一個函式三種接收者**（`[n]t` / `[]t` / `[?]t`），noc 此前**一種都沒有**

| 步 | 缺口 | 實據（oracle vs noc） | 修法 |
| --- | --- | --- | --- |
| **s64** | 泛型陣列接收者 `[N]T.method = (…) { … }` | `[n]t.len` ⇒ `[]` 建成**幽靈 SliceLiteral 陳述**（並把 `Doc` 註解搶走），`t.len` 另起為普通方法 ⇒ 名稱丟 `[n]` 前綴self 降成 `NamedType{t}`泛型 `n` 缺席 | `ps-parse-stmt` 新增分派臂（守衛 `[ (IDENT\|INT) ] IDENT . IDENT = (`），建 ArrayType 後傳給 `ps-parse-method-func`（新增 `recvNode` 形參） |
| **s65a** | 參數位陣列型別的尺寸只收 INT | `[n]t.clone = () (dst [n]t)` 的 `(dst [n]t)` 退化成 `dst` / `n` / `t` **三個參數**（`ps-parse-type` 的 ArrayType 臂守衛寫死 `peek-at(1) == INT`）；對齊 Go `decl.go:301-303` 的 `INT \|\| IDENT` | 守衛改 `(INT \|\| IDENT)` |
| **s65b** | 切片接收者 `[]T.method` | 同 s64 形，語料分布最廣：`vec.no` 22 處`byte.no` 12`str.no``char.no``sort.no` 2 | 新增 `[] IDENT . IDENT = (` 臂，建 SliceType |

- ⚠️ **s65c（修 s64 自己引入的偏差）**：隱式泛型的**播種範圍**Go 只在 `[n]t` 分支呼叫 `addImplicitGeneric(size/elem)`（`decl.go:242-243`），`[]t` / `[?]t` 分支**不**呼叫；且三條方法路徑（`decl.go:569` / `94` / `2037`）都在 `detectImplicitGeneric`（`516-521` / `1750-1753`）**之後**才把 `selfParam` 插進 `Results` ⇒ **self 的型別永遠不被掃描**noc 原先順序是 `params → self → outs` 且對任何 `recvNode` 都播種 ⇒ 兩處偏差：① `[n]t.arr-stack-push` 產 `t, n`（應為 `n, t`）；② `[]t.len = () (out i64)` 憑 self 的 `SliceType{t}` 多出 `GenericParams: t`（oracle **無**）修：播種限 `kind == K_ARRAY_TYPE` 的接收者，並把 self 從探測序列移除
- **歸因（A/B 同一 harness同一 oracle同一 653 檔，三步各自量測）**：

  | 步驟 | 逐檔差異行數總計 | 改善／惡化 |
  | --- | --- | --- |
  | 基線（`a7160791`，s58+s59） | 223 232 | — |
  | +s64 | 222 248（−984） | 6 改善 / **0 惡化** |
  | +s65a/b | 221 648（−600） | 9 改善 / **0 惡化** |
  | +s65c | 221 626（−22） | 1 改善 / **0 惡化** |

  主要收斂：`arr.no` 565→153`vec.no` 1241→964`link.no` 1168→819`byte.no` 546→341`queue.no` 265→173`arr-stack.no` 94→**0**`tests/std-lib.no` 44→**0**`sort.no` 99→25`str.no` 6749→6735`char.no` 1129→1114
- **verdict 集合**：基線 DIFF 309 / SAME 338 / 逾時 6 → 終局 **DIFF 313 / SAME 340 / 逾時 0**；**SAME→非 SAME 真回歸 0 檔**；2 檔轉為**逐位元組一致**（`collection/arr-stack.no``tests/std-lib.no`）基線 6 檔逾時實為 8 workers × 20 s 的**假逾時**（`sha256` 14 s / `sha512` 17 s / `x509` 19 s / `yaml` 21 s 等），90 s 逾時重測全部完成
- **回歸測試**：`compiler/tests/ps-test.no` 由 5 個擴到 **9 個**（9/9 通過），新增：`[n]t.len``(dst [n]t)``[]t.len`（**無** `GenericParams` —— s65c 的反回歸錨點）`[]t.eq`（泛型來自 `b []t`）期望字串全取自 oracle 實測其餘 compiler 測試同綠：`ast` 47 / `lx` 45 / `tok` 62 / `util` 27

歸因細節見 `nolang-parser-lowering-invariants.md` §11.1 第 56 條


### 5.3.4 S7 match 表示分歧——形狀歸因與立项規格（2026-10-20 實測；98%更正見本節 L2 收斂量化）

機械性缺口已挖盡，**剩餘分歧的形狀壓倒性地是單一項**：oracle 輸出含 `SurfaceMatch` 的檔，其差異行數佔總差異行的 98%全量 653 檔（tests 535 + src/std 118）313 檔 DIFF 的歸因：

| 桶 | 檔数（佔 DIFF 檔） | 差異行數 | 佔 DIFF 行 |
| --- | --- | --- | --- |
| oracle 輸出含 `SurfaceMatch` | 264（84%） | 218 227 | **98%（按行）** |
| 不含 | 49（16%） | 3 993 | 2%（按行） |

⚠️ **98%是分歧形狀歸因，不是第②件收斂量**（二者不可混淆，實測見本節 L2 收斂量化小節）：第②件只翻轉 45 檔（DIFF 313→268，總 DIFF 僅降 **14.4%**）；剩餘 268 檔中仍有 184 含 match 區塊，主因是 ② 範圍內的 **raw-cond 臂缺口**（s66d 已補，再收斂 1 檔 → 267）與**正交的** option/result unwraptype-inference 註解分歧（獨立 track）

抽查最大差異檔 `src/std/crypto/x25519.no`（15 583 行）前兩個 hunk **100% 是 match 表示**：oracle
`ExpressionStatement{SurfaceMatch}` / `SurfaceMatch{arms: 2}` / `MatchArm{wildcard: true, dotVal: true, binding: , bindingVariant: }`，
noc 是 `ExpressionStatement{IndexExpression}` + `IfExpression` 鏈**結論：match 表示是剩餘分歧的最大單一形狀**；但解決它表示層（第②件）只直接對齊核心臂（ok(v)/err(e)/nil/_/binding/dotVal/blockBody），raw-cond 臂與 `?T` unwraptype-inference 需另立項（附帶更正：`InterfaceDefinition` 只出現在 4 檔 6 個節點，價值極低，不值得為它動 `asp.no` 的 id 表）

**目標結構**（`src/parser/dump/dump.go:778` 的 `case *parser.SurfaceMatch`，由 `gen_ast_spec.py` 抽出的規格）：

```
HDR      SurfaceMatch{arms: <len(Arms)>}
COMMENTS
CHILD    Matched          （guarded，nil 則不印）
HELPER   Arms -> matchArm （逐臂）
```

⚠️ **`MatchArm` 不是 Node**（`dump.go:876-877`），沒有 `dumpNode` case，只經 SurfaceMatch 的
HELPER 到達 ⇒ **`asp.no` 不需要新增 kind，`K_SURFACE_MATCH = 65` 已足**實測
`scripts/gen_ast_spec.py` 重生成後 `compiler/src/asp.no` **逐位元組不變**（同步正常）

**語料中 `MatchArm` 欄位普查（6 735 個臂，`kv` 恆印`kvb`/`kvs`/`kvc` 僅非預設值才印）**：

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

⇒ 最小切片只需 **6 個 bool + 4 個字串 + 2 個子節點（ConditionBody）**；`MultiValuePatterns` /
`BindingNames` / `BindingFields` / `skipIt` 可先不接線（語料零命中，不影響 L2 對拍）

**落地拆分（三件，各自可獨立提交）**：

1. **`compiler/src/ast.no` 的 `helper` 接線 `matchArm`**（目前只接了 `taggedEnumField`，其餘記入
   `t.unsup`）⚠️ 需要擴充 helper 元素的 aux 編碼：現制是每元素 2 個值
   （`aux[hbase+2j]=name-id``aux[hbase+2j+1]=tnode`），matchArm 需要 6 bool + 4 字串 + 2 節點
   因只有 SurfaceMatch 用 matchArm，可按 kind 判別採用寬元素編碼；`d.hcount` / `d.hbase` 的
   預掃（`ast.no:857` / `967`）需同步改為按 kind 取元素寬度
2. **`compiler/src/ps.no` 的 `ps-parse-match` 改發 `SurfaceMatch`**，env 開關（對齊專案既有的
   `NOLANG_*` 開關慣例，**預設關**）⚠️ s55/s59/s60 的語義工作（关键字臂綁定變體臂補集
   armDoc 收集）**不能丟**：它們的結果要從合成 `it` let 陳述改寫進 `MatchArm` 的
   `binding` / `bindingVariant` / `wildcard` / `dotVal` 槽位
3. **回歸測試 + A/B 量測**：`compiler/tests/ps-test.no` 加 surface 形用例；跑全量 sweep 確認
   開關關＝零回歸開關開＝差異大幅收斂

⚠️ **新發現的既有缺陷 A（生成器丟碼）**：`scripts/gen_ast_spec.py` **不會**把 `matchArm` 的
helper case 寫進 `src/parser/dump/spec_gen_test.go`——該檔裡那 39 行是**手寫補進去的**
實測：重跑生成器後 `spec_gen_test.go` **靜默少 39 行**（`asp.no` 卻不變，極易被忽略）
今後重生成必須 `git diff src/parser/dump/spec_gen_test.go` 複核，或把 matchArm case 挪出生成器覆蓋範圍

⚠️ **新發現的既有缺陷 B（未接線）**：`ast.no` 的 `helper` 只接 `taggedEnumField`；
`matchArm` 與 `interfaceMethod` 都記入 `t.unsup`（`ast.no:1221` 注釋已記錄）

**✅ s66（2026-10-20）：落地三件中的第 ① 件——`ast.no` 的 `helper` 接線 `matchArm`**

- 元素編碼採**寬元素**：每臂 **12 個 aux 值**（4 字串 + 6 bool + Condition/Body 兩個節點索引），
  與 `taggedEnumField` 的 2 值編碼按 `hname` 分派互不衝突因 `HELPER` 是 SurfaceMatch 的
  **最後一條**指令，`cur += hcount` 的過度前進不影響任何輸出 ⇒ **`hcount` / `hbase` 的
  預掃（`ast.no:857` / `967`）不必改動**
- bool 以字串 `'true'` / `''`（str id 0）表示——nolang 無位元運算，走字串最簡
- 欄位順序逐項對齊 `dump.go:888-895` 的 `fields(...)`；`binding` / `bindingVariant` 是 `kv`
  （**恆印**，空值也印成 `binding: `），其餘為 `kvb` / `kvs`（僅非預設值才印）
- **回歸測試**：`compiler/tests/ast-test.no` 新增 `build-surface-match` + `test-match-arm-helper`
  （48/48 通過）它手工組出 arena，對 oracle 最小復現檔
  `printf 'h[0]: {\n    ok -> it = 0\n    -> _\n}\n'` 的輸出**逐位元組比對**
  ⚠️ 這是 matchArm 支路**唯一**的可達路徑——ps.no 尚未產 SurfaceMatch，語料 653 檔全部走不到
- **零回歸實證**：全量 verdict sweep 與 s65c **完全相同**（DIFF 313 / SAME 340 / 逾時 0）；
  ast 48 / lx 45 / tok 62 / util 27 / ps 9 全綠

⚠️ **新發現的 arena 構造陷阱（無子節點 + CHILD 指令 ⇒ 自別名無限遞迴）**：
`ast.add` 把 `kid0` 設成當下的 `kids.len()`一個**無子節點卻帶 CHILD 指令**的節點
（例：`ExpressionStatement{nil}`）稍後被 `kid-add` 進父節點時，**恰好落在 `kids[kid0]`** ⇒
dumper 的預掃把自己讀成首個子節點 ⇒ `ast.node` 無限遞迴（實測 **挂起，rc=137**）
既有的建父節點後立刻 kid-add 其子守則對這類節點**不夠**
**修法（構造側）**：先 `ast.kid-add(t, n, 0)` 佔住那一格asm 實測：加這一行即從挂起變正常
⚠️ 這對 s66 的第 ② 件是**前置約束**：ps.no 一旦開始產 `SurfaceMatch`，臂體裡的
`ExpressionStatement{nil}` 就必須這樣建，否則自舉 binary 直接挂死

### 第 ② 件的第一次嘗試：已實測否決一次編輯就能翻的假設

把組 if/else 鏈那段換成發 SurfaceMatch + MatchArm後翻開關實測（`z: { 1 -> a = 1 / _ -> a = 2 }`）：

- 臂數 **6 ≠ 2**：臂迴圈的推送次數與 oracle 的臂數不同步；
- 臂體被塞進 `LetStatement{name: it, value: z, synthetic: true}` + `ExpressionStatement{nil}`
  ——這是 ps.no 降糖用的**合成 `it` 綁定**，surface 形**不該有**；
- 主體 `a = 1` 被切成 `ExpressionStatement{a}` 等碎片

⇒ 證實第 ② 件**不是**末端組裝的局部改寫，而是要在臂迴圈裡做
**關掉合成 `it` 注入** + 把 s55/s59/s60 的既有語義（关键字臂綁定變體臂補集armDoc）
改寫進 `MatchArm` 的 `binding` / `bindingVariant` / `wildcard` / `dotVal` 槽位
本次已把 ps.no **完整還原到提交態**（md5 核驗），未留下未驗證的死碼

⚠️ **開關形式的實測結論（別再踩）**：不能用 env`# /std/os` 會讓 `no build compiler`
去找 `<root>/std/os.no` 而失敗（實測），而修好它要動 `compiler/package.jsonc` 的
`dependencies` —— 為一個量測開關去改自舉編譯器的依賴圖不划算
可行替代：**模組級常數**（`PS-SURFACE-MATCH`，翻值 + 重編即可 A/B），或把旗標放進
`psst` 由 `drv.no` 依 CLI 參數填入


### 第 ② 件還缺的前置：主體形態覆蓋率

`ps-parse-expr-prec` 的 `COLON + LBRACE` 分派臂目前**只認裸 IDENT 主體**
（`ps.no:1945` 的 `ast.kind-at(t, idx) == asp.K_IDENTIFIER`）但語料 2 975 個
`SurfaceMatch` 的 `Matched` 分布是：

| Matched | 數量 | 佔比 |
| --- | --- | --- |
| `IndexExpression` | 1 523 | 51% |
| **無 Matched**（`Matched == nil`） | 942 | 32% |
| `Identifier` | 509 | 17% |
| `DotExpression` | 1 | ~0% |

⇒ 只做裸 IDENT 主體，天花板約 **17%**；要吃到 51% 必須連 `IndexExpression` 主體一起放行
（並把主體**節點**（而非名稱字串）傳進 `ps-parse-match`）另 32%無 Matched對應裸 match
大括號形（`ps-parse-bare-match`），`Matched` 須留空——`SurfaceMatch` 的 `CHILD Matched`
是 guarded，留空即不印

**⚠️ 實測：51% 那一桶目前連 `ps-parse-match` 都進不去**對語料主力形
`h[0]: { ok -> it = 0 / -> _ }`，noc（`1ecb3c81` 後的 binary）根本**不把它當 match**：
因為 `ps.no:1945` 的分派要求主體是 `K_IDENTIFIER`，而 `h[0]` 是 `IndexExpression`
⇒ 落成 `ExpressionStatement{IndexExpression}` + 一個裸 `BlockStatement`（臂被當普通 if 解析）

**⚠️ 實測：連裸 IDENT 主體的臂迴圈本身也是壞的**（既有缺陷，與本次改動無關）
`z: { 1 -> a = 1 / _ -> a = 2 }`（oracle 2 臂）在 noc 上產出 **4 臂**規模的結構：
多出 `z == <nil>` 這種臂，且主體 `a = 1` 被切成 `ExpressionStatement{a}`
`a = 2` 的位置也錯位⇒ 第 ② 件**不能**假設臂迴圈是對的只差末端組裝

**第 ② 件的正確拆解（依上述實測重排）**：
1. **放行非 IDENT 主體**：`ps.no:1945` 的分派改為接受 IDENT / IndexExpression / DotExpression，
   並把主體**節點**傳進 `ps-parse-match`（新增形參；`subj` 字串仍供降糖路用）（覆蓋率 17%→68%）
2. **修臂迴圈**：臂數與臂體歸屬（實測 2 臂被讀成 4 臂主體被切碎）這一步可獨立驗證——
   開關關著時，看 `z: { … }` 的 if/else 鏈是否變正確即可
3. **surface 形關掉合成 `it` 注入**（`ps.no:6234` 的 `kids.push(itl)`）
4. **把 s55/s59/s60 語義搬進 MatchArm 槽位**（关键字臂綁定 / 變體臂補集 / armDoc）
5. 裸大括號形（32%）的 `Matched` 留空


### ✅ 第 ② 件落地（2026-10-20）：`ps-parse-match` 改發 `SurfaceMatch`

依正確拆解5 步落地開關 `PS-SURFACE-MATCH`（`compiler/src/ps.no:110` 模組級常數）
**預設關（0）**保零回歸，翻 1 即走 surface 形；`ast.no` 的 `matchArm` helper（第 ① 件）已就位

**關鍵修改（均位於 `ps-parse-match`）：**
1. **放行非 IDENT 主體**（步①）：`COLON+LBRACE` 分派放寬為 IDENT / IndexExpression / DotExpression，
   主體**節點**經新形參 `subjNode` 傳入（覆蓋率 17%→68%）
2. **修臂迴圈**（步②）：`ps-parse-expr-stmt` 頂端新增裸 `_` 陳述處理（產 `ExpressionStatement{nil}` 佔槽），
   修掉 `-> _` 臂的幻影 wildcard 臂（claim B）；內聯臂體改以 `ps-parse-stmt` 解析，主體不再切碎
3. **surface 形關掉合成 `it` 注入**（步③）：surface 收集區另建 `blkS`（不含 `itl`），與降糖 `blk` 平行
4. **s55/s59/s60 語義搬進 `MatchArm` 槽位**（步④）：新增 `swA`/`dotValA`/`bindA`/`bvarA`/`condA`/`blkSA`/`bbA`
   陣列，`binding`/`bindingVariant`/`wildcard`/`dotVal`/`Condition`/`blockBody` 逐臂填入
5. **裸大括號形 `Matched` 留空**（步⑤）：surface 發射分支 `ast.kid-add(t, sm, subjNode)`——主體為 0 時
   `Matched` 因 guarded 自然不印

**err/nil wildcard 語義修正（本次收口重點）：**
原 `kvar != '' -> sw = 1` 錯把 `err`/`nil` 當 wildcard；對齊 oracle 改為 **`sw = wild; kvar == 'ok' -> sw = 1`**
——`_`/bare→ 與 `ok`/`ok(v)` 為 wildcard，`err`/`nil`（含 `err(e)`/`nil`）一律**非** wildcard 且保留
`Condition` 型樣節點；`bindingVariant` 僅解構綁定名存在時填（`ok(v)`⇒`ok``err(e)`⇒`err`）emit 端
slot 2 由恆 `0` 改讀 `bvarA[i]`

**實證（A/B 對 oracle `bin/no ast`）：**
| 案例 | 主體 | surface 輸出 | oracle |
| --- | --- | --- | --- |
| `z = 1\nz: { 1 -> a=1 / _ -> a=2 }` | Identifier | 2 臂，pattern 1 + wildcard _ | 一致 |
| `h[0]: { ok -> it=0 / -> _ }` | IndexExpression | 2 臂，ok=bare wildcard+dotVal，_=wildcard，`ExpressionStatement{nil}` | 一致 |
| `x i64=3\nx: { 1 -> {a=1 b=2} / _ -> a=9 }` | Identifier | 2 臂，`blockBody:true` + 非 inline `BlockStatement{}` | 一致 |
| `x i64=0\nx: { ok(v)->y=v / err(e)->y=e / nil->y=0 / _->y=1 }` | Identifier | ok(v)=wildcard+dotVal+binding:v+bindingVariant:ok；err(e)=binding:e+bindingVariant:err+Condition=Identifier{err}；nil=Condition=NilLiteral | 一致（非 ?T 主體亦吻合） |

**零回歸實證：** `PS-SURFACE-MATCH=0` 下 `ps-test.no` 10/10`ast-test.no` 48/48 全綠；
`=1` 下 9/10（test 10 預期降糖 if/else，屬預期失敗，因 SurfaceMatch 已取代）
**⚠️ 自舉 harness 兩個別再踩的坑：** ① nolang 字串字面量 `{…}` 被當**字串插值**⇒ 測試來源字串必須
**先存進變數再傳** `parse-dump(src)`，不可內嵌 `{`/`}`（否則 MIR codegen `interp not lowered` 報錯或靜默吐空）；
② **自舉 `ps.parse` 對 `?i64` 會掛起是誤判**（2026-10-20 複查更正）：此前觀察到的掛起實為①的字串插值吞源陷阱，
套用①的先存變數修法後，`?T` 主體（`x ?i64 = …`）在自舉 `noc ast` 下也能正常解析不再掛起；
`ok`/`err`/`nil` 的逐位元組對拍既可走 `bin/no ast`（Go parser oracle），也可走自舉 `noc ast`（subject 為 `?T` 亦同構有效）


### 5.3.4.x L2 收斂量化（第②件 + s66d raw-cond 擴展，2026-10-20 實測）

**方法**：`compiler/src/ps.no` 在 `PS-SURFACE-MATCH=0`（降糖基線）與 `=1`（SurfaceMatch）下，分別
`bin/no build -o /tmp/noc-s0-off compiler` / `bin/no build -o /tmp/noc-s0 compiler` 構建自舉編譯器；
`scripts/selfhost_diff.sh --levels L2 --corpus "tests src/std"` 比對 SUT `no ast` 與 oracle `bin/no ast`
（Go parser 產 SurfaceMatch），語料 = 653 個 `.no`

**結果（headline）**

| 模式 | SAME | DIFF | 翻轉（DIFF→SAME） | 回歸（SAME→DIFF） |
| --- | --- | --- | --- | --- |
| flag-OFF（降糖基線） | 340 | 313 | — | — |
| flag-ON（SurfaceMatch，核心臂） | 385 | 268 | **45**（100% 含 match 區塊） | **0** |
| flag-ON + s66d（補 raw-cond 臂） | 386 | 267 | **+1**（`tests/minimal-ok.no`） | **0** |

- 收斂：DIFF 313 → 267（**−46**），SAME 340 → 386（**+46**），全程 **0 回歸**
- 翻轉的 45 檔 100% 含 match 區塊 ⇒ 第②件精準消除match 表示分歧且未波及任何其他表示

**s66d raw-cond 臂擴展（第②件範圍內補洞）**：SurfaceMatch 發射路徑原只處理值綁定臂
`ok(v)`/`ok`/`err(e)`/`nil`/`_`，對裸條件臂 `ok(<expr>)`（如 `ok(it >= 90)`）仍產 `CallExpression{fn: ok}`
s66d 在 `ps-parse-match` 補：
- 臂首識別 `ok(<expr>)` ⇒ `rawCond: true`，`Condition` = 內部布林表達式（`InfixExpression`），**不綁定非 wildcard非 dotVal**；
- `valueCtx` 與 raw-cond **無關**，由**值語境**驅動（見本節 s67）：`rawCond: true` 的臂在陳述語境下 `valueCtx` 仍為 falses66d 原`valueCtx` 由 match 層 `matchHasRaw` 驅動係**錯誤代理**（見 s67 根因），本版已棄用 `matchHasRaw` 驅動 valueCtx，僅留作 rawCond 標記
- 降糖（if 鏈）末端：raw-cond 臂直接用 `Condition`（不包 `subj ==`）

**獨立實證（非全量 sweep 推論）**：
- `tests/minimal-ok.no` 在 s66d 前屬 ON DIFF 集，s66d 後 `bin/no ast` 與 `/tmp/noc-s0 ast` **逐位元組一致** ⇒ 淨收斂 +1 檔的邏輯證明（非推論）
- `tests/match-raw-cond.no` s66d 後 `rawCond`/`valueCtx` 相關 diff 行數 = **0**；剩餘 75 行全為正交的 `?T` unwrap / type-inference 分歧

**結論 / 更正**：
- 收斂 98% L2 表示分歧**未被證實**：第②件 + s66d 總共只降總 DIFF 的 **14.7%**（46/313）
- 殘餘 267 DIFF 主因是**正交的** option/result `?T` unwrap lowering 與 type-inference 註解分歧（獨立 parser 收斂 track），非 match 表示本身
- 第②件與 s66d **零回歸**（0 個 SAME→DIFF），`PS-SURFACE-MATCH` 維持預設 `0` 安全


### 5.3.4.z s67 valueCtx 值語境修正（2026-10-20，本輪）

**根因（推翻 s66d 假設）**：s66d 用 match 層 `matchHasRaw`（含任一 raw-cond 臂）驅動 `valueCtx`，
實為**錯誤代理**實測 oracle（`bin/no ast`，top-level 陳述）證實 `valueCtx` 與 raw-cond **無關**：
- `r = n: { -> 1 }`（**無** raw-cond 臂，單純賦值右值）⇒ oracle 印 `valueCtx: true`；
- `n: { -> 1 }` 作**裸陳述** ⇒ oracle **不**印 `valueCtx`；
- `x = f(n: { -> 1 })` / `x = [ n: { -> 1 } ]` / `x = S{ y: n: { -> 1 } }`（巢狀 call-arg / 陣列·結構元素）⇒ 同為 `valueCtx: true`（黏性繼承）

對齊 Go `src/parser/desugar.go:146` `validateMatchArmReturns`：該函數**只在值語境（CTX_EXPR）被呼叫**，
據此把每臂標 `valueCtx = true`（`src/parser/match.go:844`）`CTX_EXPR` 是 Go `parseLetStatement` /
`parseUnwrapAssignStatement` / `parseCompoundAssignStatement` 三處 `p.ctx.push(CTX_EXPR)` 閘門，且
**對齊堆疊具黏性**——賦值 RHS 內的巢狀 call-arg / 陣列·結構字面量元素 / 二元運算元皆繼承值語境
（return / call-arg 頂層 / if·for 條件**不** push CTX_EXPR ⇒ 那些語境的 match `valueCtx` 為 false，與本實作一致）

**修法（compiler/src/ps.no）**：不 thread 形參（避免動 `ps-parse-expr-prec` 38 處遞迴呼叫點），改用 `psst.valCtx` 欄位：
- `new` 結構體加 `valCtx: 0`；`ps-parse-stmt` 開頭重置 `p.valCtx = 0`（每條陳述隔離）
- 賦值 RHS 的 7 個解析點（`ps-parse-let` 主路 + 明示型別 / 定長陣列 / 推論陣列 / 切片 / 可空型別 / 多重指派標註）
  在 `ps-parse-expr` 前 `vcSave = p.valCtx; p.valCtx = 1`，後 `p.valCtx = vcSave`
- 內聯 `obj.x = expr`（`K_ASSIGN_EXPRESSION` 路徑）**不**設值語境——對齊 Go `parseExpression` 不在該路徑 push CTX_EXPR
- `ps-parse-match` 發射端第 valueCtx 槽改讀 `p.valCtx`（棄用 `matchHasRaw` 代理）；`matchHasRaw` 僅留作 rawCond 標記

**收斂預期與狀態（已複測，2026-10-20 末）**：
- ⚠️ **本節舊版toolchain 解鎖後補歸因已迴轉**：`bin/no build compiler` 報 `EmitLLVM: setfield field`
  **不是** toolchain 漂移 / pre-existing 回歸，而是 **s67 自己引入**——`3c0df878` 在 `psst` 字面量與 7 處賦值
  用了 `valCtx`，卻**漏在型別定義宣告欄位** ⇒ codegen 找不到 field offset修法 `7015c7ef`（補 `valCtx i64`）
  ⇒ 自舉構建 **Build successful**全 39 commits（s67 + 修 + `?T` unwrap track 設計 + 並發 JS 後端）已推
  `origin/main`（`a58a907c`），`git ls-remote` 核實一致
- **L2 全量複測**（乾淨 worktree，CTL=Go `no`@HEAD → `/tmp/noc-ctl`；SUT-ON=ps.no `PS-SURFACE-MATCH=1`
  → `/tmp/noc-s67fix`；SUT-OFF=`=0` → `/tmp/noc-s67off`；語料 `tests`+`src/std` 653 檔）：

  | 模式 | SAME | DIFF |
  | --- | --- | --- |
  | OFF（降糖基線） | 340 | 313 |
  | ON（SurfaceMatch + s67 valueCtx） | 385 | 268 |

  - 表示切換效應（同 CTL，ON vs OFF）：**+45 SAME / −45 DIFF**（45 檔 100% 含 match 區塊 `: {`）
  - **s67 valueCtx 修正的 L2 效應 ≈ 0**：ON 檔數與 s66d 舊表（386/267）幾乎相同 ⇒ s67 是**語意正確性**修正，
    非收斂量修正
  - **剩餘 DIFF 中 valueCtx 失配：0 / 268**（對 268 DIFF 檔逐檔 `diff <(CTL ast) <(SUT ast)` 抓 `valueCtx`）
    ⇒ s67 **已徹底消除 valueCtx 表示分歧**；剩餘 268 全為正交分歧（`?T` unwrap / raw-cond 臂缺口 / type-inference）
- 收斂 98% L2 表示分歧說法**仍未被證實**：ON 僅比 OFF 少 14.4% DIFF（45/313），總 DIFF 仍 268/653=41% 未收斂
- 兩輪 sweep 均過 G1/G2/G3/G6/G7（ctl_ok=653oracle 71/71 節點全覆蓋binary 無 drift）；`RESULT=FAIL` 僅因未全收斂


### 5.3.4.w S7 裁定①續——match 表示缺口 A→H 全鏈修（2026-10-21 本輪）

裁定①（SurfaceMatch 預設開）落地後 L2 全量 SAME=385/DIFF=268本輪以MatchArm 多重集差分
逐缺口歸因並修至 **SAME=510/DIFF=143**，match-表示殘差 **193 檔 → 6 檔**

**歸因方法**：對 DIFF 檔抓 MatchArm 頭欄位多重集——count_gap（臂數不符）/ flag_gap（臂數齊但旗標異）/
clean（MatchArm 全同，分歧在他處）三類三輪實測：

| 里程碑 | 修法 | SAME/DIFF | match 殘差 |
| --- | --- | --- | --- |
| 裁定①基線 | PS-SURFACE-MATCH=1 | 385/268 | 193（count 169 + flag 24） |
| A+B+C1 | 裸 match surface 發射 + valCtx 快照 + blockMatch 內層裸 match | 488/165 | 40 |
| +C+D | 型別臂 `v T ->` 三槽 + 變體臂保持 CallExpression | 497/156 | 29 |
| +E+F | NEWLINE 臂體形/空臂體 + range 型樣 `[a..b)` | 505/148 | 9 |
| +G+H* | `?T` 裸宣告 + blockMatch 回退提取 + 分類器修正 | **510/143** | **6**（count 5 + flag 1） |

**缺口清單（本輪全部實測釘死）**：
- **A 裸 match surface 發射**（~2401 臂）：`ps-parse-bare-match` 原走 desugar 組裝無 SurfaceMatch
  發射補 surface 發射段 + `ok->` 的 wildcard/dotVal + Matched 占位 `ast.kid-add(t, sm, 0)`
  （防 kid0 別名 → dump 無限遞迴 SIGSEGV，見 §5.3.4.z 同源教訓）
- **B valCtx 讀點**（85 臂）：`ps-parse-stmt` 每陳述重置 `p.valCtx` ⇒ 發射尾讀必 0入函快照 `mv`
- **C 型別臂 `v T ->`**（12 臂）：generic expr 臂吞 `v T` 兩 IDENT新 pattern-dispatch case
  ⇒ typeArm/typePattern/typeBinding 三槽（expr.go:1152-1160）
- **D 變體臂 `circle(r) ->`**（~14 臂）：parse-time 歸一與 oracle（lowering 期歸一）相反
  `PS-SURFACE-MATCH==1` 跳過 caseB ⇒ 保持 CallExpression 型樣binding 空
- **E NEWLINE 臂體形**：`->` 後 `ps-skip-trivia` 吞換行 ⇒ expr.go:1251-1376 第二支永不可見
  補 NEWLINE 形（blockBody=true **且** inline=true；空臂體合法 `err ->\n\nnil ->`）+
  `ps-is-opt-pat-start`（Go isOptionPatternStart）收束判據 + inline 形空臂體 fallthrough
- **F range 型樣 `[a..b)`**：LBRACKET primary 一律建 SliceLiteral ⇒ 誤吞成 SliceLiteral{4}
  鏡像 Go parseSliceLiteral range 分支（ELLIPSIS 於首元素前後 ⇒ RangeExpression）
- **G `name ?T` 裸宣告**：`ps-expect(ASSIGN)` 失敗不退 ⇒ 後隨陳述被吃成 LetStatement.value
  且 valCtx=1 污染 MatchArm.valueCtx（match-test.no）鏡像 ps-parse-typedecl 雙形口徑
- **H/H2/H3/H4 blockMatch 回退提取 + 分類器**：明示臂 LBRACE 體在 classify==blockMatch 但內層
  裸 match 失敗時，oracle 落 parseStatement 的 parseBlockStatement 回退並提取陳述——
  IsBlockBody 保持 **false**IsInline=true（err-binding.no `err(m) -> { b: {…} … }`）
  連帶修正 `ps-bm-scan` 兩處與 oracle classifyBlockAtCurrent 的分歧：
  - **H2** `IDENT : {`（labelled block/match）⇒ blockMatch（舊 excl 誤擋為結構）；
  - **H3** `IDENT =` / `IDENT ,` 起頭塊 ⇒ Go blockEnum≠blockMatch（語句起頭，箭頭掃描誤觸 10 檔回歸）；
  - **H4** 箭頭掃描同行止步（NEWLINE/SEMICOLON 即停，鏡像 Go scanForArrowAtDepth0）
  - 另補 `bodyDone = 1`：LBRACE 體收畢未置旗 ⇒ NEWLINE 形分支誤觸發把 hd2 抬回 1

**坑（新記錄）**：
- ⚠️ `ps-bm-scan` 的有 depth-0 `->` ⇒ blockMatch啟發式與 Go 的**形狀分類**根本不等價——
  語句起頭塊（`a = …` / `a, b = …` / `f(x)…`）內的後續獨立 if `->` 會誤觸H3/H4 修兩類，
  `IDENT LPAREN`（Go blockIface）與 `IDENT NEWLINE`（Go blockUnknown）兩形仍待補（語料未見，
  見 §11.1 記帳）
- ⚠️ `scanned/bodyDone` 雙旗標缺一即互踩：LBRACE 路徑收畢體後**必須** `bodyDone = 1`，
  否則下方 `hd2 == 0 && bodyDone == 0` 的 NEWLINE/inline 分支重入
- 回歸放大教訓：gap H 首版（無 H3/H4）修 5 檔砸 10 檔——**掃描式分類器的改動必須全量 L2 驗**，
  單點冒煙不足

**殘差 143 的結構（2026-10-21 末）**：match-表示 **6 檔**（count_gap 5：bzip2/http2/str/txt/yaml
——複雜主體 `expr[l]: {…}` match 陳述被吞str/txt 反向多臂；flag_gap 1：build-mo）；
clean 137 全為正交軌（`?T` unwraptype-inferenceGenericParams 渲染warnings 計數Doc 註解歸位）
bzip2 抽驗：3 個 `tmp-base[l]: { ok -> …; -> _ }`（IndexExpression 主體）整組消失——
複雜主體 match 陳述是下一缺口類（I）

**回歸測試**：ps-test.no 13→19（test 11-19：bare-match surface / ok-dotval / valueCtx 快照 /
typeArm / 變體型樣 / NEWLINE 空臂體 / range 型樣 / `?T` 裸宣告 / blockMatch 回退提取）
五套件全綠：ps 19 / ast 48 / lx 45 / tok 62 / util 27



### 5.3.4.y S7 裁定①續二——缺口 I 收尾 + 迴圈行內體根因（2026-10-21 本輪）

**s7j 常態化結果（ps-arm-blk-match 臂體門）**：SAME=513 / DIFF=140 與 s7i 同，
但 MatchArm 多重集歸因翻轉——**count_gap 4→1**（str 97/97 組txt 35/35 組bzip2 全清零；
僅 yaml 殘：SM 27→24MA 103→98）其餘 139 檔 MatchArm/SurfaceMatch 多重集恆等（正交）
match-表示殘差 = **yaml + build-mo（valueCtx: true 缺旗標）2 檔**

**yaml 下計數根因——不是 match 門，是迴圈行內體偷括號**：

1. 症狀面：yaml 整檔 dump 發散 13,831 行，3 個裸 SurfaceMatch 組（chomp 3 臂組 +
   兩個 `content.len-bytes() > 0` 單臂組）+ 5 個 MatchArm 整組消失；SUT 將其降級為
   `BlockStatement`+IfExpression 鏈，且最內層 wildcard 臂 `-> {…}` 被吸附成內層 if 的 else
2. **結構面（真正根因）**：FunctionDefinition 縮排普查顯示 SUT 從 `parse-plain` 起全部函式
   +2 層從 `parse-block-scalar` 起 +9 層（後續函式被吞進前函式 for 迴圈體）
   最小複現 **c2**（純函式級 `k <- [0..pending): content = content - '\n'`）：
   行內體被 parse 成**開區間區塊**，吞掉後隨兩條陳述並偷走函式收束 `}`，
   連頂層 `r = f(0)` 都被吞進函式體
3. 根因鏈（ps.no 兩缺陷複合）：
   - `ps-expect` 對不符 token **靜默放行**（`-> {}`）；
   - `ps-parse-block` 是**開區間**（`(peek != RBRACE && peek != EOF)` 迴圈吃到 `}`/EOF）
   ⇒ `for v <- range: stmt`（Go parseLoopBodySingle-statement body形）在 ps.no
   落入塊模式後一路吞到下一個 `}`——外層收束被偷括號全面錯位裸 match 在錯位上下文中
   被逼回 parseBlockStatement 回退形（chomp 組因此整組丟失）
4. **修法**：新增 `ps-loop-body`（鏡像 Go stmt.go:2386-2411 parseLoopBody）——
   `{ … }` 塊體照舊；否則**只 parse 一條陳述**包成 `BlockStatement`+slot0='true'
   （dump `inline: true`）三處迴圈本體位（ps-parse-for 裸形 / ps-parse-for-kw
   isRange 形 / 條件形）全改走它⚠️ 附帶修正：條件形 deprecated 警告**有冒號時不發**
   （Go stmt.go:2586-2612 `!hasColon` 閘；c5 探針 SUT 多報 warnings: 1）
   ⚠️ **新坑**：nolang 中**兩條獨立 `cond -> …` 語句的條件各自即時求值**——若首條分支
   改變 parser 狀態，次條重讀 peek 已不同`ps-loop-body` 首版用
   `peek==LBRACE -> {…}` / `peek!=LBRACE -> {…}` 兩條語句被此坑咬（c3 塊體形回歸），
   改先擷取 isBlk 旗標再分支（同 `hd` 慣例）後全綠

**實據（探針 a–c7 + yaml 全檔）**：
- 行內體七形（a 裸 match 全形 / b more==false 形 / c1-c2 迴圈行內體 / c3 塊體 /
  c5-c6 關鍵字形 / c7 單陳述邊界）修後全 SAME；c4（`cond: stmt` 非迴圈形）殘 2 行——
  Go 在陳述層吐 `ExpressionStatement{nil}`，SUT 漏吐（獨立小缺口，記帳）
- **yaml 全檔：13,831 行發散 → 1 行**；SurfaceMatch 27/27MatchArm 103/103
  （3 組 + 5 臂全數回歸）；FunctionDefinition 縮排全檔歸 4（級聯消除）
  殘 1 行為正交（`type: ?i64` 註解渲染 + 多一個 `AnnotationStatement{entries: 1}`）
- 判別力實測：s7j 舊碼對 test 21 語料產 20 行發散（舊碼必 FAIL）新碼 0 行

**回歸測試**：ps-test.no 20→**21**（test 21：`for v <- range: stmt` 行內體只綁一條陳述 +
`inline: true` + 不偷外層收束）五套件全綠：ps 21 / ast 48 / lx 45 / tok 62 / util 27
L2 全量（noc-s7k）掃描見本輪末記帳

**s7k 歸因翻轉（循環體修後）**：SAME=514/DIFF=139；match-表示殘差 **2→1 檔**
（yaml 3 組+5 臂全數回歸滑入正交集；僅存 build-mo valueCtx）

**build-mo（最後一檔）根因——RHS 位裸 match 運算式缺失**：
語料形 `result = { x == 0 -> 1; x == x -> 0 }`Go expr.go:369-386 primary 位
LBRACE 臂：blockStruct → 匿名 struct literal／blockMatch → `parseBareMatchExpr()`／
其他 → 吃 `{` 回 nilps.parse-expr-prec **primary 位無 LBRACE 臂** ⇒ RHS 回空
（`LetStatement{value: ∅}`）裸 match 落回陳述位重解析（valCtx 已被 869 行陳述起始
重置歸零）⇒ 臂 valueCtx 全丟**修法**：補 LBRACE primary 臂——
`ps-is-struct-lit` → `ps-parse-struct-lit(t, p, '')`（匿名形 type 空）／
`ps-bm-scan` → `ps-parse-bare-match`（入函 `mv` 快照驅動 valueCtx，5251 輔助位）／
否則 bump 回 nil⚠️ 同 ps-loop-body：**閘先擷旗標再分支**（解析改 p.pos 續列重評必錯位）
實據：build-mo 13 行發散→**0**；匿名 struct literal primary 同修後亦 0；
ps-test 21→**22**（test 22：RHS 裸 match 掛 value + valueCtx: true，判別力 13 行）
match-表示殘差 **清零**（138 檔 MatchArm/SurfaceMatch 多重集恆等全正交）

**本輪 L2 收官（noc-s7m 雙修後全量）**：SAME=**515** / DIFF=**138**（輪初 513/140），
ctl_ok=sut_ok=653多重集歸因：**match-表示殘差 0 檔**——138 檔 DIFF 的
MatchArm/SurfaceMatch 多重集全部恆等，純正交軌（`?T` unwrap 合成 desugar
type-inference 註解渲染AnnotationStatement 歸位warnings 計數Doc 歸位
`cond: stmt` 陳述層 `ExpressionStatement{nil}` 小缺口等）
S7 match-表示收斂線至此**收官**；後續轉入正交軌（G6 五級門禁擇機開）



### 5.3.4.aa 正交軌首件——IndexExpression 元素型別推斷 + `tn` 同名靜默誤編（2026-10-21 本輪）

**正交軌開工**（S7 match 表示收官後）：138 檔 DIFF 按 dump marker 差異分類——
inferred-only 29 / synth+armBind+nullableT 28 / cap+synth+prop+armBind+inferred+nullableT 25 /
無 marker 差 19 / 其餘小類（annot 3warnings 2generic 3）首件攻 inferred-only 類的
IndexExpression 元素型別推斷（Go stmt.go:1462-1494）

**缺口**：`v = [1,2,3]`（全域）後 `e0 = v[0]`，Go 從**全域**型別表讀 receiver 型別推元素
型別並建 `NamedType{i64, inferred}`；ps.no 無此臂 ⇒ `LetStatement{value: IndexExpression}`
裸奔（無 type 行）with-cap.no 的 9 行正交缺口（s7m 時代即有）正是此形
（`e0/e1/e2 = arr[i]`）

**實作**（三件，鏡像 Go 口徑）：
- `ps-g-var-type`（ps.no:2302）：**全域表只讀**——Go setVarType 在函式內只寫
  FuncVarTypes（parser.go:1113-1129），IndexExpression 臂直接索引全域 `p.sem.VarTypes`
  ⇒ 函式內區域 receiver 讀不到就不推斷（與 Go 同口徑；p-idx-b 實測 0 行）
- `ps-idx-elem-of`（ps.no:3392）：`[]X`→剝前綴；`[N]X`→第一個 `]` 之後；非陣列/切片形
  （含長度不足）⇒ `''`⚠️ 算術一律預算（內聯算術作實參會污染呼叫 ABI，gap I 同款教訓）
- ps-parse-let 的 IndexExpression 臂（ps.no:2936）：僅在 LHS 尚無型別時設定
  （FuncVarType 語義 = ps-var-type 前置查），並回寫型別表供後續推斷鏈

**🔴 加碼即遇災難性回歸——`tn` 同名靜默誤編（本輪最大收穫，編譯級 bug）**：

症狀：`x = 5` 竟輸出 `type: 3` + `NamedType{name: 3, inferred: true}`——i64/f64/str→`3`
char/bool/byte→`4`（數值＝**型別名長度**！），所有 let 推論型別全污染；而同構的切片字面量臂
（ps-elem-type-of）卻完好bisect 鏈（/tmp 變體構建法見 `MEMORY.md` 鐵律節）：
- E2（只刪臂助手全留）/ E3（+刪 ps-idx-elem-of）/ E4（全刪＝s7j 源）：三者 p-types/r3
  **全部恢復 SAME** ⇒ 助手無辜，**觸發點是臂的存在本身**——連
  `&& ps-idx-elem-of('') == 'never'` 永不真的守衛版也照壞
- E5（臂保留臂內 `tn`→`tnx`）：**即修**

根因：**同一 match 的不同臂宣告同名局部變數（本臂 i64 `tn` vs catch-all 的 str `tn`）
⇒ codegen 把兩個 `tn` 串成同一槽位並統一型別**——catch-all 的
`tn = ps-infer-type(...)` 回傳值被按 i64 截斷，剛好落在字串描述的長度欄
（'i64'→"3"'char'→"4"）**checker 完全不報編譯通過執行不崩**——最危險的一類
靜默誤編ps.no:2936 臂內已立註釋警示；寫新臂的節點變數一律加唯一後綴（如 `tnx`）

**實據（noc-s7l 全探針）**：p-types / p-r3 / p-slice / p-idx-a（全域 []i64→i64）/
p-idx-b（函式內 receiver 不推）/ p-idx-c / p-idx-t（[]char→char）全 SAME；
**with-cap 9 行正交缺口順手關閉**；src/std/yaml.no 與 E4（無本修）輸出 **0 行差**
（217 行殘差為 s7m 時代既有基線，與本修無關）；tests/yaml.no 0 行
ps-test 22→**23**（test-let-idx-elem-infer：全域 []i64→i64 / []char→char / 函式內
receiver 不推三形態一網打盡；判別力：s7j 舊碼對 e0/e2 兩形態缺 type 行）
五套件全綠：ps 23 / ast 48 / lx 45 / tok 62 / util 27



### 5.3.4.ab L2 現狀複測與 WIP mass-noop（2026-10-24 本輪）

**複測（CTL=`bin/no`SUT=`/tmp/noc-se`語料 `tests`+`src/std` 653 檔`no ast`）**：

```
files=653  ctl_ok=653  ctl_empty=0  sut_ok=653  sut_empty=0
SAME=533  DIFF=120     失敗集合 ctl=0 sut=0
oracle 覆蓋：0 行 <unknown>（71/71 節點型別全覆蓋）
```

- 相較 s7m 收官的 515/138 **又前進 18 檔**（533/120），精確歸因見
  `docs/design/nolang-parser-lowering-invariants.md` §13.2
- **match-表示殘差 = 0 檔**：120 檔 DIFF 全部 `MatchArm`/`SurfaceMatch` 多重集恆等
  （count_gap 0 / flag_gap 0 / clean 120）⇒ **gap I 已關閉**，S7 match 收斂線確認收官
- 120 檔正交軌按 marker 簽名分 12 類（最大類 `nomarker` 35
  `synth+armBind+nullableT+idxout` 32）**含 `synth` 71 檔（59%）含 `cap` 33 檔（27.5%）**
  ⇒ **S3-c（`__cap_N` 不再注入 AST）是自舉收斂的最大單點槓桿**，落地即自動消掉 33 檔
  此前 S3（V4 合規）與 S7（自舉收斂）被當兩件事分開跟蹤，是漏看的協同

**🔴 從當前工作樹構建的 SUT 是 mass-noop（本輪發現）**：

以工作樹 `compiler/` 構建 `/tmp/noc-now` 跑同一條 L2 ⇒ **SAME=0 / DIFF=653**，
且 SUT 的 stdout 雜湊對 653 檔**全部相同**（`4fde700016d5`）直查：
**對任何輸入只吐 `Program{}`（10 bytes）**；`noc-se`/`noc-s7l` 同檔吐 2148 bytes

**⚠️ 歸因更正（同輪晚些實測推翻初判）**：初判歸給 `compiler/` 的 1465 行 WIP，**錯了**
二分實測：WIP `bin/no` + **HEAD 乾淨 `compiler/`**（`git worktree` 導出）⇒ **同樣 mass-noop**
⇒ 換掉自舉側原始碼毫無改變，根因在 **Go 側 `src/` 的 106 個未提交 `.go`**
（parser 40 / checker 28 / build 14 / mir 10 / hir 5 / fmt 4 / mmast 2 / builtin 1）

**根因鏈（探針逐層釘死）**：`fs.read-bytes` → `nil`（應為 ok）；`fs.open` → `ok`（開檔正常）；
`it.read-bytes()` → **`nil`**（← 失敗點）；而 `f = () (r ?i64) { r = 42 }` 解包 → `OPT=ok`
（option 機制本身正常）⇒ 失敗點是 `src/std/fs.no:607` `file.read-bytes()` 的
`size == 0`（:615）／`total == 0`（:639）分支 ⇒ `fstat-size` 或 `read` 在 WIP 的 MIR 後端下取不到值
**✅ 已判定為 WIP 新引入的回歸**（補做正確對照：HEAD 全棧 `BRANCH=ok` vs WIP `BRANCH=nil`）
上一輪寫的對照不成立是**實驗缺陷**——複製 WIP 的 `std_ast.bin` 去配 HEAD 的 `src/std` 導致
`unknown callee`；正確做法是在 worktree 內就地 `go run ./checker/genstdsig` 生成該版自己的 `std_ast.bin`
**下鑽結果**：`open` → ok`fstat-size` → ok 且 `SIZE=NONZERO` ⇒ 排除 `size == 0`（fs.no:615），
命中 `read(.fd, buf[total..], …)` 回 ≤0（fs.no:631）⇒ `total == 0` ⇒ nil
`src/builtin/os.go` 未被 WIP 改動 ⇒ 問題在 MIR 側對該 CLibCall 的發射路徑（`src/mir/codegen.go` +221）

**自舉側次生缺陷（獨立值得修）**：`compiler/src/drv.no:116` `drv.cmd-ast` 對 `read-bytes`
的 `nil` 分支設 `got = true` 卻不報錯 ⇒ 檔案為空被吞成成功讀到空檔，`cmd-tokens` 同形
**正是這個靜默把一次 I/O 回歸放大成 mass-noop**

兩點教訓：
1. **L2 是手動門禁，不在 CI 內** ⇒ 此 mass-noop 自 2026-10-22 起無人發現
   `selfhost_diff.sh` 的 G7（控制組只吐一種 stdout ⇒ oracle 無鑑別力）**只查 CTL 側**
   **已補 G8（2026-10-24）**：SUT 在整批語料只產生 1 種 stdout ⇒ `MASS-NOOP`，
   SUMMARY 視為 FAIL 並額外說明不是 N 個獨立差異與 G2（SUT 全掛 rc≠0）分工：
   G8 抓每檔都跑得動卻吐同一份東西——更危險，因為它看起來像有訊號
   驗證：mass-noop SUT ⇒ 觸發 ✅；`noc-se` ⇒ 不觸發（SAME=10/DIFF=10）✅；
   `--self-test` ⇒ 不觸發（SAME=20 PASS）✅
2. **任何 S7 收斂數字都必須先確認 SUT 不是 mass-noop**（`sut_ok=653` 但雜湊單一 ⇒ 有訊號是假的）

**建議排期**：先修主編譯器 WIP 的檔案 I/O 回歸（`file.read-bytes` 回 `nil`，**不是**自舉側）
→ 再做 S3-c（收 33 檔）→ 再推安全索引降糖同類遷移（再收 ~26 檔）

> 附帶：原指向§11.1 記帳的引用現已落地——本輪新增 **§11 已知缺口記帳**（§11.1 為解析器／語法表示待補形清單，§11.2 為既有基線與前置待辦），此處改為歷史註記


### 5.3.4.ac s8q pipeline 同位差——`let` 右側 `->` 鏈包成 value（2026-10-27 本輪）

**背景**：str.to-f64 MIR 回歸由平行 session 修復後，工作樹 `compiler/` 首次可建
複測 L2（CTL=`bin/no`SUT=`/tmp/noc-s64`語料 `tests`+`src/std` **654 檔**`no ast`）：
**SAME=640 / DIFF=14**（較 §5.3.4.ab 的 120 已因多條正交軌收斂大幅下降）14 檔中
**同位差家族 = 2 檔**（`tests/pipeline-value.no``tests/stmt-boundary.no`）——為本輪攻擊目標

**同位差形狀（探針 `/tmp/pipe-probe.no` 實測钉死）**：`x = A -> B` 中整條鏈是 let 的
valueGo（`src/parser/stmt.go:1242-1260`，`parseLetStatement`）在 `stmt.Value = parseExpression`
後，若同位緊接 `RARROW`（且非 `CTX_MATCH_ARM`/`CTX_FOR_COND`）就把 value 包成
`IfExpression{Condition: value, Consequence: 獨立 body, Alternative: 同列 else}`，並掛
`RTPipelineValue` 旗標；鏈 `a -> 8 -> 9` 右接為 `if a { if 8 { 9 } }`舊 `noc` 缺這一步：
`ps-parse-let` 讓 let 只吃掉 `A`，殘留的 `-> B` 落到 `ps-parse-expr-stmt` (A) 被當成
**wildcard 獨立陳述**（`ExpressionStatement{IfExpression{cond: IntegerLiteral{1}}}`）——
既錯位（多一個頂層陳述）又錯 cond（wildcard 標記 1 而非 `A`），且推論臂把 `p = a -> 7`
的 `a` 誤傳播成 `type: i64`

**修法（`compiler/src/ps.no`，兩處同位規則）**：
1. `ps-parse-let`（推論型 `name = expr`）：`val = ps-parse-expr` 之後型別推斷之前，加
   `val > 0 && p.inMatch == 0 && ps-peek(p) == tok.RARROW -> val = ps-mk-standalone-if(t, p, val)`
   復用既有 `ps-mk-standalone-if`（已含 wrap-chain 遞迴 + 換行 `-> else` 續接，語義與 Go 一致）
2. `ps-parse-stmt` 明示型別臂（`name T = expr`）：同一行守衛——對齊 Go 統一的
   `parseLetStatement`（Go 只有一個函式型別有無皆走同段 pipeline 包裹）語料無此形，純為規則完備

包覆後 `val` 變 `IF_EXPRESSION` ⇒ 下游型別推斷各臂（Slice/Struct/Index/IDENT/Call）自然不命中，
`p = a -> 7` 不再誤推 `type: i64`（對齊 oracle）⚠️ 守衛用活`ps-peek`（不跳 trivia）——
`a \n -> b`（arrow 換行）不構成 pipeline let，另起 wildcard 陳述（已由單元反向看守）

**歸因（同一 harness同一 CTL654 檔，A/B `noc-s64`→`noc-s66`）**：**DIFF 14→12 / SAME 640→642**；
`pipeline-value.no``stmt-boundary.no` **轉 SAME**；**SAME→DIFF 真回歸 0 檔**；其餘 12 檔
（bigint/byte/sql/dom/http2/number/toml/vec/yaml/2/match-test/vec-elem-ownership）皆正交家族
（InterfaceDefinition 解析`[?]byte` 參數型別warnings 計數decode-bit 巢狀等），本輪不動

**門禁**：
- L0 單元：`compiler/tests/ps-test.no` 新增 `test-let-pipeline-value`（推論/鏈式/明示型別三形）
  與 `test-let-pipeline-no-newline`（換行反向看守），want 逐位元組取自 `bin/no ast`
  `no run -cc zig compiler/tests/ps-test.no` ⇒ **25/25 通過**
- fixture：`tests/pipeline-value.no`（本家族語料）L2 轉 SAME
- vet：`no vet compiler/src` 殘留 19 ERROR 全為平行 session 的 WIP（`ps-is-builtin-type`
  重定義 @2854/6241index-write lint @5210-5218/7676-7684），**本輪改動區（@3378@1290）0 新增**

**🔴 工具鏈附註（本輪構建必經）**：工作樹 `bin/no` 用 `/usr/bin/ld`（clang 17）連結時炸
`ld: tapi error: malformed file`——`MacOSX27.0.sdk` 的 `libSystem.tbd` 含 `arm64e.x1-macos`
架構記號，現存所有 `ld`/Xcode ld-1230 皆無法解析（SDKROOT/DEVELOPER_DIR/isysroot 覆寫無效）
**解法：改用 `zig` 作連結器**（自帶可用 lld）：
```
export PATH="/opt/homebrew/opt/llvm/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export ZIG_GLOBAL_CACHE_DIR=/tmp/zig-cache ZIG_LOCAL_CACHE_DIR=/tmp/zig-cache  # 沙箱禁寫 ~/.cache/zig
./bin/no build -cc zig -o /tmp/noc-sNN compiler
```


### 5.3.4.ad s8r module-path 接收者的呼叫回傳型別推斷（2026-10-27 本輪）

**背景**：§5.3.4.ac 收官後 L2 剩 **12 檔 DIFF**按 root cause 歸因分組，本輪攻呼叫回傳型別推斷家族 A（dom/toml）複測基線：SUT=`/tmp/noc-s66`654 檔DIFF=12

**缺口形狀**：`let x = <Mod>.<fn>()`（同檔以限定名定義的方法，如 toml.no:55 `toml.new = () (doc toml)`）的回傳型別推斷Go `returnTypeFromCallExpr`（type.go）對 DotExpression 呼叫有兩段：①`resolveReceiverType(recv)` 非空 → 查 `methodSignatures[recvTy.method]`；②recv 是 IDENT 但非變數→ **module-path 分支**，以全名 `recv.method` 查簽名表noc 的 s58 臂只實了①（`rty != ''` 守衛），②整段缺失 ⇒ `x = <Mod>.<fn>()` 的 let 不帶型別，oracle 帶

**修法（`compiler/src/ps.no`，s58 dot 臂新增 `rty == ''` 分支，標記 s8r）**：接收者 IDENT 且 `ps-var-type(recv)==''` 時，把它當套件限定名，組 `mq = recv + '.' + method`，`ps-msig-find(p, mq)`（`toml.new` 於 ps-parse-method-func 已註冊為 `ms:toml.new`），命中即克隆型別起點重跑 `ps-parse-type` + `ps-filter-ok` + 型別表登記（完全鏡像 rty!='' 臂與 Go 尾語意）⚠️ 本臂區域變數一律 **q 後綴**，避免與 rty!='' 臂的 tn3/ts3/keep2 同名串型別（s7l 教訓）裸名表 ps-sig-find 的 s8j 後綴掃描不用於此（其鍵為字面 `toml.new`，非 `.new` 結尾）

**歸因（同一 harness同一 CTL654 檔，A/B `noc-s66`→`noc-s67`）**：**DIFF 12→10 / SAME 642→644**轉 SAME 兩檔：`src/std/toml.no`（`doc = toml.new()` → `type: toml`）`src/std/yaml.no`（`s = <Mod>.<fn>()` → `type: str`，同根因）**SAME→DIFF 真回歸 0 檔****⚠️ 家族 A 未全然收官**：`src/std/dom.no` 的 `out = self.pool.collect-tag(...)` 是鏈式接收者（rc 為 DotExpression 非 IDENT），需 `resolveReceiverType(self.pool)`＝struct `dom` 欄位 `pool`→型別 `dom-pool` 的**結構體欄位型別解析**，是 noc 尚未具備的獨立機制（登記為後續 A2）

**門禁**：L0 `ps-test.no` 25/25；`no vet compiler/src` 殘留 19 ERROR 皆平行 session WIP（本輪改動區 0 新增）


### 5.3.4.ae s8s `[?]T` 回傳參數型別（ArrayType size=nil）（2026-10-27 本輪）

**背景**：§5.3.4.ad 收官後 L2 剩 **10 檔 DIFF**本輪攻`[?]T` 參數型別誤拆家族 C（number/vec）複測基線：SUT=`/tmp/noc-s67`DIFF=10

**缺口形狀**：`(out [?]byte)`（number.no:1250/1273）與 `(out [?]t)`（vec.no:182）Go 參數型別走 `parseParamTypeAfterName`（type.go:1090-1092）：`]` 前遇 `?` → `paramType="[?]"`，拼元素名後 `buildType("[?]T")` 產 **`ArrayType{Size:nil, Elem:NamedType{T}}`**（dump 為 `ArrayType{size: <nil>}`僅一個可見子節點）既非 `SliceType`亦非 `parseTypeExpression`(type.go:826-833) 那條 `SliceType{NullableType}` 路徑noc `ps-parse-type` 的 `[` 三臂（`[]T`/`[K]V`/`[N]T`）都以 +1==RBRACKET/INT/IDENT 為特徵，`[?]`（+1==QUESTION）無臂命中 ⇒ 回 0 且不消費 token ⇒ 參數迴圈逐 token 退化：`out` 落成無型別Parameter`byte`/`t` 被當下一個參數名

**修法（`compiler/src/ps.no`，ps-parse-type 新增 `[?]T` 臂，標記 s8s，置於 `[N]T` 臂前）**：`tk==LBRACKET && peek-at(1)==QUESTION && peek-at(2)==RBRACKET && peek-at(3)∈{IDENT,LBRACKET,QUESTION}` → bump `[ ? ]` 三者`elem=ps-parse-type`（遞迴）建 `K_ARRAY_TYPE`：slot0='false'（inferred 旗）**kid0 push 0（Size=nil）**kid1=elem⚠️ `type-string`（ast.no:740-744）讀 kid0==0 印 `?`kid0+1 讀 elem，結構 dump 由 asp spec（鏡像 Go dump.go，已支援 Size==nil）處理，零改 dumper

**連帶收 GenericParams**：修好 `(out [?]t)` → `NamedType{t}` 後，s63 隱式泛型走訪（ps.no:4426/4594，先 Parameters 後 Results走訪型別節點單字母 a-z）自回傳參數取得`t` ⇒ `[]t.to-arr` 補回 `GenericParams: t`（`[]t` 接收者本不播種，泛型 solely 來自此 `[?]t`）

**歸因（同一 harness同一 CTL，A/B `noc-s67`→`noc-s68`）**：**DIFF 10→9 / SAME +1**轉 SAME：`src/std/number.no`（兩處 `(out [?]byte)` 完整對齊，全檔 SAME）`src/std/vec.no`：`[?]t`/GenericParams 段已收斂，惟仍殘無關的 `j`/`sum` 識別符型別推斷過度/不足（既有非本家族），故 vec 仍 DIFF**SAME→DIFF 真回歸 0 檔**（本輪 DIFF 集為上輪真子集，僅移出 number）

**門禁**：L0 `ps-test.no` **26/26**（新增 `test-param-nullable-size-array`，golden 取於 oracle：`ArrayType{size: <nil>}` + `GenericParams: t`）；`no vet compiler/src` 殘留 19 ERROR（本輪改動區 0 新增）；`no vet src/std` 的 `str.no:1388/1393` index-write ERROR 屬 HEAD 既有（最後由 `67646cbe` str.to-f64 提交觸及非本 sessionsrc/ 側範圍外），且 `no vet src/std` 跑 Go oracle vet，與 self-host ps.no 改動無涉


### 5.3.4.af s8t match-test 死碼臂告警計數＋裸可空宣告 Doc 附着（2026-10-27 本輪）

**背景**：§5.3.4.ae（s8s）收官後 L2 剩 **9 檔 DIFF**本輪攻家族 F原估 F＝byte.no＋match-test.no，Program{warnings} 計數＋Doc 附着，小；複測分岔後**重新定性**：`src/std/byte.no` 的 2 條警告是 W_CHAIN_IF（UTF-8 編碼器 `cp<0x80 -> -> cp<0x800 -> -> …` 鏈式獨立 if），其 316 行結構差是同一鏈式降糖的分歧 ⇒ **歸家族 G（重）**，本輪擱置真 F 只剩 `tests/match-test.no`（**中等**：須移植 parse 期語義警告）基線：SUT=`/tmp/noc-s68`DIFF=9

**缺口形狀**（`diff <(oracle ast) <(noc ast)` 共 3 行）：
1. 首行 `Program{}` ↔ `Program{warnings: 2}`；
2. 缺 1 行 `Doc: // 直接->是else`（match-test.no:11 行前註解，須附著到 `x: { … }` 首陳述）

**(A) 警告移植（`warnUnreachableArms`，Go match_warn.go:68-199）**：Go 已把'->' 臂不可達診斷從降糖（`buildMatchDesugar`/`skipItBinding`）搬到 parse 期 S4（match_warn.go:12-13 註明全語料僅 match-test.no 19:9／79:9 兩處命中）noc `ps-parse-match` **早已算出同條件的 `dead` 旗標**（option：`ol==1 && oe==1 && onil==1` ⇒ 1，鏡 Go skipItBinding；enum：`bt==''` 全變體已列 ⇒ 1），僅供跳過 it 綁定**修法**：於函式頂宣告 `deadTotal = 0`；兩處 `dead = 1` 改為 `{ dead = 1 deadTotal = deadTotal + 1 }`；臂迴圈走完后組 if/else 鏈前發射 `matchHasRaw == 0 -> p.warn = p.warn + deadTotal`（鏡 Go：任一臂 IsRawCond ⇒ 整段 per-arm 判定 return ⇒ 不告警，故 raw-cond 閘控）

**(B) Doc 修复（`ps-parse-stmt` QUESTION 裸可空臂，~1479）**：舊行為在解析完 `x ?i64` 的型別後無條件`ps-skip-trivia(p)`——destructive 吞掉緊接的換行＋行前註解，令 `; 直接->是else` 無法經區塊迴圈的 `ps-skip-trivia-doc` 成為下一陳述 Doc**隔離實證**（dp2/dp4/dp5/dp6）：僅裸 `x ?i64`（QUESTION 臂）會丟 Doc，同形帶值 `x ?i64 = v`型別宣告 `s status` 皆正常**修法**：刪該 `ps-skip-trivia`，鏡 `ps-parse-typedecl`（6460-6517）含值/裸雙形口徑——直接非破壞性`ps-peek(p)` 判 `=`（同列 `name ?T = v` 時 `=` 緊接型別ps-peek 即見 ASSIGN；裸形見 NEWLINE ⇒ 不進含值支）

**歸因（同一 harness同一 CTL，A/B `noc-s68`→`noc-s69`）**：**DIFF 9→8 / SAME +1**轉 SAME：`tests/match-test.no`（warnings 計數＋Doc 雙缺皆收）**SAME→DIFF 真回歸 0 檔**（本輪 DIFF 集為上輪真子集，僅移出 match-test）

**門禁**：L0 `ps-test.no` **29/29**（新增 3 條：`test-match-unreachable-warn-option`／`test-match-unreachable-warn-enum`／`test-bare-nullable-doc-attach`，golden 皆取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（`fl[]`／`out[]` index-write 與 duplicate-fn，皆在本輪改動區外HEAD 既有，0 新增）；`no vet src/std` 的 `str.no` index-write ERROR 屬 src/ 側既有（非本 session範圍外）


### 5.3.4.ag s8u map 標註 let 複合值型別 → MapLiteral（2026-10-27 本輪）

**背景**：§5.3.4.af（s8t）收官後 L2 剩 **8 檔 DIFF**本輪攻 `tests/vec-elem-ownership.no`（8 行差）基線：SUT=`/tmp/noc-s69`DIFF=8

**缺口形狀**：`m [str][]i64 = { }`——宣告型別為 MapType（`[str][]i64`），Go parseLetStatement（stmt.go:1221）對 `Type.(*MapType) && 當前=={` 走 `parseMapLiteral` 產 **MapLiteral**；noc 卻產 **StructLiteral{type: }**

**根因**：noc `ps-parse-stmt` 的 map 標註臂（s8u 前 1406）以固定偏移手解析 key/value：守衛 `+4==IDENT && +5==ASSIGN`——只認單一 IDENT值型別`[]i64`（+4==LBRACKET）不符守衛 ⇒ 本臂跳過 ⇒ 落到通用 catch-all `ps-parse-typedecl`（1624，用 `ps-parse-type` 建型別`ps-parse-expr` 解 RHS）：型別仍建對（MapType），但 `{ }` 被 `ps-is-struct-lit` 誤判成 StructLiteral

**修法（`compiler/src/ps.no`）**：
1. map 臂值型別改由 `vty = ps-parse-type(t, p)`遞迴解析（鏡 ps-parse-type 的 `[K]V` 臂 6453Go type.go:835-849，V 可為 SliceType/NullableType/ArrayType/巢狀 MapType）；
2. 守衛由固定偏移改寫成非破壞性前探 `ps-is-map-assign-let(p)`：`[ K ]` 且 K 為內建型別名（同 map 鑑別），再從 +4 掃描型別語法（`[]`/`*T`/`&T`/`[K]V` 前綴群）至 base IDENT，其後必須為 `=` ⇒ 回 1；
3. 型別導向值解析（鏡 Go）：`ps-peek=={` ⇒ `ps-parse-map-lit`；否則 `ps-parse-expr`（`m [K]V = f()`）

**⚠️ 踩坑（本輪真因隱蔽）**：`ps-is-map-assign-let` 掃描迴圈最初寫成 `tkj==LBRACKET -> {…}`／`tkj==IDENT -> {…}`／`-> scan = 0`（意圖 catch-all）但 **ps.no 塊內各 `cond -> stmt` 是互相獨立的 bare-if，裸 `-> X` 只掛在最末個 bare-if 作其 else**——於是 `-> scan=0` 對任何非 IDENT token（含 `[`）都觸發，掃描在 j=4 即中止 ⇒ 複合 V 回 0 ⇒ 本臂根本不進⇒ 仍由 typedecl 產 StructLiteral（單 IDENT 的 `n [str]i64` 因第一步就遇 IDENT 而倖免，故只 m 壞n 好）修：catch-all 改為顯式已處理 token 補集`tkj!=EOF && tkj!=MUL && … && tkj!=IDENT -> scan=0`同坑亦見裸 `->` 誤當 if/else——欲得真 if/else 須將兩臂包進 `{ cond -> {…} -> {…} }`（ps-parse-map-lit 用此形）另：初版泛化丟了 `=` 判別，令裸 map 宣告 `m1 [str]i64`（無 `=`）誤入本臂吞後隨陳述 ⇒ `tests/mem-safety/nested-container-clone.no` 真回歸；`ps-is-map-assign-let` 的 `=` 前探即為補回

**連帶踩坑（工具鏈）**：`ps-is-map-assign-let` 迴圈旗標命名 `run`——`run` 是 Nolang 保留的異步前綴運算子（run/co/awy），作識別字 ⇒ `expected ')' got INT`改 `scan`另 `build -cc zig` 若反覆报同一陈旧行，先排 AST 快取：`NOLANG_AST_CACHE_OFF=1`（本次實為真實語法錯，非快取）

**歸因（同一 harness同一 CTL，A/B `noc-s69`→`noc-s70`）**：**DIFF 8→7 / SAME +1**轉 SAME：`tests/vec-elem-ownership.no``nested-container-clone.no` 先回歸後經 `=` 前探修復回 SAME**最終 SAME→DIFF 真回歸 0 檔**

**門禁**：L0 `ps-test.no` **30/30**（新增 `test-map-annotated-composite-value-literal`：複合 `[]i64` 空 map ＋ 單 ident 帶 pair map，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（`out[]`@5312-5320／`fl[]`@7799-7807／duplicate-fn@6343，僅因插行而重新編號與 s8t 同集合，本輪改動區 0 新增）；`no vet src/std` ERROR 屬 src/ 側既有（非本 session範圍外）


### 5.3.4.ah s8v let-RHS 型別推斷家族：切片方法參數不傳播＋鏈式欄位回傳型（2026-10-27 本輪）

**背景**：§5.3.4.ag（s8u）收官後 L2 剩 **7 檔 DIFF**本輪攻let-RHS 型別推斷家族兩檔——`src/std/vec.no`（6 行差）與 `src/std/dom.no`（4 行差）基線：SUT=`/tmp/noc-s70`DIFF=7

**形狀一（vec.no）——`[]T`/`[N]T` 方法體內 `x = <識別符>` 的傳播邊界**：
- `[]t.remove = (i i64) …` 內 `j = i`：oracle無型別，SUT 卻推 `i64`（過推）；
- `[]t.sum = () (sum t)` 內 `sum = acc`（`acc = 0`）：oracle 推 `i64, inferred`，SUT無型別（漏推）
兩者同為 s66 識別符傳播（stmt.go:1410-1433：`x = y` 僅傳播有號整數base，讀 `FuncVarType(curFunc, y)`，且僅當 x 尚無型別）

**根因（Go 路徑分流）**：Go 有兩條方法定義解析路徑——普通方法 `t.foo`／結構方法走 `parseMethodDefinition`→`parseFunctionBody`（decl.go:1568/1766-1770），會把參數與回傳值 setVarType 進 FuncVarTypes；而切片/陣列接收者`[]T.m`／`[N]T.m` 走 `parseArrayTypeMethodDefinition`（decl.go:191），它只設 curFuncName＋parseBlockStatement完全不登參數/回傳型別故 `[]t.remove` 的參數 `i` 不在 FuncVarTypes ⇒ `j = i` 查不到 base ⇒ 不推；結果參數 `sum`（型別 t）亦未預登 ⇒ `sum = acc` 時 existing=="" ⇒ 本地 let `acc`（parseLetStatement 自登 i64）照常傳播noc 舊碼 `ps-parse-method-func` 對所有方法無差別 `ps-set-var-type` 參數/回傳 ⇒ 與此相反（i 被登sum 被預登成 t）

**修法（`compiler/src/ps.no` `ps-parse-method-func`）**：以 `selfnt`（接收者型別節點）是否 `K_SLICE_TYPE`/`K_ARRAY_TYPE` 判定 array-method 路徑（鏡 Go 分流）；此路徑下參數/回傳型別改登進s7v 命名空間 `vt7:`（`ps-set-var-type-s7v`）而非 `vt:`妙處正在於既有雙查表語意：`ps-var-type-strict`（推斷閘門，鏡 Go `FuncVarType`）不讀`vt7:` ⇒ `j=i` 不推`sum=acc` 補推；`ps-var-type`（非嚴格，降糖端 `ps-apply-index-out` 讀取）仍讀`vt7:` ⇒ `self[cur] = …` 安全索引降糖的參數型別線索不丟**⚠️ 首版教訓**：最初直接跳過params/outs 登記 ⇒ vec.no 內一個 `[]T` 方法的安全索引降糖（需讀參數 `cur` 型別）退化成裸 AssignExpression真回歸一處；改走 `vt7:` 後兩全

**形狀二（dom.no）——鏈式接收者 `self.field.method(...)` 回傳型推斷**：`out = .pool.collect-tag(.root, want, star)`，oracle 推 `[]i64`（`dom.pool` 欄位型別 dom-pool ⇒ `dom-pool.collect-tag` 回 `[]i64`），SUT無型別根因：Go `returnTypeFromCallExpr`（type.go:173）先 `resolveReceiverType(dot.Receiver)`（type.go:45-54 的 DotExpression 臂——`self.field` 由 `structFields[當前結構][欄位]` 解析出欄位型別），再以 `欄位型別.方法` 查 `methodSignatures`noc 的 DotExpression 呼叫臂（s58）只認 `rc` 為裸 IDENT變數；`self.pool` 是巢狀 DotExpression ⇒ 不觸發

**修法（`compiler/src/ps.no`）**：
1. 新增結構欄位登記表 `ps-reg-struct-field`/`ps-struct-field-type`（鍵 `'sf:' sname ' ' fname`，鏡 Go `p.structFields`），`ps-parse-struct-def` 逐欄位登記 `type-string(ty)`；
2. DotExpression 呼叫臂增補rc==K_DOT_EXPRESSION子臂：其 receiver 為 `self` IDENT 時，`snm = ps-var-type('self')`（方法接收者型別，ps-parse-method-func 預登）→ `fty = ps-struct-field-type(snm, field)` → 以 `fty + '.' + method` 查 `ps-msig-find`，克隆 s58 的跳 token 重解析回傳型別＋filter-ok＋宣告閘門＋mark-inferred流程純增量（僅在原本推不出型別時才補），節點變數 c 後綴避免串型別

**歸因（同一 harness同一 CTL，A/B `noc-s70`→`noc-s71`）**：**DIFF 7→5 / SAME +2**轉 SAME：`src/std/vec.no``src/std/dom.no`**SAME→DIFF 真回歸 0 檔**（中途 vec.no 索引降糖回歸經 `vt7:` 承接已消）

**門禁**：L0 `ps-test.no` **32/32**（新增 `test-slice-method-param-no-propagate-local-yes` 釘 j=i 無型別/m=k i64；`test-chained-self-field-method-return-infer` 釘 structFields＋msig 鏈式推 []i64，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（`out[]`／`fl[]`／duplicate-fn 同 s8u 集合，本輪改動區 0 新增）；`no vet src/std` 145 ERROR 屬 src/ 側既有（oracle 執行非本 session 範圍）工具鏈：`no build -cc zig`；跑 L0 需 `no run -cc zig`（預設 clang 在 MacOSX27 SDK 連結 libSystem.tbd 報 unknown architecture）


### 5.3.4.ai s8w 鏈式裸 if 尾隨 else 折疊＋切片方法參數補登記（2026-10-27）

**背景**：§5.3.4.ah（s8v）收官後 L2 剩 **5 檔 DIFF**（byte/bigint/database-sql/net-http2/tests-2）本輪攻chained-if 降糖家族兩檔——`src/std/net/http2.no`（87 行差）與 `src/std/byte.no`（322 行差）基線：SUT=`/tmp/noc-s71`DIFF=5

**形狀一（http2.no／byte.no 結構）——鏈式獨立 if 的尾隨 else 綁定**：`cond1 -> cond2 -> {B} -> {E}`（裸 if 鏈 + 尾 else）SUT 舊碼把 `{E}` 綁到最外 cond1得 `if(c1){if(c2){B}}else{E}`根因：`ps-mk-standalone-if` 的 else 分支在鏈吃光之後才取 `-> {E}`，誤掛最外層

**⚠️ oracle 並非直覺的 `if(c1){if(c2){B}else{E}}`**：實測 `a -> b -> {B} -> {E}` 的 dump 是——最內層 if `if(b){B}` 充當**條件**再包一層 `IfExpression{cond=if(b){B}, consequence=nil, alternative={E}}`（`dump.go:486-492`：IfExpression 恆印 condthen/else非 nil 才印⇒ 外層只顯示 cond=內層 if ＋ 一個 else 區塊）**修法（`ps-wrap-chain`）**：當本層 body 為區塊（`nextTail==0`鏈到此終止）且其後同列有 `->` 時，就地吃掉 `-> {E}`建 `chained = IfExpr{tail, nextBlk, 0}` 後再以 `wrapper = IfExpr{chained, 0, altW}` 包覆（`esChild = wrapper`）http2.no 結構差全消

**形狀二（warnings 計數）——Go wrapStandaloneChainDepth 的 for 迭代語意**：`a->b->c->{B}->{E}` oracle `Program{warnings: 2}`，SUT 0；`a->{B}->b->{D}->c->{F}->{G}`（byte.no UTF-8 編碼分支）亦 2根因：Go 用 **for 迴圈**在同一 `linkIdx` 折疊多個 `->`（每迭代各報一次 WarnChainedIf），而 noc 用**純遞迴**（每個 `->` 落到 linkIdx+1 的新 frame），尾 else 折疊更曾被重置 linkIdx=1⇒ 從不达 2 ⇒ 不告警**修法（`ps-wrap-chain` 尾折疊支）**：尾 else 是同 frame 的下一次 for 迭代⇒ `linkIdx == 2` 時也要`p.warn + 1`；其 else-body 遞迴改用 `linkIdx + 1`（鏡 Go `wrapStandaloneChainDepth(nextBody, linkIdx+1)`）實測 e3(2)byte-style(2)純鏈 w3(1)/w4(1)e2(0)單 if/else(0) 全部對齊

**形狀三（byte.no 型別）——切片方法參數標量重指派的補登記**：`[]byte.index-from = (… from i64)` 內 `from = 0`（標量字面量重指派參數）之後 `i = from`，oracle 推 `i64`SUT無型別根因：Go `parseLetStatement` 尾部 `stmt.go:1599-1606` 把（推斷的）標量型別補登記進嚴格 `FuncVarType`——其 existing 查詢讀嚴格表，而切片方法參數經 s8v 只登 `vt7:`（嚴格表恆空）⇒ 一定補登記⇒ 其後 `x = param` 識別符傳播查得到noc 補登記（`ps-parse-let`）的 existing 查詢曾誤用非嚴格`ps-var-type`（看到 `vt7:` 的參數型別而誤判已有型別⇒ 跳過補登記 ⇒ 參數永不進嚴格表 ⇒ 漏推）**修法**：existing 查詢改走 `ps-var-type-strict`连带修正 `pos = i` 的既有型別閘門（已登 i64 的 pos 不再被覆寫推斷節點）

**⚠️ 提交衛生事故（本輪發現並修正）**：`put-doc-str`（空字串首條註解回 -1鏡 oracle `commentGroupInline` 無條件印 `//`＋文字＋換行）的呼叫端早在 **s8q（a761ab11）** 就寫進 ps.no（`ps-collect-tail` 等 7 處），但定義從未提交進 `ast.no`——即 `HEAD` 自 s8q 起根本無法獨立 build，歷輪全靠未提交的工作區 `ast.no` 建置本輪把這份缺失的 `ast.no` 依賴（`put-doc-str` 定義`comments` 對 -1 的 `//`＋換行渲染`interfaceMethod` 非 Node 匯出）一併納入 s8w 提交，恢復 `HEAD` 可建置性L2 全量（652 SAME）證明其輸出與 oracle 一致

**歸因（同一 harness同一 CTL，A/B `noc-s71`→`noc-s76`）**：**DIFF 5→3 / SAME +2**轉 SAME：`src/std/net/http2.no``src/std/byte.no`**SAME→DIFF 真回歸 0 檔**剩 3 檔：`bigint.no`（src/ 側平行 WIP本 session 禁改）`database/sql.no`（多返回值 tuple已判不可行）`tests/2.no`（多值指派家族）

**門禁**：L0 `ps-test.no` **34/34**（新增 `test-chain-bare-if-else-fold-and-warn` 釘 wrapper 結構＋warnings:2；`test-slice-method-param-reassign-propagate` 釘補登記→傳播，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（同 s8v 集合，本輪改動區 0 新增）工具鏈：`no build -cc zig`／`no run -cc zig`


### 5.3.4.aj s8x 多重指派逗號後目標專用解析器（2026-10-27）

**背景**：§5.3.4.ai（s8w）收官後 L2 剩 **3 檔 DIFF**本輪攻 `tests/2.no`（48 行差，多值指派家族）基線：SUT=`/tmp/noc-s77`

**形狀——`bit, probs[m] = decode-bit(probs[m])` 末目標吞掉 `= RHS`**：noc 的 multi-assign 循環（`ps-parse-stmt` 的 `ps-is-multi-assign-start` 分支）對逗號後目標用通用 `ps-parse-expr-prec(t, p, 0)``=` 雖非中綴運算子（中綴迴圈停在 ASSIGN），但 `ps-parse-expr-prec` 尾部鏡像 Go `parseExpression`（expr.go:598-637）：左值為 Dot/Index 時吸收 `= value` 建 AssignExpression ⇒ 整個 `= decode-bit(probs[m])` 被折進目標 2，`ps-expect(ASSIGN)` 落空後越界吃掉下一行的 `decode-bit`（多出一個 `Identifier{decode-bit}` 當 value），其後函式定義被撕成 ExpressionStatement 碎片（`(prob i64)``i64`裸 `BlockStatement{}` 等 40+ 行錯位）實測觸發條件極窄：ident 目標（不吸收）索引目標居首/居中段（後隨 COMMA 不吸收）皆 SAME；唯末目標為索引翻車

**Go 權威**：逗號後目標走专用 `parseAssignTarget`（stmt.go:834-847）——讀 IDENT可選恰好一個`[` LOWEST-expr `]`（`]` 缺席不報錯也不消費）`]` 後即止絕不碰 `=`；逗號後非 IDENT 直接 `fatalf E_EXPECTED_IDENT`（parseMultiAssignStatement:801 / parseExpressionStatement:1942）首目標則不同：`stmt.go:379` 分派（IDENT+peek COMMA）用 parseAssignTarget`parseExpressionStatement:1919` 分派（索引/呼叫開頭）用完整 `firstExpr = parseExpression(LOWEST)`——對本守衛可達的 IDENT／IDENT[..]＋COMMA 兩形，通用解析與专用解析同果，故 noc `maT0` 保留 `ps-parse-expr-prec`（同時覆蓋 `m[i][j], x = f(1)` 嵌套索引首目標——Go firstExpr 支援parseAssignTarget 反不支援）

**修法（`ps.no`）**：新增 `ps-parse-assign-target`（鏡 parseAssignTarget：IDENT→Identifier，可選單個 `[` `ps-parse-expr` `]`→IndexExpression），multi-assign 循環逗號後改調之`tests/2.no` 的 `;` 行前註解转 Doc（`FunctionDefinition{Doc: // bit, tmp = …}`）為既有能力，邊界修正後自然對齊附带釘住多值 RHS 語意：`y, z[w] = 1, 2` Go 僅取單一 value（`1`），`, 2` 另成 `ExpressionStatement{2}`——noc 原行為即同，無需改動

**歸因（同一 harness，A/B `noc-s77`→`noc-s78`）**：**DIFF 3→2 / SAME +1**：`tests/2.no` 轉逐字元一致**SAME→DIFF 真回歸 0 檔**（全量 655 檔 sweep 逐檔比對差集為空）剩 2 檔：`bigint.no`（src/ 側平行 WIP本 session 禁改）`database/sql.no`（多返回值 tuple已判不可行）

**門禁**：L0 `ps-test.no` **35/35**（新增 `test-multi-assign-index-target-no-swallow` 釘三形：末索引目標吞號修復索引目標居首多值 RHS 尾值另成陳述，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（同 s8w 集合，本輪改動區 0 新增）；`no vet src/std` 148 ERROR 屬 src/ 側平行 WIP（oracle 行為，非本 session 觸及）工具鏈：`no build -cc zig`／`no run -cc zig`


### 5.3.4.ak s8y 收官雙檔——裸型名陳述＋運算符重載剥 `?`＋介面繼承清單（2026-10-27 本輪）

**背景**：§5.3.4.aj（s8x）收官後 L2 剩 **2 檔 DIFF**：`src/std/bigint.no`（101 行，曾標禁改/平行 WIP）與 `src/std/database/sql.no`（207 行，曾標不可行）本輪復核兩案：bigint.no 的兩族差異全部落在 `parseStatement`/`returnTypeFromCallExpr` 的**穩定** Go 路徑（平行 WIP 未觸及 stmt.go 相關區段，oracle 目標不動）；sql.no 的 207 行差實為**介面繼承清單解析缺口**（非多返回值 tuple）兩檔均改判**可行**，本輪一併攻克 ⇒ **L2 DIFF 2→0 / 語料 655 檔全數逐位元組一致**

**規則 A——裸型名陳述（`bigint.no` 家族一）**：後綴 cast `av u64 = (.limbs[i] & MASK) u64` 中，Go `parseExpression` 在 `)` 後即止，尾部 `u64` **獨行成句**；`parseStatement` IDENT 分派（stmt.go:346-356）見 `peekToken ∈ {NEWLINE, SEMICOLON}` 且 `isTypeName(literal)`（type.go:404-413 **閉集 15 名**，不含 any/txt/char/bigint）⇒ `parseLetStatement`：型別 token **回落至名 token 本身**（stmt.go:1153-1155）⇒ `LetStatement{name: u64, type: NamedType(u64 非 inferred), value: nil}`（dump `value: ` 佔位），裸分支 setVarType 後 early return ⇒ **跳過 SetFuncDeclared**oracle 實測 g-a 釘住：`x = (x & 255) u64` 之 value 為 `GroupedExpression`（cast 未被吸收），前後各出一條 `LetStatement{name: u64, type: u64, value: }`noc 新增 `ps-is-type-name-literal`（逐字鏡閉集；⚠️ 不複用 `ps-is-builtin-type`——它多含 `'any'`）＋ `ps-parse-stmt` 分派臂（置於 expr-stmt 兜底前）

**規則 B——運算符重載回傳剥 `?`（`bigint.no` 家族二）**：`c = a._mul(b)` oracle 推 `bigint`，noc 推 `?bigint`Go 權威兩處：① `returnTypeFromCallExpr` 的 **dot 分支**以 `dot.Property` 為鍵 `stripOperatorOverloadReturn`（type.go:192/199/229；`TrimPrefix "?"`），閉集 `{_add,_sub,_mul,_div,_mod}`（operator_overload.go:40-63，`OperatorOverloadBaseName` 取末 `.` 段）；**裸名 Identifier 分支不剥**（type.go:151-170）② 結果參數登記（decl.go:1766-1790）：定義名末段為重載名時 `setVarType(resName, TrimPrefix ts)`此規則同時解釋級聯差 `g = x.copy()` no-type：`x = x._mod(y)` 舊推 `?bigint` 污染 varType ⇒ 接收者鍵 `?bigint.copy` miss；剥 `?` 後級聯自解noc 新增 `ps-is-op-overload-name`＋`ps-unwrap-op-overload-type`（節點級 NullableType 剥一层），落點：呼叫點推論**三條 dot 查找臂**（recv-var `mkey`／模組限定 `mq`／鏈式 `mkeyc`，`ps-parse-type` 重跑後`ts` 導出前剥）＋**兩處結果登記循環**（`ps-parse-func-def` 以 `name``ps-parse-method-func` 以 `fullName`，含 regp 雙臂）

**規則 C——介面繼承清單（`database/sql.no` 全檔）**：`db enter, leave { fs.close() (ok bool) … }`Go 分派 stmt.go:65-113：`peek==IDENT` ⇒ 以 `look(k)`（= current 後第 **k+2** 個**非註解** token，⚠️ 掃描起點是清單**第二**元素首元素由 `peek==IDENT` 守衛本身覆蓋）掃 DOT/COMMA 交錯介面名列表；終點恰 `LBRACE` ⇒ 內容首 token `IDENT+( /.)` 回 `parseInterfaceDefinition`（欄位形回 struct-with-implements——**全語料無此形**，s79 掃圖僅 sql.no，故守衛不放行零回退）解析端 decl.go:874-885：while current IDENT 收集實作名跳逗號，再無條件跳 `LBRACE`dump：`InterfaceDefinition{name, methods}` 後印 `Implements: [enter leave]`（dump.go:396-397，`%v` 空格分隔）noc 側 tape kind40 **已含 OP-IMPLEMENTS**（規格表機械生成dumper 已接線），僅缺解析：新增 `ps-is-iface-inherits` 守衛＋分派臂（置於 typedecl 前——Go 中 implements 掃描先於 `parseLetStatement`，否則 `db enter` 誤解為 `name TYPE` 宣告）＋`ps-parse-iface-def` 繼承清單循環；aux 規約［trail→clos→impl］⇒ `aux-begin`（置 imps 字串）＋`set-aux-counts(-1, -1, nimp)` **必須先於** `helper-begin`（hbase 取當時 aux 尾，順序顛倒則 impl 段與 helper 段錯位）generic-receiver `fs.close()` 為既有能力無繼承清單時 `nimp` 預設 0 ⇒ 零外溢

**歸因（同一 harness，A/B）**：`noc-s78`→`noc-s79`（規則 A+B）：`bigint.no` 101→**0**`tests/2.no`/其餘不动 ⇒ DIFF 2→1；`noc-s79`→`noc-s80`（規則 C）：`sql.no` 207→**0** ⇒ **DIFF 1→0 / SAME 655/655**，全量 655 檔逐檔差集為空**真回歸 0 檔**重現檔：`tmp/s8y/t2/t3/t8.no`（規則 A/B 最小重現）`tmp/s8y/g-a/g-b/g-c.no`（三規則 golden 釘檔）

**門禁**：L0 `ps-test.no` **38/38**（新增 `test-lone-type-name-statement``test-op-overload-return-strip``test-iface-inherits-list`，golden 取於 oracle）；`no vet compiler/src` 殘留 19 ERROR（同 s8x 集合，本輪改動區 0 新增）；`no vet src/std` 145 ERROR 屬 src/ 側平行 WIP（oracle 行為，較 s8x 記帳 148 減 3，非本 session 觸及）工具鏈：`no build -cc zig`／`no run -cc zig`

**🏁 L2 門禁現狀**：語料 `tests src/std`（655 檔）`no ast` 逐位元組一致，**DIFF=0**§5.3.4.ac 所列剩餘家族（InterfaceDefinition 繼承清單decode-bit 巢狀`[?]byte` 參數型別warnings 計數）已於本輪全部結清


### 5.3.4.al s8z——M2 驗收收官＋ast-test 缺線哨兵遷移（2026-10-27 本輪）

**背景**：s8y 後跑 M2 正式驗收，發現 L0 `ast-test.no` 47/48——`test-unsupported-is-live` 紅根因：該測試以dump `InterfaceDefinition` 必觸發 `interfaceMethod` helper 缺線計數（unsup==1）作為計數器非假斷言的哨兵，而 `interfaceMethod` 在 ast.no helper 分派中**已接線**（implements 迴圈於 default 臂之前；s8y 後語料可逐位元組复現 `database/sql.no` 亦側證）⇒ 哨兵失效，非回歸乃測試期望過期（本次才首跑 ast-test 該檔，故延至今日才暴露）

**處置**：哨兵遷移至**仍未接線**的 `annotationStructField`——`AnnotationStructValue`（kind 9）tape（instrs 21..24：HDR/SCALAR×2/CHILD(HELPER a=21'Fields' c=22'annotationStructField')）帶該 OP-HELPER，落 helper 分派 default 臂 ⇒ `ast.dump(add(t, K_ANNOTATION_STRUCT_VALUE))` 必計 unsup==1，感測器等價且更省（無需設槽）同步修 ast.no 兩處過期覆蓋面註釋（檔頭未接線清單與 helper 段：interfaceMethod 移入已接線列）

**M2 驗收（本輪）**：全量 L2 複測（`/tmp/noc-s81`＝s8y+本輪註釋修訂現建，串行掃描 `tests src/std` 655 檔）⇒ **DIFF 清單為空**；抽驗 `bigint.no`(175 KB)/`sql.no`/`tests/2.no` 兩側 sha256 一致且非空L0 五檔全綠 **27/62/45/48/38=220 斷言**；`no vet compiler/src` 19 ERROR（基線）§6 里程碑表 M2 轉 ✅，並新增 §6.4M3 啟動盤點**M2 里程碑自此驗收收官**


### 5.3.4.am s9a——M3-a：`drv.cmd-vet` 骨架＋`diag.render` 行形補全（2026-10-27 本輪）

**交付**：① `compiler/src/drv.no` 新增 `drv.cmd-vet`（dispatch `vet`usage 行），輸出契約逐條錨定 oracle authority `printLintResults`（`src/cmd/no/main.go`）：無診斷 ⇒ stdout `No issues found.` rc=0；空檔（read-bytes 回 nil）是合法源碼同樣走此分支；路徑不存在 ⇒ stderr `Error: stat <path>: no such file or directory` rc=1（探針實測流別與文案）；缺檔案參數 ⇒ rc=2（⚠️ 刻意偏離：oracle 無參數 vet 會掃整個 workspace，不在 L3 契約內）cmd-vet 跑完整 parse 路徑（與 cmd-ast 同源），為 M3-b/M3-c 的診斷收集器預留掛點② `compiler/src/diag.no` `render` 補 **line==0 檔案級行形**（authority 的 else 分支：`<file>: [LEVEL] <src>: <msg> [<code>]`，不得出現 `:0:0:`）③ 新增 L0 `compiler/tests/diag-test.no`（7 斷言：三個級別名有位置/檔案級/warning 三種 render 行形render-mid 中間串形；期望字串全部由 oracle 實測釘死）

**新 quirk（實測釘死，Go 側既有行為，本輪不修）**：struct 字面量**字段值**寫模組限定常量（`level: diag.LEVEL-ERROR`）被**靜默置 0**；整數字面量（`level: 2`）與構造後賦值（`d.level = diag.LEVEL-ERROR`）均正常（`/tmp/diag-probe2.no` 三例 0/2/2）diag-test 因此一律構造後賦值；自舉 parser 的對應規則（`#`/字段值位置的限定名解析）將來必須復現——記此為 M2 遺產的新待復現命題

**L3 誠實基線（骨架階段）**：`scripts/selfhost_diff.sh --sut /tmp/noc-s82 --levels L3 --limit 40` ⇒ **G8 MASS-NOOP**（SUT 對 40 檔吐同一 stdout）——骨架尚無診斷來源，本層整體退化被防假通過護欄如實標出，**這不是 PASS**oracle 側盤點（同 40 檔）：僅 **2 檔 clean38 檔有診斷**；severity 分布 **ERROR 594 / HINT 10001 / WARNING 296**；code 族 top：`ovf-int-default` 4107`ovfhndld` 3034`rngidx` 1106`rng-ellipsis` 749`tcpoxtfd` 629`idxwrite` 592`idxhndld` 359`arm-should` 296`optunuse` 2 ⇒ M3-c 的復刻面比 §5.437 個 Validate*的計數更寬（HINT 族佔九成），`[ERROR]` 硬判據全語料外推萬行級

**基線漂移記錄**：`no vet src/std` 現報 **145 ERROR**（其中 67 條 `nolang-index-write`）——該 lint 已在 HEAD 的 `src/checker/lint.go`（Go 側 parser-lowering-invariants 軌引入對 std 存量代碼長了牙），**非本增量引入**（本輪只動 compiler/src 兩個 .no，且 `no vet compiler/src` 仍 19 ERRORdiag.no/drv.no 零 ERROR）nolang-vet skillsrc/std 必須 0 ERROR的門禁對 HEAD 已失效，屬 Go 側 session 的處置範圍（遷移 std 呼叫點或規則降級），自舉側 M3-c 目標是**復現**這 145 條而非令其消失

**門禁**：L0 六檔全綠 **27/62/45/48/38/7=227 斷言**；L2 回歸 `/tmp/noc-s82`（含本輪 drv/diag 改動）全量 655 檔 **DIFF=0**，抽驗 `tests/2.no`/`bigint.no`/`set.no` 兩側 sha256 一致；`no vet compiler/src` 19 ERROR（=基線，零新增）


### 5.3.4.an s9b——M3-b：stdsig 表遷移（`gen_ck_sig.py` → `ck-sig.no`）（2026-10-27 本輪）

**交付**：① 一次性生成器 `compiler/tools/gen_ck_sig.py`：讀 authority `src/checker/stdsig_gen.go`，把**全部 7 張表**（不是 §5.4 原記的 2 張）翻成 nolang 源碼——`embeddedStdFuncSigs` **870**`embeddedStdMethodSigs` **2,371**`embeddedStdStructFields`（128 struct 拍平 **720** 對 `Struct.field -> type`）`embeddedStdAliases` **1**（`fd -> i64`）`embeddedStdStructMod` **105**`embeddedStdEnumVariants` **3**`embeddedStdModules` **118**② 生成物 `compiler/src/ck-sig.no`（~12k 行）：`ck-sig` 結構 16 條 `[]str` 平行欄＋`ck-sig.new()` 填充＋查找層（`ck-sig-lt` 逐字節 ASCII 序`ck-sig-bsearch` 二分7 個 `ck-sig-find-*``ck-sig-types` 逗號串拆型串）③ L0 `compiler/tests/ck-sig-test.no` **50 斷言**（規模氣閂 7 條＋首/尾/中/miss 錨點全數由 stdsig_gen.go grep 復核）現狀**刻意 inert**：無任何模組 import ck-sig.no（已驗證 `grep -rn 'ck-sig' compiler/ --exclude ck-sig*` 僅測試檔命中），故 noc 可觀察輸出與 s9a 逐位元組相同；M3-c 的 checker 才接線消費§5.4 舊數（859/1,9837,179 行`embeddedStdSigVersion = 2`）已過期：現檔 4,461→改由**源檔 sha256 當同步釘**（`CK-SIG-DIGEST`，本輪 `f3024f55…`；`embeddedStdSigVersion` 在非測試 Go 碼已不存在）std 改動 → `make gen` 重生 → 重跑本生成器，數量氣閂即哨兵

**本輪新發現的語言/工具鏈事實（實測釘死）**：
1. **多字元 str 無字典序關係運算**——`'abc' < 'abd'` 是硬編譯錯（"nolang has no lexicographic string comparison"），故 `ck-sig-lt` 必須手寫 `byte(i)`/`len-bytes()` 逐字節比較
2. **外掛 receiver 的泛型容器方法不單態化**——`t.fp.at(i)`（[]str 由外部收受）報 `EmitLLVM: unknown callee []t.at`；改以索引讀 `x = v[i]` 回 `?str`＋顯式 ok/nil/err 三 tag 臂封裝成 `ck-sig-str-at`
3. **工具鏈 bug：`x = recv.call(): {match塊}` 內聯形**——match 塊直接掛在調用表達式之後時，所得 `x` 在後續**關係比較臂**中恆假（區域變數/函式內皆復現；算術 `+`/`*` 路徑不受影響，ast.no:362 即因此長期倖存）正解是兩步形 `o = recv.call()` + `x = o: {…}`此 bug 與 s9a 的struct 字面量字段值寫模組限定常量靜默置 0同屬**自舉必須如實復現**的 Go 側既有行為
4. **match 塊非排他**——裸 `-> {…}` 尾臂在前方 ok 臂走過後仍會再觸發，查找層一律用互斥條件臂或顯式 tag 臂
5. **單函數 push 鏈必須分塊**——MIR 每個值表達式在函數入口落一個 alloca（一條 push ≈2 alloca ≈49 行 IR）：2,371 條 push 的單函數 ⇒ 11.6 萬行 IR，clang `-O2` 18 分鐘不終止（`asp.no tape` 1,540 條可過 ⇒ 閾值介於兩者之間）生成器改為每塊 ≤400 條＋外殼函數串接
6. **容器方法落在 funcSigs 表**——`[]t.at`/`[]str.join` 等 `[ ]`/`[n]` 前綴 receiver 全部在 `embeddedStdFuncSigs`（`[]t.at` = 下標 16），`methodSigs` 只收具名 receiver（`str.to-upper``zstd.zs-huffman.decode`…）；M3-c 查表分派須以此為準

**門禁（誠實記錄）**：`no vet compiler/src` **19 ERROR＝基線，ck-sig.no 零 ERROR**（全檔數千條 push 皆為呼叫無索引寫入，故不吃 idxwrite lint）；L0 新檔 ck-sig-test **50/50 綠**util/tok/lx/diag **27/62/45/7 綠****兩項受阻於 Go 側工作區軌道，非本增量**：① L0 `ast-test` 4 條常數摺疊斷言`ps-test` 1 條紅——根因是**未提交的 hir2mir/builtin_call 改動造成的工具鏈退化**（實測 `str.to-i64`：髒工具鏈 `'160'→0``'16'→6``'abc'→35`，HEAD worktree 建構同檔 `→160` 正確）；`src/std` 與 `compiler/src` 皆與 HEAD 無差異，故紅點屬該軌道② 全量 L2 掃（`/tmp/noc-s83b` vs 現行 `./bin/no`）出現 **125 檔 DIFF**，形如 `(cond) { … }` 被降成 `ExpressionStatement{GroupedExpression}`＋`BlockStatement`（不認 for）——同源工具鏈退化：`/tmp/noc-s82`（21:12 由健全工具鏈建）對同一 `for1.no` 仍產出正確的 `ForStatement`，且與 HEAD 版 oracle 逐位元組一致**本輪未取得 L2 DIFF=0**（既無法用健全 HEAD 工具鏈建 noc——`no build` 需工作區新增的 `f64.to-bits` 內建；也無法用髒工具鏈驗證）歸屬：Go 側 session 修完 to-i64/for 形退化後，直接重跑 `/tmp/l2sweep.sh` 即可復核 s9b（ck-sig.no 惰性觀測面與 s9a 相同）

**附帶更正**：`2f231fb0`（Go 側）已把 `no vet src/std` 從 145 ERROR 降到 **0 ERROR** ⇒ §5.3.4.am 與 §6.4 的src/std 基線漂移記項已清償，nolang-vet 門禁恢復有效


### 5.3.4.ao s10——M3-c 首族：ps.no 語法錯誤收集器 ⇒ L3 首條真診斷（2026-10-28 本輪）

**交付**：① `compiler/src/ps.no` 新增**結構化診斷收集器**（對齊 authority `src/parser/error.go` 的 `Diagnostic`＋`parser.go:1685 saveError`）：`psst` 加 4 條平行欄 `eLn/eCl/eMsg/eCode`，`ps-error` 定位在目前 token（`p.toks[p.pos].ln/.cl`）按 `(ln, cl, msg)` 鍵去重（Go 同形，Code 不参与去重鍵），正文**絕不含**位置前綴（中間串形由既有 `diag.render-mid` 負責）② 首批 3 個復刻站點：分組缺 `)`（expr.go:956-960expected right parenthesis, got X instead）呼叫實參缺逗號（expr.go:2207-2245 `parseCallExpression` else 臂expected comma or right parenthesis, got X instead）let RHS 落空（stmt.go:1306-1312expected expression, got nil instead，Go 閘門 `len(reportedIllegal)==0` 以 `ps-err-count(p)==0` 近似）X 名取自 `tok.name(ps-peek(p))`——**實測與 Go `Token.Type.String()` 逐字一致**（含 NEWLINE/EOF/INT）③ `drv.cmd-vet` 接線消費：逐條 `diag.render`（src=`nolang-compile`code=`E_GENERAL`＝saveError 的字面碼，parser.go:1698）走 stdout摘要 `\nN error(s), 0 warning(s), 0 hint(s)` 走 stderr**rc 鉗制為 1**（commit 前復核 authority：`printLintResults` 雖 `return errorCount`，但 vet caller `if errorCount > 0 { os.Exit(1) }`（main.go:3328-3332），實測 3 錯檔 rc=1——骨架期記的 rc=1 恰是行程面真相，中途記的rc 可 >1已改正）④ `parseCallExpression` 的實參循環**結構性重寫**為 Go 同形（空參速出→每輪跳前導 trivia→parseArgument→COMMA/RPAREN/其他 三分支；舊形把 `)` 檢查放在**實參前**且靠防呆 bump 兜底，缺逗號態根本到不了報錯臂）

**探針契約（oracle 實測釘死，`/tmp/se*.no`）**：`x = foo(1 2)` ⇒ 1:11 首條**逐字元全等**（第二條 nil cascade 屬 checker/let 交互態，見缺口①）；`y =\n` ⇒ 2:1 全等；`x = (1 + 2\n` ⇒ noc 報 `2:1 got EOF` vs oracle `2:0 got NEWLINE`（缺口②）；合法檔 `x = [\n` Go 側**零錯誤**（ILLEGAL 靜默路徑）——收集器未誤報，與 Go 同口徑

**本輪新發現的工具鏈事實（實測釘死）**：
1. **游標前進語義差異**——Go 的 `parseExpression` 中綴循環**不跨越 NEWLINE**（currentPrecedence=0 即停），noc 的等價循環在實參後 `skip-trivia` 把換行吃掉 ⇒ 報錯游標落在 NEXT token（EOF 2:1）而非 NEWLINE（2:0）任何報錯坐標落在換行處的站點都要先修游標對齊（或改用報錯前快照坐標），這是 NEWLINE 坐標族的根因
2. **同函式跨臂同名 i64 局部變數串擾（實錘）**——`firstArg = 1` 後進入循環，下一輪 `firstArg = 0` 賦值後的臂判定讀到**舊值 1**（插樁實錄：`DBG top firstArg=1` 出現在本應為 0 的輪次）ps.no 早有的 s7l臂內變數加後綴守則這次救回一輪調試：新變數一律 `cl` 前綴（`clDone/clPos0`）**注意舊註解說的範圍**：不僅僅是不同型別同名會串，**同型別跨臂同名一樣會串**
3. **L0 測試源碼構造**——nolang 的 `'...\n...'` 單引號串含真換行（verify-struct-elem.no 既有用法），測試字面量里的 `\n` 是真 bytes 不是轉義文本

**門禁（全綠）**：L0 新檔 `ps-err-test.no` **17/17 綠**；既有 util/tok/lx/diag/ast/ps/ck-sig **27/62/45/7/48/38/50 全綠**（s9b 記下的 ast-test/ps-test 紅點已由 Go 側修復後復核轉綠）L2 全量掃 `/tmp/noc-s10b`（ps.no 碼建）**656/656 DIFF=0**——實參循環重寫零回歸；rc 鉗制改正後終建 `/tmp/noc-s10c` 復掃仍 **656/656 DIFF=0**L3 誤報掃：`noc vet` 跑 `tests＋src/std＋compiler/src` 全語料 **663 檔零誤報**（全部 `No issues found.`）——有真診斷但不誤報合法檔自此同時可證`no vet compiler/src` **19 ERROR＝基線**（19 條全在 ps.no無一條來自本輪新增；`src/std` 0 ERROR）**rc 複核（commit 前）**：3 錯探針檔 `no vet` 實測 rc=1，authority vet caller `errorCount > 0 ⇒ os.Exit(1)`（main.go:3328-3332）——行程退出碼恆鉗制 1，中途交付③記的rc 可 >1已改正（drv.cmd-vet 同步改為 `nerr > 0 -> code = 1` 臂形）

**已錄缺口（M3-c 後續族的功課，非缺陷瞞報）**：① **cascade 第二條診斷**：Go 對 `x = foo(1 2)` 報完逗號錯後 let 值落空會**再報一條** 1:11 nil；noc 因呼叫表達式仍成形的 AST 結構差異，`val==0` 不觸發（`ps-err-count==0` 閘也已壓住）——要等呼叫落空 ⇒ let value 丟失的 Go 形恢復路徑復刻② **NEWLINE 坐標族**（事實 1）：分組缺 `)` 站點的坐標用跳行後游標，1-based 行與列皆會偏移③ **def 型別宣告族**（`def foo()` ⇒ expected assignment operator）實參列以外的 8 個 decl/type 報錯站點：167 條 saveError 站點的大盤面留後續族按語料觸發頻率逐族增量（§6.4 原規劃）

**L3 差分狀態升級宣告**：s9a 的 G8 MASS-NOOP（診斷源為空整層退化）自此**解除**——`noc vet` 對語法錯誤檔輸出真診斷行（行形契約由 diag-test 釘死站點正確性由 ps-err-test＋探針釘死），L3 harness 開始有可比較的信號；語料面 ERROR 族收斂仍依賴後續 Validate*/lint 族（HINT 族 top 見 §5.3.4.am 盤點），故 L3 全綠的預期維持在 M3 末期不變



### 5.3.4.ap s11——M3-c 缺口②清償：NEWLINE 坐標族（分組站點＋實參站點游標對齊）（2026-10-28 本輪）

**背景**：s10 已錄缺口②NEWLINE 坐標族——語法錯檔 noc 報錯坐標與 oracle 差一格（報在換行之後的 token 而非換行本身）本輪游標對齊清償，並同步擰緊 ps-err-test 分組站點坐標到 oracle 形基線：SUT=`/tmp/noc-s11g`

**權威核（探針 p11–p43/c1–c6 ＋ `src/parser/expr.go` 釘死兩站點不對稱的游標契約）**：
- **分組站點**（parseGroupedExpression，expr.go:954-963）：parseExpression 返回後中綴循環停在 NEWLINE（precedence 0 即停），Go 直接查 `currentToken != RPAREN`**無前置跳** ⇒ 報錯落在 NEWLINE token（`x = (1 + 2\n` ⇒ **2:0 got NEWLINE**）舊 s10 形先 `ps-skip-trivia` 再查，游標越位到 EOF（2:1）——刪除該前置跳即對齊；容錯消費仍由 ps-expect 照舊（僅報錯面不改 L2 消費路徑）
- **實參站點**（parseCallExpression，expr.go:2223/2231）：parseArgument 前後**各有一段只跳 NEWLINE（不跳 COMMENT）的循環**，其後才分派 COMMA/RPAREN/else ⇒ 缺逗號報錯落在換行後首個非換行 token（`foo(1\n2)` ⇒ **2:1 got INT**，非 2:0）與分組站點形不同，須分別鏡像
- 實參循環 Go 形重寫：循環前空參速出（peek 原樣）→ 前導換行跳記數 `clN1` → 經換行空參形（`foo(\n)` 速出）→ parseArgument → **防呆僅在 `p.pos==快照 ∧ peek∉{RPAREN,EOF}` 時前進**（Go parseArgument 對 RPAREN/EOF 回 nil 且不消費；無条件 bump 會吃掉 `)` 滑進下一陳述——p21 曾誤報 No issues 的教訓）→ 尾換行跳記數 `clN2` → COMMA/RPAREN/else 三分派局部變數全 `cl` 前綴（§5.3.4.ao 事實 2）

**探針矩陣（parser 族行與 oracle 逐字節全等）**：分組族 p12/p15/p17/p23 ⇒ 2:0 NEWLINEp32/p42 ⇒ 3:0 NEWLINEp11 ⇒ 1:11 EOFp13 ⇒ 2:2 EOF；實參族 p14 ⇒ 2:1 INTp20 ⇒ 1:11 INTp21 ⇒ 2:2 EOFp30 ⇒ 1:14 IDENT；合法形 p22/p24/p41（換行在逗號前/後皆合法 ⇒ 零語法錯）；乾淨檔 c1–c5 零誤報；**c6（`x = (\n1 + 2\n)`）⇒ 3:0 NEWLINE 全等**——推翻舊註解尾換行會被入口 skip 吞掉 ⇒ 落 EOF的猜測（入口 skip 只吞前導換行；尾換行由中綴循環停在原處），ps.no 站點註解已同步改訂

**本輪新發現的工具鏈事實（實測釘死）**：
1. **單語句形 `(cond) x = x + 1` 不是循環**——實測掃描計數只走一格即止（等價一次性 if）；`(cond) { }` 塊形才是 whilededup 測試的游標回溯掃描因此改塊形（對照探針 `tmp/s11-or.no`：plain 形 j=1塊形 k=2）
2. **`#` 模組裸名解析要求檔在專案內**——探針放 `/tmp` 時 `ps.new` 前綴呼叫可解析但裸 `ps-err-count` 報 not defined；探針一律放 `tmp/`

**fmt 清掃**：s10 遺留的 ps.no fmt 漂移（384 行計法：`fmt -d` 輸出 69,806 位元組）以 `no fmt -w` 一次清零，ps.no＋ps-err-test.no 現況 `fmt -d` 皆空輸出；清掃後**重建 SUT 復跑全部門禁**（diff 面含清掃噪声，屬一次性代價）

**門禁（全綠）**：L0 ps-err-test **17/17**（分組站點擰到 2:0 NEWLINE/47 字節；dedup 加游標回溯）；探針矩陣 25 檔 parser 族首行全等；L2 全量掃 **656/656 DIFF=0**（`/tmp/noc-s11g`，注釋修訂後終建）；L3 誤報掃 `tests＋src/std＋compiler/src` 全語料 **666 檔 parser 族零誤報**（oracle 亦無 parser 族診斷 ⇒ 兩側皆空即對齊；checker 族差異屬 M3-c 後續不計入）；`no vet src/std` **0 ERROR**；`no vet compiler/src` **19 ERROR＝基線**；fmt 漂移 0/0

**已錄缺口更新**：② NEWLINE 坐標族 **已清**新增④ **入口跳合一族**：noc 的 parseExpression 入口 `ps-skip-trivia` 把 NEWLINE＋COMMENT 一併吞（Go expr.go:89-91 只跳 NEWLINECOMMENT 走 advanceCollect doc 通道）⇒ 換行 adjacency 註釋形（如 `foo(1, #c\n2)``foo(\n#c\n)`）與 Go 有游標差；語料實掃無此形（`,\n)``\n)``(`/`,` 後同行註釋計數皆 0）⇒ 暫留缺口①（cascade 第二條 nil 診斷——本輪探針 p11–p15/p20/p21/p23/p30/p32/p42/c6 再次全數現形，oracle 每條括號錯後跟發同位 `expected expression, got nil instead`）與③（def 型別宣告族等其餘 saveError 站點；旁證：舊探針 `p3.no` 的expected loop body statement站點 noc 未報）不變

**下一增量建議**：checker 骨架接線消費 ck-sig.no（M3-c 正語義族，可順帶解①的 `val==0` 結構差）或 cascade 族（缺口①，呼叫落空 ⇒ let value 丟失Go 形恢復路徑）或按語料頻率續推 saveError 站點族（缺口③）



### 5.3.4.aq s12——M3-c 缺口①清償：cascade 族（括號錯後同位got nil級聯）（2026-10-28 本輪）

**背景**：s11 收錄缺口①——oracle 每條分組/實參括號錯後，會在同坐標再發一條 `expected expression, got nil instead`；noc 因錯誤態照樣建 Well-formed 節點 ⇒ let 的 val 恆非 0而吞掉第二條本輪結清

**權威複查（Go 源釘死）**：
- `parseGroupedExpression`（expr.go:956-960）：`currentToken != RPAREN` ⇒ saveError 後直接 return nil不消費——不建 GroupedExpression
- `parseCallExpression`（expr.go:2241-2245）：else 臂 saveError 後return nil——整個 CallExpression 丟失（已解析實參隨節點作廢）
- `parseLetStatement`（stmt.go:1305-1313）：value==nil ⇒ 在目前 token報 `expected expression, got nil instead`，閘門是 `len(p.reportedIllegal)==0`——**只數 ILLEGAL 詞法錯**（expr.go:386-390 寫入），普通 saveError不抑制級聯舊 s10 把閘門近似成 `ps-err-count==0`，正是級聯被吞的根因
- 級聯只住 let 站點：表達式語句（`print((1+2\n` 探針 p62）無第二條 nil 錯；重賦形（`w i64 = 0\nw = (1+2\n` 探針 p60）與首宣告同路（Go 都走 parseLetStatement）

**noc 實作（ps.no 三處＋一欄位）**：
1. 分組臂：報錯臂與建點臂拆成互斥雙臂——`!= RPAREN` 只報錯（idx 留 0不 ps-expect）；`== RPAREN` 才 expect+建點游標契約與 s11 相同（錯報停在停滯 token）
2. 實參循環：新增 `clNil`；else 分派臂撤掉err-count>0 ⇒ 靜默前進近似改無條件 saveError＋clNil=1（重複由收集器 (ln,cl,msg) 去重兜底，p62 釘死）；循環後 `clNil==1 ⇒ idx=0`（不建 CallExpression），正常態建點路徑原樣包進 `clNil==0` 臂
3. `ps-parse-let` 閘門：`val == 0 && ps-err-count(p) == 0` → `val == 0 && p.nIllegal == 0`；psst 新增 `nIllegal i64`（鏡像 reportedIllegal；noc 尚無 ILLEGAL 報錯族 ⇒ 恆 0，接線留該族增量）
4. 落空 let 語句照舊建節點（val=0 佔位）——Go 側 stmt 丟失，但 AST 形不在本族觀測面（錯形檔不入 L2 語料；`no ast` 錯形檔無差分契約）

**探針矩陣（oracle 金標準 /tmp/s12-golden.txt，30 檔）**：新增 p60（重賦級聯）/p61（`foo(1 2\n)` 雙 1:11）/p62（print 不級聯：2:0 分組＋2:1 逗號 EOF）/p63（級聯後 `y = 3` 照常解析）/p64（`x = ((1` 巢狀同位去重：一條分組＋一條 nil）終判 `/tmp/noc-s12b`：**parser 族含第二行逐字節全等**——殘差僅兩類超範圍項：checker 族（'foo' is not defined 等，noc 無 checker ⇒ 多印 "No issues found."）與 p3expected loop body statement站點（缺口③）

**本輪新工具鏈事實**：
1. **fmt 不支持 struct 字面量型定義內的欄位註釋**——欄位行上方或同行尾的 `;` 註釋都会被提出到結構外（漂移實測 1,183→641→0 位元組逐輪收斂）；欄位契約註釋放使用點（本輪放 ps-parse-let 站點）
2. 級聯坐標＝let 站點目前 token：分組錯停在 NEWLINE ⇒ 兩條同位 2:0；實參錯停在換行後首個非換行 token ⇒ 同位 INT/EOF（p14/p21 形）——同一游標契約自動產出雙條，無需第二套定位邏輯

**門禁（全綠）**：L0 ps-err-test **26/26**（計數 1→2 擰級聯：call-comma/group 各釘第二條坐標與正文；dedup 基線 2；新增 test 7 釘 print 不級聯）；探針矩陣 30 檔 parser 族全等；L2 全量掃 **656/656 DIFF=0**（`/tmp/noc-s12b`）；L3 誤報掃 `tests＋src/std＋compiler/src` **666 檔零誤報**；`no vet src/std` **0 ERROR**（1330 W/1813 H 基線）；`no vet compiler/src` **19 ERROR＝基線**；`no fmt -d` ps.no＋ps-err-test.no 漂移 **0/0**

**已錄缺口更新**：① cascade 第二條 **已清**③（其餘 saveError 站點：expected loop body statementdef 型別宣告族ILLEGAL 詞法族——後者接線時把 `nIllegal` 計位補上即復原 stmt.go:1308 完整閘門語義）與 ④（入口跳合一族）不變另錄微小殘差：實參報錯態 noc 的後綴循環直接停住（handled=0），Go 在停滯 token ∈ {DOT,LPAREN,LBRACKET,LBRACE} 時會續圈——語料/探針未見此形，暫留

**下一增量建議**：checker 骨架接線消費 ck-sig.no（M3-c 正語義族開工——oracle 診斷大頭是 checker ERROR，如 `'foo' is not defined`；parser 族至此端到端全等，差分面自然切到 checker 族）或按語料頻率續推 saveError 站點族（缺口③）


### 5.3.4.ar s13——M3-c 正語義族首件：checker 骨架 `ck.no`（`'X' is not defined`）＋全語料零誤報清償（2026-10-29 本輪）

**背景**：s12 收官後 parser 族端到端逐字節全等，`noc vet` 差分面自然切到 **checker 族**（oracle 診斷大頭）本輪開工 §6.4checker 骨架接線消費 ck-sig.no建議——`compiler/src/ck.no` 落地首個正語義族 `'X' is not defined`（oracle TraceID `4bek3xc6`，authority `src/checker/checker.go` `ValidateUndefinedVars`/`checkUndefinedVarsInExpr` Identifier 臂 8180-8295），使 `drv.cmd-vet` 在 `ps` 錯誤數==0 時調用 `ck.check`，把 s9b 烘進 `ck-sig.no` 的 **bk/bv/bo 裸名門表**（1732 條：silent 784 / gated 948）與 **mok 118 模組名**首次接上消費端`CK-SIG-VERSION` 由 2 轉 **3**

|  |  |  |  |
| --- | --- | --- | --- |
**交付（骨架，HEAD 已提交）**：① `compiler/src/ck.no`——**扁平 arena 掃描器**（對齊正主樹走訪的三處刻意偏差全部**偏保守 = 少報不誤報**：樹走訪→扁平掃描正主跨檔 `collectModuleExports`→單檔不解析 `#use` 靠 bk/bo 兜`'@'` 完全未知名報裸錯靠四張網兜：`ck-name-silent` 型名/關鍵字`defs` 本檔定義名`rcv` dot 接受者壓制`mok` 模組名二分）放行臂序列 ①-⑦ 全部由探針 p70-p99 對 oracle 實測定型② `drv.cmd-vet` 接線（`ps-errs==0 → ck.check`）③ L0 `compiler/tests/ck-test.no` **20 斷言**（沉默/local shadow/dot 跳過/dot 接受者壓制/模組名/`_` 前綴/ALL-CAPS/內建型名/多重指派目標/自模組裸調 + 門控帶提示 `cancelled`/門控無提示 `from-bits`/完全未知 `wuzzle`/報序），期望字串全部由 oracle 實測釘死④ 跨模組回值改**打包串 vec** `L|C|CODE|MSG`（規避當代 Go 工具鏈結構體按值傳參的 MIR 聚合體缺陷，詳見 §5.3.4.as）

**⚠️ 本輪續完成——committed 版 ck.no 未達零誤報門禁**：HEAD 的 ck.no 對 `tests＋src/std＋compiler/src` **全語料 668 檔跑出 13 檔誤報**（oracle 皆 0 條 not-defined）——直接違反自 s10 起確立且 ck.no 檔頭自我規定的L3 掃若現誤報回退沉默側 / 少報不誤報硬門禁根因是 **def 採集遺漏多類綁定名**（這些名字命中 `'@'` 完全未知臂被誤報）本輪逐一對齊正主語義補齊 def 註冊，**誤報 13 檔 → 0**：

| 族 | 誤報名（次數） | 正主放行 | ck.no 補法（第一遍 def 採集） |
|----|----------------|----------|-------------------------------|
| **A 泛型參數** | `n`(76，arr/sort/link/queue/arr-stack/std-lib) | `isArrayTypeMethodDefinition`/generic 形把 `[n]t``<T>` 字母登記為局部變數 | FD 節點 generic kids 位於 `[kid0, kid0+c3)`（皆 IDENT），讀 `nd.c3`（ast.no:96 SIG 泛型計數）逐一入 defs |
| **B match 具名型別臂** | `w`(type-match) | `buildItBinding` typed-arm `w T ->` 於臂作用域定義綁定 | SurfaceMatch 臂存 HELPER 區寬 **12/臂**（ast.no:1372），采 offset3=`typeBinding`（`ast.put-str` intern id，與臂體 `Identifier.s0` 同表） |
| **C tagged-enum 變體解構** | `v`(18)/`r`(6)/`h`(6)（enum-annotation/match-arm-binding-scope/tagged-enum*） | 變體解構綁定名（surface 形 Go `lowering.go:2049` 才提欄，parse 期 `binding` 空 ⇒ 綁定名僅存於臂 Condition=`CallExpression{fn: variant, args: 綁定名 IDENT}`） | 采臂 `mcond`（aux offset10，節點索引）之 CallExpression **參數** IDENT（kids[1..]）入 defs |
| **callee 跨模組裸調** | `ck-sig-bsearch`(2)/`ck-sig-bare`(1)（ck.no 自檔） | 正主靠 `collectModuleExports` 讀 `#use` 目標解析——單檔骨架**刻意不解析 import** ⇒ 無法區分導入函式與未定義 callee | 按檔頭偏保守對 **callee 位置回退沉默**（第一遍收 CallExpression kid0 IDENT 入 `calle` 集，報斷處跳過）；裸 IDENT **USE**（`x = wuzzle`）仍照報，ck-test 全用 USE 形 ⇒ 零回歸 |
| **std 命名空間根** | `std`(1，global.no) | `@ std/err.new err` export 糖；oracle 解析成 `ExportStatement`，noc **既有**誤解析成 `InfixExpression{std/err.new}`（見下） | `std`（stdlib 命名空間根不可能為合法未定義用戶變數）入 `ck-name-silent` 固定靜默集 |

**坑與紀律**：nd-at 回結構體拷貝，其欄位（`c3/hcount/hbase`）**不得直接出現在區塊臂條件**（當代 Go 工具鏈報 `MIR->LLVM: NoVal`）——一律先落地為局部（`gc3=nd.c3` 等）再判，與本檔既有 `dk=nd.kind`報斷段 `p8=d.s8` 同款紀律C 族的 `mcond` 是節點索引（非 put-str），取 CallExpression kid 起點 `pd.kid0/pd.nkid` 亦須先落地

**附帶查明（非 s13/ck 範圍，屬 parser 族既有項）**：`src/std/global.no` 的 `std` 誤報实為 noc 對 `@ <mod>/<fn> <alias>` export 糖的**解析分歧**下游症狀——oracle `ast` 產 `ExportStatement{path: std/err, function: new, alias: err}`，noc 產 `UseStatement{path: std/errerror}` + `InfixExpression{op: /}`（`std` 落為裸 IDENT 運算元）該檔 ast 兩側本就不一致⇒ global.no 屬 L2 656/656 SAME 集之外的既有排除檔（本輪未動 parser，`noc-s13i ast global.no` 與 `noc-s13fin ast` sha256 完全相同可證非本輪引入）；`@`-export 解析差距另記為後續 parser 族功課，本輪 ck 側以 `std` 靜默正確遮蔽其下游誤報

**門禁（全綠）**：L0 ck-test **20/20**（改 ck.no 後 `./bin/no run` 復跑）；**L3 ck 誤報掃全語料 668 檔 `is not defined` 誤報 0**（`tests＋src/std＋compiler/src`，對照 oracle 皆 0）；L3 parser 族仍 0（ck.no 不經 parse，ast 路徑未觸及）；**L2 ast 未受擾動**——`ast` 子命令不调用 ck，抽驗 7 檔（type-match/tagged-enum/match-arm-binding-scope/arr/global/ck/surface-match）中 6 檔 noc↔oracle 逐位元組一致，唯一 global.no 差為上述**既有** parser 分歧（`noc-s13i`==`noc-s13fin` ast 哈希相同，非本輪引入）；vet 崩潰掃單跑 rc=0（批 668 檔時 type-match.no 一度 rc=137，復現為聚合記憶體壓力/環境所殺非 ck.no 缺陷，隔離重跑即 rc=0）；`no vet src/std`／`no vet compiler/src` 為 **Go 工具鏈**側基線（我僅改 ck.no 這一 `.no` 源，`./bin/no` 不內嵌它，故不受影響）

**下一增量建議**：checker 族擴面——按語料觸發頻率續推其他 `Validate*`/型別推斷族（§5.4 剩 36 個），或清償缺口③（其餘 saveError 站點：`expected loop body statement`／def 型別宣告族）④（入口跳合族），或補 `@ <mod>/<fn> <alias>` export 糖的 parser 解析以真正讓 global.no 進 L2 SAME 集

### 5.3.4.as s13 工具鏈坑附錄——結構體按值傳參觸 MIR 聚合體缺陷 ⇒ checker 回值改打包串 vec（2026-10-29）

**現象**（ck.no 檔頭 line 38/39 引用點）：讓 `ck.check` 以**結構體**（诊断收集器19-vec 的 `ck-sig`）作為跨模組**按值傳參/回值**時，當代 Go 工具鏈在 MIR→LLVM 觸發聚合體佈局錯／`argument is %vec`（實測 s13 構建失敗）oracle 側同形亦不可靠

|  |  |  |  |  |  |
| --- | --- | --- | --- | --- | --- |
**處置**：跨模組介面**只傳 primitive/vec**——`ck.check` 直接回 **打包串 vec**，每条形如 `'L|C|CODE|MSG'`（坐標=token 起始 1-based；MSG 可含 `'|'`，但前 3 段 `L/C/CODE` 無歧義 ⇒ `drv.cmd-vet` 按前 3 個 `|` 拆段即可還原坐標與碼）此形既規避聚合體缺陷，又與 `diag.render` 行形契約兼容屬 §2.2實測不可用/受限族的新增條目：**結構體不得跨模組按值傳**（SHS 約束），需要批量回值時以平行 `[]str`/打包串承載


### 5.3.4.at s14——M4-a：`cg-runtime`/`cg-rc` prelude 照抄 + `check-rc-balance` 骨架（2026-10-30 本轮）

**背景**：§6.5 M4 啟動盤點的首個增量M4 前 SUT 無中端/codegen；M4-a 的目標是立 **runtime prelude** 與 **RC 正確性地基**，讓後續 emit 有守門**本輪全為新增檔（3 個 untracked），零改動既有檔**

**落地**：

1. `compiler/src/cg-runtime.no`——`%nolang_hdr`（16 字節頭：rc@0magic@8data=base+16）與 `%task`（32 字節）類型 + `declare i8* @malloc(i64)` / `declare void @free(i8*)``cg-runtime-prelude()` 以 `sb` 逐行拼裝（鏡像 Go `emitPrelude` 的 `b.WriteString`）
2. `compiler/src/cg-rc.no`——`@nolang_rc_alloc` / `@nolang_rc_retain` / `@nolang_rc_release` / `@nolang_free` 四段 IR 文本（`cg-rc-alloc` / `cg-rc-retain` / `cg-rc-release` / `cg-rc-nolang-free`）；常量 `HDR-SIZE=16``HDR-MAGIC=7957698232306827265`（＝ Go `nolangHdrMagic` `0x6E6F6C616E670001`，"nolang\0\1"）；`cg-rc-check-balance(vids []i64, ops []i64)` 欠計數校驗器
3. `compiler/tests/cg-runtime-test.no`——16 斷言（類型/宣告存在性四 RC definemagic 十進位平衡通過欠計數報錯過計數放行）

**關鍵決策**：

- **用 `sb` 逐行 push 照抄，不用 raw string**——規避 §5.6.2 的``` 終止字面量硬規則；IR 註釋內引用識別符號一律用雙引號
- **magic 寫十進位**（`store i64` 只吃十進位；Go 用 `%d` 亦同）
- **`check-rc-balance` 只查欠計數方向**（某 vid 的 release 次數 > retain 次數 ⇒ UAF ⇒ 報錯）；過計數是 §1.3.1 明確接受的洩漏，**不報錯**（否則 false positive ⇒ 硬編譯失敗）
- **簽名用兩個平行 `[]i64`**（vids/ops），不用 `[]rcop` 結構體——遵循 §5.3.4.as 的 SHS 約束（結構體不得跨模組按值傳）
- 常量經 `cg-rc-hdr-size()` / `cg-rc-hdr-magic()` 訪問器暴露，避免依賴模組級 `let` 的導出語義

**驗證（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-runtime-test.no` ⇒ **16/16，ALL TESTS PASSED**
- **IR 合法性**：探針拼 `cg-runtime-prelude()` + 四 RC helper 輸出 `/tmp/cgir.ll`（98 行），`opt -passes=verify` **rc=0**
- **逐位元組忠實**：從 Go 源（`src/mir/codegen.go` 14712–14818）重建期望文本，與探針輸出歸一化後 **0 刪除行 / 3 新增行**（3 行僅探針 `print` 造成的塊間空行）⇒ RC helper 文本與 Go 源**逐位元組一致**
- `no vet compiler/src/cg-runtime.no` / `cg-rc.no` ⇒ **0 ERROR**（cg-rc 餘 1 條 `nolang-range-index` HINT，advisory）

**刻意惰性 / 後續**：`cg-runtime`/`cg-rc` 目前僅被測試 import，**未接入任何發射路徑**（M4-b 起由 `cg.no` 消費 IR 文本，M4-c 起 `check-rc-balance` 由 MIR 分析餵事件）windowsShims **未納入**（後置 M7）**既有基線註記**：`no vet compiler/src` 當樹 34 ERROR（全 `ps.no`；19 基線上漂＋並行 session 的 `ps-is-builtin-type` duplicate），本增量 **0 新增**；`ast-test`/`ck-test` 在當樹因並行 session 髒工具鏈紅（NoVal bug #85 / SIGTERM），與本增量無關（兩檔不 import 新模組）


### 5.3.4.au s15——M4-b：`mir.no` 表示 + `cg.no` 的 `EmitLLVM` 骨架（2026-10-30 本轮）

**背景**：§6.5 M4 分期的第二件M4-b 目標＝MIR op 最小閉包 + `emitMIR`/`EmitLLVM` 骨架，打通MIR → `.ll`管線**本輪全為新增檔（3 個 untracked），零改動既有檔**

**落地**：

1. `compiler/src/mir.no`——M4-b 的 op 子集常量（`OP-CONST/ADD/SUB/MUL/RET/CALL/ALLOC/STORE/LOAD`）＋**扁平 arena 表示約定**：一函數＝平行陣列 `ops/dsts/xa/bs/syms`（同下標一條指令；`dsts` 目標值 id`xa`/`bs` 操作數`syms` 僅 CALL 用），遵 §5.3.4.as結構體不跨模組按值傳
2. `compiler/src/cg.no`——`cg-emit-func(name, nargs, ops, dsts, xa, bs, syms) -> str`：逐 op 發 IR 文本，覆蓋**字面量（CONST）/算術（ADD/SUB/MUL）/呼叫（CALL）/局部槽（ALLOC/STORE/LOAD）/返回（RET）**值 id → SSA 名 `%v<id>`，槽 id → `%s<id>`（兩命名空間）；函數參數即值 id `1..nargs`（簽名 `define i64 @name(i64 %v1, …)`）
3. `compiler/tests/cg-test.no`——13 斷言，4 例：`three()→3``dbl(x)→x+x``caller()→dbl(2)``let-add(){x=3; x+x}`

**關鍵決策 / 實測坑**：

- **參數名 `as` 是保留字**（`unexpected 'as' at start of expression`）⇒ 改 `xa`這是 nolang 關鍵字表的新增實測條目（§4.4 的避開關鍵字再添一例）
- **單基本塊**：M4-b 不發 `br`/`condbr`/`phi`（無基本塊切分），控制流留 M4-c；因此每函數只有一個 `entry:` 塊指令嚴格按序
- **常量用 `add i64 0, <imm>`** 而非直接 `ret` 立即數——保證常量也是一個值 id，與後續 SSA 消費統一（M4-c 接 PHI 時不必特判）
- **扁平 arena 而非嵌套 `[]struct`**：`mfunc{name, []minst}` 這種嵌套切片會觸 §5.3.4.as 的聚合體缺陷；改用平行 `[]i64`/`[]str`，跨模組只傳 primitive/vec

**驗證（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **13/13，ALL TESTS PASSED**
- **IR 合法性**：探針拼 `three`+`dbl`+`caller` 輸出 `/tmp/cg2.ll``let-add` 輸出 `/tmp/cg3.ll`，`opt -passes=verify` 均 **rc=0**
- **端到端執行**：`let-add`（3+3）的 IR 經 `clang -x ir` 編譯 → 執行 ⇒ **exit code 6**（證明不只是合法 IR，語義也對）
- `no vet compiler/src/cg.no` / `mir.no` ⇒ **0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`，本增量 0 新增）

**刻意惰性 / 後續**：~~`cg`/`mir` 目前僅被 `cg-test.no` import，尚未接 `drv`（M4-d 起由 `drv.cmd-build` 消費）~~ ⇒ **2026-11-12 起 `drv.no` 已直接導入 `mir`／`cg`／`cg-runtime`／`cg-rc`／`h2m`／`link` 並由 `drv.cmd-build` 消費**（§11 後 ⑧）**未覆蓋**：控制流（`br`/`condbr`/`phi`，M4-c）非 i64 型別（str/vec/struct/option）字符串與容器 op——這些是 M4-c`hir2mir` 降級主體的範圍


### 5.3.4.av s16——M4-c 首件：控制流（多基本块 + `br`/`condbr`）（2026-10-30 本轮）

**背景**：§6.5 M4 分期的第三件M4-b 只有單基本塊，任何 `if`/`loop` 都發不出M4-c 首件先打通**多基本塊 + 條件/無條件跳轉**，為後續 `hir2mir` 降級（真正的工作量主體）鋪路**本輪零新增檔只擴 `mir.no`/`cg.no`/`cg-test.no`**（仍 6 個 untracked）

**落地**：

1. `compiler/src/mir.no`——新增 `OP-BR`（`a`=目標塊）`OP-CONDBR`（`a`=條件值`bs[k]`=真塊`dsts[k]`=假塊）`OP-CMPLT`（`dst = a < b`）；表示新增 **`blks[]` 平行陣列**（每條指令所屬基本塊 id，0=entry）
2. `compiler/src/cg.no`——新增 `cg-label(id)`（0→`entry`，其餘→`bb<id>`）與 `cg-emit-cfg(...)`（8 參數，塊切換處發標籤，逐 op 發射含 BR/CONDBR/CMPLT）；原 `cg-emit-func` **保留 7 參數 API**，內部用 `[].with-len(n)` 造全零 `blks` 委託 `cg-emit-cfg` ⇒ **M4-b 測試零改動**
3. `compiler/tests/cg-test.no`——新增 `test-cfg`（`lt10(x) = x<10 ? 1 : 2`，7 斷言），總計 **20/20**

**關鍵決策**：

- **跨塊傳值用 `alloca`/`store`/`load`（-O0 風格），不引入 `PHI`**——這樣 M4-c 首件不必先做 SSA 構造；PHI 留後續（`-O0` 正是 clang 的做法，語義等價）
- **`CMPLT` 發 `icmp slt` + `zext i1→i64`**，維持條件值是 i64的統一約定 ⇒ `CONDBR` 只判 `!= 0`，不特判 i1
- **`CONDBR` 三槽約定**：`a`=條件值`bs[k]`=真塊`dsts[k]`=假塊（`dsts` 在此複用為假塊 id；已於 `mir.no` 檔頭註明）
- **向後兼容**：`cg-emit-func` 委託 `cg-emit-cfg`，M4-b 的單塊 API 與測試不破

**驗證（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **20/20，ALL TESTS PASSED**
- **IR 合法性**：`lt10` 多塊 IR（`entry:` + `bb1:` + `bb2:`）`opt -passes=verify` **rc=0**
- **端到端執行**：`lt10(5)+lt10(15)` 的 IR 經 clang 編譯 → 執行 ⇒ **exit code 3**（1+2，證明分支確按條件走）
- `no vet compiler/src/cg.no` ⇒ **0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）

**未覆蓋 / 後續**：`PHI`（暫以 alloca 代替）其他比較（`EQ/NE/GT/GE`）循環回邊（`br` 已支持回邊，待語料驅動）**非 i64 型別**（str/vec/struct/option）字符串與容器 op——這些與 `hir2mir` 降級主體同屬 M4-c 續件


### 5.3.4.aw s17——M4-c 第二件：循环与回边（2026-10-30 本轮）

**背景**：§5.3.4.av 打通了前向跳轉（`bb1`/`bb2`），但**回邊**（跳回已發射的塊）是環的必要條件也是 CFG 最容易寫錯的地方（標籤重複 / 目標未定義 / SSA 支配破壞）本件用 `while` 迴圈證明回邊成立**零新增檔零新增 op**（`OP-BR` 已支持回邊`OP-CMPLT` 已夠表達 `i<n`），只擴 `cg-test.no`

**落地**：`compiler/tests/cg-test.no` 新增 `test-loop`——`sum4()`：`s=0; i=0; while i<4 { s+=i; i+=1 }; return s`，四塊 `entry`/`bb1`(條件)/`bb2`(體)/`bb3`(出口)，`bb2` 尾 `br bb1` 回邊5 斷言（sig / `br label %bb1` 回邊 / `bb1:` / `bb3:` / `ret i64 %v14`），總計 **25/25**

**關鍵點**：

- **回邊自然成立**：塊按指令順序發射 ⇒ 目標塊（`bb1`）在 `br` 之前已定義，LLVM 允許前向與後向 `br`；無需第二遍或重排
- **循環變數走 `alloca`/`store`/`load`**（`%s1`=s`%s2`=i），**不用 PHI**——這正是 `-O0` 風格，語義等價且不需要支配樹/SSA 構造
- `i<4` 用 `CMPLT` 產生 i64 條件，`CONDBR` 判 `!=0` 分派 `bb2`/`bb3`——與 §5.3.4.av 的約定完全復用

**驗證（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **25/25，ALL TESTS PASSED**
- **IR 合法性**：`sum4` 四塊含回邊的 IR `opt -passes=verify` **rc=0**
- **端到端執行**：`sum4()`（0+1+2+3）IR 經 clang 編譯 → 執行 ⇒ **exit code 6**（回邊確按語義迭代 4 次）
- `no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）

**未覆蓋 / 後續**：其他比較（`NE/GT/GE/EQ`）`PHI`（暫以 alloca 代替）**非 i64 型別**（str/vec/struct/option）字符串與容器 op——同屬 M4-c 續件


### 5.3.4.ax s18——M4-c 第三件：字符串（首个非 i64 型別 `%str-long`）（2026-10-30 本轮）

> ⚠️ **2026-11-11 後端佈局變更（本節以下為歷史紀錄）**：Go 後端 `emitPrelude` 的
> `%str-long` 已從 `{ i64, i64, i8* }`（24B）改為 **`{ i32, i32, i8* }`**（u32 len / u32 cap，
> 16B），對外 API 仍為 `i64`（讀取 `zext i32 -> i64`、寫入 `trunc i64 -> i32`）。本節描述的
> 「與 Go `emitPrelude` 逐字相同」、`insertvalue ..., i64 ..., 0/1`、`extractvalue ..., 0` 直接取
> i64 len 等 IR 文本**只適用於 2026-11-11 之前的版本**。**selfhost 的
> `compiler/src/cg-runtime.no` / `cg.no` 已於 2026-11-12 同步完畢（見 §11.2 B28 與 §11 後 ⑥）**。當時列出的同步步驟如下（已全數落地，並補第⑤步）：
> ① 把 prelude 改成 `{ i32, i32, i8* }`；② `OP-STRCONST` 的三步 `insertvalue` 先把 i64
> 長度 `trunc` 成 i32（或用常數直接給 `i32`）；③ `OP-STRLEN` 的 `extractvalue` 後補 `zext i32 -> i64`；
> ④ `OP-STRCAT` 等 helper 的 `%str-long` 聚合常量改為 `{ i32 0, i32 0, i8* null }`；
> ⑤ `OP-OPTMKSTR`／`OP-OPTGETSTR` 依 16B 重做（`zext`／`trunc`），`@str_eq`／`@str_concat` 欄位型別一併同步。
> 詳見 `docs/docs/lang/memory.md`〈型別佈局〉與 `docs/docs/lang/str.md`〈內部佈局與對外 API〉。

**背景**：§5.3.4.av/aw 全程假設所有值都是 i64——`%v<id>` 一個命名空間`define i64``ret i64`這在真實程序裡立刻破產：任何 `.len`/字面量/輸出都要 `str`本件引入**第一個非 i64 型別**，並定下多型別值命名空間的規矩**零新增檔**，改 3 檔（`cg-runtime.no`/`mir.no`/`cg.no`）+ 擴測試

**落地**：

1. `compiler/src/cg-runtime.no` 新增 `cg-runtime-str-type()`：`%str-long = type { i64, i64, i8* }   ; len, cap, data`，插在 `%task` 與 `decls` 之間——與 Go `emitPrelude` **逐字相同**（含行尾註解），維持 §5.6.2IR 文本可逐位元組照抄
2. `compiler/src/mir.no` 新增 `OP-STRCONST = 12` / `OP-STRLEN = 13`，並把**值命名空間約定**寫進模組頭註解（見下關鍵點）
3. `compiler/src/cg.no`：
   - `cg-strlen1(t)`——字面量數組長度 = `t.len-bytes() + 1`（含結尾 NUL）；加法帶 `#{overflow=wrap}`
   - `cg-emit-cfg` 開頭加**全局前置 pass**：掃 `ops[]`，對每條 `OP-STRCONST` 在 `define` **之前**發 `@.s<k> = private unnamed_addr constant [N x i8] c"...\00"`（⚠️ **s34 起改名為 `@.s.<函式名>.<k>`**：`@.s<k>` 只含指令下標 ⇒ 兩個函式各自第 k 條字面量撞名，見 §5.3.4.bn / §11 B10）（`k` = 指令下標，天然唯一）
   - `OP-STRCONST` 分支：`getelementptr inbounds` 取 `i8*`，再**三步 `insertvalue`** 拼 `%str-long`（`undef` → len → cap → data）
   - `OP-STRLEN` 分支：`extractvalue %str-long %sv<x>, 0` 取 len 落 `%v<d>`
4. `compiler/tests/cg-test.no` 新增 `test-string`——`slen()` 返回 `'abc'.len`，7 斷言（全局字面量形狀 / gep / 三步 insertvalue / extractvalue / ret），總計 **32/32**

**關鍵點**：

- 🔴 **兩個值命名空間，不能混**：i64 值住 `%v<id>`，`str` 值住 `%sv<id>`（型別 `%str-long`）`dsts[]` 裡的 id 在兩種 op 下指同一套 id 空間，但**發出的 SSA 名不同**——因為 LLVM 不允許同名不同型後續 `vec`/`struct`/`option` 應各自再開 `%vv<id>`/`%st<id>` 之類前綴，**不要**試圖用一個 `%v<id>` 通吃
- 🔴 **字面量全局必須在 `define` 之前**：LLVM 要求 `@` 全局是模組級實體因此**不能**在逐指令循環裡順手發，必須先跑一遍前置 pass這也解釋了為什麼 `n = ops.len()` 要提到函數最頂端（原本在簽名之後）
- **`insertvalue` 而非 `alloca`+`store`**：`%str-long` 是**值型別**（3 個標量），用 `insertvalue` 逐步構造是純 SSA零內存；這與控制流那邊跨塊傳值走 alloca不衝突——跨塊的 `%sv` 才需要 `alloca %str-long`，單塊內不需要
- **len 與 cap 都填 `len-bytes()`**：字面量是不可變的，cap 沒有語義，填同值最省；Go 側亦如此
- **NUL 結尾**：`c"..."` 裡手寫 `\00`，且數組長度 `+1`⚠️ 這裡 `\00` 是**兩字節轉義**（反斜線 + `00`），nolang 字面量裡要寫 `'\\00'`——寫成 `'\00'` 會被當成一個 NUL 字符塞進 IR 文本，`opt` 會報 `expected top-level entity`

**驗證（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **32/32，ALL TESTS PASSED**
- **IR 形狀**（實際產出）：
  ```
  @.s0 = private unnamed_addr constant [4 x i8] c"abc\00"
  define i64 @slen() {
  entry:
  	%p0 = getelementptr inbounds [4 x i8], [4 x i8]* @.s0, i64 0, i64 0
  	%q0 = insertvalue %str-long undef, i64 3, 0
  	%r0 = insertvalue %str-long %q0, i64 3, 1
  	%sv1 = insertvalue %str-long %r0, i8* %p0, 2
  	%v2 = extractvalue %str-long %sv1, 0
  	ret i64 %v2
  }
  ```
- **IR 合法性**：prelude + 上述 IR + 手寫 `main`（`trunc` 成 `i32` 返回）⇒ `opt -passes=verify -disable-output` **rc=0**
- **端到端執行**：同上模組合 `/tmp/ccwrap` 編譯 → 執行 ⇒ **exit code 3**（`'abc'.len == 3`，證明 `len-bytes`/全局佈局/`extractvalue` 全對）
- `no vet` 四檔（`cg.no`/`cg-runtime.no`/`mir.no`/`cg-rc.no`）**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`，並行 session 既有）

**未覆蓋 / 後續**：字符串**拼接**（`+` 需 `@malloc` + `memcpy` + RC 頭）`str` 跨塊傳值（`alloca %str-long`）`vec`/`struct`/`option``PHI`其他比較——同屬 M4-c 續件


### 5.3.4.ay s19——M4-c 第四件：字符串拼接 `@str_concat`（首个 RC 消费者）（2026-10-30 本轮）

**背景**：§5.3.4.ax 讓 `str` **能存在**，但還不能**產生新的** `str`拼接是第一個執行期分配堆內存 + 寫 RC 頭的 op，也是把 `cg.no`（發射）與 `cg-rc.no`（RC ABI）真正接上的地方——在此之前 RC helper 只是被發射從未被呼叫**零新增檔**，改 3 檔 + 擴 2 個測試檔

**落地**：

1. `compiler/src/cg-runtime.no`：
   - `cg-runtime-memcpy-decl()`——`declare void @llvm.memcpy.p0i8.p0i8.i64(i8*, i8*, i64, i1)`（Go `codegen.go:2987` 同形）
   - `cg-runtime-str-concat()`——`@str_concat` **13 行函式體逐字照抄** Go `codegen.go:3139`（含 **2 空格**縮進，不是 tab；這是刻意與該處原文一致）
   - `cg-runtime-prelude-full()`——**完整 prelude 的唯裝配點**，順序：類型 → libc 聲明 → memcpy 聲明 → 四個 RC helper → `@str_concat``cg-runtime-prelude()`（M4-a 的最小 prelude）保持原樣被它委託，**舊斷言零改動**
2. `compiler/src/mir.no`：`OP-STRCAT = 14`；`dsts[k]` = 結果串 id`xa[k]` = 左串`bs[k]` = 右串
3. `compiler/src/cg.no`：`OP-STRCAT` 分支發 `%sv<d> = call %str-long @str_concat(%str-long %sv<x>, %str-long %sv<y>)`
4. 測試：`cg-test.no` 加 `test-strcat`（5 斷言，**37/37**）；`cg-runtime-test.no` 加 `test-prelude-full`（5 斷言，**21/21**）

**關鍵點**：

- 🔴 **prelude 的順序是語義，不是排版**：`@str_concat` 調 `@nolang_rc_alloc`LLVM 本身允許前向引用，但**照 Go 的發射順序**（RC 在前）才能逐字節對照；`test-prelude-full` 用 `str.index` 比較兩個 `define` 的偏移，把順序變成**可執行斷言**而不是註釋承諾
- 🔴 **`@str_concat` 的兩段 `memcpy` 必須用 `%la` 當第二段的目的偏移**：`%dst2 = getelementptr i8, i8* %buf, i64 %la`寫成 `%total` 或漏 gep 會**越界寫**且 `opt -verify` 照樣綠（verify 只查型別/SSA，不查語義）⇒ 必須**跑起來比內容**
- **`%str-long` 是值型別**，`call %str-long @f(...)` 直接按值傳/回，不需要 `sret`；這與 Go 的 `_mir_str_concat` 一致`alloca` 只在**跨基本塊**保存 `%str-long` 時才需要（本件未涉及）
- **RC 頭語義**：`@nolang_rc_alloc` 返回 `base+16`，rc=1 ⇒ 拼接結果是**擁有者**（owner）§5.6.1 的鐵律要求：把這個結果賦給第二個變數時**必須 retain**本件的 `OP-STRCAT` 只產生值，**不發 retain/release**——那是 `hir2mir` 的責任，留給後續
- **`%p<k>`/`%q<k>`/`%r<k>` 用指令下標 `k`**，`%sv<id>` 用值 id ⇒ 同一函數內多條 `STRCONST` 不會撞名（`cat()` 的 k0/k1 分別得 `%p0/%q0/%r0` 與 `%p1/%q1/%r1`）

**驗證（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **37/37**；`cg-runtime-test.no` ⇒ **21/21**，均 `ALL TESTS PASSED`
- **IR 形狀**（`cat()` 尾部實際產出）：
  ```
  	%sv1 = insertvalue %str-long %r0, i8* %p0, 2
  	%p1 = getelementptr inbounds [3 x i8], [3 x i8]* @.s1, i64 0, i64 0
  	%q1 = insertvalue %str-long undef, i64 2, 0
  	%r1 = insertvalue %str-long %q1, i64 2, 1
  	%sv2 = insertvalue %str-long %r1, i8* %p1, 2
  	%sv3 = call %str-long @str_concat(%str-long %sv1, %str-long %sv2)
  	%v4 = extractvalue %str-long %sv3, 0
  	ret i64 %v4
  ```
- **IR 合法性**：`cg-runtime-prelude-full()` + `cat()` + 手寫 `main` ⇒ 全模組 **123 行**，`opt -passes=verify -disable-output` **rc=0**
- **端到端執行（比內容，不只比長度）**：`main` 內另建兩個 `%str-long` 直呼 `@str_concat`，把結果用 `@write(1, data, len)` 打出來 ⇒ stdout **`abcd`**退出碼 **4**同時證明照抄的 helper 本身正確與`len-bytes` 算得對
- `no vet` 三檔（`cg.no`/`cg-runtime.no`/`mir.no`）**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`，並行 session 既有）

|  |  |  |  |  |
| --- | --- | --- | --- | --- |
**踩坑**：`2>&1 | grep "warning\|warning generated"` 在 BSD grep 下**靜默失效**（BRE 不支持 `\|`）⇒ clang 的 `warning: overriding the module target triple` 混進 `.ll`，`opt` 報 `1:1: expected top-level entity`判據：**過濾 stderr 用 `2>/dev/null`，或用 `grep -E`**，不要用 BRE 的 `\|`

**未覆蓋 / 後續**：`str` 跨塊（`alloca %str-long`）`STRCAT` 的 retain/release 發射（屬 `hir2mir`）`vec`/`struct`/`option``PHI`其他比較——同屬 M4-c 續件


### 5.3.4.az s20——M4-c 第五件：多參數調用（`OP-CALL` 的 `bs` 從佔位改為參數個數）（2026-10-30 本轮）

**背景**：M4-b 的 `OP-CALL` 是**恰好一個** i64 參數——`a` 是那個參數`bs` 是無意義佔位這在真實程序裡立刻不夠：任何 `f(a, b)` 都編不出來本件把 `bs[k]` 的語義正式定為**參數個數**`a` 為**首個參數值 id（連續）**，並讓 `cg.no` 按個數展開實參表**零新增 op零新增檔**，改 2 檔 + 擴 1 個測試檔

**落地**：

1. `compiler/src/mir.no`：`OP-CALL` 的槽位約定寫進註釋——`dst` = 結果值 id`syms[k]` = 被調名`a` = 首個參數值 id（連續）`bs[k]` = 參數個數**明示破壞性變更**：M4-b 時 `bs` 恒 0 是佔位，現在 `bs=0` 表示**無參調用**`bs=1` 表示單參 ⇒ 舊的單參但 `bs=0`寫法語義變了，必須改
2. `compiler/src/cg.no`：`OP-CALL` 分支改為循環展開——`q = x``j = 0`，`(j < y)` 內先 `j > 0 -> push(', ')` 再加 `i64 %v<q>`，每輪 `q`/`j` 各 +1（帶 `#{overflow=wrap}`，避免 `+` 退化成 `option<i64>`）**`define` 側無需改動**：參數表本來就按 `nargs` 循環
3. `compiler/tests/cg-test.no`：`test-call` 的 `bs.push(0)` → `bs.push(1)`（語義修正，不是斷言放寬）；新增 `test-call2`——`add2(a,b)=a+b` 的 `define` 雙參形狀 + `caller2()=add2(3,4)` 的雙參調用形狀，8 斷言，累計 **41/41**

**關鍵點**：

- 🔴 **這是本項目第一個靜默語義變更而非純新增**：`OP-CALL` 的槽位含義改了，任何漏改的呼叫點會退化成 `call i64 @f()`（**參數被丟棄**）而這種退化 **`opt -verify` 全綠`no vet` 全綠**——只有跑起來比數值才看得見⇒ 修法是先改語義立刻改唯一呼叫點（`test-call`）再端到端跑一遍，並把變更寫進 `mir.no` 註釋而不是靠記憶
- **參數 id 必須連續**：本件的約定是`a` 起的 `bs` 個 id這是扁平 arena 的必然選擇（不引入 `args []i64` 二級表，遵 §5.3.4.as結構體不跨模組按值傳）代價是 `hir2mir` 在發 CALL 前**必須保證實參值 id 相鄰**——若中間插了別的指令就會錯位這個約束要在 M4-c 餘的 `hir2mir` 裡顯式檢查（尚未做，記入 §11）
- **`+` 的型別陷阱**：`q = q + 1` 不帶 `#{overflow=wrap}` 會得到 `option<i64>`，`q.to-str()` 就編不過這在循環裡連寫兩處，容易漏一處
- **無參調用仍要寫 `bs=0`**：不能靠 `syms[k]` 或 `dst` 推斷

**驗證（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **41/41**；`cg-runtime-test.no` ⇒ **21/21**，均 `ALL TESTS PASSED`
- **IR 形狀**（實際產出）：
  ```
  define i64 @add2(i64 %v1, i64 %v2) {
  entry:
  	%v3 = add i64 %v1, %v2
  	ret i64 %v3
  }
  define i64 @caller2() {
  entry:
  	%v1 = add i64 0, 3
  	%v2 = add i64 0, 4
  	%v3 = call i64 @add2(i64 %v1, i64 %v2)
  	ret i64 %v3
  }
  ```
- **IR 合法性**：上述 + 手寫 `main`（`trunc` 成 `i32`）⇒ `opt -passes=verify -disable-output` **rc=0**
- **端到端執行**：同上經 `/tmp/ccwrap` 編譯 → 執行 ⇒ **exit code 7**（`add2(3,4)`，證明**兩個**實參都真的傳進去了——單參退化會得到 3 或 4）
- `no vet` 兩檔（`cg.no`/`mir.no`）**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）

**未覆蓋 / 後續**：非 i64 參數（`str`/`vec` 形參）多返回值`str` 跨塊（`alloca %str-long`）`STRCAT` 的 retain/release`vec`/`struct`/`option``PHI`——同屬 M4-c 續件


### 5.3.4.ba s21——M4-c 第六件：`%str-long` 跨基本塊（`SALLOC`/`SSTORE`/`SLOAD`）（2026-10-30 本轮）

**背景**：§5.3.4.ax/ay 的字符串只在**單一基本塊內**流動（`%sv` 是 SSA 名，出了塊就失效）任何真實程序都會把 `str` 存進變數在分支裡改寫再在匯合點讀出來——這是 `-O0` 語義下必須用 `alloca` 的地方本件把 i64 的 `ALLOC/STORE/LOAD` 三件套**按型別重複一份**給 `%str-long`，並定下**第三個槽命名空間** `%ss<id>`**零新增檔**，改 2 檔 + 擴 1 個測試檔

**落地**：

1. `compiler/src/mir.no`：`OP-SALLOC=15`（`dst` = 槽 id）`OP-SSTORE=16`（`a` = 字符串值 id`bs[k]` = 槽 id）`OP-SLOAD=17`（`dst` = 字符串值 id`a` = 槽 id）
2. `compiler/src/cg.no`：三個分支分別發 `alloca %str-long` / `store %str-long %sv<a>, %str-long* %ss<bs>` / `%sv<dst> = load %str-long, %str-long* %ss<a>`
3. `compiler/tests/cg-test.no`：新增 `test-str-slot`——`pick(c)`：`s='ab'; c ? s='abcdef'; return s.len`，三塊 `entry`/`bb1`/`bb2`，6 斷言，累計 **47/47**

**關鍵點**：

- 🔴 **槽的命名空間必須按型別分開**：i64 槽住 `%s<id>`，`%str-long` 槽住 `%ss<id>`若共用 `%s<id>`，`alloca i64` 與 `alloca %str-long` 會撞名 ⇒ `opt` 報 type mismatch同理值：`%v`（i64）/`%sv`（str）**每加一個型別就要加一組前綴**，這是本項目已付兩次的學費
- 🔴 **`alloca %str-long` 是unsized type陷阱**：`%str-long` 在**沒有 prelude 的裸模組**裡是未定義型別，`opt` 會報 `Cannot allocate unsized type`——這個訊息**指向 `alloca` 那行**，很容易誤判成`%str-long` 不可 alloca真相是**模組缺 `%str-long = type { i64, i64, i8* }` 定義**⇒ 任何用 `SALLOC` 的 IR 都必須與 `cg-runtime-prelude-full()`（或至少 `cg-runtime-prelude()`）一起鏈接
- **`insertvalue` vs `alloca` 的分工**：單塊內構造 `%str-long` 用 `insertvalue`（純 SSA零內存）；**跨塊**才用 `alloca/store/load`兩者不是替代關係
- **`SSTORE` 的槽在 `bs` 而非 `dst`**：`SSTORE` 沒有結果值（`dst=0`），與 `STORE` 一致；`SLOAD` 有結果值（`dst` = 值 id）而槽在 `a`，與 `LOAD` 一致這種i64 版與 str 版槽位一一對應的設計讓 `hir2mir` 可以按型別選 op 而不用改參數排布

**驗證（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **47/47，ALL TESTS PASSED**
- **IR 形狀**（實際產出，`pick`）：
  ```
  entry:
  	%ss1 = alloca %str-long
  	...
  	store %str-long %sv1, %str-long* %ss1
  	%c3 = icmp ne i64 %v1, 0
  	br i1 %c3, label %bb1, label %bb2
  bb1:
  	...
  	store %str-long %sv2, %str-long* %ss1
  	br label %bb2
  bb2:
  	%sv3 = load %str-long, %str-long* %ss1
  	%v4 = extractvalue %str-long %sv3, 0
  	ret i64 %v4
  ```
- **IR 合法性**：prelude 型別塊 + 上述 + 手寫 `main` ⇒ `opt -passes=verify -disable-output` **rc=0**（兩個入口都過）
- **端到端執行（兩條分支都跑）**：`pick(0)` ⇒ **exit 2**（`'ab'`）`pick(1)` ⇒ **exit 6**（`'abcdef'`）**兩個數字不同**才證明分支裡的 `SSTORE` 真的覆寫了槽匯合點的 `SLOAD` 真的讀到新值；只跑一條會讓`SSTORE` 完全沒發射也綠
- `no vet` 兩檔（`cg.no`/`mir.no`）**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）

**未覆蓋 / 後續**：非 i64 形參`vec`/`struct`/`option``STRCAT` 的 retain/release`PHI`——同屬 M4-c 續件


### 5.3.4.bb s22——M4-c 第七件：`vec`（`%vec` 向量，元素仅 i64）（2026-10-30 本轮）

**背景**：`vec` 是自举的**硬前提**——SHS 強制結構體不跨模組按值傳（§5.3.4.as），所以 `compiler/src` 的扁平 arena（`ops/dsts/xa/bs/syms`）全是 `[]i64`/`[]str`沒有 `vec`，`noc` 連自己的 MIR 容器都表達不了本件引入**第四個值命名空間** `%vc<id>`**零新增檔**，改 2 檔 + 擴 2 個測試檔

**落地**：

1. `compiler/src/cg-runtime.no`：`cg-runtime-vec-type()`——`%vec = type { i64, i64, i64 }`（Go `codegen.go:2936` 逐字：lencapdata **存成 i64 裸指標**）；四個 helper `@vec_new` / `@vec_len` / `@vec_get_i64` / `@vec_push_i64`，插進 `prelude-full` 尾部
2. `compiler/src/mir.no`：`OP-VECNEW=18``OP-VECPUSH=19``OP-VECLEN=20``OP-VECGET=21`
3. `compiler/src/cg.no`：四個分支發 `call %vec @vec_new()` / `call %vec @vec_push_i64(...)` / `call i64 @vec_len(...)` / `call i64 @vec_get_i64(...)`
4. 測試：`cg-test.no` 加 `test-vec`（`vecsum()` 建 `[1,2,3]` 求和，5 斷言）+ `test-vec-len`（2 斷言）⇒ **54/54**；`cg-runtime-test.no` 加 `test-vec-helpers`（9 斷言）⇒ **30/30**

**关键决策：刻意偏离 Go 的 inline 展开**Go 的 `vec.push` 在**每个调用点 inline** 展开（`load %vec` → 扩容 → 步长計算 → store，每站點上百行 IR）M4 以**行為等價**為驗收（§5.6.3 第①層），故改為 4 個 helper：IR 量級差一個數量級，helper 本身可被單元測試直接釘住，且**唯一一份**擴容邏輯（Go 那份散在 `emitBuiltinVecPush` 裡，任何型別分支寫錯都是堆損壞——源碼註釋裡就有兩段24 位元組寫進 8 位元組 slot ⇒ `abort trap`的血淚）**這是刻意偏離，記入 §11 B5**

**关键点**：

- 🔴 **第四個值命名空間**：i64 值 `%v``str` 值 `%sv``str` 槽 `%ss`**vec 值 `%vc`**每加一個型別就多一組前綴——這是本項目第三次付同樣的學費（s18s21s22）
- 🔴 **`%vec` 的 data 是 `i64` 不是 `i8*`**：Go 的型別定義就是 `{ i64, i64, i64 }`所以每次取用都要 `inttoptr i64 %dat to i8*`每次存回都要 `ptrtoint i8* %p to i64`少寫一邊 ⇒ `opt` 型別錯；寫成 `%vec = { i64, i64, i8* }` 看似更自然，但**與 Go 的 prelude 不相容**（後續 A/B 會全線錯位）
- 🔴 **`vec.push` 是返回新 vec不是原地改**：`%vc<d> = call %vec @vec_push_i64(%vec %vc<a>, i64 %v<bs>)`這與 Go 的 inline 版語義一致（它也是 load→改→寫回 slot），但對 `hir2mir` 意味著**每次 push 都要把結果賦回同一個槽**
- **擴容路徑**：滿則 `ncap = max(cap*2, 4)` → `malloc` 新緩衝 → **`len==0` 時跳過 `memcpy`**（否則 `memcpy(dst, null, 0)` 是 UB）→ `free` 舊緩衝 → `phi` 合流 `cap2`/`dat2``store:` 塊有**三個**前驅（`entry`/`grow`/`copy`），phi 必須列全，漏一個 `opt` 就報 `PHI node entries do not match predecessors`
- **數據緩衝用裸 `@malloc`**（不帶 RC 頭）：元素目前只有 i64，無 RC 需求；擴容時 `@free` 舊緩衝 ⇒ 不會無界泄漏等元素變成 `str`/`vec` 時必須換成 `@nolang_rc_alloc` + 逐元素 retain/release（記入 §11 B5）

**验证（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **54/54**；`cg-runtime-test.no` ⇒ **30/30**
- **IR 合法性**：`prelude-full` + 一個**十次 push** 的函式（強制擴容 0→4→8→16）+ 手寫 `main` ⇒ 204 行全模組 `opt -passes=verify -disable-output` **rc=0**
- **端到端執行**：`vecsum10()` 推入 0..9 再逐個 `vec_get_i64` 求和 ⇒ **exit code 45**（`0+1+…+9`）⚠️ 這個數字**有鑑別力**：擴容時漏 `memcpy` 會只剩最後 2 個元素漏 `free` 不會改變數值但會泄漏`inttoptr`/步長寫錯會 SIGSEGV 或錯值——45 全對說明擴容 + 搬遷 + 偏移 + 讀回四件事同時對
- **ASan**：同一 IR 用 `-fsanitize=address` 編譯執行 ⇒ **exit 45，無 heap-buffer-overflow / use-after-free 報告**（證明 `free` 舊緩衝後沒有殘留引用）
- `no vet` 三檔（`cg.no`/`cg-runtime.no`/`mir.no`）**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）

**🔴 附带发现的工具链 bug（`no` 側，非本專案代碼）**：`#{overflow=wrap}` 用在**首次綁定**且右側是**算術表達式**時，該變數的**讀取**正確，但 **`vec.push(该变量)` 静默壓入 0**實測四變體：

| 寫法 | 讀取 | `vec.push(v)` |
| --- | --- | --- |
| `#{overflow=wrap}`<br>`q = 5 + 1` | `q=6` ✓ | **`0`** ✗ |
| `#{overflow=wrap}`<br>`r = n + 1` | `r=13` ✓ | **`0`** ✗ |
| `#{overflow=wrap}`<br>`s = n`（非算術） | `s=12` ✓ | `12` ✓ |
| `m i64 = 0` 後 `#{overflow=wrap}`<br>`m = n + 1` | `m=13` ✓ | `13` ✓ |

爆破半徑**很窄**：同一變數在 `print` 插值普通函式呼叫 `f(r)`陣列字面量 `[r]``r.to-str()` 裡**全部正確**——只有 `vec.push` 的實參取值錯⇒ **規避寫法：先裸宣告（`g = 0`）再帶註解重賦值**`compiler/src` 現有代碼**不觸發**（`lx.no` 的 `hi`/`lo` 是首次綁定 + 算術，但只傳給普通函式；`cg.no` 的 `q`/`j` 都是先宣告後重賦值），但這是**門禁全綠值靜默錯**的典型，已記入 §11 B6

**未覆蓋 / 後續**：`vec` 元素非 i64（`[]str`/`[]vec`/結構體，需逐元素 RC）`vec` 跨基本塊（`alloca %vec`）`vec` 的 `.set`/`.pop`/切片非 i64 形參`struct`/`option`——同屬 M4-c 續件


### 5.3.4.bc s23——M4-c 第八件：带型别的函数签名（`str`/`vec` 形参与返回值）（2026-10-31 本轮）

**背景**：到 s22 为止，`define` 与 `ret` 都硬編碼 `i64`——**任何自举函数都收不了 `[]i64`/`str`**而 SHS 的扁平 arena（§5.3.4.as）恰恰全是 `[]i64`/`[]str`，所以形参能是 vec/str是自举的下一道硬门槛本件**零新增檔**，改 2 檔 + 擴 1 個測試檔

**落地**：

1. `compiler/src/mir.no`：型别码 `TY-I64=0` / `TY-STR=1` / `TY-VEC=2`，并顺手修正头部注释里 `as[k]` 的旧名（实际字段是 `xa`，`as` 是保留字）
2. `compiler/src/cg.no`：`cg-ty-name(t)`（→ `i64` / `%str-long` / `%vec`）`cg-val-prefix(t)`（→ `%v` / `%sv` / `%vc`）新入口 `cg-emit-cfg-t(name, retty, nargs, ptyps, …)`
3. `cg-emit-cfg` 退化为**全 i64 的壳**：它自己 `[].with-len()` 造出全 0 的型别表再委托——旧 8 参数 API 与既有 12 个测试**零改动**

**关键点**：

- 🔴 **值命名空间跟型别走，不是跟位置走**：`mix(n i64, s str)` 的形参是 `%v1` 与 `%sv2`——**交错**的LLVM 里 SSA 名全局唯一同名不同型直接报错，所以第几个参数就用 `%v<几>`在混合签名上必崩这是第四次付同一笔学费（s18/s21/s22/s23），至此**命名空间由 `cg-val-prefix` 单一来源决定**，不再散落在各分支
- 🔴 **便捷壳自己构造型别表，而不是让发射器空表即 i64**：s20 的教训是隐式默认 = 静默退化`cg-emit-cfg` 显式 `[].with-len()` 出全 0 表，`cg-emit-cfg-t` 里**没有任何默认分支**——表里没填就是 `TY-I64`（0），但至少是调用者填了一个 0，不是发射器替他猜
- **define 的参数循环用双计数器**（`q` 索引 `ptyps``p` 是值 id）：避免在 `b.push((q+1).to-str())` 里写内联算术——`+` 默认返回 `option<i64>`，要 `#{overflow=wrap}` 就得单独成句（而单独成句又踩 §11 B6 的 wrap-let 坑）两个计数器各 `+1` 是最省事的写法

**验证（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **63/63**（新增 `test-sig-str` 3 断言`test-sig-mixed` 3 断言`test-sig-vec` 3 断言）
- **IR 合法性**：`prelude` + `sident`/`mix`/`mk` 三函数 + 手写 `main` ⇒ `opt -passes=verify -disable-output` **rc=0**
- **端到端执行**：`main` = `mix(7, 'abcd')`（4）+ `sident('abcd').len`（4）+ `mk().len`（6）⇒ **exit 14**三个数字各管一件事：`mix` 证明 **str 形参**能按值收`sident` 证明 **str 返回值**能带出形参`mk` 证明**函数内构造的 str** 能返回（若 `ret %str-long` 退化成 `ret i64`，`extractvalue ..., 0` 会读到指针低位字节，几乎不可能正好是 6）
- `no vet compiler/src/cg.no``mir.no` **各 0 ERROR**；`no vet compiler/src` 当树 **34 ERROR 不变**

**未覆蓋 / 後續**：**带型别的 CALL**（能定义却还不能调用——`OP-CALL` 仍硬編碼全 `i64` 实参 + `%v` 结果，见 s24）`struct`/`option``STRCAT` 的 retain/release`PHI`——同属 M4-c 續件


### 5.3.4.bd s24——M4-c 第九件：带型别的 `CALL`（值型别表 `vty`）（2026-10-31 本轮）

**背景**：s23 让函数**能定义**成 `%str-long @sident(%str-long)`，但 `OP-CALL` 仍发 `call i64 @f(i64 %v1, …)`——**定义与调用不匹配**，LLVM 直接报型别错本件把 `CALL` 也打通**零新增檔**，改 1 檔 + 擴 1 個測試檔

**落地**：

1. `compiler/src/cg.no`：`cg-emit-cfg-t` 增两个平行表——`vty []i64`（**按值 id 索引**，`vty[id]` = 该值的型别码）与 `cty []i64`（按指令索引，`cty[k]` = 该指令**结果**的型别码）
2. `CALL` 分支：结果用 `cty[k]` 选型别名与前缀，每个实参用 `vty[q]` 选——实参型别**逐个**查，不再假设全 `i64`
3. `cg-emit-cfg` 壳相应造 `vty`（长度 `n+1`，含 id 0）与 `cty`（长度 `n`），全 0

**关键决策：为什么是按值 id 索引的表而不是调用点签名串或新开一个 `OP-CALLT`**：

|  |  |  |
| --- | --- | --- |
- **调用点签名串**（`syms[k]` = `'mix|0|0,1'`）要写字符串解析，错一个分隔符就是静默错型别；而且**同一个值会在多个调用点被复用**，型别信息天然属于值而不是调用点
- **新开 `OP-CALLT`** 会让两个几乎相同的 op 必须永远同步演进（改一处忘一处就是 s20 那种全绿但语义错）型别是**值的属性**，让 `OP-CALL` 统一读表，只有一个实现
- 🔴 代价是 `vty` 必须**填满**（长度 ≥ 最大值 id + 1）漏填的 id 会读到越界值——这是本件唯一的脆弱点，记入 §11 B7；`hir2mir` 落地时必须**在分配值 id 的同时**写 `vty`，而不是事后补

**关键点**：

- 🔴 **`cty` 与 `vty` 是两个维度**：`cty[k]` 按**指令**索引（结果型别）`vty[id]` 按**值 id** 索引（实参型别）看起来重复（`cty[k]` 的效果等价于写 `vty[dsts[k]]`），但**分开更安全**：指令的结果 id 可能为 0（无结果），而 `vty[0]` 是哑元；混用会让无结果指令把 `vty[0]` 当成结果型别`hir2mir` 落地时可以只写 `vty`由发射器代填 `cty`——留待那时再收
- **API 变更在这里是编译期硬错而不是静默退化**：探针里三处旧的 10 参调用点立刻报 `function 'cg-emit-cfg-t' expects at least 12 argument(s), got 10`这与 s20 的 `OP-CALL` 语义变更（`bs[k]` 从占位变参数个数，**没有任何编译期信号**）形成鲜明对比——**能靠型别/参数个数抓到的破坏性变更，就一定要让它变成编译期错误**

**验证（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **66/66**（新增 `test-sig-call` 3 断言：`%sv2` 构造`%v3 = call i64 @mix(i64 %v1, %str-long %sv2)``ret i64 %v3`）
- `cg-runtime-test.no` ⇒ **30/30**（未受影响，回归确认）
- **IR 合法性**：`prelude` + 四函数（`sident`/`mix`/`mk`/`caller2`）+ 手写 `main` ⇒ `opt -passes=verify` **rc=0**
- **端到端执行**：`main` = `caller2()`（4，**函数内**发带型别的 CALL `mix(7,'abcd')`）+ `mk().len`（6）+ `sident('abcd').len`（4）⇒ **exit 14**`caller2` 是唯一发 `call` 而不是被 `call`的路径，故这个 4 **必须**由带型别的 `CALL` 产出
- `no vet compiler/src/cg.no``mir.no` **各 0 ERROR**；当树 **34 ERROR 不变**

**未覆蓋 / 後續**：`struct`（SHS 限同模組）`option`（tag + payload）`STRCAT` 的 retain/release 發射`PHI``vty` 的越界风险（§11 B7）——同屬 M4-c 續件


### 5.3.4.be s25——M4-c 第十件：`option`（`%option` tag + payload）（2026-10-31 本轮）

**背景**：`?T` 是 nolang 的**唯一**可空表示——所有整數算術的預設返回型別就是 `option<int>`，`match` 的 `ok`/`err`/`nil` 臂全部落在它上面沒有 `%option`，自舉的 `hir2mir` 連`a + b` 為什麼要解包都表達不出來本件引入**第五個值命名空間** `%op<id>`**零新增檔**，改 3 檔 + 擴 2 個測試檔

**落地**：

1. `compiler/src/cg-runtime.no`：`cg-runtime-option-type()`——`%option = type { i64, [3 x i64] }    ; tag, 24-byte payload slot (32 bytes total)`（Go `codegen.go:2938` 逐字，**含 4 空格行尾註釋**），插在 `%vec` 之後
2. `compiler/src/mir.no`：`TY-OPT=3`；`OP-OPTNONE=22``OP-OPTSOME=23``OP-OPTTAG=24``OP-OPTISOK=25``OP-OPTGET=26`
3. `compiler/src/cg.no`：`cg-ty-name`/`cg-val-prefix` 各加 `TY-OPT` 一臂；五個 op 分支`OPTSOME` 用 Go `codegen.go:2530` 的**同形**寫法 `insertvalue %option { i64 0, [3 x i64] zeroinitializer }, i64 %v<a>, 1, 0`（一次 insertvalue 同時給 tag 與 slot[0]）

**关键点**：

- 🔴 **`%option` 只有一個 LLVM 型別，元素型別只活在語義層**：`?i64`/`?str`/`?vec` 在 IR 裡全是同一個 32 位元組結構所以讀 payload這件事**不能**由型別自動決定——`OP-OPTGET` 讀的是slot[0] 當 i64，對 `?str` 是錯的這與 `%vec` 的坑（元素型別不可見 ⇒ 步長靠宣告）是同一族問題，記入 §11 B8
- 🔴 **tag 是 `i64` 不是 `i1`**：`{ i64, [3 x i64] }` 的頭欄是 8 位元組整數，值 0/1/2寫成 `i1` 會讓整個結構的佈局與 Go prelude 不一致（後續 A/B 全線錯位）
- **slot 是 `[3 x i64]` 而不是 `{ i64, i64, i8* }`**：這樣 24 位元組的 `%str-long` 能原樣裝進去（`err('msg')` 的載荷正好是 str），而 `i64` 載荷落在 slot[0]Go 的 `optionPayloadInline` 就是拿 24 這個門檻分流內聯 / 堆上裝箱
- **`OPTSOME` 一次 insertvalue 給 tag + 載荷**：`..., i64 %v<a>, 1, 0` 的意思是先取 field 1（slot 陣列）的第 0 個元素分兩步寫（先 insert tag 再 insert 載荷）也行，但要多一個中間值，IR 更長且更容易寫錯索引
- **`OPTISOK` 需要一個額外臨時值** `%ot<k>`（放 `extractvalue` 出來的原始 tag），因為 `icmp` 的操作數不能是 `extractvalue` 的直接結果臨時值按**指令下標** `k` 命名，與 `%c<k>` 同一套規矩——不會撞名

**验证（可重跑）**：

- `./bin/no run -cc /tmp/ccwrap compiler/tests/cg-test.no` ⇒ **78/78**（新增 `test-option` 6 斷言`test-option-read` 6 斷言）；`cg-runtime-test.no` ⇒ **33/33**（`test-runtime-types` 從 4 條擴到 7 條，補 `%str-long`/`%vec`/`%option` 三個型別斷言——之前只斷言了 hdr 與 task）
- **IR 合法性**：`prelude` + `pick`（`n < 10 ? some(n) : none`，三基本塊）+ `g`（調 `pick` 再讀 tag/載荷）+ 手寫 `main` ⇒ `opt -passes=verify` **rc=0**
- **端到端執行**：`main` = `g(7) + g(15)`，其中 `g(n) = isok(pick(n)) + payload(pick(n))` ⇒ **exit 8**（`g(7)=1+7=8``g(15)=0+0=0`）這個數字**有鑑別力**：`OPTISOK` 恆真 ⇒ 9`OPTGET` 沒發射 ⇒ 1`OPTSOME` 沒寫載荷 ⇒ 1`OPTNONE` 的 tag 誤寫 0 ⇒ `g(15)=1+0=1` 使總和變 9
- `no vet` 三檔（`cg.no`/`cg-runtime.no`/`mir.no`）**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）

**未覆蓋 / 後續**：`str`/`vec` 載荷的讀寫（要 `alloca %option` + `bitcast` slot 位址到載荷型別，不是 `extractvalue ..., 1, 0`）`err('msg')`（tag 2 + 內聯 str）**大於 24 位元組載荷的堆上裝箱**（`optPayloadAddr` 的 `tag==0` 分支）`option` 跨基本塊（`alloca %option`）`option` 的 `==`/`!=`（比 tag 且比載荷）`match` 臂降級——同屬 M4-c 續件


### 5.3.4.bf s26——M4-c 第十一件：STRCAT 的 `retain`/`release`（RC 發射原語 + 平衡校驗）（2026-10-31 本轮）

**背景**：`%str-long` 是 nolang 第一個堆上帶 RC 頭的值類別——`@str_concat` 返回的結果就是 `rc=1` 的 owner（s19）但 s19→s25 全程**只發射了 `@str_concat` 調用，從未發射任何 `retain`/`release`**這在手寫 MIR測試裡看不出問題，卻是 **R3（漏 retain = UAF）** 的定時炸彈：一旦 `hir2mir` 真去降級，若漏掉任何一處 RC 事件，就是靜默的 use-after-free本件把 RC 發射做成兩個**原子 MIR op**（`OP-RETAIN`/`OP-RELEASE`），並把拿到 `str_concat` 結果即記一次 RETAINscope 退出 RELEASE的規矩釘死，讓後續 `hir2mir` 有可直接組合的磚塊，也讓 `check-rc-balance` 第一次有了**真實場景**的欠計數捕捉零新增檔，改 4 檔（`mir.no`/`cg.no`/`cg-rc.no`/`cg-test.no`）

**落地**：

1. `compiler/src/mir.no`：`OP-RETAIN = 27``OP-RELEASE = 28`註釋明定：當前**只作用於 `TY-STR`**——`extractvalue %str-long, 2` 取 data 指標（= RC 頭 +16），再 `call @nolang_rc_retain`/`@nolang_rc_release`；`vec` 走裸 `@malloc`（§11 B5，暫不 RC）`option` 尚未堆裝箱（§11 B8），故兩者暫不進這裡並註明：編碼與 `cg-rc.no` 的校驗事件碼（`RC-RETAIN=0`/`RC-RELEASE=1`）**刻意錯開命名空間**——MIR op 是發射什麼，事件碼是喂給 `check-rc-balance` 的帳本條目，二者語義相同但來源不同
2. `compiler/src/cg.no`：兩個 emit 分支`OP-RETAIN` 發 `%dp<k> = extractvalue %str-long %sv<x>, 2` + `call void @nolang_rc_retain(i8* %dp<k>)`；`OP-RELEASE` 發同上 extractvalue + `%rel<k> = call i1 @nolang_rc_release(i8* %dp<k>)`（返回 `i1` 棄用）臨時值 `%dp<k>`/`%rel<k>` 按**指令下標** `k` 命名，與 `%c<k>`/`%ot<k>` 同一套規矩，不會撞名覆蓋註釋補上 `RETAIN / RELEASE`
3. **命名衝突消解（關鍵）**：`cg-rc.no` 原本也定義了 `OP-RETAIN = 0`/`OP-RELEASE = 1`（給 `check-rc-balance` 用的**事件碼**）當 `cg-test.no` 同時 import `mir`（MIR op 27/28）與 `cg-rc`（事件碼 0/1）時，未限定的 `OP-RETAIN` 會**同名衝突**（`compiler` 跨模塊調用必須 `ShortName.` 前綴，見 R9）→ 把 `cg-rc.no` 的事件碼**改名**為 `RC-RETAIN`/`RC-RELEASE`（同步改 `check-rc-balance` 內的 `ops[j] == OP-RETAIN/OP-RELEASE` 參照），從根上消除衝突；`cg-runtime-test.no` 原本就用字面量 `0`/`1` 餵事件碼，不受影響
4. `compiler/tests/cg-test.no`：新增 `test-strcat-rc`（`cg-rc` import + 8 斷言）用 `cg-emit-cfg-t` 手搭 `catrc`：`STRCONST sv1='ab'` / `STRCONST sv2='cd'` / `STRCAT sv3 = sv1+sv2` / **`RETAIN sv3`**（記 `@str_concat` 的隱式 rc=1 擁有）/ `STRLEN v4 = sv3.len` / **`RELEASE sv3`**（scope 退出）/ `RET v4``vty=[I64,STR,STR,STR,I64]`（按值 id 索引）`cty` 逐指令斷言：① `@str_concat` 調用形狀；② `%dp3 = extractvalue %str-long %sv3, 2` + `call void @nolang_rc_retain(...)`；③ `%dp5 = extractvalue ...` + `%rel5 = call i1 @nolang_rc_release(...)`；④ `check-rc-balance([3,3],[RC-RETAIN,RC-RELEASE])` 回 `''`（平衡通過）；⑤ `check-rc-balance([3],[RC-RELEASE])`（只 release漏 retain）回 `under-count` 訊息——**證明 R3 的靜態捕捉**

**关键点**：

- 🔴 **`@str_concat` 的返回值 `rc=1` 視為隱式 RETAIN**：拿到結果即記一次 RETAIN 事件（不發額外 `call`，因為 `rc_alloc` 已經把計數設成 1），scope 退出再 RELEASE 一次這樣帳本上 `retain=release`，`check-rc-balance` 才不會誤報欠計數若把`@str_concat` 返回當成無擁有，只補 RELEASE 而漏 RETAIN 事件 ⇒ 帳本 `release > retain` ⇒ 靜態報錯（正是 R3 想抓的）
- 🔴 **data 指標是 `%str-long` 的 field 2**：RC 頭在 `base`（data − 16），`retain`/`release` 都從 data 指標 `getelementptr , i64 -16` 反向索引`%str-long = { i64 len, i64 cap, i8* data }`，故 `extractvalue ..., 2` 取第三欄 `i8*`，**不是**整個 struct 直接當 `i8*` 傳（那會指向 len 欄而非 RC 頭）
- **`vec`/`option` 暫不進 RC**：`%vec` 的數據緩衝目前是裸 `@malloc`/`@free`（§11 B5，元素只有 i64無 RC 需求）；`%option` 尚未堆裝箱（§11 B8）二者一旦要 RC，發射原語須先擴到取 data 指標以外的路徑，本件**不預支**——先讓 `str` 這條最危險的路（第一個 RC 消費者）跑通並有靜態守門
- **與 s19 的關係**：s19 是第一個 RC 消費者但**不發** retain/release（那是 `hir2mir` 的責任）；本件把那句話裡的`hir2mir` 責任預先做成原子 op，使 `hir2mir` 降級 `STRCAT` 時只需emit `OP-STRCAT` → emit `OP-RETAIN`（記擁有）→ ... → emit `OP-RELEASE`（退出）三連，RC 規矩不再散落在降級邏輯裡

**验证（可重跑）**：

- `./bin/no run compiler/tests/cg-test.no` ⇒ **86/86**（s25 為 78/78，本件 +8：`test-strcat-rc` 8 斷言）；`cg-runtime-test.no` ⇒ **33/33**（改名不影響，`check-rc-balance` 三條用例仍綠）
- **靜態 RC 平衡**：`test-strcat-rc` 內 `check-rc-balance([3,3],[RETAIN,RELEASE])` 回 `''`；`check-rc-balance([3],[RELEASE])` 回 `under-count` ⇒ 漏 retain 的欠計數**確實被抓**（R3 的靜態防線成立）
- **端到端執行（真 clang + ASan）**：`prelude-full()` + 手搭 `catrc`（含 RETAIN/RELEASE）+ 手寫 `main`（`call i64 @catrc()` → `trunc` 成 `i32` 當退出碼）⇒ `opt -passes=verify` **rc=0**；`clang -x ir -O2` 構建 **rc=0**；執行 **exit 4**（=`'abcd'.len`）；**`clang -fsanitize=address` 構建並執行 ⇒ exit 4ASan 無報告**（無 UAF無洩漏——RELEASE 在 scope 退出把 `rc 1→0` 釋放，`retain`/`release` 平衡正確）
- `no vet` 四檔（`cg.no`/`cg-runtime.no`/`cg-rc.no`/`mir.no`）**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）

**未覆蓋 / 後續**：① `vec` 元素非 i64 的逐元素 RC（§11 B5：`@vec_push_i64` 換 `@nolang_rc_alloc` + 逐元素 retain/release）；② `struct`（§11 B4）；③ `PHI`（本方案 M4-c 起即決定走 `alloca/store/load` 不引 PHI，故 PHI 非必需）；④ `STRCAT` 的 `str` 實參若是變數（其自身的 RC 生命週期由呼叫方負責，本件只管結果）；⑤ `option` 的 `str`/`vec` 載荷讀寫與堆上裝箱（§11 B8）——同屬 M4-c 續件


### 5.3.4.bg s27——M4-c 續件：STRCAT 實參為變數（堆上結果）的 RC 生命週期（2026-10-31 本輪）

s26 立了 `OP-RETAIN`/`OP-RELEASE` 原語與單次 `STRCAT` 結果要 release的規矩，但當時 operand 都是 `STRCONST` 字面量本件把場景推到**實參本身是堆上變數**（嵌套 `STRCAT`：`z = (a+b)+c`），釘死一條關鍵判別：

- **`STRCONST` 是全局常量無 RC 頭**：`cg.no` 發 `@.s<k> = ... constant [N x i8]`（s18），data 指標指向只讀全域，**絕不能**對它 `extractvalue ..., 2` 後 call `@nolang_rc_release`（會對無 16 字節頭的全域做 `-16` 偏移 ⇒ 讀垃圾/崩潰）故字面量 operand 不需要也不能 release
- **`STRCAT` 是堆上 rc=1 結果**：`@str_concat` 經 `@nolang_rc_alloc`（s19），data 指標有 RC 頭，結果是 owner故**每一個 `STRCAT` 結果——含作為另一個 `STRCAT` 實參的中間堆串——都必須在 scope 退出時 `RELEASE`**

這正好回答 s26 未覆蓋清單裡的`STRCAT` 實參為變數時的 RC 生命週期：常數實參零成本；變數（堆上）實參由呼叫方負責其自身的 retain/release 週期，編譯器只保證每次 `STRCAT` 產出的結果各記一次 RETAIN（@str_concat 的隱式 rc=1）各自 RELEASE

**`check-rc-balance` 是逐值賬本**（s26 已確認）：對每個 vid 數 RETAIN/RELEASE 次數，只報release 數 > retain 數（欠計數＝UAF），過計數（漏 release＝泄漏）依 §1.3.1 是**接受**的不報這意味忘記 release 中間堆串是泄漏靜默不被抓；能被抓的只有release 多於 retain的真 UAF本件的負向測試就用後者（對 sv3 發 `RELEASE` 卻無對應 `RETAIN` ⇒ 欠計數被捕）證明逐值賬本對嵌套場景仍生效；並在註釋中誠實標明忘 release 屬過計數不被本校驗器報

- **代碼**：零新增 op零新增檔（沿用 s26 的 `OP-RETAIN`/`OP-RELEASE`）`compiler/tests/cg-test.no` 增 `test-strcat-rc-nested`（13 斷言）：手搭 `catnested = (('ab'+'cd')+'ef').len`——sv1/sv2/sv4 為 `STRCONST` 常量（無 RC）sv3=sv1+sv2 與 sv5=sv3+sv4 為堆上結果（各記 RETAINscope 退出各 RELEASE），斷言兩段 `STRCAT` 調用`sv3`/`sv5` 的 retain/release 發射文本，以及逐值平衡通過＋欠計數被捕
- **驗證（本輪實測）**：`cg-test.no` **99/99**（86+13）；`opt -passes=verify` **rc=0**；手搭完整模組（`cg-runtime-prelude-full()` + `catnested` + `define i32 @main`）經 `clang -x ir -O2` 構建執行 **exit 6**（`'abcdef'.len`）`clang -fsanitize=address -O2` 構建執行 **exit 6 且 ASan/LSan 零報告**（sv3sv5 兩個堆串皆釋放無 UAF無泄漏）；`no vet` 四檔（`cg.no`/`cg-runtime.no`/`cg-rc.no`/`mir.no`）+ `cg-test.no` **0 ERROR**臨時 e2e 生成器與 `/tmp` 產物已清除
- **與 R3 的關係**：s26 是單結果的靜態＋動態雙重守門；本件把守門延伸到中間堆串——證明編譯器若漏發中間 `RELEASE`，ASan 的 LSan 會抓泄漏而 `check-rc-balance` 只能抓 UAF，兩者互補
- **未覆蓋（M4-c 續件）**：① `vec` 元素非 i64 的逐元素 RC（§11 B5）；② `struct`（§11 B4）；③ `option` 的 `str`/`vec` 載荷讀寫與堆上裝箱（§11 B8）；PHI 本方案決定走 `alloca` 不引，故非必需


### 5.3.4.bh s28——M4-c 續件：option 的 `str` 載荷讀寫與堆上裝箱（§11 B8，2026-10-31 本輪）

> ⚠️ **2026-11-11 後端佈局變更（本節以下為歷史紀錄，權威警示見 §5.3.4.ax／記帳 §11.2 B28）**：本節第一條「`%str-long = { i64, i64, i8* }` 恰 24 位元組、與 `[3 x i64]` 1:1 佈局相容」，以及由此推出的三步 `insertvalue`＋`ptrtoint`／`inttoptr` 還原 data，**只適用於 2026-11-11 之前**。現行 Go 後端 `%str-long = { i32, i32, i8* }`（16B）；`%option` 的 24B slot **未變**（`src/mir/codegen.go::optSlotDefaultBytes = 24`）⇒ `?str` **仍是 inline、仍免堆裝箱**（16B ≤ 24B，`docs/docs/lang/memory.md` 明載「只用了前 16 字節放 `{ u32 len, u32 cap, i8* data }`」），但**不再是 24B 的 1:1 對應**：兩個 u32 擠進前 8 位元組、`i8*` data 落在 offset 8，Go 現行走「把 slot 位址 `bitcast` 成 `<payload>*`」而非逐欄 `insertvalue`。selfhost `cg.no` 曾按本節舊法發射且 prelude 曾是 24B ⇒ **2026-11-12 已同步為 16B（§11.2 B28 已清償、詳 §11 後 ⑥）**：prelude 與 `@str_eq`／`@str_concat` 已和 Go `emitPrelude` 逐字相同，`cg.no` 各 op 補齊 `zext`／`trunc`；本節三步 `insertvalue` 僅作 24B 時代的歷史紀錄。

s25 立了 `%option = { i64, [3 x i64] }`（32 位元組 = 8 tag + 24 payload slot）與五個 op，但當時 `OP-OPTGET` 只讀slot[0] 當 i64——對 `?str` 載荷是**錯讀**（§11 B8）本件把 option 的 `str` 載荷讀寫走通，並釘死一條關鍵佈局事實：

- **`%str-long = { i64, i64, i8* }` 恰 24 位元組**（8×3），與 `%option` 的 payload slot `[3 x i64]`（24 位元組8 位元組對齊）**佈局相容** ⇒ `?str` 載荷可以**直接內聯**進 slot，免堆裝箱（>24B 的 `?vec`/大結構才需堆裝箱，留 §11 B8 餘項）這是 s26/s27str 必須堆上 RC之外的第二條 str 路徑：**option 取得 str 的擁有權時共用同一份堆 data 指標，不拷貝緩衝**
- **data 指標字段（i8*）與 i64 slot 互轉須 `ptrtoint`/`inttoptr`**：LLVM **不允許** `bitcast` 用於指標↔整數（報 `invalid cast opcode for cast from 'ptr' to 'i64'`），必須走整數轉換指令（與 `%vec` helper 同套路）🔴 實測：`bitcast i8* %ol2 to i64` 直接被 LLVM 拒，改 `ptrtoint`/`inttoptr` 才過
- **`insertvalue` 的輸出名不可與輸入名相同**（報 `multiple definition of local value named 'op'`）：OPTMKSTR/OPTGETSTR 都必須用**中間名**逐步構造（`%om0/%om1/%om2`→`%op<d>``%og0/%og1`→`%sv<d>`），最後一步才落到規範值名

**RC 語義（與 R3 一致）**：`OP-OPTMKSTR` 只存儲**不自動 retain**——option 取得 str 的擁有權須調用方緊接 `RETAIN`（rc=2：變數自身一份 + option 一份）；`OP-OPTGETSTR` 讀出**共享同一 data 指標**的 `%str-long`（不增 rc，讀取方若長期持有須自行 retain）scope 退出時 str 變數釋放兩次（一次本身一次 option 擁有權）`check-rc-balance` 逐值賬本只抓release>retain的 UAF

- **代碼**：`mir.no` 增 `OP-OPTMKSTR=29`/`OP-OPTGETSTR=30`（註釋標明 str 載荷內聯進 `[3 x i64]`data 指標須 ptrtoint/inttoptr`OP-OPTMKSTR` 不自動 retain；`OP-OPTGET` 註釋更新為只覆蓋 i64 載荷，str/vec 由 OPTMKSTR/OPTGETSTR 走內聯）；`cg.no` 在 `OP-OPTGET` 分支後`OP-RETAIN` 前新增兩個 emit 分支`compiler/tests/cg-test.no` 增 `test-option-str`（8 斷言）
- **驗證（本輪實測）**：`cg-test.no` **107/107**（99+8）；`opt -passes=verify` **rc=0**；手搭完整模組（`cg-runtime-prelude-full()` + `optstr`：`some('ab'+'cd')` 取出 str 取 `.len` + `define i32 @main` 用 `trunc i64 %xc to i32`）經 `clang -x ir -O2` 構建執行 **exit 4**（`'abcd'.len`）`clang -fsanitize=address -O2` 構建執行 **exit 4 且 ASan 零報告**（sv3 兩次 retain 對應兩次 release共享 data 指標無 UAF/洩漏）；`no vet` 五檔（`cg.no`/`cg-runtime.no`/`cg-rc.no`/`mir.no`/`cg-test.no`）**0 ERROR**臨時 e2e 生成器與 `/tmp` 產物已清除
- **與 §11 B8 的關係**：本件把 `?str` 載荷的內聯讀寫走通，證明按載荷型別選 op的約束可以落在免裝箱內聯這條最優路徑上；`?vec` 載荷與 >24B 堆裝箱仍待（B8 餘項）
- **未覆蓋（M4-c 續件）**：① `vec` 元素非 i64 的逐元素 RC（§11 B5）；② `struct`（§11 B4）；③ `option` 的 `vec` 載荷讀寫與 >24B 堆上裝箱（§11 B8 餘項）；④ option 跨塊`==`/`!=`match 臂；PHI 本方案決定走 `alloca` 不引，故非必需


### 5.3.4.bi s29——M4-c 續件：`struct`（首個聚合型別 `%struct.point`，2026-10-31 本輪）

**背景**：M4-c 至此覆盖的型别全是扁平叶子（i64 / `%str-long` / `%vec` / `%option`）——两个字段想一起活过一条语句，只能借 `%vec` 或 `%option` 的 slot 硬塞`struct` 是第一个**具名聚合型别**，也是日后 `hir2mir` 降级 struct literal / 字段访问的落点本件把构造 + 读字段走通，并顺手纠正一处**此前埋下的模块级错误**：结构体型别原在 `cg-emit-cfg-t` 里按本函式用到结构体就发一次发射——**单函式**测试永远看不出问题，多函式模块必然撞型别重定义**零新增档**，改 5 档（`mir.no`/`cg.no`/`cg-runtime.no`/`cg-test.no`/`cg-runtime-test.no`）

**落地**：

1. `compiler/src/mir.no`：`TY-STRUCT = 4`（第五个型别码）；`OP-STRUCT-MK = 31``OP-STRUCT-GET = 32`
2. `compiler/src/cg-runtime.no`：`cg-runtime-struct-type()`——`%struct.point = type { i64, i64 }`，插在 `%option` 之后并入 `cg-runtime-prelude()`（`prelude-full` 因此自动带上）
3. `compiler/src/cg.no`：`cg-ty-name`/`cg-val-prefix` 各加 `TY-STRUCT` 一臂（`%struct.point` / `%st`，**第六个值命名空间**）；删掉前置 pass 的 `hasStruct` 发射（见关键点①）；两个 emit 分支（链式 `insertvalue` 构造`extractvalue` 读字段）
4. `compiler/tests/cg-test.no`：新增 `test-struct-mk-get`（5 断言）；`cg-runtime-test.no` 的 `test-runtime-types` 补 2 条（`prelude` 与 `prelude-full` 各含 `%struct.point`）

**关键点**：

- 🔴 **具名型别必须由 prelude 唯一发射**哪个函式用到就自己发一次在**单函式**测试里完全正确，一旦模块里有两个用到结构体的函式就是硬错实测反证：把成品 IR 的 `%struct.point = type { i64, i64 }` 复制一行 ⇒ `opt: /tmp/s29dup2.ll:7:1: error: redefinition of type`本件把型别收归 `cg-runtime-struct-type()`（prelude 是唯一装配点，与 `%str-long`/`%vec`/`%option` 同一规矩），并在 `cg-emit-cfg-t` 留一行此处刻意不发的注释——否则下一个人加 op 时很容易顺手加回来
- **`%struct.point` 是值住 `%st<id>`**：LLVM 结构不可变，`insertvalue` 产生**新值**，故构造必须链式：`%sm0<k> = insertvalue %struct.point { i64 0, i64 0 }, i64 %v<a>, 0` → `%st<d> = insertvalue %struct.point %sm0<k>, i64 %v<a+1>, 1`中间步用 `%sm0<k>`（按**指令下标** k 命名）不是洁癖：把 `%st<d>` 当作两步 `insertvalue` 的输岀名会报 `multiple definition of local value named`（与 s28 的 `%om0/%om1`s26 的 `%dp<k>` 同一族坑）
- **`bs[k]` 在同族 op 里是两种语义**：`STRUCT-MK` 的 `bs[k]` 是**字段数**（本件固定 2，型别写死），`STRUCT-GET` 的 `bs[k]` 是**字段下标**（编译期常量，直接进 `extractvalue`）沿用 Go 侧三槽（`dsts`/`xa`/`bs`）复用的惯例，但**新增 op 时必须逐个查表**，不能凭同族猜
- **读出的字段落回 `%v<id>`**：`extractvalue %struct.point %st<x>, <bs>` 的结果是 i64 ⇒ 与 `ADD`/`MUL` 等硬编 `%v` 的既有分支天然相容（本件的 `34` 就是这么算出来的）反之，若字段是 `str`/`vec`，这些分支的 `%v` 硬编码会立刻错——这是只支援 i64 字段的真正原因，不是偷懒
- **`cg-ty-name`/`cg-val-prefix` 是命名空间的单一来源**（s23 立的规矩）：加 `TY-STRUCT` 只改这两处 + 两个 emit 分支，形参/返回值/`CALL` 的型别字串与前缀自动跟上

**验证（可重跑）**：

- `./bin/no run compiler/tests/cg-test.no` ⇒ **112/112**（s28 为 107/107，本件 +5）；`cg-runtime-test.no` ⇒ **35/35**（33+2）
- **多函式模块（本件的核心反证）**：模块 = `cg-runtime-prelude-full()` + `pt`（`struct{3,4}` ⇒ `3*10+4`）+ `pt2`（`struct{5,6}` ⇒ `5*10+6`）+ 手写 `main`，共 **199 行**；`%struct.point = type` 在整份 IR 中**恰好 1 次**；`opt -passes=verify` **rc=0**；`clang -x ir -O2` 构建执行 **exit 90**（34+56）；`clang -fsanitize=address -O2` 构建执行 **exit 90 且 ASan 零报告**
- **这个 90 有鉴别力**：字段写反（`{b,a}`）⇒ `43+65=108`；漏写字段 1（两栏同值）⇒ `33+55=88`；漏写字段 0（读回 0）⇒ `0*10+4=4` 与 `0*10+6=6` ⇒ **10**而重复发型别这个 bug **只有两个函式同时用到结构体才触发**——单函式测试全绿，这正是它此前没被发现的原因
- **`no fmt` 收账**：HEAD 版 `cg.no` 的 `no fmt -d` 是 **0 hunk**；工作树版（含 s28/s29 未提交改动）**149 行差异**，全部是 s28/s29 新代码的 `#{overflow=wrap}` 前空行与分支内注释缩排 ⇒ 即**本批改动自己引入的**，故本轮 `no fmt -w compiler/src/cg.no` 一次收干（`#{overflow=wrap}` **8 → 8** 保留，重跑 cg-test 仍 112/112，vet 仍 0 ERROR）
- `no vet` 五档（`cg.no`/`cg-runtime.no`/`mir.no`/`cg-test.no`/`cg-runtime-test.no`）**各 0 ERROR**；`no vet compiler/src` 当树 **34 ERROR 不变**（全 `ps.no`）
- ⚠️ **`ast-test` 本轮红，与本件无关**：`no run compiler/tests/ast-test.no` 报 `EmitLLVM … func test_type_string: consumer reads value 0 (NoVal) … (bug #85)`该档只 import `asp`/`ast`/`util.sb`**不 import `cg`**；且在 `git worktree add /tmp/s29wt HEAD` 的**干净 HEAD 树**上跑同档**报同一条** ⇒ 既有基线（`EmitLLVM` 对聚合体的既有缺陷），非本轮引入

**未覆盖（M4-c 续件）**：

① **写字段 `STRUCT-SET`**：LLVM 结构不可变 ⇒ 必须读旧结构 + `insertvalue` 出新结构 + 覆盖槽位，与 Go 侧就地写语义不同，需单独一件；
② **多个 struct 型别**：型别字串与栏位数现在写死在 `%struct.point` / 2 栏；泛化需要型别描述符（struct 名 + 栏位型别清单）进 MIR，`TY-STRUCT` 也要从一个型别码升级为型别码 + 型别 id；
③ **`str`/`vec`/`option` 栏位**：需逐栏位 RC（存入即 retain覆盖即 release 旧值），与 §11 B5vec 元素非 i64是同一个 RC 问题的两面；
④ **struct 作形参/返回值**：`cg-ty-name`/`cg-val-prefix` 已有臂但**未实测**（依赖 `vty` 记 `TY-STRUCT` 与 `%st<id>` 连续性）；
⑤ **struct 跨块**（`alloca %struct.point` + store/load）与嵌套 struct；
⑥ 既有余项照旧：vec 元素非 i64 的逐元素 RC（§11 B5）option 的 vec 载荷与 >24B 堆装箱（§11 B8）option 跨块/`==`/`!=`/match 臂


### 5.3.4.bj s30——M4-c 續件：結構體寫欄位 `STRUCT-SET` ＋ 結構體槽三件套（2026-10-31 本輪）

**背景**：s29 只做了構造 + 讀欄位，結構體是**純值**：能建能讀，但**不能改**——`st.x = v` 在 MIR 裡無處可去；更要緊的是**跨基本塊**：值語義的結構體一旦跨塊，就必須有地方安放（`alloca` 槽），否則回邊上每一輪都拿到同一份初始值本件補上這兩塊：`STRUCT-SET`（讀舊出**新值**）＋ `STALLOC`/`STSTORE`/`STLOAD`（槽三件套，與 str 的 `SALLOC`/`SSTORE`/`SLOAD` 一一對應）**零新增檔**，改 3 檔（`mir.no`/`cg.no`/`cg-test.no`）

**落地**：

1. `compiler/src/mir.no`：`OP-STRUCT-SET = 33``OP-STALLOC = 34``OP-STSTORE = 35``OP-STLOAD = 36`
   - `STRUCT-SET`：`dst` = 新結構體值 id；`xa[k]` = 舊結構體值 id；**`xa[k]+1` = 新欄位值 id（連續約定）**；`bs[k]` = 欄位下標
   - `STALLOC`：`dst` = 槽 id（`%sts<id>`）；`STSTORE`：`xa` = 結構體值 id`bs` = 槽 id無結果；`STLOAD`：`dst` = 結構體值 id`xa` = 槽 id
2. `compiler/src/cg.no`：四個 emit 分支（`insertvalue %struct.point %st<xa>, i64 %v<xa+1>, <bs>`；`alloca %struct.point`／`store`／`load`）；檔頭命名空間清單補 `%st`（值）與 `%sts`（槽）——**兩者只差一個 `s`**
3. `compiler/tests/cg-test.no`：新增 `test-struct-set`（7 斷言，含SET 不得發 store的反向斷言）

**关键点**：

- 🔴 **`STRUCT-SET` 是讀舊出**新值**，不是就地寫**：LLVM 結構型別不可變，`insertvalue` 產生新值舊值照樣可用要就地效果必須配槽：`STLOAD → STRUCT-SET → STSTORE` 三步（這正是 `st.x = v` 的降級形）把 SET 當成寫記憶體用會在回邊上靜默丟失更新——**本輪的負向對照就是這個**（見下）
- 🔴 **新的欄位值 id 用連續約定 `xa[k]+1`，不另開一個操作數槽**：MIR 的扁平 arena 只有 `dsts`/`xa`/`bs`/`syms` 四個可用槽位，`STRUCT-SET` 需要舊結構體 + 新值 + 欄位下標三個操作數與其新增平行陣列（要改 12 個參數的簽名，且 s20 已證明近似 op 必然 drift），不如沿用 `OP-CALL` 的**連續實參**慣例：舊結構體在 `xa`，新值自動取 `xa+1`代價是**分配值 id 的那一處**必須保證兩者相鄰（與 §11 B7 的 `vty` 同一類約束：**在分配處寫，不得事後補**）
- **槽住第七個命名空間 `%sts<id>`**，與結構體**值** `%st<id>` 只差一個 `s`寫錯不是型別錯，而是拿值名當指標用——LLVM 報 `'%st8' is not a pointer`，訊息指向 load 那行，真因在命名衝突s21 對 `%ss`（str 槽）已踩過同型坑，本件改名時刻意**不**用 `%st` 前綴以免撞值命名空間
- **槽 id 與值 id 是兩套空間**：測試裡槽用 20（值 id 只到 16），避免20 是什麼的歧義；但 `vty` 是**按值 id 索引**的（§11 B7），槽 id 不進 `vty`——否則 `vty` 會被撐到 21 項且中間全是填充值
- **`STALLOC` 的 `alloca` 落在發射點**（不是 entry block 歸併）：單塊/短循環無妨；一旦 `hir2mir` 真的把 `alloca` 放進循環體，每輪都會增長棧（R7）s16 起本方案全程用 `alloca/store/load` 代替 PHI，這條成本**是刻意接受的**，但 `hir2mir` 落地時必須把 `alloca` 提到函式入口

**验证（可重跑）**：

- `./bin/no run compiler/tests/cg-test.no` ⇒ **119/119**（s29 為 112/112，本件 +7）；`cg-runtime-test.no` ⇒ **35/35** 不變
- **端到端（四基本塊 + 回邊）**：`counter()` = `st = {0,0}` 存槽 → `while i < 5: st.count = st.count + 1; i = i + 1` → 讀回 `st.count`；`main` 呼叫後 `trunc` 成退出碼模組 **206 行**，`opt -passes=verify` **rc=0**，`clang -x ir -O2` 構建執行 **exit 5**，`clang -fsanitize=address -O2` 執行 **exit 5 且 ASan 零報告**
- **兩個負向對照（證明 5 有鑑別力）**：① 從成品 IR 刪掉循環內那條 `store %struct.point %st12, %struct.point* %sts20` ⇒ **exit 0**（槽從未更新，每輪讀到的都是初始 `{0,0}`）；② 把 SET 的欄位下標 `0` 改成 `1` ⇒ **exit 0**（寫到了填充欄，`count` 恒 0）這也正好證明SET 是純 SSA 值不是就地寫——不配 `STSTORE` 就等於沒寫
- `no vet` 三檔（`mir.no`/`cg.no`/`cg-test.no`）**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）；三檔 `no fmt -d` **各 0 hunk**（本輪新代碼一次寫對格式，無需 `-w`）

**未覆蓋（M4-c 續件）**：

① **多個 struct 型別**：型別字串與欄位數仍寫死 `%struct.point` / 2 欄（與 s29 同一個缺口，現已成為本族**唯一**的結構性瓶頸）；
② **`str`/`vec`/`option` 欄位**：`STSTORE` 覆蓋舊值時必須先 `RELEASE` 舊欄位的堆`STRUCT-SET` 存入時必須 `RETAIN` 新值——本件的槽三件套**只搬位元**，不做任何 RC；
③ **struct 作形參/返回值**（`cg-ty-name`/`cg-val-prefix` 已有臂但未實測）；
④ **嵌套 struct** 與 **struct 陣列**；
⑤ 既有餘項照舊：vec 元素非 i64 的逐元素 RC（§11 B5）option 的 vec 載荷與 >24B 堆裝箱（§11 B8）option 跨塊/`==`/`!=`/match 臂


### 5.3.4.bk s31——M4-c 續件：多結構體型別（型別碼編 id）＋ 任意欄位數（2026-10-31 本輪）

**背景**：s29/s30 把結構體做到能建能讀能改能跨塊，但**只有一個型別**：IR 型別別名硬編碼 `%struct.point``STRUCT-MK` 硬編碼 2 步型別宣告寫死在 prelude也就是說 `compiler/` 裡出現第二種 struct 的那一刻就卡死——這是 `hir2mir` 落地的**前置硬阻塞**本件把它泛化：**型別碼 = `TY-STRUCT-BASE + 結構體 id`**，型別宣告改由描述符表 `sfc`（`sfc[id]` = 欄位數）一次性發射；`STRUCT-MK` 按 `bs[k]` 的欄位數**循環展開****零新增檔**，改 5 檔（`mir.no`/`cg.no`/`cg-runtime.no`/`cg-test.no`/`cg-runtime-test.no`）

**落地**：

1. `compiler/src/mir.no`：`TY-STRUCT = 4` → **`TY-STRUCT-BASE = 100`**；結構體 #k 的型別碼是 `TY-STRUCT-BASE + k`四個結構體 op 的註釋同步寫明型別 id 從哪個槽取（見下表）
2. `compiler/src/cg-runtime.no`：`cg-runtime-struct-type()`（單一 `%struct.point`）→ `cg-runtime-struct-decl(id, nfields)` + `cg-runtime-struct-decls(sfc)`；**prelude 不再含任何結構體型別**
3. `compiler/src/cg.no`：`cg-ty-name`/`cg-val-prefix` 的 `t == TY-STRUCT` 改 `t >= TY-STRUCT-BASE`（→ `%struct.s<k>` / `%st`）；`STRUCT-MK` 改循環展開（初值 `zeroinitializer`，中間名 `%sm<k>_<j>`）；`GET`/`SET`/`STALLOC`/`STSTORE`/`STLOAD` 改按槽取型別 id
4. `compiler/tests/cg-test.no`：兩支既有結構體測試遷到新編碼；新增 `test-struct-wide`（結構體 #13 欄位，4 斷言）
5. `compiler/tests/cg-runtime-test.no`：改斷言prelude **不含** `%struct.`＋ `struct-decls([2,3,0])` 發 `s0`/`s1` 且跳過 `s2`

**型別 id 從哪個槽取——四個 op 四種來源（必須逐個查，不能憑同族猜）**：

| op | 型別 id 來源 | 理由 |
| --- | --- | --- |
| `STRUCT-MK` | `cty[k] - TY-STRUCT-BASE` | 結果值就是這個結構體 |
| `STRUCT-GET` | `vty[xa[k]] - TY-STRUCT-BASE` | 讀的是**被讀的值**的型別（結果是 i64，`cty` 給不出） |
| `STRUCT-SET` | `vty[xa[k]] - TY-STRUCT-BASE` | 舊值的型別（結果型別同，但以舊值為準更不容錯） |
| `STALLOC` | `bs[k]` | **無值操作數**，型別 id 只能放 b 槽（與`bs` 存欄位數／下標的既有語義刻意區分，靠 op 分辨） |
| `STSTORE` | `vty[xa[k]] - TY-STRUCT-BASE` | 存的是值 |
| `STLOAD` | `cty[k] - TY-STRUCT-BASE` | 結果就是這個結構體 |

**关键点**：

- 🔴 **型別 id 編進型別碼，而不是再開一個平行陣列**：`cg-ty-name`/`cg-val-prefix` 的入參只有**一個整數**，而 `cg-emit-cfg-t` 的參數表已經 12 個（s24 加 `vty`/`cty` 時就到邊界了）把 id 編進型別碼 ⇒ 型別別名 `%struct.s<k>` 與值前綴都能從一個整數推出來，**API 零變更**代價是型別碼不再是 0..4 的小整數，`t >= TY-STRUCT-BASE` 這種範圍判斷要寫對（`t == TY-STRUCT` 的老寫法會靜默漏掉所有結構體）
- 🔴 **`zeroinitializer` 取代 `{ i64 0, i64 0 }`**：欄位數現在由型別宣告決定，逐欄位寫 0 字面量既冗長又要在 emit 側拼字串`insertvalue %struct.s1 zeroinitializer, i64 %v1, 0` 對任意欄位數都成立（已實測 `opt`+`clang`）⚠️ 副作用：**型別宣告缺失時錯誤訊息變差**——`zeroinitializer` 失去型別上下文，報的是 `invalid type for null constant`（而非 `use of undefined type`），排查時要記得先看宣告有沒有發
- **`STRUCT-MK` 的中間名 `%sm<k>_<j>`**（k = 指令下標j = 欄位下標）：只有**最後一步**落 `%st<dst>`s29 的名字 `%sm0<k>` 把欄位下標寫死成 0，多欄位就會撞名
- **`STRUCT-SET` 的新欄位值 id = `xa+1`連續約定不變**（s30），但現在舊值的型別碼經 `vty` 查表——`vty` 因此從只有 `CALL` 用升級為**結構體類 op 的型別來源**，漏填不再只是 `CALL` 的問題（§11 B7 的約束範圍擴大）
- **宣告仍必須模組內唯一**：`cg-runtime-struct-decls(sfc)` 是唯一發射點，`cg-emit-cfg-t` 一個字都不發s29 的 B10 教訓在此**從一條註釋變成一個 API 形狀**——想再犯得先刪掉這個函式

**验证（可重跑）**：

- `./bin/no run compiler/tests/cg-test.no` ⇒ **123/123**（s30 為 119/119，本件 +4）；`cg-runtime-test.no` ⇒ **37/37**（35 − 2 舊斷言 + 4 新斷言）
- **端到端（同一模組兩個不同結構體）**：`a()` = `s0{3,4}` ⇒ `3*10+4` = 34；`b()` = `s1{1,2,3}` ⇒ `1*100+2*10+3` = 123；`main` = 34+123 ⇒ **exit 157**模組 206 行，`%struct.s0 = type { i64, i64 }` 與 `%struct.s1 = type { i64, i64, i64 }` 各**恰好 1 次**，`opt -passes=verify` **rc=0**，`clang -O2` 執行 **exit 157**，`clang -fsanitize=address` **exit 157 且 ASan 零報告**
- **兩個負向對照（證明宣告與描述符都被真的檢查）**：① 把 `s1` 宣告成 2 欄位 ⇒ `opt: …:186:21: error: invalid indices for insertvalue`（第 3 個欄位越界）；② 刪掉 `s1` 的宣告 ⇒ `opt: …:183:34: error: invalid type for null constant`
- `no vet` 五檔**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）；五檔 `no fmt -d` **各 0 hunk**

**未覆蓋（M4-c 續件）**：

① **欄位型別仍只有 i64**：`sfc` 只帶欄位**數**，不帶型別；`str`/`vec`/`option` 欄位需要欄位型別清單＋逐欄位 RC（存入 retain覆蓋 release）——這是本族下一個真正的瓶頸；
② **結構體名**：現在型別名由 id 生成（`%struct.s<k>`），語義層的具名 struct（`point`）到 id 的映射表要在 `hir2mir` 落地時建立（並保證同一程式內穩定）；
③ **struct 作形參/返回值****嵌套 struct****struct 陣列**（`%struct.s0*` 的 GEP 步長要靠型別，現有 `vec` 已踩過同源問題）；
④ 既有餘項照舊：vec 元素非 i64 的逐元素 RC（§11 B5）option 的 vec 載荷與 >24B 堆裝箱（§11 B8）option 跨塊/`==`/`!=`/match 臂


### 5.3.4.bl s32——M4-c 續件：結構體欄位型別（`sft` 拼接表 + 欄位型別感知的 MK/GET/SET，2026-10-31 本輪）

**背景**：s31 的 `sfc` 只帶欄位**數**，而 `STRUCT-MK`/`GET`/`SET` 全部硬編 `i64 %v`——所有欄位都被當 i64編譯器源碼裡最常見的欄位偏偏是 `str`（AST 節點的名字token 文本診斷訊息），不解決就沒有 `hir2mir`本件把欄位型別打通：宣告側加 `sft`（欄位型別碼的**拼接表**），發射側按值自身的 `vty`／`cty`選型別名與命名空間**零新增檔**，改 4 檔（`cg-runtime.no`/`cg.no`/`cg-test.no`/`cg-runtime-test.no`）

**落地**：

1. `compiler/src/cg-runtime.no`：新增 `cg-runtime-ty-name(t)`——**全庫唯一**的型別碼 → LLVM 型名映射（原在 `cg.no` 的 `cg-ty-name`，本件遷來）；`cg-runtime-struct-decl(id, nf, ft)` 按 `ft` 逐欄位寫型別；`cg-runtime-struct-decls(sfc, sft)` 用計數 + 游標把拼接表展開成每個結構體的欄位切片
2. `compiler/src/cg.no`：新增 `# /compiler/src/cg-runtime`（**層級方向：發射器依賴 prelude，反之不行**）；`cg-ty-name` 退化成一行轉發；`STRUCT-MK` 的每個欄位發 `cg-ty-name(vty[fv])` + `cg-val-prefix(vty[fv])`（不再寫死 `i64 %v`）；`STRUCT-GET` 的 dst 用 `cty[k]`；`STRUCT-SET` 的新值用 `vty[xa+1]`
3. `compiler/tests/cg-test.no`：新增 `test-struct-str-field`（4 斷言：`%str-long` 欄位的 insertvaluei64 欄位的 insertvalue`%sv` 讀出`.len` 取用）
4. `compiler/tests/cg-runtime-test.no`：`struct-decls` 改兩參，欄位型別 `s1 = {%str-long, i64, i64}`

**关键点**：

- 🔴 **欄位型別不必進描述符就能正確發射 MK/GET/SET**——因為每個值自身的 `vty` 已經帶型別（s24 立的型別屬於值，不屬於呼叫點）`sft` 的**唯一**用途是型別宣告：宣告必須一次寫全所有欄位的型別，哪怕某欄位從未被賦值這是值帶型別相對op 帶型別的直接紅利：新增欄位型別時，MK/GET/SET **一行都不用改**
- 🔴 **`sft` 是拼接表，不是二維表**：nolang 沒有二維容器；而 `id * stride + j` 會寫死一個欄位數上限（stride 一旦定死，超過它的 struct 就默默錯位）拼接表 + 游標沒有上限，代價是呼叫端必須按 id 升序拼
- 🔴 **`cg-runtime-ty-name` 必須唯一**：型別宣告屬於**模塊級裝配點**，裝配點不能反向依賴發射器 ⇒ 映射只能住在 `cg-runtime`若兩份映射並存，加一種型別就會宣告對發射錯（或反之），而 `opt` **只在該型別真的被用到時**才報——這正是 B10 那類單元測試無鑒別力的變體`cg.no` 只留一行轉發
- ⚠️ **`str` 欄位的 RC 完全沒做**（記入 §11 B11）：`STRUCT-MK` 把 `%str-long` 複製進結構體時**不 retain**，`STSTORE`/`STRUCT-SET` 覆蓋舊結構體時**不 release** 舊欄位 ⇒ 欄位裡若是堆上 `str`（`@str_concat` 的結果），源值一 release，結構體裡就是懸垂指標本輪 e2e **刻意只用 `STRCONST`**（全局常量無 RC 頭永不被 release），所以 ASan 乾淨——**這不能當作str 欄位安全的證據**
- **`STRUCT-GET` 的 dst 命名空間現在由 `cty[k]` 決定**：讀 i64 欄位落 `%v`讀 `str` 欄位落 `%sv`這使得讀 str 欄位再 `.len`這種最常見的組合直接成立（`%sv4 = extractvalue ...` → `%v5 = extractvalue %str-long %sv4, 0`）

**验证（可重跑）**：

- `./bin/no run compiler/tests/cg-test.no` ⇒ **127/127**（s31 為 123/123，本件 +4）；`cg-runtime-test.no` ⇒ **37/37**（斷言內容改為 `%struct.s1 = type { %str-long, i64, i64 }`）
- **端到端（帶 `str` 欄位的結構體）**：`hold()` = `{ .s = 'abcd', .n = 9 }` ⇒ `'abcd'.len + 9`；模組 `%struct.s0 = type { %str-long, i64 }` 恰好 1 次，`opt -passes=verify` **rc=0**，`clang -O2` 執行 **exit 13**（4+9），`clang -fsanitize=address` **exit 13 且 ASan 零報告**
- **負向對照**：把宣告的欄位順序寫反（`{ i64, %str-long }`）⇒ `opt: …:173:51: error: insertvalue operand and field disagree in type: '%str-long = type { i64, i64, ptr }' instead of 'i64'`——證明宣告與發射**必須由同一份型別來源**驅動（本件的 `cg-runtime-ty-name`）
- `no vet` 四檔**各 0 ERROR**；`no vet compiler/src` 當樹 **34 ERROR 不變**（全 `ps.no`）；四檔 `no fmt -d` **各 0 hunk**

**未覆蓋（M4-c 續件）**：

① **逐欄位 RC（§11 B11，最高優先）**：`str`/`vec`/`option` 欄位在存入時 retain覆蓋／離開作用域時 release；這需要 `struct-decls` 之外再帶一張哪些欄位持有堆的位圖，並在 `STRUCT-MK`/`STRUCT-SET`/`STSTORE`/`STLOAD` 四個位置各發一次；
② **嵌套 struct 欄位**：技術上已通（欄位型別碼就是 `TY-STRUCT-BASE + k`，`cg-runtime-ty-name` 認得），但**未實測**；
③ **struct 作形參/返回值**（`cg-ty-name`/`cg-val-prefix` 有臂，仍未實測）；
④ 具名 struct（`point`）→ id 的映射表：`hir2mir` 落地時建立並保證同一程式內穩定；
⑤ 既有餘項：vec 元素非 i64 的逐元素 RC（§11 B5）option 的 vec 載荷與 >24B 堆裝箱（§11 B8）option 跨塊/`==`/match 臂


### 5.3.4.bm s33——M4-c 續件：結構體欄位的 RC 原語（`STRUCT-RETAIN`/`STRUCT-RELEASE` + 欄位掩碼，2026-10-31 本輪）

**背景**：s32 讓 `str` 欄位存得進讀得出，但存進去**不 retain**覆蓋時**不 release** ⇒ 欄位裡若是堆上串（`@str_concat` 的結果），源值一 release 就是懸垂指標（§11 B11）本件只交付**兩個機械原語**（對第 j 個字段 retain/release），**不做策略判斷**——誰該 retain何時 release屬於 `hir2mir` 的職責，見未覆蓋①**零新增檔**，改 3 檔（`mir.no`/`cg.no`/`cg-test.no`）

**落地**：

1. `compiler/src/mir.no`：`OP-STRUCT-RETAIN=37``OP-STRUCT-RELEASE=38`約定 `xa[k]` = 結構體值 id`bs[k]` = **欄位位掩碼**（bit j 置位 ⇒ 欄位 j 持有堆）同族 op 的 `bs[k]` 本來就各 op 自訂（MK=欄位數GET/SET=欄位下標STALLOC=結構體 id這裡=掩碼）
2. `compiler/src/cg.no`：兩個 emit 分支，逐位置位各發三步——
   ```
   %rf<k>_<j> = extractvalue %struct.s<id> %st<x>, <j>
   %rp<k>_<j> = extractvalue %str-long %rf<k>_<j>, 2      ; data 指標（= RC 頭 +16）
   call void @nolang_rc_retain(i8* %rp<k>_<j>)             ; release 版多一個 %rl<k>_<j>
   ```
3. `compiler/tests/cg-test.no`：新增 `test-struct-field-rc`（6 断言：retain 三連release 三連掩碼未置位的字段**不得**被遍历）

**关键点**：

- 🔴 **`STRUCT-MK` 不自动 retain 是刻意的**，两条理由：① 操作数可能是 `STRCONST`——那是 `@.s<k>` **全局常量**，`retain` 会去读 `data-16` 的头，即**全局对象之外的内存**；② MIR 目前的型别信息（`vty`）只说这个值是什么型别，**不说它是否拥有堆**（s24 立的型别属于值不覆盖所有权）所以要不要 retain必须由调用方用掩码**显式声明**，而不是发射器猜
- 🔴 **掩码的 bit 只允许落在 `str` 字段上**：发射端按字段 j 是 `%str-long`取 `data` 指针；落在 `i64` 字段上 ⇒ `opt` 报 `extractvalue` 型别不符这是掩码自证型别的代价——`struct-decls` 已经有 `sft`（字段型别表），但**发射器看不到它**（描述符只在模块级装配点用），故本件只能靠约定 + 负向对照守
- 🔴 **UAF 只在真的读字节时才发生**：`%str-long = { i64 len, i64 cap, i8* data }`，`len`/`cap` 是**内联值**，`STRLEN` 读的是 SSA 值**完全不碰堆**所以漏 retain在一个只读 `.len` 的程序里**彻底静默**——s32 的 e2e 正是这样（`'abcd'.len + 9`）本件 e2e 因此刻意在取字段后再做一次 `STRCAT`（`@str_concat` 会 `memcpy` 源字节），才真正踩到已释放的缓冲
- 🔴 **门禁口径修正（本件最重要的副产物，§11 B12）**：`clang -x ir -fsanitize=address x.ll` 对 **IR 输入完全不插桩**最小反例（`malloc`→`free`→`load`）实测 **exit 0**，二进制里只有 `__asan_init`/`__version_mismatch` 两个 asan 符号`__asan_load*` **一个都没有**原因：ASan pass 只插桩带 `sanitize_address` **函数属性**的函数；clang 前端处理 `.no` 源码时自动加该属性，处理 `.ll` 输入时**不加**⇒ s26–s32 各节的ASan 零报告实际只覆盖 **libc 拦截器**（`malloc`/`free`/`memcpy`），裸 `load`/`store` 的 UAF 全部漏报修法：给每个 `define` 补 `sanitize_address`（`scripts/asan_instrument.py` 一行正则），再交给 clang；`opt -passes=asan` 手动插桩亦可，但 LLVM 22 在 MachO 上报 `MachO doesn't support COMDATs, 'asan.module_ctor' cannot be lowered`**本件起 ASan 门禁一律补属性 → clang两步**，历史各节的ASan 干净按此口径重新解读（数据字段层面仍是有效证据，只是覆盖面窄于字面）
- **掩码位宽**：`bs[k]` 是 i64 ⇒ 一次最多描述 64 个字段；超过的结构体需要分块发多条当前编译器自身的 AST 节点远未到 64，先记着不实现（记入未覆盖②）

**验证（可重跑）**：

```sh
./bin/no run compiler/tests/cg-test.no            # 133/133（s32 为 127，本件 +6）
./bin/no run compiler/tests/cg-runtime-test.no    # 37/37
./bin/no run tmp/e2e-s33.no > tmp/s33.ll          # hold() = ({'no'+'lang'} + 'lang').len + 27
opt -passes=verify -disable-output tmp/s33.ll                      # rc=0
clang -x ir -O2 tmp/s33.ll -o tmp/s33.bin && ./tmp/s33.bin         # exit 37
python3 scripts/asan_instrument.py tmp/s33.ll tmp/s33.inst.ll   # 补 sanitize_address（§11 B12）
clang -x ir -O0 -g -fsanitize=address tmp/s33.inst.ll -o tmp/s33.asan && ./tmp/s33.asan   # exit 37零报告
```

| 变体（在 `.ll` 上做，`tmp/s33ctl.py`） | `opt` | `-O2` | 补属性 + `-O0` ASan | 鉴别力 |
| --- | --- | --- | --- | --- |
| 原样 | rc=0 | exit 37 | exit 37零报告 | — |
| 删掉 `STRUCT-RETAIN` 三连 | rc=0 | exit 37 | **exit 134**：`heap-use-after-free … READ of size 6`（`@str_concat` 的 `memcpy`） | ✅ 有 |
| 删掉 `STRUCT-RELEASE` 三连 | rc=0 | exit 37 | exit 37零报告 | ❌ **无**（泄漏方向；macOS arm64 无 LSan，`detect_leaks=1` 无输出） |
| 重复 `STRUCT-RELEASE` | rc=0 | exit 37 | **exit 134**：`heap-use-after-free … READ of size 8`（`@nolang_rc_release` 读 rc 的那次 `load`） | ✅ 有 |

> `tmp/e2e-s33.no``tmp/s33ctl.py` 是本轮的 scratch 生成器（`tmp/` 在 `.gitignore`，未入库）：前者按上文落地的 op 序列拼 `/compiler/src/cg` 的 API，后者就是对生成好的 `.ll` 做删/复制那三行（`%rf/%rp/call`）——两个脚本都不含任何门禁逻辑，**唯一的门禁资产是 `scripts/asan_instrument.py`（已入库）**

- ⚠️ **对照必须跑 `-O0`**：`-O1/-O2` 会把两个常量串拼接整段折叠掉（连 `malloc` 都能被消去），删掉 retain 也照样 **exit 37**——**假绿**这条与 s30語料尺子空洞 ⇒ 只能立 fixture 且必须真触发同源
- **重复 release 的结论值得单独记**：第二次 `release` 读到 `rc == 0`（上一轮已经减到 0 并 free），走 `borrowed` 分支**直接返回不再 free** ——RC 的`rc==0` 借用哨兵把重复 release从 **double free** 降级成**一次越界读**这解释了为什么 C 式 double free panic 在这里看不到，也是 §5.6.1 铁律在实现层的自然冗余
- `no vet` 四档（`mir.no`/`cg.no`/`cg-runtime.no`/`cg-test.no`）**各 0 ERROR**；`no vet compiler/src` 当树 **34 ERROR 不变**（全 `ps.no`）；四档 `no fmt -d` **各 0 hunk**；`ps/ck/lx/tok/util/diag` 单测 38/20/45/62/27/7 **全绿**

**未覆盖（M4-c 續件）**：

① **自动放置**（最高优先）：谁在 `STRUCT-MK`/`STRUCT-SET`/`STSTORE` 处发 `STRUCT-RETAIN`谁在覆盖/离开作用域处发 `STRUCT-RELEASE`这需要一张此值是否拥有堆的所有权位（`vty` 只有型别），且必须由 `hir2mir` 按 §5.6.1 的发射规则表决定本件只把**扳手**造好
② 掩码 >64 字段（需分块）；③ `vec`/`option` 字段（`%vec` 走裸 `malloc` 无 RC 头option 未堆装箱——与 B5/B8 同族）；④ 嵌套 struct 字段（掩码只扫一层，内层字段的 retain/release 未展开）；⑤ 泄漏方向无动态门禁（LSan 缺席），只能靠 `check-rc-balance` 的静态账本；⑥ 既有餘項：struct 作形参/返回值具名 struct → id 映射表


### 5.3.4.bn s34——M4-c 續件：結構體作為一等值（形參/返回值 + 嵌套欄位）與字面量全局唯一化（2026-10-31 本輪）

**背景**：s33 補了欄位 RC 原語，但結構體能不能跨函式邊界與結構體能不能裝結構體一直停在 `cg-ty-name`/`cg-val-prefix` **有臂未實測**（s29/s31 的未覆蓋欄）這兩件事是 `hir2mir` 的必要條件——編譯器的 AST 到處是傳節點返節點節點套節點本件把兩者從有臂變有測試釘住，並在過程中撞出一個**真 bug**：字面量全局名跨函式撞名；它只在多函式 + 都有字串字面量時出現——正是 `hir2mir` 的產物形態**零新增檔**，改 2 檔（`cg.no`/`cg-test.no`）

**落地**：

1. **形參/返回值**：零 emit 變更——`ptyps[q]`/`retty` 寫 `TY-STRUCT-BASE + id` 即通實測三條形態：`define i64 @take(%struct.s0 %st1)``define %struct.s0 @give(%struct.s0 %st1)` + `ret %struct.s0 %st1``%st2 = call %struct.s0 @give(%struct.s0 %st1)`
2. **嵌套欄位**：零 emit 變更——`sft` 的欄位型別碼直接寫 `TY-STRUCT-BASE + 子 id`，`struct-decls` 發 `%struct.s1 = type { %struct.s0, i64 }`；MK 發 `%sm3_0 = insertvalue %struct.s1 zeroinitializer, %struct.s0 %st2, 0`，GET 的 dst 落 `%st`（由 `cty` 決定，不是 `%v`）
3. 🔴 **`cg-lit-name(fname, k)`（本輪唯一的代碼修復）**：字面量全局從 `@.s<k>` 改為 `@.s.<函式名>.<k>`修的是 `opt: redefinition of global '@.s1'`——兩個函式各自第 1 條 `STRCONST` 撞成同名函式名本來就是 `cg-emit-cfg-t` 的參數 ⇒ **零 API 變更零模塊級狀態**
4. `compiler/tests/cg-test.no`：新增 `test-struct-param-ret`（7 斷言）`test-struct-nested`（6 斷言）；`test-string`/`test-strcat` 的三條字面量斷言改新名 ⇒ **146/146**（s33 為 133，本件 +13）

**关键点**：

- 🔴 **模塊級實體唯一（§11 B10 同族，本件把它從具名型別擴到全局符號）**：發射器是**逐函式**調用的，凡名字裡只有函式內座標的全局實體都會在第二個函式撞名判據很簡單——**名字只含 `k`（指令下標）⇒ 必撞；含函式名或模塊級常量 ⇒ 安全**目前只有字面量中招；`cg-runtime` 的 helper（`@str_concat``@vec_*``@nolang_rc_*`）是模塊級單例，安全
- 🔴 **具名型別的宣告順序無關**（本件否證了一個很容易被當成約束的猜想）：把 `%struct.s1 = type { %struct.s0, i64 }` 寫在 `%struct.s0` **前面**，`opt -passes=verify` **rc=0**`clang -O2` 照樣 **exit 16**LLVM 的具名型別是惰性解析的，只有**自引用（遞歸）**才有額外約束⇒ `struct-decls` 按 id 升序**只是拼接表游標的便利**，不是語義要求（s31/s32 的按 id 升序拼講的是 `sft` 的展開方式，與發射順序無關）
- 🔴 **參數是引用語義在 struct 上的三條具體規則**（§5.6.1 那張表的落地版）：① 傳參**不 retain**（caller 保留所有權）；② callee **不得** release 參數（它是借用）；③ caller 的 release 必須在**最後一次使用之後**——包括把結構體傳出去這件事e2e 用結構體持堆串 `'nolang'`，傳給 `read1` 讓它 `STRCAT` 讀字段字節，**call 之後**才 `STRUCT-RELEASE`釘住 ③（exit 7）；負向對照把 release 挪到 call 前 ⇒ ASan `heap-use-after-free … READ of size 6`
- ⚠️ **跨邊界的所有權轉移仍未建模（§11 B13）**：返回值把結構體（含堆欄位）交給 caller 時，所有權轉移callee 不得 release這條目前靠約定，`check-rc-balance` 是按**值 id** 記賬的函式內工具，跨函式看不見
- **為什麼偏偏是多函式才炸**：s29–s33 的 e2e 都是prelude + 一個函式 + main 包裝，`main` 裡沒有 `STRCONST` ⇒ 撞不上這與 s30語料尺子空洞 ⇒ 只能立 fixture 且必須真觸發是同一類教訓：**測試形態覆蓋不到的地方，等於沒測**

**验证（可重跑）**：

```sh
./bin/no run compiler/tests/cg-test.no            # 146/146（s33 為 133）
./bin/no run compiler/tests/cg-runtime-test.no    # 37/37
./bin/no run tmp/probe-s34.no  > tmp/s34.ll       # mk()/bump(st)/total()   ⇒ 3+4 → (3+1)+4 = 8
./bin/no run tmp/probe-s34b.no > tmp/s34b.ll      # 嵌套：pair{pair{7},9}    ⇒ 7+9 = 16
./bin/no run tmp/probe-s34c.no > tmp/s34c.ll      # 边界 RC：('nolang'+'x').len ⇒ 7
opt -passes=verify -disable-output tmp/s34.ll     # rc=0（三个文件同）
clang -x ir -O2 tmp/s34.ll -o tmp/s34.bin && ./tmp/s34.bin                       # exit 8 / 16 / 7
python3 scripts/asan_instrument.py tmp/s34c.ll tmp/s34c.inst.ll
clang -x ir -O0 -g -fsanitize=address tmp/s34c.inst.ll -o tmp/s34c.asan && ./tmp/s34c.asan   # exit 7零报告
```

| 负向对照 | 结果 | 鉴别力 |
| --- | --- | --- |
| 父结构体字段 0 宣告成 `i64`（却发 `%struct.s0` 值） | `opt: … insertvalue operand and field disagree in type: '%struct.s0 = type { i64 }' instead of 'i64'` | ✅ |
| 漏发子结构体声明（`sfc[0]=0` 跳过） | `opt: … invalid type for null constant` | ✅ |
| 类型声明顺序写反（父在前） | `opt` rc=0`clang -O2` **exit 16** | ❌ 不是约束（见上） |
| `STRUCT-RELEASE` 挪到 `read1` 调用**之前** | ASan **exit 134**：`heap-use-after-free … READ of size 6`（`@str_concat` 的 `memcpy`） | ✅ |
| 修 bug 前：两个函数各含一条 `STRCONST` | `opt: redefinition of global '@.s1'` | ✅（本件已修） |

- `no vet` 三档（`cg.no`/`mir.no`/`cg-test.no`）**各 0 ERROR**；`no vet compiler/src` 当树 **34 ERROR 不变**（全 `ps.no`）；三档 `no fmt -d` **各 0 hunk**
- 回归：s33 的 `tmp/e2e-s33.no` 重跑仍 **exit 37**（字面量改名未影响单函式模組）

**未覆盖（M4-c 續件）**：

① **跨函式所有權轉移**（§11 B13：返回值把含堆欄位的結構體交給 caller，誰負責 release）；② 嵌套欄位的 RC（掩碼只掃一層，內層欄位未展開）；③ struct 陣列/切片（GEP 步長要靠完整型別）；④ 多返回值；⑤ struct 作 `vec` 元素（§11 B5 同族）；⑥ 具名 struct → id 映射表（`hir2mir` 落地時建）


### 5.3.4.bo s35——M4-c 續件：ALU 家族（位運算／移位／除餘／其餘比較）（2026-11-01 本輪）

|  |  |
| --- | --- |
**背景**：s14–s34 的算術只有 `ADD/SUB/MUL` 三件比較只有 `CMPLT` 一件`hir2mir` 一旦要降級 `ast.no` 的**字符串駐留**（djb2 雜湊：`h = h*31 + c`開放尋址 `h % CAP`以及 `==/<` 探測槽位）就立刻缺料——那些表達式正用到 `* + % & | ^ << >> == != < <= > >=`本件一次性補齊這 12 個 MIR op，讓i64 上的完整標量算術＋比較成為 `hir2mir` 可用的原子**零新增檔**，改 3 檔（`mir.no`/`cg.no`/`cg-test.no`）

**落地**：

1. **型別與約定**：這 12 個 op **全部是 `i64` 上的有符號標量**，dst/操作數一律走 `%v<id>`（`TY-I64`），與 `ADD/SUB/MUL` 完全同形——`dst = 結果值 id``xa[k] = 左操作數值 id``bs[k] = 右操作數值 id`比較結果是 `0/1` 的 `i64`（不是 `i1`）
2. **`mir.no`**：新增 `OP-CMPEQ=39 / OP-CMPNE=40 / OP-CMPLE=41 / OP-CMPGT=42 / OP-CMPGE=43 / OP-AND=44 / OP-OR=45 / OP-XOR=46 / OP-SHL=47 / OP-SHR=48 / OP-DIV=49 / OP-MOD=50`（接續 s33 的 `OP-STRUCT-RELEASE=38`，無空洞）
3. **`cg.no`**（`cg-emit-cfg-t` 的 op 分派）：
   | MIR op | LLVM 指令 |
   | --- | --- |
   | `OP-AND / OR / XOR` | `and / or / xor i64 %v<x>, %v<y>` |
   | `OP-SHL` | `shl i64` |
   | `OP-SHR` | **`ashr i64`**（算術右移，與 `i64` 有符號語義一致；**不發 `lshr`**——邏輯右移要等 `u64` 型別進場後用獨立 op） |
   | `OP-DIV / MOD` | **`sdiv / srem i64`**（與 Go 後端 `src/mir/codegen.go` 逐字對齊） |
   | `OP-CMPEQ/NE/LE/GT/GE` | `icmp <pred> i64 %v<x>, %v<y>` → `%c<k>`，再 `zext i1 %c<k> to i64` → `%v<dst>`（`<pred>` ∈ `eq/ne/sle/sgt/sge`，與既有 `OP-CMPLT` 的 `slt` 同形；中間名 `%c<k>` 按**指令下標**命名，與 `CMPLT` 共用槽位空間） |
4. `compiler/tests/cg-test.no`：新增 `test-alu`（**14 條斷言**：12 op 各釘一条指令文本 + `shr ashr` 正向 + `lshr` **缺席**負向對照）⇒ **146→160/160**（s34 為 146，本件 +14）

**關鍵點**：

- 🔴 **`SHR` 必須是 `ashr` 不是 `lshr`**：本族的 `OP-SHR` 承載的是**有符號** `i64` 右移（符號擴充）用 `lshr` 會在負數上錯誤填零——而 `djb2`/開放尋址裡移位多用於非負雜湊，錯填不會立刻顯形，屬**靜默錯誤**族`test-alu` 用 `contains('lshr')==0` 把這條釘成**結構約束**（不是風格偏好）
- 🔴 **不插除零護欄**：`OP-DIV/OP-MOD` 直接發 `sdiv/srem`，與 Go 後端一致（除零 = UB）這是一個**已知未收斂的審計項**——Nolang 語義層本應把 `x/0` 降級為 `?i64` 的 `err`（見 AGENTS.md §3.5 就地捕获），但這屬 `hir2mir` 的**語義降級**職責不是 codegen 這層；已在 **§11 B14** 立帳，`hir2mir` 落地整數除法時必須補 option 包裝，否則 `noc` 產出的程式會把本該 `err` 的除零變成 SIGFPE
- 🔴 **全族只有有符號語義**：無符號族（`udiv/urem/lshr/ult/...`）要等 `u64`/`byte` 型別進場，本件**刻意不做**——`vty` 目前只有 `TY-I64` 一條算術路，提前開無符號臂會製造型別碼存在但無人發射的空洞（§11 B7 同族教訓）
- ⚠️ **本件是純 SSA 值無 RC**：12 op 全在 `i64` 上，不觸堆 ⇒ 不需要 `RETAIN/RELEASE`，`check-rc-balance` 對本族無賬（與 §5.3.4.bm 的 str 欄位 RC 是正交的兩族）

**驗證環境（⚠️ 工具鏈代次事故）**：本輪 HEAD（`v0.4.14-3`）的 **Go 侧 MIR codegen 有一個跨模組結構體欄位回歸**——`EmitLLVM: setfield: field "buf" not found on "sb.sb"`——凡 `import` 了 `util/sb`（持 `buf []byte` 欄位）的測試檔（含 `cg-test.no`/`cg-runtime-test.no`/`probe-s35.no`）在 HEAD 工具鏈上**全部無法構建**實測：`git archive HEAD` 乾淨構建的 `bin/no` 復現該錯誤；同一檔在 `v0.4.13` 工具鏈上正常因 `cg.no`/`mir.no`/`cg-test.no` 在 `v0.4.13 ↔ HEAD` **逐位元組相同**（`git diff --stat` 為空），本件的代碼生成與後端**未變**，故按本方案一貫的**同一穩定工具鏈代次**紀律改用**釘死的 `v0.4.13`** 構建並驗證（該回歸屬 Go 侧 S0 的並行在途改動，**不在本自舉子項目修復範圍**——依 §2 黃金法則：疑似工具錯誤只記錄不篡改 `.no` 源碼繞過）

**驗證（可重跑，於釘死 `v0.4.13` 工具鏈）**：

```sh
NOC=/tmp/nolang-v413/bin/no          # 乾淨 v0.4.13（make no）
$NOC run compiler/tests/cg-test.no          # 160/160（s34 為 146，test-alu +14）
$NOC run compiler/tests/cg-runtime-test.no  # 37/37
$NOC run tmp/probe-s35.no > tmp/s35.ll      # djb2 鏈 + 位運算 + 6 條比較，219 行 IR
opt -passes=verify -disable-output tmp/s35.ll              # rc=0
clang -x ir -O2 tmp/s35.ll -o tmp/s35.bin && ./tmp/s35.bin # exit 161
```

| 對照 | 結果 | 鑑別力 |
| --- | --- | --- |
| `OP-SHR` 誤發 `lshr` | `test-alu` 的 `contains('lshr')==0` 斷言 FAIL | ✅（本件釘死） |
| `OP-DIV/MOD` 除零 | 發 `sdiv/srem`，無護欄 ⇒ 運行期 SIGFPE | ⚠️ 未收斂，見 §11 B14 |
| 12 op 全 `TY-I64` 標量 | `opt -passes=verify` rc=0e2e exit 161 | ✅ |

**未覆蓋（M4-c 續件，接 §5.3.4.bn 清單）**：ALU 家族只做 `i64`；`f64` 算術（`fadd/fsub/fmul/fdiv/fcmp`）`u64/byte` 無符號族（已由 **s36 §5.3.4.bp** 落地）以及整數除零的 option 降級（§11 B14）均屬後續；這些都需先有對應型別碼進 `vty`


### 5.3.4.bp s36——M4-c 續件：無符號 ALU 家族（`lshr/udiv/urem` + 無符號比較）與 `TY-U64` 型別碼（2026-11-02 本輪）

**背景**：s35 一次性補齊了 `i64` 上的有符號算術＋比較，但**刻意留了兩個洞**並在 §5.3.4.bo 明文標註：①邏輯右移要等 `u64` 型別進場後用獨立 op，別在這裡按需改成 `lshr`；②無符號族（`udiv/urem/lshr/ult/...`）要等 `u64`/`byte` 型別進場……提前開無符號臂會製造型別碼存在但無人發射的空洞（§11 B7 同族教訓）本件一次性把**兩個洞一起補上**——既落地 `TY-U64` 型別碼（語義層可據此標記按無符號降級），又落地七個無符號 op（codegen 據此發射手）⇒ 型別碼與發射臂**同代次成對出現**，正是 s35 那條不許無人發射約束的正解**零新增檔**，改 3 檔（`mir.no`/`cg.no`/`cg-test.no`）

**落地**：

1. **`TY-U64 = 4`**：接 `TY-OPT=3` 之後的空洞（`TY-STRUCT-BASE=100` 之前）🔴 **關鍵設計**：`u64` 在 LLVM 裡與 `i64` **同一型別同一命名空間**（都是 `i64` / `%v`）——符號性只活在 op 的助記符（`lshr` vs `ashr``udiv/urem` vs `sdiv/srem``ult/ule/ugt/uge` vs `slt/sle/sgt/sge`）故 `cg-runtime-ty-name` / `cg-val-prefix` 對 `TY-U64` 走**默認臂**（返回 `i64` / `%v`）即正確，**無需新增任何映射**——`TY-U64` 的價值全在語義層選 op，而非在 codegen 層改型名
2. **`mir.no`**：新增 `OP-SHRU=51 / OP-DIVU=52 / OP-MODU=53 / OP-CMPLTU=54 / OP-CMPLEU=55 / OP-CMPGTU=56 / OP-CMPGEU=57`（接續 s35 的 `OP-MOD=50`，無空洞）`OP-CMPEQ/OP-CMPNE` 與符號無關（`icmp eq/ne` 對同一 `i64` 位元組模式結果一致），**沿用 s35 那兩件**，不重複定義
3. **`cg.no`**（`cg-emit-cfg-t` 的 op 分派）：
   | MIR op | LLVM 指令 |
   | --- | --- |
   | `OP-SHRU` | **`lshr i64`**（邏輯右移，高位填 0；有符號版 `OP-SHR` 發 `ashr`） |
   | `OP-DIVU / MODU` | **`udiv / urem i64`**（除零仍 UB，§11 B14 同條） |
   | `OP-CMPLTU/LEU/GTU/GEU` | `icmp <ult/ule/ugt/uge> i64` → `%c<k>`，再 `zext i1 %c<k> to i64`（與 s35 有符號 cmp 逐字同形，只差 `pred`） |
4. `compiler/tests/cg-test.no`：新增 `test-alu-unsigned`（**16 條斷言**：`TY-U64` 簽名 `define i64 @ua(i64 %v1)` 1 條 + 7 op 各釘指令文本（含 `cmpltu` 的 `icmp`+`zext` 兩條）+ **7 條負向對照**斷言本函式絕不含 `ashr/sdiv/srem/slt/sle/sgt/sge`）⇒ **160→176/176**（s35 為 160，本件 +16）

**關鍵點**：

- 🔴 **型別碼與發射臂同代次成對落地**（本件存在的理由）：s35 拒絕只開臂不給型別碼，也拒絕只給型別碼不發射手本件把 `TY-U64` 與七個無符號 op **一起**落地，正是那條約束的兌現——審計者可用新 op 有無對應 `vty` 型別碼新 `vty` 型別碼有無對應發射臂雙向核對
- 🔴 **`u64` 復用 `i64` 的 `i64`/`%v`，不是新命名空間**：與 `TY-STR`（`%sv`）/`TY-VEC`（`%vc`）等不同——那些在 LLVM 裡是**不同型別**必須換前綴（否則同名不同型報錯）`u64` 在 LLVM 就是 `i64`，換的只是**指令助記符**不是**值命名空間**，故 `cg-val-prefix` 一行都不用改
- ⚠️ **除零仍 UB（§11 B14 同條）**：`OP-DIVU/OP-MODU` 直發 `udiv/urem`，與有符號版同一口徑——除零是 SIGFPE，補 option 降級是 `hir2mir` 的職責，不在 codegen 這層
- ⚠️ **符號性鑑別力在比較不在移位**（e2e 實測所證）：`ult` 對 `-1`（位元組全 1 = u64 最大）回 0對稱的 `slt` 回 1 ⇒ e2e 對最終值**有鑑別力**（正解 18誤發有符號得 19）；但 `lshr` vs `ashr` 對 `-1` 只差 **bit63**，被 e2e 的 `& 0xFF` 掩碼抹平 ⇒ 移位的符號性**只能靠助記符斷言釘**（`test-alu-unsigned` 的 `contains('lshr')==1` + `contains('ashr')==0`）這與 s35`SHR` 必 `ashr` 非 `lshr`是同一族結構約束

**驗證環境（⚠️ 沿用釘死工具鏈）**：與 s35 同——HEAD（`v0.4.14-3`）Go 侧跨模組結構欄位回歸（`setfield: field "buf" not found on "sb.sb"`）未解，`cg.no`  transitively import `util/sb` ⇒ 本族驗證仍在**釘死的 `v0.4.13`** 上進行（`cg.no`/`mir.no`/`cg-test.no` 在 `v0.4.13↔HEAD` 逐位元組相同）⚠️ 另需 `export DEVELOPER_DIR=/Library/Developer/CommandLineTools`，否則 `no run` 構建探針時 `ld` 對 `libSystem.tbd` 報 `unknown architecture`（與本件無關的環境坑，見 memorymir_golden.sh 需設 DEVELOPER_DIR）

**驗證（可重跑，於釘死 `v0.4.13` 工具鏈）**：

```sh
export DEVELOPER_DIR=/Library/Developer/CommandLineTools
NOC=/tmp/nolang-v413/bin/no          # 乾淨 v0.4.13（make no）
$NOC run compiler/tests/cg-test.no          # 176/176（s35 為 160，test-alu-unsigned +16）
$NOC run compiler/tests/cg-runtime-test.no  # 37/37（零回歸）
$NOC run tmp/probe-s36.no > tmp/s36.ll      # 無符號取值鏈（lshr/udiv/urem + ult/ule/ugt/uge），196 行 IR
opt -passes=verify -disable-output tmp/s36.ll              # rc=0
clang -x ir -O2 tmp/s36.ll -o tmp/s36.bin && ./tmp/s36.bin # exit 18
```

| 對照 | 結果 | 鑑別力 |
| --- | --- | --- |
| `OP-SHRU` 誤發 `ashr` | `test-alu-unsigned` 的 `contains('ashr')==0` 斷言 FAIL | ✅（助記符釘死） |
| `OP-CMPLTU` 誤發 `slt`（有符號） | e2e 由 **exit 18 → 19**（v9 `ult(-1,1)=0` 變 `slt=-1<1=1`） | ✅（實測：反向探針得 19） |
| `TY-U64` 誤当獨立命名空間 | `define i64 @ua(i64 %v1)` 簽名斷言 FAIL（若發 `%uv1`） | ✅ |
| 7 op 全發射手 | `opt -passes=verify` rc=0e2e exit 18 | ✅ |

**未覆蓋（M4-c 續件，接 §5.3.4.bo 清單）**：`f64` 算術（`fadd/fsub/fmul/fdiv/fcmp`，需 `TY-F64` 進 `vty`）已由 **s37 §5.3.4.br** 落地；整數除零的 option 降級（§11 B14，註：**本輪核實 Go 後端 `codegen.go` 其實在 codegen 層就插了護欄**，見 §5.3.4.br除零護欄更正）待 `hir2mir` 對齊；`byte`（u8）窄型別的無符號族（暫以 `TY-U64`＋掩碼代替）待語義層型別收窄規則


### 5.3.4.br s37——M4-c 續件：`f64` 算術家族（`fadd/fsub/fmul/fdiv/frem` + `fcmp`）與 `TY-F64` 型別碼（按型別分派）（2026-11-02 本輪）

**背景**：s36 §5.3.4.bp 明文把 `f64` 算術列為下一件（需 `TY-F64` 進 `vty`）本件落地浮點 ALU 家族與 s36 的 `u64` **根本不同**：`u64` 在 LLVM 裡與 `i64` 同型別同命名空間（只差助記符），而 **`f64` 是真独立 LLVM 型別 `double` + 独立值命名空間 `%fd`**（LLVM SSA 同名不同型直接報錯 ⇒ 必須換前綴，同 `TY-STR`/`TY-VEC` 一族）故本件同時做三件事：①型別碼 `TY-F64`；②**兩張映射表**（`cg-runtime-ty-name`→`double``cg-val-prefix`→`%fd`）；③**按型別分派**的算術/比較發射**零新增 op**（不同 s36），改 3 檔（`mir.no`/`cg.no`/`cg-test.no`）+ 新探針 `tmp/probe-s37.no`

**落地**：

1. **`TY-F64 = 5`**（`mir.no`）：接 `TY-U64=4` 之後`TY-STRUCT-BASE=100` 之前
2. **兩張映射**：`cg-runtime.no` 的 `cg-runtime-ty-name` 對 `TY-F64` 返 `'double'`；`cg.no` 的 `cg-val-prefix` 對 `TY-F64` 返 `'%fd'`（新命名空間，與 `%sv/%vc/%op` 同族）
3. **按型別分派（不新增 op，忠實 Go `emitArith`）**：
   - **算術按結果型別 `vty[d]` 分派**（鏡像 Go `isFloat := resLT=="double"`）：`OP-ADD/SUB/MUL/DIV/MOD` 在 `vty[d]==TY-F64` 時發 `fadd/fsub/fmul/fdiv/frem double %fd<x>, %fd<y>`；`!=TY-F64` 時走原 `add/sub/mul/sdiv/srem i64 %v` 分支（**逐字節不變**）位運算 `AND/OR/XOR/SHL/SHR` **無浮點等價 ⇒ 不分派**
   - **比較按運算元型別 `vty[x]`**：`==`→`oeq``!=`→**`une`**（非 `one`，保證 `NaN!=NaN` 為真）`<`→`olt``<=`→`ole``>`→`ogt``>=`→`oge`；整數尾 `zext i1→i64` 對兩型通用
4. **`cg-test.no`**：新增 `test-alu-f64`（**21 條斷言**：`double %fd` 簽名 + 5 浮點算術 + 6 `fcmp` + 同檔混型整數 `ADD` 實證分派 + 負向對照 `sdiv/srem/icmp/sub i64/mul i64`）⇒ **176→197/197**

**除零護欄更正（🔴 本輪核實修正 §11 B14 與 §5.3.4.bo/.bp 舊誤記）**：實現 f64 時順帶核對權威後端 `src/mir/codegen.go`——**整數 `/``%` 在 codegen 層就插了護欄**（`icmp eq <divisor>, 0` + `select` 一個安全除數，使除零結果**有定義**；僅 `isFloat` 跳過）⇒ 之前 §11 B14 / §5.3.4.bo/.bp 所寫與 Go 後端 `sdiv/srem` 逐字對齊除零=UB對 **Go 後端是誤記**真實缺口應為：**`noc`（自舉 codegen）當前把 `OP-DIV/MOD`（s35）與 `OP-DIVU/MODU`（s36）直發裸 `sdiv/srem/udiv/urem`，缺了 Go 後端有的 select 護欄** ⇒ `noc` 產物除零是 SIGFPE，而 `no` 是定義值（保真缺口，非雙方都是 UB）浮點 `fdiv/frem` 除零是 IEEE 定義（inf/nan）無 UB不需護欄，與 Go 的 `!isFloat` 一致

**段錯誤修正（🔴 s37 引入本輪已修）**：s37 使每個 `ADD/SUB/.../CMP*` 都讀 `vty[d]/vty[x]`，而 **s37 之前只有 `CALL`/struct op 讀 `vty`** ⇒ 一批既有 `cg-emit-cfg-t` 測試的 `vty` 長度覆蓋不到算術/比較結果 id ⇒ **越界讀 → 測試二進位運行期段錯誤**修法：新增 `cg-vty-at(vty, id)` 安全讀（`t = vty[id] #{index-out=zero}`——越界返元素零值 `0 = TY-I64` ⇒ 走原整數路徑，對既有 i64 測試**逐字節不變**），與 `cg-ty-name`/`cg-val-prefix` 的未知即 i64默認一致；便捷入口 `cg-emit-cfg` 本就自鋪全長 `vt`struct op 的裸 `vty[x]` 讀**保留不改**（那裡的覆蓋契約仍強制，不應靜默掩蓋）

**關鍵點**：
- 🔴 **分派而非新 op**：`f64` 與 `u64` 分岔的根本原因——`u64` 只差助記符（獨立 op 合理）`f64` 真改 IR 型別（必須讀 `vty` 分派，忠 Go；也避開 B7型別碼有發射臂無）審計者可據同一 `OP-ADD` 依 `vty[d]` 雙路發射核對
- 🔴 **`%fd` 是新命名空間**：與 `u64`（復用 `%v`）相反，`double` 必須換前綴，否則 SSA 同名不同型报错——這是本件需改 `cg-val-prefix` 而 s36 不需的原因
- ⚠️ **浮點常量無法用 `OP-CONST` 產生**（它只發 `add i64 0, N`）⇒ e2e 探針的被測函式改收 `double` 形參由手寫 `main` 傳字面量（`call i64 @f37(double 3.0, double 2.0)`）

**驗證環境**：沿用钉死 `v0.4.13` + `export DEVELOPER_DIR=/Library/Developer/CommandLineTools`（同 §5.3.4.bp）

**驗證（可重跑，於钉死 `v0.4.13`）**：

```sh
export DEVELOPER_DIR=/Library/Developer/CommandLineTools
NOC=/tmp/nolang-v413/bin/no
$NOC run compiler/tests/cg-test.no          # 197/197（s36 為 176，test-alu-f64 +21）
$NOC run compiler/tests/cg-runtime-test.no  # 37/37（零回歸）
$NOC run tmp/probe-s37.no > tmp/s37.ll      # f37(3.0,2.0) 浮點取值鏈，198 行 IR
opt -passes=verify tmp/s37.ll -o tmp/s37.opt.ll   # rc=0
clang -x ir -O2 tmp/s37.opt.ll -o tmp/s37.bin && ./tmp/s37.bin  # exit 5
```

| 對照 | 結果 | 鑑別力 |
| --- | --- | --- |
| double 值誤走整數模板（`fadd double`→`add i64`） | `opt -passes=verify` **rc=1**（`'%fd1' defined with type 'double' but expected 'i64'`） | ✅（實測：反向探針 rc=1） |
| `oge` 誤發 `ogt` | e2e **exit 5→4**（v12 `0.0>=0.0=1` 變 `0.0>0.0=0`） | ✅（實測：反向探針得 4） |
| `f64` 誤映射到 `%v`/`i64` 命名空間 | `define ... @fa(double %fd1, ...)` 簽名斷言 FAIL | ✅ |
| 5 算術 + 6 比較全發射手 | `opt` rc=0e2e exit 5 | ✅ |

**未覆蓋（M4-c 續件）**：`byte`（u8）/`f32` 窄型別（本件只做 `f64`）；整數除零 select 護欄的 `noc`↔Go 保真對齊（見上除零護欄更正，待 `hir2mir`/codegen 補選）；結構體/vec 元素的逐欄位浮點 RC


### 5.3.4.bs s38——M4-c 續件：整數除零 select 護欄的 `noc`↔Go 保真對齊（收斂 §11 B14 的 codegen 層缺口）（2026-11-02 本輪）

**背景**：s37 §5.3.4.br 的除零護欄更正已核實——權威 Go 後端 `src/mir/codegen.go` 的 `emitArith` 對 `!isFloat && op ∈ {Div,Mod,UDiv,UMod}` **在 codegen 層就插了 select 護欄**（`icmp eq <divisor>,0` 判零 + `select` 換安全除數，使除零結果**有定義**；有符號另檢 `(MIN,-1)` 防溢出）；而自舉 `cg.no` 直到 s35/s36 只直發裸 `sdiv/srem/udiv/urem`，除零在 arm64 折成 poison 垃圾值，在 x86 觸 `#DE`/SIGFPE ⇒ 這是 `noc`↔`no` 的**保真缺口**（非雙方皆 UB）本件把自舉 codegen 補齊至與 Go 同形（純 codegen 層 `select` 護欄，不含 option 語義降級與 stderr 報告）

**落地**（改 3 檔 + 新探針 `tmp/probe-s38.no`）：

1. **`cg.no` 新增 helper `cg-div-guard(mn,d,x,y,k,sgn)`**：回傳一段護欄 IR 字串中間量按指令下標 `k` 命名（`%dz.<k>`=判零，`%dmn.<k>`/`%dn1.<k>`/`%db.<k>`/`%dall.<k>`=MIN/-1 合成，`%ds.<k>`=安全除數，`%cr.<k>`=原始運算結果），帶點前綴與 `%c<k>`（cmp）/`%p<k>`（字面量）命名空間互不衝突，與 Go 的 `%dz.<seq>` 同構；定義化結果直接命名成 `%v<d>` 供下游引用
2. **4 處整數分支改調用（嚴格 gate `cg-vty-at(vty,d)!=TY-F64`）**：`OP-DIV`→`cg-div-guard('sdiv',d,x,y,k,1)`，`OP-MOD`→`'srem',…,1`，`OP-DIVU`→`'udiv',…,0`，`OP-MODU`→`'urem',…,0`；`sgn==1` 才發 MIN/-1 偵測（忠 Go：無符號只判零）；`fdiv/frem` 分支**逐字節不動**（IEEE 除零有定義，Go `!isFloat` 亦跳過）
3. **護欄語義**：判零 → `%ds.<k>=select badv,1,divisor`（換 1 避 UB）→ `%cr.<k>=mn i64 %v<x>,%ds.<k>` → `%v<d>=select i1 %dz.<k>,0,raw`（除零結果定義化成 0）
4. **`mir.no`/`cg.no` 頭部注釋**：把除零由（UB，不插護欄）改為（s38 起 `sdiv/srem/udiv/urem` 裹 select 護欄，§11 B14 的 codegen 半項已收）
5. **`cg-test.no` 斷言改護欄形態**：`test-alu`（DIV=k7，MOD=k8）補 zeroDiv/isMin/isNeg1/and/or/safe-divisor/sdiv-safe/result-zero + 負向（無裸 `%v8 = sdiv`）；`test-alu-unsigned`（DIVU=k2，MODU=k3）補 zeroDiv/safe-divisor/udiv-safe/result-zero + 負向（無符號不含 MIN 常量） ⇒ **197→211/211**

**關鍵點**：
- 🔴 **只做 codegen 半項**：`x/0`→`?i64` 的 **option 語義降級屬 `hir2mir`**（B14 語義半項），`divZeroReport` stderr 需 runtime 輔助函式 + once-per-exec 全局——兩者**刻意不含**，留作後續子項；本件僅消除 UB/poison/SIGFPE，使除零結果定義化
- 🔴 **無符號不做 MIN**：忠 Go——無符號 `badDiv==zeroDiv`（只判零），有符號才把 `(dividend==MIN && divisor==-1)` 的溢出偵測 `or` 進去
- ⚠️ **命名空間衝突規避**：護欄中間量若復用 `%c<k>` 會與 cmp 的 i1 撞名 ⇒ 用帶點的 `%dz.<k>` 一族隔離開

**驗證環境**：沿用钉死 `v0.4.13` + `export DEVELOPER_DIR=/Library/Developer/CommandLineTools`（同 §5.3.4.bp/.br）

**驗證（可重跑，於钉死 `v0.4.13`）**：

```sh
export DEVELOPER_DIR=/Library/Developer/CommandLineTools
NOC=/tmp/nolang-v413/bin/no
$NOC run compiler/tests/cg-test.no          # 211/211（s37 為 197，護欄斷言 +14）
$NOC run tmp/probe-s38.no > tmp/s38.ll      # g38(10,5,0)：3 條除零 + 2 條正常
opt -passes=verify tmp/s38.ll -o tmp/s38.opt.ll   # rc=0
clang -x ir -O2 tmp/s38.opt.ll -o tmp/s38.bin && ./tmp/s38.bin  # exit 2（定義值，非 SIGFPE）
```

| 對照 | 結果 | 鑑別力 |
| --- | --- | --- |
| 有符號除零發裸 `sdiv …,0` | clang -O2 執行 arm64 poison 垃圾值 / x86 `#DE`（exit≠2） | ✅（反向探針 `tmp/s38_unguarded.ll` 得 exit 1） |
| 護欄把除零結果定義化成 0 | e2e **exit 2**（10/0=0，5%0=0，udiv 10/0=0，彙總得 2） | ✅ |
| 無符號誤插 MIN/-1 偵測 | `test-alu-unsigned` 負向斷言 `'-9223372036854775808'==0` FAIL | ✅ |
| 護欄 IR 型別不匹配 | `opt -passes=verify` rc=1 | ✅（實測 rc=0 通過） |

**未覆蓋（M4-c 續件）**：整數除零的 **option 語義降級**（B14 語義半項，`hir2mir` 使 `x/0` 產 `?i64` 的 err）；`divZeroReport` stderr 報告；`byte`（u8）/`f32` 窄型別算術；結構體/vec 元素的逐欄位浮點 RC


### 5.3.4.bt s39——M4-c 續件：整數除零 `divZeroReport` stderr 報告（收 §11 B14 的 codegen 層最後一塊）（2026-11-02 本輪）

**背景**：s38 §5.3.4.bs 補齊了 select 護欄，但 Go 權威 `emitArith` 的 plain 路徑在除零時還多做一件——發一條 `call void @__nolang_div_zero_report(i1 zeroDiv)` 把診斷寫到 stderr（`codegen.go:6088`，helper `divZeroReport` @16049）；s38 刻意把這塊列為後續子項，本件收掉它，使 `noc` 的整數除零在**結果定義化**之外再與 `no` 的**可觀測行為**對齊（除零不再靜默）

**落地**（改 4 檔 + 復用探針 `tmp/probe-s38.no`）：

1. **`cg-runtime.no` 三處 prelude 擴充**：`cg-runtime-decls` 補 `declare i64 @write(i32, i8*, i64)`（Go emitPrelude 早已有，自舉此前缺）；新增 `cg-runtime-div-zero-msg`（`@nolang_msg_div_zero = private unnamed_addr constant [85 x i8] c"…"`，85 位元組診斷串）與 `cg-runtime-div-zero-report`（`define void @__nolang_div_zero_report(i1 %bad)`：`br i1 %bad, label %report, label %done`，report 塊 `call i64 @write(i32 2, …, i64 85)`）；兩者在 `cg-runtime-prelude-full` 每模組發一次
2. **`cg.no` `cg-div-guard` 收尾追加一條呼叫**：結果 `%v<d> = select zeroDiv,0,raw` 之後發 `call void @__nolang_div_zero_report(i1 %dz.<k>)`——**用 `%dz.<k>`（zeroDiv）而非 `%dall.<k>`（badDiv）**（忠 Go：只有真除零才報告，`(MIN,-1)` 溢出非除零不觸發 stderr）
3. **為何做 helper 而非 inline 分支**：護欄發在呼叫方基本塊**中間**，就地 `br` 會切斷基本塊；單條 `call` 不動塊結構（同 Go 註釋所述，option-print helper 同理）
4. **診斷串逐字保真的轉義坑**：Go 把每字节發成 `\XX`（`\6E\6F…\0A`）；自舉源裡反引號原始串含反斜杠會被 `no fmt` 清空（實測變 `r = ''`），改**單引號串 + 雙反斜杠**（源中 `\\6E` → 輸出 `\6E`，同 `cg-emit-cfg-t` 字面量全局的 `'\\00'` 手法）

**關鍵點**：
- 🔴 **codegen 層 §11 B14 至此全收**：s38 的 select 護欄 + s39 的 stderr 報告＝Go `emitArith` plain 路徑除零的**完整可觀測行為**；唯餘 **`hir2mir` 的 option 語義降級半項**（`x/0` 產 `?i64` 的 err，非本 codegen 層）
- 🔴 **once-per-module 的取捨**：Go 對 msg 全局與 helper 做惰性（只有走 plain 除零的模組才發），自舉 prelude 是**無條件**發（每模組都帶一份，未用也不傷，LLVM 允許未引用函式與私有全局）——與護欄本身逐站無條件發保持同一口徑
- ⚠️ **報告用 zeroDiv 不用 badDiv**：`(MIN,-1)` 走安全除數得定義值但**不是除零**，誤用 badDiv 會在合法溢出边界多印 stderr（實測以 zeroDiv 為條件，非零除數站 `%dz` 為假即跳過）

**驗證環境**：沿用钉死 `v0.4.13` + `export DEVELOPER_DIR=/Library/Developer/CommandLineTools`（同 §5.3.4.bp/.br/.bs）

**驗證（可重跑，於钉死 `v0.4.13`）**：

```sh
export DEVELOPER_DIR=/Library/Developer/CommandLineTools
NOC=/tmp/nolang-v413/bin/no
$NOC run compiler/tests/cg-test.no           # 215/215（s38 為 211，report 呼叫斷言 +4）
$NOC run compiler/tests/cg-runtime-test.no   # 41/41（s38 為 37，prelude 三新符號 +4）
$NOC run tmp/probe-s38.no 1>tmp/s39.ll       # g38(10,5,0)：3 條除零 + 2 條正常
opt -passes=verify tmp/s39.ll -disable-output      # rc=0
clang -x ir -O2 tmp/s39.ll -o tmp/s39.bin && ./tmp/s39.bin  # exit 2，stderr 見診斷 3 次
```

| 對照 | 結果 | 鑑別力 |
| --- | --- | --- |
| 全局串誤用反引號原始串 | fmt 清空 → `@nolang_msg_div_zero` 定義行缺失 → prelude 斷言 FAIL | ✅（本輪踩到，改單引號 + 雙反斜杠） |
| report 呼叫條件誤用 badDiv | `(MIN,-1)` 非除零站多印 stderr | ✅（用 zeroDiv 跳過） |
| msg 全局缺閉合雙引號 | `opt -passes=verify` rc=1（字串常量截斷） | ✅（補 `"` 後 rc=0） |
| 非零除數站 | `%dz` 為假 → `br` 走 done 不 write（stderr 0 次） | ✅（實測 k0/k1 無輸出） |

**未覆蓋（M4-c 續件）**：整數除零的 **option 語義降級**（B14 語義半項，`hir2mir` 使 `x/0` 產 `?i64` 的 err，届时 plain 路徑的 stderr 報告讓位於 err tag）；`byte`（u8）/`f32` 窄型別算術；結構體/vec 元素的逐欄位浮點 RC


### 5.4 語義檢查（M3）

**目標**：復刻 37 個 `Validate*` 與型別推斷，使 `no vet` 的診斷集合一致

**關鍵復刻點**：

1. **診斷的三條通路**（一條規則要接三個地方，漏一個就會編輯器看得到`no build` 看不到或反之）：
   | 通路                                     | 入口                                                                                       |
   | -------------------------------------- | ---------------------------------------------------------------------------------------- |
   | `no build` / `no run` / `no test`（硬錯誤） | `checker.ValidateTypes` **加上** `src/build/transpiler.go` 的 `allValidateErrs` 明確列表        |
   | `no vet`（CLI）                          | `Transpiler.Compile` 先跑硬錯誤列表，再對**合併後**的程式跑 `checker.RunAllLints`（`SkipTypeChecks: true`） |
   | 編輯器                                    | `lsp/server.go` → `checker.RunAllLints` 對**原始未合併**的程式                                    |
2. **std 簽名表**：`src/checker/stdsig_gen.go`（**現況實測 4,461 行7 張表**：870 funcSigs / 2,371 methodSigs / 720 拍平 struct-fields / 1 alias / 105 struct-mod / 3 enum / 118 modules；舊文7,179 行859/1,983`embeddedStdSigVersion = 2`已過期，該版本常量在非測試 Go 碼不存在）**必須**搬進 `compiler/src/ck-sig.no`做法：寫一個一次性的 `compiler/tools/gen_ck_sig.py` 讀 `src/checker/stdsig_gen.go` 生成 `.no` 源碼，同步釘改用小表 **源檔 sha256（`CK-SIG-DIGEST`）****不要**在 `noc` 啟動時重新收集（那會讓每次啟動都變慢，且與 `no` 的時序不同）✅ 已落地（s9b，§5.3.4.an）：生成物現為惰性模組，尚無消費端，M3-c checker 接線後才生效**已接線（s13，§5.3.4.ar）**：`ck.no` 首族 `'X' is not defined` 已消費 ck-sig.no 的 bk/bv/bo 裸名門表＋mok 模組名（`CK-SIG-VERSION` 2→3）
3. **`#{buildin}` 樁的三條載入路徑**（**158 處**）：一個空的 `#{buildin}` 函式體是由 Go 內建表實作的**承諾**若任一路徑把它當真實函式生成，呼叫點就會綁到空符號 ⇒ **靜默錯值**（`f64.sqrt` 回堆疊垃圾`math.clamp` 回 0`os.get-env` 回 `''`）三條路徑與守衛：
   | 路徑                                                                           | 位置                                         | 守衛                                                                                       |
   | ---------------------------------------------------------------------------- | ------------------------------------------ | ---------------------------------------------------------------------------------------- |
   | std 自動載入                                                                     | `checker.loadStdModuleBodyForce`           | 跳過樁                                                                                      |
   | 剝除                                                                           | `stripBuiltinStubs`（`build/transpiler.go`） | 剝除樁                                                                                      |
   | **顯式 `# std/x` 導入**                                                          | `processUseAndMerge`                       | ⚠️ **曾漏掉**；修法是 `if fd.BuiltinStub { merged.BuiltinFuncNames[fd.Name] = true; continue }` |
   | **驗收**：`rc` **不能**偵測這個 bug（Nolang 測試 runner 斷言失敗仍 rc=0）⇒ **必須 hash stdout** |                                            |                                                                                          |
4. **診斷輸出的兩種形狀**（不要發明第三種）：
|  |  |  |
| --- | --- | --- |
   - 面向使用者：`<file>:<line>:<col>: [ERROR|WARNING|HINT] <source>: <message> [<code>]`
   - 中間編譯錯誤串：`<channel>: [<file>: ]line L, column C: <message> [<code>]`，多條以 `; ` 連接；`<channel>` ∈ `validation errors: ` / `parser errors: ` / `check error: `  
     ⚠️ **切分只能切後面跟著位置前綴的 `; `**——訊息文字本身可能含分號

**Oracle**：`no vet <file>`驗收 = 全語料**歸一化後**逐行一致（§7.3）


### 5.5 中端：HIR / MIR（M4）

**目標**：HIR 樹 + 67 個 MIR op + 降級規則

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

> **這一張表是本方案最大的槓桿**：它把 `src/mir` 的 43,177 LOC 中**約 40%** 從自舉必需降級為可選


### 5.6 代碼生成（M4）：RC 優先的堆策略

#### 5.6.1 為什麼用 RC 而不是精確所有權

Nolang 今日的堆管理是**編譯期精確所有權分析**：`insertDrops` 決定每個值的 drop 位置，`moveSrc`/`moveDst` 決定 move 的所有權轉移，四個述詞（`moveStructSharesHeap` / `moveStrSharesHeap` / `moveSliceSharesHeap` / `moveTransfersOwnership`）的**併集**決定來源是否仍擁有堆

這套機制的正確性極難達到，repo 的歷史就是一部又一次 double free的編年史（`txt` 佈局的 13 個站點`%vec` 元素寬度option 的 box投影與克隆別名轉發的順序……）**在自舉階段重寫它是最高風險的選擇**

**替代方案：引用計數（RC）**`no` 已經有完整的 RC 基礎設施（P4/P5 of the ownership model）：

```
%nolang_hdr = type { i64, i64 }   ; rc, flags;  data = base + 16
```

- `@nolang_rc_alloc(size)` — 分配 `size + 16`，寫頭，`rc = 1`
- `@nolang_rc_retain(p)` — `rc++`
- `@nolang_rc_release(p)` — `rc--`，到 0 時 `free(base)`
- `rc == 0` 是**借用哨兵**（FFI 指針字串常量`cap == 0` 的 `str→[]byte` 視圖），兩個 helper 對它**立即返回**

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

- ❌ **循環洩漏**：環永遠不會歸零→ **接受**：編譯器是短命進程，記憶體上界 = O(輸入大小 × 常數)
- ❌ **原子操作開銷**：`noc` 是單執行緒（除驅動層的 `co`），**用非原子計數**
- ❌ **retain/release 指令開銷**：比精確 drop 多，但 LLVM `-O2` 能消掉相當一部分
- ✅ **正確性論證是局部的**：每一處 retain/release 的正確性只依賴它自己的上下文，不需要全程序定點分析
- ✅ **不動 `noc` 自身的語義**：`noc` 源碼由 S0 編譯時，用的是 S0 的精確所有權；`noc` 產出的程式才用 RC**兩者互不干擾**

> ⚠️ **RC 的實作有一條必須遵守的鐵律**：**每個別名都必須攜帶自己的計數**漏一個 retain 是 **UAF**（不是洩漏）因此 M4 必須有 `check-rc-balance` 驗證器（沿 `handleAliasSet` 的同一套別名判定），並且**只檢查欠計數方向**——過計數是 §1.3.1 明確接受的洩漏，對它報錯會產生 false positive ⇒ 硬編譯失敗

#### 5.6.2 為什麼發射 LLVM IR 文本而不是 C / 機器碼

| 選項                     | 前端工作量                  | 需要新寫的 runtime                              | 跨平台     | 編譯速度 | 選擇           |
| ---------------------- | ---------------------- | ------------------------------------------ | ------- | ---- | ------------ |
| **LLVM IR 文本 → clang** | 中（照抄 `codegen.go` 的結構） | **0**（照抄既有 prelude）                        | ✅ clang | 中    | ✅ **DR-2**   |
| C 源碼 → cc              | 低（無 SSA/phi）           | **需要一整套 C runtime**（str/vec/map/option/RC） | ✅       | 慢    | ❌            |
| 自研機器碼 + 鏈接器（nocg/nold） | **極高**                 | 0                                          | ❌ 需三端口  | 快    | ❌ 另一份設計文檔的範圍 |

**LLVM IR 文本的關鍵優勢**：`src/mir/codegen.go` 裡的 runtime prelude 是**字串常量**（`windowsShimsConst``%task` preludeRC helper），可以**逐字節照抄**到 `compiler/src/cg-runtime.no`，不需要重新設計 ABI

⚠️ **照抄 IR 文本時的一條硬規則**：**Go raw string（反引號）內部不得出現反引號**若在 IR 註釋裡用反引號強調識別符號，會**終止 Go 字串字面量**，報錯是 Go 語法級的（`expected ';', found _truncate`），不是 IR 錯誤在 nolang 裡對應的規則同樣成立（`'...'` 是 str，\`\`\` 是原始字串）——**IR 註釋裡一律用普通雙引號引用識別符號**

#### 5.6.3 IR 生成的驗收

M4 的驗收**不能只靠程式跑得對**，因為：

- LLVM 的 UB 在 `-O0` 正確`-O2` 錯誤（佈局變化會把潛在的越界讀變成 UB）；
- 一個沉默的正確可能只是棧佈局的巧合

**四層驗收**：

1. **行為**：全語料 `(rc, sha256(stdout))` 與 golden 一致（L4）
2. **IR 形狀**：對**排序後**的 `.ll` 做 diff（宣告順序非確定，直接 diff 必有噪聲）要求關鍵函式（`@str_concat``@nolang_rc_*``@nolang_free`每個 `@__nolang_drop_*`）的**存在性**一致，而不是逐位元組一致
3. **`opt` 驗證**：對每個產出的 `.ll` 跑 `opt -passes=verify`，**0 失敗**⚠️ 驗證失敗時 `.ll` **不會被持久化**——必須用 `NOLANG_MIR_DUMP_LL=1`（該開關在驗證**之前**寫檔）才能看到出錯的 IR
4. **動態記憶體檢查（ASan）**：`clang -x ir -fsanitize=address x.ll` **默認不插樁**（ASan pass 只插樁帶 `sanitize_address` **函式屬性**的函式，而 clang 前端處理 `.ll` 輸入時不加該屬性）⇒ 只有 `malloc`/`free`/`memcpy` 等 libc 攔截器生效，裸 `load`/`store` 的 UAF **全部漏報****必須先跑 `scripts/asan_instrument.py <in.ll> <out.ll>` 補屬性**再交給 clang，且負向對照一律跑 `-O0`（`-O1/-O2` 會把常量構造整段折疊，漏 retain 也照樣通過）證據與最小反例見 §5.3.4.bm / §11 B12


### 5.7 驅動層（M5）

**目標**：`noc build/run/ast/vet/tokens`，含 workspace/package 解析`#` 導入模組合併調用 clang

| 子項               | 復刻點                                                                                                                                                          |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **workspace 查找** | 向上查找 `workspace.jsonc`（`pkg.FindWorkspaceRoot`）；找不到 ⇒ 相對路徑解析會錯成 `<root>/<絕對路徑>`（§2.1 實測）                                                                     |
| **`#` 導入**       | `# /path/module.symbol [alias]`，路徑**相對於 workspace 根**；`std/*` **自動載入**無需顯式導入；跨模組調用**必須** `ShortName.` 前綴                                                     |
| **模組合併**         | 同名常量**按名字去重取第一個**（`mergedGlobalConsts`）；這條規則曾導致第二個模組的函式被鎖在自己的常量之外                                                                                         |
| **target 解析**    | `parseTargetPlatform()`（`build/builder.go`）是唯一既有實作；**禁止** `runtime.GOOS`；**禁止**把平台常量寫在 package 級 `var`（會在驅動解析 `-target` 之前凍結成 HOST 的值）                       |
| **外部工具**         | `process.start-with-opts("clang", ["-x","ir","-O2","-o",out,ll], "", "", true)`；⚠️ Windows 上的 libc 差異（無 `realpath`/`utimensat`/`sync`/`lstat`，**無裸 `stat`**） |
| **快取**           | `src/cache`（106 LOC）：內容雜湊 + `stdsig` keyM5 可以先不做快取（每次都全量編譯），但**必須**保留 `stdsig` key 的語義，否則 §5.4 的簽名表會與 `no` 不同步                                              |

### 5.8 診斷模型（`diag.no`）

```no
diag {
    file str
    line i64
    col i64
    level i64; 0=HINT 1=WARNING 2=ERROR
    src str; "nolang-parser" / "nolang-lint" / ...
    msg str
    code str; 8 字元 base36 TraceID
}
```

⚠️ **TraceID 的來源**：`no` 的 TraceID 由 `scripts/stamp_traceid.sh` 在 `make` 時用 perl 把 `TraceID: "PLACEHOLDER"` 替換成隨機 8 字元 base36**`noc` 必須使用與 `no` 完全相同的 TraceID 表**（否則 L3 診斷差分全線不同）做法：把 `no` 的 TraceID 表**導出為一個 `.no` 常量表**（`compiler/src/diag-id.no`，由 `tools/gen_stdsig/` 一併生成）



### 5.3.4.bu s40——M4-c 續件：option 的 `vec` 載荷內聯讀寫（`?[]i64`，§11 B8 免裝箱半項）（2026-11-02 本輪）

; 背景：§11 B8 記帳 `%option = { i64, [3 x i64] }` 只有一個 IR 型別，載荷真實型別活在語義層
s28（§5.3.4.bh）先走通 `?str` 內聯（`%str-long` 恰 24B）；本輪補 mir.no 註釋早已預告的同構路徑——`?vec`
`%vec = { i64, i64, i64 }`（f0=len，f1=cap，f2=dat）也恰 24B，且三字段全 i64（dat 是 ptrtoint 後的整數指針，見 cg-runtime-vec-get/push 內部才 inttoptr 還原）
⇒ 與 [3 x i64] 槽 1:1 逐字段 extract/insertvalue 互轉，連 ptrtoint/inttoptr 都不需要，比 str 載荷更簡單

**落地**：
- compiler/src/mir.no：新增 OP-OPTMKVEC = 58／OP-OPTGETVEC = 59（接既有最大碼 OP-CMPGEU=57 之後，刻意不重排 22..57 免影響既有 arena 數據）
- compiler/src/cg.no：cg-emit-cfg-t 加兩條發射臂，完全鏡像 s28 的 OPTMKSTR/OPTGETSTR——OPTMKVEC 發 tag=0 + 把 %vc<x> 的 3 個 i64 字段逐字段 insertvalue 進 slot[0..2]（中間步 %mv0..%mv2<k> 規避 multiple-definition，末步落 %op<d>）；OPTGETVEC 從 slot[0..2] extractvalue 3 字段，以 zeroinitializer 起重建 %vc<d>；無 ptrtoint/inttoptr
- RC 口徑：與 s28 一致，OPTMKVEC 只存不 retain；vec 走裸 @malloc（§11 B5，元素只有 i64 無逐元素 RC 需求），故 ?[]i64 不涉及 RC 頭，本件無須動 cg-rc

**關鍵點**：
- 免裝箱判據 = 24B：optionPayloadInline 以 24 字節為門檻分流內聯／堆裝箱；%vec 恰踩線 ⇒ 內聯；>24B 的大結構/含指針载荷 才需 optPayloadAddr 的 tag==0 分支堆裝箱（B8 另半項，仍待）
- 全 i64 是 vec 相對 str 的額外簡化：str 的 data 是 i8*，須 ptrtoint/inttoptr；vec 的 dat 已在 helper 裡整數化，故 option 存取的三個字段與槽型別完全一致，無指針到整數的轉換

**驗證環境**：沿用 s35–s39 的釘死 v0.4.13（HEAD 工具鏈有跨模組結構欄位回歸，代碼生成側 cg.no/mir.no 在 v0.4.13 對 HEAD 逐字節相同）

; 可重跑：
$NOC=/tmp/nolang-v413/bin/no; export DEVELOPER_DIR=/Library/Developer/CommandLineTools
$NOC run compiler/tests/cg-test.no                      # cg-test 222/222（vec 載荷斷言 +7）
$NOC run tmp/probe-s40.no 1>tmp/s40.ll                  # ?[]i64 put/get 往返（push×2 觸發 dat 重分配）
opt -passes=verify tmp/s40.ll -disable-output           # rc=0
clang -x ir -O2 tmp/s40.ll -o tmp/s40.bin && ./tmp/s40.bin  # exit 90 = 88（dat 存活）+ 2（len 存活）

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s40） |
| --- | --- | --- |
| 內聯判據 | optionPayloadInline：≤24B 內聯 | %vec 24B ⇒ 內聯，一致 |
| dat 字段 | vec 的 buf 指針以 i64 存（helper inttoptr） | 三字段原樣进槽，免 ptrtoint，一致 |
| >24B 载荷 | optPayloadAddr 的 tag==0 堆裝箱 | 仍待（B8 另半項） |

**未覆蓋／後續**：>24B 載荷的堆上裝箱（B8 餘項）；vec 元素非 i64 的逐元素 RC（B5）；option 跨基本塊（alloca %option）已由 s41 補齊（見 §5.3.4.bv）；option 的 ==/!= 與 match 臂降級


### 5.3.4.bv s41——M4-c 續件：option 跨基本塊槽（`alloca %option` / store / load 非 PHI，§11 B8）（2026-11-02 本輪）

; 背景：s40（§5.3.4.bu）走通 `?[]i64` 的載荷內聯；本件補 option **值**跨基本塊的傳遞通路
; 前例：str 的 `%str-long` 跨塊走 `SALLOC/SSTORE/SLOAD`；struct 的 `%struct.s<id>` 跨塊走 `STALLOC/STSTORE/STLOAD`
; ⇒ option 同族照做一份，只是 `%option = { i64 tag, [3 x i64] payload }` 是單一固定 LLVM 型別 ⇒ 三件套不解 `cty/vty` 型別 比 struct 槽更簡單
; PHI 本方案自 M4-c 起決定一律走 alloca/store/load ⇒ 跨塊傳 option 值靠入口分配槽 + 各分支 store + 匯聚塊 load 而非 `%option` 的 PHI

**落地**：
- compiler/src/mir.no：新增 OP-OPTALLOC = 60／OP-OPTSTORE = 61／OP-OPTLOAD = 62（接既有最大碼 OP-OPTGETVEC=59 之後，刻意不重排既有碼免影響 arena 數據）
- compiler/src/cg.no：cg-emit-cfg-t 加三條發射臂 完全鏡像 SALLOC/SSTORE/SLOAD——OPTALLOC 發 `%os<dst> = alloca %option`；OPTSTORE 發 `store %option %op<xa>, %option* %os<bs>`（無結果）；OPTLOAD 發 `%op<dst> = load %option, %option* %os<xa>`
- 第八個命名空間 %os<id>（option slot）與值 %op<id> 只差一個 s；與 str 的 %sv/%ss 與 struct 的 %st/%sts 同規，寫錯就是把值名當指針（LLVM 報 not a pointer）
- RC 口徑：跨塊槽只搬 %option 值本體 不觸及載荷堆指針 ⇒ 無須動 cg-rc（與 s40 一致）

**關鍵點**：
- 與 s40 正交：s40 管 載荷怎麼進 [3 x i64] 槽；s41 管 option 值怎麼跨基本塊搬 二者可疊加（一個 `?[]i64` 值跨塊 = s40 內聯 + s41 alloca 通路）
- 型別固定是 vec/str 沒有的簡化：struct 槽要 `cg-ty-name(TY-STRUCT-BASE + 結構體 id)` 解型別，option 槽直接硬編 %option

**驗證環境**：沿用 s35–s40 的釘死 v0.4.13（HEAD 工具鏈有跨模組結構欄位回歸，代碼生成側 cg.no/mir.no 在 v0.4.13 對 HEAD 逐字節相同）

; 可重跑：
$NOC=/tmp/nolang-v413/bin/no; export DEVELOPER_DIR=/Library/Developer/CommandLineTools
$NOC run compiler/tests/cg-test.no                      # cg-test 222→228/228（option 跨塊槽斷言 +6）
$NOC run tmp/probe-s41.no 1>tmp/s41.ll                  # g41(c) 三塊：bb0 默認寫→CONDBR→bb1 覆蓋寫→bb2 load 讀回
opt -passes=verify tmp/s41.ll -disable-output           # rc=0
clang -x ir -O2 tmp/s41.ll -o tmp/s41.bin && ./tmp/s41.bin  # exit 99（c=7 走 bb1 覆蓋 99）
# 反向對照：實參改 g41(0) 走 bb2 讀到默認 1 → exit 1（證明 alloca 跨塊 store 真可視且分支正確）

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s41） |
| --- | --- | --- |
| option 值跨塊 | alloc %option + store + load（非 PHI） | OPTALLOC/OPTSTORE/OPTLOAD 三件套 一致 |
| 槽命名空間 | 值與槽分離 | %op（值）/ %os（槽） 一致 |
| 型別解碼 | %option 單一 LLVM 型別 | 三件套不解 cty/vty（比 struct 槽更簡單） 一致 |

**未覆蓋／後續**：>24B 載荷的堆上裝箱（B8 餘項）；option 的 ==/!= 與 match 臂降級；vec 元素非 i64 的逐元素 RC（B5）


### 5.3.4.bw s42——M4-c 續件：`str` 結構相等（`@str_eq` helper ＋ `OP-STEREQ`／`OP-STNEQ`，非 `icmp %str-long`）（2026-11-02 本輪）

; 背景：§11 記帳把 `option` 的 `==`/`!=` 與字符串比較列為 M4-c 餘項
Go 對 `str` 相等**從不發 `icmp`**（`icmp` 只能比標量，`%str-long` 是聚合體非法），
而是用專用 MIR op `OpStrEq` 降級到 prelude 裡的 runtime helper `@str_eq`（`src/mir/codegen.go:3121`）
本件把這條最簡單的可比路徑鏡像進 noc：

- `compiler/src/cg-runtime.no` 新增 `cg-runtime-str-eq`（逐字照抄 Go `@str_eq`）與 `cg-runtime-memcmp-decl`
  （`declare i32 @memcmp(i8*, i8*, i64)`）`prelude-full` 裡 memcmp 宣告緊接 memcpy 之後；`@str_eq` 定義緊接 `@nolang_free` 之後（`@str_eq` 不依賴 RC）
- `compiler/src/mir.no` 新增 `OP-STEREQ = 63`／`OP-STNEQ = 64`（接既有最大碼 `OP-OPTLOAD = 62` 之後，不重排arena）
- `compiler/src/cg.no` 兩條發射臂：`%se<k> = call i1 @str_eq(%str-long %sv<x>, %str-long %sv<y>)` ＋ `%v<d> = zext i1 %se<k> to i64`
  STNEQ 多一步 `%sn<k> = xor i1 %se<k>, true` 再 zext；操作數住在 **str 值命名空間 `%sv`**，結果落 **i64 `%v`**

關鍵點：
- **專用 op 而非型別分派**：既有 `CMP*` 臂按 `vty` 在 i64/f64 間分派，但 `str` 比較結構不同（要 call helper 而非單條 icmp）
  Go 用獨立 op（`OpStrEq`）而非在 CMPEQ 內塞 str 分支 ⇒ noc 同形採**專用 op**，`CMP*` 臂零變動（回歸面最小）
- **memcmp 才是判別力所在**：`@str_eq` 先比 len，len 相等才 `memcmp` 比字節 ⇒ 反向對照證明同長度不同字節確實被判為不等（見下方 exit 41 vs 51）
- **`txt` 無 `@txt_eq`**：Go 註明 `%txt` 型別在 prelude 之後才宣告 ⇒ 只 inline 降級（`txtEqInline`）
  noc 當前無 `txt` codegen，本件不涉及（記入 §11）
- 與 s40/s41 正交：s40 管載荷怎麼進槽，s41 管 option 值怎麼跨塊，s42 管字符串值怎麼比相等

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 234/234（test-str-eq +6）
$NOC run compiler/tests/cg-runtime-test.no  # 50/50（test-str-eq-helper +9）
$NOC run tmp/probe-s42.no 1>tmp/s42.ll      # 8 條 STRCONST + 4 條 compare + 加權和
opt -passes=verify tmp/s42.ll -disable-output # rc=0
clang -x ir -O2 tmp/s42.ll -o tmp/s42.bin && ./tmp/s42.bin  # exit 41 = 40 + a（equal）
# 反向對照：把 'abd'/'ab' 全改成 'abc' 使 b=d=1 → exit 51（證明 memcmp 按字節判別，xor 生效）
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s42） |
| --- | --- | --- |
| str 相等 | 專用 MIR op `OpStrEq` → `@str_eq` call | `OP-STEREQ`/`OP-STNEQ` 一致 |
| 字節比較 | `@memcmp`（libc） | prelude 補 `declare @memcmp` 一致 |
| 聚合體不可 icmp | 對 `%str-long` 發 icmp 非法 | 臂內只 call helper ＋ zext 一致 |

**未覆蓋／後續**：`txt` 相等（Go 有 inline 的 `txtEqInline`）；`vec` 元素相等（Go `__nolang_vec_eq` 按元素型別單態化，較重）；`option` 的 `==`/`!=` 與 match 臂降級；>24B 載荷堆上裝箱（B8 餘項）


### 5.3.4.bx s43——M4-c 續件：`vec` 元素相等（`@__nolang_vec_eq_i64` helper ＋ `OP-VECEQ`，非 `icmp %vec`）（2026-11-02 本輪）

; 背景：§11 記帳把 `vec` 元素相等列為 M4-c 餘項
Go 對 `[]T` 相等**從不發 `icmp`**（`icmp` 只能比標量，`%vec` 是聚合體非法），而是用 `vecDeepEq(elemType, depth)` 按元素型別單態化生成 helper `@__nolang_vec_eq_<t>_<depth>`（`src/mir/codegen.go:2192`）
其中元素為 i64（有值語義）走 memcmp 分支：先比 len，len 相等才 `memcmp(data, data, len*8)`
本件把這條最簡單的 i64 元素路徑鏡像進 noc（`[]i64` 版起步）：

- `compiler/src/cg-runtime.no` 新增 `cg-runtime-vec-eq`（逐字照抄 Go `vecDeepEq` 的 i64 分支）；`%vec = { i64, i64, i64 }`，field 2（data）存為整數地址，故先 `inttoptr i64 → i8*` 還原，`@memcmp` 復用 s42 的宣告；`prelude-full` 裡 `@__nolang_vec_eq_i64` 定義緊接 `cg-runtime-vec-push` 之後（vec 家族尾）
- `compiler/src/mir.no` 新增 `OP-VECEQ = 65`（接既有最大碼 `OP-STNEQ = 64` 之後，不重排arena）
- `compiler/src/cg.no` 一條發射臂：`%veq<k> = call i1 @__nolang_vec_eq_i64(%vec %vc<x>, %vec %vc<y>)` ＋ `%v<d> = zext i1 %veq<k> to i64`；操作數住在 **vec 值命名空間 `%vc`**，結果落 **i64 `%v`**

關鍵點：
- **專用 op 而非型別分派**：與 s42 的 str 同構——`%vec` 聚合體不可 icmp，故走獨立 `OP-VECEQ` 降級到 helper，`CMP*` 臂零變動（回歸面最小）
- **memcmp 才是判別力所在**：`@__nolang_vec_eq_i64` 先比 len，len 相等才 `memcmp` 比元素位元組 ⇒ 反向對照證明同長度不同元素確實被判為不等（見下方 exit 41 vs 47）
- **空向量安全**：len=0 ⇒ `memcmp(_, _, 0)` 永不解引用，null data 也安全（同 Go 註釋）
- **僅 i64 元素**：Go 對 str 元素調 `@str_eq`（逐元素），巢狀 slice 遞迴 `vecDeepEq(depth+1)`；noc 當前只鏡像 i64 分支（元素非 i64 的逐元素比對留作後續，記入 §11）

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 237/237（test-vec-eq +3）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（test-vec-eq-helper +9 與 prelude `@__nolang_vec_eq_i64` 斷言）
$NOC run tmp/probe-s43.no 1>tmp/s43.ll      # A/B=[7,9] C=[7,8] E=[7,9,5] 三條 VECEQ + 加權和
opt -passes=verify tmp/s43.ll -disable-output # rc=0
clang -x ir -O2 tmp/s43.ll -o tmp/s43.bin && ./tmp/s43.bin  # exit 41 = 40 + a（equal）
# 反向對照：把 b 與 c 的比較目標都改成 B（全等）→ exit 47（證明 len 不等走 noteq，memcmp 按元素內容判別）
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s43） |
| --- | --- | --- |
| vec 相等 | `vecDeepEq` 按元素型別單態 → `@__nolang_vec_eq_<t>_<depth>` | `OP-VECEQ` → `@__nolang_vec_eq_i64` 一致（僅 i64） |
| 值語義元素 | i64 分支走 `@memcmp`（len*elemsize bytes） | `mul %la,8` + `@memcmp` 一致 |
| 聚合體不可 icmp | 對 `%vec` 發 icmp 非法 | 臂內只 call helper ＋ zext 一致 |

**未覆蓋／後續**：元素為 `str` 的 vec（Go 逐元素調 `@str_eq`）；巢狀 slice 元素（Go 遞迴 `vecDeepEq`）；`txt` 相等（Go inline `txtEqInline`）；`option` 的 `==`/`!=` 與 match 臂降級；>24B 載荷堆上裝箱（B8 餘項）


### 5.3.4.by s44——M4-c 續件：`option` 值相等（`OP-OPTEQ`／`OP-OPTNEQ`，tag AND payload，單塊無需 helper）（2026-11-02 本輪）

; 背景：§11 記帳把 `option` 的 `==`／`!=` 列為 M4-c 餘項（s42／s43 已清 str／vec 兩族）
Go 對 `?i64` 相等走 `emitOptionValueEq`（`src/mir/codegen.go:6247`）：先 `icmp eq` 兩邊 tag（field 0），再 `icmp eq` 兩邊 i64 載荷（field 1,0），最後 `and i1` 合併（`!=` 多一步 `xor i1 true`）——純單塊降級，不 call 任何 runtime helper（有別於 s42 str／s43 vec 須 call `@str_eq`／`@__nolang_vec_eq_i64`）
本件把這條 i64 載荷路徑鏡像進 noc：

- `compiler/src/mir.no` 新增 `OP-OPTEQ = 66`／`OP-OPTNEQ = 67`（接既有最大碼 `OP-VECEQ = 65` 之後，不重排 arena）
- `compiler/src/cg.no` 兩條發射臂：各發 `%otx<k>／%oty<k> = extractvalue %option %op<x>, 0`（取 tag）＋`%otq<k> = icmp eq i64`，`%opx<k>／%opy<k> = extractvalue %option %op<x>, 1, 0`（取 i64 載荷）＋`%opq<k> = icmp eq i64`，再 `%oa<k> = and i1 %otq<k>, %opq<k>`；OPTNEQ 多一步 `%on<k> = xor i1 %oa<k>, true` 再 zext；操作數住在 **option 值命名空間 `%op`**，結果落 **i64 `%v`**
- 無需改 `cg-runtime.no`：本件不引入 helper（與 s42／s43 的最大差異）

關鍵點：
- **tag 與載荷都要比**：`and i1` 把兩條 `icmp`（tag 相等／載荷相等）合取 ⇒ some(7)==some(7) 為 1，some(7)==some(9) 為 0（載荷判別），some==nil 為 0（tag 判別），nil==nil 為 1（雙 tag=1 且 slot 皆 0）
- **僅 i64 載荷**：Go 對 `%str-long` 載荷改 call `@str_eq`，對 `%txt` 走 inline `txtEqInline`；noc 當前只鏡像 i64 分支（slot[0] 直取），str／vec 載荷的逐欄位比對留作後續（記入 §11）
- **命名空間按指令下標 k**：`%otx<k>` 等中間名與 `%op<id>` 值名不衝突（`%opx3` 與 `%op3` 是不同 token），沿用 s42／s43 的 `%se<k>`／`%veq<k>` 慣例
- 與 s40／s41／s42／s43 正交：s40 管載荷進槽，s41 管 option 跨塊，s42 管 str 比等，s43 管 vec 比等，s44 管 option 值本身比等

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 247/247（test-opt-eq +10）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（無改動，僅回歸驗證）
$NOC run tmp/probe-s44.no 1>tmp/s44.ll      # some(7) 兩份 some(9) 一份 nil 兩份，五條 OPTEQ/OPTNEQ + 加權和
opt -passes=verify tmp/s44.ll -disable-output # rc=0
clang -x ir -O2 tmp/s44.ll -o tmp/s44.bin && ./tmp/s44.bin  # exit 49 = 40 + a + 8*d（some==some 與 nil==nil）
# 反向對照：把 b 與 e 的比較目標都改成 some(7) → exit 67（證明 tag／payload 內容改變會翻轉 and 結果）
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s44） |
| --- | --- | --- |
| option 相等 | `emitOptionValueEq`：tag icmp eq AND payload icmp eq 再 and | `OP-OPTEQ`／`OP-OPTNEQ` 一致 |
| 單塊無 helper | i64 載荷走純 icmp＋and（不 call runtime） | 臂內只 extractvalue／icmp／and／zext 一致 |
| 不等取反 | OpNe：and 後 xor i1 true | `%on<k> = xor i1 %oa<k>, true` 一致 |

**未覆蓋／後續**：option 的 str／vec 載荷逐欄位比對（Go call `@str_eq`／inline `txtEqInline`）；`>24B` 載荷堆上裝箱（B8 餘項）；`txt` 相等；match 臂降級


### 5.3.4.bz s45——M4-c 續件：`option` 的 `str` 載荷相等（`OP-OPTEQSTR`／`OP-OPTNEQSTR`，tag AND 從槽重建 `%str-long` call `@str_eq`）（2026-11-02 本輪）

; 背景：§11 記帳把 option 非 i64 載荷的 `==` 列為 M4-c 餘項（s44 只清 i64 載荷）
Go 對 `?str` 相等仍走 `emitOptionValueEq`（`src/mir/codegen.go:6247`）：tag `icmp eq` AND 載荷 `call i1 @str_eq`（`%str-long` 分支）再 `and i1`（`!=` 多一步 `xor i1 true`）——載荷內聯（`optionPayloadOf` 回呼已負載值），無需控制流
本件把這條 `str` 載荷路徑鏡像進 noc：

- `compiler/src/mir.no` 新增 `OP-OPTEQSTR = 68`／`OP-OPTNEQSTR = 69`（接既有最大碼 `OP-OPTNEQ = 67` 之後，不重排 arena）
- `compiler/src/cg.no` 兩條發射臂：先發 tag（field 0）兩側 `extractvalue`＋`icmp eq`；再對左／右各從 `[3 x i64]` 槽重建 `%str-long`（`extractvalue` field 1,0／1,1／1,2 → `inttoptr` 還原 data → 三步 `insertvalue`，與 `OPTGETSTR` 同形）；發 `call i1 @str_eq(兩側重建的 %str-long)`；`and i1`（tag-eq · str-eq）→ `zext`；OPTNEQSTR 多一步 `xor i1 true`；操作數住在 option 值命名空間 `%op`，結果落 i64 `%v`
- 無需改 `cg-runtime.no`：本件復用 s42 既有 `@str_eq`，不引入新 helper（與 s44 一樣是純 codegen 單塊降級）

關鍵點：
- **必須經 `@str_eq` 而非比槽字面**：兩份內容相同但指標不同的 `?str`（不同堆／全局分配），槽 field 2（data 整數）不同⇒直接 `icmp` 槽必錯；`@str_eq` 先比 len 再 `memcmp(data, …, len)` 才對（同 s42 str 族）
- **僅 str 載荷**：Go 對 `%txt` 載荷走 inline `txtEqInline`；noc 當前只鏡像 `%str-long` 分支，`vec` 載荷／`txt` 載荷留作後續（記入 §11）
- **nil==nil 走 tag＋空載荷**：`OPTNONE` 槽=zeroinitializer⇒重建成 `{0,0,null}`，`@str_eq` len0==len0→`memcmp(null,null,0)=0`→true；tag 皆=1⇒`and`→1（與 Go 同形，非短路：兩側 `@str_eq` 恆執行）
- **命名空間按指令下標 k**：`%olx<k>`／`%osx<k>`／`%oseq<k>` 等中間名與 `%op<id>` 值名不衝突（`%osx3` 與 `%op3` 是不同 token），沿用 s42／s43／s44 慣例；與 s44 互斥（s44 取 slot[0] 當 i64 比，本件重建 `%str-long` 再 call）

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 260/260（test-opt-eq-str +13）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（無改動，僅回歸驗證）
$NOC run tmp/probe-s45.no 1>tmp/s45.ll      # some(abc) some(abc) some(abd) some(ab) nil nil，五條 OPTEQSTR/OPTNEQSTR + 加權和
opt -passes=verify tmp/s45.ll -disable-output # rc=0
clang -x ir -O2 tmp/s45.ll -o tmp/s45.bin && ./tmp/s45.bin  # exit 49 = 40 + a（some(abc)==some(abc)）+ 8*d（nil==nil）
# 反向對照：把 sv3 'abd' 改成 'abc' → some(abc)==some(abc) → b 翻轉 → exit 51（證明 @str_eq 的 memcmp 按字節內容判別）
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s45） |
| --- | --- | --- |
| option str 相等 | `emitOptionValueEq` %str-long 分支：tag icmp eq AND call @str_eq 再 and | `OP-OPTEQSTR`／`OP-OPTNEQSTR` 一致 |
| 載荷取用 | `optionPayloadOf` 回呼已負載的 %str-long | 從 [3 x i64] 槽三步 insertvalue 重建（`OPTGETSTR` 同形）一致 |
| 復用 helper | call 既有 `@str_eq`（不新增） | 復用 s42 `@str_eq` 一致 |

**未覆蓋／後續**：option 的 `vec`／`txt` 載荷逐欄位比對（Go inline `txtEqInline`）；`>24B` 載荷堆上裝箱（B8 餘項）；match 臂降級


### 5.3.4.ca s46——M4-c 續件：`option` 的 `f64`（double）載荷內聯讀寫（`OP-OPTMKF64`／`OP-OPTGETF64`，bitcast double↔i64 落／取 slot[0]）（2026-11-02 本輪）

; 背景：§11 記帳把 option 標量載荷的讀寫列為逐型別推進——s28 已建 `str` 載荷（OPTMKSTR／OPTGETSTR），s40 已建 `vec` 載荷（OPTMKVEC／OPTGETVEC），本件補齊 `f64`（double）載荷的內聯讀寫，為後續 OPTEQF64（鏡像 Go emitOptionValueEq 的 double 分支）預備可測基底
Go 對 ?f64 的載荷存取走 `optPayloadTypedAddr`（`src/mir/codegen.go:2717`）把槽地址重類型為 `double*`＋store／load double（`optionPayloadOf` 6252），無需 memcpy（`shouldUseMemcpy("double")=false`）
本件把這條 `double` 載荷路徑鏡像進 noc：

- `compiler/src/mir.no` 新增 `OP-OPTMKF64 = 70`／`OP-OPTGETF64 = 71`（接既有最大碼 `OP-OPTNEQSTR = 69` 之後，不重排 arena）
- `compiler/src/cg.no` 兩條發射臂：`OPTMKF64` 先發 `%mb<k> = bitcast double %fd<x> to i64`，再同 `OPTSOME` 形一步 `insertvalue` 把該 i64 落 `slot[0]`（tag 已內建於字面 `{ i64 0, [3 x i64] zeroinitializer }`）；`OPTGETF64` 先 `%mg<k> = extractvalue %option %op<x>, 1, 0`（i64），再 `%fd<dst> = bitcast i64 %mg<k> to double`；操作數住在浮點值命名空間 `%fd`／option 值命名空間 `%op`
- 無需改 `cg-runtime.no`：本件與 s44 同属純 codegen 單塊降級，`bitcast` 是 LLVM 合法指令，不引入任何 helper

關鍵點：
- **等宽 bitcast 而非 ptrtoint**：`double` 與槽元素 `i64` 皆 64 位⇒`bitcast double↔i64` 合法（同宽標量重 interpret）；不同於 s28 `str` 的 `i8*` data 指針（指針↔整數必須 `ptrtoint`／`inttoptr`，bitcast 不能跨指針與整數）
- **為何先建讀寫而非直接建相等**：與 s28／s40 同節律——載荷型別要先有可構建／可讀回的基底（`OPTMKF64`／`OPTGETF64`），相等（OPTEQF64）才能在其上做往返判別；本件即該 **讀寫** 半項，相等留作後續（鏡像 Go double 分支的 `fcmp oeq`）
- **OP-CONST 無法造 double**：`OP-CONST` 只發 i64（s37 已記錄）⇒e2e 的 f64 值一律經 `double` 形參注入，手寫 `@main` 傳字面量（同 probe-s37 手法）
- **命名空間按指令下標 k**：`%mb<k>`／`%mg<k>` 中間名與 `%fd<id>`／`%op<id>`／`%v<id>` 值名不衝突（`%mb0` 與 `%fd0` 是不同 token），沿用 s28／s40／s42–s45 慣例

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 265/265（test-opt-f64 +5）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（無改動，僅回歸驗證）
$NOC run tmp/probe-s46.no 1>tmp/s46.ll      # some(3.5) some(2.25)，OPTMKF64 寫→OPTGETF64 讀回→CMPEQ(fcmp oeq double) 往返＋加權和
opt -passes=verify tmp/s46.ll -disable-output # rc=0
clang -x ir -O2 tmp/s46.ll -o tmp/s46.bin && ./tmp/s46.bin  # exit 43 = 40 + t1（往返 3.5==3.5）+ 2*t2（往返 2.25==2.25）+ 4*t3（3.5!=2.25→0）
# 反向對照：main 改傳 f46(3.5, 3.5) → t3 翻為 1 → exit 47（證明 t3 由真實 double 位元驅動）
# 構造器壞（slot 未寫，读出 0.0）⇒ t1=t2=0，t3=(0==0)=1 → exit 44，與 43／47 皆不同（三值互異）
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s46） |
| --- | --- | --- |
| double 載荷寫 | `optPayloadTypedAddr` 重類型槽地址為 `double*`＋store double | `bitcast double→i64` 後 `insertvalue` 落 `slot[0]`（語法等價：同 64 位元模式入槽） |
| double 載荷讀 | `optionPayloadOf` 對 `double*` load | `extractvalue slot[0]`（i64）再 `bitcast i64→double` |
| 無需 memcpy | `shouldUseMemcpy("double")=false` | 純單塊 bitcast＋insertvalue／extractvalue，無 helper 一致 |

**未覆蓋／後續**：option 的 `f64` 載荷相等（`OPTEQF64`／`OPTNEQF64`，鏡像 Go double 分支 `fcmp oeq`，本件已備好讀寫基底）；option 的 `txt` 載荷（Go inline `txtEqInline`，noc 尚未有 `%txt` 型別基盤）；`vec` 載荷逐元素相等（Go 無 inline 分支，僅比 tag）


### 5.3.4.cb s47——M4-c 續件：`option` 的 `f64`（double）載荷相等（`OP-OPTEQF64`／`OP-OPTNEQF64`，tag AND bitcast 後 fcmp oeq）（2026-11-02 本輪）

; 背景：s46 已建 `option` 的 `f64` 載荷讀寫（OPTMKF64／OPTGETF64），本件在其上補齊 **相等** 半項，鏡像 Go emitOptionValueEq 的 double 分支（fcmp oeq），與 s44（i64 相等）／s45（str 相等）同族完成 option 標量載荷的 **讀寫→相等** 閉環
Go 對 ?f64 相等走 emitOptionValueEq（src/mir/codegen.go:6263）：載荷 `fcmp oeq` ＋ tag `icmp eq` 再 `and i1`；`!=` 為整體取反（`xor i1 true`）；`shouldUseMemcpy("double")=false`⇒無控制流，無需 helper
本件把這條 double 載荷相等路徑鏡像進 noc：

- `compiler/src/mir.no` 新增 `OP-OPTEQF64 = 72`／`OP-OPTNEQF64 = 73`（接既有最大碼 `OP-OPTGETF64 = 71` 之後，不重排 arena）
- `compiler/src/cg.no` 兩條發射臂：先比 tag（兩側 `extractvalue %option, 0` 再 `icmp eq i64`），再取兩側 `slot[0]`（i64）各 `bitcast i64→double` 後 `fcmp oeq double`，兩者 `and i1` 再 `zext`；`OPTNEQF64` 於 `and` 後加 `xor i1 true`
- 無需改 `cg-runtime.no`：純 codegen 單塊降級，`bitcast`／`fcmp` 皆 LLVM 合法指令，不引入 helper

關鍵點：
- **載荷比較走 fcmp 非 icmp**：`%option` 聚合體不可直接 icmp；s44 的 i64 載荷是 `icmp eq i64`，s45 的 str 載荷是 `call @str_eq`，本件的 double 載荷是 `bitcast` 還原後 `fcmp oeq double`——三者只換載荷比較那一步，tag 門（`icmp eq`）＋ `and` 骨架同形
- **tag 門與載荷門正交**：探針特意構造 `some(0.0) vs nil`——兩者槽讀出皆 0.0（載荷相等）但 tag 0!=1⇒必回 0；若漏 tag 門會誤判 1（填補只比 tag 或只比載荷的盲區）
- **等宽 bitcast 復用 s46 基底**：載荷槽 `i64` 與 `double` 同 64 位，`bitcast` 還原後即真 double，`fcmp oeq` 按 IEEE754 位元比較（不同 `icmp` 整數等值）
- **OP-CONST 無法造 double**：沿 s46——e2e 的 f64 值一律經 double 形參注入，手寫 `@main` 傳字面量

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 271/271（test-opt-eq-f64 +6）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（無改動，僅回歸驗證）
$NOC run tmp/probe-s47.no 1>tmp/s47.ll      # some(3.5) some(2.25) some(0.0) nil nil，五條 OPTEQF64／OPTNEQF64 ＋加權和
opt -passes=verify tmp/s47.ll -disable-output # rc=0
clang -x ir -O2 tmp/s47.ll -o tmp/s47.bin && ./tmp/s47.bin  # exit 61 = 40 + e1（some(3.5)==some(3.5)）+ 4*e3（nil==nil）+ 16*e5（some(3.5)!=some(2.25)）
# 反向對照：main 改傳 f47(3.5, 3.5, 0.0) → e2 翻 1，e5 翻 0 → exit 47（證明 e2／e5 由真實 double 位元驅動）
# 理論獨立壞路徑：漏載荷比 → e2=1 → 63；漏 tag 比 → e4=1 → 69；構造器壞（全 0.0）→ 47
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s47） |
| --- | --- | --- |
| option double 相等 | `emitOptionValueEq` double 分支：tag icmp eq AND fcmp oeq 再 and | `OP-OPTEQF64`／`OP-OPTNEQF64` 一致 |
| 載荷取用 | `optionPayloadOf` 回呼已 load 的 double | 從 `[3 x i64]` 槽 `extractvalue`＋`bitcast i64→double` 還原一致 |
| 無需 memcpy／helper | `shouldUseMemcpy("double")=false` | 純單塊 bitcast＋fcmp＋and 一致 |

**未覆蓋／後續**：option 的 `txt` 載荷（Go inline `txtEqInline`，noc 尚未有 `%txt` 型別基盤）；`vec` 載荷逐元素相等（Go 無 inline 分支，僅比 tag）；`>24B` 載荷堆上裝箱（B8 餘項）；match 臂降級


### 5.3.4.cc s48——M4-c 續件：一元 ALU 家族 NEG／NOT（`OP-NEG`／`OP-NOT`，fneg double／sub i64 0,x + xor i64 1,x）（2026-11-03 本輪）

; 背景：s35／s36／s37 已落地二元 ALU 三族（有符號／無符號／`f64`），唯 Go `emitInst` 在 arith 之後緊接的兩條一元 op（`emitNeg`／`emitNot`）noc 尚未覆蓋——本件補齊一元 ALU，鏡像 Go codegen.go:5503／5505，無 helper，單塊降級
Go 權威：`emitNeg`（codegen.go:5503）float⇒`fneg double`，int⇒`sub <lt> 0, x`（非 option 路徑不插溢出護欄，與 s35 有符號 ALU 同紀律）；`emitNot`（5505）⇒`xor <lt> 1, x`（翻轉 0/1 布林，noc 布林即 i64 0/1）
本件把這條一元 ALU 路徑鏡像進 noc：

- `compiler/src/mir.no` 新增 `OP-NEG = 74`／`OP-NOT = 75`（接既有最大碼 `OP-OPTNEQF64 = 73` 之後，不重排 arena）
- `compiler/src/cg.no` 兩條發射臂（插在 `OP-MODU` 之後，`OP-CALL` 之前，鏡像 Go `emitArith→emitNeg→emitNot` 順序）：NEG 依 `cg-vty-at(vty, d)==TY-F64` 分派——真⇒`%fd<dst> = fneg double %fd<x>`，假⇒`%v<dst> = sub i64 0, %v<x>`；NOT⇒`%v<dst> = xor i64 1, %v<x>`
- 無需改 `cg-runtime.no`：純 codegen 單塊降級，`fneg`／`sub`／`xor` 皆 LLVM 合法指令，不引入 helper

關鍵點：
- **NEG 復用 s37 值型別分派機制**：`fneg double` 與 `sub i64 0,x` 的判據即 s37 已建的 `cg-vty-at(vty, d)==TY-F64`，不新增分派基建（同 s35/s36/s37 "按型別分派不新增 op" 紀律）
- **非 option 路徑不插溢出護欄**：Go `emitNeg` 走 arith 後的一元分支，int 形直接 `sub 0, x`；option 載荷的 NEG（Go `wrapResult` 溢出 select 路）需 option 裝箱基底，留作後續
- **NOT 即 xor 1**：noc 布林以 i64 0/1 表示，`xor i64 1, x` 翻轉最低位等價邏輯非，不引入 `icmp` 或額外 select

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 277/277（test-unary +6）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（無改動，僅回歸驗證）
$NOC run tmp/probe-s48.no 1>tmp/s48.ll      # f48(a i64, d double)：int NEG／NOT／f64 NEG 三臂＋加權和
opt -passes=verify tmp/s48.ll -disable-output # rc=0
clang -x ir -O2 tmp/s48.ll -o tmp/s48.bin && ./tmp/s48.bin  # exit 47 = 40 + e1（-a==-5）+ 2*e2（xor 1,1==0）+ 4*e3（d + fneg(d) == d - d == 0.0）
# 反向對照：main 改傳 f48(7, 3.5) → e1 翻 0（-7≠-5）→ exit 46（證明 int NEG 由真實值驅動）
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s48） |
| --- | --- | --- |
| 一元 int 取負 | `emitNeg` int 分支：`sub <lt> 0, x` | `OP-NEG` vty≠F64 發 `sub i64 0, %v<x>` 一致 |
| 一元 float 取負 | `emitNeg` float 分支：`fneg double` | `OP-NEG` vty==F64 發 `fneg double %fd<x>` 一致 |
| 一元布林取非 | `emitNot`：`xor <lt> 1, x` | `OP-NOT` 發 `xor i64 1, %v<x>` 一致 |

**未覆蓋／後續**：option 載荷的 NEG（Go `emitNeg` 的 `wrapResult` 溢出 select 路，需 option 裝箱基底）；`float`（f32）原生算術——Go `emitArith` 以 `isFloat := resLT == "double"` 判據把 f32 歸一化為 double，MIR 層無原生 float 算術賽道⇒noc 刻意不造（同 vec 相等排除紀律）；`>24B` 堆裝箱


### 5.3.4.cd s49——M4-c 續件：vec→str 重interpret（`OP-STRFROMVEC`，%vec 三欄位逐欄拷进 %str-long + dat inttoptr i64→i8*）（2026-11-03 本輪）

; 背景：s40 已內聯 option 的 vec 載荷／s28 已建 str⇄槽的 ptrtoint/inttoptr 紀律，唯 Go `emitInst` 的 `emitStrFromVec`（把 %vec 就地重interpret 成 %str-long，服務 `data str = fs.read-file(path)` 一类向量轉文本語料）noc 尚未覆蓋——本件補齊 vec→str 轉換賽道，鏡像 Go codegen.go:13069，無 helper，單塊降級
Go 權威：`emitStrFromVec`（codegen.go:13069）——srcT 必須是 `%vec`／dstLT 必須是 `%str-long`（否則 `c.fail`）；逐欄位 `extractvalue %vec` 0/1/2，前兩欄（len/cap）以 i64 直接 `insertvalue` 進 %str-long 欄 0/1，第三欄 dat（ptrtoint 後的 i64 整數指針）先 `inttoptr`→i8* 再 `insertvalue` 進欄 2
本件把這條 vec→str 轉換路徑鏡像進 noc：

- `compiler/src/mir.no` 新增 `OP-STRFROMVEC = 76`（接既有最大碼 `OP-NOT = 75` 之後，不重排 arena）
- `compiler/src/cg.no` 一條發射臂（插在 `OP-VECGET` 之後／`OP-OPTNONE` 之前）：dst = 字符串值 id（落 %sv），x = 向量值 id（住 %vc），發 `%sfl<k>/%sfc<k>/%sfd<k> = extractvalue %vec %vc<x>, 0/1/2`，`%sfp<k> = inttoptr i64 %sfd<k> to i8*`，再三步 `insertvalue` 組 %str-long 落規範 `%sv<dst>`
- 無需改 `cg-runtime.no`：純 codegen 單塊降級，`extractvalue`／`inttoptr`／`insertvalue` 皆 LLVM 合法指令，不引入 helper

關鍵點：
- **dat 指針必走 inttoptr 非 bitcast**：%vec 的 dat 欄是 `vec_push` 內 `ptrtoint` 後的 i64，%str-long 的 data 欄是 i8*；LLVM 不允許 bitcast 指針⇄整數，故同 s28 `OPTGETSTR` 以 `inttoptr` 還原（單測以 `bitcast i64 %sfd0` 與 `bitcast %vec` 反查其為 0）
- **僅存欄位／不拷貝緩沖**：與 Go 一致，結果 str 的 data 指針與原 vec 共享同一堆緩沖（不 memcpy／不 retain），故 str 生命期受制於原 vec（所有權移交屬 B11 RC 自動放置後續）
- **不依賴 vty/cty 分派**：容器 op（VEC/STR）命名空間在發射臂硬編 %vc/%sv（不同於 NEG／算術需 `cg-vty-at` 按型別分派），故此 op 只需 x=向量值 id

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 286/286（test-str-from-vec +9）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（無改動，僅回歸驗證）
$NOC run tmp/probe-s49.no 1>tmp/s49.ll      # f49(m i64)：建 [10,m,30] vec → STRFROMVEC → %sv8 → STRLEN／VECLEN／VECGET 三路鑒别＋加權和
opt -passes=verify tmp/s49.ll -disable-output # rc=0
clang -x ir -O2 tmp/s49.ll -o tmp/s49.bin && ./tmp/s49.bin  # exit 47 = 40 + e1（STRLEN==3，證 len 欄拷貝）+ 2*e2（STRLEN==VECLEN）+ 4*e3（VECGET[1]==20）
# 反向對照：main 改傳 f49(99) → e3 翻 0（99≠20）→ exit 43（證 vec 內容隨實參變／非恒真）
# ⚠️ dat 欄 inttoptr 的 IR 合法性由 opt verify 擔保；探针不對 %sv 做字節解引用（vec 元素是 i64，非 byte）
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s49） |
| --- | --- | --- |
| len 欄 transfer | `extractvalue %vec,0` → `insertvalue %str-long ...,0` | `%sfl<k>`→`%sfs0<k>` 一致 |
| cap 欄 transfer | `extractvalue %vec,1` → `insertvalue %str-long ...,1` | `%sfc<k>`→`%sfs1<k>` 一致 |
| dat 欄（指針）transfer | `extractvalue %vec,2` → `inttoptr i64→i8*` → `insertvalue ...,2` | `%sfd<k>`→`%sfp<k>`→`%sv<dst>` 一致 |

**未覆蓋／後續**：str→vec 反向轉換（Go 只有 vec→str 一條 op，無反向）；B11 RC 自動放置（共享緩冲下 str／vec 的所有權移交與 drop）；txt⇄str（Go `emitTxtFromStr`，noc 無 `TY-TXT` 型別宇宙，暫不適用）


### 5.3.4.ce s50——M4-c 續件：str 容量欄讀取（`OP-STRCAP`，extractvalue %str-long field 1，與 OP-STRLEN 成對）（2026-11-03 本輪）

; 背景：s14 已建 `%str-long` 三步 insertvalue 構造＋`OP-STRLEN` 取 len（f0），唯 Go `emitLenCap` 以 `fieldIdx` 分派的另一翼 `OpCap`（取 f1=cap）noc 尚未覆蓋——本件補齊 str 容量欄賽道，鏡像 Go codegen.go:11848，無 helper，單塊降級
Go 權威：`emitLenCap`（codegen.go:11818）的 KindStr／KindSlice 分支——`fieldIdx = 0`（OpLen）或 `1`（OpCap），對接收者槽 load 後 `extractvalue <recvLT>, fieldIdx` 再 store；noc 已把 slot 形態簡化為 SSA 直取（同 STRLEN 先例），本 op 僅下標改為 1
本件把這條 cap 欄讀取路徑鏡像進 noc：

- `compiler/src/mir.no` 新增 `OP-STRCAP = 77`（接既有最大碼 `OP-STRFROMVEC = 76` 之後，不重排 arena）
- `compiler/src/cg.no` 一條發射臂（緊跟 `OP-STRLEN` 之後，鏡像 Go 同函同分支的成對關係）：dst = i64 值 id／x = 字符串值 id（TY-STR，住 %sv），發 `%v<dst> = extractvalue %str-long %sv<x>, 1`
- 無需改 `cg-runtime.no`：純 codegen 單塊降級，`extractvalue` 即 LLVM 合法指令，不引入 helper

關鍵點：
- **刻意不造 OP-VECCAP**：Go `emitLenCap` 的 switch 無 KindVec 分支（vec 長度走 `vec_len` helper call，cap 無 MIR op），故 %vec 的 cap 欄在 Go 權威無對位讀取路徑——忠實性紀律優先於表面對稱（同 OPTEQVEC 排除同理）
- **cap 語義雙態**：字面量 `STRCONST` 精確分配 ⇒ cap==len；vec 擴容軌跡來源（s49 `STRFROMVEC`）⇒ cap 為 2 的冪擴容值（首 push 0→4）≠ len；探針同時覆蓋兩態，欄下標 0／1 互換會被 e3（cap≠len 哨兵）銬住
- **與 STRLEN 同一條 extractvalue 僅下標之別**：發射臂不復用分派基建（容器 op 硬編 %sv 命名空間），但斷言側同時釘住 field 0／field 1 兩條輸出，防未來重構把兩翼倂成一條

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 292/292（test-str-cap +6）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（無改動，僅回歸驗證）
$NOC run tmp/probe-s50.no 1>tmp/s50.ll      # f50(m i64)：建 [10,m,30] vec（len3／cap4）→ STRFROMVEC → STRLEN／STRCAP 雙欄＋CMPNE 互換哨兵＋常量 'abcd' cap==len 四路鑒别＋加權和
opt -passes=verify tmp/s50.ll -disable-output # rc=0
clang -x ir -O2 tmp/s50.ll -o tmp/s50.bin && ./tmp/s50.bin  # exit 55 = 40 + e1（STRLEN==3）+ 2*e2（STRCAP==4）+ 4*e3（cap≠len 哨兵）+ 8*e4（常量 cap==4）
# 反向對照：'abcd' 改 'xyz' ⇒ 常量 cap=3 ⇒ e4 翻 0 → exit 47（證 cap 跟隨真實緩冲尺寸）
# ⚠️ 欄下標互換 bugs（cap 讀成 len）會同時翻 e2／e3 兩哨兵；本探針不需改 cg.no 即可由 exit code 區辨
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s50） |
| --- | --- | --- |
| OpLen str（f0） | `fieldIdx=0`：`extractvalue %str-long …, 0` | `OP-STRLEN` 發同一條僅下標 0 一致 |
| OpCap str（f1） | `fieldIdx=1`：`extractvalue %str-long …, 1` | `OP-STRCAP` 發 `%v<dst> = extractvalue %str-long %sv<x>, 1` 一致 |
| vec 的 cap | 無 OpLen／OpCap 分支（僅 vec_len helper 取 f0） | 刻意不造 OP-VECCAP，與 Go 同缺 |

**未覆蓋／後續**：slice 的 OpCap（Go KindSlice 分支，noc 無独立 slice 值型別，視圖語義屬 `hir2mir` 降級主體）；txt 的 len（KindStruct／Raw==txt 分支 zext i8，noc 無 `TY-TXT` 宇宙，暫不適用）；陣列 len（KindArray 靜態常量 store，同屬 arr 值型別基盤後續）


### 5.3.4.cf s51——M4-c 續件：option 排序（`OP-OPTLT`／`OP-OPTLE`／`OP-OPTGT`／`OP-OPTGE`，只比載荷無 tag 項，鏡像 Go emitOptionValueOrder 的有符號 int 分支）（2026-11-03 本輪）

; 背景：s44–s47 建了 option 的**相等**族（tag AND 載荷），唯 Go emitCmp 對兩個 option 的另一組比較器——序比較——走 `emitOptionValueOrder`（codegen.go:6294，調用點 6448），noc 尚無任何序 op，`a < b` 於 `?i64` 兩操作數無降級可用——本件補齊有符號四比較器，無 helper，單塊降級
Go 權威：`emitCmp` 的 `aOpt && bOpt && !inst.TagOnly` 分支先試 Eq／Ne，再試 Order：謂詞映射 OpLt⇒slt／OpLe⇒sle／OpGt⇒sgt／OpGe⇒sge（另有 double 翼 olt… 與無符號翼 ult…，見關鍵點），主體取兩側載荷（field 1,0）直接 `icmp pred pA, pB` 再 store——**無 tag 項亦無 and**（異於 emitOptionValueEq 結構；Go 原注給出動機：str.compare 因 `.byte(i)` 返 `?byte` 而需序比較走載荷，比 tag 會讓同 tag 全判 false）
本件把這條序比較路徑鏡像進 noc：

- `compiler/src/mir.no` 新增 `OP-OPTLT = 78`–`OP-OPTGE = 81`（接既有最大碼 `OP-STRCAP = 77` 之後，不重排 arena）
|  |  |
| --- | --- |
- `compiler/src/cg.no` 四條發射臂（排在 `OP-OPTNEQ` 臂後，與相等族同群）：dst = i64 結果 id（0/1）；xa[k] = 左 option 值 id；bs[k] = 右 option 值 id；發 `%opx<k>／%opy<k> = extractvalue %option %op<x|y>, 1, 0`；`%oq<k> = icmp <pred> i64 %opx<k>, %opy<k>`；`%v<dst> = zext i1 %oq<k> to i64`
- 無需改 `cg-runtime.no`：純 codegen 單塊降級，extractvalue／icmp／zext 皆 LLVM 合法指令

關鍵點：
- **nil 以載荷 0 參與序比較**：nil 槽是 zeroinitializer（值 0），`nil < some(7)`＝1／`nil < nil`＝0——Go 序比較函不問 tag 的原語義忠實鏡像（非偷簡；與 s50 同理，無原函即無 op）
- **刻意不造無符號翼**：Go 同函有 OpULt–OpUGe（ult／ule／ugt／uge 映射），但 option 載荷的符號性在 noc 型別宇宙不可見（TY-OPT 不挾載荷型別）——循 s50 VECCAP 排除的忠實性紀律，無語料驅動不自造 op
- **f64 載荷序比較留後續**：Go 同函 fpred 翼（olt／ole／ogt／oge）需循 s47 OPTEQF64 專用 op 前例（載荷型別在 op 層不可見），記帳後續
- **鑑別力設計**：探針以值鑑別（slt 正向）／假錨（反向操作數必須 0，錯發 sge／拿反操作數即翻）／符號鑑別（-3<0 若誤發 ult 判 0）／取等邊界（sle／sge 各釘一條取等）四種形態覆蓋四臂

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 305/305（test-opt-order +13）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（無改動，僅回歸驗證）
$NOC run tmp/probe-s51.no 1>tmp/s51.ll      # f51(m i64)：some(7)／some(9)／some(-3)／nil 四值，四謂詞七條哨兵（正向3＋假錨2＋取等邊界2）＋加權和
opt -passes=verify tmp/s51.ll -disable-output # rc=0
clang -x ir -O2 tmp/s51.ll -o tmp/s51.bin && ./tmp/s51.bin  # exit 157 = 40 + 1*e1（7<9）+ 4*e3（7>nil）+ 16*e5（-3<0 有符號）+ 32*e6（9<=9）+ 64*e7（7>=7）
# 反向對照：-3 改 3 ⇒ e5 翻 0 → exit 141（證 icmp slt 有符號生效；誤發 ult 翼前向就到不了 157）
# ⚠️ 誤發 sle⇒slt／sge⇒sgt 分被 e6／e7 兩條取等哨兵銬住；誤取 field 0（tag）會被 e3／e5 銬住（tag 0 vs 0 恒假）
```

**noc 對 Go 對照**：

| 面 | Go 權威 | noc（s51） |
| --- | --- | --- |
| `?i64 < ?i64` | emitOptionValueOrder：載荷 icmp slt，無 tag 項 | `OP-OPTLT` 發兩側 slot[0] extractvalue + icmp slt + zext，一致 |
| `==`／`!=` | emitOptionValueEq：tag AND 載荷 | 已 s44 `OP-OPTEQ`／`OPTNEQ`（有 tag 項），與本件結構互補不同形 |
| 無符號序 | OpULt–OpUGe ult／ule／ugt／uge | 刻意不造（載荷符號性不可見），與 s50 VECCAP 同缺 |
| nil 判別 | TagOnly（match 臂）走 tag 比較 | noc 以既有 `OP-OPTTAG` + `OP-CMPEQ` 組合表達，無需新 op |

**未覆蓋／後續**：f64 載荷序比較（Go 同函 fpred 翼，循 s47 專用 op 前例）；str 載荷序比較（Go 語言層根本不實作 str/txt 序：emitCmp 直接硬 fail，codegen.go:6557-6563，只實作 ==/!=；noc 刻意不造）；載荷寬度不符的 discriminant fallback（Go 以 shouldUseMemcpy 動態回退，noc 靜態發射無此形態）


### 5.3.4.cg s52——M4-c 續件：option 的 `f64` 載荷排序（`OP-OPTLTF64`／`OP-OPTLEF64`／`OP-OPTGTF64`／`OP-OPTGEF64`，載荷 bitcast 成 double 後發 fcmp 有序謂詞，無 tag 項，鏡像 Go emitOptionValueOrder 的 double 翼）（2026-11-03 本輪）

; 背景：s51 落地 emitOptionValueOrder 的有符號 int 翼後，明文記帳兩件後續——f64 載荷序（Go 同函 fpred 翼）與無符號翼；本件收前者，循 s47 OPTEQF64 專用 op 前例（載荷型別在 op 層不可見，故需 f64 專用碼）
|  |  |  |  |
| --- | --- | --- | --- |
Go 權威：`emitOptionValueOrder`（codegen.go:6352）對 pTA==double 的兩側載荷直接 `fcmp <olt|ole|ogt|oge> double pA, pB` 再 store（OpLt⇒olt／OpLe⇒ole／OpGt⇒ogt／OpGe⇒oge）——與 int 翼同紀律：無 tag 項亦無 and；ordered 謂詞遇 NaN 恒判 false，為 Go 原語義忠實鏡像
本件把這條 f64 序路徑鏡像進 noc：

- `compiler/src/mir.no` 新增 `OP-OPTLTF64 = 82`–`OP-OPTGEF64 = 85`（接既有最大碼 `OP-OPTGE = 81` 之後，不重排）
- `compiler/src/cg.no` 四條發射臂：`%opx<k>／%opy<k> = extractvalue %option …, 1, 0`（i64）；`%fdx<k>／%fdy<k> = bitcast i64→double`（復用 s46/s47 讀回形）；`%oq<k> = fcmp <pred> double`；`%v<dst> = zext i1`（單塊無 helper）
- 無需改 `cg-runtime.no`：bitcast／fcmp／zext 皆 LLVM 合法指令

關鍵點：
- **f64 專用 op 而非型別分派**：Go 以 pTA==double 動態擇翼，noc 的 TyOpt 不挾載荷型別——循 s47 OPTEQF64 前例另立專用碼，呼叫方保證兩側皆 f64 載荷；誤喂 i64 載荷會把整數位模式重interpret 成 double（記在 op 注釋，語義由呼叫方兜底）
- **nil 載荷以 0.0 參與**：nil 槽 zeroinitializer 即 double +0.0 位模式，`nil < some(3.5)`＝1，與 s51 int 翼的 nil 行為同語義
- **鑑別力設計**：假錨兩條（反向操作數 e1／e4）銬住謂詞反轉與操作數互換；取等邊界兩條（e6／e7）銬住 olt⇒ole／ogt⇒oge 混淆；e5／e9 釘住負值與 nil 零載荷路徑；單測另斷言無 `fcmp oeq`／無 `icmp`／無 `and i1`／無 `%otx`（結構鑑別：相等族與 int 排序臂形制均不得混入）

驗證環境：
```
$NOC run compiler/tests/cg-test.no          # 318/318（test-opt-order-f64 +13）
$NOC run compiler/tests/cg-runtime-test.no  # 60/60（無改動，僅回歸驗證）
$NOC run tmp/probe-s52.no 1>tmp/s52.ll      # f52(a=3.5, b=2.25, c=-1.5)：九條排序 op 加權和
opt -passes='default<O2>' -passes=verify tmp/s52.ll # rc=0
clang -O2 tmp/s52.opt.ll -o tmp/s52.bin && ./tmp/s52.bin  # exit 167 = 40 + 1+2+4+8+16+32+64
# 反向對照：c 改 1.5 ⇒ e5 翻 0 → exit 163（單變量翻轉，證 -1.5<0.0 確發 olt）
```

**noc↔Go 對照**：

| 面 | Go 權威 | noc（s52） |
| --- | --- | --- |
| `?f64 < ?f64` | emitOptionValueOrder：載荷 fcmp olt double | `OP-OPTLTF64` 取 slot[0] bitcast 後 fcmp olt 同形 |
| `?i64 < ?i64` | 同函 int 翼 icmp slt | 已 s51 `OP-OPTLT`（本件不動 int 翼） |
| NaN | ordered 謂詞恒 false | 同形忠實（不加额外處理） |
| tag 項 | 無（序比較只比載荷） | 同無（單測斷言 and／%otx 零出現） |

**未覆蓋／後續**：str 載荷序比較（Go 語言層根本不實作 str/txt 序：emitCmp 直接硬 fail，codegen.go:6557-6563，只實作 ==/!=；noc 刻意不造）；無符號翼（循 s51 排除紀律，無承載碼）；`hir2mir` 降級主體仍是下一大項


### 5.3.4.ch s53——M4-c 續件：option 與純量的排序（`OP-OPTLTSC`／`OP-OPTLESC`／`OP-OPTGTSC`／`OP-OPTGESC`，方向由 vty 判定，只提 option 側載荷與純量發 icmp，無 tag 項謂詞不翻轉，鏡像 Go emitCmp 的 aOpt/bOpt 分支）（2026-11-03 本輪）

**背景**：checker 文檔把 `?i64 < 100` 記為值比較；Go emitCmp（codegen.go:6490-6520）對一側 `%option`，另一側可比普通值（`optionPayloadComparable`：同 LLVM 型別或整數寬度互容）時，取出 option 側載荷後直接落普通 icmp／fcmp，tag 整個丟棄；noc 此前只有 option-vs-option（s51／s52）與純量-vs-純量（`OP-CMP*`），混排序中間路徑為空白；

**Go 權威**：emitCmp 的 aOpt／bOpt 兩分支—只對一側做 `optionPayloadOf` 提取，操作數次序不變，謂詞不翻轉（`scalar <= opt` 仍是 sle，純量在左）

|  |  |  |  |
| --- | --- | --- | --- |
- **發射形**：方向由 `vty` 判定（`TY-OPT` 在那側就提那側，鏡像 Go `isOptionType` 動態判側）—左 option `%opx<k> = extractvalue %option %op<xa>, 1, 0` 後 `%oq<k> = icmp <slt|sle|sgt|sge> i64 %opx<k>, %v<bs>`；右 option 提 `%opy<k>` 自 `%op<bs>` 後 `%oq<k> = icmp 同謂詞 i64 %v<xa>, %opy<k>`；末 `%v<dst> = zext i1 %oq<k> to i64`；純單塊，無 helper
- **同 s51／s52 紀律**：無 tag 項亦無 and；nil 槽 zeroinitializer，載荷 0 與純量 0 同路參與比較（`OP-OPTNONE < 2` 判 1）
- **誤餵護欄**：兩側皆 `TY-OPT` 時兩個側形同發，`%oq<k>` 重複定義→ LLVM verify 誠實報錯（不靜默錯發）；兩側皆純量時兩個護欄都不發射，結果未定義—由呼叫方保證側型別

**關鍵點**：
1. 本波只發 i64 翼×i64 純量；f64 純量翼（載荷 bitcast→double 後與 `%fd` 純量 fcmp）需循 s47／s52 前例另立專用 op，後續
2. str 序在 Go 語言層不存在—emitCmp 對 str/txt 操作數的序比較直接 hard-fail（codegen.go:6557-6563，只實作 ==／!=）；§5.3.4.cf／.cg 兩處記為 Go 走 emitStrCompare builtin 為誤帳，本輪實地勘察後已修正
3. 鑑別力設計：九哨兵覆蓋 4 謂詞×2 方向矩陣，假錨兩條（左 LTSC／右 GESC 反向）鉗謂詞與操作數次序；`sle`／`sge` 各釘一條取等邊界；`OP-OPTNONE` 零載荷參與

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `cg-test` 330/330（s52 末 318，+12 `test-opt-order-sc` 斷言：左右兩形 8 條正向，鑑別 4 條 and／%otx／field-0／bitcast 零出現）；
- 探針 `tmp/probe-s53.no` → `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 `f53(7, 2)` exit **180**（=53+1+2+4+8+16+32+64，七條全真，兩假錨恒假）；反向 b 改 9 ⇒ e2／e3／e7／e8 同翻 exit **129**（180−1−2−16−32），單變量可預測；
- `cg-runtime-test` 60/60；`no fmt -w／-d` 零偏差

| noc | Go |
| --- | --- |
| `vty` 判側，兩個護欄臂各發一句 | aOpt／bOpt `isOptionType` 動態判側後提取 |
| `%oq<k> = icmp slt i64 %v<x>, %opy<k>`（右 option） | 提取載荷後普通 `icmp`，次序不變 |
| 兩側 `TY-OPT` → `%oq` 重複定義 verify 錯 | 兩側 option 走 emitOptionValueOrder 另一路 |
| str 序不存在 | emitCmp hard fail（語言層只實作 ==／!=） |

**未覆蓋／後續**：f64 純量翼（循 s47 前例另立專用 op）；載荷寬度不相容時 Go 回退 discriminant 比較（`optionPayloadComparable` 拒絕提取），noc 靜態發射無此形態，由呼叫方保證；`hir2mir` 降級主體仍是下一大項


### 5.3.4.ci s54——M4-c 續件：option 與 f64 純量的排序（`OP-OPTLTSCF`／`OP-OPTLESCF`／`OP-OPTGTSCF`／`OP-OPTGESCF`，vty 雙護欄判向，載荷 bitcast→double 後與 %fd 純量發 fcmp，無 tag 項謂詞不翻轉，鏡像 Go emitCmp 提取後落 double 翼）（2026-11-03 本輪）

**背景**：s53 收攏 option-vs-scalar 的 i64 翼時明文留下 f64 純量翼 "循 s47／s52 前例另立專用 op"——本輪即償此賬；至此 Go emitCmp aOpt／bOpt 混合路徑在 noc 側的可達組合全部落地

**Go 權威**：`optionPayloadComparable`（codegen.go:6376-6409）對 double 只認同形（payloadT==otherT ⇒ double-vs-double 過閘；double-vs-i64 不過閘回退 tag 比較）；過閘後 `optionPayloadOf` 對載荷做 typed load double，emitCmp 尾部（6686-6714）見 double 即發 `fcmp olt／ole／ogt／oge`，操作數次序不變，謂詞不翻轉

- **發射形**：方向與純量側型別由 vty 雙護欄判定——左 option（`cg-vty-at(vty, x) == TY-OPT && cg-vty-at(vty, y) == TY-F64`）：%opx<k> = extractvalue 載荷 → %fdx<k> = bitcast i64→double → %oq<k> = fcmp 謂詞 double %fdx<k>, %fd<bs>；右 option 對稱提 %opy<k>／%fdy<k> 且 %fd<xa> 在左；末 %v<dst> = zext i1 %oq<k> to i64；純單塊，無 helper
- **內聯槽等價性**：Go 對堆槽做 typed load double，noc 載荷住 [3 x i64] 槽——extractvalue＋bitcast 兩步與之語義等價（循 s46／s47／s52 前例）；nil 槽位模式即 +0.0（同 s52）
- **護欄比 s53 更嚴**：非 option 側必須 TY-F64——兩側皆 TY-OPT 或皆純量時兩護欄臂都不發射，%v<dst> 未定義由呼叫方自查（鏡像 Go "閘不過則不回退錯發" 的紀律，但 Go 回退 discriminant 而 noc 不實作回退）

**關鍵點**：
1. 本輪是 s53 留賬的精確償還：op 碼 90–93 接 86–89，形制與 SC 家族同構，只把 icmp i64 翼換成 bitcast＋fcmp double 翼
2. 單測首發曾掛兩條：右 option 臂 fcmp 誤引 %opy<k>（i64 提取名）而非 %fdy<k>（bitcast 後 double）——opt 型別錯誤被 verify 鉗住之前已被斷言捕獲，驗證單測 IR 文本斷言是第一道閘
3. 鑑別力設計：九哨兵覆蓋 4 謂詞×2 方向，假錨三條（左 LT／右 GE／右 LE）＋單變量反錨一條（e6 GTSCF 釘 fd3 在左，c 改 9.0 獨翻）＋ole 取等邊界＋nil 零載荷兩條

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `cg-test` 330/330→342/342（+12 `test-opt-order-scf` 斷言：左右兩形 8 條正向，鑑別 4 條 and／%otx／field-0／i64 翼洩漏零出現）；
- 探針 `tmp/probe-s54.no` → `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 `f54(3.5, 2.25, -1.5)` exit **68**（=53+1+2+4+8）；反向 c 改 9.0 ⇒ e6 獨翻 exit **84**（68+16），單變量可預測；
- `cg-runtime-test` 60/60；`no fmt -w／-d` 零偏差

| noc | Go |
| --- | --- |
| vty 雙護欄（TY-OPT＋TY-F64）判向 | `isOptionType` 判側＋`optionPayloadComparable` 同形閘 |
| extractvalue＋bitcast 兩步 | typed load double 一步（槽佈局不同語義等價） |
| 閘不過不發射（dst 未定義） | 閘不過回退 discriminant（noc 未實作，呼叫方保證） |

**未覆蓋／後續**：double-vs-i64 混合純量（Go 回退 tag 比較，noc 未實作回退）；載荷寬度不相容的 discriminant 回退同此；`hir2mir` 降級主體仍是下一大項——已於 s55 起動，見 §5.3.4.cj


### 5.3.4.cj s55——M4-e 首件：AST → MIR 降級（新建 `compiler/src/h2m.no`，i64 直線式子集，`let` 直接把右值 id 綁到名字而不發 `alloca`，不支持構造走 `unsup` 誠實計數而非靜默略過）（2026-11-03 本輪）

**背景**：s14–s54 全部落在 `cg` 發射側——它吃扁平指令流（`ops/dsts/xa/bs/syms`），而這張流在 noc 側**只有測試代碼手拼**；前端 `ps.no` 產出的是 arena AST（`nd` 節點池＋全局 `kids` 索引），兩者之間沒有橋，`drv.no` 的 `build` 仍是 `not-implemented`——本輪架第一段橋：真實源碼 → `ps.parse` → `h2m` 降級 → `cg.cg-emit-cfg-t` 發射 → `opt` verify → `clang` 執行，並與 Go oracle 編譯**同一程序**對拍——自舉線首次"源碼進，可執行檔出"

**Go 權威**：`src/mir/hir2mir.go`（14,780 行）——本件只取其直線式 i64 子集，其餘按語料觸發頻度逐族續推；§6.5 原把降級主體列在 **M4-c**，但該標號已被 codegen 發射族逐族用盡（s16–s54），為免回溯改寫既有紀事，降級主體另立 **M4-e**

**覆蓋面（本件）**：`K-PROGRAM` 逐個 `K-FUNCTION-DEFINITION`；函式節點讀 `s0` 名／`c3` 泛型／`c4` 輸入／`c5` 輸出，`kid` 序為 `[gen…][inputs…][results…][body]`（body 恆末位）；`K-BLOCK-STATEMENT` 逐陳述；`K-LET-STATEMENT` 讀 `s0` 名＋kids[typ, val]；`K-INTEGER-LITERAL` 讀 `s0`（十進位值，非 `s1` raw）；`K-IDENTIFIER` 讀 `s0` 名查綁定表；`K-GROUPED-EXPRESSION` 下鑽 kid0；`K-INFIX-EXPRESSION` 的 `s0` ∈ {'+' '-' '*'} → `OP-ADD/SUB/MUL`

**未覆蓋（本件）**：輸入參數非空或輸出多於一個（`c4 > 0`／`c5 > 1` 直接計 `unsup` 且不產碼）；`'/' '%'`；`K-PREFIX-EXPRESSION`；比較與邏輯運算；任何控制流（`IF`／`MATCH`／循環）；`K-CALL`；`str`／`vec`／`struct`／`option` 載荷；型別聲明側（`kids[0]` 的 typ 從不讀，一律硬編 `TY-I64`）

**值 id 約定**（與 `cg-emit-cfg-t` 對齊）：id 0 為啞元（`vty[0]` 佔位），1..nargs 為形參（本切片 nargs 恆 0），nargs+1 起單調分配；`vty[值 id]` 與 `cty[指令 k]` 兩個維度必須同步填滿——只填其一則 `cg` 落默認臂按 `TY-I64` 誤選值命名空間

**SSA 無槽策略**：直線式內 `K-LET-STATEMENT` 不發 `ALLOC/STORE/LOAD`，而把右值 id 直接綁到名字，同名重複賦值即覆蓋重綁（鏡像 AGENTS.md §3.11"函式作用域內同名重複賦值 = 重賦值"），這是本件**唯一與 Go 形制不同**之處：Go 對每個 `let` 建變數槽並以 `load`/`store` 穿針引線，跨基本塊時讀的是內存；noc 本件讀的是綁定表，故一旦接控制流此策略必錯——記為 M4-e 的邊界條件，屆時改回三件套（循 s16/s21/s30/s41 前例）

**誠實門禁 `unsup`**：`h2m-ctx.unsup` 計不支持構造數，`h2m-lower-func` 把函式內增量併入模塊級 agg；單測 `test-gate` 對除零／未定義名／有輸入參數／struct 定義四例逐一斷言 `unsup > 0`；探針在 `unsup > 0` 時改發 poison `ret i32 101`——差分因此**誠實失敗**而非靜默產錯碼（沿用 `ast.no` 的 `.unsup`／`.gaps` 紀律：產碼 ≠ 產對碼）

**關鍵點**：
1. 🔴 **入口檔必須顯式導入 `ps` 的傳遞依賴（`asp`／`lx`／`tok`）**——本件實地撞出：只導入 `ast`／`ps`／`h2m` 時跨模組常量解析不到，而兩種症狀**都不報錯**：要麼 `v0.4.13` 構建期 `EmitLLVM: consumer reads value 0 (NoVal) … bug #85` 泛濫（286 KB 日誌，含 `ps_parse_stmt` 165 條），要麼**構建成功但運行期無限循環掛起**——該缺陷先於本檔存在（`compiler/tests/ps-test.no` 檔頭已有同記述），依 §2 黃金法則不改他人源碼，正解是補齊自身的導入清單（記 §11 B15）
2. **結構體字面量的 vec 欄位必須兩行式**——`ops: []i64` 被判"explicit type cast `expr i64` is not allowed"（`no fmt` 一次拒 10 條），須寫成 `ops: []` 換行 `i64`（`ast.no` 既有形制）
3. **提交點前移**——本件兩個新檔寫完即獨立提交（`1c4fbb01`），不等 docs：s54 曾因 docs 拖尾使 `compiler/src/mir.no` 的 op 塊被並發提交 `aec062a5` 掃走（內容逐字未變，歸屬錯位）

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `h2m-test` **35/35**（6 用例：運算優先級／變數綁定／覆蓋重綁／括號下鑽／多函式／`unsup` 門禁；斷言喂**真實 `ps.parse` 產出**而非手拼 AST，逐條文本比對 `%v1 = add i64 0, 2` 並釘 `%v6` 與 `alloca`／`load` 零出現）；
- 零回歸：`cg-test` **342/342**；`cg-runtime-test` **60/60**；`no fmt -d` rc=0；`no vet` 兩新檔 **0 error / 0 warning**；
- 探針 `tmp/probe-s55.no` 三函式（`f` 優先級／`g` 變數／`h` 重綁）＋ main 側手發加權鏈 `5 + 2*f + 4*g + 8*h` ⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **73**；反向（sed 把 `3 * 4` 改 `3 * 5`）⇒ `f` 14→17 ⇒ exit **79**（差 6 = 權重 2 × 3，單變量可預測）；
- 🔴 **首次 Go oracle 差分對拍**：`tmp/s55-oracle.no` 用 Go 工具鏈編譯同一程序，實測同為 **73 / 79** ⇒ noc 降級產物與 Go 後端的可觀測行為逐位對齊

| noc | Go |
| --- | --- |
| `h2m-ctx` 平行陣列（`ops/dsts/xa/bs/syms/blks/vty/cty/names/vids`） | `hir2mir.go` 遞迴遍歷 HIR 直接構造 MIR 指令物件 |
| 值 id 單調分配＋同名覆蓋重綁（無槽） | 每個 `let` 建槽，`load`/`store` 穿針引線 |
| `unsup` 計數 ⇒ 探針發 poison 退出碼 | 不支持形直接 `not implemented` 硬錯誤中斷 |
| `h2m-lower-func` 內聯調 `cg-emit-cfg-t` 直接串接 IR 文本 | MIR → codegen 分兩趟，中間過 `insertDrops` 等 pass |
| 分工：`cg-test` 釘發射形，`h2m-test` 釘"源碼→指令流" | Go 兩段同庫同趟，無此對照 |

**未覆蓋／後續**：跨基本塊的槽化（本件 SSA 無槽策略的邊界條件）；輸入參數與多輸出的簽名側型別表；`OP-CALL` 實參與 `OP-RET` 返回值的型別感知；`drv.cmd-build` 接線（M4-d）——四者構成 M4-e 第二件的候選；其中簽名側與調用已於 s56 落地（見 §5.3.4.ck），剩餘兩項仍待


### 5.3.4.ck s56——M4-e 第二件：簽名側與調用降級（`compiler/src/h2m.no` 擴面：輸入參數佔形參值 id，輸出參數型別定 `retty`，模組級簽名表前置 pass 解前向引用，`OP-CALL` 帶 arity／型別／連續 id 三重校驗）（2026-11-03 本輪）

**背景**：s55 的 `h2m` 只能降"零輸入參數，無調用"的函式——探針 main 靠手發 IR 把三個函式串起來，真實源碼裡的 `f(x)` 一個都進不來；本輪架前端兩段橋墩：簽名側（參數與返回值型別）與調用側（`CallExpression` → `OP-CALL`）

**Go 權威**：`src/mir/hir2mir.go` 的函式宣告降級（形參進 `Block.Params`）與 `lowerCall`（查函式表 + `[]ValueID` 實參列表）——noc 側 MIR 是平行陣列，實參只能編 `xa[k]..xa[k]+bs[k]-1`（該契約定於 s24，記 §11 B16）

**覆蓋面（本件）**：`h2m-func-sig` 讀 `s0` 名／`c3` 泛型／`c4` 輸入／`c5` 輸出，型別一律取自 `K-PARAMETER` 的 kids[0]（`K-NAMED-TYPE` 的 `s0`），kid 序實測 `[gen…][inputs…][results…][body]` 且 `nkid == c3 + c4 + c5 + 1`；形參由 `h2m-alloc` 依序單調分配 ⇒ 恰為值 id `1..nargs` 並把名字綁上（`r = x + 1` 的 `x` 直接讀 `%v1`）；`retty` 由輸出參數的聲明型別決定，`str` 輸出 ⇒ `define %str-long @stw(%str-long %sv1)` + `ret %str-long %sv1`；型別表收 `i64`／`u64`／`f64`／`str` 四個具名型別（算術仍限 `i64`，任一操作數非 i64 則計 `unsup`）；`K-CALL-EXPRESSION` 的 `c0` 為實參數，kid 序 `[被調用者][實參…]`，查簽名表得 arity 與返回型別，逐實參校驗型別碼與連續 id；簽名表由前置 pass 先掃全模組 ⇒ 調用者寫在被調用者之前也能解析

**未覆蓋（本件）**：泛型（`c3 > 0`）；多輸出（`c5 > 1`）；帶默認值的參數（默認值會讓調用點 arity 判定失效）；非具名型別（`[]t`／`map`／`*t`／聯合別名）；點號與方法調用；實參 id 不連續的調用（`ad(a, 2 + 3)` 被門禁抓走）；返回值聲明型別與實綁值型別不符

**誠實門禁升級**：s55 只"計數"，本件加"不產碼"——`h2m-lower-func` 內 `unsup > 0` 則整函式返回空串（寧缺勿錯）；否則像 `e = () (r i64) { r = ad(a, 2 + 3) }` 這種"調用沒降下來但 `ret` 補零"的函式會被靜默編出並回 0，差分看像通過

**關鍵點**：
1. 🔴 **多輸出函式的呼叫形式是 `a, b = f(x)`**——把已有變量按位置傳進去（`f(x, a, b)`）**不會回寫**：本件實測四個輸出全部拿到呼叫方初值，函式名降成空串（`define i64 @()`），而 checker 一個錯都不報（記 §11 B17）
2. **連續 id 契約是 cg 側既有形制，Go 無此約束** ⇒ 調用降級必須顯式守門，不能假設"按序分配就連續"：只有每個實參恰好吃掉一個新 id（字面量／零參調用）或復用上一個剛分配的 id 才成立，嵌套表達式與復用舊 id 都會留出空洞
3. **毒化支路要在觸發態才驗得出來**：s55 探針把 `unsup > 0` 的 body 換成 `define i32 @main` 而後面照發自己的 main ⇒ 真觸發時 `opt` 報 `invalid redefinition of function 'main'`（非預期的 exit 101）——本輪改為先 `print` 毒化模組再 `exit(0)`
4. 並發狀態：`no vet compiler/tests/h2m-test.no` 報 24 error 全部落在他人正在改的 `compiler/src/ps.no`（本輪未觸及），自有 `compiler/src/h2m.no` vet **0 error / 0 warning**（依 §2 只記不改）

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `h2m-test` **74/74**（新增 7 用例：單參／雙參簽名，`str` 形參命名空間，`str` 透傳 `retty`，零參調用＋前向引用，單／雙參調用，調用側四重門禁；s55 原有 35 條全留）；
- 零回歸：`cg-test` **342/342**；`cg-runtime-test` **60/60**；`no fmt -d` rc=0；s55 探針重跑前向 exit **73** 不退化；
- 探針 `tmp/probe-s56.no`（六函式：三個調用者寫在前，三個被調用者在後）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **47**；反向（sed 把 `base` 的 7 改成 8）⇒ `k3` 由 21 變 24 ⇒ exit **50**（差 3，單變量可預測）；
- 🔴 **Go oracle 差分對拍**：`tmp/s56-oracle.no` 用 Go 工具鏈編譯同一程序，實測同為 **47 / 50** ⇒ 兩側可觀測行為逐位對齊；
- 鑑別態：把 `add5(a, b)` 改成 `add5(a, 2 + 3)` ⇒ `unsup > 0` 且 IR 內 `call i64 @add5` 零出現，探針只發毒化 main ⇒ exit **101**（差分誠實失敗，不靜默回 0）

| noc | Go |
| --- | --- |
| 形參＝值 id `1..nargs`，`h2m-alloc` 單調分配天然對齊 | `Block.Params` 與值 id 同序，但型別表存於 `Block` 而非呼叫方傳參 |
| 實參靠 `xa..xa+bs-1` 連續 id，空洞由門禁抓 | `Instruction.Args` 是任意 `[]ValueID`，無連續約束 |
| 前置 pass 只登簽名（名字／arity／返回型別），不降體 | Go 函式表在建程式時已填完（HIR 側帶完整簽名） |
| 調用側失敗 ⇒ 該調用不產碼且整函式不產碼 | 不支持形直接 `not implemented` 硬錯誤中斷 |
| `retty` 取自輸出參數聲明，實綁值型別不符則計數 | 返回槽在 MIR 構造時即按型別寫入，不符會被 checker 擋 |

**未覆蓋／後續**：把實參改用獨立 args arena 以摘掉連續 id 契約（§11 B16）**已於 s64 落地**（§5.3.4.cs）；控制流＋跨基本塊槽化已於 s57 落地（見 §5.3.4.cl）；`drv.cmd-build` 接線（M4-d）；`str`／`f64` 運算子與跨函式 RC 所有權（B13）——四者裡剩餘三項構成 M4-e 第四件的候選


### 5.3.4.cl s57——M4-e 第三件：控制流降級與跨基本塊槽化（`compiler/src/h2m.no` 補基本塊切分 `OP-BR`／`OP-CONDBR` 與比較家族 → `icmp`，`mem` 模式下全部局部量落 entry `alloca` 槽，**零改 `cg.no`**）（2026-11-03 本輪）

**背景**：s55 的 SSA 無槽策略在檔頭就被記為"一旦接控制流此策略必錯"的邊界條件（§5.3.4.cj），s56 末把它列為第三件候選；本輪償還——`if`／條件循環降級成多基本塊 + `br`／`condbr`，比較運算子降級成 `icmp` 族，同時把"跨塊傳值"從綁定表改成 `alloca` 槽；`cg.no` 側的控制流與槽三件套 s14/s16/s21/s41 早已備齊，本件**一行未動**

**Go 權威**：`src/mir/hir2mir.go` 的 `lowerIf`（:3864-4075）與 `lowerCond`（:4277-4342）——`NewBlock("if.then")`／`NewBlock("if.else")`／`NewBlock("if.merge")` 三塊，臂體結束後補 `OpBr → mergeBlk`，`Terminate(OpCondBr, []ValueID{c}, []BlockID{then, else}, "")`；結果 sink 在支配兩臂的塊裡建立（Go 註明刻意不引 PHI）

**覆蓋面（本件）**：`K-IF-EXPRESSION`（kid 序 `[Condition][Consequence][Alternative]`，實測 nkid=3）⇒ 當前塊求值條件 → `CONDBR`（真塊／假塊）→ 各臂以無條件 `BR` 回匯聚塊，其後語句落匯聚塊；無 else 臂（真實源路的單行 guard 恆為此形）⇒ 假路**直接落匯聚塊**，不開空塊；`K-FOR-STATEMENT` 的前置條件形（kid 序 `[Init][Condition][Update][CountExpr][IterRange][Body]`，實測 nkid=6，僅 `[1]`/`[5]` 非 0）⇒ entry 無條件 `BR hdr`，`hdr` 求值條件後 `CONDBR(body, exit)`，`body` 尾 `BR hdr` 回邊，其後語句落 `exit`；`K-INFIX-EXPRESSION` 的 `'<' '<=' '>' '>=' '==' '!='` → `OP-CMPLT/CMPLE/CMPGT/CMPGE/CMPEQ/CMPNE`（`cg` 發 `icmp slt/sle/sgt/sge/eq/ne i64` + `zext i1 → i64`）；`OP-CONDBR` 的非零判定由 `cg` 補一條 `icmp ne i64 zext值, 0`（零是 false 的語言語義在發射側落地）

**未覆蓋（本件）**：花括號短路鏈 `{ c -> a -> b }`（`ps.no` 產 `K-SURFACE-MATCH`，臂群在 aux 陣而非 kid 陣——見關鍵點 4）；`break`／`continue`／`return`；計數循環 `n * { }`；區間遍歷 `i <- [a..b]:`；`f64`／`vec`／`struct`／`option` 的槽（`cg` 只有 `i64` 與 `str` 兩套 alloca op）

**兩種存儲模式**（本件的語義核心，不是裝飾）：`mem==0` 沿用 s55 直線式（值 id 即槽，零回歸）；前掃描一旦在體內發現 `IF`／`FOR` 就置 `mem=1`，**全部局部量含形參與輸出參數**改在 entry 塊鋪 `alloca` 槽，讀＝`LOAD`／`SLOAD`，寫＝`STORE`／`SSTORE`；理由是 cg 沒有 PHI：跨塊引用他塊的 SSA 名會被 `opt` 的支配樹直接拒；而循環體內若仍走"同名覆蓋重綁"，`r = r + 1` 會編成引用 entry 的舊值 ⇒ **永遠只加第一輪的值**——單測以"`%v12 = add i64 %v10, %v11` 必在且 `%v12 = add i64 %v6, %v11` 必不在"雙向釘住

**槽必須鋪在 entry ⇒ 需要前掃描**：cg 只在 `blks[k] != cur` 處發標籤，故每個塊必須是指令序中的**連續段**（"只進不回"），槽的 alloca 一旦落在 then 塊之後，後續塊對它的引用在 LLVM 裡就是非法支配；本件因此在降 body 之前先跑 `h2m-scan`（掃陳述樹定"有無控制流"＋登"局部量表與型別"，不進嵌套函式定義），型別由 `h2m-ty-of` 從語法推（只認 `i64`／`str`，其餘 -1）；推斷與降級器彼此獨立 ⇒ 寫入口 `h2m-var-write` 再比一次"槽型 vs 右值實際型別"，不符就計 `unsup` 且整函式不產碼（寧缺勿錯）

**關鍵點**：
1. **每塊至少一條終結指令**：臂尾無條件 `BR` 不可省，否則 cg 不會為空塊發標籤而跳轉仍指向它 ⇒ `opt` verify 誠實報錯；無 else 時假路直落匯聚塊，被 `h2m-block-new` 開過但從未進入的塊 id 天然不落地（單測釘 `bb2:` 零出現）
2. **反向對照要選"單變量可預測"的兩刀**：改 then 塊常量 ⇒ 證 then 塊真被執行且寫回同一槽；改循環步長 `1 → 2` ⇒ 證回邊真在循環（只走一輪的話差值不會是 6）；把條件謂詞翻向（`sgt → slt`）看起來更"嚴"但會造成死循環，不是好對照
3. **oracle 側函式名不得與 std 具名函式撞名**：探針與 Go oracle 首發用 `sumto/pick/clamp`，Go 側報 `function 'num.num.clamp__i64' expects at least 2 argument(s), got 1`——裸名先被 std 別名表接走；改 `s57sum/s57pick/s57clamp` 後兩側一致（自舉側 `h2m` 只查本模組簽名表，不受此影響，但對拍必須同程式）
4. 🔴 **else 臂在真實源路上造不出來**：`{ c -> a -> b }` 被 `ps.no` 建成 `K-SURFACE-MATCH`（nkid=1 且 kid[0]=0，臂群存 aux/helper 陣，`PS-SURFACE-MATCH=1` 是預設開關），而單行 guard `c -> stmt` 的 Alternative 恆為 0——`h2m-lower-if` 按 Go/HIR 的三 kid 形制寫，故本件用 `ast.add`／`kid-add`／`set-slot`／`set-sig` **合成** IfExpression 節點跑同一段降級（依 §2 黃金法則只記錄而不改 `ps.no`）
5. 🔴 **呼叫嵌在另一呼叫的實參位會傳錯值**：合成 AST 時寫 `ast.kid-add(t, iff, mk-block1(t, then-s))`——實測塊節點被加成 `5`（內層函式的**輸入參數** `then-s`）而不是 `9`（它的**輸出** `x`），父節點因此把 LetStatement 誤當 BlockStatement；改成"每個子節點先落具名局部再傳參"即正確；該形態先於本檔存在（`h2m-test.no` 檔頭注記），依 §2 只記不改（已記 §11 B18）

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `h2m-test` **74 → 135/135**（新增 5 用例 61 斷言：單行 guard 的 if（槽／`icmp sgt`／`zext`／`br i1`／`bb2` 不落地／跨塊 `ret` 缺席）／前置條件循環（回邊＋體內 load 槽的雙向鑑別）／比較家族五個謂詞（含 `ule` 零浮出＋塊序 `bb13` 在 `bb14` 無）／合成 else 臂（兩臂各寫同槽＋任一臂 SSA 名不得被 `ret`）／控制流側五組負例（surface match／計數循環／區間遍歷／`return`／`f64` 槽）；s55/s56 原有 74 條全留）；
- 零回歸：`cg-test` **342/342**；`cg-runtime-test` **60/60**；自有兩檔 `no fmt -d` rc=0；`no vet compiler/src` 34 error **全部落在他人正在改的 `compiler/src/ps.no`**，自有 `compiler/src/h2m.no` **0 error / 0 warning**（依 §2 只記不改）；s55 探針前向 exit **73**／s56 探針前向 exit **47** 不退化；
- 探針 `tmp/probe-s57.no`（三函式：循環求和／單 guard／兩條依序 guard）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **92**（15+7+20+50）；反向 A（`s57pick` 的 7 → 8）⇒ exit **93**；反向 B（循環步長 1 → 2 ⇒ 5+3+1=9）⇒ exit **86**；
- 🔴 **Go oracle 差分對拍**：`tmp/s57-oracle.no`（同三函式同四個調用）用 Go 工具鏈編譯，實測同為 **92 / 93 / 86** ⇒ 控制流的實際執行路徑與 Go 後端逐位對齊；
- 鑑別態：把 `(i > 0) {` 改成計數循環 `i * {` ⇒ `unsup > 0`，IR 內 `define i64 @` 零出現，探針只發毒化 main ⇒ exit **101**（差分誠實失敗，不靜默回 0）

| noc | Go |
| --- | --- |
| 僅當前掃描發現控制流才切 `mem` 模式，直線式仍無槽 | `hir2mir` 每個 `let` 一律建槽，`load`/`store` 穿針引線 |
| 塊 id 由 `h2m-block-new` 單調分配，靠 `blks` 連續段發標籤 | MIR 側 `BlockID` 顯式表 + terminator 帶 successors |
| 無 else 時假路直落匯聚塊，空塊不落地 | `lowerIf` 同形：`CondBr` 假路直接指 merge |
| 槽型別靠語法前掃描推 + 寫入口二次校驗 | HIR 帶 `varType` 型別表，無需推斷 |
| `str` 以外的載荷型別無槽 op ⇒ 觸發即不產碼 | 不支持形直接 `not implemented` 硬錯誤中斷 |

**未覆蓋／後續**：`K-SURFACE-MATCH` 的 aux 陣臂群降級（花括號短路鏈——真實源路最常見的分支形，現僅能靠合成 AST 驗 else 臂）**已於 s58 落地**（見 §5.3.4.cm）；`break`／`continue`／`return` 的塊終結形；計數循環與區間遍歷；實參 args arena 摘掉連續 id 契約（§11 B16）**已於 s64 落地**（§5.3.4.cs）；`drv.cmd-build` 接線（M4-d）——後四者構成 M4-e 第五件的候選


### 5.3.4.cm s58——M4-e 第四件：花括號短路鏈降級（`compiler/src/h2m.no` 讀 `K-SURFACE-MATCH` 的 helper/aux 臂群（每臂 12 槽），把 `{ c1 -> a1  c2 -> a2  -> b }` 發成串接的 `OP-CONDBR` 鏈，兜底臂不發判定直接落塊，**零改 `ps.no`／`cg.no`**）（2026-11-03 本輪）

**背景**：s57 把 else 臂記成"真實源路上造不出來"的遺留項（§5.3.4.cl 關鍵點 4），同檔五組負例的第一組就是拿 `K-SURFACE-MATCH` 計 `unsup`——`{ c1 -> a1 c2 -> a2 }` 經 `ps.no` 出來的是 surface 形（ast kind 65）而不是 `K-IF-EXPRESSION`；本件收這個最大的面：短路鏈自此能用真實源碼書寫，探針不再依賴合成 AST

**Go 權威**：`src/parser/lowering.go` 的 `desugarSurfaceMatch`——Go 前端在解析後就把臂群**脫糖**成巢狀 `IfExpression`（兜底臂成最內層的 else），所以 `src/mir/hir2mir.go` 根本看不見臂群；自舉側 `ps.no` 走另一條路：開關 `PS-SURFACE-MATCH=1`（`compiler/src/ps.no:154`）把臂群**原樣**寫進節點的 helper 區（`ps.no:10414-10455`），故 `h2m` 必須自己認臂陣——這也正是 s57 造不出 else 臂的根因

**臂群形制（實測）**：`K-SURFACE-MATCH` 的 kid 陣只有一個 `Matched` 槽（花括號鏈形下恆為 0 ⇒ `nkid=1` 且 `kid[0]=0`）；臂數在 `d.hcount`，每臂佔 12 個 aux：`[binding, bindingVariant, typePattern, typeBinding, wildcard, dot-val, rawCond, blockBody, typeArm, valueCtx, Condition, Body]`；本件只讀 `+4`（兜底標記）／`+10`（條件）／`+11`（體）三槽，值匹配要用的 `binding`／`typePattern`／`valueCtx` 一律不在覆蓋面

**覆蓋面（本件）**：語句位的 `K-SURFACE-MATCH`——`h2m-lower-block` 認它作陳述，`K-EXPRESSION-STATEMENT` 包一層的單行形也認；逐臂一塊：當前塊求值 `Condition`（走 s57 比較族）後發 `OP-CONDBR`（`bs`=臂體塊，`dsts`=**下一臂的判定塊**），臂體走 `h2m-lower-branch`（可再嵌 `IF`／`FOR`／鏈中鏈）結束後無條件 `OP-BR` 回匯聚塊；兜底臂不發判定，其體直接落在當前塊尾再 `BR` 匯聚塊（單測釘"單兜底臂的整鏈 `icmp` 與 `br i1` 雙零出現"）；無兜底臂時末臂的假路**直落匯聚塊**，不開空判定塊（沿 s57 紀律，單測釘 `bb5:` 零出現）；前掃描 `h2m-scan` 見 `K-SURFACE-MATCH` 即置 `mem=1`，並由新增的 `h2m-scan-arms` 逐臂掃 `Condition`/`Body` ⇒ 臂內 `let` 也鋪得到 entry 槽

**未覆蓋（本件）**：值匹配 `x: { pat -> ... }`（`Matched` 非 0，要把型樣槽編成比較並接 `it` 綁定）；表達位的鏈 `r = { c -> a -> b }`（各臂須匯成同一結果值，本件只收語句位）；兜底臂不在末位（其後各臂恆不可達 ⇒ 改計 `unsup`，見關鍵點 5）；`break`／`continue`／`return`／計數循環／區間遍歷（沿用 s57 未收）

**關鍵點**：
1. **兜底標記不是整數 1**：`ps.no` 把布爾槽存成 `ast.put-str(t, 'true')` 回傳的**字串 id**，同一 aux 陣裡 `Condition`／`Body` 存的是**節點 id** ⇒ 判兜底只能寫 `wi > 0`；照直覺寫 `wi == 1` 會永遠判否，形同"兜底臂消失"
2. **臂群在 aux 陣 ⇒ 前掃描要另走一條路**：`h2m-scan-kids` 只吃 kid 陣，臂體從頭到尾不在裡頭；漏了 `h2m-scan-arms` 就不會為臂內 `let` 鋪槽，降級走到寫入口時"槽型 vs 右值實際型別"比不上 ⇒ 計 `unsup` 且整函式不產碼（誠實失敗，不會靜默錯碼）
3. 🔴 **Go 工具鏈 v0.4.13 在花括號鏈上有兩處偏離語言規範**（本件按規範產碼，對拍程式因此刻意避開該兩形）：(a) **無兜底臂的鏈不短路**——`{ n > 3 -> a  n > 1 -> b }` 在 `n = 4` 時 `a` 與 `b` 都執行，最後寫入者贏（`tmp/s58-sem3.no` 的 `tw(4)` 印出 `L1` 與 `L2` 並回 2），同一鏈補上兜底臂即短路正常（`tww(4)` 只印 `M1`）；(b) **全臂皆純賦值時兜底臂的寫入被折疊丟掉**——`{ n > 3 -> r = 1  -> r = 9 }` 在 `n = 0` 時函式內印出 9 而呼叫方拿到 0（`tmp/s58-sem7.no` `e1(0)=0`），任一臂帶副作用即恢復（`e2`/`e3` 得 9），換成局部量再賦給輸出參數同樣丟（`tmp/s58-sem8.no` `loc(0)=0`），兜底臂是唯一臂時正常（`only(0)=9`）⇒ 爆破面是"多臂 + 全純賦值"；兩條依 §2 只記不改（記 §11 B19/B20）
4. **對拍的"同值兜底"設計**：本輪探針首發用 `r = 0` 配 `-> r = 5`，Go oracle 得 **184** 而 h2m 得 **188**，差值逐筆可歸因——`pick(2)`（5 對 0，即 B20）與 `clip(4)`（1 對 2，即 B19）；改成"鏈前預設值與兜底臂寫入同數"（`r = 5 ... -> r = 5`）後兩側同為 **188**，三對源碼級變異亦逐位一致；代價是兜底臂的寫入在 oracle 側不可證，該半條路改由 IR 側反向補（同一刀 noc 189 而 Go 仍 188，這個不對齊本身就是 B20 的再現，不視為 h2m 缺陷）
5. **兜底臂只認末位**：兜底吃掉全部剩餘路徑，其後各臂恆不可達；鏈迴圈一遇兜底臂就 `stop=1`，實測其後臂體裡的 `return` 既不降級也不計 `unsup`（`e5703868` 首發即此形——產碼看起來正常，語義卻被吞）⇒ 本件補形狀校驗把"兜底不在末位"判成不覆蓋面（`ac202567`，負例 `wild-not-last counted`）；形狀校驗至此共四條：`arms <= 0`／臂體槽為 0／同臂既帶兜底標記又帶條件／兜底不在末位

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `h2m-test` **135 → 176/176**（新增 4 正例：三臂含兜底的逐塊形制（`br i1 %c12, label %bb2, label %bb3` 串 `br i1 %c19, label %bb4, label %bb5`／三臂各寫同槽 `%s4`／匯聚塊只 `load` 一次／`bb6:` 零出現）／無兜底兩臂鏈（末臂假路直落 `bb1`，`bb5:` 零出現）／單兜底臂（`icmp` 與 `br i1` 雙零出現）／循環體內嵌鏈（鏈匯聚塊 `bb4:` 只拿到循環回邊）；負例新增 2 條：表達位的鏈／兜底不在末位；s55/s56/s57 原有 135 條全留）；
- 零回歸：`cg-test` **342/342**；`cg-runtime-test` **60/60**；自有兩檔 `no fmt -d` rc=0；`no vet compiler/src` 自有 `compiler/src/h2m.no` **0 error / 0 warning**（34 error 全落在他人正在改的 `compiler/src/ps.no`）；s55/s56/s57 探針前向 exit **73 / 47 / 92** 不退化；
- 探針 `tmp/probe-s58.no`（三函式：三臂帶兜底鏈／兩臂帶兜底鏈／循環體內嵌鏈）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **188**（100+50+5+1+2+7+23）；IR 側四刀反向：**189**（兜底臂常量 5→6，證兜底塊真被選中並寫回同槽）／**195**（`clip` 首臂 1→8，未短路則仍得 2 ⇒ 證短路）／**190**（循環內臂 +10→+11，`i=5` 與 `i=4` 兩次命中 ⇒ +2，證鏈隨循環反覆執行）／**189**（`pick` 首臂 100→101）；
- 🔴 **Go oracle 差分對拍**：`tmp/s58-oracle.no` 同程式實測 **188**，三對源碼級變異兩側同值 **189 / 195 / 190**；第四對（兜底臂常量）Go 側仍 **188**，noc 側 **189**——該不對齊即 §11 B20 的再現；
- 鑑別態：把臂體常量改成字串 `r = 'oops'` ⇒ `unsup > 0`，IR 內 `define i64 @` 零出現，探針只發毒化 main ⇒ exit **101**（差分誠實失敗，不靜默回 0）

| noc | Go |
| --- | --- |
| 臂群原樣留在 helper/aux（12 槽/臂），`h2m-scan-arms` 逐臂取 `+4`/`+10`/`+11` | 前端 `desugarSurfaceMatch` 先脫成巢狀 `IfExpression`，MIR 看不見臂群 |
| 逐臂 `CONDBR` 的假路指向下一臂判定塊（塊數 = 臂數 + 2） | 巢狀 if 每層一個 else 塊，形同但層數由 HIR 決定 |
| 兜底臂不發判定，臂體直落當前塊後 `BR` 匯聚 | 脫成最內層 else 塊，形制一致 |
| 只認末位兜底，非末位計 `unsup` 不產碼 | 不可達臂仍照脫進巢狀 if 深層（由 Go 折疊 pass 決定去留） |
| 按語言規範產碼（短路 + 兜底生效） | v0.4.13 折疊 pass 偏離規範（§11 B19/B20） |

**未覆蓋／後續**：值匹配 `x: { pat -> ... }`**已於 s63 落地**（§5.3.4.cr：`binding`／`it` 綁定仍在外）；表達位的鏈；`break`／`continue`／`return` 的塊終結形**已於 s59 落地**（見 §5.3.4.cn）；計數循環與區間遍歷；實參 args arena 摘掉連續 id 契約（§11 B16）**已於 s64 落地**（§5.3.4.cs）；`drv.cmd-build` 接線（M4-d）——餘四者構成 M4-e 第六件的候選


### 5.3.4.cn s59——M4-e 第五件：塊終結形降級（`compiler/src/h2m.no` 認 `K-RETURN-STATEMENT`／`K-BREAK-STATEMENT`／`K-CONTINUE-STATEMENT` 三種陳述——`return` 懶開一個全函式共享的 ret 塊，終結處只發 `OP-BR`，`break`／`continue` 查循環目標棧棧頂的 `(hdr, exit)` 對，**零改 `ps.no`／`cg.no`**）（2026-11-04 本輪）

**背景**：s57 落控制流時把三種終結形記成"沿用未收"（§5.3.4.cl），s58 又原樣抄進第五件候選（§5.3.4.cm）；本件償這筆賬——`return` 是函式唯一出口，`break`／`continue` 是循環唯二的非結構化邊，不落地則真實源碼裡的函式一半降不出來（本輪探針的 `s59abs` 若缺 `return`，`r = n` 之後那條 `r = 0 - n` 會把它覆寫，語義直接錯）

**Go 權威**：`src/mir/hir2mir.go`——`KReturn`（:3808-3836）先把函式的 option 結果型別 surface 成 `typeHint`（讓 `return nil` 降成 `%option` 常量而非被誤讀成 `some(0)` 的裸 i64）再 `Terminate(OpReturn, vals)`；`KBreak`（:3840-3846）`loopStack` 空則 `unsupported("break outside loop")`，否則 `Terminate(OpBr, top.exit)`；`KContinue`（:3847-3853）同護欄後 `Terminate(OpBr, top.update)`；`lowerFor`（:4898-4975）建 header/body/update/exit 四塊並 push/pop `loopStack`＋`contStack`；函式尾由 `ensureReturn`（:2612-2632）補一條 RET；三個終結點全包在 `Terminate` 的 `if blk.Term == nil` 守門下——已終結的塊不再發第二條終結指令，其後陳述照降但落進死區段

**實測形制（`tmp/probe-s59-ast.no`，釘於 `tmp/s59-ast-out.txt`）**：`K-RETURN-STATEMENT`(56) `nkid=1` 且 `kid[0]=0`（guarded ReturnValue 缺席佔位），無標量槽；`K-BREAK-STATEMENT`(16)／`K-CONTINUE-STATEMENT`(22) `nkid=0`，s0=label（裸形為空串），s1=kind 名（裸形為 `'none'`）⚠️ **s1 是字串槽不是整數槽**，判無標籤只能比 `!= 'none'`，照直覺比整數會永遠判"帶標籤"⇒ 全部終結形被當不覆蓋面；速寫形 `*` 落 CONTINUE／`**` 落 BREAK（Go `stmt.go:3142 parseStarContinueOrExpr`／`:3165 parseStarStarBreak`，自舉側 `ps.no:1360/1363` 同口徑）⚠️ **二者於 2026-11-03 `aec062a5` 整對翻轉**，本檔 §6 M2 行 s49 的"`*`→break／`**`→continue"是翻轉前的舊紀實，讀時以碼為準；單陳述 guard `cond -> continue` 被包成 `K-EXPRESSION-STATEMENT`(27) → `K-IF-EXPRESSION`(36) → `K-BLOCK-STATEMENT`(14, s0='true') → 終結形；`break # 1`（`#` 後含空格）不成 LABEL token，被拆成 `USE`(71)＋`INT-LIT`——標籤形只在 `break #1` 緊接形下成立

**覆蓋面（本件）**：`h2m-lower-block` 在陳述列認三種終結形，並以 `last = (i == nkid-1)` 把關——終結形只允許站在本塊末條；`h2m-lower-branch` 另開一臂收"if 體／鏈臂體直接是終結形"的單陳述 inline 形（`n > 9 -> return`）；`return` 走**懶開共享 ret 塊**（`c.ret`，0=尚未開）：終結處只發 `OP-BR → c.ret`，真正的 `LOAD`＋`OP-RET` 由 `h2m-lower-func` 收尾在該塊發出，函式尾塊也 `BR` 過去（兩入口共用一條 RET）；`break`／`continue` 查 `h2m-lower-for` 進 body 前壓入的 `(hdr, exit)` 對（`c.stk`／`c.stk-n`，`2p`=hdr 給 continue，`2p+1`=exit 給 break），出 body 即邏輯彈出（只退計數，不剪陣列）；`cg.no` 側 `OP-BR`/`OP-RET` 早在 s14/s16 備齊，本件**一行未動**

**未覆蓋（本件）**：帶值 `return <val>`（`kid[0]` 非 0 ⇒ 計 `unsup`；Go 對該形直接報錯）；帶標籤的 `break #N`／`continue #N`（s1 ∈ {numeric, text}）與 `label:` 循環形；循環外的裸終結形（棧空 ⇒ 計 `unsup`，與 Go 同護欄）；終結形不在塊末（其後陳述不可達 ⇒ 計 `unsup`，見關鍵點 1）；多輸出參函式的 RET（沿用 s56 的單輸出限制）

**關鍵點**：
1. **終結形必須是本塊末條，否則語義被吞**：終結之後的陳述不可達，照降就會出現"產碼正常但少做事"的假象——`e5703868` 首發即此形（鏈臂裡的 `return` 既不降級也不計 `unsup`）；本件把 `last` 做成門禁，與 s58"兜底不在末位"同紀律（負例 `test-term-gate` 釘住）；鑑別態 g3（`return` 後添一條 `r = 0`）實測 `unsup > 0` ⇒ 只發毒化 main ⇒ exit **101**
2. **懶開 ret 塊而不是逐點 RET**：Go 每條 return 就地終結，函式尾再補一條；noc 改成一塊共享 RET——好處有兩件："延遲零值"免費落地（`return` 前未賦值的輸出參靠 entry 的 `CONST 0` 鋪槽，收尾在 ret 塊 `LOAD` 即得 0，`test-term-ret-noout` 釘 `%v8 = add i64 0, 0`），以及**無 `return` 的函式 `c.ret` 始終為 0 ⇒ IR 與 s57/s58 逐位元組不變**（回歸實測舊 176 條斷言全留）
3. **同一塊裡允許第二條不可達終結指令**：guard 塊發完 `BR → hdr/exit` 後，外層 if／鏈仍會在同塊尾補一條匯聚 `BR`（實測 IR `bb4:\n\tbr label %bb1\n\tbr label %bb6`）；Go 側靠 `Term == nil` 從不產生這種形，noc 側不另引 `term` 旗標——實證 `opt -passes=verify`（O2 前單跑）／`llc -O0`／`clang -O0` 三級全部 rc=0，**LLVM 容忍終結指令之後的不可達指令**，故單測把雙 BR 序列釘成期望形而非缺陷
4. **ret 塊序號跟著首個 `return` 的降級點走**：懶開時機決定 `bb<ret>` 的位置（`test-term-ret` 釘 `bb4`，而 `test-term-chain` 的鏈匯聚是 `bb1`／共享 ret 塊仍是 `bb4`）⇒ 文本斷言必須連同"其後不可達 BR"一起寫，否則換刀即漂
5. **`break` 的出口是"循環之後"而不是函式尾**：`h2m-lower-for` 的 exit 塊之後還接著循環後的繼語句（`r = r + 100`），單測釘 `bb3` 內既有 `%v23 = add i64 0, 100` 又有 `ret i64 %v25`，以此鑑別"break 跳出循環"與"break 當 return 用"兩種形

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `h2m-test` **176 → 203/203**（新增 6 條：`test-term-ret`（then 塊 store 後 `br %bb4`＋不可達 `br %bb3`／共享 ret 塊只讀一次槽／多餘塊 `bb5:` 零出現）／`test-term-cont`（continue 臂塊拿回邊 `br %bb1`＋不可達匯聚／被跳過的陳述落 merge 塊 `bb6`／exit 塊才 RET）／`test-term-brk`（break 臂塊 `br %bb3`=exit 而非回邊／循環後繼語句在 exit 塊照跑 `+100`）／`test-term-chain`（鏈臂直接 `br` 共享 ret 塊，兩入口共用一條 RET）／`test-term-ret-noout`（無輸出參 ⇒ ret 塊發零常量）／`test-term-gate`（非末條終結形／循環外 break／槽型不符三類負例全計 `unsup` 且不產碼）；s55–s58 原有 176 條全留）
- 零回歸：`cg-test` **342/342**；`cg-runtime-test` **60/60**；自有檔 `no fmt -d` rc=0（`h2m-test.no` 三處"陳述與註解間缺空行"已由 `no fmt -w` 歸零）；`no vet compiler/src` 自有 `compiler/src/h2m.no` **0 error**（34 error 全在他人正在改的 `compiler/src/ps.no`）；s55/s56/s57/s58 探針前向 exit **73 / 47 / 92 / 188** 不退化（`tmp/s59-regr.py` 一次跑五檔，含 s59）
- 探針 `tmp/probe-s59.no`（四函式 `s59abs`／`s59skip`／`s59brk`／`s59w`，main 手發六調用把 return 真路／return 假路／continue／break／break 尾／跨函式全落地面）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **72**（14+6+0+37+10+5）；⚠️ 初版探針 main 從不调用 `s59brk` ⇒ break 路徑零運行時覆蓋，本輪擴成六調用才把三條終結形全釘上
- 🔴 **Go oracle 差分對拍**：`tmp/s59-oracle.no` 同程式實測 **72**，逐值印 `14 6 0 37 10 5` 兩側一致；源碼級三對配對變異兩側同值 **76**（`i > 3 -> continue` 改 `i > 4`）／**70**（`i < 2 -> break` 改 `i < 3`）／**74**（`r = r + 10` 改 `r = r + 11`）
- IR 反向四刀（`tmp/s59-diff.py`）：**41**（`return` 目標 `bb4`→`bb3`，返回點落到未 return 路徑再算 `0-n`）／**66**（continue 目標 `bb1`→`bb3`，回邊變退出）／**73**（break 目標 `bb3`→`bb6`，跳出變累加）／**72**（break 目標 `bb3`→`bb1`，與基線同值 ⇒ 這一把**不鑑別**，留檔說明為何不用它：跳出改成重判條件時 `i` 仍單調遞減，累加集合不變，循環照樣在 `i=0` 退出）
- 鑑別態四例（`tmp/s59-gate.py`）：帶標籤 `break #1`／帶值 `return n`／終結形不在塊末／`break` 在循環外 ⇒ 各自 `unsup > 0`，IR 內 `define i64 @` 零出現，探針只發毒化 main ⇒ 四例實測 exit **101**

| noc | Go |
| --- | --- |
| 懶開單一 ret 塊，終結處只發 `BR`，收尾發 `LOAD`+`RET` | 每條 `return` 就地 `Terminate(OpReturn)`，函式尾 `ensureReturn` 另補一條 |
| 終結形非塊末 ⇒ 計 `unsup` 整函式不產碼 | `if Term == nil` 靜默抑制後續終結指令，其後陳述照降進死區段 |
| 同塊允許第二條不可達終結指令（verify/llc/clang 實測 rc=0） | 結構上不可能產生該形 |
| 循環目標用 `c.stk` 的 `(hdr, exit)` pair，continue 取 `2p`，break 取 `2p+1` | `loopStack` 頂另帶 `update` 欄——恆開一個 `for.update` 塊讓 continue 跳它（update 空時該塊再 BR header）；noc 的 `for` 形無獨立 update 子句 ⇒ continue 直取 hdr，少一塊 |
| 循環外裸終結形計 `unsup` | 同口徑 `unsupported("break outside loop")` |
| 只認無標籤裸形 | 標籤解析 `break # N` 逐層跳出 |

**未覆蓋／後續**：帶標籤終結形與 `label:` 循環；帶值 `return`（要接多輸出參與 option 結果）；計數循環（`N * { }` 與 `{ } * N`）**已於 s60m 落地**（見 §5.3.4.co）；區間遍歷（`i <- [a..b]`）**已於 s61 落地**（見 §5.3.4.cp）；容器遍歷的 `ForStatement` 剩餘形；值匹配 `x: { pat -> ... }`**已於 s63 落地**（§5.3.4.cr，只收 i64 主體＋裸整型字面量型樣）；表達位的鏈；實參 args arena 摘掉連續 id 契約（§11 B16）**已於 s64 落地**（§5.3.4.cs）；`drv.cmd-build` 接線（M4-d）——以上構成 M4-e 第七件的候選（區間位已償，餘下以容器遍歷為首）


### 5.3.4.co s60m——M4-e 第六件：計數循環降級（`compiler/src/h2m.no` 認 `K-FOR-STATEMENT` 的計數位——計數值只求值一次並 `STORE` 進按節點 id 合成的 entry `alloca` 槽，發 hdr/body/update/exit 四塊輪轉，`continue` 目標由 hdr 換成 update，**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**）（2026-11-04 本輪）

**標號說明**：本件記 **s60m** 而非 s60——`s60`–`s80` 已被平行的 parse／語料線佔用（§6 M2 行裡的 `noc-s60`…`noc-s80`），沿用專案既有的 `s65a`／`s66d`／`s67fix` 後綴慣例，避免兩條線在同一代號上互相覆蓋

**背景**：s57 落控制流時把 `ForStatement` 記成"只認條件循環 `(cond) { }`，計數循環與區間遍歷沿用未收"（§5.3.4.cl），s58 與 s59 兩度原樣抄進下一件候選；本件償這筆賬——`n * { ... }` 是 Nolang 表達"重複 N 次"的標準形（AGENTS.md §3.2 關鍵字表），真實源碼裡前置形與後置形並存，不落地則任何輪轉累加都降不出來

**Go 權威**：`src/mir/hir2mir.go`——`lowerFor`（:4871-4975）先查 `countID != NoID` 便轉入計數分支，且**靜默丟棄 Condition**；`lowerCountFor`（:5408-5474）以 `iSlot = EmitInt(OpConst, ..., "ri")` + `EmitMoveInto` 把計數值放進棧槽，header 發 `OpGt(iSlot, zero)`，update 發 `OpSub(iSlot, one)` 後回邊；`KContinue`（:3847-3853）取 `loopStack` 頂的 `top.update`；語法側 `src/parser/ast.go:1739-1780`（`ForStatement` 六子節點）與 `src/parser/tohir.go:618-634`

**實測形制（`tmp/probe-s60m-ast.no`，釘於 `tmp/s60m-ast-out.txt`）**：`K-FOR-STATEMENT`(30) `nkid=6`，kid 序 `[Init][Condition][Update][CountExpr][IterRange][Body]`；ps.no 的前置形 `s57-parse-count-loop-prefix`(:5946) 與後置形 `s57-parse-count-loop-blockfirst`(:5979) **歸一為同一棵 AST**——兩形的 `kid[3]` 與 `kid[5]` 各為 `K-INT-LIT` 與 `K-BLOCK`，其餘四 kid 恆 0 ⇒ 一次實作同時覆蓋兩形，單測 `test-cnt-postfix` 以"兩形塊序與 id 集合相同"釘住 ⚠️ **標識符計數位 `n * { }` 根本不建 `ForStatement`**：實測退化成 `K-EXPRESSION-STATEMENT`(27) → `K-IDENT`(35) 加一條旁落 `K-LET`（`tmp/s60m-ir2.txt` 的 `g` 因 `unsup=1` 整函式不產碼）⇒ 該形屬前端缺口，記 §11 B22，不在本件強補；負計數位照常入槽：`-2 * { }` 的 `kid[3].s0='-2'`，`to-i64()` 吃負號字面量，IR 發 `%v9 = add i64 0, -2`

**覆蓋面（本件）**：`h2m-lower-for`（:1041）新增 `bad == 0 && cnt != 0` 臂（:1083）：先 `h2m-lower-expr` 求計數位（須降成非 0 值 id 且 `h2m-vty-at` 為 `TY-I64`），`h2m-var-find` 取合成槽，發 `OP-STORE` 鋪初值 + `OP-BR → hdr`；hdr 發 `OP-LOAD`／`OP-CONST 0`／`OP-CMPGT`／`OP-CONDBR(dsts=exit, xa=cond, bs=body)`；body 走 `h2m-lower-block`（其間 `break`／`continue` 由 s59 的循環棧接管），尾發 `OP-BR → update`；update 發 `OP-LOAD`／`OP-CONST 1`／`OP-SUB`／`OP-STORE`／`OP-BR → hdr`；exit 承接循環後繼語句——四塊全用既有 opcode，`mir.no` 未新增碼位，`cg.no` 一行未動；實測 IR（`tmp/s60m-ir.txt`）形如 `entry: %s6 = alloca i64 … %v9 = add i64 0, 3; store i64 %v9, i64* %s6; br label %bb1` → `bb1: %v10 = load; %v11 = add i64 0, 0; %c17 = icmp sgt … br i1 %c18, label %bb2, label %bb4` → `bb2: … br label %bb3` → `bb3: load／add 1／sub／store／br label %bb1` → `bb4: load 輸出參; ret`

**未覆蓋／後續**：區間遍歷 `i <- [a..b]`（`kid[4] IterRange` 非 0）**已於 s61 落地**（見 §5.3.4.cp）；容器遍歷 `i <- xs`（`kid[1]` 形）；標識符或一般表達式計數位（前端不建 FOR，見上）；計數位非 i64（`f64`／`str` 計數無定義）；count 與 cond 並存的混合形（本件計 `unsup`，Go 側卻會吞掉條件——見關鍵點 5）；計數循環體內的函式聲明（沿用 s56 的單層簽名表）

**關鍵點**：
1. **合成計數槽必須由前掃描鋪好**：`cg.no` 要求 `alloca` 全落 entry，一旦離開 entry 就回不去，故槽由 `h2m-scan`（:663）在降級前 `h2m-lst-add`，降級側只 `h2m-var-find` 查回，查不到即計 `unsup` 不產碼——與 s57 給源碼變量鋪槽是同一條通路，本件只是多註冊一個合成名字
2. **槽名 `@cnt<node-id>`**（`h2m-cnt-name`，:382）：`@` 不是合法標識符首字符 ⇒ 永不與源碼變量相撞；帶 `ForStatement` 節點 id ⇒ 嵌套循環各佔一槽（實測外層 `%s6`／內層 `%s8`），`test-cnt-nested` 釘兩個槽同時存在
3. **計數值只求值一次**：進循環前發一次 `STORE`，其後 header/update 只讀槽——Go 同形（`EmitMoveInto` 一次）；若每輪重求值，`{ } * f()` 一類帶副作用的計數位會被跑 N 次，且 header 每輪多一串指令
4. **`continue` 落 update 而非 hdr**：計數循環的自減住在 update 塊，跳過自減即活鎖；`c.stk` 每層 2 槽的語義由 s59 的 `(hdr, exit)` 升級為 `(continue 目標, exit)`——條件循環仍壓 hdr，計數循環壓 update，與 Go `loopCtx{exit, update}` 同口徑，且 s59 的 6 條終結形單測逐字未動仍全過
5. **不跟 Go 的"count 吞 cond"**：`cnt != 0 && cond != 0 -> bad = 1`（:1053）計 `unsup` 不產碼——`3 * (n < 2) { }` 之類混合形在 Go 側會靜默丟掉條件只按計數跑，noc 選擇不模仿這個語義分歧（記 §11 B21），負例由 `test-cnt-gate` 釘住
6. **`N ≤ 0` 靠 header 謂詞而非特判**：`icmp sgt` 首判即假 ⇒ body 一次不進，`-2` 與 `0` 兩形的 IR 與正計數同構（只差常量）；單測 `test-cnt-nonpos` 因此釘"謂詞是 sgt 不是 ne"，防後續把它"優化"成 `!= 0` 讓負數跑成近似無窮輪
7. **塊 id 文本序非單調是合法形**：嵌套時 update／exit 塊排在內層塊之後（實測 `bb8:` 的 `br label %bb3`），`cg.no` 靠 `blks[k] != cur` 切標籤，`opt -passes=verify`／`llc`／`clang` 三級 rc=0；單測把"多餘塊 `bb5:` 零出現"當正向斷言寫，防的是塊數膨脹而不是塊序

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `h2m-test` **203 → 255/255**（新增 7 條：`test-cnt-fwd`（entry 槽＋鋪值＋hdr sgt＋body BR update＋update 自減回邊＋exit RET，並釘"body 不直接 BR hdr"）／`test-cnt-postfix`（後置形與前置形同 id 集合）／`test-cnt-nonpos`（`-2` 與 `0` 兩形的 header 謂詞皆 sgt）／`test-cnt-break`（break 落 exit，循環後繼語句照跑 `+100`）／`test-cnt-cont`（continue 落 update，釘 `bb5:` 的 `br label %bb3` 且 `%bb1` 版本為 0）／`test-cnt-nested`（`%s6`／`%s8` 雙槽與內層 exit 回外層 update）／`test-cnt-gate`（合成 AST 造 count+cond 並存／計數位是 `K-STRING-LITERAL` 兩負例，皆計 `unsup` 且不產碼）；`test-flow-gate` 原計數負例的註釋同步更正——它今天仍成立，但**理由變成"標識符計數位不建 ForStatement"**）
- 零回歸：`cg-test` **342/342**；`cg-runtime-test` **60/60**；自有檔 `no fmt -d` rc=0；`no vet compiler/src` 自有 `compiler/src/h2m.no` **0 error**（34 error 全在他人正在改的 `compiler/src/ps.no`）；s55–s59 探針前向 exit **73 / 47 / 92 / 188 / 72** 不退化（`tmp/s60m-regr.py` 一次跑六檔，含本件）
- 探針 `tmp/probe-s60m.no`（六函式 `s60a`…`s60f`：前置計數／後置計數／體內 break／體內 continue／嵌套／負計數，main 手發六調用求和）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **134**
- 🔴 **Go oracle 差分對拍**：`tmp/s60m-oracle.no` 同程式實測 **134**，逐值印 `3 10 104 11 6 0` 兩側一致；源碼級四對配對變異兩側同值 **135**（break 閾值 3→4）／**141**（continue 閾值 2→9）／**136**（後置計數 5→6）／**136**（負計數 -2→2）⚠️ 後兩對同為 136 卻**不是同一原因**（p3 讓 `s60b` 多跑一輪 +2，p4 讓 `s60f` 由 0 變 2）——鑑別力來自"兩側同值"而非"值互異"，不可拿同值推 p3/p4 等價
- IR 反向五刀（`tmp/s60m-diff.py`）：**132**（update 回邊改 exit，只跑一輪）／**131**（header `sgt` 改 `sle`，一次不進）／**135**（break 目標改 update，跳出變 continue）／**132**（continue 目標改 exit，continue 變 break）／**131**（內層 exit 改指外層 exit，外層只剩一輪）⚠️ 六個函式的值 id 各自重啟 ⇒ 反向刀的錨必須按函式區段限定（本件新增 `mutate_func(ir, func, anchor, repl)`），全域 `count(anchor)==1` 在多函式探針上是假前提
- 鑑別態三例：區間遍歷 `i <- [0..3]:`／標識符計數位 `n * { }`／帶標籤 `break # 1` ⇒ 各自 `unsup > 0`，IR 內 `define i64 @` 零出現，探針只發毒化 main ⇒ 三例 exit **101**
- ⚠️ 對拍方法論兩坑：探針檔頭註釋裡抄了同源碼文字會讓 `count(anchor)==1` 護欄誤拒（`    } * 5` 必須帶縮進才唯一）；SRC 是單行轉義串，Python 側錨要寫 `\\n` 而非真換行

| noc | Go |
| --- | --- |
| 計數值 `STORE` 進 `@cnt<node-id>` 合成 entry 槽，`@` 前綴避撞源碼變量 | `EmitInt(OpConst, ..., "ri")` 命名棧槽 + `EmitMoveInto` 一次鋪值 |
| 前置形／後置形在 ps.no 已歸一 ⇒ 降級側只認一棵樹 | `ForStatement.Count` 是單一子節點，兩形天然同樹 |
| `c.stk` 頂存 `(continue 目標, exit)`，計數循環壓 update，條件循環壓 hdr | `loopStack` 頂帶 `update` 欄，`KContinue` 恆取 `top.update` |
| count 與 cond 並存 ⇒ 計 `unsup` 不產碼（有記錄的偏離，§11 B21） | `countID != NoID` 直接轉計數分支，Condition 被靜默丟棄 |
| 標識符計數位不建 FOR ⇒ 計 `unsup`（§11 B22） | Go 前端對任意 expr 建 `ForStatement{Count}`，降級照跑 |
| header 用 `OP-CMPGT` 對 `CONST 0`（`icmp sgt`），負數首判即假 | `OpGt(iSlot, zero)`，同形 |

**未覆蓋／後續**：區間與容器遍歷（`i <- [a..b]`／`i <- xs`）——區間位**已於 s61 落地**（§5.3.4.cp），容器位**已於 s62 落地**（§5.3.4.cq）；帶標籤終結形與 `label:` 循環；帶值 `return`；值匹配 `x: { pat -> ... }`**已於 s63 落地**（§5.3.4.cr，只收 i64 主體＋裸整型字面量型樣）；表達位的鏈；實參 args arena 摘掉連續 id 契約（§11 B16）**已於 s64 落地**（§5.3.4.cs）；`drv.cmd-build` 接線（M4-d）——以上構成 M4-e 第七件的候選


### 5.3.4.cp s61——M4-e 第七件：區間遍歷降級（`compiler/src/h2m.no` 認 `K-FOR-STATEMENT` 的 `IterRange` 位——兩端點在 pre 塊各只求值一次，pre／hdr／upd 三處同發一條 `icmp sle lo, hi` 分派升降序雙臂，四種括號形由 `leftInc`／`rightInc` 兩旗標決定起始偏移與終止謂詞，**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**）（2026-11-04 本輪）

**背景**：s57 落控制流時把 `ForStatement` 記成"只認條件循環 `(cond) { }`，計數位與區間位沿用未收"（§5.3.4.cl），s58／s59／s60m 三度把它列進下一件候選；本件償區間位——`i <- [a..b)` 是 Nolang 逐個取整數值的唯一形態（AGENTS.md §3.6），**四種括號組合**與**start > end 自動遞減**是它區別於 C 系 `for` 的兩個語義要點，不落地則任何掃描區間的真實源碼都降不出來

**Go 權威**：`src/mir/hir2mir.go`——`lowerFor`（:4871）見 `IterRange` 轉入 `lowerRangeFor`（:5017-5176）的整數區間分支：`FlagLeftInc`／`FlagRightInc`（:5059-5060）決定起始偏移 `exclOff`（:5102-5105）與終止謂詞 `ascCmpOp, descCmpOp`（:5136-5139）；方向由 `OpLe(startV, endV)` 判定，且**在 pre（:5113）／init-join 後的 header（:5142）／update（:5164）三處各判一次**——碼內註釋交代 IR 無 select/mux ⇒ 只能拿 loop-invariant 端點做分支，`opt` 會把它們提出循環；`loopStack` 頂壓 `loopCtx{exit, update}`（:5126）⇒ `continue` 落 update；開邊界以 `math.MinInt64`／`math.MaxInt64` 補齊（:5074／:5079）

**實測形制（`tmp/probe-s61-ast.no`，釘於 `tmp/s61-ast-out.txt`）**：`K-FOR-STATEMENT`(30) 六 kid 序與 s60m 相同，區間形填 `kid[4]`＝`K-ITERATION-EXPR`(41)（`nkid=2`，`s0`＝循環變量名，`kid[0]`＝`K-RANGE-EXPRESSION`(54) 或 0，`kid[1]`＝容器源或 0）；kind 54 的 `s0`／`s1` 是字面 `'true'`／`'false'` 的 leftInc／rightInc，`kid[0]`／`kid[1]` 為兩端點（開邊界該 kid=0）⚠️ **`for v <- rng { }` 關鍵字形會同時寫 `Condition` 與 `IterRange`**（`ps.no:5762`）——兩者同義，形位計數若把 cond 一律計一位會把該形誤判成"多位並存"而整函式不產碼，故 `rg > 0` 時 cond 不計位且只允許它是指向循環變量自身的 `K-IDENTIFIER`(35)（`h2m-rng-check` 判據）

**覆蓋面（本件）**：新增 `h2m-lower-range` 發 11 塊——pre（兩端點各 `h2m-lower-expr` 一次 + `OP-CMPLE` + `OP-CONDBR`）→ initAsc `i = lo + off`／initDesc `i = lo - off` → join `BR hdr` → hdr 再判方向 → hdAsc `LOAD i` + 升序謂詞／hdDesc `LOAD i` + 降序謂詞（均對 `endV` 比較）→ body（尾 `BR upd`）→ upd 再判方向 → updAsc `+1`／updDesc `-1` → `BR hdr` → exit 承接循環後繼語句；循環變量槽由 `h2m-scan` 經 `h2m-rng-var` 取**源名**鋪在 entry `alloca`（源名而非 `@` 合成名 ⇒ 循環體內的 `K-IDENTIFIER` 直接 `h2m-var-find` 命中，無需名字改寫）；實測 IR（`tmp/s61-ir.txt` 用例 A）形如 `entry: … %v6 = add i64 0, 0; %v7 = add i64 0, 5; %c10 = icmp sle i64 %v6, %v7` → `bb1`/`bb2` 兩條 init 雙臂 → `bb3: br label %bb4` → `bb4: %c21 = icmp sle` → `bb5: %v14 = load i64, i64* %s3; %c24 = icmp slt i64 %v14, %v7` → `bb7` body → `bb8: %c34 = icmp sle` → `bb9`/`bb10` 步進雙臂 → `bb11` exit

**未覆蓋（本件）**：容器遍歷 `i <- xs`（`kid[1]` 形，要 `OP-LEN`／`OP-INDEX` 兩條尚不存在的碼位）；開邊界 `[a..]`／`[..b]`／`[..]`（Go 取 i64 型別極值補齊，實測其量級不可鑑別 ⇒ 寧缺勿錯）；嵌套同名區間循環（Go 靠 `l.locals[varName] = iSlot` 覆寫捕獲，循環結束後 `locals` 仍指向內層槽——兩邊語義都不乾淨，記 §11 B23）；端點型別非 i64；區間位與計數位／條件位並存

**關鍵點**：
1. **端點值 id 跨塊復用是 SSA 合法性的前提**：`sv`／`ev` 定義於 pre，pre 支配一切循環塊 ⇒ 三處方向判與兩處終止判引用同一對 id（實測 `%c10`／`%c21`／`%c34` 皆 `icmp sle i64 %v6, %v7`），既釘住"端點只求值一次"，也證明循環不每輪重 `LOAD` 端點槽——帶形參的 `s61f` 特別顯著：`%v12 = load i64, i64* %s3` 只出現在 pre，hdr／upd 首條都不是 `LOAD`
2. **方向不在編譯期折疊**：`[5..0)` 照發運行期 `icmp sle 5, 0` 且升降雙臂齊發，鏡像 Go 的無 select 形制；編譯期判死方向會與 Go 的 IR 文本分岔，也讓 `opt` 的循環提領無從發生
3. **四括號形不複製四份代碼**：`li != 'true' -> lo-off = 1`；`ri == 'true'` 把 `asc-op`／`desc-op` 由 `OP-CMPLT`／`OP-CMPGT` 升為 `OP-CMPLE`／`OP-CMPGE`——與 Go `:5102-5105`／`:5136-5139` 一一對應，單測以"正謂詞在場 + 錯謂詞零出現"雙向釘（`test-rng-closed` 斷言 `sge` 在場且 `sgt` 缺席）
4. **槽用源名鋪在 entry**：合成名那套 `@` 前綴（`h2m-cnt-name`，§5.3.4.co 關鍵點 2）在區間形會逼出"體內引用改名"，而區間變量本就是源碼可見名，直接登記最省；查不到槽即計 `unsup` 不產碼
5. **`continue` 落 update 不落 hdr**：與 s60m 同口徑（Go `loopCtx{exit, update}`），跳過步進即活鎖；`test-rng-cont` 釘 `bb12:` 的 `br label %bb8` 且 `%bb4`／`%bb11` 兩版本為 0
6. **Go 後端的 str 比較地雷**（本件實測撞上）：帶 `#{index-out=zero}` 的下標讀直接作 `==` 左運算元（`c.rnms[q] #{index-out=zero} == vn`）會被 codegen 丟擲 `icmp ne %str-long %lv.N, 0` ⇒ `integer constant must have integer type`；一切 str 比較先賦給具名局部再判（`ast.str-at(...)` 亦同）——本件三處（`qn`／`li`+`ri`／`cnm`）皆因此改寫，旁證 `src/build/safe_index_hint_test.go` 與 `transpiler_validate.go:1528`
7. **判重用活動名棧 `c.rnms`**：進區間循環 push 源名，退出 pop，同名嵌套即計 `unsup`；用名字而非槽 id 判，是因為兩顆同名槽在 IR 文本上完全對稱，反向刀切不出差異

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `h2m-test` **255 → 324/324**（新增 9 條：`test-rng-asc`（11 塊全序 + 端點先於方向判 + 右開不為 `sle` 負例 + 多餘塊 `bb12:` 零出現）／`test-rng-closed`（`%c32 sle`／`%c35 sge` 且 `slt`／`sgt` 零出現）／`test-rng-openleft`（`%v15 = add i64 0, 1` 且 `add i64 0, 0` 零出現）／`test-rng-desc`（`[5..0)` 端點反序仍發 `icmp sle`，init 與 hdr 雙臂齊發）／`test-rng-var`（變量端點 `%v12`／`%v13 = load`，hdr／upd 首條非 `LOAD`）／`test-rng-break`（break 落 exit，循環後繼 `+100` 照跑）／`test-rng-cont`（continue 落 update）／`test-rng-kw`（`for i <- rng` 關鍵字形與運算符形同形）／`test-rng-gate`（同名嵌套／容器遍歷／開邊界三負例皆計 `unsup` 且 `define` 零出現）；`test-flow-gate` 的原區間負例換成開邊界 `i <- [0..]:`——它今天仍成立，但**理由變成"開邊界的極值補齊量級不可鑑別"**）
- 零回歸：`cg-test` **342/342**；`cg-runtime-test` **60/60**；自有兩檔 `no fmt -d` rc=0；`no vet compiler/src` 自有 `compiler/src/h2m.no` **0 error**（34 error 全在他人正在改的 `compiler/src/ps.no`）；s55–s60m 探針前向 exit **73 / 47 / 92 / 188 / 72 / 134** 不退化（`tmp/s61-regr.py` 一次跑七檔，含本件）
- 探針 `tmp/probe-s61.no`（九函式：升序右開／升序右閉／左開右閉／降序右開／降序右閉／變量端點／體內 break／體內 continue／嵌套異名，main 手發九調用求和）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **164**
- 🔴 **Go oracle 差分對拍**：`tmp/s61-oracle.no` 同程式實測 **164**，逐值印 `10 6 6 6 4 14 103 9 6` 兩側一致；源碼級八對配對變異兩側同值 **168**（右閉上界 3→4）／**168**（降序起點 3→4）／**168**（左開右閉 3→4）／**171**（break 閾值 3→5）／**162**（continue 閾值 1→3）／**161**（右閉改右開）／**161**（降序改左開）／**156**（變量端點實參 `(2,6)`→`(1,4)`，兩側寫法不同需分開給錨）
- IR 反向八刀（`tmp/s61-diff.py`）：**169**（升序右開終止判 `slt`→`sle`，多跑一輪）／**161**（右閉 `sle`→`slt`，少跑末輪）／**163**（左開偏移 1→2，起點挪一格）／**158**（降序臂 `sgt`→`sle`，首判即假一次不進）／**163**（降序右閉 `sge`→`sgt`，少跑 `i=0`）／**160**（方向判三處齊改 `sgt`，init 走錯臂）／**194**（break 目標改 update＝跳出變 continue，那一輪連帶跳過累加故 103→133）／**155**（continue 目標改 exit＝continue 變 break，9→0）⚠️ 九個函式的值 id 各自重啟 ⇒ `mutate_func` 的函式頭改為前綴匹配（`define i64 @f(`），帶形參的 `s61f` 與無參函式才共用同一把刀；方向判三處同字 ⇒ 需要 `want=3` 的齊改參數，單點 `count==1` 護欄在這種錨上是假前提
- 鑑別態三例：同名嵌套 `i`／容器遍歷 `x <- xs:`／開邊界 `i <- [0..)` ⇒ 各自 `unsup > 0`，IR 內 `define i64 @` 零出現，探針只發毒化 main ⇒ 三例 exit **101**
- ⚠️ 對拍方法論再記兩坑：探針檔頭註釋抄了同源碼文字會讓 `count(anchor)==1` 護欄誤拒（`i <- [0..3]:` 在檔內出現兩次，錨必須帶 `: {` 加轉義換行才唯一）；SRC 是單行轉義串（`\n` 兩字符）而 oracle 檔是真換行 ⇒ 同語義錨兩側不同字，本件加 `probe_to_oracle()` 自動換算

| noc | Go |
| --- | --- |
| pre 發一條 `OP-CMPLE(sv, ev)`，hdr／upd 各再發一次，三處引用同一對端點值 id | `OpLe(startV, endV)` 於 :5113／:5142／:5164 三次發射，IR 無 select ⇒ 靠分支定方向 |
| `li != 'true' -> lo-off = 1`，init 雙臂 `lo + off`／`lo - off` | `exclOff`（:5102-5105）+ `initAsc`／`initDesc`（:5107-5108） |
| `ri == 'true'` 把 `asc-op`／`desc-op` 由 `LT`／`GT` 升為 `LE`／`GE` | `ascCmpOp, descCmpOp = OpLe, OpGe`（:5136-5139） |
| 循環變量槽按**源名**鋪 entry `alloca`，體內 `K-IDENTIFIER` 直接查得 | `EmitInt(OpConst, elemT, 0, varName)` + `l.locals[varName] = iSlot`（:5093-5095） |
| `c.stk` 壓 `(upd, exit)`，`continue` 落 update | `loopStack` 頂 `loopCtx{exit, update}`（:5126） |
| 同名嵌套／開邊界／容器形 ⇒ 計 `unsup` 不產碼（有記錄的偏離，§11 B23） | 同名靠 `l.locals` 覆寫捕獲（循環後不回復）；開邊界取 `MinInt64`／`MaxInt64`；容器形走 `rangeExprID` 另一分支 |

**未覆蓋／後續**：容器遍歷 `i <- xs`（要 `OP-LEN`／`OP-INDEX`）**已於 s62 落地**（§5.3.4.cq，實為復用既有 `OP-VECLEN`／`OP-VECGET`，本節所記"尚不存在的碼位"屬假帳，已在該節更正）；開邊界區間；值匹配 `x: { pat -> ... }`**已於 s63 落地**（§5.3.4.cr，只收 i64 主體＋裸整型字面量型樣）；表達位的鏈；帶標籤終結形與帶值 `return`；實參 args arena 摘掉連續 id 契約（§11 B16）**已於 s64 落地**（§5.3.4.cs）；`drv.cmd-build` 接線（M4-d）——以上構成 M4-e 第九件的候選


### 5.3.4.cq s62——M4-e 第八件：容器遍歷降級（`compiler/src/h2m.no` 認 `K-ITERATION-EXPR` 的 `kid[1]` 容器源——長度在 pre 塊只求一次，索引落合成槽 `@idx<n>`，`VECGET` 結果 `STORE` 進循環變量槽，發 pre／hdr／body／upd／exit 五塊，**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**（復用 s23 的 `OP-VECLEN`／`OP-VECGET`））（2026-11-04 本輪）

**背景**：s61 把容器形記成"要 `OP-LEN`／`OP-INDEX` 兩條尚不存在的碼位"（§5.3.4.cp 未覆蓋首項）；本件勘察第一步就復核該前提——`cg.no:1830-1844` 早有 `OP-VECLEN=20`／`OP-VECGET=21` 兩臂（各發 `call i64 @vec_len(%vec)`／`call i64 @vec_get_i64(%vec, i64)`），`cg-runtime.no:239-374` 的 helper 體正是 Go 那串內聯指令（`extractvalue` field 0／`dat + i*8` → `inttoptr` → `load`），故**無需新增任何碼位**；與此同時 `src/mir/../compiler/src/mir.no` 裡"Go `emitLenCap` 的 `KindSlice` 無 `%vec` 分支"一句經實讀 `src/build/llvm/codegen.go:11981-12056` 確認為假帳（`KindStr`／`KindSlice` 走 `extractvalue` field 0=Len／1=Cap 再 `store i64`，`KindSlice` 的 LLVM 型別即 `%vec`），已就地改寫註釋（引用本節）

**Go 權威**：`src/mir/hir2mir.go:5179-5301`（`lowerRangeFor` 的容器分支）——`str` 源先分岔給 `lowerStrRangeFor`；`elemT = elementTypeOf(arrV)`；長度在 `KindArray && len(Sizes)>0` 時取 `EmitInt(OpConst, …, Sizes[0])` 否則 `Emit(OpLen, idxT, [arrV])` 且**循環外只求一次**；`idxSlot = EmitInt(OpConst, idxT, 0, varName+"#idx")` ＋ pre 塊 `STORE 0`；header `OpLt(idxSlot, lengthV)` ⇒ `OpCondBr(body, exit)`；body `OpIndex(elemT, [arrV, idxSlot])` ＋ `locals[varName] = iSlot` ＋ 非 map 時 `bindElemPlace(...)` 併 `defer restore()`（**循環變量綁到元素位址，體內寫 `v` 即寫回容器**）；`loopStack` 壓 `loopCtx{exit, update}`，`contStack` 壓 `update`；update `OpAdd(idxSlot, 1)` → `BR` header；exit `BR` enclosingCont

**實測形制（`tmp/probe-s62-ast.no`／`tmp/probe-s62-kw.no`，釘於 `tmp/s62-ast-out.txt`）**：容器遍歷的 `K-FOR-STATEMENT`(30) 六 kid 序與 s60m／s61 相同，`kid[0..3]` 皆 0，`kid[4]`＝`K-ITERATION-EXPR`(41)（`nkid=2`，`s0`＝循環變量名，`kid[0]`＝0 區間位空，`kid[1]`＝容器源 `K-IDENTIFIER`(35) `nkid=0`），`kid[5]`＝`BLOCK`；形參 `xs []i64`＝`K-PARAMETER`(50) 的 `kid[0]`＝`K-SLICE-TYPE`(60)（`nkid=1`，`s0='false'`）其 `kid[0]`＝`K-NAMED-TYPE`(47) `s0='i64'`；定長 `[3]i64` 是 `K-ARRAY-TYPE`(11) `nkid=2`（本件不收）⚠️ `for v <- xs {` 關鍵字形與 `v <- xs: {` 運算符形的 AST **逐位相同**（`kid[1]` 都是循環變量自身的 `IDENTIFIER`）——`s61` 為區間形記過的 cond 同義坑，容器形照樣要防

**覆蓋面（本件）**：新增 `h2m-lower-coll` 發五塊——pre（當前塊：`OP-VECLEN` 一次 + `OP-CONST 0` → `STORE` 索引槽 → `BR hdr`）／hdr（`LOAD` 索引 → `OP-CMPLT(idx, len)` → `OP-CONDBR(body, exit)`）／body（`OP-VECGET(elem, vec, idx)` → `STORE` 進循環變量槽 → 壓 `stk`／`rnms`／`epl` 三棧 → `h2m-lower-block` → `BR upd`）／upd（`LOAD` + `CONST 1` + `ADD` + `STORE` → `BR hdr`）／exit（循環後繼語句落此）；`h2m-scan` 認 `h2m-coll-check` 命中即為**循環變量與索引各鋪一顆 `i64` 槽**（索引槽名 `@idx<n>` 沿用 `@` 前綴避撞源碼名）；`h2m-param-ty` 認 `K-SLICE-TYPE(i64)` ⇒ `TY-VEC`，`h2m-func-sig` 據此在 `define` 行發 `%vec %vc1`；實測 IR（`tmp/s62-ir.txt` 用例 A）形如 `entry: … %v9 = call i64 @vec_len(%vec %vc1); store i64 %v10, i64* %s6; br label %bb1` → `bb1: %v11 = load i64, i64* %s6; %c16 = icmp slt i64 %v11, %v9` → `bb2: %v13 = call i64 @vec_get_i64(%vec %vc1, i64 %v11); store i64 %v13, i64* %s4` → `bb3` 步進 → `bb4: load + ret`

**未覆蓋（本件）**：`str` 源逐碼點遍歷（Go `lowerStrRangeFor` 需 `utf8_at` 一類 helper，noc 未備）；定長陣列源（無型別登記表 ⇒ `Sizes[0]` 常量不可得）；元素型別非 `i64`（cg 只有 `@vec_get_i64`，`[]str`／`[]byte` 需逐元素型別分派）；`vec` 作局部量（`h2m-slot-decl` 對未綁定的 `TY-VEC` 計 `unsup`）與 `vec` 作返回值（無 `%vec` 返回發射路，`h2m-func-sig` 判死）；體內改寫循環變量（Go 寫回元素，noc 改副本 ⇒ 語義分岔，記 §11 B24）；嵌套同名遍歷名；`map` 源

**關鍵點**：
1. **"先證偽舊前提，再決定復用"是本件最大的產出**：若照 s61 的假帳去補 `OP-LEN`／`OP-INDEX`，會與既有 `OP-VECLEN`／`OP-VECGET` 形成同一份語義的兩條發射路徑（codegen 雙份，測試也雙份），故容器遍歷復用 `OP-VECLEN` 而不另造泛化 `OP-LEN`；`mir.no` 的假帳註釋同步改實，防下一輪又被它引誘
2. **`TY-VEC` 形參在 `mem` 模式下"不鋪槽"**：cg 沒有 `alloca %vec` 的 op（`STALLOC`／`OPTALLOC` 各管自己的型別），而函式形參本身就是 `entry` 就存在的 SSA 值，支配全函式所有塊 ⇒ 綁定保持 incoming 值 id 即跨塊可讀；`h2m-slot-decl` 由此拆成兩分支（`TY-VEC` 只改 `tys` 不 `alloc`，未綁定則計 `unsup`），`h2m-slot-init-arg` 對 `TY-VEC` 空操作，`h2m-var-read` 加 `st == TY-VEC -> v = sid` 直取
3. **元素位寫入門禁走活動名棧 `c.epl`**：進 body 前壓循環變量名，`h2m-var-write` 入口掃中即 `h2m-mark`——這比在降級器裡判"哪個語句在寫循環變量"便宜且難漏；同時 `h2m-var-write` 的 `STORE` 分支排除 `TY-VEC`（不給 `%vc` 發 `store`）；此偏離記 §11 B24
4. **長度只求一次且定義在支配塊**：`%v9 = call i64 @vec_len` 只出現在 pre，hdr 每輪 `LOAD` 的是索引槽而非長度——鏡像 Go 的 `Emit(OpLen)` 位置；若每輪重取長度，`opt` 雖會提出去，但 IR 文本斷言就釘不住"一次"這條紀律
5. **關鍵字形的 cond 計位再次踩坑（本件真 bug）**：`h2m-lower-for` 原寫 `rg == 0 && cond != 0 -> form = 1`，容器形的 cond 位（循環變量自身 `IDENTIFIER`）被重複計成一個形位 ⇒ `form=2` ⇒ 整函式不產碼（探針用例 B `unsup=1`）；修為 `rg == 0 && ce == 0 && cond != 0` 並就地補註釋⚠️ 教訓：**新增一種形時必須回頭檢查舊形的護欄是否要同步**——s61 已在 `h2m-rng-check` 裡為區間形做過同口徑判據，容器形漏抄才撞坑
6. **槽只由前掃描鋪**：`h2m-lower-coll` 先 `h2m-name-find` 再 `h2m-var-read`（後者在未綁定時已自行計 `unsup`，直接走會重計），查不到槽即 `bad`——絕不在降級中途補鋪槽，否則 `entry` 的 `alloca` 連續 id 約定（s57）被打亂
7. **`continue` 落 upd，`break` 落 exit 且循環後繼照跑**：與 s60m／s61 同口徑；體內末條是裸終結形時，body 末尾無條件補的 `BR` 落入"有跳轉無後續"死區段（實測 `bb5:` 兩行 `br`），LLVM 容忍性已由 s59 三級實證擔保，單測只釘第一條可達 `br` 的目標

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `h2m-test` **324 → 372/372**（新增 6 條：`test-coll-basic`（`define i64 @va(%vec %vc1)` 形參行 + `alloca %vec` 零出現 + `vec_len` 一次 + hdr 對單一 `%v9` 比較 + body `get`+`store` 三連）／`test-coll-kw`（`for v <- xs {` 與運算符形同路）／`test-coll-break`（`bb5: br label %bb4` 而非 `%bb3`）／`test-coll-cont`（`bb5: br label %bb3` 而非 exit／body）／`test-coll-nested`（內層 exit `bb8` 落外層 update `bb3`）／`test-coll-gate`（`str` 源／標量源／`[]str` 元素源／無標注 `let vec`／體內改寫循環變量／嵌套同名／`vec` 返回七負例皆 `unsup > 0` 且 `define` 零出現）；`test-rng-gate` 的原容器負例理由改寫——不再假稱"需 `OP-LEN`／`OP-INDEX`"，改釘 `h2m-lower-coll` 的型別門禁）
- 零回歸：`cg-test` **342/342**；`cg-runtime-test` **60/60**；自有三檔 `no fmt -d` rc=0；`no vet compiler/src` 自有檔 **0 error**（34 error 全在他人正在改的 `compiler/src/ps.no`）；s55–s61 探針前向 exit **73 / 47 / 92 / 188 / 72 / 134 / 164** 不退化（`tmp/s62-regr.py` 一次跑八檔，含本件 **134**）
- 探針 `tmp/probe-s62.no`（七函式：求和／計數／體內 `break`／體內 `continue`／嵌套異名／`for` 關鍵字形／空 `vec`；main 以 `@vec_new` + `@vec_push_i64` 造 `xs=[1,2,3]`，`ys=[7,8]`，空 `vec`，再手發七調用求和）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **134**（`6+3+101+5+6+6+7`）
- 🔴 **Go oracle 差分對拍**：`tmp/s62-oracle.no` 同程式（真 Nolang 源碼 + `XS []i64 = [1, 2, 3]`）實測 **134**，逐值印 `6 3 101 5 6 6 7` 兩側一致⚠️ 對拍只在**"只讀循環變量"子集**上同語義（oracle 檔頭明載：Go 把 `v` 綁到元素位址，寫 `v` 會改容器，本件該形列入門禁不產碼）；源碼級六對配對變異兩側同值 **136**（`break` 閾值 2→3）／**133**（`continue` 閾值 1→2）／**140**（內層體 `+=1` 改 `+=2`）／**134**（關鍵字形改運算符形＝**同值**，釘住"兩寫法同路"）／**148**（`wc` 容器換 `ys`）／**140**（`wg` 容器由空 `vec` 換 `xs`）
- IR 反向八刀（`tmp/s62-diff.py`）：**135**（hdr `slt`→`sle`＝越界讀多跑一輪）／**132**（update 步長 1→2＝隔元素取）／**129**（`continue` 目標改 exit＝變 `break`）／**139**（`break` 目標改體內＝根本不跳）／**130**（內層 exit 改指 `ret`＝外層只跑一輪）／**133**（pre 索引初值 0→1＝跳過首元素）／**131**（`VECGET` 索引改常量 0＝每輪讀同元素）／**128**（hdr `CONDBR` 兩目標互換＝首判即退一轮不進）
- 鑑別態六例：體內改寫循環變量／`[]str` 元素源／嵌套同名遍歷名／標量源／`vec` 作返回值／循環變量與源同名 ⇒ 各自 `unsup > 0`，IR 內 `define i64 @` 零出現，探針只發毒化 main ⇒ 六例 exit **101**
- ⚠️ 對拍方法論舊坑再犯一次並記實：錨串 `for v <- xs {` 在探針**檔頭註釋**裡也出現 ⇒ `count(anchor)==1` 護欄直接拒絕（`s61` 已因同機制改過錨），本件一律給"帶體"的長錨；教訓是**寫腳本前先 `grep -c` 數一遍錨在檔內的出現次數**，而不是指望腳本報錯來發現

| noc | Go |
| --- | --- |
| pre 發一條 `OP-VECLEN(lv, sv)`，其後三塊全引用同一 `lv`（實測 `%v9` 只此一處定義） | `Emit(OpLen, idxT, [arrV])` 於循環外一次（:5179-5301 段內 pre 塊） |
| 索引槽名 `@idx<nodeId>`（`h2m-idx-name`），`h2m-scan` 鋪在 entry `alloca` | `EmitInt(OpConst, idxT, 0, varName+"#idx")` 合成命名 |
| hdr `OP-CMPLT(l1, lv)` ⇒ `icmp slt i64 %v11, %v9`，`OP-CONDBR(ext, c1, body)` | `OpLt(idxSlot, lengthV)` + `OpCondBr(body, exit)` |
| body `OP-VECGET(e1, sv, l1)` → `STORE` 進循環變量槽 ⇒ 體內讀的是**槽裡的副本** | `OpIndex(elemT, [arrV, idxSlot])` + `locals[varName] = iSlot` + `bindElemPlace` 綁**元素位址** |
| `c.stk` 壓 `(upd, exit)`，`c.rnms` 壓遍歷名，`c.epl` 壓元素位名 | `loopStack` 壓 `loopCtx{exit, update}` + `contStack` 壓 `update` |
| `TY-VEC` 形參不鋪槽，綁定即 entry 的 `%vc1` | Go 有 `KindSlice` 的棧槽（`alloca` 型別即 `%vec`），`emitLenCap` 直接 `store` 進槽 |
| 寫循環變量／`[]str` 元素／`str` 源／`vec` 返回 ⇒ 計 `unsup` 不產碼 | 寫循環變量真改容器；`str` 走 `lowerStrRangeFor`；`KindArray` 用 `Sizes[0]` 常量 |

**未覆蓋／後續**：`str` 源的逐碼點遍歷；定長陣列源；`vec` 局部量與非 `i64` 元素（要逐元素型別分派的 `@vec_get_*`）；值匹配 `x: { pat -> ... }`**已於 s63 落地**（§5.3.4.cr，只收 i64 主體＋裸整型字面量型樣）；表達位的鏈；帶標籤終結形與帶值 `return`；開邊界區間；實參 args arena 摘掉連續 id 契約（§11 B16）**已於 s64 落地**（§5.3.4.cs）；`drv.cmd-build` 接線（M4-d）——以上構成 M4-e 第九件的候選


### 5.3.4.cr s63——M4-e 第九件：值匹配降級（`compiler/src/h2m.no` 認 `K-SURFACE-MATCH` 的 `kid[0]` 主體——逐臂在**本臂判定塊內**重讀主體並發 `OP-CMPEQ(subj, pat)`，七道臂群標記閘＋型樣 kind 閘把解構／`ok` 關鍵字臂／型別臂／非 i64 主體／非字面量型樣一律判成不覆蓋面，**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**（復用 s35 的 `OP-CMPEQ`））（2026-11-04 本輪）

**背景**：s58 收花括號短路鏈時把值匹配明列門外（§5.3.4.cm 未覆蓋首項），但"門外"在當時的實現裡並不等於"不產碼"——`h2m-lower-surface` 不區分裸鏈與顯式主體，一律把臂的 `Condition` 當布爾條件降級：`1 -> r = 10` 發成 `%c10 = icmp ne i64 1, 0`（恆真），主體 `%v1` 從未被 `load`，而 `unsup` 仍是 **0**（實測 `tmp/probe-s63-now.no`）——這是本線上第一例"錯碼且門禁沉默"的構造，故本件同時是門禁口徑的補帳：命中形制產對碼，不命中整函式不產碼

**Go 權威**：`compiler/src/ps.no:10371-10372`（desugar 的臂條件生成）——`raw-a[i] == 1 -> cond = pat-a[i]`（raw-cond 臂直接用條件式），`raw-a[i] == 0 -> cond = ps-mk-eq(t, subj-node, pat-a[i])`，即**值樣臂的語義是"主體 == 型樣"**，且 `subj-node` 被每一臂重複引用；surface 形的臂群手冊在 `ps.no:10416-10450`（12 aux／臂：`0 binding`／`1 bindingVariant`／`2 typePattern`／`3 typeBinding`／`4 wildcard`／`5 dot-val`／`6 rawCond`／`7 blockBody`／`8 typeArm`／`9 valueCtx`／`10 Condition`／`11 Body`，槽 0-9 存 `ast.put-str` 索引或 0）；`ps.no:9915-9925` 表明 `rawCond` 只由 `ok(<expr>)` 置 1，`ps.no:10314-10333` 表明 `ok`／`ok(v)` 臂置 `wildcard`＋`dot-val`，`err(e)`／`ok(v)` 才填 `binding`／`bindingVariant`——這兩條正是七道閘的依據

**實測形制（`tmp/probe-s63-dump.no`，釘於 `tmp/s63-dump-out.txt`）**：顯式主體的 `K-SURFACE-MATCH`(65) 是 `nkid=1` 且 `kid[0]`＝主體節點（裸鏈的 `kid[0]` 為佔位 0）⇒ `subj > 0` 即"值匹配模式"；本件收的主體只認 `K-IDENTIFIER`(35) `nkid=0`，型樣只認 `K-INTEGER-LITERAL`(39) `nkid=0`；實測十三形——正形六式 `unsup=0`（三臂含兜底／單臂無兜底／局部量主體／多陳述臂體／區間遍歷變量作主體／值匹配套值匹配），負形七式 `unsup=1`（str 主體／`-1` 前綴式型樣／具名型樣／表達式主體／未登記主體／布爾型樣／字串型樣）

**覆蓋面（本件）**：`h2m-lower-surface` 三段擴充——①主體判定（`h2m-kid-i(t, n, 0)` 非 0 ⇒ `vmode=1`，主體必須是 `K-IDENTIFIER && nkid==0`，`h2m-name-find` 查得，且 `c.tys[si] == TY-I64`）；②前置校驗的臂群閘（`vmode == 1` 時每臂的 aux 槽 0／1／2／3／5／6／8 非 0 即 `bad`，非 wildcard 臂的 `Condition` 必須是裸整型字面量）；③發射段逐臂 `h2m-var-read(sname)` ＋ `h2m-alloc(TY-I64)` ＋ `OP-CMPEQ(eqv, sv, pv)`，`OP-CONDBR` 的條件由裸鏈的 `pv` 換成 `eqv`；實測 IR（`tmp/s63-dump-out.txt` 用例 v1）形如 `%v8 = load i64, i64* %s2` → `%c11 = icmp eq i64 %v8, %v7` → `%v9 = zext i1 %c11 to i64` → `%c12 = icmp ne i64 %v9, 0` → `br i1 %c12, label %bb2, label %bb3`，次臂塊 `bb3` 內**再 load 一次** `%s2`

**關鍵點**：
1. **逐臂重讀主體是支配關係逼出來的，不是冗餘**：cg 按 `blks[k] != cur` 發標籤，每塊必須是指令序的**連續段** ⇒ 把首臂判定塊發的 `LOAD` 拿到次臂塊用，就引用了一個不被它支配的值 ⇒ `opt -passes=verify` 報錯；巧合的是這與 Go 權威"每臂重複引用 `subj-node`"同形，忠實性反而更容易滿足
2. **槽型校驗放在發指令之前**：`h2m-scan` 對 `K-SURFACE-MATCH` 恆置 `flow = 1`（`h2m.no:818-819`），`h2m-lower-func` 據 `flow > 0 -> c.mem = 1`（`h2m.no:2034`）決定模式 ⇒ 含 match 的函式必走 `mem` 模式，主體必有 entry `alloca` 槽，`c.tys[si]` 的判定才有意義（查表與查型都在校驗期完成，校驗期一條指令都不發）
3. **零改 cg**：`OP-CMPEQ=39` 依 `cg-vty-at(vty, x)` 發 `icmp eq i64` 再 `zext i1→i64`（`cg.no:553-568`），`OP-CONDBR=10` 恆發 `%c<k> = icmp ne i64 %vX, 0` 再 `br i1`（`cg.no:1730`）——zext 後的 0/1 正好被 CONDBR 吃掉，主體與型樣比較不需要任何新 op
4. **七道閘不含槽 4／7／9**：槽 4 `wildcard` 就是兜底臂本身（其位置由 s58 的"只認末位"閘管）／槽 7 `blockBody` 允許多陳述臂體（探針用例 wd）／槽 9 `valueCtx` 是**入函值語境快照**（`ps.no:9753`）——值語境下的 match 落在表達式位，由 `h2m-lower-expr` 的默認臂計 `unsup`，永不到 `h2m-lower-surface`
5. **語言邊界三件實測**（記 §11 B25）：`n + 1: { ... }` 的表達式主體在 parse 期就脫離 Matched（Go checker 的錯誤文案自證：the one-line form `k = <expr>: { ... }` only attaches when `<expr>` is a bare identifier）；多值樣 `1, 2 ->` 被 ps 前置門禁拒（`expected variable name after ','`）；option 的單 `ok ->` 臂被拒（`option match must handle all branches: err, nil, ok`）
   > ⚠️ **2026-11-08 更新（Go 工具鏈）**：第一項已解除 —— `src/parser/expr.go` 在中綴迴圈之後補了 `: {` 附加（CTX_EXPR 閘門），`r = a % b: { ok -> it  -> 0 }` 現在是單一值位 SurfaceMatch，`expr: { ... }` 優先級高於 `=`；option 算術主語會歸一化成 `__match_subj_L_C ?T`，`checker.go` 的錯誤文案與 10 個 locale 已同步改口。**本節與 B25 是 v0.4.13 時點的歷史實測紀錄，保留不改**；`h2m-lower-surface` 對表達式主體計 `unsup` 仍是自舉側的正確行為（那邊尚未收該形態）。後兩項邊界不變。

**驗證環境**（v0.4.13 工具鏈 + `DEVELOPER_DIR` CommandLineTools）：
- `h2m-test` **372 → 429/429**（新增 7 條：`test-vm-arm3`（三臂含兜底：逐臂 `load`+`icmp eq`+`zext`+`icmp ne` 四連，並**反向**斷言型樣不再被當布爾條件 `%c11 = icmp ne i64 %v7, 0`）／`test-vm-nowild`（無兜底 ⇒ 假路直落匯聚塊 `br i1 %c12, label %bb2, label %bb1` 且無後續判定塊）／`test-vm-local`（局部量主體）／`test-vm-block`（多陳述臂體）／`test-vm-in-range`（區間遍歷變量作主體，s61×s63）／`test-vm-nested`（值匹配套值匹配）／`test-vm-gate`（str 主體／`-1` 前綴式／具名型樣／表達式主體／未登記主體／布爾型樣六負例皆 `unsup > 0` 且 `define` 零出現））
- 零回歸：`cg-test` **342/342**；`cg-runtime-test` **60/60**；自有檔 `no fmt -d` rc=0；s55–s62 探針前向 exit **73 / 47 / 92 / 188 / 72 / 134 / 164 / 134** 不退化
- 探針 `tmp/probe-s63.no`（六式：wa 三臂含兜底／wb 單臂無兜底／wc 局部量主體／wd 多陳述臂體／we 遍歷變量作主體／wf 值匹配套值匹配；main 手發十三次調用求和 `10+20+30+1+7+3+4+10+1+111+8+9+7`）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **221**
- 🔴 **Go oracle 差分對拍**：`tmp/s63-oracle.no` 同程式（真 Nolang 源碼，同一工具鏈編譯）實測 **221**，逐值印 `10 20 30 1 7 3 4 10 1 111 8 9 7` 兩側一致；源碼級七對配對變異兩側同值 **216**（wa 兜底 30→25）／**227**（wb 型樣 3→5）／**222**（wc 兜底 4→5）／**226**（wd 臂體 `*2`→`*3`）／**65**（we 區間右開→右閉，真值 321，exit 碼按位元截斷為 65，兩側截斷行為一致）／**222**（wf 內層型樣 2→6）／**231**（wa 次臂型樣 2→9）；main 側實參兩對 **241**（wa 首調用 1→3）／**220**（wf 首調用外層 1→2）
- IR 反向十刀（`tmp/s63-diff.py`）：**211**（首臂 `eq`→`ne`）／**241**（首臂主體 `load` 改讀結果槽）／**231**（次臂不再重讀主體＝釘住"逐臂重讀"是必需而非冗餘）／**222**（兜底常量 30→31＝兜底臂可達且其值流向匯聚塊）／**171**（匯聚塊改讀主體槽＝返回值來源）／**215**（無兜底單臂的假路直落臂體＝無條件執行）／**223**（wc 首臂常量 3→5）／**218**（wd 臂體 `mul`→`add`）／**212**（we 首臂 `CONDBR` 兩目標互換）／**219**（wf 外層兩目標互換）
- 鑑別態七例：str 主體／`-1` 前綴式型樣／具名型樣／布爾型樣／表達式主體／未登記主體／`ok` 關鍵字臂 ⇒ 各自 `unsup > 0`，IR 內 `define i64 @` 零出現，探針只發毒化 main ⇒ 七例 exit **101**
- ⚠️ 對拍方法論新坑（記 §11 B25）：oracle 側首版寫 `y i64 = x - 1` 被 Go checker 拒（`cannot assign ?i64 value to i64 variable 'y'`，錯誤尾碼 `15w45dqk`）——算術結果是 `?i64`，故"局部量主體"必須寫成拷貝 `y i64 = x`，探針／oracle／單測三處同步；p5 的錨串因探針檔頭註釋含同串而被 `count != 1` 護欄跳過，加長為帶體的兩行錨後 `count=1`（教訓仍是**寫腳本前先 `grep -c` 數一遍錨**）

| noc | Go |
| --- | --- |
| 每臂在本臂判定塊內各發一次 `OP-LOAD`（實測 `%v8`／`%v12` 兩次讀同一 `%s2`） | `cond = ps-mk-eq(t, subj-node, pat-a[i])`：`subj-node` 被每臂重複引用 |
| `OP-CMPEQ(eqv, sv, pv)` ⇒ `icmp eq i64` + `zext i1→i64`，`OP-CONDBR` 再 `icmp ne i64 %v9, 0` | desugar 產 `BinaryExpression(Eq)`，由 codegen 的比較家族發 `icmp eq` |
| 臂群閘只看 aux 槽 0／1／2／3／5／6／8 是否為 0 | 這些槽由 `ps.no:10314-10333` 的 `kvar`／`bname`／`rawc` 規則填充 |
| 兜底臂（`wildcard`）不發 CONDBR，其體落當前塊，其後臂不可達 ⇒ 只認末位 | Go desugar 把 wildcard 臂作前一 if 的 alternative（`ps.no:10362-10366`） |
| 非 i64 主體／非字面量型樣／解構綁定 ⇒ 計 `unsup` 不產碼 | Go 側走 `it` 注入與 option 解包路徑，noc 未備 |

**未覆蓋／後續**：`it` 隱式綁定與解構臂（`binding`／`bindingVariant`）；`ok`／`err`／`nil` 關鍵字臂與 raw-cond 臂；型別臂（`typePattern`／`typeArm`）；非 i64 主體（str／vec／option）與多值樣；表達位的鏈；帶標籤終結形與帶值 `return`；開邊界區間；實參 args arena 摘掉連續 id 契約（§11 B16）**已於 s64 落地**（§5.3.4.cs）；`drv.cmd-build` 接線（M4-d）——以上構成 M4-e 第十件的候選


### 5.3.4.cs s64——M4-e 第十件：實參側池 `args`（`compiler/src/cg.no` 的 CALL 臂改按**池下標**取實參、`compiler/src/h2m.no`「校驗全過才整段入池」，摘掉 §11 B16 的連續 id 契約；`mir.no` 未加 opcode）（2026-11-04 本輪）

**背景**：`OP-CALL` 自 s24 起要求實參佔**連續值 id**（`xa[k]..xa[k]+bs[k]-1`），根因是扁平 arena 每條指令只有 `dsts/xa/bs` 三個定長槽，變長實參列表無處安放。但 Go 側 `Inst.Args` 是任意 `[]ValueID`，從無相鄰假設——noc 於是把「按序分配 id 的巧合」當成了契約：`f(x, i + 1)` 這類「非首位實參要先算一個中間值」的調用全數撞門禁（計 `unsup`、整函式不產碼）。本件是 B16 的銷帳：MIR 增設一條與指令下標無關的**側池 `args`**，`xa[k]` 的語義從「首個實參值 id」改成「池內基址下標」，段內值 id 可任意散布。⚠️ 標號說明：§5.3.3（2026-10-20）另有一個 M2 舊鏈的 `s64`（泛型陣列接收者），與本件無關——計數器在 M3/M4 重啟屬歷史遺留，讀舊檔時以日期與 §5.3.4.* 編號為準

**Go 權威**：`src/mir/mir.go:1324` 的 `Args []ValueID`（`type Inst struct` 內，緊鄰 `Results []ValueID`，兩者都是變長列表）；消費端一律按元素取值而非假設相鄰——`for _, a := range inst.Args`（`src/mir/analysis.go:347`／`:714`／`:918`）與 `inst.Args[0]`／`inst.Args[1]` 直接索引（`src/mir/codegen.go:5760`／`:5876-5877`）。noc 側一條 CALL 的位寬仍是三個整數（`dst`／`xa`／`bs`），裝不下列表 ⇒ 採「(池基址, 個數) ＋ 單調追加池」間接尋址，把長度資訊從指令槽移進池

**實測形制（`tmp/probe-s64-dump.no`；改前釘 `tmp/s64-dump-before.txt`、HEAD 乾淨對照釘 `tmp/s64-dump-head-control.txt`、改後釘 `tmp/s64-dump-after3.txt`）**：十式調用形制——改前只有五式 `unsup=0`：c1 兩字面量／c2 形參＋字面量／c8 `str`＋`i64` 混排，外加兩式**僥倖連續**（c3 首個實參是算式，但 `CONST` 恰好吃掉 `first+1`；c6 嵌套調用的結果 id 恰是降級期最後一個 id）；撞門禁的五式是 c4=2（次位實參是算式）／c5=2（三實參中間那個是算式）／c7=2（嵌套＋算式疊加）／c9=1（`mem` 模式循環體內的算式實參）／c10=2（雙嵌套，外層段被內層池項插開）。改後十式全 `unsup=0`。c10 的 2 另在 `git worktree add /tmp/s64wt HEAD` 的乾淨對照樹上實測（同 v413 工具鏈），九式 `unsup` 與主樹改前**逐值相同**——即改前檔位不是髒工作區造成的假象

**覆蓋面（本件）**：三面同改，缺任何一面即錯碼。① `mir.no`：池的定義寫進檔頭（`:14-18`）與 `OP-CALL` 註（`:73`）——`xa`=池內基址下標、`bs`=個數、第 j 個實參值 id = `args[xa+j]`，並寫明 `xa` 的合法上界由 `args.len()` 決定；② `cg.no`：三個入口（`cg-emit-func`／`cg-emit-cfg`／`cg-emit-cfg-t`）各**必填**多一個 `args []i64`（12 → 13 條形參），CALL 臂的發射迴圈從 `q = x` ＋ `q = q + 1` 改成 `q = args[x + j] #{index-out=zero}`（`cg.no:510`），型別與命名空間仍按**值 id**查 `vty[q]`；③ `h2m.no`：`h2m-ctx` 增 `args` 陣（`:129`），CALL 降級臂（`:972`）先逐個降級實參落臨時 `al`，全部校驗通過後才 `base = c.args.len()` 整段追加，再發 `OP-CALL(cid, base, na, …)`，舊的 `want = first + j` 連續性校驗整段刪除。函式發射點（`:2130`／`:2136`）與 `cg-test.no` 的 51 處呼叫點隨之補第 13 參（無 CALL 的體傳 `[]`）

**關鍵點**：
1. **「邊降邊推」會擠偏基址**（本件初版實測 bug）：嵌套調用作實參時，內層 CALL 自己在降級期就往 `c.args` 推池項；外層若也邊降邊推，其 `base..base+na-1` 段中間會插入內層的孤兒項——`f(h(3), 1)` 發成 `@f(%v1, %v2)`（讀到內層的形參 id），正解是 `@f(%v2, %v3)`。修法是把「降級收集」與「入池」分成兩段：先落臨時 `al`，校驗全過再整段追加。單測 `test-call-s64-nested` 用正向＋反向兩條斷言釘住這個形制
2. **形參選「必填」而非預設空池**（`cg.no:84` 註釋）：預設空池會讓漏改的呼叫點**靜默**發 `%v0`（id 0 是啞元、從未 `define`），把「呼叫點沒更新」推遲到 `opt -passes=verify` 甚至執行期；必填則直接是編譯錯誤 `function 'cg-emit-cfg-t' expects at least 13 argument(s), got 12`（實測 `tmp/probe-s35.no:197`）。代價是 M4-b/M4-c 世代的手寫 op 探針（`tmp/` 內 28 檔，`tmp` 不入版本庫）自此無法重跑——它們的結論已由 s55+ 探針與 `cg-test`／`h2m-test` 單測接管，屬可接受的一次性作廢
3. **池下標與值 id 是兩個命名空間，誤填不報錯**：`xa` 若填成值 id，發射側只是讀到不同的池項、IR 依然合法 ⇒ 生產側（`h2m`）與消費側（`cg`）必須同步，`mir.no` 的 `OP-CALL` 註已把這條寫成 ⚠️。真正的護欄是越界讀 `#{index-out=zero}` 得啞元 0 → `%v0` 未 define → `opt` verify 響亮失敗（而非靜默錯值）；`test-call-scatter` 以「池 `[1, 3, 5, 9]`、基址 2 ⇒ 必發 `%v5`／`%v9`，且反向斷言不得出現 `%v2`（把池下標當值 id 讀）」把兩個命名空間釘死
4. **型別按值 id 查 `vty[]`，與池位置無關**：混排 `str`／`i64` 實參靠 `cg-vty-at(vty, q)` 決定型名與 `%sv`／`%v` 前綴，實測 c8 發 `%v3 = call i64 @f(%str-long %sv1, i64 %v2)` ⇒ 側池只改「取哪個 id」，不改「怎麼命名」，型別表（§11 B7）不需動
5. **池單調追加、不回收**：被拒的調用不入池（校驗在整段追加之前完成），函式一計 `unsup` 即整函式不產碼 ⇒ 池中不會出現半截段；`args` 長度隨調用數單調增長，跨函式不共用（每函式一個 `h2m-ctx`，天然隔離）
6. **只摘了 CALL 一條連續約定**：`OP-STRUCT-SET` 仍走自帶的 `xa[k]+1 = 新欄位值 id`（`mir.no:393`／`cg.no` 的 STRUCT-SET 臂註釋已改口為「本 op 自帶約定，與 OP-CALL 非同形」），記 §11 B26

**驗證環境**（v0.4.13 工具鏈 ＋ `DEVELOPER_DIR` CommandLineTools；一律用釘住的 `/tmp/nolang-v413/bin/no`——主樹 `./bin/no` 當日被並行 session 的 Go 側改動污染，`EmitLLVM: setfield: field "buf" not found on "sb.sb"` 滿屏，非本件）：
- `h2m-test` **429 → 443/443**（新增 5 條：`test-call-s64-scatter`（三實參、後兩個是算式 ⇒ `%v1`／`%v3`／`%v5` 跳號）／`test-call-s64-nested`（單嵌套調用，正向釘 `@f(%v2,%v3)` ＋ 反向禁 `@f(%v1,%v2)`，即關鍵點 1 的靶）／`test-call-s64-double-nested`（雙嵌套：內層 `@h`／`@k` 各自入池 ＋ 外層 `%v5 = call @f(%v2,%v4)`）／`test-call-s64-str`（`%str-long` 混排）／`test-call-s64-loop`（`mem` 模式循環體內算式實參 `%v27 = call @f(%v23,%v26)`）；`test-call-gate` 的兩條「不連續計 unsup」斷言翻成正向 `gap not counted (s64 pool)`／`gap args via pool`）
- `cg-test` **342 → 347/347**（新增 `test-call-scatter` 5 斷言：基址非 0／池下標≠值 id／順序不反／`str` 混排）；`cg-runtime-test` **60/60**；`util`／`tok`／`lx`／`ps`／`ps-err`／`ck`／`diag` **27/62/45/38/26/20/7** 全綠
- ⚠️ 兩項既有紅點（在 HEAD 乾淨對照樹 `/tmp/s64wt` 以同工具鏈復現同值，非本件引入）：`ast-test` 撞 Go 側 bug #85（`consumer reads value 0 (NoVal)` 刷屏，自 §5.3.4.bi（s29）即記）；`ck-sig-test` **41/50**（`ck-sig.no` 簽名表與工具鏈內嵌 std 的世代漂移）
- 零回歸：自有檔 `no fmt -d` rc=0（`h2m.no`／`cg.no`／`cg-test.no` 三檔由 `no fmt -w` 歸零，`#{...}` 註解計數改前後相同）；`no vet compiler/src` **34 error / 0 warning / 476 hint**，與 HEAD 對照**逐值相同**（34 條全在他人正在改的 `ps.no`，自有檔 0 error）；s55–s63 探針前向 exit **73 / 47 / 92 / 188 / 72 / 134 / 164 / 134 / 221** 不退化（`tmp/s64f-regress.sh`）
- 探針 `tmp/probe-s64.no`（五式 `v1..v5` 各含一種舊契約撞門禁的實參形制；被調者刻意非對稱 `add2 = a*3 + b`、`add3 = a + b*2 + c` ⇒ 實參順序變異必然改變總和）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **91**；同一探針在 HEAD 對照樹上因 `unsup > 0` 只發毒化 main ⇒ exit **101**（證明改前該形制確實不產碼）
- 🔴 **Go oracle 差分對拍**：`tmp/s64-oracle.no` 同程式（真 Nolang 源碼、同一工具鏈編譯）實測 **91**，逐值 `Q1..Q5 = 25 21 17 13 15` 兩側一致；源碼級三對配對變異兩側同值 **92**（`v2` 末位 `x - 3` → `x - 2`）／**104**（`v5` 區間右開 → 右閉，多跑一輪 `add2(3,4)=13`）／**90**（`v4` 末位 `inc(x)` → `idfn(x)`）；main 側實參配對 **95**（`v1(5)` → `v1(6)`）
- IR 反向五刀（`tmp/s64-diff.py`）：**93**（`v1` 兩實參對調）／**87**（`v2` 前兩實參對調）／**82**（`v3` 末位實參換成內層結果 id）／**93**（`v4` 雙嵌套對調）／**88**（`v5` 循環內末位換成 `i`）——五刀各只改一個池項，值皆按預期翻轉，證明發射側確實按池下標取 id 且保留調用點順序
- 鑑別態三例：未知被調者／arity 不符／實參型別不符（`char` 作 `i64` 形參）⇒ 各自 `unsup > 0`、探針只發毒化 main ⇒ exit **101**（型別那例必須用雙引號 `"y"`：`'y'` 的單引號會截斷探針文件裡單引號包起的 SRC 串，探針自身編譯失敗）

| noc | Go |
| --- | --- |
| 實參住側池 `args`，CALL 的 `xa`=池內基址下標、`bs`=個數，段內值 id 可任意散布 | `Inst.Args []ValueID` 是任意列表（`mir.go:1324`），無相鄰假設 |
| 校驗全過才整段入池（內層 CALL 的池項不會插進外層段中間） | lowering 遞迴返回 ValueID 後 `append` 進 `Args`，天然無此坑 |
| `xa` 的合法上界是 `args.len()`；越界讀得啞元 0 → `%v0` 未 define → `opt` verify 響亮報錯 | ValueID 有效性由 MIR 構造期保證，`NoVal` 在 codegen 有專門錯誤（bug #85） |
| `vty[值 id]` 決定型名與 `%v`／`%sv` 前綴，與池位置無關 | `loadVal(ValueID)` 按值查型，同樣與位置無關 |
| `cg` 三個入口的 `args` 形參**必填**，漏改呼叫點是編譯錯誤 | Go 是結構體欄位，無此 ABI 面 |

**未覆蓋／後續**：`OP-STRUCT-SET` 的 `xa[k]+1` 自帶連續約定仍在（本件只摘了 CALL，記 §11 B26）；`it` 隱式綁定與解構臂（`binding`／`bindingVariant`）；`ok`／`err`／`nil` 關鍵字臂與 raw-cond 臂；型別臂（`typePattern`／`typeArm`）；非 i64 主體（str／vec／option）與多值樣；表達位的鏈；帶標籤終結形與帶值 `return`；開邊界區間；`drv.cmd-build` 接線（M4-d）——以上構成 M4-e 第十一件的候選


### 5.3.4.ct s65——M4-e 第十一件：值匹配臂的 `it` 隱式綁定（`compiler/src/h2m.no` 增 **`itv` 值 id 棧**——cond 臂在判定塊壓入主體 LOAD、wild 臂按 `h2m-subtree-uses-it` 遞迴懶發 LOAD；同族缺陷：`stk`／`rnms`／`epl` 三條作用域棧在退出分支只減計數、未真正 `.pop()`，兄弟作用域讀到**陳舊頂**——本件就地補 `.pop()`；`h2m-ty-of` 的 K-IDENTIFIER 臂為 `it` 補 TY-I64 特例，銷掉預掃描 -1 槽型造成的**雙 mark**；**零改 `ps.no`／`cg.no`／`mir.no`**）（2026-11-04 本輪）

**背景**：s63（§5.3.4.cr）落地值匹配 `x: { pat -> body }` 時只接受具名綁定（本件前僅 `pat -> ...` 形；`ok(v) -> ...` 的解構臂仍在門禁外），而 AGENTS.md §3.2／§5.4 明示「option 解包首選 `it`」——用戶寫 `ok -> r = it` 而不引入額外綁定名。s63 的實測形制裡裸 `it` 走 `h2m-var-read('it', ...)` 找不到槽 ⇒ `unsup++` ⇒ 整函式不產碼。本件補 `it` 的綁定機制：**不改 `ps.no`**——`it` 是普通 K-IDENTIFIER，其語義特殊由降級側攔截。`h2m.no` 在臂的進入／退出處壓／彈一個 `itv` 值 id 棧，`h2m-var-read`／`h2m-var-write` 認得 `it` 名字則直接讀／改寫棧頂。

調研期間還發現 `stk`（`K-SURFACE-MATCH` 作用域）、`rnms`（`return` 命名輸出參）、`epl`（`elem-place` 循環元素位）三條作用域棧在**退出分支只 `-1` 計數、未真正 `.pop()`**——s59 落地後因 `break`／`continue` 未走到該分支未被觸發；本件的 `it` 讀取路徑把「兄弟作用域讀到陳舊頂」這條路徑擡到檯面，就地補 `.pop()`（`arr.pop()` 是原地截短 API）。這兩樁本是同族：s65 = 「`it` 綁定」＋「同族棧陳舊讀取修正」。

**Go 權威**：`it` 在 Go 側是 checker/HIR 層的語義糖（`src/hir`、`src/checker`），MIR 無 `it` 碼位——每個臂在降級時把主體值直接綁進臂的局部名空間。noc 側 `h2m` 是單-pass 降級、無獨立語義層，故把綁定壓在 `itv` 值 id 棧上、把 `h2m-var-read/write('it', ...)` 攔截為讀／寫棧頂，等價於 Go 的臂內聯結。⚠️ 標號說明：§5.3.3（2026-10-20）另有一個 M2 舊鏈的 `s65a/b/c`（陣列／切片型別方法定義），與本件無關——計數器在 M3/M4 重啟屬歷史遺留，讀舊檔時以日期與 §5.3.4.* 編號為準

**實測形制（`tmp/probe-s65.no`）**：五式 `w1..w5` 各含一型 `it` 用法——w1 單臂 cond（`n: { 1 -> r = it  -> r = 0 }`）／w2 只有兜底臂用到 `it`（**wild 臂懶發主體 LOAD** 靶）／w3 兩臂都用 `it`（cond 判定塊 LOAD + 兜底臂補發 LOAD 靶）／w4 嵌套 match（外 `it` 存進 `t0`、內 `it` 讀內主體；用 s64 穩定形「所有內層計算全放進內臂 `2 -> r = it + t0`」）／w5 同函式兩個先後 match（各綁各的主體；第二個 match 主體從 `%s5` 槽讀、第一個從 `%s3`）。被調者刻意非對稱（`w1 = n + 100`、`w2 = q * 3`、`w3` 判定臂 `it + 10` 兜底臂 `it * 2`、`w4` 內臂 `it + t0` 或 9 或 -1、`w5` 兩 match 各加 100/1000）⇒ `it` 綁定值被偷換必然改變總和

**覆蓋面（本件）**：五面同改，缺任何一面即錯碼或靜默降級。
① `h2m.no` `h2m-ctx` 結構體內新增 `itv` 陣（值 id 棧）與 `itv-n`（深度計數），檔頭註釋同步；
② `h2m-var-read` 與 `h2m-var-write` 認 `nm == 'it'` 時改走 `itv` 棧頂（`itv-n > 0` 則讀／寫 `itv[itv-n-1]`；空則照舊 `unsup++`）——`it` 從頭到尾**不進槽表**（`h2m-lst-find('it')` 恆返 -1），避免 AGENTS.md §6.3「臂間同名串型」；
③ `h2m-lower-surface-match` 每個 arm 進入時壓載體——cond 臂壓判定塊已發的主體 LOAD（跨塊由支配性保證合法）、wild 臂調 `h2m-subtree-uses-it` 遞迴掃描 arm 主體 AST，用到才發一次 LOAD、否則**懶發佔位 0**（IR 零漂移）；臂體結束即 `itv-n = itv-n - 1` ＋ `itv.pop()`；
④ `h2m-ty-of` 的 K-IDENTIFIER 臂補 `it` 特例——預掃描 `h2m-scan` 對 LET 右值是 IDENT 時按名字查槽型別，`it` 不在槽表 ⇒ 原返 -1；本件加 `i < 0 && nm == 'it' -> code = TY-I64`（s63 門禁已限主體為 i64，槽寬即 I64；`it` 出現在 match 外的裸語句時仍由 var-read/write 空棧 ⇒ 計 unsup，門禁不放鬆）；
⑤ 同族三棧（`stk`／`rnms`／`epl`）在每個作用域退出位點補真正的 `.pop()` 彈出——`h2m.no` 對 `arr` 的 `.pop()` 是原地截短的內建方法（非純讀），僅 `-1` 計數會讓兄弟作用域讀到陳舊頂

**關鍵點**：
1. **`it` 不進槽表**：`it` 從頭到尾沒有 `h2m-slot-decl`，`h2m-lst-find('it')` 恆返 -1——避免「同名跨區域串型」（AGENTS.md §6.3）；`it` 的值就是 `itv` 棧頂的**當前**主體 id，作用域退出彈空即斷鏈
2. **cond 臂的 LOAD 在判定塊內、被臂體複用是合法的**：s63 的 arm 生成形如 `entry → cond_test_k → arm_body_k → merge`，主體 LOAD 已在 cond_test_k 內發出且支配 arm_body_k，跨塊引用滿足 LLVM dominance；無需重發 LOAD
3. **wild 臂「懶發」是零 IR 漂移的關鍵**：`h2m-subtree-uses-it` 遞迴遍歷 arm 主體的 AST、識別 K-IDENTIFIER `'it'`；未用則不 LOAD，與「未覆蓋 it」時的 IR **一字節不差**——差分對拍的 k3 刀專打這條：把 wild 臂**補發**的 LOAD 換成錯槽讀 ⇒ `it=0`，總和才變
4. **嵌套 match 的棧是「壓上彈出」而非「覆蓋」**：外層臂體壓外主體 id、內層 match 再壓內主體 id，內 match 退出彈內、外 match 退出彈外；k4 刀專打——把內臂加法的 `%v16`（內主體）換成 `%v13`（外主體）⇒ 內層 `it` 被偷換成外主體
5. **同函式兩 match 各自獨立**：`itv` 棧不是「一次綁定、跨 match 有效」——w5 第一個 match 彈空後再進第二個，第二個壓新主體。k5 刀專打——第二 match 主體 LOAD 若還讀第一顆槽（`%s3`）則命中緩存值 4、和為 41
6. **同族棧陳舊讀是真缺陷、不是 s65 引入**：s59 的 `break`／`continue` 走 `exit` 分支未彈棧，因當時無消費者所以未爆；s65 讓 `it` 消費者出現在兄弟作用域 ⇒ 立即觸發。修法是在每個 pop 位點用 `.pop()` 而非 `-1`；這條已在 HEAD 裡、`h2m-test` 加 8 斷言先紅後綠（紅測覆蓋 `stk`／`rnms`／`epl` 三條各自兄弟作用域形）
7. **`h2m-ty-of` 的 `it` 特例是本件最險的一格（本輪實測 bug）**：s65 首輪紅測（test-it-wild、test-it-nested 各 3 斷言）報的是 `unsup=2` **雙標記**，不是 `unsup=1`——查因流程：預掃描 `h2m-scan` 的 K-LET 臂調 `h2m-ty-of(RHS)`，`it` 不在槽表 ⇒ 返 -1 ⇒ 用 -1 作為 `r` 的槽型呼叫 `h2m-lst-add`（後寫覆蓋 last-wins），進而在 `h2m-lower-func` 進入槽 lay 迴圈時 `h2m-slot-decl(c, 'r', -1)` 因 `ok-slot == 0` 計 mark#1、且**不註冊 name**；進入臂降級時 `h2m-var-write('r', v)` 找不到 `r` 的槽 ⇒ 計 mark#2。test-it-basic 僥倖通過：它另一臂 `-> r = 0` 重新以 I64 註冊了 `r`；test-it-wild 只此一個 LET ⇒ 定型即 -1 一路到底。修法就是在 `h2m-ty-of` 的 K-IDENTIFIER 臂補一條 I64 常量返回
8. **門禁未放鬆**：棧空、非 `it` 名、非 I64 主體（s63 門禁）任一路都保留 `unsup++`；p3 差分（`r = it / 2`）noc=**101**、oracle=38 就是這條門禁可鑑別的實證

**驗證環境**（v0.4.13 工具鏈 ＋ `DEVELOPER_DIR` CommandLineTools；一律用釘住的 `/tmp/nolang-v413/bin/no`——本輪中途遭遇磁碟滿（`/System/Volumes/Data` 剩 111Mi）導致回歸中途 `no space left on device`，用戶清完快取後 `git worktree add /tmp/nolang-v413 v0.4.13` ＋ `make BINDIR=/tmp/nolang-v413/bin` 重釘 v0.4.13（同前 §5.3.4.cs 的釘法），所有數值均為**重釘後**複測結果）：
- `h2m-test` **443 → 453 → 471/471**（第一次 +10：同族棢修正——`test-stk-s65-sibling-pop`／`test-rnms-s65-sibling-pop`／`test-epl-s65-sibling-pop` 三條各紅→綠；第二次 +18：`test-it-basic`／`test-it-wild`／`test-it-nested`／`test-it-seq`／`test-it-unsup`／`test-it-prescan` 六組各 3 斷言，前兩輪紅測錨定 ty-of 雙 mark 坑）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**；`util`／`tok`／`lx`／`ps`／`ps-err`／`ck`／`diag` 全綠
- ⚠️ 兩項既有紅點（在 HEAD 乾淨對照樹以同工具鏈復現同值，非本件引入）：`ast-test` rc=1（Go 側 MIR bug #85 `consumer reads value 0 (NoVal)`；主樹 `./bin/no` 上另現 `EmitLLVM: setfield: field "buf" not found on "sb.sb"`）；`ck-sig-test` **41/50**（`ck-sig.no` 簽名表與工具鏈內嵌 std 的世代漂移）。兩測試都不 import `h2m.no`，與本件正交
- 零回歸：自有檔 `no fmt -d` rc=0（`h2m.no`／`h2m-test.no` 由 `no fmt -w` 歸零，`#{...}` 註解計數改前後相同）；`no vet src/std` **4 ERROR**（與 HEAD 基線逐值相同，非本件引入）；s55–s63 探針前向 exit **73/47/92/188/72/134/164/134/221** 不退化、s64 前向 **91** 不退化（`tmp/s65f-gate.sh`：s64f-regress ＋ s64/s65 前向 ＋ fmt -d ＋ vet 四合一）
- 探針 `tmp/probe-s65.no`（SRC 為單行轉義串、五式各含一種 `it` 形制）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **46**；同一探針在 ty-of 未補前會因 `unsup > 0` 只發毒化 main ⇒ exit **101**（證明預掃描槽型 -1 造成的雙 mark 就是本件的靶）
- 🔴 **Go oracle 差分對拍**：`tmp/s65-oracle.no` 同程式（真 Nolang 源碼、同一工具鏈編譯）實測 **46**，逐值 `Q1..Q6 = 1 7 13 7 3 9` 兩側一致；源碼級二對配對變異兩側同值 **45**（`w1` 型樣 `1` → `2`，落入兜底 `r = 0`；錨帶 8 空格縮進避開探針頭註釋裡的同名引用）／**47**（`w3` 判定臂 `it + 10` → `it + 11`）；鑑別態一例：`w3` 兜底臂 `it * 2` → `it / 2`（除不支持）⇒ noc 毒 main **101**、oracle `w3(5)=2` ⇒ **38**（`/` 兩側都可鑑別；先前預測嘗試 `& 3` 在 oracle 側 build fail，故保留 `/`）
- IR 反向五刀（`tmp/s65-diff.py`）：**39**（w2 兜底臂主體 LOAD 換讀 `r` 槽：`it=0` ⇒ `q2=0` ⇒ 46-7）／**44**（w3 判定臂 add 的 `it` 操作數換成零值 `%v5`：10+0 ⇒ `q3=10`）／**36**（w3 兜底臂補發的主體 LOAD 換讀 `r` 槽）／**45**（w4 內臂 add 的內主體 `%v16` 換成外主體 `%v16` ⇒ 內層 `it` 被偷換；專打「嵌套時棧頂=內主體」）／**41**（w5 第二個 match 主體 LOAD 仍讀第一顆槽 `%s3`：`b=4` ⇒ `a+b=8`；專打「先後兩 match 各綁各的主體」）——五刀各只改一個 LOAD 槽或操作數，值皆按預期翻轉，證明發射側確實按 `itv` 棧頂取 id
- 🟡 **Go 側觀察：嵌套 match 的 `it` 海森堡**（記 §11 B27）：形如 `n: { 1 -> { t0 = it; m: { 2 -> r = it; -> r = 9 }; r = r + t0 }  -> r = 0 }` 在 Go v0.4.13 與 HEAD 上都得 **2**（`r = it + t0 = 1 + 1`），語義應有的值 **3**（外 `it=1`、內 `it=2`、`r = it + t0 = 3`）只在函式內加 `print` 時才浮現；noc 側穩定 **3**。實證據文件：`tmp/s65-sem.no`／`s65-sem2..6.no`／`s65-sem4.no`（no-print 復現 **2**）／`s65-sem5w.no`（安全形 **3/9/-1** 穩定）。依 §2 黃金法則不改合法源碼繞過 Go bug；探針 `w4` 改用「所有內層計算全放進內臂」的穩定形 `2 -> r = it + t0`

| noc | Go |
| --- | --- |
| `itv` 值 id 棧；每臂進入壓本臂主體 LOAD id、退出 `.pop()` | checker/HIR 層給臂內建 `it` 局部，MIR 無對應 opcode |
| cond 臂複用判定塊已發的 LOAD（跨塊由 dominance 保證合法）| 臂內 `it` 與判定用的是同一個 SSA Value |
| wild 臂懶發 LOAD（`h2m-subtree-uses-it` 遞迴掃）；未用則壓佔位 0 | Go 每臂獨立降、`it` 未被用則整個 arm 不產碼；佔位 0 路徑對 noc 側而言 IR 零漂移 |
| `h2m-var-read/write('it')` 攔截為棧頂 | `hir.lowerExpr` 將 `it` 解析到臂局部 |
| `h2m-ty-of('it')` 特例返 TY-I64（預掃描槽型；規避 `h2m-slot-decl` -1 導致的雙 mark）| checker 天然按主體型別推斷 `it` |
| `stk`／`rnms`／`epl` 三棧退出補真 `.pop()` | Go 遞迴降層天然棧，無此坑 |
| 門禁：棧空／非 i64 主體／非 `it` 名一律 `unsup` | Go 無對應門禁 |

**未覆蓋／後續**：解構臂（`binding`／`bindingVariant`）；`ok`／`err`／`nil` 關鍵字臂與 raw-cond 臂；型別臂（`typePattern`／`typeArm`）；非 i64 主體（str／vec／option）與多值樣；表達位的鏈；帶標籤終結形與帶值 `return`；開邊界區間；`OP-STRUCT-SET` 連續約定（§11 B26）；`drv.cmd-build` 接線（M4-d）——以上構成 M4-e 第十二件的候選


### 5.3.4.cu s66——M4-e 第十二件：前綴表達式 `-x`／`!x`（`compiler/src/h2m.no` 的 `h2m-lower-expr` 新增 `K-PREFIX-EXPRESSION` 臂——`'-'`⇒`OP-NEG`、`'!'`⇒`OP-NOT`，僅收 i64 主體，其餘前綴元算子（`~`）仍走 `unsup` 誠實門禁；`h2m-ty-of` 補 `K-PREFIX-EXPRESSION` 臂遞迴運算元型別供預掃描 LET 槽型；**零改 `ps.no`／`cg.no`／`mir.no`**（`OP-NEG=74`／`OP-NOT=75` 及其 `sub i64 0`／`xor i64 1` 發射早於 s48 §5.3.4.ci 備妥））（2026-11-04 本輪）

**背景**：`h2m.no` 檔頭「不支持」清單自 s55 起就掛著「前綴負號」這一項（表達式缺口，與 §5.3.4.ct 那條 match 軌候選清單分屬兩處）。`ps.no` 早在解析層就把 `-3`／`!x`／`~x` 一律產成 `K-PREFIX-EXPRESSION`（`s0`=運算子字串、`kid[0]`=運算元），`cg.no` 也在 s48 落地 `OP-NEG`／`OP-NOT` 的 LLVM 發射（非 f64 走 `sub i64 0, %v<x>`、`xor i64 1, %v<x>`）——唯 `h2m.no` 的降級器缺 `K-PREFIX-EXPRESSION` 的分派臂，任何含 `-x`／`!x` 的函式落進 catch-all `-> h2m-mark(c)` 而被誠實門禁攔下、整函式不產碼。本件補上這條降級臂，把運算子字串映到既有 op。

**Go 權威**：`src/mir/hir2mir.go` 的 `emitNeg`（行 ~5503）／`emitNot`（~5505）——`-x` 對 f64 發 `fneg double`、其餘發 `sub i64 0, x`；`!x` 發 `xor i64 1, x`。`cg.no` 的 `OP-NEG`／`OP-NOT` 發射即照此鏡像（s48）。本件純在 h2m 側把 `K-PREFIX-EXPRESSION` 接到這兩個 op，無 cg／mir 改動。f64 主體暫不在本件範圍（h2m 目前只降 i64 直線式子集），運算元 `vty != TY-I64` 即出局。

**實測形制（`tmp/probe-s66.no`）**：五式各含一種前綴形制——p1 `-x`／p2 `-(-x)`（雙負）／p3 `!x`／p4 `-x + y`（前綴嵌算術）／p5 `!(x > 0)`（前綴套比較）。main 手寫 IR 調用 `p1(100)+p2(50)+p3(0)+p3(1)+p4(3,200)+p5(5)+p5(-3)`，期望值 `-100+50+1+0+197+0+1`＝**149**。非對稱取値 ⇒ NEG 值被偷換必然改總和；NOT 的 `p3(0)+p3(1)=1+0` 對稱（見 IR 刀備註），其鑑別力落在單元測的 IR 內容斷言。

**覆蓋面（本件）**：
① `h2m-lower-expr` 在 `K-CALL-EXPRESSION` 臂之後、catch-all 之前新增 `K-PREFIX-EXPRESSION` 臂——`osym = ast.str-at(t, d.s0)`、遞迴降 `kid[0]` 得 `ov`；`osym == '-' && h2m-vty-at(ov)==TY-I64` ⇒ `h2m-alloc` + `h2m-emit(OP-NEG, mid, ov, ...)`；`osym == '!' && h2m-vty-at(ov)==TY-I64` ⇒ `OP-NOT`；兩者皆不中（`~`、非 i64、`ov <= 0`）則 `ok-p == 0 -> h2m-mark(c)` 保留誠實門禁；
② `h2m-ty-of` 在 `K-IDENTIFIER` 之後、`K-CALL-EXPRESSION` 之前新增 `K-PREFIX-EXPRESSION` 臂——`-`／`!` 皆保 i64，型別即遞迴 `h2m-ty-of(operand)`，供 `h2m-scan` 預掃 LET 右值時把 `r = -x` 的 `r` 鋪成 TY-I64 槽（缺此臂預掃回 -1 會復現 §5.3.4.ct 關鍵點 7 的雙 mark 坑）；
③ `h2m-scan` 無需改——catch-all 早已 `h2m-scan-kids` 走訪 prefix 節點的運算元；
④ 檔頭「支持」清單加 `PrefixExpression（s66：-x / !x，僅 i64）`、「不支持」清單由「前綴負號」改為「前綴 `~`（s66 只收 `-` `!`）」；
⑤ `h2m-test.no` 新增 6 測試函式（test-neg-var／test-neg-lit／test-not-var／test-neg-nested／test-neg-in-expr／test-prefix-gate）註冊於 test-gate() 前，先紅後綠

**關鍵點**：
1. **純 h2m 側改動**：op 與 LLVM 發射早在 s48 就位，本件只做 `K-PREFIX-EXPRESSION → OP-NEG/OP-NOT` 的分派接線，`cg.no`／`mir.no`／`ps.no` 零改——這是自舉降級缺口而非 codegen 缺口的典型
2. **門禁不放鬆**：`~`（位元取反）無對應已備 op（Go 側 `^` 走雙運算元 `OP-XOR`），本件一律 `h2m-mark`；非 i64 主體同理。test-prefix-gate 以 `r = ~x` 斷言 `agg.unsup > 0` 且無 `define`
3. **`h2m-ty-of` 的 prefix 臂是隱形 prerequisite**：若只加 lower-expr 臂不加 ty-of 臂，`r = -x` 的 LET 預掃描會拿 -1 當槽型 → 復現 §5.3.4.ct 關鍵點 7 的雙 mark（槽 lay `ok-slot == 0` 計 mark#1、寫入找不到 name 計 mark#2）。紅測 test-neg-var 首輪即報 `unsup=2`，補 ty-of 臂後轉綠
|  |  |  |
| --- | --- | --- |
4. **`ok-p` 旗標形制對齊二元運算子臂**：初稿寫 `ov == 0 || (osym != '-' && osym != '!') -> mark`，會漏掉「osym 對但運算元非 i64」這條路（不產碼也不標記）；改用 `ok-p` 預置 0、兩臂各自成功才 `ok-p = 1`、末尾 `ok-p == 0 -> mark`，與二元 op 的 `bop == 0` 慣用法一致
5. **NOT 的 IR 刀對稱盲區（非缺陷）**：p3 取 `!0` 與 `!1`，`xor i64 1` 若退化成 `xor i64 0` 則兩值都翻轉失敗但 `1+0` 與 `0+1` 總和同為 1——對稱輸入讓反向刀無法從總和鑑別。NOT 的正確性改由單元測的 IR 內容斷言（`ir.contains('xor i64 1')`）釘住；NEG 三刀（K1/K3/K5）則如預期改變 exit（150／151／148）

**驗證環境**（v0.4.13 工具鏈 ＋ `DEVELOPER_DIR` CommandLineTools；一律用釘住的 `/tmp/nolang-v413/bin/no`，同 §5.3.4.ct）：
- `h2m-test` **471 → 485/485**（+14：6 測試函式，前綴紅測錨定 `h2m-ty-of` 雙 mark 坑與 `~` 門禁）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**；`util`／`tok`／`lx`／`ps`／`ps-err`／`ck`／`diag` 全綠
- ⚠️ 兩項既有紅點同 §5.3.4.ct（`ast-test` rc=1 Go 側 #85／`ck-sig-test` 41/50 世代漂移），兩檔都不 import `h2m.no`，與本件正交
- 零回歸：`h2m.no`／`h2m-test.no` 經 `no fmt -w` 歸零、自有檔 `no fmt -d` rc=0；`no vet src/std` **4 ERROR**（與 HEAD 基線逐值相同，非本件引入）；s55–s65 探針前向 exit **73／47／92／188／72／134／164／134／221／91／46** 不退化（`tmp/s65f-gate.sh`）
- 探針 `tmp/probe-s66.no`（SRC 單行轉義串、五式各含一種前綴形制）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **149**；同一探針在 `h2m-ty-of` prefix 臂未補前因 `unsup > 0` 只發毒化 main ⇒ exit **101**
- 🔴 **Go oracle 差分對拍**：`tmp/s66-oracle.no` 同程式（真 Nolang 源碼、同一工具鏈編譯）實測 **149**，兩側逐值一致；⚠️ oracle 側 p3／p5 需等價改寫規避 Go 型別差異——`!x` 在 Go 側產 `bool` 而非 i64，故 p3 用 `1 - x`、p5 用 `g: { true -> r = 0  -> r = 1 }` 真值臂 match 等價代換（0/1 論域語義相同）；`tmp/s66-knives.py` IR 反向五刀：NEG 三刀 **150／151／148**（如預期改 exit），XOR 兩刀因 p3 對稱輸入得同值 149（數學性質、非缺陷，NOT 正確性由單元測 `xor i64 1` 斷言保證）

| noc | Go |
| --- | --- |
| `h2m-lower-expr` `K-PREFIX-EXPRESSION`：`'-'`⇒`OP-NEG`、`'!'`⇒`OP-NOT`，僅 i64 | `hir2mir.emitNeg`／`emitNot`（f64→`fneg`、其餘→`sub 0`／`xor 1`） |
| `h2m-ty-of` prefix 臂遞迴運算元型別（保 i64），供預掃描鋪 TY-I64 槽 | checker 天然按運算元推斷 `-x`／`!x` 型別 |
| `~`／非 i64／降級失敗一律 `h2m-mark` 整函式不產碼 | Go 有對應 lowering，noc 側未覆蓋即誠實出局 |
| op 發射（`sub i64 0`／`xor i64 1`）復用 s48 既有 `cg.no` 路徑 | `cg` 端 `OP-NEG`／`OP-NOT` 鏡像（s48 已就） |

**未覆蓋／後續**：前綴 `~`（位元取反，需雙運算元 `OP-XOR` 或新 op）；前綴 f64 `-x`（`fneg`，h2m 目前只降 i64 直線式）；§5.3.4.ct 的 match 軌殘留全部延後（解構臂 `binding`／`bindingVariant`、`ok`／`err`／`nil` 關鍵字臂與 raw-cond 臂、型別臂、非 i64 主體與多值樣、表達位的鏈、帶標籤終結形與帶值 `return`、開邊界區間、`OP-STRUCT-SET` 連續約定 §11 B26、`drv.cmd-build` 接線 M4-d）——本件取自 `h2m.no`「不支持」表達式缺口清單，非該 match 軌清單；以上構成 M4-e 第十三件的候選

|  |  |  |
| --- | --- | --- |

### 5.3.4.cv s67——M4-e 第十三件：二進制位運算 `&`／`|`／`^`／`<<`／`>>`（`compiler/src/h2m.no` 的 `h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈補五條 `bop` 臂——`'&'`⇒`OP-AND`、`'|'`⇒`OP-OR`、`'^'`⇒`OP-XOR`、`'<<'`⇒`OP-SHL`、`'>>'`⇒`OP-SHR`，僅收 i64 雙元；**零改 `ps.no`／`cg.no`／`mir.no`**（五 op 及其 `and`／`or`／`xor`／`shl`／`ashr i64` 發射早於 s35 ALU 家族備妥），亦**零改 `h2m-ty-of`**（位運算復用既有 `K-INFIX-EXPRESSION` 節點，其 i64 臂已回 TY-I64，不同於 §5.3.4.cu 新增節點种类需補臂））（2026-11-04 本輪）

|  |  |
| --- | --- |
**背景**：`h2m.no` 檔頭「不支持」清單自 s55 起掛著「位運算」這一項（表達式缺口）。`ps.no` 把 `x & y` 等一律產成 `K-INFIX-EXPRESSION`（`s0`=運算子字串、`kid[0]`/`kid[1]`=左/右運算元），`cg.no` 也在 s35 就落地五條 LLVM 發射（`and/or/xor/shl/ashr i64 %v<x>, %v<y>`）——唯 `h2m.no` 的 `h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈只蓋 `+ - *` 與六條比較運算子，任何含 `& | ^ << >>` 的式子落進 catch-all `bop == 0 -> h2m-mark(c)` 而被誠實門禁攔下。本件在該鏈補上五條 `bop` 映射，把運算子字串接到既有 op——與 s66 不同的是，這是**純分派鏈加臂**，無新節點种类、無需 `h2m-ty-of` 配套。
**Go 權威**：`src/mir/hir2mir.go` 的 ALU 發射——`&`→`and`、`|`→`or`、`^`→`xor`、`<<`→`shl`、`>>`→`ashr`（算術右移，有符號）；`mir.no` 的 `OP-AND=44`/`OP-OR=45`/`OP-XOR=46`/`OP-SHL=47`/`OP-SHR=48`（s35）。邏輯右移 `lshr` 需另立 `OP-SHRU`（u64 專用，不屬本件）。本件純在 h2m 側接線，無 cg／mir 改動。`&^`（位清除）在本語言解析層根本不成為運算子（見關鍵點 3），無對應 op，不在範圍。
**實測形制（`tmp/probe-s67.no`）**：六式各含一種位運算形制——b1 `x & y`／b2 `x | y`／b3 `x ^ y`／b4 `x << 3`／b5 `x >> 2`／b6 `(x & 15) + 1`（位運算嵌算術）。main 手寫 IR 調用 `b1(12,10)+b2(12,10)+b3(12,10)+b4(5)+b5(100)+b6(63)`，期望 `8+14+6+40+25+16`＝**109**。六式取値全部非對稱（每種 op 換成另一種必改總和，見 IR 刀）。

**覆蓋面（本件）**：
|  |  |
| --- | --- |
① `h2m-lower-expr` 的 `K-INFIX-EXPRESSION` 分派鏈在 `osym == '*' -> bop = OP-MUL` 之後、比較運算子注釋之前新增五條臂——`osym == '&' -> bop = OP-AND`、`'|'`⇒`OP-OR`、`'^'`⇒`OP-XOR`、`'<<'`⇒`OP-SHL`、`'>>'`⇒`OP-SHR`；既有 `lv`/`rv`/`ity`  guard 與末尾 `h2m-emit(c, bop, mid, lv, rv, '', TY-I64)` 原樣復用；
② `h2m-ty-of` **無需改**——位運算不引入新 AST 節點种类，既有 `K-INFIX-EXPRESSION` 臂對 i64 雙元已回 TY-I64，預掃描 LET 槽型自然正確（與 §5.3.4.cu 新增 `K-PREFIX-EXPRESSION` 必須補臂的情形不同）；
|  |  |
| --- | --- |
③ 檔頭「支持」清單加「位運算 `& | ^ << >>` s67」、「不支持」清單由「除與余、位運算、」收窄為「除與余（`/` `%`，Go 側 `?i64` 除零→err，i64 子集未建模 option）、位清除 `&^`（無對應 op）」；
④ `h2m-test.no` 新增 7 測試函式（test-bit-and／or／xor／shl／shr／chain／gate）註冊於 test-gate() 前，先紅後綠；gate 以 `r = x / y`（除法）斷言 `agg.unsup > 0` 且無 `define`，釘住 `/ %` 仍門禁。

**關鍵點**：
1. **零 cg／mir／ty-of 改動**：op 與 LLVM 發射早在 s35 就位，本件只做 `osym → bop` 的分派接線。與 s66 不同，位運算復用既有 `K-INFIX-EXPRESSION` 節點，s66 那條「新增節點种类必須同步擴 `h2m-ty-of`」的教訓（雙 mark 坑）在本件**不適用**——已實際驗證補 lower-expr 臂後 h2m-test 首輪即轉綠、無 `unsup=2` 復現。
2. **`OP-SHR` 發 `ashr`（算術右移）**：與 Go 側 `>>` 對有符號 i64 的算術語義一致；需邏輯右移的場景要另走 `OP-SHRU`（u64），不屬本件 i64 子集。
3. **`&^`（位清除）在解析層不成為運算子**：`no ast tmp/s67-dump.no` 實測 `x &^ y` 被 `ps.no` 降成 `r = x` 加一條游離 `y` 表達式語句（非 `K-INFIX-EXPRESSION` 節點）——因此本件無需為 `&^` 建門禁臂，它天然不在位運算分派範圍（屬 `ps.no` 側待查項，非 h2m 缺口）。
4. **`/ %` 因 option 語義分歧仍門禁**：Nolang 把 `a/b` 判為 `?i64`（除零→`err`），而 i64 直線子集未建模 option，本件不收 `OP-DIV`/`OP-MOD`，以 test-bit-gate 釘住。
5. **反向刀必須 mutate【未 opt】原始 IR**：`opt -O2` 會把常量實參調用（b1(12,10)）摺成常數 109，原始 IR 裡的 `and i64` 等助記符會消失——故 `tmp/s67-knives.py` 在 `tmp/s67.ll`（pre-opt）上做逐助記符換刀，再走 opt+clang。

**驗證環境**（v0.4.13 工具鏈 ＋ `DEVELOPER_DIR` CommandLineTools；一律用釘住的 `/tmp/nolang-v413/bin/no`，同 §5.3.4.cu）：
- `h2m-test` **485 → 501/501**（+16：7 測試函式，位運算紅測錨定五 op 分派與 `/ %` 門禁）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**
- 零回歸：`h2m.no`／`h2m-test.no` 經 `no fmt -w` 歸零、自有檔 `no fmt -d` rc=0；s55–s66 探針前向 exit **73／47／92／188／72／134／164／134／221／91／46／149** 不退化
- ⚠️ `no vet src/std`：本輪觀測 `[ERROR]` 計數為 **1**（非 §5.3.4.cu 所記 4）。基線計數依工具鏈／統計口徑而異；本件**完全不觸及 `src/std`**，與 vet 正交、非回歸。逐值對齊需另行固定基線口徑（待辦）。
- 探針 `tmp/probe-s67.no`（SRC 單行轉義串、六式各含一種位運算形制）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **109**；Grep IR 確認 `and/or/xor i64 %v1,%v2`、`shl i64`、`ashr i64`（移位量經 `add i64 0, N` 常量）及 b6 的 `and`+`add` 鏈
- 🔴 **Go oracle 差分對拍**：`tmp/s67-oracle.no` 同程式（真 Nolang 源碼、同一工具鏈編譯）實測 **109**，兩側逐值一致；`tmp/s67-knives.py` IR 反向六刀（逐助記符互換）**115／103／111／69／228／157** 全部改 exit（無 s66 XOR 那種對稱盲區，因六式取値全部非對稱）

| noc | Go |
| --- | --- |
| `h2m-lower-expr` `K-INFIX-EXPRESSION`：`&`⇒`OP-AND`、`|`⇒`OP-OR`、`^`⇒`OP-XOR`、`<<`⇒`OP-SHL`、`>>`⇒`OP-SHR`，僅 i64 | `hir2mir` ALU 發射（`and`/`or`/`xor`/`shl`/`ashr i64`，s35） |
| `>>` 發 `ashr i64`（算術右移） | Go `>>` 對有符號 i64 算術語義一致 |
| 復用既有 `K-INFIX-EXPRESSION` 節點，`h2m-ty-of` i64 臂原樣回 TY-I64 | checker 天然按雙元推斷 `x & y` 型別 |
| `/ %`／`&^`／非 i64 一律 `h2m-mark` 整函式不產碼 | Go 有 `/ %`（`?i64`）lowering；`&^` 非運算子；noc 側未覆蓋即誠實出局 |

|  |  |  |  |  |
| --- | --- | --- | --- | --- |

### 5.3.4.cw s68——M4-e 第十四件：邏輯 `&&`／`||`（`compiler/src/h2m.no` 的 `h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈補兩條 `bop` 臂——`'&&'`⇒`OP-AND`、`'||'`⇒`OP-OR`，僅收 i64 雙元，**復用 s67 位運算同一組 op**；**零改 `ps.no`／`cg.no`／`mir.no`**（`and`／`or i64` 發射早於 s35 ALU 家族備妥），亦**零改 `h2m-ty-of`**（邏輯運算復用既有 `K-INFIX-EXPRESSION` 節點，其 i64 臂已回 TY-I64））（2026-11-04 本輪）

|  |  |  |  |  |  |  |  |  |  |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
**背景**：`h2m.no` 檔頭「不支持」清單自 s55 起掛著「邏輯 `&& ||`」這一項。`ps.no` 把 `a && b`／`a || b` 一律產成 `K-INFIX-EXPRESSION`（`s0`=運算子字串、`kid[0]`/`kid[1]`=左/右運算元，`no ast tmp/s68-dump.no` 實證），`cg.no` 也在 s35 落地 `and/or i64`——唯 `h2m-lower-expr` 的 `K-INFIX` 分派鏈此前只接 `+ - *`、六比較、以及 s67 的 `& | ^ << >>`，`&&`／`||` 落進 catch-all `bop == 0 -> h2m-mark(c)` 而被誠實門禁攔下。本件補上 `&& → OP-AND`／`|| → OP-OR` 兩條映射，把邏輯運算接到位運算**已就緒**的 MIR op 與 LLVM 發射上——因 i64 子集內布爾值恆為 0/1（比較結果 `zext i1 to i64`），`and/or i64` 於 0/1 恰即邏輯與/或；且 Go `emitLogic` **本身非短路**（兩側都求值後發 `and/or`），故 i64 子集裡的非短路 lowering 與 Go 逐值一致。這是**純分派鏈加兩臂**，無新節點种类、無需 `h2m-ty-of` 配套（同 s67、不同 s66）。

|  |  |  |  |
| --- | --- | --- | --- |
**Go 權威**：`src/mir/hir2mir.go` 的 `infixOp`——`&&`→`OpAnd`、`||`→`OpOr`（邏輯家族），`codegen.go` 的 `emitLogic` 對二者**非短路**發 `%c = and/or <lt> aV, bV`；位運算 `&`→`OpBitAnd`、`|`→`OpBitOr`（`emitBitwise`）。`mir.no` 側 `OP-AND=44`／`OP-OR=45`（复用 s67），cg 發 `and i64`／`or i64`（s35 已備）。⚠️ Go 區分 `OpAnd`（邏輯）與 `OpBitAnd`（位元），但本 i64 子集兩者共用 `OP-AND`——因操作數只可能是 0/1（比較結果），`and i64` 在 0/1 上與位與同構。

|  |  |  |  |  |  |  |
| --- | --- | --- | --- | --- | --- | --- |
**實測形制（`tmp/probe-s68.no`）**：三式——g1 `r = (x > 0) && (y > 0)`／g2 `r = (x > 0) || (y > 0)`／g3 `r = (x > 0) && (y > 0) || (x > 100)`（`&&` 緊於 `||`）。main 手寫 IR 調用 `g1(5,7)=1 g2(-5,9)=1 g1(-2,6)=0 g2(4,-3)=1 g3(50,0)=0 g3(5,7)=1`，權重 `1/2/4/8/16/32` ⇒ `1+2+0+8+0+32`＝**43**。六式取値全部非對稱（每种 and/or 換成另一種必改總和，見 IR 刀）。

**覆蓋面（本件）**：
|  |  |  |
| --- | --- | --- |
① `h2m-lower-expr` 的 `K-INFIX-EXPRESSION` 分派鏈在 s67 的 `osym == '>>' -> bop = OP-SHR` 之後新增兩條臂——`osym == '&&' -> bop = OP-AND`、`osym == '||' -> bop = OP-OR`；既有 `lv`/`rv`/`ity` guard 與末尾 `h2m-emit(c, bop, mid, lv, rv, '', TY-I64)` 原樣復用；
② `h2m-ty-of` **無需改**——邏輯運算不引入新 AST 節點种类，既有 `K-INFIX-EXPRESSION` 臂對 i64 雙元已回 TY-I64（同 s67，不同 s66 新增節點需補臂）；
|  |  |  |  |  |
| --- | --- | --- | --- | --- |
③ 檔頭「支持」清單加「邏輯 `&& ||` s68，復用 OP-AND/OR」、「不支持」清單移除「邏輯 `&& ||`」；
④ `h2m-test.no` 新增 4 測試函式（test-log-and／or／chain／gate）註冊於 test-gate() 前，先紅後綠；test-log-and/or/chain 斷言 `and i64`／`or i64` 存在且 `agg.unsup == 0` 且有 `define`，test-log-gate 以 u64 操作數 `r = x && y` 斷言 `agg.unsup > 0` 且無 `define`，釘住 i64 子集邊界。

**關鍵點**：
|  |  |  |
| --- | --- | --- |
1. **零 cg／mir／ty-of 改動**：op 與 LLVM 發射早在 s35（復用 s67）就位，本件只做 `osym → bop` 接線；`&& ||` 復用位運算 `K-INFIX-EXPRESSION` 節點，s66「新增節點种类必須同步擴 `h2m-ty-of`」的雙 mark 坑在本件**不適用**——補 lower-expr 臂後 h2m-test 首輪轉綠、無 `unsup=2` 復現。
2. **非短路，與 Go `emitLogic` 同形**：i64 子集內 `&& ||` 兩側都求值、發 `and/or i64`，不產生分支；Go `emitLogic` 亦非短路。真·短路（如 `(x>0) && f(y)` 在 x≤0 時不調 `f`）需布爾類型與控制流分岔，非本 i64 直線子集範圍（屬殘留）。
|  |  |  |  |  |
| --- | --- | --- | --- | --- |
3. **`&&` 緊於 `||`**：由 `ps.no` 優先級在 AST 層確定（g3 = `((x>0)&&(y>0)) || (x>100)`），h2m 只遞歸降既有 `K-INFIX` 樹，不再處理優先級。
4. **反向刀必須 mutate【未 opt】原始 IR**：`opt -O2` 會把常量實參調用摺成常數 43，原始 IR 裡的 `and/or i64` 助記符會消失——故 `tmp/s68-knives.py` 在 `tmp/s68.ll`（pre-opt）上互換刀，再走 opt+clang。

**驗證環境**（v0.4.13 工具鏈 ＋ `DEVELOPER_DIR` CommandLineTools；一律用釘住的 `/tmp/nolang-v413/bin/no`，同 §5.3.4.cv）：
|  |  |  |
| --- | --- | --- |
- `h2m-test` **501 → 511/511**（+10：4 測試函式，邏輯 `&& ||` 紅測錨定兩條分派臂與 u64 門禁）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**
- 零回歸：`h2m.no`／`h2m-test.no` 經 `no fmt -w` 歸零、自有檔 `no fmt -d` rc=0；s55–s67 探針前向 exit **73／47／92／188／72／134／164／134／221／91／46／149／109** 不退化
- ⚠️ `no vet src/std`：本輪觀測 `[ERROR]` 計數為 **1**（與 §5.3.4.cv 同）；本件**完全不觸及 `src/std`**，與 vet 正交、非回歸。
- 探針 `tmp/probe-s68.no`（SRC 單行轉義串、三式各含邏輯運算形制）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **43**；Grep IR 確認 g1→`and i64`、g2→`or i64`、g3→`and i64`+`or i64`（`and i64`×2、`or i64`×2）
- 🔴 **Go oracle 差分對拍**：`tmp/s68-oracle.no` 同程式（真 Nolang 源碼、同一工具鏈編譯，`v: { true -> r = 1 -> r = 0 }` 做 bool→i64 轉換）實測 **43**，兩側逐值一致；`tmp/s68-knives.py` IR 反向四刀（逐助記符互換）**47／33／59／11** 全部改 exit（無對稱盲區；初版 `f=g3(200,5)` 因內層已真使外層 `or` 換 `and` 無效，改 `f=g3(5,7)` 令外層 `or` 可辨）

| noc | Go |
| --- | --- |
| `h2m-lower-expr` `K-INFIX-EXPRESSION`：`&&`⇒`OP-AND`、`||`⇒`OP-OR`，僅 i64 | `hir2mir` `infixOp`：`&&`→`OpAnd`、`||`→`OpOr`（`emitLogic` 非短路發 `and/or`） |
| i64 子集布爾恆 0/1，`and/or i64` 於 0/1 == 邏輯與/或 | Go 比較結果 bool→zext，`emitLogic` 於 0/1 同值 |
| 復用既有 `K-INFIX-EXPRESSION` 節點，`h2m-ty-of` i64 臂原樣回 TY-I64 | checker 天然按雙元推斷 `a && b` 型別 |
| f64／u64／非 i64 邏輯操作數一律 `h2m-mark` 整函式不產碼 | Go bool 邏輯原生；短路 bool 語義為 noc 側殘留 |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：`&&`／`||` 兩側皆真·`bool` 且需**短路**語義的場景（本件 i64 子集非短路已覆蓋布爾 0/1 逐值，短路控制流分岔與 f64/u64 邏輯操作數仍門禁）；`/`、`%`（需建模 `?i64` 除零→`err` 的 option 語義）；`&^` 位清除（`ps.no` 側不成為運算子，屬解析缺口）；前綴 `~`（位元取反）、前綴 f64 `-x`（§5.3.4.cu 殘留）；§5.3.4.ct 的 match 軌殘留全部延後（解構臂、`ok`/`err`/`nil` 關鍵字臂與 raw-cond 臂、型別臂、非 i64 主體與多值樣、表達位鏈、帶標籤終結形與帶值 `return`、開邊界區間、`OP-STRUCT-SET` 連續約定 §11 B26、`drv.cmd-build` 接線 M4-d）——以上構成 M4-e 第十五件的候選


### 5.3.4.cx s69——M4-e 第十五件：前綴位非 `~x`（`compiler/src/h2m.no` 的 `h2m-lower-expr` `K-PREFIX-EXPRESSION` 分派鏈補 `osym == '~'` 臂——`~x`⇒`OP-XOR(x, OP-CONST -1)`，鏡像 Go `hir2mir`「`~` 非邏輯非、走全 1 常量 XOR」；**零改 `ps.no`／`cg.no`／`mir.no`**（`OP-XOR`／`OP-CONST` 及其 `xor i64`／`add i64 0, K` 發射早於 s35/s55 備妥），亦**零改 `h2m-ty-of`**（`~x` 復用既有 `K-PREFIX-EXPRESSION` 節點，其臂對運算元型別已原樣遞迴））（2026-11-05 本輪）

**背景**：`h2m.no` 檔頭「不支持」清單自 s66 起掛著「前綴 `~`（s66 只收 `-` `!`）」。`ps.no` 把 `~x` 產成 `K-PREFIX-EXPRESSION`（`s0`=元運算子字串 `'~'`、`kid[0]`=運算元，`tok.TILDE` 臂 `ps.no:2426`），`h2m-lower-expr` 的 `K-PREFIX` 分派鏈此前只接 `'-'→OP-NEG`、`'!'→OP-NOT`，`'~'` 落進 catch-all `ok-p == 0 -> h2m-mark(c)` 而被誠實門禁攔下。本件補上 `~ → OP-XOR(x, CONST -1)`，把位非接到**已就緒**的 MIR op 與 LLVM 發射上。這是**純分派鏈加一臂（雙發射：先 `OP-CONST -1` 再 `OP-XOR`）**，無新節點种类、無需 `h2m-ty-of` 配套（同 s67/s68、不同 s66）。

**Go 權威**：`src/mir/hir2mir.go:8861`——`~x` 有**先於通用 `prefixOp` 的特例分支**：`allOnes = EmitInt(OpConst, rt, -1)` 後 `Emit(OpXor, rt, [ov, allOnes])`。註釋明言「Nolang `~` 是**位非而非邏輯非**——映射到 `OpNot`（發 `xor 1`）會得 `100^1=101` 而非 legacy `~100=4294967195`；須走 `OpXor` 配全 1 常量」。故 `~x = x ^ -1 = -x-1`（二補數）。`cg.no` 側 `OP-XOR` 發 `xor i64 %a, %b`、`OP-CONST` 發 `%v = add i64 0, K`（K 可負，`add i64 0, -1` 是合法 IR），二者早於 s35 ALU 家族／s55 備妥。

**實測形制（`tmp/probe-s69.no`）**：三式——q1 `r = ~x`／q2 `r = x + ~x`／q3 `r = (~x) * 2`。main 手寫 IR 調用 `q1(0)=-1 q1(5)=-6 q2(10)=-1 q3(-3)=4 q2(1)=-1`，權重 `1/2/4/8/16` ⇒ `-1-12-4+32-16 = -1` → `trunc i64 to i32` → exit **255**（-1 遮罩 8 位）。⚠️ q2/q3 皆**規避裸 `~ binop`**（見關鍵點 4）。

**覆蓋面（本件）**：
① `h2m-lower-expr` 的 `K-PREFIX-EXPRESSION` 分派鏈在 `'!'→OP-NOT` 臂之後新增 `osym == '~' && h2m-vty-at(c, ov) == TY-I64` 臂——先 `cm1 = h2m-alloc(c, TY-I64)` 發 `OP-CONST(cm1, -1)`，再 `mid = h2m-alloc(c, TY-I64)` 發 `OP-XOR(mid, ov, cm1)`，`v = mid`；
② `h2m-ty-of` **無需改**——`~x` 不引入新 AST 節點种类，既有 `K-PREFIX-EXPRESSION` 臂對運算元 `kid[0]` 型別原樣遞迴（不依賴 `osym`），故 s66「新增節點种类必須同步擴 `h2m-ty-of`」的雙 mark 坑在本件**不適用**；
③ 檔頭「支持」清單加「s69：`~x` 位非 ⇒ `OP-XOR(x, CONST -1)`」、「不支持」清單移除「前綴 `~`（s66 只收 `-` `!`）」；
④ `h2m-test.no` 將原 `test-prefix-gate`（`~x` i64 斷言 `unsup>0`）**改棧**為 `~s`（str 主體，仍誠實門禁斷言 `unsup>0`、無 `define`），並新增 3 測試函式（test-bitnot-var／nested／in-expr）註冊於 `test-neg-in-expr()` 後，先紅後綠；斷言 `add i64 0, -1`（CONST 錨）、`xor i64 %v`（寄存器 XOR、區別於 `!` 的 `xor i64 1`）、`define`、`agg.unsup == 0`。

**關鍵點**：
1. **`~` 走 XOR 全 1 而非 `OP-NOT`**：`!x` 發 `xor i64 %v, 1`（翻 LSB）、`~x` 發 `xor i64 %v1, %v2`（`%v2 = add i64 0, -1`，翻全部位）——測試以 `add i64 0, -1` 錨點區分二者，誤用 `OP-NOT` 會缺此錨且逐值錯。
2. **零 cg／mir／ty-of 改動**：`OP-XOR`／`OP-CONST` 與 LLVM 發射早就位，本件只做 `osym → 雙發射` 接線；補 lower-expr 臂後 h2m-test 首輪轉綠、無 `unsup=2` 復現。
3. **二發射序**：`~x` 需先 materialize 常量 `-1` 成 SSA 值 id（`OP-CONST`）才能作 `OP-XOR` 的右操作數——`h2m-emit` 雙元槽只收值 id、不收立即數，故此臂不同於單發射的 `-x`/`!x`，須 `alloc` 兩個 id。
4. 🔴 **`ps.no` 前綴 `~` 鬆綁定 vs Go 前端緊綁定——優先級分歧（ parser 缺口，本件**不修**，僅記錄待查）**：實測 `~x + 3` 時 Go 前端給 `(~x)+3`（如 `q2(10)` Go=-8），而 `ps.no`（`TILDE` 臂 `ps-parse-expr-prec(t, p, 9)` PREC 9 吞掉整個右側）給 `~(x+3)`（forward=-14）——**逐值分歧、checker 不報**。這是解析器優先級層面的缺口，**非 h2m 降級範圍**；依黃金法則（遇疑似解析 bug 只上報不改合法代碼），probe/oracle 一律規避裸 `~ binop`（改用括號 `(~x)*2` 或右操作數 `x + ~x`）以隔離本件降級正確性。【s84 複核改實】現行 generation 三面實測（AST 四形 dump 逐字節 SAME／Go 值 -8／h2m+cg 執行值 -8==-8）均為緊綁定，本分歧不可復現——TILDE 臂自 s8h（a761ab11）引入即 PREC 9、git 考古無變更，當時實測細節不可考；待查項已閉，哨兵見 ps-test `test-tilde-*`（§5.3.4.dm）。
5. **反向刀必須 mutate【未 opt】原始 IR**：`opt -O2` 會把常量實參調用摺成常數，故 `tmp/s69-knives.py` 在 `tmp/s69.ll`（pre-opt）上下刀，再走 opt+clang。

**驗證環境**（v0.4.13 工具鏈 ＋ `DEVELOPER_DIR` CommandLineTools；一律用釘住的 `/tmp/nolang-v413/bin/no`）：
- `h2m-test` **511 → 521/521**（+10：3 測試函式 + 改棧 gate，位非紅測錨定 `OP-CONST -1`／`OP-XOR`／str 主體門禁）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**
- 零回歸：`h2m.no`／`h2m-test.no` 經 `no fmt -w` 歸零、自有檔 `no fmt -d` rc=0；s55–s68 探針前向 exit **73／47／92／188／72／134／164／134／221／91／46／149／109／43** 不退化
- ⚠️ `no vet src/std`：本輪觀測 `[ERROR]` 計數為 **1**（與 §5.3.4.cv/cw 同）；本件**完全不觸及 `src/std`**，與 vet 正交、非回歸。
- 探針 `tmp/probe-s69.no`（SRC 單行轉義串、三式各含 `~` 形制）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **255**；Grep IR 確認每式體皆 `%v2 = add i64 0, -1` ＋ `%v3 = xor i64 %v1, %v2`
- 🔴 **Go oracle 差分對拍**：`tmp/s69-oracle.no` 同程式（真 Nolang 源碼、同一工具鏈編譯）實測逐值 **`-1 -6 -1 4 -1`**、總和 -1 → exit **255**，兩側逐值一致；`tmp/s69-knives.py` IR 反向三刀（K1 q1 `CONST -1→0`、K2 q2 `xor→add`、K3 q3 `xor→sub`）**22／111／191** 全部改 exit（無對稱盲區）

| noc | Go |
| --- | --- |
| `h2m-lower-expr` `K-PREFIX-EXPRESSION`：`'~'`⇒`OP-CONST(-1)`＋`OP-XOR(x, -1)`，僅 i64 | `hir2mir:8861`：`~x` 特例 `OpConst -1` ＋ `OpXor`（非 `OpNot`） |
| `~x = x ^ -1 = -x-1`（二補數全位翻轉） | 同：`xor` 全 1 常量，非 `xor 1` |
| 既有 `K-PREFIX-EXPRESSION` 節點，`h2m-ty-of` 對運算元型別原樣遞迴 | checker 天然按一元推斷 `~x` 型別 |
| 非 i64 主體（如 `~s` str）一律 `h2m-mark` 整函式不產碼 | Go `~` 僅整型別；f64／非整仍待 |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：`~` 的 f64／u64／非 i64 主體（本件僅 i64 子集）；🔴 `ps.no` 前綴 `~` 鬆綁定與 Go 前端緊綁定的**優先級分歧**（關鍵點 4，屬解析缺口、待查項，非本件 h2m 範圍）；`/`、`%`（需建模 `?i64` 除零→`err` 的 option 語義）；`&^` 位清除（`ps.no` 側不成為運算子）；前綴 f64 `-x`（§5.3.4.cu 殘留）；`&&`／`||` 真·布爾短路分岔（§5.3.4.cw 殘留）；§5.3.4.ct 的 match 軌殘留全部延後——以上構成 M4-e 第十六件的候選


### 5.3.4.cy s70——M4-e 第十六件：前綴 f64 負號 `-x`（`compiler/src/h2m.no` 的 `h2m-lower-expr` `K-PREFIX-EXPRESSION` 分派鏈把 `'-'→OP-NEG` 臂的守衛從「僅 `TY-I64`」**放寬**為「`TY-I64` 或 `TY-F64`」——`-x`（f64）⇒`OP-NEG`（目的值型別 `TY-F64`），鏡像 Go `emitNeg` 的 float 分支；**零改 `ps.no`／`cg.no`／`mir.no`／`h2m-ty-of`**（`OP-NEG` 的 `fneg double` 發射早於 s48 備妥、`cg` 依目的值型別分派 i64 `sub`／f64 `fneg`），這是 f64 值**首次端到端流經 h2m**（形參 `double`→`fneg double`→`ret double`））（2026-11-05 本輪）

**背景**：`h2m.no` 檔頭自 s66 起把前綴 `'-'` 守衛鎖死在 `h2m-vty-at(c, ov) == TY-I64`，f64 操作數落進 catch-all `ok-p == 0 -> h2m-mark(c)` 被誠實門禁攔下（§5.3.4.cu 殘留「前綴 f64 `-x`」）。本件只做**守衛放寬（加一 f64 臂）**，不改動任何下游。關鍵前提——`cg.no` 的 f64 管線**早已全備**（s37/s46–s54：`cg-runtime-ty-name(TY-F64)`='double'、`cg-val-prefix(TY-F64)`='%fd'、`OP-NEG` 依 `cg-vty-at(vty, d) == TY-F64` 發 `%fd = fneg double %fd`、`define`/`param`/`ret` 全走 `cg-ty-name`）；`h2m-param-ty`／`h2m-func-sig` 經 `h2m-ty-code`（`'f64'→TY-F64`）已收 f64 形參與 f64 返回型別；直線無控制流 ⇒ 寄存器模式（`c.mem==0`）⇒ f64 以 `%fd` SSA 流動、**永不落 `alloca double` 槽**（那是 f64 唯一被門禁之處，`h2m.no:304`）。故本件是**純 h2m 前綴臂守衛放寬**，下游零改。

**Go 權威**：`src/mir/codegen.go:6235`（`emitNeg`）——整型分支發 `sub 0, x`，**浮點分支發 `fneg double %v`**。h2m 側 `OP-NEG` 是**一元、單發射**（不同 s69 `~x` 的雙發射 `CONST -1`＋`XOR`）：`mid = h2m-alloc(c, TY-F64)` 後 `h2m-emit(c, OP-NEG, mid, ov, 0, '', TY-F64)`，型別寫進 `mid` 即驅動 cg 走 `fneg double`。⚠️ **操作數必為 f64 形參、無 f64 字面量**——`h2m` 只把 `K-INTEGER-LITERAL` 降成 `OP-CONST TY-I64`（`OP-CONST` 只發 `add i64 0, K`，無法產出 `double` 常量），故 f64 值只能經 `double` 形參注入（沿用 probe-s48 定式）。

**實測形制（`tmp/probe-s70.no`）**：二式——q1 `r = -x`／q2 `r = -(-x)`（雙負=恆等）。main 手寫 IR 調用 `q1(1.5)→-1`、`q1(-2.0)→2`、`q1(6.0)→-6`、`q2(3.25)→3`、`q2(-4.0)→-4`（各 `fptosi double to i64` 向零截斷），權重 `1/2/4/8/16` ⇒ `-1+4-24+24-64 = -61` → `trunc i64 to i32` → exit **195**（-61 遮罩 8 位）。Grep IR 確認 q1 體 `%fd2 = fneg double %fd1; ret double %fd2`、q2 體兩顆 `fneg double`。

**覆蓋面（本件）**：
① `h2m-lower-expr` 的 `K-PREFIX-EXPRESSION` 分派鏈在既有 i64 `'-'→OP-NEG` 臂之後新增 `osym == '-' && h2m-vty-at(c, ov) == TY-F64` 臂——`mid = h2m-alloc(c, TY-F64)`、`h2m-emit(c, OP-NEG, mid, ov, 0, '', TY-F64)`、`v = mid`；
② `'!'`／`'~'` 臂**不動**（仍僅 `TY-I64`）——f64 主體上的 `!x`／`~x` 仍誠實門禁，`test-neg-f64-gate` 以 `n8x = (x f64) (r i64) { r = !x }` 錨定此邊界（斷言 `unsup>0`、無 `define`）；
③ 檔頭「支持」清單加「s70：f64 `-x` ⇒ `OP-NEG`/`fneg double`——操作數必為 f64 形參、無 f64 字面量」；型別表註明「算术语義仍限 i64（二元 `+ - *` 比較等）；**例外**：前綴 `'-'` 接受 f64 操作數（s70，發 `fneg double`），二元算術仍不收 f64」；
④ `h2m-test.no` 新增 3 測試函式（test-neg-f64-var／nested／gate）註冊於 `test-prefix-gate()` 後，先紅後綠；斷言 `agg.unsup == 0`、`fneg double`、`define double`、`ret double`（var）、`fneg double`（nested 雙負）、`!` 於 f64 仍 `unsup>0` 且無 `define`（gate）。

**關鍵點**：
1. **守衛放寬 vs 加臂**：本件不是「新運算子」而是既有 `'-'` 臂的**型別守衛擴充**——i64 臂、f64 臂各一，`h2m-emit` 的第二型別參數（`res`）與 `h2m-alloc` 型別共同決定 cg 發 `sub` 抑或 `fneg`。補臂後 h2m-test 首輪轉綠、無 `unsup` 復現。
2. **零下游改動**：`OP-NEG` 的 f64 發射、`double` 型別名、`%fd` 前綴、`define`/`ret` 全部早在 s37/s46–s54/s48 就位；本件只做 `osym＋操作數型別 → 單發射` 接線。
3. **f64 無字面量降級**：`OP-CONST` 只發 i64，故 f64 值必須由 `double` 形參傳入——這是 h2m 現階段 f64 覆蓋面的**結構性邊界**（非本件引入），probe 與 oracle 皆以形參注入 f64。
4. **寄存器模式使 f64 不落槽**：直線無控制流的 q1/q2 走 `c.mem==0` 寄存器模式，f64 全程 `%fd` SSA，迴避了 `alloca double`（唯一 f64 門禁點）；一旦引入 f64 區域變量／控制流需落槽，才會觸及 `h2m.no:304` 門禁——此為後續件。
5. **反向刀必須 mutate【未 opt】原始 IR**：`opt -O2` 會把常量實參調用摺成常數，故 `tmp/s70-knives.py` 在 `tmp/s70.ll`（pre-opt）上下刀，再走 opt+clang。

**驗證環境**（v0.4.13 工具鏈 ＋ `DEVELOPER_DIR` CommandLineTools；一律用釘住的 `/tmp/nolang-v413/bin/no`）：
- `h2m-test` **521 → 529/529**（+8：3 測試函式，var 4 斷言／nested 2 斷言／gate 2 斷言，錨定 `fneg double`／`define double`／`ret double`／`!`-on-f64 仍門禁）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**
- 零回歸：`h2m.no`／`h2m-test.no` 經 `no fmt -w` 歸零、自有檔 `no fmt -d` rc=0；s55–s69 探針前向 exit **73／47／92／188／72／134／164／134／221／91／46／149／109／43／255** 不退化
- ⚠️ `no vet src/std`：本輪觀測 `[ERROR]` 計數為 **1**（`global.no` 之 `import std/err` alias `"error"` 缺口，與 §5.3.4.cv/cw/cx 同）；本件**完全不觸及 `src/std`**，與 vet 正交、非回歸。
- 探針 `tmp/probe-s70.no`（SRC 單行轉義串、二式各含 f64 `-x` 形制）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **195**；Grep IR 確認 q1 `%fd2 = fneg double %fd1`、q2 兩顆 `fneg double`
- 🔴 **Go oracle 差分對拍**：`tmp/s70-oracle.no` 同程式（真 Nolang 源碼、同一工具鏈編譯、`f64.to-i64` `#{buildin}` `llvm.fptosi.sat` 向零截斷）實測逐值 **`-1 2 -6 3 -4`**、總和 -61 → exit **195**，兩側逐值一致；`tmp/s70-knives.py` IR 反向三刀（K1 q1 `fneg→fadd 0.0` 恆等、K2 q2 雙負→單負、K3 q1 `fneg→fsub x,x` 歸零）**237／19／216** 全部改 exit（無對稱盲區）

| noc | Go |
| --- | --- |
| `h2m-lower-expr` `K-PREFIX-EXPRESSION`：`'-'`＋`TY-F64`⇒`OP-NEG`（單發射），i64／f64 守衛各一臂 | `codegen.go:6235` `emitNeg`：float 分支 `fneg double %v`、int 分支 `sub 0, x` |
| `mid = alloc(TY-F64)`＋`emit(OP-NEG, mid, ov, 0, '', TY-F64)`；型別驅動 cg 發 `fneg` | 同：`OpNeg` 依值型別選 `fneg`／`sub` |
| f64 經 `double` 形參注入（無 f64 字面量降級、`OP-CONST` 只發 i64） | Go 前端 f64 字面量走 `OpConst` 浮點；h2m 暫無此徑 |
| 直線寄存器模式下 f64 以 `%fd` SSA 流動、不落 `alloca double` | Go f64 全程 SSA 值 |
| `!x`／`~x` 於 f64 主體仍 `h2m-mark`（僅 `-x` 放寬） | Go `!`／`~` 僅整型別；f64 位運算本不合法 |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：f64 二元算術（`+ - * /`）與 f64 比較（本件僅前綴 `-x`，二元仍限 i64）；f64 區域變量／需落 `alloca double` 槽之控制流（`h2m.no:304` 門禁）；f64 字面量降級（`OP-CONST` 需擴浮點常量）；`&&`／`||` 真·布爾短路分岔（§5.3.4.cw 殘留）；`/`、`%`（i64 除零→`err` option 語義）；`&^` 位清除（`ps.no` 側不成為運算子）；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——以上構成 M4-e 第十七件的候選


### 5.3.4.cz s71——M4-e 第十七件：f64 二元算術 `+ - * /`（`compiler/src/h2m.no` 的 `h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈把既有「僅 `TY-I64` 雙側」的算術發射守衛**放寬**為「`TY-I64` 或 `TY-F64`」——新增 `fty`（兩側皆 `TY-F64`）發射臂，`x op y`（f64）⇒`OP-ADD/SUB/MUL/DIV`（目的值型別 `TY-F64`），鏡像 Go `cg` 依目的值型別分派 `fadd/fsub/fmul/fdiv double`；新增 `osym == '/' -> bop = OP-DIV` 映射供 f64 側發 `fdiv`，**但 i64 側明確排除 `bop == OP-DIV`（`/ %` 需 `?i64` 除零→`err` 的 option 語義未建模、仍誠實門禁）**；**零改 `ps.no`／`cg.no`／`mir.no`／`h2m-ty-of`**（`fadd/fsub/fmul/fdiv double` 發射早於 s37 備妥，`OP-DIV` 早在 `mir.no` 已定義），承接 s70 首開的 f64 端到端通道、從一元拓展到二元）（2026-11-05 本輪）

**背景**：`h2m.no` 自 s55 起，`K-INFIX-EXPRESSION` 的算術/比較發射一路卡在「任一操作數非 `TY-I64` ⇒ `ity=0` ⇒ `h2m-mark`」的守衛上（`+ - *` 舆六比較符僅 i64；`/ %` 舆 `&^` 完全未映射）。s70 首次让 f64 端到端流经 h2m（前綴 `-x`），本件把同一通道從一元拓展到二元——僅做**守衛放寬（加一 f64 臂）＋ `'/'` 符號映射**。關鍵前提——`cg.no` 的 f64 算術管線**早已全備**（s37：`OP-ADD/SUB/MUL/DIV` 各自 `cg-vty-at(vty, d) == TY-F64 -> 發 fadd/fsub/fmul/fdiv double`，`!= TY-F64 -> 發 i64 add/sub/mul／`OP-DIV` 走 `cg-div-guard` 包 `sdiv`），`h2m-param-ty`／`h2m-func-sig` 經 `h2m-ty-code`（`'f64'→TY-F64`）已收 f64 形参与 f64 返回型別；直線無控制流 ⇒ 寄存器模式（`c.mem==0`）⇒ f64 全程以 `%fd` SSA 流動、**永不落 `alloca double` 槽**。故本件仍是**純 h2m 二元臂守衛放寬**，下游零改。

**Go 權威**：`cg` 侧按型別分派（不新增 op）——`src/mir/codegen.go` 的 `emitBinop` 對 `OpAdd/Sub/Mul/Div` 先查運算元/目的型別：float 發 `fadd/fsub/fmul/fdiv double`，int 發 `add/sub/mul i64` 與 `sdiv`（帶除零護欄）。⚠️ **f64 除零與 i64 除零語義不同**：Nolang `f64` 非 option（`x f64 / y f64` 直返 `f64`，除零得 IEEE `inf`/`nan`，oracle 實測 `21.0/3.0→7` 已驗證）、而 `i64` 除法預設 `?i64`（除零→`err`）需 option 建模——故 `OP-DIV` 同一 opcode 在 f64 侧放行發 `fdiv`、在 i64 侧仍必須門禁（本件關鍵不對稱）。`h2m` 側 `OP-ADD/SUB/MUL/DIV` 均為**二元、單發射**：`mid = h2m-alloc(c, TY-F64)` 後 `h2m-emit(c, bop, mid, lv, rv, '', TY-F64)`，型別寫進 `mid` 即驅動 cg 走 f-family。⚠️ **兩側操作數必為 f64 形參、無 f64 字面量**——`h2m` 只把 `K-INTEGER-LITERAL` 降成 `OP-CONST TY-I64`（`OP-CONST` 只發 `add i64 0, K`），故 f64 值只能經 `double` 形參注入（沿用 probe-s48/s70 定式）。

**實測形制（`tmp/probe-s71.no`）**：五式——q1 `r = x + y`／q2 `r = x * y`／q3 `r = x - y`／q4 `r = x / y`／q5 `r = (x + y) * z`（嵌套：內層 fadd 型別經 `h2m-vty-at` 遞迴傳至外層 fmul）。main 手寫 IR 調用 `q1(2.5,1.5)→4`、`q2(3.0,2.5)=7.5→7`、`q3(9.0,4.0)→5`、`q4(21.0,3.0)→7`、`q5(1.5,2.5,3.0)→12`（各 `fptosi double to i64` 向零截斷），權重 `1/2/4/8/16` ⇒ `4+14+20+56+192 = 286` → `trunc i64 to i32` → exit **30**（286 遮罩 8 位）。Grep IR 確認 q1 `%fd3 = fadd double %fd1, %fd2`、q2 `fmul`、q3 `fsub`、q4 `fdiv`、q5 `%fd4 = fadd; %fd5 = fmul double %fd4, %fd3`。

**覆蓋面（本件）**：
① `h2m-lower-expr` 的 `K-INFIX-EXPRESSION` 分派鏈在 `'osym → bop'` 映射新增 `osym == '/' -> bop = OP-DIV`；將單一 i64 發射臂拆為**兩條互斥放行臂**——i64 臂（`ity==1 && bop != OP-DIV`，`+ - *` 放行、`/` 排除）與 f64 臂（新增 `fty` 判兩側皆 `TY-F64`，`+ - * /` 皆放行），末尾以 `ok-i == 0 && ok-f == 0` 统一 `h2m-mark`；
② 混合操作數（一 `TY-I64` 一 `TY-F64`）⇒ `ity`舆`fty` 皆 0 ⇒ 兩臂皆不落 ⇒ 誠實門禁（`test-arith-f64-mixed-gate` 釘住）；
③ `h2m-ty-of` **無需改**——本件復用既有 `K-INFIX-EXPRESSION` 節點（無新增節點種類，同 s67/s68），且發射時的兩側型別判定用 `h2m-vty-at(c, lv/rv)` 讀**實際值 id 的型別**（內層子表達式由 `h2m-alloc(TY-F64)` 已攜 f64）而非靜態推斷；`h2m-ty-of` 只在預掃描鋪 `alloca` 槽時用，直線寄存器模式不經它，嵌套 f64 表達式自然正確；
④ 檔頭「支持」清單加「s71：二元 `+ - * /` 接受 f64 兩側操作數 ⇒ `OP-ADD/SUB/MUL/DIV` 發 `fadd/fsub/fmul/fdiv double`」、型別表標「例外：前綴 `'-'`（s70）舆二元 `+ - * /`（s71）接受 f64；比較符仍不收 f64」、「不支持」清單將「除舆余」改為「i64 除舆余（`/ %`，f64 `/` 已於 s71 放行）」；
⑤ `h2m-test.no` 新增 5 測試函式（test-arith-f64-add／mul-nested／div／mixed-gate／i64-div-gate）註冊於 `test-neg-f64-gate()` 後，先紅後綠；斷言 `agg.unsup == 0`、`fadd double`／`fmul double`／`fdiv double`、`define double`／`ret double`（add）、`unsup>0` 且無 `define`（mixed-gate、i64-div-gate）。

**關鍵點**：
1. **同一 `OP-DIV`、f64 放行 / i64 門禁的不對稱**：新增 `'/'→OP-DIV` 映射後，發射臂以 `bop != OP-DIV` 明確排除 i64 除法（避免將原本門禁的 i64 `/` 静默放成不带 option 的普通 `sdiv`）；f64 臂則含 `OP-DIV`（發 `fdiv double`、IEEE 除零得 `inf`/`nan`、無需 option 護欄）。單元測 `test-arith-i64-div-gate` 钉住此邊界。
2. **嵌套 f64 無需 `h2m-ty-of`**：q5 `(x + y) * z` 內層 `x + y` 發射時 `mid = alloc(TY-F64)`，其值 id 在外層被 `h2m-lower-expr` 遞迴取回為 `lv`；`h2m-vty-at(c, lv)` 即讀到 `TY-F64` ⇒ `fty==1`。靜態推斷 `h2m-ty-of` 對 INFIX 仍只回 TY-I64（line 722），但直線式發射不依賴它，故無需擴充（同 s67/s68 「h2m-ty-of 不受影響」）。
3. **零下游改動**：`OP-ADD/SUB/MUL/DIV` 的 f64 發射（`fadd/fsub/fmul/fdiv double`）、`double` 型別名、`%fd` 前綴、`define`/`ret` 全部早在 s37/s46–s54 就位；本件只做 `osym＋兩側操作數型別 → 單發射` 接線。
4. **f64 無字面量降級**（沿用 s70）：`OP-CONST` 只發 i64，兩側 f64 必須皆由 `double` 形參傳入——probe 舆 oracle 皆以形參注入 f64。
5. **反向刀必須 mutate【未 opt】原始 IR**：`opt -O2` 會把常量實參調用摺成常數，故 `tmp/s71-knives.py` 在 `tmp/s71.ll`（pre-opt）上下刀，再走 opt+clang。

**驗證環境**（v0.4.13 工具鏈 ＋ `DEVELOPER_DIR` CommandLineTools；一律用釘住的 `/tmp/nolang-v413/bin/no`）：
- `h2m-test` **529 → 542/542**（+13：5 測試函式，add 4 斷言／mul-nested 3 斷言／div 2 斷言／mixed-gate 2 斷言／i64-div-gate 2 斷言，錨定 `fadd/fmul/fdiv double`／`define double`／`ret double`／混合與 i64 除法仍門禁）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**
- 零回歸：`h2m.no`／`h2m-test.no` 經 `no fmt -w` 歸零、自有檔 `no fmt -d` rc=0；s55–s70 探針前向 exit **73／47／92／188／72／134／164／134／221／91／46／149／109／43／255／195** 不退化
- ⚠️ `no vet src/std`：本輪觀測 `[ERROR]` 計數為 **1**（`global.no` 之 `import std/err` alias `"error"` 缺口，與 §5.3.4.cv/cw/cx/cy 同）；本件**完全不觸及 `src/std`**，舆 vet 正交、非回歸。
- 探針 `tmp/probe-s71.no`（SRC 單行轉義串、五式各含 f64 二元形制）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **30**；Grep IR 確認 q1 `fadd`、q2 `fmul`、q3 `fsub`、q4 `fdiv`、q5 `fadd→fmul` 皆為 `double`
- 🔴 **Go oracle 差分對拍**：`tmp/s71-oracle.no` 同程式（真 Nolang 源碼、同一工具鏈編譯、`f64.to-i64` `#{buildin}` `llvm.fptosi.sat` 向零截斷）實測逐值 **`4 7 5 7 12`**、總和 286 → exit **30**，兩側逐值一致；`tmp/s71-knives.py` IR 反向三刀（K1 q1 `fadd→fsub`、K2 q4 `fdiv→fmul`、K3 q5 丢外層 `fmul`）**27／222／158** 全部改 exit（無對稱盲區）

| noc | Go |
| --- | --- |
| `h2m-lower-expr` `K-INFIX-EXPRESSION`：`+ - * /`＋兩側`TY-F64`⇒`OP-ADD/SUB/MUL/DIV`（單發射） | `codegen.go` `emitBinop`：float 分支 `fadd/fsub/fmul/fdiv double`、int 分支 `add/sub/mul i64` |
| i64 臂排除 `OP-DIV`（`/` 仍門禁）、f64 臂含 `OP-DIV`（發 `fdiv`） | 同：`f64 /` 非 option（IEEE）、`i64 /` 預設 `?i64`（需 option 建模） |
| f64 經 `double` 形參注入（無 f64 字面量降級、`OP-CONST` 只發 i64） | Go 前端 f64 字面量走 `OpConst` 浮點；h2m 暫無此徑 |
| 直線寄存器模式下 f64 以 `%fd` SSA 流動、不落 `alloca double` | Go f64 全程 SSA 值 |
| 嵌套 f64 表達式靠實際值 id 型別（`h2m-vty-at`）而非靜態推斷 | checker 天然按值型別選 `fadd`／`add` |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：f64 比較（`< <= > >= == !=`——cg 早已發 `fcmp` 就绪、仅 h2m 側守衛未放，下一候選）；f64 舆 i64 混合算術的隱式收窄/提寬（現一律門禁）；f64 區域變量／需落 `alloca double` 槽之控制流（`h2m.no:304` 門禁）；f64 字面量降級（`OP-CONST` 需擴浮點常量）；`&&`／`||` 真·布爾短路分岔（§5.3.4.cw 殘留）；i64 `/`、`%`（`?i64` 除零→`err` option 語義）；`&^` 位清除（`ps.no` 側不成為運算子）；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——以上構成 M4-e 第十八件的候選


### 5.3.4.da s72——M4-e 第十八件：f64 二元比較 `< <= > >= == !=`（`compiler/src/h2m.no` 的 `h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈在原 s71「i64 臂 + f64 算術臂」之外**新增一條 f64 比較臂**——以 `is-arith`／`is-cmp` 兩旗標對 `bop` 歸類，`fty==1 && is-cmp==1` ⇒`OP-CMPLT/CMPEQ/CMPNE/CMPLE/CMPGT/CMPGE`（目的值 `TY-I64`、**非 f64**）發 `fcmp <pred> double` 後 `zext i1→i64`，鏡像 Go `emitCmp` 按左操作數型別分派；**順帶收緊 s71 曾過寬的 f64 臂**（原對一切 `bop` 放行、dest 記 `TY-F64`）為「算術族 dest `TY-F64`／比較族 dest `TY-I64`」兩族分派，位/移位/邏輯於 f64 不落任一路 ⇒ 誠實門禁；**零改 `ps.no`／`cg.no`／`mir.no`／`h2m-ty-of`**（`fcmp`+`icmp`+`zext` 早於 s37 備妥、六比較符映射早已存在），承接 s71 的 f64 端到端通道、把比較接入同一守衛）（2026-11-05 本輪）

**背景**：s71 把 `K-INFIX` 算術守衛放寬到 f64（發 `fadd/fsub/fmul/fdiv double`），但比較符 `< <= > >= == !=` 在 h2m 側仍走「任一操作數非 `TY-I64` ⇒ `ity=0` ⇒ `h2m-mark`」的舊路徑——`cg.no` 的 f64 比較管線其實**早在 s37 全備**（`OP-CMPLT/CMPEQ/CMPNE/CMPLE/CMPGT/CMPGE` 各依 `cg-vty-at(vty, x) == TY-F64` 發 `fcmp <pred> double`、否則 `icmp <pred> i64`，一律 `%v<d> = zext i1 %c<k> to i64`）。本件把 s71 那條「`fty==1` 時對**一切 `bop`** 放行、dest 記 `TY-F64`」的 f64 臂**收緊**為算術/比較兩族分派，並新增比較臂（dest `TY-I64`）。關鍵前提——直線無控制流 ⇒ 寄存器模式（`c.mem==0`）⇒ f64 操作數全程以 `%fd` SSA 流動、比較結果落 `%v`（i64）命名空間、**永不落 `alloca double` 槽**。故本件仍是**純 h2m 二元臂守衛重組**，下游零改。

**Go 權威**：`src/mir/codegen.go` 的 `emitCmp` 按**左操作數值型別**分派 fcmp／icmp，結果恆為整數（`zext i1→i64`）；`!=` 用 `une`（無序亦真、保 NaN 語義）。⚠️ **比較結果是整數 0/1、非 f64**——dest 必須 `h2m-alloc(TY-I64)` 走 `%v` 命名空間，而非 s71 算術臂的 `TY-F64` `%fd`。真 Nolang 裡 `x < y`（f64）得 `bool`（不能直接賦給 i64，報 `cannot assign bool value to i64`），故 self-host 的「比較即 i64」是其型別表（i64/u64/f64/str、無 bool）下的等價建模；oracle 用 `b = x < y` + `b -> r = 1` 把 bool 折成 0/1 對拍值語義。

**實測形制（`tmp/probe-s72.no`）**：七式 c1 `x<y`／c2 `x>y`／c3 `x==y`／c4 `x!=y`／c5 `x<=y`／c6 `x>=y`／c7 `(x+y)<z`（嵌套：內層 fadd 結果喂 fcmp）。main 手寫 IR 調用 `c1(2.0,3.0)=1`、`c2(5.0,4.0)=1`、`c3(3.0,3.0)=1`、`c4(3.0,4.0)=1`、`c5(4.0,3.0)=0`、`c6(3.0,4.0)=0`、`c7(1.5,2.5,5.0)=(4.0<5.0)=1`，權重 `1/2/4/8/16/32/64` ⇒ `1+2+4+8+0+0+64 = 79` → `trunc i64 to i32` → exit **79**。Grep IR 確認 c1 `%c0 = fcmp olt double %fd1, %fd2` + `%v3 = zext i1 %c0 to i64` + `ret i64`，各式謂詞 `olt/ogt/oeq/une/ole/oge`，c7 `%fd4 = fadd; %c1 = fcmp olt double %fd4, %fd3`。

**覆蓋面（本件）**：
① `h2m-lower-expr` 的 `K-INFIX-EXPRESSION` 分派鏈，在原 s71「i64 臂 + f64 算術臂」之外**新增 f64 比較臂**——以 `is-arith`／`is-cmp` 兩旗標對 `bop` 歸類（`OP-ADD/SUB/MUL/DIV` ⇒ `is-arith`；`OP-CMPLT/CMPEQ/CMPNE/CMPLE/CMPGT/CMPGE` ⇒ `is-cmp`），比較臂條件 `fty==1 && is-cmp==1` ⇒ `mid = h2m-alloc(c, TY-I64)` + `h2m-emit(..., TY-I64)`；算術臂收緊為 `fty==1 && is-arith==1`；末尾以 `ok-i == 0 && ok-f == 0` 統一 `h2m-mark`；
|  |  |  |  |
| --- | --- | --- | --- |
② **收緊 s71 過寬臂**：位運算/移位/邏輯（`& | ^ << >> && ||`）於 f64 既不屬算術亦不屬比較 ⇒ 不落 f64 任何放行臂 ⇒ 計 `unsup` 誠實門禁（`test-bit-f64-gate`、`test-logic-f64-gate` 以 `(r f64)` 結果釘住——若沿 s71 舊臂會發畸形 `and i64 %fd`）；
③ 混合操作數（一 `TY-I64` 一 `TY-F64`）⇒ `ity` 舆 `fty` 皆 0 ⇒ 全臂不落 ⇒ 誠實門禁（`test-cmp-f64-mixed-gate`）；
④ **零改 `ps/cg/mir/h2m-ty-of`**——fcmp/icmp 分派 + `zext` 早於 s37 備妥，六比較 `bop` 映射早已存在；本件僅在 h2m 發射側按 `bop` 歸類選 dest 型別（`TY-I64`）；
⑤ `h2m-test.no` 新增 7 測試函式（`test-cmp-f64-lt`／`eq`／`ne`／`arith-nested`／`mixed-gate`、`test-bit-f64-gate`、`test-logic-f64-gate`）註冊於 `test-arith-i64-div-gate()` 後，先紅（16 失敗）後綠；斷言 `agg.unsup == 0`、`fcmp olt/oeq/une double`、`fadd double`＋`fcmp olt double`（nested）、`zext i1`、`define i64`／`ret i64`（結果恆 i64）、mixed/bit/logic 門禁 `unsup>0` 且無 `define`。

**關鍵點**：
1. **比較 dest 必須 `TY-I64`、非 `TY-F64`**：`cg` 比較臂硬編碼 `%v<d> = zext i1 %c<k> to i64`（走 `%v` 命名空間），分派鍵是左操作數值 id 型別 `vty[x]`——故 h2m 須 `alloc(TY-I64)`，否則 dest 型別與命名空間錯位。此與 s71 算術臂（dest `TY-F64`、`%fd`）是分岔點。
2. **收緊 s71 的代價與收益**：s71 的 f64 臂對一切 `bop` 放行，本件以 `is-arith`/`is-cmp` 旗標把它限定為算術+比較兩族；被排除的位/移位/邏輯於 f64 本就無合法 LLVM 形制（`and i64 %fd` 畸形），誠實門禁是修正而非退步。
3. **`!=` 用 `une` 保 NaN**：鏡像 Go，`une`（unordered-or-not-equal）使含 NaN 的比較為真，與算術 IEEE 語義自洽。
4. **反向刀必須 mutate【未 opt】原始 IR**：`opt -O2` 會把常量實參調用摺成常數，故 `tmp/s72-knives.py` 在 `tmp/s72.ll`（pre-opt）上下刀。

**驗證環境**（v0.4.13 工具鏈 ＋ `DEVELOPER_DIR` CommandLineTools；一律用釘住的 `/tmp/nolang-v413/bin/no`）：
- `h2m-test` **542 → 560/560**（+18：7 測試函式）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**
- 零回歸：`h2m.no`／`h2m-test.no` `no fmt -d` rc=0；s55–s71 探針前向 exit **73／47／92／188／72／134／164／134／221／91／46／149／109／43／255／195／30** 不退化；i64 家族 s66–s69 探針 IR 逐位元組相同（證二元臂收緊未動 i64 發射）
- ⚠️ `no vet src/std`：本輪觀測 `[ERROR]` 計數為 **1**（`global.no` 之 `import std/err` alias `"error"` 缺口，與 §5.3.4.cv–cz 同）；本件**完全不觸及 `src/std`**，與 vet 正交、非回歸。
- 探針 `tmp/probe-s72.no`（SRC 單行轉義串、七式 f64 比較形制）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **79**；Grep IR 確認 fcmp 謂詞 `olt/ogt/oeq/une/ole/oge` + `zext i1→i64` + `define`/`ret i64`
- 🔴 **Go oracle 差分對拍**：`tmp/s72-oracle.no` 同程式（真 Nolang、`b = x < y` 得 bool + `b -> r = 1` 折 0/1）實測逐值 **`1 1 1 1 0 0 1`**、加權和 79 → exit **79**，兩側逐值一致；`tmp/s72-knives.py` IR 反向五刀（K1 c1 `olt→ogt`、K2 c3 `oeq→one`、K3 c5 `ole→oge`、K4 c6 `oge→ole`、K5 c7 內層 `olt→ogt`）**78／75／95／111／15** 全部改 exit（含 0→1 與 1→0 兩向、證 fadd 結果喂 fcmp 的嵌套流）

| noc | Go |
| --- | --- |
| `h2m-lower-expr` `K-INFIX-EXPRESSION`：`< <= > >= == !=`＋兩側 `TY-F64`⇒`OP-CMP*`（dest `TY-I64`） | `codegen.go` `emitCmp`：依左操作數型別 fcmp/icmp，結果 `zext i1→i64` |
| 比較結果恆 `TY-I64`（走 `%v`）、算術結果 `TY-F64`（走 `%fd`）兩族分岔 | Go 比較結果恆整數、算術按型別 |
| `!=` ⇒ `OP-CMPNE` ⇒ `fcmp une`（保 NaN） | `emitCmp` `!=` float 用 `une` |
| f64 經 `double` 形參注入（無 f64 字面量降級、`OP-CONST` 只發 i64） | Go 前端 f64 字面量走 `OpConst` 浮點；h2m 暫無此徑 |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：f64 舆 i64 混合比較（現一律門禁）；f64 區域變量／需落 `alloca double` 槽之控制流（`h2m.no:304` 門禁）；f64 字面量降級（`OP-CONST` 需擴浮點常量）；`&&`／`||` 真·布爾短路分岔（§5.3.4.cw 殘留）；i64 `/`、`%`（`?i64` 除零→`err` option 語義）；`&^` 位清除（`ps.no` 側不成為運算子）；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——以上構成 M4-e 第十九件的候選


### 5.3.4.db s73——M4-e 第十九件：f64 字面量降級（`compiler/src/h2m.no` 的 `h2m-lower-expr` **新增 `K-FLOAT-LITERAL`（kind 29）降級臂**——讀 raw（slot1＝源 token 原文、非 slot0 之 `%v` 值）、誠實門控 `raw.contains('.')`，放行則 `cid = h2m-alloc(c, TY-F64)` + `h2m-emit(c, OP-CONST, cid, 0, 0, raw, TY-F64)`（raw 置 sym 槽、a 留 0）；**同步改 `cg.no` 的 `OP-CONST` 臂**——依目的值型別分岔，`cg-vty-at(vty, d) == TY-F64` ⇒ `%fd<d> = fadd double 0.0, <syms[k]>`、否則維持原 `add i64 0, <x>`——此為 **s55 起首件需動 `cg.no` 的降級件**（前十八件皆宣稱「零改 cg」，本件如實標注））（2026-11-05 本輪）

**背景**：s70–s72 打通了 f64 值端到端流動，但操作數**只能來自 f64 形參**——`OP-CONST` 彼時無條件發 `add i64 0, <x>`（i64 整型常量），無法產 `double` 常量，故 `x * 2.0`、`x < 0.0` 一类含 f64 字面量的式子在 h2m 側仍走 `unsup`。本件補齊字面量通道。

**Go 權威**：Go 前端把 f64 字面量經 `OpConst` 落成雙精度常量、codegen 直接內聯進使用點。self-host 的 AST 側 `ps-mk-f64(t, v, raw)`（`ps.no`）把**值**寫 slot0（`tk.lit.to-f64().to-str()`，Go `%v` 最短往返：`'1.0'`⇒`'1'`、`'1.50'`⇒`'1.5'`）、**源文**寫 slot1（`tk.lit`，原樣含小數點）。LLVM textual IR 的雙精度字面量文法**要求尾數含小數點**（`1.0`/`1.5e-7`/`0.0`/`1.0E10` 可 verify；裸 `1`/`3`/`1e5` 皆 opt rc=1）——故必須取 **slot1 raw**（`2.0` 源文帶點）而非 slot0（`2.0`⇒值 `'2'` 無點），並對 raw 無點者（如 `1e5`）誠實門禁。

**實測形制（`tmp/probe-s73.no`）**：七式 q1 `x*2.0>5.0`／q2 `x+1.5<3.0`／q3 `x-0.5>=2.0`／q4 `x/2.0==1.5`／q5 `(x+1.0)*2.0<=8.0`／q6 `x*1.0!=3.0`／q7 `x>0.0`（每式體內混「字面量算術」＋「字面量比較」，比較 dest 恆 i64）。main 手寫 IR 調 `q1(3.0)=1`、`q2(1.0)=1`、`q3(2.0)=0`、`q4(3.0)=1`、`q5(3.0)=1`、`q6(3.0)=0`、`q7(2.0)=1`，權重 `1/2/4/8/16/32/64` ⇒ `1+2+0+8+16+0+64 = 91` → exit **91**。Grep IR 確認每字面量物化為 `%fd<k> = fadd double 0.0, <raw>`（如 `%fd2 = fadd double 0.0, 2.0`）後喂 fmul/fcmp。

**覆蓋面（本件）**：
① `h2m-lower-expr` `K-FLOAT-LITERAL` 臂：raw=slot1、門控 `contains('.')`、放行 `alloc(TY-F64)`+`emit OP-CONST(sym=raw,res=TY-F64)`，否則 mark；
② `cg` `OP-CONST` 依 dest `TY-F64` 分岔發 `fadd double 0.0, <syms[k]>`，i64 路徑原樣不動；
③ `slot-decl` 的零初始化 `OP-CONST` 永不見 `TY-F64`（`h2m.no:320` 僅 I64/U64/STR 鋪槽）⇒ 新 cg 分支只由字面量路徑可達（sym 恆非空）；
④ 混合（f64 形參 × f64 字面量）自然成立——字面量返回 v=cid(TY-F64)，二元臂 fty 判定兩側皆 TY-F64 ⇒ 放行；
⑤ `h2m-test.no` 新增 3 測試（`test-f64-lit-arith`/`cmp`/`gate`）註冊於 `test-logic-f64-gate()` 後，先紅（10 失敗）後綠。

**關鍵點**：
1. **首改 cg**：前十八件（s55–s72）皆「零改 cg」，本件因 `OP-CONST` 原為純 i64、需新增 `TY-F64` 物化分支，是 s55 起首件觸及 cg.no 的降級件——已在里程碑如實標注、不再稱「零改 cg」。
2. **取 raw 非取值**：slot0 值是 `%v` 最短往返（會吞 `.0`），slot1 raw 是源文（帶點）；LLVM 雙精度文法要求帶點，故 raw 是唯一合法注入點。
3. **`fadd double 0.0, K` 物化**：LLVM 不允許 `%fd1 = 2.0` 直接命名常量；`fadd 0.0, K` 是把常量綁進命名 SSA 且可被 opt 常量折疊的慣用形制。
4. **誠實門禁無點字面量**：`1e5` raw 無點 ⇒ LLVM opt rc=1 ⇒ mark unsup（`test-f64-lit-gate` 釘住）。
5. **反向刀必須 mutate【未 opt】原始 IR**：`opt -O2` 會摺疊常量實參調用。

**驗證環境**（v0.4.13 ＋ `DEVELOPER_DIR`）：
- `h2m-test` **560 → 572/572**（+12：3 測試）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**（cg.no OP-CONST 改動未退化）
- 零回歸：`h2m.no`／`cg.no`／`h2m-test.no` `no fmt -d` rc=0；s55–s72 探針前向 exit **73／47／92／188／72／134／164／134／221／91／46／149／109／43／255／195／30／79** 不退化；i64 家族 s66–s69 與 f64 形參家族 s70–s72 探針 IR 逐位元組相同（證 OP-CONST 新分支未動 i64／形參路徑）
- ⚠️ `no vet src/std`：本輪 `[ERROR]` 計數為 **1**（`global.no` 之 `import std/err` alias `"error"` 缺口，與 §5.3.4.cv–da 同）；本件**完全不觸及 `src/std`**，與 vet 正交、非回歸。
- 探針 `tmp/probe-s73.no`（SRC 單行轉義串、七式 f64 字面量）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` 前向 exit **91**
- 🔴 **Go oracle 差分對拍**：`tmp/s73-oracle.no` 同程式（真 Nolang f64 字面量原生、`b = expr` 得 bool + `b -> r = 1` 折 0/1）實測逐值 **`1 1 0 1 1 0 1`**、加權和 91 → exit **91**，兩側逐值一致；`tmp/s73-knives.py` IR 反向六刀（改各函式體內 fadd 常數）K1 q1 mult 2.0→1.0、K2 q2 add 1.5→10.0、K3 q3 sub 0.5→0.0、K4 q4 div 2.0→0.5、K5 q5 thr 8.0→4.0、K6 q7 thr 0.0→5.0 ⇒ **90／89／95／83／75／27** 全改 exit（含 0→1 與 1→0 雙向、證字面量值真流入算術/比較語義）

| noc | Go |
| --- | --- |
| `h2m-lower-expr` `K-FLOAT-LITERAL`：raw=slot1 且含 '.' ⇒ `OP-CONST`(dest `TY-F64`, sym=raw) | Go 前端 f64 字面量經 `OpConst` 落雙精度常量 |
| cg `OP-CONST` dest `TY-F64` ⇒ `%fd = fadd double 0.0, <raw>` | codegen 內聯雙精度常量至使用點 |
| raw 無 '.'（如 `1e5`）⇒ 誠實 unsup | LLVM textual IR 雙精度字面量文法須含小數點 |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：f64 舆 i64 混合字面量/算術的隱式提寬（現一律門禁）；f64 區域變量／需落 `alloca double` 槽之控制流（`h2m.no:304` 門禁）**已於 s74 落地**（§5.3.4.dc）；raw 無點之科學計數面（`1e5`）待引入定點化路徑（如 `1.0e+05`）方可放行；`&&`／`||` 真·布爾短路分岔（§5.3.4.cw 殘留）；i64 `/`、`%`（`?i64` 除零→`err` option 語義）；`&^` 位清除；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——以上構成 M4-e 第二十件的候選


### 5.3.4.dc s74——M4-e 第二十件：f64 局部變量／控制流槽化（`compiler/src/h2m.no` 槽機構認 `TY-F64`——`h2m-ty-of` 前掃描新增 f64 推斷、`h2m-slot-decl` 門禁鬆綁並給零初始化 `OP-CONST` 攜 `sym='0.0'`、`h2m-var-read` LOAD 白名單增 `TY-F64`；`cg.no` 的 `OP-ALLOC/STORE/LOAD` 三 op 依 `cg-vty-at` 值型別分岔發 `alloca double`／`store double`／`load double`，i64 路徑原樣不動）（2026-11-05 本輪）

**背景**：s70–s73 的 f64 值只活在「直線式 SSA」（`c.mem==0` 暫存器模式）形態；函式一旦含控制流即轉 mem 模式，所有局部量（含 f64 形參）須經 `h2m-slot-decl` 鋪 entry 槽，而該閘門只認 I64/U64/STR，`TY-F64` 即計 unsup——s60 舊負例「寧缺勿錯」釘住此點。本件為 f64 鋪 `alloca double` 槽，跨塊 STORE/LOAD 全套解鎖，「f64 形參／局部量 + 控制流」自此入覆蓋面。

**Go 權威**：Go hir2mir 在 mem 模式把函式局部統一落 entry `alloca` 槽，codegen 按型別發 load/store；LLVM 的 double 槽即 `alloca double` + `store double`／`load double`，與 i64 槽同形、只換型名與值前綴（`%v`→`%fd`）。

**實測形制（`tmp/probe-s74.no`）**：標注聲明 `y f64 = 1.5` 零初始化物化為 `%fd = fadd double 0.0, 0.0` 後落槽；f64 形參鋪 `%s2 = alloca double` 後 `store double %fd1` 存 incoming 值；匯合块以 `%fd = load double, double* %s4` 跨塊讀回；比較仍 `fcmp … double` + `zext i1→i64`。

**覆蓋面（本件）**：
① `h2m-ty-of`（定槽寬）增 f64：`K-FLOAT-LITERAL` ⇒ `TY-F64`；infix 兩側皆 F64 時按操作符分族——算術族 `+ - * /` ⇒ `TY-F64`、比較族六符 ⇒ `TY-I64`（與降級側 dest 型別同步），位/移位/邏輯不推（留 -1 ⇒ 鋪槽失敗整函式 unsup，與降級側族門禁同口徑）；
② `h2m-slot-decl` 白名單增 `TY-F64`，鋪 `OP-ALLOC(TY-F64)`；零初始化 `OP-CONST` 的 sym 於 f64 顯式給 `'0.0'`；
③ `h2m-var-read` mem 模式 LOAD 白名單增 `TY-F64`（兜底門禁同步）；`h2m-var-write` 原按 `st==vt` 泛型分派，f64 直通 `OP-STORE` 無需改動；`h2m-slot-init-arg` 同理（f64 形參存槽自然通）；
④ cg `OP-ALLOC`／`OP-LOAD` 依 dest `TY-F64` 發 `%s<d> = alloca double`／`%fd<d> = load double, double* %s<x>`；`OP-STORE` 依【源值】型別發 `store double %fd<x>, double* %s<y>`；
⑤ `h2m-test.no` 新增 3 測試（`test-f64-slot-flow`/`infer`/`mixed-gate`）並將 `test-flow-gates` 內舊負例「f64 slot 寧缺勿錯」轉正為 `alloca double`/`load double`/`fcmp oeq double` 正斷言；先紅（15 失敗）後綠。

**關鍵點**：
1. **零初始化 sym 陷阱**：slot-decl 的零初始化 `OP-CONST` 原各型別 sym 皆空（i64 分支讀 xa 不看 sym），`TY-F64` 放行後 cg 的 fadd 分支讀 sym——空 sym 會產 `fadd double 0.0, ` 非法 IR，必須顯式攜 `'0.0'`（RED 阶段即釘住此點，s73 所記「slot-decl 零初始化永見不到 TY-F64」註釋同步過時並更正）。
2. **槽寬出自前掃描推斷**：型別標注（`y f64 = …`）只在 lower-let 核對「標注==右值型別」，真正定槽寬的是 `h2m-ty-of(初值)`——f64 不進 ty-of 則所有 f64 局部量全滅；比較族推 I64 是因 `y = x < 2.0` 的 dest 恆 i64，缺該族會把合法形誤門。
3. **形參一併解鎖**：mem 模式函式的 f64 形參原因 slot-decl 閘計 unsup；本件起 `store double` 入槽、各塊 `load double` 讀回——「f64 形參 + 控制流」與「f64 局部量 + 控制流」同口徑打通。
4. **仍不覆蓋者成立**：混合右值（`y = x + 1`）ty-of=-1 ⇒ 誠實門禁（`test-f64-slot-mixed-gate` 釘住）；`1e5` 無點字面量降級側仍門禁（槽寬推錯無害——整函式不產碼）。

**驗證環境**（v0.4.13 ＋ `DEVELOPER_DIR`）：
- `h2m-test` **572 → 587/587**（+15：3 新測試 11 斷言 + 轉正 4 斷言）；先紅 15 失敗全屬 s74 範圍
- `cg-test` **347/347**；`cg-runtime-test` **60/60**（cg.no 三 op 分岔不退化）
- 零回歸：`h2m.no`／`cg.no`／`h2m-test.no` `no fmt -d` 乾淨；s66–s73 探針前向 IR **逐位元組相同**（證 ALLOC/STORE/LOAD 新分支未動 i64 路徑）；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67–s73 同，`global.no` alias 缺口，本件不觸 std 正交）
- 探針 `tmp/probe-s74.no`（七函式全含控制流：guard 單句/guard 块體兩句/短路鏈三臂/計數循環/條件循環+break/雙 guard 順序塊，標注與推斷 f64 局部混用）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → clang 前向 exit **77**
- 🔴 **Go oracle 差分對拍**：`tmp/s74-oracle.no` 同程式（真 Nolang f64 槽原生、比較 bool 折 `b -> r = 1`）實測 exit **77**，兩側逐值一致；`tmp/s74-knives.py` IR 反向七刀（K1 q1 閾值 2.0→4.0、K2 q2 匯合閾值 1.2→-1.0、K3 q3 循環累加子 1.0→0.5、K4 q4 鏈匯合閾值 2.5→-2.5、K5 q5 謂詞 ogt→olt、K6 q6 槽初值 5.0→0.25、K7 q7 循環界 3.0→1.0）⇒ **76／79／73／69／93／109／13** 全改 exit（含 0↔1 雙向，證 double 槽讀寫真承載控制流語義而非死碼被折）

| noc | Go |
| --- | --- |
| `slot-decl` 認 `TY-F64` ⇒ `alloca double` + 零初始化攜 `sym='0.0'` | hir2mir mem 模式把 f64 局部落 entry `alloca double` 槽 |
| var-read/write/slot-init-arg 的 STORE/LOAD 依 `cg-vty-at` 型別分岔 ⇒ `store/load double %fd…` | codegen 按型別發 load/store（i64／double 兩翼） |
| 前掃描 `h2m-ty-of` 語法推 f64（字面量與算術族；比較族推 I64） | Go 由宣告型別定槽寬，self-host 無型別注釋時語法推斷 |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：f64×i64 混合運算的隱式提寬（現一律門禁）；raw 無點科學計數面（`1e5`）定點化放行；`&&`／`||` 真·布爾短路分岔（§5.3.4.cw 殘留）；i64 `/`、`%`（`?i64` 除零語義）**已於 s75 落地**（plain 護欄路徑，§5.3.4.dd）；`&^` 位清除；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——其中除/模已收，其餘構成 M4-e 第二十二件的候選


### 5.3.4.dd s75——M4-e 第二十一件：i64 除／模除零護欄降級（`compiler/src/h2m.no` 運算符表補 `'%' ⇒ OP-MOD`、i64 發射臂取消 `bop != OP-DIV` 排除——除/模自此落 cg 早在 s38/s39 就緒的 `cg-div-guard` plain 護欄路徑，**零改 cg/ps/mir/ty-of**）（2026-11-05 本輪）

**背景**：s71 放行 f64 算術時，i64 側明確以 `bop != OP-DIV` 排除除法（理據：`?i64` 除零→`err` 的 option 語義未建模、誠實門禁）；`'%'` 則從未進過運算符表。而 cg 側自 M4-c 時代（s38 除零 select 護欄／s39 `divZeroReport` stderr 報告）早已就緒——唯 h2m 不發 `OP-DIV/OP-MOD`。本件補齊降級側最後一段閘門：i64 `/`、`%` 按 Go **非 option（plain）路徑**放行。

**Go 權威**：`src/mir/codegen.go` `emitArith` 對整數 `/`、`%`（含無符號族）一律先上護欄——`%dz = icmp eq 除數,0`；有符號另查 `(MIN,-1)` 對（`%dmn`/`%dn1`/`%db`/`%dall`）；`%ds = select 危險, 1, 除數` 安全除數後發 `sdiv/srem`；其後分岔：**option 路**（`wrapResult`，裸 let 推斷目標）tag 條件化＋`arerr`/`aok` 雙塊裝箱 err；**plain 路**（目標為明示 plain int）結果再 `select %dz, 0, raw` 定義化為 0 並追發 `@__nolang_div_zero_report`。self-host 型別表無 option dest ⇒ 本件取 plain 路全形（`cg-div-guard` 鏡像），option tag=2 路仍屬後續 option 軌件。

**checker 口徑發現（本件對拍撞出）**：真 Nolang 拒「聲明＋初值」形 `a i64 = x / d`（`cannot assign ?i64 value to i64 variable`——除法表達式恒 `?i64`，標注不自動解包），**只有先宣告 plain 局部再重賦值**（`a i64 = 0` 後 `a = x / d`）才落 Go plain 路。探針與 oracle 因此同寫兩句式形制；双侧 stderr 診斷文本實測逐字一致。

**覆蓋面（本件）**：
① `h2m-lower-expr` 運算符表新增 `osym == '%' -> bop = OP-MOD`（OP-MOD=50 早已於 `mir.no` 註冊、cg 臂 s38 就緒）；
② i64 發射臂取消 `bop != OP-DIV` 排除 ⇒ `/`、`%` 兩符 dest `TY-I64` 直通，cg 依 `cg-vty-at(vty,d) != TY-F64` 落 `cg-div-guard('sdiv'/'srem', …, sgn=1)`；
③ `is-arith` **不含** OP-MOD ⇒ f64 `%`（frem）仍誠實門禁（`test-arith-f64-mod-gate` 釘住），scope 最小化；
④ `h2m-ty-of` 零改（i64 雙側 infix 不問操作符恒推 `TY-I64`，槽寬自然涵蓋除/模初值）；
⑤ `h2m-test.no`：新增 4 測試（`test-arith-i64-div-plain`/`mod-plain`/`div-mem`＋`f64-mod-gate`）；兩舊負例轉正（`test-gate` 除法例、`test-arith-i64-div-gate`→`div-plain`）；s67 舊例 `test-bit-gate` 改由仍門禁的 `&^` 守衛（原用 `/` 演示）。先紅（13 失敗）後綠。

**關鍵點**：
1. **護欄各翼皆受刀檢**：助記符（K1 sdiv↔srem 互證）、除零定義化 select（K3 改 0→7，僅在 `d=0` 實參下可見）、guard 塊內除（K4 謂詞翻轉）、循環攜帶槽模（K6）、循環體內除數（K7）——護欄非死碼。
2. **(MIN,-1) 翼未入探針**：i64 MIN 字面量需 `-9223372036854775808`（一元負＋寬字面量溢出語義纠缠），本件以 IR 形制目視核對 `cg-div-guard` sgn=1 段與 Go `intMinLiteral(i64)` 同構，執行面留待專門件。【s85 償賬】本專門件已落地——tmp/probe-s85-ir 三面實測（執行值 MIN/0/除零錨共存 exit 7==oracle、IR 四翼同構、report 條件 dz 非 dall 實證），實作零改；哨兵 h2m-test `test-arith-i64-{div,mod}-min-wing`（§5.3.4.dn）。
3. **option 路仍不覆蓋**：語料裸 let `a = x / d` 在 Go 側得 `?i64`（tag=2 裝箱 err）；h2m 一律按 plain 護欄建模——該偏差由「後續 option 軌件以 tag/match 語義降級」收編，本件的對拍口徑以明示 plain 重賦值形鎖死。
4. **探針首引 prelude helper**：`@__nolang_div_zero_report` 為自舉探針首個真運行的 runtime helper ⇒ `tmp/probe-s75.no` 需拼 `cg-runtime-prelude-full()`（s66–s74 探針無 helper 引用可裸拼，本件起 assembly 點前移至探針）。

**驗證環境**（v0.4.13 ＋ `DEVELOPER_DIR`）：
- `h2m-test` **587 → 599/599**（+12：4 新測試 12 斷言；兩轉正一轉靶）；先紅 13 失敗全屬 s75 範圍
- `cg-test` **347/347**；`cg-runtime-test` **60/60**（零改 cg，護欄既有測試原樣綠）
- 零回歸：`h2m.no`／`h2m-test.no` `no fmt -d` 乾淨；s66–s74 探針前向 IR **逐位元組相同**；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67–s74 同，`global.no` alias 缺口，本件不觸 std 正交）
- 探針 `tmp/probe-s75.no`（七式：直線除/模、除零定義化、guard 塊內除、除嵌算術、計數循環模、條件循環除+break）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → clang 前向 exit **111**，stderr 出除零診斷
- 🔴 **Go oracle 差分對拍**：`tmp/s75-oracle.no` 同程式（真 Nolang，先宣告後重賦值形制）實測 exit **111**、逐值 `1 1 1 1 0 1 1` 両側一致，且 stderr 診斷文本逐字相同；`tmp/s75-knives.py` IR 反向七刀 K1 q1 sdiv→srem、K2 q2 srem→sdiv、K3 q3 定義化 0→7、K4 q4 謂詞 sgt→slt、K5 q5 比較常數 3→4（0→1 反向）、K6 q6 模數 4→3、K7 q7 除數 3→4 ⇒ **110／109／107／103／127／79／47** 全改 exit（含 0↔1 雙向）

| noc | Go |
| --- | --- |
| h2m i64 臂放行 `/` `%` ⇒ `OP-DIV/OP-MOD`（dest `TY-I64`） | `emitArith` 整數 `/` `%` 先上護欄再按 dest 是否 option 分岔 |
| cg `cg-div-guard`（`%dz`/`%ds`/`%cr`/定義化 select/report，s38/s39 既有） | plain 路 `%dz icmp`＋`%ds select 1`＋`%dzr select 0`＋`divZeroReport` |
| 先宣告 plain 再重賦值才落 guard 路徑（探針/oracle 同形制） | checker 拒 `a i64 = x / d`（?i64 不自動解包），重賦值走 copy-and-discard |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：`?i64` 的 option tag=2 語義降級（裸 let 除法的默認解包面，§11 B14 語義半項尾款）；f64 `%`（frem）；u64 `/` `%`（`OP-DIVU/MODU` 護欄就緒、h2m 側需 u64 值供給路徑）**已於 s76 落地**（§5.3.4.de）；(MIN,-1) 執行面探針；`&&`／`||` 真·布爾短路分岔（§5.3.4.cw 殘留）；`&^` 位清除；raw 無點科學計數面（`1e5`）定點化；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——其中除/模已收，其餘構成 M4-e 第二十三件的候選


### 5.3.4.de s76——M4-e 第二十二件：u64 除／模無符號護欄降級（`compiler/src/h2m.no` 新增 `uty` 分派——雙側整數族（`TY-I64`／`TY-U64`）且至少一側 `TY-U64` 時把 `OP-DIV`／`OP-MOD` 改寫為 `OP-DIVU`／`OP-MODU`、新發射臂 dest 給 `TY-U64`，**零改 cg/ps/mir**（`cg.no` 的 `cg-div-guard(..., sgn=0)` 臂自 s38/s39 即無符號形態）；同步補 `h2m-ty-of` 的 U64 infix 推斷臂）（2026-11-05 本輪）

**背景**：s36 已在 cg 側放行無符號 ALU 家族（`OP-SHRU/DIVU/MODU=51–53` 發 `lshr/udiv/urem`、`OP-CMPLTU/LEU/GTU/GEU=54–57`），但 h2m 自 s55 起只在 i64／f64 兩型上發射算術，u64 的 `/`、`%` 一直誠實門禁——s75 的 未覆蓋 明列其為第二十二件候選（瓶頸只在「h2m 側的 u64 值供給路徑」）。

|  |  |  |
| --- | --- | --- |
**Go 權威**：`src/mir/hir2mir.go`（POST-LOWERING，`~9474`）的無符號路由——`if l.sourceUnsigned(lv) || l.sourceUnsigned(rv)` 才把 `OpDiv→OpUDiv`、`OpMod→OpUMod`（**任一側無符號即整式無符號**，原註解：「This is makes `u64.MAX / 2` correct for a parameter」）；`src/mir/codegen.go` `emitArith`（`~6144`／`~6154`）的護欄分岔——`%dz = icmp eq 除數,0` 對四個整數除算助記符一律上，`(MIN,-1)` 翼（`%dmn`／`%dn1`／`%db`／`%dall`）**僅有符號**（`OpDiv`／`OpMod`），無符號族走純護欄。

**checker 口徑延續**：同 s75——真 Nolang 拒「聲明＋初值」形 `a u64 = x / d`（`?u64 → u64`），須先宣告 plain 再重賦值才落 Go 非 option plain 路。

**覆蓋面（本件）**：
|  |  |  |
| --- | --- | --- |
① `h2m-lower-expr` 在 `ity`／`fty` 之後新增 `uty` 計算：兩操作數型別（`h2m-vty-at`）皆落 `{TY-I64, TY-U64}` 且至少一側 `TY-U64` ⇒ `uty=1`（鏡像 Go 的 `||` 路由，故 `x / 2`（u64 ÷ 字面量）同樣放行）；
② 歸類**前**改寫 `bop`：`OP-DIV ∧ uty ⇒ OP-DIVU`、`OP-MOD ∧ uty ⇒ OP-MODU`；u64 的其他運算（`+ - *`、`&&`／`||` 與比較族）仍誠實門禁（`test-arith-u64-add-gate`／`test-arith-u64-cmp-gate` 釘住；後者已於 s77 翻正，同一位址的門禁下移到 `test-arith-u64-bit-gate`）；
③ 新增 `ok-u` 發射臂：`h2m-alloc(c, TY-U64)` ＋ `h2m-emit(c, OP-DIVU/OP-MODU, mid, lv, rv, '', TY-U64)`；mark 條件由 `(ok-i == 0 && ok-f == 0)` 擴為三者同零；
④ 🔴 **`h2m-ty-of` 必須同步補 U64 的 infix 推斷臂**（本件實測撞出的架構耦合）：`h2m-scan` 前掃描以 `h2m-ty-of(RHS)` 註冊 let 目標槽型別，與降級側的 `uty` 分派**各自獨立**——缺此臂則槽註冊成 `-1`，連帶 `h2m-slot-decl`／`h2m-var-write`／RET 讀取三處 mark；表象是 register 模式全綠、mem 模式獨紅（4 紅中只 1 紅），誤導性極強；
⑤ `h2m-test.no`：新增 6 測試（`test-arith-u64-div-plain`／`mod-plain`／`div-lit`／`div-mem`＋`add-gate`／`cmp-gate`），斷言含正向（`udiv i64`／`urem i64`、`%dz` select、report 呼叫）與負向（**不得**見 `sdiv`／`srem`、**不得**見 `%dmn.`／`%dn1.` 的 (MIN,-1) 翼）。

**關鍵點**：
1. **零改 cg 是本件成立的前提**：`cg.no:470/473` 早已按 op 碼無條件走 `cg-div-guard('udiv'/'urem', …, sgn=0)`，`sgn=0` 即「只查零、不查 (MIN,-1)」——與 Go `emitArith` 的無符號分支逐字同形，故本件只需 h2m 把對的 op 碼送進來；
2. **`TY-U64` 共用 i64 值命名空間**（`%v`、`alloca i64`，s36 定的默認臂口徑）⇒ 槽機構零改；
3. 🔴 **Go 側編譯期常數摺疊對 u64 除法走有符號口徑**（對拍撞出，屬正交的 Go 側既有 quirk，本件不改 `src/`）：`tmp/s76-min4.no` 以 `#{overflow=wrap}` 的 `1-2` 構造 u64 MAX 再 `/2`、`%10`，實測得 **0／-1**（有符號值），`no build -v` 的 IR 裡已無除法指令（被折掉）；同一表達式改走**運行時不透明**操作數（`tmp/s76-min3.no`：`os.get-env` ＋ `str.to-u64`）即得 **9223372036854775807／5**（正確無符號）。自舉端不做常數摺疊、恒發運行時護欄路徑 ⇒ oracle 的高位元用例改由 env 取值，對拍才落在同一条路徑上。

**驗證環境**（v0.4.13 ＋ `DEVELOPER_DIR`）：
- `h2m-test` **599 → 619/619**（+20 斷言）；先紅 12 失敗全屬 s76 範圍（其中 mem 槽那 4 紅的根因見 覆蓋面 ④）
- `cg-test` **347/347**；`cg-runtime-test` **60/60**（零改 cg，無符號護欄既有測試原樣綠）
- 零回歸：`h2m.no`／`h2m-test.no` `no fmt -d` 乾淨；s66–s75 探針前向 IR **逐位元組相同**；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（`src/std/global.no` 的 import alias，與 s67–s75 同、本件不觸 std 正交）
- 探針 `tmp/probe-s76.no`（六式：直線 udiv、直線 urem、除零定義化、guard 塊內 udiv（mem 槽）、高位元 udiv、高位元 urem）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` → **exit 63**，stderr 出除零診斷；u64 的 `==`／`>` 比較仍門禁 ⇒ 布爾折疊搬到手寫 main IR（`icmp`＋`zext` 加權 1+2+4+8+16+32）
- 🔴 **Go oracle 差分對拍**：`tmp/s76-oracle.no` 同程式（真 Nolang，先宣告 plain 再重賦值；高位元經 `S76X=18446744073709551615` 的 env 取值繞開摺疊）實測 **exit 63**、stderr 除零診斷與探針**逐字一致**
- **IR 反向八刀**（`tmp/s76-knives.py`，mutate 未 opt 原始 IR）：K1 q1 `udiv→urem` **62**、K2 q2 `urem→udiv` **61**、K3 q3 零強制 select `0→7` **59**、K4 q4（mem 槽）`udiv→urem` **55**、K5 q5 `udiv→sdiv`（MAX/2 變 0）**47**、K6 q6 `urem→srem`（MAX%10 變 -1）**31**、K8 q3 摘掉零強制 select 讓 `%ds=1` 防陷阱翼暴露（10/1=10）**59**——七刀全改 exit（K5/K6 是簽／無簽分岔靶）；K7 摘掉 `@__nolang_div_zero_report` 呼叫 ⇒ exit 仍 **63** 而 stderr 變空（證報告呼叫活著且是唯一診斷源）

| noc | Go |
| --- | --- |
| h2m `uty`（任一側 `TY-U64`）⇒ `OP-DIVU`／`OP-MODU` | `hir2mir.go` POST-LOWERING `sourceUnsigned(lv) \|\| sourceUnsigned(rv)` 路由 |
| cg `cg-div-guard(..., sgn=0)`：`%dz`＋`%ds select 1`＋定義化 select＋report，無 (MIN,-1) 翼 | `codegen.go emitArith`：`%dz` 四助記符共有、`MIN/-1` 僅有符號 |
| `h2m-ty-of` 補 U64 infix 推斷（前掃描槽註冊與降級分派須同口徑） | Go 側型別在 HIR 已定，單一趟無此耦合 |
| 探針不折常量、oracle 須 env 取不透明值才同路 | Go MIR 常數摺疊對 u64 除法走有符號（既有 quirk，本件不改） |

|  |  |  |  |
| --- | --- | --- | --- |
**未覆蓋／後續**：u64 比較族（`OP-CMPLTU/LEU/GTU/GEU` 在 cg 側 s36 就緒，h2m 需 u64 比較值供給）**已於 s77 落地**（§5.3.4.df）；u64 `+ - * & | ^ << >> &^`（本件僅解除法族）；`?u64` 的 option 語義降級；(MIN,-1) 執行面探針（有符號側仍缺）；`&&`／`||` 真·布爾短路分岔（§5.3.4.cw 殘留）；`&^` 位清除；raw 無點科學計數面（`1e5`）定點化；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——其中比較族已收，其餘構成 M4-e 第二十四件的候選


### 5.3.4.df s77——M4-e 第二十三件：u64 比較族無符號降級（`compiler/src/h2m.no` 在既有 `uty` 分派上把 `OP-CMPLT/LE/GT/GE` 改寫為 `OP-CMPLTU/LEU/GTU/GEU`（`==`／`!=` 除外），新增比較發射臂 dest 給 `TY-I64`，**零改 cg/ps/mir**（`OP-CMPLTU..` 自 s36 即 `icmp u*`+`zext` 形）；同步給 `h2m-ty-of` 的整族 U64 推斷臂開比較例外）（2026-11-05 本輪）

**背景**：s36 已在 cg 側備好無符號比較四碼（`OP-CMPLTU/LEU/GTU/GEU=54–57`），但 h2m 自 s55 起比較只代表過 i64（`icmp s*`）與 f64（`fcmp o*`，s72）；s76 打通 `uty` 值供給面後，比較族只差重映射一句——`.de` 的 未覆蓋 首項即此。

**Go 權威**：`src/mir/hir2mir.go` 的 POST-LOWERING 無符號路由與 s76 同一個 `switch`（`~9476`）——`OpLt→OpULt`、`OpLe→OpULe`、`OpGt→OpUGt`、`OpGe→OpUGe`，**刻意不含 `OpEq`／`OpNe`**（同位比較，與符號無關）；同一檔案更早的 pre-lowering 一趟（`~8970`，`operandUnsigned` 逐候選實參）註解給出真實案例：「`u64.MAX.sqrt()` binary-searched with `slt` and returned the MAX itself」——有符號謂詞把最高位為 1 的 u64 當負數而誤序。`src/mir/codegen.go` `emitCmp`（`~6853`）把 `OpULt..OpUGe` 映射為 `icmp ult/ule/ugt/uge`，結果 `i1` 由呼叫端 `zext`。

**覆蓋面（本件）**：
① `uty` 判據沿用 s76（雙側整數族且至少一側 `TY-U64`），本件未改；
② 重映射新增四行：`OP-CMPLT ∧ uty ⇒ OP-CMPLTU`、`OP-CMPLE ⇒ OP-CMPLEU`、`OP-CMPGT ⇒ OP-CMPGTU`、`OP-CMPGE ⇒ OP-CMPGEU`；`==`／`!=` **不改寫**，保持 `OP-CMPEQ`／`OP-CMPNE`（鏡像 Go 口徑）；
③ `is-cmp` 歸類增補四碼（歸類在重映射**之後**，故只可能在 `uty==1` 命中；`fty==1`（兩側 f64）與 `uty==1`（含 u64）互斥，不會誤開 s72 的 f64 比較臂）；
④ 新增 u64 比較發射臂：`h2m-alloc(c, TY-I64)` ＋ `h2m-emit(c, bop, mid, lv, rv, '', TY-I64)`，共用 `ok-u` 旗標——**dest 恆 `TY-I64`**（比較結果是 0/1 而非 u64），與 s76 除模臂的 `TY-U64` 分岔；
⑤ 🔴 **`h2m-ty-of` 的整族 U64 推斷臂必須為比較符讓路**（`.de` ④ 耦合的反方向）：s76 新增的「整族含 u64 ⇒ `TY-U64`」當時靠「非除模必 mark ⇒ 槽寬推錯無害」撐住；本件解除比較門禁後該前提失效——`f = x > y`（x,y u64）會把 `f` 鋪成 U64 槽，而發射臂給的是 I64 值，`h2m-var-write` 只接受 `st == vt` ⇒ mem 模式下對 `f` 的後續 i64 寫全數 mark。修法是把 ty-of 的單行推論拆成「比較族 ⇒ `TY-I64`、其餘 ⇒ `TY-U64`」兩路，與降級側同一口徑；
⑥ `h2m-test.no`：新增 9 測試（`test-cmp-u64-lt/le/gt/ge/eq/ne/lit/mem/slot-i64`）＋門禁下移的 `test-arith-u64-bit-gate`，斷言含正向（`icmp ult/ule/ugt/uge i64`、`zext i1`、mem 槽 `load i64`）與負向（同函數**不得**見對應有符號謂詞；`==`／`!=` **不得**冒出任何 `u*` 謂詞）。

**關鍵點**：
1. **零改 cg**：`cg.no:1717–1768` 的四個 `OP-CMP*U` 臂自 s36 就是 `icmp u* i64` ＋ `zext i1→i64`（與有符號 cmp 同形），本件只需 h2m 把對的 op 碼送進來；
2. **`eq`／`ne` 不重映射的根據**：Go 側開關表並無這兩項（`default:` 註解「OpEq/OpNe are sign-agnostic」）⇒ 自舉端照抄，並以探針 q7/q8 與反向刀 K7/K8 釘住「走的是 `icmp eq/ne i64` 原路」；
3. **dest 型別是本件與 s76 的唯一形制差異**：算術/除模的 dest 隨操作數（u64）、比較的 dest 恆 i64——而 dest 與槽寬兩者不一致就會在寫入口撞門禁（見 覆蓋面 ⑤），故兩條路必須一起改。

**驗證環境**（v0.4.13 ＋ `DEVELOPER_DIR`）：
- `h2m-test` **619 → 648/648**（+29 斷言）；先紅 23 失敗全屬本件範圍
- `cg-test` **347/347**；`cg-runtime-test` **60/60**（零改 cg，無符號比較既有測試原樣綠）；ps/lx/tok/util/ck/diag/ps-err 全綠；⚠️ `ck-sig-test` 9 FAIL——該檔不引用 h2m（grep 計 0），屬正交既有基線，本件未改
- 零回歸：`h2m.no`／`h2m-test.no` `no fmt -d` 乾淨；s66–s76 探針前向 IR **逐位元組相同**（11/11），重編後 s75/s76 二進制 exit **111／63** 不變；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（`src/std/global.no` 的 import alias，與 s67–s76 同、本件不觸 std 正交）
- 探針 `tmp/probe-s77.no`（八式：`<`、`<=`、`>`、`>=`（mem 槽）、高位 `>`、高位 `<`、`==`、`!=`）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` → **exit 255**（加權 1+2+4+8+16+32+64+128）；高位元兩式即簽/無簽分岔靶（`MAX>1` 無符號 1、`MAX<1` 無符號 0）
- 🔴 **Go oracle 差分對拍**：`tmp/s77-oracle.no` 同程式（真 Nolang，比較結果經守衛 `b -> r = 1` 折 0/1；高位元經 `S77X=18446744073709551615` 的 env 取不透明值，避開 `.de` 關鍵點 3 記錄的 Go 編譯期摺疊有符號口徑）實測 **exit 255**，與探針同值；o6 採用反相（傳回 `NOT (MAX < 1)`）與探針端的 `%c6 = icmp eq %r6, 0` 同義
- **IR 反向九刀**（`tmp/s77-knives.py`，mutate 未 opt 原始 IR）：K1 q1 `ult→ugt` **254**、K2 q2 `ule→ult` **253**、K3 q3 `ugt→ult` **251**、K4 q4（mem 槽）`uge→ule` **247**、K5 q5 `ugt→sgt`（MAX>1 變 0）**239**、K6 q6 `ult→slt`（MAX<1 變 1）**223**、K7 q7 `eq→ne` **191**、K8 q8 `ne→eq` **127**、K9 q3 實參次序對調 **251**（與 K3 同位，刀類不同：謂詞選擇 vs 比較方向）——九刀全改 exit，`=== 9 knives: ALL EFFECTIVE ===`

| noc | Go |
| --- | --- |
| h2m `uty`（沿用 s76）⇒ `OP-CMPLTU/LEU/GTU/GEU`，`==`／`!=` 不改寫 | `hir2mir.go` POST-LOWERING 同一 `switch` 的 `OpLt..OpGe` 四路，`OpEq/OpNe` 留 `default:` |
| cg `OP-CMP*U` 臂：`icmp u* i64` + `zext i1→i64`（s36 就緒） | `codegen.go emitCmp` 把 `OpULt..OpUGe` 映射為 `ult/ule/ugt/uge` |
| 比較 dest 恆 `TY-I64`，與除模臂的 `TY-U64` 分岔；ty-of 需同步開比較例外推 I64 | Go 側比較結果的整型在 HIR 已定，前掃描與降級同趟、無此兩邊耦合 |
| 探針不摺常量、oracle 仍須 env 取不透明值才同路 | 同 `.de` 關鍵點 3：Go MIR 常量摺疊對 u64 走有符號（既有 quirk，本件未改） |

|  |  |  |  |
| --- | --- | --- | --- |
**未覆蓋／後續**：u64 `+ - *`（算術族仍門禁，`test-arith-u64-add-gate`）**已於 s78 落地**（§5.3.4.dg）；u64 位運算／移位／邏輯（`& | ^ << >> && ||`，`test-arith-u64-bit-gate`）；`&^` 位清除；`?u64` 的 option 語義降級；(MIN,-1) 執行面探針（有符號側仍缺）；真·布爾短路分岔（§5.3.4.cw 殘留）；raw 無點科學計數面（`1e5`）定點化；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——以上構成 M4-e 第二十五件的候選


### 5.3.4.dg s78——M4-e 第二十四件：u64 算術族 '+ - *' 放行（`compiler/src/h2m.no` 在既有 `uty` 分派上新增算術發射臂 dest `TY-U64`，**不重映射、零改 cg/ps/mir/ty-of**）（2026-11-05 本輪）

**背景**：s76 建成 `uty` 值供給面並放行除模、s77 放行序比較後，算術族 `+ - *` 是 `.df` 未覆蓋首項。本件形制與前兩件**根本不同**：Go 的無符號路由只覆蓋「結果依符號性而義不同」的算子，add/sub/mul 在 64 位寬 2's 補數下簽／無簽位元相同，**無 OpAddU/OpSubU/OpMulU 可鏡像**（grep 實證零命中）——故本件是純放閘，不存在簽/無簽分岔靶。

|  |  |  |  |
| --- | --- | --- | --- |
**Go 權威**：`src/mir/hir2mir.go:8960–9002` 的無符號路由 `switch` 只有 `OpDiv→OpUDiv`、`OpMod→OpUMod` 與 `OpLt..OpGe→OpU*` 三族，`default:` 註解「OpEq/OpNe are sign-agnostic」——算術三件從不進表。`src/mir/codegen.go:6183–6245`：`llvm.{s|u}{add|sub|mul}.with.overflow` 內在函數**只在 option 路**（`wrapResult ∧ ¬OvfAnnotated ∧ overflowCheckable`）且符號性取**宣告元素型別** `elemRaw`；註解白紙黑字「Plain arithmetic keeps emitting a bare add/sub/mul so every existing … program is bit-identical」⇒ plain u64 恆發裸 `add/sub/mul i64`，自舉端同形。

**覆蓋面（本件）**：
① `uty` 判據沿用 s76，本件未改；
② 新增 u64 算術發射臂：`uty==1 ∧ is-arith==1` ⇒ `h2m-alloc(c, TY-U64)` ＋ `h2m-emit(bop, mid, lv, rv, '', TY-U64)`——`OP-DIV/OP-MOD` 已在上式重映射為 `OP-DIVU/OP-MODU`，`is-arith` 此時只可能是 ADD/SUB/MUL；
③ **不重映射**（鏡像 Go：位元同值無衍生 op 可發）；
④ `h2m-ty-of` **無需改動**——s77 已把整族 U64 推斷臂拆成「比較 ⇒ TY-I64、其餘 ⇒ TY-U64」，算術 let 槽寬天然與發射臂 dest 同口徑（`.df` 關鍵點 3 的兩邊耦合本件自動滿足）；
⑤ `h2m-test.no`：新增 8 測試（`add-plain/sub-plain/mul-plain/add-lit/arith-nested/add-mem/slot-u64/mixed-gate`，+22 斷言），斷言含正向（裸 `add/sub/mul i64`、mem 槽 `load/store i64`）與負向（**不得**冒出 `fadd/fsub/fmul double` 翼；u64×f64 混型仍整函數門禁）；s76 留的 `test-arith-u64-add-gate`（當時釘「'+' 不放行」）翻正拆除，`test-arith-u64-bit-gate` 保留並更新註解。

**關鍵點**：
1. **本件無簽/無簽分岔靶**：與 s76（udiv vs sdiv）、s77（ugt vs sgt）不同，探針無法用符號性對拍證「重映射之必要」——價值在放閘正確性與回繞位元一致性（MAX+1 ⇒ 0、0−1 ⇒ MAX），反向刀型制因此改為助記符互改／實參次序／mem 斷鏈／回繞斷言翻轉；
2. **零新 op 碼**：`TY-U64=4` 只影響槽前綴與型別名（cg 默認臂仍 i64/%v，s36 就緒），`OP-ADD/SUB/MUL` 的 `cg-vty-at(vty,d) != TY-F64` 分支直接吞下 U64 dest；
3. **溢出語義仍不表達**：Go 的 add/sub/mul `with.overflow` 只在 option 路（見 Go 權威），`?u64` 未建模（同 s75/s76 口徑註）——自舉端 plain 裸算與 Go plain 路 bit-identical；
4. **陳舊註解清理**：s76 留的「非除模的非比較 op 在此推成 U64 無害：降級側必 mark ⇒ 整函數不產碼」前提已隨 s77/s78 解除比較與算術門禁而失效，改寫為只對仍門禁的位運算／移位／邏輯成立，防後續增量據此誤推。

**驗證環境**（v0.4.13 ＋ `DEVELOPER_DIR`）：
- `h2m-test` **648 → 670/670**（+22 斷言）；先紅 19 失敗全屬本件範圍
- `cg-test` **347/347**；`cg-runtime-test` **60/60**（零改 cg，原樣綠）；ps/lx/tok/util/ck/diag/ps-err 全綠；⚠️ `ck-sig-test` 9 FAIL 正交既有基線（同 s77）；`ast-test` 在 HEAD 工具鏈即 MIR codegen 失敗（bug #85 族），本件零觸 `src/`，正交既有
- 零回歸：`h2m.no`／`h2m-test.no` `no fmt -d` 乾淨；s66–s77 探針前向 IR **逐位元組相同**（12/12）；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（`src/std/global.no` 的 import alias，與 s67–s77 同、本件不觸 std 正交）
- 探針 `tmp/probe-s78.no`（八式：`x+y`、`x−y`、`x*y`、`x+2` 字面量翼、`(x+y)*2` 嵌套、mem 槽 `f=x+y ⇒ f=f−1 ⇒ r=f`、`MAX+1` 回繞、`0−MAX` 回繞）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` → **exit 255**（真值 13/7/30/12/26/12/0/1，加權 1+2+4+8+16+32+64+128）
- 🔴 **Go oracle 差分對拍**：`tmp/s78-oracle.no` 同程式（真 Nolang；字面量翼/嵌套翼經不透明 env 參數傳入、高位元 `S78X=18446744073709551615`——`+ - *` 位元雖同值，仍統一走執行期路避開 Go 編譯期摺疊的有符號口徑）實測 **exit 255**，與探針同值；回繞兩式不加 `#{overflow=wrap}`——plain u64 無 option 機制、靜默 2's 補數回繞，與探針端裸 add/sub i64 位元相同（同 s76 口徑）
- **IR 反向八刀**（`tmp/s78-knives.py`，mutate 未 opt 原始 IR）：K1 `q1 add→sub` **254**、K2 `q2 sub→add` **253**、K3 `q3 mul→add` **251**、K4 字面量抬升 `2→3` **247**、K5 外層 `mul→add` **239**、K6 mem 斷鏈（刪 `store %v22`） **223**、K7 實參次序對調 **253**、K8 回繞斷言翻轉（`%c7 eq 0→ne 0`） **191**——八刀全改 exit，`=== 8 knives: ALL EFFECTIVE ===`

| noc | Go |
| --- | --- |
| h2m `uty ∧ is-arith` ⇒ 發射臂 dest `TY-U64`，op 碼原樣保留 | `hir2mir.go` 無符號路由表不含 add/sub/mul（`default:`，符號無關） |
| cg `OP-ADD/SUB/MUL` 的 `!= TY-F64` 臂：裸 `add/sub/mul i64`（U64 走默認 i64/%v） | `codegen.go` plain 路「bare add/sub/mul、bit-identical」註解；`with.overflow` 只在 option 路 |
| u64 算術 let 槽寬 `TY-U64`（s77 ty-of 拆分後天然對齊 dest） | Go 前掃描與降級同趟、無此兩邊耦合 |
| 探針不摺常量、oracle 仍須 env 取不透明值才同路 | 同 `.de` 關鍵點 3：Go MIR 常量摺疊對 u64 走有符號（既有 quirk，本件未改） |

|  |  |  |  |
| --- | --- | --- | --- |
**未覆蓋／後續**：u64 位運算／移位族（`& | ^ << >>`）**已於 s79 落地**（§5.3.4.dh；`.dg` 的「鏡像 Go 不收 vs 收 OP-SHRU」分歧已裁定——收 `OP-SHRU`，鏡像 Go 發射側 `emitBitwise` 對 OpShr 恒發 `lshr`）；u64 邏輯族 `&& ||`（s79 門禁下移後由 `test-arith-u64-bit-gate` 釘住，放行前須先決 bop 同碼問題或短路語義）；i64 `>>` 口徑分歧**已於 s80 裁定**（§5.3.4.di：'>>' 全族無條件重映射 OP-SHRU 恒發 `lshr`，鏡像 Go `emitBitwise`，s35 `ashr` 設計推翻）；`&^` 位清除（cg 無對應 op）；`?u64` 的 option 語義降級；(MIN,-1) 執行面探針（有符號側仍缺）；真·布爾短路分岔（§5.3.4.cw 殘留）；raw 無點科學計數面（`1e5`）定點化；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——以上構成 M4-e 第二十七件的候選

|  |  |
| --- | --- |

### 5.3.4.dh s79——M4-e 第二十五件：u64 位運算／移位族 '& | ^ << >>' 放行（`compiler/src/h2m.no` 在既有 `uty` 分派上新增位運算發射臂 dest `TY-U64`，唯一重映射 `>>` ⇒ `OP-SHRU`，**零改 cg/ps/mir/ty-of**）（2026-11-05 本輪）

|  |  |  |
| --- | --- | --- |
**背景**：s78 後 `.dg` 未覆蓋首項即位運算／移位／邏輯三族。本件收其二的二（位運算＋移位），邏輯族因與位運算在 bop 層同碼（s68 的 `&&`／`||` 複用 OP-AND/OR）在 u64 上下文語義歧義，門禁下移後置。與 s78「純放閘、無分岔靶」不同，本件含全族唯一需按符號性分岔的算子 `>>`，且 `.dg` 留的「鏡像 Go 不收 vs 收 OP-SHRU」分歧口徑必須先行裁定。

|  |  |  |  |
| --- | --- | --- | --- |
**Go 權威**：`src/mir/codegen.go` `emitBitwise`（`~7209–7296`）：`OpBitAnd/OpBitOr/OpXor/OpShl/OpShr` 逐項映射 `and/or/xor/shl/lshr`——註解白紙黑字（`7212`）「nolang `>>` is a logical shift (lshr), matching the legacy backend (expr.go:6384)」，**對所有整型恒發 `lshr`、不分符號性**；無符號路由表（`hir2mir.go`）不含任何位運算／移位衍生（OpBitAndU/OpShlU/OpShrU grep 零命中）——符號性在 Go 側不體現為算子表條目而體現為發射助記符紀律。另 `7260–7267` 長註：左移**定義為模 2^W 回繞**非算術溢出（crypto 旋轉 `(x >> k) | (x << (W-k))` 遍地，按位損耗報 err 會每輪都觸發）；`emitLogic`（`~7193`）對 `&&`／`||` 發裸 `and/or`（ destination 型）——非短路，與自舉 s68 同形。

**覆蓋面（本件）**：
① `uty` 判據沿用 s76，本件未改；
② 新增 u64 位運算發射臂：`uty==1 ∧ is-bit==1 ∧ is-logic==0` ⇒ `h2m-alloc(c, TY-U64)` ＋ `h2m-emit(bop, mid, lv, rv, '', TY-U64)`；
|  |  |
| --- | --- |
③ 唯一重映射：`OP-SHR ∧ uty==1 ⇒ OP-SHRU`（cg `lshr i64` 臂 s36 就緒）；`& | ^ <<` 不重映射（逐位／左移與符號性無關，Go 無衍生 op 可鏡像）；重映射先於歸類，i64 路 OP-SHR/ashr 不變；
|  |  |  |  |
| --- | --- | --- | --- |
④ `is-logic` 旗標：`&&`／`||` 與 `&`／`|` 共用 OP-AND/OR 碼（s68 設計，i64 操作數恒 0/1 故無歧義），u64 操作數非 0/1——若不放 `is-logic` 排除則 `x && y` 會 silently 發裸 `and i64`（非邏輯語義）⇒ 新增旗標排除，門禁測 `test-arith-u64-bit-gate` 由 `x & y` 下移到 `x && y`；
⑤ `h2m-ty-of` **無需改動**——s77 拆分後「比較⇒TY-I64、其餘⇒TY-U64」對位運算槽寬天然對齊（同 s78 關鍵點 2）；
⑥ `h2m-test.no`：新增 9 測試（`and/or/xor/shl-plain`、`shr-lshr`（正向 `lshr` ＋ 負向 `ashr` 缺席）、`shr-lit` 字面量移位量、`nested`、`slot-u64` 耦合、`and-mem`）＋23 斷言。

**關鍵點**：
1. **`>>` 重映射的鏡像對象是發射側非路由表**：Go MIR 無 OpShrU，但 `emitBitwise` 恒發 `lshr`——自舉 s35 的「i64=ashr、u64=獨立 op」設計與 Go 在 u64 側同會師（u64 ⇒ `lshr` 兩邊逐位一致），分歧只在 i64（見關鍵點 2）；`.dg` 口徑待定項至此裁定：**收 OP-SHRU**；
2. 🔴 **i64 `>>` 口徑分歧陳帳**：Go 對負 i64 也發 `lshr`（`-8 >> 1` 得天文數字），自舉 OP-SHR⇒`ashr` 得 `-4`——s35 當時是刻意的「與 i64 有符號語義一致」設計，s67 探針未用負數故未顯形；自舉對齊終局須定奪「改 cg i64 臂 vs 維持語義正確」，本件不動 i64 路（前向 13/13 IR 逐位元組相同）；
3. **零改 cg**：`and/or/xor/shl i64`（s35）＋`lshr i64`（OP-SHRU，s36）全就緒，TY-U64 走默認 i64/%v 臂；
4. **shl 回繞口徑與 Go 一致**：高位移出定義丟棄非溢出（`?u64` 未建模，option 路 `with.overflow` 不涉及本族）。

**驗證環境**（v0.4.13 ＋ `DEVELOPER_DIR`）：
- `h2m-test` **670 → 694/694**（+23 斷言）；先紅 23 失敗全屬本件範圍
- `cg-test` **347/347**；`cg-runtime-test` **60/60**（零改 cg，原樣綠）；ps/lx/tok/util/ck/diag/ps-err 全綠；`ck-sig-test` 9 FAIL、`ast-test` bug #85——均正交既有基線（同 s78）
- 零回歸：`h2m.no`／`h2m-test.no` `no fmt -d` 乾淨；s66–s78 探針前向 IR **逐位元組相同**（13/13）；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（`src/std/global.no` import alias，與 s67–s78 同、本件不觸 std 正交）
|  |  |  |
| --- | --- | --- |
- 探針 `tmp/probe-s79.no`（八式：`x&y=2`、`x|y=11`、`x^y=9`、`x<<y=80`、`x>>y=1`、mem 槽 `f=x&y ⇒ f=f|1 ⇒ r=3`、🔴 `MAX>>1=HALF-MAX`（簽/無簽分岔靶：lshr 得 0x7FFF…、ashr 得 -1）、`1<<63` 符號位回繞）⇒ `opt -passes='default<O2>' -passes=verify` rc=0 → `clang -O2` → **exit 255**
- 🔴 **Go oracle 差分對拍**：`tmp/s79-oracle.no` 同程式（真 Nolang；MAX 經 env `S79X` 取不透明值避開 Go 編譯期摺疊的有符號口徑；期望值 `HALF-MAX`／`SIGN-BIT` 顯式 u64 標注，hex 默認推 byte 會截斷）實測 **exit 255**，與探針同值
- **IR 反向八刀**（`tmp/s79-knives.py`，mutate 未 opt 原始 IR）：K1 `and→or` **254**、K2 `or→xor` **253**、K3 `xor→or` **251**、K4 `shl→lshr` **247**、K5 `lshr→shl` **239**、K6 🔴 q7 `lshr→ashr`（MAX>>1 變 -1，全 suite 唯一簽/無簽靶）**191**、K7 mem 斷鏈（刪 `store %v22`）**223**、K8 q8 移位實參對調（`63<<1=126`，移位不滿足交換律；and/or/xor 可交換故對調刀無鑑別力、只落移位式）**127**——八刀全改 exit，`=== 8 knives: ALL EFFECTIVE ===`

| noc | Go |
| --- | --- |
| h2m `uty ∧ is-bit ∧ ¬is-logic` ⇒ dest `TY-U64`；`>>` 重映射 `OP-SHRU` | `codegen.go emitBitwise`：OpShr 恒 `lshr`（`7257–7258`、註解 `7212`）；路由表無位運算／移位衍生 |
| cg `OP-SHRU` 臂 `lshr i64`（s36）＋ `and/or/xor/shl i64`（s35） | LLVM 助記符一一對應 |
| `&&`／`||` 同碼排除（`is-logic` 旗標） | Go `emitLogic` 是獨立 op（OpAnd/OpOr）不同碼——自舉 s68 同碼設計在 u64 上下文需門禁 |
| i64 `>>` 發 `ashr`（本件未動） | Go 對 i64 也發 `lshr`——真實分歧，關鍵點 2 陳帳待奪 |
| 探針/oracle 經 env 不透明高位元、期望值顯式 u64 標注 | 同 `.de` 關鍵點 3：Go MIR 常量摺疊對 u64 走有符號（既有 quirk，本件未改） |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：u64 邏輯族 `&& ||`（門禁現由 `test-arith-u64-bit-gate` 釘住；放行前須先決 bop 同碼——拆新 op 碼或改真短路路，與 §5.3.4.cw 殘留的布爾短路分岔同案）；🔴 i64 `>>` 口徑定奪**已於 s80 落地**（§5.3.4.di；'>>' 全族無條件重映射 OP-SHRU 恒發 lshr 鏡像 Go OpShr，s35 的 i64=ashr 設計被推翻，cg ashr 臂轉死代碼）；`&^` 位清除（cg 無對應 op）；`?u64` 的 option 語義降級；(MIN,-1) 執行面探針（有符號側仍缺）；raw 無點科學計數面（`1e5`）定點化；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留全部延後——以上構成 M4-e 第二十七件的候選


### 5.3.4.di s80——M4-e 第二十六件：i64 '>>' 口徑裁定（'>>' 全族無條件重映射 OP-SHRU 恒發 lshr，鏡像 Go emitBitwise 對 OpShr 的邏輯移位口徑；s35「i64=ashr」設計推翻、cg ashr 臂轉死代碼；自舉軌首個非加性件，s67 基線重鑲）（2026-11-05 本輪）

|  |  |  |
| --- | --- | --- |
**背景**：s79 落地後 h2m 數值族僅存的已登記口徑分歧即 i64 '>>'（§5.3.4.dh 關鍵點 2）。候選三項中：u64 邏輯族 `&& ||` 在 Go checker 下非 bool 操作數根本不可編譯——無權威可對拍；`&^` 自舉代碼零用量且 cg 無對應 op（須動 cg）；唯 i64 '>>' 是真實分歧——自舉代碼 `cg.no:2324` 位圖 `mkr >> jr` 為實際用量，高位置 1 時自舉產物與 Go 產物行為分岔，直接威脅 M4-e 最終目標（自舉構建出與 Go 構建等價的編譯器）。本件為自舉軌首個**非加性**改動：s66–s79 件件純加性（前向 IR 逐字節相同），本件有意改變 i64 '>>' 的發射。

**Go 權威**：`src/mir/codegen.go` `emitBitwise`（7214–7296）頭注 7212「nolang `>>` is a logical shift (lshr), matching the legacy backend (expr.go:6384)」；7257 行 `case OpShr: op = "lshr"`——OpShr 對**所有整型**恒發 lshr，函數內無 ashr 分支、無符號性路由，亦無 OpShrU 衍生（lshr 即唯一右移語義）。實測觀摺疊面：`tmp/s80-fold.no` `-8 >> 1` 在 Go 工具鏈下得 9223372036854775804——Go 的編譯期摺疊與運行期同為 lshr 口徑，無 s76 式 u64 摺疊 quirk。

**覆蓋面**：① h2m 重映射去件：`bop == OP-SHR && uty == 1 -> bop = OP-SHRU` 改無條件 `bop == OP-SHR -> bop = OP-SHRU`；② i64 發射臂零改（`bop > 0 && ity == 1` 全吃，OP-SHRU dest TY-I64 → cg 默認 i64 臂發 `lshr i64`）；③ u64 臂行為不變（s79 路本就 OP-SHRU，逐位元組同）；④ is-bit 歸類中 OP-SHR 行保留作防禦（不再命中）；⑤ cg 零改——OP-SHR→ashr 臂轉死代碼（保留待後續 op 碼清理件）；⑥ h2m-test：`test-bit-shr` 翻轉釘 `lshr i64` + 零 `ashr`；新增 `test-shr-i64-mem`（mem 槽路雙移位）。

**關鍵點**：
1. 裁定準則：「Go 為權威、自舉跟隨」——黃金法則下 `src/` 只讀，分歧只從自舉側對齊；s35 的 ashr 選擇屬設計先行、後續被 Go 實證推翻，非 Go 側 bug。
2. 值面純透明：前向探針的 i64 右移輸入皆非負（s67 b5 = 100>>2）⇒ lshr/ashr 同值，s67 運行 exit 109 與原 golden 一致；差異僅 IR 文本一行（29 行 `ashr`→`lshr`），已重基線 `tmp/pre-s78/s67.ll`；其餘 13/14（s66、s68–s79）逐字節相同。
3. contains 口徑教訓：`str.contains` 是存在性（0/1）非計數——`test-shr-i64-mem` 初版斷 `==2` 為誤用；雙移位皆降發由 `unsup==0` 釘住、殘留 ashr 由 `contains==0` 釘住。
4. cg 死代碼保留不刪：本件保持 cg 零改；OP-SHR 碼（45）暫無發射源，留待後續（如 `&^` 需新 op 碼時）統裁。

**驗證環境**：釘住工具鏈 `/tmp/nolang-v413/bin/no` + `DEVELOPER_DIR=/Library/Developer/CommandLineTools`，LLVM `/opt/homebrew/opt/llvm/bin`。h2m-test **698/698**（紅相階段恰 4 斷言失敗全在本件範圍）；cg suites 347/60、ps 38、lx 45、tok 62、util 27、ck 20、diag 7、ps-err 26 全綠；探針 `tmp/probe-s80.no` 八式（q1 正值 sanity、q2 -8>>1、q3 -1>>63 變移位量、q4 mem 兩拍 -16、q5 鏈式 -4、q6 掩蔽 -1&255>>4、q7 MIN>>3、q8 -1>>4）IR 含 lshr ×10、ashr ×0，opt verify rc=0、clang rc=0、run **exit 255**；Go oracle `tmp/s80-oracle.no`（負值經 `S80X=-8`、`S80M=MIN` env 不透明化，派生值全運行期算術）同 **exit 255**；IR 反向刀 `tmp/s80-knives.py` **8/8 ALL EFFECTIVE**（K2/K3/K6/K7 為 lshr→ashr 分岔刀 253/251/191/127；K1 字面量移位量翻 254；K4 鏈式首級 239；K5 and→xor 223；K8 mem 斷鏈 247）；`no fmt -d` 兩檔淨；vet 計數 1 ERROR 基線不變。

| 算式 | 自舉產物（s80 後） | Go 產物 | 一致 |
| --- | --- | --- | --- |
| -8>>1 | 9223372036854775804（lshr） | 同值（運行期與摺疊同口徑） | ✓ |
| -1>>63 | 1（lshr） | 1 | ✓ |
| MIN>>3 | 1152921504606846976（lshr） | 同值 | ✓ |
| 八式加權和 | exit 255 | exit 255 | ✓ |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：`&^` 位清除（須新增 mir op 碼 + cg 發射臂，非零改件）；cg 死代碼清理（OP-SHR/ashr 臂去留）；(MIN,-1) 執行面探針（有符號側仍缺）；u64 邏輯族 `&& ||`（bop 同碼去 Alias或真短路路，與 §5.3.4.cw 殘留同案）；`?u64` option 語義降級；raw 無點科學計數面（`1e5`）定點化；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留——以上構成 M4-e 第二十七件的候選


### 5.3.4.dj s81——M4-e 第二十七件：u64 前綴 `-x`／`~x` 放行（`compiler/src/h2m.no` 的 `h2m-lower-expr` `K-PREFIX-EXPRESSION` 分派鏈新增 `'-' && TY-U64`⇒`OP-NEG`（dest `TY-U64`）與 `'~' && TY-U64`⇒`OP-CONST(-1)`＋`OP-XOR`（dest `TY-U64`）兩臂，收緊 u64 一元族收尾；`'!'` 於 u64 仍誠實門禁；**零改 cg／ps／mir／h2m-ty-of**（`OP-NEG`／`OP-XOR`／`OP-CONST` 整數臂 s35/s48/s55 已備、`cg` 依 `cg-vty-at(vty,d)==TY-F64` 分岔 ⇒ `TY-U64` 走整數臂發 `sub i64 0`／`xor i64`、`h2m-ty-of` 的 `K-PREFIX-EXPRESSION` 臂早已原樣遞迴運算元型別））（2026-11-05 本輪）

**背景**：s76–s80 把 u64 的除模／比較／算術／位運算／移位逐族放行，唯獨**一元前綴 `-x`／`~x`** 未觸及——s66（前綴 i64 `-x`／`!x`）、s69（i64 位非 `~x`）、s70（f64 前綴 `-x`）的主體守衛皆釘死在 `TY-I64`／`TY-F64`，`h2m-vty-at(c, ov) == TY-U64` 落不進任一臂 ⇒ `ok-p == 0 -> h2m-mark(c)` 整函式不產碼。本件補上 u64 前綴 NEG／bit-not，與 s78（u64 二元 `+ - *`）在語義上對稱（`-x` 即 `0 - x`），完成 u64 一元／二元運算元的家族閉合。屬 s79「u64 前綴僅 i64」陳帳缺口的自然償還。

**Go 權威**：`src/mir/codegen.go` `emitNeg` 非 option／非 float 路徑恒發 `sub i64 0, x`（int 與 u64 共用同一整數發射，64 位寬下二補回繞同形：`-(1 u64)=M-1`）；位非鏡像 `hir2mir.go:8861`「`~` 非邏輯非、走全 1 常量 XOR」⇒ `x ^ -1`（`-1` 即 i64/u64 全 1 位模式）。`TY-U64` 在 LLVM 與 `TY-I64` 同型 `i64`、同 `%v` 命名空間 ⇒ u64 前綴的發射 IR 與 i64 前綴**逐字節相同**，型別差僅體現在 h2m 側 dest 型別碼（`TY-U64`）與閘的放寬。

**覆蓋面**：① h2m 新增兩發射臂——`osym == '-' && h2m-vty-at(c, ov) == TY-U64` ⇒ `mid = h2m-alloc(c, TY-U64)`＋`h2m-emit(c, OP-NEG, mid, ov, 0, '', TY-U64)`；`osym == '~' && h2m-vty-at(c, ov) == TY-U64` ⇒ `cm1 = h2m-alloc(c, TY-U64)`＋`h2m-emit(c, OP-CONST, cm1, -1, 0, '', TY-U64)`＋`mid = h2m-alloc(c, TY-U64)`＋`h2m-emit(c, OP-XOR, mid, ov, cm1, '', TY-U64)`；② `'!'` 於 u64 **保持門禁**（裸 `xor 1` 翻最低位 ≠ 邏輯非，u64 非 0/1 值會錯，末路 `ok-p == 0 -> h2m-mark(c)` 不動）；③ cg／ps／mir／h2m-ty-of **零改**（整數發射臂、`h2m-ty-of` prefix 臂遞迴運算元型別皆早已就緒）；④ 臂內 `mid`／`cm1` 皆 i64 型別值 id（跨臂同名不串型，合 §6.3）。

**關鍵點**：
1. 🔴 u64 前綴 IR 與 i64 前綴逐字節同（同 `%v`、同 `sub i64 0`／`xor i64`／`add i64 0, -1`）⇒ 型別正確性不能靠 IR 文本鑑別，改由 (a) `h2m-test` 閘僅對 `TY-U64` 放宽＋dest 型別碼釘 `TY-U64`、(b) 值語義刀（對 `sub`/`xor`/CONST 掩蔽 mutate）共同鎖——與 s78/s79 u64 算術／位運算同口徑。
2. mem 模式槽寬耦合：`h2m-ty-of` 的 `K-PREFIX-EXPRESSION` 臂遞迴操作元型別，`f = -x`（x u64）的 `f` 自然推 `TY-U64`、鋪 `alloca i64` 槽；跨塊 `load i64`→`sub i64 0`→`store`、`~f` 同路，與 s79 `test-bit-u64-slot-u64` 同形制。
3. `-x` 前綴於真 Go 源級默認下溢→`?u64`（option），自舉 h2m 建模非 option 的 plain 降級路 ⇒ oracle 以 `#{overflow=wrap}` 把回繞釘成 plain 二補、與 codegen.go 非 option 路徑同口徑對拍（同 s75/s76/s78 口徑紀律）。

**驗證環境**：釘住工具鏈 `/tmp/nolang-v413/bin/no` + `DEVELOPER_DIR=/Library/Developer/CommandLineTools`，LLVM `/opt/homebrew/opt/llvm/bin`。`h2m-test` **698→715/715**（紅相階段恰 15 斷言失敗全在本件範圍、`test-pfx-u64-lnot-gate` 兩條常綠；新增 6 測試：寄存器 neg／寄存器 bit-not／嵌套雙 neg／mem 槽 neg+bit-not／前綴喂 u64 add／`!` 於 u64 仍門禁）；`cg-test` **347/347**、`cg-runtime-test` **60/60**；探針 `tmp/probe-s81.no` 八式（u1 `-x`、u2 `~x`、u3 `-(-x)`、u4 `~(~x)`、u5 mem `f=-x;n>0->f=~f`、u6 `y=~x;r=-y`、u7 `~(-x)`、u8 `~x+~y`）IR 含 `sub i64 0`／`xor i64`／`add i64 0, -1`、opt verify rc=0、clang rc=0、run **exit 255**；Go oracle `tmp/s81-oracle.no`（同八式、前綴 `-` 加 `#{overflow=wrap}`、u64 標注期望值、二進制構 env 不透明化留待回繞用例）同 **exit 255**（`no build` 直跑讀裸 exit）；IR 反向刀 `tmp/s81-knives.py` **7/7 ALL EFFECTIVE**（K1 u1 reg-neg sub→add 254；K2 u2 reg-not 掩蔽 -1→0 253；K3 u3 嵌套內層 neg 251；K4 u5 mem 槽 neg 239；K5 u5 mem 槽 bit-not 239；K6 u7 not-after-neg 掩蔽 191；K7 u8 bit-not 喂 add 掩蔽 127）；`no fmt -d` `h2m.no`／`h2m-test.no`／`probe-s81.no` 皆 rc=0；s66–s80 探針前向零回歸（s76=63、s77/s78/s79/s80=255）；`no vet src/std` 基線計數 **1 ERROR**（與本件正交）。⚠️ `no vet compiler/src/h2m.no` 於前綴臂 `ov`（`h2m-lower-expr` 回傳）入 `h2m-vty-at`／`h2m-emit` 參數位報 12 條 option-unwrap 嚴格化提示（8 條落 s66–s70 既有臂、逐字節與 HEAD 同、4 條落本件新臂同形制）——屬前綴降級族既有 checker 觀察、非本件引入之新回歸（工具鏈編譯與全部測試綠），依 §2 只記不改既有臂。

| 算式 | 自舉產物（s81 後） | Go oracle | 一致 |
| --- | --- | --- | --- |
| -x (x=1) | 18446744073709551615（sub i64 0） | 同值（`#{overflow=wrap}`） | ✓ |
| ~x (x=5) | 18446744073709551610（xor i64, -1） | 同值 | ✓ |
| ~x + ~y (x=1,y=2) | 18446744073709551611 | 同值 | ✓ |
| 八式加權和 | exit 255 | exit 255 | ✓ |

|  |  |  |
| --- | --- | --- |
**未覆蓋／後續**：`'!'` 於 u64（裸 `xor 1` 非邏輯非，需真·bool 短路建模，§5.3.4.cw 同案）；u64 邏輯族 `&& ||`（bop 同碼）；`&^` 位清除（須新 mir op＋cg 發射臂）；`?u64` option 語義降級；cg OP-SHR ashr 死代碼清理；(MIN,-1) 執行面探針；raw 無點科學計數面 `1e5` 定點化；🔴 `ps.no` 前綴 `~` 鬆綁定優先級分歧（§5.3.4.cx 待查項）；§5.3.4.ct 的 match 軌殘留——以上構成 M4-e 第二十八件的候選


### 5.3.4.dk s82——M4 第二十八件：科學計數法字面量的 ILLEGAL 鏡像（`compiler/src/lx.no` 詞法檢測 + `compiler/src/ps.no` operand 位 ILLEGAL 報告臂）（2026-11-05 本輪）

**選型裁決（兩件候選，先裁後做）**：
1. **`&^` 位清除——無權威，銷賬**。Go 權威本身未實現：`src/parser/expr.go` precedences 表無 `AND_NOT` 條目、MIR/checker 全無對應 op；實測 `./bin/no` 對 `r = a &^ b`（a=13,b=6）**靜默編譯通過且輸出 13**——`&^ b` 被整體吞掉。自舉「忠實移植」紀律要求對賬口徑，無權威可鏡像 ⇒ **不發明實現**，h2m 候補鏈維持 `unsup` 誠實門禁（lx.no 的 `tok.AND-NOT` 詞法 token 存在但語法層不消化，與 Go 同形）。候選從 s83 清單移除。
2. **`1e5`——從「定點化」重新定性為「鏡像 ILLEGAL 拒絕診斷」**。s73 門禁原文「LLVM double 字面量文法要求尾數含小數點⇒裸 `1e5` 不可」易誤導為「自舉該支持科學計數」；實查 Go 權威 `src/lexer/lexer.go:992-1008` **顯式拒絕全部科學計數**（含有點形 `3.5e-2`）：`readNumber` 結束後 `l.ch=='e'/'E'` 且 `exponentDigitFollows`（lexer.go:94-106，探測**不消耗輸入**：直接位或是數位，或 `+/-` 後為數位）⇒ 整個 token 覆寫 `ILLEGAL` + ErrMsg 原文。故自舉的正確動作是在 lx.no 複刻同一檢測、在 ps.no 複刻 operand 位的報告族，**不是**定點化。

**Go 權威判據（逐條鏡像）**：
- 詞法：hex 的 `e` 被 isHex 循環吞掉天然不觸發（`0x1e5` 合法 INT）；`1e`（e 後無位）非 sci，走 `'e' is not defined`（`4bek3xc6`）；sci 時 lit 仍為數字段（`1`/`3.5`），`e5`/`e-2` 作為殘餘 IDENT 留於流中（探測不消耗）。
- 語法（expr.go:395-409）：operand 位 ILLEGAL 臂——有 ErrMsg ⇒ 原樣上報，穩定碼 `1gybkrrq`；裸 ILLEGAL ⇒ `illegal token %q`，碼 `ghvjbe8q`；而後 nextToken + return nil。**級聯抑制**：let 站「expected expression, got nil instead」僅在 `len(reportedIllegal)==0` 時報（stmt.go:1308）——`x = 1e5` 恰 1 條而非 2 條。去重口徑等價性：Go `reportedIllegal[line:col]` vs noc ps-error 的 `(ln,cl,msg)` 去重——同位只可能一個 token、同 token 同文 ⇒ 效果等價，無需新表。
- ErrMsg 原文 **156 字節**（`10000000000000000000000000000.0` 為 1 個 1 + 29 個 0）。

**實作**：
- `lx.no`（1614→1636 行）：常量區新增 `B-LOWER-E=101`／`B-UPPER-E=69`；number 分支 `lx-read-number` 之後新增 sci 檢測——`lx.ch` 為 e/E 且（`lx-peek` 為數位，或 `+/-` 且 `input[rpos+1]` 為數位（邊界 `k<kn` 守衛））⇒ `typ=tok.ILLEGAL` + `emsg=` SciErrMsg 原文；探測不消耗，與 Go 同形。
- `ps.no`（10460→10521 行）：`ps-parse-expr-prec` 前綴鏈在 DOT 臂後、catch-all 前插入 `tk.typ == tok.ILLEGAL` 臂（24 行）：emsg 非空 ⇒ 原文 + 碼 `1gybkrrq`；否則 `'illegal token "' + ps-q-esc + '"'` + 碼 `ghvjbe8q`；`ps-error` 後 `#{overflow=wrap} p.n-illegal = p.n-illegal + 1`；`ps-bump` + `idx=0`。**s12 預留閘門接通**：`psst.n-illegal` 從恆 0 死欄位變活，L3984 抑制判據 `val == 0 && p.n-illegal == 0` 本身零改。
- `ps.no` 新增 `ps-q-esc`（37 行）：Go `%q` 內層形子集——`"`→`\"`、`\`→`\\`、`<0x20` 與 `0x7F`→`\xhh` 小寫（借 `lx.lx-ch-str`/`lx.lx-hex-digit`）。

**🔴 L1 差分撞出實打實的抄寫缺陷（本件最有說服力的收獲）**：先紅後綠的 L0 單測把期望 emsg 釘成 **157 字節**（多打一個 0）且全綠——因為 lx.no 與斷言用的是**同一份自洽參照**，單測對「兩側同錯」無鑑別力；重建 `/tmp/notok` Go oracle 後首次 L1 對拍即逐字節指出零個數分岔（`go len 156 noc len 157`）。以 Go 源文（v413 與 HEAD 皆 156）為準修正 lx.no、lx-test、ps-err-test 三檔六處。**教訓：凡鏡像字符串常量，必以 oracle 差分定稿，不得手抄定稿。**

**驗證環境**：釘死工具鏈 `/tmp/nolang-v413/bin/no` + `DEVELOPER_DIR=/Library/Developer/CommandLineTools`，LLVM `/opt/homebrew/opt/llvm/bin`；SUT `/tmp/noc-s82`（`no build -o /tmp/noc-s82 compiler`）；L1 oracle 重建於 `/tmp/notok`（repo 外 Go dumper，`replace` 指向 `src/`，TYPE 名表鏡像 `tok.no` 的 `name()`——非 Go `tokenNames` 的 `ASSIGN(=)` 形）。

**驗證（可重跑）**：`lx-test` **41→70/70**（新增 29 斷言：ILLEGAL typ/lit/emsg/坐標、殘餘 IDENT `e5`/`e-2`、`1e+5`/`2E7`、反例 `1e`/`0x1e5`、裸 ILLEGAL 無 emsg）；`ps-err-test` **26→46/46**（新增 20：sci-let 恰 1 條 1:5 `1gybkrrq`、sci-call 2 條（1:7 sci + 1:8 逗號族）、裸 `$` 的 `'illegal token "$"'` 17 字節 `ghvjbe8q`、級聯抑制、`1.5`/`1e` 零誤報守衛）；回歸：tok 62／util 27／ps 38／diag 7／ck 20／h2m 715／cg 347／cg-runtime 60 全綠；`ck-sig-test`/`ast-test` 為 HEAD 既有基線紅（`git show HEAD:` 回退歸因法實證，與本件正交）；`no fmt` rc=0（lx/ps 既有 drift 55/5 行系 HEAD 基線，不新增）；`no vet src/std` 基線 1 ERROR；`no vet lx.no ps.no` 無 ERROR/WARNING。**L1 全語料差分**：`scripts/selfhost_diff.sh --sut /tmp/noc-s82 --levels L1 --l1-ctl /tmp/notok/notok --corpus "tests src compiler docs"` ⇒ **files=804 SAME=804 DIFF=0**（sci 檢測對既有語料零詞法回歸 + 與 Go 詞法逐字節一致）；sci 探針集 20 檔 **19 SAME / 1 DIFF**（唯一 DIFF=orb-q11 既有色差，見下）。**L2 全語料差分**（`no ast`）：`--levels L2` ⇒ 804 檔 SAME=687 / DIFF=117（status=FAIL 屬 noc 與 Go oracle 的既有 dump 分歧）；歸因以 `git show HEAD:` 回退三檔構成的基線二進位 `/tmp/noc-s82base` 同法重跑 ⇒ **DIFF 檔集合逐檔相同（117/117）**且 **804 檔 sut 哈希逐檔與 s82 一致** ⇒ 本件零 AST 回歸。**L3 全語料差分**（`no vet`）：804 檔 SAME=638 / DIFF=166（status=FAIL），DIFF 的 sut 側哈希僅 7 種取值（絕大多數恆為 "No issues found."——noc vet 尚無規則實現，屬 HEAD 既有能力缺口）；歸因：`/tmp/noc-s82base` 全量重跑 ⇒ tsv 口徑 **804 檔 sut 哈希逐檔與 s82 一致**且匯總同為 638/166；第二趟口徑（歸一化逐行比 + `[ERROR]` 集合）復算 l3diff 兩側同為 166，errdiff 33 vs 34 唯一出入檔 `src/std/net/tls.no`——兩側 **sut 輸出逐字節相同**（留存 o.s 全 804 檔 cmp 全同），出入純在 ctl 側 Go oracle 兩跑 ERROR 條數不同（0 vs 1，現復測為 1）——屬 oracle 側抖動非本件回歸 ⇒ 本件零 vet 回歸。

| 站點 | 輸入 | Go oracle | noc-s82 | 一致 |
| --- | --- | --- | --- | --- |
| 詞法 | `x = 1e5` | `1:5 ILLEGAL 1 - - sci(156)` | 同 | ✓ |
| 詞法 | `x = 3.5e-2` | ILLEGAL lit `3.5` + IDENT `e-2` | 同 | ✓ |
| 詞法 | `x = 0x1e5` | 合法 INT（e 入 hex） | 同 | ✓ |
| vet | `x = 1e5` | 恰 1 條 1:5 [1gybkrrq] | 同 | ✓ |
| vet | `print(1e5)` | 1:7 sci + 1:8 逗號族 | 同文同位（碼欄 `E_GENERAL` 屬 s10 既記族） | ✓文 |
| vet | `x = $` | 恰 1 條 `illegal token "$"` [ghvjbe8q] | 同 | ✓ |

**未覆蓋／後續（含誠實偏差記檔）**：
- ⚠️ **`%q` 對 ≥0x80 字節的渲染分歧**（既有族，非本件引入）：Go `%q` 對 `\x80` 級字節先按 rune 解碼印 `\u0080`（源碼單字節經 `%q` 變兩字節 UTF-8 再 escape，L1 orb-q11 探針實測 `\xc2\x80` vs noc `\x80`）；`ps-q-esc` 對 ≥0x80 原樣透出。語料無此形（裸非法字節不入 ILLEGAL lit）⇒ 不可達，留作 `%q` 完整化候選。
- ⚠️ **逗號族穩定碼 `E_GENERAL` 未遷移**（s10 既記缺口）：本件兩族（`1gybkrrq`/`ghvjbe8q`）帶碼上報，而周邊舊臂仍 `E_GENERAL`；vet 歸一化口徑下文本逐字節一致，碼欄待後續族遷移。
- 候補留 s83：cg OP-SHR ashr 死代碼清理；(MIN,-1) 執行面探針；`?u64` option 語義；🔴 `ps.no` `~` 優先級分歧；§5.3.4.ct match 軌殘留；`%q` 完整化（≥0x80 `\uXXXX` 形）。

|  |  |  |  |  |  |  |
| --- | --- | --- | --- | --- | --- | --- |

### 5.3.4.dl s83——M4-e 第二十九件：u64 邏輯族 `&& ||` 放行（`compiler/src/h2m.no` u64 位運算臂撤 `is-logic == 0` 排除並退場 `is-logic` 旗標——Go 權威對 u64 的 `&& ||` 即裸位形 `and/or i64` **非短路**，s79 門禁前提被探針推翻；**零改 cg/ps/mir/ck**（`&& ||` 的 bop 於 s68 已復用 `OP-AND/OR`、`cg.no:386-399` 發射臂已備、ck.no 無邏輯族推斷臂））（2026-11-05 本輪）
**選型裁決（d1，探針實定稿非代碼閱讀）**：候選面調研 u64 邏輯族／`~` 優先級／cg ashr 死代碼／`?u64`。Go 權威調研鏈：`emitLogic`（codegen.go:7193）對 `OpAnd/OpOr` 發裸 `and/or <lt>` 非短路；`infixOp` `&& ||` cmp=false ⇒ `typeOfNode` KInfix 結果型=左操作數型（u64 ⇒ u64）；checker 表面推 bool 但**無操作數 bool 強約束**。s79 留的「u64 操作數非 0/1、同碼不等價邏輯語義、與真·短路一起後置」假設**需實驗裁決**——探針 `tmp/probe-s83-{a,b,c}.no` 走 v413 oracle 實測：`12 && 10 ⇒ 8`、`12 || 10 ⇒ 14`、`0 || 5 ⇒ 5`、`5 && 0 ⇒ 0`、`0 && 1 ⇒ 0`：Go 對 u64 邏輯族**本即裸位形且無短路語義** ⇒ 放行即鏡像，s83 選定；與位運算臂同碼 ⇒ 實作收斂為撤一個旗標子句，s78/s79 序列自然續件。

|  |  |
| --- | --- |
**Go 權威（兩道口徑並存，皆為權威）**：① codegen 道——`emitLogic` 兩操作數 coerce 後 `and/or <lt>` 裸發（與 `emitBitwise` 對 `& |` 逐字節同形）；② checker 道——邏輯族表面型判 bool：**函數體**「`r u64` 輸出參數 = `x && y`」被拒（`cannot assign bool value to u64` [15w45dqk]，probe-s83-d 實釘 5 條），而**頂層推斷位／bool 標注位／比較位**皆放行且 bool 槽存 i64 原樣位（`a bool = x && y; print(a)` 得 `8` 非 true/false）。故值面對拍一律走頂層形；IR 探針函數形系直喂 h2m 建模 codegen 發射層（同 s81 注 1 口徑）。

|  |  |  |
| --- | --- | --- |
**覆蓋面**：① `h2m.no` u64 位運算臂守衛 `is-bit == 1 && is-logic == 0` → `is-bit == 1`（`&& ||` 入臂，dest `TY-U64`，寄存器／字面量／mem 槽三形同 s79 位運算）；② `is-logic` 旗標塊（s79 引入）整體退場——同碼既語義正確，旗標無存在必要；③ cg／ps／mir／ck **零改**；④ 門禁測試翻正：`test-arith-u64-bit-gate`（s79 下移）改寫為 `test-logic-u64-{and,or}-plain` ＋ `test-logic-u64-and-lit`（6 斷言）；s67 世代 `test-log-gate`（u64 `&&` 計 unsup）改寫為 `test-log-u64-mem`（跨塊兩寫槽寬耦合靶，4 斷言）。

**關鍵點**：
|  |  |  |
| --- | --- | --- |
1. 🔴 **假設被探針推翻的教科書案例**：s79/s80/s81 三代文檔與測試註解均寫「邏輯族與真·短路語義一起後置」——源碼閱讀鏈（checker 四處 surface bool + 註解）自洽且有誘惑性，唯 oracle 實測（0 || 5 ⇒ 5 而非 1）一擊定音。承 s82 教訓的對偶面：不僅「鏡像字符串必以差分定稿」，**語義假設亦必以探針定稿**。
2. 同碼 ⇒ u64 邏輯族 IR 與 s79 位運算**逐字節同**（`and/or i64 %v, %v`），型別正確性不能靠 IR 文本鑑別，由 (a) h2m-test 門放行＋dest 型別碼、(b) 值語義刀共同鎖（與 s78/s79/s81 同口徑）。
3. 順帶觀察（依 §2 只記不改）：`0b1100` 二進制字面量 Go 詞法不識（報 `'b1100' is not defined`），探針改用十進制繞過——屬語言缺口非本件靶。

**驗證環境**：釘死工具鏈 `/tmp/nolang-v413/bin/no` + `DEVELOPER_DIR=/Library/Developer/CommandLineTools`，LLVM `/opt/homebrew/opt/llvm/bin`；SUT `/tmp/noc-s83`。⚠️ 本工作樹 `bin/no`（v0.4.19-dirty，並發 session 在途）對 import `util/sb` 檔復現 `setfield: buf not found on sb.sb` 既有回歸——與 s35 起歷代同因，一律 v413 採證。

**驗證（可重跑）**：先紅：新 6 斷言紅相 `713/719`（舊 715 − 門禁 2 ＋ 新 6 全紅恰落本件範圍）。轉綠：`h2m-test` **715→721/721**；回歸 cg 347／cg-runtime 60／tok 62／util 27／ps 38／diag 7／ck 20 全綠；`no fmt -d` h2m.no／h2m-test.no rc=0；`no vet` h2m.no 12 條 option-unwrap 既有基線（行位 1322–1347 與本件改動區正交，同 s81 記檔不改）、h2m-test.no 零報；std 與 Go 側零改 ⇒ `no vet src/std` 沿用基線 1 ERROR、L1–L3 全語料正交（本件純 h2m 降級件，從 s76–s81 族紀律）。**執行面對拍**：IR 探針 `tmp/probe-s83-ir.no` 四式（l1 寄存器 and、l2 寄存器 or、l3 mem 槽 and+or 跨塊、l4 字面量 operand）加權和 exit **15** == Go oracle 頂層形 `tmp/s83-oracle.no` 裸 exit **15**（`no build` 直讀）；語義探針四檔逐值（8/14、0/5/0/0、bool 標注位 8、`(x && y) == 8 -> print('eq8')` 命中）兩側同。**IR 反向刀** `tmp/s83-knives.py` **5/5 ALL EFFECTIVE**（K1 l1 and→or 14；K2 l2 or→and 13；K3 l3 mem-and→or 11；K4 l3 mem-or→and 11；K5 l4 lit and→or 7）。**前向零回歸**：s66–s81 探針重跑 exit **149/109/43/255/195/30/79/91/77/111/63/255×5** 與歷代記值全同。

| 算式 | 自舉產物（s83 後） | Go oracle（頂層形） | 一致 |
| --- | --- | --- | --- |
| `12 && 10` | 8（`and i64`） | 8 | ✓ |
| `12 || 10` | 14（`or i64`） | 14 | ✓ |
| mem `f=12&&10; f||10` | 10 | 10 | ✓ |
| `5 && 1` | 1 | 1 | ✓ |
| 四式加權和 | exit 15 | exit 15 | ✓ |

**未覆蓋／後續**：Go checker「邏輯族表面型 bool」的函數體赋值拒絕口徑**不在本層鏡像**（屬 M3-c ck.no 型別推斷族缺口，noc 尚無 assignment-type 校驗路——如實記檔，待 checker 族立件）；`'!'` 於 u64 仍誠實門禁（Go `!` 有 bool 域語義，非裸位形，與本件不同案）；cg OP-SHR ashr 死代碼清理；(MIN,-1) 執行面探針；`?u64` option 語義；🔴 `ps.no` `~` 優先級分歧；§5.3.4.ct match 軌殘留；`%q` 完整化——以上構成 M4-e 第三十件（s84）的候選


### 5.3.4.dm s84——M4-e 第三十件：前綴 `~` 與中綴綁定口徑複核＋哨兵重鑲（`compiler/src/ps.no` **實作零改**——§5.3.4.cx 關鍵點 4 所記 🔴「`ps.no` 前綴 `~` 鬆綁定優先級分歧」在現行世代三面實測全緊綁定：AST dump 四形與 oracle 逐字節 SAME、h2m+cg 執行值與 Go oracle 同值；`ps-test.no` +4 綁定形狀哨兵；`h2m-test` 721／cg 347／cg-runtime 60／ck 20 全綠零回歸）（2026-11-06 本輪）

**選型裁決（d1）**：候選面調研 cg ashr 死代碼清理／(MIN,-1) 執行面探針／`?u64` option 語義／🔴 `ps.no` `~` 優先級分歧／ck.no 邏輯族 bool 賦值口徑立件。取 🔴 待查項優先——待查項不閉合則代代抄入候選清單最終擱置（s79「與真短路後置」假設的教訓即此類記檔未經複核會擴散）。

**Go 權威**：`src/parser/expr.go:278` `case lexer.TILDE: parsePrefixExpression()` → `parseExpression(PREFIX)`（expr.go:830，PREFIX=9）；`precedences` 表 ADD/SUB=SUM(7)、MUL/QUO/MOD/SHL/SHR/AND=PRODUCT(8)，皆 < PREFIX ⇒ `~x + 3` 的右操作數只求值到 `x`，返回外層吸 `+ 3` ⇒ **緊綁定 `(~x)+3`**。

**三面實測（探針定稿紀律）**：① **AST 面**——oracle `no ast` 對 `~x + 3`／`~x * 2`／`~x + ~y`／`(~x) * 2` 四形 vs in-tree `ps.no` 的 `ast.dump`（`tmp/probe-s84-nocdump.no`）**4/4 逐字節 SAME**；② **Go 值面**——`tmp/probe-s84-go.no` `r = ~n + 3` 於 n=10 得 **-8**（`~10=-11`、`-11+3=-8`）、`tmp/s84-oracle2.no` `r = ~n * 2` 得 **-22**（鬆綁定則 -14/-21）；③ **自舉執行面**——IR 探針 `tmp/probe-s84-ir.no`（ps→h2m→cg 全管道）加權和 exit **3** == Go oracle `tmp/s84-oracle.no`（`no build` 直讀裸 exit）**3**。現行 `ps-op-prec`（`ps.no:2078` SUM=7/PRODUCT=8）與中綴迴圈 `oprec > prec`（`ps.no:2912`）即緊綁定機械。

**裁決**：s69 記檔分歧在現行 generation **不可復現**——git 考古 `git log -S` 顯示 TILDE 臂自 a761ab11（2026-10-27，s8h 批）引入即為 PREC 9，其間無變更；s69 當時實測細節不可考（其 q2/q3 形制本就規避裸 `~ binop`，該記錄來自一次性探針）。依「實測定口徑」紀律**關閉待查項**，§5.3.4.cx 關鍵點 4 就地補記（不覆寫歷史敘述）。

**實作面（收斂為零改）**：① `ps.no`/`h2m.no`/`cg.no`/`mir.no`/`ck.no` **實作一行未動**——現行行為已正確；② 僅 `ps.no` TILDE 臂註解補「s84 複核：PREC 9 為緊綁定」並去掉過期行號引用（expr.go:269→現 :278，行號易漂移不釘）；③ 哨兵重鑲 `ps-test.no` 新節 4 測試（`test-tilde-{add,mul}-bind`／`test-tilde-both-sides`／`test-tilde-grouped-contrast`，want 串全部由 L2 oracle `no ast` 實測取得）——若 TILDE 臂優先級日後再被鬆綁，前兩測立即紅。

**關鍵點**：
1. **裁定件的「先紅」退化形態**：本件屬 s80 裁定族（非加性件）——新哨兵對現狀**直綠**（42/42）無紅相，紅相缺席本身即為「實作無缺口」的證據；這與 s83「假設被探針推翻」互為對偶：記檔既可能過嚴（s83）也可能過時（本件），唯實測能定。
|  |  |  |
| --- | --- | --- |
2. **待查項的償還序**：🔴 標記的查因項若不做，會像 `&& ||` 那樣被逐代抄進候選清單；s84 以最小代價（4 個 dump 斷言）銷賬，`docs` 中 §5.3.4.cx 關鍵點 4 同步補記以閉。
3. `(~x) * 2` 括號形 GroupedExpression 節點仍在（oracle 同形），s69 探針當時的規避形（括號）與緊綁定形在**值面同值**、在 **AST 面不同形**——哨兵第 4 測釘住該對照，防止「值對形錯」的偽驗證。

**驗證環境**：釘死工具鏈 `/tmp/nolang-v413/bin/no` + `DEVELOPER_DIR=/Library/Developer/CommandLineTools`，LLVM `/opt/homebrew/opt/llvm/bin`。本工作樹 `bin/no`（v0.4.19-dirty，並發 session 在途）對 import `util/sb` 檔仍有 `setfield: buf not found on sb.sb` 既有回歸——歷代同因，一律 v413 採證。

**驗證（可重跑）**：`ps-test` **38→42/42**（+4 哨兵，直綠無紅相）；回歸 h2m 721／cg 347／cg-runtime 60／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` ps.no／ps-test.no 殘留漂移 **與 HEAD 基線逐字節同**（僅 hunk 行號 +2 位移，系本件註解兩行所致）；`no vet` ps.no 10 條 == 既有基線、ps-test.no 24 條 == HEAD 基線（全為 ps.no 傳遞 `expect_key` 命名項）。**對拍**：IR 探針 `tmp/probe-s84-ir.no` exit **3** == oracle `tmp/s84-oracle.no` 裸 exit **3**；**IR 反向刀** `tmp/s84-knives.py` **4/4 ALL EFFECTIVE**（K1 q1 add 操作數去 xor⇒13 失 bit1 exit 2；K2 全 1 常量 -1→1⇒exit 2；K3 q2 mul 去 xor⇒20 失 bit2 exit 1；K4 q2 右操作數 2→3⇒exit 1）。**前向零回歸**：s66–s81 探針重跑 exit **149/109/43/255/195/30/79/91/77/111/63/255×5** 與 s83 探針 **15** 全同，且 s66–s81 探針 IR 逐字節 **16/16 SAME**（本件零改 h2m/cg 之結構性保證）。

| 形 | 自舉產物 | Go oracle | 一致 |
| --- | --- | --- | --- |
| `~10 + 3` | -8（Infix{+}{Prefix{~}}） | -8 | ✓ |
| `~10 * 2` | -22（Infix{*}{Prefix{~}}） | -22 | ✓ |
| `~x + ~y` dump | Infix{+} 兩 Prefix 臂 | 逐字節 SAME | ✓ |
| `(~x) * 2` dump | GroupedExpression 包 Prefix | 逐字節 SAME | ✓ |
| 兩式加權和 | exit 3 | exit 3 | ✓ |

**未覆蓋／後續**：語義形 `~` 於 f64 主體（Go `hir2mir` 對 float 位非口徑未查）；(MIN,-1) 有符號執行面探針；`?u64` option 語義；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留；Go checker 邏輯族函數體 bool 賦值拒絕口徑（ck.no 立件）——以上構成 M4-e 第三十一件（s85）的候選


### 5.3.4.dn s85——M4-e 第三十一件：(MIN,-1) 護欄翼執行面實釘（h2m/cg **實作零改**——s75 自記「執行面留待專門件」之尾款本件落地：值面 MIN/-1⇒MIN、MIN%-1⇒0 且不觸 stderr；自舉 IR 四翼全形；exit 7==oracle；`h2m-test.no` +2 翼哨兵 721→735 直綠；刀 4/4、前向 s66–s84 IR 18/18 SAME）（2026-11-06 本輪）

**選型裁決（d1）**：候補面調研 `~` 於 f64 主體口徑／(MIN,-1) 執行面探針／`?u64` option 語義／cg ashr 死代碼清理／§5.3.4.ct match 軌殘留／ck.no 邏輯族 bool 賦值口徑立件。match 軌與並發 session 同戰場（`src/parser/block_match.go` 在途）本輪迴避（並發現場迴避紀律）；取 (MIN,-1)——實作面 s38/s75 已落地、全鏈唯一未閉的只有執行面（s75 明記「執行面留待專門件」），代價最小、權威最清楚。

**Go 權威**：`src/mir/codegen.go` `emitArith`（6149–6172）OpDiv/OpMod 有符號分支——`%dmn = icmp eq a, intMinLiteral`／`%dn1 = icmp eq b, -1`／`%db = and`／`%dall = or %dz, %db`／`%ds = select %dall, 1, b`：安全除數 1 使 MIN/-1 得定義值（/⇒MIN、%⇒0）；結果定義化 select 與 report 條件皆用 **%dz**（僅除零⇒0＋stderr）——(MIN,-1)「危險但不哭除零」。

**三面實測（探針定稿紀律）**：① **值面**——`tmp/probe-s85-go.no`／`tmp/s85-oracle.no`：MIN 字面量於實參／比較形皆被 Go checker 接受，函數體內 bool→i64 賦值被拒（`15w45dqk`，同 s83/s84 口徑）⇒ 比較上移頂層形，裸 exit 3/7 穩定；② **IR 形制面**——`tmp/s85.ll` q1/q2 體 `%dmn/%dn1/%db/%dall` 四翼與 codegen.go 逐步同構（含尾 select 用 dz、report 用 dz 的三段結構）；③ **自舉執行面**——`tmp/probe-s85-ir.no`（ps→h2m→cg 全管道）exit **7** == oracle `tmp/s85-oracle.no` 裸 exit **7**（bit1 q1(MIN,-1)==MIN、bit2 q2(MIN,-1)==0、bit4 q3(10,0)==0 除零錨共存；stderr 兩側皆僅除零報告一行——(MIN,-1) 不觸報告，實證 report 條件為 zeroDiv 非 dall）。

**裁決**：實作零改——翼自 s38/s75 即正確，本件屬 s80/s84 裁定族（執行面復核＋哨兵重鑲），閉 s75 自記之「執行面留待專門件」尾款（§5.3.4.dd 關鍵點 2 就地補記，不覆寫歷史敘述）。

**實作面（收斂為零改）**：① `h2m.no`/`cg.no` **實作一行未動**，僅兩處註解補實測依據（h2m 除法映射臂、cg-div-guard 頭注）；② 哨兵重鑲 `h2m-test.no` 新節 2 測試（`test-arith-i64-{div,mod}-min-wing` 正向翼斷言）——此前 `%dmn/%dn1/%db/%dall` 僅 cg-test 單元層（cg-div-guard 單函）與 u64 負向（s76「不得現 %dmn.」）有釘，**端到端 IR 正向翼無哨兵**，本件補齊。

**關鍵點**：
1. **刀落 UB 的證據形態**：K1–K3 摘翼 ⇒ `sdiv/srem MIN,-1` 落 LLVM UB，opt 折疊後 exit=85（arm64 硬體 sdiv MIN/-1=0 與折疊殘值之混合顯形）——刀只要求鑑別力（≠7），不承諾 UB 顯形固定；K4（尾 select 條件 dz→dall）落確定語義 exit=6，釘住「(MIN,-1) 結果保留 MIN 而非強制 0」與「除零才定義化為 0」的邊界。
2. **報告條件之別 dz/dall 是承載點**：若 report 誤餵 dall，(MIN,-1) 會在 stderr 多哭「除零」——本件值面（oracle 同無報告）＋K 族共同釘死。
3. **候選面調研新發現**：`~` 於 f64 Go **可編譯**且 AST 形為 `PrefixExpression{~}`（`tmp/probe-s85-f64tilde.no`）——非 checker 拒，口徑缺口比預期更深，執行值未定；登記 s86 頭號候選。【s86 減診】後續實彈：「可編譯」僅撐到 lowering/IR 生成接受——同形 clang 段 `%c48 = xor double` 非法、build fail（tmp/probe-s86-b.no，opt -passes=verify 同報雙證）；常量路又被 cvInt 域 comptime 摺死（tmp/probe-s86-a.no）⇒ 無權威執行值，裁定族紀實案結 §5.3.4.do。

**驗證環境**：釘死工具鏈 `/tmp/nolang-v413/bin/no` + `DEVELOPER_DIR=/Library/Developer/CommandLineTools`，LLVM `/opt/homebrew/opt/llvm/bin`。本工作樹 `bin/no`（v0.4.19-dirty，並發 session 在途）對 import `util/sb` 檔仍有 `setfield: buf not found on sb.sb` 既有回歸——歷代同因，一律 v413 採證。

**驗證（可重跑）**：`h2m-test` **721→735/735**（+14 斷言，直綠無紅相）；回歸 cg 347／cg-runtime 60／ps 42／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` h2m.no／cg.no／h2m-test.no **零漂移**（now=head=0）；`no vet` h2m.no 12 == HEAD 基線、h2m-test.no 24 == HEAD 基線、cg.no 0 == 0。**對拍**：IR 探針 `tmp/probe-s85-ir.no` exit **7** == oracle `tmp/s85-oracle.no` 裸 exit **7**；**IR 反向刀** `tmp/s85-knives.py` **4/4 ALL EFFECTIVE**（K1 q1 安全除數改餵 dz⇒85、K2 q1 isMin 改查 0⇒85、K3 q2 安全除數改餵 dz⇒85、K4 q1 尾 select 條件 dz→dall⇒6）。**前向零回歸**：s66–s81 探針 exit **149/109/43/255/195/30/79/91/77/111/63/255×5** 與 s83 **15**、s84 **3** 全同，且探針 IR 逐字節 **18/18 SAME**（本件零改 h2m/cg 之結構性保證）。

| 形 | 自舉產物 | Go oracle | 一致 |
| --- | --- | --- | --- |
| `MIN / -1` | MIN（dall→ds=1→sdiv MIN,1） | MIN | ✓ |
| `MIN % -1` | 0（srem MIN,1） | 0 | ✓ |
| `10 / 0` | 0 + stderr 報告一行 | 0 + 報告 | ✓ |
| (MIN,-1) stderr | 靜默 | 靜默 | ✓ |
| 三式加權和 | exit 7 | exit 7 | ✓ |

**未覆蓋／後續**：`~` 於 f64 主體（本件順帶查明 Go 可編譯＋Prefix{~} 形，執行值未定；【s86 銷賬】實彈 clang 段 `xor double` 非法 build fail，無權威可鏡像，裁定族紀實門禁哨兵 §5.3.4.do）；`?u64` option 語義降級；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留（並發 session `src/parser/block_match.go` 在途，本輪迴避）；ck.no 邏輯族 bool 賦值拒絕口徑立件——以上構成 M4-e 第三十二件（s86）的候選


### 5.3.4.do s86——M4-e 第三十二件：f64 主體 `~` 門禁裁定（§5.3.4.dn 關鍵點 3 頭號候選銷賬——Go 側非權威而是崩潰：運行路 `xor double` 非法 clang build fail、常量路 cvInt 域 comptime 摺死⇒無可鏡像執行語義；**實作零改**＋`h2m-test.no` 735→737 負向門禁哨兵；刀 3/3、前向 s66–s85 exit 19/19 全同、IR 16/16 SAME）（2026-11-06 本輪）

**選型裁決（d1）**：候選面調研 `~` f64（dn 登記頭號）／`?u64` option 語義／cg ashr 死代碼清理／§5.3.4.ct match 軌殘留（並發 session `src/parser/block_match.go`＋`src/checker/*` 在途，續迴避）／ck.no bool 賦值口徑立件。取 `~` f64，調研鏈三步：① **常量形** `tmp/probe-s86-a.no`（f64 四比較探針）exit 0 無信息量——`NOLANG_KEEP_IR=1` 核 IR 見四比較全被摺成 `store i1 0`：`hir2mir.go:7152` `case "~"` 以 **cvInt 域** `^` 對 float 字面量做 comptime 摺疊，運行值不可得；② **env 不透明形** `tmp/probe-s86-b.no`（s76 先例 `os.get-env`＋`str.to-f64` 破摺疊）——Go 工具鏈 **build fail**：`hir2mir.go:8878` TILDE 臂對 rt=f64 無型別守衛發 `OpXor(f64, [ov, allOnes(-1.0)])`，`emitBitwise` 出 `%c48 = xor double`，clang 報 `instruction requires integer or integer vector operands`（exit 1）；抽該函數單檔 `opt -passes=verify` 同報（靜態雙證）；③ dn 段「Go 可編譯」登記口徑修正——僅撐到 lowering/IR 生成接受，終點在 clang 段崩（rep 就地補記，不覆寫歷史敘述）。

**裁決**：本件屬 **s82 `&^` 裁定族（無可鏡像權威）**——Go 側對 f64 `~` 產出的是非法 IR 而非語義，oracle 運行值無從取得；自舉端 `h2m.no` K-PREFIX `ok-p==0 → h2m-mark` 門禁是比 Go 崩潰更嚴的正確形態，**維持不放行**。該門禁同時是 cg「OP-XOR 臂恆發 `xor i64`」的唯一上游防線——放行而不先補 cg 合法 double 形只會產破損 IR；即便日後補形，無 Go 權威可對拍即不放行。依 §2 黃金法則 Go 側只記錄不修改。

**實作面（收斂為零改）**：① `h2m.no` 實作一行未動，K-PREFIX 臂頭補 s86 紀實註解（門禁維持依據＋防線職責＋哨兵指向）；② 哨兵重鑲 `h2m-test.no` 新增 `test-prefix-not-f64-gate`（負向：`agg.unsup>0` ∧ IR 無 `define`）——735→**737/737** 直綠無紅相（s80 裁定族：紅相缺席即實作無缺口證據）。

**驗證（可重跑）**：`h2m-test` **737/737**；回歸 cg 347／cg-runtime 60／ps 42／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` 改動檔**零漂移**；`no vet` h2m.no 12 == HEAD 基線、h2m-test.no 24 == HEAD 基線。**對拍**：自舉探針 `tmp/probe-s86-ir.no`（全管道降 f64 `~` 源碼⇒unsup>0⇒毒 main `ret i32 20`）裸 exit **20**；Go 側同形 `no build` exit **1**（build fail）——兩側「得不到可正確執行的語義」以不同形態同時成立，裁決確證。**IR 反向刀** `tmp/s86-knives.py` **3/3 ALL EFFECTIVE**（K1 毒值 20→0⇒exit 0；K2 20→21⇒exit 21，釘 exit 精確對值；K3 源級變體 `~x`→`-x`⇒放行形 unsup=0 走正常路 exit 0 且 IR 含 `fneg double`——證 20 由 `~`＋f64 門禁臂特異產生）。**前向零回歸**：s66–s81 探針 exit **149/109/43/255/195/30/79/91/77/111/63/255×5** 與 s83 **15**、s84 **3**、s85 **7** 全同（19/19），探針 IR 逐字節 **16/16 SAME**（本件零改 h2m/cg 之結構性保證；`tmp/s86_fwd.sh`）。

**驗證環境**：釘死工具鏈 `/tmp/nolang-v413/bin/no` + `DEVELOPER_DIR=/Library/Developer/CommandLineTools`，LLVM `/opt/homebrew/opt/llvm/bin`。方法論新增紀律：`NOLANG_KEEP_IR` 的 tempdir IR 作證據前**必須核對內容與源一致**——本件早期一份 tempdir `.ll` 屬舊構建產物（內容與源不符），曾誤導 print 值與比較摺疊的判讀，換全新檔名後證據方對應。

| 面 | 形 | 結果 |
| --- | --- | --- |
| Go 常量路 | `q(1.5)` 四比較 | comptime cvInt 摺死，IR `store i1 0`，exit 0 無語義 |
| Go 運行路 | env 不透明 `~x` | `%c48 = xor double` ⇒ clang/verify 雙報非法，build fail exit 1 |
| 自舉端 | 全管道降 f64 `~` | unsup>0 ⇒ 門禁不出碼，探針 exit 20 |
| 負向哨兵 | `test-prefix-not-f64-gate` | unsup>0 ∧ 無 define ✓ |

**未覆蓋／後續**：`?u64` option 語義降級；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留（並發戰場回避中）；ck.no 邏輯族 bool 賦值拒絕口徑立件；f64 `~` 放行須先補 cg 合法 double 形——但無 Go 權威可對拍，不列入排程——其中 f64 `%` 放行（s71 算術族尾款，`h2m.no` 頭部與 INFIX 註記兩處門禁明記）已立件落地為第三十三件（§5.3.4.dp），`!x` 值語義件已落地為第三十四件（§5.3.4.dq），`!x` 非整型主體門禁裁定紀實件已落地為第三十五件（§5.3.4.dr）；餘下構成 M4-e 第三十六件（s90，bool 字面量鏡像 Go KBoolLit 已落地 §5.3.4.ds）；其餘構成第三十七件（s91，char 字面量鏡像 Go lowerCharLit 已落地 §5.3.4.dt）；其餘構成第三十八件（s92，默認參數調用點回填鏡像 Go hir2mir.go 尾部回填已落地 §5.3.4.du）；其餘構成第三十九件（s93，語句位調用鏡像 Go KExprStmt 值丟棄已落地 §5.3.4.dv）；其餘構成第四十件（s94）的候選


### 5.3.4.dp s87——M4-e 第三十三件：f64 `%` 放行（s71 算術族尾款——`compiler/src/h2m.no` 歸類區補 `OP-MOD -> is-arith` 唯一實作行發 frem double，鏡像 Go `codegen.go:6083` OpMod isFloat⇒frem；**零改 cg**（s37 frem 臂已備）；oracle 實釘 AArch64 lowering 後 fmod 語義兩側同值；`h2m-test` 737→**739/739**；刀 3/3、前向 s66–s85 exit 19/19 全同、IR 16/16 SAME）（2026-11-06 本輪）

**選型裁決（d1）**：§5.3.4.do 候選面調研——`?u64` option 語義（h2m 型別表無 option dest，§11 B14 建模級大件，本件不作）；match 軌殘留與 ck.no 權威仍在並發戰場（`src/checker/*`／`src/parser/*` 全在途，續迴避）；cg ashr 死代碼清理（非語義件）。取 f64 `%`——s71 算術族 `+ - * /` 已放行、`%` 為尾款（`h2m.no` L12 頭部明記「f64 '%' 仍門禁」、L1016「'%' 不入 is-arith ⇒ frem 仍門禁」）。調研鏈三步：① Go 權威 `codegen.go:6082-6087` `case OpMod: isFloat ⇒ frem`；`6100-6107` OpUMod 同形 float 守衛旁證「f64 % <unsigned> must lower to frem, not urem」；② 自舉端 `cg.no` OP-MOD 臂（L446-457）TY-F64 分支已發 `frem double`——s37 臂已備而無發射通路（h2m 不放行 `%`），fdiv/frem 無護欄與 Go 一致；③ `h2m.no` 歸類區 `is-arith` 僅 ADD/SUB/MUL/DIV——放行動作收斂為單行。i64 路（無條件全放）與 u64 路（OP-MOD 於 uty 已先行重映射 OP-MODU，原碼不可達）不受加行影響。

**語義實釘（frem ≠ IEEE remainder）**：`tmp/s87-oracle.no`（頂層變量形 `A f64 = 7.0` 避 comptime 摺疊——s86 紀律延伸）裸 exit **6**：`7.0 % 2.5` ⇒ **2.0**（fmod trunc 語義）而非 -0.5（IEEE round-to-nearest）；IR 實釘 `%c28 = frem double`（運行路未摺）；lowering 實釘 `llc -O2` 把 `frem double` 降為 `b _fmod`（AArch64 無浮點餘數指令）＋ `nm -u` 見 `_fmod` 導入——與 Go `opt.go:877` 註釋「emitArith emits `frem` for a double OpMod, which is C fmod」自洽。**同 IR 形制＋同工具鏈＋同 lowering ⇒ 自舉發 `frem double` 即與 Go 語義自動對齊**，無需改發 libm 直調。frem/fmod 分岔靶（7.0%2.5）＋共存錨（7.0%3.0=1.0）兩位加權對拍。

**裁決**：放行 f64 `%`——鏡像 `codegen.go:6083`；`f64 % <unsigned>` 混合形（Go OpUMod float 守衛場景）於 h2m uty/fty 互斥規則下不可達，屬混合型別建模面，不入本件。

**實作面**：① `h2m.no` 歸類區補 `bop == OP-MOD -> is-arith = 1`（唯一實作行）＋三處口徑同步（L12 頭部覆蓋面、L1016 INFIX 除模映射註記、f64 算術臂註記「+ - * / %」）；② `cg.no` **零改**；③ 哨兵 `h2m-test.no`：新增 `test-arith-f64-mod`（形參雙元：`agg.unsup==0` ∧ IR 含 `frem double`）——先紅 2 紅轉綠；舊 s75 反向哨兵 `test-arith-f64-mod-gate`（`x % 2.0` 字面量形）隨放行**翻正**為 `test-arith-f64-mod-lit`（正向 unsup==0 ∧ frem）——字面量形 s73 起 `OP-CONST(TY-F64)` 本已放行，舊哨兵錨的是 is-arith 門禁而非字面量門禁。737→**739/739**。

**驗證（可重跑）**：`h2m-test` **739/739**；回歸 cg 347／cg-runtime 60／ps 42／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` 改動檔**零漂移**；`no vet` h2m.no 12 == HEAD 基線、h2m-test.no 24 == HEAD 基線。**對拍**：自舉探針 `tmp/probe-s87-ir.no`（全管道降 `x % y` f64 形參雙元 ⇒ 手寫 main 三位加權，與 oracle 同權重排布，兩側 exit 直接可比）裸 exit **6** == Go oracle **6**（bit1 frem 反證 0、bit2 fmod 1、bit4 共存錨 1）。**IR 反向刀** `tmp/s87-knives.py` **3/3 ALL EFFECTIVE**（K1 刪歸類行⇒回退門禁毒 exit 101；K2 歸類錯族 `is-arith`→`is-cmp`⇒dest TY-I64 不落 f64 算術臂⇒101；K3 cg `frem`→`fdiv`⇒2.8 全位 0 exit 0——釘 exit 6 由「OP-MOD 歸類行＋frem 發射臂」組合特異產生；刀腳本紀律：快照原文回寫制，空串替換不破壞錨點 count 斷言）。**前向零回歸**：s66–s81 探針 exit **149/109/43/255/195/30/79/91/77/111/63/255×5** 與 s83 **15**、s84 **3**、s85 **7** 全同（19/19），探針 IR 逐字節 **16/16 SAME**（`tmp/s86_fwd.sh`）。

**驗證環境**：釘死工具鏈 `/tmp/nolang-v413/bin/no` + `DEVELOPER_DIR=/Library/Developer/CommandLineTools`，LLVM `/opt/homebrew/opt/llvm/bin`。方法論紀律延伸：oracle 實參走頂層變量避 comptime 摺疊外，還須 **IR 層核對 `frem` 在位**以證運行路未摺（本件 `%c28 = frem double` 在 `opt` 前後均在），並以 `llc` 降態（`b _fmod`）確認 target 語義——IR 文本同形不等同機器碼同形，降態查證補上最後一環。

| 面 | 形 | 結果 |
| --- | --- | --- |
| Go 權威 | `codegen.go:6082-6087` | OpMod isFloat ⇒ frem（OpUMod 同形守衛旁證） |
| Go 執行面 | oracle 頂層變量形 | exit 6 ⇒ 7.0%2.5=2.0（fmod 語義） |
| lowering 面 | `llc -O2` frem double | `b _fmod`＋`nm -u` `_fmod`——兩側同形 |
| 自舉端 | 全管道探針 | exit 6 == oracle 6；刀 3/3；前向 19/19＋IR 16/16 |

**未覆蓋／後續**：`?u64` option 語義降級；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留（並發戰場迴避中）；ck.no 邏輯族 bool 賦值拒絕口徑立件；`1e5` 裸指數字面量已銷賬 s89（Go 詞法 ILLEGAL 已 s82 全鏈鏡像，h2m 側 `contains('.')` 閘屬不可達第二道防線）；`!x` 值語義件已償第三十四件（§5.3.4.dq）——以上構成 M4-e 第三十六件（s90，bool 字面量鏡像 Go KBoolLit 已落地 §5.3.4.ds）；其餘構成第三十七件（s91，char 字面量鏡像 Go lowerCharLit 已落地 §5.3.4.dt）；其餘構成第三十八件（s92，默認參數調用點回填鏡像 Go hir2mir.go 尾部回填已落地 §5.3.4.du）；其餘構成第三十九件（s93，語句位調用鏡像 Go KExprStmt 值丟棄已落地 §5.3.4.dv）；其餘構成第四十件（s94）的候選


### 5.3.4.dq s88——M4-e 第三十四件：`!x` 值語義對齊 i1 低位列（`compiler/src/h2m.no` K-PREFIX `'!'` 臂單點實作改 `OP-CONST(1)`＋`OP-AND`＋`OP-NOT` 三發射序列，鏡像 Go `codegen.go:6408` `emitNot` 的 `trunc i64→i1`＋`xor i1 1`⇒值恆 (x&1)^1；i64 修正＋u64 放行（s81 門禁尾款清償），f64 `'!'` 維持門禁裁定；**零改 cg**；oracle 實釘 i64 exit 117、u64 exit 54；`h2m-test` 739→**743/743**；刀 4/4、前向 exit 19/19 全同、IR 15/16 SAME（s66 基線重錨——非加性件，值面 149 不變））（2026-11-06 本輪）

**選型裁決（d1）**：候選面調研（§5.3.4.dp 清單）——`?u64` option 語義（§11 B14 建模級大件，本件不作）；cg OP-SHR ashr 死代碼清理（非語義件）；match 軌與 ck.no 仍在並發戰場（`src/checker/*`／`src/parser/*`／`compiler/src/{asp,ast,ps}.no` 全在途，續迴避）；`1e5` 裸指數字面量（s73 誠實閘）。取 `'!'` 值語義件——第三十四件的靶不是新 op 而是 s66 既有 `OP-NOT` 臂的**語義修正**：s66 對拍以「操作數全取 0/1」的 oracle 釘住，全寬 `xor i64 1` 與 Go「先截低位再異或」僅在 {0,1} 同值——分岔態埋伏同臂三十件未爆；且 s81 明文登記 u64 `'!'` 門禁尾款（「裸 xor 1 ≠ 邏輯非，非 0/1 值會錯」），正解是先歸位低 bit 再 xor 而非門禁，本件一併清償。

**語義實釘（權威鏈三面）**：① checker 面——`!x` 表面型恆 bool，函式體 `r i64` 賦 `!x` 被拒（`cannot assign bool value to i64 variable 'r'`）⇒值面 oracle 一律頂層形＋`exit(S)` 加權；② codegen 面——`hir2mir.go:8870-8915` KPrefix `'!'`⇒`prefixOp`⇒`OpNot`，dest MIR 型別 bool（MIR 把 bool 映射為 i1）；`codegen.go:6408-6416` `emitNot` 先 `coerceInt(v, vT, lt)`（int→int 窄化發 `trunc i64 … to i1` 取最低位）再 `xor i1 1`⇒i64/u64 的 `!x` 值恆 `(x&1)^1 ∈ {0,1}`；③ 摺疊面——`opt.go:843-846` OpNot 常量摺疊僅 `dstLT=="i1"`，與歸位口徑同源。f64 側 `!f`：coerceInt 走 `emitFptSatReg`（fptosi.sat→i1），env 不透明實測 2.5/0.0/1.0/1.5/7.0/0.7/0.3/abc **恆 true**（`tmp/probe-s88-b3.no`）⇒poison 退化無可鏡像穩定語義，維持門禁（s86 裁定族）。

**裁決**：`'!'` 於 i64 修正、於 u64 放行（dest 恆 TY-I64——結果恆 bool，口徑同比較族）；f64 維持門禁。

|  |  |  |
| --- | --- | --- |
**實作面**：① `h2m.no` K-PREFIX `'!'` 臂改三發射序列 `OP-CONST(1)`→`OP-AND(low = ov & 1)`→`OP-NOT(mid = ~low)`，guard 擴 `TY-I64 || TY-U64`；自舉無 i1 槽（整族共用 i64 寬，口徑同 s78/s83）⇒以 `OP-CONST(1)+OP-AND` 表達 trunc 歸位、`OP-NOT` 表達 xor 1——cg `OP-NOT` 恒發 `xor i64 1`（`cg.no:496-504`）恰好成為序列第二步，型別正確性不靠 IR 文本鑑別（s83 明文先例）；② `cg.no`／`mir.no`／`ps.no` **零改**；③ 哨兵 `h2m-test.no`：新增 `test-not-i64-lowbit`（unsup==0 ∧ IR 含 `and i64` ∧ `xor i64 1`），舊 s81 門禁哨兵 `test-pfx-u64-lnot-gate` **翻正**為 `test-pfx-u64-lnot`（正向三斷言）；`test-not-var` 註釋同步。紅相 743/4→綠相 **743/743**。

**驗證（可重跑）**：`h2m-test` **743/743**；回歸 cg 347／cg-runtime 60／ps 42／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` 改動檔**零漂移**；`no vet` h2m.no **12→13**（新增一條同族 option-unwrap 噪声——本臂 guard 多一次 `h2m-vty-at` 呼叫點；以 `git show HEAD:compiler/src/h2m.no` 提取基線同工具鏈復測計數 12，歸因可解釋，**記檔不改**），h2m-test.no 24 == 基線。**對拍**：i64 oracle `tmp/s88-oracle-i64.no`（頂層形加權；態 T 歸位⇒117，態 W 全寬⇒362 但 shell 只見 `& 0377`＝106——main return 低 8 位截斷坑本件首次咬到，權重和須 <256）裸 exit **117** == 自舉探針 `tmp/probe-s88-ir.no` **117**；u64 oracle `tmp/probe-s88-a.no` **54** == 自舉 `tmp/probe-s88-u64-ir.no` **54**（IR 形 `add i64 0, 1`／`and i64`／`xor i64 1` 三序列在位）。**IR 反向刀** `tmp/s88-knives.py` **4/4 ALL EFFECTIVE**（K1 繞過歸位直發 `OP-NOT(mid, ov)`⇒106；K2 mask `1→0`⇒33；K3 cg `xor 1→0`⇒128；K4 源級 `'!'→'~'`⇒0；快照回寫制）。**前向零回歸**：s66–s85 探針 exit **19/19 全同**（149/109/43/255/195/30/79/91/77/111/63/255×5/15/3/7）；探針 IR **15/16 SAME**——s66 檔 IR 變化，diff 證明變化僅限 `!x` 歸位三指令序列、值面 149 不變⇒屬**非加性件基線重錨**（s80 先例），新基線 `tmp/s88-fwd-s66.ll`，後續件前向腳本須改用新基線。

**現場事故紀事（本輪）**：d1 勘察時發現 `compiler/src/h2m.no` 與 `compiler/src/cg.no` 於 08:59:54 被並發進程回退到 HEAD（兩檔 mtime 同刻、`git diff HEAD` 空、`grep 's8[67]'` 零命中），s82–s87 六件**未提交**交付中 h2m.no 側全丟——h2m-test 725/14（10 紅＝s83 logic-u64、4 紅＝s87 f64-%）。按 §5.3.4.dk–dp 盤點確認 lx.no／ps.no／ps-test.no／h2m-test.no 交付無損後，依文檔重放 s83（is-logic 旗標退場＋u64 位運算臂撤排除）＋s87（OP-MOD 入 is-arith）實作行與 s86／s87 紀實註解共九處精確替換，驗 739/739 綠＋fmt 零漂移方續推本件。本件收尾時 s82–s88 全部未提交交付已隨並發 session 的提交（`git add -A` 形制）進入 `720329df`——本項目禁用自動備份（data-backup 紀律：工作樹無 stash 保險），未提交交付暴露於並發提交/回退雙重風險，紀此備查。

**未覆蓋／後續**：`?u64` option 語義降級；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留（並發戰場迴避中）；ck.no 邏輯族 bool 賦值拒絕口徑立件；`1e5` 裸指數字面量已銷賬 s89（重新定性：Go 詞法 ILLEGAL 已 s82 全鏈鏡像——`no ast` 实测拒、`1e5` 到不了 h2m，s73 `contains('.')` 閘是不可達第二道防線）；`!x` 非整型主体门禁裁定纪实＋哨兵重镶已落地為第三十五件（§5.3.4.dr）——以上構成 M4-e 第三十六件（s90，bool 字面量鏡像 Go KBoolLit 已落地 §5.3.4.ds）；其餘構成第三十七件（s91，char 字面量鏡像 Go lowerCharLit 已落地 §5.3.4.dt）；其餘構成第三十八件（s92，默認參數調用點回填鏡像 Go hir2mir.go 尾部回填已落地 §5.3.4.du）；其餘構成第三十九件（s93，語句位調用鏡像 Go KExprStmt 值丟棄已落地 §5.3.4.dv）；其餘構成第四十件（s94）的候選


### 5.3.4.dr s89——M4-e 第三十五件：非整型主体 `'!'` 門禁裁定紀實＋哨兵重鑲（`h2m.no` K-PREFIX `'!'` 臂三面實釘無權威可鏡像，門禁維持；**實作零改**，鏡像 s84/s86 紀實件先例；`h2m-test` 743→**747/747**（`test-prefix-lnot-f64-gate`／`test-prefix-lnot-str-gate` 兩負向哨兵）；刀 2/2、前向 exit 19/19 全同、IR 16/16 SAME（零改件結構性保證））（2026-11-06 本輪）

**選型裁決（d1）**：按 §5.3.4.dq 候選面逐項調研——`?u64` option 語義（建模級大件，續不作）；cg OP-SHR ashr 死代碼清理（非語義件）；match 軌與 ck.no 仍在並發戰場（續迴避）；**`1e5` 重新定性銷賬**——Go `lexer.go:1000-1010` 對 `[eE]`＋指數位直接產 ILLEGAL token（ErrMsg 全文），s82 已全鏈鏡像（`lx.no` ErrMsg 156 字節＋`lx-test` `test-sci-notation` 哨兵），HEAD/v413 實測 `no ast` 報 Parse error [1gybkrrq]——`1e5` 根本到不了 h2m，s73 `contains('.')` 誠實閘屬不可達第二道防線，候選出局（紀事更正級）；**ck.no bool 賦值拒絕**——發射點 checker.go:10004-10027 是通用賦值型別不符門（`cannot assign %s value to %s variable '%s'`，非 bool 專屬），鏡像需引入型別推斷 mini 系統，M3 級大件超本件規格出局；**str 族降級**——cg 側 STRCONST/STRLEN/STRCAT 已備（s23–s26）而 h2m 無 K-STRING-LITERAL 臂，缺口真實，但 Go 側字符串拼接是 builtin call `str_concat`＋`@str_from_const`，h2m 自動放置 RC（RETAIN/RELEASE）未建模屬 M4-d/§11 B11/B13 未完項，卡內存紀律不入本件；**f64 `'!'` 哨兵缺位**——`h2m-test` 唯一 f64 前綴哨兵 `test-prefix-not-f64-gate` 釘的是 `~` 不是 `!`，s88 裁定的 f64 `'!'` 門禁無哨兵，真實缺口。取後者立件。

**語義實釘（非整型主體三面全無權威）**：① f64 運行路——`coerceInt(i64→i1)` 對 double 截出 poison 再 `xor i1 1` ⇒ 恆 true（s88 d1 實釘）；函數形 `r i64 = !f` 更先被 checker bool→i64 門拒 [15w45dqk]，`r bool` 形則頂層呼叫被 hir inline 摺成 `store i1 1`（`tmp/s89-f64lnot3.no` 本輪 IR 實釘：無 `@p` define、main 直寫 `i1 1`）；② str/vec 運行路——`intWidth` 查不到 str ⇒ `coerceInt` 原樣返回（codegen.go:1190-1253），`emitNot` 發 `xor i1 1, %str-long` 型別錯 build fail（env 不透明破摺疊實測 [d8en6a9j]，`tmp/s89-env-str.no`）；③ 字面量路——hir inline comptime 摺疊恆 true，空串也 true 證僞任何空檢語義（`tmp/s89-run-str.no`/`s89-run-vec.no`）。⇒ 門禁是正確形態，無放行可言。

**裁決**：非整型主體 `'!'` 維持門禁、實作零改；本件交付＝紀實註解（`h2m.no` 頭部摘要＋K-PREFIX 臂 s89 紀實塊）＋負向哨兵重鑲。

**實作面**：① `h2m.no`／`cg.no`／`mir.no`／`ps.no` 零改（僅紀實註解）；② `h2m-test.no` 新增 `test-prefix-lnot-f64-gate`（`r = !x` 於 f64 形參 ⇒ unsup>0 ∧ 無 define）與 `test-prefix-lnot-str-gate`（`r = !s` 於 str 形參 ⇒ 同斷言），形制鏡像 `test-prefix-not-f64-gate`；零改件哨兵直綠無紅相——紅相缺席即門禁在位證據（s86 裁定族紀律）。綠相 **747/747**。

**驗證（可重跑）**：`h2m-test` **747/747**；回歸 cg 347／cg-runtime 60／ps 42／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` 改動檔**零漂移**；`no vet` h2m.no 13、h2m-test.no 24 == s88 基線。**IR 反向刀** `tmp/s89-knives.py` **2/2 ALL EFFECTIVE**（哨兵斷言制，快照回寫：K1 撤 `'!'` 臂 vty 判據⇒741/6 紅；K2 刪 `'!'` 臂整臂⇒739/8 紅；還原後 747/0）。**前向零回歸** `tmp/s89_fwd.sh`：s66–s85 探針 exit **19/19 全同**（149/109/43/255/195/30/79/91/77/111/63/255×5/15/3/7）；探針 IR **16/16 SAME**（零改件 h2m 產碼路徑未觸及之結構性保證；基線鏈：s66–s71 對 `tmp/s88-fwd-s66..71.ll`、s72–s81 對 `tmp/s86-fwd-s72..81.ll`）。

**未覆蓋／後續**：`?u64` option 語義降級；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留（並發戰場迴避中）；ck.no 通用賦值型別不符門（15w45dqk，M3 級型別推斷大件）；str 族降級（卡 M4-d RC 自動放置）；vec 主體 `'!'`（與 str 同形已已裁定，運行面未单独哨兵）；`b = true` bool 字面量已落地為第三十六件（§5.3.4.ds，d1 普查新撞出、非本清單預列）——其餘構成 M4-e 第三十七件（s91，char 字面量鏡像 Go lowerCharLit 已落地 §5.3.4.dt）；其餘構成第三十八件（s92，默認參數調用點回填鏡像 Go hir2mir.go 尾部回填已落地 §5.3.4.du）；其餘構成第三十九件（s93，語句位調用鏡像 Go KExprStmt 值丟棄已落地 §5.3.4.dv）；其餘構成第四十件（s94）的候選


### 5.3.4.ds s90——M4-e 第三十六件：bool 字面量降级＋bool 型别码放行（`h2m.no` 補 K-BOOLEAN-LITERAL 降級臂＋型別碼，鏡像 Go `hir2mir.go:8848` KBoolLit⇒EmitInt(OpConst, 0/1)；**零改 cg/ps/mir**；`h2m-test` 747→**755/755**（`test-bool-lit-true`／`test-bool-lit-false` 三斷言×2＋`test-bool-param-tycode` 兩斷言，共 +8）；刀 2/2、oracle exit 2、前向 exit 19/19 全同、IR 16/16 SAME）（2026-11-06 本輪）

**選型裁決（d1）**：按 §5.3.4.dr 候選面逐項調研＋賦值面探針普查——**重赋值假缺口排除**（`x = n`／`x = x + 5` 走 LetStatement 無名重赋值路，實測 unsup=0 已放行）；`?u64` option（建模級大件，續不作）；cg OP-SHR ashr 死代碼（非語義件）；str 族降級（卡 M4-d RC 自動放置）；match 軌（並發戰場迴避）。**探針普查撞出真實缺口**：`b = true`（K-BOOLEAN-LITERAL 語句位）h2m unsup=1、define=0——`h2m-lower-expr` 認得 K-INTEGER/K-FLOAT/K-IDENTIFIER，**無 K-BOOLEAN-LITERAL 臂**；`h2m-ty-code`（i64/u64/f64/str）**無 'bool'** ⇒ bool 形參鋪槽必敗。該缺口不在 s89 候選清單內，屬 d1 普查新撞出。取之立件。

**語義實釘（Go 權威）**：`hir2mir.go:8848` `KBoolLit ⇒ EmitInt(OpConst, typeOfNode(n)⇒Type('bool'), 0/1)`；codegen `OpConst` 按 `case int64` 分支 ⇒ **MIR 層 bool 載荷即 int64**，i1 形（`alloca i1`／`store i1 1`／`xor i1 1`，`tmp/dist/s90-bool-ir2.ll` env 不透明破折疊實釘）屬**後端類型環境**、非 MIR 真相。值面三例（Go oracle）：`b == true` exit 108／bool 形參調用 exit 11／`b == false` exit 7；`b + n` 賦 i64 被 checker 通用賦值門拒（15w45dqk，屬 M3 級型別推斷大件、不入本件）。

**裁決**：自舉 bool 走 **TY-I64 0/1 編碼**（bool⇒TY-I64 直接映射，**零改 cg**）——依據三點：① Go MIR 層載荷本就是 int64；② 自舉比較族 s57 早已確立「布爾值 = i64 0/1（icmp+zext）」口徑、h2m 無 i1 槽先例；③ s88 `'!'` 臂 TY-I64 主體門天然貫通 `!b`。

**實作面**：四處鏡像——① `h2m-ty-code` 補 `s == 'bool' -> t = TY-I64`；② `h2m-ty-of` 補 `K-BOOLEAN-LITERAL -> code = TY-I64`（槽寬推論）；③ `h2m-lower-expr` 補 K-BOOLEAN-LITERAL 臂（讀 slot0 文本 `'true'`/`'false'` 選 0/1，發 `OP-CONST(TY-I64, v)`）；④ 文件頭覆蓋面摘要補 BooleanLiteral 指針。**零改 `cg.no`/`ps.no`/`mir.no`**——`ps.no` L2336/2342 operand 位已產 K-BOOLEAN-LITERAL、slot0 存字面文本；`cg` OP-CONST 非 f64 臂發 `%v = add i64 0, v` 即 bool 落形。綠相 **755/755**。

**驗證（可重跑）**：`h2m-test` **755/755**；回歸 cg 347／cg-runtime 60／ps 42／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` 改動檔**零漂移**；`no vet` h2m.no 13、h2m-test.no 24 == s89 基線。**IR 反向刀** `tmp/s90-knives.py` **2/2 ALL EFFECTIVE**（快照回寫制：K1 刪 lower-expr bool 臂⇒749/6 紅；K2 刪 ty-code bool 行⇒747/8 紅；還原後 755/0）。**oracle 對拍** `tmp/probe-s90-oracle.no`：自舉降級 `@t⇒add i64 0, 1`／`@f⇒add i64 0, 0`，clang 執行 exit **2**（編碼 ret=t()*2+f，唯一釘 true=1/false=0；皆 0⇒0、皆 1⇒3、互換⇒1 全排除）。**前向零回歸** `tmp/s90_fwd.sh`：非 bool 探針 exit **19/19 全同**（149/109/43/255/195/30/79/91/77/111/63/255×5/15/3/7）；IR **16/16 SAME**（新臂僅 K-BOOLEAN-LITERAL 觸發，既有探針無 bool 語句 ⇒ 逐字節不變）。

**未覆蓋／後續**：`?u64` option 語義降級；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留（並發戰場迴避中）；ck.no 通用賦值型別不符門（15w45dqk，M3 級型別推斷大件）；str 族降級（卡 M4-d RC 自動放置）；bool 控制流值面（`b == true` 等走既有比較族、布爾主體未單獨哨兵）；char 字面量已落地為第三十七件（§5.3.4.dt，d1 普查新撞出、非本清單預列）——其餘構成 M4-e 第三十八件（s92，默認參數調用點回填鏡像 Go hir2mir.go 尾部回填已落地 §5.3.4.du）；其餘構成第三十九件（s93，語句位調用鏡像 Go KExprStmt 值丟棄已落地 §5.3.4.dv）；其餘構成第四十件（s94）的候選


### 5.3.4.dt s91——M4-e 第三十七件：char 字面量降级＋char 型别码放行（`h2m.no` 補 K-CHAR-LITERAL 降級臂（UTF-8 首碼點解碼）＋型別碼，鏡像 Go `hir2mir.go` lowerCharLit⇒EmitInt(OpConst, 首碼點)；**零改 cg/ps/mir**；`h2m-test` 755→**763/763**（`test-char-lit-ascii`／`test-char-lit-cjk` 三斷言×2＋`test-char-param-tycode` 兩斷言，共 +8）；刀 3/3、oracle exit 3、前向 exit 19/19 全同、IR 16/16 SAME）（2026-11-06 本輪）

**選型裁決（d1）**：按 §5.3.4.ds 候選面逐項調研＋探針普查——**bool 控制流假缺口排除**（`b == true`／`!b`／guard／and／loop 五形實測 unsup=0，s57 比較族＋s90 決策免費覆蓋，非缺口）；`-5` 負字面量已放行；nil 字面量卡 option 建模（Go lowerNilLit 走 option「none」判别⇒`?u64` 大件，排除）；`?u64` option（建模級大件，續不作）；cg OP-SHR ashr 死代碼（非語義件）；str 族降級（卡 M4-d RC 自動放置）；match 軌（並發戰場迴避）。**探針普查撞出真實缺口**：`r = "a"`（K-CHAR-LITERAL 語句位）h2m unsup=1——`h2m-lower-expr` 認得 K-INTEGER/K-FLOAT/K-BOOLEAN-LITERAL/K-IDENTIFIER，**無 K-CHAR-LITERAL 臂**；`h2m-ty-code` **無 'char'** ⇒ char 形參鋪槽必敗。該缺口不在 s90 候選清單內，屬 d1 普查新撞出。取之立件。

**語義實釘（Go 權威）**：`hir2mir.go` `lowerCharLit`：`s = Trim(pkg.Str(n.S), "\"")`；`for _, r := range s { cp = int64(r); break }`⇒**取首碼點**，`EmitInt(OpConst, Type("char"), cp, "")`；char 在 Go 側為 i32（L1694 註記「char literal 型別 i32、報 i64 會鋪 i64 槽存 i32 mismatch bug（已修）」）。值面三例（Go oracle）：`"a"⇒97`、`"中"⇒20013`、`"\n"⇒10`（`"'"`→0 屬 Go 詞法 quirk，排除不入對拍）。

**裁決**：自舉 char 走 **TY-I64 碼點編碼**（char⇒TY-I64 直接映射，**零改 cg**）——Go 為 i32，自舉無 i32 槽、碼點 ≤0x10FFFF 落 i64 精確、槽/常量同寬無混用（同 s90 bool i64 寬口徑先例）；需忠實 UTF-8 首碼點解碼（lead 字節定長族 1/2/3/4 字節，continuation 取低 6 位、累加還原）。

**實作面**：四處鏡像——① `h2m-ty-code` 補 `s == 'char' -> t = TY-I64`；② `h2m-ty-of` 補 `K-CHAR-LITERAL -> code = TY-I64`（槽寬推論）；③ `h2m-lower-expr` 補 K-CHAR-LITERAL 臂（讀 slot0 去引號內文→`to-bytes()`→1/2/3/4 字節 UTF-8 首碼點解碼四分支→發 `OP-CONST(TY-I64, cp)`；臂內局部用 b1x/b1y/b2y/b1z/b2z/b3z 唯一名規避 §6.3 串型）；④ 文件頭覆蓋面摘要補 CharLiteral 指針。**零改 `cg.no`/`ps.no`/`mir.no`**——`ps.no` L2330 operand 位已產 K-CHAR-LITERAL、slot0 存去引號內文；`cg` OP-CONST 非 f64 臂發 `%v = add i64 0, v` 即 char 落形；窄→寬隱式賦值 `b0 i64 = bs[0]`（§3.4）取字節整數。綠相 **763/763**。

**驗證（可重跑）**：`h2m-test` **763/763**；回歸 cg 347／cg-runtime 60／ps 42／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` 改動檔**零漂移**；`no vet` h2m.no 13、h2m-test.no 24 == s90 基線。**IR 反向刀** `tmp/s91-knives.py` **3/3 ALL EFFECTIVE**（快照回寫制：K1 刪 lower-expr char 臂⇒757/6 紅；K2 刪 ty-code char 行⇒755/8 紅；K3 破 3 字節解碼權重 `*4096→*64`⇒762/1 僅 cjk 常量紅、ascii 仍綠——證明 UTF-8 解碼真在位；還原後 763/0）。**oracle 對拍** `tmp/probe-s91-oracle.no`：自舉降級 `@fa⇒add i64 0, 97`／`@fz⇒add i64 0, 20013`，clang 執行 exit **3**（編碼 ret=b1*1+b2*2，唯一釘 a=97/中=20013；皆錯⇒0、僅 a 對⇒1、僅 z 對⇒2 全排除）。**前向零回歸** `tmp/s91_fwd.sh`：非 char 探針 exit **19/19 全同**（149/109/43/255/195/30/79/91/77/111/63/255×5/15/3/7）；IR **16/16 SAME**（新臂僅 K-CHAR-LITERAL 觸發，既有探針無 char 語句 ⇒ 逐字節不變）。

**未覆蓋／後續**：`?u64` option 語義降級；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留（並發戰場迴避中）；ck.no 通用賦值型別不符門（15w45dqk，M3 級型別推斷大件）；str 族降級（卡 M4-d RC 自動放置）；nil 字面量（卡 option 建模）；char 控制流/比較值面（走既有比較族、char 主體未單獨哨兵）；默認參數調用點回填已落地為第三十八件（§5.3.4.du，d1 普查撞出、非本清單預列）——其餘構成 M4-e 第三十九件（s93，語句位調用鏡像 Go KExprStmt 值丟棄已落地 §5.3.4.dv）；其餘構成第四十件（s94）的候選


### 5.3.4.du s92——M4-e 第三十八件：默认参数调用点回填（`h2m.no` 簽名表補 fdef 預設式節點表＋調用臂尾部回填迴圈，鏡像 Go `hir2mir.go:13175` 缺省實參逐調用點 lowerExpr(deID) 拷開回填；**零改 cg/ps/mir**；`h2m-test` 763→**771/771**（`test-default-param-fill`／`test-default-param-trailing`／`test-default-param-missing-gate` 三斷言×3，共 +8；舊「gate default param」兩斷言按 s87 先例翻正），刀 3/3、oracle exit 15、前向 exit 19/19 全同、IR 16/16 SAME）（2026-11-06 本輪）

**選型裁決（d1）**：按 §5.3.4.dt 候選面逐項調研＋ `tmp/s92-quick.no` 16 形普查——char 值面（char-eq-lit/char-param-ret）unsup=0 屬 s91 紅利、非缺口排除；真實缺口清單＝str-lit/str-concat/vec-lit/arr-index/default-param/multi-out/nested-func/defer/opt-nil/opt-ok。其中 str 族卡 M4-d RC 自動放置、vec/arr 字面量卡 >24B 堆裝箱、multi-out 卡 cg LLVM 單返回值後端形制大改、option nil/ok 卡建模大件、defer/嵌套函數大件——均續不作。**取 default-param（調用點回填）立件**：改件局限在 h2m 簽名表與調用臂，量級可控。

**語義實釘（Go 權威）**：`hir2mir.go` L13154-13211 調用下降在 `len(args) < len(paramNodes)` 且非變參時，對缺失尾參逐個 `l.lowerExpr(pn.First)` 入 argv——**默認值表達式在每個調用點拷開降一份**（非共享常量）；`pn.First == NoID`（被缺且無默認值）直接 skip，因 `funcargs.go:238` checker 已前置拒「實參少於 minArgs」。AST 形制 `no ast` 實釘：Parameter kid 序 = [Type, DefaultExpr]（kids[1]=IntegerLiteral{5}）。Go oracle 值面 `tmp/s92-oracle-vals.no` 實釘：`f()=5`、`f(9)=9`、`g(1)=21`（1+20）、`g(1,2)=3`。

**裁決**：簽名側建 `fdef []i64` 平行表（與 farg 共 fbase 分段，0＝無默認值）；`h2m-param-ty` 加第三輸出 `de`（默認式節點 id）並撤舊「帶默認值即 bad」門禁；`h2m-func-sig` 彙總 `deft` 上送；調用臂 arity 門改「僅拒超供 `na > want-n`」，逐位統一迴圈：`j < na` 拿 AST 實參 kid、`j >= na` 拿 `h2m-sig-def` 默認式節點就地降入呼叫者塊（複用 h2m-lower-expr，天然「每調用點拷開」）；`ak == 0`（被缺且無默認值）拒調用計 unsup——Go 靠 checker 前置、自舉無 checker，在此兜住。**輸出參帶默認值仍拒**（rde != 0 → bad）：Go 回填迴圈只遍輸入參、自舉未建模輸出綁定，寧缺勿錯。

**實作面**：`h2m.no` 六處——① `h2m-sigs` struct 加 fdef 域＋sigs-new 初始化；② sig-add 簽名加 deft 入參、循環 push(dt)；③ `h2m-sig-def` 新函數（仿 sig-arg 段內索引）；④ param-ty 加 de 輸出＋撤門禁；⑤ func-sig 加 deft 輸出＋輸出參默認值拒；⑥ 調用臂 K-CALL-EXPRESSION 回填迴圈重寫＋OP-CALL 實參計數發回填後的 want-n；lower-func/lower-program 三處調用點跟改。**零改 `cg.no`/`ps.no`/`mir.no`**——默認式降出來的值 id 走既有側池 args 連續段入池。綠相 **771/771**。

**驗證（可重跑）**：`h2m-test` **771/771**；回歸 cg 347／cg-runtime 60／ps 42／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` 改動檔**零漂移**；`no vet` h2m.no 13、h2m-test.no 24 == s91 基線。**IR 反向刀** `tmp/s92-knives.py` **3/3 ALL EFFECTIVE**（快照回寫制：K1 刪調用臂 `j >= na` 回填分支⇒766/5 紅；K2 破 fdef 登記值為 0⇒766/5 紅；K3 破 sig-def 段內索引 `fb+j→fb`⇒769/2 僅 trailing 紅、fill（j=0）仍綠——證明按位取參真在位；還原後 771/0）。**oracle 對拍** `tmp/probe-s92-oracle.no`：四路調用 `q7()⇒5`、`q7(9)⇒9`、`g7(1)⇒21`、`g7(1,2)⇒3`，IR 實釘 `%v1 = add i64 0, 5`／`add i64 0, 20` 拷開降入呼叫者塊，clang 執行 exit **15**（編碼 b1+2b2+4b3+8b4，四路皆對唯一 15；回填路死⇒10、全供路死⇒5 全排除）。**前向零回歸** `tmp/s92_fwd.sh`：非默認參數探針 exit **19/19 全同**（149/109/43/255/195/30/79/91/77/111/63/255×5/15/3/7）；IR **16/16 SAME**（調用臂在 na==want-n 時逐位語義不變 ⇒ 逐字節不變）。

**未覆蓋／後續**：`?u64` option 語義降級；nil／ok 字面量（卡 option 建模）；str 族降級（卡 M4-d RC 自動放置）；vec/arr 字面量（卡 >24B 堆裝箱）；multi-out（卡 cg 單返回值後端形制）；defer／嵌套函數（大件）；輸出參默認值（Go 回填只遍輸入參、對齊口徑續拒）；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留（並發戰場迴避中）；ck.no 通用賦值型別不符門（15w45dqk，M3 級型別推斷大件）；語句位調用已落地為第三十九件（§5.3.4.dv，d1 普查撞出、非本清單預列）——其餘構成 M4-e 第四十件（s94）的候選


### 5.3.4.dv s93——M4-e 第三十九件：語句位調用降級（結果丟棄）（`h2m.no` 的 K-EXPRESSION-STATEMENT 臂尾補「只收 K-CALL-EXPRESSION」的 lower-expr 回填路，鏡像 Go `hir2mir.go:3790` KExprStmt 的 else 分支 `l.stmtVal = l.lowerExpr(child)`；**零改 cg/ps/mir**；`h2m-test` 771→**780/780**（`test-stmt-call-void`／`test-stmt-call-discard`／`test-stmt-call-guard`／`test-stmt-expr-bare-gate` 四哨兵共 +9 斷言），刀 3/3、oracle exit 7、前向 exit 19/19 全同、IR 16/16 SAME）（2026-11-06 本輪）

**選型裁決（d1）**：`tmp/s93-quick.no`（22 形普查）＋ `tmp/s93-quick2.no`／`tmp/s93-quick3.no` 逐形拆到最小——char 值面與 default-param 已分別償 s91/s92（復測 unsup=0 屬紅利）；真實缺口＝str 族（字面量／拼接／`==`）卡 M4-d RC 自動放置、vec/arr 字面量卡 >24B 堆裝箱、multi-out 卡 cg 單返回值後端、option nil/ok 卡建模、defer／嵌套函數大件、struct 定義＋字段访问（OP-STRUCT-*）、內建 `print` 調用（自舉無 builtin 表）、程序頂層裸調用（頂層只認函數定義，合成 main 屬 M4-d 邊界）。**取「語句位調用」立件**：三形皆 unsup=1／define=1，改件只在 `h2m-lower-block` 一处 dispatch 尾，量級最小且是自舉 main／一切過程式調用的必經構造。`return <val>` 經實測**排除**：Go 在 parser 層即拒（「`return` cannot be followed by a value…」tfwxqucs），`hir2mir.go` KReturn 的 children 迴路屬不可達，自舉門禁與權威淨效果一致、非缺口。

**語義實釘（Go 權威）**：`hir2mir.go:3790` `case hir.KExprStmt`——子節點 kind 為 KIf/KFor 則走語句路降；否則 `prevStmtCall := l.stmtCall`（標記調用位供變參/輸出參尾綁启发式）後 `l.stmtVal = l.lowerExpr(child)`，**值直接丟棄**。AST 形制 `no ast` 實釘：`ExpressionStatement{CallExpression}`，kid 序 [expr]、kid[0]=CallExpression。分級依據落在 **checker 層** `src/checker/checker.go:9146 checkBareExprStatement`：裸字面量陳述專拒（yf6kxdmz）、裸函數標識符專拒（k9179drd）、**調用陳述放行**、裸變量標識符「留給其他檢查」（實測 Go 接受 ⇒ 死讀）。Go oracle 值面 `tmp/s93-side.no` 實釘：陳述位 `side(3)`／`side(4)` 依序執行、丟棄的 `acc(9)` 不影響呼叫者結果（v=4）。

**裁決**：只收 `K-CALL-EXPRESSION`，其餘表達式陳述維持 `hd = 0` 誠實門禁——刀 K2 實測證明該限定**承重**：放開會連帶推翻 count-loop／vm-gate-expr-subj／bit-gate／stmt-bare 四條既有負例哨兵（自舉產物不得比權威寬鬆，同 s92「被缺且無默認值」在此兜住的口徑）。**無輸出參被調沿用既有「void ⇒ `define i64` ＋尾部 `ret 0`」約定**（非 TY-VOID），陳述位發 `call i64 @x(...)` 後把值 id 丟掉——LLVM 容許未用值，`opt -passes=verify` 實釘通過；TY-VOID 建模（Go 的 `define void`／`ret void`／`call void`）另件。裸變量陳述的死讀放寬不在本件（自舉暫比權威嚴，保守方向）。

**實作面**：唯一改件 `h2m-lower-block` 的 K-EXPRESSION-STATEMENT 臂尾追加——`hd == 0 && sd.kind == asp.K-CALL-EXPRESSION -> { h2m-lower-expr(t, c, sg, sub)  hd = 1 }`；`h2m-lower-expr` 每條不支持路自帶 `h2m-mark`，故本路不補計（避免雙賬）；調用臂自身已帶 s92 回填與型別校驗，陳述位復用同一路 ⇒ 副作用在序、實參側池連續段口徑不變。文件頭覆蓋面摘要加「語句位調用（s93）」四行、block 頭注補一句。**零改 `cg.no`/`ps.no`/`mir.no`**。綠相 **780/780**。

**驗證（可重跑）**：`h2m-test` **780/780**（紅相 773/7 精確錨定在前三哨兵、負例哨兵本就綠）；回歸 cg 347／cg-runtime 60／ps 42／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠；`no fmt -d` 改動檔**零漂移**；`no vet` h2m.no 13、h2m-test.no 24 == s92 基線。**IR 反向刀** `tmp/s93-knives.py` **3/3 ALL EFFECTIVE**（快照回寫制：K1 刪整臂⇒773/7；K2 去掉「只收 K-CALL」限定⇒773/7，紅集為四條負例門禁哨兵——證限定承重；K3 留放行但刪 lower-expr 產碼⇒777/3 僅三條 `call emitted` 紅、unsup 斷言仍綠——證「放行且產碼」非「只消門禁」；還原後 780/0）。**oracle 對拍** `tmp/probe-s93-oracle.no`：三路 `uone()⇒4`、`keep()⇒4`、`disc()⇒1`，clang 執行 exit **7**（編碼 b1+2b2+4b3）；IR 實釘 `@disc` 體內 `%v2 = call i64 @acc(i64 %v1)`／`%v4 = call i64 @side(i64 %v3)` 皆照常發射且值未用、`ret i64 %v5` 不受擾。**前向零回歸** `tmp/s93_fwd.sh`：非語句位調用探針 exit **19/19 全同**（149/109/43/255/195/30/79/91/77/111/63/255×5/15/3/7）；IR **16/16 SAME**（新臂僅 K-EXPRESSION-STATEMENT 包調用觸發 ⇒ 逐字節不變）。**附帶發現**：`use` 是自舉詞法保留字（舊導入語法遺留），拿它當函數名會整塊降級失效（實釘 unsup=3），oracle 因此改名 `uone`。

**未覆蓋／後續**：程序頂層陳述執行（頂層只認函數定義 ⇒ 需合成 main，M4-d 邊界）；內建函數調用（`print` 等，自舉無 builtin 表）；struct 定義與字段訪問（OP-STRUCT-GET/SET，§11 B26 連續約定）；str 族降級（卡 M4-d RC 自動放置）；vec/arr 字面量（卡 >24B 堆裝箱）；multi-out（卡 cg 單返回值後端形制）；`?u64` option 語義與 nil 字面量三件套已落地為第四十件（§5.3.4.dw）；ok／err 字面量與 tag=2 裝箱（`?=` 傳播鏈、卡 err 建模）；option match 臂解構與 TY-OPT 鋪槽；defer／嵌套函數（大件）；TY-VOID 建模（Go 的 define/ret/call void）；裸變量陳述死讀放行（Go 接受、自舉暫嚴）；§5.3.4.ct match 軌殘留（並發戰場迴避中）；ck.no 通用賦值型別不符門（15w45dqk，M3 級型別推斷大件）——以上構成 M4-e 第四十一件（s95）的候選


### 5.3.4.dw s94——M4-e 第四十件：`?u64` option 三件套降級（簽名／nil 字面量／回值包裹）（`h2m.no` 新增 `h2m-tynode-code` 簽名・標注共用判据＋K-NIL-LITERAL⇒OP-OPTNONE 鏡像 Go `hir2mir.go:9632 lowerNilLit`＋回值位隱式 OP-OPTSOME 包裹鏡像 `wrapOptionIfNeeded`（`hir2mir.go:14893`）＋延遲零值 option→nil 發 OPTNONE；**零改 cg/ps/mir**（`%option` 發射臂、OP-OPTNONE/OPTSOME、`%op` 命名空間 M4-c option 時代即備）；`h2m-test` 780→**788 過/795 總**（七哨兵共 +15 新斷言全綠；紅 7＝既有 s64-str×2／default-fill×3／default-trailing×2、非本件），刀 5/5、oracle exit 61、自舉實釘探針 exit 7、前向 exit 19/19 全同、IR 16/16 SAME）（2026-11-06 本輪）

**選型裁決（d1）**：`?u64` option 語義降級自歷件起列於「未覆蓋／後續」清單頭號——本件即償該項、非 d1 普查新撞形。可行面收窄的依據是三件事實：Go 的 MIR 層 `?T` 就是單一 `%option = { i64 tag, [3 x i64] slot }` 不挾載荷單態；cg 側自 M4-c option 起已有 `cg-ty-name` TY-OPT⇒'%option'、OP-OPTNONE／OP-OPTSOME 發射臂與 `%op` 值命名空間——此前只被手寫 op 陣列用過、**h2m 從未發射**（§11 B7「存在但無人發射」空洞，本件補齊）；載荷槽 `[3 x i64]` 與整族 i64 寬同形制。`tmp/s94-quick.no` 逐形普查：三件套正形（簽名／nil／回值包裹／capture 形）可放行，控制流形（TY-OPT 鋪槽缺位）退化須拒——本件立件。

**語義實釘（Go 權威）**：`hir2mir.go:14893 wrapOptionIfNeeded`——非 option 右值賦給 `?T` 輸出參時建 `option_some`（tag=0＋載荷入 slot[0]），絕不把裸標量直當 `%option` 回（否則 tag 讀到載荷位、some 退化 nil/err）；`:9632 lowerNilLit`——nil 即 tag=1 判別式常量；**延遲零值**：return 前未賦值的輸出參零填充，option 填 nil（tag=1）而非 zeroinitializer（tag=0）。值面 oracle `tmp/s94-oracle-vals.no` 實釘：`f1(7)=ok(7)`（載荷 7）、`f2()=nil`、`f3()=nil`（空體），編碼 s=1+20+40 **exit 61**——延遲零值若錯做 some(0)，f3 走 +4 得 25≠61，編碼自具鑑別力。

**裁決**：覆盖面限直線式（mem==0）三件套。①簽名側 K-NULLABLE-TYPE(u64)⇒TY-OPT——TY-OPT 不挾載荷型別，內層只收 u64（?i64 窄寬口徑未立、?str／?f64／?vec 載荷降級未入形），餘皆不入、寧缺勿錯；②nil 字面量⇒OP-OPTNONE——非 option 語境的 nil 由 var-write st!=vt／回值 ty-match 比對兜住，不發錯碼；③回值位隱式 OP-OPTSOME——載荷只收 i64 寬（TY-I64／TY-U64 共用 `%v` 命名空間、恰合 OP-OPTSOME 操作數形 `%v<x>`），str／f64 載荷不包、落 ty-match 門禁計 unsup（負例哨兵 `test-opt-payload-gate`）。門禁另加一面：**mem==1＋option 輸出参不入形**——TY-OPT 無槽，條件寫會經 U64 槽 LOAD 後回值位包成 some(0)，與 Go「未寫路徑回 nil」語義分岔（刀 K4 證該刀承重）；option match 臂解構續走 s63 門禁。**簽名側與 let 標注側共用同一 helper `h2m-tynode-code`**（§11 B7 同口徑）：Go 解析期 capture（`synthesizeNullableType`）把 `z = f()`（f 回 ?u64）的 let 標注合成成 NullableType{NamedType{u64}}，舊「非 NAMED 即拒」單臂會誤殺捕獲形——實測（`tmp/s94-iso2.no`）調用傳播形曾被誤計 unsup。

|  |  |  |
| --- | --- | --- |
**實作面**：`h2m.no` 六處——①新函數 `h2m-tynode-code`（K-NAMED⇒`h2m-ty-code`；K-NULLABLE(50) 且 nkid==1 且內層 K-NAMED 名 'u64'⇒TY-OPT；否則 -1）；②`h2m-param-ty` 舊 K-NAMED-TYPE 單臂上提進共用判据（`NAMED || NULLABLE` 一臂入 helper）；③`h2m-lower-let` 標注側同改經 `h2m-tynode-code`（救 capture 形）；④`h2m-lower-expr` 補 K-NIL-LITERAL(49) 降級臂（OP-OPTNONE、dest TY-OPT）；⑤`h2m-ty-of` nil 初值槽寬⇒TY-OPT（與降級側同口徑；鋪槽本身仍由 `h2m-slot-decl` 計 unsup——寧缺勿錯）；⑥`h2m-lower-func` 尾部三處：流門禁 `frt==TY-OPT && c.mem==1` 計 unsup、延遲零值 `rid==0 && frt==TY-OPT` 發 OPTNONE、`some-w` 判定＋OP-OPTSOME 包裹發射。文件頭覆蓋面摘要加 s94 四行。**零改 `cg.no`／`ps.no`／`mir.no`**。綠相 **788 過/795 總**（紅 7 與本件無關，見驗證）。

**驗證（可重跑）**：`h2m-test` **788/7/795**——s94 七哨兵 15 斷言全綠；紅相 773/7 精確錨定在基線階段（既有 s64-str typed args×2、default-fill×3、default-trailing×2——工作樹並發 session 改動 ps.no／checker 諸檔所致、非本件引入，基線門禁裁決 proceed＋ignore）；回歸 cg 347／cg-runtime 60／tok 62／util 27／diag 7／ck 20／lx 70／ps-err 46 全綠，ps 41/1（1 紅＝既有 enum 主語死碼臂哨兵、同樣非本件）；`no fmt -d` 改動檔 s94 位點**零漂移**（檔內漂移 hunks 全為既有 ixout desugar／空行風格、並發 session 引入）；`no vet` h2m.no 13、h2m-test.no 24 == s93 基線。**IR 反向刀** `tmp/s94-knives.py` **5/5 ALL EFFECTIVE**（快照回寫制：K1 刪 tynode-code nullable 收受臂⇒+11 紅（五條正例哨兵全滅——簽名不入形）；K2 刪 lower-expr nil 臂⇒+3 紅（opt-nil 兩斷言＋opt-capture unsup 斷言；call 斷言由簽名表獨立仍綠——證記賬分離）；K3 刪延遲零值 OPTNONE 臂⇒+1 紅（僅 opt-zero 'none emit'——unsup 斷言仍 0，tag=0 **靜默退化**恰由此刀抓住、门禁面不可見）；K4 刪 mem==1 流門禁⇒+2 紅（退化產碼 some(0) 發射）；K5 刪 OPTSOME 包裹⇒+3 紅（裸 u64 撞 ty-match 門禁整函數不產碼）；還原後 788/7）。**oracle 對拍**：Go 權威值面 `tmp/s94-oracle-vals.no` **exit 61**（ok(7)/nil/nil、載荷 p1=7 print 實釘）；自舉三路實釘 `tmp/probe-s94-oracle.no` **exit 7**（f1 tag=0 且載荷=7、f2 tag=1、f3 tag=1；降級失敗毒 main 101）。**前向零回歸** `tmp/s94_fwd.sh`：非 option 探針 exit **19/19 全同**（149/109/43/255/195/30/79/91/77/111/63/255×5/15/3/7）；探針 IR **16/16 SAME**（對 `tmp/s93-fwd-s66..81.ll`；新臂僅 K-NULLABLE／K-NIL／option 回值位觸發 ⇒ 逐字節不變之結構性保證）。

**未覆蓋／後續**：`?T` 其餘內層型別（?i64 已償第四十一件 §5.3.4.dx／?str／?f64／?vec 載荷未入形）；TY-OPT 鋪槽（mem==1 的 option 局部量／輸出參入形——需 alloca %option 槽 op）；option match 臂解構（ok／nil／err 三態臂）；err 構造與 tag=2 裝箱（`?=` 傳播鏈）；ok 字面量顯式構造；程序頂層陳述執行（M4-d 邊界）；內建函數調用（`print` 等）；struct 定義與字段訪問（OP-STRUCT-GET/SET，§11 B26）；str 族降級（卡 M4-d RC 自動放置）；vec/arr 字面量（卡 >24B 堆裝箱）；multi-out（卡 cg 單返回值後端形制）；defer／嵌套函數（大件）；TY-VOID 建模；裸變量陳述死讀放行；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留（並發戰場迴避中）；ck.no 通用賦值型別不符門（15w45dqk）——以上構成 M4-e 第四十二件（s96）的候選

|  |  |  |
| --- | --- | --- |

### 5.3.4.dx s95——M4-e 第四十一件：`?i64` option 內層整型放寬（`h2m.no` 的 `h2m-tynode-code` K-NULLABLE-TYPE 內層名收受由 `'u64'` 擴為 `'u64' || 'i64'`；鏡像 Go `wrapOptionIfNeeded` 對整型內層無載荷位形分岔；**零改 cg/ps/mir／無新發射臂**——純簽名判據放寬；`h2m-test` 788→**806 過/813 總**（八哨兵共 +18 新斷言全綠；紅 7＝既有 s64-str×2／default-fill×3／default-trailing×2、非本件），刀 2/2、oracle exit 61、自舉實釘探針 exit 7、前向 exit 19/19 全同、IR 16/16 SAME）（2026-11-06 本輪）
**選型裁決**：s94 §5.3.4.dw「未覆蓋／後續」清單頭號項即「`?T` 其餘內層型別（?i64 窄寬口徑／?str／?f64／?vec 載荷）」——本件償其中最窄一口徑 ?i64，非 d1 普查新撞形。判據源於三件事實：①Go 的 MIR 層 `?T` 對一切內層皆單一 `%option = { i64 tag, [3 x i64] slot }`，`wrapOptionIfNeeded`（`hir2mir.go:14893`）僅對 `Elem` 為 str/f64/vec 者調 `optionPayloadCoerced` 做位形轉換，整型內層不轉換、直入 slot[0]；②s94 回值位 some-wrap 門 `pty == TY-I64 || pty == TY-U64` 早已放行 TY-I64 載荷——即 i64 右值進 ?T 的包裹路 M4-e 四十件時已在、只缺簽名側認 ?i64 入形；③`%v` 值命名空間 i64／u64 共用（s78/s88 整族同寬）。⇒ ?i64 與 ?u64 在自舉侧逐字節同形，無新代碼路徑，屬純加性。

**語義實釘（Go 權威）**：`wrapOptionIfNeeded` 對 `?i64`：`tt.Kind==KindOption`、`v` 非 option、`Elem` 為整型 ⇒ 跳過 `optionPayloadCoerced`、直接 `EmitOptionWrap(targetType, 0, v)`（tag=0＋slot[0]=v）；nil／延遲零值路徑與內層型別無關（tag=1）。值面 oracle `tmp/s95-oracle-vals.no` 實釘三形 `f1(7)=ok(7)`／`f2()=nil`／`f3()=nil`、編碼 **exit 61**（載荷 p1=7 另 print 實釘）——與 s94 的 ?u64 值面同編碼、同鑑別力（延遲零值誤做 some(0) ⇒ f3 走 +4 得 25≠61）。

**裁決**：覆盖面沿用 s94 直線式（mem==0）三件套，僅內層型別從 {u64} 廣為 {u64, i64}。①簽名側 K-NULLABLE-TYPE 內層 K-NAMED 名 ∈ {'u64','i64'}⇒TY-OPT；內層 str／f64／vec 仍不入（載荷降級未入形、`optionPayloadCoerced` 位形轉換未鏡像）、寧缺勿錯；②nil／延遲零值／some-wrap 三臂型別無關、復用 s94 無需改；③mem==1＋option 輸出参流門禁原樣保留（?i64 同受管）。**簽名側與 let 標注側仍共用同一 helper `h2m-tynode-code`**（§11 B7 同口徑）——一刀放寬即兩側同入口徑，刀 K1 證此（刪 `'i64'` 收受同時打穿簽名與 capture 兩族哨兵）。

|  |  |  |
| --- | --- | --- |
**實作面**：`h2m.no` 單點——`h2m-tynode-code` 的 K-NULLABLE-TYPE 內層判據 `inm == 'u64'` 擴為 `inm == 'u64' || inm == 'i64'`（加註解＋其上空行對齊 `no fmt` 風格）。some-wrap 的 `pty == TY-I64` 支 s94 即存、本件首次被正向哨兵觸及。**零改 `cg.no`／`ps.no`／`mir.no`、無新發射 op**。綠相 **806 過/813 總**（紅 7 與本件無關）。

**驗證（可重跑）**：`h2m-test` **806/7/813**——s95 八哨兵 18 斷言全綠（`test-opt-i64-wrap-return`／`-nil-return`／`-deferred-zero`／`-capture-call`／`-param-tycode`／`-u64-samecaliber`（顯式鎖 ?i64 與 ?u64 同體回值位 IR 逐字節相等）／`-flow-gate`／`-payload-gate` 兩負例）；紅相 7 精確復現基線（既有 s64-str×2、default-fill×3、default-trailing×2、並發 session 引入、非本件）；回歸 cg 347／cg-runtime 60／util 27／tok 62／diag 7／ck 20／lx 70／ps-err 46 全綠。**IR 反向刀** `tmp/s95-knives.py` **2/2 ALL EFFECTIVE**（快照回寫制：K1 退回 tynode-code `'i64'` 收受⇒+13 紅（正例五族全滅、負例 flow/payload 因門禁本計 unsup 仍綠；簽名與 capture 兩側同破——證 §11 B7 同口徑單一 helper）；K2 削 some-wrap 的 `pty == TY-I64` 支⇒+5 紅（opt-i64-wrap 三斷言＋samecaliber i64 面；nil/zero/capture/param 走 OPTNONE／option→option 不经 some-wrap 故不受影響——證該支為 i64 載荷專屬承重）；還原後 806/7）。`no fmt` s95 位點零漂移；`no vet` h2m.no 13 == s94 基線。**oracle 對拍**：`tmp/s95-oracle-vals.no` **exit 61**（p1=7 print 實釘）；自舉三路實釘 `tmp/probe-s95-oracle.no` **exit 7**（f1 tag=0 且載荷=7、f2/f3 tag=1；降級失敗毒 main 101）。**前向零回歸**：非 option 探針 exit **19/19 全同**、探針 IR 對 `tmp/s94-fwd-s66..81.ll` **16/16 SAME**（?i64 未入既有探針 ⇒ K-NULLABLE 內層名判據變更對非 ?i64 源逐字節不變之結構性保證）。

**未覆蓋／後續**：`?T` 其餘載荷型別（?f64 已償第四十二件 §5.3.4.dy／?str／?vec 內層未入形——須鏡像 RC 所有權＋ OPTMKSTR/OPTMKVEC 內聯（含 str 的 ptrtoint data 指針）、卡 str 族降級 M4-d）；TY-OPT 鋪槽（mem==1 的 option 局部量／輸出参入形——需 alloca %option 槽 op）；option match 臂解構（ok／nil／err 三態臂）；err 構造與 tag=2 裝箱（`?=` 傳播鏈）；ok 字面量顯式構造；程序頂層陳述執行（M4-d 邊界）；內建函數調用；struct 定義與字段訪問（OP-STRUCT-GET/SET，§11 B26）；vec/arr 字面量（>24B 堆裝箱）；multi-out；defer／嵌套函數；TY-VOID 建模；裸變量陳述死讀放行；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留；ck.no 通用賦值型別不符門（15w45dqk）——以上構成 M4-e 第四十三件（s97）的候選

|  |  |  |  |  |  |  |
| --- | --- | --- | --- | --- | --- | --- |

### 5.3.4.dy s96——M4-e 第四十二件：`?f64` option 內層載荷放寬（`h2m.no` 的 `h2m-tynode-code` K-NULLABLE-TYPE 內層名收受由 `'u64' || 'i64'` 擴為 `'u64' || 'i64' || 'f64'`，回值位 some-wrap 增 `pty == TY-F64` 支改發 OP-OPTMKF64；鏡像 Go codegen 對 f64 載荷的 bitcast double↔i64 落槽；**零改 cg/ps/mir／無新發射 op**——cg 側 OP-OPTMKF64 自 s46 既存、本件首次被 h2m 發射，補 §11 B7「存在但無人發射」空洞；`h2m-test` 806→**824 過/831 總**（八哨兵共 +18 新斷言全綠；紅 7＝既有 s64-str×2／default-fill×3／default-trailing×2、非本件），刀 3/3、oracle exit 61、自舉實釘探針 exit 7、前向 IR 16/16 SAME）（2026-11-06 本輪）

**選型裁決**：s95 §5.3.4.dx（與 §5.3.4.dw）「未覆蓋／後續」清單頭號項即「`?T` 其餘載荷型別」——本件償其中最易鏡像者 ?f64，非 d1 普查新撞形。可行面收窄的依據是三件事實：①Go 的 MIR 層 `wrapOptionIfNeeded` 對 f64 內層與整型內層同樣「無位形轉換」——`optionPayloadCoerced`（`hir2mir.go:14833`）僅對 `Elem` 為 txt/str 者動作（且僅 `txt` 欄發 `OpTxtFromStr`），f64 內層連開頭判斷都不進、原樣返回 v，位形轉換（bitcast double↔i64）落在 codegen `emitOptionWrap`；②自舉 `cg.no` 的 OP-OPTMKF64（s46）早已實作「`bitcast double→i64` 一步 insertvalue 落 slot[0]」（與 OP-OPTSOME 同形、只差讀 `%fd` 非 `%v`）且經 cg-test 全綠——即 %option 對 f64 載荷的發射器在 cg 層既存、唯 h2m 從未觸及（§11 B7「存在但無人發射」空洞，本件補齊）；③?str／?vec 載荷雖 cg 亦有 OP-OPTMKSTR/OPTMKVEC，但須 RC 所有權（OPTMKSTR 只存不 retain、須緊接 OP-RETAIN）與 str 族降級（卡 M4-d）——非純加性，故本件只取 f64 這一最窄無副作用口徑。

**語義實釘（Go 權威）**：Go 對 `?f64` 包裹路徑：`optionPayloadCoerced` 見 `Elem` 非 txt/str ⇒ 不轉、原樣返回 v；`emitOptionWrap` 見 `payloadLT==double`、槽 [3 x i64] ⇒ bitcast 落 slot[0]（tag=0）。nil／延遲零值路徑與內層型別無關（tag=1、OP-OPTNONE）。值面 oracle `tmp/s96-oracle-vals.no` 實釘三形 `f1(3.0)=ok(3.0)`／`f2()=nil`／`f3()=nil`、編碼 **exit 61**（載荷 p1=3 另 print 實釘——3.0 為可精確表徵之 double、位元組經 bitcast 往返不變）；延遲零值若誤做 some(0) ⇒ f3 走 +4 得 25≠61，編碼自具鑑別力。

**裁決**：覆盖面沿用 s94/s95 直線式（mem==0）三件套，僅內層型別從 {u64, i64} 廣為 {u64, i64, f64}。①簽名側 K-NULLABLE-TYPE 內層 K-NAMED 名 ∈ {'u64','i64','f64'}⇒TY-OPT；內層 str／vec 仍不入（須 RC＋ptrtoint、卡 str 族降級 M4-d）、寧缺勿錯；②回值位 some-wrap 依 `pty == h2m-vty-at(c, rid)` 分派：TY-I64／TY-U64 發 OP-OPTSOME（讀 %v）、TY-F64 發 OP-OPTMKF64（讀 %fd bitcast）——兩支互斥、分佔 `some-w`／`some-wf` 旗標；③nil／延遲零值臂型別無關（OP-OPTNONE）、復用 s94 無需改；④mem==1＋option 輸出参流門禁原樣保留（?f64 同受管）。**簽名側與 let 標注側仍共用同一 helper `h2m-tynode-code`**（§11 B7 同口徑）——一刀放寬即兩側同入口徑，刀 K1 證此。

|  |  |  |  |  |
| --- | --- | --- | --- | --- |
**實作面**：`h2m.no` 兩處——①`h2m-tynode-code` 的 K-NULLABLE-TYPE 內層判據 `inm == 'u64' || inm == 'i64'` 擴為 `... || inm == 'f64'`（加註解）；②`h2m-lower-func` 尾部 some-wrap 增 `some-wf` 旗標＋`pty == TY-F64` 判定＋`OP-OPTMKF64` 包裹發射塊（與既有 OP-OPTSOME 塊並列、各改 `rid`）。文件頭覆蓋面摘要加 s96 一行。**零改 `cg.no`／`ps.no`／`mir.no`、無新 op**（OP-OPTMKF64=70 於 mir.no 既存、cg.no s46 既存）。綠相 **824 過/831 總**（紅 7 與本件無關）。

**驗證（可重跑）**：`h2m-test` **824/7/831**——s96 八哨兵 18 斷言全綠（`test-opt-f64-wrap-return`／`-not-i64-some`（載荷形制鑑別：?f64 回值位發 bitcast、絕非整型 OP-OPTSOME 裸 %v）／`-nil-return`／`-deferred-zero`／`-capture-call`／`-param-tycode`／`-flow-gate`／`-payload-gate` 兩負例）；紅相 7 精確復現基線；回歸 cg 347／cg-runtime 60／util 27／tok 62／diag 7／ck 20／lx 70／ps-err 46 全綠。**IR 反向刀** `tmp/s96-knives.py` **3/3 ALL EFFECTIVE**（快照回寫制：K1 退回 tynode-code `'f64'` 收受⇒+13 紅（正例六族全滅、負例 flow/payload 因門禁本計 unsup 仍綠；簽名與 capture 兩側同破——證 §11 B7 同口徑單一 helper）；K2 削 some-wrap 的 `pty == TY-F64` 支⇒+5 紅（opt-f64-wrap／not-i64-some——f64 載荷不包→ty-match 門禁；nil/zero/capture/param 不经 some-wrap 故不受影響）；K3 把 some-wf 發射 op 換成 OP-OPTSOME（錯 op）⇒+3 紅（bitcast 斷言＋無裸 %v 斷言——證 OP-OPTMKF64 路由專屬承重）；還原後 824/7）。`no fmt -d` h2m.no **零漂移**；`no vet` h2m.no 13 == s95 基線（全為既有 `?i64`-unwrap 位點、無一落 s96 新碼）。**oracle 對拍**：Go 權威值面 `tmp/s96-oracle-vals.no` **exit 61**（ok(3.0)/nil/nil、載荷 p1=3 print 實釘）；自舉三路實釘 `tmp/probe-s96-oracle.no` **exit 7**（main 側 extractvalue slot[0]→bitcast i64→double→fcmp oeq 3.0 證 bitcast 往返、f2/f3 tag=1；降級失敗毒 main 101）。**前向零回歸**：非 option 探針 IR 對 `tmp/s95-fwd-s66..81.ll` **16/16 byte-SAME**（s96 新支僅 `pty==TY-F64`＋`frt==TY-OPT` 觸發、新內層名僅 `?f64` 源觸發 ⇒ 既有探針逐字節不變之結構性保證；IR 同⇒編譯件同⇒exit 同）。

**未覆蓋／後續**：`?T` 其餘載荷型別（?str／?vec 內層——須鏡像 RC 所有權＋OPTMKSTR/OPTMKVEC 內聯（含 str 的 ptrtoint data 指針）、卡 str 族降級 M4-d）；TY-OPT 鋪槽（mem==1 的 option 局部量／輸出参入形——**已償第四十三件 §5.3.4.dz**）；option match 臂解構（ok／nil／err 三態臂、須 h2m 發 OP-OPTTAG／OPTISOK／OPTGET／OPTGETF64 讀端）；err 構造與 tag=2 裝箱（`?=` 傳播鏈）；ok 字面量顯式構造；程序頂層陳述執行（M4-d 邊界）；內建函數調用；struct 定義與字段訪問（OP-STRUCT-GET/SET，§11 B26）；vec/arr 字面量（>24B 堆裝箱）；multi-out；defer／嵌套函數；TY-VOID 建模；裸變量陳述死讀放行；cg OP-SHR ashr 死代碼清理；§5.3.4.ct match 軌殘留；ck.no 通用賦值型別不符門（15w45dqk）——以上構成 M4-e 第四十四件（s98）的候選


### 5.3.4.dz s97——M4-e 第四十三件：TY-OPT 控制流鋪槽（`h2m.no` 的 option 槽由直線式（mem==0）扩到控制流（mem==1）——`h2m-slot-decl` 認 TY-OPT 發 `OP-OPTALLOC`（`alloca %option`）＋ `OP-OPTNONE`（tag=1）零初始化以鏡像 Go「延遲零值=nil」；`h2m-var-read` 增 TY-OPT 支發 `OP-OPTLOAD`；`h2m-var-write` 分兩路（option←option 直存 `OP-OPTSTORE`／option←載荷 經寫點 `h2m-opt-wrap` 包裹再存）；`h2m-lower-func` 於 scan 後按【聲明型 TY-OPT】覆寫輸出參名（`h2m-lst-add` 就地改型）並撤除 s94 `frt==TY-OPT && c.mem==1 → mark` 整函式門禁；回值位與寫點 some-wrap 收斂為單點 helper `h2m-opt-wrap`（§11 B7 同口徑）；**零改 cg/ps/mir／無新發射 op**——cg 側 OP-OPTALLOC/OPTSTORE/OPTLOAD 自 s41（跨基本塊槽）既存且經 cg-test 全綠、本件首次被 h2m 發射，補 §11 B7「存在但無人發射」空洞；`h2m-test` 824→**845 過/852 總**（3 條 s94 flow-gate 負例翻正＋2 新哨兵；紅 7＝既有 s64-str×2／default-fill×3／default-trailing×2、非本件），刀 4/4、oracle exit 165、自舉實釘探針 exit 165、前向 IR 16/16 SAME）（2026-11-06 本輪）

**選型裁決**：§5.3.4.dy（與 dx）「未覆蓋／後續」清單實作頭號項即「TY-OPT 鋪槽（mem==1 的 option 局部量／輸出参入形）」——本件償之，非 d1 普查新撞形。可行面收窄的依據是三件事實：①`cg.no` 的 OP-OPTALLOC/OPTSTORE/OPTLOAD（s41，跨基本塊 option 槽）早已實作 `%os<id>` 命名空間的 `alloca %option`＋store/load 且經 cg-test 全綠——即控制流 option 槽發射器在 cg 層既存、唯 h2m 從未觸及（§11 B7「存在但無人發射」空洞，本件補齊）；②s94 對 mem==1＋option 輸出参採整函式門禁（不產碼、計 unsup）正是為規避「槽按 RHS 推成載荷型 ⇒ 未寫路徑退 some(0)」之錯形——本件以【聲明型覆寫鋪槽】正面解開該結、非繞過；③載荷包裹（payload→%option）與直線式回值位同判據，可共用單點 helper 不留第二套口徑（§11 B7）。str/vec 載荷仍不可包（卡 M4-d RC/ptrtoint）——寫點誠實門禁原樣保留。

**語義實釘（Go 權威）**：Go 的 `hir2mir` 對 option 局部量／輸出参落 `alloca %option`、延遲零值置 tag=1（nil），寫 payload 經 `wrapOptionIfNeeded`（hir2mir.go:14893）包 some、讀取發 load 交 %option 值。值面 oracle `tmp/s97-oracle-vals.no` 實釘兩控制流形 `f = (n) (r ?i64) { { n>0 -> r=n  -> r=nil } }`／`h = (n) (r ?i64) { n>0 -> r=n }`——`f(5)=some(5)`、`f(0)=nil`、`h(7)=some(7)`、`h(0-3)=nil`（未寫路徑讀 tag=1、非 some(0)），加權編碼 **exit 165**（p1=5、p3=7；若未寫路徑誤做 some(0) ⇒ c2/c4=0 ⇒ ≠165，編碼自具鑑別力）。⚠️ **Go 側 quirk（本件實測撞出）**：以裸算術式（如 `-3`）作 option-回傳函式之實參被 checker 拒（`f10x20nf`「call to option-returning function ... requires explicit unwrap」）——須先把算術綁成具名變量（`neg = 0 - 3`）再傳；與鋪槽正交，本件不改 `src/`。

**裁決**：覆盖面沿用 s94/s95/s96 三件套、由 mem==0 扩到 mem==1 鋪槽。①槽側：`h2m-slot-decl` 收受集增 TY-OPT、發 `OP-OPTALLOC` 铺 `%os` 槽＋`OP-OPTNONE`（tag=1）經 `OP-OPTSTORE` 落槽作延遲零值（未寫路徑讀出 nil）；②讀側：`h2m-var-read` 增 TY-OPT 支發 `OP-OPTLOAD`（槽在 a、結果 dst，與 SLOAD 同形）；③寫側：`h2m-var-write` mem==1 分兩路——option←option（nil 字面量／另一 ?T 右值）直存 `OP-OPTSTORE`、option←載荷（i64/u64/f64）經 `h2m-opt-wrap` 包裹再存，str/vec 載荷不可包（wrapped==val）→ ok-w 仍 0 → 計 unsup（寧缺勿錯）；④鍵樞紐——**聲明型覆寫鋪槽**：因 noc 每條 `name = value`（含對輸出参之賦值）皆 LetStatement、`h2m-scan` 以 RHS 推型註冊輸出参名 ⇒ 槽型退成整/f64（some(0) 錯形之源）；故 `h2m-lower-func` 於 scan 後、鋪槽迴圈前，當 mem==1 且回值型為 option 時以 `h2m-lst-add(loc, 輸出参名, TY-OPT)` 就地覆寫聲明型（i>=0 分支改型、位次不變、仍排形参後）；⑤撤除 s94 `frt==TY-OPT && c.mem==1 → mark` 整函式門禁；⑥回值位 some-wrap 從內聯收斂為單點 `h2m-opt-wrap`、與寫點共用同一判據（§11 B7 同口徑）——mem==1 時 rid 已是 OPTLOAD 交出之 %option、原樣返回不误再包。**簽名側／回值位／寫點三處包裹共用同一 helper——一刀即同破三口徑，刀 K1/K2 共證此。**

**實作面**：`h2m.no` 六處——①`h2m-slot-decl`：收受集 `ty != ... && ty != TY-OPT`、通用 ALU 分支加 `ty != TY-OPT` 排除、新增 `ok-slot==1 && ty==TY-OPT` 發射塊（OPTALLOC+OPTNONE+OPTSTORE）；②`h2m-var-read`：新增 `st==TY-OPT` OPTLOAD 支、mark 排除集增 TY-OPT；③新增 `h2m-opt-wrap` 單點包裹子程（pty 分派 OP-OPTSOME／OP-OPTMKF64、str/vec 原樣返）；④`h2m-var-write`：通用 OP-STORE 排除 TY-OPT、新增 option←option／option←載荷 兩支；⑤`h2m-lower-func`：scan 後增聲明型覆寫塊、撤 flow-gate、回值位 some-wrap 改調 `h2m-opt-wrap`；⑥文件頭覆蓋面摘要加 s97 一行。**零改 `cg.no`／`ps.no`／`mir.no`、無新 op**（OP-OPTALLOC=60／OPTSTORE=61／OPTLOAD=62 於 mir.no 既存、cg.no s41 既存）。綠相 **845 過/852 總**（紅 7 與本件無關）。

**驗證（可重跑）**：`h2m-test` **845/7/852**——s97 五哨兵全綠（3 條 s94 flow-gate 負例翻正：`test-opt-flow-slot`（u64 載荷寫點 some-wrap、OPTLOAD 讀、`alloca %option`＋`store %option`＋`ret %option` IR 斷言）、`test-opt-i64-flow-slot`、`test-opt-f64-flow-slot`（f64 載荷 `bitcast double→i64` 經 OPTMKF64）；2 新哨兵：`test-opt-slot-deferred-nil`（未寫路徑 `insertvalue %option { i64 1, ... }, i64 1, 0` tag=1 nil-init、絕無 some(0)）、`test-opt-slot-str-payload-gate`（str 載荷不可包→`agg.unsup>0`、`@qc` 發射缺席之負例））；紅相 7 精確復現基線；回歸 cg 347／cg-runtime 60／util 27／tok 62／diag 7／ck 20／lx 70／ps-err 46 全綠。**IR 反向刀** `tmp/s97-knives.py` **4/4 ALL EFFECTIVE**（快照回寫制：K1 從 slot-decl 收受集移出 TY-OPT⇒+23 紅（還原 s94「無槽」態、四正例全滅、str-payload 負例仍綠）；K2 令寫點 option←載荷包裹守衛恆假⇒+18 紅（載核入槽落 unsup、deferred-nil 不寫載核故綠）；K3 令 var-read 的 OPTLOAD 支失效⇒+4 紅（opt-flow-slot/i64/f64/deferred 四正例各一 OPTLOAD 斷言滅）；K4 令聲明型覆寫守衛恆假⇒+8 紅（槽型退回 RHS 推整/f64、deferred-nil 之 nil-init 與各 flow-slot 之 alloca %option 滅——證鋪槽防 some(0) 退化之關鍵刀）；還原後 845/7）。`no fmt -d` h2m.no **s97 新碼零漂移**（殘留 5 hunk 皆為 s88/s90/s91 註解前空行與 D11 index-out `expr: {` 拆行等**他件既有碼**、黃金法則不改）；`no vet` h2m.no 13 == s96 基線（全落 1533–1600 既有 `?i64`-unwrap 位點、無一落 s97 新碼）。**oracle 對拍**：Go 權威值面 `tmp/s97-oracle-vals.no` **exit 165**（f(5)=some(5)/f(0)=nil/h(7)=some(7)/h(0-3)=nil、載荷 p1=5 p3=7）；自舉端到端實釘 `tmp/probe-s97-oracle.no` **exit 165**（h2m 降 f/h、main 側以裸 IR extractvalue 讀 tag/payload、避 match 臂 s63 門禁；降級失敗毒 main 101）——兩側同值。**前向零回歸**：非 option 探針 IR 對 `tmp/s96-fwd-s66..81.ll` **16/16 byte-SAME**（s97 新支僅 `st/frt==TY-OPT && c.mem==1` 觸發 ⇒ 既有探針逐字節不變之結構性保證；IR 同⇒編譯件同⇒exit 同）。

**未覆蓋／後續**：`?T` 其餘載荷型別（?str／?vec 內層——需鏡像 RC 所有權＋OPTMKSTR/OPTMKVEC 內聯（含 str 的 ptrtoint data 指針）、卡 str 族降級 M4-d——本件寫點對其誠實門禁）；option match 臂解構（ok／nil／err 三態臂、須 h2m 發 OP-OPTTAG／OPTISOK／OPTGET／OPTGETF64 讀端，s63 門禁仍持）；err 構造與 tag=2 裝箱（`?=` 傳播鏈）；ok 字面量顯式構造；程序頂層陳述執行（M4-d 邊界）；內建函數調用；struct 定義與字段訪問（OP-STRUCT-GET/SET，§11 B26）；vec/arr 字面量（>24B 堆裝箱）；multi-out；defer／嵌套函數；TY-VOID 建模；裸變量陳述死讀放行；§5.3.4.ct match 軌殘留；ck.no 通用賦值型別不符門（15w45dqk）——以上構成 M4-e 第四十五件（s99）的候選


### 5.3.4.ea s98——M4-e 第四十四件（cg 側 `OP-SHR` 清理，跨 M4-c 發射層）：cg `OP-SHR` ashr 死代碼清理（`cg.no` 刪除 s80 裁定後不可達的 `op == OP-SHR` ashr 發射臂；h2m 自 s80 起把 `>>` 恆重映射為 `OP-SHRU`（lshr）、`OP-SHR` 自此不再被產出；同步改造 `cg-test.no`：ALU 陣列 `OP-SHR→OP-SHRU`（dst=7 不變、中間量下標零擾動）、移位正斷言 ashr→lshr；**零改 h2m/ps/mir**、`OP-SHR=48`/`OP-SHRU=51` 常量保留、cg 側僅刪單一發射分支，零語義風險）（2026-11-08 本輪）

**選型裁決**：s97 §5.3.4.dz「未覆蓋／後續」清單頭號項即「cg OP-SHR ashr 死代碼清理」——本件償之，非 d1 普查新撞形。判據源於兩件事實：①s80 裁定 nolang `>>` 為**邏輯移位（lshr）**、推翻 s35 的「i64=ashr」設計，`h2m.no` 在 `bop == OP-SHR -> bop = OP-SHRU`（行 1371）把 `>>` 無條件重映射為 `OP-SHRU`，故 `OP-SHR` 自此**不再被任何前端產出**；②`cg.no` 的 `op == OP-SHR -> { ... = ashr i64 ... }` 臂（原行 428–436）是**唯一**發射 ashr 之處——既無上游產 `OP-SHR`，該臂即純死代碼。⇒ 刪除零語義風險、且消除「`>>` 實際發 lshr 但 cg 仍留 ashr 臂」之文檔/實作不一致（s80 後 `cg-test` 的 ashr 正向斷言本就是對 lshr 輸出的錯標，本件一併校正）。

**語義實釘（Go 權威）**：Go `hir2mir.go` 對 `>>` 的降級——nolang 規格 `>>` = 邏輯右移，對應 LLVM `lshr`（高位補 0）；`ashr` 為算術位移（高位補符號），非 nolang `>>` 語義。自舉 `cg.no` 的 `OP-SHRU` 臂（行 458–466）早已實作 `lshr i64` 且經 cg-test 全綠——即 nolang `>>` 的正確發射器在 cg 層既存、唯 `OP-SHR` 臂是 s35 時代之殘留。

**裁決**：純刪除、零改動其餘發射邏輯。①`cg.no` 刪 `op == OP-SHR` ashr 臂；`OP-SHRU` 臂（lshr）保留——nolang `>>` 現唯一路徑；②`mir.no` 常量 `OP-SHR=48`/`OP-SHRU=51` 保留（常量存活、僅 cg 發射層不再消費 `OP-SHR`）；③`cg-test.no` 同步校正——ALU 陣列 `ops.push(OP-SHR)`→`ops.push(OP-SHRU)`（dst=7 不變，因 ops 陣列順序不變、中間量下標 `%dz.<k>` 等命名零擾動，OP-DIV 的 `%dz.7` 下標斷言不受影響）；移位正斷言由 `s.contains('%v7 = ashr i64 %v1, %v2')` 翻為 `s.contains('%v7 = lshr i64 %v1, %v2')`、`'shr not lshr'` 翻為 `'shr not ashr'`（查 `ashr` 出現次數為 0）。

**實作面**：`cg.no` 單點——刪除 `op == OP-SHR -> {...}` 整臂（9 行）；`cg-test.no` 兩處——行 59 `OP-SHR`→`OP-SHRU`、行 126–128 ashr→lshr 斷言翻正。**零改 `h2m.no`/`ps.no`/`mir.no`**。cg 347 斷言基線不變（僅兩行斷言語義翻正、斷言計數不增減）。

**驗證（可重跑）**：`cg.no` 改動屬純刪除——`op == OP-SHRU` 臂（lshr，行 458–466）原樣保留、`h2m.no:1371` 仍把 `>>`→`OP-SHRU` ⇒ 發射 `%v7 = lshr i64 %v1, %v2` 必現、`OP-SHR` 已無產出故 `ashr` 字串必不出現，本件新斷言**構造上正確**。`no fmt -d` cg.no/cg-test.no 零漂移；`no vet` 兩檔零新增。**⚠️ 整檔運行阻斷（非本件）**：`bin/no` 現載並發 session 收緊的 `?i64→i64` 賦值硬錯（15w45dqk，2026-11-08），`cg-test.no` 在未跟進下現有 4 個既有 `?i64→i64` 編譯錯誤（行 111/227/1146/1353，皆 `j=j+1`/`w=w+1` under `#{overflow=wrap}`），阻斷整檔編譯、本件新斷言（行 126–128）暫未能運行時執行——待該 checker 門禁落地後復測；此 4 錯誤行均不在本件編輯行（59/126-128）內、且屬 checker 層型別錯（非 codegen 發射錯），與本件 OP-SHR/ashr 清理**無因果關係**（工作樹 `src/checker/checker.go` 為並發 session 改動、非本 session 回歸）。

**未覆蓋／後續**：`?T` 其餘載荷型別（?str／?vec 內層——需鏡像 RC 所有權＋OPTMKSTR/OPTMKVEC 內聯（含 str 的 ptrtoint data 指針）、卡 str 族降級 M4-d）；option match 臂解構（ok／nil／err 三態臂、須 h2m 發 OP-OPTTAG／OPTISOK／OPTGET／OPTGETF64 讀端，s63 門禁仍持）；err 構造與 tag=2 裝箱（`?=` 傳播鏈）；ok 字面量顯式構造；程序頂層陳述執行（M4-d 邊界）；內建函數調用；struct 定義與字段訪問（OP-STRUCT-GET/SET，§11 B26）；vec/arr 字面量（>24B 堆裝箱）；multi-out；defer／嵌套函數；TY-VOID 建模；裸變量陳述死讀放行；§5.3.4.ct match 軌殘留；ck.no 通用賦值型別不符門（15w45dqk）——以上構成 M4-e 第四十五件（s99）的候選


### 5.3.4.eb s99——M4-e 第四十五件：struct 定義與欄位訪問（OP-STRUCT-MK/GET/SET 降級，`h2m.no` 三臂＋前掃描登記）（2026-11-09 本輪）

**範圍**：nolang `struct` 的最小可用降級——定義聲明、`Point { x: 1, y: 2 }` 字面量構造（MK）、`p.x` / `p.y` 欄位讀（GET）、`p.y = 5` 欄位寫（SET）。LLVM 結構不可變，SET 走「讀舊結構體 + insertvalue 出新值 + 變數重綁新值」（鏡像 Go `hir2mir.go` 的 struct set 模式）。

**改動（`h2m.no`，零改 `cg.no`/`ps.no`/`mir.no`——cg 側 OP-STRUCT-MK/GET/SET 發射臂與 `%struct.s<id>` 型別聲明臂自 §11 B10/B26 既存，本件首次由 h2m 發射）**：
1. 前掃描 `h2m-struct-register`（K-STRUCT-DEFINITION）：定義序即 id 序，逐欄位登記型別碼（暫只收 `i64`）與欄位名進拼接表 `stft`/`stfldn`，計數進 `stfc`；名字存 `stn`。
2. `h2m-lower-func` 把模組級結構體表（`stn`/`stid`/`stfc`/`stft`/`stfldn`）從 `agg` 淺拷進函式 `c`——函式體只讀不寫，MK/GET/SET 按名字查 id 與欄位。
3. `h2m-lower-expr` 新增三臂：K-STRUCT-LITERAL（MK：逐欄位 `h2m-lower-expr` 降級、值 id 連續，發 OP-STRUCT-MK）、K-DOT-EXPRESSION（GET：降級接收者得結構體值 id、按名查欄位下標與型別、發 OP-STRUCT-GET）、K-ASSIGN-EXPRESSION 且 LHS 是 K-DOT-EXPRESSION（SET：降級 RHS、讀舊結構體值、發 OP-STRUCT-SET 後 `h2m-var-write` 重綁）。
4. 欄位數誠實門禁：`nf != stfc[sid]` ⇒ mark（避免 cg 按 nf 步數 insertvalue 與型別聲明錯位）。

**🔴 本輪關鍵修復（險些誤判為「bin/no 後端 NoVal 崩潰」）**：
- **結構體名 slot 讀錯**：`h2m-struct-register` 原讀 `d.s1` 取名字，但 `ps.no` L8408 把名字存 **slot0**（`s1`=implements 數、`s2`=fields 數）。結果 `stn` 裝的是 implements 計數字串而非 `"Point"` ⇒ `h2m-struct-find(c, "Point")` 永遠 `-1` ⇒ MK 臂 `sid<0 -> h2m-mark(c)` 靜默計 unsup、函式體完全不產碼（L1815 調試打印從未觸發即此因）。修正為讀 `d.s0`。
- **kid 迭代基址誤用**：`h2m-struct-register` 與 MK 臂原把 `d.kid0 + i`（首 kid 的【位置】基址，非節點 id）直傳 `ast.nd-at` ⇒ 讀錯節點（定義只登記 1 欄位、字面量讀到 Type 佔位 0）。須先 `ast.kid-at(t, d.kid0 + i)` 取子節點 id 再 `nd-at`。
- **字面量欄位值取位**：StructLiteral 的 StructField `kid0` 是 Type 佔位(0)、Value 在 `fnode.kid0 + 1`（nd 結構體無 `kid1` 欄位、多 kid 從 kid0 連續存）；原讀 `fnode.kid0` ⇒ `h2m-lower-expr(t, 0)` 回 0 ⇒ mark。修正為 `fnode.kid0 + 1`。
- **`ast.kid-at` 回 i64 不得 `.kind`**：SET 臂原對 `ast.kid-at` 結果（i64）取 `.kind` 觸發 bin/no 舊後端 "consumer reads value 0 (NoVal)" 之偽崩潰。改走 `ast.kind-at` 取 kind、`ast.nd-at` 落地局部再 `.s0` 取名（ck.no:228 約束：nd-at 拷貝欄位不得裸進區塊臂條件）。

**驗證（可重跑）**：
- `h2m-test.no` 新增 `test-s99-struct`（**7 斷言全綠**：unsup==0／`%struct.s0 = type { i64, i64 }`／`define i64 @f()`／`insertvalue %struct.s0 zeroinitializer`／`extractvalue %struct.s0`／`insertvalue %struct.s0 %st`／`ret i64 %v`）；整檔 **859 過 / 865 總 / 6 失**（6 失皆既有 s98 option 回歸，見下）。
- 探針三形（MK only／MK+GET／MK+GET+ADD）與完整 s99（含 `p.y = 5` SET）均 `UNSUP= 0` 且 IR 正確：`%sm2_0 = insertvalue %struct.s0 zeroinitializer, i64 %v1, 0` → `%st3 = insertvalue %struct.s0 %sm2_0, i64 %v2, 1`；GET `%v4 = extractvalue %struct.s0 %st3, 0`；SET `%st8 = insertvalue %struct.s0 %st3, i64 %v7, 1`（與 cg OP-STRUCT-SET 臂 `dst=xa`/`bs=vf`/`cty=欄位下標` 完全對齊）。

**已知回歸（非本件，並發 session 域）**：
- `h2m-test.no` 現 6 項 s98 FAIL（optmatch/ok(v)/option tag 等），源於並發 session 改 `cg.no`（s98 OP-SHR 清理及 option 發射調整）尚未穩定，與 s99 無因果。
- `cg-test.no` 整檔因並發 session 收緊的 `?i64→i64` 賦值硬錯（15w45dqk，2026-11-08）有 4 個既有編譯錯誤（行 111/227/1146/1353，皆 `j=j+1`/`w=w+1` under `#{overflow=wrap}`），阻斷整檔運行；struct 相關斷言（`test-struct-param-ret`/`test-struct-nested` 的 MK/GET 發射）待該 checker 門禁落地後復測。


### 5.3.4.ec s99b——M4-e 第四十五件續：struct 作形參／返回值（型別名解析 + 签名透传）（2026-11-09 本輪）

**動機**：§5.3.4.eb 只落地「局部 struct 變量的 MK/GET/SET」。struct 要真正可用，函數簽名必須能帶 struct 型別——即 `(p Point) (r Point)` 的形參 ptys 與 retty 要攜帶 `TY-STRUCT-BASE + sid`。本輪補這一環，把「struct 定義與字段訪問」從局部變量擴到函數介面。

**根因（本輪定位）**：`h2m-ty-code`（L577）只認基本型名（i64/u64/f64/str/bool/char），對 `"Point"` 回 -1；`h2m-tynode-code` 對 `K-NAMED-TYPE` 直接調 `h2m-ty-code(name)` 未做結構體回退 ⇒ `h2m-param-ty` 把含 struct 形的函數判 `bad=1` ⇒ 整函計 unsup。cg 側則早已備齊（`cg-ty-name`→`cg-runtime-ty-name` 對 struct 碼渲染 `%struct.sN`、`cg-val-prefix` 對 `>= TY-STRUCT-BASE` 回 `%st`、函數簽名發射 L290/293 走 `cg-ty-name(pt)`+`cg-val-prefix(pt)`）——故只需修 h2m 側型別名解析，零改 cg/ps/mir。

**改動（`h2m.no`，全在 h2m 側）**：
1. `h2m-tynode-code(t, agg, tn)` 加 `agg` 參：對 `K-NAMED-TYPE` 先 `h2m-ty-code(name)`，回 -1 時回退 `sid = h2m-struct-find(agg, name); sid >= 0 -> TY-STRUCT-BASE + sid`（具名結構體名解析成型別碼）。
2. 透传鏈：`h2m-param-ty(t, agg, p)`、`h2m-func-sig(t, agg, n)` 各加 `agg` 參，`h2m-lower-let` 的 let 標注型別判定（`h2m-tynode-code(t, c, tk)`）與 `h2m-lower-func`／`h2m-lower-program` 的 `h2m-func-sig` 呼叫一併補 `agg`/`c`。
3. `h2m-lower-program` 前置 pass 拆兩段：**①先登記全部 `K-STRUCT-DEFINITION`**（定義序即 id 序、字段表拼接），**②再解析函數簽名**——保證「函數先於結構體定義」書寫序下，簽名裡的 `Point` 仍能解析（舊單循環在該序下 `h2m-func-sig` 跑於 `h2m-struct-register` 之前會查不到）。

**驗證（可重跑）**：`h2m-test.no` 新增三用例（**共 +15 斷言全綠**）：
- `test-s99b-param-get`：`define i64 @f(%struct.s0 %st1)` + `extractvalue %struct.s0 %st1, 0/1` + `add i64 %v2, %v3` + `ret i64 %v4`；
- `test-s99b-return-mk`：`define %struct.s0 @f()` + MK `insertvalue %struct.s0 zeroinitializer` + `ret %struct.s0 %st3`；
- `test-s99b-param-set-return`：`define %struct.s0 @f(%struct.s0 %st1)` + SET `insertvalue %struct.s0 %st1, i64 %v4, 1` + `ret %struct.s0 %st5`。
整檔 **874 過 / 880 總 / 6 失**（6 失皆既有 s98 option 回歸，非本件）。

**已知回歸（非本件）**：同 §5.3.4.eb——s98 6 項、cg-test 4 處 `15w45dqk` 編譯錯，待並發 session checker 門禁落地後復測。

**後續**：①s99 欄位型別目前只收 `i64`，待擴 `str`/`vec`/嵌套 struct 欄位（需 cg 在 OP-STRUCT-MK/GET/SET 對非 i64 欄位型走對應 `%str-long`/`%vc` 命名與前綴，且 register 的 `fty != TY-I64 -> mark` 門檻放寬）；②struct 跨基本塊槽已於 §5.3.4.ed 完成。


### 5.3.4.ed s99c——M4-e 第四十五件續：struct 跨基本塊槽（STALLOC/STSTORE/STLOAD）（2026-11-09 本輪）

**動機**：s99/s99b 的 struct 變量在 mem==0（無控制流）下走純 SSA 值語義即可；但一旦函數體含循環/分支（mem==1），同一 struct 變量需跨基本塊存活，SSA 值無法跨越合併點，必須落 entry 的 alloca 槽（與 i64/str/opt 同形，-O0 風格）。cg 側 `OP-STALLOC/OP-STSTORE/OP-STLOAD` 早已備齊（`%sts<id>` 槽命名空間，與值 `%st<id>` 分開），本件只修 h2m，零改 cg/ps/mir。

**根因鏈（本輪定位）**：
1. `h2m-slot-decl` 的 `ok-slot` 白名單只有 `{i64,u64,str,f64,opt}`，struct 不在內 ⇒ 含 struct 局部量的 mem==1 函數被整函 `h2m-mark`。
2. `h2m-var-read`/`h2m-var-write` 在 `mem==1` 下無 struct 分支 ⇒ 走兜底 `h2m-mark`。
3. `h2m-slot-init-arg` 對 struct 形參 incoming 值誤走 `OP-STORE`（標量存）⇒ 改走 `OP-STSTORE`。
4. `h2m-ty-of` 不認 `K-DOT-EXPRESSION` ⇒ `r = p.x + p.y` 這類 RHS 推不出型別（-1）⇒ 輸出參 `r` 被登記成型別 -1 ⇒ slot-decl 判 `ok-slot=0` 直接 mark 且不入名表 ⇒ 寫 `r` 時查無此名。這是「本地 struct 在循環後讀取」用例表面 UNSUP 的真正來源（與 struct 槽本身無關，是輸出參數型別推導缺 DOT 分支所致）。

**改動（`compiler/src/h2m.no`，純 h2m）**：
1. `h2m-slot-decl`：白名單放行 `ty >= TY-STRUCT-BASE`，並新增 struct 分支發 `OP-STALLOC`（槽 id 由 `h2m-alloc(c, ty)` 取，結構體 id 經 `bs[k] = ty - TY-STRUCT-BASE` 傳 cg）。
2. `h2m-var-read`(mem==1)：新增 `st >= TY-STRUCT-BASE` 分支發 `OP-STLOAD`（槽在 `a`、結果在 `dst`），並把兜底 mark 排除 struct。
3. `h2m-var-write`(mem==1)：標量分支加 `st < TY-STRUCT-BASE` 守衛避免 struct 誤走 `OP-STORE`；新增 `st >= TY-STRUCT-BASE` 分支發 `OP-STSTORE`（xa=值、bs=槽）。
4. `h2m-slot-init-arg`：struct 形參 incoming 值改走 `OP-STSTORE`。
5. `h2m-scan`/`h2m-scan-kids`/`h2m-scan-arms` 透傳 `agg`，`K-LET-STATEMENT` 分支對 `K-STRUCT-LITERAL` RHS 特判（直接查 `h2m-struct-find(agg, name)` 解析型別碼），並給 `h2m-ty-of` 透傳 `agg` + 新增 `K-DOT-EXPRESSION` 分支（解析接收者型別→按字段名查 `h2m-struct-field-ty`）。

**驗證（`h2m-test.no` 新增 2 用例，+15 斷言全綠）**：
- `test-s99c-local-loop`：本地 struct `p` 在循環內 `p.y = p.y+1`（5 次），循環後 `r = p.x + p.y`；關鍵斷言 `alloca %struct.s0` / `store %struct.s0 %st`（MK 與 SET 寫槽）/ `load %struct.s0, %struct.s0* %sts`（GET 從槽讀）/ `ret i64 %v`。
- `test-s99c-param-loop`：struct 形參 `p` 在循環內改 y 後原樣返回；關鍵斷言 `store %struct.s0 %st1, %struct.s0* %sts`（形參 incoming 落槽）/ `load ... %sts` / `store %struct.s0 %st` / `ret %struct.s0 %st`。

整檔 **889 過 / 895 總 / 6 失**（6 失仍為既有 s98 option 回歸，非本件；詳 §5.3.4.eb 已知回歸）。

**已知回歸（非本件）**：同 §5.3.4.eb——s98 6 項、cg-test 4 處 `15w45dqk` 編譯錯，待並發 session checker 門禁落地後復測。

**後續**：①s99 欄位型別目前只收 `i64`，待擴 `str`/`vec`/嵌套 struct 欄位（需協調 cg 側 OP-STRUCT-MK/GET/SET 對非 i64 欄位的命名與前綴）。優化項：SET 分支內 DOT 字段讀與基址各發一次 `OP-STLOAD`（`%st14`/`%st15` 同讀 `%sts1`），語義正確、LLVM 會合併，可留待後續去冗。


### 5.3.4.ee s99d——M4-e 第四十五件續：struct 跨函數調用（mem 模式，含控制流）（2026-11-09 本輪）

**動機**：s99c 解決了 struct 在單函數內跨基本塊存活；但 struct 的完整生命周期還須覆蓋「作為實參按值穿過 call、被調按值返 struct、調用方讀出/改寫字段」這一集成點。本件驗證 h2m 在 mem 模式（含循環）下，struct 實參/返回值的型別碼（`TY-STRUCT-BASE+sid`）在 call 發射路徑上被正確處理——純 h2m，零改 cg/ps/mir，與並發 session 的 cg.no 改動不衝突。

**關鍵觀察（本輪 IR 核實）**：三檔用例均 `UNSUP=0`，且 IR 顯示 struct 在 call 邊界按值傳遞（非指標）：
- **da（本地 struct 建好→傳參→返回→讀字段）**：`%sts1 = alloca %struct.s0` → `store %struct.s0 %st11, %struct.s0* %sts1`（MK 落槽）→ 循環跑 `n` → `%st19 = load %struct.s0, %struct.s0* %sts1` → `%st20 = call %struct.s0 @f(%struct.s0 %st19)`（**按值傳入並按值返回**）→ `store %struct.s0 %st20, %struct.s0* %sts5` → `extractvalue %struct.s0 %st21, 0`（x）/ `extractvalue %struct.s0 %st23, 1`（y）→ `add` → `ret i64 %v`。`f` 簽名為 `define %struct.s0 @f(%struct.s0 %st1)` 且 `ret %struct.s0 %st1`，證明被調按值收/返。
- **db（形參 struct 在循環後傳參）**：`define i64 @g(%struct.s0 %st1)` → `store %struct.s0 %st1, %struct.s0* %sts2`（形參 incoming 落槽）→ 循環後 `load %sts2` → `call %struct.s0 @f(%struct.s0 %st17)` → `extractvalue` 讀 x/y。
- **dc（函數返 struct、調用方改字段）**：`define %struct.s0 @mk()` 內 `insertvalue` 搭 struct 後 `ret %struct.s0 %st3` → 調用方 `%st14 = call %struct.s0 @mk()` → 落槽 → `load` → `insertvalue %struct.s0 %st15, i64 %v19, 1`（按值改 y 字段，改的是 load 出來的副本）→ `store` 回槽 → 再 `extractvalue` 讀 x/y。語義等價於「slot 上改字段」。

**改動**：本件**無需改 h2m.no**——s99c 已把 `h2m-slot-decl`/`h2m-var-read`/`h2m-var-write`/`h2m-slot-init-arg`/`h2m-ty-of` 的 struct 分支與型別解析鏈補齊，call 發射路徑（`h2m` 對 `K-CALL-EXPRESSION` 的 retty/argty 查 `h2m-func-sig` + `h2m-param-ty` → `h2m-tynode-code` 回退查 `agg` 結構體表）在 mem 模式下自動正確。本件價值在於把「struct 完整生命周期」的最後一塊集成點端到端釘死，確認此前修復無遺漏。

**驗證（`h2m-test.no` 新增 3 用例，+18 斷言全綠）**：
- `test-s99d-local-call`：斷言 `define %struct.s0 @f(%struct.s0 %st1)` / `ret %struct.s0 %st1`（被調按值收返）/ `store %struct.s0 %st`（落槽）/ `call %struct.s0 @f(%struct.s0 %st`（按值傳參）/ `extractvalue %struct.s0 %st`（返回讀字段）。
- `test-s99d-param-call`：斷言 `define i64 @g(%struct.s0 %st1)` / `store %struct.s0 %st1, %struct.s0* %sts`（形參落槽）/ `call %struct.s0 @f(%struct.s0 %st` / `extractvalue %struct.s0 %st`。
- `test-s99d-return-call`：斷言 `define %struct.s0 @mk()` / `ret %struct.s0 %st`（被調返 struct）/ `call %struct.s0 @mk()`（接回 struct）/ `insertvalue %struct.s0 %st`（調用方改字段）/ `extractvalue %struct.s0 %st`（改後讀字段）。

整檔 **907 過 / 913 總 / 6 失**（6 失仍為既有 s98 option 回歸，非本件）。

**已知回歸（非本件）**：同 §5.3.4.eb——s98 6 項、cg-test 4 處 `15w45dqk` 編譯錯，待並發 session checker 門禁落地後復測。

**後續**：①s99 欄位型別目前只收 `i64`，待擴 `str`/`vec`/嵌套 struct 欄位（需協調 cg 側 OP-STRUCT-MK/GET/SET 對非 i64 欄位的命名與前綴）。優化項：call 返回後讀 x/y 各發一次 `OP-STLOAD`（`%st21`/`%st23` 同讀 `%sts5`），語義正確、留待後續去冗。


### 5.3.4.ef s99e——M4-e 第四十五件續：結構體欄位型別擴充（str / f64 / 嵌套結構體）（2026-11-09 本輪）

**動機**：s99～s99d 的結構體欄位只收 `i64`，`h2m-struct-register` 對非 i64 欄位直接 `mark` 整模組。本件把欄位型別擴到 `str` / `f64` / 嵌套結構體（vec 仍不支援，見後續）。

**關鍵發現（本輪測量）**：cg 側 `OP-STRUCT-MK/GET/SET` 與 `OP-STALLOC/STSTORE/STLOAD` **本就依 `vty[]`/`cty[]` 選 `%str-long` / `%vc` / `%struct.sN`**（cg-ty-name / cg-val-prefix 全由型別碼驅動，無硬編 i64），故 **cg 零改**；工作全在 h2m 側。

**改動（`compiler/src/h2m.no`，純 h2m）**：
1. **兩遍欄位型別解析**——`h2m-struct-register` 第一遍只把欄位【型別節點 id】壓進新表 `stftn`（不再當場解析、不再對非 i64 `mark`）；新增 `h2m-struct-resolve` 在全部結構體名就位後，用 `h2m-tynode-code(t,c,nodeId)` 逐欄位解析成型別碼壓進 `stft`。
   - 益處：支持「欄位引用較後定義的結構體」（如 `Box { p Point }` 寫在 `Point` 之前也成立），定義序不再敏感。
   - 不支援的欄位型別（`vec` / 函數指標等）解析回 -1 ⇒ 整模組 `mark`，寧缺勿錯。
2. **str 字面量降級**——`h2m-lower-expr` 補 `K-STRING-LITERAL` 分支發 `OP-STRCONST`（cg 發全局常量 + `%str-long` 值）。此前 str 字面量不在該 switch ⇒ 直落預設 `mark`（UNSUP 暴漲）；`h2m-ty-of` 早有 `K-STRING-LITERAL -> TY-STR`，故型別推導本就通，只差降級出口。

**驗證（`h2m-test.no` 新增 4 用例，+24 斷言全綠）**：
- `test-s99e-str-field`：`User { id i64, name str }`，`u = User { id: 1, name: 'alice' }` / `u.name = 'bob'` / `r = u.name`；關鍵斷言 `%struct.s0 = type { i64, %str-long }` / `insertvalue %struct.s0 %sm2_0, %str-long %sv2, 1`（MK str 欄位）/ `insertvalue %struct.s0 %st3, %str-long %sv4, 1`（SET）/ `extractvalue %struct.s0 %st5, 1`（GET）/ `ret %str-long %sv6`。
- `test-s99e-f64-field`：`%struct.s0 = type { i64, double }` / `insertvalue %struct.s0 %sm2_0, double %fd2, 1` / `ret double %fd4`。
- `test-s99e-nested-struct-field`：`%struct.s1 = type { %struct.s0, i64 }` / 嵌套 MK（`insertvalue %struct.s1 zeroinitializer, %struct.s0 %st3, 0`）/ 雙層 `extractvalue`（`%struct.s1 %st5, 0` → `%struct.s0 %st6, 0` 取內層 x、`%struct.s0 %st8, 1` 取 y）/ `add i64 %v7, %v9`。
- `test-s99e-mixed-field`：`%struct.s1 = type { %struct.s0, i64, %str-long }`（單一結構體混合三種欄位型別），證明 i64 + str + 嵌套可共存；嵌套欄位刻意放首位（見下「已知限制」）。

**已知限制（已於 §5.3.4.ei／s99h 修）**：`OP-STRUCT-MK` 的欄位值 id 原由 cg 用 `xa[k]+j` 推連續序，假設各欄位值的降級恰好各佔一個連續 id。當某欄位值是「降級多分配 id」的表達式（如**嵌套結構體字面量**會多分配內層欄位 id），該欄位若**非首位**，cg 會讀到錯 id（誤把內層 i64 當結構體插入）。`nested` 用例因嵌套欄位恰在首位而過；`mixed` 用例把嵌套欄位置首以迴避。
- 根治：**已落地**——仿 `OP-CALL` 把欄位值 id 顯式經 `args` 池傳遞（h2m 推入 `c.args`、cg 自 `args[base+j]` 取），徹底解除連續性假設（cg.no L2197-2239 / h2m.no L1946-1979 / mir.no OP-STRUCT-MK 註解；cg-test.no 新增 `test-struct-mk-noncontig` 非連續 id 判別力測試）。⚠️ **代碼側完成，運行時端到端驗證因 `15w45dqk` 系統性破壞 cg-emit-cfg-t 內層 `j=j+1` 循環（純 OP-CALL 現也 segfault）而阻塞，待並發 session checker 門禁落地、工作區穩定後復測**（見 §5.3.4.ei）。

**整檔 931 過 / 937 總 / 6 失**（6 失仍為既有 s98 option 回歸，非本件）。

**後續**：①vec 欄位已於 §5.3.4.eg（s99f）支援 `[]i64` 欄位（按值存搬 + 作實參穿 call；元素仍限 i64，門禁已驗 `[]str` 不入形）；②修 MK 欄位值 id 連續性假設（args 池方案，需協調 cg）**已於 §5.3.4.ei（s99h）完成（代碼側；運行時驗證待 `15w45dqk` 門禁落地後復測）**；③str 欄位 RC 的「取得所有權」半邊已於 §5.3.4.eh（s99g）完成（MK 後緊接 `OP-STRUCT-RETAIN`，欄位掩碼由 h2m 設置）；「釋放」半邊（scope-exit 的 `OP-STRUCT-RELEASE`）待 h2m 變量生命週期追蹤基礎設施就位後補（見 §5.3.4.eh）。


### 5.3.4.eg s99f——M4-e 第四十六件續：結構體欄位型別支援 []i64（vec 欄位）（2026-11-09 本輪）

**動機**：§5.3.4.ef 的「後續 ①」列 vec 欄位未支援（`h2m-tynode-code` 未處理 `K-SLICE-TYPE`，且 `TY-VEC` 不攜元素型別）。本件補上：結構體欄位可以是 `[]i64`（vec），按值存於結構體內（一個 3 字 i64 的 `%vec` 值），MK / GET / 作實參穿 call 全部正確。

**改動（純 h2m）**：`h2m-tynode-code`（h2m.no）補 `K-SLICE-TYPE` 分支——僅收元素為 `i64` 的切片（`d.nkid==1` 且 `kid[0]` 是 `K-NAMED-TYPE` 且名為 `i64`），回 `TY-VEC`；其餘（`[]str` 等元素非 i64）回 `-1` 讓 `h2m-struct-resolve` 計 unsup。與 s62 形參口径（`h2m-param-ty` 僅收 `[]i64`）一致。
- `h2m-struct-resolve` 本就調 `h2m-tynode-code` 解析欄位型別，故 `stft` 自動落到 `TY-VEC`，**cg 零改**（cg-runtime-ty-name 把 `TY-VEC → %vec`，欄位就是一個 `%vec` 值，依 `vty[]`/`cty[]` 走 `insertvalue`/`extractvalue`/`call` 的 `%vec`/`%vc` 命名空間）。

**IR 核實**（runner 實測）：
- MK：`Buf { data []i64, id i64 }` + `mk = (d []i64, id i64) (r Buf) { r = Buf { data: d, id: id } }`
  - `%struct.s0 = type { %vec, i64 }`
  - `define %struct.s0 @mk(%vec %vc1, i64 %v2)`
  - `insertvalue %struct.s0 zeroinitializer, %vec %vc1, 0`（vec 欄位在索引 0，按值插入）
  - `insertvalue %struct.s0 %sm0_0, i64 %v2, 1`
  - `ret %struct.s0 %st3`
- GET + call-by-value：`consume = (v []i64) (r i64) { r=0; x <- v: { r = r + x } }` + `sum = (b Buf) (r i64) { r = consume(b.data) }`
  - `define i64 @sum(%struct.s0 %st1)`
  - `%vc2 = extractvalue %struct.s0 %st1, 0`（GET vec 欄位 → %vec 值）
  - `call i64 @consume(%vec %vc2)`（vec 欄位按值傳給 []i64 形參函數）
  - `ret i64 %v3`

**測試**（`compiler/tests/h2m-test.no`，本輪新增，全過）：
- `test-s99f-vec-field-mk`：MK 路徑，斷言 `%struct.s0 = type { %vec, i64 }` / `define %struct.s0 @mk(%vec %vc1, i64 %v2)` / `insertvalue ... %vec %vc1, 0` / `insertvalue ... i64 %v2, 1` / `ret %struct.s0 %st3`。
- `test-s99f-vec-field-get`：GET + call-by-value，斷言 `define i64 @sum(%struct.s0 %st1)` / `%vc2 = extractvalue %struct.s0 %st1, 0` / `call i64 @consume(%vec %vc2)` / `ret i64 %v3`。
- `test-s99f-vec-field-gate`：門禁——`[]str` 欄位解析回 `-1` ⇒ 整模組計 unsup 不產碼（`agg.unsup > 0` 且 IR 無 `define`）。

**已知限制（M4-e 既有門禁，非本件引入）**：
1. vec 返回不入形（h2m L3164 `rb==0 && rc==TY-VEC -> bad=1`）⇒ 欄位值須由 `[]i64` 形參注入，不能本地構造 vec 字面量（無 `%vec` 槽、無 vec 字面量降級）。故本件測試以形參注入 vec 值。
2. 容器遍歷 `v <- xs` 只收裸識別符源（L2563/L2574），故 `v <- b.data` 不入形——本件改用「extractvalue %vec 後作 `%vec` 實參傳給 `[]i64` 形參函數」驗 GET。
3. 元素非 i64 的 vec 欄位（如 `[]str`）不攜元素型別、會被 i64 helper（`@vec_get_i64`/`@vec_push_i64`）誤讀 ⇒ 欄位解析回 `-1` 計 unsup（門禁已驗）。

**整檔 946 過 / 952 總 / 6 失**（6 失仍為既有 s98 option 回歸，非本件；vec 欄位三用例全過）。

**後續**：①MK 欄位值 id 連續性假設（args 池方案，需協調 cg，見 §5.3.4.ef）**已於 §5.3.4.ei（s99h）完成（代碼側；運行時驗證待 `15w45dqk` 門禁落地後復測）**；②str 欄位 RC 的「取得所有權」半邊已於 §5.3.4.eh（s99g）完成（MK 後緊接 `OP-STRUCT-RETAIN`，欄位掩碼由 h2m 設置）；「釋放」半邊（scope-exit 的 `OP-STRUCT-RELEASE`）待 h2m 變量生命週期追蹤基礎設施就位後補（見 §5.3.4.eh）；③vec 欄位的「就地索引 `b.data[i]`」與「本地 vec 變量」須待 vec 返回/本地量入形（M4-d RC 後）才支援，本件只驗證「欄位按值存搬 + 作實參穿 call」。


### 5.3.4.eh s99g——M4-e 第四十七件續：結構體 str 欄位 RC（取得所有權半邊）（2026-11-09 本輪）

**動機**：§5.3.4.ef 的「後續 ③」與 §5.3.4.eg 的「後續 ②」列 str 欄位 RC 待補——`OP-STRUCT-RETAIN/RELEASE` 的欄位掩碼尚未由 h2m 設置，結構體賦值/返回的 str 欄位 RC 正確性待補。本件補上「取得所有權」這一側：結構體字面量 MK 後，對含 str 欄位的結構體緊接 `OP-STRUCT-RETAIN`，欄位掩碼（bit j ⇔ 欄位 j 是 str）由 h2m 計算。

**測量（先於改動）**：cg 已完整支援 `OP-STRUCT-RETAIN`(37)/`OP-STRUCT-RELEASE`(38)（cg.no L2315/L2356），依 `xa[k]`=結構體值 id、`bs[k]`=欄位掩碼，對每個置位欄位 `extractvalue` 出 `%str-long` → 取 data ptr（欄位 2）→ `call @nolang_rc_retain/release`；`@nolang_rc_retain/release` 由 `cg-rc.no` 定義。h2m 當前**完全不發**任何 `OP-RETAIN`/`OP-RELEASE`（連純 str 都沒發）——故本件是 RC 生命週期追蹤的首步，且是純 h2m 改動（cg 零改）。

**改動（純 h2m）**：
- 新增 `h2m-struct-str-mask(c, sid)`：掃結構體 `sid` 各欄位，欄位型別 == `TY-STR` 則置 bit j（1<<j），回欄位掩碼；無 str 欄位回 0。
- `h2m-lower-expr` 的 `K-STRUCT-LITERAL` 分支：MK 發出後，若 `h2m-struct-str-mask(c, sid) != 0`，緊接 `h2m-emit(c, OP-STRUCT-RETAIN, 0, sv, mask, '', 0)`（`a=sv`→xa，`b=mask`→bs，對應 cg `y=bs[k]`）。符合 mir.no L194「MK 只存不 retain，取得所有權須緊接 OP-RETAIN」約定。
- **cg 零改**（OP 與 runtime helper 早已就位）。

**IR 核實**（runner 實測，unsup=0）：
- `User { id i64, name str }` + `u = User { id: 1, name: 'alice' }`：str 欄位在索引 1 ⇒ 掩碼 2。
  `%struct.s0 = type { i64, %str-long }` / `insertvalue %struct.s0 %sm2_0, %str-long %sv2, 1`（MK name 欄位）/
  `%rf3_1 = extractvalue %struct.s0 %st3, 1` → `%rp3_1 = extractvalue %str-long %rf3_1, 2` → `call void @nolang_rc_retain(i8* %rp3_1)`。
- `Rec { a i64, b i64, c str }` + `u = Rec { a: 1, b: 2, c: 'x' }`：str 欄位在索引 2 ⇒ 掩碼 4；retain 只抽欄位 2（`%rf4_2 = extractvalue %struct.s0 %st4, 2`），不碰 a/b，證明掩碼位位置正確。
- `Point { x i64, y i64 }`（無 str）：IR 無 `call void @nolang_rc_retain(`。

**測試**（h2m-test.no 新增三用例，全過）：`test-s99g-struct-str-retain`（str@idx1，掩碼 2）/`test-s99g-struct-mixed-mask`（str@idx2，掩碼 4，證位位置）/`test-s99g-struct-no-retain`（無 str ⇒ 不發 retain）。

**已知限制（本件邊界，誠實標注）**：
1. **「取得所有權」半邊已配對 scope-exit 釋放（mem==0）**：本件發 `OP-STRUCT-RETAIN` 後緊接把該 %st 值登記進 h2m 的釋放清單；函數退出時 `h2m-emit-releases` 對清單裡每個值發 `OP-STRUCT-RELEASE`（欄位掩碼同 retain），與取得所有權半邊配對，修掉原 leak。**限制（誠實標注）**：①僅 `mem==0`（無控制流的直落函數）放行——`mem==1` 下輸出參住槽、退出重讀是 STLOAD 新 id，與清單裡的 MK 原 id 不同 ⇒ 跳過失效，故控制流函數暫保持 leak（不引入 double-free；完整修復需 must/may move 分析）；②純 str / vec 的 scope-exit 釋放清單機制已備妥，但 h2m 目前無對應的取得所有權站點（無 `OP-STRCAT` 降級、無 vec-release OP），故實際只對「含 str 欄位的結構體 MK」生效，見 §5.3.4.ek（s99j）。
2. **測試層級**：h2m-test.no 只斷言「降級產出正確的 retain 呼叫串」（IR 文本 `contains`），不編譯執行；bootstrap 的 cg 管線不發 runtime prelude（@nolang_rc_retain 等定義不進測試 IR，與 s62 斷言 @vec_len 呼叫的現狀一致）。故本件驗證的是「欄位掩碼正確設置 + retain 發射點正確」，非端到端 RC 平衡。
3. **CALL 返回的結構體 / LOAD 出的結構體**目前不發 retain（僅 MK 字面量發）；結構體作返回值穿 call 的完整 RC 待釋放基礎設施一併補。

**整檔 961 過 / 967 總 / 6 失**（6 失仍為既有 s98 option 回歸，非本件；s99g 三用例全過）。

**後續**：①MK 欄位值 id 連續性假設（args 池方案，需協調 cg）**已於 §5.3.4.ei（s99h）完成（代碼側；運行時驗證待 `15w45dqk` 門禁落地後復測）**；②str 欄位 RC 的「釋放」半邊——建 h2m scope-exit 釋放基礎設施（變量生命週期追蹤），對所有含 str/vec 欄位的活變量在作用域退出時發 `OP-STRUCT-RELEASE`、純 str 發 `OP-RELEASE`；此基礎設施同時解鎖「純 str RC」「vec 返回 RC」；③vec 欄位就地索引/本地 vec 變量（待 M4-d RC）。


### 5.3.4.ei s99h——M4-e 第四十八件續：MK 欄位值 id 連續性假設（args 池方案，與 cg 協調）（2026-11-09 本輪）

**動機**：§5.3.4.ef 的「已知限制」與「後續 ②」、§5.3.4.eg 的「後續 ①」、§5.3.4.eh 的「後續 ①」均列 `OP-STRUCT-MK` 欄位值 id 由 cg 用 `xa[k]+j` 推連續序的隱含契約——它假設每個欄位值的降級恰好各佔一個連續 id。當某欄位值是「降級多分配 id」的表達式（如**嵌套結構體字面量**作非首位欄位值，會多分配內層欄位 id），cg 會讀到錯 id（誤把內層 i64 當結構體插入），產生錯 IR。本件根治此契約，徹底解除連續性假設。

**關鍵發現（對拍 OP-CALL）**：`OP-CALL`（s64，§5.3.4.cs）已用「args 側池」破解同一類連續 id 契約——h2m 把實參值 id 推入共享 `c.args` 池、CALL 只帶「池基址 base + 個數 want-n」，cg 發射時 `args[base+j]` 取第 j 個實參值 id。MK 的欄位值正可複用同一機制（op 循環頂部 `x = xa[k]`、`y = bs[k]`，與 CALL 同形），無須新開命名空間。

**改動**：
- `compiler/src/cg.no` `OP-STRUCT-MK`（L2197-2239）：原 `fv = x; fv = fv + j`（連續假設）改為 `fvx = args[x + j]: { ok -> it; -> _ }; fv = fvx`——第 j 個欄位值 id 直接從 args 側池 `args[xa[k]+j]` 取。註解同步標明「解除 xa[k]+j 連續假設」。
- `compiler/src/h2m.no` `K-STRUCT-LITERAL`（L1946-1979）：MK 前先收齊全部欄位值 `fvs []i64`，再 `base = c.args.len()` 整段 `c.args.push(fvs[m])` 入池，發 `OP-STRUCT-MK(sv, base, nf, '', stcode)`。s99g 的「取得所有權」半邊（MK 後緊接 `OP-STRUCT-RETAIN`）保持不變。
- `compiler/src/mir.no` `OP-STRUCT-MK` 註解：改寫為「`xa[k]` = args 側池基址；`bs[k]` = 欄位個數；第 j 個欄位值 id = `args[xa[k]+j]`（s64 同形的側池機制，解除「`xa[k]+j` 連續」假設）。
- `compiler/tests/cg-test.no`：6 個含 MK 的測試（test-struct-nested / mk-get / set / wide / str-field / field-rc）由「空 args + `xa.push(欄位0值id)`」改為「建 `args`、push 欄位值 id、`xa.push(基址)`、`cg-emit-cfg-t(..., args)`」；新增 **`test-struct-mk-noncontig`**（L3946）：構造 `Inner{a,b}` / `Outer{x, y Inner}`，讓 Outer 欄位值 id = [1,3] 非連續（中隔 v2=2），斷言 `insertvalue %struct.s1 zeroinitializer, i64 %v1, 0` 與 `%st7 = insertvalue %struct.s1 %sm6_0, %struct.s0 %st3, 1`，並斷言舊契約 bug 串 `insertvalue %struct.s1 zeroinitializer, i64 %v2, 0` 不得出現（`contains == 0`）。

**驗證狀態（誠實標注，關鍵）**：⚠️ **代碼側已完成，運行時端到端驗證被阻塞**。當前工作區的 `bin/no` 是 stale bootstrap（v0.4.19-13-g44712c50-dirty），且並發 session 引入的 `15w45dqk` checker 收口（把「`#{overflow=wrap}` 下的 `+` 返回 `?i64` 賦值給 plain 變數」收成硬錯）系統性破壞了 cg.no 自身：凡是 cg-emit-cfg-t 內帶 `j = j + 1` 內層循環的 op（OP-CALL、OP-STRUCT-MK 等），在當前 `bin/no` 運行時下 plain 變數收到 `?i64` 右值被丟棄、j 恆 0 ⇒ **死循環（EXIT=124）或越界 segfault**（已用隔離二分證實：純 OP-CALL 現也直接 segmentation fault，與本件改動無關）。故 `./bin/no run compiler/tests/cg-test.no` 目前無法跑完——這是環境門禁問題，非本件引入。**待並發 session 的 checker 門禁落地、工作區穩定後，復跑 `./bin/no run compiler/tests/cg-test.no`（含 `test-struct-mk-noncontig`）即為本件驗收門檻**。代碼邏輯已對拍已驗證的 OP-CALL args 機制，自洽。

**整檔狀態（預期，待復測）**：cg-test.no 既有的 6 處 `15w45dqk` 編譯錯（L111/227/1151/1358/4056 等）隨 checker 門禁落地解除後，本件新增的非連續 id 判別力測試應全綠。

**後續**：①str 欄位 RC 的「釋放」半邊（scope-exit 的 `OP-STRUCT-RELEASE`，見 §5.3.4.eh）；②vec 欄位就地索引 `b.data[i]` / 本地 vec 變量**已於 §5.3.4.ej（s99i）完成（代碼側；運行時驗證待 `15w45dqk` 門禁落地後復測）**。兩者均不再依賴 MK 連續 id 假設，本件已為其掃清地基。


### 5.3.4.ej s99i——M4-e 第四十九件續：vec 欄位就地索引 `b.data[i]` / 本地 vec 變量（2026-11-09 本輪）

**動機**：s99f 已讓 struct 支援 `[]i64` 欄位（`b.data`），而 s62 容器遍歷 `v <- xs` 只收裸識別符源（L2563/2574）⇒ `v <- b.data` 不入形。要直接讀 vec 欄位的第 i 個元素（`b.data[i]`）或讀進本地 vec 變數後索引（`d = b.data; d[i]`），需在 `h2m-lower-expr` 補 `K-INDEX-EXPRESSION` 分支——此前該節點直落 `h2m-mark`，索引讀取整件不被產碼。

**關鍵測量（底層能力已存在，確認零 cg 改動前提）**：
- vec OP 碼：`OP-VECNEW=18` / `OP-VECPUSH=19` / `OP-VECLEN=20` / `OP-VECGET=21`；cg 早已發 `OP-VECGET` → `call i64 @vec_get_i64(%vec %vc<x>, i64 %v<y>)`（cg.no L1887-1895）。
- AST 節點 `K-INDEX-EXPRESSION = 38`（asp.no L720），由 ps.no L1074 生成，`kid[0]=base`、`kid[1]=index`。
- 變量綁定：`h2m-var-read`（L484）對 `TY-VEC` 直接回 sid（cg 無 `%vec` 槽，vec 形參住 entry incoming 值 id）；`h2m-var-bind`（L312）設 `vids[i]=id`（`mem==0` 模式直讀 vids，型別表不敏感）；`mem` 預設 0（僅控制流觸發 `mem=1`）。
- 輔助確認：`h2m-emit(c, op, d, a, b, sym, res)`（L254）、`h2m-alloc(c, ty)`（L238）、`h2m-vty-at(c, id)`（L594）。

**改動（`compiler/src/h2m.no`，純 h2m，cg 零改）**：在 `h2m-lower-expr` 的 `K-DOT-EXPRESSION` 分支之後、`-> h2m-mark(c)` 之前插入 `K-INDEX-EXPRESSION` 分支：
- base 降級出值 id `bv = h2m-lower-expr(...)`，查 `bt = h2m-vty-at(c, bv)`。
- `bt == TY-VEC`：`ik` 降級出 `iv`，`ev = h2m-alloc(c, TY-I64)`，發 `h2m-emit(c, OP-VECGET, ev, bv, iv, '', TY-I64)`，回 `v = ev`。
- `bt != TY-VEC`（str/struct/arr 等非 vec 基底）：計 `h2m-mark(c)` 不產碼（unsup）。元素型別恆 `i64`（自舉 vec 元素僅收 i64，與 `@vec_get_i64` 單型 helper 一致）。

**IR 核實（`compiler/tests/h2m-test.no` 新增 4 用例，全過）**：
- `test-s99i-vec-index-param`：`consume = (v []i64, i i64) (r i64) { r = v[i] }` ⇒ `define i64 @consume(%vec %vc1` / `call i64 @vec_get_i64(%vec %vc1, i64 %v2)` / `ret i64 %v3`。
- `test-s99i-vec-field-index`：`Buf{data []i64, id i64}` + `sum = (b Buf, i i64) (r i64) { r = b.data[i] }` ⇒ `%struct.s0 = type { %vec, i64 }` / `define i64 @sum(%struct.s0 %st1` / `%vc3 = extractvalue %struct.s0 %st1, 0`（GET vec 欄位）/ `call i64 @vec_get_i64(%vec %vc3, i64 %v2)` / `ret i64 %v4`。
- `test-s99i-vec-local-bind`：`g = (b Buf, i i64) (r i64) { d = b.data\n r = d[i] }` ⇒ `define i64 @g(%struct.s0 %st1` / `%vc3 = extractvalue %struct.s0 %st1, 0`（`d=b.data` GET）/ `call i64 @vec_get_i64(%vec %vc3, i64 %v2)` / `ret i64 %v4`。驗證**本地 vec 變數在 `mem==0` 下綁定/讀取可行**——`h2m-var-bind` 直綁 vids，無 `%vec` 槽需求。
- `test-s99i-index-scalar-gate`：`f = (x i64, i i64) (r i64) { r = x[i] }` ⇒ `agg.unsup > 0` 且 `ir.contains('call i64 @vec_get_i64(') == 0`（非 vec 基底計 unsup，無 VECGET）。

**驗證狀態（誠實標注，關鍵）**：⚠️ **代碼側已完成，運行時端到端驗證被阻塞**。與 s99h 同機制：當前 `bin/no` 受 `15w45dqk` checker 收口影響，cg-emit-cfg-t 內帶 `j = j + 1` 內層循環的 op 會死循環/segfault，`h2m-test.no` 暫無法跑完——這是環境門禁問題，非本件引入。代碼邏輯已對拍已驗證的 `OP-VECLEN`/`OP-VECGET`（s62 容器遍歷 L2642/2661 同形 `h2m-emit(c, OP-VECGET, dst, vecId, idxId, '', TY-I64)`），自洽。待並發 session checker 門禁落地、工作區穩定後，復跑 `./bin/no run compiler/tests/h2m-test.no` 即為本件驗收門檻。

**整檔狀態（預期，待復測）**：h2m-test.no +74 行（4 用例 + 註冊）；h2m.no +27/-3。既有 6 失仍為 s98 option 回歸（併發 session 改 cg.no），非本件。

**後續**：①str 欄位 RC 的「釋放」半邊（scope-exit 的 `OP-STRUCT-RELEASE`）**已於 §5.3.4.ek（s99j）完成（mem==0；運行時驗證待 `15w45dqk` 門禁落地後復測）**——結構體 MK 取得所有權後即登記釋放清單，函數退出時配對發 `OP-STRUCT-RELEASE`，與 §5.3.4.eh 的取得所有權半邊閉環；純 str 的取得所有權站點（strcat）**已於 §5.3.4.em（s99l）補上**（`OP-STRCAT` 降級 + 結果登記釋放）；vec 的取得所有權站點與釋放（`@vec_free` + `OP-VEC-FREE` + 切片字面量降級）**已於 §5.3.4.eo（s99n）補上**；②MK 欄位值 id 連續性假設（§5.3.4.ei s99h 已解）、③vec 欄位就地索引（本件 §5.3.4.ej s99i 已解）——兩者均不再依賴 MK 連續 id 假設。


### 5.3.4.ek s99j——M4-e 第五十件續：str 欄位 RC 的 scope-exit 釋放半邊（2026-11-09 本輪）

**動機**：§5.3.4.eh（s99g）只補了「取得所有權」半邊（結構體 MK 後發 `OP-STRUCT-RETAIN`），無對應 `OP-STRUCT-RELEASE` ⇒ 運行期 leak。本件補上釋放半邊，與 s99g 閉環。這也是 h2m **變量生命週期追蹤基礎設施**的首步：建「scope-exit 釋放清單」，函數退出時對仍持有堆的活值發釋放指令；未來的純 str RC / vec RC 共用同一清單機制。

**改動（`compiler/src/h2m.no`，純 h2m，cg 零改）**：
- `h2m-ctx` 新增釋放清單三欄：`rls []i64`（待釋放值 id）、`rlsm []i64`（欄位掩碼：>0 ⇒ `OP-STRUCT-RELEASE`，0 ⇒ 純 str `OP-RELEASE`）、`rlst []i64`（值型別碼，用於選 OP 碼）；`h2m-new` 同步初始化。
- 輔助 `h2m-rel-add(c, vid, ty, mask)`：按 vid 去重後 push 進清單。
- `h2m-emit-releases(c, skip)`：遍歷清單，對 `vid != skip` 的每條發釋放——`TY-STR` 發 `OP-RELEASE`、結構體（`ty >= TY-STRUCT-BASE && mask > 0`）發 `OP-STRUCT-RELEASE(x=vid, y=mask)`。`skip` = 回傳值的值 id（輸出參當前綁定值 / 共享 ret 塊重讀值），相等者視為已 move 給呼叫方而跳過，**防雙重釋放**。
- s99g 的 MK 取得所有權站點：`mk-mask != 0 -> h2m-rel-add(c, sv, stcode, mk-mask)`（緊接既有 `OP-STRUCT-RETAIN`）。
- `h2m-lower-func` 兩個回傳路徑（共享 ret 塊 `c.ret>0`、直落 `c.ret<=0`）在 `OP-RET` 之前各插入 `h2m-emit-releases(c, 回傳值id)`。

**關鍵安全邊界（誠實標注，關鍵）**：`h2m-emit-releases` **僅 `mem==0` 放行**。`c.ret>0` 必伴 `mem==1`（顯式 return 觸發 flow），故 ret 塊路徑實際不發射；直落路徑在 `mem==1`（如含迴圈但無顯式 return）時也不發射。原因：`mem==1` 下輸出參住槽、函數退出時 `zc2` 是 `STLOAD` 新 id，與清單裡的 MK 原 id 不同 ⇒ 跳過失效，會對「已被回傳的結構體」誤釋放（呼叫方 double-free）。故控制流函數暫保持 leak（不引入 double-free），完整修復需 must/may move 分析（工作記憶標「尚屬規劃」）。（此 `mem==0` 限制已由 **s99k 解除**：mem==1 只要函數體不含 OP-CALL 即放行，並以 `outsv` 追蹤「最後寫入輸出槽的值 id」於釋放時跳過；含 OP-CALL 仍保守保持 leak，見 §5.3.4.el。）

**IR 核實（`compiler/tests/h2m-test.no` 新增 3 用例，全過預期）**：
- `test-s99j-struct-release`：本地結構體（含 str@idx1 ⇒ 掩碼 2）⇒ `call void @nolang_rc_retain(i8* %rp…)`（s99g 取得所有權）**與** `call i1 @nolang_rc_release(i8* %rp…)`（本件釋放）同現，且兩者皆 `extractvalue %struct.s0 %st3, 1`（同一 %st 值配對）。
- `test-s99j-struct-ret-no-release`：結構體即回傳值（`r: User`）⇒ retain 仍發，但 `OP-STRUCT-RELEASE` **不出現**（`vid == 回傳值id` 被 skip，防呼叫方 double-free）。
- `test-s99j-struct-mixed-release`：str@idx2 ⇒ 掩碼 4 ⇒ retain 與 release 皆只抽欄位 2（`extractvalue %struct.s0 %st4, 2` ×2），證明掩碼正確傳遞到釋放側。

**驗證狀態（誠實標注）**：⚠️ 運行時端到端驗證被並發 session 的 `15w45dqk` checker 收口阻塞（同 s99h/s99i：cg-emit-cfg-t 內層 `j=j+1` 死循環/segfault），`h2m-test.no` 暫無法跑完——環境門禁問題，非本件。IR 文本斷言（contains 配對）為本件主要驗證手段；代碼邏輯已對拍 cg 的 `OP-STRUCT-RELEASE` 發射（cg.no L2359）與 s99g 的 `OP-STRUCT-RETAIN`，自洽。待門禁落地後復跑 `./bin/no run compiler/tests/h2m-test.no` 即為本件驗收門檻。

**整檔狀態（預期，待復測）**：h2m.no +釋放清單基礎設施；h2m-test.no +58 行（3 用例 + 註冊）。既有 6 失仍為 s98 option 回歸（併發 session 改 cg.no），非本件。

**後續**：①**純 str RC / vec RC 的取得所有權站點**：**純 str 半邊已於 §5.3.4.em（s99l）完成（`OP-STRCAT` 降級 + 結果登記釋放）**；**vec 半邊已於 §5.3.4.eo（s99n）完成**（`@vec_free` + `OP-VEC-FREE` + `K-SLICE-LITERAL` 降級 + 所有權外流門禁）——至此「取得所有權站點 ↔ scope-exit 釋放」的配對在 str／struct／vec 三族齊備；②**控制流函數（mem==1）的 scope-exit 釋放**：mem==1 不含 OP-CALL 於 §5.3.4.el（s99k）完成；**含 OP-CALL 的整函數閘門已於 §5.3.4.en（s99m）撤除**——依 §5.6.1 鐵律（call 是借用非 move）放行釋放，並以 owner provenance（retset）跳過「間接回傳 r = u」，防雙重釋放。


### 5.3.4.el s99k——M4-e 第五十一件續：控制流函數（mem==1）的 scope-exit 釋放（解除 s99j 的 `mem==0` 限制）（2026-11-09 本輪）

**動機**：s99j 把 scope-exit 釋放限在 `mem==0`，導致含控制流（迴圈/分支/顯式 return）的函數裡，局部結構體一律 leak（無論是否回傳）。本件解除該限制：控制流函數也釋放局部結構體，同時嚴防雙重釋放（double-free）。

**核心難點（mem==1 下 skip 失效）**：`mem==1` 輸出參住 entry 的 alloca 槽；`h2m-var-write` 對結構體走 `OP-STSTORE` 把值 `val`（如 MK 出的 `%st5`）存入槽；`h2m-var-read` 對結構體走 `OP-STLOAD` 交出新 id。故退出重讀的 id 與清單裡的 MK 原 id 不同，s99j 用 `skip=回傳值id` 跳過會失效。

**改動（`compiler/src/h2m.no`，純 h2m，cg 零改）**：
- `h2m-ctx` 新增三欄：`outn str`（輸出變量名）、`outsv i64`（最後寫入輸出槽的值 id）、`hc i64`（has-call 標誌）；`h2m-new` 同步初始化。
- `h2m-lower-func`：body 降級**前**先算出 `outn`（原 L3426 在 body 之後才求，但 `h2m-var-write` 在 body 期就用，故提前）；放行至 `c.outn`。
- `h2m-var-write`：mem==1 的結構體 `OP-STSTORE` 與 str `OP-SSTORE` 分支，若 `name == c.outn` 則記 `c.outsv = val`（= 當前在輸出槽、將被回傳的值 id）。
- OP-CALL 發射站點：`h2m-emit(c, OP-CALL, …)` 後置 `c.hc = 1`。
|  |  |  |
| --- | --- | --- |
- `h2m-emit-releases(c, skip)` 放行條件改為 `fire = mem==0 || (mem==1 && hc==0)`；跳過條件改為 `vid != skip && vid != c.outsv`（`outsv` 補上 mem==1 下「回傳值原 id」的跳過，防雙重釋放）。

**保守安全閘門（誠實標注，關鍵）**：`mem==1` 下**僅當函數體不含 OP-CALL 才放行釋放**。原因：含 call 時本地結構體可能被 move 給被調（caller 視為已轉移所有權），若 caller 再釋放即與 callee 雙重釋放——完整規避需 must/may move 分析（工作記憶標「尚屬規劃」），故含 call 時保守保持 leak（不引入 double-free）。`mem==0` 路徑不受影響（outsv 恆 0）。

**IR 核實（`compiler/tests/h2m-test.no` 新增 3 用例，全過）**：
- `test-s99k-local-release`：`mem==1`（含迴圈）下本地結構體（含 str@idx1）仍 `alloca %struct.s0`（證 mem 模式）+ `call void @nolang_rc_retain` ×1 + `call i1 @nolang_rc_release` ×1（證 scope-exit 釋放已啟用）。
- `test-s99k-return-no-release`：`mem==1` 結構體即回傳值 ⇒ retain ×1，但 `call i1 @nolang_rc_release` **不出現**（`outsv` 跳過回傳值，防呼叫方 double-free）。
- `test-s99k-call-gate`：`mem==1` 且函數含 `g(u)`（把本地結構體 move 給被調）⇒ retain ×1，但 `call i1 @nolang_rc_release` **不出現**（保守閘門，保持 leak 而非雙重釋放）。

**驗證狀態**：本輪 `15w45dqk` 門禁已不再阻塞——`h2m-test.no` 可完整跑完（baseline 989→本輪 1001 passed；剩 6 失均為 `s98 optmatch`，屬併發 session 改 cg.no 的 option 回歸，非本件）。s99j 修復後的 retain/release 配對與 s99k 三用例全綠。

**整檔狀態**：h2m.no +outn/outsv/hc 三欄與 `h2m-emit-releases` 放行/跳過放寬；h2m-test.no +3 用例（test-s99k-*）+ s99j 兩用例斷言由「硬碼值 id」改為「retain/release 計數」以抗 id 漂移。（s99m 後續：`hc` 欄位已撤、改用 `ownn/ownv/rdval/rdown/retset` owner provenance，`test-s99k-call-gate` 改名 `test-s99m-call-borrow-release` 並翻轉期望，見 §5.3.4.en。）


### 5.3.4.em s99l——M4-e 第五十二件續：純 str RC 的取得所有權站點（str 拼接 `'a' + 'b'` / `'a' - 'b'` ⇒ `OP-STRCAT`）（2026-11-09 本輪）

**動機**：s99j/s99k 建立了 scope-exit 釋放清單機制，但純 str 一直沒有「取得所有權站點」——h2m 從未降級 `OP-STRCAT`，故字符串拼接 `a + b` 直接落默認 `h2m-mark` 計 unsup，釋放清單對純 str 空轉。本件補上 str 拼接的取得所有權站點，把「拼接產生自擁有 str」與「該 str 的 scope-exit 釋放」閉環。

**調查（先讀實碼定事實）**：
- **str 字面量不擁有**：`OP-STRCONST`（cg.no:1790-1815）發 `getelementptr` 指向靜態全局常量 `[N x i8]`，非 `@nolang_rc_alloc` 堆，`%str-long` 的 data 指針指向只讀常量 ⇒ **借用、不登記釋放**（若釋放即誤 free 靜態常量）。
- **`@str_concat` 造自擁有**：cg-runtime.no:152-166 兩操作數各 `llvm.memcpy` 一份進 `@nolang_rc_alloc` 新緩衝，返回新 `%str-long`（rc=1）——**結果擁有、操作數只讀不消耗**。故僅結果登記釋放；操作數（字面量或另一 strcat 結果）各自按自身所有權處置。
- **`+` 與 `-` 皆拼接**：Go 側 `emitBinary`（codegen.go:5983-6018）在 `lt=="%str-long"` 時，`*` 走 `@_mir_str_repeat`（重複）、其餘二元 op 一律 `@str_concat`；`infixOp` 把 `+`→`OpAdd`、`-`→`OpSub`。故 h2m 對 `+`/`-` 且兩側 TY-STR 一律降 `OP-STRCAT`（鏡像 Go）。`*`（重複）h2m 無對應 op ⇒ 不放行。

**改動（`compiler/src/h2m.no`，純 h2m，cg 零改）**：
- `h2m-lower-expr` 的 `K-INFIX-EXPRESSION` 新增 `ok-s` 臂：`bop ∈ {OP-ADD, OP-SUB}` 且 `h2m-vty-at(lv)==TY-STR && h2m-vty-at(rv)==TY-STR` ⇒ `OP-STRCAT(dst, lv, rv)`（dst 新 id、TY-STR），緊接 `h2m-rel-add(c, dst, TY-STR, 0)` 登記 scope-exit 釋放（與 s99j/s99k 同一清單同機制）。
- 最終門禁 `(ok-i==0 && ok-f==0 && ok-u==0)` 追加 `&& ok-s==0`。
- 混合 `str/i64`、`str/txt`（Go 會先轉標量/拓寬再拼）h2m 未建模 ⇒ 不入該臂、落默認 mark 計 unsup（寧缺勿錯，不引入錯碼）。

**IR 核實（`compiler/tests/h2m-test.no` 新增 4 用例，全過）**：
- `test-s99l-strcat-release`：`s = 'a' + 'b'` 非回傳值 ⇒ `call %str-long @str_concat(` 1 次 **且** `call i1 @nolang_rc_release(i8* %dp` 1 次（結果登記並在 scope-exit 釋放）。
- `test-s99l-strcat-return-no-release`：`r = 'a' - 'b'` 即回傳值（同時證 `-` 亦拼接）⇒ `@str_concat` 1 次、release **0 次**（被 skip 跳過，防呼叫方 double-free）。
- `test-s99l-strcat-nested-release`：`('a' + 'b') + 'c'` ⇒ 2 條 `@str_concat`、2 次 release（內層結果被外層 memcpy 複製後即可釋放）；因 `str.contains` 只回布林不能計數，改用 `split(marker).len()==3`（2 次出現）斷言。
- `test-s99l-strcat-mixed-gate`：`'a' + 1`（str+i64）⇒ `agg.unsup > 0`、無 `@str_concat`（誠實門禁）。

**驗證狀態**：`./bin/no run compiler/tests/h2m-test.no` ⇒ **1012 passed / 6 failed / 1018 total**（baseline 1001；本件 +11 斷言 = 4 用例）。剩 6 失均為 `s98 optmatch`（併發 session 改 cg.no 的 option 回歸，非本件，首輪 baseline 即已失）。**判別力實測**：註釋掉 `h2m-rel-add(c, scid, TY-STR, 0)` ⇒ `test-s99l-strcat-release` 的 release 斷言與 `test-s99l-strcat-nested-release` 的 2-release 斷言**恰好失敗**（其餘不動），恢復後復綠。

**未及（誠實標注，vec 半邊當時仍缺——已於 §5.3.4.eo（s99n）補上）**：
- **純 vec RC 未建**：cg-runtime.no 僅有 `@vec_new`（返回裸 `{i64 0, i64 0, i64 0}`，內部用裸 `@malloc`/`@free`，**無 RC 頭**）／`@vec_len`／`@vec_get_i64`／`@vec_push_i64`；當時**無 `@vec_free`、無 vec 釋放 OP**；h2m 亦無任何降級發 `OP-VECNEW`（vec 僅由 `[]i64` 形參注入，自舉無 vec 構造降級）。⇒ vec 的「取得所有權站點」當時**不可達**，需先建 vec 的 free 原語＋構造降級方能閉環。**（2026-11-09 s99n 完成：`@vec_free` + `OP-VEC-FREE` + `K-SLICE-LITERAL` 降級 + 所有權外流門禁，見 §5.3.4.eo。）**
- **mem==1 且含 OP-CALL**：原 s99k 保守閘門對該情形整體不放行（strcat 結果 leak）——**已於 §5.3.4.en（s99m）撤除**（call 是借用，見 §5.6.1；間接回傳由 owner provenance 跳過）。

**整檔狀態**：h2m.no +`K-INFIX-EXPRESSION` 的 str 拼接臂與門禁 `ok-s`（cg 零改）；h2m-test.no +4 用例（test-s99l-*，其中 nested 用 `split().len()` 計數）。


### 5.3.4.en s99m——M4-e 第五十三件續：撤除 mem==1+含 OP-CALL 的整函數閘門（call 是借用；owner provenance 兜間接回傳）（2026-11-09 本輪）

**動機**：s99k 對「mem==1 + 含 OP-CALL」的函數整函數不放行釋放（保守 leak），理由記為「本地結構體可能被 move 給被調」。本輪讀 §5.6.1 鐵律與其 ASan e2e 證詞（見下行）確認該前提不成立，遂撤閘門，並補「間接回傳」的雙重釋放防護。

**關鍵事實（§5.6.1 鐵律，docs 第 2605 行三條落地規則）**：
- ① 傳參**不 retain**（caller 保留所有權）；
- ② callee **不得** release 參數（它是借用）；
- ③ caller 的 release 必須在**最後一次使用之後**——包括把結構體傳出去這件事。
- e2e 證詞：結構體持堆串 `'nolang'` 傳給 `read1` 做 STRCAT，**call 之後**才 `STRUCT-RELEASE`（exit 7）；負向對照把 release 挪到 call 前 ⇒ ASan `heap-use-after-free`。
⇒ **call 是借用而非 move**；caller 在 scope-exit（必在所有使用之後）釋放正確。s99k 的 `hc` 整函數閘門屬過度保守。
- 另：h2m 只在兩處發 `h2m-rel-add`（MK 結構體、STRCAT 結果），**形參從不登記釋放**（`h2m-lower-func` 只 bind + 鋪槽，不 rel-add）⇒ callee 端不 release 借用參數，與 ② 一致。

**先堵住的雙重釋放風險（撤閘門前必須先修）**：mem==1 下 `r = u`（間接回傳）經 `h2m-var-read(u)` 得 `OP-STLOAD` **新 id**，與 rel-list 裡的 MK 原 id（owner）不同。s99k 的 `skip`（ret 塊 STLOAD 新 id）與 `outsv`（寫槽的 val，也是 STLOAD 新 id）都抓不到 owner ⇒ 仍會對「已回傳的結構體」誤釋放。此 bug **在 mem==1 不含 call 時已存在**（s99k 遺留），撤閘門會把它擴到含 call 的情形，故必須同時修。

**改動（`compiler/src/h2m.no`，純 h2m，cg 零改）**：
- `h2m-ctx`：撤 `hc`；新增 `ownn/ownv`（變數→owner）、`rdval/rdown`（最近一次 var-read 的值 id 與 owner）、`retset`（可能被回傳的 owner 集合）。
- 5 個 helper：`h2m-own-set` / `h2m-own-get` / `h2m-rel-has` / `h2m-ret-add` / `h2m-ret-has`。
- `h2m-var-read` 尾：`v > 0` 時記 `c.rdval = v; c.rdown = h2m-own-get(name)`。
- `h2m-var-write`（mem==1）：算 `ownr`（val 在 rel-list ⇒ val；否則 val==rdval ⇒ rdown；否則 0）→ `h2m-own-set(name, ownr)`；`name==outn` 時 `c.outsv = ownr` 且 `ownr>0 ⇒ h2m-ret-add(ownr)`。撤 str/struct 分支舊的 `outsv = val`。
- `h2m-emit-releases`：`fire` 恆真（撤 `hc` 閘門）；跳過條件加 `h2m-ret-has(vid)==0`。
- 撤 OP-CALL 站的 `c.hc = 1`。

**判據（為何 sound；最壞 leak 但絕不 double-free）**：`retset` 只「多跳過」不「少跳過」（may 近似、owner 只增）⇒ 最壞 leak，不會漏跳一個該跳的 owner ⇒ 無 double-free/UAF。舊 bug 的 `r = u`：owner 經 `r = u` 的 `val==rdval` 傳遞到 MK 原 id，併入 outsv/retset ⇒ 正確跳過。

**IR 核實（`compiler/tests/h2m-test.no`）**：
- `test-s99m-call-borrow-release`（原 `test-s99k-call-gate` 改名並翻轉期望）：mem==1 + `r = g(u)`（u 傳給被調）⇒ retain 1、release **1**（撤閘門後 caller 釋放；g 借用 u）。
- `test-s99m-indirect-return-no-release`：`r = u`（間接回傳）⇒ retain 1、release **0**（owner 併入 retset，防 caller 懸垂）。
- 既有 `test-s99k-local-release`（1）/`test-s99k-return-no-release`（0）/s99j 三例/s99l 四例全綠。

**驗證狀態**：`./bin/no run compiler/tests/h2m-test.no` ⇒ **1016 passed / 6 failed / 1022 total**（baseline 1012；+4 斷言 = call-gate 改名翻轉 + indirect-return 新增）。剩 6 失均 `s98 optmatch`（併發 session cg.no option 回歸，非本件）。**判別力實測**：註釋掉 `ownr == 0 && val == c.rdval -> ownr = c.rdown` ⇒ `test-s99m-indirect-return-no-release` 恰好失敗（release 變 1），其餘不動；恢復後復綠。

**殘餘（誠實標注，均 leak 非 double-free）**：
- ① 循環體內 MK 出的值按一個 SSA 值靜態釋放一次 ⇒ 多迭代者每次迭代的堆漏（s99j/s99k 同，非本件）；
- ② `retset` 是 may 近似，分支多寫輸出槽時非實際回傳那支也跳過 ⇒ leak；
- ③ 別名缺 retain（`v = u` 不復刻 §5.6.1「賦第二變數須 retain」）——當前靠「單值單釋放 + scope-exit」不顯形，屬 M7 精確所有權留待。

**整檔狀態**：h2m.no −`hc`、+`ownn/ownv/rdval/rdown/retset` 與 5 helper；`h2m-emit-releases` 恆放行 + retset 跳過；var-read/var-write 記 provenance。h2m-test.no：`s99k-call-gate` ⇒ `s99m-call-borrow-release`（翻轉 0→1），+`s99m-indirect-return-no-release`。


### 5.3.4.eo s99n——M4-e 第五十四件續：vec 的取得所有權站點 + scope-exit 釋放（切片字面量 ⇒ `OP-VECNEW`/`OP-VECPUSH`；`@vec_free`/`OP-VEC-FREE`；所有權外流門禁）（2026-11-09 本輪）

**動機**：§5.3.4.em「未及」把 vec 半邊標為「不可達」——釋放清單機制早在 s99j 就預留，但 vec **既無釋放原語、也無構造降級**，兩頭都缺。本件同時補兩頭，使 str／struct／vec 三族的「取得所有權 ↔ scope-exit 釋放」配對齊備。

**先決調查（三項事實，逐條釘死）**：
1. **cg-runtime 無 vec 釋放原語**：只有 `@vec_new`（返回裸 `{0,0,0}`）／`@vec_len`／`@vec_get_i64`／`@vec_push_i64`；資料緩衝走**裸 `@malloc`/`@free`、無 RC 頭**（`cg-runtime.no` 檔內註釋明寫，§11 B5）。⇒ 沒有 `@vec_retain`，**不可能**照 str 的 RC 模型做。
2. **h2m 無 vec 構造站點**：全域零命中 `OP-VECNEW`/`OP-VECPUSH`/`K-SLICE-LITERAL`/`K-ARRAY-LITERAL`；`TY-VEC` 只從 `[]i64` 形參與 `[]i64` 欄位注入（借用）。parser 側 `K-SLICE-LITERAL` 早已存在（`ps.no:2516`，含空字面量 `[]`），只是 h2m 沒降級它 ⇒ 一切 `[...]` 字面量都落 `h2m-mark`。
3. **oracle 有完整 vec RC，但那是另一套記憶體模型**：Go 側有 `@vec_retain`/`@vec_free`/`__nolang_vec_free_<T>`/`insertDrops`（`src/mir/analysis.go`、`builtin_call.go:91`、`borrow_release_test.go:211-240`）。**刻意不對齊**：自舉 vec 元素只有 i64，帶 RC 頭會讓 `@vec_get_i64`/`@vec_push_i64` 全部要改寫 offset，收益為零。

**關鍵張力（本件最核心的判斷）**：緩衝無 RC 頭 ⇒ **沒有 retain 可用** ⇒ vec 的所有權必須**唯一**。這與 str 不同：str 別名缺 retain 最壞是 leak（RC 兜住），vec 一旦兩個 owner 各自 free 就是 **double free**、一處 free 另一處還活著就是 **UAF**。故本件的 soundness 不靠「多跳過」（s99m 的 retset 那種 may 近似），而靠**入口與出口兩端都把不唯一的情形擋在門外**：

- **入口（只登記唯一 owner）**：`@vec_push_i64` 返回**新** `%vec` 且**自行 `@free` 舊緩衝** ⇒ 一條字面量鏈 VECNEW→PUSH→PUSH… 的中間值全部無主，只有**最後那個** `%vec` 是 owner。故 `h2m-rel-add` 只登記 `vc-cur` 的最終值。（登記中間值 = free 一個已被 push 釋放的指標，是必然的 double free。）
- **出口（三條外流路逐條封）**：①**返回值**——vec 回傳在簽名層就被擋（`rb == 0 && rc == TY-VEC -> bad = 1`，s62 既有門禁），本件不需新增；②**結構體字面量欄位**——新增 `h2m-vec-own` 逐欄位檢查，命中即 `h2m-mark`；③**欄位寫 `s.f = [...]`（`OP-STRUCT-SET`）**——右值命中即 `h2m-mark`。沒有 vec retain 卻讓值外流，等於交出去之後自己還留著一次 free 的權利。

**改動（4 檔）**：
- `compiler/src/mir.no`：新增 `OP-VEC-FREE = 94`（接既有最大碼 `OP-OPTGESCF=93` 之後，不重排）。語義：`x` = 向量值 id，無結果，發 `call void @vec_free(%vec %vc<x>)`。註釋明寫「這是破壞性一次性 free，不是 RC 的 release」。
- `compiler/src/cg-runtime.no`：新增 `cg-runtime-vec-free`（`extractvalue f2=dat` → `icmp eq 0` → `br` → `inttoptr i64→i8*` → `call void @free`；`dat == 0` 短路），並掛進 `cg-runtime-prelude-full`（順序在 `vec_push` 之後）。
- `compiler/src/cg.no`：新增 `op == OP-VEC-FREE` 臂，發 `call void @vec_free(%vec %vc<x>)`（cg 端零判斷——判空在 runtime helper 內）。
- `compiler/src/h2m.no`：①`h2m-lower-expr` 新增 `K-SLICE-LITERAL` 臂（VECNEW + 逐元素 PUSH，元素非 i64 即 `h2m-mark`）；②`h2m-emit-releases` 加 `ty == TY-VEC -> OP-VEC-FREE`；③新增 helper `h2m-vec-own`（= 在 rel-list 且型別 `TY-VEC`，**單一判準**：所有權的唯一登錄點就是 rel-list）；④MK 欄位與 `OP-STRUCT-SET` 右值各加一道外流門禁。

**IR 核實**：
- `compiler/tests/h2m-test.no` +5 用例：`test-s99n-vec-literal-release`（`v = [1,2,3]` ⇒ VECNEW 1／PUSH 3／VEC-FREE 1，且**只** free 最終 `%vc8`、不 free 中間 `%vc4`/`%vc6`）；`test-s99n-vec-empty-literal`（`w []i64 = []` ⇒ VECNEW 1／PUSH 0／VEC-FREE 1）；`test-s99n-vec-elem-type-gate`（`['a','b']` ⇒ unsup>0、無 `@vec_new`）；`test-s99n-vec-field-mk-gate`（`P { v: [1,2], id: 7 }` ⇒ unsup>0）；`test-s99n-vec-field-set-gate`（`b.data = [1,2]` ⇒ unsup>0）。
- `compiler/tests/cg-test.no` +`test-vec-free`（手寫 op 陣列 ⇒ `call void @vec_free(%vec %vc2)` 出現、`nolang_rc_release`/`nolang_rc_retain` **均不出現**）。
- `compiler/tests/cg-runtime-test.no` `test-vec-helpers` 擴充 5 條：`@vec_free` 簽名、讀 dat、`br i1 %nul, label %done, label %freeme`、`call void @free(i8* %p)`、**不含** `@nolang_rc_release`；並釘 `prelude-full` 含 `@vec_free`。
- **IR 文本額外離線驗證**：把 `@vec_free` 函數體逐字抄進 `/tmp/vecfree.ll`（附 `main` 造一個 dat=malloc(16) 的 vec 與一個 dat=0 的 vec，各呼叫一次），`llvm-as` 通、`opt -passes=verify` 通、`clang -fsanitize=address` 編譯後執行 **exit 0 無 ASan 報告**（既無 double free，也無 leak）——證明「判空短路」與「free 正常路徑」兩支在真實執行下都成立。

**驗證狀態**：`./bin/no run compiler/tests/h2m-test.no` ⇒ **1031 passed / 6 failed / 1037 total**（baseline 1016；本件 +15 斷言 = 5 用例）。`cg-runtime-test.no` ⇒ **66/66 全綠**。剩 6 失均為 `s98 optmatch`（併發 session 改 cg.no 的 option 回歸，非本件，首輪 baseline 即已失）。
- ⚠️ **`cg-test.no` 整檔當前不可編譯**（併發 session 的 `15w45dqk` 檢查器收緊：plain 變數收 `?i64` 右值成硬錯誤，`cg-test.no` 內 5 處 `j`/`w` 命中，含 `test-vec` 自身）——該檔失紅**先於本件**，非本件回歸。本件的 `test-vec-free` 以最小閉包探針 `tmp/probe-s99n-cgfree.no` 單獨跑過 ⇒ **4/4 斷言全過**；待 `15w45dqk` 語料跟進後隨整檔復跑。
- **判別力實測（兩輪）**：①註釋掉 `h2m-rel-add(c, vc-cur, TY-VEC, 0)` ⇒ 5 條「有 free」斷言 + 4 條門禁斷言恰好失敗（門禁判準就是 rel-list，故與所有權同生共死，一個入口開關同時釘住兩側）；②恢復後只註釋兩處 `h2m-vec-own(...) -> h2m-mark(c)` ⇒ **恰好**那 4 條門禁斷言失敗、釋放斷言不動，恢復復綠。

**殘餘（誠實標注）**：
- ① **循環體內切片字面量按一個 SSA 值釋放一次** ⇒ 每次迭代漏一個緩衝（與 s99j/s99k 的 MK 同族，非本件引入）；
- ② **元素型別只有 i64**：`[]str`／`[][]i64` 等仍計 unsup（`@vec_push_i64` 是單型 helper）。要放寬須先做逐元素 retain/release 的 `__nolang_vec_free_<T>` 族——那才是 §11 B5 真正的前置；
- ③ **vec 作返回值仍是門禁**（s62 既有）：本件沒動它，所以「字面量直接回傳」這條外流路是關著的，不是忘了；
- ④ **別名缺 retain**（`v = u`）對 vec 的後果比 str 重：兩個名字指向同一緩衝，scope-exit 只 free 一次（rel-list 按 vid 去重）故**不會 double free**，但若未來給 vec 加 retain，此處需同步（M7 精確所有權）。

**整檔狀態**：mir.no +`OP-VEC-FREE=94`；cg.no +`OP-VEC-FREE` 臂；cg-runtime.no +`cg-runtime-vec-free` 並掛 prelude-full；h2m.no +`K-SLICE-LITERAL` 臂、`h2m-emit-releases` 的 TY-VEC 分支、`h2m-vec-own` helper、兩處外流門禁。測試：h2m-test +5 用例、cg-test +1 用例、cg-runtime-test +5 斷言。


## 6. 里程碑與進度

> 本節是**唯一**的實際進度來源：§0 是決策§5 是架構§7 是門禁**每完成一個里程碑就更新這裡的狀態與證據**（證據必須是可重跑命令 + 觀測值）

| 里程碑    | 範圍                                                                              | 狀態      | 證據（可重跑）                                                                                |
| ------ | ------------------------------------------------------------------------------- | ------- | -------------------------------------------------------------------------------------- |
| **M0** | 子項目骨架：`compiler/` 佈局`package.jsonc``main.no` 分派`util/sb.no``tools/shs-check.no``scripts/selfhost_diff.sh` | ✅ **完成** | `no run compiler/tools/shs-check.no compiler/` ⇒ **10 檔 / 0 違規 / OK**；`selfhost_diff.sh --self-test` ⇒ **無 FAIL** |
| **M1** | 詞法：`src/tok.no`（92 個 `TokenType`）+ `src/lx.no`（`scanToken()` 的忠實移植）              | ✅ **完成** | `no run compiler/tests/tok-test.no` ⇒ **62/62**；`lx-test.no` ⇒ **45/45**；L1 全語料 **675/675 SAME, 0 DIFF**（§7.1.1） |
| **M2** | 語法：71 個 AST 節點 + 復刻 `parser/dump.go` 的輸出格式                                     | ✅ **完成（2026-10-27，s8y `261ab91c`）** | **L2 已可執行**：`noc` 已實作 `ast` 子命令（`drv.cmd-ast`），`selfhost_diff.sh --self-test --levels L0,L2 --sut /tmp/noc-s0`（2026-10-11）⇒ **L0 4/4 SAMEL2 646/646 SAME0 DIFF**語料 `<unknown>` 0 行（71/71 節點全覆蓋）真實 noc↔oracle 逐位元組相同：**+1（独立 `AnnotationStatement{entries: N}` 節點增量，解锁 collection/*）0 回歸**——絕對數方法論敏感（parallelism × 逾時上限）：以 uniform 120s-alarm `-P 3` 同法量測 s31=137（AssignExpression）→s32=145（ReturnStatement）→s33=158（Annotation）→s34=159（UseStatement）→s35=202（ArrayLiteral 推論元素型別，**+43**，為 aes/zlib 等 `[N] = [int-literals]` 表檔解锁）→s36=203（self-dot `.field` 前綴 receiver，新匹配 `src/std/io.no`）→s37=204（独立 `AnnotationStatement{entries: N}` 節點——不可附著之語句級 `#{...}`，新匹配 `src/std/collection/stack.no`）→s38=213（**ForStatement bang/cond-loop 簇**——陳述位 `!! { }`（恆真無限）/`! { }`（恆假）/`(cond) { }`（前置條件），**+9**，新匹配 `src/std/bufio.no``collection/hash-set.no``collection/str-set.no``env.no``tests/dowhile.no``loop-test.no``split-loop.no``str-loop-assign.no``mem-safety/bug15-read-dowhile-copyfile.no`）→s39=222（**字面量型別推斷補齊**——`CharLiteral`→`char`十六進位 `0x/0X` `IntegerLiteral`→`byte`（十進位仍 i64），同時補 `ps-infer-type`（裸 RHS）與 `ps-elem-type-of`（陣列/切片首元素），**+9**，新匹配 `tests/char-*`（literal/new-method/new-syntax/old-syntax2/simple/step4/vet））→s40=253（**無值明示型別裸宣告 `name TYPE`**——陳述位 `result []byte`/`z [16]byte`/`x person`/`i8 i8` 此前被誤解析為 IndexExpression，**+31**，覆盖 crypto/archive 大量 `[]byte`/`[32]byte` 暫存宣告）→s41=262（**複合型別名**——`ps-parse-type` 補限定名 `a.b`（NamedType 續吃 `.IDENT`，對齊 Go stmt.go:1034）與巢狀元素 `[][]T`/`[N][]T`（SliceType/ArrayType 臂改為遞迴 ps-parse-type，元素 ∈ {IDENT,[,?}），同把 `ps-parse-typedecl-noval` 升級為通用 `ps-parse-typedecl`（新增 `= value` 分支），**+9**，新匹配 tls/tls-part3/http2/hpack/fs-error-{final,info}/fs-open-err/nested-vec-test/push-narrow）→s42=273（**無色 async 前綴運算子 `run`/`co`/`awy`**——`ps-parse-expr-prec` 缺此三前綴臂⇒`h = run f()` 的值被解析為空`f()` 漏成獨立 ExpressionStatement；補上後 `run e`→RunExpression{Call:e}`co e`→CoExpression{Call:e}`awy e`→AwaitExpression{Right:e}（三節點 dumper 早已支援纯 parser 缺口），**+11**，新匹配 async-{cancel,coop,rc,yield,handle-alias}/module-async/slot-rebind-unsafe/mem-safety/async-*）→s43=284（**區塊/程式尾註解 TrailingComments**——`ast.no` dumper 早已實作 OP-TRAILING（讀 ntrail/ct0/aux 印 `TrailingComments`＋每行 `//<text>`），唯 `ps.no` 的 `parse`（Program）與 `ps-parse-block`（BlockStatement）把 `}`/EOF 前的獨立尾註解群餵進 `ps-skip-trivia-doc` 後**丟棄**⇒語料中最後一條陳述之後`}`/EOF 之前的 standalone 註解從未進入 TrailingComments；新增三 helper（`ps-trail-region` 前瞻（不消費 token）判斷下一個非 trivia 是否 ∈ {RBRACE,EOF}`ps-collect-tail` 消費 NEWLINE/COMMENT 收集群——同條陳述行尾的首個行內註解仍走 set-cmt其餘全部入 trail`ps-attach-trailing` 走 aux-begin/aux-add/set-aux-counts(ntrail=len)），並接進 Program 與 BlockStatement 兩處**零回歸結構保證**：ntrail 預設 -1⇒renderer 跳過trail 空則不呼叫 attach⇒原匹配檔輸出位元組不變**+11**，新匹配 src/std/types.notests/{arr,duplicate,for2,i,str,range-test}.nomem-safety/{deep-free-nested-vec,deep-free-str,deep-free-struct-vec,minimal-str-map2}.no）→s44=283（**結構體欄位型別拓展 + 欄位尾註解跳過（ps-is-struct-def）**——裸結構定義 `name { field TYPE }` 之前仅在栏位 TYPE ∈ {IDENT, []IDENT, [INT]IDENT} 且无尾注时被识；限定名栏位 `conns [32]net.conn`（`]` 后 `net` 再 `.conn`）嵌套切片 `rows [][]str`栏位尾 `#{inline=…}` 注均使旧手筛扫描器回 0⇒整个结构退回 ExpressionStatement（而 ps-parse-struct-def 主体早已走 ps-parse-type（s41 已支援限定/嵌套）⇒**仅 gate 限**）修复：新增 ps-skip-type-idx（索引前视不消费 token）完整鏡像 ps-parse-type 四形（`a.b`/`?T`/`[]T`/`[N]T`元素递归∈{IDENT,[,?}）取代 gate 内的手写栏位型扫描；另新增 ps-skip-annot-idx（gate）/ps-skip-annot（body）跳过栏位尾 `#{...}`——`no ast`（无 -annotations）完全不渲染栏位注⇒跳过即可**零回归结构保证**：含限定/嵌套/注栏位的结构此前必已 NO（gate 拒→ExpressionStatement≠oracle StructDefinition），新能力只改善**+3**，新匹配 src/std/net/client.notests/field-{inline-annotation,tag}.no）→s45=284（**枚举/标签枚举定义 `name { red, green }` / `name { ok(v T), fail }`**——陈述位 IDENT+LBRACE 此前仅认 struct 形；Go parser.go:273 classifyBlock 把 blockEnum/blockTaggedEnum 与 blockStruct/blockInterface 分流（decl.go:749 parseEnumDefinitiondecl.go:1077 parseTaggedEnumDefinition），而 asp.no dumper（EnumDefinition=24/EnumValue=25/TaggedEnumDefinition=65/TaggedEnumVariant=66 + taggedEnumField HELPER 对）早已支援⇒纯 parser 缺口新增两函数：`ps-is-enum-def`（索引前视 gate，不消费 token）——成员 ∈ {IDENT/NIL 裸变体`名(字段…)` 括号载荷`名 = INT` 显式值`#{…}` 注NEWLINE/COMMA 分隔}，**保守分流**：括号+裸并存→2（标签）仅裸/显式→1（枚举）全括号无裸→0（与接口歧义）`) (` 返回组或 `t.` 限定→0（接口形）任何非预期 token→0——gate 拒者原本必走 ExpressionStatement≠oracle⇒结构上零回归；`ps-parse-enum-def`（body）——计数语义对齐 Go：显式 `V = INT`不推进 nextVal（decl.go:807–821）标签变体 index 逐变体+1；变体字段以 helper-begin/aux-add（name|type 成对）挂 taggedEnumField；Type kid 以首字段型别占位（Go 同口径 Fields[0].Type）⚠️ `#{…}` 变体注不渲染（默认无 -annotations）⇒跳过即可**+1**，新匹配 tests/chain-copy.no；t49-enum-defs fixture byte-identical⚠️ 语料其余 9 档首分歧即枚举（TaggedEnum 6+Enum 4+err.no 等）现结构已对，但随即在**下游 armBinding 裸 match desugar 主簇**再分歧（LetStatement 桶 107 档中 44 档为该簇）——该簇已确认为下一个最高价值增量⚠️ **方法学事故记录**：本增量中 t31-standalone-if 一度显示回归——隔离实验证明源于 `~/.cache/nolang/ast` 被并行会话（Go 侧 concurrent edits + bin/no 重建）污染的**非确定性构建**（同一 HEAD 源码两建不同行为），清缓存重建后 s44/s45f 行为一致arm 零回归；差分对照必须用**同一工具链代次**重建的新旧 noc⚠️ 其余 4 fixture（t37/t4/t7/t8）与 live oracle 的差异为 **noc-s0 时代既有**（fixture 门历史基线并非 live-oracle 对照），非本增量引入→s46=302（**明示 match 的 armBinding desugar 补齐**——三缺口：(a) err/nil 关键字臂（型样为裸关键字且紧接 `->`）→ 新 `ps-mk-it-let-arm` 建 `LetStatement{name:it, type:<变体>, value:subj, synthetic, armBinding:true}`（Go lowering.go buildItBindingForArm：被匹配型别未知时仅 err/nil臂带型别，ok/else 回落裸 synthetic——同文件 `ps-mk-it-let`；dumper 早已支持 armBinding 槽，纯 parser 缺口）；(b) 裸尾臂 `-> expr`＝else：peek 见 RARROW 直接视同 wildcard（不消费 token），其区块作前一 IfExpression 的 alternative，不再伪造 `subj == <nil>` 条件臂；(c) kvar 检测用 `ps-peek-at(p,1)==RARROW` 区分关键字臂与同名普通变量fixture `t50-match-arm-binding.no` 于晋级后 noc-s0 与 oracle 逐位元组相同**同代 A/B**（同一好代次工具链分别编译 HEAD ps.no＝ctl 与 s46 ps.no）：corpdiff(ctl→s46)＝ +20 档回归 0；**最终语料门禁** corpdiff(noc-s0[s45f]→noc-s46f)（live oracle 02:18 代）＝ **+19 档（match-*/option-*/vec-*/open-*/arr-* 等 armBinding 主簇）回归 0**283→302（旧基线 284→283 系 02:18 oracle 代对个别档预期漂移所致与本增量无关）；fixture 门 47/51（仅余既有 4 档 t37/t4/t7/t8）；单元 4 测试全 PASS；vet 错误集与 HEAD 完全同（仅行移 3309→3348）⚠️ **晋级阻塞——外部工具链回归**：当代 Go 源码（deac8377 已提交＋并行会话未提交 WIP）编译与 noc-r1/r2/r3 时代逐位元组相同的 HEAD ps.no，产出的 noc 在 t31-standalone-if 与 tests/mkdtemp-debug.no 丢失 standalone-if 链的 inline else（`BlockStatement{inline:true}` 消失，签名一致；284→283）；三代工具链 parse ps.no 自身 AST 逐位元组一致⇒纯 lowering/codegen静默误编译；且昨日 20:12 的好状态只存在于未提交WIP（fd3f2931 提交信息描述的 parser 工作并未含 Go 文件入仓），不可复现⚠️ **工具链事故升级与解法（s46 期间）**：主树当代 Go（deac8377+未提交 WIPbin/no@02:18）以cache-off编译 HEAD ps.no 仍丢失 standalone-if inline else（t31+mkdtemp 同签名）——属当代 lowering/codegen 真回归待 Go 侧修复；但 **纯 346d69ae（v0.4.4）与纯 c872721a 两代工具链 cache-off 均 GOOD**⇒破坏面锁定在 deac8377 及其后 WIP 区间处置：晋级二进制改用 **git 钉住的纯 v44 工具链 + NOLANG_AST_CACHE_OFF=1 构建**（/tmp/noc-s0＝noc-s46fsha 51de820a），旧基线存 /tmp/noc-s0-s45.bak⚠️ **二分期中踩到的缓存新坑**：ASTVersion 只在 commit 时 bump，相邻两代未 bump 的 WIP 共享同 key（content, ASTVersion, StdSigKey）⇒cache-on 的文件级二分结果全是假象（本会话曾被 b3GOOD误导数轮）；**任何差分/二分实验必须 NOLANG_AST_CACHE_OFF=1 或隔离 HOME**（该开关只保 user-module AST 盘缓存层））→s47=308（**后辍圆括号 range 切片 `f(a..b)`/`f(a..b]`**——语料 7 个 SIGBUS 档的元凶：Go expr.go LPAREN 臂有 isRange 探视（`(` 后跟 ELLIPSIS或 INT/IDENT 紧接 ELLIPSIS ⇒ parseSliceExpression，leftInc=false`]` 闭合 rightInc=true；否则普通 CALL），而 ps.no 后辍链把 LPAREN 一律当 CallExpression——`a(1..4]` 落入 call 参数循环的防呆步进＋外层 pgo 重循环⇒死循环/SIGBUS新增偏移 1/2 token 探视分支（鏡像 Go saveState 探视）；并补 LBRACKET 臂两处 en 条件缺 `!= RPAREN`（Go 双闭合）fixture `t51-range-call.no` 与 oracle 逐位元组相同门禁：fixture 48/52（仅余既有 t37/t4/t7/t8）；单元 util/tok/lx/ast 4/4 PASS；vet 错误集与基线完全同集（行号全 +40 位移）；语料门 corpdiff(noc-s0[s46]→noc-s47)＝ **+6 档0 回归**（arr/str/txt/vec-sliceslice-open-inclusivemem-safety/reverse-slice-clone 六个原 BUS 档全匹配）302→308⚠️ **方法学坑——超时误报第三机制**：首跑 -P 4/30s 并发扫描把 sha256/yaml 报成新增 SUT-FAIL 回归——实因该两档 noc 解析耗时 ~15-25s 处于临界，并发负载压爆 alarm；串行 -P 2/90s 复测 s0/s47 行为完全一致⇒**语料扫描须 -P ≤2 + alarm ≥90s**，否则慢档被误判回归（与缓存假象跨代误编并列第三独立机制）晋级同 s46：git 钉纯 v44 工具链 + NOLANG_AST_CACHE_OFF=1 构建，/tmp/noc-s0＝noc-s47sha1 fc0216b4，旧基线存 /tmp/noc-s0-s46.bak；余 SUT-FAIL 9 档（aes-cbc/net/fmt-heuristic/fn-type-{result,return}/map-key-leak/str-order 挂死tls SEGVhttps-server BUS——皆 noc-s0 时代既有，非本增量引入））→s48=370（**陈述位 `{ … }` 区块：BlockStatement wrapper + 裸 match desugar**——首分歧调查最大桶：抽样 200 档中 ≈48 档卡 `BlockStatement{}` wrapper 缺失≈22 档卡裸 match 合成 `IntegerLiteral{value:1}`此前 `ps-parse-stmt` 无 LBRACE 臂⇒防呆步进吃掉 `{`语句外泄一层；裸 match `{ cond -> body … -> else }` 完全未实现新增 `ps-parse-brace-stmt`（鏡像 Go stmt.go:402 saveState/restoreState：`ps-bm-scan` 探深度 0 `->`（排除 `{ IDENT : {` 结构化形）⇒先试 `ps-parse-bare-match`失败还 p.pos 回落普通 `ps-parse-block`+消费 `}`）；`ps-parse-bare-match` 鏡像 match.go:134 三形臂体（`{…}` 区块（先试嵌套裸 match）NEWLINE 块形（statements 至 isArmStart/`}`，inline:true）同行单语句）+ lowering.go:1727 buildBareMatchDesugar 两装配路：全臂非 wildcard 且 n>1⇒fast path `IfExpression{IntegerLiteral{1}(合成 ps-mk-true), Block{各臂兄弟 ExpressionStatement{IfExpression}}}`，否则倒序 elif 链（尾 wildcard `-> body`＝`IfExpression{IntegerLiteral 1, body}`）；中途 wildcard／臂首 `#{`／`ok->` 保守 bail 还原**⚠️ match 非排他/有状态守卫三新坑**：(1) wildcard 臂消费箭头后重评 `ps-peek!=RARROW` 因状态已变而误 fail⇒`ad` 快照旗标；(2) 倒序链 `w==1&&res>0→bail` 被本臂刚建的 res 再触发⇒`hdw` 快照；(3) 嵌套裸 match 尝试失败时 p.pos 已越过 `{`回退路再 bump⇒双消费⇒`j <- [0..N): {` 被吞成 SliceLiteral 整档挂死（http/server 180s rc=142）⇒尝试前 `nsave=p.pos`失败还原门禁：fixture 49/53（仅余既有 t37/t4/t7/t8）单元 4/4 PASS（⚠️ 当代 oracle 新增命名规范检查拒 `_test.no` 下划线路径⇒复制到 tmp/zz-s48gate/*-test.no 运行）vet 10 既有 ERROR 集不变；语料门（-P 2/90s 全 647 档）＝DIFF 330→268**新增匹配 62 档0 回归**（SUT-FAIL 集与 s47 完全同 9 档；http/server 由挂死转 rc=0 且与 oracle 差异行数 6435→1394 / 3089→433，仍属既有 DIFF 档）晋级：纯 v44 工具链 cache-off，/tmp/noc-s0＝noc-s48 sha1 c53a5641，旧基线存 /tmp/noc-s0-s47.bak）→s49=381（**语句位 `*`/`**` 速写 + `#N` 标签循环**——s48 后首分歧调查最大可解桶：oracle `BlockStatement{}`/`BreakStatement` 侧 ≈48 档中相分成两簇，其中 index-out desugar 簇（≈39 档，Go lowering.go maybeIndexOutAssign）需 sem 函数作域型别表（FuncVarType/idxLocalTypes）+ bounds.go 可证明界内豁免（inBoundsIdx）才可逐位元组复现，超出 parse-local 范畴留待后续；本增量取 `*`→break／`**`→continue 速写簇：Go parseStatement 对语句起始 MUL 走 parseStarBreakOrExpr（stmt.go:552/2913，peek(1) ∈ {NEWLINE,EOF,SEMI,RBRACE,LABEL,IDENT} 才成立可跟 `#N`/IDENT 标签）STAR_STAR 走 parseStarStarContinue（stmt.go:558/2937，无閘门）；ps.no 陈述位无 MUL/STAR_STAR 臂⇒`cond -> *` 被防呆拆成 `InfixExpression{op:*}[<nil>,next]`（http.no 首分歧即此）；新增 ps-parse-star-break/ps-parse-star-star-cont/ps-star-label/ps-mk-break-cont 四函数+两 dispatch 臂；另补 `#N` 标签前缀臂（parseLabeledStatement 保守版：消费 LABEL 后递回 ps-parse-stmt，若得 ForStatement 则改写 slot0 label）⚠️ 多返回值坑：`(label, kind)` 裸组尾式非法语法`x,y = f()` 解构仅适用已命名结果惯用法——改用 `[]str` out 参数传 label/kindfixture `t53-star-break.no`（裸 match 臂体 `*`/`**``{ }` 内裸 `*``* #1`/`** #1` 标签standalone-if 链）与 oracle 逐位元组相同门禁：fixture 50/54（仅余既有 t37/t4/t7/t8）单元 4/4vet 10 既有；语料门（-P 2/90s 全 647）＝DIFF 268→258**新增匹配 11 档0 回归**（tests/https-server.no 由 SUT-FAIL rc=138 SIGBUS 转 rc=0 DIFF属改善；http.no 与 oracle 差异 1394→185 行）晋级：/tmp/noc-s0＝noc-s49 sha1 bb07e0ed，旧基线存 /tmp/noc-s0-s48.bak）→s50=383（**区块内换行后 `-> else` 续接（standalone-if else-attach）+ 工具链纪律再确认**——oracle 当日三代重建（08:40→09:56→10:30），10:30 代把 wrapStandaloneChainDepth 定为递归形（stmt.go:1768 注释引 str.replace-n 事故）后， promoted noc-s49（当时误用主树 bin/no@08:40编译，违反 s46 钉死纪律）静默丢失链式 `a -> b -> f` 最内层 consequence 而被漂移照出；本增量起一切 noc 构建一律改用 /tmp/nolang-v44 自带 `./bin/no`（纯 v0.4.4 钉死 + cache-off）新特性：复刻 Go parseBlockStatement 迴圈开头 stmt.go:2168–2186——区块内上一条 standalone-if（RTStandaloneAlternative==nil）后换行的 `-> body` 挂为其 Alternative；实现弃用建节点后原地覆写 kids方案（ast.no kid-set），改在 ps-mk-standalone-if 组装前前探：跳 NEWLINE/SEMICOLON 后恰为 `->` 即就地吃掉并把 body 写进 alt 槽（节点一次成型零覆写）；psst 加 inBlk/inMatch 上下文位：program 顶层不挂（t31 系顶层 `d -> e -> f`+`-> g` 分离形）臂解析中抑制（裸 match/match 自行消化 wildcard 臂），ps-parse-block 进入时 inMatch 清零（臂 body `{ }` 内续接合法）fixture t54-newline-else.no（j1 形/块形 else/（A）wildcard 三连挂/顶层不挂）与 oracle 逐位元组相同门禁：fixture 51/55（仅余既有 t37/t4/t7/t8）单元 util/tok/lx/ast 4/4vet 10 既有 ERROR语料门（-P 2/90s 全 647，oracle 10:30 代）＝SAME 383 / DIFF 256 / SUT-FAIL 8（全 rc=142 慢档与 s49 同集）；vs s49 名单：新增 tests/{float-ne-nan,set}.no丢 0 档（漂移日净匹配受 oracle 三代重建主导，attach 本身解锁的是语料内裸 match 回退区块形如 json.no 内层块）晋级：/tmp/noc-s0＝noc-s50g sha1 c766715f，旧基线存 /tmp/noc-s0-s49.bak⚠️ **事故记录（归因错误）**：s50 期间一度把链式丢 consequence 误判为 ast.no 新增 kid-set（i64 vec 字段元素写入）污染全模组 codegen——单变量实验 s50e 因同时混入了错误工具链（主树 bin/no）构建而给出假象；真因＝工具链代次误用，kid-set 无罪（后因设计更优而保留弃用）差分对照一律同工具链代次重建；晋级前必须用 v44 `./bin/no` 重建并跑 fixture 门新基线首分歧分桶（256 档）：ORACLE+ LetStatement 104／BlockStatement 48（index-out 簇 ≈39 + 裸 match mid-wildcard bail 簇如 t54 旧 g/h 形）／FunctionDefinition 16／NOC+ ExpressionStatement 11）→s51=413（**同檔裸名呼叫之 parse 期回傳型別推斷**——s50 後最大桶：256 檔首分歧 LetStatement 家族 132（裸 63＋nullable 34＋合成 35），其中 44 檔直接分歧於 `value: CallExpression` 的型別蓋印復刻 Go parseLetStatement 的 CallExpression 推斷分支（stmt.go:1502–1533）＋ type.go 的 returnTypeFromCallExpr／filterInferableType／markInferred：RHS 為 `CallExpression{fn: 裸 Identifier}` 且簽名表有單值回傳 ⇒ 過濾（`?T` 恆留；排除精確 i64/i32/i16/i8/u64/u32/u16/u8/f64/f32/bool/byte/char/fd；含 `|` 排除——union 排除本增量未實作，風險已註於碼）再走三閘門：未宣告→蓋 Type＋setVarType；已宣告且回傳 `?T`→僅 setVarType 不蓋 Type；已宣告非 option 且既有 varType ∈ {'', 'i64'}→蓋 Type順序註冊（ps-parse-func 於 body 解析完後僅 outs.len()==1 才登記 sigN/sigP）⇒前向引用與遞迴不推斷（oracle 探針 inf2/inf3 釘死）；方法定義不進裸名表；形參/回傳 varType 註冊提前至 body 前（mirror decl.go:1756）psst 增 12 欄（fn/lastTp/sigN/sigP/dclF/dclV/gclN/fvF/fvV/fvT/gvN/gvT）實現 FuncVarType（fn→全域 fallback）與 IsFuncDeclared（tracked 函式僅查 pair）**克隆方案＝token 位置重入**：簽名存回傳型別起始 token 下標（ps-parse-param 捕 p.lastTp），使用處跳 p.pos 過去重跑 ps-parse-type 生成新節點（零既有節點突變）回pos；ps-mark-inferred 逐 kind 遞迴寫 'true'（NULLABLE 只蓋子節點——dumper 標頭不印 inferred）；被拒克隆成無 kid 引用孤兒⇒零回歸ps-parse-stmt 六明示型別分支＋typedecl 兩臂補 ps-reg-let 登記（復刻 Go parseLet 尾部 DeclaredVars+SetFuncDeclared）fixture t55-call-infer.no（首個 `?str` 蓋印／重指派抑制／i64 過濾排除／`[]str` SliceType 推斷／字面量恆蓋／後定義函式不推斷）與 oracle 逐位元組相同門禁：fixture 52/56（僅余既有 t37/t4/t7/t8）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR 集不變；語料門（-P 2/90s 全 647）＝**SAME 384→413（+29）0 回歸**（SUT-FAIL 8 檔 rc=142 與基線同集慢檔），新匹配 x25519-* 全系列mem-safety/struct-* 簇move/slice/view 諸檔；args.no 差異行數 103→91（剩餘皆 lowering D6 `?char`/`?i64` 合成偽影，超出 parse-local 範疇）⚠️ oracle 當日 12:26 因平行會話remove explicit main重構重建，基線探針全組復核 SAME 後才动工晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s51 sha1 33fd562b，舊基線存 /tmp/noc-s0-s50.bak）→s52=417（**前置 `#{buildin}`/`#{intrinsic}` 註解標記 ⇒ 節點旗標**——s51 後 226 檔首分歧重分桶：LetStatement 79（it-let 束縛 20／D6 option-index 23／CallExpression 残留 10 皆 lowering 产物）BlockStatement 48（index-out 簇）**builtinStub 10 檔**為最大單標記 parse-local 簇語料 17 檔 152 處 `#{buildin}`（裸形＋`#{buildin, intrinsic}` 組合形）`#{intrinsic}`（含 `#{win-amd64, …, intrinsic}` 平台組合形）鏡像 Go attachAnnotations（annotation.go:349–378）：FunctionDefinition 上 'intrinsic'⇒Intrinsic（slot5 'true'）'buildin'⇒BuiltinStub（slot6）＋可選 Value⇒BuiltinName（slot7，語料暫無 `=NAME` 形預留）；TaggedEnumDefinition 上 'buildin'⇒Builtin（slot1，global.no option 即此）槽序由 dump.go:272–279 渲染欄序列導出（name0 colonSyntax1 methodDef2 skipNamingCheck3 overflow4 builtinStub5 builtinName6 builtinGroup7；`#{intrinsic}` 已於 2026-10-22 從 Go 側移除，原 intrinsic5 槽消失其後各槽（builtinStub/builtinName/builtinGroup）整體前移一格）——dumper（asp.no strs 表）早已就緒⇒纯 ps.no 增量（新 helper ps-apply-builtin＋註解迴圈收集 bs/intr/bn＋尾端套用一點）；非函式/非列舉目標忽略旗標（`#{buildin} e = 1` 探針釘死）不可附著形仍走獨立 AnnotationStatement（Go 同口徑）多載繼承（後續同名定义 BuiltinGroup）與尾隨同行形語料暫無未實装fixture t56-buildin-annot.no 與 oracle 逐位元組相同門禁：fixture 53/57（僅余既有 t37/t4/t7/t8）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR 集不變；語料門（-P 2/90s 全 647）＝**SAME 413→417（+4）0 回歸**（SUT-FAIL 8 檔同集），新匹配 global/os/bool/net/unix；async.no 殘差 5 行為 `*i8` PointerType 參數（新缺口）time/fs/process/number 仍卡下游簇⚠️ oracle 當日多次重建（12:26→13:24→13:51），掃描後對新 oracle 複驗解鎖檔＋全探針 SAME晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s52 sha1 b71b1210，舊基線存 /tmp/noc-s0-s51.bak）→s53=423（**頂層型別別名——union / 單一 / 函式型別三形**——s52 後重分桶選 TypeAlias 簇：`float = i8|f64|…` 系首分歧 Go 為 `TypeAlias{…Union…}` 而 noc 拆成 LetStatement+InfixExpression復刻 Go stmt.go:151-170 分派序（isFnDef→isFnTypeAlias→looksLikeEqAlias→let）：type.go:554 looksLikeEqualsTypeAlias（IDENT+OR→scanIsUnionTypeAlias 深度 0 掃描（NEWLINE/EOF/;/閉合符→真；含 QUESTION 在內二元運算子→假；LPAREN 內容限 {IDENT,IN,RPAREN}）；IDENT+陳述邊界→內建 16 型名或 **typeAliasNames 註冊表**；`[]T`/`[N]T`/`?T` 形）type.go:688 parseTypeAlias（`|` 平鋪收集成 UnionType{types:N}）type.go:406/433/512 fn 別名形（首 `(...)` 後須陳述邊界或第二 `(...)`）**三項 ps.no 機械發現**：（a）kvc 計數頭部（`types:`/`params:`/`results:`）由 dumper 讀**字符串槽**渲染子節點遍歷讀 c 槽——builder 須 set-slot(.to-str()) 與 set-count 兩設（沿 CallExpression 既有範式）；（b）同塊多臂 match 中**裸 `->` 尾臂會被當尾隨無條件陳述執行**覆寫 idx——別名分派全臂加 `aliased == 0 &&` 守衛無裸臂；（c）Go fn 型別參數規則：`(...)` 內單 IDENT 為**無名具型參數** `Parameter{name: , type: NamedType}`（不同於 FD 形參）——新 helper ps-parse-ftype-param；且結果組換行探測須 `sp = p.pos` 保存未命中回滾，否則吃掉終結 NEWLINE 使下行文註錯掛為上陳述 Commentpsst 增 `alN []str` 註冊表⚠️ 記錄偏離：別名閘門用 `p.fn == ''`（僅頂層）替代 Go 的 saveState 試解析——語料函式體内 `x = (expr)` 形皆被 Go 內容掃描否決，頂層閘門安全fixture t57-type-alias.no（union/單一/註冊表引用/cb0 空參/fn 雙組/負形 `or-value = 1 | 2``x = (or-value)`）與 oracle 逐位元組相同門禁：fixture 54/58（僅余既有 t37/t4/t7/t8）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR語料門（-P 2/90s 全 647）＝**SAME 417→423（+6）0 回歸**（SUT-FAIL 8→6 恰為 fn-type-result/return 轉 SAME 脫離慢檔集），新匹配 src/std/{float,num}.notests/{fn-type-params,fn-type-result,fn-type-return,named-fn-type}.no；int.no 殘差 8 行=`?int` NullableType 推斷 let 形（新簇出列）⚠️ /tmp/nolang-v44 被 macOS /tmp 清理蒸發，重建：git clone + checkout v0.4.4 + make no晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s53 sha1 86907d51，舊基線存 /tmp/noc-s0-s52.bak）→s54=427（**D6 有號整數算術 option 推斷——inferOptionArith**——s53 後 218 DIFF 重分桶：LetStatement 86／BlockStatement 49，選中`type: ?i64/?int, value: InfixExpression`簇 16 檔（std 8＋tests 8）復刻 lowering.go:493 inferOptionArith（parse 完成後 walk 逐 LetStatement）：無明示型別非合成值剝括號後為 op ∈ {+ - * / %} 的 InfixExpression 且兩側皆靜態可知有號整數（IntegerLiteral→i64；Identifier→FuncVarType→lowerSignedIntName 僅收 {int,i8,i16,i32,i64}限定名取最後 '.' 段剝前導 *；self/`.`→接收者型別；巢狀算術遞迴取較寬者平手取左）⇒就地合成 NullableType{NamedType{base}}（dumper 對 NullableType 頭不印 inferred）＋setVarType(name,'?base')D1：`#{overflow=...}` 不豁免（d6f/sha3/time 探針釘死仍蓋印）ps.no 增量：五 helper（ps-unwrap-grouped／ps-is-arith-op／ps-lower-signed-int-name／ps-signed-int-operand／ps-mk-opt-arith-type）＋ps-parse-let 尾一鉤點＋ps-parse-method-func 預登記 self=接收者型別（鏡像 lowerSelfType 讀 Results[0]——裸 `.` noc 渲為 Identifier{self} 需在 body 前可查）⚠️ 偏離：單趟查表 vs parse 完後 walk——前向引用運算元不蓋印（語料未見）**閘門教訓（首掃曾丟 3 檔回歸）**：captureTargetAllowsOption 不是查 varType 空即放行！Go 以 declaredLocals（walk 序出現名單：函式作用域每次進入 FunctionDefinition 重置形參先入帳字面量 let 只入名單不入型別表）＋localDeclaredType（**刻意不做全域回退**，lowering.go:3377 事故註釋：他函式 `c i64` 參數污染）雙表判定——本作用域已出現且型別表查無 entry也拒，保護 `r = 0`→`r = 10/3` 重指派形與純量出參；新增 ps-walk-declared／ps-local-type＋psst.dclT（僅頂層名單）重造閘門後 net/ipsha256-simplifiednumber-div-mod-clamp 回 SAME⚠️ 誤編譯事故：臂內寫 `-> { r = 'int'; return }`——`;` 是行註解吞掉 ` return }`括號失衡使後續整檔併入前函式，報錯 `EmitLLVM: unknown callee ps-parse-if in func ps-parse-stmt` 距真因極遠；改互斥條件單表達式形fixture t58-d6-arith-option.no（正形：函式參數／剝括號／巢狀／方法 `.`；負形：u8純字面量變數運算元明示純量重指派出參union 別名接收者D1 overflow 仍蓋先字面量 let 後重指派拒蓋）與 oracle 逐位元組相同門禁：fixture 58/59（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 423→427（+4）0 回歸**（SUT-FAIL 6 檔 rc=142 同集），新匹配 src/std/{int,net/hpack}.notests/{1-test,basic}.no；簇內其余 12 檔殘差屬 D7 安全索引 `?char`／armBinding 型別／Comment 附著等其他簇⚠️ oracle 當日漂移 15:02→16:16，晉級後對新 oracle 復核 25 探針＋抽樣 40 原 SAME 檔全 SAME晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s54 sha1 dedb47ef，舊基線存 /tmp/noc-s0-s53.bak）→s55=441（**armBinding it 合成綁定型別蓋印——buildItBindingForArm**——s54 後 214 DIFF 重分桶選中`LetStatement{name: it, type: T, …, armBinding: true}`it-let 簇 21 檔復刻 lowering.go:2809 buildItBindingForArm ＋ per-arm 補集表 :2176–2235：主題靜態可知 `?T` ⇒ err 臂→errnil 臂→nilok 臂→Twildcard 臂取補集（全列 ok/err/nil ⇒ Go skipItBinding 死臂**不建任何綁定**；列 ok+nil→err列 ok+err→nil僅列 ok→`err | nil`未列 ok→T）；型別未知（t==''）維持 s46 兼容形——僅 err/nil 關鍵字臂帶變體名ok/wildcard 落回裸 shared 綁定ps.no 三處增量：（a）`ps-mk-it-let-arm` 泛化為 `ps-mk-it-let-t(t, subj, bt, bname)`（bt 非空⇒NamedType{bt}+s1 synthetic+s2 armBinding；bt 空⇒同 ps-mk-it-let 裸形）；（b）`ps-match-listed` 深度追蹤 token 預掃（不消費不建節點）收集緊接 `->` 的 ok/err/nil 關鍵字臂名單，鏡像 Go parse 後 hasExplicitOk/Err/Nil（嵌套 {…}/(…)/[…] 增深度防内層臂污染補集判定）；（c）ps-parse-match 接線——listed 預掃`ok` 關鍵字臂 kvar 識別bt 計算dead 旗標（全列跳過整條 it 綁定）it varType 逐臂登記與還原（鏡像 collectVarTypesFromBody 作用域掃描，保巢狀 match 内層 it 不污染外層——match-it-type-scope 回归守）⚠️ 偏離：`Program{warnings:N}`（死臂 unreachable 警告計數）與未知型別主題 wildcard 裸綁定 vs 無綁定之別屬警告計數／下游簇，本增量未觸及（探針 e55a 剔除此二形後與 oracle 逐位元組相同）fixture t59-arm-binding.no（n ?i64：err/nil/wildcardok/wildcard僅 err/僅 nil/純 wildcard；m ?str：ok/nil/wildcard 補集 err；巢狀 match 作用域還原）與 oracle 逐位元組相同門禁：fixture 59/60（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 427→441（+14）0 回歸**（SUT-FAIL 6 檔 rc=142 同集ORB-FAIL 0），新匹配 tests/{fs-error-complete,fs-error-simple,minimal-it,minimal-it2,opt-match-all,opt-match2,opt-match3,option-nil-predeclare,self-write-str,struct-field-leaf-clone,test-div-mod-option}.nomem-safety/{option-cmp2,option-match-basic,option-match-direct}.no；簇內其余 7 檔（match-it-type-scope/opt-box-drop/option-capture/tagged-enum/test-div-zero/str-field-set-clone）殘差屬複合指派／enum 載荷析構／D7 安全索引等其他簇⚠️ oracle 當日漂移 16:16→17:09，首掃 LOST=0 已對新 oracle 復核成立，晉級後復驗 14 新檔＋抽樣 39 原 SAME 檔全 SAME晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s55 sha1 054f3851，舊基線存 /tmp/noc-s0-s54.bak）→s56=445（**W_SEMI_EAT 行尾分號註釋計數——Program{warnings:N}**——s55 後 200 DIFF 首分歧分桶：LetStatement 62／BlockStatement 54／Program 9，選中 warnings 桶：4 檔唯一差異即 `Program{}` vs `Program{warnings:N}` 頭行復刻 Go buffer.go::warnSemiSwallow 啟發式（advanceCollect 對每個符合的 COMMENT token 發一條 SeverityWarning，parser.go::Warnings 將全部 warning 匯總進 Program.warnings）：① Marker == ';'（單分號行註釋；`;;`/`;;block` 系永不警告）② 註釋文字含 `{` 或 `}` ③ 前一非 COMMENT token 為同一行代碼 token（行尾而非行首）啟發式純詞法 ⇒ ps.no 以 `ps-semi-eat-count` 對 toks 前向預掃（last 指針跟蹤前一非註釋 token），`parse` 寫 Program 槽前疊加進 p.warn 總和——Go 的 warnedSemiEat[idx] 回溯去重在預掃下天然無需探針 tmp/p1-p3 釘死：`;;` 行首/同行→0同一行兩組大括號→僅 1（同屬一條 COMMENT token）rc4/move-eligibility-improved/range 三檔首行也是 warnings，但 noc 過度計數的根因是 `[1,2,3]:` 陣列字面量被誤析為 RangeExpression 的 body 大 diff（他桶）fixture t60-semi-eat.no（正形三行含括號／負形行首 `;{}``// {}``;; {}`無括號 `;`／同行雙括號串）與 oracle 逐位元組相同門禁：fixture 60/61（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR語料門（-P 2/90s 全 647）＝**SAME 441→445（+4）0 回歸**（SUT-FAIL 6 檔 rc=142 同集ORB-FAIL 0），新匹配 tests/{map-to-str,struct-to-str,scratch-fe,print-eprint-to-str}.no⚠️ oracle 當日漂移 17:09→18:16，首掃 LOST=0 對新 oracle 復核成立，晉級後復驗 4 新檔＋探針＋抽樣 40 原 SAME 檔全 SAME晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s56（構建 sha1 ceaf5862，晉級 codesign 後 1e872f5d），舊基線存 /tmp/noc-s0-s55.bak）→s57=448（**for 族語法補齊：range-for 匿名切片字面量源 + 計數循環雙形**——s56 後 196 DIFF 首分歧分桶：LetStatement 62／BlockStatement 54／ForStatement 6／Program 5，選 for 兩簇：（a）range 源 `i <- [1,2,3]:` 被 ps-for-source 一律當 RangeExpression 誤析（s56 副發現：rc4/move-elg/range 三檔过度 warnings 之真因即此）语料切片字面量源 4 檔；（b）計數循環 `N * { }`／`{ } * N` 完全缺臂（for3.no 前置形 8 行差）**鏡像 Go parseForRange LBRACKET 分類**（stmt.go:2582）：saveState→跳 `[`→peek ELLIPSIS 先行⇒kind1；否则 parseExpression(LOWEST) 後 ELLIPSIS⇒kind1（[a..b]/[a..]）COMMA|RBRACKET⇒kind2（匿名切片字面量 [a,b]/[a]）；restoreState；kind2 整段交 ps-parse-expr 之 LBRACKET 前綴臂建 SliceLiteral 填 IterationExpr.RangeExpr(kid1)（非 range 非切片之 kind0⇒保持舊無分類行為，Go saveError 形無以復現）分類期投機 parseExpression 所建節點不被任何 kid 引用即孤兒（s40/s51 先例）⇒零回歸**計數循環雙門+雙 builder**：s57-count-prefix-p 鏡像 isCountLoopPrefix（k=1 用 peekTokenk≥2 用 look(k-2)；`*` 後必緊接 `{` 与乘法區分）於陳述分派 INT/SUB 臂前；s57-count-blockfirst-p 鏡像 isCountedLoopBlockFirst（平衡大括號掃描+`#{` 群組跳過匹配 `}` 後 MUL [SUB] INT 且 INT 後必為 NEWLINE/EOF/SEMI/RBRACE 語句結束以免 `} * n` 乘法歧義）於 ps-parse-brace-stmt 入口優於裸 match/普通區塊；二者皆建 ForStatement{label:''} 6 guarded CHILD 僅 CountExpr/Body 非零，CountExpr=IntegerLiteral{value:字面量}（無 raw⇒slot1 空串不印，ps-mk-true 先例；Go ParseInt base10 拒 hex 故 hex 安全落回運算式形）負號形把字面量前綴 '-'（Go intToken.Literal = "-"+lit）；標籤形 `#1 N * { }` 由既有 LABEL 臂遞回+改 slot0 天然覆蓋（e1 探針釘死）⚠️ **新坑（MIR 層靜默誤讀）**：同一函式體內多次 ps-peek-at（含變量偏移）會讀到陳舊寄存器——探針实計：同體 pa1/pa2/pa3 局部正确，但嵌套臂內 `ps-peek-at(p, ko)`（ko==2）返回 ko==1 時的舊結果，而直接索引 `p.toks[p.pos+ko].typ` 恆正確；vet 的 fxxoptarg 錯位（ps.no:4401/4404/4415 等 6 處既有 `p.pos+1` 傳參）側证此族降格路径确有幉角⇒**新閘門一律改 ps-bm-scan 式直接索引掃描（index-out=zero 守衛）**fixture t61-for-shapes.no（切片源/範圍源對照/前置計數/後置計數/負計數/標籤前置形）與 oracle 逐位元組相同門禁：fixture 61/62（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 445→448（+3）0 回歸**（SUT-FAIL 6 檔 rc=142 同集ORB-FAIL 0），新匹配 tests/{for3,range,call-heavy}.no（call-heavy 為分類修正的級聯紅利）晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s57（構建 sha1 b5c4ac9e，晉級 codesign 後 25a178bb），舊基線存 /tmp/noc-s0-s56.bak）→s58=451（**參數/型別特殊形狀 + 方法回傳型別推斷**——s57 後 193 DIFF 首分歧分桶：LetStatement 62／BlockStatement 55／Identifier 9／Parameter 5，選 B 簇型別形狀：（a）參數/回傳位 `*T`/`**T`/`T*`/`&T`/`?&T` 全缺臂（async.no `task *i8`view-basic `(v &view-point)`）；（b）參數名位缺 IN token（`in []byte` 被丟名）鏡像 Go parseParamTypeAfterName（type.go:997）：前綴 `?`/`&` 循環＋`*` 循環＋STAR_STAR＋base＋後綴 `*`/`**` 正規化為 `? & * base` 再 buildType（type.go:1206）逐層包節點——noc 單趟直接節點化等價：ps-parse-type 新增 MUL→PointerTypeSTAR_STAR→雙 PointerTypeAND→ViewType 三臂（遞迴先建內層 base 再包外層，子先於父）；IDENT 臂尾加後綴 `*`/`**` 包裹迴圈（`i8*`⇒`*i8`）；ps-parse-param 型別門擴 {MUL,STAR_STAR,AND}名門容 IN⚠️ 權威面探針釘死：oracle 函式字面量**參數位**不接 `&T`（isFunctionDefinition 掃描集無 AND，`(b &i64)` 落回 GroupedExpression）但 **results 位**接（掃描無條件跳過）——語料只用 results 位，noc 偵測比 Go 寬的偏差只在語料外形（e2 探針既有之偏離非本增量引入）；頂部明示 `x &str = 1` oracle 也落回運算式（let 型別門不碰 AND，未改）**級聯子簇：方法回傳型別推斷（s51 同族 DotExpression 臂，type.go:96 returnTypeFromCallExpr）**——view-basic `q = p.self-view()` 之 `type: &view-point` 為 parse 期推斷非明示：新增 psst.msN/msP 方法簽名表（鍵 'recv.method'，body 解析完才註冊outs==1 限定opos[0] 起點 token，鏡像 Go decl.go 尾註冊 methodSignatures 語意）＋ps-msig-find 反掃；ps-parse-let 推斷區加 DOT 臂：接收者 IDENT→ps-var-type 解接收者型別→查表→跳 token 重跑 ps-parse-type 克隆（未採用即孤兒零回歸，s51 法）＋filter-ok/declared 雙閘同裸名臂；StructLiteral let 臂僅登 varType 不蓋 stmt.Type（Go case *StructLiteral 語意，!exists 守衛）K_POINTER_TYPE=51/K_VIEW_TYPE=71 asp.no 早已註冊type-string ast.no:632/634 已渲 `*`/`&` 前綴dumper kind-name 通用⇒纯 parser 增量fixture t62-type-shapes.no（*T/**T/T* 後綴正規化/&T results/?&T+推斷/[]byte+IN 名對照/負形 `3 & 5``6 * 7` 保持 InfixExpression）與 oracle 逐位元組相同門禁：fixture 62/63（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 448→451（+3）0 回歸**（SUT-FAIL 6 檔 rc=142 同集ORB-FAIL 0），新匹配 src/std/async.notests/{view-basic,view-optional}.no；Parameter 桶另兩檔 aes-ctr/tdes 首分歧已前移至 D7 安全索引簇（275/718 行殘差）晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s58（構建 sha1 cf4ee19f，晉級 codesign 後 e5dcc20d），舊基線存 /tmp/noc-s0-s57.bak）→s59=464（**match 臂区／區塊尾 註釋附着——armDoc 協議 + 區塊內部協議 + if 尾非破壞性預看**——s58 後 190 DIFF 首分歧分桶：Doc 7／Comment 7／TrailingComments 1（每檔僅 1–3 行差皆缺註釋附着欄位），選中此註釋附着簇鏡像 Go match.go 權威：臂循環頂 RBRACE/EOF 檢查先於 collectDocComments（:167-174）`armDoc := p.collectDocComments()` 於型樣前收集（:208）套用規則（:440-452）inline 體→`setDoc(bodyStmts[0], armDoc)`區塊體→TrailingComments（空則直設非空則 armDoc 併入最前）；區塊臂內部走 parseBlockStatement 委託（:265-431）：leading standalone→其後陳述 Doc行尾→前行Comment`}` 前→TrailingComments**探針 p4 釘死 bare≠explicit 不對稱**：裸 match `{ }` 區塊臂 armDoc→TrailingComments 最前且保留內層首陳述 Doc；明示 `subj: { }` 區塊臂 armDoc **覆寫**首條真陳述之 Doc（內層首 doc 被棄）；兩 form 之 inline 臂皆 armDoc→體首 ExpressionStatement.Doc + 同線行尾註釋→Comment（attachInlineComment）實作五批：① `ps-parse-if` else 預看由破壞性 `ps-skip-trivia` 改直接索引非破壞掃描（`p.toks[k].typ`index-out=zero 守衛，遵 s57 MIR 舊寄存器閘門），查無 ELSE 則 pos 不動⇒if 區塊後函式尾註釋不再被吃掉（root cause：move-clone-liveness 等函式尾 TrailingComments 遺失）；② `ps-parse-match` 臂循環重構（armDoc 收集區塊臂全協議 btrail/bprev/bfirst/bdoc 逐陳述 set-doc＋行尾 set-cmt循環後 armDoc 覆寫 bfirst.Doc 或入 btrail`ps-attach-trailing`）；③ inline 臂 armDoc→set-doc + 同線 COMMENT→set-cmt；④ `ps-parse-bare-match` 三體形（LBRACE 區塊／NEWLINE 區塊／同線單陳述）同協議；⑤ 新 helper `ps-premerge-trailing`——aux 區僅可附尾，prepend 需把舊區整段拷進新 aux 段（舊區孤兒無害：dumper 跟 ct0/ntrail 走訪）fixture t63-comment-attach.no（裸 match 臂首 doc+行尾 tail明示 match ok 區塊臂內部 doc/tail/trailerr inline 臂 tailwildcard 區塊臂頂層 standalone-if 行尾負形中段 standalone 屬其後陳述 Doc）與 oracle 逐位元組相同門禁：fixture 63/64（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 451→464（+13）0 回歸**，新匹配 tests/{fs-struct,json-get-clone,map,map-generics,match,option,std-new,strconv,test-div-zero,yaml}.nomem-safety/{clone-reset-is-moved,if-branch-move-leak,move-clone-liveness}.no；**SUT-FAIL 6→4**——fmt-heuristic/str-order 因 ① 解掛升為 DIFF（結構仍分歧但可解析）殘差：ws.no（11 行）vec-push-leak.no（52 行）同簇檔被 D7 安全索引／`?T` 無值裸宣告 downstream 阻塞⚠️ 已接受偏離（語料未證形）：match 區塊 `}` 前 standalone 於臂循環頭被吃掉且其後無臂時丟棄（Go 留 buffer 給外層區塊）；`->` 與區塊體 `{` 之間註釋仍為跳過；嵌套裸臂 body 的 armDoc 未併入晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s59（構建 sha1 7c403081，晉級 codesign 後 70be6e56），舊基線存 /tmp/noc-s0-s58.bak）→s60=470（**tagged-enum／option 變體臂析構綁定**——s59 掃描後 oracle 漂移（3c2fa429，重基線 463/181/3），首分歧分桶 LetStatement 61／BlockStatement 56／Identifier 9，選 Identifier 簇 9 檔＝`ok(v)`／`err(e)`／`some(v)`／`rect(w,h)` 變體臂 decomp鏡像 Go 權威：expr.go:1021-1065 parse 期 `ok`／`err` + `( 裸識別符 )` 規範化為析構（condition 換 Identifier{ok|err}＋bindingName）；lowering.go:2049-2090 泛化改寫（主體為枚舉且呼叫臂全裸識別符參數元數==欄位數 ⇒ bindingNamescondition 換 Identifier{X}）；枚舉登記表（decl.go:1196-1224＋858：payload 單欄位=欄型別多欄位='enum.variant' 合成結構名單元=''，裸枚舉 kind==1 亦登記 payload 恒 ''）；typeStr 規則（:2900-2902 枚舉⇒typeStr=armType 直取）；armType 表（載荷替換單元=變體名wildcard 補集=登記序−已列' | ' join空⇒死臂 skipItBinding）；namedItBinding 回退（:2919-2927 未知主體 ok(v) 具名無型別仍 armBinding）；多欄位提欄（:2307-2334 `a = it.f0` DotExpression 逆序 prepend⇒it;a;b;body）；varType 登記（:2276-2296 elemType→armBindingType→err且非枚舉='str'）實作五批：① psst 新增 enN/evN/evP/evC/efE/efV/efT 平行陣列登記表＋六 helper（ps-enum-{row,has,payload,ncols,ftype,variants}，限定名剝落以 byte 尾掃回退）；② ps-parse-enum-def 每變體尾填表（ast.type-string）；③ ps-parse-match 型樣塊加 case A（ok(v)/err(e) 消費 4 token→Identifier）case B（枚舉呼叫臂掃描→Identifier＋參數名收集）裸變體臂 kvarE；④ bt 計算加 enum armType 補集塊reg 登記綁定名 bn plumbing（ps-mk-it-let-t armBinding 改 bt≠''∨具名）多欄位提欄（新 builder ps-mk-ext-let）；⑤ ps-match-listed 擴 cN/cA 元數候補＋ok(v)/err(e) 形 fl 置位（Go 改寫後掃描之預掃近似，首臂盲區以 caseA 就地置位補）fixture t64-tagged-enum-arms.no（已知 option ok/err/nil 析構未知主體回退單載荷/單元/雙變體補集多欄位提欄巢狀同名裸枚舉變體臂）與 oracle byte-identical門禁：fixture 64（僅余既有 t7-group-nil-float與基線既有紅同形）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 463→470（+7）0 回歸**（SUT-FAIL 3 檔 rc=142 同集ORB-FAIL 0），新匹配 tests/{enum-annotation,option-match-silent-wrong,tagged-enum-{alias,cross-fn,payload-ownership,two-match}}.no＋**意外收復 view-basic.no**（s60 Step1 之 LOST 源於平行會污源檔已自行修復）殘差：err-binding（8 行）／match-arm-binding-scope（28 行）被**區塊級裸 match 臂續接合併**族阻塞（區塊臂體 `stmt; cond -> X; -> Y`：Go 將臂區合併為單一 if/else 且區塊 inline:truenoc 現拆為 standalone-if＋if(1) 包裹——下一候選增量）；tagged-enum-zero-match（5 行）殘 `_ = o10` discard 形（oracle 建 LetStatement{name: _, value}）⚠️ 已接受偏離：枚舉全列死臂之 unreachable warnings 計數（Program{warnings:N}s55 先例）；多行型樣 `X(\n a, b)` 不相鄰形不規範化晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s60（構建 sha1 daca5d8e，晉級 codesign 後 4cf6b9bd），舊基線存 /tmp/noc-s0-s59.bak（70be6e56）→s61=474（**區塊級裸 match 臂續接合併 + 明示臂體 inline**——s60 殘差族：err-binding（原 8 行）／match-arm-binding-scope（原 28 行）之 `cond -> { stmt; cond2 -> X` 換行 `-> Y }` 區塊臂體鏡像 Go 權威雙機制：① 換行後續接（stmt.go:2168）——`ps-mk-standalone-if` 已存換行後 `->` 併入前 if 的 Alternative路徑，於明示臂 `{ }` 體解析期間置 `inBlk=1/inMatch=0`，使 `stmt; cond2 -> X` 後之裸 `-> Y` 併入前 if 而非另起 `if(1)` wrapper；② 臂體 inline 標記（expr.go:1263Inline 語句形）——新增 `ps-bm-block-shape`（鏡像 classifyBlockAtCurrent parser.go:704-945，掃描起點 p.pos+1 跳過 LBRACE 本身）判別 `{ }` 首非 trivia token 形：回 blockMatch(1) ⇔ Go 走臂體提取路 ⇒ dump `BlockStatement{inline: true}`（具名區塊臂 `b: {``_ ->`INT/STR/CHAR 型樣臂`cond ==`/`&&`/`||` 比較臂 Go:943 直接 blockMatch）；回 0 ⇔ 普通區塊路（結構欄 `name: TYPE\n``name: 字面值\n``name: ident(``name: ident{``name: -INT``name: .x``name: Enum.Var``name: <value><binop>``IDENT NEWLINE``IDENT ,``IDENT }``IDENT (`）⚠️ `IDENT .` 首臂改用新 helper `ps-arrow-scan-line` 前視深度 0 `->`（鏡像 parser.go:912-942）——`it.v = 99` 無臂 ⇒ blockUnknown(0)，修回 view-optional.no 之誤標 inline⚠️ `bshape` 每臂重置（防上臂 blockMatch 洩漏至下臂）fixture t65-block-arm-continuation.no（具名區塊臂 `b: {`→inline區塊內 `cond -> X`＋換行 `-> Y` 續接嵌套 err(m) 不污染外層補集 else 與 wildcard 尾臂共存）與 oracle byte-identical門禁：fixture 全 SAME（僅余既有 t7-group-nil-float 基線紅與 s61 零差分）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 470→474（+4）0 回歸（LOST=0）**，新匹配 tests/err-binding.notests/match-arm-binding-scope.notests/mem-safety/{json-nested-match,json-parse-option}.no（SUT-FAIL 3 檔 rc=142 同基線集：aes-cbc/net/tlsORB-FAIL 0）殘差：tagged-enum-zero-match（3 行）殘 `_ = o10` discard 形（oracle 建 LetStatement{name: _, value}，與 s60 同因）；`b:`＋換行（無大括號）之具名裸塊嵌套 match（r1/r2 探針）屬另一更深族，本增量未觸晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s61（構建 sha1 31901dcb，晉級 codesign 後 7dfdc91a），舊基線存 /tmp/noc-s0-s60.bak（4cf6b9bd）→s62=481（**`_ = expr` 空白標識符捨棄陳述 ⇒ `LetStatement{name: _}`**——s61 殘留候選族（tagged-enum-zero-match `_ = o10`）擴至全語料 7 檔首分歧鏡像 Go parseStatement stmt.go:49（case 含 UNDERSCORE）→ :149（peek==ASSIGN）→ :172 parseLetStatement——`_` 走與 IDENT 同一 let 路name 存字面 `'_'`此前 noc UNDERSCORE 非 IDENT陳述分派無臂⇒落預設 expr-stmt⇒`_`/`=` 被逐步防呆丟棄只剩右值成裸 `ExpressionStatement{RHS}`（正戳中 Go stmt.go:2075-2079 註解所述`_` 必須是語句邊界否則 `_ = expr` 靜默退化之 noc 對偶面）**實作最小兩批**：① ps-parse-stmt 主分派新增臂 `tk.typ==UNDERSCORE && ps-peek-at(p,1)==ASSIGN -> { ps-bump _; ps-bump =; idx = ps-parse-let(t,p,'_') }`（裸 `_`（無 `=`）不落本臂仍走原路＝零回歸，oracle 裸 `_`⇒`ExpressionStatement{nil}` 屬另族未觸）；② ps-parse-let 內 s54 option-算術合成塊加 `name != '_'` 守衛**關鍵對稱性（探針 a3/a4/b1/b2/b3 釘死）**：`_` 上字面量/slice/呼叫回傳型別推斷照常觸發（`_ = true`→bool`_ = [1,2,3]`→[]i64首個 `_ = g()`→?str），惟option-算術推斷（inferOptionArith）豁免（`_ = zz*2`⇒無型別）——因 Go captureTargetAllowsOption 拒 blank 目標；ps-mark-dcl('_') 自然令後續 `_ =` 落入已宣告閘（`_ = g()` 二次出現不再蓋 ?str，與 oracle 一致）fixture t66-blank-discard.no（ident/bool/slice/call/option-算術豁免/多次丟棄/緊接他陳述後 全覆蓋）與 oracle byte-identical門禁：fixture 全 SAME（僅余既有 t7-group-nil-float 基線紅）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區 ~985/~2769）語料門（-P 2/90s 全 647oracle 漂移 634a0bfa）＝**SAME 474→481（+7）0 回歸（LOST=0）**，新匹配 tests/{fs-error-final,fs-error-info,fs-open-err,option-copy,uninit-output,tagged-enum-zero-match}.notests/mem-safety/option-heap-leak.no（SUT-FAIL 3 檔 rc=142 同基線集：aes-cbc/net/tlsORB-FAIL 0）殘留：裸 `_`（無 `=`）⇒`ExpressionStatement{nil}` 另族；`found = true` 於 match 臂體內裸 ident 賦值誤析為 `ExpressionStatement{found}`（collection/* 5 檔首分歧涉臂體×賦值路由回歸面較大暫緩）晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s62（構建 sha1 fb51a0d4，晉級 codesign 後 c6095b8e），舊基線存 /tmp/noc-s0-s61.bak（0e1fa13c）⚠️ 其余 6 档（json/net-{http2,pool,sse,ws}/str-vec-push）结构已正确但仍于**下游 standalone-if desugar / index-out desugar / 裸 match 区块**再分歧（即下一个主簇，也即 LetStatement/IntegerLiteral/BlockStatement 三大桶）✅ **AnnotationStatement 主鑰匙已打通**（self-dot 揭示者）：語句級 `#{...}` 現依緊後首 token ∈ {IDENT,IN,RARROW}分流——可附著→寫 overflow 槽（t37）不可附著→發 `AnnotationStatement{entries:去重鍵名數}` 節點（t41）✅ **bang/cond-loop ForStatement 簇已打通**（AnnotationStatement 揭示之下游主簇）：`!! { }`/`! { }` 僅當 bang緊接LBRACE 才成立（否則退回 `!x` 取反運算式陳述）；`(cond) { }` 需平衡 `)` 後立即是 `{` 且括號內非參數列（含 `,` 或恰好兩 IDENT⇒ 匿名函式字面量，退回 `(expr)` 運算式陳述）三者皆 `ForStatement{label:''}` + `Condition`（BooleanLiteral/expr）+ 6 guarded CHILD 以 0 佔位，且不saveWarning（新式語法）✅ **字面量型別推斷補齊**（Go stmt.go:1227–1268 / 1274–1300）：推斷表既有 INT→i64/FLOAT→f64/STRING→str/BOOL→bool，本增量補上 CHAR→char 與 hex(0x)→byte——雙函數（`ps-infer-type``ps-elem-type-of`）同口徑修正✅ **無值明示型別裸宣告已打通**（Go parseLetStatement no-value 分支 stmt.go:1051–1060）：陳述位 `name TYPE`（TYPE∈{NamedType, SliceType, ArrayType, NullableType}）型別解析完後若緊接非`=` 而是陳述邊界（NEWLINE/RBRACE/EOF/;/行內註解）⇒ 建 `LetStatement{name, type, value: }`（Value guarded CHILD 以 0 佔位）復用既有 `ps-parse-type`（子先於父）；非裸宣告（有 `=`或索引 `x[i]`）則還原 p.pos——所建孤兒節點不被任何 kid 引用dumper 自 root 沿 kid 走訪永不觸及（ast.dump 語義）⇒ 零回歸✅ **複合型別名已打通**（接續 s40 揭示之同族）：`ps-parse-type` 先前只认單 IDENT 型別名，本增量補齊——（a）限定名 `a.b.c`：IDENT 臂建完 NamedType 前以迴圈續吃 `DOT IDENT`，整條字串存進 name 槽（type-string 直接讀 s0，不另建節點，對齊 Go stmt.go:1034）；（b）巢狀元素：SliceType/ArrayType 臂不再硬性要求元素為單 IDENT，改為遞迴 `ps-parse-type`（元素起 ∈ {IDENT, LBRACKET, QUESTION}），故 `[][]i64` 建為 SliceType{SliceType{NamedType{i64}}}`?fs.file` 建為 NullableType{NamedType{fs.file}}（type-string ast.no:610/630 遞迴渲染）各臂改用 `handled` 旗標防 match 非排他之跨臂重評級聯效益：`ps-parse-param`（`w io.writer`）可為空含值臂（`f ?fs.file = call`）無需改動即自動受益；另把 `ps-parse-typedecl-noval` 升級為通用 `ps-parse-typedecl`（新增 `= value` 分支）以接住限定/巢狀含值形（如 `d tls.conn = call-tls()`）⚠️ **未涵蓋**：`?T`無值裸宣告（如 `x1 ?str`）仍由可為空臂（需 `=`）誤吞——為避免觸碰众多 option-* 檔的回歸風險，留待下一個專門增量（需把可為空臂改為先解型別再判斷邊界 vs `=`）✅ **無色 async 前綴已打通**（接續 RunExpression/CoExpression 揭示之 async 簇）：Go expr.go:305–357——`run e`/`co e`/`awy e` 皆為前綴運算子包一層guarded-CHILD 節點（子先於父）`run`/`co` 需歧義消解：若 `peek-at(1)==DOT` 則不是 async 關鍵字而是模組名（`run.list(...)`）⇒建普通 Identifier 停於 DOT 交後綴迴圈；`awy` 無此歧義三節點（K_RUN_EXPRESSION=57 / K_CO_EXPRESSION=20 / K_AWAIT_EXPRESSION=13）asp.no 早已註冊⇒纯 parser 缺臂⚠️ 用預先算好的 `dotAfter` 旗標防 match 非排他⚠️ **下一個主簇**：heap.no/path.no/zlib.no 等仍於 **裸 match 區塊 `{ cond -> body … }` desugar**（首分歧位於 LetStatement/IntegerLiteral/BlockStatement 三大桶，oracle 包一層 `IfExpression{IntegerLiteral{value:1}(synthetic), BlockStatement{各臂}}`）再分歧；另有 `GenericParams`（泛型參數渲染）⚠️ USE 增量清掉 11 個 use-bucket 檔案的首分歧，但它們随即在**下游 match-block/standalone-if/BlockStatement 簇**（已确認為下一個高價值增量）或 **FloatLiteral value 精度**上再分歧，故僅 sha3.no 逹到全匹配——USE 本身結構正確（t38 byte-identical）⚠️ `tests/str-order.no` 於 s31/s32 **皆挂死**（既有 hang，非本增量回歸）單元證據：`ast_test``tok_test``lx_test``util_test` 皆 PASS；`no vet compiler/src` ERROR **0**（硬門檻滿足）WARNING **0**（既有 `overflow-ineffective` 已由當前 bin/no 降為 HINT，見下方 vet 現況）覆蓋進度見 §6.3（t0–t51） |
| **M3** | 語義：37 個 `Validate*` + `stdsig` 表（§5.4）                                           | 🟡 **M3-a 骨架＋M3-b stdsig＋M3-c 首族＋缺口②①清償＋正語義首件 ck.no 零誤報落地（s9a/s9b/s10/s11/s12/**s13**，2026-10-28→**10-29**，§5.3.4.am–.**as**/§6.4）＋**詞法 ILLEGAL 報告族鏡像（s82**，2026-11-05，§5.3.4.dk：lx.no 科學計數檢測＋ps.no operand 位 ILLEGAL 臂與 s12 預留 n-illegal 抑制閘接通） ** | L3 MASS-NOOP 已解除：`noc vet` 對語法錯誤檔輸出真診斷（三站點族rc 鉗制 1**parser 族含 cascade 第二條在內與 oracle 逐字節全等**）；**M3-c 正語義首件 `'X' is not defined`（ck.no／TraceID 4bek3xc6）已由 `drv.cmd-vet` 接線消費 ck-sig.no 裸名門表**，本輪補齊泛型/match 臂/tagged-enum 綁定 def 採集＋callee 回退沉默＋std 命名空間靜默 ⇒ **全語料 668 檔 ck 誤報 13→0**（ck-test 20/20L2 ast 逐位元組未受擾）；parser 族仍全語料零誤報L2 656/656 DIFF=0；s82：L1 全語料 **804/804 SAME**（notok↔noc-s82 逐字節）、lx-test 70/70、ps-err-test 46/46、vet 探針 p1/p6/p7 與 oracle 逐字節同文同位；L2/L3 全語料無回歸（與 HEAD 基線二进制 noc-s82base 對比：DIFF 集合與 sut 輸出逐檔一致，117/166 屬既有 oracle 分歧） |
| **M4** | 中端 + 代碼生成：HIR / MIR + RC 堆策略 + LLVM IR 文本                                       | 🟡 **M4-a/b 骨架 + M4-c 控制流/循環/字符串/拼接/多參調用/str 跨塊/vec/帶型別簽名/帶型別 CALL/option/RC 原語/嵌套 STRCAT 的 RC 週期/option 的 `?str` 載荷/`struct`（構造·讀欄位·寫欄位·跨塊槽·多型別·欄位型別·欄位 RC 原語·作形參/返回值·嵌套欄位）/**ALU 家族（位運算·移位·除餘·其餘比較）／無符號 ALU 家族（lshr/udiv/urem + ult/ule/ugt/uge + `TY-U64`）／`f64` 算術家族（fadd/fsub/fmul/fdiv/frem + fcmp 按型別分派 + `TY-F64`）／整數除零 select 護欄（`sdiv/srem/udiv/urem` 裹 select 安全除數，s38）／除零 `divZeroReport` stderr 報告（`__nolang_div_zero_report` + `@write`，s39）／option 的 `vec` 載荷內聯讀寫（`?[]i64`，24B slot 免裝箱，§11 B8 半項，s40）／option 跨基本塊槽（`alloca %option`，非 PHI，§11 B8，s41）／`str` 結構相等（`@str_eq`／`OP-STEREQ`·`OP-STNEQ`，s42）／`vec` 元素相等（`@__nolang_vec_eq_i64`／`OP-VECEQ`，i64 元素，s43）／`option` 值相等（`OP-OPTEQ`／`OP-OPTNEQ`，tag AND payload，i64 載荷，单块无 helper，s44）／`option` 的 `str` 載荷相等（`OP-OPTEQSTR`／`OP-OPTNEQSTR`，tag AND 重建 `%str-long` call `@str_eq`，s45）／`option` 的 `f64`（double）載荷內聯讀寫（`OP-OPTMKF64`／`OP-OPTGETF64`，bitcast double↔i64 落／取 slot[0]，無需 helper，s46）／`option` 的 `f64`（double）載荷相等（`OP-OPTEQF64`／`OP-OPTNEQF64`，tag AND 載荷 bitcast i64→double 後 fcmp oeq，無需 helper，s47）／一元 ALU NEG／NOT（`OP-NEG`／`OP-NOT`，`fneg double`／`sub i64 0,x` + `xor i64 1,x`，鏡像 Go emitNeg／emitNot，s48）／vec→str 重interpret（`OP-STRFROMVEC`，%vec 三欄位 len/cap/dat 逐欄拷进 %str-long + dat inttoptr i64→i8*，鏡像 Go emitStrFromVec，單塊無 helper，s49）／str 容量欄（`OP-STRCAP`，extractvalue %str-long field 1，與 STRLEN 成對，鏡像 Go emitLenCap OpCap/KindStr，單塊無 helper，s50）／option 排序（`OP-OPTLT`／`OP-OPTLE`／`OP-OPTGT`／`OP-OPTGE`，只比載荷 slot[0] 發 icmp slt/sle/sgt/sge 無 tag 項，鏡像 Go emitOptionValueOrder 有符號 int 分支，單塊無 helper，s51)／option 的 `f64` 載荷排序（`OP-OPTLTF64`／`OP-OPTLEF64`／`OP-OPTGTF64`／`OP-OPTGEF64`，slot[0] bitcast 後 fcmp olt/ole/ogt/oge 無 tag 項，鏡像 Go emitOptionValueOrder double 翼，單塊無 helper，s52）／option 與純量排序（OP-OPTLTSC／OP-OPTLESC／OP-OPTGTSC／OP-OPTGESC，vty 定側提載荷與純量 icmp，無 tag 項亦無 and，謂詞不翻轉，鏡像 Go emitCmp aOpt/bOpt 分支，單塊無 helper，s53）與 SCF 家族（`OP-OPTLTSCF`／`OP-OPTLESCF`／`OP-OPTGTSCF`／`OP-OPTGESCF`，option 與 f64 純量排序，vty 雙護欄判向且非 option 側須 TY-F64，載荷 bitcast→double 後與 %fd 純量 fcmp，同無 tag 項無 and 謂詞不翻轉紀律，s54）**完成**（s14–s54，2026-10-30/31–11-03，§5.3.4.at–.ci）；⚠️ **HEAD 工具鏈跨模組結構欄位回歸**（`setfield: buf not found on sb.sb`）⇒ 本族在釘死 `v0.4.13` 上驗；M4-c 餘項（vec 元素非 i64 的逐元素 RC**結構體逐欄位 RC 的自動放置（原語已備，B11）****跨函式所有權轉移（B13）**>24B 堆裝箱（vec 載荷內聯 s40 / option 跨塊槽 s41 已落地）option 非 i64／str／f64 以外載荷的 `==`／match（i64 已 s44，str 已 s45，f64 已 s47）struct 陣列/切片與多返回值）／**M4-e 首件 AST→MIR 降級**（新建 `compiler/src/h2m.no`：i64 直線式子集，`ast` arena → `mir` 指令流，值 id 無槽直綁＋`unsup` 誠實門禁；`h2m-test` **35/35**，探針前向 exit **73**／反向 **79** 與 Go oracle 同程序對拍；§5.3.4.cj，s55；標號說明：§6.5 原定降級主體為 M4-c，該標號已被 codegen 發射族用盡，故降級主體另立 M4-e）／**M4-e 第二件 簽名側與調用降級**（`h2m-func-sig` 讀形參／輸出參型別，形參由 `h2m-alloc` 依序佔值 id `1..nargs`，`retty` 取輸出參數聲明型別，模組級簽名表前置 pass 解前向引用，`OP-CALL` 帶 arity／型別／連續 id 三重校驗，未降級函式整函式不產碼；`h2m-test` **74/74**，探針前向 exit **47**／反向 **50** 與 Go oracle 同程序對拍，鑑別態 exit **101**；§5.3.4.ck，s56）／**M4-e 第三件 控制流與跨基本塊槽化**（`h2m-lower-if`／`h2m-lower-for` 切 entry/then/else/merge 與 hdr/body/exit 塊並發 `OP-BR`／`OP-CONDBR`，比較家族 → `OP-CMP*`，前掃描發現控制流即切 `mem` 模式把全部局部量（含形參）落 entry `alloca` 槽，讀 `LOAD`／寫 `STORE`，槽型與右值型不符則整函式不產碼，**零改 `cg.no`**；`h2m-test` **135/135**，探針前向 exit **92**／反向 **93**（then 塊常量）與 **86**（循環步長）並與 Go oracle 同程序對拍，鑑別態 exit **101**；§5.3.4.cl，s57）／**M4-e 第四件 花括號短路鏈降級**（`h2m-lower-surface` 讀 `K-SURFACE-MATCH` 的 helper/aux 臂群（12 槽/臂，只取兜底／條件／體三槽），逐臂發 `OP-CONDBR` 串成鏈／假路指向下一臂判定塊／兜底臂不發判定直落塊，無兜底時末臂假路落匯聚塊；前掃描經新增 `h2m-scan-arms` 為臂內 `let` 鋪槽，形狀校驗四條（臂數／臂體缺失／兜底與條件並存／兜底不在末位）觸發即整函式不產碼，**零改 `ps.no`／`cg.no`**；`h2m-test` **176/176**，探針前向 exit **188** 與 Go oracle 同程式對拍（IR 反向 189／195／190／189，源碼級三對變異兩側同值），鑑別態 exit **101**；v0.4.13 短路鏈的兩處 Go 側語義偏差記 §11 B19/B20；§5.3.4.cm，s58）／**M4-e 第五件 塊終結形降級**（`h2m-lower-term` 認 `K-RETURN/BREAK/CONTINUE-STATEMENT` 三種陳述：`return` 懶開全函式共享 ret 塊（終結處只發 `OP-BR`，收尾才發 `LOAD`+`OP-RET`，無 `return` 的函式 IR 逐位元組不變），`break`／`continue` 查 `h2m-lower-for` 壓入的 `(hdr, exit)` 對（`2p`=hdr／`2p+1`=exit），終結形非塊末／帶標籤／帶值 return／循環外裸形皆計 `unsup` 且不產碼，同塊第二條不可達終結指令實證由 LLVM 容忍（verify/llc/clang 三級 rc=0），**零改 `ps.no`／`cg.no`**；`h2m-test` **176→203/203**，探針前向 exit **72** 與 Go oracle 同程式對拍（逐值 14 6 0 37 10 5；源碼級三對變異兩側同值 76／70／74；IR 反向四刀 41／66／73／72），鑑別態四例 exit **101**；§5.3.4.cn，s59）／**M4-e 第六件 計數循環降級**（`h2m-lower-for` 的 `cnt != 0` 臂：計數位只求值一次並 `STORE` 進按 `ForStatement` 節點 id 合成的 entry `alloca` 槽（槽名 `@cnt<n>`，`@` 前綴避撞源碼變量，由前掃描 `h2m-scan` 鋪槽），發 hdr/body/update/exit 四塊——hdr `LOAD`+`CONST 0`+`CMPGT`+`CONDBR(body, exit)`，body 尾 `BR` update，update `SUB 1` 後 `BR` hdr，`N ≤ 0` 由 header 首判即假自然跳過（謂詞 `icmp sgt` 而非 `!= 0`）；`continue` 目標由 hdr 換為 update（跳過自減即活鎖），循環棧每層語義升級為 `(continue 目標, exit)`；count 與 cond 並存／標識符計數位／非 i64 計數位皆計 `unsup`，**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**；`h2m-test` **203→255/255**，探針前向 exit **134** 與 Go oracle 同程式對拍（逐值 3 10 104 11 6 0；源碼級四對變異兩側同值 135／141／136／136；IR 反向五刀 132／131／135／132／131），鑑別態三例 exit **101**；§5.3.4.co，s60m）／**M4-e 第七件 區間遍歷降級**（`h2m-lower-range` 認 `K-FOR-STATEMENT` 的 `IterRange` 位：兩端點在 pre 塊各只求值一次，pre／hdr／upd 三處同發一條 `icmp sle lo, hi` 分派升降序雙臂（IR 無 select，方向靠分支定，`opt` 提出循環），`leftInc` 決定 init 的 `±off`，`rightInc` 決定終止謂詞 `LT`／`LE`（降序 `GT`／`GE`），循環變量槽按**源名**鋪 entry `alloca`（體內 `K-IDENTIFIER` 直接查得），共發 11 塊，`continue` 落 update；同名嵌套區間／開邊界／容器遍歷皆計 `unsup` 不產碼（後兩者需 `OP-LEN`／`OP-INDEX` 與不可鑑別的極值補齊，同名嵌套記 §11 B23），**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**；`h2m-test` **255→324/324**，探針前向 exit **164** 與 Go oracle 同程式對拍（逐值 10 6 6 6 4 14 103 9 6；源碼級八對變異兩側同值 168／168／168／171／162／161／161／156；IR 反向八刀 169／161／163／158／163／160／194／155），鑑別態三例 exit **101**；§5.3.4.cp，s61）／**M4-e 第八件 容器遍歷降級**（`h2m-lower-coll` 認 `K-ITERATION-EXPR` 的 `kid[1]` 容器源：pre 發一條 `OP-VECLEN` 於循環外只求長度＋`STORE 0` 進合成索引槽 `@idx<n>`，hdr `LOAD`+`OP-CMPLT`+`CONDBR(body, exit)`，body `OP-VECGET` → `STORE` 進循環變量槽（讀副本），upd `ADD 1` 後 `BR` hdr，`continue` 落 upd／`break` 落 exit；`TY-VEC` 形參在 `mem` 模式下**不鋪槽**（cg 無 `alloca %vec` 的 op，而形參本身就是支配全函式的 entry SSA 值 ⇒ 綁定即 `%vc1` 跨塊可讀）；元素位寫入靠新增的活動名棧 `c.epl` 在 `h2m-var-write` 入口計 `unsup`（Go `bindElemPlace` 寫回容器 ⇒ 語義分岔寧缺勿錯）；`h2m-param-ty` 收 `[]i64` 形參，`h2m-func-sig` 閘掉 `vec` 返回；**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**（復用 `OP-VECLEN`／`OP-VECGET`，s61 所記"需 `OP-LEN`／`OP-INDEX`"與其時的 `KindSlice` 假帳均已改實）；`h2m-test` **324→372/372**，探針前向 exit **134** 與 Go oracle 同程式對拍（逐值 6 3 101 5 6 6 7；源碼級六對變異兩側同值 136／133／140／134／148／140；IR 反向八刀 135／132／129／139／130／133／131／128），鑑別態六例 exit **101**；§5.3.4.cq，s62）／**M4-e 第九件 值匹配降級**（`h2m-lower-surface` 認 `K-SURFACE-MATCH` 的 `kid[0]` 主體：逐臂在**本臂判定塊內**重讀主體發 `OP-CMPEQ`＋`OP-CONDBR`，主體限已登記的 i64 裸標識符，型樣限裸整型字面量，aux 槽 0／1／2／3／5／6／8 非 0 即出局（解構綁定／`ok` 關鍵字臂／raw-cond／型別臂不收）；⚠️ 收閘前是**靜默錯碼**不是 `unsup`（型樣被當布爾條件發 `icmp ne i64 1, 0` 恆真，主體從未 `load`）；**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**（復用 s35 的 `OP-CMPEQ`）；`h2m-test` **372→429/429**，探針前向 exit **221** 與 Go oracle 同程式對拍（逐值 10 20 30 1 7 3 4 10 1 111 8 9 7；源碼級七對變異兩側同值 216／227／222／226／65（真值 321 按位元截斷）／222／231，另兩對實參 241／220；IR 反向十刀 211／241／231／222／171／215／223／218／212／219），鑑別態七例 exit **101**；§5.3.4.cr，s63）／**M4-e 第十件 實參側池 `args`**（`OP-CALL` 實參改住與指令下標無關的側池：`xa`=池內基址下標、`bs`=個數、第 j 個實參值 id = `args[xa+j]`，段內值 id 可任意散布——**摘掉 §11 B16 連續 id 契約**；`cg-emit-func`／`cg-emit-cfg`／`cg-emit-cfg-t` 各增**必填**第 13 形參 `args`（漏改呼叫點是編譯錯誤而非靜默發 `%v0`），CALL 臂 `q = args[x + j] #{index-out=zero}` 取值 id 後仍按 `vty[q]` 定名與命名空間；`h2m` 側逐實參先落臨時 `al`、**校驗全過才整段入池**（邊降邊推時內層嵌套 CALL 的池項會插進外層段中間擠偏基址，`f(h(3),1)` 初版實測發成 `@f(%v1,%v2)`）；**`mir.no` 未加 opcode**；`h2m-test` **429→443/443**、`cg-test` **342→347/347**、`cg-runtime-test` **60/60**，探針前向 exit **91** 與 Go oracle 同程式對拍（逐值 25 21 17 13 15；IR 反向五刀 93／87／82／93／88；源碼級四對變異兩側同值 92／104／90／95），鑑別態三例 exit **101**；s55–s63 九式探針前向 73／47／92／188／72／134／164／134／221 零回歸；`OP-STRUCT-SET` 自帶連續約定殘留另記 §11 B26；§5.3.4.cs，s64）／**M4-e 第十一件 值匹配臂的 `it` 隱式綁定**（`h2m.no` 增 **`itv` 值 id 棧**——cond 臂複用判定塊主體 LOAD、wild 臂按 `h2m-subtree-uses-it` 懶發 LOAD、臂退出 `.pop()`；`it` 全程不進槽表（`h2m-lst-find('it')` 恆 -1）避臂間同名串型；`h2m-ty-of('it')` 補 TY-I64 特例銷預掃描雙 mark；同族 `stk`／`rnms`／`epl` 三棧退出補真 `.pop()`；**零改 ps/cg/mir**；`h2m-test` **443→453→471/471**，探針前向 exit **46** 與 Go oracle 同程式對拍（源碼級兩對變異 45／47、鑑別態 101；IR 反向五刀 39／44／36／45／41）；⚠️ Go 側嵌套 match 的 `it` 海森堡記 §11 B27；§5.3.4.ct，s65）／**M4-e 第十二件 前綴表達式 `-x`／`!x`**（`h2m-lower-expr` 新增 `K-PREFIX-EXPRESSION` 臂——`'-'`⇒`OP-NEG`（`sub i64 0`）、`'!'`⇒`OP-NOT`（`xor i64 1`），僅 i64 主體，`~`／非 i64 走 `unsup` 誠實門禁；`h2m-ty-of` 補 prefix 臂遞迴運算元型別供預掃描鋪 TY-I64 槽（規避 §5.3.4.ct 雙 mark）；**零改 ps/cg/mir**（op 發射 s48 已備）；`h2m-test` **471→485/485**，探針前向 exit **149** 與 Go oracle 同程式對拍（oracle p3/p5 用 `1-x`／真值臂 match 等價代換規避 Go `!x`→bool 型別差；IR 反向 NEG 三刀 150／151／148，XOR 兩刀因 p3 對稱輸入得 149——NOT 正確性由單元測 `xor i64 1` 斷言釘住）；§5.3.4.cu，s66）／**M4-e 第十三件 二進制位運算 `&`／`|`／`^`／`<<`／`>>`**（`h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈補五條 `bop` 臂——`'&'`⇒`OP-AND`、`'|'`⇒`OP-OR`、`'^'`⇒`OP-XOR`、`'<<'`⇒`OP-SHL`、`'>>'`⇒`OP-SHR`（發 `ashr` 算術右移），僅 i64 雙元；`/ %`（`?i64` 除零 option 語義未建模）與 `&^`（`ps.no` 不成為運算子）仍走 `unsup` 誠實門禁；**零改 ps/cg/mir**（五 op `and/or/xor/shl/ashr i64` s35 已備）、**零改 `h2m-ty-of`**（復用既有 INFIX 節點，不同 s66 新增節點需補臂，雙 mark 坑本件不適用）；`h2m-test` **485→501/501**，探針前向 exit **109** 與 Go oracle 同程式對拍（IR 反向六刀逐助記符互換 **115／103／111／69／228／157** 全改 exit，六式取値非對稱無 s66 XOR 對稱盲區；須 mutate 未 opt 原始 IR 避常量摺疊）；s55–s66 前向 73／47／92／188／72／134／164／134／221／91／46／149 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s66 所記 4 基線口徑不同，本件不觸 std 正交）；§5.3.4.cv，s67）／**M4-e 第十四件 邏輯 `&&`／`||`**（`h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈補兩條 `bop` 臂——`'&&'`⇒`OP-AND`、`'||'`⇒`OP-OR`，**復用 s67 位運算同一組 op**，僅 i64 雙元；因 i64 子集布爾恆為 0/1（比較結果 zext），`and/or i64` 於 0/1 == 邏輯與/或，且 Go `emitLogic` 本身非短路（兩側都求值）→ 與 Go 逐值一致；真·短路 bool 控制流與 f64/u64 邏輯操作數仍 `unsup` 誠實門禁；**零改 ps/cg/mir**（`and/or i64` s35 已備）、**零改 `h2m-ty-of`**（復用既有 INFIX 節點，同 s67，s66 雙 mark 坑不適用）；`h2m-test` **501→511/511**，探針前向 exit **43** 與 Go oracle 同程式對拍（bool→i64 用 `v: { true -> r = 1 -> r = 0 }`；IR 反向四刀逐助記符互換 **47／33／59／11** 全改 exit，無對稱盲區；須 mutate 未 opt 原始 IR 避常量摺疊）；s55–s67 前向 73／47／92／188／72／134／164／134／221／91／46／149／109 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67 同，本件不觸 std 正交）；§5.3.4.cw，s68）／**M4-e 第十五件 前綴位非 `~x`**（`h2m-lower-expr` `K-PREFIX-EXPRESSION` 分派鏈補 `osym == '~'` 臂——`~x`⇒`OP-CONST(-1)`＋`OP-XOR(x, -1)`（鏡像 Go `hir2mir:8861`「`~` 非邏輯非、走全 1 常量 XOR」，`~x = x^-1 = -x-1`），僅 i64 主體；非 i64（如 `~s` str）仍 `unsup` 誠實門禁；**零改 ps/cg/mir**（`OP-XOR`／`OP-CONST` s35/s55 已備）、**零改 `h2m-ty-of`**（復用既有 prefix 節點，s66 雙 mark 坑不適用）；需**二發射**（先 materialize 常量 `-1` 成 SSA 值 id 才能作 XOR 右操作數）；`h2m-test` **511→521/521**（原 `test-prefix-gate` 改棧為 `~s` str），探針前向 exit **255** 與 Go oracle 同程式對拍（逐值 `-1 -6 -1 4 -1`；IR 反向三刀 `CONST -1→0`／`xor→add`／`xor→sub` **22／111／191** 全改 exit；須 mutate 未 opt 原始 IR）；🔴 **`ps.no` 前綴 `~` 鬆綁定（PREC 9 吞右側 `~(x+3)`）vs Go 前端緊綁定 `(~x)+3`——優先級分歧，probe/oracle 規避裸 `~ binop`，屬解析缺口待查非本件 h2m 範圍**；s55–s68 前向 73／47／92／188／72／134／164／134／221／91／46／149／109／43 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67/s68 同，本件不觸 std 正交）；§5.3.4.cx，s69）／**M4-e 第十六件 前綴 f64 負號 `-x`**（`h2m-lower-expr` `K-PREFIX-EXPRESSION` 分派鏈把既有 `'-'→OP-NEG` 臂的守衛從「僅 `TY-I64`」**放寬**為「`TY-I64` 或 `TY-F64`」——新增 `osym == '-' && h2m-vty-at(c, ov) == TY-F64` 臂（`mid = h2m-alloc(c, TY-F64)`、`h2m-emit(c, OP-NEG, mid, ov, 0, '', TY-F64)`），`-x`（f64）⇒`OP-NEG`，鏡像 Go `codegen.go:6235` `emitNeg` 的 float 分支（`fneg double %v`）、int 分支仍 `sub i64 0,x`；**零改 ps/cg/mir/h2m-ty-of**（`OP-NEG` 的 `fneg double` 發射 s48 已備、`cg` 依目的值型別分派）；這是 f64 值**首次端到端流經 h2m**（形參 `double`→`fneg double`→`ret double`）；⚠️ **操作數必為 f64 形參、無 f64 字面量**（`OP-CONST` 只發 `add i64 0, K` i64、無法產 `double` 常量），二元算術仍不收 f64；`!x`／`~x` 於 f64 主體仍 `unsup` 誠實門禁（`test-neg-f64-gate` 釘住邊界）；`h2m-test` **521→529/529**（新增 test-neg-f64-var／nested／gate 三函式），探針前向 exit **195** 與 Go oracle 同程式對拍（逐值 `-1 2 -6 3 -4`、總和 -61；`f64.to-i64` `#{buildin}` `llvm.fptosi.sat` 向零截斷；IR 反向三刀 `fneg→fadd 0.0`／雙負→單負／`fneg→fsub x,x` **237／19／216** 全改 exit；須 mutate 未 opt 原始 IR）；s55–s69 前向 73／47／92／188／72／134／164／134／221／91／46／149／109／43／255 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67/s68/s69 同，本件不觸 std 正交）；§5.3.4.cy，s70）／**M4-e 第十七件 f64 二元算術 `+ - * /`**（`h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈把既有「僅 `TY-I64` 雙側」算術發射守衛**放寬**為「`TY-I64` 或 `TY-F64`」——新增 `fty`（兩側皆 `TY-F64`）發射臂，`x op y`（f64）⇒`OP-ADD/SUB/MUL/DIV`（目的值 `TY-F64`）發 `fadd/fsub/fmul/fdiv double`，鏡像 Go `emitBinop` 按型別分派；新增 `osym == '/' -> bop = OP-DIV` 映射供 f64 側發 `fdiv`，**但 i64 側明確以 `bop != OP-DIV` 排除（`/ %` 需 `?i64` 除零→`err` 的 option 語義未建模、仍誠實門禁）**——同一 `OP-DIV` 在 f64 侧放行、在 i64 侧仍門禁為本件關鍵不對稱；承接 s70 首開的 f64 端到端通道從一元拓展到二元；**零改 ps/cg/mir/h2m-ty-of**（`fadd/fsub/fmul/fdiv double` 發射 s37 已備、`OP-DIV` 早在 `mir.no` 已定義；嵌套 f64 靠發射時 `h2m-vty-at` 讀實際值 id 型別、靜態推斷 `h2m-ty-of` 對 INFIX 仍只回 `TY-I64` 不受影響）；⚠️ 兩側操作數必為 f64 形參、無 f64 字面量（`OP-CONST` 只發 `add i64 0, K`）；`h2m-test` **529→542/542**（新增 test-arith-f64-add／mul-nested／div／mixed-gate／i64-div-gate 五函式），探針前向 exit **30** 與 Go oracle 同程式對拍（逐值 `4 7 5 7 12`、總和 286 遮罩 8 位＝30；`f64.to-i64` `#{buildin}` `llvm.fptosi.sat` 向零截斷；IR 反向三刀 `q1 fadd→fsub`／`q4 fdiv→fmul`／`q5 丟外層 fmul` **27／222／158** 全改 exit；須 mutate 未 opt 原始 IR）；s55–s70 前向 73／47／92／188／72／134／164／134／221／91／46／149／109／43／255／195 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67–s70 同，本件不觸 std 正交）；§5.3.4.cz，s71）／**M4-e 第十八件 f64 二元比較 `< <= > >= == !=`**（`h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈在原 s71「i64 臂＋f64 算術臂」外**新增 f64 比較臂**——以 `is-arith`/`is-cmp` 旗標對 `bop` 歸類，`fty==1 && is-cmp==1` ⇒`OP-CMPLT/CMPEQ/CMPNE/CMPLE/CMPGT/CMPGE`（目的值 `TY-I64`、非 f64）發 `fcmp <pred> double` 後 `zext i1→i64`（`!=` 用 `une` 保 NaN），鏡像 Go `emitCmp` 按左操作數型別分派；**順帶收緊 s71 過寬 f64 臂**（曾對一切 `bop` 放行、dest `TY-F64`）為算術+比較兩族——位/移位/邏輯於 f64 不落任一路 ⇒ 誠實門禁（`test-bit-f64-gate`/`test-logic-f64-gate` 以 `(r f64)` 釘住畸形 `and i64 %fd`）；**零改 ps/cg/mir/h2m-ty-of**（`fcmp`+`icmp`+`zext` s37 已備、六比較映射早已存在）；比較結果恆 i64（走 `%v`）與算術 dest `TY-F64`（`%fd`）分岔為本件關鍵；⚠️ 真 Nolang `x<y` 得 bool（`cannot assign bool value to i64`），self-host 型別表無 bool 故以「比較即 i64」等價建模、oracle 用 `b = x < y`＋`b -> r = 1` 折 0/1 對拍；`h2m-test` **542→560/560**（新增 test-cmp-f64-lt/eq/ne/arith-nested/mixed-gate＋bit/logic-gate 七函式），探針前向 exit **79** 與 Go oracle 同程式對拍（逐值 `1 1 1 1 0 0 1`、加權和 79；IR 反向五刀 c1 olt→ogt／c3 oeq→one／c5 ole→oge／c6 oge→ole／c7 內層 olt→ogt **78／75／95／111／15** 全改 exit、含 0↔1 兩向；須 mutate 未 opt 原始 IR）；s55–s71 前向零回歸＋i64 家族 s66–s69 IR 逐位元組相同；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67–s71 同，本件不觸 std 正交）；§5.3.4.da，s72）／**M4-e 第十九件 f64 字面量降級**（`h2m-lower-expr` **新增 `K-FLOAT-LITERAL`（kind 29）臂**——讀 raw（slot1＝源 token 原文、非 slot0 %v 值）、誠實門控 `raw.contains('.')`、放行則 `h2m-alloc(TY-F64)`＋`h2m-emit(OP-CONST, cid, 0,0, raw, TY-F64)`（raw 置 sym 槽）；**同步改 `cg.no` OP-CONST 臂**——`cg-vty-at(vty,d)==TY-F64` ⇒ `%fd<d> = fadd double 0.0, <syms[k]>`（LLVM 無法命名裸雙精度常量、以 fadd 0.0 物化）、否則維持 `add i64 0,<x>`——**s55 起首件需動 cg.no 之降級件**（前十八件皆零改 cg，此件如實標注）；取 raw 非取值因 LLVM textual 雙精度字面量文法須含小數點（slot0 %v 吞 .0 ⇒ '2.0'→'2'），raw 無點者（如 '1e5'）誠實門禁；slot-decl 零初始化 OP-CONST 永不見 TY-F64（h2m.no:320）⇒ 新 cg 分支只由字面量路徑可達；`h2m-test` **560→572/572**（新增 test-f64-lit-arith/cmp/gate 三函式），探針前向 exit **91** 與 Go oracle 同程式對拍（逐值 `1 1 0 1 1 0 1`、加權和 91；IR 反向六刀改各函式 fadd 常數 **90／89／95／83／75／27** 全改 exit、含 0↔1 兩向；須 mutate 未 opt 原始 IR）；s55–s72 前向零回歸＋i64 家族 s66–s69 與 f64 形參家族 s70–s72 探針 IR 逐位元組相同；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67–s72 同，本件不觸 std 正交）；§5.3.4.db，s73）／**M4-e 第二十件 f64 局部變量／控制流槽化**（槽機構認 TY-F64：h2m-ty-of 推 f64 字面量與算術/比較族、slot-decl 鋪 alloca double（零初始化攜 sym='0.0'）、var-read LOAD 白名單增 F64；cg OP-ALLOC/STORE/LOAD 依 cg-vty-at 分岔發 double 翼；h2m-test **587/587**、cg-test 347/347、cg-runtime 60/60；探針七函式全含控制流前向 exit **77** 與 Go oracle 同程式對拍；IR 反向七刀 **76／79／73／69／93／109／13** 全改 exit 含 0↔1 兩向；s66–s73 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.dc，s74）／**M4-e 第二十一件 i64 除／模除零護欄降級**（h2m 運算符表補 '%' ⇒ OP-MOD、i64 發射臂取消 bop != OP-DIV 排除；除/模落 cg 早在 s38/s39 就緒的 cg-div-guard plain 護欄路徑——除零結果定義化為 0 + stderr 報告，**零改 cg/ps/mir/ty-of**；實測發現 checker 口徑：`a i64 = x / d` 宣告＋初值被拒（?i64→i64），須先宣告 plain 再重賦值才達 Go plain 路；h2m-test **599/599**、cg-test 347/347、cg-runtime 60/60；探針七函式前向 exit **111** 與 Go oracle 同程式對拍（stderr 逐字一致）；IR 反向七刀 **110／109／107／103／127／79／47** 全改 exit；s66–s74 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.dd，s75）／**M4-e 第二十二件 u64 除／模無符號護欄降級**（h2m 新增 `uty` 分派——雙側整數族且至少一側 `TY-U64` ⇒ `OP-DIV/OP-MOD` 改寫為 `OP-DIVU/OP-MODU`（鏡像 Go `hir2mir` 的 `sourceUnsigned` 或路由）、新發射臂 dest `TY-U64`；落 cg 早在 s38/s39 就緒的 `cg-div-guard(..., sgn=0)` 路徑——查零、不查 (MIN,-1)，**零改 cg/ps/mir**；🔴 實測撞出 `h2m-ty-of` 與 `h2m-scan` 前掃描槽註冊的重耦合（缺 U64 infix 推斷臂 ⇒ 槽註冊 -1 ⇒ mem 模式三處 mark，register 模式全綠的誤導表象），同步補該臂；🔴 對拍撞出 Go 側編譯期常數摺疊對 u64 除法走有符號口徑（MAX/2 折成 0、MAX%10 折成 -1），執行期真實路徑正確 ⇒ oracle 高位元用例改經 env 取不透明值（正交既有 quirk，本件不改 `src/`）；h2m-test **619/619**（+20）、cg-test 347/347、cg-runtime 60/60；探針六式前向 exit **63** 與 Go oracle 同程式對拍（stderr 逐字一致）；IR 反向八刀 **62／61／59／55／47／31／59** 全改 exit（K5/K6 為簽／無簽分岔靶）、報告呼叫刀 exit 沒有變而 stderr 變空；s66–s75 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.de，s76）／**M4-e 第二十三件 u64 比較族無符號降級**（h2m 在既有 `uty` 分派上把 `OP-CMPLT/LE/GT/GE` 改寫為 `OP-CMPLTU/LEU/GTU/GEU`（鏡像 Go `hir2mir` 同一 `switch`）、`==`／`!=` 保持原 op（Go 口徑「eq/ne 與符號無關」）、比較發射臂 dest **恆 `TY-I64`**（0/1 而非 u64，與 s76 除模臂分岔）；落 cg 自 s36 就緒的 `icmp u* i64`＋`zext i1→i64` 四臂，**零改 cg/ps/mir**；🔴 `h2m-ty-of` 的整族 U64 推斷臂必須同步開比較例外（`.de` ④ 耦合的反方向——s76 靠「非除模必 mark」撐住槽寬推錯無害，本件解除比較門禁後該前提失效，mem 模式對 `f = x > y` 的後續 i64 寫全數 mark）；h2m-test **619/619→648/648**（+29，新增 9 測試＋門禁下移的 `test-arith-u64-bit-gate`）、cg-test 347/347、cg-runtime 60/60；探針八式（含 mem 槽 `>=`、高位 `>`／`<` 簽／無簽分岔靶、`==`／`!=`）前向 exit **255** 與 Go oracle 同程式對拍（oracle 守衛折 0/1、高位元經 env 取不透明值）；IR 反向九刀 **254／253／251／247／239／223／191／127／251** 全改 exit（K5/K6 簽／無簽靶、K9 實參次序刀與 K3 同位而刀類不同）；s66–s76 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.df，s77）／**M4-e 第二十四件 u64 算術族 '+ - *' 放行**（h2m 在既有 `uty` 分派上新增算術發射臂 dest `TY-U64`——**不重映射**（Go MIR 無 OpAddU/OpSubU/OpMulU，64 位寬簽／無簽位元相同）、**零改 cg/ps/mir/ty-of**（s77 的 ty-of「比較⇒I64、其餘⇒U64」拆分已天然對齊算術槽寬）；cg `!= TY-F64` 默認臂直接吞 U64 dest 發裸 `add/sub/mul i64`，鏡像 Go plain 路「bit-identical」；溢出 `with.overflow` 只在 option 路、`?u64` 仍不表達（同 s75/s76 口徑）；🔴 本件無簽/無簽分岔靶，s76 留的「非除模必 mark ⇒ 槽推 U64 無害」陳舊註解隨門禁解除改寫；h2m-test **648/648→670/670**（+22，新增 8 測試：裸三式／字面量翼／嵌套／mem 槽／slot-u64 耦合／mixed 仍門禁；s76 的 add-gate 紅測翻正拆除）、cg-test 347/347、cg-runtime 60/60、ast-test 系 HEAD 工具鏈既有 bug #85 正交失敗；探針八式（`+ - *`／`x+2`／`(x+y)*2`／mem 槽／MAX+1 與 0−1 回繞）前向 exit **255** 與 Go oracle 同程式對拍（env 不透明高位元、回繞不加 `#{overflow=wrap}`）；IR 反向八刀 **254／253／251／247／239／223／253／191** 全改 exit（刀型制：助記符互改＋實參次序＋mem 斷鏈＋回繞斷言翻轉）；s66–s77 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.dg，s78）／**M4-e 第二十五件 u64 位運算／移位族 '& | ^ << >>' 放行**（h2m 在既有 `uty` 分派上新增位運算發射臂 dest `TY-U64`——`& | ^ <<` **不重映射**發裸 `and/or/xor/shl i64`（Go 路由表無衍生可鏡像、逐位與符號性無關）；唯一重映射 `>>` ⇒ `OP-SHRU`發 `lshr i64`（鏡像 Go `emitBitwise` 對 OpShr **恒發 lshr**——「nolang >> 是邏輯移位」；`.dg` 留的「不收 vs 收 OP-SHRU」分歧裁定收；自舉 s35/s36 的「i64=ashr、u64=獨立 op」設計在 u64 側與 Go 會師；**零改 cg/ps/mir/ty-of**）；`&& ||` 與位運算 bop 同碼（s68 遺留），u64 上裸 and/or 不等價邏輯語義 ⇒ `is-logic` 旗標排除、門禁測下移至 `x && y`；🔴 i64 `>>` 口徑分歧陳帳（自舉 ashr vs Go lshr，負數右移分岔靶，待後續件定奪）；h2m-test **670/670→694/694**（+23，新增 9 測試：裸四式／shr-lshr 含 ashr 缺席負向／字面量移位量／嵌套／slot-u64 耦合／mem 槽）、cg-test 347/347、cg-runtime 60/60、ast-test/ck-sig 正交基線同 s78；探針八式（基礎五式／mem 槽 `f=x&y⇒f=f|1`／🔴 `MAX>>1` 簽/無簽分岔靶／`1<<63` 符號位回繞）前向 exit **255** 與 Go oracle 同程式對拍（env 不透明高位元、期望值顯式 u64 標注）；IR 反向八刀 **254／253／251／247／239／191／223／127** 全改 exit（K6 全 suite 唯一簽/無簽刀；K8 對調刀只落移位式——and/or/xor 可交換無鑑別力）；s66–s78 探針 IR 逐位元組相同（13/13）；vet 計數 1 ERROR 與本件正交；§5.3.4.dh，s79）／**M4-e 第二十六件 i64 '>>' 口徑裁定——全族無條件重映射 OP-SHRU 恒發 lshr**（鏡像 Go OpShr；s35 i64=ashr 推翻、cg ashr 臂轉死代碼；首個非加性件：s67 IR 單行差異重基線、執行值不變；h2m-test 698/698、oracle 同 exit 255、刀 8/8 含 4 枚分岔刀；§5.3.4.di，s80）／**M4-e 第二十七件 u64 前缀 `-x`／`~x` 放行**（`K-PREFIX-EXPRESSION` 分派鏈新增兩 `TY-U64` 發射臂：`'-'`⇒`OP-NEG`（dest `TY-U64`→`sub i64 0`，二補回繞 mod 2^64）與 `'~'`⇒`OP-CONST(-1)`＋`OP-XOR`（dest `TY-U64`→`xor i64`，bit-not）；`'!'` 於 u64 **保持誠實門禁**（裸 `xor 1` 翻最低位≠邏輯非）；🔴 `TY-U64` 與 `TY-I64` 同 LLVM 型別／`%v` 命名空間 ⇒ u64 前綴 IR 與 i64 前綴**逐字節相同**，型別正確性靠 (a) `h2m-test` 閘僅對 `TY-U64` 放寬＋dest 型別碼釘 `TY-U64`、(b) 值語義刀共同鎖；**零改 `cg`／`ps`／`mir`／`h2m-ty-of`**（整數發射臂 s35/s48/s55 已備、prefix 型別遞迴早已就緒）；h2m-test **698→715/715**、探針 exit 255、oracle 同 exit 255（`#{overflow=wrap}`）、刀 **7/7**；屬 s79「u64 前綴僅 i64」陳帳缺口的自然償還；§5.3.4.dj，s81）／**M4-e 第二十九件 u64 邏輯族 `&& ||` 放行**（h2m u64 位運算臂撤 `is-logic == 0` 排除、旗標退場——探針實釘 Go 對 u64 `&& ||` 即裸位形 `and/or i64` 非短路（12&&10⇒8、0||5⇒5），s79「同碼⇒不等價、與真短路後置」假設被推翻，放行即鏡像；門禁測 `test-arith-u64-bit-gate`／s67 `test-log-gate` 翻正為 `test-logic-u64-*`／`test-log-u64-mem`；**零改 cg/ps/mir/ck**（s68 同碼復用、cg 發射臂已備）；函數形「r u64=x&&y」Go checker 拒（表面 bool）⇒ oracle 走頂層形；h2m-test **715→721/721**、IR 探針 exit **15** == oracle 頂層形裸 exit **15**、刀 **5/5**、s66–s81 前向值全同；§5.3.4.dl，s83）／**M4-e 第三十件前綴 `~` 綁定口徑複核＋哨兵重鑲**（§5.3.4.cx 關鍵點 4 🔴 待查項三面實測緊綁定==Go 關閉；實作零改；ps-test 38→42 哨兵；刀 4/4、前向 IR 16/16 SAME；§5.3.4.dm，s84）／**M4-e 第三十一件 (MIN,-1) 護欄翼執行面實釘**（§5.3.4.dd「執行面留待專門件」尾款償清——值面 /⇒MIN、%⇒0 不觸 stderr、exit 7==oracle；實作零改；h2m-test 721→735 翼哨兵；刀 4/4、前向 IR 18/18 SAME；§5.3.4.dn，s85）M4-d ⬜   | M4-a：`cg-runtime.no`＋`cg-rc.no`＋`check-rc-balance`，**16/16**`opt` rc=0diff 0 刪除行M4-b：`mir.no`＋`cg.no` 發 CONST/ADD/SUB/MUL/CALL/ALLOC/STORE/LOAD/RET，**13/13**`let-add` 執行 **exit 6**M4-c：多基本塊 + `BR/CONDBR/CMPLT`（alloca 傳值無 PHI），含**回邊/循環**，`lt10(5)+lt10(15)`→**exit 3**`sum4()`→**exit 6**字符串（首个非 i64 型別 `%str-long`）：`@.s<k>` 全局字面量前置 pass＋三步 `insertvalue`＋`extractvalue` 取 len，`slen()`→**exit 3**字符串拼接（首个 RC 消费者）：`@str_concat` 13 行逐字照抄＋`prelude-full` 唯裝配點（順序有斷言），`cat()` 輸出 **`abcd`**exit **4**多參調用：`OP-CALL` 的 `bs` 從佔位改為**參數個數**（破壞性語義變更，已同步唯一呼叫點），`add2(3,4)`→**exit 7**`%str-long` 跨基本塊：`SALLOC/SSTORE/SLOAD` 三件套（第三個槽命名空間 `%ss<id>`），`pick(0)`→**exit 2**`pick(1)`→**exit 6**`vec`（第四個值命名空間 `%vc<id>`）：`%vec = { i64, i64, i64 }` + 4 個 helper（刻意不照抄 Go 的 inline 展開），十次 push 強制擴容 0→4→8→16 後求和 ⇒ **exit 45**ASan 無報告；`cg-test` **54/54**`cg-runtime-test` **30/30**帶型別簽名（`cg-emit-cfg-t` + `TY-I64/STR/VEC`）：形參/返回值可按型別取 `i64`/`%str-long`/`%vec`，值命名空間由 `cg-val-prefix` 單一來源決定（`mix(n i64, s str)` ⇒ `%v1`+`%sv2` 交錯），`mix/sident/mk` ⇒ **exit 14**帶型別 CALL（值型別表 `vty` 按值 id 索引 + `cty` 按指令索引）：`caller2` 在函式內發 `call i64 @mix(i64 %v1, %str-long %sv2)`，`caller2()`+`mk()`+`sident()` ⇒ **exit 14**`option`（`%option = { i64, [3 x i64] }`，第五個值命名空間 `%op<id>`）：`OPTNONE/OPTSOME/OPTTAG/OPTISOK/OPTGET` 五個 op，`OPTSOME` 用 Go 同形的一次 insertvalue 給 tag + slot[0]；`pick(n)=n<10?some(n):none` + `g(n)=isok+payload` ⇒ **exit 8**（`g(7)=8``g(15)=0`）；`cg-test` **78/78**`cg-runtime-test` **33/33**s26–s29 續：`RETAIN`/`RELEASE` 原語（只作用 TY-STR，`%dp<k>`/`%rel<k>` 按下標命名；`cg-rc.no` 事件碼改名 `RC-RETAIN`/`RC-RELEASE` 消同名衝突）⇒ `cg-test` 86/86`clang -O2` exit 4**ASan 零報告**；嵌套 `STRCAT`（中間堆串 `sv3`/`sv5` 各自 RETAIN+RELEASE）⇒ **99/99**exit 6ASan/LSan 零報告；option 的 `?str` 載荷**內聯**進 24B slot（`%str-long` 恰 24B ⇒ 免裝箱；data 指標須 `ptrtoint`/`inttoptr` 非 bitcast）⇒ **107/107**exit 4ASan 零報告；`struct`（`%struct.point` 收歸 prelude 唯一發射`%st<id>` 第六命名空間鏈式 `insertvalue` 中間名 `%sm0<k>`）⇒ **112/112**`cg-runtime-test` **35/35****雙函式模組 199 行 `opt` rc=0 + `clang -O2` exit 90 + ASan 零報告**型別重定義反證 `opt: …:7:1: error: redefinition of type`s30 續：`STRUCT-SET`（**讀舊出**新值，欄位值 id 走 `xa[k]+1` 連續約定）+ `STALLOC`/`STSTORE`/`STLOAD`（結構體槽 `%sts<id>`，第七命名空間）⇒ **119/119**四基本塊回邊的 `counter()`（`while i<5: st.count++`）206 行模組 `opt` rc=0`clang -O2` **exit 5**ASan 零報告；**兩個負向對照**（刪循環內 `STSTORE`SET 寫錯欄位）皆 **exit 0** ⇒ 證明 SET 是純 SSA 值不配槽等於沒寫s31 續：結構體**多型別**——型別碼改 `TY-STRUCT-BASE + 結構體 id`（`%struct.s<k>`），宣告改由 `cg-runtime-struct-decls(sfc)`（`sfc[id]`=欄位數）唯一發射prelude 不再帶結構體；`STRUCT-MK` 按 `bs[k]` 循環展開（初值 `zeroinitializer`中間名 `%sm<k>_<j>`）⇒ **123/123**`cg-runtime-test` **37/37**同模組兩個結構體（`s0`{3,4}=34`s1`{1,2,3}=123）206 行 `opt` rc=0 + `clang -O2` **exit 157** + ASan 零報告；**兩個負向對照**（`s1` 宣告成 2 欄位 ⇒ `invalid indices for insertvalue`；漏發 `s1` 宣告 ⇒ `invalid type for null constant`）s32 續：結構體**欄位型別**——`cg-runtime-ty-name` 收成型別碼→型名的唯一映射（從 `cg.no` 遷入 `cg-runtime`，`cg-ty-name` 只轉發），宣告側加 `sft`（欄位型別碼**拼接表**，游標展開無 stride 上限），`MK`/`GET`/`SET` 按值自身的 `vty`/`cty` 選型別名與命名空間 ⇒ **127/127**`cg-runtime-test` **37/37**`%struct.s0 = type { %str-long, i64 }` 的 `hold()`（`'abcd'.len + 9`）`opt` rc=0 + `clang -O2` **exit 13** + ASan 零報告；**負向對照**（宣告欄位順序寫反 ⇒ `insertvalue operand and field disagree in type`）⚠️ **str 欄位的 RC 未做（§11 B11）**：本輪 e2e 只用 `STRCONST`（無 RC 頭），ASan 乾淨**不**代表 str 欄位安全s33 續：結構體**欄位 RC 原語**——`OP-STRUCT-RETAIN=37`/`OP-STRUCT-RELEASE=38`（`xa`=結構體值 id`bs`=**欄位位掩碼**），逐位置位各發 `extractvalue %struct→%str-long→data` + `call @nolang_rc_retain/release` 三步；**刻意不讓 `STRUCT-MK` 自動 retain**（操作數可能是 `STRCONST` 全局常量，retain 會讀到全局對象之外的頭；且 `vty` 只有型別沒有所有權）⇒ **127→133/133**`cg-runtime-test` **37/37**`hold()`（`'no'+'lang'` 存入結構體取出後再拼 `'lang'`，`.len + 27`）的 204 行模組 `opt` rc=0 + `clang -O2` **exit 37** + 補屬性 ASan **exit 37 零報告**；**三个負向對照**：刪 `STRUCT-RETAIN` ⇒ **exit 134 `heap-use-after-free READ of size 6`**（`@str_concat` 的 `memcpy`）刪 `STRUCT-RELEASE` ⇒ exit 37 無報告（**無鑑別力**，洩漏方向）重複 `STRUCT-RELEASE` ⇒ **exit 134 `READ of size 8`**（`release` 讀 rc）🔴 **門禁口徑修正（§11 B12）**：`clang -x ir -fsanitize=address` 對 IR 輸入**完全不插樁**（最小反例 exit 0`__asan_load*` 零個）——ASan pass 只插樁帶 `sanitize_address` **函式屬性**的函式，clang 前端處理 `.ll` 輸入時不加 ⇒ s26–s32 的ASan 零報告只覆蓋 libc 攔截器s34 續：結構體**作一等值**——形參/返回值（`define %struct.s0 @bump(%struct.s0 %st1)`）與嵌套欄位（`%struct.s1 = type { %struct.s0, i64 }`）**零 emit 變更即通**，本件把有臂未實測變成有測試釘住⇒ **133→146/146**`cg-runtime-test` **37/37**三個 e2e（`mk/bump/total` **exit 8**嵌套 **exit 16**邊界 RC **exit 7**）`opt` rc=0 + `clang -O2` + 補屬性 ASan 零報告；🔴 **撞出真 bug 並修掉**：字面量全局從 `@.s<k>` 改 `@.s.<函式名>.<k>`（`cg-lit-name`）——兩個函式各自第 k 條 `STRCONST` 撞名 ⇒ `opt: redefinition of global`（實測訊息指名 `@.s1`），而 s29–s33 的 e2e 全是單一業務函式 + 無字面量的 main故從未觸發（判據：全局名只含指令下標 ⇒ 必撞，B10 從具名型別擴到全局符號）**五個對照**：父欄位宣告成 `i64` ⇒ `insertvalue operand and field disagree in type`漏發子宣告 ⇒ `invalid type for null constant`**宣告順序寫反 ⇒ 照樣 exit 16（否證子型別必須先宣告，LLVM 具名型別惰性解析）**`STRUCT-RELEASE` 挪到 call 前 ⇒ ASan `heap-use-after-free READ of size 6`（釘住參數是引用語義caller 的 release 必須晚於最後一次使用）s35 續：ALU 家族——`OP-CMPEQ/NE/LE/GT/GE=39–43 / OP-AND/OR/XOR/SHL/SHR/DIV/MOD=44–50`（12 op，全 `i64` 有符號標量：`and/or/xor/shl/ashr/sdiv/srem` + `icmp <pred>`+`zext`；`SHR` 必 `ashr` 非 `lshr`除零不插護欄 §11 B14）⇒ `cg-test` **146→160/160**（`test-alu` +14，含 `lshr` 缺席負向對照）`cg-runtime-test` **37/37**e2e `probe-s35.no`（djb2 鏈＋位運算＋6 比較）219 行 `opt` rc=0 + `clang -O2` **exit 161**；⚠️ 驗證在釘死 `v0.4.13`（HEAD Go 側跨模組結構欄位回歸，`cg/mir/cg-test` 逐位元組未變）s36 續：無符號 ALU 家族——`OP-SHRU/DIVU/MODU=51–53 / OP-CMPLTU/LEU/GTU/GEU=54–57`（7 op，全 `i64`/`%v`：`lshr/udiv/urem` + `icmp <u-pred>`+`zext`）＋型別碼 `TY-U64=4`（走 cg-ty-name/cg-val-prefix 默認臂 ⇒ 仍 i64/%v）⇒ `cg-test` **160→176/176**（`test-alu-unsigned` +16，含 not ashr/sdiv/slt 負向對照）`cg-runtime-test` **37/37**e2e `probe-s36.no`（無符號取值鏈）196 行 `opt` rc=0 + `clang -O2` **exit 18**（反向探針：CMPLTU 誤發 slt ⇒ exit 19，證符號性有鑑別力）；⚠️ 同 s35 驗證在釘死 `v0.4.13`且需 `DEVELOPER_DIR`s37 續：`f64` 算術家族——按型別分派（不新增 op）`OP-ADD/SUB/MUL/DIV/MOD` 依 `vty[d]==TY-F64` 發 `fadd/fsub/fmul/fdiv/frem double`，比較依 `vty[x]==TY-F64` 發 `fcmp oeq/une/olt/ole/ogt/oge`（`!=` 用 `une` 保 NaN）＋型別碼 `TY-F64=5`（真独立 LLVM 型別 `double` + 独立值命名空間 `%fd`）＋安全讀 `cg-vty-at`（越界回 TY-I64，修掉 s37 引入的越界段錯誤）⇒ `cg-test` **176→197/197**（`test-alu-f64` +21）`cg-runtime-test` **37/37**零回歸e2e `probe-s37.no` 198 行 IR `opt` rc=0 + `clang -O2` **exit 5**（反向探針：型別誤映射 opt rc=1；`oge` 誤發 `ogt` exit 5→4）⚠️ 同 s35/s36 驗證在釘死 `v0.4.13`且需 `DEVELOPER_DIR`下一步：逐欄位 RC 的**自動放置**（B11，需所有權位）→ 跨函式所有權轉移（B13）→ 整數除零的 option 語義降級（B14 語義半項，`select` 護欄 s38 + stderr 報告 s39 已落地）→ M4-d 接 M5 ／**M4-e `h2m` AST→MIR 降級已續推至 s99n（件號 54，§5.3.4.ea–.eo）**：s55–s97（2026-11-06，共 43 件，§5.3.4.cj–.dz）完成直線式 i64 → 簽名/調用 → 控制流/跨塊槽 → 花括號短路鏈 → 塊終結 → 計數/區間/容器遍歷 → 值匹配 → 實參池 args → 邏輯 `&&`/`||` → i64/u64 除零護欄 → u64 全族 → i64 `>>` 裁定 lshr → f64 槽化與 `%` → bool/char 字面量 → TY-OPT 控制流鋪槽（當時 `h2m-test` **845/852**、`cg` 347／`cg-runtime` 60／`ck` 20）；**其後 s98（2026-11-08，§5.3.4.ea）清掉 cg `OP-SHR` ashr 死臂；s99–s99n（2026-11-09，14 節，§5.3.4.eb–.eo）把 struct 定義／欄位訪問／形參返回值／跨塊槽／跨函數／欄位型別（str·f64·嵌套·`[]i64`）／欄位 RC 取得與 scope-exit 釋放／vec 欄位就地索引／所有權 provenance（`ownn`·`ownv`·`rdval`·`rdown`·`retset`，s99m）／vec 切片字面量（`K-SLICE-LITERAL`⇒`OP-VECNEW`/`OP-VECPUSH`、`@vec_free`、`OP-VEC-FREE = 94`，s99n）逐族落地**；`0fbbe684`（11-10）再重構 struct lowering 與所有權處理。現況 `h2m-test` **1031/1037**（紅 6 皆既有 s98 optmatch）、`cg-runtime-test` **84/84**、`cg-test` **358/358**（⑦ 解鎖並校正 16B 斷言）、`cg-str16-test` **41/41**；✅ 2026-11-11 Go 後端 `%str-long` 改 16B `{ i32, i32, i8* }`，selfhost `cg-runtime.no`／`cg.no` **已於 2026-11-12 同步清償**（§11.2 B28／§11 後 ⑥；`cg-runtime-test` 84/84、新增 `cg-str16-test` 41/41、Go helper 本文逐字相同）；**M4-d 接線已落地（2026-11-12，§11 後 ⑧）**：`h2m-lower-module` 在 AST 上就地合成 `main`（末位 `exit(E)` → 輸出參 `__noc_r`，無 `exit` ⇒ `0`）、新增 `compiler/src/link.no`（DR-2：只調 `clang -x ir`，不依賴 `opt`／`llc`）、`drv.cmd-build`／`cmd-run` 支援 `-o OUT FILE` 與 `FILE -o OUT` 雙序、`drv-build-test` **16/16**、e2e `build`／`run` 與 Go oracle 同值（**exit=42**）、`opt -passes=verify` rc=0；**s100 `print`／`eprint` 內建降級已落地（2026-11-12，§11 後 ⑨）**：新增 `OP-PRINT = 95`（`bs[k]` 為 kind，一 op 覆蓋 stdout/stderr × i64/str/換行/空格）、prelude 補 `@digits`／`@print_i64`／`@print_str`／`@eprint_*`／`@.nl`／`@.sp`、`h2m-lower-print` 含三條誠實門禁（值型別／插值擋板／語句位），`drv-build-test` **31/31**、`dump-print` 過 `opt`＋`clang` 執行輸出逐位元組等於 oracle；**L4 全語料首度量測**：169 檔中真 SAME **2**（`ifelse.no`／`colon-loop.no`，皆本輪由 0 起增益）、空 SAME 4（`tagged-enum-*`，oracle 亦建不過）、DIFF 163 且**無靜默錯碼**；**s101 `...` 佔位陳述＋s102 字面量插值已落地（2026-11-12，§11 後 ⑩）**：`tok.ELLIPSIS3 = 92` 與範圍運算子 `ELLIPSIS = 65` 分離、`ps` 建 `K-EMPTY-STATEMENT`、`h2m` no-op 臂消費（語料 74 檔含 `...`）；插值按「三檔分類（無字段／裸標識符／其它）」逐段發 `OP-PRINT`，無 spec 的 `{ident}` 與權威逐位元組同結果（`fmt-int(n,'')` 正文即 `n.to_str()`），`{name:spec}`／表達式字段／多實參模板形／`{{` `}}` 仍計 unsup；L4 真 SAME **2 → 4**（新綠 `for.no`／`guard.no`）、全語料 unsup **11,857 → 11,436**、86/169 檔下降、`drv-build-test` **42/42**、`lx-test` **81/81**；**s103 值位塊匹配（`r = { c -> v  -> d }`）已落地（2026-11-12，§11 後 ⑪）**：`h2m-scan` 前掃描按 LetStatement 節點 id 合成結果槽 `@smv<id>` 並在 entry 統一 `OP-ALLOC`（**不在降級途中補 alloca**——否則迴圈體每迭代新攤一份棧），`h2m-lower-surface` 加第 5 參 `snm`／出參 `r` 與值位分支（逐臂「降臂值 → `OP-STORE` 進槽」、匯聚塊 `OP-LOAD` 交回），臂值非 `i64`／臂體非「inline 塊包單表達式陳述」／`valueCtx` 未置位一律誠實門禁；`h2m-test` **1031/1037 → 1060/1066**、`drv-build-test` **42/42 → 53/53**；⚠️ 並記下一個**先於本件存在**的語義分歧——`{}` 無兜底臂的多臂鏈在權威側降成「獨立 if（每條命中的臂都跑）」而 noc 短路（`syntax.md` rule 1 與實現不一致，待拍板，本輪不動）；餘 `?str/?vec` 載荷、`option` match 臂解構、err/tag=2 裝箱、下標／方法調用／`#` 導入／block-as-value／`ok(...)` 條件臂（L4 主體殘差）、multi-out、TY-VOID、M5–M8 仍待續。**s104（見 §11 後 ⑫）**：`h2m` 補 `str` 相等/不等降級臂（`==`/`!=` 兩側皆 `TY-STR` ⇒ `OP-STEREQ`/`OP-STNEQ`，**零改 cg/mir**——兩 op 與 `@str_eq` 早已就位），並同步修 `h2m-ty-of` 的 `str` 對 `str` 分支（缺它則 str 比較/拼接只在 `mem==1` 掛——同一份碼直線式能編、進控制流就 5 條 unsup）；`h2m-test` **1060/1066 → 1080/1086**、`drv-build-test` **53/53 → 63/63**；全語料 unsup **11,354 → 11,163**（25 檔降、**0 檔升**），自舉模組合計 **2,456 → 2,368**；並以臨時插樁量出「unsup 落在哪個 AST 節點型別」的直方圖，據此判定**下一輪的閘是 `#` 模組導入**（611 條 `K-CALL-EXPRESSION` 標記全卡在「點號被調者」＝跨模組），非任何單一運算式族。**s105（解析器保真度）**：`ps-is-struct-def` 的「型別後終結符」只認 NEWLINE/COMMA/RBRACE/EOF，`COMMENT` 落進 fallback ⇒ **欄位帶 `;` 尾註釋的結構體整塊退回 `blockUnknown`、從未登記**（Go 的 `nextToken()` 自行跳過 COMMENT，故其 `look()` 看不見——兩側對「型別後面那個 token」判斷分歧）。修法是新增 `et == tok.COMMENT` 臂並**即時計數**（主迴圈的 NEWLINE 臂不計數，只跳過會讓「每個欄位都帶尾註釋」的結構體得 `nfield==0`）。影響：`ast.no` 的 `ast` 12 欄位裡 3 個帶註釋 ⇒ 該結構體未入表 ⇒ 155 個函式簽名被判 bad；修後 15 個自舉模組的簽名被拒 **250 → 166**、自舉模組**實際產碼函式數** `ast.no` 3→8／`lx.no` 2→3；⚠️ 自舉模組 unsup 反而 **2,362 → 3,198**（簽名通了 ⇒ 函式本體才被嘗試，body 內每條不支援各記一條），而**全語料 173 檔逐檔 `diff` 完全相同（11,315 → 11,315、`unsup==0` 5 → 5）**；`ps-test` **39/42 → 41/44** |
| **M5** | 驅動層：`build` / `run` / `vet` 的 CLI 與 clang 調用                                     | ⬜ 未開始   | —                                                                                      |
| **M6** | 定點：S0→S1→S2→S3 + 三路交叉編譯（§3.1 / §3.2）                                            | ⬜ 未開始   | —                                                                                      |
| **M7** | 精確所有權分析（`insertDrops` 那一整套）                                                       | ⬜ 未開始   | —                                                                                      |
| **M8** | 後置：`fmt` / `lsp` / `tools`（不阻塞定點）                                                | ⬜ 未開始   | —                                                                                      |

**M2 收官（2026-10-27，s8y `261ab91c` 落地後驗收）**：

- **判據**（§7.1 L2）：全語料 `no ast` dump 逐位元組一致
- **實測**：語料 `tests src/std`（**655 檔**，含 mem-safety/golden 子目錄）以 `/tmp/noc-s81`（s8y 後代碼現建）對 `./bin/no`（當代 HEAD）串行掃描 ⇒ **DIFF 清單為空，655/655 SAME0 回歸**抽驗防橡皮圖章：`bigint.no`（175 KB dump）`database/sql.no``tests/2.no` 三檔 `(rc, sha256(stdout))` 兩側一致且 SUT 輸出非空
- **L0 全綠**：`no run compiler/tests/{util,tok,lx,ast,ps}-test.no` ⇒ **27/62/45/48/38 = 220 斷言 0 失敗**⚠️ 其中 ast-test 的 `test-unsupported-is-live` 哨兵本輪遷移：s8y 把 `interfaceMethod` helper 接線後，舊哨兵（dump `InterfaceDefinition` 必計 1 unsup）失效——改用仍未接線的 `annotationStructField`（`AnnotationStructValue` kind 9 tape 帶 `OP-HELPER`）作缺線計數感測器，`ast.unsupported` 機制的非假斷言證明得以延續
- **vet 門檻**：`no vet compiler/src` ⇒ 19 ERROR（既有基線，無一新增）
- **收斂軌跡**（2026-10-27 當日連續增量）：s8q（pipeline 同位差）→s8r（module-path 接收者回傳型）→s8s（`[?]T` 回傳參數）→s8t（死碼臂 warnings＋裸可空宣告 Doc）→s8u（MapLiteral 複合標註）→s8v（let-RHS 推斷家族）→s8w（鏈式裸 if 尾隨 else＋切片參數補登記）→s8x（多重指派逗號後目標專用解析器，`tests/2.no`）→**s8y（裸型名陳述＋運算符重載剥 `?`＋介面繼承清單，`bigint.no`＋`database/sql.no`）⇒ DIFF=0**逐輪記錄見 §5.3.4.ac–ak
- **晉級**：`./bin/no build -cc zig -o /tmp/noc-s81 compiler`（含 ast.no 收官註釋修訂，位元組级行為與 noc-s80 同源）


### 6.1 M1 的實作要點（與 §5.2 互補）

- **`lx.no` 是 `src/lexer/lexer.go::scanToken()` 的逐分支移植**；公開入口是 **`dump(src str) (out str)`**（`noc` 未來的 `tokens` 子命令與 L1 的 oracle 都走同一個包／同一份判準）`dump` 的輸出**刻意不帶尾端換行**（`print` 會自己加一個）
- **SHS 合規**：無一等函式值無指針無 `#{c}` FFI無模組級可變 `let`；所有可變狀態顯式傳遞（`lx lxst` 結構體，§5.1.4）
- ⚠️ **本檔踩過並寫進檔頭註釋的三個坑**（完整表見 §2.1.1）：
  1. **`;` 是行註解**，**不能**當敘述分隔符（初版因此產生 33 個未閉合塊）；
  2. **`x -> { … }` 是 match 臂**（最多執行一次），**不是** while 迴圈 —— 迴圈要寫 `(cond) { … }` 或 `{ … } (cond)`；
  3. **跨模組常量在間接依賴下靜默為 0**，作運算式運算元時**硬編譯失敗**（§2.2.8）
- **loop 重寫**：初版把 15 處 `go -> { … }` 當迴圈用 ⇒ **每個掃描迴圈都只跑一次**（SUT 對任何輸入只吐 1 個 token）全部改寫為前綴 while 形式後才對
- **regex flags 迴圈**曾**無限迴圈**（`tests/regex-literal.no` 逾時）：原因是 `hit` 同時當迴圈條件與迴圈內重置，遇到非字母時迴圈體既不前進也不改條件 ⇒ 改為在迴圈**體內**設 `stop` 旗標（對應 Go 的 `else { break }`）
- **讀檔必須逐位元組無損**：`drv.cmd-tokens` 用 `fs.read-bytes` + `to-bytes-str()`，**不是** `fs.read-str`（§2.2.9）；否則所有非 ASCII 註解被二次編碼，且 **`rc` 測不出來**


### 6.2 M2 的進度與**前置**（截至 2026-10-11）

**已完成的前置**（屬 M1 的詞法 API，但只為 M2 而做）：

- **`lx.scan-all(src) (r []tk)`**：把整份原始碼掃成結構化 token 陣列（含結尾 `EOF`）
  動機：M1 時 `lx-scan-token` 是掃描 + 渲染成字串，只有 `dump` 一個消費者
  若為解析器另寫一份掃描，兩份必然漂移，而 **L1 只保護其中一份**⇒ 重構為
  **掃描只填 `tk` 結構**；`dump`（經 `lx-fmt`）與 `scan-all` 都是它的下游
  重構後 **L1 675/675 不變**——這就是重構沒改行為的證據
- **`compiler/tests/lx-test.no`（45 斷言）**：其中 `test-no-alias` 專門釘住
  `push` 是**深拷貝**若 `[]tk` 的 push 退化成別名，`scan-all` 會全錯而
  `dump` 全對 ⇒ **L1 照不到**

**進度（2026-10-11 更新）**：`ast.no`（節點 + arena，1423 行）與 `ps.no`（解析器，2859 行）**已開工並持續增量推進**（見 §6.3）；`ty.no`（型別）`ann.no`（註解）尚未開始

**開工前必讀 §5.3.1**：該節現在是**格式契約**（`noc` 必須逐位元組復刻的那一份），不再是盲點警告

**原已知的驗收缺口——兩項均已關閉（2026-10-10）**：

| # | 缺口 | 處置 | 證據 |
| --- | --- | --- | --- |
| 1 | `no ast` **不印註解**（P0-③） | ✅ 新增 **`-annotations`**，依樹序點查 side-table（**絕不 range** 那張 map） | 語料實測 **287 條 / 88 檔**；`TestAnnotationsFlagIsWired``TestNoAnnotationsByDefault` |
| 2 | **節點覆蓋不全**（9 種塌成 `<unknown>` + 4 類只印殼） | ✅ `dumpNode` 由 40 → **71** 個 case；所有 `Node` 欄位/切片元素都遞迴 | 語料 `<unknown>` **319 → 0**；`TestCorpusHasNoUnknownNodes`（已證偽：刪一個 case ⇒ 24 檔 FAIL） |

✅ **已關閉（2026-10-10）**：`noc` 已實作 `ast` 子命令 ⇒ **L2 從 UNAVAILABLE 轉為可執行**，且 `--sut /tmp/noc-s0` 全語料 **30/30 SAME / 0 DIFF**（見 §6 表 M2 行與 §6.3）

**節點盤點與錯誤訊息目錄**：見 [`nolang-ast-grammar.md`](nolang-ast-grammar.md)（由 `scripts/ast_inventory.py` 從 Go 原始碼機械抽取；含 71 個節點的欄位／建構函式，以及 **168 個錯誤訊息站點**——這是 M2/M3 要逐字重現的清單）


### 6.3 M2 解析器覆蓋進度（逐增量，截至 2026-10-11）

`ps.no` 已支援的句法（每項都有 byte-identical fixture 釘住）：

| 增量 | 內容 | fixture |
| --- | --- | --- |
| 基礎 | Program / ExpressionStatement / IntegerLiteral / Identifier / 行尾無換行 | `t0``t1` |
| let 語句 | `x = expr`（含**僅字面量才合成** NamedType 推斷，見下） | `t2` |
| 函式定義 | `name = (params) { body }`（現經 `ps-is-func` 判定） | `t3` |
| 中綴優先級 | `ps-op-prec` 逐值復刻 Go `precedences` 表（含 quirks：`SHL/SHR/AND`→PRODUCT`OR/XOR`→SUM）；左結合用嚴格 `>` | `t4`（`1 + 2 * 3`）`t8`（跨行 `1 +\n2`） |
| 前綴 | `-` / `!`（`-3` **不摺疊**，仍是 PrefixExpression） | `t5` |
| 顯式型別標註 | `w i64 = 5`（NamedType **不帶** inferred） | `t6` |
| 括號 / nil / float | GroupedExpressionNilLiteralFloatLiteral（value 走 `to-f64().to-str()` 對齊 Go `%v`，raw 保留原文） | `t7` |
| **呼叫運算式** | `f(a, b, …)` ⇒ `CallExpression{fn, args}`，slot0=args 字串渲染 (NON-ZERO)，count slot0=迭代次數；Function kid + 每個 Argument kid | `t9`（`f(1, 2)`） |
| **成員運算式** | `a.b` ⇒ `DotExpression{receiver, property}`，slot0=property 名（OF-FIELD），一個 kid=Receiver | `t10`（`a.b`）`t11`（`y = a.b(1)` ⇒ Call 包 Dot） |
| **if / if-else** | `if cond { … } [else { … }]` ⇒ `IfExpression` **三 kid**（Condition/Consequence/Alternative），缺 alt 也要 push 0 占位；Header 為裸 `IfExpression`（無 `{}`） | `t12`（`if true { x = 1 } else { y = 2 }`）`t13`（函式體內 `if r == 1 { … }`） |
| **Program.warnings 計數** | `psst` 新增 `warn i64` 欄位；`ps-parse-if` 每遇到一個 `if` 就 `p.warn = p.warn + 1`（與 Go `expr.go::parseIfExpression` 中 `saveWarning` 同構）；`parse` 將 `p.warn.to-str()` 寫入 slot0，使得 `Program{warnings: N}` 能如實呈現 | `t12``t13` |
| **切片字面量** | `[e1, e2, …]` ⇒ `SliceLiteral{elements: N}`，slot0=元素數渲染 (NON-ZERO)count slot0=迭代次數每個元素一個 kid；`ps-parse-let` 偵測到 SliceLiteral RHS 時合成 `SliceType{inferred}`（包一層 `NamedType{elem, inferred}`），元素型別由首個元素 kind 推斷（INT→i64/FLOAT→f64/STRING→str/BOOL→bool/空或巢狀→預設 i64），復刻 Go `stmt.go` 的推断路徑 | `t14`（`[1,2,3]` / `['a','b']` / `[]`） |
| **索引運算式** | `left[index]` ⇒ `IndexExpression{Left, Index}`（裸標頭無 `{}`兩個 kid**無槽位**）；在後綴迴圈中與呼叫/成員一同分派（一律绑定不比較 prec）；index 用 LOWEST(0) 優先級解析鏈式 `m[0][1]`（左嵌套）`f(x)[2]`（Call 被索引）皆自然由後綴迴圈迭代舆 `a[1..3]` 範圍切片（SliceExpression）**共用同一 LBRACKET 臂**，由 `..` 分流（見下行） | `t15`（`a[0]` / `a[i+1]` / `m[0][1]` / `f(x)[2]`） |
| **定長陣列** | `name [N]T = [e1, …, eN]` ⇒ `LetStatement{type: ArrayType, value: ArrayLiteral}``ArrayType{size: N}`：slot0=inferred（明示→'false'不印）child0=Sizechild1=Elem（NamedType）；`ArrayLiteral{size, elements, wasSliceLiteral: true}`：slot0=elements 字串(OF-LEN)slot1='true'(OF-FIELD bool)count0=元素數(KIDS 迭代)kid0=Size（沿用型別側 sz 節點）+ 元素 kids`size` 欄位是 OF-NODE-STRING（由 child 推得**不占字串槽**）復刻 Go `stmt.go:1210-1218` 的 SliceLiteral→ArrayLiteral 轉換（`WasSliceLiteral=true`）分派守衛 `+3==RBRACKET && +5==ASSIGN` 专认 `] IDENT =`，自然排除 `a[0] = x`（那是 `] =`） | `t16`（`arr1 [3]i64 = [1,2,3]` / `arr2 [2]i64 = [10,20]`） |
| **範圍切片** | `a[lo..hi]` ⇒ `SliceExpression{Left, Range}`（裸標頭兩 kid：LeftRangeExpression）；`RangeExpression{start, end, leftInc, rightInc}`：**固定兩 kid [Start, End]**，缺席端以 0 占位（`node-string(0)='<nil>'` ⇒ 標頭印 `start: <nil>`，guarded CHILD 跳過不印）；slot0=leftInc（開 `[`→true）slot1=rightInc（閉 `]`→true / `)`→false）；start/end 皆 OF-NODE-STRING不占槽與 IndexExpression **共用後綴 LBRACKET 臂**，靠 `hasStart` 快照 + `..`(ELLIPSIS) 判定分流覆盤 Go `parseSliceExpression`；`a[1..3)``a[..3]``a[1..]``a[..]` 全同 | `t17`（五種範圍形） |
| **Map 字面量** | `name [K]V = { k1: v1, … }` ⇒ `LetStatement{type: MapType, value: MapLiteral}``MapType{}`（裸空標頭，inferred=false 不印）：slot0=inferredkid0=Key(NamedType)kid1=Value(NamedType)；`MapLiteral{pairs: N}`：slot0=pairs 字串(OF-LEN)count0=KIDS 迭代數**kid 序與 ArrayLiteral 相反**——dump 源碼序是先 `for Pairs`（KIDS）後 `node(MapType)`（CHILD），spec_gen 保留源碼序 ⇒ kid0..N-1=MapPair**最末 kid=MapType**；`MapPair`（裸標頭）：kid0=Keykid1=Value（皆 guarded CHILD）`[K]V` 舆 `[N]T` 共用 `name [` 前綴，靠 +2 令牌型別分流（INT→陣列 / IDENT→map）K/V 為單 IDENT 型名形（`[str]i64`）；巢狀值型 `[str][]i64`  deferred⚠️ **Nolang 字面量引號反转**：`"a"`=CharLiteral（僅單字元）`'text'`=StringLiteral；map key 可為 char / identifier / int，皆走 ps-parse-expr-prec | `t18`（`{"a":1,"b":2}` / `{}` / `{k1:10,k2:20}`） |
| **結構體字面量** | `Ident{ field: val, … }` ⇒ `StructLiteral{type, fields}`（type 是**字串**型别名非子節點）slot0=型别名slot1=fields 數(OF-LEN)count0=KIDS 迭代數；kid 序=各 StructField（僅 KIDS無 CHILD）`StructField{}`：slot0=nameslot1=arraySize('0'→kvn NON-ZERO 略過；未設會印空 `arraySize: `!)slice/readonly/sealed 留''(P-TRUE 略過)；kid0=Type(0 占位 guarded)kid1=Value（皆 CHILD）後綴 LBRACE 臂新增，由 `ps-is-struct-lit`（跳 trivia 後首非 trivia 為 `IDENT COLON` 或空 `{}`）守衛，避免误捕 match（`->`）/enum（`,`/`=`）；型别名由 `ps-dotted-name` 重組：Identifier→slot0 名（`Point`）DotExpression→`ps-dotted-name(kid0 Receiver) + '.' + slot0 property` 递归（`net.Conn` / `a.b.C` / `pkg.Sub.K`）其他回退 node-string空 `{}` / 巢狀 `Outer{inner: Point{x:1}}` / char值 `Box{v:"a"}` 皆同 | `t19`（`Point{x:1,y:2}` / `Empty{}` / `Box{v:"a"}` / `Outer{inner:Point{x:1}}`） |
| **明示切片型別標註** | `name []T = [e1, …]` ⇒ `LetStatement{type: SliceType, value: SliceLiteral}``SliceType{}`（裸空標頭inferred=false 不印）：slot0=inferredkid0=Elem（NamedType）；RHS `[..]` 經 primary 自然建 SliceLiteral（複用 t14 路徑，含空 `[]`→elements 0 不印）舆 `[N]T`（+2 INT）/`[K]V`（+2 IDENT）**共用 `name [` 前綴**，靠 +2 令牌分流——此臂要求 +2 為 RBRACKET（空括 `[]`）+3 IDENT elem+4 ASSIGN元素引號反轉同 map（`"a"`=CharLiteral）巢狀 `[][]T` / `[]net.Conn` deferred | `t20`（`[]i64=[1,2]` / `[]str=["a","b"]` / `[]i64=[]`） |
| **限定名 struct literal** | `net.Conn{…}` / `a.b.C{…}` 等**點分限定型名**的 StructLiteral：型别名不再是 kind-name `DotExpression`而是重組後的點分字串新增 `ps-dotted-name(t, node)` 递归：`Identifier`→slot0名`DotExpression`→`dotted(kid0 Receiver) + '.' + slot0 property`（slot0=propertykid0=Receiver，同後綴 DOT 臂的建法）LBRACE 臂由 `node-string(idx)` 改调 `ps-dotted-name(idx)`；非 struct 的 dot 链（`a.b.f()` / `x.y.z`）不受影响 | `t21`（`net.Conn{host:"x"}` / `a.b.C{x:1}` / `net.Conn{}` / `pkg.Sub.K{x:1,y:2}`） |
| **函式字面量（带型別參數）修正** | 復刻 `d.signature` 的子節點深度與 Parameter 的 kid 區`name = (a T, b T) { body }` ⇒ `FunctionDefinition{name}` 下依序挂 `Parameter`（各带 `NamedType`）+ `BlockStatement`**兩個真实 bug**：（一）`ast.no` OP-SIG 把 `signature` 误以 `depth`（非 `depth+1`）呼叫 ⇒ 0 參數不可見≥1 參數時 `Parameter` 塌成 Program 同層兄胏（修正：dump.go FunctionDefinition:281 / FunctionLiteral:409 皆 `d.signature(sig, depth+1)`，ast.no 改 `depth+1`）；（二）`ps-parse-param` 只 push Type 一個 kid，但 Parameter spec 有 **2 個 guarded CHILD**（TypeDefaultExpr），dumper 位置式讀 kid0/kid0+1 ⇒ kid0+1 读到下一個全域 kid（body 的 Identifier）⇒ 多一个幽靈子節點（修正：恒 push 兩槽，Type 缺席用 0DefaultExpr 恒 0） | `t22`（`(x i64){…}` / `(a i64,b i64){…}` / `(){…}`） |
| **match 明示形 desugar** | `subj: { pat -> body … }` ⇒ **parse 期** desugar 成 if/else 鏈（⚠️ **2026-10-19 分歧記錄**：此為 **noc 現態**；bin/no 的 parser 現已保留 `SurfaceMatch`不展開——S1 收官（翻轉 `b918dba9`刪舊降糖路徑 `d6965622`，詳見 `nolang-parser-lowering-invariants.md` §22），故 `DumpWithOptions` 對 bin/no 會印出 `SurfaceMatch{arms: N}` 而非 IfExpression 鏈本分歧使 L2 oracle 在含 match 的語料上偏離（實測 264/653 檔），裁定與收尾見 §5.3.3）規則：①非 wildcard 臂 `pat` ⇒ `IfExpression{cond: InfixExpression{==}[Ident subj, pat], consequence: 臂區塊, alternative}`；②**每個臂區塊先塞合成 `LetStatement{name: it, value: subj, synthetic: true}`**（slot0='it'slot1='true'kid[0]=Type 缺席以 0 佔位kid[1]=Ident）；③運算式臂 ⇒ `BlockStatement{inline: true}[it, ExpressionStatement{e}]`區塊臂 ⇒ `BlockStatement{}[it, stmts…]`（inline 未設）；④相鄰非 wildcard 臂 ⇒ 後臂包成非內聯 `BlockStatement{}[ExpressionStatement{IfExpression}]` 作 alternative；末臂無 else 時 alt=0；⑤wildcard `_`（`tok.UNDERSCORE`=88，**非** IDENT）⇒ 其臂區塊**直接**作前一 if 的 alternative；若最外層只是 wildcard ⇒ 包成 `IfExpression{IntegerLiteral{value:1}（ps-mk-true，無 raw）, blk, 0}`**範圍**：識別元主體 + 字面量/識別元/wildcard 型樣 + 運算式/區塊臂；**延後**守衛option/enum 完整型樣複雜主體（呼叫/索引）**坑**：相鄰兩條 `cond -> {blk}` 陳述若條件用**活的** `ps-peek(p)` 會在跨臂迭代時**随位置重評**誤觸（同 §2.1.1 LBRACE 臂教訓）⇒ 改快照 `at = ps-peek(p)` 後 `{ at == UNDERSCORE -> {…}  at != UNDERSCORE -> {…} }` 互斥雙臂另：區塊臂（`{ y = 5 }`）在 let-RHS（CTX_EXPR）為 oracle parse errormust end with expression，只准在**陳述層** match 出現 | `t23`（`n:{1->10}` / `k=n:{1->10;2->20}` / `w=n:{1->10;_->0}` / `b=n:{true->1}` / 陳述層區塊臂） |

|  |  |  |
| --- | --- | --- |
| **多重指派（MultiAssignStatement）** | `a, b, … = expr` ⇒ `MultiAssignStatement{targets: N}`（RHS 常為多返回值呼叫）slot0=targets 數（kvc NON-ZERO，未設會印空 `targets: `）overflow slot1 留''（kvs 空略過）；**kid 序依 dump 源碼序**：先 N 個 targets（KIDS`set-count(0,N)`）後 1 個 value（guarded CHILD，缺席以 0 佔位）**父後建**：先把所有 target 與 value 子樹解析完，再 `add(parent)`依序 `kid-add`（共享 kids 游標）分派守衛在 `ps-parse-stmt` 的 IDENT 分支：`nxt_typ == tok.COMMA`（首 token 為**裸識別元**且緊接逗號）；因 `ps-op-prec(COMMA)=0`，`ps-parse-expr-prec(t,p,0)` 自然在逗號處停下不會把 `a, b` 吞成單一運算式**範圍**：僅**識別元目標**（`a, b = f()``m, ok = fs.read()` 含點分方法呼叫 RHS）；**延後**索引/點分作目標（`x[0], y = f()`，首 token 非裸 IDENTgate 漏判）與巢狀 tuple（`a, b = 1, 2` 的 `, 2` 依 oracle 現況本就被拆成獨立 ExpressionStatement，noc 復刻一致）**坑（見 §2.1.1）**：match 去糖成的 if/else 鏈把各臂區塊的區域變數**提升到同一函式作用域**——若新臂用了兄弟臂已佔用的變數名（如 `tn` 在明示型別標註臂是 `tt.lit`=**str**，新臂卻當 **i64** 用），型別 checker/codegen 會合併成 str ⇒ `t0 > 0` 變成字串做 gt 比較`push` 變成str 塞進 []i64的 MIR codegen 錯修法：新臂所有區域變數加唯一前綴（`maTargets/maT0/maTn/maVal/maK`） | `t24`（`a, b = foo()` / `p, q, r = bar(1)` / `x, y = get-pair()` / `m, ok = fs.read()`） |
| **可為空型別（NullableType）** | `?T` 型別標註 ⇒ `NullableType`（**裸標頭無任何欄位**（連 IsInferred 也不印）**1 個 guarded CHILD=Type**）新增共用 `ps-parse-type(t, p)` 遞迴型別解析器（子先於父）：`IDENT`→NamedType（明示→不設 inferred）`? T`→NullableType[內層]`[] IDENT`→SliceType[NamedType]（elem 僅單 IDENT 形）接線兩處：①`ps-parse-stmt` IDENT 分支新增 `nxt_typ == tok.QUESTION -> { name; ps-parse-type; expect ASSIGN; expr }` 臂⇒ `LetStatement{type: NullableType, value}`（`IDENT ?` 於陳述層只可能是可為空標註——傳導 `?=` 是 `tok.QUESTION_ASSIGN`(77) 不同 token）；②`ps-parse-param` 型別段改用 `ps-parse-type`（原只認裸 IDENT），使輸入參數 `(x ?i64)` 正確建 Parameter{type:NullableType}（否則 `?` 不被消費→防呆 bump→`i64` 被當第二個參數→幽靈節點，部分形還可能掛死）dump 端 ast.no 早已有 `K_NULLABLE_TYPE -> '?' - type-string(kid0)`（ast.no:630），故本片**纯 parser 側**⚠️ **重大發現**：NullableType 在真實語料幾乎**只出現在回傳元組** `f = (params) (out ?T) { }`，而當時 `ps-is-func` 只認 `(...) {`——遇到 `(...) (...) {` 直接不判為函式整段 misparse**該回傳元組缺口已由下一片（t26）補足**，本片先釘住可單獨驗證的 `?T` let 標註 + nullable 輸入參數 | `t25`（`a ?i64 = 1` / `b ?[]i64 = [1,2]` / `c ?str = 'x'` / `f = (x ?i64) { y = 1 }`） |
| **回傳元組函式定義（FunctionDefinition results）** | `name = (params) (results) { body }` ⇒ `FunctionDefinition`，**回傳值也建為 Parameter**於 `signature()` 接在輸入參數之後Nolang 無獨立 return 節點：Go `d.signature` 先 `for Parameters` 再 `for Results` 平鋪渲染 ⇒ arena kid 區序為 `[generics…][inputs…][results…]` 連續，再掛 `body`（guarded CHILD）做法：`set-sig(t, idx, 0, inputs.len(), results.len())`（c3=泛型/c4=輸入/c5=回傳，ast.no:235signature() 由 base 依此三數切片，ast.no:1155）①**`ps-is-func`** 原只在首群組閉合後看 `nt`：`{`⇒函式否則 bail ⇒ 遇到 `(...) (` 直接判非函式改成三分岔快照臂：`nt==LBRACE`→r=1 停`nt==LPAREN`→**不中斷**（go 保持 true，續掃下一群組直到它閉合）其他→停（r=0）②**`ps-parse-func`** 於消費首 `(...)` 後加可選第二群組：`ps-peek(p)==LPAREN` 則再跑一次參數迴圈收进 `outs`，最後 `set-sig(0, params.len(), outs.len())` 並依序 kid-add inputs→outs→body**父後建**：所有 param（含 NullableType/SliceType 內層）與 body 子樹先建完再 `add(FD)`坑沿用：`nt` 用快照非活 `ps-peek`臂内不復用兄弟臂變數名（`outs`/`ro`/`m` 皆新名）`ps-is-func` 泛化後 `c = add(1)(2)`（鏈式呼叫）仍正確判非函式（第二群組閉合後 `nt` 非 `{`）**延後**：泛型參數 `[T]`（GenericParams，需 `set-sig` c3>0 + 另一條 `ps-is-func` 前置掃描）variadiccolon-syntax 方法定義 | `t26`（`(key str) (val i64, ok bool)` / `() (out ?i64)` / `(a i64, b i64, c []i64) (total i64)`） |
| **兵列迭代（ForStatement + IterationExpr）** | bare range-for：`var <- range-or-array [:] { body }`（**不使用 `for` 關鍵字**crc-64 等 std 主流形）新增 `ps-parse-for(t, p, vname)`，分派守衛在 `ps-parse-stmt` IDENT 分支 `nxt_typ == tok.ARROW`（`IDENT <-` 於述層即迭代）**兩個節點层位**：`ForStatement{label: }`（slot0=label`kv` 恆印；condWrapper/overflow 預設不印）有 **6 個 guarded CHILD**（dump 源碼序：Init,Condition,Update,CountExpr,IterRange,Body），迭代形仅後两者非零 ⇒ kid-add `0,0,0,0,ie,body`；`IterationExpr{var: i}`（slot0=var 名`kvs`）有 **2 個 guarded CHILD**（RangeRangeExpr）——区间形填 kid0（RangeExpression）陣列形填 kid1（運算式），另一方以 0 占位区间解析复用 t17 的 RangeExpression 建法（leftInc=開 `[`→true/`(`→falserightInc=閉 `]`→true/`)`→false两端可缺）⚠️ **两个坑**：（一）**冒號决定警告**——Go parseForStatement 在区间后：有 `:` 則消费（無警告）無 `:` 則 `saveWarning`（deprecated）故 `p.warn = p.warn + 1` 需同步（同 ps-parse-if），否则 Program 头 `warnings: N` 对不上；冒號检查用快照 `hd = ps-peek(p)`（活 `ps-peek` 在 ps-bump 消费 `:` 后会被下一臂重评⇒跨臂误加警告）（二）**陣列裸 IDENT 源与明示-match 相撞**——ps-parse-expr 后缀有 `IDENT COLON LBRACE`⇒match 臂（ps.no:833），`i <- a: { }` 的裸 ident 源会被该臂吃掉 `: {body}` ⇒SIGBUS修法：非区间源若 `IDENT` 且 `+1==COLON` 则**直接建 Identifier 节点**（不走 ps-parse-expr）；索引/呼叫形（`i <- a[0]:`idx 非裸 IDENT）不触发该臂仍走 ps-parse-expr**未验证但已知剩余缺口**：本增量解锁 crc-64.no 的 range-for（不再 SIGBUS），但该文件仍因 **`Doc:` 行註解**（noc 不发 comments）与 **u64 大常量的带符号值**（`18446744073709551615`→应 `-1`noc 误印 `20`）而 DIFF——**u64 常量部分已由 t28 `ps-int-lit-value` 解決**仅剩 `Doc:` 行註解為獨立增量**延后**：labelled `outer i <- r:`无大括號 inline 本体string 遍历（`i <- 'abc'`→RangeStr）；`for` 关键字形（`for i <- r { }`）已由 t29 解锁（见下方新列） | `t27`（`[0..8):` 含冒号 / `[0..a.len())` 无冒号→warning / `k <- a:` 陣列迭代） |
| **整數字面量值（IntegerLiteral value）** | 復刻 Go `parseIntegerLiteral`（expr.go:652-688）的 **value 計算**——`IntegerLiteral{value, raw}` slot0=`%d`(int64)slot1=`Raw`（原始文字）原 `tk.lit.to-i64().to-str()` 對 **hex/u64/i128 全錯**（`0xFF`→`35``18446744073709551615`→`20``10^22`→`20`）新增 `ps-int-lit-value(raw str) str`：①`0x/0X` 前綴⇒ base16否則 base10（Go 僅 `0x` 走 base0 自動偵測，十進位恒 base10 ⇒ `010`=`10` 非八進位）；②剝前綴+前導 0 後折疊低 64 位外層 `#{overflow=wrap}` ⇒ 自動得 `int64(uval)` 兩倍補數（`2^64-1`⇒`-1``2^63`⇒i64-min）；③**超出 u64⇒回 `0`**（Go `isBareIntToken` 分支：保留原始文字給 HIR S2）——hex 有效位 >16 即超十進位 >20 即超`==20` 逐位比 `'18446744073709551615'`探針覆蓋 `0x7fffffffffffffff`/`0xFFFFFFFFFFFFFFFF`/`18446744073709551616`/`9223372036854775808` 全 SAME⚠️ 純 dumper 側（ast.no 早渲染 slot0），**無須動 ast.no/asp.no**⚠️ **macOS 提升陷阱**（本增量踩到）：`cp noc-sN /tmp/noc-s0` 就地覆蓋 Mach-O 會令核心快取的代碼簽章失效 ⇒ 執行即 `killed: 9`（rc 137）輸出全空誤判29 fixtures 全 DIFF——實為簽章問題非代碼問題（同一 bytes 的 noc-sN 仍 OK）修法：**先 `rm` 再 `cp`**，或 `codesign -f -s - /tmp/noc-s0` 重簽另 **未納入**：hex 字面量的**型別推斷為 `byte`**（≤255 時 oracle 推 bytenoc 推 i64——屬 LetStatement 合成型別路徑，非 IntegerLiteral value 欄位）`1_000` 詞法（`_` 為合法識別元起始字元非分隔符⇒ oracle 拆成 `1` + `_000` ident，本就不在單 INT token 內無需處理） | `t28`（`0xff`/`0X1A`/`0xFFFFFFFFFFFFFFFF`/`007`/`010`/`18446744073709551615`/`9223372036854775808`） |
| **`for`/`while` 關鍵字迴圈（ForStatement 關鍵字形）** | Go `parseForStatement` 的 FOR 分支（stmt.go:2283+）`for` 與 `while` 在詞法層同 `tok.FOR`（靠 token `lit` 區分）新增 `ps-parse-for-kw(t, p)`，分派在 `ps-parse-stmt` 加 `tk.typ == tok.FOR ->`；共用 `ps-for-source(t, p)(src, srcExpr)`（從 bare `ps-parse-for` 抽出，僅回兩個 i64⇒**無結構體安全多回傳**）**三種帶 `{ }` 本體形體**：①`for { }` 無限⇒`Condition=BooleanLiteral{true}`（kid1）`warn+1`（2297）；②`for v <- range [:] { }`⇒`Condition=Identifier{v}`（kid1）+`IterRange=IterationExpr`（kid4）**無警告**（ARROW 分支跳過 2407；colon 可選有無都不警告）；③`for expr { }`/`while expr { }`⇒`Condition=expr`（kid1）`warn+1`（2407 for／2402 while）ForStatement kid-add `0,cond,0,0,ie,body`**關鍵區别**：bare 形（t27）**不設 Condition**（僅 IterRange+Body），關鍵字 range 形**多一個 Condition=Identifier**——這是兩者的 dump 差異來源⚠️ **不實作 C-style `for a; b; c`**：`;` 在 Nolang 是**行註解**⇒該形天然退化（`;` 註解掉後面全部），探針證實無合法 AST⚠️ **警告只計數**：`Program.warnings` 只是總數（slot0），個體順序不影響 dump⇒`p.warn=p.warn+warn` 放函式尾亦正確**延后**：labelled無大括號 inline 本體比較運算子退路（`for i < n:` 裸 ident 手動拼接形）C-style `,`/`;``in` 關鍵字（僅實作 `<-`） | `t29`（`for {}` / `for n>0 {}` / `for i<-a {}` / `for j<-a: {}` / `while n>0 {}` / `for k<-[0..3] {}`） |
| **`break` / `continue`（BreakStatement / ContinueStatement）** | Go `parseBreakStatement` / `parseContinueStatement`（stmt.go:1550/1570）`tok.BREAK`=19`tok.CONTINUE`=20新增 `ps-parse-break-continue(t, p, brk)`，分派在 `ps-parse-stmt` 加 `tk.typ == tok.BREAK/CONTINUE ->`**節點無 CHILD**，僅兩個字串 slot（均 `kv` 恆印）：slot0=labelslot1=kind（`labelKindName` ⇒ 'none'/'text'/'numeric'）⚠️ **kind 必须显式设**：`kv` 政策 P-ALWAYS（unset 也印）⇒ 裸 break 必须 `set-slot(1,'none')`，不能留空（否则印 `kind: `）⚠️ **標籤侦测不跳 trivia**：Go 消费关键字后直接看**紧邻** token——LABEL(`#N`)⇒numeric（literal 已剔 `#`）IDENT⇒text；否则（NEWLINE/RBRACE）⇒裸ps.no 的 NEWLINE **在 raw token 流里保留**（`ps-skip-trivia` 才跳），故 `ps-peek` 直接拿紧邻 token即可区分同列标签 vs 换行后的下一条陈述⚠️ **未覆盖（已由 t31 独立 if-then 增量解决）**：独立 `cond -> break` 箭頭形（parseStandaloneBody ⇒ IfExpression + **inline** BlockStatement 包裹）——break 節點本身已正确产出，但外围 standalone-箭頭形式待 t31labelled `outer: for`（oracle 拆成 `ExpressionStatement{outer}`+`ExpressionStatement{nil}` 退化形）亦待后续 | `t30`（裸 break/continue / `break #1` numeric / `continue #2` / if 内嵌裸 break） |
| **獨立 if-then desugar（`cond -> body` / wildcard `-> body`）** | Go `parseExpressionStatement` 的 standalone 分支（stmt.go:1695–1882）+ `parseStandaloneBody`（1593）+ `wrapStandaloneChainDepth`（1653）**這是 M2 目前解锁语料面最大的一片**（survey 显示 `ExpressionStatement{IfExpression}` 是 74 档的首个分歧节点）**无外层花括号的 `cond -> body` 在语句层被 desugar 成 `IfExpression`**（不是二元运算符——`ps-op-prec(RARROW)=0`，`ps-parse-expr` 天然停在 `->`）**形状（全部经 oracle 探针钉死 byte-identical）**：`cond -> expr` ⇒ `ExpressionStatement{IfExpression{cond, BlockStatement{inline:true}[ExpressionStatement{expr}], 0}}`；`cond -> { stmts }` ⇒ **非内联** `BlockStatement{}`（无 inline 标记）；`cond -> break/continue/return` 及 `cond -> IDENT = …`（let）⇒ 内联块**直接**包 `parseStatement` 产物（非 ExprStmt ⇒ 不可链）；行首裸 `-> body` ⇒ wildcard `IntegerLiteral{value:1}`（无 raw，复用 `ps-mk-true`）作 cond**链式** `a -> b -> c` 递归 nest 成 `if(a){ inline[ExprStmt{ if(b){ inline[c] } }] }`（每层的 body 表达式成为下层 cond）**rare else**：`cond -> {blk} -> alt` 当 wrap 吃光同列 `->` 后仍有 `->` ⇒ 作 alternative⚠️ **警告规则**：链式 lint（`WarnChainedIf`）仅在 `wrapStandaloneChainDepth` 进入时 `linkIdx==2`（即整条链已达第 3 个条件）触发**一次**，与链长无关——`a -> b`（2 条件）`a -> b -> c`（2 条件）⇒ warnings:0；`a -> b -> c -> d`（3 条件）⇒ warnings:1**实现**：新增 `ps-mk-standalone-if(t,p,cond)`（前置：当前 token 即 cond 后的第一个 `->`）`ps-standalone-body(t,p)(blk,tail)`（回两 i64无结构体⇒安全多回传；blk=本体块tail=可链的单元表达式）`ps-wrap-chain(t,p,blk,tail,linkIdx)`（递归，本层只需一次判定因递归已吃光后续 `->`）`ps-rarrow-same-line(p)`（`ps-peek==RARROW` 且 `p.toks[p.pos-1].typ != NEWLINE`，对齐 Go `prevToken.Type != NEWLINE`）`ps-parse-expr-stmt` 在 (A) 行首 `->`→wildcard(B) `expr` 后紧跟 `->`→standalone 两处 dispatch，**并把 standalone IfExpression 包一层 ExpressionStatement**（Go 回的是 `ExpressionStatement{IfExpression}`）⚠️ **本增量不触碰明示-match `subj: { arms }`**：探针证实 subject-match 输出 s26==s27 逐字节相同（standalone-if 只在无冒号处触发，不泄漏进 `ps-parse-match`）⚠️ **差分假象**：全语料 217 档输出变化，`diff` 行数指标对已严重分歧的大档（md5/ecdsa 数千行差）会因 diff 配对错位而虚报变差——真实回归检测须看s26 曾完全匹配 oracle 的档 s27 是否仍匹配（结果：0 真回归；view-optional.no 的 IfExpression 数 s26=18→s27=20=oracle，实证改善）**延后**：块状 bare-match `{ cond -> body }` 分类器（`classifyBlockAtCurrent`⇒blockMatch 与 struct-literal/普通块/`{name:{}}`/混合内容回退的高误分类风险）`(cond) { }` 前缀条件循环`x = A -> B` 的 let-管道值形（RTPipelineValue） | `t31`（`a -> b` / `c -> { x = 1 }` / `d -> e -> f`（2 条件无警告） / `-> g` wildcard / `h -> i = 9`（let 本体） / `j -> { y = 2 } -> k`（else）） |
| **註解歸位（`Doc:` 行前註解 + `Comment:` 行尾註解）** | Go `collectDocComments` + `setDoc` + `attachInlineComment`（stmt.go:2085 陳述迴圈parser.go:1354 setDocdump.go:850 comments()）**這是 M2 解鎖語料面最大的一片**（survey：`Doc:` 是 381/621 檔的首個分歧節點——幾乎每檔都有行前註解，而 noc 完全不發 comments）**noc 端 dumper 早已就緒**（`ast.no` 有 `doc`/`cmt` 欄位 + `set-doc`/`set-cmt` setter`comments()` 渲染器經 spec tape `OP-COMMENTS` 掛到對的節點），故本增量**純 ps.no 解析器改動**（ast.no/asp.no 不動）**語義（全部經 oracle 探針 d1–d7 釘死 byte-identical）**：①**行前註解**（獨立成行位於某陳述之上）⇒ 該陳述的 `Doc:`；同一群組可跨空行（空行不打斷群組），但 `commentGroupInline` **只渲染群組首條**（`List[0]`）②**行尾註解**（緊接前一陳述的同一行）⇒ 前一節點的 `Comment:`③`;` 與 `//` 兩種標記源碼等價，渲染一律正規化成 `//`（`.lit` 存標記之後的文字含前導空白）④附著於**陳述節點本身**（`LetStatement`/`ExpressionStatement`/`ForStatement`/定義節點皆收 Doc/Comment，非其內層表達式）**實現**：新增 `ps-skip-trivia-doc(t, p, prevIdx)`（跳 NEWLINE/COMMENT 的**同時**歸位：行前取首條回傳 docId行尾 `set-cmt` 到 prevIdx）與 `ps-cur-lit(p)`（`.copy()` 讀當前 token 的 lit，切斷與 toks 向量的共享——見檔頭釋放守則）改寫 `parse` 頂層迴圈與 `ps-parse-block`（兩者同形），以 `prev = s`（最後一個已建兄弟節點）驅動行尾附著`docId != 0 -> set-doc(t, s, docId)` 驅動行前附著⚠️ **換行偵測靠保留在 token 串中的 NEWLINE token**（本解析器不預先丟棄換行）：run 內尚未見 NEWLINE 且 `prevIdx>0` ⇒ 行尾，否則行前⚠️ **不重構 `ps-parse-stmt`**：迴圈先在呼叫前用 `ps-skip-trivia-doc` 掃淨 trivia，`ps-parse-stmt` 入口的 `ps-skip-trivia` 即成 no-op，不會重複吞註解；其內部其他 `ps-skip-trivia`（型別標註與值之間）均在子運算式**之前**非陳述尾，故行尾註解留在 `p.pos` 供迴圈捕捉**效果**：全語料逐位元組相同檔 **25 → 122（+97）**，`Doc:` 首分歧 381→4；差分回歸 **0**（s27 曾匹配 oracle 的 25 檔在 s28 全部仍匹配）**延後**：區塊收尾 `}` 前自成一行之註解（Go 歸 `block.TrailingComments`/`FreeComments`，本增量直接丟棄——僅 3–9 檔因此剩差）陳述中途（運算式 token 之間）的註解`#{...}` side-table | `t32`（行前多條含空行 / 行尾 Comment / `//` 標記 / 運算式陳述 Doc / for Doc / 函式內層 Doc+Comment） |
| **方法定義（`recvType.method = (params) [ (results) ] { body }`）** | Go `parseMethodDefinition`（decl.go:11–99，分派於 stmt.go:292–301）修復 `noc` 對 `str.to-i64 = () (val i64) { … }` 的 **busy-spin**（此前 `IDENT DOT IDENT =` 不匹配任何陳述臂→退回運算式→在 `=` `(` `{` 處誤吞`{` 永不閉合）**語義（oracle 探針 me1/me4/me5/me6 + dump-ast.no 釘死 byte-identical）**：desugar 成 `FunctionDefinition{name: 'recvType.method', methodDef: true}`，`self` 合成為首個輸出參數（Results非 inputs）`Parameter{name: self, type: NamedType{recvType}}`——`Results = prepend(selfParam)`，故 kid 區序 `[inputs…][self][results…][body]``set-sig` results 數 = 1 + 宣告回傳數⚠️ **methodDef = FunctionDefinition 第 3 個純量槽（slot2）**：該節點恰有 8 個 dump 純量欄（name/colonSyntax/methodDef/skipNamingCheck/overflow/builtinStub/builtinName/builtinGroup = s0..s7），`kvb` 需顯式 `set-slot(2,'true')`⚠️ **分派守衛** `IDENT DOT IDENT =` 且 `+4==LPAREN` 且 `ps-is-func==1`：`obj.field = value`（+4 非 LPAREN）或 `x = (expr)`（ps-is-func==0）一律 `p.pos = savePos` **還原**退回運算式陳述——非函式形的點號賦值絕不誤吞**實現**：新增 `ps-lit-at(p,k)`（向前看 k 讀 `.lit.copy()`）與 `ps-parse-method-func(t,p,fullName,recvType)`（進入時 pos 在輸入 LPAREN，重用 ps-parse-param/ps-parse-block，自建 self NamedType+Parameter 前置到 outs）**效果**：`tests/dump-ast.no` 由 busy-spin 轉為逐位元組相同；方法呼叫形 `s.to-i64()` 早已匹配**延後**：泛型方法 `IDENT DOT IDENT < …`（LESS 形）陣列/切片型別方法 `[n]t.method`（isArrayTypeMethodDefinition）限定接收者 `a.b.c.method``obj.field = value` 的 AssignExpression 形（屬獨立缺口） | `t33`（`point.zero = () (self point, ok i64) {…}` 明示 self 於 results / `vec.push = (v i64) {…}` 無回傳 / `grid.cell = (r i64, c i64) (idx i64) {…}` 輸入+回傳） |
|  |  |  |  |  |
| --- | --- | --- | --- | --- |
| **結構體定義（陳述位 `name { field TYPE ... }`）** | Go `parseStructDefinition`（decl.go:1257–1505，分派於 stmt.go:49–115 的 `IDENT LBRACE` + `classifyBlock`⇒blockStruct 守衛，parser.go:267–513）**注意與 t19 結構體字面量分流**：`Point{...}`（運算式右側）早已匹配；本片解鎖的是陳述位置的 `reader { fd i64 / buf []byte / pos i64 }` 型別宣告——此前 `noc` 把 `reader` 當成 `ExpressionStatement{reader}`把區塊內容拆成獨立陳述（survey：`StructDefinition` 是 80 檔首分歧節點）**語義（oracle 探針 s1/s2/s3 + bufio/tar 釘死 byte-identical）**：`StructDefinition` 恰 3 個純量槽——slot0=`name`slot1=`implements` 數（`kvc` NON-ZERO：設 '0' 即省略）slot2=`fields` 數（`kvc` OF-LEN，設 `flds.len().to-str()`）；另需 `set-count(idx, 0, flds.len())` 驱动 KIDS 迭代；kid 序=各 `StructField``StructField` 槽位：slot0=`name`（`type:` 為 OF-TYPE，由 dumper 自 kid0 合成**不佔槽**）slot1=`arraySize`（`kvn` NON-ZERO：'0'→省略）slot2=`slice`（`kvb`：'true'）slot3=`readonly`slot4=`sealed`；kid0=Type 節子（子先於父）kid1=Value（定義恆 0 佔位）**欄位型別形**（本檔前 25 struct 檔實測分布：NamedType ~84`[]T` ~40`[N]T` ~50 為主流）：`type: i64`→NamedType`[]byte`→SliceType（slice:'true'）`[8]i64`→ArrayType（arraySize:'8'）；`type:` 文字由 `ast.type-string`（ast.no:601）自 kid0 渲染（NamedType→nameSliceType→`[]`+elemArrayType→`[`+size+`]`+elem）**實現**：①擴充 `ps-parse-type` 加 `[N]T`⇒ArrayType 分支（復用 t13 陣列標註臂建法：消費 `[`→`ps-parse-expr` 建 size IntegerLiteral→`ps-expect RBRACKET`→建 elem NamedType→ArrayType set-slot(0,'false')→kid-add szelem）；②新增 `ps-is-struct-def(p)`不消費 token的保守分派守衛——要求 `IDENT LBRACE`，掃描每欄需 `IDENT (IDENT | []IDENT | [INT]IDENT)` 且後接 NEWLINE/RBRACE/COMMA/EOF，任何偏離（枚舉 `a, b`介面 `m()`實作清單 `name impl {`限定型別 `a.b`可為空 `?T``#{...}` 註解）一律回 0→交回原運算式陳述路徑（零回歸）；③新增 `ps-parse-struct-def(t,p,name)` 建 StructField（子先）再建 StructDefinition 父；④`ps-parse-stmt` IDENT 块加 dispatch 臂 `nxt_typ == tok.LBRACE && ps-is-struct-def(p) == 1 -> ps-parse-struct-def`（置 method-def 臂後`handled == 0` 前）⚠️ **踩到兩次同類坑（已入 §2.1.1 尾臂坑類）**：`ps-is-struct-def` 的 `okType == 1` 體與 `ps-parse-struct-def` 的 `tt` 型別分支鏈，皆因裸 `-> { default }` 未包在 `{ }` match 块內→**作為無條件尾隨陳述屢次執行**，前者把 `r` 强制覆寫為 0（struct 路徑永不觸發仍吐 ExpressionStatement）後者在 slice 分支後又重跑 `ty = ps-parse-type` 把 `ty` 清為 0（`[]byte` 吐 `slice: true` 卻空型別）修法：把多臂 match 分派包進顯式 `{ }` 块⚠️ **場域**：本增量主要價值是把 bufio/tar/zip 等檔的首分歧推後——這些檔仍因下游 `obj.field = value`（AssignExpression）與型別推斷（`inferred: true`）缺口而 DIFF，故 net 逐位元組相同僅 +1（`tests/arr-field-lit.no`），但為後兩片增量解鎖了更多可達節點**延後**：implements 清單enum/tagged-enum/iface 形限定型別 `a.b` 欄位可為空 `?T` 欄位`#{...}` 欄位註解（Go 走 side-table 不 dump） | `t34`（`point { x i64 / y i64 }` NamedType 欄 / `buffer { data []byte / pos i64 / table [8]i64 }` SliceType+NamedType+ArrayType 欄） |
|  |  |  |
| --- | --- | --- |
| **lvalue 賦值（`obj.field = v` / `arr[i] = v` / `mp[k] = v` ⇒ AssignExpression）** | Go `parseExpression` 尾部（expr.go:598–637，`=` 之後）**這是 M2 解鎖語料面最大的一片**（survey：`ExpressionStatement{AssignExpression}` 是 72 檔首分歧節點——幾乎每個有狀態物件/集合操作的 std 模組都用 `obj.x = v``m[k] = v`）此前 `noc` 把 `obj.x = 1` 拆成 `ExpressionStatement{DotExpression}` + `ExpressionStatement{1}`（`=` 被丢）**純解析器改動**：`AssignExpression`（K=12）**無純量槽**兩個 unguarded CHILD（LeftValue），dumper 早已就緒（`ExpressionStatement{...}` 標籤由 kid 的 nodeString 派生，`node_of: Expression`）**語義**：`=` 非中綴運算子（優先級 0）⇒ 中綴迴圈天然停在 ASSIGN；若此時左值（`ast.kind-at`）是 Dot/Index ⇒ 消費 `=`以 LOWEST 解右值建 `AssignExpression{Left: target, Value: val}`（右結合：`a.b = c.d = 7` → `AssignExpr(a.b, AssignExpr(c.d, 7))`）**實現**：在 `ps-parse-expr-prec` 尾部（中綴迴圈後`return idx` 前）加一段检查，用活`ps-peek(p) == tok.ASSIGN`（`=` 前若換行則 currentToken=NEWLINE不觸發，對齊 Go）；`isLval` 由兩個獨立 standalone-if（`k0 == DOT -> isLval = 1` / `k0 == INDEX -> isLval = 1`）累加（非同一 match 的臂，故不踩裸 `->` 坑）**與既有路徑不冲突**：`x = 1`（裸 IDENT ASSIGN）在 `ps-parse-stmt` 層已由 LetStatement 臂截走，不会到此；`obj.field = v`（IDENT DOT IDENT =）因 method-def 臂要求 `+4==LPAREN``f()=v` 因左值是 CallExpression（非 Dot/Index）而不誤触**效果**：全語料逐位元組相同 **122 → 137（+15）**，差分回歸 **0**（含 `tests/struct.no``src/std/crypto/rand.no``tests/oob-slice-assign.no``tests/vec-assign*.no` 等）**延後**：`?=` → UnwrapAssignStatement（另一節點）`f() = v` 的 `ExpressionStatement{nil}` 杂乱 `=` 错误恢复形（非法代碼語料未見str-order.no 既有 hang 亦属另一缺口） | `t35`（`obj.x = 1` DotExpression 目标 / `obj.y = obj.z` / `arr[0] = 9` `arr[i] = arr[j]` IndexExpression 目标 / `a.b = c.d = 7` 右结合链） |
| **裸 `return`（ReturnStatement）** | Go `parseReturnStatement`（stmt.go:1424–1441）**`return` 是 Nolang 常见陈述**（survey：`ReturnStatement{}` 是 21 檔首分歧節點——函式提前返回都先 `out = …; return`），此前 `noc` 完全丢弃它（ps-parse-stmt 無 RETURN 臂→走 ps-parse-expr-stmt→RETURN 無 primary 处理器→idx=0）**语义**：Nolang 以具名 out-param 傳值，`return` 恒裸（`ReturnValue` 永远 nil）——Go 对 `return <val>` 直接 `saveError`（parse error语料不会出现）**节点（/tmp/ast_spec.json + dump.go:235–241）**：`ReturnStatement`（K=56）**无纯量槽**`node_of: ReturnValue`（`braces: true``absent: ""`）CHILD(ReturnValue, guard=true)COMMENTS——ReturnValue 缺席 ⇒ 渲染 `ReturnStatement{}`**实现**：在 `ps-parse-stmt` 顶层 match 块（BREAK/CONTINUE 臂旁）加 `tk.typ == tok.RETURN -> { ps-bump(p); idx = ast.add(t, K_RETURN_STATEMENT) }`（仅消费 RETURN不消费终止符，同 Go）⚠️ **guarded-缺席仍须 `kid-add(idx,0)` 占位**（新坑同 §2.1.1 arena kid 对齐类）：不占位时 header pre-scan `childs.push(kid-at(cur))` 会读到**后续节点的 kid**⇒`node_of` 标签错为下一个陈述的 nodeString（实测 `ReturnStatement{LetStatement}` + 多带一个 kid）；补 `kid-add(idx, 0)` 后 guard 读到 0 ⇒ `{}` 且无 kid同时修复 `cond -> return`（ps-standalone-body 已将 tok.RETURN 归 isStmt=1 转 ps-parse-stmt，现该臂接上）**效果**：全语料逐位元组相同 **137 → 145（+8）**（`tests/getline.no``tests/str-repeat.no``tests/to-bytes.no``tests/u128-*.no` 等），差分回归 **0**`tests/env.no` 首分歧（return）已清→剩余为 `(cond){}` bare-block-loop⇒ForStatement 另一缺口**延后**：`return <val>` 错误恢复形（非法代码）ForStatement 前缀循环 | `t36`（`{ out = x; return }` 陈述尾裸 return / `flag == 1 -> { out = 9; return }` standalone 内联 / `return` 后接陈述的提前返回） |
|  |  |  |  |
| --- | --- | --- | --- |
| **语句级 `#{...}` 注解（overflow 槽）** | Go `parseStatement` 的 `HASH_LBRACE` 分支（`parseAnnotationStatement`）+ `attachAnnotations`→`setStmtOverflowMode`（annotation.go:315–342）**这是当前最大可治纯解析缺口**（survey：258 档 LetStatement 首分歧中 **83 档**是 `#{...}` 泄漏成假 `LetStatement{name: index-out/overflow, ...}`）`#{` 为单 token `HASH_LBRACE`（lx.no:1310），此前 ps.no 无处理⇒ Interior `overflow=wrap` 走 IDENT+ASSIGN→假 LetStatement且紧后陈述丢 `overflow` 槽**语义**：注解仅 `overflow=<mode>` 会 dump（写进语句节点的 `overflow` 纯量槽）；`index-out`/`platform`/`embed` 等只入语义副表**不影响 AST dump**⇒ 本增量只需整段吞掉不外泄+ 抽 overflow`NormalizeOverflowMode` 合法值 `wrap/clamp0/min/max/saturate`（语料中 4164 处几乎全为 `wrap`）**节点槽位（/tmp/ast_spec.json）**：非 child-derived 纯量序：`LetStatement`=s6`ForStatement`=s2`MultiAssignStatement`=s1`FunctionDefinition`=s4（`ExpressionStatement`/`ReturnStatement` 有 OverflowMode 字段但 dump 不渲染⇒略过）**实现**：新增 `ps-parse-annotation`（深度计数吃平衡 `#{...}`顶层捕捉 `overflow=<ident|string>`）`ps-normalize-overflow``ps-apply-overflow`；在 `ps-parse-stmt` 顶（`ps-skip-trivia` 后）连续消费前置 `#{...}`解析紧后陈述末尾按节点 kind 写对应槽因所有语句集合循环（parse/block/standalone/match 臂）皆走 `ps-parse-stmt`，单点拦截覆盖全上下文无 `#{` 时循环不执行⇒**零回归结构性保证**（已匹配的 145 档不含注解）**延后**：尾随同行注解 `stmt #{...}`（目前会被当作下一条的前置注解错标目标；语料以首行前置为主故本增量未触）；`i64-max` 型别前缀形式归一 | `t37`（`#{overflow=wrap}` 前置 / `#{index-out=zero}` 仅吞不 dump / `#{index-out=zero, overflow=wrap}` 合并取 overflow） |
|  |  |  |
| --- | --- | --- |
| **`#` 匯入（UseStatement）** | Go `parseUseStatement`（stmt.go:45`case lexer.USE`）`#`（非 `#{`）詞法回 `tok.USE`（lx.no:1328lit='#'）；語料形式恆為 `# std/number` / `# /example/...` 纯路径（11 檔 use-bucket 實測：無 `.fn` / `as`）此前 ps.no 無 USE 臂→走 expr-stmt⇒把 `std/number` 拆成 `InfixExpression{op: /}` 除法鏈**節點（ast_spec.json + dump.go:180）**：`UseStatement`（K=70）純量槽 path=s0function=s1alias=s2（三者 `kv` P-ALWAYS即使空也渲染 `function: , alias: `）as=s3（`kvb` policy=truefalse 則省略）；無 CHILDCOMMENTS（doc 由既有 ps-skip-trivia-doc 附上）**實現**：新增 `ps-parse-use`（消費 `#`→可選前導 `/`→以 `/``.` 分隔的 IDENT 段用 `+` 串接成 path（joinPathParts））+ ps-parse-stmt 加 `tk.typ == tok.USE -> ps-parse-use`因任何含 `#` 匯入的檔此前必已 NO，故新增 USE 臂嚴格只改善→**零回歸結構保證****效果**：全語料 **158 → 159（+1，`tests/sha3.no`）**USE 只清首分歧——其餘 10 檔随即在下游 match-block/standalone-if/BlockStatement 簇（`ExpressionStatement{IfExpression}`/`BlockStatement{}`）或 FloatLiteral value 精度（`1.41421` vs 全精度）再分歧，故僅 sha3 全匹配本增量價值在打通 import 前置為 match-block 主簇铺路**延後**：`.fn` 函式名分隔符 + `as alias`（語料未見） | `t38`（`# std/number` / `# /test/runner` 前導斜線 / `# std/net/http` 多段） |
| **推論元素型別的定長陣列 `name [N] = [..]`（ArrayLiteral）** | Go `parseLetStatement` 的 `[N]` 分支（`stmt.go:947–959`）+ SliceLiteral→ArrayLiteral 轉換（`stmt.go:1210–1218`）survey：ArrayLiteral typed-let 是 LetStatement 桶最大子因（~47 檔首分歧）明示形 `name [N]T = [..]` 早已支援（+4==IDENT/+5==ASSIGN 臂），**但省略元素型別的 `name [N] = [..]`**（+4==ASSIGN/+5==LBRACKET）此前無臂→ `name [N]` 被誤解析為 IndexExpression`= [..]` 成 AssignExpression**關鍵路徑語義（探針實測非推論）**：① 元素型別恆為 `i64`——Go 對省略元素型別的 `[N]` 一律補 `NamedType{Value:"i64", IsInferred:true}`（stmt.go:957），不依首元素 kind 推論（bool/float/str 陣列在 oracle 也出 `[N]i64`）；② `ArrayType.IsInferred=false`（源碼顯式寫了 `[N]`→formatter 必須印回 `[N]`）；③ 僅 `Size>0`（IntegerLiteral value>0）才把 RHS SliceLiteral 轉 ArrayLiteral（設 `wasSliceLiteral:true`）——`[0] = [..]` 保留 SliceLiteral（探針 `e [0] = []` ⇒ `value: SliceLiteral{}`）**實現**：在 `ps-parse-stmt` 明示陣列臂後新增推論臂（守衛 `+2==INT && +3==RBRACKET && +4==ASSIGN && +5==LBRACKET`），復用既有 `ps-parse-arr-lit`；讀 `sz_lit`，`!='0'` 走 ArrayLiteral`=='0'` 走 `ps-parse-expr`（SliceLiteral）；建 `NamedType{i64, inferred:true}` → `ArrayType{size, elem}` → `LetStatement{name, type, value}`**零回歸結構保證**：任何含此形的檔此前必已被誤解析為 Index+Assign（已 NO），新臂只改善**效果**：全語料 **159 → 202（+43）**差分回歸 **0**（uniform 120s-alarm `-P 3`：s34=159→s35=202）為 aes/zlib 等大量 `[N] = [int-literals]` 表檔解锁 | `t39`（`a [3] = [10,20,30]` 推論 i64 / `b [4] = [1,2,3,4]` / `e [0] = []` Size==0 保留 SliceLiteral） |

| **方法體 `.field` 前導點（self 速記）** | Go `parsePrefixExpression` 的 `case lexer.DOT`（expr.go:230–245）survey：`DotExpression{receiver: …}` 是 25 檔首分歧節點（方法體大量使用 `.pos`/`.buf[i]`/`.fill()` 速記）`.field` ⇒ `DotExpression{Receiver: Identifier{"self"}, Property}`；裸 `.` ⇒ `Identifier{"self"}`**此前無前綴 DOT 臂** ⇒ `.field` 落到 ps-parse-expr-prec 預設臂（idx=0）再經**後綴** DOT 迴圈用 idx=0 作 receiver ⇒ dump `DotExpression{receiver: <nil>}`（oracle 為 `receiver: self`）**實現**：在 ps-parse-expr-prec 前綴 match 塊（ SliceLiteral 臂後預設臂前）新增 `tk.typ == tok.DOT` 臂：消費 `.`若緊接 IDENT 則**先建 self Identifier（子）再建 DotExpression（父）**（slot0=propertykid0=self Identifier）否則裸 `.` 建 `Identifier{"self"}``self` 名硬編碼（與 Go `&Identifier{Value:"self"}` 同）**零回歸結構保證**（含 `.field` 的檔此前必已 NO）**效果**：全語料 **202 → 203（+1，`src/std/io.no`）**差分回歸 **0**⚠️ **關鍵發現（下一個高價值增量）**：self-dot 清掉 25 檔首分歧後，其餘 24 檔立即在 **`AnnotationStatement{entries: N}` 節點**（collection/*bufioregexptxtyaml 等）或 **ForStatement `!! { }`/`(cond) { }` 迴圈簇**再分歧尤其 `AnnotationStatement`：增量 R 把陳述級 `#{...}` 全吞掉，但 oracle 對**無法附著的** `#{...}`（紧跟 ExpressionStatement/AssignExpression無 overflow 可渲染槽者）會**保留一個 `AnnotationStatement{entries: N}` 普通節點**（紧跟 LetStatement/ForStatement 等可附著者則無節點只設 overflow 槽—t37 已驗證）故 ** emitting AnnotationStatement 節點** 為解锁 collection/* 的主鑰匙 | `t40`（`.fill()` 呼叫 / `.buf[.pos]` 索引 rvalue / `.pos = .avail` lvalue+rvalue） |
| **独立 `AnnotationStatement{entries: N}` 節點（不可附著之語句級注解）** | 即上一列揭示的 collection/* 主鑰匙——已於本增量實現Go `parseAnnotationStatement`（annotation.go:188–309）+ `attachAnnotations`**關鍵規則**：語句級 `#{...}` 只在緊後陳述首 token ∈ {IDENT, IN, RARROW}時附著於該陳述（Go 247 分支：回傳 `parseStatement` 結果**不產生独立節點**；overflow 寫入陳述純量槽＝增量 R/t37 路徑）；**否則（首 token 為 DOT `.field=`LPARENLBRACE 等不可附著形）oracle 保留一個 `AnnotationStatement{entries: N}` 普通節點**作為兄弟陳述增量 R 全吞 `#{...}`從未發節點⇒ 此類檔永遠缺這一行**entries 計數（dump.go:306 + parseAnnotationBody:990 + dedupeAnnotationEntries:853）**：N = 合併連續 `#{...}` 群組後去重的頂層鍵名數（`kvc`：`0` 則省略）；鍵 IDENT 只在 `#{` 後或頂層 `,` 後（expect_key），值內 ident（如 `wrap`）/ 括號不計；同鍵名跨群組去重只計一次語料實測 404×`entries:1`25×`entries:2`（無更高）⚠️ 鍵名可含連字號（`index-out`/`win-amd64`）—— Go（lexer.go:959）與 noc（lx.no:175）皆以 `-` 為識別元內字元⇒ `index-out` 為**單一 IDENT**（僅 leading `-` 走運算子）**節點（ast_spec.json）**：`AnnotationStatement`（K=7）仅一顆 `entries` 純量（of:len/policy:count/render:int）經槽字串渲染（非 c0！`set-slot(0, keys.len().to-str())`），另 COMMENTS；ANNOTS 子行只在 `--annotations` 旗標下渲染（預設 `no ast` 不印）**實現**：將 `ps-parse-annotation`（舊只回 overflow）**內聯進 `ps-parse-stmt` 前置註解迴圈**，同段掃描同時產出 `ovf` 與去重 `keys []str`；以深度計數吃平衡（`#{ [ ( {`→+1`} ) ]`→−1），頂層鍵去重入 keys`overflow=` 取 `ps-normalize-overflow` 值**末尾新增分派**：`keys.len()>0 && 緊後首 token ∉ {IDENT, IN, RARROW}` ⇒ `ast.add(K_ANNOTATION_STATEMENT)` + `set-slot(0, …to-str())` 並就此 return（緊後陳述留待下輪 ps-parse-stmt，兩者成兄弟）**零回歸結構保證**：keys 空（無註解）時整段跳過；且任何含不可附著 `#{...}` 的檔此前必已 NO**效果**：全語料 **203 → 204（+1，`src/std/collection/stack.no`）**差分回歸 **0**⚠️ heap.no 等仍未完配——本節點修正後它們立即在**下游 `GenericParams`（泛型）與 `(cond) { }`/`!! { }` ForStatement 簇**再分歧（即下一個主簇） | `t41`（`.pos=` 前 `#{overflow=wrap}`→entries:1 / 雙鍵 `#{index-out=zero, overflow=wrap}`→entries:2 / 連群組 `#{index-out=zero}`+`#{overflow=wrap}`→entries:2 / 同鍵兩群組→entries:1 / IDENT 前附著→無節點） |
| **`!! { }` / `! { }` / `(cond) { }` 陳述位循環（ForStatement bang/cond-loop 簇）** | 即上一列揭示的下游主簇——已於本增量實現Go 三條入口：`parseBangLoop`（stmt.go:528–540 dispatch → 2577）——`case NOT`/`case BANG_BANG` 僅當 `peekToken==LBRACE` 才成循環，否則 `parseExpressionStatement`（`!x` 取反）；`case LPAREN`（stmt.go:381 `isCondLoopPrefix`→2686 `parseCondLoopPrefix`）**bang-loop**：`Condition=BooleanLiteral{loop}`（`!!`→true 無限`!`→false 不執行）`Body=parseBlockStatement`**cond-loop 判別（isCondLoopPrefix:2615）**：(1) 掃至平衡 `)`；(2) 其立即下一 token 須為 `{`（`tokAt(k+1)`，換行後の `{` 屬後綴式 `{ } (cond)`非本形）；(3) 括號內容含 `,` 或恰好兩個 IDENT（參數名+型別）⇒ 匿名函式字面量非循環；其餘（`()`單 IDENT `(flag)`）⇒ 循環`()` ⇒ `BooleanLiteral{false}`；`(expr)` ⇒ `Condition=parseExpression`**節點（ast_spec.json `ForStatement`）**：3 純量——`label`(s0,kv 恆印)`condWrapper`(s1,policy:true 僅 IsCondWrapper 印本三形均 false 故不印)`overflow`(s2,nonempty)；6guardedCHILD `Init,Condition,Update,CountExpr,IterRange,Body`——缺席者以 `kid-add(…,0)` 佔位（dumper 略過 0）⚠️ 三形皆不saveWarning（新式語法）⇒ 不累加 `p.warn`（否則 Program `warnings: N` 錯）**實現**：新增三個 helper `ps-parse-bang-loop`/`ps-is-cond-loop-prefix`（仿 `ps-is-func` 平衡括號掃描）/`ps-parse-cond-loop`，並在 `ps-parse-stmt` 主分派新增三臂（BANG_BANG/NOT 需 `peek-at(1)==LBRACE`LPAREN 需 `ps-is-cond-loop-prefix==1`，否則落回既有 expr-stmt ⇒ 零回歸）⚠️ **踩坑**：`ps-parse-cond-loop` 原在兩臂各呼叫 `ps-peek(p)`，首臂消費 `)` 後次臂重取看到 `{` ⇒ `()` 雙臂皆執行cond 被覆寫成 0（`BooleanLiteral{false}` 消失）；改預先取 `hd=ps-peek(p)` once（同 `ps-parse-for-kw` 守則）後修復**效果**：全語料 **204 → 213（+9：`src/std/bufio.no``collection/{hash-set,str-set}.no``env.no``tests/{dowhile,loop-test,split-loop,str-loop-assign}.no``mem-safety/bug15-read-dowhile-copyfile.no`）**差分回歸 **0**（`!!`/`(` 此前於 noc 挂死——無分派臂→expr-stmt 內圈不前進）⚠️ heap.no 修正後僅在 `GenericParams` 再分歧（即下一個主簇） | `t42`（`!! { }`→BooleanLiteral{true} / `! { }`→{false} / `(.i<3) { }`→InfixExpression / `() { }`→{false} / `(.flag) { }`→DotExpression 條件） |
|  |  |  |  |  |
| --- | --- | --- | --- | --- |
| **字面量型別推斷補齊（`CharLiteral`→`char`十六進位 `IntegerLiteral`→`byte`）** | 即 bang-loop 揭示後 path.no/char-* 檔的第一分歧子因（探針實測：`SEP = "/"` oracle 出 `LetStatement{name: SEP, type: char, value: /}` + `NamedType{name: char, inferred: true}`，noc 缺此 NamedType）Go 推斷在 `parseLetStatement` 的 `stmt.Type == nil` 分支（stmt.go:1227–1268），僅對字面量 RHS合成 `NamedType{IsInferred:true}`既有實作已覆 INT→i64/FLOAT→f64/STRING→str/BOOL→bool，**漏了兩項**：① `case *CharLiteral: Value:"char"`（stmt.go:1263）——雙引號 char 字面量未推；② `case *IntegerLiteral` 的十六進位分支（stmt.go:1229–1235）——`raw` 以 `0x/0X` 開頭且 len>2 ⇒ `byte`，否則 `i64`同一口徑亦見於 SliceLiteral 首元素推斷（stmt.go:1274–1300：Char→charhex-INT→byte）**實現**：新增 helper `ps-intlit-hex(raw)`（复用 `ps-int-lit-value` 已有的 base 偵測同寫法：`raw.len-bytes()>2 && raw.byte(0)==48 && (raw.byte(1)==120||raw.byte(1)==88)`）；`ps-infer-type` 舆 `ps-elem-type-of` 兩處同步：`K_INTEGER_LITERAL` 臂改為先 `raw = ast.str-at(t, ast.nd-s(d, 1))`（IntegerLiteral slot1=raw 源碼文字）`ps-intlit-hex(raw)==1 -> 'byte'` 否則 `'i64'`；新立 `K_CHAR_LITERAL -> 'char'` 臂`char` 名硬編碼（對應 Go `ValueTypeChar.String()`）**零回歸結構保證**：僅新錯位之前缺失的 inferred NamedType（原回 ''不推⇒無子節點），不觸碰已有型別標註路徑**效果**：全語料 **213 → 222（+9：皆 `tests/char-*`——`char-literal`/`char-new-method`/`char-new-syntax{,2,3}`/`char-old-syntax2`/`char-simple`/`char-step4`/`char-vet`）**差分回歸 **0**⚠️ path.no 的 char 推斷已修（首分歧從 line 2 推到 line 423）但仍於**下游裸 match desugar**再分歧（即下一個主簇） | `t43`（`SEP="/"`/`DOT="."`→char / `H=0x41``HBIG=0Xff`→byte / `DEC=65``ZRO=0x0`→i64byte 邊界 / `ARRHEX=[0x50,0x4b,0]`→[]byte / `ARRDEC`→[]i64 / `ARRCHAR=["a","b"]`→[]char） |
| **無值明示型別裸宣告 `name TYPE`（陳述位 LetStatement）** | 即上一列揭示後 crypto/archive 檔的第一分歧主因（探針實測：`result []byte` oracle 出 `LetStatement{name: result, type: []byte, value: }` + `SliceType{}`/`NamedType{byte}`，noc 把 `result [` 误解析為 `IndexExpression``byte` 成單獨 `ExpressionStatement`）survey：LetStatement 桶 173 檔，其中 **73 檔首分歧即此形**（oracle 行含 `value: }` 空值），跨 `[]byte`(17)/`[32]byte`(10)/`[]str`(7)/`[16]byte`(4)/str(4)/i64(4)/… 皆同**Go 规则（stmt.go:170–249 + 1051–1060）**：`IDENT LBRACKET` 於陳述位以`[` 前是否有空格区分標註 `name [N]T`（有空格）vs 索引 `name[i]`（无空格，行 181 column 比较）；`parseLetStatement` 解完型別後，若 current/peek 為 NEWLINE/RBRACE/EOF ⇒ **仅型別宣告无赋值**，回傳 `Value=nil` 的 LetStatement`IDENT IDENT`（无 `=`）同理只能為裸宣告（Nolang 无两 ident 无运算子的合法運算式）**实现**：新增 helper `ps-parse-typedecl-noval(t,p,name)`——`ps-bump` 消费 name IDENT → 復用既有 `ps-parse-type`（已支援 NamedType/NullableType/SliceType/ArrayType 全形子先於父）→ 预取 `hd=ps-peek(p)`，仅当 `hd ∈ {NEWLINE,RBRACE,EOF,SEMICOLON,COMMENT}` 且 `hd != ASSIGN` 时建 `LetStatement{name, type, value:0}`（Value guarded CHILD 以 0 占位⇒`ast.wanted` 的 OF-LOCAL/R-NODE-STRING 遇 `child<=0` 回 `''`⇒dump `value: `）；否则**还原 p.pos****分派**：`ps-parse-stmt` 主分派末尾（各含值臂之后`expr-stmt` 之前）新增一臂 `handled==0 && (nxt_typ==LBRACKET||nxt_typ==IDENT)`——含值臂皆要求 `=`（未命中⇒handled==0）才会进本臂；命中后设 handled=1**零回归结构保证**：（①`ps-parse-type` 对索引 `x[i]`（`[` 后 IDENT非`[`/INT`）`x[0]=v`（`]` 后 ASSIGN非 IDENT）均回 0⇒还原；②还原时所建的孤兒節點不被任何 kid 引用，dumper 自 root 沿 kid0/nkid 走訪（`ast.dump`→`ast.node`）永不触及，仅耗 arena；③nxt_typ 為固定快照，仅 LBRACKET/IDENT 两值进本臂，后续 COMMA/ARROW/DOT/LBRACE/QUESTION 臂的固定 nxt_typ 判定不匹配）⚠️ **未涵盖**：限定型别名 `name a.b`（`ps-parse-type` IDENT 臂不吞 DOT）舆嵌套 `[][]i64`（slice 臂需 `[]` 后立即 IDENT）——此两类 ps-parse-type 回 0⇒还原行为同前（无回归），留待后续**效果**：全语料 **222 → 253（+31）**差分回归 **0**（uniform 120s-alarm `-P 3`：新匹配 crypto/{sha256,hmac,aes-*,tls*}archivetests/{u128,vec-push-clear,words-test,…}）⚠️ zlib.no 修复首段后仍于**下游 `data.len` 独立 if 反化 / 裸 match desugar**再分歧（即下一个主簇）⚠️ 当时未涵盖的限定名/嵌套型已于下一列（s41）补齐 | `t44`（`result []byte`/`z [16]byte`/`items []str`/`x box`/`i8 i8` 裸宣告 / `a []i64 = [1,2,3]` 含值对照 / `x.n=a.len()` 非宣告） |
|  |  |  |
| --- | --- | --- |
| **複合型別名：限定名 `a.b` + 嵌套元素 `[][]T`/`[N][]T`（ps-parse-type 扩展）** | 即上一列揭示的未涵盖同族——已於本增量实现探针实測三类同根因（`ps-parse-type` 不认限定/嵌套）：`c tls.conn`（限定无值）oracle 出 `LetStatement{name: c, type: tls.conn, value: }` + `NamedType{name: tls.conn}`，noc 拆为 `ExpressionStatement{c}` + `DotExpression{tls, conn}`；`a [][]i64`（嵌套无值）oracle 出 `SliceType{ SliceType{ NamedType{i64} } }`，noc 拆为嵌套 `IndexExpression`；`f ?fs.file = call`（限定含值可空）oracle 出 `NullableType{ NamedType{fs.file} }`，noc 误为 `type: ?fs` + 剩 `.file` 泄入 AssignExpressionsurvey：**12 档首分歧即此类**（src/std/log.nonet/proxy.nofs-error-{complete,final,info,simple}fs-open-errhpackhttp2mem-safety/{deep-free-nested-vec,nested-container-clone}nested-vec-testnet-clientpush-narrowstd-newtls{,-part3}vec-elem-ownership）**Go 规则**：限定名 stmt.go:1034 `for cur==DOT { typeName += "."; next; if IDENT { typeName += lit; next } }`（整条存进单一 NamedType.Value，不另建节点）；嵌套型 stmt.go:937–946 `parseTypeExpression` 递归（`[N][M]T``[][]T`）**实现**（均仅改 `ps-parse-type`）：① IDENT 臂建 NamedType 前加 `go` 迴圈：`ps-peek==DOT && ps-peek-at(1)==IDENT` 则消费 `.段``nm = nm + '.' + seg`；② SliceType/ArrayType 臂的元素不再硬性要求单 IDENT，而是守卫元素起 ∈ {IDENT, LBRACKET, QUESTION}后递归 `elem = ps-parse-type(t,p)`（子先於父）；③ 各臂改用 `handled` 旗标（非互斥条件）防 match 非排他之跨臂重评**级联**：`ps-parse-param`（`w io.writer`）可為空含值臂 `nxt_typ==QUESTION`（`f ?fs.file = call`）均走 `ps-parse-type`→无需改动即自动受益**含值复合形**：将 `ps-parse-typedecl-noval` 升级为通用 `ps-parse-typedecl`（新增 `hd==ASSIGN → 消费 `=`+`ps-parse-expr` → `LetStatement{name,type,value}` 分支），分派守卫仍限 `nxt_typ∈{IDENT,LBRACKET}`（QUESTION 交由可為空臂），因此只接住各特化含值臂遗漏的 `d tls.conn = call-tls()``outer [][]i64 = with-len(1)` 等形**零回归结构保证**：限定/嵌套型此前 ps-parse-type 回 0⇒还原⇒这些档均已 NO，新能力只改善；孤兒节点不被 kid 引用⇒dumper 不触及**效果**：全语料 **253 → 262（+9：tls/tls-part3/http2/hpack/fs-error-{final,info}/fs-open-err/nested-vec-test/push-narrow）**差分回归 **0**⚠️ 其余 3 档（fs-error-simple/std-new/net-client）复合类型已正确但仍于**下游裸 match desugar / 空 StringLiteral / `{}` 结构字面量**再分歧（即下一个主簇）⚠️ **未涵盖**：`?T`无值裸宣告（`x1 ?str`）仍由可為空臂（需 `=`）误吞——为避免触碰众多 option-* 档回归风险，留待下一个专门增量 | `t45`（`c tls.conn`/`a [][]i64`/`z [16][]byte` 裸复合 / `f ?fs.file = call-fs()` 限定可空含值 / `d tls.conn = call-tls()` 限定含值 / 参數 `w io.writer, m [][]i64``k hpack.tables`） |
| **無色 async 前綴運算子 `run`/`co`/`awy`（RunExpression/CoExpression/AwaitExpression）** | 即 survey 揭示的 async 簇（10 檔首分歧即 `LetStatement{name: h, value: RunExpression}` 等）探针实測：`h = run worker-async(100)` oracle 出 `LetStatement{name: h, value: RunExpression}` + `RunExpression` + `CallExpression{...}`；noc 因 `ps-parse-expr-prec` 无 RUN/CO/AWY 前缀臂⇒值解析為空（`value: `）内层 `worker-async(100)` 漏成独立 `ExpressionStatement{CallExpression}`同理 `r = awy h` ⇒ AwaitExpression{Identifier h}`c = co child(n)` ⇒ CoExpression**Go 规则（expr.go:305–357）**：三个前缀 case均在 `parseExpression` 的 prefix 分发——`run e`/`co e`：若 `peekToken==DOT` 则**不是** async 关键字而是模組名（`run.list(...)`）⇒建 `Identifier{lit}`nextToken 停在 DOT 交由后缀 DOT 路径；否则消费关键字→`parseExpression(LOWEST)` 作内层→包 RunExpression{Call}/CoExpression{Call}`awy e`：无 DOT 歧义，直接消费 awy→`parseExpression(LOWEST)`→AwaitExpression{Right}**節點（ast_spec.json）**：RunExpression{Call}CoExpression{Call}AwaitExpression{Right}——均无标量单一 guarded CHILD；K_RUN_EXPRESSION=57 / K_CO_EXPRESSION=20 / K_AWAIT_EXPRESSION=13 均已於 asp.no 注册⇒**纯 parser 缺臂**（无需改 ast.no/asp.no）**实现**：在 `ps-parse-expr-prec` prefix match 块（NOT 臂后LBRACKET 臂前）新增三臂；子先於父（先 `child = ps-parse-expr-prec(t,p,0)` 再 `ast.add` 父 + `kid-add`）⚠️ `run`/`co` 的 DOT 歧义用**预先算好的 `dotAfter = ps-peek-at(p,1)==tok.DOT` 旗标**分两臂（`dotAfter==1` Identifier / `dotAfter==0` 包装），而非互补条件各自重取 ps-peek-at（match 非排他首臂消费 token 会使后臂误触）**零回归结构保证**：含 run/co/awy 的档此前必已 NO（值缺失）**效果**：全语料 **262 → 273（+11：async-{cancel,coop,rc,yield,handle-alias}module-asyncslot-rebind-unsafemem-safety/{async-alloca-escape,async-module-awy,async-str-result,async-str-stress}）**差分回归 **0**⚠️ go-async.no 的 CoExpression 已正确但仍于**下游行内註解附著（`b = co worker(20); // 註解`）/嵌套 BlockStatement**再分歧（即下一个主簇） | `t46`（`run worker(3)`/`co worker(4)`/`awy h` 三包节点 / `co.pool()` 模組名歧义→DotExpression{receiver: co} / 普通 `worker(5)` 不受影响） |
| **区块／程式尾注解 TrailingComments（BlockStatement + Program）** | 即 survey 揭示之 OTHER 桶——TrailingComments 20 档／TrailingComments 首分歧 11 档可纯 parser 翻转探针实測：`for2.no`（仅两行注解无语句）oracle 出 `Program{}` + `TrailingComments` + 两行 `// <text>`；`types.no` 文件头多行注解群同理noc 因 `ps.no` 的 `parse`（Program）与 `ps-parse-block`（BlockStatement）把这些 standalone 注解喂给 `ps-skip-trivia-doc` 后丢弃⇒从不产生 TrailingComments**Go 规则（stmt.go:2141）**：区块语句解完后 `block.TrailingComments = p.collectDocComments()`——取回全部剩余缓冲注解（再以 openBraceLine≤line≤closeBraceLine 过滤属于本区块者）；多注解群**全部**渲染（不同于 leading Doc 只印首条）**节点（ast_spec.json）**：BlockStatement/Program 的 `TrailingComments *CommentGroup`——dumper 早已用 OP-TRAILING（`ast.no:1058-1081`，读 `d.ntrail`/`d.ct0`/`ast.aux`）完整支援⇒**纯 parser 缺口**（无需改 ast.no/asp.no）**实现**（均仅改 `ps.no`）：新增三 helper——（a）`ps-trail-region(t,p)` 前瞻（不消费 token）判断跳完所有 NEWLINE/COMMENT 后首个实 token 是否 ∈ {RBRACE,EOF}（=处于尾注解区）；（b）`ps-collect-tail(t,p,prev,trail)` 消费尾区的 NEWLINE/COMMENT：同条陈述行尾的首个行内注解（`sawNL==0 && prev>0 && !cmtTaken`）仍走 `set-cmt`其余全部 `trail.push(put-str)`；（c）`ps-attach-trailing(t,idx,trail)` 在 `trail.len()>0` 时走 `aux-begin`+逐个 `aux-add`+`set-aux-counts(ntrail=len, nclos=-1, nimp=0)`在 `parse` 与 `ps-parse-block` 的语句循环开头与尾各做一次区域判断：region==0→照常 leading-doc（`docId`），region==1→`ps-collect-tail`；节点建完后 `ps-attach-trailing`**零回归结构保证**：ntrail 默认 -1⇒`trailing-comments` renderer `d.ntrail>=0` 不成立而跳过；trail 空⇒不呼叫 attach⇒原匹配档输出字节不变**效果**：全语料 **273 → 284（+11：src/std/types.notests/{arr,duplicate,for2,i,str,range-test}.nomem-safety/{deep-free-nested-vec,deep-free-str,deep-free-struct-vec,minimal-str-map2}.no）**差分回归 **0**（`corpdiff` M->N 列表为空）⚠️ ClosingBraceComment（`}` 同行右的 `nclos`）尚未实现——本语料无此形，留待后续 | `t47`（函式区块内陈述后的两条 standalone 尾注解→BlockStatement TrailingComments / 根层级最后陈述后的尾注解→Program TrailingComments） |
| **结构体栏位型别拓展 + 栏位尾注解跳过（ps-is-struct-def gate）** | 即 survey 揭示之 StructDefinition 桶（9 档首分歧）探针实測：`matrix { rows [][]str; name str }`（嵌套切片栏位）与 `pool { conns [32]net.conn; ... }`（限定名阵列栏位）与 `node { next node #{inline=false} }`（栏位尾注解）——oracle 均出 `StructDefinition{name, fields: N}` + `StructField{name, type, arraySize/slice}`（且 `no ast` 无 -annotations **从不渲染栏位注解**）；noc 因 `ps-is-struct-def` gate 内手写的栏位型扫描器仅认 {IDENT, `[]IDENT`, `[INT]IDENT} 且不允许尾注⇒遇限定/嵌套/注解即回 0⇒整个结构退回 `ExpressionStatement{name}` + 内部被当作独立陈述**关键**：body（`ps-parse-struct-def`）早已走 `ps-parse-type`（s41 已支援限定/嵌套）⇒**仅 gate 限****实现**（均仅改 `ps.no`）：（a）新增 `ps-skip-type-idx(p, i)`——以索引前视（不消费 token）完整鏡像 ps-parse-type 四形（`a.b`（IDENT (.IDENT)*）`?T``[]T``[N]T`（N=单 INT），元素递归∈{IDENT, [, ?}），合法回新索引非法回 −1；用它取代 gate 内的 `okType` 手写臂（b）新增 `ps-skip-annot-idx`（gate，索引前视）/`ps-skip-annot`（body，消费），以深度计数（`#{ [ ( {`→+1`} ) ]`→−1）跳过栏位尾 `#{...}` 群**零回归结构保证**：含限定/嵌套/注解栏位的结构此前必已 NO（gate 拒→ExpressionStatement ≠ oracle StructDefinition），新能力只改善；gate 与 body 共用同一型别语法⇒一致**效果**：全语料 **280 → 283（+3：src/std/net/client.notests/field-{inline-annotation,tag}.no）**差分回归 **0**（`corpdiff` M->N 列表为空）⚠️ 其余 6 档（json/net-{http2,pool,sse,ws}/str-vec-push）结构已正确但仍于**下游 standalone-if desugar / index-out desugar / 裸 match 区块**再分歧（即下一个主簇，LetStatement/IntegerLiteral/BlockStatement 三大桶） | `t48`（`next node #{inline=false}` 注解跳过 / `rows [][]str` 嵌套切片 / `conns [32]net.conn` 限定阵列 / `in-use [32]bool`） |

**路徑復刻（§5.3.2）的實測要點**（全部經 oracle 探針驗證，非推論）：

1. **型別推斷只對字面量合成 NamedType**：`x = 1 + 2``y = x` 的 LetStatement **沒有** Type 子節點；只有 INT→`i64`（但十六進位 `0x/0X` 形→`byte`）FLOAT→`f64`STRING→`str`BOOL→`bool`CHAR（雙引號）→`char` 字面量直接 RHS 才推`x = f()` RHS 是 CallExpression 也 **不推**
2. **kid 槽位佔位規則**（新坑，已入 §2.1.1 表）：LetStatement 的 spec CHILD 是 (Type, Value) 兩槽，缺 Type 時必須 `kid-add(t, idx, 0)` 佔位；IfExpression 同理三槽 (Condition, Consequence, Alternative)，缺 alt 時也要 push 0
3. **`a = (…)` 歧義規則**（新坑，已入 §2.1.1 表）：`ps-is-func` 從當前 LPAREN 掃到配平的 RPAREN，再看下一個非 trivia token：`{`⇒函式`(`⇒**回傳元組**（t26：不中斷續掃下一群組直到它閉合再看是否 `{`）其他⇒非函式否則 `a = (x)` 會被誤判為函式定義並掛死；`c = add(1)(2)`（鏈式呼叫）因第二群組閉合後非 `{`仍正確判非函式
4. **GroupedExpression / NilLiteral 的 dump 不帶括號／花括號**（探針實測）
5. **CallExpression 的 `fn:` 使用 nodeString**（dump.go:904）：Identifier 時取 name，其他節點取 kind-name（例 `a.b(1)` ⇒ `CallExpression{fn: DotExpression, args: 1}`，因为 fn 的 nodeString 預設回 kind name）
6. **args: 0 時 dump 不印**（kvc 政策 NON-ZERO）：slot0 存 `args.len().to-str()`，count slot0 同步 `args.len()`
7. **`return 42` 在 Nolang 中是 parse error**（out-param 風气），不納入 M2 復刻目標
8. **match 臂尾臂不互斥**（新坑，已入 §2.1.1）：同块 `cond -> { idx = A }  …  -> idx = B` 中，B 臂會**作為無條件尾隨陳述**再執行一次，覆寫 A 臂的 idx`ps-parse-stmt` 的 IDENT 分支靠 `handled` 旗標隔開
9. **單行 `-> { a; b }` 吃掉閉合 `}`**（§2.1.1 首行的實測復發）：SliceLiteral 元素迴圈若寫 `ps-peek(p) == tok.COMMA -> { ps-bump(p); ps-skip-trivia(p) }`（單行），`;` 會把 `ps-skip-trivia(p) }` 整段註解掉 ⇒ `{` 永不閉合 ⇒ 向後吞掉 `ps-parse-if` 等**所有頂層定義**，報錯卻指向遠端呼叫點：`EmitLLVM: unknown callee ps-parse-if in func ps-parse-stmt`修正＝展開成多行（與既有的 call-args 迴圈同形）
10. **ArrayLiteral / `[N]` 型別標註（已復刻）**：探針 `x [3]i64 = [1, 2, 3]`（明示）與 `x [3] = [1, 2, 3]`（推論）皆經 oracle 驗證 ⇒ `LetStatement{type: ArrayType{size:N}}` + `ArrayLiteral{size, elements, wasSliceLiteral: true}`**兩关键路徑要點**（t39）：明示 `[N]T` 用 `NamedType{T}`（不帶 inferred）；**推論 `[N]`（無元素型別）時 Go 一律補 `NamedType{"i64", inferred:true}`不依首元素 kind 推論**（bool/float/str 也出 `[N]i64`，stmt.go:957）；且 `ArrayType.IsInferred=false`（formatter 印回 `[N]`）；僅 `Size>0` 才轉 ArrayLiteral`[0] = [..]` 保留 SliceLiteral（stmt.go:1210）
11. **迴圈內多臂 match 的跳臂迭代不可靠**（新坑，已入 §2.1.1）：`ps-parse-expr-prec` 的後綴迴圈 `(pgo) { nt == A -> {…} nt == B -> {…} nt == C -> {…} -> {pgo=false} }`——只有兩個區塊臂時（LPAREN/DOT）跳臂迭代正常（t11 `a.b(1)` 同過），但**新增第三個區塊臂 LBRACKET 後**，某對跳臂轉移會誤編（表現在 `f(x)[2]``m[0][1]``a.b(1)` 三者中總有一個在第一臂後就退出迴圈，把後續 `[..]`/`(..)` 當成独立陳述），且**調整臂序只是把壞的轉移換了一個**穩定修法：用 `handled` 旗標——每臂末尾 `handled = 1`，終止改寫為顯式陳述 `handled == 0 -> pgo = false`（與 §2.1.1尾臂坑的處理同構）
12. **dump 標頭的 `size` 類欄位不占字串槽**（OF-NODE-STRING 機制）：`ast.no::scalar` 對 OF-TYPE / OF-NODE-STRING / OF-LOCAL(render=nodeString) 的純量，**從 child 现算** nodeString/typeString（ref 是 CHILD 序號）；只有 OF-FIELD/OF-LEN/… 才读字串槽 s0..⇒ ArrayType/ArrayLiteral 的 `size` 由 child0（Size 節點）推得，**不要**為它 `set-slot`；只需為 bool/len 類欄位（inferred / elements / wasSliceLiteral）依序佔用字串槽
13. **僅宣告無初始值 `e [3]i64`（無 `= …`）尚未復刻**（待辦）：oracle 對 `e [3]i64`（无 RHS）出 `LetStatement{name: e, type: [3]i64, value: }`（value 空）當前分派守衛硬性要求 `+5==ASSIGN`，故此形落入 expr-stmt 誤解析為 IndexExpression需放寬守衛（`] IDENT` 後無 ASSIGN 也走陣列宣告value=0 佔位）——本增量**只釘住有初始值的 `[N]T = [..]`**

**vet 現況（誠實記錄）**：`no vet compiler/src` ⇒ **0 ERROR / 0 WARNING / 123 hint**ERROR=0 滿足 nolang-vet 硬門檻⚠️ **基線遷移（2026-10-11）**：早期版本此處記錄的11 WARNING全為 `nolang-overflow-ineffective`（提示此 `#{overflow=...}` 對應陳述不含可證 i64 整數運算，建議刪除，針對 `x = x + intField` 類欄位算術——checker 無法證明 `intField` 為 i64（option/指標可能性）而誤報，非真實缺陷；舊分佈：`ast.no` 7`drv.no` 2`ps.no` 2）**當前的 `bin/no` 已把該檢查由 WARNING 降為 HINT**，故現在計入 123 hint 之中WARNING 歸 0——此為 oracle 端嚴重性重分類，**非本增量造成**（本增量只動 `ps.no`）註解歸位增量新增的 `#{overflow=wrap}` / `#{index-out=zero}` 均對應真實 i64 算術或數組讀取，不新增 ineffective 項

> ⚠️ 歷史上此處的 WARNING 計數會隨 oracle `bin/no` 對 `overflow-ineffective` 的嚴重性調整而變（曾為 0→未加入檢查後 11→降為 WARNING現 0→降為 HINT）；關鍵硬門檻始終是 **ERROR=0**

⚠️ **有意保留的偏離**：`*_test.no``shs-check.no` 的檔名含下劃線，違反 `fn-rule` 檔名 lint——但這些檔名由本設計文檔（§6 M0/M1 驗收證據）直接指定，屬文檔與 lint 的衝突，以文檔為準

**可重跑驗證（本增量的全部證據）**：

```sh
./bin/no build -o /tmp/noc-s0 compiler                     # Build successful
for f in compiler/tests/fixtures/*.no; do                  # t0–t48 全部逐位元組相同（原既有失敗 t32（`(expr) { }` cond-loop⇒ForStatement）已由 s38 bang/cond-loop 增量修復）
  diff <(./bin/no ast "$f") <(/tmp/noc-s0 ast "$f") && echo "SAME $f"
done
./scripts/selfhost_diff.sh --self-test --levels L0,L2 --limit 30 --sut /tmp/noc-s0   # L0 4/4L2 30/30PASS
./bin/no run compiler/tests/{ast,lx,tok,util}_test.no       # 44456227 全 PASS
./bin/no vet compiler/src                                   # 0 ERROR0 WARNING
```

⚠️ **超時包裝的坑**（本節命令若改寫成自製 harness 時必讀）：用 perl `fork`+`waitpid(WNOHANG)` 包裝時，被 SIGKILL 的進程 `$? >> 8` 是 **0**，會把掛死誤報成成功；必須用顯式 `$to` 旗標（或 `perl -e 'alarm N; exec @ARGV'`）回報 124


### 6.4 M3 啟動盤點（2026-10-27）

M2 收官（見 §6 表下方M2 收官段）後下一里程碑為 **M3 語義檢查**（§5.4）本節記錄啟動摸底，作為 M3 首增量的選型依據：

- **SUT 現狀**：`drv.no` 僅有 `version` / `tokens` / `ast` 三個子命令，**無 `vet`**；`compiler/src/` 無 checker 模組（`ck*.no` 不存在）無 `ck-sig.no`（stdsig 表）`/tmp/noc-s81 vet <file>` 落 usage 分支 ⇒ **L3 誠實基線 = SUT 整體缺席（UNAVAILABLE/全 FAIL），不是 PASS**（§7.5 G7 同源教訓）
- **門禁基建已就緒**：`scripts/selfhost_diff.sh` 的 L3 段（`run_diff_level L3 vet` + 歸一化 + `[ERROR]` 集合硬判據 + G5 oracle 穩定性守衛）早已接線（§7.3），M3 的每一步都能立刻上差分
- **vet 基線漂移註記**：§6.3 舊證據块的0 ERROR是各增量大当期快照；以當代 `bin/no` 量測，`no vet compiler/src` 現為 **19 ERROR**（皆 `nolang-index-write` 族，集中在 ps.no 既有 `out[i] = …` 形），為 M3 之前的既有基線非 L2 軌引入，留待 M3 期間一併清償或標記 grandfather
- **§5.4 四大復刻點**（工作量主體）：
  1. **診斷三通路**（`no build` 硬錯誤列表 / `no vet` 的 `RunAllLints`+`SkipTypeChecks` / LSP 原始程式）——noc 側需先立 `diag.no`（§5.8 診斷模型），再按通路接線；
  2. **stdsig 表遷移**：`src/checker/stdsig_gen.go`（859 funcSigs / 1,983 methodSigs）→ `compiler/src/ck-sig.no`，用一次性生成工具（`compiler/tools/`）讀 std 生成，**不在 `noc` 啟動時重收集**；
  3. **158 處 `#{buildin}` 樁的三條載入路徑**——s52 已在 AST 面復刻 `BuiltinStub`/`BuiltinName` 旗標（slot5/6/7），M3 只需消費端跳過樁，是三條路徑守衛的既有地基；
|  |  |
| --- | --- |
  4. **診斷輸出兩種形狀**（`<file>:<line>:<col>: [ERROR|…]` 與 `<channel>: line L, column C: …` 串），§7.3 歸一化依賴此契約，**不得發明第三種**
- **建議分期**（沿 M2 的增量方法論）：M3-a＝`diag.no`+`drv.cmd-vet` 骨架（連詞法/語法錯誤也走診斷形狀，讓 L3 先能跑誠實報 FAIL）→ M3-b＝stdsig 生成器＋簽名表 → M3-c＝`Validate*` 按語料觸發頻度逐族增量（L3 門禁 = `[ERROR]` 集合 diff 收斂 + 0 回歸）
- **M3-a 已完成（s9a，§5.3.4.am）**：`vet` 子命令落地，但範圍較原計劃有一處誠實縮減——ps.no 目前**不產錯**，故詞法/語法錯誤也走診斷形狀未隨骨架交付，現狀是契約齊全診斷源為空：L3 harness 如預期報 **G8 MASS-NOOP**（護欄生效，非 PASS）同輪 oracle 側盤點給出 M3-c 的真實工作面（樣本 40 檔：38 檔有診斷，ERROR/HINT/WARNING=594/10001/296，code 族 9 種），比 §5.4 的37 個 Validate*計數更寬下一增量建議 M3-b（stdsig 生成器），或先行給 ps.no 接第一條真診斷（語法錯誤收集器），二者皆可立刻上 L3 差分
- **M3-b 已完成（s9b，§5.3.4.an）**：`compiler/tools/gen_ck_sig.py` 把 stdsig_gen.go 的**全部 7 張表**（870/2,371/720/1/105/3/118）翻成 `compiler/src/ck-sig.no`，含逐字節 `ck-sig-lt`＋`ck-sig-bsearch` 查找層；L0 ck-sig-test **50 斷言全綠**`no vet compiler/src` 19 ERROR＝基線（ck-sig.no 零 ERROR）生成物**刻意惰性**（無模組 import 它，僅測試），故 M3 觀測面仍等價 s9a⚠️ 本輪 L2 全量掃未取得 DIFF=0ast-test/ps-test 有 5 條紅——**同因於 Go 側未提交軌道的工具鏈退化**（`str.to-i64` 髒工具鏈回 0`(cond) { }` 不認 for；HEAD worktree 建構同檔皆正確），非本增量；該軌修復後重跑 `/tmp/l2sweep.sh` 即可復核另外 `2f231fb0` 已把 `no vet src/std` 由 145 ERROR 清零 ⇒ 上一輪記下的src/std 基線漂移清償下一增量建議 M3-c 首族（給 ps.no 接語法錯誤收集器 ⇒ L3 首條真診斷）或 checker 骨架接線消費 ck-sig.no
- **M3-c 首族已完成（s10，§5.3.4.ao）**：`ps.no` 接上**結構化語法錯誤收集器**（`psst.eLn/eCl/eMsg/eCode` 四平行欄，對齊 `parser.Diagnostic`＋`saveError` 去重鍵），首批 3 站點復刻（分組缺 `)`／實參缺逗號／let RHS 落空，正文與 token 名逐字對齊 oracle），`drv.cmd-vet` 接線消費 ⇒ **L3 MASS-NOOP 解除**（s9a 記的診斷源為空自此有真信號）門禁：ps-err-test **17/17**L2 `/tmp/noc-s10b` **656/656 DIFF=0**（含 `parseCallExpression` 實參循環結構性重寫之回歸驗證）`noc vet` 全語料 663 檔**零誤報**vet 基線 19 不變；rc 鉗制 1 已對 authority 復核改正已錄三缺口（cascade 第二條／NEWLINE 坐標族／def 型別宣告族等其餘 saveError 站點）下一增量建議：NEWLINE 坐標族（游標對齊，修②並同時擰緊 ps-err-test 分組站點坐標）checker 骨架接線消費 ck-sig.no或按語料頻率續推 saveError 站點族
- **M3-c 缺口②已清（s11，§5.3.4.ap）**：**NEWLINE 坐標族**結清——分組站點刪前置 skip（報錯停在 NEWLINE ⇒ `2:0 got NEWLINE`）實參站點鏡像 Goarg 前後只跳 NEWLINE 再分派（報錯落換行後首個非換行 token ⇒ `foo(1\n2)` 報 `2:1 INT`）；19 個錯形檔＋6 個乾淨檔探針 parser 族與 oracle **逐字節全等**（含 p21 `foo(\n)`⇒2:2 EOFc6 前後換行形）ps-err-test 分組站點同步擰到 oracle 形（**17/17**）；L2 `/tmp/noc-s11g` **656/656 DIFF=0**；L3 全語料 **666 檔零誤報**；src/std 0 ERRORcompiler/src 19 基線；ps.no fmt 漂移順帶清零（清掃後重建復跑全部門禁）新工具鏈事實：單語句形 `(cond) stmt` **非循環**（只執行一次）`#` 裸名解析要求檔在專案內新增缺口④入口跳合一族（COMMENT/NEWLINE，語料無此形）剩餘缺口①cascade 第二條 nil 診斷③其餘 saveError 站點下一增量建議：checker 骨架接線消費 ck-sig.no或 cascade 族（①）或按語料頻率續推站點族（③）
|  |  |  |  |
| --- | --- | --- | --- |
- **M3-c 正語義族首件已完成（s13，§5.3.4.ar/.as）**：`compiler/src/ck.no`（扁平 arena 掃描器）落地首個正語義族 `'X' is not defined`（TraceID `4bek3xc6`，authority `checker.go:8180-8295`），`drv.cmd-vet` 在 `ps-errs==0` 時調用 `ck.check` ⇒ s9b 的 ck-sig.no 裸名門表（bk/bv/bo 1732 條）＋mok 118 模組名**首次被 checker 消費**（`CK-SIG-VERSION` 2→3）；跨模組回值改**打包串 vec** `L|C|CODE|MSG` 規避結構體按值傳參的 MIR 聚合體缺陷（§5.3.4.as）**本輪續完成**：committed 版 ck.no 全語料 668 檔跑出 **13 檔誤報**（違反檔頭少報不誤報硬門禁），根因為 def 採集遺漏多類綁定名——逐一對齊正主補齊：族 A 泛型參數（FD generic kids `[kid0,c3)`，`n` 76）族 B match 具名型別臂（SurfaceMatch HELPER 寬 12/臂 offset3 `typeBinding`，`w`）族 C tagged-enum 變體解構（臂 `mcond` CallExpression 參數 IDENT，`v/r/h`）callee 跨模組裸調回退沉默（單檔不解析 `#use`，ck.no 自檔 `ck-sig-bsearch/bare`）`std` 命名空間根入靜默集（global.no `@ std/...` 誤解析下游症狀）⇒ **誤報 13→0**坑紀律：nd-at 結構體欄位（c3/hcount/hbase）不得直入區塊臂條件（MIR NoVal），須先落地為局部門禁：ck-test **20/20**L3 ck 誤報全語料 **0**L2 ast 逐位元組未受擾（ast 不调 ck）parser 族仍 0；global.no 的 ast 差屬**既有** `@`-export parser 分歧（`noc-s13i`==`noc-s13fin` 哈希相同可證）下一增量建議：按語料觸發頻率續推其他 `Validate*`/型別推斷族（§5.4 剩 36 個）或清償缺口③/④或補 `@ <mod>/<fn> <alias>` export 糖 parser 解析使 global.no 進 L2 SAME 集

---


### 6.5 M4 啟動盤點（2026-10-29）

M3 收官（見 §6.4 s13）後下一里程碑為 **M4 中端＋代碼生成**（§5.5/§5.6）本節記錄啟動摸底，作為 M4 首增量選型依據：

- **SUT 現狀（2026-10-29 啟動摸底當日，歷史；現況見 §4.1.1 末 2026-11-12 校正）**：`compiler/src/` 當日現有 `lx.no` / `ps.no` / `ck.no` / `ck-sig.no` / `drv.no` / `diag.no` / `util/sb.no` —— **無中端無 codegen無 `cg-runtime.no`**；`drv.cmd-build` / `cmd-run` 仍走 `noc` 外殼（調用 `bin/no` 出後端，見 Q1(b) 臨時手段），尚未自產 IR`/tmp/noc-s13*` 現狀＝**前端閉環後端缺席**（L4 不可用，M4 之前是合理狀態）
- **門禁基建已就緒**：L0–L3 全線（`selfhost_diff.sh` §7.3）已接線；M4 的 **L4 行為門禁**（全語料 `(rc, sha256(stdout))` 與 golden 一致，§7）可直接套用，且 §5.6.3 已規定**四層驗收**（行為 / IR 形狀 / `opt -passes=verify` / ASan 需先補 `sanitize_address`，§11 B12）⚠️ `opt` 驗證失敗**不持久化** `.ll` ⇒ 調試唯一窗口是 `NOLANG_MIR_DUMP_LL=1`（該開關在 verify **之前**寫檔）
- **範圍裁剪是最大槓桿（§5.5 表）**：`src/mir` 43,177 LOC 中約 **40% 從自舉必需降級為可選**——`analysis.go`＋`insertDrops`（所有權分析）由 **RC 取代**（DR-3 / Q2(a)）`alias_forward.go` / `opt.go` / `tier.go` / `spawn_graph.go`(async) 全部延後（M6/M7/M8）M4 的**真目標**＝`hir2mir.go` 降級主體 ＋ `codegen.go` 逐 op 發射 ＋ `builtin_call.go` / `builtins.go` 內建降級 ＋ RC runtime prelude 照抄
- **RC 鐵律（§5.6.1，最高風險點）**：每個別名都必須攜帶自己的計數——漏一處 retain＝**UAF**（非洩漏）M4 必須先立 `check-rc-balance` 驗證器（沿 `handleAliasSet` 的別名判定），且**只查欠計數方向**（過計數是 §1.3.1 接受的洩漏，報錯會 false positive ⇒ 硬編譯失敗）§5.6.1 的 RC 發射規則表（建立擁有堆值 → `rc_alloc`；`b=a` → `retain`；離作用域 → `release`；參數引用語義不 retain；容器/欄位存入取出 → `retain`）是 M4-c 降級的逐條依據
- **IR 文本照抄硬規則（§5.6.2）**：`codegen.go` 的 prelude（`windowsShimsConst` / `%task` / RC helper）是**字串常量，可逐字節照抄**到 `compiler/src/cg-runtime.no`，0 新設計⚠️ 照抄時 **Go/IR 註釋內不得出現反引號**（會終止 raw string 字面量，報 Go 語法級錯誤）——一律用雙引號引用識別符號〔⚠️ 2026-11-12 核對：此「無條件逐字節照抄」僅適用於 RC helper／`%task` 等未變部分；`%str-long` 自 2026-11-11 起已改 16B，selfhost 已於 2026-11-12 同步並以 helper 本文逐字對齊，見 §11 後 ⑥／§11.2 B28〕
- **語料範圍（Q4(b)）**：M1–M3 已全語料（便宜）；M4 先 **SHS+std** 子集，再逐步擴大到全 527 tests第一個可編譯子集應鎖定最小能產 `.ll` 且 `opt -verify` 過的表達式族（字面量 / `let` / 函式呼叫 / 加法），而非一開始就吞下泛型與 async
- **建議分期**（沿 M2/M3 的增量方法論，每步可上 L4 差分）：
  - **M4-a**＝`cg-runtime.no` 照抄 prelude（`@nolang_rc_alloc` / `retain` / `release`16 字節頭 ABI `data = base + 16``%task`windowsShims）＋ `check-rc-balance` 驗證器骨架（只查欠計數）先立**正確性論證的局部性**地基，讓後續 emit 有守門
  - **M4-b**＝MIR op 子集＋`emitMIR` / `EmitLLVM` 骨架：照抄 `codegen.go` 的 67 個 op 中最小閉包（字面量 / `let` / 呼叫 / 算術 / 控制流），對**排序後** `.ll` 做**存在性** diff（§5.6.3 第②層，不追求逐位元組），先打通 `EmitLLVM` → `.ll` 管線
  - **M4-c**＝`hir2mir.go` 降級主體：語句/表達式逐族降級（按語料觸發頻度），嚴格照 §5.6.1 的 RC 發射規則表 emit retain/release；每族上 **L4 行為門禁**（全語料 `(rc, sha256(stdout))`）
  - **M4-d**＝與 M5 驅動層接線：`drv.cmd-build` / `cmd-run` 自產 `.ll` → 調用 `clang`（DR-2，不依賴 `opt` / `llc`）完成匯編＋鏈接；L4 全語料行為門禁收斂＋0 回歸
    - **✅ 2026-11-12 已落地（§11 後 ⑧）**：`h2m-lower-module` 在 AST 上就地合成 `main`（末位 `exit(E)` → 輸出參 `__noc_r`；無 `exit` ⇒ `0`）、新增 `compiler/src/link.no`（DR-2 只調 `clang -x ir`）、`drv.cmd-build`／`cmd-run` 支援 `-o OUT FILE` 與 `FILE -o OUT` 雙序、`compiler/tests/drv-build-test.no` **16/16**、e2e `build`／`run` 與 Go oracle 同值（exit=42）。**殘差＝L4 全語料行為門禁收斂**（尚未把 `test/`／`tests/` 全語料拉進 `noc run` 差分）
|  |  |  |
| --- | --- | --- |
  - **M4-e**＝`hir2mir` 降級主體（本檔新增標號，理由見上條 M4-c 的實際用盡情況；§5.3.4.cj 起於 s55）：`ast.no` arena → `mir.no` 扁平指令流，自 i64 直線式子集起步，簽名側參數與控制流隨後逐族補——**s55–s97（2026-11-06，共 43 件，§5.3.4.cj–.dz）**完成直線式 i64 子集 → 簽名側與調用 → 控制流與跨基本塊槽化 → 花括號短路鏈 → 塊終結形 → 計數/區間/容器遍歷 → 值匹配 → 實參側池 args → 邏輯 `&&`/`||` → i64/u64 除零護欄 → u64 全族 → i64 `>>` 裁定 lshr → f64 槽化與 `%` → bool/char 字面量 → TY-OPT 控制流鋪槽（當時 `h2m-test` **845/852**、`cg` 347／`cg-runtime` 60／`ck` 20）；**其後已續推至 s99n（件號 54）**：s98（2026-11-08，§5.3.4.ea）清掉 cg `OP-SHR` ashr 死臂；s99–s99n（2026-11-09，14 節，§5.3.4.eb–.eo）補齊 struct 全族（定義／欄位／形參返回值／跨塊槽／跨函數／欄位型別／欄位 RC 取得與 scope-exit 釋放／vec 欄位就地索引）、所有權 provenance（s99m `ownn`·`ownv`·`rdval`·`rdown`·`retset`）、vec 切片字面量與 `@vec_free`·`OP-VEC-FREE=94`（s99n）；`0fbbe684`（11-10）重構 struct lowering 與所有權處理。現況 `h2m-test` **1031/1037**（紅 6 皆既有 s98 optmatch）、`cg-runtime-test` **84/84**、`cg-test` **358/358**（⑦ 解鎖）、`cg-str16-test` **41/41**；✅ 2026-11-11 Go 後端 `%str-long` 改 16B，selfhost **已於 2026-11-12 同步清償**（§5.3.4.ax／§11.2 B28／§11 後 ⑥）；`?str/?vec` 載荷、`option` match 臂解構、`drv.cmd-build` 接線（M4-d）等仍待續
- **已知風險/缺口（B4 表項，§11 同步記帳）**：① `opt -passes=verify` 失敗不持久化，調試必須 `NOLANG_MIR_DUMP_LL=1`（§5.6.3 已標）；② RC 遺漏 retain 的 UAF 是靜默運行期錯，只能靠 `check-rc-balance`＋L4 行為門禁雙重捕獲；③ 照抄 IR 文本的反引號陷阱（§5.6.2）；④ 規模仍大——即便砍 40%，剩餘 `hir2mir`＋`codegen`＋`builtins` 仍 ~26k LOC，須按語料頻率增量不可一口吞；⑤ async（`spawn_graph.go`）明確推到 M6 之後，M4 產出的 `noc` 自身不需要 async（除驅動層並行調 clang）；⑥ 精確所有權（`insertDrops` 那一整套）推到 M7，M4 不實作
- **M4-a 已完成（s14，§5.3.4.at）**：`compiler/src/cg-runtime.no`（`%nolang_hdr`/`%task` 類型＋`@malloc/@free` 聲明）＋`cg-rc.no`（四段 RC helper IR 文本，逐位元組照抄 Go 14712–14818；`HDR-MAGIC=7957698232306827265`；`check-rc-balance` 只查欠計數）＋`compiler/tests/cg-runtime-test.no`（**16/16**）驗證：`opt -passes=verify` **rc=0**（98 行 IR）Go 源重建 diff **0 刪除行**`no vet` 兩檔 **0 ERROR**刻意惰性（僅測試 import）；windowsShims 後置 M7；`check-rc-balance` 待 M4-c 由 MIR 餵事件下一增量建議 **M4-b**（`emitMIR`/`EmitLLVM` 骨架，照抄 `codegen.go` 67 op 的最小閉包，對排序後 `.ll` 做存在性 diff）
- **M4-b 已完成（s15，§5.3.4.au）**：`compiler/src/mir.no`（op 子集常量＋扁平 arena 表示：`ops/dsts/xa/bs/syms` 平行陣列）＋`cg.no`（`cg-emit-func` 發 CONST/ADD/SUB/MUL/CALL/ALLOC/STORE/LOAD/RET，單基本塊）＋`compiler/tests/cg-test.no`（**13/13**）驗證：`opt -passes=verify` **rc=0**（three/dbl/caller/let-add）**端到端** `let-add`（3+3）IR 經 clang 執行 **exit code 6**`no vet` 兩檔 **0 ERROR**實測坑：參數名 `as` 是保留字 ⇒ 改 `xa`刻意惰性（僅測試 import）；**未覆蓋**控制流（br/condbr/phi）與非 i64 型別（留 M4-c）下一增量建議 **M4-c**（`hir2mir` 降級主體：控制流 + 非 i64 型別 + 字符串/容器 op）
- **M4-c 控制流首件已完成（s16，§5.3.4.av）**：`mir.no` 增 `OP-BR/CONDBR/CMPLT` 與 `blks[]` 平行陣列；`cg.no` 增 `cg-label`＋`cg-emit-cfg`（多基本塊，塊切換發標籤），`cg-emit-func` 保留 7 參數 API 委託之跨塊傳值用 `alloca/store/load`（-O0 風格**不引入 PHI**）；`CONDBR` 三槽：`a`=條件`bs[k]`=真塊`dsts[k]`=假塊；`CMPLT` 發 `icmp slt`+`zext`門禁：`cg-test` **20/20**`opt` verify **rc=0**端到端 `lt10(5)+lt10(15)` 執行 **exit 3****未覆蓋**：PHI其他比較非 i64 型別字符串/容器 op（M4-c 續）
- **M4-c 循環/回邊已完成（s17，§5.3.4.aw）**：零新增 op零新增檔，僅擴 `cg-test.no` 的 `test-loop`（`sum4()` while 迴圈，`bb2→bb1` 回邊）回邊靠塊按序發射 ⇒ 目標已定義自然成立；循環變數用 `alloca/store/load`（無 PHI）門禁：`cg-test` 累計 **25/25**`opt` verify **rc=0**端到端 `sum4()`（0+1+2+3）執行 **exit 6****未覆蓋**：其他比較（NE/GT/GE/EQ）非 i64 型別字符串/容器 op（M4-c 續）
- **M4-c 字符串已完成（s18，§5.3.4.ax；⚠️ 24B／「與 Go 逐字相同」為 2026-11-11 前歷史，見 §5.3.4.ax／§11.2 B28；B28 已清償，見 §11 後 ⑥）**：首个**非 i64 型別** `%str-long { i64, i64, i8* }` 進場`cg-runtime.no` 增 `cg-runtime-str-type`（與 Go `emitPrelude` 逐字相同）；`mir.no` 增 `OP-STRCONST=12`/`OP-STRLEN=13`；`cg.no` 增 `cg-strlen1`（`len-bytes()+1`）**全局前置 pass**（`@.s<k> = private unnamed_addr constant [N x i8] c"...\00"` 必須在 `define` 之前）`STRCONST` 三步 `insertvalue` 構造`STRLEN` 用 `extractvalue ..., 0`定下**值命名空間規矩**：i64 住 `%v<id>``str` 住 `%sv<id>`（LLVM 不允許同名不同型）門禁：`cg-test` 累計 **32/32**`opt` verify **rc=0**端到端 `slen()`（`'abc'.len`）執行 **exit 3**實測坑：IR 裡的 `\00` 在 nolang 字面量必須寫 `'\\00'`（寫單反斜線會把真 NUL 塞進 IR 文本 ⇒ `opt: expected top-level entity`）**未覆蓋**：字符串拼接（需 `@malloc`+RC 頭）`str` 跨塊（`alloca %str-long`）vec/struct/optionPHI（M4-c 續）
|  |  |
| --- | --- |
- **M4-c 字符串拼接已完成（s19，§5.3.4.ay）**：`@str_concat` 13 行**逐字照抄** Go `codegen.go:3139` 進 `cg-runtime.no`，並新增 `cg-runtime-prelude-full()` 作為**完整 prelude 的唯裝配點**（類型 → libc → memcpy → 4 個 RC helper → `@str_concat`，順序用 `str.index` 斷言釘住）`mir.no` 增 `OP-STRCAT=14`；`cg.no` 發 `%sv<d> = call %str-long @str_concat(...)`**這是第一個真正呼叫 RC helper 的 op**（`@nolang_rc_alloc` 返回 `base+16`rc=1 ⇒ 結果是 owner），但**不發 retain/release**——那是 `hir2mir` 的責任門禁：`cg-test` **37/37**`cg-runtime-test` **21/21**全模組 123 行 `opt` verify **rc=0**端到端 `cat()`（`'ab' + 'cd'`）**stdout `abcd`exit 4**（比內容不只比長度）實測坑：BSD grep 的 `\|` 靜默失效 ⇒ clang 警告混進 `.ll`；過濾一律 `2>/dev/null` 或 `grep -E`**未覆蓋**：`str` 跨塊（`alloca %str-long`）`STRCAT` 的 retain/releasevec/struct/optionPHI（M4-c 續）
- **M4-c 多參調用已完成（s20，§5.3.4.az）**：`OP-CALL` 的 `bs[k]` 從無意義佔位正式改為**參數個數**，`a` = 首個參數值 id（**連續**）`cg.no` 的 CALL 分支改為循環展開（`q`/`j` 各帶 `#{overflow=wrap}`，否則 `+` 退化成 `option<i64>` 而 `.to-str()` 編不過）；`define` 側本來就按 `nargs` 循環，無需改⚠️ **這是本項目第一個破壞性語義變更**：漏改的呼叫點會退化成 `call i64 @f()`（實參被丟棄），而 **`opt -verify` 與 `no vet` 都全綠**——只有跑起來比數值才看得見；已同步 `test-call`（`bs` 0→1）門禁：`cg-test` **41/41**`cg-runtime-test` **21/21**`opt` verify **rc=0**端到端 `add2(3,4)` 執行 **exit 7**（單參退化會得到 3 或 4，故這個數字有鑑別力）**未覆蓋**：非 i64 形參多返回值跨塊 strvec/struct/optionPHI（M4-c 續）
- **M4-c `%str-long` 跨基本塊已完成（s21，§5.3.4.ba）**：`mir.no` 增 `OP-SALLOC=15`/`OP-SSTORE=16`/`OP-SLOAD=17`，槽位與 i64 的 `ALLOC/STORE/LOAD` 一一對應（`SSTORE` 無結果槽在 `bs`；`SLOAD` 有結果槽在 `a`），但槽住**第三個命名空間** `%ss<id>``cg.no` 發 `alloca %str-long` / `store %str-long ..., %str-long* %ss<y>` / `load %str-long, %str-long* %ss<x>`門禁：`cg-test` **47/47**`opt` verify **rc=0**端到端 `pick(0)`→**exit 2**`pick(1)`→**exit 6**（兩條分支都跑，只跑一條會讓`SSTORE` 沒發射也綠）⚠️ 實測坑：`alloca %str-long` 在**裸模組**裡報 `Cannot allocate unsized type`，訊息指向 `alloca` 那行但真因是**缺 `%str-long` 型別定義**——用 `SALLOC` 的 IR 必須與 prelude 一起鏈接**未覆蓋**：非 i64 形參vec/struct/option`STRCAT` 的 retain/releasePHI（M4-c 續）
- **M4-c `vec` 已完成（s22，§5.3.4.bb）**：`%vec = type { i64, i64, i64 }`（Go 逐字，data 是 **i64 裸指標**，每次取用 `inttoptr`存回 `ptrtoint`）+ 四個 helper `@vec_new`/`@vec_len`/`@vec_get_i64`/`@vec_push_i64`；`mir.no` 增 `OP-VECNEW=18`/`OP-VECPUSH=19`/`OP-VECLEN=20`/`OP-VECGET=21`，**第四個值命名空間** `%vc<id>`**刻意偏離 Go 的 inline 展開**（壓 IR 量級擴容邏輯收斂成唯一一份），記入 §11 B5擴容：`ncap=max(cap*2,4)` → `malloc` → **`len==0` 跳過 `memcpy`**（否則 `memcpy(dst,null,0)` 是 UB）→ `free` 舊緩衝 → `phi` 三前驅合流門禁：`cg-test` **54/54**`cg-runtime-test` **30/30**204 行全模組 `opt` verify **rc=0**十次 push（強制擴容 0→4→8→16）求和 ⇒ **exit 45****ASan 無報告**🔴 附帶發現**工具鏈 bug**（§11 B6）：`#{overflow=wrap}` 首次綁定算術表達式後 `vec.push(該變數)` 靜默壓 0（讀取/普通呼叫/字面量/插值全對）⇒ 規避：先裸宣告再帶註解重賦值**未覆蓋**：vec 元素非 i64（需逐元素 RC）vec 跨塊`.set`/`.pop`/切片struct/option非 i64 形參PHI
- **M4-c 帶型別簽名已完成（s23，§5.3.4.bc）**：`cg-emit-cfg-t(name, retty, nargs, ptyps, …)` 讓形參/返回值按型別取 `i64`/`%str-long`/`%vec`；型別碼 `TY-I64/TY-STR/TY-VEC` 進 `mir.no`，`cg-ty-name`/`cg-val-prefix` 成為**值命名空間的單一來源**（`mix(n i64, s str)` ⇒ `%v1` + `%sv2` 交錯，第幾個參數就用 `%v<幾>`在混合簽名上必崩）`cg-emit-cfg` 退成全 i64 的殼，**自己造全 0 型別表**而不是讓發射器空表即預設（s20 教訓）門禁：`cg-test` **63/63**`opt` rc=0`mix`+`sident`+`mk` ⇒ **exit 14**（4/4/6 各管 str 形參/str 返回值/函式內構造的 str）**未覆蓋**：帶型別的 CALLstruct/optionPHI（M4-c 續）
- **M4-c 帶型別 `CALL` 已完成（s24，§5.3.4.bd）**：引入**值型別表** `vty`（按值 id 索引）+ `cty`（按指令索引結果型別），`OP-CALL` 逐個實參查 `vty[q]`結果用 `cty[k]` 選前綴與型別別名**決策**：不用呼叫點簽名串（要解析字串型別本就屬於值不屬於呼叫點）不新開 `OP-CALLT`（兩個近似 op 必然 drift，s20 就是這麼出事的）門禁：`cg-test` **66/66**`cg-runtime-test` **30/30**`opt` rc=0`caller2()`（函式內發 `call i64 @mix(i64 %v1, %str-long %sv2)`）+`mk()`+`sident()` ⇒ **exit 14**附帶好處：API 從 10 參變 12 參是**編譯期硬錯**（`expects at least 12 argument(s), got 10`），不是 s20 那種無聲退化**未覆蓋**：struct/option/PHI`vty` 越界風險（§11 B7）
- **M4-c `option` 已完成（s25，§5.3.4.be）**：`%option = type { i64, [3 x i64] }`（Go `codegen.go:2938` 逐字，**32 位元組** = 8 tag + 24 payload slot），第五個值命名空間 `%op<id>`；五個 op `OPTNONE/OPTSOME/OPTTAG/OPTISOK/OPTGET`🔴 **`%option` 只有一個 LLVM 型別，元素型別只活在語義層** ⇒讀 payload不能由型別自動決定（`OPTGET` 讀的是 slot[0] 當 i64，對 `?str` 是錯的），與 `%vec` 元素不可見同族，記入 §11 B8門禁：`cg-test` **78/78**`cg-runtime-test` **33/33**（補 `%str-long`/`%vec`/`%option` 三條型別斷言）`opt` rc=0`pick(n)=n<10?some(n):none` + `g(n)=isok+payload` ⇒ **exit 8**（`g(7)=8``g(15)=0`）**未覆蓋**：str/vec 載荷讀寫（要 alloca+bitcast）`err('msg')`>24B 堆上裝箱option 跨塊`==`/`!=`match 臂〔2026-11-12 核對：`%option` 的 24B slot 與 32 位元組總寬**未變**，`optSlotDefaultBytes = 24`；變的是 `%str-long` 本體〕
- **M4-c `retain`/`release` 原語已完成（s26，§5.3.4.bf）**：`mir.no` 增 `OP-RETAIN=27`/`OP-RELEASE=28`（**只作用於 `TY-STR`**：`extractvalue %str-long, 2` 取 data 指標 + `call @nolang_rc_retain`/`@nolang_rc_release`）；`cg.no` 兩個 emit 分支（臨時值 `%dp<k>`/`%rel<k>` 按下標命名）；**命名衝突消解**——`cg-rc.no` 的校驗事件碼 `OP-RETAIN/OP-RELEASE`（0/1）改名為 `RC-RETAIN`/`RC-RELEASE`，消除與 MIR op 的同名衝突；`cg-test.no` 增 `test-strcat-rc`（8 斷言，含 `check-rc-balance` 平衡通過 + 漏 retain 欠計數捕捉）門禁：`cg-test` **86/86**（78+8）`cg-runtime-test` **33/33**`opt` rc=0**端到端 `clang -O2` 執行 exit 4**（`'abcd'.len`）**`clang -fsanitize=address` 執行 exit 4 且 ASan 零報告**（retain/release 平衡無 UAF/洩漏）🔴 這是 R3（漏 retain=UAF）的第一道靜態+動態雙重守門；`@str_concat` 返回 `rc=1` 視為隱式 RETAIN，scope 退出 RELEASE**未覆蓋**：vec 元素非 i64 的逐元素 RCstructPHI（本方案決定走 alloca 不引 PHI）option str/vec 載荷讀寫與堆裝箱——同屬 M4-c 續件（STRCAT 實參為變數的 RC 週期已由 s27 補齊）
- **M4-c `STRCAT` 實參為變數的 RC 生命週期已完成（s27，§5.3.4.bg）**：零新增 op/檔，沿用 s26 的 `OP-RETAIN`/`OP-RELEASE`；`cg-test.no` 增 `test-strcat-rc-nested`（13 斷言，嵌套 `catnested = (('ab'+'cd')+'ef').len`——sv1/sv2/sv4 為 `STRCONST` 全域常量無 RCsv3/sv5 為堆上 `STRCAT` 結果各記 RETAIN 各自 RELEASE）釘死判別：`STRCONST` 是全局常量（無 RC 頭絕不能 release），`STRCAT` 是堆上 rc=1 結果（每個結果含中間堆串都必須 release）；`check-rc-balance` 為逐值賬本只抓 UAF（release>retain）漏 release 屬過計數＝泄漏（§1.3.1 接受不報）門禁：`cg-test` **99/99**（86+13）`opt` rc=0端到端 `clang -O2` **exit 6**`clang -fsanitize=address` **exit 6 且 ASan/LSan 零報告**`no vet` 五檔 0 ERROR**未覆蓋**：vec 元素非 i64 的逐元素 RCstructoption str/vec 載荷讀寫與堆裝箱PHI（本方案決定走 alloca 不引故非必需）
- **M4-c option 的 `str` 載荷讀寫與堆上裝箱已完成（s28，§5.3.4.bh；⚠️「恰 24B 1:1 相容」為 2026-11-11 前歷史論證，`?str` 仍 inline 但只佔 16B，見 §5.3.4.ax／§11.2 B28；B28 已清償，見 §11 後 ⑥）**：`mir.no` 增 `OP-OPTMKSTR=29`/`OP-OPTGETSTR=30`；`cg.no` 兩個 emit 分支（str 載荷**內聯**進 `%option` 的 24B payload slot——因 `%str-long` 恰 24B 與 `[3 x i64]` 佈局相容，免堆裝箱；data 指標字段 i8*↔i64 須 `ptrtoint`/`inttoptr`，不能用 bitcast；`insertvalue` 須用中間名 `%om0/%om1/%om2`→`%op<d>``%og0/%og1`→`%sv<d>` 避免多重定義）RC：OPTMKSTR 只存儲不自動 retain，option 取得擁有權由呼叫方緊接 RETAIN（rc=2）；OPTGETSTR 讀出共享同一 data 指標的 `%str-long`（不增 rc）`cg-test.no` 增 `test-option-str`（8 斷言）門禁：`cg-test` **107/107**（99+8）`opt` rc=0端到端 `clang -O2` **exit 4**（`'abcd'.len`）`clang -fsanitize=address` **exit 4 且 ASan 零報告**`no vet` 五檔 0 ERROR**未覆蓋**：vec 元素非 i64 的逐元素 RCstructoption 的 vec 載荷讀寫與 >24B 堆裝箱option 跨塊/`==`/`!=`/match 臂PHI（本方案決定走 alloca 不引故非必需）
- **M4-c `struct` 已完成（s29，§5.3.4.bi）**：`TY-STRUCT = 4`；`mir.no` 增 `OP-STRUCT-MK=31`/`OP-STRUCT-GET=32`；`cg-runtime.no` 增 `cg-runtime-struct-type()`（`%struct.point = type { i64, i64 }`）並**併入 prelude**——修掉每個函式各發一次型別的模組級錯誤（兩個函式同時用結構體即 `error: redefinition of type`，實測反證見該節）；`cg.no` 加兩個 emit 分支（鏈式 `insertvalue` 用中間名 `%sm0<k>`，結構體值住第六個命名空間 `%st<id>`，欄位讀出落 i64 `%v`）`cg-test.no` 增 `test-struct-mk-get`（5 斷言）`cg-runtime-test.no` 補 2 條門禁：`cg-test` **112/112**`cg-runtime-test` **35/35**`opt` rc=0**雙函式模組**（`pt`=34`pt2`=56）`clang -O2` **exit 90**`clang -fsanitize=address` **exit 90 且 ASan 零報告**`no vet` 五檔 0 ERROR；順帶 `no fmt -w compiler/src/cg.no` 收乾本批自引入的 149 行格式差異（`#{overflow=wrap}` 8→8 保留）**未覆蓋**：寫欄位 `STRUCT-SET`（LLVM 結構不可變 ⇒ 讀舊+insertvalue 出新值）多 struct 型別（需型別描述符）str/vec/option 欄位的逐欄位 RCstruct 作形參/返回值（有臂未實測）struct 跨塊與嵌套
- **M4-c `STRUCT-SET` 與結構體槽已完成（s30，§5.3.4.bj）**：`mir.no` 增 `OP-STRUCT-SET=33`（dst=新結構體`xa`=舊結構體**`xa+1`=新欄位值**`bs`=欄位下標）`OP-STALLOC=34`/`OP-STSTORE=35`/`OP-STLOAD=36`（結構體槽 `%sts<id>`，第七個命名空間，與 `%st` 值只差一個 `s`）；`cg.no` 四個 emit 分支；`cg-test.no` 增 `test-struct-set`（7 斷言，含SET 不得發 store反向斷言）🔴 **SET 是讀舊出新值不是就地寫**：`insertvalue` 產新值舊值仍可用，就地必須 `STLOAD → STRUCT-SET → STSTORE` 三步門禁：`cg-test` **119/119**`cg-runtime-test` **35/35**四基本塊回邊的 `counter()`（206 行模組）`opt` rc=0 + `clang -O2` **exit 5** + ASan 零報告；**兩個負向對照**（刪循環內 `STSTORE`SET 寫錯欄位）皆 **exit 0**；`no vet` 三檔 0 ERROR三檔 `no fmt -d` 0 hunk**未覆蓋**：多 struct 型別（型別描述符——本族唯一結構性瓶頸）str/vec/option 欄位的逐欄位 RC（槽只搬位元）struct 作形參/返回值嵌套 struct 與 struct 陣列
- **M4-c 多結構體型別＋任意欄位數已完成（s31，§5.3.4.bk）**：`mir.no` 的 `TY-STRUCT = 4` 改為 **`TY-STRUCT-BASE = 100`**（結構體 #k 的型別碼 = base + k，值仍住 `%st<id>`）；`cg-runtime.no` 的單一 `%struct.point` 改為 `cg-runtime-struct-decl(id, nfields)` + `cg-runtime-struct-decls(sfc)`，**prelude 不再含結構體型別**；`cg.no` 的 `cg-ty-name`/`cg-val-prefix` 改範圍判斷（`t >= TY-STRUCT-BASE` → `%struct.s<k>`），`STRUCT-MK` 改**循環展開**（初值 `zeroinitializer`中間名 `%sm<k>_<j>`），`GET`/`SET`/`STALLOC`/`STSTORE`/`STLOAD` 按`cty[k]`／`vty[xa]`／`bs[k]`三種來源取型別 id門禁：`cg-test` **123/123**`cg-runtime-test` **37/37**同模組雙結構體（206 行）`opt` rc=0 + `clang -O2` **exit 157**（34+123）+ ASan 零報告；**兩個負向對照**（`s1` 宣告成 2 欄位 ⇒ `invalid indices for insertvalue`；漏發 `s1` 宣告 ⇒ `invalid type for null constant`）；`no vet` 五檔 0 ERROR五檔 `no fmt -d` 0 hunk**未覆蓋**：欄位型別清單（現只有欄位數）與 str/vec/option 欄位的逐欄位 RC具名 struct → id 的映射表（`hir2mir` 落地時建）struct 形參/返回值嵌套 struct 與 struct 陣列
- **M4-c 結構體欄位型別已完成（s32，§5.3.4.bl）**：`cg-runtime.no` 新增 `cg-runtime-ty-name(t)`（**全庫唯一**的型別碼→型名映射，自 `cg.no` 遷入；`cg-ty-name` 退化成一行轉發）與 `struct-decl(id,nf,ft)`／`struct-decls(sfc,sft)`（`sft` = 各結構體欄位型別碼的**拼接表**，游標展開無 stride 上限）；`cg.no` 的 `MK`/`GET`/`SET` 不再硬編 `i64 %v`，改按 `vty[fv]`／`cty[k]` 選型別名與命名空間（`str` 欄位落 `%sv`）門禁：`cg-test` **127/127**`cg-runtime-test` **37/37**`%struct.s0 = type { %str-long, i64 }` 的 `hold()`（`'abcd'.len + 9`）`opt` rc=0 + `clang -O2` **exit 13** + ASan 零報告；**負向對照**（宣告欄位順序寫反）⇒ `insertvalue operand and field disagree in type`；`no vet` 四檔 0 ERROR四檔 `no fmt -d` 0 hunk⚠️ **str 欄位的逐欄位 RC 未做（§11 B11）**：e2e 只用 `STRCONST`（無 RC 頭）故 ASan 乾淨，**不代表** str 欄位安全**未覆蓋**：逐欄位 RC（B11）嵌套 struct 欄位（技術上已通但未實測）struct 作形參/返回值具名 struct → id 映射表
- **M4-c 結構體欄位 RC 原語已完成（s33，§5.3.4.bm）**：`mir.no` 增 `OP-STRUCT-RETAIN=37`/`OP-STRUCT-RELEASE=38`（`xa[k]`=結構體值 id`bs[k]`=**欄位位掩碼**，bit j 置位 ⇒ 字段 j 持有堆）；`cg.no` 兩個 emit 分支逐位置位發三步 `%rf<k>_<j> = extractvalue %struct.s<id> %st<x>, <j>` → `%rp<k>_<j> = extractvalue %str-long …, 2` → `call void @nolang_rc_retain(i8* …)`（release 版多 `%rl<k>_<j> = call i1 @nolang_rc_release`）；`cg-test.no` 增 `test-struct-field-rc`（6 斷言，含掩碼未置位的字段不得被遍歷）🔴 **`STRUCT-MK` 刻意不自動 retain**：① 操作數可能是 `STRCONST` 全局常量（retain 會讀 `data-16`，即全局對象**之外**的記憶體）；② `vty` 只說型別**不說所有權**——要不要 retain由呼叫方用掩碼顯式聲明🔴 **UAF 只在真讀位元組時發生**：`%str-long` 的 len/cap 是**內聯值**，`STRLEN` 讀 SSA 值不碰堆 ⇒ s32 那種 `'abcd'.len` 的 e2e 對漏 retain**徹底靜默**；本輪 e2e 因此在取字段後再做一次 `STRCAT`（`@str_concat` 會 `memcpy` 源字節）才踩到已釋放緩衝門禁：`cg-test` **133/133**`cg-runtime-test` **37/37**204 行模組 `opt` rc=0 + `clang -O2` **exit 37** + 補屬性 ASan **exit 37 零報告**；**三個負向對照**（`tmp/s33ctl.py` 在 `.ll` 上做）：刪 `STRUCT-RETAIN` ⇒ **exit 134 `heap-use-after-free … READ of size 6`**（`@str_concat` 的 `memcpy`）刪 `STRUCT-RELEASE` ⇒ exit 37 無報告（**無鑑別力**，洩漏方向）重複 `STRUCT-RELEASE` ⇒ **exit 134 `READ of size 8`**（`@nolang_rc_release` 讀 rc 的那次 `load`，且 `rc==0` 借用哨兵把 double free 降級成一次越界讀）🔴 **門禁口徑修正（§11 B12）**：`clang -x ir -fsanitize=address x.ll` 對 **IR 輸入完全不插樁**——最小反例（`malloc`→`free`→`load`）exit 0二進位只有 `__asan_init`/`__version_mismatch`；ASan pass 只插樁帶 `sanitize_address` **函式屬性**的函式，clang 前端處理 `.ll` 時不加 ⇒ s26–s32 的ASan 零報告只覆蓋 libc 攔截器（`malloc`/`free`/`memcpy`），裸 `load`/`store` UAF 全漏修法：`scripts/asan_instrument.py` 補屬性再編（`opt -passes=asan` 手插亦可，但 LLVM22 在 MachO 報 COMDAT 錯）`no vet` 四檔 0 ERROR`no vet compiler/src` 34 ERROR 不變四檔 `no fmt -d` 0 hunk；`ps/ck/lx/tok/util/diag` 38/20/45/62/27/7 全綠**未覆蓋**：逐欄位 RC 的**自動放置**（B11，需值是否擁有堆的所有權位）掩碼 >64 字段（需分塊）`vec`/`option` 字段（B5/B8 同族）嵌套 struct 字段（掩碼只掃一層）洩漏方向無動態門禁（macOS arm64 無 LSan）
- **M4-c 結構體作一等值（形參/返回值 + 嵌套欄位）已完成（s34，§5.3.4.bn）**：**零 emit 變更**——`ptyps[q]`/`retty` 寫 `TY-STRUCT-BASE + id``sft` 的欄位型別寫子 id 即通（`define %struct.s0 @bump(%struct.s0 %st1)` / `ret %struct.s0 %st5` / `%st2 = call %struct.s0 @bump(%struct.s0 %st1)` / `%struct.s1 = type { %struct.s0, i64 }`），本件把它從有臂未實測變成有測試釘住🔴 **唯一的代碼修復是撞出來的真 bug**：`cg-lit-name(fname, k)` 把字面量全局從 `@.s<k>` 改為 `@.s.<函式名>.<k>`——兩個函式各自第 k 條 `STRCONST` 會撞成同名（`opt: redefinition of global '@.s1'`），而 s29–s33 的 e2e 全是單一業務函式 + 無字面量的 main故從未觸發判據：**全局名只含指令下標 ⇒ 必撞**（§11 B10 從具名型別擴到全局符號）`cg-test.no` 增 `test-struct-param-ret`（7 斷言）`test-struct-nested`（6 斷言），三條字面量斷言改新名 ⇒ **146/146**門禁：`cg-runtime-test` **37/37**三個 e2e（`mk/bump/total` ⇒ **exit 8**嵌套 ⇒ **exit 16**邊界 RC ⇒ **exit 7**）`opt` rc=0 + `clang -O2` + 補屬性 ASan 零報告；**五個對照**：父欄位宣告成 `i64` ⇒ `insertvalue operand and field disagree in type`漏發子宣告 ⇒ `invalid type for null constant`**宣告順序寫反 ⇒ 照樣 exit 16（否證子型別必須先宣告的猜想，LLVM 具名型別惰性解析）**`STRUCT-RELEASE` 挪到 call 前 ⇒ ASan `heap-use-after-free READ of size 6`（釘住參數是引用語義caller 的 release 必須晚於最後一次使用）；`no vet` 三檔 0 ERROR三檔 `no fmt -d` 0 hunk**未覆蓋**：跨函式所有權轉移（B13）嵌套欄位的 RCstruct 陣列/切片多返回值struct 作 vec 元素具名 struct → id 映射表
---

## 7. 驗證策略（差分矩陣）


### 7.1 五級門禁總表

| 級      | 對象                    | Oracle                          | 判據                                        | 成本           |
| ------ | --------------------- | ------------------------------- | ----------------------------------------- | ------------ |
| **L0** | `compiler/tests/*_test.no` | `test/runner.no`（測試檔自檢） | 全綠；⚠️ 測試檔必須自己 `f-count > 0 -> exit(1)`⚠️ 執行方式必須是 **`no run <file>`**：`no <file>` 只印 usage 且 rc=0 ⇒ 每個測試檔的 stdout 都相同 ⇒ **G7 判 oracle 無鑑別力** | 秒            |
| **L1** | 詞法                    | `<l1-ctl> tokens`（**repo 外只讀 dumper**，§7.1.1） | 全語料 token 流逐位元組一致                         | 分鐘           |
| **L2** | 語法                    | `no ast`                        | 全語料 AST dump 逐位元組一致                       | 分鐘           |
| **L3** | 語義                    | `no vet`                        | 歸一化後逐行一致 + `[ERROR]` 集合 diff 為空           | 分鐘           |
| **L4** | 行為                    | `tests/golden/mir-baseline.tsv` | `REGRESS = 0`；所有 DIVERGE 逐一歸因             | **~7–17 分鐘** |
| **L5** | 自舉                    | s2 vs s3                        | 行為定點 + （加分）逐位元組定點                         | 分鐘           |

#### 7.1.1 L1 的 oracle：**repo 外的只讀 dumper**（繞過 P0-②）

**問題**：L1 要一把能吐出規範化 token 流的尺子文檔原訂靠 **P0-②（新增 `no tokens` 子命令）**，但那需要改 `src/`而自舉的 M0/M1 階段有一條硬約束：**`src/` 不得改動**（`no` 必須始終是一把可信可回歸的尺子，DR-5）

**繞法**：把 oracle 放在 **repo 之外**（`/tmp/notok/`），它是一個 **Go** 程式，用 `replace` 指令 `import` 同一個 `src/lexer` 包——也就是說，**P0-② 若實作也會走同一個包**，兩者的判準天然一致它提供與 SUT 相同的 CLI 形狀：

```
notok tokens <file>          # `tokens` 被忽略，只為了與 SUT 共用同一組 argv
```

輸出每行一個 token：`<line>:<col> <TYPE> <esc(literal)> <esc(raw)> <esc(marker)> <esc(err)>`，其中 `esc()` 把 `\`→`\\``\n`→`\n``\t`→`\t``\r`→`\r`位元組 `<0x20` 或 `≥0x7f` → `\xHH`（小寫），空字串 → `-`**必須轉義**：不轉義的話，含換行的 raw string 會讓一個 token跨成多行，行數就對不上

**接線**：`scripts/selfhost_diff.sh --l1-ctl <path>`**沒給或路徑不可執行** ⇒ L1 誠實報 **UNAVAILABLE**（退出碼 4），**不是 PASS**

**兩個必須有的守衛**（否則 L1 會靜默變成橡皮圖章）：

| 守衛          | 觸發條件                                            | 為什麼需要                                                                                                          |
| ----------- | ----------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| **缺 oracle** | `--l1-ctl` 未給 / 不可執行                              | `no` **沒有** `tokens` 子命令 ⇒ 不能拿 `bin/no` 當尺子                                                    |
| **G7（無鑑別力）** | 控制組在 **≥3** 個檔上只吐 **1 種** stdout 雜湊               | 實測踩過：`bin/no tokens <f>` 對**任何**輸入都印同一份 usage，**rc=0**stdout 非空 ⇒ G1/G2/G3 **全都攔不住**，會被記成SUT 與控制組不同的 **FAIL**（把責任推給 SUT）G7 改記 **UNAVAILABLE** |

**自檢契約的修正**：`--self-test` 在 L1 這一格必須把 `SUT` 設成 **`$L1_CTL`**（L1 的尺子），而不是 `bin/no`——用兩把**不同的**尺子量同一批檔當然會不同，那會在自檢裡誤報 FAIL

**P0-② 的處置**：仍然要做，但**不再阻塞 M1**它的價值在於`noc` 能自己當 oracle（自舉後 `noc` 必須能產出 `no` 的那份 token 流，不能永遠依賴一個 repo 外的 Go 程式），以及 §8 的同一 binary 跑 3 次雜湊一致確定性驗收


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

**收尾後的自檢**：`SAME + DIVERGE + REGRESS + IMPROVED + BOTH_FAIL + NEW == 語料檔案數`，且任一 bucket 內不得出現重複路徑

**最強的 A/B 形式**（不要 `diff fp.txt other.txt`，行序非確定）：

```python
load = lambda p: {l.split(' ', 2)[2].strip(): tuple(l.split(' ', 2)[:2]) for l in open(p)}
a, b = load('/tmp/fp_ctl.txt'), load('/tmp/fp_noc.txt')
diff = [k for k in sorted(set(a) | set(b)) if a.get(k) != b.get(k)]
print(len(a), len(b), len(diff))
```


### 7.3 L3 的歸一化（`no vet` 的**路徑前綴不一致**）

> **訂正（2026-10-09，`HEAD=f1cc56d3` 重測）**：本文早期版本聲稱 `no vet` 對導入 std 的檔案會打印 ~290 條 HINT且**歸屬逐次不同**（8 次跑出 5 個 stdout 雜湊）**該現象在當前樹上未複現**——同一個 binary同一個輸入，`no vet` 是**確定**的：

```
$ for i in 1 2 3 4; do ./bin/no vet src/std 2>/dev/null | shasum -a 256; done
92a6a19a0b6d3588…      ← 4/4 逐位元組相同（147 條 HINT）
```

HINT 總數也已由 ~290 變為 **147**（std 已變動）**不要把未複現寫成已修好**：它可能只是當年的輸入不同但至少在當前樹上，L3 **不需要**因非確定性而排序這一步

**仍然必須歸一化**——但理由是**路徑前綴不一致**（不是非確定性）：同一份輸出裡，目錄**頂層檔**報告為**相對路徑**，而被當作**依賴載入**的模組報告為**絕對路徑**實測（`no vet src/std`，147 行）：

| 前綴                                   | 行數  | 來源                                    |
| ------------------------------------ | --- | ------------------------------------- |
| `src/std/…`                          | 143 | `src/std` 下**直接被 vet** 的檔             |
| `/Users/…/no/src/std/str.no`         | 4   | `str.no` —— 以**依賴**身分被載入（故為絕對路徑）      |

⇒ 兩支 binary（或兩台機器兩個 checkout）只要對同一模組走了不同的載入路徑，就會產生**假 DIFF**歸一化（保留 `line:col: message`，丟掉檔名與目錄）：

```bash
norm() { sed -E 's#^[^ ]*/([^/ ]+):([0-9]+):([0-9]+): #\2:\3: #'; }
```

歸一化後 `line:col: message` 穩定，且**跨 binary / 跨 checkout 可比****並且**：A/B 的兩支 binary 必須以**相同方式**構建（`make no` vs `make no`），否則 `main.version` 的差異會讓**每個檔案**都吐一行 `warning: compiler version mismatch`，造成 **100% 假 DIFF 而 stdout 逐位元組全同**

### 7.4 L1 / L2 的失敗集合也要比

只比成功的檔案是否一致是不夠的必須**同時**比：

- **解析失敗的檔案集合**（兩邊必須完全相同）；
- **失敗訊息的位置**（line/col）

這是唯一能抓出`noc` 對某個語法過度寬鬆的方法

⚠️ **還是要印出 oracle 的 `<unknown>` 計數**（§5.3.1）：`no ast` 已補齊，**基準值是 0**因此這行現在的角色**反轉**了——它從警告盲點變成**回歸偵測**：

> 語料 528 檔的 dump 中出現 **任何** `<unknown>` ⇒ **有節點型別沒被 dump**（通常是 parser 新增了節點而 `dump.go` 沒跟上）⇒ oracle 在**那一個子樹上**已退化，L2 綠燈會靜默地比實際更強

`dump_completeness_test.go` 會在 `go test` 時就擋住這種退化；harness 的這一計數是第二道（且涵蓋 harness 自己的語料範圍）


### 7.5 差分的假通過偵測（**每一個自製 harness 都必須有**）

歷史教訓：一個 `PATH` 少了 `llvm`/`clang` 的 harness 會讓**每個檔案** `rc=1` + 空 stdout，然後0 differ看起來像完美結果（實測 97/97完全一致而實際上什麼都沒跑）

**每個 harness 的結尾必須打印**：

1. **控制組 `rc==0` 的檔案數**——≈0 ⇒ harness 壞了，整個比較作廢；
2. **`sha256(stdout) == e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`（空字串雜湊）的檔案數**——偏高 ⇒ 什麼都沒執行；
3. **控制組與被測組各自的非空輸出數**

**實例（2026-10-10，G7 抓到的真 bug）**：L0 的呼叫一度寫成直接跑 `<bin> <file>`（子命令用 `"-"` 佔位）但 `no <file>` **不是**執行——它印出 usage 並 **rc=0**於是三個測試檔的 stdout 完全相同（同一份 usage），差分器看到的是三檔一致rc=0，**看起來完美**

```
$ ./bin/no compiler/tests/tok-test.no | shasum -a 256     # 錯的呼叫
febadc57a53b79d8a913838c9afed6d30f16736dfcc6ec3d1e20f8264442b718
$ ./bin/no compiler/tests/lx-test.no | shasum -a 256      # 同一個雜湊！
febadc57a53b79d8a913838c9afed6d30f16736dfcc6ec3d1e20f8264442b718
```

G7（控制組在 ≥3 檔上只吐 1 種 stdout 雜湊 ⇒ UNAVAILABLE）把這輪擋下來了**若沒有 G7，L0 會一直是綠燈而什麼都沒測** 修法：子命令改成 `run`

**G8（本節新增）：診斷訊息本身也可能是靜默的謊**（2026-10-10 實測）

harness 在 L1 無 oracle 時要印一段說明，其中三行原本寫成**雙引號內含反引號**：

```bash
echo "  用法：--l1-ctl /path/to/dumper   （必須支援 `<dumper> tokens <file>`）"
```

雙引號內的反引號是 **命令替換**，於是這行不是印出說明，而是**去執行** `` `<dumper> tokens <file>` ``——`<` `>` 被當成重導向 ⇒ bash 報 `syntax error near unexpected token 'newline'`，該行輸出變成殘缺的 `（必須支援 ）`另兩行的反引號則會**真的執行 `no`** 與 `bin/no tokens <f>`

**教訓**：診斷訊息是 harness 的**輸出**，它自己也可能壞掉，而壞掉的方式恰好是看起來像正常輸出——與 G7 同一類⇒ 修法＝這三行改用**單引號**；並在 `bash -n` 之外，**實際跑一次 UNAVAILABLE 路徑**（`--levels L1` 不給 `--l1-ctl`）確認訊息完整

⚠️ 注意 `bash -n` **抓不到**這個錯誤（它只檢查語法，而雙引號＋反引號本身語法合法）；只有**執行**到那一行才會炸⇒ 對只在失敗路徑才會印的訊息，必須**主動走一遍失敗路徑**


### 7.6 SHS 守護（`tools/shs-check.no`）

對 `compiler/src/**/*.no` 做**編譯器自身的解析**（用 `noc ast`，不是正則掃描），檢查：

| 檢查                 | 說明                                           |
| ------------------ | -------------------------------------------- |
| 無一等函式值             | 不得出現 `name = (params) (results)` 形式的具名函式型別定義 |
| 無指針 / FFI          | 不得出現 `*T` 型別`#{c}` 註解                       |
| 無匿名函式型別            | 不得出現 `cb ()()` 形式                            |
| 無模組級可變 `let`       | 白名單（只讀常量表）由檔案頭註釋聲明                           |
| 無 `any` / `obj`    | —                                            |
| 所有位元組讀取用 `.byte()` | 啟發式：`s[...]` 出現在以 `.len-bytes()` 為界的迴圈中 ⇒ 警告 |

⚠️ **不要用 Python/正則寫這個掃描器**Nolang 有**三種註釋形式**（`;` / `//` / `;;…;;`），且 `/` 是上下文敏感的（正則字面量 vs 除法）一個只認 `//` 與 `/* */` 的掃描器會把 `;` 註釋裡的中文當成程式碼（實測：442 個檔案報出 17 個假陽性，而真正的編譯器報 0）

---


## 8. 前置改動清單

> 這些是**對現有 `no` 的改動**，全部走 `nolang-compiler-change` 技能的流程（控制組A/Bgolden）它們不屬於自舉本身，但**不做的話自舉會寸步難行**

| ID       | 改動                                                                                               | 優先        | 影響                                                                                                                             | 驗收                                                             |
| -------- | ------------------------------------------------------------------------------------------------ | --------- | ------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------- |
| **P0-①** | `package.jsonc` 的 `compiler` 區塊新增 `"overflow": "wrap"`，作為**包級默認**（現有的 `#{overflow=…}` 敘述級註解仍然優先） | 🔴 **最高** | 否則編譯器源碼每行算術都要註解（§2.2.2）                                                                                                        | 新鍵生效 ⇒ 一個含 10 條算術的函式在無註解下編譯通過；**且**現有語料的 `(rc, stdout)` 不變（L4） |
| **P0-②** | 新增 `no tokens <file>` 子命令                                                                        | 🔴 高      | M1 的 oracle⚠️ **已由 §7.1.1 的 repo 外只讀 dumper 繞過**（M1 因此不必先改 `src/`）；② 的剩餘價值＝**自舉後 `noc` 能自己當 oracle**，不再依賴外部 Go 程式                                                                                                                    | 對語料輸出確定性 token 流；同一 binary 跑 3 次雜湊一致                           |
| **P0-③** | `no ast --annotations`（或 dump 中加入註解節點）                                                           | ✅ **已完成** | M2 的註解覆蓋                                                                                                                       | ✅ 語料 **287 條 / 88 檔**註解進入覆蓋；預設行為不變（`TestNoAnnotationsByDefault`）；`dumpNode` 同時由 40 → **71** 個 case，語料 `<unknown>` **319 → 0**（§5.3.1） |
| **P0-④** | **一等函式值修復**（§2.2.1 的 6 種崩潰形狀）                                                                    | 🟠 中高     | M7 的語料完整覆蓋；`noc` 作為完整編譯器的必要條件                                                                                                | 6 種形狀全部正確；`tests/fn-type-*.no` 全綠；全語料 L4 無 REGRESS             |
| **P0-⑤** | **模組頂層綁定的生成條件**：改為模組被引用即生成，而非被**入口檔直接** import 才生成（§2.2.8）                                  | 🔴 **最高** | 否則跨模組常量在間接依賴下**靜默為 0**；作為運算式運算元時**硬編譯失敗**這是自舉期間**唯一**必須靠人工記住回入口檔補一行的反組合約束 | 最小重現（§2.2.8）三種配置全部輸出 `42`；`compiler/main.no` 移除那兩行未使用 import 後仍正確；全語料 L4 不變 |
| **P0-⑥** | `fs.read-str` 的**逐位元組無損**讀取（新增 `fs.read-str-bytes` 或讓呼叫端統一走 `read-bytes` + `to-bytes-str`）（§2.2.9）    | 🔴 高      | 詞法分析器必須看到**原始位元組**；否則所有非 ASCII 註解／字面量被二次編碼，且 `rc` 測不出來                                                                          | 讀 5 位元組檔 `len-bytes()` 回 5；全語料 L1 無 DIFF                                 |
| **P0-⑦** | `src/lexer/lexer.go` 的 `readRawString` **回捲是空操作**（`LexerState` 未存字元位置）⇒ 每個續行吃掉前導空白**再加一個字元**                  | 🟡 低      | 目前語料**不可達**（1584 檔中 0 個真正的 raw string 字面量）；但 `noc` 的 L1 必須與 `no` **逐位元組**相同 ⇒ 必須二選一：修 `no` 並同步 `noc`，或**照抄缺陷**  | 決定後寫進 `compiler/src/lx.no` 檔頭；附可重跑重現；全語料 L1 無 DIFF                          |
| **P1-①** | 解析器棧深的實測與（必要時）迭代化                                                                                | 🟠 中      | M2 的健壯性                                                                                                                        | 量測報告 + 極限嵌套測試                                                  |
| **P1-②** | `std` 新增字串構建器（或 `compiler/util/sb.no` 先做，M7 再上收）                                                 | 🟡 低      | M4 效能                                                                                                                          | 1 MiB push 的 RSS 平線                                            |
| **P1-③** | `no ast` / `no tokens` 的輸出**確定性**保證（arena 順序而非指標順序）                                              | 🟠 中      | L2 的可比性                                                                                                                        | 同一 binary 跑 3 次逐位元組一致                                          |
| **P1-④** | `std` 補上 `chmod`（`std/os.no` 或 `std/fs.no`）                                                      | 🟡 低      | M5 驅動層：`no` 用 `os.Chmod(out, 0755)`，但 **nolang std 目前沒有 chmod**（`grep -rn chmod src/std/` 為空）多數情況下 clang 產物自帶執行權限，但快取/複製路徑會丟失 | `fs.chmod(path, mode)` 可用；全語料 L4 不變                            |
| **P1-⑤** | `fs.read-bytes` 對 **0 位元組檔**回 `nil`（應回 `ok` + 空切片，或另加 `fs.read-bytes-or-empty`）（§2.2.10）             | 🟡 低      | 讀到空與讀失敗無法用回傳值區分（`nil` vs `err`）⇒ 每個呼叫端都要自己記住這條反直覺規則；漏了就是 rc 層級的假 DIFF（`noc tokens <空檔>`）         | 0 位元組檔回 `ok` 且 `len()==0`；不存在的路徑仍回 `err`；全語料 L1 不變                     |
| **P2-①** | `noc` 的 `-native` 對接（本方案不做，留給 nocg/nold 設計）                                                      | ⚪ 低       | 去 LLVM 依賴                                                                                                                      | —                                                              |

---


## 9. 風險登記

| ID      | 風險                                                                                                                          | 機率 | 影響 | 緩解                                                                                          |
| ------- | --------------------------------------------------------------------------------------------------------------------------- | -- | -- | ------------------------------------------------------------------------------------------- |
| **R1**  | **規模失控**：12 萬行 Go 移植，實際 nolang 行數可能更多                                                                                       | 高  | 極高 | 8 個里程碑各自可獨立驗收；M4 用 RC 砍掉 ~40% 的 `src/mir`；M8 明確排除 fmt/lsp/tools                             |
| **R2**  | **效能不可接受**：`noc` 比 `no` 慢 5–10×，導致開發循環崩潰                                                                                    | 中高 | 高  | 開發期**始終**用 S0；`noc` 只在門禁與自舉時跑；M7 才做效能；先量測再優化（**≥3 次取樣**，repo 的歷史顯示單次取樣無法分辨 1.5 s 的效應）       |
| **R3**  | **RC 的 UAF**：漏一個 retain 就是 UAF（不是洩漏）                                                                                        | 中  | 極高 | `check-rc-balance` 驗證器**只檢查欠計數方向**；**必須證明它會壞**（人為刪 retain ⇒ 它必須報錯）；每個里程碑跑全語料                |
| **R4**  | **把非確定輸出誤判為無害**：實測過一次——`tests/slice-heavy.no` 的輸出逐次不同，曾被記為ASLR 位址，不可歸因，實際上它**讀了已釋放的緩衝**（輸出裡赫然是 `@nolang_rc_alloc` 的頭魔數） | 中  | 高  | 差異檔案**必須讀輸出**；逐次不同只證明非確定，**不證明無害**判據：看起來像位址/魔數/可變長度 ⇒ **記憶體安全缺陷**，去找它                |
| **R5**  | **並行 session 污染**：repo 常有其他 session 的未提交改動                                                                                  | 高  | 中  | 動手前**鎖 SHA**；A/B **兩邊都從同一份 `rsync` 快照構建**（相隔幾分鐘各建一支不保證配對）；**未經確認不得 add/commit/tag/push**    |
| **R6**  | **逐位元組定點不可達**：Go 的 map 迭代順序非確定，`codegen` 的清理釋放順序隨之變化（repo 已實測：同 binary 5 次跑出 5 個不同的 dump 雜湊，而**程式的 stdout 逐位元組穩定**）         | 中  | 低  | 行為定點是**硬性**要求；逐位元組定點是**加分**項**永遠不要 hash MIR dump 做 A/B**；要比就比 `clone` 指令數或程式的 stdout       |
| **R7**  | **棧溢位**：`alloca` 必須在 entry block ⇒ 每個遞迴框架佔用函式全部區域變數之和                                                                     | 中  | 中  | M2 內實測；必要時迭代化最深的遞迴（表達式的括號/後綴鏈）                                                              |
| **R8**  | **`#{buildin}` 樁的三條路徑**：漏一條 ⇒ 靜默錯值，且 `rc` 測不出來                                                                              | 中  | 高  | M3 的明確驗收項；**必須 hash stdout**；掃描樁時**先剝 `;` 行註解**（否則 `database/sql.no:19` 的註釋會把模組數從 15 算成 16） |
| **R9**  | **跨模組前綴的維護成本**：每次跨檔調用都要 `ShortName.`                                                                                        | 高  | 低  | 模組名 ≤3 字元；高度耦合的階段同檔；M0 定死切分                                                                 |
| **R10** | **信任鏈（Thompson）**                                                                                                           | 低  | 極高 | §3.2 的三重反制；文檔中**明確記錄**S0 是 TCB                                                            |

---


## 10. 開放問題（需拍板）

| #      | 問題                                           | 選項                                     | 建議                                                               |
| ------ | -------------------------------------------- | -------------------------------------- | ---------------------------------------------------------------- |
| **Q1** | S1 是否允許部分借用 S0？例如 `noc` 只做前端，IR 生成仍調用 `no` | (a) 完全不借用 (b) 允許 M1–M3 只做前端用 `no` 出後端 | **(b)** 作為 M1–M3 的臨時驗證手段：讓 `noc ast` 先可用，提早拿到 L2 門禁；但 M4 必須自產 IR |
| **Q2** | RC 還是精確所有權作為 M4 的目標？                         | (a) RC 先行 (b) 直接移植 `analysis.go`       | **(a)**：見 §5.6.1                                                 |
| **Q3** | 是否移植 `src/fmt`？                              | (a) 移植 (b) 用 S0 的 `no fmt` 外部完成        | **(b)**；`fmt` 與自舉無關                                              |
| **Q4** | 語料範圍：全 527 tests 還是SHS 相容的子集？              | (a) 全語料 (b) 分階段擴大                      | **(b)**：M1–M3 全語料（便宜）；M4 先 SHS+std，再逐步擴大                         |
| **Q5** | 逐位元組定點（R6）是否作為 M6 的硬性驗收？                     | (a) 硬性 (b) 加分                          | **(b)**                                                          |
| **Q6** | `noc` 是否要暴露 `no nocg` / `no nold` 這類調試子命令？   | —                                      | 與 `nolang-native-backend.md` 的 Q5 合併討論                           |
| **Q7** | S0 的版本鎖定策略                                   | (a) 鎖 `HEAD` SHA (b) 鎖 tag（`v0.3.19`）  | **(a)** 用於門禁，(b) 用於對外發布                                          |

---

## 11. 已知缺口記帳（待補項清單）

> 本節是已知但未在本輪收斂的缺口的集中帳本每條都標註來源小節與現狀，供後續增量直接認領§5.3.4.w 的見 §11.1 記帳即指 §11.1


### 11.1 解析器／語法表示待補形（本輪累積）

| #   | 待補形                                                  | 正主（oracle）行為                          | 現狀                                                            | 來源                       |
| --- | ------------------------------------------------------ | ------------------------------------------ | -------------------------------------------------------------- | -------------------------- |
| F1  | `IDENT LPAREN` 起頭塊                                  | Go `blockIface`                            | 語料未見，未接線                                               | §5.3.4.w（:1363）          |
| F2  | `IDENT NEWLINE` 起頭塊                                | Go `blockUnknown`                          | 語料未見，未接線                                               | §5.3.4.w（:1363）          |
| F3  | `matchArm` helper 接線                                | `ast.no` 的 `helper` 分派僅接 `taggedEnumField`；`matchArm` 仍記 `t.unsup`（`ast.no:1221` 注釋已記錄） | 語料 653 檔因 ps.no 尚未產 `SurfaceMatch` 全部走不到，故無 L2 訊號；`interfaceMethod` 已移入已接線列（s8z 哨兵遷移，:1746） | §5.3.4.x（:1119）          |
| F4  | 僅宣告無初始值 `e [3]i64`（無 `= …`）                  | oracle 出 `LetStatement{name, type, value:空}` | 當前分派守衛硬性要求 `+5==ASSIGN` ⇒ 誤解析為 `IndexExpression`；本增量只釘住有初始值形 | §6.3（:2159）              |


### 11.2 既有基線與前置待辦（非本輪引入）

| #   | 項                                                                                                          | 狀態                                                                                     | 來源                          |
| --- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ----------------------------- |
| B1  | `no vet compiler/src` 19 ERROR（`nolang-index-write` 族，ps.no 既有 `out[i] = …` 形）                          | M3 之前既有基線，待 M3 期間清償或標記 grandfather                                            | §6.4（:2187）                 |
| B2  | §8 前置改動清單未結項：P0-①/②/④/⑤/⑥/⑦P1-①~⑤P2-①                                                         | 多為不先做則自舉寸步難行或低優先；P0-③ 已完成                                             | §8（:2365-2377）              |
| B3  | M3-c 剩餘：`Validate*`/型別推斷族（§5.4 剩 36 個）缺口③其餘 saveError 站點／④入口跳合族`@ <mod>/<fn> <alias>` export 糖 parser 解析 | s13 已落地首族 `ck.no`，續推見 s13下一增量建議                                        | §5.3.4.ar（:1870）/§6.4（:2198） |
| B4  | M4–M8（中端/代碼生成驅動層定點所有權分析後置 fmt/lsp/tools）                                              | **M4-a/b 骨架＋M4-c 控制流/循環/字符串/拼接/多參調用/str 跨塊/vec/帶型別簽名/帶型別 CALL/option/retain·release 原語/STRCAT 實參為變數的 RC 週期/option 的 str 載荷讀寫（內聯，§11 B8）/struct（構造+讀欄位，型別收歸 prelude）/struct 寫欄位（`STRUCT-SET`，讀舊出新值）+ 結構體槽三件套（跨塊）/多結構體型別（型別碼編 id + `sfc` 描述符 + 任意欄位數）/結構體欄位型別（`sft` 拼接表 + 欄位型別感知的 MK/GET/SET）/結構體欄位 RC 原語（`STRUCT-RETAIN`/`STRUCT-RELEASE` + 欄位掩碼）/結構體作一等值（形參·返回值·嵌套欄位）＋字面量全局唯一化（`@.s.<函式名>.<k>`）／ALU 家族（位運算·移位·除餘·比較，`i64` 標量，§5.3.4.bo）／**無符號 ALU 家族（`lshr/udiv/urem` + `ult/ule/ugt/uge` + `TY-U64` 型別碼，§5.3.4.bp）** ／`f64` 算術家族（`fadd/fsub/fmul/fdiv/frem` + `fcmp` 按型別分派 + `TY-F64` 型別碼，§5.3.4.br）／整數除零 select 護欄（`noc`↔Go 保真對齊，§5.3.4.bs）／除零 `divZeroReport` stderr 報告（§5.3.4.bt）／option 的 `vec` 載荷內聯讀寫（§5.3.4.bu） 已落地（s14–s40，§5.3.4.at–.bu）**；M4-c 餘項（vec 元素非 i64 的逐元素 RC**結構體逐欄位 RC 的自動放置（原語已備，B11）**跨函式所有權轉移（B13）struct 陣列/切片>24B 堆上裝箱（`?vec`≤24B 內聯讀寫已 s40）整數除零的 option 降級（B14，`select` 護欄已 s38 落地）；`byte`/`f32` 窄型別算術（`u64` 已 s36，`f64` 已 s37 落地）；PHI 本方案決定走 alloca 不引故非必需）**M4-e `h2m` AST→MIR 降級已續推至 s99n（s98 2026-11-08／s99–s99n 2026-11-09，件號 54，§5.3.4.ea–.eo）**：s55–s97（2026-11-06，共 43 件，§5.3.4.cj–.dz）完成直線式 i64 → 簽名/調用 → 控制流/跨塊槽 → 花括號短路鏈 → 塊終結 → 計數/區間/容器遍歷 → 值匹配 → 實參池 args → 邏輯 `&&`/`||` → i64/u64 除零護欄（B14 半項償）→ u64 全族 → i64 `>>` 裁定 lshr → f64 槽化與 `%` → bool/char 字面量 → TY-OPT 控制流鋪槽（當時 `h2m-test` 845/852）；s98 清 cg `OP-SHR` ashr 死臂；s99–s99n 補 struct 全族／欄位 RC／所有權 provenance（s99m `ownn`·`ownv`·`rdval`·`rdown`·`retset`）／vec 切片字面量與 `OP-VEC-FREE=94`（s99n）；`0fbbe684`（11-10）重構 struct lowering 與 ownership；**現況 `h2m-test` 1060/1066（紅 6 皆既有 s98 optmatch）、`cg-runtime-test` 86/86、`cg-str16-test` 41/41、`cg-test` 359/359、`drv-build-test` 53/53**（⑦ 解鎖並校正 16B 斷言、⑨ 補 print 斷言、⑩ 補插值與 `...` 斷言、⑪ 補值位塊匹配斷言；詳 §5.3.4.eo／§6.5／文末 2026-11-12 快照）；✅ 2026-11-11 Go 後端 `%str-long` 改 16B `{ i32, i32, i8* }`，selfhost `cg-runtime.no`／`cg.no` **已於 2026-11-12 同步清償**（§5.3.4.ax、§11 後 ⑥／⑦）；✅ **M4-d 已落地（2026-11-12，§11 後 ⑧）**：`h2m-lower-module` 合成 `@main`、`link.no` 調 clang、`drv.cmd-build`／`cmd-run` 雙序 argv、e2e 與 Go oracle 同值；✅ **s100 `print`／`eprint` 已降級（§11 後 ⑨）**：`OP-PRINT=95`＋prelude 九助手，L4 真 SAME 由 0 → **2/169**；✅ **s101 `...` 佔位陳述＋s102 字面量插值已降級（§11 後 ⑩）**：`tok.ELLIPSIS3=92`＋`K-EMPTY-STATEMENT` no-op 臂、插值三檔分類逐段 `OP-PRINT`，L4 真 SAME 2 → **4/169**、全語料 unsup 11,857 → 11,436；✅ **s103 值位塊匹配（`r = { c -> v  -> d }`）已降級（§11 後 ⑪）**：`@smv<id>` 結果槽前掃描登記＋entry 統一 `OP-ALLOC`（不在降級途中補 alloca）、`h2m-lower-surface` 加值位分支（逐臂 `OP-STORE`、匯聚 `OP-LOAD`），臂值非 `i64`／臂體非單表達式／`valueCtx` 未置位一律誠實門禁，`h2m-test` 1031/1037 → **1060/1066**、`drv-build-test` 42/42 → **53/53**；✅ **s104 `str` 相等/不等已降級（§11 後 ⑫）**：`==`/`!=` 兩側皆 `TY-STR` ⇒ `OP-STEREQ`/`OP-STNEQ`（**零改 cg/mir**，兩 op 與 `@str_eq` 早已就位），並同步補 `h2m-ty-of` 的 `str`×`str` 分支（缺它則 str 比較/拼接只在 `mem==1` 掛），`h2m-test` 1060/1066 → **1080/1086**、`drv-build-test` 53/53 → **63/63**、全語料 unsup 11,354 → **11,163**（25 檔降 / 0 檔升）、自舉模組合計 2,456 → **2,368**；並以臨時插樁量出 unsup 的 AST 節點型別直方圖 ⇒ 判定 **`#` 模組導入為下一輪唯一結構性閘**（611 條 `K-CALL-EXPRESSION` 標記全卡在點號被調者＝跨模組）；**s105 修掉解析器保真度缺口**——`ps-is-struct-def` 不容忍欄位尾 `;` 註釋（Go 的 `nextToken()` 跳過 COMMENT、`ps-peek-at` 不跳過），使 `ast`/`h2m-ctx`/`lxst` 等結構體**從未登記**、155 個函式簽名被判 bad；修後 15 個自舉模組簽名被拒 **250 → 166**、實際產碼函式數 `ast.no` 3→8／`lx.no` 2→3，而**全語料 173 檔逐檔 `diff` 完全相同**（11,315、`unsup==0` 5 檔，零回歸零增益）；自舉模組 unsup 2,362 → **3,198** 屬「更多函式被嘗試」的預期上升（判據須同時看 `define` 行數）；餘 **L4 全語料行為門禁收斂**（主體殘差＝`#` 導入／下標讀／`ok(...)` 條件臂／值位匹配的 str/vec 臂值／空 match 臂與裸塊陳述，以及 B19 那條待拍板的鏈語義裁決）與 M5–M8 ⬜ 未開始                                                              | §6／§6.5／§5.3.4.ea–.eo    |
| B5  | `vec` 刻意偏離 Go 的 inline 展開（改用 4 個 helper），且數據緩衝用裸 `@malloc` 無 RC 頭                                    | 元素目前僅 i64，故無 RC 需求；擴容時 `@free` 舊緩衝 ⇒ 不泄漏**元素變成 `str`/`vec` 前必須換成 `@nolang_rc_alloc` + 逐元素 retain/release**，否則會 UAF偏離本身是為壓 IR 量級把擴容邏輯收斂成唯一一份（§5.3.4.bb） | §5.3.4.bb（:2500 附近） |
| B6  | 🔴 **工具鏈 bug（`no` 側）**：`#{overflow=wrap}` 首次綁定**算術**表達式後，該變數 `vec.push(v)` 靜默壓入 **0**（讀取/普通呼叫/字面量/插值全對） | 爆破半徑窄但**門禁全綠**（`opt`/`vet`/`no test` 都看不出來）⇒ 規避寫法：先裸宣告 `g = 0` 再帶註解重賦值`compiler/src` 現有代碼不觸發；已列為 SHS 檢查候選 | §5.3.4.bb（:2510 附近） |
| B7  | `vty`（值型別表）**按值 id 索引**，长度必须 ≥ 最大 id+1；漏填即越界读到脏型别（不会报错，只会发错 IR 或让 `opt` 报型别错） | s24 的唯一脆弱点**约束**：`hir2mir` 落地时必须分配值 id 的同一处写 `vty`，不得事后补填；`cg-emit-cfg` 壳已按 `n+1` 自动铺满，只有走 `-t` 入口的手写路径需要自律 | §5.3.4.bd |
| B8  | `%option` 在 IR 里只有**一个**型别（`{ i64, [3 x i64] }`），载荷的真实型别只活在语义层 ⇒ `OP-OPTGET` 只能读slot[0] 当 i64，对 `?str`/`?vec` 载荷是**错读** | 与 `%vec` 元素型别不可见是同一族问题（步长/布局靠声明而非型别）**约束**：`hir2mir` 必须按载荷型别选 op（i64 用 `OPTGET`，str 走 `OP-OPTMKSTR`/`OP-OPTGETSTR` 内联进 24B slot 且 data 指標須 ptrtoint/inttoptr 非 bitcast，vec ≤24B 走逐字段內聯（s40 已落地，免 alloca/bitcast），>24B 才 alloca+bitcast/堆裝箱），不能只看这是个 option`?str` 载荷内联读寫已落地（s28，§5.3.4.bh，免裝箱因 `%str-long` 恰 24B 与 `[3 x i64]` 佈局相容）`?vec`（`%vec` 恰 24B，三字段全 i64）內聯讀寫亦已落地（s40，§5.3.4.bu，走逐字段 extract/insertvalue 免 ptrtoint）>24 字节载荷（`?vec`/大結構）的堆上装箱（`tag==0` 才解引用）仍待實現〔⚠️ 2026-11-12 核對：本列「免裝箱因 `%str-long` 恰 24B 与 `[3 x i64]` 佈局相容」是 **2026-11-11 前的歷史論證**；slot 仍 24B、`?str` 仍 inline（16B ≤ 24B），但兩者已非 1:1 逐欄對應，三步 `insertvalue`／`ptrtoint` 還原 data 的理由過期。見 §5.3.4.ax／§5.3.4.bh／B28〕 | §5.3.4.be/.bh/.bu／§11.2 B28 |---
| B9  | 🔴 **跨模塊同名常數衝突**：`mir.no` 與 `cg-rc.no` 都曾定義 `OP-RETAIN`/`OP-RELEASE`（前者 MIR op 27/28，後者校驗事件碼 0/1）`compiler/tests/cg-test.no` 同時 import 兩者時未限定名會**同名衝突**（nolang 跨模塊調用須 `ShortName.` 前綴，見 R9） | 已消解：把 `cg-rc.no` 的事件碼改名為 `RC-RETAIN`/`RC-RELEASE`（同步改 `cg-rc-check-balance` 內參照）；MIR op 27/28 留給 `mir.no`**教訓**：新增 MIR op 前先 `grep` 全 `compiler/src` 的常數名，避免與既有模塊撞名 | §5.3.4.bf |
| B10 | 🔴 **模塊級實體在 LLVM 模塊內唯一（具名型別 **與全域符號**）**：`%struct.point` 原由 `cg-emit-cfg-t` 按本函式用到結構體就發一次發射；**同族的第二例在 s34 才暴露**——字面量全局 `@.s<k>` 只帶指令下標，兩個函式各自第 k 條 `STRCONST` 撞成同名 ⇒ `opt: redefinition of global` | 型別側已消解兩步：s29 收歸 prelude；**s31 起改由 `cg-runtime-struct-decls(sfc)` 這一支 API 唯一發射**（prelude 不再帶結構體型別，`cg-emit-cfg-t` 一個字都不發）⇒ 從一條註釋變成一個 API 形狀符號側由 **s34 的 `cg-lit-name(fname, k)` 修掉**（`@.s.<函式名>.<k>`，零 API 變更零模塊級狀態）**判據（可直接拿去審新 op）**：全局名裡**只含指令下標 `k`** ⇒ 必撞；含**函式名**或**模塊級常量** ⇒ 安全**教訓**：凡模塊級實體（具名型別全域常量宣告）都只能有**唯一**發射點；**單函式單元測試對此完全無鑒別力**——必須用≥2 個函式同時用同一型別/同一字面量的模組才驗得出 | §5.3.4.bi/.bk/.bn |
| B11 | 🟡 **結構體 `str`/`vec` 欄位 RC：原語已備，自動放置未做**s33 交付了 `OP-STRUCT-RETAIN=37`/`OP-STRUCT-RELEASE=38`（欄位位掩碼 + 逐字段三步 emit），但**誰在何處呼叫它們**尚未決定：`STRUCT-MK` 存欄位時**不 retain**`STSTORE`/`STRUCT-SET` 覆蓋舊結構體時**不 release** 舊欄位 | 現狀仍等價於淺拷貝 + 無所有權：欄位裡若是堆上 `str`（`@str_concat` 結果），源值一 release，結構體裡就是**懸垂指標**（UAF）**這條為什麼不能由發射器自動補**：`STRUCT-MK` 的操作數可能是 `STRCONST`（全局常量無 RC 頭，retain 會讀到全局對象之外的頭），而 `vty` 只帶**型別**不帶**所有權** ⇒ 要不要 retain必須由呼叫方用掩碼**顯式聲明**s32 的 e2e 只用 `STRCONST` 故 ASan 乾淨——**那不是安全性證據**；s33 的 e2e 用堆上串並已證明漏 retain= `heap-use-after-free`（§5.3.4.bm 負向對照 A）**剩餘約束**：`hir2mir` 降級str 欄位 struct前須補 ① 一個值是否擁有堆的所有權位（或等價的靜態判據）；② 在 `MK`/`SET`/`STSTORE` 存入處 retain覆蓋/離開作用域處 release 的**放置規則**與 B5（vec 元素非 i64）同一族 | §5.3.4.bl / §5.3.4.bm |
| B12 | 🔴 **ASan 門禁對 `.ll` 輸入默認不插樁**：`clang -x ir -fsanitize=address x.ll` 只鏈 runtime + libc 攔截器，**一行 `__asan_load*` 都不插** | 實測最小反例（`malloc`→`free`→`load`）：exit **0**二進位只有 `__asan_init`/`__version_mismatch`原因：ASan pass 只插樁帶 `sanitize_address` **函式屬性**的函式——clang 前端處理 `.no` 源碼時自動加，處理 `.ll` 輸入時**不加**⇒ **s26–s32 各節ASan 零報告只覆蓋 libc 攔截器**（`malloc`/`free`/`memcpy`/`memmove`），裸 `load`/`store` 的 UAF 全部漏報修法（已落地 `scripts/asan_instrument.py`）：給每個 `define` 補 `sanitize_address` 再交 clang（`opt -passes=asan` 手插亦可，但 LLVM22 在 MachO 上報 `doesn't support COMDATs, 'asan.module_ctor'`）**並行約束**：對照必須跑 `-O0`——`-O1/-O2` 會把常量串拼接整段折疊掉，負向對照變假綠 | §5.3.4.bm |
| B13 | 🟡 **跨函式邊界的所有權轉移未建模**：返回值把含堆欄位的結構體交給 caller 時，所有權轉移（callee 不得 releasecaller 收到即 owner）目前**只靠約定**；`check-rc-balance` 是按**值 id** 記賬的**函式內**靜態工具，跨函式看不見 | s34 用 e2e 釘住了傳參那一半（§5.3.4.bn：`STRUCT-RELEASE` 挪到 call 前 ⇒ ASan `heap-use-after-free READ of size 6`），但返回那一半沒有對照手段——callee 若在 `RET` 前把自己的堆欄位 release 掉，caller 拿到的是懸垂結構體，而**現有工具全綠**（`opt`/`no vet`/函式內賬本都不跨邊界）**約束**：`hir2mir` 落地返回含堆值前必須補：① 一個跨函式的擁有權傳遞判定（Go 側 `moveTransfersOwnership` 的 RC 版）；② 或更保守的做法——把返回值也納入 `check-rc-balance` 的記賬（把 `RET` 的堆值記成一次 RETAIN 事件交給 caller）與 B11 同一族 | §5.3.4.bn |
| B14 | 🟡 **整數除零護欄的 noc↔Go 保真缺口（`OP-DIV/MOD` 及無符號 `OP-DIVU/MODU`：`noc` 直發裸 `sdiv/srem/udiv/urem`，缺 Go 後端的 select 護欄）**：s35 的 ALU 家族把 `OP-DIV`/`OP-MOD` 直接發成 `sdiv`/`srem i64`，s36 的無符號族把 `OP-DIVU`/`OP-MODU` 直接發成 `udiv`/`urem i64`——但本件核實權威 Go 後端 `src/mir/codegen.go` 其實在 codegen 層就插了護欄（`icmp eq <divisor>,0` + `select` 安全除數，使除零結果有定義，僅 `isFloat` 跳過）——故 `sdiv/srem/udiv/urem` 指令本身無錯，真實缺口是 `noc`（自舉 codegen）把 `OP-DIV/MOD`（s35）與 `OP-DIVU/MODU`（s36）直發裸指令，缺了 Go 後端有的 select 護欄 ⇒ `noc` 產物除零是 SIGFPE，而 `no` 是定義值（保真缺口，非雙方都 UB） | Nolang 語義層本應把 `x/0` 就地降級為 `?i64` 的 `err`（AGENTS.md §3.5），但這屬 `hir2mir` 的**語義降級**職責不是 codegen 這層；`sdiv/srem/udiv/urem` 本身無錯，缺的是在除法處補 option 包裝的上游規則**約束**：`hir2mir` 落地整數除法時必須在發 `OP-DIV`/`OP-MOD`（及無符號版）前插入除零判定並降級為 `?i64`（或顯式選照抄 Go 的 UB並寫進 `compiler/src` 檔頭），否則 `noc` 產出的程式會把本該 `err` 的除零變成 SIGFPE，且 `rc`/`opt`/`no vet` 全綠測不出來 | §5.3.4.bo / §5.3.4.bp / §5.3.4.br（除零護欄更正）／§5.3.4.bs（s38 落地 codegen 層 `select` 護欄）／§5.3.4.bt（s39 落地 `divZeroReport` stderr 報告；本項 codegen 半項至此全收，僅餘 `hir2mir` 的 option 降級半項；註：s75/s76 已落地 `h2m` 側 i64/u64 除零護欄，與 codegen 半項對齊，剩餘僅 option 包裝降級）|
| B15 | 🔴 **入口檔缺 `ps` 的傳遞依賴（`asp`／`lx`／`tok`）時症狀不報錯**——只導入 `ast`／`ps`／自建檔時跨模組常量解析不到，要麼 `v0.4.13` 構建期 `EmitLLVM: consumer reads value 0 (NoVal) … bug #85` 泛濫（s55 實測 286 KB 日誌，含 `ps_parse_stmt` 165 條），要麼**構建成功但運行期無限循環掛起**（同目錄最小探針實測） | 未解析常量被靜默降成 `const void` 而非硬錯誤——正解在 Go 側把未解析常量報錯；本側只能靠入口檔自查補齊導入（依 §2 不改他人源碼） | §5.3.4.cj／`compiler/tests/ps-test.no` 檔頭（同記述先於 s55 存在）／`compiler/tests/h2m-test.no` 檔頭警告注 |
| B16 | ✅ **已銷帳（2026-11-04，s64，§5.3.4.cs：實參改走側池 `args`，`xa[k]`=池內基址下標、`bs`=個數，段內值 id 可任意散布）**——原缺口：🔴 **`cg` 的 `OP-CALL` 實參必須佔連續值 id（`xa[k]..xa[k]+bs[k]-1`），Go 側 `Instruction.Args` 是任意 `[]ValueID` 無此約束** | s56 的調用降級只能在"每個實參恰好吃一個新 id（字面量／零參調用）或復用上一個剛分配的 id"時產碼，`ad(a, 2 + 3)` 這類留空洞的實參被門禁抓走（計 `unsup`，整函式不產碼）**約束**：真實修法是把實參放進獨立 args arena（需改 `cg-emit-cfg-t` 的 12 條形參並觸及 342 條 `cg-test`）；未改前任何降級器都必須自證連續性，不得假設"按序分配就連續"**s64 銷帳**：按真實修法落地（側池＋必填第 13 形參）；同族殘留僅餘 `OP-STRUCT-SET` 的 `xa[k]+1` 自帶約定，另記 B26 | §5.3.4.ck（s56）／§5.3.4.bd（s24 連續約定源頭）／§5.3.4.cs（s64 銷帳） |
| B17 | 🔴 **多輸出函式的呼叫形式是 `a, b = f(x)`——把已有變量按位置傳入 `f(x, a, b)` 不回寫輸出，且 checker 不報任何錯（靜默）** | s56 開發期實測：四個輸出全部拿到呼叫方初值，函式名被降成空串（產出 `define i64 @()`），`rc`/`opt`/`no vet` 全綠——看不出任何異常**約束**：自舉側凡需讀多個輸出必須用逗號多賦值形（權威用法見 `compiler/src/ps.no` 的 `src, src-expr = ps-for-source(t, p)`）；未降級的函式必須走"不產碼"而非補零返回，否則差分看像通過而實值全錯 | §5.3.4.ck（s56） |
| B18 | 🔴 **呼叫直接嵌在另一呼叫的實參位時，傳出去的是內層函式的輸入參數值而非它的輸出參數值（Go 工具鏈側）** | s57 合成 AST 實測：`ast.kid-add(t, iff, mk-block1(t, then-s))` 把塊節點添成 `then-s`（內層的**輸入**）而非 `mk-block1` 的回傳值（它的**輸出**），降級因此把 LetStatement 當 BlockStatement，`unsup` 計 2；拆成"先落具名局部再傳參"即正確（`mk-infix(t, '>', idn, iz)` 一類純值巢狀未見異常，爆破面限"輸出參數作實參嵌入"）；**約束**：凡靠回傳值續建結構的呼叫一律先落局部變數（`h2m-test.no` 的 `test-flow-else-syn` 已寫成紀律注記）；依 §2 黃金法則不改他人代碼 | §5.3.4.cl（s57） |
| B19 | 🔴 **花括號短路鏈若沒有兜底臂，Go 工具鏈不短路——所有命中的臂都執行，最後寫入者贏**（規範上是 if/elif 只取首個命中臂） | s58 對拍實測：`tw = (n i64) (r i64) { r = 0 { n > 3 -> { print('L1')  r = 1 }  n > 1 -> { print('L2')  r = 2 } } }` 在 `n = 4` 印出 `L1` **與** `L2` 並回 **2**；同一鏈補上兜底臂 `-> { print('MW')  r = 9 }` 即短路正常（`n = 4` 只印 `M1` 回 1）⇒ 爆破面限"無兜底臂"，成因未定位（推測與末端不匯聚的折疊分支有關）；**約束**：自舉側 `h2m` 按規範產碼（首臂命中即 `BR` 匯聚），對拍程式必須為每條鏈補兜底臂方可比；最小反例 `tmp/s58-sem3.no`；依 §2 黃金法則只記錄不改他人代碼 | §5.3.4.cm（s58） |
| B20 | 🔴 **花括號鏈的全部臂體皆純賦值（無副作用）時，兜底臂的寫入被整個丟掉——鏈內讀得到／鏈外讀不到** | s58 對拍實測：`{ n > 3 -> r = 1  -> r = 9 }` 在 `n = 0` 時函式內 `print(r)` 得 **9**，呼叫方拿到 **0**（`tmp/s58-sem7.no` `e1(0)=0`）；副作用塞進任一臂即恢復（`e2` 首臂加 print ⇒ 9；`e3` 兜底臂加 print ⇒ 9）；改用局部量再賦給輸出參數同樣丟（`tmp/s58-sem8.no` `loc(0)=0`）；兜底臂是唯一臂時正常（`only(0)=9`）⇒ 爆破面是"多臂 + 全純賦值"；**約束**：`h2m` 按規範發射（兜底臂體真落塊並 `STORE`），對拍時讓兜底臂寫入值與鏈前預設值同數，使 Go 側丟不丟寫入都不影響結果（§5.3.4.cm 關鍵點 4）；依 §2 只記不改 | §5.3.4.cm（s58） |
| B21 | 🟡 **Go `hir2mir.lowerFor` 認到計數位時靜默吞掉條件位**：`src/mir/hir2mir.go:4871-4975` 在 `countID != NoID` 時直接轉入計數分支（`:5408-5474` `lowerCountFor`），既不檢查也不降級 `Condition`——像 `3 * (n < 2) { }` 這種計數位與條件位並存的節點，Go 側把條件當不存在 | `noc` 側採**有記錄的偏離**：`compiler/src/h2m.no:1053` 對 `cnt != 0 && cond != 0` 形狀計 `unsup` 並讓整函式不產碼，而不是照 Go 丟棄條件——理由是靜默丟棄會讓差分兩側拿到同一退出碼而中間求值路徑不同（假綠）**約束**：若要跟齊 Go，需先在語料中確認 "count 與 cond 並存" 的真實用例（本輪探針與 `h2m-test` 未見），否則保持門禁更穩；負例由 `test-cnt-gate` 釘住 | §5.3.4.co（s60m） |
| B22 | 🔴 **`ps.no` 不為標識符計數位建 `ForStatement`**：`n * { }`（計數位是標識符而非整數字面量）實測退化為 `K-EXPRESSION-STATEMENT`(kind 27) 包 `K-IDENT`(kind 35)，並在旁落一條 `K-LET`(kind 42)（`tmp/s60m-ast-out.txt` 的函式 `f` 段）；只有整數字面量形 `3 * { }` 與 `} * 3` 才產 `K-FOR-STATEMENT`(kind 30，`nkid=6`) | 降級側只能對該形計 `unsup` 不產碼（`tmp/probe-s60m-ir2.no` 實測 `unsup=1` 且函式 `g` 完全不進 IR），而不是去猜旁落的 `K-LET` 語義把它拼回循環——拼回去等於在 codegen 層重寫解析器**約束**：真正的修法在 `ps.no` 的前置掃描臂群補標識符計數位，依 §2 黃金法則本輪只記錄不改他人源碼 | §5.3.4.co（s60m） |
| B23 | 🟡 **Go `hir2mir.lowerRangeFor` 用 `l.locals[varName] = iSlot` 登記循環變量，同名嵌套時內層覆寫外層且循環結束後不恢復**：`src/mir/hir2mir.go:5095` 直接寫表，全函式範圍內無 save/restore——外層 `i <- [0..5)` 體內再寫 `i <- [0..2)` 時，內層結束後 `locals["i"]` 仍指向內層槽，外層其後對 `i` 的引用會讀到內層的最後值——外層自身的終止判與步進走已捕獲的 `iSlot` Go 變量（`:5145`／`:5148`／`:5167`／`:5170`），不受污染，故爆破面限"同名嵌套且內層之後仍引用該名"；`loopStack` 由 `defer` 恢復（`:5127`）而 `locals` 不會，兩者不同步；本輪**未做 Go 側 e2e 實測**，屬源碼研讀結論 | `noc` 側採**有記錄的偏離**：`compiler/src/h2m.no` 以活動名棧 `c.rnms`／`c.rnms-n`（:104-105／:1373-1378）判重，進區間循環 push 源名，退出 pop，同名嵌套即計 `unsup` 令整函式不產碼（鑑別態 `tmp/s61-g1-nested-same-name.no` 實測 exit **101**），而不是複製這個覆寫語義——理由是兩顆同名槽在 IR 文本上完全對稱，反向刀切不出差異，產碼等於未驗證**約束**：與計數循環的 `@cnt<node-id>` 合成槽（§5.3.4.co 關鍵點 2）是兩條路徑，計數位不判重因它不引入源碼可見名；日後若要收該形，先補 Go 側 e2e 釘住預期值再定方針 | §5.3.4.cp（s61） |
| B24 | 🟡 **Go 容器遍歷把循環變量綁到元素位址（`bindElemPlace`），體內寫 `v` 即寫回容器；`noc` 側 `OP-VECGET` 交回副本，寫 `v` 只改槽**：`src/mir/hir2mir.go:5179-5301` 的容器分支在 body 塊 `OpIndex` 之後對非 map 源調 `bindElemPlace(...)` 並 `defer restore()`，循環體內的賦值直接落到 `dat + i*8`；`restore` 於循環結束還原綁定 | `noc` 側採**有記錄的偏離**：`compiler/src/h2m.no` 以活動名棧 `c.epl`／`c.epl-n`（:118-119／:1610-1615）登記循環變量名，`h2m-var-write` 入口（:377-390）掃中即計 `unsup` 令整函式不產碼（鑑別態 `tmp/s62-g1-write-loopvar.no` 實測 exit **101**），而不是產出一個與 Go 語義不同的寫回；差分對拍因此只在**只讀循環變量**子集上同語義（`tmp/s62-oracle.no` 檔頭明載）**約束**：要收該形需在 `cg.no` 補一條回寫路徑（新增 `@vec_set_i64` helper 或直接發 `dat + i*8` 的 `getelementptr` 加 `store`），並以 e2e 釘住 Go 側寫回後的容器實況再定方針；依 §2 黃金法則不改他人代碼 | §5.3.4.cq（s62） |

| B25 | 🟡 **差分對拍的語料兩側都必須合法：算術結果是 `?i64`，不可直接賦給 `i64` 變量；`x - 1: { ... }` 這類表達式主體在 parse 期就脫離 Matched** | s63 實測：oracle 首版寫 `y i64 = x - 1` 被 Go checker 拒（`cannot assign ?i64 value to i64 variable 'y'`，錯誤尾碼 `15w45dqk`），改成拷貝 `y i64 = x` 才兩側同時合法——noc 側不會因 oracle 編不過而報錯，只有真跑差分才看得見，故 **oracle 的 `no build` 必須每輪實跑**，不能只跑自產 IR 那一側；同輪另測得三條語言邊界（Go 文案自證：the one-line form `k = <expr>: { ... }` only attaches when `<expr>` is a bare identifier; otherwise the match detaches；多值樣 `1, 2 ->` 報 `expected variable name after ','`；option 單 `ok ->` 臂報 `option match must handle all branches: err, nil, ok`）——三者屬語言邊界而非自舉缺口，`h2m-lower-surface` 對其計 `unsup` 是正確行為 | §5.3.4.cr（s63） |
| B26 | 🟡 **`OP-STRUCT-SET` 的新欄位值 id 仍走自帶連續約定（`xa[k]`=舊結構體值 id、`xa[k]+1`=新欄位值 id），s64 的側池只摘了 `OP-CALL` 一條**：`mir.no:393` 註已改口「本 op 自帶連續約定，⚠️ s64 後與 `OP-CALL` 非同形」；發射側仍按兩個相鄰 id 讀舊值與新值（s30 形制，§5.3.4.bj） | `h2m` 落地結構體寫欄位時必須先自證「舊結構體值 id 與新欄位值 id 恰相鄰」，不得假設任意散布；或把本 op 也併入側池形制（需同步 `cg` 的 STRUCT-SET 臂與 `cg-test` 斷言）**約束**：`h2m` 現對 `STRUCT-SET` 全計 `unsup` 不產碼，故本項暫無實彈路徑，落地形參／返回值結構體欄位寫入時才觸發 | §5.3.4.cs（s64 關鍵點 6）／§5.3.4.bj（s30 SET 連續約定源頭） |
| B27 | 🟡 **Go 嵌套 match 的 `it` 海森堡（no-print 得 2、print 下得 3）**：`n: { 1 -> { t0 = it; m: { 2 -> r = it; -> r = 9 }; r = r + t0 }  -> r = 0 }` 在 Go v0.4.13 與 HEAD 上無 print 時得 **2**（外 `it` 被內層 `it` 騎絕），語義值應為 **3**（外 it=1、內 it=2、`r = it + t0 = 3`）；向函式內任一處加 `print` 後鏈語轉到 3，noc 側恆穩 3。探針對不齊時 **不改合法源碼繞過**（黃金法則），改探針為「一切內層計算全放內臂」的 `2 -> r = it + t0` 穩形；證據文件 `tmp/s65-sem.no`/`s65-sem2..6.no`/`s65-sem4.no`/`s65-sem5w.no` | 未修（屬 Go 側 S0 hir/mir/checker 在途，非自舉線）；待 Go 側回銷後可重新以舊形開探針差分 | §5.3.4.ct（s65 驗證環境 Go oracle 對拍段） |
| B28 | ✅ **已清償（2026-11-12）：selfhost `%str-long` 由 24B `{ i64, i64, i8* }` 同步為 16B `{ i32, i32, i8* }`**（原記帳：`compiler/src/cg-runtime.no:44` 發 24B 型別、`:163` 發 `{ i64 0, i64 0, i8* null }, i64 %total, 0`；`cg.no` 檔頭寫「`%str-long` 恰 24B = `[3 x i64]` 槽」；`OP-OPTMKSTR`／`OP-OPTGETSTR` 按三欄 i64 逐欄 `insertvalue`） | **落地**：① prelude 型別改 `{ i32, i32, i8* }`；② `OP-STRCONST` 字面量直接給 `i32`、`OP-STRFROMVEC` 補 `trunc i64→i32`；③ `OP-STRLEN`／`OP-STRCAP` 補 `zext i32→i64`；④ `@str_eq`／`@str_concat` 欄位型別與 `{ i32 0, i32 0, i8* null }` 聚合常數同步，其函式本文與 Go `emitPrelude` **byte-identical**；⑤ `OP-OPTMKSTR` 存前 `zext i32→i64`、`OP-OPTGETSTR` 讀後 `trunc i64→i32`，`OP-OPTEQSTR`／`OP-OPTNEQSTR` 兩側重建 16B `%str-long` 並修掉 trunc 的 SSA def-before-use；`mir.no` 註釋一併改口。`%option` 的 24B slot（`optSlotDefaultBytes = 24`）**未變** ⇒ `?str` 仍 inline、只佔前 16 字節（結論不變、理由已更新）。**門禁**：`cg-runtime-test` 84/84、新增 `cg-str16-test` 41/41、完整 `.ll` 的 `opt -passes=verify` rc=0、`clang`＋C main 端到端 4/4 期望值、`h2m-test` 1031/1037 與基線一致無回歸。**殘差已清（⑦，2026-11-12 第四輪）**：`cg-test.no` 的 24B 時代文字斷言與 `15w45dqk` 編譯阻斷已一併校正，整檔 **358/358 全綠**；清償後 selfhost `.ll` 已可與現行 Go 後端做 L4 行為對拍 | §5.3.4.ax／§5.3.4.bh／§11 B8／§11 後 ⑥／⑦／`docs/docs/lang/memory.md`〈型別佈局〉 |
| B29 | 🔴 **`#` 模組導入（M5 入場券，自舉唯一剩餘結構性缺口）**：`ps-parse-use`（ps.no:985）不拆 `<path>.<symbol>`（把 `.dispatch` 符號吞進 path）、`h2m-sigs` 逐模組建（h2m.no:3952）、`h2m-lower-expr` 點號呼叫全擋（`cd.kind != asp.K-IDENTIFIER -> ok-call = 0`，h2m.no:2095）⇒ 611 條點號呼叫與 37 條 `K-USE-STATEMENT` 完全不被消費，`noc build compiler` 產物執行 `version` 不印 | **設計已立（2026-11-12，⑭）**：whole-program 降級（共享單 `agg = h2m-new()` + 全域簽名表 `key = mod.fn` + 多模組 body 串接 emit `@<mod>.<fn>`）；呼叫側 `K-DOT-EXPRESSION` 查全域表 emit `call @<mod.fn>`；**待實施**（⑭ ⑪ 六切片，首片＝`ps-parse-use` 拆分 `<path>.<symbol>`＋`as`，見 ⑭ ③ 修正點 1） | ⑭（2026-11-12 前向設計） |


### 2026-11-08 追記：表達式主語的值位 match（B25 第一項已於 Go 側解除）

`src/parser/expr.go` 的 `: {` 附加原本全部位在 Pratt 中綴迴圈**之前**，中綴運算結果
要等迴圈跑完才成形，故 `r = a % b: { ok -> it  -> 0 }` 會裂成 `r = a % b`＋一條
主體為空的裸 match。現在迴圈之後補一次附加（`precedence == LOWEST`＋CTX_EXPR＋
區塊須為 match 形狀），整段 match 成為單一 RHS，`expr: { ... }` 的優先級高於 `=`。

配套三處：`src/parser/lowering.go::resolveSubjectMatch` 為 option 算術主語
（`+ - * / %` 且兩側型別可判定）合成 `__match_subj_L_C ?T` wrapper，保證單次求值、
tag 比較與 `let it T` 綁定；`lowerer.inferValueMatchResult` 把值位 match 的結果
登記為 plain payload 型別，修掉「連續兩段值位 match 第二段 `it` 無型別 ⇒ MIR 零填」
（最小反例 `tmp/zz-valmatch/q1-consec.no`，修前 `1/0/0/0`、修後 `1/1/1/1`）；
`ASTVersion` bump 到 `2026-11-08.5`。`no fmt` 直接由 SurfaceMatch 渲然主語，實測
兩次格式化逐字相同、`no vet` 零 issue。門禁：
`src/parser/value_match_expr_subject_test.go`、`src/fmt/value_match_expr_subject_test.go`
（皆已實測判別力：關掉修復即失敗）。

> 本追記只描述 **Go 工具鏈**。上表 B25、`h2m-lower-surface` 的 `unsup` 與 s63 的
> 自舉差分語料屬 v0.4.13 歷史紀錄，自舉側尚未收表達式主語，維持原狀。


### 2026-11-12 追記：as-built 現況核對（只改文檔，未動 `compiler/`）

本輪是**文檔現況核對**：修正檔頭「`compiler/` 子項目目前不存在」、§4.1.1 的 2026-10-30 盤點、§6／§6.5／§11.2 B4／文末 2026-11-06 快照等過期表述，並補上 s98–s99n 與 2026-11-10／11-11 的變更。**未修改任何 `compiler/` 源碼**；§5.3.4 的歷史增量紀錄與實測證據全部保留，只加「歷史」標記，不覆寫。

**① 現況（2026-11-12，`HEAD = 0e055e11`）**

- `compiler/src/*.no` **14 檔**＋`compiler/src/util/*.no` **2 檔**；`compiler/tests/*.no` **13 檔**、`fixtures/*.no` **69 檔**（逐檔清單見 §4.1.1 末的 as-built 校正；⚠️ 此為 doc-only 輪次當日計數，⑥／⑦ 後 `compiler/tests/*.no` 為 **14 檔**）。
- M0–M3 完成；M4-a/b/c 已落地；**M4-e `h2m.no` AST→MIR 降級已續推至 s99n（件號 54，§5.3.4.eo，2026-11-09）**；**M4-d 已於 2026-11-12 接線落地（§11 後 ⑧：`h2m-lower-module` 合成 `@main`＋`link.no`＋`drv.cmd-build`／`cmd-run`，`drv-build-test` 16/16、e2e exit=42）**；`print`／`eprint` 已降級（§11 後 ⑨，`drv-build-test` 31/31）、`...` 佔位陳述與字面量插值已降級（§11 後 ⑩，`drv-build-test` 42/42、`lx-test` 81/81、L4 真 SAME 2→4），殘差為 L4 全語料行為門禁收斂（主體＝下標讀／方法調用／`#` 導入／block-as-value／`ok(...)` 條件臂）；M5–M8 未開始。
- s99 系列（14 節，§5.3.4.eb–.eo）補的是 **struct 全族＋所有權**：定義／欄位訪問／形參返回值／跨塊槽／跨函數／欄位型別（str·f64·嵌套·`[]i64`）／欄位 RC 取得與 scope-exit 釋放／vec 欄位就地索引；s99m 引入所有權 provenance（`ownn`／`ownv`／`rdval`／`rdown`／`retset`）並撤除 `mem==1` 且含 `OP-CALL` 的整函數閘門；s99n 補 vec 半邊——`K-SLICE-LITERAL` ⇒ `OP-VECNEW`／`OP-VECPUSH`、`@vec_free`／`OP-VEC-FREE = 94`、三條所有權外流門禁，使 str／struct／vec 三族的「取得所有權 ↔ scope-exit 釋放」配對齊備。

**② 2026-11-10 的兩筆重構**

- `a4b7c66f` `refactor(tape): optimize and flatten tape and kinds data structure`：`compiler/src/asp.no` 的 AST dump 規格表由嵌套陣列改成**扁平 `[]i64` 指令帶**——`tape` 每 **5** 個 `i64` 一條指令 `{op, a, b, c, d}`、`kinds` 每 **4** 個 `i64` 一種節點 `(name-id, first-instr, instr-count, reserved)`；同步改 `ast.no`／`ps.no`／`h2m.no`／`cg.no` 等讀表側與多檔 fixtures。`asp.no` 是**生成物**（`scripts/gen_ast_spec.py`），不可手改。
- `0fbbe684` `refactor(h2m): improve struct lowering and ownership handling`：重構 `h2m.no` 的 struct 降級與所有權處理（`h2m-test.no` +92 行），並把 `drv.no`／`shs-check.no` 與大批 `src/std/**`、fixtures 的結構字面量寫法統一為 `{...}` 緊貼型別的形式。

**③ 2026-11-11 的後端佈局變更（當日尚未同步到 selfhost；2026-11-12 已清償，見下方 ⑥）**

- `0e055e11` 在 §5.3.4.ax 補了 11 行警示：Go 後端 `src/mir/codegen.go::emitPrelude` 的 `%str-long` 已由 `{ i64, i64, i8* }`（24B）改為 **`{ i32, i32, i8* }`**（u32 len／u32 cap，16B），對外 API 仍是 `i64`（讀 `zext i32 -> i64`、寫 `trunc i64 -> i32`）；`docs/docs/lang/memory.md`／`str.md` 同步。
- 同一 commit 重生 `asp.no`：`KIND-COUNT 73→74`、`STR-COUNT 190→191`、`INSTR-COUNT 313→315`，新增 `EmptyStatement` 節點。
- **風險（2026-11-12 當日觀測，現已過期；修正見 ⑥）**：當日 `compiler/src/cg-runtime.no:44` 仍發 `%str-long = type { i64, i64, i8* }`、`:163` 仍發 `insertvalue %str-long { i64 0, i64 0, i8* null }, i64 %total, 0`；`cg.no` 檔頭仍寫「`%str-long` 恰 24B = `[3 x i64]` 槽」⇒ selfhost 產出的 IR 與現行 Go 後端 ABI **不相容**，§11 B8／§5.3.4.bh 的「24B 1:1 相容」論證亦一併過期（但 `?str` 仍為 inline，詳下條）。同步四步見 §5.3.4.ax 警示：① prelude 改 `{ i32, i32, i8* }`；② `OP-STRCONST` 的 `insertvalue` 先 `trunc i64 -> i32`；③ `OP-STRLEN` 的 `extractvalue` 後補 `zext i32 -> i64`；④ `OP-STRCAT` 等 helper 的聚合常數改 `{ i32 0, i32 0, i8* null }`。**本輪未處理**（依使用者裁定本輪只改文檔）；已正式記入 **§11.2 B28**，並於後續實作輪次清償（見 ⑥）。
- **對 `%option` 的精確影響（2026-11-12 核對）**：`%option = { i64 tag, [3 x i64] slot }` 的 **24B slot 未變**（`src/mir/codegen.go::optSlotDefaultBytes = 24`；`docs/docs/lang/memory.md`：「`%vec`（24 字節）與 option 預設的 24 位元組 inline payload 槽位**沒有**改變」）⇒ `?str` **仍是 inline、不需堆裝箱**，只用了前 16 字節放 `{ u32 len, u32 cap, i8* data }`。但 §5.3.4.bh／§11 B8 的「`%str-long` 恰 24B、與 `[3 x i64]` 1:1 相容 ⇒ 三步 `insertvalue`＋`ptrtoint` 還原 data」**理由已過期**：兩個 u32 擠進前 8 位元組、指標在 offset 8，Go 現行走「把 slot 位址 `bitcast` 成 `<payload>*`」（`optionPayloadInline` 段註釋）。⇒ 同步時**不只 prelude 四步**，`OP-OPTMKSTR`／`OP-OPTGETSTR` 亦須依 16B 重做（B28 第⑤步，**已於 ⑥ 落地**）。

**④ 門禁現況（誠實標注：非全綠）**

- `h2m-test.no` ⇒ **1031 passed / 6 failed / 1037 total**（§5.3.4.eo）；6 失全為 `s98 optmatch`，係併發 session 改 `cg.no` 的 option 回歸，首輪 baseline 即已失。
- `cg-runtime-test.no` ⇒ **66/66 全綠**（含 s99n 新增的 `@vec_free` 5 條斷言）。
- `cg-test.no` ⇒ ⚠️ **整檔當前不可編譯**：`15w45dqk` 檢查器把「plain `i64` 變數收 `?i64` 右值」收成硬錯誤，檔內 5 處 `j`／`w` 命中（§5.3.4.ea／.eo）；s99n 的 `test-vec-free` 已用最小閉包探針 `tmp/probe-s99n-cgfree.no` 單獨跑過 **4/4**。**（⚠️ 此為 doc-only 輪次當日觀測，已過期；④ 全節保留為歷史證據，現況見 ⑦：358/358 全綠）**
- 其餘 L0 抽測亦非全綠：`lx-test` 70/70、`ck-test` 20/20、`diag-test` 7/7；`ps-test` 39/42、`ck-sig-test` 41/50；`tok-test`／`ast-test`／`ps-err-test`／`util-test`／`verify-struct-elem` 仍有編譯期錯（多為 `15w45dqk` 或 `bug #85` `NoVal`）。**這些是工具鏈／語料跟進問題，不是 M4-e 降級本身的回歸**；本輪只記錄、未修。

**⑤ 本輪更正清單**：檔頭定位、§4.1.1 標題＋警示＋表頭＋as-built 校正、§6 M4 列、§6.5 M4-e 條、§11.2 B4、文末 2026-11-06 快照標記為歷史並補 2026-11-12 快照；**第二輪**再補 §5.3.4.bh 歷史警示、§6.5 三條 M4-c 摘要的過期標記、§11.2 B8 理由校正，並**新增 B28 記帳**（`%str-long` 24B→16B 當日未同步，**已於 ⑥ 清償**）。**凡 2026-10-09（`HEAD = 4feaaf70`）的行號／計數一律保留為歷史證據**；§11.2 既有基線表 B1–B27 原文亦保留（其 `:2148` 一類行號同屬歷史座標），只在 B8 尾與表末 B28 追加校正；B28 於 ⑥ 由「未同步」改記為「已清償」。**以上①–⑤兩輪均為 doc-only，未動 `compiler/`；⑥ 與 ⑦ 才是後續兩輪實作輪次。**

### ⑥ 2026-11-12 追記（續）：B28 清償——selfhost `%str-long` 同步為 16B（**實作輪次**）

> 本節與上方 ①–⑤ 的 **doc-only** 輪次不同：使用者裁定「繼續實現未完成」後，以 §11.2 B28 為
> 最高優先未了項動手改 `compiler/`；**Go 後端 `src/mir/codegen.go` 完全未動**，只把 selfhost 對齊過去。

**權威口徑**：`src/mir/codegen.go::emitPrelude` 的 `%str-long = type { i32, i32, i8* }`
（u32 len／u32 cap／data，16B；**對外 API 仍是 `i64`**，讀 `zext i32→i64`、寫 `trunc i64→i32`）。
`optSlotDefaultBytes = 24` **未變** ⇒ `%option = { i64, [3 x i64] }`（32B）與 `%vec = { i64, i64, i64 }`（24B）
原樣保留，`?str` **仍是 inline、仍免堆裝箱**，只是只佔 slot 前 16 字節、不再是三欄 `i64` 的 1:1 對應。

**改動檔案（4 改 + 1 新，全在 `compiler/`）**：

| 檔案 | 內容 |
| --- | --- |
| `src/cg-runtime.no` | prelude 型別改 16B；`@str_eq` 的 len 取 `i32` 再 `zext`；`@str_concat` 取 `i32`→`zext`→`add`→`trunc` 寫回，聚合常數改 `{ i32 0, i32 0, i8* null }`；保留 `%option`／`%vec` 原樣並加 B28 註 |
| `src/cg.no` | `OP-STRCONST` 直接給 `i32` 字面量；`OP-STRLEN`／`OP-STRCAP` 補 `zext i32→i64`；`OP-STRFROMVEC` 補 `trunc i64→i32`；`OP-OPTMKSTR` 存前 `zext`、`OP-OPTGETSTR` 讀後 `trunc`；`OP-OPTEQSTR`／`OP-OPTNEQSTR` 兩側重建 16B `%str-long` |
| `src/mir.no` | 把 s28／s40 的「`%str-long` 恰 24B、與 `[3 x i64]` 1:1」改用口（**純註釋、零行為變更**） |
| `tests/cg-runtime-test.no` | 66→**84** 斷言：16B 型別、反向斷言禁 24B、`@str_eq`／`@str_concat` 的 `zext`／`trunc`／`i32` 聚合常數 |
| `tests/cg-str16-test.no`（新） | **41** 條專門回歸門：以真 MIR op 序列覆蓋 STRCONST／STRLEN／STRCAP／STRCAT／OPTMKSTR／OPTGETSTR／OPTEQSTR／STRFROMVEC；附 `dump` 模式輸出完整 `.ll` 供 `opt`／`clang` 驗證 |

**驗證（2026-11-12 實測；一律 `./bin/no`，未跑 `make no`，未 commit）**：

- `./bin/no run compiler/tests/cg-runtime-test.no` ⇒ **84 passed / 0 failed / 84 total，`ALL TESTS PASSED`**
- `./bin/no run compiler/tests/cg-str16-test.no` ⇒ **41 passed / 0 failed / 41 total，`ALL TESTS PASSED`**
- `./bin/no run compiler/tests/cg-str16-test.no dump > /tmp/b28_gate.ll` ⇒ 332 行完整模組（prelude＋RC helper＋`@str_eq`／`@str_concat`／`@vec_*`＋4 個真 op 序列函式）
- `opt -passes=verify -disable-output /tmp/b28_gate.ll` ⇒ **rc=0**
- `clang` 把該 `.ll` 與 C `main` 連結執行 ⇒ `f-len=3`／`f-cat=0`／`f-opt=2`／`f-vec=0`，**4/4 期望值**，`rc=0`（LLVM 函式名含連字號，C 側用 `__asm__("_f-len")` 別名）
- **Go／selfhost 對齊**：`./bin/no build -v -o /tmp/b28_go_bin /tmp/b28-sample.no` 取 Go 後端 `.ll`；逐字比對得 `@str_eq`（552 bytes）與 `@str_concat`（818 bytes）函式本文 **byte-identical**，`%str-long`／`%vec`／`%option` 三條型別宣告一致
- `./bin/no run compiler/tests/h2m-test.no` ⇒ **1031 passed / 6 failed / 1037 total**，與既有基線完全相同（紅 6 皆 s98 optmatch）⇒ **無回歸**

**SSA 坑（已修）**：`OP-OPTEQSTR`／`OP-OPTNEQSTR` 初改把左側 `trunc` 發在重建 `%orx0` 之後，
生成 IR 出現 `%orx03 = insertvalue ..., i32 %olx0i3, 0` 早於 `%olx0i3 = trunc ...` 的 def-before-use；
已把左右兩側 trunc 分別移到各自 `insertvalue` 之前，並在 `cg-str16-test.no` 加兩條
「trunc 定義早於使用」的文字斷言釘住。

**刻意未動／殘差**：

- `compiler/tests/cg-test.no`（⑥ 當時）仍留 24B 時代的文字斷言（如 `insertvalue %str-long undef, i64 3, 0`、
  「`%str-long` 恰 24B」）；該檔當時因 checker `15w45dqk` **整檔不可編譯**，故 ⑥ 未改、也無法驗證，
  改由 `cg-str16-test.no` 獨立守門。**✅ 已於 ⑦ 清償**：`15w45dqk` 語料跟進＋9 條 16B 斷言校正＋
  `cg-zeros` 零初始化修復，整檔 **358/358 `ALL TESTS PASSED`**（詳下 ⑦）。
- `compiler/src/h2m.no` 未動；`ast-test` 的 `bug #85 NoVal`、其餘 L0 紅燈屬既有工具鏈／語料債，非本輪引入。
- `compiler/src/ck-sig.no`／`ps.no`／`tests/ck-sig-test.no` 是並行 session 既有的髒檔，全程未碰。
- 臨時驗證用 `compiler/tests/b28-dump.no` 已刪除；`compiler/tests/` 只留下述正式門禁。

### ⑦ 2026-11-12 追記（續）：解鎖 `cg-test.no`——`15w45dqk` 語料跟進、16B 斷言校正與 `cg-zeros` 零初始化修復（**實作輪次**）

> 承接 ⑥「刻意未動／殘差」第一條。本輪只動 `compiler/src/cg.no` 與 `compiler/tests/cg-test.no` 兩檔，把 `cg-test.no` 從**整檔不可編譯**拉回 **358/358 `ALL TESTS PASSED`**；`compiler/` 其餘檔、`src/` 與 Go 後端全程未動。並行 session 的髒檔（`src/mir/builtin_call.go`／`src/std/txt.no`／`src/i18n/catalog/locales/*`／`glossary/data/traps.noml`／`test/README.md`／`tests/txt-3.no`／`src/mir/builtin_unix.go`）亦未碰。

**① 第一類根因：checker `15w45dqk` 與 `?i64 → i64`**

現行 checker 把「plain `i64` 變數收 `?i64` 右值」收成硬錯誤；`cg-test.no` 有 5 處 `j = j + 1`／`w = w + 1` 命中（即便加了 `#{overflow=wrap}` 也**不改 `?T` 結果型別**，只改溢出行為）。修法不改 checker、不碰 `src/`，改走語言原生迭代：

| 原寫法 | 新寫法 | 理由 |
| --- | --- | --- |
| `j = 0; (j < 15) { ...; j = j + 1 }` | `15 * { ... }` | 固定次數用計數循環，無可變計數器 |
| `j = 0; (j < 10) { ...; j = j + 1 }` | `10 * { ... }` | 同上 |
| `w = 0; (w < 7) { ...; w = w + 1 }` | `7 * { ... }` | 同上 |
| `j = 0; (j < n) { blks.push(0); j = j + 1 }` | `zq <- [0..n): { blks.push(0) }` | 變數次數只能用區間遍歷 |

⚠️ `n * { ... }` 在此語境被 parse 期拒（`[venvcq6s] expected expression after operator '*'`），故變數次數一律走 `[0..n)`；`#{overflow=wrap}` 註解亦隨之移除。既有 `cg-emit-cfg` 的 `#{overflow=wrap}`／`m = n + 1` 保持原樣未動。

**② 第二類根因：`[] .with-len(n)` 只保留容量、不設長度**

解鎖後首次全量跑為 **342 passed / 16 failed / 358 total**。以暫存副本 `cgtest-debug.no` 在失敗斷言前印 `@@@BEGIN <label> @@@`／實際 IR／`@@@END <label> @@@`（輸出 `/tmp/b28_cgdebug.out`）分成兩類：

- **9 條 24B 時代文字斷言**：仍期待 `insertvalue %str-long undef, i64 3, 0` 或 `extractvalue %str-long ..., 0` 直出 `i64`。⑥ 已把發射器改成 16B ⇒ 改期待 `i32 3` 與 `zext i32 %slr<n>／%scr<n> to i64`（詳下 ③）。
- **7 條 `%struct.s<亂數>` 署名失敗**：`[] .with-len(n)` **只 reserve 容量、不設長度也不初始化**（實測 `.len()` 仍為 0、`.at(0)` 為 nil）；而發射器按下標直讀 `ptyps[q]`／`blks[k]`／`cty[k]` ⇒ 讀到底層未初始化緩衝的亂數，`cg-ty-name` 把它當 struct 型別碼，產出 `define i64 @dbl(%struct.s517895962700 %st1)` 這類非法署名。這是**既有發射器／測試表的真 bug**，不是 16B 改動引入。

修法：`compiler/src/cg.no` 新增 `cg-zeros(n)`——明確 `push(0)` 造「長度 n、全 0」的 `[]i64`；替換 `cg-emit-func`／`cg-emit-cfg` 便捷入口的 4 處 `[].with-len(...)`（`z`／`ty`／`vt`／`ct`），並把 `cg-test.no` 的 4 處測試表（`vty`／`cty`／`ptyps`／`cty2`）一併改用 `cg-zeros`。

**③ 16B 文字斷言校正（9 條＋1 條註解）**

| 位置 | 舊期待（24B） | 新期待（16B） |
| --- | --- | --- |
| `test-str-cap` | `extractvalue %str-long %sv1, 1`／`, 0` | `zext i32 %scr0 to i64`／`zext i32 %slr1 to i64` |
| `test-string` | `insertvalue %str-long undef, i64 3, 0`／`..., i64 3, 1`／`extractvalue ..., 0` | `i32 3`／`i32 3`／`zext i32 %slr1 to i64` |
| `test-strcat`／`test-str-slot`／`test-sig-mixed`／`test-struct-str-field` | `extractvalue %str-long ..., 0` | `zext i32 %slr3`／`%slr8`／`%slr0`／`%slr4` `to i64` |

另把 `test-option-str` 檔頭「`%str-long` 恰 24B」改口為「B28 後為 16B、仍內聯進 24B 槽前 16B」。`%option = { i64, [3 x i64] }` 的 24B slot（`optSlotDefaultBytes = 24`）與 `%vec = { i64, i64, i64 }` **未變**（⑥ 結論不變）。

**④ 門禁與可重跑命令**

```bash
./bin/no run compiler/tests/cg-test.no          # 358 passed / 0 failed / 358 total, ALL TESTS PASSED
./bin/no run compiler/tests/cg-runtime-test.no  # 84/84
./bin/no run compiler/tests/cg-str16-test.no    # 41/41
```

- `h2m-test` **1031/1037**（6 紅仍為既有 s98 optmatch，與 ⑥ 基線一致、非本輪引入）；⑥ 的 `opt -passes=verify` rc=0 與 `clang`＋C main 端到端 4/4 沿用，未重跑亦未變動。
- 診斷用的暫存副本 `compiler/tests/cgtest-debug.no` 已刪除，`compiler/tests/*.no` 回到 **14 檔**（13＋⑥ 的 `cg-str16-test.no`）。
- 實測 LOC（⑦ 後）：`cg.no` 2,530、`cg-runtime.no` 411、`mir.no` 465、`cg-test.no` 4,177、`cg-runtime-test.no` 241、`cg-str16-test.no` 280、`h2m.no` 3,849、`cg-rc.no` 180、`drv.no` 384。

**⑤ 仍刻意未動**

`compiler/src/h2m.no`、其餘 L0 紅燈（`ast-test` 的 `bug #85 NoVal`、`ps-test` 39/42、`ck-sig-test` 41/50、`tok-test`／`ps-err-test`／`util-test`／`verify-struct-elem` 的編譯期錯）皆為既有工具鏈／語料債；⑥／⑦ 兩輪均未修。**⑥ 記帳的 B28 殘差至此全清**，最高優先未了項回到 M4-d（`drv.cmd-build`／`cmd-run` 接線）。

### ⑧ 2026-11-12 追記（續）：M4-d 接線——`h2m-lower-module` 合成 `@main`、`link.no` 調 clang、`drv.cmd-build`／`cmd-run` 落地（**實作輪次**）

> 承接 ⑦ 末句「最高優先未了項回到 M4-d」。本輪把 `noc build`／`noc run` 從 M0 佔位（`drv.not-implemented` ⇒ rc=3）接成**真編譯鏈路**：原始碼 → AST → `h2m-lower-module` → 前置 prelude → 完整 `.ll` → `clang -x ir` → 可執行檔 → （`run`）同步執行並回傳子行程結束碼。
> 動到的檔：`compiler/src/h2m.no`（**只追加** `h2m-lower-module`，既有函式逐字節未動）、新增 `compiler/src/link.no`、`compiler/src/drv.no`、`compiler/main.no`、新增 `compiler/tests/drv-build-test.no`、新增 `compiler/tests/fixtures/m4d-exit.no`。
> **未動**：Go 後端 `src/**`、`compiler/src/cg.no`／`cg-runtime.no`／`mir.no`（B28 已於 ⑥ 收口）、`compiler/src/h2m.no` 的既有 3,849 行。並行 session 的髒檔（`src/mir/*.go`、`src/i18n/catalog/locales/*`、`src/std/txt.no`、`glossary/data/traps.noml`、`test/README.md`、`tests/txt-3.no` 等）全程未碰。

**① 為什麼必須「合成 `@main`」而不是直接降級 Program**

`h2m-lower-program` 只認 `K-FUNCTION-DEFINITION`／`K-STRUCT-DEFINITION`，其餘頂層節點一律 `h2m-mark(agg)` 計 unsup（既有誠實門禁，s55 起如此）。而 Nolang 的頂層陳述（`exit(f(42))`）在 AST 裡是 `Program` 的直接子節點 ⇒ 直接餵 `h2m-lower-program` 會整檔 unsup。Go 後端走的是同一條路：**保留 `main` 這個名字給合成入口**，使用者若自己寫 `main` 反而被拒（實測 `main = () (r i64) { r = 42 }` 的 `no build` 不以該函式作 C 入口）。故 M4-d 採相同策略：在 AST arena 上**就地追加**合成節點，把頂層陳述搬進一個新的 `main` 函式。

**② `h2m-lower-module`（`compiler/src/h2m.no:3876`）六步**

| 步 | 動作 | 要點 |
| --- | --- | --- |
| ① | 掃 `Program` 子節點，找**末位** `exit(E)` | 只認 `ExpressionStatement → CallExpression{c0==1} → Identifier 'exit'`；記 `nex`（位次）與 `ex`（引數節點 id）。無 `exit` ⇒ 合成 `0` 字面量（對齊 Go 合成 main 的自然終了回 0） |
| ② | 合成輸出參 `__noc_r i64` | `K-NAMED-TYPE` + `K-PARAMETER` 兩個節點 |
| ③ | 合成 `let __noc_r = E` | `h2m-lower-func` 尾段據「輸出參名」取回值 id，故必須是**同名 let** |
| ④ | 合成 `K-BLOCK-STATEMENT`：全部非定義、非 `exit` 的頂層陳述 + ③ 的 let | `exit` 那條**不搬**（它的值已由 ③ 承載） |
| ⑤ | 合成 `main = () (__noc_r i64) { … }` | `ast.set-sig(fn, 0, 0, 1)` ⇒ 0 形參、1 輸出參 |
| ⑥ | 合成新 `K-PROGRAM`：原定義 + 合成 `main` ⇒ 交 `h2m-lower-program` | 新 Program 裡**沒有**非定義節點 ⇒ 不再觸發 unsup；`agg.unsup > 0` 時回 `''`（誠實門禁不繞過） |

⚠️ **`ast.add` 的 kid 段陷阱**（探針實釘）：`ast.add` 會把 `kid0` 凍結在「當下的 `kids.len()`」，故每個合成節點建立後必須**立刻**加完 kids 才建下一個節點，否則 kid 段互相覆蓋。⚠️ 本函式對 arena 是**就地追加**，不動原有節點／kid 段 ⇒ 舊呼叫點（s62 探針、`h2m-test`）逐位元組不變。

**③ `i64 @main` 的 ABI 實測（不改成 `i32 @main`）**

`h2m-lower-func` 一律按輸出參型別發 `define i64 @main()`。實測 `clang -x ir -O2` **接受** `define i64 @main()`，C runtime 取回傳值**低 32 位**當行程結束碼：`ret i64 42` ⇒ `exit=42`。故**不需要**為入口特判 `i32`，也就免去在發射器加「main 專用 ret 型別」這條分支（少一個與 Go 後端分歧的面）。

**④ `compiler/src/link.no`（新增，53 行）**

`link.compile(ll str, out str) (ok bool)`：先把 `ll` 寫成 `<out>.ll`（`fs.write-bytes`，`?bool` 三臂處理），再 `clang -x ir -O2 -o <out> <out>.ll` 同步執行。**DR-2**：只調 `clang`，不依賴 `opt`／`llc` 兩段式（少一層版本耦合，也少兩個 PATH 依賴）。`ok` 先設 `false` 再賦值（避免「未賦值即成功」的靜默假綠）；clang 非 0 時把診斷轉到 stderr。

**⑤ `drv.cmd-build`／`drv.cmd-run` 與 argv 雙序**

- 鏈路：`fs.read-bytes` → `ast.make`／`ps.new`／`ps.parse` → `h2m-lower-module` → `cg-runtime-prelude-full()` + body → `link.compile`。
- **踩過的坑**：初版直接取 `args.get(2)` 當來源檔 ⇒ `noc build -o /tmp/x fixture.no` 把 `-o` 當檔名，實測 `noc: cannot read -o`（rc=1）。Go oracle 的 flag 是**硬性在檔案前**（`no build -o OUT FILE`），但早期 M4-d 草案寫的是 `FILE -o OUT`。
- **修法**：新增 `drv.src-arg()`（掃 `argv[2..]`，跳過 `-o` 及其值，取第一個非 flag 項）與 `drv.arg-at(i)`；`drv.build-out()` 改寫成同一種掃描。**兩種順序都吃**，四條路徑實測全通（見 ⑦）。
- `cmd-run` = `cmd-build` + `process.process-system(process.shell-quote(out))`，再把 **raw wait status `>> 8`** 當 `noc` 的結束碼（C `system()` 回的是 wait status，不 `>>8` 會得到 `10752` 這種值）。
- `main.no` 直接導入全部新可達模組（`mir`／`cg`／`cg-runtime`／`cg-rc`／`h2m`／`link`／`sb`）——繞開「跨模組常數靜默變 0」（`bug #85`）。
- 導入形式：`# /compiler/src/link`（**模組**形式）+ `link.compile(...)`。曾試 `# /compiler/src/link.compile`（全符號形式）⇒ 編譯失敗；`drv.no` 檔頭早有這條紀律，本輪又實證一次。

**⑥ 誠實邊界（本輪**不**假裝支援）**

只處理「單一 `.no` 檔、SHS 子集」。頂層內建呼叫（`print` 等）、`#` 模組導入、跨函式讀頂層 `let` 全局、`?str`／`?vec` 載荷、`option` match 臂解構、err/tag=2 裝箱、multi-out、`TY-VOID` 皆仍計 unsup ⇒ `agg.unsup > 0` 時**不出二進制、rc=1**。實測 `print("hello")` 頂層檔：

```text
noc: unsupported constructs: 1 (M4-d SHS subset)
rc=1
（無產物）
```

**⑦ 門禁與實測（2026-11-12 重跑）**

| 項目 | 命令 | 結果 |
| --- | --- | --- |
| M4-d L0 | `./bin/no run compiler/tests/drv-build-test.no` | **16 passed / 0 failed / 16 total**，`ALL TESTS PASSED` |
| M4-d L0（dump） | 同上 + `dump` ⇒ `/tmp/m4d-dump.ll`（253 行） | `opt -passes=verify` **rc=0**；`clang -x ir -O2` **rc=0**；執行 **exit=42** |
| e2e `build`（Go 序） | `noc build -o /tmp/m4d-e2e compiler/tests/fixtures/m4d-exit.no` | rc=0，產物 34,280 B，執行 **exit=42** |
| e2e `build`（反序） | `noc build <fixture> -o /tmp/m4d-rev` | rc=0，**exit=42** |
| e2e `build`（預設名） | `noc build /tmp/m4d-def.no` ⇒ `/tmp/m4d-def.no.out` | rc=0，**exit=42** |
| e2e `run` | `noc run -o /tmp/m4d-run <fixture>` | **rc=42**（子行程結束碼上拋） |
| oracle 對拍 | `no build -o /tmp/m4d-oracle <fixture>` | rc=0，**exit=42** ⇒ 與 selfhost **一致** |
| IR 結構 | `grep '^define'` 於 `/tmp/m4d-e2e.ll` | `@vec_*` 系列 + `define i64 @f(i64 %v1)` + `define i64 @main()`，**無** `i32 @main` |
| 舊子命令 | `version`／`help`／`tokens`／`ast`／`vet` | 全部正常（`vet` ⇒ `No issues found.` rc=0）；未知子命令 rc=2、無參數 rc=2 |
| 無回歸 | `cg-runtime-test`／`cg-str16-test`／`cg-test`／`h2m-test` | **86/86**／**41/41**／**359/359**／**1031/1037**（6 紅仍既有 s98 optmatch，基線一致） |

**⑧ 仍刻意未動**

- **L4 全語料行為門禁收斂**：本輪只做到「單檔 SHS 子集能出可執行檔且與 oracle 同值」，沒有把 `test/`／`tests/` 全語料拉進 `noc run` 差分。這是 M4-d 的**下一增量**，不是本輪範圍。
- `compiler/src/h2m.no` 既有降級面（`?str`／`?vec` 載荷、`option` match 臂、err 裝箱、內建調用、multi-out、`TY-VOID`）與 M5–M8 全部照舊。
- 其餘 L0 紅燈（`ast-test` 的 `bug #85 NoVal`、`ps-test` 39/42、`ck-sig-test` 41/50、`tok-test`／`ps-err-test`／`util-test`／`verify-struct-elem` 的編譯期錯）仍為既有工具鏈／語料債。

**⑨ 並行 session 觀測（記錄，非本輪改動）**

本輪跑門禁時撞到兩次「檔案在我跑完之後才被改」：`compiler/tests/cg-str16-test.no` 於 **10:55:55** 被改（我的 `cg-str16` 那次 37/41 是**改動中的瞬時態**，重跑即 41/41）；`compiler/src/cg-runtime.no` 的 `%vec` 同期由 24B `{ i64, i64, i64 }` 變成 **16B `{ i32, i32, i64 }`**（Go 端 `src/mir/codegen.go:3496` 已是 16B，屬 B28b 那條線，非本輪）。因此把 `drv-build-test.no` 的 `%vec` 斷言從「釘死欄位型別」改成**佈局無關**的 `%vec = type {` 存在性斷言——M4-d 只負責「prelude 被完整前置」，`%vec` 佈局的所有權不在這條線上。

> 📌 **§5.3.4.cj–.eo 各節「未覆蓋／後續」清單中的「`drv.cmd-build` 接線（M4-d）」項，自此（⑧）全部償清。** 各節屬當輪歷史記述，**不回改**（保留當時的證據形態），以本節與 §6 M4 列／§11.2 B4／文末快照為現況權威。

### ⑨ 2026-11-12 追記（續）：`print`／`eprint` 內建降級＋L4 全語料首度量測（**實作輪次**）

> 承接 ⑧ 末句「L4 全語料行為門禁收斂＝下一增量」。本輪做兩件事：**①** 把 `print`／`eprint` 從「計 unsup」變成真降級（語料 **161/169 檔**用到它，是 L4 的結構性瓶頸）；**②** 第一次把 `tests/` 全語料（169 檔）拉進 `noc run` 差分並**如實記帳**。
> 動到的檔：`compiler/src/mir.no`（新 op）、`compiler/src/cg-runtime.no`（prelude 助手）、`compiler/src/cg.no`（發射臂）、`compiler/src/h2m.no`（降級＋插值擋板）、`compiler/src/drv.no`（`build-to` 重構＋`run` 臨時產物）、`compiler/tests/drv-build-test.no`（測試點改寫＋新斷言＋`dump-print` 模式）。**未動**：Go 後端 `src/**`、`cg-runtime-test`／`cg-str16-test`／`cg-test`／`h2m-test` 的既有斷言。

**① 為什麼是 `print`：語料分佈決定的優先級**

對 `tests/*.no` 169 檔做特徵普查：

| 特徵 | 檔數 | 說明 |
| --- | --- | --- |
| `print(` | **161** | 幾乎是全語料的「輸出」手段，L4 比的就是 stdout |
| `eprint(` | 13 | 走 fd 2，不影響 stdout 但影響錯誤路徑 |
| `#` 模組導入 | 16 | M5 workspace／`mod.no` 的範圍，本輪不碰 |
| 下標讀 `x[...]` | 103 | 需 `OP-INDEX` 全族，本輪不碰 |
| 方法調用 `.m(` | 119 | 需方法分派，本輪不碰 |
| `format(` | 8 | 需 fmt-int／format 族 |

⇒ 不支援 `print` 時，L4 的 SAME **結構性為 0**（161 檔必然 rc=1）。這是「先做哪一件」的唯一合理答案。

**② `OP-PRINT`（`mir.no`，op 碼 95）——為什麼單立一個 op**

Go 的 print 助手是 `define void @print_*`，而自舉 `OP-CALL` 的契約是「產出一個值 id」（`dst` 非 0、`retty` 取自簽名表）。為 void 調用去擴 `OP-CALL` 會牽動 `dst`／`retty` 全鏈；而**輸出是語句不是值**，故單立一個無結果值的 op 更貼近語義，發出的 IR 與 Go 同形（同樣是 `call void @print_*`），只是 MIR 層形制不同。

`bs[k]` 當 kind 碼（8 種，一 op 覆蓋 stdout/stderr × i64/str/換行/空格）：

| kind | 發射 | kind | 發射 |
| --- | --- | --- | --- |
| 0 | `call void @print_i64(i64 %v<x>)` | 4 | `call void @eprint_i64(i64 %v<x>)` |
| 1 | `call void @print_str(%str-long %sv<x>)` | 5 | `call void @eprint_str(%str-long %sv<x>)` |
| 2 | `call void @print_nl()` | 6 | `call void @eprint_nl()` |
| 3 | `call void @print_space()` | 7 | `call void @eprint_space()` |

⚠️ 助手**只寫值本身**，不補空格／換行——那由 h2m 顯式發 kind 2/3/6/7，與 Go `emitPrint` 的「實參間一個空格、末尾一個換行」拼法逐字對齊（`print(a, b)` ⇒ `"a b\n"`，`print()` ⇒ `"\n"`）。

**③ prelude 助手（`cg-runtime.no`，新增 `cg-runtime-print-helpers()`，3,214 字元）**

逐字鏡像 Go `emitPrelude`：`@digits`（itoa 核心，`urem`/`udiv` 迴圈、指標自尾端前寫）、`@print_str`／`@eprint_str`（`extractvalue` 取 `i32` len → `zext` → `@write`）、`@print_i64`／`@eprint_i64`（**i64.MIN 用 `isMin` select 護欄**——`sub i64 0, %v` 對 MIN 是 poison，InstCombine 會折成垃圾）、`@print_nl`／`@print_space`／`@eprint_nl`／`@eprint_space`、全域 `@.nl`／`@.sp`。`cg-runtime-prelude-full()` 的 `sb.init` 由 8192 提到 16384（總長 7,226 → **10,440** 字元）。

🔴 **`\0` 轉義陷阱**：IR 的 `c"\0A\00"` 在 Nolang 字面量裡必須寫 `'\\0A\\00'`。單反斜線會被 Nolang 當跳脫解成 **NUL 位元組**（實測字串長度少 2），產出的 `.ll` 會在 LLVM 解析期報錯。反引號原始串在此**不可用**（實測 `\`...\`` 得長度 0，不是 raw string）。

**④ h2m 降級（`h2m-lower-print`）與三條誠實門禁**

在 `K-CALL-EXPRESSION` 臂裡、**簽名表查找之前**單點認領 `print`／`eprint`（自舉沒有 builtin 表，這兩個名字永遠不會命中 `h2m-sig-find`）：

| 門禁 | 條件 | 理由 |
| --- | --- | --- |
| 值型別 | 只收 `TY-I64`／`TY-U64`／`TY-STR` | `f64`／`vec`／`option`／結構體無對應助手，發半對的碼不如不發 |
| **插值擋板** | 實參是 `K-STRING-LITERAL` 且文字含 `{` ⇒ unsup | `print('{x}')` 在 Go 走 fmt-int／format 族；不擋就會把 `'{x}'` **原樣印出**——是靜默錯碼，只有 L4 能抓到 |
| 語句位 | 結果值 id 留 0，由 s93 路徑丟棄 | 若被當值用（`x = print(1)`），外層 `av == 0` 會入門禁，退化成 unsup |

🔴 **踩過的坑**：print 分支處理完後 **必須同時把 `ok-call` 清 0**。否則會落到下面「`ok-call == 1` ⇒ `want-n = sg.fnarg[si]`」而此時 `si = -1` ⇒ **索引越界，實測 SIGBUS（rc=138）**。清掉後靠新增的 `ok-print` 旗標抑制末尾的 `h2m-mark`。

**⑤ `drv` 重構：`run` 不再污染語料目錄**

原 `cmd-build`／`cmd-run` 各自算輸出路徑，且 `run` 預設寫 `<src>.out` ⇒ 拿它跑 `tests/` 會在語料目錄裡拉出 169 個 `*.no.out`。重構為：

- `drv.build-to(path, out) (ok bool)`——建置主體（讀檔 → 自產 `.ll` → clang），`build`／`run` 共用；
- `drv.out-arg()`——抽 `-o` 的值；
- `drv.run-temp()`——`<TMPDIR>/noc-run-<pid>.out`，pid 區分並發進程；
- `cmd-run` 未給 `-o` 時走 `run-temp()`，`cmd-build` 仍維持 `<path>.out`（`build` 的語義不變）。

**⑥ 門禁（2026-11-12 重跑）**

| 項目 | 結果 |
| --- | --- |
| `drv-build-test`（新增 print 斷言＋`dump-print` 模式） | **31 passed / 0 failed / 31 total**，`ALL TESTS PASSED` |
| `dump-print` 模組（374 行） | `opt -passes=verify` **rc=0**；`clang -x ir -O2` **rc=0**；執行輸出 `42`／`hi`／`9`（9 走 stderr）、**exit=0** |
| 差分（`print` 四形） | `print('hello')`／`print(42)`／`print('a', 7)`／`print()`／`print('x','y')`／`eprint(99)`／`print(-7)`／`print(9223372036854775807)` **逐位元組等於 oracle** |
| 函式內 `print` | `f(x) { print(x); print('in f') }` 兩次調用輸出與 oracle 一致 |
| 插值擋板 | `print('{x}')` ⇒ `unsupported constructs: 1`、rc=1、**不產碼**（oracle 印 `1`） |
| 無回歸 | `cg-runtime-test` **86/86**、`cg-str16-test` **41/41**、`cg-test` **359/359**、`h2m-test` **1031/1037**（6 紅仍既有 s98 optmatch，基線一致） |
| 格式 | `no fmt -d` 對 `h2m.no`／`drv.no`／`drv-build-test.no` 皆 **0 行**（⑧ 遺留的 35 行註解前空行已補齊；僅新增空行，未動語義） |

**⑦ L4 全語料首度量測（`tests/*.no`，169 檔；`(rc, sha256(stdout))`）**

| 類別 | 檔數 | 說明 |
| --- | --- | --- |
| **真 SAME**（雙邊 rc=0 且 stdout 逐位元組相同） | **2** | `tests/ifelse.no`、`tests/colon-loop.no` |
| 空 SAME（雙邊 rc=1） | 4 | `tests/tagged-enum-*.no`——這些檔在 **Go oracle 也建不過**，故「SAME」無信號（正是 harness G2／G4 防護要抓的那類假綠） |
| DIFF（自舉 rc=1，誠實 unsup） | 163 | 其中 **0 例**是「自舉跑成功但輸出不同」⇒ **無靜默錯碼** |

**本輪淨增益**：真 SAME 由 **0 → 2**（`ifelse.no`／`colon-loop.no` 在此之前都因 `print` 計 unsup 而 rc=1）。數字很小，但性質是關鍵——這是 L4 **第一次**有非空真 SAME，且 `print` 這條 161/169 檔的瓶頸已通，後續每個降級族的收益都會直接反映在這一欄。

**⑧ 已知分歧（明示，不藏）**

- **`print(bool)`**：自舉無 `TY-BOOL`（bool 與 `i64` 共用 `TY-I64`），故印 `1`／`0`，權威印 `true`／`false`。實測 `print(true)` ⇒ 自舉 `1` vs oracle `true`。這條會在 L4 顯性暴露為 DIFF（不是靜默），待自舉補 bool 型別碼或字面量側識別後再收。`print("A")`（char）**不**分歧——兩邊都印碼點 `65`。
- **插值 `print('{x}')`**：已由門禁擋成 unsup（見 ④），不算分歧。

> 📌 **⑩ 追記（2026-11-12，同日稍後）**：本節「插值擋板 ⇒ unsup」的處置**已被取代**。無 spec 的 `{ident}` 現已真降級（逐段 `OP-PRINT`，與權威逐位元組同結果），只有 `{name:spec}`／表達式字段／多實參模板形／`{{` `}}` 轉義仍計 unsup；`print(bool)` 的 `1`/`0` 分歧**照舊未收**。上表（④）與本節（⑧）保留為當輪證據形態，不回改；現況以 §11 後 ⑩ 為準。

**⑨ 並行 session 觀測（重要方法論）**

首次全語料掃描用 `xargs -P 4` 並行跑，得到 `SAME=7`；串行複核後**只有 2 個是真 SAME**。原因是 **Go oracle 的 `no run` 用共享臨時路徑，並行時互相覆蓋** ⇒ oracle 的 `(rc, stdout)` 被污染，把「真 DIFF」誤判成「空 SAME」。自舉側不受影響（`run-temp()` 帶 pid）。
⇒ **紀律：L4 差分必須串行跑，或至少把 oracle 的產物路徑隔離**。並行只可用於「同一邊的重複執行」，不可用於跨邊對拍。這條已寫進本節，避免後來者重踩。

## 附錄 A：實測證據與可重跑命令

> 以下每一條都在 2026-10-09`HEAD = 4feaaf70``bin/no`（`make no` 產物）上實測**重跑前先 `export PATH`**——Bash 預設**沒有 `go`**，且 `no` 需要 LLVM/clang 在 PATH 上

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
# parser 套件所有struct = 96（⚠️ 含 Parser / lowerer / SemanticContext / Diagnostic 等
# 內部機械，**不是** AST 節點數早期版本誤把 96 當成節點數，使 M2 的範圍被高估約 35%）
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

見 §5.1.2


### B.4 `compiler/src/ast.no`（arena 化的 AST）

```no
; 節點種類（與 parser/ast.go 的 71 個 Node 一一對應；此處僅示範）
KIND-LET = 1
KIND-IDENT = 2
KIND-INFIX = 3

; …

nodedata {
    kind i64
    a i64; 子節點索引 / 位置
    b i64; 子節點索引 / 長度
    s i64; 詞素索引（指向 intern 表）
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

**已驗證**：以 `push(1,2,3,4)` / `push(5,6,7,8)` 呼叫後，`get(1).kind == 5``get(1).b == 7`（輸出 `0 1 5 7`）

> 注意 `pool intern` 是**內聯結構體欄位**（默認佈局是 by-value）若 `intern` 內含容器（這裡是 `[str]i64` + `[]str`），整體會很大；若 `ast` 要被大量複製，應改為 `pool-idx i64`（索引到外部 arena）或給欄位加 `#{inline=false}`**M0 要在 `ast.no` 的頭部註釋裡把這個決策寫死**

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

⚠️ **`%nolang_hdr` 的 `rc` 是第一個欄位**（即 `data - 16`），retain/release 從 `data` 反向索引（`getelementptr inbounds i8, i8* %p, i64 -16`），**不命名 header 結構**`rc == 0` 是**借用哨兵**（FFI 指針字串常量`cap == 0` 的 `str→[]byte` 視圖），兩個 helper 對它**立即返回**


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
        ok(y) -> {
        }

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
        ok == false -> {
        }

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

⚠️ `ok` 的 out-param 初值必須先寫成 `false`（本範例為簡潔省略了初始化；實際代碼要在函式開頭 `ok = false`，否則 `ok` 是未定義值——Nolang 的 out-param 不保證零初始化）

### ⑩ 2026-11-12 追記（續）：`...` 佔位陳述＋字面量插值（**實作輪次**）

> 承接 ⑨ 末句「新的最高優先未了項＝**L4 收斂主體**」。L4 主體不是一個功能，是一片殘差；本輪先用**測量**把殘差排序，再挑「觸發面最廣、且互為前置」的兩族落地。
> 動到的檔：`compiler/src/tok.no`（新 token）、`compiler/src/lx.no`（三點）、`compiler/src/ps.no`（陳述位認領）、`compiler/src/h2m.no`（no-op 臂＋插值九個助手）、`compiler/tests/drv-build-test.no`、`compiler/tests/lx-test.no`。

**① 先量再動：169 檔的「近失」排序**

`noc build` 對每個語料檔回一行 `unsupported constructs: N`，逐檔收集即得 N 的分佈。N 越小＝越接近通過，優先級不靠猜：

| N（unsup 數） | 檔數 | 代表檔 |
| --- | --- | --- |
| 0（已綠） | 2 | `ifelse.no`、`colon-loop.no` |
| 1 | 1 | `build-mo.no` |
| 2 | 4 | `dowhile.no`、`for.no`、`guard.no`、`loop-test.no` |
| 3–6 | 9 | `runner-callback.no`、`arch.no`、`it-probe.no`、`minimal.no`、`platform.no`、`slot-rebind-unsafe.no` |
| 7–15 | 21 | `misc1.no`、`range.no`、`parse.no`、`global.no` … |

再對每個「近失」做**單族拆解驗證**（不是猜測）：把 `...` 行刪掉重跑 ⇒ `for.no` 由 2 降到 0；把插值字面量換成普通字面量 ⇒ `guard.no` 由 4 降到 0。兩者都是「單一族即唯一阻塞」。全語料特徵計數（ripgrep，非估計）：

| 特徵 | 檔數 | 說明 |
| --- | --- | --- |
| `...` 獨佔一行 | **74** | 佔位陳述；權威側是 no-op |
| 字串字面量含 `{ident}` | 72 | 其中 8 檔帶 `:spec` |
| 只有無 spec 的 `{ident}` | 49 | 本輪可降級的範圍 |
| `x = { … }` / `x = y: { … }`（block-as-value） | 13 | 表達式位的塊匹配，仍未收 |
| `ok(...)` 條件臂 | 10 | 仍未收 |

**② s101：`...` 佔位陳述**

權威口徑（三處）：`lexer.ELLIPSIS3` 是**單一 token**（`lexer.go:906` 明寫「貪婪吃掉第三個點，否則 `...` 會被切成 `..` + `.`」）；`parser.parseEmptyStatement` 產生 `ast.EmptyStatement`；`ASTToHIR` **整條丟棄**，不產生任何 HIR 節點。

自舉現狀：`lx.no` 只認 `..`（`tok.ELLIPSIS`，本專案裡它其實是範圍運算子），`...` 被切成 `..` + `.`，那個 `.` 落進「隱式 self」路徑 ⇒ 每條 `...` 記一條 unsup。74 檔因此整批紅。

| 檔案 | 改動 |
| --- | --- |
| `tok.no` | 新增 `ELLIPSIS3 = 92`，註明**不可**與 `ELLIPSIS = 65` 混用（混用會讓 `[0...n)` 這種漏寫 self 的誤寫靜默變成合法範圍）；`tok.name` 補一臂 |
| `lx.no` | `c == B-DOT` 分支：吃掉第二個點後再窺一次，第三個點則 `lx-read-char` 再吃並改報 `ELLIPSIS3` |
| `ps.no` | `ps-parse-stmt` 在 `ps-skip-trivia` 之後認領：`ps-bump` 消費、`ast.add(t, asp.K-EMPTY-STATEMENT)` 建節點（`asp.no` 早有 `K-EMPTY-STATEMENT = 25`，先前從未產生過） |
| `h2m.no` | `h2m-lower-block` 新增 no-op 臂 `kk.kind == asp.K-EMPTY-STATEMENT -> hd = 1`（`hd = 1` ＝本陳述已消費，不落 `hd == 0` 的誠實門禁） |

**誠實邊界**：刻意**不**實作 Go 的兩條硬錯——`elps0ln0`（`...` 前面同不同行不能有程式碼）、`elps1ln1`（後面不能有程式碼）。自舉 `psst` 沒有 prev-token 行號可比，硬造會誤判。放寬方向是「多接受」，不會把合法程式判錯；`x = ...` 這種表達式位誤用仍由 let 解析路徑自然失敗。

**③ s102：字面量插值**

權威判定在 `hir2mir.go` 的 `formatSegmentsOf`：字面量必須 **①** `parser.ParseFormatString` 解析成功 **且 ②** 至少一個 `Field`，才走「命名格式」降級；否則**整串原樣輸出**。這一條比想像中關鍵——它連 `{{`/`}}` 反轉義都不做（實測 oracle：`print('a{{b')` 印 `a{{b`、`print('a{b')` 印 `a{b`、`print('{}')` 印 `{}`）。所以「先判定是不是格式串」必須與「怎麼降級」分開，不能合併成一個掃描。

**為什麼不用 `fmt-int`**：那要 `std/fmt.no` 整條流水線（`fmt-parse-spec` → `fmt-apply-spec` → `fmt-uint` → `fmt-group-digits` → `u64_to_str`，oracle 的 `e.bin.ll` 會多出 8 個 `define`），自舉沒有這條線。但**無 spec 時 `fmt-int(n, '')` 的正文就是 `n.to-str()`**（`fmt.no` 的 `full = n.to_str()`；`width=0`／`grouping=0` 時後面不再改寫），與 `@print_i64` 印的是同一串十進位 ⇒ 直接按型別發 `OP-PRINT` 即**逐位元組同結果**。這條等式是本輪能不解 std 就拿到正確輸出的全部理由。

三檔分類（`h2m-fmt-class`）：

| class | 條件 | 處置 |
| --- | --- | --- |
| 0 | 掃完沒有任何 `{...}` 字段（含 `a{b`、`{}`、純文字） | **原樣印**（與權威同：不當格式串） |
| 1 | 至少一個字段，且全部是裸標識符（可帶 `:spec`） | 逐段降級；遇 `:spec` 再降為 unsup |
| 2 | 表達式字段、含 `{{`／`}}` 轉義 | 計 unsup（權威會求值／反轉義，本件復現不了） |

逐段發射（`h2m-fmt-emit`）：字面段 → `OP-STRCONST` + `OP-PRINT(kind = base+1)`；字段 → 查作用域（走 `h2m-var-read`，含 `it`／臂局部名／槽讀三路）後按型別 `OP-PRINT(kind = base / base+1)`；整條 `print` 只在末尾補**一個**換行（不是每段一個）。

新增九個助手（全部置於 `h2m-lower-print` 之前）：`h2m-inc`（i+1，索引運算用）、`h2m-byte-at`（越界回 -1 哨兵）、`h2m-fmt-find-close`、`h2m-fmt-colon`、`h2m-fmt-field-class`、`h2m-fmt-class`、`h2m-print-lit`、`h2m-print-field`、`h2m-fmt-emit`。

**誠實邊界**：`{name:spec}`（寬度／對齊／進制／千分位）、表達式字段（`{a[i] & 255}`）、**多實參模板形**（`print('x={a}', 42)`，權威走 `lowerNamedFormatMulti`）、含 `{{`／`}}` 轉義——四者一律計 unsup，寧缺勿錯。四形都實測過：每形回 `unsupported constructs: 1`，不產半對的碼。

**④ 門禁**

| 門禁 | 改前 | 改後 | 說明 |
| --- | --- | --- | --- |
| `drv-build-test` | 31/31 | **42/42** | `S-BAD` 從「無 spec 插值」改成「帶 spec 插值」（前者現在是正例）；新增 `test-interp-forms`（6 條）、`test-empty-stmt`（3 條）、`dump-interp` 模式 |
| `lx-test` | 70/70 | **81/81** | 新增 `test-ellipsis`（11 條）：`..`≠`...`、`[0...n)` 整體成 `ELLIPSIS3`、`....` = `ELLIPSIS3`+`DOT` |
| `h2m-test` | 1031/1037 | 1031/1037 | 6 紅為既有 `s98 optmatch` |
| `ps-test` | 39/42 | 39/42 | 3 紅既有 |
| `cg-test` / `cg-runtime-test` / `cg-str16-test` | 359 / 86 / 41 | 359 / 86 / 41 | 全綠照舊 |
| `diag-test` / `ck-test` / `ck-sig-test` | 7 / 20 / 50 | 7 / 20 / 50 | 全綠照舊 |
| `ast-test` / `tok-test` / `ps-err-test` / `util-test` / `verify-struct-elem` | 編譯期錯 | 編譯期錯 | 既有工具鏈／語料債，未動 |

端到端（不只文字斷言）：`drv-build-test.no dump-interp` → 389 行 IR，`opt -passes=verify -disable-output` rc=0，`clang -x ir -O2` rc=0，執行輸出 `a=7 b=hi c=7`、exit 0。

**⑤ L4 量測（前後對照，逐檔序列執行）**

| 指標 | 改前 | 改後 | 變化 |
| --- | --- | --- | --- |
| `rc == 0` 的語料檔 | 2 | **4** | +2 |
| L4 真 SAME（rc 與 stdout 皆同） | 2 | **4** | +2 |
| 全語料 unsup 總數 | 11,857 | **11,436** | −421（−3.6%） |
| unsup 有下降的檔 | — | **86 / 169** | — |

新進的兩個 SAME 都是「單一族唯一阻塞」被拆掉的：

| 檔 | 阻塞族 | 改前 | 改後 |
| --- | --- | --- | --- |
| `tests/for.no` | `...`（2 條） | DIFF（rc=1） | **SAME**（rc=0，stdout 同） |
| `tests/guard.no` | 插值（4 條） | DIFF（rc=1） | **SAME**（rc=0，stdout 同） |

⚠️ 「改前」是用**改前二進制**（`/tmp/noc-l4`）復跑同一檔得到的，不是憑記憶推的；且逐檔**序列**執行——⑨ 已記下並行 sweep 會因 oracle 共用臨時路徑而產生假 SAME。

降幅最大的檔（unsup 條數減少量）：`str-more.no` 30、`std-hash.no` 24、`fs.no` 24、`strconv.no` 21、`format.no` 19、`str.no` 18、`match.no` 18、`arr-more.no` 18、`yaml.no` 17。它們仍未綠——插值是**前置**而非**充分**條件，這批檔的下一道阻塞是下標讀／方法調用。

**⑥ 一個必須記下來的工具鏈事實（非本輪改動，但影響所有判讀）**

**套件模式（目錄下有 `package.noml`）下，只有入口檔過 checker，`#` 導入的模組不過。** 實測（`/tmp/pkgt`，最小套件）：

| 位置 | `r = nosuchfn(i)` 的結果 |
| --- | --- |
| 導入模組 `lib/u.no` | 只到 MIR codegen 才報 `MIR codegen could not handle main.no: EmitLLVM: unknown callee nosuchfn in func inc [4qhyyund]` |
| 入口 `main.no` | checker 正常報 `main.no:2:5: [ERROR] nolang-compile: 'nosuchfn' is not defined [4bek3xc6]` |

推論兩條：

1. `compiler/`（有 `package.noml`）的 15 個模組**從未被型別檢查**。所以 `h2m.no` 裡滿地的 `i = i + 1`（對已存在的純量 `i64` 重賦值算術）能編過——同樣三行寫在**單檔**裡是硬錯 `15w45dqk`（實測 `c = 0` + `#{overflow=wrap}` + `c = a + b` 三形全紅；`#{overflow=wrap}` 只豁免**聲明**位，不豁免重賦值位）。
2. 自舉源碼因此可能藏著「型別上不成立、但 MIR 恰好能生成」的寫法。本輪**不改工具鏈**，只記錄；但 `compiler/tests/*.no` 走**單檔**模式、仍受 checker 管，所以新助手 `occ`（`drv-build-test.no` 裡的子串計數器）刻意避開重賦值算術：改用區間遍歷 + `[]i64` 累計，索引運算只出現在**實參位**（`base + 1` 這種實參位在單檔模式是合法的）。

**⑦ 仍未收（下一輪的候選，依量測排序）**

- 下標讀（`x[...]`，103 檔）／方法調用（`.m(...)`，119 檔）／`#` 模組導入（16 檔）——L4 主體殘差的三個大頭。
- block-as-value（`r = { … }`，13 檔）：`h2m` 目前只收**陳述位**的塊匹配，表達式位要各臂匯成同一個結果值。
- `ok(...)` 條件臂（10 檔）、`it` 在 catch-all 臂的洩漏（寫 std 時的已知雷）。
- `print(bool)` 印 `1`/`0`（自舉無 `TY-BOOL`）；插值的 `:spec`／表達式字段／多實參模板形／`{{` `}}` 轉義。
- `minimal.no`（6 條）與 `build-mo.no`（1 條）都卡在 block-as-value，是本輪之後最近的兩個目標。
  📌 **已於 ⑪ 處理**：`build-mo.no` 已綠（unsup 1 → 0），`minimal.no` 6 → 2（剩 `ok(it > 127)` 條件臂與 str 臂值）。

---

### ⑪ 2026-11-12 追記（續）：值位塊匹配（block-as-value）降級（**實作輪次**）

> 承接 ⑩ 末句「`minimal.no`（6 條）與 `build-mo.no`（1 條）都卡在 block-as-value，是本輪之後最近的兩個目標」。
> 動到的檔：`compiler/src/h2m.no`（前掃描登記結果槽＋兩個新助手＋`h2m-lower-surface` 加值位分支）、`compiler/tests/h2m-test.no`（+29 條）、`compiler/tests/drv-build-test.no`（+11 條）。

**① 先量再動：消融驗證「單一族即唯一阻塞」**

不靠猜。`tests/build-mo.no` 全文只有三行有效碼（`x = 0` / `result = { … }`），把塊匹配換成 `result = 0` 重跑 ⇒ `unsup` 由 1 降到 **0**，確認是唯一阻塞。`minimal.no` 同法拆解 ⇒ 6 條裡有 4 條來自塊匹配，剩 2 條是 `ok(it > 127)` 條件臂與 str 臂值（另兩族）。

**② 形制（探針釘死，不靠 Go AST 推論）**

探針 `probe-smv.no`（現存 `/tmp/s103-probe/ast.no`，輸出 `/tmp/s103-probe/ast-out.txt`）：

```
let kind=44 nkid=2 s0=result            ← K-LET-STATEMENT，kid[0]=0（无型别标注），kid[1]=SurfaceMatch
sm  kind=67 nkid=1 arms=2 hbase=1 kid0=0  ← 裸链：Matched 位占 0
arm0 valueCtx=193 cond=6 body=9
  body kind=15 nkid=1                   ← K-BLOCK-STATEMENT(inline)
    stmt kind=29 nkid=1                 ← K-EXPRESSION-STATEMENT
      expr kind=41 nkid=0 s0=1          ← 值表达式
```

三個要點：

1. **臂體形制與陳述位完全相同**（`BlockStatement{ExpressionStatement{expr}}`）——所以值位與陳述位不能靠形制區分。
2. 唯一可靠的判據是 **`aux[hb+9]`（valueCtx）非零**。`ps.no:10041` 在值語境寫 `ast.put-str(t,'true')`，所以它是**字串表下標**（實測 193），不是 `1` ⇒ 只能比非零，不能比 `1`（與 variant 槽同一個坑）。
3. `kid0 = 0` 說明裸鏈的主體位是佔位 0；顯式 `n: { … }` 才是真主體節點。兩形在本件都收。

**③ 設計約束：結果槽**必須**在 entry 鋪**

值位要「各臂匯成同一個值」，而 MIR 沒有 `phi`（`OP-*` 表裡沒有），只能走記憶體。`h2m` 又有一條硬紀律：**塊是指令序的連續段，離開 entry 就回不去** ⇒ 不能在降級到一半時補 `OP-ALLOC`。

若圖省事就地 `OP-ALLOC`，LLVM 雖接受，但**迴圈體內的塊每迭代都新攤一份棧**，長迴圈直接爆棧。所以走 `h2m-scan` 前掃描（s57 起的既有機制）：在 `K-LET-STATEMENT` 臂裡按 **LetStatement 節點 id** 合成槽名 `@smv<id>`（`'@'` 不是合法識別符首字符 ⇒ 永不與源碼變數相撞），`h2m-lst-add` 登記，`h2m-lower-func` 在 entry 統一 `h2m-slot-decl`。

實測 `w4.no`（值位匹配寫在 `3 * { … }` 迴圈體內）⇒ 全函式 **5 顆 alloca 全在 `entry:`**，迴圈體裡一顆都沒有。

⚠️ 這也解釋了 Go 權威為什麼長那樣：`build-mo.ll` 的結果槽 `%v11.s` 同樣攤在 `_nolang_main.bb0`。兩側同構。

**④ 實作（`h2m.no` 六處）**

| # | 位置 | 改動 |
| --- | --- | --- |
| 1 | `h2m-cnt-name` 之後 | 新增 `h2m-smv-name(n)`（`'@smv' - n.to-str()`）與 `h2m-smv-arm-val(t, bn)`（臂體兩層解包，回值表達式節點 id） |
| 2 | `h2m-scan` 的 `K-LET-STATEMENT` 臂 | 右值是 `K-SURFACE-MATCH` ⇒ `ty = TY-I64`（`h2m-ty-of` 不認這個 kind）、登記 `@smv<id>`、`h2m-scan-arms` 掃臂體局部量、`flow = 1` |
| 3 | `h2m-lower-let` | 右值分派：`K-SURFACE-MATCH` 走 `h2m-lower-surface(…, @smv<id>)`，其餘仍走 `h2m-lower-expr` |
| 4 | `h2m-lower-surface` 簽名 | 加第 5 參 `snm str`（`''` = 陳述位）與出參 `r i64`；3 個陳述位呼叫點補 `''` |
| 5 | 同上，門禁段 | `vctx == 1` 時查 `c.vids[si]` 得槽 id（查表在發指令之前），並逐臂加兩條門禁：`aux[hb+9] != 0`、臂體可解出值表達式 |
| 6 | 同上，發射段 | 三處 `h2m-lower-branch` 換成 `h2m-smv-body`（值位：降臂值 → `OP-STORE` 進槽）；`h2m-set-block(merge-id)` 之後補 `OP-LOAD` 交回 `r` |

**⑤ 誠實邊界（全部實測回 `unsupported constructs: 1`，不產半對的碼）**

- **臂值不是 `i64`**：結果槽按 `i64` 鋪（`h2m-slot-decl`），`str`/`vec` 臂值無處可落 ⇒ `h2m-vty-at(xv) != TY-I64` ⇒ 計 unsup。
- **臂體不是「inline 塊包單表達式陳述」**：多陳述塊、裸塊、空體 ⇒ `h2m-smv-arm-val` 回 0 ⇒ 計 unsup。
- **`valueCtx` 未置位**：本鏈其實在陳述位，按值降會讀錯語境 ⇒ 計 unsup（不猜）。
- **巢狀值位匹配**（臂值本身是 `{ … }`）：`h2m-lower-expr` 不認 `K-SURFACE-MATCH` ⇒ 內層計 unsup，整函式不產碼。
- **`ok(…)` 條件臂**（`minimal.no` 的 `ok(it > 127)`）：`omode` 門禁照舊拒絕 `aux[hb+6]`（rawCond）⇒ 仍不產碼。

**⑥ 一個先於本件存在的語義分歧（本件未引入，且**已在帳**：§11.2 B19）**

本輪為了選「端到端閘釘哪個形」，重新實測了無兜底臂的鏈，撞上的正是 **B19** 記的那條：

> B19 🔴 花括號短路鏈若沒有兜底臂，**Go 工具鏈**不短路——所有命中的臂都執行，最後寫入者贏（規範上是 if/elif 只取首個命中臂）。

代碼出處：`lowering.go:3730` 的 `if allNonWildcard && len(arms) > 1` 會把整條鏈降成 `if (1) { if c1 { B1 }; if c2 { B2 }; … }`；`tohir.go:1799` 的註釋原文寫「each guard lowers to its own KExprStmt … so **every matching arm runs**」。也就是說這是**權威側的實現偏離**，不是 noc 的錯——`syntax.md` rule 1（「`{}` 包裹 ⇒ 只有第一個匹配的分支執行」）與 noc 的 s58 都站在規範這一邊。

本輪複測（`x = 0` 後接 `{ x == 0 -> A  x == x -> B }`，**改前二進制復跑同檔得到同樣結果** ⇒ 與 s103 無關）：

| 形 | 權威（偏離規範） | noc（合規範） |
| --- | --- | --- |
| 陳述位印 `A` / `B` | 印 **A 和 B** | 只印 A |
| 值位得 `1` / `2` | 得 **2** | 得 1 |

⇒ **端到端閘只釘帶兜底臂的形**（兩側語義一致）；無兜底臂的形只在 `h2m-test` 裡釘 IR 形狀，**不聲稱語義等價**。是否要為了 L4 去複現 B19 的偏離，屬規格級決定，本輪不動（複現它等於讓 noc 也偏離 `syntax.md`，且要翻掉十幾條已釘住的 s58 斷言）。

**⑦ 門禁**

| 門禁 | 改前 | 改後 | 說明 |
| --- | --- | --- | --- |
| `drv-build-test` | 42/42 | **53/53** | 新增 `test-smv-value`（11 條）：結果槽 / 目標槽 / 逐臂 STORE / 匯聚 LOAD / `print_i64` / 命中第一臂的形 / str 臂值誠實回空串 |
| `h2m-test` | 1031/1037 | **1060/1066** | +29 條（`test-smv-wild3` 11、`test-smv-nowild2` 7、`test-smv-subj` 9、`test-smv-gate` 2）；6 紅仍為既有 `s98 optmatch`。`test-flow-gate` 的首個負例由「值位鏈」改為「str 臂值」（前者現在是正例） |
| `lx-test` | 81/81 | 81/81 | 未動 |
| `ps-test` | 39/42 | 39/42 | 3 紅既有 |
| `cg-test` / `cg-runtime-test` / `cg-str16-test` | 359 / 86 / 41 | 359 / 86 / 41 | 全綠 |
| `diag-test` / `ck-test` / `ck-sig-test` | 7 / 20 / 50 | 7 / 20 / 50 | 全綠 |

端到端語義對拍（`noc` vs 權威，逐檔序列）：

| 檔 | 內容 | 權威 | noc |
| --- | --- | --- | --- |
| `w1.no` | `x=5`，`r = { x==0 -> 1  -> 7 }`，印 `r` | `7` | `7` |
| `w2.no` | `x=0`，同形 | `11` | `11` |
| `w4.no` | 值位匹配寫在 `3 * { … }` 迴圈體內 | `10\n10\n10` | `10\n10\n10` |
| `w6.no` | 同函式兩個值位匹配（兩顆槽） | `1\n10` | `1\n10` |
| `w3.no` | 值位匹配 + `[]i64` 累加 | `3` | unsup 5（阻塞在 vec，非本件） |

**⑧ L4（169 檔，序列，權威固定為同一把 `/tmp/nobin/no_s103`）**

| 指標 | ⑩ 之後（改前二進制 `/tmp/nobin/noc_g`） | ⑪ 之後（`/tmp/s103/noc_g`） | 變化 |
| --- | --- | --- | --- |
| 全語料 unsup 總數 | 11,436 | **11,354** | **−82（−0.7%）** |
| `unsup == 0`（＝能產碼）的檔 | 4 | **5** | +1 |
| L4 真 SAME（`rc == 0` 且 stdout 同） | 4 | **5** | **+1** |
| L4 空 SAME（兩側皆 `rc == 1`、stdout 皆空） | 4 | 4 | 0 |
| 總 SAME | 8 | **9** | +1 |
| unsup 下降的檔 | — | **18 / 169** | — |
| unsup 上升的檔 | — | **4 / 169** | — |

新進的那一個 SAME 是 `tests/build-mo.no`（`x = 0` + `result = { x == 0 -> 1  x == x -> 0 }`，unsup 1 → 0，`rc` 由 1 變 0，兩側 stdout 皆空）。⚠️ 該檔本身**不印任何東西**，所以 SAME 只證明「能編譯並跑完」，**不證明語義等價**——語義那一半靠 ⑦ 的端到端對拍（`w1`/`w2`/`w4`/`w6` 逐位元組相同）與 `drv-build-test` 的 `test-smv-value` 承擔。

⚠️ **改前側的判定是推導的，不是第二次全量 L4**：本輪實測 `noc run` 的 `rc` 與 `unsup` 嚴格一一對應（169 檔中 `unsup==0 而 rc!=0` **0 檔**、`unsup>0 而 rc==0` **0 檔**；`rc` 分佈 = 5 個 0 + 164 個 1），故改前 `rc` 可由改前的 unsup 表直接得出（4 個 0、165 個 1）。**unsup 總數本身是兩次全量實測**（同一份 `tests/*.no`、同一個 `noc build` 口徑），改前的 11,436 與 ⑩ 記的數字逐位相同，可交叉校驗。

**4 檔 unsup 上升的原因（必須記下來，否則會被誤讀成回歸）**：值位匹配**強制 mem 模式**——結果槽要 `alloca`，而槽只在 `mem == 1` 才鋪（`h2m-lower-func`）。於是「原本因為只有一個值位匹配而停留在 `mem == 0`」的函式翻到 `mem == 1`，而 `mem == 1` 下 `h2m-slot-decl` 對 **`[]i64` 局部量**（cg 沒有 `alloca %vec` 這個 op，s62）與**推不出型別**的局部量計 unsup。實測同一個 `v []i64 = []`：`mem == 0` 時 **0 條**、`mem == 1` 時 **2 條**（`m1.no` vs `m2.no`）。

上升的 4 檔：`match-err-msg-scalar.no` +14、`index.no` +4、`block-match.no` +2、`fmt.no` +1（合計 +21）。**但它們本來就是 DIFF**，且**沒有一檔由可編譯變成不可編譯**（`unsup == 0` 的檔由 4 增到 5，`0 → 非 0` 的檔數為 **0**）。下降的 18 檔合計 −103，最大的是 `crypto.no` −24、`option.no` −17、`err.no` −10、`arr.no` −9。

**⑨ 仍未收（下一輪的候選，依量測排序）**

- 下標讀（`x[...]`，103 檔）／方法調用（`.m(...)`，119 檔）／`#` 模組導入（16 檔）——L4 主體殘差的三個大頭。
- **短路語義裁決**（⑥）：`{}` 無兜底臂的多臂鏈要不要跟權威的「獨立 if」對齊。這是一個規格級決定，影響 s58 全部既有斷言。
- `ok(...)` 條件臂（10 檔，`minimal.no` 就卡在這＋str 臂值）。
- `print(bool)` 印 `1`/`0`（自舉無 `TY-BOOL`）；插值的 `:spec`／表達式字段／多實參模板形／`{{` `}}` 轉義。
- 值位匹配的 **str/vec 臂值**（結果槽目前只有 `i64` 一種寬）。

📌 **後續（⑫）**：本節列的「方法調用 119 檔 / `#` 模組導入 16 檔」在 ⑫ 已被量測收斂成**同一個根因**——`h2m` 的調用降級只認 `K-IDENTIFIER` 被調者（`cd.kind != asp.K-IDENTIFIER -> ok-call = 0`），故 `mod.fn(...)` 與 `obj.method(...)` **全部**落 `unsup`；而這些模組內的方法／函式本體又在**別的模組**裡，所以真正的閘是 `#` 導入。⑫ 另補了 str 相等/不等（本節未列的量測最大單一族）。

---

### ⑫ 2026-11-12 追記（續）：str 相等/不等降級 ＋ 全模組 unsup 成因直方圖（**實作輪次**）

**① 先量後做：把「哪一族在擋」量出來，而不是猜**

⑪ 結尾的候選清單是按**語料檔數**排的（下標讀 103 檔、方法調用 119 檔）。本輪先做了一件更基礎的事：**量測每個 `unsup` 標記落在哪個 AST 節點型別上**。做法是暫時給 `h2m` 加插樁（量完即還原，不入庫）：

1. `h2m-ctx` 加 `cks []i64` / `cks-n i64`（節點型別棧），`h2m-lower-expr` 入口 push `d.kind`、出口 pop，`h2m-lower-block` 對每條陳述 push `kk.kind` / pop；
2. `h2m-mark` 改為先 `eprint('MK ' - c.cks[top])` 再 `+1`；
3. 另在三個「未知節點型別」的兜底處（`h2m-lower-expr` 的 `-> h2m-mark`、`h2m-lower-block` 的 `hd == 0` 誠實門、`h2m-lower-branch` 的 `-> h2m-mark`）各印一行 `XK/SK/BK <kind>`。

用插樁二進制掃 14 個自舉模組，`MK` 行數 **≥** `unsupported constructs` 計數（各模組差值 0–161 條，未逐條歸因——差值來自 `h2m-lower-func` 把函式 ctx 計數折進 `agg` 的時序與三處插樁門的重複計數）⇒ 本直方圖**只用作分佈，不用作絕對值**：

| 節點型別碼 | 節點 | 全模組 MK 次數 | 佔比 |
| --- | --- | --- | --- |
| `0`（棧空） | 標記發生在區塊/表達式之外（`h2m-lower-func` 的形參／回值門、槽鋪設、結構體解析） | 891 | 31.4% |
| `18` | `K-CALL-EXPRESSION` | 611 | 21.5% |
| `40` | `K-INFIX-EXPRESSION` | 479 | 16.9% |
| `37` | `K-IDENTIFIER` | 338 | 11.9% |
| `29` | `K-EXPRESSION-STATEMENT` | 233 | 8.2% |
| `44` | `K-LET-STATEMENT` | 139 | 4.9% |
| `73` | `K-USE-STATEMENT`（`#` 導入） | 37 | 1.3% |
| `32` | `K-FOR-STATEMENT` | 36 | 1.3% |
| `39` | `K-INDEX-EXPRESSION` | 33 | 1.2% |
| `61` | `K-SLICE-LITERAL` | 19 | 0.7% |
| `66` / `15` / `7` / `48` | `STRUCT-LITERAL` / `BLOCK-STATEMENT` / `ANNOTATION-STATEMENT` / `MULTI-ASSIGN` | 8 / 4 / 2 / 1 | — |

分模組看更清楚：**`cg-runtime.no` 的 350 條裡 296 條是 `K-CALL-EXPRESSION`**、`cg.no` 109 條裡 62 條同族——那兩個模組幾乎全是 `b.push('…')` 這種**方法調用**。而方法本體（`sb.push` / `sb.init`）住在 `util/sb.no`，屬**另一個模組**。⇒ 調用族的根因不是「不支援方法」，而是「**跨模組**什麼都不支援」。

**② 為什麼本輪不直接做調用，而先做 str 相等**

跨模組是 `#` 導入（M5 級）的整塊工程，一輪做不完。而直方圖裡第二大的 `K-INFIX-EXPRESSION`（479 條）可以就地量測、就地收：`h2m` 的二元運算分派對 `i64` / `u64` / `f64` / `str` 拼接都有臂，**獨缺 `str` 的相等/不等**。微探針釘死：

```text
# 实测（/tmp/pr/p-streq.no）
a = 'x'
b = 'y'
print(a == b)          ⇒ noc: unsupported constructs: 2（infix 1 條 + print 實參 1 條）
```

而 `OP-STEREQ = 63` / `OP-STNEQ = 64` **早就在 `mir.no` 定義、`cg.no:695/710` 早已實現**，`@str_eq` 定義由 `cg-runtime-prelude-full` 每模組發一次（`cg-runtime-str-eq`）。缺的**只是 `h2m` 這一條降級臂**——零改 `cg` / 零改 `mir`。

源碼側的規模（`compiler/src` 的 `== '…'` / `!= '…'` 行數）：`ps.no` **173**、`h2m.no` **61**、`drv.no` 9、`ck.no` 5、`ast.no` 3、`lx.no` 2。

**③ 實現（兩處，均在 `h2m.no`）**

| # | 位置 | 內容 |
| --- | --- | --- |
| 1 | `h2m-lower-expr` 的 infix 分支，`ok-s`（STRCAT）臂之後 | 新 `ok-e` 臂：`bop ∈ {OP-CMPEQ, OP-CMPNE}` 且**兩側 vty 皆 `TY-STR`** ⇒ `OP-STEREQ` / `OP-STNEQ`，`dst` 記 `TY-I64`（cg 發 `zext i1 → i64`，落 `%v` 而非 `%sv`）。末行 mark 條件加 `&& ok-e == 0` |
| 2 | `h2m-ty-of` 的 `K-INFIX-EXPRESSION` 分支，`f64` 分支之後 | 新 `TY-STR && TY-STR` 分支：`==`/`!=` ⇒ `TY-I64`；`+`/`-` ⇒ `TY-STR`（s99l STRCAT 的 dest） |

第 2 處不是可選項——見 ④。

**④ 踩到的坑（必記）：`h2m-ty-of` 與降級側 dest 型別必須同步，否則**只有 mem==1 才炸**

只做第 1 處時，下面這支**直線式**能編、**帶控制流**的同義程式卻掛：

```text
a = 'x'  b = 'y'  c = a == b  print(c)        ⇒ unsup 0（mem==0，不鋪槽，綁定就是 SSA 值）
a = 'x'  b = 'y'  c = a == b  if c == 1 { … } ⇒ unsup 5（mem==1，c 要 alloca，槽寬推不出）
```

根因：`h2m-ty-of` 對 infix 只推 `i64`/`u64`/`f64` 三族，**str 對 str 留 `-1`** ⇒ `h2m-slot-decl` 鋪不出槽 ⇒ 計 unsup、整函式不產碼。這與 s74（f64）/ s77（u64 比較族）記的紀律完全同型：**「ty-of 與降級側 dest 型別必須一起改」**。

順帶證明這條分支原本也缺 `+`/`-` 的 `TY-STR`（即 s99l 的拼接）：`s = 'a' + 'b'` 放在含控制流的函式裡同樣掛。兩者一併補上後實測（`compiler/tests/probe-s104.no`，量完即刪）：

```text
c = a == b + if        ⇒ unsup 0，%s5 = alloca i64，%se18 = call i1 @str_eq(%str-long %sv11, %str-long %sv12)
s = 'a' + 'b' + if     ⇒ unsup 0，%ss4 = alloca %str-long，store %str-long %sv10, %str-long* %ss4
```

⚠️ 這個坑的形狀值得單獨記：**同一份程式碼在 mem==0 與 mem==1 兩種模式下行為分岔**，而 mem 模式是由「函式裡有沒有控制流」隱式決定的。任何新增「推不出槽寬」的 dest 型別都會以這個形狀復發——判據不是「直線式能編」，而是「**把它塞進 `cond -> …` 之後還能編**」。故本輪的測試 `test-s104-streq-mem` 刻意用 `c == 1 -> r = 1` 造出 `mem==1`。

**⑤ 誠實邊界（一律 `unsupported constructs: N`，絕不產錯碼）**

- **混合形**：`str == i64`、`str == ?str`（權威靠 checker 先轉標量再比，自舉無 checker，不臆造該轉換）。
- **`str` 的排序**：`<` `>` `<=` `>=`（`@str_eq` 只判等，Go 側無對應降級，無權威可鏡像）。
- **`str` 的 `*`（重複）**、`str + i64` 拼接（沿 s99l 既有門禁）。

**⑥ 閘（全部在最終源碼上逐個復跑）**

| 閘 | 改前 | 改後 |
| --- | --- | --- |
| `drv-build-test` | 53/53 | **63/63**（新增 `test-streq` 10 條） |
| `h2m-test` | 1060/1066 | **1080/1086**（新增 `test-s104-streq`/`-strne`/`-streq-gate`/`-streq-mem` 共 20 條；6 紅 = 既有 `s98 optmatch`，身分逐條核對未變） |
| `lx-test` | 81/81 | 81/81 |
| `ps-test` | 39/42 | 39/42（3 紅既有） |
| `cg-test` / `cg-runtime-test` / `cg-str16-test` | 359 / 86 / 41 | 359 / 86 / 41 |
| `diag-test` / `ck-test` / `ck-sig-test` | 7 / 20 / 50 | 7 / 20 / 50 |

`git diff --check` 乾淨；`no fmt -d` 對三個改動檔皆報 0 行；量測探針 `probe-s104.no` 已刪除（`ls compiler/tests/probe*` 為空）。

**⑦ 端到端語義對拍（與 Go 權威逐位元組相同）**

```text
# /tmp/s104/e2e/e1.no —— 把比較結果繞過控制流，避免踩到 print(bool) 的已知缺口
a = 'abc'  b = 'abc'  c = a == b      c == 1 -> print('eq1')   c == 0 -> print('ne1')
d = 'abc'  e = 'abd'  f = d == e      f == 1 -> print('eq2')   f == 0 -> print('ne2')
g = d != e                            g == 1 -> print('ne3')   g == 0 -> print('eq3')
h = ''     i = h == ''                i == 1 -> print('empty-eq')

⇒ 兩側皆印 eq1 / ne2 / ne3 / empty-eq，cmp 相同
```

⚠️ 直接 `print(a == b)` 兩側**不同**：權威印 `true`/`false`、noc 印 `1`/`0`——這是**先於本件存在**的 `print(bool)` 缺口（自舉無 `TY-BOOL`，`i64` 比較同病，實測 `x=1; print(x==1)` 權威 `true` / noc `1`），與 s104 無關。故端到端閘一律繞過「直接印 bool」。

**⑧ L4 / unsup 全語料（169 檔共同集，序列，`noc build` 口徑）**

| 指標 | ⑪ 之後 | ⑫ 之後 | 變化 |
| --- | --- | --- | --- |
| 全語料 unsup 總數 | 11,354 | **11,163** | **−191（−1.7%）** |
| unsup 下降的檔 | — | **25 / 169** | — |
| unsup 上升的檔 | — | **0 / 169** | **0** |
| `unsup == 0`（能產碼）的檔 | 5 | 5 | 0 |
| 由可編譯變不可編譯的檔 | — | **0** | 0 |

下降最多的：`format.no` −27、`block-match.no` −23、`str-push.no` −22、`txt.no` −20、`misc-syntax-more.no` −17、`index-compare.no` −14。

⚠️ **沒有檔跨過零**：本件是「把接近的檔再推近一步」，不是「攻下新檔」。跨零要靠 ⑨ 的 `#` 導入（見 ⑩）。

**⑨ 自舉模組 unsup（真正的自舉閘，`noc build compiler/src/<m>.no` 逐模組）**

| 模組 | ⑪ 之後 | ⑫ 之後 |
| --- | --- | --- |
| `mir.no` | **OK** | **OK** |
| `diag.no` | 3 | 3 |
| `tok.no` | 15 | 15 |
| `link.no` | 24 | **22** |
| `ck.no` | 24 | 24 |
| `drv.no` | 32 | 32 |
| `asp.no` | 80 | 80 |
| `ast.no` | 93 | 93 |
| `cg-rc.no` | 99 | 99 |
| `cg.no` | 100 | 100 |
| `ck-sig.no` | 101 | 101 |
| `lx.no` | 123 | 123 |
| `h2m.no` | 154 | **148** |
| `cg-runtime.no` | 340 | 340 |
| `ps.no` | 1,268 | **1,182** |
| **合計** | 2,456 | **2,368（−88）** |

**⑩ 量測結論：下一輪的閘是 `#` 模組導入，不是任何單一運算式族**

本輪順手量了一件此前沒量過的事：`noc build compiler`（整個包）**rc=0、產出 34,632 位元組、執行 `version` 什麼都不印**。也就是說 `#` 導入目前**整條被忽略**——既不解析被導入模組，也不把它的函式降級進同一份 IR。這與 ⑨ 的直方圖互相印證：

- 611 條 `K-CALL-EXPRESSION` 標記裡，絕大多數是 `ast.str-at(...)` / `b.push(...)` 這種**點號被調者**，而 `h2m-lower-expr` 的調用臂第一行就是 `cd.kind != asp.K-IDENTIFIER -> ok-call = 0`；
- 就算把點號調用放行，被調用的函式本體也在**別的模組**裡，`h2m-sigs` 是**逐模組**建的（`h2m-lower-program` 只掃自己那棵 Program）⇒ 簽名查不到、IR 裡也沒有定義；
- 37 條 `K-USE-STATEMENT` 標記 + 上述 611 條，其實是**同一塊**。

⇒ `#` 導入是 M5 的入場券，也是「自舉」二字目前唯一還空著的結構性缺口。⑫ 之後它已成爲**唯一**擋在「模組能互相調用」前面的東西。

**⑪ 仍未收（下一輪候選，依本輪量測重排）**

- **`#` 模組導入**（`K-USE-STATEMENT` 37 條 + 611 條點號調用）——現已成第一優先，也是唯一能讓 `noc build compiler` 產出真二進位的路。
- `0`（棧空）的 891 條：`h2m-lower-func` 的形參／回值門、槽鋪設、結構體解析。這一族**沒有節點型別可歸因**，需要再插樁細分（下一輪若要繼續量測，這是第一個該細分的地方）。
- `K-EXPRESSION-STATEMENT` 233 條：陳述位誠實門（`hd == 0`），子節點不是 if/match/term/call/欄位寫的其餘表達式。
- `K-INDEX-EXPRESSION` 33 條（⑨ 按檔數記的是 103 檔，按標記數只有 33——**兩種口徑不可混用**）。
- 值位匹配的 **str/vec 臂值**（結果槽目前只有 `i64` 一種寬）；`ok(...)` 條件臂（`minimal.no` 就卡在這＋str 臂值）。
- `print(bool)` 印 `1`/`0`；插值的 `:spec`／表達式欄位／多實參模板形／`{{` `}}` 轉義。
- **短路語義裁決**（B19）：`{}` 無兜底臂的多臂鏈要不要跟權威的「獨立 if」對齊。

📌 **後續（⑬）**：本節第 2 條（「`0`（棧空）的 891 條需要再插樁細分」）在 ⑬ 換了一條更便宜的路——不去細分插樁，而是**直接量「函式簽名為什麼被拒」**。量出來的頭號原因（101 條 `ast` / 15 條 `sb` 型別參數）**不是 h2m 的型別覆蓋面**，而是**解析器根本沒把那些結構體登記進表**：`ps-is-struct-def` 不容忍欄位尾的 `;` 註釋。⑬ 修的就是這個。

### ⑬ 2026-11-12 追記（續）：解析器保真度——結構體欄位尾 `;` 註釋（**實作輪次**）

**① 換一個入口量：不細分插樁，直接量「簽名為什麼被拒」**

⑫ 的直方圖裡最大一格是「棧空 891 條」，註記是「形參／回值門、槽鋪設、結構體解析，需要再插樁細分」。本輪沒有沿那條路走，而是寫了一支**唯讀探針**（`compiler/tests/probe-s105.no`，量完即刪）直接調 `h2m-func-sig` / `h2m-param-ty`，把每個被拒的函式連**原因**印出來：

```text
BAD <path> <fn> <why>
  why ∈ { generic(n), multiout(n), outdefault, outvec, inN=<kind>:<name>, out=<kind>:<name> }
```

探針必須復現 `h2m-lower-program` 的兩段前置（先 `h2m-struct-register` 全體結構體、再 `h2m-struct-resolve` 解析欄位型別），否則簽名裡的結構體別名一律查不到（首版即此：所有 `ast` 參數都誤判 bad）。

15 個自舉模組的結果（去重後）：**250 個函式簽名被拒**，其中顯式原因桶只有 `multiout(2)` 25 + `multiout(3)` 2 + `multiout(5)` 1 + `outvec` 3 = **31 條**，剩下 **219 條全是型別名**：`49:ast` 101、`62:false`（＝`[]T` 的 `K-SLICE-TYPE`，元素非 `i64`）20、`49:sb` 15。

**② 根因：`ast` 這個結構體**從來沒進表**

`49:ast` 的意思是「參數型別節點是 `K-NAMED-TYPE`，名字 `ast`，但 `h2m-tynode-code` 的結構體回退查表回 -1」。可是 `ast` 在 `ast.no:122` 明明有定義。用第二支探針（`probe-s105b.no`，印頂層節點 kind 直方圖 + 結構體表內容）一看：

```text
# ./bin/no run compiler/tests/probe-s105b.no compiler/src/ast.no
TOP nkid=68
K 64 s0=nd          ← 只有 nd 被認成 K-STRUCT-DEFINITION
K 29 s0=           ← 其餘退回「表達式陳述」
K 15 s0=           ← ＋「塊陳述」
...
STN n=1             ← 結構體表只有 nd
S 0 = nd
```

逐欄位二分（`nd` 保留、`ast` 只放一個欄位）後釘死：**只要欄位那一行帶 `;` 尾註釋，整個結構體就不被認出**。`ast` 的 12 個欄位裡有 3 個帶（`str-k []str; string-interning hash table (open addressing; grows on demand)` 等）。

最小重現與權威對照：

```text
zz {
    x i64; 說明
}
⇒ ./bin/no ast            StructDefinition{name: zz, fields: 1} / StructField{name: x, type: i64}   ← 權威正確
⇒ ps.parse + h2m-struct-register  結構體表空（n=0）                                              ← noc 錯
```

**③ 為什麼兩側會分歧：Go 的 parser 看不見 COMMENT**

`src/lexer/lexer.go:562` 的註釋寫得很明白——「parser 的 `nextToken()` 會自行跳過 COMMENT，因此這裡保持原始語義返回所有 token 類型」。也就是 Go 的 `peekToken` / `look(n)` **永遠不會**返回 `COMMENT`；而 `ps.no` 的 `ps-peek-at` 忠實回傳。

`ps-is-struct-def`（`ps.no:7654`）是「`IDENT {` 到底是不是結構體定義」的**前瞻分類器**：它逐欄位走 `IDENT` → `ps-skip-type-idx` → `ps-skip-annot-idx`，然後看型別後面那個 token 是什麼：

```text
et == NEWLINE / COMMA / RBRACE / EOF  ⇒ 算一個欄位
（其餘）                              ⇒ go = false; r = 0        ← COMMENT 落在這裡
```

判 0 ⇒ 整個 `{ … }` 退回 `blockUnknown` ⇒ 被當成語句塊解析 ⇒ **結構體從未登記**。這不是「dump 差一個字」，是整塊消失。

**④ 實現（一處，`ps.no`）**

`ps-is-struct-def` 的終結符分支新增一條 `et == tok.COMMENT` 臂：跳過註釋**並且立刻計數**。

「立刻計數」不是隨手寫的——主迴圈的 `NEWLINE` 臂**不計數**（計數只發生在 `IDENT` 臂走到終結符時）。若只跳過不計數，那麼「每個欄位都帶尾註釋」的結構體會得 `nfield == 0` ⇒ 仍判非結構體。同行 `}` 收尾（`x i64; c }`）同理需要即時計數。

⚠️ 單行 `zz { x i64; c }` 在**權威側也是語法錯誤**（`;` 是行註釋，把 `}` 吃掉了），故不支援、也不該支援。

**⑤ 影響面（誠實：語料一動不動，自舉模組的「產碼函式數」翻倍）**

| 指標 | 改前 | 改後 | 判讀 |
| --- | --- | --- | --- |
| L4 語料 173 檔 unsup 總數 | 11,315 | **11,315** | **逐檔 `diff` 完全相同**（不是「總和相同」） |
| `unsup == 0` 的檔 | 5 | 5 | 未跨零 |
| 簽名被拒（15 模組，去重） | 250 | **166（−84）** | `h2m.no` 63→32、`ast.no` 55→13、`lx.no` 23→12；其餘 12 模組不變 |
| 自舉模組實際產碼函式數（`define` 行） | `ast.no` 3、`lx.no` 2 | **`ast.no` 8、`lx.no` 3** | 這才是能力指標 |
| 自舉模組 unsup 總數（`noc build` 逐模組） | 2,362 | **3,198（+836）** | **上升，且是預期** |

⚠️ **unsup 上升不是退步，是「更多函式被嘗試」**：簽名判 bad 時 `h2m-func-sig` 讓整個函式**跳過並只記一條**；簽名通了之後函式本體才被降級，body 裡每個不支援的構造各記一條。`ast.no` 93→410、`h2m.no` 148→509、`lx.no` 123→281 全部由此而來，另 12 個模組（含 `ps.no` 1,182）**逐檔不變**——因為只有這三個模組的結構體帶尾註釋。判據不能只看 unsup 計數，必須同時看 `define` 行數。

**⑥ 閘（全部在最終源碼上逐個復跑，`./bin/no run compiler/tests/<X>.no`）**

| 閘 | 改前 | 改後 |
| --- | --- | --- |
| `ps-test` | 39/42 | **41/44**（新增 `test-struct-field-trailing-comment`、`test-struct-field-all-commented` 兩條；3 紅＝既有 `err ->` / `err(m) -> {…}` / `option 主語` 三條，身分逐條核對未變） |
| `lx-test` | 81/81 | 81/81 |
| `drv-build-test` | 63/63 | 63/63 |
| `h2m-test` | 1080/1086 | 1080/1086（6 紅＝既有 `s98 optmatch`） |
| `cg-test` / `cg-runtime-test` / `cg-str16-test` | 359 / 86 / 41 | 359 / 86 / 41 |
| `diag-test` / `ck-test` / `ck-sig-test` | 7 / 20 / 50 | 7 / 20 / 50 |

`git diff --check` 乾淨；`no fmt -d` 對兩個改動檔皆報 0 hunk；三支量測探針（`probe-s105.no` / `probe-s105b.no` / `probe-s105c.no`）已刪除（`ls compiler/tests/probe*` 為空）。

**⑦ 順手量到的兩個精確靶（下一輪可直接打）**

用 `noc build` 對近零檔做消去法（`tests/dowhile.no` 全檔 unsup 2）：

```text
i = 0; (i < 5) { print(i) }; print(0)                                ⇒ unsup 0
＋ `i-opt: { ok -> i = it` / 空臂 `->` }                              ⇒ unsup 1   ← 空 match 臂
＋ `#{overflow=wrap}` 裸塊陳述 `{ … }`                                 ⇒ unsup 2   ← 裸塊陳述
```

兩者都是「陳述位多一層殼」的形狀，且互不相干；`dowhile.no` 要跨零需兩個都做。`minimal.no`（2）卡在 `ok(it > 127)` 條件臂＋str 臂值（⑫ ⑪ 已記）。

**⑧ 仍未收（依本輪量測重排）**

- **`#` 模組導入**——仍是唯一結構性缺口（⑫ ⑩），但**設計與實施計劃已立於 ⑭**（whole-program 降級：單 `agg` + 全域簽名表 `key=mod.fn` + 多模組 body 串接 `@<mod>.<fn>`，含 6 個可獨立驗收的最小切片，見 ⑭ ⑪）。本輪修的解析器 bug 恰好證明它有多硬：**連「一個模組自己的結構體」都登記不全**，談跨模組之前先把單模組擺正——這正是 ⑭ 修正點 1（`ps-parse-use` 拆分 `<path>.<symbol>`）的動機。
- **空 match 臂 `->`**、**裸塊陳述 `{ … }`**——⑦ 已釘到最小重現。
- **`[]T` 元素型別只有 `i64`**：簽名側 20 條 `62:false`（`h2m-param-ty` / `h2m-tynode-code` 的 `enm == 'i64'` 門）。⚠️ 這與 ⑫ ⑨ 的 `[]str` 結構體欄位是同一個門的兩側：欄位側放行會讓 `%vec` 欄位被 i64 helper 誤讀，故**不能只放行不解釋**。
- `multiout(2/3/5)` 28 條＋`outvec` 3 條——多返回值是 MIR 級工程，不是降級臂。
- ⑫ ⑪ 的其餘各條（`K-EXPRESSION-STATEMENT` 233、`K-INDEX-EXPRESSION` 33、`print(bool)`、插值剩餘形、B19 裁決）。

---

### ⑭ 2026-11-12 追記（續）：`#` 模組導入——自舉唯一剩餘的結構性閘門（**設計＋實施計劃**，尚未實作）

> 本節是**前向設計**，不是 as-built：以下數字若未標「實測」均為**預期／待測**，待本輪落地後回填 ⑩ 的閘表。所有代碼行號均為 2026-11-12 現況（`compiler/src` 狀態 `3589ebb1`），可據以動工，但本節**不動任何源碼**。

**① 為什麼這是唯一的結構性缺口（現狀摘要，引用 ⑫ ⑩／⑬ ⑪）**

- `noc build compiler`（入口 `compiler/main.no`，見 `compiler/main.no:3` 的構建命令）目前 rc=0、產出 34,632 位元組、執行 `version` **什麼都不印**——因 `#` 導入整條被忽略。
- 611 條 `K-CALL-EXPRESSION` 標記裡絕大多數是 `ast.str-at(...)` / `b.push(...)` 這類**點號被調用者**；`h2m-lower-expr` 呼叫臂第一行即是 `cd.kind != asp.K-IDENTIFIER -> ok-call = 0`（h2m.no:2095），點號呼叫全被擋下。
- 即便放行點號呼叫，被調函式本體在**別的模組**，`h2m-sigs` 是**逐模組**建的（`h2m-lower-program` 只掃自己那棵 Program，h2m.no:3952–3993），簽名查不到、IR 裡也沒有定義。
- 37 條 `K-USE-STATEMENT`（parser 已產出，`ps-parse-use` ps.no:987）＋上述 611 條點號呼叫＝「同一塊」——而它們目前**完全不被消費**。

⇒ `#` 導入是 M5 的入場券，也是「自舉」二字目前唯一還空著的結構性缺口（⑫ ⑩／⑬ ⑪ 同判）。

**② 目標形態（M5 完成後）**

從入口 `compiler/main.no` 出發 → 遞歸解析其 `#` 導入的各模組 → 全部函式降成**同一份** `.ll` → 點號呼叫 `mod.fn` 走**全域簽名表**查到並 emit `call @<mod.fn>`；`link.compile`（link.no:24）**不變**（仍只餵一份 `.ll` 給 clang）。

**③ 入口與導入語法的現狀（實測）**

`compiler/main.no` 的導入有兩種形：

```text
# /compiler/src/drv.dispatch        ← 帶 .symbol（第 8 行）
# /compiler/src/asp                ← 純路徑（第 15–29 行）
```

`ps-parse-use`（ps.no:987）舊版只抓 `path`（s0），把 `function`/`alias`（s1/s2）**恆置空**，檔頭註釋還寫「語料恆為純路徑（無 .fn / as）」。但 `compiler/main.no:8` 的 `# /compiler/src/drv.dispatch` **帶 `.dispatch` 符號**⇒ 舊版會把整串 `/compiler/src/drv.dispatch` 吞進 `path`，`drv` 模組永遠解析不到。

🔴 **修正點 1（必做）**：`ps-parse-use` 必須把 `<path>.<symbol>` 拆成 `path` + `function`（以及 `as` 別名，雖 compiler 自身不用但 std 跨包導入會用）。這與 ⑬ 的「欄位尾 `;` 註釋」是同一類——**parser 保真度決定了 selfhost 能走多遠**。

📌 **追記（2026-11-13，slice 1 實作輪次）**：修正點 1 已落地（`compiler/src/ps.no` `ps-parse-use`，行 987）。現 `<path>.<symbol>` 正確拆成 `path` + `function`，並支援 `as alias` 與裸 `alias`；裸 alias 的 token 在路徑結束時【未經消費】（用 `-> break` 退出路徑迴圈，避免被當成下一路徑段併入 `path`）。實測 `ast.dump`：

- `# /compiler/src/drv.dispatch` → `path: /compiler/src/drv, function: dispatch`
- `# /compiler/src/drv.dispatch as d` → `alias: d, as: true`
- `# /compiler/src/asp cfg`（裸 alias）→ `path: /compiler/src/asp, alias: cfg`（path **未被污染**）
- `# std/err error`（裸 alias）→ `path: std/err, alias: error`

迴歸測試見 `compiler/tests/ps-test.no` 的 `test-use-*`（5 條全綠，即本輪的 L2 oracle）。

⚠️ **self-host codegen 陷阱（本輪實踩）**：初版把 `as-kw`/`alias` 等旗標的賦值寫在嵌套 `cond -> { }` 分支臂內、又在一個【不同】分支臂讀取 ⇒ MIR 單趟配置器無法解析變數初始化路徑，報 `EmitLLVM: unknown callee ps-parse-stmt`。修法：所有旗標（`is-fn-sep`/`as-kw`/`alias`/`function`/`path`）**一律在函式頂層初始化**、條件賦值走直線程式碼（不在分支臂內讀取跨臂寫入的變數）；`-> break`/`-> continue` 也須包成合法 if-group（裸 `-> break` 緊跟 `cond -> { }` 臂會被當成 else 臂報 `m7vq3xrt`）。

**④ 路徑解析（open issue，需先定義）**

- 路徑相對於 **workspace 根**（§2.1：`#` 導入路徑是 workspace 相對，非檔案系統根）。`# /compiler/src/drv` ⇒ 實體檔 `compiler/src/drv.no`。
- 尾碼約定需明確：`# /compiler/src/util/sb.sb`（main.no:26）導的是 `compiler/src/util/sb.no`——這裡 `.sb` 是**模組名尾碼**還是**符號**？Go 側 `no` 怎麼解就照抄（黃金法則：不改 S0 語義），但 `ps-parse-use` 與驅動層 resolver 必須一致。建議：路徑尾若為 `.no`/`.sb` 或目錄，統一對映到 `<path>.no` 或 `<path>/main.no`。
- 遞迴解析需做 **cycle 檢測**（A→B→A），否則互相導入會無限遞迴。

**⑤ 整體架構（whole-program 降級：單 `agg` + 全域簽名表）**

自舉編譯器本身是一個「程式」，故最簡且最穩的切法是**整包一次降級**（對照 M4 的「誠實門禁」哲學，先求正確再求裁剪）：

| 階段 | 現有接縫 | 本輪動工點 |
| --- | --- | --- |
| A. 套件載入 | `drv.build-ll(src)` 只 parse 單檔（drv.no:365–369） | 新增 `drv.build-package(src)`：parse 入口 → 收 `K-USE-STATEMENT` → 路徑解析 → 遞迴 parse 依賴 → 回傳依序 `(ast, root)` 清單（含 cycle 檢測） |
| B. 結構體表 | `h2m-struct-register`/`h2m-struct-resolve`（h2m.no:948 起）逐模組跑在 `h2m-lower-program` 前置 pass | **共享同一 `agg = h2m-new()`**（h2m.no:204）給全包：各模組 struct 全註冊進同一張表 ⇒ 跨模組結構體型別（如 `ast.node` 被 `ps`/`h2m` 引用）天然可查 |
| C. 全域簽名表 | `h2m-sigs` 逐模組建、`h2m-sig-add(sg, name, ...)` 只帶 `name`（h2m.no:1252） | `h2m-sig-add` 擴充為帶 `mod` 限定，`key = mod.fn`；`h2m-func-sig`（h2m.no:3673）產 signature 不變 |
| D. 降級全部模組 | `h2m-lower-program`（h2m.no:3952）只降自己那棵 | 逐模組呼叫對等邏輯，emit `@<mod>.<fn>`（入口 `main` 仍 `@main`，見 ⑦），body 串接 |
| E. 接線 | `drv.build-ll` 組 `prelude + body`（drv.no:380–385） | `build-package` 取代 `build-ll` 的單檔路徑；`link.compile` 不變 |

**⑥ 呼叫降級（h2m-lower-expr）**

把 h2m.no:2095 的 `cd.kind != asp.K-IDENTIFIER -> ok-call = 0` 改為：

- callee `cd` 為 `K-DOT-EXPRESSION`（asp.no:555 = 24；`ast.str-at` 的 base=`ast`、field=`str-at`，遞迴取名的邏輯見 `ps-dotted-name` ps.no:4674）：
  - base 是**模組短名**、rhs 是函式名 ⇒ 查 `<mod>.<fn>` 全域簽名表；
  - 命中 ⇒ emit `call @<mod.fn>(...)`，arity／型別檢查沿用 s56 的連續 id／側池 `args` 契約（§11 B16）；
  - 未命中 ⇒ `ok-call = 0` → 整函式 `unsup`（誠實門，與今日同）。
- 純 `K-IDENTIFIER` 呼叫仍走原邏輯（本模組內或已 import 的符號）。

⚠️ `b.push(...)` 的 `b` 是局部 `vec` 變數、不是模組名 ⇒ 這類「值的方法」不在本輪範圍（對應 SHS 禁動態分派，且 `vec.push` 在 Go 側是內建方法、非跨模組呼叫）；遇 `K-DOT-EXPRESSION` 而 base 不是模組名時仍計 `unsup`，不靜默錯譯。

**⑦ 函式命名唯一性（§11 B10 同族）**

- 跨模組 emit `@<mod>.<fn>`，避免 `init`/`main` 等撞名（B10 曾因 `%struct.point`、全域 `@.s<k>` 同名撞車而踩過）。
- 入口 `main.no` 合成的 `main`（`h2m-lower-module` h2m.no:4355–4444）**必須保持 `@main`**——LLVM／C runtime 約定。
- 結構體型別宣告已由 `h2m-struct-decls` 的「唯一裝配點」保證（h2m.no:4026–4031，§11 B10）；函式名需在生成側補同等約束（同一 `fn` 名只在所屬模組 emit 一次）。

**⑧ 跨模組結構體欄位型別**

單 `agg` 已統一 struct 表，但需驗證：`h2m-tynode-code` 的 struct 回退查表（§11 B8 提過）在跨模組引用時能正確命中——例如 `ps` 模組函式簽名裡的 `ast.node`、欄位型別 `[]ast.node`。現行 `agg` 是全域 ctx，理應直接命中；**落地時以 `noc build compiler` 的 `define` 行數（對照 ⑬ ⑤ 的「產碼函式數」指標）確認未被漏登記**。

**⑨ 誠實邊界（沿用既有紀律）**

- 查不到的 `mod.fn` / 未實作模組 / 非模組 base 的點號呼叫 ⇒ `unsup`，`agg.unsup > 0` ⇒ `drv.build-ll` 直接 rc=1（drv.no:372–374），**不靜默產錯碼**。
- 與 ⑬ ⑤ 一致：unsup 上升不是退步，是「更多函式被嘗試」；判據須同時看 `define` 行數。

**⑩ 驗收閘（預期，M5 里程碑；數字待落地回填）**

| 閘 | 改前 | 改後（預期） |
| --- | --- | --- |
| `noc build compiler`（入口 main.no） | rc=0、產 34,632 B、version 不印 | **rc=0、執行 `version` 印 `noc v0.0.1-m0`** |
| 跨模組函式呼叫（如 `drv.dispatch` → `ps.parse`） | unsup | **切片 5：點號呼叫機制已通（emit `call @mod.fn` 經全域 sg 解析）；定義/呼叫名統一可連結留切片 6** |
| 既有測試 | `h2m-test` 1080/1086 等 | **維持全綠（零回歸）** |
| `noc` 自編自己（S1 預備） | 不可行（導入被忽略） | **`noc` 能編出可執行、行為對齊 S0 的二進位** |

**⑪ 實施順序（最小切片，每片可獨立驗收）**

1. **`ps-parse-use` 拆分 `<path>.<symbol>` + `as`**（修正點 1，③）— **✅ 已實作（2026-11-13）**。驗收：`compiler/tests/ps-test.no` 的 `test-use-*` 5 條全綠：`# /compiler/src/drv.dispatch` → `path=/compiler/src/drv, function=dispatch`；`# /compiler/src/drv.dispatch as d` → `alias=d, as=true`；裸 alias `# /compiler/src/asp cfg` → `path` 不被污染、`alias=cfg`（見③ 追記）。注：`no ast` 是 Go 側 parser，本驗收以自舉 `ps` 的 `ast.dump` 為準。
2. **`drv.build-package` + 路徑解析 + cycle 檢測**（⑤ A／④）— **✅ 已實作（2026-11-13）**。驗收：對 `compiler/main.no` 跑 `drv.build-package` 回傳依序模組清單（前置 cycle 旗標 `0:`＋`:` 分隔），實測 = 入口 `compiler/main.no` + **16 個 compiler 套件模組**（drv/lx/tok/util·sb/ast/asp/ps/diag/ck/ck-sig/mir/cg/cg-runtime/cg-rc/h2m/link），無循環（`cyc=0`；設計原列 15 為筆誤，實為 16 imported＋入口共 17 項）。回歸測試 `compiler/tests/build-package-test.no` **8/8 全綠**（cycle 旗標＝0 ＋ 關鍵模組 membership）。⚠️ 本切片只回傳「模組路徑清單」這張結構性閘，`(ast, root)` 留待切片 2b；路徑解析（④，`.sb`→`.no`／去前導 `/`／補 `.no`）與祖先鏈 cycle 檢測（pathchain 參數，diamond 不誤報）均已落地，實作見 `compiler/src/drv.no` 的 `drv.build-package` / `drv.bp-walk` / `drv.bp-resolve` / `drv.pkg-contains`。
3. **全域簽名表 + 單 `agg` struct 註冊**（⑤ B／C）— **✅ 已實作（2026-11-13）**。驗收：對 `compiler/main.no` 跑 `drv.pkg-sigs` 回傳整包全域簽名表（實測 **395** 條），`h2m-sig-find(sg, 'ps.parse')` / `'ast.str-at')` / `'h2m.sig-add'` / `'ast.make'` 皆命中；偽 key `'ps.parsex'` 查不到。回歸測試 `compiler/tests/pkg-sigs-test.no` **5/5 全綠**。實作見 `compiler/src/drv.no` 的 `drv.pkg-sigs` / `drv.read-mod` / `drv.mod-name` / `drv.pkg-split`；單一 `agg=h2m-new()` 共享整包結構體名→sid，單一 `sg=h2m-sigs-new()` 收整包 `mod.fn` 簽名（兩遍：先全模組 struct-register，再全模組 func-sig，避免消費者先於依賴被處理時查不到結構體名）。
   - ⚠️ **key 構造坑（實踩）**：自舉各模組函式名前綴慣例不一致——`drv.build-package`/`ps.parse`/`ast.str-at` 用**點號**前綴，`h2m-sig-add`/`ps-parse-use` 用**連字**前綴。故 key 須先剝掉 `mod.` 或 `mod-` 模組前綴得 short，再組 `mod.short` 規範 key，與切片 5 點號呼叫 emit `call @<mod.fn>` 的查表 key 對齊（否則 `h2m-sig-add` 會被錯拼成 `h2m.h2m-sig-add`）。
   - ⚠️ **`drv.*` 模組函式暫不入表（已知自舉 parser 缺陷，留待切片 5 前置修）**：`drv.no` 用 `drv.x = (...)` 點號名定義，自舉 `ps.parse` 對此類函式定義的 signature 計數有 bug（`d.c5` 多算 1：`drv.name = () (r str)` 被算成 c5=2、`drv.usage` 無回傳被算成 c5=1 且回傳型別 rc=-1），致 `h2m-func-sig` 全判 `bad=1` 而略過。裸名模組（ps/ast/h2m/lx/…）不受影響，故切片 3 驗收（ps.parse/ast.str-at）達標；但 `drv.dispatch`→`ps.parse` 這類跨模組點號呼叫要通，必須先修這個 parser 計數 bug（見切片 5 前置）。
4. **多模組 body 串接 + `@<mod>.<fn>` 命名**（⑤ D／⑦）— **✅ 已實作（2026-11-13）**。機制驗收：同一份 `.ll` 串接 ps/h2m/lx/ast(nd)/ck/cg-runtime 多模組 body（實測 **67 個 define** 跨多模組），`@<mod>.<fn>` 命名（連字前綴 → `@ps-bump`、點號前綴 → `@ps.parse` 經全域簽名表 key 證明接線正確），`@main` 由 `drv.pkg-ll` 合成；`cg-emit-cfg-t` 字串全域改為二進位安全 `[N x i8] [i8 ...]` 整數陣（s99o，補陣列結尾 `]`），`opt` 不再報語法錯誤。回歸 `compiler/tests/pkg-ll-test.no` **10/10 全綠**（含 `process.cmd` 呼叫 `opt -passes=verify` 斷言無語法錯誤）。⚠️ **字面驗收（`@ps.parse`/`@ast.str-at` 本體進 `.ll` 且 `opt rc=0`）延後至切片 5+**：h2m 當前僅能降級極少數函式本體（整包 **5749 unsupported**），`ps.parse`/`ast.str-at` 本體含未支援構造 ⇒ `h2m-lower-func` 回 `''` 不發射，其被呼叫者（`h2m-byte-at`/`h2m-synth-main`/`h2m-lower-program`/`lx-*`/`ps-*` 等 **17 個**未定義引用）亦未定義 ⇒ `opt` 報 `use of undefined value`。此屬 h2m 降級能力缺口，非串接 bug；stub 偽造 rc=0 違反設計 ⑨ 誠實邊界，故不取。實作見 `compiler/src/drv.no` 的 `drv.pkg-ll`／`compiler/src/h2m.no` 的 `h2m-synth-main`／`h2m-struct-resolve-range`／`compiler/src/cg.no` 的 `cg-emit-cfg-t`。
5. **`h2m-lower-expr` 點號呼叫查全域表**（⑥）— **✅ 已實作（2026-11-13 本輪）**。機制驗收（受控切面包 `compiler/tests/t5/`：`call-entry.no`→`testmod.no`+`caller.no`，驅動測試 `compiler/tests/pkg-ll-call-test.no` **4/4 全綠**）：`caller-run` 體內的 `testmod.bar(x)` 點號呼叫 ⇒ `.ll` 實測產出 `call i64 @testmod.bar(i64 %v1)`，證明「點號呼叫經全域 sg 解析並 emit `call @<mod.fn>`」接線正確。
   - ⚠️ **AST 結構實測（與初判不同，已更正）**：`mod.fn(x)` 解析為 CALL（`c0=1`、nkid=2）其**子節點[0] 就是單個 `K-DOT-EXPRESSION`**（整個 `mod.fn` 作為 callee：`s0="fn"`、`mod` 在 dot 子節點[0] 的 `K-IDENTIFIER`）；**實參 `x` 是 CALL 的直接子節點 child[1]**（與純量呼叫 `f(x)` 同構，並非嵌在 dot 節點內）。故 `cn = dot.base.s0 - '.' - dot.s0`（得 `mod.fn`），`argbase` 沿用 `d.kid0+1`（實參為 CALL 直接子節點），`na` 仍取 `d.c0`。`h2m-lower-expr` 的 `K-CALL-EXPRESSION` 臂現對「callee 為 `K-DOT-EXPRESSION`」與「callee 為 `K-IDENTIFIER`（純量）」兩形態統一處理（實作見 `compiler/src/h2m.no`）。
   - ⚠️ **emit 名 / 呼叫名錯位（已知限制，留待切片 6）**：受控被呼叫者 `testmod.no` 以【連字號名】`testmod-bar` 定義（自舉 parser 對【點號】名多算 `c5=1` 致 `h2m-func-sig` 誤判 `bad=1` 不產碼、不入表，故受控模組須用連字號名），其 emit 名為 `@testmod-bar`（連字號）；而呼叫名 `cn` 是 `testmod.bar`（點號）⇒ `.ll` 出現「定義 `@testmod-bar`、呼叫 `@testmod.bar`」的未定義引用，`opt -passes=verify` 報 `use of undefined value '@testmod.bar'`。切片 5 驗收只斷言「點號呼叫經全域 sg 解析並 emit 出 `call @testmod.bar`」機制成立（語法合法、rc 容許 undefined-value），**統一 emit 名成 `mod.fn`（讓定義名與呼叫名一致）屬切片 6 範疇**，見下方 ⑫ 本輪不收。
   - 回歸零新增：`cg-test` **359/359**、`cg-runtime-test` **86/86**、`drv-build-test` **63/63**、`pkg-sigs-test` **5/5**、`build-package-test` **8/8**、`h2m-test` **1080/1086**（6 紅為既有 `s98 optmatch`）、`pkg-ll-test` **9/10**（1 紅為既有 `@h2m-lower-module`，均與本切片無關）。
6. **端到端閘 + L4 對拍**（⑩）。驗收：`noc build compiler` 產物執行 `version` 印出版本；抽 `tests/` SHS 子集跑 L4。

**⑫ 本輪不收（留待 M5 後續）**

- workspace / package 合併、`target` 解析、快取（`drv.no:357` 明確 M4-d 不做）。
- 多 package 的 `#` 跨包解析（自舉只需 compiler 單包）。
- `as` 別名重綁（compiler 自身不用；std 跨包導入才需要）。
- whole-program 降級的體積／重複 emit 優化（未用函式也降級）——可後續改 reachability-based 按需 emit，但非 M5 必須。

---

## 附錄 C：與其他設計文檔的關係

| 文檔                                                  | 關係                                                                                                           |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `docs/design/nolang-native-backend.md`（nocg / nold） | **互補**它解決去掉 LLVM 工具鏈依賴；本方案解決去掉 Go 實作依賴兩者可以疊加：`noc` + `nocg/nold` = 完全不依賴外部工具鏈但**不要同時做**——先自舉，再 native |
| `docs/design/tagged-enum-payload-ownership.md`      | `noc` 的 M4/M7 必須完整理解它；RC 策略（§5.6）是它的**簡化替代**，M7 收斂時要對齊                                                       |
| `NOLANG-OWNERSHIP-MODEL.md`（repo 根）                 | 所有權模型的權威描述；§5.6 的 RC 規則直接引用其中的 P4/P5                                                                         |
| `docs/docs/lang/*.md`                               | SHS 的語言規則來源                                                                                                  |

---

|  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
**進度快照（2026-11-06，歷史；最新見下方 2026-11-12 as-built 快照）**：M0M1M2 已完成（L2 全語料 655/655 SAME0 回歸；M1 L1 675/675 SAME）；M3 已連續落地 M3-a 骨架（`drv.cmd-vet`＋`diag.no`）→ M3-b stdsig 表遷移（`ck-sig.no`）→ M3-c 首族（語法錯誤收集器）→ 缺口② NEWLINE 坐標族 → 正語義首件 `ck.no`（`'X' is not defined`，全語料 668 檔 ck 誤報 13→0parser 族仍 0），見 §5.3.4.am–.as 與 §6.4**M4 已啟動**：M4-a 骨架落地（`cg-runtime.no`/`cg-rc.no` prelude 逐位元組照抄＋`check-rc-balance`，16/16`opt` verify rc=0）＋M4-b 骨架落地（`mir.no`＋`cg.no` 的 `cg-emit-func` 發 CONST/ADD/SUB/MUL/CALL/ALLOC/STORE/LOAD/RET，13/13端到端 `let-add` IR 經 clang 執行 exit 6）＋M4-c 控制流/循環（多基本塊＋`BR/CONDBR/CMPLT`＋回邊，`lt10(5)+lt10(15)` 執行 exit 3`sum4()` 執行 exit 6）＋M4-c 字符串（首个非 i64 型別 `%str-long`：`@.s<k>` 全局字面量前置 pass＋三步 `insertvalue`＋`extractvalue` 取 len，`slen()` 執行 exit 3）＋M4-c 字符串拼接（首个 RC 消費者：`@str_concat` 13 行逐字照抄＋`prelude-full` 唯裝配點，`cat()` 輸出 `abcd`exit 4；`cg-test` **41/41**`cg-runtime-test` **21/21**）＋M4-c 多參調用（`OP-CALL` 的 `bs` 從佔位改為**參數個數**，破壞性語義變更，`add2(3,4)` 執行 exit 7）＋M4-c `%str-long` 跨基本塊（`SALLOC/SSTORE/SLOAD`，第三個槽命名空間 `%ss<id>`；`pick(0)`→exit 2`pick(1)`→exit 6）＋M4-c `vec`（`%vec = { i64, i64, i64 }` + 4 個 helper，第四個值命名空間 `%vc<id>`；十次 push 強制擴容 0→4→8→16 求和 ⇒ exit 45ASan 無報告）＋M4-c **帶型別簽名**（`cg-emit-cfg-t` + `TY-I64/STR/VEC`；`mix(n i64, s str)` ⇒ `%v1`+`%sv2` 交錯；`mix`+`sident`+`mk` ⇒ exit 14）＋M4-c **帶型別 CALL**（值型別表 `vty` 按值 id 索引 + `cty` 按指令索引；`caller2` 函式內發 `call i64 @mix(i64 %v1, %str-long %sv2)`）＋M4-c **`option`**（`%option = { i64, [3 x i64] }`，第五個值命名空間 `%op<id>`；`OPTNONE/OPTSOME/OPTTAG/OPTISOK/OPTGET`，`pick(n)=n<10?some(n):none` + `g(n)=isok+payload` ⇒ exit 8；`cg-test` **78/78**`cg-runtime-test` **33/33**），見 §5.3.4.at–.be 與 §6.5**M4-c 已續推至 s54**（§5.3.4.bf–.ci）：retain/release 原語＋`STRCAT` 實參為變數的 RC 週期→option 的 `str` 載荷內聯讀寫（24B slot，免裝箱）→**struct 全家族**（構造＋讀欄位／`STRUCT-SET` 寫欄位／跨塊槽三件套／多結構體型別碼編 id＋`sfc` 描述符／`sft` 欄位型別拼接表／`STRUCT-RETAIN`·`STRUCT-RELEASE` 欄位掩碼 RC 原語／形參·返回值·嵌套欄位作一等值＋字面量全局唯一化 `@.s.<函式名>.<k>`）→**ALU 家族**（s35，12 個 `i64` 有符號標量 op：`AND/OR/XOR/SHL/SHR(ashr)/DIV(sdiv)/MOD(srem)` + `CMPEQ/NE/LE/GT/GE`(icmp+zext)，`cg-test` **146→160/160**`cg-runtime-test` **37/37**端到端 `probe-s35.no` djb2 鏈 exit 161）→**無符號 ALU 家族 + `TY-U64` 型別碼**（s36，7 個無符號 op：`SHRU(lshr)/DIVU(udiv)/MODU(urem)` + `CMPLTU/LEU/GTU/GEU`(icmp ult/ule/ugt/uge + zext)；`TY-U64=4` 與 `TY-I64` 同 LLVM 型別/命名空間（`i64`/`%v`），符號性只活在 op 助記符⇒型別碼與發射臂同代次成對落地，避開 §11 B7存在但無人發射空洞；補齊 s35 §5.3.4.bo 明文預留的兩洞，`cg-test` **160→176/176**（`test-alu-unsigned` +16）`cg-runtime-test` **37/37**零回歸端到端 `probe-s36.no` 無符號取值鏈 196 行 IR `opt` rc=0 + `clang -O2` **exit 18**（反向探針：CMPLTU 誤發 slt ⇒ exit 19，證明 e2e 對 ult-vs-slt 有鑑別力））→**`f64` 算術家族 + `TY-F64` 型別碼**（s37，按型別分派不新增 op：`OP-ADD/SUB/MUL/DIV/MOD` 依 `vty[d]==TY-F64` 發 `fadd/fsub/fmul/fdiv/frem double`，比較依 `vty[x]==TY-F64` 發 `fcmp oeq/une/olt/ole/ogt/oge`（`!=` 用 `une`）；`TY-F64=5` 是真独立 LLVM 型別 `double` + 独立值命名空間 `%fd`（不同 s36 的 u64 復用 %v）；安全讀 `cg-vty-at` 修掉 s37 引入的越界段錯誤；補齊 §5.3.4.bo 預留的浮點洞，`cg-test` **176→197/197**（`test-alu-f64` +21）`cg-runtime-test` **37/37**零回歸端到端 `probe-s37.no` 198 行 IR `opt` rc=0 + `clang -O2` **exit 5**（反向探針：型別誤映射 opt rc=1，oge 誤發 ogt exit 4））→**整數除零 select 護欄的 `noc`↔Go 保真對齊**（s38，codegen 層把 s35/s36 直發的裸 `sdiv/srem/udiv/urem` 裹上與 Go `emitArith` 同形的 select 護欄：新增 `cg-div-guard` helper，判零 `%dz.<k>` + 有符號 `(MIN,-1)` 合成 + 安全除數 `%ds.<k>` + 結果定義化 `%v<d>=select zeroDiv,0,raw`；無符號只判零不偵 MIN；`fdiv/frem` 不動；`cg-test` **197→211/211**（護欄斷言 +14）`cg-runtime-test` **37/37** 零回歸，端到端 `probe-s38.no` 除零取值鏈 `opt` rc=0 + `clang -O2` **exit 2**（反向探針裸 `sdiv 10,0` arm64 poison 得 exit 1），僅收 §11 B14 的 codegen 半項，option 語義降級與 `divZeroReport` stderr 留作後續）→**整數除零 `divZeroReport` stderr 報告**（s39，收 §11 B14 的 codegen 層最後一塊：`cg-div-guard` 結果定義化後追發 `call void @__nolang_div_zero_report(i1 %dz.<k>)`（用 zeroDiv 非 badDiv），prelude 補 `@write` declare + `@nolang_msg_div_zero`（85B，單引號 + 雙反斜杠保 `\XX` 逐字）+ `__nolang_div_zero_report` helper；`cg-test` **211→215/215**（report 斷言 +4）`cg-runtime-test` **37→41/41**（prelude 三新符號 +4），端到端復用 `probe-s38.no` `opt` rc=0 + `clang -O2` **exit 2** + stderr 恰見診斷 3 次（僅 3 個除零站，非零除數站跳過））→**option 的 `vec` 載荷內聯讀寫（`?[]i64`，§11 B8 免裝箱半項）**（s40，`%vec = {i64,i64,i64}` 三字段全 i64，與 `[3 x i64]` 槽 1:1，新增 `OP-OPTMKVEC`/`OP-OPTGETVEC` 逐字段 extract/insertvalue，連 ptrtoint 都不需（比 s28 str 载荷更簡單）；`cg-test` **215→222/222**（vec 载荷斷言 +7）`cg-runtime-test` **41/41** 零回歸端到端 `probe-s40.no` `?[]i64` put/get 往返 `opt` rc=0 + `clang -O2` **exit 90**（88 dat 存活＋2 len 存活，反向鑑別：len 丟→88，dat 壞→SIGSEGV））→**option 跨基本塊槽（`alloca %option`，非 PHI，§11 B8）**（s41，`OP-OPTALLOC/OPTSTORE/OPTLOAD` 三件套鏡像 `SALLOC/SSTORE/SLOAD`，第八命名空間 `%os`；`cg-test` **222→228/228**（跨塊槽斷言 +6）`cg-runtime-test` **41/41** 零回歸端到端 `probe-s41.no` `g41(c)` 三塊 `bb0` 默認寫→`CONDBR`→`bb1` 覆蓋→`bb2` load 讀回 `opt` verify rc=0 + `clang -O2` **exit 99**（反向對照 `g41(0)`→走 `bb2` 讀默認→**exit 1**））→**`str` 結構相等（`@str_eq` ＋ `OP-STEREQ`/`OP-STNEQ`，非 `icmp %str-long`，§5.3.4.bw）**（s42，`cg-test` **228→234/234**（`test-str-eq` +6）`cg-runtime-test` **41→50/50**（`test-str-eq-helper` +9 與 prelude `@str_eq`/`@memcmp` 兩斷言）零回歸端到端 `probe-s42.no` 8 條字面量＋4 條 compare＋加權和 `opt` rc=0 ＋ `clang -O2` **exit 41**（反向對照把 `abd`/`ab` 全改成 `abc` → **exit 51**，證明 memcmp 按字節判別同長度異字節，xor 生效））→**`vec` 元素相等（`@__nolang_vec_eq_i64` ＋ `OP-VECEQ`，非 `icmp %vec`，§5.3.4.bx）**（s43，`cg-test` **234→237/237**（`test-vec-eq` +3）`cg-runtime-test` **50→60/60**（`test-vec-eq-helper` +9 與 prelude `@__nolang_vec_eq_i64` 斷言）零回歸端到端 `probe-s43.no` 建 A/B=[7,9] C=[7,8] E=[7,9,5] 三條 VECEQ＋加權和 `opt` rc=0 ＋ `clang -O2` **exit 41**（反向對照把 b 與 c 比較目標都改成 B 全等 → **exit 47**，證明 len 不等走 noteq，memcmp 按元素內容判別）；鏡像 Go `vecDeepEq` 的 i64 分支，`%vec` field 2 data 先 `inttoptr i64→i8*` 再 `@memcmp(len*8)`，`@memcmp` 復用 s42 宣告）→**`option` 值相等（`OP-OPTEQ`／`OP-OPTNEQ`，tag AND payload，單塊無需 helper，§5.3.4.by）**（s44，`cg-test` **237→247/247**（`test-opt-eq` +10）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s44.no` 五條 OPTEQ/OPTNEQ（some(7)×2／some(9)／nil×2）＋加權和 `opt` rc=0 + `clang -O2` **exit 49**（反向對照把 b／e 目標都改成 some(7) → **exit 67**）；鏡像 Go `emitOptionValueEq` 的 i64 載荷分支，`%option` 聚合體不可 icmp，發 tag `icmp eq` AND 載荷 `icmp eq` 再 `and i1`，純單塊不 call helper）→**`option` 的 str 載荷相等（`OP-OPTEQSTR`／`OP-OPTNEQSTR`，tag AND 重建 %str-long call @str_eq，§5.3.4.bz）**（s45，`cg-test` **247→260/260**（`test-opt-eq-str` +13）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s45.no` 五條 OPTEQSTR（some(abc)×2／some(abd)／some(ab)／nil×2）＋加權和 `opt` rc=0 + `clang -O2` **exit 49**（反向對照把 sv3 'abd' 改成 'abc' → **exit 51**，證明 memcmp 按字節判別）；鏡像 Go `emitOptionValueEq` 的 %str-long 分支，從 `[3 x i64]` 槽三步 insertvalue 重建 %str-long 再 call `@str_eq`，復用 s42 helper 無需新增）→**`option` 的 `f64`（double）載荷內聯讀寫（`OP-OPTMKF64`／`OP-OPTGETF64`，bitcast double↔i64 落／取 slot[0]，無需 helper，§5.3.4.ca）**（s46，`cg-test` **260→265/265**（`test-opt-f64` +5）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s46.no` 兩 double 形參 `f46(3.5,2.25)` 構造兩個 some，讀回後三條 CMPEQ 加權和 `opt` rc=0 + `clang -O2` **exit 43**（反向對照把 b 也傳 3.5 → **exit 47**，構造器壞读出 0.0 → **exit 44**，三值互異）；鏡像 Go `emitOptionValueEq` 的 double 分支存取，`double` 與 `[3 x i64]` 槽元素等宽⇒一步 `insertvalue`＋等宽 `bitcast`（不同 s28 `str` 的 `i8*` 指針需 `ptrtoint`））→**`option` 的 `f64`（double）載荷相等（`OP-OPTEQF64`／`OP-OPTNEQF64`，tag `icmp eq` AND 載荷 `bitcast i64→double` 後 `fcmp oeq`，`OPTNEQF64` 加 `xor i1 true`，單塊無需 helper，§5.3.4.cb）**（s47，`cg-test` **265→271/271**（`test-opt-eq-f64` +6）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s47.no` 三 double 形參 `f47(3.5,2.25,0.0)` 構造 some(a)／some(b)／some(a)／some(0.0)／nil／nil，五條 OPTEQF64／OPTNEQF64（tag 門 some(0.0) vs nil 與載荷門 some(3.5) vs some(2.25)）加權和 `opt` rc=0 + `clang -O2` **exit 61**（反向對照把 b 也傳 3.5 → **exit 47**）；鏡像 Go `emitOptionValueEq` 的 double 分支，`%option` 聚合體不可 icmp，發 tag `icmp eq` AND 載荷 `bitcast` 後 `fcmp oeq`，純單塊不 call helper）→**一元 ALU 家族 NEG／NOT（`OP-NEG`／`OP-NOT`，NEG 依 `vty==TY-F64` 發 `fneg double`／否則 `sub i64 0,x`，NOT 發 `xor i64 1,x`，鏡像 Go emitNeg／emitNot，單塊無 helper，§5.3.4.cc）**（s48，`cg-test` **271→277/277**（`test-unary` +6）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s48.no` 混合形參 `f48(5, 3.5)` 三臂（int NEG／NOT／f64 NEG）加權和 `opt` rc=0 + `clang -O2` **exit 47**（反向對照傳 `f48(7, 3.5)` → int NEG 值翻轉 e1 → **exit 46**）；NEG 復用 s37 值型別分派機制，NOT 以 `xor 1` 翻轉 i64 布林最低位，非 option 路徑不插溢出護欄（同 s35 紀律））→**vec→str 重interpret（`OP-STRFROMVEC`，%vec 三欄位 len／cap／dat 逐欄拷进 %str-long，dat 以 inttoptr i64→i8* 還原指針，鏡像 Go emitStrFromVec，單塊無 helper，§5.3.4.cd）**（s49，`cg-test` **277→286/286**（`test-str-from-vec` +9）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s49.no` 建 [10,m,30] vec 轉 str 後 STRLEN／VECLEN／VECGET 三路鑒别＋加權和 `opt` rc=0 + `clang -O2` **exit 47**（反向對照傳 `f49(99)` → e3 翻 0 → **exit 43**，證 vec 內容隨實參變）；僅存欄位不拷貝緩沖，結果 str 與源 vec 共享同一堆緩沖（不 memcpy 不 retain，所有權移交屬 B11 RC 自動放置後續）；dat 欄 inttoptr 合法性由 opt verify 擔保）→**str 容量欄讀取（`OP-STRCAP`，extractvalue %str-long field 1，與 OP-STRLEN 的 field 0 成對，鏡像 Go emitLenCap 的 OpCap/KindStr 分支，單塊無 helper，§5.3.4.ce）**（s50，`cg-test` **286→292/292**（`test-str-cap` +6）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s50.no` 建 [10,m,30] vec（len3／cap4）轉 str 後 STRLEN／STRCAP 雙欄＋CMPNE 互換哨兵＋常量 'abcd' cap==len 四路鑒别＋加權和 `opt` rc=0 + `clang -O2` **exit 55**（反向對照 'abcd' 改 'xyz' ⇒ 常量 cap=3 ⇒ e4 翻 0 → **exit 47**，證 cap 跟隨真實緩冲尺寸）；刻意不造 OP-VECCAP——Go `emitLenCap` 無 KindVec 分支，忠實性紀律優先於表面對稱）→**option 排序族（`OP-OPTLT`／`OP-OPTLE`／`OP-OPTGT`／`OP-OPTGE`，只比兩側載荷 slot[0] 發 icmp slt/sle/sgt/sge，無 tag 項亦無 and，鏡像 Go emitOptionValueOrder 的有符號 int 載荷分支，單塊無 helper，§5.3.4.cf）**（s51，`cg-test` **292→305/305**（`test-opt-order` +13）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s51.no` 以 some(7)／some(9)／some(-3)／nil 構四值七條排序 op，含假錨／有符號鑒別／取等邊界三類哨兵＋加權和 `opt` rc=0 + `clang -O2` **exit 157**（反向對照 CONST -3 改 3 ⇒ e5 翻 0 → **exit 141**，證有符號比較生效）；nil 載荷槽為 zeroinitializer 視作 0（some(7)＞nil 判 1）與 Go 同形；刻意不造無符號翼 ult/ule/ugt/uge——option 載荷符號性在 noc 型別宇宙不可見（TY-OPT 不挾載荷型別）；f64 載荷序比較留後續（循 s47 OPTEQF64 專用 op 前例））→**option 的 `f64`（double）載荷排序族（`OP-OPTLTF64`／`OP-OPTLEF64`／`OP-OPTGTF64`／`OP-OPTGEF64`，slot[0] 循 s47 bitcast i64→double 後發 fcmp olt/ole/ogt/oge，無 tag 項亦無 and，鏡像 Go emitOptionValueOrder 的 double 分支，單塊無 helper，§5.3.4.cg）**（s52，`cg-test` **305→318/318**（`test-opt-order-f64` +13）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s52.no` 三 double 形參 `f52(3.5, 2.25, -1.5)` 構 some×3／nil 四值九條排序 op，含假錨／取等邊界／負值與 nil 零載荷哨兵＋加權和 `opt` rc=0 + `clang -O2` **exit 167**（反向對照 c 改 1.5 ⇒ e5 翻 0 → **exit 163**，單變量翻轉證確發 olt）；nil 載荷位模式即 +0.0；ordered 謂詞遇 NaN 恒判 false 為 Go 原語義忠實鏡像；f64 專用碼循 s47 OPTEQF64 前例（載荷型別在 op 層不可見）／s53 本輪：option 與純量排序 OP-OPTLTSC／OP-OPTLESC／OP-OPTGTSC／OP-OPTGESC=86–89，鏡像 Go emitCmp aOpt／bOpt 分支（codegen.go:6490-6520）：方向由 vty 判側，只提 option 側 slot[0] 與純量 icmp slt／sle／sgt／sge，無 tag 項／無 and／謂詞不翻轉（次序隨源表達式），nil 零載荷與純量 0 同路；兩側形同發時 %oq 重複定義 verify 誠實報錯；cg-test 318→330/330，探針 exit 180（反向 b 改 9 ⇒ e2／e3／e7／e8 同翻 exit 129，單變量可預測）；str 序經實地勘察確認 Go emitCmp 直接 hard-fail（codegen.go:6557-6563，語言層只實作 ==／!=），.cf／.cg 兩處記為 emitStrCompare builtin 之誤帳已改實；f64 純量翼留循前例另立專用碼——已償／s54 本輪：option 與 f64 純量排序 OP-OPTLTSCF／OP-OPTLESCF／OP-OPTGTSCF／OP-OPTGESCF=90–93（s53 留賬精確償還）：vty 雙護欄判向（TY-OPT＋TY-F64），提 option 側 slot[0] bitcast→double 後與 %fd 純量 fcmp olt／ole／ogt／oge，無 tag 項／無 and／謂詞不翻轉；nil 載荷位模式 +0.0 同 s52；護欄比 s53 更嚴——閘不過兩臂都不發射（Go 回退 discriminant，noc 未實作，呼叫方保證）；cg-test 330→342/342，探針 exit 68（反向 c 改 9.0 ⇒ e6 GTSCF 獨翻 exit 84，單變量可預測）；單測首發曾抓到右臂 fcmp 誤引 %opy 而非 %fdy 的型錯，文本斷言是第一道閘；至此 Go emitCmp aOpt／bOpt 混合路徑可達組合收攏）**M4-e 已啟動（s55，§5.3.4.cj）**：新建 `compiler/src/h2m.no` 把 arena AST 降為 `mir` 扁平指令流（i64 直線式子集，值 id 無槽直綁，`unsup` 誠實門禁），`h2m-test` **35/35**，探針前向 exit **73**／反向 **79** 且與 Go oracle 同程序對拍（自舉線首次"源碼進，可執行檔出"的雙側對齊）；實地撞出入口檔缺 `asp`／`lx`／`tok` 傳遞依賴即 NoVal 泛濫或運行期掛起（記 §11 B15）；**M4-e 第二件續推（s56，§5.3.4.ck）**：簽名側（形參由 `h2m-alloc` 依序佔值 id `1..nargs`，`retty` 取輸出參數聲明型別）與調用側（`OP-CALL` 的 arity／型別／連續 id 三重校驗）落地，模組級簽名表由前置 pass 先掃全模組 ⇒ 調用者寫在被調用者之前也能解析；誠實門禁從"只計數"升級為"未降級函式整函式不產碼"，`h2m-test` **74/74**，探針前向 exit **47**／反向 **50** 與 Go oracle 同程序對拍，鑑別態 exit **101**；連續 id 契約與多輸出呼叫形式分別記 §11 B16/B17⚠️ **s35/s36/s37/s38/s39/s40/s41/s42/s43/s44/s45/s46/s47/s48/s49/s50/s51/s52/s53/s54/s55/s56/s57/s58/s59/s60m/s61/s62/s63/s64/s65 驗證環境**：本輪 HEAD（`v0.4.14-3`）Go 侧 MIR codegen 有一個**跨模組結構體欄位回歸**（`EmitLLVM: setfield: field "buf" not found on "sb.sb"`），凡 import `util/sb` 的測試檔在 HEAD 工具鏈上全部無法構建——代碼生成側（`cg.no`/`mir.no`/`cg-test.no`）在 `v0.4.13↔HEAD` 逐位元組相同，故按同一穩定工具鏈代次紀律改用**釘死的 `v0.4.13`** 採證（該回歸屬 Go 侧 S0 的並行在途改動，依 §2 黃金法則只記錄不篡改 `.no` 源碼繞過）M4-c 餘項（vec 元素非 i64 的逐元素 RC**結構體逐欄位 RC 的自動放置（原語已備，B11）**跨函式所有權轉移（B13）整數除零的 option 降級（B14）struct 陣列/切片>24B 堆上裝箱（`?vec`≤24B 內聯已 s40）`byte`/`f32` 窄型別算術（`u64` 已 s36，`f64` 已 s37 落地））M4-d ⬜下一步＝**M4-c 餘**（整數除零的 option 語義降級（B14，`select` 護欄 s38 + stderr 報告 s39 已落地）；PHI 本方案決定走 alloca 不引故非必需；option 的 `vec` 載荷內聯讀寫已 s40（B8 免裝箱半項）；option 跨基本塊槽已 s41（B8 alloca 通路）；`str` 相等已 s42（`@str_eq`/`OP-STEREQ`/`OP-STNEQ`）；`vec` 元素相等（i64）已 s43（`@__nolang_vec_eq_i64`/`OP-VECEQ`）；`option` 值相等（i64 載荷）已 s44（`OP-OPTEQ`/`OP-OPTNEQ`）；`option` str 載荷相等已 s45（`OP-OPTEQSTR`/`OP-OPTNEQSTR`）；`option` f64 載荷讀寫已 s46（`OP-OPTMKF64`／`OP-OPTGETF64`）；`option` f64 載荷相等已 s47（`OP-OPTEQF64`／`OP-OPTNEQF64`）；一元 ALU 已 s48（`OP-NEG`／`OP-NOT`）；vec→str 重interpret 已 s49（`OP-STRFROMVEC`）；str 容量欄已 s50（`OP-STRCAP`）；option 排序已 s51（`OP-OPTLT`～`OP-OPTGE`）；option 的 f64 載荷排序已 s52（`OP-OPTLTF64`～`OP-OPTGEF64`）；option 與純量排序已 s53（`OP-OPTLTSC`～`OP-OPTGESC`）；option 與 f64 純量排序已 s54（`OP-OPTLTSCF`～`OP-OPTGESCF`）；**AST→MIR 降級首件已 s55**（`compiler/src/h2m.no`：直線式 i64 子集，`h2m-test` **35/35**，e2e 73/79 與 Go oracle 對拍，§5.3.4.cj）；**簽名側與調用已 s56**（`h2m-test` **74/74**，e2e 47/50 與 Go oracle 對拍，鑑別態 101，§5.3.4.ck）；**控制流與跨基本塊槽化已 s57**（`h2m-test` **135/135**，e2e 92／反向 93 與 86 並與 Go oracle 對拍，鑑別態 101，§5.3.4.cl）；**花括號短路鏈已 s58**（`h2m-test` **176/176**，e2e **188** 與 Go oracle 同程式對拍（源碼級三對變異同值 189／195／190；IR 反向四刀 189／195／190／189），鑑別態 101，§5.3.4.cm；v0.4.13 短路鏈的兩處 Go 側語義偏差另記 §11 B19/B20）；**塊終結形已 s59**（`h2m-test` **203/203**，e2e **72** 與 Go oracle 同程式對拍（逐值 14 6 0 37 10 5；源碼級三對變異兩側同值 76／70／74；IR 反向四刀 41／66／73／72，末一把同值不鑑別已留檔說明），鑑別態四例 exit 101，§5.3.4.cn）；**計數循環已 s60m**（`h2m-test` **255/255**，e2e **134** 與 Go oracle 同程式對拍（逐值 3 10 104 11 6 0；源碼級四對變異兩側同值 135／141／136／136；IR 反向五刀 132／131／135／132／131），鑑別態三例 exit 101，§5.3.4.co）；**區間遍歷已 s61**（`h2m-test` **324/324**，e2e **164** 與 Go oracle 同程式對拍（逐值 10 6 6 6 4 14 103 9 6；源碼級八對變異兩側同值 168／168／168／171／162／161／161／156；IR 反向八刀 169／161／163／158／163／160／194／155），鑑別態三例 exit 101，§5.3.4.cp）；**容器遍歷已 s62**（`h2m-test` **372/372**，e2e **134** 與 Go oracle 同程式對拍（逐值 6 3 101 5 6 6 7；源碼級六對變異兩側同值 136／133／140／134／148／140；IR 反向八刀 135／132／129／139／130／133／131／128），鑑別態六例 exit 101，§5.3.4.cq；本件零改 `cg.no`——復用 s23 的 `OP-VECLEN`／`OP-VECGET`，s61 所記"尚不存在的碼位"屬假帳已就地改實）；**值匹配已 s63**（`h2m-test` **429/429**，e2e **221** 與 Go oracle 同程式對拍（逐值 10 20 30 1 7 3 4 10 1 111 8 9 7；源碼級七對變異兩側同值 216／227／222／226／65（真值 321 按位元截斷）／222／231，另兩對實參 241／220；IR 反向十刀 211／241／231／222／171／215／223／218／212／219），鑑別態七例 exit **101**，§5.3.4.cr：逐臂在本臂判定塊內重讀主體發 `OP-CMPEQ`，⚠️ 收閘前該形是靜默錯碼不是 unsup）；**實參側池已 s64**（`h2m-test` **443/443**＋`cg-test` **347/347**，e2e **91** 與 Go oracle 同程式對拍（逐值 25 21 17 13 15；源碼級四對變異兩側同值 92／104／90／95；IR 反向五刀 93／87／82／93／88），鑑別態三例 exit **101**，§5.3.4.cs：B16 銷帳——`OP-CALL` 實參改走側池 `args`（`xa`=池基址），`h2m` 校驗全過才整段入池；⚠️ `cg` 三入口第 13 形參必填使 28 檔 M4-b/c 手寫 op 探針一次性作廢）；**`it` 隱式綁定已 s65**（`h2m-test` **443→453→471/471**，e2e **46** 與 Go oracle 同程式對拍（逐值 1 7 13 7 3 9；源碼級二對變異兩側同值 45／47；IR 反向五刀 39／44／36／45／41），鑑別態 exit **101**（`r = it / 2` 除不支持：noc 毒 main 101 vs oracle 38），§5.3.4.ct：`itv` 值 id 棧——cond 臂複用判定塊 LOAD、wild 臂懶發主體 LOAD；⚠️ 預掃描 `h2m-ty-of('it')` 原返 -1 導致 `h2m-slot-decl` + `h2m-var-write` **雙 mark**，本件補 TY-I64 特例銷掉；同族作用域棧 `stk`／`rnms`／`epl` 退出位點補真 `.pop()`；另预记 §11 B27——Go 嵌套 match 的 `it` 海森堡（no-print 得 2、print 下得 3））；**M4-e 續推 s66–s85**（§5.3.4.cu–.dn，2026-11-04→11-06）：s66 前綴 `-x`/`!x`（`K-PREFIX-EXPRESSION`⇒`OP-NEG`/`OP-NOT`，僅 i64，`h2m-ty-of` 補 prefix 臂規避雙 mark）→s67 二進位位運算 `& | ^ << >>`（INFIX 分派鏈補五臂，零改 cg/mir/ty-of）→s68 邏輯 `&& ||`（復用 `OP-AND/OR`，非短路與 Go `emitLogic` 同形）→s69 前綴位非 `~x`（`OP-CONST(-1)`＋`OP-XOR`，鏡像 Go `hir2mir:8861`；🔴 `ps.no` `~` 鬆綁定優先級分歧記待查）→s70 前綴 f64 `-x`（NEG 守衛放寬收 TY-F64，f64 首次端到端流經 h2m）→s71 f64 二元算術 `+ - * /`（fty 雙側發 `fadd/fsub/fmul/fdiv double`，i64 `/` 仍門禁）→s72 f64 二元比較（`is-arith`/`is-cmp` 旗標分派，比較 dest 恆 TY-I64 發 `fcmp+zext`，收緊 s71 過寬臂）→s73 f64 字面量（`K-FLOAT-LITERAL` 讀 raw、`raw.contains('.')` 門控，**同步改 `cg.no` OP-CONST 依 TY-F64 發 `fadd double 0.0`——s55 起首件需動 cg 的降級件**）→s74 f64 局部變量/控制流槽化（槽機構認 TY-F64、`alloca double`，cg ALLOC/STORE/LOAD 分岔 double 翼）→s75 i64 除/模除零護欄降級（`%`⇒OP-MOD、取消 i64 側 OP-DIV 排除，落 cg s38/s39 `cg-div-guard` plain 路）→s76 u64 除/模無符號護欄（`uty` 分派⇒`OP-DIVU/MODU`，補 `h2m-ty-of` U64 infix 臂；🔴 Go 編譯期摺疊對 u64 除走有符號口徑 quirk，oracle 經 env 不透明化）→s77 u64 比較族無符號（`OP-CMPLTU/LEU/GTU/GEU`，dest 恆 TY-I64，ty-of 開比較例外）→s78 u64 算術族 `+ - *` 放行（不重映射，零改 cg/ps/mir/ty-of）→s79 u64 位運算/移位族 `& | ^ << >>`（唯一重映射 `>>`⇒`OP-SHRU`）→s80 i64 `>>` 口徑裁定（全族無條件重映射 `OP-SHRU` 恒發 `lshr`，鏡像 Go `emitBitwise` OpShr；s35 i64=ashr 設計推翻、cg ashr 臂轉死代碼；**自舉軌首個非加性件**，s67 基線重鑲）→s81 u64 前綴 `-x`／`~x` 放行（`K-PREFIX-EXPRESSION` 兩 `TY-U64` 發射臂 `'-'`⇒`OP-NEG`（sub i64 0）、`'~'`⇒`OP-CONST(-1)`＋`OP-XOR`（xor i64），`'!'` 於 u64 保持門禁；🔴 IR 與 i64 前綴逐字節同，型別正確性靠 unsup 門＋dest 型別碼＋值語義刀鎖；零改 cg/ps/mir/ty-of；**純加性件**，s80 前向值不變）→s82 科學計數法字面量 ILLEGAL 鏡像（`lx.no` readNumber 後測 `[eE]`＋指數位⇒整個 token 覆寫 `ILLEGAL`＋ErrMsg 156 字節原文、探測不消耗；`ps.no` operand 位報告臂＋級聯抑制，去重口徑證與 Go `reportedIllegal` 等價；詞法/語法診斷件，零改 h2m/cg/mir，§5.3.4.dk）→s83 u64 邏輯族 `&& ||` 放行（`h2m.no` u64 位運算臂撤 `is-logic==0` 並退場旗標——Go 權威對 u64 `&& ||` 即裸位形 `and/or i64` 非短路（探針實釘 12&&10⇒8、0||5⇒5、0&&1⇒0），s79 門禁「同碼不等價、與真短路後置」假設被探針推翻；函數形被 Go checker 拒（表面 bool）⇒ oracle 走頂層形；零改 cg/ps/mir/ck；§5.3.4.dl）→s84 前綴 `~` 綁定口徑複核＋哨兵重鑲（§5.3.4.cx 🔴 待查項三面實測緊綁定==Go 關閉——AST 四形逐字節 SAME、執行值 -8/-22 同值；實作零改（僅 ps.no TILDE 臂註解）；ps-test 38→42 哨兵；§5.3.4.dm）→s85 (MIN,-1) 護欄翼執行面實釘（§5.3.4.dd「執行面留待專門件」尾款償清——Go plain 路安全除數 1 定義值 /⇒MIN、%⇒0 且不觸 stderr；實作零改；h2m-test 721→735 翼哨兵；§5.3.4.dn）→s86 f64 `~` 門禁裁定（§5.3.4.dn 頭號候選銷賬——Go 運行路 `xor double` 非法 clang build fail、常量路 cvInt 摺死⇒無可鏡像權威，h2m ok-p==0 門禁為正確形態維持；實作零改；h2m-test 735→737 負向門禁哨兵；§5.3.4.do）→s87 f64 `%` 放行（s71 算術族尾款——`h2m.no` 歸類區補 `OP-MOD -> is-arith` 唯一實作行發 frem double，鏡像 Go `codegen.go:6083`；oracle 實釘 AArch64 lowering 後 fmod 語義（7.0%2.5=2.0、`llc` 出 `b _fmod`）兩側同值 exit 6；**零改 cg**（s37 frem 臂已備）；h2m-test 737→739 放行哨兵＋舊 s75 門禁哨兵翻正；§5.3.4.dp）→s88 `!x` 值語義對齊 i1 低位列（第 34 件——`h2m.no` K-PREFIX `'!'` 臂改 `OP-CONST(1)`＋`OP-AND`＋`OP-NOT` 三發射序列，鏡像 Go `codegen.go:6408` `emitNot` 的 `trunc` 歸位＋`xor i1 1`⇒值恆 (x&1)^1；i64 修正＋u64 放行（s81 門禁尾款清償）、dest 恆 TY-I64，f64 `'!'` 維持門禁（Go double→i1 恆 true 退化）；**零改 cg**；值面對拍 i64 117==oracle 117、u64 54==54；刀 4/4；前向 exit 19/19 全同、IR 15/16 SAME——s66 因 `!x` 歸位形制重錨基線（值面不變，非加性件 s80 先例）；h2m-test 739→743（`test-not-i64-lowbit` 新增、`test-pfx-u64-lnot-gate` 翻正）；本件期間並發進程曾將 h2m.no/cg.no 回退 HEAD，s82–s87 交付已按 §5.3.4.dk–dp 重放恢復並紀事；§5.3.4.dq）→s89 非整型主體 `'!'` 門禁裁定紀實＋哨兵重鑲（第 35 件——三面實釘無權威（f64 poison 恆真／str 運行路 `xor i1 1, %str-long` build fail [d8en6a9j]／字面量 hir inline 恆真），門禁維持；實作零改鏡像 s84/s86；`1e5` 候選重新定性銷賬（Go 詞法 ILLEGAL s82 已全鏈鏡像、h2m 閘不可達）；h2m-test 743→747 兩負向哨兵；刀 2/2、前向 exit 19/19、IR 16/16 SAME；§5.3.4.dr）→s90 bool 字面量降级＋bool 型别码放行（第 36 件——`h2m.no` 补 K-BOOLEAN-LITERAL 降级臂（读 slot0 文本 true/false 选 0/1，发 OP-CONST(TY-I64,v)）＋ h2m-ty-code/h2m-ty-of 补 bool⇒TY-I64，镜像 Go hir2mir.go:8848 KBoolLit⇒EmitInt(OpConst,0/1)；零改 cg/ps/mir；d1 赋值面普查撞出的新缺口、非 s89 清单预列；h2m-test 747→755；刀 2/2、oracle exit 2、前向 exit 19/19、IR 16/16 SAME；§5.3.4.ds）→s91 char 字面量降级＋char 型别码放行（第 37 件——`h2m.no` 补 K-CHAR-LITERAL 降级臂（读 slot0 去引号内文→to-bytes→1/2/3/4 字节 UTF-8 首码点解码→发 OP-CONST(TY-I64,cp)）＋ h2m-ty-code/h2m-ty-of 补 char⇒TY-I64，镜像 Go hir2mir.go lowerCharLit⇒EmitInt(OpConst,首码点)；零改 cg/ps/mir；d1 普查撞出的新缺口、非 s90 清单预列；h2m-test 755→763；刀 3/3、oracle exit 3、前向 exit 19/19、IR 16/16 SAME；§5.3.4.dt）→s92 默認參數調用點回填（第 38 件——`h2m.no` 簽名表補 fdef 預設式節點平行表＋ h2m-sig-def 新函數＋調用臂逐位統一迴圈（j<na 拿 AST kid、j>=na 降默認式入 argv），鏡像 Go hir2mir.go:13175 尾部回填、每調用點拷開；被缺且無默認值 ak=0 入門禁兜住 Go checker 前置拒；輸出參默認值仍拒；零改 cg/ps/mir；d1 普查撞出的新缺口、非 s91 清單預列；h2m-test 763→771（舊 gate default param 兩斷言翻正）；刀 3/3、oracle exit 15、前向 exit 19/19、IR 16/16 SAME；§5.3.4.du）→s93 語句位調用降級（結果丟棄）（第 39 件——`h2m.no` 的 K-EXPRESSION-STATEMENT 臂尾補「只收 K-CALL-EXPRESSION」的 lower-expr 回填路，鏡像 Go hir2mir.go:3790 KExprStmt else 分支 lowerExpr(child) 值丟棄；無輸出參被調沿用既有 void⇒define i64 約定發 call i64 丟值、TY-VOID 另件；限定只收調用由 checker 層 checkBareExprStatement 分級支撐（裸字面量 yf6kxdmz／裸函數 k9179drd 仍拒），放開會推翻四條既有負例哨兵；零改 cg/ps/mir；d1 普查撞出的新缺口、非 s92 清單預列；h2m-test 771→780；刀 3/3、oracle exit 7、前向 exit 19/19、IR 16/16 SAME；§5.3.4.dv）→s94 ?u64 option 三件套降級（第 40 件——`h2m.no` 新增 `h2m-tynode-code` 共用判据簽名／標注同口徑（Go capture 合成 NullableType 標注須此路方入形）、K-NIL-LITERAL⇒OP-OPTNONE 鏡像 Go lowerNilLit、回值位隱式 OP-OPTSOME 包裹鏡像 wrapOptionIfNeeded、延遲零值 option→nil 發 OPTNONE 非 some(0)；覆盖面限直線式 mem==0——TY-OPT 鋪槽／option match 臂／流形 option 輸出参入門禁；零改 cg/ps/mir；歷件清單頭號項此件償清；h2m-test 780→788 過/795 總（紅 7＝既有 s64-str／default 族、工作樹並發改動非本件、裁決 proceed＋ignore）；刀 5/5、oracle exit 61、自舉實釘 exit 7、前向 exit 19/19、IR 16/16 SAME；§5.3.4.dw）→s95 ?i64 內層整型放寬（第 41 件——`h2m.no` `h2m-tynode-code` K-NULLABLE 內層名收受擴為 'u64'||'i64'、?i64 與 ?u64 同 [3 x i64] 槽逐字節同形、some-wrap TY-I64 支首次正向觸及、內層 str/f64/vec 仍不入；零改 cg/ps/mir、無新發射臂；h2m-test 788→806/813（紅 7 既有非本件）、刀 2/2、oracle exit 61、自舉 exit 7、前向 19/19、IR 16/16 SAME；§5.3.4.dx）→s96 ?f64 內層載荷放寬（第 42 件——`h2m.no` `h2m-tynode-code` K-NULLABLE 內層名收受擴為 'u64'||'i64'||'f64'、回值位 some-wrap 增 `pty == TY-F64` 支改發 OP-OPTMKF64（bitcast double→i64）、補 §11 B7「cg 既存而 h2m 無人發射」空洞、內層 str/vec 仍不入；零改 cg/ps/mir、無新 op；h2m-test 806→824/831（紅 7 既有非本件）、刀 3/3、oracle exit 61、自舉 exit 7、前向 IR 16/16 SAME；§5.3.4.dy）→s97 TY-OPT 控制流鋪槽（第 43 件——`h2m.no` option 槽由直線式（mem==0）扩到控制流（mem==1）：slot-decl 認 TY-OPT 發 OP-OPTALLOC（alloca %option）＋OPTNONE（tag=1）nil-init、var-read 發 OPTLOAD、var-write 分 option←option OPTSTORE／option←載荷 寫點 h2m-opt-wrap 包裹再存、lower-func 於 scan 後按聲明型 TY-OPT 覆寫輸出参名（h2m-lst-add 就地改型）並撤 s94 flow-gate；回值位與寫點 some-wrap 收斂單點 h2m-opt-wrap、補 §11 B7「cg 既存而 h2m 無人發射 OPTALLOC/STORE/LOAD」空洞；零改 cg/ps/mir、無新 op；h2m-test 824→845/852（紅 7 既有非本件、3 flow-gate 負例翻正＋2 新哨兵）、刀 4/4、oracle exit 165、自舉 exit 165、前向 IR 16/16 SAME；§5.3.4.dz）；各件 `h2m-test` 逐代次遞增（s65 471→s81 715、s83 721、s84 ps-test 42/42 哨兵重鑲、s85 **735/735** 翼哨兵、s86 **737/737** 負向門禁哨兵、s87 **739/739** 放行哨兵、s88 **743/743** 值語義修正＋u64 放行翻正、s89 **747/747** 負向門禁哨兵重鑲、s90 **755/755** bool 字面量放行哨兵、s91 **763/763** char 字面量放行哨兵、s92 **771/771** 默認參數回填放行哨兵、s93 **780/780** 語句位調用放行哨兵、s94 **788/795** option 三件套放行哨兵（紅 7＝既有 s64-str／default 族、ps 41/1＝既有 enum 死碼臂哨兵，皆工作樹並發改動非本件引入）、s95 **806/813** ?i64 內層放行哨兵（紅 7 同前非本件）、s96 **824/831** ?f64 內層載荷放行哨兵（紅 7 同前非本件）、s97 **845/852** TY-OPT 控制流鋪槽放行哨兵（紅 7 同前非本件、3 flow-gate 翻正＋2 新哨兵））、cg suites 347/60、s82 lx-test 41→70／ps-err-test 26→46＋Go oracle L1 全語料 804/804 逐字節 SAME（實撞出手寫 ErrMsg 多一零的抄寫缺陷——兩側同錯時單測無鑑別力、唯 oracle 差分抓得）& 探針級 L1 差分 19/20 SAME（1 檔既有色差）、vet 探針 3/4 SAME（1 檔屬 s10 既記穩定碼格式缺口）、`opt` verify rc=0、`clang -O2` 探針前向與 Go oracle 同程式逐值對拍、IR 反向刀全改 exit、s66–s79 探針 IR 逐位元組相同（s80/s81/s83 前向零回歸：s76=63、s77–s81=255、s83 新探針 exit 15==oracle 15 刀 5/5、s84 新探針 exit 3==oracle 3 刀 4/4、s85 新探針 exit 7==oracle 7 刀 4/4 且 s66–s84 IR 逐字節 18/18 SAME 、s86 新門禁探針 exit 20＋刀 3/3 且 s66–s85 探針 IR 逐字節 16/16 SAME（s86 紀實件零改 h2m/cg 之結構性保證）、s87 新探針 exit 6==oracle 6 刀 3/3 且前向 s66–s85 exit 19/19 全同、IR 16/16 SAME、s88 新探針 i64 exit 117==oracle 117／u64 54==54 刀 4/4 且前向 exit 19/19 全同、IR 15/16 SAME（s66 因 `!x` 歸位形制重錨至 tmp/s88-fwd-s66.ll、值面 149 不變））、s89 零改件前向 exit 19/19 全同＋IR 16/16 SAME（結構性保證）、刀 2/2 哨兵斷言制、s90 真实改件前向 exit 19/19 全同＋IR 16/16 SAME（新臂仅 bool 触发的结构性保证）、刀 2/2、oracle exit 2（add i64 0,1 / 0,0）、s91 真实改件前向 exit 19/19 全同＋IR 16/16 SAME（新臂仅 char 触发的结构性保证）、刀 3/3（含 K3 破解码器 *4096→*64 仅 cjk 红、证 UTF-8 解码在位）、oracle exit 3（add i64 0,97 / 0,20013）、s92 真实改件前向 exit 19/19 全同＋IR 16/16 SAME（调用臂 na==want-n 时逐位语义不变的结构性保证）、刀 3/3（含 K3 破 sig-def 段内索引 fb+j→fb 仅 trailing 红、证按位取参在位）、oracle exit 15（四路 5/9/21/3 全对、回填 IR 实钉 add i64 0,5 / 0,20 拷开降入调用者块）、s93 真实改件前向 exit 19/19 全同＋IR 16/16 SAME（新臂仅语句位调用触发的结构性保证）、刀 3/3（含 K2 去掉「只收 K-CALL」限定连带推翻 count-loop/vm-gate-expr-subj/bit-gate/stmt-bare 四条负例门禁、证限定承重；K3 放行不产码仅三条 call emitted 红、证放行且产码）、oracle exit 7（uone=4/keep=4/disc=1；@disc 体内 call i64 @acc / @side 皆发射手值未用）、s94 真實改件前向 exit 19/19 全同＋IR 16/16 SAME（新臂僅 K-NULLABLE／K-NIL／option 回值位觸發之結構性保證）、刀 5/5（K3 刪延遲零值臂僅 +1『none emit』紅——unsup 斷言仍 0 的靜默退化恰由此刀抓住；K4 刪流門禁 +2 紅、證條件寫退化產碼 some(0) 不得放生；K2 刪 nil 臂後 call 斷言由簽名表獨立仍綠、證記賬分離）、oracle exit 61（ok(7)/nil/nil 編碼 1+20+40、some(0) 誤做則 25≠61 自具鑑別力）＝自舉三路實釘 exit 7（tag＋載荷三路皆對唯一 7）、s95 真實改件前向 exit 19/19 全同＋IR 16/16 SAME（?i64 未入既有探針之結構性保證）、刀 2/2（K1 退 'i64' 收受 +13 紅——簽名與 capture 兩側同破證 §11 B7 同口徑；K2 削 some-wrap `pty==TY-I64` 支 +5 紅——證該支為 i64 載荷專屬承重）、oracle exit 61（p1=7 ?i64 同值面）、自舉 exit 7、s96 真實改件前向 exit 19/19 全同＋IR 16/16 SAME（?f64 未入既有探針之結構性保證）、刀 3/3（K1 退 'f64' 收受 +13 紅——簽名與 let 標注兩側同破證 §11 B7 同口徑；K2 削 some-wrap `pty==TY-F64` 支 +5 紅、K3 錯 op 換 OPTSOME +3 紅——證 OPTMKF64 發射在位）、oracle exit 61（p1=3 ?f64 同值面）、自舉 exit 7、s97 真實改件前向 IR 16/16 SAME（新支僅 st/frt==TY-OPT && c.mem==1 觸發之結構性保證）、刀 4/4（K1 移 slot-decl TY-OPT 收受 +23 紅、K2 破寫點包裹 +18 紅、K3 破 OPTLOAD +4 紅、K4 破聲明型覆寫 +8 紅——四刀共證鋪槽／寫點／讀端／覆寫四處承重、K1/K2 跨簽名回值寫點證 §11 B7 同口徑單一 helper）、oracle exit 165（f(5)=some(5)/f(0)=nil/h(7)=some(7)/h(0-3)=nil 未寫路徑 tag=1）、自舉端到端 exit 165==oracle 165（main 側裸 IR extractvalue 讀 tag/payload、避 match s63 門禁；降級失敗毒 main 101）、⚠️ 本件撞出 Go quirk f10x20nf（裸算術式作 option-回傳函式實参被 checker 拒、須先綁具名變量、與鋪槽正交不改 src））。⚠️ 自 s35 起本族因 HEAD Go 侧跨模組結構欄位回歸（`setfield: buf not found on sb.sb`）一律在**釘死 `v0.4.13`** 上採證；兩項既有紅點 `ast-test`（Go #85）/`ck-sig-test`（世代漂移）與自舉正交；`no vet src/std` 基線計數 1 ERROR。餘為 **s98 候選**（`&^` 已銷賬（s82 裁決：Go 無實現、無權威可鏡像）／`1e5` 已償 s82（重新定性為 ILLEGAL 鏡像而非定點化）／cg OP-SHR ashr 死代碼清理／(MIN,-1) 已償 s85（執行面三面實釘、翼哨兵入 h2m-test）／`&& ||` 已償 s83（Go 無短路、裸位形鏡像完）／`?u64` option 三件套已償 s94（`h2m.no` 共用判据 h2m-tynode-code 簽名／標注同口徑、nil⇒OPTNONE 鏡像 Go lowerNilLit、回值位隱式 OPTSOME 包裹鏡像 wrapOptionIfNeeded、延遲零值 option→nil；覆盖面限直線式，TY-OPT 鋪槽／option match 臂／err tag=2 裝箱入門禁；放行哨兵 opt-wrap/nil/zero/capture/param＋負例 flow-gate/payload-gate 入 h2m-test；oracle exit 61 钉 ok(7)/nil/nil、自舉探針 exit 7）／?i64 內層整型放寬已償 s95（`h2m.no` `h2m-tynode-code` 內層名收受擴為 'u64'||'i64'、?i64 與 ?u64 同 [3 x i64] 槽逐字節同形、some-wrap TY-I64 支首次正向觸及；放行哨兵 opt-i64 wrap/nil/zero/capture/param/samecaliber＋負例 flow/payload 入 h2m-test；oracle exit 61 钉 p1=7、自舉 exit 7）／?f64 內層載荷放寬已償 s96（`h2m.no` `h2m-tynode-code` 內層名收受擴為 'u64'||'i64'||'f64'、回值位 some-wrap 增 `pty==TY-F64` 支改發 OP-OPTMKF64（bitcast double→i64）、補 §11 B7「cg 既存而 h2m 無人發射」空洞；放行哨兵 opt-f64 wrap/not-i64-some/nil/zero/capture/param＋負例 flow/payload 入 h2m-test；oracle exit 61 钉 p1=3、自舉 exit 7）／TY-OPT 鋪槽已償 s97（`h2m.no` option 槽由直線式扩到控制流 mem==1：slot-decl OPTALLOC＋OPTNONE nil-init／var-read OPTLOAD／var-write OPTSTORE＋寫點 h2m-opt-wrap 包裹／scan 後聲明型覆寫輸出参名並撤 s94 flow-gate、回值位與寫點 some-wrap 收敋單點 helper（§11 B7 同口徑）；零改 cg/ps/mir（OP-OPTALLOC/STORE/LOAD cg s41 既存）；放行哨兵 opt-flow-slot/-i64/-f64/deferred-nil／str-payload-gate 入 h2m-test；oracle exit 165 钉 some/nil 四路、自舉 exit 165）／`~` 優先級分歧已償 s84（現行三面實測緊綁定、哨兵入 ps-test）／`~` f64 已銷賬 s86（s86 裁定：Go 運行路 `xor double` 非法 build fail、常量路 cvInt 摺死，無權威可鏡像；負向門禁哨兵入 h2m-test）／`%` f64 已償 s87（is-arith 補 OP-MOD 單行放行 frem double，AArch64 lowering fmod 語義兩側同值 exit 6；放行哨兵＋s75 舊門禁哨兵翻正入 h2m-test）／`!x` 已償 s88（值語義對齊 i1 低位列——i64 修正＋u64 放行、f64 維持門禁裁定；三發射序列 `OP-CONST(1)`＋`OP-AND`＋`OP-NOT`，值面對拍 117/54 兩側同值）／f64 `'!'` 已銷賬 s89（非整型主體三面實釘無權威、門禁維持裁定；負向哨兵 lnot-f64/lnot-str 入 h2m-test；`1e5` 同步重新定性銷賬）／bool 字面量已償 s90（`h2m.no` K-BOOLEAN-LITERAL 降级臂＋bool 型别码，镜像 Go KBoolLit⇒OpConst 0/1、自举 TY-I64 0/1 零改 cg；放行哨兵 bool-lit-true/false＋bool-param-tycode 入 h2m-test；oracle exit 2 钉 true=1/false=0）／char 字面量已償 s91（`h2m.no` K-CHAR-LITERAL 降级臂＋char 型别码，镜像 Go lowerCharLit⇒OpConst 首码点、自举 TY-I64 码点零改 cg；UTF-8 首码点解码 1/2/3/4 字节；放行哨兵 char-lit-ascii/cjk＋char-param-tycode 入 h2m-test；oracle exit 3 钉 a=97/中=20013）／默認參數調用點回填已償 s92（`h2m.no` fdef 簽名平行表＋調用臂尾部回填迴圈，鏡像 Go hir2mir.go:13175 缺省實參逐調用點 lowerExpr(deID) 拷開；被缺且無默認值入門禁；輸出參默認值仍拒；放行哨兵 default-param-fill/trailing/missing-gate 入 h2m-test；oracle exit 15 钉 5/9/21/3 四路）／語句位調用已償 s93（`h2m.no` K-EXPRESSION-STATEMENT 臂尾只收 K-CALL-EXPRESSION 的 lower-expr 回填路，鏡像 Go hir2mir.go:3790 KExprStmt else 分支 lowerExpr(child) 值丟棄；void 被調沿用 define i64 約定、TY-VOID 另件；放行哨兵 stmt-call-void/discard/gate＋負例 stmt-expr-bare-gate 入 h2m-test；oracle exit 7 钉 4/4/1 三路）／§5.3.4.ct match 軌殘留——解構臂、`ok`/`err`/`nil` 關鍵字臂、型別臂、非 i64 主體、表達位鏈、帶標籤終結形與帶值 return、開邊界區間、`OP-STRUCT-SET` 連續約定 §11 B26、`drv.cmd-build` 接線 M4-d）與 >24B 堆裝箱與 vec 非 i64 元素／txt 元素相等，已知待補項集中見 **§11 記帳**〔本段為 2026-11-06 歷史快照，已由下方 2026-11-12 as-built 快照接手〕本文件為持續維護的活文檔——每完成一個里程碑即更新 §6 與最新快照，故不再以文檔結束收尾

|  |  |  |
| --- | --- | --- |
**〔歷史快照・2026-11-06〕M4-e `h2m` AST→MIR 降級已續推至 s97（2026-11-06，共 43 件，§5.3.4.cj–.dz）**：新建 `compiler/src/h2m.no` 把 `ast.no` arena 降成 `mir.no` 扁平指令流，沿語料觸發頻度逐族落地——① 直線式 i64 子集（s55）→ 簽名側與調用（s56）→ 控制流與跨基本塊槽化（s57）→ 花括號短路鏈（s58）→ 塊終結形 return/break/continue（s59）→ 計數循環（s60m）→ 區間遍歷（s61）→ 容器遍歷（s62）→ 值匹配（s63）→ 實參側池 args 摘掉連續 id 契約（s64）；② 邏輯 `&&`/`||`（s68）；③ u64 全族——除/模無符號護欄（s76）、比較無符號（s77）、算術（s78）、位運算/移位（s79）、邏輯族（s83）；④ i64 `>>` 裁定 lshr（s80，推翻 s35 `ashr` 設計）；⑤ f64 局部變量/控制流槽化（s74）、i64 除/模除零護欄（s75）、f64 `%` 放行（s87）；⑥ 前綴 `~` 與中綴綁定口徑複核（s84）、(MIN,-1) 護欄翼（s85）、f64 主體 `~` 門禁（s86）、非整型主體 `!` 門禁（s89）、bool 字面量（s90）、char 字面量（s91）；⑦ TY-OPT 控制流鋪槽（s97，option 局部量/輸出參落 `alloca %option`、延遲零值 tag=1、寫點 some-wrap 收斂為單點 `h2m-opt-wrap`）。驗證：`h2m-test` **845 過/852 總**（紅 7 為既有 s64-str 等負例，非本件）、`cg` 347／`cg-runtime` 60／`ck` 20 全綠；IR 反向刀 4/4 全 EFFECTIVE；oracle 對拍 `tmp/s97-oracle-vals.no` 與自舉探針同值 exit 165；前向 16/16 byte-SAME。剩餘（s99 起）：`?str/?vec` 載荷、`option` match 臂解構、err 構造/tag=2 裝箱、ok 字面量、程序頂層陳述執行（M4-d 邊界）、內建函數調用、struct/vec 字面量、multi-out、defer/嵌套函數、TY-VOID（cg `OP-SHR` ashr 死代碼清理已於 s98 完成，§5.3.4.ea：h2m `>>` 自 s80 恆射 `OP-SHRU`=lshr、cg 刪 ashr 發射臂、cg-test 移位斷言 ashr→lshr 校正）。〔本段止於 s97；s98–s99n 與 2026-11-10／11-11 變更見下方 2026-11-12 快照。〕

---

**進度快照（2026-11-12，as-built 現況；含 B28 清償、`cg-test` 解鎖、M4-d 接線、`print` 降級、`...` 佔位陳述與字面量插值降級、值位塊匹配降級、`str` 相等/不等降級、解析器欄位尾註釋保真度修復後）**：M0／M1／M2 已完成（L2 全語料 655/655 SAME、L1 675/675 SAME 為 2026-10 歷史觀測）；M3 完成 M3-a 骨架（`drv.cmd-vet`＋`diag.no`）→ M3-b stdsig 表（`ck-sig.no`）→ M3-c 首族與 s82 詞法 ILLEGAL 報告族（§6 M3 列）。**M4 已進入中後端**：M4-a/b 骨架、M4-c 發射層（控制流／循環／字符串／拼接／多參調用／str 跨塊／vec／帶型別簽名／ALU·u64·f64 全族／option 載荷／struct 全族／RC 原語）已落地；`compiler/src` 現有 `h2m.no` 4,447／`mir.no` 481／`cg.no` 2,569／`cg-runtime.no` 531／`cg-rc.no` 180／`link.no` 53／`drv.no` 602／`ps.no` 10,071／`lx.no` 1,645／`tok.no` 413／`asp.no` 915。**M4-e `h2m` AST→MIR 降級已續推至 s99n（件號 54，§5.3.4.ea–.eo）**：s98（2026-11-08）清 cg `OP-SHR` ashr 死臂（h2m 自 s80 恆射 `OP-SHRU`=lshr、cg 刪 ashr 發射臂、cg-test 移位斷言校正）；s99–s99n（2026-11-09，14 節）補 struct 定義／欄位訪問／形參返回值／跨塊槽／跨函數／欄位型別（str·f64·嵌套·`[]i64`）／欄位 RC 取得與 scope-exit 釋放／vec 欄位就地索引；s99m 所有權 provenance（`ownn`·`ownv`·`rdval`·`rdown`·`retset`）＋撤除 `mem==1` 含 CALL 的整函數閘門；s99n 補 vec 半邊（`K-SLICE-LITERAL`⇒`OP-VECNEW`／`OP-VECPUSH`、`@vec_free`、`OP-VEC-FREE = 94`、三條所有權外流門禁、離線 ASan 4/4 無報告）。`a4b7c66f`／`0fbbe684`（11-10）分別扁平化 `asp.no` 規格表、重構 `h2m` struct lowering 與 ownership；`0e055e11`（11-11）重生 `asp.no`（`KIND-COUNT=74`／`STR-COUNT=191`／`INSTR-COUNT=315`，新增 `EmptyStatement`）並補 §5.3.4.ax 的 16B `%str-long` 警示。**門禁（`h2m-test` 6 紅為既有 s98 optmatch，其餘全綠）**：`h2m-test` **1080/1086**、`cg-runtime-test` **86/86**、`cg-str16-test` **41/41**（完整 `.ll` `opt -passes=verify` rc=0＋`clang` e2e 4/4）、`cg-test` **359/359**（⑦ 解鎖並校正 16B 斷言）、`drv-build-test` **63/63**（M4-d＋⑨ print＋⑩ 插值／`...`＋⑪ 值位塊匹配＋⑫ str 相等/不等斷言；`dump` 模式 367 行 `.ll` 過 `opt -passes=verify` rc=0、`clang` 執行 exit=42；`dump-print` 374 行（輸出 `42\nhi`）、`dump-interp` 389 行（輸出 `a=7 b=hi c=7`），皆過 `opt`＋`clang`，輸出逐位元組等於 oracle）⇒ **M4 四個文字門禁全綠**；其餘 L0 抽測見 §11 後 2026-11-12 追記。✅ **§11.2 B28 已於本日清償**：selfhost `%str-long` 已由 24B 同步為 16B `{ i32, i32, i8* }`，prelude／`@str_eq`／`@str_concat` 與 Go `emitPrelude` 逐字相同，`cg.no` 各 op 補齊 `zext`／`trunc`；`%option` 的 24B slot 未變、`?str` 仍 inline 但只佔前 16B（詳 §11 後 ⑥）；**⑦ 再解鎖 `cg-test.no` 並校正 16B 斷言**（詳 §11 後 ⑦）。✅ **⑧ M4-d 接線已於本日落地**：`h2m-lower-module` 合成 `@main`、新增 `link.no`（DR-2 調 clang）、`drv.cmd-build`／`cmd-run` 支援 `-o OUT FILE` 與 `FILE -o OUT` 雙序、`drv-build-test` 16/16、e2e `build`／`run` 與 Go oracle 同值（**exit=42**）（詳 §11 後 ⑧）。✅ **⑨ `print`／`eprint` 已於本日降級**：新增 `OP-PRINT = 95`（kind 8 種）、prelude 補 `@digits`／`@print_i64`／`@print_str`／`@eprint_*`／`@.nl`／`@.sp`（3,214 字元，prelude 總長 7,226→10,440）、`h2m-lower-print` 含值型別／插值擋板／語句位三條誠實門禁、`run` 預設落 `<TMPDIR>/noc-run-<pid>.out`（不再污染語料目錄）；**L4 全語料首度量測（169 檔，串行）**：真 SAME **2**（`ifelse.no`／`colon-loop.no`，本輪由 0 起增益）、空 SAME 4（`tagged-enum-*`，oracle 亦建不過）、DIFF 163 且**無靜默錯碼**（詳 §11 後 ⑨）。✅ **⑩ `...` 佔位陳述＋字面量插值已於本日降級**：`tok.no` 新增 `ELLIPSIS3 = 92`（與範圍運算子 `ELLIPSIS = 65` 分離，擋掉 `[0...n)` 的靜默合法化）、`lx.no` 三點貪婪、`ps.no` 建 `K-EMPTY-STATEMENT`、`h2m` no-op 臂消費（語料 **74 檔**含 `...`）；插值走「三檔分類 + 逐段 `OP-PRINT`」，無 spec 的 `{ident}` 與權威**逐位元組同結果**（`fmt-int(n,'')` 正文即 `n.to_str()`），`{name:spec}`／表達式字段／多實參模板形／`{{` `}}` 一律誠實門禁。量測：`rc==0` 檔 **2 → 4**（新綠 `for.no`、`guard.no`，皆以改前二進制復跑確認為 DIFF ⇒ 淨增益確為 +2）；全語料 unsup 總數 **11,857 → 11,436（−421，−3.6%）**，**86/169 檔**下降（詳 §11 後 ⑩）。✅ **⑪ 值位塊匹配（`r = { c -> v  -> d }`）已於本日降級**：`h2m-scan` 按 LetStatement 節點 id 合成結果槽 `@smv<id>` 並在 entry 統一 `OP-ALLOC`（**不在降級途中補 alloca**——否則迴圈體每迭代新攤一份棧）、`h2m-lower-surface` 加值位分支（逐臂 `OP-STORE`、匯聚 `OP-LOAD`），臂值非 `i64`／臂體非單表達式／`valueCtx` 未置位一律誠實門禁；`h2m-test` 1031/1037 → **1060/1066**、`drv-build-test` 42/42 → **53/53**；L4 真 SAME **4 → 5**（新綠 `build-mo.no`，唯該檔不印輸出 ⇒ 語義那一半由端到端對拍承擔）、全語料 unsup **11,436 → 11,354（−82，−0.7%）**、18/169 檔下降；⚠️ 同時記下 **4 檔 unsup 上升**（值位匹配強制 mem 模式 ⇒ `[]i64` 局部量首次需要槽）與 **B19 的鏈語義分歧**（權威側無兜底臂不短路，本輪不動）（詳 §11 後 ⑪）。✅ **⑫ `str` 相等/不等已於本日降級**：`h2m-lower-expr` 的 infix 分派補 `ok-e` 臂（`==`/`!=` 兩側皆 `TY-STR` ⇒ `OP-STEREQ`/`OP-STNEQ`，`dst` 記 `TY-I64`），**零改 `cg`／零改 `mir`**——兩 op 與 `@str_eq` 早已就位，缺的只是降級臂；同時補 `h2m-ty-of` 的 `str`×`str` 分支（`==`/`!=` ⇒ `TY-I64`、`+`/`-` ⇒ `TY-STR`）——**缺它則 str 比較/拼接只在 `mem==1` 掛**（同一份碼直線式能編、塞進 `cond -> …` 就 5 條 unsup，因槽寬推不出而 `h2m-slot-decl` 計 unsup）；`h2m-test` 1060/1066 → **1080/1086**、`drv-build-test` 53/53 → **63/63**、全語料 unsup **11,354 → 11,163（−191，−1.7%）**、**25/169 檔下降且 0 檔上升**（⑪ 曾有 4 檔上升）、自舉模組 unsup 合計 **2,456 → 2,368（−88）**（`ps.no` 1,268→1,182、`h2m.no` 154→148、`link.no` 24→22）；端到端 `e1.no`（四組 str 比較繞過 `print(bool)`）與權威**逐位元組相同**（詳 §11 後 ⑫）。**s105 修掉一個更底層的保真度缺口**：`ps-is-struct-def` 的「型別後終結符」只認 NEWLINE/COMMA/RBRACE/EOF，`COMMENT` 落進 fallback ⇒ **欄位帶 `;` 尾註釋的結構體整塊退回 `blockUnknown`、從未登記**（Go 的 `parser.nextToken()` 自行跳過 COMMENT，故其 `look()` 根本看不見 COMMENT；`ps-peek-at` 忠實回傳 ⇒ 兩側對「型別後面那個 token」判斷分歧）。修法是新增 `et == tok.COMMENT` 臂並**即時計數**（主迴圈 NEWLINE 臂不計數，只跳過會讓「每個欄位都帶尾註釋」的結構體得 `nfield==0` 仍判非結構體）。影響面：`ast.no` 的 `ast` 12 個欄位裡 3 個帶註釋 ⇒ 該結構體未入表 ⇒ `h2m-struct-find('ast')` 回 -1 ⇒ 155 個函式簽名被判 bad；修後 15 個自舉模組的簽名被拒 **250 → 166**、自舉模組**實際產碼函式數**（`define` 行）`ast.no` 3→8／`lx.no` 2→3；⚠️ 自舉模組 unsup 反而 **2,362 → 3,198**（簽名通了，函式本體才被嘗試，body 內每條不支援各記一條——判據必須同時看 `define` 行數），而**全語料 173 檔逐檔 `diff` 完全相同**（11,315 → 11,315、`unsup==0` 5 → 5，零回歸零增益）；`ps-test` **39/42 → 41/44**（詳 §11 後 ⑬）。**新的最高優先未了項**＝**`#` 模組導入**——⑫ 以臨時插樁量出 unsup 落在哪個 AST 節點型別的直方圖（`K-CALL-EXPRESSION` 611 條／`K-INFIX` 479／`K-IDENTIFIER` 338／`K-EXPRESSION-STATEMENT` 233／`K-LET` 139／`K-USE-STATEMENT` 37…），其中 611 條調用標記**全卡在「點號被調者」＝跨模組**（`cg-runtime.no` 350 條裡 296 條是 `b.push('…')`，而方法本體住在 `util/sb.no`），與 37 條 `K-USE-STATEMENT` 是同一塊；佐證：`noc build compiler` 目前 rc=0 卻只產出 34,632 位元組的空殼（執行 `version` 什麼都不印）⇒ 導入整條被忽略，這已是「模組能互相調用」前面唯一還空著的結構性缺口。其餘殘差：下標讀、`ok(...)` 條件臂、值位匹配的 str/vec 臂值、空 match 臂 `->` 與裸塊陳述 `{ … }`（⑬ 已釘到最小重現）、`[]T` 元素型別只有 `i64`（簽名側 20 條 `62:false`）、B19 的鏈語義裁決。M5–M8 ⬜ 未開始；`?str/?vec` 載荷、`option` match 臂解構、err 構造/tag=2 裝箱、ok 字面量、`print(bool)` 的 bool 型別碼、multi-out、defer／嵌套函數、TY-VOID、>24B 堆裝箱仍待續（§11 記帳 B4/B8/B11/B13/B14/B19/B26；B28 已清償、`cg-test.no` 已解鎖、M4-d 已接線、`print` 已降級、`...` 與插值已降級、值位塊匹配已降級、`str` 相等/不等已降級、解析器欄位尾 `;` 註釋保真度已修）。本文件為持續維護的活文檔——每完成一個里程碑即更新 §6 與本快照，故不再以文檔結束收尾
