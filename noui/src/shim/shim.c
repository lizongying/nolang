/*
 * noui platform shim —— 把 SDL3 的 C API 摊平成标量函数供 Nolang #{c} 调用。
 * 设计约束（实测）：Nolang FFI 支持 i64/i32/f64/str/ptr，不支持结构体按值传参。
 * 故所有 SDL 结构体（SDL_FRect / SDL_Event）在此文件内部构造，Nolang 只传标量。
 */
#include <SDL3/SDL.h>
#include <SDL3_ttf/SDL_ttf.h>
#include "shim.h"

static SDL_Window *g_window = NULL;
static SDL_Renderer *g_renderer = NULL;
static TTF_Font *g_font = NULL;
static int g_quit = 0;

int noui_init(void) {
    g_quit = 0;
    if (!SDL_Init(SDL_INIT_VIDEO)) return 0;
    if (!TTF_Init()) return 0;
    return 1;
}

long long noui_window_create(const char *title, int w, int h) {
    g_window = SDL_CreateWindow(title, w, h, 0);
    if (!g_window) return 0;
    g_renderer = SDL_CreateRenderer(g_window, NULL);
    if (!g_renderer) return 0;
    return (long long)(void *)g_window;
}

void noui_quit(void) {
    if (g_font) { TTF_CloseFont(g_font); g_font = NULL; }
    if (g_renderer) { SDL_DestroyRenderer(g_renderer); g_renderer = NULL; }
    if (g_window) { SDL_DestroyWindow(g_window); g_window = NULL; }
    TTF_Quit();
    SDL_Quit();
}

void noui_begin_frame(int r, int g, int b, int a) {
    if (!g_renderer) return;
    SDL_SetRenderDrawColor(g_renderer, (Uint8)r, (Uint8)g, (Uint8)b, (Uint8)a);
    SDL_RenderClear(g_renderer);
}

void noui_draw_rect(float x, float y, float w, float h, int r, int g, int b, int a) {
    if (!g_renderer) return;
    SDL_FRect rect;
    rect.x = x; rect.y = y; rect.w = w; rect.h = h;
    SDL_SetRenderDrawColor(g_renderer, (Uint8)r, (Uint8)g, (Uint8)b, (Uint8)a);
    SDL_RenderFillRect(g_renderer, &rect);
}

/* 字体 shaping：SDL_ttf 3.x 内部用 HarfBuzz 做 shaping；渲染成 surface -> texture -> 绘制。
 * 保留旧 noui_draw_text 作为 DebugText 回退（无字体时可用）。 */
void noui_draw_text(float x, float y, const char *text) {
    if (!g_renderer) return;
    if (g_font) {
        noui_font_draw(x, y, 255, 255, 255, 255, text);
        return;
    }
    SDL_RenderDebugText(g_renderer, x, y, text);
}

int noui_font_init(const char *path, double ptsize) {
    if (g_font) { TTF_CloseFont(g_font); g_font = NULL; }
    g_font = TTF_OpenFont(path, (float)ptsize);
    return g_font ? 1 : 0;
}

void noui_font_close(void) {
    if (g_font) { TTF_CloseFont(g_font); g_font = NULL; }
}

void noui_font_draw(double x, double y, int r, int g, int b, int a, const char *text) {
    if (!g_renderer || !g_font || !text) return;
    SDL_Color fg = { (Uint8)r, (Uint8)g, (Uint8)b, (Uint8)a };
    SDL_Surface *surf = TTF_RenderText_Blended(g_font, text, 0, fg);
    if (!surf) return;
    SDL_Texture *tex = SDL_CreateTextureFromSurface(g_renderer, surf);
    SDL_DestroySurface(surf);
    if (!tex) return;
    float tw = 0.0f, th = 0.0f;
    SDL_GetTextureSize(tex, &tw, &th);
    SDL_FRect dst = { (float)x, (float)y, tw, th };
    SDL_RenderTexture(g_renderer, tex, NULL, &dst);
    SDL_DestroyTexture(tex);
}

void noui_set_color(int r, int g, int b, int a) {
    if (!g_renderer) return;
    SDL_SetRenderDrawColor(g_renderer, (Uint8)r, (Uint8)g, (Uint8)b, (Uint8)a);
}

void noui_end_frame(void) {
    if (!g_renderer) return;
    SDL_RenderPresent(g_renderer);
}

/* 阶段23：滚动视口裁剪。坐标是逻辑像素（调用方已把定点 /100）。
 * w<=0 或 h<=0 视为清除，避免 SDL 对空矩形的未定义行为。 */
void noui_clip(int x, int y, int w, int h) {
    if (!g_renderer) return;
    if (w <= 0 || h <= 0) { noui_clip_clear(); return; }
    SDL_Rect rect;
    rect.x = x; rect.y = y; rect.w = w; rect.h = h;
    SDL_SetRenderClipRect(g_renderer, &rect);
}

void noui_clip_clear(void) {
    if (!g_renderer) return;
    SDL_SetRenderClipRect(g_renderer, NULL);
}

/* 事件摊平：编码进一个 i64 返回。
 * 返回 0=无事件；否则低 8 位=事件类型，中 24 位=x 坐标(int)，高 24 位=y 坐标(int)。
 * 按键事件把 key code 放 x 槽。滚轮额外用 bit56..63 带符号增量（见下）。
 * 这样 Nolang 只需处理标量，避开输出指针。 */
long long noui_poll(void) {
    SDL_Event e;
    if (!SDL_PollEvent(&e)) return 0;
    long long type = 0, x = 0, y = 0, d = 0;
    switch (e.type) {
        case SDL_EVENT_QUIT:
            g_quit = 1;
            type = 1;
            break;
        case SDL_EVENT_MOUSE_BUTTON_DOWN:
            if (e.button.button == SDL_BUTTON_LEFT) {
                type = 2; x = (long long)e.button.x; y = (long long)e.button.y;
            } else {
                return 0;
            }
            break;
        /* 阶段29：左键抬起。拖分栏（Dock）需要「按下抓住 -> 移动 -> 松开放手」
         * 的完整三段，缺了抬起就只能靠再次按下结束拖拽。坐标槽与按下一致。 */
        case SDL_EVENT_MOUSE_BUTTON_UP:
            if (e.button.button == SDL_BUTTON_LEFT) {
                type = 6; x = (long long)e.button.x; y = (long long)e.button.y;
            } else {
                return 0;
            }
            break;
        case SDL_EVENT_MOUSE_MOTION:
            type = 3; x = (long long)e.motion.x; y = (long long)e.motion.y;
            break;
        /* 阶段23：滚轮。x/y 槽放光标位置（命中要用），增量走 integer_y
         * （整格 tick，SDL3 >= 3.2.12），夹到 int8 放 bit56..63 —— 24 位掩码装不下负数。 */
        case SDL_EVENT_MOUSE_WHEEL: {
            type = 5;
            x = (long long)e.wheel.mouse_x;
            y = (long long)e.wheel.mouse_y;
            long long w = (long long)e.wheel.integer_y;
            if (w > 127) w = 127;
            if (w < -128) w = -128;
            d = w;
            break;
        }
        case SDL_EVENT_KEY_DOWN:
            type = 4; x = (long long)e.key.key;
            if (e.key.key == SDLK_ESCAPE) g_quit = 1;
            break;
        default:
            return 0;
    }
    /* 每槽限制 24 位，避免相互覆盖 */
    x &= 0xFFFFFF;
    y &= 0xFFFFFF;
    return type | (x << 8) | (y << 32) | ((d & 0xFF) << 56);
}

int noui_should_quit(void) { return g_quit; }
void noui_delay(int ms) { SDL_Delay((Uint32)ms); }

const char *noui_version(void) {
    static char buf[32];
    int v = SDL_GetVersion();
    SDL_snprintf(buf, sizeof(buf), "%d.%d.%d",
        SDL_VERSIONNUM_MAJOR(v), SDL_VERSIONNUM_MINOR(v), SDL_VERSIONNUM_MICRO(v));
    return buf;
}
