/*
 * noui platform shim —— 把 SDL3 的 C API 摊平成标量函数供 Nolang #{c} 调用。
 * 设计约束（实测）：Nolang FFI 支持 i64/i32/f64/str/ptr，不支持结构体按值传参。
 * 故所有 SDL 结构体（SDL_FRect / SDL_Event）在此文件内部构造，Nolang 只传标量。
 */
#include <SDL3/SDL.h>
#include <SDL3_ttf/SDL_ttf.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <math.h>
#include "shim.h"
#include "img_png.h"

/* ==========================================================================
 * 阶段80：多窗口。窗口注册表 + 「attach」语义：全局 g_window / g_renderer
 * 始终指向当前 attach 的槽位，所有既有 setter/getter/绘制函数因此无需改签名
 * 就能逐窗工作。窗口 id 为 1 基槽位号（0 = 失败 / 无）。
 * ========================================================================== */
#define NOUI_WIN_MAX 8
static SDL_Window *g_windows[NOUI_WIN_MAX];
static SDL_Renderer *g_renderers[NOUI_WIN_MAX];
static Uint32 g_sdl_ids[NOUI_WIN_MAX];   /* SDL windowID -> 事件路由用 */
static int g_attached = -1;              /* 当前 g_window/g_renderer 指向的槽位 */
static int g_event_win = 0;              /* 最近一次事件所属窗口（1 基；0=未知） */

static SDL_Window *g_window = NULL;
static SDL_Renderer *g_renderer = NULL;
static TTF_Font *g_font = NULL;
static int g_quit = 0;
static long long g_frames = 0;      /* noui_end_frame 调用次数（测试闸用） */
static long long g_max_frames = 0;  /* NOUI_MAX_FRAMES，0 = 不限 */
static char g_text_buf[256];        /* 最近一次 SDL_EVENT_TEXT_INPUT 的 UTF-8 */
static char g_drop_buf[1024];       /* 阶段81：最近一次 DROP_FILE / DROP_TEXT */

/* 阶段81：事件类型码（低 8 位），与 src/app.no 头部注释一致。 */
#define NOUI_EV_QUIT            1
#define NOUI_EV_LDOWN           2
#define NOUI_EV_MOVE            3
#define NOUI_EV_KEYDOWN         4
#define NOUI_EV_WHEEL           5
#define NOUI_EV_LUP             6
#define NOUI_EV_TEXT            7
#define NOUI_EV_RDOWN           8
#define NOUI_EV_RUP             9
#define NOUI_EV_MDOWN           10
#define NOUI_EV_MUP             11
#define NOUI_EV_KEYUP           12
#define NOUI_EV_FINGER_DOWN     13
#define NOUI_EV_FINGER_UP       14
#define NOUI_EV_FINGER_MOTION   15
#define NOUI_EV_PEN_DOWN        16
#define NOUI_EV_PEN_UP          17
#define NOUI_EV_PEN_MOTION      18
#define NOUI_EV_DROP_FILE       19
#define NOUI_EV_DROP_POS        20
#define NOUI_EV_DROP_BEGIN      21
#define NOUI_EV_DROP_COMPLETE   22
#define NOUI_EV_PEN_AXIS        23
#define NOUI_EV_DROP_TEXT       24

/* 修饰键位（aux 字节 bit0..3）。 */
#define NOUI_MOD_SHIFT 1
#define NOUI_MOD_CTRL  2
#define NOUI_MOD_ALT   4
#define NOUI_MOD_META  8

/* 打包一个事件进 i64：type | x<<8 | y<<32 | aux<<56，各槽限 24 位。 */
static long long ev_pack(int type, long long x, long long y, int aux) {
    if (x < 0) x = 0;
    if (y < 0) y = 0;
    if (x > 0xFFFFFF) x = 0xFFFFFF;
    if (y > 0xFFFFFF) y = 0xFFFFFF;
    return (long long)type | (x << 8) | (y << 32) |
           (((long long)(aux & 0xFF)) << 56);
}

/* 当前全局修饰键状态 -> aux 位掩码。 */
static int mods_byte(void) {
    SDL_Keymod m = SDL_GetModState();
    int b = 0;
    if (m & SDL_KMOD_SHIFT) b |= NOUI_MOD_SHIFT;
    if (m & SDL_KMOD_CTRL)  b |= NOUI_MOD_CTRL;
    if (m & SDL_KMOD_ALT)   b |= NOUI_MOD_ALT;
    if (m & SDL_KMOD_GUI)   b |= NOUI_MOD_META;
    return b;
}

/* 归一化量（0..1）-> 0..255，越界夹紧。 */
static int quant_unit(float v) {
    if (!(v > 0.0f)) return 0;
    if (v >= 1.0f) return 255;
    return (int)(v * 255.0f + 0.5f);
}

/* 把 g_window / g_renderer 指到指定槽位（-1 = 都不指）。 */
static void win_attach(int slot) {
    g_attached = slot;
    g_window = (slot >= 0) ? g_windows[slot] : NULL;
    g_renderer = (slot >= 0) ? g_renderers[slot] : NULL;
}

/* 由 SDL windowID 反查槽位；未知返回 -1。 */
static int win_slot_of(Uint32 sdl_id) {
    int i;
    for (i = 0; i < NOUI_WIN_MAX; i++) {
        if (g_windows[i] && g_sdl_ids[i] == sdl_id) return i;
    }
    return -1;
}

/* 阶段78：已加载纹理注册表（id = 下标 + 1，0 = 无效）。
 * 阶段80：存的是与 renderer 无关的 SDL_Surface；SDL_Texture 按窗口槽位缓存
 * （纹理属于创建它的 renderer，跨窗复用会坏）。 */
#define NOUI_IMG_MAX 64
static SDL_Surface *g_imgs[NOUI_IMG_MAX];
static int g_img_w[NOUI_IMG_MAX];
static int g_img_h[NOUI_IMG_MAX];
static SDL_Texture *g_img_tex[NOUI_WIN_MAX][NOUI_IMG_MAX];

static void win_tex_purge_slot(int slot);
static void win_tex_purge_all(void);
static SDL_Texture *win_img_tex(long long id);

int noui_init(void) {
    g_quit = 0;
    g_frames = 0;
    g_text_buf[0] = '\0';
    const char *mf = getenv("NOUI_MAX_FRAMES");
    if (mf && *mf) g_max_frames = strtoll(mf, NULL, 10);
    if (!SDL_Init(SDL_INIT_VIDEO)) return 0;
    if (!TTF_Init()) return 0;
    return 1;
}

long long noui_window_create(const char *title, int w, int h) {
    int i;
    for (i = 0; i < NOUI_WIN_MAX; i++) {
        if (g_windows[i]) continue;
        g_windows[i] = SDL_CreateWindow(title, w, h, 0);
        if (!g_windows[i]) return 0;
        g_renderers[i] = SDL_CreateRenderer(g_windows[i], NULL);
        if (!g_renderers[i]) {
            SDL_DestroyWindow(g_windows[i]);
            g_windows[i] = NULL;
            return 0;
        }
        g_sdl_ids[i] = SDL_GetWindowID(g_windows[i]);
        win_attach(i);          /* 新窗成为当前窗（沿用「最近建的就是主窗」语义） */
        return (long long)i + 1;
    }
    return 0;                   /* 槽位满 */
}

/* 当前 attach 到指定窗口（1 基 id）。返回 1=成功，0=无效 id。 */
int noui_window_attach(long long id) {
    int slot = (int)id - 1;
    if (slot < 0 || slot >= NOUI_WIN_MAX || !g_windows[slot]) return 0;
    win_attach(slot);
    return 1;
}

/* 已打开的窗口数量。 */
int noui_window_count(void) {
    int i, n = 0;
    for (i = 0; i < NOUI_WIN_MAX; i++) if (g_windows[i]) n++;
    return n;
}

/* 关闭指定窗口（1 基 id）。返回 1=成功，0=无效 id。
 * 关闭后 attach 到第一个还活着的窗；都关了就不 attach。 */
int noui_window_close(long long id) {
    int slot = (int)id - 1;
    if (slot < 0 || slot >= NOUI_WIN_MAX || !g_windows[slot]) return 0;
    if (g_renderers[slot]) { SDL_DestroyRenderer(g_renderers[slot]); g_renderers[slot] = NULL; }
    SDL_DestroyWindow(g_windows[slot]);
    g_windows[slot] = NULL;
    g_sdl_ids[slot] = 0;
    win_tex_purge_slot(slot);
    if (g_attached == slot) {
        int i;
        for (i = 0; i < NOUI_WIN_MAX; i++) {
            if (g_windows[i]) { win_attach(i); return 1; }
        }
        win_attach(-1);
    }
    return 1;
}

/* 最近一次 noui_poll 事件所属的窗口（1 基；0=未知 / 非窗口事件）。 */
int noui_window_event(void) { return g_event_win; }

void noui_quit(void) {
    int i;
    for (i = 0; i < NOUI_IMG_MAX; i++) {
        if (g_imgs[i]) { SDL_DestroySurface(g_imgs[i]); g_imgs[i] = NULL; }
        g_img_w[i] = 0;
        g_img_h[i] = 0;
    }
    win_tex_purge_all();
    if (g_font) { TTF_CloseFont(g_font); g_font = NULL; }
    for (i = 0; i < NOUI_WIN_MAX; i++) {
        if (g_renderers[i]) { SDL_DestroyRenderer(g_renderers[i]); g_renderers[i] = NULL; }
        if (g_windows[i]) { SDL_DestroyWindow(g_windows[i]); g_windows[i] = NULL; }
        g_sdl_ids[i] = 0;
    }
    win_attach(-1);
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
    /* 测试闸：NOUI_MAX_FRAMES=N（>0）时第 N 帧后置 g_quit，让「等到退出事件」的
     * 窗口 demo 在 dummy 驱动下也能确定性收尾（dummy 不投递 SDL_EVENT_QUIT）。
     * 默认 0 = 不限，交互使用行为不变。 */
    if (g_max_frames > 0) {
        g_frames++;
        if (g_frames >= g_max_frames) g_quit = 1;
    }
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

/* ==========================================================================
 * 阶段52：窗口选项（C-WINDOW-EXT 直接驱动真窗口）
 * 全部标量进出；查询类把宽 / 高打包进一个 i64（w << 32 | h），flags 原样返回。
 * 未建窗口时一律空操作（组件逻辑测试会在没有窗口的情况下跑）。
 * ========================================================================== */
void noui_window_set_title(const char *title) {
    if (g_window && title) SDL_SetWindowTitle(g_window, title);
}

void noui_window_set_resizable(int yes) {
    if (g_window) SDL_SetWindowResizable(g_window, yes ? true : false);
}

void noui_window_set_min_size(int w, int h) {
    if (g_window) SDL_SetWindowMinimumSize(g_window, w, h);
}

void noui_window_set_max_size(int w, int h) {
    if (g_window) SDL_SetWindowMaximumSize(g_window, w, h);
}

void noui_window_set_fullscreen(int yes) {
    if (g_window) SDL_SetWindowFullscreen(g_window, yes ? true : false);
}

void noui_window_set_always_on_top(int yes) {
    if (g_window) SDL_SetWindowAlwaysOnTop(g_window, yes ? true : false);
}

void noui_window_set_bordered(int yes) {
    if (g_window) SDL_SetWindowBordered(g_window, yes ? true : false);
}

void noui_window_set_opacity(double o) {
    if (!g_window) return;
    if (o < 0.0) o = 0.0;
    if (o > 1.0) o = 1.0;
    SDL_SetWindowOpacity(g_window, (float)o);
}

void noui_window_set_size(int w, int h) {
    if (g_window) SDL_SetWindowSize(g_window, w, h);
}

long long noui_window_get_size(void) {
    if (!g_window) return 0;
    int w = 0, h = 0;
    SDL_GetWindowSize(g_window, &w, &h);
    return ((long long)w << 32) | (unsigned int)h;
}

long long noui_window_get_flags(void) {
    if (!g_window) return 0;
    return (long long)SDL_GetWindowFlags(g_window);
}

const char *noui_window_get_title(void) {
    if (!g_window) return "";
    const char *t = SDL_GetWindowTitle(g_window);
    return t ? t : "";
}

/* 事件摊平：编码进一个 i64 返回。
 * 返回 0=无事件；否则低 8 位=事件类型，中 24 位=x 坐标(int)，高 24 位=y 坐标(int)。
 * 按键事件把 key code 放 x 槽。滚轮额外用 bit56..63 带符号增量（见下）。
 * 这样 Nolang 只需处理标量，避开输出指针。 */
/* 事件携带的 SDL windowID（不同事件类型字段名不同，统一取一次）。 */
static Uint32 ev_window_id(const SDL_Event *e) {
    switch (e->type) {
        case SDL_EVENT_MOUSE_BUTTON_DOWN:
        case SDL_EVENT_MOUSE_BUTTON_UP:
            return e->button.windowID;
        case SDL_EVENT_MOUSE_MOTION:
            return e->motion.windowID;
        case SDL_EVENT_MOUSE_WHEEL:
            return e->wheel.windowID;
        case SDL_EVENT_KEY_DOWN:
        case SDL_EVENT_KEY_UP:
            return e->key.windowID;
        case SDL_EVENT_TEXT_INPUT:
            return e->text.windowID;
        /* 阶段81：触摸 / 笔 / 拖放也带窗口 id */
        case SDL_EVENT_FINGER_DOWN:
        case SDL_EVENT_FINGER_UP:
        case SDL_EVENT_FINGER_MOTION:
        case SDL_EVENT_FINGER_CANCELED:
            return e->tfinger.windowID;
        case SDL_EVENT_PEN_DOWN:
        case SDL_EVENT_PEN_UP:
            return e->ptouch.windowID;
        case SDL_EVENT_PEN_MOTION:
            return e->pmotion.windowID;
        case SDL_EVENT_PEN_AXIS:
            return e->paxis.windowID;
        case SDL_EVENT_DROP_FILE:
        case SDL_EVENT_DROP_TEXT:
        case SDL_EVENT_DROP_BEGIN:
        case SDL_EVENT_DROP_COMPLETE:
        case SDL_EVENT_DROP_POSITION:
            return e->drop.windowID;
        default:
            return 0;
    }
}

long long noui_poll(void) {
    SDL_Event e;
    if (!SDL_PollEvent(&e)) return 0;
    /* 阶段80：记录事件来自哪个窗口（未知窗口保持上次值） */
    Uint32 wid = ev_window_id(&e);
    if (wid) {
        int ws = win_slot_of(wid);
        g_event_win = (ws >= 0) ? ws + 1 : 0;
    }
    long long type = 0, x = 0, y = 0, d = 0;
    switch (e.type) {
        case SDL_EVENT_QUIT:
            g_quit = 1;
            type = NOUI_EV_QUIT;
            break;
        /* 阶段81：左 / 右 / 中键的按下与抬起统一编码；修饰键走 aux 字节。 */
        case SDL_EVENT_MOUSE_BUTTON_DOWN:
        case SDL_EVENT_MOUSE_BUTTON_UP: {
            int up = (e.type == SDL_EVENT_MOUSE_BUTTON_UP);
            switch (e.button.button) {
                case SDL_BUTTON_LEFT:   type = up ? NOUI_EV_LUP  : NOUI_EV_LDOWN;  break;
                case SDL_BUTTON_RIGHT:  type = up ? NOUI_EV_RUP  : NOUI_EV_RDOWN;  break;
                default:                type = up ? NOUI_EV_MUP  : NOUI_EV_MDOWN;  break;
            }
            if (e.button.button != SDL_BUTTON_LEFT &&
                e.button.button != SDL_BUTTON_RIGHT &&
                e.button.button != SDL_BUTTON_MIDDLE) return 0;
            x = (long long)e.button.x;
            y = (long long)e.button.y;
            d = mods_byte();
            break;
        }
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
        /* 阶段81：key-down 与 key-up 同构：键码拆 x(低 24) + y(高 8)，
         * 修饰键状态整体走 aux 字节（Shift/Ctrl/Alt/Meta）。 */
        case SDL_EVENT_KEY_DOWN:
        case SDL_EVENT_KEY_UP: {
            type = (e.type == SDL_EVENT_KEY_DOWN) ? NOUI_EV_KEYDOWN : NOUI_EV_KEYUP;
            long long kc = (long long)e.key.key;
            x = kc & 0xFFFFFF;
            y = (kc >> 24) & 0xFF;
            d = mods_byte();
            if (e.type == SDL_EVENT_KEY_DOWN && kc == SDLK_ESCAPE) g_quit = 1;
            break;
        }
        /* 阶段76：文本输入（IME 组字结果、Unicode）。字符串塞不进 i64 事件码，
         * 先存内部缓冲，由 noui_text_take() 取走；坐标槽留 0。 */
        case SDL_EVENT_TEXT_INPUT: {
            const char *txt = e.text.text ? e.text.text : "";
            strncpy(g_text_buf, txt, sizeof(g_text_buf) - 1);
            g_text_buf[sizeof(g_text_buf) - 1] = '\0';
            type = NOUI_EV_TEXT;
            break;
        }
        /* 阶段81：触摸。SDL 给的是归一化坐标（0..1），换算成当前窗口像素；
         * 压力量化进 aux（0..255）。 */
        case SDL_EVENT_FINGER_DOWN:
        case SDL_EVENT_FINGER_UP:
        case SDL_EVENT_FINGER_MOTION: {
            int vw = 0, vh = 0;
            if (g_window) SDL_GetWindowSize(g_window, &vw, &vh);
            float sx = (vw > 0) ? (float)vw : 1.0f;
            float sy = (vh > 0) ? (float)vh : 1.0f;
            if (e.type == SDL_EVENT_FINGER_DOWN)          type = NOUI_EV_FINGER_DOWN;
            else if (e.type == SDL_EVENT_FINGER_UP)       type = NOUI_EV_FINGER_UP;
            else                                          type = NOUI_EV_FINGER_MOTION;
            x = (long long)(e.tfinger.x * sx);
            y = (long long)(e.tfinger.y * sy);
            d = quant_unit(e.tfinger.pressure);
            break;
        }
        /* 阶段81：压感笔。坐标为窗口像素；axis 事件的 value 量化进 aux
         * （压力轴 0..1 直接可用；角度等其他轴按同一映射近似）。 */
        case SDL_EVENT_PEN_DOWN:
        case SDL_EVENT_PEN_UP:
        case SDL_EVENT_PEN_MOTION:
        case SDL_EVENT_PEN_AXIS: {
            float px = 0.0f, py = 0.0f, val = 0.0f;
            switch (e.type) {
                case SDL_EVENT_PEN_DOWN:
                    type = NOUI_EV_PEN_DOWN; px = e.ptouch.x; py = e.ptouch.y; break;
                case SDL_EVENT_PEN_UP:
                    type = NOUI_EV_PEN_UP; px = e.ptouch.x; py = e.ptouch.y; break;
                case SDL_EVENT_PEN_MOTION:
                    type = NOUI_EV_PEN_MOTION; px = e.pmotion.x; py = e.pmotion.y; break;
                default:
                    type = NOUI_EV_PEN_AXIS; px = e.paxis.x; py = e.paxis.y;
                    val = e.paxis.value; break;
            }
            x = (long long)px;
            y = (long long)py;
            d = quant_unit(val);
            break;
        }
        /* 阶段81：文件 / 文本拖放。路径或文本进内部缓冲，由 noui_drop_take()
         * 取走（读完即空）；位置事件带坐标。 */
        case SDL_EVENT_DROP_FILE:
        case SDL_EVENT_DROP_TEXT: {
            const char *data = e.drop.data ? e.drop.data : "";
            strncpy(g_drop_buf, data, sizeof(g_drop_buf) - 1);
            g_drop_buf[sizeof(g_drop_buf) - 1] = '\0';
            type = (e.type == SDL_EVENT_DROP_FILE) ? NOUI_EV_DROP_FILE : NOUI_EV_DROP_TEXT;
            x = (long long)e.drop.x;
            y = (long long)e.drop.y;
            break;
        }
        case SDL_EVENT_DROP_BEGIN:
            type = NOUI_EV_DROP_BEGIN;
            break;
        case SDL_EVENT_DROP_COMPLETE:
            type = NOUI_EV_DROP_COMPLETE;
            break;
        case SDL_EVENT_DROP_POSITION:
            type = NOUI_EV_DROP_POS;
            x = (long long)e.drop.x;
            y = (long long)e.drop.y;
            break;
        default:
            return 0;
    }
    return ev_pack((int)type, x, y, (int)d);
}

int noui_should_quit(void) { return g_quit; }
void noui_delay(int ms) { SDL_Delay((Uint32)ms); }

/* 取走最近一次文本输入事件的 UTF-8。返回内部静态缓冲，**读完即清空**
 * （下一次调用返回 ""），所以调用方要在收到 type=7 的同一轮里取。
 * 缓冲上限 256 字节：更长的一次输入会被截断（正常 IME 组字远小于它）。 */
const char *noui_text_take(void) {
    static char out[sizeof(g_text_buf)];
    if (g_text_buf[0] == '\0') return "";
    memcpy(out, g_text_buf, sizeof(out));
    g_text_buf[0] = '\0';
    return out;
}

/* 系统剪贴板。copy 直接转交 SDL；paste 把 SDL 分配的字符串拷进静态缓冲后
 * 立刻释放，避免每粘一次漏一块。缓冲上限 4096 字节，超长截断。 */
void noui_clipboard_copy(const char *text) {
    if (text) SDL_SetClipboardText(text);
}

const char *noui_clipboard_paste(void) {
    static char buf[4096];
    char *t = SDL_GetClipboardText();
    if (!t) { buf[0] = '\0'; return buf; }
    strncpy(buf, t, sizeof(buf) - 1);
    buf[sizeof(buf) - 1] = '\0';
    SDL_free(t);
    return buf;
}

const char *noui_version(void) {
    static char buf[32];
    int v = SDL_GetVersion();
    SDL_snprintf(buf, sizeof(buf), "%d.%d.%d",
        SDL_VERSIONNUM_MAJOR(v), SDL_VERSIONNUM_MINOR(v), SDL_VERSIONNUM_MICRO(v));
    return buf;
}

/* ============================================================================
 * 阶段78：图片 / 精灵。加载 = PNG 解码 + 建 SDL_Texture；绘制 = 拉伸到目标盒。
 * 纹理由 noui_quit() 统一释放（注册表上限 NOUI_IMG_MAX，满了返回 0）。
 * ============================================================================ */
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

static void win_tex_purge_slot(int slot) {
    int i;
    if (slot < 0 || slot >= NOUI_WIN_MAX) return;
    for (i = 0; i < NOUI_IMG_MAX; i++) {
        if (g_img_tex[slot][i]) { SDL_DestroyTexture(g_img_tex[slot][i]); g_img_tex[slot][i] = NULL; }
    }
}

static void win_tex_purge_all(void) {
    int s;
    for (s = 0; s < NOUI_WIN_MAX; s++) win_tex_purge_slot(s);
}

long long noui_img_load(const char *path) {
    if (!path) return 0;
    long len = 0;
    unsigned char *data = read_file_all(path, &len);
    if (!data) return 0;
    noui_img_t im;
    if (noui_png_decode(data, len, &im) != 0) { free(data); return 0; }
    free(data);
    SDL_Surface *sf = SDL_CreateSurface(im.w, im.h, SDL_PIXELFORMAT_RGBA8888);
    if (!sf) { noui_img_free(&im); return 0; }
    if (SDL_MUSTLOCK(sf)) SDL_LockSurface(sf);
    memcpy(sf->pixels, im.rgba, (size_t)im.w * (size_t)im.h * 4);
    if (SDL_MUSTLOCK(sf)) SDL_UnlockSurface(sf);
    int iw = im.w, ih = im.h;
    noui_img_free(&im);
    int i;
    for (i = 0; i < NOUI_IMG_MAX; i++) {
        if (!g_imgs[i]) {
            g_imgs[i] = sf;
            g_img_w[i] = iw;
            g_img_h[i] = ih;
            return (long long)i + 1;
        }
    }
    SDL_DestroySurface(sf);
    return 0;
}

long long noui_img_size(long long id) {
    int i = (int)id - 1;
    if (i < 0 || i >= NOUI_IMG_MAX || !g_imgs[i]) return 0;
    return ((long long)g_img_w[i] << 32) | (unsigned int)g_img_h[i];
}

void noui_draw_image(double x, double y, double w, double h, long long id) {
    int i = (int)id - 1;
    if (i < 0 || i >= NOUI_IMG_MAX || !g_imgs[i]) return;
    if (!g_renderer || g_attached < 0) return;
    /* 纹理由 surface 按当前窗口惰性创建并缓存：每个 renderer 一份 */
    SDL_Texture *tex = win_img_tex(id);
    if (!tex) return;
    SDL_FRect dst;
    dst.x = (float)x; dst.y = (float)y; dst.w = (float)w; dst.h = (float)h;
    SDL_RenderTexture(g_renderer, tex, NULL, &dst);
}

/* ============================================================================
 * 阶段81：拖放取件、DPI 缩放与新增绘制原语
 * ============================================================================ */

/* 取走最近一次 DROP_FILE / DROP_TEXT 的路径或文本（读完即空）。 */
const char *noui_drop_take(void) {
    static char empty[1] = {'\0'};
    if (g_drop_buf[0] == '\0') return empty;
    /* 直接返回内部缓冲会把「已取走」的事实暴露给调用方之外的状态；为了保持
     * 静态缓冲可复用，复制到独立静态区再清空主缓冲。 */
    static char taken[1024];
    memcpy(taken, g_drop_buf, sizeof(taken));
    g_drop_buf[0] = '\0';
    return taken;
}

/* 当前 attach 窗口的 DPI 缩放 x1000（1000 = 1.0x）。无窗口 / 查询失败回退 1000。 */
long long noui_window_display_scale1000(void) {
    if (!g_window) return 1000;
    float s = SDL_GetWindowDisplayScale(g_window);
    if (!(s > 0.0f)) return 1000;
    return (long long)(s * 1000.0f + 0.5f);
}

/* 图片纹理惰性创建（按当前 attach 窗口槽位缓存），供旋转绘制复用。 */
static SDL_Texture *win_img_tex(long long id) {
    int i = (int)id - 1;
    if (i < 0 || i >= NOUI_IMG_MAX || !g_imgs[i]) return NULL;
    if (!g_renderer || g_attached < 0) return NULL;
    SDL_Texture *tex = g_img_tex[g_attached][i];
    if (!tex) {
        tex = SDL_CreateTextureFromSurface(g_renderer, g_imgs[i]);
        if (tex) g_img_tex[g_attached][i] = tex;
    }
    return tex;
}

/* 圆角矩形近似：SDL_Renderer 没有圆角 API，用「中央矩形 + 上下逐行 sqrt 收窄」
 * 画出连续边界。半径夹到 min(w,h)/2 以内；负尺寸安全跳过。 */
void noui_draw_round_rect(float x, float y, float w, float h, float rad,
                          int r, int g, int b, int a) {
    if (!g_renderer || w <= 0.0f || h <= 0.0f) return;
    if (rad <= 0.0f) {
        SDL_FRect dst = { x, y, w, h };
        SDL_SetRenderDrawColor(g_renderer, (Uint8)r, (Uint8)g, (Uint8)b, (Uint8)a);
        SDL_RenderFillRect(g_renderer, &dst);
        return;
    }
    float maxr = (w < h ? w : h) * 0.5f;
    if (rad > maxr) rad = maxr;
    SDL_SetRenderDrawColor(g_renderer, (Uint8)r, (Uint8)g, (Uint8)b, (Uint8)a);
    SDL_FRect mid = { x, y + rad, w, h - 2.0f * rad };
    SDL_RenderFillRect(g_renderer, &mid);
    int row;
    for (row = 0; row < (int)rad; row++) {
        float dy = (float)row + 0.5f;              /* 行中心到圆边的距离 */
        float half = sqrtf(rad * rad - dy * dy);   /* 该行半宽 */
        if (half <= 0.0f) continue;
        SDL_FRect line;
        line.x = x + rad - half;
        line.y = y + (rad - dy) - 0.5f;
        line.w = w - 2.0f * rad + 2.0f * half;
        line.h = 1.0f;
        SDL_RenderFillRect(g_renderer, &line);
        /* 镜像到底边 */
        line.y = y + h - rad + dy - 0.5f;
        SDL_RenderFillRect(g_renderer, &line);
    }
}

/* 投影：以目标矩形为形，向下偏移 dy，画多层外扩的半透明圆角矩形叠出柔和边缘。
 * alpha 逐级递减，层数固定 6 层（够柔和且开销恒定）。 */
void noui_draw_shadow(float x, float y, float w, float h, float rad, float dy,
                      int r, int g, int b, int a) {
    if (!g_renderer || w <= 0.0f || h <= 0.0f || a <= 0) return;
    int layer;
    for (layer = 5; layer >= 0; layer--) {
        float t = (float)layer / 5.0f;             /* 1 -> 外层最淡 */
        float grow = 2.0f + t * 4.0f;
        int la = (int)((float)a * (1.0f - t) * 0.35f);
        if (la <= 0) continue;
        noui_draw_round_rect(x - grow, y - grow + dy, w + 2.0f * grow,
                             h + 2.0f * grow, rad + grow, r, g, b, la);
    }
}

/* 图片旋转 / 翻转：绕目标盒中心旋转 angle 度，flip 位 1=水平、2=垂直。 */
void noui_draw_image_rot(double x, double y, double w, double h, long long id,
                         double angle, int flip) {
    SDL_Texture *tex = win_img_tex(id);
    if (!tex || !g_renderer) return;
    SDL_FRect dst;
    dst.x = (float)x; dst.y = (float)y; dst.w = (float)w; dst.h = (float)h;
    SDL_FlipMode mode = SDL_FLIP_NONE;
    if (flip & 1) mode |= SDL_FLIP_HORIZONTAL;
    if (flip & 2) mode |= SDL_FLIP_VERTICAL;
    SDL_RenderTextureRotated(g_renderer, tex, NULL, &dst,
                             angle, NULL, mode);
}
