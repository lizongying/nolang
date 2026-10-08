---
sidebar_position: 5
---

# MIR 優化器 vs LLVM `opt -O2`

_編譯器自帶的優化器與 LLVM `opt` 的完整指標對比_

## 這是什麼

編譯器有**兩個**可以優化同一個程式的地方：

| | 位置 | 開關 | 作用對象 |
|---|---|---|---|
| 編譯器自帶優化器 | `src/mir/opt.go` | `-opt[=N]` / `NOLANG_MIR_OPT` | **MIR**（HIR 降級後、codegen 之前） |
| codegen 前導裁剪 | `src/mir/codegen.go` | 同上（級別 2） | 已生成 IR 中**無人引用的 runtime helper** |
| LLVM `opt` | `src/build/builder.go` | `NOLANG_OPT_LEVEL`（預設 `-O2`） | 已生成的 **LLVM IR** |

本頁回答三個問題：

1. 自帶優化器自己能做到多少？
2. 和 LLVM 的 `opt -O2` 比起來如何？
3. 有沒有什麼是 `opt -O2` **做不到**、只有編譯器自己做得了的？

## 開關

`-opt[=N]`，**預設關閉**（不改變歷史行為）：

```bash
no build main.no          # 預設：不啟用
no build -opt main.no     # = -opt=1
no build -opt=2 main.no   # 級別 2
no build -opt=0 main.no   # 顯式關閉
```

| 級別 | 含義 |
| ---- | ---- |
| `0` / `off` / `false` / `no` / `none` | 關閉（預設） |
| `1` / `on` / `true` / `yes` / `-opt` | 常數折疊、整數恆等式改寫、複製傳播、死純量消除 |
| `2` | 級別 1 + 常數分支折疊、不可達塊刪除、空塊跳轉串接、單前驅塊合併、**前導裁剪** |

同一開關也可用環境變量 `NOLANG_MIR_OPT`；命令行 flag 只是把它寫進該變量，因此兩者不會不一致。`-opt` 是全局開關，`build` / `run` / `test` 都支持。

未識別的值（例如 `-opt=bogus`）一律視為**關閉**，拼錯不會意外啟用優化。

```bash
NOLANG_MIR_OPT=2 NOLANG_MIR_OPT_STATS=1 no build -o out main.no
# [miropt] level=2 funcs=3 folded=5 (arith=2 cmp=3) ident=1 copies=2 dead=4 \
#          cfg{condbr=3 unreachable=4(5 insts) threaded=1 merged=2}
# [miropt] prelude: dropped 44 unreferenced helpers
```

## 方法

### 對比臂

| 臂 | 含義 |
| --- | --- |
| `mir` | 編譯器原樣吐出的 IR（不做 MIR 優化、不做 LLVM 優化）。**基準線** |
| `mir+mir_opt` | 開啟 MIR 優化器後吐出的 IR |
| `mir+llvm_O1` | 對 `mir` 套 `opt -O1` |
| `mir+llvm_O2` | 對 `mir` 套 `opt -O2` ← **題目要求的對比** |
| `mir+llvm_O3` | 對 `mir` 套 `opt -O3` |
| `mir_opt+llvm_O2` | 對 `mir+mir_opt` 套 `opt -O2` |
| `mir_opt+llvm_O3` | 對 `mir+mir_opt` 套 `opt -O3` |

`mir_opt+llvm_O*` 與 `mir+llvm_O*` 是**唯一能分離出新 pass 貢獻**的一對：同一後端、同一 LLVM pass 流水線，只有 MIR 優化器不同。

### 指標（完整集）

| 指標 | 定義 |
| --- | --- |
| **IR 指令數** | 以 LLVM 指令開頭的行數（SSA 定義，或無結果的 `ret`/`br`/`store`/`call`/…）。宣告、全域、型別定義、標籤、註解、空行都不計，因此這個數字追蹤的是**可執行工作量**，不是文本開銷 |
| **IR 大小** | 產生的 `.ll` 位元組數 |
| **編譯時間** | 整個 `no build` 的 wall-clock 秒數（前端 + MIR + `opt`/`llc`/`cc`）。兩個 MIR 臂之間的差就是 pass 本身的成本 |
| **執行檔大小** | 連結後可執行檔的位元組數 |
| **執行時間** | 執行 `--run N` 次的中位數 |

**這五個指標必須一起看。** 指令數和位元組數會往**相反方向**移動：`opt -O2` 積極 inline，被 inline 的函式體連同符號名、attribute group、除錯 metadata 一起搬進呼叫者，於是**指令數可能下降而文本變大**。單一指標會把這件事完全藏起來。

### 語料

`tests/**`、`test/**`、`example/**`、`bench/**` 共 **631** 個 `.no`，其中 629 個可建置（2 個失敗，見文末）。

## 結果

### 全語料（631 檔，MIR 級別 2）

> 下表量測於**最終建置**（含空塊串接修正、前導裁剪、註解規則修正、task handle 複製傳播修正）。
> 631/631 建置成功；編譯掃描 `rc-diffs=0 stderr-only-diffs=0`。

| 臂 | IR 指令數 | vs `mir` | IR 大小 | vs `mir` | 編譯 | vs `mir` | 執行檔 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `mir` | 4,787,295 | 0.00% | 188.91 MiB | 0.00% | 68.07 m | 0.00% | 38.87 MiB |
| `mir+mir_opt` | 4,051,667 | **−15.37%** | 161.91 MiB | **−14.29%** | 67.36 m | −1.04% | 35.04 MiB |
| `mir+llvm_O2` | 2,419,872 | **−49.45%** | 160.85 MiB | −14.85% | 65.51 m | −3.76% | 30.41 MiB |
| `mir_opt+llvm_O2` | 1,959,259 | **−59.07%** | 135.41 MiB | **−28.32%** | 65.28 m | −4.10% | 27.75 MiB |

**邊際值**（MIR pass 相對 LLVM 的增量）：

| 配對 | 指令數 | 大小 | 編譯時間 |
| --- | ---: | ---: | ---: |
| `mir` → `mir+mir_opt` | −15.37% | −14.29% | −1.04% |
| `mir+llvm_O2` → `mir_opt+llvm_O2` | **−19.03%** | **−15.81%** | −0.35% |

最後一列是本頁最重要的數字：**在 `opt -O2` 跑過之後，MIR 這一層仍然拿掉 19% 的指令與 16% 的文本**。這是前導裁剪的貢獻——正是 LLVM 因為 linkage 而做不到的那件事。

### 逐檔分佈

總計會被少數大檔主導，所以另外看有多少程式真的被改動：

| 優化器 | 變小 | 不變 | 變大 |
| --- | ---: | ---: | ---: |
| MIR 優化器（級別 2） | **630** | 0 | **0** |
| `mir` → `mir+llvm_O2` | 547 | 1 | 82 |

MIR 優化器在 **630/631** 個程式上縮小了 IR，**沒有任何一個變大**。`opt -O2` 則讓 82 個程式的指令數反而增加（inlining）。

### 優化器實際做了什麼（全部程式加總）

| 動作 | 次數 |
| --- | ---: |
| 走訪函式 | 8,411 |
| 常數折疊 | 23,118（算術 13,921 / 比較 9,197） |
| 整數恆等式改寫 | 1,801 |
| 複製傳播刪除的複製 | 1,948 |
| 刪除死純量指令 | 27,695 |
| 常數分支折疊 | 9,899 |
| 刪除不可達塊 | 9,931（連帶 12,610 條指令） |
| 空塊跳轉串接 | 8,732 |
| 單前驅塊合併 | 20,042 |

## 關鍵發現

### 1. 自帶優化器有效，且方向正確

−15.37% 指令數 / −14.29% 大小，630 個程式變小、0 個變大。控制流清理與前導裁剪貢獻了主要部分。

### 2. LLVM `opt -O2` 移除的量大一個數量級

−49.55% 指令數 / −15.03% 大小。這不令人意外——見下方「公平性限制」。

### 3. 局部清理在 `-O2` 之後確實被吃掉——但 MIR 層的**總**邊際值不是零

只看局部清理（常數折疊、死碼消除、控制流清理），LLVM 的 pass 流水線全都做，而且做得更徹底。這也是為什麼在加入前導裁剪之前，`mir+llvm_O2` → `mir_opt+llvm_O2` 的邊際值量到的是 **−0.06%**，實質等於零。

但**在 `-O2` 之後整體仍拿掉 19.03% 指令 / 15.81% 文本**（見上表）。差額幾乎全部來自第 4 點的前導裁剪——那不是重造輪子，而是 LLVM **因為 linkage 而不能做**的事。這正是「自帶優化器該做什麼」的答案：**不是把 LLVM 的局部 pass 再做一遍，而是做它做不了的模組級判斷。**

本頁**不把這個結論包裝成「所以自帶優化器沒用」**，也不反向包裝成「所以自帶優化器很有用」：局部清理那一半確實沒有回報，有回報的是全模組那一半。

### 4. 真正的大塊浪費不在 MIR，而在 **codegen 前導**——而且 `opt -O2` 動不了它

`emitPrelude` 把整個 runtime 寫進**每一個**模組（`str_*`、`print_*`、`eprint_*`、`vec_*`、`nolang_*`），不管程式有沒有用到。

以一行程式 `main = () { print('hi') }; main()` 為例，模組裡有 **60 個函式定義**（`.ll` 共 61,812 bytes），而**從 C 入口 `main` 可達的只有 8 個**：

```
main, _nolang_main, print_str, print_nl, str_from_const, str_free,
nolang_rc_alloc, nolang_free
```

其餘 **52 個從未被呼叫**。其中 48 個定義落在 `emitPrelude()` 的輸出區間 `[preludeStart, preludeEnd)`（`codegen.go:470-472`）內，共 36,043 bytes，佔整個 `.ll` 的 **58.3%**；而這個區間裡**只有 4 個是可達的**（`print_str`、`print_nl`、`str_from_const`、`str_free`，合計 1,388 bytes），另外 **44 個是死的**（34,655 bytes，佔該區間的 **96.1%**）：

| 家族 | 死定義數 |
| --- | ---: |
| `nolang.*`（utf8 / 時間） | 14 |
| `str_*` | 12 |
| `print_*` | 8 |
| `eprint_*` | 5 |
| `vec_*` | 2 |
| `_mir_str_*` | 2 |
| 其他（`digits`） | 1 |
| **合計** | **44** |

按指令行數算，前導區間共 1,399 行指令，其中 **1,129 行（81%）落在死定義裡**。單一最大的一塊是 `str_from_double`（4,187 bytes），其次是 `nolang.utf8_cp_put`（3,525）與 `print_double`（2,997）。

> 註一：早期版本寫「51 個從未被呼叫」，那是把**註解裡出現的名字**也算成引用的結果（`vec_free` 僅因被註解提到而存活）。切掉註解後的正確數字是 **52**。
>
> 註二：本節所有位元組數**以 bytes 計**。用文字模式讀檔會得到**字元**數（前導的註解含 `—` 與 `§`，各佔 2–3 bytes），在 61 KB 的模組上差 188 bytes。`.ll` 的實際大小以 `wc -c` 為準。

`opt -O2` 為什麼不刪？因為 **GlobalDCE 只刪 `internal` 定義**，而前導是以 **external linkage** 發出的。在該程式上實測：60 個定義中**恰好 7 個**是 `define internal`（`nolang.utf8_*`），`opt -O2` **恰好只刪掉這 7 個**，其餘 53 個全部存活；而且 `.ll` 反而**變大**（61,812 → 69,874 bytes，**+8,062**），因為 inlining 給留下的那些掛上了 metadata。

把前導標成 `internal` 不是解法：`no` 模組可以跟 C 連結，連結器可能需要的 helper 不能是 internal。唯一正確的做法是**證明「就這個模組而言」哪些 helper 無人可達**——這是全模組的問題，因此實作在 codegen（`optPrunePrelude`），在 `-opt=2` 時對**已完成的 IR 文本**做可達性裁剪。

#### 那能不能乾脆把前導**移除**？不能——但可以「有條件地移除」

兩個實驗直接回答這件事（`/tmp/optwork/spot/can_we_delete*.py`）：

1. **整段砍掉**（連同其中的 global / declare / 型別定義）→ `llc` 立刻失敗：

   ```
   error: use of undefined value '@.mir.str.1'
   ```

   因為前導區間裡不只有函式，還有**程式自己的字串字面值**——`@.mir.str.1` 就是那個 `"hi"`。此外還有 `%str-long` / `%option` / `%task` / `%nolang_hdr` 等型別、`@malloc` / `@write` / `@llvm.memcpy` 等外部宣告。**但真正的「宣告面」只有 1,151 bytes**（型別 258 ＋ declare 393 ＋ global 347 ＋ 其他 153）；區間內另外那 17,516 bytes 非程式碼內容**有 93.8% 是說明註解**——那是可以回收的，見下面「完全按需發射」一節。

2. **只把 48 個 `define` 區塊換成同名 `declare`**（資料全留）→ `llc` 通過，改由**連結器**報出程式真正需要的符號，不多不少 **4 個**：

   ```
   print_nl   print_str   str_free   str_from_const
   ```

   這與可達性分析獨立算出的那 4 個**完全一致**——兩個互不相干的方法得到同一個答案。所以前導的 48 個定義裡有 **44 個（91.7%）是死的**。

#### 完全按需發射呢？可行，機制甚至已經存在

`codegen.go:75-83` 的 `extraFuncs` / `extraFuncsBody` 就是一個按需發射器，註解寫得很清楚——「helper functions that emission **discovers** while lowering a function body」「extraFuncs is the already-emitted set, so a helper is written exactly once」。option / struct / enum / ptr 的 helper（`emitOptionDropHelper`、`emitStructCloneHelper`、`emitEnumDropHelper`、`emitPtrFieldGetHelper`…，共 8 個寫入點）全都走這條路。**前導是唯一沒有被轉換的部分**：它是 `emitPrelude` 裡四段 raw string，1,556 行、48 個 `define`。

按需發射真正的成本在別處：**65 個前導符號名散在 23 個檔案裡，單是 `codegen.go` 就有 368 行提到它們**。按需要求在每一個寫出 `@name` 的地方登記，漏一個就是連結錯誤；而且 `usedFname` 的符號預留必須在使用者函式命名**之前**完成（現在靠 `definedSymbols(prelude)` 播種），得改成一份靜態的 48 個名字清單。型別與 global 也必須先發（`%str-long` 被 `%option` 用），所以表頭永遠無條件。

但它能多拿的東西，**幾乎全是註解**：

| | 位元組 | vs 原始 | 定義數 |
| --- | ---: | ---: | ---: |
| `-opt=0` | 61,812 | — | 60 |
| 只刪程式碼（早期版本） | 27,157 | −56.1% | 16 |
| **程式碼 + 該函式自己的註解** | **14,548** | **−76.5%** | 16 |

模型產物（`/tmp/optwork/spot/ondemand_model.py`）`llc` 通過、連結通過、輸出正確的 `hi`；實際實作後量到的數字相同（差 9 bytes，來自註解邊界的判定）。**多拿的 12,609 bytes 裡沒有一條指令**——全部是 44 個被刪函式留下的孤兒註解，佔已刪程式碼 34,655 bytes 的 36.4%。最大一筆是 `nolang.utf8_width` 的 **1,700 bytes** 註解，為一個已經不存在的函式；`str_clone` 1,504、`str_retain` 1,171、`print_double` 827。

**這件事不需要重寫 emitter**：只要讓 `optPrunePrelude` 在丟掉一個 `define` 區塊時，把它上方那串註解一起丟掉（`optTrailingComment`：向後掃描，遇到第一個非註解行就停——所以註解若其實是在描述一個**型別**或 **global**，那個宣告會擋在中間，它自然不會被刪；表頭也因為以 `declare` 行結尾而受保護）。**已實作**，見下面的分解表。

於是「完全按需」在**程式碼面**已經沒有東西可拿——現行裁剪對 48 個定義的取捨與按需完全一致（都是留 4 刪 44）。剩下的只有兩件事，都已經拿到或已知大小：

1. ~~孤兒註解 12,609 bytes~~ → **已實作**。
2. `extraFuncsBody` 裡那 8 個死 helper **4,851 bytes** → 需要把裁剪區間從一個變成兩個，尚未做。

**必須說清楚的限制**：刪註解**不會縮小執行檔**。註解在 `opt`/`llc` 解析時就被丟棄，所以 `-opt=0` 與 `-opt=2` 的執行檔分別是 36,616 / 34,648 bytes，**與只刪程式碼的版本完全相同**。那 12,609 bytes 是**IR 文本**與**編譯期解析量**的收益，反映在「IR 大小」這項指標上，不是程式碼大小。

裁剪後模組的位元組分解**精確閉合**（三欄相加等於總數，加總也等於差值）：

| | 定義（程式碼） | 註解 | 型別 / declare / global | 合計 |
| --- | ---: | ---: | ---: | ---: |
| `-opt=0` | 42,589 | 17,469 | 1,754 | **61,812** |
| `-opt=2` | **7,934** | **4,860** | 1,754 | **14,548** |
| 差 | −34,655 | −12,609 | 0 | **−47,264** |

型別、`declare` 與 global 一個都沒動——它們是模組要合法所需的宣告面，1,754 bytes，與程式無關。前導的程式碼**與**註解都是**逐模組相同的固定文字**，所以這是一筆**每個模組約 47 KB 的固定稅**。

語料抽樣 40 檔（`prelude_sample.py`）顯示這筆稅與程式大小無關：

- 前導定義數**恆為 48**（少數 49–51，來自平台 shim）；裁剪後保留 **0–19 個，中位數 9**。
- `tests/dup.no`、`tests/q.no`、`tests/u128-types.no` 保留 **0 個**——整個前導的程式碼對它們**全部**是死的。
- 因此節省比例完全由模組大小決定：一行程式 −76.5%，`test/std/pbkdf2.no`（3.0 MB）只有 −4.1%。

**為什麼不把 runtime 拿出來做成預編譯庫、讓 `emitPrelude` 整個消失？** 三個具體障礙：

1. 前導是**逐模組參數化**的，不是固定 blob。`initOptionSlot()`（`codegen.go:1254`）依 `mod.OptionInlineThreshold` 算出 `optSlotBytes`，`emitPrelude` 用它寫出 `%option = type { i64, [3 x i64] }` 以及所有觸及 option slot 的 helper——每個模組的 `%option` 都可能不同。
2. 前導的**符號名會被拿去避開使用者函式重名**：`EmitLLVM` 用 `definedSymbols(prelude)` 播種 `usedFname`，使用者函式若撞名會被加後綴（`src/util.no` 的 `str-eq` → `str_eq` 就是這個機制在處理）。移掉前導等於換掉一整套命名規則。
3. 會失去「編譯器輸出單一自足 `.ll`」的性質，並需要多一份 runtime 原始碼、每目標 triple 的建置、以及一個額外的連結步驟。

收益上限就是那 34,655 bytes 的程式碼（外加 12,609 bytes 的孤兒註解，已由裁剪一併處理），而文字裁剪在**不動任何建置流程**的前提下已經拿到，且預設關閉（`-opt=0`）時輸出位元組完全相同。

實測（同一份 `print('hi')`）：

| | 定義數 | `.ll` 位元組 | 執行檔 |
| --- | ---: | ---: | ---: |
| `-opt=0` | 60 | 61,812 | 36,616 |
| `-opt=2`（只刪程式碼） | 16 | 27,157（−56.1%） | 34,648（−5.4%） |
| `-opt=2`（程式碼＋註解） | **16** | **14,548（−76.5%）** | **34,648（−5.4%）** |

第三列與第二列的執行檔大小**完全相同**：註解在 `opt`/`llc` 解析時就被丟棄，所以 12,609 bytes 是 IR 文本與編譯期解析量的收益，不是程式碼大小的收益。

裁掉的 44 個**全部都是不可達的**，區間內可達的 4 個一個都沒動——這是這一步正確性的核心不變式，由 `src/mir/prelude_prune_test.go` 的端到端測試守住：先斷言 OFF 臂的定義數不為 0（否則測試會空轉），再斷言 ON 臂嚴格更小，最後逐條檢查每個 `call` 到的符號仍然存在。

裁剪的近似一律**偏向保留**：無法解析出唯一名字的區塊保留、區塊沒有閉合大括號就從該處起停止裁剪。漏掉一個優化只是多幾個位元組；刪錯一個就是連結錯誤。唯一刻意**不**算引用的，是 LLVM 註解（`;` 之後）裡出現的名字——沒有任何東西會執行它，連結器也看不到它。這一條正是這一步能真正生效的關鍵：前導自我註解很重，不切註解時 `nolang_async_wait` 只因為被註解**點名**就活著，並連帶保住整個 async 叢集；七個 utf8 helper 也因為上方區塊註解把它們列了一遍而全數存活。切註解讓刪除量從 33 個提升到 **44** 個。

還有一個**已知未覆蓋的缺口**：`nolang_ready_grow`、`nolang_async_{enqueue,yield,wait,done,run}`、`nolang_rc_{retain,release}` 這 8 個在 `print('hi')` 上同樣不可達，但裁剪動不了它們，因為它們**不在** `[preludeStart, preludeEnd)` 內——它們是在 `extraFuncsBody` 裡緩衝、在使用者函式之後才沖出的（`codegen.go:575-577`）。這 8 個合計 **4,851 bytes**，是裁剪後模組（14,548）的 **33.3%**，也是這個方向上還剩下的全部空間（孤兒註解那 12,609 bytes 已回收）。要回收得讓 `optPrunePrelude` 再多處理一個區間（`extraFuncsBody` 沖出處，`codegen.go:575`），目前尚未做。

### 5. MIR 裡**沒有**死函式——這是一個量測出來的否定結果

「刪掉 main 到不了的函式」看起來是同一類穩賺的優化，於是實作並量測了它：在 **631 個檔案**上，`deadfuncs` 全部為 **0**。

原因是 HIR 降級本來就是**需求驅動**的（`hir2mir.go` 從根集合走訪，只降級被呼叫到的函式），所以 MIR 模組在構造上就不含不可達函式。該 pass 因此**沒有出貨**——一個在 631 檔上一次都不會觸發的 pass 只是優化器裡的死程式碼。

**結論：MIR 層沒有可回收的死函式；可回收的死碼全部在 codegen 前導。**

### 6. 開啟 inlining 之後，IR 指令數是弱指標

`opt -O1` 讓指令數 −34.45%，但**位元組數 +9.30%**；`-O3` 位元組數幾乎不動。原因是 inlining（見「指標」一節）。**指令數可能同時下降**（被呼叫者的函式體折疊成常數後死掉），**而文本變大**。

## 公平性限制（引用數字前必讀）

這是在比較兩個**成熟度差很多、且不在同一層**的優化器：

- `mir` 是編譯器自己剛從 HIR 降級出來、**未經優化**的 SSA IR；
- `opt -O2` 是成熟的 mid-level 優化器，約 200 個 pass（inlining、GVN、LICM、loop unrolling、SROA…），而且是在 codegen 已經做完自己的降級**之後**才跑。

所以這張表回答的是「**自帶 pass 有沒有抓到同一類浪費、差距有多大**」，**不是**「自帶 pass 比 LLVM 好」。

`mir` → `mir+llvm_O2` 的巨大落差是預期中的，不是新 pass 的缺陷。真正應該讀的是 `mir_opt+llvm_O*` 那一對的**邊際值**：MIR pass 移除了多少 LLVM 流水線自己沒移除的東西。

而第 4 點給出了這個問題的正確答案：**邊際價值不在「做 LLVM 也會做的事」，而在「做 LLVM 因為 linkage 而不能做的事」**。

## 正確性驗證

優化器改寫的是所有權分析（`Analyze`）即將讀取的指令流，所以正確性必須獨立驗證，不能只看數字。

| 驗證 | 範圍 | 結果 |
| --- | --- | --- |
| 單元測試 | `src/mir/opt_test.go`、`src/mir/prelude_prune_test.go` | 通過。每個端到端案例都配一個開關 OFF 的對照組 |
| 編譯掃描 | 631 個檔案，off vs on，比對 rc 與正規化 stderr | **`control-ok=629 rc-diffs=0 stderr-only-diffs=0`** |
| 死函式掃描 | 631 個檔案，統計 `deadfuncs` | **0**（見關鍵發現第 5 點） |
| 執行 A/B | 631 檔全語料，比對四臂的 (exit code, stdout sha256) | **完成** — 0 個由 MIR 造成，見下 |
| 倉庫測試 | `go test ./...` | 通過（`cmd/no` 有一個**既有**失敗，見下） |

**同一 binary 開／關**是刻意的設計：因為優化器在環境開關後面，兩側跑的是同一個可執行檔，所以不需要重建，也不會有「另一份未提交的工作混進來」的問題。

### 1. 編譯掃描真的抓到 bug（不可達塊）

第一版會刪除不可達塊，但那些塊裡有其他地方仍在引用的值的**唯一產生者**，導致 10 個檔案以 `EmitLLVM: move slot` / `value N … has no storage slot` 失敗。修正後 0 個迴歸。

### 2. 複製傳播的健全性缺口

`defs[s] == 1` 界定了來源在**全模組**的定義次數，但**參數貢獻零個定義**，所以「參數 + 之後被賦值一次」也讀成 `defs == 1`。修正：加入 `lastDefPos` 與 `redefinedLater` 閘門。

### 3. 空塊串接造成 49 個程式 double free（由**執行** oracle 抓到，不是編譯掃描）

編譯掃描完全乾淨（`rc-diffs=0 stderr-only-diffs=0`）、所有單元測試都過，但 **631 檔中有 49 個程式的輸出悄悄不同**，且**級別 1 也一樣**。

最小重現 `tests/mem-safety/option-match-basic.no`：開啟 pass 後最後一行 `end` 消失，行程以 SIGTRAP（exit 133）結束。

根因：`Analyze` 的 drop 放置問的是「**哪些邊**到達這個區塊」。串接把一個空 trampoline 摺進目標塊後，目標塊**多了一個它本來沒有的前驅**，而那個前驅已經在自己的結尾 drop 過該值——於是唯一的 start-drop 變成第二次 free。

修正：只允許在「該空塊是目標的**唯一**前驅」時串接。此時 `liveIn[bid] == liveIn[tgt]`（空塊不執行任何東西），重導只是**改標路徑**而不是新增路徑，原本落在 `bid` 開頭的 drop 改落在 `tgt` 開頭、對同一批路徑生效。

**教訓**：這一類 bug 只有**執行** oracle 看得到。編譯掃描與單元測試都是綠的。

（上文的「49 個」是**修正當時**的狀態；最終建置的全語料量測顯示 MIR 造成的差異為 **0 個**，見「執行 A/B」一節。）

### 4. 複製傳播折疊 task handle 副本，讓第二次 `awy` 讀到已歸零的 handle

同樣是執行 oracle 抓到的，同樣是**級別 1 就中**。631 檔語料的五指標量測把 33 個程式列為「各臂輸出不同」；逐一分類後發現其中只有 **2 個**是 MIR 造成的（其餘 31 個是 `-O0` 臂本身會中途崩潰，輸出被截斷，雜湊自然不穩）：

```
tests/async-rc.no            L0: 1 42 42      L1/L2: 1 42 0
tests/async-handle-alias.no  L0: 1a=42 1b=42  L1/L2: 1a=42 1b=0 / 2a=42 2b=0 2c=0
```

MIR 的實際形狀（`tests/async-rc.no` 的 `test-alias-both`）：

```
run         dst=16  args=[15]        ; h  = run worker-async(21)
move        dst=17  args=[16]        ; h2 = h
task-retain dst=0   args=[17]        ; 第二個引用
await       dst=18  args=[16]        ; a = awy h
await       dst=19  args=[17]        ; b = awy h2
```

`optCopyProp` 把 `move` 刪掉、把讀者從 17 改指到 16。但 **`OpAwait` 是破壞性讀取**——它釋放該引用並把 slot 歸零；而 R 層的紀律是「**每一份 handle 副本一個引用計數，由 `OpAwait` 釋放**」（`tier.go` 的 `case OpTaskRetain, OpAwait:`，以及 `hir2mir.go` 中「每個 handle 副本之後緊接一個 `OpTaskRetain`」）。兩個 `OpAwait` 共用一個 slot，第一個把它歸零，第二個就讀到 0。

`tests/async-rc.no` 的註解本身就寫明了這個形狀：「the two awaits shared one slot, so the second read a zeroed handle and printed 0 instead of 42」。

修正：新增 `optHandleConsumer`，把 `OpTaskRetain` / `OpAwait` 列為 handle 消費者；只要副本的**目的地或來源**被這類指令讀取，就**整條放棄傳播**（不重導、不刪除）。來源那一側是鏡像的同類漏洞：`move t = h; awy h; …用 t` 把讀者重導到 `h`，等於把 await 剛歸零的 handle 交出去。這是一份白名單而不是對「哪些指令會改寫參數」的猜測——`drop` 與所有權改寫都由 `Analyze` 在**這個 pass 之後**才插入，因此在這裡唯一的破壞性讀者就是 async 這兩個。

回歸測試三條：`TestOptCopyPropKeepsAHandleCopyAlive` 用 hir2mir 的真實形狀建 fixture，斷言傳播數為 0、`move` 仍在、handle 指令仍讀 17；`TestOptCopyPropKeepsACopyWhoseSourceIsConsumed` 覆蓋來源被消費的鏡像情形；`TestOptCopyPropStillPropagatesWithoutHandleOps` 是對照組（換成普通 `OpAdd` 讀者），斷言同一份 fixture **仍然**被優化掉，否則前兩條會因為「什麼都不做」而空轉通過。

### 執行 A/B：已完成（631 檔）

完整語料的執行 A/B 已跑完。判讀方式是把每個程式在四臂下的 stdout sha256 逐一比對，並用 harness 的 `run` 欄位區分「真的不一致」與「某一臂本身不穩定」：

| 類別 | 數量 |
| --- | ---: |
| 四臂全部決定性執行 | **625** |
| 某一臂不穩定（全部落在 `-O0` 臂） | 6 |
| 其中：**MIR 造成**（`mir+llvm_O2` ≠ `mir_opt+llvm_O2`） | **0** |
| 其中：僅 LLVM 層級差異（`mir` == `mir+mir_opt`，但與 `-O2` 兩臂不同） | 25 |

**在 `-O2` 這個層級上，MIR 優化器對 631 個程式的行為影響為零。** 剩下的 25 個是既有的 LLVM 層級差異（浮點／codegen 類：`aes*`、`base64`、`chacha20`、`dns`、`ed25519`、`fmt-spec`、`nbody-debug`、`x509`、`pem`、`url` …），與本工作無關——它們在 `mir` 與 `mir+mir_opt` 兩臂完全相同。

那 6 個不穩定的是 **`-O0` 臂自身的既有缺陷**，不是優化器造成的。證據：

- `tests/aes-enc.no` 在 `-O0` 且**優化器關閉**時就 SIGSEGV（`rc=-11`），輸出為空；同一份程式在 `-O2` 下正常。
- `tests/fmt-helpers.no` 在 `-O0` 且優化器關閉時第 10 行印出垃圾值 `-7772297905469128602`；開啟優化器印出正確的 `102`，`-O2` 下兩者都是 `102`。也就是說這裡是**優化器意外避開了 `-O0` 的誤編譯**，方向與「優化器改壞程式」相反。

`-O0` 是一條既有的脆弱路徑；本頁因此把 `-O0` 兩臂僅當作 IR 基準，執行期結論一律以 `-O2` 兩臂為準。

### 已知的既有失敗（與本工作無關）

- `tests/async-cancel.no`、`tests/async-coop.no`、`tests/https-server.no`、`src/checker/*` 在本次工作開始前就已被另一份未提交的工作修改。
- `go test ./...` 中 `cmd/no` 的 `TestFixRedundantTypeInFile/hex_literal_slice_preserved` 失敗。已用「把我的改動還原後重跑」驗證：**同樣失敗**，因此是既有的，不是本工作造成的。
- `test/std/math.no`、`test/std/regexp.no` 在五指標 harness 中需要模組限定呼叫名而無法建置（631/631 的編譯掃描則涵蓋它們，且 0 差異）。

## 尚未覆蓋的部分

- **前導裁剪只作用於 `[preludeStart, preludeEnd)`**。`extraFuncsBody` 裡的 10 個 rc/async helper（`nolang_ready_grow`、`nolang_async_*` ×5、`nolang_rc_*` ×3、`nolang_free`）不在區間內，其中 8 個在 `print('hi')` 上同樣不可達卻回收不掉。要回收需讓 `optPrunePrelude` 多處理一個區間，尚未實作，**規模未量化**。
- **`-O0` 路徑本身有既有的誤編譯**（見「執行 A/B」一節）。本頁的執行期結論全部以 `-O2` 兩臂為準；修 `-O0` 不在本工作範圍內。
- **`opt -O1` / `-O3` 兩臂**只在 `--extended` 下量測，未納入本頁的主表。

## 重現方式

```bash
# 1) 建置編譯器
cd src && go build -o ../bin/no ./cmd/no

# 2) 完整指標對比（預設走 tests/ test/ example/ bench/，MIR 級別 2）
python3 scripts/miropt_vs_llvm.py

# 只跑一部分、換級別、加 -O1/-O3 臂、或留一份機器可讀的結果
python3 scripts/miropt_vs_llvm.py --roots tests --level 1 --extended --json /tmp/miropt.json

# 3) 行為 A/B（同一 binary，開／關執行，比對 exit code 與 stdout）
python3 scripts/miropt_exec_ab.py --no ./bin/no --level 2
```

`miropt_vs_llvm.py` 輸出：各臂五指標總表、**邊際值**（MIR pass 相對 LLVM 的增量）、逐檔分佈、優化器動作統計。產生的 `.ll` 會保留在 workdir 供人工檢視。

`miropt_exec_ab.py` 輸出：`same` / `diff` / `skipped` 計數、false-pass 防呆、以及每一筆行為差異的詳細內容。

兩者都需要 `no` 與 LLVM `opt` 在 `PATH` 上（或用 `--no` / `--opt` 指定路徑）。
