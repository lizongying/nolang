# 效能對比測試結果（2026-07-20 第二次）

> 測試環境：Apple M1, macOS
> 優化等級：zig cc -O3 / rustc -C opt-level=3 / go build -ldflags="-s -w"

## 測試項目

- **有 I/O**：`fib(40) × 10,000,000` 次（含 print）
- **無 I/O**：`fib(40/41) × 10,000,000` 次（純計算，全域 volatile 編譯屏障防止優化）

## 有 I/O（含 printf / println / fmt.Println）

| 語言 | 編譯器 | real | user | 指令數 | RSS |
|------|--------|------|------|--------|-----|
| **C** | zig cc -O3 | 0.68s | 0.68s | 10.9B | 1.4MB |
| **Nolang** | no build → zig cc -O3 | **0.68s** | **0.68s** | 11.1B | **1.4MB** |
| **Rust** | rustc -C opt-level=3 | 5.82s | 5.82s | 36.4B | 1.6MB |
| **Go** | go build -ldflags="-s -w" | 7.07s | 7.07s | 49.2B | 10.4MB |

## 無 I/O（純計算，fib(40/41) × 10M）

| 語言 | 編譯器 | real | user | 指令數 | RSS |
|------|--------|------|------|--------|-----|
| **C** | zig cc -O3 | 0.13s | 0.13s | 2.07B | 1.4MB |
| **Nolang** | no build → zig cc -O3 | **0.13s** | **0.13s** | **2.06B** | 1.4MB |
| **Rust** | rustc -C opt-level=3 | 0.17s | 0.17s | 2.07B | 1.5MB |
| **Go** | go build -ldflags="-s -w" | 0.14s | 0.14s | 2.56B | 3.4MB |

## 純計算對比圖

```
指令數（越低越好）:
Nolang: 2.06B  ████████████████████████████████████████   (1.00×)
C     : 2.07B  █████████████████████████████████████████  (1.00×)
Rust  : 2.07B  █████████████████████████████████████████  (1.00×)
Go    : 2.56B  ██████████████████████████████████████████ (1.24×)

執行時間 real（越低越好）:
Nolang: 0.13s  █████████████                             (1.0×)
C     : 0.13s  █████████████                             (1.0×)
Go    : 0.14s  ██████████████                            (1.1×)
Rust  : 0.17s  ██████████████████                        (1.3×)
```

## 分析

### 無 I/O 純計算

| 指標 | C | Nolang | Rust | Go |
|------|---|--------|------|----|
| 指令數 | 2.07B | 2.06B (-0.5%) | 2.07B (0%) | 2.56B (+24%) |
| real time | 0.13s | 0.13s | 0.17s | 0.14s |

- **Nolang ≈ C**（指令數差異 <1%，同一 LLVM 後端生成相同機器碼）
- **Rust** 指令數與 C/Nolang 一致，執行時間多 30%
- **Go** 多 24% 指令，但 real time 與 C 接近

### I/O 開銷（有 I/O vs 無 I/O）

| 語言 | 純計算 | 含 I/O | 時間放大 | 指令放大 |
|------|--------|--------|---------|---------|
| **C** | 0.13s / 2.07B | 0.68s / 10.9B | **×5.2** | **×5.3** |
| **Nolang** | 0.13s / 2.06B | 0.68s / 11.1B | **×5.2** | **×5.4** |
| **Rust** | 0.17s / 2.07B | 5.82s / 36.4B | **×34.2** | **×17.6** |
| **Go** | 0.14s / 2.56B | 7.07s / 49.2B | **×50.5** | **×19.2** |

- C 和 Nolang 的 I/O 放大倍數完全一致（×5.2），底層都是直接調用 libc printf
- Rust 的 `println!` 開銷 ×34 時間 / ×18 指令（格式化 + 鎖定）
- Go 的 `fmt.Println` 開銷 ×51 時間 / ×19 指令（反射 + buffer + GC）

## 結論

**純計算效能：Nolang = C ≈ Rust >> Go**

Nolang 透過 LLVM 後端生成與 C 完全相同品質的機器碼，指令數差異 <0.5%。

**有 I/O 時 Nolang 與 C 完全持平**（real 0.68s），Rust 慢 8.5×，Go 慢 10.4×。

**I/O 效能差距**來自標準庫實作差異，非語言本身：
- C / Nolang：printf → libc 輕量封裝（×5 放大）
- Rust：println! → 格式化 + 鎖定（×34 放大）
- Go：fmt.Println → 反射 + buffer + GC（×51 放大）

## 編譯器版本

| 語言 | 編譯器 |
|------|--------|
| C | zig cc 0.16.0 |
| Nolang | no build (LLVM 21.1.5) → zig cc -O3 |
| Rust | rustc 1.91.0 |
| Go | go 1.25.4 |


no
```no
; Fibonacci — 無 I/O 純計算
fib = (n i64) (o i64) {
    a = 0
    b = 1
    i = 2
    {
        c = a + b
        a = b
        b = c
        i = i + 1
    } (i <= n)
    o = b
}

{
    result = fib(40)
    print(result)
} * 10000000

```

c
```c
// Fibonacci — C 版本，與 Nolang 相同引用傳遞模式
// 編譯: clang -O2 -o test/bench/fib_c test/bench/fib.c

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

rs
```rs
// Fibonacci — Rust 版本，與 Nolang 相同引用傳遞模式
// 編譯: rustc -O -o test/bench/fib_rust test/bench/fib.rs

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

go
```go
// Fibonacci — Go 版本，與 Nolang 相同引用傳遞模式
// 編譯: go build -o test/bench/fib_go test/bench/fib.go

package main

import (
	"fmt"
)

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