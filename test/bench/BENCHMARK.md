# Nolang VS Rust

lizongying/nolang

## 測試環境

| 項目 | 值 |
|------|-----|
| CPU | Apple M1 (ARM64, 3.2GHz) |
| RAM | 16 GB |
| OS | macOS 15.x |
| C 編譯器 | zig cc 0.16.0 (`-O2 -target aarch64-macos`) |
| Rust 編譯器 | rustc 1.91.0 (`-O`) |
| Go 編譯器 | go 1.22.x (`go build -ldflags="-s -w"`) |
| Nolang | `nolang-build --target llvm` → `zig cc -O2` |

---

## 測試程式碼

四種語言完全相同的邏輯結構：引用傳递輸出參數，`fib(40) × 10,000,000` 次。

### Nolang (`test/bench/fib.no`)

```no
fib(n i64, o i64) {
    a = 0
    b = 1
    i = 2
    for i <= n {
        c = a + b
        a = b
        b = c
        i = i + 1
    }
    o = b
}

for iter = 0; iter < 10000000; iter++ {
    result = 0
    fib(40, result)
    println-i64(result)
}
```

### C (`test/bench/fib.c`)

```c
#include <stdio.h>

void fib(long n, long *o) {
    long a = 0, b = 1, c, i;
    for (i = 2; i <= n; i++) {
        c = a + b;
        a = b;
        b = c;
    }
    *o = b;
}

int main() {
    for (int iter = 0; iter < 10000000; iter++) {
        long result;
        fib(40, &result);
        printf("%ld\n", result);
    }
    return 0;
}
```

### Rust (`test/bench/fib.rs`)

```rust
fn fib(n: i64, o: &mut i64) {
    let mut a = 0;
    let mut b = 1;
    let mut i = 2;
    while i <= n {
        let c = a + b;
        a = b;
        b = c;
        i += 1;
    }
    *o = b;
}

fn main() {
    for _ in 0..10000000 {
        let mut result: i64 = 0;
        fib(40, &mut result);
        println!("{}", result);
    }
}
```

### Go (`test/bench/fib.go`)

```go
package main

import "fmt"

func fib(n int64, o *int64) {
    var a, b, c int64 = 0, 1, 0
    var i int64 = 2
    for i <= n {
        c = a + b
        a = b
        b = c
        i++
    }
    *o = b
}

func main() {
    for iter := 0; iter < 10000000; iter++ {
        var result int64 = 0
        fib(40, &result)
        fmt.Println(result)
    }
}
```

---

## 測量工具

macOS `/usr/bin/time -l`，記錄：
- `real` — 真實執行時間
- `user` — 用戶態 CPU 時間
- `sys` — 系統態 CPU 時間
- `instructions retired` — 退休指令數（硬體計數器）
- `peak memory footprint` — 峰值記憶體用量

---

## 測試結果

### 可執行檔體積

| 語言 | 編譯器 | 大小 | 相對 C |
|------|--------|------|--------|
| **C** | zig cc -O2 | **49K** | 1.0× |
| **Nolang** | zig cc -O2 | **49K** | 1.0× |
| **Rust** | rustc -O | **456K** | 9.3× |
| **Go** | go build -s -w | **1.5M** | **31×** |

### 執行效能

`fib(40) × 10,000,000` 次，含輸出 I/O：

| 語言 | real | user | sys | 指令數 | 峰值 RSS |
|------|------|------|-----|--------|----------|
| **C** (zig cc) | 4.88s | 0.62s | 0.04s | **11.0B** | 935KB |
| **Nolang** (zig cc) | 5.38s | 0.61s | 0.04s | **11.1B** | 935KB |
| **Rust** (rustc -O) | 5.44s | 1.61s | 3.78s | 54.6B | 935KB |
| **Go** (go build) | 6.25s | 2.52s | 3.72s | 60.9B | **8.8MB** |

### 指令數對比

```
C       : 11.0B  ████
Nolang  : 11.1B  ████   ← 僅多 1%
Rust    : 54.6B  ████████████████████
Go      : 60.9B  ██████████████████████
```

---

## 關於靜態連結的說明

### macOS 限制

在 macOS 上，**所有程式都必須動態連結 `/usr/lib/libSystem.B.dylib`**（核心系統庫）。這不是語言選擇，而是作業系統的限制——macOS 不提供靜態版本的 `libSystem`。

```bash
# 所有語言的連結狀況完全相同
otool -L fib_nolang  → /usr/lib/libSystem.B.dylib
otool -L fib_c       → /usr/lib/libSystem.B.dylib
otool -L fib_rust    → /usr/lib/libSystem.B.dylib
otool -L fib_go      → /usr/lib/libSystem.B.dylib
```

Go 也額外連結了 `/usr/lib/libresolv.9.dylib`，但這同樣是系統動態庫。

### 實際靜態內容

因為 `libSystem.dylib` 在執行期由所有程式共享，可執行檔大小的比較反映的是**語言 runtime + 標準庫的額外體積**：

| 語言 | 檔案大小 | 包含的靜態內容 |
|------|---------|---------------|
| C / Nolang | 49K | printf + 啟動程式碼（無 runtime） |
| Rust | 456K | Rust 標準庫（格式化、panic handler、hash） |
| Go | 1.5M | Go runtime（GC、goroutine scheduler、fmt 反射） |

### Linux 靜態連結（僅供參考）

如果需要在 Linux 上進行完全靜態比較，可使用 zig cc 的 musl 目標：

```bash
# 靜態編譯（x86_64 Linux + musl libc）
zig cc -O2 -target x86_64-linux-musl -static -o fib_c_static fib.c
```

這種方式產生的二進位完全靜態，不依賴任何系統 `.so`。但效能數據因 OS / libc 不同無法與 macOS 直接比較。

---

## 編譯鏈

```
Nolang: .no → lexer + parser → LLVM IR → zig cc -O2 → 可執行檔
C:      .c  → zig cc -O2 → 可執行檔
Rust:   .rs → rustc -O → 可執行檔（含 std）
Go:     .go → go build → 可執行檔（含 runtime）
```

## 對比腳本

```bash
# 編譯
zig cc -O2 -target aarch64-macos -o test/bench/fib_c_zig test/bench/fib.c
zig cc -O2 -target aarch64-macos test/bench/fib_nolang.ll -o test/bench/fib_nolang_zig
rustc -O -o test/bench/fib_rust test/bench/fib.rs
go build -ldflags="-s -w" -o test/bench/fib_go test/bench/fib.go

# 執行 + 測量
/usr/bin/time -l test/bench/fib_c_zig
/usr/bin/time -l test/bench/fib_nolang_zig
/usr/bin/time -l test/bench/fib_rust
/usr/bin/time -l test/bench/fib_go

# 或使用自動化腳本
bash test/bench/run.sh
```
