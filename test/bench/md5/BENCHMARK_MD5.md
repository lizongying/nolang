# MD5 基準測試

測試各語言實現 MD5 在 1,000,000 次迭代中的效能。

## 測試結構（三者一致）

```
for i in 0..1_000_000:
    input[2] = byte(i)          # 修改輸入阻止優化
    result = md5(input)         # 計算 MD5
print(result[0])                # 輸出防止死代碼消除
```

## 目錄結構

```
test/bench/md5/
├── BENCHMARK_MD5.md
├── go/
│   ├── md5-bench.go            # Go 標準庫 crypto/md5
│   └── md5-bench_go            # 編譯結果
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
./md5-bench_go

# Rust
cd test/bench/md5/rust
cargo build --release
./target/release/md5-bench_rust

# Nolang（需要 md5.no 編譯通過後）
cd test/bench/md5/nolang
nolang-build md5-bench.no -o /tmp/md5-bench
/tmp/md5-bench
```
