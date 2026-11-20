/*
 * noui SDL_GPU backend shim —— 用 SDL_GPU (Metal/Vulkan/DX12) 画彩色矩形。
 * 与 shim.c (SDL_Render 回退) 并列：此文件导出 noui_gpu_* 标量 FFI。
 * 顶点格式：position(vec2 f32) + color(vec4 f32) = 24 bytes/vertex。
 * 每个 draw-rect 追加 6 顶点（2 三角形）到 CPU 侧数组，frame() 一次性上传绘制。
 * MSL shader 以源码形式交给 SDL_CreateGPUShader (format=MSL)，macOS 走 Metal。
 */
#include <SDL3/SDL.h>
#include <stdlib.h>
#include <string.h>
#include "gpu_shim.h"

typedef struct { float x, y, r, g, b, a; } Vertex;

static SDL_GPUDevice *g_dev = NULL;
static SDL_Window *g_win = NULL;
static SDL_GPUGraphicsPipeline *g_pipe = NULL;
static SDL_GPUBuffer *g_vbuf = NULL;
static Vertex *g_verts = NULL;
static int g_vcount = 0;
static int g_vcap = 0;
static int g_quit = 0;

/* MSL：顶点直接把裁剪空间坐标与颜色传给 fragment */
static const char *VERT_MSL =
"#include <metal_stdlib>\n"
"using namespace metal;\n"
"struct VSIn { float2 pos [[attribute(0)]]; float4 color [[attribute(1)]]; };\n"
"struct VSOut { float4 pos [[position]]; float4 color; };\n"
"vertex VSOut vs_main(VSIn in [[stage_in]]) {\n"
"    VSOut o; o.pos = float4(in.pos, 0.0, 1.0); o.color = in.color; return o;\n"
"}\n";

static const char *FRAG_MSL =
"#include <metal_stdlib>\n"
"using namespace metal;\n"
"struct VSOut { float4 pos [[position]]; float4 color; };\n"
"fragment float4 fs_main(VSOut in [[stage_in]]) { return in.color; }\n";

static SDL_GPUShader *load_shader(const char *src, SDL_GPUShaderStage stage) {
    SDL_GPUShaderCreateInfo ci;
    SDL_zero(ci);
    ci.code_size = strlen(src);
    ci.code = (const Uint8 *)src;
    ci.entrypoint = (stage == SDL_GPU_SHADERSTAGE_VERTEX) ? "vs_main" : "fs_main";
    ci.format = SDL_GPU_SHADERFORMAT_MSL;
    ci.stage = stage;
    return SDL_CreateGPUShader(g_dev, &ci);
}

int noui_gpu_init(void) {
    g_quit = 0;
    if (!SDL_Init(SDL_INIT_VIDEO)) return 0;
    g_dev = SDL_CreateGPUDevice(SDL_GPU_SHADERFORMAT_MSL, true, NULL);
    return g_dev ? 1 : 0;
}

long long noui_gpu_window_create(const char *title, int w, int h) {
    if (!g_dev) return 0;
    g_win = SDL_CreateWindow(title, w, h, 0);
    if (!g_win) return 0;
    if (!SDL_ClaimWindowForGPUDevice(g_dev, g_win)) return 0;

    SDL_GPUShader *vs = load_shader(VERT_MSL, SDL_GPU_SHADERSTAGE_VERTEX);
    SDL_GPUShader *fs = load_shader(FRAG_MSL, SDL_GPU_SHADERSTAGE_FRAGMENT);
    if (!vs || !fs) return 0;

    SDL_GPUVertexBufferDescription vb = { 0, sizeof(Vertex), SDL_GPU_VERTEXINPUTRATE_VERTEX, 0 };
    SDL_GPUVertexAttribute attrs[2];
    attrs[0].location = 0; attrs[0].buffer_slot = 0;
    attrs[0].format = SDL_GPU_VERTEXELEMENTFORMAT_FLOAT2; attrs[0].offset = 0;
    attrs[1].location = 1; attrs[1].buffer_slot = 0;
    attrs[1].format = SDL_GPU_VERTEXELEMENTFORMAT_FLOAT4; attrs[1].offset = 8;

    SDL_GPUGraphicsPipelineCreateInfo pci;
    SDL_zero(pci);
    pci.vertex_shader = vs;
    pci.fragment_shader = fs;
    pci.vertex_input_state.vertex_buffer_descriptions = &vb;
    pci.vertex_input_state.num_vertex_buffers = 1;
    pci.vertex_input_state.vertex_attributes = attrs;
    pci.vertex_input_state.num_vertex_attributes = 2;
    pci.primitive_type = SDL_GPU_PRIMITIVETYPE_TRIANGLELIST;
    /* target format must match the swapchain */
    SDL_GPUColorTargetDescription ctd;
    SDL_zero(ctd);
    ctd.format = SDL_GetGPUSwapchainTextureFormat(g_dev, g_win);
    pci.target_info.num_color_targets = 1;
    pci.target_info.color_target_descriptions = &ctd;
    g_pipe = SDL_CreateGPUGraphicsPipeline(g_dev, &pci);
    SDL_ReleaseGPUShader(g_dev, vs);
    SDL_ReleaseGPUShader(g_dev, fs);
    return g_pipe ? (long long)(void *)g_win : 0;
}

void noui_gpu_draw_rect(float x, float y, float w, float h, int r, int g, int b, int a) {
    if (g_vcount + 6 > g_vcap) {
        int cap = g_vcap ? g_vcap * 2 : 256;
        while (cap < g_vcount + 6) cap *= 2;
        Vertex *nv = (Vertex *)realloc(g_verts, (size_t)cap * sizeof(Vertex));
        if (!nv) return;
        g_verts = nv; g_vcap = cap;
    }
    float nx = x / 200.0f - 1.0f;
    float ny = 1.0f - y / 150.0f;
    float nw = w / 200.0f;
    float nh = h / 150.0f;
    float cr = r / 255.0f, cg = g / 255.0f, cb = b / 255.0f, ca = a / 255.0f;
    Vertex v[6] = {
        { nx,      ny,      cr,cg,cb,ca },
        { nx + nw, ny,      cr,cg,cb,ca },
        { nx,      ny - nh, cr,cg,cb,ca },
        { nx + nw, ny,      cr,cg,cb,ca },
        { nx + nw, ny - nh, cr,cg,cb,ca },
        { nx,      ny - nh, cr,cg,cb,ca },
    };
    memcpy(g_verts + g_vcount, v, sizeof(v));
    g_vcount += 6;
}

void noui_gpu_frame(void) {
    if (!g_dev || !g_win || !g_pipe) return;
    SDL_GPUCommandBuffer *cb = SDL_AcquireGPUCommandBuffer(g_dev);
    if (!cb) return;
    SDL_GPUTexture *swap = NULL;
    Uint32 sw = 0, sh = 0;
    if (!SDL_AcquireGPUSwapchainTexture(cb, g_win, &swap, &sw, &sh)) { return; }
    if (!swap) { SDL_SubmitGPUCommandBuffer(cb); return; }

    /* upload vertices via transfer buffer */
    SDL_GPUBuffer *vbuf = NULL;
    if (g_vcount > 0) {
        size_t bytes = (size_t)g_vcount * sizeof(Vertex);
        SDL_GPUBufferCreateInfo bci; SDL_zero(bci);
        bci.usage = SDL_GPU_BUFFERUSAGE_VERTEX;
        bci.size = (Uint32)bytes;
        vbuf = SDL_CreateGPUBuffer(g_dev, &bci);
        SDL_GPUTransferBufferCreateInfo tci; SDL_zero(tci);
        tci.usage = SDL_GPU_TRANSFERBUFFERUSAGE_UPLOAD;
        tci.size = (Uint32)bytes;
        SDL_GPUTransferBuffer *xfer = SDL_CreateGPUTransferBuffer(g_dev, &tci);
        void *dst = SDL_MapGPUTransferBuffer(g_dev, xfer, false);
        memcpy(dst, g_verts, bytes);
        SDL_UnmapGPUTransferBuffer(g_dev, xfer);
        SDL_GPUCommandBuffer *ucb = SDL_AcquireGPUCommandBuffer(g_dev);
        SDL_GPUTransferBufferLocation src = { xfer, 0 };
        SDL_GPUBufferRegion region = { vbuf, 0, (Uint32)bytes };
        SDL_GPUCopyPass *cp = SDL_BeginGPUCopyPass(ucb);
        SDL_UploadToGPUBuffer(cp, &src, &region, false);
        SDL_EndGPUCopyPass(cp);
        SDL_SubmitGPUCommandBuffer(ucb);
        SDL_ReleaseGPUTransferBuffer(g_dev, xfer);
    }

    SDL_GPUColorTargetInfo ct; SDL_zero(ct);
    ct.texture = swap;
    ct.load_op = SDL_GPU_LOADOP_CLEAR;
    ct.store_op = SDL_GPU_STOREOP_STORE;
    ct.clear_color = (SDL_FColor){ 0.12f, 0.12f, 0.16f, 1.0f };
    SDL_GPURenderPass *pass = SDL_BeginGPURenderPass(cb, &ct, 1, NULL);
    if (g_vcount > 0 && vbuf) {
        SDL_BindGPUGraphicsPipeline(pass, g_pipe);
        SDL_GPUBufferBinding bb = { vbuf, 0 };
        SDL_BindGPUVertexBuffers(pass, 0, &bb, 1);
        SDL_DrawGPUPrimitives(pass, (Uint32)g_vcount, 1, 0, 0);
    }
    SDL_EndGPURenderPass(pass);
    SDL_SubmitGPUCommandBuffer(cb);
    if (vbuf) SDL_ReleaseGPUBuffer(g_dev, vbuf);
    g_vcount = 0;
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
    free(g_verts); g_verts = NULL; g_vcount = g_vcap = 0;
    if (g_pipe) { SDL_ReleaseGPUGraphicsPipeline(g_dev, g_pipe); g_pipe = NULL; }
    if (g_win && g_dev) { SDL_ReleaseWindowFromGPUDevice(g_dev, g_win); }
    if (g_win) { SDL_DestroyWindow(g_win); g_win = NULL; }
    if (g_dev) { SDL_DestroyGPUDevice(g_dev); g_dev = NULL; }
    SDL_Quit();
}
