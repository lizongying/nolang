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
