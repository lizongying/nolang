---
name: nolang-selfhost-perf
description: Nolang 自举编译器（`compiler/src/*.no`）的性能约定与实测基线。用于编写或修改自举 lexer / parser 代码、诊断 O(N²) 退化、评估字符串／容器遍历开销时参考。涵盖「先 str.to-bytes() 再遍历」约定、`vec.push` 摊还 O(1) 不变量、结构体字段借用的整块拷贝陷阱，以及 print 插值／timeout 等判读陷阱。
---

# Nolang 自举编译器性能约定

本文是 `compiler/`（自举编译器）的性能判据来源，全部结论均有可重跑探针与实测数值。
设计背景见 [`docs/design/nolang-selfhost.md`](../../../docs/design/nolang-selfhost.md) §2.1.2。

## 1. 核心约定：**先 `str.to-bytes()`，再走 `[]byte`**

需要对字串做**位元組層面遍歷**時，一律在入口處轉一次：

```nolang
; ✅ 推薦：轉一次，之後 b[i] 索引 + b.len() 取長度（皆 O(1)）
f = (s []byte) (r str) {
    n = s.len()
    i <- [0..n): { ... b[i] ... }
}

; 呼叫端：str.to-bytes()
r = f(t.lit.to-bytes())
```

相較於直接對 `str` 逐位元組索引：

```nolang
; ⚠️ 對【區域變數／參數】是 O(1)，可用；但一旦接收者是【結構體字段】就是 O(N)
f = (s str) (r str) {
    n = s.len-bytes()
    i <- [0..n): { ... s.byte(i) ... }
}
```

**三條配套事實**

1. `str.byte(i)` **本身**由 `emitBuiltinRawByteAt`（`src/mir/codegen.go:15115`）展開為
   `GEP + load + zext`，是純 O(1)——對區域變數／參數接收者沒有問題。
2. 🔴 **讀取 `str` 結構體字段是一次「借用」⇒ 整塊深拷貝 ⇒ O(N)。**
   放進逐位元組的掃描迴圈 ⇒ 每次迭代拷一份 ⇒ 總量 **O(N²)**。
   （把長度快取到局部變數**救不了**：實測 0.836s vs 0.852s，仍在二次。）
3. `str.len()` 回的是**碼點數**，不是位元組數（`src/std/str.no:41-44`，
   實作為 `n = .cp-index(.len-bytes())`）；`[]byte.len()` 才是位元組數。

⚠️ **方法真名是 `str.to-bytes()`（`src/std/str.no:464`），沒有 `str.bytes()`。**
需求討論時常被口述為 `bytes()`／`str.bytes()`，照抄會直接 compile error；
寫碼前以 `src/std/str.no` 的實際定義為準。

🔴 **硬約束：`str.to-bytes()` 【本身】是一次 O(N) 拷貝，不是零成本視圖。**
（`src/std/str.no:464`：`val = with-len(.len-bytes())` 再逐位元組複製。）
故只能在**入口／邊界**呼叫一次；放進逐位元組迴圈或每次 push 都轉，
會把 §2 的攤還 O(1) 重新打回 **O(N²)**。

**好處**

- 省去「單獨快取長度」的欄位（如 `lxst.n`）—— 少一份需與實際緩衝同步的狀態，
  長度快取一旦漂移就是靜默錯；
- 不用 `len-bytes()`；
- 轉出來的 `[]byte` 可直接用於後續遍歷。

**看似反直覺、其實正確**：`sb.push(s str)` 內部用 `s.byte(i)` 逐位元組追加是對的
（`s` 是參數 ⇒ O(1)，且位元組終究要拷進 `.buf`）。改成 `sb.push(s []byte)` 並要求
呼叫端先 `to-bytes()`，只是多加一次 O(N) 拷貝與一次配置，毫無好處。

### 實測（多 token 語料，`tokens` 命令，min-of-3）

| 版本 | 45K | 91K | 236K | 性質 |
|------|-----|-----|------|------|
| HEAD（`lxst.input` 為 `str`） | 0.112s | 0.418s | **2.577s** | **O(N²)** |
| 優化版（`lxst.input` 為 `[]byte`） | 0.016s | 0.025s | **0.053s** | **O(N)** |

單 token（200K 識別符）：1.695s → 0.015s（**113×**）。
`ast`（yaml.no 81KB）：0.386s → 0.042s（**9.2×**）。

---

## 2. 第二條不變量：`vec.push` 必須攤還 O(1)

`emitBuiltinVecPush`（`src/mir/builtin_call.go`）**只能在 `len == cap` 時重分配**：

- 滿（`len == cap`）→ `alloc(cap*2 或 1)` + `memcpy(len*stride)` + `release(舊)`（攤還 O(1)）
- 未滿（`len < cap`）→ **原地寫入，零分配、零拷貝**

早期實作把 `alloc + memcpy + release` 寫成**直線碼**（無 `if needGrow` 守衛），
即便 `len < cap` 也每次重分配並拷貝 `len` 位元組 ⇒ Σlen = N²/2 ⇒ **O(N²)**。
因為所有經 `sb` 構建的字串都走 `push`，這讓自舉 lexer 整體退化成 O(N²)。

實證：預分配 `with-cap(N)` 的緩衝上做 200K 次 push（永不需擴容）
⇒ 修前 **1.020s**、修後 **0.071s**（**14.4×**）⇒ 快路徑確實被走到。

---

## 3. 容器寫入語義：**一律原地寫**

> 使用者指示（2026-10-21）：「對 arr / vec / str 的元素操作應該原地寫，包括字段是以上類型。」

容器元素（含**結構體字段**裡的容器）的寫入**一律原地**，
不再為「借用別名」做整塊拷貝（COW）。

安全性：未滿時的原地寫發生在**已分配容量內**（下標 `len < cap`），屬界內寫入；
別名各方各自的 `{len,cap,data}` 描述符是**按值拷貝**的，
故被別名容器的邏輯內容（下標 `0..len-1`）不受影響。
引用計數仍平衡：借用的 retain 由借用值死亡時的 `OpRelease` 歸還，
與 push 是否重分配無關（見 `insertBorrowRetains` / `borrowReleaseValues`）。

---

## 4. 判讀陷阱（每一條都讓數據變空洞）

| 陷阱 | 後果 | 正確做法 |
|------|------|---------|
| 🔴 **`print('x={v}')` 插值只對 `str` 有效，`i64`/`bool` 恆印空** | 既有 `tests/mem-safety/*.no` 多以 bool 輸出當斷言 ⇒ **輸出全空、零判別力**；只看「沒崩」會得出假的安全感 | 用多參數形式 `print('x: ', n)`；並加一條**自檢**（如印 4242）證明探針有判別力 |
| 🔴 **macOS 沒有 `timeout`**（亦無 `gtimeout`） | 套件比對若包 `timeout 60`，108 次運行全得 `EXIT=127`，整輪廢數據且**看似「測試失敗」** | 改後台執行，或用 `perl` 自製鬧鐘 |
| ⚠️ **`no build` 的 `-o` 必須寫在檔案參數之前** | 寫在後面會被當位置參數忽略，產物靜默落到 `dist/<源檔名>`；A/B 比對時兩版產物會混成同一個檔 | `no build -o /tmp/out main.no` |
| ⚠️ **量測原語成本時，循環不變式會被 LICM 外提** | 在迴圈體裡呼叫 `s.len-bytes()` 測出來「平坦」，是外提的假象，不是真的 O(1) | 把呼叫放進 helper，或讓引數隨迭代變化 |
| 🔴 **別用 `no test compiler` / `no test compiler/tests` 跑自舉套件** | 它**遞迴掃進 `fixtures/`（68 個輸入素材）**並把素材當測試跑 ⇒ 8 條無關 FAIL 噪音，且會在某素材上**卡死逾 8 分鐘**（曾 EXIT=137 被殺） | 逐個跑：`no run compiler/tests/<ast-test\|lx-test\|ps-test\|tok-test\|util-test>.no`（測試檔頭記載的入口），全程 ~1m10s |

### 其他既有鐵律

- 驗證用 `<repo>/bin/no`；PATH 的 `no` 常陳舊，測開關前**必須先重編**。
- ⚠️ **禁止 `cd src && go build`** —— 會缺 `StdSigKey` 注入而**靜默失效**（二進位能跑但簽名表是空的）。一律走 `make`（`make bin/no` 或 `make test`）。
- 跑測試一律 `make test`（注入 `checker.StdSigKey`）；`make test` **不含** `./cmd/no/...`，補跑時務必加 `-count=1`。
- Bash `grep` 對本倉庫部分檔案會返回**假空**。凡「符號是否還在」的否定結論，一律改用 Grep 工具複核，並同行加一個已知存在的肯定錨點。
- 禁止任何 `git checkout` / `reset` / `restore` / `stash` / `clean`（工作區常被並發 session 編輯）。比對／實驗改用工位 `git worktree add` 或唯讀的 `git show <rev>:<path>`。
