// md5-bench — C MD5 benchmark (macOS CommonCrypto)
//
// 100,000 iterations of MD5("abc", modifying input[2] each iteration)
//
// Build: clang -O3 -o md5-bench_c md5-bench.c
// Run:   ./md5-bench_c

#include <stdio.h>
#include <CommonCrypto/CommonDigest.h>

int main() {
    unsigned char input[3] = {'a', 'b', 'c'};
    unsigned char result[CC_MD5_DIGEST_LENGTH];

    for (int i = 0; i < 100000; i++) {
        input[2] = (unsigned char)(i & 255);
        CC_MD5(input, 3, result);
    }

    // Output to prevent dead code elimination
    printf("%d\n", result[0]);
    return 0;
}
