/* 阶段78：最小 PNG 解码器（noui 图片 / 精灵管线）
 *
 * 只依赖 zlib（inflate）。覆盖 UI 场景实际需要的子集：
 *   - bit depth 8，非隔行（interlace=0）
 *   - 色彩类型 0(灰) / 2(RGB) / 3(调色板) / 4(灰+alpha) / 6(RGBA)
 *   - 五种扫描线滤波器全实现（0 None / 1 Sub / 2 Up / 3 Average / 4 Paeth）
 * 输出一律 RGBA8。不支持的格式返回非 0，调用方按「加载失败」处理。
 *
 * 为什么不用 SDL_image：noui 的 shim 要保持零额外依赖（CI 只装 sdl3/sdl3_ttf），
 * 而 PNG 是 sprite 的事实标准；手写 inflate 封装比重型图像库更可控。
 */
#include "img_png.h"

#include <stdlib.h>
#include <string.h>
#include <zlib.h>

unsigned char *noui_png_alloc_rgba(int w, int h) {
    return (unsigned char *)malloc((size_t)w * (size_t)h * 4);
}

static unsigned be32(const unsigned char *p) {
    return ((unsigned)p[0] << 24) | ((unsigned)p[1] << 16) |
           ((unsigned)p[2] << 8) | (unsigned)p[3];
}

static int paeth(int a, int b, int c) {
    int p = a + b - c;
    int pa = p > a ? p - a : a - p;
    int pb = p > b ? p - b : b - p;
    int pc = p > c ? p - c : c - p;
    if (pa <= pb && pa <= pc) return a;
    if (pb <= pc) return b;
    return c;
}

/* 反滤波：raw 是 h*(1+w*bpp) 的扫描线流，写回 RGBA8 out */
static int unfilter_rgba(const unsigned char *raw, int w, int h, int ch,
                         const unsigned char *pal, int pal_n,
                         unsigned char *out) {
    size_t stride = (size_t)w * (size_t)ch;
    unsigned char *prev = (unsigned char *)calloc(stride, 1);
    unsigned char *cur = (unsigned char *)malloc(stride);
    if (!prev || !cur) { free(prev); free(cur); return -1; }
    for (int y = 0; y < h; y++) {
        const unsigned char *line = raw + (size_t)y * (stride + 1);
        int filt = line[0];
        const unsigned char *src = line + 1;
        if (filt > 4) { free(prev); free(cur); return -1; }
        for (size_t i = 0; i < stride; i++) {
            int a = i >= (size_t)ch ? cur[i - ch] : 0;
            int b = prev[i];
            int c = i >= (size_t)ch ? prev[i - ch] : 0;
            int v = src[i];
            switch (filt) {
                case 0: break;
                case 1: v += a; break;
                case 2: v += b; break;
                case 3: v += (a + b) >> 1; break;
                case 4: v += paeth(a, b, c); break;
            }
            cur[i] = (unsigned char)v;
        }
        unsigned char *o = out + (size_t)y * w * 4;
        for (int x = 0; x < w; x++) {
            const unsigned char *p = cur + (size_t)x * ch;
            unsigned char r, g, b, al = 255;
            switch (ch) {
                case 1: {
                    /* 灰度，或调色板索引 */
                    if (pal) {
                        int idx = p[0];
                        if (idx >= pal_n) { free(prev); free(cur); return -1; }
                        r = pal[idx * 3];
                        g = pal[idx * 3 + 1];
                        b = pal[idx * 3 + 2];
                    } else {
                        r = g = b = p[0];
                    }
                    break;
                }
                case 2: r = g = b = p[0]; al = p[1]; break;
                case 3: r = p[0]; g = p[1]; b = p[2]; break;
                default: r = p[0]; g = p[1]; b = p[2]; al = p[3]; break;
            }
            o[x * 4] = r; o[x * 4 + 1] = g; o[x * 4 + 2] = b; o[x * 4 + 3] = al;
        }
        unsigned char *t = prev; prev = cur; cur = t;
    }
    free(prev);
    free(cur);
    return 0;
}

int noui_png_decode(const unsigned char *data, long len, noui_img_t *out) {
    static const unsigned char sig[8] = {137, 80, 78, 71, 13, 10, 26, 10};
    if (!data || len < 8 || memcmp(data, sig, 8) != 0) return -1;
    long pos = 8;
    int w = 0, h = 0, bitd = 0, ct = -1;
    unsigned char pal[256 * 3];
    int pal_n = 0;
    unsigned char *idat = NULL;
    size_t idat_len = 0, idat_cap = 0;
    int seen_ihdr = 0, seen_iend = 0;

    while (pos + 8 <= len) {
        unsigned clen = be32(data + pos);
        const char *type = (const char *)(data + pos + 4);
        long body = pos + 8;
        if (body + (long)clen + 4 > len) break;
        if (memcmp(type, "IHDR", 4) == 0 && clen >= 13) {
            w = (int)be32(data + body);
            h = (int)be32(data + body + 4);
            bitd = data[body + 8];
            ct = data[body + 9];
            int comp = data[body + 10];
            int filt = data[body + 11];
            int inter = data[body + 12];
            if (w <= 0 || h <= 0 || w > (1 << 24) || h > (1 << 24)) return -1;
            if (bitd != 8 || comp != 0 || filt != 0 || inter != 0) return -1;
            seen_ihdr = 1;
        } else if (memcmp(type, "PLTE", 4) == 0) {
            pal_n = (int)(clen / 3);
            if (pal_n > 256) pal_n = 256;
            memcpy(pal, data + body, (size_t)pal_n * 3);
        } else if (memcmp(type, "IDAT", 4) == 0) {
            if (seen_ihdr && idat_len + clen > idat_cap) {
                size_t ncap = idat_cap ? idat_cap * 2 : 65536;
                while (ncap < idat_len + clen) ncap *= 2;
                unsigned char *nb = (unsigned char *)realloc(idat, ncap);
                if (!nb) { free(idat); return -1; }
                idat = nb; idat_cap = ncap;
            }
            if (seen_ihdr) {
                memcpy(idat + idat_len, data + body, clen);
                idat_len += clen;
            }
        } else if (memcmp(type, "IEND", 4) == 0) {
            seen_iend = 1;
            break;
        }
        pos = body + (long)clen + 4;
    }
    (void)seen_iend;
    if (!seen_ihdr || !idat || ct < 0) { free(idat); return -1; }
    int ch;
    switch (ct) {
        case 0: ch = 1; break;
        case 2: ch = 3; break;
        case 3: ch = 1; break;
        case 4: ch = 2; break;
        case 6: ch = 4; break;
        default: free(idat); return -1;
    }
    if (ct == 3 && pal_n <= 0) { free(idat); return -1; }

    size_t stride = (size_t)w * (size_t)ch;
    size_t raw_len = (size_t)h * (stride + 1);
    unsigned char *raw = (unsigned char *)malloc(raw_len);
    if (!raw) { free(idat); return -1; }
    uLongf dst_len = (uLongf)raw_len;
    int zr = uncompress(raw, &dst_len, idat, (uLong)idat_len);
    free(idat);
    if (zr != Z_OK || dst_len != raw_len) { free(raw); return -1; }

    unsigned char *rgba = (unsigned char *)malloc((size_t)w * (size_t)h * 4);
    if (!rgba) { free(raw); return -1; }
    const unsigned char *palp = (ct == 3) ? pal : NULL;
    int rr = unfilter_rgba(raw, w, h, ch, palp, pal_n, rgba);
    free(raw);
    if (rr != 0) { free(rgba); return -1; }
    out->rgba = rgba;
    out->w = w;
    out->h = h;
    return 0;
}

void noui_img_free(noui_img_t *im) {
    if (im && im->rgba) {
        free(im->rgba);
        im->rgba = NULL;
    }
    im->w = 0;
    im->h = 0;
}
