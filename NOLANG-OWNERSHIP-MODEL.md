# Nolang 混合所有權模型（Hybrid Ownership）— 工業級設計方案

**版本**：v1（完成版，2026-10-01）
**基線**：`bin/no` @ 2026-09-30 22:42（HEAD `5b88ab53` 工作樹）
**適用後端**：MIR（`src/mir`）
**前置文件**：`docs/docs/lang/memory.md`（現行模型權威描述）、`NOLANG-AUDIT-2026-09-27.md`
**改動規範**：skill `nolang-compiler-change`（金標、A/B 歸因、並行 session 陷阱）

**完成狀態（2026-10-01）**：§1.2（修正 A 安全子集）、§1.3（修正 B：P4 ＋ P5 的 `%task` 列）、
§1.4／§4.3（修正 C：P3 報告-only）、§1.5（克隆欄位投影缺陷）、**P1（Tier 推斷框架）** 皆已落地並通過驗收；
§2.3 的四個缺陷三個已修、一個（§2.3.3）降為已文檔化的取捨。**§8 的待決事項已全部定案。**
**仍未落地**：P2 的完整版（首寫點 clone ＋ 借用別名 ＋ 所有者 drop 延後）、P4/P5 的**參數側**
（header-aware 配置 ＋ retain）、P6 的驗證器——三者互相依賴且會動 ABI／語義，見 §5 各節的取捨說明。

---

## 0. 結論摘要

你的三檔直覺方向是對的，而且比現行模型更進一步。但直接照字面實作會踩三個坑；本方案的核心價值就在把這三點釘死，並給出可落地的分階段路線。

| # | 你的表述 | 需要修正的地方 | 為什麼 |
|---|---|---|---|
| A | 「寫操作 → 觸發 clone（CoW）」 | 靜態檔**不該**引入 runtime shared-bit 檢查。要的是「**clone 插入點從賦值處搬到首次寫處**」——純編譯期、路徑敏感、零 runtime 分支 | 若用 runtime shared-bit，每次寫都要一個 branch + header 讀取，就違背了你自己要的「靜態檔無運行時開銷」 |
| B | 「協程 → 回退 RC」 | RC 的作用域**只有一條邊**：協程 spawn。因為 MIR 後端目前**完全不支援閉包**（`hir.KFuncLit` 由 `parser/tohir.go:920` 產生，但 `src/mir` 無任何消費者） | 沒有閉包逃逸、沒有真執行緒，就沒有第二條逃逸邊。RC 的工程量因此從「語言級」降到「一個邊界」 |
| C | 「只有一個協程任務 → 不必 RC」 | 判準不是「任務數量 == 1」，而是 **spawn graph 的線性化條件**（handle 不逃逸、每條路徑恰好 await 一次、callee 不再 spawn）。而這個快速路徑**已經實作**了 | 1 個任務也可能逃逸（存進 vec/global、await 兩次、不 await），此時仍是不可靜態確定 |

**最重要的判斷**：P0 不是 RC，而是**修 async 邊界的現存缺陷**。我已實測到：

- 同一個 handle `awy` 兩次 → **SIGSEGV**（§2.3.1）
- spawn 之後重賦值參數變數 → **靜默資料損壞**（讀到 40 個 NUL，rc=0，無任何報錯）（§2.3.2）

這兩個缺陷今天就在生產路徑上，且與所有權模型無關地獨立可修。**先把邊界變安全，再談 RC。**

> **狀態（2026-10-01）**：兩者**都已修**。§2.3.1 由 P0 的槽歸零 ＋ 修正 B 的 RC 共同收掉；
> §2.3.2 由 P0 的 `asyncArgOwnedCopy` 深拷貝收掉。§2.3.3 的洩漏在修正 B 之後降為
> 「已文檔化的取捨」（rc 停在 1），§2.3.4 的 ready queue 已改為可增長佇列。

---

## 1. 你的模型：判定與三處必要修正

### 1.1 對的部分

現行 MIR 的 clone/move 決策**已經是「按後續是否使用」判定的**，與你的描述一致：

> `analysis.insertDrops`（`src/mir/analysis.go:1534`）對每個 `OpMove` 問一句：**源在這次搬移之後還活著嗎？**
> - 還活著（`liveOut[blk][src]` 或塊內後續有非 drop 讀取）→ 改寫成 `OpClone`，深拷貝，兩邊各自持有、各自釋放
> - 可證明已死 → 保留零拷貝 move，源豁免 drop

四個判別式依序短路：`moveStructSharesHeap`（`analysis.go:1140`）→ `moveStrSharesHeap`（`:1242`）→ `moveSliceSharesHeap`（`:1277`）→ `moveTransfersOwnership`（`:2186`）；驗證器 `checkDropCount`（`:2402`）必須共用同一聯集 `moveExemptsSource`（`:2225`）。

所以你說「move 是確認只有一處使用，clone 是多個使用者」——**現況正是如此**，只是判定發生在**賦值點**而非**寫入點**。

### 1.2 修正 A：靜態檔的 CoW 是「clone 插入點搬移」，不是 runtime shared-bit

**你的原意**：讀的時候不複製，寫的時候才複製。

**現況**：`b = a` 這個賦值本身就可能 clone（因為判定在賦值點）。

**問題**：如果為了支援「讀時零拷貝」而給每個 buffer 加 runtime shared 標記，那**每次寫**都要：

```
if (hdr->shared) { clone(); hdr->shared = 0; }
```

這個 branch 出現在所有熱路徑（`s[i]=c`、`v.push`、`p.f=y`、`s.append`）。對「靜態可確定」的多數程式而言，這是**純浪費**——因為編譯期已經知道這裡是不是唯一持有者。

**正解**：把 clone 從「賦值點」搬到「**該別名生命期內的第一個寫入點**」。

- 讀（`b = a`，之後只讀）→ 完全不動，零成本、零拷貝。這正是你要的「只讀引用，不拷貝」。
- 寫（`b[0] = x`，且分析證明 `a` 在此仍活）→ **在該寫之前**插入一次 `OpClone`。之後 `b` 獨佔，後續寫不再檢查。
- 若別名已死 → 連 clone 都不用，直接寫。

**收益**：比現況**更少** clone（現況在「賦值了但從沒寫」時也會 clone）。而且**零 runtime 分支**——這才是「靜態可確定 → 無運行時開銷」的正確落地方式。

> 實作上這不是新機制，而是把 `insertDrops` 既有的 liveness 查詢**換一個查詢點**：從「move 的源是否還活」改成「首寫時，該值是否有其他活躍別名」。判定謂詞可完全複用 `readNonDropAfterInBlock`（`analysis.go:1073`）與 `liveOut`。

#### 1.2.1 實作現況（2026-09-30）——讀完程式碼後對本節的兩處更正

**更正 1：賦值點的 clone 不在 `insertDrops`，在 `hir2mir`。** 本節原本假設 `b = a` 會先降為 `OpMove`、再由 `insertDrops` 依 liveness 改寫成 `OpClone`。實測（`NOLANG_MIR_DUMP_MIR=1`）並非如此：**owned slice 的全新綁定在 `hir2mir.go:3003` 就直接產生 `OpClone`**（註釋在 `:2984`：「a fresh binding has no pre-existing slot to move into, so it always clones」），`insertDrops` 從未見過那個 move。`str` 與 struct 的新綁定則是**共用同一個 ValueID**（`l.locals[name] = val`），連 move 都沒有。

⇒ 所以修正 A 的觸點不是「換一個 liveness 查詢點」，而是**多一個 pass**：在 `Analyze` 裡、`insertDrops` 之前跑 `forwardReadOnlyAliases`（`src/mir/alias_forward.go`），同時辨識 `OpMove`（會被 `insertDrops` clone 的）與 `OpClone`（`hir2mir` 直接產生的）兩種賦值點拷貝。

**更正 2：「首寫點 clone」若照字面實作會改變可觀測行為。** 把 clone 一路延到「別名生命期內的第一個寫入點」，等於讓 `b = a; print(b[0]); a[0] = 99` 的 `print(b[0])` 讀到 `a` 的新值（今天讀舊值）。而且兩邊共用一個 buffer 卻各自 drop ⇒ 雙重 free。要真的做「首寫點」，必須同時引入**借用（不 drop 的別名）**與**把所有者的 drop 延到別名最後使用之後**兩個新機制。

⇒ 因此本輪實作的是**安全子集**：
- **只讀別名 → 零拷貝**（本節第一條 bullet，也是本節自稱的收益來源：「現況在『賦值了但從沒寫』時也會 clone」）。實作方式是把別名的所有使用改寫成源、刪掉那個 move/clone ⇒ 共用 buffer、**只剩一個 drop（源的）**。
- **有寫入的別名 → 維持在賦值點 clone**。這是「首寫點」的**保守超集**（只會更早 clone，不會更晚），所以所有會被寫的別名行為與今天逐位元組相同。

**安全準則（白名單，預設拒絕）**：別名必須只被讀取（`OpIndex`/`OpLen`/`OpCap`/`OpUtf8At`/`OpStrEq`/`OpTxtFromStr`/`OpClone`/`OpDrop`/純量運算）；**源在別名存活期間不得被寫入或逃逸**（傳給 callee 算逃逸——callee 透過借用的指標真的能改到呼叫方的 buffer，已實測）；別名與源都必須只被定義一次且不得被重新綁定。任一條件無法證明 ⇒ 保留今天的 clone。

**實測（544 個語料檔，`Analyze` 後 `OpClone` 總數）**：**328 → 164（−50%）**，其中 66 個檔案有 clone 降到 41 個，`tests/opt-box-drop.no` 12→6、`tests/mem-safety/nested-container-clone.no` 35→13、`test/std/option.no` 29→6、`test/std/txt.no` 16→0。

**⚠️ 金標掃描抓到兩個真實缺陷（都已修，這正是「先寫保守子集、再讓語料說話」的價值）。** 兩者都不是「分析不夠保守」而是**守則本身的兩個盲點**：

1. **別名鏈（alias chain）**：`c = b` 而 `b` 本身已被前向 ⇒ 我原本「先蒐集所有改寫、最後一次套用」的寫法讓 `c` 仍指向**已被刪除的** value id。
   症狀：`tests/mem-safety/nested-container-clone.no` 直接編譯失敗
   （`EmitLLVM: index slot: value id 124 (type %vec) has no slot in func 2`）。
   **修法**：改成**依程式順序邊決定邊改寫**，後面的候選自然看到已改寫過的運算元。
   **可迴歸的不變式是「每個運算元都有定義」**，不是 clone 數——壞版本刪掉的 clone 數量跟好版本一樣多。

2. **源的 buffer 不屬於源（borrow source）**：`OpEnumField` 抽出的 tagged enum 載荷是**借用**，
   由 enum 自己的 drop 釋放。把它前向後，別名活得比那個 drop 久。
   症狀：`tests/tagged-enum-two-match.no` 第二次 match 的 `for x <- items` 疊代**已釋放的載荷**，
   `4b:` 整段印不出東西（rc=0、無任何診斷——**靜默 UAF**）。
   **修法**：新增準則 **(r4) 源必須擁有自己的 buffer**，用既有的 `isBorrowRead`
   （涵蓋 `OpIndex` 元素讀、`OpGetField` 非-str owned 欄位、`OpEnumField` 載荷抽取）
   ＋`isSliceViewOfArray`。⚠️ 因此本 pass **必須在 `markEnumPayloadOwners` 之後**執行
   （`isBorrowRead` 要讀 `enumOwnsPayload`），故呼叫點在 `insertDrops` 內、`m.Liveness` 之前，而非 `Analyze`。

修完後語料總數 **328 → 168（−49%）**；被準則 (r4) 擋下的 4 個 clone 正是造成上述兩個缺陷的那些。

#### 1.2.2 實作現況（2026-10-01）——「源已死 ⇒ move」與選項剝離的重複 clone

§1.2.1 把 pass 限定在「**只讀**別名零拷貝」，有寫入的別名一律維持賦值點 clone。本輪補上另一半中**可靜態證明安全**的部分：**源在拷貝點之後已死**時，那個拷貝根本不必存在——它是 **move**，不是 copy。

**做法**：`forwardReadOnlyAliases` 的主迴圈新增一條規則（跑在既有的只讀決策**之前**）：

- **條件**：`inst.Op == OpClone`、源在該指令之後**已死**（`!liveOut[bid][src] && !readNonDropAfterInBlock(...)`，與 `insertDrops` 同一組判定）、源與目的都是 owned slice、且**源擁有自己的 buffer**（見下）。
- **動作**：把 `OpClone` **改寫成 `OpMove`**，交給 `insertDrops` 既有的 slice-move 邏輯（源已死 ⇒ 保留零拷貝 move 並豁免源的 drop；源還活著 ⇒ 立刻 clone 回來）。因此這條規則**只可能移除拷貝、不可能新增**，也不可能 under-clone。

**🔴 關鍵教訓：這條規則必須放在 (r4) 之後。** 第一版把它放在 (r4)「源必須擁有自己的 buffer」之前，於是它連**借用型**來源的 clone 也改寫成 move，把**所有者仍要釋放**的 buffer 交給目的端 ⇒ **雙重 free**（不是洩漏）。語料實測兩個真實破壞：

- `tests/tagged-enum-two-match.no` 輸出**改變**；
- `tests/tagged-enum-zero-match.no` **崩潰**（`rc=133`）。

**修法**：把 (r4) **上移**到這條規則之前，讓它同時守住「前向」與「改寫成 move」兩個決策——`isBorrowRead` 是「這條指令交出的是所有權還是視圖」的唯一真源，就必須在**兩個**決策前都被諮詢。修好後兩檔恢復 0 差異、不崩潰。（這也說明：**回歸測試抓得到的是「行為」**，而行為差異只在語料上才看得見——單元測試當時是綠的。）

**收益（508 檔語料，`NOLANG_MIR_DUMP_MIR=1` 逐檔統計 `clone` 指令數）**：

| 檔案 | clone（前 → 後） |
|---|---|
| `tests/mem-safety/nested-container-clone.no` | 13 → 9 |
| `tests/std-new.no` | 1 → 0 |
| 其餘 506 檔 | 不變 |

**clone 總數 211 → 206；增加數 = 0**（符合 P2 驗收的「只減不增」）；**rc 差異 = 0**；受影響檔案的 stdout **逐位元組相同**。

⚠️ **「clone 數沒變」不等於「規則沒觸發」。** 以 `NOLANG_ALIAS_STATS=1` 逐檔統計觸發次數，
全語料**恰有 4 檔觸發、共 29 次**：

| 檔案 | 觸發次數 | clone 變化 |
|---|---|---|
| `mem-safety/nested-container-clone.no` | 25 | 13 → 9 |
| `std-new.no` | 2 | 1 → 0 |
| `mem-safety/slice-view-escape.no` | 1 | 0（被 `insertDrops` clone 回來） |
| `nested-vec-test.no` | 1 | 0（同上） |

後兩者正是「規則改了 OP、`insertDrops` 又依當下的 liveness clone 回來」的情形：最終指令形狀
與控制組相同（差別只剩**不確定的 drop 順序**），所以 clone 數看不出來。**這 4 檔的建置 rc 與
程式 stdout 都與控制組逐位元組相同。**

**收益的來源是「選項剝離」的重複拷貝。** `?[]T` 的剝離（`s []i64 = o`）會用 `vecDeepClone` 給目的端一份**私有**載荷，`hir2mir` 又為那個全新綁定再 clone 一次 ⇒ **同一份載荷付兩次深拷貝**。把綁定那次改寫成 move 後只剩一次。

**⚠️ 這更正了 §1.2.1 的一個推論**：§1.2.1 把「非 clone 型選項剝離」列為借用是對的；但 **clone 型（`?[]T`）剝離的目的端是所有者**（`isBorrowRead` 回報它「擁有」，`optionSlicePeelClones` 為真）。所以規則會——也**應該**——在它上面生效。

**不變式以「轉移鏈」為準，不是以 value id 為準。** 改寫成 move 後，釋放載荷的 drop 從「剝離的暫存值」換成「綁定的值」。`option_peel_ownership_test.go` 的 `peelDst` 因此改為**跟隨轉移鏈**：從剝離目的端出發，本身被 drop 就通過，否則跟著 `OpMove` 走到鏈尾再檢查。這仍然是對**不變式**（「載荷被釋放」）的斷言，而不是對某個 value id 的斷言——**已驗證它仍能抓到 2026-09-25 的那個洩漏**：把 `optionSlicePeelClones` 強制回 `false` 模擬修復前行為，`TestOptionSlicePeelFreshBindingDropsClone` 與 `TestOptionSlicePeelSpellingsAgreeOnDrop` 都 FAIL。

**洩漏實測（直接量不變式，不靠推論）**：50 萬次 `s []i64 = o` 迴圈，峰值 RSS 控制組 1,671,168 B vs 本版 1,687,552 B（差 16 KB ＝ 雜訊）；若每圈洩漏一份 24 B 載荷，差距會是 ~12 MB。

**仍未實作**：真正的「路徑敏感首寫 clone 插入」＋配套的**借用別名（不 drop）**與**所有者 drop 延後**。§1.2.1 更正 2 已說明照字面實作會改變可觀測行為並雙重 free，必須連同那兩個機制一起做；在此之前，所有**會被寫入且源仍活著**的別名都維持賦值點 clone（保守超集）。

### 1.3 修正 B：RC 的作用域只有協程 spawn 這一條邊

一個「不可靜態確定的逃逸邊界」需要同時滿足：**別名跨出線性上下文**，且**跨出後的存活順序不可靜態判定**。

nolang 目前有幾種候選邊界，逐一檢視：

| 候選邊界 | 是否存在 | 是否需要 RC |
|---|---|---|
| 函數返回 | 存在 | **不需要**——返回值走 move（`out = x`），呼叫方接管，順序確定 |
| 存入全域 | 存在 | **不需要 RC**——全域生命期 = 進程生命期，順序確定（`emitGlobalHeapFree` 在 `main` ret 前） |
| 存入容器 | 存在 | **不需要 RC**——容器是值的所有者，順序確定 |
| 切片視圖逃逸 | 存在 | **不需要 RC**——`demoteUnsafeSliceViews`（`analysis.go:278`）已在「逃逸且可能被覆寫」時降級為自有拷貝 |
| 閉包捕獲 | **不存在** | MIR 後端未實作 `KFuncLit` |
| 真執行緒 | **不存在** | 協作式單執行緒調度 |
| **協程 spawn（`OpRun`）** | **存在** | ✅ **需要 RC** |

**結論**：RC 只需覆蓋 `OpRun` 這一條邊。這讓工程量可控，也讓「先做靜態檔、後做 RC」的分階段路線成立。

#### 1.3.1 實作現況（2026-09-30）——本節結論正確，但**範圍比預期更窄**

**實作了什麼。** `%nolang_hdr = { i64 rc, i64 flags }`（16 bytes，置於 data 之前）與三個執行時原語
`@nolang_rc_alloc` / `@nolang_rc_retain` / `@nolang_rc_release` 已落地（`codegen.go` 的 `emitAsyncScheduler`）。
**唯一被提升到 R 檔的物件是 `%task` 本體**——也就是 §4.1 的 `OpRun 的結果 → R` 這一條約束。

計數紀律（**每個「引用」一個計數**）：

| 事件 | 動作 |
|---|---|
| `run f(x)` | `@nolang_rc_alloc(i64 32)` → `rc = 1`（回傳的 handle 持有的那一個） |
| handle 的**複製**（`h2 = h`） | `OpTaskRetain` → `@nolang_rc_retain`（新增 MIR op） |
| `awy h` | `@nolang_rc_release` → 歸零時才 free |

**這修掉了一個 P0 沒修好的真實缺陷。** P0 的做法是「`awy` 無條件 free 三個容器，然後把被 await 的
**槽**歸零」，但 handle 只是一個 i64，**槽歸零不等於引用歸零**：

```no
h = run dbl(21)
h2 = h          ; 舊版：這裡什麼都沒發生
print(awy h)    ; 舊版：free 掉 task，把 h 的槽歸零 —— 但 h2 的槽還指著已釋放的記憶體
print(awy h2)   ; 舊版：在已釋放的 task 上操作 → 實測 SIGSEGV（印完 42 之後崩）
```

修正後 rc 1 → 2 → 1 → 0，兩個 `awy` 都讀到有效結果，最後一個才釋放。實測：別名鏈
（`h3 = h2`）在舊版 SIGSEGV，新版正確印出三次 42。

**槽歸零仍然保留**，因為它處理的是另一半問題：**同一個槽**被 await 兩次（`awy h; awy h`）——
RC 對它無能為力（兩次讀到同一個非零指標），只有歸零能讓第二次走守衛分支。

**`@nolang_rc_release` 回傳 `i1`**（是否歸零）。`awy` 據此分支，只在歸零時 free 那兩個**借用**的容器
（args struct 與 result buffer）。⚠️ **`%task` 本體不在那裡 free**：`release` 已經 free 了配置塊的
**base（`data - 16`）**，再 free `data` 就是 free 一個已經消失的塊的**內部位址**——實作時就是這樣
先撞到 `trace/BPT trap` 的。

**本節的「RC 只覆蓋 spawn 這一條邊」是對的，但真正的瓶頸在別處。** §4.4 對參數的規定是
「retain 參數；release 於任務尾」，那需要**參數自己的 buffer 帶 header**，也就是參數的
**配置點**必須改用 `rc_alloc`。而參數的配置點散在 `@str_from_const` / `@str_concat` / `@str_clone` /
`vecDeepClone` / 各種回傳路徑——**這才是 §3.4 所說的「28 個 `@malloc` 站點」那筆帳**，不是 `%task`。
所以：

- 本輪落地的是 **P4 ＋ P5 的 `%task` 那一列**（編譯器對 task 的配置有完全控制權，不需要 tier 推斷）。
- **參數側仍是 P0 的深拷貝**（`asyncArgOwnedCopy`）。要換成 retain，前置條件是把配置點改成
  header-aware，屬 §4.1 tier 推斷 ＋ P4 的完整版。
- ⚠️ 一個**不改 ABI** 的中間方案值得先評估：把 `run` 的參數視為 **move**（源在 spawn 之後
  已死時零拷貝轉移所有權，仍活時才拷貝）——`insertDrops` 已經有這套 liveness 判定，且 §3.3 明確說
  「move 是計數中性的」。它的代價是需要一條新的跨層協議（analysis 的豁免決定要傳給 codegen），
  以及「參數位置被 consume」的 drop 抑制。**未實作**。

**已知取捨**：一個**從未被 await 的 handle 引用**會使計數停在 1 而洩漏（例如 `h = run f(); h2 = h`
之後只 await `h`）。這與舊行為同級（舊版未 await 的 handle 本來就整組洩漏），不是迴歸；要收掉它需要
handle 成為一個**有 `OpDrop` 的 owned 值**，那又是另一層改動。

**量測**：300k 次 spawn+await 的 RSS，舊 8.40 MB → 新 7.96 MB，輸出完全相同（無洩漏）；
`tests/async*.no` 與 `tests/module-async.no` 逐位元組相同；10 個差異探針中只有新的別名案例改變
（SIGSEGV → 正確輸出）。

### 1.4 修正 C：「單協程任務」的判準是 spawn graph，不是計數

「只有一個協程任務就按線性執行」——這個直覺對，但「一個任務」不等於「線性」。反例：

```
h = run f-async(s)
s = 'overwrite'      ; 參數 buffer 被提前 free → 任務讀到懸空（§2.3.2 已實測）
v = awy h
```

只有一個任務，但**兩個執行上下文的存活期重疊方式**使得「誰最後釋放」不可靜態判定。

**正確判準**（spawn graph 線性化，四條全滿足才降級為靜態檔）：

1. handle **不逃逸**：不被存進容器/全域、不被返回、不作為參數傳出；
2. 從 spawn 到函數退出的**每一條路徑**恰好經過一次該 handle 的 `OpAwait`；
3. callee **不再 spawn**（無嵌套 `OpRun`）；
4. 跨越邊界的值在 await 之後**不再被任一方使用**。

滿足時：協程等價於同步呼叫。**這個快速路徑已經實作**——`emitAsyncAwait`（`codegen.go:12593`）在未完成時直接同步呼叫 `resume_fn`，註釋明確寫道「for flat (top-level) await trees … the synchronous drive yields byte-identical output to the legacy event loop without ever entering `nolang_async_run`」。

### 1.5 既有缺陷修復：克隆欄位讀取不得再投影回來源（2026-10-01）

§1.2 的「首寫 clone」在 `emitGetField` 那一側已經落地一種形狀：**owned `str` 欄位被讀出時
`str_clone`**，讓讀出的暫存擁有自己的堆緩衝（否則暫存與欄位共用一個 buffer，兩邊都 drop →
double free）。

但 `lvalueAddrOf`（`codegen.go:7499`）把**每一個** getfield 都當成投影，把 `&struct.field`
交給呼叫方——那是**來源那一份**的位址。來源一旦被 drop，`contains` / `trim` / `set-byte`
全部讀到已釋放的記憶體。

**可觀測症狀**：`std/markdown.no` 的表格渲染不確定。`line.body.contains('|')` 在已釋放的
buffer 上回傳 false，`| Alice | 30 |` 就掉進段落分支，同一份輸入時而印 `<tr>` 時而印
`<p>| Alice | 30 |</p>`。發生率取決於堆佈局（pristine HEAD `5b88ab53` 約 1.5%，
`NOLANG_OPT_LEVEL=-O0` 下 **100% 可重現**，`-O3` 約 25%）。

> **量測方法**：`-O0` 讓這個 heisenbug 變成確定性，才可能做歸因。
> `NOLANG_MIR_NO_LVALUE=1`（關閉全部投影）在 `-O0`/`-O3` 都是 200/200 正確——
> 確認來源就是投影，但**不能用它當修法**：那會把 `json.set` / `vec.insert` 這類
> 依靠投影寫回的 std 方法全部打回原型的無動作 bug。

**修法**：`getFieldWasCloned` 判定「這次的讀取是克隆」，`sourceAlreadyDropped` 判定
「來源（沿整條投影鏈，不只是直接接收者）的 drop 是否已經發出」。兩者同時成立才停止投影，
退回該值自己的槽位（持有克隆）。

> ⚠️ **「已 drop」不等於「已死」**。這一點是修法的成敗關鍵：
> `w.b.s.set-byte(1, 88)` 裡的中間值 `w.b` 在 getfield 之後也沒有任何後續使用（已死），
> 但它的 drop 被排在函式末尾——**遠在呼叫之後**——所以投影位址仍然有效，而且它是
> 「寫入能碰到欄位」的唯一途徑。第一版用「來源是否在讀取點仍存活」當條件，
> 結果 `tests/set-byte-receiver-writeback.no`、`len-assign-grow.no`、
> `std-byte-indexing.no`、`path-char2.no`、`path-clean-dotdot.no` 五檔
> 靜默丟失寫入（金標 SAME 462→456、DIVERGE 7→13）。只有 drop 的**實際發出位置**
> 能決定 buffer 何時被釋放。

**驗收**：`NOLANG_OPT_LEVEL=-O0` 下 0/200 → **200/200**；上述五檔與控制組逐位元組相同；
`tests/markdown.no` 多出一條原本會失敗的斷言 `ok: table`。

---

## 2. 現狀基線（實測）

### 2.1 現行決策點與實作位置

| 機制 | 位置 | 觸發 |
|---|---|---|
| 借用逃逸檢查 | `analysis.go:215` `checkBorrowEscapes` | **僅 `OpBorrow`**（`&x` 視圖綁定，`hir2mir.go:9480`） |
| 切片視圖降級 | `analysis.go:278` `demoteUnsafeSliceViews` | 逃逸 **且** 可能被覆寫 |
| drop 插入 | `analysis.go:1534` `insertDrops` | 每個 owned 局部值、每條 CFG 路徑、最後使用之後 |
| drop 數量驗證 | `analysis.go:2402` `checkDropCount` | 「每個 owned 局部恰好一個 drop」 |
| 棧槽重綁定 | `memory.md` §棧類型 move | 源後續**完全無引用**才允許 |

### 2.2 記憶體佈局（ABI 現狀）

```
%str-long = type { i64, i64, i8* }        ; len, cap, data        (codegen.go:1875)
%vec      = type { i64, i64, i64 }        ; len, cap, data        (codegen.go:1876)
%option   = type { i64, [N x i64] }       ; tag, payload slot     (codegen.go:1878)
%task     = type { void (i8*)*, i64, i1, i1, i8* }  ; 32 bytes    (codegen.go:1893)
```

**關鍵事實：`data` 是裸指標，配置塊沒有 header。** `@str_clone` 就是 `malloc(len+1)` 後直接 memcpy（`codegen.go:2457`）。

**這件事決定了 RC / runtime-CoW 的可行性**：要嘛引入 header（ABI 變更，`codegen.go` 內 28 個 `@malloc` 站點與 18 個 `@free` 站點要統一走新的 alloc/release 入口），要嘛用 side table（單執行緒下不需鎖，但每次複製/釋放都要 hash 查表——對系統語言不可接受）。

→ **本方案採用 header，但只在 C/R 檔；S 檔完全不變**（§3.4）。

### 2.3 async 邊界缺陷（三個已實測 ＋ 一個附帶發現）— 修復狀態

| 小節 | 缺陷 | 狀態（2026-10-01） |
|---|---|---|
| §2.3.1 | 同 handle await 兩次 → SIGSEGV | ✅ **已修**：P0 槽歸零 ＋ 修正 B RC（§1.3.1） |
| §2.3.2 | spawn 後重賦值堆參數 → 靜默 UAF | ✅ **已修**：P0 `asyncArgOwnedCopy` 深拷貝 |
| §2.3.3 | handle 從不 await → 洩漏 | ⚠️ **降為已文檔化取捨**：修正 B 後 rc 停在 1（§1.3.1 末段） |
| §2.3.4 | ready queue 固定 256 槽、無溢出檢查 | ✅ **已修**：改為可增長佇列（`codegen.go:11739`） |

以下四個小節保留**當時**的實測與根因（作為修復的對照組），並在各節開頭標註現況。

#### 2.3.1 同一 handle await 兩次 → SIGSEGV（✅ 已修）

```no
work-async = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

probe = () {
    h = run work-async(21)
    a = awy h
    print(a)
    b = awy h      ; 第二次：容器已被第一次 free
    print(b)
}

probe()
```

實測輸出：

```
42
Error: signal: segmentation fault
rc=1
```

**根因**：`emitAsyncAwait`（`codegen.go:12593`）在讀完結果後無條件釋放三個容器：

```
call void @free(i8* %aawy.freeres)    ; 結果緩衝區
call void @free(i8* %dataI8)          ; args 結構
call void @free(i8* %aawy.freetask)   ; %task 本體
```

但 handle 只是個 `i64`（`ptrtoint i8* -> i64`，`codegen.go:12534`），**沒有「已釋放」狀態**。第二次 `awy` 走同一條路徑，對已釋放指標再 free 一次。

> ✅ **修復**：兩層。P0 在 await 後把 handle 的**槽歸零**，第二次 await 走守衛分支（處理「同一槽 await 兩次」）；
> 修正 B 再把 `%task` 提升到 R 檔（`@nolang_rc_alloc(i64 32)`，`codegen.go:12498`），handle 的每次**複製**
> 發 `OpTaskRetain`（`codegen.go:12572`），`awy` 改為 `@nolang_rc_release` 且只在歸零時 free（`codegen.go:12701`）
> ——處理「同一 handle 經別名 await 多次」。兩者互補，缺一不可（§1.3.1）。
> 迴歸釘：`src/mir/async_rc_handle_test.go`、語料 `tests/async-handle-alias.no`。

#### 2.3.2 spawn 後重賦值參數 → 靜默資料損壞（UAF）（✅ 已修）

```no
echo-async = (s str) (r str) {
    r = s
}

probe = () {
    s str = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'
    h = run echo-async(s)
    s = 'BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB'   ; 舊 buffer 在此被 free
    v = awy h
    print(v)
}

probe()
```

A/B 對照（唯一差異是那一行重賦值）：

| 版本 | 輸出 |
|---|---|
| 對照組（不重賦值） | `AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA` ✅ |
| 實驗組（重賦值） | 40 個 `NUL`（`^@`×40）❌，**rc=0，無任何診斷** |

**根因**：`emitAsyncRun`（`codegen.go:12310`）對每個參數做的是**位元複製**：

```
%arun.argbuf = call i8* @malloc(i64 24)
store %str-long %areg, %str-long* %arun.argbuf.t    ; 只複製 {len,cap,data} 描述符
```

`data` 指標與呼叫方**共享**。而 `insertDrops` 的 move 豁免清單**不含 `OpRun`**（只列了 `OpMove`/`OpOptionWrap`/`OpStrFromVec`/`OpEnumNew`/`OpSetField`），所以呼叫方的局部值仍保有自己的 drop → 重賦值時 free 掉任務還握著的 buffer。

同時，callee 的參數是「借用」語義（`insertDrops` 從不 drop 參數），所以任務**不會**釋放它。**唯一所有者是呼叫方，而呼叫方的生命期可能早於任務。**

> ✅ **修復（P0）**：`emitAsyncRun` 對堆擁有型別參數改走 `asyncArgOwnedCopy`（`codegen.go:12219`，
> 呼叫點 `:12433`）——`str` 用 `@str_clone`、`vec` 用 `vecDeepClone`、heap-option 遞迴深拷貝，
> 每個 argbuf 因此**擁有自己的 buffer**。argbuf 的釋放仍在 wrapper 內、與深拷貝配對（`:12340` 的註釋）。
> 參數側的 `retain`（§4.4 的完整版）**未做**，前置條件見 §1.3.1 末段。

#### 2.3.3 handle 從不 await → 洩漏（⚠️ 降為已文檔化取捨）

`@malloc` 的三個容器（task / args / 各 argbuf）唯一的釋放點就是 `emitAsyncAwait`。因此：

- handle 被丟棄、或存入容器後從不取出 await → task + args + argbufs **全數洩漏**；
- handle 被存進 `[]i64` 之類的容器 → 容器本身是 `i64` 值，不觸發任何 drop。

> ⚠️ **修正 B 之後的現況**：`%task` 已有 RC header，但**從未被 await 的 handle 引用**會使計數停在 1
> 而洩漏（例如 `h = run f(); h2 = h` 之後只 await `h`）。這與舊行為**同級**（舊版未 await 的 handle
> 本來就整組洩漏），**不是迴歸**。要收掉它需要 handle 成為一個**有 `OpDrop` 的 owned 值**——
> 那是另一層改動，見 §1.3.1 末段。

#### 2.3.4 附帶發現：ready queue 曾是固定 256 槽環形佇列，無溢出檢查（✅ 已修）

> ✅ **修復**：`@nolang_ready_q` 已從 `global [256 x i8*]` 改為**堆配置、可增長**的佇列
> （`codegen.go:11739`）：容量按需倍增，`@nolang_ready_grow`（`codegen.go:11763`）在倍增時把存活視窗
> `[head, tail)` 壓實到索引 0，因此環永遠不會追上自己、任何條目都不會被覆寫。

以下是修復前的原始描述（保留作對照）：

`nolang_async_enqueue` 對固定 `[256 x i8*]` 寫入時只做 `urem 256`，**不檢查 head/tail 是否追上**。若同時存活超過 256 個未完成的 task（例如在迴圈裡 `run` 而不 await），會**靜默覆寫**佇列條目——任務遺失或指標錯亂。這與所有權模型無關，但屬於同一邊界的健全性缺口。

---

## 3. 目標模型

### 3.1 檔位語義表

| 檔位 | 名稱 | 成立條件 | 表示法 | 寫入行為 | 運行時開銷 |
|---|---|---|---|---|---|
| **S** | 靜態唯一 | 分析可證：該 buffer 在其生命期內**從不與其他活躍引用別名** | 裸指標（**與現況完全相同**） | 直接寫 | **0** |
| **C** | 靜態 CoW | 存在別名，但別名與寫入**都在同一線性上下文**，且分析能定位「首次寫」 | 裸指標（**與現況完全相同**） | 首次寫之前插入一次 clone，之後獨佔 | **0**（clone 只在真有寫時發生；無分支） |
| **R** | 引用計數 | 別名跨出線性上下文（**協程 spawn**），存活順序不可靜態判定 | header + refcount | 寫入前檢查唯一性，或複製時 retain | 每次複製/釋放一次 inc/dec |

**核心原則：檔位是「值」的靜態屬性，由分析推斷，並由驗證器強制。**

S 與 C 共用**完全相同的 ABI**（裸指標）——它們的差異純粹是編譯期決定「要不要在首寫前插 clone」。只有 R 需要 header。

這帶來兩個好處：

1. **P1/P2 階段（S 與 C）不需要任何 ABI 變更**，可以直接用金標驗證「輸出位元組全等」，風險極低。
2. R 的引入是**加法**，不動既有路徑，可用同一套 A/B 手法歸因。

### 3.2 檔位晶格與推斷

定義偏序 `S ⊑ C ⊑ R`（join = 取上界）。直覺：S 的保證最強，R 最弱；一個值只要**任何一個**使用點需要較弱的保證，整個值就取該檔位。

**為什麼要定點而非單遍掃描**：別名關係是傳遞的。`a` 別名給 `b`，`b` 存進 `c`，`c` 跨協程邊界 → `a` 也必須是 R。這是個單調資料流問題，用 worklist 定點求解即可（值數量 = 函數內 SSA 值數，規模很小）。

### 3.3 指令語義表（對照現行）

| 場景 | 現況 | 目標（S/C） | 目標（R） |
|---|---|---|---|
| `b = a`，`a` 之後**只讀** | clone（賦值點） | **零拷貝別名** | `retain(a)`，零拷貝 |
| `b = a`，`a` 之後**被寫** | clone | 首寫前 clone | `retain` + 寫時檢查唯一性 |
| `b = a`，`a` 之後**已死** | move（零拷貝，源豁免 drop） | 不變 | `retain`（或移轉，見下） |
| `out = x`（返回值） | move | 不變 | 不變（move 是計數中性的） |
| `v.push(x)` | 深拷貝元素 | 不變（容器是所有者） | 若容器為 R：`retain` 元素 |
| 函數結束 | `OpDrop` | 不變 | `OpRelease`（計數歸零才 free） |
| `run f(x)` | 位元複製參數（**bug**） | 線性化 → 同步呼叫，不變 | `retain` 參數；`release` 於任務尾 |
| `awy h` | 無條件 free 三容器（**bug**） | 恰好一次 | `release(handle)`，計數歸零才 free |

**「move 是計數中性的」** 是關鍵設計：移轉不改變活躍別名數，所以 R 檔的 move 既不需要 inc 也不需要 dec。這讓 R 檔可以**完全重用**既有的 move 分析，只是把 `OpDrop` 換成 `OpRelease`。

### 3.4 ABI：header 設計（僅 R 檔）

```
struct nolang_hdr {          ; 16 bytes，置於 data 之前
    i64 rc;                  ; 活躍別名數，>= 1
    i64 flags;               ; bit0: RC 模式；bit1: 靜態/借用（永不 free，永不 clone）
}
; data = (u8*)base + 16
```

- **S / C 檔完全不變**：`malloc(n)`、`data = base`、`free(data)`。`codegen.go` 既有 28/18 個站點不動。
- **R 檔**：`alloc(n)` = `malloc(16+n)`，`data = base+16`；`release(p)` = `base = p-16`，`if (--hdr->rc == 0) { 遞迴釋放子項; free(base) }`。
- **借用（borrowed）sentinel**：`flags.bit1 = 1` 表示「非本編譯器配置」（FFI 回傳、`cap==0 && data!=NULL` 的 str→[]byte 視圖、字串常量）。`release` 對它直接返回，`clone` 對它產生一個真正 R 檔的新塊。**這條必須有**——否則會重現歷史上 `ensureVecBuffer` 誤 free 借用視圖的 `trace/BPT trap`。

**不需要 runtime tag 區分 S/C/R**：檔位是編譯期已知的，每個 alloc/free/clone 站點都用自己的檔位編譯。這正是「靜態檔零開銷」能成立的原因，也帶來一條必須由驗證器守住的不變式（§3.5）。

### 3.5 不變式（驗證器必須強制）

| # | 不變式 | 違反後果 |
|---|---|---|
| I1 | 每個值**恰好一個**檔位 | 對裸指標做 header 存取 → 讀寫錯位 |
| I2 | S/C 值不得在未經顯式提升的情況下存入 R 容器 | 容器釋放時 free 錯位址 |
| I3 | 每個 `retain` 都有配對的 `release`；每條路徑上 `#retain == #release + 1`（+1 = 建立時的計數） | 洩漏或 UAF |
| I4 | S/C 值在每條路徑上恰好一次 `OpDrop` | 洩漏或 double free（**現行 `checkDropCount` 已守**） |
| I5 | `release` 只在計數歸零時 free，且 free 的是 `base` 而非 `data` | heap 損壞 |
| I6 | 借用 sentinel 永不 free、永不 clone 原地 | free 非本編譯器記憶體 |

---

## 4. 演算法

### 4.1 Tier 推斷（單調定點）

```
輸入：函數 f 的 MIR
初始化：每個值 → S
worklist ← 所有值
重複直到收斂：
    對每個 v：
        t = tier(v)
        for 每個使用點 u：
            t ⊔= constraint(u, v)
        if t != tier(v): tier(v) = t; 把 v 的所有別名加入 worklist

constraint(u, v)：
    OpRun 的參數/結果          → R      ; 唯一的 R 來源
    存入 R 檔容器              → R
    被 R 檔值別名              → R      ; 傳遞性
    別名且別名後有寫入          → C
    僅別名且只讀               → S      ; 讀不提升檔位
    其他                       → S
```

**收斂性**：晶格高度為 3，每輪只升不降，最多 2 輪 × |值| 次。

### 4.2 Tier C：首次寫 clone 插入

這是把現行「賦值點 clone」改成「首寫點 clone」的核心改動，也是**唯一改動 `insertDrops` 語義**的部分。

```
對每個值 v，tier(v) == C：
    找出 v 的「別名集合」A(v) = { 與 v 共享 buffer 的所有值 }
    對 A(v) 中每個值 w，找出 w 生命期內的第一個寫入點 W(w)
    若存在某個 w 使 W(w) 存在，且在该點 v 仍活（liveOut 或塊內後續非 drop 讀取）：
        在 W(w) 之前插入 clone：w = clone(v)
        （之後 w 獨佔；A(v) 中其他成員的後續寫各自判定）
```

**保守性要求**：只要分析無法證明「在 W 時 v 已死」，就必須插 clone。這是**安全方向**——多插 clone 只損失效能，少插 clone 是 UAF。

**可複用的既有件**：

- `readNonDropAfterInBlock`（`analysis.go:1073`）——「之後是否還有真正的資料讀取」，`OpDrop` 不算讀取（這個區分是既有正確性的一部分，見 `analysis.go:1600` 的註釋）
- `liveOut`（`m.Liveness`）
- `emitClone`（`codegen.go:5933`）——已有 `%str-long`（`@str_clone`）、`%vec`（`vecDeepClone`）、heap-option 三條路徑

**驗收標準**：金標掃描中 **SAME 桶不得減少**、**REGRESS 必須為 0**，且所有 clone 數量的變化必須能用「原本在賦值點 clone、現在搬到了首寫點」逐檔解釋。

### 4.3 Escape / spawn graph 與線性化判定

```
建圖：節點 = 函數；邊 = OpRun（spawn）
對每個 spawn 邊 (caller → callee)：
    linearizable = handle 不逃逸(caller 內)
                 && ∀ 路徑 caller.exit：恰好一次 OpAwait(該 handle)
                 && callee 無 OpRun
                 && 跨越邊界的值在 await 後未被任一方使用
    if linearizable: 標記為「同步」→ 該邊的值維持 S/C，不提升到 R
    else:            該邊的值提升到 R
```

**「handle 不逃逸」的可判定形式**（保守）：
- handle 值只被 `OpAwait` 使用，且
- 不被 `OpSetField` / `OpIndexStore` / `OpStore` 到容器或全域，且
- 不作為呼叫參數傳出，且
- 不作為返回值。

**注意**：判定必須**按 spawn 邊**而非按 handle 值。同一個函數裡一個線性化的 spawn 和一個逃逸的 spawn 可以並存，前者不應被後者拖進 R 檔。

### 4.4 RC 插入

在 `insertDrops` 的同一趟完成，複用同一份 liveness：

| 位置 | 插入 |
|---|---|
| 建立 R 值（alloc / spawn 的參數 / 跨邊界返回） | `rc = 1` |
| R 值的**別名**（讀取、存入容器、傳給另一個 spawn） | `retain` |
| R 值的 move | 不插入（計數中性） |
| R 值的生命期結束 | `release` |
| 協程任務尾 | 對每個 R 參數 `release` |
| `awy h` | `release(handle)`（**不再**無條件 free 三容器） |
| `%task` 本體 | 併入 R：`run` 時 rc=1（handle），容器內指標為借用（不 retain），`release` 歸零時 free task+args+argbufs |

**「參數為借用」** 是這裡的關鍵設計：args 結構與 argbuf 由 task 獨佔持有（rc=1，生命期 = task 生命期），不參與 RC；**它們內部的堆指標指向的 buffer** 才是 R 值，需要 retain。這樣既修好了 §2.3.2 的 UAF，也修好了 §2.3.3 的洩漏。

### 4.5 驗證器

| 驗證器 | 對應不變式 | 位置 |
|---|---|---|
| `checkDropCount` → `checkRefBalance` | I3, I4 | `analysis.go:2402`（擴充） |
| `checkTierSoundness`（新） | I1, I2 | 新增，掛進 `ValidateTypes` 的硬錯誤清單 |
| `checkReleaseTarget`（新） | I5, I6 | 新增 |

**新增診斷必須四處齊備**（見 skill）：`checker.go` 的 `ValidateXxx` + `lint.go` 的 `RunAllLints` + `build/transpiler.go` 的硬錯誤清單 + `TraceID`。⚠️ 若驗證器放在**新檔案**，`Makefile` 的 `TRACE_ID_FILES` 不含它，`TraceID: "PLACEHOLDER"` **永遠不會被替換** → 必須硬編碼字面 id。

⚠️ **驗證器的假陽性在此是硬編譯失敗**（`rep.HasErrors()` 會 gate codegen）。歷史上 `checkDropCount` 用錯謂詞導致 14 個檔案無法編譯。**新驗證器必須與 `insertDrops` 共用同一組判定謂詞**，不得各寫一份。

---

## 5. 分階段路線圖

每階段都有獨立的驗收門檻與回滾點。**P0 與 P1–P2 之間無依賴**，可並行。

### P0 — 修 async 邊界缺陷（不引入新 ABI）

**目標**：讓 §2.3 的三個缺陷消失。這是純 bug 修復，可獨立發布。

**做法**（最小改動版）：
1. `emitAsyncRun` 的參數搬移：對堆擁有型別參數改為 **deep clone** 進 argbuf（複用 `emitClone`/`@str_clone`/`vecDeepClone`），並在 `insertDrops` 的豁免清單**不**加入 `OpRun`（呼叫方保留自己的 drop，兩邊各自獨立）。
2. `%task` 加一個「已 await」狀態位（`%task` 第 4 個欄位已是 `cancelled`，可擴充為 `flags`，或新增欄位），`emitAsyncAwait` 在 free 前檢查並置位；重複 await 走 no-op 分支。
3. 加一個「已釋放」防護：handle 歸零（`store i64 0`）表示已 await，第二次 await 直接返回零值或報硬錯。

**驗收**：
- 新語料 `tests/async-ownership.no`（見附錄 A），在修復前**必須失敗**、修復後通過；
- 金標：SAME 不減、REGRESS=0；
- 三個探針（`/tmp/probe_async_doubleawait.no` 等）全部轉綠。

**回滾**：改動集中於 `codegen.go` 的 `emitAsyncRun`/`emitAsyncAwait` 兩個函數，`git checkout` 即回滾。

> **實作進度（2026-09-30）：已完成**。
> - §2.3.1 的槽歸零與 §2.3.2 的 `asyncArgOwnedCopy` 深拷貝都已落地；
> - 語料 `tests/async-ownership.no`（含 `test-double-await` 與 `test-str-arg-reassign` 兩個形狀）；
> - 五個探針在乾淨 HEAD 上重現（SIGSEGV / 40 個 NUL），修復後全部轉綠；
> - ⚠️ P0 的槽歸零**只解決「同一槽」重複 await**；**別名 handle**（`h2 = h`）的重複 await 由
>   修正 B 的 RC 接手（§1.3.1）——兩者互補。

### P1 — Tier 推斷框架（只實作 S）

**目標**：把「檔位」概念引進編譯器，但**所有值都判為 S**，行為與現況位元組全等。

**做法**：新增 `tier.go`，實作 §3.2 的晶格與 §4.1 的定點；在 `insertDrops` 之前執行；所有 `constraint` 先返回 S。

**驗收**：**金標 0 差異**（用 `/tmp/mir_golden/fp.txt` 逐檔比對 `(rc, sha256)`，必須「N 檔中 0 檔變動」）。這是純重構的證明。

> **實作進度（2026-10-01）：已完成（報告-only）**。
> 新增 `src/mir/tier.go`：`Tier`（S/C/R）、`joinTier`（晶格上界）、`tierConstraint`（P1 全部回 S）、
> `inferTiers`（§4.1 的單調定點）、`DumpTiers`（`NOLANG_MIR_TIER=1` 時把每個函式的檔位分佈印到 stderr）。
> 在 `Analyze` 中緊接 `DumpSpawnGraph` 之後呼叫；**預設不輸出**，因此對編譯輸出 0 差異。
> 迴歸釘 `src/mir/tier_test.go`（5 測：晶格 join、P1 全 S 契約、`inferTiers` 全 S、不修改模組、預設靜默）。
>
> ⚠️ **兩個實作細節值得記下**：
> 1. **`inferTiers` 必須顯式 seed 每個值**。`TierS` 是晶格的零值，所以「只寫入上升的值」會讓全 S 的
>    推論回傳**空 map**（`len == 0`）——第一版就是這樣被 `TestInferTiersIsAllS` 的 vacuous 守衛抓到。
> 2. 定點放在 `Analyze` 的**報表端**（與 `DumpSpawnGraph` 同位置）而非 `insertDrops` 之前：
>    P1 的結果沒有任何消費者，放在報表端可保證 0 成本、0 差異；P2 讓它變成 load-bearing 時再前移。
>
> **A/B 驗收（同一瞬間 `rsync` 快照；A=含 P1、B=不含 P1、A-env=含 P1 且開啟 dump）**：
> 64 個語料檔（`tests/async*.no`、`module-async`、`markdown`、`mem-safety/*` 等）三者
> `(rc, stdout)` **全部逐位元組相同 → 0 差異**；`go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` 全綠。

### P2 — Tier C：首寫 clone 插入

**目標**：把 clone 從賦值點搬到首寫點。

**做法**：實作 §4.2；`constraint` 加入「別名且別名後有寫入 → C」。

**驗收**：
- 金標：SAME 不減、REGRESS=0；
- **所有 clone 數量變化逐檔可解釋**（用 `NOLANG_MIR_DUMP_MIR=1` 統計 `clone` 指令數，前後對比）；
- 預期方向：clone 數量**只減不增**（首寫點比賦值點更晚、更精確）。

**風險**：若某檔 clone 數量**增加**，說明 liveness 查詢點的搬移引入了不保守的判定 → 必須查清後才前進。

> **實作進度（2026-09-30）**：已落地**安全子集**——「只讀別名零拷貝」，見 §1.2.1。
> 新增 pass `forwardReadOnlyAliases`（`src/mir/alias_forward.go`），在 `Analyze` 中於 `insertDrops` 之前執行；
> 迴歸釘 `src/mir/alias_forward_test.go`（8 測：3 個正向在舊碼 FAIL、5 個危險情境在兩邊都 PASS）。
> 語料 544 檔 `OpClone` 總數 **328 → 164**，方向符合預期（只減不增）。
> **尚未實作**：真正的「首寫點」路徑敏感 clone 插入，以及與之配套的**借用別名**（不 drop）與
> **所有者 drop 延後**兩個機制。在引入這兩者之前，任何寫入型別名都維持賦值點 clone（保守超集）。

> **實作進度（2026-10-01）：補上「源已死 ⇒ move」——即「首寫點」在源已死時的特例。**
> 同一支 pass 新增一條規則（詳見 §1.2.2）：`OpClone` 且源在拷貝點之後已死 ⇒ 改寫成 `OpMove`，
> 交給 `insertDrops` 既有的 slice-move 邏輯。**這條規則只可能移除拷貝、不可能新增。**
> 迴歸釘 `src/mir/alias_forward_test.go` 追加 3 測（`TestSourceDeadAliasIsMovedNotCloned`、
> `TestSourceDeadAfterEarlierReadsAliasIsMoved`、`TestSourceLiveAfterBindingIsNotMoved`；
> 前兩者在舊碼 FAIL、第三個兩邊都 PASS）。
>
> **驗收對照（508 檔語料，`NOLANG_MIR_DUMP_MIR=1` 逐檔統計）**：
> - 金標：**rc 差異 = 0**；受影響檔案的 stdout **逐位元組相同**。
> - **clone 總數 211 → 206，增加數 = 0** ⇒ 符合「只減不增」。
> - 全部 clone 變化逐檔可解釋：`mem-safety/nested-container-clone.no` 13→9、`std-new.no` 1→0，
>   其餘 506 檔不變。兩者的收益都來自**選項剝離的重複 clone**（同一份 `?[]T` 載荷被剝離與
>   全新綁定各深拷貝一次）。
>
> 🔴 **本輪最重要的一課：規則的位置就是它的守則。** 第一版把新規則放在 (r4) 之前，
> 於是它也改寫了**借用型**來源的 clone ⇒ 雙重 free；`tagged-enum-two-match.no` 輸出改變、
> `tagged-enum-zero-match.no` 崩潰（`rc=133`）。**單元測試當時全綠**——只有語料抓得到。
> 修法是把 (r4) 上移，讓它同時守住兩個決策。
>
> ⚠️ 這條規則**更正了 §1.2.1 的一個推論**：clone 型的 `?[]T` 剝離，其目的端是**所有者**
> （`isBorrowRead` 回報「擁有」），因此規則會在它上面生效——這是本輪收益的唯一來源。
> 同時 `option_peel_ownership_test.go` 的 `peelDst` 改為**跟隨轉移鏈**（不變式是「載荷被釋放」，
> 不是「某個 value id 有 drop」），並已驗證它**仍能抓到 2026-09-25 的洩漏**。

### P3 — Escape / spawn graph 分析

**目標**：實作 §4.3 的線性化判定，產出「哪些 spawn 邊是線性的」報告。**先只報告，不改變行為。**

**驗收**：對 `tests/async*.no` 與 `tests/module-async.no` 輸出判定結果，人工核對。此時編譯輸出仍應 0 差異。

> **實作進度（2026-10-01）：已完成（報告-only）**。
> 新增 `src/mir/spawn_graph.go`：`SpawnGraph()` 對每個 `OpRun` 站點產出 `SpawnEdge`，
> 逐條評估 §4.3 的四個判準——
> 1. **不逃逸**：handle 只被 `OpAwait` / `OpTaskRetain` / 別名運算使用；`OpSetField` /
>    `OpIndexStore` / `OpStore` / 呼叫引數 / `OpReturn` / **out-param 返回** 一律算逃逸。
>    刻意保守：`async-cancel(h)` 是已知偽陽性（內建既不保存也不 retain），但不做白名單——
>    白名單正是這類分析開始說謊的方式。
> 2. **每條路徑恰好一次 await**：從 spawn 前向走 CFG 到每個出口計數；遇到回到 spawn 的
>    回邊就**切斷**（那一輪屬於下一次迭代的 handle），並記一條 note。
> 3. **callee 不再 spawn**：`spawnsTransitively` 走呼叫圖，**傳遞**檢查（只查直接呼叫會漏掉
>    兩層以下的嵌套，而 `emitAsyncAwait` 的同步快速路徑只對 flat await tree 成立）。
> 4. **跨界值在 await 之後不被使用**：從 await 點前向可達的指令中找引數的讀取；`OpDrop`
>    不算讀取（沿用 `readNonDropAfterInBlock` 的同一個區分）。
>
> 判定**按 spawn 邊**而非按 handle 值（§4.3 明確要求）：同一函式裡一個 LINEAR 與一個
> NON-LINEAR 的 spawn 並存時，前者不被後者拖累。
>
> **報告出口**：`NOLANG_MIR_SPAWN_GRAPH=1 no build …` 把判定印到 stderr；預設關閉，
> 因此對編譯輸出 0 差異（`DumpSpawnGraph` 的第一行就是環境閘門）。
> 迴歸釘 `src/mir/spawn_graph_test.go`（9 測：1 正向、4 個各釘一條判準、1 個 per-edge、
> 1 個「不修改模組」、1 個「預設靜默」）。
>
> 實測判定（`NOLANG_MIR_SPAWN_GRAPH=1`）：
> - `async.no` 10 邊全 LINEAR；`async-yield.no` 1 邊 LINEAR；`module-async.no` 1 邊 LINEAR。
> - `async-cancel.no` / `async-coop.no`：帶 `async-cancel(h)` 的那條 NON-LINEAR
>   （逃逸：傳給 `async-cancel`），其餘 LINEAR。
> - `async-handle-alias.no`：`alias-both` 2 次 await、`alias-chain` 3 次、
>   `same-slot-twice` 2 次 → NON-LINEAR；`via-container` 逃逸（`[]t.push`）+ 0 次 await；
>   `mk` 經 **out-param 返回**；`many` 因迴圈變數在 await 之後仍被使用（criterion 4）
>   → NON-LINEAR。這正是 R 檔要接手的形狀。
> - `async-ownership.no`：`test-double-await` NON-LINEAR，其餘 8 條 LINEAR。
>   ⚠️ `test-str-arg-reassign`（§1.4 的反例形狀）判為 LINEAR —— 正確：P0 在 spawn 時
>   就把 owned 引數深拷進 task 自己的 buffer，§1.4 的例子因此已由 P0 收掉，而
>   「await 之後不再使用」這個判準看的是 await **之後**；該形狀以 note 記錄而非判決。

### P4 — Header ABI 與 RC 執行時原語

**目標**：引入 §3.4 的 header，實作 `alloc`/`release`/`retain`。**但先不接到任何值上**（所有值仍是 S/C，走舊路徑）。

**做法**：在 `codegen.go` 新增 runtime helper，並把 28 個 `@malloc` / 18 個 `@free` 站點中**屬於 R 檔的**改走新入口（S/C 的不動）。

**驗收**：金標 0 差異（新 helper 未被任何程式碼引用 → 死碼，LLVM 會移除）。用 `-v` 比對 IR 的排序後差異（skill 提到宣告順序不確定，必須 `diff <(sort a.ll) <(sort b.ll)`）。

> **實作進度（2026-09-30）**：**已完成**。`%nolang_hdr = { i64 rc, i64 flags }`（16 bytes）與
> `@nolang_rc_alloc` / `@nolang_rc_retain` / `@nolang_rc_release(i8*) -> i1` 三支 helper 已加入
> `emitAsyncScheduler`（`src/mir/codegen.go`），**無條件發出**（與排程器其餘部分一致），
> 未 spawn 的程式裡是死碼。`rc == 0` 為**借用 sentinel**（依 §8 決策 #2），retain/release 對它直接返回。
> ⚠️ **仍未做的事**：28 個 `@malloc` 站點**一個都沒動**——本輪只需要 task 一個站點（見 P5），
> 其餘站點要等參數側提升時才需要。
> 註：`%task` 是唯一走新入口的配置，因此「R 檔的 malloc 站點」目前是 1 個，不是 28 個。

### P5 — 在逃逸邊界插入 retain/release

**目標**：把 P3 判定的「非線性 spawn 邊」上的值提升到 R 檔，插入 retain/release。

**驗收**：
- §2.3 的三個缺陷在此階段**由 RC 機制統一解決**（P0 的修復可以被 P5 取代，或保留為雙保險）；
- 新語料 `tests/async-rc.no`（跨邊界共享、多任務、取消後不 await、handle 存容器）；
- 洩漏量測：`/usr/bin/time -l <binary> | grep "maximum resident"`，用**可觀測**的大負載（skill 強調：資料必須被讀取，否則 LLVM 會把整個 malloc/free 對消掉）；
- 金標：REGRESS=0。

> **實作進度（2026-09-30）**：**已落地 `%task` 這一列**（見 §1.3.1）。
> - `run` → `@nolang_rc_alloc(i64 32)`，`rc = 1`；`awy` → `@nolang_rc_release`，歸零才 free 兩個借用容器。
> - handle 的**複製**新增 MIR op `OpTaskRetain`，在 `hir2mir` 的**兩條**複製路徑發出：
>   全新綁定（`l.locals[name] = fresh` 那條）與重綁定（`carryAsyncResType`，涵蓋 local ＋ module-global
>   兩個呼叫點）。**兩條都要**——只做一條會讓另一種拼法留著 use-after-free。
> - 迴歸釘 `src/mir/async_rc_handle_test.go`（**7 測**；3 個 IR 斷言在乾淨 HEAD 上 FAIL，
>   已用 `git archive HEAD` 快照實測）＋ 語料 `tests/async-handle-alias.no`（乾淨 HEAD rc=1 SIGSEGV，
>   修復後 rc=0）。
> - ⚠️ **未落地：參數側的 retain**（§4.4「retain 參數；release 於任務尾」）。前置條件與替代方案
>   見 §1.3.1 末段；目前參數仍是 P0 的深拷貝。
> - ✅ **P3 已完成（2026-10-01）**：spawn-graph 線性化判定已落地（報告-only，見 §5 P3 的實作進度）。
>   本輪 `%task` 的提升確實不需要它（編譯器對 task 的生命期有完全控制權），但它是**參數側提升**
>   （§4.4「retain 參數」）的前置條件——哪些 spawn 邊能降級為同步呼叫，決定哪些參數根本不需要 RC。

### P6 — 驗證器、語料與文檔

**目標**：`checkRefBalance` / `checkTierSoundness` / `checkReleaseTarget`；擴充語料；更新 `docs/docs/lang/memory.md`。

**驗收**：`no vet src/std` 必須 0 error；`./bin/no test test/std/` 的 FAIL 集合**逐行不變**（現況 14 失敗 / 7 檔，全為預存）。

---

## 6. 驗證矩陣

| 層次 | 手段 | 判準 |
|---|---|---|
| 單元 | `cd src && go test ./mir/ ./checker/ ./parser/ ./fmt/ ./lexer/ ./hir/` | ok |
| 端到端 | `GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default bash scripts/mir_golden.sh` | SAME 不減、REGRESS=0 |
| 純重構 | `/tmp/mir_golden/fp.txt` 前後逐檔 diff `(rc, sha256)` | 「N 檔中 0 檔變動」 |
| 洩漏 | `/usr/bin/time -l <binary>` | 大負載下 RSS 有界 |
| 診斷 | `./bin/no vet src/std` | 0 error |
| 標準庫 | `./bin/no test test/std/` | FAIL 集合逐行不變 |

**本輪實測（2026-10-01，基線 `5b88ab53`）**：

| 檢查 | 結果 |
|---|---|
| `go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | 全綠 |
| `go test ./build/` | 恰為 4 個**預存**失敗（與 HEAD 相同） |
| `no vet src/std` | **0 error / 6205 warning / 936 hint**（＝基線，未增） |
| 金標（凍結 507 檔語料，單一瞬間控制組） | 控制組 `SAME=462 DIVERGE=7 REGRESS=13`；修正後 `SAME=461 DIVERGE=8 REGRESS=13`，`IMPROVED=BOTH_FAIL=UNSTABLE=0`，`NEW=25` |
| 桶差異 | **只有 `tests/markdown.no` 由 SAME → DIVERGE**，其餘六桶逐位元組相同 |
| `sort fp.txt` 後 diff | **2 行** |
| 五個寫回敏感檔 | 與控制組逐位元組相同 |
| P3 對輸出的影響 | G sweep（含 P3）與 B sweep（不含）除 `markdown.no` 外完全相同 → **0 行為改變** |
| P1 對輸出的影響 | A/B（同快照：A=含 P1、B=不含、A-env=含 P1 且開啟 dump）64 檔 `(rc, stdout)` **全等 → 0 差異** |
| **P2（2026-10-01）對 clone 的影響** | 508 檔逐檔 `NOLANG_MIR_DUMP_MIR=1`：**clone 211 → 206，增加 0 檔**；rc 差異 **0** |
| P2 對輸出的影響 | 受影響的 2 檔（`nested-container-clone.no`、`std-new.no`）＋另外 3 檔的 stdout **逐位元組相同** |
| P2 洩漏量測 | 50 萬次 `s []i64 = o` 迴圈峰值 RSS：控制組 1,671,168 B vs 本版 1,687,552 B（雜訊級） |

> **P2 的驗收方式與 P1 不同。** P1 是純重構 ⇒ 要求「0 差異」；P2 是**行為改變** ⇒ 判準是
> 「clone 只減不增、rc 不變、受影響檔案的輸出逐位元組相同」。用 `clone` 指令數逐檔比對是刻意的：
> `insertDrops` 有權把改寫後的 move **再 clone 回來**，所以「clone 數沒變」只代表最終指令形狀相同。

> 🔴 **不能用 MIR-dump 的 sha256 逐檔比對——`drop` 的順序是不確定的。** `insertDrops` 以 Go **map**
> 迭代擺放 drop，所以同一份 binary、同一個輸入、連續 5 次會得到 **5 個不同的 sha**（實測：
> `9,6,2,1,12,16` / `1,16,6,9,2,12` / …），而**建置出的程式 stdout 每次逐位元組相同**。
> 本輪一度用 dump-sha 掃語料，結果 94 檔裡報了 **91 檔「改變」**——其中多數檔案的 pass
> `forwarded=0`、連一個 reason 計數都沒有（＝根本沒觸發）。那是**雜訊**，看起來卻像全面迴歸。
> 因此：**比 clone 數（順序無關），或比程式 stdout**（`mir_golden.sh` 正是 hash stdout 而非 MIR）。
> 若 dump diff **只有 `drop` 行在移動**、行數相同、且沒有任何 `clone`/`move` 差異 ⇒ 就是這個不確定性，
> 用「同一份 binary 跑兩次」即可確認。

> ⚠️ **`no vet src/std` 的基線在本輪期間被別的 session 改動了。** 本輪實測為
> **1 error / 6152 warning / 921 hint**；其中那 1 個 error 是 `src/std/json.no:117` 的
> `[ovfhndld]`，屬於**別的 session 未提交的 `json.no` 編輯**（用控制組 binary 跑同一條命令得到
> **完全相同**的錯誤 ⇒ 與本輪無關）。warning/hint 較基線**更低**，方向正確。

> `tests/markdown.no` 的 SAME → DIVERGE 是**改善**：控制組 11 行（缺 `ok: table`），
> 修正後 12 行（含 `ok: table`）。金標基線捕捉的是**失敗**狀態，故本檔由「與基線一致」
> 變成「與基線不一致」，方向正確。

**必守的操作紀律**（skill 已載明，此處重申）：

- ⚠️ **一次只跑一個金標掃描**（`WORKDIR=/tmp/mir_golden` 是硬編碼），並行會產生**假 REGRESS**；
- ⚠️ 掃描前後各記一次 `ls -l bin/no`，中途被別的 session 重建 → 整場作廢；
- ⚠️ **`make no` 會把其他 session 未提交的工作烘進你的 binary** → A/B 的控制組必須用 `git archive <SHA>` 的乾淨快照，且**只套用你自己的 hunk**；
- ⚠️ 改 `src/std/**` 後必須 `touch src/std_embed.go && make no`（**連改註解都算**）。

---

## 7. 風險、非目標與已知取捨

### 7.1 非目標

| 非目標 | 理由 |
|---|---|
| 循環引用收集 | RC 的固有限制（同 Rust `Rc`）。**必須寫進文檔**：用 `heap`（現為二元堆積，非 arena）或顯式斷環。若日後需要，另立「可選循環收集器」 |
| 真執行緒的原子 RC | 當前是協作式單執行緒調度，計數不需原子。未來若加 threads，用**編譯期開關**切原子版本，而非 runtime 分支 |
| 改動 `%txt` | 棧類型，無堆，不參與任何檔位 |
| 改動借用視圖語義 | `cap==0 && data!=NULL` 的視圖、FFI 回傳，維持現行 sentinel 語義 |
| 閉包逃逸 | MIR 未實作 `KFuncLit`；若日後實作，RC 邊界需新增一條（本方案的晶格與驗證器可直接擴充） |

### 7.2 主要風險

| 風險 | 影響 | 緩解 |
|---|---|---|
| **檔位判定不保守** | 少插 clone → UAF，且**靜默**（如 §2.3.2 的 40 個 NUL） | 所有判定「無法證明 → 往保守方向倒」；驗證器與插入器共用謂詞 |
| **克隆欄位的投影位址**（§1.5，已修） | owned `str` 欄位被讀出時 clone，投影卻仍指向**來源**；來源 drop 後 UAF（`std/markdown` 表格不確定） | `getFieldWasCloned && sourceAlreadyDropped` 才停止投影；判準必須是 drop 的**實際發出位置**，不是「已死」 |
| **header 引入後 ABI 混用** | 對裸指標做 header 存取 → 記憶體損壞 | I1/I2 由 `checkTierSoundness` 強制；P4 階段先讓 helper 成為死碼驗證 0 差異 |
| **`shouldUseMemcpy` 被間接改變** | 大聚合的 `loadVal` 由「回傳值」變「回傳槽指標」→ 走進從未執行過的路徑 | 任何可能改變 `computeTypeSize` / 4096 門檻 / 欄位佈局的改動**必須跑金標** |
| **驗證器假陽性** | `rep.HasErrors()` gate codegen → 整檔編不過 | 與插入器共用謂詞；每個新驗證器附「修復前必須失敗」的單元測試 |
| ~~**ready queue 溢出**（§2.3.4）~~ | ~~任務靜默遺失~~ | ✅ **已修**：改為可增長佇列（`codegen.go:11739`），不再是風險 |

### 7.3 已知取捨（必須寫進使用者文檔）

1. **RC 檔的循環引用會洩漏。** 這是確定性記憶體管理的標準代價。
2. **R 檔有 inc/dec 開銷。** 但只要不跨協程邊界，值就留在 S/C 檔——這是本方案相對「全語言 RC」的核心優勢。
3. **跨協程邊界的深拷貝**（P0 的最小修復版）比 RC 貴，但語義最簡單。P5 之後可改為 retain 以省掉拷貝。

---

## 8. 待決問題（已全部定案，2026-10-01）

| # | 問題 | 決策 | 落地情況 |
|---|---|---|---|
| 1 | P0 的修復策略：先深拷貝參數，還是直接跳 P5 的 RC？ | **先深拷貝**（可獨立發布、可獨立回滾，且給 P5 一個正確性對照組） | ✅ 已照做：`asyncArgOwnedCopy`（§2.3.2） |
| 2 | header 的 sentinel 編碼：`rc = 0` 還是 `flags.bit1`？ | **`rc = 0`** 單一判準（`release` 只需一次比較）；`flags` 保留給未來擴充 | ✅ 已照做：`rc == 0` 即借用 sentinel（`codegen.go:1905`） |
| 3 | 檔位是否對使用者可見（`#{tier=R}` 之類）？ | **先不開放**（檔位是推斷結果，開放會讓它變成契約，驗證器複雜度大增） | ✅ 未開放 |
| 4 | `%task` 欄位擴充：新增欄位還是複用 `cancelled`？ | **新增欄位**（`cancelled` 是 `i1` 且語義明確） | ✅ 已照做：`%task` 現為 32 bytes、5 欄（`codegen.go:1893`） |
| 5 | 是否處理 §2.3.4 的 ready queue 溢出？ | 原建議**報硬錯**（靜默覆寫最危險） | ✅ **已處理，但改採更好方案**：佇列本身改為**可增長**（`codegen.go:11739`），溢出根本不存在，因此無需報錯 |

---

## 附錄 A：語料清單（實際落地）

草案曾提議 7 個新檔；實作時收斂為 **2 個新檔 ＋ 既有檔案的擴充**（把形狀相近的案例放同一檔，
讓 `no test` 的斷言一次覆蓋多個判準）。實際語料如下：

| 檔案 | 狀態 | 覆蓋的形狀 |
|---|---|---|
| `tests/async-ownership.no` | **新增** | `test-str-arg-reassign` / `test-vec-arg-reassign` / `test-struct-arg-reassign`（§2.3.2）；`test-double-await`（§2.3.1）；`test-null-handle`（§2.3.3）；`test-single-await` / `test-two-handles` / `test-struct-rebind` / `test-handle-rebind(-typed)` |
| `tests/async-handle-alias.no` | **新增** | `alias-both`（2 次 await）、`alias-chain`（3 次）、`via-return`（`mk` 經 out-param 返回）、`via-container`（存 `[]t` 逃逸）、`same-slot-twice`、`many`（spawn+await 循環，計數必須平衡否則 RSS 無界） |
| `tests/async.no` | 既有 | 10 條 flat spawn 邊（全 LINEAR）——P3 的「防過度提升」對照組 |
| `tests/async-cancel.no` | 既有 | `test-cancel` / `test-no-cancel` / `test-cancelled` / `test-yield` |
| `tests/async-coop.no` | 既有 | `test-run-ok` / `test-cancelled` |
| `tests/async-yield.no` | 既有 | `test-yield-inside` / `test-yield-plain` |
| `tests/module-async.no` | 既有 | 模組層級的 async 呼叫 |

⚠️ 語料撰寫注意（skill 已載明）：`;` 是**行註解**不是陳述分隔符；`''` 是 `str`、`""` 是 `char`；切片寫 `[a..b)`；寫測試檔不必繞開 `s.len()` / `n.to-str()`，但 **option match 必須三個臂都寫**。

## 附錄 B：觸點索引

| 主題 | 位置 |
|---|---|
| drop 插入 | `src/mir/analysis.go:1539` `insertDrops` |
| 只讀別名前向 | `src/mir/alias_forward.go:198` `forwardReadOnlyAliases`（呼叫點 `analysis.go:1550`） |
| **源已死 ⇒ move（P2，2026-10-01）** | `alias_forward.go:357` `srcIsBorrow`（(r4) 上移，同時守住兩個決策）、`:393` 規則本體；統計鍵 `clone-src-dead-to-move`（`:183`） |
| 選項剝離所有權 | `analysis.go:1501` `optionSlicePeelClones`、`analysis.go:1386` `isBorrowRead`（`OpMove` 分支） |
| 選項剝離回歸釘 | `src/mir/option_peel_ownership_test.go` `peelDst`（**跟隨轉移鏈**，見 §1.2.2） |
| move 四判別式 | `analysis.go:1140` / `:1242` / `:1277` / `:2186` |
| 判別式聯集 | `analysis.go:2225` `moveExemptsSource` |
| drop 數量驗證 | `analysis.go:2402` `checkDropCount` |
| 塊內讀取判定 | `analysis.go:1073` `readNonDropAfterInBlock` |
| 借用逃逸 | `analysis.go:215` `checkBorrowEscapes` |
| 切片視圖降級 | `analysis.go:278` `demoteUnsafeSliceViews` |
| **spawn graph / 線性化（P3）** | `src/mir/spawn_graph.go` `SpawnGraph` / `evalSpawnEdge`（報告-only，`NOLANG_MIR_SPAWN_GRAPH=1`） |
| **Tier 推斷（P1）** | `src/mir/tier.go` `Tier` / `joinTier` / `tierConstraint` / `inferTiers` / `DumpTiers`（報告-only，`NOLANG_MIR_TIER=1`） |
| MIR 指令定義 | `src/mir/mir.go:376-381`（move/clone/drop/borrow）、`:485-500`（run/await/task-retain） |
| 型別佈局 | `src/mir/codegen.go:1875-1893`（`%str-long` / `%vec` / `%option` / `%task`） |
| R 檔 header 說明 | `codegen.go:1895`（`rc == 0` 為借用 sentinel） |
| `@str_clone` | `codegen.go:2457` |
| `emitClone` | `codegen.go:5933` |
| `emitIndexStore` | `codegen.go:7239` |
| `lvalueAddrOf`（現接受 `use *Inst`） | `codegen.go:7499` |
| 克隆欄位守衛 | `codegen.go:7649` `getFieldWasCloned`、`:7679` `sourceAlreadyDropped`、`:7769` `emitPos` |
| `emitGetField`（兩處 `@str_clone`） | `codegen.go:7797`（`:7901`、`:7958`） |
| `emitSetField` | `codegen.go:8063` |
| async 排程器 | `codegen.go:11737` `emitAsyncScheduler`（ready queue 在 `:11739`，`@nolang_ready_grow` `:11763`） |
| `emitAsyncRun` | `codegen.go:12310`（參數深拷貝 `asyncArgOwnedCopy` `:12219`，呼叫點 `:12433`；`rc_alloc` `:12498`） |
| `emitAsyncAwait` | `codegen.go:12593`（`release` `:12701`） |
| `emitTaskRetain` | `codegen.go:12572`（`OpTaskRetain` 定義 `mir.go:500`；發出點 `hir2mir.go:2961` / `:9635`） |
| `%task` 型別 | `codegen.go:1893`（**32 bytes**，第 5 欄 = waiter） |
| async 語義（使用者面） | `src/std/async.no`（D10） |
| 現行模型文檔 | `docs/docs/lang/memory.md`（已知限制在 `:380`） |
| `KFuncLit`（未被 MIR 消費） | `src/hir/hir.go:84`、`src/parser/tohir.go:920` |
| 迴歸釘（測試） | `src/mir/{alias_forward,async_boundary_ownership,async_rc_handle,spawn_graph,str_field_lvalue,tier}_test.go`、`src/mir/option_peel_ownership_test.go`、`src/parser/stmt_boundary_block_test.go` |
