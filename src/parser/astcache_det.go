package parser

import (
	"bytes"
	"encoding/gob"
	"sort"
)

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
// Every map reachable from CachedProgram lives in one of three types. That set
// was established by reflecting over a decoded std blob, not by inspection:
//
//	SemanticContext   VarTypes, EnumVariants, DeclaredVars, EnumVariantPayload,
//	                  EnumVariantFields, IfaceImpls, FuncVarTypes,
//	                  FuncDeclaredVars, IdxLocalTypes
//	Program           BuiltinFuncNames
//	NodeFact          EmbedFiles
//
// Three more node-keyed tables hang off CachedProgram ITSELF rather than off a
// node type. Each is flattened to `map[uint32]...` keyed by the occurrence
// index of walkProgramNodes (see astcache.go), because gob does not preserve
// pointer identity for Node-keyed maps, and each has its own key-sorted
// shadow:
//
//	CachedProgram     Anns      -> detU32Anns      ([]*AnnotationEntry)
//	                  TypeFacts -> detU32Facts     (*TypeFact)
//	                  NodeFacts -> detU32NodeFacts (detNodeFact)
//
// (The pointer-keyed originals — SemanticContext.anns / typeFacts / nodeFacts —
// are unexported and so never encoded. `Program.FmtTrivia` is node-keyed too
// but is deliberately NOT serialized: fmt and LSP always fresh-parse. The
// fourth node-keyed table, `SemanticContext.nodeSem`, was deleted in Stage 7
// of the THIR plan once all 18 of its fields had moved out.)
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

type detStrBytes struct {
	K string
	V []byte
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

func detSortStrBytes(m map[string][]byte) []detStrBytes {
	if len(m) == 0 {
		return nil
	}
	out := make([]detStrBytes, 0, len(m))
	for k, v := range m {
		out = append(out, detStrBytes{K: k, V: v})
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

func detRebuildStrBytes(s []detStrBytes) map[string][]byte {
	out := make(map[string][]byte, len(s))
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

// detNodeFact 是 `NodeFact` 的確定性影子。
//
// `NodeFact.EmbedFiles` 是整張節點事實表裡唯一的 map 欄位：gob 對 map 按隨機
// 迭代序編碼，直接進線會讓 blob 不可重現。故在這裡排成按鍵遞增的 slice
// （`detStrBytes`），解碼時用 `detRebuildStrBytes` 還原。
//
// ⚠️ 其餘 8 欄（[]byte / CalleeKind / bool / string）gob 都能按位元組穩定編碼，
// 不需要影子化 —— 影子只服務 map。
type detNodeFact struct {
	EmbedData     []byte
	EmbedFiles    []detStrBytes
	Callee        CalleeKind
	IndexInBounds bool
	ModuleConst   bool
	SourceFile    string
	ModuleOwner   string
	VariadicUnion string
	GenericUnion  string
}

func detFromNodeFact(f *NodeFact) detNodeFact {
	if f == nil {
		return detNodeFact{}
	}
	return detNodeFact{
		EmbedData:     f.EmbedData,
		EmbedFiles:    detSortStrBytes(f.EmbedFiles),
		Callee:        f.Callee,
		IndexInBounds: f.IndexInBounds,
		ModuleConst:   f.ModuleConst,
		SourceFile:    f.SourceFile,
		ModuleOwner:   f.ModuleOwner,
		VariadicUnion: f.VariadicUnion,
		GenericUnion:  f.GenericUnion,
	}
}

func detToNodeFact(d detNodeFact) *NodeFact {
	return &NodeFact{
		EmbedData:     d.EmbedData,
		EmbedFiles:    detRebuildStrBytes(d.EmbedFiles),
		Callee:        d.Callee,
		IndexInBounds: d.IndexInBounds,
		ModuleConst:   d.ModuleConst,
		SourceFile:    d.SourceFile,
		ModuleOwner:   d.ModuleOwner,
		VariadicUnion: d.VariadicUnion,
		GenericUnion:  d.GenericUnion,
	}
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
	// Anns 是 `CachedProgram.Anns`（註解表）的確定性影子：map 迭代序非確定，
	// 故排成按鍵遞增的 slice。見 detFromAnnsMap。
	Anns []detU32Anns
	// TypeFacts 是 `CachedProgram.TypeFacts`（型別事實表）的確定性影子，同上。
	TypeFacts []detU32Facts
	// NodeFacts 是 `CachedProgram.NodeFacts`（節點事實表）的確定性影子，同上。
	// 條目值是 `detNodeFact` 而非 `*NodeFact`，因為 `NodeFact.EmbedFiles` 是
	// map —— 必須先在影子裡排好序（見 detNodeFact 的註解）。
	NodeFacts []detU32NodeFacts
}

type detU32Anns struct {
	K uint32
	V []*AnnotationEntry
}

type detU32Facts struct {
	K uint32
	V *TypeFact
}

type detU32NodeFacts struct {
	K uint32
	V detNodeFact
}

// ---- struct -> shadow ----

func detFromSem(s *SemanticContext) detSem {
	return detSem{
		VarTypes:           detSortStrStr(s.VarTypes),
		EnumVariants:       detSortStrStrs(s.EnumVariants),
		DeclaredVars:       detSortStrBool(s.DeclaredVars),
		EnumVariantPayload: detSortStrMapStr(s.EnumVariantPayload),
		EnumVariantFields:  detSortStrMapStrs(s.EnumVariantFields),
		IfaceImpls:         detSortStrStrs(s.IfaceImpls),
		FuncVarTypes:       detSortStrMapStr(s.FuncVarTypes),
		FuncDeclaredVars:   detSortStrMapBool(s.FuncDeclaredVars),
		IdxLocalTypes:      detSortStrMapStr(s.IdxLocalTypes),
		UnionMembers:       detSortStrStrs(s.UnionMembers),
	}
}

func detToSem(d detSem) *SemanticContext {
	return &SemanticContext{
		VarTypes:           detRebuildStrStr(d.VarTypes),
		EnumVariants:       detRebuildStrStrs(d.EnumVariants),
		DeclaredVars:       detRebuildStrBool(d.DeclaredVars),
		EnumVariantPayload: detRebuildStrMapStr(d.EnumVariantPayload),
		EnumVariantFields:  detRebuildStrMapStrs(d.EnumVariantFields),
		IfaceImpls:         detRebuildStrStrs(d.IfaceImpls),
		FuncVarTypes:       detRebuildStrMapStr(d.FuncVarTypes),
		FuncDeclaredVars:   detRebuildStrMapBool(d.FuncDeclaredVars),
		IdxLocalTypes:      detRebuildStrMapStr(d.IdxLocalTypes),
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

// detFromAnnsMap / detToAnnsMap 是註解表（`CachedProgram.Anns`）與其確定性
// 影子之間的轉換。與 TypeFacts / NodeFacts 同構（occurrence 編號鍵、按鍵
// 排序），唯一差別是值型別：這裡是 `[]*AnnotationEntry`。
func detFromAnnsMap(m map[uint32][]*AnnotationEntry) []detU32Anns {
	if len(m) == 0 {
		return nil
	}
	out := make([]detU32Anns, 0, len(m))
	for k, v := range m {
		out = append(out, detU32Anns{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

func detToAnnsMap(s []detU32Anns) map[uint32][]*AnnotationEntry {
	if len(s) == 0 {
		return nil
	}
	out := make(map[uint32][]*AnnotationEntry, len(s))
	for i := range s {
		out[s[i].K] = s[i].V
	}
	return out
}

// detFromTypeFactsMap / detToTypeFactsMap 是型別事實表（`CachedProgram.TypeFacts`）
// 與其確定性影子之間的轉換，與 detFromAnnsMap 同構。
func detFromTypeFactsMap(m map[uint32]*TypeFact) []detU32Facts {
	if len(m) == 0 {
		return nil
	}
	out := make([]detU32Facts, 0, len(m))
	for k, v := range m {
		if v == nil {
			continue
		}
		out = append(out, detU32Facts{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

func detToTypeFactsMap(s []detU32Facts) map[uint32]*TypeFact {
	if len(s) == 0 {
		return nil
	}
	out := make(map[uint32]*TypeFact, len(s))
	for i := range s {
		out[s[i].K] = s[i].V
	}
	return out
}

// detFromNodeFactsMap / detToNodeFactsMap 是節點事實表（`CachedProgram.NodeFacts`）
// 與其確定性影子之間的轉換，與 detFromTypeFactsMap 同構，差別只在值要經
// detFromNodeFact / detToNodeFact 過一層（EmbedFiles 排序）。
func detFromNodeFactsMap(m map[uint32]*NodeFact) []detU32NodeFacts {
	if len(m) == 0 {
		return nil
	}
	out := make([]detU32NodeFacts, 0, len(m))
	for k, v := range m {
		if v == nil {
			continue
		}
		out = append(out, detU32NodeFacts{K: k, V: detFromNodeFact(v)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

func detToNodeFactsMap(s []detU32NodeFacts) map[uint32]*NodeFact {
	if len(s) == 0 {
		return nil
	}
	out := make(map[uint32]*NodeFact, len(s))
	for i := range s {
		out[s[i].K] = detToNodeFact(s[i].V)
	}
	return out
}

// GobEncode implements gob.GobEncoder with a byte-reproducible layout: the same
// CachedProgram always produces the same bytes, so a regenerated artifact can be
// compared (and skipped) byte-for-byte.
func (cp *CachedProgram) GobEncode() ([]byte, error) {
	if cp == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	d := detCached{
		Prog:      detFromProgram(cp.Prog),
		Anns:      detFromAnnsMap(cp.Anns),
		TypeFacts: detFromTypeFactsMap(cp.TypeFacts),
		NodeFacts: detFromNodeFactsMap(cp.NodeFacts),
	}
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
	out := CachedProgram{Prog: detToProgram(d.Prog)}
	// 三張 node-keyed 表都必須在 Program 解碼完成後重建：它們靠 occurrence 走查
	// 把 uint32 鍵換回 AST 節點指針，走查的對象是剛解出的那棵樹。
	// 換回 AST 節點指針，走查的對象是剛解出的那棵樹。
	if len(d.Anns) > 0 {
		out.Anns = detToAnnsMap(d.Anns)
		RestoreAnns(out.Prog, out.Anns)
	}
	if len(d.TypeFacts) > 0 {
		out.TypeFacts = detToTypeFactsMap(d.TypeFacts)
		RestoreTypeFacts(out.Prog, out.TypeFacts)
	}
	if len(d.NodeFacts) > 0 {
		out.NodeFacts = detToNodeFactsMap(d.NodeFacts)
		RestoreNodeFacts(out.Prog, out.NodeFacts)
	}
	*cp = out
	return nil
}
