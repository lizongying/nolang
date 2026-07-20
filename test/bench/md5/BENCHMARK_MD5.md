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
| Nolang | `.no` → LLVM IR → opt -O3 → clang |

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
| **Nolang** | clang -O3 | **705K** | 1.5× |
| **Go** | go build -s -w | **1.7M** | 3.7× |

### 執行效能

`MD5(input, 修改 input[2]) × 100,000` 次，10 次取截尾平均值（毫秒精度）：

| 語言 | 平均耗時 | 最佳 | 最差 | 相對最快 |
|------|----------|------|------|----------|
| **Rust** (md-5 crate) | **16.2ms** | 15.8ms | 16.6ms | 1.0× |
| **Nolang** (std/hash/md5) | **27.3ms** | 26.2ms | 29.4ms | 1.7× |
| **Go** (crypto/md5) | **65.7ms** | 62.2ms | 89.1ms | 4.1× |

### 指令數對比

```
Rust    :   81M  ████
Nolang  :  197M  ██████████
Go      :  554M  ████████████████████████
```

### 峰值記憶體對比

```
Rust    :  1.0MB  █
Go      :  2.5MB  ██
Nolang  :  7.0MB  ███████
```

### 詳細數據

| 語言 | 耗時(ms) | 指令數 | 周期數 | 峰值 RSS | 二進制大小 |
|------|----------|--------|--------|----------|------------|
| **Rust** | 16.2 | 81M | 35M | 1.0MB | 464K |
| **Nolang** | 27.3 | 197M | 71M | 7.0MB | 705K |
| **Go** | 65.7 | 554M | 186M | 2.5MB | 1.7M |

## 分析

### 效能

- **Rust** 最快（16.2ms），md-5 crate 使用原生 `u32` 類型和 `u32::from_le_bytes` 零開銷載入
- **Nolang** 次之（27.3ms），為 Rust 的 1.7×，比 Go 快 2.4×
- **Go** 最慢（65.7ms），crypto/md5 標準庫在此場景下效能較低

### Nolang vs Rust 差距分析

通過 `no build -v` 輸出 LLVM IR 並與 Rust md-5 crate 源碼對比，定位了以下差距：

| 差距 | Rust 做法 | Nolang 做法 | 影響 |
|------|----------|------------|------|
| **整數寬度** | `u32` 原生 32 位，`wrapping_add` 自動溢出 | `i64`，每次運算需 `& mask` | 64 輪 × 1 mask = 6.4M 指令 |
| **字載入** | `u32::from_le_bytes` 1 條指令載入 4 字節 | 逐字節載入 + 移位 + 或 | ~12 條指令/word × 16 = 19.2M |
| **邊界檢查** | 無（直接記憶體存取） | `@nolang.bounds_check` 每次存取 | 128 次/區塊 |
| **旋轉** | `rotate_left` CPU 原生指令 | `(t<<n)|(t>>m)` 2-3 條指令 | 64 輮 × 2 = 12.8M |
| **填充處理** | 預建填充緩衝區，壓縮函數無分支 | 緩衝區填充 + 無分支字載入 | 已優化 |

**根本原因**：Nolang 僅有 `i64` 類型，MD5 是 32 位算法，每次加法/旋轉都需額外的 `& mask` 指令來模擬 32 位溢出。這是語言層面的限制，需未來支援 `u32` 類型才能根本解決。

### Nolang MD5 優化歷程

通過 `no build -v` 輸出 LLVM IR 進行分析，實施了以下優化：

#### 優化 1：內聯 rot-left（消除 64 次函數呼叫/區塊）
- 將 `r = rot-left(t, 7)` 替換為 `a = (b + ((t << 7) | (t >> 25))) & mask`
- IR：rot-left 呼叫從 64 降至 0（md5 函數內）

#### 優化 2：位移替代 8 路 if-else（消除 128 個分支）
- 將 8 路 `len-byte-idx == N -> b = (blen >> (N*8)) & 255` 替換為 `b = (blen >> (lbidx * 8)) & 255`
- IR：分支標籤從 427 降至 203

#### 優化 3：預建填充緩衝區（消除字載入全部分支）
- 使用 `[64]byte` 緩衝區：先填充 data + 0x80 + zeros + length
- 字載入變為無分支的 `buf[0] | (buf[1] << 8) | ...`
- 消除了 16 路 if-else 字分派（wi == 0 → w0 = word, ...）
- 周期數從 80M 降至 71M（-11%），時間從 28ms 降至 27ms

#### 優化 4：簡化 F/G 布林函數（減少依賴鏈長度）
- F: `(b&c)|(~b&d)` → `d^(b&(c^d))`（4 運算 → 3 運算）
- G: `(b&d)|(c&~d)` → `c^(d&(b^c))`（4 運算 → 3 運算）
- 改善指令級並行，周期數進一步下降

#### 嘗試但放棄的方案

| 方案 | 結果 | 原因 |
|------|------|------|
| `w [16]i64` 陣列替代 w0-w15 | 分支降至 91，但記憶體暴增至 13.2MB | 每迭代 malloc 64×8=512 字節 |
| `load-word` 輔助函數 | 指令數增加 16M（208M vs 192M） | -O3 未內聯，16 次函數呼叫開銷 |
| 移除旋轉結果 `& mask` | 無效果 | LLVM 優化器已自動移除冗餘 mask |

優化前後對比：

| 指標 | 優化前 | 優化後 | 變化 |
|------|--------|--------|------|
| 耗時 | 28.0ms | 26.2ms | -6.4% |
| 指令數 | 203M | 197M | -3.0% |
| 周期數 | 80M | 71M | -11.3% |
| rot-left 呼叫 | 64 | 0 | -100% |
| 分支標籤 | 427 | ~100 | -77% |

### 記憶體

- **Rust** 記憶體使用最低（1.0MB），md-5 crate 零分配設計
- **Go** 次之（2.5MB），含 Go runtime 基礎開銷
- **Nolang** 最高（7.0MB），因 `[64]byte` 填充緩衝區每次 md5 呼叫分配 64 字節堆記憶體
  - 未來可通過棧分配或緩衝區復用降低

### 體積

- **Rust** 可執行檔最小（464K），但包含 md-5 crate 依賴
- **Nolang** 中等（705K），包含 std/hash/md5 靜態連結
- **Go** 最大（1.7M），包含 Go runtime（GC、scheduler 等）

## 未來優化方向

1. **支援 `u32` 類型**：消除所有 `& mask` 操作，預計節省 ~6.4M 指令（3%）
2. **多字節載入內建函數**：類似 `u32::from_le_bytes`，消除逐字節組裝，預計節省 ~19M 指令（10%）
3. **消除邊界檢查**：編譯器在可證明安全時跳過檢查，預計節省 ~12M 指令（6%）
4. **棧分配陣列**：避免 `[64]byte` 緩衝區的堆分配，降低記憶體使用
5. **`rotate_left` 內建函數**：映射到 CPU 原生指令（ARM64: `ROR`），消除移位+或+mask

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
   - 根因：`%vec` 類型的全局變數在 `generateMainFunction` 中被跳過
   - 修復：將 `%vec` 加入需要 runtime 初始化的類型列表

5. **`generateLet` 將全局變數錯誤標記為局部變數**
   - 根因：`g.funcLocalNames[name] = true` 覆蓋了全局變數標記
   - 修復：僅對非全局變數設置 `funcLocalNames`

6. **統一 `.len` 屬性存取**
   - vec/arr/str 統一使用 `.len`，標準庫新增 `str.len`、`[n]t.len` 方法定義

7. **啟用 -O3 優化**
   - 將 `opt -O2` 改為 `opt -O3`，周期數降低 ~10%
