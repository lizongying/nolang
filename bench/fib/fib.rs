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
