# Nolang 混合所有權模型（Hybrid Ownership）— 工業級設計方案

**版本**：草案 v1
**基線**：`bin/no` @ 2026-09-30 16:33（HEAD 工作樹）
**適用後端**：MIR（`src/mir`）
**前置文件**：`docs/docs/lang/memory.md`（現行模型權威描述）、`NOLANG-AUDIT-2026-09-27.md`
**改動規範**：skill `nolang-compiler-change`（金標、A/B 歸因、並行 session 陷阱）

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

---

## 1. 你的模型：判定與三處必要修正

### 1.1 對的部分

現行 MIR 的 clone/move 決策**已經是「按後續是否使用」判定的**，與你的描述一致：

> `analysis.insertDrops`（`src/mir/analysis.go:1530`）對每個 `OpMove` 問一句：**源在這次搬移之後還活著嗎？**
> - 還活著（`liveOut[blk][src]` 或塊內後續有非 drop 讀取）→ 改寫成 `OpClone`，深拷貝，兩邊各自持有、各自釋放
> - 可證明已死 → 保留零拷貝 move，源豁免 drop

四個判別式依序短路：`moveStructSharesHeap`（`analysis.go:1136`）→ `moveStrSharesHeap`（`:1238`）→ `moveSliceSharesHeap`（`:1273`）→ `moveTransfersOwnership`（`:2175`）；驗證器 `checkDropCount`（`:2391`）必須共用同一聯集 `moveExemptsSource`（`:2214`）。

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

> 實作上這不是新機制，而是把 `insertDrops` 既有的 liveness 查詢**換一個查詢點**：從「move 的源是否還活」改成「首寫時，該值是否有其他活躍別名」。判定謂詞可完全複用 `readNonDropAfterInBlock`（`analysis.go:1069`）與 `liveOut`。

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

滿足時：協程等價於同步呼叫。**這個快速路徑已經實作**——`emitAsyncAwait`（`codegen.go:11874`）在未完成時直接同步呼叫 `resume_fn`，註釋明確寫道「for flat (top-level) await trees … the synchronous drive yields byte-identical output to the legacy event loop without ever entering `nolang_async_run`」。

---

## 2. 現狀基線（實測）

### 2.1 現行決策點與實作位置

| 機制 | 位置 | 觸發 |
|---|---|---|
| 借用逃逸檢查 | `analysis.go:215` `checkBorrowEscapes` | **僅 `OpBorrow`**（`&x` 視圖綁定，`hir2mir.go:9468`） |
| 切片視圖降級 | `analysis.go:278` `demoteUnsafeSliceViews` | 逃逸 **且** 可能被覆寫 |
| drop 插入 | `analysis.go:1530` `insertDrops` | 每個 owned 局部值、每條 CFG 路徑、最後使用之後 |
| drop 數量驗證 | `analysis.go:2391` `checkDropCount` | 「每個 owned 局部恰好一個 drop」 |
| 棧槽重綁定 | `memory.md` §棧類型 move | 源後續**完全無引用**才允許 |

### 2.2 記憶體佈局（ABI 現狀）

```
%str-long = type { i64, i64, i8* }        ; len, cap, data        (codegen.go:1865)
%vec      = type { i64, i64, i64 }        ; len, cap, data        (codegen.go:1866)
%option   = type { i64 tag, [N x i64] slot }                      (codegen.go:1868)
%task     = type { void (i8*)*, i64, i1, i1 }  ; 24 bytes         (codegen.go:1873)
```

**關鍵事實：`data` 是裸指標，配置塊沒有 header。** `@str_clone` 就是 `malloc(len+1)` 後直接 memcpy（`codegen.go:2421-2438`）。

**這件事決定了 RC / runtime-CoW 的可行性**：要嘛引入 header（ABI 變更，`codegen.go` 內 28 個 `@malloc` 站點與 18 個 `@free` 站點要統一走新的 alloc/release 入口），要嘛用 side table（單執行緒下不需鎖，但每次複製/釋放都要 hash 查表——對系統語言不可接受）。

→ **本方案採用 header，但只在 C/R 檔；S 檔完全不變**（§3.4）。

### 2.3 🔴 已實測的三個 async 邊界缺陷

#### 2.3.1 同一 handle await 兩次 → SIGSEGV

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

**根因**：`emitAsyncAwait` 在讀完結果後無條件釋放三個容器（`codegen.go:11951-11962`）：

```
call void @free(i8* %aawy.freeres)    ; 結果緩衝區
call void @free(i8* %dataI8)          ; args 結構
call void @free(i8* %aawy.freetask)   ; %task 本體
```

但 handle 只是個 `i64`（`ptrtoint i8* -> i64`，`codegen.go:11864`），**沒有「已釋放」狀態**。第二次 `awy` 走同一條路徑，對已釋放指標再 free 一次。

#### 2.3.2 spawn 後重賦值參數 → 靜默資料損壞（UAF）

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

**根因**：`emitAsyncRun` 對每個參數做的是**位元複製**（`codegen.go:11800-11820`）：

```
%arun.argbuf = call i8* @malloc(i64 24)
store %str-long %areg, %str-long* %arun.argbuf.t    ; 只複製 {len,cap,data} 描述符
```

`data` 指標與呼叫方**共享**。而 `insertDrops` 的 move 豁免清單**不含 `OpRun`**（只列了 `OpMove`/`OpOptionWrap`/`OpStrFromVec`/`OpEnumNew`/`OpSetField`），所以呼叫方的局部值仍保有自己的 drop → 重賦值時 free 掉任務還握著的 buffer。

同時，callee 的參數是「借用」語義（`insertDrops` 從不 drop 參數），所以任務**不會**釋放它。**唯一所有者是呼叫方，而呼叫方的生命期可能早於任務。**

#### 2.3.3 handle 從不 await → 洩漏（原始碼推論）

`@malloc` 的三個容器（task / args / 各 argbuf）唯一的釋放點就是 `emitAsyncAwait`。因此：

- handle 被丟棄、或存入容器後從不取出 await → task + args + argbufs **全數洩漏**；
- handle 被存進 `[]i64` 之類的容器 → 容器本身是 `i64` 值，不觸發任何 drop。

#### 2.3.4 附帶發現：ready queue 是固定 256 槽環形佇列，無溢出檢查

`nolang_async_enqueue`（`codegen.go:11512-11520`）對 `@nolang_ready_q[256]` 寫入時只做 `urem 256`，**不檢查 head/tail 是否追上**。若同時存活超過 256 個未完成的 task（例如在迴圈裡 `run` 而不 await），會**靜默覆寫**佇列條目——任務遺失或指標錯亂。這與所有權模型無關，但屬於同一邊界的健全性缺口，建議一併處理（改為溢出時同步驅動或報錯）。

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

- `readNonDropAfterInBlock`（`analysis.go:1069`）——「之後是否還有真正的資料讀取」，`OpDrop` 不算讀取（這個區分是既有正確性的一部分，見 `analysis.go:1587` 的註釋）
- `liveOut`（`m.Liveness`）
- `emitClone`（`codegen.go:5199`）——已有 `%str-long`（`@str_clone`）、`%vec`（`vecDeepClone`）、heap-option 三條路徑

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
| `checkDropCount` → `checkRefBalance` | I3, I4 | `analysis.go:2391`（擴充） |
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

### P1 — Tier 推斷框架（只實作 S）

**目標**：把「檔位」概念引進編譯器，但**所有值都判為 S**，行為與現況位元組全等。

**做法**：新增 `tier.go`，實作 §3.2 的晶格與 §4.1 的定點；在 `insertDrops` 之前執行；所有 `constraint` 先返回 S。

**驗收**：**金標 0 差異**（用 `/tmp/mir_golden/fp.txt` 逐檔比對 `(rc, sha256)`，必須「N 檔中 0 檔變動」）。這是純重構的證明。

### P2 — Tier C：首寫 clone 插入

**目標**：把 clone 從賦值點搬到首寫點。

**做法**：實作 §4.2；`constraint` 加入「別名且別名後有寫入 → C」。

**驗收**：
- 金標：SAME 不減、REGRESS=0；
- **所有 clone 數量變化逐檔可解釋**（用 `NOLANG_MIR_DUMP_MIR=1` 統計 `clone` 指令數，前後對比）；
- 預期方向：clone 數量**只減不增**（首寫點比賦值點更晚、更精確）。

**風險**：若某檔 clone 數量**增加**，說明 liveness 查詢點的搬移引入了不保守的判定 → 必須查清後才前進。

### P3 — Escape / spawn graph 分析

**目標**：實作 §4.3 的線性化判定，產出「哪些 spawn 邊是線性的」報告。**先只報告，不改變行為。**

**驗收**：對 `tests/async*.no` 與 `tests/module-async.no` 輸出判定結果，人工核對。此時編譯輸出仍應 0 差異。

### P4 — Header ABI 與 RC 執行時原語

**目標**：引入 §3.4 的 header，實作 `alloc`/`release`/`retain`。**但先不接到任何值上**（所有值仍是 S/C，走舊路徑）。

**做法**：在 `codegen.go` 新增 runtime helper，並把 28 個 `@malloc` / 18 個 `@free` 站點中**屬於 R 檔的**改走新入口（S/C 的不動）。

**驗收**：金標 0 差異（新 helper 未被任何程式碼引用 → 死碼，LLVM 會移除）。用 `-v` 比對 IR 的排序後差異（skill 提到宣告順序不確定，必須 `diff <(sort a.ll) <(sort b.ll)`）。

### P5 — 在逃逸邊界插入 retain/release

**目標**：把 P3 判定的「非線性 spawn 邊」上的值提升到 R 檔，插入 retain/release。

**驗收**：
- §2.3 的三個缺陷在此階段**由 RC 機制統一解決**（P0 的修復可以被 P5 取代，或保留為雙保險）；
- 新語料 `tests/async-rc.no`（跨邊界共享、多任務、取消後不 await、handle 存容器）；
- 洩漏量測：`/usr/bin/time -l <binary> | grep "maximum resident"`，用**可觀測**的大負載（skill 強調：資料必須被讀取，否則 LLVM 會把整個 malloc/free 對消掉）；
- 金標：REGRESS=0。

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
| **header 引入後 ABI 混用** | 對裸指標做 header 存取 → 記憶體損壞 | I1/I2 由 `checkTierSoundness` 強制；P4 階段先讓 helper 成為死碼驗證 0 差異 |
| **`shouldUseMemcpy` 被間接改變** | 大聚合的 `loadVal` 由「回傳值」變「回傳槽指標」→ 走進從未執行過的路徑 | 任何可能改變 `computeTypeSize` / 4096 門檻 / 欄位佈局的改動**必須跑金標** |
| **驗證器假陽性** | `rep.HasErrors()` gate codegen → 整檔編不過 | 與插入器共用謂詞；每個新驗證器附「修復前必須失敗」的單元測試 |
| **ready queue 溢出**（§2.3.4） | 任務靜默遺失 | 與 P0 一併處理 |

### 7.3 已知取捨（必須寫進使用者文檔）

1. **RC 檔的循環引用會洩漏。** 這是確定性記憶體管理的標準代價。
2. **R 檔有 inc/dec 開銷。** 但只要不跨協程邊界，值就留在 S/C 檔——這是本方案相對「全語言 RC」的核心優勢。
3. **跨協程邊界的深拷貝**（P0 的最小修復版）比 RC 貴，但語義最簡單。P5 之後可改為 retain 以省掉拷貝。

---

## 8. 需要你決策的未決問題

1. **P0 的修復策略**：先做「spawn 時深拷貝參數」（簡單、貴、語義清晰），還是直接跳 P5 的 RC（一步到位、改動大）？
   → 建議：**先深拷貝**。理由是它可獨立發布、可獨立回滾，且給 P5 一個正確性對照組。

2. **header 的 sentinel 編碼**：借用/靜態塊用 `rc = 0`（計數 0 表示永不 free）還是 `flags.bit1`？
   → 建議 `rc = 0` 單一判準，因為 `release` 只需一次比較；`flags` 保留給未來擴充。

3. **檔位是否對使用者可見**：要不要提供 `#{tier=R}` 之類的註解讓使用者強制指定？
   → 建議**先不開放**。檔位是推斷結果，開放註解會讓它變成契約，驗證器複雜度大增。

4. **`%task` 的欄位擴充**：新增欄位（`%task` 由 24 → 32 bytes）還是複用 `cancelled` 為 `flags` 位元組？
   → 建議**新增欄位**，因為 `cancelled` 是 `i1` 且語義明確，複用會讓 `async-cancelled()` 的讀取路徑變複雜。

5. **是否處理 §2.3.4 的 ready queue 溢出**：溢出時同步驅動（行為改變）還是報硬錯（更安全）？
   → 建議**報硬錯**，因為靜默覆寫是目前最危險的行為。

---

## 附錄 A：新語料清單

| 檔案 | 覆蓋 | 修復前預期 |
|---|---|---|
| `tests/async-double-await.no` | 同 handle await 兩次 | SIGSEGV |
| `tests/async-arg-reassign.no` | spawn 後重賦值堆參數（§2.3.2） | 靜默資料損壞 |
| `tests/async-handle-drop.no` | handle 從不 await | 洩漏（量測） |
| `tests/async-handle-container.no` | handle 存進 `[]i64` 後取出 await | 崩潰/洩漏 |
| `tests/async-rc-shared.no` | 多任務共享同一 buffer（P5） | — |
| `tests/async-linear.no` | 線性化判定：單 spawn、單 await | 修復前後**輸出必須相同**（防過度提升） |
| `tests/async-cancel-noawait.no` | cancel 後不 await | 洩漏 |

⚠️ 語料撰寫注意（skill 已載明）：`;` 是**行註解**不是陳述分隔符；`''` 是 `str`、`""` 是 `char`；切片寫 `[a..b)`；寫測試檔不必繞開 `s.len()` / `n.to-str()`，但 **option match 必須三個臂都寫**。

## 附錄 B：觸點索引

| 主題 | 位置 |
|---|---|
| drop 插入 | `src/mir/analysis.go:1530` `insertDrops` |
| move 四判別式 | `analysis.go:1136` / `:1238` / `:1273` / `:2175` |
| 判別式聯集 | `analysis.go:2214` `moveExemptsSource` |
| drop 數量驗證 | `analysis.go:2391` `checkDropCount` |
| 塊內讀取判定 | `analysis.go:1069` `readNonDropAfterInBlock` |
| 借用逃逸 | `analysis.go:215` `checkBorrowEscapes` |
| 切片視圖降級 | `analysis.go:278` `demoteUnsafeSliceViews` |
| MIR 指令定義 | `src/mir/mir.go:376-381`（move/clone/drop/borrow）、`:485-493`（run/await） |
| 型別佈局 | `src/mir/codegen.go:1865-1874` |
| `@str_clone` | `codegen.go:2421` |
| `emitClone` | `codegen.go:5199` |
| `emitAsyncRun` | `codegen.go:11720`（參數位元複製在 `:11800-11820`） |
| `emitAsyncAwait` | `codegen.go:11874`（容器 free 在 `:11951-11962`） |
| async 排程器 | `codegen.go:11504-11585`（ready queue 在 `:11512`） |
| `%task` 型別 | `codegen.go:1873` |
| async 語義（使用者面） | `src/std/async.no`（D10） |
| 現行模型文檔 | `docs/docs/lang/memory.md`（已知限制在 `:398-399`） |
| `KFuncLit`（未被 MIR 消費） | `src/hir/hir.go:84`、`src/parser/tohir.go:920` |
