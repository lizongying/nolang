#!/bin/sh
# noui 一键构建 + 运行脚本
# 依赖：brew install sdl3；Nolang 编译器 bin/no（仓库根）
set -e

W=$(cd "$(dirname "$0")" && pwd)
# noui 位于仓库根，bin/no 在其父目录
NO=$(cd "$W/.." && pwd)/bin/no
SDL_LIB=/opt/homebrew/opt/sdl3/lib
TTF_LIB=/opt/homebrew/opt/sdl3_ttf/lib
export PATH="/opt/homebrew/bin:/opt/homebrew/opt/llvm/bin:$PATH"

echo "[1/3] 编译 C shim（SDL_Render+TTF / SDL_GPU）"
chmod +x "$W/build-shims.sh"
"$W/build-shims.sh"

echo "[2/3] 构建 Nolang 应用（经 -cc 包装器注入 -I/-L）"
chmod +x "$W/ccwrap.sh"
cd "$W"
"$NO" build -cc "$W/ccwrap.sh" -o dist/app .

echo "[3/3] 运行（窗口显示计数器，点击 +/- 改数值，ESC 退出）"
export DYLD_LIBRARY_PATH="$SDL_LIB:$TTF_LIB:$DYLD_LIBRARY_PATH"
"$W/dist/app"
