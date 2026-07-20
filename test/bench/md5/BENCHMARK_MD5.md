# MD5 基準測試

測量各語言實現 MD5 在 100,000 次迭代中的效能。

## 測試環境

| 項目 | 值 |
|------|-----|
| CPU | Apple M1 (ARM64, 3.2GHz) |
| RAM | 16 GB |
| OS | macOS |
| C 編譯器 | clang (LLVM 21.1.5) |
| Rust 編譯器 | rustc (`cargo build --release`) |
| Go 編譯器 | go build (`-ldflags="-s -w"`) |
| Nolang | `.no` → LLVM IR → opt -O2 → clang |

## 測試結構

三個版本均使用 `[]byte` 類型輸入，計算 `MD5(input)` 100,000 次，每次修改 `input[2]` 以防止死代碼消除，輸出 `result[0]`。

```
input = []byte("abc")       // 或等價的 []byte 初始化
for i <- 0..100_000:
    input[2] = i & 255
    result = md5(input)
print(result[0])
```

## 目錄結構

```
test/bench/md5/
├── BENCHMARK_MD5.md
├── bench_ms.py                 # 毫秒精度基準測試腳本
├── go/
│   └── md5-bench.go            # Go 標準庫 crypto/md5
├── nolang/
│   └── md5-bench.no            # Nolang std/hash/md5
└── rust/
    ├── Cargo.toml
    └── src/main.rs             # md-5 crate
```

## 編譯與執行

```bash
# Go
cd test/bench/md5/go
go build -ldflags="-s -w" -o md5-bench_go .

# Rust
cd test/bench/md5/rust
cargo build --release

# Nolang
no build test/bench/md5/nolang/md5-bench.no

# 執行毫秒精度對比
python3 test/bench/md5/bench_ms.py
```

## 測試結果

### 可執行檔體積

| 語言 | 編譯器 | 大小 | 相對最小 |
|------|--------|------|----------|
| **Rust** | cargo --release | **464K** | 1.0× |
| **Nolang** | clang -O2 | **705K** | 1.5× |
| **Go** | go build -s -w | **1.7M** | 3.7× |

### 執行效能

`MD5(input, 修改 input[2]) × 100,000` 次，10 次取截尾平均值（毫秒精度）：

| 語言 | 平均耗時 | 最佳 | 最差 | 相對最快 |
|------|----------|------|------|----------|
| **Rust** (md-5 crate) | **16.3ms** | 15.9ms | 19.6ms | 1.0× |
| **Nolang** (std/hash/md5) | **28.0ms** | 27.4ms | 28.5ms | 1.7× |
| **Go** (crypto/md5) | **66.3ms** | 61.8ms | 82.7ms | 4.1× |

### 指令數對比

```
Rust    :   81M  ████
Nolang  :  194M  █████████
Go      :  554M  ████████████████████████
```

### 峰值記憶體對比

```
Nolang  :  0.9MB  █
Rust    :  1.0MB  █
Go      :  2.3MB  ██
```

### 詳細數據

| 語言 | 耗時(ms) | 指令數 | 峰值 RSS | 二進制大小 |
|------|----------|--------|----------|------------|
| **Rust** | 16.3 | 81M | 1.0MB | 464K |
| **Nolang** | 28.0 | 194M | 0.9MB | 705K |
| **Go** | 66.3 | 554M | 2.3MB | 1.7M |

## 分析

### 效能

- **Rust** 最快（16.3ms），md-5 crate 使用了 SIMD 優化和零拷貝設計
- **Nolang** 次之（28.0ms），為 Rust 的 1.7×，比 Go 快 2.4×
- **Go** 最慢（66.3ms），crypto/md5 標準庫在此場景下效能較低

### Nolang MD5 優化

通過 `no build -v` 輸出 LLVM IR 進行分析，實施了以下優化：

1. **內聯 rot-left**（消除 64 次函數呼叫/區塊）
   - 將 `r = rot-left(t, 7)` 替換為 `a = (b + (((t << 7) | (t >> 25)) & mask)) & mask`
   - IR：rot-left 呼叫從 64 降至 0（md5 函數內）

2. **位移替代 8 路 if-else**（消除 128 個分支標籤）
   - 將 8 路 `len-byte-idx == N -> b = (blen >> (N*8)) & 255` 替換為 `b = (blen >> (lbidx * 8)) & 255`
   - IR：分支標籤從 427 降至 203（-53%）

3. **保留 w0-w15 獨立變數**（避免 malloc 開銷）
   - 測試 `w [16]i64` 陣列方案：分支降至 91 但記憶體暴增至 13.2MB（每迭代 malloc）
   - 回退為獨立變數：記憶體僅 0.9MB，且指令數更少（194M vs 202M）

優化前後 IR 對比：

| 指標 | 優化前 | 優化後 | 變化 |
|------|--------|--------|------|
| IR 行數 | 3,441 | 2,668 | -22% |
| 分支標籤 | 427 | 203 | -53% |
| allocas | 238 | 45 | -81% |
| rot-left 呼叫 | 64 | 0 | -100% |
| 指令數 | 203M | 194M | -4.4% |
| 峰值記憶體 | 1.5MB | 0.9MB | -40% |

### 記憶體

- **Nolang** 記憶體使用最低（0.9MB），優化後較前版（1.5MB）進一步降低
- **Rust** 次之（1.0MB），md-5 crate 零分配設計
- **Go** 最高（2.3MB），含 Go runtime 基礎開銷（GC、scheduler 等）

### 體積

- **Rust** 可執行檔最小（464K），但包含 md-5 crate 依賴
- **Nolang** 中等（705K），包含 std/hash/md5 靜態連結
- **Go** 最大（1.7M），包含 Go runtime（GC、scheduler 等）

## 修復的 Bug

在完成此對比的過程中，修復了以下編譯器 bug：

1. **字串索引賦值在循環中導致 bus error/segfault**
   - 根因：字串字面量的 data 指標指向唯讀全域常量 `@.str.N`
   - 修復：將字串字面量數據複製到 heap 上可寫記憶體（malloc + memcpy）

2. **超過約 500K 次迭代時棧溢出 segfault**
   - 根因：循環體內 `alloca` 臨時輸出空間不釋放，棧線性增長
   - 修復：使用 `@llvm.stacksave` / `@llvm.stackrestore` 包裹循環內的 alloca

3. **`len()` 內建函數對全局 `%vec` 變數生成錯誤引用**
   - 根因：`len()` 硬編碼 `%%%s` 生成局部變數引用，未使用 `varAddr()` 區分全局/局部
   - 修復：改用 `g.varAddr(ident.Value)` 正確引用全局變數

4. **`[]byte` 全局變數初始化被跳過**
   - 根因：`%vec` 類型的全局變數在 `generateMainFunction` 中被跳過，初始化代碼未生成
   - 修復：將 `%vec` 加入需要 runtime 初始化的類型列表

5. **`generateLet` 將全局變數錯誤標記為局部變數**
   - 根因：`generateLet` 中 `g.funcLocalNames[name] = true` 覆蓋了全局變數標記
   - 修復：僅對非全局變數設置 `funcLocalNames`

6. **統一 `.len` 屬性存取**
   - 變更：vec/arr/str 統一使用 `.len` 屬性獲取長度，md5 改用 `data.len`
   - 標準庫新增 `str.len`、`[n]t.len` 方法定義（與 `[]t.len` 一致）
