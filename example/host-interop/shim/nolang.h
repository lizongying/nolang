#ifndef NOLANG_H
#define NOLANG_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#if defined(_WIN32)
#  define NOLANG_API __declspec(dllexport)
#elif defined(__GNUC__)
#  define NOLANG_API __attribute__((visibility("default")))
#else
#  define NOLANG_API
#endif

/* Stable public API. Never expose the internal nl_* symbols to host code. */
NOLANG_API int64_t nolang_add(int64_t a, int64_t b);
NOLANG_API double  nolang_scale(double x, double k);
NOLANG_API int64_t nolang_sum3(int64_t a, int64_t b, int64_t c);

#ifdef __cplusplus
}
#endif

#endif /* NOLANG_H */
