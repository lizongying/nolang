# Nolang 建置工具
#
# 目標：
#   bin/nolang         — Nolang 命令列工具（CLI + 編譯器）
#   vscode-nolang/server/nolang-lsp — LSP Server
#
# 用法：
#   make              — 建置所有目標
#   make nolang        — 僅建置主工具
#   make clean         — 清除建置產出
#   make help          — 顯示說明

GO      ?= go
BINDIR  ?= bin
SRCMOD   = src/go.mod
BIN      = $(BINDIR)/nolang

.PHONY: all clean help lsp fmt-lsp $(BINDIR)

all: $(BIN)

# ── 目錄 ─────────────────────────────────

$(BINDIR):
	mkdir -p $(BINDIR)

# ── 主工具 ────────────────────────────────

$(BIN): src/cmd/cli/main.go $(SRCMOD) | $(BINDIR)
	cd src && $(GO) build -o ../$(BIN) ./cmd/cli

all: $(BIN)

# ── LSP Server ────────────────────────────

LSP_BIN   = vscode-nolang/server/nolang-lsp

$(LSP_BIN): src/cmd/lsp/main.go $(SRCMOD)
	mkdir -p vscode-nolang/server
	cd src && $(GO) build -o ../vscode-nolang/server/nolang-lsp ./cmd/lsp

all: $(LSP_BIN)

lsp: FORCE
	mkdir -p vscode-nolang/server
	cd src && $(GO) build -o ../vscode-nolang/server/nolang-lsp ./cmd/lsp

package: FORCE
	make lsp
	cd vscode-nolang && bun run package

FORCE:

# ── 其他 ───────────────────────────────────

fmt-lsp: $(BIN)
	$(BIN) fmt -w -d tests/lsp

clean:
	rm -rf $(BINDIR)

help:
	@echo "Nolang 建置目標："
	@echo "  make            建置所有目標"
	@echo "  make nolang         建置 bin/nolang"
	@echo "  make lsp            建置 vscode-nolang/server/nolang-lsp"
	@echo "  make clean         清除建置產出"
	@echo ""
	@echo "環境變數："
	@echo "  GO=go           指定 Go 編譯器（預設 go）"
	@echo "  BINDIR=bin      指定輸出目錄（預設 bin）"
