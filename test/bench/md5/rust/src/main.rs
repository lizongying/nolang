// md5-bench — Rust MD5 基準測試（md-5 crate）
//
// 測量 1,000,000 次 MD5（"abc" 變體）
// 每次迭代修改輸入以阻止優化器消除循環。
//
// 編譯：cargo build --release
// 執行：./target/release/md5-bench_rust

use md5::{Md5, Digest};

fn main() {
    let mut input = b"abc".to_vec();
    let mut result = [0u8; 16];

    for i <- 0..1_000_000 {
        input[2] = i as u8;
        let mut hasher = Md5::new();
        hasher.update(&input);
        result = hasher.finalize().into();
    }

    // 輸出以防止死代碼消除
    println!("{}", result[0]);
}
