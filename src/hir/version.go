package hir

// HIRVersion is the version of the HIR *shape*: everything that runs after
// ParseProgram — H0 sugar elimination, the match representation, union/slice/
// generic monomorphisation, assembly renaming and module prefixing, clone
// isolation. It is the counterpart of parser.ASTVersion, and the split between
// them is normative (design doc `nolang-parser-lowering-invariants.md` §S6 /
// C9):
//
//	parser.ASTVersion — what ParseProgram RETURNS. Folded into the Tier-1 /
//	  Tier-0 AST cache keys. Bump ⇒ every cached AST blob is invalidated.
//	hir.HIRVersion    — what lowering and assembly BUILD from that AST. No
//	  disk cache exists for HIR today, so nothing reads this at runtime.
//
// The point of keeping them apart is that an HIR-only change must not throw
// away the AST cache: the AST of an unchanged source file is still the AST of
// that source file, no matter how the lowering passes were reshaped. Before
// S6 there was one version and the rule was "bump it for any lowering change",
// which invalidated the whole cache for a change that cannot affect a blob.
//
// This constant exists now, before there is anything to key, for two reasons:
//   - so that the day an HIR disk cache is added, its key has a name and a
//     documented policy instead of inheriting ASTVersion's;
//   - so that the split is asserted rather than assumed — see
//     parser.TestASTCacheKeyCoversParseVersionOnly, which fails if the AST key
//     ever starts depending on this value.
//
// Bump rule: any change that alters the HIR tree produced from an unchanged
// AST. Not required for MIR/LLVM changes (those are downstream of HIR).
const HIRVersion = "2026-10-21.1"
