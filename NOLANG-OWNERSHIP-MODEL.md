# Nolang 混合所有權模型（Hybrid Ownership）— 設計方案

**版本**：v2.2（2026-10-02）
**基線**：`a6c8559d`。所有 A/B 都用**自建快照**（`/tmp/**/no-*`），全程不使用 `bin/no`（並行 session 會重建它）。
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
>
> ⚠️ **驗收的控制組一律指名 SHA**：本輪控制組 = `no-head777`（真 HEAD `777b9389`）；「修復前」=
> `a6c8559d`（HEAD 的父提交）。並行 session 會把 HEAD 往前推，「HEAD 控制樹」不是穩定的指稱。

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

### 0.3 完成狀態（2026-10-02）

**已落地並通過驗收**

- **header ABI 全型別**（§2.1）：37 個 alloc ＋ 35 個 free 站點全部改走 `@nolang_rc_alloc` / `@nolang_free`。
- **借讀的引用計數**（§2.2）：`OpRetain` / `OpRelease` 成對落地；語料 1717 retain / 1693 release。
- **`vecDeepFree`**：深釋放配深克隆（對偶 `vecDeepClone`）。
- **async 邊界的兩條**（§2.3／§2.4）：spawn 參數「源已死 ⇒ move」、`%task` 的 RC ＋ 兩個漏計缺陷修復。
- **分析與驗證器**（§3）：tier 推斷（報告-only）、spawn graph 線性化（報告-only）、`checkRefBalance`。
- **規則一（§1.1）**：只讀前向移除、owned `str` / `[]T` / **`?str` / `?[]T`** 綁定改深拷貝（§4.2 a）。
  過程中一併修掉 `emitOptionDrop` 的 `case "vec"` 死碼（`?[]T` 的 payload 從未釋放，§4.2 a）。

**未落地**（皆為刻意，前置條件見 §4）

1. **規則一的剩餘型別**（§4.2 a）：只剩 **map**。`isOwnedLocal` 對 map 為 false，而 map 是**引用語意**
   （`owned=false`、從不釋放），共用是它的定義而非缺陷；要收就得先決定 map 的擁有權歸屬，不是補一個
   `OpClone` 能解決的。
2. **參數側的 retain**（源仍活時用 retain 取代深拷貝）：**原寫的「buffer 要帶 header」這個前置條件
   已不存在**（header ABI 是全型別落地，參數 buffer 今天就帶 header，§4.2 b ①）。
   ⚠️ **真正的障礙是語義**：無條件 retain 會把 spawn 邊界的「快照」變成「共享」——任務會看到呼叫端
   在 spawn→`awy` 之間對 `x` 的**原位寫入**——與 §6.3 第 5 條衝突。（分歧**只有**這一種形狀：
   重賦值／離開作用域兩者結果相同；且範圍只到**輸入參數**，out param `r` 不在協議內。）
   正確形式是「**可證明無原位寫入 ⇒ retain**」的靜態閘門（無法證明就退回深拷貝，§4.2 b ⑤），
   前置是「呼叫端寫入點」分析（§3.1，今天仍報告-only）。
3. **`checkTierSoundness` / `checkReleaseTarget`**：要等第 2 項的「呼叫端寫入點／tier」分析落地
   （不是等 header——header 已經有了）才有內容（§4.2 c）。

**一句話總結**：**S／C 兩檔的機制完整且被驗證；R 檔只覆蓋了 `%task`。** 「值對值別名」整類已從模型與
實作裡刪掉（`str` / `[]T` / `?str` / `?[]T` 已收，只剩 map 這種引用語意的型別）；剩下的主要待辦是
參數側的 retain。

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
    OpRun 的參數／結果     → R      ; 目前唯一的 R 來源
    存入 R 檔容器          → R
    被 R 檔值別名          → R      ; 傳遞性
    借讀視圖且之後有寫入    → C
    僅借讀且只讀           → S      ; 讀不提升檔位
    （值對值賦值）         → S      ; 規則一：不產生別名
```

**收斂性**：晶格高度 3，每輪只升不降，最多 2 輪 × |值| 次。

> ⚠️ **`inferTiers` 必須顯式 seed 每個值。** `TierS` 是晶格的零值，所以「只寫入上升的值」會讓全 S 的
> 推論回傳**空 map**。目前是**報告-only**（`NOLANG_MIR_TIER=1` 印到 stderr），對編譯輸出 0 差異。

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
- ⚠️ `async-cancel(h)` 是**已知偽陽性**（內建既不保存也不 retain），但**刻意不做白名單**——
  白名單正是這類分析開始說謊的方式。
- 目前是**報告-only**（`NOLANG_MIR_SPAWN_GRAPH=1`），對編譯輸出 0 差異。

### 3.3 驗證器

| 驗證器 | 不變式 | 狀態 |
|---|---|---|
| `checkDropCount` | I4 | ✅ 已落地 |
| `checkRefBalance` | I3（安全方向） | ✅ 已落地 |
| `checkTierSoundness` | I1, I2 | ⬜ **刻意未實作**（前置條件見 §4.2 c） |
| `checkReleaseTarget` | I5, I6 | ⬜ **刻意未實作**（同上） |

**`checkRefBalance` 的要點**：與插入器**共用謂詞**（直接用 `spawn_graph.go` 的 `handleAliasSet`，
同時走兩種 `OpMove` 編碼、`OpCast`、`OpPhi`）；**只報安全方向**（每個非 root 別名成員必須是某個
`OpTaskRetain` 的運算元；沒 await 的引用不報）；**按 spawn 站點計**（`OpRun` 且 `Sym != ""` 才是真正的
spawn，`Sym == ""` 的 `run <var>` 只是轉發，沒有新計數可平衡）。
**必須有「修復前必須失敗」的測試**（用 `stripTaskRetains` 把修復加上的 retain 拿掉，再要求驗證器開火）
——否則一個「什麼都沒看」的空驗證器也會全綠。

**為什麼 `checkTierSoundness` / `checkReleaseTarget` 此刻刻意不寫**：它們要守的 I1/I2/I5/I6 是
**header ABI 接上使用者值之後才存在**的性質。今天唯一提升到 R 檔的物件是 `%task`——
I1 由 `inferTiers` 的結構性 seed 保證、I2 是真空的（語言裡沒有 R 容器）、I5/I6 是 **codegen 層**的性質
（MIR 看不到 `@nolang_rc_release` 的函式體，而 `ValidateTypes` 是 AST 層的鏈，放不進去）。
**先寫空檢查比不寫更糟**——它會讓「驗證器存在」變成一個虛假的保證。

⚠️ **驗證器的假陽性在此是硬編譯失敗**（`rep.HasErrors()` 會 gate codegen）。**新驗證器必須與插入器
共用同一組判定謂詞**，且要跑**全語料的編譯掃描**。新增診斷要四處齊備（`checker.go` 的 `ValidateXxx`
＋ `lint.go` 的 `RunAllLints` ＋ `build/transpiler.go` 的硬錯誤清單 ＋ `TraceID`）；
⚠️ 若驗證器放在**新檔案**，`Makefile` 的 `TRACE_ID_FILES` 不含它，`TraceID: "PLACEHOLDER"`
**永遠不會被替換** ⇒ 必須硬編碼字面 id（`checkRefBalance` 因此放在**既有檔案** `analysis.go`）。

---

## 4. 落地狀態

### 4.1 已落地

§0.3 已列。細節與量測見 §2／§3，語料見附錄 A，觸點見附錄 B。

### 4.2 未落地

#### (a) 規則一（§1.1）—— **`str`／`[]T`／`?str`／`?[]T` 已落地；只剩 map（引用語意）**

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

**剩餘缺口：只剩 map。** `isOwnedLocal` 對 map 為 false，而 map 是**引用語意**——`Type.Owned` 為 false、
從不被釋放——所以「兩個名字共用一個 map」是它的定義，不是所有權缺陷。要收得先決定 map 的擁有權歸屬，
不是補一個 `OpClone` 能解決的。**刻意不做。**

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
> dangling 指標。守衛讓它變成 no-op ⇒ `nested-container-clone.no` 恢復全綠。**真正的修法是讓 map 的
> 引用計數**（`put` retain 或 clone），那是 §4.2(a) 開頭宣告的 map 缺口，**刻意不做**。

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


#### (b) 參數側的 retain（源仍活時）—— **前置條件已消失；真正的障礙是語義，已重新界定**

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
——正是 §3.1 tier 推斷要算的東西（今天仍是報告-only）。**在沒有那個分析之前，深拷貝是唯一保持語義的
選擇**；無條件 retain 會讓「任務看到什麼」取決於呼叫端在 `awy` 之前寫了什麼，是**語義退化**而非優化。
（輸入參數的契約「不改」正是這個閘門想要**靜態化**的東西——與其在文件裡寫「不應該寫」，不如讓
分析證明它沒寫、然後省掉那份拷貝。）

#### (c) `checkTierSoundness` / `checkReleaseTarget`

前置條件 = (b)。(b) 重新界定之後，等的是**同一個**「呼叫端寫入點／tier」分析（§3.1，今天報告-only）——
要等使用者值真的被提升到 R 檔（或 C 檔 CoW 有寫入點可查）才有東西可守。理由見 §3.3；
「為什麼此刻刻意不寫」見 §3.3 末（先寫空檢查比不寫更糟）。

### 4.3 已知殘留

**借讀 release 的活性精度（不是配對問題）**：借讀值被 option-wrap／enum 建構子**吃掉**時，引用隨容器走，
release 落在**剝離結果**上；在直線程式碼裡配對成立，但那個結果的 drop 位置由（保守的）活性分析決定，
**在迴圈裡會被提到迴圈外** ⇒ 每圈漏一塊（`x = a[0]` 配 `#{index-out}` 的形狀）。語料實測
1717 retain / 1693 release，未配對的 24 個全屬此類；**沒有任何檔案是 release > retain**。
收掉它需要更精確的死亡點分析（must-defined／支配），**與規則一無關**。

---

## 5. 驗證矩陣與操作紀律

| 層次 | 手段 | 判準 |
|---|---|---|
| 單元 | `cd src && go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | ok |
| 端到端 | `GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default bash scripts/mir_golden.sh` | SAME 不減、REGRESS=0 |
| 純重構 | `fp.txt` 前後逐檔 diff `(rc, sha256)` | 「N 檔中 0 檔變動」 |
| **編譯掃描** | 只比 `no build` 的**成功／失敗**（不執行程式） | 全語料 0 差異；**驗證器的假陽性就用這個抓** |
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

**量測基準（截至 2026-10-02，規則一補完 `?str`/`?[]T` 後；控制組 = `no-head777`，真 HEAD `777b9389`）**：
`go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` 全綠（`go vet ./mir/` 乾淨）；
`no vet src/std` E 與控制組**逐位元組相同**（`0 error(s), 6205 warning(s), 936 hint(s)`）；`no test` **8 個 FAIL、與控制組逐行相同**；
語料 **515 檔編譯掃描 0 regression**（與控制組**逐檔相同**，509 過 / 6 敗，敗者兩邊同一組）；
`tests/rule1-binding.no` 在控制組上 `?[]i64` 崩潰、改動後 **9 例全過**；`?[]i64` 10 萬圈 heapstat 由 **100050 塊降到 50 塊**
（`?str` 兩邊皆 50 塊）；`nested-container-clone.no` **12/12 rc=0**；`rule1_binding_test.go` 在 `a6c8559d` 上 **6 敗**、
在 HEAD 與改動後**全過**。

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
| **header 引入後 ABI 混用** | 對裸指標做 header 存取 → 記憶體損壞 | I1/I2 由 `checkTierSoundness` 強制（前置條件見 §4.2 c） |
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
| **12** | **參數側（源仍活）要改成 retain 嗎？** | **不能只做 retain。** 原本記錄的前置條件（「buffer 要帶 header ⇒ 配置點改走 `rc_alloc`」）**已不存在**——header ABI 是全型別落地，參數 buffer 今天就帶 header。真正的障礙是**語義**：retain 會把 spawn 邊界的「快照」變成「共享」，任務會看到呼叫端在 spawn→`awy` 之間對 `x` 的**原位寫入**（實測：深拷貝印 `1`、retain 印 `99`），與使用者文檔「跨協程共享可變堆數據不成立」衝突，且**語料無任何一例測它 ⇒ 會是靜默改變**。要收就收成「**可證明無原位寫入 ⇒ retain**」的靜態閘門——無法證明就退回深拷貝，比 C 檔 CoW 簡單（不需運行時 clone-on-write）且零風險（§4.2 b ⑤）；前置是「呼叫端寫入點」分析（§3.1）。在那之前維持深拷貝。快照語義由 `tests/async-arg-move.no` 第 9 例釘住 |

---

## 附錄 A：語料清單

| 檔案 | 狀態 | 覆蓋的形狀 |
|---|---|---|
| `tests/async-ownership.no` | 新增（P0） | 參數重賦值（str／vec／struct）、同槽雙 await、null handle、單次 await、雙 handle |
| `tests/async-handle-alias.no` | 新增 | 別名雙 await、別名鏈、out-param 返回、存容器逃逸、同槽兩次、spawn+await 循環 |
| `tests/async-rc.no` | 新增 | 跨邊界共享／多任務／取消後不 await／handle 存容器 ＋ 兩個漏計缺陷迴歸（void 別名、`run` 轉發） |
| `tests/async-arg-move.no` | 新增 | spawn 參數 move：5 個該 move（6 個參數）＋ 3 個該維持深拷貝的反向案例（重賦值／讀取）＋ **第 9 例釘「快照」語義**（呼叫端原位寫入 ⇒ 任務仍看到 spawn 當下的值），是 §4.2(b)「無條件 retain」的守門員 |
| `tests/async.no` / `async-cancel.no` / `async-coop.no` / `async-yield.no` / `module-async.no` | 既有 | flat spawn 邊（線性化對照組）、取消、協作、模組層級 async |
| `tests/rule1-binding.no` | 新增（規則一） | `a = b` 的 `str`／`[]i64`／struct 寫入獨立性 ＋ 只讀 ＋ 源已死 move；**唯一 stdout 可觀測的規則一缺陷** |

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
| 借讀 retain／release | `analysis.go` `insertBorrowRetains` / `borrowRetained` / `borrowReleaseValues`；`mir.go` `OpRetain` / `OpRelease`；`codegen.go` `emitRetain` / `emitRelease` |
| 深釋放配深克隆 | `codegen.go` `vecDeepClone` / `vecElemNeedsDeepFree` / `vecDeepFree` |
| spawn 參數 move | `analysis.go`（`wouldDrop` ＋ 獨立第二迴圈）、`mir.go` `Module.spawnArgMoves`、`spawn_graph.go` `SpawnArgClasses` / `DumpSpawnArgMoveStats`、`codegen.go` `emitAsyncRun` |
| spawn graph / 線性化 | `src/mir/spawn_graph.go` `SpawnGraph` / `evalSpawnEdge`（報告-only） |
| Tier 推斷 | `src/mir/tier.go` `Tier` / `joinTier` / `tierConstraint` / `inferTiers` / `DumpTiers`（報告-only） |
| handle 身分集合 | `hir2mir.go` `asyncHandles`（`asyncResTypes` 只管結果型別） |
| `run <handle-var>` 轉發 | `hir2mir.go` `lowerAsyncRun` |
| 借用逃逸 / 視圖降級 | `analysis.go` `checkBorrowEscapes` / `demoteUnsafeSliceViews` |
| 塊內讀取判定 | `analysis.go` `readNonDropAfterInBlock` |
| header ABI | `codegen.go` `@nolang_rc_alloc` / `@nolang_free` / `@nolang_rc_retain` / `@nolang_rc_release`；型別 `%str-long` / `%vec` / `%option` / `%task`（32 bytes） |
| async 排程器 | `codegen.go` `emitAsyncScheduler`（ready queue 可增長）、`emitAsyncRun`、`emitAsyncAwait`、`emitTaskRetain` |
| 克隆欄位守衛 | `codegen.go` `getFieldWasCloned` / `sourceAlreadyDropped` / `lvalueAddrOf` / `emitGetField` |
| MIR 指令定義 | `src/mir/mir.go`（move/clone/drop/borrow、run/await/task-retain、retain/release） |
| 現行模型文檔 | `docs/docs/lang/memory.md` |
| `KFuncLit`（未被 MIR 消費） | `src/hir/hir.go`、`src/parser/tohir.go` |
| 迴歸釘（測試） | `src/mir/{alias_forward,async_boundary_ownership,async_rc_handle,spawn_arg_move,spawn_graph,str_field_lvalue,tier,vec_deep_free,borrow_release,p4_header_abi}_test.go`、`src/mir/option_peel_ownership_test.go`、`src/parser/{struct_literal_field_value,stmt_boundary_block}_test.go` |
