package mir

import (
	"testing"
)

// 對普通（非 option）值做 `ok`/`err`/`nil` value-match 時，variant 臂永不匹配
// （見 sem.no 實測：i64/str subject 一律走 default）。lowering 過去把 bare
// variant 降成「subject 型別的零常量」再比較；對 struct-with-slice 這會產生
// 非法 LLVM `icmp eq %node`，opt-verify 直接失敗（min-icmp.no）。
//
// 修法：plainVariantPattern 標記這類常量，KInfix 的 ==/!= 折成 bool 常量。
// 本測試直接檢查 MIR：不得再有「兩個 struct 操作數的 eq/ne」指令。

func TestPlainStructValueMatchVariantFolded(t *testing.T) {
	src := `node {
    name str
    deps []node
}
n node
n.name = 'x'
n.deps = []
z node = n
v node = z: { ok -> it
    -> z }
print(v.name)
`
	mod := lowerSourceForTest(t, src)
	structEq := 0
	for _, inst := range mod.Insts {
		if inst.Op != OpEq && inst.Op != OpNe {
			continue
		}
		if len(inst.Args) != 2 {
			continue
		}
		at := mod.Type(mod.Value(inst.Args[0]).Type)
		bt := mod.Type(mod.Value(inst.Args[1]).Type)
		if at != nil && bt != nil && at.Kind == KindStruct && bt.Kind == KindStruct &&
			at.Raw == bt.Raw {
			structEq++
		}
	}
	if structEq != 0 {
		t.Fatalf("plain struct value-match must fold the variant test to a bool constant, got %d struct-vs-struct eq/ne instructions", structEq)
	}
}

// 對照：option subject 的 ok 臂必須照常匹配（bareVariant → TagOnly 路徑不受
// plainVariantPattern 短路影響）：其比較是 option tag，不是 struct 值比較。
func TestOptionStructValueMatchStillTagOnly(t *testing.T) {
	src := `node {
    name str
    deps []node
}
n node
n.name = 'x'
n.deps = []
z ?node = n
v ?node = z: { ok -> it
    -> z }
print(v.name)
`
	mod := lowerSourceForTest(t, src)
	tagOnly := 0
	for _, inst := range mod.Insts {
		if (inst.Op == OpEq || inst.Op == OpNe) && inst.TagOnly {
			tagOnly++
		}
	}
	if tagOnly == 0 {
		t.Fatal("option struct value-match must still emit tag-only variant comparison")
	}
}
