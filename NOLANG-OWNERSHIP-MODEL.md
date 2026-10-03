# Nolang 混合所有權模型（Hybrid Ownership）— 設計方案

**版本**：v2.9（2026-10-03）
**基線**：`96e8d3e7`（v2.4 的文件提交）。所有 A/B 都用**自建快照**（`/tmp/**/no-*`），全程不使用 `bin/no`（並行 session 會重建它）。
**適用後端**：MIR（`src/mir`）
**前置文件**：`docs/docs/lang/memory.md`（現行模型權威描述）、`NOLANG-AUDIT-2026-09-27.md`
**改動規範**：skill `nolang-compiler-change`（金標、A/B 歸因、並行 session 陷阱）

> **v2.0 相對 v1.3 的變更**：新增 **§1.1 規則一「值對值賦值一律深拷貝」**（使用者定案，2026-10-01），
> 並據此**重整全文**——移除已作廢的「首寫 clone（修正 A）」實作考古、P0–P6 的逐輪進度敘事，
> 以及 async 邊界三個缺陷的修復過程記錄（只保留結論）。**未落地事項集中在 §4**。
>
> **v2.1 相對 v2.0 的變更**：規則一**落地**（`str` / `[]T`）——`forwardReadOnlyAliases` 的前向整段刪除、
> 改名 `lowerDeadSourceCopiesToMoves`；owned `str` 的新綁定改走深拷貝路徑。新增
> `src/mir/rule1_binding_test.go` 與 `tests/rule1-binding.no`。
>
> **v2.2 相對 v2.1 的變更**：規則一**補完 `?str` / `?[]T`**（inline-payload 的 option）——
> 綁定閘門開到 owned option（`hir2mir.go`）、新增 `emitOptionCloneHelper`（drop helper 的鏡像）、
> **並修掉 `emitOptionDrop` 的 `case "vec"` 死碼**（`?[]T` 的 payload 從來沒被釋放過，實測
> 10 萬圈漏 100000 塊）。落地後暴露一個既有迴歸（`vecDeepFree` 的深釋放無視元素引用計數）並修好，
> 見 §4.2(a)。§0.3／§1.1／§1.3／§4.2(a)／§5／§6.2／§7 同步更新。
> **只剩 map 未收**（§4.2 a；`isOwnedLocal` 對 map 為 false，map 是引用語意，不是所有權缺陷）。
> 🔴 **這一句的三個斷言在 v2.4 全部被推翻**（`Type.Owned` 對 map 是 `true`；共用是缺陷不是定義；
> 修法與 map 的擁有權歸屬無關）。保留原文供對照，正確版本見 §4.2 a 與 §4.4 b。
>
> ⚠️ **驗收的控制組一律指名 SHA**：本輪控制組 = `no-head777`（真 HEAD `777b9389`）；「修復前」=
> `a6c8559d`（HEAD 的父提交）。並行 session 會把 HEAD 往前推，「HEAD 控制樹」不是穩定的指稱。
>
> **v2.3 相對 v2.2 的變更**：**參數側的 retain 落地為「靜態閘門」**（§2.3b／§4.2 b ⑥）——
> spawn 參數「源仍活但**可證明** spawn 之後不原位寫入、且型別可共享（owned `str`／**非擁有元素**的
> `[]T`）」時，共用 buffer ＋ 第二個引用（`@str_retain`/`@vec_retain`），否則維持深拷貝。
> 新增 `src/mir/spawn_arg_retain_test.go`（9 例）與 `tests/async-arg-retain.no`。
> ⚠️ **對既有語料零觸發**（保語義的精化）：44 個 `OpRun` 檔定點 A/B **0 差異**、全量 golden 517 檔
> 的 DIVERGE/REGRESS 集合與 base **完全相同**；反向對照（關掉閘門）證明寫入守衛**承重**。
>
> **v2.4 相對 v2.3 的變更**：**修掉兩個可觀測缺陷，並更正 v2.3 的一個錯誤歸因**（§4.4）。
> 1. **切片視圖的來源被提前釋放**（§4.4 a）：`b = a[i..j]` 之後讀 `b` 讀到**已釋放的**記憶體
>    （`print(a[2..4])` 印堆位址）。修法：`Module.viewSrc` ＋ `defUse` 沿鏈標 use。
> 2. **`%vec` 元素賦值的淺拷貝**（§4.4 b）：`emitIndexStore` 只為 `%str-long` 元素深拷貝，
>    `%vec` 元素留成位元複製 ⇒ `[][]i64` 元素賦值 `trace/BPT trap`，且 **map 的值懸空**
>    （`map.no` 的 `.vals[idx] = val` 就是這種元素寫入）。補上與 `vec.push` 對稱的分支後
>    **規則一的剩餘型別（map 的值）就此收完**。IR 直證：`hashmap_*_put` 的 `vec_clone` 呼叫 **0 → 2**。
> 3. 🔴 **更正**：v2.3 把 `tests/slice-heavy.no` 的金標差異記為「ASLR、不可歸因」——**那是錯的**，
>    它是缺陷 1 的 use-after-free（讀到 header magic `0x6E6F6C616E670001`）。修好後該檔指紋
>    與凍結金標**逐位元組相同**，全量 DIVERGE **9 → 8**。教訓見 §5 操作紀律。
> 4. **`Type.Owned` 對 map 是 `true`**（v2.2 起的錯誤診斷），`§4.2 a` 已改寫。
> 5. 新增 `src/mir/view_liveness_test.go`、`tests/slice-view-liveness.no`、`tests/vec-elem-ownership.no`。
>
> **v2.5 相對 v2.4 的變更**：**把「接收者投影」的失效條件補完**（§1.2／§4.5）——v2.4 只寫了第一層
> （「來源已經沒有那個值了」）。本輪由**使用者回報的症狀**（`str.trim()` 的結果長度為 0）引出，
> 補上第二層與一個缺失的護欄：
> 1. **一個 value 被定義第二次就撤回投影**（§4.5 a）：MIR 的 value id 是**變數**不是 SSA 名，
>    重新綁定（`line = line.trim()`）走 **move-into 編碼**（`Dst = NoVal`）**再定義同一個 id**，
>    而 `defInst` 只記 `Dst > NoVal` ⇒ **move 隱形** ⇒ 投影停在**舊**的儲存位置。
>    新增 `definedValue(inst)`（跨兩種編碼）＋ `defCount`：**第二次定義即 `delete(defInst)`**。
> 2. **被投影的路徑被寫入 ⇒ 撤回投影**（§4.5 b）：投影 `&root.field`／`&container[i]` 只在
>    「**那條路徑上沒人寫過**」時才還代表同一個值。新增 `sourceWrittenBetween(read, use)`
>    （寫入必須**嚴格落在** read 與 use 之間）＋ `projectionChain`／`reachableBlocks`（與
>    `sourceAlreadyDropped` 共用），**同時掛上 `OpGetField` 與 `OpIndex`**——後者原本
>    **完全沒有護欄**，症狀是 `SIGTRAP`（use-after-free）。`getFieldWasCloned` 更名
>    `projectionWasCloned`（field 與 index 共用）。🔴 前提是 `projectionWasCloned(v)`：
>    **非 clone 的欄位不可套用**（`x = o.c; x.n = 7` 必須繼續投影）。
> 3. 本輪另落地兩個**與所有權無關**的改動（§4.6，只作記錄以免日後誤判為迴歸）：單字元 `str` 字面量
>    ＋ `char` 字面量的 `+`/`-` 被位元組折疊（`'x' - "a"` 得 `23` 而非 `"xa"`）；`go` 關鍵字的無色
>    協程轉寫——**它只是 `run`/`awy` 的語法糖，§2.3／§2.3b／§2.4 的邊界協議逐字不變**。
> 新增 `src/mir/rebind_retracts_projection_test.go`、`src/mir/str_lit_char_concat_test.go`、
> `tests/rebind-retracts-view.no`。
>
> **v2.6 相對 v2.5 的變更**：**tier 推斷真的落地了**（§3.1／§3.3／§4.2 c）。v2.5 時
> `tierConstraint` 對每個 use 都回 S ⇒ 推斷是**恆等映射** ⇒ 那時寫 `checkTierSoundness` 是**自證**
> （拿恆真的推斷去驗證自己）。本輪把兩件事**一起**做：
> 1. **真實的 constraint 函式。** `tierConstraint`（指令內可判定的列）回答 §3.1 的 R 列：spawn 的
>    **結果**（`%task` handle）、**被 §4.2 b 閘門共享**的 spawn 參數（讀回 `spawnArgRetains` 這個
>    **已記錄的決定**，不重算）、`OpTaskRetain`／`OpAwait` 的運算元。`tierStep`（需要看**當前 tier
>    map** 的兩列）補上**別名傳遞性**（`OpMove`／`OpCast`／`OpPhi` 的兩端是同一個引用 ⇒ 取 join；
>    **`OpClone` 刻意不在其中**——克隆是新值，這正是規則一能說「值對值不產生別名」的原因）與 **I2**
>    （存入 R 檔容器者亦為 R；今天真空，但規則先立著）。**C 檔的觸發點是新的 `writtenThroughSet`**：
>    借讀視圖**被寫穿**（`OpSetField`／`OpIndexStore` 的 `Args[0]`）⇒ C——這正是 §4.2 c 要的
>    「C 檔 CoW 有寫入點可查」。
> 2. **`checkTierSoundness` 落地為硬閘門**（I1，§3.3）。它從 **IR 那一側**讀「這是 R 物件」的地面真相
>    ——真實 spawn 的 handle 及**其全部別名**（`handleAliasSet`，與 spawn graph／`checkRefBalance`
>    共用同一個謂詞）、§4.2 b 共享的 spawn 參數、每個 `OpTaskRetain`／`OpAwait` 運算元——並要求推斷
>    對它們**一律回 R**；外加**分區完整性**（每個值都有檔位，且不低於自己使用點的要求——後半是一趟
>    fixpoint 撐不住的**別名鏈**）。**I2 刻意不查**（今天唯一的 R 物件是 `%task`，不是容器 ⇒ 真空；
>    空檢查比不檢查更糟）。
> **非空性證明（兩個方向都做）**：把**恆等分區**（每個值 S，即 v2.5 的真實行為）餵給
> `checkTierRSites` ⇒ **必須開火**；把**空 map** 餵給 `checkTierPartition` ⇒ **必須開火**（那是 P1
> 唯一的真缺陷）。兩者都有測試，且控制組是**重建原行為**而非「關掉新行為」。
> ⚠️ **仍然沒有任何東西依 tier 改寫 IR。** R 檔描述的是編譯器**已經**在發的機制（handle 的
> retain/release ＋ §4.2 b 閘門）；要讓 tier 變成**規定式**，得把這個 pass 移到
> `insertBorrowRetains`／`emitAsyncRun` **之前**，那是 P2／P5 的工作（§4.2 c 記了這條界線與理由）。
> 改寫：`src/mir/tier.go`、`src/mir/tier_test.go`（10 個測試）、`src/mir/analysis.go`（接線）。
>
> **v2.7 相對 v2.6 的變更**：**撤銷 §4.3 的「借讀 release 死亡點精度」殘留**——它是**誤判**，
> 而且從未在當前樹上複現過。本輪用三條獨立證據把它證偽（§4.3 有完整表格）：MIR 直讀顯示**剝離結果的
> drop 就在迴圈內**（原條目說「會被提到迴圈外」）；8 個形狀 × 2,000,000 次迭代的端到端 RSS **全部平線**
> （1.7 MB）；語料 691 檔（**全部**成功 lower）逐值不對稱只有 **13 筆**，**全部**可用
> 「option 的深釋放是 rc 感知」與「release 落在 move 目的地」解釋。原文的
> 「1717 retain / 1693 release、未配對 24 個」**無法複現**（同 roots／同 lowering 路徑／同粒度得 68/58/13），
> 已標註為不可信。
> **本輪另加一條釘子** `TestBorrowInLoopIsDisposedEveryIteration`（含**負對照**：沒有迴圈攜帶累加器的
> 形狀必須不滿足該性質），並**先證明它會壞**：把 `insertDrops` 的 start-drop 從迴圈內提到迴圈外
> （臨時 hack，已回退）⇒ 測試 **FAIL**、RSS 漲到 66 MB／259 MB。**本輪無生產程式碼變更**（只加測試
> ＋改文件）。
>
> **v2.8 相對 v2.7 的變更**：**修掉一個「呼叫臨時變數的 `alloca` 落在迴圈內」的堆疊溢位**（§4.6 第三列）。
> 它是**量 map 洩漏時撞到的**、與所有權無關的獨立缺陷：LLVM 的 `alloca` 是**指令**不是宣告，每次執行都
> 保留棧空間、且只在函式返回時回收 ⇒ 迴圈體裡的一條 `alloca` **每圈增長一次棧**，無上限。實測
> `i <- [0..200000) { m.put('key', 1) }` 在 ~180k 圈 **SIGSEGV（rc=139）**，而峰值 RSS 只有 15.6 MB
> （**棧**溢位，不是堆耗盡）。`-O2` 後的 IR 仍留著 `%carg12 = alloca %str-long` 與 `%cres14 = alloca i1`
> 在迴圈體內：**LICM 不提 `alloca`，mem2reg/SROA 也無法提升不在 entry block 的 `alloca`**。
> 修法：`emitFunc` 先把函式體寫進暫存 builder，結束時用 `hoistEntryAllocas` 把**靜態** `alloca`
> 全部搬進 entry block（**動態** `alloca i8, i64 %n` 必須留在原地——它的人數運算元可能由後面的 block
> 定義，entry 不支配它）。修好之後同一支程式 **rc=0**，並**解除遮蔽**了 map 洩漏：現在可以量到
> 800,000 圈 ⇒ **651 MB 且線性成長**（§4.3）。
> 生產改動：`codegen.go`（`sb` 改成 `*strings.Builder` 以便逐函式重導 ＋ 新增 `hoistEntryAllocas`／
> `isHoistableAlloca`）、`analysis.go`／`spawn_graph.go`／`mir.go`（建構點補 `sb`）、
> 新增 `src/mir/entry_alloca_test.go`（3 個測試，含**動態 alloca 負對照**；**已證明會壞**：關掉 hoist
> ⇒ `TestCallTemporariesAreAllocatedInTheEntryBlock` FAIL，報 `%cres11 = alloca i64`）。
>
> **驗收**：控制組 = **同一棵樹、只把 `hoistEntryAllocas(x)` 換成 `x`** 的 binary（`/tmp/no_kill`），
> 治療組 = `/tmp/no_fix`。**全語料 691 檔「編譯＋執行」A/B**（比 `rc` ＋ stdout sha）：
> BUILDFAIL 集合**完全相同**（3 檔：`src/std/number.no`、`tests/opt-container-len.no`、`tests/std-new.no`，
> 皆非本輪），**只有 4 筆差異且全部解釋**：`std-unix-fs-os.no`／`std-unix-fs-os-2.no` 是**本質非決定性**
> （印 `mkstemp` 隨機名，**同一支 binary 連跑 3 次 3 個 sha**）；`tls.no`／`https-server.no` 是
> `-P 6` 下載撞 8s 逾時（**序列重跑兩邊 sha 逐字相同**）。`no vet src/std` 的 **error 集合逐行相同**
> （47 error，皆來自別的 session 的未提交工作）。核心套件 `go test` 全 ok；本次改的 6 檔 `gofmt` 乾淨。

---

## 0. 一頁摘要

### 0.1 三檔

| 檔位 | 名稱 | 成立條件 | 表示法 | 運行時開銷 |
|---|---|---|---|---|
| **S** | 靜態唯一 | 該 buffer 在其生命期內**從不與其他活躍引用別名** | 裸指標 | **0** |
| **C** | 靜態 CoW | 存在別名（**只能來自元素／欄位讀取**，§1.2），且別名與寫入都在同一線性上下文 | 裸指標 | 0（首寫之前插一次 clone） |
| **R** | 引用計數 | 別名跨出線性上下文（**協程 spawn**），存活順序不可靜態判定 | header + refcount | 每次複製／釋放一次 inc/dec |

**核心原則**：檔位是「值」的靜態屬性，由分析推斷、由驗證器強制。**S 與 C 共用完全相同的 ABI**（裸指標），
只有 R 需要 header。這帶來兩個好處：S/C 階段不需要任何 ABI 變更（可用金標驗證「輸出位元組全等」），
而 R 的引入是**加法**，不動既有路徑。

### 0.2 兩條拷貝規則

1. **值對值賦值一律深拷貝**（`a = b`）——不產生別名。**§1.1**
2. **元素／欄位讀取保留別名**（`a = b[i]`、`a = b.c`）——維持原本的簡稱別名能力。**§1.2**

這兩條合起來就是整個模型的分界線：**值與值之間只有「擁有」與「搬移」，沒有共享**；
唯一允許的共享是「視圖」，而視圖只從容器／聚合的內部取出來。

### 0.3 完成狀態（2026-10-03）

**已落地並通過驗收**

- **header ABI 全型別**（§2.1）：37 個 alloc ＋ 35 個 free 站點全部改走 `@nolang_rc_alloc` / `@nolang_free`。
- **借讀的引用計數**（§2.2）：`OpRetain` / `OpRelease` 成對落地。**語料實測（可複現）**：`tests` ＋
  `test` ＋ `example` ＋ `src/std` 共 **691 檔全部成功 lower**，`OpRetain=68` / `OpRelease=58` /
  `OpDrop=11151`。⚠️ 舊版此處記的「1717 retain / 1693 release」**無法複現、已作廢**（§4.3）。
- **`vecDeepFree`**：深釋放配深克隆（對偶 `vecDeepClone`）。
- **async 邊界的兩條**（§2.3／§2.4）：spawn 參數「源已死 ⇒ move」、`%task` 的 RC ＋ 兩個漏計缺陷修復。
- **async 邊界第三條：spawn 參數的「靜態閘門 retain」**（§4.2 b ⑥，2026-10-02）：源仍活但**可證明**
  spawn 之後不原位寫入、且型別可共享（owned `str`／非擁有元素的 `[]T`）⇒ 共用 buffer ＋ 第二個引用
  （`@str_retain`/`@vec_retain`），否則維持深拷貝。對既有語料**零觸發**（保語義的精化）。
- **分析與驗證器**（§3）：tier 推斷（**已落地，且是硬閘門**，2026-10-03）、spawn graph 線性化
  （報告-only）、`checkRefBalance`、`checkTierSoundness`（I1，2026-10-03）。
- **規則一（§1.1）**：只讀前向移除、owned `str` / `[]T` / **`?str` / `?[]T`** 綁定改深拷貝（§4.2 a）。
  過程中一併修掉 `emitOptionDrop` 的 `case "vec"` 死碼（`?[]T` 的 payload 從未釋放，§4.2 a）。
- **切片視圖的壽命**（§4.4 a，2026-10-02）：`Module.viewSrc` ＋ `defUse` ⇒ 視圖的來源活到視圖的最後一次
  使用。修掉「`print(a[i..j])` 印堆位址」的 use-after-free。**這是本輪最重要的更正**：上一輪把它誤判為
  「ASLR、不可歸因」而放過（§4.2 b ⑥）。
- **`%vec` 元素賦值的深拷貝**（§4.4 b，2026-10-02）：`emitIndexStore` 補上與 `vec.push` 對稱的 `%vec`
  分支（共用 `vecElemTypeID`）⇒ 一併讓 **map 的值**擁有自己的 buffer（規則一的剩餘型別就此收完），
  並修掉 `[][]i64` 元素賦值的 `trace/BPT trap`。
- **接收者投影的失效條件補完**（§4.5，2026-10-03）：**重新綁定**（value 被第二次定義 ⇒ `defCount > 1`）
  與**被投影的路徑被寫入**（`sourceWrittenBetween`）都撤回投影；`OpIndex` 補上原本缺失的護欄
  （症狀是 `SIGTRAP`）。修掉「`str.trim()` 的結果**內容對、長度為 0／舊值**」。前提 `projectionWasCloned`。

**待辦**（前置條件見 §4）

1. ~~**規則一的剩餘型別**（§4.2 a）：只剩 **map**。`isOwnedLocal` 對 map 為 false，而 map 是**引用語意**
   （`owned=false`、從不釋放），共用是它的定義而非缺陷；要收就得先決定 map 的擁有權歸屬，不是補一個
   `OpClone` 能解決的。~~
   ✅ **已落地（2026-10-02），但原文的三個理由全是錯的**，見 §4.4(b)。摘要：`Type.Owned` 對 map **是 `true`**
   （`ClassifyOwnership` 的 `isMapRaw` 分支）；共用**不是**「它的定義」而是**可觀測的懸空**；修法**不是**
   map 的擁有權歸屬，而是 **`emitIndexStore` 對 `%vec` 元素的深拷貝**（`vecDeepClone` 早已存在，只是註解
   說「沒有 vec clone helper」而沒接上）。同一個修法一併修好 `[][]i64` 的元素賦值（原本 `trace/BPT trap`）。
2. ~~**參數側的 retain**（源仍活時用 retain 取代深拷貝）~~ ✅ **已落地為靜態閘門**（§2.3b／§4.2 b ⑤⑥）。
   原文記的「buffer 要帶 header」前置**已不存在**（header ABI 全型別落地，§4.2 b ①）；真正的障礙是
   **語義**——無條件 retain 會把 spawn 邊界的「快照」變成「共享」——所以收成「**可證明無原位寫入 ⇒
   retain**」（`spawnArgWritesAfter` ＋ `spawnArgRetainSafe`），無法證明就退回深拷貝。
   **刻意收窄**：只覆蓋 owned `str` 與**元素不擁有堆**的 `[]T`；`[]str`/`[][]i64`、struct、以及任何
   無法證明無寫入者**維持深拷貝**。⚠️ 對既有語料**零觸發**（保語義的精化，見 §4.2 b ⑥）。
3. ~~**`checkTierSoundness`**~~ ✅ **已落地（2026-10-03，v2.6）**：tier 推斷不再報告-only（§3.1），
   驗證器守住 I1 並成為**硬閘門**（§3.3）。全語料 575 檔編譯掃描 **0 個 `tier-soundness`**（無假陽性）。
   **`checkReleaseTarget`（I5/I6）仍未落地，且仍不該寫**：它要守的性質活在 `@nolang_rc_release` 的
   **函式體**裡，MIR 看不到（§3.3）。
4. **map 的儲存從不釋放**（§4.3）：**只洩漏、不懸空**，但本輪（2026-10-03）複查確認它是**功能**
   而非小修——根因是**三段**（`StructOwnedLeafFieldIdxs` 只看 `KindStr` ⇒ 不插 drop；`emitStructDropHelper`
   只釋放 `%str-long` leaf；`emitLeafFieldsCloneR` 只深拷貝 `%str-long` leaf），且 `%str-long` 硬寫點
   在 `codegen.go` **十餘處**。**只做前兩段會把洩漏升級成 double free** ⇒ 與 §4.2 c 同級，**判定不落地**。
   **v2.8 複查：洩漏量首次可以量到底**（原本被棧溢位截斷，§4.6）：丟棄一個 `[str]i64` ≈ **814 B**，
   50k→44.8 MB、100k→87.7 MB、200k→164 MB、**800k→651 MB，線性無上界**。**負對照**：`init()` 但
   從不 `put` ⇒ 1,000,000 圈仍是 **1.69 MB 平線**（漏的是 `put` 配置的 buffer，不是 `init`）。
   ✅ **v2.10：tier 1 已落地**——`hashmap-*` / `static_hashmap-*` 的 `keys`/`vals`/`occ` 現在**既會被
   深拷貝（規則一）也會被釋放**：同一支探針從 **200k→164 MB / 800k→651 MB（線性）** 變成
   **200k→1.82 MB / 800k→1.80 MB（平線）**，且 800k 圈 `rc=0`、無 SIGTRAP／SIGSEGV。
   做法、逐型別放行表與全部數字見 §4.3 的「v2.10」小節。**其餘 ~30 個 struct 型別**
   （`set-*`／`heap-*`／json、yaml 的 pool 型別……）**維持不落地**。

**一句話總結**：**S／C 兩檔的機制完整且被驗證；R 檔只覆蓋了 `%task`。** 「值對值別名」整類已從模型與
實作裡刪掉（`str` / `[]T` / `?str` / `?[]T` 已收，**map 的值**亦已收——修在**元素寫入**而非 map 自身，§4.4 b）；
參數側的 retain 已收成**靜態閘門**（窄範圍、對既有語料零觸發）；規則一那輪另修掉兩個**可觀測缺陷**：
**切片視圖的來源被提前釋放**（§4.4 a）與 **`%vec` 元素賦值的淺拷貝**（§4.4 b）；v2.5 修掉
**接收者投影的兩層失效條件**（§4.5）；v2.6 **tier 推斷落地並成為硬閘門**（§3.1／§3.3，
含 C 檔的寫入點 `writtenThroughSet`）；v2.7 **撤銷了 §4.3 的「借讀 release 死亡點精度」殘留**
（三條獨立證據證偽，並補上會壞的釘子）；v2.10 **把 §4.3 的十餘處 `%str-long` 硬寫點收斂成單一型別
分派入口，並對 `hashmap-*`／`static_hashmap-*` 放行**（map 的儲存終於會釋放，規則一的洞同時補上，
語料 A/B 692 檔全等）。剩下的主要待辦**只有兩項**：
**§4.3 其餘 struct 型別的放行**（`set-*`／`heap-*`／pool 型別，逐型別 ＋ 各自的全語料 A/B），
以及把 tier 從**描述式**變成**規定式**
（§4.2 c 的界線：那需要把 pass 移到 `insertBorrowRetains` 之前）。

---

## 1. 模型

### 1.1 規則一：值對值賦值一律深拷貝（`a = b`）

**規則。** 當右邊是**具名變數**（整個值）、左邊是**全新綁定**時，左邊必須取得**自己的一份深拷貝**；
它**不得**與右邊共用 buffer，也**不得**只是右邊的另一個名字。

編譯器只允許兩種降低：

| # | 情形 | 降低 |
|---|---|---|
| 1 | 源在賦值之後**仍活** | **深拷貝**（`OpClone`）：`%str-long` → `@str_clone`、`%vec` → `vecDeepClone`、struct 遞迴深拷貝 |
| 2 | 源在賦值之後**已死** | **move**（零拷貝，源豁免 drop） |

**被禁止的第三種降低是「零拷貝別名」**：把 `a` 的所有使用改寫成 `b`、兩者共用一個 buffer、只留一個 drop。
這一種曾經存在（`forwardReadOnlyAliases` 的只讀前向），**已於 2026-10-02 移除**（§4.2 a）。

**為什麼。** 值對值的隨意別名沒有任何語義價值，卻讓「誰擁有、誰釋放、寫入是否可被觀測」的分析
憑空多出一整類情形——別名鏈、第二個別名、跨塊別名、參數別名、選項剝離的別名……每一類都要一組守則
與一組反例。**用一次深拷貝買回「一個值只有一個所有者」，是划算的**：拷貝是一次性的成本，
分析複雜度是永久的成本。

> **為什麼保留第 2 種（move）。** move 不產生別名——源在搬移之後沒有任何可觀測的讀取——所以它與深拷貝
> 在建置出的程式上**可觀測等價**，但零成本。它不屬於「別名」。
> ⚠️ 若連這一條也要收掉（**無條件**深拷貝），說一聲即可：那只是效能取捨，不影響語義。

**落地範圍。** 三種型別的 `a = b` 原本行為**互不相同**，2026-10-02 起統一（§4.2 a）：

| 型別 | 原本的行為 | 現況 |
|---|---|---|
| `%vec`（owned slice） | 新綁定直接 `OpClone`（`hir2mir.go`），但**只讀時會被 `forwardReadOnlyAliases` 刪掉** ⇒ 變別名 | ✅ 前向移除後即為深拷貝 |
| `%str-long` | 與源**共用同一個 SSA 值**（`l.locals[name] = val`）⇒ 別名 | ✅ 改走「拷貝進新值」路徑 |
| `?str` / `?[]T`（owned option） | 被綁定閘門**排除**（`isOwnedLocal` 為 true）⇒ 兩個名字綁到同一個 SSA 值 | ✅ 閘門開到 owned option；`emitClone` 的 option→option 分支改呼叫 `emitOptionCloneHelper` |
| struct | 本來就產生 `OpClone`（實測 MIR：`clone dst=18:MyData args=[15]`） | ✅ 未動 |

**未收的型別**：只剩 **map**。`isOwnedLocal` 對 map 為 false，但 map 是**引用語意**——`Type.Owned` 為
false、從不被釋放——所以「兩個名字共用一個 map」是它的定義，不是所有權缺陷；要收得先決定 map 的
擁有權歸屬。見 §4.2 a。

### 1.2 規則二：元素／欄位讀取保留別名（`a = b[i]`、`a = b.c`）

從容器／聚合**取出一個子值**的讀取，**維持原本的簡稱別名能力**：`a` 拿到的是 `b` 內部儲存的**視圖**
（描述符的位元複製），與 `b` 共用 backing buffer。

- **這是刻意的**：它是 nolang 的「簡稱」慣用法（`line.body`、`items[i]`、`w.b.s`），
  改成深拷貝會讓每一次欄位／元素讀取都配置一次堆記憶體。
- **它的安全性由兩支既有機制守住**：
  1. **借用逃逸檢查**（`checkBorrowEscapes`）＋ **切片視圖降級**（`demoteUnsafeSliceViews`）：
     視圖逃逸且可能被覆寫時，降級為自有拷貝。
  2. **借讀的引用計數**（`OpRetain` / `OpRelease`，§2.2）：`v.push` 之類的 grow 會釋放舊 buffer，
     所以共用 buffer 的每個借讀值都必須各帶一個計數。

> **規則一與規則二的分界線就是「有沒有跨越值的邊界」。** `a = b` 是**值與值**的關係 ⇒ 擁有；
> `a = b[i]` 是**值與其內部**的關係 ⇒ 視圖。這條線讓「誰擁有 buffer」永遠只有一個答案：
> 容器／聚合擁有，視圖不擁有。

> ⚠️ **owned `str` 欄位的讀取是半個例外。** `emitGetField` 讀出 owned `str` 欄位時會 `@str_clone`，
> 讓讀出的暫存擁有自己的 buffer（否則暫存與欄位共用 buffer、兩邊都 drop ⇒ double free）。
> 伴隨的缺陷（已修）：`lvalueAddrOf` 曾把**每一個** getfield 都當成投影，把 `&struct.field` 交給呼叫方
> ——那是**來源那一份**的位址；來源 drop 之後 `contains` / `trim` / `set-byte` 全部讀到已釋放的記憶體
> （症狀：`std/markdown.no` 的表格渲染不確定，`-O0` 下 100% 重現）。修法是
> `getFieldWasCloned && sourceAlreadyDropped` 才停止投影。
> **🔴 「已 drop」≠「已死」**——必須用 drop 的**實際發出位置**判定，不是用存活分析。
>
> **🔴 投影有兩層失效條件，v2.4 只寫了第一層（2026-10-03 補完，§4.5）。**
> 第一層是「**來源已經沒有那個值了**」：`sourceAlreadyDropped`（來源的 drop 落在 read 與 use 之間）
> 與 `sourceWrittenBetween`（那條路徑在 read 與 use 之間**被寫過**）。
> 第二層是「**這個 value 已經不是當初那個值了**」：`defCount > 1`（value 被第二次定義 ⇒ 撤回投影）。
> 兩層都必要，且**都必須以「這個讀取產生了獨立的 owned `%str-long`」為前提**（`projectionWasCloned`）
> ——只有這樣，退回自己的槽位才永遠安全。判準一律是**結構性的**（指令的定義數、指令的相對位置），
> **不是存活分析**——這是本節兩次修正共同的教訓（「已 drop」≠「已死」；move-into 編碼對存活分析隱形）。

### 1.3 指令語義表

| 場景 | 規則一改動前 | 目標（＝現況） |
|---|---|---|
| `a = b`，源之後**仍活** | slice：深拷貝後可能被前向刪掉；str：共用 SSA 值；owned option：被閘門排除 ⇒ 共用 SSA 值 | **深拷貝**（`str`/`[]T`/`?str`/`?[]T` 已落地，§1.1） |
| `a = b`，源之後**已死** | slice：`OpClone` → `OpMove`；str：共用 SSA 值 | **move**（零拷貝、無別名） |
| `a = b[i]`、`a = b.c` | 別名（借讀）＋ `OpRetain`/`OpRelease` | **不變**（§1.2） |
| `out = x`（返回值） | move | 不變 |
| `v.push(x)` | 深拷貝元素 | 不變（容器是所有者） |
| 函數結束 | `OpDrop` | 不變（R 檔為 `OpRelease`） |
| `run f(x)` | 源已死 ⇒ move；源仍活 ⇒ 深拷貝 | 不變（§2.3） |
| `awy h` | `@nolang_rc_release`，歸零才 free | 不變（§2.4） |

> 前兩列的「目標」欄已於 2026-10-02 落地（`str` / `[]T`，再補 `?str` / `?[]T`；只剩 map 見 §4.2 a）。

**「move 是計數中性的」** 是關鍵設計：搬移不改變活躍別名數，所以 R 檔的 move 既不需 inc 也不需 dec。
這讓 R 檔可以完全重用既有的 move 分析，只是把 `OpDrop` 換成 `OpRelease`。

### 1.4 檔位晶格與推斷

偏序 `S ⊑ C ⊑ R`（join = 取上界）。一個值只要**任何一個**使用點需要較弱的保證，整個值就取該檔位。
別名關係是**傳遞的**（`a` 別名給 `b`，`b` 存進 `c`，`c` 跨協程邊界 ⇒ `a` 也必須是 R），
所以用單調資料流（worklist 定點）求解；規模 = 函數內 SSA 值數，很小。

**規則一讓 C 檔的來源大幅縮小**：值對值不再產生別名之後，C 檔的別名**只可能來自借讀視圖**（§1.2）。

### 1.5 不變式

| # | 不變式 | 違反後果 |
|---|---|---|
| I1 | 每個值**恰好一個**檔位 | 對裸指標做 header 存取 → 讀寫錯位 |
| I2 | S/C 值不得在未經顯式提升的情況下存入 R 容器 | 容器釋放時 free 錯位址 |
| I3 | 每個 `retain` 都有配對的 `release` | 洩漏或 UAF |
| I4 | 每個 owned 局部值在每條路徑上恰好一次 `OpDrop` | 洩漏或 double free（`checkDropCount` 已守） |
| I5 | `release` 只在計數歸零時 free，且 free 的是 `base` 而非 `data` | heap 損壞 |
| I6 | 借用 sentinel 永不 free、永不原地 clone | free 非本編譯器記憶體 |
| **I7（本版新增）** | **值對值賦值不產生別名**（§1.1） | 回到「隨意別名」的分析複雜度 |

> ⚠️ **I3 的「等號」形式不可作為硬錯誤。** 等號對「從未被 await 的引用」為假，而那是**已接受的洩漏**
> （§2.4）；在此處假陽性 = 硬編譯失敗。實作強制的是它的**安全方向**：每個非 root 別名成員都必須是
> 某個 `OpTaskRetain` 的運算元。少了 retain 不是洩漏而是 **UAF**（§2.4 的 D1）。

---

## 2. ABI 與執行時原語

### 2.1 header ABI（已全型別落地）

```
struct nolang_hdr {          ; 16 bytes，置於 data 之前
    i64 rc;                  ; 活躍別名數，>= 1
    i64 flags;               ; == nolangHdrMagic (0x6E6F6C616E670001) ⇒ 是本編譯器配置的塊
}
; data = (u8*)base + 16
```

- **落地的是「全型別」版**：所有編譯器擁有的堆 buffer 都經 `@nolang_rc_alloc`
  （`malloc(16+n)`、`data = base+16`、把 magic 寫進 `flags`）配置，經 `@nolang_free` 釋放；
  `codegen.go` 原有的 37 個 alloc ＋ 35 個 free 站點全部換過。**沒有「S/C 用裸 malloc、R 才帶 header」
  的分裂**——這是「靜態檔零開銷」在 ABI 這一層被放棄的部分。
- **🔴 `@nolang_free` 是 RELEASE，不是 free。** magic 命中 ⇒ `rc--`，**只在 `rc == 0` 時** `free(base)`；
  magic 不命中 ⇒ 對 `p` 直接 `free`（FFI 回傳、字串常量等非本編譯器記憶體）。
  ⇒ **任何沒有配對 release 的 retain 都是保證洩漏。** 這條推翻了先前「retain-only 不洩漏」的結論
  ——當時 heapstat 兩邊讀數「相同」，是因為**兩邊都在漏**。
- **🔴 這個 ABI 把「同指標 double free」（macOS 靜默容忍）放大成「內部指標 free」（`trace/BPT trap`）**
  ⇒ 它是既有所有權缺陷的**探測器**，不是單純換一組呼叫。實測：pre-P4 在 `MallocScribble=1` 下
  `tests/mem-safety/nested-container-clone.no` 已經 0/20 ⇒ 那個共享元素 buffer 的缺陷是**既有**的。
- **借用（borrowed）sentinel**：`flags.bit1` 的構想未採用。實作用的是 R 檔的 `rc == 0`
  （`@nolang_rc_release` 直接返回）與 C 檔的 `cap == 0`（`@str_free` / `@vec_free` 直接返回）；
  非本編譯器記憶體由 magic 檢查擋掉。**這條必須有**——否則會重現歷史上 `ensureVecBuffer`
  誤 free 借用視圖的 `trace/BPT trap`。

### 2.2 借讀的計數：`OpRetain` / `OpRelease`

header ABI 讓「grow 路徑釋放被替換的 buffer」變成不安全：借讀（`a[0]`、非克隆欄位讀取、enum 載荷讀取、
option→slice 剝離）拿到的是擁有者描述子的**位元複製** ⇒ 兩個值命名同一個 buffer。`v.push` 之類的 grow
會釋放舊 buffer；若該值正是這種複製，就釋放了擁有者還在指的記憶體。

修法是**成對的引用計數**：

| 動作 | MIR | codegen | 判準 |
|---|---|---|---|
| 借讀（owned slice） | `OpRetain`（`insertBorrowRetains` 緊接借讀之後插入） | `@vec_retain` | `borrowRetained`：`isBorrowRead && Owned && KindSlice` |
| 借讀值死亡 | `OpRelease`（`insertDrops` 每條路徑恰好一次） | `@vec_free`（**淺層**） | `borrowReleaseValues`：**讀回流中實際存在的 `OpRetain`** |

- **只有 `%vec` 借讀值得 refcount**：retain 的唯一用途是讓 grow 的無條件釋放安全，只有切片會 grow。
  **retain 是承重的**——拿掉它，`nested-vec-test` 0/20 崩。
- **`OpRelease` 必須是獨立的 op，不能重用 `OpDrop`**，有兩個獨立理由：
  1. `OpDrop` 會把值帶進 `wouldDrop`，而那是 clone-vs-move 分析讀的謂詞 ⇒ 會改變別處的降低決策。
     實測：`tests/tagged-enum-two-match.no` 20/20 SIGSEGV。
  2. 即使避開第 1 點，`emitDrop` 的 `%vec` 分支會**依元素型別**選深釋放；借用不擁有它的元素
     ⇒ 那是 double free。所以 `OpRelease` **永遠淺層**，且**只經 `droppable`** 放行
     （區域變數，只餵放置、不餵任何決策）。
- **🔴 配對必須是結構性的**：種子**讀回 `OpRetain` 指令本身**，而不是重新求值 `borrowRetained`。
  後者的答案取決於 `enumOwnsPayload`，而該 map 在 retain pass 之後會被 `markEnumPayloadOwners`
  與 `insertDrops` 的 moveSrc 迴圈改動 ⇒ 重求值會產生**沒有 retain 的 release**
  （實測：`tagged-enum-two-match.no` 出現 `release args=[76]` 而全檔 retain 為 0，rc=139）。
  **通則：當一個 pass 發出 X、後面的 pass 必須發出 X 的對偶時，後者要 key 在已發出的指令上，
  不要 key 在會漂移的謂詞上。**
- **深釋放必須配深克隆**：`emitBuiltinVecPush` 對 `%str-long` / `%vec` 元素做深克隆，
  但 `@vec_free` 只釋放外層 buffer ⇒ 每個元素漏一塊。`vecDeepFree`（與 `vecElemNeedsDeepFree`
  共用同一個謂詞——這正是這對函式之所以 sound 的原因）補上這半。
  **這類缺陷在 header ABI 之前就存在**：`[]str` push 迴圈在 pre-P4 就漏 100047 塊 / 10 萬圈。
- `@nolang_rc_retain` **必須有與 `@nolang_free` 相同的 magic 檢查**，否則會改到「前一個活配置的尾巴」。

### 2.3 async 邊界 A：spawn 參數的 move

`run f(x)` 對每個「擁有堆」的參數在 spawn 邊界做一次深拷貝（任務可能到 `awy` 才執行，而呼叫端在那之前
可以重賦值或離開作用域）。當 `x` 在 spawn 之後**可證明已死**時，那份拷貝是純浪費：所有權直接轉移進任務
即可——零拷貝，而且**不需要任何新 ABI**。

**判準（三個條件，全部必要）**：

| # | 條件 | 為什麼必要 |
|---|---|---|
| 1 | `wouldDrop[x]` | 呼叫端**真的**會釋放 x。借用（slice view／borrow read）與參數都不會被呼叫端釋放，把它們 move 進任務＝任務去 free 別人的 buffer |
| 2 | 源在 spawn 之後已死 | 用的是 `insertDrops` 判斷 clone-vs-move 的**同兩個謂詞**（`!liveOut` 且塊內之後無非 drop 讀取） |
| 3 | callee 該位置的參數歸類為 `str`／`vec`／`struct` | wrapper 才會釋放 payload。否則 wrapper 只釋放容器，move 進去的 payload 就洩漏 |

**為什麼條件 3 是充分的**：對這三類，**wrapper 的 free 與呼叫端的 `OpDrop` 降成同一支 helper**
（`@str_free` / `@vec_free` / struct 的解構子）。所以轉移所有權只是把**同一次 free** 從呼叫端搬到任務，
**不改變釋放了什麼**。反過來說，wrapper 釋放不了的類別（map、owning tagged enum、元素不可深拷貝的 slice）
歸類為 `""` 因而被排除——即使 `dropOwnsHeap` 說它們擁有堆。

**為什麼不需要「每條路徑恰好 await 一次」**：任務從未被 await 時，argbuf **兩種做法都會洩漏**——
釋放 payload 的是 wrapper（`w_free`），而 wrapper 只在 await 時執行。拷貝洩漏的是副本，move 洩漏的是本體，
**同一筆洩漏、少一次拷貝**。

**跨層協議**：判準需要兩邊的資訊（liveness 在 analysis、參數歸類在 codegen）。所以**只在 `insertDrops`
決定一次**，記進 `Module.spawnArgMoves`；`emitAsyncRun` 讀它跳過深拷貝，`checkDropCount` 讀它豁免
`missing-drop`。**三處都不重算**——重算正是 clone 與被抑制的 drop 會漂移的方式。參數歸類抽成
`Module.SpawnArgClasses`（`spawn_graph.go`），codegen 的 `asyncArgKinds` 是它的唯一實作；做法沿用
本檔既有的 `Module.OptionPayloadBoxed`——立一個只設 `mod` 的**拋棄式 codegen** 去問 emitter 自己的規則。
（`extraFuncs` 必須是真的 map：`vecDeepClone` 會在裡面記憶化，nil map 會 panic。）

> **🔴 唯一的真坑：`wouldDrop` 必須在 `moveSrc` 迴圈之後收集。** 該迴圈會設 `m.enumOwnsPayload`，
> 而 `dropOwnsHeap` **優先**讀它；提前收集就會漏掉「此處才變成 owning enum」的值 ⇒ 它們的 drop 被拿掉
> ⇒ 硬編譯失敗。**只有全語料的「編譯掃描」抓得到**（行為掃描與單元測試都看不到）。

### 2.3b async 邊界 A′：spawn 參數的「靜態閘門 retain」（§4.2 b ⑥）

move 處理「源已死」；當源**仍活**時，今天一律深拷貝。**可證明無原位寫入**時，改成**共用 buffer ＋
第二個引用**（retain）——零拷貝，且語義與深拷貝**逐位元組相同**（快照只有「呼叫端在 spawn 之後原位寫入」
才會分歧，而閘門正是把它排除掉）。

**兩個閘門，皆為必要**：

| 閘門 | 謂詞 | 拒絕的情形 |
|---|---|---|
| **型別可共享** | `spawnArgRetainSafe` | `[]str`/`[][]i64`（呼叫端 drop 是**深釋放**，會釋放任務仍要讀的元素 buffer）；`struct`（遞迴 retain 不存在）；map／tagged enum（本就不在 `SpawnArgClasses` 的 `str/vec/struct` 內） |
| **呼叫端不寫** | `spawnArgWritesAfter` | spawn 之後任一可達指令「可能寫」`x`（含**呼叫**、`OpMove`、`Alloc/Store/IndexStore/SetField`…）；**無法證明**（步數超預算）亦視為「有寫」 |

**為什麼是「兩個」而不是「一個」**：`str` 是單一區塊，共用只牽涉那塊 buffer；但 `[]T` 的呼叫端 drop 是
**深釋放**（`@__nolang_vec_free_*` 會釋放**元素**），所以只有在元素**不擁有堆**時，第二個引用才安全——
否則任務會讀到被呼叫端釋放掉的元素。這就是 `VecElemOwnsHeap`（＝ `vecElemNeedsDeepFree`）的作用。

**跨層協議與 move 完全同形**：`insertDrops` 決定一次、記進 `Module.spawnArgRetains`；`emitAsyncRun` 讀它
（`share := moved || retained`）補第二個引用。**兩者互斥**：源已死 ⇒ move（不 retain）；源仍活 ⇒ 才考慮
retain。`emitSpawnArgRetain` 對 `str` 發 `@str_retain`、對 `vec` 發 `@vec_retain`；兩者的 `cap==0`
守衛讓借用視圖對稱 no-op（借用本就不擁有 buffer，wrapper 的 free 也是 no-op）。

**平衡論證**：呼叫端保留綁定與 drop，wrapper 的 `w_free` 釋放第二個引用；`@nolang_free` 是 **release**，
所以**無論 `awy` 與呼叫端 drop 的順序**，buffer 都恰好在 rc 歸零時釋放一次。

### 2.4 async 邊界 B：`%task` 的引用計數

**唯一被提升到 R 檔的物件是 `%task` 本體**（32 bytes、5 欄）。計數紀律（每個「引用」一個計數）：

| 事件 | 動作 |
|---|---|
| `run f(x)` | `@nolang_rc_alloc(i64 32)` → `rc = 1`（回傳的 handle 持有的那一個） |
| handle 的**複製**（`h2 = h`、`h2 = run h`） | `OpTaskRetain` → `@nolang_rc_retain` |
| `awy h` | `@nolang_rc_release` → 歸零時才 free 兩個**借用**容器（args struct 與 result buffer） |
| `%task` 本體 | `release` 已經 free 了配置塊的 **base（`data - 16`）**；**不可再 free `data`**（那會是 free 一個已消失的塊的內部位址 ⇒ `trace/BPT trap`） |

**槽歸零與 RC 互補，缺一不可**：槽歸零處理「**同一個槽**被 await 兩次」（RC 無能為力——兩次讀到同一個
非零指標，只有歸零能讓第二次走守衛分支）；RC 處理「**同一個 handle 經別名** await 多次」。

**🔴 兩個「複製漏計」缺陷（已修）**：retain 的閘門本來掛在「有沒有已知的**任務結果型別**」
（`asyncResTypes`），**不是**「是不是 handle」——

| 缺陷 | 形狀 | 修復前實測 | 成因 |
|---|---|---|---|
| D1：void 任務 | `h = run void-async(); h2 = h; awy h; awy h2` | **rc = 139（SIGSEGV）** | 結果是 void ⇒ `asyncResTypes` 沒有條目 ⇒ 複製不 retain ⇒ rc 停在 1，第一次 `awy` 就 free |
| D2：`run <handle>` 轉發 | `h = run dbl(21); h2 = run h; awy h; awy h2` | 印 **`42 0`**（無崩潰） | `lowerAsyncRun` 的 `KIdent` 分支**直接回傳運算元**，既不建新值也不 retain |

**D1 的關鍵是「不對稱」**：同一個形狀在任務回傳 `i64` 時完全正確（`42 42`），只在回傳 void 時崩潰。
**D2 的關鍵是「同義的兩種拼法必須等價」**：`h2 = h` 與 `h2 = run h` 都表示第二個引用，修復後產生
**逐指令相同**的序列。修法＝新增 `asyncHandles`（**身分**）與 `asyncResTypes`（**結果型別**）分離；
`run <handle>` 改走同一套計數紀律（運算元不是 handle 時原樣回傳，不對未知形狀加 retain）。

> **🔴 一個 map 兩個用途就是一個 bug 的胚胎**：為 A 目的條件填充的集合，不可用「存在性」當 B 目的的判準。

**已知取捨**：一個**從未被 await 的 handle 引用**會使計數停在 1 而洩漏（例如 `h = run f(); h2 = h`
之後只 await `h`）。這與舊行為同級（舊版未 await 的 handle 本來就整組洩漏），**不是迴歸**。
要收掉它需要 handle 成為一個**有 `OpDrop` 的 owned 值**，那是另一層改動。

---

## 3. 演算法

### 3.1 Tier 推斷（單調定點）

```
初始化：每個值 → S（顯式 seed，見下）
worklist ← 所有值
重複直到收斂：
    對每個 v：t = tier(v) ⊔= 各使用點的 constraint；若上升，把 v 的別名加入 worklist

constraint(u, v)：
    OpRun 的結果            → R      ; %task handle，目前唯一的 R 來源
    OpRun 的參數（閘門共享） → R      ; 讀回 spawnArgRetains（§4.2 b ⑥ 的決定，不重算）
    OpRun 的參數（深拷貝／move）→ S   ; 呼叫端不留引用，值沒有跨邊界
    OpTaskRetain / OpAwait  → R      ; 第二個引用必須自己計數（§2.4）
    被 R 檔值別名           → R      ; 傳遞性（tierStep 的別名邊）
    存入 R 檔容器           → R      ; I2（tierStep；今天真空）
    借讀視圖且之後有寫入     → C      ; writtenThroughSet
    僅借讀且只讀            → S      ; 讀不提升檔位
    （值對值賦值）          → S      ; 規則一：不產生別名
```

**收斂性**：晶格高度 3，每輪只升不降。**別名鏈需要不只一趟**（`h → h2 → h3` 要三趟），
所以「只跑一趟」的 fixpoint 是錯的——這正是 `checkTierPartition` 的**閉包**那一半在守的東西。

**實作落點（2026-10-03）**：**指令內可判定**的列在 `tierConstraint`（`tier.go`），**需要看當前 tier
map** 的兩列在 `tierStep`；C 檔的觸發點在 `inferTiers` 的前置 pass（`writtenThroughSet`）。這個切分
是刻意的：`tierConstraint` 是設計表格的**可直接單測**的形式，`tierStep` 是把它閉合的資料流。

> ⚠️ **`inferTiers` 必須顯式 seed 每個值。** `TierS` 是晶格的零值，所以「只寫入上升的值」會讓全 S 的
> 推論回傳**空 map**——那是 P1 唯一的真缺陷，現在由 `checkTierPartition` 的 seed 那一半守住
> （測試用**空 map** 餵它，必須開火）。
>
> 🔴 **`OpClone` 不是別名邊。** 傳遞性只走 `OpMove`／`OpCast`／`OpPhi`；把 `OpClone` 算進去會讓
> 「克隆出來的獨立值」繼承來源的 R 檔 ⇒ 推斷**過度提升** ⇒ 驗證器（只查「S/C 不得是 R 物件」的
> **單向**）就再也抓不到真正的漏提升。**過度提升會讓檢查變安靜**，這與假陽性同樣危險。
>
> **觀測**：`NOLANG_MIR_TIER=1` 印 S/C/R 直方圖到 stderr（對編譯輸出 0 差異）。它現在只是**量測面**，
> 不再是「唯一的可觀測」——真正的消費者已經是 `checkTierSoundness`（§3.3）。

### 3.2 Escape / spawn graph 線性化

```
建圖：節點 = 函數；邊 = OpRun（spawn）
對每個 spawn 邊 (caller → callee)：
    linearizable = handle 不逃逸(caller 內)
                && ∀ 路徑 caller.exit：恰好一次 OpAwait(該 handle)
                && callee 不再 spawn（傳遞檢查）
                && 跨越邊界的值在 await 之後未被任一方使用
    if linearizable: 標記為「同步」→ 該邊的值維持 S/C，不提升到 R
    else:            該邊的值提升到 R
```

- **判定必須按 spawn 邊**而非按 handle 值：同一個函數裡一個線性化的 spawn 與一個逃逸的 spawn 可以並存，
  前者不應被後者拖進 R 檔。
- **「handle 不逃逸」的保守形式**：只被 `OpAwait` / `OpTaskRetain` / 別名運算使用；
  `OpSetField` / `OpIndexStore` / `OpStore` / 呼叫引數 / `OpReturn` / out-param 返回一律算逃逸。
- 「每條路徑恰好一次 await」從 spawn 前向走 CFG 到每個出口計數；遇到回到 spawn 的回邊就**切斷**
  （那一輪屬於下一次迭代的 handle）。
- ⚠️ `cancel(h)` 是**已知偽陽性**（內建既不保存也不 retain），但**刻意不做白名單**——
  白名單正是這類分析開始說謊的方式。
- 目前是**報告-only**（`NOLANG_MIR_SPAWN_GRAPH=1`），對編譯輸出 0 差異。

### 3.3 驗證器

| 驗證器 | 不變式 | 狀態 |
|---|---|---|
| `checkDropCount` | I4 | ✅ 已落地 |
| `checkRefBalance` | I3（安全方向） | ✅ 已落地 |
| `checkTierSoundness` | I1 | ✅ **已落地（2026-10-03，v2.6）**，見下 |
| `checkReleaseTarget` | I5, I6 | ⬜ **刻意未實作**（§4.2 c） |

**`checkRefBalance` 的要點**：與插入器**共用謂詞**（直接用 `spawn_graph.go` 的 `handleAliasSet`，
同時走兩種 `OpMove` 編碼、`OpCast`、`OpPhi`）；**只報安全方向**（每個非 root 別名成員必須是某個
`OpTaskRetain` 的運算元；沒 await 的引用不報）；**按 spawn 站點計**（`OpRun` 且 `Sym != ""` 才是真正的
spawn，`Sym == ""` 的 `run <var>` 只是轉發，沒有新計數可平衡）。
**必須有「修復前必須失敗」的測試**（用 `stripTaskRetains` 把修復加上的 retain 拿掉，再要求驗證器開火）
——否則一個「什麼都沒看」的空驗證器也會全綠。

**`checkTierSoundness` 的要點（2026-10-03 落地）**：守的是 I1——「被推斷成 S/C 的值**真的**沒有跨
線性上下文的別名」——而**兩個方向是從相反的兩側寫的**，這才是它不自我證明的理由：
- **constraint**（`tierConstraint`／`tierStep`）是**設計表格**：「這個使用點要求運算元是什麼檔位」；
- **驗證器**讀的是 **IR 那一側**的地面真相：「這是 R 物件」——真實 spawn 的 handle **及其全部別名**
  （`handleAliasSet`，與 spawn graph／`checkRefBalance` **共用同一個謂詞**）、§4.2 b 共享的 spawn
  參數、每個 `OpTaskRetain`／`OpAwait` 運算元。

兩者都**讀回已記錄的決定**（`spawnArgRetains` 是 `insertDrops` 寫下的；retain/await 是**串流裡的
指令**）而不是各自重算（§4.5 的紀律），所以「推斷的結論」與「IR 的假設」一旦漂移就是**硬編譯失敗**，
而不是靜默的錯檔位。另有**分區完整性**那一半：每個值都有檔位，且不低於自己使用點的要求。

**非空性（必須「修復前會壞」）**：把**恆等分區**（每個值 S——即 v2.5 的真實行為）餵給
`checkTierRSites`，它**必須開火**；把**空 map** 餵給 `checkTierPartition`，它**必須開火**。
兩個測試都要求「先證明它會壞」，且控制組是**忠實重建原行為**（改寫整個 map），不是「關掉新行為」
——後者在「新舊都會寫」的分支上不等價（§5 操作紀律）。

**I2 為什麼仍然不查**：今天唯一的 R 物件是 `%task`，而 `%task` 不是容器 ⇒ 規則真空。§3.3 的判決
不變：**空檢查比不寫更糟**，因為它讓「驗證器存在」變成虛假的保證。

**`checkReleaseTarget` 為什麼仍刻意不寫**：I5/I6 是 **codegen 層**的性質（MIR 看不到
`@nolang_rc_release` 的函式體，而 `ValidateTypes` 是 AST 層的鏈，放不進去）。等 R 檔接上使用者值
（P5）再寫。

⚠️ **驗證器的假陽性在此是硬編譯失敗**（`rep.HasErrors()` 會 gate codegen）。**新驗證器必須與插入器
共用同一組判定謂詞**，且要跑**全語料的編譯掃描**。新增診斷要四處齊備（`checker.go` 的 `ValidateXxx`
＋ `lint.go` 的 `RunAllLints` ＋ `build/transpiler.go` 的硬錯誤清單 ＋ `TraceID`）；
⚠️ 若驗證器放在**新檔案**，`Makefile` 的 `TRACE_ID_FILES` 不含它，`TraceID: "PLACEHOLDER"`
**永遠不會被替換** ⇒ 必須硬編碼字面 id（`checkRefBalance` 因此放在**既有檔案** `analysis.go`）。

---

## 4. 落地狀態

### 4.1 已落地

§0.3 已列。細節與量測見 §2／§3，語料見附錄 A，觸點見附錄 B。

### 4.2 落地狀態（(a) 全落；(b) 已落閘門；(c) I1 已落、I5/I6 仍不落）

#### (a) 規則一（§1.1）—— **`str`／`[]T`／`?str`／`?[]T`／map 的值 已全部落地**

**已落地的三處**（2026-10-02）：

| 位置 | 原本的行為 | 現在 |
|---|---|---|
| `src/mir/alias_forward.go` | `forwardReadOnlyAliases`：把只讀別名的所有使用改寫成源、**刪掉那個拷貝**（共用 buffer、只剩一個 drop）；`aliasWritesUnobservable` 還會在源已死時前向**被寫**的別名 | 改名 **`lowerDeadSourceCopiesToMoves`**，**只保留「源已死 ⇒ `OpClone` 改寫成 `OpMove`」**；前向與 `aliasUse`／`aliasHazard`／`rewriteUses` 整段刪除 |
| `src/mir/hir2mir.go` 新綁定 | owned `str` 的 `a = b` 走 `l.locals[name] = val`，**共用同一個 SSA 值**（⇒ 同一個 buffer、同一個 drop） | owned `str` 改走「拷貝進新值」路徑：發出 bitwise `OpMove` 到 fresh value，再由 `insertDrops` 依活性改成 `OpClone`（源仍活）或保留 move（源已死） |
| `src/mir/hir2mir.go` 新綁定（**v2.2**） | **owned option（`?str` / `?[]T`）被閘門排除**（`isOwnedLocal` 為 true）⇒ 兩個名字綁到同一個 SSA 值 | 閘門改為 `!isOwnedLocal(val) \|\| ownedStr \|\| ownedOpt`；`ownedOpt` 用**宣告型別**排除 option→slice 的剝離 |

owned `[]T` 本來就在新綁定直接降低成 `OpClone`；規則一只需把「只讀前向」移除即成立。

**可觀測缺陷（已修）**：

1. `s = 'hello'; c = s; c[0] = "H"` —— 舊版兩個名字綁到同一個 SSA 值 ⇒ 寫 `c` 連 `s` 一起改，
   實測印 `Hello` / `Hello`；新版印 `hello` / `Hello`。
2. （**v2.2**）`o ?str = '…'; p ?str = o; o = '…'` —— 舊版重綁 `o` 之後 **`p` 也跟著變**（兩個名字是同一個值）；
   新版 `p` 保留舊值。`?[]i64` 同形狀用長度觀測：舊版 `2`/`2`，新版 `3`/`2`。

**順帶修掉的缺陷：`emitOptionDrop` 的 `case "vec"` 是死碼。** `emitOptionDrop` 原本 `switch elemRaw`
（比對字面型別名），但 slice 的 `elemRaw` 是 `"[]i64"` / `"[]str"`……**永遠不會等於 `"vec"`**，
所以 inline `?[]T` 的 payload 一路落到 `default`，**從未被釋放**。實測（10 萬圈，`malloc_zone_statistics`
的 `blocks_in_use`）：

```
o ?[]i64 = [10,20,30]; p ?[]i64 = o   ×100000
  HEAD（777b9389）: 100050 塊   ← 每圈漏 1 塊
  改動後（E）     :     50 塊   ← 無洩漏
```

修法有兩處、**必須成對**：(1) `switch` 改比對 **payload LLVM 型別**（`%str-long` / `%vec`）而不是元素字串
——這也順帶讓型別別名（`MyStr = str`）走到正確分支；(2) `%vec` 分支在元素 owned 時走 `vecDeepFree`
（與 `emitDrop` 的 `%vec` 分支、以及新的深拷貝 helper 對齊）。**兩者互為鏡像**：
`emitOptionPayloadContentClone` 是 `emitOptionPayloadContentFree` 的鏡像、`vecDeepClone` 與 `vecDeepFree`
同進退——淺拷貝配深釋放會 double free，深拷貝配淺釋放會洩漏。

**`?[]T` 的深拷貝 helper（`emitOptionCloneHelper`）有兩個必須記住的細節**（第一版都踩過）：

- **`vecDeepClone` 吃的是「元素」型別，不是「切片」型別。** 傳切片本身的 TypeID 會讓它克隆
  `[]([]T)`——每個元素讀 24 位元組再遞迴——直接走出一段 `[]i64` buffer 之外。**症狀是 SIGSEGV**，
  而且**只有全語料編譯掃描／實際執行才看得到**（單元測試看不到）。用 `sliceElemTypeOfRaw(elemRaw)`
  取 `TypeMap[elemRaw]` 的 `.Elem`。
- **helper 名必須用「元素 raw」當鍵，不能用 payload 型別。** `?[]i64` 與 `?[]str` 的 payload 型別
  都是 `%vec`，用 `payloadLT` 當鍵會讓第二個元素型別**靜默沿用第一個的 clone**。
  （`optDropName` 用 payload 型別是對的——釋放 `%vec` 與元素無關。）

**map —— 已於 2026-10-02 收掉；但原文的診斷是錯的。** 原文（保留下方供對照）寫：

> `isOwnedLocal` 對 map 為 false，而 map 是**引用語意**——`Type.Owned` 為 false、從不被釋放——所以
> 「兩個名字共用一個 map」是它的定義，不是所有權缺陷。要收得先決定 map 的擁有權歸屬，不是補一個
> `OpClone` 能解決的。**刻意不做。**

三句都不成立：

| 原文的斷言 | 事實 |
|---|---|
| 「`Type.Owned` 對 map 為 false」 | **`true`**。`ClassifyOwnership("[str][]i64")` 走 `isMapRaw` 分支回 `true`（`mir.go:892`），`KindOfRaw` 亦回 `KindMap`（`mir.go:921`）。原文的依據不存在 |
| 「共用是 map 的**定義**」 | 共用是**可觀測的缺陷**。探針（下方）在修復前印 `107`，正解是 `1` |
| 「不是補一個 `OpClone` 能解決的」 | 對——**但也不必**。修法與 map 的擁有權歸屬無關，在 **`emitIndexStore` 的元素深拷貝**（§4.4 b） |

**真正的位置**：`hashmap-*-tmpl.put` 的 `.vals[idx] = val`（`map.no:102/128`），當 map 是 `[K][]T` 時就是
一次 **`%vec` 元素寫入**；而 `emitIndexStore` 只為 `%str-long` 元素深拷貝，把 `%vec` 元素留成**位元複製**
（原本的註解寫「there is no vec clone helper」——`vecDeepClone` 早已存在，只是沒接上）。
⇒ **缺陷的位置在元素寫入、症狀在 map**；一處修好，`map.put` 與 `[][]i64` 元素賦值同癒（§4.4 b）。

**最小重現**（`m` 是 `[str][]i64`）：

```no
list1 []i64 = [1, 2, 3]
m [str][]i64 = {}
m.put('k', list1)
list1 = [9, 9, 9]        ; 重綁 ⇒ 釋放 map 仍在用的 buffer
v ?[]i64 = m.get('k')
v: { ok(x) -> print(x[0])  nil -> print('nil')  err(e) -> print('err') }
```

| 版本 | 輸出 | 說明 |
|---|---|---|
| 控制組（**沒有**重綁那行） | `1` | buffer 還活著 ⇒ 兩版都對，**不具鑑別力** |
| 修復前（HEAD `e5b75772`） | `107` | 已釋放的 buffer 被重用；`107` 是 `'k'` 的位元組。**確定性**的錯值（不是隨機） |
| 修復後 | `1` | ✅ |

**🔴 落地後才浮現的迴歸：`vecDeepFree` 的深釋放無視元素的引用計數（已修）。**

inline `?[]T` 開始釋放 payload 之後，`tests/mem-safety/nested-container-clone.no` 立刻
`trace/BPT trap`（`MallocScribble=1` 下是 `pointer being freed was not allocated`）。最小重現是
`[][]str` 的 `a0 = a[0]`，**與 map 無關**。根因不在 option，而在**容器自己的深釋放**：

```
a [][]str ; a[0]  ← 借讀：retain(element) ⇒ element 的 buffer rc = 2
a 的 drop：__nolang_vec_free_12_0(a)
            每個元素呼叫 __nolang_vec_free_3_1(element)
              → 無條件 @str_free(element 內每個 str)   ← ★ 這裡
              → @nolang_free(element.data)             （rc 2→1，buffer 活著）
option 的 drop：__nolang_vec_free_3_0(element)
              → @str_free(同樣那些 str)  ← ★ 第二次 ⇒ double free
```

**retain 只保護 element 的「buffer」，沒有保護它的「內容」。** 容器深釋放把內容釋放掉了，
借用還活著；借用自己的深釋放再釋放一次 ⇒ double free。**判準應該是「內容的壽命 = buffer 的壽命」**：
釋放一個 element 的內容，只在這次釋放**真的把它的 buffer 收到 0** 時才成立。

修法（`codegen.go` `vecDeepFree`，`perElem != ""` 分支）把深釋放改成**引用計數感知**：

| 情況 | 行為 |
|---|---|
| 區塊沒有 magic header（不是本編譯器配置的） | **跳過**（I6）—— dangling 或外部指標 |
| 有 header，`rc > 1`（還有別的持有者） | 只 `@nolang_free`（release），**不動內容** |
| 有 header，`rc == 1`（唯一持有者） | 釋放內容 ＋ 釋放 buffer（＝原行為） |

`rc == 1` 涵蓋整個既有語料，所以**行為逐位元組不變**；只有被 retain 的借用元素改變。
「沒有 header ⇒ 跳過」同時是 I6 的落實（**永不 free 非本編譯器記憶體**）：所有 owned buffer 都出自
`@nolang_rc_alloc`（`@str_clone`、vec clone/alloc 全部走它；全檔唯一的裸 `@malloc` 就在
`@nolang_rc_alloc` 自己體內），所以沒有 header 的 `%vec` 只可能是**已釋放的**或外部的。

> ⚠️ **這個守衛把 map 的缺陷從「崩潰」降級為「洩漏」。** map 的 `put` 是
> `store %vec %lv330, ptr %ep363`——**位元複製描述子、不 retain**（見 `hashmap_*_put` 的 IR），
> 所以 map 的引用**不計數**：呼叫端的 `list1` drop 會把共用的 buffer 釋放掉，map 之後就 dangling。
> `m1.get()` 的 `vec_retain` 對已釋放的區塊是 no-op（magic 沒了），於是 option 的釋放會踩到
> dangling 指標。守衛讓它變成 no-op ⇒ `nested-container-clone.no` 恢復全綠。
>
> ✅ **2026-10-02 更新：真正的修法已落地**，但**不是**原文猜的「`put` retain 或 clone」，而是讓
> `emitIndexStore` 對 `%vec` 元素**深拷貝**（§4.4 b）。`put` 的 `.vals[idx] = val` 因此自己就拿到一份
> 私有 buffer，呼叫端重綁不再影響 map。**守衛仍然必要**——它守的是「借用的元素被容器深釋放」那條
> **獨立**的路徑（上面 `[][]str` 的例子），只是不再是 map 缺陷的唯一遮羞布。

**驗收（已過，2026-10-02）**：

| 檢查 | 結果 |
|---|---|
| `a = b` 之後對 `b` 的寫入／重綁**不得**影響 `a` | ✅ `tests/rule1-binding.no` 9 例：可觀測差異只有 `str` 獨立性（舊 `Hello`/`Hello` → 新 `hello`/`Hello`）與兩個 **option** 獨立性（舊 `another…`/`2` → 新 `a fairly…`/`3`），其餘逐位元組相同 |
| 單元測試在修復前**必須失敗** | ✅ `rule1_binding_test.go`：**6** 個 live-source 測試（4 個 `str` ＋ 2 個 option）在**修復前的控制樹 `a6c8559d`（HEAD 的父提交，引入所有權閘門之前）** FAIL（`clones=0`）；4 個 dead-source／剝離守門員兩邊都過。⚠️ **在 HEAD（`777b9389`）上 10 個全過**——HEAD 已含第一版閘門與 clone helper，只是 helper 有元素型別／鍵的 bug，**clone 計數看不到**（見下） |
| `tests/mem-safety/nested-container-clone.no` | ✅ 12/12，rc=0（修 `vecDeepFree` 之前：test 5 崩；只修一半：test 10 崩） |
| 語料 **515 檔編譯掃描** | ✅ **0 regression**：與控制組 `no-head777`（真 HEAD，`codegen.go` 與 `HEAD:src/mir/codegen.go` 逐位元組相同，且已含 `src/std/{heap,set}.no`）**515/515 檔結果完全相同**（509 過 / 6 敗）。敗的 6 檔兩邊同一組，全是既有的 `EmitLLVM: unknown callee …` 建置失敗（`map-key-leak` `m2.get`；其餘 5 檔 `str.init`），**與本次改動無關**；**無任何 `rc 0→1`** |
| `go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | ✅ 全綠（`go vet ./mir/` 亦乾淨——`c.fail` 的格式字串必須是常數） |
| `no test` | ✅ 改動後 **8 個 FAIL**（排序後集合）：`conn-init fd`、`expected nil`、`tests/mem-safety/{map-key-leak,map-tombstone,minimal-option-str,minimal-str-map,minimal-str-map2,option-str-match}.no`——與控制組**逐行相同**；**無 `nested-container-clone`** |
| `no vet src/std` | ✅ E 與 `no-head777` 讀數**逐位元組相同**：`0 error(s), 6205 warning(s), 936 hint(s)` |
| **洩漏（heapstat）** | ✅ `?[]i64` 10 萬圈：HEAD 100050 塊 → 改動後 50 塊；`?str` 兩邊皆 50 塊 |

> ⚠️ **`tests/rule1-binding.no` 的 `?[]i64` 例子在 HEAD 會當場崩潰。** HEAD 的
> `emitOptionCloneHelper` 有本節上述的兩個 bug（元素型別、helper 鍵），所以「`p = o` 深拷貝」在
> `?[]i64` 上走錯 stride ⇒ `no run` 報 `Error: signal: segmentation fault`（rc=1），**只印到
> `opt vec independence` 為止**；`?str` 例子沒有元素型別，HEAD 印得對。改動後 9 例全過、印出 `3`/`2`。
> **這正是單元測試抓不到的那一類**：clone 計數在 HEAD 上就已是 1（見上表），要**執行**才看得到記憶體錯誤。
> 所以「新測試要在**修復前**的控制樹上先失敗」與「**可觀測的端到端測試**」缺一不可——前者釘住 clone 的有無，
> 後者釘住 clone 的**正確性**。

> ⚠️ **這是行為改變，不是純重構。** 舊版 `a = b` 共用 buffer；改成深拷貝後會多一次配置。
> 判準因此是「**clone 只增不減、且每一筆增加都能對應到一條值對值賦值**」，不是「0 差異」。


#### (b) 參數側的 retain（源仍活時）—— **已落地為靜態閘門（⑤／⑥；機制見 §2.3b）**

**① 原文的前提是錯的（已更正）。** 原文寫「參數的 buffer 必須帶 header ⇒ 配置點要改走 `rc_alloc`，
即 §2.1 的『其餘 `@malloc` 站點』那筆帳」。那筆帳**已經結清**：header ABI 落地的是**全型別**版，
所有編譯器擁有的堆 buffer 都經 `@nolang_rc_alloc` 配置（`codegen.go` 全檔唯一的裸 `@malloc`
**呼叫**就在 `@nolang_rc_alloc` 自己體內）。所以參數的 buffer **今天就已經帶 header**，
`@nolang_rc_retain` / `@nolang_free`（＝release）可以直接作用在它上面。**這個前置條件不存在。**

**② 真正的障礙：retain 會把「快照」變成「共享」。** `run f(x)` 今天在 spawn 邊界深拷貝，所以任務
看到的是 **spawn 當下的快照**。若改成 retain（共用同一塊 buffer、靠 rc 續命），任務就會看到
**呼叫端在 spawn 與 `awy` 之間對 `x` 的原位寫入**。實測（探針語法同 `tests/async-arg-move.no`）：

```no
a []i64 = [1, 2, 3]
ta = run first-async(a)   ; first-async 讀 v[0]
a[0] = 99                 ; 原位寫入（不是重賦值）
print(awy ta)             ; 深拷貝 ⇒ 1    ／    retain ⇒ 99
```

**重賦值**（`a = [...]`）兩種做法都得到舊值（舊 buffer 靠 rc 活到任務釋放），所以
`tests/async-arg-move.no` 的第 6–8 例**無法區分**兩者——它們只釘住「源仍活 ⇒ 不得 move」。
真正會分歧的是**原位寫入**，而語料裡沒有任何一例測它。

**只有「原位寫入」這一種形狀會分歧。** 「spawn 之後呼叫端動到 `x`」共有三種形狀，另外兩種
**兩種做法結果完全相同**：

| 形狀 | 深拷貝 | retain |
|---|---|---|
| **原位寫入**（`x[i] = …`、`x.push(…)`） | 任務看到 spawn 當下的值 | 任務看到**寫入後**的值 ⇒ **分歧** |
| **重賦值**（`x = …`） | 任務看到舊值 | 呼叫端釋放把 rc 2→1，舊 buffer 活到任務釋放 ⇒ **同為舊值** |
| **離開作用域**（函式返回） | 任務看到舊值 | 同上，rc 只降到 1，**不懸空** ⇒ 同為舊值 |

⇒ 深拷貝與 retain 的分界**只有**「呼叫端是否在 spawn 之後原位寫入輸入參數」這一個問題。

**③ 這與使用者文檔直接衝突。** `docs/docs/lang/memory.md` 的「async 共享數據」一節明寫
「**跨協程共享可變堆數據在語言層不成立**」，而 retain 正是引入這種共享。⇒ **(b) 不是「還沒做」，
而是「照原樣做會改變語言語義」**；而且語料裡沒有任何一例測原位寫入，所以這個改變會是**靜默**的。
（⚠️ 這個「不共享」此前只寫在使用者文檔，模型文件沒有正式記錄 ⇒ 已補進 §6.3 第 5 條。）

**④ 範圍只到「輸入參數」——回傳值是 out param，且語言的呼叫模型就是「輸入唯讀、輸出可寫」。**

依 `docs/docs/lang/syntax.md`「函數定義」：

| 事實 | 內容 |
|---|---|
| **沒有「返回值」機制** | 結果一律透過**具名結果參數（out-param）**傳出，它是 out-parameter 的**語法糖**；接收變數由**呼叫處**決定（LHS 綁定 `a, b = swap(x, y)`，或尾隨引數 `add1(5, 3, res)`） |
| **輸入參數唯讀** | 複合型別傳**唯讀引用**；函數體內**禁止**寫入輸入參數及其子欄位 |
| **輸出參數可寫** | 呼叫方可把**既有變數**綁定到輸出槽，函數直接在該記憶體修改；也可不綁定，由函數生成新值交付 |
| **別名規則** | 輸入唯讀引用與輸出槽**可以指向同一物件**；只要寫只發生在輸出區、輸入僅讀取即為合法，**編譯器不做靜態別名檢查** |

⇒ 呼叫模型本來就是「**輸入共享唯讀引用、輸出被寫**」（這也解釋了同步呼叫為何共享：實測 `mut(a)` 印
`99`）。async 的深拷貝是這個模型在「被呼叫端活得比呼叫端框架久」時的一次**壽命偏離**，
而 retain 修的正是**同一個壽命問題**、且更貼近原本的呼叫模型——這是 (b) 值得一想的理由。
⚠️ 但「輸入唯讀」約束的是**被呼叫端**，不是呼叫端：呼叫端在 spawn 之後仍可寫自己的 `a`，
那正是 ② 裡唯一會分歧的形狀。

**async 的 `r` 在哪裡**：`emitAsyncRun` 與 `asyncArgKinds` 都用 `cf.ResultParams` 建 `isResult` 並
**`continue`** 掉這些位置；argbuf 多配置一個**結果槽位**（`numFields = len(argTypes) + 1`），
由 **wrapper** 把它的指標當 out param 傳進去，`awy` 再把結果取出。⇒ 對 async，`r` 的儲存是
**argbuf 自己的**（呼叫端在 spawn 時不傳 `r`），所以 **clone／free 協議只作用於輸入參數 `i`**。

**⑤ 重新界定：正確的形式是「可證明無原位寫入 ⇒ retain」的靜態閘門。** 綜合 ②（分歧只有一種形狀）
與 ④（只關於輸入參數）：

| 呼叫端在 spawn 之後對輸入參數 `x` | 降低 |
|---|---|
| **可證明沒有原位寫入** | **retain**（零拷貝；rc 續命 ⇒ 重賦值／離開作用域都安全） |
| 有原位寫入，或**無法證明** | 維持深拷貝（＝今天的行為） |

**這比 C 檔 CoW 更適合。** CoW 要在每一次寫入插「`rc > 1` ⇒ 先 clone」的**運行時**檢查；
閘門版只在 spawn 做一次**靜態**決定，而且**無法證明時退回今天的深拷貝** ⇒ 保守方向、**零風險**。
效果也一樣好：大多數「spawn 完就不再動它」的呼叫端直接零拷貝。

所以 (b) 的正確前置條件**不是** header（已有），而是**「呼叫端在 spawn 之後對 `x` 的寫入點」分析**
——正是 §3.1 tier 推斷要算的東西。（tier 推斷後來（2026-10-03）落地了，但**只當驗證器**、**不依 tier
改寫 IR**，所以它**不能**取代這裡的閘門——見 §4.2 c 的「循環依賴」說明。）**在沒有那個分析之前，深拷貝是唯一保持語義的
選擇**；無條件 retain 會讓「任務看到什麼」取決於呼叫端在 `awy` 之前寫了什麼，是**語義退化**而非優化。
（輸入參數的契約「不改」正是這個閘門想要**靜態化**的東西——與其在文件裡寫「不應該寫」，不如讓
分析證明它沒寫、然後省掉那份拷貝。）

**⑥ 落地（2026-10-02，本輪）——形式與 ⑤ 完全一致，但範圍刻意收窄。** 閘門已實作：

| 觸點 | 內容 |
|---|---|
| `analysis.go` `spawnArgWritesAfter(startBlk, startIdx, v)` | 從 spawn **之後**做前向 CFG 走訪（`state{b,idx}` seen-set ＋ **20 萬步**預算；**超預算 ⇒ 回 `true`**＝保守退回深拷貝），逐一問 `spawnArgUseWrites`；回邊目標從 index 0 重檢 |
| `analysis.go` `spawnArgUseWrites(inst, v)` | **白名單**：只有純讀的 op（`OpLoad/OpIndex/OpSliceOp/OpLen/OpCap/OpUtf8At/OpGetField/OpCast/OpPhi/比較與算術/…`）回 false；其餘（`Alloc/Store/Move/SetField/IndexStore/StructLit/OptionWrap/EnumNew/OpCall/OpRun/OpReturn`）一律當**可能寫**。**`OpMove` 刻意不在白名單**——move 會抑制呼叫端的 drop、破壞 retain 的配對（⇒ 洩漏） |
| `analysis.go` `spawnArgRetainSafe(kind, elem)` | **可共享的型別**：`str`（單一區塊）；`vec` 且元素**不擁有堆**（`!VecElemOwnsHeap`，即 `[]i64/[]f64/[]byte/POD struct`）。`[]str`/`[][]i64`（呼叫端 drop 是**深釋放** `@__nolang_vec_free_*`，會釋放任務仍要讀的元素 buffer）與 `struct`（遞迴 retain 不存在）**拒絕**，不近似 |
| `analysis.go` `VecElemOwnsHeap(elem)` | 丟棄式 codegen 呼叫 `c.vecElemNeedsDeepFree(elem)`（與 `SpawnArgClasses` 同慣用法）——**一謂詞一實作**，不重寫判準 |
| `mir.go` `Module.spawnArgRetains` | 與 `spawnArgMoves` 並列；`insertDrops` 決定、`emitAsyncRun` 讀回（**跨層單一決策點**，與 move 同紀律）。nil map ⇒ 深拷貝的安全預設；每次 `Analyze` 重新初始化（殘留項＝雙引用＝洩漏） |
| `codegen.go` `emitSpawnArgRetain` | 對共享參數補**第二個引用**：`str` ⇒ `@str_retain`、`vec` ⇒ `@vec_retain`（兩者的 `cap==0` 守衛讓借用視圖對稱 no-op）。`emitAsyncRun` 的 `share := moved \|\| retained` 取代原本的 `moved` 判準 |

**為什麼安全**：呼叫端**保留**自己的綁定與 drop；wrapper 的 `w_free` 釋放第二個引用；`@nolang_free`
是 **release**，**無論順序**都恰好在 rc==0 釋放一次。

**驗收（已過，2026-10-02）**：

| 檢查 | 結果 |
|---|---|
| 單元測試（`src/mir/spawn_arg_retain_test.go`，9 例） | ✅ 全 PASS。涵蓋：該共享（live 未寫 vec）／原位寫入退回深拷貝／owned 元素拒絕／struct 拒絕／源已死走 move（**move 優先**）／只讀**呼叫**拒絕／`print(a)`（inline len/index ⇒ 可共享）／型別契約表／白名單 |
| 端到端（`tests/async-arg-retain.no`，新增） | ✅ 4 例；觸發 **2** 次 retain；base 與 retain 輸出**逐位元組相同**（`7 8 ABC 65 1 190`）；5 次 rc 全 0（含 20 圈迴圈的計數平衡檢查） |
| 反向對照（**閘門關掉的 naive 版**） | ✅ **關鍵**：把 `spawnArgWritesAfter` 改成 `return false` ⇒ 原位寫入探針印 **99**（誤編譯——任務看到 spawn **後**的寫入），守衛版印 **1**；只讀探針三版皆 `1 2`。**證明寫入守衛是承重的**，差異可歸因於它 |
| 定點 A/B（語料中 **44** 個含 `run`/`awy` 的檔） | ✅ **0 行為差異**（比 `(rc, stdout sha256)`），且 **0 次閘門觸發**——既有語料沒有「live 但未寫」的 spawn 參數形狀 ⇒ 對既有程式是**可證明的 no-op** |
| 全量 golden A/B（**517** 檔） | ✅ base 與 retain 的 **DIVERGE(9)／REGRESS(6) 集合完全相同**；唯一 fingerprint 差異是 `tests/slice-heavy.no`。⚠️ **本行原本把該差異記為「ASLR 位址、不可歸因」，那是錯的**——見 §4.4 a：它是**真的 use-after-free**。「同 binary 跑三次給三個雜湊」是真的，但**成因判斷錯了**，而這個誤判讓一個真缺陷被當成噪音放過了 |

**⚠️ 已知限制（回報，不隱藏）**：這個特性對**既有語料零效果**（0 觸發）。它是**保語義的精化**，
好處目前只在合成／特定形狀的程式上量得到——與 §4.2(b) 早先的發現一致（「語料無任何一例測原位寫入」）。
效益要等真實程式出現「spawn 完就不再動參數」的形狀才會顯現。**其餘仍維持深拷貝**：`[]str`/`[][]i64`、
struct、以及任何**無法證明**無寫入的參數。

#### (c) `checkTierSoundness` ✅ 已落地（2026-10-03）；`checkReleaseTarget` ⬜ 仍不落地

**原判決（2026-10-02）**：前置 = 真實的 tier 推斷，而推斷當時對每個 use 回 S ⇒ 恆等映射 ⇒
拿它驗證「推斷是否可靠」是**自證**，恆真。本輪的解法是**同時解掉兩邊**，而不是繼續等。

| 動作 | 內容 |
|---|---|
| 填實 `tierConstraint`（`mir/tier.go`） | 三條**非平凡**來源：`OpRun` 的 dst 與 `spawnArgRetains` 的參數 ⇒ **R**；`OpTaskRetain`／`OpAwait` 的操作數 ⇒ **R**；其餘 ⇒ S |
| 填實 `tierStep` | (1) 指令局部約束；(2) **別名傳遞**：`OpMove`／`OpCast`／`OpPhi` 雙向 join（`OpClone` **不**傳——clone 就是為了斷開別名）；(3) **I2**：`OpSetField`／`OpIndexStore` 把值存進 R 容器 ⇒ 被存的值抬到 R |
| C 檔種子 | `writtenThroughSet`（`OpSetField`／`OpIndexStore` 的 `Args[0]`）＋ `isBorrowRead`——正是 §3.1 表列的「C 檔 CoW 有寫入點可查」 |
| `checkTierSoundness` 讀 **IR 的地面真相** | 不重跑推斷，而是**獨立**讀：spawn handle、`handleAliasSet` 的成員、`spawnArgRetains` 的參數、`OpTaskRetain`／`OpAwait` 的操作數——這些**必須**被推斷成 R；C 的種子必須落在 `writtenThroughSet`／`isBorrowRead` 上 |

**為什麼這不是自證**：驗證器與推斷**讀不同的東西**。推斷讀「我自己的約束表」，驗證器讀「IR 裡哪些值
真的是 R 物件」（spawn handle 的定義、`handleAliasSet` 的連通分量）。若 `tierConstraint` 退化成全 S，
`checkTierRSites` 立刻在 spawn handle 上報錯——**這是可證明會壞的**（`tier_test.go` 的
`TestCheckTierRSitesFiresOnIdentityPartition` 把約束釘成全 S 即觸發；`TestCheckTierPartitionFiresOnUnseededMap`
釘 C 的種子）。語料掃描（575 檔）**零** `tier-soundness` 假陽性。

**仍然沒有任何東西依 tier 改寫 IR，這是刻意的。** 要變成規定式，得把這個 pass 移到
`insertBorrowRetains`（借讀 retain）與 `emitAsyncRun`（spawn 的 retain／拷貝）**之前**——而
`tierConstraint` 的 R 列**讀回 `spawnArgRetains`**，那個決定是 `insertDrops` 寫的 ⇒
**把 pass 往前搬會形成循環依賴**。所以 tier 推斷今天只**驗證**已經由別處（借讀／spawn 閘門）做出的
決定，不自己下決定。`checkTierSoundness` 因此必須排在 `insertDrops` **之後**（`analysis.go` 的呼叫點有註記）。

**`checkReleaseTarget`（I5/I6）—— 仍不落地。** 它要守的是「`OpRelease` 的目標確實是被 retain 過的
那個值」。今天唯一會 retain/release 的是**借讀**（§2.2）與 **spawn 閘門**（§4.2 b ⑥），兩者的配對已由
`checkRefBalance` 與各自的單元測試釘住；對**尚未存在的 R 檔使用者值**寫檢查，沒有可檢查的對象。
⇒ 先寫空檢查比不寫更糟（它會讓人以為這一塊已經被守住了）。

### 4.3 已知殘留

**借讀 release 的死亡點精度 —— 本輪（2026-10-03，v2.7）複查：不存在，原條目是誤判，予以撤銷。**

原條目（v2.1 起）主張：借讀值被 option-wrap／enum 建構子**吃掉**時，引用隨容器走，release 落在
**剝離結果**上；那個結果的 drop 位置由（保守的）活性分析決定，**在迴圈裡會被提到迴圈外** ⇒ 每圈漏一塊
（`x = a[0]` 配 `#{index-out}` 的形狀）。**前半段是對的，後半段是錯的。**

**三條獨立證據**（都只用**現行樹**，且都可重跑）：

| # | 證據 | 指令 | 結果 |
|---|---|---|---|
| ① | **MIR 直讀** | `NOLANG_MIR_DUMP_MIR=1 no build x.no`（`x = a[0]` 在迴圈裡） | `index dst=21(owned) → retain args=[21] → option-wrap dst=22 args=[21] → move dst=0 args=[22 20]`；**剝離結果的 drop 就在迴圈內**（`drop dst=0 args=[29]` 落在迴圈的 some 臂，可回到迴圈頭）——**沒有被提到迴圈外** |
| ② | **端到端 RSS** | `/usr/bin/time -l ./probe.bin`，2,000,000 次迭代 | 8 個形狀（option-wrap／純臨時值／`y = x` clone／grow 路徑 `x.push`／三層嵌套／`[]str` 元素／跨迴圈逃逸／`#{index-out}` 直讀）**全部 1.7 MB 平線**。**正對照**：同一個 harness 跑已知的 map 洩漏（§4.3 下一條）**300,000 次迭代即 151 MB** ⇒ harness 偵測得到洩漏 |
| ③ | **語料逐值不對稱** | 691 檔（`tests` ＋ `test` ＋ `example` ＋ `src/std`，**全部**成功 lower） | `OpRetain=68` / `OpRelease=58` / `OpDrop=11151`；不對稱**只有 13 筆**，**全部**可解釋（下表） |

**為什麼 retain 是承重的、而「沒有配對的 `OpRelease`」是正確的。** `@nolang_free` 只**遞減** header 的
rc、**歸零才 free**。`a[0]`（配 `#{index-out}`）產生 **owned 深拷貝**（rc=1），`retain` 把它抬到 2，因為
**之後有兩次處置**：①`option-wrap` 出來的 option 被 drop ⇒ **深度釋放**（rc 2→1，**不 free**）；
②剝離結果的 drop ⇒ rc 1→0（**free**）。retain 與「option 的深釋放」正好配平；**再補一條 `OpRelease`
反而是 underflow**（提前 free）。這正是 `borrowReleaseValues` 的種子**只讀回 `OpRetain` 指令**、
且**故意不做 clone 閉包**的原因（`analysis.go` 的函式註解有完整推導）。

**13 筆殘餘的歸類**（前 3 筆 `borrowReleaseValues` 的註解已記載）：

| 檔案 | 筆數 | 形狀 | 配平方式 |
|---|---|---|---|
| `tests/mem-safety/nested-container-clone.no` | 10 | `retain → option-wrap`（無 move） | option 的**深釋放是 rc 感知的**（證據 ②），且剝離結果在迴圈內被 drop |
| `mem-safety/struct-field-{move-test,shallow-copy-bug,uaf-bug}.no` | 3 | `getfield → retain args=[24] → move dst=0 args=[24 23] → release args=[23]` | **release 落在 move 的目的地**（`borrowReleaseValues` 的 move 閉包） |

**沒有任何 `release > retain` 的真實案例。** 同一支掃描另報 3 筆「release 無 retain」（正是上表後 3 筆），
那是同一條 move 轉移的另一面，**不是 underflow**。

**「先證明它會壞」。** 把 `insertDrops` 的 start-drop 全部從迴圈內提到迴圈外（臨時 hack，**已回退**，
`grep -c TEMP-HACK == 0` ＋ diff-stat 回到原值）⇒ 新增的釘子
`TestBorrowInLoopIsDisposedEveryIteration` **FAIL**，且端到端 RSS 從 1.7 MB 漲到
**66 MB（`[]i64` 元素）/ 259 MB（`[]str` 元素）**。⇒ 原條目描述的**機制是真的**，只是**當前樹上不成立**。
釘子本身帶**負對照**（沒有迴圈攜帶累加器的同形狀必須**不**滿足該性質），所以它不是恆真斷言。

⚠️ **原文的「語料實測 1717 retain / 1693 release，未配對的 24 個」無法複現。** 同 roots、同 lowering
路徑（`lexer → parser → ASTToHIR → LowerHIR`）、同粒度（MIR 指令、逐值）得到的是 **68 / 58 / 13**。
原文未記測量方法，本輪改用上表 ③ 的可複現數字。**保留這一行是為了標示舊數字不可信，不是為了引用它。**

> **與 §4.4 a 的關係（原文的這段推理仍然有效，只是結論反了）。** §4.4 a 修的是「來源被視圖**提前**
> 釋放」（**過早釋放**，會讀到垃圾）；本節原以為是「release 被**延後**到迴圈外」（只洩漏）。兩者判準相反
> 這點是對的，所以 §4.4 a 的 `viewSrc` 確實**不影響**本節。**但本節的症狀從來沒有被觀測到**——
> 本輪的 MIR 直讀與 RSS 都指向「drop 就在迴圈內」。**教訓**：一個「只洩漏、不影響輸出」的殘留
> 最容易被記進文件而永不複驗；**沒有可複現測量的殘留不該寫進 §4.3**（見 §7 決策 17）。

**map 的儲存從不釋放（只洩漏，不懸空）**：`map.no` 沒有 `deinit`/`free`，`remove` 只標墓碑
（註解自承「不清 keys/vals」）。IR 直證：`m [str][]i64 = {}` 的 `alloca %hashmap_str_slice_i64` 有
`init`／`put`／`get`，**沒有任何 drop** ⇒ `keys`/`vals`/`occ` 三個 buffer 全部洩漏。與
`docs/docs/lang/memory.md:400`（「hashmap 未實現 key/value 的深層 free」）的既有記載一致。

⚠️ **這與 §4.4 b 是兩件不同的事，別把它們混為一談。** §4.4 b 修的是**懸空**（值被 map 與呼叫端共用，
呼叫端一重綁就 dangling ⇒ **記憶體不安全**）；本條是**洩漏**（map 自己擁有值了，但從不釋放 ⇒ 只是浪費）。
修法要給 map 一個真正的解構子，並決定 `remove` 是否釋放被刪項的 key/value——**與元素寫入無關**。

> **本輪（2026-10-03）複查：確認是「功能」而非「小修」，且一行都不能偷。** 重新用 `NOLANG_MIR_DUMP_MIR`
> 與 `NOLANG_MIR_DUMP_LL` 各證一次，並把**根因鏈**釘死（三段缺一不可）：
>
> | # | 位置 | 現況 | 後果 |
> |---|---|---|---|
> | ① 分類 | `mir.go` `StructOwnedLeafFieldIdxs`（`ty.Kind != KindStr` 就 `continue`） | 只看 `KindStr` 欄位，`%vec` 欄位**跳過** | `hashmap-*` 被判「不擁有堆」⇒ **根本不插 drop** |
> | ② 解構 | `codegen.go` `emitStructDropHelper`（`if fLT != "%str-long" { continue }`） | 只釋放 `str` leaf | 即使插了 drop，也**不會**碰 `keys`/`vals`/`occ` |
> | ③ 拷貝 | `codegen.go` `emitLeafFieldsCloneR`（同樣 `fLT != "%str-long"` 就 `continue`） | 只深拷貝 `str` leaf | 見下方「陷阱」 |
>
> **MIR 直證**（`probe = () { m [str]str = {} ; m.init() ; m.put('k','v') }`）：
> `structlit dst=17:hashmap-str-str(owned=false)`，之後**只有兩個 `str` 臨時值的 drop**，
> **`17` 本身沒有 drop**。**LLVM 直證**：`%v0.s = alloca %hashmap_str_str` 在函式結束時
> **沒有任何 `@vec_free`/`@str_free` 碰它**（只有 `hashmap_str_str_put` 內部那兩個 `str_free`）。
> 與本節上方、以及 `docs/docs/lang/memory.md:400` 的記載一致。
>
> 🔴 **陷阱：只做 ①＋② 會把「洩漏」升級成「double free」（記憶體不安全）。** 因為 struct 賦值
> （`b = a`）走的是 `emitLeafStructClone`→`emitLeafFieldsCloneR`，而**欄位讀取**（`x = a.v`）
> 也在多處各自 `@str_clone`（`emitGetField` 等，全檔十餘處硬寫 `%str-long`）。只加 drop 不加
> 這些 clone，兩個 struct 就共用同一條 vec buffer，**兩邊都 drop ⇒ double free**。
> 換句話說 ③ 不是可選項，① ② ③ 必須**同一次**落地，且要與所有 `%str-long` 硬寫點**對齊**。
> ⇒ 本輪**判定不落地**（與 §4.2 c 同級的理由：改動面廣、風險是「不安全」而非「浪費」，
> 且在並行 session 環境下無法安全地做全語料 A/B）。要做時，先把上面十餘處 `%str-long` 抽成
> 「owned leaf 依型別分派」的單一入口，再逐 leaf 型別（`str` → `vec` → `map`）放行。
>
> **v2.8 複查：洩漏量終於可以量到底了（原本被另一個 bug 遮蔽）。** 之前量到 200,000 圈就
> **SIGSEGV**，所以只能報「≈880 B/圈、200k 崩潰」。崩潰的原因**不是**洩漏，是 §4.6 第三列的
> 棧溢位（呼叫臨時變數的 `alloca` 在迴圈內）——**v2.8 修好之後，同一支程式不再崩**，洩漏因此可以
> 一路量到 800,000 圈：
>
> | 圈數 | 峰值 RSS | 說明 |
> |---|---|---|
> | 50,000 | 44.8 MB | |
> | 100,000 | 87.7 MB | |
> | 200,000 | **164 MB**（修好前：rc=139） | 棧溢位曾在此處截斷測量 |
> | 800,000 | **651 MB** | **線性成長，無上界** |
>
> 斜率 ≈ **814 B/圈**（一個被丟棄的 `[str]i64` 的三個 buffer）。**正／負對照都在**：
> `m [str]i64 = {}` ＋ `m.len()`（**從不 put**）跑 **1,000,000 圈仍是 1.69 MB 平線**
> ⇒ 洩漏**只在真的 put 過、buffer 真的被配置之後才發生**，`init()` 本身不是漏點。
> （可重跑：`/usr/bin/time -l` 量 `i <- [0..N) { m [str]i64 = {} ; m.put('key',1) }`。）
> ⚠️ v2.7 之前記的「沒用過的 fresh map 每圈 ~80 B」是**錯的**——那次量到的是棧增長，不是堆洩漏。
>
> **v2.9 複查：③ 目前連 map 自己都不成立——「map 賦值是淺拷貝」（規則一的洞）。** 這是**比洩漏更該先
> 處理**的一條，而且它讓 ① ② ③ 的「同一次落地」從紀律變成硬需求。探針（可重跑）：
>
> ```
> m  [str]str = {}      print(m.get('a'))    ; => yyy   ← 被 m2 的寫入改到了
> m.put('a', 'xxx')
> m2 [str]str = m
> m2.put('a', 'yyy')    print(m2.get('a'))  ; => yyy
> ```
>
> ⚠️ **只看 `m.len()` 會得到相反的結論**：同源探針 `m2 = m; m2.put('b',2)` 印 `1` 與 `2`
> （`size` 是純量欄位，按值複製）⇒ 曾因此誤判成「map 賦值已深拷貝」。**要用「寫入後回讀原物件」**
> 才測得出共享（`get` 才是，`len` 不是）。⇒ `cloneLeafStructKey` 對 `hashmap-*` 回 false
> （`StructHasOwnedLeafFields` 為 false）⇒ `emitClone` 落到 `emitMove` 的**逐位元搬移** ⇒
> `keys`/`vals`/`occ` 三個 buffer **共享**。這同時是**規則一（§1.1）的未完成項**：值對值賦值
> 對「含 `[]T` 欄位的 struct」目前不是深拷貝。
>
> **v2.9 複查：把「不落地」從判斷變成有數字。** `StructHasOwnedLeafFields` 在 `analysis.go`／
> `codegen.go`／`hir2mir.go` 有 **~25 個呼叫點**（drop 插入、零初始化、clone、move 分類、
> option payload、巢狀遞迴……），**放寬 `StructOwnedLeafFieldIdxs` 會把它們全部翻面**。語料實測
> （全 691 檔，暫時插樁後全數移除）：
> **36 個 struct 型別、52 個檔案**帶有 owned `[]T` 欄位——**不只有 `hashmap-*`**：
>
> `json.json-pool`／`yaml.yaml-parser`／`yaml.yaml-pool`／`tls.conn`／`tls.server-conn`／
> `bigint.bigint`／`bufio.reader`／`gzip.bit-reader`／`pem.pem-block`／`deque.deque`／
> `stack.stack`／`heap-i64`／`heap-f64`／`set-i64`／`set-str`／`process.cmdopts`／
> `hashmap-{str,i32,u8,bool}-*`（8 種）……
>
> 欄位型別含 `[]i64`／`[]str`／`[]byte`／`[]bool`／`[]f64`／`[]i32`／`[]u8`／`[][]str`／
> `[][]i64`／`[]json.json-value`／`[]yaml.yaml-node`。⇒ **這不是「給 map 加個解構子」，
> 是「讓一整類 struct 開始有解構子」**，且 `emitStructDropHelper` 的註解自承：owned leaf 的釋放
> 還要「欄位**讀取**也給出 clone 而不是別名」才安全（即 `projectionWasCloned` 那套，§4.5）。
> **維持不落地**；要做時的順序是 ③（clone，含欄位讀取）→ ②（drop）→ ①（分類），
> 且**逐 struct 型別放行**（先 `hashmap-*`，再 `set-*`/`heap-*`，最後 `json`/`yaml` 的 pool 型別）。

### 4.4 上一輪（2026-10-02）修的兩個可觀測缺陷

兩者都**不是合成探針**——既有語料／既有形狀就能觀測到——且都**與規則一無關**：它們是在規則一那輪把
`vecDeepFree` 改成引用計數感知、並讓 option payload **真的開始釋放**之後，**才浮現或才被看見**的。

#### (a) 切片視圖的來源被提前釋放 —— `Module.viewSrc`

**症狀**：`b = a[i..j]` 之後讀 `b`，讀到**已釋放的記憶體**。

```no
a = [10, 20, 30, 40, 50]
b = a[2..4]
print(b)          ; 修復前：[4341880592, 4341880608, ...]（堆位址，每次不同）
                  ; 修復後：[30, 40, 50]
```

**根因**：`b = a[i..j]` 對 `[]T` 產生**視圖**——`b.cap == 0`、`b.data == a.data + i*8`，即 b 別名 a 的
buffer。但活性分析只看得到 `b`、**看不到 `a`**，於是 `a` 的 drop 被放在 `a` 自己的最後一次使用
（早於視圖的），之後透過 `b` 讀到的就是已釋放的記憶體。

**修法**：新增 `Module.viewSrc`（`Analyze` 由**仍帶 `SliceFlagView`** 的 `OpSliceOp` 建立，沿轉移鏈遞迴
解析），`defUse` 把「使用視圖」算成「使用來源」⇒ 來源的活性延伸到視圖的最後一次使用。

| 觸點 | 內容 |
|---|---|
| `mir.go` `Module.viewSrc` | `map[ValueID]ValueID`（視圖 → 來源）。nil ⇒ 既有行為；每次 `Analyze` 重建（殘留項＝釘住已死的來源） |
| `analysis.go` `buildViewSrc` | 收集 `direct[dst] = Args[0]`，再**有界（64 跳）**傳遞解析成 `resolved`；自環跳過 |
| `cfg.go` `defUse` | 每個 arg 沿 `viewSrc` 鏈（≤64 跳）逐一標記為 use |

**與 `demoteUnsafeSliceViews` 的關係**：後者在 `hir2mir.go` **早於 `Analyze()`** 執行，會把「逃出框架／
來源之後被覆寫」的視圖之 `SliceFlagView` 位元清掉。`viewSrc` 建在 `Analyze` 內、**只收位元還在的**，
所以兩者天然一致——`TestViewSrcMatchesSurvivingViewBit` 就釘這條不變式（entry 存在 ⇔ 位元仍在）。

**驗收**：

| 檢查 | 結果 |
|---|---|
| 端到端 `tests/slice-view-liveness.no`（新增，5 例） | ✅ 3 次執行**逐位元組相同**（`[30, 40, 50]` / `[1, 2]` / `8` `9` / `1 2 3 4` / `[30, 40]`） |
| **修復前必須失敗** | ✅ 同檔在 HEAD binary 上印 `[4307408912, 4307408928, 4307408944]`——**堆位址，且三次三個值** |
| 單元測試 `view_liveness_test.go`（3 例） | ✅ 全過，**10 次重跑 10 次過**。⚠️ 首版**不穩定**（4/5 失敗）：它把 `drop <view>` 當成 `<view>` 的一次「使用」，斷言於是退化成「**兩個 drop 誰先誰後**」，而那是 `insertDrops` 迭代 Go map 決定的。**修法：掃描 use 時排除 `OpDrop`** |
| 單元測試**修復前必須失敗** | ✅ 把 `buildViewSrc` 改成 `return nil` ⇒ `viewSrc[15] = 0, want 2` |
| 全量 golden A/B（**517** 檔，`/tmp/no_vs` vs HEAD binary） | ✅ **只有 1 檔改變**：`tests/slice-heavy.no`；新指紋 `2144cf54…` **與凍結金標逐位元組相同** ⇒ 修好了一個既有的 `DIVERGE`（見下方更正） |

> 🔴 **同時更正一個錯誤的歷史結論。** §4.2 b ⑥ 原本把 `tests/slice-heavy.no` 的金標差異記為
> 「**ASLR 位址、不可歸因於本次改動**」。**那是錯的。** 該檔印出堆位址**不是**因為 ASLR，而是因為它
> 讀的是**已釋放的** buffer——讀到的頭部 magic `7957698236827265`（`0x6E6F6C616E670001`）就是鐵證：
>
> ```
> 修復前 /tmp/no_base：      [0, 7957698236827265, 8236, 0]        ← a[2..5]
>                            [4383792448, 4383792512, 4383792528]  ← v[1..3]
> 修復後 /tmp/no_vec：       [30, 40, 50, 0]  /  [2, 3, 4]          ← 與凍結金標相同
> ```
>
> 「同 binary 重跑三次三個雜湊」是真的，但**成因是「釋放後的區塊被不同配置重用」**——那正是
> use-after-free 的指紋，不是不確定性。**教訓已寫進 §5 的操作紀律。**

#### (b) `%vec` 元素賦值的淺拷貝 —— `emitIndexStore`（一併修好 map）

**症狀**：

```no
outer [][]i64 = with-len(1)
inner []i64 = [1, 2, 3]
outer[0] = inner
inner = [9, 9, 9]
print(outer[0][0])   ; 修復前：印 0 然後 `signal: trace/BPT trap`（rc=1）；修復後：1
```

**根因**：`emitIndexStore` **已經**為 `%str-long` 元素深拷貝（`@str_clone`），卻把 `%vec` 元素留成
**位元複製**——原註解寫「there is no vec clone helper」，但 `vecDeepClone` 早在規則一那輪就為 `?[]T`
寫好了。**一句過期的註解 ⇒ 一條已存在的修法沒有接上。**

**為什麼這同時是 map 的缺陷**：`hashmap-*-tmpl.put` 的 `.vals[idx] = val`（`map.no:102/128`）對 `[K][]T`
就是一次 `%vec` 元素寫入（§4.2 a 的探針）。修在元素寫入，map 就自己擁有私有 buffer。

**修法**：在 `%str-long` 分支之後補一個對稱的 `%vec` 分支，`vecDeepClone(vt.Elem, 0)`——**傳「切片」的
元素型別**（`[]i64` → `i64`），**不是**切片型別本身（傳錯會克隆 `[][]i64`、每元素走 24 位元組走出 8 位元組
的 buffer ⇒ SIGSEGV；與 §4.2 a 的 `?[]T` helper 踩的是同一個坑）。元素型別優先取自**被寫入的值**
（`mirTypeOfValue(Args[2])`），退回**接收者的元素**（`[][]T` → `[]T` → `T`）。

**驗收**：

| 檢查 | 修復前 | 修復後 |
|---|---|---|
| `[][]i64` 元素賦值（上方） | `0` ＋ `trace/BPT trap`（rc=1） | `1` ✅ |
| map `put` ＋ 重綁（§4.2 a 探針） | `107` | `1` ✅ |
| **IR 直證**（`NOLANG_MIR_DUMP_LL=1`，`hashmap_str_slice_i64_put` 內） | `call %vec @__nolang_vec_clone_8_0` **0** 次 | **2** 次——正是 `map.no` 的兩個 `.vals[idx] = val` 站點（`put` 的更新路徑與插入路徑） |
| map 控制組（**不**重綁） | `1` | `1`（兩版皆對，**不具鑑別力**） |
| `vec.push`（對照：早已深拷貝） | `1` | `1` |
| 端到端 `tests/vec-elem-ownership.no`（新增，5 例） | `0` ＋ `trace/BPT trap`（rc=1） | `1 1 7 2 1 5`（rc=0），3 次逐位元組相同 ✅ |
| `tests/slice-heavy.no` | golden `DIVERGE` | `2144cf54…` ✅ |
| `go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | — | ✅ 全綠 |
| 全量 golden（**517** 檔 ＋ 新增 2 檔） | — | 見 §5 量測基準 |

**⚠️ 這也會增加配置**：每次 `%vec` 元素寫入多一次 malloc ＋ memcpy（`cap` 設為 `len`）。判準與規則一
相同——「**clone 只增不減，且每一筆增加都能對應到一條元素寫入**」，不是「0 差異」。

### 4.5 本輪（2026-10-03）修的兩個投影缺陷

兩者都由**使用者回報的症狀**引出（`str.trim()` 的結果**內容對、長度為 0**），根因都在**接收者投影**
（`lvalueAddrOf` 那一族，§1.2），**與規則一無關**：它們是「投影」這條線在 v2.4 只寫了第一層
失效條件（來源已 drop）之後剩下的兩個缺口。

#### (a) 重新綁定之後投影仍然生效 —— `defCount` / `definedValue`

```no
line = lines[0]          ; OpIndex 定義 line
line = line.trim()       ; OpMove **重新定義同一個 value id**
print(line)              ; 內容正確
print(line.len())        ; 修復前：0（或舊長度）；修復後：正確長度
```

**根因**：**MIR 的 value id 是「變數」，不是 SSA 名。** 同一條 value 可以被多條指令定義，而
「寫入既有綁定」的 move 用的是**另一種編碼**（`Dst = NoVal`、`Args = [src, dst]`）。
`codegen.defInst` 的建表迴圈只記 `inst.Dst > NoVal` ⇒ **move 隱形** ⇒ `defInst[line]` 永遠停在
`OpIndex` ⇒ `lvalueAddrOf` 把**重新綁定之後**的每一次使用都投影回**舊的容器元素**
（`%epN = getelementptr …, %edpM, i64 %i`）。`trim` 的結果寫進 **out-param**（不是接收者），
所以 `lines[0]` 根本沒變；`line.len()` 卻讀 `&lines[0]` ⇒ **內容對、長度舊**。容器 drop 落地之後
就是 use-after-free ⇒ 讀到 `0`（正是使用者看到的數字；換個檔案佈局會讀到舊長度）。

**修法**（`src/mir/`）：

| 觸點 | 內容 |
|---|---|
| `analysis.go` `definedValue(inst)` | 跨**兩種** move 編碼取「這條指令定義了誰」（`Dst`，或 `moveDst(inst)`）；放在 `moveSrc`／`moveDst`／`moveLike` 旁 |
| `codegen.go` `defCount map[ValueID]int` | 建表迴圈改成 `defCount[d]++; if > 1 { delete(defInst, d) } else { defInst[d] = iid }` ⇒ **一個 value 被定義第二次就撤回投影**，退回自己的槽位 |

**語義**：投影只描述**第一個**定義的儲存位置。`move dst=N args=[src]`（fresh 編碼）本來就無投影
（`lvalueAddrOf` 的 switch 沒有 `OpMove` case）⇒ 修法只是讓**兩種 move 編碼行為一致**。
**保守性：只會減少投影，不會新增。**

**驗收**：

| 檢查 | 結果 |
|---|---|
| 端到端 `tests/rebind-retracts-view.no`（新增） | 修復前 `fail 1 … got=0` ＋ fail 5／6；修復後 **9/9 ok** |
| 單元測試 `rebind_retracts_projection_test.go`（新增） | 斷言 **IR 的接收者運算元**：修復前 `@str_plen receiver = %ep62`（element 投影）／`%lvg11`（field 投影）⇒ FAIL；修復後 `%vN.s` ⇒ PASS（`TestReboundElementViewIsNotProjected`／`TestReboundFieldViewIsNotProjected`） |
| 對照組（**必須仍**投影） | ✅ `TestSingleDefinitionElementViewStillProjects`、`tests/set-byte-receiver-writeback.no` 7/7、`tests/len-assign-grow.no` 5/5 |
| 全語料 A/B（**480** 檔；控制組 = 改動前**同一棵樹**的 `make no` 產物） | **465 逐位元組相同**；15 個差異**全為環境噪音**（12 × `compiler version mismatch`、2 × 隨機 temp 路徑、1 × pid）⇒ **0 行為回歸** |

> 🔴 **「先證明它會壞」的陷阱：臨時 kill switch 必須忠實重放原行為。** 第一版寫成
> `if defCount > 1 && !kill { delete } else { defInst[d] = iid }` ⇒ 語義變成「**最後**定義勝」
> ⇒ 探針的接收者仍是 `%vN.s`，**測試假通過**（先被誤判成「探針形狀不對」，查了 MIR／LLVM 才發現
> 是開關不忠實）。教訓與 §5「驗證器要守的東西必須先真的會壞」同源：**控制組要重放原行為，
> 不是「把新行為關掉」**——兩者在不變的舊分支上等價，在「新舊都寫入」的分支上不等價。

#### (b) 被投影的路徑被寫入 —— `sourceWrittenBetween`（`OpIndex` 原本沒有護欄）

```no
lines = ['aaaa'] ; line = lines[0]
lines = ['bb']        ; 重綁容器
print(line.len())     ; 修復前：SIGTRAP（舊 buffer 已 drop，是 use-after-free）；修復後：2

h.f = '  hi  ' ; g = h.f
h.f = 'zzzz'          ; 重綁欄位（**整支程式沒有任何 drop**）
print(g.len())        ; 修復前：4（內容仍是 '  hi  '）；修復後：6
```

**根因**：投影 `&container[i]`／`&root.field` 描述的是**儲存位置**，只有「**那條路徑上沒人寫過**」
時才還代表同一個值。兩種寫入形狀：容器重綁 ⇒ `move args=[new, ls]` 寫進鏈上的 `ls`
（舊 buffer 同時被 drop）；欄位重綁 ⇒ `setfield args=[h, newval]` 寫進鏈上的 `h`——
**後者整支程式沒有任何 drop，所以 `sourceAlreadyDropped` 抓不到**，必須另立「寫入」這條守則。

**修法**（`src/mir/codegen.go`）：

| 觸點 | 內容 |
|---|---|
| `projectionChain(read)` | 把原本內嵌在 `sourceAlreadyDropped` 裡的**鏈走訪**抽出，兩條守則共用 |
| `reachableBlocks(f, from)` | 前向可達區塊（兩條守則共用） |
| `sourceWrittenBetween(read, use)` | 掃可達區塊找**寫入鏈上任一 value** 的指令（`OpSetField`／`OpIndexStore` 的 `Args[0]`、move-like 的 `moveDst`），位置必須**嚴格落在** read 與 use 之間 |
| `getFieldWasCloned` → **`projectionWasCloned`** | 它其實是「這個讀取產生了獨立的 owned `%str-long`」，field 與 index 兩種形狀共用 |
| `sourceAlreadyDropped \|\| sourceWrittenBetween` | **同時掛到 `OpGetField` 與 `OpIndex` 兩分支**（`OpIndex` 原本**完全沒有護欄** ⇒ 這就是 `SIGTRAP` 的來源） |

🔴 **「嚴格」是關鍵**：**寫入本身就是 use** 的那些（`b.s.len = 3`、`b.s[0] = 97`、
`b.s.set-byte(…)`）正是投影存在的理由——把它們算進「read 與 use 之間」就會讓**寫回變成寫副本**。
🔴 **前提是 `projectionWasCloned(v)`**：讀取有 clone ⇒ 自己的槽是完整私有值，退回它永遠安全。
這也是為什麼**不能**把守則套到非 clone 的欄位（`x = o.c; x.n = 7` 必須繼續投影）。

**驗收**：

| 檢查 | 結果 |
|---|---|
| 端到端（`tests/rebind-retracts-view.no` case 8／9） | 控制組（只含 (a)）case 8 **SIGTRAP**、case 9 印 `fail 9 … got=4`；修復後 **9/9 ok** |
| 單元測試 | `TestReboundContainerViewIsNotProjected`／`TestReboundFieldWriteViewIsNotProjected`：關掉守則後接收者分別是 `%ep109`／`%lvg14` ⇒ FAIL；修復後 `%vN.s` ⇒ PASS。**case 9 的探針全程式沒有任何 drop** ⇒ 單獨釘住「寫入」這條 |
| 全語料 A/B（**481** 檔；控制組 = 只含 (a) 的 `./bin/no`） | **465 逐位元組相同**、15 個已知環境噪音、**唯一差異就是自己的新 case 8／9**（控制組 SIGTRAP）⇒ **0 非預期回歸** |
| `go test ./mir/ ./parser/ ./hir/ ./checker/ ./fmt/ ./lexer/` ＋ `no vet src/std` | ✅ 全綠／**0 error**；gofmt 乾淨 |

#### (c) 同族但**刻意未收**（回報，不隱藏）

- **map 的儲存從不釋放**（§4.3）——本輪再次確認是「功能」而非小修（三段根因 ＋ 十餘處 `%str-long`
  硬寫點），**判定不落地**。
- **tier 推斷落地**（§4.2 c 的前置）——**已落地（2026-10-03，v2.6）**：`tierConstraint` 不再對每個 use
  回 S，且 `checkTierSoundness` 從 IR 側獨立驗證（§3.3）。**但仍不依 tier 改寫 IR**（循環依賴，見 §4.2 c）。
- **借讀 release 的死亡點精度**（§4.3）——**v2.7 撤銷**：三條獨立證據（MIR 直讀／8 形狀 × 2M 迭代
  RSS 全平線／691 檔語料逐值不對稱僅 13 筆且全部可解釋）證偽「drop 被提到迴圈外」。
  **不再是殘留**；新增的釘子 `TestBorrowInLoopIsDisposedEveryIteration` 守住它（並已證明會壞）。
- **`%vec` 元素賦值多一次 malloc**（§4.4 b）——是**正確性的代價**（深拷貝必要），不是缺陷。

### 4.6 本輪的三個非所有權改動（記錄，供追溯）

不屬本模型的範圍，但同輪落地、**都會改變程式輸出**，故記在這裡以免日後誤判為迴歸。

| 改動 | 症狀／內容 | 修法 | 驗證 |
|---|---|---|---|
| **單字元 `str` 字面量 ＋ `char` 字面量的位元組折疊** | `'x' - "a"` **靜默**得整數 `23`（`120-97`）、`'x' + "a"` 得 `217`；使用者要的是拼接 `"xa"`。`'xy' - "a"`（多字元）**已經**正確 ⇒ 只有「單字元 `'…'` 折位元組」那條路徑錯 | `hir2mir.go` 的 `+`/`-` 折疊條件加 `&& !charLitSibling(i)`——**只**豁免兄弟是 `"…"` **char 字面量節點**（`hir.KCharLit`）。🔴 **不可**豁免「char 型別的值」：那會讓 `c - '0'`（數字字元取值慣用法）從 `5` 變成拼接 `"50"`（**第一版就踩，實測回歸**） | 新 `src/mir/str_lit_char_concat_test.go`（控制組 FAIL）；`tests/str-ops.no` 輸出**逐位元組相同**；全語料編譯掃描 468 檔 **0 FAIL**。⚠️ 殘留不對稱（已知未解）：`'x' - "a"` = `"xa"` 但 `'x' - c`（`c` 是 `"a"` 變數）= `23` |
| **`go` 關鍵字的無色協程轉寫** | `r = go f(x)` 自動單態 `f-async`（`hir.MonomorphizeGo`）；陳述層級 desugar 成 `run`＋`awy`（`hir.DesugarGoInlineAwait`）；協程組（裸 `{}`）內 `go` 並發 spawn。殺手開關 `NOLANG_ASYNC_GO=0` | 順帶修兩個編譯器缺陷：checker 缺 `*parser.GoExpression` 的型別推斷（`go` 預設 `i64`）；模組層級 `go` 的 `insertTopBefore`（handle 從未進程式 ⇒ `awy` 等一個不存在的 handle ⇒ `trace/BPT trap`） | 🔴 **對本模型的意義：`go` 只是 `run`/`awy` 的語法糖 ⇒ §2.3／§2.3b／§2.4 的邊界協議逐字不變**（move／retain 閘門／`%task` 計數全走同一條路徑）。8 個 `go` 檔全 `rc=0`；核心套件全 ok |
| **呼叫臨時變數的 `alloca` 落在迴圈內 ⇒ 棧溢位**（v2.8） | `i <- [0..200000) { m.put('key', 1) }` **SIGSEGV（rc=139）**，峰值 RSS 卻只有 15.6 MB（**棧**溢位不是堆耗盡）。門檻約 160k 圈過、180k 圈崩。macOS crash report：`EXC_BAD_ACCESS / KERN_PROTECTION_FAILURE`、`Thread stack size exceeded`、幀 `_xzm_xzone_malloc_tiny → hashmap_str_i64_put → main` | **根因**：LLVM 的 `alloca` 是**指令**不是宣告——每次執行都保留棧空間，**只在函式返回時回收**。呼叫臨時變數（`%cargN` 引數暫存、`%cresN` 出參暫存）是在**呼叫點**發的，所以落在迴圈體裡就每圈增長一次。`-O2` 後 IR 仍留著 `%carg12 = alloca %str-long`／`%cres14 = alloca i1` 在 `bb5`：**LICM 不提 `alloca`，mem2reg/SROA 也無法提升不在 entry block 的 `alloca`**。被 inline 掉的呼叫沒事（`f()` 300k 圈正常），所以只有**呼叫沒被 inline 的函式**才會中。<br>**修法**：`emitFunc` 把函式體寫進暫存 builder（`codegen.sb` 改成 `*strings.Builder` 以便逐函式重導），結束時 `hoistEntryAllocas` 把**靜態** `alloca` 搬進 entry block；**動態** `alloca i8, i64 %n` 留在原地（人數運算元可能是後面 block 定義的 register，entry 不支配它 ⇒ 搬了就是非法 IR） | 修好後同一支程式 **rc=0**（`m.len()` 印 `1` ⇒ `put` 有去重）。**副作用**：解除遮蔽了 map 洩漏，現在可量到 800k 圈／651 MB 線性成長（§4.3）。新釘子 `src/mir/entry_alloca_test.go`（3 個測試；**負對照**：動態 alloca 必須**不**搬；**已證明會壞**：關掉 hoist ⇒ `TestCallTemporariesAreAllocatedInTheEntryBlock` FAIL，報 `%cres11 = alloca i64`） |

---

## 5. 驗證矩陣與操作紀律

| 層次 | 手段 | 判準 |
|---|---|---|
| 單元 | `cd src && go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | ok |
| 端到端 | `GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default bash scripts/mir_golden.sh` | SAME 不減、REGRESS=0 |
| 純重構 | `fp.txt` 前後逐檔 diff `(rc, sha256)` | 「N 檔中 0 檔變動」 |
| **編譯掃描** | 只比 `no build` 的**成功／失敗**（不執行程式）；**設計上就是 no-op** 的改動再逐檔比 `sha256(stderr)` | 全語料 0 差異；**驗證器的假陽性就用這個抓**（tier 落地實測 **575/575**，`rc` 與 `stderr` 皆相同） |
| 洩漏 | C 檔 `__attribute__((destructor))` 印 `malloc_zone_statistics` 的 `blocks_in_use`（`-O0`） | 大負載下有界 |
| 診斷 | `no vet src/std` | 0 error |
| 標準庫 | `no test` | FAIL 集合逐行不變 |

**量測與掃描的硬性陷阱**（每一條都踩過）：

- ⚠️ **一次只跑一個金標掃描**（`WORKDIR` 是硬編碼），並行會產生**假 REGRESS**；
  掃描前後各記一次 `ls -l bin/no`，中途被別的 session 重建 ⇒ 整場作廢。
- ⚠️ **`fp.txt` 必須 `sort` 後才 diff**（多 job 並行 `>>` ⇒ 行序不定）。
- 🔴 **永遠不要 hash MIR dump 做 A/B**：`insertDrops` 迭代 Go **map** ⇒ 同一份 binary、同一個輸入，
  **連跑 5 次得 5 個不同 sha**（而建置出的程式 stdout 每次相同）。要比就比 **clone 數**（順序無關）
  或**程式 stdout**。曾因此讓 94 檔報 91 檔「改變」，而多數檔 `forwarded=0` 根本沒觸發。
- ⚠️ **`/usr/bin/time -l <no> run x.no` 量到的是編譯器的 RSS**（~169 MB）。要量程式本身必須
  `no build -o /tmp/x x.no` 再量 `/tmp/x`。
- 🔴 **`make no` 會把其他 session 未提交的工作烘進 binary** ⇒ `bin/no` 不能當 A/B 的一邊；
  **「HEAD」也不是控制組**（並行 session 常態在場）。標準做法：**同一次 `rsync` 快照**成兩份，
  只在一份回退**自己的** hunk，各自 build；同一檔常混有別人的 hunk ⇒ **逐 hunk 篩**。
  ✅ 控制組**不必是 HEAD**：把新函式的第一行改成 `return false` 就是忠實控制組。
  ✅ **反向對照最有價值**：把守則整批換成 `return true`（naive）⇒ 負向探針**真的 miscompile**；
  若 naive 在**合法**情形上也與控制組一致，就證明差異全來自守則、不是 naive 沒生效。
- ⚠️ 改 `src/std/**` 後必須 `touch src/std_embed.go && make no`（**連改註解都算**）。
- 🔴 **自製 A/B harness 的兩個陷阱**：`$$` 在背景子 shell 裡仍是**父 shell 的 pid** ⇒ 多個 worker
  共用同一個輸出檔、互相覆寫後各自 hash ⇒ **會製造假差異**；`perl -e 'exit($? >> 8)'` 把**信號死亡**
  回報成 `rc=0` ⇒ 掃描表上的 SIGSEGV 顯示 `rc=0`（但 stdout 的 sha 仍不同，差異不會漏）。
- 🔴 **驗證器要守的東西必須先真的會壞。** 順序是「先找到／造出缺陷 → 再寫守它的驗證器 →
  附『修復前必須失敗』的測試」；反過來會寫出恆真的空檢查。
  **但如果被驗的 pass 本身就是恆等映射，先修 pass、再讓驗證器讀「另一側」的來源**（§3.3 的
  `checkTierSoundness`：推斷讀約束表，驗證器讀 IR 的 R 物件地面真相）——**兩個來源才可否證**；
  舊的恆等映射要當**負對照**（餵它進去必須開火），不是當契約去釘。
- 🔴 **臨時 kill switch 必須「忠實重放原行為」，不是「把新行為關掉」。** 第一版寫成
  `if cond && !kill { A } else { B }` 會**連帶改變語義**（變成「最後定義勝」）⇒ 控制組與新版的
  **接收者運算元相同** ⇒ **測試假通過**，還會讓你誤判成「探針形狀不對」而走錯方向（§4.5 a）。
  正確寫法是把整段換成**原行為的那一段**（`defInst[d] = iid`），而不是在新邏輯外掛一個開關。
- ⚠️ **`bin/no` 會被別的 session 用「沒有 ldflags」的 `go build` 覆蓋**（14.3 MB → 19.1 MB、
  `version` 變 `dev`）⇒ 語料 A/B 會多出一批 `compiler version mismatch` 噪音。要用就 `make no`
  重建並**核對 size／version**（`rm -f bin/no && make no`）；更穩的做法是 `go build -o /tmp/no_<x> ./cmd/no`
  自建快照。
- ⚠️ **`gofmt -w` 前先確認該檔在 HEAD 乾不乾淨**（`mir/hir2mir.go`、`mir/mir.go` 在 HEAD 就不乾淨）。
- 🔴 **`no vet src/std` 的 hint 數會抖**：同一個 binary、同一個目錄，連跑 6 次得 5 次 **936**、1 次 **938**
  （差的是 `src/std/fs.no` 的 2 個 `tcpoxtfd` hint）。**error 數恆為 0**，所以判準只能用 error；
  **不要用 hint/warning 的總數做 A/B**（會製造假差異）。診斷：`<no> vet src/std 2>&1 | tail -1`。
- ⚠️ **`no test` 的 FAIL 行數會抖**：`grep '^FAIL:'` 會同時撈到**測試程式自己印的** `FAIL: ...`
  （例如 `FAIL: conn-init fd`）。要比就比**排序後的整份 FAIL 集合**，不是行數。
- ⚠️ **`go build` 不跑 `go vet`，`go test` 跑。** `c.fail(format, args...)` 是 printf-like：
  傳**拼接出來的字串**（`c.fail("x " + raw)`）能 `go build` 過，卻在 `go test` 的 vet 檢查下
  `non-constant format string` 而**整包測試編不過**。一律寫成 `c.fail("... %s", raw)`。
- ⚠️ **`vecDeepClone` / `vecDeepFree` 吃的是「元素」型別，不是容器型別**；`optDropName` 用
  payload 型別是對的（釋放 `%vec` 與元素無關），但 clone 的 helper 名**必須**用元素 raw 當鍵，
  否則 `?[]i64` 與 `?[]str` 會共用一個 helper。兩者的錯誤都**只有全語料掃描／實際執行看得到**。
- ⚠️ shell：一律 `grep -E`（BRE 的 `\|` / `\b` / `\s` **靜默回空**）；`a && b` 中 grep 未中會
  **靜默截斷** ⇒ 用 `;`；macOS 無 `timeout` / `cat -A`。
- 🔴 **金標 A/B 出現「恰好 1 檔差異」時，「同 binary 重跑不一致」只排除「確定性輸出」，不排除缺陷。**
  `tests/slice-heavy.no` 曾被這樣歸類為「ASLR」而放過——**結論是錯的**（§4.4 a）。它印出堆位址**不是**
  因為 ASLR，而是因為它讀的是**已釋放的** buffer；讀到的頭部 magic `7957698236827265`（`0x6E6F6C616E670001`）
  就是鐵證。重跑不一致的**成因**是「釋放後的區塊被不同配置重用」——**那正是 use-after-free 的指紋**。
  正確判準是兩步：(1) 同 binary 重跑，不一致 ⇒ 只排除「輸出確定」；(2) **看輸出內容**——若像位址／magic／
  長度不定的垃圾，就當**缺陷**去查，不要當噪音關掉。
  （`scripts/mir_golden.sh` 的 `UNSTABLE` 是 `$MIR_GOLDEN_UNSTABLE` **硬編碼白名單**、預設為空，
  所以它不會自動把這種檔歸類為 UNSTABLE，而是報成 `DIVERGE`——**這是對的，白名單應保持為空**。）

**量測基準（截至 2026-10-02，規則一補完 `?str`/`?[]T` 後；控制組 = `no-head777`，真 HEAD `777b9389`）**：
`go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` 全綠（`go vet ./mir/` 乾淨）；
`no vet src/std` E 與控制組**逐位元組相同**（`0 error(s), 6205 warning(s), 936 hint(s)`）；`no test` **8 個 FAIL、與控制組逐行相同**；
語料 **515 檔編譯掃描 0 regression**（與控制組**逐檔相同**，509 過 / 6 敗，敗者兩邊同一組）；
`tests/rule1-binding.no` 在控制組上 `?[]i64` 崩潰、改動後 **9 例全過**；`?[]i64` 10 萬圈 heapstat 由 **100050 塊降到 50 塊**
（`?str` 兩邊皆 50 塊）；`nested-container-clone.no` **12/12 rc=0**；`rule1_binding_test.go` 在 `a6c8559d` 上 **6 敗**、
在 HEAD 與改動後**全過**。

**參數側 retain 閘門的量測基準（2026-10-02，v2.3）**：`spawn_arg_retain_test.go` **9 例全 PASS**（＋move/classes 家族）；
`tests/async-arg-retain.no` 觸發 **2** 次 retain、base 與 retain 輸出逐位元組相同、5 次 rc 全 0；
**44** 個 `OpRun` 檔定點 A/B **0 差異且 0 觸發**；全量 golden **517** 檔的 `DIVERGE(9)`／`REGRESS(6)` 集合
與 base **完全相同**（唯一 fp 差異 `slice-heavy.no` 當時被記為 ASLR——**該判斷已於 §4.4 a 推翻**）；
反向對照（`spawnArgWritesAfter`→`return false`）⇒ 原位寫入探針 **99**（誤編譯）vs 守衛版 **1**。

**本輪兩個缺陷修復的量測基準（2026-10-02，§4.4；控制組 = `git archive HEAD`（`e5b75772`）的樹 ＋ 只覆蓋自己的 4 個檔）**：

| 檢查 | 結果 |
|---|---|
| `go test -count=1 ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | ✅ 全綠；`view_liveness_test.go` **10 次重跑 10 次過** |
| `no vet src/std` | ✅ 與 `/tmp/no_vs` **逐位元組相同**：`0 error(s), 6205 warning(s), 936 hint(s)` |
| 全量 golden（`/tmp/no_vec2` vs 凍結金標，517 檔） | `SAME=468`、**`DIVERGE=8`**（原 9，減去 `slice-heavy.no`）、`UNSTABLE=0`、`REGRESS=6`、`IMPROVED=0`、`BOTH_FAIL=0`、`NEW=36` |
| 全量 golden A/B（`/tmp/no_vs` vs base，僅視圖修復） | ✅ **只有 1 檔改變** = `tests/slice-heavy.no`，且新指紋＝凍結金標 |
| 端到端 | ✅ `tests/slice-view-liveness.no`（5 例）、`tests/vec-elem-ownership.no`（5 例）皆 rc=0、3 次逐位元組相同；**兩檔在修復前的 binary 上分別印堆位址／`trace/BPT trap`** |
| IR 直證（`NOLANG_MIR_DUMP_LL=1`） | ✅ `hashmap_str_slice_i64_put` 內的 `call %vec @__nolang_vec_clone_8_0`：**0 → 2**（兩個 `.vals[idx] = val` 站點） |

> ⚠️ **`REGRESS=6` 是既有的地板，不是本輪造成的。** 這 6 檔（`mem-safety/{map-key-leak,map-tombstone,
> minimal-option-str,minimal-str-map,minimal-str-map2,option-str-match}.no`）在金標裡是 `rc=0`，但
> **在 `/tmp/no_vs`（僅視圖修復）與 `/tmp/no_vec2` 上都是 `rc=1`**，錯同一句
> `EmitLLVM: unknown callee str.init`（`map-key-leak` 是 `m2.get`）。⇒ **金標的這 6 筆是過期的**
> （凍結於 `str.init` 壞掉之前）。
>
> **刻意不刷新金標。** 刷新會把一個**真的既有缺陷**從 `REGRESS` 洗成 `SAME`——正是 harness 註解警告的
> 「re-freeze 是唯一會銷毀證據的操作」。`$MIR_GOLDEN_UNSTABLE` **維持空**（本輪再次確認：`UNSTABLE=0`）。
> 要收就修 `str.init`／`m2.get`，不是改金標。
> ✅ **2026-10-03 更新：已解，且解法證實了上面這段判斷。** 根因不是 `str.init` 本身壞掉，而是這 6 檔
> 用了**無標註**的 map 字面值 `m = {}`——在新的型別推斷下 `{}` 被解成 `str` ⇒ 走到 `str.init`。
> 修法是**改測試**（補上 `m [str]str = {}` 等真型別，`46be7004`），**不是改金標**：重算的 6 筆指紋與
> **原金標逐位元組相同**（⇒ 測試只是編譯不過，不是行為改變）。全語料 golden 517 檔 **`REGRESS` 6 → 0**、
> `UNSTABLE=0`、`DIVERGE=8`（與本模型無關）。`map-key-leak.no` 另把 `make-map()` 綁成 `m2`
> （回傳的 map 由呼叫端擁有、**必須**被 drop；丟棄它會讓測試測不到它要測的洩漏）。
> **教訓：`REGRESS` 不減 ≠ 地板；先查「測試自己是不是壞了」，再決定要不要動金標。**
> （`DIVERGE=8` 其餘 8 檔為 `ffi-sqlite`／`markdown`／`net-client`／`path-char`／`std-new`／`std-unix-fs-os`／
> `test-div-mod-option`／`tls`，皆與本輪無關。）

**本輪投影修復的量測基準（2026-10-03，v2.5；控制組 = 改動前「同一棵樹」的 `make no` 產物）**：

| 檢查 | 結果 |
|---|---|
| `go test -count=1 ./mir/ ./parser/ ./hir/ ./checker/ ./fmt/ ./lexer/` | ✅ **全綠**（先前由其他 session 造成的 3 個 `fmt` FAIL 本輪已不在） |
| `no vet src/std` | ✅ **0 error** |
| 端到端（`/tmp/no_v25` 自建 binary） | ✅ `tests/rebind-retracts-view.no` **9/9**；`slice-view-liveness.no`、`vec-elem-ownership.no`、`rule1-binding.no`、`async-arg-retain.no`、`async-arg-move.no`、`go-async.no` 全 `rc=0` |
| 全語料行為 A/B | ✅ (a) **480** 檔：465 逐位元組相同 ＋ 15 個環境噪音；(b) **481** 檔：465 相同 ＋ 15 個噪音 ＋ **唯一差異＝自己的新 case 8／9**（控制組 SIGTRAP） |
| 金標 | ⚠️ 本輪**未重跑全量 golden**：改動只影響**接收者運算元**（單元測試已直接斷言），且已用全語料**行為** A/B 覆蓋。`REGRESS` 的地板已於 10-03 的測試修正輪**歸零**（見上） |

---

## 6. 風險、非目標與取捨

### 6.1 非目標

| 非目標 | 理由 |
|---|---|
| 循環引用收集 | RC 的固有限制（同 Rust `Rc`）。用 `heap` 或顯式斷環 |
| 真執行緒的原子 RC | 當前是協作式單執行緒調度，計數不需原子。未來若加 threads，用**編譯期開關**切原子版本 |
| 改動 `%txt` | 棧類型，無堆，不參與任何檔位 |
| 改動借用視圖語義 | `cap==0 && data!=NULL` 的視圖、FFI 回傳，維持現行 sentinel 語義 |
| 閉包逃逸 | MIR 未實作 `KFuncLit`；若日後實作，RC 邊界需新增一條 |

### 6.2 主要風險

| 風險 | 影響 | 緩解 |
|---|---|---|
| **檔位判定不保守** | 少插 clone → UAF，且**靜默** | 所有判定「無法證明 → 往保守方向倒」；驗證器與插入器共用謂詞 |
| **規則一落地時的迴歸**（已落地） | 深拷貝增加 ⇒ 效能；改錯方向 ⇒ 共用 buffer ⇒ double free；**clone 與 drop 不同步 ⇒ 洩漏或 double free** | 逐檔解釋每一筆 clone 變化；**515** 檔編譯掃描（0 regression）＋ `no test` FAIL 集合比對 ＋ heapstat 洩漏量測（§4.2 a）。`emitOptionCloneHelper` 與 option 的 drop 是**成對**寫的：`emitOptionPayloadContentClone` 是 `emitOptionPayloadContentFree` 的鏡像，`vecDeepClone`／`vecDeepFree` 同進退 |
| **header 引入後 ABI 混用** | 對裸指標做 header 存取 → 記憶體損壞 | I1/I2 由 `checkTierSoundness` 強制（已落地，§3.3／§4.2 c）；I5/I6（`checkReleaseTarget`）仍**刻意未實作** |
| **`shouldUseMemcpy` 被間接改變** | 大聚合的 `loadVal` 由「回傳值」變「回傳槽指標」→ 走進從未執行過的路徑 | 任何可能改變 `computeTypeSize` / 4096 門檻 / 欄位佈局的改動**必須跑金標** |
| **驗證器假陽性** | `rep.HasErrors()` gate codegen → 整檔編不過 | 與插入器共用謂詞；每個新驗證器附「修復前必須失敗」的測試 |

### 6.3 已知取捨（必須寫進使用者文檔）

1. **RC 檔的循環引用會洩漏。** 確定性記憶體管理的標準代價。
2. **R 檔有 inc/dec 開銷。** 但只要不跨協程邊界，值就留在 S/C 檔。
3. **跨協程邊界的深拷貝**比 RC 貴，但語義最簡單。⚠️ **不能只改成 retain**：那會讓任務看到呼叫端的
   **原位寫入**（共享），與第 5 條衝突；正確形式是 C 檔 CoW（§4.2 b）。
4. **規則一讓 `a = b` 多一次深拷貝。** 這是刻意的：用一次拷貝換掉一整類別名分析。
5. **spawn 邊界對「輸入參數」傳遞獨立副本（快照），不共享。** 任務看到的是 **spawn 當下**的輸入值。
   這是「協程之間不共享可變堆數據」的落實；代價是每次 spawn 一次深拷貝。
   ⚠️ 這條**此前只存在於使用者文檔**（`docs/docs/lang/memory.md`「async 共享數據」），模型文件未正式
   記錄 ⇒ 補記於此，並由 `tests/async-arg-move.no` 第 9 例釘住。
   **兩點範圍**：①只到**輸入參數**——回傳值 `r` 是宣告在參數位置的 **out param**，由任務自己寫入，
   不在 clone／free 協議內（§4.2 b ④）；②與「改成 retain」**只在「呼叫端原位寫入輸入參數」這一種
   形狀上分歧**，重賦值與離開作用域兩者結果相同（§4.2 b ②）。

---

## 7. 決策紀錄

| # | 問題 | 決策 |
|---|---|---|
| 1 | P0 的修復策略：先深拷貝參數，還是直接跳 RC？ | **先深拷貝**（可獨立發布、可獨立回滾，且給後續 RC 一個正確性對照組） |
| 2 | header 的 sentinel 編碼：`rc = 0` 還是 `flags.bit1`？ | **`rc = 0`** 單一判準；`flags` 保留給未來擴充 |
| 3 | 檔位是否對使用者可見（`#{tier=R}`）？ | **先不開放**（檔位是推斷結果，開放會讓它變成契約） |
| 4 | `%task` 欄位擴充：新增欄位還是複用 `cancelled`？ | **新增欄位**（`cancelled` 是 `i1` 且語義明確） |
| 5 | 是否處理 ready queue 溢出？ | **改為可增長佇列**（溢出根本不存在，因此無需報錯） |
| 6 | I3 的驗證器強制**等號**還是**不等式**？ | **不等式**。等號對「未 await 的引用」為假，而那正是已接受的洩漏；在此處假陽性＝硬編譯失敗 |
| 7 | `run <handle>` 的語義：**補 retain** 還是**報錯**？ | **補 retain**（語義明確的第二個引用；語料與文檔都沒有這個拼法 ⇒ 無相容性包袱） |
| 8 | 參數側：先做 retain，還是先做**不改 ABI 的 move**？ | **先做 move**（§2.3）。原本記的「retain 要等 header」是錯的——header ABI 已全型別落地；「源仍活 ⇒ retain」真正卡的是「呼叫端寫入點」分析（§4.2 b） |
| **9** | **`a = b` 要不要保留零拷貝別名？** | **不保留：一律深拷貝**（§1.1）。隨意別名毫無語義價值，卻讓分析憑空多出一整類情形。`a = b[i]` / `a = b.c` 維持原樣（§1.2） |
| 10 | 規則一的綁定閘門開到多大？ | **開到 owned `str` ＋ owned option**（＋既有的 owned `[]T`）。`?str`/`?[]T` 的 drop 側**本來就是 no-op 死碼**（見 §4.2 a），所以放寬閘門的同時必須把 drop 修好，否則 clone 只會製造洩漏。閘門用**宣告型別**排除 option→slice 的**剝離**（`p []T = o` 由既有的剝離區塊處理，實測不變：`move dst=…:[]i64`）。map 的 `isOwnedLocal` 為 false 且是引用語意 ⇒ **不收**（§4.2 a） |
| 11 | 移除前向後，`lowerDeadSourceCopiesToMoves` 還要不要處理 str／struct？ | **不要，只留 owned slice**。str/struct 的 move 已由 `insertDrops` 用同一條活性規則處理；在這裡改寫 OP 會**跳過目的地的型別檢查**（`vecDeepClone` 依元素型別取 stride ⇒ 型別不符可編譯但靜默錯誤） |
| **12** | **參數側（源仍活）要改成 retain 嗎？** | **不能只做 retain。** 原本記錄的前置條件（「buffer 要帶 header ⇒ 配置點改走 `rc_alloc`」）**已不存在**——header ABI 是全型別落地，參數 buffer 今天就帶 header。真正的障礙是**語義**：retain 會把 spawn 邊界的「快照」變成「共享」，任務會看到呼叫端在 spawn→`awy` 之間對 `x` 的**原位寫入**（實測：深拷貝印 `1`、retain 印 `99`），與使用者文檔「跨協程共享可變堆數據不成立」衝突，且**語料無任何一例測它 ⇒ 會是靜默改變**。**結論：收成「可證明無原位寫入 ⇒ retain」的靜態閘門**（§4.2 b ⑤）——無法證明就退回深拷貝，比 C 檔 CoW 簡單（不需運行時 clone-on-write）且零風險。✅ **已於 2026-10-02 落地**（§4.2 b ⑥）：`spawnArgWritesAfter`＋`spawnArgRetainSafe`，範圍**刻意收窄**到 `str`／非擁有元素的 `[]T`，對既有語料**零觸發**。快照語義仍由 `tests/async-arg-move.no` 第 9 例釘住；`tests/async-arg-retain.no` 釘住共享路徑的計數平衡 |
| **13** | **接收者投影的失效條件用什麼判準？** | **結構性判準**：①「**這個 value 已經不是當初那個值了**」⇒ `defCount > 1` 就撤回投影（value id 是變數，重新綁定走 move-into 編碼，`Dst = NoVal`，存活分析看不到）；②「**來源已經沒有那個值了**」⇒ `sourceAlreadyDropped`（drop 落在 read 與 use 之間）**或** `sourceWrittenBetween`（那條路徑在 read 與 use 之間**被寫過**）。**不用存活分析**——「已 drop」≠「已死」，且 move-into／`setfield` 對存活分析隱形（§4.5）。🔴 臨時 kill switch 必須**忠實重放原行為**，否則控制組變成「最後定義勝」⇒ 測試假通過 |
| **14** | 投影守則要不要套到**非 clone** 的欄位／元素？ | **不要。** 前提是 `projectionWasCloned(v)`——讀取產生了**獨立的 owned `%str-long`** ⇒ 自己的槽是完整私有值，退回它永遠安全。套到非 clone 的欄位會讓 `x = o.c; x.n = 7` 的寫回變成**寫副本**（值沒改到）。所以 `getFieldWasCloned` 更名為 `projectionWasCloned`（field 與 index 共用同一個前提） |
| **15** | **tier 推斷該「落地成規定式」還是「落地成驗證器」？** | **只能當驗證器（描述式），不能當規定式。** 這是**依賴問題不是範圍問題**：要讓 tier 改寫 IR，得把 pass 移到 `insertBorrowRetains`／`emitAsyncRun` 之前，但 `tierConstraint` 的 R 列**讀回 `spawnArgRetains`**——那是 `insertDrops` 寫的 ⇒ **往前搬成循環依賴**。所以它只驗證別處做出的決定，且呼叫點必須在 `insertDrops` **之後**（§4.2 c） |
| **16** | **驗證器怎麼才不會變成自證？** | **讓它讀「另一側」的來源。** 推斷讀**自己的約束表**；`checkTierSoundness` 讀 **IR 的 R 物件地面真相**（spawn handle ＋ `handleAliasSet` 全部別名、`spawnArgRetains` 的參數、`OpTaskRetain`／`OpAwait` 運算元）。**舊的恆等映射（全 S）降級為負對照**——餵它進去必須開火（§3.3／§4.2 c）。⚠️ 兩側都必須是**讀回已記錄的決定**，不得各自重算（§4.5 紀律） |
| **17** | **「只洩漏、不影響輸出」的殘留要怎麼記？** | **沒有可複現測量的殘留不該寫進 §4.3。** §4.3 的「借讀 release 死亡點精度」存在了兩個版本、被「複查」過一次，卻**從未被任何測量支持**——它的症狀（drop 被提到迴圈外）在現行樹上**不存在**（§4.3 三條證據）。這一類殘留的危險正在於**沒有輸出可看** ⇒ 永遠不會有人發現它已經不成立。**紀律**：寫進 §4.3 前必須附**可重跑的指令 ＋ 觀測值**；觀測不到就寫「未複現」而不是「已知」。⚠️ 反向也成立：**已撤銷的殘留要留痕**（本節保留舊數字並標註不可信），否則後人會再引用一次 |
| **18** | **量洩漏時高圈數「崩潰」該怎麼歸因？** | **先證明崩的是什麼，再拿它當洩漏的證據。** v2.7 量 map 洩漏時把 200,000 圈的 **rc=139** 記成「洩漏大到崩潰」，還報了「沒用過的 fresh map 每圈 ~80 B」。v2.8 證明**兩者都錯**：崩的是**棧**溢位（`alloca` 在迴圈內，§4.6），峰值 RSS 只有 15.6 MB；而那 ~80 B/圈量的**就是棧增長**，不是堆洩漏（`m [str]i64 = {}` ＋ `m.len()` 跑 1,000,000 圈是 **1.69 MB 平線**）。**紀律**：①崩潰**必須**區分棧／堆（看峰值 RSS：棧溢位時 RSS 很小；`rc=139`＋`Thread stack size exceeded` 的 crash report 是決定性的）；②「崩潰」**截斷**了洩漏的量測 ⇒ 修好崩潰**之前**報的斜率都不可信；③斜率要用**兩點以上**算，並配一個**負對照**（本條：`init()` 但不 `put`） |
| **19** | **IR 的結構性不變式該由 emitter 保證，還是交給 `opt`？** | **由 emitter 保證。** `alloca` 必須在 entry block 是不變式，但 `-O2` 不會幫你做：**LICM 不提 `alloca`**，`mem2reg`/`SROA` 也**無法提升**不在 entry block 的 `alloca`。後果是「呼叫被 inline 就沒事、沒被 inline 就棧溢位」——一個**只在特定 inline 決策下才成立**的不變式，本質上是不變式沒被建立。修法是 emitter 端逐函式重寫（`hoistEntryAllocas`），**不是**加 pass 或調 opt 參數。⚠️ 重寫必須**放過動態 `alloca`**（人數運算元可能是後面 block 定義的 register ⇒ entry 不支配它） |

---

## 附錄 A：語料清單

| 檔案 | 狀態 | 覆蓋的形狀 |
|---|---|---|
| `tests/async-ownership.no` | 新增（P0） | 參數重賦值（str／vec／struct）、同槽雙 await、null handle、單次 await、雙 handle |
| `tests/async-handle-alias.no` | 新增 | 別名雙 await、別名鏈、out-param 返回、存容器逃逸、同槽兩次、spawn+await 循環 |
| `tests/async-rc.no` | 新增 | 跨邊界共享／多任務／取消後不 await／handle 存容器 ＋ 兩個漏計缺陷迴歸（void 別名、`run` 轉發） |
| `tests/async-arg-move.no` | 新增 | spawn 參數 move：5 個該 move（6 個參數）＋ 3 個該維持深拷貝的反向案例（重賦值／讀取）＋ **第 9 例釘「快照」語義**（呼叫端原位寫入 ⇒ 任務仍看到 spawn 當下的值），是 §4.2(b)「無條件 retain」的守門員 |
| `tests/async-arg-retain.no` | 新增（§4.2 b ⑥） | spawn 參數**共享**（靜態閘門）：`[]i64` 共享（`a[1]` 讀）＋ `str` 共享（`s[0]` 讀）＋ **原位寫入退回深拷貝**（`a[0]=99` ⇒ 任務仍見 `1`，快照守衛）＋ **20 圈迴圈共享**（釘住引用計數平衡）；觸發 2 次 retain，輸出 base 與 retain 逐位元組相同 |
| `tests/async.no` / `cancel.no` / `async-coop.no` / `async-yield.no` / `module-async.no` | 既有 | flat spawn 邊（線性化對照組）、取消、協作、模組層級 async |
| `tests/rule1-binding.no` | 新增（規則一） | `a = b` 的 `str`／`[]i64`／struct 寫入獨立性 ＋ 只讀 ＋ 源已死 move；**唯一 stdout 可觀測的規則一缺陷** |
| `tests/slice-view-liveness.no` | 新增（§4.4 a） | 切片視圖的來源壽命：`b = a[i..j]` 之後讀 `b`（修復前印**堆位址**）＋ 巢狀視圖 ＋ 來源重綁 |
| `tests/vec-elem-ownership.no` | 新增（§4.4 b） | `%vec` 元素賦值的所有權：`outer[0] = inner` ＋ 重綁來源（修復前 `trace/BPT trap`）＋ `vec.push` 對照 |
| `tests/rebind-retracts-view.no` | 新增（§4.5） | **接收者投影的撤回**：9 例——重新綁定後讀 `len()`（case 1／5／6，修復前 `got=0`）＋ 容器重綁（case 8，修復前 **SIGTRAP**）＋ 欄位重綁（case 9，**全程式無 drop**，單獨釘「寫入」這條）＋ 對照組（單次定義**仍**投影，case 7） |
| `tests/set-byte-receiver-writeback.no` / `tests/len-assign-grow.no` | 既有 | **反向對照組**：寫入本身就是 use ⇒ **必須繼續投影**（修投影守則時這兩檔是守門員） |

⚠️ 語料撰寫注意：`;` 是**行註解**不是陳述分隔符；`''` 是 `str`、`""` 是 `char`；切片寫 `[a..b)`；
**option match 必須三個臂都寫**。

## 附錄 B：觸點索引

> 行號會隨每次插入而位移上百行 ⇒ **只信任函式名**。

| 主題 | 位置 |
|---|---|
| drop 插入 | `src/mir/analysis.go` `insertDrops` |
| **規則一的降低** | `src/mir/alias_forward.go` `lowerDeadSourceCopiesToMoves`（源已死 ⇒ `OpClone`→`OpMove`）；前向已刪；測試 `alias_forward_test.go` |
| 新綁定的深拷貝 | `src/mir/hir2mir.go`（owned slice → `OpClone`；owned `str` → 拷貝進新值）；測試 `rule1_binding_test.go` |
| 選項剝離所有權 | `analysis.go` `optionSlicePeelClones` / `isBorrowRead`；回歸釘 `option_peel_ownership_test.go`（`peelDst` **跟隨轉移鏈**） |
| move 四判別式 | `analysis.go` `moveStructSharesHeap` / `moveStrSharesHeap` / `moveSliceSharesHeap` / `moveTransfersOwnership`；聯集 `moveExemptsSource` |
| drop 數量驗證（I4） | `analysis.go` `checkDropCount` |
| 計數平衡驗證（I3） | `analysis.go` `checkRefBalance`；別名集合重用 `spawn_graph.go` `handleAliasSet`；診斷 `missing-task-retain` |
| 借讀 retain／release | `analysis.go` `insertBorrowRetains` / `borrowRetained` / `borrowReleaseValues`（種子**只讀回 `OpRetain` 指令**、**不做 clone 閉包**、**做 move 閉包**）；`mir.go` `OpRetain` / `OpRelease`；`codegen.go` `emitRetain` / `emitRelease`；釘子 `borrow_release_test.go`（含 v2.7 的 `TestBorrowInLoopIsDisposedEveryIteration`，帶負對照） |
| 深釋放配深克隆 | `codegen.go` `vecDeepClone` / `vecElemNeedsDeepFree` / `vecDeepFree` |
| spawn 參數 move | `analysis.go`（`wouldDrop` ＋ 獨立第二迴圈）、`mir.go` `Module.spawnArgMoves`、`spawn_graph.go` `SpawnArgClasses` / `DumpSpawnArgMoveStats`、`codegen.go` `emitAsyncRun` |
| **spawn 參數 retain（靜態閘門）** | `analysis.go` `spawnArgWritesAfter` / `spawnArgUseWrites` / `spawnArgRetainSafe` / `VecElemOwnsHeap`、`mir.go` `Module.spawnArgRetains`、`codegen.go` `emitSpawnArgRetain`（`emitAsyncRun` 的 `share := moved \|\| retained`）、`spawn_graph.go` `DumpSpawnArgMoveStats`（報告-only 的 `retained`） |
| spawn graph / 線性化 | `src/mir/spawn_graph.go` `SpawnGraph` / `evalSpawnEdge`（報告-only） |
| Tier 推斷 | `src/mir/tier.go` `Tier` / `joinTier` / `tierConstraint` / `tierStep` / `writtenThroughSet` / `inferTiers` / `checkTierSoundness`（＝`checkTierPartition` ＋ `checkTierRSites`）/ `DumpTiers`（`NOLANG_MIR_TIER=1` 只印直方圖，**推斷本身已是硬閘門**） |
| handle 身分集合 | `hir2mir.go` `asyncHandles`（`asyncResTypes` 只管結果型別） |
| `run <handle-var>` 轉發 | `hir2mir.go` `lowerAsyncRun` |
| 借用逃逸 / 視圖降級 | `analysis.go` `checkBorrowEscapes` / `demoteUnsafeSliceViews` |
| 塊內讀取判定 | `analysis.go` `readNonDropAfterInBlock` |
| header ABI | `codegen.go` `@nolang_rc_alloc` / `@nolang_free` / `@nolang_rc_retain` / `@nolang_rc_release`；型別 `%str-long` / `%vec` / `%option` / `%task`（32 bytes） |
| async 排程器 | `codegen.go` `emitAsyncScheduler`（ready queue 可增長）、`emitAsyncRun`、`emitAsyncAwait`、`emitTaskRetain` |
| 克隆欄位守衛 | `codegen.go` `getFieldWasCloned` / `sourceAlreadyDropped` / `lvalueAddrOf` / `emitGetField` |
| **呼叫臨時變數的 `alloca` 只能在 entry block（v2.8）** | `codegen.go` `hoistEntryAllocas` / `isHoistableAlloca`（由 `emitFunc` 在函式結束時套用；`codegen.sb` 是 `*strings.Builder`，逐函式重導到暫存 builder）；呼叫臨時變數的發出點：`%cargN`（引數暫存）、`%cresN`（出參暫存）、`%icrN`／`%adsN`；釘子 `src/mir/entry_alloca_test.go` |
| **接收者投影的撤回（v2.5）** | `analysis.go` `definedValue`（跨兩種 move 編碼取「定義了誰」）＋ `moveSrc`／`moveDst`／`moveLike`；`codegen.go` `defCount`（第二次定義 ⇒ `delete(defInst)`）、`projectionWasCloned`（原 `getFieldWasCloned`）、`projectionChain`、`reachableBlocks`、`sourceWrittenBetween`；`lvalueAddrOf` 的 `OpGetField`／`OpIndex` 兩分支共用 `sourceAlreadyDropped \|\| sourceWrittenBetween` |
| MIR 指令定義 | `src/mir/mir.go`（move/clone/drop/borrow、run/await/task-retain、retain/release） |
| 現行模型文檔 | `docs/docs/lang/memory.md` |
| `KFuncLit`（未被 MIR 消費） | `src/hir/hir.go`、`src/parser/tohir.go` |
| 迴歸釘（測試） | `src/mir/{alias_forward,async_boundary_ownership,async_rc_handle,spawn_arg_move,spawn_arg_retain,spawn_graph,str_field_lvalue,tier,vec_deep_free,borrow_release,p4_header_abi}_test.go`、`src/mir/option_peel_ownership_test.go`、`src/mir/{view_liveness,rebind_retracts_projection,str_lit_char_concat}_test.go`、`src/mir/entry_alloca_test.go`（v2.8：迴圈內的呼叫臨時變數必須被提到 entry block，含動態 `alloca` 負對照）、`src/parser/{struct_literal_field_value,stmt_boundary_block}_test.go`；語料 `tests/{rebind-retracts-view,set-byte-receiver-writeback,len-assign-grow}.no` |
