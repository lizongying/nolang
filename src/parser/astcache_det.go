package parser

import (
	"bytes"
	"encoding/gob"
	"sort"

	"github.com/lizongying/nolang/ty")

// ---------------------------------------------------------------------------
// Deterministic (byte-reproducible) encoding of CachedProgram.
//
// encoding/gob encodes maps in Go's randomized iteration order, so encoding the
// same CachedProgram twice yields different bytes. A cache HIT is unaffected —
// the decoded value is identical either way — but the ARTIFACT is not
// reproducible, and that has three visible costs:
//
//   - genstdsig's "don't rewrite the blob if its content is unchanged" guard
//     never fires, so every regeneration rewrites the 2.4 MB Tier-0 blob and
//     forces every package that embeds it to recompile;
//   - two builds from identical sources produce different `no` binaries, which
//     defeats release reproducibility;
//   - Tier-1's on-disk entries are rewritten instead of being reused.
//
// CachedProgram therefore implements GobEncoder/GobDecoder and routes every
// encode/decode through the shadows below, where each map becomes a key-SORTED
// slice. Everything else — the AST itself, comments, annotations — still goes
// through gob unchanged, so the decoded tree has exactly the shape the
// occurrence walk in astcache.go depends on.
//
// Every map reachable from CachedProgram lives in one of two types. That set
// was established by reflecting over a decoded std blob, not by inspection:
//
//	SemanticContext   VarTypes, EnumVariants, DeclaredVars, EnumVariantPayload,
//	                  EnumVariantFields, IfaceImpls, FuncVarTypes,
//	                  FuncDeclaredVars, IdxLocalTypes, UnionMembers
//	Program           BuiltinFuncNames
//
// (NodeFact.EmbedFiles used to be the third; it left with the node-facts table.)
//
// Two node-keyed tables used to hang off CachedProgram ITSELF
// (`TypeFacts` / `NodeFacts`), flattened to `map[uint32]...` keyed by the
// occurrence index of walkProgramNodes because gob does not preserve pointer
// identity for Node-keyed maps. Both were deleted on 2026-10-26: the cache now
// carries `Prog` only, and every derived fact is rebuilt from the tree by
// BuildSemViews (see semviews.go). Their shadows (`detU32Facts` /
// `detU32NodeFacts` / `detNodeFact`) went with them.
//
// (The pointer-keyed originals — SemanticContext.typeFacts / nodeFacts /
// captureFacts — are unexported and so never encoded. `Program.FmtTrivia` is
// node-keyed too but is deliberately NOT serialized: fmt and LSP always
// fresh-parse. The third node-keyed table, `SemanticContext.anns`, was deleted
// outright: `#{...}` entries are syntax, so they live on the AST nodes
// (`AnnotatedNode`) and ride along with gob's tree encoding.
// `SemanticContext.nodeSem` was deleted in Stage 7 of the THIR plan once all 18
// of its fields had moved out.)
//
// 🔴 `detSem` deliberately omits `ExternFuncSigs` / `ExternMethodSigs`: those are
// INJECTED from outside the file (cross-module and std signature tables), not
// read from its text, so baking them into a content-addressed blob would put
// caller state in a source artifact. They come back through
// SemRebuildConfig.Apply during BuildSemViews — which is why the rebuild cannot
// be skipped on decode.
//
// 🔴 Adding a map field to any of those types silently reintroduces
// nondeterminism — the new field is not in a shadow, so gob encodes it directly.
// TestCachedProgramEncodingIsDeterministic catches that: it encodes a real
// parsed program twice and compares the bytes.
//
// Nil-ness: gob preserves nil-ness for maps but not for slices, so a map routed
// through a slice cannot recover "was nil". The decoder therefore always
// materialises non-nil maps. That matches a fresh parse, where
// NewSemanticContext creates them non-nil, and every reader already tolerates
// nil (SetVarType and its siblings lazily create the maps they write to), so a
// decoded program ends up closer to a freshly parsed one, not further.
// ---------------------------------------------------------------------------

// ---- sorted key/value entries that stand in for maps on the wire ----

type detStrStr struct {
	K string
	V string
}

type detStrStrs struct {
	K string
	V []string
}

type detStrBool struct {
	K string
	V bool
}

type detStrMapStr struct {
	K string
	V []detStrStr
}

type detStrMapStrs struct {
	K string
	V []detStrStrs
}

type detStrMapBool struct {
	K string
	V []detStrBool
}

// ---- map -> sorted slice ----

func detSortStrStr(m map[string]string) []detStrStr {
	if len(m) == 0 {
		return nil
	}
	out := make([]detStrStr, 0, len(m))
	for k, v := range m {
		out = append(out, detStrStr{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

func detSortStrStrs(m map[string][]string) []detStrStrs {
	if len(m) == 0 {
		return nil
	}
	out := make([]detStrStrs, 0, len(m))
	for k, v := range m {
		out = append(out, detStrStrs{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

func detSortStrBool(m map[string]bool) []detStrBool {
	if len(m) == 0 {
		return nil
	}
	out := make([]detStrBool, 0, len(m))
	for k, v := range m {
		out = append(out, detStrBool{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

func detSortStrMapStr(m map[string]map[string]string) []detStrMapStr {
	if len(m) == 0 {
		return nil
	}
	out := make([]detStrMapStr, 0, len(m))
	for k, v := range m {
		out = append(out, detStrMapStr{K: k, V: detSortStrStr(v)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

func detSortStrMapStrs(m map[string]map[string][]string) []detStrMapStrs {
	if len(m) == 0 {
		return nil
	}
	out := make([]detStrMapStrs, 0, len(m))
	for k, v := range m {
		out = append(out, detStrMapStrs{K: k, V: detSortStrStrs(v)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

func detSortStrMapBool(m map[string]map[string]bool) []detStrMapBool {
	if len(m) == 0 {
		return nil
	}
	out := make([]detStrMapBool, 0, len(m))
	for k, v := range m {
		out = append(out, detStrMapBool{K: k, V: detSortStrBool(v)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

// ---- sorted slice -> map ----

func detRebuildStrStr(s []detStrStr) map[string]string {
	out := make(map[string]string, len(s))
	for _, e := range s {
		out[e.K] = e.V
	}
	return out
}

func detRebuildStrStrs(s []detStrStrs) map[string][]string {
	out := make(map[string][]string, len(s))
	for _, e := range s {
		out[e.K] = e.V
	}
	return out
}

func detRebuildStrBool(s []detStrBool) map[string]bool {
	out := make(map[string]bool, len(s))
	for _, e := range s {
		out[e.K] = e.V
	}
	return out
}

func detRebuildStrMapStr(s []detStrMapStr) map[string]map[string]string {
	out := make(map[string]map[string]string, len(s))
	for _, e := range s {
		out[e.K] = detRebuildStrStr(e.V)
	}
	return out
}

func detRebuildStrMapStrs(s []detStrMapStrs) map[string]map[string][]string {
	out := make(map[string]map[string][]string, len(s))
	for _, e := range s {
		out[e.K] = detRebuildStrStrs(e.V)
	}
	return out
}

func detRebuildStrMapBool(s []detStrMapBool) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(s))
	for _, e := range s {
		out[e.K] = detRebuildStrBool(e.V)
	}
	return out
}

// ---- shadows ----

type detSem struct {
	VarTypes           []detStrStr
	EnumVariants       []detStrStrs
	DeclaredVars       []detStrBool
	EnumVariantPayload []detStrMapStr
	EnumVariantFields  []detStrMapStrs
	IfaceImpls         []detStrStrs
	FuncVarTypes       []detStrMapStr
	FuncDeclaredVars   []detStrMapBool
	IdxLocalTypes      []detStrMapStr
	UnionMembers       []detStrStrs
}

type detProgram struct {
	Statements       []Statement
	FreeComments     []*CommentGroup
	TrailingComments *CommentGroup
	Warnings         []string
	// Errors mirrors Program.Errors. Only ASTToHIRWithMap writes that field, and
	// it runs after ParseProgram has cached the program, so a cached blob always
	// carries an empty slice — mirroring it anyway keeps the shadow honest and
	// stops the determinism test from having a reason to special-case it.
	Errors []string
	// SemPresent distinguishes a nil *SemanticContext from an empty one. gob
	// drops zero values, so a non-nil Sem with every map empty would otherwise
	// be indistinguishable from a nil one.
	SemPresent       bool
	Sem              detSem
	BuiltinFuncNames []detStrBool
}

type detCached struct {
	Prog *detProgram
}

// ---- spelling <-> structure bridges ----
//
// The deterministic shadow stores SPELLINGS, because the AST-cache fingerprint
// must stay a function of the text a program was written with: interning order,
// pool generation and pointer identity are all deliberately invisible to it.
// These four adapters are the only place the two representations meet.

func detStrings(m map[string]*ty.Type) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v.Raw()
	}
	return out
}

func detStringsMap(m map[string]map[string]*ty.Type) map[string]map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(m))
	for k, v := range m {
		out[k] = detStrings(v)
	}
	return out
}

func detTypes(m map[string]string) map[string]*ty.Type {
	if m == nil {
		return nil
	}
	out := make(map[string]*ty.Type, len(m))
	for k, v := range m {
		out[k] = tyPool.Parse(v)
	}
	return out
}

func detTypesMap(m map[string]map[string]string) map[string]map[string]*ty.Type {
	if m == nil {
		return nil
	}
	out := make(map[string]map[string]*ty.Type, len(m))
	for k, v := range m {
		out[k] = detTypes(v)
	}
	return out
}


// ---- struct -> shadow ----

func detFromSem(s *SemanticContext) detSem {
	return detSem{
		VarTypes:           detSortStrStr(detStrings(s.VarTypes)),
		EnumVariants:       detSortStrStrs(s.EnumVariants),
		DeclaredVars:       detSortStrBool(s.DeclaredVars),
		EnumVariantPayload: detSortStrMapStr(detStringsMap(s.EnumVariantPayload)),
		EnumVariantFields:  detSortStrMapStrs(s.EnumVariantFields),
		IfaceImpls:         detSortStrStrs(s.IfaceImpls),
		FuncVarTypes:       detSortStrMapStr(detStringsMap(s.FuncVarTypes)),
		FuncDeclaredVars:   detSortStrMapBool(s.FuncDeclaredVars),
		IdxLocalTypes:      detSortStrMapStr(detStringsMap(s.IdxLocalTypes)),
		UnionMembers:       detSortStrStrs(s.UnionMembers),
	}
}

func detToSem(d detSem) *SemanticContext {
	return &SemanticContext{
		VarTypes:           detTypes(detRebuildStrStr(d.VarTypes)),
		EnumVariants:       detRebuildStrStrs(d.EnumVariants),
		DeclaredVars:       detRebuildStrBool(d.DeclaredVars),
		EnumVariantPayload: detTypesMap(detRebuildStrMapStr(d.EnumVariantPayload)),
		EnumVariantFields:  detRebuildStrMapStrs(d.EnumVariantFields),
		IfaceImpls:         detRebuildStrStrs(d.IfaceImpls),
		FuncVarTypes:       detTypesMap(detRebuildStrMapStr(d.FuncVarTypes)),
		FuncDeclaredVars:   detRebuildStrMapBool(d.FuncDeclaredVars),
		IdxLocalTypes:      detTypesMap(detRebuildStrMapStr(d.IdxLocalTypes)),
		UnionMembers:       detRebuildStrStrs(d.UnionMembers),
	}
}

func detFromProgram(p *Program) *detProgram {
	if p == nil {
		return nil
	}
	d := &detProgram{
		Statements:       p.Statements,
		FreeComments:     p.FreeComments,
		TrailingComments: p.TrailingComments,
		Warnings:         p.Warnings,
		Errors:           p.Errors,
		BuiltinFuncNames: detSortStrBool(p.BuiltinFuncNames),
	}
	if p.Sem != nil {
		d.SemPresent = true
		d.Sem = detFromSem(p.Sem)
	}
	return d
}

func detToProgram(d *detProgram) *Program {
	if d == nil {
		return nil
	}
	p := &Program{
		Statements:       d.Statements,
		FreeComments:     d.FreeComments,
		TrailingComments: d.TrailingComments,
		Warnings:         d.Warnings,
		Errors:           d.Errors,
		BuiltinFuncNames: detRebuildStrBool(d.BuiltinFuncNames),
	}
	if d.SemPresent {
		p.Sem = detToSem(d.Sem)
	}
	return p
}

// GobEncode implements gob.GobEncoder with a byte-reproducible layout: the same
// CachedProgram always produces the same bytes, so a regenerated artifact can be
// compared (and skipped) byte-for-byte.
func (cp *CachedProgram) GobEncode() ([]byte, error) {
	if cp == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	d := detCached{Prog: detFromProgram(cp.Prog)}
	if err := gob.NewEncoder(&buf).Encode(&d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder, the exact inverse of GobEncode.
func (cp *CachedProgram) GobDecode(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	var d detCached
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&d); err != nil {
		return err
	}
	// 解碼只還原語法。衍生事實不在 blob 裡 —— 呼叫方必須接著呼叫
	// BuildSemViews(prog, p)（見 semviews.go）。這裡刻意不代勞：重建需要一個
	// 按「新鮮解析同一份原始碼」配置好的 Parser（跨模組簽名表、枚舉變體、
	// Filename），而 gob 解碼器沒有那些輸入。少做會靜默丟事實，故寧可讓它
	// 留在呼叫方、由門禁釘住。
	*cp = CachedProgram{Prog: detToProgram(d.Prog)}
	return nil
}
