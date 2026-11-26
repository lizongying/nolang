#!/bin/sh
# Nolang (no CLI) 一鍵安裝
#
#   curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
#
# 可選環境變數：
#   NO_VERSION      版本，例如 0.4.28 / v0.4.28；預設 latest
#   NO_FLAVOR       embedded（預設，內置 LLVM）或 plain（純 Go，需系統 LLVM/clang）
#   NO_INSTALL_DIR  安裝目錄；預設 $HOME/.local/bin
#   NO_REPO         GitHub 倉庫；預設 lizongying/nolang

set -eu

REPO="${NO_REPO:-lizongying/nolang}"
VERSION="${NO_VERSION:-latest}"
FLAVOR="${NO_FLAVOR:-embedded}"
INSTALL_DIR="${NO_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "需要 curl，請先安裝"

case "$FLAVOR" in
  embedded|plain) ;;
  *) die "NO_FLAVOR 必須是 embedded 或 plain" ;;
esac

# ── 平台偵測 ────────────────────────────────────────────────────────────
case "$(uname -s)" in
  Linux)                 OS=linux ;;
  Darwin)                OS=darwin ;;
  MINGW*|MSYS*|CYGWIN*)  OS=windows ;;
  *) die "不支援的作業系統: $(uname -s)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) die "不支援的架構: $(uname -m)" ;;
esac

if [ "$OS" = windows ]; then
  EXE=.exe
else
  EXE=
fi

# ── 解析版本 ────────────────────────────────────────────────────────────
# 走 releases/latest 的最終重定向網址取 tag，不打 releases API（匿名有速率限制）。
if [ "$VERSION" = latest ]; then
  LATEST_URL=$(curl -fsSL --retry 3 --connect-timeout 20 -o /dev/null -w '%{url_effective}' \
    "https://github.com/${REPO}/releases/latest") || die "無法取得 ${REPO} 的最新 release"
  TAG=${LATEST_URL##*/}
  case "$TAG" in
    v*) ;;
    *) die "無法從 ${LATEST_URL} 解析版本 tag" ;;
  esac
else
  TAG="v$(printf '%s' "$VERSION" | sed 's/^v*//')"
fi
VER=${TAG#v}
say "→ 安裝 no ${VER} (${OS}/${ARCH}, ${FLAVOR})"

# ── 組出 asset 網址 ─────────────────────────────────────────────────────
# release asset 命名（<tag> 為完整 tag，含前導 v，例如 v0.4.35）：
#   內置 LLVM: no-llvm-<os>-<arch>-<tag>[.exe]
#   純 Go:     no-<os>-<arch>-<tag>[.exe]
asset_url() {
  case "$1" in
    embedded) printf 'https://github.com/%s/releases/download/%s/no-llvm-%s-%s-%s%s' \
                "$REPO" "$TAG" "$OS" "$ARCH" "$TAG" "$EXE" ;;
    plain)    printf 'https://github.com/%s/releases/download/%s/no-%s-%s-%s%s' \
                "$REPO" "$TAG" "$OS" "$ARCH" "$TAG" "$EXE" ;;
  esac
}

TMP=$(mktemp "${TMPDIR:-/tmp}/no-install.XXXXXX") || die "無法建立暫存檔"
trap 'rm -f "$TMP"' EXIT HUP INT TERM

URL=$(asset_url "$FLAVOR")
say "→ 下載 ${URL##*/}"
if ! curl -fL --retry 3 --connect-timeout 20 --max-time 3600 -o "$TMP" "$URL"; then
  # 舊版（< v0.4.0）沒有內置 LLVM 產物；自動退回純 Go 版。
  if [ "$FLAVOR" != embedded ]; then
    die "下載失敗: $URL"
  fi
  say "! 找不到內置 LLVM 版本，改試純 Go 版（需系統 LLVM/clang）"
  FLAVOR=plain
  URL=$(asset_url plain)
  say "→ 下載 ${URL##*/}"
  curl -fL --retry 3 --connect-timeout 20 --max-time 3600 -o "$TMP" "$URL" \
    || die "下載失敗: $URL"
fi

[ -s "$TMP" ] || die "下載的檔案是空的"

# ── 安裝 ────────────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR" || die "無法建立安裝目錄 $INSTALL_DIR"
DEST="$INSTALL_DIR/no$EXE"
mv -f "$TMP" "$DEST" || die "無法寫入 $DEST"
chmod 755 "$DEST" || die "無法設定執行權限 $DEST"
trap - EXIT HUP INT TERM

say "✓ 已安裝 $DEST"
"$DEST" version || die "安裝完成但無法執行 no version"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) say "! 請把 $INSTALL_DIR 加入 PATH：export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac

say "! 編譯 .no 程式還需要 C 連結器：建議 no build -cc zig，或安裝 clang。"
