// md5-bench — Rust MD5 基準測試（md-5 crate）
//
// 測量 100,000 次 MD5（"abc"，每次修改 input[2] 防止死代碼消除）
//
// 編譯：cargo build --release
// 執行：./target/release/md5-bench

use md5::{Md5, Digest};

fn main() {
    let mut input = *b"abc";
    let mut result = [0u8; 16];

    for i in 0..100_000u32 {
        input[2] = (i & 255) as u8;
        let mut hasher = Md5::new();
        hasher.update(input);
        result = hasher.finalize().into();
    }

    // 輸出以防止死代碼消除
    println!("{}", result[0]);
}
