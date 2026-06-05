# Nolang 建置工具
#
# 目標：
#   bin/nolang-cli    — CLI 命令列工具
#
# 用法：
#   make              — 建置所有目標
#   make nolang-cli   — 僅建置 CLI
#   make clean        — 清除建置產出
#   make help         — 顯示說明

GO      ?= go
BINDIR  ?= bin
SRCMOD   = src/go.mod
CLI_BIN  = $(BINDIR)/nolang-cli

.PHONY: all clean help nolang-lsp $(BINDIR)

all: $(CLI_BIN)

# ── 目錄 ─────────────────────────────────

$(BINDIR):
	mkdir -p $(BINDIR)

# ── CLI ───────────────────────────────────

$(CLI_BIN): src/cmd/cli/main.go $(SRCMOD) | $(BINDIR)
	cd src && $(GO) build -o ../$(CLI_BIN) ./cmd/cli

# ── nolang-build ──────────────────────────

BUILD_BIN  = $(BINDIR)/nolang-build

$(BUILD_BIN): src/build/main.go $(SRCMOD) | $(BINDIR)
	cd src && $(GO) build -o ../$(BUILD_BIN) ./build

all: $(BUILD_BIN)

# ── LSP Server ────────────────────────────

LSP_BIN   = vscode-nolang/server/nolang-lsp

$(LSP_BIN): src/cmd/lsp/main.go $(SRCMOD)
	mkdir -p vscode-nolang/server
	cd src && $(GO) build -o ../../vscode-nolang/server/nolang-lsp ./cmd/lsp

all: $(LSP_BIN)

nolang-lsp: FORCE
	mkdir -p vscode-nolang/server
	cd src && $(GO) build -o ../../vscode-nolang/server/nolang-lsp ./cmd/lsp

FORCE:

# ── 其他 ───────────────────────────────────

clean:
	rm -rf $(BINDIR)

help:
	@echo "Nolang 建置目標："
	@echo "  make            建置所有目標"
	@echo "  make nolang-cli     建置 bin/nolang-cli"
	@echo "  make nolang-build   建置 bin/nolang-build"
	@echo "  make nolang-lsp     建置 vscode-nolang/server/nolang-lsp"
	@echo "  make clean         清除建置產出"
	@echo ""
	@echo "環境變數："
	@echo "  GO=go           指定 Go 編譯器（預設 go）"
	@echo "  BINDIR=bin      指定輸出目錄（預設 bin）"
