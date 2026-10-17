package main

// S4 形狀門禁（astpatch）。
//
// 背景：astpatch 用反射走訪 AST 並就地改寫。糖形態（SkipLowering）下 match 是
// SurfaceMatch，其 Arms 是 []MatchArm——值結構體、刻意不實作 parser.Node。舊走訪器
// 只看「切片元素是否實作 Node」決定要不要下鑽，於是整條 Arms 被跳過：臂條件、臂體、
// 巢狀 match 對所有 op 不可見，**且不報錯**（switch 靜默跳過）。op.go 的
// carriesNodes + walkSlice 值結構體分支補上這個缺口。
//
// 舊的 surface 形態（`SkipUnwrapLowering` + `SkipSafeIndexLowering`）已隨 §4.3 旗標
// 一起退役，本門禁現只覆蓋糖形態（正式形態）。
//
// 三條門禁：
//  1. TestArmSubtreeVisibleBothShapes —— 臂 fixture：rename 必須伸進臂內，輸出
//     須把臂內呼叫改名（少了就是靜默丟功能）。
//  2. TestValueStructSliceBlastRadius —— 把「非 Node 但承載 Node 的結構體切片」
//     的爆炸半徑釘死在 SurfaceMatch.Arms 一個。parser 以後若長出新的這類切片，
//     走訪器會開始下鑽它、行為隨之改變——本門禁失敗，逼一次 review。
//     同時釘住 MatchArm 不實作 Node（這是值結構體分支存在的理由；哪天它實作了，
//     walkSlice 的介面路徑會對值元素呼叫 Elem() 而 panic）。
//  3. TestCorpusRenameShapeParity —— 532 份語料糖形態跑同一條 rename，比 op 計數
//     與輸出可重解析性。語料裡有沒有臂內被改名呼叫由 1 承擔非空洞性；本條是前向迴歸網。
import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// armSrc 的 match 臂裡有一個 rename 必須夠到的呼叫。糖形態下臂子樹只有當反射
// 走訪器下鑽 SurfaceMatch.Arms 才可見。
const armSrc = `run = () {
    v = get-v()
    v: {
        ok -> helper(inner())
        nil -> print('n')
    }
}
helper = (a i64) (r i64) {
    r = a
}
inner = () (r i64) {
    r = 1
}
get-v = () (r ?i64) {
    r = 1
}
`

// runRename 在糖形態下解析 src 並套用 rename op，回傳（輸出, op 計數）。
func runRename(t *testing.T, src, from, to string) (string, int) {
	t.Helper()
	program, srcStr, err := parseSourceShape("probe.no", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	patch := []byte(`{"ops":[{"op":"rename","from":` + jsonStr(from) + `,"to":` + jsonStr(to) + `}]}`)
	out, n, err := applyPatch(program, srcStr, patch)
	if err != nil {
		t.Fatalf("applyPatch: %v", err)
	}
	return out, n
}

// jsonStr 把字串包成 JSON 字面量（語料檔名/函式名都是安全識別字，無需轉義）。
func jsonStr(s string) string { return `"` + s + `"` }

func TestArmSubtreeVisibleBothShapes(t *testing.T) {
	const from, to = "inner", "innerX"
	out, n := runRename(t, armSrc, from, to)

	// 非空洞：rename 必須真的伸進臂內（`inner()` → `innerX()`），
	// 且原呼叫消失。糖形態下少改任何一處都意味著臂子樹對走訪器不可見。
	if !strings.Contains(out, "innerX()") {
		t.Errorf("糖形態下臂內的 inner() 沒有被改名——SurfaceMatch.Arms 對走訪器不可見：\n%s", out)
	}
	if strings.Contains(out, "inner(") {
		t.Errorf("改名後仍殘留 inner( 呼叫：\n%s", out)
	}
	if n == 0 {
		t.Fatalf("rename 計數為 0——fixture 沒有觸發（防 0 == 0）")
	}
}

// TestValueStructSliceBlastRadius 走訪糖形態語料，收集所有「非 Node 但承載 Node
// 的結構體切片」，把集合釘死在 SurfaceMatch.Arms。
func TestValueStructSliceBlastRadius(t *testing.T) {
	// MatchArm 必須不實作 Node：這是 walkSlice 值結構體分支存在的理由。
	// 若哪天 MatchArm 實作了 Node，walkSlice 會改走介面路徑並對值元素呼叫
	// Elem()（panic）——那時要連同改 walkSlice，故在此釘住逼 review。
	nt := reflect.TypeOf((*parser.Node)(nil)).Elem()
	mt := reflect.TypeOf(parser.MatchArm{})
	if mt.Implements(nt) || reflect.PointerTo(mt).Implements(nt) {
		t.Errorf("parser.MatchArm 現在實作 Node 了：walkSlice 的值結構體分支與介面路徑"+
			"會互相打架（值元素走介面路徑會 panic），請連同 op.go 一起 review（owner=%v ptr=%v）",
			mt.Implements(nt), reflect.PointerTo(mt).Implements(nt))
	}

	type slot struct{ owner, field, elem string }
	found := map[slot]bool{}

	var walkStruct func(s reflect.Value)
	var walkField func(fv reflect.Value, owner string)

	walkStruct = func(s reflect.Value) {
		if !s.IsValid() || s.Kind() != reflect.Struct {
			return
		}
		tp := s.Type()
		for i := 0; i < tp.NumField(); i++ {
			if !tp.Field(i).IsExported() {
				continue
			}
			walkField(s.Field(i), tp.Name())
		}
	}
	walkField = func(fv reflect.Value, owner string) {
		if !fv.IsValid() {
			return
		}
		switch fv.Kind() {
		case reflect.Interface:
			if fv.IsNil() {
				return
			}
			cp := fv.Elem()
			if cp.Kind() == reflect.Ptr && !cp.IsNil() {
				walkStruct(cp.Elem())
			}
		case reflect.Ptr:
			if fv.IsNil() {
				return
			}
			walkStruct(fv.Elem())
		case reflect.Slice:
			et := fv.Type().Elem()
			if implementsNode(et) {
				// 介面/Node 切片（[]Statement / []Expression）：舊走訪器本來就會
				// 下鑽，不是新增面——但仍要走訪元素，否則到不了 SurfaceMatch。
				for i := 0; i < fv.Len(); i++ {
					walkField(fv.Index(i), owner)
				}
				return
			}
			if et.Kind() == reflect.Struct && carriesNodes(et) {
				found[slot{owner, "", et.String()}] = true
				for i := 0; i < fv.Len(); i++ {
					walkStruct(fv.Index(i))
				}
			}
		}
	}

	files := corpusFiles(t)
	parsed := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		p := parser.New(lexer.New(string(data)))
		p.SkipLowering = true // 糖形態：SurfaceMatch 只存在於此形態
		prog := p.ParseProgram()
		if prog == nil || len(p.Errors()) > 0 {
			continue
		}
		parsed++
		walkStruct(reflect.ValueOf(prog).Elem())
	}
	if parsed == 0 {
		t.Fatal("語料一份都沒解析成功——門禁空洞")
	}

	want := []slot{{"SurfaceMatch", "", "parser.MatchArm"}}
	got := make([]slot, 0, len(found))
	for s := range found {
		got = append(got, s)
	}
	sort.Slice(got, func(i, j int) bool {
		if got[i].owner != got[j].owner {
			return got[i].owner < got[j].owner
		}
		return got[i].elem < got[j].elem
	})
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("爆炸半徑不再是 1（SurfaceMatch.Arms []parser.MatchArm）：\n"+
			"  got  %v\n  want %v\n"+
			"parser 長出了新的「非 Node 但承載 Node 的結構體切片」，走訪器會開始下鑽它、"+
			"astpatch 的改寫行為隨之改變——請 review 這個新切片是否應該被下鑽。",
			got, want)
	}
	t.Logf("語料 %d/%d 份（糖形態）；爆炸半徑 = %v", parsed, len(files), got)
}

// TestCorpusRenameShapeParity 對每份語料檔取第一個函式定義名，糖形態跑同一條
// rename，比較 op 計數並確認輸出能重新解析。只用 rename（不涉 replace/insert 的
// snippet 拼接），因此不觸及「snippet 形態必須與主樹一致」那條獨立的坑。
//
// 輸出比對前置：輸出必須能重新解析。糖形態輸出壞了 = 正式形態的產物壞了，直接 FAIL。
// surface 形態（`SkipUnwrapLowering` + `SkipSafeIndexLowering`）已隨 §4.3 旗標一起退役，
// 本門禁現只覆蓋糖形態（正式形態）。
func TestCorpusRenameShapeParity(t *testing.T) {
	files := corpusFiles(t)
	parsed, renamed, skipped := 0, 0, 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		src := string(data)

		prog, _, err := parseSourceShape(f, src)
		if err != nil {
			skipped++
			continue
		}
		parsed++

		name := firstFuncName(prog)
		if name == "" {
			name = "__zz_absent_name__" // 無函式定義：rename 計數為 0，仍有價值
		}
		to := name + "_zz"
		patch := []byte(`{"ops":[{"op":"rename","from":` + jsonStr(name) + `,"to":` + jsonStr(to) + `}]}`)

		out, n, err := applyPatch(prog, src, patch)
		if err != nil {
			t.Errorf("%s: sugar applyPatch: %v", f, err)
			continue
		}
		if n > 0 {
			renamed++
		}
		if errs := reparseErrs(out); len(errs) > 0 {
			t.Errorf("%s: 糖形態（正式形態）輸出無法重新解析：%v", f, errs)
			continue
		}
	}
	t.Logf("語料 %d/%d 份（parsed=%d skipped=%d）；實際發生 rename 的檔案 %d 份",
		len(files), len(files), parsed, skipped, renamed)
	if renamed == 0 {
		t.Errorf("沒有任何一份語料觸發 rename——前向迴歸網空洞（0 == 0）")
	}
}

// reparseErrs 用糖形態重新解析一段輸出，回傳解析錯誤（空 = 輸出是合法的 .no）。
func reparseErrs(code string) []string {
	p := parser.New(lexer.New(code))
	p.SkipLowering = true
	p.ParseProgram()
	return p.Errors()
}

// corpusFiles 收集 532 份語料（<repo>/tests/**/*.no）。
func corpusFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir("../../../tests", func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".no") {
			files = append(files, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk corpus: %v", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("語料為空——門禁空洞")
	}
	return files
}

// firstFuncName 回傳程式裡第一個函式定義的名字（無則空字串）。
func firstFuncName(prog *parser.Program) string {
	for _, st := range prog.Statements {
		if fd, ok := st.(*parser.FunctionDefinition); ok && fd.Name != "" {
			return fd.Name
		}
	}
	return ""
}
