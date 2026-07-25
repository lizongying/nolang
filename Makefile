# Nolang 構建

GO        ?= go
BINDIR    ?= bin
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u '+%s' 2>/dev/null || echo "0")
LD_FLAGS  ?= -ldflags="-s -w -X main.version=$(GIT_COMMIT) -X main.buildDate=$(BUILD_DATE)"
SRCMOD     = src/go.mod
GO_SOURCES := $(shell find src -name '*.go' -type f)
NO_SOURCES := $(shell find src/std -name '*.no' -type f)
NO_BIN    = $(BINDIR)/no
LSP_BIN    = vscode-nolang/server/lsp
WASM_DIR  = docs/static/wasm
NO_WASM   = $(WASM_DIR)/no.wasm
LSP_WASM  = $(WASM_DIR)/lsp.wasm

.PHONY: all no lsp package clean help FORCE no-wasm lsp-wasm playground

all: $(NO_BIN) $(LSP_BIN)

no: $(NO_BIN)

lsp: $(LSP_BIN)

$(BINDIR):
	mkdir -p $(BINDIR)

# ── NO ────────────────────────────────
$(NO_BIN): $(GO_SOURCES) $(NO_SOURCES) src/go.mod src/go.sum | $(BINDIR)
	cd src && $(GO) build $(LD_FLAGS) -o ../$(NO_BIN) ./cmd/no

# ── LSP ────────────────────────────
$(LSP_BIN): FORCE
	mkdir -p $(dir $@)
	cd src && $(GO) build $(LD_FLAGS) -o ../$@ ./cmd/lsp

package: FORCE
	$(MAKE) lsp
	cd vscode-nolang && bun run package

# ── NO WASM ────────────────────────────
# Cross-compile `no` to WebAssembly (wasip1) for the browser playground.
# Requires Go 1.21+ (project uses 1.25.4). Output: docs/static/wasm/no.wasm
no-wasm: $(NO_WASM)

$(NO_WASM): $(GO_SOURCES) $(NO_SOURCES) src/go.mod src/go.sum | $(WASM_DIR)
	cd src && GOOS=wasip1 GOARCH=wasm $(GO) build $(LD_FLAGS) -o ../$@ ./cmd/no

$(WASM_DIR):
	mkdir -p $@

# ── LSP WASM ────────────────────────────
# Cross-compile LSP server to WebAssembly (wasip1) for the browser playground.
lsp-wasm: $(LSP_WASM)

$(LSP_WASM): $(GO_SOURCES) $(NO_SOURCES) src/go.mod src/go.sum | $(WASM_DIR)
	cd src && GOOS=wasip1 GOARCH=wasm $(GO) build $(LD_FLAGS) -o ../$@ ./cmd/lsp

# ── PLAYGROUND ────────────────────────────
# Build no.wasm + lsp.wasm + Docusaurus site for the playground.
playground: no-wasm lsp-wasm
	cd docs && bun install && bun run build

FORCE:

clean:
	rm -rf $(BINDIR)

help:
	@echo "Nolang 構建目標："
	@echo "  make            構建所有目標"
	@echo "  make no         構建 bin/no"
	@echo "  make lsp        構建 vscode-nolang/server/lsp"
	@echo "  make package    編譯 LSP 並打包 VSCode 拓展"
	@echo "  make no-wasm    編譯 no 為 WebAssembly (wasip1) → docs/static/wasm/no.wasm"
	@echo "  make lsp-wasm   編譯 LSP 為 WebAssembly (wasip1) → docs/static/wasm/lsp.wasm"
	@echo "  make playground 建構 no.wasm + lsp.wasm + Docusaurus 站點"
	@echo "  make clean      清理"
	@echo "  make help       幫助"
	@echo ""
	@echo "環境變量："
	@echo "  GO=go           指定 Go 編譯器（默認 go）"
	@echo "  BINDIR=bin      指定輸出目錄（默認 bin）"
	@echo "  LD_FLAGS=...    自定義鏈接標誌（內建注入 Git commit）"
	@echo "  WASI_SYSROOT=path  wasi-sysroot 路徑（no build -target wasm32-wasi 時需要）"
