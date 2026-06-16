# Nolang 構建

GO      ?= go
BINDIR  ?= bin
SRCMOD   = src/go.mod
CLI_BIN  = $(BINDIR)/nolang
LSP_BIN  = vscode-nolang/server/nolang-lsp

.PHONY: all nolang lsp package clean help FORCE

all: $(CLI_BIN) $(LSP_BIN)

nolang: $(CLI_BIN)

lsp: $(LSP_BIN)

$(BINDIR):
	mkdir -p $(BINDIR)

# ── CLI ────────────────────────────────
$(CLI_BIN): FORCE
	cd src && $(GO) build -o ../$(CLI_BIN) ./cmd/cli

# ── LSP ────────────────────────────
$(LSP_BIN): FORCE
	mkdir -p $(dir $@)
	cd src && $(GO) build -o ../$@ ./cmd/lsp

package: FORCE
	$(MAKE) lsp
	cd vscode-nolang && bun run package

FORCE:

clean:
	rm -rf $(BINDIR)

help:
	@echo "Nolang 構建目標："
	@echo "  make            構建所有目標"
	@echo "  make nolang     構建 bin/nolang"
	@echo "  make lsp        構建 vscode-nolang/server/nolang-lsp"
	@echo "  make package    編譯 LSP 並打包 VSCode 拓展"
	@echo "  make clean      清理"
	@echo "  make help       幫助"
	@echo ""
	@echo "環境變量："
	@echo "  GO=go           指定 Go 編譯器（默認 go）"
	@echo "  BINDIR=bin      指定輸出目錄（默認 bin）"
