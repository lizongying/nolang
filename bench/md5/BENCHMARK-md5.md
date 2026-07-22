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
| **Nolang** | clang -O3 | **757K** | 1.6× |
| **Go** | go build -s -w | **1.7M** | 3.7× |

### 執行效能

`MD5(input, 修改 input[2]) × 100,000` 次，15 次取截尾平均值（毫秒精度）：

| 語言 | 平均耗時 | 最佳 | 最差 | 相對最快 |
|------|----------|------|------|----------|
| **Rust** (md-5 crate) | **16.1ms** | 15.5ms | 16.8ms | 1.0× |
| **Nolang** (std/hash/md5, u32) | **17.3ms** | 16.8ms | 18.0ms | 1.07× |
| **Go** (crypto/md5) | **65.5ms** | 63.1ms | 67.5ms | 4.1× |

### 指令數對比

```
Rust    :  80M  ████
Nolang  :  88M  ████▌
Go      : 547M  ███████████████████████████
```

### 周期數對比

```
Rust    :  34M  ████
Nolang  :  39M  ████▌
Go      : 179M  ██████████████████████
```

### 峰值記憶體對比

```
Rust    : 1.5MB  █
Nolang  : 1.5MB  █
Go      : 4.0MB  ███
```

### 詳細數據

| 語言 | 耗時(ms) | 指令數 | 周期數 | IPC | 峰值 RSS | 二進制大小 |
|------|----------|--------|--------|-----|----------|------------|
| **Rust** | 15.3 | 80M | 34M | 2.37 | 1.5MB | 464K |
| **Nolang** | 16.9 | 88M | 39M | 2.27 | 1.5MB | 757K |
| **Go** | 65.5 | 547M | 179M | 3.06 | 4.0MB | 1.7M |

## 分析

### 效能

- **Rust** 最快（15.3ms），md-5 crate 使用原生 `u32` 類型和 `u32::from_le_bytes` 零開銷載入
- **Nolang** 次之（16.9ms），為 Rust 的 **1.10×**，比 Go 快 **3.6×**
- **Go** 最慢（61.4ms），crypto/md5 標準庫在此場景下效能較低

### Nolang vs Rust 差距分析

通過 `no build -v` 輸出 LLVM IR 並與 Rust md-5 crate 源碼對比，定位了以下差距：

| 差距 | Rust 做法 | Nolang 做法 | 影響 |
|------|----------|------------|------|
| **字載入** | `u32::from_le_bytes` 1 條指令載入 4 字節 | `load-le-u32` 內建函數，1 條指令載入 | 已消除差距 |
| **邊界檢查** | 無（直接記憶體存取） | 全塊用 `load-le-u32` 直接載入（無 BC）；末塊殘留 7 次 BC | ~2M 指令 |
| **旋轉** | `rotate_left` CPU 原生指令 | `rotate-left` 內建 → LLVM `fshl` → ARM64 `ROR` | 已消除差距 |
| **資料拷貝** | 無拷貝，直接從輸入指標處理區塊 | 全塊直接從 `data` 載入；末塊用 `load-le-u32`/`store-le-u32` 4 字節拷貝 | 已消除大部分 |
| **緩衝區分配** | 棧分配 | `alloca` 堆疊分配（已從 malloc 優化） | 已消除差距 |
| **range 迴圈開銷** | 無（Rust 用 `for` + 迭代器） | 常數邊界生成簡單 `for`（無 select）；變數邊界循環分裂（方向只判斷一次） | 已消除差距 |

**剩餘差距**（~1.10×）：主要來自：
1. **末塊殘留邊界檢查**（7 次/呼叫 × 100K = 700K 次）：末塊的剩餘位元組拷貝仍有逐字節邊界檢查
2. **memset 開銷**：每次 md5 呼叫都需 memset 64 位元組緩衝區
3. **LLVM 優化路徑差異**：Rust IR 使用 `noalias`/`dereferenceable`/`captures(none)` 等屬性幫助 LLVM 做更激進的優化，Nolang IR 缺少這些屬性

#### 優化歷程

| 方案 | 結果 | 原因 |
|------|------|------|
| `w [16]i64` 陣列替代 w0-w15 | 分支降至 91，但記憶體暴增至 13.2MB | 每迭代 malloc 64×8=512 字節 |
| `load-word` 輔助函數 | 指令數增加 16M（208M vs 192M） | -O3 未內聯，16 次函數呼叫開銷 |
| `load-le-u32` 內建函數 | 指令數 196M → 94M（**-52%**） | 直接生成 `load i32` 指令 |
| `rotate-left/right` 內建函數 | 週期數 55M → 38M（**-31%**） | 映射至 ARM64 `ROR` 原生指令 |
| BCE（邊界檢查消除） | 指令數進一步降低 | 編譯器消除可證明安全的檢查 |
| `alloca` 緩衝區分配 | RSS 7.6MB → 1.5MB（**-80%**） | 棧分配替代堆分配 |
| 全塊直接載入 + 4 字節拷貝 | 指令數 94M → 89M（**-5%**），差距 1.15× → 1.07× | 全塊用 `load-le-u32(data, off)` 無拷貝；末塊用 `load-le-u32`/`store-le-u32` 4 字節拷貝 |
| range 迴圈循環分裂 | 指令數 89M → 88M，差距 1.07× → 1.10× | 常數邊界生成簡單 `for`（無 select）；變數邊界方向只判斷一次，分離 asc/desc cond 區塊 |
| 方法語法 + 類型推導 + clear() | 語法改進（效能不變） | `load-le-u32(data, off)` → `data.load-le-u32(off)`；返回值自動推導 `u32`；`j <- [0..64): buf[j] = 0` → `buf.clear()` |

優化前後對比：

| 指標 | i64 優化前 | u32 優化後 | 內建函數+alloca 後 | 直接載入優化後 | 循環分裂後 | 總變化 |
|------|-----------|-----------|-------------------|----------------|-----------|--------|
| 耗時 | 28.0ms | 22.5ms | 18.4ms | 17.3ms | 16.9ms | **-39.6%** |
| 指令數 | 203M | 196M | 94M | 89M | 88M | **-56.7%** |
| 周期數 | 80M | 55M | 38M | 39M | 39M | **-51.3%** |
| 峰值 RSS | 7.6MB | 7.6MB | 1.5MB | 1.5MB | 1.5MB | **-80.3%** |
| 與 Rust 差距 | 1.77× | 1.50× | 1.15× | 1.07× | 1.10× | **-37.9%** |

### 記憶體

- **Rust** 與 **Nolang** 均為 1.5MB，md-5 crate 和 std/hash/md5 均使用棧分配
- **Go** 最高（4.0MB），含 Go runtime 基礎開銷
- Nolang 先前因 `[64]byte` 填充緩衝區每次 md5 呼叫 malloc 64 字節導致 7.6MB RSS，
  現已通過 `alloca` 優化降至 1.5MB，與 Rust 持平

### 體積

- **Rust** 可執行檔最小（464K），但包含 md-5 crate 依賴
- **Nolang** 中等（705K），包含 std/hash/md5 靜態連結
- **Go** 最大（1.7M），包含 Go runtime（GC、scheduler 等）

## 未來優化方向

1. **殘餘邊界檢查消除**：進一步分析 IR，消除剩餘的不可證明安全的邊界檢查
2. **LLVM 優化通道調優**：研究 Rust 和 Nolang IR 差異，引導 LLVM 選擇更優的優化路徑
3. **向量化**：探索 MD5 壓縮函數的 SIMD 向量化可能性

## 修復的 Bug

在完成此對比的過程中，修復了以下編譯器 bug：

1. **字串索引賦值在循環中導致 bus error/segfault**
   - 根因：字串字面量的 data 指標指向唯讀全域常量 `@.str.N`
   - 修復：將字串字面量數據複製到 heap 上可寫記憶體（malloc + memcpy）

2. **超過約 500K 次迭代時棧溢出 segfault**
   - 根因：循環體內 `alloca` 臨時輸出空間不釋放，棧線性增長
   - 修復：使用 `@llvm.stacksave` / `@llvm.stackrestore` 包裹循環內的 alloca

3. **方法呼叫被誤解析為模組限定呼叫**
   - 根因：`data.load-le-u32(off)` 中 `data` 被當作模組前綴剝離，找到全域 `load-le-u32` 內建函數
   - 修復：模組前綴剝離前檢查首段是否為已註冊變數，若為變數則跳過剝離，交由方法解析路徑處理
