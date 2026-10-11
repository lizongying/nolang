package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
)

// ExternSignatureFingerprint hashes the cross-module tables that shape a parse,
// so a consumer can bake the CONFIGURATION a parse used and later prove that
// the same configuration would be used again.
//
// Why this exists: parsing is not inference-free. Lowering consults the
// injected tables (parser.isSafeIndexBase resolves a container type through
// them), so the same source text parses into a DIFFERENT AST depending on what
// was injected. Anything that caches a parse result — the Tier-0 baked std AST
// blob, the Tier-1 on-disk AST cache — is therefore only valid for the
// configuration it was produced with. Tier 1 folds the tables into its cache
// key (see build.astCacheSalt); Tier 0 stores this fingerprint in the blob and
// verifies it at load time, which is what makes "parse std with a bare
// parser.New and bake the result" fail loudly instead of silently shipping an
// AST the build path would never have produced.
//
// The four inputs mirror SetExternSignatures / SetExternEnumVariants exactly:
//
//	funcSigs     funcSignatures    (key "module.fn" or bare "fn" -> result types)
//	methodSigs   methodSignatures  (key "module.struct.method"  -> result types)
//	structFields structFields      (struct -> field -> type)
//	enumVariants sem.EnumVariants  (enum -> variant names)
//
// ASTVersion is folded in as well: it is already the project's "the AST shape or
// its serialization changed" knob, and the fingerprint guards a serialized
// artifact.
func ExternSignatureFingerprint(funcSigs, methodSigs map[string][]string, structFields map[string]map[string]string, enumVariants map[string][]string) string {
	h := sha256.New()
	fpWriteString(h, ASTVersion)
	fpWriteStrSlices(h, funcSigs)
	fpWriteStrSlices(h, methodSigs)
	fpWriteStrMapStr(h, structFields)
	fpWriteStrSlices(h, enumVariants)
	return hex.EncodeToString(h.Sum(nil))
}

// ExternSignatureFingerprint reports the configuration THIS parser was given.
//
// It deliberately reads the extern* snapshots rather than funcSignatures /
// methodSignatures / structFields / sem.EnumVariants: the parser keeps
// appending locally discovered definitions to those while it parses (see
// decl.go), so they answer "what does this parser know now" instead of "what
// was this parser configured with". Fingerprinting the accumulated tables would
// make the value depend on how much of the module had been parsed — useless as
// a configuration stamp.
func (p *Parser) ExternSignatureFingerprint() string {
	if p == nil {
		return ExternSignatureFingerprint(nil, nil, nil, nil)
	}
	return ExternSignatureFingerprint(p.externFuncSigs, p.externMethodSigs, p.externStructFields, p.externEnumVariants)
}

// ---- deterministic writers (length-prefixed, key-sorted) ----

func fpWriteString(h hash.Hash, s string) {
	fmt.Fprintf(h, "%d:%s\n", len(s), s)
}

func fpWriteStrings(h hash.Hash, ss []string) {
	fmt.Fprintf(h, "%d\n", len(ss))
	for _, s := range ss {
		fpWriteString(h, s)
	}
}

func fpWriteStrSlices(h hash.Hash, m map[string][]string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(h, "%d\n", len(keys))
	for _, k := range keys {
		fpWriteString(h, k)
		fpWriteStrings(h, m[k])
	}
}

func fpWriteStrMapStr(h hash.Hash, m map[string]map[string]string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(h, "%d\n", len(keys))
	for _, k := range keys {
		fpWriteString(h, k)
		inner := m[k]
		ikeys := make([]string, 0, len(inner))
		for ik := range inner {
			ikeys = append(ikeys, ik)
		}
		sort.Strings(ikeys)
		fmt.Fprintf(h, "%d\n", len(ikeys))
		for _, ik := range ikeys {
			fpWriteString(h, ik)
			fpWriteString(h, inner[ik])
		}
	}
}
