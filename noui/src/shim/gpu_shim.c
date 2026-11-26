/*
 * noui SDL_GPU backend shim —— 用 SDL_GPU (Metal/Vulkan/DX12) 画彩色四边形与纹理。
 * 与 shim.c (SDL_Render 回退) 并列：此文件导出 noui_gpu_* 标量 FFI。
 *
 * 阶段79：只有彩色矩形（无纹理管线）。
 * 阶段82：补齐纹理管线 ——
 *   - 纹理注册表：PNG 图片与文字蒙版纹理共用（id 从 1 起，0=无效）；
 *   - 两条图形管线：四边形（片元 SDF 圆角，aux 携带 size/rad）与纹理采样；
 *   - 指令按 Scene 顺序入 op 队列，帧末按序绘制，保证 z 序与软件后端一致；
 *   - alpha 混合打开（src_alpha / one_minus_src_alpha），半透明主题色生效；
 *   - 文字走 TTF -> surface -> RGBA 纹理（白底 alpha 蒙版，着色时乘颜色），
 *     按字符串做 LRU 缓存，避免每帧重建纹理。
 * MSL shader 以源码形式交给 SDL_CreateGPUShader (format=MSL)，macOS 走 Metal。
 */
#include <SDL3/SDL.h>
#include <SDL3_ttf/SDL_ttf.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include "gpu_shim.h"
#include "img_png.h"

/* ---- 顶点格式 ----
 * 四边形：clip pos(2) + uv(2) + color(4) + aux(3: sx, sy, rad) = 48 bytes。
 * 纹理：  clip pos(2) + uv(2) + color(4) = 32 bytes。 */
typedef struct { float x, y, u, v, r, g, b, a, sx, sy, rad; } QVert;
typedef struct { float x, y, u, v, r, g, b, a; } TVert;

enum { OP_QUAD = 0, OP_TEX = 1 };
typedef struct {
    int kind;
    int start;          /* 在各自顶点数组里的起始下标（顶点序号） */
    long long tex;      /* OP_TEX：纹理 id */
} Op;

#define NOUI_GPU_TEX_MAX 256
#define NOUI_GPU_TEXT_MAX 128

static SDL_GPUDevice *g_dev = NULL;
static SDL_Window *g_win = NULL;
static SDL_GPUGraphicsPipeline *g_pipe_quad = NULL;
static SDL_GPUGraphicsPipeline *g_pipe_tex = NULL;
static SDL_GPUSampler *g_smp = NULL;

static QVert *g_qv = NULL;
static int g_qn = 0, g_qcap = 0;
static TVert *g_tv = NULL;
static int g_tn = 0, g_tcap = 0;
static Op *g_ops = NULL;
static int g_on = 0, g_ocap = 0;

static SDL_GPUTexture *g_tex[NOUI_GPU_TEX_MAX];
static Uint32 g_tex_w[NOUI_GPU_TEX_MAX];
static Uint32 g_tex_h[NOUI_GPU_TEX_MAX];

/* 文字纹理缓存（key -> 纹理 id），LRU 淘汰 */
typedef struct { char *key; long long id; int tick; } TextEnt;
static TextEnt g_texts[NOUI_GPU_TEXT_MAX];
static int g_tick = 0;

static TTF_Font *g_font = NULL;
static int g_quit = 0;
/* 清屏色（由 noui_gpu_set_clear 设置，默认深灰蓝） */
static float g_clear_r = 0.12f, g_clear_g = 0.12f, g_clear_b = 0.16f, g_clear_a = 1.0f;

/* ============================================================================
 * MSL 着色器
 * ============================================================================ */
/* 四边形：aux.xy = 盒子像素尺寸，aux.z = 圆角半径；片元用 SDF 画圆角 + 边缘 AA */
static const char *VS_QUAD_MSL =
"#include <metal_stdlib>\n"
"using namespace metal;\n"
"struct VSIn {\n"
"    float2 pos [[attribute(0)]]; float2 uv [[attribute(1)]];\n"
"    float4 color [[attribute(2)]]; float3 aux [[attribute(3)]];\n"
"};\n"
"struct VSOut { float4 pos [[position]]; float2 uv; float4 color; float3 aux; };\n"
"vertex VSOut vs_quad(VSIn in [[stage_in]]) {\n"
"    VSOut o; o.pos = float4(in.pos, 0.0, 1.0); o.uv = in.uv;\n"
"    o.color = in.color; o.aux = in.aux; return o;\n"
"}\n";

static const char *FRAG_QUAD_MSL =
"#include <metal_stdlib>\n"
"using namespace metal;\n"
"struct VSOut { float4 pos [[position]]; float2 uv; float4 color; float3 aux; };\n"
"fragment float4 fs_quad(VSOut in [[stage_in]]) {\n"
"    float2 size = in.aux.xy;\n"
"    float rad = max(in.aux.z, 0.0);\n"
"    float2 p = in.uv * size - size * 0.5;\n"
"    float2 q = abs(p) - (size * 0.5 - rad);\n"
"    float dist = min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - rad;\n"
"    float aa = fwidth(dist);\n"
"    float cov = 1.0 - smoothstep(-aa, aa, dist);\n"
"    return float4(in.color.rgb, in.color.a * cov);\n"
"}\n";

/* 纹理：采样后乘顶点色（文字蒙版靠这个染色） */
static const char *VS_TEX_MSL =
"#include <metal_stdlib>\n"
"using namespace metal;\n"
"struct VSIn {\n"
"    float2 pos [[attribute(0)]]; float2 uv [[attribute(1)]];\n"
"    float4 color [[attribute(2)]];\n"
"};\n"
"struct VSOut { float4 pos [[position]]; float2 uv; float4 color; };\n"
"vertex VSOut vs_tex(VSIn in [[stage_in]]) {\n"
"    VSOut o; o.pos = float4(in.pos, 0.0, 1.0); o.uv = in.uv;\n"
"    o.color = in.color; return o;\n"
"}\n";

static const char *FRAG_TEX_MSL =
"#include <metal_stdlib>\n"
"using namespace metal;\n"
"struct VSOut { float4 pos [[position]]; float2 uv; float4 color; };\n"
"fragment float4 fs_tex(VSOut in [[stage_in]], texture2d<float> tex [[texture(0)]],\n"
"                       sampler smp [[sampler(0)]]) {\n"
"    float4 c = tex.sample(smp, in.uv);\n"
"    return float4(c.rgb * in.color.rgb, c.a * in.color.a);\n"
"}\n";

static SDL_GPUShader *load_shader(const char *src, const char *entry,
                                  SDL_GPUShaderStage stage, Uint32 nsamp) {
    SDL_GPUShaderCreateInfo ci;
    SDL_zero(ci);
    ci.code_size = strlen(src);
    ci.code = (const Uint8 *)src;
    ci.entrypoint = entry;
    ci.format = SDL_GPU_SHADERFORMAT_MSL;
    ci.stage = stage;
    /* SDL_GPU 依赖调用方声明的资源数：为 0 时 Metal 后端不会下发
     * setFragmentSamplerStates，Metal 校验层会以「missing Sampler binding」中止。 */
    ci.num_samplers = nsamp;
    return SDL_CreateGPUShader(g_dev, &ci);
}

/* 标准 alpha 混合（两个管线共用，保证行为一致） */
static SDL_GPUColorTargetBlendState make_blend(void) {
    SDL_GPUColorTargetBlendState b;
    SDL_zero(b);
    b.src_color_blendfactor = SDL_GPU_BLENDFACTOR_SRC_ALPHA;
    b.dst_color_blendfactor = SDL_GPU_BLENDFACTOR_ONE_MINUS_SRC_ALPHA;
    b.color_blend_op = SDL_GPU_BLENDOP_ADD;
    b.src_alpha_blendfactor = SDL_GPU_BLENDFACTOR_ONE;
    b.dst_alpha_blendfactor = SDL_GPU_BLENDFACTOR_ONE_MINUS_SRC_ALPHA;
    b.alpha_blend_op = SDL_GPU_BLENDOP_ADD;
    b.enable_blend = true;
    return b;
}

/* ============================================================================
 * 设备 / 窗口
 * ============================================================================ */
int noui_gpu_init(void) {
    g_quit = 0;
    if (!SDL_Init(SDL_INIT_VIDEO)) return 0;
    if (!TTF_Init()) return 0;
    g_dev = SDL_CreateGPUDevice(SDL_GPU_SHADERFORMAT_MSL, true, NULL);
    return g_dev ? 1 : 0;
}

/* GPU 后端是否已初始化（app-shutdown 依此选择清理路径）。 */
int noui_gpu_active(void) { return g_dev != NULL; }

/* 设置清屏色（0..255），下一帧 frame() 生效。 */
void noui_gpu_set_clear(int r, int g, int b, int a) {
    g_clear_r = r / 255.0f;
    g_clear_g = g / 255.0f;
    g_clear_b = b / 255.0f;
    g_clear_a = a / 255.0f;
}

static void make_pipeline(SDL_GPUShader *vs, SDL_GPUShader *fs, int textured,
                          SDL_GPUGraphicsPipeline **out) {
    SDL_GPUVertexBufferDescription vb = { 0, textured ? sizeof(TVert) : sizeof(QVert),
                                          SDL_GPU_VERTEXINPUTRATE_VERTEX, 0 };
    SDL_GPUVertexAttribute attrs[4];
    attrs[0].location = 0; attrs[0].buffer_slot = 0;
    attrs[0].format = SDL_GPU_VERTEXELEMENTFORMAT_FLOAT2; attrs[0].offset = 0;
    attrs[1].location = 1; attrs[1].buffer_slot = 0;
    attrs[1].format = SDL_GPU_VERTEXELEMENTFORMAT_FLOAT2; attrs[1].offset = 8;
    attrs[2].location = 2; attrs[2].buffer_slot = 0;
    attrs[2].format = SDL_GPU_VERTEXELEMENTFORMAT_FLOAT4; attrs[2].offset = 16;
    Uint32 nattr = 3;
    if (!textured) {
        attrs[3].location = 3; attrs[3].buffer_slot = 0;
        attrs[3].format = SDL_GPU_VERTEXELEMENTFORMAT_FLOAT3; attrs[3].offset = 32;
        nattr = 4;
    }

    SDL_GPUGraphicsPipelineCreateInfo pci;
    SDL_zero(pci);
    pci.vertex_shader = vs;
    pci.fragment_shader = fs;
    pci.vertex_input_state.vertex_buffer_descriptions = &vb;
    pci.vertex_input_state.num_vertex_buffers = 1;
    pci.vertex_input_state.vertex_attributes = attrs;
    pci.vertex_input_state.num_vertex_attributes = nattr;
    pci.primitive_type = SDL_GPU_PRIMITIVETYPE_TRIANGLELIST;
    /* target format must match the swapchain */
    SDL_GPUColorTargetDescription ctd;
    SDL_zero(ctd);
    ctd.format = SDL_GetGPUSwapchainTextureFormat(g_dev, g_win);
    ctd.blend_state = make_blend();
    pci.target_info.num_color_targets = 1;
    pci.target_info.color_target_descriptions = &ctd;
    *out = SDL_CreateGPUGraphicsPipeline(g_dev, &pci);
}

long long noui_gpu_window_create(const char *title, int w, int h) {
    if (!g_dev) return 0;
    g_win = SDL_CreateWindow(title, w, h, 0);
    if (!g_win) return 0;
    if (!SDL_ClaimWindowForGPUDevice(g_dev, g_win)) return 0;

    SDL_GPUShader *vq = load_shader(VS_QUAD_MSL, "vs_quad", SDL_GPU_SHADERSTAGE_VERTEX, 0);
    SDL_GPUShader *fq = load_shader(FRAG_QUAD_MSL, "fs_quad", SDL_GPU_SHADERSTAGE_FRAGMENT, 0);
    SDL_GPUShader *vt = load_shader(VS_TEX_MSL, "vs_tex", SDL_GPU_SHADERSTAGE_VERTEX, 0);
    SDL_GPUShader *ft = load_shader(FRAG_TEX_MSL, "fs_tex", SDL_GPU_SHADERSTAGE_FRAGMENT, 1);
    if (!vq || !fq || !vt || !ft) return 0;

    make_pipeline(vq, fq, 0, &g_pipe_quad);
    make_pipeline(vt, ft, 1, &g_pipe_tex);

    SDL_GPUSamplerCreateInfo si;
    SDL_zero(si);
    si.min_filter = SDL_GPU_FILTER_LINEAR;
    si.mag_filter = SDL_GPU_FILTER_LINEAR;
    si.mipmap_mode = SDL_GPU_SAMPLERMIPMAPMODE_NEAREST;
    si.address_mode_u = SDL_GPU_SAMPLERADDRESSMODE_CLAMP_TO_EDGE;
    si.address_mode_v = SDL_GPU_SAMPLERADDRESSMODE_CLAMP_TO_EDGE;
    si.address_mode_w = SDL_GPU_SAMPLERADDRESSMODE_CLAMP_TO_EDGE;
    g_smp = SDL_CreateGPUSampler(g_dev, &si);

    SDL_ReleaseGPUShader(g_dev, vq);
    SDL_ReleaseGPUShader(g_dev, fq);
    SDL_ReleaseGPUShader(g_dev, vt);
    SDL_ReleaseGPUShader(g_dev, ft);
    return g_pipe_quad && g_pipe_tex ? (long long)(void *)g_win : 0;
}

long long noui_gpu_window_get_size(void) {
    if (!g_win) return 0;
    int w = 0, h = 0;
    SDL_GetWindowSize(g_win, &w, &h);
    return ((long long)w << 32) | (unsigned int)h;
}

long long noui_gpu_display_scale1000(void) {
    if (!g_win) return 1000;
    float s = SDL_GetWindowDisplayScale(g_win);
    if (!(s > 0.0f)) return 1000;
    return (long long)(s * 1000.0f + 0.5f);
}

/* ============================================================================
 * 字体与纹理
 * ============================================================================ */
int noui_gpu_font_init(const char *path, double ptsize) {
    if (g_font) { TTF_CloseFont(g_font); g_font = NULL; }
    g_font = TTF_OpenFont(path, (float)ptsize);
    return g_font ? 1 : 0;
}

int noui_gpu_font_active(void) { return g_font ? 1 : 0; }

void noui_gpu_font_close(void) {
    if (g_font) { TTF_CloseFont(g_font); g_font = NULL; }
}

static SDL_GPUTexture *create_texture(const unsigned char *rgba, int w, int h) {
    if (!g_dev || !rgba || w <= 0 || h <= 0) return NULL;
    SDL_GPUTextureCreateInfo ti;
    SDL_zero(ti);
    ti.type = SDL_GPU_TEXTURETYPE_2D;
    ti.format = SDL_GPU_TEXTUREFORMAT_R8G8B8A8_UNORM;
    ti.usage = SDL_GPU_TEXTUREUSAGE_SAMPLER;
    ti.width = (Uint32)w;
    ti.height = (Uint32)h;
    ti.layer_count_or_depth = 1;
    ti.num_levels = 1;
    SDL_GPUTexture *tex = SDL_CreateGPUTexture(g_dev, &ti);
    if (!tex) return NULL;

    size_t bytes = (size_t)w * (size_t)h * 4;
    SDL_GPUTransferBufferCreateInfo tci;
    SDL_zero(tci);
    tci.usage = SDL_GPU_TRANSFERBUFFERUSAGE_UPLOAD;
    tci.size = (Uint32)bytes;
    SDL_GPUTransferBuffer *xfer = SDL_CreateGPUTransferBuffer(g_dev, &tci);
    if (!xfer) { SDL_ReleaseGPUTexture(g_dev, tex); return NULL; }
    void *dst = SDL_MapGPUTransferBuffer(g_dev, xfer, false);
    if (!dst) {
        SDL_ReleaseGPUTransferBuffer(g_dev, xfer);
        SDL_ReleaseGPUTexture(g_dev, tex);
        return NULL;
    }
    memcpy(dst, rgba, bytes);
    SDL_UnmapGPUTransferBuffer(g_dev, xfer);

    SDL_GPUCommandBuffer *cb = SDL_AcquireGPUCommandBuffer(g_dev);
    if (!cb) {
        SDL_ReleaseGPUTransferBuffer(g_dev, xfer);
        SDL_ReleaseGPUTexture(g_dev, tex);
        return NULL;
    }
    SDL_GPUTextureTransferInfo src = { xfer, 0, (Uint32)w, (Uint32)h };
    SDL_GPUTextureRegion dstr = { tex, 0, 0, 0, 0, 0, (Uint32)w, (Uint32)h, 1 };
    SDL_GPUCopyPass *cp = SDL_BeginGPUCopyPass(cb);
    SDL_UploadToGPUTexture(cp, &src, &dstr, false);
    SDL_EndGPUCopyPass(cp);
    SDL_SubmitGPUCommandBuffer(cb);
    SDL_ReleaseGPUTransferBuffer(g_dev, xfer);
    return tex;
}

/* 注册表：id = 下标 + 1（0 = 无效）。满了返回 0。 */
static long long reg_texture(SDL_GPUTexture *tex, int w, int h) {
    int i;
    if (!tex) return 0;
    for (i = 0; i < NOUI_GPU_TEX_MAX; i++) {
        if (!g_tex[i]) {
            g_tex[i] = tex;
            g_tex_w[i] = (Uint32)w;
            g_tex_h[i] = (Uint32)h;
            return (long long)i + 1;
        }
    }
    return 0;
}

static void drop_texture_at(int i) {
    if (i < 0 || i >= NOUI_GPU_TEX_MAX || !g_tex[i]) return;
    SDL_ReleaseGPUTexture(g_dev, g_tex[i]);
    g_tex[i] = NULL;
    g_tex_w[i] = 0;
    g_tex_h[i] = 0;
}

static unsigned char *read_file_all(const char *path, long *out_len) {
    FILE *f = fopen(path, "rb");
    if (!f) return NULL;
    if (fseek(f, 0, SEEK_END) != 0) { fclose(f); return NULL; }
    long n = ftell(f);
    if (n <= 0) { fclose(f); return NULL; }
    rewind(f);
    unsigned char *buf = (unsigned char *)malloc((size_t)n);
    if (!buf) { fclose(f); return NULL; }
    size_t got = fread(buf, 1, (size_t)n, f);
    fclose(f);
    if (got != (size_t)n) { free(buf); return NULL; }
    *out_len = n;
    return buf;
}

long long noui_gpu_img_load(const char *path) {
    if (!path || !g_dev) return 0;
    long len = 0;
    unsigned char *data = read_file_all(path, &len);
    if (!data) return 0;
    noui_img_t im;
    if (noui_png_decode(data, len, &im) != 0) { free(data); return 0; }
    free(data);
    SDL_GPUTexture *tex = create_texture(im.rgba, im.w, im.h);
    int w = im.w, h = im.h;
    noui_img_free(&im);
    if (!tex) return 0;
    long long id = reg_texture(tex, w, h);
    if (!id) { SDL_ReleaseGPUTexture(g_dev, tex); return 0; }
    return id;
}

long long noui_gpu_tex_size(long long id) {
    int i = (int)id - 1;
    if (i < 0 || i >= NOUI_GPU_TEX_MAX || !g_tex[i]) return 0;
    return ((long long)g_tex_w[i] << 32) | (unsigned int)g_tex_h[i];
}

static void text_cache_put(const char *text, long long id) {
    int i, victim = -1;
    for (i = 0; i < NOUI_GPU_TEXT_MAX; i++) {
        if (!g_texts[i].key) { victim = i; break; }
        if (victim < 0 || g_texts[i].tick < g_texts[victim].tick) victim = i;
    }
    if (victim < 0) return;
    if (g_texts[victim].key) {
        /* 淘汰：释放旧纹理（旧 id 若仍被本帧 op 引用也无妨，op 只在本帧内有效） */
        long long old = g_texts[victim].id;
        free(g_texts[victim].key);
        drop_texture_at((int)old - 1);
    }
    g_texts[victim].key = strdup(text);
    g_texts[victim].id = id;
    g_texts[victim].tick = ++g_tick;
}

/* 文字 -> 白底 alpha 蒙版纹理（带 LRU 缓存）。无字体返回 0。 */
long long noui_gpu_text_load(const char *text) {
    if (!text || !g_font || !g_dev) return 0;
    int i;
    for (i = 0; i < NOUI_GPU_TEXT_MAX; i++) {
        if (g_texts[i].key && strcmp(g_texts[i].key, text) == 0) {
            g_texts[i].tick = ++g_tick;
            return g_texts[i].id;
        }
    }
    SDL_Color fg = { 255, 255, 255, 255 };
    SDL_Surface *surf = TTF_RenderText_Blended(g_font, text, 0, fg);
    if (!surf) return 0;
    /* SDL 的 RGBA8888 在内存里是 A,B,G,R（小端），而 GPU R8G8B8A8_UNORM 期望
     * 字节序 R,G,B,A；SDL_PIXELFORMAT_ABGR8888 在小端机上正是 R,G,B,A。 */
    SDL_Surface *rgba = SDL_ConvertSurface(surf, SDL_PIXELFORMAT_ABGR8888);
    SDL_DestroySurface(surf);
    if (!rgba) return 0;
    SDL_GPUTexture *tex = create_texture((const unsigned char *)rgba->pixels,
                                         rgba->w, rgba->h);
    int w = rgba->w, h = rgba->h;
    SDL_DestroySurface(rgba);
    if (!tex) return 0;
    long long id = reg_texture(tex, w, h);
    if (!id) { SDL_ReleaseGPUTexture(g_dev, tex); return 0; }
    text_cache_put(text, id);
    return id;
}

/* ============================================================================
 * 指令入队（帧内累积，frame() 统一绘制）
 * ============================================================================ */
static void get_view(int *vw, int *vh) {
    *vw = 200; *vh = 150;
    if (g_win) SDL_GetWindowSize(g_win, vw, vh);
    if (*vw <= 0 || *vh <= 0) { *vw = 200; *vh = 150; }
}

static void ops_push(int kind, int start, long long tex) {
    if (g_on + 1 > g_ocap) {
        int cap = g_ocap ? g_ocap * 2 : 128;
        Op *nv = (Op *)realloc(g_ops, (size_t)cap * sizeof(Op));
        if (!nv) return;
        g_ops = nv; g_ocap = cap;
    }
    g_ops[g_on].kind = kind;
    g_ops[g_on].start = start;
    g_ops[g_on].tex = tex;
    g_on++;
}

static void quad_push(const double px[4], const double py[4], double w, double h,
                      double rad, int r, int g, int b, int a) {
    int vw, vh;
    get_view(&vw, &vh);
    if (g_qn + 6 > g_qcap) {
        int cap = g_qcap ? g_qcap * 2 : 256;
        while (cap < g_qn + 6) cap *= 2;
        QVert *nv = (QVert *)realloc(g_qv, (size_t)cap * sizeof(QVert));
        if (!nv) return;
        g_qv = nv; g_qcap = cap;
    }
    int st = g_qn;
    float cr = r / 255.0f, cg = g / 255.0f, cb = b / 255.0f, ca = a / 255.0f;
    float sx = (float)w, sy = (float)h, sr = (float)rad;
    QVert v[6];
    static const float U[6] = { 0.0f, 1.0f, 0.0f, 1.0f, 1.0f, 0.0f };
    static const float V[6] = { 0.0f, 0.0f, 1.0f, 0.0f, 1.0f, 1.0f };
    static const int MAP[6] = { 0, 1, 2, 1, 3, 2 };
    int k;
    for (k = 0; k < 6; k++) {
        int m = MAP[k];
        v[k].x = (float)(px[m] / vw * 2.0 - 1.0);
        v[k].y = (float)(1.0 - py[m] / vh * 2.0);
        v[k].u = U[k]; v[k].v = V[k];
        v[k].r = cr; v[k].g = cg; v[k].b = cb; v[k].a = ca;
        v[k].sx = sx; v[k].sy = sy; v[k].rad = sr;
    }
    memcpy(g_qv + g_qn, v, sizeof(v));
    g_qn += 6;
    ops_push(OP_QUAD, st, 0);
}

static void tex_push(long long id, double x, double y, double w, double h,
                     double angle, int flip, int r, int g, int b, int a) {
    int vw, vh;
    get_view(&vw, &vh);
    int i = (int)id - 1;
    if (i < 0 || i >= NOUI_GPU_TEX_MAX || !g_tex[i]) return;
    if (g_tn + 6 > g_tcap) {
        int cap = g_tcap ? g_tcap * 2 : 128;
        while (cap < g_tn + 6) cap *= 2;
        TVert *nv = (TVert *)realloc(g_tv, (size_t)cap * sizeof(TVert));
        if (!nv) return;
        g_tv = nv; g_tcap = cap;
    }
    int st = g_tn;
    double cx = x + w * 0.5, cy = y + h * 0.5;
    double rad = angle * 3.14159265358979323846 / 180.0;
    double cs = cos(rad), sn = sin(rad);
    double dx[4] = { x - cx, x + w - cx, x - cx, x + w - cx };
    double dy[4] = { y - cy, y - cy, y + h - cy, y + h - cy };
    double px[6], py[6];
    static const int MAP[6] = { 0, 1, 2, 1, 3, 2 };
    for (int k = 0; k < 6; k++) {
        int m = MAP[k];
        px[k] = cx + dx[m] * cs - dy[m] * sn;
        py[k] = cy + dx[m] * sn + dy[m] * cs;
    }
    float cr = r / 255.0f, cg = g / 255.0f, cb = b / 255.0f, caa = a / 255.0f;
    TVert v[6];
    static const float U[6] = { 0.0f, 1.0f, 0.0f, 1.0f, 1.0f, 0.0f };
    static const float V[6] = { 0.0f, 0.0f, 1.0f, 0.0f, 1.0f, 1.0f };
    for (int k = 0; k < 6; k++) {
        float u = U[k], vv = V[k];
        if (flip & 1) u = 1.0f - u;
        if (flip & 2) vv = 1.0f - vv;
        v[k].x = (float)(px[k] / vw * 2.0 - 1.0);
        v[k].y = (float)(1.0 - py[k] / vh * 2.0);
        v[k].u = u; v[k].v = vv;
        v[k].r = cr; v[k].g = cg; v[k].b = cb; v[k].a = caa;
    }
    memcpy(g_tv + g_tn, v, sizeof(v));
    g_tn += 6;
    ops_push(OP_TEX, st, id);
}

void noui_gpu_draw_rect(double x, double y, double w, double h,
                        int r, int g, int b, int a) {
    double px[4] = { x, x + w, x, x + w };
    double py[4] = { y, y, y + h, y + h };
    quad_push(px, py, w, h, 0.0, r, g, b, a);
}

void noui_gpu_draw_round(double x, double y, double w, double h, double rad,
                         int r, int g, int b, int a) {
    double px[4] = { x, x + w, x, x + w };
    double py[4] = { y, y, y + h, y + h };
    quad_push(px, py, w, h, rad, r, g, b, a);
}

void noui_gpu_draw_tex(double x, double y, double w, double h, long long id,
                       int r, int g, int b, int a) {
    tex_push(id, x, y, w, h, 0.0, 0, r, g, b, a);
}

void noui_gpu_draw_tex_rot(double x, double y, double w, double h, long long id,
                           double angle, int flip) {
    tex_push(id, x, y, w, h, angle, flip, 255, 255, 255, 255);
}

/* 投影：与软件后端同构，多层外扩的半透明圆角矩形叠加 */
void noui_gpu_draw_shadow(double x, double y, double w, double h, double rad,
                          double dy, int r, int g, int b, int a) {
    int layer;
    for (layer = 5; layer >= 0; layer--) {
        double t = (double)layer / 5.0;
        double grow = 2.0 + t * 4.0;
        int la = (int)((double)a * (1.0 - t) * 0.35);
        if (la <= 0) continue;
        noui_gpu_draw_round(x - grow, y - grow + dy, w + 2.0 * grow,
                            h + 2.0 * grow, rad + grow, r, g, b, la);
    }
}

/* ============================================================================
 * 帧绘制
 * ============================================================================ */
static SDL_GPUBuffer *upload_vertex_buffer(const void *data, size_t bytes,
                                           SDL_GPUCopyPass *cp) {
    SDL_GPUBufferCreateInfo bci;
    SDL_zero(bci);
    bci.usage = SDL_GPU_BUFFERUSAGE_VERTEX;
    bci.size = (Uint32)bytes;
    SDL_GPUBuffer *buf = SDL_CreateGPUBuffer(g_dev, &bci);
    if (!buf) return NULL;
    SDL_GPUTransferBufferCreateInfo tci;
    SDL_zero(tci);
    tci.usage = SDL_GPU_TRANSFERBUFFERUSAGE_UPLOAD;
    tci.size = (Uint32)bytes;
    SDL_GPUTransferBuffer *xfer = SDL_CreateGPUTransferBuffer(g_dev, &tci);
    if (!xfer) { SDL_ReleaseGPUBuffer(g_dev, buf); return NULL; }
    void *dst = SDL_MapGPUTransferBuffer(g_dev, xfer, false);
    if (!dst) {
        SDL_ReleaseGPUTransferBuffer(g_dev, xfer);
        SDL_ReleaseGPUBuffer(g_dev, buf);
        return NULL;
    }
    memcpy(dst, data, bytes);
    SDL_UnmapGPUTransferBuffer(g_dev, xfer);
    SDL_GPUTransferBufferLocation src = { xfer, 0 };
    SDL_GPUBufferRegion region = { buf, 0, (Uint32)bytes };
    SDL_UploadToGPUBuffer(cp, &src, &region, false);
    SDL_ReleaseGPUTransferBuffer(g_dev, xfer);
    return buf;
}

void noui_gpu_frame(void) {
    if (!g_dev || !g_win) return;
    SDL_GPUCommandBuffer *cb = SDL_AcquireGPUCommandBuffer(g_dev);
    if (!cb) return;
    SDL_GPUTexture *swap = NULL;
    Uint32 sw = 0, sh = 0;
    if (!SDL_AcquireGPUSwapchainTexture(cb, g_win, &swap, &sw, &sh)) return;
    if (!swap) { SDL_SubmitGPUCommandBuffer(cb); return; }

    /* 顶点数据一次性上传（四边形 + 纹理两个 buffer，同一个 copy pass） */
    SDL_GPUBuffer *qbuf = NULL, *tbuf = NULL;
    if (g_qn > 0 || g_tn > 0) {
        SDL_GPUCopyPass *cp = SDL_BeginGPUCopyPass(cb);
        if (g_qn > 0) qbuf = upload_vertex_buffer(g_qv, (size_t)g_qn * sizeof(QVert), cp);
        if (g_tn > 0) tbuf = upload_vertex_buffer(g_tv, (size_t)g_tn * sizeof(TVert), cp);
        SDL_EndGPUCopyPass(cp);
    }

    SDL_GPUColorTargetInfo ct;
    SDL_zero(ct);
    ct.texture = swap;
    ct.load_op = SDL_GPU_LOADOP_CLEAR;
    ct.store_op = SDL_GPU_STOREOP_STORE;
    ct.clear_color = (SDL_FColor){ g_clear_r, g_clear_g, g_clear_b, g_clear_a };
    SDL_GPURenderPass *pass = SDL_BeginGPURenderPass(cb, &ct, 1, NULL);

    int i = 0;
    while (i < g_on) {
        if (g_ops[i].kind == OP_QUAD) {
            int j = i;
            while (j < g_on && g_ops[j].kind == OP_QUAD) j++;
            if (qbuf) {
                SDL_BindGPUGraphicsPipeline(pass, g_pipe_quad);
                SDL_GPUBufferBinding bb = { qbuf, 0 };
                SDL_BindGPUVertexBuffers(pass, 0, &bb, 1);
                SDL_DrawGPUPrimitives(pass, (Uint32)((j - i) * 6), 1,
                                      (Uint32)g_ops[i].start, 0);
            }
            i = j;
        } else {
            int ti = (int)g_ops[i].tex - 1;
            if (tbuf && ti >= 0 && ti < NOUI_GPU_TEX_MAX && g_tex[ti]) {
                SDL_BindGPUGraphicsPipeline(pass, g_pipe_tex);
                SDL_GPUTextureSamplerBinding sb = { g_tex[ti], g_smp };
                SDL_BindGPUFragmentSamplers(pass, 0, &sb, 1);
                SDL_GPUBufferBinding bb = { tbuf, 0 };
                SDL_BindGPUVertexBuffers(pass, 0, &bb, 1);
                SDL_DrawGPUPrimitives(pass, 6, 1, (Uint32)g_ops[i].start, 0);
            }
            i++;
        }
    }

    SDL_EndGPURenderPass(pass);

    SDL_SubmitGPUCommandBuffer(cb);
    if (qbuf) SDL_ReleaseGPUBuffer(g_dev, qbuf);
    if (tbuf) SDL_ReleaseGPUBuffer(g_dev, tbuf);
    g_qn = 0; g_tn = 0; g_on = 0;
}

int noui_gpu_should_quit(void) {
    SDL_Event e;
    while (SDL_PollEvent(&e)) {
        if (e.type == SDL_EVENT_QUIT || (e.type == SDL_EVENT_KEY_DOWN && e.key.key == SDLK_ESCAPE)) g_quit = 1;
    }
    return g_quit;
}

void noui_gpu_delay(int ms) { SDL_Delay((Uint32)ms); }

void noui_gpu_quit(void) {
    int i;
    for (i = 0; i < NOUI_GPU_TEXT_MAX; i++) {
        free(g_texts[i].key);
        g_texts[i].key = NULL;
        g_texts[i].id = 0;
    }
    for (i = 0; i < NOUI_GPU_TEX_MAX; i++) {
        if (g_tex[i]) { SDL_ReleaseGPUTexture(g_dev, g_tex[i]); g_tex[i] = NULL; }
        g_tex_w[i] = 0;
        g_tex_h[i] = 0;
    }
    free(g_qv); g_qv = NULL; g_qn = g_qcap = 0;
    free(g_tv); g_tv = NULL; g_tn = g_tcap = 0;
    free(g_ops); g_ops = NULL; g_on = g_ocap = 0;
    if (g_font) { TTF_CloseFont(g_font); g_font = NULL; }
    if (g_smp) { SDL_ReleaseGPUSampler(g_dev, g_smp); g_smp = NULL; }
    if (g_pipe_quad) { SDL_ReleaseGPUGraphicsPipeline(g_dev, g_pipe_quad); g_pipe_quad = NULL; }
    if (g_pipe_tex) { SDL_ReleaseGPUGraphicsPipeline(g_dev, g_pipe_tex); g_pipe_tex = NULL; }
    if (g_win && g_dev) { SDL_ReleaseWindowFromGPUDevice(g_dev, g_win); }
    if (g_win) { SDL_DestroyWindow(g_win); g_win = NULL; }
    if (g_dev) { SDL_DestroyGPUDevice(g_dev); g_dev = NULL; }
    TTF_Quit();
    SDL_Quit();
}
