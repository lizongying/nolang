# Tagged enum 載荷所有權 — 完整方案

> 狀態：Phase 1 已實作並驗證（見 §6、§8）。Phase 2/3 為設計，尚未實作。
> 相關檔案：`src/mir/analysis.go`、`src/mir/codegen.go`、`src/mir/hir2mir.go`、`src/mir/mir.go`
> 迴歸測試：`tests/tagged-enum-two-match.no`

## 1. 問題

**現行模型**：tagged enum 是一個 `{ i64 tag, [N x i64] payload }` 的 union 結構，而
**enum 自己不是 `Owned` 型別，永遠不會被 drop**。載荷的所有權預期由「抽取」搬出去：

- `enum-new` 把載荷參數的擁有權收進 enum（`analysis.go` 對 `OpEnumNew` 的 args 標 `moveSrc`）；
- 抽取（`OpEnumField`）把載荷讀出來時，**接手**那份擁有權並負責 drop。

只要「抽取恰好一次」這個前提成立，這個模型是自洽且零額外成本的。它有三個破口：

| # | 破口 | 症狀 | 狀態 |
|---|---|---|---|
| A | 臂主體**每次讀欄位名都重新投影**（`enumArmFieldValue`），與 parser 合成的綁定各投影一次 → 兩個擁有者 | `trace/BPT trap`（double free） | **已修**（`lowerer.armBoundNames` 讓讀取回傳綁定） |
| B | **同一個 enum 被 match 兩次** → 兩次抽取、第二次讀已釋放緩衝 | 修前 double free／編譯失敗；修後**靜默空字串** | **已修**（本方案 Phase 1） |
| C | enum 建構後**從未被 match** → 載荷永遠不釋放 | 洩漏（`checkDropCount` 目前不報，靜默） | 本方案（Phase 3） |

B 的最小重現：

```no
e-res { ok(v str), fail, }

main = () {
    q e-res = ok('hi')
    q: { ok(v) -> print('first: '  - v)  fail -> print('no') }
    q: { ok(v) -> print('second: ' - v)  fail -> print('no') }   ; 印出空字串
}
```

## 2. 設計原則（使用者指定）

> 看用戶的意圖：**如果只有一次，那就是 move；多次，就是 clone。**

對應到本問題：

- **單次取用** → 載荷**搬出**（move）。零額外成本，維持現行路徑。
- **可能多次取用** → enum **自己擁有並釋放載荷**（tag-switched drop），抽取改為
  **clone**（每個抽取自擁一份，各自 drop）。

這正是 Nolang 既有的 **option 模型**：`?T` 自己擁有並 drop 載荷，剝殼一律
`str_clone` / `vecDeepClone`（見 `MEMORY.md` 的「剝殼不消耗 option」條）。本方案讓
tagged enum 在多取用時**收斂到同一個模型**，而不是發明第三套。

## 3. 判準：single-use vs multi-use

### 3.1 為什麼不能用型別層級判準

`typeOwnsHeap(ty)` 是**型別**層級的。若讓所有「帶 owned 載荷的 tagged enum」都擁有載荷，
則單次取用的 enum 也會被 drop —— 而它的抽取已經把載荷搬走了，兩邊都 drop 就是 double free。
所以「是否擁有」必須是**值**層級的判斷。

### 3.2 判準（Phase 1，函式內）—— 「被 match 兩次」，不是「被抽取兩次」

⚠️ 這一條是實作中修正過的。第一版判準寫成：

```
extra(v) = #{ OpEnumField i : i.Args[0] == v }
owning(v) ⟺ extra(v) >= 2          ; ❌ 錯的
```

**為什麼錯**：一次 match 一個**多欄位**變體會抽**每一個**欄位 ——

```no
rect(w, h) -> ...      ; 產生 OpEnumField slot 0 與 slot 1
```

於是 `tests/tagged-enum.no` 的 `shape`（`rect` 只被 match 一次）得到 `extra == 2` →
被誤判成 owning → `moveSrc` 豁免被抑制 → `checkMoves` 報
`[use-after-move] value 3 dropped after move`（實測）。**單次 match ≠ 兩次 match。**

真正標識「重複 match」的是 **arm body**，而每個 arm body 都降落到自己的 basic block，故：

```
owning(v) ⟺ 存在兩個（以上）block，各自從 v 抽了**同一組** slot
```

- 一個 block 抽多個 slot = 一次 match 一個變體 → **不算**。
- 兩個 block 抽同一組 slot = 同一變體被 match 兩次 → owning。
- 兩個 block 抽**不同**的 slot 集合 = 同一次 match 的不同臂（變體不是靜態已知）→
  刻意留在 move 路徑（見 §7.4）。

實作：`markEnumPayloadOwners` + `sameSlotSetTwice`（`analysis.go`）。

### 3.3 為什麼不做 union-find（別名群）

`let r = q`（tagged enum 的複製）在 MIR 是 `move dst=r args=[q]`。理論上應把
`q`／`r` 併成同一個別名群再計數。Phase 1 **刻意不做**，改為一條更窄、更安全的規則：
**`OpMove` 的目的地永不標為 owning**（它是別名，所有權留在來源）。
因為 `OpMove` 在 enum 值之間也被用於 `it` 槽的別名（例：`tests/tagged-enum.no` 的
`move dst=25:a-res args=[21]`），合併會把不同臂的 `it` 值混在一起而**過度標記**。
代價是 §7.2 的已知限制。

### 3.4 白名單：helper 能釋放的載荷形狀

即使 `owning` 成立，還要 `enumPayloadFreeable(ty)` 通過才真的標記：該 enum 型別
**所有**變體的**所有** owned 欄位都必須是 `emitEnumDropHelper` 認得的形狀
（`str` / 切片`vec` / 有 owned leaf 或指標欄位的內嵌 struct）。否則留在既有的
move-once 路徑 —— 洩漏是安全的一側，半釋放不是。

## 4. 機制

### 4.1 值層級標記（`src/mir/analysis.go`）

```go
// Module 新增：enumOwnsPayload[v] == true 表示 v 這個 tagged-enum 值自己擁有載荷
enumOwnsPayload map[ValueID]bool        // mir.go；NewModule 初始化，Analyze 開頭清空

func (m *Module) markEnumPayloadOwners(f *Function)  // §3.2 的判準 + §3.4 的白名單
func (m *Module) sameSlotSetTwice(blocks map[BlockID]map[int64]bool) bool
func (m *Module) enumPayloadFreeable(ty *Type) bool  // §3.4
func (m *Module) enumFieldFreeable(ft *Type) bool    // §3.4；與 emitEnumDropHelper 同步

func (m *Module) dropOwnsHeap(f *Function, v ValueID) bool {
    if m.enumOwnsPayload[v] { return true }   // ← 新增，其餘不變
    ...
}
```

三處配套（都在 `analysis.go`）：

1. `insertDrops` 入口呼叫 `markEnumPayloadOwners(f)` —— 必須在任何
   `dropOwnsHeap` / `isBorrowRead` 詢問之前。
2. `moveSrc` 的 `isTransferringMove` 規則加上 `&& !m.enumOwnsPayload[inst.Args[0]]`：
   owning enum 的 `OpMove` 是**別名**，不是轉移（載荷內嵌在 enum 裡，位元複製只複製了
   data 指標）。不豁免的話會**抑制掉唯一的那次 drop**，修復變成洩漏。
3. `isBorrowRead` 新增 `OpEnumField` 分支（§4.4）。

**不動 `typeOwnsHeap`** —— 它是型別層級的，且同時驅動呼叫慣例與 clone 決策（`Type.Owned`
的註解明確警告過），擴寬它會改變每個 struct 的 ABI。`dropOwnsHeap` 的 enum 分支刻意放在
型別查詢**之前**：tagged enum 的 `Type.Owned` 依設計是 `false`。

### 4.2 tag-switched drop（`src/mir/codegen.go`）

**完全比照 `emitOptionDrop` / `emitOptionDropHelper`**（`codegen.go:4868`、`:4957`）。
那段程式碼的註解已經把理由寫死了，本方案直接沿用：

> 釋放必須是 **tag-guarded** 且住在**helper 函式**裡，因為 `emitDrop` 在 block 中間執行，
> 在那裡開新 basic block 會破壞 LLVM 驗證（`emitOptionBoxHelpers`、
> `emitStructDropHelper` 同理）。

新增：

```go
func enumDropName(lt string) string            // "__nolang_enum_drop_" + sanitize(...)
func (c *codegen) emitEnumDropHelper(ei *TaggedEnumInfo, lt string)
```

產生的 helper 形狀（每個變體一個 branch，只對**帶 owned 欄位**的變體產生）：

```llvm
define void @__nolang_enum_drop_tenum_e_res(%tenum_e_res* %p) {
entry:
  %tp = getelementptr inbounds %tenum_e_res, %tenum_e_res* %p, i32 0, i32 0
  %t  = load i64, i64* %tp
  %c0 = icmp eq i64 %t, 0
  br i1 %c0, label %v0, label %next0
v0:                                   ; ok(v str)
  ; payload 槽位 bitcast 成 %str-long*，載入後 @str_free
  call void @str_free(%str-long %lv)
  br label %done
next0:
  ...                                 ; 其餘變體；無 owned 欄位者直接 br %done
done:
  ret void
}
```

**逐欄位釋放規則**（與 `emitStructDropHelper` 一致）：

| 欄位型別 | 釋放方式 |
|---|---|
| `str` | `@str_free`（本身就跳過 cap==0 / null data） |
| `vec` / `[]T` | 取 field 2 的 data 指標 → `@free` |
| 內嵌 struct（有 owned leaf 或 ptr 欄位） | `call @__nolang_drop_<struct>(...)`（遞迴） |
| 純量 / 無 owned 欄位 | 略過 |

`emitDrop` 新增分支（放在 `loadVal` **之前**，早退不留副作用）：

```go
func (c *codegen) emitDrop(inst *Inst) error {
    lt, _ := c.ptype(inst.Args[0])
    if c.mod.enumOwnsPayload[inst.Args[0]] {
        if val := c.mod.Value(inst.Args[0]); val != nil {
            if ei := c.taggedEnumOf(enumRawOfType(c.mod, val.Type)); ei != nil {
                if slot := c.valSlot[inst.Args[0]]; slot != "" {
                    c.emitEnumDropHelper(ei, lt)
                    c.sb.WriteString(fmt.Sprintf("  call void @%s(%s* %s)\n", enumDropName(lt), lt, slot))
                    return nil
                }
            }
        }
    }
    _, v := c.loadVal(inst.Args[0])
    ...
```

⚠️ 分支鏈收尾：**最後一個變體**的 false 邊直接指向 `done`。若給它自己的 `nvN` 標籤，
該 block 會「落到」`done:` 而**沒有終結指令** → LLVM 報 `expected instruction opcode`
（實作時踩到）。變體數為 0 時 entry 也要補一條 `br label %done`。

（`%vec` 的 payload 直接用 `@free(data)` 也可以，但走 helper 才能 tag-guard。）

### 4.3 抽取 clone（`src/mir/codegen.go` `emitEnumField`）

**完全比照 `emitGetField` 對 owned `str` 欄位的處理**：`emitGetField` 會
`call %str-long @str_clone(...)`，所以讀出來的是**私有緩衝**，`isBorrowRead` 對它回
`false`（要 drop）—— 這段理由在 `analysis.go:870-897` 有完整註解。

`emitEnumField` 在 `enumOwnsPayload[inst.Args[0]]` 為真、且欄位是 **owned `str`** 時，
於 load 之後插入 `@str_clone`。

### 4.4 分析層的配套（`src/mir/analysis.go` `isBorrowRead`）

新增 `OpEnumField` 分支，**鏡射 `OpGetField` 的判斷**：

```go
if inst.Op == OpEnumField {
    if !m.enumOwnsPayload[inst.Args[0]] {
        return false   // 單次取用：抽取把載荷搬出去並負責 drop
    }
    if ty := m.valueTypeOf(f, inst.Dst); ty != nil && ty.Owned && ty.Kind == KindStr {
        return false   // codegen 已 clone → 私有緩衝，要 drop
    }
    return m.dropOwnsHeap(f, inst.Dst)   // 其餘 owned 欄位：借用，不 drop
}
```

這樣 `%vec` / struct 載荷是**借用**（由 enum 的 drop 負責），`str` 載荷是**clone**後自擁。

### 4.5 參數（callee 不 drop）

`insertDrops` 早已把輸入參數排除在 `declared` 之外（輸入參數是**借用**，呼叫者保留所有權）。
所以：

- enum 參數**永遠不會**被 callee drop（既有規則，不需改）；
- Phase 1 的判準在 callee 內**獨立計算**，若參數在 callee 內被 match ≥2 次，它會被標成
  owning → 抽取會 clone（正確，自擁私有副本）→ 但沒有 drop（借用）→ 正確。

## 5. 相容性 / 回歸風險

| 情境 | Phase 1 行為 | 風險 |
|---|---|---|
| match 1 次（語料庫絕大多數） | **完全不變**（move） | 無 |
| match ≥2 次 | 新增 tag-switched drop + 抽取 clone | 新程式碼，靠測試把關 |
| match 0 次 | 不變（既有洩漏） | 無（不變差） |
| 單次 match 多欄位變體 | 不變（判準見 §3.2） | 無 |
| 同一次 match 的不同臂抽不同變體 | 不變（§7.4） | 無 |
| enum 被複製 | 不變 | §7.2 限制 |

**關鍵安全性質**：判準只在「同一組 slot 被兩個 block 抽過」時才啟用，所以語料庫裡所有
單次 match 的 enum **走的程式碼路徑與修改前逐條相同**。golden sweep 的 `REGRESS=0`
因此是可以達成的（實測見 §8）。

## 6. 測試計畫與實測

1. **`tests/tagged-enum-payload-ownership.no`**（round 2 新增，覆蓋 A 的各種形狀）。
2. **`tests/tagged-enum-two-match.no`**（本輪新增，B 的迴歸，7 組）：
   單欄位 str ×2、改名綁定 ×2、多欄位變體 ×2（同時守住 §3.2 的判準）、切片載荷 ×2、
   match 三次、兩次 match 之間插入語句、巢狀（外層 move + 內層 owning 互不干擾）。
3. **A/B 鑑別力**（三個 binary，同一份測資）：

   | binary | `tests/tagged-enum-two-match.no` |
   |---|---|
   | HEAD（只有 round 1） | 編譯失敗：`%lv73` 型別不符（第二個 match 綁到舊槽位） |
   | **round 2 only**（本輪判準停用） | `1b:`／`2b:` 印**空字串**，case 4 **segmentation fault** |
   | **本版** | 7 組全部正確，rc=0 |

   round-2-only 的 binary 是把 `markEnumPayloadOwners` 開頭改成 `if true { return }` 另建的
   scratch build，用來證明本測資確實鑑別本輪的修復，而不只是 round 2 的。
4. **洩漏檢查**：`no build` 不應出現新的 `missing-drop`。
5. **golden sweep**：`GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default scripts/mir_golden.sh`
   → 必須 `REGRESS=0`，且 `sum(buckets) == 464`。三個既有的 DIVERGE
   （`default-params` / `std-hash` / `std-new`）必須維持 `NOW==HEAD`。
6. `go test ./mir ./parser ./checker ./fmt ./hir`、`no vet src/std` 0 error。

### 6.2 實測結果（2026-09-25）

```
golden: tests/golden/mir-baseline.tsv (464 entries)
SAME=      461
DIVERGE=   3      (default-params / std-hash / std-new)
UNSTABLE=  0
REGRESS=   0      ← 關鍵
IMPROVED=  0
BOTH_FAIL= 0
NEW=       14     (相對 golden 新增的測檔；含本輪兩支)
```

`461 + 3 = 464` ✓。三個 DIVERGE 逐一做 **golden / HEAD / NOW 的三方比對（rc + sha）**：
三者皆 `NOW == HEAD` → 是 golden 凍結後才漂移的既有偏差，**不是**本次造成的。

`go test ./mir ./parser ./checker ./fmt ./hir` 全 ok；`no vet src/std` **0 error**。
NEW 桶的 14 支（golden 未覆蓋，sweep 不比較）另行逐支做 HEAD vs NOW 的 rc+sha 比對：
除本輪兩支由 rc=1 改善為 rc=0 外，其餘全部相同。

> 附帶發現（**既有、與本修復無關**）：`tests/markdown.no` 的 stdout **本身不確定**
> —— 同一支 binary 連跑 14 次會得到兩種輸出（約 10:4），差別是 `ok: table` 那行有時
> 不出現（`markdown.to-html` 的表格分支）。HEAD 與 NOW 的分布**完全相同**（皆 10:4），
> 故非本次造成。⚠️ 它目前不在 golden 裡（golden 早於該檔），所以 sweep 沒有比較到它；
> 但**下次 `-update` 會把一個不確定的 hash 凍進去**。建議先修該不確定性，或把它加進
> `$MIR_GOLDEN_UNSTABLE`（附理由與 bug 號）。

### 6.1 實測中發現、**與本修復無關**的既有缺陷（A/B 已確認 HEAD 同樣失敗）

寫測試時撞到，記錄下來避免以後誤判成本次修復的迴歸：

| 形狀 | 症狀 |
|---|---|
| `x.len()`（切片／`str` 的長度方法） | 無法編譯：`consumer reads value 0 (NoVal)`（bug #85）。**純切片區域變數也一樣** |
| 臂綁定的整數上呼叫 `.to-str`（`full(v) -> print(v.to-str)`） | 同上 bug #85。改用 `print(v)` 可直接印出 |
| `print('x' - items[0] - items[1])`（切片載荷內聯索引） | 同上 bug #85。改成先賦值給區域變數即可 |

## 7. 已知限制（Phase 1 不做，明列避免誤解）

1. **跨函式的多次 match**：`main` match 一次、`show(q)` 內再 match 一次 → 兩邊各自算 1 次，
   都不 owning → 仍是 move-once 語義（第二次讀到已釋放緩衝）。**與修改前一致，未變差。**
2. **複製後的多次 match**：`r e-res = q` 之後 `q:` 與 `r:` 各 match 一次 → 目標是 move
   **目的地**故永不 owning，來源也只算 1 次 → 都不 owning。**與修改前一致。**
3. **match 0 次的洩漏**：維持既有行為（Phase 3）。
4. **同一次 match 的不同臂抽不同變體**（變體非靜態已知）：兩個 block 抽**不同**的 slot 集合
   → 不 owning → move 路徑。若真的是同一個值在執行期被抽兩次，這個形狀**修不到**
   （判準刻意保守，見 §3.2 第三條）。
5. **欄位形狀不在白名單內的 enum**（如 `?T` 載荷）：不 owning（§3.4）。

> 這幾項要正確處理，需要**跨程序**的所有權分析（callee 參數的 owning 標記 + 呼叫者逃逸
> 標記），以及 `OpMove` 別名群的 union-find。設計上可行，但會讓語料庫大面積改變行為，
> 屬於 Phase 2，需先有 Phase 1 的綠燈作為地基。

## 8. 分階段

| Phase | 內容 | 狀態 |
|---|---|---|
| **1** | 值層級 `enumOwnsPayload`（同一組 slot 被兩個 block 抽過）＋ tag-switched drop ＋ 抽取 clone ＋ `isBorrowRead`／`moveSrc` 配套 | **已實作** |
| **2** | 跨程序：callee enum 參數的 owning 標記（R1）＋ 呼叫者標記（R2）＋ 逃逸閘門 | **已實作** |
| **2b** | 別名（`r = q`）：別名「之後還會被讀」時把那個 `OpMove` 改寫成 `OpClone`（tag-switched 深拷貝），兩邊各自擁有一塊；並統一 `checkMoves` 與 `insertDrops` 的 owning-enum 豁免 | **已實作** |
| 3 | match 0 次 → owning（修洩漏），並讓 `checkDropCount` 對 tagged enum 也做檢查 | 未實作 |

### 8.1 落地的檔案

| 檔案 | 內容 |
|---|---|
| `src/mir/mir.go` | `Module.enumOwnsPayload`、`Module.enumExtractSites` 欄位 + `NewModule` 初始化 |
| `src/mir/analysis.go` | `markEnumPayloadOwners`、`sameSlotSetTwice`、`enumPayloadFreeable`、`enumFieldFreeable`、`markEnumParamOwners`、`calleeConsumesEnumArg`、`enumEscapeSinks`、`enumPayloadCloneable`、`moveEnumSharesHeap`、`moveTransfersOwnership`；`Analyze` 清空標記並跑 R1 前導掃描；`insertDrops` 入口呼叫；`dropOwnsHeap` enum 分支；`isBorrowRead` 的 `OpEnumField` 分支；別名改寫（§10）；`checkMoves` 改用 `moveTransfersOwnership` |
| `src/mir/codegen.go` | `enumDropName`、`emitEnumDropHelper`、`emitEnumPayloadFieldFree`、`enumCloneName`、`emitEnumCloneHelper`、`enumPayloadSlotPtr`、`emitEnumPayloadFieldClone`；`emitDrop` 分支；`emitEnumField` 的 owned-`str` clone；`emitClone` 的 enum 分支 |
| `tests/tagged-enum-two-match.no` | Phase 1 迴歸測試（7 組） |
| `tests/tagged-enum-cross-fn.no` | Phase 2 迴歸測試（7 組） |
| `tests/tagged-enum-alias.no` | Phase 2b 迴歸測試（10 組） |

---

## 9. Phase 2 設計：跨函式（已實作）

### 9.1 觀測：enum 以「指向呼叫者槽位的指標」傳遞

實測 `tmp/xfn-probe.no`（`main` match 一次、`show(q)` 內再 match 一次）：

```llvm
  call void @show(ptr %v1.s)                       ; main 把 q 的槽位位址傳出去
define void @show(ptr readonly captures(none) %p0) ; callee 直接讀呼叫者的儲存
```

⇒ **callee 的參數就是呼叫者的儲存**。所以：

- callee **絕不可**釋放該載荷（它不擁有那塊儲存）；
- 呼叫者**必須**釋放（它擁有）。

而 Phase 1 之前的行為正好相反：callee 的抽取「搬出」載荷並 drop ⇒ 釋放呼叫者的緩衝；
呼叫者自己的抽取也 drop ⇒ **double free**（實測 `in show:` 印空字串 + `trace/BPT trap`）。

### 9.2 兩條規則

**R1（callee 側）**：函式 `f` 的 **參數／result param** `p` 若是 enum 型別且被抽取
（有 `OpEnumField args[0]==p`）→ 標 `enumOwnsPayload[p] = true`。

- 效果：抽取改為 clone（owned `str`）或 borrow（`%vec`／內嵌 struct）；param 本來就
  排除在 `droppable` 之外，所以 callee 不會 drop 它。
- 這是**安全**的一側：callee 停止釋放一塊它不擁有的緩衝。
- 也是**必要**的：少了它 callee 會釋放呼叫者的緩衝。

**R2（caller 側）**：函式 `f` 的 enum 值 `v` 若作為引數傳給「**會抽取對應參數**的已知
Nolang 函式」→ 標 owning。

- 效果：呼叫者在 `v` 的最後使用處釋放一次；callee 依 R1 借用／clone ⇒ 無論呼叫幾次都正確。
- ⚠️ 條件刻意寫成「**會抽取**的 callee」而不是「任何呼叫」：傳給不抽取的 callee（例如只比較
  tag 的述詞）不需要改變任何東西，加上這一條把 blast radius 從「所有 enum 引數」縮到
  「會搬走載荷的那些」。這需要一個**前導掃描**：`Analyze` 先算出每個函式抽取了哪些 param。

`OpCallExtern` / `OpCallFFI` 不參與 R2 的觸發（C 函式不可能抽取 Nolang 的 tagged enum）；
`Variadic` callee 不參與（引數↔參數不是 1:1，配對不可靠）；`OpCall` 但 `Callee != NoVal`
（間接呼叫，fn 型別的區域變數／參數）無法靜態解析 callee，不參與。

### 9.3 閘門（R2/R3 共用，維持「一個載荷恰好一次 drop」）

| 閘門 | 理由 |
|---|---|
| **move 目的地**不 owning | `move dst=r args=[q]` 是位元別名（載荷內嵌在 enum 裡），所有權留在來源；兩邊都標就是 double free |
| **escape sink** 不 owning | 值流進 `OpSetField`／`OpIndexStore`／`OpOptionWrap`／`OpEnumNew`，或本身就是 result param，或被 move 進一個會 escape 的值（用 worklist 傳遞）⇒ sink 會保留一份**淺別名**，在 `v` 的結束處釋放會讓那份別名懸空 |
| **不透明的呼叫**不 owning | `OpCall` 的 callee 解析成 builtin、以及 `OpCallExtern`／`OpCallFFI`／間接呼叫 ⇒ 其儲存行為未建模，一律把所有引數視為逃逸 |

最後一條是實測逼出來的，不是預先想到的：`l.push(q)` 不是 `OpIndexStore`，而是
`OpCall` 到 `vec.push` builtin（`lookupBuiltin` 可解析），它把 `q` 複製進容器，而容器
元素的載荷**永遠不會**被 drop 機制走訪（tagged enum 不是 owned 型別）⇒ 元素持有一份
淺別名卻不擁有。少了這條閘門，R2 會在 `q` 的結束處釋放載荷，容器元素就懸空：
實測 `tmp/esc5.no` 印出 `from vec:`（空）+ `trace/BPT trap`。加了之後同一支程式
印出 `from vec: hi`、rc=0（代價是洩漏，正是刻意選的安全一側）。

⚠️ **過度近似是安全且單調的**：多標一個逃逸只會讓 R2「拒絕釋放」，也就是退回 Phase 2
之前的行為（洩漏），不可能製造 REGRESS。反之漏標會製造懸空指標。所以這裡一律寧可過度。

### 9.4 為什麼不（現在）做 union-find

`OpMove` 別名群合併是 R2 之外的第三條路，能把「`r e-res = q` 之後傳 `r`」也修好。
但它需要決定「一群別名裡哪一個負責 drop」（要比較生命期），而 Phase 1 的
`moveDest` 排除已經保證了**不會 double free**。留給 Phase 2b。

**後記（Phase 2b 實作後）**：真正的修法不是 union-find，而是**沿用既有的生命期慣用法**
——`insertDrops` 早就為 `str`／`vec`／struct 做了「來源還活著就改寫成 `OpClone`」的判斷
（`moveStrSharesHeap` / `moveSliceSharesHeap` / `moveStructSharesHeap`）。enum 只是少了
同一條分支。詳見 §10。

### 9.5 殘留限制（安全的一側）

1. **escape sink 的洩漏**：值同時「被傳給會抽取的 callee」又「存進 struct／容器
   （含 `l.push(q)` 這類 builtin 呼叫）」時，R2 被閘門擋下 ⇒ 沒人釋放 ⇒ **洩漏**
   （不是懸空）。這是刻意的取捨。
2. **別名鏈** —— **已由 §10（Phase 2b）修好**。原本 `r e-res = q` 之後只傳 `r` 時，
   `r` 是 move 目的地不 owning、`q` 沒被傳 ⇒ 都不 owning ⇒ 洩漏。
3. **`enumEscapeSinks` 的保守性**：任何解析成 builtin 的 `OpCall`、以及
   `OpCallExtern`／`OpCallFFI`／間接呼叫，其所有引數都算逃逸。這會讓「enum 傳給
   builtin 又傳給會抽取的 callee」這種組合退回洩漏。實務上 enum 引數很少進 builtin，
   且這個方向的誤差只會洩漏不會懸空。
4. **含結構體指標欄位的 enum 不能走 §10 的深拷貝**（沒有 struct clone helper）⇒
   那一類的別名維持舊路徑：洩漏，或 analyzer 大聲報 `[use-after-move]`。
5. **match 0 次的洩漏**（Phase 3）與 `checkDropCount` 對 tagged enum 的檢查仍未做。

### 9.6 驗收（實測）

**三支二進位 A/B**（同一份原始碼，`HEAD` = `4ed84631`，`Phase 1 only` = 本輪
`markEnumParamOwners` 的 early-return 開關，`本版` = Phase 1+2）：

| 輸入 | HEAD | Phase 1 only | 本版 |
|---|---|---|---|
| `tmp/xfn-probe.no` | `in main: hi` → `trace/BPT trap` (rc=1) | `in show:`（空）→ `trace/BPT trap` (rc=1) | `in main: hi` / `in show: hi` (rc=0) |
| `tmp/xfn-test.no`（= 新測內容） | `main: one` → `trace/BPT trap` (rc=1) | `show: sho`（截斷）→ `trace/BPT trap` (rc=1) | 15 行全正確 (rc=0) |

**新測** `tests/tagged-enum-cross-fn.no`（7 組）：呼叫者 match + callee match、只 callee
match、兩個參數且其一之後再用、巢狀呼叫鏈、method receiver（struct receiver + enum 參數）、
無載荷變體（`fail`）、不消耗的 callee 之後才消耗。輸出 `rc=0`、`sha256=6dd956d9…`（前 8 位元）。
⚠️ 內容刻意避開三個既有 bug #85 形狀（見 §6.1）。

**golden sweep**（`GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default`）：

```
SAME=461  DIVERGE=3  UNSTABLE=0  REGRESS=0  IMPROVED=0  BOTH_FAIL=0  NEW=15
461 + 3 = 464 ✓（= baseline 行數）
```

- 三個 DIVERGE（`tests/default-params.no`、`tests/std-hash.no`、`tests/std-new.no`）
  三方比對皆為 `golden ≠ HEAD == NOW`（rc 與 sha256 都比）⇒ 既有偏差，非本輪造成。
- `NEW` 由 14 → 15（新增 `tests/tagged-enum-cross-fn.no`）。15 支逐支 A/B：12 支
  `HEAD == NOW`；3 支為三個 enum 測試，全部 `HEAD=1`（失敗）→ `NOW=0`（通過）。
- sweep 期間 `shasum -c` 驗證 `bin/no` 未被並行 session 重建（`bin/no: OK`）。

**其他閘門**：`go test ./mir/ ./parser/ ./checker/ ./fmt/ ./hir/` 全 ok；
`no vet src/std` = `0 error(s)`；`tests/tagged-enum.no` 落在 `SAME`（sha 與 golden 一致）。

---

## 10. Phase 2b 設計：別名 `r = q`（已實作）

### 10.1 觀測：`r e-res = q` 是**位元別名**，不是所有權轉移

tagged enum 的載荷**內嵌**在 enum 裡（`{ i64 tag, [N x i64] payload }`），所以
`r e-res = q` 降落到 `OpMove`，而 `emitMove` 對它是 `load %T, %T* src` +
`store %T, %T* dst` —— r 與 q 的 payload 槽指向**同一塊**緩衝。這跟 `str` 的
「複製 {len,cap,data}」是同一個形狀，只是深了一層。

實測（`tmp/aliasA.no`：`q e-res = ok('hi')` / `r e-res = q` / `show(r)`）：

```
### func main
    enum-new   dst=2:e-res args=[1]      ; q
    move       dst=3:e-res args=[2]      ; r  <- 位元別名
    call       dst=0 args=[3]            ; show(r)
    call       dst=0 args=[4]            ; print('done')
    drop       dst=0 args=[4]
    TERM return
```

`main` 裡**沒有任何** enum 的 `drop` ⇒ 那塊緩衝永遠不釋放 = **洩漏**（輸出仍然正確，
所以只能靠 MIR／IR 看出來）。

### 10.2 為什麼 Phase 1 的「來源擁有」只在別名是死的時候成立

Phase 1 選「**來源**擁有」（見 §9.2 下方那段註解），因為來源只有一個，而
`let it = q` 這種 scrutinee 別名可能有很多個 —— 挑來源當唯一的 drop 點最省事。

但那個模型的前提是「別名是死的」：

| 情況 | 「來源擁有」的後果 |
|---|---|
| 別名之後**不再被讀** | 正確：單一緩衝、單一 drop |
| 別名之後**被讀** | 在來源的最後使用處釋放 ⇒ 別名**懸空** |
| 讓兩邊**都**擁有 | **double free**（`checkMoves` 會直接報 `[use-after-move] ... (double-free risk)`） |

第二、三種是同一枚硬幣的兩面，所以實測 `r = q; show(q); show(r)` 在 Phase 2 是
**編譯錯誤**（rc=1），不是執行期崩潰：

```
[use-after-move] value 2 dropped after move at inst 19 (double-free risk)
```

### 10.3 規則：別名被讀 → 改寫成 `OpClone`，兩邊各自擁有

沿用 `str`／`vec`／struct 早就用的**生命期**判準（§9.4 後記），只是方向要對稱：
那些型別的所有權在**目的地**（來源被豁免），所以判準問「來源還活著嗎」；
enum 的所有權在**來源**，所以判準問「**別名還活著嗎**」。

```
for each OpMove of a tagged enum whose payload is clonable:
    dstLive = dst 在 move 之後被讀（liveOut 或同 block 內 readNonDropAfter）
    if dstLive:
        inst.Op = OpClone                    # 真正的深拷貝
        enumOwnsPayload[src] = true          # 來源擁有原緩衝
        enumOwnsPayload[dst] = true          # 別名擁有自己那份
    else:
        維持 OpMove（單一緩衝轉移），交給下面的「來源擁有」規則
```

- **為什麼只改「別名會被讀」的**：別名是死的就代表那份副本沒人要，維持原本的
  單一緩衝轉移即可 —— 這也是 `let it = q`（scrutinee）的形狀，**不能**clone，
  否則每次 match 都多一次深拷貝。
- **為什麼兩邊都標 owning 是安全的**：clone 之後兩邊是**不同的**緩衝，各自 drop
  一次，正是「一個載荷恰好一次 drop」。
- 改寫成 `OpClone` 之後，`isTransferringMove` 對它回 false ⇒ `moveSrc` 不會被設
  ⇒ 來源保留自己的 drop。`OpClone` 與 `OpMove` 的 def/use 形狀相同，所以先前算好的
  `liveOut` 仍然有效（與 §4 既有的 OpMove→OpClone 改寫同一個理由）。

### 10.4 閘門：只有 `str` / 切片載荷能走

`enumPayloadCloneable` 比 `enumPayloadFreeable` **更窄**：釋放一個欄位只需要它的位址，
拷貝它卻需要一個 cloner，而目前只有 `str`（`@str_clone`）與切片（`vecDeepClone`）有。
含**結構體指標欄位**的 enum 回 false，維持舊路徑（洩漏，或 analyzer 大聲報錯）——
寧可如此，也不要半套深拷貝造成的 double free。

### 10.5 codegen：tag-switched 深拷貝 helper

`@__nolang_enum_clone_<T>(%T* dst, %T* src)`，形狀與 `emitEnumDropHelper` 完全對稱：
先整塊 `load`/`store`（涵蓋 tag 與所有**不擁有**堆的欄位），再按 tag 分支，對每個
owned 欄位從 src 的槽拷貝到 dst 的槽（`@str_clone` / `vecDeepClone`）。

同樣的兩個坑（與 drop helper 共用）：
- 分支必須在**函式**裡 —— `emitClone` 是在 block 中間發射的，當場開 basic block 會
  讓 LLVM 驗證失敗。
- **最後一個變體**的 false 邊要直接指 `done`，否則該 block 落到 `done:` 沒有終結指令
  → `expected instruction opcode`。

實測輸出（`tmp/aliasA.no`，未優化 IR）：

```llvm
define void @__nolang_enum_clone_tenum_e_res(ptr %dst, ptr %src) {
entry:
  %ecv = load %tenum_e_res, ptr %src
  store %tenum_e_res %ecv, ptr %dst
  %ect = load i64, ptr %ectp                  ; tag
  %cc0 = icmp eq i64 %ect, 0                  ; ok
  br i1 %cc0, label %vv0, label %nv0
vv0:
  %eg6 = bitcast ptr %eg5 to %str-long*
  %eg8 = bitcast ptr %eg7 to %str-long*
  %ec0_0 = load %str-long, %str-long* %eg6
  %en0_0 = call %str-long @str_clone(%str-long %ec0_0)
  store %str-long %en0_0, %str-long* %eg8
  br label %done
nv0:
  %cc1 = icmp eq i64 %ect, 1                  ; fail（無載荷）
  br i1 %cc1, label %vv1, label %done
vv1:
  br label %done
done:
  ret void
}
```

### 10.6 附帶修好的既有不一致：`checkMoves` vs `insertDrops`

Phase 1 只把「owning enum 的 move 是別名、來源保留 drop」的豁免加在 `insertDrops`
（`moveSrc`），**沒有**加在 `checkMoves`。於是 analyzer 自己插入的那個 drop，會被
`checkMoves` 當成「move 之後又 drop」而報 `[use-after-move] ... (double-free risk)`。

觸發形狀（`tmp/case8.no`，實測在 Phase 2 二進位上是 **rc=1 編譯錯誤**）：

```
a8 e-res = ok('eight')
b8 e-res = a8        ; 別名是死的
show(a8)             ; 只有來源被消費 -> 來源 owning -> 來源被 drop
```

修法 = 把兩處的判準抽成同一個 `moveTransfersOwnership(f, inst)`
（`isTransferringMove && !isOptionPeelMove && !enumOwnsPayload[args[0]]`），
`insertDrops` 與 `checkMoves` 共用。**兩個 pass 對「所有權是否轉移」必須有同一個答案**，
否則編譯器會拒絕自己產生的正確輸出。

### 10.7 驗收（實測）

**新測** `tests/tagged-enum-alias.no`（10 組）：別名是唯一消費者、來源與別名都被消費、
三層別名鏈、兩層鏈每層都被消費、無載荷變體（有／無別名）、同一別名消費兩次、
別名是死的只有來源被消費，以及**切片載荷**（`[]i64`）的別名唯一消費者／來源與別名
都被消費——後兩組專門走 `emitEnumPayloadFieldClone` 的 `%vec` 分支（實測生成
`call %vec @__nolang_vec_clone_9_0`），與 `%str-long` 是不同的 code path。
輸出 `rc=0`、`sha256=4bff13f7…`（前 8 位元）。

**三支二進位 A/B**（`HEAD` = `4ed84631`、`Phase2` = Phase 1+2、`NOW` = Phase 1+2+2b）：

| 輸入 | HEAD | Phase 2 | NOW |
|---|---|---|---|
| `tmp/aliasA.no`（別名是唯一消費者） | rc=1 crash | rc=0 但**洩漏** | rc=0，2 alloc / 2 drop ✓ |
| `tmp/aliasB.no`（來源與別名都被消費） | rc=1 crash | rc=1 **編譯錯誤** | rc=0 ✓ |
| `tmp/aliasC.no`（三層鏈） | rc=1 crash | rc=0 但**洩漏** | rc=0，3 alloc / 3 drop ✓ |
| `tmp/case8.no`（別名是死的） | rc=1 crash | rc=1 **編譯錯誤** | rc=0 ✓ |
| `tests/tagged-enum-alias.no` | rc=1 | rc=1 編譯錯誤 | rc=0 ✓ |

洩漏用 MIR 的 drop/clone/move 計數客觀量測（`main` 內）：

| 探針 | Phase 2 | NOW |
|---|---|---|
| `aliasA` | moves=1 clones=0 drops=1 | moves=0 clones=1 drops=3 |
| `aliasC` | moves=2 clones=0 drops=1 | moves=0 clones=2 drops=4 |

Phase 2 的那 1 個 drop 是 `print('done')` 的 `str` 常數；enum 的載荷（1 次 malloc）
完全沒被釋放。NOW 的 drops = 1（常數）+ 每個擁有者各 1，與 clone 次數對得上。

**golden sweep**：

```
SAME=461  DIVERGE=3  UNSTABLE=0  REGRESS=0  IMPROVED=0  BOTH_FAIL=0  NEW=16
461 + 3 = 464 ✓（= baseline 行數）
```

- 三個 DIVERGE 仍 `golden ≠ HEAD == NOW`（rc 與 sha256 都比）。
- `tests/tagged-enum.no` 仍在 `SAME`（未動到 golden）。
- NEW 由 15 → 16（新增 `tests/tagged-enum-alias.no`）。16 支逐支三方比對：
  11 支三邊全同；`tests/markdown.no` 是**已知不確定輸出**（見 §6.2；三支二進位各自
  連跑 12 次都得到同樣兩個 hash，比例 8:4 / 9:3 / 9:3）；4 支為 enum 測試，
  `HEAD=1 → NOW=0`，其中 `tagged-enum-alias.no` 另有 `Phase2=1 → NOW=0`。

**其他閘門**：`go test ./mir/ ./parser/ ./checker/ ./fmt/ ./hir/` 全 ok；
`no vet src/std` = `0 error(s)`；`analysis.go`／`codegen.go` gofmt-clean。
