# Nolang 構建

GO        ?= go
BINDIR    ?= bin
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u '+%s' 2>/dev/null || echo "0")
LD_FLAGS  ?= -ldflags="-s -w -X main.version=$(GIT_COMMIT) -X main.buildDate=$(BUILD_DATE)"
SRCMOD     = src/go.mod
NO_BIN    = $(BINDIR)/no
LSP_BIN    = vscode-nolang/server/lsp

.PHONY: all no lsp package clean help FORCE

all: $(NO_BIN) $(LSP_BIN)

no: $(NO_BIN)

lsp: $(LSP_BIN)

$(BINDIR):
	mkdir -p $(BINDIR)

# ── NO ────────────────────────────────
$(NO_BIN): FORCE
	cd src && $(GO) build $(LD_FLAGS) -o ../$(NO_BIN) ./cmd/no

# ── LSP ────────────────────────────
$(LSP_BIN): FORCE
	mkdir -p $(dir $@)
	cd src && $(GO) build $(LD_FLAGS) -o ../$@ ./cmd/lsp

package: FORCE
	$(MAKE) lsp
	cd vscode-nolang && bun run package

FORCE:

clean:
	rm -rf $(BINDIR)

help:
	@echo "Nolang 構建目標："
	@echo "  make            構建所有目標"
	@echo "  make no         構建 bin/no"
	@echo "  make lsp        構建 vscode-nolang/server/lsp"
	@echo "  make package    編譯 LSP 並打包 VSCode 拓展"
	@echo "  make clean      清理"
	@echo "  make help       幫助"
	@echo ""
	@echo "環境變量："
	@echo "  GO=go           指定 Go 編譯器（默認 go）"
	@echo "  BINDIR=bin      指定輸出目錄（默認 bin）"
	@echo "  LD_FLAGS=...    自定義鏈接標誌（內建注入 Git commit）"
