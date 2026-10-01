# Nolang 混合所有權模型（Hybrid Ownership）— 工業級設計方案

**版本**：v1.2（2026-10-01 第四輪）
**基線**：`2a39c4e1`（2026-10-01 第四輪）。本輪所有 A/B 都用**自建快照** `/tmp/p6/no-{A,B,C,D,M,P,E}`，全程未使用 `bin/no`（並行 session 會重建它）。
**適用後端**：MIR（`src/mir`）
**前置文件**：`docs/docs/lang/memory.md`（現行模型權威描述）、`NOLANG-AUDIT-2026-09-27.md`
**改動規範**：skill `nolang-compiler-change`（金標、A/B 歸因、並行 session 陷阱）

**完成狀態（2026-10-01 第四輪）**

- ✅ **已落地並通過驗收**：§1.2.1／§1.2.2／§1.2.3（修正 A 的三條 bullet——只讀別名、源在**拷貝點**已死、
  源在**首寫點**已死）、§1.3.1／**§1.3.2**（修正 B：P4 ＋ P5 的 `%task` 列，含 void 任務與 `run <handle>`
  的漏計修復）、**§1.6（修正 B 的參數側：不改 ABI 的 move 中間方案）**、§1.4／§4.3（修正 C：P3
  報告-only）、§1.5（克隆欄位投影缺陷）、P1（Tier 推斷框架）、**P6 的 `checkRefBalance` ＋ 語料 ＋ 文檔**。
- ✅ **§2.3 的四個缺陷**：三個已修，一個（§2.3.3）降為已文檔化的取捨。**§8 的待決事項已全部定案**（8 題）。
- ⬜ **仍未落地——三項，皆為刻意，且每一項都寫明了前置條件**：
  1. **P4/P5 參數側的「header-aware ＋ retain」路線**。本輪落地的 §1.6 是這條路線的**前半**——
     「源在 spawn 之後已死 ⇒ move」（零拷貝，且**不引入新 ABI**）。剩下的後半是「源**仍活**時
     用 `retain` 取代深拷貝」，它需要參數自己的 buffer 帶 header，也就是 §3.4 說的
     「28 個 `@malloc` 站點」那筆帳，屬 §4.1 tier 推斷 ＋ P4 的完整版。
  2. **P2 剩下的 §4.2 分支**：「`v` 在 `W` 仍活 ⇒ 在 `W` 之前插 clone」。它**不改變靜態 clone 數**
     （今天在賦值點已恰好一個），收益純動態且需要值分裂＋支配分析 ⇒ **刻意不做**（§1.2.3 末段）。
  3. **P6 的 `checkTierSoundness` / `checkReleaseTarget`**：它們要守的 I1/I2/I5/I6 是 header ABI 接上
     **使用者值**之後才存在的性質，而今天唯一提升到 R 檔的仍是 `%task` ⇒ **先寫會是空檢查**
     （§4.5）。**前置條件＝第 1 項的後半。**

**一句話總結**：**S／C 兩檔（靜態唯一、靜態 CoW）已經是完整且被驗證的；R 檔只落地了 `%task` 一列，
尚未覆蓋使用者值。** 參數側（§1.6）現在分成兩半：**「源已死 ⇒ move」已落地**（零拷貝、不引入 ABI、
本輪實測 0 行為差異），**「源仍活 ⇒ retain」仍待 header-aware 配置**——所以這份文件作為
「設計方案」是完整的，作為「已實作的模型」還差第 1 項的後半。

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

> `analysis.insertDrops`（`src/mir/analysis.go:1551`）對每個 `OpMove` 問一句：**源在這次搬移之後還活著嗎？**
> - 還活著（`liveOut[blk][src]` 或塊內後續有非 drop 讀取）→ 改寫成 `OpClone`，深拷貝，兩邊各自持有、各自釋放
> - 可證明已死 → 保留零拷貝 move，源豁免 drop

四個判別式依序短路：`moveStructSharesHeap`（`analysis.go:1157`）→ `moveStrSharesHeap`（`:1259`）→ `moveSliceSharesHeap`（`:1294`）→ `moveTransfersOwnership`（`:2294`）；驗證器 `checkDropCount`（`:2510`）必須共用同一聯集 `moveExemptsSource`（`:2333`）。

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

> 實作上這不是新機制，而是把 `insertDrops` 既有的 liveness 查詢**換一個查詢點**：從「move 的源是否還活」改成「首寫時，該值是否有其他活躍別名」。判定謂詞可完全複用 `readNonDropAfterInBlock`（`analysis.go:1090`）與 `liveOut`。

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

> ⚠️ **本段已被 §1.2.3 部分取代（2026-10-01 第二輪）。** 「別名被寫入、但源在**每個寫入點都已死**」
> 那半**已經落地**並有測試釘住。仍未實作的只剩 **「源在寫入點仍活」** 那一支，而它是**刻意不做**
> （理由：不改變靜態 clone 數、需值分裂＋支配分析；見 §1.2.3 末段與 §4.2）。

#### 1.2.3 實作現況（2026-10-01，第二輪）——「源在首寫時已死 ⇒ 直接前向」

§1.2.2 補上了「**源在拷貝點**已死 ⇒ move」。本輪補上 §1.2 第三條 bullet 在一般情形下的落地：
**別名會被寫入，但源在每一個寫入點都已死**。實作位置同在 `forwardReadOnlyAliases`（放寬 (r1)），
判定集中在新增的 `aliasWritesUnobservable`。

**為什麼「源在寫入點已死」恰好是正確的判準**（不是近似、不是啟發式）。前向把別名的所有使用改成
源的使用，所以合併後的程式相對「兩份 buffer」的程式**只多了兩種觀測**：

- (i) 寫**別名**之後讀**源**；
- (ii) 寫**源**之後讀**別名**。

(ii) 就是既有的 (r2)。**(i) 的否定恰好就是「源在該寫入點已死」**（`liveOut` 為假、且該塊內之後
沒有非 drop 的讀取——與 `insertDrops` 同一組判定）。**沒有第三種情形**：同一個值內部的讀寫在
兩個程式裡逐位元組相同。所以這是**完備的判準**，不是「更精確的猜測」。

**四道額外守則**（全部只會回 false；回 false 就退回今天的賦值點 clone）：

| 守則 | 內容 | 為什麼不能省 |
|---|---|---|
| (w1) | 只做 owned slice 的 `b[i] = x` | slice 寫入穿過共用的 backing buffer，正是 §1.2 描述的情形；struct 欄位寫、slot 寫、重新綁定維持保守 |
| (w2) | 源**必須擁有自己的 buffer**（`!isParamValue`） | **slice 參數是呼叫方的 buffer**——`mutate(v []i64) { v[0] = 99 }` 真的會改到呼叫方——所以寫入型前向必須拒絕參數。只讀型可以，因為它不寫 |
| (w3) | 源**恰好被複製一次**（就是這一條） | 第二份副本會成為這些寫入的觀測者 |
| (w4) | **別名自己沒有被複製**（`c = b`） | `c = b` 會被 `rewriteUses` 改成 `c = a`，然後可能自己也被前向，變成第三個觀測者——(w3) 數的是「源的副本」，看不到這個 |

(w3)/(w4) 建立在**改寫前**的指令流上（`copiesOf` 在唯一的那趟掃描裡建立），所以判定**與候選的
走訪順序無關**——否則「先前向 `c` 還是先前向 `b`」會給出不同答案。

**每一道守則都有實測的 miscompile 對照。** 把 `aliasWritesUnobservable` 的第一行改成
`return true`（＝移除全部守則，只保留「別名被寫入就前向」），同一組探針在兩顆 binary 上的 stdout：

| 探針 | 控制組（有守則） | naive（無守則） | 擋下它的守則 |
|---|---|---|---|
| 寫別名後再讀源 | `1 1 99` | `1 99 99` | (w7) |
| 兩個別名 `b = a; c = a` | `1 99 1` | `1 99 99` | (w3) |
| 別名鏈 `b = a; c = b` | `1 1 99` | `1 99 99` | (w4) |
| 源是 slice 參數 | `1 99 1` | `1 99 99` | (w2) |
| 迴圈內寫、迴圈後讀源 | `1 7` | `7 7` | (w7)（跨塊） |
| **寫別名後不再讀源（合法）** | `1 99` | `1 99` | —（本來就該通過） |

最後一列是關鍵：naive 在**合法**情形上與控制組一致，所以上表的差異全部來自守則，而不是來自
「naive 根本沒生效」。

**驗收（509 檔語料；單一瞬間 `rsync` 快照，A＝放寬後、B＝在 `aliasWritesUnobservable` 入口 `return false`）**：

- `rc` 差異 **0 檔**；程式 stdout 的 sha256 差異 **0 檔**；
- clone 總數 **210 → 210**：**增加 0 檔、減少 0 檔**；
- 放寬規則在語料上**觸發 0 次**。

**⚠️ 本輪最重要的（也是反直覺的）發現：語料裡沒有這個形狀。**
用 `NOLANG_ALIAS_STATS=1` 逐檔統計，全語料 509 檔中「別名被寫入」的候選**只有 1 個**：
`tests/mem-safety/deep-clone.no` 的 `test-clone-vec`——

```
a = [10, 20, 30]
b = a
b[0] = 99
a[0] == 10 -> ok = true      ; 源在寫入點之後仍被讀取 ⇒ 必須拒絕
```

而它**正是必須被拒絕的那一種**（這個測試的存在目的就是斷言 `b[0] = 99` 不會影響 `a`）。
所以本輪的收益是 **0 個 clone**，但代價也是 **0**：509 檔行為逐位元組不變。

擋住其他候選的是既有的 (r1)/(r3)：`dst-bad-use` 216 次（別名逃逸——傳給 callee／存進容器）、
`dst-not-fresh` 114 次（`EmitMoveInto` 形狀）、`src-dead` 9826 次（`insertDrops` 已經處理）、
`src-is-borrow` 42 次。

**洩漏量測（直接量不變式）**：50 萬次「讀源 → 寫別名」迴圈，A 與 B 的峰值 RSS **都是 1,671,168 B**
（與 §1.2.2 的控制組數字相同）；若每圈洩漏一份 24 B 載荷，差距會是 ~12 MB。同一支程式的 clone 數
A=0、B=1，stdout 相同（`50000000`）。

**仍未實作：§4.2 的「在 W(w) 之前插入 clone」那一支（v 在 W 仍活時）。**
必須說清楚它**不是本輪遺漏，而是刻意不做**，理由有三：

1. **它不改變驗收指標。** 今天在賦值點已經有**恰好一個** clone；把 clone 搬到首寫點之後仍然是
   **恰好一個**。靜態 clone 數不變 ⇒ §5 的驗收判準（SAME 不減、REGRESS=0、clone 只減不增）對它完全無感。
2. **它的收益是動態的，而且需要跨塊的值分裂。** 只有當 W 位於條件／迴圈內時才少做一次 clone
   （今天是無條件在賦值點做）。要做到這點必須把別名**拆成兩個值**（W 之前 ≡ 源、W 之後是 clone），
   也就是需要支配關係分析＋兩個值各自的 drop 擺放。現行 IR 的新綁定是**單一 SSA 值**，
   沒有可供 `w = clone(v)` 寫入的 slot，所以 §4.2 的 `w = clone(v)` 記法在此**無法照抄**。
3. **只支援單一基本塊的版本零收益。** 若把範圍限縮到「別名生命期全在同一個基本塊」，W 與賦值點
   **必然同時執行**，動態 clone 數一模一樣 ⇒ 純風險、零收益。

在此之前，所有「會被寫入且源在該寫入點仍活」的別名都維持賦值點 clone（保守超集）。

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
  以及「參數位置被 consume」的 drop 抑制。
  ✅ **已實作（2026-10-01 第四輪）**，完整設計與量測見 **§1.6**。實作後發現它的成本比預期低、
  且**不需要** §4.3 的判準 2（見 §1.6 的「為什麼不需要 one-await」）。

**已知取捨**：一個**從未被 await 的 handle 引用**會使計數停在 1 而洩漏（例如 `h = run f(); h2 = h`
之後只 await `h`）。這與舊行為同級（舊版未 await 的 handle 本來就整組洩漏），不是迴歸；要收掉它需要
handle 成為一個**有 `OpDrop` 的 owned 值**，那又是另一層改動。

**量測**：300k 次 spawn+await 的 RSS，舊 8.40 MB → 新 7.96 MB，輸出完全相同（無洩漏）；
`tests/async*.no` 與 `tests/module-async.no` 逐位元組相同；10 個差異探針中只有新的別名案例改變
（SIGSEGV → 正確輸出）。

#### 1.3.2 實作現況（2026-10-01，第二輪）——retain 的閘門用錯了謂詞，兩條路徑漏計

§1.3.1 宣稱「別名 handle 的 SIGSEGV 已由 RC 修掉」。**這個宣稱只對一半的值成立**：修正把 retain
的閘門掛在「這個值有沒有已知的**任務結果型別**」（`asyncResTypes[v]` 有無條目）上，而不是掛在
「這個值是不是 handle」。這兩件事**不是同一件事**，於是兩條路徑整條漏掉了 retain：

| 缺陷 | 形狀 | 修復前實測 | 成因 |
|---|---|---|---|
| **D1：void 任務** | `h = run void-async(); h2 = h; awy h; awy h2` | **rc = 139（SIGSEGV）** | 任務結果是 void ⇒ `asyncResTypes` 沒有條目 ⇒ 複製不 retain ⇒ rc 停在 1，第一次 `awy` 就 free，第二次 release 已釋放的塊 |
| **D2：`run <handle>` 轉發** | `h = run dbl(21); h2 = run h; awy h; awy h2` | **印出 `42 0`**（無崩潰） | `lowerAsyncRun` 的 `KIdent` 分支**直接回傳運算元**，既不建新值也不 retain；兩次 `awy` 落在同一個值 ⇒ 第一次把槽歸零，第二次讀到 0 |

**D1 的關鍵是「不對稱」**：同一個形狀在任務回傳 `i64` 時完全正確（`42 42`），只在回傳 void 時
崩潰。這正是「用結果型別當 handle 判準」的指紋——型別存在就對，不存在就錯。

**D2 的關鍵是「同義的兩種拼法必須等價」**：`h2 = h` 與 `h2 = run h` 都表示「同一任務的第二個
引用」，前者 retain、後者不 retain。修復前兩者的 MIR 形狀不同（`run h` 那次沒有 `task-retain`），
雖然都印 `42 0` ——修復後 `h2 = run h` 與 `h2 = h` 產生**逐指令相同**的序列，這才是正確的等價。

**修法**（全部在 `src/mir/hir2mir.go`，不引入新 ABI、不改 MIR op）：

1. **新增 `asyncHandles map[ValueID]bool`**：handle 的**身分**與任務的**結果型別**分開記。
   在 `lowerCall` 的 OpRun 分支**無條件**記入（void 任務也記），`carryAsyncResType` 一併傳遞並
   在來源非 handle 時**清除**（避免陳舊身分）。原本的 `asyncResTypes` 只保留「結果型別」的職責。
2. **retain 的閘門改讀 `asyncHandles`**：新綁定路徑與 `carryAsyncResType` 兩處都改。
3. **`lowerAsyncRun` 的 `run <handle-var>` 分支**：改為「建一個新值 + `OpTaskRetain`」，與
   `h2 = h` 走同一套計數紀律。運算元不是 handle 時**原樣回傳**（不對未知形狀加 retain）。

**為什麼修法選「補 retain」而不是「報錯」**：`run <handle>` 在 `spawn_graph.go` 裡已被明文認定為
「forward 一個既有 handle、不是 spawn 站點」，語義上就是第二個引用；把它變成硬錯誤會拒收一個
語義明確的程式。語料與文檔都沒有這個拼法（全庫 grep 無 `= run <var>`），所以沒有相容性包袱。

**量測**：新語料 `tests/async-rc.no`（6 個案例）在修復前 **rc=139**、修復後 6 行全對、rc=0。
9 個單元測試（`async_rc_handle_test.go`，新增 5 個）在修復前 **FAIL**、修復後 PASS。
510 檔語料的編譯與行為掃描見 §6。

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

滿足時：協程等價於同步呼叫。**這個快速路徑已經實作**——`emitAsyncAwait`（`codegen.go:12613`）在未完成時直接同步呼叫 `resume_fn`，註釋明確寫道「for flat (top-level) await trees … the synchronous drive yields byte-identical output to the legacy event loop without ever entering `nolang_async_run`」。

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

### 1.6 修正 B 的參數側：不改 ABI 的 move 中間方案（2026-10-01 第四輪，已落地）

§1.3.1 末段把「參數側」拆成兩半，本節收掉前半。

**要解的問題**。`run f(x)` 對每個「擁有堆」的參數在 spawn 邊界做一次深拷貝（P0 的
`asyncArgOwnedCopy`，`codegen.go`），因為任務可能到 `awy` 才執行，而呼叫端在那之前可以重賦值
或離開作用域。當 x 在 spawn 之後**可證明已死**時，那份拷貝是純浪費：x 的所有權直接轉移進任務
即可——零拷貝，而且**不需要任何新 ABI**。

**判準（三個條件，全部必要）**：

| # | 條件 | 為什麼必要 |
|---|---|---|
| 1 | `wouldDrop[x]`（見下） | 呼叫端**真的**會釋放 x。借用（slice view／borrow read）與參數都不會被呼叫端釋放，把它們 move 進任務＝任務去 free 別人的 buffer |
| 2 | 源在 spawn 之後已死 | 用的是 `insertDrops` 判斷 clone-vs-move 的**同兩個謂詞**：`!liveOut[bid][x]` 且 `!readNonDropAfterInBlock(bid, iid, x)` |
| 3 | callee 該位置的參數歸類為 `str`／`vec`／`struct` | wrapper 才會釋放 payload。否則 wrapper 只釋放容器，move 進去的 payload 就洩漏 |

**為什麼條件 3 是充分的**（本節的關鍵論證）：對這三類，**wrapper 的 free 與呼叫端的 `OpDrop`
降成同一支 helper**。`emitDrop` 對 `%str-long` 發 `@str_free`、對 `%vec` 發 `@vec_free`、
對「有 ptr 欄位或有內聯擁有葉子」的 struct 發該 struct 的解構子；`asyncWrapperFor` 的 `w_free`
發的是**完全相同的三支**。所以轉移所有權只是把**同一次 free** 從呼叫端搬到任務，
**不改變釋放了什麼**。反過來說，wrapper 釋放不了的類別（map、owning tagged enum、
元素不可深拷貝的 slice）歸類為 `""` 因而被排除——即使 `dropOwnsHeap` 說它們擁有堆。

**為什麼不需要 §4.3 的判準 2（每條路徑恰好 await 一次）**。§1.3.1 當初估「無洩漏的 move 還需要
判準 2」，**那個估計是錯的**：任務從未被 await 時，argbuf **兩種做法都會洩漏**——釋放 payload 的
是 wrapper（`w_free`），而 wrapper 只在 await 時執行。拷貝洩漏的是副本，move 洩漏的是本體，
**同一筆洩漏、少一次拷貝**。所以 move 並沒有「拿拷貝換洩漏」。

**跨層協議（本方案真正的成本）**。判準需要兩邊的資訊：liveness 在 analysis、參數歸類在 codegen。
所以**只在 `insertDrops` 決定一次**，記進 `Module.spawnArgMoves`（ValueID → bool）；
`emitAsyncRun` 讀它來跳過深拷貝，`checkDropCount` 讀它來豁免 `missing-drop`。三處都不重算——
重算正是 clone 與被抑制的 drop 會漂移的方式。

**共用謂詞**。參數歸類抽成 `Module.SpawnArgClasses`（`spawn_graph.go`），codegen 的
`asyncArgKinds` 是它的唯一實作。做法沿用本檔既有的 `Module.OptionPayloadBoxed`：立一個
**只設 `mod` 的拋棄式 codegen** 去問 emitter 自己的規則，理由與那裡逐字相同——
「兩邊一旦不一致，就會有一邊 free 掉另一邊共用的東西」。（`extraFuncs` 必須是真的 map：
`vecDeepClone` 會在裡面記憶化，nil map 會 panic。）

**落地點**：

| 檔案 | 改動 |
|---|---|
| `src/mir/spawn_graph.go` | 新增 `SpawnArgClasses`（共用歸類）；`DumpSpawnArgMoveStats` 加上 `moved=` 欄位（讀回 `spawnArgMoves`，不是重算） |
| `src/mir/mir.go` | 新增 `Module.spawnArgMoves` |
| `src/mir/analysis.go` | `insertDrops` 新增 move 判定；抽出 `wouldDrop` 並**留在原位**（見下）；`checkDropCount` 讀 `spawnArgMoves` 豁免；`Analyze` 開頭重置該集合 |
| `src/mir/codegen.go` | `emitAsyncRun` 讀 `spawnArgMoves`；`moved` 時跳過 str/vec 克隆、struct 的 srcSlot 溢寫與兩段克隆走訪 |
| `src/mir/spawn_arg_move_test.go` | 6 個單元測試（MIR 的 drop 集合 ＋ IR 的 free 站點） |
| `tests/async-arg-move.no` | 8 個案例（5 個該 move＝6 個參數、3 個該維持拷貝） |

**🔴 實作時踩到的唯一一個真坑：`wouldDrop` 的計算位置。** 第一版把 `wouldDrop` 放在
`moveSrc` 迴圈**之前**（因為 move 判定要用它），結果 `tests/tagged-enum-zero-match.no` 與
`tests/tagged-enum-alias.no` 由 `ok` 變 `fail`（共 4 條 `[missing-drop]`）。原因是那個迴圈
**會改模組狀態**：`moveEnumSharesHeap` 分支會設 `m.enumOwnsPayload[src]`／`[dst]`，而
`dropOwnsHeap` **優先**讀 `enumOwnsPayload`。在它之前收集 `wouldDrop`，就會漏掉「在這裡才變成
owning enum」的那些值 ⇒ 它們的 drop 被拿掉 ⇒ 硬編譯失敗。修法是把 move 判定移到**獨立的第二個
迴圈**，`wouldDrop` 留在原位（與改動前的 `droppable` 同一個時間點）。

> 這個坑只有**編譯掃描**抓得到（診斷是硬錯誤），行為掃描與單元測試都看不到。它同時是本檔
> 「A/B 掃描必須跑全語料、不能只跑自己關心的子集」這條紀律的一個實例。

**量測**（詳見 §6 第四輪表）：511 檔編譯掃描 **0 差異**；40 檔含 `run`/`awy` 的行為掃描
**0 差異**；新語料 8 案例在 move 開／關兩側**輸出逐位元組相同**、rc=0；6 個新單元測試在
「move 關閉」的對照組上**4 個 FAIL**（其餘 2 個是反向對照，兩側都必須 PASS）。

---

## 2. 現狀基線（實測）

### 2.1 現行決策點與實作位置

| 機制 | 位置 | 觸發 |
|---|---|---|
| 借用逃逸檢查 | `analysis.go:215` `checkBorrowEscapes` | **僅 `OpBorrow`**（`&x` 視圖綁定，`hir2mir.go:9540`） |
| 切片視圖降級 | `analysis.go:278` `demoteUnsafeSliceViews` | 逃逸 **且** 可能被覆寫 |
| drop 插入 | `analysis.go:1551` `insertDrops` | 每個 owned 局部值、每條 CFG 路徑、最後使用之後 |
| drop 數量驗證 | `analysis.go:2510` `checkDropCount` | 「每個 owned 局部恰好一個 drop」 |
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
| §2.3.4 | ready queue 固定 256 槽、無溢出檢查 | ✅ **已修**：改為可增長佇列（`codegen.go:11741`） |

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

**根因**：`emitAsyncAwait`（`codegen.go:12613`）在讀完結果後無條件釋放三個容器：

```
call void @free(i8* %aawy.freeres)    ; 結果緩衝區
call void @free(i8* %dataI8)          ; args 結構
call void @free(i8* %aawy.freetask)   ; %task 本體
```

但 handle 只是個 `i64`（`ptrtoint i8* -> i64`，`codegen.go:12554`），**沒有「已釋放」狀態**。第二次 `awy` 走同一條路徑，對已釋放指標再 free 一次。

> ✅ **修復**：兩層。P0 在 await 後把 handle 的**槽歸零**，第二次 await 走守衛分支（處理「同一槽 await 兩次」）；
> 修正 B 再把 `%task` 提升到 R 檔（`@nolang_rc_alloc(i64 32)`，`codegen.go:12518`），handle 的每次**複製**
> 發 `OpTaskRetain`（`codegen.go:12592`），`awy` 改為 `@nolang_rc_release` 且只在歸零時 free（`codegen.go:12738`）
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
> （`codegen.go:11741`）：容量按需倍增，`@nolang_ready_grow`（`codegen.go:11763`）在倍增時把存活視窗
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

⚠️ **I3 的「等號」形式不可作為硬錯誤，實作採用的是它的安全方向。** 上表的等號
（`#retain == #release + 1`）對「從未被 await 的引用」是**假**的——而 §1.3.1 明文接受那是
**已文檔化的洩漏**（`h = run f(); h2 = h` 之後只 await `h`，計數停在 1）。若驗證器真的強制等號，
它會在一個**被接受的程式**上開火；而 §4.5 說明了這裡的假陽性就是**硬編譯失敗**（`Analyze` 的
報告 gate codegen）。所以 `checkRefBalance` 強制的是**同時為真且承重**的那一半：

> **任務 handle 的每個別名都必須自己帶一個計數**（每個非 root 的別名成員都必須是某個
> `OpTaskRetain` 的運算元）。

少了 retain 不是洩漏而是 **UAF**：兩個引用共用一個計數，第一次 `awy` 就把任務 free 掉，第二次
release 已釋放的塊。§1.3.2 的 D1 就是這個缺陷（實測 rc = 139）。「多算」的那一側
（`#release < #retain + 1`）**刻意不報**，因為它正是已接受的洩漏。

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

- `readNonDropAfterInBlock`（`analysis.go:1090`）——「之後是否還有真正的資料讀取」，`OpDrop` 不算讀取（這個區分是既有正確性的一部分，見 `analysis.go:1600` 的註釋）
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

> **本節的「retain 參數」目前只落地了一半，而且落地的不是 retain 而是 move。**
> §1.6 的判準把參數分開處理：
>
> - **源在 spawn 之後已死 ⇒ move**（✅ 已落地）：所有權直接轉移，不 retain、不拷貝、不計數。
>   這正是上表「R 值的 move → 不插入（計數中性）」那一列的具體實例。
> - **源仍活 ⇒ retain 參數**（⬜ 未落地）：這一半才需要參數自己的 buffer 帶 header，
>   也就是本節說的「args 內部的堆指標指向的 buffer 才是 R 值」——而那個 buffer 今天的配置點
>   （`@str_from_const` / `@str_concat` / `@str_clone` / `vecDeepClone` …）**都不帶 header**，
>   所以無從 retain。這是 §3.4 那筆「28 個 `@malloc` 站點」的帳。
>
> 換句話說：**「借用」是今天的事實，不是最終設計**；最終設計要靠 header ABI 把參數側的
> 「仍活」情形也收掉。§1.6 只是先把不需要 ABI 的那一半做掉。

### 4.5 驗證器

| 驗證器 | 對應不變式 | 位置 | 狀態 |
|---|---|---|---|
| `checkDropCount` | I4 | `analysis.go`（既有） | ✅ 已落地 |
| `checkRefBalance` → 擴充 `checkDropCount` | I3 | `analysis.go`，`Analyze` 內 `checkDropCount` 之後 | ✅ **已落地（2026-10-01 第二輪）** |
| `checkTierSoundness`（新） | I1, I2 | 新增，掛進 `ValidateTypes` 的硬錯誤清單 | ⬜ **刻意未實作**（理由見下） |
| `checkReleaseTarget`（新） | I5, I6 | 新增 | ⬜ **刻意未實作**（理由見下） |

**`checkRefBalance` 實作要點**（詳見 §3.5 的 I3 註記與 §1.3.2）：

- **與插入器共用謂詞**：別名集合直接用 `spawn_graph.go` 的 `handleAliasSet`（同時走兩種
  `OpMove` 編碼、`OpCast`、`OpPhi`），不另寫一份——這正是本節下方那條警告的要求。
- **只報安全方向**：每個非 root 的別名成員必須是某個 `OpTaskRetain` 的運算元。
  沒 await 的引用（已接受的洩漏）**不報**。
- **按 spawn 站點計**：`OpRun` 且 `Sym != ""` 才是真正的 spawn（`rc = 1` 的來源）；
  `Sym == ""` 的 `run <var>` 只是轉發，沒有新計數可平衡。
- **必須有「修復前必須失敗」的測試**（§7.2）：`TestCheckRefBalanceFiresOnMissingRetain` 與
  `TestCheckRefBalanceFiresOnForwardedHandleWithoutRetain` 用 `stripTaskRetains` **把修復加上的
  retain 拿掉**，再要求驗證器開火——否則一個「什麼都沒看」的空驗證器也會全綠。

**為什麼 `checkTierSoundness` / `checkReleaseTarget` 此刻刻意不寫。** 這兩支要守的不變式
（I1/I2 的檔位一致性、I5/I6 的 release 目標）都是**header ABI 接上值之後才存在**的性質。而現況
（§1.3.1、§5 P4/P5）**唯一提升到 R 檔的物件是 `%task`**，其餘 28 個 `@malloc` 站點一個都沒動：

- **I1** 由 `inferTiers` 的「顯式 seed 每個值」結構性保證（P1 已用 `TestInferTiersIsAllS` 的非空
  守衛釘住），寫成程式層驗證器只會是一支恆真的空檢查。
- **I2** 在今天**是真空的**：語言裡沒有 R 容器，S/C 值無處可存。
- **I5/I6** 是 **codegen 層**的性質（`release` 是否 free `base`、借用 sentinel 是否早退），
  MIR 看不到 `@nolang_rc_release` 的函式體；而 §4.5 上表的「掛進 `ValidateTypes`」是 **AST 層**
  的鏈，檔位與 RC 都是 MIR 概念，**放不進去**。

所以本輪把「能承重的那一支」落地，並把其餘三支的**前置條件**寫清楚：它們要等 P4 的完整版
（參數側 header-aware 配置）把 header 接上使用者值之後才有實質內容。**先寫空檢查比不寫更糟**
——它會讓「驗證器存在」變成一個虛假的保證。

**新增診斷必須四處齊備**（見 skill）：`checker.go` 的 `ValidateXxx` + `lint.go` 的 `RunAllLints` +
`build/transpiler.go` 的硬錯誤清單 + `TraceID`。⚠️ 若驗證器放在**新檔案**，`Makefile` 的
`TRACE_ID_FILES` 不含它，`TraceID: "PLACEHOLDER"` **永遠不會被替換** → 必須硬編碼字面 id。
（`checkRefBalance` 因此放在**既有檔案** `analysis.go`，不新增檔案。）

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
> ⚠️ **這段是 2026-09-30 的狀態；下面兩段（10-01）已取代它。** 現在只剩「源在寫入點仍活」那一支
> 未做，且是**刻意不做**（見 §1.2.3 末段）。

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

> **實作進度（2026-10-01，第二輪）：補上「源在**首寫點**已死 ⇒ 直接前向」——§1.2 第三條 bullet 的一般情形。**
> 同一支 pass 放寬 (r1)：別名的使用可以是讀取，或**源在該寫入點已死**的元素寫入（`b[i] = x`）。
> 判定集中在新增的 `aliasWritesUnobservable`（§1.2.3 有完整推導與四道守則）。
> 迴歸釘 `src/mir/alias_forward_test.go` 追加 7 測：3 個正向（`TestAliasWrittenAfterSourceLastReadIsNotCloned`、
> `…InLoop`、以及既有的 `TestSourceDead…` 家族）與 4 個守則各自的負向
> （`TestAliasWrittenBeforeSourceLastReadStillCloned`、`TestAliasWrittenWithSecondAliasStillCloned`、
> `TestAliasChainWrittenStillCloned`、`TestAliasOfParameterWrittenStillCloned`、
> `TestAliasWrittenThenEscapedStillCloned`）。
>
> **驗收（509 檔語料，A/B 同快照；B 在 `aliasWritesUnobservable` 入口 `return false`）**：
> - `rc` 差異 **0**、stdout sha256 差異 **0**；
> - clone 總數 **210 → 210**（增加 0、減少 0）⇒ 符合「只減不增」；
> - 規則**觸發 0 次**——語料裡唯一「別名被寫入」的候選是 `mem-safety/deep-clone.no` 的
>   `test-clone-vec`，而它**必須**被拒絕（該測試的用途正是斷言 `b[0]=99` 不影響 `a`）。
>
> 🔴 **本輪的結論是負面的，但正是它讓 P2 可宣告完成**：§1.2 三條 bullet 中**所有會消除 clone 的**
> 情形（只讀別名、源在拷貝點已死、源在首寫點已死）**都已落地**，而語料顯示**消除的潛力已經用盡**。
> §4.2 剩下的「v 在 W 仍活 ⇒ 在 W 之前插 clone」那一支**不改變靜態 clone 數**（今天在賦值點已經是
> 恰好一個），其收益純粹是動態的，且需要跨塊的值分裂＋支配分析——**刻意不做**，理由與前提寫在 §1.2.3。

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
- 新語料 `tests/async-rc.no`（跨邊界共享、多任務、取消後不 await、handle 存容器）；✅ **已建立（2026-10-01，P6）**——本輪同時補上兩個漏計缺陷的迴歸案例
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
> - ✅ **參數側的「源已死 ⇒ move」已落地（2026-10-01 第四輪）**：`insertDrops` 判定並記進
>   `Module.spawnArgMoves`，`emitAsyncRun` 據此跳過深拷貝，`checkDropCount` 據此豁免
>   `missing-drop`；共用歸類在 `Module.SpawnArgClasses`。**不引入新 ABI、不改 MIR op**。
>   完整設計、判準與量測見 **§1.6**；語料 `tests/async-arg-move.no`、單元測試
>   `spawn_arg_move_test.go`。
> - ⚠️ **未落地：參數側的 retain**（§4.4「retain 參數；release 於任務尾」），也就是
>   **「源仍活 ⇒ 用 retain 取代深拷貝」**那一半。前置條件與替代方案見 §1.3.1 末段與 §4.4 的註記：
>   參數自己的 buffer 必須帶 header，屬 §3.4 那筆「28 個 `@malloc` 站點」的帳。
>   今天這一半仍是 P0 的深拷貝（`asyncArgOwnedCopy`）。
> - ✅ **P3 已完成（2026-10-01）**：spawn-graph 線性化判定已落地（報告-only，見 §5 P3 的實作進度）。
>   本輪 `%task` 的提升確實不需要它（編譯器對 task 的生命期有完全控制權），但它是**參數側提升**
>   （§4.4「retain 參數」）的前置條件——哪些 spawn 邊能降級為同步呼叫，決定哪些參數根本不需要 RC。

### P6 — 驗證器、語料與文檔

**目標**：`checkRefBalance` / `checkTierSoundness` / `checkReleaseTarget`；擴充語料；更新 `docs/docs/lang/memory.md`。

**驗收**：`no vet src/std` 必須 0 error；`./bin/no test test/std/` 的 FAIL 集合**逐行不變**（現況 14 失敗 / 7 檔，全為預存）。

> **實作進度（2026-10-01，第二輪）：`checkRefBalance` ＋ 語料 ＋ 文檔已落地；另兩支驗證器刻意不寫。**
>
> 本輪**先修了兩個真實缺陷**才寫驗證器——驗證器要守的東西必須先真的會壞（§1.3.2 的 D1/D2）：
>
> | 交付 | 內容 |
> |---|---|
> | **缺陷修復** | `hir2mir.go` 新增 `asyncHandles`（handle 身分）與 `asyncResTypes`（結果型別）分離；`run <handle>` 轉發改走計數路徑。**不引入新 ABI、不改 MIR op** |
> | **驗證器** | `analysis.go:2710` `checkRefBalance`（I3 的安全方向），掛進 `Analyze`，重用 `handleAliasSet` |
> | **語料** | `tests/async-rc.no`（6 案例）；修復前 **rc=139**，修復後 6 行全對、rc=0 |
> | **單元測試** | `async_rc_handle_test.go` 新增 5 測（共 9）；其中 2 個在**修復前的樹上 FAIL**（已用 `/tmp/p6/A` 快照實證） |
> | **文檔** | `docs/docs/lang/memory.md` 新增「handle 複製的引用計數漏計」一節；本檔 §1.3.2 / §3.5 I3 / §4.5 / §6 / 附錄 B |
>
> **`checkTierSoundness` / `checkReleaseTarget` 刻意不寫**：理由與前置條件寫在 §4.5。一句話——
> 它們要守的不變式（I1/I2/I5/I6）是 header ABI 接上**使用者值**之後才存在的性質，而今天唯一
> 提升到 R 檔的仍是 `%task`。**先寫空檢查比不寫更糟**，因為那會讓「驗證器存在」變成虛假的保證。
>
> **驗收結果**（詳見 §6 本輪實測表）：`no vet src/std` = **0 error / 6205 warning / 936 hint**（＝基線）；
> 510 檔語料的**編譯** A/B 比較 **0 差異**（504 檔兩邊皆通過）⇒ 驗證器無假陽性；
> 39 個含 `run`/`awy` 的語料檔中，行為差異**恰好 1 個**，就是新增的 `tests/async-rc.no`。

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

**本輪實測（2026-10-01 第二輪，P2 首寫放寬；基線 `eb64eaa7`）**：

| 檢查 | 結果 |
|---|---|
| `go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | 全綠 |
| `no vet src/std` | **0 error / 6205 warning / 936 hint**（＝基線；A 與 B 完全相同） |
| A/B 語料掃描（509 檔，同快照；A＝放寬、B＝規則入口 `return false`） | **rc 差異 0 檔、stdout sha256 差異 0 檔** |
| clone 總數 | **210 → 210**，增加 0 檔、減少 0 檔 |
| 規則觸發次數 | **0**（語料唯一候選 `mem-safety/deep-clone.no::test-clone-vec` 必須被拒絕） |
| 反向對照（把守則換成 `return true`） | 5 個探針**真的 miscompile**（含源是 slice 參數時**改到呼叫方**）；合法探針兩邊一致 |
| 洩漏 | 50 萬次「讀源 → 寫別名」迴圈峰值 RSS **A = B = 1,671,168 B**；clone A=0 / B=1，stdout 相同 |
| 建置出的 binary 位元組 | A 與 B 的 `bin/no` 皆未被本輪使用（用 `/tmp/p2f/no-{A,B}`）；`bin/no` 在本輪期間**被別的 session 重建**（mtime 1790820647 → 1790828076）⇒ 全程未觸及 |

> **P2 的驗收方式與 P1 不同。** P1 是純重構 ⇒ 要求「0 差異」；P2 是**行為改變** ⇒ 判準是
> 「clone 只減不增、rc 不變、受影響檔案的輸出逐位元組相同」。用 `clone` 指令數逐檔比對是刻意的：
> `insertDrops` 有權把改寫後的 move **再 clone 回來**，所以「clone 數沒變」只代表最終指令形狀相同。
> 第二輪的 509 檔掃描同時量了 `rc`、stdout 的 sha256 與 clone 數三項，因此「0 差異」這次是
> **三重**的，不是單一指標的沉默。

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

**本輪實測（2026-10-01 第三輪，P6 handle 引用計數；基線 `eb64eaa7`）**：

| 檢查 | 結果 |
|---|---|
| `go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | 全綠 |
| `no vet src/std` | **0 error / 6205 warning / 936 hint**（＝基線；修復＋驗證器後完全相同） |
| 新增單元測試 | 5 個（共 9）；**2 個在修復前的樹上 FAIL**（`/tmp/p6/A` 快照實證：`OpTaskRetain count = 0, want 1`） |
| 探針（直接跑建置出的程式） | `h2 = run h; awy h; awy h2`：**`42 0` → `42 42`**；void 任務 `h2 = h`：**rc=139 → rc=0** |
| 新語料 `tests/async-rc.no` | 修復前 **rc=139（SIGSEGV）**；修復後 6 行全對、rc=0 |
| **編譯**掃描（510 檔，B＝修復、C＝修復＋`checkRefBalance`） | **0 差異**；兩邊皆 504 檔通過 ⇒ **驗證器無假陽性**（這是驗證器的關鍵門檻，因為假陽性＝硬編譯失敗） |
| 行為掃描（39 檔含 `run`/`awy`，A＝修復前、B＝修復後） | 差異**恰好 1 檔**＝新增的 `tests/async-rc.no`；其餘 38 檔 `(rc, stdout)` 逐位元組相同 |
| 行為掃描（40 檔**不含** `run`/`awy` 的抽樣，A vs B） | **0 差異** ⇒ 沒有 std 層級的連帶改變 |
| `bin/no` | 全程未使用（用 `/tmp/p6/no-{A,B,C}`）；`src/` 工作樹只有我自己的 3 個檔案 |

> 🔴 **為什麼行為掃描可以只跑「含 `run`/`awy`」的檔案子集。** 本輪的三處改動都以
> `l.asyncHandles` 為閘門，而該集合**只在 `OpRun` 站點被寫入**；沒有 `run` 就沒有 `OpRun`，
> 也就沒有任何 handle、沒有 `OpTaskRetain`、`carryAsyncResType` 對非 handle 的行為與改動前
> **逐字相同**。所以「不含 `run`/`awy` 的檔案 MIR 全等」是**由建構保證**的，不是抽樣推論——
> 抽樣只是再加一道保險（0 差異的實測）。

> ⚠️ **本輪的掃描工具本身有過一個缺陷，值得記下。** 第一版 harness 用 `$$` 當每個 worker 的
> 臨時檔名，但 `$$` 在**背景子 shell 裡仍是父 shell 的 pid** ⇒ 4 個 worker 共用同一個 `out` 檔，
> 互相覆寫後再各自 hash。這種 harness 缺陷**會製造假差異**（正是 §6 上方「91 檔假改變」那類
> 雜訊的成因）。修法是每個 worker 用自己的 tag 開目錄。
> 另一個限制：`run_to` 只回傳**退出碼**，被信號殺死時 `$? >> 8` 為 0 ⇒ SIGSEGV 在掃描表裡
> 顯示成 `rc=0`，但**stdout 的 sha 仍然不同**（崩潰時輸出被截斷），所以差異不會被漏掉——
> 只是「rc=139」這個數字要從直接執行取得，不能從掃描表讀。

**本輪實測（2026-10-01 第四輪，§1.6 spawn 參數 move；基線 `2a39c4e1`）**：

| 檢查 | 結果 |
|---|---|
| `go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | 全綠 |
| `no vet src/std` | **0 error / 6205 warning / 936 hint**（＝基線，未增） |
| 新增單元測試 | 6 個（`spawn_arg_move_test.go`）；**4 個在「move 關閉」的對照組上 FAIL**（`/tmp/p6/no-P`：`spawnArgMoves` 空、main 仍有解構子、`moved=0`、歸類為 0 個）。另 2 個是反向對照，兩側都必須 PASS |
| 新語料 `tests/async-arg-move.no` | 8 案例；move 開／關兩側 stdout **逐位元組相同**、rc=0；`NOLANG_MIR_SPAWN_ARG_MOVE=1` 顯示 **moved=6**，3 個仍活參數**未** move |
| **編譯**掃描（511 檔，M＝改動前、E＝改動後） | **0 差異**；兩邊皆 505 檔通過 |
| 編譯掃描（**第一次**，同一組 M vs E） | **2 檔差異**：`tagged-enum-zero-match.no`／`tagged-enum-alias.no` 由 ok 變 fail ⇒ **抓到 §1.6 那個 `wouldDrop` 順序坑**；修好後歸零 |
| 行為掃描（40 檔含 `run`/`awy`，P＝同樹但 move 關閉、E＝move 開啟） | **0 差異**（`(crc, rc, stdout-sha256)` 逐檔相同） |
| 金標（凍結 511 檔語料，P vs E，各跑一次；`fp.txt` 先 `sort`） | 桶統計**完全相同**：`SAME=461 DIVERGE=8 REGRESS=13 IMPROVED=0 BOTH_FAIL=0 NEW=29 UNSTABLE=0`；`fp` 逐行 diff **只有 2 行**，且兩行都是 `tests/std-new.no` |
| ↑ 那 2 行的歸因 | **與本輪無關**，見下方專段。`tests/std-new.no` 的 stdout 是**當前工作目錄的不排序列表**（`read-dir`），而兩次金標執行之間 `/tmp/gold` 多了 `fp-P.txt` ⇒ 列表內容不同 |
| `gofmt` | 本輪改動的四個檔（`analysis.go`／`codegen.go`／`spawn_graph.go`／`spawn_arg_move_test.go`）**0 差異**；`mir.go` 在 HEAD 就有 202 行既有差異，改動後**仍是 202 行**且不含本輪識別字 |
| `bin/no` | 全程未使用（用 `/tmp/p6/no-{A,B,C,D,M,P,E}`） |

> 🔴 **本輪的 A/B 控制組改用 `no-P` 而不是 `no-M`。** 掃描期間 `git status` 出現
> `src/checker/stdsig_gen.go` 與 `src/std/json.no` 的改動——**別的 session 正在同一棵樹上工作**。
> `no-M` 是較早建的 binary，與 `no-E` 之間隔著那些改動 ⇒ M vs E 的差異**無法歸因**。
> `no-P` 是**當前樹的副本**，只把 `SpawnArgClasses` 改成回傳空（＝move 關閉），所以 P vs E 的
> 差異**只可能來自本輪的改動**。這也是 §1.6 那個順序坑能被乾淨歸因的原因。

> 🔴 **`tests/std-new.no` 是本語料裡的一個「以 cwd 為輸入」的測試，金標對它先天不穩定。**
> 它的 stdout 是 `read-dir('.')` 的**不排序**列表，所以
> (a) 換一個 cwd 就換一個 hash、(b) 同一個 cwd 在兩次執行之間多一個檔就換一個 hash。
> 實測（同一個 binary、同一個 cwd `/tmp/gold`）：`1370a7eb…`；`touch /tmp/gold/zzz-marker`
> 之後 → `76e1b328…`；刪掉 marker → 回到 `1370a7eb…`。
> 而金標基線記的是 `5649192d…`、本輪 P 是 `53758abc…`、E 是 `31fe8550…`——**三個都不相同**。
> 結論：那 2 行差異是**harness 產物**（兩次執行之間 `/tmp/gold` 多了 `fp-P.txt`），
> 不是本輪改動。附帶建議：`tests/std-new.no` 應該排序它的目錄列表，或改用固定的子目錄，
> 否則它永遠只能落在 `UNSTABLE` 桶。（本輪未改它——不屬於本輪範圍。）

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
| ~~**ready queue 溢出**（§2.3.4）~~ | ~~任務靜默遺失~~ | ✅ **已修**：改為可增長佇列（`codegen.go:11741`），不再是風險 |

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
| 5 | 是否處理 §2.3.4 的 ready queue 溢出？ | 原建議**報硬錯**（靜默覆寫最危險） | ✅ **已處理，但改採更好方案**：佇列本身改為**可增長**（`codegen.go:11741`），溢出根本不存在，因此無需報錯 |
| 6 | I3 的驗證器強制**等號**（`#retain == #release + 1`）還是**不等式**（每個別名都有 retain）？ | **不等式**。等號對「未 await 的引用」為假，而那正是 §1.3.1 明文接受的洩漏；在此處假陽性＝硬編譯失敗，所以只能強制同時為真且承重的那一半 | ✅ 已照做：`checkRefBalance`（§3.5 I3 註記、§4.5）；510 檔編譯掃描 0 差異 |
| 7 | `run <handle>` 的語義：**補 retain**（＝第二個引用）還是**報錯**？ | **補 retain**。`spawn_graph.go` 已明文認定它是「forward 既有 handle、非 spawn 站點」；語料與文檔都沒有這個拼法（全庫無 `= run <var>`）⇒ 無相容性包袱，補成與 `h2 = h` 等價 | ✅ 已照做：`hir2mir.go:8340`；探針 `42 0` → `42 42` |
| 8 | 參數側：等 header-aware 的 `retain`，還是先做**不改 ABI 的 move**？ | **先做 move**。判準 1/2/3 見 §1.6；條件 3 之所以充分，是因為 wrapper 的 free 與呼叫端 `OpDrop` 對這三類**降成同一支 helper**。它不需要 §4.3 的判準 2——未 await 的任務兩種做法**洩漏量相同**。剩下的「源仍活 ⇒ retain」那一半才需要 header | ✅ 已照做：`spawnArgMoves` ＋ `SpawnArgClasses`；511 檔編譯掃描 0 差異、40 檔行為掃描 0 差異（§1.6、§6 第四輪） |

---

## 附錄 A：語料清單（實際落地）

草案曾提議 7 個新檔；實作時收斂為 **4 個新檔 ＋ 既有檔案的擴充**（把形狀相近的案例放同一檔，
讓 `no test` 的斷言一次覆蓋多個判準）。實際語料如下：

| 檔案 | 狀態 | 覆蓋的形狀 |
|---|---|---|
| `tests/async-ownership.no` | **新增（P0）** | `test-str-arg-reassign` / `test-vec-arg-reassign` / `test-struct-arg-reassign`（§2.3.2）；`test-double-await`（§2.3.1）；`test-null-handle`（§2.3.3）；`test-single-await` / `test-two-handles` / `test-struct-rebind` / `test-handle-rebind(-typed)` |
| `tests/async-handle-alias.no` | **新增（P5）** | `alias-both`（2 次 await）、`alias-chain`（3 次）、`via-return`（`mk` 經 out-param 返回）、`via-container`（存 `[]t` 逃逸）、`same-slot-twice`、`many`（spawn+await 循環，計數必須平衡否則 RSS 無界） |
| `tests/async-rc.no` | **新增（P6，2026-10-01）** | P5 指定的四種形狀（跨邊界共享／多任務／取消後不 await／handle 存容器）＋兩個**漏計缺陷**的迴歸：`test-void-alias`（void 任務的 handle 複製，修復前 **rc=139**）、`test-run-forward`（`run <handle>` 轉發，修復前印 `42 0`） |
| `tests/async-arg-move.no` | **新增（§1.6，2026-10-01 第四輪）** | spawn 參數的 move：**該 move 的 5 個案例（6 個參數）**——`test-str-move`／`test-str-move-large`（100 字元，double free 較易被配置器抓到）／`test-vec-move`／`test-struct-move`／`test-two-moves`（同一次 spawn 兩個已死參數）；**該維持深拷貝的 3 個反向案例**——`test-str-live-copy`（spawn 後仍被讀取）／`test-arg-reassign`（§2.3.2 的重賦值形狀）／`test-struct-live-copy`。⚠️ 這個檔只是煙霧測試：兩個失敗方向（少一次拷貝、double free）都無法從 stdout 可靠看出，真正的守門員是 `src/mir/spawn_arg_move_test.go` |
| `tests/async.no` | 既有 | 10 條 flat spawn 邊（全 LINEAR）——P3 的「防過度提升」對照組 |
| `tests/async-cancel.no` | 既有 | `test-cancel` / `test-no-cancel` / `test-cancelled` / `test-yield` |
| `tests/async-coop.no` | 既有 | `test-run-ok` / `test-cancelled` |
| `tests/async-yield.no` | 既有 | `test-yield-inside` / `test-yield-plain` |
| `tests/module-async.no` | 既有 | 模組層級的 async 呼叫 |

⚠️ 語料撰寫注意（skill 已載明）：`;` 是**行註解**不是陳述分隔符；`''` 是 `str`、`""` 是 `char`；切片寫 `[a..b)`；寫測試檔不必繞開 `s.len()` / `n.to-str()`，但 **option match 必須三個臂都寫**。

## 附錄 B：觸點索引

> 行號為 2026-10-01 第四輪落地後的工作樹（基準 `2a39c4e1` ＋本輪四個檔案的增刪）。行號只為
> 定位方便；函式名才是穩定鍵——本輪 `analysis.go` 的插入使多筆行號位移上百行，故只信任名字。

| 主題 | 位置 |
|---|---|
| drop 插入 | `src/mir/analysis.go:1551` `insertDrops` |
| 只讀別名前向 | `src/mir/alias_forward.go:218` `forwardReadOnlyAliases`（呼叫點 `analysis.go:1562`） |
| **源已死 ⇒ move（P2，2026-10-01）** | `alias_forward.go:391` `srcIsBorrow`（(r4) 上移，同時守住兩個決策）、`:393` 規則本體；統計鍵 `clone-src-dead-to-move`（`:203`） |
| **源在首寫時已死 ⇒ 前向（P2 第二輪，2026-10-01）** | `alias_forward.go:614` `aliasWritesUnobservable`（(w1)–(w7)）、`:248` `copiesOf`（改寫前建立 ⇒ 與走訪順序無關）、`:514` 呼叫；統計鍵 `dst-written-src-dead` / `dst-written`（`:203`） |
| 選項剝離所有權 | `analysis.go:1513` `optionSlicePeelClones`、`analysis.go:1398` `isBorrowRead`（`OpMove` 分支） |
| 選項剝離回歸釘 | `src/mir/option_peel_ownership_test.go` `peelDst`（**跟隨轉移鏈**，見 §1.2.2） |
| move 四判別式 | `analysis.go:1157` `moveStructSharesHeap` / `:1259` `moveStrSharesHeap` / `:1294` `moveSliceSharesHeap` / `:2294` `moveTransfersOwnership` |
| 判別式聯集 | `analysis.go:2333` `moveExemptsSource` |
| drop 數量驗證（I4） | `analysis.go:2510` `checkDropCount` |
| **R 檔計數平衡驗證（I3，P6）** | `analysis.go:2710` `checkRefBalance`（呼叫點 `Analyze` `analysis.go:420`，緊接 `checkDropCount` `:416`）；別名集合重用 `spawn_graph.go:385` 的 `handleAliasSet`；診斷種類 `missing-task-retain` |
| **handle 身分集合（P6）** | `hir2mir.go:331` `asyncHandles`（`asyncResTypes` 只管結果型別）；記入點 `hir2mir.go:8104`（OpRun，**無條件**）、`:2979`（新綁定）、`:8366`（`run <handle>` 轉發）、`:9704`（`carryAsyncResType`，來源非 handle 時清除 `:9701`） |
| **spawn 參數 move：共用歸類（§1.6）** | `spawn_graph.go:260` `SpawnArgClasses`（拋棄式 codegen，沿用 `mir.go:316` `OptionPayloadBoxed` 的做法）；唯一實作是 `codegen.go:12263` `asyncArgKinds` |
| **spawn 參數 move：決策與記錄（§1.6）** | `analysis.go:1771` 起、`insertDrops` 內的獨立第二迴圈（條件 1/2/3，`SpawnArgClasses` 呼叫 `:1821`，記錄 `:1833`）；`wouldDrop` 抽出在 `:1754`（**必須留在 `moveSrc` 迴圈之後**）；結果寫入 `mir.go:1186` `Module.spawnArgMoves` |
| **spawn 參數 move：豁免（§1.6）** | `analysis.go:2608` `checkDropCount` 讀 `spawnArgMoves` 豁免 `missing-drop`；`Analyze` 開頭重置該集合（`analysis.go:404`） |
| **spawn 參數 move：codegen（§1.6）** | `codegen.go:12426` `emitAsyncRun` 的 `moved :=` 閘門（`alt == plt && kind != ""`），跳過 str/vec 克隆與 struct 的 srcSlot／兩段走訪 |
| **spawn 參數 move：迴歸釘（§1.6）** | `src/mir/spawn_arg_move_test.go`（6 測；4 個在 move 關閉的對照組上 FAIL）；語料 `tests/async-arg-move.no` |
| **spawn 參數 move：量測開關（§1.6）** | `spawn_graph.go:286` `DumpSpawnArgMoveStats`（`NOLANG_MIR_SPAWN_ARG_MOVE=1`，`moved=` 讀回 `spawnArgMoves` 而非重算） |
| **`run <handle-var>` 轉發（P6）** | `hir2mir.go:8340` `lowerAsyncRun`（`:8362` 非 handle 原樣回傳、`:8366` 建新值＋retain） |
| 塊內讀取判定 | `analysis.go:1090` `readNonDropAfterInBlock` |
| 借用逃逸 | `analysis.go:215` `checkBorrowEscapes` |
| 切片視圖降級 | `analysis.go:278` `demoteUnsafeSliceViews` |
| **spawn graph / 線性化（P3）** | `src/mir/spawn_graph.go` `SpawnGraph` / `evalSpawnEdge`（報告-only，`NOLANG_MIR_SPAWN_GRAPH=1`） |
| **Tier 推斷（P1）** | `src/mir/tier.go` `Tier` / `joinTier` / `tierConstraint` / `inferTiers` / `DumpTiers`（報告-only，`NOLANG_MIR_TIER=1`） |
| MIR 指令定義 | `src/mir/mir.go:376-381`（move/clone/drop/borrow）、`:485-500`（run/await/task-retain） |
| 型別佈局 | `src/mir/codegen.go:1875-1893`（`%str-long` `:1875` / `%vec` `:1876` / `%option` / `%task` `:1893`） |
| R 檔 header 說明 | `codegen.go:1895`（R 檔 header 註釋起點；`rc == 0` 為借用 sentinel `:1905`；`@nolang_rc_alloc` `:11929`、`@nolang_rc_release` `:11961`） |
| `@str_clone` | `codegen.go:2457` |
| `emitClone` | `codegen.go:5933` |
| `emitIndexStore` | `codegen.go:7239` |
| `lvalueAddrOf`（現接受 `use *Inst`） | `codegen.go:7499` |
| 克隆欄位守衛 | `codegen.go:7649` `getFieldWasCloned`、`:7679` `sourceAlreadyDropped`、`:7775` `emitPos` |
| `emitGetField`（兩處 `@str_clone`） | `codegen.go:7797`（`:7901`、`:7958`） |
| `emitSetField` | `codegen.go:8063` |
| async 排程器 | `codegen.go:11737` `emitAsyncScheduler`（ready queue 在 `:11741`，`@nolang_ready_grow` `:11763`） |
| `emitAsyncRun` | `codegen.go:12310`（參數深拷貝 `asyncArgOwnedCopy` `:12219`，呼叫點 `:12433`；`rc_alloc` `:12518`）；**move 閘門 `moved :=` `:12426`（§1.6）** |
| `emitAsyncAwait` | `codegen.go:12613`（`release` `@nolang_rc_release` `:12738`） |
| `emitTaskRetain` | `codegen.go:12592`（`OpTaskRetain` 定義 `mir.go:500`；發出點 `hir2mir.go:2994`（新綁定）/ `:8370`（`run <handle>`）/ `:9711`（`carryAsyncResType`）） |
| `%task` 型別 | `codegen.go:1893`（**32 bytes**，第 5 欄 = waiter） |
| async 語義（使用者面） | `src/std/async.no`（D10） |
| 現行模型文檔 | `docs/docs/lang/memory.md`（已知限制在 `:380`） |
| `KFuncLit`（未被 MIR 消費） | `src/hir/hir.go:84`、`src/parser/tohir.go:920` |
| 迴歸釘（測試） | `src/mir/{alias_forward,async_boundary_ownership,async_rc_handle,spawn_arg_move,spawn_graph,str_field_lvalue,tier}_test.go`、`src/mir/option_peel_ownership_test.go`、`src/parser/stmt_boundary_block_test.go` |
| **P6 語料（2026-10-01）** | `tests/async-rc.no`（6 案例：別名／void 別名／`run` 轉發／多任務／容器／取消不 await；修復前 rc=139） |
