package parser

import (
	"bytes"
	"encoding/gob"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// 這一組測試鎖住「option 算術型別鏈」的推斷：只要鏈上任何一環拿不到型別，
// 值位 match 的主語就會退化成 `<slot> == 0`（`ok` 被當成常量 0），臂永不命中，
// 而失敗是**靜默**的——觀測到的只是「ok 臂裡的賦值沒發生」。
//
// 歷史事故：aes.dec-block 的輪次計數器（`nr = ek.len() / 16 - 1` → `round` →
// `round-opt`）整條鏈無型別 ⇒ 迴圈不遞減 ⇒ `test-std/src/aes.no` 掛死；
// number.i64.sqrt 的二分迴圈（`n = .` → `hi = n / 2` → `mid`）同理。

// TestOptionArithChainCallRHS：`n = data.len() / 16` 要推出 ?i64。
// 這條需要 builtinContainerMethodReturnType（`.len()` 在簽名表裡沒有條目）。
func TestOptionArithChainCallRHS(t *testing.T) {
	src := `v = (data []byte) (r i64) {
    n = data.len() / 16
    n: {
        ok -> print('ok=' + it.to-str())

        nil -> print('nil')

        err -> print('err')
    }
    r = 0
}

d []byte = with-len(176)
v(d)
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=n t=?i64") {
		t.Fatalf("`data.len() / 16` 應推斷為 ?i64，HIR 內找不到 `let s=n t=?i64`:\n%s", dump)
	}
	// ok 臂的 it 必須綁成 payload 型別，否則 MIR 零填。
	if !strings.Contains(dump, "let s=it t=i64") {
		t.Fatalf("ok 臂的 it 應綁成 i64:\n%s", dump)
	}
}

// TestOptionArithChainOptionOperand：運算元已是 ?i64 時，`round - 1` 仍是 ?i64。
// checker 的口徑可由 `y i64 = round - 1` 觸發 15w45dqk 證實，parser 必須一致。
func TestOptionArithChainOptionOperand(t *testing.T) {
	src := `v = (nr i64) (r i64) {
    round = nr - 1
    round-opt = round - 1
    round-opt: {
        ok -> round = it
        ->
    }
    r = 0
}

print(v(10))
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=round t=?i64") {
		t.Fatalf("round 應為 ?i64:\n%s", dump)
	}
	if !strings.Contains(dump, "let s=round-opt t=?i64") {
		t.Fatalf("`round - 1` 的運算元是 ?i64，結果仍應為 ?i64:\n%s", dump)
	}
}

// TestOptionArithChainLiteralOperand：無標註的整數字面量 let（`lo = 1`）要能被
// astLocalVarType 回放成 i64，否則 `mid = (lo + hi) / 2` 無型別。
func TestOptionArithChainLiteralOperand(t *testing.T) {
	src := `v = (n i64) (r i64) {
    lo = 1
    hi = n / 2
    mid = (lo + hi) / 2
    mid: {
        ok -> r = it
        -> r = 0
    }
    r = 0
}

print(v(10))
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=mid t=?i64") {
		t.Fatalf("`(lo + hi) / 2` 應推斷為 ?i64（lo 為字面量 i64）:\n%s", dump)
	}
}

// TestOptionArithChainSelfRHS：方法內 `n = .` 要取到接收者型別，否則
// `hi = n / 2` 起整條鏈無型別（number.i64.sqrt 的二分迴圈掛死）。
func TestOptionArithChainSelfRHS(t *testing.T) {
	src := `i64.half-chain = () (r i64) {
    n = .
    hi = n / 2
    hi: {
        ok -> r = it
        -> r = 0
    }
    r = 0
}

print(10.half-chain())
`
	dump := dumpHIR(t, src)
	// 注意：`n = .` 本身不是 option 算術，let 節點不帶 InferredType（與
	// `x = f()` 同口徑）；可觀測的契約是**下游**能取到型別。
	if !strings.Contains(dump, "let s=hi t=?i64") {
		t.Fatalf("`n / 2` 應推斷為 ?i64:\n%s", dump)
	}
}

// TestOptionArithChainSelfFieldTemporary：接收者欄位參與算術時，暫時綁定也必須
// 保留 ?T；否則後續對暫時變數的值位 match 沒有型別，ok 臂會靜默失效。
func TestOptionArithChainSelfFieldTemporary(t *testing.T) {
	src := `holder {
    pos i64
}

holder.advance = (n i64) (out i64) {
    delta = self.pos + n
    out = delta: {
        ok -> it
        -> 0
    }
}
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=delta t=?i64") {
		t.Fatalf("self.pos + n 應推斷為 ?i64，HIR 內找不到 `let s=delta t=?i64`:\n%s", dump)
	}
	if !strings.Contains(dump, "let s=it t=i64") {
		t.Fatalf("ok 臂的 it 應綁成 i64:\n%s", dump)
	}
}

func TestOptionArithChainSelfFieldAssignmentTemporary(t *testing.T) {
	src := `holder {
    pos i64
}

holder.advance = (n i64) {
    step = self.pos + 1
    delta = self.pos + n
    self.pos = delta: {
        ok -> it
        ->
    }
}
`
	cfg := SemRebuildConfig{Filename: "self-field-assignment.no"}
	p := New(lexer.NewCached(cfg.Filename, src))
	cfg.Apply(p)
	fresh := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	dump := ASTToHIR(fresh).Dump()
	if !strings.Contains(dump, "let s=delta t=?i64") {
		t.Fatalf("field-target value match must retain delta as ?i64:\n%s", dump)
	}
	if !strings.Contains(dump, "let s=it t=i64") {
		t.Fatalf("field-target value match must bind it as i64:\n%s", dump)
	}
	if got := fresh.Sem.InferredType(findLetByName(t, fresh, "step")); got != "" {
		t.Fatalf("unrelated receiver arithmetic acquired inferred type %q", got)
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&CachedProgram{Prog: fresh}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	BuildSemViews(decoded.Prog, cfg)
	if got := ASTToHIR(decoded.Prog).Dump(); got != dump {
		t.Fatalf("field-target inference diverged after cache rebuild (%d vs %d bytes)", len(got), len(dump))
	}
	if got := decoded.Prog.Sem.InferredType(findLetByName(t, decoded.Prog, "step")); got != "" {
		t.Fatalf("cache rebuild gave unrelated receiver arithmetic inferred type %q", got)
	}
}

// TestOptionArithChainSelfFieldCacheRoundTrip：接收者欄位推斷在 fresh parse 與
// CachedProgram 重建兩條路徑上必須一致；否則快取命中會改變接收者欄位算術的 option 型別。
func TestOptionArithChainSelfFieldCacheRoundTrip(t *testing.T) {
	src := `holder {
    n i64
}

holder.first = () (out ?i64) {
    {
        #{overflow=wrap}
        item = .n - 1
        out = item: {
            ok -> it
            -> nil
        }
    }: {
        err -> out = nil
        -> _
    }
}
`
	cfg := SemRebuildConfig{Filename: "self-field-cache.no"}
	p := New(lexer.NewCached(cfg.Filename, src))
	cfg.Apply(p)
	fresh := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&CachedProgram{Prog: fresh}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	BuildSemViews(decoded.Prog, cfg)

	want := ASTToHIR(fresh).Dump()
	got := ASTToHIR(decoded.Prog).Dump()
	if got != want {
		t.Fatalf("self-field inference diverged after cache rebuild (%d vs %d bytes)", len(got), len(want))
	}
	if !strings.Contains(want, "t=?i64") {
		t.Fatalf("self-field index read lost its option type:\n%s", want)
	}
}

// TestValueMatchNestedMethodReceiverInference locks the receiver-dependent type
// chain used by nested method traversals. The post-parse inference pass runs after
// the parse-time method stack has unwound, but must still resolve `.sources[i]` as
// ?holder and the following `source.columns[j]` as ?column.
func TestValueMatchNestedMethodReceiverInference(t *testing.T) {
	src := `column {
    name str
}

empty-column = () (out column) {
    out = column{
        name: ''
    }
}

holder {
    columns []column
}

empty-holder = () (out holder) {
    out = holder{
        columns: []
    }
}

engine {
    sources []holder
}

engine.expand = () {
    i <- [0.. .sources.len()): {
        source = .sources[i]: {
            ok -> it

            -> empty-holder()
        }
        j <- [0..source.columns.len()): {
            column = source.columns[j]: {
                ok -> it

                -> empty-column()
            }
            print(column.name)
        }
    }
}
`
	dump := dumpHIR(t, src)
	for _, want := range []string{
		"let s=source t=holder",
		"let s=column t=column",
		"let s=it t=column",
	} {
		if !strings.Contains(dump, want) {
			t.Fatalf("nested method value match lost %q:\n%s", want, dump)
		}
	}
	foundColumnSubject := false
	for _, line := range strings.Split(dump, "\n") {
		if strings.Contains(line, "let s=__match_subj_") && strings.Contains(line, "t=?column") {
			foundColumnSubject = true
			break
		}
	}
	if !foundColumnSubject {
		t.Fatalf("inner safe-index subject was not normalized to ?column:\n%s", dump)
	}

	cfg := SemRebuildConfig{Filename: "nested-method-value-match.no"}
	p := New(lexer.NewCached(cfg.Filename, src))
	cfg.Apply(p)
	fresh := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	want := ASTToHIR(fresh).Dump()

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&CachedProgram{Prog: fresh}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	BuildSemViews(decoded.Prog, cfg)
	if got := ASTToHIR(decoded.Prog).Dump(); got != want {
		t.Fatalf("nested method value-match inference diverged after cache rebuild (%d vs %d bytes)", len(got), len(want))
	}
}

// TestReceiverInferenceScopedToValueMatchDependencies locks the boundary between
// receiver-dependent temporaries that feed value matches and unrelated self-field
// arithmetic. Only the former may recover the method receiver after parsing.
func TestReceiverInferenceScopedToValueMatchDependencies(t *testing.T) {
	src := `holder {
    table-size i64
}

holder.build-table = () (out i64) {
    #{overflow=wrap}
    step = (.table-size >> 1) + (.table-size >> 3) + 3

    #{overflow=wrap}
    mask = .table-size - 1

    delta = .table-size + 1
    out = delta: {
        ok -> it
        -> 0
    }
}
`
	cfg := SemRebuildConfig{Filename: "scoped-receiver-inference.no"}
	p := New(lexer.NewCached(cfg.Filename, src))
	cfg.Apply(p)
	fresh := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	for _, name := range []string{"step", "mask"} {
		if got := fresh.Sem.InferredType(findLetByName(t, fresh, name)); got != "" {
			t.Fatalf("unrelated self-field binding %s acquired inferred type %q", name, got)
		}
	}
	if got := fresh.Sem.InferredType(findLetByName(t, fresh, "delta")); got != "?i64" {
		t.Fatalf("value-match dependency delta lost ?i64: %q", got)
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&CachedProgram{Prog: fresh}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	BuildSemViews(decoded.Prog, cfg)
	for _, name := range []string{"step", "mask"} {
		if got := decoded.Prog.Sem.InferredType(findLetByName(t, decoded.Prog, name)); got != "" {
			t.Fatalf("cache rebuild gave unrelated binding %s inferred type %q", name, got)
		}
	}
	if got := decoded.Prog.Sem.InferredType(findLetByName(t, decoded.Prog, "delta")); got != "?i64" {
		t.Fatalf("cache rebuild lost delta ?i64: %q", got)
	}
}

// TestLowerSurfaceBindsProgram：astLocalVarType 的 AST 回放依賴 p.prog，
// 生產路徑原本從不賦值（只有單測手動設定），於是那條兜底在真實編譯裡恆回空。
// 這個測試釘住 LowerSurface 的綁定，避免它再次靜默失效。
func TestLowerSurfaceBindsProgram(t *testing.T) {
	src := `v = (data []byte) (r i64) {
    n = data.len()
    r = 0
}

d []byte = with-len(16)
v(d)
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	p.LowerSurface(prog)
	if p.prog == nil {
		t.Fatalf("LowerSurface 之後 p.prog 必須非 nil，否則 astLocalVarType 恆回空字串")
	}
}
