#ifndef NOUI_IMG_PNG_H
#define NOUI_IMG_PNG_H

/* 阶段78：最小 PNG 解码（zlib inflate + 反滤波），输出 RGBA8。
 * 支持 bit depth 8 / 非隔行 / 色彩类型 0,2,3,4,6。 */
typedef struct {
    unsigned char *rgba; /* w*h*4，内存顺序 R,G,B,A */
    int w;
    int h;
} noui_img_t;

unsigned char *noui_png_alloc_rgba(int w, int h);
int noui_png_decode(const unsigned char *data, long len, noui_img_t *out);
void noui_img_free(noui_img_t *im);

#endif
