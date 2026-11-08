package main

import (
	"fmt"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
)

// cacheCommand implements `no cache` — management of the on-disk user AST
// cache (~/.cache/nolang/ast, overridable via $NOLANG_AST_CACHE_DIR).
//
// Why this exists: the cache key is
//
//	sha256(ASTVersion ‖ parse-time salt ‖ source content)
//
// (parser.UserASTCache.KeyFor), and the salt carries checker.StdSigKey — the
// content signature of every file listed in src/hash.noml, i.e. the std library
// AND the compiler's own sources (see build.(*Transpiler).astCacheSalt). So a
// `no` built via `make` re-keys itself whenever the compiler or std changes:
// old blobs stop being hit automatically, and no manual cleaning is required.
// `no cache clear` is only an escape hatch (hand-rolled binaries, entries
// written by a compiler that predates the salt); `no cache stat` shows what is
// on disk. ASTVersion still matters as the second axis — bump it when a
// parse-time output shape changes (inferred types, synthetic nodes) so caches
// shared across machines are invalidated too.
func cacheCommand(args []string) {
	sub := "help"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "stat":
		if parser.UserASTCacheOff() {
			fmt.Println(i18n.P("evesyggh", "AST cache: disabled (NOLANG_AST_CACHE_OFF is set)"))
			return
		}
		st := parser.UserASTCacheStats()
		fmt.Printf(i18n.P("icfe5o5a", "AST cache directory: %s\n"), st.Dir)
		fmt.Printf(i18n.P("82hkddyo", "Entries: %d\n"), st.Entries)
		fmt.Printf(i18n.P("u9kq06kz", "Total size: %d bytes\n"), st.Bytes)
		fmt.Printf(i18n.P("6709xe69", "Parser ASTVersion: %s\n"), parser.ASTVersion)
	case "clear":
		n, err := parser.UserASTCacheClear()
		if err != nil {
			fmt.Printf(i18n.P("fftzl9dl", "clear failed: %v\n"), err)
			return
		}
		fmt.Printf(i18n.P("6m53s6q8", "Removed %d cache entries.\n"), n)
	default:
		fmt.Println(i18n.P("ksvya88a", "Usage: no cache <stat|clear>"))
		fmt.Println(i18n.P("t2fwnrfi", "  stat   show cache location, entry count, size and ASTVersion"))
		fmt.Println(i18n.P("e7xfrsz0", "  clear  remove all cached AST blobs (escape hatch for suspected stale entries)"))
		fmt.Println(i18n.P("t8440cno", "Env: NOLANG_AST_CACHE_OFF=1 disables the cache; NOLANG_AST_CACHE_DIR overrides the directory."))
		fmt.Println(i18n.P("gskcg79t", "Note: entries are versioned — bumping ASTVersion invalidates them automatically."))
	}
}
