// Fibonacci — C 版本，純計算無 I/O（附編譯屏障）
// 編譯: zig cc -O2 -target aarch64-macos -o test/bench/fib_c_noprint test/bench/fib_noprint.c

void fib(long n, long *o) {
    long a = 0, b = 1, c, i;
    for (i = 2; i <= n; i++) {
        c = a + b;
        a = b;
        b = c;
    }
    *o = b;
}

static volatile long g_sink = 0;

int main() {
    long result = 0;
    for (int iter = 0; iter < 10000000; iter++) {
        // 每次傳入不同 n，防止編譯器迴圈提升
        long n = iter < 5000000 ? 40 : 41;
        fib(n, &result);
        g_sink = result;  // 寫入全域 volatile — 編譯器無法優化掉迴圈
    }
    return (int)g_sink;
}
