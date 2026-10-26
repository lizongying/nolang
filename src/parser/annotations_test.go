package parser

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// ─────────────────────────────────────────────────────────────────────────────
// `#{...}` 條目住在 AST 節點上（`AnnotatedNode.Annotations`）—— 語法即真相。
//
// 為什麼必須這樣：Stage 3 曾把註解搬進 `SemanticContext.anns`（節點指針鍵的
// side-table），而**尾隨註解**（`x = v[5] #{index-out=zero}`）只寫表、不落節點。
// 後果有三：
//   - 「從 AST 重建註解」在原理上不可能（表裡有、樹上沒有）；
//   - `CloneProgram` / 模組合併各需一條手工通道，漏一處就靜默丟註解；
//   - 跨進程要另開一條 occurrence 編號的序列化通道。
//
// 現在條目隨節點走：gob 的樹編碼天然帶上它，clone/merge 天然跟隨，
// `AnnotationsOf` / `PlatformKeysOf` / `GenericParamsOf` 退化成對節點的純查詢。
// ─────────────────────────────────────────────────────────────────────────────

// semTableLens 是兩張 node-keyed 語義表（型別事實 / 節點事實）的條目數快照。
// 註解不再是表，故不在此列 —— 「寫某表不動其他表」的判别力由這兩格承擔。
func semTableLens(sem *SemanticContext) [2]int {
	return [2]int{sem.TypeFactsTab().Len(), sem.NodeFactsTab().Len()}
}

// annFingerprint 把一組條目壓成可比較的字符串（含 Trailing / Propagated，
// 因為這兩者決定 formatter 是否重複打印、以及 desugar 的目標定位）。
func annFingerprint(ents []*AnnotationEntry) string {
	if len(ents) == 0 {
		return ""
	}
	var b strings.Builder
	for _, e := range ents {
		if e == nil {
			b.WriteString("<nil>;")
			continue
		}
		fmt.Fprintf(&b, "%s|t=%v|p=%v;", e.String(), e.Trailing, e.Propagated)
	}
	return b.String()
}

// annTestNode 返回一個可攜帶註解的節點（`StructField` 嵌了 `AnnotatedNode`）。
func annTestNode() *StructField {
	return &StructField{Name: "n"}
}

// TestAnnotationSetOverwrites pins the overwrite semantics: a rewriting pass
// (e.g. `moveOverflowAnnotations`) replaces the whole entry set. Appending
// instead would re-trigger overflow handling.
func TestAnnotationSetOverwrites(t *testing.T) {
	sem := NewSemanticContext()
	n := annTestNode()
	sem.SetRawAnnotations(n, []*AnnotationEntry{{Key: "a"}, {Key: "b"}})
	if got := len(sem.AnnotationsOf(n)); got != 2 {
		t.Fatalf("first Set recorded %d entries, want 2", got)
	}
	sem.SetRawAnnotations(n, []*AnnotationEntry{{Key: "c"}})
	ents := sem.AnnotationsOf(n)
	if len(ents) != 1 || ents[0].Key != "c" {
		t.Fatalf("Set did not overwrite: %d entries left", len(ents))
	}
}

// TestAnnotationAccessorsAreNilSafe：非可註解節點、nil 節點、nil Sem 都必須
// 靜默降級（解碼後的 Program 可能帶 nil Sem，走訪器仍會問這些訪問器）。
func TestAnnotationAccessorsAreNilSafe(t *testing.T) {
	sem := NewSemanticContext()
	// 非可註解節點：寫入是 no-op，讀取為 nil。
	plain := &Identifier{Value: "x"}
	sem.SetRawAnnotations(plain, []*AnnotationEntry{{Key: "a"}})
	if sem.AnnotationsOf(plain) != nil {
		t.Error("a non-annotatable node must report no annotations")
	}
	// nil 節點。
	sem.SetRawAnnotations(nil, []*AnnotationEntry{{Key: "a"}})
	if sem.AnnotationsOf(nil) != nil {
		t.Error("nil node must report no annotations")
	}
	if sem.PlatformKeysOf(nil) != nil || sem.GenericParamsOf(nil) != nil {
		t.Error("nil node must yield no platform keys / generic params")
	}
	// nil Sem：receiver 不參與查詢，仍應讀到節點上的條目。
	var nilSem *SemanticContext
	n := annTestNode()
	setAnnotationsOf(n, []*AnnotationEntry{{Key: "overflow"}})
	if got := nilSem.AnnotationsOf(n); len(got) != 1 {
		t.Errorf("nil Sem must still see node-resident annotations, got %d", len(got))
	}
}

// TestAnnWritesDoNotTouchFactTables is the separation assertion: writing an
// annotation must not create entries in the two fact tables.
func TestAnnWritesDoNotTouchFactTables(t *testing.T) {
	sem := NewSemanticContext()
	n := annTestNode()
	before := semTableLens(sem)
	sem.SetRawAnnotations(n, []*AnnotationEntry{{Key: "overflow"}})
	after := semTableLens(sem)
	if after != before {
		t.Errorf("SetRawAnnotations touched a fact table: before=%v after=%v", before, after)
	}
	if got := len(sem.RawAnnotationsOf(n)); got != 1 {
		t.Fatalf("annotation not readable after write: %d entries", got)
	}
}

// TestAnnotationsOfEqualsRawAnnotationsOf pins the unification: the two
// accessors read one store, so the "has ResolveProgram run yet?" distinction
// that forced `AnnotationsOf(n) || RawAnnotationsOf(n)` downstream is gone.
func TestAnnotationsOfEqualsRawAnnotationsOf(t *testing.T) {
	sem := NewSemanticContext()
	n := annTestNode()
	// Write through one accessor, read through the other, WITHOUT ResolveProgram.
	sem.SetRawAnnotations(n, []*AnnotationEntry{{Key: "overflow"}})
	if len(sem.AnnotationsOf(n)) != 1 {
		t.Error("AnnotationsOf must see entries written via SetRawAnnotations")
	}
	// And the reverse direction.
	m := annTestNode()
	sem.SetAnnotations(m, []*AnnotationEntry{{Key: "index-out"}})
	if len(sem.RawAnnotationsOf(m)) != 1 {
		t.Error("RawAnnotationsOf must see entries written via SetAnnotations")
	}
}

// TestPlatformKeysOfIsPureFunction pins that platform keys are computed from the
// entries on demand, not materialised by ResolveProgram.
func TestPlatformKeysOfIsPureFunction(t *testing.T) {
	sem := NewSemanticContext()
	n := annTestNode()
	sem.SetRawAnnotations(n, []*AnnotationEntry{
		{Key: "mac-arm64"}, // ValidPlatformKeys member (boolean key)
		{Key: "overflow", Value: &AnnotationIdentValue{Value: "wrap"}}, // not a platform key
	})
	before := semTableLens(sem)
	// No ResolveProgram call — deliberately.
	keys := sem.PlatformKeysOf(n)
	if len(keys) != 1 || keys[0] != "mac-arm64" {
		t.Fatalf("PlatformKeysOf = %v, want [mac-arm64]", keys)
	}
	if after := semTableLens(sem); after != before {
		t.Errorf("PlatformKeysOf is not pure: table lengths changed %v -> %v", before, after)
	}
}

// TestGenericParamsOfIsPureFunction is the same pin for `#{generic=[K,V]}`.
func TestGenericParamsOfIsPureFunction(t *testing.T) {
	sem := NewSemanticContext()
	n := annTestNode()
	sem.SetRawAnnotations(n, []*AnnotationEntry{{
		Key:   "generic",
		Value: &AnnotationArrayValue{Elements: []AnnotationValue{&AnnotationIdentValue{Value: "K"}}},
	}})
	if got := sem.GenericParamsOf(n); len(got) != 1 || got[0] != "K" {
		t.Fatalf("GenericParamsOf = %v, want [K]", got)
	}
}

// TestTrailingAnnotationLandsOnNode 是**回归门禁**，直指当年那个缺口：
// `x = v[5] #{index-out=zero}` 的条目必须落在**所属陈述**上，而不是只进一张表。
// 全仓有 59 处尾随注解（其中 18 处 `#{index-out}`），丢一个就是静默的错误代码。
//
// ⚠️ 用 `SkipLowering = true`（surface 树）断言：`#{index-out}` 是**降糖期的
// 输入**，`maybeIndexOutAssign` 会把整条 `l = a[i]` 换成 `*BlockStatement`，
// 注解随原节点一起离树。这是既有行为（旧的 side-table 也一样丢，因为 blob 走
// 的是降糖后的树），不是本轮的回归。真正必须承载注解的是 surface 树 ——
// 也正是 Stage D 要缓存的那棵树。
func TestTrailingAnnotationLandsOnNode(t *testing.T) {
	src := "f = (a []i64, i i64) (r i64) {\n" +
		"    l = a[i] #{index-out=zero}\n" +
		"    r = l + 1\n" +
		"}\n"
	p := New(lexer.NewCached("trailing.no", src))
	p.Filename = "trailing.no"
	p.SkipLowering = true
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}

	var found []*AnnotationEntry
	walkProgramNodes(prog, func(n Node) {
		for _, e := range annotationsOf(n) {
			if e.Key == "index-out" && e.Trailing {
				found = append(found, e)
			}
		}
	})
	if len(found) == 0 {
		t.Fatal("anti-vacuity: the trailing `#{index-out=zero}` never landed on a node — " +
			"this is exactly the defect that made AST-side rebuild impossible")
	}
}

// TestAnnotationsSurviveCacheRoundTrip：blob 命中必须让注解原样回来。
// 丢注解不会报错 —— 平台过滤会留下 mac-arm64 专属函数、overflow 处理会静默消失
// （历史上 `nolang-overflow-ineffective` 伪阳性只在缓存命中时出现）。
//
// 现在注解随 AST 的 gob 树编码往返，不再有独立通道；本测试守住这个新事实。
//
// 两种形态都测：默认（降糖后，今天的 blob 形态）与 surface（Stage D 的目标形态）。
// 尾随注解只在 surface 形态下必然存在 —— `#{index-out}` 是降糖期的输入，承载它的
// 语句被 `maybeIndexOutAssign` 换掉后注解随节点离树（既有行为）。
func TestAnnotationsSurviveCacheRoundTrip(t *testing.T) {
	src := "#{mac-arm64}\ntarget = 1\n\n#{overflow=wrap}\nother = 2\n\n" +
		"f = (a []i64, i i64) (r i64) {\n" +
		"    l = a[i] #{index-out=zero}\n" +
		"    r = l + 1\n" +
		"}\n"

	cases := []struct {
		name         string
		skipLowering bool
		wantTrailing bool
	}{
		{"lowered", false, false},
		{"surface", true, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			p := New(lexer.NewCached("ann-round-trip.no", src))
			p.Filename = "ann-round-trip.no"
			p.SkipLowering = c.skipLowering
			prog := p.ParseProgram()
			if prog == nil || len(p.Errors()) > 0 {
				t.Fatalf("parse failed: %v", p.Errors())
			}

			before := collectAnnFingerprints(prog)
			if len(before) == 0 {
				t.Fatal("anti-vacuity: no annotations attached, the round-trip is vacuous")
			}

			cp := &CachedProgram{Prog: prog}
			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(cp); err != nil {
				t.Fatalf("encode: %v", err)
			}
			var dcp CachedProgram
			if err := gob.NewDecoder(&buf).Decode(&dcp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if dcp.Prog == nil {
				t.Fatal("decoded CachedProgram has a nil Program")
			}

			after := collectAnnFingerprints(dcp.Prog)
			if len(after) != len(before) {
				t.Fatalf("annotation occurrences changed across the blob round-trip: before=%d after=%d",
					len(before), len(after))
			}
			for i := range before {
				if before[i] != after[i] {
					t.Fatalf("annotation #%d changed across the blob round-trip:\n  before=%q\n  after =%q",
						i, before[i], after[i])
				}
			}

			// 平台键与 overflow 必须仍可派生（驱动 build 的平台过滤与 codegen）。
			foundPlatform, foundOverflow, foundTrailing := false, false, false
			walkProgramNodes(dcp.Prog, func(n Node) {
				if ks := dcp.Prog.Sem.PlatformKeysOf(n); len(ks) == 1 && ks[0] == "mac-arm64" {
					foundPlatform = true
				}
				for _, e := range annotationsOf(n) {
					if e.Key == "overflow" {
						foundOverflow = true
					}
					if e.Key == "index-out" && e.Trailing {
						foundTrailing = true
					}
				}
			})
			if !foundPlatform {
				t.Error("platform key lost across the blob round-trip; FilterByPlatform would " +
					"silently drop platform-specific code on a cache HIT")
			}
			if !foundOverflow {
				t.Error("overflow annotation lost across the blob round-trip")
			}
			if foundTrailing != c.wantTrailing {
				t.Errorf("trailing annotation presence = %v, want %v (shape %s)",
					foundTrailing, c.wantTrailing, c.name)
			}
		})
	}
}

// collectAnnFingerprints 按 occurrence 序收集树中每个带注解节点的指纹，
// 用来做「往返前后逐条相等」的比对。
func collectAnnFingerprints(prog *Program) []string {
	var out []string
	walkProgramNodes(prog, func(n Node) {
		if fp := annFingerprint(annotationsOf(n)); fp != "" {
			out = append(out, fmt.Sprintf("%T:%s", n, fp))
		}
	})
	return out
}

// TestParsePopulatesAnnotations 是语料级反空洞：真实解析必须把注解填到节点上。
func TestParsePopulatesAnnotations(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.no"))
	if err != nil || len(files) == 0 {
		t.Skipf("no corpus under ../../tests: %v", err)
	}
	const sample = 80
	checked, withAnns, totalNodes, trailing := 0, 0, 0, 0
	for i, path := range files {
		if i >= sample {
			break
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		p := New(lexer.NewCached(path, string(raw)))
		p.Filename = path
		prog := p.ParseProgram() // full lowering, the build shape
		if prog == nil || len(p.Errors()) > 0 {
			continue
		}
		checked++
		n := 0
		walkProgramNodes(prog, func(node Node) {
			ents := annotationsOf(node)
			if len(ents) == 0 {
				return
			}
			n++
			for _, e := range ents {
				if e.Trailing {
					trailing++
				}
			}
		})
		if n > 0 {
			withAnns++
		}
		totalNodes += n
	}
	if checked < 20 {
		t.Fatalf("anti-vacuity: only %d corpus files parsed cleanly, want >= 20", checked)
	}
	if withAnns == 0 || totalNodes == 0 {
		t.Fatalf("anti-vacuity: no annotated nodes across %d parsed corpus files", checked)
	}
	t.Logf("annotated nodes: %d across %d/%d files (%d trailing entries)",
		totalNodes, withAnns, checked, trailing)
}

// TestAnnotationsLiveOnAST 是全语料的一致性门禁：注解只能在节点上，
// 且树中必须真的存在一批注解（否则一切断言恒真）。
func TestAnnotationsLiveOnAST(t *testing.T) {
	files := corpusForAnnotations(t)
	annotatedNodes, trailing, checked := 0, 0, 0

	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		p := New(lexer.NewCached(path, string(src)))
		p.Filename = filepath.Base(path)
		prog := p.ParseProgram()
		if prog == nil || len(p.Errors()) > 0 {
			continue // 故意写坏的 fixture
		}
		checked++

		walkProgramNodes(prog, func(n Node) {
			ents := annotationsOf(n)
			if len(ents) == 0 {
				return
			}
			annotatedNodes++
			for _, e := range ents {
				if e.Trailing {
					trailing++
				}
			}
		})
	}

	if checked < 400 {
		t.Fatalf("only %d files parsed cleanly; expected the bulk of the corpus", checked)
	}
	if annotatedNodes < 500 {
		t.Fatalf("only %d annotated nodes found across %d files; the node-field write "+
			"path is broken (this gate would pass vacuously)", annotatedNodes, checked)
	}
	if trailing < 10 {
		t.Fatalf("only %d trailing annotation entries found; trailing annotations are the "+
			"ones that used to be lost entirely", trailing)
	}
	t.Logf("%d annotated node occurrences (%d trailing) across %d files", annotatedNodes, trailing, checked)
}

// corpusForAnnotations 收集语料：tests/*.no + src/std 下的全部 .no。
// 后者是尾随注解的集中地（src/std/yaml.no:338 等），必须覆盖。
func corpusForAnnotations(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.no"))
	if err != nil {
		t.Fatalf("glob tests: %v", err)
	}
	err = filepath.WalkDir(filepath.Join("..", "std"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".no") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk std: %v", err)
	}
	return files
}

// TestCloneProgramCopiesAnnotations pins the clone contract: annotations travel
// with the cloned nodes. Before the move to node-resident storage this needed a
// hand-written `AnnTable.Clone(remap)`; now it falls out of the AST deep copy —
// but it must be *proved*, because union monomorphization clones statements and
// a lost annotation there is silent.
func TestCloneProgramCopiesAnnotations(t *testing.T) {
	src := "#{overflow=wrap}\nf = (i i64) (r i64) {\n  r = i + 1\n  r\n}\n"
	p := New(lexer.NewCached("ann-clone.no", src))
	p.Filename = "ann-clone.no"
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	before := collectAnnFingerprints(prog)
	if len(before) == 0 {
		t.Fatal("anti-vacuity: no annotations to clone")
	}
	cp := CloneProgram(prog)
	after := collectAnnFingerprints(cp)
	if len(after) != len(before) {
		t.Fatalf("clone lost annotations: before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("annotation #%d changed in the clone:\n  before=%q\n  after =%q",
				i, before[i], after[i])
		}
	}

	// 副本上的条目必须是副本节点的字段，不能与模板共享同一个 AnnotationEntry
	// 指针（否则对副本写注解会污染模板）。
	origEntries := map[*AnnotationEntry]bool{}
	walkProgramNodes(prog, func(n Node) {
		for _, e := range annotationsOf(n) {
			origEntries[e] = true
		}
	})
	shared := 0
	walkProgramNodes(cp, func(n Node) {
		for _, e := range annotationsOf(n) {
			if origEntries[e] {
				shared++
			}
		}
	})
	if shared > 0 {
		t.Fatalf("%d annotation entry pointer(s) shared between the clone and the template", shared)
	}
}
