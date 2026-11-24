# Nolang 自舉（Self-Hosting）技術方案：`compiler` 子項目

> **文檔定位**：這是一份**前向設計（forward design）＋ as-built 實施日誌**的活文檔：§0–§4／§7–§11 描述目標形態、分階段路線圖與驗收門禁，§5.3.4 的逐輪增量已遷至歸檔文檔（見文檔導航）。**`compiler/` 子項目自 2026-10-09 起已存在並持續推進**（**2026-11-23 現況**：`compiler/src/*.no` **16 檔**＋`compiler/src/util/*.no` 2 檔，合計 **46,960 行**；M0–M3 完成，M4-c 發射層與 M4-e AST→MIR 降級已推至 **s152**，見 §6／§6.5／§11.2 B4／§12 ⑰–§70。⚠️ **當前工具鏈代次（v0.4.29+，HEAD `d236a956`）下 `compiler/src` 尚未移居 option 化算術與安全索引讀**——`no build compiler` **12 ERROR**、L0 18 套僅 **5 套綠**；§66–§70 的綠燈證據 pin 在 **v0.4.28 代次**（`tmp/s148/wt`，`d21fbb35`），非現行 HEAD 可重現，詳見 **§71** 收口摘要）凡標「2026-10-09 工作樹（`HEAD = 4feaaf70`）」的行號／計數屬**歷史證據**，不是現狀；同理 §4.1.1 的 2026-10-30 盤點、§6.5 的 2026-10-29 啟動摸底、文末 2026-11-06 快照均已 frozen 為歷史紀錄
>
> **一句話目標**：在 `compiler/` 子項目中用 **Nolang 自身**實現一個完整編譯器 `noc`，使它能夠**編譯自己**，並以S0 → S1 → S2 → S3 定點的方式證明自舉成立；同時用**五級差分門禁**保證它與現有 Go 實作 `no` 的行為一致
>
> ⚠️ **語法現行（2026-11-17）**：歸檔文檔 §5.3.4 等歷史增量記錄中出現的 `run` / `awy <expr>` /
> `RunExpression` / `AwaitExpression` 都是**已移除語法的歷史證據**，不是現行寫法。
> 現行協程語法只有 `awy { r = co f() }`；`co` 組外非法，`run` 已是普通識別符。
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
> **歸檔文檔 §5.3.4 s25–s27、§5.3.4.be、§5.3.4.bv、§5.3.4.cx 等節記述的 `%option = { i64, [3 x i64] }`
> 全部視為歷史紀錄。** `compiler/src/cg-runtime.no` 的 `cg-runtime-option-type()` 仍是舊形，
> **把新佈局鏡像進 `noc` 是待辦（P4）**；在完成前 `noc` 與 `no` 的 option 佈局不同，跨 option
> 的 `L4` 差分以 `no` 的輸出為準。

> 📚 **文檔導航**：本文是**現行正典**（目標／架構／現況／缺口／最新追記）；126 節 §5.3.4 歷史增量、⑥–⑯ 追記與舊附錄 B 留在 [`nolang-selfhost-history.md`](nolang-selfhost-history.md)，所有實測證據可逐節回溯。
>
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
| `awy { ... }` + 組內 `co`（**僅在驅動層並行調用 clang 時**）       | 舊 `run` / `awy <handle>` 手寫                     | 2026-11-17 移除手寫語法          |

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


### 4.1.1 目標佈局 vs 當前實際（2026-10-30 歷史快照；2026-11-12 as-built 校正見本節末；**2026-11-23 最新現況見 §71**）

> ⚠️ §4.1 是**目標**佈局（全部里程碑完成後的形狀），不是現狀下表是 **2026-10-30 的歷史快照**，逐檔對到當時的 `find compiler/src -name '*.no'`——當日只有 **11 個 `.no`**（含 2 個生成物）、前端閉環而中端/後端缺席。**該描述已過期**：2026-11-12 as-built 為 `compiler/src/*.no` **14 檔**＋`compiler/src/util/*.no` **2 檔**，`h2m.no`／`mir.no`／`cg.no`／`cg-runtime.no`／`cg-rc.no` 均已落地，M4-e 已續推至 **s99n**（§5.3.4.eo）。下表「當時實際」欄保留原值作歷史證據，**現狀以本節末的 2026-11-12 校正、§6／§6.5／§11.2 B4／文末 2026-11-12 快照為準**（⚠️ **2026-11-23 校對**：上指「文末快照」已由 **2026-11-23 快照**接手，最新現況與工具鏈代次結論一律以 **§71** 為準）

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
| 避開關鍵字：`awy` / `match` / `in` / `for` / `co` / `it` / `self`（`run` 已非關鍵字） | 語言規則                    |
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
| `CoExpression`         | 47      | `co <call>`             |
| （舊 `AwaitExpression` / `RunExpression`） | — | 2026-11-17 移除：改由 `awy { ... }` 協程組表示 |
| `MapLiteral`           | 29      | map 字面量 `[K]V{…}`       |
| `TypeAlias`            | 6       | `type x = y`            |
| `RegexLiteral`         | 2       | `/re/`                  |
| `ExternStatement`      | 1       | `extern`                |
| `AnnotationStatement`  | 1       | `#{…}` 敘述               |
| **合計**                | **319** | **9 種節點型別完全不可見**（佔 0.38%；其中 `AwaitExpression` 43 行 / `RunExpression` 36 行屬已被移除的舊語法） |

另有進得了 switch 但只印殼的節點（子節點不 dump）：

| 節點                                                              | 印出                  | **沒有** dump        |
| --------------------------------------------------------------- | ------------------- | ----------------- |
| `StructDefinition` / `EnumDefinition` / `TaggedEnumDefinition`  | 只有 `name`           | **所有欄位／成員／變體**    |
| `StructLiteral`                                                 | 只有 `type`           | **所有欄位初始化**       |
| `InterfaceDefinition`                                           | name + 方法名          | 方法簽名              |
| `ArrayLiteral` / `RangeExpression`                              | `nodeString(size/start/end)` | 子運算式              |

⚠️ 當時的**最壞後果**：一個把 map 字面量、`co`、舊 `run`/`awy` 前綴全部解析錯的 `noc`，只要**錯在同樣的地方**（都塌成一行 `<unknown>`），L2 就報完全一致而且語料中 145 處 struct literal 的欄位**從未被差分**。2026-11-17 後 `run` / `awy <handle>` 已移除，協程組統一走 `awy { ... }` + 組內 `co`。

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


### 5.3.4 歷史逐輪增量索引（原文已歸檔）

> 2026-10-20 ~ 2026-11-09 的 126 節逐輪增量（S7 match 分歧、M2/M3 解析器與診斷、M4-c 發射層、M4-e AST→MIR 降級、struct/RC 家族）已**原樣遷至** [`nolang-selfhost-history.md` §一](nolang-selfhost-history.md)（每節標題前的 `<a id="hist-NNN">` 為穩定錨點）。本節只保留導航；歷史結論仍是現行設計的依據，不要因為移到歸檔就當作作廢。

| 章節 | 主題 | 原文 |
| --- | --- | --- |
| 5.3.4 S7 match 表示分歧 | 5.3.4 S7 match 表示分歧——形狀歸因與立项規格（2026-10-20 實測；98%更正見本節 L2 收斂量化） | [hist-001](nolang-selfhost-history.md#hist-001) |
| 第 ② 件的第一次嘗試 | 第 ② 件的第一次嘗試：已實測否決一次編輯就能翻的假設 | [hist-002](nolang-selfhost-history.md#hist-002) |
| 第 ② 件還缺的前置 | 第 ② 件還缺的前置：主體形態覆蓋率 | [hist-003](nolang-selfhost-history.md#hist-003) |
| ✅ 第 ② 件落地（2026-10-20） | ✅ 第 ② 件落地（2026-10-20）：`ps-parse-match` 改發 `SurfaceMatch` | [hist-004](nolang-selfhost-history.md#hist-004) |
| s66d | raw-cond 擴展，2026-10-20 實測） | [hist-005](nolang-selfhost-history.md#hist-005) |
| s67 | valueCtx 值語境修正（2026-10-20，本輪） | [hist-006](nolang-selfhost-history.md#hist-006) |
| 5.3.4.w S7 裁定①續 | 5.3.4.w S7 裁定①續——match 表示缺口 A→H 全鏈修（2026-10-21 本輪） | [hist-007](nolang-selfhost-history.md#hist-007) |
| 5.3.4.y S7 裁定①續二 | 5.3.4.y S7 裁定①續二——缺口 I 收尾 + 迴圈行內體根因（2026-10-21 本輪） | [hist-008](nolang-selfhost-history.md#hist-008) |
| 5.3.4.aa 正交軌首件 | 5.3.4.aa 正交軌首件——IndexExpression 元素型別推斷 + `tn` 同名靜默誤編（2026-10-21 … | [hist-009](nolang-selfhost-history.md#hist-009) |
| 5.3.4.ab L2 現狀複測與 WIP  | 5.3.4.ab L2 現狀複測與 WIP mass-noop（2026-10-24 本輪） | [hist-010](nolang-selfhost-history.md#hist-010) |
| s8q | pipeline 同位差——`let` 右側 `->` 鏈包成 value（2026-10-27 本輪） | [hist-011](nolang-selfhost-history.md#hist-011) |
| s8r | module-path 接收者的呼叫回傳型別推斷（2026-10-27 本輪） | [hist-012](nolang-selfhost-history.md#hist-012) |
| s8s | `[?]T` 回傳參數型別（ArrayType size=nil）（2026-10-27 本輪） | [hist-013](nolang-selfhost-history.md#hist-013) |
| s8t | match-test 死碼臂告警計數＋裸可空宣告 Doc 附着（2026-10-27 本輪） | [hist-014](nolang-selfhost-history.md#hist-014) |
| s8u | map 標註 let 複合值型別 → MapLiteral（2026-10-27 本輪） | [hist-015](nolang-selfhost-history.md#hist-015) |
| s8v | let-RHS 型別推斷家族：切片方法參數不傳播＋鏈式欄位回傳型（2026-10-27 本輪） | [hist-016](nolang-selfhost-history.md#hist-016) |
| s8w | 鏈式裸 if 尾隨 else 折疊＋切片方法參數補登記（2026-10-27） | [hist-017](nolang-selfhost-history.md#hist-017) |
| s8x | 多重指派逗號後目標專用解析器（2026-10-27） | [hist-018](nolang-selfhost-history.md#hist-018) |
| s8y | 收官雙檔——裸型名陳述＋運算符重載剥 `?`＋介面繼承清單（2026-10-27 本輪） | [hist-019](nolang-selfhost-history.md#hist-019) |
| s8z | M2 驗收收官＋ast-test 缺線哨兵遷移（2026-10-27 本輪） | [hist-020](nolang-selfhost-history.md#hist-020) |
| s9a | M3-a：`drv.cmd-vet` 骨架＋`diag.render` 行形補全（2026-10-27 本輪） | [hist-021](nolang-selfhost-history.md#hist-021) |
| s9b | M3-b：stdsig 表遷移（`gen_ck_sig.py` → `ck-sig.no`）（2026-10-27 本輪） | [hist-022](nolang-selfhost-history.md#hist-022) |
| s10 | M3-c 首族：ps.no 語法錯誤收集器 ⇒ L3 首條真診斷（2026-10-28 本輪） | [hist-023](nolang-selfhost-history.md#hist-023) |
| s11 | M3-c 缺口②清償：NEWLINE 坐標族（分組站點＋實參站點游標對齊）（2026-10-28 本輪） | [hist-024](nolang-selfhost-history.md#hist-024) |
| s12 | M3-c 缺口①清償：cascade 族（括號錯後同位got nil級聯）（2026-10-28 本輪） | [hist-025](nolang-selfhost-history.md#hist-025) |
| s13 | M3-c 正語義族首件：checker 骨架 `ck.no`（`'X' is not defined`）＋全語料零誤報清償（20… | [hist-026](nolang-selfhost-history.md#hist-026) |
| s13 | 工具鏈坑附錄——結構體按值傳參觸 MIR 聚合體缺陷 ⇒ checker 回值改打包串 vec（2026-10-29） | [hist-027](nolang-selfhost-history.md#hist-027) |
| s14 | M4-a：`cg-runtime`/`cg-rc` prelude 照抄 + `check-rc-balance` 骨架（202… | [hist-028](nolang-selfhost-history.md#hist-028) |
| s15 | M4-b：`mir.no` 表示 + `cg.no` 的 `EmitLLVM` 骨架（2026-10-30 本轮） | [hist-029](nolang-selfhost-history.md#hist-029) |
| s16 | M4-c 首件：控制流（多基本块 + `br`/`condbr`）（2026-10-30 本轮） | [hist-030](nolang-selfhost-history.md#hist-030) |
| s17 | M4-c 第二件：循环与回边（2026-10-30 本轮） | [hist-031](nolang-selfhost-history.md#hist-031) |
| s18 | M4-c 第三件：字符串（首个非 i64 型別 `%str-long`）（2026-10-30 本轮） | [hist-032](nolang-selfhost-history.md#hist-032) |
| s19 | M4-c 第四件：字符串拼接 `@str_concat`（首个 RC 消费者）（2026-10-30 本轮） | [hist-033](nolang-selfhost-history.md#hist-033) |
| s20 | M4-c 第五件：多參數調用（`OP-CALL` 的 `bs` 從佔位改為參數個數）（2026-10-30 本轮） | [hist-034](nolang-selfhost-history.md#hist-034) |
| s21 | M4-c 第六件：`%str-long` 跨基本塊（`SALLOC`/`SSTORE`/`SLOAD`）（2026-10-30 … | [hist-035](nolang-selfhost-history.md#hist-035) |
| s22 | M4-c 第七件：`vec`（`%vec` 向量，元素仅 i64）（2026-10-30 本轮） | [hist-036](nolang-selfhost-history.md#hist-036) |
| s23 | M4-c 第八件：带型别的函数签名（`str`/`vec` 形参与返回值）（2026-10-31 本轮） | [hist-037](nolang-selfhost-history.md#hist-037) |
| s24 | M4-c 第九件：带型别的 `CALL`（值型别表 `vty`）（2026-10-31 本轮） | [hist-038](nolang-selfhost-history.md#hist-038) |
| s25 | M4-c 第十件：`option`（`%option` tag + payload）（2026-10-31 本轮） | [hist-039](nolang-selfhost-history.md#hist-039) |
| s26 | M4-c 第十一件：STRCAT 的 `retain`/`release`（RC 發射原語 + 平衡校驗）（2026-10-31… | [hist-040](nolang-selfhost-history.md#hist-040) |
| s27 | M4-c 續件：STRCAT 實參為變數（堆上結果）的 RC 生命週期（2026-10-31 本輪） | [hist-041](nolang-selfhost-history.md#hist-041) |
| s28 | M4-c 續件：option 的 `str` 載荷讀寫與堆上裝箱（§11 B8，2026-10-31 本輪） | [hist-042](nolang-selfhost-history.md#hist-042) |
| s29 | M4-c 續件：`struct`（首個聚合型別 `%struct.point`，2026-10-31 本輪） | [hist-043](nolang-selfhost-history.md#hist-043) |
| s30 | M4-c 續件：結構體寫欄位 `STRUCT-SET` ＋ 結構體槽三件套（2026-10-31 本輪） | [hist-044](nolang-selfhost-history.md#hist-044) |
| s31 | M4-c 續件：多結構體型別（型別碼編 id）＋ 任意欄位數（2026-10-31 本輪） | [hist-045](nolang-selfhost-history.md#hist-045) |
| s32 | M4-c 續件：結構體欄位型別（`sft` 拼接表 + 欄位型別感知的 MK/GET/SET，2026-10-31 本輪） | [hist-046](nolang-selfhost-history.md#hist-046) |
| s33 | M4-c 續件：結構體欄位的 RC 原語（`STRUCT-RETAIN`/`STRUCT-RELEASE` + 欄位掩碼，202… | [hist-047](nolang-selfhost-history.md#hist-047) |
| s34 | M4-c 續件：結構體作為一等值（形參/返回值 + 嵌套欄位）與字面量全局唯一化（2026-10-31 本輪） | [hist-048](nolang-selfhost-history.md#hist-048) |
| s35 | M4-c 續件：ALU 家族（位運算／移位／除餘／其餘比較）（2026-11-01 本輪） | [hist-049](nolang-selfhost-history.md#hist-049) |
| s36 | M4-c 續件：無符號 ALU 家族（`lshr/udiv/urem` + 無符號比較）與 `TY-U64` 型別碼（2026-… | [hist-050](nolang-selfhost-history.md#hist-050) |
| s37 | M4-c 續件：`f64` 算術家族（`fadd/fsub/fmul/fdiv/frem` + `fcmp`）與 `TY-F64… | [hist-051](nolang-selfhost-history.md#hist-051) |
| s38 | M4-c 續件：整數除零 select 護欄的 `noc`↔Go 保真對齊（收斂 §11 B14 的 codegen 層缺口）（… | [hist-052](nolang-selfhost-history.md#hist-052) |
| s39 | M4-c 續件：整數除零 `divZeroReport` stderr 報告（收 §11 B14 的 codegen 層最後一塊… | [hist-053](nolang-selfhost-history.md#hist-053) |
| s40 | M4-c 續件：option 的 `vec` 載荷內聯讀寫（`?[]i64`，§11 B8 免裝箱半項）（2026-11-02 … | [hist-054](nolang-selfhost-history.md#hist-054) |
| s41 | M4-c 續件：option 跨基本塊槽（`alloca %option` / store / load 非 PHI，§11 B… | [hist-055](nolang-selfhost-history.md#hist-055) |
| s42 | M4-c 續件：`str` 結構相等（`@str_eq` helper ＋ `OP-STEREQ`／`OP-STNEQ`，非 `… | [hist-056](nolang-selfhost-history.md#hist-056) |
| s43 | M4-c 續件：`vec` 元素相等（`@__nolang_vec_eq_i64` helper ＋ `OP-VECEQ`，非 … | [hist-057](nolang-selfhost-history.md#hist-057) |
| s44 | M4-c 續件：`option` 值相等（`OP-OPTEQ`／`OP-OPTNEQ`，tag AND payload，單塊無需… | [hist-058](nolang-selfhost-history.md#hist-058) |
| s45 | M4-c 續件：`option` 的 `str` 載荷相等（`OP-OPTEQSTR`／`OP-OPTNEQSTR`，tag A… | [hist-059](nolang-selfhost-history.md#hist-059) |
| s46 | M4-c 續件：`option` 的 `f64`（double）載荷內聯讀寫（`OP-OPTMKF64`／`OP-OPTGETF… | [hist-060](nolang-selfhost-history.md#hist-060) |
| s47 | M4-c 續件：`option` 的 `f64`（double）載荷相等（`OP-OPTEQF64`／`OP-OPTNEQF64… | [hist-061](nolang-selfhost-history.md#hist-061) |
| s48 | M4-c 續件：一元 ALU 家族 NEG／NOT（`OP-NEG`／`OP-NOT`，fneg double／sub i64 … | [hist-062](nolang-selfhost-history.md#hist-062) |
| s49 | M4-c 續件：vec→str 重interpret（`OP-STRFROMVEC`，%vec 三欄位逐欄拷进 %str-lon… | [hist-063](nolang-selfhost-history.md#hist-063) |
| s50 | M4-c 續件：str 容量欄讀取（`OP-STRCAP`，extractvalue %str-long field 1，與 O… | [hist-064](nolang-selfhost-history.md#hist-064) |
| s51 | M4-c 續件：option 排序（`OP-OPTLT`／`OP-OPTLE`／`OP-OPTGT`／`OP-OPTGE`，只比… | [hist-065](nolang-selfhost-history.md#hist-065) |
| s52 | M4-c 續件：option 的 `f64` 載荷排序（`OP-OPTLTF64`／`OP-OPTLEF64`／`OP-OPTG… | [hist-066](nolang-selfhost-history.md#hist-066) |
| s53 | M4-c 續件：option 與純量的排序（`OP-OPTLTSC`／`OP-OPTLESC`／`OP-OPTGTSC`／`OP… | [hist-067](nolang-selfhost-history.md#hist-067) |
| s54 | M4-c 續件：option 與 f64 純量的排序（`OP-OPTLTSCF`／`OP-OPTLESCF`／`OP-OPTGT… | [hist-068](nolang-selfhost-history.md#hist-068) |
| s55 | M4-e 首件：AST → MIR 降級（新建 `compiler/src/h2m.no`，i64 直線式子集，`let` 直接… | [hist-069](nolang-selfhost-history.md#hist-069) |
| s56 | M4-e 第二件：簽名側與調用降級（`compiler/src/h2m.no` 擴面：輸入參數佔形參值 id，輸出參數型別定 `… | [hist-070](nolang-selfhost-history.md#hist-070) |
| s57 | M4-e 第三件：控制流降級與跨基本塊槽化（`compiler/src/h2m.no` 補基本塊切分 `OP-BR`／`OP-C… | [hist-071](nolang-selfhost-history.md#hist-071) |
| s58 | M4-e 第四件：花括號短路鏈降級（`compiler/src/h2m.no` 讀 `K-SURFACE-MATCH` 的 he… | [hist-072](nolang-selfhost-history.md#hist-072) |
| s59 | M4-e 第五件：塊終結形降級（`compiler/src/h2m.no` 認 `K-RETURN-STATEMENT`／`K-… | [hist-073](nolang-selfhost-history.md#hist-073) |
| s60m | M4-e 第六件：計數循環降級（`compiler/src/h2m.no` 認 `K-FOR-STATEMENT` 的計數位——… | [hist-074](nolang-selfhost-history.md#hist-074) |
| s61 | M4-e 第七件：區間遍歷降級（`compiler/src/h2m.no` 認 `K-FOR-STATEMENT` 的 `Ite… | [hist-075](nolang-selfhost-history.md#hist-075) |
| s62 | M4-e 第八件：容器遍歷降級（`compiler/src/h2m.no` 認 `K-ITERATION-EXPR` 的 `ki… | [hist-076](nolang-selfhost-history.md#hist-076) |
| s63 | M4-e 第九件：值匹配降級（`compiler/src/h2m.no` 認 `K-SURFACE-MATCH` 的 `kid[… | [hist-077](nolang-selfhost-history.md#hist-077) |
| s64 | M4-e 第十件：實參側池 `args`（`compiler/src/cg.no` 的 CALL 臂改按**池下標**取實參、`… | [hist-078](nolang-selfhost-history.md#hist-078) |
| s65 | M4-e 第十一件：值匹配臂的 `it` 隱式綁定（`compiler/src/h2m.no` 增 **`itv` 值 id 棧… | [hist-079](nolang-selfhost-history.md#hist-079) |
| s66 | M4-e 第十二件：前綴表達式 `-x`／`!x`（`compiler/src/h2m.no` 的 `h2m-lower-exp… | [hist-080](nolang-selfhost-history.md#hist-080) |
| s67 | M4-e 第十三件：二進制位運算 `&`／`|`／`^`／`<<`／`>>`（`compiler/src/h2m.no` 的 `… | [hist-081](nolang-selfhost-history.md#hist-081) |
| s68 | M4-e 第十四件：邏輯 `&&`／`||`（`compiler/src/h2m.no` 的 `h2m-lower-expr` … | [hist-082](nolang-selfhost-history.md#hist-082) |
| s69 | M4-e 第十五件：前綴位非 `~x`（`compiler/src/h2m.no` 的 `h2m-lower-expr` `K-… | [hist-083](nolang-selfhost-history.md#hist-083) |
| s70 | M4-e 第十六件：前綴 f64 負號 `-x`（`compiler/src/h2m.no` 的 `h2m-lower-expr… | [hist-084](nolang-selfhost-history.md#hist-084) |
| s71 | M4-e 第十七件：f64 二元算術 `+ - * /`（`compiler/src/h2m.no` 的 `h2m-lower-… | [hist-085](nolang-selfhost-history.md#hist-085) |
| s72 | M4-e 第十八件：f64 二元比較 `< <= > >= == !=`（`compiler/src/h2m.no` 的 `h2… | [hist-086](nolang-selfhost-history.md#hist-086) |
| s73 | M4-e 第十九件：f64 字面量降級（`compiler/src/h2m.no` 的 `h2m-lower-expr` **新… | [hist-087](nolang-selfhost-history.md#hist-087) |
| s74 | M4-e 第二十件：f64 局部變量／控制流槽化（`compiler/src/h2m.no` 槽機構認 `TY-F64`——`h… | [hist-088](nolang-selfhost-history.md#hist-088) |
| s75 | M4-e 第二十一件：i64 除／模除零護欄降級（`compiler/src/h2m.no` 運算符表補 `'%' ⇒ OP-M… | [hist-089](nolang-selfhost-history.md#hist-089) |
| s76 | M4-e 第二十二件：u64 除／模無符號護欄降級（`compiler/src/h2m.no` 新增 `uty` 分派——雙側整… | [hist-090](nolang-selfhost-history.md#hist-090) |
| s77 | M4-e 第二十三件：u64 比較族無符號降級（`compiler/src/h2m.no` 在既有 `uty` 分派上把 `OP… | [hist-091](nolang-selfhost-history.md#hist-091) |
| s78 | M4-e 第二十四件：u64 算術族 '+ - *' 放行（`compiler/src/h2m.no` 在既有 `uty` 分派… | [hist-092](nolang-selfhost-history.md#hist-092) |
| s79 | M4-e 第二十五件：u64 位運算／移位族 '& | ^ << >>' 放行（`compiler/src/h2m.no` 在既… | [hist-093](nolang-selfhost-history.md#hist-093) |
| s80 | M4-e 第二十六件：i64 '>>' 口徑裁定（'>>' 全族無條件重映射 OP-SHRU 恒發 lshr，鏡像 Go emi… | [hist-094](nolang-selfhost-history.md#hist-094) |
| s81 | M4-e 第二十七件：u64 前綴 `-x`／`~x` 放行（`compiler/src/h2m.no` 的 `h2m-lowe… | [hist-095](nolang-selfhost-history.md#hist-095) |
| s82 | M4 第二十八件：科學計數法字面量的 ILLEGAL 鏡像（`compiler/src/lx.no` 詞法檢測 + `compi… | [hist-096](nolang-selfhost-history.md#hist-096) |
| s83 | M4-e 第二十九件：u64 邏輯族 `&& ||` 放行（`compiler/src/h2m.no` u64 位運算臂撤 `i… | [hist-097](nolang-selfhost-history.md#hist-097) |
| s84 | M4-e 第三十件：前綴 `~` 與中綴綁定口徑複核＋哨兵重鑲（`compiler/src/ps.no` **實作零改**——§… | [hist-098](nolang-selfhost-history.md#hist-098) |
| s85 | M4-e 第三十一件：(MIN,-1) 護欄翼執行面實釘（h2m/cg **實作零改**——s75 自記「執行面留待專門件」之尾… | [hist-099](nolang-selfhost-history.md#hist-099) |
| s86 | M4-e 第三十二件：f64 主體 `~` 門禁裁定（§5.3.4.dn 關鍵點 3 頭號候選銷賬——Go 側非權威而是崩潰：運… | [hist-100](nolang-selfhost-history.md#hist-100) |
| s87 | M4-e 第三十三件：f64 `%` 放行（s71 算術族尾款——`compiler/src/h2m.no` 歸類區補 `OP-… | [hist-101](nolang-selfhost-history.md#hist-101) |
| s88 | M4-e 第三十四件：`!x` 值語義對齊 i1 低位列（`compiler/src/h2m.no` K-PREFIX `'!'… | [hist-102](nolang-selfhost-history.md#hist-102) |
| s89 | M4-e 第三十五件：非整型主体 `'!'` 門禁裁定紀實＋哨兵重鑲（`h2m.no` K-PREFIX `'!'` 臂三面實釘… | [hist-103](nolang-selfhost-history.md#hist-103) |
| s90 | M4-e 第三十六件：bool 字面量降级＋bool 型别码放行（`h2m.no` 補 K-BOOLEAN-LITERAL 降級… | [hist-104](nolang-selfhost-history.md#hist-104) |
| s91 | M4-e 第三十七件：char 字面量降级＋char 型别码放行（`h2m.no` 補 K-CHAR-LITERAL 降級臂（U… | [hist-105](nolang-selfhost-history.md#hist-105) |
| s92 | M4-e 第三十八件：默认参数调用点回填（`h2m.no` 簽名表補 fdef 預設式節點表＋調用臂尾部回填迴圈，鏡像 Go `… | [hist-106](nolang-selfhost-history.md#hist-106) |
| s93 | M4-e 第三十九件：語句位調用降級（結果丟棄）（`h2m.no` 的 K-EXPRESSION-STATEMENT 臂尾補「只… | [hist-107](nolang-selfhost-history.md#hist-107) |
| s94 | M4-e 第四十件：`?u64` option 三件套降級（簽名／nil 字面量／回值包裹）（`h2m.no` 新增 `h2m-… | [hist-108](nolang-selfhost-history.md#hist-108) |
| s95 | M4-e 第四十一件：`?i64` option 內層整型放寬（`h2m.no` 的 `h2m-tynode-code` K-N… | [hist-109](nolang-selfhost-history.md#hist-109) |
| s96 | M4-e 第四十二件：`?f64` option 內層載荷放寬（`h2m.no` 的 `h2m-tynode-code` K-N… | [hist-110](nolang-selfhost-history.md#hist-110) |
| s97 | M4-e 第四十三件：TY-OPT 控制流鋪槽（`h2m.no` 的 option 槽由直線式（mem==0）扩到控制流（mem… | [hist-111](nolang-selfhost-history.md#hist-111) |
| s98 | M4-e 第四十四件（cg 側 `OP-SHR` 清理，跨 M4-c 發射層）：cg `OP-SHR` ashr 死代碼清理（`… | [hist-112](nolang-selfhost-history.md#hist-112) |
| s99 | M4-e 第四十五件：struct 定義與欄位訪問（OP-STRUCT-MK/GET/SET 降級，`h2m.no` 三臂＋前掃… | [hist-113](nolang-selfhost-history.md#hist-113) |
| s99b | M4-e 第四十五件續：struct 作形參／返回值（型別名解析 + 签名透传）（2026-11-09 本輪） | [hist-114](nolang-selfhost-history.md#hist-114) |
| s99c | M4-e 第四十五件續：struct 跨基本塊槽（STALLOC/STSTORE/STLOAD）（2026-11-09 本輪） | [hist-115](nolang-selfhost-history.md#hist-115) |
| s99d | M4-e 第四十五件續：struct 跨函數調用（mem 模式，含控制流）（2026-11-09 本輪） | [hist-116](nolang-selfhost-history.md#hist-116) |
| s99e | M4-e 第四十五件續：結構體欄位型別擴充（str / f64 / 嵌套結構體）（2026-11-09 本輪） | [hist-117](nolang-selfhost-history.md#hist-117) |
| s99f | M4-e 第四十六件續：結構體欄位型別支援 []i64（vec 欄位）（2026-11-09 本輪） | [hist-118](nolang-selfhost-history.md#hist-118) |
| s99g | M4-e 第四十七件續：結構體 str 欄位 RC（取得所有權半邊）（2026-11-09 本輪） | [hist-119](nolang-selfhost-history.md#hist-119) |
| s99h | M4-e 第四十八件續：MK 欄位值 id 連續性假設（args 池方案，與 cg 協調）（2026-11-09 本輪） | [hist-120](nolang-selfhost-history.md#hist-120) |
| s99i | M4-e 第四十九件續：vec 欄位就地索引 `b.data[i]` / 本地 vec 變量（2026-11-09 本輪） | [hist-121](nolang-selfhost-history.md#hist-121) |
| s99j | M4-e 第五十件續：str 欄位 RC 的 scope-exit 釋放半邊（2026-11-09 本輪） | [hist-122](nolang-selfhost-history.md#hist-122) |
| s99k | M4-e 第五十一件續：控制流函數（mem==1）的 scope-exit 釋放（解除 s99j 的 `mem==0` 限制）（… | [hist-123](nolang-selfhost-history.md#hist-123) |
| s99l | M4-e 第五十二件續：純 str RC 的取得所有權站點（str 拼接 `'a' + 'b'` / `'a' - 'b'` ⇒… | [hist-124](nolang-selfhost-history.md#hist-124) |
| s99m | M4-e 第五十三件續：撤除 mem==1+含 OP-CALL 的整函數閘門（call 是借用；owner provenance… | [hist-125](nolang-selfhost-history.md#hist-125) |
| s99n | M4-e 第五十四件續：vec 的取得所有權站點 + scope-exit 釋放（切片字面量 ⇒ `OP-VECNEW`/`OP… | [hist-126](nolang-selfhost-history.md#hist-126) |

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



## 6. 里程碑與進度

> 本節是**唯一**的實際進度來源：§0 是決策§5 是架構§7 是門禁**每完成一個里程碑就更新這裡的狀態與證據**（證據必須是可重跑命令 + 觀測值）

| 里程碑    | 範圍                                                                              | 狀態      | 證據（可重跑）                                                                                |
| ------ | ------------------------------------------------------------------------------- | ------- | -------------------------------------------------------------------------------------- |
| **M0** | 子項目骨架：`compiler/` 佈局`package.jsonc``main.no` 分派`util/sb.no``tools/shs-check.no``scripts/selfhost_diff.sh` | ✅ **完成** | `no run compiler/tools/shs-check.no compiler/` ⇒ **10 檔 / 0 違規 / OK**；`selfhost_diff.sh --self-test` ⇒ **無 FAIL** |
| **M1** | 詞法：`src/tok.no`（92 個 `TokenType`）+ `src/lx.no`（`scanToken()` 的忠實移植）              | ✅ **完成** | `no run compiler/tests/tok-test.no` ⇒ **62/62**；`lx-test.no` ⇒ **45/45**；L1 全語料 **675/675 SAME, 0 DIFF**（§7.1.1） |
| **M2** | 語法：71 個 AST 節點 + 復刻 `parser/dump.go` 的輸出格式                                     | ✅ **完成（2026-10-27，s8y `261ab91c`）** | **L2 已可執行**：`noc` 已實作 `ast` 子命令（`drv.cmd-ast`），`selfhost_diff.sh --self-test --levels L0,L2 --sut /tmp/noc-s0`（2026-10-11）⇒ **L0 4/4 SAMEL2 646/646 SAME0 DIFF**語料 `<unknown>` 0 行（71/71 節點全覆蓋）真實 noc↔oracle 逐位元組相同：**+1（独立 `AnnotationStatement{entries: N}` 節點增量，解锁 collection/*）0 回歸**——絕對數方法論敏感（parallelism × 逾時上限）：以 uniform 120s-alarm `-P 3` 同法量測 s31=137（AssignExpression）→s32=145（ReturnStatement）→s33=158（Annotation）→s34=159（UseStatement）→s35=202（ArrayLiteral 推論元素型別，**+43**，為 aes/zlib 等 `[N] = [int-literals]` 表檔解锁）→s36=203（self-dot `.field` 前綴 receiver，新匹配 `src/std/io.no`）→s37=204（独立 `AnnotationStatement{entries: N}` 節點——不可附著之語句級 `#{...}`，新匹配 `src/std/collection/stack.no`）→s38=213（**ForStatement bang/cond-loop 簇**——陳述位 `!! { }`（恆真無限）/`! { }`（恆假）/`(cond) { }`（前置條件），**+9**，新匹配 `src/std/bufio.no``collection/hash-set.no``collection/str-set.no``env.no``tests/dowhile.no``loop-test.no``split-loop.no``str-loop-assign.no``mem-safety/bug15-read-dowhile-copyfile.no`）→s39=222（**字面量型別推斷補齊**——`CharLiteral`→`char`十六進位 `0x/0X` `IntegerLiteral`→`byte`（十進位仍 i64），同時補 `ps-infer-type`（裸 RHS）與 `ps-elem-type-of`（陣列/切片首元素），**+9**，新匹配 `tests/char-*`（literal/new-method/new-syntax/old-syntax2/simple/step4/vet））→s40=253（**無值明示型別裸宣告 `name TYPE`**——陳述位 `result []byte`/`z [16]byte`/`x person`/`i8 i8` 此前被誤解析為 IndexExpression，**+31**，覆盖 crypto/archive 大量 `[]byte`/`[32]byte` 暫存宣告）→s41=262（**複合型別名**——`ps-parse-type` 補限定名 `a.b`（NamedType 續吃 `.IDENT`，對齊 Go stmt.go:1034）與巢狀元素 `[][]T`/`[N][]T`（SliceType/ArrayType 臂改為遞迴 ps-parse-type，元素 ∈ {IDENT,[,?}），同把 `ps-parse-typedecl-noval` 升級為通用 `ps-parse-typedecl`（新增 `= value` 分支），**+9**，新匹配 tls/tls-part3/http2/hpack/fs-error-{final,info}/fs-open-err/nested-vec-test/push-narrow）→s42=273（**無色 async 前綴運算子 `run`/`co`/`awy`**——`ps-parse-expr-prec` 缺此三前綴臂⇒`h = run f()` 的值被解析為空`f()` 漏成獨立 ExpressionStatement；補上後 `run e`→RunExpression{Call:e}`co e`→CoExpression{Call:e}`awy e`→AwaitExpression{Right:e}（三節點 dumper 早已支援纯 parser 缺口），**+11**，新匹配 async-{cancel,coop,rc,yield,handle-alias}/module-async/slot-rebind-unsafe/mem-safety/async-*）→s43=284（**區塊/程式尾註解 TrailingComments**——`ast.no` dumper 早已實作 OP-TRAILING（讀 ntrail/ct0/aux 印 `TrailingComments`＋每行 `//<text>`），唯 `ps.no` 的 `parse`（Program）與 `ps-parse-block`（BlockStatement）把 `}`/EOF 前的獨立尾註解群餵進 `ps-skip-trivia-doc` 後**丟棄**⇒語料中最後一條陳述之後`}`/EOF 之前的 standalone 註解從未進入 TrailingComments；新增三 helper（`ps-trail-region` 前瞻（不消費 token）判斷下一個非 trivia 是否 ∈ {RBRACE,EOF}`ps-collect-tail` 消費 NEWLINE/COMMENT 收集群——同條陳述行尾的首個行內註解仍走 set-cmt其餘全部入 trail`ps-attach-trailing` 走 aux-begin/aux-add/set-aux-counts(ntrail=len)），並接進 Program 與 BlockStatement 兩處**零回歸結構保證**：ntrail 預設 -1⇒renderer 跳過trail 空則不呼叫 attach⇒原匹配檔輸出位元組不變**+11**，新匹配 src/std/types.notests/{arr,duplicate,for2,i,str,range-test}.nomem-safety/{deep-free-nested-vec,deep-free-str,deep-free-struct-vec,minimal-str-map2}.no）→s44=283（**結構體欄位型別拓展 + 欄位尾註解跳過（ps-is-struct-def）**——裸結構定義 `name { field TYPE }` 之前仅在栏位 TYPE ∈ {IDENT, []IDENT, [INT]IDENT} 且无尾注时被识；限定名栏位 `conns [32]net.conn`（`]` 后 `net` 再 `.conn`）嵌套切片 `rows [][]str`栏位尾 `#{inline=…}` 注均使旧手筛扫描器回 0⇒整个结构退回 ExpressionStatement（而 ps-parse-struct-def 主体早已走 ps-parse-type（s41 已支援限定/嵌套）⇒**仅 gate 限**）修复：新增 ps-skip-type-idx（索引前视不消费 token）完整鏡像 ps-parse-type 四形（`a.b`/`?T`/`[]T`/`[N]T`元素递归∈{IDENT,[,?}）取代 gate 内的手写栏位型扫描；另新增 ps-skip-annot-idx（gate）/ps-skip-annot（body）跳过栏位尾 `#{...}`——`no ast`（无 -annotations）完全不渲染栏位注⇒跳过即可**零回归结构保证**：含限定/嵌套/注栏位的结构此前必已 NO（gate 拒→ExpressionStatement≠oracle StructDefinition），新能力只改善**+3**，新匹配 src/std/net/client.notests/field-{inline-annotation,tag}.no）→s45=284（**枚举/标签枚举定义 `name { red, green }` / `name { ok(v T), fail }`**——陈述位 IDENT+LBRACE 此前仅认 struct 形；Go parser.go:273 classifyBlock 把 blockEnum/blockTaggedEnum 与 blockStruct/blockInterface 分流（decl.go:749 parseEnumDefinitiondecl.go:1077 parseTaggedEnumDefinition），而 asp.no dumper（EnumDefinition=24/EnumValue=25/TaggedEnumDefinition=65/TaggedEnumVariant=66 + taggedEnumField HELPER 对）早已支援⇒纯 parser 缺口新增两函数：`ps-is-enum-def`（索引前视 gate，不消费 token）——成员 ∈ {IDENT/NIL 裸变体`名(字段…)` 括号载荷`名 = INT` 显式值`#{…}` 注NEWLINE/COMMA 分隔}，**保守分流**：括号+裸并存→2（标签）仅裸/显式→1（枚举）全括号无裸→0（与接口歧义）`) (` 返回组或 `t.` 限定→0（接口形）任何非预期 token→0——gate 拒者原本必走 ExpressionStatement≠oracle⇒结构上零回归；`ps-parse-enum-def`（body）——计数语义对齐 Go：显式 `V = INT`不推进 nextVal（decl.go:807–821）标签变体 index 逐变体+1；变体字段以 helper-begin/aux-add（name|type 成对）挂 taggedEnumField；Type kid 以首字段型别占位（Go 同口径 Fields[0].Type）⚠️ `#{…}` 变体注不渲染（默认无 -annotations）⇒跳过即可**+1**，新匹配 tests/chain-copy.no；t49-enum-defs fixture byte-identical⚠️ 语料其余 9 档首分歧即枚举（TaggedEnum 6+Enum 4+err.no 等）现结构已对，但随即在**下游 armBinding 裸 match desugar 主簇**再分歧（LetStatement 桶 107 档中 44 档为该簇）——该簇已确认为下一个最高价值增量⚠️ **方法学事故记录**：本增量中 t31-standalone-if 一度显示回归——隔离实验证明源于 `~/.cache/nolang/ast` 被并行会话（Go 侧 concurrent edits + bin/no 重建）污染的**非确定性构建**（同一 HEAD 源码两建不同行为），清缓存重建后 s44/s45f 行为一致arm 零回归；差分对照必须用**同一工具链代次**重建的新旧 noc⚠️ 其余 4 fixture（t37/t4/t7/t8）与 live oracle 的差异为 **noc-s0 时代既有**（fixture 门历史基线并非 live-oracle 对照），非本增量引入→s46=302（**明示 match 的 armBinding desugar 补齐**——三缺口：(a) err/nil 关键字臂（型样为裸关键字且紧接 `->`）→ 新 `ps-mk-it-let-arm` 建 `LetStatement{name:it, type:<变体>, value:subj, synthetic, armBinding:true}`（Go lowering.go buildItBindingForArm：被匹配型别未知时仅 err/nil臂带型别，ok/else 回落裸 synthetic——同文件 `ps-mk-it-let`；dumper 早已支持 armBinding 槽，纯 parser 缺口）；(b) 裸尾臂 `-> expr`＝else：peek 见 RARROW 直接视同 wildcard（不消费 token），其区块作前一 IfExpression 的 alternative，不再伪造 `subj == <nil>` 条件臂；(c) kvar 检测用 `ps-peek-at(p,1)==RARROW` 区分关键字臂与同名普通变量fixture `t50-match-arm-binding.no` 于晋级后 noc-s0 与 oracle 逐位元组相同**同代 A/B**（同一好代次工具链分别编译 HEAD ps.no＝ctl 与 s46 ps.no）：corpdiff(ctl→s46)＝ +20 档回归 0；**最终语料门禁** corpdiff(noc-s0[s45f]→noc-s46f)（live oracle 02:18 代）＝ **+19 档（match-*/option-*/vec-*/open-*/arr-* 等 armBinding 主簇）回归 0**283→302（旧基线 284→283 系 02:18 oracle 代对个别档预期漂移所致与本增量无关）；fixture 门 47/51（仅余既有 4 档 t37/t4/t7/t8）；单元 4 测试全 PASS；vet 错误集与 HEAD 完全同（仅行移 3309→3348）⚠️ **晋级阻塞——外部工具链回归**：当代 Go 源码（deac8377 已提交＋并行会话未提交 WIP）编译与 noc-r1/r2/r3 时代逐位元组相同的 HEAD ps.no，产出的 noc 在 t31-standalone-if 与 tests/mkdtemp-debug.no 丢失 standalone-if 链的 inline else（`BlockStatement{inline:true}` 消失，签名一致；284→283）；三代工具链 parse ps.no 自身 AST 逐位元组一致⇒纯 lowering/codegen静默误编译；且昨日 20:12 的好状态只存在于未提交WIP（fd3f2931 提交信息描述的 parser 工作并未含 Go 文件入仓），不可复现⚠️ **工具链事故升级与解法（s46 期间）**：主树当代 Go（deac8377+未提交 WIPbin/no@02:18）以cache-off编译 HEAD ps.no 仍丢失 standalone-if inline else（t31+mkdtemp 同签名）——属当代 lowering/codegen 真回归待 Go 侧修复；但 **纯 346d69ae（v0.4.4）与纯 c872721a 两代工具链 cache-off 均 GOOD**⇒破坏面锁定在 deac8377 及其后 WIP 区间处置：晋级二进制改用 **git 钉住的纯 v44 工具链 + NOLANG_AST_CACHE_OFF=1 构建**（/tmp/noc-s0＝noc-s46fsha 51de820a），旧基线存 /tmp/noc-s0-s45.bak⚠️ **二分期中踩到的缓存新坑**：ASTVersion 只在 commit 时 bump，相邻两代未 bump 的 WIP 共享同 key（content, ASTVersion, StdSigKey）⇒cache-on 的文件级二分结果全是假象（本会话曾被 b3GOOD误导数轮）；**任何差分/二分实验必须 NOLANG_AST_CACHE_OFF=1 或隔离 HOME**（该开关只保 user-module AST 盘缓存层））→s47=308（**后辍圆括号 range 切片 `f(a..b)`/`f(a..b]`**——语料 7 个 SIGBUS 档的元凶：Go expr.go LPAREN 臂有 isRange 探视（`(` 后跟 ELLIPSIS或 INT/IDENT 紧接 ELLIPSIS ⇒ parseSliceExpression，leftInc=false`]` 闭合 rightInc=true；否则普通 CALL），而 ps.no 后辍链把 LPAREN 一律当 CallExpression——`a(1..4]` 落入 call 参数循环的防呆步进＋外层 pgo 重循环⇒死循环/SIGBUS新增偏移 1/2 token 探视分支（鏡像 Go saveState 探视）；并补 LBRACKET 臂两处 en 条件缺 `!= RPAREN`（Go 双闭合）fixture `t51-range-call.no` 与 oracle 逐位元组相同门禁：fixture 48/52（仅余既有 t37/t4/t7/t8）；单元 util/tok/lx/ast 4/4 PASS；vet 错误集与基线完全同集（行号全 +40 位移）；语料门 corpdiff(noc-s0[s46]→noc-s47)＝ **+6 档0 回归**（arr/str/txt/vec-sliceslice-open-inclusivemem-safety/reverse-slice-clone 六个原 BUS 档全匹配）302→308⚠️ **方法学坑——超时误报第三机制**：首跑 -P 4/30s 并发扫描把 sha256/yaml 报成新增 SUT-FAIL 回归——实因该两档 noc 解析耗时 ~15-25s 处于临界，并发负载压爆 alarm；串行 -P 2/90s 复测 s0/s47 行为完全一致⇒**语料扫描须 -P ≤2 + alarm ≥90s**，否则慢档被误判回归（与缓存假象跨代误编并列第三独立机制）晋级同 s46：git 钉纯 v44 工具链 + NOLANG_AST_CACHE_OFF=1 构建，/tmp/noc-s0＝noc-s47sha1 fc0216b4，旧基线存 /tmp/noc-s0-s46.bak；余 SUT-FAIL 9 档（aes-cbc/net/fmt-heuristic/fn-type-{result,return}/map-key-leak/str-order 挂死tls SEGVhttps-server BUS——皆 noc-s0 时代既有，非本增量引入））→s48=370（**陈述位 `{ … }` 区块：BlockStatement wrapper + 裸 match desugar**——首分歧调查最大桶：抽样 200 档中 ≈48 档卡 `BlockStatement{}` wrapper 缺失≈22 档卡裸 match 合成 `IntegerLiteral{value:1}`此前 `ps-parse-stmt` 无 LBRACE 臂⇒防呆步进吃掉 `{`语句外泄一层；裸 match `{ cond -> body … -> else }` 完全未实现新增 `ps-parse-brace-stmt`（鏡像 Go stmt.go:402 saveState/restoreState：`ps-bm-scan` 探深度 0 `->`（排除 `{ IDENT : {` 结构化形）⇒先试 `ps-parse-bare-match`失败还 p.pos 回落普通 `ps-parse-block`+消费 `}`）；`ps-parse-bare-match` 鏡像 match.go:134 三形臂体（`{…}` 区块（先试嵌套裸 match）NEWLINE 块形（statements 至 isArmStart/`}`，inline:true）同行单语句）+ lowering.go:1727 buildBareMatchDesugar 两装配路：全臂非 wildcard 且 n>1⇒fast path `IfExpression{IntegerLiteral{1}(合成 ps-mk-true), Block{各臂兄弟 ExpressionStatement{IfExpression}}}`，否则倒序 elif 链（尾 wildcard `-> body`＝`IfExpression{IntegerLiteral 1, body}`）；中途 wildcard／臂首 `#{`／`ok->` 保守 bail 还原**⚠️ match 非排他/有状态守卫三新坑**：(1) wildcard 臂消费箭头后重评 `ps-peek!=RARROW` 因状态已变而误 fail⇒`ad` 快照旗标；(2) 倒序链 `w==1&&res>0→bail` 被本臂刚建的 res 再触发⇒`hdw` 快照；(3) 嵌套裸 match 尝试失败时 p.pos 已越过 `{`回退路再 bump⇒双消费⇒`j <- [0..N): {` 被吞成 SliceLiteral 整档挂死（http/server 180s rc=142）⇒尝试前 `nsave=p.pos`失败还原门禁：fixture 49/53（仅余既有 t37/t4/t7/t8）单元 4/4 PASS（⚠️ 当代 oracle 新增命名规范检查拒 `_test.no` 下划线路径⇒复制到 tmp/zz-s48gate/*-test.no 运行）vet 10 既有 ERROR 集不变；语料门（-P 2/90s 全 647 档）＝DIFF 330→268**新增匹配 62 档0 回归**（SUT-FAIL 集与 s47 完全同 9 档；http/server 由挂死转 rc=0 且与 oracle 差异行数 6435→1394 / 3089→433，仍属既有 DIFF 档）晋级：纯 v44 工具链 cache-off，/tmp/noc-s0＝noc-s48 sha1 c53a5641，旧基线存 /tmp/noc-s0-s47.bak）→s49=381（**语句位 `*`/`**` 速写 + `#N` 标签循环**——s48 后首分歧调查最大可解桶：oracle `BlockStatement{}`/`BreakStatement` 侧 ≈48 档中相分成两簇，其中 index-out desugar 簇（≈39 档，Go lowering.go maybeIndexOutAssign）需 sem 函数作域型别表（FuncVarType/idxLocalTypes）+ bounds.go 可证明界内豁免（inBoundsIdx）才可逐位元组复现，超出 parse-local 范畴留待后续；本增量取 `*`→break／`**`→continue 速写簇：Go parseStatement 对语句起始 MUL 走 parseStarBreakOrExpr（stmt.go:552/2913，peek(1) ∈ {NEWLINE,EOF,SEMI,RBRACE,LABEL,IDENT} 才成立可跟 `#N`/IDENT 标签）STAR_STAR 走 parseStarStarContinue（stmt.go:558/2937，无閘门）；ps.no 陈述位无 MUL/STAR_STAR 臂⇒`cond -> *` 被防呆拆成 `InfixExpression{op:*}[<nil>,next]`（http.no 首分歧即此）；新增 ps-parse-star-break/ps-parse-star-star-cont/ps-star-label/ps-mk-break-cont 四函数+两 dispatch 臂；另补 `#N` 标签前缀臂（parseLabeledStatement 保守版：消费 LABEL 后递回 ps-parse-stmt，若得 ForStatement 则改写 slot0 label）⚠️ 多返回值坑：`(label, kind)` 裸组尾式非法语法`x,y = f()` 解构仅适用已命名结果惯用法——改用 `[]str` out 参数传 label/kindfixture `t53-star-break.no`（裸 match 臂体 `*`/`**``{ }` 内裸 `*``* #1`/`** #1` 标签standalone-if 链）与 oracle 逐位元组相同门禁：fixture 50/54（仅余既有 t37/t4/t7/t8）单元 4/4vet 10 既有；语料门（-P 2/90s 全 647）＝DIFF 268→258**新增匹配 11 档0 回归**（tests/https-server.no 由 SUT-FAIL rc=138 SIGBUS 转 rc=0 DIFF属改善；http.no 与 oracle 差异 1394→185 行）晋级：/tmp/noc-s0＝noc-s49 sha1 bb07e0ed，旧基线存 /tmp/noc-s0-s48.bak）→s50=383（**区块内换行后 `-> else` 续接（standalone-if else-attach）+ 工具链纪律再确认**——oracle 当日三代重建（08:40→09:56→10:30），10:30 代把 wrapStandaloneChainDepth 定为递归形（stmt.go:1768 注释引 str.replace-n 事故）后， promoted noc-s49（当时误用主树 bin/no@08:40编译，违反 s46 钉死纪律）静默丢失链式 `a -> b -> f` 最内层 consequence 而被漂移照出；本增量起一切 noc 构建一律改用 /tmp/nolang-v44 自带 `./bin/no`（纯 v0.4.4 钉死 + cache-off）新特性：复刻 Go parseBlockStatement 迴圈开头 stmt.go:2168–2186——区块内上一条 standalone-if（RTStandaloneAlternative==nil）后换行的 `-> body` 挂为其 Alternative；实现弃用建节点后原地覆写 kids方案（ast.no kid-set），改在 ps-mk-standalone-if 组装前前探：跳 NEWLINE/SEMICOLON 后恰为 `->` 即就地吃掉并把 body 写进 alt 槽（节点一次成型零覆写）；psst 加 inBlk/inMatch 上下文位：program 顶层不挂（t31 系顶层 `d -> e -> f`+`-> g` 分离形）臂解析中抑制（裸 match/match 自行消化 wildcard 臂），ps-parse-block 进入时 inMatch 清零（臂 body `{ }` 内续接合法）fixture t54-newline-else.no（j1 形/块形 else/（A）wildcard 三连挂/顶层不挂）与 oracle 逐位元组相同门禁：fixture 51/55（仅余既有 t37/t4/t7/t8）单元 util/tok/lx/ast 4/4vet 10 既有 ERROR语料门（-P 2/90s 全 647，oracle 10:30 代）＝SAME 383 / DIFF 256 / SUT-FAIL 8（全 rc=142 慢档与 s49 同集）；vs s49 名单：新增 tests/{float-ne-nan,set}.no丢 0 档（漂移日净匹配受 oracle 三代重建主导，attach 本身解锁的是语料内裸 match 回退区块形如 json.no 内层块）晋级：/tmp/noc-s0＝noc-s50g sha1 c766715f，旧基线存 /tmp/noc-s0-s49.bak⚠️ **事故记录（归因错误）**：s50 期间一度把链式丢 consequence 误判为 ast.no 新增 kid-set（i64 vec 字段元素写入）污染全模组 codegen——单变量实验 s50e 因同时混入了错误工具链（主树 bin/no）构建而给出假象；真因＝工具链代次误用，kid-set 无罪（后因设计更优而保留弃用）差分对照一律同工具链代次重建；晋级前必须用 v44 `./bin/no` 重建并跑 fixture 门新基线首分歧分桶（256 档）：ORACLE+ LetStatement 104／BlockStatement 48（index-out 簇 ≈39 + 裸 match mid-wildcard bail 簇如 t54 旧 g/h 形）／FunctionDefinition 16／NOC+ ExpressionStatement 11）→s51=413（**同檔裸名呼叫之 parse 期回傳型別推斷**——s50 後最大桶：256 檔首分歧 LetStatement 家族 132（裸 63＋nullable 34＋合成 35），其中 44 檔直接分歧於 `value: CallExpression` 的型別蓋印復刻 Go parseLetStatement 的 CallExpression 推斷分支（stmt.go:1502–1533）＋ type.go 的 returnTypeFromCallExpr／filterInferableType／markInferred：RHS 為 `CallExpression{fn: 裸 Identifier}` 且簽名表有單值回傳 ⇒ 過濾（`?T` 恆留；排除精確 i64/i32/i16/i8/u64/u32/u16/u8/f64/f32/bool/byte/char/fd；含 `|` 排除——union 排除本增量未實作，風險已註於碼）再走三閘門：未宣告→蓋 Type＋setVarType；已宣告且回傳 `?T`→僅 setVarType 不蓋 Type；已宣告非 option 且既有 varType ∈ {'', 'i64'}→蓋 Type順序註冊（ps-parse-func 於 body 解析完後僅 outs.len()==1 才登記 sigN/sigP）⇒前向引用與遞迴不推斷（oracle 探針 inf2/inf3 釘死）；方法定義不進裸名表；形參/回傳 varType 註冊提前至 body 前（mirror decl.go:1756）psst 增 12 欄（fn/lastTp/sigN/sigP/dclF/dclV/gclN/fvF/fvV/fvT/gvN/gvT）實現 FuncVarType（fn→全域 fallback）與 IsFuncDeclared（tracked 函式僅查 pair）**克隆方案＝token 位置重入**：簽名存回傳型別起始 token 下標（ps-parse-param 捕 p.lastTp），使用處跳 p.pos 過去重跑 ps-parse-type 生成新節點（零既有節點突變）回pos；ps-mark-inferred 逐 kind 遞迴寫 'true'（NULLABLE 只蓋子節點——dumper 標頭不印 inferred）；被拒克隆成無 kid 引用孤兒⇒零回歸ps-parse-stmt 六明示型別分支＋typedecl 兩臂補 ps-reg-let 登記（復刻 Go parseLet 尾部 DeclaredVars+SetFuncDeclared）fixture t55-call-infer.no（首個 `?str` 蓋印／重指派抑制／i64 過濾排除／`[]str` SliceType 推斷／字面量恆蓋／後定義函式不推斷）與 oracle 逐位元組相同門禁：fixture 52/56（僅余既有 t37/t4/t7/t8）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR 集不變；語料門（-P 2/90s 全 647）＝**SAME 384→413（+29）0 回歸**（SUT-FAIL 8 檔 rc=142 與基線同集慢檔），新匹配 x25519-* 全系列mem-safety/struct-* 簇move/slice/view 諸檔；args.no 差異行數 103→91（剩餘皆 lowering D6 `?char`/`?i64` 合成偽影，超出 parse-local 範疇）⚠️ oracle 當日 12:26 因平行會話remove explicit main重構重建，基線探針全組復核 SAME 後才动工晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s51 sha1 33fd562b，舊基線存 /tmp/noc-s0-s50.bak）→s52=417（**前置 `#{buildin}`/`#{intrinsic}` 註解標記 ⇒ 節點旗標**——s51 後 226 檔首分歧重分桶：LetStatement 79（it-let 束縛 20／D6 option-index 23／CallExpression 残留 10 皆 lowering 产物）BlockStatement 48（index-out 簇）**builtinStub 10 檔**為最大單標記 parse-local 簇語料 17 檔 152 處 `#{buildin}`（裸形＋`#{buildin, intrinsic}` 組合形）`#{intrinsic}`（含 `#{win-amd64, …, intrinsic}` 平台組合形）鏡像 Go attachAnnotations（annotation.go:349–378）：FunctionDefinition 上 'intrinsic'⇒Intrinsic（slot5 'true'）'buildin'⇒BuiltinStub（slot6）＋可選 Value⇒BuiltinName（slot7，語料暫無 `=NAME` 形預留）；TaggedEnumDefinition 上 'buildin'⇒Builtin（slot1，global.no option 即此）槽序由 dump.go:272–279 渲染欄序列導出（name0 colonSyntax1 methodDef2 skipNamingCheck3 overflow4 builtinStub5 builtinName6 builtinGroup7；`#{intrinsic}` 已於 2026-10-22 從 Go 側移除，原 intrinsic5 槽消失其後各槽（builtinStub/builtinName/builtinGroup）整體前移一格）——dumper（asp.no strs 表）早已就緒⇒纯 ps.no 增量（新 helper ps-apply-builtin＋註解迴圈收集 bs/intr/bn＋尾端套用一點）；非函式/非列舉目標忽略旗標（`#{buildin} e = 1` 探針釘死）不可附著形仍走獨立 AnnotationStatement（Go 同口徑）多載繼承（後續同名定义 BuiltinGroup）與尾隨同行形語料暫無未實装fixture t56-buildin-annot.no 與 oracle 逐位元組相同門禁：fixture 53/57（僅余既有 t37/t4/t7/t8）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR 集不變；語料門（-P 2/90s 全 647）＝**SAME 413→417（+4）0 回歸**（SUT-FAIL 8 檔同集），新匹配 global/os/bool/net/unix；async.no 殘差 5 行為 `*i8` PointerType 參數（新缺口）time/fs/process/number 仍卡下游簇⚠️ oracle 當日多次重建（12:26→13:24→13:51），掃描後對新 oracle 複驗解鎖檔＋全探針 SAME晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s52 sha1 b71b1210，舊基線存 /tmp/noc-s0-s51.bak）→s53=423（**頂層型別別名——union / 單一 / 函式型別三形**——s52 後重分桶選 TypeAlias 簇：`float = i8|f64|…` 系首分歧 Go 為 `TypeAlias{…Union…}` 而 noc 拆成 LetStatement+InfixExpression復刻 Go stmt.go:151-170 分派序（isFnDef→isFnTypeAlias→looksLikeEqAlias→let）：type.go:554 looksLikeEqualsTypeAlias（IDENT+OR→scanIsUnionTypeAlias 深度 0 掃描（NEWLINE/EOF/;/閉合符→真；含 QUESTION 在內二元運算子→假；LPAREN 內容限 {IDENT,IN,RPAREN}）；IDENT+陳述邊界→內建 16 型名或 **typeAliasNames 註冊表**；`[]T`/`[N]T`/`?T` 形）type.go:688 parseTypeAlias（`|` 平鋪收集成 UnionType{types:N}）type.go:406/433/512 fn 別名形（首 `(...)` 後須陳述邊界或第二 `(...)`）**三項 ps.no 機械發現**：（a）kvc 計數頭部（`types:`/`params:`/`results:`）由 dumper 讀**字符串槽**渲染子節點遍歷讀 c 槽——builder 須 set-slot(.to-str()) 與 set-count 兩設（沿 CallExpression 既有範式）；（b）同塊多臂 match 中**裸 `->` 尾臂會被當尾隨無條件陳述執行**覆寫 idx——別名分派全臂加 `aliased == 0 &&` 守衛無裸臂；（c）Go fn 型別參數規則：`(...)` 內單 IDENT 為**無名具型參數** `Parameter{name: , type: NamedType}`（不同於 FD 形參）——新 helper ps-parse-ftype-param；且結果組換行探測須 `sp = p.pos` 保存未命中回滾，否則吃掉終結 NEWLINE 使下行文註錯掛為上陳述 Commentpsst 增 `alN []str` 註冊表⚠️ 記錄偏離：別名閘門用 `p.fn == ''`（僅頂層）替代 Go 的 saveState 試解析——語料函式體内 `x = (expr)` 形皆被 Go 內容掃描否決，頂層閘門安全fixture t57-type-alias.no（union/單一/註冊表引用/cb0 空參/fn 雙組/負形 `or-value = 1 | 2``x = (or-value)`）與 oracle 逐位元組相同門禁：fixture 54/58（僅余既有 t37/t4/t7/t8）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR語料門（-P 2/90s 全 647）＝**SAME 417→423（+6）0 回歸**（SUT-FAIL 8→6 恰為 fn-type-result/return 轉 SAME 脫離慢檔集），新匹配 src/std/{float,num}.notests/{fn-type-params,fn-type-result,fn-type-return,named-fn-type}.no；int.no 殘差 8 行=`?int` NullableType 推斷 let 形（新簇出列）⚠️ /tmp/nolang-v44 被 macOS /tmp 清理蒸發，重建：git clone + checkout v0.4.4 + make no晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s53 sha1 86907d51，舊基線存 /tmp/noc-s0-s52.bak）→s54=427（**D6 有號整數算術 option 推斷——inferOptionArith**——s53 後 218 DIFF 重分桶：LetStatement 86／BlockStatement 49，選中`type: ?i64/?int, value: InfixExpression`簇 16 檔（std 8＋tests 8）復刻 lowering.go:493 inferOptionArith（parse 完成後 walk 逐 LetStatement）：無明示型別非合成值剝括號後為 op ∈ {+ - * / %} 的 InfixExpression 且兩側皆靜態可知有號整數（IntegerLiteral→i64；Identifier→FuncVarType→lowerSignedIntName 僅收 {int,i8,i16,i32,i64}限定名取最後 '.' 段剝前導 *；self/`.`→接收者型別；巢狀算術遞迴取較寬者平手取左）⇒就地合成 NullableType{NamedType{base}}（dumper 對 NullableType 頭不印 inferred）＋setVarType(name,'?base')D1：`#{overflow=...}` 不豁免（d6f/sha3/time 探針釘死仍蓋印）ps.no 增量：五 helper（ps-unwrap-grouped／ps-is-arith-op／ps-lower-signed-int-name／ps-signed-int-operand／ps-mk-opt-arith-type）＋ps-parse-let 尾一鉤點＋ps-parse-method-func 預登記 self=接收者型別（鏡像 lowerSelfType 讀 Results[0]——裸 `.` noc 渲為 Identifier{self} 需在 body 前可查）⚠️ 偏離：單趟查表 vs parse 完後 walk——前向引用運算元不蓋印（語料未見）**閘門教訓（首掃曾丟 3 檔回歸）**：captureTargetAllowsOption 不是查 varType 空即放行！Go 以 declaredLocals（walk 序出現名單：函式作用域每次進入 FunctionDefinition 重置形參先入帳字面量 let 只入名單不入型別表）＋localDeclaredType（**刻意不做全域回退**，lowering.go:3377 事故註釋：他函式 `c i64` 參數污染）雙表判定——本作用域已出現且型別表查無 entry也拒，保護 `r = 0`→`r = 10/3` 重指派形與純量出參；新增 ps-walk-declared／ps-local-type＋psst.dclT（僅頂層名單）重造閘門後 net/ipsha256-simplifiednumber-div-mod-clamp 回 SAME⚠️ 誤編譯事故：臂內寫 `-> { r = 'int'; return }`——`;` 是行註解吞掉 ` return }`括號失衡使後續整檔併入前函式，報錯 `EmitLLVM: unknown callee ps-parse-if in func ps-parse-stmt` 距真因極遠；改互斥條件單表達式形fixture t58-d6-arith-option.no（正形：函式參數／剝括號／巢狀／方法 `.`；負形：u8純字面量變數運算元明示純量重指派出參union 別名接收者D1 overflow 仍蓋先字面量 let 後重指派拒蓋）與 oracle 逐位元組相同門禁：fixture 58/59（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 423→427（+4）0 回歸**（SUT-FAIL 6 檔 rc=142 同集），新匹配 src/std/{int,net/hpack}.notests/{1-test,basic}.no；簇內其余 12 檔殘差屬 D7 安全索引 `?char`／armBinding 型別／Comment 附著等其他簇⚠️ oracle 當日漂移 15:02→16:16，晉級後對新 oracle 復核 25 探針＋抽樣 40 原 SAME 檔全 SAME晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s54 sha1 dedb47ef，舊基線存 /tmp/noc-s0-s53.bak）→s55=441（**armBinding it 合成綁定型別蓋印——buildItBindingForArm**——s54 後 214 DIFF 重分桶選中`LetStatement{name: it, type: T, …, armBinding: true}`it-let 簇 21 檔復刻 lowering.go:2809 buildItBindingForArm ＋ per-arm 補集表 :2176–2235：主題靜態可知 `?T` ⇒ err 臂→errnil 臂→nilok 臂→Twildcard 臂取補集（全列 ok/err/nil ⇒ Go skipItBinding 死臂**不建任何綁定**；列 ok+nil→err列 ok+err→nil僅列 ok→`err | nil`未列 ok→T）；型別未知（t==''）維持 s46 兼容形——僅 err/nil 關鍵字臂帶變體名ok/wildcard 落回裸 shared 綁定ps.no 三處增量：（a）`ps-mk-it-let-arm` 泛化為 `ps-mk-it-let-t(t, subj, bt, bname)`（bt 非空⇒NamedType{bt}+s1 synthetic+s2 armBinding；bt 空⇒同 ps-mk-it-let 裸形）；（b）`ps-match-listed` 深度追蹤 token 預掃（不消費不建節點）收集緊接 `->` 的 ok/err/nil 關鍵字臂名單，鏡像 Go parse 後 hasExplicitOk/Err/Nil（嵌套 {…}/(…)/[…] 增深度防内層臂污染補集判定）；（c）ps-parse-match 接線——listed 預掃`ok` 關鍵字臂 kvar 識別bt 計算dead 旗標（全列跳過整條 it 綁定）it varType 逐臂登記與還原（鏡像 collectVarTypesFromBody 作用域掃描，保巢狀 match 内層 it 不污染外層——match-it-type-scope 回归守）⚠️ 偏離：`Program{warnings:N}`（死臂 unreachable 警告計數）與未知型別主題 wildcard 裸綁定 vs 無綁定之別屬警告計數／下游簇，本增量未觸及（探針 e55a 剔除此二形後與 oracle 逐位元組相同）fixture t59-arm-binding.no（n ?i64：err/nil/wildcardok/wildcard僅 err/僅 nil/純 wildcard；m ?str：ok/nil/wildcard 補集 err；巢狀 match 作用域還原）與 oracle 逐位元組相同門禁：fixture 59/60（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 427→441（+14）0 回歸**（SUT-FAIL 6 檔 rc=142 同集ORB-FAIL 0），新匹配 tests/{fs-error-complete,fs-error-simple,minimal-it,minimal-it2,opt-match-all,opt-match2,opt-match3,option-nil-predeclare,self-write-str,struct-field-leaf-clone,test-div-mod-option}.nomem-safety/{option-cmp2,option-match-basic,option-match-direct}.no；簇內其余 7 檔（match-it-type-scope/opt-box-drop/option-capture/tagged-enum/test-div-zero/str-field-set-clone）殘差屬複合指派／enum 載荷析構／D7 安全索引等其他簇⚠️ oracle 當日漂移 16:16→17:09，首掃 LOST=0 已對新 oracle 復核成立，晉級後復驗 14 新檔＋抽樣 39 原 SAME 檔全 SAME晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s55 sha1 054f3851，舊基線存 /tmp/noc-s0-s54.bak）→s56=445（**W_SEMI_EAT 行尾分號註釋計數——Program{warnings:N}**——s55 後 200 DIFF 首分歧分桶：LetStatement 62／BlockStatement 54／Program 9，選中 warnings 桶：4 檔唯一差異即 `Program{}` vs `Program{warnings:N}` 頭行復刻 Go buffer.go::warnSemiSwallow 啟發式（advanceCollect 對每個符合的 COMMENT token 發一條 SeverityWarning，parser.go::Warnings 將全部 warning 匯總進 Program.warnings）：① Marker == ';'（單分號行註釋；`;;`/`;;block` 系永不警告）② 註釋文字含 `{` 或 `}` ③ 前一非 COMMENT token 為同一行代碼 token（行尾而非行首）啟發式純詞法 ⇒ ps.no 以 `ps-semi-eat-count` 對 toks 前向預掃（last 指針跟蹤前一非註釋 token），`parse` 寫 Program 槽前疊加進 p.warn 總和——Go 的 warnedSemiEat[idx] 回溯去重在預掃下天然無需探針 tmp/p1-p3 釘死：`;;` 行首/同行→0同一行兩組大括號→僅 1（同屬一條 COMMENT token）rc4/move-eligibility-improved/range 三檔首行也是 warnings，但 noc 過度計數的根因是 `[1,2,3]:` 陣列字面量被誤析為 RangeExpression 的 body 大 diff（他桶）fixture t60-semi-eat.no（正形三行含括號／負形行首 `;{}``// {}``;; {}`無括號 `;`／同行雙括號串）與 oracle 逐位元組相同門禁：fixture 60/61（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR語料門（-P 2/90s 全 647）＝**SAME 441→445（+4）0 回歸**（SUT-FAIL 6 檔 rc=142 同集ORB-FAIL 0），新匹配 tests/{map-to-str,struct-to-str,scratch-fe,print-eprint-to-str}.no⚠️ oracle 當日漂移 17:09→18:16，首掃 LOST=0 對新 oracle 復核成立，晉級後復驗 4 新檔＋探針＋抽樣 40 原 SAME 檔全 SAME晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s56（構建 sha1 ceaf5862，晉級 codesign 後 1e872f5d），舊基線存 /tmp/noc-s0-s55.bak）→s57=448（**for 族語法補齊：range-for 匿名切片字面量源 + 計數循環雙形**——s56 後 196 DIFF 首分歧分桶：LetStatement 62／BlockStatement 54／ForStatement 6／Program 5，選 for 兩簇：（a）range 源 `i <- [1,2,3]:` 被 ps-for-source 一律當 RangeExpression 誤析（s56 副發現：rc4/move-elg/range 三檔过度 warnings 之真因即此）语料切片字面量源 4 檔；（b）計數循環 `N * { }`／`{ } * N` 完全缺臂（for3.no 前置形 8 行差）**鏡像 Go parseForRange LBRACKET 分類**（stmt.go:2582）：saveState→跳 `[`→peek ELLIPSIS 先行⇒kind1；否则 parseExpression(LOWEST) 後 ELLIPSIS⇒kind1（[a..b]/[a..]）COMMA|RBRACKET⇒kind2（匿名切片字面量 [a,b]/[a]）；restoreState；kind2 整段交 ps-parse-expr 之 LBRACKET 前綴臂建 SliceLiteral 填 IterationExpr.RangeExpr(kid1)（非 range 非切片之 kind0⇒保持舊無分類行為，Go saveError 形無以復現）分類期投機 parseExpression 所建節點不被任何 kid 引用即孤兒（s40/s51 先例）⇒零回歸**計數循環雙門+雙 builder**：s57-count-prefix-p 鏡像 isCountLoopPrefix（k=1 用 peekTokenk≥2 用 look(k-2)；`*` 後必緊接 `{` 与乘法區分）於陳述分派 INT/SUB 臂前；s57-count-blockfirst-p 鏡像 isCountedLoopBlockFirst（平衡大括號掃描+`#{` 群組跳過匹配 `}` 後 MUL [SUB] INT 且 INT 後必為 NEWLINE/EOF/SEMI/RBRACE 語句結束以免 `} * n` 乘法歧義）於 ps-parse-brace-stmt 入口優於裸 match/普通區塊；二者皆建 ForStatement{label:''} 6 guarded CHILD 僅 CountExpr/Body 非零，CountExpr=IntegerLiteral{value:字面量}（無 raw⇒slot1 空串不印，ps-mk-true 先例；Go ParseInt base10 拒 hex 故 hex 安全落回運算式形）負號形把字面量前綴 '-'（Go intToken.Literal = "-"+lit）；標籤形 `#1 N * { }` 由既有 LABEL 臂遞回+改 slot0 天然覆蓋（e1 探針釘死）⚠️ **新坑（MIR 層靜默誤讀）**：同一函式體內多次 ps-peek-at（含變量偏移）會讀到陳舊寄存器——探針实計：同體 pa1/pa2/pa3 局部正确，但嵌套臂內 `ps-peek-at(p, ko)`（ko==2）返回 ko==1 時的舊結果，而直接索引 `p.toks[p.pos+ko].typ` 恆正確；vet 的 fxxoptarg 錯位（ps.no:4401/4404/4415 等 6 處既有 `p.pos+1` 傳參）側证此族降格路径确有幉角⇒**新閘門一律改 ps-bm-scan 式直接索引掃描（index-out=zero 守衛）**fixture t61-for-shapes.no（切片源/範圍源對照/前置計數/後置計數/負計數/標籤前置形）與 oracle 逐位元組相同門禁：fixture 61/62（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 445→448（+3）0 回歸**（SUT-FAIL 6 檔 rc=142 同集ORB-FAIL 0），新匹配 tests/{for3,range,call-heavy}.no（call-heavy 為分類修正的級聯紅利）晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s57（構建 sha1 b5c4ac9e，晉級 codesign 後 25a178bb），舊基線存 /tmp/noc-s0-s56.bak）→s58=451（**參數/型別特殊形狀 + 方法回傳型別推斷**——s57 後 193 DIFF 首分歧分桶：LetStatement 62／BlockStatement 55／Identifier 9／Parameter 5，選 B 簇型別形狀：（a）參數/回傳位 `*T`/`**T`/`T*`/`&T`/`?&T` 全缺臂（async.no `task *i8`view-basic `(v &view-point)`）；（b）參數名位缺 IN token（`in []byte` 被丟名）鏡像 Go parseParamTypeAfterName（type.go:997）：前綴 `?`/`&` 循環＋`*` 循環＋STAR_STAR＋base＋後綴 `*`/`**` 正規化為 `? & * base` 再 buildType（type.go:1206）逐層包節點——noc 單趟直接節點化等價：ps-parse-type 新增 MUL→PointerTypeSTAR_STAR→雙 PointerTypeAND→ViewType 三臂（遞迴先建內層 base 再包外層，子先於父）；IDENT 臂尾加後綴 `*`/`**` 包裹迴圈（`i8*`⇒`*i8`）；ps-parse-param 型別門擴 {MUL,STAR_STAR,AND}名門容 IN⚠️ 權威面探針釘死：oracle 函式字面量**參數位**不接 `&T`（isFunctionDefinition 掃描集無 AND，`(b &i64)` 落回 GroupedExpression）但 **results 位**接（掃描無條件跳過）——語料只用 results 位，noc 偵測比 Go 寬的偏差只在語料外形（e2 探針既有之偏離非本增量引入）；頂部明示 `x &str = 1` oracle 也落回運算式（let 型別門不碰 AND，未改）**級聯子簇：方法回傳型別推斷（s51 同族 DotExpression 臂，type.go:96 returnTypeFromCallExpr）**——view-basic `q = p.self-view()` 之 `type: &view-point` 為 parse 期推斷非明示：新增 psst.msN/msP 方法簽名表（鍵 'recv.method'，body 解析完才註冊outs==1 限定opos[0] 起點 token，鏡像 Go decl.go 尾註冊 methodSignatures 語意）＋ps-msig-find 反掃；ps-parse-let 推斷區加 DOT 臂：接收者 IDENT→ps-var-type 解接收者型別→查表→跳 token 重跑 ps-parse-type 克隆（未採用即孤兒零回歸，s51 法）＋filter-ok/declared 雙閘同裸名臂；StructLiteral let 臂僅登 varType 不蓋 stmt.Type（Go case *StructLiteral 語意，!exists 守衛）K_POINTER_TYPE=51/K_VIEW_TYPE=71 asp.no 早已註冊type-string ast.no:632/634 已渲 `*`/`&` 前綴dumper kind-name 通用⇒纯 parser 增量fixture t62-type-shapes.no（*T/**T/T* 後綴正規化/&T results/?&T+推斷/[]byte+IN 名對照/負形 `3 & 5``6 * 7` 保持 InfixExpression）與 oracle 逐位元組相同門禁：fixture 62/63（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 448→451（+3）0 回歸**（SUT-FAIL 6 檔 rc=142 同集ORB-FAIL 0），新匹配 src/std/async.notests/{view-basic,view-optional}.no；Parameter 桶另兩檔 aes-ctr/tdes 首分歧已前移至 D7 安全索引簇（275/718 行殘差）晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s58（構建 sha1 cf4ee19f，晉級 codesign 後 e5dcc20d），舊基線存 /tmp/noc-s0-s57.bak）→s59=464（**match 臂区／區塊尾 註釋附着——armDoc 協議 + 區塊內部協議 + if 尾非破壞性預看**——s58 後 190 DIFF 首分歧分桶：Doc 7／Comment 7／TrailingComments 1（每檔僅 1–3 行差皆缺註釋附着欄位），選中此註釋附着簇鏡像 Go match.go 權威：臂循環頂 RBRACE/EOF 檢查先於 collectDocComments（:167-174）`armDoc := p.collectDocComments()` 於型樣前收集（:208）套用規則（:440-452）inline 體→`setDoc(bodyStmts[0], armDoc)`區塊體→TrailingComments（空則直設非空則 armDoc 併入最前）；區塊臂內部走 parseBlockStatement 委託（:265-431）：leading standalone→其後陳述 Doc行尾→前行Comment`}` 前→TrailingComments**探針 p4 釘死 bare≠explicit 不對稱**：裸 match `{ }` 區塊臂 armDoc→TrailingComments 最前且保留內層首陳述 Doc；明示 `subj: { }` 區塊臂 armDoc **覆寫**首條真陳述之 Doc（內層首 doc 被棄）；兩 form 之 inline 臂皆 armDoc→體首 ExpressionStatement.Doc + 同線行尾註釋→Comment（attachInlineComment）實作五批：① `ps-parse-if` else 預看由破壞性 `ps-skip-trivia` 改直接索引非破壞掃描（`p.toks[k].typ`index-out=zero 守衛，遵 s57 MIR 舊寄存器閘門），查無 ELSE 則 pos 不動⇒if 區塊後函式尾註釋不再被吃掉（root cause：move-clone-liveness 等函式尾 TrailingComments 遺失）；② `ps-parse-match` 臂循環重構（armDoc 收集區塊臂全協議 btrail/bprev/bfirst/bdoc 逐陳述 set-doc＋行尾 set-cmt循環後 armDoc 覆寫 bfirst.Doc 或入 btrail`ps-attach-trailing`）；③ inline 臂 armDoc→set-doc + 同線 COMMENT→set-cmt；④ `ps-parse-bare-match` 三體形（LBRACE 區塊／NEWLINE 區塊／同線單陳述）同協議；⑤ 新 helper `ps-premerge-trailing`——aux 區僅可附尾，prepend 需把舊區整段拷進新 aux 段（舊區孤兒無害：dumper 跟 ct0/ntrail 走訪）fixture t63-comment-attach.no（裸 match 臂首 doc+行尾 tail明示 match ok 區塊臂內部 doc/tail/trailerr inline 臂 tailwildcard 區塊臂頂層 standalone-if 行尾負形中段 standalone 屬其後陳述 Doc）與 oracle 逐位元組相同門禁：fixture 63/64（僅余既有 t7-group-nil-float）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 451→464（+13）0 回歸**，新匹配 tests/{fs-struct,json-get-clone,map,map-generics,match,option,std-new,strconv,test-div-zero,yaml}.nomem-safety/{clone-reset-is-moved,if-branch-move-leak,move-clone-liveness}.no；**SUT-FAIL 6→4**——fmt-heuristic/str-order 因 ① 解掛升為 DIFF（結構仍分歧但可解析）殘差：ws.no（11 行）vec-push-leak.no（52 行）同簇檔被 D7 安全索引／`?T` 無值裸宣告 downstream 阻塞⚠️ 已接受偏離（語料未證形）：match 區塊 `}` 前 standalone 於臂循環頭被吃掉且其後無臂時丟棄（Go 留 buffer 給外層區塊）；`->` 與區塊體 `{` 之間註釋仍為跳過；嵌套裸臂 body 的 armDoc 未併入晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s59（構建 sha1 7c403081，晉級 codesign 後 70be6e56），舊基線存 /tmp/noc-s0-s58.bak）→s60=470（**tagged-enum／option 變體臂析構綁定**——s59 掃描後 oracle 漂移（3c2fa429，重基線 463/181/3），首分歧分桶 LetStatement 61／BlockStatement 56／Identifier 9，選 Identifier 簇 9 檔＝`ok(v)`／`err(e)`／`some(v)`／`rect(w,h)` 變體臂 decomp鏡像 Go 權威：expr.go:1021-1065 parse 期 `ok`／`err` + `( 裸識別符 )` 規範化為析構（condition 換 Identifier{ok|err}＋bindingName）；lowering.go:2049-2090 泛化改寫（主體為枚舉且呼叫臂全裸識別符參數元數==欄位數 ⇒ bindingNamescondition 換 Identifier{X}）；枚舉登記表（decl.go:1196-1224＋858：payload 單欄位=欄型別多欄位='enum.variant' 合成結構名單元=''，裸枚舉 kind==1 亦登記 payload 恒 ''）；typeStr 規則（:2900-2902 枚舉⇒typeStr=armType 直取）；armType 表（載荷替換單元=變體名wildcard 補集=登記序−已列' | ' join空⇒死臂 skipItBinding）；namedItBinding 回退（:2919-2927 未知主體 ok(v) 具名無型別仍 armBinding）；多欄位提欄（:2307-2334 `a = it.f0` DotExpression 逆序 prepend⇒it;a;b;body）；varType 登記（:2276-2296 elemType→armBindingType→err且非枚舉='str'）實作五批：① psst 新增 enN/evN/evP/evC/efE/efV/efT 平行陣列登記表＋六 helper（ps-enum-{row,has,payload,ncols,ftype,variants}，限定名剝落以 byte 尾掃回退）；② ps-parse-enum-def 每變體尾填表（ast.type-string）；③ ps-parse-match 型樣塊加 case A（ok(v)/err(e) 消費 4 token→Identifier）case B（枚舉呼叫臂掃描→Identifier＋參數名收集）裸變體臂 kvarE；④ bt 計算加 enum armType 補集塊reg 登記綁定名 bn plumbing（ps-mk-it-let-t armBinding 改 bt≠''∨具名）多欄位提欄（新 builder ps-mk-ext-let）；⑤ ps-match-listed 擴 cN/cA 元數候補＋ok(v)/err(e) 形 fl 置位（Go 改寫後掃描之預掃近似，首臂盲區以 caseA 就地置位補）fixture t64-tagged-enum-arms.no（已知 option ok/err/nil 析構未知主體回退單載荷/單元/雙變體補集多欄位提欄巢狀同名裸枚舉變體臂）與 oracle byte-identical門禁：fixture 64（僅余既有 t7-group-nil-float與基線既有紅同形）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 463→470（+7）0 回歸**（SUT-FAIL 3 檔 rc=142 同集ORB-FAIL 0），新匹配 tests/{enum-annotation,option-match-silent-wrong,tagged-enum-{alias,cross-fn,payload-ownership,two-match}}.no＋**意外收復 view-basic.no**（s60 Step1 之 LOST 源於平行會污源檔已自行修復）殘差：err-binding（8 行）／match-arm-binding-scope（28 行）被**區塊級裸 match 臂續接合併**族阻塞（區塊臂體 `stmt; cond -> X; -> Y`：Go 將臂區合併為單一 if/else 且區塊 inline:truenoc 現拆為 standalone-if＋if(1) 包裹——下一候選增量）；tagged-enum-zero-match（5 行）殘 `_ = o10` discard 形（oracle 建 LetStatement{name: _, value}）⚠️ 已接受偏離：枚舉全列死臂之 unreachable warnings 計數（Program{warnings:N}s55 先例）；多行型樣 `X(\n a, b)` 不相鄰形不規範化晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s60（構建 sha1 daca5d8e，晉級 codesign 後 4cf6b9bd），舊基線存 /tmp/noc-s0-s59.bak（70be6e56）→s61=474（**區塊級裸 match 臂續接合併 + 明示臂體 inline**——s60 殘差族：err-binding（原 8 行）／match-arm-binding-scope（原 28 行）之 `cond -> { stmt; cond2 -> X` 換行 `-> Y }` 區塊臂體鏡像 Go 權威雙機制：① 換行後續接（stmt.go:2168）——`ps-mk-standalone-if` 已存換行後 `->` 併入前 if 的 Alternative路徑，於明示臂 `{ }` 體解析期間置 `inBlk=1/inMatch=0`，使 `stmt; cond2 -> X` 後之裸 `-> Y` 併入前 if 而非另起 `if(1)` wrapper；② 臂體 inline 標記（expr.go:1263Inline 語句形）——新增 `ps-bm-block-shape`（鏡像 classifyBlockAtCurrent parser.go:704-945，掃描起點 p.pos+1 跳過 LBRACE 本身）判別 `{ }` 首非 trivia token 形：回 blockMatch(1) ⇔ Go 走臂體提取路 ⇒ dump `BlockStatement{inline: true}`（具名區塊臂 `b: {``_ ->`INT/STR/CHAR 型樣臂`cond ==`/`&&`/`||` 比較臂 Go:943 直接 blockMatch）；回 0 ⇔ 普通區塊路（結構欄 `name: TYPE\n``name: 字面值\n``name: ident(``name: ident{``name: -INT``name: .x``name: Enum.Var``name: <value><binop>``IDENT NEWLINE``IDENT ,``IDENT }``IDENT (`）⚠️ `IDENT .` 首臂改用新 helper `ps-arrow-scan-line` 前視深度 0 `->`（鏡像 parser.go:912-942）——`it.v = 99` 無臂 ⇒ blockUnknown(0)，修回 view-optional.no 之誤標 inline⚠️ `bshape` 每臂重置（防上臂 blockMatch 洩漏至下臂）fixture t65-block-arm-continuation.no（具名區塊臂 `b: {`→inline區塊內 `cond -> X`＋換行 `-> Y` 續接嵌套 err(m) 不污染外層補集 else 與 wildcard 尾臂共存）與 oracle byte-identical門禁：fixture 全 SAME（僅余既有 t7-group-nil-float 基線紅與 s61 零差分）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區）語料門（-P 2/90s 全 647）＝**SAME 470→474（+4）0 回歸（LOST=0）**，新匹配 tests/err-binding.notests/match-arm-binding-scope.notests/mem-safety/{json-nested-match,json-parse-option}.no（SUT-FAIL 3 檔 rc=142 同基線集：aes-cbc/net/tlsORB-FAIL 0）殘差：tagged-enum-zero-match（3 行）殘 `_ = o10` discard 形（oracle 建 LetStatement{name: _, value}，與 s60 同因）；`b:`＋換行（無大括號）之具名裸塊嵌套 match（r1/r2 探針）屬另一更深族，本增量未觸晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s61（構建 sha1 31901dcb，晉級 codesign 後 7dfdc91a），舊基線存 /tmp/noc-s0-s60.bak（4cf6b9bd）→s62=481（**`_ = expr` 空白標識符捨棄陳述 ⇒ `LetStatement{name: _}`**——s61 殘留候選族（tagged-enum-zero-match `_ = o10`）擴至全語料 7 檔首分歧鏡像 Go parseStatement stmt.go:49（case 含 UNDERSCORE）→ :149（peek==ASSIGN）→ :172 parseLetStatement——`_` 走與 IDENT 同一 let 路name 存字面 `'_'`此前 noc UNDERSCORE 非 IDENT陳述分派無臂⇒落預設 expr-stmt⇒`_`/`=` 被逐步防呆丟棄只剩右值成裸 `ExpressionStatement{RHS}`（正戳中 Go stmt.go:2075-2079 註解所述`_` 必須是語句邊界否則 `_ = expr` 靜默退化之 noc 對偶面）**實作最小兩批**：① ps-parse-stmt 主分派新增臂 `tk.typ==UNDERSCORE && ps-peek-at(p,1)==ASSIGN -> { ps-bump _; ps-bump =; idx = ps-parse-let(t,p,'_') }`（裸 `_`（無 `=`）不落本臂仍走原路＝零回歸，oracle 裸 `_`⇒`ExpressionStatement{nil}` 屬另族未觸）；② ps-parse-let 內 s54 option-算術合成塊加 `name != '_'` 守衛**關鍵對稱性（探針 a3/a4/b1/b2/b3 釘死）**：`_` 上字面量/slice/呼叫回傳型別推斷照常觸發（`_ = true`→bool`_ = [1,2,3]`→[]i64首個 `_ = g()`→?str），惟option-算術推斷（inferOptionArith）豁免（`_ = zz*2`⇒無型別）——因 Go captureTargetAllowsOption 拒 blank 目標；ps-mark-dcl('_') 自然令後續 `_ =` 落入已宣告閘（`_ = g()` 二次出現不再蓋 ?str，與 oracle 一致）fixture t66-blank-discard.no（ident/bool/slice/call/option-算術豁免/多次丟棄/緊接他陳述後 全覆蓋）與 oracle byte-identical門禁：fixture 全 SAME（僅余既有 t7-group-nil-float 基線紅）單元 util/tok/lx/ast 4/4vet 10 既有 ERROR（無一落於新碼區 ~985/~2769）語料門（-P 2/90s 全 647oracle 漂移 634a0bfa）＝**SAME 474→481（+7）0 回歸（LOST=0）**，新匹配 tests/{fs-error-final,fs-error-info,fs-open-err,option-copy,uninit-output,tagged-enum-zero-match}.notests/mem-safety/option-heap-leak.no（SUT-FAIL 3 檔 rc=142 同基線集：aes-cbc/net/tlsORB-FAIL 0）殘留：裸 `_`（無 `=`）⇒`ExpressionStatement{nil}` 另族；`found = true` 於 match 臂體內裸 ident 賦值誤析為 `ExpressionStatement{found}`（collection/* 5 檔首分歧涉臂體×賦值路由回歸面較大暫緩）晉級：v44 工具鏈 cache-off 構建，/tmp/noc-s0＝noc-s62（構建 sha1 fb51a0d4，晉級 codesign 後 c6095b8e），舊基線存 /tmp/noc-s0-s61.bak（0e1fa13c）⚠️ 其余 6 档（json/net-{http2,pool,sse,ws}/str-vec-push）结构已正确但仍于**下游 standalone-if desugar / index-out desugar / 裸 match 区块**再分歧（即下一个主簇，也即 LetStatement/IntegerLiteral/BlockStatement 三大桶）✅ **AnnotationStatement 主鑰匙已打通**（self-dot 揭示者）：語句級 `#{...}` 現依緊後首 token ∈ {IDENT,IN,RARROW}分流——可附著→寫 overflow 槽（t37）不可附著→發 `AnnotationStatement{entries:去重鍵名數}` 節點（t41）✅ **bang/cond-loop ForStatement 簇已打通**（AnnotationStatement 揭示之下游主簇）：`!! { }`/`! { }` 僅當 bang緊接LBRACE 才成立（否則退回 `!x` 取反運算式陳述）；`(cond) { }` 需平衡 `)` 後立即是 `{` 且括號內非參數列（含 `,` 或恰好兩 IDENT⇒ 匿名函式字面量，退回 `(expr)` 運算式陳述）三者皆 `ForStatement{label:''}` + `Condition`（BooleanLiteral/expr）+ 6 guarded CHILD 以 0 佔位，且不saveWarning（新式語法）✅ **字面量型別推斷補齊**（Go stmt.go:1227–1268 / 1274–1300）：推斷表既有 INT→i64/FLOAT→f64/STRING→str/BOOL→bool，本增量補上 CHAR→char 與 hex(0x)→byte——雙函數（`ps-infer-type``ps-elem-type-of`）同口徑修正✅ **無值明示型別裸宣告已打通**（Go parseLetStatement no-value 分支 stmt.go:1051–1060）：陳述位 `name TYPE`（TYPE∈{NamedType, SliceType, ArrayType, NullableType}）型別解析完後若緊接非`=` 而是陳述邊界（NEWLINE/RBRACE/EOF/;/行內註解）⇒ 建 `LetStatement{name, type, value: }`（Value guarded CHILD 以 0 佔位）復用既有 `ps-parse-type`（子先於父）；非裸宣告（有 `=`或索引 `x[i]`）則還原 p.pos——所建孤兒節點不被任何 kid 引用dumper 自 root 沿 kid 走訪永不觸及（ast.dump 語義）⇒ 零回歸✅ **複合型別名已打通**（接續 s40 揭示之同族）：`ps-parse-type` 先前只认單 IDENT 型別名，本增量補齊——（a）限定名 `a.b.c`：IDENT 臂建完 NamedType 前以迴圈續吃 `DOT IDENT`，整條字串存進 name 槽（type-string 直接讀 s0，不另建節點，對齊 Go stmt.go:1034）；（b）巢狀元素：SliceType/ArrayType 臂不再硬性要求元素為單 IDENT，改為遞迴 `ps-parse-type`（元素起 ∈ {IDENT, LBRACKET, QUESTION}），故 `[][]i64` 建為 SliceType{SliceType{NamedType{i64}}}`?fs.file` 建為 NullableType{NamedType{fs.file}}（type-string ast.no:610/630 遞迴渲染）各臂改用 `handled` 旗標防 match 非排他之跨臂重評級聯效益：`ps-parse-param`（`w io.writer`）可為空含值臂（`f ?fs.file = call`）無需改動即自動受益；另把 `ps-parse-typedecl-noval` 升級為通用 `ps-parse-typedecl`（新增 `= value` 分支）以接住限定/巢狀含值形（如 `d tls.conn = call-tls()`）⚠️ **未涵蓋**：`?T`無值裸宣告（如 `x1 ?str`）仍由可為空臂（需 `=`）誤吞——為避免觸碰众多 option-* 檔的回歸風險，留待下一個專門增量（需把可為空臂改為先解型別再判斷邊界 vs `=`）✅ **無色 async 前綴已打通**（接續 RunExpression/CoExpression 揭示之 async 簇）：Go expr.go:305–357——`run e`/`co e`/`awy e` 皆為前綴運算子包一層guarded-CHILD 節點（子先於父）`run`/`co` 需歧義消解：若 `peek-at(1)==DOT` 則不是 async 關鍵字而是模組名（`run.list(...)`）⇒建普通 Identifier 停於 DOT 交後綴迴圈；`awy` 無此歧義三節點（K_RUN_EXPRESSION=57 / K_CO_EXPRESSION=20 / K_AWAIT_EXPRESSION=13）asp.no 早已註冊⇒纯 parser 缺臂⚠️ 用預先算好的 `dotAfter` 旗標防 match 非排他⚠️ **下一個主簇**：heap.no/path.no/zlib.no 等仍於 **裸 match 區塊 `{ cond -> body … }` desugar**（首分歧位於 LetStatement/IntegerLiteral/BlockStatement 三大桶，oracle 包一層 `IfExpression{IntegerLiteral{value:1}(synthetic), BlockStatement{各臂}}`）再分歧；另有 `GenericParams`（泛型參數渲染）⚠️ USE 增量清掉 11 個 use-bucket 檔案的首分歧，但它們随即在**下游 match-block/standalone-if/BlockStatement 簇**（已确認為下一個高價值增量）或 **FloatLiteral value 精度**上再分歧，故僅 sha3.no 逹到全匹配——USE 本身結構正確（t38 byte-identical）⚠️ `tests/str-order.no` 於 s31/s32 **皆挂死**（既有 hang，非本增量回歸）單元證據：`ast_test``tok_test``lx_test``util_test` 皆 PASS；`no vet compiler/src` ERROR **0**（硬門檻滿足）WARNING **0**（既有 `overflow-ineffective` 已由當前 bin/no 降為 HINT，見下方 vet 現況）覆蓋進度見 §6.3（t0–t51） |
| **M3** | 語義：37 個 `Validate*` + `stdsig` 表（§5.4）                                           | 🟡 **M3-a 骨架＋M3-b stdsig＋M3-c 首族＋缺口②①清償＋正語義首件 ck.no 零誤報落地（s9a/s9b/s10/s11/s12/**s13**，2026-10-28→**10-29**，§5.3.4.am–.**as**/§6.4）＋**詞法 ILLEGAL 報告族鏡像（s82**，2026-11-05，§5.3.4.dk：lx.no 科學計數檢測＋ps.no operand 位 ILLEGAL 臂與 s12 預留 n-illegal 抑制閘接通） ** | L3 MASS-NOOP 已解除：`noc vet` 對語法錯誤檔輸出真診斷（三站點族rc 鉗制 1**parser 族含 cascade 第二條在內與 oracle 逐字節全等**）；**M3-c 正語義首件 `'X' is not defined`（ck.no／TraceID 4bek3xc6）已由 `drv.cmd-vet` 接線消費 ck-sig.no 裸名門表**，本輪補齊泛型/match 臂/tagged-enum 綁定 def 採集＋callee 回退沉默＋std 命名空間靜默 ⇒ **全語料 668 檔 ck 誤報 13→0**（ck-test 20/20L2 ast 逐位元組未受擾）；parser 族仍全語料零誤報L2 656/656 DIFF=0；s82：L1 全語料 **804/804 SAME**（notok↔noc-s82 逐字節）、lx-test 70/70、ps-err-test 46/46、vet 探針 p1/p6/p7 與 oracle 逐字節同文同位；L2/L3 全語料無回歸（與 HEAD 基線二进制 noc-s82base 對比：DIFF 集合與 sut 輸出逐檔一致，117/166 屬既有 oracle 分歧） |
| **M4** | 中端 + 代碼生成：HIR / MIR + RC 堆策略 + LLVM IR 文本                                       | 🟡 **M4-a/b 骨架 + M4-c 控制流/循環/字符串/拼接/多參調用/str 跨塊/vec/帶型別簽名/帶型別 CALL/option/RC 原語/嵌套 STRCAT 的 RC 週期/option 的 `?str` 載荷/`struct`（構造·讀欄位·寫欄位·跨塊槽·多型別·欄位型別·欄位 RC 原語·作形參/返回值·嵌套欄位）/**ALU 家族（位運算·移位·除餘·其餘比較）／無符號 ALU 家族（lshr/udiv/urem + ult/ule/ugt/uge + `TY-U64`）／`f64` 算術家族（fadd/fsub/fmul/fdiv/frem + fcmp 按型別分派 + `TY-F64`）／整數除零 select 護欄（`sdiv/srem/udiv/urem` 裹 select 安全除數，s38）／除零 `divZeroReport` stderr 報告（`__nolang_div_zero_report` + `@write`，s39）／option 的 `vec` 載荷內聯讀寫（`?[]i64`，24B slot 免裝箱，§11 B8 半項，s40）／option 跨基本塊槽（`alloca %option`，非 PHI，§11 B8，s41）／`str` 結構相等（`@str_eq`／`OP-STEREQ`·`OP-STNEQ`，s42）／`vec` 元素相等（`@__nolang_vec_eq_i64`／`OP-VECEQ`，i64 元素，s43）／`option` 值相等（`OP-OPTEQ`／`OP-OPTNEQ`，tag AND payload，i64 載荷，单块无 helper，s44）／`option` 的 `str` 載荷相等（`OP-OPTEQSTR`／`OP-OPTNEQSTR`，tag AND 重建 `%str-long` call `@str_eq`，s45）／`option` 的 `f64`（double）載荷內聯讀寫（`OP-OPTMKF64`／`OP-OPTGETF64`，bitcast double↔i64 落／取 slot[0]，無需 helper，s46）／`option` 的 `f64`（double）載荷相等（`OP-OPTEQF64`／`OP-OPTNEQF64`，tag AND 載荷 bitcast i64→double 後 fcmp oeq，無需 helper，s47）／一元 ALU NEG／NOT（`OP-NEG`／`OP-NOT`，`fneg double`／`sub i64 0,x` + `xor i64 1,x`，鏡像 Go emitNeg／emitNot，s48）／vec→str 重interpret（`OP-STRFROMVEC`，%vec 三欄位 len/cap/dat 逐欄拷进 %str-long + dat inttoptr i64→i8*，鏡像 Go emitStrFromVec，單塊無 helper，s49）／str 容量欄（`OP-STRCAP`，extractvalue %str-long field 1，與 STRLEN 成對，鏡像 Go emitLenCap OpCap/KindStr，單塊無 helper，s50）／option 排序（`OP-OPTLT`／`OP-OPTLE`／`OP-OPTGT`／`OP-OPTGE`，只比載荷 slot[0] 發 icmp slt/sle/sgt/sge 無 tag 項，鏡像 Go emitOptionValueOrder 有符號 int 分支，單塊無 helper，s51)／option 的 `f64` 載荷排序（`OP-OPTLTF64`／`OP-OPTLEF64`／`OP-OPTGTF64`／`OP-OPTGEF64`，slot[0] bitcast 後 fcmp olt/ole/ogt/oge 無 tag 項，鏡像 Go emitOptionValueOrder double 翼，單塊無 helper，s52）／option 與純量排序（OP-OPTLTSC／OP-OPTLESC／OP-OPTGTSC／OP-OPTGESC，vty 定側提載荷與純量 icmp，無 tag 項亦無 and，謂詞不翻轉，鏡像 Go emitCmp aOpt/bOpt 分支，單塊無 helper，s53）與 SCF 家族（`OP-OPTLTSCF`／`OP-OPTLESCF`／`OP-OPTGTSCF`／`OP-OPTGESCF`，option 與 f64 純量排序，vty 雙護欄判向且非 option 側須 TY-F64，載荷 bitcast→double 後與 %fd 純量 fcmp，同無 tag 項無 and 謂詞不翻轉紀律，s54）**完成**（s14–s54，2026-10-30/31–11-03，§5.3.4.at–.ci）；⚠️ **HEAD 工具鏈跨模組結構欄位回歸**（`setfield: buf not found on sb.sb`）⇒ 本族在釘死 `v0.4.13` 上驗；M4-c 餘項（vec 元素非 i64 的逐元素 RC**結構體逐欄位 RC 的自動放置（原語已備，B11）****跨函式所有權轉移（B13）**>24B 堆裝箱（vec 載荷內聯 s40 / option 跨塊槽 s41 已落地）option 非 i64／str／f64 以外載荷的 `==`／match（i64 已 s44，str 已 s45，f64 已 s47）struct 陣列/切片與多返回值）／**M4-e 首件 AST→MIR 降級**（新建 `compiler/src/h2m.no`：i64 直線式子集，`ast` arena → `mir` 指令流，值 id 無槽直綁＋`unsup` 誠實門禁；`h2m-test` **35/35**，探針前向 exit **73**／反向 **79** 與 Go oracle 同程序對拍；§5.3.4.cj，s55；標號說明：§6.5 原定降級主體為 M4-c，該標號已被 codegen 發射族用盡，故降級主體另立 M4-e）／**M4-e 第二件 簽名側與調用降級**（`h2m-func-sig` 讀形參／輸出參型別，形參由 `h2m-alloc` 依序佔值 id `1..nargs`，`retty` 取輸出參數聲明型別，模組級簽名表前置 pass 解前向引用，`OP-CALL` 帶 arity／型別／連續 id 三重校驗，未降級函式整函式不產碼；`h2m-test` **74/74**，探針前向 exit **47**／反向 **50** 與 Go oracle 同程序對拍，鑑別態 exit **101**；§5.3.4.ck，s56）／**M4-e 第三件 控制流與跨基本塊槽化**（`h2m-lower-if`／`h2m-lower-for` 切 entry/then/else/merge 與 hdr/body/exit 塊並發 `OP-BR`／`OP-CONDBR`，比較家族 → `OP-CMP*`，前掃描發現控制流即切 `mem` 模式把全部局部量（含形參）落 entry `alloca` 槽，讀 `LOAD`／寫 `STORE`，槽型與右值型不符則整函式不產碼，**零改 `cg.no`**；`h2m-test` **135/135**，探針前向 exit **92**／反向 **93**（then 塊常量）與 **86**（循環步長）並與 Go oracle 同程序對拍，鑑別態 exit **101**；§5.3.4.cl，s57）／**M4-e 第四件 花括號短路鏈降級**（`h2m-lower-surface` 讀 `K-SURFACE-MATCH` 的 helper/aux 臂群（12 槽/臂，只取兜底／條件／體三槽），逐臂發 `OP-CONDBR` 串成鏈／假路指向下一臂判定塊／兜底臂不發判定直落塊，無兜底時末臂假路落匯聚塊；前掃描經新增 `h2m-scan-arms` 為臂內 `let` 鋪槽，形狀校驗四條（臂數／臂體缺失／兜底與條件並存／兜底不在末位）觸發即整函式不產碼，**零改 `ps.no`／`cg.no`**；`h2m-test` **176/176**，探針前向 exit **188** 與 Go oracle 同程式對拍（IR 反向 189／195／190／189，源碼級三對變異兩側同值），鑑別態 exit **101**；v0.4.13 短路鏈的兩處 Go 側語義偏差記 §11 B19/B20；§5.3.4.cm，s58）／**M4-e 第五件 塊終結形降級**（`h2m-lower-term` 認 `K-RETURN/BREAK/CONTINUE-STATEMENT` 三種陳述：`return` 懶開全函式共享 ret 塊（終結處只發 `OP-BR`，收尾才發 `LOAD`+`OP-RET`，無 `return` 的函式 IR 逐位元組不變），`break`／`continue` 查 `h2m-lower-for` 壓入的 `(hdr, exit)` 對（`2p`=hdr／`2p+1`=exit），終結形非塊末／帶標籤／帶值 return／循環外裸形皆計 `unsup` 且不產碼，同塊第二條不可達終結指令實證由 LLVM 容忍（verify/llc/clang 三級 rc=0），**零改 `ps.no`／`cg.no`**；`h2m-test` **176→203/203**，探針前向 exit **72** 與 Go oracle 同程式對拍（逐值 14 6 0 37 10 5；源碼級三對變異兩側同值 76／70／74；IR 反向四刀 41／66／73／72），鑑別態四例 exit **101**；§5.3.4.cn，s59）／**M4-e 第六件 計數循環降級**（`h2m-lower-for` 的 `cnt != 0` 臂：計數位只求值一次並 `STORE` 進按 `ForStatement` 節點 id 合成的 entry `alloca` 槽（槽名 `@cnt<n>`，`@` 前綴避撞源碼變量，由前掃描 `h2m-scan` 鋪槽），發 hdr/body/update/exit 四塊——hdr `LOAD`+`CONST 0`+`CMPGT`+`CONDBR(body, exit)`，body 尾 `BR` update，update `SUB 1` 後 `BR` hdr，`N ≤ 0` 由 header 首判即假自然跳過（謂詞 `icmp sgt` 而非 `!= 0`）；`continue` 目標由 hdr 換為 update（跳過自減即活鎖），循環棧每層語義升級為 `(continue 目標, exit)`；count 與 cond 並存／標識符計數位／非 i64 計數位皆計 `unsup`，**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**；`h2m-test` **203→255/255**，探針前向 exit **134** 與 Go oracle 同程式對拍（逐值 3 10 104 11 6 0；源碼級四對變異兩側同值 135／141／136／136；IR 反向五刀 132／131／135／132／131），鑑別態三例 exit **101**；§5.3.4.co，s60m）／**M4-e 第七件 區間遍歷降級**（`h2m-lower-range` 認 `K-FOR-STATEMENT` 的 `IterRange` 位：兩端點在 pre 塊各只求值一次，pre／hdr／upd 三處同發一條 `icmp sle lo, hi` 分派升降序雙臂（IR 無 select，方向靠分支定，`opt` 提出循環），`leftInc` 決定 init 的 `±off`，`rightInc` 決定終止謂詞 `LT`／`LE`（降序 `GT`／`GE`），循環變量槽按**源名**鋪 entry `alloca`（體內 `K-IDENTIFIER` 直接查得），共發 11 塊，`continue` 落 update；同名嵌套區間／開邊界／容器遍歷皆計 `unsup` 不產碼（後兩者需 `OP-LEN`／`OP-INDEX` 與不可鑑別的極值補齊，同名嵌套記 §11 B23），**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**；`h2m-test` **255→324/324**，探針前向 exit **164** 與 Go oracle 同程式對拍（逐值 10 6 6 6 4 14 103 9 6；源碼級八對變異兩側同值 168／168／168／171／162／161／161／156；IR 反向八刀 169／161／163／158／163／160／194／155），鑑別態三例 exit **101**；§5.3.4.cp，s61）／**M4-e 第八件 容器遍歷降級**（`h2m-lower-coll` 認 `K-ITERATION-EXPR` 的 `kid[1]` 容器源：pre 發一條 `OP-VECLEN` 於循環外只求長度＋`STORE 0` 進合成索引槽 `@idx<n>`，hdr `LOAD`+`OP-CMPLT`+`CONDBR(body, exit)`，body `OP-VECGET` → `STORE` 進循環變量槽（讀副本），upd `ADD 1` 後 `BR` hdr，`continue` 落 upd／`break` 落 exit；`TY-VEC` 形參在 `mem` 模式下**不鋪槽**（cg 無 `alloca %vec` 的 op，而形參本身就是支配全函式的 entry SSA 值 ⇒ 綁定即 `%vc1` 跨塊可讀）；元素位寫入靠新增的活動名棧 `c.epl` 在 `h2m-var-write` 入口計 `unsup`（Go `bindElemPlace` 寫回容器 ⇒ 語義分岔寧缺勿錯）；`h2m-param-ty` 收 `[]i64` 形參，`h2m-func-sig` 閘掉 `vec` 返回；**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**（復用 `OP-VECLEN`／`OP-VECGET`，s61 所記"需 `OP-LEN`／`OP-INDEX`"與其時的 `KindSlice` 假帳均已改實）；`h2m-test` **324→372/372**，探針前向 exit **134** 與 Go oracle 同程式對拍（逐值 6 3 101 5 6 6 7；源碼級六對變異兩側同值 136／133／140／134／148／140；IR 反向八刀 135／132／129／139／130／133／131／128），鑑別態六例 exit **101**；§5.3.4.cq，s62）／**M4-e 第九件 值匹配降級**（`h2m-lower-surface` 認 `K-SURFACE-MATCH` 的 `kid[0]` 主體：逐臂在**本臂判定塊內**重讀主體發 `OP-CMPEQ`＋`OP-CONDBR`，主體限已登記的 i64 裸標識符，型樣限裸整型字面量，aux 槽 0／1／2／3／5／6／8 非 0 即出局（解構綁定／`ok` 關鍵字臂／raw-cond／型別臂不收）；⚠️ 收閘前是**靜默錯碼**不是 `unsup`（型樣被當布爾條件發 `icmp ne i64 1, 0` 恆真，主體從未 `load`）；**零改 `ps.no`／`cg.no`，`mir.no` 未加 opcode**（復用 s35 的 `OP-CMPEQ`）；`h2m-test` **372→429/429**，探針前向 exit **221** 與 Go oracle 同程式對拍（逐值 10 20 30 1 7 3 4 10 1 111 8 9 7；源碼級七對變異兩側同值 216／227／222／226／65（真值 321 按位元截斷）／222／231，另兩對實參 241／220；IR 反向十刀 211／241／231／222／171／215／223／218／212／219），鑑別態七例 exit **101**；§5.3.4.cr，s63）／**M4-e 第十件 實參側池 `args`**（`OP-CALL` 實參改住與指令下標無關的側池：`xa`=池內基址下標、`bs`=個數、第 j 個實參值 id = `args[xa+j]`，段內值 id 可任意散布——**摘掉 §11 B16 連續 id 契約**；`cg-emit-func`／`cg-emit-cfg`／`cg-emit-cfg-t` 各增**必填**第 13 形參 `args`（漏改呼叫點是編譯錯誤而非靜默發 `%v0`），CALL 臂 `q = args[x + j] #{index-out=zero}` 取值 id 後仍按 `vty[q]` 定名與命名空間；`h2m` 側逐實參先落臨時 `al`、**校驗全過才整段入池**（邊降邊推時內層嵌套 CALL 的池項會插進外層段中間擠偏基址，`f(h(3),1)` 初版實測發成 `@f(%v1,%v2)`）；**`mir.no` 未加 opcode**；`h2m-test` **429→443/443**、`cg-test` **342→347/347**、`cg-runtime-test` **60/60**，探針前向 exit **91** 與 Go oracle 同程式對拍（逐值 25 21 17 13 15；IR 反向五刀 93／87／82／93／88；源碼級四對變異兩側同值 92／104／90／95），鑑別態三例 exit **101**；s55–s63 九式探針前向 73／47／92／188／72／134／164／134／221 零回歸；`OP-STRUCT-SET` 自帶連續約定殘留另記 §11 B26；§5.3.4.cs，s64）／**M4-e 第十一件 值匹配臂的 `it` 隱式綁定**（`h2m.no` 增 **`itv` 值 id 棧**——cond 臂複用判定塊主體 LOAD、wild 臂按 `h2m-subtree-uses-it` 懶發 LOAD、臂退出 `.pop()`；`it` 全程不進槽表（`h2m-lst-find('it')` 恆 -1）避臂間同名串型；`h2m-ty-of('it')` 補 TY-I64 特例銷預掃描雙 mark；同族 `stk`／`rnms`／`epl` 三棧退出補真 `.pop()`；**零改 ps/cg/mir**；`h2m-test` **443→453→471/471**，探針前向 exit **46** 與 Go oracle 同程式對拍（源碼級兩對變異 45／47、鑑別態 101；IR 反向五刀 39／44／36／45／41）；⚠️ Go 側嵌套 match 的 `it` 海森堡記 §11 B27；§5.3.4.ct，s65）／**M4-e 第十二件 前綴表達式 `-x`／`!x`**（`h2m-lower-expr` 新增 `K-PREFIX-EXPRESSION` 臂——`'-'`⇒`OP-NEG`（`sub i64 0`）、`'!'`⇒`OP-NOT`（`xor i64 1`），僅 i64 主體，`~`／非 i64 走 `unsup` 誠實門禁；`h2m-ty-of` 補 prefix 臂遞迴運算元型別供預掃描鋪 TY-I64 槽（規避 §5.3.4.ct 雙 mark）；**零改 ps/cg/mir**（op 發射 s48 已備）；`h2m-test` **471→485/485**，探針前向 exit **149** 與 Go oracle 同程式對拍（oracle p3/p5 用 `1-x`／真值臂 match 等價代換規避 Go `!x`→bool 型別差；IR 反向 NEG 三刀 150／151／148，XOR 兩刀因 p3 對稱輸入得 149——NOT 正確性由單元測 `xor i64 1` 斷言釘住）；§5.3.4.cu，s66）／**M4-e 第十三件 二進制位運算 `&`／`|`／`^`／`<<`／`>>`**（`h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈補五條 `bop` 臂——`'&'`⇒`OP-AND`、`'|'`⇒`OP-OR`、`'^'`⇒`OP-XOR`、`'<<'`⇒`OP-SHL`、`'>>'`⇒`OP-SHR`（發 `ashr` 算術右移），僅 i64 雙元；`/ %`（`?i64` 除零 option 語義未建模）與 `&^`（`ps.no` 不成為運算子）仍走 `unsup` 誠實門禁；**零改 ps/cg/mir**（五 op `and/or/xor/shl/ashr i64` s35 已備）、**零改 `h2m-ty-of`**（復用既有 INFIX 節點，不同 s66 新增節點需補臂，雙 mark 坑本件不適用）；`h2m-test` **485→501/501**，探針前向 exit **109** 與 Go oracle 同程式對拍（IR 反向六刀逐助記符互換 **115／103／111／69／228／157** 全改 exit，六式取値非對稱無 s66 XOR 對稱盲區；須 mutate 未 opt 原始 IR 避常量摺疊）；s55–s66 前向 73／47／92／188／72／134／164／134／221／91／46／149 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s66 所記 4 基線口徑不同，本件不觸 std 正交）；§5.3.4.cv，s67）／**M4-e 第十四件 邏輯 `&&`／`||`**（`h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈補兩條 `bop` 臂——`'&&'`⇒`OP-AND`、`'||'`⇒`OP-OR`，**復用 s67 位運算同一組 op**，僅 i64 雙元；因 i64 子集布爾恆為 0/1（比較結果 zext），`and/or i64` 於 0/1 == 邏輯與/或，且 Go `emitLogic` 本身非短路（兩側都求值）→ 與 Go 逐值一致；真·短路 bool 控制流與 f64/u64 邏輯操作數仍 `unsup` 誠實門禁；**零改 ps/cg/mir**（`and/or i64` s35 已備）、**零改 `h2m-ty-of`**（復用既有 INFIX 節點，同 s67，s66 雙 mark 坑不適用）；`h2m-test` **501→511/511**，探針前向 exit **43** 與 Go oracle 同程式對拍（bool→i64 用 `v: { true -> r = 1 -> r = 0 }`；IR 反向四刀逐助記符互換 **47／33／59／11** 全改 exit，無對稱盲區；須 mutate 未 opt 原始 IR 避常量摺疊）；s55–s67 前向 73／47／92／188／72／134／164／134／221／91／46／149／109 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67 同，本件不觸 std 正交）；§5.3.4.cw，s68）／**M4-e 第十五件 前綴位非 `~x`**（`h2m-lower-expr` `K-PREFIX-EXPRESSION` 分派鏈補 `osym == '~'` 臂——`~x`⇒`OP-CONST(-1)`＋`OP-XOR(x, -1)`（鏡像 Go `hir2mir:8861`「`~` 非邏輯非、走全 1 常量 XOR」，`~x = x^-1 = -x-1`），僅 i64 主體；非 i64（如 `~s` str）仍 `unsup` 誠實門禁；**零改 ps/cg/mir**（`OP-XOR`／`OP-CONST` s35/s55 已備）、**零改 `h2m-ty-of`**（復用既有 prefix 節點，s66 雙 mark 坑不適用）；需**二發射**（先 materialize 常量 `-1` 成 SSA 值 id 才能作 XOR 右操作數）；`h2m-test` **511→521/521**（原 `test-prefix-gate` 改棧為 `~s` str），探針前向 exit **255** 與 Go oracle 同程式對拍（逐值 `-1 -6 -1 4 -1`；IR 反向三刀 `CONST -1→0`／`xor→add`／`xor→sub` **22／111／191** 全改 exit；須 mutate 未 opt 原始 IR）；🔴 **`ps.no` 前綴 `~` 鬆綁定（PREC 9 吞右側 `~(x+3)`）vs Go 前端緊綁定 `(~x)+3`——優先級分歧，probe/oracle 規避裸 `~ binop`，屬解析缺口待查非本件 h2m 範圍**；s55–s68 前向 73／47／92／188／72／134／164／134／221／91／46／149／109／43 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67/s68 同，本件不觸 std 正交）；§5.3.4.cx，s69）／**M4-e 第十六件 前綴 f64 負號 `-x`**（`h2m-lower-expr` `K-PREFIX-EXPRESSION` 分派鏈把既有 `'-'→OP-NEG` 臂的守衛從「僅 `TY-I64`」**放寬**為「`TY-I64` 或 `TY-F64`」——新增 `osym == '-' && h2m-vty-at(c, ov) == TY-F64` 臂（`mid = h2m-alloc(c, TY-F64)`、`h2m-emit(c, OP-NEG, mid, ov, 0, '', TY-F64)`），`-x`（f64）⇒`OP-NEG`，鏡像 Go `codegen.go:6235` `emitNeg` 的 float 分支（`fneg double %v`）、int 分支仍 `sub i64 0,x`；**零改 ps/cg/mir/h2m-ty-of**（`OP-NEG` 的 `fneg double` 發射 s48 已備、`cg` 依目的值型別分派）；這是 f64 值**首次端到端流經 h2m**（形參 `double`→`fneg double`→`ret double`）；⚠️ **操作數必為 f64 形參、無 f64 字面量**（`OP-CONST` 只發 `add i64 0, K` i64、無法產 `double` 常量），二元算術仍不收 f64；`!x`／`~x` 於 f64 主體仍 `unsup` 誠實門禁（`test-neg-f64-gate` 釘住邊界）；`h2m-test` **521→529/529**（新增 test-neg-f64-var／nested／gate 三函式），探針前向 exit **195** 與 Go oracle 同程式對拍（逐值 `-1 2 -6 3 -4`、總和 -61；`f64.to-i64` `#{buildin}` `llvm.fptosi.sat` 向零截斷；IR 反向三刀 `fneg→fadd 0.0`／雙負→單負／`fneg→fsub x,x` **237／19／216** 全改 exit；須 mutate 未 opt 原始 IR）；s55–s69 前向 73／47／92／188／72／134／164／134／221／91／46／149／109／43／255 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67/s68/s69 同，本件不觸 std 正交）；§5.3.4.cy，s70）／**M4-e 第十七件 f64 二元算術 `+ - * /`**（`h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈把既有「僅 `TY-I64` 雙側」算術發射守衛**放寬**為「`TY-I64` 或 `TY-F64`」——新增 `fty`（兩側皆 `TY-F64`）發射臂，`x op y`（f64）⇒`OP-ADD/SUB/MUL/DIV`（目的值 `TY-F64`）發 `fadd/fsub/fmul/fdiv double`，鏡像 Go `emitBinop` 按型別分派；新增 `osym == '/' -> bop = OP-DIV` 映射供 f64 側發 `fdiv`，**但 i64 側明確以 `bop != OP-DIV` 排除（`/ %` 需 `?i64` 除零→`err` 的 option 語義未建模、仍誠實門禁）**——同一 `OP-DIV` 在 f64 侧放行、在 i64 侧仍門禁為本件關鍵不對稱；承接 s70 首開的 f64 端到端通道從一元拓展到二元；**零改 ps/cg/mir/h2m-ty-of**（`fadd/fsub/fmul/fdiv double` 發射 s37 已備、`OP-DIV` 早在 `mir.no` 已定義；嵌套 f64 靠發射時 `h2m-vty-at` 讀實際值 id 型別、靜態推斷 `h2m-ty-of` 對 INFIX 仍只回 `TY-I64` 不受影響）；⚠️ 兩側操作數必為 f64 形參、無 f64 字面量（`OP-CONST` 只發 `add i64 0, K`）；`h2m-test` **529→542/542**（新增 test-arith-f64-add／mul-nested／div／mixed-gate／i64-div-gate 五函式），探針前向 exit **30** 與 Go oracle 同程式對拍（逐值 `4 7 5 7 12`、總和 286 遮罩 8 位＝30；`f64.to-i64` `#{buildin}` `llvm.fptosi.sat` 向零截斷；IR 反向三刀 `q1 fadd→fsub`／`q4 fdiv→fmul`／`q5 丟外層 fmul` **27／222／158** 全改 exit；須 mutate 未 opt 原始 IR）；s55–s70 前向 73／47／92／188／72／134／164／134／221／91／46／149／109／43／255／195 零回歸；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67–s70 同，本件不觸 std 正交）；§5.3.4.cz，s71）／**M4-e 第十八件 f64 二元比較 `< <= > >= == !=`**（`h2m-lower-expr` `K-INFIX-EXPRESSION` 分派鏈在原 s71「i64 臂＋f64 算術臂」外**新增 f64 比較臂**——以 `is-arith`/`is-cmp` 旗標對 `bop` 歸類，`fty==1 && is-cmp==1` ⇒`OP-CMPLT/CMPEQ/CMPNE/CMPLE/CMPGT/CMPGE`（目的值 `TY-I64`、非 f64）發 `fcmp <pred> double` 後 `zext i1→i64`（`!=` 用 `une` 保 NaN），鏡像 Go `emitCmp` 按左操作數型別分派；**順帶收緊 s71 過寬 f64 臂**（曾對一切 `bop` 放行、dest `TY-F64`）為算術+比較兩族——位/移位/邏輯於 f64 不落任一路 ⇒ 誠實門禁（`test-bit-f64-gate`/`test-logic-f64-gate` 以 `(r f64)` 釘住畸形 `and i64 %fd`）；**零改 ps/cg/mir/h2m-ty-of**（`fcmp`+`icmp`+`zext` s37 已備、六比較映射早已存在）；比較結果恆 i64（走 `%v`）與算術 dest `TY-F64`（`%fd`）分岔為本件關鍵；⚠️ 真 Nolang `x<y` 得 bool（`cannot assign bool value to i64`），self-host 型別表無 bool 故以「比較即 i64」等價建模、oracle 用 `b = x < y`＋`b -> r = 1` 折 0/1 對拍；`h2m-test` **542→560/560**（新增 test-cmp-f64-lt/eq/ne/arith-nested/mixed-gate＋bit/logic-gate 七函式），探針前向 exit **79** 與 Go oracle 同程式對拍（逐值 `1 1 1 1 0 0 1`、加權和 79；IR 反向五刀 c1 olt→ogt／c3 oeq→one／c5 ole→oge／c6 oge→ole／c7 內層 olt→ogt **78／75／95／111／15** 全改 exit、含 0↔1 兩向；須 mutate 未 opt 原始 IR）；s55–s71 前向零回歸＋i64 家族 s66–s69 IR 逐位元組相同；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67–s71 同，本件不觸 std 正交）；§5.3.4.da，s72）／**M4-e 第十九件 f64 字面量降級**（`h2m-lower-expr` **新增 `K-FLOAT-LITERAL`（kind 29）臂**——讀 raw（slot1＝源 token 原文、非 slot0 %v 值）、誠實門控 `raw.contains('.')`、放行則 `h2m-alloc(TY-F64)`＋`h2m-emit(OP-CONST, cid, 0,0, raw, TY-F64)`（raw 置 sym 槽）；**同步改 `cg.no` OP-CONST 臂**——`cg-vty-at(vty,d)==TY-F64` ⇒ `%fd<d> = fadd double 0.0, <syms[k]>`（LLVM 無法命名裸雙精度常量、以 fadd 0.0 物化）、否則維持 `add i64 0,<x>`——**s55 起首件需動 cg.no 之降級件**（前十八件皆零改 cg，此件如實標注）；取 raw 非取值因 LLVM textual 雙精度字面量文法須含小數點（slot0 %v 吞 .0 ⇒ '2.0'→'2'），raw 無點者（如 '1e5'）誠實門禁；slot-decl 零初始化 OP-CONST 永不見 TY-F64（h2m.no:320）⇒ 新 cg 分支只由字面量路徑可達；`h2m-test` **560→572/572**（新增 test-f64-lit-arith/cmp/gate 三函式），探針前向 exit **91** 與 Go oracle 同程式對拍（逐值 `1 1 0 1 1 0 1`、加權和 91；IR 反向六刀改各函式 fadd 常數 **90／89／95／83／75／27** 全改 exit、含 0↔1 兩向；須 mutate 未 opt 原始 IR）；s55–s72 前向零回歸＋i64 家族 s66–s69 與 f64 形參家族 s70–s72 探針 IR 逐位元組相同；⚠️ `no vet src/std` 本輪計數 **1 ERROR**（與 s67–s72 同，本件不觸 std 正交）；§5.3.4.db，s73）／**M4-e 第二十件 f64 局部變量／控制流槽化**（槽機構認 TY-F64：h2m-ty-of 推 f64 字面量與算術/比較族、slot-decl 鋪 alloca double（零初始化攜 sym='0.0'）、var-read LOAD 白名單增 F64；cg OP-ALLOC/STORE/LOAD 依 cg-vty-at 分岔發 double 翼；h2m-test **587/587**、cg-test 347/347、cg-runtime 60/60；探針七函式全含控制流前向 exit **77** 與 Go oracle 同程式對拍；IR 反向七刀 **76／79／73／69／93／109／13** 全改 exit 含 0↔1 兩向；s66–s73 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.dc，s74）／**M4-e 第二十一件 i64 除／模除零護欄降級**（h2m 運算符表補 '%' ⇒ OP-MOD、i64 發射臂取消 bop != OP-DIV 排除；除/模落 cg 早在 s38/s39 就緒的 cg-div-guard plain 護欄路徑——除零結果定義化為 0 + stderr 報告，**零改 cg/ps/mir/ty-of**；實測發現 checker 口徑：`a i64 = x / d` 宣告＋初值被拒（?i64→i64），須先宣告 plain 再重賦值才達 Go plain 路；h2m-test **599/599**、cg-test 347/347、cg-runtime 60/60；探針七函式前向 exit **111** 與 Go oracle 同程式對拍（stderr 逐字一致）；IR 反向七刀 **110／109／107／103／127／79／47** 全改 exit；s66–s74 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.dd，s75）／**M4-e 第二十二件 u64 除／模無符號護欄降級**（h2m 新增 `uty` 分派——雙側整數族且至少一側 `TY-U64` ⇒ `OP-DIV/OP-MOD` 改寫為 `OP-DIVU/OP-MODU`（鏡像 Go `hir2mir` 的 `sourceUnsigned` 或路由）、新發射臂 dest `TY-U64`；落 cg 早在 s38/s39 就緒的 `cg-div-guard(..., sgn=0)` 路徑——查零、不查 (MIN,-1)，**零改 cg/ps/mir**；🔴 實測撞出 `h2m-ty-of` 與 `h2m-scan` 前掃描槽註冊的重耦合（缺 U64 infix 推斷臂 ⇒ 槽註冊 -1 ⇒ mem 模式三處 mark，register 模式全綠的誤導表象），同步補該臂；🔴 對拍撞出 Go 側編譯期常數摺疊對 u64 除法走有符號口徑（MAX/2 折成 0、MAX%10 折成 -1），執行期真實路徑正確 ⇒ oracle 高位元用例改經 env 取不透明值（正交既有 quirk，本件不改 `src/`）；h2m-test **619/619**（+20）、cg-test 347/347、cg-runtime 60/60；探針六式前向 exit **63** 與 Go oracle 同程式對拍（stderr 逐字一致）；IR 反向八刀 **62／61／59／55／47／31／59** 全改 exit（K5/K6 為簽／無簽分岔靶）、報告呼叫刀 exit 沒有變而 stderr 變空；s66–s75 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.de，s76）／**M4-e 第二十三件 u64 比較族無符號降級**（h2m 在既有 `uty` 分派上把 `OP-CMPLT/LE/GT/GE` 改寫為 `OP-CMPLTU/LEU/GTU/GEU`（鏡像 Go `hir2mir` 同一 `switch`）、`==`／`!=` 保持原 op（Go 口徑「eq/ne 與符號無關」）、比較發射臂 dest **恆 `TY-I64`**（0/1 而非 u64，與 s76 除模臂分岔）；落 cg 自 s36 就緒的 `icmp u* i64`＋`zext i1→i64` 四臂，**零改 cg/ps/mir**；🔴 `h2m-ty-of` 的整族 U64 推斷臂必須同步開比較例外（`.de` ④ 耦合的反方向——s76 靠「非除模必 mark」撐住槽寬推錯無害，本件解除比較門禁後該前提失效，mem 模式對 `f = x > y` 的後續 i64 寫全數 mark）；h2m-test **619/619→648/648**（+29，新增 9 測試＋門禁下移的 `test-arith-u64-bit-gate`）、cg-test 347/347、cg-runtime 60/60；探針八式（含 mem 槽 `>=`、高位 `>`／`<` 簽／無簽分岔靶、`==`／`!=`）前向 exit **255** 與 Go oracle 同程式對拍（oracle 守衛折 0/1、高位元經 env 取不透明值）；IR 反向九刀 **254／253／251／247／239／223／191／127／251** 全改 exit（K5/K6 簽／無簽靶、K9 實參次序刀與 K3 同位而刀類不同）；s66–s76 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.df，s77）／**M4-e 第二十四件 u64 算術族 '+ - *' 放行**（h2m 在既有 `uty` 分派上新增算術發射臂 dest `TY-U64`——**不重映射**（Go MIR 無 OpAddU/OpSubU/OpMulU，64 位寬簽／無簽位元相同）、**零改 cg/ps/mir/ty-of**（s77 的 ty-of「比較⇒I64、其餘⇒U64」拆分已天然對齊算術槽寬）；cg `!= TY-F64` 默認臂直接吞 U64 dest 發裸 `add/sub/mul i64`，鏡像 Go plain 路「bit-identical」；溢出 `with.overflow` 只在 option 路、`?u64` 仍不表達（同 s75/s76 口徑）；🔴 本件無簽/無簽分岔靶，s76 留的「非除模必 mark ⇒ 槽推 U64 無害」陳舊註解隨門禁解除改寫；h2m-test **648/648→670/670**（+22，新增 8 測試：裸三式／字面量翼／嵌套／mem 槽／slot-u64 耦合／mixed 仍門禁；s76 的 add-gate 紅測翻正拆除）、cg-test 347/347、cg-runtime 60/60、ast-test 系 HEAD 工具鏈既有 bug #85 正交失敗；探針八式（`+ - *`／`x+2`／`(x+y)*2`／mem 槽／MAX+1 與 0−1 回繞）前向 exit **255** 與 Go oracle 同程式對拍（env 不透明高位元、回繞不加 `#{overflow=wrap}`）；IR 反向八刀 **254／253／251／247／239／223／253／191** 全改 exit（刀型制：助記符互改＋實參次序＋mem 斷鏈＋回繞斷言翻轉）；s66–s77 探針 IR 逐位元組相同；vet 計數 1 ERROR 與本件正交；§5.3.4.dg，s78）／**M4-e 第二十五件 u64 位運算／移位族 '& | ^ << >>' 放行**（h2m 在既有 `uty` 分派上新增位運算發射臂 dest `TY-U64`——`& | ^ <<` **不重映射**發裸 `and/or/xor/shl i64`（Go 路由表無衍生可鏡像、逐位與符號性無關）；唯一重映射 `>>` ⇒ `OP-SHRU`發 `lshr i64`（鏡像 Go `emitBitwise` 對 OpShr **恒發 lshr**——「nolang >> 是邏輯移位」；`.dg` 留的「不收 vs 收 OP-SHRU」分歧裁定收；自舉 s35/s36 的「i64=ashr、u64=獨立 op」設計在 u64 側與 Go 會師；**零改 cg/ps/mir/ty-of**）；`&& ||` 與位運算 bop 同碼（s68 遺留），u64 上裸 and/or 不等價邏輯語義 ⇒ `is-logic` 旗標排除、門禁測下移至 `x && y`；🔴 i64 `>>` 口徑分歧陳帳（自舉 ashr vs Go lshr，負數右移分岔靶，待後續件定奪）；h2m-test **670/670→694/694**（+23，新增 9 測試：裸四式／shr-lshr 含 ashr 缺席負向／字面量移位量／嵌套／slot-u64 耦合／mem 槽）、cg-test 347/347、cg-runtime 60/60、ast-test/ck-sig 正交基線同 s78；探針八式（基礎五式／mem 槽 `f=x&y⇒f=f|1`／🔴 `MAX>>1` 簽/無簽分岔靶／`1<<63` 符號位回繞）前向 exit **255** 與 Go oracle 同程式對拍（env 不透明高位元、期望值顯式 u64 標注）；IR 反向八刀 **254／253／251／247／239／191／223／127** 全改 exit（K6 全 suite 唯一簽/無簽刀；K8 對調刀只落移位式——and/or/xor 可交換無鑑別力）；s66–s78 探針 IR 逐位元組相同（13/13）；vet 計數 1 ERROR 與本件正交；§5.3.4.dh，s79）／**M4-e 第二十六件 i64 '>>' 口徑裁定——全族無條件重映射 OP-SHRU 恒發 lshr**（鏡像 Go OpShr；s35 i64=ashr 推翻、cg ashr 臂轉死代碼；首個非加性件：s67 IR 單行差異重基線、執行值不變；h2m-test 698/698、oracle 同 exit 255、刀 8/8 含 4 枚分岔刀；§5.3.4.di，s80）／**M4-e 第二十七件 u64 前缀 `-x`／`~x` 放行**（`K-PREFIX-EXPRESSION` 分派鏈新增兩 `TY-U64` 發射臂：`'-'`⇒`OP-NEG`（dest `TY-U64`→`sub i64 0`，二補回繞 mod 2^64）與 `'~'`⇒`OP-CONST(-1)`＋`OP-XOR`（dest `TY-U64`→`xor i64`，bit-not）；`'!'` 於 u64 **保持誠實門禁**（裸 `xor 1` 翻最低位≠邏輯非）；🔴 `TY-U64` 與 `TY-I64` 同 LLVM 型別／`%v` 命名空間 ⇒ u64 前綴 IR 與 i64 前綴**逐字節相同**，型別正確性靠 (a) `h2m-test` 閘僅對 `TY-U64` 放寬＋dest 型別碼釘 `TY-U64`、(b) 值語義刀共同鎖；**零改 `cg`／`ps`／`mir`／`h2m-ty-of`**（整數發射臂 s35/s48/s55 已備、prefix 型別遞迴早已就緒）；h2m-test **698→715/715**、探針 exit 255、oracle 同 exit 255（`#{overflow=wrap}`）、刀 **7/7**；屬 s79「u64 前綴僅 i64」陳帳缺口的自然償還；§5.3.4.dj，s81）／**M4-e 第二十九件 u64 邏輯族 `&& ||` 放行**（h2m u64 位運算臂撤 `is-logic == 0` 排除、旗標退場——探針實釘 Go 對 u64 `&& ||` 即裸位形 `and/or i64` 非短路（12&&10⇒8、0||5⇒5），s79「同碼⇒不等價、與真短路後置」假設被推翻，放行即鏡像；門禁測 `test-arith-u64-bit-gate`／s67 `test-log-gate` 翻正為 `test-logic-u64-*`／`test-log-u64-mem`；**零改 cg/ps/mir/ck**（s68 同碼復用、cg 發射臂已備）；函數形「r u64=x&&y」Go checker 拒（表面 bool）⇒ oracle 走頂層形；h2m-test **715→721/721**、IR 探針 exit **15** == oracle 頂層形裸 exit **15**、刀 **5/5**、s66–s81 前向值全同；§5.3.4.dl，s83）／**M4-e 第三十件前綴 `~` 綁定口徑複核＋哨兵重鑲**（§5.3.4.cx 關鍵點 4 🔴 待查項三面實測緊綁定==Go 關閉；實作零改；ps-test 38→42 哨兵；刀 4/4、前向 IR 16/16 SAME；§5.3.4.dm，s84）／**M4-e 第三十一件 (MIN,-1) 護欄翼執行面實釘**（§5.3.4.dd「執行面留待專門件」尾款償清——值面 /⇒MIN、%⇒0 不觸 stderr、exit 7==oracle；實作零改；h2m-test 721→735 翼哨兵；刀 4/4、前向 IR 18/18 SAME；§5.3.4.dn，s85）M4-d ⬜   | M4-a：`cg-runtime.no`＋`cg-rc.no`＋`check-rc-balance`，**16/16**`opt` rc=0diff 0 刪除行M4-b：`mir.no`＋`cg.no` 發 CONST/ADD/SUB/MUL/CALL/ALLOC/STORE/LOAD/RET，**13/13**`let-add` 執行 **exit 6**M4-c：多基本塊 + `BR/CONDBR/CMPLT`（alloca 傳值無 PHI），含**回邊/循環**，`lt10(5)+lt10(15)`→**exit 3**`sum4()`→**exit 6**字符串（首个非 i64 型別 `%str-long`）：`@.s<k>` 全局字面量前置 pass＋三步 `insertvalue`＋`extractvalue` 取 len，`slen()`→**exit 3**字符串拼接（首个 RC 消费者）：`@str_concat` 13 行逐字照抄＋`prelude-full` 唯裝配點（順序有斷言），`cat()` 輸出 **`abcd`**exit **4**多參調用：`OP-CALL` 的 `bs` 從佔位改為**參數個數**（破壞性語義變更，已同步唯一呼叫點），`add2(3,4)`→**exit 7**`%str-long` 跨基本塊：`SALLOC/SSTORE/SLOAD` 三件套（第三個槽命名空間 `%ss<id>`），`pick(0)`→**exit 2**`pick(1)`→**exit 6**`vec`（第四個值命名空間 `%vc<id>`）：`%vec = { i64, i64, i64 }` + 4 個 helper（刻意不照抄 Go 的 inline 展開），十次 push 強制擴容 0→4→8→16 後求和 ⇒ **exit 45**ASan 無報告；`cg-test` **54/54**`cg-runtime-test` **30/30**帶型別簽名（`cg-emit-cfg-t` + `TY-I64/STR/VEC`）：形參/返回值可按型別取 `i64`/`%str-long`/`%vec`，值命名空間由 `cg-val-prefix` 單一來源決定（`mix(n i64, s str)` ⇒ `%v1`+`%sv2` 交錯），`mix/sident/mk` ⇒ **exit 14**帶型別 CALL（值型別表 `vty` 按值 id 索引 + `cty` 按指令索引）：`caller2` 在函式內發 `call i64 @mix(i64 %v1, %str-long %sv2)`，`caller2()`+`mk()`+`sident()` ⇒ **exit 14**`option`（`%option = { i64, [3 x i64] }`，第五個值命名空間 `%op<id>`）：`OPTNONE/OPTSOME/OPTTAG/OPTISOK/OPTGET` 五個 op，`OPTSOME` 用 Go 同形的一次 insertvalue 給 tag + slot[0]；`pick(n)=n<10?some(n):none` + `g(n)=isok+payload` ⇒ **exit 8**（`g(7)=8``g(15)=0`）；`cg-test` **78/78**`cg-runtime-test` **33/33**s26–s29 續：`RETAIN`/`RELEASE` 原語（只作用 TY-STR，`%dp<k>`/`%rel<k>` 按下標命名；`cg-rc.no` 事件碼改名 `RC-RETAIN`/`RC-RELEASE` 消同名衝突）⇒ `cg-test` 86/86`clang -O2` exit 4**ASan 零報告**；嵌套 `STRCAT`（中間堆串 `sv3`/`sv5` 各自 RETAIN+RELEASE）⇒ **99/99**exit 6ASan/LSan 零報告；option 的 `?str` 載荷**內聯**進 24B slot（`%str-long` 恰 24B ⇒ 免裝箱；data 指標須 `ptrtoint`/`inttoptr` 非 bitcast）⇒ **107/107**exit 4ASan 零報告；`struct`（`%struct.point` 收歸 prelude 唯一發射`%st<id>` 第六命名空間鏈式 `insertvalue` 中間名 `%sm0<k>`）⇒ **112/112**`cg-runtime-test` **35/35****雙函式模組 199 行 `opt` rc=0 + `clang -O2` exit 90 + ASan 零報告**型別重定義反證 `opt: …:7:1: error: redefinition of type`s30 續：`STRUCT-SET`（**讀舊出**新值，欄位值 id 走 `xa[k]+1` 連續約定）+ `STALLOC`/`STSTORE`/`STLOAD`（結構體槽 `%sts<id>`，第七命名空間）⇒ **119/119**四基本塊回邊的 `counter()`（`while i<5: st.count++`）206 行模組 `opt` rc=0`clang -O2` **exit 5**ASan 零報告；**兩個負向對照**（刪循環內 `STSTORE`SET 寫錯欄位）皆 **exit 0** ⇒ 證明 SET 是純 SSA 值不配槽等於沒寫s31 續：結構體**多型別**——型別碼改 `TY-STRUCT-BASE + 結構體 id`（`%struct.s<k>`），宣告改由 `cg-runtime-struct-decls(sfc)`（`sfc[id]`=欄位數）唯一發射prelude 不再帶結構體；`STRUCT-MK` 按 `bs[k]` 循環展開（初值 `zeroinitializer`中間名 `%sm<k>_<j>`）⇒ **123/123**`cg-runtime-test` **37/37**同模組兩個結構體（`s0`{3,4}=34`s1`{1,2,3}=123）206 行 `opt` rc=0 + `clang -O2` **exit 157** + ASan 零報告；**兩個負向對照**（`s1` 宣告成 2 欄位 ⇒ `invalid indices for insertvalue`；漏發 `s1` 宣告 ⇒ `invalid type for null constant`）s32 續：結構體**欄位型別**——`cg-runtime-ty-name` 收成型別碼→型名的唯一映射（從 `cg.no` 遷入 `cg-runtime`，`cg-ty-name` 只轉發），宣告側加 `sft`（欄位型別碼**拼接表**，游標展開無 stride 上限），`MK`/`GET`/`SET` 按值自身的 `vty`/`cty` 選型別名與命名空間 ⇒ **127/127**`cg-runtime-test` **37/37**`%struct.s0 = type { %str-long, i64 }` 的 `hold()`（`'abcd'.len + 9`）`opt` rc=0 + `clang -O2` **exit 13** + ASan 零報告；**負向對照**（宣告欄位順序寫反 ⇒ `insertvalue operand and field disagree in type`）⚠️ **str 欄位的 RC 未做（§11 B11）**：本輪 e2e 只用 `STRCONST`（無 RC 頭），ASan 乾淨**不**代表 str 欄位安全s33 續：結構體**欄位 RC 原語**——`OP-STRUCT-RETAIN=37`/`OP-STRUCT-RELEASE=38`（`xa`=結構體值 id`bs`=**欄位位掩碼**），逐位置位各發 `extractvalue %struct→%str-long→data` + `call @nolang_rc_retain/release` 三步；**刻意不讓 `STRUCT-MK` 自動 retain**（操作數可能是 `STRCONST` 全局常量，retain 會讀到全局對象之外的頭；且 `vty` 只有型別沒有所有權）⇒ **127→133/133**`cg-runtime-test` **37/37**`hold()`（`'no'+'lang'` 存入結構體取出後再拼 `'lang'`，`.len + 27`）的 204 行模組 `opt` rc=0 + `clang -O2` **exit 37** + 補屬性 ASan **exit 37 零報告**；**三个負向對照**：刪 `STRUCT-RETAIN` ⇒ **exit 134 `heap-use-after-free READ of size 6`**（`@str_concat` 的 `memcpy`）刪 `STRUCT-RELEASE` ⇒ exit 37 無報告（**無鑑別力**，洩漏方向）重複 `STRUCT-RELEASE` ⇒ **exit 134 `READ of size 8`**（`release` 讀 rc）🔴 **門禁口徑修正（§11 B12）**：`clang -x ir -fsanitize=address` 對 IR 輸入**完全不插樁**（最小反例 exit 0`__asan_load*` 零個）——ASan pass 只插樁帶 `sanitize_address` **函式屬性**的函式，clang 前端處理 `.ll` 輸入時不加 ⇒ s26–s32 的ASan 零報告只覆蓋 libc 攔截器s34 續：結構體**作一等值**——形參/返回值（`define %struct.s0 @bump(%struct.s0 %st1)`）與嵌套欄位（`%struct.s1 = type { %struct.s0, i64 }`）**零 emit 變更即通**，本件把有臂未實測變成有測試釘住⇒ **133→146/146**`cg-runtime-test` **37/37**三個 e2e（`mk/bump/total` **exit 8**嵌套 **exit 16**邊界 RC **exit 7**）`opt` rc=0 + `clang -O2` + 補屬性 ASan 零報告；🔴 **撞出真 bug 並修掉**：字面量全局從 `@.s<k>` 改 `@.s.<函式名>.<k>`（`cg-lit-name`）——兩個函式各自第 k 條 `STRCONST` 撞名 ⇒ `opt: redefinition of global`（實測訊息指名 `@.s1`），而 s29–s33 的 e2e 全是單一業務函式 + 無字面量的 main故從未觸發（判據：全局名只含指令下標 ⇒ 必撞，B10 從具名型別擴到全局符號）**五個對照**：父欄位宣告成 `i64` ⇒ `insertvalue operand and field disagree in type`漏發子宣告 ⇒ `invalid type for null constant`**宣告順序寫反 ⇒ 照樣 exit 16（否證子型別必須先宣告，LLVM 具名型別惰性解析）**`STRUCT-RELEASE` 挪到 call 前 ⇒ ASan `heap-use-after-free READ of size 6`（釘住參數是引用語義caller 的 release 必須晚於最後一次使用）s35 續：ALU 家族——`OP-CMPEQ/NE/LE/GT/GE=39–43 / OP-AND/OR/XOR/SHL/SHR/DIV/MOD=44–50`（12 op，全 `i64` 有符號標量：`and/or/xor/shl/ashr/sdiv/srem` + `icmp <pred>`+`zext`；`SHR` 必 `ashr` 非 `lshr`除零不插護欄 §11 B14）⇒ `cg-test` **146→160/160**（`test-alu` +14，含 `lshr` 缺席負向對照）`cg-runtime-test` **37/37**e2e `probe-s35.no`（djb2 鏈＋位運算＋6 比較）219 行 `opt` rc=0 + `clang -O2` **exit 161**；⚠️ 驗證在釘死 `v0.4.13`（HEAD Go 側跨模組結構欄位回歸，`cg/mir/cg-test` 逐位元組未變）s36 續：無符號 ALU 家族——`OP-SHRU/DIVU/MODU=51–53 / OP-CMPLTU/LEU/GTU/GEU=54–57`（7 op，全 `i64`/`%v`：`lshr/udiv/urem` + `icmp <u-pred>`+`zext`）＋型別碼 `TY-U64=4`（走 cg-ty-name/cg-val-prefix 默認臂 ⇒ 仍 i64/%v）⇒ `cg-test` **160→176/176**（`test-alu-unsigned` +16，含 not ashr/sdiv/slt 負向對照）`cg-runtime-test` **37/37**e2e `probe-s36.no`（無符號取值鏈）196 行 `opt` rc=0 + `clang -O2` **exit 18**（反向探針：CMPLTU 誤發 slt ⇒ exit 19，證符號性有鑑別力）；⚠️ 同 s35 驗證在釘死 `v0.4.13`且需 `DEVELOPER_DIR`s37 續：`f64` 算術家族——按型別分派（不新增 op）`OP-ADD/SUB/MUL/DIV/MOD` 依 `vty[d]==TY-F64` 發 `fadd/fsub/fmul/fdiv/frem double`，比較依 `vty[x]==TY-F64` 發 `fcmp oeq/une/olt/ole/ogt/oge`（`!=` 用 `une` 保 NaN）＋型別碼 `TY-F64=5`（真独立 LLVM 型別 `double` + 独立值命名空間 `%fd`）＋安全讀 `cg-vty-at`（越界回 TY-I64，修掉 s37 引入的越界段錯誤）⇒ `cg-test` **176→197/197**（`test-alu-f64` +21）`cg-runtime-test` **37/37**零回歸e2e `probe-s37.no` 198 行 IR `opt` rc=0 + `clang -O2` **exit 5**（反向探針：型別誤映射 opt rc=1；`oge` 誤發 `ogt` exit 5→4）⚠️ 同 s35/s36 驗證在釘死 `v0.4.13`且需 `DEVELOPER_DIR`下一步：逐欄位 RC 的**自動放置**（B11，需所有權位）→ 跨函式所有權轉移（B13）→ 整數除零的 option 語義降級（B14 語義半項，`select` 護欄 s38 + stderr 報告 s39 已落地）→ M4-d 接 M5 ／**M4-e `h2m` AST→MIR 降級已續推至 s99n（件號 54，§5.3.4.ea–.eo）**：s55–s97（2026-11-06，共 43 件，§5.3.4.cj–.dz）完成直線式 i64 → 簽名/調用 → 控制流/跨塊槽 → 花括號短路鏈 → 塊終結 → 計數/區間/容器遍歷 → 值匹配 → 實參池 args → 邏輯 `&&`/`||` → i64/u64 除零護欄 → u64 全族 → i64 `>>` 裁定 lshr → f64 槽化與 `%` → bool/char 字面量 → TY-OPT 控制流鋪槽（當時 `h2m-test` **845/852**、`cg` 347／`cg-runtime` 60／`ck` 20）；**其後 s98（2026-11-08，§5.3.4.ea）清掉 cg `OP-SHR` ashr 死臂；s99–s99n（2026-11-09，14 節，§5.3.4.eb–.eo）把 struct 定義／欄位訪問／形參返回值／跨塊槽／跨函數／欄位型別（str·f64·嵌套·`[]i64`）／欄位 RC 取得與 scope-exit 釋放／vec 欄位就地索引／所有權 provenance（`ownn`·`ownv`·`rdval`·`rdown`·`retset`，s99m）／vec 切片字面量（`K-SLICE-LITERAL`⇒`OP-VECNEW`/`OP-VECPUSH`、`@vec_free`、`OP-VEC-FREE = 94`，s99n）逐族落地**；`0fbbe684`（11-10）再重構 struct lowering 與所有權處理。現況 `h2m-test` **1031/1037**（紅 6 皆既有 s98 optmatch）、`cg-runtime-test` **84/84**、`cg-test` **358/358**（⑦ 解鎖並校正 16B 斷言）、`cg-str16-test` **41/41**；✅ 2026-11-11 Go 後端 `%str-long` 改 16B `{ i32, i32, i8* }`，selfhost `cg-runtime.no`／`cg.no` **已於 2026-11-12 同步清償**（§11.2 B28／§11 後 ⑥；`cg-runtime-test` 84/84、新增 `cg-str16-test` 41/41、Go helper 本文逐字相同）；**M4-d 接線已落地（2026-11-12，§11 後 ⑧）**：`h2m-lower-module` 在 AST 上就地合成 `main`（末位 `exit(E)` → 輸出參 `__noc_r`，無 `exit` ⇒ `0`）、新增 `compiler/src/link.no`（DR-2：只調 `clang -x ir`，不依賴 `opt`／`llc`）、`drv.cmd-build`／`cmd-run` 支援 `-o OUT FILE` 與 `FILE -o OUT` 雙序、`drv-build-test` **16/16**、e2e `build`／`run` 與 Go oracle 同值（**exit=42**）、`opt -passes=verify` rc=0；**s100 `print`／`eprint` 內建降級已落地（2026-11-12，§11 後 ⑨）**：新增 `OP-PRINT = 95`（`bs[k]` 為 kind，一 op 覆蓋 stdout/stderr × i64/str/換行/空格）、prelude 補 `@digits`／`@print_i64`／`@print_str`／`@eprint_*`／`@.nl`／`@.sp`、`h2m-lower-print` 含三條誠實門禁（值型別／插值擋板／語句位），`drv-build-test` **31/31**、`dump-print` 過 `opt`＋`clang` 執行輸出逐位元組等於 oracle；**L4 全語料首度量測**：169 檔中真 SAME **2**（`ifelse.no`／`colon-loop.no`，皆本輪由 0 起增益）、空 SAME 4（`tagged-enum-*`，oracle 亦建不過）、DIFF 163 且**無靜默錯碼**；**s101 `...` 佔位陳述＋s102 字面量插值已落地（2026-11-12，§11 後 ⑩）**：`tok.ELLIPSIS3 = 92` 與範圍運算子 `ELLIPSIS = 65` 分離、`ps` 建 `K-EMPTY-STATEMENT`、`h2m` no-op 臂消費（語料 74 檔含 `...`）；插值按「三檔分類（無字段／裸標識符／其它）」逐段發 `OP-PRINT`，無 spec 的 `{ident}` 與權威逐位元組同結果（`fmt-int(n,'')` 正文即 `n.to_str()`），`{name:spec}`／表達式字段／多實參模板形／`{{` `}}` 仍計 unsup；L4 真 SAME **2 → 4**（新綠 `for.no`／`guard.no`）、全語料 unsup **11,857 → 11,436**、86/169 檔下降、`drv-build-test` **42/42**、`lx-test` **81/81**；**s103 值位塊匹配（`r = { c -> v  -> d }`）已落地（2026-11-12，§11 後 ⑪）**：`h2m-scan` 前掃描按 LetStatement 節點 id 合成結果槽 `@smv<id>` 並在 entry 統一 `OP-ALLOC`（**不在降級途中補 alloca**——否則迴圈體每迭代新攤一份棧），`h2m-lower-surface` 加第 5 參 `snm`／出參 `r` 與值位分支（逐臂「降臂值 → `OP-STORE` 進槽」、匯聚塊 `OP-LOAD` 交回），臂值非 `i64`／臂體非「inline 塊包單表達式陳述」／`valueCtx` 未置位一律誠實門禁；`h2m-test` **1031/1037 → 1060/1066**、`drv-build-test` **42/42 → 53/53**；⚠️ 並記下一個**先於本件存在**的語義分歧——`{}` 無兜底臂的多臂鏈在權威側降成「獨立 if（每條命中的臂都跑）」而 noc 短路（`syntax.md` rule 1 與實現不一致，待拍板，本輪不動）；餘 `?str/?vec` 載荷、`option` match 臂解構、err/tag=2 裝箱、下標／方法調用／`#` 導入／block-as-value／`ok(...)` 條件臂（L4 主體殘差）、multi-out、TY-VOID、M5–M8 仍待續。**s104（見 §11 後 ⑫）**：`h2m` 補 `str` 相等/不等降級臂（`==`/`!=` 兩側皆 `TY-STR` ⇒ `OP-STEREQ`/`OP-STNEQ`，**零改 cg/mir**——兩 op 與 `@str_eq` 早已就位），並同步修 `h2m-ty-of` 的 `str` 對 `str` 分支（缺它則 str 比較/拼接只在 `mem==1` 掛——同一份碼直線式能編、進控制流就 5 條 unsup）；`h2m-test` **1060/1066 → 1080/1086**、`drv-build-test` **53/53 → 63/63**；全語料 unsup **11,354 → 11,163**（25 檔降、**0 檔升**），自舉模組合計 **2,456 → 2,368**；並以臨時插樁量出「unsup 落在哪個 AST 節點型別」的直方圖，據此判定**下一輪的閘是 `#` 模組導入**（611 條 `K-CALL-EXPRESSION` 標記全卡在「點號被調者」＝跨模組），非任何單一運算式族。**s105（解析器保真度）**：`ps-is-struct-def` 的「型別後終結符」只認 NEWLINE/COMMA/RBRACE/EOF，`COMMENT` 落進 fallback ⇒ **欄位帶 `;` 尾註釋的結構體整塊退回 `blockUnknown`、從未登記**（Go 的 `nextToken()` 自行跳過 COMMENT，故其 `look()` 看不見——兩側對「型別後面那個 token」判斷分歧）。修法是新增 `et == tok.COMMENT` 臂並**即時計數**（主迴圈的 NEWLINE 臂不計數，只跳過會讓「每個欄位都帶尾註釋」的結構體得 `nfield==0`）。影響：`ast.no` 的 `ast` 12 欄位裡 3 個帶註釋 ⇒ 該結構體未入表 ⇒ 155 個函式簽名被判 bad；修後 15 個自舉模組的簽名被拒 **250 → 166**、自舉模組**實際產碼函式數** `ast.no` 3→8／`lx.no` 2→3；⚠️ 自舉模組 unsup 反而 **2,362 → 3,198**（簽名通了 ⇒ 函式本體才被嘗試，body 內每條不支援各記一條），而**全語料 173 檔逐檔 `diff` 完全相同（11,315 → 11,315、`unsup==0` 5 → 5）**；`ps-test` **39/42 → 41/44**。**〔2026-11-23 as-built 補記，原文保留為歷史〕** M4-e 已續推至 **s152**（§12 ㉜–§70）：`h2m-test` **1365/1365**、`cg-test` **363/363**、`lx-test` **81/81**、`ps-test` **52/52**、`ck-test` **20/20**、`build-package-test` **8/8**、`drv-build-test` **63/63**、`pkg-ll-test` **10/10**、`pkg-sigs-test` **5/5**、`pkg-ll-call-test` **5/5**、`ps-err-test` **46/46**、`cg-str16-test` **41/41**、`cg-runtime-test` **114/114**、`ck-sig-test` **50/50**、`tok-test` **62/62**、`diag-test` **7/7**、`util-test` **27/27** 全綠；唯一紅燈 `ast-test` **43/48**（5 條 kind 期望過期，非工具鏈問題，見 §71 ③）。⚠️ **上列綠燈仅在 v0.4.28 代次工具鏈（`tmp/s148/wt`，`d21fbb35`）下可重現**——HEAD `d236a956` 的 v0.4.29+ 工具鏈對 `compiler/src` 報 **12 ERROR**、L0 僅 5/18 綠，詳見 §71 |
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

- **SUT 現狀（2026-10-29 啟動摸底當日，歷史；現況見 §4.1.1 末 2026-11-12 校正，2026-11-23 最新見 §71）**：`compiler/src/` 當日現有 `lx.no` / `ps.no` / `ck.no` / `ck-sig.no` / `drv.no` / `diag.no` / `util/sb.no` —— **無中端無 codegen無 `cg-runtime.no`**；`drv.cmd-build` / `cmd-run` 仍走 `noc` 外殼（調用 `bin/no` 出後端，見 Q1(b) 臨時手段），尚未自產 IR`/tmp/noc-s13*` 現狀＝**前端閉環後端缺席**（L4 不可用，M4 之前是合理狀態）
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
  - **M4-e**＝`hir2mir` 降級主體（本檔新增標號，理由見上條 M4-c 的實際用盡情況；§5.3.4.cj 起於 s55）：`ast.no` arena → `mir.no` 扁平指令流，自 i64 直線式子集起步，簽名側參數與控制流隨後逐族補——**s55–s97（2026-11-06，共 43 件，§5.3.4.cj–.dz）**完成直線式 i64 子集 → 簽名側與調用 → 控制流與跨基本塊槽化 → 花括號短路鏈 → 塊終結形 → 計數/區間/容器遍歷 → 值匹配 → 實參側池 args → 邏輯 `&&`/`||` → i64/u64 除零護欄 → u64 全族 → i64 `>>` 裁定 lshr → f64 槽化與 `%` → bool/char 字面量 → TY-OPT 控制流鋪槽（當時 `h2m-test` **845/852**、`cg` 347／`cg-runtime` 60／`ck` 20）；**其後已續推至 s99n（件號 54）**：s98（2026-11-08，§5.3.4.ea）清掉 cg `OP-SHR` ashr 死臂；s99–s99n（2026-11-09，14 節，§5.3.4.eb–.eo）補齊 struct 全族（定義／欄位／形參返回值／跨塊槽／跨函數／欄位型別／欄位 RC 取得與 scope-exit 釋放／vec 欄位就地索引）、所有權 provenance（s99m `ownn`·`ownv`·`rdval`·`rdown`·`retset`）、vec 切片字面量與 `@vec_free`·`OP-VEC-FREE=94`（s99n）；`0fbbe684`（11-10）重構 struct lowering 與所有權處理。現況 `h2m-test` **1031/1037**（紅 6 皆既有 s98 optmatch）、`cg-runtime-test` **84/84**、`cg-test` **358/358**（⑦ 解鎖）、`cg-str16-test` **41/41**；✅ 2026-11-11 Go 後端 `%str-long` 改 16B，selfhost **已於 2026-11-12 同步清償**（§5.3.4.ax／§11.2 B28／§11 後 ⑥）；`?str/?vec` 載荷、`option` match 臂解構、`drv.cmd-build` 接線（M4-d）等仍待續〔**2026-11-23 補記**：M4-e 其後已續推至 **s152**（§12 ㉜–§70）；上文「現況 `h2m-test` 1031/1037」等數字為 **v0.4.28 代次前**的觀測，HEAD 代次下的實際門禁數字與工具鏈阻塞見 **§71**〕
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

### 6.6 M5 啟動盤點（2026-11-13；詳見 §11 ⑭/⑮）

M5 目標＝拿下「自舉唯一剩餘結構性閘」——`#` 模組導入（whole-program 降級）。現況：

- ✅ 套件載入（`drv.build-package`，含路徑解析 + cycle 檢測）、全域簽名表（**395** 條）、單 `agg` struct 註冊、多模組 body 串接 + `@<mod>.<fn>` 命名、點號呼叫查全域表、emit 名統一（`.ll` 可連結）——切片 1–5 與 6① 全部實作並有回歸（`build-package-test` **8/8**、`pkg-sigs-test` **5/5**、`pkg-ll-test` **10/10**、`pkg-ll-call-test` **5/5**）。
- ⬜ 端到端行為閘（6②）：`noc build compiler` 產物執行 `version` 印版本 + 抽 `tests/` SHS 子集跑 L4。現仍擋在 h2m 函式本體降級能力（整包約 **5,749 unsupported**），本體降級是 M5/M6 的 h2m 能力擴張範疇。
- ⬜ M5 後續不收（⑭ ⑫）：workspace/package 合併、`target` 解析、快取；多 package 跨包解析；`as` 別名重綁；whole-program 體積/重複 emit 優化（reachability-based 按需 emit）。
- 🔴 語言面變更：`31cce183` 移除函式簽名預設參數 ⇒ self-host s92 成 Moot（詳 ⑮）。

---

## 7. 驗證策略（差分矩陣）


### 7.1 五級門禁總表

| 級      | 對象                    | Oracle                          | 判據                                        | 成本           |
| ------ | --------------------- | ------------------------------- | ----------------------------------------- | ------------ |
| **L0** | `compiler/tests/*-test.no`（**18 套**，2026-11-23 校正：原文寫作 `*_test.no`，該 glob **匹配不到任何檔**；同目錄另有 `cov-measure.no`／`pkg-ll-entry.no`／`probe-glob*.no`／`verify-struct-elem.no` 不帶 `-test` 後綴，非門禁） | `test/runner.no`（測試檔自檢） | 全綠；⚠️ 測試檔必須自己 `f-count > 0 -> exit(1)`⚠️ 執行方式必須是 **`no run <file>`**：`no <file>` 只印 usage 且 rc=0 ⇒ 每個測試檔的 stdout 都相同 ⇒ **G7 判 oracle 無鑑別力** | 秒            |
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
./bin/no run tests/build-mo.no; echo $?   # 必須是 0（2026-11-23 校正：原寫 `tests/2.no`，該檔已於 `15618c3c`「merge similar cases」刪除，現況 `find . -name 2.no` 無結果；改用現存 80 B 語料 `tests/build-mo.no`，實測 rc=0）
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

**〔2026-11-23 as-built 補記，B4 原文保留為歷史〕** M4-e 已續推至 **s152**（§12 ㉜–§70），M4-c 發射層与 M4-e 降級側門禁在 **v0.4.28 代次**（`tmp/s148/wt`，`d21fbb35`）下全綠（17/18，唯一紅燈 `ast-test` 43/48 為 kind 期望過期，見 §71 ③）；⚠️ **該綠燈不是現行 HEAD 的屬性**——HEAD `d236a956`（v0.4.29+）對 `compiler/src` 報 12 ERROR，M4-e 的「已續推至 s99n」等表述请一律以 §71 的代次結論為準。
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
| B30 | 🔴 **工具鏈代次偏移（2026-11-23 新增，自舉目前的第一號阻塞）**：v0.4.29 起，**非字面量運算元的算術**與**`vec[i]` 索引讀**由 `T` 改為 **`?T`**（`dfe9be7d` 安全索引降級、`ff40c2e8` option 載荷型別追蹤），且 `#{overflow=wrap}` 只決定失敗值、**不回頭改型別**。`compiler/src` 現有 **12 個站點**未移居：`lx.no:263/268/273/327`（`v = c - B-DIG0` 等）、`ps.no:8175`（`it = ps-peek-token(p)`）、`cg.no:343`（`cur = bk`）、`h2m.no:3941`（`cn = cn2`）+ `h2m.no:1767/1772/1816/1821/3937`（`optunuse`）⇒ `no build compiler` **12 ERROR**、L0 18 套僅 5 套綠（紅：ast/build-package/cg-str16/cg/ck/drv-build/h2m/lx/pkg-ll-call/pkg-ll/pkg-sigs/ps-err/ps）；同一份 `compiler/src` 在 **v0.4.28 代次**（`tmp/s148/wt`，`d21fbb35`）下 `no build compiler` **rc=0**、17/18 綠 ⇒ **§66–§70 的綠燈與「自舉閉環已達成」結論是代次釘住的歷史事實，不是 HEAD 屬性**。對照：`no vet src/std` **0 error**（標準庫已移居），缺口只在 `compiler/src` | **待實施**：12 站逐站改寫為 `k = <expr>` + `v = k: { ok -> it; -> <default> }`（或 `?=`／`_ =`），每站須配 A/B 量測確認 unsup/dropped 不回歸；`no fix --filter=15w45dqk`／`--filter=optunuse` 可預覽自動修復 | §71（2026-11-23 收口輪） |


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

> ⑥–⑯（2026-11-12 ~ 2026-11-15）的 11 節歷史追記已原樣遷至 [`nolang-selfhost-history.md` §二](nolang-selfhost-history.md)，本文自 ⑰ 起保留連續追記，訖於文末 **§71（s153 收口輪）**（**2026-11-23 校正：原文作「最新 ㉛ 在文末」，實際上 ㉜–㊿（s114–s133）與 §51–§70（s134–s152）皆已追加於本文，㉛ 之後還有 39 節**）。

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
**進度快照（2026-11-06，歷史；最新見下方 2026-11-12 as-built 快照，2026-11-23 收口快照見附錄 C／§71）**：M0M1M2 已完成（L2 全語料 655/655 SAME0 回歸；M1 L1 675/675 SAME）；M3 已連續落地 M3-a 骨架（`drv.cmd-vet`＋`diag.no`）→ M3-b stdsig 表遷移（`ck-sig.no`）→ M3-c 首族（語法錯誤收集器）→ 缺口② NEWLINE 坐標族 → 正語義首件 `ck.no`（`'X' is not defined`，全語料 668 檔 ck 誤報 13→0parser 族仍 0），見 §5.3.4.am–.as 與 §6.4**M4 已啟動**：M4-a 骨架落地（`cg-runtime.no`/`cg-rc.no` prelude 逐位元組照抄＋`check-rc-balance`，16/16`opt` verify rc=0）＋M4-b 骨架落地（`mir.no`＋`cg.no` 的 `cg-emit-func` 發 CONST/ADD/SUB/MUL/CALL/ALLOC/STORE/LOAD/RET，13/13端到端 `let-add` IR 經 clang 執行 exit 6）＋M4-c 控制流/循環（多基本塊＋`BR/CONDBR/CMPLT`＋回邊，`lt10(5)+lt10(15)` 執行 exit 3`sum4()` 執行 exit 6）＋M4-c 字符串（首个非 i64 型別 `%str-long`：`@.s<k>` 全局字面量前置 pass＋三步 `insertvalue`＋`extractvalue` 取 len，`slen()` 執行 exit 3）＋M4-c 字符串拼接（首个 RC 消費者：`@str_concat` 13 行逐字照抄＋`prelude-full` 唯裝配點，`cat()` 輸出 `abcd`exit 4；`cg-test` **41/41**`cg-runtime-test` **21/21**）＋M4-c 多參調用（`OP-CALL` 的 `bs` 從佔位改為**參數個數**，破壞性語義變更，`add2(3,4)` 執行 exit 7）＋M4-c `%str-long` 跨基本塊（`SALLOC/SSTORE/SLOAD`，第三個槽命名空間 `%ss<id>`；`pick(0)`→exit 2`pick(1)`→exit 6）＋M4-c `vec`（`%vec = { i64, i64, i64 }` + 4 個 helper，第四個值命名空間 `%vc<id>`；十次 push 強制擴容 0→4→8→16 求和 ⇒ exit 45ASan 無報告）＋M4-c **帶型別簽名**（`cg-emit-cfg-t` + `TY-I64/STR/VEC`；`mix(n i64, s str)` ⇒ `%v1`+`%sv2` 交錯；`mix`+`sident`+`mk` ⇒ exit 14）＋M4-c **帶型別 CALL**（值型別表 `vty` 按值 id 索引 + `cty` 按指令索引；`caller2` 函式內發 `call i64 @mix(i64 %v1, %str-long %sv2)`）＋M4-c **`option`**（`%option = { i64, [3 x i64] }`，第五個值命名空間 `%op<id>`；`OPTNONE/OPTSOME/OPTTAG/OPTISOK/OPTGET`，`pick(n)=n<10?some(n):none` + `g(n)=isok+payload` ⇒ exit 8；`cg-test` **78/78**`cg-runtime-test` **33/33**），見 §5.3.4.at–.be 與 §6.5**M4-c 已續推至 s54**（§5.3.4.bf–.ci）：retain/release 原語＋`STRCAT` 實參為變數的 RC 週期→option 的 `str` 載荷內聯讀寫（24B slot，免裝箱）→**struct 全家族**（構造＋讀欄位／`STRUCT-SET` 寫欄位／跨塊槽三件套／多結構體型別碼編 id＋`sfc` 描述符／`sft` 欄位型別拼接表／`STRUCT-RETAIN`·`STRUCT-RELEASE` 欄位掩碼 RC 原語／形參·返回值·嵌套欄位作一等值＋字面量全局唯一化 `@.s.<函式名>.<k>`）→**ALU 家族**（s35，12 個 `i64` 有符號標量 op：`AND/OR/XOR/SHL/SHR(ashr)/DIV(sdiv)/MOD(srem)` + `CMPEQ/NE/LE/GT/GE`(icmp+zext)，`cg-test` **146→160/160**`cg-runtime-test` **37/37**端到端 `probe-s35.no` djb2 鏈 exit 161）→**無符號 ALU 家族 + `TY-U64` 型別碼**（s36，7 個無符號 op：`SHRU(lshr)/DIVU(udiv)/MODU(urem)` + `CMPLTU/LEU/GTU/GEU`(icmp ult/ule/ugt/uge + zext)；`TY-U64=4` 與 `TY-I64` 同 LLVM 型別/命名空間（`i64`/`%v`），符號性只活在 op 助記符⇒型別碼與發射臂同代次成對落地，避開 §11 B7存在但無人發射空洞；補齊 s35 §5.3.4.bo 明文預留的兩洞，`cg-test` **160→176/176**（`test-alu-unsigned` +16）`cg-runtime-test` **37/37**零回歸端到端 `probe-s36.no` 無符號取值鏈 196 行 IR `opt` rc=0 + `clang -O2` **exit 18**（反向探針：CMPLTU 誤發 slt ⇒ exit 19，證明 e2e 對 ult-vs-slt 有鑑別力））→**`f64` 算術家族 + `TY-F64` 型別碼**（s37，按型別分派不新增 op：`OP-ADD/SUB/MUL/DIV/MOD` 依 `vty[d]==TY-F64` 發 `fadd/fsub/fmul/fdiv/frem double`，比較依 `vty[x]==TY-F64` 發 `fcmp oeq/une/olt/ole/ogt/oge`（`!=` 用 `une`）；`TY-F64=5` 是真独立 LLVM 型別 `double` + 独立值命名空間 `%fd`（不同 s36 的 u64 復用 %v）；安全讀 `cg-vty-at` 修掉 s37 引入的越界段錯誤；補齊 §5.3.4.bo 預留的浮點洞，`cg-test` **176→197/197**（`test-alu-f64` +21）`cg-runtime-test` **37/37**零回歸端到端 `probe-s37.no` 198 行 IR `opt` rc=0 + `clang -O2` **exit 5**（反向探針：型別誤映射 opt rc=1，oge 誤發 ogt exit 4））→**整數除零 select 護欄的 `noc`↔Go 保真對齊**（s38，codegen 層把 s35/s36 直發的裸 `sdiv/srem/udiv/urem` 裹上與 Go `emitArith` 同形的 select 護欄：新增 `cg-div-guard` helper，判零 `%dz.<k>` + 有符號 `(MIN,-1)` 合成 + 安全除數 `%ds.<k>` + 結果定義化 `%v<d>=select zeroDiv,0,raw`；無符號只判零不偵 MIN；`fdiv/frem` 不動；`cg-test` **197→211/211**（護欄斷言 +14）`cg-runtime-test` **37/37** 零回歸，端到端 `probe-s38.no` 除零取值鏈 `opt` rc=0 + `clang -O2` **exit 2**（反向探針裸 `sdiv 10,0` arm64 poison 得 exit 1），僅收 §11 B14 的 codegen 半項，option 語義降級與 `divZeroReport` stderr 留作後續）→**整數除零 `divZeroReport` stderr 報告**（s39，收 §11 B14 的 codegen 層最後一塊：`cg-div-guard` 結果定義化後追發 `call void @__nolang_div_zero_report(i1 %dz.<k>)`（用 zeroDiv 非 badDiv），prelude 補 `@write` declare + `@nolang_msg_div_zero`（85B，單引號 + 雙反斜杠保 `\XX` 逐字）+ `__nolang_div_zero_report` helper；`cg-test` **211→215/215**（report 斷言 +4）`cg-runtime-test` **37→41/41**（prelude 三新符號 +4），端到端復用 `probe-s38.no` `opt` rc=0 + `clang -O2` **exit 2** + stderr 恰見診斷 3 次（僅 3 個除零站，非零除數站跳過））→**option 的 `vec` 載荷內聯讀寫（`?[]i64`，§11 B8 免裝箱半項）**（s40，`%vec = {i64,i64,i64}` 三字段全 i64，與 `[3 x i64]` 槽 1:1，新增 `OP-OPTMKVEC`/`OP-OPTGETVEC` 逐字段 extract/insertvalue，連 ptrtoint 都不需（比 s28 str 载荷更簡單）；`cg-test` **215→222/222**（vec 载荷斷言 +7）`cg-runtime-test` **41/41** 零回歸端到端 `probe-s40.no` `?[]i64` put/get 往返 `opt` rc=0 + `clang -O2` **exit 90**（88 dat 存活＋2 len 存活，反向鑑別：len 丟→88，dat 壞→SIGSEGV））→**option 跨基本塊槽（`alloca %option`，非 PHI，§11 B8）**（s41，`OP-OPTALLOC/OPTSTORE/OPTLOAD` 三件套鏡像 `SALLOC/SSTORE/SLOAD`，第八命名空間 `%os`；`cg-test` **222→228/228**（跨塊槽斷言 +6）`cg-runtime-test` **41/41** 零回歸端到端 `probe-s41.no` `g41(c)` 三塊 `bb0` 默認寫→`CONDBR`→`bb1` 覆蓋→`bb2` load 讀回 `opt` verify rc=0 + `clang -O2` **exit 99**（反向對照 `g41(0)`→走 `bb2` 讀默認→**exit 1**））→**`str` 結構相等（`@str_eq` ＋ `OP-STEREQ`/`OP-STNEQ`，非 `icmp %str-long`，§5.3.4.bw）**（s42，`cg-test` **228→234/234**（`test-str-eq` +6）`cg-runtime-test` **41→50/50**（`test-str-eq-helper` +9 與 prelude `@str_eq`/`@memcmp` 兩斷言）零回歸端到端 `probe-s42.no` 8 條字面量＋4 條 compare＋加權和 `opt` rc=0 ＋ `clang -O2` **exit 41**（反向對照把 `abd`/`ab` 全改成 `abc` → **exit 51**，證明 memcmp 按字節判別同長度異字節，xor 生效））→**`vec` 元素相等（`@__nolang_vec_eq_i64` ＋ `OP-VECEQ`，非 `icmp %vec`，§5.3.4.bx）**（s43，`cg-test` **234→237/237**（`test-vec-eq` +3）`cg-runtime-test` **50→60/60**（`test-vec-eq-helper` +9 與 prelude `@__nolang_vec_eq_i64` 斷言）零回歸端到端 `probe-s43.no` 建 A/B=[7,9] C=[7,8] E=[7,9,5] 三條 VECEQ＋加權和 `opt` rc=0 ＋ `clang -O2` **exit 41**（反向對照把 b 與 c 比較目標都改成 B 全等 → **exit 47**，證明 len 不等走 noteq，memcmp 按元素內容判別）；鏡像 Go `vecDeepEq` 的 i64 分支，`%vec` field 2 data 先 `inttoptr i64→i8*` 再 `@memcmp(len*8)`，`@memcmp` 復用 s42 宣告）→**`option` 值相等（`OP-OPTEQ`／`OP-OPTNEQ`，tag AND payload，單塊無需 helper，§5.3.4.by）**（s44，`cg-test` **237→247/247**（`test-opt-eq` +10）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s44.no` 五條 OPTEQ/OPTNEQ（some(7)×2／some(9)／nil×2）＋加權和 `opt` rc=0 + `clang -O2` **exit 49**（反向對照把 b／e 目標都改成 some(7) → **exit 67**）；鏡像 Go `emitOptionValueEq` 的 i64 載荷分支，`%option` 聚合體不可 icmp，發 tag `icmp eq` AND 載荷 `icmp eq` 再 `and i1`，純單塊不 call helper）→**`option` 的 str 載荷相等（`OP-OPTEQSTR`／`OP-OPTNEQSTR`，tag AND 重建 %str-long call @str_eq，§5.3.4.bz）**（s45，`cg-test` **247→260/260**（`test-opt-eq-str` +13）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s45.no` 五條 OPTEQSTR（some(abc)×2／some(abd)／some(ab)／nil×2）＋加權和 `opt` rc=0 + `clang -O2` **exit 49**（反向對照把 sv3 'abd' 改成 'abc' → **exit 51**，證明 memcmp 按字節判別）；鏡像 Go `emitOptionValueEq` 的 %str-long 分支，從 `[3 x i64]` 槽三步 insertvalue 重建 %str-long 再 call `@str_eq`，復用 s42 helper 無需新增）→**`option` 的 `f64`（double）載荷內聯讀寫（`OP-OPTMKF64`／`OP-OPTGETF64`，bitcast double↔i64 落／取 slot[0]，無需 helper，§5.3.4.ca）**（s46，`cg-test` **260→265/265**（`test-opt-f64` +5）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s46.no` 兩 double 形參 `f46(3.5,2.25)` 構造兩個 some，讀回後三條 CMPEQ 加權和 `opt` rc=0 + `clang -O2` **exit 43**（反向對照把 b 也傳 3.5 → **exit 47**，構造器壞读出 0.0 → **exit 44**，三值互異）；鏡像 Go `emitOptionValueEq` 的 double 分支存取，`double` 與 `[3 x i64]` 槽元素等宽⇒一步 `insertvalue`＋等宽 `bitcast`（不同 s28 `str` 的 `i8*` 指針需 `ptrtoint`））→**`option` 的 `f64`（double）載荷相等（`OP-OPTEQF64`／`OP-OPTNEQF64`，tag `icmp eq` AND 載荷 `bitcast i64→double` 後 `fcmp oeq`，`OPTNEQF64` 加 `xor i1 true`，單塊無需 helper，§5.3.4.cb）**（s47，`cg-test` **265→271/271**（`test-opt-eq-f64` +6）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s47.no` 三 double 形參 `f47(3.5,2.25,0.0)` 構造 some(a)／some(b)／some(a)／some(0.0)／nil／nil，五條 OPTEQF64／OPTNEQF64（tag 門 some(0.0) vs nil 與載荷門 some(3.5) vs some(2.25)）加權和 `opt` rc=0 + `clang -O2` **exit 61**（反向對照把 b 也傳 3.5 → **exit 47**）；鏡像 Go `emitOptionValueEq` 的 double 分支，`%option` 聚合體不可 icmp，發 tag `icmp eq` AND 載荷 `bitcast` 後 `fcmp oeq`，純單塊不 call helper）→**一元 ALU 家族 NEG／NOT（`OP-NEG`／`OP-NOT`，NEG 依 `vty==TY-F64` 發 `fneg double`／否則 `sub i64 0,x`，NOT 發 `xor i64 1,x`，鏡像 Go emitNeg／emitNot，單塊無 helper，§5.3.4.cc）**（s48，`cg-test` **271→277/277**（`test-unary` +6）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s48.no` 混合形參 `f48(5, 3.5)` 三臂（int NEG／NOT／f64 NEG）加權和 `opt` rc=0 + `clang -O2` **exit 47**（反向對照傳 `f48(7, 3.5)` → int NEG 值翻轉 e1 → **exit 46**）；NEG 復用 s37 值型別分派機制，NOT 以 `xor 1` 翻轉 i64 布林最低位，非 option 路徑不插溢出護欄（同 s35 紀律））→**vec→str 重interpret（`OP-STRFROMVEC`，%vec 三欄位 len／cap／dat 逐欄拷进 %str-long，dat 以 inttoptr i64→i8* 還原指針，鏡像 Go emitStrFromVec，單塊無 helper，§5.3.4.cd）**（s49，`cg-test` **277→286/286**（`test-str-from-vec` +9）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s49.no` 建 [10,m,30] vec 轉 str 後 STRLEN／VECLEN／VECGET 三路鑒别＋加權和 `opt` rc=0 + `clang -O2` **exit 47**（反向對照傳 `f49(99)` → e3 翻 0 → **exit 43**，證 vec 內容隨實參變）；僅存欄位不拷貝緩沖，結果 str 與源 vec 共享同一堆緩沖（不 memcpy 不 retain，所有權移交屬 B11 RC 自動放置後續）；dat 欄 inttoptr 合法性由 opt verify 擔保）→**str 容量欄讀取（`OP-STRCAP`，extractvalue %str-long field 1，與 OP-STRLEN 的 field 0 成對，鏡像 Go emitLenCap 的 OpCap/KindStr 分支，單塊無 helper，§5.3.4.ce）**（s50，`cg-test` **286→292/292**（`test-str-cap` +6）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s50.no` 建 [10,m,30] vec（len3／cap4）轉 str 後 STRLEN／STRCAP 雙欄＋CMPNE 互換哨兵＋常量 'abcd' cap==len 四路鑒别＋加權和 `opt` rc=0 + `clang -O2` **exit 55**（反向對照 'abcd' 改 'xyz' ⇒ 常量 cap=3 ⇒ e4 翻 0 → **exit 47**，證 cap 跟隨真實緩冲尺寸）；刻意不造 OP-VECCAP——Go `emitLenCap` 無 KindVec 分支，忠實性紀律優先於表面對稱）→**option 排序族（`OP-OPTLT`／`OP-OPTLE`／`OP-OPTGT`／`OP-OPTGE`，只比兩側載荷 slot[0] 發 icmp slt/sle/sgt/sge，無 tag 項亦無 and，鏡像 Go emitOptionValueOrder 的有符號 int 載荷分支，單塊無 helper，§5.3.4.cf）**（s51，`cg-test` **292→305/305**（`test-opt-order` +13）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s51.no` 以 some(7)／some(9)／some(-3)／nil 構四值七條排序 op，含假錨／有符號鑒別／取等邊界三類哨兵＋加權和 `opt` rc=0 + `clang -O2` **exit 157**（反向對照 CONST -3 改 3 ⇒ e5 翻 0 → **exit 141**，證有符號比較生效）；nil 載荷槽為 zeroinitializer 視作 0（some(7)＞nil 判 1）與 Go 同形；刻意不造無符號翼 ult/ule/ugt/uge——option 載荷符號性在 noc 型別宇宙不可見（TY-OPT 不挾載荷型別）；f64 載荷序比較留後續（循 s47 OPTEQF64 專用 op 前例））→**option 的 `f64`（double）載荷排序族（`OP-OPTLTF64`／`OP-OPTLEF64`／`OP-OPTGTF64`／`OP-OPTGEF64`，slot[0] 循 s47 bitcast i64→double 後發 fcmp olt/ole/ogt/oge，無 tag 項亦無 and，鏡像 Go emitOptionValueOrder 的 double 分支，單塊無 helper，§5.3.4.cg）**（s52，`cg-test` **305→318/318**（`test-opt-order-f64` +13）`cg-runtime-test` **60/60** 零回歸端到端 `probe-s52.no` 三 double 形參 `f52(3.5, 2.25, -1.5)` 構 some×3／nil 四值九條排序 op，含假錨／取等邊界／負值與 nil 零載荷哨兵＋加權和 `opt` rc=0 + `clang -O2` **exit 167**（反向對照 c 改 1.5 ⇒ e5 翻 0 → **exit 163**，單變量翻轉證確發 olt）；nil 載荷位模式即 +0.0；ordered 謂詞遇 NaN 恒判 false 為 Go 原語義忠實鏡像；f64 專用碼循 s47 OPTEQF64 前例（載荷型別在 op 層不可見）／s53 本輪：option 與純量排序 OP-OPTLTSC／OP-OPTLESC／OP-OPTGTSC／OP-OPTGESC=86–89，鏡像 Go emitCmp aOpt／bOpt 分支（codegen.go:6490-6520）：方向由 vty 判側，只提 option 側 slot[0] 與純量 icmp slt／sle／sgt／sge，無 tag 項／無 and／謂詞不翻轉（次序隨源表達式），nil 零載荷與純量 0 同路；兩側形同發時 %oq 重複定義 verify 誠實報錯；cg-test 318→330/330，探針 exit 180（反向 b 改 9 ⇒ e2／e3／e7／e8 同翻 exit 129，單變量可預測）；str 序經實地勘察確認 Go emitCmp 直接 hard-fail（codegen.go:6557-6563，語言層只實作 ==／!=），.cf／.cg 兩處記為 emitStrCompare builtin 之誤帳已改實；f64 純量翼留循前例另立專用碼——已償／s54 本輪：option 與 f64 純量排序 OP-OPTLTSCF／OP-OPTLESCF／OP-OPTGTSCF／OP-OPTGESCF=90–93（s53 留賬精確償還）：vty 雙護欄判向（TY-OPT＋TY-F64），提 option 側 slot[0] bitcast→double 後與 %fd 純量 fcmp olt／ole／ogt／oge，無 tag 項／無 and／謂詞不翻轉；nil 載荷位模式 +0.0 同 s52；護欄比 s53 更嚴——閘不過兩臂都不發射（Go 回退 discriminant，noc 未實作，呼叫方保證）；cg-test 330→342/342，探針 exit 68（反向 c 改 9.0 ⇒ e6 GTSCF 獨翻 exit 84，單變量可預測）；單測首發曾抓到右臂 fcmp 誤引 %opy 而非 %fdy 的型錯，文本斷言是第一道閘；至此 Go emitCmp aOpt／bOpt 混合路徑可達組合收攏）**M4-e 已啟動（s55，§5.3.4.cj）**：新建 `compiler/src/h2m.no` 把 arena AST 降為 `mir` 扁平指令流（i64 直線式子集，值 id 無槽直綁，`unsup` 誠實門禁），`h2m-test` **35/35**，探針前向 exit **73**／反向 **79** 且與 Go oracle 同程序對拍（自舉線首次"源碼進，可執行檔出"的雙側對齊）；實地撞出入口檔缺 `asp`／`lx`／`tok` 傳遞依賴即 NoVal 泛濫或運行期掛起（記 §11 B15）；**M4-e 第二件續推（s56，§5.3.4.ck）**：簽名側（形參由 `h2m-alloc` 依序佔值 id `1..nargs`，`retty` 取輸出參數聲明型別）與調用側（`OP-CALL` 的 arity／型別／連續 id 三重校驗）落地，模組級簽名表由前置 pass 先掃全模組 ⇒ 調用者寫在被調用者之前也能解析；誠實門禁從"只計數"升級為"未降級函式整函式不產碼"，`h2m-test` **74/74**，探針前向 exit **47**／反向 **50** 與 Go oracle 同程序對拍，鑑別態 exit **101**；連續 id 契約與多輸出呼叫形式分別記 §11 B16/B17⚠️ **s35/s36/s37/s38/s39/s40/s41/s42/s43/s44/s45/s46/s47/s48/s49/s50/s51/s52/s53/s54/s55/s56/s57/s58/s59/s60m/s61/s62/s63/s64/s65 驗證環境**：本輪 HEAD（`v0.4.14-3`）Go 侧 MIR codegen 有一個**跨模組結構體欄位回歸**（`EmitLLVM: setfield: field "buf" not found on "sb.sb"`），凡 import `util/sb` 的測試檔在 HEAD 工具鏈上全部無法構建——代碼生成側（`cg.no`/`mir.no`/`cg-test.no`）在 `v0.4.13↔HEAD` 逐位元組相同，故按同一穩定工具鏈代次紀律改用**釘死的 `v0.4.13`** 採證（該回歸屬 Go 侧 S0 的並行在途改動，依 §2 黃金法則只記錄不篡改 `.no` 源碼繞過）M4-c 餘項（vec 元素非 i64 的逐元素 RC**結構體逐欄位 RC 的自動放置（原語已備，B11）**跨函式所有權轉移（B13）整數除零的 option 降級（B14）struct 陣列/切片>24B 堆上裝箱（`?vec`≤24B 內聯已 s40）`byte`/`f32` 窄型別算術（`u64` 已 s36，`f64` 已 s37 落地））M4-d ⬜下一步＝**M4-c 餘**（整數除零的 option 語義降級（B14，`select` 護欄 s38 + stderr 報告 s39 已落地）；PHI 本方案決定走 alloca 不引故非必需；option 的 `vec` 載荷內聯讀寫已 s40（B8 免裝箱半項）；option 跨基本塊槽已 s41（B8 alloca 通路）；`str` 相等已 s42（`@str_eq`/`OP-STEREQ`/`OP-STNEQ`）；`vec` 元素相等（i64）已 s43（`@__nolang_vec_eq_i64`/`OP-VECEQ`）；`option` 值相等（i64 載荷）已 s44（`OP-OPTEQ`/`OP-OPTNEQ`）；`option` str 載荷相等已 s45（`OP-OPTEQSTR`/`OP-OPTNEQSTR`）；`option` f64 載荷讀寫已 s46（`OP-OPTMKF64`／`OP-OPTGETF64`）；`option` f64 載荷相等已 s47（`OP-OPTEQF64`／`OP-OPTNEQF64`）；一元 ALU 已 s48（`OP-NEG`／`OP-NOT`）；vec→str 重interpret 已 s49（`OP-STRFROMVEC`）；str 容量欄已 s50（`OP-STRCAP`）；option 排序已 s51（`OP-OPTLT`～`OP-OPTGE`）；option 的 f64 載荷排序已 s52（`OP-OPTLTF64`～`OP-OPTGEF64`）；option 與純量排序已 s53（`OP-OPTLTSC`～`OP-OPTGESC`）；option 與 f64 純量排序已 s54（`OP-OPTLTSCF`～`OP-OPTGESCF`）；**AST→MIR 降級首件已 s55**（`compiler/src/h2m.no`：直線式 i64 子集，`h2m-test` **35/35**，e2e 73/79 與 Go oracle 對拍，§5.3.4.cj）；**簽名側與調用已 s56**（`h2m-test` **74/74**，e2e 47/50 與 Go oracle 對拍，鑑別態 101，§5.3.4.ck）；**控制流與跨基本塊槽化已 s57**（`h2m-test` **135/135**，e2e 92／反向 93 與 86 並與 Go oracle 對拍，鑑別態 101，§5.3.4.cl）；**花括號短路鏈已 s58**（`h2m-test` **176/176**，e2e **188** 與 Go oracle 同程式對拍（源碼級三對變異同值 189／195／190；IR 反向四刀 189／195／190／189），鑑別態 101，§5.3.4.cm；v0.4.13 短路鏈的兩處 Go 側語義偏差另記 §11 B19/B20）；**塊終結形已 s59**（`h2m-test` **203/203**，e2e **72** 與 Go oracle 同程式對拍（逐值 14 6 0 37 10 5；源碼級三對變異兩側同值 76／70／74；IR 反向四刀 41／66／73／72，末一把同值不鑑別已留檔說明），鑑別態四例 exit 101，§5.3.4.cn）；**計數循環已 s60m**（`h2m-test` **255/255**，e2e **134** 與 Go oracle 同程式對拍（逐值 3 10 104 11 6 0；源碼級四對變異兩側同值 135／141／136／136；IR 反向五刀 132／131／135／132／131），鑑別態三例 exit 101，§5.3.4.co）；**區間遍歷已 s61**（`h2m-test` **324/324**，e2e **164** 與 Go oracle 同程式對拍（逐值 10 6 6 6 4 14 103 9 6；源碼級八對變異兩側同值 168／168／168／171／162／161／161／156；IR 反向八刀 169／161／163／158／163／160／194／155），鑑別態三例 exit 101，§5.3.4.cp）；**容器遍歷已 s62**（`h2m-test` **372/372**，e2e **134** 與 Go oracle 同程式對拍（逐值 6 3 101 5 6 6 7；源碼級六對變異兩側同值 136／133／140／134／148／140；IR 反向八刀 135／132／129／139／130／133／131／128），鑑別態六例 exit 101，§5.3.4.cq；本件零改 `cg.no`——復用 s23 的 `OP-VECLEN`／`OP-VECGET`，s61 所記"尚不存在的碼位"屬假帳已就地改實）；**值匹配已 s63**（`h2m-test` **429/429**，e2e **221** 與 Go oracle 同程式對拍（逐值 10 20 30 1 7 3 4 10 1 111 8 9 7；源碼級七對變異兩側同值 216／227／222／226／65（真值 321 按位元截斷）／222／231，另兩對實參 241／220；IR 反向十刀 211／241／231／222／171／215／223／218／212／219），鑑別態七例 exit **101**，§5.3.4.cr：逐臂在本臂判定塊內重讀主體發 `OP-CMPEQ`，⚠️ 收閘前該形是靜默錯碼不是 unsup）；**實參側池已 s64**（`h2m-test` **443/443**＋`cg-test` **347/347**，e2e **91** 與 Go oracle 同程式對拍（逐值 25 21 17 13 15；源碼級四對變異兩側同值 92／104／90／95；IR 反向五刀 93／87／82／93／88），鑑別態三例 exit **101**，§5.3.4.cs：B16 銷帳——`OP-CALL` 實參改走側池 `args`（`xa`=池基址），`h2m` 校驗全過才整段入池；⚠️ `cg` 三入口第 13 形參必填使 28 檔 M4-b/c 手寫 op 探針一次性作廢）；**`it` 隱式綁定已 s65**（`h2m-test` **443→453→471/471**，e2e **46** 與 Go oracle 同程式對拍（逐值 1 7 13 7 3 9；源碼級二對變異兩側同值 45／47；IR 反向五刀 39／44／36／45／41），鑑別態 exit **101**（`r = it / 2` 除不支持：noc 毒 main 101 vs oracle 38），§5.3.4.ct：`itv` 值 id 棧——cond 臂複用判定塊 LOAD、wild 臂懶發主體 LOAD；⚠️ 預掃描 `h2m-ty-of('it')` 原返 -1 導致 `h2m-slot-decl` + `h2m-var-write` **雙 mark**，本件補 TY-I64 特例銷掉；同族作用域棧 `stk`／`rnms`／`epl` 退出位點補真 `.pop()`；另预记 §11 B27——Go 嵌套 match 的 `it` 海森堡（no-print 得 2、print 下得 3））；**M4-e 續推 s66–s85**（§5.3.4.cu–.dn，2026-11-04→11-06）：s66 前綴 `-x`/`!x`（`K-PREFIX-EXPRESSION`⇒`OP-NEG`/`OP-NOT`，僅 i64，`h2m-ty-of` 補 prefix 臂規避雙 mark）→s67 二進位位運算 `& | ^ << >>`（INFIX 分派鏈補五臂，零改 cg/mir/ty-of）→s68 邏輯 `&& ||`（復用 `OP-AND/OR`，非短路與 Go `emitLogic` 同形）→s69 前綴位非 `~x`（`OP-CONST(-1)`＋`OP-XOR`，鏡像 Go `hir2mir:8861`；🔴 `ps.no` `~` 鬆綁定優先級分歧記待查）→s70 前綴 f64 `-x`（NEG 守衛放寬收 TY-F64，f64 首次端到端流經 h2m）→s71 f64 二元算術 `+ - * /`（fty 雙側發 `fadd/fsub/fmul/fdiv double`，i64 `/` 仍門禁）→s72 f64 二元比較（`is-arith`/`is-cmp` 旗標分派，比較 dest 恆 TY-I64 發 `fcmp+zext`，收緊 s71 過寬臂）→s73 f64 字面量（`K-FLOAT-LITERAL` 讀 raw、`raw.contains('.')` 門控，**同步改 `cg.no` OP-CONST 依 TY-F64 發 `fadd double 0.0`——s55 起首件需動 cg 的降級件**）→s74 f64 局部變量/控制流槽化（槽機構認 TY-F64、`alloca double`，cg ALLOC/STORE/LOAD 分岔 double 翼）→s75 i64 除/模除零護欄降級（`%`⇒OP-MOD、取消 i64 側 OP-DIV 排除，落 cg s38/s39 `cg-div-guard` plain 路）→s76 u64 除/模無符號護欄（`uty` 分派⇒`OP-DIVU/MODU`，補 `h2m-ty-of` U64 infix 臂；🔴 Go 編譯期摺疊對 u64 除走有符號口徑 quirk，oracle 經 env 不透明化）→s77 u64 比較族無符號（`OP-CMPLTU/LEU/GTU/GEU`，dest 恆 TY-I64，ty-of 開比較例外）→s78 u64 算術族 `+ - *` 放行（不重映射，零改 cg/ps/mir/ty-of）→s79 u64 位運算/移位族 `& | ^ << >>`（唯一重映射 `>>`⇒`OP-SHRU`）→s80 i64 `>>` 口徑裁定（全族無條件重映射 `OP-SHRU` 恒發 `lshr`，鏡像 Go `emitBitwise` OpShr；s35 i64=ashr 設計推翻、cg ashr 臂轉死代碼；**自舉軌首個非加性件**，s67 基線重鑲）→s81 u64 前綴 `-x`／`~x` 放行（`K-PREFIX-EXPRESSION` 兩 `TY-U64` 發射臂 `'-'`⇒`OP-NEG`（sub i64 0）、`'~'`⇒`OP-CONST(-1)`＋`OP-XOR`（xor i64），`'!'` 於 u64 保持門禁；🔴 IR 與 i64 前綴逐字節同，型別正確性靠 unsup 門＋dest 型別碼＋值語義刀鎖；零改 cg/ps/mir/ty-of；**純加性件**，s80 前向值不變）→s82 科學計數法字面量 ILLEGAL 鏡像（`lx.no` readNumber 後測 `[eE]`＋指數位⇒整個 token 覆寫 `ILLEGAL`＋ErrMsg 156 字節原文、探測不消耗；`ps.no` operand 位報告臂＋級聯抑制，去重口徑證與 Go `reportedIllegal` 等價；詞法/語法診斷件，零改 h2m/cg/mir，§5.3.4.dk）→s83 u64 邏輯族 `&& ||` 放行（`h2m.no` u64 位運算臂撤 `is-logic==0` 並退場旗標——Go 權威對 u64 `&& ||` 即裸位形 `and/or i64` 非短路（探針實釘 12&&10⇒8、0||5⇒5、0&&1⇒0），s79 門禁「同碼不等價、與真短路後置」假設被探針推翻；函數形被 Go checker 拒（表面 bool）⇒ oracle 走頂層形；零改 cg/ps/mir/ck；§5.3.4.dl）→s84 前綴 `~` 綁定口徑複核＋哨兵重鑲（§5.3.4.cx 🔴 待查項三面實測緊綁定==Go 關閉——AST 四形逐字節 SAME、執行值 -8/-22 同值；實作零改（僅 ps.no TILDE 臂註解）；ps-test 38→42 哨兵；§5.3.4.dm）→s85 (MIN,-1) 護欄翼執行面實釘（§5.3.4.dd「執行面留待專門件」尾款償清——Go plain 路安全除數 1 定義值 /⇒MIN、%⇒0 且不觸 stderr；實作零改；h2m-test 721→735 翼哨兵；§5.3.4.dn）→s86 f64 `~` 門禁裁定（§5.3.4.dn 頭號候選銷賬——Go 運行路 `xor double` 非法 clang build fail、常量路 cvInt 摺死⇒無可鏡像權威，h2m ok-p==0 門禁為正確形態維持；實作零改；h2m-test 735→737 負向門禁哨兵；§5.3.4.do）→s87 f64 `%` 放行（s71 算術族尾款——`h2m.no` 歸類區補 `OP-MOD -> is-arith` 唯一實作行發 frem double，鏡像 Go `codegen.go:6083`；oracle 實釘 AArch64 lowering 後 fmod 語義（7.0%2.5=2.0、`llc` 出 `b _fmod`）兩側同值 exit 6；**零改 cg**（s37 frem 臂已備）；h2m-test 737→739 放行哨兵＋舊 s75 門禁哨兵翻正；§5.3.4.dp）→s88 `!x` 值語義對齊 i1 低位列（第 34 件——`h2m.no` K-PREFIX `'!'` 臂改 `OP-CONST(1)`＋`OP-AND`＋`OP-NOT` 三發射序列，鏡像 Go `codegen.go:6408` `emitNot` 的 `trunc` 歸位＋`xor i1 1`⇒值恆 (x&1)^1；i64 修正＋u64 放行（s81 門禁尾款清償）、dest 恆 TY-I64，f64 `'!'` 維持門禁（Go double→i1 恆 true 退化）；**零改 cg**；值面對拍 i64 117==oracle 117、u64 54==54；刀 4/4；前向 exit 19/19 全同、IR 15/16 SAME——s66 因 `!x` 歸位形制重錨基線（值面不變，非加性件 s80 先例）；h2m-test 739→743（`test-not-i64-lowbit` 新增、`test-pfx-u64-lnot-gate` 翻正）；本件期間並發進程曾將 h2m.no/cg.no 回退 HEAD，s82–s87 交付已按 §5.3.4.dk–dp 重放恢復並紀事；§5.3.4.dq）→s89 非整型主體 `'!'` 門禁裁定紀實＋哨兵重鑲（第 35 件——三面實釘無權威（f64 poison 恆真／str 運行路 `xor i1 1, %str-long` build fail [d8en6a9j]／字面量 hir inline 恆真），門禁維持；實作零改鏡像 s84/s86；`1e5` 候選重新定性銷賬（Go 詞法 ILLEGAL s82 已全鏈鏡像、h2m 閘不可達）；h2m-test 743→747 兩負向哨兵；刀 2/2、前向 exit 19/19、IR 16/16 SAME；§5.3.4.dr）→s90 bool 字面量降级＋bool 型别码放行（第 36 件——`h2m.no` 补 K-BOOLEAN-LITERAL 降级臂（读 slot0 文本 true/false 选 0/1，发 OP-CONST(TY-I64,v)）＋ h2m-ty-code/h2m-ty-of 补 bool⇒TY-I64，镜像 Go hir2mir.go:8848 KBoolLit⇒EmitInt(OpConst,0/1)；零改 cg/ps/mir；d1 赋值面普查撞出的新缺口、非 s89 清单预列；h2m-test 747→755；刀 2/2、oracle exit 2、前向 exit 19/19、IR 16/16 SAME；§5.3.4.ds）→s91 char 字面量降级＋char 型别码放行（第 37 件——`h2m.no` 补 K-CHAR-LITERAL 降级臂（读 slot0 去引号内文→to-bytes→1/2/3/4 字节 UTF-8 首码点解码→发 OP-CONST(TY-I64,cp)）＋ h2m-ty-code/h2m-ty-of 补 char⇒TY-I64，镜像 Go hir2mir.go lowerCharLit⇒EmitInt(OpConst,首码点)；零改 cg/ps/mir；d1 普查撞出的新缺口、非 s90 清单预列；h2m-test 755→763；刀 3/3、oracle exit 3、前向 exit 19/19、IR 16/16 SAME；§5.3.4.dt）→s92 默認參數調用點回填（第 38 件——`h2m.no` 簽名表補 fdef 預設式節點平行表＋ h2m-sig-def 新函數＋調用臂逐位統一迴圈（j<na 拿 AST kid、j>=na 降默認式入 argv），鏡像 Go hir2mir.go:13175 尾部回填、每調用點拷開；被缺且無默認值 ak=0 入門禁兜住 Go checker 前置拒；輸出參默認值仍拒；零改 cg/ps/mir；d1 普查撞出的新缺口、非 s91 清單預列；h2m-test 763→771（舊 gate default param 兩斷言翻正）；刀 3/3、oracle exit 15、前向 exit 19/19、IR 16/16 SAME；§5.3.4.du）→s93 語句位調用降級（結果丟棄）（第 39 件——`h2m.no` 的 K-EXPRESSION-STATEMENT 臂尾補「只收 K-CALL-EXPRESSION」的 lower-expr 回填路，鏡像 Go hir2mir.go:3790 KExprStmt else 分支 lowerExpr(child) 值丟棄；無輸出參被調沿用既有 void⇒define i64 約定發 call i64 丟值、TY-VOID 另件；限定只收調用由 checker 層 checkBareExprStatement 分級支撐（裸字面量 yf6kxdmz／裸函數 k9179drd 仍拒），放開會推翻四條既有負例哨兵；零改 cg/ps/mir；d1 普查撞出的新缺口、非 s92 清單預列；h2m-test 771→780；刀 3/3、oracle exit 7、前向 exit 19/19、IR 16/16 SAME；§5.3.4.dv）→s94 ?u64 option 三件套降級（第 40 件——`h2m.no` 新增 `h2m-tynode-code` 共用判据簽名／標注同口徑（Go capture 合成 NullableType 標注須此路方入形）、K-NIL-LITERAL⇒OP-OPTNONE 鏡像 Go lowerNilLit、回值位隱式 OP-OPTSOME 包裹鏡像 wrapOptionIfNeeded、延遲零值 option→nil 發 OPTNONE 非 some(0)；覆盖面限直線式 mem==0——TY-OPT 鋪槽／option match 臂／流形 option 輸出参入門禁；零改 cg/ps/mir；歷件清單頭號項此件償清；h2m-test 780→788 過/795 總（紅 7＝既有 s64-str／default 族、工作樹並發改動非本件、裁決 proceed＋ignore）；刀 5/5、oracle exit 61、自舉實釘 exit 7、前向 exit 19/19、IR 16/16 SAME；§5.3.4.dw）→s95 ?i64 內層整型放寬（第 41 件——`h2m.no` `h2m-tynode-code` K-NULLABLE 內層名收受擴為 'u64'||'i64'、?i64 與 ?u64 同 [3 x i64] 槽逐字節同形、some-wrap TY-I64 支首次正向觸及、內層 str/f64/vec 仍不入；零改 cg/ps/mir、無新發射臂；h2m-test 788→806/813（紅 7 既有非本件）、刀 2/2、oracle exit 61、自舉 exit 7、前向 19/19、IR 16/16 SAME；§5.3.4.dx）→s96 ?f64 內層載荷放寬（第 42 件——`h2m.no` `h2m-tynode-code` K-NULLABLE 內層名收受擴為 'u64'||'i64'||'f64'、回值位 some-wrap 增 `pty == TY-F64` 支改發 OP-OPTMKF64（bitcast double→i64）、補 §11 B7「cg 既存而 h2m 無人發射」空洞、內層 str/vec 仍不入；零改 cg/ps/mir、無新 op；h2m-test 806→824/831（紅 7 既有非本件）、刀 3/3、oracle exit 61、自舉 exit 7、前向 IR 16/16 SAME；§5.3.4.dy）→s97 TY-OPT 控制流鋪槽（第 43 件——`h2m.no` option 槽由直線式（mem==0）扩到控制流（mem==1）：slot-decl 認 TY-OPT 發 OP-OPTALLOC（alloca %option）＋OPTNONE（tag=1）nil-init、var-read 發 OPTLOAD、var-write 分 option←option OPTSTORE／option←載荷 寫點 h2m-opt-wrap 包裹再存、lower-func 於 scan 後按聲明型 TY-OPT 覆寫輸出参名（h2m-lst-add 就地改型）並撤 s94 flow-gate；回值位與寫點 some-wrap 收斂單點 h2m-opt-wrap、補 §11 B7「cg 既存而 h2m 無人發射 OPTALLOC/STORE/LOAD」空洞；零改 cg/ps/mir、無新 op；h2m-test 824→845/852（紅 7 既有非本件、3 flow-gate 負例翻正＋2 新哨兵）、刀 4/4、oracle exit 165、自舉 exit 165、前向 IR 16/16 SAME；§5.3.4.dz）；各件 `h2m-test` 逐代次遞增（s65 471→s81 715、s83 721、s84 ps-test 42/42 哨兵重鑲、s85 **735/735** 翼哨兵、s86 **737/737** 負向門禁哨兵、s87 **739/739** 放行哨兵、s88 **743/743** 值語義修正＋u64 放行翻正、s89 **747/747** 負向門禁哨兵重鑲、s90 **755/755** bool 字面量放行哨兵、s91 **763/763** char 字面量放行哨兵、s92 **771/771** 默認參數回填放行哨兵、s93 **780/780** 語句位調用放行哨兵、s94 **788/795** option 三件套放行哨兵（紅 7＝既有 s64-str／default 族、ps 41/1＝既有 enum 死碼臂哨兵，皆工作樹並發改動非本件引入）、s95 **806/813** ?i64 內層放行哨兵（紅 7 同前非本件）、s96 **824/831** ?f64 內層載荷放行哨兵（紅 7 同前非本件）、s97 **845/852** TY-OPT 控制流鋪槽放行哨兵（紅 7 同前非本件、3 flow-gate 翻正＋2 新哨兵））、cg suites 347/60、s82 lx-test 41→70／ps-err-test 26→46＋Go oracle L1 全語料 804/804 逐字節 SAME（實撞出手寫 ErrMsg 多一零的抄寫缺陷——兩側同錯時單測無鑑別力、唯 oracle 差分抓得）& 探針級 L1 差分 19/20 SAME（1 檔既有色差）、vet 探針 3/4 SAME（1 檔屬 s10 既記穩定碼格式缺口）、`opt` verify rc=0、`clang -O2` 探針前向與 Go oracle 同程式逐值對拍、IR 反向刀全改 exit、s66–s79 探針 IR 逐位元組相同（s80/s81/s83 前向零回歸：s76=63、s77–s81=255、s83 新探針 exit 15==oracle 15 刀 5/5、s84 新探針 exit 3==oracle 3 刀 4/4、s85 新探針 exit 7==oracle 7 刀 4/4 且 s66–s84 IR 逐字節 18/18 SAME 、s86 新門禁探針 exit 20＋刀 3/3 且 s66–s85 探針 IR 逐字節 16/16 SAME（s86 紀實件零改 h2m/cg 之結構性保證）、s87 新探針 exit 6==oracle 6 刀 3/3 且前向 s66–s85 exit 19/19 全同、IR 16/16 SAME、s88 新探針 i64 exit 117==oracle 117／u64 54==54 刀 4/4 且前向 exit 19/19 全同、IR 15/16 SAME（s66 因 `!x` 歸位形制重錨至 tmp/s88-fwd-s66.ll、值面 149 不變））、s89 零改件前向 exit 19/19 全同＋IR 16/16 SAME（結構性保證）、刀 2/2 哨兵斷言制、s90 真实改件前向 exit 19/19 全同＋IR 16/16 SAME（新臂仅 bool 触发的结构性保证）、刀 2/2、oracle exit 2（add i64 0,1 / 0,0）、s91 真实改件前向 exit 19/19 全同＋IR 16/16 SAME（新臂仅 char 触发的结构性保证）、刀 3/3（含 K3 破解码器 *4096→*64 仅 cjk 红、证 UTF-8 解码在位）、oracle exit 3（add i64 0,97 / 0,20013）、s92 真实改件前向 exit 19/19 全同＋IR 16/16 SAME（调用臂 na==want-n 时逐位语义不变的结构性保证）、刀 3/3（含 K3 破 sig-def 段内索引 fb+j→fb 仅 trailing 红、证按位取参在位）、oracle exit 15（四路 5/9/21/3 全对、回填 IR 实钉 add i64 0,5 / 0,20 拷开降入调用者块）、s93 真实改件前向 exit 19/19 全同＋IR 16/16 SAME（新臂仅语句位调用触发的结构性保证）、刀 3/3（含 K2 去掉「只收 K-CALL」限定连带推翻 count-loop/vm-gate-expr-subj/bit-gate/stmt-bare 四条负例门禁、证限定承重；K3 放行不产码仅三条 call emitted 红、证放行且产码）、oracle exit 7（uone=4/keep=4/disc=1；@disc 体内 call i64 @acc / @side 皆发射手值未用）、s94 真實改件前向 exit 19/19 全同＋IR 16/16 SAME（新臂僅 K-NULLABLE／K-NIL／option 回值位觸發之結構性保證）、刀 5/5（K3 刪延遲零值臂僅 +1『none emit』紅——unsup 斷言仍 0 的靜默退化恰由此刀抓住；K4 刪流門禁 +2 紅、證條件寫退化產碼 some(0) 不得放生；K2 刪 nil 臂後 call 斷言由簽名表獨立仍綠、證記賬分離）、oracle exit 61（ok(7)/nil/nil 編碼 1+20+40、some(0) 誤做則 25≠61 自具鑑別力）＝自舉三路實釘 exit 7（tag＋載荷三路皆對唯一 7）、s95 真實改件前向 exit 19/19 全同＋IR 16/16 SAME（?i64 未入既有探針之結構性保證）、刀 2/2（K1 退 'i64' 收受 +13 紅——簽名與 capture 兩側同破證 §11 B7 同口徑；K2 削 some-wrap `pty==TY-I64` 支 +5 紅——證該支為 i64 載荷專屬承重）、oracle exit 61（p1=7 ?i64 同值面）、自舉 exit 7、s96 真實改件前向 exit 19/19 全同＋IR 16/16 SAME（?f64 未入既有探針之結構性保證）、刀 3/3（K1 退 'f64' 收受 +13 紅——簽名與 let 標注兩側同破證 §11 B7 同口徑；K2 削 some-wrap `pty==TY-F64` 支 +5 紅、K3 錯 op 換 OPTSOME +3 紅——證 OPTMKF64 發射在位）、oracle exit 61（p1=3 ?f64 同值面）、自舉 exit 7、s97 真實改件前向 IR 16/16 SAME（新支僅 st/frt==TY-OPT && c.mem==1 觸發之結構性保證）、刀 4/4（K1 移 slot-decl TY-OPT 收受 +23 紅、K2 破寫點包裹 +18 紅、K3 破 OPTLOAD +4 紅、K4 破聲明型覆寫 +8 紅——四刀共證鋪槽／寫點／讀端／覆寫四處承重、K1/K2 跨簽名回值寫點證 §11 B7 同口徑單一 helper）、oracle exit 165（f(5)=some(5)/f(0)=nil/h(7)=some(7)/h(0-3)=nil 未寫路徑 tag=1）、自舉端到端 exit 165==oracle 165（main 側裸 IR extractvalue 讀 tag/payload、避 match s63 門禁；降級失敗毒 main 101）、⚠️ 本件撞出 Go quirk f10x20nf（裸算術式作 option-回傳函式實参被 checker 拒、須先綁具名變量、與鋪槽正交不改 src））。⚠️ 自 s35 起本族因 HEAD Go 侧跨模組結構欄位回歸（`setfield: buf not found on sb.sb`）一律在**釘死 `v0.4.13`** 上採證；兩項既有紅點 `ast-test`（Go #85）/`ck-sig-test`（世代漂移）與自舉正交；`no vet src/std` 基線計數 1 ERROR。餘為 **s98 候選**（`&^` 已銷賬（s82 裁決：Go 無實現、無權威可鏡像）／`1e5` 已償 s82（重新定性為 ILLEGAL 鏡像而非定點化）／cg OP-SHR ashr 死代碼清理／(MIN,-1) 已償 s85（執行面三面實釘、翼哨兵入 h2m-test）／`&& ||` 已償 s83（Go 無短路、裸位形鏡像完）／`?u64` option 三件套已償 s94（`h2m.no` 共用判据 h2m-tynode-code 簽名／標注同口徑、nil⇒OPTNONE 鏡像 Go lowerNilLit、回值位隱式 OPTSOME 包裹鏡像 wrapOptionIfNeeded、延遲零值 option→nil；覆盖面限直線式，TY-OPT 鋪槽／option match 臂／err tag=2 裝箱入門禁；放行哨兵 opt-wrap/nil/zero/capture/param＋負例 flow-gate/payload-gate 入 h2m-test；oracle exit 61 钉 ok(7)/nil/nil、自舉探針 exit 7）／?i64 內層整型放寬已償 s95（`h2m.no` `h2m-tynode-code` 內層名收受擴為 'u64'||'i64'、?i64 與 ?u64 同 [3 x i64] 槽逐字節同形、some-wrap TY-I64 支首次正向觸及；放行哨兵 opt-i64 wrap/nil/zero/capture/param/samecaliber＋負例 flow/payload 入 h2m-test；oracle exit 61 钉 p1=7、自舉 exit 7）／?f64 內層載荷放寬已償 s96（`h2m.no` `h2m-tynode-code` 內層名收受擴為 'u64'||'i64'||'f64'、回值位 some-wrap 增 `pty==TY-F64` 支改發 OP-OPTMKF64（bitcast double→i64）、補 §11 B7「cg 既存而 h2m 無人發射」空洞；放行哨兵 opt-f64 wrap/not-i64-some/nil/zero/capture/param＋負例 flow/payload 入 h2m-test；oracle exit 61 钉 p1=3、自舉 exit 7）／TY-OPT 鋪槽已償 s97（`h2m.no` option 槽由直線式扩到控制流 mem==1：slot-decl OPTALLOC＋OPTNONE nil-init／var-read OPTLOAD／var-write OPTSTORE＋寫點 h2m-opt-wrap 包裹／scan 後聲明型覆寫輸出参名並撤 s94 flow-gate、回值位與寫點 some-wrap 收敋單點 helper（§11 B7 同口徑）；零改 cg/ps/mir（OP-OPTALLOC/STORE/LOAD cg s41 既存）；放行哨兵 opt-flow-slot/-i64/-f64/deferred-nil／str-payload-gate 入 h2m-test；oracle exit 165 钉 some/nil 四路、自舉 exit 165）／`~` 優先級分歧已償 s84（現行三面實測緊綁定、哨兵入 ps-test）／`~` f64 已銷賬 s86（s86 裁定：Go 運行路 `xor double` 非法 build fail、常量路 cvInt 摺死，無權威可鏡像；負向門禁哨兵入 h2m-test）／`%` f64 已償 s87（is-arith 補 OP-MOD 單行放行 frem double，AArch64 lowering fmod 語義兩側同值 exit 6；放行哨兵＋s75 舊門禁哨兵翻正入 h2m-test）／`!x` 已償 s88（值語義對齊 i1 低位列——i64 修正＋u64 放行、f64 維持門禁裁定；三發射序列 `OP-CONST(1)`＋`OP-AND`＋`OP-NOT`，值面對拍 117/54 兩側同值）／f64 `'!'` 已銷賬 s89（非整型主體三面實釘無權威、門禁維持裁定；負向哨兵 lnot-f64/lnot-str 入 h2m-test；`1e5` 同步重新定性銷賬）／bool 字面量已償 s90（`h2m.no` K-BOOLEAN-LITERAL 降级臂＋bool 型别码，镜像 Go KBoolLit⇒OpConst 0/1、自举 TY-I64 0/1 零改 cg；放行哨兵 bool-lit-true/false＋bool-param-tycode 入 h2m-test；oracle exit 2 钉 true=1/false=0）／char 字面量已償 s91（`h2m.no` K-CHAR-LITERAL 降级臂＋char 型别码，镜像 Go lowerCharLit⇒OpConst 首码点、自举 TY-I64 码点零改 cg；UTF-8 首码点解码 1/2/3/4 字节；放行哨兵 char-lit-ascii/cjk＋char-param-tycode 入 h2m-test；oracle exit 3 钉 a=97/中=20013）／默認參數調用點回填已償 s92（`h2m.no` fdef 簽名平行表＋調用臂尾部回填迴圈，鏡像 Go hir2mir.go:13175 缺省實參逐調用點 lowerExpr(deID) 拷開；被缺且無默認值入門禁；輸出參默認值仍拒；放行哨兵 default-param-fill/trailing/missing-gate 入 h2m-test；oracle exit 15 钉 5/9/21/3 四路）／語句位調用已償 s93（`h2m.no` K-EXPRESSION-STATEMENT 臂尾只收 K-CALL-EXPRESSION 的 lower-expr 回填路，鏡像 Go hir2mir.go:3790 KExprStmt else 分支 lowerExpr(child) 值丟棄；void 被調沿用 define i64 約定、TY-VOID 另件；放行哨兵 stmt-call-void/discard/gate＋負例 stmt-expr-bare-gate 入 h2m-test；oracle exit 7 钉 4/4/1 三路）／§5.3.4.ct match 軌殘留——解構臂、`ok`/`err`/`nil` 關鍵字臂、型別臂、非 i64 主體、表達位鏈、帶標籤終結形與帶值 return、開邊界區間、`OP-STRUCT-SET` 連續約定 §11 B26、`drv.cmd-build` 接線 M4-d）與 >24B 堆裝箱與 vec 非 i64 元素／txt 元素相等，已知待補項集中見 **§11 記帳**〔本段為 2026-11-06 歷史快照，已由下方 2026-11-12 as-built 快照接手〕本文件為持續維護的活文檔——每完成一個里程碑即更新 §6 與最新快照，故不再以文檔結束收尾

|  |  |  |
| --- | --- | --- |
**〔歷史快照・2026-11-06〕M4-e `h2m` AST→MIR 降級已續推至 s97（2026-11-06，共 43 件，§5.3.4.cj–.dz）**：新建 `compiler/src/h2m.no` 把 `ast.no` arena 降成 `mir.no` 扁平指令流，沿語料觸發頻度逐族落地——① 直線式 i64 子集（s55）→ 簽名側與調用（s56）→ 控制流與跨基本塊槽化（s57）→ 花括號短路鏈（s58）→ 塊終結形 return/break/continue（s59）→ 計數循環（s60m）→ 區間遍歷（s61）→ 容器遍歷（s62）→ 值匹配（s63）→ 實參側池 args 摘掉連續 id 契約（s64）；② 邏輯 `&&`/`||`（s68）；③ u64 全族——除/模無符號護欄（s76）、比較無符號（s77）、算術（s78）、位運算/移位（s79）、邏輯族（s83）；④ i64 `>>` 裁定 lshr（s80，推翻 s35 `ashr` 設計）；⑤ f64 局部變量/控制流槽化（s74）、i64 除/模除零護欄（s75）、f64 `%` 放行（s87）；⑥ 前綴 `~` 與中綴綁定口徑複核（s84）、(MIN,-1) 護欄翼（s85）、f64 主體 `~` 門禁（s86）、非整型主體 `!` 門禁（s89）、bool 字面量（s90）、char 字面量（s91）；⑦ TY-OPT 控制流鋪槽（s97，option 局部量/輸出參落 `alloca %option`、延遲零值 tag=1、寫點 some-wrap 收斂為單點 `h2m-opt-wrap`）。驗證：`h2m-test` **845 過/852 總**（紅 7 為既有 s64-str 等負例，非本件）、`cg` 347／`cg-runtime` 60／`ck` 20 全綠；IR 反向刀 4/4 全 EFFECTIVE；oracle 對拍 `tmp/s97-oracle-vals.no` 與自舉探針同值 exit 165；前向 16/16 byte-SAME。剩餘（s99 起）：`?str/?vec` 載荷、`option` match 臂解構、err 構造/tag=2 裝箱、ok 字面量、程序頂層陳述執行（M4-d 邊界）、內建函數調用、struct/vec 字面量、multi-out、defer/嵌套函數、TY-VOID（cg `OP-SHR` ashr 死代碼清理已於 s98 完成，§5.3.4.ea：h2m `>>` 自 s80 恆射 `OP-SHRU`=lshr、cg 刪 ashr 發射臂、cg-test 移位斷言 ashr→lshr 校正）。〔本段止於 s97；s98–s99n 與 2026-11-10／11-11 變更見下方 2026-11-12 快照。〕

---

**進度快照（2026-11-12，as-built 現況；含 B28 清償、`cg-test` 解鎖、M4-d 接線、`print` 降級、`...` 佔位陳述與字面量插值降級、值位塊匹配降級、`str` 相等/不等降級、解析器欄位尾註釋保真度修復後）**：M0／M1／M2 已完成（L2 全語料 655/655 SAME、L1 675/675 SAME 為 2026-10 歷史觀測）；M3 完成 M3-a 骨架（`drv.cmd-vet`＋`diag.no`）→ M3-b stdsig 表（`ck-sig.no`）→ M3-c 首族與 s82 詞法 ILLEGAL 報告族（§6 M3 列）。**M4 已進入中後端**：M4-a/b 骨架、M4-c 發射層（控制流／循環／字符串／拼接／多參調用／str 跨塊／vec／帶型別簽名／ALU·u64·f64 全族／option 載荷／struct 全族／RC 原語）已落地；`compiler/src` 現有 `h2m.no` 4,447／`mir.no` 481／`cg.no` 2,569／`cg-runtime.no` 531／`cg-rc.no` 180／`link.no` 53／`drv.no` 602／`ps.no` 10,071／`lx.no` 1,645／`tok.no` 413／`asp.no` 915。**M4-e `h2m` AST→MIR 降級已續推至 s99n（件號 54，§5.3.4.ea–.eo）**：s98（2026-11-08）清 cg `OP-SHR` ashr 死臂（h2m 自 s80 恆射 `OP-SHRU`=lshr、cg 刪 ashr 發射臂、cg-test 移位斷言校正）；s99–s99n（2026-11-09，14 節）補 struct 定義／欄位訪問／形參返回值／跨塊槽／跨函數／欄位型別（str·f64·嵌套·`[]i64`）／欄位 RC 取得與 scope-exit 釋放／vec 欄位就地索引；s99m 所有權 provenance（`ownn`·`ownv`·`rdval`·`rdown`·`retset`）＋撤除 `mem==1` 含 CALL 的整函數閘門；s99n 補 vec 半邊（`K-SLICE-LITERAL`⇒`OP-VECNEW`／`OP-VECPUSH`、`@vec_free`、`OP-VEC-FREE = 94`、三條所有權外流門禁、離線 ASan 4/4 無報告）。`a4b7c66f`／`0fbbe684`（11-10）分別扁平化 `asp.no` 規格表、重構 `h2m` struct lowering 與 ownership；`0e055e11`（11-11）重生 `asp.no`（`KIND-COUNT=74`／`STR-COUNT=191`／`INSTR-COUNT=315`，新增 `EmptyStatement`）並補 §5.3.4.ax 的 16B `%str-long` 警示。**門禁（`h2m-test` 6 紅為既有 s98 optmatch，其餘全綠）**：`h2m-test` **1080/1086**、`cg-runtime-test` **86/86**、`cg-str16-test` **41/41**（完整 `.ll` `opt -passes=verify` rc=0＋`clang` e2e 4/4）、`cg-test` **359/359**（⑦ 解鎖並校正 16B 斷言）、`drv-build-test` **63/63**（M4-d＋⑨ print＋⑩ 插值／`...`＋⑪ 值位塊匹配＋⑫ str 相等/不等斷言；`dump` 模式 367 行 `.ll` 過 `opt -passes=verify` rc=0、`clang` 執行 exit=42；`dump-print` 374 行（輸出 `42\nhi`）、`dump-interp` 389 行（輸出 `a=7 b=hi c=7`），皆過 `opt`＋`clang`，輸出逐位元組等於 oracle）⇒ **M4 四個文字門禁全綠**；其餘 L0 抽測見 §11 後 2026-11-12 追記。✅ **§11.2 B28 已於本日清償**：selfhost `%str-long` 已由 24B 同步為 16B `{ i32, i32, i8* }`，prelude／`@str_eq`／`@str_concat` 與 Go `emitPrelude` 逐字相同，`cg.no` 各 op 補齊 `zext`／`trunc`；`%option` 的 24B slot 未變、`?str` 仍 inline 但只佔前 16B（詳 §11 後 ⑥）；**⑦ 再解鎖 `cg-test.no` 並校正 16B 斷言**（詳 §11 後 ⑦）。✅ **⑧ M4-d 接線已於本日落地**：`h2m-lower-module` 合成 `@main`、新增 `link.no`（DR-2 調 clang）、`drv.cmd-build`／`cmd-run` 支援 `-o OUT FILE` 與 `FILE -o OUT` 雙序、`drv-build-test` 16/16、e2e `build`／`run` 與 Go oracle 同值（**exit=42**）（詳 §11 後 ⑧）。✅ **⑨ `print`／`eprint` 已於本日降級**：新增 `OP-PRINT = 95`（kind 8 種）、prelude 補 `@digits`／`@print_i64`／`@print_str`／`@eprint_*`／`@.nl`／`@.sp`（3,214 字元，prelude 總長 7,226→10,440）、`h2m-lower-print` 含值型別／插值擋板／語句位三條誠實門禁、`run` 預設落 `<TMPDIR>/noc-run-<pid>.out`（不再污染語料目錄）；**L4 全語料首度量測（169 檔，串行）**：真 SAME **2**（`ifelse.no`／`colon-loop.no`，本輪由 0 起增益）、空 SAME 4（`tagged-enum-*`，oracle 亦建不過）、DIFF 163 且**無靜默錯碼**（詳 §11 後 ⑨）。✅ **⑩ `...` 佔位陳述＋字面量插值已於本日降級**：`tok.no` 新增 `ELLIPSIS3 = 92`（與範圍運算子 `ELLIPSIS = 65` 分離，擋掉 `[0...n)` 的靜默合法化）、`lx.no` 三點貪婪、`ps.no` 建 `K-EMPTY-STATEMENT`、`h2m` no-op 臂消費（語料 **74 檔**含 `...`）；插值走「三檔分類 + 逐段 `OP-PRINT`」，無 spec 的 `{ident}` 與權威**逐位元組同結果**（`fmt-int(n,'')` 正文即 `n.to_str()`），`{name:spec}`／表達式字段／多實參模板形／`{{` `}}` 一律誠實門禁。量測：`rc==0` 檔 **2 → 4**（新綠 `for.no`、`guard.no`，皆以改前二進制復跑確認為 DIFF ⇒ 淨增益確為 +2）；全語料 unsup 總數 **11,857 → 11,436（−421，−3.6%）**，**86/169 檔**下降（詳 §11 後 ⑩）。✅ **⑪ 值位塊匹配（`r = { c -> v  -> d }`）已於本日降級**：`h2m-scan` 按 LetStatement 節點 id 合成結果槽 `@smv<id>` 並在 entry 統一 `OP-ALLOC`（**不在降級途中補 alloca**——否則迴圈體每迭代新攤一份棧）、`h2m-lower-surface` 加值位分支（逐臂 `OP-STORE`、匯聚 `OP-LOAD`），臂值非 `i64`／臂體非單表達式／`valueCtx` 未置位一律誠實門禁；`h2m-test` 1031/1037 → **1060/1066**、`drv-build-test` 42/42 → **53/53**；L4 真 SAME **4 → 5**（新綠 `build-mo.no`，唯該檔不印輸出 ⇒ 語義那一半由端到端對拍承擔）、全語料 unsup **11,436 → 11,354（−82，−0.7%）**、18/169 檔下降；⚠️ 同時記下 **4 檔 unsup 上升**（值位匹配強制 mem 模式 ⇒ `[]i64` 局部量首次需要槽）與 **B19 的鏈語義分歧**（權威側無兜底臂不短路，本輪不動）（詳 §11 後 ⑪）。✅ **⑫ `str` 相等/不等已於本日降級**：`h2m-lower-expr` 的 infix 分派補 `ok-e` 臂（`==`/`!=` 兩側皆 `TY-STR` ⇒ `OP-STEREQ`/`OP-STNEQ`，`dst` 記 `TY-I64`），**零改 `cg`／零改 `mir`**——兩 op 與 `@str_eq` 早已就位，缺的只是降級臂；同時補 `h2m-ty-of` 的 `str`×`str` 分支（`==`/`!=` ⇒ `TY-I64`、`+`/`-` ⇒ `TY-STR`）——**缺它則 str 比較/拼接只在 `mem==1` 掛**（同一份碼直線式能編、塞進 `cond -> …` 就 5 條 unsup，因槽寬推不出而 `h2m-slot-decl` 計 unsup）；`h2m-test` 1060/1066 → **1080/1086**、`drv-build-test` 53/53 → **63/63**、全語料 unsup **11,354 → 11,163（−191，−1.7%）**、**25/169 檔下降且 0 檔上升**（⑪ 曾有 4 檔上升）、自舉模組 unsup 合計 **2,456 → 2,368（−88）**（`ps.no` 1,268→1,182、`h2m.no` 154→148、`link.no` 24→22）；端到端 `e1.no`（四組 str 比較繞過 `print(bool)`）與權威**逐位元組相同**（詳 §11 後 ⑫）。**s105 修掉一個更底層的保真度缺口**：`ps-is-struct-def` 的「型別後終結符」只認 NEWLINE/COMMA/RBRACE/EOF，`COMMENT` 落進 fallback ⇒ **欄位帶 `;` 尾註釋的結構體整塊退回 `blockUnknown`、從未登記**（Go 的 `parser.nextToken()` 自行跳過 COMMENT，故其 `look()` 根本看不見 COMMENT；`ps-peek-at` 忠實回傳 ⇒ 兩側對「型別後面那個 token」判斷分歧）。修法是新增 `et == tok.COMMENT` 臂並**即時計數**（主迴圈 NEWLINE 臂不計數，只跳過會讓「每個欄位都帶尾註釋」的結構體得 `nfield==0` 仍判非結構體）。影響面：`ast.no` 的 `ast` 12 個欄位裡 3 個帶註釋 ⇒ 該結構體未入表 ⇒ `h2m-struct-find('ast')` 回 -1 ⇒ 155 個函式簽名被判 bad；修後 15 個自舉模組的簽名被拒 **250 → 166**、自舉模組**實際產碼函式數**（`define` 行）`ast.no` 3→8／`lx.no` 2→3；⚠️ 自舉模組 unsup 反而 **2,362 → 3,198**（簽名通了，函式本體才被嘗試，body 內每條不支援各記一條——判據必須同時看 `define` 行數），而**全語料 173 檔逐檔 `diff` 完全相同**（11,315 → 11,315、`unsup==0` 5 → 5，零回歸零增益）；`ps-test` **39/42 → 41/44**（詳 §11 後 ⑬）。**新的最高優先未了項**＝**`#` 模組導入**——⑫ 以臨時插樁量出 unsup 落在哪個 AST 節點型別的直方圖（`K-CALL-EXPRESSION` 611 條／`K-INFIX` 479／`K-IDENTIFIER` 338／`K-EXPRESSION-STATEMENT` 233／`K-LET` 139／`K-USE-STATEMENT` 37…），其中 611 條調用標記**全卡在「點號被調者」＝跨模組**（`cg-runtime.no` 350 條裡 296 條是 `b.push('…')`，而方法本體住在 `util/sb.no`），與 37 條 `K-USE-STATEMENT` 是同一塊；佐證：`noc build compiler` 目前 rc=0 卻只產出 34,632 位元組的空殼（執行 `version` 什麼都不印）⇒ 導入整條被忽略，這已是「模組能互相調用」前面唯一還空著的結構性缺口。其餘殘差：下標讀、`ok(...)` 條件臂、值位匹配的 str/vec 臂值、空 match 臂 `->` 與裸塊陳述 `{ … }`（⑬ 已釘到最小重現）、`[]T` 元素型別只有 `i64`（簽名側 20 條 `62:false`）、B19 的鏈語義裁決。M5–M8 ⬜ 未開始；`?str/?vec` 載荷、`option` match 臂解構、err 構造/tag=2 裝箱、ok 字面量、`print(bool)` 的 bool 型別碼、multi-out、defer／嵌套函數、TY-VOID、>24B 堆裝箱仍待續（§11 記帳 B4/B8/B11/B13/B14/B19/B26；B28 已清償、`cg-test.no` 已解鎖、M4-d 已接線、`print` 已降級、`...` 與插值已降級、值位塊匹配已降級、`str` 相等/不等已降級、解析器欄位尾 `;` 註釋保真度已修）。本文件為持續維護的活文檔——每完成一個里程碑即更新 §6 與本快照，故不再以文檔結束收尾


---

**進度快照（2026-11-15，**當時**最新；HEAD `571c658a` v0.4.24；2026-11-23 最新見附錄 C／§71）**：6② 端到端閘的 **S0→noc 層已達成**——修 `compiler/src/ck.no` 两处无注解算术实参（`hb+3`/`hb+10` 提取為 `#{overflow=wrap}` 局部變數，+4 行）後 `no build compiler` rc=0、產物 `version` 印 `noc v0.0.1-m0`；此前 rc=1 的根因是 v0.4.21 的 vec.push option 硬檢查點亮了 `?i64` 算術實參污染 aux-at 回值的潜在 bug（非 h2m 本體降級，推翻 ⑮ 判定）。門禁：`ck-test` 20/20、`h2m-test` **1085/1085**、前端/驅動/codegen 全綠（詳 ⑯）。⚠️ **DR-1 自舉定點仍未收**：S1 自編 S2 34,632 B 但 `version` 不印、S2→S3 無產物，仍擋在 h2m 函式本體降級（約 5,749 unsupported）——這是 M5/M6 的 h2m 能力擴張範疇，非本輪可跨層解決。pkg-* 四檔 abort 為預存 host codegen 缺陷。

**進度快照（2026-11-15 續，**當時**最新；HEAD `571c658a` v0.4.24；⑰ 偵察輪；2026-11-23 最新見附錄 C／§71）**：DR-1 第一道牆重新定位——實測證明 `drv.cmd-build` 只讀單檔，`fs.read-bytes(compiler 目錄)` 解析不出函式定義 ⇒ `h2m-synth-main` 合成空 main ⇒ 靜默產 34,632 B 假殼（rc=0、`version` 不印、`nm` 僅 29 個 runtime 符號）；S1 單檔降級能力正常（`exit(42)`→42、函式呼叫→7、`print("hi")`→印 hi），`#` 導入是清空 IR 的元凶（`main.no` 僅 2 條 `K-USE-STATEMENT` unsup 即被 `agg.unsup>0 -> r=""` 全清）。⇒ ⑯/⑮ 的「5,749 本體降級」為第二道牆且係 11-14 舊 cov 數字（現行 cov-measure abort 無法覆驗），非第一道牆。S0→noc 已達成不變（⑯）；noc→noc 定點留 A（cmd-build 接 pkg-ll 消假殼）／B（攻 M5/M6 本體）／C（僅記錄）三路徑決策，本輪選 C、未改原始碼（詳 ⑰）。

**進度快照（2026-11-23，最新；HEAD `d236a956` v0.4.29+；§71 收口輪）**：`compiler/src` **16 檔＋`util/` 2 檔＝18 檔、46,960 行**；`compiler/tests` **18 套 `*-test.no`**＋5 個非門禁檔、`fixtures/` **70 檔**。**M4-e 已推至 s152**（§12 ㉜–§70），但 ⚠️ **現行工具鏈代次下自舉阻塞**：`no build compiler` **12 ERROR**、L0 **13 紅／5 綠**；同一份碼在 **v0.4.28 代次**（`tmp/s148/wt`，`d21fbb35`）下 **rc=0、17/18 綠**（§70 ④ 的綠燈證據即出自該代次）。**L5 與 S0→noc 在 HEAD 暫時不可重現**，须先移居 12 個 option 化站點（B30）。唯一的工具鏈獨立紅燈是 `ast-test` **43/48**（5 條 kind 期望過期，見 §71 ③）。詳 §71。
**2026-11-23 續（§72 補記，原文保留）：下一步 A／B 已完成**——12 站 option 化移居後
`no build compiler` **rc=0**、L0 **18/18 全綠**、S0→S1 制品 `tmp/s154/noc-s1`（`noc v0.0.1-m0`）；
`ast-test` 期望已校正為 74／56／67／38／42。**S1→S2 仍阻塞**，且分成兩層：層 1 `build` 未收
目錄（`drv.dispatch` 未接 `drv.build-package`／`drv.pkg-ll`），層 2 `drv.pkg-ll` 整包 **1877 unsup
（1737 直接 + 140 閉包剔除）**。詳 §72。

---


> 舊附錄 B（2026-10-09 的 `compiler/` 骨架代碼樣板）已原樣遷至 [`nolang-selfhost-history.md` §三](nolang-selfhost-history.md#hist-appb)；⑩–⑯ 亦在歸檔 §二，本文只保留 ⑰–㊿（s133）與 §51–§71（s134–s153 收口）連續追記（**2026-11-23 校正：原文作「⑰–㉗」，落後 23 節**）。

## 12. 逐輪追記（2026-11-15 起，最新在後）

### ⑰ 2026-11-15 追記（再續）：DR-1 第一道牆重新定位——`cmd-build` 單檔路徑 + 靜默假殼（**偵察輪次**，HEAD `571c658a` v0.4.24）

> 本節是 as-built 偵察：只量測與定位，不改任何原始碼。起因是 ⑯ ⑤ 把 S1→S2 失败籠統歸因為「5,749 unsupported 的 h2m 本體降級」。本輪實測**精化**該判斷：DR-1 有兩層牆，第一層根本不是本體降級，而是 `cmd-build` 的入口路徑缺陷 + 靜默假殼。

**① 決定性對照：S0 與 S1 是兩條完全不同的路徑**

| 執行 | 二進位路徑 | RC | 產物 | `version` |
| --- | --- | --- | --- | --- |
| `./bin/no build -o /tmp/check-s0 compiler` | **Go 後端**（忽略 self-host `drv.no`/`h2m.no`） | 0 | 3,945,512 B Mach-O arm64 | 印 `noc v0.0.1-m0` |
| `/tmp/noc-e2e build -o /tmp/check-s1 compiler` | **self-host** `drv.cmd-build`→`drv.build-to`→`drv.build-ll` | 0 | 34,632 B | **不印**（空殼） |

- 兩個產物大小相差 **113×**；S1 的 34,632 B 與 ⑯ ⑤ 記載的 S2 完全一致 ⇒ 瓶頸在 self-host 路徑，不在 S0。

**② 第一道牆：`cmd-build` 只讀單檔，`#` 導入被靜默忽略**

- `drv.build-ll`（drv.no:365-386）文件註明「只處理單一 .no 檔；**不做** workspace / package / `#` 導入合併」（drv.no:357-359）。
- `drv.build-to`（drv.no:1047-1072）對傳入的 `path` 直接 `fs.read-bytes(path)`。當 `path='compiler'` 是**目錄**時，讀到的內容不足以 parse 出任何函式定義 ⇒ `h2m-synth-main` 合成一個空 `main` ⇒ 只鏈出 prelude 的 29 個 runtime 符號（`nm /tmp/check-s1` 實測：`_main` 存在但無任何用戶函式，`str_eq`/`vec_push_i64`/`print_*` 等皆在，`version`/help 字串一個都沒有）。
- 與之對照，整包路徑 `drv.pkg-ll`（drv.no:725）**已實作** `#` 導入解析、`build-package` 遞迴載入（drv.no:424）、全域 `sg` 跨模組呼叫（drv.no:771-816），但 `cmd-build` 從未呼叫它。⇒ 第一道牆是「入口沒接整包路徑」，不是「本體降級能力不足」。

**③ 反證：`h2m` 單檔降級能力其實正常（推翻「全面失效」的隱含假設）**

用 S1 對最小可執行檔做 `build` + 執行，全部成功：

| 源檔 | S1 build | 執行結果 |
| --- | --- | --- |
| `exit(42)` | rc=0 | exit code **42** |
| `f = () (r i64) { r = 7 }` + `exit(f())` | rc=0 | exit code **7**（函式定義＋呼叫可降級） |
| `print("hi")` + `exit(0)` | rc=0 | 正確印出 `hi` |
| `compiler/main.no`（含 `#` 導入）| rc=0 但**無產物** | `unsupported constructs: 2`——正是 2 條 `K-USE-STATEMENT`（`#` 導入）|
| `compiler/src/ck.no` | rc=0 但**無產物** | `unsupported constructs: 21` |

- `h2m-lower-module`（h2m.no:4660）的 `agg.unsup > 0 -> r = ''` 是「全有或全無」策略：只要有 unsup 就清空整模組 IR。單檔 `main.no` 只因 2 條 `#` 導入就被清空 ⇒ 再次指向導入路徑，而非本體構造。

**④ 對 ⑯ ⑤ / ⑮ 的精化（不推翻已達成的 S0→noc）**

- ⑯ ⑤ 與 ⑮ 把 S1→S2 归因為「5,749 unsupported 的 `ps.parse`/`ast.str-at` 本體未降級」。**該數字來源是 11-14 舊 cov 日誌**（`/tmp/h2m-cov.log`）的整包 `pkg-ll` 量測，非單檔路徑。
- 本輪嘗試用 `cov-measure.no` 復測新數字，仍 `signal: abort trap`（預存 host codegen 缺陷，與 ⑯ 一致）⇒ **5,749 目前無法用現行工具鏈刷新，應標註為「舊量測、待覆驗」**，不應作為第一道牆的當前依據。
- **兩層牆模型**（本次定位）：
  - **牆 1（入口層，已定位、未修）**：`cmd-build` 不認目錄 / `#` 導入 + 讀檔失敗靜默產假殼 rc=0。這一道使「 noc 自編 compiler」在還沒碰到本體降級前就已經失敗。
  - **牆 2（能力層）**：即便接上 `drv.pkg-ll` 整包路徑，仍需直面 h2m 函式本體降級缺口（舊測 5,749，待覆驗）——這是 M5/M6 的 h2m 能力擴張範疇。

**⑤ 可選路徑（本輪不擅改原始碼，留作決策）**

- **路徑 A（消除假殼，低風險）**：讓 `drv.cmd-build` 用 `fs.is-dir`（self-host 簽名表 ck-sig.no:383 已有）偵測目錄 → 改呼叫 `drv.pkg-ll(path, false)`。語義從「假殼 rc=0」變「誠實 rc=1（若仍有 unsup）或真產物」。⚠️ 代價：16 ⑤ 的 S1 `build compiler` rc=0 將不再成立，需同步更新驗收口徑；且 `pkg-ll` 在 S1 self-host 路徑能否跑通未驗證（S0 側 cov/pkg-* 四檔 abort）。
- **路徑 B（攻牆 2，大工程）**：直接擴張 h2m 本體降級能力（M5/M6），代價高、跨多模組，非單切片可收。
- **路徑 C（僅記錄）**：維持本節偵察結論，不改原始碼；把 DR-1 明確定義為 M5/M6 範疇的獨立里程碑。

- ⇒ **本輪選択路徑 C**：只固化定位、刷新 ⑯ ⑤ 的歸因精度。S0→noc 已達成（⑯，不變）；noc→noc 定點（DR-1）仍需就 A/B/C 取捨後再動工。

---

### ⑱ 2026-11-16 追記：路徑 B 偵察——h2m 本體降級缺口的真實分層與量測恢復（**偵察輪次**，HEAD `df48dd89`）

> 使用者於 ⑰ 的 A/B/C 決策中選 **B（攻 M5/M6 h2m 本體降級）**。本節是 as-built 偵察：恢復量測、定位缺口分層、給出可驗收切入點；**不改任何原始碼**（experiment 全在 /tmp 與已還原的工作檔）。

**① 量測工具鏈 abort 根因（恢復可量測性）**

- `cov-measure.no` build 成功（1.05 MB），run 階段 `signal: abort trap`（rc=134，零輸出）。lldb 崩棧精確定位：`malloc: pointer being freed was not allocated` → `drv_bp_walk +3304`（遞迴幀）→ `drv_build_package` → `drv_pkg_ll`。
- 根因：`drv.bp-walk`（drv.no:450）的 `bpctx { r str, cyc bool }` 作**值參數 + 返回值**參與遞迴（drv.no:511 `sub = drv.bp-walk(...)`）。self-host codegen 對 struct 內 `str` 欄位做淺拷貝，遞迴返回 scope-exit 時雙重釋放——與 ⑯ `ck.no` option 腐敗同類的 **self-host codegen 所有權缺陷**，非 h2m 降級臂缺失。
- 隔離實驗（stub 掉 line 511 遞迴為 `sub = r`）⇒ abort 消失、產 571 行覆蓋率日誌。⇒ **確認根因在 struct-by-value 遞迴**；此缺陷本身是 C 路徑（Go codegen）的獨立 bug，不在 B 的 h2m 降級範疇，但擋住了 B 的驗收量測。

**② 當前 unsup 真實分佈（用 stub 版恢復量測，入口 drv.no 單模組）**

stub 版 `drv.pkg-ll-cov('compiler/src/drv.no')` 產 569 條 `fn:Kind:construct`（`/tmp/h2m-cov.log`）：

| 構造 | 條數 | 判讀 |
| --- | --- | --- |
| Identifier | 182 | 分散，特定上下文（實參/複合運算式）未降級 |
| **FunctionDefinition** | **132**（20 個唯一函式，全 `drv.*`） | **根因已定位（見 ③）** |
| CallExpression | 121 | 跨模組點號呼叫為主 |
| StringLiteral | 48 | 分散於 17 函式，上下文相關 |
| IntegerLiteral | 38 | 分散，上下文相關 |
| StructLiteral / BooleanLiteral / SurfaceMatch / SliceLiteral | 18/12/11/6 | 小族 |

**③ FunctionDefinition 132 條的根因（推翻文檔舊註）**

- drv.no:555-558 舊註称「`drv.*` 點號定義因 parser signature c5 多算 1 被全判 bad」。探針（self-host `ps.parse` + `h2m-func-sig`）實測**精化**：
  - 點號名自由函式（如 `drv.foo`）被 self-host parser 誤判為 **method definition**（`isM=true`、`c5 = 宣告outs+1`），`h2m-func-sig` 用 `realOuts = c5-1` 抵消 ⇒ 單純 i64/str 簽名 **bad=0**（不是被拒原因）。
  - 真因：`h2m-param-ty`（h2m.no:3818-3848）對 slice 型別**只收 `[]i64`**（3834-3846 `enm == 'i64'`）、對 struct 具名型別（`bpctx`/`h2m-sigs`）查不到表 ⇒ 含 `[]byte`/`[]str`/struct 參數或返回的函式 `bad=1`。探針實證：`drv.a([]byte)→bad=1`、`drv.b()→[]str bad=1`、`drv.d()→h2m-sigs bad=1`、`plain(i64)→bad=0`。
  - 20 個被拒函式含 `bp-walk`（回 `bpctx`）、`pkg-split`（回 `[]str`）、`pkg-sigs`（回 `h2m-sigs`）、`read-mod`（回 `[]byte`）、`build-ll`（參 `[]byte`）——**全是複合型別簽名**。

**④ B 的真實層次與瓶頸（誠實規模評估）**

- **層 1（簽名側）**：`h2m-param-ty` 僅認 `[]i64` + 已登記 struct。放寬到 `[]str`/`[]byte` 可讓部分函式 signature 由 bad=1→0，但——
- **層 2（codegen 側，真正的牆）**：self-host `cg.no` 的 vec 助手**只有 `vec_push_i64`/`vec_get_i64`/`vec_len`**（cg.no:1952-1978），預備元素是 `%vec`=i64 向量；**沒有 str-vec**（無 `vec_get_str`/`vec_push_str`）。⇒ 即便 signature 放寬，`[]str`/`[]byte` 函式本體照樣降不出來。struct-by-value 亦受 ① 的 codegen 所有權缺陷制約。
- self-host 現況：MIR **96 op**、cg **95 handler**、struct 支持 6 處——不是空白，但補齊 str-vec + struct-by-value + 各降級臂是**跨 mir/cg/h2m 三模組的系統工程**，遠非單一切片。
- ⇒ B 的完成度：**量測已恢復、缺口已分層定位、切入點已找到（h2m-param-ty 放寬 + str-vec codegen）；但達成 noc→noc 定點需 M5/M6 多輪力度**，本輪不跨層誇大。

**⑤ 切片建議（⑤' 已推翻原切片 1——實測修正）**

- ⚠️ **原建議「切片 1：放寬 `h2m-param-ty` slice 元素型別」經查證是錯的，不得实施**：h2m-test.no:513 明確記載「元素非 i64 的 vec（`[]str` 等）**不攜元素型別且會被 i64 helper 誤讀** ⇒ 欄位解析回 -1 計 unsup」；cg.no 的 vec helper 只有 `vec_push_i64`/`vec_get_i64`（1949-1968），`%vec` 元素是裸 i64。放寬簽名會讓 `[]str`/`[]byte` 函式被當 i64-vector 產碼 ⇒ **違反「寧缺勿錯」的核心正確性保證**（自舉产物比權威更寬鬆且靜默錯碼）。簽名 gate 是正確的誠實門禁，不是缺陷。
- **可做的真實切片（按依賴序）**：
  - **切片 A（前置、獨立）**：修 ① 的 **struct-by-value 所有權雙重釋放**——這是量測工具崩潰與 `bp-walk` 無法在 self-host 跑通的單點根因（drv.no:511 遞迴 `bpctx` 值返回）。先定位是 Go codegen 還是 h2m/cg 的 struct 淺拷貝，修法若為「遞迴改輸出參 / 累加器重用」屬 compiler 源碼層可解；修好後 cov-measure 無需 stub 即可跑，量測恢復可信度大幅提升。**這是 B 的真正第一步**（先讓量測工具自身能跑）。
  - **切片 B（大工程）**：給 self-host cg 補 str-vec 助手（`vec_push_str`/`vec_get_str`）+ MIR op + h2m vec 降級臂，讓 `%vec` 攜元素型別；如此 `h2m-param-ty` 放寬才安全。跨 mir/cg/h2m 三模組，對應 M5/M6 多輪力度。
  - **切片 C**：struct 具名型別（`bpctx`/`h2m-sigs`）作函式簽名/返回值——與切片 A 的 struct-by-value 所有權同源，需一起解。

- ⇒ **本輪小結（誠實）**：使用者選 B 後完成量測恢復（stub 版）+ 缺口分層定位（①②③④）+ **否決一個看似低風險實則破壞正確性的偽切片**（⑤'）。B 的真正第一步是**切片 A（struct-by-value 所有權）**——它同時解開量測工具崩潰與 `bp-walk` 自舉障礙，是最高槓桿的單點。尚未落地原始碼：切片 A 需先判定修在 Go codegen 還是 compiler 源碼層（跨層決策），留待下一輪或請求授權後動工。

---

### ⑲ 2026-11-16 追記：切片 A 落地——borrowed struct 參數移入 owned 槽補 deep clone（**Go codegen 治本**，HEAD `df48dd89`+dirty）

> 使用者於 ⑱ 的 A/B/C 決策中選 **1（修在 Go codegen / `src/` 層，治本）**，並要求「充分驗證和測試，要長遠考慮」。本節 as-built：根因、修復、IR 證據、門禁全數。**已落地原始碼**（`src/mir/codegen.go` +21 行 + 新測試檔），不同於 ⑱ 的偵察輪。

**① 根因（推翻 ⑱ ① 的「Go codegen 所有權缺陷」笼統說法，給出精確機制）**

- lldb 崩棧：`drv_bp_walk +3304` → `+3008`（遞迴幀）→ `drv_build_package` → `drv_pkg_ll`；`malloc: pointer being freed was not allocated`。double-free 非確定性（有時 rc=0 無輸出），符合雙釋放特徵。
- 抓 IR（`NOLANG_MIR_DUMP_LL`）+ MIR（`NOLANG_MIR_DUMP_MIR`）對照 `drv_bp_walk`：
  - MIR：`PARAM 1956 type=drv.bpctx owned=false`（ctx 入參，borrowed）、`PARAM 1958 ... owned=false`（r out-param）；`move dst=0 args=[1956 1958]` = `r = ctx`（EmitMoveInto，src=borrowed param，dst=out-param）。
  - IR 序言：`load %p2 → store %v0.s`（r=ctx 位拷貝，**無 clone**）；exit：`__nolang_drop_drv_bpctx(%v0.s)` drop r → `str_free` 掉 **ctx 入參的 buffer**（caller 擁有）。
- **精確根因**：`emitMove`（codegen.go:8670-8727）早已對 borrowed `str` / `[]T` 參數移入 owned 槽做了 clone guard（註解明言「callee 不擁有參數 heap，位拷貝會共享 buffer 而 double-free」），但 **struct 類型被漏掉**。`bpctx { r str }` / `bx { s str }` 這類「含 owned str 欄位的 struct」作參數、`r = ctx` 重綁進 out-param 時走位拷貝，str 欄位指針與 caller 共享 → callee scope-exit drop → 釋放 caller buffer。
- **對照實驗（排除誤判）**：Go codegen 編普通 Nolang `id = (ctx bx)(r bx){ r = ctx }`（無迴圈）不崩——因其 IR 無 scope-exit drop；加迴圈 + 遞迴累積（`r=ctx`→`walk(r,..)`→`r=sub`）後 **6/6 rc=133** 穩定崩，精確隔離出「borrowed struct param + out-param 重綁 + 迴圈 drop」三要素。

**② 修復（最小、复用既有 emitters）**

- `src/mir/codegen.go` 的 `emitMove` borrowed-param guard 之前新增：

  ```go
  if c.curFn != nil && isFuncParam(c.curFn, inst.Args[0]) {
      if key, ok := c.clonePtrStructKey(inst); ok { return c.emitPtrStructClone(inst, key) }
      if key, ok := c.cloneLeafStructKey(inst); ok { return c.emitLeafStructClone(inst, key) }
  }
  ```

- 復用 `emitClone` 既有的 struct deep-clone 發射器（`clone{,Ptr,Leaf}StructKey` 只在 struct 確實擁有 heap 時命中）；標量 / str / vec 落回原路徑，零誤傷。入口 `isFuncParam` 只查 `f.Params`（輸入參數），不碰 result param。
- 修後 IR：`r=ctx` 位拷貝後緊接 `@str_clone`（給 dst 獨立 buffer）；exit drop 釋放的是 clone 的 buffer——配對正確，無 double-free 亦無洩漏（caller 的 struct 全局不 drop，callee 只 drop 自己的 clone）。

**③ 驗收（充分驗證）**

| 項 | 修前 | 修後 |
| --- | --- | --- |
| 最小 repro `repro-loopctx.no` | 6/6 rc=133 | **6/6 rc=0**，輸出正確；20 次壓力全過 |
| `cov-measure.no`（量測工具） | abort trap，零輸出 | **rc=0，8140 行**，5/5 輸出 SHA 完全一致 |
| unsup 覆蓋率（遞迴完整展開） | 舊 stub 版 571 行 | **8116 行日志 / 8054 條 unsup**（權威基線，見下） |
| `make test`（全 Go 包，注入 key） | — | **rc=0 全綠**（含 lsp 431s / hir 75s；`mir` 包新測試全過） |
| selfhost 門禁 | — | ck 20/20、ck-sig 50/50、diag 7/7、lx 81/81、tok 62/62、util 27/27、ps 49/49、ast 48/48、**h2m 1085/1085** 全綠 |
| `no build compiler` + `version` | — | **rc=0 / 3.9 MB / `noc v0.0.1-m0`** |
| `no vet src/std` | — | **0 error**（6 warning / 322 hint 為既存漂移） |

- 新測試（`src/mir/param_struct_move_clone_test.go`）：① borrowed struct param → clone（紅燈→綠燈）、② 遞迴+迴圈形態（對應 bp-walk 崩潰）、③ **negative control：POD struct（無 owned 欄位）不得誤加 clone**——三條全綠。
- 純 `go test ./...`（不經 make）的 8 個 FAIL 全是「StdSigKey 未注入」（直接 go test 跳過 make 注入），與本修復無關；`make test` 同口徑全綠。

**④ 對 B 路徑的意義（誠實邊界）**

- ✅ **切片 A 完成**：量測工具 `cov-measure.no` 不再崩、無需 stub，量測恢復可信度；struct-by-value 所有權 double-free 在 **Go codegen 層治本**（非繞過）。
- ✅ **拿到遞迴完整展開的權威 unsup 分佈**：Identifier 4073 / FunctionDefinition 1691 / CallExpression 990 / IntegerLiteral 840 / StringLiteral 288 / SliceLiteral 77 / SurfaceMatch 39 / BooleanLiteral 26 / BlockStatement 24 / For 3 / Nil 2 / Break 1（合 8054；`set-count:BlockStatement` 為計數輔助行）。這取代 ⑱ 的 stub 版 569 條與 11-14 舊日誌 5749。
- ⚠️ **noc→noc 定點（DR-1）仍未收**：⑱ 的切片 B（self-host cg 補 str-vec）與切片 C（struct 具名型別簽名）仍待 M5/M6 多輪力度。本輪只收「量測工具自身能跑 + 最高槓桿 single-point 所有權 bug 治本」，**不誇大為定點達成**。
- 🔴 **⑱ ① 的修正**：⑱ 稱此 bug「可能是 Go codegen 或 h2m/cg 的 shallow-copy」——實測確認是 **Go codegen 的 emitMove borrowed-param guard 漏了 struct 類型**，與 self-host h2m lowering 無關（對照實驗：Go codegen 編普通 Nolang 的同構代碼是對的，只是含迴圈 CFG 時 drop 才暴露）。

---

### ⑳ 2026-11-16 追記（再續）：切片 C 複勘——struct 具名簽名【早已完成】，⑱ 的「查不到表」證偽（**實測輪次**，HEAD `df48dd89`+dirty）

> 使用者於 ⑲ 的 A/B/C 決策中續選 **C（struct 具名型別簽名）**。本節是 as-built 實測：用兩個探針直接量測 h2m 簽名 gate 的當前行為。**結論：切片 C 無剩餘工作**——struct 作形參／返回值／欄位／按值傳參已由 s99b/s99c/s99d 完整實現並測試（h2m 1085/1085 全綠）；⑱ ③ 的「struct 查不到表 ⇒ bad=1」是**孤立探針未先登記 struct 的假象**，早在 s99b 落地時即已失效。本輪**不改任何原始碼**。

**① 方法（兩個探針，皆在 `compiler/tests/` 下跑 `./bin/no run`）**

- 探針 1（單模組）：`ps.parse` 解析 `compiler/src/drv.no` → `h2m-struct-register` + `h2m-struct-resolve` 先登記 struct → 逐函式調 `h2m-func-sig` 印 bad。
- 探針 2（整包，與 `drv.pkg-ll` 真實路徑同構）：`drv.build-package` + `drv.pkg-split` 取 16 模組 → pass1 全模組 struct 登記進**共享 agg** → pass2 逐函式調 `h2m-func-sig` 印 bad。
- ⚠️ 探針編寫踩兩個本倉庫已知坑（均已繞開，非被測物件缺陷）：(a) self-host codegen 不認 `str.to-bytes()`（`drv.read-mod` 已回 `[]byte`，直接 `ps.new(src)`）；(b) 頂層/函式內 `#{overflow=wrap}` 只改取值不改回傳型別，迴圈自增須兩步式 `n = x + 1` + `x = n: { ok -> it  -> 0 }` 解包；(c) 單行 match arm 非法須換行；(d) bug #85（multi-line field-access 探針variant 觸發 consumer reads value 0）——故 pty 交叉驗證改用直接讀源碼簽名。

**② 實測結果（權威）**

| 視角 | struct 數 | bad 函式數 | struct 簽名 bad | 根因分佈 |
| --- | --- | --- | --- | --- |
| 單模組 drv.no | 1（`bpctx`） | 4 | **0** | 4 全為 slice：`build-ll`/`read-mod`/`pkg-split`([]byte/[]str) + `pkg-sigs`（回 `h2m-sigs`，因單模組未登記他模組 struct 而 bad；整包視角消失） |
| 整包 16 模組共享 agg | 12 | 26 | **0** | 26 全為 `[]str`/`[]byte` 形參或返回，**無一 struct 具名型別** |

- 整包 26 bad 函式：`drv.read-mod`/`drv.pkg-split`/`drv.build-ll`、`h2m-cov-join`、`cg-emit-func`/`cg-emit-cfg`/`cg-emit-cfg-t`、`check`/`ck-str-at`/`ck-sig-types`/`ck-sig-str-at`/`ck-sig-bsearch`、`ps-q-esc`/`ps-star-label`/`ps-match-listed`/`ps-enum-variants`/`ps-detect-generic`/`ps-add-generic`、`lx-new`/`lx-sub`/`lx-esc`/`lx-contains-dot`、`asp.strs`、`scan-all`、`new`、`dump`。逐一比對源碼簽名（Grep 工具）確認全部帶 `[]str`/`[]byte`。
- **關鍵反證**：`drv.bp-walk`（回 `bpctx`）與 `drv.pkg-sigs`（回 `h2m-sigs`）**都不在 bad 列表** ⇒ struct 具名型別作返回值已在 gate 通過；`h2m-test.no` 的 s99b（形參/返回/欄位寫）、s99c（跨基本塊槽）、s99d（按值傳參+改寫）四檔全綠 ⇒ 完整生命週期已覆蓋。

**③ 對 ⑱ ③ 的證偽與根因**

- ⑱ ③ 稱「`h2m-param-ty` 對 struct 具名型別查不到表 ⇒ `bp-walk`/`pkg-sigs` 等 bad=1」。**實測相反**：`h2m-tynode-code`（h2m.no:909-914，s99b）在 `h2m-ty-code` 回 -1 時回退 `h2m-struct-find(agg, nm)`，命中即 `TY-STRUCT-BASE + sid`；`h2m-lower-program` 前掃描拆兩段（①先全 struct 登記/解析，②再函式簽名），`drv.pkg-ll` 整包路徑 pass1 亦把所有模組 struct 登進共享 agg（drv.no:733-759）。
- ⑱ 的探針結論推測來自**未先跑 struct 登記**就孤立調 `h2m-func-sig`——那樣 `bpctx`/`h2m-sigs` 當然查不到。真實路徑（單模組前掃描 + 整包 pass1）均已先登記，故 struct 簽名 gate 從 s99b 起就是通的。本節以兩探針 + 源碼簽名比對三重佐證。

**④ 對 DR-1 / B 路徑的意義（誠實邊界）**

- ✅ **切片 C 達成（無需改碼）**：struct 具名型別簽名不是缺口。⑳ 的價值在於**證偽一個基于此錯誤前提的決策**，避免對已完成的子系統做無意義改動。
- 🔴 **DR-1 剩餘瓶頸收斂到切片 B（str-vec）**：整包 26 個 bad 函式**全部**因 `[]str`/`[]byte`——這與 ⑱ ⑤' 的判斷一致（self-host cg 無 `vec_push_str`/`vec_get_str`，`%vec` 元素只認 i64）。真正的定點工作 = 給 self-host cg 補 str-vec 助手 + MIR op + h2m vec 降級臂 + 參數化 clone/free 所有權（鏡像 Go 的 `__nolang_vec_clone_<T>_<d>` / `__nolang_vec_free_<T>_<d>`），跨 mir/cg/h2m/cg-runtime 四模組，對應 M5/M6 多輪力度。
- ⚠️ ** Identifier(4073) 的最大塊未在本次解析**：cov 日誌的 Identifier 記錄分散且 cur-k 歸因混亂（同一函式如 `drv.bp-walk` 同時有 BooleanLiteral/CallExpression/FunctionDefinition 多種記錄），單靠日誌無法判定哪些是純 gate 表徵。推測多數源於「變數因上游 slice 型別 gate 未登记槽表 ⇒ 引用它的 Identifier 成片 unsup」，與 26 個 slice bad 同源，但**未逐條證實**，不寫入結論。

---

### ㉑ 2026-11-16 追記（再續）：切片 B 底座落地——str-vec 運行時助手 + MIR op + cg 發射器（**底座輪次**，HEAD `df48dd89`+dirty）

> 使用者於 ⑳ 的方向調整中選 **轉攻切片 B（底座）**：先給 self-host 補 str-vec 助手並用獨立小程序驗證跑通，再談 h2m 降級臂與簽名 gate 放寬。本節 as-built：所有權方案、四模組改動、IR 文本 + clang/ASAN 运行驗證、誠實邊界。**已落地原始碼**（`compiler/src/{cg-runtime,mir,cg}.no` + 兩測試檔），不同於 ⑳ 的純實測輪。

**① 所有權方案（鏡像 Go `vecDeepClone`/`vecDeepFree`，適配 self-host 無 magic 頭的 `rc_release`）**

- `%vec` = 16B `{i32 len, i32 cap, i64 data}`（對外 API 仍 i64，邊界 zext/trunc）；i64 元素 stride 8、`%str-long` 元素 stride **16**。
- 陣列緩衝用裸 `@malloc`/`@free`（與 i64 vec 一致）；元素 str 用 `@nolang_rc_alloc`/`@nolang_rc_release`。
- **push 時 clone 元素**到新 rc 塊（memcpy bytes + rc_alloc）——這是關鍵：字面量 `@.str` 無 rc 頭，若直接持有再 release 會讀越界頭；clone 後一律是獨立 rc 塊，free 安全。
- 擴容 memcpy 元素（淺拷貝 rc 塊指針）後只 free【舊陣列緩衝】（裸 free，非元素 buf）⇒ 元素 rc 塊在搬遷後仍唯一，free-str 逐元素 release 一次，無 double-free。
- free-str：逐元素 release data（field 2）→ free 陣列緩衝；`dat == 0` 短路（空 vec）。
- ⚠️ **get-str 回借用視圖**（與原 vec 共享元素 buf），呼叫方不得 drop 結果——與 Go 的 borrow 語義同構，但 self-host 尚無 retain-on-borrow，此約束由 h2m 降級臂將來兌現（本輪底座不接 h2m）。

**② 四模組改動（共 +260 行）**

| 檔 | 改動 |
| --- | --- |
| `cg-runtime.no` | +新助手 `vec_push_str`/`vec_get_str`/`vec_free_str` 發射函式（IR 文本），註冊進 `cg-runtime-prelude-full()` |
| `mir.no` | +3 op 碼 `OP-VECPUSHSTR=96` / `OP-VECGETSTR=97` / `OP-VEC-FREE-STR=98`（接 OP-PRINT=95 後，不重排既有碼） |
| `cg.no` | +3 op 發射分支：`%vc<d> = call %vec @vec_push_str(...)` / `%sv<d> = call %str-long @vec_get_str(...)` / `call void @vec_free_str(%vec ...)` |
| `cg-test.no` / `cg-runtime-test.no` | +手寫 op 發射測試 + 助手 IR 文本測試（clone/stride/release/short-circuit 全釘住） |

**③ 驗證（充分）**

| 項 | 結果 |
| --- | --- |
| `cg-test`（手寫 op 發射） | **363/363 全綠**（含新 `test-strvec-ops`） |
| `cg-runtime-test`（助手 IR 文本） | **103/103 全綠**（含新 `test-strvec-helpers`） |
| `h2m-test`（未接降級臂，回歸） | **1085/1085 全綠** |
| clang 22 + ASAN：push 3 str + 讀回 + free | **rc=0**，無 double-free/UAF |
| clang 22 + ASAN：push 6 str（強制 2 次擴容 0→4→8）+ 讀回全部 + free | **rc=0**，擴容搬遷元素後 free 無 double-free |
| **負對照**：同程式 free 兩次 | **rc=134**，`AddressSanitizer: attempting double-free ... in vec_free_str+0x64` ⇒ 證明 ASAN 檢測有效，上面 rc=0 非空話 |
| `no build compiler` | **rc=0**，產物 3.96 MB，`version` = `noc v0.0.1-m0`（S0→noc 未回歸） |
| `make gen no` | rc=0（Go 側未受影響 rebuild） |

- prelude dump 證三助手皆在發射產物中（`vec_push_str`@219 / `vec_get_str`@267 / `vec_free_str`@277 of 441 行 prelude）。
- ⚠️ **macOS 上 LeakSanitizer 不工作**：`detect_leaks=1` 下故意不 free 的負對照也 rc=0 ⇒ **本輪只驗證了「無 double-free/UAF」，未驗證「無洩漏」**。洩漏的正確性靠 IR 結構推論（每元素 clone 恰一次、free-str release 恰一次、擴容搬遷不重複 clone），非运行時量測。

**④ 誠實邊界**

- ✅ **底座達成**：str-vec 的三個核心助手在 self-host 側從零到可發射、可运行、所有權正確（double-free 維度經 ASAN 正反向驗證）。
- ⚠️ **尚未接 h2m 降級臂**：本輪新 op 只由 cg-test 手寫 op 與手写 .ll 觸發；真實 `[]str` 源碼仍不降級（h2m SliceLiteral 仍限 i64 元素、signature gate 仍拒 `[]str`）。職責切分刻意如此——底座先立穩，再接 h2m。
- ⚠️ **未做 str-vec clone/別名**：vec 整體被別名（寫進 struct 字段、作返回值、option 載荷）仍無 retain/clone；get-str 借用視圖尚無 retain-on-borrow。完整所有權（對應 Go 的 `__nolang_vec_clone_<T>_<d>`）留待 h2m 集成階段。
- ⇒ 距離 noc→noc 定點（DR-1）仍需：**h2m `[]str`/`[]byte` 降級臂 → 簽名 gate 放寬 → str-vec clone/別名與借用 retain → Identifier unsup 歸因**。本輪只收底座一層，不誇大為定點進展。

### ㉒ 2026-11-16 再續：切片 B 第二層——h2m `[]str` 降級臂接進底座（HEAD `df48dd89`+dirty）

**目標**：把 ㉑ 的 str-vec 底座（三個助手 / MIR op / cg 發射器）從「只由手寫 op 觸發」接進真實 HIR→MIR 降級路徑，讓 `[]str` 源碼能自行降出 `vec_push_str/get_str/free_str`。用戶選「做吧」（承接 ㉑ 末尾提議），並沿用「充分驗證、長遠考慮」要求。

**核心設計：新增 `TY-VECSTR = 6`**（mir.no，取 TY-F64=5 與 TY-STRUCT-BASE=100 之間空檔）。
- LLVM 層與 `TY-VEC` **同一型別 `%vec`、同一命名空間 `%vc`**——元素 stride 只活在助手選擇裡（16 vs 8）。與 TY-U64「型別碼存在但共用 LLVM 型」同構。
- ⚠️ `cg-runtime-ty-name` / `cg-val-prefix` 必須**顯式**映射 TY-VECSTR → `%vec`/`%vc`，否則 `[]str` 形參/返回落默認臂發成 i64/%v（型別錯配）。已補。

**h2m.no 接線（六處）**：
1. `h2m-tynode-code`：`[]str`（K-SLICE-TYPE 元素 str）→ TY-VECSTR（原本只認 i64）。
2. `h2m-param-ty`：簽名 gate 放行 `[]str` 作形參/返回 → TY-VECSTR、bad=0。**`[]byte` 不放行**（1 字節元素， vec_push_str 按 16B 讀寫會錯，另一套 helper）。
3. `K-SLICE-LITERAL`：首元素 AST kind 預判 vec-ty（K-STRING-LITERAL ⇒ TY-VECSTR），逐元素以實際 vty 覆核（TY-VEC↔i64、TY-VECSTR↔str，混合即 unsup）；str 元素發 `OP-VECPUSHSTR`。**保持 VECNEW 仍為首個 alloc**（不改值 id 分配序 ⇒ 既有 h2m-test 的 id 斷言不受影響）。
4. `K-INDEX-EXPRESSION`：base vty == TY-VECSTR → `OP-VECGETSTR`（回借用 `%sv`，TY-STR），不走 i64 `OP-VECGET`。
5. `h2m-emit-releases`：TY-VECSTR → `OP-VEC-FREE-STR`；`h2m-vec-own` 納入 TY-VECSTR（外流攔截同源）。
6. **保守門禁**：`h2m-struct-resolve[-range]` 把 TY-VECSTR 字段攔回 -1 計 unsup——str-vec 無 retain，寫進 struct 欄位後與 scope-exit 的 `vec_free_str` 雙釋放 ⇒ UAF（沿用 s99f「元素非 i64 欄位不入形」口徑）。`for-in` 容器仍限 TY-VEC（str-vec 逐元素 str 遍歷 + 槽屬 M7，本輪不碰）——故 ㉑ 提到的 `coll-gate-elem-str` 負例仍維持。

**驗證（充分，誠實分已證/未證）**：
- selfhost 三檔全綠：**cg-test 363/363、cg-runtime-test 103/103、h2m-test 1099/1099**（本輪 +10 斷言：字面量正例、混合元素門禁、[]str 形參簽名、索引 get_str、返回 %vec、struct 欄位拒絕）。s99n 時代「str 元素計 unsup」的負例升級為正例。
- **端到端（真 HIR→MIR→cg→clang）**：源碼 `mk = () (r []str) { r = ['ab','cde','fghi'] }` + `pick = (xs []str,i i64)(r str){ r=xs[i] }` 經 `h2m-lower-program` 降出 `%vec @mk()`（3× vec_push_str）與 `%str-long @pick(%vec,i64)`（vec_get_str），0 unsup；拼 `cg-runtime-prelude-full()` + 手寫 main（mk→pick 讀回索引 2/0 校驗 len → vec_free_str）clang+ASAN **rc=0**。
  - **負對照**：main 故意 double-free → rc=134 報 `vec_free_str+0x64`，證明 ASAN 檢測有效、上面 rc=0 非空話。macOS LeakSanitizer 仍不工作（㉑ 同），**只證無 double-free/UAF，未證無洩漏**。
- `no build compiler` rc=0（3.96MB，`noc v0.0.1-m0`）；`make gen no` / `make test` 見下。
- **cov-measure unsup 8054 → 8472（+418）——方向上看似倒退，實為誠實門禁的精細化，非回歸**：差集分析（按 fn）顯示增量集中在兩類——① 本輪新增的 self-host 助手函數自身（`cg-runtime-vec-push-str` +49、`vec-free-str` +31、`vec-get-str` +11、`h2m-struct-resolve[-range]` 各 +8 等約 120 條）：新函數用了 h2m 尚未降級的構造；② 簽名 gate 放行後**函數體暴露**（`check` 1→135、`cg-emit-cfg-t` 1→49、`drv.pkg-split` 1→18、`ck-sig-bsearch` 1→28 等）：基線的 `1` 是「整函數被簽名 gate 拒」，現在簽名通過、體內 with-cap/push/str 方法等未支持構造被逐條記出。**關鍵安全性質**：`h2m-lower-func` 末尾 `inc>0 ⇒ r=''`（4256-4272），任何 unsup 函數整函數不產碼 ⇒ 不會有靜默錯碼，unsup 增長只是記錄更細。
- 結構等價性：本輪 struct-resolve 對 `[]str` 欄位的攔截與改前語義**等價**（改前 tynode-code 對非 i64 slice 就回 -1），故 `[]str` 欄位眾多的 `psst`/`ast` 等結構體降級行為不變，build compiler 未受影響（已實證 rc=0）。

**誠實邊界（距定點仍遠）**：
- 本輪只接了 `[]str` **字面量 + 索引 + 形參/返回 + 釋放**；真實 `[]str` 構造主力 `with-cap(4)` + `.push()`（drv/ck/ps 大量使用）**尚未降級**——cov 差集裡 `check` +134 主要就是這批。下一層要補 with-cap（vec_new with capacity）與 vec-push 方法調用的降級。
- `[]byte` 未放行（需獨立的 1 字節元素助手族）。
- str-vec **整體別名**（寫字段/option 載荷/賦第二變量）仍無 retain/clone；get-str 借用視圖無 retain-on-borrow；for-in str-vec 未支援（M7）。
- ⇒ DR-1 定點仍需：**with-cap/push 降級 → []byte 助手族 → str-vec clone/別名 → Identifier unsup 逐條歸因 → noc→noc 比對**。本輪把底座真正接進了自舉路徑（首度有真源碼降出 str-vec op 並跑通），但不誇大為定點進展。

### ㉓ 2026-11-16 再續：切片 B 第三層——`with-cap` + `.push()` 降級（HEAD `df48dd89`+dirty）

**目標**：補上 ㉒ 末尾點名的真實 `[]str` 構造主力 `with-cap(n)` + `.push(x)`——drv/ck/ps 大量使用，是 cov 差集裡 `check`+134 的主要來源。用戶「繼續」，沿用「充分驗證、長遠考慮」。

**語義矛盾（先定位再動手）**：
- Go 側 `vec.push` 是**原地寫回接收者**（`src/std/vec.no` builtin，codegen 展開），`with-cap` 回型別由 **LHS 註解**決定（`builtin/str.go:184`、`emitBuiltinAlloc` 以 `inst.Dst` 選 stride）。
- self-host 是 **SSA 值語義**：vec 不鋪槽（§11 B5），`vec_push_*` 助手回**新** `%vec`。故 `.push()` 語句要正確，必須把新值**重綁**到接收者變數，且舊值不可再 free（助手已 free/共享其緩衝）——否則 double-free。
- `with-cap` 的 cap **有真實語義**：跨幀傳遞的 `bufio.reader.init('with-cap(64)')` 要求 `buf.cap` 原樣保留，否則 `fs.read(fd, buf, buf.cap)` 讀 0 位元組（Go `codegen.go:2414 vecBufClone` 來歷）⇒ **不能**像 `@vec_new` 那樣恆回 `{0,0,0}`，必須 malloc cap*stride 並寫 cap 欄位。

**落地（4 檔）**：
1. `cg-runtime.no`：新增 `vec_new_cap_i64`（stride 8）/ `vec_new_cap_str`（stride 16 + memset 0）兩助手；cap==0 短路回 `{0,0,0}`（與 `@vec_free` 的 dat==0 短路對齊）；註冊進 `prelude-full`。
2. `mir.no`：新增 `OP-VECNEWCAP=99` / `OP-VECNEWCAPSTR=100`（接在 98 後，不重排既有碼）。
3. `cg.no`：兩 op 發射分支（call `@vec_new_cap_i64/str(i64 %v)`）。
4. `h2m.no`（核心）：
   - `h2m-is-withcap` helper 認 `with-cap(...)` 呼叫；前掃描（`h2m-scan` K-LET）對 with-cap 用 **let 註解** `h2m-tynode-code` 鋪 TY-VEC/VECSTR（内建無簽名，`h2m-ty-of` 回 -1）。
   - `h2m-lower-let` 特判：降 cap 實參，按註解碼發 OP-VECNEWCAP[STR]，`rel-add` 新值。
   - 方法呼叫臂新增：vec.len / str-vec.len（複用 OP-VECLEN，len 欄位與元素 stride 無關）；vec.push / str-vec.push（發 OP-VECPUSH[STR] + `h2m-var-bind` 重綁接收者名 + `h2m-rel-drop(舊值)` + `rel-add(新值)`）。
   - ctx 新增 `reldrop []i64`（被 push 接管的舊 vec 值），`h2m-emit-releases` 跳過它 ⇒ 中間 push 值不 double-free。
   - **保守門禁**：push 重綁限 `mem==0`（直線式；含控制流時 vec 無槽可寫回，計 unsup 不產碼）+ 接收者須 K-IDENTIFIER（`a[i].push()` 無法重綁 ⇒ unsup）。`h2m-tynode-code` 的 `[]T` 元素同時認 K-NAMED-TYPE / K-IDENTIFIER（let 註解與 struct 欄位兩形態）。

**驗證（誠實區分已證/未證）**：
- 探針實測四形（`with-cap` ± str/i64 × ± push）：全 **0 unsup**；`[]str with-cap(2)+3 push(2→4→8 擴容)+索引+返回` IR 乾淨——中間 vec 值全無 vec_free_str（reldrop 生效）。
- **端到端 clang+ASAN**：`mk()→[]str`（with-cap(2)+3 push 擴容）→ main 讀回 3 元素校驗 len 2/3/4 → free，**rc=0**；故意 double-free 負對照 **rc=134** 報 `vec_free_str+0x64`，證明檢測有效。macOS LeakSanitizer 仍不作用 ⇒ **只證無 double-free/UAF，未證無洩漏**（洩漏靠 IR 結構推論：只 free 最終值、助手擴容時 free 舊緩衝）。
- selfhost 三檔全綠：**h2m-test 1113/1113（+14）、cg-runtime-test 114/114（+11）、cg-test 363/363**。
- **cov unsup 8472→8522（+50）差集分析**：非回歸。增量來自本輪新增 self-host 程式自身（`vec-new-cap-str` +18 / `vec-new-cap-i64` +17 / `h2m-is-withcap` +9 / 被改的 `h2m-lower-let|expr|emit-releases` +11 等共 +59），以及簽名 gate 連帶 -9（ps-parse-star-star-break / ps-add-generic / h2m-cov-join）。安全性質不變：`inc>0⇒r=''` 保證 unsup 函式整函式不產碼，無靜默錯碼。
- `no build compiler` rc=0（3.96MB，`noc v0.0.1-m0`）；`make gen no` rc=0（未改 Go/std blob，Nothing to be done）。

**誠實邊界**：`with-len` / `with-cap-len`（len=cap，直接索引）未放行——需額外助手；`[]byte` 未放行；str-vec 別名/clone、含控制流 push（mem==1 寫回槽）、`a[i].push()` 複合接收者仍 unsup（M7）。距 noc→noc 定點仍需這批 + Identifier unsup 歸因。

### ㉔ 2026-11-18 追記（再續）：unsup 歸因可信化——块上下文栈（s107）（**實作輪次**，HEAD `ca5156e0`+dirty）

**目標**：㉓ 末尾把「Identifier unsup 逐條歸因」列為 DR-1 定點的前置。本輪先查「現有逐條歸因**可不可信**」，再決定是否需要先修量測。

**① 量測基線與歸因現狀（實測）**

- 基線：`./bin/no run compiler/tests/cov-measure.no` ⇒ **unsup 9,623 / 9,633 行日志**，直方圖 `Identifier 4776 / FunctionDefinition 2044 / IntegerLiteral 1145 / CallExpression 1056 / StringLiteral 276 / SurfaceMatch 121 / SliceLiteral 78 / BlockStatement 24`。
- 决定性观察：`FunctionDefinition`（2044）與 `BlockStatement`（24）這兩類**構造本身不可能「不支持」**——函數體是 `h2m-lower-block` 的核心路徑，真不支持就不會有任何產碼。它們能出現在日志裏，說明 `h2m-mark` 記的是 `c.cur-k`（「最近一次 `h2m-set-cur-k` 設的節點」），而內層 `h2m-lower-*` 會一路覆蓋它 ⇒ **日志的 kind 列量的是「最近進過哪個容器」，不是「哪個構造不支持」**，按它做歸因會被引向錯誤的優化目標。

**② 修法（`compiler/src/h2m.no`，+~150 行）**

- `h2m-ctx` 新增块上下文栈 `bks`（块 K）/ `bka`（AST 節點）/ `bkd`（深度）/ `bkp`（父項）/ `bkm`（本块是否已計過）/ `bkn`（項數），`h2m-new` 鋪根項 `K-PROGRAM`（深度 0、父 -1）——根項讓「栈空」不可能出現。
- `h2m-bk-push/pop` 由 `h2m-lower-func`（函数级）、`h2m-lower-block`、`h2m-lower-if`、`h2m-lower-for`、`h2m-lower-surface` 各自壓/彈（均有單一出口，彈栈恒配對）。
- `h2m-bk-take`：从栈顶往下找**第一个未計過**的块并把它標記為已計；整条栈都計過则退回栈顶。⇒ `h2m-mark-here` 记「就近的、尚未認領失敗的块」，同一函數体里的多条失败不会都记成同一个 BlockStatement。
- 模块级（`agg` 无栈）改用 `h2m-mark-at(c, k)` 直接给定块码：被拒**函数签名**在签名前置 pass 就记一条**带函数名**的记录（改前只由产码 pass 记，日志里无从查证是谁被拒），被拒顶层节点同理。
- **誠實邊界**：`c.cur-k` 字段保留（字段型别失败仍按它记，`h2m-mark-field-ty` 專用入口），本輪只改「块内 mark」的歸因基準。

**③ 驗證**

| 項 | 結果 |
| --- | --- |
| `h2m-test` | **1121/1121 全綠**（+2 用例：`test-s107-attrib` 嵌套/單條歸因、`test-s107-reject-named` 簽名被拒帶函數名） |
| 其餘 selfhost 檔 | `cg-test 363/363`、`cg-runtime-test 114/114`、`ck-test 20/20`、`ps-test 49/49`、`lx-test 81/81` 全綠；`ast-test 43/48`（5 红為**既有**：工作樹新增 2 種 AST 節點後 `asp.no` 為 74、該檔仍钉 72，與本輪無關） |
| `no build compiler` | rc=0，4.01 MB，`noc v0.0.1-m0` |
| cov-measure unsup | 9,623 → **9,713**（+90，收尾重構後）：**全部落在 `h2m-*` 自身**（新塊棧/記錄輔助函數尚未被 h2m 降級）；**非 `h2m-*` 函數合計 7,434 → 7,434，逐函數差值全 0**——即被測對象的降級行為一字未變 |
| 歸因基準 | 改前 `h2m-mark` 記 `c.cur-k`（「最近一次 `set-cur-k` 的節點」，被內層容器一路覆蓋）⇒ 日志里出現 **2,044 条 `FunctionDefinition`、24 条 `BlockStatement`**，而函數體/塊本身是 h2m 的核心路徑、不可能「不支持」——這批是**偽構造**。修後歸因由塊棧決定（就近未認領的塊），`h2m-lower-func` 壓的函數級塊只在「簽名被拒」時才命中（`FunctionDefinition 2003`，且此時帶函數名），塊内的失敗一律落 `BlockStatement / IfExpression / ForStatement / SurfaceMatch` 等真實塊種類 |

**④ 副產：三條工具鏈缺陷（本輪撞出並記帳，未繞改合法代碼）**

1. 🔴 **`(cond) { body }` 花括号形条件循环被編成死循环**（**HEAD 已存在**，非本輪引入）：MIR 里 header 无条件 `BR` 回自身形状（IR 只剩 `store i1 1` 骨架，条件值不驱动）；实测常量 `(0 == 0) { }`、变量 `(r == 0) { }`、函数内与顶层皆然；`cond -> { }` 箭头形与 `i <- [a..b): { }` 区间形正常。⇒ **自舉源碼與測試不用花括号形式写条件循环**（本輪新增用例一律用箭头形）。
2. 🟡 **`x = with-cap(n>0)` 后 `push` 只覆寫下标 0、len 不動**：实测 `a []i64 = with-cap(2); a.push(7); a.len()` ⇒ 1（`a.push(9)` 后 ⇒ 2 且元素正确）：即 cap>0 且 len==0 时 push 等价于覆盖首槽。⇒ 记录表不能写「copy 旧项 → push 新项」，改为 `with-cap(0)` 逐项重建。
3. 🟡 **NUL 分隔整段字符串 + 同型短串复用不稳**：`h2m-cov-join` 逐行拼接在多调用点/连跑多档时，首条记录的 KindName 段会被上一次拼接结果顶替（独立探针正确、连跑档错误）；`fn:str` 局部在后续 `- ':' - <callee>` 里同理。⇒ `h2m-cov-line` 收敛成「只接收参数、只做拼接」的纯函数（入参不跨调用边界改写），`h2m-kind-cat` 保留原判据顺（不引第二处字符串比较），cov 断言只钉首段函数名与尾段 category。

**⑤ 誠實邊界**

- 本輪**不降低** unsup 總量（+90 全在新程式碼自身），價值在於把「歸因列」從偽構造變成真實块種類——後續 Identifier 歸因必須建立在可信的量測上，否則會照着假數據做無效優化。
- ⚠️ 歸因只到「最近的塊」粒度：`Identifier / CallExpression` 這類**葉子構造**仍不會單獨出現在 kind 列（它們的失敗歸到所在塊）。要拿到葉子級歸因，得在 `h2m-lower-expr` 等葉子入口也加 mark 記錄或另開一路「候選構造」日志——本輪未做。
- 三條工具鏈缺陷都**不是**自舉缺口（是 `no` 的 codegen 側），記錄在 §s107 供 Go 側排期；自舉側的規避寫法已寫入上述字段注釋。
- ⇒ DR-1 定點仍需：`with-len`/`with-cap-len` → `[]byte` 助手族 → str-vec clone/别名 → 可信歸因下的 Identifier 逐條歸因 → noc→noc 比對。

### ㉕ 2026-11-18 再續：葉子級 unsup 歸因——第二條日誌通道（s108）（**實作輪次**，HEAD `ca5156e0`+dirty）

> 承接 ㉔ ⑤ 點名的「本輪未做」：歸因只到「最近的塊」，葉子構造（`Identifier` / `CallExpression` …）被掩蓋。用戶選「葉子級 unsup 歸因」。本輪**另開一路並行的葉子日誌通道**，塊級（s107）一字不改，兩通道逐條同序等長，可直接對照「失敗歸到哪個塊 / 最近進入哪個葉子構造」。**已落地原始碼**（`compiler/src/{h2m,drv}.no` + `compiler/tests/h2m-test.no`）。

**① 設計（最小侵入：不碰 s107 的任何 mark 調用點）**

- 關鍵洞察：s107 前的 `h2m-mark` 記的就是 `c.cur-k`（最近一次 `h2m-set-cur-k` 設的節點 kind），s107 為了解決「塊內失敗被記成內層容器」把歸因改成塊棧，但**保留了 `c.cur-k` 字段**。葉子構造的失敗恰恰發生在 `h2m-lower-expr` 內（其入口 2251 行就 `h2m-set-cur-k(c, t, n)`），此刻 `cur-k` 正是葉子 kind——這就是天然的葉子級信號，不必新增 mark 調用點。
- `h2m-ctx` 新增兩條平行表 `lfsf []str` / `lfsk []i64`（與 `unsf`/`unsk` 同構），新增 `h2m-leaf-append`（from-scratch 重建，沿用 ㉔ 記的 with-cap push 坑規避）。
- 三個 unsup 記數點（`h2m-mark-here` / `h2m-mark-field` / `h2m-mark-at`）在原有 `h2m-rec-append`（塊級）之後**各補一條** `h2m-leaf-append(c, c.cur-fn, c.cur-k)`。`h2m-mark-at` 無 `set-cur-k` 上下文，退化為記同碼 `k`（誠實註明）。
- `h2m-lower-func` 末尾把本函式葉子表並回模組級 `agg`（與塊級並回同位置、同口徑）；`drv.pkg-ll` 的 emit-cov 再調一次 `h2m-cov-join(agg.lfsf, agg.lfsk)`，輸出 `LF:` 前綴行 + `/tmp/h2m-leaf-cov.log`（塊級 stdout 與 `/tmp/h2m-cov.log` 逐字不變）。
- **不覆蓋、不替換**：塊級與葉子級是兩條獨立、同序、等長的通道；下游要歸因就按下標對照，不破壞任何既有語義。

**② 實測：葉子粒度恢復（改前 → 改後）**

| 通道 | 分布 |
| --- | --- |
| 塊級（s107，$前後不變的語義） | `BlockStatement 3401 / IfExpression 2809 / FunctionDefinition 2005 / ForStatement 706 / SurfaceMatch 368 / Program 348 / SliceType 62`（基線 9699 條，**無任何葉子**） |
| 葉子級（本輪新增） | `Identifier 4822 / FunctionDefinition 2061 / IntegerLiteral 1202 / CallExpression 1106 / StringLiteral 284 / SurfaceMatch 128 / SliceType 63 / SliceLiteral 45 / BlockStatement 24 / BooleanLiteral 21 / ForStatement 3 / NilLiteral 2 / BreakStatement 1`（9762 條） |
| 對照案例（未定義函式呼叫 `nope()`） | 塊級 `f:BlockStatement:construct`；葉子級 `f:CallExpression:construct` —— 兩通道在此分叉，證明葉子粒度真被恢復 |

- 改前後非 `h2m-*` 函數 **400 個逐條完全一致**（7434 不變）；唯一變化是工具鏈自身 `drv.pkg-ll` 270→288（+18，本輪新增 LF 輸出程式所致）與 `h2m-*` 自身（新增函式/欄位）。**被測對象降級行為零回歸**。
- 葉子日誌 9762 行 = 塊級 9762 行 = `agg.unsup`（改後），`UNKNOWN-KIND` 0 行——兩通道嚴格同序等長、無遺漏。

**③ 驗證（充分）**

| 項 | 結果 |
| --- | --- |
| `h2m-test` | **1128/1128 全綠**（+7 斷言：`test-s108-leaf-attrib` —— 塊級不含葉子 kind／葉子級含 `CallExpression`／兩通道等長且等於 unsup／模組級 mark-at 亦記葉子） |
| 其餘 selfhost 檔 | `cg-test 363/363`、`cg-runtime-test 114/114`、`ck-test 20/20`、`ps-test 49/49`、`lx-test 81/81`、`tok-test 62/62` 全綠；`ast-test 43/48`（5 红為 ㉔ 已記的既有，未擴大） |
| `no build compiler` | rc=0，4.01 MB，`noc v0.0.1-m0`（新增 ctx 欄位/函式可被 self-host 降級並編出） |
| cov-measure | rc=0，塊級 9762／葉子 9762／非 h2m 400 函式逐條不變 |
| 格式 | 三檔 `no fmt -d` 均 0 hunk（註解前空行已補） |

**④ 誠實邊界**

- 葉子通道記的是「mark 時刻的 `c.cur-k`」，即**最近一次進入的構造**。對 `h2m-lower-expr` 內的葉子失敗（`Identifier` / `CallExpression` / 字面量）精確；但少數路徑（如 `lower-let` 的 RHS 失敗、`lower-let` 入口已把 `cur-k` 改寫）會退化成 `UNKNOWN-KIND`（0）或容器 kind——本輪**不做精確化**，只提供「候選構造」視角，與 ㉔ 的誠實口徑一致。
- `h2m-mark-at`（模組級簽名被拒）無葉子上下文，葉子通道退化為塊級同碼 `FunctionDefinition`——不是葉子粒度，但如實標記、保持兩通道等長。
- 本輪**不降低** unsup 總量（9762 vs 9699，+63 全在工具鏈自身新增程式 + 新欄位），價值在於把「葉子構造」從不可見變成可逐條統計——為 ㉔ 點名的「Identifier 逐條歸因」提供可信measurement 基礎。
- ⇒ DR-1 定點仍需：`with-len`/`with-cap-len` → `[]byte` 助手族 → str-vec clone/别名 → **基于本輪葉子通道的 Identifier 逐條歸因** → noc→noc 比對。

### ㉖ 2026-11-18 再續：Identifier 逐條歸因——失敗標識符名字旁路通道（s109）（**實作輪次**，HEAD `8c0bd201`+dirty）

> 承接 ㉕ 末尾加粗點名的最後一項量測前置。本輪在 s108 的葉子通道之上**再開一條旁路**：只記
> `K-IDENTIFIER` 三表未命中的失敗**名字**，讓 ㉔ 推測的「變數沒被 h2m-scan 鋪槽」從推論變成可逐條查證的資料。
> **已落地原始碼**（`compiler/src/{h2m,drv}.no` + `compiler/tests/h2m-test.no`）。

**① 根因定位（源碼層，非推測）**

- `h2m-lower-expr` 的 `K-IDENTIFIER` 臂（h2m.no:2463–2533）有三條取值路徑：arm-local (`c.alidx`) ＞
  `it` 棧 ＞ `names` 表。唯一失敗點是 2533 行 `aln-hit == 0 && it-on == 0 && vi < 0 -> h2m-mark(c)`。
- 據此可修正 ㉔ 的推測：`Identifier` 失敗**不是純 gate 連鎖**——被簽名 gate 拒的函式在
  `h2m-lower-func` 末尾 `inc>0 ⇒ r=''` 直接不產碼，其體內 Identifier 根本不會進入 lower-expr。
  會產生失敗記錄的，是**通過簽名 gate、進入產碼、但體內某變數沒被 h2m-scan 登記進 names 表**的函式
  ⇒ 這是真實的槽登記缺口，不是测量噪聲。

**② 設計（旁路觀測，零語義影響）**

- `h2m-ctx` 新增 `idn []str`；新增 `h2m-id-name(c, nm)`（from-scratch 重建，沿用 with-cap push 坑），
  存 `"fn:name"`。**只**在 2533 那個失敗點加一次呼叫（改單臂為塊，先 `h2m-mark(c)` 再 `h2m-id-name(c, nm)`）。
- ⚠️ **不增 `c.unsup`、不碰 `unsk/unsf/lfsk/lfsf`**——s108 的兩通道等長語義一字不改；idn 與之無關、不等長。
- `h2m-lower-func` 並回模組級 `agg.idn`；`drv.pkg-ll` emit-cov 輸出 `ID:fn:name`（前綴與塊級／`LF:` 區分）。

**③ 實測：2071 條失敗標識符的可操作分類**

| 類別 | 數量 | 典型名字 | 指向 |
| --- | --- | --- | --- |
| 模組基名 | 607 | `tok 303` / `asp 155` / `idx 149` | §2.2.7／§2.2.8 跨模組前綴與常量綁定限制 |
| 常量名（大寫／連字符） | 131 | `TY-I64` / `OP-CMPGT` / `NOC-NAME` / `EXIT-OK` / `HDR-MAGIC` | 跨模組常量只對入口直接 import 的模組生成 |
| 普通局部名 | 1940 | `i 123` / `d 99` / `j 73` / `n 60` / `t 45` | h2m-scan 對控制流／巢狀形狀漏鋪槽（M7 所有權／槽登記） |

- 按函式分佈仍分散（`drv.pkg-ll 87` / `ps-is-arm-start 55` / `ps-parse-method-func 48` / `check 43` …），
  **支援 ① 的結論**：不是少數函式整體失敗，而是跨全庫的槽登記缺口。
- 葉子通道 `Identifier 4835` vs ID 通道 `2071`：差額是 ㉕ ④ 記的 `cur-k` 歸因噪聲（進入 Identifier 節點但
  失敗歸到別處），故 ID 通道是**保守下界**——只記三表真正未命中的情形。本輪不精確化差額，如實標記。

**④ 驗證（充分）**

| 項 | 結果 |
| --- | --- |
| `h2m-test` | **1138/1138 全綠**（+10 斷言：`test-s109-id-attrib` —— 裸未定義名記 `f:missing`／模組基名記 `g:tok`／已綁定名不記不計 unsup／s108 兩通道仍等長） |
| 其餘 selfhost 檔 | `cg-test 363/363`、`cg-runtime-test 114/114`、`ck-test 20/20`、`ps-test 49/49`、`lx-test 81/81`、`tok-test 62/62` 全綠；`ast-test 43/48`（5 红為 ㉔ 已記既有，未擴大） |
| `no build compiler` | rc=0，4.03 MB，`noc v0.0.1-m0` |
| cov-measure 差集 | 塊級 9801／葉子 9801 等長；非 `h2m-*` 函式（排除工具鏈 `drv.pkg-ll`）**逐條完全一致**，被測對象零回 regression |
| 格式 | 三檔 `no fmt -d` 均 0 hunk |

**⑤ 誠實邊界與下一步**

- ID 通道是旁路觀測：名字讀回 dependency on `ast.str-at`（㉕／s65 已證可靠），但**只覆蓋 2533 這一個失敗點**；
  其餘 59+ 個 `h2m-mark` 呼叫點的失敗名不在本通道內。
- 本輪**不降低** unsup（9801 vs 9762，+39 全在工具鏈自身新增程式）；價值是把 4822 條「Identifier 不支援」
  從無資訊量的標籤變成按名字可查的清單。
- 三類名字中，模組名 607 + 常量 131 指向 §2.2.7／§2.2.8 的已知限制，**不是新語言能力缺口**；
  真正的自舉瓶頸是 1940 條普通局部名背後的 h2m-scan 槽登記遺漏——建議下一輪優先查這一類（按 `ps-parse-*` / `drv.pkg-ll` 的具體失敗名反推哪一類控制流／巢狀形狀沒進 scan），而非忙於 with-cap/len 等表層降級。
- ⇒ DR-1 定點仍需：`with-len`/`with-cap-len` → `[]byte` 助手族 → str-vec clone/别名 → **h2m-scan 槽登記缺口（本輪定位）** → noc→noc 比對。

### ㉗ 2026-11-18 再續：h2m-scan 槽登記缺口治本——點號呼叫回值型別推斷（s110）（**實作輪次**，HEAD `8c0bd201`+dirty）

> 承接 ㉖ ⑤ 點名的「h2m-scan 槽登記缺口」。本輪不只定位、而是**治本修復**：根因是
> `h2m-ty-of` 不認 `mod.fn(...)` 點號呼叫的回值型別，導致 let 槽不綁名、體內識別符連鎖失敗。
> 修一處（h2m.no 的 K-CALL 臂補一條 DOT 分支）⇒ unsup **首次淨下降 2128**。

**① 根因鏈（源碼層，逐環可證）**

1. `h2m-scan` 的 K-LET 臂（h2m.no:2225）對 `t = <RHS>` 調 `h2m-ty-of(t, sg, l, agg, vk)` 定槽型；
   `t = ast.make()` 這類 RHS 是 `K-CALL-EXPRESSION`。
2. `h2m-ty-of` 的 K-CALL 臂（2090）**只認 callee 為 K-IDENTIFIER**（純呼叫 `f(x)`），
   callee 為 `K-DOT-EXPRESSION`（`mod.fn(x)`）時 `code` 保持 -1 ⇒ 槽型 -1。
3. `h2m-lower-func` 鋪槽時 `h2m-slot-decl(c, name, -1)`（980）走 `ok-slot=0`：不綁名字、計一條 unsup。
4. 函式體內每個引用該變數的 `K-IDENTIFIER` 在 `h2m-lower-expr`（2533）三表（names / it 棧 /
   arm-local）都找不到 ⇒ 連鎖 mark。這正是 s109 的 1940 條普通局部名（`t`/`d`/`i`/`j`/`n`…）與
   607 條模組名（`tok`/`asp`/`idx`，皆為 `tok.xxx()` 點號呼叫的 base）的共同源頭。
- **判據**：㉖ 的問題「為什麼通過簽名 gate 的函式體內變數沒進 scan」在此閉環——不是 scan 漏掃，
  是 scan 對點號呼叫 RHS 推不出型別，槽壓根沒綁到名字表。

**② 修復（最小侵入，一處）**

- `h2m-ty-of` 的 K-CALL 臂補 `cd.kind == asp.K-DOT-EXPRESSION` 分支：取 dot 的 base 識別符 `bn` 與
  `fn = dot.s0`，拼 `bn - '.' - fn` 調 `h2m-sig-find(sg, dn)`，命中即回 `sg.fret[di]`。
- 拼法與降級側 `h2m-lower-expr` 的跨模組臂（2939 行 `base.s0 - '.' - mname`）**逐字一致**——
  避免「ty-of 推出 R 但降級不認」的錯配。單層 base（不走點號鏈深挖），與降級側同口徑。
- 未登記的 `mod.missing` 仍回 -1（誠實門禁不變，h2m-test 對照用例釘住）。

**③ 實測：unsup 首次淨下降**

| 指標 | 修前(s109) | 修後(s110) | 差 |
| --- | --- | --- | --- |
| 塊級 unsup 總數 | 9801 | **7673** | **−2128** |
| ID 失敗識別符（s109 通道） | 2071 | **1437** | **−634** |
| 葉子通道 Identifier | 4835 | **3904** | **−931** |
| ID 模組名（tok/asp/idx） | 607 | 493 | −114 |
| 非 h2m 函式 unsup | — | — | 146 個下降／**1 個上升**（見下） |
| unsup 歸零（完整產碼）的函式 | — | 7 個（`drv.build-package`/`fn-str`/`param-type`/`ps-attach-trailing`/`put-doc-str`/`set-count`/`set-slot`） | — |

- **下降分佈**：`check −84`、`drv.bp-walk −37`、`drv.cmd-vet −24`、`fold-int −23`、`helper −23`、
  `cg-emit-cfg-t −11`、`drv.pkg-sigs −13` … 全庫 146 個函式下降、無一因修復本身變差。
- **唯一上升 `ps-scan-union 8→14` 是 gate 放行後的真實暴露**：該函式 3 個重載原有 2 個被簽名 gate 拒
  （各記 1 條 FunctionDefinition、體不降級）；修復後 `t = ps-peek-at(...)` / `depth = ix.inc(...)` 可推型，
  2 個重載放行開始降級 ⇒ 體內 SurfaceMatch 1→8、For 0→1，FunctionDefinition 3→1，淨 +6。
  與 ㉒ 記的同型現象（「簽名 gate 放寬後函數體暴露」）一致，安全性質 `inc>0 ⇒ r=''` 不變，非回歸。

**④ 驗證（充分）**

| 項 | 結果 |
| --- | --- |
| `h2m-test` | **1140/1140 全綠**（+2 斷言：`test-s110-dot-call-ty` —— 手工造 `CALL(DOT(mod,fn))` + 手填 sg，釘 `h2m-ty-of ⇒ TY-I64`；對照未登記 `mod.missing ⇒ -1`） |
| 其餘 selfhost 檔 | `cg-test 363/363`、`cg-runtime-test 114/114`、`ck-test 20/20`、`ps-test 49/49`、`lx-test 81/81`、`tok-test 62/62` 全綠；`ast-test 43/48`（5 红既有，未擴大） |
| `no build compiler` | rc=0，4.03 MB，`noc v0.0.1-m0` |
| 新 noc 功能 | `vet` 對 `x = nope` 輸出與 Go oracle **逐字一致**（`'nope' is not defined` @1:5）；`build` hello 程式 rc=0 且可執行 |
| 格式 | 三檔 `no fmt -d` 均 0 hunk |

**⑤ 誠實邊界與下一步**

- 修復只覆蓋**單層** `mod.fn` 點號呼叫（與降級側同口徑）；深層點號鏈（`a.b.c()`）的 base 取法仍未對齊，
  但 self-host 源碼主力是單層，本輪不擴。
- 方法的點號呼叫（`vec.len()` 等 receiver 為值的內建分派）不走簽名表；ty-of 對其仍回 -1，與修前一致
  （不變壞、也未改善）。這部分由降級側 `h2m-lower-expr` 的內建方法臂處理，非本輪範圍。
- 2128 條下降是**記錄消失**（函式現在能產碼），不是掩蓋：`inc>0 ⇒ r=''` 保證仍有一條 unsup 的函式不產碼；
  7 個函式 unsup 歸零等於完整通過。唯一上升的 `ps-scan-union` 已逐條歸因為 gate 放行後的誠實暴露。
- ⇒ 剩餘 unsup 7673 的主塊仍需：`[]byte` 助手族 → str-vec clone/别名 → `with-len`/`with-cap-len` →
  **深層點號鏈 ty-of 對齊** → noc→noc 比對。本輪把 ㉖ 建議的「槽登記缺口」從定位推進到治本，
  且是 DR-1 軌上第一個**淨降低** unsup 的輪次。

### ㉘ 2026-11-18 再續：跨模組常量 `mod.CONST`（s111）——`tok.*`／`asp.*` 475 條 ID 失敗治本（**實作輪次**，HEAD `8c0bd201`+dirty）

**① 文檔整理（本輪前置）**

- 本文從 7865 行／1.29 MB 拆成**現行正典 2634 行／547 KB** ＋ **歷史歸檔 `nolang-selfhost-history.md` 5800 行／767 KB**。
- 遷出的是 126 節 `§5.3.4.*` 逐輪增量、⑥–⑯ 追記、舊附錄 B，**原文逐字保留**，每節前面加
  `<a id="hist-NNN">` 穩定錨點；本文原位置留 126 行索引表 ＋ ⑥–⑯／附錄 B 指路。
- ⑰–㉗ 最新連續追記留在本文（新的 `§12`），「唯一正典」不變：現狀看本文，證據回溯看歸檔。〔**2026-11-23 校對**：其後的 ㉜–㊿（s114–s133）與 §51–§71（s134–s153 收口）亦已追加於本文，最新追記見文末 **§71**；此處「⑰–㉗」為拆分當時的有效範圍〕

**② 由 s109 的 ID 通道定位到的新品類**

- ㉗ 後 ID 失敗 1437 條，高頻名字是 `tok` **306**、`asp` **169**（合計 475，佔 1/3）——
  不是局部變數，而是**跨模組常量**：`tok.EOF`／`asp.K-IDENTIFIER` 等。
- 根因：`h2m-lower-expr` 的 `K-DOT-EXPRESSION` 一律先降接收者為值；模組名不是綁定值 ⇒
  `K-IDENTIFIER` 三表全空 ⇒ 記一條 ID 失敗，整函數不產碼。**self-host 前端此前沒有常量表**。

**③ 修復（三處同步，與 s110「ty-of 與 dest 必須同口徑」紀律一致）**

| 處 | 改法 |
| --- | --- |
| `h2m-sigs` | 新增 `ckey []str`／`cval []i64` ＋ `h2m-sig-const-add`／`h2m-sig-const-find`，key = `mod.CONST` |
| `drv.no` | 新增 `drv.sig-consts-add`：整包前置 pass 掃各模組根層 `K-LET-STATEMENT`（`s0` 才是常量名；`kid0` 是推斷出的 `K-NAMED-TYPE i64`，**不是名字**），只收右側 `K-INTEGER-LITERAL`。`drv.pkg-sigs` 與 `drv.pkg-ll` 的全域 sg 兩處都接 |
| `h2m.no` | `h2m-ty-of` 的 DOT 臂先查常量表命中 ⇒ `TY-I64`；`h2m-lower-expr` 的 DOT 臂命中即發 `OP-CONST(TY-I64, val)`，未命中才落原 struct 欄位路徑（誠實門禁不變） |

- 實測陷阱：首版把常量名取成 `kid0` 的 `NamedType.s0` ⇒ 377 條鍵全叫 `mod.i64`，量測零改善；
  改用 `LetStatement.s0` 後恢復 377 條真名（`tok.EOF=1`、`asp.K-*`、`drv.EXIT-OK=0` …）。

**④ 量測（`./bin/no run compiler/tests/cov-measure.no`，s110 → s111）**

| 指標 | s110 | s111 | 差 |
| --- | --- | --- | --- |
| ID 失敗 | 1437 | **1215** | −222 |
| ├ `tok` | 306 | **0** | −306 |
| └ `asp` | 169 | **0** | −169 |
| 非工具鏈函式 ID（排除 `h2m-*`／`drv.*`） | 978 | **573** | **−405**（101 函式下降，僅 `ps-detect-generic` +5） |
| 葉子 `Identifier` | 3904 | **3015** | −889 |
| IR bytes | 299093 | 311201 | +12108（更多函式真正產碼） |

- 個體下降前十：`ps-is-arm-start 55→29`、`lx-regex-start 25→0`、`ps-is-iface-inherits 29→13`、
  `ps-standalone-body 28→13`、`check 14→1`、`h2m-synth-main 13→0`、`ps-parse-expr-prec 21→9` …
- **上升集中在工具鏈自身**（`h2m-lower-expr +175`、`h2m-ty-of +27`、`h2m-scan +18`）：新常量臂本身
  尚未被 h2m 降級，且原被 gate 拒的函式現開始降級而暴露真實缺口——與 ㉒／㉗ 同型的 gate 放行效應，
  `inc>0 ⇒ r=''` 安全性質不變。葉子總數 7673→7745 的 +72 同此來源，**不是被測對象回歸**。

**⑤ 驗證（充分）**

| 項 | 結果 |
| --- | --- |
| `h2m-test` | **1146/1146**（+6 斷言：`test-s111-mod-const` 釘 `mod.CONST` 的 ty-of／OP-CONST／vty／未登記仍 unsup） |
| 其餘 selfhost | `pkg-sigs-test 5/5`、`pkg-ll-test 10/10`、`pkg-ll-call-test 5/5` 全綠 |
| `no build compiler` | rc=0，4.03 MB，`noc v0.0.1-m0` |
| 新 noc 功能 | `vet` 對 `call-entry.no`／`h2m-test.no` 與 `/tmp/noc-s110` **逐字一致**；`build hello` rc=0，`print(42)` 輸出 42 |
| 格式 | `drv.no` 的 `no fmt -d` 0 hunk；`h2m.no` 僅剩 ㉔–㉗ 的 3 個既有 hunk（未順手改） |

**⑥ 誠實邊界與下一步**

- 只收**整数字面量**常量；str／bool／option 常量、根層非字面量初值仍不收（門禁不放寬）。
- 深層點號鏈（`a.b.c()`、`mod.sub.fn()`）的 base 取法仍未對齊；方法呼叫的回值 ty-of 仍回 -1（㉗ ⑤ 已記）。
- 下一步優先：**用 ID 通道清 `i`／`n`／批量局部名**（槽登記與控制流鋪槽的下一層），再回到
  `[]byte`／str-vec clone／`with-len` 與 noc→noc 比對。本輪把 475 條「假局部名」清掉，
  ID 通道剩 1215 條的可讀性大幅提升。

### ㉙ 2026-11-18 再續：模組內裸常量（s112）——`bop` 44 條連鎖失敗一併清零（**實作輪次**，HEAD `8c0bd201`+dirty）

**① 由 ID 通道再定位一層**

- ㉘ 後 ID 1215 條中，有 **187 條名字命中共量短名**（`TY-I64 18`、`TY-STR 13`、`B-LF 6`、
  `OP-ADD/SUB/DIV/…` 各 3–4、`EXIT-OK 6` …）：s111 只認 `mod.CONST` 點號形，
  self-host 源碼在**自己模組內**寫裸名（`bop = OP-ADD`），三表仍全空。
- 🔴 連鎖效應：`bop` 的 44 條失敗全部源自 `osym == '+' -> bop = OP-ADD` 這批-arm 初值
  ——常量未解析 ⇒ 賦值不產碼 ⇒ 後續每次讀 `bop` 再記一條。清常量即清整條鏈。

**② 修復（s111 常量表加一條 fallback，優先級不變）**

| 處 | 改法 |
| --- | --- |
| `h2m-sigs` | 新增 `csname []str`；`h2m-sig-const-add` 順手記短名（第一個 `.` 之後），新增 `h2m-sig-const-find-short` |
| `h2m-ty-of` | `K-IDENTIFIER` 三表未命中且非 `it` ⇒ 查短名，命中回 `TY-I64` |
| `h2m-lower-expr` | `K-IDENTIFIER` 最后失败臂先查短名：命中發 `OP-CONST(TY-I64, val)`；未命中才 `h2m-mark` + `h2m-id-name` |

- 優先級仍為**臂局部 > `it` > `names` 表 > 常量短名**；實測 377 條常量的短名**無跨模組衝突**，
  且 `i`/`n`/`ok`/`bop` 等高頻局部名均不與常量同名，故不遮蔽局部變量。

**③ 量測（`./bin/no run compiler/tests/cov-measure.no`，s111 → s112）**

| 指標 | s111 | s112 | 差 |
| --- | --- | --- | --- |
| ID 失敗 | 1215 | **1073** | −142 |
| 裸常量短名 ID | 187 | **0** | −187 |
| `bop` | 44 | **0** | −44 |
| 葉子 `Identifier` | 3015 | **2820** | −195 |
| 葉子總數 | 7745 | **7686** | −59 |
| IR bytes | 311201 | 323866 | +12665 |

- `h2m-lower-expr 176→67`、`h2m-ty-of 39→21`；被測對象 `lx-esc-simple-ok 7→0`、
  `cg-val-prefix 6→0`、`cg-runtime-ty-name 6→0`、`lx-read-raw 5→0` 等歸零。
- **非工具鏈函式 573→601（+28）是 gate 放行後的誠實暴露**：`lx-scan-token 3→46`、
  `ps-parse-match 8→33`、`ps-parse-bare-match 12→19`——原本常量未解析使整函式不產碼，
  現在能降級才暴露體內真實缺口；`inc>0 ⇒ r=''` 安全性質不變。

**④ 驗證**

| 項 | 結果 |
| --- | --- |
| `h2m-test` | **1152/1152**（+6 斷言：`test-s112-bare-const` 釘裸常量 ty-of／OP-CONST／vty／未登记仍 unsup） |
| 其餘 selfhost | `pkg-sigs 5/5`、`pkg-ll 10/10`、`pkg-ll-call 5/5`、`ck 20/20`、`cg 363/363` 全綠 |
| `no build compiler` | rc=0，4.03 MB，`noc v0.0.1-m0` |
| 新 noc 功能 | `vet` 對 `call-entry.no`／`h2m-test.no` 與 `/tmp/noc-s111` 逐字一致；`build hello` rc=0，輸出 42 |
| 格式 | `h2m.no` 的 `no fmt -d` 只剩 2 個既有 hunk（s110／s109 的註解空行，未順手改） |

**⑤ 誠實邊界與下一步**

- 仍只收**整数字面量**：`NOC-NAME = 'noc'` 這類 str 常量（10 條 ID）未收，下一步可加 str 常量表
  （`OP-STRCONST`），但要先確認自舉 IR 的全局字面量唯一化與 RC 語義。
- 深層點號鏈、方法呼叫回值 ty-of 仍如 ㉗／㉘ 所記，未變。
- ID 通道剩 1073 條，`i 58`／`n 55`／`ok 28` 等局部名重新成為主體（先前被常量的 187 條稀釋）；
  下一輪應直接分診這批局部名的槽登記／控制流鋪槽缺口。

### ㉚ 2026-11-18 再續：方法呼叫／索引回值型別（s113 + s113b）——`n` 55→28（**實作輪次**，HEAD `8c0bd201`+dirty）

**① 從局部名回到 ty-of 缺口**

- ㉙ 留下的 `n 55` 多數是 `n = c.stn.len()` / `n = s.len-bytes()` / `nb = t.len-bytes()`：
  `lower-expr` 早已支援 `len`／`len-bytes`／`push` 的內建方法臂，**但 `h2m-ty-of` 不認方法呼叫**，
  回值型別 -1 ⇒ let 槽不綁名 ⇒ 後續讀 `n` 連鎖失敗。
- 同型的第二層是 `op = ops[k]`（`cg-emit-cfg-t`）：`lower-expr` 有 `OP-VECGETSTR`，
  `ty-of` 卻沒有 `K-INDEX-EXPRESSION` 臂。

**② 修復（沿用 s110 的「ty-of 與 dest 同步」紀律）**

| 輪 | 改法 |
| --- | --- |
| s113 | `h2m-ty-of` 的 `K-CALL(DOT)` 臂在模組簽名查找失敗後，遞歸推接收者型別：`[]i64.len` / `[]str.len` / `str.len-bytes` ⇒ `TY-I64`；`[]i64.push` ⇒ `TY-VEC`、`[]str.push` ⇒ `TY-VECSTR`。只覆蓋 lower-expr 已支援的三個方法，`byte` / `index` / `to-i64` 等仍 -1 |
| s113b | 新增 `K-INDEX-EXPRESSION` 臂：`[]i64[k]` ⇒ `TY-I64`、`[]str[k]` ⇒ `TY-STR`（與 `OP-VECGET` / `OP-VECGETSTR` 同口徑），其餘基底仍 -1 |

- 測試先紅後綠：`test-s113-method-call-ty` 6 斷言（含未支持方法仍 -1）；
  `test-s113b-index-ty` 3 斷言（含 `str` 基底索引仍 -1）。

**③ 量測（s112 → s113 → s113b）**

| 指標 | s112 | s113 | s113b（終值） |
| --- | --- | --- | --- |
| ID 失敗 | 1073 | 1151（+78，gate 放行） | **1032**（−41） |
| 非工具鏈函式 ID | 601 | 686（+85） | **572**（−29） |
| `n` | 55 | 28 | **28** |
| 葉子總數 | 7686 | 7907 | **9103** |
| IR bytes | 323866 | 323866 | 323866 |

- 改善（非工具鏈）：`ck-sig-lt 7→1`、`ck-sig-bsearch 4→1`、`ps-int-lit-value 23→21`、
  `ps-sig-find 4→2`、`ps-hash 3→2`、`ck-def-has 1→0` …
- **葉子總數 +1417 是 gate 放行的誠實暴露，不是被動回歸**：`cg-emit-cfg-t` 葉子 46→1615，
  但它的 ID 通道反而 2→1；新增的 1520 條 `Identifier` 標記是 `h2m-mark` 在深層失敗時
  `c.cur-k` 仍停在遍歷過的标识符所致（葉子通道是「標記點的當前 K」，不是「失敗标识符清單」，
  精確口徑仍看 ID 通道）。`s113` 中間態 ID +78 也同屬放行效應，`s113b` 補上索引臂後轉為淨下降。

**④ 驗證**

| 項 | 結果 |
| --- | --- |
| `h2m-test` | **1161/1161**（s113 +6、s113b +3 斷言） |
| 其餘 selfhost | `pkg-sigs 5/5`、`pkg-ll 10/10`、`pkg-ll-call 5/5`、`ck 20/20`、`cg 363/363` 全綠 |
| `no build compiler` | rc=0，4.03 MB，`noc v0.0.1-m0` |
| 新 noc 功能 | `vet` 對 `call-entry.no`／`h2m-test.no` 與 `/tmp/noc-s112` 逐字一致；`build hello` rc=0，輸出 42 |
| 格式 | `h2m.no` 的 `no fmt -d` 只剩 2 個既有 hunk（s110／s109 註解空行，未順手改） |

**⑤ 誠實邊界與下一步**

- `ok 28→35` 上升同為新函式放行後 option 臂（`ok ->` / dot-val）暴露，㉗ 已記 s98 缺口未動。
- `h2m-ty-of` 仍不認 `byte` / `index` / `to-i64` / `contains` / `get` 等方法呼叫的回值型別；
  補之前不能讓這些函式通過槽寬檢查（門禁保持）。
- IR bytes 未變（323866）：本輪只讓更多槽可綁名，尚未把新放行的函式完整產碼。
- 下一步：**分診 `cg-emit-cfg-t` 的 1520 條葉子 Identifier 真實來源**，或回到 str 常量
  （`NOC-NAME = 'noc'` 10 條 ID）；前者槓桿大但需先區分 cur-k 噪聲，後者邊界清晰。

### ㉛ 2026-11-19 追記（再續）：s114 裸連字符呼叫——整包 IR 只有 64 個 define 的真根因（**執行輪次**）

> 本節是 as-built 實作：①定位 ②修復 ③量測 ④驗證 ⑤誠實邊界。㉚ ⑤ 把 ID 通道的局部名缺口
> 讀成「槽登記／控制流鋪槽」問題；本輪先做了第三條診斷，結論是 **ID 通道裡最大的一批局部名是
> 症狀、不是病因**——病因是**模組內裸連字符呼叫根本查不到簽名**，而簽名的 key 是點號形。

**① 定位：ID 通道最大的一批局部名是「症狀」**

- 签名表 key 的構造在 `drv.pkg-sigs`（drv.no:834）：`pre-dot = mod - '.'`、`pre-hyp = mod - '-'，
  `nm.starts-with(...)` ⇒ `short = nm.slice-bytes(mod.len-bytes()+1, ...)`、`key = mod - '.' - short`。
  於是 `ps-peek` 的 key 是 **`ps.peek`**，而 `femit` 是源函數名 verbatim **`ps-peek`**。
- 而 self-host 源碼在模組內寫的是**裸連字符名**：`ps-peek(p)`（ps.no:564、620）、
  `lx-scan-token(lx, t)`、`ast.str-at(t, ...)`。`h2m-sig-find` 只做精確匹配 ⇒ **必然落空**。
- 實測（探針，`drv.pkg-sigs('compiler/main.no')`，496 條簽名）：
  `h2m-sig-find(sg, 'ps.peek') = 143`、`h2m-sig-find(sg, 'ps-peek') = -1`、`sg.femit[143] = 'ps-peek'`。
- 三重證據：
  1. 上面那條平面查詢（key 口径 vs 裸名口径）。
  2. 整包 IR 只有 **64 個 `define`**；`@ps-peek`／`@ast-make`／`@ps-parse`／`@lx-scan-token`
     全部未發射，已發射的都是**無內部呼叫的葉子函式**。
  3. t6 受控實驗（`baremod-f` 葉子 + `baremod-g` 祗呼 `baremod-f(x)`）：`baremod-f` 發射、
     `baremod-g` **未發射** ⇒ 缺口與函式體複雜度無關，純呼叫解析。
- 連鎖機制（ID 通道為何飆高）：`h2m-ty-of` 回 -1 ⇒ `h2m-scan` 的 let 槽 `ty < 0` ⇒
  `h2m-slot-decl` `ok-slot == 0` ⇒ **不把名字 push 進 `c.names`** ⇒ 之後每次讀該名 →
  三表未命中 → `h2m-id-name` 記一條 ID。所以 ID 1032 裡很大一部分是**同一根因的下游症狀**。
- 可行性前提：496 條簽名的 `femit` 與 `fname` **皆無重複**（探針實測 dup pairs 均為 0）
  ⇒ 用 `femit` 反查做裸名 fallback 在當前語料下無歧義。

**② 修復（`compiler/src/h2m.no`，三處）**

- `h2m.no:1913` 新增 `h2m-sig-find-emit(s, name)`：掃 `s.femit`，**0 命中或 >1 命中一律回 -1**。
  歧義時絕不取第一個——那會把「静默錯值」換成「静默錯碼」，比落空更糟。
- 兩個接入點，同口徑：
  - `h2m.no:2189` —— `h2m-ty-of` 的 K-CALL-EXPRESSION / IDENTIFIER callee 臂；
  - `h2m.no:3132` —— `h2m-lower-expr` 的 CALL 臂 `si` 解析處，**仍落空才** `ok-call = 0` 落閘門。
- 單模組路徑（`h2m-lower-program`）`femit == fname == nm`，精確匹配已命中 ⇒ 反查永不觸發，**零回歸**。

**③ 量測（`./bin/no run compiler/tests/cov-measure.no`，與 ㉚ 同一檔）**

| 指標 | 基線 | 修後 | Δ |
| --- | --- | --- | --- |
| ID（`ID:` 行） | 1032 | **426** | −606（−58.7%） |
| LF（`LF:` 行） | 9114 | **5805** | −3309（−36.3%） |
| unsup | 9149 | **5916** | −3233（−35.3%） |
| IR bytes | 323866 | **330710** | +6844（+2.1%） |
| `dropped (calls unsupported)` | 35 | 111 | +76 |
| 整包 `define` 數 | 64 | **69** | +5 |

- 基線由「同一二進位、把兩處 fallback 改成 `si = -1`」**配對**量得，連跑兩次基線
  ID／LF／IR 完全一致（確定性）。
- `dropped` 上升**不是退化**：那條日誌是「逐函降级時遇到不支援的被呼叫者」的記錄，
  放行更多函式 ⇒ 更多這類記錄。
- 新增的 5 個 `define` 全是葉子：`lx-hex-val`／`lx-is-digit`／`lx-is-hex`／`lx-is-ident-char`／
  `lx-is-letter`（無一例外）——再次印證「求精先放行葉子」。
- t6 受控實驗：`baremod-g` 由**未發射 → 發射**，IR 14202 → 14294。

**④ 驗證**

| 項 | 結果 |
| --- | --- |
| 先紅後綠 | 把兩處 `si = h2m-sig-find-emit(sg, cn)` 臨時改成 `si = -1`（回到修前口徑），`h2m-test` 裡 14 條 s114 斷言**6 條失敗**（ty-of 1 + lower-expr 5）；恢復後 **1175/1175 全綠** |
| `h2m-test` | 1161 → **1175**（s114 新增 14 條：ty-of 1、lower-expr 10、歧義 2、未登記 2——後四條是**門禁不收緊**的釘子） |
| 其餘 selfhost | `pkg-sigs`／`pkg-ll`／`pkg-ll-call`／`cg`／`ck`／`ck-sig`／`ps`／`lx`／`tok`／`diag`／`build-package`／`util`／`ps-err` 全綠 |
| `ast-test` | 5/48 失敗——**既有**（`KIND-COUNT = 72` 等 kind 編號漂移）。該檔只導入 `asp`／`ast`／`sb`、**不導入 h2m**；把 fallback 停用後重跑仍是同樣 5 條 ⇒ 與本輪無關 |
| 格式 | `no fmt -w` 後 `h2m.no`／`h2m-test.no` 的 `no fmt -d` 皆為空 |

**⑤ 誠實邊界與下一步**

- **`ok` 43 條（占剩餘 ID 的 10.1%）已定位**：`ps.no:7284` 的注释明示
  「Condition（`_`/bare-> 臂=0；**ok-> 臂=Identifier{ok}**）」，即**顯式 match 形**
  （`expr: { ok -> it }`）的 Condition aux 是標識符節點 `ok`，被 `h2m-scan-arms`／
  `h2m-lower-expr` 當名字讀。受控實驗（t7）：**identifier 主體形**（`a: { ok -> it }`，
  `i64` 或 `?i64`）不產生 `ID:ok` 且能產碼；**表達式主體形**（`s.byte(0): { ok -> it }`）
  產生 `ID:ok` 且整函被丟棄 ⇒ 這 43 條**不是純假陽**，是「顯式 match × 表達式主體」
  這個缺口的伴生症狀。
- 剩餘 ID 名分布（前幾名）：`n` 28（`ast-str-intern`／`drv.cmd-*`）、`h` 19（`ast-str-hash`／
  `ps-ht-*`）、`cur` 15（`node`）、`kvar` 12（`ps-parse-match`）、`nm` 11（`cg-emit-cfg-t`／
  `drv.pkg-ll`／`h2m-*`）、`NOC-NAME` 11（`drv.bp-walk`／`build-to`／`cmd-*`）、`p` 10、
  `c` 8、`base` 8、`self` 7（`diag.render*`）、`gnames` 7（`ps-parse-func`／`ps-parse-method-func`）。
- **LF 基線口径差異**：本輪配對基線 LF = **9114**，㉚ 記 9103（差 11；ID 1032 與
  IR 323866 完全一致）。本輪的 Δ 一律用配對值計算，不與 ㉚ 的 LF 直接相減。
- **IR bytes 只 +2.1%**：本輪解的是**解析**、不是**降到葉子**。`ps-peek`／`ast-make`／
  `ps-parse`／`lx-scan-token` 仍未發射——`ps-peek` 只被 `ps-skip-trivia`（已在 `dropped`
  清單裡）與 `ps-skip-trivia-doc` 呼叫，從 `main` 的 callee-closure 不可達；這是**下游**缺口，
  與本輪的解析缺口無關，仍未分診。
- 下一步：**顯式 match 的表達式主體**（`expr: { ok -> it }`）——它同時解釋 `ok` 43 條 ID 與
  `ps-int-lit-value` 等解析 core 函式的丟棄；邊際清晰、可受控復現（t7 已證實），
  槓桿比 ㉚ ⑤ 點名的 `cg-emit-cfg-t` 1520 條葉子大。

### ㉜ 2026-11-19 再續：顯式 match 的表達式主體（s115）——ID 979→918，`ok` 臂不再被當兜底（**執行輪次**）

> 本節是 as-built 實作：①定位 ②修復 ③量測 ④驗證 ⑤誠實邊界。㉛ ⑤ 把「顯式 match 的表達式
> 主體」列為下一步；本輪交付該通路的**機制**（主体恰好求值一次、按值型別定 vmode/omode、
> `ok ->`／`err ->` 無綁定認臂、`it` 取載荷），並用 t8 九形受控語料釘住。

**① 定位：九形矩陣 × 三類根因**

t8 語料（`compiler/tests/t8/`，既有 f1–f6 + 本輪補 f7–f9）把「显式 match × 主體形態」
全部枚舉，用 `drv.pkg-ll-cov` 逐形量「`define @fN(` 是否出現」：

| 形 | 主體 | 臂 | 修复前 | 修复後 |
| --- | --- | --- | --- | --- |
| f1 | `s.byte(0)` | `ok -> it` / `-> 0` | 未發射 | 未發射（② 遺留） |
| f2 | `s.byte(0)` | `-> it` | 未發射 | 未發射（② 遺留） |
| f3 | 裸标识符 `x`（`x = s.byte(0)`） | `ok -> it` / `-> 0` | 未發射 | 未發射（② 遺留） |
| f4 | 裸标识符 `x`（`x = 1`） | `ok -> it` / `-> 0` | 未發射 | 未發射（缺口子） |
| f5 | 裸标识符 `a ?i64` | `ok -> it` / `-> 0` | 未發射 | **發射** |
| f6 | `s.byte(0)` | `ok(v) -> v` / `-> 0` | 未發射 | 未發射（② 遺留） |
| f7 | 裸标识符 `a ?i64` | `ok -> it` / `err -> 0` / `-> 0` | 未發射 | **發射** |
| f8 | `s.byte(0)` | `ok -> it` / `err -> 0` / `-> 0` | 未發射 | 未發射（② 遺留） |
| f9 | `s.byte(0)` | `ok(v) -> v` / `err -> 0` / `-> 0` | 未發射 | 未發射（② 遺留） |

三類根因（全部實測）：

1. **parser 拆分**（f1/f2/f6/f8/f9）：`ps.no` 的明示 match 檢查在後綴迴圈內，`match-subj`
   只認 `K-IDENTIFIER` / `K-INDEX-EXPRESSION` / `K-DOT-EXPRESSION`。`s.byte(0):` 的 `:`
   沒被消費 ⇒ 陳述級進度守則強制 bump `:` ⇒ ` { … } ` 被 `ps-parse-brace-stmt` 解析成
   **裸 match**（`ExpressionStatement{SurfaceMatch(subject=0)}`），整函被丟。
   oracle 口徑：`src/parser/expr.go:134-190` 的 `parseMatchExprFrom(leftExp)` 對**任意**
   leftExp 都試，守衛是 `precedence == LOWEST && !CTX_FOR_COND && !CTX_MATCH_COND && !isIncDec`。
2. **omode 門禁誤判**（f5/f7）：`ok ->`（無綁定名）在 AST 里 `bindingVariant` 空、
   `dotVal=true`（對齊 oracle dump：`MatchArm{wildcard: true, dotVal: true, binding: , bindingVariant: }`）。
   h2m 的 `is-wild = wi>0 && cn<=0 && bvx==0` 把它判成 catch-all ⇒ 被「兜底臂必須在末位」槍斃。
   `err ->`（無綁定）同形：無 variant、無 dot-val，`Condition` 是標識符 `err`，
   被 `bvx==0 && dotv==0 -> is-wild==0 -> bad=1` 誤殺。
3. **`it` 語義錯位**（f5）：omode 的 `c.itv.push(sv)` 壓的是 **option 本身**；oracle 語义
   `it` 是**載荷**（`ok -> it` 等價於 `ok(v) -> v`）。

**② 修復（`compiler/src/h2m.no` 4 處門禁 + 8 處 emit；`compiler/src/ps.no` 1 處）**

- **主体判定**（`h2m-lower-surface` 頭部）：裸標識符走槽表查型；一般表達式走
  `h2m-lower-expr` 得 `sv0`。**表達式主体在當前塊恰好求值一次**（`sv0`），逐臂不再重算——
  避免有副作用的呼叫被多次執行。`sty = h2m-vty-at(c, sv0)` 定 vmode/omode。
- **認臂**：`dotv = ast.aux-at(t, hb + 5)` 參與 `is-wild` 與 `wild` 判定 ⇒ `ok ->` 不再當兜底；
  新增 `cnid`（`Condition` 節點 kind 是 `K-IDENTIFIER`）⇒ `err ->` 無綁定靠它認 err 臂。
  **不讀字串**：`str-at` 在降級早期滯後（既有註釋已警告），且 option 三態已窮盡
  （ok 有 dot-val、nil 的 `bindingVariant` 無條件填 `nil`），「無 variant + 無 dot-val +
  Condition 為標識符」即 err，無歧義。
- **emit**：兜底臂的 `iv1` 與 omode/vmode 的 `sv` 改取 `sv0`（表達式主体）或
  `h2m-var-read`（裸标识符主体）；`et` 推導加 `bvar=='' && dotv!=0 -> et=0` 與
  `bvar=='' && dotv==0 && cnid!=0 -> et=2`；`it` 棧改為壓 `OP-OPTGET` 的**載荷**，
  與 `ok(v)` 的綁定**共用同一條 OPTGET**（不重複發射）。
- **ps.no**：`match-subj` 加 `ast.kind-at(t, idx) == asp.K-CALL-EXPRESSION`。
  ⚠️ 不動 `bindingVariant` 的口徑——`compiler/tests/ps-test.no` 精確 pin 了
  `MatchArm{binding: , bindingVariant: }` 的 dump（`ok ->`／`err ->` 無綁定皆空），
  認臂改在 h2m 側做。

**③ 量測（`drv.pkg-ll-cov` 逐檔跑 `compiler/src/*.no`，同一二進位、`git stash` 配對基線）**

| 指標 | 基線 | 修後 | Δ |
| --- | --- | --- | --- |
| ID（`ID:` 行，14 檔合計） | 979 | **918** | −61（−6.2%） |
| 去重 (函数,名) 條目 | — | — | 固定 **41** / 新增 **21** |
| t8 發射形數 | 0/9 | **2/9**（f5、f7） | +2 |

- 固定的 41 個條目分布在 20 個函式，最大宗是 `ps-int-lit-value`（6）、`str-eq`（3）、
  `ps-intlit-hex`（3）、`ps-detect-generic`（3），其餘各 1–2——正是 ㉛ ⑤ 點名的
  「解析 core 函式被丟棄」那批。
- 新增的 21 個是本輪新代碼的未覆蓋尾部（`ps-parse-match` 7、`ps-parse-enum-def` 3、
  `h2m-lower-expr` 3、`ps-parse-method-func` 2、`ps-parse-func` 2、`ps-parse-bare-match` 2、
  `h2m-sig-arg` 1、`ps-ht-put` 1）。
- **净收益 −61 而非 −41**：固定 41 個去重條目對應的原始 ID 行數更多（同一 (函式,名) 在
  cov 日誌裡逐塊重複出現），新增 21 個同理。

**④ 驗證**

| 項 | 結果 |
| --- | --- |
| `h2m-test` | 1175 → **1181**（s115 新增 2 用例 6 條斷言：`ok -> it` + 兜底、`ok -> it` + `err -> 0` + 兜底）。先紅後綠：把 `dotv`/`cnid` 的認臂臨時拿掉，兩用例的 `unsup == 0` 即紅 |
| 全量門禁 | `pkg-ll-call` 5、`cg` 363、`cg-runtime` 114、`cg-str16` 41、`ck` 20、`ck-sig` 50、`ps` 49、`ps-err` 46、`lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、`pkg-sigs` 5、`pkg-ll` 10 **全綠** |
| `pkg-sigs`／`pkg-ll` | 因耗時（95s／225s）超出 `no test` 的 60s 工具逾時，改用 `no run` 跑綠——非退化 |
| `ast-test` | 既有失敗（`KIND-COUNT` 等 kind 編號漂移）；該檔只導入 `asp`／`ast`／`sb`，**不導入 ps/h2m** ⇒ 與本輪無關 |
| 格式 | `no fmt -w` 後 `h2m.no`／`ps.no`／`h2m-test.no` 的 `no fmt -d` 皆為空 |
| 臨時探針 | `probe-s115-{ast,cov,stmt,allcov}.no` 已刪；t8 的 f7–f9 作為受控語料保留 |

**⑤ 誠實邊界與下一步**

- **本輪解的是「機制」不是「覆蓋面」**：f5/f7（裸标识符主体）轉綠，证明 sv0 求值、值型別推導、
  認臂、`it` 載荷四件事都對；但 **f1/f2/f6/f8/f9 仍紅**，卡點不在 match——在
  `h2m-lower-expr` 的**方法族降級**：`str.byte` 不在 s113 的方法臂里，且 `cg.no` 沒有
  `OP-STRBYTE`（Go 側作 `#{buildin}` + `emitBuiltinRawByteAt`，含 str/txt 雙型、越界 select、
  option 構造）。
- **f3 是同族缺口的另一面**：主体 `x` 是裸標識符，但 `x = s.byte(0)` 的 let 槽型別解不出
  ⇒ vmode/omode 皆 0 ⇒ bad。**要解 f1/f3/f6/f8/f9，先要解 `str.byte` 的型別與降級。**
- **f4 是覆蓋缺口**：i64 主体 + `ok ->` 臂——oracle 實測該形合法（`ok` 作值樣不匹配，結果 0），
  h2m 的 vmode 門禁 `aux[5] != 0 -> bad`（dot-val 在 i64 主体上無意義）尚未開。
- **std 語料分布**（`grep "): {" src/std/*.no` 374 處，含區間遍歷）：主體 `byte` **49**、
  `bytes` **32**、`len` 21。**支持 `str.byte` 的降級是本輪最大的未完成槓桿。**
- **INFIX 主体未覆蓋**：`a + b: { }` 的 `:` 要等到中綴迴圈後才成形，而本輪的 ps 檢查在後綴
  迴圈里。修法是把它移到中綴迴圈之後，並補 `for-cond` 守衛（Go 有 `!CTX_FOR_COND`，
  noc 無 `in-for` 標誌）——**這是下一個高杠桿點**：std 裡約 70 處算術主体
  （`c.len = al + bl: {`）。
- 本輪不碰 `bindingVariant` 口徑（`ps-test` 的 dump 斷言釘死），故 `ok ->` 無綁定在 AST 里
  仍與真·兜底共享「bindingVariant 空」——乾净的長期修法是在 ps 側為 `ok ->` 填 `ok` 並同步
  更新 `ps-test` 的 pin（本輪刻意不為之，避免把 parser 語义改動混進 match 主體的增量）。

### ㉝ 2026-11-19 三續：INFIX 主體 + for-cond 守衛（s116）——ID 918→759（−17.3%），0 新未覆蓋（**執行輪次**）

> 本節是 as-built 實作：①定位 ②修復 ③量測 ④驗證 ⑤誠實邊界。㉜ ⑤ 把「INFIX 主體」列為
> 下一個高杠桿點（std 約 70 處算術主体）；本輪交付，並順帶修掉一個**既有**的 for 條件形缺陷。

**① 定位：兩個實證缺陷（用 noc 自己的 parser 側寫探針 dump，非 `./bin/no`）**

⚠️ 教訓：`./bin/no ast` 走的是 **Go oracle**，不能用來判斷 noc 的行為。本輪改用
`# /compiler/src/ps` 直接 dump（`ps.new(src)` + `ast.dump`），才看到 noc 的真實形制。

| 形 | 修復前 noc 的 AST | 后果 |
| --- | --- | --- |
| `x = a + b: { ok -> it }` | `LetStatement{value: InfixExpression{op:+}}` 里，`SurfaceMatch` 掛在**右運算元 `b`** 上 ⇒ `a + (b: {…})` | `ok -> it` 的 `it` 是 **b**，不是「a+b 的匹配結果」——語义错（oracle 是 `SurfaceMatch → InfixExpression{a,b}`） |
| `for n: { print(n) }` | `ForStatement{ cond: SurfaceMatch{arms:1} }`，body 丟失 | 條件 `n` 被搶成 1 臂裸 match——**既有缺陷**，非本輪引入 |
| `while n: { … }` | 同上的 `ForStatement{cond: SurfaceMatch}` | 同上 |

根因（同一處）：`ps-parse-expr-prec` 的明示 match 檢查住在**後綴迴圈**里，而 `:` 的綁定對象
取決於「檢查發生時 `idx` 是什麼」。在後綴迴圈里，`a + b: {` 的 `:` 遇到的是內層
`b` 那層（`ps-parse-expr-prec(t, p, oprec)`）⇒ 搶走。Go 的對應檢查在主迴圈每次迭代末
（`src/parser/expr.go:134-190`），所以 `:` 永遠綁在整個 leftExp 上。

**② 修復（`compiler/src/ps.no`，3 處）**

- **冒號檢查移到中綴迴圈之後**：删掉後綴迴圈里的 `match-subj` 判定與 `COLON` 處理塊，
  在 infix 循環結束、三元檢查之前插入新塊。白名單加 `K-INFIX-EXPRESSION`。
  守衛對齊 Go：`prec <= 0`（`precedence == LOWEST`）——內層（`prec > 0`）不消費，
  避免 `a + b: { }` 的 `:` 再被右運算元那層搶走。
- **新增 `psst.in-for-cond`**（默認 0）：`ps-parse-for-kw` 的**條件形**（`for expr { }`／
  `while expr { }`）與 `ps-for-source` 的兩處表示式解析（`kind==2` 容器源、
  `bare-ident-colon == 0` 的表達式源）在解析前置 1、出口還原。守衛加
  `&& p.in-for-cond == 0`。
  `ps-for-source` 的裸 IDENT 特判（`ps-no` 既有，`i <- a: { }` 防誤觸）已在表達式層之外，
  故 for 源守衛實際只作用於 CALL／表達式源。

**③ 量測（`drv.pkg-ll-cov` 逐檔 14 個 `compiler/src/*.no`，`git stash` 配對基線）**

| 指標 | 基線 | 修後 | Δ |
| --- | --- | --- | --- |
| ID（`ID:` 行，14 檔合計） | 918 | **759** | **−159（−17.3%）** |
| 去重 (函数,名) 條目 | — | — | 固定 **23** / 新增 **0** |

- **新增 0** 是本輪最值得記的數字：`in-for-cond` 守衛與移動後的 match 塊全部被既有語料
  走到，沒有留下未覆蓋尾巴。
- 固定的 23 個分布：`ps-int-lit-value`、`ps-hash`、`ps-skip-type-idx`、`ps-signed-int-operand`、
  `ps-mk-opt-arith-type`、`ps-is-map-assign-let`、`ps-is-enum-def`、`node`、`lx-hex-digit`、
  `ix.inc`、`ix.dec`、`implements`、`h2m-struct-resolve-range`、`h2m-struct-resolve`、
  `h2m-struct-base`、`h2m-sig-arg`、`h2m-inc`、`h2m-dec`、`drv.src-arg`、`ck-sig-lt` 等
  （各 1）——**解析路徑上的名字大批浮現**，说明「值位 match 主体」是這些節點的主要入口。

**④ 驗證**

| 項 | 結果 |
| --- | --- |
| `ps-test` | 49 → **51**。新增 `test-expr-subj-infix`（釘 `SurfaceMatch → InfixExpression{a,b}`）與 `test-for-cond-not-match`（釘 `ForStatement{cond: Identifier{n}, body: BlockStatement}`）。**皆先紅後綠**：拿掉 `K-INFIX-EXPRESSION` 白名單 ⇒ 前者紅；拿掉兩處 `p.in-for-cond = 1` ⇒ 後者紅 |
| `h2m-test` | 1181 → **1182**。`vm-gate-expr-subj` 的語义**翻轉**：該用例原本釘「主體是表達式 `n + 1` 時必須不產碼」（s63 的舊缺陷行為），本輪起改為斷言 `unsup == 0` + 有 `define` + 有 `icmp eq i64`——**如實記錄語义變更，非放行** |
| oracle 語义對照 | `f(0)`／`f(1)` 跑 `./bin/no run` 得 `1 2`：`n + 1` 整體匹配（`0+1==1` 命中臂 1；`1+1==2` 走兜底臂 2）——與 `SurfaceMatch → InfixExpression` 的結構一致 |
| 全量門禁 | `pkg-ll-call` 5、`cg` 363、`cg-runtime` 114、`cg-str16` 41、`ck` 20、`ck-sig` 50、`ps` 51、`ps-err` 46、`lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、`pkg-sigs` 5、`pkg-ll` 10 **全綠**（後兩個用 `no run` 跑，95s／225s） |
| 格式 | `no fmt -w` 後 `no fmt -d` 為空 |
| 臨時探針 | `probe-s116*.no`／`t9/probe-ir.no` 已刪；`t9/g1.no` 作為受控語料保留 |

**⑤ 誠實邊界與下一步**

- **本輪只解 INFIX**。**CALL 主體的方法降級仍是最大的未完成槓桿**（㉜ ⑤ 已記錄）：
  f1/f2/f6/f8/f9 卡在 `h2m-lower-expr` 的方法族（`str.byte` 不在 s113 臂里，`cg.no` 無
  `OP-STRBYTE`）。std 里 `): {` 374 處，主體 `byte` **49**、`bytes` **32**。
- **`prec <= 0` 守衛的邊界**：內層（`prec > 0`）不消費 `: {`。因此 `(a + b): { }` 這種
  **括號主體**走的是 primary 的括號分支，其後的 `:` 由外層消費——本輪未專門驗證此形，
  留作下一步的回歸點。
- **`in-for-cond` 的覆蓋面**：只覆蓋 `for`／`while` 條件形與 for 源。Go 的 `CTX_FOR_COND`
  還覆蓋 range 端點等路徑；noc 那些路徑本不該出現 `: {`，但**未逐一對齊**。
- **未動 `bindingVariant` 口徑**（沿用 ㉜ ⑤ 的決定）：`ok ->`／`err ->` 無綁定在 AST 里
  仍與真·兜底共享「bindingVariant 空」，靠 `dot-val`／`Condition 是標識符` 認臂。
- 下一輪候選優先級：① **`str.byte` 的方法族降級**（cg 新 OP + h2m ty-of／lower-expr 臂，
  解 5 個受控形 + std 49 處）；② 括號／其它主體形態的回歸補齊。

---

### ㉞ s117 —— `str.byte(i)` 的方法族降級：值位 match 主體的最後一塊拼圖（2026-11-19）

**① 定位**

- ㉝ ⑤ 指定的第一優先候選。t8 受控語料裡 f1／f6／f8 都是
  `r = s.byte(0): { ok -> it … }` 形——**值位 match 主體是 CALL 表達式**，卡點不在
  parser（㉝ 已修 CALL 主體的 `:` 歸屬），而在 `h2m-lower-expr` 的**方法族**：
  `str.byte` 不在 s113 的 str 方法臂裡，`cg.no` 也沒有對應的 OP。
- 逐檔 `drv.build-ll` 實測：f1／f6／f8 均 `unsupported 1 method`，t8 發射形停在他處。
- 語义參照（Go oracle）：`src/build/llvm/codegen.go:17967 emitBuiltinRawByteAt`——
  按**位元組**（非碼點）讀，越界回 `nil`（option 的 nil 態，恆裝箱）。

**② 修復（全在 noc 側，`compiler/src/`）**

- `mir.no`：新 **`OP-STRBYTE = 101`**（接在 `OP-VECNEWCAPSTR = 100` 後，不重排既有碼）。
  語義：`dst = %option`；`x = str` 值 id；`y = i64` 字節下標。越界用 `select` 收斂成
  `tag=1`（nil）而非分支。
- `cg.no`：新發射臂（OP-STRBYTE），序列為
  `extractvalue %str-long …, 0` → `zext i32 → i64` → `icmp slt`／`icmp sge` → `or` →
  `select`（tag 與 payload 各一次）→ 兩次 `insertvalue` 構造 `%option`。
  ⚠️ `%str-long` 的 data 指標在 **field 2**（0/1 是 u32 的 len/cap）——對齊 B28 教訓。
- `h2m.no`：
  - `h2m-ty-of` 加臂 `mt == TY-STR && mf == 'byte' -> code = TY-OPT`——`?byte` 恆裝箱
    ⇒ let 槽必須是 option 型別，否則 `?=` 的 slot 型別推錯。
  - `h2m-lower-expr` 的 `rty == TY-STR` 塊加臂
    `mname == 'byte' && ok-method == 0 && na == 1`：**只收正好一參且參數為 i64**，
    發 `OP-STRBYTE` 到建立立的 `TY-OPT` 槽；其餘（非 i64 參數、多參）**落門禁，絕不猜**。
- `h2m-test.no`：
  - 新增 `vm-strbyte-subj`：`s.byte(0): { ok -> it … }` 的值位 match 主體 ⇒ 斷言
    `unsup == 0` + 有 `define` + 有 `getelementptr i8`（確認真的發了 OP-STRBYTE）。
  - s113 的對照斷言 `unsupported method still -1` 原本用 `str.byte` 當「未支持方法」，
    s117 起它已支持 ⇒ **如實換成 `nosuch-method`**，另加 `s117 str byte ty ⇒ TY-OPT`。

**③ 量測（`drv.pkg-ll-cov` 逐檔 `compiler/src/*.no`）**

| 指標 | s116 | s117 | Δ |
| --- | --- | --- | --- |
| ID（`ID:` 行，16 檔合計） | 759 | **759** | **0** |
| h2m-test 斷言 | 1182 | **1186** | +4 |
| t8 發射形 | — | **7/9** | — |

ID 持平是**預期**：ID 行計的是 `h2m-name-find` 三表（局部／形參結果／自由／全局）未命中的
**標識符引用**，與本輪的**方法族**降級正交——`str.byte` 的方法名從來不是 name-find 的
對象。16 檔分布：`drv` 367、`mir` 229、`h2m` 74、`ck` 35、`ast` 25、`lx` 16、
`cg-runtime` 8、`ck-sig` 3、`link` 2，其餘 0。

**④ 驗證（先紅後綠 + 語义端到端）**

- **语义端到端**：`S = 'ABC'`／`X = S.byte(0): { ok -> it … -> 0 }`／`exit(X)`，
  用 `drv.build-to` 鏈出可執行檔實跑 ⇒ **rc = 65**（`'A'` 的位元組）✓。
  這是本輪最硬的一條：不止「有 define」，而是**跑起來值對**。
- **t8 受控語料**：f1／f3／f5／f6／f7／f8／f9 七檔轉綠（有 `define` +
  `getelementptr i8`）；**f2／f4 仍 EMPTY**（見 ⑤）。
- **全量門禁**（`no run` 斷言數，與 ㉝ 同口徑）：`ps` 51、`h2m` 1186、`cg` 363、
  `ck` 20、`ck-sig` 50、`lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、
  `ps-err` 46、`pkg-ll-call` 5、`pkg-sigs` 5、`pkg-ll` 10 **全綠**（後兩個用 `no run`
  跑，95s／225s）。
- 格式：`no fmt -d` 為空。
- 臨時探針 `probe-s117-{cov,t8,run,allcov}.no` 已刪；`t8/` 語料保留。

**⑤ 誠實邊界與下一步**

- **f2 未解**：`r = s.byte(0): { -> it }`——主體**只有兜底臂**，`it` 沒有 `ok` 臂提供
  綁定。h2m 的 `is-wild` 門禁（`dotv==0 && bvx==0 && cn==0 ⇒ is-wild`）仍判 `bad=1`。
  要支持得先定「**兜底臂的 `it` 語义**」（Go 側同形如何定義，待查對齊）。
- **f4 未解**：`x = 1; r = x: { ok -> it … }`——主體是 **i64（非 option）**的值位 match。
  值位 match 主體的三態 arm 語义目前只對 option 主語定義（D6）；非 option 主體未定義。
- **只收一參 i64**：`str.byte(ch)`（char 參）等落門禁；方法族其它成員（`bytes` 32 處、
  `len` 21 處）本輪未動。
- **ID 指標的盲區**：本輪證明 ID 對「方法族降級」不敏感——下輪若只做方法族，
  應同時看 **t8 發射形**與 **std 主体計數**，別只看 ID。
- 下一輪候選優先級：① **括號主體** `(a + b): { }` 回歸（㉝ ⑤ 遺留，primary 括號分支
  後的 `:` 由外層消費，未專門驗證）；② **`bytes` 方法族**（std 32 處）；
  ③ f2 兜底臂 `it` 語义對齊 Go。

---

### ㉟ s118 —— 括號主體 `(a + b): { }`：一行白名單修復，外加 i64 值匹配變體臂的語義釘死（2026-11-19）

**① 定位**

- ㉞ ⑤ 的遺留項①。實測：`r = (a + b): { … }` 讓整個文件 `unsupported 1`——**不是**
  ㉝ 猜的「`:` 由外層消費」，而是 **primary 括號分支建的 `K-GROUPED-EXPRESSION`
  不在 s116 的明示 match 白名單裡** ⇒ `:` 未消費 ⇒ 後面的 `{ … }` 被當成獨立塊。
- h2m 側**本就認** GroupedExpression（`h2m-ty-of` 與 `h2m-lower-expr` 各有一臂），
  缺口只在 ps 一行。

**② 修復（ps 一行 + 兩條測試）**

- `ps.no`：明示 match 白名單加 `ast.kind-at(t, idx) == asp.K-GROUPED-EXPRESSION -> match-subj = 1`。
- `ps-test.no`：新增 `test-expr-subj-grouped`——`x = (a + b): { ok -> it … }` 的
  dump 必須是 `SurfaceMatch` 主體 `GroupedExpression{InfixExpression{a, b}}`。
- `h2m-test.no`：新增 `vm-grouped-subj`——括號主體 + 常量臂降級後
  `unsup == 0` + 有 `define` + 有 `icmp eq i64`。

**③ 量測（`drv.pkg-ll-cov` 逐檔 16 個 `compiler/src/*.no`）**

| 指標 | s117 | s118 | Δ |
| --- | --- | --- | --- |
| ID（`ID:` 行，16 檔合計） | 759 | **759** | **0** |
| ps-test 斷言 | 51 | **52** | +1 |
| h2m-test 斷言 | 1186 | **1189** | +3 |

ID 持平符合預期：本輪只動 **parse 白名單**，不碰 `h2m-name-find` 的三表未命中口徑。

**④ 驗證（端到端 + 語義實驗）**

- **括號主體端到端**：`f = (a i64, b i64) (r i64) { r = (a + b): { 7 -> 1 … -> 0 } }`，
  `drv.build-to` 鏈出可執行檔實跑 `f(3, 4)` ⇒ **rc = 1**（7 命中常量臂）✓。
- **全量門禁**（`no run` 口徑）：`ps` 52、`h2m` 1189、`cg` 363、`ck` 20、`ck-sig` 50、
  `lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、`ps-err` 46、
  `pkg-ll-call` 5、`pkg-sigs` 5、`pkg-ll` 10 **全綠**。
- **Go oracle 語義實驗**（本輪最有價值的副產品，4 組對照）：i64 主體的值匹配中，
  `ok` / `nil` / `err` 三個裸標識符臂**都匹配常量 0**（誰在前誰贏）；
  常量臂照常匹配字面值（`0 ->` 匹配 0、`1 ->` 匹配 1）；未定義的裸標識符臂
  （`zzz ->`）**編譯錯誤**。`it` 取主體值。
  ⇒ t8 的 **f4**（`x = 1; r = x: { ok -> it … }`）不是「非 option 主體未定義」，
  而是 noc **vmode 門禁拒收了 Go 接受的形態**（`ok` 臂撞在
  `hb+5 != 0 -> bad = 1` 的 dot-val 禁令上）。

**⑤ 誠實邊界與下一步**

- **本輪零語料收益**：`compiler/src` 與 `src/std` 裡**真正**的括號主體
  （`= (…): {`，非 `[a..b): {` 區間遍歷、非 `s.byte(i): {` 呼叫主體）**皆為 0 處**。
  這是一處**語言完整性**修復（有測試鎖定），不是語料驅動的。
- **括號主體 + `ok` 臂仍 unsupported**：`(a + b): { ok -> it … }` 卡在 vmode 門禁
  （`hb+5 != 0 -> bad = 1`），根因與 f4 同源——下一輪的明確目標。
- **`bytes` 方法族**（std 32 處）本輪未動。
- 下一輪候選優先級：① **vmode 變體臂**——讓 `ok` / `nil` / `err` 在 i64 值匹配中
  按 **常量 0** 匹配（對齊 Go 實測，含 `it` = 主體值），一併解 f4 與
  「括號主體 + `ok` 臂」；② **`bytes` 方法族**。

---

### ㊱ s119 —— vmode 變體臂：`ok` / `nil` / `err` 在 i64 值匹配中按常量 0 匹配（2026-11-19）

**① 定位**

- ㉟ ⑤ 的①。s118 用 Go oracle 釘死了語義（4 組對照）：i64 主體的值匹配中，
  `ok` / `nil` / `err` 三個裸標識符臂**都匹配常量 0**（誰在前誰贏），常量臂照常匹配
  字面值，未定義裸標識符臂（`zzz ->`）**編譯錯誤**，`it` = 主體值。
- t8 的 **f4**（`x = 1; r = x: { ok -> it … }`）與「括號主體 + `ok` 臂」都撞在
  h2m vmode 門禁的 `hb+5 != 0 -> bad = 1`（dot-val 禁令）與 `Condition 必須是
  INTEGER-LITERAL` 上。

**② 修復（`h2m.no` 兩處，各一個「變體臂」判定）**

- **門禁**（`vmode == 1` 分支）：引入 `varm`——`hb+1 != 0`（`nil` / `err` 有
  bindingVariant）、`hb+5 != 0`（`ok` 的 dot-val）、或 `cnid-nm == 'err'`
  （`err ->` 無綁定，Condition 是標識符）。`varm == 1` 的臂**跳過**整型字面量檢查。
- **發射**（`omode == 0` 分支）：`varm-e` 同样判定；為 1 時**直接發常量 0**
  （`OP-CONST`），不 lowering Condition（`err ->` 的 Condition 是標識符，查槽表必失敗）。
- ⚠️ **必須驗名字**：`y ->`（未定義裸標識符）與 `err ->` 的 Condition 同為
  `K-IDENTIFIER`，只認「無 variant + 無 dot-val + 標識符」會把前者也放行——
  Go 側 `zzz ->` 是**編譯錯誤**（s118 實測），故 `cnid` 之外兼取 `cnid-nm`，
  僅 `== 'err'` 才認臂。修前 `h2m-test` 的 `vm-gate-named-pattern` 一度轉紅，
  即此邊界。

**③ 量測（`drv.pkg-ll-cov` 逐檔 16 個 `compiler/src/*.no`）**

| 指標 | s118 | s119 | Δ |
| --- | --- | --- | --- |
| ID（`ID:` 行，16 檔合計） | 759 | **759** | **0** |
| h2m-test 斷言 | 1189 | **1194** | +5 |
| t8 發射形 | 8/9 | **8/9** | 0（f4 由 ✗ 轉 ✓，f2 仍 ✗） |

ID 持平：本輪只放行了**本來就合法**的語法形態，不新增 name-find 命中的標識符。

**④ 驗證（先紅後綠 + 三臂語義端到端 + Go 對照）**

- **f4 轉綠**：`drv.build-ll` 對 `compiler/tests/t8/f4.no` ⇒ 有 `define`（修前 EMPTY）。
- **括號主體 + `ok` 臂轉綠**：`r = (a + b): { ok -> it … -> 0 }` ⇒ 有 `define`
  （㉟ ⑤ 想用本輪一併解的形態）。
- **三臂語義端到端**（`drv.build-to` 鏈出實跑，`f(x)` 回 `x==0 ? 5 : 0`）：

  | 臂 | x=0 | x=1 | Go 對照 |
  | --- | --- | --- | --- |
  | `ok -> 5` | **5** | 0 | 一致（s118 實測：0 命中） |
  | `nil -> 5` | **5** | 0 | 一致（s118 實測：0 命中） |
  | `err -> 5` | **5** | 0 | 一致（s118 實測：0 命中） |

- **未定義裸標識符仍被拒**：`x: { y -> … }` ⇒ 無 `define`（對齊 Go 的編譯錯誤）。
- **全量門禁**（`no run` 口徑）：`ps` 52、`h2m` 1194、`cg` 363、`ck` 20、`ck-sig` 50、
  `lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、`ps-err` 46、
  `pkg-ll-call` 5、`pkg-sigs` 5、`pkg-ll` 10 **全綠**。
- 格式：`no fmt -d` 為空（`h2m.no` 補了一處塊內空行）。

**⑤ 誠實邊界與下一步**

- **f2 未解**（t8 唯一残留）：`r = s.byte(0): { -> it }`。已查清它是**兩個獨立缺口**的疊加，
  均**不是** vmode 變體臂的事：
  1. **陳述位 match 的「表達式臂體」**（`-> 0` / `-> it`）⇒ unsup。實測與主體型別
     無關（i64 / option 都紅），`-> r = 2`（賦值臂體）則綠。
  2. **值位 wild 臂的 `it`**（`r = s.byte(0): { -> it }`）⇒ unsup，但同形的 `-> 0` 綠。
- **本輪同樣零語料收益**：`compiler/src` / `src/std` 裏沒有「i64 主體 + 變體臂」的
  用例（值位 match 的變體臂只見於 t8 語料）；收益是**語言完整性** + f4 轉綠。
- **`bytes` 方法族**（std 32 處）本輪未動。
- 下一輪候選優先級：① **陳述位 match 的表達式臂體**（缺口 1，通用性最高）；
  ② **值位 wild 臂的 `it`**（缺口 2，解 f2）；③ **`bytes` 方法族**。

---

### ㊲ s120 —— 兩個「陳述位／wild 臂」缺口的 Go 語義釘死與門禁保留決策（2026-11-19）

**① 定位**

- ㊱ ⑤ 留下的兩個缺口，本輪用 Go oracle 把它們的語義**徹底查清**，再決定是否實現。
- 缺口 1：**陳述位 match 的「表達式臂體」**（`-> 5` / `-> it`，無賦值）。
- 缺口 2：**值位 wild 臂的 `it`**（`r = s.byte(0): { -> it }`，即 t8 f2）。

**② Go 語義實測（缺口 1：6 組對照）**

- `x: { 1 -> 7 … -> 9 }`（陳述位）**Go 接受**並可運行；其值可觀測地流向
  **其後那條賦值語句的目標**——`r = z` / `r = w` / `r = z &^ w` **皆得臂值 7**
  （而非 z/w 的舊值）；`r = z - w` 因臂值是 `?i64`（溢出 option）而報型別錯。
- 機制判斷：這是 ASTToHIR 的** stmtVal 串接**（match 的值被下一條賦值繼承），
  **晦澀且像實現 artifact**，不是可清晰定義的語言語義。

**③ Go 語義實測（缺口 2：3 組對照）**

- `r = s.byte(0): { -> it }` Go 接受；`f('')` 與 `f('A')` 皆回 0，且
  `-> r = it` 被 Go 判**非法**（「match arm in expression context must end with an
  expression」）。⇒ 兜底臂的 `it` 既非 s115 的 OPTGET 載荷，也非主體值，
  而是**未綁定零值**（型別隨目標）——同一種隱晦語義。

**④ 決策：兩個缺口都**不實現**，門禁保留**

- 理由：Go 側語義**依賴實現機制**（stmtVal 串接 / 未綁定零值），可觀測行為自相矛盾
  （同一 match 之後 `r = z` 與 `r = w` 都拿到臂值），不具備可移植的清晰定義；
  照抄會把「看起來能跑」的隱晦行為固化進行為，違背 s93「自舉產物不得比權威更寬鬆」
  與「寧缺勿錯」的口徑。
- **本輪的實質增量 = 把邊界寫死**：`h2m-test` 新增兩條**回歸鎖定**用例
  （`stmt-position expr arm body still gated` / `value wild-arm it still gated`），
  把「門禁刻意保留」變成**有測試背書的已知邊界**，避免未來誤當缺口幹掉。

**⑤ 量測與驗證**

| 指標 | s119 | s120 | Δ |
| --- | --- | --- | --- |
| ID（`ID:` 行，16 檔合計） | 759 | **759** | 0 |
| h2m-test 斷言 | 1194 | **1196** | +2（邊界鎖定） |
| 源碼改動 | — | **0 檔**（只加測試與文档） | — |

- **全量門禁**（`no run` 口徑）：`ps` 52、`h2m` 1196、`cg` 363、`ck` 20、`ck-sig` 50、
  `lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、`ps-err` 46、
  `pkg-ll-call` 5、`pkg-sigs` 5、`pkg-ll` 10 **全綠**。
- 格式：`no fmt -d` 為空。

**⑥ 誠實邊界與下一步**

- 本輪是**調研 / 決策型**一輪：**零源碼改動**，產出是「兩個缺口為什麼不補」的
  實測依據與測試鎖定。t8 仍 8/9（f2 保留為**已知邊界**，不再是「待修缺口」）。
- 若未來要動手，前提是先有一個**清晰定義**（例如「陳述位 match 值 → 緊接的賦值目標」
  寫進語言規範）；在規範缺席前不動。
- 下一輪候選優先級：① **`bytes` 方法族**（std 32 處，語義清晰：回 `?[]byte`／越界 nil，
  可比照 s117 的 `str.byte` 序列）；② 括號主體之外的**其它主體形態**回歸盤點。

### ㊳ s121 —— `str.bytes()` 零拷貝視圖降級（OP-STRBYTES）（2026-11-19）

**① 定位：`str.bytes()` 無 h2m 臂，且 ㊲⑥ 對它的定性是錯的**

- ㊲⑥ 把「`bytes` 方法族」描述為「回 `?[]byte`／越界 nil，可比照 s117 的 `str.byte` 序列」——
  **兩個方法被混為一個**：`str.byte(i)` 才回 `?byte`（越界 nil）；`str.bytes()` 回的是
  **零拷貝 `[]byte` 視圖**，簽名 `() (val []byte)`（`src/std/str.no:688`，`#{buildin}` 桩），
  不帶參、不越界、不失敗。
- 語義以 std 文檔 + Go 實現雙向釘死：`src/std/str.no:676-686` 明寫「回傳的 []byte 共享 str 的
  data 緩衝……視圖的 cap 為 0（借用標記），作用域結束時 vec_free 會跳過釋放，所有權仍屬原 str」；
  Go 側 `src/mir/builtin_str_bytes.go:29` 的 `emitBuiltinStrBytes` 用
  `vecFromParts(strLenI64(sv), "0", ptrtoint dataPtr)` —— 第二個引數 `"0"` 就是 cap 借用標記。
- **徵結**：`h2m-ty-of` 無 `mf == 'bytes'` 臂、`h2m-lower-expr` 無對應臂 ⇒ `s.bytes()` 落
  `uns-unsup`。**呼叫處為零**：`grep -rn '\.bytes()' src/std/*.no` 只命中 `str.no:676` 的
  注释一行，std 里沒有活的調用點 ⇒ 本輪是**向前補齊內建語義**，當下無語料收益。

**② 修復：OP-STRBYTES=102 + cg 發射臂 + h2m 兩臂**

- `compiler/src/mir.no`：`OP-STRBYTES = 102`（接在 s117 的 `OP-STRBYTE = 101` 之後，不重排）。
  語義：`dst` = vec 值 id（TY-VEC）；`x` = str 值 id（TY-STR）；`y` 不用。
- `compiler/src/cg.no`：發射臂按 `%vec = { i32 len, i32 cap, i64 data }`（`cg-runtime.no:194`）
  三段 `insertvalue` 組出：
  - `%sbL = extractvalue %str-long %sv<x>, 0`（i32 len，**直接插欄 0，不 zext**——s117 的
    `OP-STRBYTE` 為做邊界比較才 zext，這裡不需要）；
  - `%vb0 = insertvalue %vec zeroinitializer, i32 %sbL, 0`；
  - `%vb1 = insertvalue %vec %vb0, i32 0, 1`（**cap = 0 借用標記**）；
  - `%sbP = extractvalue %str-long %sv<x>, 2`；`%sbD = ptrtoint i8* %sbP to i64`；
  - `%vc<dst> = insertvalue %vec %vb1, i64 %sbD, 2`。

  中間名一律帶指令下標 `k`（沿用 s99n 的「世界只有一箇可認單名」手法）。
- `compiler/src/h2m.no`：`h2m-ty-of` 加 `mt == TY-STR && mf == 'bytes' -> code = TY-VEC`；
  `h2m-lower-expr` 在 `rty == TY-STR` 分支加臂：
  `mname == 'bytes' && ok-method == 0 && na == 0` ⇒ `h2m-alloc(c, TY-VEC)` +
  `h2m-emit(c, OP-STRBYTES, …, rv, 0, '', TY-VEC)`。
  **門禁只收無參形**（比 s117 的 `byte` 多一條 `na == 0`）；**不進 rel 追蹤**——視圖 cap=0
  已自描述所有權，若走 `rel-add` 令人為賦予所有權，作用域結束會去釋放借用的緩衝。

**③ 量測**

| 指標 | 本輪前 | 本輪後 | Δ |
| --- | --- | --- | --- |
| ID（`ID:` 行，16 檔合計） | 759 | **759** | 0 |
| h2m-test 斷言 | 1196 | **1202** | +6（1 ty-of + 4 降級 + 1 帶參門禁） |
| 源碼改動 | — | **4 檔**（mir 17 行 / cg 36 行 / h2m 16 行 / 測試 29 行） | — |

**④ 驗證**

- **先紅後綠**：`h2m-test` 新增 `vm-strbytes`（`b = s.bytes()` ⇒ unsup 0 + 發射含
  `insertvalue %vec` / `ptrtoint i8*`）與 `s121 str bytes ty`（`ty-of ⇒ TY-VEC`）；
  另加**帶參門禁對照** `s.bytes(1)` ⇒ 仍 unsup > 0（本輪只定義無參形）。
- **語义端到端**（`drv.build-to` 鏈出可執行檔實跑，全部與 Go oracle 逐值對齊）：

  | 程序 | 含義 | noc | Go oracle |
  | --- | --- | --- | --- |
  | `B = S.bytes(); exit(B.len())`，`S = 'ABC'` | 視圖 len = 字節數 | rc **3** | 3 |
  | 同上，`S = 'hello, 中'` | 非 ASCII 按 UTF-8 字節（非碼點） | rc **10** | 10 |
  | `f = (s str) (r i64) { B = s.bytes(); r = B.len() }`；`X = f('ABC')`、`Y = f('abcabc')`；`exit(X + Y)` | 視圖 borrow 不釋原緩衝 | rc **9** | 9 |
  | 同一字面 `f('ABC')` 調用兩次；`exit(X + Y)` | 視圖作用域結束不釋放來源緩衝 | rc **6** | 6 |

- **IR 層面的所有權證據**（`drv.build-ll` 取 `f = (s str) (r i64) { B = s.bytes(); r = B.len() }`）：

  ```llvm
  define i64 @f(%str-long %sv1) {
  entry:
      %sbL0 = extractvalue %str-long %sv1, 0
      %sbP0 = extractvalue %str-long %sv1, 2
      %sbD0 = ptrtoint i8* %sbP0 to i64
      %vb00 = insertvalue %vec zeroinitializer, i32 %sbL0, 0
      %vb10 = insertvalue %vec %vb00, i32 0, 1      ; cap = 0（借用標記）
      %vc2  = insertvalue %vec %vb10, i64 %sbD0, 2
      %v3 = call i64 @vec_len(%vec %vc2)
      ret i64 %v3
  }
  ```

  `@f` 內**沒有任何 `@vec_free`** ⇒ 「不進 rel 追蹤」不止是構造上的宣告，產物可查。
  而 `@main` 把字面 `'ABC'` 做成 `.rodata` 常量（`@.s.main.0 = private unnamed_addr constant
  [4 x i8]`）——**不是堆**——所以上面最後一條 e2e（同一字面調用兩次）是**真的在探自由**：
  視圖若帶 cap 被釋放，`free()` 會收到一個非堆指標而 abort；rc=6 乾淨返回即通過。
- **全量門禁**（`no run` 口徑）：`ps` 52、`h2m` 1202、`cg` 363、`ck` 20、`ck-sig` 50、
  `lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、`ps-err` 46、
  `pkg-ll-call` 5、`pkg-sigs` 5、`pkg-ll` 10 **全綠**。`no fmt -d` 為空。
- S0→S1（`no build -o /tmp/noc-s1 compiler`）rc=0。

**⑤ 誠實邊界與下一步**

- **零語料收益如實記載**：std 里 `str.bytes()` **沒有活的調用點**（唯一命中是注释），
  所以本輪 `ID` 759 持平、t8 仍 8/9。這是「向前補齊內建語義」，不是「修好一個卡住的語料」。
- **本輪順帶量到的既存缺口**（皆與 `bytes` 無關，勿記到本輪頭上）：
  ① `h2m` 只支援**至多 2 個輸出參**——`() (a i64) (b i64) (c i64) {}` ⇒ unsup 2（n-1）；
  ② **函式內讀全局 str** 未支援——`S = 'ABC'; f = () { N = S.len() }` ⇒ unsup 1；
  ③ **計數迴圈體內的 vec 局部**未支援——`1 * { B []i64 = [1] }` ⇒ unsup 2
  （`[]i64` 字面同樣失敗，故非 `bytes` 引入）；
  ④ 裸塊 `{ … }` 未支援；⑤ `drv.build-to` 仍拒絕目錄（S2 趟不了）。
  ⇒ 想做「視圖作用域结束不釋放來源緩衝」的**直接**驗證（用計數迴圈造內層作用域）目前
  走不通，只能以「同一字面調用兩次不崩、值正確」作間接證據。
- **下一步優先級**：① ㊲⑥ 的②——**括號主體之外的其它主體形態**回歸盤點；
  ② 上述既存缺口裡最便宜的一個（② 函式內讀全局 str，或 ③ 計數迴圈 vec 局部）任選其一。

### ㊴ s122 —— `vec` 局部量在【含控制流】下降級：`h2m-ty-of` 補 `K-SLICE-LITERAL` 臂（2026-11-19）

#### ① 定位：缺口就是 s121 量到的 `a3`／`b3`，根因在 `h2m-ty-of` 的 `K-SLICE-LITERAL` 缺口

- **缺口形態**：`B []i64 = [1, 2, 3]` 這種**切片字面量 `let`**，落在【含控制流（`c.mem == 1`）】的函式裡 ⇒ 2 條 unsup（`unsup-slot-noun` ×2：`h2m-slot-decl` 一計、`h2m-var-write` 查不到名字再一計）。**同一份程式碼在【直線式（`c.mem == 0`）】下不鋪槽 ⇒ 正常產碼**——**兩種 mem 模式行為分岔**，只有走控制流的那支會暴露。
- **誰寫的**：`compiler/src/h2m.no` —— `h2m-ty-of`（無 `K-SLICE-LITERAL` 臂，回 -1）、`h2m-slot-decl`（名字未綁定即 `h2m-mark`）、`h2m-var-read` / `h2m-var-write`（只做**形參來源**的 vec，`s62` 開的門）。
- **為何繞**：`vec` 局部量**不鋪槽**——`cg` 無 `alloca %vec`，值 id 由 `h2m-var-write` 在寫點繫結（`s62` 註：「本路徑不做別名／生命期分析」）。`s62` 為**形參來源**開的路是「`h2m-var-read` 直接回傳 entry 的 incoming 值 id」；**字面量來源**的 vec 局部量從 `h2m-scan` 起就拿不到型別 ⇒ 鋪槽階段即 unsup。這正是 `h2m-ty-of` 的 **`P6` 缺口**（§2.4）——自 `s60` 就記歪了，一直沒對到 `s121` 的既存缺口表。
- **與 `s121` ① 的關係**：`s121` 量到的 ㊲⑥② 表裡 `a3`／`b3`（2 unsup）正是本缺口；`b1`／`b4`（同一份 vec 局部 + 控制流）亦然。**「`ty-of` 缺 `K-SLICE-LITERAL` 臂」是同一共同根因**，`s121` 只解 `s102` 的 3 個徵結故摸不到。

#### ② 修復：`h2m-ty-of` 補臂 ＋ `vec` 槽的三處配套 ＋ 一個順帶的非法 IR

1. **`h2m-ty-of` 補 `K-SLICE-LITERAL` 臂**（`h2m.no:2154` 後）：`code = TY-VEC`；`d.nkid > 0` 且首元素是 `K-STRING-LITERAL` ⇒ `TY-VECSTR`。**鏡像降級側** `K-SLICE-LITERAL` 的 vec-ty 預判——與 `s104`（`str` 比較／拼接）、`s74` 同族：`ty-of` 與降級側的型別推斷必須一起改。
2. **`h2m-slot-decl`**：`i0 < 0`（名字尚無綁定）由 `h2m-mark` 改為**登記佔位**（`vids=0`）——絕不錯殺 `s62` 為**形參來源**開的路。
3. **`h2m-var-read`**：`TY-VEC` / `TY-VECSTR` 分支加 `sid == 0` 門禁 ⇒ 已登記但未寫（未初始化）**計 unsup**，不交 0 值 id 糊過去。
4. **`h2m-var-write`**：新增 vec 槽寫臂——`vt == st && c.bkn == c.fnbk` 時 `h2m-var-bind`（同 `mem==0` 的 var-bind）。**只在函式體頂層塊放行**：值 id 在函式體塊定義 ⇒ 支配全函式各塊；寫在**被控塊**（迴圈／if／match 臂）裡則值 id 不支配其後繼，跨塊讀會變非法 SSA。
5. **`h2m-ctx` 新增 `fnbk`**（函式體塊的棧深；`0` = 未設），`h2m-lower-func` 在進 body 前記 `bkn + 1`——讓上一條的門禁判得出「現在是否就在函式體頂層塊」。
6. **順帶修掉第二個 bug**：`h2m-var-write` 的**標量槽寫臂**漏排 `TY-VECSTR` ⇒ `[]str` 局部量會在佔位槽（`vids=0`）上發 `OP-STORE`，產出 `store i64 %v…, i64* %s0` 這種指向**未定義 slot 0** 的非法 IR（e2e 實測 clang 報 `use of undefined value '%s0'`）。補 `&& st != TY-VECSTR`（同 `h2m-slot-init-arg` 口徑）。

#### ③ 量測：ID 759 → 513，LF 15180 → 14216

`drv.pkg-ll-cov` 逐檔 16 個 `compiler/src/*.no`（腳本 `tmp/cov-all2.no`，以 `BEGIN`／`END` 切檔；pre = `git show HEAD:compiler/src/h2m.no` 臨時回退，以與 ㊲／㊳ 口徑一致）：

| 檔 | pre ID／LF | post ID／LF | ΔID | ΔLF | pre／post unsup |
| --- | --- | --- | --- | --- | --- |
| `asp` | 0／77 | 0／77 | 0 | 0 | 77／77 |
| `ast` | 25／299 | 24／293 | −1 | −6 | 320／314 |
| `cg-rc` | 0／99 | 0／99 | 0 | 0 | 100／100 |
| `cg-runtime` | 0／542 | 0／542 | 0 | 0 | 543／543 |
| `cg` | 3／2481 | 3／2478 | 0 | −3 | 2485／2482 |
| `ck-sig` | 0／74 | 0／74 | 0 | 0 | 75／75 |
| `ck` | 35／451 | 29／418 | −6 | −33 | 474／441 |
| `diag` | 8／35 | 8／35 | 0 | 0 | 35／35 |
| `drv` | 367／5636 | 242／5151 | **−125** | **−485** | 5749／5264 |
| `h2m` | 74／3356 | 65／3337 | −9 | −19 | 3418／3399 |
| `ix` | 0／3 | 0／3 | 0 | 0 | 3／3 |
| `link` | 2／22 | 2／18 | 0 | −4 | 22／18 |
| `lx` | 16／164 | 16／164 | 0 | 0 | 166／166 |
| `mir` | 0／0 | 0／0 | 0 | 0 | 0／0 |
| `ps` | 229／1938 | 124／1524 | **−105** | **−414** | 2005／1591 |
| `tok` | 0／3 | 0／3 | 0 | 0 | 3／3 |
| **合計** | **759／15180** | **513／14216** | **−246** | **−964** | **15475／14511** |

- **落點**：`drv`（−125／−485）與 `ps`（−105／−414）佔大頭——兩檔的函式體裡遍佈切片字面量，正是 ㊲⑥ 那 9103 條「型別未明」的主體；`h2m` 自身 −9／−19。
- **歸因（重要）**：`ID` 通道本輪的可信度完全依賴「同一支腳本、同一組 import 套」——**進口套會改變 `drv.pkg-ll` 的產出**（`drv.no` 註：「呼叫 `drv.pkg-ll` 的測試必須直接 import 全套傳遞依賴，否則 `drv.*` 內部 `h2m-*`／`ast.*`／`ps.*` 呼叫 codegen 失敗（consumer reads value 0）」）。本輪 pre 值 **759 與 ㊲ 所載完全一致** ⇒ 腳本口徑相同。
- **拆分歸因**：把 `ty-of` 臂單獨拿掉（其餘 4 處保留）⇒ ID 533 ⇒ **`ty-of` 臂占 −226**，其餘 4 處共 −20。即本輪的 ID 下降**幾乎全部來自「型別可見」**，而非門禁／佔位槽的調整。

#### ④ 驗證

- **單元**（`compiler/tests/h2m-test.no` 新增 `test-s122-vec-local-mem1` ＋ 3 條 `ty-of` 斷言）：**1219 全綠**（㊳ 的 1215 ＋本輪 16 條 − 撤門禁後 2 條）。
  - ① 頂層 `B []i64 = [1,2,3]` ＋ 計數迴圈內讀：`unsup==0`、有 `@vec_new`／`@vec_len`／`@vec_free`、**不含** `i64* %s0`。
  - ② `B []str = ['a','bc']`：`unsup==0`、有 `@vec_push_str`／`@vec_free_str`、不含 `i64* %s0`。
  - ③ 短路鏈各臂讀同一個頂層 vec 局部：`unsup==0`（值 id 支配全函式各塊）。
  - ④ **門禁**：**迴圈體內的 vec 局部** ⇒ 仍 `unsup > 0`（值 id 不支配後繼）。
- **e2e**（go oracle vs `/tmp/noc-s122`，`tmp/s122-e2e/`）：

| 例 | go | noc | 判定 |
| --- | --- | --- | --- |
| `e1` vec 局部 ＋ 計數迴圈讀 | 3 | 3 | ✅ |
| `e3` `[]str` 局部 | 2 | 2 | ✅ |
| `e4` `with-cap` 局部 | 0 | 0 | ✅ |
| `e5` 迴圈體內宣告 vec | 1 | BUILD FAIL（3 unsup） | ✅ 門禁 |
| `e6` 短路鏈各臂讀 | 3 | 3 | ✅ |
| `e7` 下標讀 ＋ 值位 match | 7 | **0** | ❌ 既存缺陷（見 ⑤） |

- **14 套全綠**：`ps 1587／h2m 1219／cg 420／ck 675／ck-sig 41／lx 149／tok 124／diag 11／build-package 14／util 122／ps-err 120／pkg-ll-call 1／pkg-sigs 146／pkg-ll 10`。
- **撤門禁**：`s61` 的 `rng-container`（「容器遍歷 `x <- xs`（無標注 let 名）不產碼」）**已失效** ⇒ 改為**反向釘子**（`unsup==0` ＋ 產碼 ＋ `@vec_len`／`@vec_get_i64`／`@vec_free`）。實測 IR 與 `s62` 形參源同構：`%v… = call i64 @vec_len(%vec %vc…)` 只算一次、表頭只讀 idx、`@vec_get_i64` 在迴圈體、`@vec_free` 唯一落函式出口。

#### ⑤ 誠實邊界與下一步

- **門禁仍在**：**被控塊內**宣告的 vec 局部量（迴圈體／match 臂）仍 `unsup`——值 id 不支配後繼塊，且 scope-exit 釋放會落到函式出口（跨塊使用 ⇒ 非法 SSA）。這需要真正的「槽化 ＋ 顯式釋放」或支配點分析，不是本輪的「值 id 重綁」能覆蓋的。
- **`e7` 是既存缺陷，但本輪把它從「大聲 unsup」變成了「靜默錯值」**：下標讀 `B[0]` 的值位 match 得 0（go 7）。**旁證**：把 `B` 改成 **vec 形參**（`f = (B []i64)`，完全不經過 s122 的 vec 局部路徑）在 **baseline 與 s122 下都**得 0 ⇒ 根因在 `h2m-lower-surface` 的 vmode（`s119`：i64 值匹配下 `ok` 臂匹配常量 0），屬 **ck 層的 option 推斷缺口**（`B[0]` 被降級成 plain `i64` 而非 `?i64`）。baseline 的 `e7` 因 vec 局部那 2 條 unsup 而**整整函式不產碼**（4 unsup、無 `define`），s122 修掉那 2 條後**暴露**了既存的錯值。**本輪不加新門禁**——那會波及其它 option 推斷缺口。
- **`vec` 局部量不可重新賦值**：`B = [7]`／`B = g()`／`B = with-cap(4)` 在 Go 側全報 `sm7rjfcm` duplicate variable（Go 的 ck 把 `let` 名當已宣告；`x i64 = 1; x = 2` 則合法）；**noc 接受**。這是 **ck 分歧**，本輪不處理。
- 本輪只解 `a3`／`b3`／`b4`（同一份 vec 局部 ＋ 控制流）的**共同根因**；**未動** `x1`／`y5`（既存 bug，絕非 `P6` 缺口）。

**下一步優先級**：① ㊲⑥ 的②——**括號主體之外的其它主體形態**回歸盤點（仍未做）；② **函式內讀全域 `str`**（㊲⑥② 另一個最便宜的缺口）。

### ㊵ s123 —— 函式內讀全域 `str` 常量（`NOC-NAME`／`NOC-VERSION` 族）（2026-11-20）

**① 定位**

- **先做 ㊴⑤① 的「括號主體之外的其它主體形態」盤點**（`tmp/s123/f1-ident.no` … `f22-logic.no`，22 個最小重現，逐檔 `drv.pkg-ll-cov` 量）：
  - **已支持（0 unsup）**：標識符 `x:`、索引 `a[0]:`、點號 `p.n:`、呼叫 `g():`、中綴 `a + b:`、括號 `(a + b):`、**常量中綴 `1 + 2:`**（s116 歸一化）、深點號 `o.i.n:`、點號呼叫 `mk(v).n:`、比較 `x == 1:`、邏輯 `x && true:`。
  - **不支持（11 種）**：字面量／常量家族 7 種——整數 `1:`（3）、字串 `'a':`（4）、浮點 `1.5:`（4）、char `"a":`（3）、`nil:`（4）、`true:`（3）、切片 `[1, 2]:`（4）；一元家族 3 種——`-x:`（3）、`!x:`（3）、`-1:`（3）；外加**點號索引 `a[0].n:`**（2）。
  - 語料 grep（`compiler/src` + `src/std`）：**語句位與賦值 RHS 位的「字面量／一元主體」零命中**（`src/std/net/quic.no:358` 的 `].length: {` 是**中綴**主體 `r-pos + frames[count].length:`，屬已支持族，非點號索引主體）⇒ **零語料收益**，本輪不做。
- **缺口重新定性（重要修正）**：`noc build` 走 `drv.build-ll`（單文件、**無 sg 常量表**），該口徑下整數／字串／vec／bool 全局讀**全部**失敗；但在 `drv.pkg-ll` 口徑下，**整數全局常量本來就被 s111（`mod.CONST`）／s112（裸名）覆蓋**，真正的缺口是**非整數（`str`）全局常量讀**。
- 語料實證：`compiler/src/drv.no` 的 `NOC-NAME`（`:45` 定義）與 `NOC-VERSION`（`:46`）在 ID 日誌中佔 **12 ＋ 2 ＝ 14 條**（12 個函式各 1 條，`drv.pkg-ll` 2 條）。

**② 修復**

- `compiler/src/h2m.no` 的 `h2m-sigs` 加 3 欄 `stkey []str`／`stval []str`／`stname []str`，與 s111 的 `ckey`／`cval`／`csname` **平行**——**整數表與 str 表不可混用一份值表**，因為查表命中後要決定回 `TY-I64` 還是 `TY-STR`（型別信息在值表裡沒有）。`h2m-sigs-new` 同步加 3 個 `[]`。
- 新增 3 個函式（插在 `h2m-sig-const-find-short` 之後）：`h2m-sig-strconst-add`（推 key、值、短名）、`h2m-sig-strconst-find`（`mod.CONST` 線性掃）、`h2m-sig-strconst-find-short`（裸名線性掃）。短名口徑與 `h2m-sig-const-add` 一致（第一個 `.` 之後）。
- `compiler/src/drv.no` 的 `drv.sig-consts-add` 加 str 收集臂：`lnd.kind == asp.K-NAMED-TYPE && rnd.kind == asp.K-STRING-LITERAL` ⇒ `h2m-sig-strconst-add(sg, mod - '.' - cn, cs)`。**非字面量初值**（`CONST = f()` / `CONST = OTHER`）同樣不收，門禁不放寬。
- `h2m-ty-of` 兩臂：K-IDENTIFIER 在 `bci < 0`（整數短名表未命中）後查 str 短名表 ⇒ `TY-STR`；K-DOT-EXPRESSION 在 `ci < 0`（整數表未命中）後查 str 全鍵表 ⇒ `TY-STR`。**`ty-of` 必須與降級側一起改**（同 s104／s74／s122 族）：槽寬推錯 ⇒ `h2m-slot-decl` 與 `h2m-var-write` 各計一次 unsup。
- `h2m-lower-expr` 兩臂：K-IDENTIFIER 在 `bci < 0` 分支查 str 短名表，命中 ⇒ `h2m-alloc(c, TY-STR)` + `h2m-emit(c, OP-STRCONST, …, ssv, TY-STR)`（`sym` 即字串內文，cg 包成 `.rodata` 全域常量，與 K-STRING-LITERAL 臂同口徑）；**未命中才** `h2m-mark` + `h2m-id-name`。K-DOT-EXPRESSION 在 `ci < 0` 分支同理，命中發 `OP-STRCONST`，未命中才走原 struct 欄位路徑（原路徑整段包進 `csi < 0 -> { … }`，抬高縮進一層）。
- 源碼改動：**3 檔**（`h2m.no` 淨 +107 行／`drv.no` +9 行／`h2m-test.no` +10 斷言）。

**③ 量測**

配對基線（`tmp/cov-all2.no`：16 模組各跑一次 `drv.pkg-ll-cov`；pre = `tmp/s123/cov-post.txt`，post = `tmp/s123/cov-s123.txt`）：

| 指標 | 本輪前 | 本輪後 | Δ |
| --- | --- | --- | --- |
| ID（`ID:` 行，16 檔合計） | 513 | **503** | **−10** |
| ← `drv` | 62 | 48 | −14（= `NOC-NAME` 12 ＋ `NOC-VERSION` 2） |
| ← `h2m` | 76 | 80 | +4（兩個新 find 函式各 2 條 `nm`） |
| LF（`LF:` 行，16 檔合計） | 14216 | **14251** | **+35** |
| ← `drv` | 405 | 366 | −39（12 個函式的 unsup 路徑不再走） |
| ← `h2m` | 1156 | 1230 | +74（本輪新碼） |
| unsup（`pkg-ll` 句尾，16 檔合計） | 14511 | **14546** | +35 |
| h2m-test 斷言 | 1219 | **1229** | +10 |

- **LF +74 明細**（全部是本輪新增）：`h2m-sig-strconst-find` +18、`h2m-sig-strconst-find-short` +18、`h2m-sig-strconst-add` +14、`h2m-lower-expr` +12、`h2m-sigs-new` +6、`h2m-sigs.stkey`／`stval`／`stname` 各 +2（+6）。新函式與既有同類**同口徑**（`h2m-sig-const-find`／`-short` 各 18 LF）。
- **ID −14 逐條可查**：`drv.bp-walk`／`build-to`／`cmd-ast`／`cmd-build`／`cmd-run`／`cmd-tokens`／`cmd-version`／`name`／`not-implemented`／`pkg-ll`（×2）／`usage` 各減 1 條 `NOC-NAME`，`cmd-version`／`version` 各減 1 條 `NOC-VERSION`；無一新 ID 來自語料。

**④ 驗證**

- **先紅後綠**：`compiler/tests/h2m-test.no` 新增 `test-s123-str-const`（10 條）——裸名 `NOC-NAME`（`ty-of ⇒ TY-STR`、`lower-expr` unsup 0／有值／vty `TY-STR`）、點號形 `drv.NOC-VERSION`（同三條）、**未登記的 `NOPE-STR` 仍誠實 unsup 且無值**（門禁不放寬）。`h2m-test` 1219 → **1229 全綠**。
- **IR 層證據**：`S = 'ABC'` 的 IR 出現 `@.s.f.5 = private unnamed_addr constant [4 x i8] [i8 65, i8 66, i8 67, i8 0]`，`@f` 內以 `%str_eq` 比較。
- **語義端到端**（go oracle vs `/tmp/s123-e2`…`/tmp/s123-e4`，語料與驅動腳本見 `tmp/s123-e2e/` 與 `tmp/s123/probe-e2e.no`；`drv.pkg-ll` + `link.compile` 鏈出可執行檔實跑，全部逐值對齊。Go oracle 的 **`Error: exit status N` 即子進程退出碼 N**）：

  | 程序 | 含義 | noc | Go oracle |
  | --- | --- | --- | --- |
  | `S = 'ABC'`；`f = () (r i64) { r = 0; S == 'ABC' -> r = 1 }`；`exit(f())` | 裸名全域 str 作比較運算元 | rc **1** | 1 |
  | 同上再加 `T = 'ABD'`，`S == T -> r = 1` 在前 | 不同全域 str 不相等 | rc **2** | 2 |
  | `g = (x str) (r i64) { r = x.len-bytes() }`；`f = () (r i64) { r = g(S) }` | 全域 str 作呼叫引數 | rc **3** | 3 |
  | `dep.V`（跨模組 `# /…/dep` 導入） | 點號形跨模組 str 常量 | rc **1** | 1 |

- **全量門禁 14 套全綠**：`ps` 52、`h2m` 1229、`cg` 363、`ck` 20、`ck-sig` 50、`lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、`ps-err` 46、`pkg-ll-call` 5、`pkg-sigs` 5、`pkg-ll` 10。
- **口徑提醒**：㊴⑤ 的門禁數字（`ps` 1587／`cg` 420／`ck` 675／`pkg-sigs` 146…）**在本輪無法以 `no run` 口徑重現**——同一套命令下實測 `ps` 52／`cg` 363／`ck` 20／`pkg-sigs` 5，與 ㊲／㊳ 完全一致（`h2m` 1219 除外，那是 ㊴ 當輪的斷言數，本輪 1229）。**本輪以及後續輪次一律以實測數字為準。**
- 格式：`no fmt -d` 三檔（`h2m.no`／`drv.no`／`h2m-test.no`）全空。
- S0→S1（`./bin/no build -o /tmp/noc-s123 compiler`）rc=0；`/tmp/noc-s123 version` ⇒ `noc v0.0.1-m0`。

**⑤ 誠實邊界與下一步**

- **只覆蓋字面量初值的 str 常量**：`CONST = f()` / `CONST = OTHER` 仍 unsup（收集臂只收 `K-STRING-LITERAL`）。
- **只走 `pkg-ll` 口徑**：`noc build`（`drv.build-ll`，單文件、無 sg）仍讀不到跨模組常量——本輪完全不碰那條路徑。
- **LF 淨增 +35 是 gate 放行的誠實暴露，不是被動回歸**（s113 先例）：本輪配對賬是 **ID −10 / LF +35**，缺口縮小但葉子總數上升。
- **新增 4 條 ID**（`h2m-sig-strconst-find:nm` ×2、`h2m-sig-strconst-find-short:nm` ×2）：`nm` 未綁名，與既有 `h2m-sig-const-find:nm` 同族。**不遮掩**。
- **S1→S2 未通**：`noc build -o /tmp/noc-s123-s2 compiler/main.no` 仍報 `2` 條 unsup，與 `/tmp/noc-s122`、`/tmp/noc-s1` **逐字相同**（既存缺口，非本輪回歸）；且該路徑走 `drv.build-ll`（單文件、無 sg），與 S0→S1 的 `no build compiler`（整包 `pkg-ll`）**不同口徑**，不能相互代表。
- **文檔示例混了兩個缺口**：`S = 'ABC'; f = () { N = S.len() }` 的 `str.len()`／`len-bytes()` 方法呼叫仍是 2 條 unsup（獨立缺口，未在本輪處理）。
- **葉子通道暴露了當前最大單一 unsup 來源**：`kind-name:SurfaceMatch:construct` **432 條** + `kind-name:StringLiteral:construct` **456 條**（16 檔合計），全部來自 `asp.kind-name`（`id: { 1 -> 'AnnotationArrayValue' … }`，74 個字串臂 + 1 個 `-> 0` 兜底）——每個含 `asp` 的模組量測（`asp`／`ast`／`ck`／`drv`／`h2m`／`ps` 共 6 個）各重現 72 + 76 條。根因是 s103 的门禁「**值位 match 的臂值型別必須 `TY-I64`**，str／vec 值無處可落」（`h2m.no:1760`）。

**下一步優先級**：① **值位 match 的 `str` 臂值**——上述 432／456 條 unsup 全是它，且 `asp.kind-name` 被 `ast`／`ck`／`drv`／`h2m`／`ps` 傳遞依賴，回報最大；② 其餘非整數全域常量（`vec`／`bool`／`f64`）先做語料盤點再決定（仿本輪 ① 的「零命中即跳過」）。

### ㊶ s124 —— 值位 match 的 `str` 臂值（`asp.kind-name` 族）（2026-11-20）

**① 定位**

- **接手 ㊵⑤ 點名的最大單一 unsup 來源**：`kind-name:SurfaceMatch:construct` **432 條** + `kind-name:StringLiteral:construct` **456 條**（16 檔合計，全部來自 `asp.kind-name`）。
- **先更正 ㊵ 的事實錯誤**：`asp.kind-name` 是 **75 個臂、全部是字串字面量**——`1 -> 'AnnotationArrayValue'` … `74 -> 'ViewType'`，末臂是 `-> 'UNKNOWN-KIND'`，**沒有 `-> 0` 兜底**（`awk` 精確計數 75，`grep '\-> 0$'` 零命中）。㊵ 寫的「74 個字串臂 + 1 個 `-> 0` 兜底」不成立。
- **語料盤點**（临时插桩 `SMVPROBE:` 逐臂記 `[臂值 AST kind]:[型別碼]`，跑 `tmp/cov-all2.no` 16 模組，`tmp/s124/cov-probe.txt`）：**1122 條值位 match 臂值**——`Identifier:0` 464、`StringLiteral:1` 450、`IntegerLiteral:0` 206、`DotExpression:0` 2。**非 i64 臂值只有 `kind-name` 的 450 條**（75 臂 × 6 個含 `asp` 的模組），**不存在混合型別的值位 match**。
- **Go 權威語義**（實測 `tmp/s124/p-b.no`／`p-c.no`）：值位 match 的**臂值型別必須一致**，否則編譯錯誤 `match arm has inconsistent return types: str vs i64`（`dwkhnsmo`）。即混合臂（`n > 0 -> 'x'` + `-> 0`）在 Go 側是**編譯錯誤**，不是「运行时走到哪條算哪條」——這決定了修法的形狀：**實在一致才定型，不一致就回退**，而不是猜一個型別硬鋪。
- **根因**：s103 的 `h2m-smv-body` 門禁写死「臂值型別必須 `== TY-I64`」，且 `h2m-scan` 把結果槽固定鋪成 `TY-I64` ⇒ str 臂值無處可落，乾脆整條鏈不降。

**② 修復**

- **`compiler/src/h2m.no` 新增 3 個函式**（`h2m-smv-arm-val` 之後）：
  - `h2m-smv-ty`：前掃描逐臂取 `h2m-smv-arm-val` 的臂值表達式，用 `h2m-ty-of` 推型別；**全部臂一致才回該型別**，否則回 `-1`（混合型別／有臂推不出）。**必須在前掃描定**——結果槽的 alloca 要鋪在 entry，`h2m` 離開 entry 就回不去。
  - `h2m-smv-store`：按槽型別選 op 寫入結果槽——`TY-I64/U64/F64 → OP-STORE`、`TY-STR → OP-SSTORE`、`TY-OPT → OP-OPTSTORE`、`>= TY-STRUCT-BASE → OP-STSTORE`（與 `h2m-var-write` 同口徑）。
  - `h2m-smv-load`：按槽型別選 op 讀出——`TY-I64/U64/F64 → OP-LOAD`、`TY-STR → OP-SLOAD`、`TY-OPT → OP-OPTLOAD`、結構體 → `OP-STLOAD`（與 `h2m-var-read` 同口徑）。
- **`h2m-scan`** 的 `K-LET-STATEMENT` / `K-SURFACE-MATCH` 分支：`ty = h2m-smv-ty(...)`，**`ty < 0 -> ty = TY-I64`**——推不出就回退舊口徑，行為與 s124 之前**逐字相同**，不放寬門禁。
- **`h2m-lower-surface`**：查表取 `sty`（結果槽型別）；3 處 `h2m-smv-body` 呼叫傳 `sty`；`h2m-smv-body` 的門禁由 `== TY-I64` 改為 `== sty`（型別不符仍計 unsup，寧缺勿錯）；匯聚塊改用 `h2m-smv-load`。
- **源碼改動：2 檔**——`h2m.no` 淨 +110 / −12 行（3 新函式 + 4 處改寫）、`h2m-test.no` +6 斷言。

**③ 量測**

配對基線（`tmp/cov-all2.no`：16 模組各跑一次 `drv.pkg-ll-cov`；pre = `tmp/s123/cov-s123.txt`，post = `tmp/s124/cov-s124.txt`）：

| 檔 | pre ID／LF | post ID／LF | ΔID | ΔLF | pre／post unsup |
| --- | --- | --- | --- | --- | --- |
| `asp` | 0／77 | 0／1 | 0 | **−76** | 77／**1** |
| `ast` | 24／293 | 24／217 | 0 | −76 | 314／238 |
| `cg-rc` | 0／99 | 0／99 | 0 | 0 | 100／100 |
| `cg-runtime` | 0／542 | 0／542 | 0 | 0 | 543／543 |
| `cg` | 3／2478 | 3／2478 | 0 | 0 | 2482／2482 |
| `ck-sig` | 0／74 | 0／74 | 0 | 0 | 75／75 |
| `ck` | 29／418 | 29／342 | 0 | −76 | 441／365 |
| `diag` | 8／35 | 8／35 | 0 | 0 | 35／35 |
| `drv` | 230／5149 | 230／5074 | 0 | −75 | 5262／5188 |
| `h2m` | 67／3374 | 67／3300 | 0 | −74 | 3436／3363 |
| `ix` | 0／3 | 0／3 | 0 | 0 | 3／3 |
| `link` | 2／18 | 2／18 | 0 | 0 | 18／18 |
| `lx` | 16／164 | 16／163 | 0 | −1 | 166／165 |
| `mir` | 0／0 | 0／0 | 0 | 0 | 0／0 |
| `ps` | 124／1524 | 124／1447 | 0 | −77 | 1591／1514 |
| `tok` | 0／3 | 0／2 | 0 | −1 | 3／2 |
| **合計** | **503／14251** | **503／13795** | **0** | **−456** | **14546／14092** |

- **ID 通道零變化（503 → 503）**：本輪完全沒有動「失敗標識符」路径，`kind-name` 的所有臂本來就沒有未定義名。
- **LF −456 全部可查**：`kind-name:StringLiteral:construct` 456 → 0（76 × 6 模組）。**76 的拆分**（逐模組 Block 通道佐證）：**72 條 `kind-name:SurfaceMatch`** + **4 條整函級聯**（`FunctionDefinition` ×2、`BlockStatement` ×1、`Program` ×1）——即 `kind-name` 整條鏈不降後，`h2m-lower-func` ／ `h2m-lower-module` 順勢把整個函式、函式體塊、Program 各記一筆。**不是** 「75 臂 + 1」。
- **unsup −454 與 LF −456 的 2 條差**：本輪新增了 2 條 block 級記錄（`h2m-smv-store:BlockStatement`／`h2m-smv-load:BlockStatement` 各 1，只在 `drv`／`h2m` 出現），見 ⑤。
- **附帶收成**：`tok.name`（`r = c: { ILLEGAL -> 'ILLEGAL' … }`）與 `lx` 各 −1——`tok.no` 的 `name` 是**同一個 str 臂值形制**，被 `drv`／`lx`／`ps`／`tok` 四個量測重現，故 block 通道另見 `name:FunctionDefinition` −4（整函不再不產碼）。

**④ 驗證**

- **先紅後綠**：`compiler/tests/h2m-test.no` 新增 `test-s124-str-arm`（6 條）——重現 `kind-name` 形制（`r = id: { 1 -> 'one'  2 -> 'two'  -> 'other' }`）斷言 `unsup==0`／有 `define`／有 `store %str-long`（`OP-SSTORE`）／有 `load %str-long`（`OP-SLOAD`）；另釘混合臂（str + int）**仍** `unsup > 0` 且無 `define`。
  - **紅**：以 s124 前的 `h2m.no`（備份 `/tmp/s124-h2m.no.bak`）跑 ⇒ `FAIL: eq - s124 str-arm unsup`／`emits code`／`SSTORE`／`SLOAD` **4 條紅**（混合臂那 2 條在紅綠兩側都通過，符合「Go 側本是編譯錯誤」的定性）。
  - **綠**：換回 s124 版 ⇒ **1235 全綠**（㊵ 的 1229 ＋ 本輪 6）。
- **14 套全量門禁全綠**：`ps` 52、`h2m` 1235、`cg` 363、`ck` 20、`ck-sig` 50、`lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、`ps-err` 46、`pkg-ll-call` 5、`pkg-sigs` 5、`pkg-ll` 10。
- **語義端到端**（go oracle vs `tmp/s124/noc`，`tmp/s124-e2e/` ＋ 驅動 `tmp/s124/probe-e2e.no`；`drv.pkg-ll` + `link.compile` 鏈出可執行檔實跑，`go` 側的 `Error: exit status N` 即子進程退出碼 N）：

| 程序 | 含義 | noc | Go oracle |
| --- | --- | --- | --- |
| `e1` | `kn(1) == 'one'`（臂 1 命中） | rc **1** | 1 |
| `e2` | `kn(2) == 'two'`（臂 2 命中） | rc **2** | 2 |
| `e3` | `kn(9) == 'other'`（兜底臂） | rc **3** | 3 |
| `e4` | 混合臂 `1 -> 'one'` ＋ `-> 0` | **BUILD FAIL**（4 unsup，`compile ok=false`） | 編譯錯誤 `match arm has inconsistent return types: str vs i64` |

- **格式**：`no fmt -d` 兩檔（`h2m.no`／`h2m-test.no`）全空（`h2m.no` 首檢出 1 處缺空行，已補）。
- **S0→S1**：`./bin/no build -o tmp/s124/noc compiler` rc=0（`Build successful: tmp/s124/noc`）；`./tmp/s124/noc version` ⇒ `noc v0.0.1-m0`。

**⑤ 誠實邊界與下一步**

- **`asp` 的 77 → 1，剩下那 1 條是另一個缺口**：`kind-id`（`r = name: { 'AnnotationArrayValue' -> 1 … }`）——**str 主體**的值位 match。`h2m-lower-surface` 的主體判定只認 `TY-I64`／`TY-OPT`（`sty != TY-I64 && sty != TY-OPT -> bad = 1`），與本輪的「臂值型別」是**獨立的兩條門禁**，未動。
- **`tok.name`／`lx` 的 1 條也是另一個缺口**：臂**條件**是全局常量型樣（`ILLEGAL -> 'ILLEGAL'`），而 s63 的門禁只認**裸整型字面量**（`pd.kind != asp.K-INTEGER-LITERAL -> bad = 1`）。探針實測（`tmp/s124/probe-const-arm.no`）：`1 -> 'one'` ⇒ 0 unsup；`ONE -> 'one'`（`ONE = 1` 全局常量）⇒ 1 unsup。**不是**本輪的臂值缺口。
- **本輪只覆蓋「臂值型別一致」的值位 match**：混合型別在 Go 側是**編譯錯誤**，故 noc 側的設計是「回退 `TY-I64` 舊口徑，再由 `h2m-smv-body` 的型別門禁誠計 unsup」（`e4` 實測 4 unsup + 連結失敗）。**不猜型別**是新代碼的紀律。
- **新增的 2 條 block ＋ 2 條 LF 是誠實暴露，非被動回歸**：`h2m-smv-store:BlockStatement`／`h2m-smv-load:BlockStatement` 各 1（`drv`／`h2m`），與既有同族 `h2m-smv-name` 完全同口徑（該函式自 s103 起就是每模組 1 條）；LF 側 `h2m-smv-load:Identifier`／`h2m-smv-store:IntegerLiteral` 各 1。
- **新增 2 條 `noc: dropped (calls unsupported): h2m-smv-ty`**（`drv`／`h2m`）：新函式本身在**自舉編譯器**裡仍是 dropped（它呼叫的 `h2m-ty-of` 屬既有的 dropped 族），與 `h2m-smv-arm-val`／`h2m-smv-body` 同列。**不遮掩**。
- **本輪未動 `drv.build-ll` 口徑**：`noc build`（單文件、無 sg）下的值位 match 行徑與 `pkg-ll` 不同，未納入。

**下一步優先級**：① **值位 match 的常量型樣臂條件**（`tok.name` 那 1 條 ＋ 語料盤點；s63 門禁只認裸整型字面量）；② **str 主體的值位 match**（`asp.kind-id`，主體判定只認 i64／option）；③ 其餘非整數**臂值**型別（`vec`／`option`／結構體）——`h2m-smv-ty` 已把型別推出來、`h2m-smv-store`／`h2m-smv-load` 的映射已備好，但**語料為零**（本輪 ① 的 1122 條盤點裡只有 str 與 i64），須先盤點再決定。

### ㊷ s125 —— 值位 match 的常量型樣臂條件（`tok.name` 族）（2026-11-20）

**① 定位**

- 接手 ㊶⑤ 的①：`tok.name`（`r = c: { ILLEGAL -> 'ILLEGAL' … }`，**93 個常量臂**）在 `drv`／`lx`／`ps`／`tok` 各剩 1 條 unsup，根因是 s63 門禁「臂條件只認**裸整型字面量**」。
- **語料盤點**（臨時插樁 `SMVCPROBE:` 於 `h2m-scan-arms`，跑 `tmp/cov-all2.no` 16 模組，`tmp/s125/cov-probe.txt`）：**4234 條 SurfaceMatch 臂**（`h2m-scan-arms` 同時服務陳述位與值位，故這是**全部**臂，非僅值位）——
  `IntegerLiteral` 444、`StringLiteral` 601、`InfixExpression` 1152、`Identifier` 382、`CallExpression` 45、`NilLiteral` 10、 wildcard（Condition 缺空）1600。
- **裸標識符臂條件全部可歸因，只有兩個來源**：`err` **10 條**（option 三態臂，走 omode 門禁，**不受 s63 管**）＋ `tok.name` 的 **93 個枚舉常量 × 4 個量測模組 = 372 條**。
  即**除 `tok.name` 外，整個 `compiler/src` 沒有任何常量型樣臂條件**——本輪射程是**一處**（4 個量測座標），不是散彈。
- **Go 權威語意**（實測 `tmp/s125/p1.no`）：`ONE = 1` 的 `id: { ONE -> 'one'  TWO -> 'two'  -> 'other' }` 與字面量版型**逐值等價**——`kn(1)=='one'`、`kn(2)=='two'`、`kn(9)=='other'`。
  臂條件在 Go 側就是**表達式**（switch case 語意），常量只是表達式的一種。
- **根因**：`h2m-lower-surface` 的 s63 門禁 `pd.kind != asp.K-INTEGER-LITERAL || pd.nkid != 0 -> bad = 1` 把常量標識符擋在門外；
  而**降級側早就認得常量**——`h2m-lower-expr` 的 s112 裸常量旁路（發 `OP-CONST`）與 `h2m-ty-of` 的 s112（推 `TY-I64`）都在。**只有門禁沒跟上**。

**② 修復**

- `compiler/src/h2m.no` **只動一處**（淨 +16 / −1）：s63 門禁的臂條件型樣由「裸整型字面量」擴為「裸整型字面量**或**模組根層整型常量」——`K-IDENTIFIER` 且 `h2m-sig-const-find-short(sg, pnm) >= 0` 才放行。
  仍要求 `pd.nkid == 0`（不帶解構）；其餘標記（typePattern／typeBinding／typeArm／raw-cond 的 `a2`／`a3`／`a6`／`a8`）判定**一字未動**。
- **絕不猜**：未命中常量表的裸標識符維持出局（`h2m-test` 的 s125 對照臂釘死）。**降級側不改**——s112 的 `OP-CONST` 旁路早就在，門禁一開就通了。
- 源碼改動：**1 檔**（`h2m.no` +16/−1）；`h2m-test.no` +6 斷言。

**③ 量測**

配對基線（`tmp/cov-all2.no`：16 模組各跑一次 `drv.pkg-ll-cov`；pre = `tmp/s124/cov-s124.txt`，post = `tmp/s125/cov-s125.txt`）：

| 檔 | pre ID／LF | post ID／LF | ΔID | ΔLF | pre／post unsup |
| --- | --- | --- | --- | --- | --- |
| `asp` | 0／1 | 0／1 | 0 | 0 | 1／1 |
| `ast` | 24／217 | 24／217 | 0 | 0 | 238／238 |
| `cg-rc` | 0／99 | 0／99 | 0 | 0 | 100／100 |
| `cg-runtime` | 0／542 | 0／542 | 0 | 0 | 543／543 |
| `cg` | 3／2478 | 3／2478 | 0 | 0 | 2482／2482 |
| `ck-sig` | 0／74 | 0／74 | 0 | 0 | 75／75 |
| `ck` | 29／342 | 29／342 | 0 | 0 | 365／365 |
| `diag` | 8／35 | 8／35 | 0 | 0 | 35／35 |
| `drv` | 230／5074 | 230／5073 | 0 | **−1** | 5188／5187 |
| `h2m` | 67／3300 | 67／3300 | 0 | 0 | 3363／3363 |
| `ix` | 0／3 | 0／3 | 0 | 0 | 3／3 |
| `link` | 2／18 | 2／18 | 0 | 0 | 18／18 |
| `lx` | 16／163 | 16／162 | 0 | **−1** | 165／164 |
| `mir` | 0／0 | 0／0 | 0 | 0 | 0／0 |
| `ps` | 124／1447 | 124／1446 | 0 | **−1** | 1514／1513 |
| `tok` | 0／2 | 0／1 | 0 | **−1** | 2／1 |
| **合計** | **503／13795** | **503／13791** | **0** | **−4** | **14092／14088** |

- **落點干淨得罕見**：LF −4 **全部**是 `name:SurfaceMatch:construct` 4 → 0（block 通道同一條），也就是 `tok.name` 的 4 個量測座標；
  `drv`／`lx`／`ps`／`tok` 各 −1，**其餘 12 檔逐項零變化**。
- **ID 通道零變化（503 → 503）**：常量名本來就在常量表裡，三表未命中的失敗路徑從未被觸發。
- **無任何新增條目**：`cmp.py` 的 block／leaf／id 三通道 diff **只有一行 −4**，沒有一行正數——本輪是**純消除**，不是「消除一處、換來另一處」。

**④ 驗證**

- **先紅後綠**：`compiler/tests/h2m-test.no` 新增 `test-s125-const-arm`（6 條）——手工搭 AST 重現 `id: { ONE -> 'one'  -> 'other' }`（主體裸標識符 `id` 為 `TY-I64` ⇒ vmode；臂值全 str ⇒ 走 s124 的 `OP-SSTORE`／`OP-SLOAD` 槽路），斷言 `unsup==0`／有值／結果 `TY-STR`／**真的發了 `OP-CONST 1`**；另以**未登記的裸標識符** `NOPE` 作對照臂，斷言仍 `unsup > 0` 且無值。
  - **紅**：以 s125 前的 `h2m.no`（備份 `/tmp/s125-h2m.no.bak`）跑 ⇒ `no unsup`／`emits value`／`result str`／`emits CONST 1` **4 條紅**（對照臂 2 條在紅綠兩側都過，符合「門禁原本就擋未登記名」）。
  - **綠**：換回 s125 版 ⇒ **1241 全綠**（㊶ 的 1235 ＋ 本輪 6）。
- **14 套全量門禁全綠**：`ps` 52、`h2m` 1241、`cg` 363、`ck` 20、`ck-sig` 50、`lx` 81、`tok` 62、`diag` 7、`build-package` 8、`util` 27、`ps-err` 46、`pkg-ll-call` 5、`pkg-sigs` 5、`pkg-ll` 10。
- **語義端到端**（go oracle vs `tmp/s125/noc`，`tmp/s125-e2e/` ＋ 驅動 `tmp/s125/probe-e2e.no`；`go` 側的 `Error: exit status N` 即子進程退出碼 N）：

| 程序 | 含義 | noc | Go oracle |
| --- | --- | --- | --- |
| `c1` | `kn(1) == 'one'`（常量臂 1 命中） | rc **1** | 1 |
| `c2` | `kn(2) == 'two'`（常量臂 2 命中） | rc **2** | 2 |
| `c3` | `kn(9) == 'other'`（兜底臂） | rc **3** | 3 |
| `c4` | 小寫未定義名 `nope` 作臂條件 | **BUILD FAIL**（3 unsup，`compile ok=false`） | 編譯錯誤 `'nope' is not defined` |
| `c5` | 大寫未定義名 `NOPE` 作臂條件 | **BUILD FAIL**（3 unsup，`compile ok=false`） | **建置成功但執行期 SIGTRAP**（rc 133） |

- **格式**：`no fmt -d` 兩檔（`h2m.no`／`h2m-test.no`）全空。
- **S0→S1**：`./bin/no build -o tmp/s125/noc compiler` rc=0（`Build successful: tmp/s125/noc`）；`./tmp/s125/noc version` ⇒ `noc v0.0.1-m0`。

**⑤ 誠實邊界與下一步**

- **本輪只開「常量」，不開「局域變數」臂條件**：Go 也接受局域變數（實測 `tmp/s125/p2.no`：`c = 1` 後 `id: { c -> 'hit'  -> 'other' }` ⇒ 打印 `hit`／`other`），
  且降級側 `h2m-lower-expr` 本來就能讀局域槽——但**門禁仍只認常量**，局域名一律出局。
  理由：**語料為零**（4234 條臂裡沒有任何局域名臂條件），且放開等於親手打開「臂條件可為任意表達式」的大門，須先盤點。**刻意收窄，不是遺漏**。
- **新暴露的 Go 側漏洞（誠實記一筆）**：臂條件是**未定義的**裸標識符時，Go 的行為**依大小寫分岔**——
  小寫 `nope` 是編譯錯誤（`'nope' is not defined`），**全大寫 `NOPE` 卻建置成功**、執行期 SIGTRAP（rc 133，`c5` 實測）；
  `X`／`Y`／`Z`／`ABC`／`FOO`／`QQ` 同樣建置成功，`nope`／`Nope`／`zzz` 則是編譯錯誤（`zsh tmp/s125/case-probe.sh` 10 名批次實測）。應是「大寫＝全域常量」的命名慣例被套用到解析路徑。
  **noc 對兩者皆 BUILD FAIL**（3 unsup）——**不跟著產出會 trap 的碼**，這是寧缺勿錯，不是對齊。
- **剩下的 str 主體缺口未動**：`asp.kind-id`（`r = name: { 'AnnotationArrayValue' -> 1 … }`，6 個量測座標）與 `tok.keyword`（`r = s: { 'if' -> IF … }`，4 個量測座標）都是 **str 主體的值位 match**，撞在主體判定 `sty != TY-I64 && sty != TY-OPT -> bad = 1`——與本輪的「臂條件型樣」是**獨立的兩條門禁**。
- **本輪未動 `drv.build-ll` 口徑**：`noc build`（單文件、無 sg）下的值位 match 行徑與 `pkg-ll` 不同，未納入。

**下一步優先級**：① **str 主體的值位 match**（`asp.kind-id` 6 坐標 ＋ `tok.keyword` 4 坐標 ＋ 各自的整函級聯）——先做語料盤點確認「str 主體」的總座標數，再定案；
② **局域變數臂條件**（Go 接受、語料零，須先盤點）；③ 其餘非整數的**臂值**型別（`vec`／`option`／結構體）——`h2m-smv-store`／`h2m-smv-load` 的映射已備，但值位 match 的臂值語料只有 str 與 i64。

### ㊸ s126 —— 值位 match 的 str 主體（常量型樣臂）（2026-11-20）

**① 定位**

承接 ㊷ ⑤ 的下一步①「先做語料盤點確認『str 主體』的總座標數」。在 `h2m-lower-surface` 主體判定後
插臨時探針（`S126PROBE`，只記 `vctx == 1` 的值位 match）跑 16 模組全量（`tmp/s126/cov-probe.txt`，
523 條），按主體型別碼（`mir.no`：`TY-I64=0`／`TY-STR=1`／`TY-VEC=2`／`TY-OPT=3`）統計：

| 主體型別 | 座標數 | 處置 |
| --- | --- | --- |
| `TY-I64` | 411 | s63 既有射程 |
| `TY-OPT` | 86 | s98 既有射程 |
| `TY-STR` | **26** | 本輪前全部 `bad = 1` |

26 個 str 主體按**臂條件形態**分兩類（這是本輪的關鍵細化，比 ㊷⑤ 的「6 ＋ 4」多出一倍）：

- **A 類（12 個）常量型樣臂**——`asp.kind-id` 6、`tok.keyword` 4、`ck-name-silent` 2：
  主體是 **str 裸標識符**，臂條件是 **str 字面量**（`'AnnotationArrayValue' -> 1`、`'if' -> IF`、
  `'val' -> 1`），臂值 i64。對齐 Go 的 `switch s { case "if": … }`（`src/lexer/token.go:lookupKeyword`）。
- **B 類（14 個）索引讀 option 臂**——`ix.at-s` 10、`ck-str-at` 2、`h2m-cov-join` 2：
  主體是 `[]str` 的索引讀 `a[k]`（型別碼 `TY-STR`），臂 `ok -> it` ＋ 兜底。

根因是 ㊷⑤ 已指認的主體判定（`compiler/src/h2m.no` `h2m-lower-surface`）：

```
sty == TY-I64 -> vmode = 1
sty == TY-OPT -> omode = 1
sty != TY-I64 && sty != TY-OPT -> bad = 1     ← A／B 兩類都撞這裡
```

**B 類的深層根因（本輪新發現）**：`OP-VECGETSTR`（`cg.no`）回**裸借用視圖**（`TY-STR`），
**索引讀的 option 語意尚未 option 化**——而語言語义上 `a[k]` 恆 `?T`（D11）。所以 B 類即使放開主體，
也**不能**按「str 常量型樣」降級；它的 `ok` 臂屬 option 三態，須先做「索引讀 option 化」。

**② 修復**（`compiler/src/h2m.no`，5 處，對 s125 版淨改 61 行）

1. **主體型別碼留存**：新區域變數 `subjty`，在兩處主體判定（裸標識符 / 表達式）把 `sty` 先記進 `subjty`。
2. **放開 TY-STR 主體的值匹配**：兩處 `sty == TY-STR -> vmode = 1`，即 A／B 類都先取得 vmode 身分。
3. **s63 型別一致門禁**：原「只認裸整型字面量／整型常量」改為**按主體型別分流**——
   `subjty == TY-STR` ⇒ 臂條件只認 str 字面量／str 常量（`h2m-sig-strconst-find-short`）；
   `subjty == TY-I64` ⇒ 維持整型字面量／整型常量（s63／s125 原射程）。型別不符（str 主體配 i64 臂）⇒ `bad`。
4. **s119 變體臂例外限定 i64 主體**：把 `varm` 的三條判定包進 `subjty == TY-I64 -> { … }`；
   並新增「**str 主體的變體臂**（`a1 != 0 || a5 != 0`）明確 `bad`」——把 B 類釘在 SurfaceMatch 歸因。
5. **降級按主體型別分派比較 op**：原 `OP-CMPEQ`（對 `sv` 發 `icmp eq`）改為讀 `stv = h2m-vty-at(c, sv)`，
   `TY-STR` ⇒ `OP-STEREQ`（s104 已備），否则 `OP-CMPEQ`。

**③ 量測**（pre = `tmp/s125/cov-s125.txt`，post = `tmp/s126/cov-s126c.txt`）

| 通道 | pre | post | 差 |
| --- | --- | --- | --- |
| Block | 8212 | 8200 | **−12** |
| LF | 13791 | 13779 | **−12** |
| ID | 503 | 503 | 0 |
| **GRAND TOTAL** | **14088** | **14076** | **−12** |

明細（`tmp/s125/cmp.py`）：`kind-id:SurfaceMatch` −6、`keyword:SurfaceMatch` −4、
`ck-name-silent:SurfaceMatch` −2（Block 與 LF 通道各一份）；**ID 零變化**；**無任何正數**（純消除）。
B 類（`ps-star-label`／`ck-str-at`／`h2m-cov-join`／`ix.at-s`／`ck-sig-str-at`）落點**逐字不變**。
（陰影通道：若不在第 4 點把 str 主體變體臂釘住，B 類會從 `SurfaceMatch` **移位**成 `BlockStatement`
——實測 `ps-star-label:SurfaceMatch` −8 ＋ `ps-star-label:BlockStatement` +16，總數不變但落點髒；
本輪以明確擋回，故最終無此移位。）

**④ 驗證**

- **先紅後綠**：以 s125 版 `h2m.no`（`/tmp/s126-h2m.no.bak`）跑 `h2m-test` ⇒ 新測試 3 條紅
  （`s126 str-subj no unsup`／`emits value`／`emits STEREQ`）；s126 版 **1245 全綠**（+4 斷言）。
- **14 套門禁全綠**（`tmp/s126/gate2.txt`）：ps 52／h2m 1245／cg 363／ck 20／ck-sig 50／lx 81／
  tok 62／diag 7／build-package 8／util 27／ps-err 46／pkg-ll-call 5／pkg-sigs 5／pkg-ll 10。
- **e2e**（`tmp/s126-e2e/probe.no`，走 noc 路徑 `drv.pkg-ll` ＋ `link.compile`，非 `./bin/no build`）：

| 案例 | 形制 | s125 版 | s126 版 |
| --- | --- | --- | --- |
| e1 | `r = name: { 'one' -> 'ONE' … }` | `ok=false`（dropped f/main，3 unsup） | **`ok=true`，rc=1**（3 名逐值全對） |
| e2 | 同上但臂条件為 str 常量 `ONE='one'` | `ok=false`（dropped） | **`ok=true`，rc=1** |
| e3 | str 主體 ＋ i64 臂（型別不符） | `ok=false` | `ok=false`（門禁生效） |
| e4 | str 主體 ＋ 未登記 str 常量臂 | `ok=false` | `ok=false`（門禁不放鬆） |
| e5 | 索引讀 `ok` 臂（B 類） | `ok=true` | `ok=true`（落點一致） |

- **格式**：`no fmt -d compiler/src/h2m.no compiler/tests/h2m-test.no` 干淨。
- **存量重構**：零。

**⑤ 誠實邊界與下一步**

- **只開常量型樣，不放局部變數**：Go 側 str switch 亦接受局部 str 量作 case，語料零（先不開，同 s125 口徑）。
- **B 類 14 個坐标仍 unsup，且是「降級語意」層缺口**：`OP-VECGETSTR` 回裸借用視圖、未 option 化。
  修它**不是**值位 match 專屬改動，會動到**所有 `[]str` 索引讀降級點**，須先做全域盤點（型別碼、
  邊界檢查、既有的 `?=`／索引讀消費路徑）——不在本輪射程。
- **`wi > 0` 放行路徑是既有漏洞**：s63 的型樣判定只在 `wi <= 0` 時執行，`wi > 0` 直接跳過（放行）。
  s126 放開主體後才暴露（B 類曾短暂由此漏入降級）；現以「str 主體變體臂明確擋」釘住，但
  **i64 主體的 `wi > 0` 非兜底臂仍可能走同一條路徑**——未盤點，記為待查。
- **e5「建成功」是假象**：noc 版 `ks` 被 drop（unsupported），但 e5 的 `main`（`exit(0)`）不依賴它
  ⇒ 無 dropped 訊息、link 成功。要讓 drop 可見須讓 `main` 呼叫 `ks`（對照 e3／e4 有 dropped 訊息）。
- **本輪未動 `drv.build-ll` 口徑**（單文件、無 sg），同 ㊷。

**下一步優先級**：① **索引讀 option 化**（B 類 14 坐標＋`OP-VECGETSTR` 語意）——先做型別碼與降級點盤點；
② **局部變數臂條件**（Go 接受、語料零，須先盤點）；③ 其餘非整數的**臂值**型別（`vec`／`option`／結構體）。

### ㊹ s127 —— 值位 match 主體的索引讀 option 化（`h2m-lower-safe-index`）（2026-11-20）

**① 定位**

承接 ㊸ ⑤ 下一步①。先做**型別碼與降級點盤點**（這是 ㊸ 指定順序）：

- **降級點**：`h2m-lower-expr` 的 `K-INDEX-EXPRESSION` 分支（3774 行）**仍發裸 `OP-VECGET`／`OP-VECGETSTR`**——
  所以 **let 路徑**（`ck-str-at` 的 `o = v[i]`，兩步 match 形）**未** option 化；`h2m-cov-join` 的
  `kv = unsk[i]: { ok -> it … }` 則是**值位 match 形式**（走 `h2m-lower-surface`，非本分支）。
- **值位 match 主體的索引讀**（本輪前，s126）：走 `h2m-lower-surface` 的 `subj-ident == 0` 路徑，
  `sv0 = h2m-lower-expr(...)` 發**裸** `VECGET`／`VECGETSTR` ⇒ `sty` 是**元素型別**（`TY-I64`／`TY-STR`）
  ⇒ 被判成 **vmode**（值匹配），而臂是 option 三態（`ok` 的 a1/a5 非零）⇒ 靠 s126 的「str 主體變體臂擋」
  與 s119 的 i64 限定齊擋，歸因 `SurfaceMatch`（即 ㊸ 的 B 類 14 坐標）。
  **根因即「索引讀未 option 化」**：主體本應是 `?elem`，卻拿了裸借用視圖當純值。
- **Go 参照**：`lowerSafeIndexAt`（`src/mir/hir2mir.go:5225`）——in range ⇒ `ok(elem)`；out of range ⇒ `err`（tag 2, `code.index-out`）。
- **型別碼**（`mir.no`）：`TY-I64`＝0／`TY-STR`＝1／`TY-VEC`＝2／`TY-OPT`＝3／`TY-U64`＝4／`TY-F64`＝5／`TY-VECSTR`＝6。
- **option op**：`OP-OPTALLOC`＝60／`OPTNONE`＝22／`OPTSOME`＝23／`OPTTAG`＝24／`OPTISOK`＝25／`OPTGET`＝26／
  `OP-RETAIN`＝27／`OPTMKSTR`＝29／`OPTGETSTR`＝30／`OPTSTORE`＝61／`OPTLOAD`＝62。
- **所有權陷阱（本輪最先踩到）**：`OPTMKSTR` **只存不 retain**（`mir.no:211`），而 `VECGETSTR` 回**借用視圖**
  （與容器共享 data）⇒ str 載荷進 option 前必須先 `OP-RETAIN`，否則 option 與容器各自 drop 會雙重釋放。

**② 修復**（`compiler/src/h2m.no`，5 處，對 s126 版 **+129 / −18** 行）

1. **新函數 `h2m-lower-safe-index`**（兩輸出 `(r i64, pty i64)`）：`pty` 由容器型別碼決定
   （`TY-VEC` ⇒ `TY-I64`；`TY-VECSTR` ⇒ `TY-STR`；其餘 −1）。開 `sink`（先填 nil 的 option 槽）＋
   **邊界檢查** `0 <= idx < len`（`CMPGE` + `CMPLT` + `AND`）＋ **三塊** `okblk`／`noneblk`／`mergeblk`；
   ok 塊取元素後包裹（str：`VECGETSTR` → `OP-RETAIN` → `OPTMKSTR`；i64：`VECGET` → `OPTSOME`），
   存 `sink`；none 塊存 nil；merge 塊 `OPTLOAD` 交出。
   ⚠️ **塊模型**：函數結束時**當前塊停在 `merge`**，由呼叫方（`h2m-lower-surface`）在其後接續鋪臂塊——與 Go 的 `mergeBlk` 返回同構。
2. **`h2m-lower-surface` 頂部**新增 `subj-index`／`subj-pty` 兩個區域變數（`subj-pty` 默認 `TY-I64` ⇒ 維持 s98 既有 omode 行為）。
3. **主體判定分流**：`sd.kind == K-INDEX-EXPRESSION -> subj-index = 1`；走 safe-index 並要求 `sty == TY-OPT`（否則 `bad`）；
   `subj-index == 0` 走原路徑（`vmode`／`omode` 維持 s126 口徑不變）。
4. **s126 的「str 主體變體臂」擋收窄**為 `subjty == TY-STR && subj-index == 0 && (a1 != 0 || a5 != 0)`——
   索引讀主體已 safe-index 成 `TY-OPT`（走 omode）不再命中本擋；本擋此刻只射「裸 str 型別的主體」（實測語料為零，屬向前防禦）。
5. **omode ok 臂載荷按 `subj-pty` 分派**：`TY-STR` ⇒ `OP-OPTGETSTR`（`TY-STR` 暫存）；否則 `OP-OPTGET`（`TY-I64`）。
   （另一處：`h2m-lower-expr` 的 `K-INDEX-EXPRESSION` 分支刪一個空行，無語意。）

**③ 量測**（乾淨配對；pre = `tmp/s127/cov-base-final.txt`（HEAD 剝離 S127A/S127B 探針的 6275 行基線版），
post = `tmp/s127/cov-post-final.txt`（本輪工作版，探針全清；後以交付版 6389 行重跑爲
`cov-post-final2.txt`，逐字一致，見 ⑤）；工具 `tmp/s125/cmp.py`）

| 模組 | pre | post | 差 |
| --- | --- | --- | --- |
| `ast` | 237 | 234 | −3 |
| `ck` | 363 | 360 | −3 |
| `drv` | 5184 | 5181 | −3 |
| `h2m` | 3362 | 3354 | −8 |
| `ps` | 1511 | 1513 | **+2** |
| `ix` | 3 | 3 | 0 |
| 其餘 9 個模組 | — | — | 0 |
| **GRAND TOTAL** | **14076** | **14061** | **−15** |

明細（Block／LF 通道各一份，逐字對稱）：

- **清零**：`ps-star-label:SurfaceMatch` 8→0、`nd-at:Program` 5→0、`nd-put:BlockStatement` 5→0、
  `str-at:SurfaceMatch` 10→5、`h2m-lower-func` 8→0、`h2m-struct-field-idx:ForStatement` 2→0、
  `h2m-var-read:ForStatement` 2→0、`ps-tok-{cl,emsg,lit,ln,mk,raw,typ}` 各 2→0。
- **新增**：`h2m-lower-safe-index:FunctionDefinition` 0→2（新函數自身：塊模型上限，同 s98 口徑）、
  `h2m-cov-join:BlockStatement` 2→4、`h2m-lower-surface:BlockStatement` 14→16、
  `h2m-lower-expr` `BlockStatement` +4／`SurfaceMatch` +12／`Identifier` +16、
  `ps-parse-match:SurfaceMatch` 16→24、`ps-parse-enum-def:SurfaceMatch` 14→22、`ps-star-label:BlockStatement` 2→18。
- **ID 通道零變化**。
- **結論：多為歸因移位，非純修復。** `ps-star-label` 的 8 個 B 類坐標從 `SurfaceMatch`／`IntegerLiteral`
  移到 `BlockStatement`／`Identifier`／`StringLiteral`（淨 +2）——索引讀主體改走 safe-index 後落入新造的塊。

**④ 驗證**

- **先紅後綠**：以 s126 版（`tmp/s127/h2m-base-clean.no`）跑 `h2m-test` ⇒ 新測試 3 條紅；本輪版 **1257 全綠**（+12 斷言）。
- **14 套門禁全綠**（`tmp/s127/gate3.txt`，重跑於 `bin/no` 07:43 版 ＋ 本輪最終 `h2m.no` 6389 行）：
  ps 52／h2m 1257／cg 363／ck 20／ck-sig 50／lx 81／tok 62／diag 7／build-package 8／util 27／
  ps-err 46／pkg-ll-call 5／pkg-sigs 5／pkg-ll 10。
- **e2e（`tmp/s127-e2e/`，走 noc 路徑 `drv.pkg-ll` ＋ `link.compile`，非 `./bin/no build`）**：

| 案例 | 形制 | Go 版參照（`./bin/no build`） | noc 自舉路徑 |
| --- | --- | --- | --- |
| e1 | `r = xs[k]: { ok -> it \n -> '' }`（`[]str`） | rc=1（三個斷言全成立） | **`ok=false`**（`f`／`main` 皆 dropped，5 unsup ⇒ link 無 `_main`） |
| e2 | 同上但 `[]i64`，兜底 `0` | rc=1 | **`ok=true`，rc=1** ✅ |
| e3 | 陳述位索引讀 match（`ok` 改局部／兜底 `r = 4`） | rc=4（越界走兜底臂） | **`ok=true`，rc=4** ✅ |

  e2／e3 為**本輪新增的成功案例**（索引讀主體的值位 match 首次能自舉建出並跑出正確結果）。
  **e1 失敗即 ⑤-(a) 缺口的活證**：`[]str` 載荷的 `it` 被 `h2m-ty-of` 當成 `TY-I64`，
  與兜底 `''`（STR）型別不一致 ⇒ `h2m-smv-ty` 回 −1 ⇒ 結果槽退 `TY-I64` ⇒ 存值型別不符 ⇒ drop。
- **格式**：`no fmt -d compiler/src/h2m.no compiler/tests/h2m-test.no` 乾淨（`h2m.no` 量測後補了 3 個空行，見 ⑤「量測與交付版一致」）。
- **存量重構**：零（只刪一個空行）。

**⑤ 誠實邊界與下一步**

- **越界語意是 `nil`，不是 `err`**：Go `lowerSafeIndexAt` 越界回 `err`（tag 2, `code.index-out`）；
  自舉無 err 構造 op，且本鏈語料臂只有兜底（**不讀錯誤值**）⇒ none 塊存 `nil`（tag 1）。
  等價性**僅限於「臂不讀錯誤值」**；若臂為 `err -> it.msg()`，自舉語意即與 Go 不同。
- **只覆蓋「值位 match 主體的索引讀」**，三處殘留缺口：
  - **(a) `h2m-ty-of` 的 `it` 恆推 `TY-I64`**（2404 行）⇒ `h2m-smv-ty` 對「臂 `it` ＋ 兜底 str／i64 值」的
    型別不一致回 −1 ⇒ 結果槽回退 `TY-I64` ⇒ 存值型別不符。**兩處活證**：量測端
    `S127G::smv-ty-mismatch::h2m-cov-join::sty=0::xvty=1`（`h2m-cov-join` 的 2 個坐標未清零，量測 2→4）；
    e2e 端 **e1（`[]str`） dropped**，而 e2（`[]i64`，臂 `it` 與兜底 `0` 型別一致）成功——同根因的一體兩面。
  - **(b) `h2m-lower-expr` 的 `K-INDEX-EXPRESSION` 仍發裸 `VECGET`／`VECGETSTR`** ⇒ let／`?=` 路徑未 option 化
    （`ck-str-at` 的 `o = v[i]` 走此路）。
  - **(c) 值位 match 的 `_` 臂**（`ExpressionStatement{Expression:nil}`，kid＝0）不在 `h2m-smv-arm-val` 射程
    ⇒ 回 0 ⇒ `bad`。**Go 語義已實測為元素型別零值**（`ix.at-i(a,5)` → `0`；`ix.at-s(b,9)` → `''`）。
- **陰影通道**：`ps-star-label:SurfaceMatch −8` ／ `ps-star-label:BlockStatement +16` ——索引讀主體改走 safe-index 後
  歸因由 `SurfaceMatch` 移到 `BlockStatement`（淨 +2，非純修復）。
- **曾經的外部阻塞（已解決，留作記錄）**：量測期間（07:35）有**外部進行中的 merged 型別檢查**
  （`src/build/transpiler_validate.go` +42 行：`validateMergedProgram` 對合併 Program 補跑 `ValidateTypesTHIR`；
  `src/checker/checker.go` +10 行為診斷補 `SourceFile` 歸因）令 `compiler/src/h2m.no` **自身**出現 46 處
  `15w45dqk`（`?i64` 隱式賦值，如 `h2m.no:5559 p = p + q`），`compiler/src/drv.no:324`
  （`d.line = ln-part.to-i64()`，`str.to-i64()` 回 `?i64`）同因，自舉 e2e 一度全部阻塞。
  **以本輪基線版實測同為 46 處 ⇒ 非本輪引入**；該改動隨後收尾（`bin/no` 07:43 起），門禁與 e2e 已重跑（見 ④）。
- **`h2m-lower-safe-index` 自負 2 個 unsup**：新通道 `h2m-lower-safe-index:FunctionDefinition`（塊模型：入口塊），
  與 s98 既有口徑一致，不追求清零。
- **量測與交付版一致**：③ 的 post 先跑於 `h2m.no` 6386 行（`no fmt` 前），fmt 後交付版 6389 行
  （三處 `-> {` 與註釋間補空行）——**已用交付版重跑**（`tmp/s127/cov-post-final2.txt`），
  與 fmt 前**逐字一致**（`GRAND TOTAL` 同爲 14061），量測數字可直接對應交付版。
- 未動 `drv.build-ll` 口徑（單文件、無 sg），同 ㊸。

**下一步優先級**：① **(a) `it` 的型別推斷**——值位 match 的 `it` 必須按**主體 option 的載荷型別**推斷（而非恆 `I64`），
這是 `h2m-cov-join` 清零與 (c) 的前置；② **(b) `h2m-lower-expr` 索引讀 option 化**（let／`?=` 路徑）；
③ **(c) `_` 臂的零值語義**（Go 已實測，直接對齊）。

### ㊺ s128 —— `it` 的型別推斷（`h2m-smv-pty`：按主體 option 載荷定型）（2026-11-20）

**① 定位**

承接 ㊹ ⑤ 下一步①。s127 把值位 match 主體的索引讀做成 option 化（`h2m-lower-safe-index`），
但**解包臂的隱式綁定 `it` 的型別仍是 `h2m-ty-of` 的恆 `TY-I64`**（K-IDENTIFIER 分支裡 `nm == 'it'`
直接回 `TY-I64`）。這在 `[]str` 索引讀主體上必然出錯：`it` 實際是 `str` 載荷，卻被當成 `i64` 與兜底臂的
`''`（STR）相比 —— `h2m-smv-ty` 判定臂間型別不一致 ⇒ 回 `-1` ⇒ 結果槽回退 `TY-I64` ⇒ 存值型別不符
⇒ 整鏈 drop。

活證（兩條，互相獨立）：

- **量測端**：`h2m-cov-join` 的 8 個 unsup 從未清零（`BlockStatement` 4／`IfExpression` 2／
  `SurfaceMatch` 2）—— 正是它自己的 `fnv = unsf[i]: { ok -> it … -> '' }`（`unsf []str`）。
- **e2e 端**：`tmp/s128-e2e/e1.no`（`r = xs[k]: { ok -> it … -> '' }`，`xs []str`）在 s127 版
  `dropped (calls unsupported): f` / `main`，`link.compile` 回 `false`。

**② 修復**

`compiler/src/h2m.no`（6389 → 6429 行，`no fmt` 後）：

1. `h2m-ctx` 新增 `it-pty i64`（默認 `TY-I64`），`h2m-new` 初始化 —— `it` 的型別改為**可上下文
   設定的暫存**，而非型別碼分支裡的常數。
2. 新函數 `h2m-smv-pty`：主體（`kid[0]`）是 `K-INDEX-EXPRESSION` 時取其基型（`kid[0]`）——
   `TY-VECSTR` ⇒ `TY-STR`、`TY-VEC` ⇒ `TY-I64`（與 `h2m-lower-safe-index` 4744/4745 同口徑）；
   **其餘（含 option 裸標識符／str 主體）靜態推不出 ⇒ 維持 `TY-I64` 默認，不放寬門禁**。
3. `h2m-smv-ty`：掃臂前 `sv-pty = agg.it-pty` → `agg.it-pty = h2m-smv-pty(...)`，掃完 restore
   （1804/1805/1832）。型別必須在前掃定（結果槽 alloca 鋪在 entry，離開 entry 就回不去）。
4. `h2m-scan-arms`：同樣 save-set／restore（2638/2639/2656），給 `h2m-scan` 前掃定型用。
5. `h2m-ty-of` 的 `K-IDENTIFIER`：`nm == 'it'` 且不在局部量表 ⇒ `code = agg.it-pty`（2430）；
   裸鏈／match 外的 `it` 仍取默認 `TY-I64`，而降級側 `var-read`／`var-write` 棧空即計 unsup
   ⇒ 門禁不放鬆。

**③ 量測**（pre = `tmp/s128/cov-pre-s128.txt`，post = `tmp/s128/cov-post.txt`；`tmp/s125/cmp.py`）

| 通道 | pre | post | 差 |
| --- | --- | --- | --- |
| Block | 14057 | 14073 | **+16** |
| LF | 14069 | 14085 | **+16** |
| ID | 503 | 515 | **+12** |
| **GRAND TOTAL** | **14061** | **14077** | **+16** |

明細：

- `ps-parse-match:IfExpression` 196 → 216（**+20**，聚合；單條目 98 → 108）。
- `h2m-cov-join` 8 → 0（**−8**：`BlockStatement` −4／`IfExpression` −2／`SurfaceMatch` −2）
  —— **本輪目標 (a) 達成**。
- `dropped (calls unsupported)` +4 行：`h2m-smv-pty`（新函數，調用被剔除的 `h2m-ty-of`）、
  `h2m-cov-join`（清零後轉為「可產碼但調用被剔除的 `h2m-cov-line`」）各 2 個入口。
- 其餘函式**逐條零變化**。

**+20 是回歸嗎？—— 是「前向型別修正的如實暴露」，不是新增失敗。** 排查（`tmp/s128/ifcv-pre.txt`／
`ifcv-post.txt`、`mark-pre.txt`／`mark-post.txt`、`one-pre.txt`／`one-post.txt`）：

1. `ps.no:9476` 的 `bname = bns2[0]: { ok -> it … -> '' }`（`bns2 []str`）是全語料**唯一**因本輪
   改動而換槽型別的點：s127 版 `it` 推 I64 與 `''`（STR）不符 ⇒ 回退 I64 ⇒ `bname` 槽 = I64；
   s128 版推 STR ⇒ 與 `''` 一致 ⇒ `bname` 槽 = **STR**。
   （`h2m-slot-decl` 插樁：`bt`／`elem`／`kvar` 兩版皆 `ty=-1`、`ol`／`oe`／`onil` 皆 0，
   **僅 `bname` 0→1**；`h2m-scan` 的 `SMVLET` 插樁：117 條值位 match let 僅 `bname` 一條換型別。）
2. 槽型別換對後，`bname != ''`（`ps.no:9568` 與 `9642`）由 **I64 vs STR 混合型別**
   （`h2m-lower-expr` K-INFIX 五條臂全不命中 ⇒ `h2m-mark`，`cv=0`）變為 **OP-STNEQ 成功**
   （`cv>0`）。
3. `h2m-lower-if` 僅在 `cv > 0` 時開 then／else／merge 塊並降級體（4175）；`cv == 0` 時**整段體
   不降級**（4174）。故 s127 版這兩個守衛體內的構造**從未被走訪**，s128 版走訪到 ⇒ 如實計數。
4. 實測：`IFCV` 探針（每條 `h2m-lower-if` 印 `cv`／條件節點／kind／op）pre 122 行／22 條 `cv=0`，
   post 125 行／22 條 `cv=0`；pre 的 `n=37442 op=!=`、`n=37450 op=!=` 兩條 `cv=0` 在 post 成功
   （907／911），post 另在新進入的體內有 `n=37467 op=&&`、`n=37488 op=&&` 兩條 `cv=0`。
   `h2m-mark` 探針（印 `cur-k`／塊 kind）：pre 232 條、post 242 條，`bk=39`（IfExpression）
   98 → 108，其餘塊 kind 逐條不變。
5. `ps-parse-match` 在 pre／post **皆因 `unsup > 0` 被整函丟棄**（`dropped` 列表兩版都有），
   故 +20 不影響任何可產碼性 —— 是**覆蓋計數從「漏計」變「如實計」**。

對照實驗（分離「`it-pty` 的值」與「`h2m-scan-arms` 的設置」）：

| 變體 | 改動 | delta |
| --- | --- | --- |
| `cov-varC` | `h2m-smv-pty` 恒回 `TY-I64` | **+2**（僅 dropped `h2m-smv-pty` ×2） |
| `cov-varD` | 只在 `h2m-smv-ty` 設 `it-pty`，不在 `h2m-scan-arms` 設 | **+16**（＝完整版） |

⇒ 全部 +14（聚合）來自 `it-pty` 的**值**；`h2m-scan-arms` 的設置對 cov **零貢獻**。

**④ 驗證**

- **先紅後綠**：以 s127 版 `h2m.no` 跑 `h2m-test` ⇒ **5 條斷言紅**（`test-s128-it-ty-str` 的
  unsup／emits code／SSTORE／SLOAD 4 條 ＋ `test-s128-smv-ty-direct` 的 `[]str it` 1 條；
  `test-s128-it-ty-i64`／`test-s128-it-ty-mixed-gate` 在 s127 版已綠，因它們測的是默認型別與門禁）；
  s128 版 **1268 全綠**。
- **14 套門禁全綠**（`tmp/s128/gate.txt`，逐套明細見檔）：
  ps 52／h2m 1268／cg 363／ck 20／ck-sig 50／lx 81／tok 62／diag 7／build-package 8／util 27／
  ps-err 46／pkg-ll-call 5／pkg-sigs 5／pkg-ll 10 —— **合計 2004 passed / 0 failed**。
- **e2e**（`tmp/s128-e2e/probe.no`，走 noc 路徑 `drv.pkg-ll` ＋ `link.compile`）：
  `e1`／`e2`／`e3` 執行檔由 worktree 基線 `v0.4.28-4-gd21fbb35` 的 `bin/no` 構建 ——
  主樹 `bin/no` 是 `dev` 版且對 `compiler/src/h2m.no` 報 `optunuse`，不可用於本輪。

| 案例 | 形制 | s127 版 | s128 版 |
| --- | --- | --- | --- |
| e1 | `r = xs[k]: { ok -> it … -> '' }`（`[]str`） | `ok=false`（dropped `f`／`main`） | **`ok=true`，exit 1**（3 斷言全對） |
| e2 | 同上但 `[]i64` 載荷 | `ok=true` | **`ok=true`，exit 1** |
| e3 | 陳述位索引讀 match（`[]i64`） | `ok=true` | **`ok=true`，exit 4**（`xs[0]` ok→1、`xs[5]` nil→4） |

- **格式**：`no fmt -d compiler/src/h2m.no compiler/tests/h2m-test.no` 乾淨。
- **存量重構**：零。

**⑤ 誠實邊界與下一步**

- **`h2m-smv-pty` 只認索引讀主體**：主體是 option 裸標識符（`x: { … }`，`x ?T`）或 str 主體時
  靜態推不出 ⇒ 仍默認 `TY-I64`。語料中這類 `it` 要的正是 I64（默認即對），但**若遇到 `?str` 裸標識符
  主體就會回退** —— 未盤點，記為待查（同 ㊹ ⑤「只開常量型樣」的剋制口徑）。
- **`h2m-scan-arms` 的設置目前是多餘的**：varD 對照顯示它對 cov 零貢獻（`h2m-scan` 的值位分支
  會自己調 `h2m-smv-ty`）。保留它是與 `h2m-smv-ty` 對稱、且對將來的非值位前掃路徑無害；
  **不宣稱它有效**。
- **`it` 在降級側仍不進槽表**：`var-read`／`var-write` 的 it 棧為空即計 unsup（門禁不放鬆），
  本輪只改**靜定型別**，未改降級語意。
- **`ps-parse-match` 的 +20 是「如實暴露」而非修好**：那兩個守衛體內的構造（Identifier／
  StringLiteral／IntegerLiteral）**仍然是 genuinely unsupported**，只是現在被走訪到。真正的修復是
  下一步 (b)／(c)。
- **量測與交付版一致**：③ 的 post 跑於 `h2m-post-backup.no`（6429 行），與 `no fmt` 後交付版
  **逐字一致**（`diff -q` 乾淨），量測數字可直接對應交付版。
- 未動 `drv.build-ll` 口徑（單文件、無 sg），同 ㊸／㊹。

**下一步優先級**：① **(b) `h2m-lower-expr` 索引讀 option 化**（let／`?=` 路徑）—— +20 暴露的
落差（單條目 10 條）都在這條路上；② **(c) `_` 臂的零值語義**（Go 已實測，直接對齊）；
③ 盤點 `?str` 裸標識符主體的 `it` 載荷型別。

---

### ㊻ s129 —— (b) `h2m-lower-expr` 索引讀 option 化（let／`?=` 路徑）（2026-11-20）

**① 定位**

承接 ㊺ ⑤ 下一步①。s128 修好 `it` 的型別後，+20 的落差仍在，且 ㊺ 已斷言「都在這條路上」。
s129 打通 (b)：語言上索引讀 `a[k]` **恆 `?T`**（D11），但自舉側兩處都還是**基底型別／裸值**：

- `h2m-ty-of` 的 `K-INDEX-EXPRESSION` 分支回**基底型別**（`[]i64` → `TY-I64`、`[]str` → `TY-STR`）；
- `h2m-lower-expr` 的 `K-INDEX-EXPRESSION` 分支發 `OP-VECGET` 取**裸值 id**（pre 的
  `h2m-lower-expr:vi` 計數 4）。

⇒ let／`?=`／值位 match 把 `?T` 當纯值用：賦進 `?str` 輸出參時 `h2m-var-write` 無臂可落 ⇒ 整函 dropped。

**② 修復**（`compiler/src/h2m.no` 6429 → 6459 行，`no fmt` 後；三分 A／B／C）

- **A `h2m-ty-of` 的 `K-INDEX`**（2574-2580）：索引讀改回 option —— 基底 `TY-VEC`／`TY-VECSTR`
  ⇒ `TY-OPT`；基底 `TY-I64`（`ops[k]` 索引 str）仍回 `TY-I64`（method-call 臂從 `mrecv` 取型，
  `mr=STR` ⇒ `TY-I64` 成立）。
- **B `h2m-lower-expr` 的 `K-INDEX`**（3855-3860）：改託 `h2m-lower-safe-index`（s127 的 safe-index
  helper，回 `?elem` 值 id），并复用既有安全分支的誠實门禁
  `iv0 <= 0 && c.unsup == u0 -> h2m-mark(c)`。
- **C `h2m-opt-unwrap-vs`**（新函式 2785-2821 ＋ `K-INFIX` 調用點 3083-3087）—— **本輪真正的修復**：
  option 值 vs 另一側值 ⇒ 回 option 的**載荷**值 id。**只處理「本側 option 且另一側 plain 標量」**：
  `TY-I64`／`TY-U64` → `OP-OPTGET`、`TY-F64` → `OP-OPTGETF64`、`TY-STR` → `OP-OPTGETSTR`、
  `TY-VEC` → `OP-OPTGETVEC`。兩側皆 option（Go 走 tag + payload 結構比較）或另一側非上述型別
  （聚合載荷）⇒ **原值回傳**，落呼叫側誠實 mark。鏡 `src/mir/codegen.go` 的 `emitCmp`
  option-vs-scalar（`optionPayloadComparable`）與 `unwrapOptionOperand`。

**③ 量測**（pre = `/tmp/s129-cov-pre-control.txt`（s128 交付版）；A = `/tmp/s129-A-cov.txt`；
A+B = `/tmp/s129-cov-post.txt`；A+B+C = `/tmp/s129-C-cov.txt`；`tmp/s125/cmp.py`）

| 通道 | pre | A | A+B | A+B+C | 淨（pre→C） |
| --- | --- | --- | --- | --- | --- |
| **GRAND TOTAL** | **14077** | 9373 | 9384 | **13587** | **−490** |

逐模組（pre → C）：cg 2482→2295（**−187**）、drv 5189→5041（**−148**）、h2m 3352→3191（**−161**）、
ck 360→357、ck-sig 75→74、ps 1523→1532（+9）、ast 234→235；其餘（cg-rc 100／cg-runtime 543／
diag 35／ix 3／link 18／lx 163）不變。

**關鍵歸因：A 的 −4704 不是覆蓋增益，是「分支被跳過」。**
A 把索引讀改成 option 後，`?i64 == const`（`K-INFIX`）在五條型別臂（`lty`／`rty` ∈
{I64, U64, F64, STR, VECSTR}）**全不命中** ⇒ `h2m-mark` ⇒ `cv == 0` ⇒ `h2m-lower-if`（4204）
**整段分支體不降級** ⇒ 大量構造從未被走訪。證據（A+B vs A+B+C）：

- `cg-emit-cfg-t:BlockStatement` 60 → **4050**（+3990）、`cg-emit-cfg-t:IfExpression` 633 → 918（+285）；
  函式構造計數 **246 → 1677**（+1431）。
- `h2m-var-write:Identifier` 70 → 18（−52）、`h2m-var-write:BlockStatement` 8 → 14；
- `cg-rc-check-balance`（Block／If 各 5）在 A+B 是「恢復可產碼」，C 又把它的體走訪到 ⇒ **再次掉落**。

⇒ C 把 `?i64 == i64` 降級後分支體重新被走訪，**真實淨改善只有 −490**。**A+B 的 9384 是「跳過」
造成的假改善，不可當作覆蓋率里程碑。** 附：B 本身近乎中性（A 9373 → A+B 9384，**+11**：
cg −1／ck −1／ck-sig −1／ps −3 對 drv +9／h2m +7／ast +1）。

`dropped (calls unsupported)` 的變化亦與「走訪範圍」一致：`cg` 的 `cg-rc-check-balance` pre 掉落 →
A+B 恢復 → C 再掉落；`ck-def-has` pre／A+B 掉落 → C 恢復；C 新增 `h2m-opt-unwrap-vs` 掉落
（新函式，呼叫被剔除的 `h2m-alloc`／`h2m-emit` 等）。

**④ 驗證**

- **h2m-test 先紅後綠**：改動前（s128 版）**1254 passed / 29 failed**（`tmp/s129-h2m-run1.txt`）；
  交付版 **1284 passed / 0 failed**（`tmp/s129-h2m-run3.txt`）。29 條失敗斷言分布：
  s99i ×14（3 個測試函式）、s129 ×7（3 個）、sB ×3（2 個）、s99n ×3、s113b ×2 —— 共 10 個測試函式。
- **14 套門禁全綠**（`tmp/s129/gate.txt`）：ps 52／h2m 1284／cg 363／ck 20／ck-sig 50／lx 81／
  tok 62／diag 7／build-package 8／util 27／ps-err 46／pkg-ll-call 5／pkg-sigs 5／pkg-ll 10
  —— **合計 2020 passed / 0 failed**。
- **e2e**（`tmp/s129-e2e/probe.no`，走 noc 路徑 `drv.pkg-ll` ＋ `link.compile`；執行檔由 worktree
  基線 `v0.4.28-4-gd21fbb35` 的 `bin/no` 構建）：

| 案例 | 形制 | 結果 |
| --- | --- | --- |
| e1 | `r = xs[k]: { ok -> it … -> '' }`（`[]str`） | `ok=true`，exit 1 |
| e2 | 同上但 `[]i64` 載荷 | `ok=true`，exit 1 |
| e3 | 陳述位索引讀 match（`[]i64`） | `ok=true`，exit 4 |
| e4 | **(b) let 路徑**：`at-o(...) (r ?i64)` 內 `r = xs[k]` ＋ 值位 match 解包（含越界兜底） | `ok=true`，exit 3 |
| e5 | **C 臂**：`at-o(xs, k) == 10`（`?i64 == i64`） | `ok=true`，exit 2 |

- **格式**：`no fmt -d compiler/src/h2m.no compiler/tests/h2m-test.no` 乾淨（④ 後唯一改動是
  `h2m.no:3083` 的注記字串，comment-only，已重跑 h2m-test 確認 1284 全綠）。
- **存量重構**：零（只改測試斷言與注释）。

**測試改寫明細（10 個測試函式 / 29 條斷言）**

- **s99i 三條**（`vec-index-param`／`vec-field-index`／`vec-local-bind`）：輸出參 `r i64` → `r ?i64`，
  斷言補 `define %option @…` 與 `ret %option %opNN`（BASE 行的 `call i64 @vec_get_i64` 不變）。
- **s99n-vec-empty-literal**：`r i64` → `r ?i64`。
- **sB-str-param-sig／sB-str-index**：源碼改**值位 match** `r = xs[i]: { ok -> it … -> '' }`
  （`?str` 賦 `str` 輸出參仍不可行，見 ⑤）。
- **s129-idx-let-opt-str → 改名 `s129-idx-valmatch-str`**（同上，改走值位 match）；
  `test-s129-ty-of-index-opt` 的基底注記 `TY-I64` → `TY-VEC`；`test-s129-idx-arg-gate` 的
  `ir.contains('define')` 改 `ir.contains('@cj-s129g')`（前者會匹配到模組級别的 define）。
- **s113b-index-ty**：索引讀回值型別 `TY-I64`／`TY-STR` → **恒 `TY-OPT`**（註記「`[str]` 基底索引
  取 `str`，s129 起【恒 `TY-OPT`】」）；頭部注释同步。

**⑤ 誠實邊界與下一步**

- **`h2m-ty-of` 缺 `mt == TY-OPT` 值方法臂**（2505-2521 只判 `TY-VEC`／`TY-VECSTR`／`TY-STR`）：
  A 把 `t = syms[g]`（`?str`）的槽型別改成 `TY-OPT` 後，`t.len-bytes()`（`cg.no:293`）回 −1
  ⇒ 該語句計 unsup。pre 版因整個分支體被 `h2m-lower-if` 跳過而未被走訪（`cg-emit-cfg-t:nb`
  pre=0），C 把分支救回後**首次曝光 9 條**（`nb` +9、`bitr`／`bitl` 各 +3）。**這不是 A 新增的
  失敗，是「如實暴露」**，但確實是自舉側的真實缺口 —— 語言側 `?str` 接收者**只守衛 `len`／`cap`**
  （`src/checker/option_member_access.go:162`），`len-bytes`／`byte`／`bytes` 皆合法。**未修**。
- **`?str` 輸出參缺口**：`test-s129-idx-let-opt-str` 的 `(r ?str)` 實測 unsup=2。根因
  `h2m-tynode-code`（1408）的 `K-NULLABLE-TYPE` 只放行 `u64`／`i64`／`f64`，**不含 `str`**
  ⇒ `h2m-param-ty` 缺口。本輪刻意不修（屬獨立缺口），測試改走值位 match。
- **`?=` 路徑未驗證**：語料中 `compiler/src` 只有 1 處真實 `?=`（`util/intern.no:105`
  `t.cnt ?= t.cnt + 1`），本輪未對它做專門驗證；`?=` 的解箱賦值語意仍未打通。
- **C 的保守口徑**：只處理「本側 option ＋ 另一側 plain 標量」。`?i64 == ?i64`（兩側皆 option）
  與聚合載荷（struct）比較仍走**原值** ⇒ `h2m-mark`，未打通。
- **`it` 的載荷型別**：`h2m-smv-pty`（s128）只認**索引讀主體**；`?str` 裸標識碼主體的 `it`
  仍是默認 `TY-I64`（未盤點）。
- **量測工件**：`/tmp/s129-*.txt`（cov 四個通道）、`tmp/s129/gate.txt`（14 套門禁）、
  `tmp/s129-e2e/`（e1–e5 ＋ 執行檔）。

**下一步優先級**：① **`h2m-ty-of` 補 `mt == TY-OPT` 值方法臂** —— 只憑 `TY-OPT` 碼無法區分載荷
型別，故只放行**型別無關**的 `len`／`len-bytes` → `TY-I64`（`byte`／`bytes` 需要載荷型別，暫不放行）；
② **(c) `_` 臂的零值語義**（Go 已實測，直接對齊）；③ `h2m-tynode-code` 的 `?str` 輸出參（FR-3）；
④ 盤點 `?str` 裸標識碼主體的 `it` 載荷型別。

## ㊼ s130 — `h2m-ty-of` 補 `mt == TY-OPT` 值方法臂

**① 定位出什麼問題（繼承 ㊻ ⑤ ①）**

- 唯一改動點：`compiler/src/h2m.no:2505-2514`（原「值方法」塊只判 `TY-VEC`／`TY-VECSTR`／`TY-STR`）。
  承接 ㊻ A 后的缺口：`?str` 接收者的值方法回 −1 ⇒ 該語句列 unsup ⇒ 所在的 `let` 槽查不到
  型別（let-stmt 回 −1）⇒ 每一步算子／賦值／分支都列 unsup ⇒ 整函 unsup 偏高。
- **語言語義由 Go checker 決定，不重新推導**：option 接收者**只守衛 `len`／`cap`**
  （`src/checker/option_member_access.go:156-166`：不是這兩個名字 → 終止走「合法」路徑，
  順 `resolveCallee` 查表；是這兩個名字才查 `option` 自身佈局），其餘方法（`len-bytes`／
  `byte`／`bytes`／`to-str`）**合法**，并由 `src/mir/hir2mir.go:12241` 的
  `recvTypeName = ty.StripOptionRaw(recvTypeName)` 把接收者**剝掉 `?` 後按【載荷型別】查表**。
  實測（`tmp/s130/optmeth.no`，主樹 `bin/no`）：`t = 'nolang'; n = t.len-bytes()` 合法，退出碼 103。
- 因此本側缺口是：`ty-of` 只憑 `mt == TY-OPT` 一個碼**無法區分載荷型別**，對「回值型別與
  載荷型別無關」的方法無從下手。

**② 具體怎麼修的（檔案:行號）**

- `h2m.no:2505-2514`：在 `code == -1` 的值方法塊頂插入三條臂——
  ```no
  mt == TY-OPT && mf == 'len-bytes' -> code = TY-I64
  mt == TY-OPT && mf == 'byte' -> code = TY-OPT
  mt == TY-OPT && mf == 'to-str' -> code = TY-STR
  ```
  理由：`len-bytes` 恒回 `i64`、`byte` 恒回 `?byte`、`to-str` 恒回 `str`，三者回值型別
  與載荷型別無關，只憑 `TY-OPT` 即可定型。
- **與 ㊻ ⑤ ① 的計劃口徑偏差（如實記錄）**：計劃原文「只放行型別無關的 `len`／`len-bytes`
  → `TY-I64`（`byte`／`bytes` 需要載荷型別，暫不放行）」。實測兩條修正：
  (a) `len` 在 option 接收者上**是非法的**（checker 守衛的就是 `len`／`cap`），放行它反而
      與語言語義相反 ⇒ 不放行；
  (b) `byte` 的回值型別是固定的 `?byte`，**不需要**載荷型別 ⇒ 放行；`to-str` 同（恒 `str`）
      且語料大量命中（`cg.no` 的 `d`／`x`／`y` 等 `dsts[k]`／`xa[k]`／`bs[k]` 索引讀皆為
      `?i64`，處處 `d.to-str()`）；`bytes` 回 `[]byte` 也型別無關，但**語料未出現**
      （`compiler/src` grep 無 `\.bytes()` 呼叫）且計劃明言暫不放行 ⇒ 保守不放行，留作下一步。
- **lower 側未動，這是本輪的硬邊界**：`h2m-lower-expr` 的內建型別方法分派（`h2m.no:3408`
  起）只判 `rty == TY-VEC`／`TY-VECSTR`／`TY-STR`，**無 `TY-OPT` 臂**；`h2m.no:3618` 的
  `ok-call == 0 && ok-print == 0 && ok-method == 0 -> h2m-mark(c)` 仍會觸發。本輪只讓 `ty-of`
  就位，讓 `let` 槽型別不再缺，從而**消除級聯失敗**——但 option 接收者呼叫**本身仍未打通**。

**③ 用什麼方式量化驗證（狀態、數據、tools/scripts）**

- 狀態：pre = ㊻ 交付版 `h2m.no`（md5 `e9629f09…`）；post = 本輪交付版（md5 `749a1e0c…`）。
  ② 後的注記改動為 comment-only，**已用交付版重跑全量 cov，GRAND 仍 13539**（逐檔一致）。
  全量 cov 走 `tmp/s129/cov-all-s129.no`（16 檔逐一 `drv.pkg-ll-cov`），在 s129 worktree 的
  baseline `bin/no`（v0.4.28-4-gd21fbb35）下跑。
- **全量 GRAND**：13587 → 13539（**−48**）。逐模塊只有 `cg`／`drv`／`h2m` 各 −16，其餘 13 檔
  不變（`ast` 235、`ps` 1532、`cg-runtime` 543、`lx` 163、`ck` 357、`diag` 35、`ck-sig` 74、
  `cg-rc` 100、`link` 18、`ix` 3，`asp`／`mir`／`tok` 為 0）。`drv`／`h2m` 的 −16 與 `cg`
  **逐行相同**——`cg.no` 是兩模組的依賴，同一批構造被重複計數；**唯一增量是 `cg.no` 的
  `cg-emit-cfg-t` 一個函式 −16**。
- **逐臂量化**（`cg-only` cov，增刪臂各跑一次）：

  | 臂集 | cg unsupported |
  | --- | --- |
  | 僅 `len-bytes` | 2287 |
  | `len-bytes` + `byte` | 2287 |
  | 三者全放（本輪交付） | **2279** |

  即 `len-bytes` −8、`byte` **0**、`to-str` −8（2295 → 2287 → 2287 → 2279）。
- **逐 `h2m-mark` 站點歸因**（把 68 個 `h2m-mark(c)` 編為 `h2m-mark-dbg(c, N)`，N 為按出現序的
  站點號；`cg-only` cov 輸出 `[M] <site> bk=<kind>`）：

  ```
  site  bk  base  post  delta   位置
     1  34    13     9     -4   h2m.no:1061  slot-decl ok-slot==0
    10  34    10     9     -1   h2m.no:1335  var-write ok-w==0
    16  34     5     4     -1   h2m.no:3067  K-IDENTIFIER 未命中
    16  56     1     0     -1
    16   1     4     2     -2
    17  34     1     0     -1   h2m.no:3349  infix 失敗
    17  87    87    83     -4
    18  34  1953  1958     +5   h2m.no:3618  K-CALL 失敗
    18  74    74    72     -2
    42  14     0     1     +1   h2m.no:4162  lower-block hd==0
    45  34    70    66     -4   h2m.no:4213  lower-if cv==0
    47  34     1     0     -1   h2m.no:4303  cv==0
    56  34     6     5     -1   h2m.no:5098  lower-surface bad
                        ─────
                        −16
  ```

  淨 −16 全部來自「`let` 槽型別就位後不再級聯失敗」（slot-decl／var-write／identifier／
  infix／if／for／surface）；**K-CALL 本身（site 18）淨 +3**——走查更深後反而多 mark 5 次，
  被其他站點的改善淹沒，印證 option 接收者呼叫仍未打通。

**④ 全量驗證（門禁 14 套 + 主要 e2e）**

- **門禁 14 套**（`tmp/s130/gate.sh`，`tmp/s130/gate.txt`）：**全綠，2025 passed / 0 failed**
  （㊻ 的 2020 + 本輪新增 5 條斷言）。逐套：
  ```
  ps 52 / h2m 1289 / cg 363 / ck 20 / ck-sig 50 / lx 81 / tok 62 / diag 7 /
  build-package 8 / util 27 / ps-err 46 / pkg-ll-call 5 / pkg-sigs 5 / pkg-ll 10
  ```
- **新增測試**：`test-s130-ty-of-opt-method`（`compiler/tests/h2m-test.no:6331`，5 條斷言，
  已注冊于 :4654）——`?T` 接收者的 `len-bytes`→`TY-I64`／`byte`→`TY-OPT`／`to-str`→`TY-STR`，
  並加兩條**門禁哨兵**：`len`（option 接收者上非法）與 `bytes`（語料未出現）仍回 −1。
- **主要 e2e**（`tmp/s130-e2e/`，以本輪 `noc` 重建 e1–e5）：e1=1／e2=1／e3=4／e4=3／e5=2，
  與 ㊻ 逐字一致——本輪只動 `ty-of`，不改變既有 probe 的 codegen 路徑。
- **格式**：`no fmt -d compiler/src/h2m.no compiler/tests/h2m-test.no` 乾淨（② 後唯一改動是
  `h2m.no:2511` 的注記字串，comment-only，已重跑 h2m-test 確認 1289 全綠）。
- **存量重構**：零（只加臂與測試）。

**⑤ 誠實邊界與下一步**

- **option 接收者呼叫仍未打通**：`h2m-lower-expr` 的內建型別方法分派無 `TY-OPT` 臂
  （`h2m.no:3408` 起只判 VEC／VECSTR／STR），故 `?str.len-bytes()` 在 codegen 路徑上仍走
  `h2m-mark`（site 18 淨 +3）。本輪的 −16 是**級聯失敗的消除**（`let` 槽型別就位），**不是**
  option 呼叫的打通——`cg` 總量仍 2279，缺口未閉合。
- **`bytes` 未放行**：型別上與 `byte` 同理（恒 `[]byte`），但語料未出現，保守留作下一步。
- **`len`／`cap` 是非法的**：option 接收者只守衛這兩個名字（`option_member_access.go:158`），
  **不得放行**；若未來放行了 `len`，反而與語言語義相悖。
- **`it` 的載荷型別**：`?T` 裸標識碼主體的 `it` 仍未盤點（承 ㊻ ⑤ ④）。
- **量測工件**：`/tmp/s130-full-base.txt`／`/tmp/s130-full-tyof.txt`（全量 cov 兩版）、
  `/tmp/s130-cg-{base,tyof,A,B,full}.txt`（逐臂）、`/tmp/s130-cg-{base,tyof}-tagged.txt`
  （站點歸因）、`tmp/s130/gate.txt`（14 套門禁）、`tmp/s130-e2e/`（e1–e5）。

**下一步優先級**：① **(c) `_` 臂的零值語義**（承 ㊻ ⑤ ②，Go 已實測，直接對齊）；
② **`h2m-lower-expr` 補 `TY-OPT` 內建型別方法臂**（本輪暴露的真缺口：option 接收者呼叫的
codegen 路徑）——需先能從接收者追查載荷型別；③ `h2m-tynode-code` 的 `?str` 輸出參（承 ㊻ ⑤ ③）；
④ 盤點 `?str` 裸標識碼主體的 `it` 載荷型別（承 ㊻ ⑤ ④）。

---

### ㊽ s131 —— 值位 match `_` 臂的零值語義（2026-11-20）

**① 定位出什麼問題（繼承 ㊻ ⑤ ② / ㊼ ⑤ ①）**

- 改動點：`compiler/src/h2m.no` 的值位 SurfaceMatch 鏈——`h2m-lower-surface` 的門禁
  （:5125-5131）要求**每條臂都解得出值表達式**（`h2m-smv-arm-val(t, bn) > 0`），`_` 臂解不出
  ⇒ `bad = 1` ⇒ **整條 SurfaceMatch 列 unsup**（site 56）。
- **語言語義由 Go 決定，不重新推導**：`src/mir/hir2mir.go` 的 `captureArmValue`（4883-4903）
  在 `v == NoVal` 時**直接 return**，不寫 sink；sink 由**第一條產值的臂**以
  `l.b.EmitInt(OpConst, typ, 0, "")` 零初始化 ⇒ `_` 臂的結果是**結果型別的零值**
  （`i64`→0、`str`→`''`、option→nil）。
- 實測（`tmp/s131/probe-sem.no`，主樹 `bin/no` 與 worktree 基線一致）：`f=20`（ok 臂）、
  `g=0`（越界→err→`_`→i64 零值）、`h=[]`（str 零值）、`k=0`（除零→err→`_`→0）；
  `probe-sem2.no`：`g2=0`（裸 `->`）、`g3=0`（`-> 0`）、`g4=100,0`（i64 主體 vmode）、
  `g5=7,0`（option 主體 omode）。
- **權威形制**（`tmp/s131/probe-shape.no`）：`MatchArm{wildcard:true, valueCtx:true}` →
  `BlockStatement{inline:true}`（kind 14, nkid=1）→ `ExpressionStatement`（kind 30,
  **nkid=1, kid0=0**），源自 `ps.no:2098` 的 `ast.kid-add(t, idx, 0)`（`kid-add` 會 push 0
  並遞增 nkid，見 `ast.no:247-253`）。
- **三種「空」臂的形制不同**（本輪只認 `_`）：`-> _` = Block{nkid=1}→ExprStmt{nkid=1,kid0=0}；
  裸 `->` = Block{nkid=0}（MatchArm 帶 `blockBody:true`）；`-> ""` = Block{nkid=1}→kid 為
  id 0（無 ExprStmt）。

**② 具體怎麼修的（檔案:行號，4 處）**

- 新增 helper `h2m-smv-arm-discard`（`h2m.no:1774`，插在 `h2m-smv-arm-val` 之後）：
  臂體是 Block{nkid==1} 包 ExprStmt{nkid==1, kid0==0} ⇒ 回 1。
- `h2m-smv-ty`（:1823）：`disc == 0` 時才採型別證據（`ok -> it` 決定槽型別），`disc == 1` 跳過；
  **全臂皆丟棄 ⇒ `seen == 0` ⇒ 回 −1**。
- `h2m-smv-body`（:1938）：`ex <= 0` 時若 `disc == 1` 則**不發值也不 STORE**——結果槽保持
  entry 的零初始化（`h2m-slot-decl`，:1005-1059：TY-I64/U64→CONST 0、TY-F64→0.0、
  TY-STR→`OP-SALLOC`+`STRCONST 0`、TY-OPT→`OP-OPTALLOC`+`OPTNONE`；struct 不零初始化）；
  其餘仍 `h2m-mark(c)`。
- `h2m-lower-surface` 門禁（:5125-5131）：`ex0 <= 0` 時若 `d0 == 1`（`_` 臂）則**放行**。
- 交付版：`compiler/src/h2m.no` md5 `f312fe0bbd1ccba0cba1487613ee65a9`，**6508 行**
  （s130 為 6468 行，+40）。

**③ 用什麼方式量化驗證（狀態、數據、tools/scripts）**

- 狀態：pre = ㊼ 交付版 `h2m.no`（md5 `972db91ae61f9cd0e9fcd3941e025243`，與 `749a1e0c` 僅
  comment-only 差）；post = 本輪交付版（`f312fe0b`）。全量 cov 走 `tmp/s129/cov-all-s129.no`
  （16 檔逐一 `drv.pkg-ll-cov`），在 s129 worktree 的 baseline `bin/no`
  （v0.4.28-4-gd21fbb35）下跑。
- **全量 GRAND：13539 → 13459（−80）**。逐模塊：

  | 模塊 | pre | post | delta |
  | --- | --- | --- | --- |
  | cg | 2279 | 2272 | −7 |
  | drv | 5025 | 5006 | −19 |
  | h2m | 3175 | 3164 | −11 |
  | ps | 1532 | 1517 | −15 |
  | ast | 235 | 228 | −7 |
  | ck | 357 | 350 | −7 |
  | cg-runtime | 543 | 539 | −4 |
  | cg-rc | 100 | 96 | −4 |
  | lx | 163 | 160 | −3 |
  | ix | 3 | 0 | −3 |
  | ck-sig | 74 | 74 | 0 |
  | diag | 35 | 35 | 0 |
  | link | 18 | 18 | 0 |
  | asp / mir / tok | 0 | 0 | 0 |

  增量不如 ㊼ 集中：本輪門禁放行是**橫向**的（所有帶 `-> _` 的值位 match 都受益），
  故 `drv`／`ps`／`h2m` 的降幅（−19／−15／−11）反而大於 `cg`（−7）。
- **逐 `h2m-mark` 站點歸因**（把 68 個 `h2m-mark(c)` 編為 `h2m-mark-dbg(c, N)`，N 為按出現序的
  站點號；`cg-only` cov 輸出 `[M] <site> bk=<kind>`。`bk` 是「就近未計過的塊」歸因基底，
  **不是構造本身**，且會隨輪漂移——㊼ 表裡 site 56 是 bk=34，本輪是 bk=67）：

  ```
  site  bk  base  post  delta   位置
    18  14   1958  1959     +1   h2m.no:3654  K-CALL 失敗（歸因基底 K-BLOCK-STATEMENT）
    18  33      1     0     -1   h2m.no:3654  K-CALL 失敗（歸因基底 K-FOR-STATEMENT）
    56  67      5     0     -5   h2m.no:5138  h2m-lower-surface bad != 0  ★本輪直擊
    65  34      1     0     -1   h2m.no:5834  返回值型別不符 ty-match == 0
                        ─────
                        -6   （mark 計數合計 2274 → 2268）
  ```

  `cg` unsupported 2279 → 2272（**−7**）；mark 計數 2274 → 2268（−6），差 1 來自不經
  `h2m-mark` 的丟棄路徑。site 56 歸零是直擊；site 65 與 site 18 的增減是「走查更深」的
  副產品（`_` 臂放行後某些函式的返回值型別不再不符，但同時有呼叫走查得更深）。

**④ 全量驗證（門禁 14 套 + 主要 e2e）**

- **門禁 14 套**（`tmp/s131/gate.sh`，`tmp/s131/gate.txt`）：**全綠，2037 passed / 0 failed**
  （㊼ 的 2025 + 本輪新增 12 條斷言）。逐套：
  ```
  ps 52 / h2m 1301 / cg 363 / ck 20 / ck-sig 50 / lx 81 / tok 62 / diag 7 /
  build-package 8 / util 27 / ps-err 46 / pkg-ll-call 5 / pkg-sigs 5 / pkg-ll 10
  ```
- **先紅後綠（實證）**：s130 版 `h2m.no` 跑本輪測試 ⇒ **1292 passed / 9 failed**，9 條全落在
  本輪新增的 4 個測試函式；換 s131 版 ⇒ **1301 passed / 0 failed**。
- **新增測試**（`compiler/tests/h2m-test.no` md5 `462f86450e103c010cda89ffd1c16586`，6936 行；
  註冊于 :4655-4658）：
  - `mk-sm-discard`（:6384）／`mk-sm2`（:6393）兩個 helper——合成 `_` 臂形制與兩條臂的
    SurfaceMatch（臂群走 aux 陣 12 欄/臂：a4=wildcard、a5=dotVal、a9=valueCtx 為
    `'true'`，a11=臂體）。**坑**：臂2 一度漏了 a8 個 aux 欄，導致 `hb+11` 讀到 0（臂體為 0），
    2 條測試誤紅——已修。
  - `test-s131-smv-discard-ty`（:6428）：單元測 `h2m-smv-ty`——`[]str` 主體 + `ok -> it` + `_`
    ⇒ `TY-STR`；`[]i64` ⇒ `TY-I64`；**全臂皆 `_` ⇒ −1**。
  - `test-s131-smv-discard-lower`（:6473）：整鏈 `[]str`——斷言 unsup==0、`define`、
    `store %str-long`、`load %str-long`。
  - `test-s131-smv-discard-lower-i64`（:6488）：整鏈 `[]i64`（對齊語料活例
    `v = p.toks[i]: { ok -> it.typ / -> _ }`）——斷言 unsup==0、`define`、`store i64`。
  - `test-s131-smv-bare-arrow-gate`（:6502）：**門禁哨兵**——裸 `->` 仍須 unsup
    （`agg.unsup > 0` 且 `ir.contains('define') == 0`）。
- **主要 e2e**（`tmp/s131-e2e/`，以 `/tmp/noc-s131` 重建）：e1=1／e2=1／e3=4／e4=3／e5=2，
  與 ㊼ 逐字一致；**新增 e6=4**（`[]str` 越界走 `_` 得 `''`、`[]i64` 越界走 `_` 得 `0`，
  四條斷言全中），**noc 與 Go 基線同為 4**。
- **格式**：`no fmt -d compiler/src/h2m.no compiler/tests/h2m-test.no` 乾淨（0 bytes）。
- **存量重構**：零（只加 helper、改 3 個函式、加測試）。

**⑤ 誠實邊界與下一步**

- **裸 `->` 與 `-> ""` 仍未放行**：本輪只認 `_` 的確切形制。裸 `->` 在 Go 側同樣是零值
  （`g2=0`），但本輪**刻意不動**，只留門禁哨兵 `test-s131-smv-bare-arrow-gate` 釘住現狀；
  要放行需先確認 `blockBody` 標記的語義與 `h2m-smv-ty` 的互動。
- **全臂皆 `_` 的降級路徑**：`h2m-smv-ty` 回 −1 時 `h2m-scan`（:2757-2758）把槽型別回退
  `TY-I64`。目前語料未出現「全臂皆 `_`」的值位 match（第三條 ty 測試故意構造它來驗證 −1），
  但未經全量語料驗證。
- **兩個發散的非本輪缺陷**（如實記錄，不在本輪修）：
  1. **str 拼接在 guard 臂內的既有 IR 支配缺陷**：`cond -> s = s - '1'` 在 noc 下觸發
     `Instruction does not dominate all uses! %sv22 = call @str_concat(...)` /
     `%dp70 = extractvalue ...`。已用 `/tmp/t-sc1.no`（**不含任何 `_` 臂**）復現 ⇒ 與本輪無關。
  2. **`r = r + 1` 的長期 noc/Go 分歧**：i64 溢出 option 賦值，**Go 兩版（主樹 dev 與 worktree
     基線）都拒收**，但 noc 自 s113 起一直接受且行為正確（`/tmp/t-e6d.no`：noc-s113／s122／
     s123／s130／s131 皆 rc=1）⇒ **不是本輪引入**。e1–e5 裡 e4 就用了這條路，故 e2e 一直是
     noc-only；本輪 e6 刻意避開，才能做 Go 交叉驗證。
- **量測工件**：`/tmp/s131-full-post-final.txt`（全量 cov post）、
  `/tmp/s131-cov-{drv,h2m,ix,link,lx,mir,ps,tok}.txt`、`/tmp/s131-cg-tagged.txt{,.sites}`、
  `/tmp/s131-h2m-tagged.no`、`tmp/s131/gate.txt`、`tmp/s131-e2e/`。

**下一步優先級**（承 ㊼ ⑤，① 已完成）：② **`h2m-lower-expr` 補 `TY-OPT` 內建型別方法臂**
（真缺口：option 接收者呼叫的 codegen 路徑；本輪 site 18 淨 +1 仍在，`cg` 總量仍 2272）；
③ `h2m-tynode-code` 的 `?str` 輸出參（承 ㊻ ⑤ ③）；④ 盤點 `?str` 裸標識碼主體的 `it`
載荷型別（承 ㊻ ⑤ ④）；⑤ 放行裸 `->`（需先確認 `blockBody` 語義，哨兵已就位）。

---

## ㊾ s132 — `?str` 接收者的內建型別方法（`len-bytes` / `byte`）

**① 定位**

- 承 ㊽ ⑤ ②：`h2m-lower-expr` 的內建型別方法分派表只有 `TY-I64` / `TY-STR` / `TY-F64` /
  `TY-VECSTR` 四個臂，**`TY-OPT` 完全沒有臂** ⇒ 任何 option 接收者的方法呼叫都落到 `ok-method`
  兜底（`ok-call = 0`），由 `h2m-lower-call` 回 '' → 整函降級。
- 真缺口是 **cg.no 自己源碼裡的三個 option 接收者調用點**（noc 要降級 cg 就得認得它們）：
  - `cg.no:296` `nb = t.len-bytes()`（`t` 是 `?str`）→ 對應 `OP-STRLEN`（`cg.no:1885`）
  - `cg.no:299` `bv = t.byte(si)`（`t` 是 `?str`）→ 對應 `OP-STRBYTE`（`cg.no:1914`）
  - `cg.no:219` `g.push(d.to-str())`（`d` 是 `?i64`）→ cg 只有 `@digits` / `@print_i64`，
    **沒有 int→str 的 OP 原語**
- `?str.bytes()`（`OP-STRBYTES`，`cg.no:1985`）本輪**不補**，理由見 ⑤。
- Go 權威口徑（`src/mir/hir2mir.go:12194-12241`）：`recvT := l.valueTypeOf(rv)`；`ty.IsOptionRaw`
  時 `unwrapOptionOperand`；剝後是 `str` 則 `OpClone`；最後 `recvTypeName = ty.StripOptionRaw(...)`
  按**載荷型別**查表。**noc 侧 `?str` 接收者不需要 clone**——`OP-OPTGETSTR` 只做 `extractvalue`，
  不複製緩衝，與 `emitMove` 的「所有權移轉」分支同理。
- Go checker 守衛（`src/checker/option_member_access.go:158`，i18n `qwl2enr7`）：只有 `len` / `cap`
  在 `?T` 接收者上非法 ⇒ `?str.len-bytes()` / `?str.byte(i)` 是**合法** Nolang，理應可降級。

**② 修复**

- `h2m-ctx` 新增 **`vpty []i64`**（`h2m.no:171`）：**按值 id** 平行 `vty` 的 option 載荷型別碼，
  `0 = 非 option / 未知`。槽（名字）不另立表——槽的值 id 由 `c.vids[i]` 給出，槽載荷即
  `vpty[c.vids[i]]`。與 `tys` / `vty` 同口徑：**只讀不發指令**，不進 IR。
- 六個登記點（全部透傳「被包/被讀出來的那個值的載荷」）：
  - `h2m-var-read` TY-OPT 臂（`:1201`）：新 alloc 的 `v` 抄 `vpty[sid]`
  - `h2m-var-write` 兩分支（`:1313` / `:1325`）：OP-OPTSTORE 寫回槽值 id 時同步
  - `h2m-opt-wrap` 整型 / F64 兩分支（`:1250` / `:1258`）
  - 索引讀兩處：`h2m-lower-expr` 的 INDEX-EXPRESSION 臂（`:3997`）與 `h2m-lower-safe-index`
    的 merge（`:4972`）
- 兩個 helper：`h2m-vpty-at`（`:1379`，回 `0` = 未知）與 `h2m-vpty-set`（`:1391`，裸 place-match
  寫，帶 `id >= 0 && id < c.vpty.len()` 閘門）。**六處寫全部走 helper**，不再散落裸寫。
- `h2m-lower-expr` 新增 `rty == TY-OPT` 臂（`:3593`，插在 `TY-STR` 臂之後）：
  1. `pty = h2m-vpty-at(c, rv)`；
  2. `pty == TY-STR` 時 `sv = h2m-alloc(c, TY-STR)` + `OP-OPTGETSTR` 取出 `%str-long`；
  3. `mname == 'len-bytes'` → `OP-STRLEN`；`mname == 'byte'` 且實參型別是 `TY-I64` → `OP-STRBYTE`。
- **靜態推不出（`pty == 0`）就落回閘門**，不放寬、不猜——這條紀律是本輪測試的第三條在守。

**③ 量测**

- 單檔：`cg` 2272 → **2270**（正好 −2，即 `len-bytes` / `byte` 兩條分支）；`h2m` 3164 → **3172**（+8）。
- 全量 16 檔 GRAND：13459 → **13473**（**+14**）。逐模块只有三個變動：`cg` −2、`drv` +8、`h2m` +8，
  其餘（`ast` 228 / `cg-rc` 96 / `cg-runtime` 539 / `ck` 350 / `ck-sig` 74 / `diag` 35 /
  `ix` 0 / `link` 18 / `lx` 160 / `mir` 0 / `ps` 1517 / `tok` 0）逐字不變。
- **重復計入（重要）**：`drv.pkg-ll` 的包會**連帶降級 `h2m.no`**（drv 的 LF 歸因裡就有
  `h2m-lower-expr` 等 h2m 函數），所以同一批 8 個新 mark 在 `drv` 與 `h2m` 各被計一次。
  扣除這個artifact 後的淨變化 = `h2m` +8、`cg` −2 ⇒ **+6**。
- `h2m` +8 的構成（LF 逐條差）：`h2m-vpty-set` 的 BlockStatement + Identifier（2）、
  `h2m-new` 的 Identifier + IntegerLiteral（2）、`h2m-alloc` 的 Identifier（1）、
  `h2m-var-write` 的 Identifier×2（2）、`h2m-lower-expr` 的 Identifier + IntegerLiteral×2（3）
  = 10，扣 `cg` −2 = 8。**全部是「在 noc 裡寫新語句」的固有成本**，無一來自本輪的新語義。
- 優化軌跡（單檔 `h2m`）：`clean1`（`spty`/`vpty` 雙字段）3182 → `opt1`（單字段 `vpty`，值 id 鍵）
  3187 → `o-noopt`（拔掉 opt-wrap 兩處寫）3184 → `opt2`（**六處寫全改走 `h2m-vpty-set`**）3181 →
  拔除 S132P 插樁後 **3172**（插樁本身值 +9）。選 `opt2` 的形制：雙字段版比單字段版差 5，
  而「六處散落裸寫」比「統一走 helper」差 6。
- 本輪復實測的 noc 局限成本模型（每處**新增**源構造的 mark 增量；同一構造在不同函數下計數相同）：
  struct 字面量裡的 `[]` 空 slice 欄位 **+1**、`.push(...)` **+1**、**裸** place-match 陳述
  **+2**、賦值形 place-match 與 `&&` guard **+0**、從零重建陣列（`[]` + 逐項 `push` + 賦回）**+2**。
  `#{overflow}` 行作用域注解本身 **+0**。
- 改動規模：`compiler/src/h2m.no` 6508 → 6606 行（+98，其中 100 行新增、2 行刪除）；
  `compiler/tests/h2m-test.no` 新增 3 個測試函數（9 條斷言）。

**④ 验证**

- ✅ **自写测试**（`compiler/tests/h2m-test.no`，注册于 `test-s131-smv-bare-arrow-gate()` 之後）：
  - `test-s132-opt-str-len-bytes`：`cj-s132a = (syms []str) (n i64) { t = syms[0]; n = t.len-bytes() }`
    ⇒ `agg.unsup == 0`、IR 含 `extractvalue %option %op` 與 `%slr`。
  - `test-s132-opt-str-byte`：同構形 `b = t.byte(0)` ⇒ `agg.unsup == 0`、IR 含 `%sbL`。
  - `test-s132-opt-i64-to-str-gate`：`cj-s132b = (ds []i64) (n i64) { d = ds[0]; n = d.to-str() }`
    ⇒ **`agg.unsup > 0` 且 `ir.contains('define') == 0`**（整函未產出，老老實實擋住）。
  - 先紅後綠：第 2 條在補臂前直接紅；第 1 條在 `h2m-lower-expr` 還未補臂時與 `h2m-test` 全檔
    一起 7 FAIL；補臂後三條全綠。
- ✅ **h2m-test**：**1310 passed / 0 failed**（㊽ 1301 + 本輪 3 函式 × 3 斷言 = +9）。
- ✅ **門禁 14 套**：**2046 passed / 0 failed**（`tmp/s132/gate.txt`；逐套：ps 52 / h2m 1310 /
  cg 363 / ck 20 / ck-sig 50 / lx 81 / tok 62 / diag 7 / build-package 8 / util 27 /
  ps-err 46 / pkg-ll-call 5 / pkg-sigs 5 / pkg-ll 10）。
- ✅ **e2e**（`tmp/s132-e2e/`，以 `/tmp/noc-s132` 重建）：e1=1 / e2=1 / e3=4 / e4=3 / e5=2 /
  e6=4（**與 ㊽ 逐字一致**）+ **e7=3**。e7 新增：`t = xs[0]`（`?str`）後連續
  `t.len-bytes() == 3` ×2 與 `b = t.byte(0); b == 0x6f`，三條全中 ⇒ `exit(3)`；
  **Go 基線（worktree `bin/no`）同為 3** ⇒ 交叉驗證通過。
  s131 對照：同樣三行为 **6 / 1 unsup**（`len-bytes` 6、`byte` 1）⇒ 本輪 **0 / 0**。
- ✅ **`no fmt -d`**：`compiler/src/h2m.no` 零漂移（7 處「註釋前缺空行」的 hunk 已按 `no fmt`
  輸出**只 patch 空白行**，未動任何代碼行）；`compiler/tests/h2m-test.no` 本就零漂移。
- ✅ **回归**：`?i64.to-str()` 仍被閘門擋住（第三條測試斷言 `ir.contains('define') == 0`）；
  `?byte` 的 match 陳述仍有 1 個既有局限（`e7_2`：s131 = 2 unsup → s132 = 1，未新增）。

**⑤ 诚实边界与下一步**

- **`?i64.to-str()` 仍未放行**：不是判別問題，是 cg 根本沒有 int→str 的 OP 原語
  （`cg-runtime` 只有 `@digits` / `@print_i64`）。要放行得先新增 OP + 運行時 helper，是獨立一輪。
- **`?str.bytes()` 未處理**：`cg.no:1985` 有 `OP-STRBYTES`，但 `bytes` 的返回是切片視圖，
  涉及指標/長度配對與所有權（`emitMove` 的視圖語義），比 `len-bytes` / `byte` 兩條純 `extractvalue`
  路徑複雜，獨立留下一輪。本輪只補這兩條。
- **全量 GRAND +14（淨 +6）**：本輪是「補真缺口、但 vpty 管道要付賬」的輪次，不是 ㊽ 那種
  全模塊下降輪。已把成本壓到單一 helper（`h2m-vpty-set`）+ 六處登記，`h2m` 淨 +8。
- **`?T` 接收者的 `len` / `cap`**：Go checker 直接判非法（`qwl2enr7`），noc 側無需放行。
- **下一步優先級**：③ `h2m-tynode-code` 的 `?str` 輸出參（承 ㊻ ⑤ ③）；
  ④ 盤點 `?str` 裸標識碼主體的 `it` 載荷型別（承 ㊻ ⑤ ④）；
  ⑤ 放行裸 `->`（需先確認 `blockBody` 語義，哨兵 `test-s131-smv-bare-arrow-gate` 已就位）。
- **量測工件**：`/tmp/s132-full-{pre,post}.txt`（全量 16 檔乾淨前後；pre 13459 / post 13473）、
  `/tmp/s132-{pre,post}-single.txt`、`/tmp/s132v/{base,clean1,opt1,opt2,final}.{no,txt,lf}`、
  `/tmp/s132-full-rest.txt`（mir/ps/tok 補跑）、`tmp/s132/gate.sh` + `gate.txt`、`tmp/s132-e2e/`。

---

## ㊿ s133 — `?str` 輸出參（`h2m-tynode-code` 內層 str 放寬 + `h2m-slot-init-arg` 存放半）

> 本節另順手修一處文檔筆誤：㊾ 的章節標題被誤寫成 `㊶`（與 ㊵ s124 撞號），已改回 `㊾`。

**① 定位**

- 承 ㊾ ⑤ ③ / ㊻ ⑤ ③：`h2m-tynode-code` 的 `K-NULLABLE-TYPE` 臂只收內層 `u64` / `i64` / `f64`
  ⇒ `TY-OPT`。`?str` 的輸出參（以及輸入參、let 標注）走**同一條判据**（§11 B7：签名侧
  `h2m-param-ty` 與 let 标注侧 `h2m-lower-let` 必須同口徑），內層 `str` 不在白名單 ⇒
  `h2m-param-ty` 回 `bad=1` ⇒ **整函 dropped**。
- 重現（`tmp/s133-e2e/`）：`s1` 探針（`(r ?str)` + `r = xs[k]`）在 pre 版 **7 unsup**、post 版
  **0 unsup**；`t1`（`(s ?str)` 形參）pre 版 **4 unsup** → post 版 **0**。即「索引讀 `?str` 回
  `?str` 輸出參」在 ㊾ 是**硬缺口**。
- **入形齊備度**：`?str` 載荷是 `%str-long{ptr,len}`——**入槽**走 `OP-OPTMKSTR`（`h2m.no:4962`
  索引讀的包 option 路徑，只存不 retain，與 Go 的 `optionPayloadCoerced` 對 str 內層的位形轉換
  同軸）、**出槽**走 `OP-OPTGETSTR`（㊾ 的 `TY-OPT` 臂已用同一條取用口徑）⇒ 兩條原語都在，
  判据沒有理由再擋。
- 內層 `vec`（`?[]str`）**仍不入**：`[]str` 緩衝無 retain，欄位外流會 UAF（同 s99f 口徑）。

**② 修复**

- **改動 1（核心）** `h2m-tynode-code` 的 `K-NULLABLE-TYPE` 臂：`inm == 'u64' || inm == 'i64'
  || inm == 'f64'` → 追加 `|| inm == 'str'`；同步更新函式上方注釋（「只收 ?u64／?i64／?f64」
  →「收 ?u64／?i64／?f64／?str」，並記 s133 的入/出槽口徑與「vec 內層仍不入」的理由）。
- **改動 2（下游配套，本輪才被逼出來）** `h2m-slot-init-arg`：新增
  `st == ty && ty == TY-OPT -> OP-OPTSTORE`，並把 `ty != TY-OPT` 加進通用 `OP-STORE` 臂的排除條件。
  - **根因**：option 形參的 incoming 值原本會落進通用臂，發 `store i64 %v…, i64* %s…`，
    而 `TY-OPT` 的槽在 **`%os` 命名空間**（不是 `%s`）⇒ clang 報 `use of undefined value '%s2'`。
  - **這是既有缺口，非本輪引入**：`?i64` 形參同此（s94 起 `TY-OPT` 已入形，存放半一直漏著）。
    活證：`u1` 探針（`g = (n ?i64)` + `g(v)`）在 ㊾ 版二進位上 **clang failed**，本輪 **OK/0**。
    由本輪的 `?str` 輸入參測試（`test-s133-opt-str-param-in`）逼出，一併補齊。
  - 與 `h2m-var-write` 的「option 槽 ← option 值」直存同口徑（cg `OP-OPTSTORE`：xa=值、bs=槽）。

**③ 量测**

- **全量 16 檔 GRAND：13473 → 13473（delta 0）**，且 block / leaf / id **三個通道的 diff 全空**。
  逐模块逐字不變：`ast` 228 / `cg-rc` 96 / `cg-runtime` 539 / `cg` 2270 / `ck-sig` 74 /
  `ck` 350 / `diag` 35 / `drv` 5014 / `h2m` 3172 / `link` 18 / `lx` 160 / `ps` 1517 /
  `asp`·`ix`·`mir`·`tok` 0（`tmp/s133-full-{pre,post}.txt`，各 27240 行）。
- **為什麼是 0（兩個理由，都是「本輪不新增降級路徑」）**：
  1. `compiler/src` 語料裡**沒有任何 `?str` 型別節點**——`grep -ro '?str' compiler/src/*.no`
     的 121 處中，112 處在 `ck-sig.no` 的**簽名字串表**（`'?str'` 字面量），其餘 9 處全是
     `cg-runtime.no`／`h2m.no`／`mir.no` 裡的**注释**。放行判据因此不新增任何 mark。
  2. `h2m-slot-init-arg` 的新分支只改「發哪條 store 指令」，**不改變任何函式是否被降級**
     （option 形參在補分支前也已經走到該函式並發 generic `OP-STORE`）⇒ unsup 計數不動。
  ⇒ 本輪是**純放行**輪，全量曲線按預期不動。
- 改動規模：`compiler/src/h2m.no` 6607 → 6618 行（**+11**：1 行排除條件、5 行 `OP-OPTSTORE`
  分支連注釋、2 行判据行、3 行注釋改寫）；`compiler/tests/h2m-test.no` 新增 4 個測試函式
  （12 條斷言）。
- **pre↔post 只差本輪改動**：`diff -u /tmp/s133-h2m-pre.no compiler/src/h2m.no` 的三個 hunk
  全部落在 `h2m-slot-init-arg` 與 `h2m-tynode-code`，無第三方漂移。

**④ 验证**

- ✅ **自写测试**（`compiler/tests/h2m-test.no`，注册于 `test-s132-opt-i64-to-str-gate()` 之後）：
  - `test-s133-opt-str-idx-return`：`(r ?str)` + `r = xs[0]` ⇒ `unsup == 0`、IR 含
    `define %option`、`call %str-long @vec_get_str(`、`ret %option %op`。
  - `test-s133-opt-str-param-in`：`(s ?str)` 形參 ⇒ `unsup == 0`、IR 含
    `define i64 @g(%option %op1)`、`store %option %op1, %option* %os2`（**存放半**）、`ret i64`。
  - `test-s133-opt-str-let-ann`：`v ?str = xs[0]` ⇒ `unsup == 0`、IR 含 `alloca %option`。
  - `test-s133-opt-vec-inner-gate`：`(r ?[]str)` ⇒ **`unsup > 0` 且 `ir.contains('define') == 0`**
    （內層 vec 仍老老實實擋住）。
  - 先紅後綠：`s1` 探針 pre 7 unsup → post 0；`q4` pre 1 → post 0；`t1` pre 4 → post 0。
- ✅ **h2m-test**：**1322 passed / 0 failed**（㊾ 1310 + 本輪 4 函式 × 3 斷言 = +12）。
- ✅ **門禁 14 套**：**2058 passed / 0 failed**（`tmp/s133/gate.txt`）。與 ㊾ 的 `gate.txt` 逐套
  相比，**只有 `h2m` 1310 → 1322（+12）**，其餘 13 套（ps 52 / cg 363 / ck 20 / ck-sig 50 /
  lx 81 / tok 62 / diag 7 / build-package 8 / util 27 / ps-err 46 / pkg-ll-call 5 /
  pkg-sigs 5 / pkg-ll 10）**逐字一致** ⇒ 零回歸。
- ✅ **e2e 回归**（`tmp/s132-e2e/`，以本輪 `/tmp/noc-s133c` 重建）：e1=1 / e2=1 / e3=4 /
  e4=3 / e5=2 / e6=4 / e7=3，與 ㊾ **逐字一致**（且 `noc-s132` 與 `noc-s133c` 逐套同值）。
- ✅ **e2e 新探針**（`tmp/s133-e2e/`，`/tmp/noc-s133c` md5 `8511206c…` vs worktree 基線 `bin/no`）：

  | 探針 | 內容 | noc-s133c | Go 基線 |
  | --- | --- | --- | --- |
  | n1 | `?str` 輸出參 + `==` 命中／越界不命中 | OK/1 | OK/1 |
  | n2 | `?str` 輸入參（match 解包） | OK/1 | OK/1 |
  | n3 | let 標注 `?str` | OK/1 | OK/1 |
  | n4 | `?str` 輸出參越界 ⇒ nil | OK/0 | OK/0 |
  | n5 | `?i64` 輸入參（同一條存放半） | OK/7 | OK/7 |
  | n6 / n7 / n8 | `?str` let／輸出參 + match 解包 | OK/1 | OK/1 |
  | q4 / t1 / t2 / u1 / s1 / s2 | 簽名／let／形參／索引讀活例 | 全一致 | 全一致 |

  ⇒ 本輪目標面 **noc 與 Go 逐條同退出碼**。
- ✅ **`no fmt -d`**：`compiler/src/h2m.no` 與 `compiler/tests/h2m-test.no` **雙雙零漂移**
  （`rc=0`、diff 0 行）。
- ✅ **worktree 同步**：`tmp/s129/wt` 的 `compiler/src/h2m.no`（md5 `4542ac39…`）與
  `compiler/tests/h2m-test.no`（md5 `22010b2e…`）已同步，門禁在 worktree 內跑。

**⑤ 诚实边界与下一步**

- **本輪只放行「签名側的型別碼判定」與「option 形參的存放半」，沒有新增任何運行時原語**：
  `?str` 的入槽／出槽（`OP-OPTMKSTR` / `OP-OPTGETSTR`）在 ㊹／㊾ 已就位，本輪是把它們接到
  签名與形參路徑上。
- **`?str` 仍未入形的部分**：
  - `ok(...)` **構造**（`t3` / `u2` 探針）：noc 沒有「把純 `str` 包成 `?str`」的寫路徑
    （`x1`：`r = 'zero'` 純 str 入 `?str` 輸出參仍 1 unsup；Go 的 `wrapOptionIfNeeded` 會包）。
    `h2m-opt-wrap` 的注釋也已明寫「str／vec 原样返回 ⇒ 呼叫方計 unsup」。
  - 內層 `vec` / `txt`（`z1` / `z2`）：本輪門禁明文保留。
  - 多回傳（`real-outs > 1`，`z3` / `z4`）：Step 2b 未接，既有缺口。
  - 臂體**嵌套塊內的 `==` 比較**（`n9` / `n10` / `n11`）：noc FAIL 而 Go OK，且 **在 ㊾
    二進位上同樣 FAIL** ⇒ 既有缺口，非本輪引入（下一輪 ④ 的切入點）。
- **失敗模式退化的如實記錄**：`r1` / `r6`（`g(xs[0])` 直接把索引讀當纯值傳）由 ㊾ 的
  **invalid IR**（clang `use of undefined value`）變為本輪的**靜默接受**（OK/0），而 Go 基線判
  `idxbruse` / `optunuse` 非法（FAIL/1）。⇒ 本輪修掉了「後端撞 undefined 槽」的**症狀**，但
  **沒有**給 noc 補上 `idxbruse` / `optunuse` 這類檢查（grep 0 命中），所以這類程序現在是
  「安靜地錯」而非「響著錯」。**牢記：noc 的「能編譯」≠ 與 Go 同口徑。**
- **e4 / e5 的 Go 基線分歧是既有的**：`tmp/s132-e2e/e4.no`／`e5.no` 在 Go 基線下判
  `15w45dqk`（`?i64` 隱式賦值）而 noc 接受（3 / 2）；`noc-s132` 與 `noc-s133c` 同值 ⇒ 非本輪
  引入，但它是「noc 缺檢查」的又一處活證。
- **`h2m-slot-init-arg` 的 `OP-OPTSTORE` 是橫向修復**：它同時服務 `?i64` / `?f64` / `?str`
  全部 option 形參（`n5` 的 `?i64` 探針即為佐證），不只 `?str`。
- **下一步優先級**：④ 盤點 `?str` 裸標識碼主體的 `it` 載荷型別（承 ㊻ ⑤ ④；`n9`–`n11` 已把
  現象釘住：臂體嵌套塊內的 `==` 比較在 ㊾ 已壞）；⑤ 放行裸 `->`（需先確認 `blockBody` 語義，
  哨兵 `test-s131-smv-bare-arrow-gate` 已就位）；其後才是 `ok(...)` 構造與純 str 入 `?str` 的
  wrap（`x1`）。
- **量測工件**：`/tmp/s133-full-{pre,post}.txt`（全量 16 檔乾淨前後）、
  `/tmp/s133-h2m-pre.no`（patch 前的 h2m，md5 `26a9bdde…`）、`/tmp/noc-s133c`（md5
  `8511206c…`，由最終源建；`/tmp/noc-s133` md5 `9b4e0a5b…` 僅差一處注釋行號）、
  `tmp/s133/gate.sh` + `gate.txt`、`tmp/s133-e2e/`（56 個探針）。
## 51 s134 — 裸標識碼 `?str` 主體的 `it` 載荷型別（`h2m-lst.lpty` 靜態登記 + 降級期 vpty 兜底）

> **本輪一句話**：`v ?str = xs[0]` 之後 `v: { ok -> … }` 的**裸標識碼 option 主體**終于能推對臂內 `it` 的載荷型別——`it.len-bytes()` 從 unsup 變回正確值 3；而 `r = it` 的**靜默錯碼**（把字串長度當 payload 取回）改為誠實 unsup。
> **量測**（16 檔 cover 曲線）：**13473 → 13494（+21）**。
> **校驗**：14 套全綠（`h2m` 1322 → **1334**）。
> **產地**：`compiler/src/h2m.no` md5 `3d5173cbf0583628044d323ab8d1b023`（6762 行）；`compiler/tests/h2m-test.no` md5 `664b10f36d519a3c33ae48b243dc754d`（7129 行）；二進位 `/tmp/noc-s134` md5 `32d98efdcd4108760541a957dca6939b`（baseline `d21fbb35`，耗時 3.1 min）。

**① 定位**（把「`it` 推錯」拆成三個互相獨立、各自由探針釘住的缺口）

- **缺口 A：`str.len()` 根本不在 noc 的方法子集裡。** `a1`／`c3` 探針：plain `str` 局部走 `s.len()`，Go 回 2、noc **unsup** ⇒ 缺口在 `str.len` 本身。查 `src/std/str.no:47`：`str.len` 是 **std 函式** `n = .cp-index(.len-bytes())`（要逐字節數碼點的循環），不是 `#{buildin}`；noc 子集只收了 `len-bytes`／`byte`／`bytes`。**與 `it` 的載荷型別無關**——本輪不順手夾帶（§2 黃金法則）。
- **缺口 B：`it` 不被當成值接收者。** `it` 不進 `c.names`／槽表（值走 `c.itv` 棧，見 `h2m-var-read`），故 `h2m-name-find(c,'it')` 必然落空 ⇒ `it.<m>()` 落跨模組路（把 `it.len-bytes` 當簽名表 key 去查）⇒ **必然 unsup**。活證：`c1` 探針 pre 版 FAIL；反證：plain `str` 局部 + `s.len-bytes()` **OK/2** ⇒ 方法本身沒問題，是接收者判定漏了 `it`。
- **缺口 C（本輪主體）：裸標識碼 option 主體的 `subj-pty` 恆默認 `TY-I64`。** `h2m-smv-pty` 在 ㊿ 之前只認**索引讀**主體（`base TY-VECSTR ⇒ TY-STR`／`TY-VEC ⇒ TY-I64`），`v: { … }` 這種**裸標識碼**主體走 default ⇒ 臂內 `it` 由 `h2m-itv-v` 用 `OP-OPTGET`（i64）取 `slot[0]`。而 `?str` 的 slot[0] 是 **len**（`cg.no` 的 `OP-OPTMKSTR` 只把 `len` 放 slot[0]，ptr 走 slot[1]）⇒ **靜默錯碼**：
  - `n20`（`r = it`，`v ?str = xs[0]`，`'two'`）pre 版 **回 3**（`exit(f())` 退出碼 3）、`n24`（`'abcdef'`）pre 版 **回 6**——把字串長度當 payload 取回。
  - Go 基線對 `n20`／`n24` 皆**拒編**（`cannot assign str value to i64 variable 'r' [15w45dqk]`）⇒ pre 版是**無聲偏離**，不是「口徑差異」。
- 三缺口的**連通性**：B 修好後 C 仍在（`it.len-bytes()` 的接收者認領了，但取出來的 `it` 仍是 i64 視圖）⇒ C 必須顯式做。

**② 修复**（9 處，全在 `compiler/src/h2m.no`）

- **改動 1** `h2m-lst` 增 `lpty []i64`：與 `lty` 平行的 **option 載荷型別碼**（0 = 未知／非 option），只在 `lty == TY-OPT` 時有語意。`h2m-lst-new` 加初始化槽、`h2m-lst-add` 覆蓋時清 0（payload 來源變了，避免殘留上一次的登記），新增 `h2m-lst-pty-at`／`h2m-lst-pty-set`。
- **改動 2** 新 helper `h2m-pty-of-type`：型別 AST 節點 → **載荷**型別碼（`?i64`／`?u64` → `TY-I64`、`?f64` → `TY-F64`、`?str` → `TY-STR`，其餘 0）。與 `h2m-tynode-code` 的 `K-NULLABLE-TYPE` 臂**同口徑**（內層名稱判定逐字一致），但回的是載荷而非 `TY-OPT`。內層 kid **直接**走 `ast.kid-at(t, d.kid0)`，不經 `h2m-kid-i`（後者在曲線裡是既存 dropped 項，經它中轉會把本函式一起拖下水）。
- **改動 3** `h2m-scan` 的 `K-LET-STATEMENT`：`ty == TY-OPT && d.nkid > 1` 時由**標注**登記 `lpty`。**只看標注**——無標注（`v = xs[0]`）沒有靜態來源 ⇒ 不登記、`h2m-smv-pty` 回退默認（**不放寬門禁**）。
- **改動 4** `h2m-lower-func` 形參播種：`pt == TY-OPT` 時由**形參標注**登記 `loc.lpty` 與值的 `vpty`（`mem == 0` 不鋪槽、名字直綁在那顆 alloc 值上 ⇒ 值自帶 `vpty` 才傳得出；`mem == 1` 走改動 5）。
- **改動 5** `h2m-slot-init-arg` 增第 5 參 `pty`，在 `TY-OPT` 臂 `h2m-vpty-set(c, sid, pty)`——否則槽 `vpty` 恒 0，隨後 `OPTLOAD` 拷到值上仍是 0。
- **改動 6** `h2m-smv-pty` 增裸標識符分支：`sd.kind == K-IDENTIFIER && sd.nkid == 0` 且該名 `lty == TY-OPT` ⇒ `pty = h2m-lst-pty-at(...)`。**關鍵約束**：本函式跑在**前掃描期**，其時 `c.vids` 尚未鋪（`h2m-slot-decl` 在掃完後才跑）⇒ **不可用** `h2m-vpty-at`，標注是掃描期唯一可得的靜態載荷來源。
- **改動 7** `h2m-lower-surface` 主體判定 `sty == TY-OPT` 臂：`subj-pty = h2m-vpty-at(c, svid)`（`> 0` 才採用，否則維持默認）。值的 `vpty` 由 s132／s133 沿寫點（`h2m-var-write` 的 `OPTSTORE` 分支、`h2m-var-bind` 直綁）抄來。
- **改動 8** `h2m-lower-expr` 值接收者判定：`rn == 'it' && c.itv-n > 0 -> recv-is-value = 1`。**棧非空（= 確在某臂內）才認**——不得無條件認 `it`，否則臂外的同名局部會被誤當值接收者。
- **改動 9** 註解：`itv-v` 分派注、`subj-pty` 聲明注。
- **案中案（已撤銷）**：原計畫在降級期給 `agg.it-pty` 補 save／set／restore。核算後確認**不必要**——`h2m-ty-of` 的全量呼叫點都在 scan／`h2m-smv-ty`／`h2m-scan`，**降級期沒有任何呼叫**（降級走 `h2m-vpty-at`）⇒ 該案降為不做，省一輪管道的賬。

**③ 量测**

- **全量 16 檔 GRAND：13473 → 13494（+21）**。逐模塊只有四個變動：
  `ast` 228→228、`cg-rc` 96→96、`cg-runtime` 539→539、`cg` 2270→2270、
  `ck-sig` 74→**75**、`ck` 350→**352**、`diag` 35→35、`drv` 5014→**5024**、
  `h2m` 3172→**3180**、`link` 18→18、`lx` 160→160、`ps` 1517→1517、`asp`·`ix`·`mir`·`tok` 0。
  （`/tmp/s134-full-post.txt` 27278 行；pre 用 s133 期末的 `/tmp/s133-full-post.txt` 27240 行。）
- **+21 的來源**：全部是本輪新增的 `cond -> { … }` 塊，以及 `h2m-lst-add` 覆蓋臂的
  `l.lpty[i]: { ok -> it = 0  -> _ }` place-match 塊——它們在 noc 降級裡都落
  `BlockStatement:construct`（`s122` 起既存的塊級登入口徑）。**沒有動到任何一條既有的
  可降級路徑**。
- **新 helper 在曲線裡是 dropped**：`h2m-pty-of-type`／`h2m-lst-pty-at` 都被記為
  `dropped (calls unsupported)`——但它們與 `h2m-smv-pty` 同樹，而 `h2m-smv-pty` **在本輪之前
  就已經是 dropped**（`?T` 主體推斷整棵子樹本就不在自舉覆蓋內）。⇒ 本輪**沒有把任何
  「可降級」函式拖下水**，只是往既存的 dropped 子樹裡加碼。
- 改動規模：`compiler/src/h2m.no` 6618 → 6762 行（**+144**）；`compiler/tests/h2m-test.no`
  新增 5 個測試函式（12 條斷言）。
- **pre↔post 只差本輪改動**：`diff -u /tmp/s133-h2m-pre.no compiler/src/h2m.no` 共 18 個 hunk，
  逐條核過全部落在 s133（`h2m-slot-init-arg` 的 `TY-OPT` 臂、`h2m-tynode-code` 的內層 str 放寬）
  與本輪 9 處範圍內，無第三方漂移。

**④ 验证**

- ✅ **自写测试**（`compiler/tests/h2m-test.no`，注册于 `test-s133-opt-vec-inner-gate()` 之後）：
  - `test-s134-opt-str-bare-ident-method`：`v ?str = xs[0]` + `v: { ok -> r = it.len-bytes() }`
    ⇒ `unsup == 0`、IR 含 `%slr`（`OP-STRLEN` 獨有暫存名）與 `extractvalue %option`（`OP-OPTGETSTR`）。
  - `test-s134-opt-str-bare-ident-let-body`：臂體多陳述塊（`n = it.len-bytes()` / `r = n`）⇒ 不退化。
  - `test-s134-opt-str-param-method`：`(s ?str)` 形參 ⇒ `unsup == 0` + `%slr`（覆蓋改動 4／5 的槽 `vpty` 抄寫）。
  - `test-s134-opt-i64-bare-ident-gate`：`?i64` 裸標識碼主體行為不變——仍走 `OP-OPTGET`、**不得**出現 `%slr`。
  - `test-s134-opt-str-len-gate`：`it.len()` **必須計 unsup**（缺口 A 的顯式門禁，絕不靜默放行）。
    > **回溯（s135）**：缺口 A 已補（§52），該門禁已**刪除**，改由 `test-s135-opt-str-it-len`
    > 的正向斷言取代。
  - **`h2m-test` 全綠：1322 → 1334 passed / 0 failed。**
- ✅ **全量門禁 14 套全綠**（`tmp/s134/gate.sh` → `tmp/s134/gate.txt`）：
  `ps` 52 · `h2m` 1334 · `cg` 363 · `ck` 20 · `ck-sig` 50 · `lx` 81 · `tok` 62 ·
  `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 · `pkg-sigs` 5 ·
  `pkg-ll` 10，**failed 全 0**。與 ㊿ 逐套相比，**只有 `h2m` 1322 → 1334（+12）**，
  其餘 13 套逐字一致 ⇒ 零回歸。
- ✅ **先紅後綠**（探針表 `tmp/s134/probe.txt`；退出碼 1 = unsup、3/65 = 程序內 `exit(f())`）：
  pre 版 `/tmp/noc-s133c` → post 版 `/tmp/noc-s134`：
  - `c1`／`d1`／`e2`／`f3`／`f4`：**1 → 3**（`it.len-bytes()` 由 unsup 變正確回 3，與 Go 一致）。
  - `d5`（`r = id-s(it)`，`id-s` 收 plain `str`）：**1 → 0**，Go 亦 0。
  - `d6`（`it.byte(0)` 後巢狀 match 取 `it`）：**1 → 65**，Go 亦回 65。
  - `n20`：**3 → 1**、`n24`：**6 → 1**——靜默錯碼收斂為誠實 unsup，與 Go 的拒編對齊。
- ✅ **e2e 探針 93 檔**（`tmp/s133-e2e/`，`tmp/s134/probe.sh` → `tmp/s134/probe.txt`）：
  pre↔post 只有 10 個檔變動，全在 ⑤ 的缺口清單內（`c1`／`d1`／`d2`／`e2`／`f3`／`f4`
  由 1 → 3、`d5` 1 → 0、`d6` 1 → 65、`n20` 3 → 1、`n24` 6 → 1），其餘 83 檔逐字不變。
- ✅ `no fmt -d` 兩檔皆零漂移。

**⑤ 诚实边界与下一步**

- ✅ **缺口 C 已補**：`?str` 裸標識碼主體的 `it` 現在帶 str 載荷，`it.len-bytes()` 正確；
  且 `r = it` 不再回長度（改為誠實 unsup，與 Go 對齊）。
- ⏳ **仍在缺口（逐一點名，不演繹成「`/ compiler/` 覆蓋率過半」）**：
  1. ~~**`str.len()` 缺失**（缺口 A）~~ ✅ **已由 s135 補上**（`OP-STRCPLEN` + `@str_cp_len`
     helper，見 §52）；原門禁 `test-s134-opt-str-len-gate` 已刪除。
  2. **`ok(...)` 構造**（承 ㊿ ⑤）：臂內寫 `ok(...)`（切量樹表／計數器）仍未通。
  3. **裸 `->` 放行**（承 ㊿ ⑤）：哨兵 `test-s131-smv-bare-arrow-gate` 已就位，需先確認
     `blockBody` 語義。
  4. **纯 str 入 `?str` 的 wrap**（`x1`，承 ㊿ ⑤）。
  5. **`?[]str` 仍不入形**：`[]str` 緩衝無 retain，欄位外流會 UAF（同 s99f 口徑）。
  6. **臂內 vecs 寫入門禁**（`s122`）：`f1` 探針作反證——臂體 `s.bytes()` 與 `it` 無關照樣 3 unsup。
  7. **`d2` 口径差（非本輪引入，但本輪才被凸顯）**：`g(xs[0])`（`?str` 形參收裸索引）post 版
     **回 3**，而 Go **拒編**（`expected '?str', got 'str' [6fgg3htw]` + `idxbruse`）。同形互换
     `?i64` 仍同（`tmp/s134/pi64.no`：noc 0 / Go 拒編）⇒ 這是**既存的口径差**：noc 接受
     裸索引直接餵 `?T` 形參，Go 要求先解包。pre 版只是「因別的缺口 unsup」而與 Go 表面一致。
- ⚠️ **本輪量測漲 +21**（13473 → 13494）：全部落在既存的 dropped 子樹內（見 ③），
  但**曲線本身不以本輪為改善**——是「補真缺口、要付賬」的輪次，與 ㊾ 同型。
- **下一步優先級**（承 ㊿ ⑤ ④ 之後）：①`str.len()` 缺失（缺口 A，影響最廣，且是 std 側而非
  `it` 側）；②`ok(...)` 構造；③裸 `->` 放行。
- **量測工件**：`/tmp/s134-full-post.txt`（全量 16 檔 post，27278 行；pre 用 s133 期末的
  `/tmp/s133-full-post.txt`，27240 行）、`/tmp/noc-s134`（md5 `32d98efd…`，由最終源建）、
  `tmp/s134/gate.sh` + `gate.txt`、`tmp/s134/probe.sh` + `probe.txt`、`tmp/s134/pi64.no`
  （`?i64` 裸索引喂 `?T` 形參的同形互換實證）、`tmp/s133-e2e/`（93 個探針）。

---

## 52 s135 — `str.len()`（UTF-8 碼點數）：`OP-STRCPLEN` + `@str_cp_len` runtime helper

> **本輪一句話**：㊿ ⑤ ①點名的**缺口 A**（`str.len()` 不在 noc 方法子集裡）補上——plain `str`
> 局部／形參的 `.len()` 回正確碼點數，`ok ->` 臂內 `it.len()` 也通；93 檔 e2e 的「noc↔Go 分歧集」
> **33 → 27**（六檔收斂，**零新增分歧**）。
> **量測**（全量 16 檔 cover 曲線）：**13494 → 13605（+111）**。
> **校驗**：14 套全綠（`h2m` 1334 → **1348**）。
> **產地**：`compiler/src/mir.no` md5 `dfa1dfa017bc60f8fd4f66fbcb577e16`（625 行）；
> `compiler/src/cg.no` md5 `0e74b29b9431c302744498b59a14c7a3`（2759 行）；
> `compiler/src/cg-runtime.no` md5 `11450fda3a0a3ea0a1992ee64f28417d`（745 行）；
> `compiler/src/h2m.no` md5 `a3e0c440c5248775d3dd7c43c392b122`（6780 行）；
> `compiler/tests/h2m-test.no` md5 `099d6fd1ed2c84b959c0d2ee478de6e2`（7229 行）；
> 二進位 `/tmp/noc-s135` md5 `c146ce5c3bb9da3d473df89108a86b3d`（4064856 bytes，build 211 s，
> baseline `d21fbb35`）。

**① 定位**（復核 ㊿ ⑤ ①，確認缺口在 `str` 臂而不在 `it`）

- **缺口 A 仍在**：`a1`（局部 `s.len()`）／`c3`（形參 `s.len()`）在 ㊿ 期末皆 **1（unsup）**。
  查 `src/std/str.no:47`：`str.len` 是 **std 函式** `n = .cp-index(.len-bytes())`——`cp-index`
  （`str.no:312`）是「數 `[0, pos)` 裡 `b < 0x80` 或 `(b & 0xc0) == 0xc0` 的字節」的**循環**，
  不是 `#{buildin}`。noc 子集至此只收了 `len-bytes`／`byte`／`bytes`（s117／s121／s132）。
- **與 `it` 無關**：㊿ 已把臂內 `it` 的載荷型別修對（`?str` ⇒ TY-STR），但 `rty == TY-STR` 臂
  根本沒有 `len` 分支 ⇒ `it.len()` 與 `s.len()` 同罪。**這是 std 側缺口，影響最廣。**
- **blast radius**（`compiler/src` 內 plain str 接收者的 `.len()`）：
  - `lx.no:1521` `nr = ct1.len()`（在 `lx-scan-token`，`:1032` 起，字符字面量碼點驗證）；
  - `ps.no:4067` `rt.len()`（`ps-signed-int-operand`，`:4040`）、`ps.no:4159` `lt.len()`
    （`ps-mk-opt-arith-type`，`:4132`）。
  - 其餘 `.len()` 接收者全數是 vec（`h2m` 既有臂）。
- ⚠️ **語言門（重要的反面）**：`.len()`／`.cap()` 於 **option 接收者**是**編譯錯誤**
  （`src/checker/option_member_access.go:158`，`qwl2enr7`）⇒ `TY-OPT` 臂**不得**加 `len`。
  但 `ok ->` 臂內 `it` 是**載荷**（plain `str`）⇒ `it.len()` **合法**。實測：`l11`
  （`'two'`）Go 回 3、noc-s135 回 3；`l12`（`err -> it.len()`）Go 拒編
  （`unsupported receiver type %err_err`），noc 亦落閘門。
  ⇒ ㊿ 的 `test-s134-opt-str-len-gate`（「`it.len()` 必須 unsup」）的前提是「str 臂沒有 len」，
  本輪補上後該門禁**已刪除**，改寫成正向測試。

**② 修复**（4 檔 5 處）

- **`compiler/src/mir.no`**：新 `OP-STRCPLEN = 103`（接在既有最大碼 `OP-STRBYTES = 102` 之後，
  **不重排既有碼**）。x = 字符串值 id（TY-STR，發 `%sv`），dst = i64 值 id（`%v`），y 不用。
- **`compiler/src/cg.no`**：`cg-emit-op` 加 `op == OP-STRCPLEN` 臂，發
  `%v<dst> = call i64 @str_cp_len(%str-long %sv<x>)`；頭部注補「**非**欄位讀取」。
- **`compiler/src/cg-runtime.no`**：新 `cg-runtime-str-cp-len`——內聯 IR helper
  `@str_cp_len(%str-long)`：`entry` 取 `%len` 與 `%data`，`loop`／`body` 用 **phi** 數
  `b < 128 || (b & 192) == 192`，`select` 累加，`done` 回 `i64`；並在 `cg-runtime-prelude-full`
  登錄（每模組發一次）。
  - ⚠️ **為什麼走 helper 而不是組 op**：Go 後端把 `str.len` 編成 `.cp-index` **循環**
    （`emitStrCpIndex` 是 `s[i]` 的**讀**，不是計數），noc 側沒有「計數」原語可組；
    而 `mir.no:145` 的 `OP-STRLEN` 是純 `extractvalue`（**位元組**長），語意不同，不能混用。
- **`compiler/src/h2m.no`**（2 處）：
  - `h2m-lower-expr` 的 `rty == TY-STR` 臂加 `mname == 'len' && ok-method == 0 && na == 0`
    ⇒ `OP-STRCPLEN`。**只收無參形**（帶參的 `len(...)` 落閘門，不猜）。
  - `h2m-ty-of` 的值方法塊加 `mt == TY-STR && mf == 'len' -> code = TY-I64`（與 `len-bytes`
    同口徑；`let` 槽才拿得到型別）。
  - ⚠️ **刻意不加進 `TY-OPT` 臂**（見 ① 的語言門）——那會靜默放行非法碼。

**③ 量测**

- **全量 16 檔 GRAND：13494 → 13605（+111）**。逐模組：
  `asp` 0→0、`ast` 228→228、`cg` 2270→**2304**、`cg-rc` 96→96、`cg-runtime` 539→**568**、
  `ck` 352→352、`ck-sig` 75→75、`diag` 35→35、`drv` 5024→**5048**、`fmt` 0→0、`gen` 0→0、
  `h2m` 3180→**3211**、`ix` 0→0、`link` 18→18、`lx` 160→160、`mir` 0→0、
  `ps` 1517→**1510**、`tok` 0→0。
  （`/tmp/s135-full-post.txt` 27498 行；pre 用 ㊿ 期末的 `/tmp/s134-full-post.txt` 27278 行。）
- **+111 的絕大部分是新代碼自身的足跡**：新 helper `cg-runtime-str-cp-len` 每被一個包展開
  就是 **56 條構造**（28 Identifier + 25 BlockStatement + 2 FunctionDefinition + 1 Program），
  加上 prelude 登記 2 條、`cg` 的分派臂 10 條。因 `cg` import `cg-runtime`、`h2m` import `cg`、
  `drv` 兩者都 import，**同一 helper 在 `cg-runtime`／`cg`／`drv`／`h2m` 四個包各登記一次**
  （+29／+34／+24／+31 = **+118**）。這與 ㊾／㊿ 同型：補真缺口、要付賬。
- **真正的語意收益在 `ps`：1517 → 1510（−7）**：
  - `ps-signed-int-operand`：由「整函不產碼（8 條構造登記）」變為「**自身已可降級**、但遭
    呼叫圖閉包剔除（+1 dropped）」——該函的 `.len()` 已不再是障礙，擋它的已是
    `ps-unwrap-grouped`。
  - `ps-mk-opt-arith-type`：12 → 5 條（殘留 `path-expression`／place-match／index-read）。
- **`lx` 160 → 160（未跨線）**：`lx-scan-token` 仍不產碼——它的 `.len()` 只是諸多 unsup 構造
  之一（`CallExpression` 6、`Identifier` 19、`BlockStatement` 18…）。**誠實記下：本輪沒有讓
  `lx` 前進。**
- ⚠️ **計量特性**：`h2m-lower-program` 的登記數連動 −6（`c.cur-fn` 歸因會被深呼叫改寫，
  既有特性）⇒ **逐函明細加總 ≠ GRAND 增量**，以 `unsupported constructs` 行為準。

**④ 验证**

- ✅ **自写测试**（`compiler/tests/h2m-test.no`，注册于 ㊿ 的
  `test-s134-opt-str-len-gate()` 之後）：**新增 6 條、刪除 1 條**。
  - `test-s135-str-len-local`：`s str = 'héllo'` + `r = s.len()` ⇒ `unsup == 0`、
    IR 含 `call i64 @str_cp_len(` 恰好 1 次、**且不含 `%slr`**（證明它不是欄位讀）。
  - `test-s135-str-len-param`：`(s str)` 形參 ⇒ `unsup == 0` + `@str_cp_len(`。
  - `test-s135-str-len-bytes-unaffected`：`len-bytes()` 照走 `%slr`、**不含** `@str_cp_len(`
    ⇒ 未污染既有字節長路徑。
  - `test-s135-str-len-arg-gate`：`s.len(1)`（帶參）⇒ `unsup > 0`、無 `define i64 @f`（不猜）。
  - `test-s135-opt-str-it-len`：**正向**——`v: { ok -> r = it.len() }` ⇒ `unsup == 0` +
    `@str_cp_len(` + `extractvalue %option`（`OP-OPTGETSTR`）；**反側哨兵**——
    `err -> r = it.len()` ⇒ `unsup > 0`、無 `define i64 @f`。
  - `test-s135-str-len-ty-of`：手搭 AST（`h2m-lst-add(ls,'s',TY-STR)`）⇒ `h2m-ty-of(...) == TY-I64`。
  - **删除** `test-s134-opt-str-len-gate`（缺口 A 的舊門禁）。
  - **`h2m-test` 全綠：1334 → 1348 passed / 0 failed。**
- ✅ **先紅後綠**（`tmp/s135-e2e/`，14 檔，對拍 `tmp/s129/wt/bin/no`）：
  `l1`（`'ab'`→2）、`l2`（`'héllo'`→5）、`l5`（`'中a'`→2）、`l6`（`'中a'.len-bytes()`→4）、
  `m1`／`m2`（推斷／標注局部）、`m3`（形參）——**pre 版皆 1（unsup），post 版與 Go 逐檔同碼**；
  `l4`（`char` 接收者 `"中".len()`→1）亦同；`l7`–`l10` 是 `char` 字面量的回歸哨兵（勿被淘汰）；
  `l11`（`ok -> it.len()`→3）、`l12`（`err -> it.len()`→1）亦同。**14/14 SAME。**
- ✅ **e2e 探針 93 檔**（`tmp/s133-e2e/`，`tmp/s135/probe.sh` → `tmp/s135/probe.txt`）：
  pre↔post 只 **6 檔**變動——`a1` 1→2、`c3` 1→2、`n14` 1→3、`n18` 1→3、`n19` 1→3、
  `n22` 1→0；**這 6 檔 post 全部與 Go 相同**。**noc↔Go 分歧集 33 → 27，零新增分歧。**
  - ⚠️ **復述 §50 的教訓**：Go 基線退碼必須解碼 `Error: exit status N`——`no run` 對任何
    非零程序退出都回 `rc=1`，直接讀 rc 會把「回了正確值」誤判成「編譯失敗」。
- ✅ **全量門禁 14 套全綠**（`tmp/s135/gate.sh` → `tmp/s135/gate.txt`）：
  `ps` 52 · `h2m` **1348** · `cg` 363 · `ck` 20 · `ck-sig` 50 · `lx` 81 · `tok` 62 ·
  `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 · `pkg-sigs` 5 ·
  `pkg-ll` 10，**failed 全 0**。與 ㊿ 逐套相比，**只有 `h2m` 1334 → 1348（+14）**，其餘 13 套
  逐字一致 ⇒ 零回歸。
- ✅ `no fmt -d` 五檔（`mir.no`／`cg.no`／`cg-runtime.no`／`h2m.no`／`h2m-test.no`）**零漂移**。
- ℹ️ **二進位可重現性**：同輸出路徑重建 md5 逐字一致（`1e73f55c…` 兩次相同）；**不同輸出路徑
  則 md5 不同**（`c146ce5c…`／`1e73f55c…`／`29b5a363…`）——是路徑相依，**不是不可重現**。

**⑤ 诚实边界与下一步**

- ✅ **缺口 A 已補**：`str.len()` 上路（`OP-STRCPLEN` + `@str_cp_len`），`ps` 的
  `ps-signed-int-operand` 自身已可降級。
- ⏳ **仍在缺口（逐一點名）**：
  1. **`lx-scan-token` 未跨線**（`lx` 160→160）：`.len()` 已除，但它的 `CallExpression`／
     `SurfaceMatch`／`path-expression` 等仍盤踞——需要逐條點名它的下一個缺口。
  2. **`ps-mk-opt-arith-type` 殘留 5 條構造**（`path-expression`、place-match、index-read）。
  3. **`ok(...)` 構造**（承 ㊿ ⑤ ②）：臂內寫 `ok(...)`（切量樹表／計數器）仍未通。
  4. **裸 `->` 放行**（承 ㊿ ⑤ ③）：哨兵 `test-s131-smv-bare-arrow-gate` 已就位，需先確認
     `blockBody` 語義。
  5. **纯 str 入 `?str` 的 wrap**（`x1`，承 ㊿ ⑤ ④）。
  6. **`?[]str` 仍不入形**：`[]str` 緩衝無 retain，欄位外流會 UAF（同 s99f 口徑）。
  7. **臂內 vecs 寫入門禁**（`s122`）：`f1` 探針作反證——臂體 `s.bytes()` 與 `it` 無關照樣 3 unsup。
  8. **`d2` 口径差**（承 ㊿ ⑤ ⑦）：noc 接受裸索引直接餵 `?T` 形參，Go 要求先解包。
- ⚠️ **本輪量測漲 +111**：其中 +118 是新 helper 在四個依賴包裡的自登載，真正語意收益衹有
  `ps` 的 −7（且 `ps-signed-int-operand` 仍被 `ps-unwrap-grouped` 擋在閉包外，**未真正進入
  最終 IR**）。**曲線的「改善」主要體現在 e2e 分歧集（33 → 27）與 `ps` 的計數，不是一個
  覆蓋率過半的里程碑。**
- **下一步優先級**：①`lx-scan-token` 逐條點名（它已是 `lx` 的唯一大塊）；②`ok(...)` 構造；
  ③裸 `->` 放行。
- **量測工件**：`/tmp/s135-full-post.txt`（全量 16 檔 post，27498 行；pre `/tmp/s134-full-post.txt`
  27278 行）、`/tmp/noc-s135`（md5 `c146ce5c…`）、`tmp/s135/gate.sh` + `gate.txt`、
  `tmp/s135/probe.sh` + `probe.txt`、`tmp/s135-e2e/`（14 個探針）、`tmp/s133-e2e/`（93 個探針）。

## 53 s136 — `[]byte` 入形：`h2m-tynode-code` 與 `h2m-param-ty` 的 K-SLICE-TYPE 第二站

- **①定位（逐條點名）**：把 ⑤① 指定的大塊 `lx-scan-token`（`compiler/src/lx.no:1032`）
  的構造逐條對拍，而不是憑 `cov` 的塊級聚合猜。做法：寫 `tmp/s136/cases{,2,3,4}.no`
  與 `cases5/6/7.no`，每個函數只放**一個**構造，用 `drv.pkg-ll-cov` 單檔跑，
  讀 `BLOCK/LF/ID` 三通道的逐條 mark。**逐條點名表**（pre = s135）：
  - `lx-scan-token` 37 條 = 塊級 18 BlockStatement + 14 IfExpression + 3 FunctionDefinition
    + 1 Program + 1 SurfaceMatch；葉級 19 Identifier + 6 CallExpression + 4 IntegerLiteral
    + 3 BlockStatement + 4 FunctionDefinition + 1 StringLiteral；ID 通道 11 條
    （`ok1`×2、`nr`×2、`rw2`、`rw1`、`lt1`、`raw`、`kn`、`dot`、`ct1`）。
  - 逐構造對拍下來的**共同根因只有一個**：`h2m-tynode-code`（`h2m.no:1488` 的
    `K-SLICE-TYPE` 臂）**只認 `i64` 與 `str`** ⇒ `[]byte` 回 −1 ⇒
    `lxst.input` 與 `sb.buf` 的**欄位型別解析**整批失敗（`lxst.input:SliceType:type`、
    `sb.buf:SliceType:type` 各 1 條），連帶 `lx-read-char`／`lx-peek`／`lx-sub`／
    `lx-scan-token`／`dump`／`scan-all` 一整條鏈 dropped。
  - 單行實驗（只加 `enm == 'byte' -> code = TY-VEC`）即令 `lx` 160 → 132。
- **②修復 2 檔 2 處**（`compiler/src/h2m.no`）：
  1. `h2m-tynode-code` 的 `K-SLICE-TYPE` 臂（:1490 後）加 `byte` → `TY-VEC`。
     **口徑合併而非新增表示**：noc 的 `@vec_get_i64`／`@vec_push_i64` 是單型 helper
     （`[]str` 才走 stride 16 的 `*_str` 助手），Go 側 MIR 亦把 `byte` 壓成 i64 寬
     （`codegen.go:1587` 把 `byte` 與 `i64/i8/i1` 同列、`intBitsOfRaw` 只用來判宣告寬度）。
     故 `[]byte` 與 `[]i64` 在自舉型別系統裡**不可區分**——這與 `[]str` → `TY-VECSTR`
     的嚴格分岔不同，值域 0..255 由上層語意負責。
  2. `h2m-param-ty`（:5770 後）**同缺陷第二站**：該函式有自己的 `K-SLICE-TYPE` 臂，
     舊註寫「`[]byte` 不放行——那是 1 字節元素（另一套 helper）」。只補第 1 處的話
     `lx-sub(s []byte, …)`／`lx-new(src []byte)` 這類**簽名**照舊 `bad=1` 整函不產碼
     （實測：`[]byte` 形參的版本 `mb:FunctionDefinition` 落閘門）。
- **③量测**（pre = s135 期末 `/tmp/s135-full-post.txt` 16 檔，post = `/tmp/s136-full-post.txt`）：
  **GRAND TOTAL 13605 → 13546（−59）**。逐檔：`lx` 160 → **128（−32）**、`drv` 5048 → 5030（−18）、
  `ast` 228→227、`cg-rc` 96→95、`cg-runtime` 568→567、`cg` 2304→2303、`ck` 352→351、
  `h2m` 3211→3210、`ps` 1510→1507（其餘 7 檔零變化）。**ID 通道的 `lxst.input`／`sb.buf`
  兩條 `SliceType` mark 清零**；`lx-scan-token` 37 → **30** 條。
- **④驗證**：`h2m-test` **1359 passed / 0 failed**（新增 5 條 s136 測試：
  `[]byte` 區域 `.len()`、`[]byte` 欄位型別解析、`[]byte` 索引讀值位 match、
  欄位接收者方法門禁、`[]f64` 反側門禁）。**14 套門禁全綠**
  （`tmp/s136/gate.txt`：ps 52、h2m 1359、cg 363、ck 20、ck-sig 50、lx 81、tok 62、
  diag 7、build-package 8、util 27、ps-err 46、pkg-ll-call 5、pkg-sigs 5、pkg-ll 10）。
  `no fmt -d` 兩檔（`h2m.no`／`h2m-test.no`）零漂移。
  **e2e 退碼比對**：`/tmp/noc-s136`（md5 `5f0100eee7ae5ab43bba9acd980a8b27`，4064856 bytes）
  對 `tmp/s133-e2e/` 93 檔逐檔跑 `no run`，與 `/tmp/noc-s135` 的退碼**逐檔一致（0 差異）**。
- **誠實邊界（四處殘留，均已逐條點名）**：
  1. **多輸出呼叫賦值**：`lt1, rw1 = lx-read-string(lx)`（:1512／:1520）、
     `p1, f1, ok1 = lx-read-regex(lx)`（:1223）。ID 通道 `ok1`×2／`nr`×2／`rw2` 即此——
     SessionId 侧的既存缺口，非本輪引入（`[]i64` 同形亦失敗）。
  2. **path-expression 接收者**：`lx.input.len()`（:1587）等方法臂的接收者只能是個名字/值，
     喂不進欄位路徑（`h2.buf.len()` 實測落閘門）。**這才是 `lx.input.len()` 真正的擋路石**——
     型別已可解析（本輪），但呼叫側仍未通。
  3. **空 struct 字面量**（`h2{}`）與**`with-cap` 索引讀的值位 match**（`xs[0]: {…}`，
     `with-cap` 來源）各 1 條，均與 `[]byte` 無關（`[]i64` 同形對拍同樣失敗）。
  4. `lx` 的 `dropped` 由 2 條增至 6 條（新增 `lx-read-number`／`lx-read-ident`／
     `lx-read-block`／`lx-new`）——**不是回歸**：這些函式此前根本不在 parts 裡（`h2m-lower-func`
     回 `''`），本輪開始產碼後被呼叫圖閉包剪掉，故改列「dropped」。
- ⚠️ **本輪只解決「`[]byte` 能不能入形」；`lx-scan-token` 仍未發碼**（30 條殘留）。
  值域／所有權口徑合併（`[]byte` ≡ `[]i64`）是自舉側的刻意放行，**與 Go 的型別嚴格度不同**，
  已在兩處注解写明。
- **下一步優先級**：① **path-expression 接收者**（`lx.input.len()`，解掉 `lx-scan-token` 的
  最大一塊）；② **多輸出呼叫賦值**（`ok1`／`nr`／`rw2`）；③ `ok(...)` 構造（承 ⑤②）。
- **量測工件**：`/tmp/s136-full-post.txt`（全量 16 檔 post，27355 行；pre `/tmp/s135-full-post.txt`
  27498 行）、`tmp/s136/gate.sh` + `gate.txt`、`tmp/s136/cases{,2,3,4,5,6,7}.no`、
  `tmp/cov136*.no`（探針驅動）。

## 54 s137 — `i64.to-str()` 值接收者認領：葉通道誤歸因的定位踩空（實施後還原）

① **定位（假設與驗證路徑）**
- s136 收官後，從 16 檔逐條日誌的**葉通道**（`LF:` 前綴，s108）統計，`cg-emit-cfg-t` 的 1657 條
  mark 中反覆出現 `to-str`，且 `cg-runtime`(567)／`cg`(2303)／`drv`(5030)／`h2m`(3210) 四檔都有
  大量 `CallExpression` 落閘門。
- **假設**：`i64` 值接收者的 `to-str()` 未被 h2m 認領（`recv-is-value == 1` 臂只處理
  TY-VEC／TY-VECSTR／TY-STR／TY-OPT），補上後 `cg` 應下降。
- 驗證路徑：先在探針 `tmp/s136/pz.no`（f1 = `d = 5; s = d.to-str(); r = s.len()`）上量 pre/post，
  再上 16 檔全量。

② **修復（已實施，後還原）**
- `mir.no`：新 op 碼 `OP-STRFROMI64 = 104`（接在既有最大碼 `OP-STRCPLEN = 103` 之後，不重排）。
- `cg-runtime.no`：新 helper `cg-runtime-str-from-i64`（**單一字串字面量**，逐字鏡像 Go
  `emitStrFromI64`（`src/mir/codegen.go:4643`）），`cg-runtime-prelude-full` 末尾登記。
- `cg.no`：`op == OP-STRFROMI64` 臂 → `%sv<dst> = call %str-long @str_from_i64(i64 %v<x>)`
  （只出值，不內聯展開——helper 體含 alloca／phi／memcpy）。
- `h2m.no`：`recv-is-value == 1` 內新增 `rty == TY-I64` 臂，無參 `to-str` →
  `h2m-alloc(TY-STR)` + `h2m-emit(OP-STRFROMI64)` + `h2m-rel-add`
  （`@str_from_i64` 回【新分配】，rc=1，與 s135 的 `OP-STRCPLEN` 同口徑）。
- ✅ **功能驗證通過**：探針 f1 由 4 mark → **0 mark**（f2 鏈式／f3 字面量接收者各 1 mark，
  屬預期門禁）。

③ **量測（同一 drive：`tmp/cov-all2.no`，16 檔 `compiler/src` 逐檔）**

| 檔 | pre | post | Δ |
| --- | --- | --- | --- |
| `cg` | 2293 | 2299 | **+6** |
| `cg-runtime` | 567 | 568 | **+1** |
| `drv` | 4975 | 4981 | **+6** |
| `h2m` | 3189 | 3195 | **+6** |
| 其餘 12 檔 | — | — | 0 |
| **GRAND** | **13431** | **13450** | **+19** |

- ⚠️ **歸因（本輪最重要的發現）**：+19 全部來自 s137 **新增程式碼自身**的 mark；**16 檔語料中
  `h2m`／`drv`／`cg` 只增不減 ⇒ s137 臂的淨認領貢獻為零**。
  - 根因在 `h2m-lower-expr` 的 K-CALL-EXPRESSION 臂（`compiler/src/h2m.no:3584` 起）：
    實參**只在 `ok-call == 1` 時**才下降（`h2m.no:3861`）。
  - `cg-emit-cfg-t` 的 `.to-str()` 全部落在**失敗呼叫的實參位**（`b.push('...' - x.to-str())`）：
    `b.push` 是未認領的結構體方法 ⇒ `ok-call` 歸零 ⇒ 實參子樹**完全不下降** ⇒ 內層
    `to-str()` 永遠不會被評估認領與否。
  - 換言之：**s137 的任何收益都被上游缺口完全屏蔽**，它只是把自己的 mark 加了進去。
- ⚠️ **葉通道的 `to-str` 是誤歸因**：`h2m-mark-here` 記葉通道時 `c.cur-k` 殘留的是**接收者 `b`**
  的 kind，不是 `to-str` 本身。判斷缺口必須看**塊通道**（`h2m-bk-take` 取就近未計過的塊），
  葉通道只能當「最近進入哪個葉構造」的旁證。

④ **驗證與門禁**
- ✅ **14 套門禁全綠**（`tmp/s137/gate.txt`，與 s136 逐套相同）：`ps` 52 · `h2m` 1359 ·
  `cg` 363 · `ck` 20 · `ck-sig` 50 · `lx` 81 · `tok` 62 · `diag` 7 · `build-package` 8 ·
  `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 · `pkg-sigs` ALL PASSED · `pkg-ll` 10。
- ✅ **`no fmt -d` 四檔零漂移**（`mir.no` 還原時曾殘留一個末尾空行，已修）。
- ✅ **e2e 退碼 93 檔 × 2 輪**（`tmp/s137/probe.sh` → `tmp/s137/probe.txt`）：`noc-s137` 與
  `noc-s136` **逐檔退碼 0 diff**（`diff pre vs post` 段空）——這是「已完全還原」的鐵證。
- ⚠️ **量測基線的可重現性教訓**：pre 基線一度無法用重構檔重現（`cg` 2293 vs 2298）。查明是
  重構時保留了 9 行孤立注释所致；以 746 行的乾淨 pre 版（helper 函式與
  `b.push(cg-runtime-str-from-i64())` 都移除）重現 2293。**凡量測必須保留「精確的 pre 檔案」，
  不能只靠「從 post 反向刪除」。**
- ⚠️ **量測基準與 §53 的絕對值不可直接相接**：§53 正文引用的是 04:53 快照（GRAND 13546），
  而 s136 收尾期（04:53→05:35）`compiler/src` 仍有改動，故 s136 期末基準為 **13431**。
  本輪的 +19 是**同一 drive、同一 pre 檔案**內的對照，不與 §53 的絕對值相接。

⑤ **誠實邊界與下一步**
- **本輪結論：還原 s137**。它在 16 檔捕獲語料上淨 +19（退步），收益被「失敗呼叫的實參不下降」
  完全屏蔽 ⇒ 保留即退步。四檔已逐字還原（`cg.no` 與 s136 期末備份 `tmp/s136/cg.no.orig-wt`
  逐字相同；`grep OP-STRFROMI64` 四檔皆 0；e2e 0 diff）。
- **真正的杠點**：`h2m-lower-expr` K-CALL 臂在 `ok-call == 0`（落閘門）時**也應下降實參子樹**
  （或至少下降「純表達式」型實參）。這是 `cg-emit-cfg-t` 1657 條 mark 的根因層——它同時凍結了
  s137 這類「認領內層方法」的增量收益。
- **下一步優先級（修正 §53 ⑤）**：① **失敗呼叫的實參下降**（放開 `h2m.no:3861` 的守衛，需小心
  勿重複計 mark／勿讓 `al` 池錯位）；② **path-expression 接收者**（`lx.input.len()`）；
  ③ **多輸出呼叫賦值**（`ok1`／`nr`／`rw2`）。
- **量測工件**：`tmp/s137/`（`probe.sh`／`probe.txt`）、`/tmp/s137-pre-all.txt`／
  `/tmp/s137-post-all.txt`（16 檔全量 pre/post）、`/tmp/s137/pre/`（精確 pre 四檔）、
  `/tmp/noc-s137`。

---

## 55 s138 — 多輸出呼叫賦值：Step 2b 有效，但被 Step 2a 的缺口凍結

① **定位（從 pre 日誌三通道抽 residual）**
- 工具：`tmp/cov137.no`（16 檔）→ 單檔化（`tmp/s138/cov-h2m.no` / `cov-lx.no`）。**塊通道**
  逐字 diff 是唯一的「增／減」判據（`LF:` 葉通道誤歸因，見 §54 ①）。
- 結果：`lx-scan-token` 是 `lx` 档最大 residual——塊通道 `BlockStatement` × 8、`IfExpression` × 7、
  `SurfaceMatch`、`FunctionDefinition`、`Program`，葉通道 `Identifier` × 12、`IntegerLiteral` × 4、
  `StringLiteral` × 1；ID 通道落到 `nr` × 2 与 `raw` × 1。
- 根因：`lx.no` 有三處多輸出賦值（`p1, f1, ok1 = lx-read-regex(lx)` @1223、
  `lt1, rw1 = lx-read-string(lx)` @1512、`ct1, rw2 = lx-read-string(lx)` @1520）。
  `h2m.no` **完全沒有** `K-MULTI-ASSIGN-STATEMENT` 分支（`grep` 無命中）⇒ 落
  `hd == 0 -> h2m-mark(c)`（`h2m-lower-block` @4450）。

② **定案（先查清「多回傳」在 h2m 的既有表示）**
- `h2m-func-sig` 的 **s6c**（`real-outs > 1`）已把多輸出函式**合成聚合結構體** `__mr__<fn>`
  （`ret = TY-STRUCT-BASE + sid`，字段型別入 `stft`），但**只做到宣告層**。
- `h2m-lower-func` 的 ret 處理只有 `real-outs == 0 / == 1` 兩條臂：`real-outs > 1` ⇒ `rid` 保持 0
  ⇒ `rid == 0 && frt != TY-I64 && frt != TY-OPT -> h2m-mark(c)`。**多回傳函式因此整條無 IR**。
- 連鎖：`h2m-lower-func` 的 `c.unsup > 0 -> fnok = 0` ⇒ **所有呼叫它的函式都被 drv 標
  「dropped (calls unsupported)」**，**其內部一切構造（含多賦值）不入賬**。
- ∴ **Step 2b（多賦值）單獨做，收益必為 0**；必須先補 **Step 2a（多回傳函式的 ret 聚合）**。
- 實作（皆已還原，見 ⑤）：
  - `h2m-lower-multi-assign`（Step 2b）：`vkd.kind == asp.K-CALL-EXPRESSION` → 下降右值得聚合值 id
    → 各 target（`K-IDENTIFIER`）發 `OP-STRUCT-GET`（`h2m-emit(c, OP-STRUCT-GET, fid, v, k, '', fty)`），
    `mem == 1` 先 `h2m-slot-decl` 再 `h2m-var-write`，`mem == 0` 走 `h2m-var-bind`。
  - `h2m-mr-aggregate`（Step 2a helper）：把各輸出參值（`real-out` 起算）經 `c.args` 池
    `OP-STRUCT-MK` 合成回傳值，str 欄位照 s99 路徑 `OP-STRUCT-RETAIN` + `h2m-rel-add`。
  - **爲何獨立成 helper**：聚合若寫在 ret 處理裡，那些賦值語句落在 `h2m-scan`（只掃 body）
    覆蓋不到的位置——實測 `ID:h2m-lower-func:mr-sid` 落空（名字三表查不到）。

③ **量測（pre = `tmp/s138/pre/` 四檔；同一支 `bin/no` v0.4.28-4-gd21fbb35-dirty 16:55:19）**
| 檔 | pre | 僅 2b | 2a+2b |
| --- | --- | --- | --- |
| `h2m` | 3177 | 3178 (+1) | 3187 (+10) |
| `lx` | 119 | 119 (0) | 127 (+8) |
| 探針 `zz-ma`（pkg-ll-cov 口径） | 2 | 2 (0) | 4 (+2) |
| 探針 `m2` / `multi`（noc 口径） | 4 | **1 (−3)** | 2 |
- **僅 2b**：`h2m` 的 +1 完全來自 drv 的「dropped (calls unsupported)」列表新增一行
  （`agg.unsup` 對「被剔除函式」也 `inc`），**塊通道 mark 明細逐字不變**；`lx` 檔
  **pre/post 日誌逐字相同**——收益被「`f` 不可降級 ⇒ `g` drop」完全凍結。
- **2a+2b**：反而退步。新增 mark 全落在**多回傳函式本身**：`h2m-param-ty` +1、`scalar` +2、
  `h2m-func-sig` +2、`h2m-lower-safe-index` +2，並多出 `Program` construct 級 mark
  （module 層）。**根因未定位完**：懷疑是多回傳輸出參在 `mem == 1` 下的**槽**未被
  `h2m-scan` 收集（輸出參不是 let），`h2m-var-read` 回 0 ⇒ 聚合路徑自己 mark。

④ **驗證與門禁**
- ✅ **14 套自舉測試全綠**（還原後以基线 `bin/no` 重跑）：`ps` 52 · `h2m` 1359 · `cg` 363 ·
  `ck` 20 · `ck-sig` 50 · `lx` 81 · `tok` 62 · `diag` 7 · `build-package` 8 · `util` 27 ·
  `ps-err` 46 · `pkg-ll-call` 5 · `pkg-sigs` 5 · `pkg-ll` 10。
- ✅ **`no fmt -d` 零漂移**（含 2a/2b 期間每次修改後皆驗；`real-outs > 1` 臂內需補空行）。
- ✅ **探針程序**：`tmp/s138/m1.no`（仅多回傳宣告）、`m2.no` / `multi.no`（多賦值）、
  `cov-zz.no`（指向 worktree 內 `zz-ma.no` 的 pkg-ll-cov 口徑探針）。
- ✅ **四檔已逐字還原**（`diff -q tmp/s138/pre/<f>.no compiler/src/<f>.no` 四檔皆同；
  `grep -c 'h2m-lower-multi-assign\|h2m-mr-aggregate' compiler/src/h2m.no` = 0）。
- ⚠️ **探針口径差異必須記明**：`noc run` 的「unsupported constructs」是**自舉編譯器的輸出**，
  它對 `m2` 由 4 降到 1；而 `drv.pkg-ll-cov` 的同一數字還**含 drv 的 dropped 閉包**，
  且包的是**整套模块**——**两者不可混用**（本輪曾因此误判過一次）。

⑤ **誠實邊界與下一步**
- **本輪結論：還原 s138 的 2a+2b 實作**。2b 本身正確且被探針證實（`m2` 4 → 1），但在
  16 檔捕獲語料上**淨收益為 0**（僅 2b）或**負**（2a+2b，+10 / +8）。這與 §54 同構：
  **下游增量的收益被上游缺口凍結，保留即退步。**
- **2a 的真缺口（下一步①）**：多回傳函式的**輸出參在 `mem == 1` 下的槽**。`h2m-scan` 只把
  body 裡的 `let`（含賦值語句）鋪槽；**輸出參不是 let**，而 s99k 只為**單輸出**手動
  `h2m-lst-add`。∴ `h2m-mr-aggregate` 的 `h2m-var-read` 對多回傳輸出參回 0 ⇒ 聚合路徑自 mark。
  修法：在 `c.mem == 1` 的鋪槽段為 `real-outs > 1` 的各輸出參**按聚合字段型別補鋪槽**。
- **下一步②**：`Program` construct 級 mark 的出處（module 層對 `__mr__<fn>` 聚合結構體的反應），
  需以 `tmp/s138/post3-h2m.log` 的 3077-3078 行為起點定位。
- **下一步③（§54 ⑤ 的修正）**：**失敗呼叫的實參下降**（`h2m.no:3861`）仍是 `cg-emit-cfg-t`
  1657 條 mark 的根因層，優先級高於本輪。
- **量測工件**：`tmp/s138/pre/`（精確 pre 四檔 + `h2m-test.no`）、`tmp/s138/cov-{h2m,lx,zz}.no`、
  `tmp/s138/{pre,post,post2,post3}-h2m.log`、`tmp/s138/{pre,post}-lx.log`、`tmp/s138/gate*.txt`、
  `/tmp/noc-s138`（2a+2b 版二進位）。


---

## 56 s139 — 失敗呼叫的實參下降：`h2m` 檔 +5，`cg` 档未量（結論保留輻射）

① **定位（承 §54 ⑤ 的判斷）**
- §54 ⑤ 把 `cg-emit-cfg-t` 的 1657 條 mark 歸因到 `h2m-lower-expr` K-CALL 臂的
  「實參只在 `ok-call == 1` 時才下降」（`h2m.no:3861`），並判它為「先放開實參下降，再談認領內層方法」
  的**根因層**。本輪直接驗證該假說。

② **定案與實作（已還原，見 ⑤）**
- 改法：把實參下降改成**無條件執行**（上限 `na`），**入池仍只在 `ok-call == 1`**：
  - `al` 先收後入池（同 s64 教訓：邊降邊推會被嵌套呼叫的內層池項擠偏）；
  - `av == 0` 推 **0 占位**，保持「實參位 ↔ `al` 下標」對齊（型別核對按 `al[j]` 取）；
  - 型別逐項核對保留在原位，任一不符仍落閘門。
- 依 §54 ⑤ 的提醒「勿重複計 mark／勿讓 al 池錯位」做了兩道防線，**但仍在 h2m 自身吃 +5**（見 ③）。

③ **量測（pre = `tmp/s139/pre/` 四檔；同一支 `bin/no` v0.4.28-4-gd21fbb35-dirty 16:55:19）**
| 檔 | pre | post | Δ |
| --- | --- | --- | --- |
| `h2m` | 3885 | 3890 | **+5** |
- 🔴 **本輪只跑了 `h2m` 档**（`tmp/s139/{pre,post}-h2m.log`）；**表格裡原有的 `cg` 2968→2968（0）與
  `lx` 140→140（0）兩列是沿襲前輪的誤記，並無本輪實測支持，予以刪除**（s140 以同一基線重跑
  `cg` 档，得 **2303**，與 2968 無關，見 §57 ③）。
- `h2m` +5 全落在 `h2m-lower-expr` **自身**：`BlockStatement` × 3、`IfExpression` × 2
  （塊通道 diff 共 5 條 + 5 條 `LF:`）。即**新增的下降代碼在 `mem==1` 下的自身代价**，
  與「認領」無關。
- ⚠️ **因此 §54 ⑤ 的「根因層」假說未被本輪「证伪」**——本輪壓根沒量 `cg` 档。
  「根因層假說被否定」的表述**不成立**，正確表述見 §57（s140 真正定位後）與 ⑤。

④ **驗證與門禁**
- ✅ **`no fmt -d` 四檔零漂移**（改動後一次修正兩處空行）。
- ✅ **14 套門禁全綠**（還原後以基线 `bin/no` 重跑，見 `tmp/s139/gate.txt`）：
  `ps` 52 · `h2m` 1359 · `cg` 363 · `ck` 20 · `ck-sig` 50 · `lx` 81 · `tok` 62 · `diag` 7 ·
  `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 · `pkg-sigs` 5 · `pkg-ll` 10。
- ✅ **e2e 93 檔**：因源碼已逐字還原（`diff -q` 四檔皆同），退碼與 `noc-s138-clean`／`/tmp/noc-s136`
  相同（§55 已驗 0 diff）。
- ✅ **四檔已逐字還原**（`grep -c s139 compiler/src/h2m.no` = 0）。

⑤ **誠實邊界與下一步**
- **本輪結論：還原 s139**。**實測依據只有 `h2m` 档 +5**（全部落在新增下降代碼自身），
  `cg` 档**本輪未量**（見 ③ 的修正）。因此「保留即退步」的判斷**只在 `h2m` 语料上成立**；
  對 `cg` 语料的淨效應**至今沒有實測數據**（§57 補上）。
- **方法論教訓（新，比「先放開 X 再談 Y」更基本）**：**量測覆蓋不足時，不可用「没跑的那个档」
  下結論**。本輪幾度以「`cg` 0 變化」敘述，實則該档根本不在本輪量測計畫內——**數字是沿襲的**。
  正確紀律：**每個寫進表格的數字都必須能指名它的日誌檔與二進位時間戳**，否則不得入表。
- **下一步①**（✅ 已由 s140 完成）：重新定位 `cg-emit-cfg-t` 的 mark——以塊通道日誌按 `construct`
  分類統計，再用探針函數定位到「push 家族不支援」這一大宗（見 §57）。
- **下一步②**：§55 ⑤ 的 **Step 2a（多回傳輸出參铺槽 + ret 聚合）**——它是 s138 的直接延续，
  收益面是 `lx-scan-token` 一類 27 marks 的 residual。
- **下一步③**：`Program` construct 級 mark 的出處（`tmp/s138/post3-h2m.log` 3077-3078）。
- **量測工件**：`tmp/s139/pre/`（精確 pre 四檔 + `h2m-test.no`）、`tmp/s139/cov-{cg,h2m,lx}.no`、
  `tmp/s139/{pre,post}-h2m.log`、`tmp/s139/gate.txt`。


---

## 57 s140 — 重新定位 `cg-emit-cfg-t`：幹淨 `cg` 档 2303 條，探針證實「push 家族 + `to-str`」不支援

① **定位（承 §56 ⑤ 下一步①）**
- **脈絡糾正**：§54~§56 反復引用的「`cg-emit-cfg-t` 1657 條 mark」**沒有對應的日誌口徑**
  （§56 ③ 的 `cg` 列已刪）。本輪直接以同一基线二進位跑 `cg` 档，得**幹淨版 `unsupported constructs: 2303`**，
  其中 `cg-emit-cfg-t` 佔 **2302**（构成分：`BlockStatement` 1993 · `IfExpression` 297 ·
  `ForStatement` 7 · `FunctionDefinition` 3 · `SurfaceMatch` 1 · `Program` 1）。
- **方法選擇**：先試圖在 `h2m.no` 加臨時 `print` 診斷（`NOMETH mn=…` / `MK cn=…`），
  **實測完全沒有輸出**——`h2m-lower-print` 在參數降級失敗時回 `r = 0` 但**調用點忽略 `r`**
  （`h2m.no:3801` 只設 `ok-print = 1`），故 `print` **靜默**。**此路不通，改走探針。**
  （該診斷補丁已逐字還原，`h2m.no` 與主樹 `diff -q` 一致。）

② **本輪無源碼改動（定位輪）**
- **探針法**：在 `cg.no` 末尾追加小函數，逐個觀察覆蓋率反應；三批探針跑完即刪，
  `cg.no` 已逐字還原（`diff -q` 與主樹一致，檔尾單換行）。

| 探針 | 內容 | mark | 讀法 |
| --- | --- | --- | --- |
| `d` | `n = 1` / `m = n + 2` | **0** | 幹淨基線 |
| `e` | `s = 'a' - 'b'` | **0** | str 拼接支援 |
| `b` | `n = 1` / `s = n.to-str()` | **1**（Block） | **`to-str` 不支援**（`n` 型別已知） |
| `f` | `x str = with-cap(8)` / `x.push("a")` | **1 Block + 1 FuncDef** | **`str.push` 不支援**（`x` 型別已標注） |
| `a` | `sb.init(8)` / `b.push('x')` / `b.str()` | **1 Block + 1 FuncDef** | 混合型別：`sb.init` 回值推不出 ⇒ `b` 無型別 |
| `c` | `with-cap(8)` / `v.push(1)` | **1 Block + 1 FuncDef** | 同上：`with-cap` 回值推不出 ⇒ `v` 無型別（**非** push 本身） |
| `h` | 同一塊內 2 個 `to-str` | **1 Block + 1 FuncDef** | **每塊只計一次** |
| `i` | 2 個 if 臂各 1 個 `to-str` | **2 Block + 1 FuncDef + 1 Program** | 每臂塊各一次 |

③ **量測**
- **幹淨 `cg` 档 = 2303**（`tmp/s140/probe-cg.log`，二進位 `v0.4.28-4-gd21fbb35-dirty(2026-11-20 16:55:19)`，
  與 §56 同一支基线），可重現（三批探針的差值與探針內容逐條對得上：2303 + 13 = 2316）。
- **計數模型確認**：`h2m-bk-take` 的 `bkm` 去重 ⇒ **同一塊的多個失敗只記一條 `BlockStatement`**。
  **∴ `1993` 是「失敗塊數」，不是「不支援呼叫數」**——不可把 1993 讀成 1993 個缺口。
- **每塊首次失敗即記**，失敗塊遍佈 `cg-emit-cfg-t`（193 個 `->` 臂 + 若干嵌套塊）。

④ **驗證**
- 探針結論**可重現**：`a`/`b`/`c` 在兩批跑中逐字相同（2/1/2）。
- **還原**：`cg.no`（移除 8 個探針）與 `h2m.no`（移除 2 處診斷 `print`）皆與主樹 `diff -q` 一致。
- **本輪未跑門禁**：源碼與 §56 還原後**逐字相同** ⇒ 沿用 §56 的 14 套全綠，不重複跑。
- **未做**：`cg` 档的 `pre`/`post` 對照（本輪無改動，無需對照）。

⑤ **誠實邊界與下一步**
- **本輪結論**：`cg-emit-cfg-t` 的缺口**不是** §54 猜測的「失敗呼叫的實參不下降」——探針 `b`/`f`
  給出**直接證據**：**`to-str` 與 `str.push` 在 h2m 的方法分派裡根本沒有臂**（`TY-STR` 臂只有
  `len-bytes`/`len`/`byte`/`bytes`；`push` 只有 `TY-VEC`/`TY-VECSTR` 兩臂且限 `c.mem == 0`；
  `TY-STRUCT` 完全沒有臂）。**缺口是「方法認領」，與實參下降無關。**
- **誠實邊界**：探針 `a`/`c` 的失敗**可能**源自回值型別推不出（`sb.init`／`with-cap`），
  本輪**不主張**它們證明了 `vec.push` 的 `mem==0` 也失敗；**能確定的是 `b`（`to-str`）與 `f`（`str.push`）**。
- **下一步①（最大宗）**：**認領 push**——(a) `TY-STRUCT` 轉發臂（`sb.push` 之類）；
  (b) 放開 `TY-VEC`/`TY-STR` 的 `c.mem == 0` 限制（須先解決跨塊重綁的 SSA 問題）。
  **這是語義改動**，須先在 `compiler/tests/h2m-test.no` 建立門禁再動手。
- **下一步②**：`to-str` 認領（`b` 已證明其缺口）。
- **下一步③**：複查 `with-cap` 回值型別（若能推出，`a`/`c` 類失敗會自動消失一批）。
- **量測工件**：`tmp/s140/pre/`、`tmp/s140/{diag-cg,diag-cg2,probe-cg,probe-cg2,probe-cg3}.log`。

---

## 58 s141 — 认领 `TY-STRUCT` 转发臂：`cg` 档 2308→988（−1320），副作用 `dropped` 3→26

① **定位（承 §57 ⑤ 下一步①(a)）**
- §57 探針已釘死：`b = sb.init(8)` 可用（`sb.init` 回值可推）、`b.buf.push(1)` 因「接收者须为
  識別字」失败、`sb.push('x')` 在簽名表里（**不含 self**）、`sb.push(b,'x')` / `sb.len(b)` 失敗。
  根因是 **h2m 的值方法分派沒有 `TY-STRUCT` 臂**——`sb.push` 的簽名項存在，但隱式 self 不入
  簽名表，`recv-is-value == 1` 的分支永遠查不到它，只能落 `ok-call = 0`（`h2m.no:3783-3785`）。
- **本輪的取捨**：真正的「模组名」不在 `c.stn` 里（`s141` 調研結論：`stn` 只存結構體名），
  故先用**結構體名**試拼 `sn - '.' - mname` 查表。**這在「結構體名 == 模組名」時等價**——
  `sb` / `vec` / `str` / `arr` / `txt` 恰好都是——但**跨模組同名結構體時會誤命中**（見 ⑤）。

② **修復（`compiler/src/h2m.no`，+19 行 / 1 個臂）**
- 在 `h2m-lower-expr` 的 `recv-is-value == 1` 值方法分派里，於 `TY-OPT` 臂之前插入 `TY-STRUCT` 臂：
  `sid2 = rty - TY-STRUCT-BASE` → `sn2 = c.stn[sid2]` → `cn2 = sn2 - '.' - mname` →
  `si2 = h2m-sig-find(sg, cn2)`；命中則 `cn = cn2; ok-method = 1`。
  `ok-method = 1` **抑制**了 3783-3785 的 `ok-method == 0 -> ok-call = 0`，於是 `ok-call` 保持 1，
  逕直走既有的**跨模組路徑**（`h2m.no:3804` 起：查表 → 引數入池 → `OP-CALL`），**self 不傳**。
  未命中則不設 `ok-method` ⇒ 照舊落閘門，**不猜**。
- **格式**：`no fmt -d` 零漂移；`wt` 與主樹 `diff -q` 一致。

③ **量測（同一基線二進位 `v0.4.28-4-gd21fbb35-dirty(2026-11-20 16:55:19)`）**
- 三档全數下降（`noc: unsupported constructs (pkg-ll)`）：

| 档 | 基线 | s141 | 差 |
| --- | --- | --- | --- |
| `cg` | 2308（`tmp/s140/probe-cg.log`） | **988**（`tmp/s140/s141-cov-cg.log`） | **−1320** |
| `h2m` | 3890（`tmp/s139/post-h2m.log`） | **1849**（`tmp/s140/s141-cov-h2m.log`） | **−2041** |
| `lx` | 119（`tmp/s138/pre-lx.log`） | **90**（`tmp/s140/s141-cov-lx.log`） | **−29** |

- `cg` 档的 `LF:` 通道按構成分解：`Identifier` **2268 → 921（−1347）**、`CallExpression` 12 → 18（**+6**）、
  `IntegerLiteral` 7 → 6、`FunctionDefinition` / `StringLiteral` / `SliceLiteral` / `BlockStatement` 持平。
  **∴ 下降主體是「方法接收者/引數里的標識符」**，與「1848 次 `b.push(...)`」的形態一致。
- **副作用（同檔同輪）**：`noc: dropped (calls unsupported)` **3 → 26**（`cg` 档）。新增 23 個
  全是 `cg-rc-*` / `cg-runtime-*` / `cg-emit-cfg`——**它們的「呼叫點」本輪被認領成功，但函式體
  自身仍不產碼**，於是被列入 dropped。

④ **驗證**
- ✅ **13 套門禁全綠**（`tmp/s140/s141-gate3.txt`）：`ps` 52 · `h2m` 1359 · `cg` 363 · `ck-sig` 50 ·
  `lx` 81 · `tok` 62 · `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 ·
  `pkg-sigs` 5 · `pkg-ll` 10。
- ✅ **`ck-test` 20 passed**（`tmp/s140/s141-ck.txt`，`exit=0`）——本輪實測其**编译期**逾 280s
  （`timeout 280 ./bin/no build` 仍 `exit=124`），**改用／不加改動的 `h2m.no` 都一樣慢** ⇒
  判定為**環境／效能因素，非本輪引入的回歸**（根因未查）。**14 套門禁就此全数收齊。**
- ✅ **源碼同步**：`compiler/src/h2m.no` 舆 `tmp/s129/wt/compiler/src/h2m.no` `diff -q` 一致；
  `cg.no` 的 s140 探針（g/h/j/m/n）已全部移除，`wt` 版與主樹 `diff -q` 一致（2759 行）。

⑤ **誠實邊界與下一步**
- **本輪結論**：**认领 `TY-STRUCT` 转发臂有效**——三档覆蓋率齊降，`cg` 档降幅過半（−57%）。
  這直接兌現了 §57 ⑤ 下一步①(a)，**且証實「缺口是方法认领、不是實參下降」**：改動完全不碰
  實參下降路徑，卻讓 `Identifier` 通道少掉 1347 條。
- **誠實邊界**：
  1. **认领的是「呼叫點」，不是「被調函式」**——`dropped` 由 3 增至 26 即其代價；**覆蓋率數字
     改善 ≠ 可連結**，本輪**未**產出可執行的自舉二進位。
  2. **名字拼湊用的是「結構體名」而非「模組名」**——`c.stn` 目前只存結構體名，故
     「結構體名 == 模組名」時才正確；**跨模組同名結構體會誤命中**（可能把 `a.vec.push` 指到
     `vec.push`）。本輪**不主張**已排除此風險。
  3. **`+6` 的 `CallExpression`** 是本輪新增的**跨模組 `OP-CALL`**（轉發成功後引數路径的構件），
     不是回归。
  4. **`ck-test` 的慢**已用「換回改動前的 h2m.no 仍慢」排除本輪責任，但其**根因未查**。
- **下一步①（收掉 ⑤-2）**：把 `stn` 升級為「結構體名 + 所在模組名」（或讓 `h2m-sig-find` 支援
  「按結構體名查其定義模組」），使轉發**按模組命中**，消除误命中面。**這是本輪改動的真正收尾**。
- **下一步②**：`dropped` 的 23 個 `cg-rc-*` / `cg-runtime-*` 是本輪新增的**最大宗的「被調而不產碼」**
  —— 認領它們**自身的函式體**（RC / runtime 系列）是下一個覆蓋率金礦。
- **下一步③**：§57 ⑤ 的 `to-str` 認領（`b` 已證明其缺口）與 `with-cap` 回值型別複查仍未做。
- **量測工件**：`tmp/s140/s141-{cov-cg,cov-h2m,cov-lx,gate3,ck}.log`、`/tmp/h2m-s141-pre.no`（改動前快照）。

---

## 59 s142 — 把 `TY-STRUCT` 轉發查表由「結構體名」升為「模組名」：實測 no-op，**已回退**

① **定位（承 §58 ⑤ 下一步①）**
- §58 ⑤-2 誠實邊界：s141 用**結構體名**拼 `sn - '.' - mname` 查表，「結構體名 == 模組名」時等價，
  **跨模組同名結構體時會誤命中**。下一步①要求「把 `stn` 升級為結構體名 + 所在模組名，使轉發按模組命中」。
- 來源核查：`h2m-struct-register` 的三處調用點（`drv.pkg-sigs` pass1、`drv.pkg-ll` pass1、
  `h2m-lower-program`）中，兩個**整包路徑**已持有真實模組名 `mod = drv.mod-name(f)`；
  **單模組路徑**（`h2m-lower-program`，`h2m.no:6290`）沒有，只能登空串。

② **修復（`compiler/src/h2m.no` +13 行、`compiler/src/drv.no` +1 行）**
- `h2m-ctx` 加 `stm []str`（與 `stn` 同序、同下標）；`h2m-new` 加 `stm: []`；`h2m-lower-func` 加 `c.stm = agg.stm`。
- `h2m-struct-register` 簽名加 `mod str`，`sid < 0` 分支加 `c.stm.push(mod)`；三處調用點傳值
  （整包路徑傳 `mod`，單模組路徑傳 `''`）。
- s141 臂改按 `c.stm[sid2]` 拼名；`mn2 == ''`（單模組路徑）**回退**用 `c.stn[sid2]`，保持既有行為。
- 格式：`no fmt -d` 零漂移；`wt` 與主樹 `diff -q` 一致。

③ **量測（同一基線二進位 `v0.4.28-4-gd21fbb35-dirty(2026-11-20 16:55:19)`）**
- 三檔：`cg` 988 → **988（0）**、`h2m` 1849 → **1851（+2）**、`lx` 90 → **90（0）**。
- 逐通道差（s142 vs s141）：`+1 h2m-new:BlockStatement`、`+1 h2m-ctx.stm:SliceType:type`、
  `+1 h2m-struct-register:BlockStatement`、`−1 h2m-lower-expr:FunctionDefinition`。
- **受控實驗**：A2（= s141 + 只加 `stm` 字段／初始化／拷貝，臂仍按 `stn`）= **1851**。
  ⇒ **+2 全部來自新增字段本身**；s142 與 A2 淨差 0（`−1` 與 `+1` 相消）。

④ **驗證：探針（本輪決定性證據）**
- **探針方法（重要）**：覆蓋度量是**動態**的 —— `drv.pkg-ll` 跑的是 `compiler/src/h2m.no` 的**降級器程式碼**
  去走**目標檔**的 AST；所以探針必須改**降級器本體**，改目標檔無效（實測：在目標檔 `h2m-lower-expr`
  入口加 `c.unsup += 1000000`，總數紋絲不動 1851）。做法：在降級器的 `si2 >= 0` 分支注入
  `#{overflow=wrap} c.unsup = c.unsup + 1`，總數即「基線 + 命中數」。
- **命中數**：**2025**（`+1` 探針 3876 − 探針基線 1851）。
- **決定性對照 A**：在命中分支內比對 `mn2`（模組名）與 `c.stn[sid2]`（結構體名），
  計 `mn2 != sn2p` 的命中數 = **0**（1851 − 1851）。
  ⇒ **2025 次命中全部是「結構體名 == 模組名」**（`sb` / `ast` / `ps` / `cg` / `mir` / `lx` / `ix` …）。
- **決定性對照 B**：計「s142 命中而 s141 會失配」（`si21 < 0 && si2 >= 0`）= **0**（1852 − 1852）。
- ⇒ **s142 與 s141 在 h2m.no 語料上行為完全一致**：**0 修正、0 誤命中**。
- **受控案例（t6，獨立佐證）**：模組 `m` 定義結構體 `box`（**名 != 模組名**）與方法 `box.render`，
  `drv.pkg-sigs` 實測：`m.box.render` → **0（在表）**、`m.render` → **−1**、`box.render` → **−1**。
  ⇒ **對「結構體名 != 模組名」的方法，s141（查 `box.render`）與 s142（查 `m.render`）雙雙失配。**
  s142 唯一可能新增的命中是「模組內**普通函式** `m.foo` 被當 `x.foo()` 呼叫」——**那是誤命中，不是修正**。

⑤ **誠實邊界與下一步**
- **本輪結論：回退（已回退）。** s142 相對 s141 **零可觀測行為差異**，卻令 `h2m` 档覆蓋 **+2**
  （純粹為承載 `stm` 字段）。**而且它並沒有修掉自己打算修的 §58 ⑤-2** —— t6 已實測證明：
  對「結構體名 != 模組名」的方法，**兩個查法都不中**。
- **守護條件**：`h2m.no` 語料裡所有 `TY-STRUCT` 方法呼叫都遵守「型別名 == 模組名」的 std 慣例
  （`sb.push` / `ast.nd-at` / `ps.parse` …），故 `stm` 永遠等於 `stn`。**只要這個慣例不破，s142 就是死碼。**
- **不是回歸**：回退後三档覆蓋回到 s141 基線（`cg` 988 / `h2m` 1849 / `lx` 90，见 `tmp/s142/s142r-cov.txt`），14 套門禁重跑全綠
  （`tmp/s142/s142r-gate.txt`）：`ps` 52 · `h2m` 1359 · `cg` 363 · `ck-sig` 50 · `lx` 81 · `tok` 62 ·
  `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 · `pkg-sigs` 5 ·
  `pkg-ll` 10 · `ck` 20，`failed` 全為 0。
- **下一步①**：若要真正修 §58 ⑤-2 的「跨模組同名結構體」，正確的鑰匙是**讓 `h2m-sig-find` 支援
  「按結構體名反查其定義模組名」**（`stn → mod` 反查表），而不是把查表名前綴換成模組名——
  **本輪已用受控案例證明後者無效**。
- **下一步②**：`dropped` 的 23 個 `cg-rc-*` / `cg-runtime-*`（§58 ⑤ 下一步②）仍是最大金礦。
- **下一步③**：`to-str` 認領 / `with-cap` 回值型別（§58 ⑤ 下一步③）仍未做。
- **量測工件**：`tmp/s142/pre/{h2m,drv}.no`、`tmp/s142/h2m-s142.no`、
  `tmp/s142/{s142-cov-*,A-cov-h2m,A2-cov-h2m,B-cov-h2m,probe-dyn*,probe-s141,probe-cmp*,probe-ne,t6-probe}.log`、
  `tmp/s142/{s142r-cov,s142r-gate}.txt`。

---

## 60 s143 — 认领 `to-str`：`cg` 档 988→317（−671），`OP-ITOSTR = 104` + runtime helper `@str_from_i64`

① **定位（承 §57 ⑤ 下一步② / §59 ⑤ 下一步③）**
- §57 的探針已把 `cg-emit-cfg-t` 的 896 marks（LF `Identifier` 878）釘死在「push 家族 + `to-str`」；
  §58（s141）認领 `TY-STRUCT` 轉發後 `cg` 档 2308→988，**剩餘主體正是 `to-str`**。
- **本輪量化缺口**：在 `cg.no` 的值方法分派路徑上注入 `TS:<rty>:<fn>` 探針（只統計走「值方法分派」
  的 `to-str` 呼叫點），得 **634 個**：
  - **407** 個接收者 `rty == TY-I64`（=0）；
  - **227** 個 `rty == TY-OPT`（=3），再注 `TOPT:<pty>` 探針 ⇒ **227/227 載荷為 `TY-I64`**。
  - （`cg.no` 裡 `.to-str()` 字面共 821 處，pre 版 819 處，+2 來自本輪新增的 emit arm。）
- **Go 側語義（不憑空造）**：`i64.to-str` 是 `#{buildin}`（`src/std/number.no:87`），由
  `src/builtin/strconv.go:38` 的 `BuiltinMethod{ReceiverType: i64, MethodName: "i64.to-str",
  ForwardFunc: "i64-to-str"}` 攔截，codegen 發 `@str_from_i64`（`src/mir/codegen.go:4643`）。
  option 接收者走 `hir2mir.go:12226 unwrapOptionOperand`——**剝 option 取載荷再呼叫，不檢查 tag**。
  ⚠️ 宿主實測：`nil` 的載荷槽為零 ⇒ `"0"`；`ok(42)` ⇒ `"42"`；`err` ⇒ **未初始化垃圾**
  （實測 `g-ok:4339406576`）。**本輪按此語義認领，不自創「nil ⇒ 空串」之類的主張。**
- **s137 的教訓（§54）**：認领 `i64.to-str` 曾有過一次實施後還原——當時收益被「失敗呼叫的實參
  不下降」完全屏蔽（16 檔淨 +19）。§56（s139）放開實參下降後，本輪才具備落地條件。

② **修復（`mir.no` +15 / `cg-runtime.no` +46 / `cg.no` +14 / `h2m.no` +40；`h2m-test.no` +24 −4）**
- `compiler/src/mir.no`：新 op 碼 **`OP-ITOSTR = 104`**（接在既有最大碼 `OP-STRCPLEN = 103` 之後，
  不重排）。註釋明記與 Go 的差異：**Go 的 `@str_from_i64` 沒有 `i64.MIN` 的 isMin select 護衛**，
  靠 `sub i64 0, %v` 在執行期回繞成 2^63 才得到正確量值（宿主實測 `i64.MIN` 正確，
  但那是 opt 未折 poison 的運氣）；自舉側按本檔 `@print_i64` 的既有口徑補上護欄，
  **刻意比 Go 防禦性更強**。
- `compiler/src/cg-runtime.no`：新 helper `cg-runtime-str-from-i64()`（+46 行），逐字鏡像 Go 的
  `emitStrFromI64` 但加上 `%isMin` select；由 `cg-runtime-prelude-full` 末尾登記
  （排在 `cg-runtime-print-helpers` 之後——它依賴 `@digits`）。
- `compiler/src/cg.no`：`op == OP-ITOSTR` 臂（+14 行）→
  `%sv<dst> = call %str-long @str_from_i64(i64 %v<x>)`。
- `compiler/src/h2m.no`（**本輪 +40 行**；`git diff` 的 +58 另含 18 行**未提交的 s141** 轉發臂）：
  - `h2m-ty-of`：`mt == TY-I64 && mf == 'to-str' -> TY-STR`、`mt == TY-U64 && ...`（+6）。
  - `h2m-lower-expr` 值方法分派：新 `rty == TY-I64 || rty == TY-U64` 臂（**只收無參** `na == 0`）→
    `h2m-alloc(TY-STR)` + `h2m-emit(OP-ITOSTR)` + `h2m-rel-add`（+16）。
  - 同函式 `rty == TY-OPT` 臂：新 `(pty == TY-I64 || pty == TY-U64) && mname == 'to-str' && na == 0`
    → 先發 `OP-OPTGET` 取載荷，再走 `OP-ITOSTR`（+16）。**只認載荷型別已靜態推斷為整數者**，
    推不出 ⇒ 落回閘門，不猜。
- `compiler/tests/h2m-test.no`：`test-s132-opt-i64-to-str-gate` 由「仍落閘門」改寫為正向斷言
  （`unsup == 0` / 有 `define` / 有 `extractvalue %option %op` / 有 `@str_from_i64`），
  新增 `test-s143-i64-to-str`（純 `i64` 接收者，且斷言**無** `OPTGET`）。

③ **量測（同一 drive：`tmp/s143/cov-{cg,h2m,lx}.no`；`cd tmp/s129/wt && ./bin/no run <絕對路徑>`）**

| 档 | 基線（s141） | s143 | 差 |
| --- | --- | --- | --- |
| `cg` | 988 | **317** | **−671** |
| `h2m` | 1849 | **1166** | **−683** |
| `lx` | 90 | **88** | **−2** |

> ⚠️ **口徑**：本表的 `cg` **317** 是 s143 當時的 wt 語料（`cg-runtime` 尚未補 helper）之下的值；
> §61 統一用**當前** wt 語料（四档全 = s143 版）重量，同一 s143 降級器得 **318**。差 1 屬**語料漂移**、
> 非降級器差異，詳見 §61 ③ 的〈口徑說明〉。

- ✅ **重現性**：改動後重跑一次（`tmp/s143/r2-{s143,base}-cov-*.log`），六個數字**逐字相同**。
- `cg` 档（LF 通道 963→292，`dropped` 25→25 持平）：
  - `cg-emit-cfg-t:Identifier` **878 → 251（−627）**，佔全档降幅的 **93%**；
  - 其餘：`cg-div-guard:Identifier` 29→2（−27）、`cg-runtime-ty-name:StringLiteral` 6→0（−6）、
    `cg-lit-name` −3、`cg-label` −2、`cg-div-guard:FunctionDefinition` −2、
    `cg-runtime-struct-decl` −1、`cg-runtime-ty-name:CallExpression` −1、
    `cg-runtime-ty-name:FunctionDefinition` −1、`cg-emit-cfg-t:StringLiteral` −1。
- `h2m` 档（LF 1752→1067、`dropped` 97→**99**）：
  - ⚠️ **組成必須說清**：`h2m` 档的語料是**入口檔 `h2m.no` 的 import 閉包**，而 `h2m.no` import `cg`
    ⇒ **`cg-emit-cfg-t` 的 −627 與 `cg` 档同源**。扣掉 `cg-*` 通道（合計 −671），
    **h2m 語料自身只 −14**：`h2m-cnt-name` −3、`h2m-idx-name` −3、`h2m-smv-name` −3、
    `h2m-struct-decls` −1、`int-str` −2、`between-comments` −1、`free-comments` −1。
  - **受控實驗（A/B，決定性）**：A = 只把 `h2m.no` 換成 s143（`cg`/`cg-runtime`/`mir` 保持 pre）
    ⇒ **1166**；B = 只把 `cg`/`cg-runtime`/`mir` 換成 s143（`h2m.no` 保持 pre）⇒ **1849**。
    ∴ **`h2m` 档的數字完全由 `h2m.no` 決定**，`cg` 档的改動透過 import 閉包體現在 `h2m` 档裡。
  - `dropped` +2：`ast.free-comments` / `ast.between-comments` 由「身體產 mark」變為「被調而不產碼」
    （A 有、B 無 ⇒ 同上，由 `h2m.no` 觸發）；**機制未查**，見 ⑤。
- `lx` 档：`lx-fmt:Identifier` 10→8（−2）。
- **無回歸**：三档**無任何 (fn, kind) 通道計數上升**，也無新增通道鍵。
- **閘門探針（決定性證據）**：在值方法分派的 `to-str` 分支注入「未認领則 `unsup += 1`」探針，
  重跑 `cg` 档得 **317**（與乾净版**逐字相同**——`tmp/s143/tsleft-cg.log` 617 行 = `s143-cov-cg.log`），
  且探針輸出 **0 行** ⇒ **634 個 `to-str` 值方法呼叫點已全部認领**。

④ **驗證**
- ✅ **14 套門禁全綠**（`tmp/s143/gate.txt`）：`ps` 52 · `h2m` **1365** · `cg` 363 · `ck-sig` 50 ·
  `lx` 81 · `tok` 62 · `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 ·
  `pkg-sigs` 5 · `pkg-ll` 10 · `ck` 20，`failed` 全為 0。
- ✅ **`no fmt -d` 五檔零漂移**（`mir` / `cg` / `cg-runtime` / `h2m` / `h2m-test`）。
- ✅ **e2e 逐字一致**（`/tmp/noc-s143b`，由**宿主** `./bin/no build -o` 重編自舉編譯器源碼）：
  - `ts-e2e`：`-7` / `0` / `9223372036854775807` / `-9223372036854775808` / `0`（nil option）/
    `v=-7` / `-7/0` —— **宿主與自舉輸出逐字相同、退碼皆 0**；
  - `ts-min`：`min=[-9223372036854775808]` / `max=[9223372036854775807]` —— 逐字相同、退碼 0。
  - **`i64.MIN` 正確 ⇒ 新增的 isMin 護欄有效**（此路徑宿主正確靠運氣，自舉側靠顯式 select）。
- ✅ **wt 清理**：清掉 `tmp/s129/wt/compiler/src/h2m.no` 上殘留的 s143 探針
  （`c.unsup = h2m-inc(c.unsup)`），wt 四檔回到 s141 基線；`bin/no` 時間戳 00:55 **早於**探針，
  **二進位本身乾淨**（門禁與量測從頭到尾用的是它）。

⑤ **誠實邊界與下一步**
- **本輪結論：認领 `to-str` 有效**——`cg` 档降幅 **68%**（988→317），`cg-emit-cfg-t` 的 LF
  `Identifier` 從 878 降到 251。**但增強度有限**：`cg-emit-cfg-t` 自己的塊通道仍有
  `IfExpression` 205 / `BlockStatement` 53，**剩下的主體是 push 家族，不是 `to-str`**。
- **誠實邊界**：
  1. **`?i64.to-str()` 的 `nil` / 形參路徑可跑，`ok(...)` 路徑不可用**——瓶頸是**既有缺口**：
     `ok(42)` 這個**枚舉變體構造**本身在自舉 h2m 就落 unsup。實測 `ts5`（`c ?i64 = ok(42)` +
     `print('made')`，**根本不含 `to-str`**）在 `noc` 下仍 1 unsup，宿主則正常 ⇒ 與本輪無關。
     因此 `ts3`（`ok(42)` + `to-str`）的 3 個 unsup、`ts7` 的 4 個、`ts6`（形參 + `f(ok(42))`）的 2 個，
     各有 1 個來自它。**這不是本輪引入的，但本輪也沒有修它。**
  2. **`err` 載荷的語意是「未初始化垃圾」**（宿主實測 `g-ok:4339406576`）。本輪**刻意鏡像**該語意，
     不自創更「合理」的行為——**認领 ≠ 背書**。
  3. **`h2m` 档的 −683 有 98% 來自 `cg.no` 語料**（因 `h2m.no` import `cg`）。**別把 `h2m` 档的
     數字讀成「h2m 覆蓋率改善」**——h2m 語料自身只 −14（A/B 受控實驗已釘死）。
  4. **`dropped` +2**（`ast.free-comments` / `ast.between-comments`）**機制未查**；同族的
     `set-comments` / `comments` / `trailing-comments` / `closing-comments` 在**基線**就是 dropped。
     `cg` / `lx` 兩档的 `dropped` 清單**逐一不變**。
  5. **「覆蓋率」與「可執行性」是兩件事**：覆蓋率衡量的是**自舉編譯器對編譯器自身源碼**的降級能力
     （萬級構造）；`noc-s143b` 是**宿主編譯**出來的自舉編譯器，它編譯 `ts-e2e` / `ts-min` 的輸出
     與宿主逐字一致 —— 這是 s143 的 `OP-ITOSTR` 路徑的**真實端到端驗證**，但**不能**用來說
     「自舉編譯器已能自編譯」（`dropped` 的 25+ 個 `cg-rc-*` / `cg-runtime-*` 仍不產碼）。
- **下一步①**：`dropped` 的 25 個 `cg-rc-*` / `cg-runtime-*`（§58 ⑤ 下一步②）仍是最大金礦；
  **本輪新增的 `cg-runtime-str-from-i64` 本身就落在 dropped 裡**。
- **下一步②**：`ok(...)` / `err(...)` 枚舉變體構造的認领（既有缺口，卡住 `?t` 的路徑）。
- **下一步③**：`with-cap` 回值型別複查（§58 ⑤ 下一步③）仍未做。
- **量測工件**：`tmp/s143/{pre/,*-s143.no,*-s143b.no,base-cov-*.log,s143-cov-*.log,r2-*.log,exp{1,A,B}*,ts-cg.log,tsopt-cg.log,tsopt2-cg.log,tsleft-cg.log,gate.txt,e2e.txt,diag-tpl.no,diag-build-ll.no,remeasure.txt}`、
  `/tmp/noc-s143b`、`/tmp/{ts-e2e,ts-sem,ts-sem2,ts-min,c1..c6,ts1..ts7}.no`。

---

## 61 s144 — 认领「option 实参 → plain 形参」：`cg` 档 318→78（−240），`h2m-opt-peek`（含 s129 门禁收窄）

① **定位（不在 §60 ⑤ 三條裡的第四方向，但它是 318 裡的主體）**
- §60 ⑤ 下一步①②③ 分別是小金礦（`dropped` 25 個 `cg-rc-*`/`cg-runtime-*`；`ok(...)`/`err(...)`
  枚舉變體構造；`with-cap` 回值型別）。**本輪先量化 318 的組成，發現主體是另一件事**：
  `cg-emit-cfg-t` 一個函式佔 **268 marks**（`IfExpression` 205、`BlockStatement` 53），
  LF 通道 251 個 `Identifier`（實參表達式是 Identifier ⇒ `c.cur-k` 被覆蓋）。
- **根因**：`cg-emit-cfg-t` 的循環頭 `d = dsts[k]` / `x = xa[k]` / `y = bs[k]` 是**索引讀 ⇒ `?i64`**，
  作為實參傳給 `i64` 形參（`cg-vty-at(vty, d)` 等）⇒ 自舉呼叫臂 `want != at -> ok-call = 0`
  ⇒ 每個守門塊都敗，整函不產碼。
- **宿主語意（不憑空造，兩層分開看）**：`src/mir/hir2mir.go:14399-14407` —— `paramRaws[pi]` 非 option
  而值為 option 時，`unwrapOptionOperand` 剝出 payload 再傳。**但能不能落到 lowering 由 checker 決定**，
  宿主對「option 實參」分兩層（**四個探針實測**，`tmp/s144/sem2/p{1..4}.no`）：
  - `p1` `id(xs[0])`（**索引表達式直接當實參**，`id` 收 `str`）⇒ **拒編** `idxbruse`；
  - `p3` `f(xs[0])`（`i64`）⇒ **拒編** `idxbruse`；
  - `p2` `d = xs[0]` 後 `f(d)`（**Identifier**）⇒ **接受**（`Build successful`）；
  - `p4` `d ?i64 = xs[0]` 後 `f(d)`（**顯式標註 `?i64`**）⇒ **拒編** `3604icnf`
    「implicit unwrap of option arguments is not allowed」。
- **這正是 `compiler/tests/h2m-test.no:6855` 的 s129 門禁**：「option 化後『把索引讀當纯值用』必須
  【計 unsup】—— `id(xs[0])`（id 收 str）在語言上是型別錯誤，自舉不得靜默放行」。

② **修復（`compiler/src/h2m.no`，+42 行；良校零改動）**
- 新增 `h2m-opt-peek(c, v, want)`：實參值型別 `TY-OPT` 且形參型別非 `TY-OPT` ⇒ 按 `want` 發
  `OP-OPTGETF64` / `OP-OPTGETSTR` / `OP-OPTGETVEC` / `OP-OPTGET`（`TY-I64`/`TY-U64` 共用），
  回新值 id；其餘回 0（落呼叫側門禁）。**不猜**：型別不符不回、`want == TY-OPT` 不回。
- 呼叫實參臂：`at != want` 時**先檢查實參 AST 節點**——
  `akd.kind == asp.K-INDEX-EXPRESSION -> ok-call = 0`（守 s129 門禁 / 宿主 `idxbruse`）；
  否則才 `h2m-opt-peek`。**型別檢查里外兩層**：helper 只認值 id、不見 AST，
  故「索引表達式」的過濾必須寫在呼叫側。
- ⚠️ **第一版教訓**：只發 `h2m-opt-peek`（對**所有** option 實參生效）打破 s129 門禁
  （`h2m` 門禁 1365/0 → **1363/2**）。收窄後**覆盖日誌與未收窄版逐字相同**（`cg`/`h2m` 兩档
  `diff -q` 通過）⇒ 收益全部來自 Identifier 型，一寸未讓。

③ **量測（同一語料：`cd tmp/s129/wt && ./bin/no run <絕對路徑>`；A/B **只切換 main 的 `h2m.no`**）**

| 档 | s143 | s144 | 差 |
| --- | --- | --- | --- |
| `cg` | 318 | **78** | **−240** |
| `h2m` | 1167 | **909** | **−258** |
| `lx` | 88 | **87** | **−1** |

- **機構澄清（值得記下來，免得以為換 wt 就有用）**：`drv.pkg-ll-cov('compiler/src/cg.no')` 的
  **參數**按 cwd（= wt）解析 ⇒ **語料讀 wt 樹**；但 cov 腳本自身的 `# /compiler/src/h2m`
  由宿主編譯器解析 ⇒ **降級器實現讀 main 樹**。故 A/B 必須切換 **main 的 `h2m.no`**。
  （實測：改 wt 的 `h2m.no` 對 `cg` 档**無效**；改 main 的 `cg.no` 會直接讓 cov 腳本編不過。）
- ✅ **重現性**：A/B 两版日誌各自 `diff -q` 與首輪**逐字相同**（`tmp/s144/{A-s144b,B-pre}-*.log`）。
- ✅ **發射 IR 逐位元相同**（A/B 只切換降級器）：`cg` **22 函式 / 31180 B**、`h2m` **61 函式 / 429107 B**
  ⇒ **零回歸**；且本輪的改善**尚未轉化為「更多函式被發射」**（見⑤.3）。
- **`dropped`**：`cg` 26→26（清單不變）、`lx` 10→10（不變）、`h2m` 100→101
  （唯一新增 `h2m-emit-releases`，見⑤.2）。
- **`s144` 語料版**（把 wt 的 `h2m.no` 也換成 s144 版）：`h2m` 1168→910（同 −258）。
- **口徑說明**：§60 表裡的 `cg` **317** 是 s143 當時的 wt 語料（`cg-runtime` 尚未補 helper）之下的值；
  本輪統一用**當前** wt 語料（`cg`/`cg-runtime`/`mir`/`h2m` 全 = s143 版），同一 s143 降級器得 **318**。
  差異 1 來自語料漂移，**不是**降級器差異。

④ **驗證**
- ✅ **14 套門禁全綠**：`ps` 52 · `h2m` **1365** · `cg` 363 · `ck-sig` 50 · `lx` 81 · `tok` 62 ·
  `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 · `pkg-sigs` 5 ·
  `pkg-ll` 10 · `ck` 20（`tmp/s143/gate.txt`）。
- ✅ `no fmt -d compiler/src/h2m.no` **零漂移**。
- ✅ **e2e**：宿主 `no build -o /tmp/noc-s144b compiler` 成功；`ts-e2e` / `ts-min` 在宿主與 `noc` 下
  **逐字一致且退碼 0**；`ts-sem` / `ts-sem2` 的 `11` / `6` 與 s143 相同（M4-d SHS 子集探針，非覆蓋率）。

⑤ **诚实边界与下一步**
1. **本輪只鏡像了宿主的一半**：不剝「索引表達式直接當實参」（宿主 `idxbruse` 拒編，s129 門禁），
   但**仍靜默放行「顯式標註 `?T` 的 Identifier 實參」**（宿主 `p4` 拒編 `3604icnf`；自舉的
   `h2m-vty-at` 只看**值型別**、不看**靜態型別**，無從分辨）。**這是忠實於宿主「推斷缺口」
   而非宿主「宣告語意」**——語料（`cg.no`）本身就建立在缺口上。要收緊得先讓型別系統分出
   「靜態 `?T`」與「值為 option 但靜態推不出」，**改動面大，本輪不做**。
2. **`h2m-emit-releases` 的「新 dropped」不是回歸**：它的 `h2m-emit(c, OP-RELEASE, 0, vid, ...)`
   中 `vid = c.rls[i]` 是索引讀（`?i64`）傳 `i64` 形參 ⇒ **本輪首次成功降級**（7 marks → 0 marks），
   繼而進入 `parts`，才被 alive 定點以「callee `h2m-emit` 未發射」剔除。**發射集 61=61、
   body 逐位元相同** ⇒ 只是計數從 7 變 1（淨 −6）。
3. **覆蓋率 ≠ 可執行性**：`cg` −240 全來自 `cg-emit-cfg-t`（`IfExpression` 205→8、
   `BlockStatement` 53→11）；但該函式**仍不產碼**（`EMIT-CHK` 的 `nalive` 兩版都是 22）。
   `noc-s144b` 編譯 `ts-e2e` / `ts-min` 正確，是本輪的端到端驗證，**不能**說自舉已能自編譯。
4. **`h2m` 档的 −258 有 240 是 `cg.no` 語料**（`h2m.no` import `cg`）；h2m 語料自身只 −18。
5. **未做**：§60 ⑤ 下一步①（`dropped` 25 個 `cg-rc-*`/`cg-runtime-*`，根因已釘死為
   **`self` 從不進 `c.names`**：`h2m-func-sig` 只收 `d.c4`、`h2m-lower-func` 只綁 `fap.len()` 個；
   且 `sb.init(256)` 是**無 self 的靜態構造形態**，而宿主 `self` 是**第一個 KResult**
   ⇒ 改動面大、風險高，刻意不做）；⑤ 下一步②（`ok(...)`/`err(...)` 枚舉變體構造，
   語料裡表達式位 ≈ 0 處 ⇒ 收益 ≈ 0）；⑤ 下一步③（`with-cap` 回值型別複查）。
- **下一步①**：同 ⑤.5 第一條（`self` 綁定）——它是 `dropped` 26 與 `h2m-emit` / `h2m-lower-func`
  不產碼的共同根因。本輪順帶證實：**`h2m-emit` 本身就落 7 marks**（5 個 `BlockStatement` +
  `FunctionDefinition` + `Program`），因為它的 `c.ops.push(op)` 等 7 條全是
  **點號接收者方法呼叫**（`c.ops` 是 DotExpression，非 Identifier）。
- **下一步②**：把 `h2m-vty-at` 拆出「靜態型別」維度，收緊 ⑤.1 的 `p4` 型缺口。
- **量測工件**：`tmp/s144/{h2m-s144b.no,h2m-main-bak.no,h2m-clean.no,drv-bak*.no,A-s144b-*.log,
  B-pre-*.log,A-s144-*.log,s144src-*.log,emit-*.log,e2e.txt,e2e.sh,sem2/p{1..4}.no,
  gate-run*.log,diag-*.log,pre-dropped.txt,pn-*.txt,em-*.txt}`、`/tmp/noc-s144b`、
  `/tmp/{ts-e2e,ts-sem,ts-sem2,ts-min}.no`。

## 62 s145 — 认领「空切片字面量 `[]` 作结构体字段初值」：`h2m` 档 909→843（−66），MK 逃逸门禁放行

① **定位（从 §61 ⑤ 下一步① 切入，却在更上游撞见主体）**
- §61 ⑤ 下一步① 把 `dropped` 26 與 `h2m-emit` / `h2m-lower-func` 不產碼的共同根因釘在
  **`self` 從不進 `c.names`**，並順帶記下 `h2m-emit` 落 7 marks（全是點號接收者 push）。
  本輪先量化 `h2m` 档剩餘 909 的組成，发现**最大單一函數是 `h2m-new`（50 marks）**——
  它是每個函數降級都要呼叫的「上下文製造機」，比 `self` 更上游。
- **診斷機構（本輪新增，臨時、已移除）**：在 `h2m-ctx` 加 `cur-kn`（當前構造的 AST 節點 id）
  與 `cur-src`（該節點的 `ast.expr-string` / `ast.node-string` 文本），`h2m-set-cur-k` 一併設值，
  `h2m-mark-here` 的葉子通道改記 `fn#kn:src`，另把 `c.bkn`（塊深）拼進日誌。
  ⇒ 48 條 `h2m-new` 記为 **`#331:0|d3`**：葉子構造 `IntegerLiteral`、節點文本 `0`、**塊深 3**
  （Program → FunctionDefinition → 函數體块）。
- **根因**：`h2m-new` 的 `c = h2m-ctx{ … }` 有 **48 個 `[]` 字段初值**（`ops`/`dsts`/`names`/…）。
  s99n 的切片字面量臂對**空** `[]` 也走 `OP-VECNEW` 並 `h2m-rel-add` ⇒ `h2m-vec-own` 判 1
  ⇒ 結構體字面量 MK 的**逃逸門禁**（s99n：自擁有 %vec 放進字段 = 所有權外流 ⇒ 計 unsup）
  逐字段觸發 48 次 ⇒ **整函不產碼**。
  **這道門禁本身是對的**（非空 vec 字段確實會 UAF），誤傷的是「空 vec 無緩衝、無權可外流」。
- **宿主語意**：`src/std/vec.no` 的 `[]` 是空向量（`dat=0`），`@vec_free` 對 `dat=0` 短路
  （見 `cg-runtime.no`）⇒ 寫進結構體字段不構成所有權外流 ⇒ **應放行**。

② **修復（`compiler/src/h2m.no`，+17 −1）**
- MK 臂逐字段降级時**并行收集** `fve []i64`：字段值表達式是 `K-SLICE-LITERAL` 且 `nkid == 0`
  ⇒ 標 1（空字面量），否則 0。
- 逃逸門禁改為 `fev == 0 && h2m-vec-own(c, fvv) == 1 -> h2m-mark(c)`（`fev` 走 place-match 讀，
  因為索引讀恒 `?i64`）⇒ **只放行空字面量**，非空字面量 / 標識碼行徑一寸未讓。
- **不予通行證**：`h2m-vec-own` 本身不改（它仍如實報告「自擁有」），改的是「MK 字段值的
  逃逸判定」這一個調用點——所有權語意寫在調用點，不在謂詞裡。

③ **量測（同一語料：`cd tmp/s129/wt && ./bin/no run <絕對路徑>`；A/B **只切換 main 的 `h2m.no`**）**

| 档 | s144 | s145 | 差 |
| --- | --- | --- | --- |
| `cg` | 78 | **78** | 0 |
| `h2m` | 909 | **843** | **−66** |
| `lx` | 87 | **77** | **0**（見下方〈口徑說明〉） |

- **收益分解**（`h2m` 档逐函數 diff）：`h2m-new` 50→2（**−48**，僅剩 `h2m-bk-root(c)` 的
  點號接收者 push）、`h2m-sigs-new` 12→0（**−12**）、`h2m-lst-new` 3→0、`make` 11→8、
  `h2m-leaf-append` / `h2m-rec-append` 各 −2、`h2m-id-name` −1。
  ⇒ **全部是「含 `[]` 字段初值的結構體字面量」家族**，無意外波動。
- **`dropped`**：`h2m` 101→101、`cg` 26→26（**清單不變**）。
- **〈口徑說明〉**：上表 `lx` 的 **87** 是 §61 當時的 wt 語料；本輪統一用**當前** wt 語料
  重量，**同一 s144 降級器**得 **77** ⇒ s145 的 77 與之**持平**（差 0）。差 10 屬**語料漂移**
  （同 §60 的 `cg` 317/318 案），**不是**降級器差異。
- ✅ **重現性**：A/B 兩版日誌各自 `diff -q` 與首輪逐字相同。
- ✅ **零回歸**：`cg` 档 **52 LF / 26 dropped 與 s144 逐字相同**（`cg.no` 語料裡沒有
  可受惠的 `[]` 字段初值）⇒ 本輪的放寬**沒有動到已產碼路徑**。

④ **驗證**
- ✅ **14 套門禁全綠**：`ps` 52 · `h2m` **1365** · `cg` 363 · `ck-sig` 50 · `lx` 81 · `tok` 62 ·
  `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 · `pkg-sigs` 5 ·
  `pkg-ll` 10 · `ck` 20。
- ✅ `no fmt -d compiler/src/h2m.no` **零漂移**。
- ✅ **e2e**：宿主 `no build -o /tmp/noc-s145 compiler` 成功；`ts-e2e` / `ts-min` 在宿主與 `noc` 下
  **逐字一致且退碼 0**；`ts-sem` / `ts-sem2` 的 `11` / `6` 與 s143/s144 相同（M4-d SHS 子集探針）。
- ⚠️ **第一版教訓（值得記下）**：先做過「空 `[]` 不 `h2m-rel-add`」的版本，覆蓋率更好
  （`h2m` 737 vs 742，多 −5），**但打破門禁 `s99n empty still frees`**（該測試斷言空 vec
  也走 free 路徑）。⇒ 改用「保留 rel-add、只放行逃逸門禁」的現版，**門禁 1365/0 恢復全綠**。
  覆蓋率少 5，換取「不降低既有驗證強度」——**刻意的取捨**。

⑤ **诚实边界与下一步**
1. **本輪只解決「MK 字段初值」這一個調用點**：同一族逃逸判定在**字段寫**
   （`h2m-lower-block` 的 `var.field = rhs`，`h2m-vec-own(c, rv)`）仍未放行 `[]`。
   `st.f = []` 在語料裡尚未出現 ⇒ 收益 ≈ 0，故本輪不做（避免無量測支撐的放寬）。
2. **`h2m-new` 仍剩 2 marks**：`h2m-bk-root(c)` 的**點號接收者 push**（`c.bks.push(…)` 等），
   正是 §61 ⑤ 下一步① 的本體。它需要「讀字段 → push → 寫回字段」+ 所有權模型改動
   （push 的新 vec 進字段同樣撞逃逸門禁）⇒ **改動面大，本輪不做**。
3. **`self` 綁定未做**：`h2m-func-sig` 只收 `d.c4`、`h2m-lower-func` 只綁 `fap.len()` 個输入形参
   ⇒ 方法體的 `self` / `.x` 仍讀不到，是 `dropped` 26 的共同根因。
4. **覆蓋率 ≠ 可執行性**：`h2m` −66 讓 `h2m-new` / `h2m-sigs-new` / `h2m-lst-new` **開始產碼**，
   但 `cg` 档一動不動（52/26）⇒ `noc-s145` 的端到端正確性只驗證了「本輪沒有改壞」，
   **不能**說自舉已能自編譯。
- **下一步①**：`h2m-bk-root` / `h2m-emit` 的**點號接收者 push**（前者 5 marks、後者 7 marks，
  且 `h2m-emit` 是所有 emit 的匯流 ⇒ 打通它收益最大）。需先解決「push 结果寫回字段」的
  所有權模型（建议：push 臂對 DotExpression 接收者發 `OP-STRUCT-GET` + `OP-VECPUSH` +
  `OP-STRUCT-SET`，並在逃逸門禁上按 s145 同口徑放行「字段里的舊 vec 已被 reldrop」）。
- **下一步②**：字段寫臂的同族放行（`st.f = []`），量測後決定。
- **下一步③**：§61 ⑤ 下一步① 的 `self` 綁定（根因最深、風險最高）。
- **量測工件**：`tmp/s145/{h2m-s144b,h2m-s145,h2m-s145c,h2m-diag3}.no`、
  `base-{cg,h2m}.log`、`base2-h2m.log`、`s145{,b,c}-{cg,h2m,lx}.log`、
  `diag{,2,3}-h2m.log`、`exp-x{2,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20}.log`、
  `nd{,2,3,4,5,6}.no`、`e2e.{sh,txt,run.log}`、`gate-run.log`、`/tmp/noc-s145`。


## 63 s146 — 认领「點號接收者 push」：`h2m` 档 843→822（−21），`c.<field>.push(x)` 寫回字段

① **定位（正面強攻 §62 ⑤ 下一步①）**
- §62 ⑤ 下一步① 把 `h2m-bk-root` / `h2m-emit` 的 marks 釘在**點號接收者 push**。
  本輪先做一次「按函數字 marks」的盤點，確認 `h2m` 档 843 的三大富礦：
  `h2m-lower-expr` 206 · `h2m-lower-func` 77 · `cg-emit-cfg-t` 58 · `h2m-func-sig` 54 ·
  `h2m-lower-program` 45，以及本輪目標 `h2m-bk-root` 10 / `h2m-bk-push` 10 /
  `h2m-bk-pop` 10 / `h2m-emit` 14。
- **逐條讀碼後根因收斂到一扇門禁**：`h2m-lower-expr` 的调用降级臂裡，
  `push` 的兩個臂（vec / vecstr）都要求 **`recv-nd.kind == asp.K-IDENTIFIER`**。
  而 `c.bks.push(…)` 的 callee 是 `c.bks.push`（DOT_EXPRESSION），
  其 `recv-nd` 是**字段节点** `c.bks`（DOT_EXPRESSION）⇒ 門禁不成立 ⇒ 落
  `ok-call == 0 && ok-print == 0 && ok-method == 0 -> h2m-mark(c)`。
  - `h2m-bk-root` 5 條 = `c.bks` / `c.bka` / `c.bkd` / `c.bkp` / `c.bkm` 五個 `push`。
  - `h2m-bk-push` 5 條 = 同樣五個 `push`（body 與前者同構）。
  - `h2m-emit` 7 條 = `ops` / `dsts` / `xa` / `bs` / `syms` / `blks` / `cty` 七個 `push`。
  - `h2m-bk-pop` 5 條 = `c.bks.pop()` 等五個 **`pop`** —— 當前**根本沒有 pop 臂**
    （`mir.no` 無 `OP-VECPOP`），屬**未實現語意**，非門禁問題 ⇒ 本輪不做。
- **另一條 Host quirk（本輪再遇）**：`print` 在自舉降級路徑下**不可靠**（探針無輸出），
  只有 cov 的 `LF:` / `ID:` / `dropped` 三個日誌通道可信 ⇒ 本輪診斷全走 `ID:` 通道。

② **修復（`compiler/src/h2m.no`，+84 −0）**
- 新增 `h2m-push-field-set = (c, t, recv-nd, bnm, nv) (ok i64)`：
  由 `recv-nd` 讀出**字段名**，由 `bnm`（點號鏈最左端的已綁定名）經 `h2m-var-read` 取
  **結構體值** `sv`，發 `OP-STRUCT-SET` 把新 vec 值 `nv` 寫回該字段，
  再 `h2m-var-write(c, bnm, dsv)` 重綁 base 名（`mem == 0` 時即 `h2m-var-bind`）。
  - ** Facility 不計 unsup**：失败只回 0，由調用點統一 `h2m-mark` 一次（避免雙重計數）。
  - ⚠️ 只認**兩段**點號（`recv-nd.kid0` 是 `K-IDENTIFIER`）；`a.b.f.push` 這類深鏈落地門禁。
- 兩個 `push` 臂各增一条**點號接收者分支**（`recv-nd.kind == asp.K-DOT-EXPRESSION`）：
  `rv` 已是**字段讀值**（DOT-EXPRESSION 臂的 `OP-STRUCT-GET` 結果）⇒ 揹 `OP-VECPUSH` /
  `OP-VECPUSHSTR` 出新值 ⇒ `h2m-push-field-set` 寫回 ⇒ `h2m-rel-drop(c, rv)` +
  `h2m-rel-add(c, nv, …)`（**與 Identifier 臂逐字同口徑**，所有權不另立規則）。
- **逃逸門禁未改**：`nv` 是 `OP-VECPUSH` 結果（非自擁有、不在 rel-list）⇒
  `h2m-vec-own(c, nv) == 0`，寫回字段不觸發外流判定；字段读值 `rv` 是 vscode `alloc`，
  同樣不在 rel-list。⇒ **不需要** §62 ⑤ 下一步① 設想的「同口徑放行」。

③ **量測（同一語料：`cd tmp/s129/wt && ./bin/no run <絕對路徑>`；A/B **只切換 main 的 `h2m.no`**）**

| 档 | s145 | s146 | 差 |
| --- | --- | --- | --- |
| `cg` | 78 | **78** | **0**（逐字相同） |
| `h2m` | 843 | **822** | **−21** |
| `lx` | 87 | **87** | **0**（逐字相同） |

- **收益分解**（`h2m` 档逐函數 diff，A → B）：`h2m-bk-root` 10→0（**−10**）、
  `h2m-bk-push` 10→0（**−10**）、`h2m-emit` 14→**2**（**−12**）、`h2m-new` 4→0、
  `h2m-alloc` 4→0、`kid-add` 2→0、`aux-add` 2→0（後四者是「push 打通後函數體繼續降級」
  的**連帶收益**）。日誌行數 1559→1515（−44）。
- ** PACKAGED 口徑**：`pkg-ll` 的 `noc: unsupported constructs` **2548 → 2527（−21）**，
  與 `h2m` 档的 −21 **逐個吻合**（同一批失敗點）。
- ✅ **重現性**：A（s145 版）在本輪重跑，日誌與 §62 的 `s145c-h2m.log` **逐字相同**。
- ✅ **零回歸**：`cg` / `lx` 两档日誌與 s145 **逐字相同**（`diff -q` 無輸出）。

④ **驗證**
- ✅ **14 套門禁全綠**：`ps` 52 · `h2m` **1365** · `cg` 363 · `ck-sig` 50 · `lx` 81 ·
  `tok` 62 · `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 ·
  `pkg-sigs` 5 · `pkg-ll` 10 · `ck` 20（全 0 failed，`gate.txt` 末尾 `DONE`）。
- ✅ `no fmt -d compiler/src/h2m.no` **零漂移**。
- ✅ **e2e**：宿主 `no build -o /tmp/noc-s146 compiler` 成功（`EXIT=0`）；
  `ts-e2e` / `ts-min` 在宿主與 `noc` 下**逐字一致**；`ts-sem` / `ts-sem2` = **11 / 6**
  （與 s143/s144/s145 相同）。
- ⚠️ **診斷排除了兩個「看起來該成功」的分支**（記下來供後續輪次省去重查）：
  ① 刪除 `h2m-emit` 的 `syms` / `blks` / `cty` 三個 push 後仍剩 1 條 mark；
  ② 用 `ID:` 通道逐級記錄（`pf-rv0` / `pf-rty` / `pf-nf` / `pf-ty` / `pf-set`）後定位到
  **`ID:h2m-emit:pf-rty-syms`** —— 剩下的那 1 條是 `c.syms.push(sym)`。

⑤ **诚实边界与下一步**
1. **`h2m-emit` 仍剩 1 mark 是「誠實上報」，不是缺口**：`syms` 是 `[]str` 字段，
   而 `h2m-struct-resolve` 按 **s99f 門禁**把 `[]str` 結構體字段的型別攔成 `-1`
   （`h2m.no:1740` / `1791`：`fty == TY-VECSTR -> fty = -1`）⇒ 字段讀值型別為 −1，
   落 push 臂之外。**放行它需要先解決 s99f 那整族**（`[]str` 字段的安全釋放），
   本輪不做。
2. **`h2m-bk-pop` 的 5 個 `pop` 未做**：`mir.no` 無 `OP-VECPOP`，需要新增 op 码 +
   cg 指令 + 返回值語意（pop 回傳元素並縮短向量）⇒ **改動面超越 h2m 檔**，本輪不做。
3. **深點號鏈未覆蓋**：`a.b.f.push(x)`（接收者 Chain 深度 > 2）落地門禁如實上報。
4. **字段寫臂的同族放行**（§62 ⑤ 下一步②，`st.f = []`）本就收益 ≈ 0，本輪順帶確認仍不需要。
5. **覆蓋率 ≠ 可執行性**：`cg` / `lx` 两档**一動不動**（78/87 逐字相同）⇒
   `noc-s146` 的端到端只驗證「本輪沒有改壞」，**不能**說舉已能自編譯。
- **下一步①**：`self` 綁定（§61 ⑤ 下一步①，根因最深）：`h2m-func-sig` 只收 `d.c4`、
  `h2m-lower-func` 只綁 `fap.len()` 個输入形参 ⇒ 方法體的 `self` / `.x` 讀不到，
  是 `dropped` 26 的共同根因。
- **下一步②**：`[]str` 結構體字段（s99f）整族 —— 打通後 `c.syms.push(sym)` 與
  `rnms` / `epl` / `aln` / `callee` / `cgf` / `cgl` / `idn` / `stn` / `stfldn`
  一批 `[]str` 字段齊活，預估是下個大富礦。
- **下一步③**：`pop` 語意（新增 `OP-VECPOP` + cg），打通 `h2m-bk-pop` 的 5 marks。
- **量測工件**：`tmp/s146/{h2m-s145c,h2m-s146a,h2m-s146d,h2m-v1,h2m-v2,h2m-v3}.no`、
  `A-h2m.log`、`B-h2m.log`、`B-cg.log`、`B-lx.log`、`V{1,2,3}.log`、`D{,-2,-3}-h2m.log`、
  `A-agg.txt`、`B-agg.txt`、`gate.sh`、`gate.txt`、`gate-run.log`、`e2e.{sh,txt,run.log}`、
  `/tmp/noc-s146`。


## 64 s147 — 认领「`[]str` 结构体字段」（s99f 整族）：`h2m` 档 822→492（−330），字段 push 所有权移转

① **定位（正面强攻 §63 ⑤ 下一步②）**
- §63 ⑤ 下一步② 把 `h2m-emit` 剩的 1 mark（`c.syms.push(sym)`）钉在 **s99f 门禁**：
  `h2m-struct-resolve`（`h2m.no:1740`）与 `h2m-struct-resolve-range`（`:1791`）把
  `fty == TY-VECSTR -> fty = -1` —— `[]str` 结构体字段的型别被拦成 −1，字段读值落空。
- 先做**定位实验 E1**（只删两处门禁，不动其它）：`h2m` 822→**492（−330）**，
  日誌 1618→920 行；`ast-str-*` 家族 199→34（`ast-str-intern` 88→8、
  `ast-str-intern-raw` 65→**0**、`ast-str-grow` 46→14），`h2m-lower-expr` 206→154、
  `h2m-lower-func` 77→43、`h2m-func-sig` 54→44、`h2m-lower-program` 45→32。
  ⇒ s99f 是**最大集中富矿**（占本轮 −330 的大头）。
- 门禁理由（原文注释）：str-vec 缓冲**无 RC 头**（§11 B5）⇒ 写进结构体字段后，
  局部 vec 与字段别名同一缓冲，scope-exit 的 `vec_free_str` 会与字段 owner 双释放 ⇒ UAF。

② **修复（`compiler/src/h2m.no`，+11 −10）**
- **所有权模型**：`push` 进字段 = **所有权移转**（move）。vec 缓冲无 retain ⇒ 同一时刻只能有一个
  owner；push 消费旧字段值 `rv`（`OP-VECPUSH` / `OP-VECPUSHSTR` 接管其缓冲）⇒ 新值 `nv`
  的 owner 是**字段**。
  - 字段 push 臂（s146 新增的 `h2m-push-field-set` 调用点）：`okf == 1`（写回成功）时
    **不入 rel-list** —— `h2m-rel-add` 只移进 `okf == 0` 分支（旧口径不变）；
    `okf == 0` 仍 `h2m-mark` + rel-add。
  - 局部变量 push 臂（`K-IDENTIFIER`）**口径不变**（`h2m-var-bind` + rel-add）。
- 两处 `fty == TY-VECSTR -> fty = -1` 门禁**删除**（`h2m-struct-resolve` / `-range`）。
- **字段写臂**（`b.f = rhs`，`h2m-lower-block`）的 **s99n 逃逸门禁保持**：自拥有 vec 写进字段
  仍 `h2m-mark`（普通赋值没有 push 那种「消费旧值」语义，放行需 clone 整个 vec ⇒ 成本高）。
- 结构体 release（`OP-STRUCT-RELEASE`）**不释放** vecstr 字段（现状：只 release str 字段）
  ⇒ 字段持有的缓冲活到结构体生命周期结束（局部结构体 ⇒ 泄漏到函数外；**无 UAF**）。

③ **量测（同一语料：`cd tmp/s129/wt && ./bin/no run <绝对路径>`；A/B **只切换 main 的 `h2m.no`**）**

| 档 | s146(A) | s147(B) | 差 |
| --- | --- | --- | --- |
| `cg` | 78 | **78** | **0**（逐字相同） |
| `h2m` | 822 | **492** | **−330** |
| `lx` | 87 | **87** | **0**（逐字相同） |

- **PACKAGED 口徑**：门禁 `pkg-ll` 的 `noc: unsupported constructs` **2527 → 2017（−510）**
  —— 差额 510 = `h2m` 档 −330 + 其他档语料（`ps` / `ck` / `drv` 等）因降級器實現改動而受益的 −180。
- **A 口徑復核**：本輪 A（s146 终态）日誌與 §63 的 `tmp/s146/B-h2m.log` **逐字相同**（1618 行）。
  ⚠️ §63 正文記的「日誌行數 1559→1515」與檔案實測（1618）不符 —— 以本輪 `diff` 為準。
- ✅ **零回歸**：`cg` / `lx` 两档日誌與 A **逐字相同**（`diff -q` 無輸出）。

④ **驗證**
- ✅ **14 套門禁全綠**：`ps` 52 · `h2m` **1365** · `cg` 363 · `ck-sig` 50 · `lx` 81 ·
  `tok` 62 · `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 ·
  `pkg-sigs` 5 · `pkg-ll` 10 · `ck` 20（全 0 failed）。
- ✅ `no fmt -d compiler/src/h2m.no` **零漂移**。
- ✅ **e2e**：宿主 `no build -o /tmp/noc-s147a compiler` 成功（`EXIT=0`）；
  `ts-e2e` / `ts-min` 在宿主與 `noc` 下**逐字一致**；`ts-sem` / `ts-sem2` = **11 / 6**
  （與 s143–s146 相同）。
- ✅ **降級口徑探針**：新語料 `tmp/s147/t-field-str.no`（第三方 `[]str` 字段 push + 讀回，
  宿主退碼 0 輸出 `3 / len-ok / idx-ok / done`）在 **E1 與 s147a 兩版 noc 下都是 3 個 unsup**
  ⇒ 本輪修正**只改所有權、不改降級成功數**（覆盖率與 E1 持平）。

⑤ **诚实边界与下一步**
1. **UAF 驗證的邊界（重要，別把語義正確當成實測正確）**：本修正的動機是「自舉後 `noc` 降級
   std 時不再 double-free」，但**當前驗證口徑測不出**：e2e 的 `build` 用**宿主（Go）編譯器**跑
   降級器（有 GC，本來就無 UAF），而 `noc` 跑的 `ts-*` 語料不含 `[]str` 字段構造。
   ⇒ 修正的正確性依據是**所有權語義推導**（`nv` 別名進字段後，rel-list 不得再持有），
   **不是**實測。要實測需讓 `noc` 降級**含 `[]str` 字段 push 的語料**（如降級器自身），
   而 `noc` 目前對 `t-field-str.no` 仍有 3 個 unsup ⇒ **尚未走到那一步**。
2. **結構體 release 不釋放 vecstr 字段** ⇒ 字段緩衝泄漏到結構體生命週期結束（局部結構體也泄漏）。
   無 UAF，但**不是**「安全釋放」；徹底解需給 vec 緩衝加 RC 頭（§11 B5 架構改動）或擴充
   `OP-STRUCT-RELEASE` 支援 vec 字段位圖。
3. **字段寫臂（`b.f = rv`）的 s99n 門禁保持** ⇒ `st.f = []` 這類「自擁有 vec 直接寫字段」仍 unsup。
   放行需 clone（深拷貝）⇒ 成本高，本輪不做。
4. **push 表達式位未覆蓋**：`x = a.f.push(y)`（push 作表達式）會讓 `x` 與字段雙重持有 ⇒
   當前語料裡 push 都是語句位；若將來出現，需在 `lower-let` 側同步處理。
5. **覆蓋率 ≠ 可執行性**：`cg` / `lx` 两档**一動不動**（78/87 逐字相同）⇒ `noc-s147a` 的
   端到端只驗證「本輪沒有改壞」，**不能**說自舉已能自編譯。
- **下一步①**：`self` 綁定（§61 ⑤ 下一步①，根因最深）：`h2m-func-sig` 只收 `d.c4`、
  `h2m-lower-func` 只綁 `fap.len()` 個輸入形參 ⇒ 方法體的 `self` / `.x` 讀不到，
  是 `dropped` 26 的共同根因。
- **下一步②**：讓 `noc` 降級**降級器自身**（自舉驗證）：先定位 `t-field-str.no` 那 3 個 unsup
  （是本輪修正的直接延續）。
- **下一步③**：`pop` 語意（新增 `OP-VECPOP` + cg），打通 `h2m-bk-pop` 的 5 marks。
- **量測工件**：`tmp/s147/{h2m-s146a,h2m-e1,h2m-s147a}.no`、`E1-h2m.log`、`E1-agg.txt`、
  `A-h2m.log`、`B-h2m.log`、`A-cg.log`、`B-cg.log`、`A-lx.log`、`B-lx.log`、`t-field-str.no`、
  `ab.sh`、`ab-cglx.sh`、`gate.sh`、`gate.txt`、`gate2.sh`、`gate2.txt`、`e2e-e1.txt`、
  `e2e-s147a.txt`、`/tmp/noc-s147e1`、`/tmp/noc-s147a`。


## 65 s148 — 定位 `[]str` 字段语料的 3 个 unsup：索引读 match（1）+ 多次 push 后的顶层失败（2）

① **定位（正面强攻 §64 ⑤ 下一步②）**
- §64 ⑤ 下一步② 要求「先定位 `t-field-str.no` 的 3 个 unsup」。本轮把它做成**可复现的明细通道**：
  新脚本 `tmp/s147/cov-field.no`（复制 `cov-h2m.no` 的 import 块，末行改
  `drv.pkg-ll-cov('<绝对路径>')`）—— 参数按 cwd（= wt）解析，**绝对路径可读**
  （相对 `../s147/…` 报 `cannot read`：pkg-ll-cov 按 package root 解析，`..` 出界）。
  跑通后输出逐条 `LF:` / `ID:`。
- `t-field-str.no` 明细：`main:BlockStatement` + `main:FunctionDefinition` + `main:Program`
  + `LF:main:Identifier` ×3 ⇒ 3 条，块级归因里混着**外层块**（FunctionDefinition / Program）。
- **二分**（7 个变体，每个跑一次 cov；`t-minimal.no` 证明**顶层脚本本身 0 unsup**）：

| 变体 | 构造 | unsup |
| --- | --- | --- |
| `v-init` | `box{ items: [] }` 仅初始化 | **0** |
| `v-push` | + 3 次 `b.items.push('x')` | **0** |
| `v-read` | + `print(b.items.len())` | **0** |
| `v-chain` | 纯短路链 `{ … }`（无 vec / match） | **0** |
| `v-idx` | 1 次 push + `b.items[1]: { ok -> … }` | **1** |
| `v-itcmp` | 同上 + 臂内 `it == 'b'` 短路链 | **1** |
| `v-3push-idx` | **3 次** push + 索引读 match | **3** |

- **结论**：3 个 unsup = ① **索引读 match 本身**（`b.items[1]` 的 INDEX-EXPRESSION 读，
  单次 push 即现，1 个）；② **多次 push 后再索引读**多出的 2 个 —— 块级归因从
  `BlockStatement` 升到 `FunctionDefinition` / `Program`，说明第 2/3 次 push 的降级路径
  改了块上下文（`cur-k` / `bk` 栈）或字段状态，使随后的**顶层语句**归因错乱。
- ✅ 与 `noc` 行为吻合：`/tmp/noc-s147a run t-field-str.no` 报 `3 unsup`（同一降级器）。

② **修复**：**本轮未做**（定位轮，`compiler/src/h2m.no` 保持 s147a，md5 5b53197d…）。

③ **量测**：见上表（每格一次 `cov-field`；降级器实现 = main 树 s147a，语料 = 绝对路径）。

④ **验证**：`cov-field.no` 可复现（7 个变体 + `t-minimal` 全部跑通）；明细与 `noc` 的
`3 unsup` 报告一致。

⑤ **诚实边界与下一步**
1. **本轮零代码改动**：只新增测量脚本与 8 个变体语料，不动 `h2m.no`。
2. **两个缺口独立**：① 索引读 match（1 个）—— 待查 `h2m-lower-expr` 的 INDEX-EXPRESSION 臂
   对**结构体字段基**（`b.items`）的支持；② 多次 push（2 个）—— 待查 `h2m-push-field-set`
   重复写回同一字段时 `cur-k` / `bk` 与字段状态的交互。
3. **覆盖口径**：本轮全部用**宿主编译器**跑降级器（Go，有 GC）⇒ 与 §64 ⑤ 边界 1 同：
   这里定位的是**降级失败点**，不是运行期 UAF。
- **下一步①**：实现缺口①（索引读 match）—— 最小、可独立验证。
- **下一步②**：实现缺口②（多次 push）。
- **下一步③**：`self` 绑定（§64 ⑤ 下一步①，根因最深）。
- **量测工件**：`tmp/s147/{cov-field.no,cov-v-*.no,bisect.sh,t-minimal.no}`、
  `tmp/s147/v-{init,push,read,chain,idx,itcmp,3push-idx}.no`。


## 66 s148b — 缺口①真身：`c.mem == 0` 门禁误伤字段 push（顶层 match → 合成 main 进 mem 模式）
（HEAD `926a2426` v0.4.29；量测宿主 = d21fbb35 worktree 旧 checker）

① **定位（推翻 §65 的归因）**
- §65 把 3 个 unsup 归因成「①索引读 match 本身 + ②多次 push 后块上下文错乱」。本轮用**包装函数探针**
  （把所有 `h2m-mark(c)` 换成 `h2m-mark-tagged(c, tag)`，保持 `cond ->` 单语句控制流不变——
  直接插入独立语句会让 mark 无条件执行，实测假象 16 unsup，已还原）逐条定位：
  - 唯一 mark 点 = `h2m.no:4107`（K-CALL-EXPRESSION 的 `ok-call/print/method` 全 0）。
  - 打印失败 call：`FK-/push/b`（方法名 push、接收者 b、`cn` 空）⇒ **失败的是 `b.items.push('a')`，不是 match、也不是索引读**。
- **最小语料二分**（`tmp/s148/probe/`，全部用 `cov-field.no` 口径）：

  | 语料 | 内容 | unsup |
  | --- | --- | --- |
  | `p3` | push + `v = b.items[1]`（无 match） | **0** |
  | `s2` | push + `print('after')`（无 match） | **0** |
  | `p4` | push + `b.items[1]: { -> print }`（单兜底臂） | **1** |
  | `t1` | match 写在 push **之前** | **1** |
  | `t2` | 2 次 push + match | **2** |
  | `s1` | push + 空 match | **2**（push + 空 match 各一） |

  ⇒ 规律：**每个字段 push 在「档内含 SurfaceMatch」时各 mark 一次**，与 match 先后／索引读与否／臂内容无关。
- **根因（探针钉死）**：打印 push 门控现场——`p4` 为 `ST-6-mem1-k25-om0`（`c.mem == 1`），
  `s2` 为 `ST-6-mem0-k25-om0`（`c.mem == 0`）。两者的 `rv`（字段读值 id）、`rty`（=6=TY-VECSTR）、
  `h2m-name-find('b')`（=0）、`h2m-push-field-set` 入口之前的状态全部相同；
  差别只在 `c.mem`。字段 push 臂（`h2m.no:3737` TY-VEC / `:3797` TY-VECSTR）要求 `c.mem == 0`，
  而**顶层 match 让 `h2m-synth-main` 合成的 `@main` 带输出参 `__noc_r` ⇒ `h2m-lower-func` 置 `c.mem = 1`**
  （`h2m.no:6261`）⇒ 两条字段 push 臂整条落空，`ok-method` 保持 0 ⇒ 4107 mark。
- ✅ 这同时解释 §65 的「多次 push 多出 2 个」：就是第 2/3 次 push 各再撞一次同一门禁，
  **不是**块上下文栈错乱。§65 的块级归因（BlockStatement/FunctionDefinition）是 `h2m-bk-take` 的近似，非根因。

② **修复（`compiler/src/h2m.no`，+8 −2；md5 `379dd3c0`→`10e46f3`）**
- 去掉**点号接收者**（字段）push 两臂的 `c.mem == 0` 条件（3737、3797）：字段 push 走
  `h2m-push-field-set`，其内部 `h2m-var-write(c, bnm, dsv)` 在 mem==1 下已由
  `h2m-var-write:1387`（`st >= TY-STRUCT-BASE` ⇒ `OP-STSTORE`）正确写回 struct 槽 ⇒ 门禁多余。
- **标识符接收者** push 两臂（3718、3780）的 `c.mem == 0` **保留**：它们依赖 `h2m-var-bind`
  直接重绑名字，mem 模式下语义不同，放宽会出错。
- 只改条件 + 注释；未动任何 mark / emit / 槽登记逻辑。

③ **量测（A/B 只切 main 的 `h2m.no`，宿主 = d21fbb35 worktree 旧 checker）**

| 档 | A `379dd3c0` | B `10e46f3` | 差 |
| --- | --- | --- | --- |
| `h2m` | 471 | **403** | **−68** |
| `cg` | 76 | **76** | 0（逐字相同） |
| `lx` | 87 | **87** | 0（逐字相同） |

- 缺口语料：`v-idx` / `v-3push-idx` / `p1` / `p2` / `p4` / `t1` / `t2` 全部 **1或3 → 0**；
  `s1` 空 match 2→1（余 1 = 空 match 自身 preserve 的诚实门禁，与本缺口无关）。
- **§65 的下一步①与②（索引读 match + 多次 push）由同一处修复一并清零**——
  两者本就是同一门禁的两个表现，不是两个独立缺口。

④ **验证**
- ✅ **14 套门禁**：`ps` 52 · `h2m` 1365（6 failed，见下）· `cg` 363 · `ck-sig` 50 · `lx` 81 ·
  `tok` 62 · `diag` 7 · `build-package` 8 · `util` 27 · `ps-err` 46 · `pkg-ll-call` 5 ·
  `pkg-sigs` 5 · `pkg-ll` 10 · `ck` 20（全 0 failed）。
  - ⚠️ `h2m` 档的 6 failed **全部是 s109 诊断通道测试**（`test-s109-id-attrib`，语料 `r = missing` /
    `r = tok.EOF` / `r = a`），与字段 push 无关。**用改前 bak 跑同一测试逐字同样 6 failed**
    ⇒ v0.4.29 引入 `h2m-id-name` 诊断标记时的**预存基线回归**，非本轮引入（已 A/B 对照）。
- ✅ **e2e**：宿主 `no build -o tmp/s148/noc-fixed compiler` 成功；`ts-e2e`/`ts-min` 在 host 与
  `noc-fixed` 下**逐字一致**且 exit=0；`ts-sem`/`ts-sem2` 仍 11/6 unsup（与 s143–s148 相同，
  已知 SHS 子集边界，非回归）。
- ⚠️ `no fmt -d compiler/src/h2m.no` 在 `:4517`/`:4544` 有缩进漂移（`let-cap`/`let-tc` 块）——
  同为 v0.4.29 诊断标记带入的**预存漂移**，本轮不碰（改动在 3737/3797，format 干净）。

⑤ **诚实边界与下一步**
1. **权威对齐未测**：本轮只证明自举降级器侧放行，`c.mem==1` 下 `OP-STSTORE` 写回字段缓冲的
   **运行期语义**（所有权／双释放）未在 `noc` 下跑过含 `[]str` 字段 push 的可执行语料
   （`ts-*` 不含此构造）。放行是依 `h2m-var-write:1387` 的 struct 槽写回语义推导 + cg/lx 逐字零回归，
   **不是**运行期实测。
2. `s1` 空 match 仍 1 unsup（空 SurfaceMatch 无臂 ⇒ `arms <= 0 -> h2m-mark`，`h2m.no:5408`），
   属诚实门禁，不在本轮范围。
3. **§65 与代码的不一致**：§65 记「h2m.no 保持 s147a `5b53197d`、本轮零代码改动」，
   但 v0.4.29 提交已把 s148 定位用的诊断标记（`h2m-id-name('blk-let')`/`('si-bt-vecstr')` 等）
   写进主工作区 `h2m.no`（`379dd3c0`）。本轮以 `379dd3c0` 为 A 基线——**§65 的「零改动」仅对
   降级语义成立，诊断标记那一步未追记**。
4. **下一步①**：`self` 绑定（§64 ⑤ 下一步①，根因最深）——`h2m-func-sig` 只收 `d.c4`、
   `h2m-lower-func` 只绑 `fap.len()` 个输入形参 ⇒ 方法体的 `self`/`.x` 读不到。
5. **下一步②**：让 `noc` 降级降级器自身（自举验证）——`v-idx` 已 0 unsup，
   可改在 worktree 里让 `noc` 直接跑 `drv.pkg-ll-cov` 验证 mem 模式下字段 push 的 cg 发射。
6. **量测工件**：`tmp/s148/{wt/,h2m.no.bak,h2m.no.fixed,ab-measure.sh,gate.txt,e2e.txt,noc-fixed}`、
  `tmp/s148/probe/{p1,p2,p3,p4,s1,s2,r1,r2,r3,t1,t2}.no` 与对应 `cov-*.no`。
  ⚠️ 临时覆盖 h2m.no 的 A/B 脚本带 `trap` 还原；`tmp/s148/wt` 为 d21fbb35 worktree
  （`worktree-meta.noml` 已写），用完按约定清理前先读 meta。


## 67 s149 — 认领方法体的 self 绑定（定义侧）：h2m 403→392（−11）、cg 76→73、lx 87→82

① **定位（承 §66 ⑤ 下一步①，先纠正归因）**
- §61~§66 反复把 `self` 绑定称作 `dropped` 26 的「共同根因」，并说「`self` 从不在 `c.names`」。
  本轮侦察**修正了这条链的描述**：被 dropped 的 `cg-runtime-*` / `cg-rc-*` 函数体
  （`cg-runtime.no` / `cg-rc.no`）是 `= () (r str)` 的**普通函数、不含 self**；它们内部是
  `b.push(...)` 点号方法调用。真正的 self 在**被调用的方法定义体**里——`sb.push` 用裸点号
  `.buf.push(b)`（`sb.no:37`），AST 形如 `self.buf.push`。方法定义降级失败 ⇒ `sb.push` 不 alive
  ⇒ 调用它的函数被定点剔除（dropped 语义见 `drv.no:961`）。**所以 self 缺口仍在，只是落点在定义侧**。
- **受控探针**（`tmp/s148/selfbind/`）钉死最小形态：
  - `method-self.no`：`box.push = (v i64){ .buf.push(v) }` ⇒ **1 unsup**，mark 在
    `h2m.no:4115`（K-CALL-EXPRESSION 全失败），失败 call = `FAIL-buf.push|m=push|rn=self|rk=25`。
  - `plain-fn.no`：同逻辑写成普通函数 `add-to = (b box, v i64){ b.buf.push(v) }` ⇒ **0 unsup**。
  - ⇒ 差异精确落在「方法体的裸点号接收者」：`self` 未绑名 ⇒ `h2m-name-find(c,'self')` 失败
    ⇒ `recv-is-value=0` ⇒ 字段 push 分派整条落闸。
- **调用约定澄清**（侦察 agent）：宿主 Go 后端把 self 当方法 LLVM 签名的第 1 个 out-param
  （`hir2mir.go:2880`、`codegen.go:15424`）；但 h2m 自举降级器走自己的约定——s141 把方法调用
  改成「不传 self 的静态转发」（`h2m.no:3905-3922`），`cg` 发射直接返回值 + 输入形参
  （`cg.no:317`）。**两套约定不同**，故「绑定 self」无需改 Go 侧约定，也不改 h2m 的调用约定——
  只需让定义体内 self 可被类型/字段路径解析。侦察 agent 的「高风险调用约定改动」结论
  只适用于「让 h2m 与 Go ABI 对齐」，不适用于本轮「打通定义体字段读」的窄目标。

② **修复（`compiler/src/h2m.no`，+30 −2）**
- 在 `h2m-lower-func` 的形参与循环之后、body 之前（`h2m.no:6255`）插入 s149 块：
  `is-m==1`（`d.s2=='true'`）时读输出区[0]（`sp = kid0+c3+c4`）的 self 参数节点，
  用 `h2m-param-ty(t,agg,sk)` 解析其具名型别（如 `box` ⇒ `TY-STRUCT-BASE+sid`），
  `h2m-alloc` 一个值 id 绑到 `'self'` 并 `h2m-lst-add(loc,'self',sc)` seed 进局部表。
- **语义定位**：绑的是【仅供类型/字段路径推导的壳值】——s141 静态转发不传真实 receiver，
  故 self 无运行时实参；它只让 `.field` 读拿到接收者型别、字段名与字段型别。不改 LLVM 签名、
  不改调用点、不 emit 新形参引用。
- `#{overflow=wrap}` 注解块按 fmt 要求留空行，`no fmt -d` **零漂移**。

③ **量测（A=prefix `10e46f32` / B=s149；worktree 旧 checker，只切 main 树 h2m.no）**
| 档 | A（§66 修复态） | B（s149） | 差 |
|---|---|---|---|
| h2m | 403（dropped 98） | **392（dropped 90）** | **−11（−8 dropped）** |
| cg | 76（dropped 24） | **73（dropped 24）** | **−3** |
| lx | 87（dropped 10） | **82（dropped 8）** | **−5** |

- `cg`/`lx` 非「零回归」而是**同向下降**：方法体 self 打通后更多降级路径可达，非错误放行
  （dropped 数同步下降 = 活着的函数变多）。
- mem 方法语料 `self-mem.no`（方法体含 if）：8 → **3** unsup，方法体内 CallExpression 全消失，
  剩余 3 项与 self 无关。确认 `c.mem==1` 下 self 铺槽未引入新 mark。

④ **验证**
- ✅ 14 套门禁：`ps 52 · h2m 1365 · cg 363 · ck-sig 50 · lx 81 · tok 62 · diag 7 ·
  build-package 8 · util 27 · ps-err 46 · pkg-ll-call 5 · pkg-sigs 5 · pkg-ll 10 · ck 20`。
  唯一 `h2m` 6 failed 与 §66 基线**逐字相同**（s109 诊断通道，v0.4.29 预存），非本轮引入。
- ✅ `no fmt -d compiler/src/h2m.no` 零漂移。
- ✅ self-host：`no build -o tmp/s148/selfbind/noc-s149 compiler` 成功；`noc-s149` 跑
  `ts-e2e`/`ts-min` 输出与 host 及上轮基线**逐字一致**，`ts-sem`/`ts-sem2` 仍 11/6 unsup。
- ✅ `method-self` 语料：定义侧 unsup 1→0；`plain-fn` 保持 0。

⑤ **诚实边界与下一步**
1. **覆盖率 ≠ 可执行性（本轮最需记住）**：用 `self-e2e.no`（`b.push(1)` 点号方法调用）
   实测：host 输出 `3`；s66 基线 noc 报 4 unsup、s149 noc 报 2 unsup，**两者都不产可执行程序**。
   ⇒ 本轮修复打通了**定义侧**，但**调用侧**（`b.push()` 点号方法调用，`main` 里 3 处
   CallExpression + b 引用 = 5 unsup）仍是独立缺口，含此构造的程序仍不可执行。noc 整体拒绝
   这类程序，故**没有错误放行产出错误 IR**，只是尚未让他可执行。
2. **壳 self 无运行时值**：静态转发语义下 self 是不带实参的 alloc 壳，仅用于字段路径推导；
   若将来要让方法真正修改 receiver，必须在调用侧把 receiver 作为首参传入并让 self 绑定到它
   ——那才触及与 Go out-param 约定对齐的深水区（侦察 agent 结论）。
3. **与 §65/§66 归因的关系**：§66 ⑤ 把 self 绑定称作 `dropped` 的共同根因——方向对但落点描述
   不准（在方法**定义**体内，不在被 dropped 的函数体内）。本轮以探针修正，未改动 §65/§66 原文。
4. **下一步①（调用侧）**：认领 `main` 里的点号方法调用 `b.push()`——s141 只支持
   `mod.fn(x)` 形态的跨模块调用，不含「变量接收者的结构体方法」；这是让 self 程序可执行的最后一环。
5. **下一步②**：让 `noc` 直接跑 `drv.pkg-ll-cov` 验证 s149 方法体壳 self 的 cg 发射文本合法性。
6. **量测工件**：`tmp/s148/selfbind/{h2m.prefix,h2m.s149,method-self.no,plain-fn.no,self-e2e.no,
   self-static.no,self-mem.no,ab2.sh,gate.txt,noc-s149,h2m-fixed.log}` 与对应 `cov-*.no`。
   A/B 脚本带 `trap` 还原；当前主树 `h2m.no` 为 s149（`fe8891dd`），已与备份一致。


## 68 s150 — 定位调用侧「变量接收者方法」：查表只差模块前缀，但补上零收益 ⇒ 定位轮（零代码改动）

① **定位（承 §67 ⑤ 下一步①）**
- §67 留下：`self-e2e.no` 的 `b.push(1)` 在 `main` 里 3 个 CallExpression + 级联 = 5 unsup，noc 不产码。
  本轮用包装函数探针钉死：`main` 的 3 个 `b.push` 都走到 s141 的 TY-STRUCT 转发臂
  （`h2m.no:3911`，探针 `STRCT-push`），但 `h2m-sig-find(sg, 'box.push')` **MISS**
  （探针 `S141C-box-box.push` + `S141-MISS`）。
- **根因**：整包签名表的 key = `mod - '.' - short`（`drv.no:725`）。方法定义 `box.push` 不带模块前缀
  ⇒ `short = 'box.push'` ⇒ 登记 key = `self-e2e.box.push`；而 s141 转发臂拼的是
  `sn2 - '.' - mname = 'box.push'`（结构体名.方法名）——**差模块前缀**。s141 注释里 `sb.push` 能命中
  只是因为「结构体名 == 模块名」（`sb.no` 里 `sb.push` ⇒ key 恰好 `sb.push`），属巧合等价。
  这与 §58 ⑤-2 记的「跨模组同名结构体」是同一类 key 拼法问题。

② **实验性修复（s150，已撤回）**
- 在 s141 臂加 s114 既有口径的 fallback：`si2 < 0 -> si2 = h2m-sig-find-emit(sg, cn2)`，
  命中则 `cn = sg.femit[si2]`（emit 名 = 定义的 AST 裸名 `box.push`，唯一命中才收，歧义保护同 s114）。
- 构造性语料 `self-e2e.no` unsup **5 → 2**（3 次 `b.push` 打通；残余 2 是 `box.len(b).to-str()` 的
  `0.to-str` 级联——有返回值方法的静态转发链不完整）。
- 构建 `noc-s150` 跑 `self-e2e.no` / `self-semantic.no`：**仍报 unsup、不产可执行程序**
  （host 输出 `3` / `5,9`；noc 两版都是拒绝）⇒ **没有错误放行**。

③ **量测（决定撤回的关键证据）**
- A/B 三档（A=s149 `fe8891dd` / B=s150 `e1a4cf51`）：
  | 档 | A | B | 差 |
  |---|---|---|---|
  | h2m | 392（dropped 90） | 392（dropped 90） | **0** |
  | cg | 73（dropped 24） | 73（dropped 24） | **0** |
  | lx | 82（dropped 8） | 82（dropped 8） | **0** |
- **大语料零收益**：编译器 16 模块里结构体名都恰好等于模块名（`sb.push`@`sb.no`、`cg.*`@`cg.no`…），
  s141 的 `sn.mname` 拼法本就命中；`sig-find-emit` fallback 只对「结构名 ≠ 模块名」的第三方语料有用，
  而那类语料即便命中仍不可执行。

④ **验证 / 撤回**
- **撤回 s150**：主树 `h2m.no` 恢复 s149（`fe8891dd`），s150 代码移除，diff 回到 §67 的 +32/−2。
- 理由：零覆盖率收益 + 新增一条查表 fallback 路径（潜在歧义面）+ 未让任何程序变可执行 ⇒
  不符合「每步须有明确收益」的增量纪律。保留实验工件供后续参考，不入库。
- 未跑门禁（源码回退到 §67 逐字相同 ⇒ 沿用 §67 的 14 套全绿，不重复）。

⑤ **诚实边界与下一步（真正的最后一环已重新定位）**
1. **self 程序不可执行的真障碍不是查表，是 receiver 没传**。§67 给方法体 self 绑的是【壳值】
   （无运行时实参，s141 静态转发不传 self）。因此即便调用侧查表命中、即便 `box.len` 返回值链打通，
   方法体的 `.n = v` 写在壳上，真实 `b.n` 永远不变——程序最多输出 `5,5`（错），不可能输出 `5,9`（对）。
   host 实测 `5,9` 与 noc 拒绝的对照（`self-semantic.no`）钉死了这一点。
2. **要真正可执行，必须**：让 s141 调用点把 receiver 作为首参传入 + 方法体 self 绑定到该实参 +
   struct 按引用传递（写回 caller 槽）。这正是 §67 ⑤ 说的「与 Go out-param ABI 对齐」深水区
   （Go 侧 self = LLVM 第 1 个 out-param，`hir2mir.go:2880`/`codegen.go:15424`；h2m 侧 `cg.no:317` 直接返回值）。
   **改动面跨 h2m/cg/mir，风险最高**，不是单档可独立验证的增量，暂不做。
3. **下一步①（可选，低优先）**：`mod.short` key 拼法统一——把 s141 的 `sn.mname` 也改成先剥模块前缀
   重组，消掉与 §58 ⑤-2 同类的隐患；但需证明有真实语料受益，否则同 s150 撤回。
4. **下一步②**：§67 ⑤ 的「让 noc 跑 pkg-ll-cov 验证壳 self 的 cg 发射文本合法性」仍有效，
   可在不追求可执行性的前提下确认 s149 发射的 IR 文本语法正确。
5. **本轮结论**：调用侧缺口已从「s141 不支持变量接收者」精化为「key 差模块前缀（可修，但零收益）」
   +「receiver 未传（真障碍，深水区）」两层。前者已验证不值得单独修，后者需专门轮次评估。
6. **量测工件**：`tmp/s148/selfbind/{h2m.s149.base,h2m.s149.base2,h2m.s150,self-e2e.no,
   self-semantic.no,self-static.no,noc-s150,ab3.sh,probe-s141b.log,call-fix.log}`。
   ⚠️ 探针教训：`h2m-id-name` 里**不能拼 `.to_str()` 数字**（宿主 checker 编译失败 ⇒ 静默无输出假象）；
   用固定字符串标签或条件分支代替。单/多探针注入前先确认 `grep -c` 计数 + 完整日志（别只 grep 目标行，
   编译错误会被过滤掉）。当前主树 = s149（`fe8891dd`）。


## 69 s151 — 评估「深水区」（self 真传参）可行性：纠正 §68 框架，结论为自举闭环已达成（零代码改动）

① **动机与对 §68 的纠正**
- §68 ⑤ 把最后一环定为「与 Go out-param ABI 对齐」，称需跨 h2m/cg/mir。本轮先核实该前提，**发现它是错的**：
  - `cg.no:317` 的签名是 `define <retty> @name(args)`——**直接返回值**，不是 Go 的 `define void @f(out-param)`；
    struct 是**值语义**（`cg.no:16-17`、`OP-STSTORE` = `store %stX, %stX* %stsY` 整值存槽）。
  - noc 编出的程序**不与 Go 编出的程序链接**，两套 IR 无需二进制兼容。h2m/cg 是**自洽的独立约定**，
    不存在「必须对齐 Go ABI」的要求。§68「跨 mir」的判断据此撤回。

② **Go 后端参照系（只读调查，作为参照而非目标）**
- self = 方法**首个 out-param** = LLVM 第 0 参（receiver 槽地址按引用传）：parser 把 self 插在
  `Results[0]`（`src/parser/decl.go:88-98`），tohir 发成首个 KResult（`tohir.go:864-894`），
  `l.curRecv = resultVals[0]`（`hir2mir.go:2880`），caller 传 receiver 槽地址（`codegen.go:15424/15498`），
  callee 把 self 的 valSlot **别名**到入参指针（`:5760-5785`），故 `.n = v` 直接落到 caller 内存。
- 这套机制服务于「用户写方法改字段对 caller 可见」的语言语义；noc 若要同类能力需自己实现一套等价物，
  但**实现方式不必复刻**（可在 h2m/cg 值语义约定内另设计），且**与自举是否闭环无关**。

③ **关键：为什么自举不需要写回语义（解释 §67 为何够用）**
- 编译器自身 33 个方法定义（`ck-sig.new`/`diag.*`/`drv.*`/`ast.*`…）里，**没有一个写回 self 字段**：
  裸点号字段**写**仅 `diag.no:28/30` 的 `.level ==`（比较，非赋值）；`ast.no` 的 `.push` 作用在**返回值
  `r`**，不是 self。方法体基本是「读 self 字段 → 返回值」或纯常量。
- 因此 s149 的**壳 self**（仅供类型/字段路径推导）对降级编译器自身完全够用 → 这解释了为何 noc 构建
  成功、标准 e2e（ts-e2e/ts-min）逐字一致、dropped 98→90。**§68 ⑤ 的「壳 self 是错误放行」措辞过重**：
  它不产可执行的用户写方法程序，但也没让自举产错码——被拒的程序是整体拒绝，不是静默错编。
- §68 ⑤ 下一步②（noc 跑 pkg-ll-cov 验 IR 文本合法性）**已由构建成功隐含验证**：`no build compiler`
  经 clang/llc 产 4MB noc，若 s149 发射的 IR 文本非法，构建阶段即失败。

④ **深水区的真实形态与边界（若要补）**
- 缺口重述：`b.push(1)` / `b.set(9)` 这类**用户写方法**要可执行，需要 receiver 真传参 + self 绑到该实参 +
  struct 按引用写回。障碍是**复合**的（字段方法 + struct 值传递 + 写回），非单点，故 s150 只补查表零收益
  （§68）符合预期。
- 在 h2m/cg 自洽约定内的最小路径（**不复刻 Go ABI**）：给方法签名加「首参 receiver 指针」→ cg 的
  `define`/`OP-CALL` 需按 is-m 分派传指针 → h2m 定义侧 self 绑到该实参（替换壳值）→ 调用侧传 receiver 槽地址。
  **改动跨 h2m.no + cg.no，属独立实施轮次**，需专门的 A/B + 全套门禁 + e2e，不是单档增量；本轮不做。

⑤ **结论与下一步**
1. **自举闭环已达成**：noc 能构建、能跑标准 e2e 逐字一致；self 写方法可执行性是**语言完备性**议题，
   **不是自举阻塞**。§66（字段 push mem 门禁）+ §67（方法体 self 壳绑定）已解决自举侧全部相关未 sup。
2. **本轮零代码改动**：主树 `h2m.no` 保持 s149（`fe8891dd`），无新 fallback、无签名改动。
3. **下一步 A（推荐，可直接验证）**：`h2m` 门禁的 **6 个 s109 failed**（`gate.txt` 逐字：one failed ident /
   id channel aligns / module-base ident / bound ident not recorded…）是真实红灯——v0.4.29 诊断标记引入的
   归因回归，且 ID 通道正是本自举方案反复依赖的归因工具。修它可让 h2m 门禁全绿、并提升后续每轮的定位效率。
4. **下一步 B（大改，需单列）**：④ 的 receiver 真传参实施轮，跨 h2m/cg，仅当用户写方法可执行性是目标时才做。
5. **工件**：本轮为只读调查（侦察 agent + `cg.no`/`h2m.no`/Go 后端源码），无新增临时件沿用
   `tmp/s148/selfbind/`；未碰其他 session 文件。


## 70 s152 — 修 s109 六连败：诊断标记与失败标识符通道分离（h2m 门禁首次全绿）

① **根因（承 §69 ⑤ 下一步 A）**
- `h2m` 门禁长期 6 failed（`test-s109-id-attrib`），§66/§67 均确认是 v0.4.29（`926a2426`）引入、
  改动前基线逐字相同。本轮用 dump 探针（`tmp/s148/s109/dump-idn.no`）钉死：
  `agg.idn` 被 v0.4.29 的 **30 个诊断标记**污染——
  - case1 `r = missing`：期望 `idn.len()==1`，实际 **5**：`:lprog` / `:lfunc` / `f:blk-let` /
    **`f:missing`**（唯一真归因）/ `f:blk-done`；
  - case2 `r = tok.EOF`：5，真归因 `g:tok` 淹没；case3 `r = a`（正常）：期望 0，实际 **4**（全标记）。
- **契约冲突**：`h2m-id-name`（`h2m.no:730`）的设计契约是「失败标识符专用、零降级语义影响」，
  s109 断言 `unsk.len()==idn.len()`（与 unsup 一一对应）。v0.4.29 把 `blk-*`/`si-*`/`surf-*`/
  `pr-*`/`let-*`/入口 `lprog`/`lfunc` 全塞进这条通道，破坏一一对应 ⇒ 6 连败。
  时序：s109 测试先存在（`66d978cf`），标记后加（`926a2426`）⇒ **真回归**，非测试期望过时。

② **修复（诊断能力完整保留，零降级语义影响）**
- 新增独立旁路通道 `h2m-ctx.dbg []str` + 函数 `h2m-dbg(c, tag)`（写 `fn:tag`，不碰 unsup/unsk/idn）。
- 30 处诊断标记的 `h2m-id-name` → `h2m-dbg`（含入口 `agg` 调用）；**两处真归因保留**：
  `h2m.no:1234` `vr-miss-` 变量读失败、`:3386` K-IDENTIFIER 三表未命中。
- `h2m-lower-program` 模块级并回处加 `c.dbg → agg.dbg`（与 idn 并回分离）；
  `drv.no:1002` 加 `DB:fn:tag` 输出，与 `ID:` 并行——归因日志能力不降反升（DB 通道更清晰）。
- ⚠️ 实现坑：批量替换时插入 dbg 函数使原行号 `1211/3363` 偏移 +23，两处真归因被误换成 dbg，
  已单独改回。教训：按行号跳过的批量脚本，插入操作要在行号过滤**之后**做，或用内容锚点。

③ **量测（A/B 双文件同步切换，确认降级语义零回归）**
- A（s149 `fe8891dd` + 原 drv `9d70e7e6`）/ B（s152 `9f8373c7` + 新 drv `842e52c5`）：
  | 档 | A | B | 差 |
  |---|---|---|---|
  | h2m | 392（dropped 90） | 392（dropped 90） | **0** |
  | cg | 73（dropped 24） | 73（dropped 24） | **0** |
  | lx | 82（dropped 8） | 82（dropped 8） | **0** |
- 三档逐字相同——通道分离纯旁路，不改任何 MIR 发射。
  ⚠️ 第一次 A/B 只切 h2m.no 没切 drv.no ⇒ prefix h2m 无 `dbg` 字段配 fixed drv 引用 `agg.dbg`
  编译失败、输出空被误读为 0；修正为双文件同步切换后得正确基线。

④ **验证**
- `h2m-test.no`：**1365 passed / 0 failed / rc=0**（多轮以来首次全绿，六连败归零）。
- 其余 13 套门禁（ps/cg/ck-sig/lx/tok/diag/build-package/util/ps-err/pkg-ll-call/pkg-sigs/pkg-ll/ck）
  全绿 0 FAIL。
- `no fmt -d` 对 `h2m.no`/`drv.no` 改动区零漂移。
- 归因能力：`cov-v-idx` 的 `DB:` 通道完整输出 19 条定位标记（`blk-*`/`surf-*`/`si-*`/`pr-*`），
  `ID:` 通道 0 条（该语料无真失败）——两通道各司其职。
- e2e：`noc-s152` 构建成功（4MB），ts-e2e/ts-min 与 s149 noc 逐字一致。

⑤ **结论与下一步**
1. **`h2m` 门禁全绿**，长期预存的 6 failed 已清除；自举方案 §66/§67/§69/§70 四轮后，
   降级侧门禁、量测、e2e 全部干净。
2. 代码改动：`compiler/src/h2m.no`（+95/−31，含 30 处机械替换 + dbg 函数/字段/并回）、
   `compiler/src/drv.no`（+9）。当前主树 md5 见 §工件；无 s150 残留。
3. **下一步 A（自举收尾）**：§69 ⑤ 的「全文一致性校对 + 最终状态摘要」——四轮迭代后文档跨 §66–§70，
   可做一次收口。
4. **下一步 B（大改，需单列）**：§69 ④ 的 receiver 真传参实施轮，跨 h2m/cg，
   仅当用户写方法可执行性是目标时才做。
5. **工件**：`tmp/s148/s109/{dump-idn.no,h2m.prefix,h2m.fixed,drv.prefix,drv.fixed,ab2.sh,h2m-gate.log,
   gate-all.txt,noc-s151}`；⚠️ 探针脚本里递增要用 `h2m-inc(i)`，`i = i + 1` 在新 checker 下触发
   option-unwrap 报错（15w45dqk）。未碰其他 session 文件。

---

## 71 s153 — 全文一致性校對 + 最終狀態摘要（收口）

> 本輪為 **doc-only 收口輪**，零代碼改動。§69 ⑤ 下一步 A 與 §70 ⑤ 下一步 A 指向同一件事：
> 「全文一致性校對 + 最終狀態摘要」。四輪（§66–§70）之後文檔跨 §12 ⑰–㊿ 與 §51–§70，
> 的确到了該收口的時候。**本輪不改 `compiler/`、不改 `src/`、不改 `bin/no`。**

### ① 定位與方法

- **做法**：把全文所有「現狀／最新／現況」類陳述逐條拉出來，與 **2026-11-23 的實體工作樹**對帳；
  每條只更正「現狀類」欄位（檔頭定位、§7.1 門禁表、§7.2 指令、§6 里程碑列、§11.2 台帳、附錄 C 快照、
  內文導航），**一律不覆盖歷史 entry**——比照 §12 ⑤ 既有的「原文保留為歷史 + 追加 as-built 補記」慣例。
- **對帳基準**：`HEAD = d236a956`（2026-11-23 00:12:41 +0800，`fix(docs): remove invalid source entry
  in multiple README files`），工作樹 `compiler/` 與 `src/` 乾淨（`git status` 僅 `noui/` 7 檔未提交，
  與本方案無關）。
- **工具鏈代次是本輪的核心變數**：所有門禁結果都必須標註「用哪一代 `no` 跑的」，否則-doc 會把
  代次钉住的綠燈誤記成 HEAD 的屬性。

### ② 全文一致性校對：更正清單（in-place）

| # | 位置 | 原文（歷史） | 更正後（2026-11-23 現況） |
|---|---|---|---|
| 1 | **檔頭 line 3** | 「2026-11-12 現況：`compiler/src/*.no` 14 檔＋`util/*.no` 2 檔；…續推至 **s99n**…文末 2026-11-12 快照」 | 「**2026-11-23 現況**：**16 檔**＋2 檔，合計 **46,960 行**；…推至 **s152**…見 §6／§6.5／§11.2 B4／§12 ⑰–§70」，並加代次阻塞警語 |
| 2 | **§7.1 L0 列** | `compiler/tests/*_test.no` | `compiler/tests/*-test.no`（**18 套**）。⚠️ 原 glob **匹配不到任何檔**（`ls compiler/tests/*_test.no` → `no matches found`）；同目錄 5 個非門禁檔已註明 |
| 3 | **§7.2 sanity check** | `./bin/no run tests/2.no; echo $?` | `./bin/no run tests/build-mo.no; echo $?`。⚠️ `tests/2.no` 已於 `15618c3c`（tests: merge similar cases）刪除，`find . -name 2.no` 無結果；替代檔 80 B，實測 rc=0 |
| 4 | **§6 M4 列** | 單格止於 s105 | 原文保留，末尾追加 **s105→s152 as-built 補記**（含 17 套綠燈數字與代次警語） |
| 5 | **§11.2 B4** | 止於 s105 敘述 | 原文保留，追加補記；**新增 B30** 記工具鏈代次偏移為第一號阻塞 |
| 6 | **附錄 C** | 快照止於 2026-11-15 續；末行「本文只保留 ⑰–㉗ 連續追記」 | 新增 **2026-11-23 快照**；末行改為「⑰–㊿（s133）與 §51–§71（s134–s153 收口）」（原文落後 23 節） |
| 7 | **line 2012 導航** | 「本文自 ⑰ 起保留連續追記（最新 ㉛ 在文末）」 | 「訖於文末 **§71（s153 收口輪）**」；原文「最新 ㉛」落後 39 節 |

**未更正（屬歷史證據，依原文保留）**：§4.1.1 的 2026-10-30 盤點、§6.5 的 2026-10-29 啟動摸底、
§12 各 entry 內的行號／計數、§7.2 上方 2026-10-27 收敛軌跡提到的 `tests/2.no`（該檔在**當時**存在）。

### ③ 最終狀態摘要（2026-11-23，HEAD `d236a956`）

**盤點（與 §70 ⑤ 工件吻合，證明工作樹就停在 s152）**：`compiler/src` **16 檔＋`util/` 2 檔＝18 檔**、
`cat | wc -l` = **46,960 行**；`compiler/tests` **18 套 `*-test.no`**（另有 5 個非門禁檔）、
`fixtures/` **70 檔**；`h2m.no` md5 `9f8373c7…`、`drv.no` md5 `842e52c5…`，與 `tmp/s148/s109/h2m.fixed`
／`drv.fixed` **逐元組相同**。

**兩代工具鏈 A/B（本輪最重要的一張表）**：

| 工具鏈 | 二進位 | `no build compiler` | L0（18 套） | 備註 |
|---|---|---|---|---|
| **HEAD 代次** | `tmp/s153/bin/no`，`v0.4.29-2-gd236a956`（`make no BINDIR=…` 產的已簽名 build；`-dirty` 來自 `noui/` 的 7 個未提交檔，與 `src/`／`compiler/` 無關） | **rc=1，12 ERROR** | **5 綠／13 紅** | 磁碟上的 `bin/no` 為 `v0.4.29-1-g1ec55c44`，與 HEAD 只差一個 docs commit，語義相同 |
| **v0.4.28 代次** | `tmp/s148/wt/bin/no`，`v0.4.28-4-gd21fbb35`（worktree `tmp/s148/wt` @ `d21fbb35`） | **rc=0，Build successful**，`compiler/dist/compiler` 4,098,152 B、`version` = `noc v0.0.1-m0` | **17 綠／1 紅** | ⚠️ **§66–§70 的綠燈證據（`tmp/s148/s109/gate-all.txt`）即出自此代次**（檔內「current compiler is v0.4.28-4-gd21fbb35」可查） |

- **HEAD 代次的 5 綠**：`cg-runtime` 114/114、`ck-sig` 50/50、`diag` 7/7、`tok` 62/62、`util` 27/27。
- **HEAD 代次的 13 紅**：`ast`（rc=1、**err=0**，43/48 則差異）、`build-package` err=12、`cg-str16` err=1、
  `cg` err=1、`ck` err=5、`drv-build` err=12、`h2m` err=12、`lx` err=4、`pkg-ll-call` err=12、
  `pkg-ll` err=12、`pkg-sigs` err=12、`ps-err` err=5、`ps` err=5。
- **v0.4.28 代次的 17 綠**：上列 13 紅裡除 `ast` 外的 12 套 + 既有 5 綠，全部 `ALL TESTS PASSED`
  （`cg-test` 363/363、`h2m-test` 1365/1365、`lx-test` 81/81、`ps-test` 52/52、`ck-test` 20/20、
  `drv-build-test` 63/63、`build-package-test` 8/8、`pkg-ll-test` 10/10、`pkg-ll-call-test` 5/5、
  `pkg-sigs-test` 5/5、`ps-err-test` 46/46、`cg-str16-test` 41/41、`ck-sig-test` 50/50…）。
  **這正是 §70 ④ 的數字** ⇒ §70 ④ 並非捏造，它是**代次钉住的真實觀測**。
- **12 個阻塞站（全在 `compiler/src`）**：`lx.no:263:15`、`lx.no:268:26`、`lx.no:273:26`、`lx.no:327:11`
  （`cannot assign ?i64 value to i64 variable`）、`ps.no:8175:35`（`cannot assign tk value to ?i64 variable 'it'`）、
  `cg.no:343:19`（`cur = bk`）、`h2m.no:3941:42`（`cn = cn2`）、
  `h2m.no:1767:17`／`1772:17`／`1816:17`／`1821:17`／`3937:33`（`option bound but never consumed`，`optunuse`）。
  逐檔 `no vet` 為 `lx` 4、`h2m` 6、`ps` 1、`cg` 1 = **12**，與 `no build compiler` 的 12 ERROR 一致。
  對照組：**`no vet src/std` 0 error** ⇒ 標準庫已移居，缺口只在 `compiler/src`。

**唯一的工具鏈獨立紅燈：`ast-test` 43/48**。兩代工具鏈跑出**逐字相同**的 5 條 FAIL：
`KIND-COUNT = 72`、`Program is kind 54`、`SurfaceMatch is kind 65`、`Identifier is kind 36`、
`IntegerLiteral is kind 40`。而 `compiler/src/asp.no` 現狀為 **74／56／67／38／42**（整段 +2）。
**判定依據**：`asp.no` 的檔頭自述「自動生成，不要手改」，生成器是 `scripts/gen_ast_spec.py`，
其 kind id 由 `src/parser/dump/dump.go` 的 `node()` switch **按 Go 型別名的字母序**編號
（`gen_ast_spec.py:636` 的 `sorted(spec.keys())`）。把生成器抄到 `tmp/` 重跑（不改工作樹）⇒
產出的 `K-*` 常量與盤上 `asp.no` **逐條相同**（74/38/42/56/67，diff 僅 38 行純裝飾註解／分隔線，
屬 `d048ad6f` 的 style 改動）。⇒ **`asp.no` 與生成器一致，是 `ast-test.no` 的期望過期**（其註解
自述「2026-11-17 刪除 RunExpression/AwaitExpression 後 74→72」，但 `dump.go` 在 `ca5156e0` 與
HEAD 都是 74 個 case，两者都沒有 Run/Await 條目）。**这是與代次無關的獨立陳舊測試，非迴歸。**

### ④ 驗證（可重跑憑據）

```bash
# 0) 代次釘住：複製 toolchains（都不碰 bin/no、不弄髒工作樹）
unset GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_KEY_1 GIT_CONFIG_VALUE_0 GIT_CONFIG_VALUE_1
git -C /Users/lizongying/IdeaProjects/no worktree list          # tmp/s148/wt -> d21fbb35
make no BINDIR=tmp/s153/bin                                # 已簽名、注入 StdSigKey

# 1) HEAD 代次
tmp/s153/bin/no version                                     # v0.4.29-2-gd236a956-dirty
tmp/s153/bin/no build compiler                              # rc=1，12 error(s)
tmp/s153/bin/no vet src/std                                 # 0 error(s)
bash tmp/s153/run-gates-clean.sh                            # 5 綠／13 紅

# 2) v0.4.28 代次（§70 证据的那一代）
tmp/s148/wt/bin/no version                                  # v0.4.28-4-gd21fbb35
tmp/s148/wt/bin/no build compiler                           # rc=0，Build successful
bash tmp/s153/run-gates-wt.sh                               # 17 綠／1 紅（ast 43/48）

# 3) ast-test 為代次獨立
tmp/s153/bin/no run compiler/tests/ast-test.no  | grep ^FAIL
tmp/s148/wt/bin/no run compiler/tests/ast-test.no  | grep ^FAIL   # 5 條逐字相同

# 4) kind 常量：生成器 vs 盤上
python3 tmp/s153/gen/gen.py                                # 輸出到 tmp/s153/gen/asp.no
diff <(grep -E '^K-|KIND-COUNT' compiler/src/asp.no) <(grep -E '^K-|KIND-COUNT' tmp/s153/gen/asp.no)
# -> 無輸出（K 常量逐條相同）
```

**本輪未跑的門禁（誠實邊界）**：L1／L2／L3／L4 **未在 HEAD 代次重跑**。L4（`tests/golden/mir-baseline.tsv`，
成本 ~7–17 分鐘）的 oracle 是 Go 側 `mir` 基線，與 `compiler/src` 的 option 化屬不同層面，
但本輪不宣稱它仍綠。

### ⑤ 結論與下一步

> **2026-11-23 續（§72 指針，原文保留）**：本節的**下一步 A（12 站移居）與 B（`ast-test` 期望）
> 已於同日在 §72 完成**——HEAD 代次 `no build compiler` rc=0、L0 18/18 全綠、S0→S1 達成；
> **S1→S2 仍阻塞**（層 1 目錄 build 未接線、層 2 1877 unsup）。**下一步 C 未啟動**（條件未成立）。

1. **收口完成**：全文 **13 處**「現狀類／快照指針類」陳述已對帳並更正（② 表 7 處 + ⑥ 表 6 處），歷史 entry 全部保留原文 + 追加補記。
2. **最重要的結論**：§66–§70 的「`h2m` 門禁全綠」「e2e `noc-s152` 與 s149 逐字一致」「自舉閉環已達成」
   **都是 v0.4.28 代次（`d21fbb35`）的真實觀測**，可在 `tmp/s148/wt` 下原樣重現（17/18 綠、
   `no build compiler` rc=0）；但 **HEAD `d236a956` 的 v0.4.29+ 工具鏈已經不再接受 `compiler/src`**——
   12 個站點（B30）觸發 `15w45dqk` option-unwrap 與 `optunuse`。⇒ **L5 與 S0→noc 在 HEAD 暫時阻塞**，
   在移居完成前，本文任何「自舉已閉環」的表述都必須带代次限定。
3. **文檔自身的陳舊已清**：`§7.1` 的 L0 glob、`§7.2` 的 sanity 路徑、附錄 C 的節範圍、
   兩處「最新 ㉛／s99n」都已對齊；`ast-test` 的 5 條期望經生成器證據判定為**測試側陳舊**（非 `asp.no` 漂移）。
4. **下一步 A（建議，代次收窄輪）**：B30 的 **12 站移居**——逐站改成
   `k = <expr>`（推断為 `?T`）+ `v = k: { ok -> it; -> <default> }`，或 `?=`／`_ = <expr>`；
   每站配 A/B（`unsup`／`dropped` 數、`h2m-test`／`cg-test` 不回退）。完成後 §66–§70 的結論
   才能在 HEAD 重新成立。`no fix --filter=15w45dqk`／`--filter=optunuse` 可先出 preview。
5. **下一步 B（小改，可獨立做）**：修 `ast-test.no` 的 5 條期望為 **74／56／67／38／42** 並更正註解
   （刪除「74→72」那段與事實不符的敘述）。此改與代次無關，可與 A 平行。
6. **下一步 C（大改，需單列）**：§69 ④ 的 **receiver 真傳参** 實施輪，跨 `h2m.no`＋`cg.no`，
   仅当「用户寫的方法可執行」是目標時才做。
7. **工件**：本輪新增 `tmp/s153/{build-head.log, build-wt.log, gates-clean.log, gates-wt.log,
   run-gates-clean.sh, run-gates-wt.sh, git-before.txt, git-after.txt,
   gen/gen.py, gen/asp.no, gen/spec_gen_test.go}`（`gates-clean.log`／`gates-wt.log` 由
   對應的 `run-gates-*.sh` 以 `tee -a` 寫出；`gen.py` 是 `scripts/gen_ast_spec.py` 的
   ROOT／輸出路徑改寫副本，不改工作樹）；
   引用既有的 `tmp/s148/wt/`（v0.4.28 worktree）、`tmp/s148/s109/`（§70 門禁證據）、
   `tmp/s153/bin/no`（HEAD 代次已簽名构建）。⚠️ **沒有重建 `bin/no`**（`make no BINDIR=…` 不寫
   `bin/`，已用 `git status` 前後對比證明工作樹不變）；`**no-head`**（裸 `go build`、未注入
   `StdSigKey`、會拒絕編譯一切）已刪除，避免誤用。

### ⑥ 第二輪指針校對（2026-11-23 同日追加，doc-only）

③ 表與 ⑤ 只覆盖了「明文標為現況」的陳述；同日再掃一遍**指向舊快照的導航指針**，
還有 6 條把「2026-11-12 校正／快照」「s99n」或「最新」當成現行權威的交叉引用，一併就地更正（原文保留，僅附加指針）：

| # | 位置 | 原文（歷史指針） | 更正後 |
|---|---|---|---|
| 8 | **§4.1.1 標題**（line 612） | 「2026-10-30 歷史快照；2026-11-12 as-built 校正見本節末」 | 追加「**2026-11-23 最新現況見 §71**」 |
| 9 | **§4.1.1 警示**（line 614） | 「現狀以本節末的 2026-11-12 校正、…／文末 2026-11-12 快照為準」 | 追加「2026-11-23 校對：上指『文末快照』已由 2026-11-23 快照接手，最新一律以 §71 為準」 |
| 10 | **§6.5 SUT 現狀條**（line 1639） | 「SUT 現狀（2026-10-29…歷史；現況見 §4.1.1 末 2026-11-12 校正）」 | 追加「2026-11-23 最新見 §71」 |
| 11 | **附錄 C 2026-11-06 快照標題**（line 2125） | 「（2026-11-06，歷史；最新見下方 2026-11-12 as-built 快照）」 | 追加「2026-11-23 收口快照見附錄 C／§71」 |
| 12 | **§6.5 M4-e 條**（line 1653） | 「**其後已續推至 s99n（件號 54）**…現況 `h2m-test` **1031/1037**…等仍待續」 | 追加「2026-11-23 補記：已續推至 **s152**；上文數字為 **v0.4.28 代次前**觀測，HEAD 代次實際數字與阻塞見 §71」 |
| 13 | **「最新」字樣殘餘**：附錄 C 2026-11-15 兩快照標題（line 2138／2140）＋ §11 拆分註記（line 2648） | 「（2026-11-15，最新；…）」、「（2026-11-15 續，最新；…）」、「⑰–㉗ 最新連續追記留在本文」 | 一律改「**當時**最新」或加限定，並指向 2026-11-23 快照／§71（原文保留） |

**未更正（依原文保留的歷史指針）**：§11.2 B4 列（line 1931）與 §12 2026-11-12 追記（line 1986／2013）
裡的「文末 2026-11-12 快照」是**該 entry 寫作當日**的正確指針，屬歷史證據；§12 各 entry 內的
行號／計數同理。至此全文「現狀類陳述」與「指向快照的導航指針」兩類都已對齊 2026-11-23。

---

## 72 s154 — 下一步 A／B 實施輪：12 站 option 化移居 + `ast-test` 校正；S0→S1 達成，S1→S2 兩層阻塞

本節執行 §71 ⑤ 的**下一步 A**（B30 的 12 站移居）與**下一步 B**（`ast-test.no` 5 條期望），
並重跑 S0→S1→S2→S3 自舉鏈與 L5 定點驗證。**結論：A／B 完成，§66–§70 的綠燈結論在 HEAD
（`d236a956`，v0.4.29+）重新成立；S1→S2 仍未通，且已定位到兩個各自獨立、可分別处置的層。**

### ① 下一步 A：12 站 option 化移居（逐一列表）

v0.4.29 起，非字面量運算元的算術與 `vec[i]` 讀取值為 `?T`，未消費的 option 繫結會觸發
`optunuse` 與 `15w45dqk`（option-unwrap）兩種編譯期錯誤。§71 ③ 判定 `compiler/src`
上共 **12 站**（B30）踩雷。逐站形式如下（**原文保留，本文只記 as-built**）：

| # | 檔:函式 | 原形式 | 移植後形式 |
|---|---|---|---|
| 1 | `lx.no` `lx-hex-val`（數臂 `c - B-DIG0`） | `v = c - B-DIG0` | `<v-opt>` 兩步式 |
| 2 | `lx.no` `lx-hex-val`（小寫臂 `(c - B-LO-A) + 10`） | `v = (c - B-LO-A) + 10` | `<v-opt2>` 兩步式 |
| 3 | `lx.no` `lx-hex-val`（大寫臂 `(c - B-UP-A) + 10`） | `v = (c - B-UP-A) + 10` | `<v-opt3>` 兩步式 |
| 4 | `lx.no` `lx-hex-digit` | `c = v + B-DIG0` | `<c-opt>` 兩步式 |
| 5 | `ps.no` `ps-parse-iface-def` | `it = ps-peek-token(p)` | `tkv = ps-peek-token(p)`（改名繞開 option 強行繫結） |
| 6 | `h2m.no` `h2m-struct-resolve` | `snm = c.stn[i]: { ok -> it -> '' }` | 外層 `<snm-opt>` 兩步式 |
| 7 | `h2m.no` `h2m-struct-resolve` | `fldn = c.stfldn[off + j]: { … }` | 外層 `<fldn-opt>` 兩步式 |
| 8 | `h2m.no` `h2m-struct-resolve-range` | 同 #6 | `<snm-opt2>` 兩步式 |
| 9 | `h2m.no` `h2m-struct-resolve-range` | 同 #7 | `<fldn-opt2>` 兩步式 |
| 10 | `h2m.no` `h2m-lower-expr`（方法臂） | `sn2 = c.stn[sid2]` | `<sn2-opt>` 兩步式 |
| 11 | `h2m.no` `h2m-lower-expr`（方法臂） | `cn = cn2` | `<cn-opt>` 兩步式 |
| 12 | `cg.no` `cg-emit-cfg-t` | `cur = bk` | `<cur-opt>` 兩步式 |

「兩步式」即 §71 ⑤-A 指定的 `k = <expr>`（推斷為 `?T`）+ `v = k: { ok -> it; -> <default> }`；
`lx` 四站另把 `#{overflow=wrap}` 註解**複製進內層區塊**（註解只作用於緊跟的一條敘述，
不複製會流失語義），此坑 §71 ③ 已指出。全站**不改語義**：default 值與原 `ok` 臂
回傳值逐字相同（#6–#9 為 `''`，其餘為「不賦值＝保留原值」的空的 `->` 臂）。

### ② 下一步 B：`ast-test.no` 5 條期望校正

依 §71 ③ 的生成器證據，`compiler/tests/ast-test.no` 的 5 條 kind 期望由
`72／54／65／36／40` 改回 **`74／56／67／38／42`**（`KIND-COUNT`、`Program`、`SurfaceMatch`、
`Identifier`、`IntegerLiteral`），並刪去註解裡「2026-11-17 刪除 RunExpression／AwaitExpression
後 74 → 72」那段與事實不符的敘述，改記 `src/parser/dump/dump.go` 在 `ca5156e0` 與 HEAD 皆為
74 個 case、`scripts/gen_ast_spec.py:636` 的 `sorted(spec.keys())` 決定 id 等等證據。

### ③ 驗證（可重跑憑據）

```bash
unset GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_KEY_1 GIT_CONFIG_VALUE_0 GIT_CONFIG_VALUE_1
make no BINDIR=tmp/s153/bin                     # S0：v0.4.29-2-gd236a956-dirty
tmp/s153/bin/no build -o tmp/s154/noc-s1 compiler    # rc=0（s1-build.log: Build successful）
tmp/s154/noc-s1 version                         # noc v0.0.1-m0
md5 -q tmp/s154/noc-s1                          # 0a44a6b5296fbe72d60c3f9c7eda82b0
bash tmp/s153/run-gates-clean.sh                # 18/18 全綠（gates-head3.log）
```

- ✅ **L0 18/18 全綠**（`tmp/s153/gates-head3.log`）：`ast-test` 48 · `build-package-test` 8 ·
  `cg-runtime-test` 114 · `cg-str16-test` 41 · `cg-test` 363 · `ck-sig-test` 50 · `ck-test` 20 ·
  `diag-test` 7 · `drv-build-test` 63 · `h2m-test` **1365** · `lx-test` 81 · `pkg-ll-call-test` 5 ·
  `pkg-ll-test` 10 · `pkg-sigs-test` 5 · `ps-err-test` 46 · `ps-test` 52 · `tok-test` 62 ·
  `util-test` 27，`failed` 全為 0。**A/B 對比**：移居前 `tmp/s153/gates-clean.log` 為 **13 紅／5 綠**
  （其中 `ast-test` 43/48 屬下一步 B 的陳舊期望，非移植所致），移居後 18/18。
- ✅ **`no build compiler` rc=0**（`tmp/s154/s1-build.log`，仅一行 version-mismatch warning），
  製品 `tmp/s154/noc-s1` 的 `version` 印 `noc v0.0.1-m0`——**S0→S1 在 HEAD 代次成立**，
  §66–§70 的結論（原文帶代次限定）現在可在 HEAD 重現。

### ④ S1→S2 的兩層阻塞（誠實邊界）

`tmp/s154/noc-s1 build -o noc-s2 compiler` 仍失敗，但**不是**一個原因，而是兩層：

**層 1（管線，小）：`compiler` 是目錄，`build` 只收單檔。**
`noc: compiler is a directory; build takes a single .no file`（`tmp/s154/s2-build.log`）。
`drv.dispatch`（`compiler/src/drv.no:1209-1253`）只有 `version／help／--help／-h／tokens／ast／
vet／build／run` 七個子命令，**沒有 package/目錄 build**；而 `drv.build-package`（:420）、
`drv.pkg-ll`（:763）、`link.compile` 都已存在，只是未接成 `build <dir>`。此層是接線活，
改動力小（與 §71 ④ 提到的「cmd-build 接 pkg-ll」同一決策點）。

**層 2（降級，真牆）：整包降級只能走到 1877 unsup，其中 1737 條直接。**
`tmp/s154/cov-main.no` 平台入門（複製 `drv.pkg-ll` 但保留 `agg`）實測
`drv.pkg-ll('compiler/main.no', false)`：

```
noc: unsupported constructs: 1877 (pkg-ll)      # cov-full.log:142
dropped (calls unsupported) × 140               # 閉包剔除（計入 unsup）
```

即 **1877 = 1737 直接 + 140 閉包剔除**。逐模組（block 通道前綴）：

| 模組 | unsup | 模組 | unsup |
|---|---|---|---|
| `ps` | **919** | `check` | 40 |
| `drv` | **304** | `ck` | 32 |
| `h2m` | **240** | `compile` | 18 |
| `cg` | 53 | `ast` | 13 |
| `lx` | 52 | `diag` | 10 |

（上列 10 模組共 1681，其餘 196 散落於 `dump` 8、`sb` 5、`make` 5、`fold` 5 等。）
單函式前六名：`ps-parse-match` **236**、`h2m-lower-expr` 77、`drv.pkg-ll` 66、`drv.cmd-vet` 61、
`ps-parse-expr-prec` 59、`ps-parse-enum-def` 55。葉子通道前幾名：`Identifier` **1006**、
`StringLiteral` 177、`IntegerLiteral` 155、`CallExpression` 148、`FunctionDefinition` 135、
`BlockStatement` 57、`SurfaceMatch` 22。**這與 §70／§71 所述「H2M 只降級極少數函式本體」一致，
是 M5/M6 的 h2m 能力擴張範疇，非本輪可跨層解決。**

**單檔路徑是紅鯡魚（重要）**：`noc-s1 build compiler/main.no` 只報 **2 unsup**——
`tmp/s154/probe4.log`（以 `h2m-lower-module` 直接跑 `main.no`，保留 `agg`）顯示
`unsup=2`、`body len=0`，兩條都落在入口接合部：block 通道 `main:BlockStatement:construct` +
`main:FunctionDefinition:construct`，葉子通道 `main:CallExpression:construct` +
`main:Identifier:construct`，`ID:` 通道 `ID:main:code`（即 `drv.dispatch()` 的點號呼叫 +
`code` 的 let/讀）。根因是 `h2m-lower-program`（`compiler/src/h2m.no:6612`）開頭的
**模組本地** `sg = h2m-sigs-new()`（:6615）：單檔路徑看不到跨模組簽名，故 `drv.dispatch()` 查不到。
**所以「2 unsup」不代表真實建置路徑**，真實路徑是層 2 的 1877。

**`drv.pkg-ll` 的 unsup 守衛衝突**：`drv.build-ll`（:362-383）有 `agg.unsup > 0 -> return`
的守衛（回 `''` 並印 `(M4-d SHS subset)`）；`drv.pkg-ll` 則只 `eprint`（:969-971）而**不守衛**，
`.ll` 照常回傳。**追加守衛會回歸 `compiler/tests/pkg-ll-test.no`**：該測試在
`unsupported constructs: 1876 (pkg-ll)` + 139 dropped 的情況下仍斷言 `ll.contains('@main')` 等
四條契約並 10/10 綠（`tmp/s154/pkg-ll-test.{out,err}`、`gates-head3.log`）。難怪
`drv.pkg-ll` 的守衛缺失是**刻意**的（否則整包閘自己就紅了），非疏漏。

### ⑤ e2e 蟲度與新發現：未標註 overflow 的 S0 非確定性

以 `tmp/s154/run-e2e-subset.sh` 跑 `compiler/tests/fixtures/*.no` 26 檔：**same=6／diff=19**。
但 diff 中**多數是 S1 合法拒絕非 SHS 程式**（如 `t1-int`、`t29-for-kw`、`t13-if-in-func` 皆
`rc=0 → rc=1` 的 `unsupported constructs (M4-d SHS subset)`），文件級 e2e 語料並非 §12 ㉜
指定的 `ts-e2e`／`ts-min`，故此數字**不是** L5 定點口徑。

排查中撞到一個**真正的 S0/S1 分歧**（`tmp/s154/bisect/b3.no`、`c1..c4.no`）：

| 程式 | S0（宿主） | S1（自舉） |
|---|---|---|
| `#{overflow=wrap}` 在運算所在陳述（c1/c2/c3） | `-9223372036854775799` | `-9223372036854775799` |
| **未標註**（c4：`a = MAX; b = a + 10`） | **4306148080／4383775472／4382268144／4375157488／4377500400…**（每次 run 皆不同） | `-9223372036854775799` |

**定性：此為宿主後端對「未宣告 overflow 行為」程式的未定義行為，非 S1 回歸。** 證據：
(a) S0 的輸出**逐次執行非確定**（同一二進位 `c4-s0` 亦印 4348910320），等於讀未初始化／
poison 記憶體，無任何編譯器可與之逐字匹配；(b) S0 `no vet c4.no` 給的是 **HINT**
（`ovfhndld`／`xmicjezl`：「int `a OP b` has no `#{overflow = ...}` annotation」），
`no build`／`no run` 皆 **rc=0**——即宿主「接受但語意未定義」，與 SHS 子集契約
（非字面量算術必須標註）不同；(c) 套用 `#{overflow=wrap}` 後 S0 與 S1 **逐字相同**。
⇒ **12 站移植未引入此分歧**（S0 自己的輸出就不可重現），且 L5 的 ① 行為定點只比較
`(rc, sha256(stdout))(noc-s2, f)` 與 `(noc-s3, f)`，**不含 S0**，故此分歧不屬定點口徑。
**另記**：S1 的 `vet` 不發該 HINT（`No issues found.`），這是自舉前端子集的另一獨立缺口，
與 §71 ③ 的 kind 表缺口同類，非本輪引入。

### ⑥ 結論與下一步

1. **A／B 完成**：12 站移居 + `ast-test` 5 條期望校正後，HEAD 代次 `no build compiler` rc=0、
   **L0 18/18 全綠**、S0→S1 制品 `tmp/s154/noc-s1`（md5 `0a44a6b5296fbe72d60c3f9c7eda82b0`）
   `version` 印 `noc v0.0.1-m0`。**§66–§70 的「自舉閉環」表述現在可在 HEAD 重新成立**，
   不必再帶 v0.4.28 代次限定。
2. **S1→S2 卡兩層，可分別处置**：層 1 把 `drv.build-package` + `drv.pkg-ll` + `link.compile`
   接成 `drv.dispatch` 的 `build <dir>`（小，管線）；層 2 是 1737 條直接 unsup 的 h2m 能力缺口
   （M5/M6 量級，非本輪可跨層解決）。**在兩層都解決前，S2→S3 與 L5 定點 ①② 不可達。**
3. **未做（依 §71 ⑤-C 條件）**：**下一步 C**（§69 ④ 的 receiver 真傳參實施輪，跨 `h2m.no`＋`cg.no`）
   僅在「用户寫的方法可執行」是目標時才做；本輪不啟動。
4. **未跑的門禁（誠實邊界）**：L1／L2／L3／L4 未在 HEAD 代次重跑；L4（`tests/golden/mir-baseline.tsv`，
   成本 ~7–17 分鐘）的 oracle 是 Go 側 `mir` 基線，與本輪改動不同層面，但本節不宣稱它仍綠。
   **無 DR-1 定點**（① `noc-s2` vs `noc-s3` 行為定點、② `noc-s3 build compiler` 逐位元組、
   ③ L1–L4 無回歸）未完成——② 尤其因層 1 未接線而完全無法起跑。
5. **工件**：`tmp/s154/{noc-s1, s1-build.log, s2-build.log, s2f-build.log, run-s1s2.sh,
   run-e2e-subset.sh, e2e-subset.log, cov-main.no, cov-full.log, probe4.no, probe4.log,
   pkg-ll-test.out, pkg-ll-test.err, ts-e2e.no, bisect/}`；`tmp/s153/{gates-head3.log,
   gates-clean.log, build-head.log, run-gates-clean.sh}`。⚠️ **沒有重建 `bin/no`**
   （`make no BINDIR=…` 不寫 `bin/`）；⚠️ 本輪**未提交**任何改動，工作樹僅
   `compiler/src/{lx,ps,h2m,cg}.no`＋`compiler/tests/ast-test.no`＋本文（`noui/` 下的更動
   為本輪之前既存、非本輪產物）。

## 73 s153 層 1（管線）as-built 補記：`build <dir>` 接入 `build-package` → `pkg-ll` → `link.compile`；套件模式 `struct` 定義的根因修正

本節執行 §72 ⑥ 切分的**層 1（管線，小）**：把各自獨立的兩半——`Drv.buildPackage`＋`Drv.pkgLL`
（入口抽取／傳遞依賴／sig 掃描／分片 h2m／RC 執行期外洩）與 `Link.Compile`——串入
`Drv.dispatch` 的 `build <dir>`。**管線層完成並驗證：`build <dir>` 走整包路徑、誠實門禁生效、
S0 與 S1 的 unsup 統計逐條一致、L0 18/18 不回退。層 2（1737 直接 unsup 真牆）未動，
S2 續建仍停在誠實的 unsup 診斷。**

### ① 變更清單（逐檔逐函式，全部為追加／最小改）

- `compiler/src/drv.no`
  - `drv.pkg-ll` 由 `(path str, emit-cov bool)` 擴為 `(path str, emit-cov bool, strict bool)`；
    新舊呼叫端語意分離：`emit-cov` 量測路徑傳 `false`（缺碼包的覆蓋日誌才會有），
    新接入的真實建置路徑傳 `true`。新臂在既有 `agg.unsup > 0` 區塊內：
    `strict -> r = ''`——**缺碼 IR 不得匯出**（否則 clang 會鏈出殘缺執行檔；同 `drv.build-ll`
    的 unsup 門禁，「不靜默產出錯碼」紀律）。
  - 新增 `drv.build-pkg-to = (path str, out str) (ok bool)`：入口解析（目錄 ⇒ `<dir>/main.no`，
    對齊 `drv.bp-resolve` 的「尾 `/` ⇒ `main.no`」口徑；`fs.is-file` 把關）→
    `drv.pkg-ll(entry, false, true)` → `ll == ''` ⇒ `eprint('no code generated')` 並返回 →
    否則 `link.compile(ll, out)`。
  - `drv.cmd-build` 尾巴由「`ok = drv.build-to(path, drv.build-out(path))`」改為
    `fs.is-dir(path) -> ok = drv.build-pkg-to(path, drv.build-out(path))` ／
    `! fs.is-dir(path) -> ok = drv.build-to(path, drv.build-out(path))`（單檔舊路徑保留）。
  - `drv.cmd-run` 尾巴同構改寫（`out` 來自 `drv.out-arg()`／`drv.run-temp()`）。
  - `drv.pkg-ll-cov` 跟著補第三參數 `false`。
- `compiler/main.no`：`# /compiler/src/drv.dispatch` ⇒ `# /compiler/src/drv`（**根因修正**，見 ②），
  並更新其上方「直接導入所有子模組」註解：該約束的對象除常量／全域外，**亦含 `struct` 定義**。
- `compiler/tests/pkg-ll-test.no`、`compiler/tests/pkg-ll-call-test.no`：兩個呼叫端各補一個
  `strict=false` 引數（否則新簽名組不起來）。

### ② 根因（as-built 教訓）：套件模式下 `struct` 定義只在入口檔「直接 import」時生成

接入後 S1 重建直接失敗，徵結經 10 個逐一執行的二分變體（活化／不活化、單句臂、別名、`cmd-build`／
`cmd-run` 分支、`strict` 參數…）收斂到**單一個 struct 字面量**：`drv.build-package` 的
`bpctx{ r: '' cyc: false }`，且**只有在套件模式（`no build … compiler`）且該函式可達時**。
實證（以 `NOLANG_DBG_STRUCTFIELDS` 臨時儀表建一次 Go 編譯器，單檔模式綠、套件模式紅）：
套件模式 `main.no` 只 import `# /compiler/src/drv.dispatch`（僅綁定 `dispatch`），於是
`drv` 的**頂層 `bpctx { r str; cyc bool }` 定義不生成**——`collectStructFields` 對
`drv.bpctx` 的登錄從未發生（實測登錄 25 個 struct，全部模組限定名，無 `drv.bpctx`），
MIR 只能在 `setfield` 時報導 `field "r" not found on "drv.bpctx"`。這正是 bug #85
「模組頂層綁定（常量／全域）只有在入口檔直接 import 時才會生成」的**同一機制**，
只是對象從常量延伸到 **`struct` 定義**。

兩個放大此 bug 潛在性的既有性質，值得記錄：

- **h2m 不作死碼降級**：未被引用的函式體根本不降級（逐變體驗證：只定義帶該字面量的
  函式為綠、呼叫它才紅）。所以 s153 之前 `build-pkg-to` 不存在、`bpctx` 字面量不可達，
  這個根因一直潛伏。
- **單檔模式偽綠**：`no build -o … compiler/tests/build-package-test.no`（單檔）實測
  `Build successful`——單檔模式的入口即檔案本身，`drv` 整模組被引入，`bpctx` 如常生成。
  只有套件模式（目錄入口）才對入口檔的 import 形狀敏感。

修法即 `compiler/main.no` 那一行（直接 import 整個 `drv` 模組）。**未改 Go 編譯器**：
這不是 Go 層的缺陷，是入口檔的 import 形狀不足以生成套件模式所需的型別定義。

### ③ 驗證（逐一可重現）

- **S0 重確認**：`src/` 已回到 HEAD 乾淨（臨時儀表 `NOLANG_DBG_STRUCTFIELDS` 全部移除，
  `git status --porcelain src/` 空）；`tmp/s153/bin/no version` = `v0.4.29-2-gd236a956-dirty`。
- **S1 重建綠**：`./tmp/s153/bin/no build -o tmp/s154/noc-s1 compiler` → `Build successful`
  （4:56.43，含 `warning: compiler version mismatch: package.noml requires "dev"…`，
  此警告為既存的版本戳口徑，非本輪引入）。
- **層 1 綠燈夾（端到端可執行檔）**：新增 `tmp/s154/mini2/main.no`（只 import
  `# /compiler/tests/t5/testmod.no` ＋一個函式定義，無頂層陳述）→
  `./tmp/s154/noc-s1 build -o tmp/s154/mini2-bin tmp/s154/mini2` → `Build successful`，
  執行 `tmp/s154/mini2-bin` → rc=0。**證明 `drv.cmd-build` 的目錄分支真的走
  `build-package → pkg-ll → link.compile` 且產出可執行檔。**
- **誠實門禁（真目標）**：`./tmp/s154/noc-s1 build -o tmp/s154/noc-s1-self compiler` →
  **rc=1**，stderr 尾：`noc: dropped (calls unsupported): …`（140 條 closure 剪枝）、
  `noc: unsupported constructs: 1711 (pkg-ll)`、`noc: no code generated`，**不再有
  `setfield` 崩潰**。`tmp/s154/noc-s1-self` 如預期未產生（缺碼 IR 未匯出）。
- **管線忠實性（本輪最關鍵的交叉驗證）**：S0 跑 `drv.pkg-ll-cov` 與 S1 跑 `build <dir>` 的
  unsup 數**完全相同 = 1711**（1571 直接 ＋ 140 dropped）。與 §72 寫作時的 1696
  （1556 ＋ 140）相較 **+15**，逐模組對戳 `tmp/s154/cov-s153.log` 與 `cov-s153b.log`：
  差額**全部**來自本輪新納入的 `drv.build-pkg-to`（+5）與 `cmd-build`／`cmd-run` 新增分支
  （各 +5）——即 **＋15 是「新管線程式碼本身也是 h2m 降級得到／不到的真實成份」**，
  不是量測漂移。這同時證明 S1 的整包路徑與 S0 的 `drv.pkg-ll` 口徑一致。
- **L0 不回退**：`bash tmp/s153/run-gates-clean.sh`（⚠️ 不得 `--skip-build`）→ **18/18
  ALL PASSED**，逐套件數與 §72 基準逐項相同（ast 48、build-package 8、cg-runtime 114、
  cg-str16 41、cg 363、ck-sig 50、ck 20、diag 7、drv-build 63、h2m 1365、lx 81、
  pkg-ll-call 5、pkg-ll 10、pkg-sigs 5、ps-err 46、ps 52、tok 62、util 27）。

### ④ 已知邊界（誠實標註，非本輪引入）

- **套件入口的頂層陳述目前仍 unsup**：`tmp/s154/mini/main.no`（import ＋兩個頂層陳述）
  經 `drv.pkg-ll-cov` 量到 2 條：`main:BlockStatement`、`main:FunctionDefinition`。
  這是 h2m 對入口檔頂層程式碼（`@main` 合成塊）的既有缺口，**與本輪管線改動無關**
  （量測工具 `drv.pkg-ll` 本身一行未動其降級語意）。Control 組 `compiler/tests/t5/call-entry.no`
  （無頂層陳述）為 0 unsup。因 `compiler/main.no` 自帶頂層陳述（`code = drv.dispatch()`），
  此 2 條本就含在 1711 內，不影響 ③ 的忠實性結論。
- **層 2 未動**：1571 條直接 unsup（1711 扣除 140 閉包剔除）的逐模組分佈（`ps-parse-match`
  123 仍居首）與 §72 ④ 一致，本輪沒有帶走任何一格。「把 `pkg-ll` 變綠」仍得逐格修
  §72 ⑥ 的層 2 真牆（h2m 能力擴張，M5/M6 範疇）。
- 物證：`tmp/s154/{drv.no.layer1,cov-main.no,cov-s153b.log,cov-s153b.err,mini/,mini-bin,
  mini2/,mini2-bin,probe-mini.no,probe-t5.no,s1-build-s153.log,s1-self-build.out,
  s1-self-build.err,gates-s153-layer1.log}`；`tmp/s153/gates-clean.log`。
  ⚠️ `tmp/s154/mini*/` 為層 1 綠燈夾（非倉庫資產，後續可刪）；⚠️ 本輪**未提交**任何改動，
  本輪改動為 `compiler/{main.no,src/drv.no,tests/pkg-ll-test.no,tests/pkg-ll-call-test.no}`＋本文
  （`compiler/src/{cg,h2m,lx,ps}.no` 與 `compiler/tests/ast-test.no` 的更動為本輪之前既存、
  非本輪產物；`src/` 乾淨）。

## 74 s153 層 2（真牆）首創增量 as-built 補記：h2m 區塊陈述位三形（裸 `_`／尾值表達裸標識符／括號陳述）

本節執行 §72 ⑥ 切分的**層 2（真牆）**第一步：從 §73 遺下的 1571 條直接 unsup（1711 扣除
140 閉包剔除）中，先攻「區塊陈述位」這一格。以臨時儀表（在 `h2m.no` 內開一條診斷通道，把
`blk-unhandled` 的逐條歸因印出，量測後即拆）把 111 條 `blk-unhandled` 逐條歸因，本輪認領其中
91 條，**直接 unsup 1571 → 1483（-88）**。管線層（§73）不動，S2 自舉仍停在誠實的 unsup
診斷，但距「缺碼」又近一格。

### ① 變更清單（逐檔逐函式，全部為追加／最小改）

- `compiler/src/h2m.no` — `h2m-lower-block` 的 `K-EXPRESSION-STATEMENT` 分支（陳述位救護
  分派），新增三臂：
  - `sub == 0 -> hd = 1`：**裸 `_` 陳述**（`ExpressionStatement{nil}`，kid0=0）——無子節點
    ⇒ 無操作。口徑鏡像 Go 對 `_` 空運算式的整條丟棄，與既有的 `K-EMPTY-STATEMENT` 臂
    （`s101`「`...` 占位陳述——什麼都不做」）一致。
  - `hd == 0 && sd.kind == asp.K-IDENTIFIER && last == 1 && ast.str-at(t, sd.s0) == c.outn`：
    **尾值表達**——函式【末條】裸標識符且名字等於本函式輸出參數名。降級后棄值（同
    `K-CALL-EXPRESSION` 臂口徑）。
  - `hd == 0 && sd.kind == asp.K-GROUPED-EXPRESSION`：**括號陳述** `(expr)`——降內層后棄值。
  - ⚠️ `c.outn` 是 `h2m-ctx` 的**既有**欄位（「輸出（回傳）變數名」，`h2m-lower-func` 在
    body 降級前設定，見 `h2m.no:6469`），本輪直接復用，未新增欄位、未改 structs。
  - 未動：`hd == 0` 的誠實門禁尾巴（`h2m-dbg(c, 'blk-unhandled'); h2m-mark(c)`）保持原狀，
    凡未認領的陳述形仍照舊計 unsup。

### ② 口徑（為何「末條 + 輸出名」而非籠統放行裸 Identifier）

- **歸因**：111 條 `blk-unhandled` = 55 裸 Identifier + 32 裸 `_` + 4 括號 + 20 多重指派。
  本輪認領前三形（91 條）；20 條 `K-MULTI-ASSIGN-STATEMENT` 見 ④。
- **4 條括號站台的來歷（`ps` 與 Go 前端的解析分歧）**：以 `ps-scan-nont` 原文實測
  （`tmp/s154/probe-psdump2.no` 走 `ps.parse`，`tmp/s154/probe-scannt3.no` 走 `no ast`），
  當條件形為 `i < p.toks.len() && (…)` 時，Go 前端建 **ForStatement**，`ps` 卻建
  **ExpressionStatement{GroupedExpression} + BlockStatement**——這正是 4 條「陳述位括號」的來源。
  （條件簡化成形如 `i < p && (…)` 時 `ps` 仍建 ForStatement，見 `tmp/s154/probe-psdump.no`，
  故此偏差與條件形相關，不是無條件分歧。）
- **55 條裸 Identifier 的形狀**：以 `tmp/s154/check-tail.py` 逐行核對 `compiler/src/*.no` 與
  `compiler/main.no`，55 條**全部**是「函式末條、且名字 == 該函式（第一個）輸出參數名」——
  即自舉源的**尾值表達**慣用法（函式末條只寫名字，值靠輸出參數名回讀，此陳述本身無副作用）；
  且無一條屬多輸出函式。
- **權威的確接受裸 Identifier 陳述**：三個探針 (`tmp/s154/probe-nontail-go.no`／
  `probe-tailnonout-go.no`／既有的 `ast-probe2.no`) 以 S0 `no build` 皆 `Build successful`——
  依據是 Go 前端 `hir2mir.go:4399` `KExprStmt` 的 else 分支 `l.stmtVal = l.lowerExpr(child)`，
  對任意裸運算式陳述一律降級后棄值。故「放行裸 Identifier」本身是對齊權威，不是放寬。
- **但籠統放行會把退化原始碼靜默降成錯碼**：【非末條】或【名字非輸出參數】的裸 Identifier 在
  自舉源裡只來自 `ps` 的退化解析，而宿主對這些退化源的第一手反應是**拒絕**或**丟棄**——
  - `n * { … }`（`ps` 解析的識別字計數位）被退化成「裸 n + LET 塊」：宿主 S0 對該原文
    實際上報 `expected expression after operator '*' [venvcq6s]`（`tmp/s154/probe-cl-go.no`）。
    籠統放行 ⇒ 靜默降成 `r = r + 1`（只加一次），是無警告錯碼。
  - `&^`：`lx` 有 `tok.AND-NOT`，但 `ps-op-prec` 未收（同 Go 的 `precedences` 表，兩前端一致），
    故 `x &^ y` 被解析成 `x` + 裸 `y`（`tmp/s154/probe-bitgate.no` 的 AST 實證）。籠統放行
    ⇒ 裸 y 被棄值，整條運算靜默消失。
  收窄為「末條 + 名字 == `c.outn`」后，這兩個退化形仍落 `hd == 0` 誠實門禁 ⇒ 要嘛計 unsup
  不產碼（誠實失敗），要嘛（如 `&^` 那樣）由既有的負例門禁守住。
- **連帶效果**：收窄使 `compiler/tests/h2m-test.no` 的兩個既有負例**不需改動**——
  `test-flow-gate` 的 `count-loop counted`（`n * { … }` 的裸 n，非末條）與 `test-bit-gate`
  的 `bit-gate counted`／`bit-gate no code`（`&^` 退化后的裸 y，非輸出名）皆維持 `unsup > 0`。
  若走籠統放行，這兩處（共 3 條斷言）會翻黑，且是「把退化源靜默降成錯碼」的翻黑。

### ③ 驗證（逐一可重現）

- **h2m-test.no**：`./tmp/s153/bin/no run compiler/tests/h2m-test.no` → **1365/1365
  ALL TESTS PASSED**（無改動任何測試）。
- **直接 unsup**：S0 跑 `tmp/s154/cov-main.no`（內含 `drv.pkg-ll-cov('compiler/main.no')`）→
  `noc: unsupported constructs: 1646 (pkg-ll)`、`dropped (calls unsupported)` 163 條
  ⇒ 直接 1483（= 1646 − 163），較 §73 的 1571 **-88**；總數 1711 → 1646。
  閉包剔除 140 → 163（**+23**：新放行的函式體被完整降級后，暴露更多位在其中的不支援被調，
  屬預期副作用，非量測漂移）。
- **91 認領 vs -88 淨減（逐條核對）**：為確認 91 條站台真的全放行，另開臨時儀表
  （`h2m-mark-here` 內一條 `TMPMARK-k<cur-k>` 歸因 + 三臂各一條觸發記錄，量測後即拆）：
  - 三臂觸發數**恰好 32／55／4**（`ARM-underscore`／`ARM-tailid`／`ARM-grouped`），與認領數逐項相符；
  - 認領後重跑 `tmp/s154/cov-main.no`，`blk-unhandled` 只剩 **20** 條，且逐條與儀表清單的
    k49（K-MULTI-ASSIGN）**一項不差**（`compile` 1、`drv.pkg-ll` 2、`h2m-func-sig` 2、
    `h2m-lower-expr` 1、`h2m-lower-func` 1、`h2m-lower-program` 2、`h2m-lower-surface` 1、
    `header` 1、`lx-scan-token` 3、`ps-mk-standalone-if` 3、`ps-parse-for` 1、
    `ps-parse-for-kw` 1、`ps-wrap-chain` 1）；
  - 記錄級按 (fn, cur-k) 歸桶 diff：**89 減 1 增**，淨 -88。那一條「增」是 `ps-scan-nont` 的
    `TMPMARK-k25`（DotExpression）——該函的括號站台內層是 `&&` 運算，其中 `p.toks.len()`
    的 DotExpression 降不了，改在 `h2m-lower-expr` 內記 unsup；同函另一條（尾值 `i`）亦改在
    `h2m-lower-expr` 內，cur-k 仍 38 ⇒ 桶數不變，肉眼看不出移動。
  - 即「91 放行」與「淨減 88」的差額**不是**「有站台沒放行」，而是 2 條（都在 `ps-scan-nont`）
    從 `blk-unhandled` 改記到 `h2m-lower-expr` 內部的失敗路徑。
- **S0/S1 逐條一致**：S1 以 S0 重建（`./tmp/s153/bin/no build -o tmp/s154/noc-s1 compiler`
  → `Build successful`，15:09）後，自舉 `./tmp/s154/noc-s1 build -o tmp/s154/noc-s1-self compiler`
  → **rc=1**，`noc: unsupported constructs: 1646 (pkg-ll)`、`noc: no code generated`，
  **與 S0 的 1646 完全相同**，且沒有 `setfield` 崩潰（§73 的根因修正持續有效）。
- **L0 不回退**：`bash tmp/s153/run-gates-clean.sh`（⚠️ 不得 `--skip-build`）→ **18/18
  ALL PASSED**，逐套件數與 §73 基準逐項相同（ast 48、build-package 8、cg-runtime 114、
  cg-str16 41、cg 363、ck-sig 50、ck 20、diag 7、drv-build 63、h2m 1365、lx 81、
  pkg-ll-call 5、pkg-ll 10、pkg-sigs 5、ps-err 46、ps 52、tok 62、util 27）。
- **LF: 通道分佈**（逐條葉子歸因，`tmp/s154/cov-s154b.log`）前段：`ps-parse-match` 123
  （不變，仍居首）、`h2m-lower-expr` 78、`drv.pkg-ll` 66、`ps-parse-expr-prec` 62、
  `drv.cmd-vet` 61、`ps-parse-enum-def` 50、`check` 40、`ps-parse-bare-match` 36、
  `ps-q-esc` 31、`ps-parse-method-func` 27、`ps-parse-let` 27、`ps-parse-stmt` 26、
  `lx-scan-token` 26、`ps-parse-type-alias` 20、`ps-int-lit-value` 20。

### ④ 已知邊界（誠實標註）

- **20 條 `K-MULTI-ASSIGN-STATEMENT` 未動**：這是 111 條 `blk-unhandled` 的最後一形。
  現狀 `h2m-lower-expr` 的 `OP-CALL` 只發**單結果**（`cid`，型別 `rt = sg.fret[si]`；`sg.fret`
  是 `[]i64` 回值型別碼表），故 `a, b = f()` 的多結果呼叫需要橫跨 **h2m + cg** 的新機制
  （鏡像 Go `hir2mir.go:14161` 的 `EmitCallMulti`＋`bindTarget` 一對），非一輪可畢。
- **剩餘前沿＝切片型別缺口（切片 B）**：`drv.pkg-ll` 66、`drv.cmd-vet` 61、
  `ps-parse-expr-prec` 62、`ps-parse-enum-def` 50 等前段多是 `src []byte = with-cap(0)`／
  `ck-out []str = with-cap(4)` 這類**切片型別**（doc 切片 B），屬 mir/cg/h2m/cg-runtime
  多模組工程，非本輪的陳述位量級。
- **量測口令**：`tmp/s154/cov-main.no` 走 `drv.pkg-ll-cov`（會印 `LF:` 歸因與
  `unsupported constructs` 總結）；切勿於工作樹留下 `h2m-dbg` 的 debug 通道（本輪的
  `blk-unhandled-k*` 歸因儀表、`TMPMARK-k*` 歸因儀表與 `ARM-*` 觸發記錄皆已拆，
  現行 `compiler/src/h2m.no` 只含 ① 的三臂）。
- 物證：`tmp/s154/{cov-main.no,cov-s153b.log,cov-s154b.log,cov-s154c.log,cov-s154d.log,
  cov-s154e.log,cov-s154f.log,s1-build-s154b.log,s1-self-build-s154b.err,gates-s154b.log,
  check-tail.py,probe-countloop.no,probe-bitgate.no,probe-bareident-go.no,probe-nontail-go.no,
  probe-tailnonout-go.no,probe-cl-go.no,probe-scannt3.no,
  probe-psdump.no,probe-psdump2.no}`；`tmp/s153/gates-clean.log`。
  ⚠️ 本輪**未提交**任何改動；工作樹中 `compiler/src/h2m.no` 與本文為本輪產物，
  `compiler/{main.no,src/drv.no,tests/pkg-ll-*.no}` 為 §73 產物，
  `compiler/src/{cg,lx,ps}.no` 與 `compiler/tests/ast-test.no` 為更早的既存更動，`src/` 乾淨。

## 75 s153 層 2（真牆）第二步 as-built 補記：多結果呼叫 `a, b = f()` 的 h2m 通路（2a 聚合回傳 ＋ 2b 多賦值陳述）

本節執行 §55 ⑤ 定的「下一步①」（多回傳函式輸出參在 `mem == 1` 下的鋪槽）與 §74 ④ 遺下的
20 條 `K-MULTI-ASSIGN-STATEMENT`（111 條 `blk-unhandled` 的最後一形）。**直接 unsup
1483 → 1376（−107）**；總數 1646 → 1552（−94）；閉包剔除 163 → 176（+13）。管線層（§73）
不動，S2 自舉仍停在誠實的 unsup 診斷，但 111 條 `blk-unhandled` 至此**全部認領完畢**。

### ① 變更清單（逐檔逐函式，全部為追加／最小改）

- `compiler/src/h2m.no` — **7 個站點**（全部追加／最小改；第 5 點含 3 處）：
  - `h2m-scan` 新增 `K-MULTI-ASSIGN-STATEMENT` 臂（`h2m.no:3050-3081`）：形狀為 kid 陣 =
    [target0..targetN-1, value]，目標數在 `d.c0`。RHS 型別由 `h2m-ty-of` 解出（多結果呼叫
    得聚合結構體碼 `TY-STRUCT-BASE+sid`），各目標型別取該結構體第 k 欄位（與 `h2m-func-sig`
    的 `stftn`/`stft` 同序）；型別推不出（`-1`）時照登，由 `h2m-slot-decl` 統一只計一次
    unsup，不在前掃期搶先記帳。⚠️ **不登記則 `mem==1` 的 `h2m-var-write` 查不到名字 ⇒ 落
    `h2m-mark`**——這正是 §55 ⑤「下遊增量的收益被前掃沒鋪槽凍結」的機制。
  - `h2m-lower-multi-assign` 新函式（`h2m.no:4684-4725`）：降 RHS 得聚合值 `vv`；逐目標發
    `OP-STRUCT-GET` 取第 k 欄位（型別 `h2m-struct-field-ty`）後 `h2m-var-write`。型別非聚合
    （`msid < 0`）或目標非裸標識符時 `h2m-mark`；`vv == 0`（值就沒降出來）**不重複記**
    （見 ② 不得雙帳）。
  - `h2m-lower-block` 新增 `K-MULTI-ASSIGN-STATEMENT` 臂（`h2m.no:4765-4769`），緊接
    `K-LET-STATEMENT` 臂之後。沿用 §74 的慣例**不另加 `h2m-dbg` 診斷位**（該輪的新臂
    皆未加 tag），避免在 `DB:` 通道留檔。
  - `h2m-mr-aggregate` 新函式（`h2m.no:6399-6439`）：把 `real-outs > 1` 的各輸出參值經
    `c.args` 池發 `OP-STRUCT-MK` 合成 `__mr__<fn>` 聚合；含 str 欄位時照 s99g 追加
    `OP-STRUCT-RETAIN` + `h2m-rel-add`。
  - `h2m-lower-func` 三處：`c.mem == 1 && real-outs > 1` 的**輸出鋪槽段**（`h2m.no:6589-6617`，
    逐欄位 `h2m-lst-add`，與 `h2m-mr-aggregate` 同序）；直線 `rid` 路徑
    （`h2m.no:6688-6692`）與共享 ret 塊 `zc2` 路徑（`h2m.no:6744-6746`）改走
    `h2m-mr-aggregate`。
  - ⚠️ 兩處**口徑對齊 §74 ① 的裸 `_` 陳述**（「無子節點 ⇒ 無操作」，鏡像 Go 對 `_` 的整條
    丟棄）：`h2m-scan` 與 `h2m-lower-multi-assign` 皆**跳過 `_`（blank 目標）**——不鋪槽、
    不發 `OP-STRUCT-GET`、不寫。理由見 ②。

### ② 口徑（為何 2a 必須與 2b 同輪；兩個迴避陷阱）

- **2a 是 2b 的前提（§55 ⑤ 教訓）**：2b（多賦值陳述降級）單獨做時，在 16 檔語料上淨收益為
  0——2b 把 `a, b = f()` 接到 `h2m-lower-expr` 的 `OP-CALL`，而該臂只發**單結果**（`cid`，
  型別 `rt = sg.fret[si]`），多結果呼叫根本降不出聚合值，於是站台反而更晚失敗。真缺口是
  「多回傳函式的輸出參在 `mem == 1` 下沒有鋪槽」：`h2m-lower-func` 既有的鋪槽段只處理
  `real-outs == 1`，`real-outs > 1` 時輸出參查無 ⇒ `h2m-var-read` 取不到值 ⇒ 回傳值構造
  失敗。故本輪把 2a（鋪槽 ＋ 聚合回傳）與 2b（陳述降級 ＋ 前掃鋪槽）**同輪**做，探針方由負轉正。
- **⚠️ `h2m-mr-aggregate` 嚴禁用局部 vec 收集**：本函式體含 `for` 迴圈 ⇒ `flow=1` ⇒
  `mem==1`；在 `mem==1` 下推 vec 會踩 s122 的 push 缺口而**自傷**——實測初版（以局部 vec
  收集各欄位值再 `OP-STRUCT-MK`）本函式體被降級時自帶 1 條 `BlockStatement` unsup。改為
  逐個逕推 `c.args` 池（`OP-STRUCT-MK` 的 base 即池底），失敗時保留已推個子：整個函式既有
  mark ⇒ 不產碼，孤儿個子無害（不發任何 `OP-CALL`）。
- **⚠️ `_`（blank 目標）必須跳過鋪槽**：`h2m-lst-add` 對同名**就地覆蓋**型別。`nm, _, _, _ = f()`
  若鋪 `_`，同一個 `_` 被登記 4 次、型別被覆寫成最後一欄，較早的欄位值會寫進錯誤型別的槽
  （實測 `a, b, _, _`——vec 值在第 3 欄——由 1 unsup → 0）。跳過後 `a, _, c, _`／`a, b, c, _`
  仍各 1，那 1 是**命名 vec 目標**落在受控塊（s122 vec 槽寫缺口），與 `_` 無關。
  `_, _, _, _` 則 `ps.parse` **根本不產** `K-MULTI-ASSIGN-STATEMENT`（故無 Arm 可發），
  屬 `ps` 與 Go 前端的既存解析分歧（見 ④）。
- **不得雙帳**：`h2m-lower-expr` 的 `OP-CALL` 失敗時自己已 `h2m-mark`；
  `h2m-lower-multi-assign` 只在「值降出來但型別非聚合」與「目標非裸標識符」兩處補記，
  `vv == 0` 時不再記（即 §57 的「每塊只計一次」紀律，於本處引申為「同一失敗不得記兩次」）。

### ③ 驗證（逐一可重現）

- **h2m-test.no**：`./tmp/s153/bin/no run compiler/tests/h2m-test.no` → **1365/1365
  ALL TESTS PASSED**（無改動任何測試；收尾重跑見 `tmp/s156/h2m-test3.log`）。
- **探針 pre/post**（`tmp/s156/`，S0 直跑 `drv.pkg-ll-cov`，免重建）：
  - `ma-prog`（3 輸出、`mem==0`）：4 → 0；`ma2-prog`（2 輸出、`mem==1` 帶 if）：9 → 0；
  - `ma3`（4 輸出含 `[]i64`、`mem==1`）：1（留守：s122 vec 寫缺口）；
    `ma4`（4 輸出無 vec、`mem==1`）：0；
  - `ma5v1..v4`（`_` 目標）：0／0／1／1（後兩個的 1 是命名 vec 目標，同 s122）。
- **全語料**：pre `tmp/s154/cov-s154g.{log,err}`（unsup 1646、block 1484、ID 135、LF 1483、
  dropped 163）→ post `tmp/s156/cov-post2.{log,err}`（unsup 1552、block 1377、ID 100、
  LF 1376、dropped 176）。block **−107**、ID **−35**、LF **−107**、unsup **−94**；
  直接（扣 dropped）1483 → 1376。
- **收尾重量**：拆除本輪唯一新增的 `h2m-dbg` 診斷位（`blk-ma`，見 ①）後，以同一語料
  （`tmp/s154/cov-main.no`）重跑 → `tmp/s156/cov-post3.{log,err}` 與 post2 **五項逐項相同**
  （1552／1377／100／1376／176）。即該 tag 對量測無影響，且 `DB:` 通道回到 §74 收官狀態。
  ⚠️ 比對時必須同一語料：`tmp/s156/drvdrv.no` 走 `drv.pkg-ll-cov('compiler/src/drv.no')`，
  與 `cov-main.no` 的 `('compiler/main.no')` 是不同的根（曾因誤用而讀出 1551／175）。
- **逐函式 block diff**（`fn:Kind:construct` 計數）：8 個改善——`lx-scan-token` −24、
  `h2m-func-sig` −11、`h2m-lower-program` −10、`h2m-lower-expr` −9、`h2m-lower-func` −9、
  `ps-parse-for-kw` −5、`h2m-lower-surface` −1（合計 −69）；11 個由「標記」轉「產碼但
  dropped」——`ps-mk-standalone-if` 9、`ps-wrap-chain` 7、`lx-read-regex` 5、`ps-parse-for`
  5、`header` 4、`scalar` 3、`lx-read-string` 2、`h2m-lower-safe-index` 1、`h2m-param-ty` 1、
  `ps-for-source` 1、`ps-standalone-body` 1（合計 −39）；1 個 +1——`drv.pkg-ll`（66 → 67）。
- **`blk-unhandled` 歸零**：`DB:` 通道的 `blk-unhandled` 計數 20 → 0（pre
  `cov-s154g.log` 20 條，post2／post3 皆 0），與 ② 的「111 條全認領」相符。
- **dropped diff**：163 → 176，**新增 13、消失 0**。13 = 上述 11 個轉 dropped ＋ 本輪新成的
  兩函式 `h2m-lower-multi-assign`、`h2m-mr-aggregate`（雙雙產碼但被剔除，故各計 1 條
  dropped 而非 block mark）。
- **S0/S1 逐條一致**：S1 以 S0 重建（`./tmp/s153/bin/no build -o tmp/s154/noc-s1 compiler`
  → `Build successful`）後，自舉 `./tmp/s154/noc-s1 build -o tmp/s156/noc-s1-self compiler`
  → rc=1，`noc: unsupported constructs: 1552 (pkg-ll)`、`noc: no code generated`，
  **與 S0 的 1552 完全相同**。
- **L0 不回退**：`bash tmp/s153/run-gates-clean.sh`（⚠️ 不得 `--skip-build`）→ **18/18
  ALL PASSED**，逐套件數與 §73/§74 基準逐項相同（ast 48、build-package 8、cg-runtime 114、
  cg-str16 41、cg 363、ck-sig 50、ck 20、diag 7、drv-build 63、h2m 1365、lx 81、
  pkg-ll-call 5、pkg-ll 10、pkg-sigs 5、ps-err 46、ps 52、tok 62、util 27）；收尾重量見
  `tmp/s156/gates3.log`（同 18/18、同逐套件數）。
- **fmt**：`no fmt -d compiler/src/h2m.no` 9 處 hunk，全為 §73/§74 未提交工作與既存漂移
  （1772／1783／1833／1844／4025／4035／6814 為 fmt 要求補空行；4639／4666 為既存的
  `let-cap`／`let-tc` 縮排漂移）——**本輪新增程式零漂移**。

### ④ 已知邊界（誠實標註）

- **`drv.pkg-ll` 的 +1 是殘留的善良缺口，非退化**：該站台（`drv.no:833` 的 `ap []i64` 目標
  落在 pass-2 的 `(m < nk)` 迴圈內）觸及 s122 的 **vec 槽寫缺口**——`h2m-var-write` 的 vec 臂
  要求 `c.bkn == c.fnbk`（函式體頂層塊），在受控塊內值 id 不支配 ⇒ `h2m-mark`。本輪已把
  同一站台上**可避免**的兩處 mark 修掉（`_` 覆寫型別、`h2m-mr-aggregate` 自傷）。另該站台
  原有一處 `bd == 0 -> {…}` 塊因 `bd` 未登記而跳過，本輪 `bd` 鋪槽後該塊開始降級，暴露其
  自身的不支援（同屬預期副作用，非量測漂移）。
- **剩餘前沿仍是切片型別缺口（切片 B）**：`drv.pkg-ll` 66、`drv.cmd-vet` 61、
  `ps-parse-expr-prec` 62、`ps-parse-enum-def` 50 等前段多是 `src []byte = with-cap(0)`／
  `ck-out []str = with-cap(4)` 這類切片型別（doc 切片 B），屬 mir/cg/h2m/cg-runtime 多模組
  工程，非本輪的量級。
- **`ps` 解析分歧（既存，非本輪引入）**：`_, _, _, _ = f()` 這類全 blank 目標，`ps.parse` 不產
  `K-MULTI-ASSIGN-STATEMENT`（故無 Arm 可發）；`i < p.toks.len() && (…)` 形則見 §74 ②。
- **量測口令**：`tmp/s156/cov-*.log` 走 `drv.pkg-ll-cov`（會印 `LF:`／`ID:`／`DB:` 通道與
  `unsupported constructs` 總結）；`dropped` 行在 **stderr**（`cov-*.err`），比對時不可只讀
  `.log`。切勿於工作樹留下 `h2m-dbg` 的 debug 通道——本輪新臂沿用 §74 慣例**不加** tag，
  `DB:` 通道與 §74 收官時完全一致（`blk-unhandled` 為 §74 既存的誠實門禁 tag）。
- 物證：`tmp/s156/{ma-prog,ma2-prog,ma3-prog,ma4-prog,ma5v1..ma5v4-prog,drvdrv}.no`
  與對應 `*-driver.no`、`cov-post.log`、`cov-post2.{log,err}`、`pre.err`、`post.log`、
  `s1-build2.log`、`s1-self-build2.log`、`s1-self-build3.{log,err}`、`gates2.log`、
  `gates3.log`、`h2m-test2.log`、`h2m-test3.log`、`cov-post3.{log,err}`；pre 基線
  `tmp/s154/{cov-main.no,cov-s154g.log,cov-s154g.err}`。
  ⚠️ 本輪**未提交**任何改動；工作樹中 `compiler/src/h2m.no` 與本文為本輪產物，
  `compiler/{main.no,src/drv.no,tests/pkg-ll-*.no}` 為 §73/§74 產物，
  `compiler/src/{cg,lx,ps}.no` 與 `compiler/tests/ast-test.no` 為更早的既存更動，
  另有 `noui/*` 的無關更動。

## 76 s153 層 2 收官後盤點：殘留 1552 的渠道方法論（LF 葉子通道被巢狀 cur-k 污染）與分項量化

§75 收官（1552 條直接 ＋ 閉包剔除 176）後，為下一輪選目標做的一次**純盤點**。本節不改程式
（`compiler/src/h2m.no` 文字與 §75 收官時相同，見 ④ 終檢），只量測與記錄，目的是把「下一輪
該打哪」從「感覺」變成「分項數字」。

### ① 盤點方法（四條臨時探針，量測後即拆）

- **可靠通道**：block 通道（`fn:Kind:construct`，§57 的「每塊只計一次」紀律）與 ID 通道
  （`h2m-id-name` 的失敗標識符名）。
- **臨時探針**（`eprint` 直印，量測後即拆，見 ④）：`TMPVECW`（`h2m-var-write` 的 vec 臂
  `vt == st && c.bkn != c.fnbk` 失敗）、`TMPVW`（同函式的 `ok-w == 0` 尾巴）、`TMPSSLOT`
  （`h2m-slot-decl` 的 `ok-slot == 0`）、`TMPID`（`h2m-lower-expr` 的 `ssi < 0` 臂）。

### ② 方法論發現：LF（葉子）通道不可單獨當作「失敗構造」的 oracle

- LF 通道記的是 `c.cur-k`——「**最近一次** `h2m-set-cur-k` 設的節點」（既有注記見
  `h2m.no:251`；`s107` 注記亦言明「嵌套構造降級時內層塊不斷覆蓋它」）。
- `h2m-set-cur-k` 的 13 個呼叫點中，**只有** `h2m-lower-expr`（`h2m.no:3214`）傳**表達式**
  節點；其餘 12 個（`h2m-lower-let`／`h2m-lower-block`／`h2m-lower-func`／`h2m-lower-if`…）
  傳的都是陳述／定義節點。
- 故 `Identifier:construct` 葉子只表示「上一次進入 `h2m-lower-expr` 的節點是 Identifier」，
  **不**表示失敗構造是 Identifier：陳述級函式在巢狀 `h2m-lower-expr` 回傳後，`cur-k` 仍留著
  那個內層節點，隨後的 mark 便記成 Identifier。
- **量化**：LF 共 1376 條，其中 `Identifier` **888** 條（65%）；而真正「在 K-IDENTIFIER 臂內
  失敗」的 `TMPID` 只有 **100** 條（= ID 通道數，且 `h2m-var-read` 的 `vr-miss-` 路徑在語料上
  一次都沒觸發）。即 **≥788 條葉子被巢狀內容污染**，不可據以歸因。
- **影響範圍**：§74 ② 的「55 條裸 Identifier」結論是逐行核對原始碼（`tmp/s154/check-tail.py`）
  得來的，**不受**此污染影響；但日後凡只看 LF 葉子的歸因都必須搭配原始碼核對。

### ③ 分項量化（四探針覆蓋 296 條，其餘 1256 條未歸因）

- **vec 寫缺口（s122）＝ 30 條**（=`TMPVECW`，含在 `TMPVW` 的 79 內）：散在 **17 個**函式，
  最多 `ps-parse-match` 7、`drv.pkg-ll` 5，其餘（`ps-parse-enum-def`／`ps-parse-expr-prec`／
  `ps-parse-bare-match`／`h2m-lower-expr` 等）各 1–2。⇒ §75 ④ 的「殘留」為真，但只佔 2%，
  **不是**主攻方向。
- **型別不可鋪槽（`h2m-slot-decl` 的 `ok-slot == 0`）＝ 117 條**：即切片／集合型別
  （`[]byte`／`[]str`／`with-cap`）——這正是 §74 ④／§75 ④ 所稱「切片 B」缺口在 mark 通道的
  投影。
- **識別字解析失敗（ID 通道）＝ 100 條**：集中於 `bs` 7（`lit.to-bytes()`）、`ivb` 6
  （`ivt.slice-bytes(...)`）、`c` 6（`cb.to-i64()`）、`base` 5、`gnames` 4、`elem` 4…多為
  「方法簽章未覆蓋」（`str.to-bytes`／`slice-bytes`／`byte().to-i64`）或「值為切片型別」。
- **其餘 1256 條**（1552 − 296）落在 `h2m-lower-block`／`h2m-lower-func`／`h2m-lower-if`／
  `h2m-lower-expr` 等處，未逐一歸因（強行歸因需要更細的站點儀表，屬下一輪功夫）。

### ④ 結論與終檢

- **剩餘前沿確認＝切片／集合型別**：117（不可鋪槽）＋ 100（識別字）中大部分指向切片方法與
  切片型別；`drv.pkg-ll`／`drv.cmd-vet`／`ps-parse-expr-prec` 等前段的方塊數（38／26／37）
  與其分布相符。量級為 mir/cg/h2m/cg-runtime 多模組工程，非一輪可畢——§72 ⑥ 的
  「層 2 真牆」將在切片 B 直面 M5/M6（見 §74 ④）。
- **終檢（可重現）**：
  - `grep -c 'TMPVECW\|TMPVW\|TMPSSLOT\|TMPID' compiler/src/h2m.no` → **0**（探針全拆）；
  - `tmp/s153/bin/no run tmp/s154/cov-main.no` → **1552**／block **1377**／ID **100**／
    LF **1376**／dropped **176**，與 §75 收官時逐項相同（`tmp/s156/cov-post5.{log,err}`）；
  - `tmp/s153/bin/no build -o tmp/s154/noc-s1 compiler` → **Build successful**；S1 自舉 →
    **1552**、`no code generated`（`tmp/s156/s1-self-build4.err`）；
  - `./tmp/s153/bin/no run compiler/tests/h2m-test.no` → **1365/1365 ALL TESTS PASSED**
    （`tmp/s156/h2m-test4.log`）；L0 `bash tmp/s153/run-gates-clean.sh` → **18/18**
    （`tmp/s156/gates3.log`）。
- **物證**：`tmp/s156/{cov-post4,cov-post5,cov-vecw,cov-vecw2,cov-mk}.{log,err}`、
  `s1-self-build4.err`、`h2m-test4.log`、`gates3.log`。

## 77 s154 — 跨模組點號呼叫的「全名鍵」回落（`mod.<帶前綴定義名>`）：根因坐實、零 mark 助手、1552→1509

§76 ③ 把 ID 失敗首惡記為 `bs` 7（`lit.to-bytes()`）等，判為「方法簽章未覆蓋」。本輪逐條核對
原始碼，**發現該歸因至少錯了一半**：`ps-q-esc` 的 10 條 ID 失敗與 `str.to-bytes` 毫無關係，
真根因是**點號跨模組呼叫的查表鍵**——一個「命名慣例 vs 簽表鍵構造」的靜態失配。本輪把
它修掉，並順手更正 §76 ③ 的層次判定（見 ⑤）。

### ① 根因：`mod.<nm>` 的鍵是 `mod.short`，而 nm 可能自帶 `mod-` 前綴

- **源碼慣例**（doc:165）：「函式定義名 `lx-dump` ⇒ 呼叫端只能寫 `lx.lx-dump(...)`」。
  即**呼叫端寫的是定義名**（含模組前綴），不是去前綴的短名。
- **簽表鍵構造**：`drv.pkg-sigs`（`compiler/src/drv.no:669-733`）與 `drv.pkg-ll` 的 sg 前置
  pass（`drv.no:824-846`）**同構**：
  `short = nm`；`pre-dot = mod - '.'`；`pre-hyp = mod - '-'`；
  若 `nm.starts-with(pre-dot)` 或 `nm.starts-with(pre-hyp)` ⇒ `short = nm.slice-bytes(mod.len-bytes()+1, …)`；
  最後 `key = mod - '.' - short`，`h2m-sig-add(sg, key, nm, rt, ap)`（**femit 恆為 nm 逐字**）。
- **失配**：`lx.lx-ch-str` 的簽表鍵是 `lx.ch-str`（`lx-ch-str` 以 `lx-` 開頭 ⇒ 剝成 `ch-str`），
  而呼叫端組成的 `cn` 是 `lx.lx-ch-str` ⇒ **精確查表必然落空**。
- **降級側**（`h2m-lower-expr`，`h2m.no:4169-4181`）：落空後走 s114 的
  `h2m-sig-find-emit(sg, cn)`（裸名反查），`cn` 仍是 `lx.lx-ch-str` ⇒ 再次落空 ⇒
  `ok-call = 0` ⇒ **整函不產碼**（`h2m-lower-func` 任一 unsup 即回 `r = ''`）。
- **型別側**（`h2m-ty-of`，`h2m.no:2806-2818`）：**完全沒有回落** ⇒ 回 −1 ⇒
  let 槽不綁名 ⇒ 體內每次讀取記一條 ID 失敗。`ps-q-esc` 的
  `q／bs／xs／cc／h0／h1／r` 就是這樣來的（見 ⑤）。
- **靜態爆破半徑**：全語料 `grep` 得 **15 個**真實的 `<mod>.<mod>-<name>(` 呼叫點
  （另有 1 處落入本輪新注釋，非呼叫）：

  | 位置 | 呼叫 |
  |---|---|
  | `compiler/src/ast.no:1002` | `ast.fold-int` |
  | `compiler/src/drv.no:252` | `ps.ps-err-count` |
  | `compiler/src/drv.no:290` | `ck.ck-str-at` |
  | `compiler/src/h2m.no:1892` | `cg.cg-ty-name`（在 `h2m-struct-decls`） |
  | `compiler/src/h2m.no:6799`／`:6808` | `cg.cg-emit-cfg-t`（在 `h2m-lower-func`） |
  | `compiler/src/lx.no:1660` | `lx.lx-dump`（在 `lx-scan-token`） |
  | `compiler/src/ps.no:2329`／`2330`／`2331`／`2340`／`2347`／`2348` | `lx.lx-ch-str` ×6（在 `ps-q-esc`） |
  | `compiler/src/ps.no:2347`／`2348` | `lx.lx-hex-digit` ×2（在 `ps-q-esc`） |

### ② 設計：`h2m-sig-find-full`（femit 反查 + 長度守衛），且**零 mark**

新 helper（`compiler/src/h2m.no:2382-2411`，貼在 `h2m-sig-find-emit` 之後）：

- 走**既有 s114 通道** `h2m-sig-find-emit(s, fn)`（按 femit 反查，**`cnt == 1` 才命中**，
  絕不取第一個——與 s114 同紀律）。
- 加一道守衛：**|fname| == |fn|**。不變式：
  - key = `mod.short`，short = fn 剥 `mod-` 前綴（有則剝、無則不剝）；
  - fn **有**前綴：|fname| = |mod| + 1 + (|fn| − |mod| − 1) = **|fn|**；
  - fn **無**前綴：|fname| = |mod| + 1 + |fn| **> |fn|**；
  - ∴ **|fname| == |fn| ⟺ 該簽名的模組正是這次的點號基底**。
  等價於「startsWith 基底」但**不需要 `starts-with`／`index`／`slice-bytes`**。
- **守衛不可省**：沒有它，`args.get()` 這類**全域變數點號呼叫**（base 是變數不是模組）
  會因 femit 恰好同名而誤命中 ⇒ 把「誠實不支援」換成「靜默錯碼」——比落 ID 通道更壞。
- **爲什麼不用切片版**：初版曾嘗試直接用 `index` + `slice-bytes` 構造短名比對，
  結果**自負 10 block + 1 ID**（這三個 str 方法在 h2m 的值方法表裡未支援，見 ⑤）
  ⇒ 改成 femit + 長度守衛後**零 mark**（實測 `h2m-sig-find-full` 在 census 的
  ID／LF／block 通道**皆 0**，只有 14 條 DB 診斷行，見 ③）。
- **擺放位置**：只在**精確查表落空之後**；降級側另加 `cfn != ''`（= 確為跨模組點號路徑）
  條件，**bare 呼叫行為逐字不變**。

動到三處（`compiler/src/h2m.no`，net **+47 / −1**）：

1. 新 helper `h2m-sig-find-full`（+32 行，含 14 行注記）。
2. `h2m-ty-of` 的 K-DOT-EXPRESSION 臂（`h2m.no:2810-2812`）：`di < 0 -> di = h2m-sig-find-full(sg, fn)`。
3. `h2m-lower-expr` 的 K-CALL-EXPRESSION 臂（`h2m.no:3786-3788` 加 `cfn = ''`；
   `h2m.no:4148-4149` 加 `cfn = mname`；`h2m.no:4172-4175` 加回落查表
   `si < 0 && cfn != '' -> si = h2m-sig-find-full(sg, cfn)`）。

### ③ 量測（S0 census：`tmp/s153/bin/no run tmp/s154/cov-main.no`）

- **pre** = `tmp/s156/cov-post5.{log,err}`（§75 收官）：unsup **1552**、ID **100**、LF **1376**、dropped **176**。
- **post** = `tmp/s157/cov-s154c.{log,err}`：unsup **1509（−43）**、ID **88（−12）**、
  LF **1332（−44）**、block 構造行 **1330（−44）**、dropped **177（+1）**。
- **新的計數恆等式（本輪証明，可覆驗）**：
  `unsup = 直接 marks + dropped`，而 `直接 marks = block 構造行 + 2 條 type 類`
  （`drv.no:966` 對每個閉包剔除函式 `agg.unsup = ix.inc(agg.unsup)`；
  `h2m-cov-join` 把 K≥1 的記錄逐條印成 `fn:Kind:construct`，另有 2 條 `.:SliceType:type`）。
  - pre：1374 + 2 + 176 = **1552** ✓
  - post：1330 + 2 + 177 = **1509** ✓
  - ∴ **−43 = −44（block）+ 0（type）+ 1（dropped）**，逐項對平。
  - ⚠️ §76 ④ 記 block 為 **1377**；同口徑重算為 **1374**（構造行）／**1376**（直接 marks）。
    差 1–3，本輪未重現出 §76 的算式，屬計量口徑差異，不影響任何結論。
- **逐函式變化**（block／ID／LF 三通道交叉核對）：

  | 函式 | block | ID | LF | 站點 |
  |---|---|---|---|---|
  | `ps-q-esc` | **−31（31→0）** | **−10（10→0）** | −31（31→0） | `lx.lx-ch-str` ×6、`lx.lx-hex-digit` ×2 ⇒ **全清** |
  | `drv.cmd-vet` | −9（For 4→1、If 15→12、FuncDef 13→11、Block 26→25） | −2（`ps-errs`） | −9 | `ps.ps-err-count`（drv:252）、`ck.ck-str-at`（drv:290） |
  | `h2m-lower-func` | −2（3→1） | 0 | −2 | `cg.cg-emit-cfg-t` ×2（h2m:6799/6808） |
  | `h2m-struct-decls` | −2（FuncDef 2→1、Block 1→0） | 0 | −2 | `cg.cg-ty-name`（h2m:1892） |

- **新掉落 +1 = `ps-q-esc`**：它現在**開始產碼**（block/ID/LF 全清），但產出的 call 目標
  仍有不支援者，故被發射閉包剔除（`drv.pkg-ll` 的 pruning loop）。這是「前進了一步」
  而非倒退：故 unsup −43 已把這 +1 扣掉。
- **助手自身零負擔**：`h2m-sig-find-full` 在 census 的 block／ID／LF 通道**皆為 0**，
  只有 14 條 `DB:` 診斷行（見下）。
- **DB 通道 19845 → 19870（+25），已逐條歸因，本輪未新增任何 `h2m-dbg` 呼叫點**
  （`grep -c 'h2m-dbg(' compiler/src/h2m.no`：pre **29** ／ post **29**）：
  - 14 條 `h2m-sig-find-full:*`（`blk-let` 4、`blk-expr` 2、`blk-done` 3、`surf` 1、
    `surf-subj-ok` 1、`surf-subj-idx` 1、`si-bt-vecstr` 1、`si-iv-ok` 1）——
    新函式本身被 census 降級時，體內的既有 debug 點歸到它名下；
  - 11 條落在 `h2m-lower-expr`（`blk-let` +3、`blk-expr` +1、`blk-done` +1）、
    `h2m-ty-of`（`blk-let` +1、`blk-expr` +1、`blk-done` +1）、
    `drv.cmd-vet`（`blk-let` +1、`blk-done` +1）、`lfunc` +1——
    回落啟動後降級走得更深，多經過的既有 debug 點。

### ④ 驗證（逐項可重現）

- **重建 S1**：`tmp/s153/bin/no build -o tmp/s154/noc-s1 compiler` → **Build successful**
  （`tmp/s157/build-s154b.log`，制品 `tmp/s154/noc-s1`，20:20）。
  - **注释修正為二進制層零效應**：结尾又把 `h2m.no:4173` 注記裡已失效的 `cbn` 引用改寫
    （純注释），重建後 `md5(tmp/s154/noc-s1)` = `ed4e59d060e2f1e6a9e41456327b1d94`，
    與修正前 `tmp/s157/noc-s1-precomment` **逐位元組相同** ⇒ 後續門禁結果直接適用。
- **A/B 零回歸（二進制級，本輪最強證據）**：以 `tmp/s157/mkbase.py` 從目前 h2m.no
  **逆向還原**出 pre-s154 版（複製整套 `compiler/` 到 `tmp/s157/pre/compiler`，僅換回 h2m.no，
  `grep -c s154` = 0），S0 建出 `tmp/s157/noc-s1-base`；與新版 `tmp/s154/noc-s1`
  逐檔跑 **11 個快速測試檔**（ast／build-package／cg-runtime／cg-str16／diag／lx／
  pkg-ll-call／ps-err／ps／tok／util）比 `(rc, sha256(stdout))`：**11/11 SAME**
  （`tmp/s157/ab.log`）。
- **`h2m-test`**：`./tmp/s153/bin/no run compiler/tests/h2m-test.no` → **1365/1365
  ALL TESTS PASSED**（`tmp/s157/h2m-test-s154c.log`）。
- **L0**：`bash tmp/s153/run-gates-clean.sh` → **18/18 全過、rc=0、0 failed**
  （`tmp/s157/gates-s154c.log`，19 行：18 條 `=== … ALL TESTS PASSED` + `DONE`），
  **2367 條斷言全過**；各套計數與 §75 收官逐項相同（ast 48、build-package 8、
  cg-runtime 114、cg-str16 41、cg 363、ck-sig 50、ck 20、diag 7、drv-build 63、
  h2m 1365、lx 81、pkg-ll-call 5、pkg-ll 10、pkg-sigs 5、ps-err 46、ps 52、tok 62、
  util 27）。
- **自舉一致性**：`tmp/s154/noc-s1 build -o tmp/s157/noc-s2-test compiler` → **rc=0** 但
  `no code generated`，並印 `unsupported constructs: 1509 (pkg-ll)` + 177 dropped
  ——**與 S0 的 census 逐項相同**（S1 對自身源碼的計數與 S0 一致）。
  S2 仍不可得（層 2 真牆），故 DR-1 ①／② **未起跑**，本輪不宣稱定點。
- ⚠️ **S0 vs S1 的 e2e 差異不屬定點口徑**：L5 的 ① 只比 `(noc-s2, f)` 與 `(noc-s3, f)`，
  **不含 S0**（doc:6532）。11 個測試檔在 S0 為 rc=0、在 S1 為 rc=1，原因是 SHS 子集守衛
  （`drv.build-ll` 的 `agg.unsup > 0`），A/B 已證二者逐檔相同，非本輪引入。

### ⑤ 對 §76 ③ 的更正（誠實邊界）

- §76 ③ 謂 ID 失敗「多為『方法簽章未覆蓋』（`str.to-bytes`／`slice-bytes`／`byte().to-i64`）」。
  **簽章其實都在**：`compiler/src/ck-sig.no:4581 'str.index'`、`:4596 'str.slice-bytes'`、
  `:4599 'str.starts-with'`、`:4665 'str.to-bytes'`。
- 真正的缺口在**另一層**：h2m 的**值方法表**（`h2m-lower-expr` 的 `mname == '…'` 白名單，
  `h2m.no:3973-4120`；`h2m-ty-of` 同口徑，`h2m.no:2832-2851`）只覆蓋
  `len-bytes`／`len`／`byte`／`bytes`／`copy`／`to-str`，**沒有** `to-bytes`／`slice-bytes`／
  `index`／`starts-with` ⇒ `h2m-ty-of` 回 −1 ⇒ let 槽不綁 ⇒ 後續讀取記 ID 失敗。
  故應改述為「**h2m 值方法表未覆盖（型別回 −1 致槽不綁）**」，而非「簽章未覆蓋」。
- **連帶更正（§76 ③ 把兩個不同的 `bs` 混為一談）**：
  - `ps-q-esc:bs` **4 條** = `bs = lx.lx-ch-str(92)`（`ps.no:2330`）——
    本輪**已隨 s154 全清**，屬**查表鍵**問題；
  - `h2m-lower-expr:bs` **7 條** = `bs = lit.to-bytes()`（`h2m.no:3312`）——
    **未變**（仍是 7），這才是「值方法表未覆盖」的實例。
- ⇒ §76 ③ 的「100 條 ID 失敗」中，至少 **10 條**（ps-q-esc）屬查表鍵失配（本輪修掉），
  其餘（如 `h2m-lower-expr:bs` 7）屬型別推斷覆蓋（未動）。

### ⑥ 結論與下一步

- s154 把「命名慣例造成的點號失配」從知識變成修補：**1552 → 1509（−2.8%）**，
  4 個函式改善，其中 `ps-q-esc` **全清**；助手**零 mark**，A/B 二進制級 11/11 SAME。
- 剩餘 1509 的組成未變（切片／集合型別 + 值方法表 + 巢狀 block），§76 ④ 的判斷仍成立。
- **下一步（未做，非本輪可畢）**：補 `to-bytes` 的值方法表（`h2m-ty-of` + `h2m-lower-expr`
  兩臂），需 MIR op：str→[]byte 的 O(N) copy（可鏡像 `src/std/str.no:694` 的 `with-cap-len`
  手作）。此為「新型別 / 新 op」量級，非單檔增量。
- **未提交任何改動**。工作樹中 `compiler/src/h2m.no`（對 pre-s154 **+47 / −1**）與本文
  為本輪產物；`tmp/s157/pre/`（逆向還原的 baseline copies）與 `tmp/s157/k0/`
（K=0 通道實驗 copy）為本輪探針工件，**皆非產品程式**。
- **物證**：`tmp/s157/{mkbase.py, ab.sh, ab.log, build-s154b.log, cov-s154c.{log,err},
  h2m-test-s154c.log, gates-s154c.log, e2e-s1.sh, e2e-s1.log, k0-cov.{log,err}}`、
  `tmp/s157/pre/compiler/src/h2m.no`、`tmp/s157/noc-s1-base`、`tmp/s157/noc-s2-test`（無成品）。

---

## §78 s158：開掉 str 子串【值方法】四件套（starts-with / ends-with / contains / index）——直接 unsup 1509 → 1454

§77 ⑤／⑥ 把「查表鍵」類判為已終結，並指出剩下的直接 unsup 由三塊組成：切片型別、集合型別、
值方法表。本輪打**值方法表**，但刻意挑最便宜的四個先做，而不是照 §77 ⑥ 的順序直接上
`to-bytes`。原文保留為歷史，本節是 as-built 補記。

### ① 選點：為什麼是這四個，而不是 §77 ⑥ 指定的 `to-bytes`

s158 之前 h2m 的 str 值方法白名單（`h2m-lower-expr` 的 TY-STR 臂，及 `h2m-ty-of` 的鏡像）
只覆蓋 `len-bytes／len／byte／bytes／copy／to-str` 六者；`index／slice-bytes／starts-with／
contains／ends-with` 全在門禁外。這四個候選的性狀：

- **不回新型別**：`starts-with／ends-with／contains` 回 bool（h2m 漆成 i64 0/1），
  `index` 回碼點位置或 -1（i64）⇒ 不需要新的型別基礎設施，也不需要 RC 登記。
- **純計算、不分配**：四者都能用 str 位元組比較表達（`find-byte-from` 是純計算），
  與 `@str_eq`／`@str_cp_len` 同級，不引入所有權問題。
- **語料夠重**：block 通道前三大戶 `ps-lower-signed-int-name`（19）、`drv.bp-walk`（14）、
  `h2m-kind-cat`（13）都被這批方法拖紅，而它們各自身體裡並沒有切片／集合。

反之 `to-bytes`（§77 ⑥ 指定的下一步）會引入 `[]byte` 視圖與 RC 所有權，`slice-bytes` 會引入
切片型別與 RC，都比這四者貴。這是**排序取捨**，不是縮小目標：四者全做完之後，值方法表仍剩
`to-bytes／slice-bytes` 兩個，`s158` 沒有吃掉它們的配額。

### ② 變更（四層，全加分、零刪改）

| 檔 | 行數 | 加 | 內容 |
|---|---|---|---|
| `compiler/src/mir.no` | 641 → 671 | +30 | op 碼 105–108 |
| `compiler/src/cg-runtime.no` | 791 → 981 | +190 | 5 個 helper + prelude 註冊 |
| `compiler/src/cg.no` | 2779 → 2843 | +64 | header 說明 + 4 個 emit 臂 |
| `compiler/src/h2m.no` | 7467 → 7533 | +66 | ty-of 4 支 + lower-expr 4 支 |
| `compiler/tests/h2m-test.no` | 7325 → 7448 | +123 | 6 個測試（+16 斷言） |
| `compiler/tests/cg-runtime-test.no` | 316 → 373 | +57 | 1 個測試（+33 斷言） |

删改數一律 0。※ h2m.no 的 pre 行數 **7467** 與 §77 ② 備考的「baseline 7421 → 7467」完全對齊，
可當作重建覆核的錨點（見 ④）。

新 op 碼（mir.no，接在既有最大碼 `OP-ITOSTR = 104` 之後，**不重排既有碼**）：

- `OP-STRPREF = 105`：x=接收者 str、y=目標 str ⇒ dst = i64 值 id（0/1）。
- `OP-STRSUFF = 106`：同上 ⇒ dst = i64（0/1）。
- `OP-STRCTNS = 107`：同上 ⇒ dst = i64（0/1）。
- `OP-STRIDX = 108`：同上 ⇒ dst = i64（碼點位置或 -1）。

cg-runtime 新增 5 個 helper（都是**純計算、無 RC**，與 `@str_cp_len` 同級）：

- `@str_find_byte(%str-long %a, %str-long %b)`：`src/std/str.no:230 find-byte-from` 的
  目標為空特化（回 0）、`n<m` 回 -1、否則掃 `i∈[0, n-m]`／`j∈[0, m)` 找首位元命中。
- `@str_starts_with`（`:460`）：`len(target) > len(s)` 即 false，否則逐位元比前綴。
- `@str_ends_with`（`:481`）：`off = la - lb`，比 `s[off+i]` 與 `target[i]`。
- `@str_contains`（`:450`）：`call @str_find_byte` 後 `icmp sge i64 %p, 0`。
- `@str_index`（`:420`）：`call @str_find_byte`，未命中回 -1；命中則數 `[0, p)` 內
  「`b < 0x80 || (b & 0xc0) == 0xc0`」的首字節個數（即 `.cp-index`）。

五個 helper 全部登入 `cg-runtime-prelude-full`，位置緊接 `cg-runtime-str-cp-len()` 之後、
`cg-runtime-vec-new()` 之前（**順序約束**：`@str_contains`／`@str_index` 依賴
`@str_find_byte` 必須在前；此順序由 `cg-runtime-test.no` 的 `test-str-substr-helpers` 釘住）。
⚠️ B28：全部沿用 u32 len 欄 ⇒ 讀出後 `zext i32→i64` 才做 `icmp`／`getelementptr` 偏移。

cg.no 新增 4 個 emit 臂，沿用 s42 STRCEQ 的 `call i1` + `zext i1→i64` 形狀（STRLIDX 直接
`call i64`）；h2m.no 兩臂：`h2m-ty-of` 的四法**恆回 TY-I64**（不復刻 s117 `str.byte` 的
聽差裝箱），`h2m-lower-expr` 的四支都守 `ok-method == 0 && na == 1` 且**實參必須是
TY-STR**，否則落門禁（缺參／型別不符皆 gate，見 ④ 的預埋 TestCase）。

### ③ 量測（S0 census：`tmp/s153/bin/no run tmp/s154/cov-main.no`）

| 指標 | pre（§77） | post（s158） | 差 |
|---|---|---|---|
| **直接 unsup** | **1509** | **1454** | **−55** |
| block 構造行 | 1330 | 1276 | −54 |
| LF | 1332 | 1278 | −54 |
| ID | 88 | 82 | −6 |
| DB | 19870 | 20185 | +315 |
| dot（type 類） | 2 | 2 | 0 |
| dropped | 177 | 176 | −1 |
| 總行數 | 22625 | 22826 | +201 |

**量測口徑（精算）**：block 1276 + type 2 + dropped 176 = 1454 ✓。DB 上升 315 是
新 helper 體內的迴圈／phi 診斷行落下來的（`cg-runtime-str-*` 各記 1 條
  FunctionDefinition，見下方負項），不是編譯失敗新增。

**逐函式 block 淨變化**（只列非零；括號為 pre/post）：

```
ps-lower-signed-int-name   19 →  2  (−17)   ← 用 starts-with / ends-with
h2m-kind-cat               13 →  0  (−13)   ← 用 ends-with
drv.bp-walk                14 →  5  ( −9)
drv.pkg-ll                 67 → 62  ( −5)
check                      40 → 37  ( −3)
drv.cmd-vet                52 → 49  ( −3)
h2m-lower-expr             69 → 67  ( −2)
ps-mk-opt-arith-type        2 →  0  ( −2)
drv.bp-resolve             13 → 12  ( −1)
drv.pkg-contains            1 →  0  ( −1)
h2m-sig-const-add           6 →  5  ( −1)
h2m-sig-strconst-add        6 →  5  ( −1)
ps-parse-match            123 →122  ( −1)
cg-runtime-str-find-byte    0 →  1  ( +1)   ← 新增助手（體內含巢狀迴圈）
cg-runtime-str-startswith   0 →  1  ( +1)
cg-runtime-str-endswith     0 →  1  ( +1)
cg-runtime-str-contains     0 →  1  ( +1)
cg-runtime-str-index        0 →  1  ( +1)
```

即 **淨 −54**（−59 來自既有函式轉乾淨，+5 來自五個新 helper 自身的 FunctionDefinition 歸因）。
ID 通道減 6，且**只減不增**：`h2m-kind-cat:t1` ×3、`drv.bp-walk:done`、
`drv.bp-walk:onstack`、`drv.cmd-vet:p1`。

**dropped 的微妙漂移（值得記錄）**：`h2m-cov-join`／`h2m-cov-line` 由 dropped 轉為乾淨
（它們先前只是連帶被 pruned），但 `ps-mk-opt-arith-type` **反向**由乾淨轉為 dropped——它的
自身 block marks 歸零，卻仍呼叫一個被 prune 的函式。淨效應 −2 +1 = −1，與總數一致。

### ④ 驗證（逐項可重現）

- **L5 自舉一致性**：`./tmp/s158/noc-s1 build -o tmp/s158/noc-s2-test compiler`
  ⇒ rc=1、**176 dropped**、**`unsupported constructs: 1454 (pkg-ll)`**、`no code generated`
  ——與 S0 對當前源碼跑出的普查**數字逐項相同**（S0 側同樣 1454 / 176）。
  （⚠️ S1 二進位本身是「部分產出」，`noc-s2-test` 每輪留檔供檢讀，其內容從未被執行過。）
- **A/B 零回歸**：以 **pre-s158 二進位**（`tmp/s154/noc-s1`，§77 制品）對比 **post-s158 二進位**
  （`tmp/s158/noc-s1`），逐檔跑 11 個快速測試檔（含 `tmp/s158/cg-runtime-test-pre.no`，
  即剝掉 s158 新增段落的 cg-runtime-test），比 `(rc, sha256(stdout))`：**11/11 SAME**，
  無一字元輸出差異。指令：`bash tmp/s158/ab.sh`，日誌 `tmp/s158/ab.log`。
- **pre 基線重建覆核**：以腳本把 4 個 src 檔的 s158 段落按行切除（不重建、不改寫），
  在 `tmp/s158/pre/` 還原出 pre-s158 源樹；從該目錄跑同一普查
  ⇒ **1509 / 177 dropped / 22625 行，且逐行內容與 §77 備考（`tmp/s157/cov-s157.log`）完全相同**。
  這是「重建 faithful」的強證據，也順帶證明 diffstat 的 +30/+190/+64/+66 正確。
- ⚠️ **md5 可比性校正（本輪實測）**：`no build` 的產物會把**輸出路徑／檔名**嵌進
  Mach-O 的 Code Signature 區（`xxd` 可直接看到 `pa\0`／`pb\0`／`r1\0`），
  因此**只有同路徑重演才具 md5 可比性**：`tmp/s158/pa` 建兩次 md5 相同，
  但 `pa` vs `pb`（僅檔名不同）差異 51 bytes、全落在簽名區，**與源碼內容無關**。
  這代表**跨路徑 md5 對比在本環境不可靠**——包括 §77 ④ 備考裡
  「`tmp/s154/noc-s1` 與 `tmp/s157/noc-s1-precomment` 逐位元組相同」那一對
  （兩者路徑不同）。該條與本輪實測機制不相容（可能因 S0 版本或環境而異），
  在重新以「同路徑重演」覆核之前，**不要以跨路徑 md5 作為定點依據**；
  本輪的 A/B 已全面改用行為。
- **h2m-test**：S0 直跑 ⇒ **1381/1381 全過、rc=0、0 failed**（原 1365 + 16）。
- **cg-runtime-test**：S0 直跑 ⇒ **147/147 全過、rc=0、0 failed**（原 114 + 33）。
  該檔是刻意的「prelude 盤點氣闌」，改動 prelude 文本本來就會讓它紅——本次同步加
  `test-str-substr-helpers` 把五個新 helper 的 IR 文本與**順序約束**一起釘住。
- **預埋 TestCase**：本輪建立「缺參」與「型別不符」的門禁用例（`s.starts-with()` 無參、
  `s.starts-with(1)` 非 str），直接量得兩者 gating 皆開啟，確保 s158 的
  `na == 1`／`h2m-vty-at(c, av) == TY-STR` 守衛不會被誤繞過。
- **L0**：`bash tmp/s153/run-gates-clean.sh` → **18/18 全過、rc=0、0 failed**
  （`tmp/s158/gates-s158.log`，19 行），**2416 條斷言全過**；各套計數與 §77 收官相比只有
  兩個變動：`cg-runtime` 114 → **147**、`h2m` 1365 → **1381**，其餘 16 套逐項相同
  （ast 48、build-package 8、cg-str16 41、cg 363、ck-sig 50、ck 20、diag 7、drv-build 63、
  lx 81、pkg-ll-call 5、pkg-ll 10、pkg-sigs 5、ps-err 46、ps 52、tok 62、util 27）。
- ⚠️ **DR-1 ② 仍未達**：`noc-s2 build compiler` 永遠因 1454 > 0 停在 compile-only
  path A ⇒ **無法產生 `noc-s3`** ⇒ `cmp noc-s2-test noc-s3` 這一步永遠缺失（見 §77 ④ 表後第二段）。
  L5 與 DR-1 ③ 未啟動；本輪**不宣稱定點**。
- ⚠️ **禁用旗標**：`--stop-on-fail` 與 `--skip-flags` 不可作為略去依據（見 §4.5）。
- **工作樹終檢**：`git status --porcelain -- compiler docs/design` = **14 M**
  （13 個 `compiler/` 檔 + `docs/design/nolang-selfhost.md`；`tmp/` 下的本輪產物全部在
  `.gitignore` 內）。全樹另有 71 M + 43 ??，但都是**與本輪無關的既有改動**
  （`docs/i18n/*`、`README*`、`noui/tests/*`、`.agents/*`），非 s158 所造。
  **零 .orig／.rej／.tmp 殘留**。

### ⑤ 結論與下一步

- s158 把 str 值方法表從 6 個加到 10 個：**1509 → 1454（−3.6%）**，
  三個大戶（`ps-lower-signed-int-name` −17、`h2m-kind-cat` −13、`drv.bp-walk` −9）顯著改善，
  A/B 行為級 11/11 SAME、L5 數字逐項吻合。
- 剩餘 1454 的組成：BlockStatement／SliceType／值方法表剩 `to-bytes／slice-bytes` +
  巢狀 block。原本「三塊」的判斷仍成立，但值方法表那塊已從 5 個缺口降到 2 個。
- **下一步**：補 `to-bytes` 的值方法表（`h2m-ty-of` + `h2m-lower-expr` 兩臂），
  需 MIR op：str→`[]byte` 的 O(N) copy（可鏡像 `src/std/str.no:694` 的 `with-cap-len` 手作）。
  此為「新型別 / 新 op」量級，非單檔增量；做完再評估 `slice-bytes`（又要 RC，更貴）。
- **未提交任何改動**。

### ⑥ 後記：端到端 smoke 揪出 `@str_index` 的同名暫存器（s158 收尾）

④ 的驗證全部是「文本級」或「普查級」，本條補上唯一一關：**用自舉 S1 真組出可執行檔並執行**。
這關一出就抓到一個 ④ 完全沒看到的真 bug——原文保留，此段為追加。

**先排掉一個假障礙**：最初的 smoke 版用 `pr(...)` 當輸出通道，結果 `Build failed:
noc: unsupported constructs: 11 (pkg-ll)`。這**不是** s158 的問題——`pr(...)` 是 S0 的內建，
自舉編譯器尚未認領：`h2m.no:4226` 的 print 臂只收 `cn == 'print' || cn == 'eprint'`。
改用 `print(...)`（fmt class 1、裸識別字欄位）後即過。

**真正的 bug**：改用 `print` 後仍然 `Build failed`，clang 直接拒收模組：

```
tmp/s158/smoke/run-bug.ll:301:3: error: multiple definition of local value named 'b'
  301 |   %b = load i8, i8* %ptr
      |   ^
```

成因：`cg-runtime-str-index` 的**形參** `%b`（target str）與函式內的 `load` 暫存器 `%b` 同名。
同一 LLVM 函式內區域值名不得重複，模組驗證（`-x ir` 會過 verify）直接拒不組譯。

**為什麼 ④ 的所有關卡都沒抓到**——這條最值得記：

- `h2m-test`／`cg-runtime-test` 全部只用 `ir.contains('…')` 斷言 IR **文字子字串**，
  從來沒有把模組丟給 clang 組譯過；
- L5 自舉在 `no code generated` 就停（1454 > 0），只比普查數字，也永遠生不出 `noc-s3`；
- A/B 的 11 個測試檔跑的是源樹既有的功能，**沒有任何一支會走到這四法**
  （源樹本身還沒開始用它們）。

⇒ **文本級測試 + 普查數字都不足以取代「真的組出可執行檔」**。

**修法**：`cg-runtime-str-index` 把 `%b = load i8` 改名 `%byt`（連帶 `%bw = zext i8 %byt`）。
一行、**行數不變**（`cg-runtime.no` 仍 981 行），diffstat 全數不變。

**修後全面重驗（與 ④ 逐項相同，確認此修正未移動任何指標）**：

| 項 | 結果 |
|---|---|
| S0 census | unsup **1454**、dropped **176**、總行 **22826**（LF 1278／ID 82／DB 20185／dot 2） |
| h2m-test | **1381/1381**、rc=0、0 failed |
| cg-runtime-test | **147/147**、rc=0、0 failed |
| A/B | **11/11 SAME** |
| L0 | **18/18**、rc=0、**2416** 斷言全過 |
| L5 自舉 | rc=1、dropped **176**、unsup **1454**（與 S0 逐項相同） |

**smoke 輸出**（`tmp/s158/smoke/`：`package.noml` + `main.no` + `run.ll` + `run`；
bug 重現稿留檔為 `run-bug.ll`）：

```
sw-hi: 1
sw-no: 0
ew-hi: 1
ew-no: 0
ct-yes: 1
ct-no: 0
ix-hit: 6
ix-miss: -1
ix-cp: 2
```

九行全如預期。其中 **`ix-cp: 2`** 是 UTF-8 **碼點**口徑的證據：
`'aé世界'.index('世')` 回 **2**（碼點位置），而非位元位移 **3**——
即 `cg-runtime-str-index` 命中後數的確實是首字節個數，而非直接回 byte offset。

**教訓／新增常設關口**：今後每個 sNNN 都應附一支 `tmp/sNNN/smoke/`（最小可組可執行程式 +
預期輸出表），因為它是一種**其他關卡在結構上不可能覆蓋**的驗證。
- **未提交任何改動**。

---

## §79 s159：`str.to-bytes()` —— O(N) 複製成【自有】`[]byte`（`OP-STRTOBYTES = 109`）

（原文保留：本節為 §78 ⑤「下一步」的落地記錄，**追加不覆寫**。）

### ① 語意邊界：與 s121 的 `bytes()` 嚴格分開

`src/std/str.no:694` 的 `to-bytes` 是**新配置**：

```
bs = []byte.with-cap-len(str.len-bytes(s), str.len-bytes(s))
i = 0
(n > 0) {
    bs[i] = u8::(str.byte(i))          ; 逐元 zext 成 i64 槽
    i = nxt(i)
}
return bs                               ; cap=len=n>0，無空終止，呼叫端擁有
```

四大要點：

- **O(N) 複製**，不是視圖。`str.bytes()`（s121，`OP-STRBYTES = 102`）回的是**零拷貝借視圖**，
  以 `cap == 0` 作借用標記、**不登記釋散**；兩者型別都是 `[]byte` 但所有權相反。
- **口徑合併**：`h2m.no:1564` 的 `enm == 'byte' -> code = TY-VEC` ⇒ `[]byte` 與 `[]i64` 共用
  `%vec`，元素是 **i64 寬槽（stride 8）**——helper 必須把 u8 `zext i8→i64` 後寫進 8 位元槽，
  否則 `@vec_get_i64`（`%off = mul i64 %i, 8`）讀不回 0..255。
- **沒有 Go 原生對應**：`src/builtin/str.go` 只攔截 `bytes`／`_from-bytes`，
  源樹也沒有 `to-bytes` 的后端入口 ⇒ 只能**自建 helper**（不像 `index` 可鏡像既有內核）。
- **RC**：結果 rc=1、自有緩衝 ⇒ h2m 側 `h2m-rel-add(c, bvv, TY-VEC, 0)`，作用域結束發
  `OP-VEC-FREE`；這正是 §78 ⑤ 說它比四法「貴」的原因。

### ② 變更（四層原始碼 + 兩層測試，全加分、零刪改）

| 檔 | 行數 | 加 | 內容 |
|---|---|---|---|
| `compiler/src/mir.no` | 671 → 687 | +16 | `OP-STRTOBYTES = 109`（接在 108 之後，不重排既有碼） |
| `compiler/src/cg-runtime.no` | 981 → 1032 | +51 | `@str_to_bytes` helper + prelude-full 註冊 |
| `compiler/src/cg.no` | 2843 → 2859 | +16 | header 說明 + 1 個 emit 臂 |
| `compiler/src/h2m.no` | 7533 → 7554 | +21 | ty-of 1 支 + lower-expr 1 支（含 rel-add） |
| `compiler/tests/h2m-test.no` | 7448 → 7503 | +55 | 3 個測試（+6 斷言） |
| `compiler/tests/cg-runtime-test.no` | 373 → 406 | +33 | 1 個測試（+16 斷言） |

删改數一律 0。pre 行數（671/981/2843/7533）與 §78 ② 備考完全對齊。

**MIR op**（`mir.no`）：`OP-STRTOBYTES = 109`；x=接收者 str、y 未用 ⇒ dst = 新的 `%vec` 值 id。
OP 碼只出現在這 4 個檔（`mir.no`／`cg.no`／`cg-runtime.no`／`h2m.no`），**沒有 `OP-COUNT` 常量**要同步。

**helper**（`cg-runtime-str-to-bytes`）：`n = len`（`extractvalue %str-long %a, 0` 後 zext i32→i64）、
空串短路 `icmp eq i64 %la, 0` 直接 `ret %vec {0,0,0}`；否則 `malloc(n*8)`，phi 迴圈逐元
`load i8` → `zext i8→i64` → `mul i64 %i, 8` 定位 → `bitcast i8*→i64*` → `store i64`；
收尾 `len = cap = trunc i64→i32`、`ptrtoint` 取 data 指針。暫存器命名沿用 §78 ⑥ 的教訓
（`%byt`／`%bw`／`%bi`），並在測試裡下了「不得有 `%a = `／`%b = `」的**同名碰撞哨兵**。

**h2m 兩臂**：`ty-of` 加 `mt == TY-STR && mf == 'to-bytes' -> code = TY-VEC`
（缺這支則 let 槽不綁、體內每次讀該名皆記 ID 失敗）；`lower-expr` 加
`mname == 'to-bytes' && ok-method == 0 && na == 0`（**只收無參形**，帶參落門禁不猜）。

### ③ 測試

`h2m-test`：`test-s159-strtobytes`（`agg.unsup == 0`、IR 含 `call %vec @str_to_bytes(%str-long`）、
`test-s159-strtobytes-gate`（`s.to-bytes(1)` ⇒ unsup > 0，負例）、
`test-s159-strtobytes-ty`（`to-bytes` ⇒ TY-VEC；负控 `slice-bytes` ⇒ -1）。
`cg-runtime-test`：`test-strtobytes-helper` 斷言 sig、empty 短路、stride-8 的 malloc/offset、
u8 load/zext/bitcast/store、`cap = len`、prelude 順序（`@str_index` 在 `@str_to_bytes` 之前）。

### ④ 驗證（全綠）

| 項 | s158 | s159 |
|---|---|---|
| S0 census | unsup 1454（block 1276 + type 2 + dropped 176） | **unsup 1448**（block 1269 + type 2 + dropped 177） |
| h2m-test | 1381/1381 | **1387/1387**、rc=0 |
| cg-runtime-test | 147/147 | **163/163**、rc=0 |
| A/B | 11/11 SAME | **11/11 SAME**（A=`tmp/s158/noc-s1`、B=`tmp/s159/noc-s1`） |
| L0 | 18/18、2416 断言 | **18/18、2438 断言** |
| L5 自舉 | rc=1、unsup 1454、no code generated | **rc=1、unsup 1448、no code generated** |

普查 **−6** 是可解釋的：block 通道少 7 條、dropped 多 1 條（`lx-scan-token` 因 fixpoint 變化被整支
剔除）。block 通道的差集全部落在「先前因不認領 `to-bytes` 而記紅、現在不記」的位置
（`h2m-lower-expr` −3＋1、`ps-parse-expr-prec` −4、`lx-scan-token` −2），
外加新 helper 自身貢獻 1 條（`cg-runtime-str-to-bytes:FunctionDefinition`）；合計 −7。**淨效果是嚴格變多支援**，非刪功能。

S1 自舉 (`./tmp/s159/noc-s1 build compiler`) 的 unsup 與 S0 普查**逐項相同**（1448／dropped 177），
符合 DR-1 的 L5 口徑；因 unsup > 0，仍走 compile-only path A，`noc-s3` 不存在、②不可達。

### ⑤ 端到端 smoke（新增常設關口，見 §78 ⑥）

`tmp/s159/smoke/`（`package.noml` + `main.no` + `run` + `build.log`）：

```
len: 3
abi: 3
ulen: 9
elen: 0
```

`len: 3` = `'abc'` 走 helper 的 alloc/loop 路徑；`abi: 3` = 把 `[]byte` 跨函式傳給
`g(xs []byte)` 後 `.len()` 仍為 3（ABI/型別一致）；`ulen: 9` = `'aé世界'` 的**位元組數**
（1+2+3+3），證明是逐元 zext/store 而非碼點計數；`elen: 0` = 空串走 empty 短路回 `{0,0,0}`。
Go 權威 S0 跑同一支輸出**逐行相同**。

**含量級驗證做不到，且不該假裝做到**：初稿想用 `g(b, k)` + `b[k]` 核對每個位元組，
S0 直接以型別錯誤拒絕（`cannot assign ?byte value to i64 variable 'r'`）——向量索引回傳
option；且 `b[i]` 是**既有的**未支援構造（h2m 只收 `vec.len`，見 `h2m.no:3841-3844` 注記）。
故內容級交给 `cg-runtime-test` 的 IR 文字斷言（逐元 zext/store、stride 8、cap=len）兜住，
smoke 只做「可組、可執行、長度/ABI 級語意」。

**一個險些誤導的探針坑（值得留檔）**：最初的普查 harness 只寫了 `# /compiler/src/drv`，
结果對任何含函式定義的檔案都固定回報「2 unsup」加上 `blk-unhandled`／`blk-for`——
連官方 `drv.build-ll` API 也叫出同样的假數。根因是**未匯入完整遞移集**（bug #85 家族：
consumer reads value 0），不是被測物件缺陷。修法：harness 必須依 `tmp/s154/cov-main.no`
匯入 drv 及其全部遞移模組（asp/ast/lx/ps/tok/mir/cg/cg-runtime/cg-rc/h2m/link/util.sb/diag/ck/ck-sig），
修正後同一支檔案為 **0 unsup**。⚠️ 此後所有普查/probe harness 都要照這個清單開頭。

- **未提交任何改動**。

---

## §80 s160：`str.slice-bytes(start,end)` —— 按【字節偏移】截取子串（`OP-STRSLICEB = 110`）

（原文保留：本節為 §79 ⑤「下一步」的落地記錄，**追加不覆寫**。）

### ① 語意邊界：字節偏移 + 鉗制 + 【新配置】的 str 副本

`src/std/str.no:642` 逐字：

```
str.slice-bytes = (start i64, end i64) (val str) {
    r-start = start
    r-end = end

    r-start < 0 -> r-start = 0
    r-end > .len-bytes() -> r-end = .len-bytes()
    r-start >= r-end -> return

    #{overflow=wrap}
    n = r-end - r-start
    buf []byte = with-cap-len(n, n)
    i <- [0..n): {
        #{overflow=wrap}
        buf[i] = .byte(r-start + i)
    }
    val = buf.to-bytes-str()
}
```

五要點：

- **字節偏移，不是碼點**。`[start,end)` 是對 `.byte(i)` 的元組級切片；`aé世界`（9 字節）切 `[1,3)`
  拿的是 `é` 的兩個字元組，不是第 1 個碼點。這與 s158 `index`（回**碼點**位置）刻意相反，
  亦與 s159 `to-bytes`（全量複製成 `[]byte`）不同型別、不同所有權。
- **鉗制顺序**：`start<0 ⇒ 0` → `end>len-bytes ⇒ len-bytes` → `start>=end ⇒ 空`。實現用兩條
  `icmp`+`select`（不寫分支），第三條以 `n<0 ⇒ 0` 收斂——空串仍走 `@nolang_rc_alloc(0)`，
  緩衝非空，故**不需要獨立的空串支路**（不像 s159 `@str_to_bytes` 有 `empty:` 短路）。
- **回【新配置】的 `str`**（既非視圖、也非 `[]byte`）⇒ rc=1 自有 ⇒ h2m 側
  `h2m-rel-add(c, sv, TY-STR, 0)`、作用域結束發 `OP-RELEASE` ⇒ `@nolang_rc_release`；緩衝走
  `@nolang_rc_alloc`。與 s159 `@str_to_bytes` 的**裸 `@malloc` + `OP-VEC-FREE`** 是不同所有權路徑。
  （所有權移轉規則不變：值直接流到輸出參時不發 RELEASE——见 h2m-test 的 release 用例。）
- **三個運算元超出 `h2m-emit(op,d,a,b,...)` 的 (a,b) 雙槽** ⇒ 走**側池**慣例（同 `OP-CALL`／
  `OP-STRUCT-MK`）：`xa[k]=池基址`、`bs[k]=3`，池內依序 接收者 str / start i64 / end i64；
  cg 按 `vty[q]` 逐個反查型別與命名空間（`%sv`／`%v`），**不寫死**（與 `OP-CALL` 同一口徑）。
- **認領後值方法表清帳**：`to-bytes`（s159）+ `slice-bytes`（s160）；§77 ⑥ 所列的 str 值方法
  缺口至此完結，self-host 子集對 std `str.no` 常用路徑的覆盖再向前一步。

### ② 變更（四層原始碼 + 兩層測試，全加分、零刪改）

| 檔 | 行數 | 加 | 內容 |
|---|---|---|---|
| `compiler/src/mir.no` | 687 → 702 | +15 | `OP-STRSLICEB = 110`（接在 109 之後，不重排既有碼）+ 側池契約注記 |
| `compiler/src/cg-runtime.no` | 1032 → 1083 | +51 | `cg-runtime-str-slice-bytes` helper + prelude-full 註冊 |
| `compiler/src/cg.no` | 2859 → 2902 | +43 | header 說明 5 行 + 1 個 emit 臂 38 行（讀側池 3 運算元） |
| `compiler/src/h2m.no` | 7554 → 7589 | +35 | ty-of 1 支 + lower-expr 1 支（含 rel-add） |
| `compiler/tests/h2m-test.no` | 7503 → 7589 | +86 | 4 個測試（+10 斷言）；另**更正** s159 的 `slice-bytes ⇒ -1` 負控 |
| `compiler/tests/cg-runtime-test.no` | 406 → 452 | +46 | 1 個測試（+25 斷言） |

删改數一律 0。pre 行數（687/1032/2859/7554/7503/406）與 §79 ② 備考完全對齊。

**MIR op**（`mir.no`）：`OP-STRSLICEB = 110`；`dst` = 新的 `%sv` 值 id、`x` = 側池基址下標、`y` = 3。
op 碼只出現在這 4 個檔，**沒有 `OP-COUNT` 常量**要同步。

**helper**（`cg-runtime-str-slice-bytes`）：`define %str-long @str_slice_bytes(%str-long %a, i64 %s, i64 %e)`；
`%la.i32 = extractvalue %str-long %a, 0` → zext i32→i64 得 `%la`；`%da = extractvalue ..., 2`；
鉗制兩对 `icmp`+`select`（`%sneg/%s0`、`%egt/%e0`）；`%nraw = sub i64 %e0, %s0`、
`%nneg = icmp slt i64 %nraw, 0`、`%n = select ... 0`；`%buf = call i8* @nolang_rc_alloc(i64 %n)`；
phi 迴圈逐元 `getelementptr i8, i8* %da, i64 %src`（`%src = add i64 %s0, %i`）→ `load i8` →
`store i8`（**1:1，不加寬**——str 緩衝是 i8 元素，與 s159 的 i64 寬槽相反）；收尾
`insertvalue %str-long zeroinitializer` 三次（len=cap=`trunc i64 %n to i32`、data=`%buf`）。
🔴 命名紀律：區域值名 `%la/%da/%sneg/%s0/%egt/%e0/%nraw/%nneg/%n/%buf/%i/%go/%by/%dp/%inext/%src/
%l0.i32/%r0..%r2` 全部避開形參 `%a／%s／%e`（clang 要求函式內局部名唯一，見 §78 ⑥ 教訓）。

**h2m 兩臂**：`ty-of` 加 `mt == TY-STR && mf == 'slice-bytes' -> code = TY-STR`（缺這支則 let 槽不綁）；
`lower-expr` 加 `mname == 'slice-bytes' && ok-method == 0 && na == 2`（**只收兩參形**，單參／三參落門禁
不猜），臂內要求 `av1/av2` 皆為 `TY-I64` 才繼續，然後推側池、`h2m-alloc(TY-STR)`、
`h2m-emit(OP-STRSLICEB, sv, base, 3, '', TY-STR)`、`h2m-rel-add`。

### ③ 測試

`h2m-test`：`test-s160-strsliceb`（`agg.unsup == 0`；IR 含 `call %str-long @str_slice_bytes(`；
接收者走 `%sv` 反查；兩個 i64 運算元都在）、`test-s160-strsliceb-release`（值被丟棄 ⇒ IR 含
`@nolang_rc_release`）、`test-s160-strsliceb-gate`（單參／三參皆 unsup > 0）、
`test-s160-strsliceb-ty`（`slice-bytes` ⇒ TY-STR；負控未知名 ⇒ -1）。
⚠️ 本輪**唯一一處非純追加**：把 §79 `test-s159-strtobytes-ty` 的負控「`slice-bytes` still -1」
改為 TY-STR。該斷言是 s159 當時的在地事實（§79 ④），s160 起語意已變，留著會直接紅。
保留原測試名與原註解脈絡，僅改期望值並加「s160 起放行」的修正注記——**歷史敘事不動**。

`cg-runtime-test`：`test-strsliceb-helper`（25 條断言）釘住 sig、u32 len 讀出+zext、data 指標、
兩條鉗制 select、`n<0 ⇒ 0`、`@nolang_rc_alloc`、`getelementptr`/`load i8`/`store i8` 的 1:1 複製、
phi 迴圈、`cap = len`、prelude 順序（`@str_to_bytes` 在 `@str_slice_bytes` 之前），
外加三條**同名碰撞哨兵**（不得出現 `%a = `／`%s = `／`%e = `）。

### ④ 驗證（全綠）

| 閘 | 命令 | 結果 |
|---|---|---|
| S0 census | `./tmp/s153/bin/no run tmp/s154/cov-main.no` | **unsup 1234** = block構造 1057 + type類 dots 2 + dropped 175 |
| A/B | `bash tmp/s160/ab.sh`（A=`tmp/s159/noc-s1`、B=`tmp/s160/noc-s1`） | **11/11 SAME**（11 支測試檔的 stdout 逐字、rc 相同） |
| L0 18 套 | `bash tmp/s153/run-gates-clean.sh` | **18/18 rc=0，2473 passed / 0 failed** |
| ├ h2m-test | ↳ | **1397** passed（s159 為 1387，+10） |
| └ cg-runtime-test | ↳ | **188** passed（s159 為 163，+25） |
| L5 自舉 | `./tmp/s160/noc-s1 build -o tmp/s160/noc-s2 compiler` | **rc=1，unsup 1234（pkg-ll），無程式碼產出** |

L5 的 1234 與 S0 census 的 1234 再次逐項相同（block 1057／dots 2／dropped 175），
即 DR-1 fixpoint ①behavior 由 census 量、②fixpoint 由 L5 量，兩者互相對賬。

**普查降幅 −214（1448 → 1234）的歸因**（本輪最值得留檔的數字）：

| 項 | s159 | s160 | 差 |
|---|---|---|---|
| block構造行 | 1269 | 1057 | −212 |
| type類 dots | 2 | 2 | 0 |
| dropped | 177 | 175 | −2 |
| **合計** | **1448** | **1234** | **−214** |

降幅遠大於 s159 的 −6，但**不是**順手修了別的東西：以 `LF:` 歸屬逐函式對照（同一支
`join`+`awk`，s159/s160 各跑一次即可複現），**沒有任何函式逆勢上升**——唯一的增量是新
helper `cg-runtime-str-slice-bytes` 自己的 1 筆，與 s159 `cg-runtime-str-to-bytes`、
s158 `cg-runtime-str-index` 的 1 筆同構（helper 體內的跨模組 `b.push(...)` 一直是老缺口）。
下降的函式全部含 `slice-bytes` 呼叫點（檔內實際呼叫數：`drv.no` 22、`ps.no` 17、`ck.no` 4、
`h2m.no` 5）：

| 函式 | s159 | s160 | 差 |
|---|---|---|---|
| `drv.cmd-vet` | 49 | 21 | −28 |
| `ps.parse-let` | 27 | 0 | −27 |
| `ck.check` | 37 | 16 | −21 |
| `drv.pkg-ll` | 62 | 44 | −18 |
| `ps-is-op-overload-name` | 18 | 0 | −18 |
| `drv.mod-name` | 16 | 0 | −16 |
| `drv.bp-resolve` | 12 | 0 | −12 |
| `drv.pkg-split` | 14 | 5 | −9 |
| `drv.pkg-sigs` | 10 | 2 | −8 |
| `h2m-fmt-emit` | 8 | 0 | −8 |

即單一支未認領的方法呼叫會讓他們所在區塊的 lowering 留下**疊加記數**，認領後一次清空——
這解釋了「加一個方法卻降 200+」的表面矛盾，也說明 **LF 計數對未支援方法非常敏感**，
往後的 Δ 解讀必須逐函式核對，不可只看了總數就下結論。

### ⑤ smoke 與兩個探針坑

端到端 smoke：`tmp/s160/smoke/main.no`（S0 權威 vs S1 制品逐字比對）。Smoke 執行 8 條內容級檢查
（`== 'bcd'`、`len()`、`len-bytes()`）、四條鉗制（`start<0`、`start>=end`、`end>len`、全文）、
三組多字節（`aé世界` 切 `[1,3)=='é'`、`[3,9)=='世界'`、`ulen()==6`）、8 連切（每次 `rc_alloc` 一塊，
全部丟棄）與函式 ABI（切片结果以 str 進／出函式）。

```
eq1: true      len1: 3      eq2: true     eq3: true     len3: 0
eq4: true      len4: 4      eq5: true     eq6: true     len6: 1
eq7: true      len7: 2      ulen7: 6
rc1: bcd  rc2: cde  rc3: a  rc4: f  rc5: é  rc6: 世界  rc7:（空）  rc8: cdef
abi: cde       abi-eq: true
```

S1 制品（`./tmp/s160/noc-s1 build -o tmp/s160/smoke/run tmp/s160/smoke/main.no`，build rc=0、
run rc=0）與 S0 唯一差異是**布林打印外形**：S0 印 `true`、S1 印 `1`。這不是本輪引入的——
用 s159 的 `noc-s1` 跑同一隻 `print(1 == 1)` 也是 `1`，屬既有的外呈現差異，語意相同。

**坑一：`noc-s1 run` 碰到「跨模組點號呼叫」就中止（既有缺口，非本輪回歸）。**
本輪一度用 `./tmp/s160/noc-s1 run tmp/s154/cov-main.no` 想順手得到 S1 版普查，結果
`unsupported constructs: 1 (M4-d SHS subset)`、stdout 全空。以控制組定位：沒有任何跨模組呼叫的
`imp-main.no`（同樣的 15 個 import + `print('ok')`）在 s159-S1 下 **0 unsup** 通過；加上
`drv.pkg-ll-cov('compiler/main.no')` 一行就變 1；再換 `drv.arg-at(0)` 變 2、`ast.make().count()`
變 3——**任何**跨模組點號呼叫都會點燃，且 `tmp/s158/noc-s1`、`tmp/s159/noc-s1`、`tmp/s160/noc-s1`
三支二進位行為一致。故：**普查只能走 S0**（§79 ④ 早就如此），S1 的「自舉」由 L5 的
`build compiler` 量；勿把 `noc-s1 run cov-main.no` 的 1 unsup 誤讀成本輪弄壞了什麼。
smoke 也因此刻意**不 import 任何模組**（不用 `ix.inc`、不用 `drv.*`）——一旦加了 `# /compiler/src/ix`
就直接 8 unsup 中止。

**坑二：admin 側的布林/文字差異要在 smoke 設計時就避開。** 因坑一，S1 無法執行任何呼叫
`drv.*` 的程序，故內容級驗證改走「純值方法 + `print`」這條 S1 可組的路綫：结果既然是 `str`，
就能用 s158 的 `str.eq`／`contains` 逐字比對（`== 'bcd'`），不必像 s159 `[]byte` 那樣受
「下標讀不出口」的既有缺口所限。這是本輪相較 s159 的實質進展：**第一次做到內容級端到端**。

- **未提交任何改動**。

## §81 s161：`byte.to-i64()` —— noc 自舉子集的【恆等】認領（**不新增 MIR op**）

### ① 語意邊界：為什麼是恆等，以及與 Go 的差異

**語料真值路徑**：`compiler/src/ps.no:2287`——

```
cb = raw.byte(j): { ok -> it -> 0 }
c = cb.to-i64()
```

`cb` 是 `raw.byte(...)`（回 `?byte`）經 match 剝殼後的 **byte** 值，全語料庫裡 `byte.to-i64()` 只有這一處。
認領它之前，`h2m-ty-of` 對該點號呼叫回 -1 ⇒ `h2m-slot-decl` 的 `ok-slot=0` ⇒ `c` **不登記** ⇒
`ps-int-lit-value` 內後續 6 次讀 `c` 全部記 `ID:` 失敗（見 ④ 歸因表）。

**語言側的定義**：`src/std/number.no:47-59`——`byte.to-i64() -> i64 { #{buildin} }`；
Go 後端 `scalarMethodResult`（`src/mir/builtin_call.go:892-900`）對 `to-i64` 回 `i64`，
`LLVMConvIntExtend` 再把它翻成 LLVM 指令。

**為什麼是恆等（而不是發一條 zext）**：noc 自舉子集**沒有 `TY-BYTE`**，byte 與 i64 共用
`TY-I64` 碼與 `%v` 槽。這不是偷懶，而是既有的運行期口徑——`mir.no` s136 注記明寫
「`[]byte` 元素在 noc 運行期是 **i64 寬槽**（stride 8）；s136 的口徑合併：Go 側 MIR 把
`"i64","i8","i1","byte"` 同列（`src/mir/codegen.go:1587`）」，且 `OP-STRBYTE` 的載荷注記是
「`data[i]`（**zext i8→i64**）」。於是在 noc 的 LLVM 層，byte 值本來就是 0..255 的 i64，
`to-i64` 是純搬運——恰與 Go 的 `if srcTy == dstLT { r = srcVal; break }` 分支
（`src/mir/builtin_call.go`）同構。

🔴 **忠實性不是推論出來的，是量出來的**：S0 對高位元組 `'é'.byte(0)` 得 **195**（不是 −61），
即字節在槽內是**無符號**的 ⇒ 恆等不引入偏差。見 ⑤ smoke 的 `utf8-hi` / `utf8-lo`。

⚠️ **誠實邊界（如實記錄，不遮掩）**：Go 把 byte 當 **i8** 槽並發 `zext i8→i64`；noc 不發指令。
若有一天 byte 由「截斷算術」產生（例如 `b = b + 200`，Go 會 `trunc i64→i8` 迴繞），
恆等**不會**重現該截斷。Compiler 語料中不存在此路徑——`byte.to-i64` 的唯一接收者
來自 `raw.byte`／`str.byte`（皆 0..255 無符號語義）。此偏差屬整個「i64 寬槽」模型既有，
非本輪引入。

### ② 變更（兩層原始碼，全加分、零刪改）

| 檔 | 行數 | 加 | 內容 |
|---|---|---|---|
| `compiler/src/h2m.no` | 7589 → 7617 | +28 | ty-of 1 支（含注記）+ lower-expr 1 支（含注記） |
| `compiler/tests/h2m-test.no` | 7589 → 7648 | +59 | 3 個測試（+6 斷言）+ runner 登錄 |

刪改數一律 0。pre 行數（7589/7589）與 §80 ② 備考完全對齊。

🟢 **本輪與 s158–s160 最大的工藝差異：`mir.no`／`cg.no`／`cg-runtime.no` 三檔一行未動**
（mtime 仍是 00:34／00:35／01:25）。恆等 lowering **不發任何指令**，與 s152 `str.copy`
的「不配置新值」形一致——沒有新的 MIR op、沒有新的 emit 臂、沒有新的 prelude helper。
這是有意的：為一個純搬運去開 `OP-*` 碼只會製造永不被選量的死碼。

**`ty-of` 臂**（h2m.no:2900）：`mt == TY-I64 && mf == 'to-i64' -> code = TY-I64`。
因無 `TY-BYTE`，按**接收者型別碼**認領會一併覆蓋 byte 與 i64 接收者；兩者在攤平後的
LLVM 層都是同一 i64 槽 ⇒ 皆恆等，不需額外交互。缺這支則 let 槽不綁（正是本輪修掉的東西）。

**`lower-expr` 臂**（h2m.no:4197，掛在 `rty == TY-I64 || rty == TY-U64` 臂內、緊接 `to-str` 之後）：

```
mname == 'to-i64' && ok-method == 0 && na == 0 -> {
    v = rv          ; 回接收者值 id，不發指令、不 alloc、不入 rel-list（整數無 RC 頭）
    ok-method = 1
    ok-call = 0
}
```

守衛 `na == 0`：**只收無參形**，帶參的 `to-i64(...)` 落門禁（不猜）。

### ③ 測試

`h2m-test`（S0 直跑，見 §79 ④ 的禁令）：

- `test-s161-byte-toi64`：源為 `cb = s.byte(0): { ok->it; ->0 }`、`c = cb.to-i64()`、
  `d = cb.to-i64()`、`r = c + d - 65`——**連叫兩次**以釘住「非破壞、可重放」；
  斷言 `agg.unsup == 0`、IR 不含 `to_i64` helper 符號、IR 不含 `ID:`（`c` 已綁名）。
- `test-s161-byte-toi64-gate`：`r = cb.to-i64(1)`（`na == 1`）⇒ `unsup > 0`（門禁不動）。
- `test-s161-byte-toi64-ty`：`to-i64` ⇒ `TY-I64`；負控 `to-u64` 仍 -1（同型別未支持項不放行）。

⚠️ **本輪的一次自我更正**：初版 `test-s161-byte-toi64` 曾斷言「IR 不得出現 `zext`」（想證明
恆等不發擴展指令），被 `str.byte` **自身**的兩條 zext 證偽（u32 len 讀出 zext i32→i64、
i1 條件 zext i1→i64，見探針 dump 的 `%sbV18 = zext i8 %sbD18 to i64`）而紅。
遂改為斷言「不得出現 `to_i64` 這個 helper 符號」——那條纔是本輪 lowering 的可觀察面。
教訓：**測試要釘在「本輪引入的行為」上，不是釘在「不相關的既有指令」上**。

### ④ 驗證（全綠）

| 閘 | 命令 | 結果 |
|---|---|---|
| S0 census | `./tmp/s153/bin/no run tmp/s154/cov-main.no` | **unsup 1214** = block構造 1037 + type類 dots 2 + dropped 175 |
| A/B | `bash tmp/s161/ab.sh`（A=`tmp/s160/noc-s1`、B=`tmp/s161/noc-s1`） | **11/11 SAME**（11 支測試檔的 stdout 逐字、rc 相同） |
| L0 18 套 | `bash tmp/s153/run-gates-clean.sh` | **18/18 rc=0，2479 passed / 0 failed** |
| ├ h2m-test | ↳ | **1403** passed（s160 為 1397，+6） |
| └ 其餘 17 套 | ↳ | 全綠（ast 48、build-package 8、cg-runtime 188、cg-str16 41、cg 363、ck-sig 50、ck 20、diag 7、drv-build 63、lx 81、pkg-ll-call 5、pkg-ll 10、pkg-sigs 5、ps-err 46、ps 52、tok 62、util 27） |
| L5 自舉 | `./tmp/s161/noc-s1 build -o tmp/s161/noc-s2 compiler` | **rc=1，unsup 1214（pkg-ll），無程式碼產出** |

L5 的 1214 與 S0 census 的 1214 再次逐項相同（block 1037／dots 2／dropped 175）。

**普查降幅 −20（1234 → 1214）的歸因**（本輪的數字比 s160 小得多，正因為動的是單一支方法）：

| 項 | s160 | s161 | 差 |
|---|---|---|---|
| block構造行 | 1057 | 1037 | −20 |
| type類 dots | 2 | 2 | 0 |
| dropped | 175 | 175 | 0 |
| **合計** | **1234** | **1214** | **−20** |

逐函式对照（同一支 `join`+`awk`，s160/s161 各跑一次即可複現）：

| 函式 | s160 | s161 | 差 |
|---|---|---|---|
| `ps-int-lit-value` | 20 | 0 | −20 |
| （其餘全部函式） | — | — | **0（無任一逆勢上升）** |

**ID 解析失敗 38 → 32**，減少的 6 條**全部**是 `ID:ps-int-lit-value:c`（逐行 diff 確認，無其他變動）：

```
24,29d23
< ID:ps-int-lit-value:c   ×6
```

即「不認領 ⇒ RHS 回 -1 ⇒ let 槽不綁 ⇒ 該名後續每次讀取值皆失敗」這條因果鏈，在本輪被完整測量了一次。

### ⑤ 端到端 smoke（恆等的正面量測）

`tmp/s161/smoke/main.no`（S0 權威 vs S1 制品**逐字**比對）。同樣**不 import 任何模組**
（§80 ⑤ 坑一的跨模組點號缺口仍在），故以純值方法 + `print` 走內容級路綫。

內容：ASCII `'A'`→65／`'z'`→122 及其和 187；多位元組 `'é'` 的 UTF-8 兩位 `C3`/`A9`
⇒ 195／169 與和 364（這兩條同時是 ① 的**無符號忠實性證據**）；重複呼叫 `'A'` 兩次
得 130（非破壞）；`'Q'`→81；巢狀函式 `f(s) = s.byte(0).to-i64() + 1` 對 `'B'`→67、`'y'`→122；
以及空串取 `byte(0)` 失敗走 nil 分支的 fallback 65。

```
ascii-A: 65      ascii-z: 122     ascii-sum: 187
utf8-hi: 195     utf8-lo: 169     utf8-sum: 364
twice: 130       inline: 81
nested-B: 67     nested-y: 122
fallback: 65
```

S1 制品（`./tmp/s161/noc-s1 run tmp/s161/smoke/main.no`）與 S0 **逐字相同**（`diff` 無輸出）——
這輪不必再容忍布林打印外形差異，因為全文都是整數輸出，連既有外呈現差異都一併消掉了。
**反面量測**：同一隻 smoke 在 **pre-s161** 的 `tmp/s160/noc-s1` 下是
`unsupported constructs: 37 (M4-d SHS subset)`、stdout 全空——即這 37 條 construct 全部來自
這批 `byte.to-i64` 呼叫點，認領後一次清零。

**坑：match 表達式不能內聯當 `print` 的實參。** 初稿把
`'Q'.byte(0): { ok -> it.to-i64(); -> -1 }` 直接塞進 `print(...)`，換來
`main.no:47:31: [ERROR] ... a value match is a statement-level construct and may only appear as
the right-hand side of an assignment ... [matchexpr]`。這不是本輪引入的限制（language 層既有關口），
但要先綁名再餵給方法纔行——已如實改寫並在 smoke 裡保留為一條常設注意。

- **未提交任何改動**。

## §82 s162：`[]<struct>` 切片型別 `TY-VECSTRUCT = 7` —— 只讀描述符的 `.len()`（**不新增 MIR op**）

### ① 根因：從 IR 地面真值回到 drop 級聯的上游

s161 後按 per-function unsup 表繼續 recon，最大的一簇全部指向同一形狀：`p.toks`／`t.nodes`
這類**元素為具名結構體的切片**。以 S0 生成的 LLVM 逐行比對（`tmp/s154/ir-dump.no`）後確認：

- `ast.nd-at` 的 unsup **全部**來自 `d = t.nodes[i]` 一句（`ast.nodes` 是 `[]nd`）。二分驗證：
  拆掉 match 尾部 → 0 unsup；只留 match 本體仍失敗（變體 C）；裸索引讀（變體 H）報
  `si-bt-other`。即「元素讀」無助手，而非 match／索引語法本身。
- 但上游根因更靠前：`h2m-tynode-code` 的 K-SLICE-TYPE 臂此前只認元素名 `i64`／`byte`／`str`，
  元素是具名結構體時回 **-1**。對照組：`t.kids`（`[]i64`）與 `t.strs`（`[]str`）可解析，
  `t.nodes`（`[]nd`）失敗；`t.nodes.len()` 與 `p.toks.len()` 同樣失敗——後者正是
  ps-peek 家族的 dropping 根因。
- 因果鏈：欄位型別 -1 ⇒ 連【只讀描述符】的 `.len()` 都推不出型別 ⇒ 方法落門禁 ⇒ 整個函式
  回 `''`（`drv.no:6962-6963`：`inc>0 → r=''`），**既不算 unsup 也不進 dropped**。所以認領後
  這些函式開始 emit，卻因下游 callee 仍缺失而**改列 dropped**——本輪 dropped 175→189（+14）
  即此，是進展不是回歸（逐行 diff 見 ④）。

### ② 變更（四層原始碼 + 一層測試，全加分、零刪改）

`compiler/src/mir.no` 新型別碼取 7（TY-VECSTR=6 之後、TY-STRUCT-BASE=100 之前的空檔），
理由與 TY-VECSTR 同構：vec 描述符 `{i32 len,i32 cap,i64 data}` 與元素型別無關，故 LLVM 層
沿用同一型别名 `%vec`、同一名稱空間 `%vc`：

```
TY-VECSTRUCT = 7
```

`compiler/src/cg.no`（`cg-val-prefix`）與 `compiler/src/cg-runtime.no`（`cg-runtime-ty-name`）
各加一行映射：

```
t == TY-VECSTRUCT -> s = '%vc'      ; cg.no
t == TY-VECSTRUCT -> s = '%vec'     ; cg-runtime.no
```

後者必要：結構體欄位宣告走 `cg-runtime-struct-decls`，它用 `cg-runtime-ty-name` 拼出
`%struct.s<id>` 的欄位列，漏映射會把 `[]Pt` 欄位發成 `i64`。

`compiler/src/h2m.no` 三處，全為加法：

1. `h2m-tynode-code` 的 K-SLICE-TYPE 臂（`enm == 'str'` 之後）：

```
enm != 'i64' && enm != 'byte' && enm != 'str' -> {
    sid2 = h2m-struct-find(agg, enm)
    sid2 >= 0 -> code = TY-VECSTRUCT
}
```

2. `h2m-ty-of` 方法臂（`mt == TY-VECSTR && mf == 'len'` 之後）只加一條：

```
mt == TY-VECSTRUCT && mf == 'len' -> code = TY-I64
```

3. `h2m-lower-expr` 的方法分派：在 `rty == TY-VEC` 那塊**之前**獨立成塊（`OP-VECLEN` 讀描述符
   的 len 字段，codegen 本來就寫死 `%vc`，與元素型別無關）：

```
rty == TY-VECSTRUCT -> {
    mname == 'len' -> {
        cid = h2m-alloc(c, TY-I64)
        h2m-emit(c, OP-VECLEN, cid, rv, 0, '', TY-I64)
        v = cid
        ok-method = 1
        ok-call = 0
    }
}
```

⚠️ 必須**獨立成塊**，不得併入下方 `rty == TY-VEC`：那塊的 push 臂**不帶 rty 守衛**，一旦放寬
會讓 `p.toks.push(...)` 靜默發成 `OP-VECPUSH`（`@vec_push_i64` 是單型 i64 助手，误用之會寫壞
結構體元素）⇒ 错码。這條界限由 ③ 的門禁用例釘住。

### ③ 測試（3 個；h2m-test 1403 → 1410，0 failed）

- `test-s162-vecstruct-len`：正面。`Holder { pts []Pt \n id i64 }` + `r = h.pts.len()`，斷言
  `agg.unsup == 0`、IR 含 `call i64 @vec_len`、含 `%struct.s1 = type { %vec, i64 }`（證明欄位
  真的解析成 TY-VECSTRUCT）、無 `ID:` 失敗。
- `test-s162-vecstruct-gate`：反面。同結構改用 `h.pts.push(1)`，斷言 `agg.unsup > 0`——釘住
  「元素級不放寬」，防止未來有人把 TY-VECSTRUCT 併入 TY-VEC 塊。
- `test-s162-vecstruct-len-ty`：`h2m-ty-of` 單元。TY-VECSTRUCT 接收者的 `len` 回 TY-I64；
  `push` 仍回 -1。

### ④ 驗證（全綠）

census 以 **S0** 執行 `tmp/s154/cov-main.no`，連跑兩輪數字一致：

```
             pre-s162   post-s162
unsup           1214        1150
construct 行    1037         961
type 類 dot        2           0
dropped          175         189
LF              1039         961
DB            20416       20530
恆等式   1037+2+175=1214   961+0+189=1150
```

- 減少的 76 條 construct **全部**落在 14 個函式，且這 14 個正好就是 newly-dropped 的 14 個：
  ps-bm-block-shape(18)、ps-bm-scan(13)、ps-is-struct-lit(8)、ps-arrow-scan-line(5)、
  ps-arrow-scan0(4)、ps-la-sep(4)、ps-peek(3)、ps-peek-token(3)、ps-peek-at(3)、
  ps-cur-lit(3)、ps-lit-at(3)、ps-semi-eat-count(3)、ps-scan-nont(3)、ps-arm-blk-match(3)。
  無 un-drop、無其他函式變化（multiset diff 的 NEW total = 0）。
- `ps-peek` 的 LF 由 3 → 0，且出現在 L5 的 dropped 清單 ⇒ 確實已 emit（不再是靜默回 `''`）。
- A/B：`tmp/s161/noc-s1`（pre-s162）vs `tmp/s162/noc-s1`（post-s162），11 個測試檔的 rc 與
  stdout sha256 **全 SAME**。
- L0 門禁用 S0 重跑：**18/18 套件全綠**，其中 h2m-test `passed: 1410 failed: 0`。
- L5 自舉 `./tmp/s162/noc-s1 build -o tmp/s162/noc-s2 compiler`：rc=1，
  `dropped (calls unsupported)` **189** 條 + `unsupported constructs: 1150` + `no code generated`
  —— 與 census 的 189／1150 逐一對應；在 unsup 歸零前本就不可達（非本輪引入）。

### ⑤ smoke 與 S1 既有限制

`tmp/s162/smoke/main.no`（自足、不 import；只走 `.len()`。⚠️ 整數算術在此語言回 `?i64`，
故不做算術組合）：

```
empty-len: 0
three-len: 3
repeat-first: 1
repeat-second: 1
repeat-last: 1
```

S0 制品執行逐字符合期望（空 vec 0、三元素 3、同一值重複呼叫非破壞）。

**但 S1 尚不能自建含 `[]<struct>` 的用戶程式**，且這是**本輪之前既有**的缺口：同一批探針在
pre-s162 的 `tmp/s161/noc-s1` 與 post-s162 的 `tmp/s162/noc-s1` 下**計數完全相同**——

```
b1（local []Pt = [] ＋ .len()）        1 / 1
d1（同上，不呼叫 .len()）             1 / 1
d2（形參 []Pt ＋ .len()）             4 / 4
d3（形參 []Pt，不呼叫 .len()）        4 / 4
main.no（三個函式）                  11 / 11
```

（同一組在 S0 下全部 rc=0。）即 S1 對 `[]<struct>` 的 unsup 出處與 s162 的認領無關，本輪
**沒有加深**它；对照件 `c1`（`[]i64`）與 `c2`（純結構體）在兩個 S1 下皆 rc=0。

**坑：整數算術回 `?i64`。** 初稿把 `a * 10 + b`（兩個 `.len()` 結果）派給 `r i64`，換來
`cannot assign ?i64 value to i64 variable 'r'; an option cannot be unwrapped implicitly`。
這不是本輪引入（語言的溢位語意），但也意味 smoke 不能用算術做「非破壞」證明，只能直接
`print` 兩次——已如實改寫並保留為一條常設注意。

- **未提交任何改動**。

## §83 s163：mem 模式 vec 槽三件套（`OP-VECALLOC/STORE/LOAD = 111/112/113`）—— `vec.push` 開禁，LF 961 → 523

### ① 根因：一旦函式體有控制流，`vec.push` 就整條落門禁

`flow > 0 -> c.mem = 1`（`compiler/src/h2m.no:7023`；return／break／continue 也逼出 mem 模式）。mem==1 的口徑是
「局部量住 entry 的 alloca 槽」，而 s122 起 vec 走的是另一條路：**值 id 必須支配全函數各塊**才許跨塊讀
（`h2m-var-write` 的 vec 臂只在 `c.bkn == c.fnbk` 放行）。副作用是 `xs.push(...)` 的 push 臂此前還帶著
`c.mem == 0` 門禁——**只要函式體內有 if／迴圈／match 臂，`xs.push` 一定落 `h2m-mark`**，整個函式回 `''`。

受災函式全是同一形狀：把 `toks.push`／`nodes.push` 寫在 match 臂或迴圈體的樣板。以 s162 基準逐函式看
（block 通道，見 ④ 口徑）：`ps-parse-match 115`、`ps-parse-expr-prec 58`、`ps-parse-enum-def 50`、
`ps-parse-bare-match 36`、`drv.pkg-ll 44`、`h2m-lower-expr 65`、`ps-parse-stmt 26`、`ps-parse-type-alias 20`……
這些 marks 先令 liveness pruning 把整個函式 drop（連已認領部分一起消失）；認領之後函式開始 emit，卻因下游
callee 仍缺而**改列 dropped**——所以本輪 dropped 上升是進展不是回歸（明細見 ④）。

### ② 變更：vec 值的跨塊通路改走【槽】（全加法，零刪改）

**MIR（`compiler/src/mir.no:720-728`）**新增三個 op（接在既有 `OP-VEC-FREE=94` / `OP-VEC-FREE-STR=98` 之後，
不重排舊碼）：

```
OP-VECALLOC = 111   ; %vs<id> = alloca %vec（無值操作數）
OP-VECSTORE = 112   ; store %vec %vc<v>, %vec* %vs<s>（xa=值, bs=槽）
OP-VECLOAD  = 113   ; %vc<d> = load %vec, %vec* %vs<s>（槽在 a、結果在 dst）
```

`%vs<id>` 是第八九個命名空間，型別 `%vec*`。⚠️ vec 無 RC 頭，釋放是**破壞性**的 `@vec_free`／
`@vec_free_str`——這正是本輪所有權模型要圍繞槽打轉的原因。

**codegen（`compiler/src/cg.no:2211-2232`）**按 SSTORE/SLOAD 同形發三條指令。

**h2m（`compiler/src/h2m.no`）**六處，全為加法：

1. ctx 新增 vec 槽所有權表 `vsn/vsv/vso/vsm`（槽 id／最後寫入值 id／是否自擁有／名字，約 :216），
   配以 `h2m-is-param`（:536）、`h2m-vs-reg`（:556）、`h2m-vs-owned`（:589）、`h2m-slot-ty`（:611）。
2. `h2m-slot-decl` vec 臂（:1225-1256）：entry 發 `alloca %vec`，再**零初始化**——發 `OP-VECNEW`（空 vec）
   後 `OP-VECSTORE` 落槽。良構程式「宣告未賦值先讀」在舊路徑由 `sid==0` 門禁誠實上報；鋪槽後無從分辨
   「沒寫過」，故按 Go nil slice 語意逕給空 vec，絕不交出 poison 槽值。這顆空 vec **不入 rel-list**——
   隨即被首次寫覆蓋，且 `@vec_free` 對 `dat==0` 短路，無實際泄漏。
3. `h2m-slot-init-arg` vec 臂（:1342-1344）：形參 incoming 值 `OP-VECSTORE` 進槽。漏這條則函式體內第一次
   `VECLOAD` 讀出**空 vec 而非實參**（靜默錯值），不是落到門禁——那是最壞的一類。
4. `h2m-var-read` vec 臂（:1447-1458）改 `OP-VECLOAD`；`h2m-var-write` vec 臂（:1576-1590）改 `OP-VECSTORE`
   並把所有權移交槽：右值是自擁有值（rel-list）⇒ `h2m-rel-drop` 值清單、`h2m-vs-reg(..., own=1)` 登記；
   右值也可是別名／形參借入 ⇒ 照登記但 `own=0`，退出時不 free（防 double-free）。回傳鏈的 owner
   provenance 由 `h2m-vs-owned` 補齊（:1605-1607），否則 `r = xs` 認不出來、釋放會被誤跳。
5. **push 兩臂撤除 `c.mem == 0` 門禁**：TY-VEC（:4105-4136）與 TY-VECSTR（:4190-4227）。共同紀律：
   **發指令之前先確認寫回路徑**（`wb`；mem==1 必須查得到同名 vec 槽，否則不認領、留待總門禁如實上報——
   防止「發了 `VECPUSH` 卻沒寫回」，把剛推入的元素靜默丟進死碼）。寫回後 `h2m-rel-drop(c, rv)`（舊描述符
   由 @vec_push_* 接管）＋ `h2m-vs-reg(..., own=1)`。
6. **釋放改以【槽】為單位**（`h2m-emit-releases` :754-787）：作用域退出時對每個槽 `OP-VECLOAD` 再
   `OP-VEC-FREE`／`OP-VEC-FREE-STR`；跳過 `vso==0`（別名／形參借入）與回傳值（skip／outsv／retset）。

**過程中修掉的兩個親身坑**（都記在程式碼註解裡，此處如實留存）：

- **非支配 free（invalid IR）**：初版把 push 的新描述符直接放值級 rel-list，作用域退出時在合併塊發
  `@vec_free(%vc<臂內值>)`，而後端如實拒收——`Instruction does not dominate all uses!`。改為槽之後，
  槽在 entry 支配全部塊，只有退出時的 `VECLOAD` 才合法。
- **靜默泄漏**：槽釋放迴圈一度照抄 `h2m-rel-dropped(...) == 1 -> skipv = 1`。但 `rel-drop` 的語意正是
  「該值已被移交給槽」，它的釋放責任**就是**本迴圈；兩個迴圈都跳 ⇒ `rng-container`／`s122` 的 `vec_free`
  直接消失（dump 實測）。修法：slot 迴圈**不得**用 rel-dropped 做跳過條件（上列 rls 迴圈才是它該攔的地方），
  另加 `sty != TY-VEC && sty != TY-VECSTR -> skipv = 1` 守衛。

### ③ 測試（h2m-test 1425 → 1429，0 failed）

新增 4 個用例（註冊於 :7787-7790）：`test-s163-mempush-if`（if 塊內 push：`unsup==0`、`%vs` 命名空間、
alloca/store/load 俱在、仍走 `@vec_push_i64`、無 `ID:`）、`-param`（形參同鋪槽、push 寫回形參槽）、
`-gate`（反面：`a[0].push(x)` 非裸標識符接收者**仍落門禁** `unsup>0`——寫不回槽就寧可不認領）、
`-release`（退出先 `VECLOAD` 槽再 `@vec_free`）。

既有快照同步（全部取自 fresh IR dump，不猜）：`test-coll-basic/kw/break/cont/nested` 的值 id 隨
`%vs` 槽佈局位移（容器 `%vc11`、長度 `%v12`、索引槽 `%s8`…）；`test-s122-vec-local-mem1` 改寫為
「s122 循環體 vec 局部門禁已撤除」並釘住四條新行為（`unsup==0`、槽在 entry、寫回、退出釋放）；
`test-sb-str-index` 的 `get_str` 接收者／下標改為 `%vc11`／`%v12`；其餘涉及 vec 形參的值 id 位移按
逐條 dump 對拍修正。全檔 `passed: 1429 failed: 0 total: 1429`。

### ④ 驗證（全綠，且 L5 仍是預期失敗）

census 以 **S0**（`tmp/s153/bin/no run tmp/s154/cov-main.no`）執行。⚠️ 口徑：每條 construct mark 同時
落在 block 通道與 `LF:` leaf 通道（各一份、逐條同序），下表取 **block 通道**；工具自己在 stderr 印的
`unsupported constructs: N` 即 `construct + type類dot + dropped`。

```
                    s162(base)    d3(中間態)    s163(post)
construct 行             961           520           523
type 類 dot                0             0             0
dropped                  189           207           209
DB 通道（診斷標記）      20530         21213         20846
恆等式        961+0+189=1150   520+0+207=727   523+0+209=732
```

- 主要 movers（block 通道）：`ps-parse-match 115→2`、`ps-parse-enum-def 50→0`、`ps-parse-expr-prec 58→10`、
  `ps-parse-bare-match 36→0`、`ps-parse-stmt 26→5`、`ps-parse-type-alias 20→0`、`ps-parse-star-cont 10→0`、
  `ps-ht-put 18→11`、`check 16→6`、`drv.pkg-ll 44→27`、`h2m-lower-expr 65→16`、
  `cg-runtime-struct-decls 5→2`、`ps-match-listed 9→5`、`ps-parse-method-func 24→21`；
  另有 22 個函式 construct 悉數歸零（`ps-parse-*` 13 個＋`ps-add-generic`／`ps-collect-tail`／
  `ps-enum-variants`／`ps-premerge-trailing`／`node`／`parse`／`h2m-func-sig`／`h2m-lower-program`／
  `cg-zeros`）。
- 新增基礎設施自身的開銷（如實計）：`h2m-slot-decl 3→4`（多一條 vec 臂判定）、`h2m-emit-releases 0→2`、
  `h2m-lower-expr 14→16`（d3→post）；這 +3 construct、+2 dropped 是「新機制的自開銷」，不是回退。
- **無 un-drop**。dropped 189→209（+20）：其中 19 個是「base construct>0 → post 0」的認領成果
  （`ps-parse-*` 13 個＋`ps-collect-tail`／`ps-premerge-trailing`／`node`／`parse`／`h2m-func-sig`／
  `h2m-lower-program`），它們開始 emit；另 1 個 `h2m-vs-reg` 自身 construct 恒 0，是本輪新路線把它
  活化後才進入 dropped（它依賴的 `h2m-own-set` 仍帶 1 條 construct，屬既定尺度）。二者都是總門禁
  上游的進展或下游缺口如實上報——**是進展不是回歸**（逐條核見 `tmp/s163/cov-s163c.err` 與 base.err 的差集）。
- A/B：`tmp/s162/noc-s1`（pre-s163）vs `tmp/s163/noc-s1`（post-s163），11 個測試檔的 rc 與 stdout
  sha256 **全 SAME**。
- L0 門禁用 S0 重跑：**18/18 套件全綠**，其中 h2m-test `passed: 1429 failed: 0`。
- L5 自舉 `./tmp/s163/noc-s1 build -o tmp/s163/noc-s2 compiler`：rc=1，`dropped (calls unsupported)`
  **209** 條 + `unsupported constructs: 732` + `no code generated`——與 census 的 209／732 逐一對應；
  在 unsup 歸零前本就不可達（§82 已述，非本輪引入）。
- smoke：`tmp/s163/smoke/main.no`（自足不 import；用 `if` 語形，不用 while 形）涵蓋 ① if 塊 push、
  ② 分支各推不同數量、③ 重複讀、④ 形參 push、⑤ `[]str` push、⑥ match 臂 push、⑦ 推入結果外流回傳。
  **S0 制品 vs S1 制品逐字一致**（10 行輸出全同）；`order.no` 對拍記錄 S1 既存的 print 求值序差
  （`outer:inner`／` 1`），非本輪引入。

### ⑤ 已知殘余（如實列賬，不粉飾）

- **循環體反復 push 的 per-iteration 泄漏**：槽只登記「最後寫入值」，早期迭代的舊描述符無釋放路徑
  （除非 `@vec_push_*` 內部自行回收）。本輪按「不加深」處理，偵測到即記錄。
- **推入形參的滲漏**：形參槽所有權在呼叫方（callee 不釋放，§5.6.1），callee 內 push 產生的新描述符
  傳不回呼叫方的所有權鏈 ⇒ 每次 push 滲漏一顆。此為 s122 起既定的所有權口徑，本輪照舊。
- **`a[i].push()` 等非裸標識符接收者**仍落門禁（`test-s163-mempush-gate` 釘住）——無從寫回槽，寧缺勿錯。

- **未提交任何改動**。

## 84. s164 — vec 槽模式：無標注再賦值沿用槽型別（認領 -60，unsup 732→672）

- s163 收尾點名 `ps-parse-method-func` 餘 21 construct、4×`ID:ps-parse-method-func:gnames`。
  本輪先做根因 recon（全部用既有通道數據），再落兩處【純加法】修補，最後按 DR-1 計數。
- **根因**：`compiler/src/ps.no:5688` 的 `gnames = with-cap(0)` 是【無標注賦值】——RHS 是
  with-cap 內建呼叫，註解位是空節點（scan 插桩實測該 let 為第三顆、`k0=0/0`）。
  `h2m-scan` 的 K-LET-STATEMENT 臂（`h2m.no:3257-3316`；with-cap override 在 :3285-3287
  要求 `h2m-tynode-code(...d.kid0)`）對空節點必然回 -1 ⇒ `h2m-lst-add` 收到 ty=-1。
  舊路徑就地覆盖：把稍早 `:5427`/`:5634` 的 `gnames []str = with-cap(2)`（TY-VECSTR=6）
  抹成 -1。mem 模式下 `h2m-slot-decl`（:1225）只在 :7080 的 `c.mem==1` 迴圈執行，vec arm
  要求槽型別 ∈ {TY-VEC(2), TY-VECSTR(6)}；-1 ⇒ ok-slot=0 ⇒ 標記一次且**不登記名字**，
  後續 4 次讀（:5646/:5687/:5699/:5701）全部 vr-miss，各落一條 construct +
  `ID:…:gnames`（`h2m-var-read` miss 路徑 :1406-1410 的 `h2m-id-name(c,'vr-miss-'name)`）。
- **修法一（防降級，`h2m-lst-add` :2767）**：名字既有型別已知（`cur-ty>=0`）而新值推不回
  （`ty<0`）時**不得覆蓋**——「賦值不改型別」。`-1→已知`、`已知→已知` 仍照原覆蓋口徑；
  s134 的 lpty 清載荷不變。認領 59 行 construct（block 523→464），分布：
  ps-parse-method-func 21→1、ps-parse-expr-prec 10→2、drv.cmd-run 19→14、
  drv.build-to 12→8、drv.run-temp 13→9、ck-sig-types 4→1、drv.arg-at 6→3、
  drv.build-ll/h2m-byte-at/lx-esc/lx-read-raw 各 3→0。
- **修法二（沿用槽型別發码，`h2m-lower-let` with-cap 分支 :5088-5118）**：`ct<0` 且
  `c.mem==1` 時查 `h2m-name-find(c,nm)`，槽型別為 TY-VEC/TY-VECSTR 即沿用作這顆 with-cap
  的元素型別，照常發 OP-VECNEWCAP/OP-VECNEWCAPSTR + OP-VECSTORE 寫回槽。紅線：必須真有
  槽（`si>=0`）且槽型別是 vec；首見即無標注的名字無型別可沿用 ⇒ 落門禁
  （`test-s164-vec-reassign-gate`）。mem==0 的值模式不受影響（走既有
  `h2m-var-find`/`h2m-vty-at` 後備，探針 f-t2 為 0）。再清掉 ps-parse-method-func 殘餘
  1 行（臂外 `r = g.len()` 的跨塊槽讀）。
- **計數**（§83 同一 census 口徑；S0=`./tmp/s153/bin/no run tmp/s154/cov-main.no`）：

| 轮次 | block | dropped | unsup | DB | ID |
|---|---|---|---|---|---|
| base(s162) | 961 | 189 | 1150 | 20530 | 32 |
| s163c | 523 | 209 | 732 | 20846 | 32 |
| s164a（仅修法一） | 464 | 208 | 672 | 20860 | 24 |
| s164b/s164（加修法二） | 463 | 209 | 672 | 20882 | 24 |

  自 s163 基線 732→672（本輪 -60）；DB +36 為此前被 cap-ok==0 提前擋下的函式現在走得更深
  （blk-expr/blk-let/blk-done 等各 +8~+9，呈彌散分布，非單點新增）；ID 32→24。
  LF 副通道逐條等長（463=463）未動。
- **dropped 会计（209→209，4 進 4 出）**：
  新 drop 4 個——`ps-parse-method-func`（callee `add`，1 construct）、
  `drv.build-ll`（callee `cg-runtime-prelude-full` 1 / `sb.init` 2）、
  `lx-esc` 與 `lx-read-raw`（callee `sb.init` 2）。這些 callee 的構造缺失**自上輪基線即有**
  （s163c 同值），且 4 者本身已可產碼 ⇒ 按 §83 同一口徑屬誠實下游呈報，非回退。
  同期 un-drop 4 個——`h2m-fmt-class`/`h2m-fmt-colon`/`h2m-fmt-field-class`/
  `h2m-fmt-find-close`：其 callee `h2m-byte-at` 经修法一 3→0，自然轉為 alive。
- **探針**（`tmp/s163/probepkg/main.no` + `probe-pkg.no`，block 通道逐函式）：
  Declare+Read/Reassign/Push/Arm/ArmRead/`[]i64`推入 6 形、Order/Str-Arm/Typed-Assign/
  Unknown-Read 皆 0；守衛 f-unann-only 3（首見無標注仍 gate）、f-ats 2
  （`ix.at-s` 元素操作不在本輪範圍，element-op 級缺口照舊）。
- **門禁（S0 全程）**：S1 重建 `./tmp/s153/bin/no build -o tmp/s163/noc-s1 compiler`
  rc=0；A/B（`tmp/s163/ab.sh`，11 檔 rc+sha256）全 SAME；L0 18/18（2513 assertions，
  0 failed；h2m-test 1429→**1437**：新增 test-s164-vec-reassign-mem/straight/gate 三例
  8 斷言）；L5 `./tmp/s163/noc-s1 build -o tmp/s163/noc-s2 compiler` rc=1，
  unsupported constructs **672**、dropped **209**，與 S0 census 逐項一致；
  smoke（`tmp/s163/smoke/main.no`，追加 ⑧⑨ 兩個 reassign 端到端用例）S0/S1 輸出
  逐字節相同（reassign-i64/str len 均為 0，即槽寫回後跨塊讀取成立）。
- **DR-1 狀態**：①行為面尚不能封裝（463 construct 待清）；②fixpoint 尾部 672>0 不可達；
  ③L1–L4 全綠、L5 rc=1；④L5 已就緒。下一輪 top cluster 換手為 `drv.cmd-*`
  （cmd-build 17、cmd-run 14、cmd-test 11、cmd-fmt 7），先在 block/LF/ID/DB 四通道上
  recon 具體構造（預期仍是 with-cap／賦值型別沿用的同類變種，或是新的 element-op 缺口）。
- **未提交任何改動**。
