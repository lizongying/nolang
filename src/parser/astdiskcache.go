package parser

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ASTVersion bumps whenever the AST shape or the gob serialization of
// *Program / CachedProgram changes in a way that would make a previously
// cached blob decode into a structurally different tree. It is folded into
// every on-disk cache key, so bumping it invalidates all stale entries at
// once without scanning the directory.
// 2026-10-11.1 — walkProgramNodes switched from per-unique-node to
// per-occurrence indexing (see the comment on walkProgramNodes): the old
// encoding attached nodeSem entries to the wrong nodes after a gob round-trip.
// The uint32 keys keep their type, so old entries would still DECODE — they
// must be invalidated by this bump or a stale blob would restore a wrong
// mapping instead of failing.
// 2026-10-11.2 — CachedProgram now implements GobEncoder/GobDecoder, so its
// serialization changed from "gob of the struct" to "gob of a deterministic
// shadow with key-sorted maps" (see astcache_det.go). Old entries decode into
// a different wire shape and must be invalidated.
// 2026-10-12.1 — the unified option rule (D6/D1) changed what ParseProgram
// PRODUCES without changing the wire shape: `x = a + b` (and `a - b`, `a * b`,
// and annotated arithmetic) now gets a synthesized `?T` type annotation from
// the lowering pass, and `#{overflow=…}` no longer downgrades it to a plain
// int. The added `matchArm.valueCtx` field is unexported, so gob ignores it and
// old blobs still decode — but they decode into the PRE-CHANGE lowering result.
// A stale entry therefore silently resurrects the old semantics (measured: a
// `no vet` false-positive `ovf-int-default` on `main` as the sole function with
// a 2-statement body, reproducible only on a cache HIT and gone under
// NOLANG_AST_CACHE_OFF=1). Bump so every stale entry is invalidated.
//
// Rule of thumb: bump this for any change to the lowering pass, not just for
// changes to the gob layout. The cache key is (content, ASTVersion, sigSalt) —
// it cannot see the compiler build, so a semantics-only change would otherwise
// be masked by cached blobs.
//
// 2026-10-12.3：D7-R1 反轉 —— 重新載入 bounds.go 的「可證明越界安全」豁免
// （p.inBoundsIdx / isSafeIndexBase 早退 / AnalyzInBoundsForFunc 合併 /
// checker + fmt 三方位接）。這會改變 lowering 的輸出形狀（無數取不再被降級成
// option、不再插入 __cap_N / match 區塊），屬必 bump 的 lowering 變更。
//
// 2026-10-13.2：15c72ccd 之後的提交（c721fe6c 裸 match 降級 + */** 速寫、
// 762ea045/33055cc5 解析期推斷區域變數與函式回傳型別）都改變了 ParseProgram
// 的**輸出形狀**卻沒有 bump——按上方 rule of thumb 這就該 bump。影響面只到
// Tier 1（入口檔 + 本地模組，見 build.parseFile / CompileTarget）：開發期設了
// NOLANG_STD_SRC 時，std 模組走 parseEmbeddedProgram，既不讀也不寫 Tier 1；
// Tier 0 blob 由 make 的 STDSIG_GEN 依賴（含 src/parser/*.go）自動重烤，加上
// ExternSignatureFingerprint 折入了 ASTVersion，雙重自洽，無需靠本次 bump。
// （更正：先前把 `no test test/std/ed25519.no` 的簽名 S 全 0 歸因於陳舊 Tier 1
// blob，為誤判——該症狀是 std 模組的 MIR 降級缺陷：lowerIf 在 arm body 走安全
// 索引後未替當前 block 接上 terminator，已由 src/mir/hir2mir.go 修正。當時 scratch
// 檔的內容是新增的，Tier 1 必然 miss；sc-muladd 所在的 std 又不經過 Tier 1，所以
// NOLANG_AST_CACHE_OFF 兩側看到的其實是不同世代的 bin/no（平行 session 在中間
// 重建過），那個 A/B 不成立。）
//
// 2026-10-14.1：補齊 parse 期區域變數型別推斷的兩個缺口，改變 ParseProgram 的
// 輸出（更多 LetStatement 帶上推斷型別，進而使 inferOptionArith 為更多算術綁定
// 合成 `?T`）：
//   - 裸標量字面量（`j = 1` / `f = 1.5` / `s = 'x'` / `b = true` / `c = "a"`）
//     的推斷型別原本只寫進 stmt.Type（IsInferred）而未登記語義表，導致
//     signedIntOperandType 對「推斷出的有號整數區域變數」查不到型別；
//   - 識別符賦值 `x = y` 未傳播 y 的已知型別，`i = 0; j = i` 這條鏈斷在 j。
//
// 兩者疊加使 `p-opt = j + 1` 推不出 `?i64`，ok arm 永不命中而落到 fallback。
// 實測 nojq dec-valid 的科學記數分支：`1e3`/`1E3`/`1.5e2`/`1e0` 被誤判 invalid，
// 且只在 Tier 1 cache HIT 時復現、`NOLANG_AST_CACHE_OFF=1` 即消失——正是本檔
// 開頭 rule of thumb 描述的陳舊 blob 症狀。按該規則 bump 以失效所有舊條目。
//
// 2026-10-15.1：D7 最終定案（兩層分離）—— isSafeIndexBase 不再依據 p.inBoundsIdx
// 對「可證明越界安全」的讀取早退，in-bounds 讀取照常降級為 option（用戶側必須
// 解包；內部消除改由 codegen/MIR 負責）。這改變 lowering 輸出形狀（更多 __cap_N
// / match 區塊），屬必 bump 的 lowering 變更。
const ASTVersion = "2026-10-15.1"

// UserASTCache is Tier 1 of the incremental-build AST cache: an on-disk,
// content-addressed store of parsed user-module Programs. It is independent
// of process and workspace, so a fresh `no build`/`no test` reuses the AST of
// every unchanged user file and only re-lexes+re-parses what actually changed.
//
// The cache key is (source content hash + ASTVersion + an external-signature
// salt). The salt covers exactly the inputs that ParseProgram consumes beyond
// the raw source text — injected cross-file function/struct signatures, the
// anonymous-fn-type flag, and the std signature key — so a restored Program is
// always identical to a fresh parse under the same inputs and is never used
// where it would be wrong. AST only: MIR/LLVM IR is never stored, so there is
// no target/opt-level/bounds-check dimension to the key.
type UserASTCache struct {
	dir    string
	max    int
	mu     sync.Mutex
	approx int  // in-process approximate on-disk entry count
	primed bool // approx has been synced with the real directory at least once
}

// NewUserASTCache creates a cache rooted at dir with the given max entry count.
// The directory is created if missing.
func NewUserASTCache(dir string, max int) (*UserASTCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if max <= 0 {
		max = 4096
	}
	return &UserASTCache{dir: dir, max: max}, nil
}

var (
	userASTCacheOnce sync.Once
	userASTCacheInst *UserASTCache
	userASTCacheErr  error
)

// DefaultUserASTCache returns the process-global on-disk user AST cache rooted
// at $NOLANG_AST_CACHE_DIR (default ~/.cache/nolang/ast). A nil, non-error
// result means caching is disabled (dir unwritable or NOLANG_AST_CACHE_OFF set);
// callers skip the cache silently. The instance is created at most once per
// process.
func DefaultUserASTCache() (*UserASTCache, error) {
	userASTCacheOnce.Do(func() {
		if os.Getenv("NOLANG_AST_CACHE_OFF") != "" {
			userASTCacheInst = nil
			return
		}
		dir := os.Getenv("NOLANG_AST_CACHE_DIR")
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				userASTCacheInst = nil
				return
			}
			dir = filepath.Join(home, ".cache", "nolang", "ast")
		}
		max := 4096
		if v := os.Getenv("NOLANG_AST_CACHE_MAX"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				max = n
			}
		}
		c, err := NewUserASTCache(dir, max)
		if err != nil {
			userASTCacheInst = nil
			userASTCacheErr = err
			return
		}
		userASTCacheInst = c
	})
	return userASTCacheInst, userASTCacheErr
}

// KeyFor derives the content-addressed cache key for a source blob under a
// given external-signature salt. The salt must capture every ParseProgram
// input other than the source text itself (see the UserASTCache doc comment).
func (c *UserASTCache) KeyFor(content, sigSalt string) string {
	h := sha256.New()
	h.Write([]byte(ASTVersion))
	h.Write([]byte{0})
	h.Write([]byte(sigSalt))
	h.Write([]byte{0})
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *UserASTCache) pathFor(key string) string {
	return filepath.Join(c.dir, key+".gob.gz")
}

func (c *UserASTCache) tmpPathFor(key string) string {
	return filepath.Join(c.dir, key+"."+strconv.Itoa(os.Getpid())+".tmp")
}

// Get decodes and returns the cached Program for key, or (nil, false). The
// returned Program is freshly decoded (not shared across calls), preserving
// the "parse fresh per load" semantics the codegen pipeline relies on.
func (c *UserASTCache) Get(key string) (*Program, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := os.ReadFile(c.pathFor(key))
	if err != nil {
		return nil, false
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	defer zr.Close()
	var cp CachedProgram
	if err := gob.NewDecoder(zr).Decode(&cp); err != nil {
		return nil, false
	}
	RestoreNodeSem(cp.Prog, cp.NodeSem)
	return cp.Prog, true
}

// Put serializes prog and writes it to the cache under key, evicting the
// oldest entries when over capacity. It never returns an error that should
// abort a build; failures are silent.
func (c *UserASTCache) Put(key string, prog *Program) {
	if c == nil || prog == nil {
		return
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	cp := &CachedProgram{Prog: prog, NodeSem: SerializeNodeSem(prog)}
	if err := gob.NewEncoder(zw).Encode(cp); err != nil {
		return
	}
	if err := zw.Close(); err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.primed {
		if es, err := os.ReadDir(c.dir); err == nil {
			c.approx = len(es)
		}
		c.primed = true
	}
	final := c.pathFor(key)
	tmp := c.tmpPathFor(key)
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return
	}
	c.approx++
	if c.approx > c.max {
		c.evict()
	}
}

// evict trims the oldest entries (by mtime) until the on-disk count is back at
// or below c.max. It recomputes the real count first so cross-process growth
// is also bounded.
func (c *UserASTCache) evict() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type fe struct {
		name string
		mt   time.Time
	}
	files := make([]fe, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fe{e.Name(), info.ModTime()})
	}
	c.approx = len(files)
	over := c.approx - c.max
	if over <= 0 {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mt.Before(files[j].mt) })
	for i := 0; i < len(files) && over > 0; i++ {
		if strings.HasSuffix(files[i].name, ".tmp") {
			continue
		}
		if os.Remove(filepath.Join(c.dir, files[i].name)) == nil {
			over--
		}
	}
	c.approx = c.max
}
