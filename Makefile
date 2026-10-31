# Nolang 構建

GO        ?= go
# GOHOST runs host-side build TOOLS (genstdsig) that must build AND execute on
# the BUILD host, never inheriting a cross-compile environment. `make no-llvm`
# for the Windows target exports GOOS=windows/GOARCH/CC=zig-...-windows-gnu; the
# STDSIG_GEN prerequisite would then cross-build genstdsig into a .exe and fail to
# exec it on the Linux runner ("fork/exec .../genstdsig.exe: exec format error").
# Force host defaults (empty GOOS/GOARCH => host) and pure-Go (the generator's
# checker/hir/parser/lexer deps need no cgo). The real no/no-llvm binaries are
# still built with the ambient GOOS/GOARCH so cross-compilation keeps working.
GOHOST     = env GOOS= GOARCH= CGO_ENABLED=0 $(GO)
BINDIR    ?= bin
GIT_VERSION := $(shell git describe --tags --dirty --always 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u '+%s' 2>/dev/null || echo "0")
LD_FLAGS  ?= -ldflags="-s -w -X main.version=$(GIT_VERSION) -X main.buildDate=$(BUILD_DATE)"
SRCMOD     = src/go.mod
GO_SOURCES := $(shell find src -name '*.go' -type f)
NO_SOURCES := $(shell find src/std -name '*.no' -type f)
NO_BIN    = $(BINDIR)/no
NO_HOME   = $(HOME)/no/bin
NO_LINK   = /usr/local/bin/no
LSP_BIN    = vscode-nolang/server/lsp
WASM_DIR  = docs/static/wasm
NO_WASM   = $(WASM_DIR)/no.wasm
LSP_WASM  = $(WASM_DIR)/lsp.wasm
PLAYGROUND_PORT ?= 3000

# ── TRACE ID ────────────────────────────────────
# Each ValidateResult{} literal in the checker source carries
#   TraceID: "PLACEHOLDER"
# The `stamp-traceid` target replaces every PLACEHOLDER with a
# unique 8-char random base36 string via perl.  If a real ID is
# already in place, it is a no-op so repeated `make` keeps the
# same IDs.
TRACE_ID_FILES = src/checker/checker.go src/checker/funcargs.go src/checker/unresolved.go src/checker/option_compare.go src/lsp/vet.go

.PHONY: all no lsp package clean help FORCE no-wasm lsp-wasm playground playground-smoke gen stamp-traceid install test

all: $(NO_BIN) $(LSP_BIN) install

no: $(NO_BIN) install

lsp: $(LSP_BIN)

$(BINDIR):
	mkdir -p $(BINDIR)

# ── STDSIG GEN ────────────────────────────────────
# Generate stdsig_gen.go (baked-in std signature tables) before building no/lsp.
STDSIG_GEN = src/checker/stdsig_gen.go
# The tables are collected by walking HIR, so the generator output depends on
# the hir/parser/lexer packages just as much as on the checker itself: a change
# to how a definition is lowered (e.g. dropping the implicit `self` result from
# method signatures) alters the baked tables without touching checker/*.go.
STDSIG_GEN_SRCS := $(wildcard src/checker/genstdsig/*.go) src/checker/checker.go src/checker/stdsig_cache.go \
	$(wildcard src/hir/*.go) $(wildcard src/parser/*.go) $(wildcard src/lexer/*.go)

$(STDSIG_GEN): $(STDSIG_GEN_SRCS) $(NO_SOURCES) src/go.mod src/go.sum
	cd src && $(GOHOST) run ./checker/genstdsig

.PHONY: gen
gen: $(STDSIG_GEN)

# ── TEST (must go through make so StdSigKey is injected) ──
# `go test` directly leaves StdSigKey empty, which now hard-fails
# CollectStdModuleSignatures — the only valid build path is `make`. Inject the
# same key the binaries get (see LD_FLAGS_WITH_SIG) so tests exercise the real
# embedded-table fast path instead of erroring. Packages that don't import
# checker simply ignore the -X (verified).
# Scope is ./... so that this target and the CI gate (pr-verify, same command)
# cannot drift: the old hand-listed set silently omitted cmd/no, lexer, builtin,
# cache, extract_checker and the root package. -count=1 likewise matches CI —
# without it a cached "ok" can outlive the change that broke the package.
# ./tools/... carries the S4 sugar-shape gates for astpatch / std_qualify /
# std_methodify (tools/std_qualify's gate calls buildOwnership ->
# CollectStdModuleSignatures, so it NEEDS the injected key — bare
# `go test ./tools/...` would hard-exit on it).
.PHONY: test
test: $(STDSIG_GEN) verify-ty
	cd src && $(GO) test -count=1 $(LD_FLAGS_WITH_SIG) ./...

# ── TYPE-SYSTEM GATE ──────────────────────────────
# A type is a structure (src/ty), not a string. This gate fails when a package
# outside src/ty grows NEW ad-hoc type-string parsing (prefix scans for `?`/`&`/
# `*`/`[`, `|`-splits, prefix trims). The remaining occurrences are recorded in
# scripts/ty_strings_baseline.txt as a ratchet: a file may never exceed its
# number, and the gate reports when one drops so the number can be lowered.
.PHONY: verify-ty
verify-ty:
	./scripts/check_ty_strings.sh

# ── STD SIG KEY (injected at build time) ──────────
# The independent genstdsig tool hashes the CURRENT on-disk src/std tree from
# scratch and prints the key (`-key-only`). We capture that into STD_SIG_KEY
# and inject it via -X (checker.StdSigKey). At runtime the ONLY thing that
# matters is whether StdSigKey is empty: empty means the binary was NOT built
# via `make` (e.g. `cd src && go build` or bare `go test`), so the checker
# hard-fails and demands `make no`; non-empty means `make gen` baked the
# signature tables from this same src/std before compiling, so they are present
# and fresh and are used directly. There is no stale-table comparison — the
# build path (`make` always runs `make gen` first) guarantees freshness.
# STD_SIG_KEY is a recursive var, so the tool runs at recipe time — after
# `make gen` has regenerated stdsig_gen.go from the same src/std.
STD_SIG_KEY = $(shell cd src && GOOS= GOARCH= CGO_ENABLED=0 $(GO) run ./checker/genstdsig -key-only 2>/dev/null)
STD_SIG_X   = -X github.com/lizongying/nolang/checker.StdSigKey=$(STD_SIG_KEY)
# Merge the injected std-sig key INTO -ldflags (Go rejects -X outside
# -ldflags), so it sits alongside version/buildDate. Preserves any user
# override of LD_FLAGS.
LD_FLAGS_WITH_SIG = $(subst -ldflags=",-ldflags="$(STD_SIG_X) ,$(LD_FLAGS))
# Same set of flags plus the LLVM C API build default, for the statically
# linked "no-llvm" variant (see the no-llvm target). Go rejects -X outside
# -ldflags, so it is merged in the same way as the std-sig key.
LLVM_CAPI_ON_X       = -X github.com/lizongying/nolang/build/llvmapi.defaultOn=1
LD_FLAGS_WITH_SIG_ON = $(subst -ldflags=",-ldflags="$(LLVM_CAPI_ON_X) ,$(LD_FLAGS_WITH_SIG))

# ── STAMP TRACE ID ────────────────────────────────
# Replace every PLACEHOLDER in checker source files with a unique
# 8-char random base36 string.  Only replaces when placeholders are
# still present; once real IDs are in place, repeated `make` is a no-op.
.PHONY: stamp-traceid
stamp-traceid: FORCE
	@bash scripts/stamp_traceid.sh $(TRACE_ID_FILES)

# Regenerate the English catalogs from the source:
#   src/i18n/catalog/locales/en.noml      from i18n.T / i18n.P / i18n.Errorf call sites
#   src/i18n/catalog/locales/en.doc.noml  from `; ... ; [label](#anchor)` doc comments
# Run this after adding or editing a message or a doc comment, then translate any
# new keys in zh-CN.noml / zh-CN.doc.noml. A doc value is seeded once and then
# preserved, so hand edits to en.doc.noml survive. The catalog gate
# (`go test ./i18n/catalog/`) fails when a committed file is stale.
.PHONY: i18n
i18n: FORCE
	cd src && $(GO) run ./i18n/cmd/genen

# ── NO ────────────────────────────────
$(NO_BIN): $(GO_SOURCES) $(NO_SOURCES) $(STDSIG_GEN) src/go.mod src/go.sum | $(BINDIR)
	$(MAKE) stamp-traceid
	cd src && $(GO) build $(LD_FLAGS_WITH_SIG) -o ../$(NO_BIN) ./cmd/no
	chmod +x $(NO_BIN)

# ── LLVM C API（實驗開關，默認關閉）───────────────────────────────────
# 默認構建（make no）必須保持純 Go：不引入 cgo、不鏈接 LLVM 開發庫，否則 wasip1
# 交叉構建（playground 的 no.wasm / lsp.wasm）與 CI 都會被 LLVM 綁死。
# `make no-capi` 額外產出一個帶 `-tags llvm_capi` 的 bin/no-capi：只有在這個二進位裡
# NOLANG_LLVM_CAPI=1（或 --llvm-capi）才真的走「進程內用 LLVM C API 把 IR 編成 .o」；
# 其他二進位遇到該開關會印一行 warning 並回退到既有 clang 路徑。
# 此目標刻意不 install：~/no/bin/no 維持純 Go 版本。
# ⚠️ 一律給 $(LLVM_CONFIG) 加引号 —— CI 在 Windows 上傳入的路徑是
# "C:/Program Files/LLVM/bin/llvm-config.exe"（含空格），不加引号会被 sh
# 拆成多個 token，使 llvm-config 静默失敗、後續 cgo 鏈接全面崩。引號在
# Linux/macOS 的無空格路徑（llvm-config / llvm-config-22）上等价於原行。
LLVM_CONFIG ?= llvm-config
LLVM_CAPI_CFLAGS  := $(shell "$(LLVM_CONFIG)" --cflags 2>/dev/null)
LLVM_CAPI_LDFLAGS := $(shell "$(LLVM_CONFIG)" --ldflags --libs --link-shared 2>/dev/null)
# 靜態鏈接 libLLVM 時要補的 C++ 運行時等系統庫（llvm-config --system-libs 在
# macOS 上為空，Linux 上也常漏掉 C++ 運行時）。可用 LLVM_STATIC_EXTRA_LIBS 覆寫。
#
# 內置 LLVM 變體（make no-llvm）採「按宿主平台裁剪」策略：每個發布平台的二進位只鏈接
# 「構建宿主的架構 backend」+ 額外目標（預設 WebAssembly——Nolang 的瀏覽器 / Edge
# 輸出，與宿主無關）+ passes（PassBuilder / LLVMRunPasses 所在）。其餘 target
# （powerpc / mips / sparc / amdgpu / ...）與未用到的元件靠 llvm-config 的傳遞依賴
# 自然排除；連結期再靠 --gc-sections / -dead_strip 丟棄未參考的段／目標碼，進一步縮小
# 二進位。所以不同 target 的產物各自最小（x86_64 runner 的二進位不含 aarch64 / arm
# backend，反之亦然）。設 FULLLLVM=1 則改為「鏈接完整 LLVM 並註冊全部後端」
# （x86 + aarch64 + arm + wasm，見下方 LLVM_TARGET_DEFINES 的 FULLLLVM 分支），
# 代價是產物大得多。
# ⚠️ 發布的 no-llvm_* 資產一律用 FULLLLVM=1：`no build -target <非宿主架構>` 需要
# 對應後端，否則 llvmapi 會硬錯（LLVMGetTargetFromTriple 失敗，不回退 clang），
# 而 `.github/actions/build-nolang` 要靠它「單一 Linux runner 交叉編譯全部目標」。
# 見 .github/workflows/build.yml 的 build-llvm-variants。
#
# 宿主 target 預設由 uname -m 推導（x86_64→x86, aarch64/arm64→aarch64, arm*→arm），
# 可用 LLVM_HOST_TARGET=<x86|aarch64|arm> 顯式覆寫（CI 在每個 matrix 條目裡個別指定，
# 因為 Windows 下 uname -m 不可靠，且 Rosetta 的 x86_64 需要明確指定）。額外 target
# 用 LLVM_EXTRA_TARGETS 調整（設為空 = 嚴格只鏈宿主平台，不含 wasm）。
#
# 注意：真正鏈接進來的 target 必須與 llvmapi_capi.go 裡實際註冊的 target 完全一致——
# Makefile 會據此產生 NOLANG_LLVM_TARGET_* 預處理器定義（見下方 LLVM_TARGET_DEFINES），
# 只對「真有鏈接」的 target 呼叫其 LLVMInitializeXxx* 註冊函式；若註冊了未鏈接的
# target，連結期會出現未定義符號錯誤。
HOST_M := $(shell uname -m 2>/dev/null)

ifeq ($(FULLLLVM),1)
# 完整 LLVM：所有元件 + 所有 target 定義都開。
LLVM_STATIC_COMPONENTS := $(shell "$(LLVM_CONFIG)" --components 2>/dev/null)
LLVM_TARGET_DEFINES := -DNOLANG_LLVM_TARGET_X86 -DNOLANG_LLVM_TARGET_AARCH64 -DNOLANG_LLVM_TARGET_ARM -DNOLANG_LLVM_TARGET_WEBASSEMBLY
else
# 按宿主平台裁剪：先推導宿主 target（可被 LLVM_HOST_TARGET 覆寫）。
ifeq ($(LLVM_HOST_TARGET),)
ifeq ($(HOST_M),x86_64)
LLVM_HOST_TARGET := x86
endif
ifeq ($(HOST_M),aarch64)
LLVM_HOST_TARGET := aarch64
endif
ifeq ($(HOST_M),arm64)
LLVM_HOST_TARGET := aarch64
endif
ifneq ($(LLVM_HOST_TARGET),)
else
ifneq ($(filter arm%,$(HOST_M)),)
LLVM_HOST_TARGET := arm
endif
endif
ifeq ($(LLVM_HOST_TARGET),)
LLVM_HOST_TARGET := x86
endif
endif

# 額外固定鏈接的 target（預設 wasm；設 LLVM_EXTRA_TARGETS= 可只留宿主平台）。
LLVM_EXTRA_TARGETS ?= webassembly
LLVM_STATIC_COMPONENTS := $(LLVM_HOST_TARGET) $(LLVM_EXTRA_TARGETS) passes

# 依實際鏈接的 target 產生預處理器定義，供 llvmapi_capi.go 決定要註冊哪些 target。
LLVM_TARGET_DEFINES :=
ifneq ($(filter webassembly,$(LLVM_EXTRA_TARGETS)),)
LLVM_TARGET_DEFINES += -DNOLANG_LLVM_TARGET_WEBASSEMBLY
endif
ifeq ($(LLVM_HOST_TARGET),x86)
LLVM_TARGET_DEFINES += -DNOLANG_LLVM_TARGET_X86
endif
ifeq ($(LLVM_HOST_TARGET),aarch64)
LLVM_TARGET_DEFINES += -DNOLANG_LLVM_TARGET_AARCH64
endif
ifeq ($(LLVM_HOST_TARGET),arm)
LLVM_TARGET_DEFINES += -DNOLANG_LLVM_TARGET_ARM
endif
endif
# 某些發行版 LLVM（如 Linux arm64）在 --link-static 時會由 `passes` 元件帶出
# -lPolly / -lPollyISL，但套件並未隨附對應的靜態庫，導致 ld 報
# "cannot find -lPolly"。Polly 是選用的 polyhedral 最佳化庫，nolang 只用
# PassBuilder / LLVMRunPasses 的標準 passes，不依賴 Polly。scripts/llvm-link-flags.sh
# 會執行 llvm-config 並丟棄「在 $(llvm-config --libdir) 中確實不存在」的 -lPolly*
# 旗標；存在則維持原樣，以免影響已正常鏈接的環境（如 macOS、x86_64 Linux）。
# （過濾邏輯放進獨立 script，避免 Make 在 $(shell ...) 裡解析複雜 shell 管線出錯。）
LLVM_STATIC_CGO_LDFLAGS := $(shell bash $(CURDIR)/scripts/llvm-link-flags.sh "$(LLVM_CONFIG)" $(LLVM_STATIC_COMPONENTS) 2>/dev/null)
# 連結期丟棄未參考的 LLVM 段／目標碼。macOS 的 ld64 不認 --gc-sections，改用 -dead_strip。
ifeq ($(shell uname -s),Darwin)
LLVM_GC_FLAGS ?= -Wl,-dead_strip
else
LLVM_GC_FLAGS ?= -Wl,--gc-sections
endif
BREW_PREFIX := $(shell command -v brew >/dev/null 2>&1 && brew --prefix 2>/dev/null)
ifeq ($(shell uname -s),Darwin)
# macOS：CommandLineTools 下沒有 /usr/include，cgo 需要明確的 SDKROOT，
# 否則會在 runtime/cgo 階段報 'stdlib.h' file not found。
LLVM_CAPI_CGO_ENV := SDKROOT=$(shell xcrun --show-sdk-path 2>/dev/null)
# LLVM 的間接依賴（zstd / z3 / xml2）在 Homebrew 下多為 keg-only，不在默認庫搜索
# 路徑裡，必須顯式給 -L$(brew --prefix)/lib，否則 ld 會報 `library 'zstd' not found`。
ifeq ($(BREW_PREFIX),)
LLVM_STATIC_EXTRA_LIBS ?= -lc++
else
LLVM_STATIC_EXTRA_LIBS ?= -L$(BREW_PREFIX)/lib -lc++
endif
else
LLVM_STATIC_EXTRA_LIBS ?= -lstdc++ -lm
endif

.PHONY: no-capi
no-capi: $(GO_SOURCES) $(NO_SOURCES) $(STDSIG_GEN) src/go.mod src/go.sum | $(BINDIR)
	@command -v "$(LLVM_CONFIG)" >/dev/null 2>&1 || { echo "llvm-config not found: set LLVM_CONFIG=/path/to/llvm-config (macOS: brew install llvm)"; exit 1; }
	$(MAKE) stamp-traceid
	cd src && $(LLVM_CAPI_CGO_ENV) CGO_ENABLED=1 CGO_CFLAGS="$(LLVM_CAPI_CFLAGS)" CGO_LDFLAGS="$(LLVM_CAPI_LDFLAGS)" \
		$(GO) build -tags llvm_capi $(LD_FLAGS_WITH_SIG) -o ../$(BINDIR)/no-capi ./cmd/no
	chmod +x $(BINDIR)/no-capi
	@echo "Built: $(BINDIR)/no-capi (LLVM C API backend; enable with NOLANG_LLVM_CAPI=1 or --llvm-capi)"

# ── 內置 LLVM 的發布變體（靜態 libLLVM，默認啟用 C API）─────────────────
# 與 no-capi 的差別只有兩點：
#   1. 靜態鏈接 libLLVM（--link-static），二進位自帶 LLVM → 使用者不必安裝 LLVM；
#      配合 `-cc zig`（zig 自帶 clang + lld + libc，且其 LLVM 版本與此處對齊）
#      就能做到零外部工具鏈構建。代價：產物從 ~12 MB 變成上百 MB。
#   2. 注入 defaultOn=1，讓這個變體「開箱即用」地走 C API；顯式
#      NOLANG_LLVM_CAPI=0（或 --llvm-capi=0）仍可關回外部 clang 路徑。
# cgo 無法交叉編譯，故本目標只能在目標平台的原生 runner 上構建（見 CI）。
.PHONY: no-llvm
no-llvm: $(GO_SOURCES) $(NO_SOURCES) $(STDSIG_GEN) src/go.mod src/go.sum | $(BINDIR)
	@command -v "$(LLVM_CONFIG)" >/dev/null 2>&1 || { echo "llvm-config not found: set LLVM_CONFIG=/path/to/llvm-config"; exit 1; }
	$(MAKE) stamp-traceid
	cd src && $(LLVM_CAPI_CGO_ENV) CGO_ENABLED=1 CGO_CFLAGS="$(LLVM_CAPI_CFLAGS) $(LLVM_TARGET_DEFINES)" CGO_LDFLAGS="$(LLVM_STATIC_CGO_LDFLAGS) $(LLVM_STATIC_EXTRA_LIBS) $(LLVM_GC_FLAGS)" \
		$(GO) build -tags llvm_capi $(LD_FLAGS_WITH_SIG_ON) -o ../$(BINDIR)/no-llvm ./cmd/no
	chmod +x $(BINDIR)/no-llvm
	@echo "Built: $(BINDIR)/no-llvm (static libLLVM; C API on by default, disable with NOLANG_LLVM_CAPI=0)"

# ── INSTALL ────────────────────────────
# 每次 make 都強制重裝全局 no：binary 存到 ~/no/bin/，並在 /usr/local/bin/ 建軟鏈接
# （與 `no install` 的安裝約定一致）。install 會先 unlink 目標，覆蓋運行中的 binary 也安全。
.PHONY: install
install: $(NO_BIN)
	@mkdir -p $(NO_HOME)
	install -m 0755 $(NO_BIN) $(NO_HOME)/no
	ln -sf $(NO_HOME)/no $(NO_LINK)
	@echo "Installed: $(NO_LINK) -> $(NO_HOME)/no"

# ── LSP ────────────────────────────────────
$(LSP_BIN): $(GO_SOURCES) $(NO_SOURCES) $(STDSIG_GEN) src/go.mod src/go.sum
	$(MAKE) stamp-traceid
	mkdir -p $(dir $@)
	cd src && $(GO) build $(LD_FLAGS_WITH_SIG) -o ../$@ ./cmd/lsp
	chmod +x $@

package: FORCE
	$(MAKE) lsp
	cd vscode-nolang && bun run package

# ── NO WASM ────────────────────────────
# Cross-compile `no` to WebAssembly (wasip1) for the browser playground.
# Requires Go 1.21+ (project uses 1.25.4). Output: docs/static/wasm/no.wasm
no-wasm: $(NO_WASM)

$(NO_WASM): $(GO_SOURCES) $(NO_SOURCES) $(STDSIG_GEN) src/go.mod src/go.sum | $(WASM_DIR)
	$(MAKE) stamp-traceid
	cd src && GOOS=wasip1 GOARCH=wasm $(GO) build $(LD_FLAGS_WITH_SIG) -o ../$@ ./cmd/no

$(WASM_DIR):
	mkdir -p $@

# ── LSP WASM ────────────────────────────
# Cross-compile LSP server to WebAssembly (wasip1) for the browser playground.
lsp-wasm: $(LSP_WASM)

$(LSP_WASM): $(GO_SOURCES) $(NO_SOURCES) $(STDSIG_GEN) src/go.mod src/go.sum | $(WASM_DIR)
	$(MAKE) stamp-traceid
	cd src && GOOS=wasip1 GOARCH=wasm $(GO) build $(LD_FLAGS_WITH_SIG) -o ../$@ ./cmd/lsp

# ── PLAYGROUND ────────────────────────────
# Build no.wasm + lsp.wasm + Docusaurus site for the playground.
playground: no-wasm lsp-wasm
	cd docs && bun install && bun run build

# ── PLAYGROUND SMOKE TEST ────────────────────────────
# Start Docusaurus dev server and verify playground page + wasm assets are served.
# Override PLAYGROUND_PORT to use a different port (default 3000).
# Note: --noproxy '*' bypasses any http_proxy env var so local requests work.
playground-smoke: no-wasm lsp-wasm
	@cd docs && bun install
	@echo "Starting Docusaurus dev server on port $(PLAYGROUND_PORT)..."
	@set -e; \
	trap 'pkill -f "docusaurus start" 2>/dev/null || true' EXIT; \
	(cd docs && bun run start --no-open --port $(PLAYGROUND_PORT) > /tmp/docusaurus-smoke.log 2>&1 &); \
	for i in $$(seq 1 30); do \
		if curl -sf --noproxy '*' http://localhost:$(PLAYGROUND_PORT)/nolang/playground -o /dev/null 2>&1; then \
			echo "Dev server is up (after $${i}s)."; \
			break; \
		fi; \
		if [ $$i -eq 30 ]; then \
			echo "ERROR: dev server did not start within 30s"; \
			cat /tmp/docusaurus-smoke.log; \
			exit 1; \
		fi; \
		sleep 1; \
	done; \
	curl -sf --noproxy '*' http://localhost:$(PLAYGROUND_PORT)/nolang/playground -o /dev/null; \
	curl -sf --noproxy '*' http://localhost:$(PLAYGROUND_PORT)/nolang/wasm/no.wasm -o /dev/null; \
	curl -sf --noproxy '*' http://localhost:$(PLAYGROUND_PORT)/nolang/wasm/lsp.wasm -o /dev/null; \
	echo "Playground smoke test passed."

FORCE:

clean:
	rm -rf $(BINDIR)

help:
	@echo "Nolang 構建目標："
	@echo "  make            構建所有目標"
	@echo "  make no         構建 bin/no 並重裝全局 no"
	@echo "  make no-capi    構建 bin/no-capi（帶 LLVM C API 後端，開關默認關閉）"
	@echo "  make no-llvm    構建 bin/no-llvm（靜態內置 LLVM，C API 默認開啟）"
	@echo "  make install    強制重裝全局 no（~/no/bin/no + /usr/local/bin/no 軟鏈接）"
	@echo "  make lsp        構建 vscode-nolang/server/lsp"
	@echo "  make gen        gen stdsig_gen.go"
	@echo "  make stamp-traceid  替換 checker 源碼中的占位符為各自唯一的隨機 base36 ID"
	@echo "  make package    跨譯 LSP 並打包 VSCode 拓展"
	@echo "  make no-wasm    跨譯 no 為 WebAssembly (wasip1) → docs/static/wasm/no.wasm"
	@echo "  make lsp-wasm   跨譯 LSP 為 WebAssembly (wasip1) → docs/static/wasm/lsp.wasm"
	@echo "  make playground 建構 no.wasm + lsp.wasm + Docusaurus 站點"
	@echo "  make playground-smoke 啟動 dev server 並驗證 playground + wasm 資源可訪問"
	@echo "  make clean      清理"
	@echo "  make help       幫助"
	@echo ""
	@echo "環境變量："
	@echo "  GO=go           指定 Go 跨譯器（默認 go）"
	@echo "  BINDIR=bin      指定輸出目錄（默認 bin）"
	@echo "  LD_FLAGS=...    自定義鏈接標誌（內建注入 Git commit）"
	@echo "  WASI_SYSROOT=path  wasi-sysroot 路徑（no build -target wasm32-wasi 時需要）"
	@echo "  PLAYGROUND_PORT=3000  playground-smoke 使用的端口（默認 3000）"
