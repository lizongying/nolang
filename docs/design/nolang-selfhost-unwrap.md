# ?T unwrap lowering 自舉收斂 track（S7 正交 track ①）

> 狀態：2026-10-20 立項（scoping 完成，規格已提取）。實作待 `bin/no build compiler` 解除 toolchain 阻塞後進行。
> 歸屬：S7 match 表示分歧收斂的**正交殘餘桶**（非 match 表示本身）。

## 1. 問題陳述

L2 全語料（653 檔）殘餘 DIFF 268 檔中，約 88 檔源碼含 `?`（option/result 型別）。抽樣 `tests/match-raw-cond.no` 實證：其 `?T` unwrap 分歧**不是** match 表示問題，而是 **`?=` unwrap-assign 算子**在自舉解析器（`compiler/src/ps.no`）中**完全未被識別**。

noc（SurfaceMatch 開、s66d 狀態）把 `x ?= a` 解析成兩條垃圾 `ExpressionStatement{x}` / `ExpressionStatement{a}`，而 oracle（`bin/no ast`）將 `?=` 在**解析期 desugar** 成 `__unwrap_N` let + `== nil`/`== err` 的 IfExpression 鏈 + 合成 `it` 綁定 + `propagation` 傳播。

## 2. 實證錨定

### 最小 repro（`/tmp/min-unwrap.no`）
```
unwrap-test = (a ?i64) (r ?i64) {
    x ?= a
    r = x
}
```
`x ?= a` 在第 **3** 行。

### oracle（`bin/no ast`，第 3 行 ⇒ `__unwrap_3`）
```
Program{}
    FunctionDefinition{name: unwrap-test}
        Parameter{name: a, type: ?i64}   ; NullableType{NamedType{i64}}
        Parameter{name: r, type: ?i64}
        BlockStatement{}
            BlockStatement{}
                LetStatement{name: __unwrap_3, type: ?i64, value: a}
                    NullableType{NamedType{name: i64}}
                    Identifier{name: a}
                ExpressionStatement{IfExpression}
                    IfExpression
                        InfixExpression{op: ==}      ; __unwrap_3 == nil
                            Identifier{name: __unwrap_3}
                            Identifier{name: nil}
                        BlockStatement{}              ; nil 臂
                            LetStatement{name: it, type: nil, value: __unwrap_3, synthetic: true, armBinding: true}
                                NamedType{name: nil}
                                Identifier{name: __unwrap_3}
                            LetStatement{name: r, value: __unwrap_3, propagation: true}
                                Identifier{name: __unwrap_3}
                            ReturnStatement{}
                        BlockStatement{}              ; else：err 檢查
                            ExpressionStatement{IfExpression}
                                IfExpression
                                    InfixExpression{op: ==}   ; __unwrap_3 == err
                                        Identifier{name: __unwrap_3}
                                        Identifier{name: err}
                                    BlockStatement{}          ; err 臂
                                        LetStatement{name: it, type: err, value: __unwrap_3, synthetic: true, armBinding: true}
                                            NamedType{name: err}
                                            Identifier{name: __unwrap_3}
                                        LetStatement{name: r, value: __unwrap_3, propagation: true}
                                            Identifier{name: __unwrap_3}
                                        ReturnStatement{}
                                    BlockStatement{}          ; else：ok 臂
                                        LetStatement{name: it, type: i64, value: __unwrap_3, synthetic: true, armBinding: true}
                                            NamedType{name: i64}
                                            Identifier{name: __unwrap_3}
                                        LetStatement{name: x, type: i64, value: it}
                                            NamedType{name: i64}
                                            Identifier{name: it}
            LetStatement{name: r, value: x}
                Identifier{name: x}
```

### 語料範圍
`grep -rEo '\?=' tests src/std` ⇒ **23 檔 / 57 處**。樣本：`tests/dist/{sha256,hmac,http-rest,sse,json-*,pipeline-value,net-http,guard,...}` 等。

## 3. ⚠️ 關鍵去風險發現（決定 track 可行性）

`__unwrap_N` 的 `N` **不是全域計數器**，而是 **`tok.Line`**（`src/parser/lowering.go:4676`：`fmt.Sprintf("__unwrap_%d", tok.Line)`）。

- `min-unwrap.no` `x ?= a` 第 3 行 → `__unwrap_3` ✓
- `match-raw-cond.no` `x ?= a` 第 43 行 → `__unwrap_43` ✓；`y ?= b` 第 44 行 → `__unwrap_44` ✓

⇒ 編號完全由源碼行號決定，**可逐位元組對齊**，L2 收斂對 `?=` 檔可達 SAME。

## 4. 精確 desugar 規格（`lowerUnwrapAssign`，lowering.go:4603-4973）

`?=` 在解析期被降級，**不走 SurfaceMatch 形**（oracle `no ast` 輸出 IfExpression 鏈，非 SurfaceMatch）。故 ps.no 實作須**直接產 IfExpression 鏈**，可複用現有 `ps-parse-match` 的 `PS-SURFACE-MATCH=0` 降糖路徑（建 SurfaceMatch 結構後走 if/else 降級）。

### 結構（通用骨架）
```
__unwrap_L = <RHS>                        ; L = ?= 記號行號；type 視 RHS 推斷（見 §4.3）
[<guards>]                                ; option 運算元守衛（僅 RHS 含 option 運算元時）
[<preStmt>]                               ; 僅 target==result 參數：result = __unwrap_L（提前）
__unwrap_L: {                             ; 內部 match，降級為 IfExpression 鏈
    nil -> { it = __unwrap_L (type:nil, synthetic, armBinding); [result = __unwrap_L (propagation);] return }
    err -> { it = __unwrap_L (type:err,  synthetic, armBinding); [result = __unwrap_L (propagation);] return }
    ->    { it = __unwrap_L (type:i64,  synthetic, armBinding); <target> = it }   ; ok 臂
}
```

### 4.1 三種 target 分支（`lowerUnwrapAssign` 4817-4890）
| 條件 | nil/err 臂 | ok 臂 |
| --- | --- | --- |
| `target == result` 參數（`r ?= a`） | 僅 `return`（SkipItBinding）；`result = __unwrap_L` 提前至 match 前 | `<target> = it` |
| `target != result` 參數、有 result（`x ?= a`，r 是 result） | `result = __unwrap_L`（propagation:true）+ `return` | `x = it`（type:i64） |
| 無 option result 參數 | `result = __unwrap_L`（propagation）+ `return`；並 `saveError("?= can only be used inside a function with an option-typed result param")` | `x = it` |

### 4.2 合成 `it` 綁定型別（ok 臂）
- nil 臂：`it` type=`nil`
- err 臂：`it` type=`err`
- ok 臂：`it` type=`i64`（overflow 預設 option 內部資料恆 i64；窄型別已 zext/符號擴展）

### 4.3 `__unwrap_L` 的 type 推斷（lowering.go:4698-4781）
- RHS = `InfixExpression`（如 `a ?= b - c`）：`?i64`（NullableType{NamedType{i64}}）；elemType=`i64`
- RHS = `CallExpression`（如 `a ?= f(n)`）：從回傳型別推 `?T`，elemType=去 `?` 後的元素型別
- RHS = `IndexExpression`（安全索引 `v[i]`）：`?elem`，elemType=元素型別
- 後備：RHS 是直接識別符（如 `inner ?= res`）→ 從 RHS 變數的 option 型別推 `?elem`
- LHS 目標區域變數須標註 elemType（避免 codegen 被全域 VarTypes 同名污染 → 型別錯配/崩潰）

### 4.4 額外分支（後續 slice，非最小形）
- **寫入側 `a[i] ?= v`**（`lowerUnwrapAssignToIndex`，4653-4657）：目標為安全索引基底時接管，建安全索引 match。
- **option 運算元守衛**（`guardOptionOperands`，4665-4673）：RHS 含 option 運算元（`a ?= b + c + d`）時，為每個 option 運算元綁定臨時變數並注入 `nil/err -> result = __opt_N; return` 守衛。
- **明確非 option RHS 報錯**（4667-4671）：字面量/已知非 option 區域變數 ⇒ `saveError("... is not an option — use = instead")`。

## 5. 實作切片計畫（由小到大，每片可獨立驗證）

- **Slice 1（覆蓋主體）**：識別 `?=` 算子 + 簡單本地 target（`x ?= a`，有 result 參數、target≠result）。產 `__unwrap_L` let（type 由 RHS 推，先支援 Identifier / CallExpression / InfixExpression 三種 RHS）+ IfExpression 鏈（nil/err/ok 三臂，ok 臂 `x = it`、nil/err 臂 `result = __unwrap_L` propagation + return）。**預期覆蓋絕大部分 57 處。**
- **Slice 2**：`target == result` 參數（`r ?= a`）→ `result = __unwrap_L` 提前 + nil/err 臂僅 `return`。
- **Slice 3**：RHS 型別推斷補全（IndexExpression 安全索引、`?i64` 後備推斷、LHS 區域變數型別標註）。
- **Slice 4**：寫入側 `a[i] ?= v`（`lowerUnwrapAssignToIndex`）。
- **Slice 5**：option 運算元守衛 + 明確非 option RHS 報錯。
- **Slice 6（正交）**：`?` 字尾 unwrap 算子（若語料存在，需另立最小 repro 確認 lowering 形態）。

## 6. 測試策略

- **單元**：`compiler/tests/ps-test.no` 擴充 `?=` 形態（參照 match/arr 測試寫法：來源字串存變數再傳 `parse-dump`，避免 `{`/`}` 插值吞源；非 `?T` 主體先用非 nullable 投影驗槽位健全性，逐位元組對拍取自 oracle 實測）。
- **全量 L2**：`scripts/selfhost_diff.sh --levels L2 --corpus "tests src/std"`，SUT=`/tmp/noc-s0`（重建含本 track）、CTL=`bin/no ast`；預期 `?=` 檔由 DIFF→SAME，0 回歸。
- **反空洞**：每片實作後還原該片邏輯確認測試由「通」變「掛/錯」再恢復（對稱於 s58/s59 回歸測試手法）。

## 7. 待辦 / 阻塞

1. **阻塞**：`bin/no build compiler` 報 `EmitLLVM: setfield field`（`compiler/main.no`；`bin/no` 為 `v0.4.10-37-g9a984668-dirty` vs `compiler/package.jsonc` 要求 `"dev"`，版本漂移）。ps.no 改動**無法本地驗證**，須先解除（走隔離 worktree 或待並發 session `src/` 改動合入後重建 `bin/no`）。
2. Slice 1 實作須在阻塞解除後進行；本文件為實作依據。
3. `?` 字尾 unwrap 算子是否獨立存在於語料 ⇒ 立項前先 grep 確認。
