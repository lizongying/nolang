// Fibonacci — Rust 版本，純計算無 I/O（附編譯屏障）
// 編譯: rustc -O -o test/bench/fib_rust_noprint test/bench/fib_noprint.rs

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
    let mut result: i64 = 0;
    for i <- 0..10000000 {
        let n = if i < 5000000 { 40 } else { 41 };
        fib(n, &mut result);
        std::hint::black_box(result);
    }
}

