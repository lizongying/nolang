#include <stdint.h>

#include "nolang.h"

/*
 * Internal Nolang symbols. `no build` emits every user function as
 * `define void @<name>(<inputs by value>, <result out-pointer>)`.
 * These signatures are NOT a public ABI: abi_guard.py re-checks them against
 * dist/libnolang.ll on every build and fails loudly if they ever change.
 * The `nl_` prefix avoids collisions with the compiler runtime prelude.
 */
extern void nl_add(int64_t a, int64_t b, int64_t *out);
extern void nl_scale(double x, double k, double *out);
extern void nl_sum3(int64_t a, int64_t b, int64_t c, int64_t *out);

int64_t nolang_add(int64_t a, int64_t b) {
    int64_t out = 0;
    nl_add(a, b, &out);
    return out;
}

double nolang_scale(double x, double k) {
    double out = 0.0;
    nl_scale(x, k, &out);
    return out;
}

int64_t nolang_sum3(int64_t a, int64_t b, int64_t c) {
    int64_t out = 0;
    nl_sum3(a, b, c, &out);
    return out;
}
