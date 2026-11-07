package parser

// place_write_value_match_test.go — D11 回归：place-match ok 臂裡
// `it = <值 match>` 必須真的寫回元素位置。
//
// 症狀（修復前）：`a[i]: { ok -> it = a[9]: { ok(it1) -> it1 / -> _ } / -> _ }`
// 能編譯、能印，但 a[i] 保持原值。原因是 rewriteItPlaceWrites 把使用者寫的
// `let it = <SurfaceMatch>` 換成合成 AssignExpression，而 H0 在 expression
// 位置降糖該值 match 時產出 cond/then 皆空的退化 KIf，整條指派被丟棄；
// 隨後的合成 store 於是寫回 match 之前綁定的原始元素值。
//
// 修法：把值 match 提升為合成 `LetStatement`（`__place_val_L_C = <match>`），
// 與源碼 `t = a[i]: { … }` 同構，再讓 `it` 從該暫存重綁。
//
// 本檔釘住兩個可觀測形狀：① 存在 `__place_val_` 合成 let 且其值是完整的
// if 鏈；② 合成 `it = …` 指派的 RHS 必須是識別符，不得是退化 KIf。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
)

const placeWriteValueMatchSrc = `pw = (i i64) (res i64) {
    a = [10, 20, 30]
    a[i]: {
        ok -> it = a[9]: {
            ok(it1) -> it1

            -> 0
        }

        -> _
    }
    res = a[1]
}
`

// TestPlaceWriteValueMatchHoistedShape 驗證 D11 提升形狀。
func TestPlaceWriteValueMatchHoistedShape(t *testing.T) {
	p := New(lexer.New(placeWriteValueMatchSrc))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	pkg, _ := ASTToHIRWithMap(prog)

	placeLet := 0
	placeLetHasIf := 0
	degenerateAssignIf := 0
	assignFromTemp := 0

	var walk func(id int32, depth int)
	walk = func(id int32, depth int) {
		if id <= hir.NoID || depth > 30 {
			return
		}
		n := pkg.Node(id)
		if n == nil {
			return
		}
		kids := pkg.Children(id)
		switch n.Kind {
		case hir.KLet:
			if strings.HasPrefix(pkg.Str(n.S), "__place_val_") {
				placeLet++
				for _, c := range kids {
					cn := pkg.Node(c)
					if cn != nil && cn.Kind == hir.KIf && len(pkg.Children(c)) > 0 {
						placeLetHasIf++
					}
				}
			}
		case hir.KAssign:
			if len(kids) >= 2 {
				lhs, rhs := pkg.Node(kids[0]), pkg.Node(kids[1])
				if lhs != nil && lhs.Kind == hir.KIdent && pkg.Str(lhs.S) == "it" {
					if rhs != nil && rhs.Kind == hir.KIf && len(pkg.Children(kids[1])) == 0 {
						degenerateAssignIf++
					}
					if rhs != nil && rhs.Kind == hir.KIdent && strings.HasPrefix(pkg.Str(rhs.S), "__place_val_") {
						assignFromTemp++
					}
				}
			}
		}
		for _, c := range kids {
			walk(c, depth+1)
		}
	}
	for _, id := range pkg.Top {
		walk(id, 0)
	}

	if placeLet == 0 {
		t.Fatal("no __place_val_ synthetic let: value match was not hoisted (place-write would silently drop the write)")
	}
	if placeLetHasIf == 0 {
		t.Fatal("hoisted __place_val_ let has no if-chain value: the value match was not lowered")
	}
	if degenerateAssignIf != 0 {
		t.Fatalf("found %d `it = <degenerate KIf>` assignment(s): the old D11 bug is back", degenerateAssignIf)
	}
	if assignFromTemp == 0 {
		t.Fatal("`it` is not rebound from the hoisted temp: place-write store would read the original element")
	}
}
