package main

import (
	"fmt"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
)

// cacheCommand implements `no cache` — management of the on-disk user AST
// cache (~/.cache/nolang/ast, overridable via $NOLANG_AST_CACHE_DIR).
//
// Why this exists: the cache is content-addressed with ASTVersion in the key,
// so bumping ASTVersion (required whenever a parse-time output shape changes —
// inferred types, synthetic nodes) invalidates old blobs AUTOMATICALLY. The
// failure mode is forgetting the bump: old blobs then keep hitting and serve
// pre-change inferred types, and the only symptom is a confusing "old error
// still reported after the fix". `no cache clear` is the manual escape hatch;
// `no cache stat` shows what is on disk. For a build-time detection of the
// forgotten-bump case see NOLANG_AST_CACHE_VERIFY (UserASTCache.Get).
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
