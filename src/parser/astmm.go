package parser

// MetaMessage 承載用的 AST ↔ 遞歸節點樹（ASTTree）編解碼。
//
// 形態：**一棵完整的遞歸 AST 樹**。每個 AST 節點 → 一個 ASTNode（kind + 標量
// cell + 子節點 Kids），子節點就是樹邊；不再有 1-based 索引引用、不再有「扁平
// 索引節點表」，也不再有「切割提升根森林」——mm ≥0.2.13 的容器上限已放開到
// 4GB（string 仍 64KB），整棵樹可原樣塞進單一 mm 文檔，因此本檔只負責忠實地把
// AST 展開成一棵樹，切分/封裝交給上層。
//
// 歷史制約（已解除，留檔備註）：
//   - C8 自引用爆栈：mm ≤0.2.10 反射編碼遇 []*Node/*Node 無限遞歸；0.2.11 補
//     refCtx 路徑棧迴圈檢測後可正常展開。
//   - maxDepth：0.2.11=32、0.2.12=256、0.2.13=1024——真樹最深嵌套（實測 ≈216）
//     已在預算內，故移除深度切割。
//   - 容器 64KB（Max2Byte）：0.2.13 起放到 4GB，故移除按位元組的切割提升森林。
//
// 仍有效的 mm 約束：
//   - string ≤65535：單一 cell 值超標仍要切段（見 splitLongCells 的 #N 協議）；
//     []byte 走 base64 後同理分段，**不使用 JSON**。
//   - C2 零值拒绝：標量「缺席即零值」；空容器 allow_empty（解碼退化出一個零值
//     元素，sanitize 統一剔除）。
//   - C1 介面欄位：樹型別全是具體結構，Kids 是 []ASTNode 值語義。
//
// 集合型欄位（map / 非節點 slice / []byte / 定長陣列）不再用 JSON blob，而是
// 用一組**專用容器節點**原樣展開（見下方 kind* 常數與 value/setValue）。
//
// 語義副表 nodeSem（未導出 map[Node]*NodeSemantics）沒有索引可引用：改為在被
// 登記節點的樹位置上內嵌一個 ASTSemEntry 包裝子節點（KidF "__sem__"），解碼端
// 純遞歸拼回屬主。

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// MMWireVersion 是節點樹格式的格式版本。AST 形狀（ast.go）或本檔案的編碼規則
// 任一變化都必須 bump，否則舊 .ast.zip 會解出結構不同的樹。
// 2026-10-15.3：mm v0.2.13 起改「單棵完整遞歸樹」——移除切割提升根森林與 JSON
// blob，集合型欄位改用專用容器節點（MMSlice/MMMap/MMBytes/MMVal/MMEntry）。
const MMWireVersion = "2026-10-15.3"

// mmCellMax 是單一 cell 值的字串上限。mm 對單一字串仍有 65535 位元組的硬限制
// （實測：鍵名前綴+tag 還要佔額外位元組），超過就必須切段（見 splitLongCells
// 的 #N 後綴協議）。容器已放開到 4GB，唯獨字串仍受此限。
const mmCellMax = 56000

// 專用容器節點的 kind 名（不是任何 Go 型別名，故不入 mmKindTypes；編解碼端
// 一律先比對這些常數，命中才走容器路徑，否則按具體 AST 節點型別查註冊表）。
const (
	kindVal   = "MMVal"   // 一個標量值（出現在 slice 元素 / map 鍵值等「無名位置」）
	kindSlice = "MMSlice" // 非節點 slice / 定長陣列：Kids 依序是元素
	kindMap   = "MMMap"   // map：Kids 依 key 規範序排列，每個是 MMEntry
	kindBytes = "MMBytes" // []byte / []uint8：base64 分段存進 cell "b"
	kindEntry = "MMEntry" // map 的一項：KidF=["Key","Value"]
)

// 語義包裝節點的欄位名（不是任何 Go 欄位名）。
const semKidName = "__sem__"

// ASTTree 是一整個模組 AST 的具體化表示：**單棵完整遞歸樹**。
type ASTTree struct {
	Ver    string
	AstVer string
	Nodes  int32    // 節點總數（校驗/日誌）
	Root   *ASTNode // 樹根（Program）
}

// ASTNode 是樹上的一個節點：kind + 標量 cell + 子節點（樹邊）。
//   - 具體 AST 節點：K 是 Go 結構型別名，C 是標量欄位，KidF/Kids 是節點型欄位
//     （切片欄位以「同名連續 run」展開），集合型欄位以單一容器子節點展開。
//   - 容器節點（kind* 常數）：K 是容器名，KidF/C 依容器語義解釋（見 value 系列）。
type ASTNode struct {
	K    string    `mm:"desc=節點型別名或容器名"`
	C    []MMCell  `mm:"allow_empty;desc=標量欄位"`
	KidF []string  `mm:"allow_empty;desc=子節點欄位名（DFS 序，與 Kids 一一對應）"`
	Kids []ASTNode `mm:"allow_empty;desc=子節點"`
}

// MMCell 是一個標量欄位。T 決定 V 的解釋方式：
//
//	s  字串      i  整數      f  浮點（十六進位精確格式）   b  布林（只存 true）
//	n  nil（無名位置的空介面/空指標，V 恆空）
type MMCell struct {
	N string `mm:"desc=欄位名"`
	T string `mm:"desc=值類型"`
	V string `mm:"desc=值"`
}

// ASTSemEntry 是 nodeSem 語義條目的樹內包裝：挂在被登記節點的 Kids 下
// （KidF 欄位名 "__sem__"），Sem 指向 NodeSemantics 子樹。
type ASTSemEntry struct {
	Sem *NodeSemantics
}

// mmKindTypes 是 kind 名 → 具體型別的註冊表，供解碼用。
// 與 gobNodeTypes 同源（見 gob_register.go），另加幾個只作為「具體結構欄位」
// 出現、永遠不會躲在介面後面的型別。
var mmKindTypes = map[string]reflect.Type{}

func init() {
	for _, v := range gobNodeTypes {
		t := reflect.TypeOf(v).Elem()
		mmKindTypes[t.Name()] = t
	}
	// 完整註冊表 = gobNodeTypes（躲在介面後的型別）
	// + 本包/lexer 所有結構體（具體欄位型別，如 Program / Parameter /
	// FuncSignature / CommentedNode / lexer.Token）。清單由 scripts 機械產生，
	// 新增結構體忘記註冊會讓解碼在語料測試上直接失敗。
	for _, v := range []any{
		ASTNode{},
		ASTSemEntry{},
		ASTTree{},
		AnnotationArrayValue{},
		AnnotationBoolValue{},
		AnnotationEntry{},
		AnnotationIdentValue{},
		AnnotationIntValue{},
		AnnotationRangeValue{},
		AnnotationStatement{},
		AnnotationStringValue{},
		AnnotationStructField{},
		AnnotationStructValue{},
		ArrayLiteral{},
		ArrayType{},
		AssignExpression{},
		AwaitExpression{},
		BlockStatement{},
		BooleanLiteral{},
		BreakStatement{},
		CachedProgram{},
		CallExpression{},
		CastExpression{},
		CharLiteral{},
		CoExpression{},
		Comment{},
		CommentGroup{},
		CommentedNode{},
		ConditionalExpression{},
		ContinueStatement{},
		Diagnostic{},
		DotExpression{},
		EnumDefinition{},
		EnumValue{},
		ExportStatement{},
		ExpressionStatement{},
		ExternStatement{},
		FloatLiteral{},
		ForStatement{},
		FormatField{},
		FormatSegment{},
		FormatSpec{},
		FuncSignature{},
		FunctionDefinition{},
		FunctionLiteral{},
		FunctionType{},
		GroupedExpression{},
		Identifier{},
		IfExpression{},
		InBoundsIndex{},
		IndexExpression{},
		InfixExpression{},
		IntegerLiteral{},
		InterfaceDefinition{},
		InterfaceMethod{},
		IterationExpr{},
		LetStatement{},
		MMCell{},
		MapLiteral{},
		MapPair{},
		MapType{},
		MultiAssignStatement{},
		NamedType{},
		NilLiteral{},
		NodeSemantics{},
		NullableType{},
		Parameter{},
		Parser{},
		PointerType{},
		PrefixExpression{},
		Program{},
		RangeExpression{},
		RegexLiteral{},
		ReturnStatement{},
		RunExpression{},
		SemanticContext{},
		SliceExpression{},
		SliceLiteral{},
		SliceType{},
		StringLiteral{},
		StructDefinition{},
		StructField{},
		StructLiteral{},
		SurfaceMatch{},
		TaggedEnumDefinition{},
		TaggedEnumField{},
		TaggedEnumVariant{},
		TypeAlias{},
		UnionType{},
		UnwrapAssignStatement{},
		UseStatement{},
		UserASTCache{},
		ViewType{},
		boundsAnalyzer{},
		captureNode{},
		detCached{},
		detNodeSem{},
		detProgram{},
		detSem{},
		detStrBool{},
		detStrBytes{},
		detStrMapBool{},
		detStrMapStr{},
		detStrMapStrs{},
		detStrStr{},
		detStrStrs{},
		detU32Sem{},
		hirConv{},
		lexState{},
		lowerer{},
		matchArm{},
		mmDecoder{},
		mmEncoder{},
		parsePanic{},
		parserState{},
		returnTypeInfo{},
		lexer.Lexer{},
		lexer.LexerState{},
		lexer.Position{},
		lexer.Token{},
	} {
		t := reflect.TypeOf(v)
		mmKindTypes[t.Name()] = t
	}
}

// MMKinds 返回目前註冊的所有 kind 名（測試用：確保與 gob 註冊表一致）。
func MMKinds() []string {
	out := make([]string, 0, len(mmKindTypes))
	for k := range mmKindTypes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── 編碼 ────────────────────────────────────────────────────────────────────

type mmEncoder struct {
	count  int32                      // 已建節點總數
	semOcc map[uintptr]*NodeSemantics // nodeSem 按底層節點指標索引（同一指標的每次出現都掛包裝）
	onPath map[uintptr]bool           // 路徑棧：真環（AST 正常不該有）直接報錯
}

// EncodeASTTree 把 *Program 展開成單棵完整遞歸樹。srcSHA 可為空（由 manifest 承載）。
func EncodeASTTree(p *Program, srcSHA string) (*ASTTree, error) {
	if p == nil {
		return nil, fmt.Errorf("mm: nil program")
	}
	e := &mmEncoder{semOcc: map[uintptr]*NodeSemantics{}, onPath: map[uintptr]bool{}}
	if p.Sem != nil {
		for n, ns := range p.Sem.nodeSem {
			if n == nil || ns == nil {
				continue
			}
			e.semOcc[reflect.ValueOf(n).Pointer()] = ns
		}
	}
	root, err := e.node(reflect.ValueOf(p))
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("mm: program root encoded to nil")
	}
	return &ASTTree{
		Ver:    MMWireVersion,
		AstVer: ASTVersion,
		Nodes:  e.count,
		Root:   root,
	}, nil
}

// node 編碼一個節點值（介面／指標／結構體），返回樹節點；nil 返回 nil。
func (e *mmEncoder) node(v reflect.Value) (*ASTNode, error) {
	var id uintptr
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, nil
		}
		if v.Kind() == reflect.Pointer {
			id = v.Pointer()
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("mm: expected struct node, got %s", v.Kind())
	}
	if id != 0 {
		if e.onPath[id] {
			return nil, fmt.Errorf("mm: cyclic AST node %s (tree form cannot encode cycles)", v.Type().Name())
		}
		e.onPath[id] = true
		defer delete(e.onPath, id)
	}

	kind := v.Type().Name()
	if kind == "" {
		return nil, fmt.Errorf("mm: anonymous struct type in AST")
	}
	n := &ASTNode{K: kind}
	e.count++

	addCell := func(c *MMCell) { n.C = append(n.C, *c) }
	addKid := func(fieldName string, kid *ASTNode) {
		if kid == nil {
			return
		}
		n.KidF = append(n.KidF, fieldName)
		n.Kids = append(n.Kids, *kid)
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // 未導出欄位：gob 也同樣丟棄，保真度對齊 gob
		}
		if err := e.field(f.Name, v.Field(i), n, addCell, addKid); err != nil {
			return nil, fmt.Errorf("mm: %s.%s: %w", kind, f.Name, err)
		}
	}

	// 語義副表包裝：nodeSem 以指標身份為鍵，同一節點每次樹上出現都掛一份，
	// 解碼端按「屬主節點 → Sem」配對拼回。
	if id != 0 {
		if sem, ok := e.semOcc[id]; ok {
			wrap := &ASTNode{K: "ASTSemEntry", KidF: []string{"Sem"}}
			sv, err := e.node(reflect.ValueOf(sem))
			if err != nil {
				return nil, fmt.Errorf("mm: %s node_sem: %w", kind, err)
			}
			if sv == nil {
				return nil, fmt.Errorf("mm: %s node_sem wrapped nil", kind)
			}
			wrap.Kids = append(wrap.Kids, *sv)
			addKid(semKidName, wrap)
		}
	}

	n.C = splitLongCells(n.C)
	return n, nil
}

// field 把一個具名欄位編進節點 n：
//   - 標量 → cell；
//   - 節點型（介面/指標/結構體）→ 單一子節點；
//   - 節點切片（元素是介面/指標/結構體）→ 同名連續 run 子節點；
//   - 非節點切片 / []byte / 定長陣列 / map → 單一「容器子節點」（e.value）。
func (e *mmEncoder) field(name string, fv reflect.Value, n *ASTNode, addCell func(*MMCell), addKid func(string, *ASTNode)) error {
	switch fv.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Struct:
		kid, err := e.node(fv)
		if err != nil {
			return err
		}
		addKid(name, kid)
		return nil

	case reflect.Slice, reflect.Array:
		if fv.Kind() == reflect.Slice && fv.Len() == 0 {
			return nil
		}
		if elemIsNode(fv.Type().Elem()) && fv.Kind() == reflect.Slice {
			// 節點切片：逐元素作為同名 run（解碼端按 run 分組還原列表欄位）。
			for i := 0; i < fv.Len(); i++ {
				kid, err := e.node(fv.Index(i))
				if err != nil {
					return err
				}
				addKid(name, kid)
			}
			return nil
		}
		// 標量切片 / []byte / 定長陣列：一個容器子節點。
		kid, err := e.value(fv)
		if err != nil {
			return err
		}
		addKid(name, kid)
		return nil

	case reflect.Map:
		if fv.Len() == 0 {
			return nil
		}
		kid, err := e.value(fv)
		if err != nil {
			return err
		}
		addKid(name, kid)
		return nil

	case reflect.String:
		if fv.String() == "" {
			return nil
		}
		addCell(&MMCell{N: name, T: "s", V: fv.String()})
		return nil

	case reflect.Bool:
		if !fv.Bool() {
			return nil // false 以「缺席」表示（mm 不接受空值）
		}
		addCell(&MMCell{N: name, T: "b", V: "true"})
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Int() == 0 {
			return nil
		}
		addCell(&MMCell{N: name, T: "i", V: strconv.FormatInt(fv.Int(), 10)})
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if fv.Uint() == 0 {
			return nil
		}
		addCell(&MMCell{N: name, T: "i", V: strconv.FormatUint(fv.Uint(), 10)})
		return nil

	case reflect.Float32, reflect.Float64:
		if fv.Float() == 0 {
			return nil
		}
		// 十六進位浮點：精確往返，不受格式化位數影響
		addCell(&MMCell{N: name, T: "f", V: strconv.FormatFloat(fv.Float(), 'x', -1, 64)})
		return nil
	}
	return fmt.Errorf("unsupported kind %s", fv.Kind())
}

// value 把一個「無名位置」的值（切片元素、map 鍵/值、巢狀容器）編成容器節點。
// 具名標量欄位走 cell（見 field），此處只處理無法用欄位名表達的巢狀形狀。
func (e *mmEncoder) value(v reflect.Value) (*ASTNode, error) {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return &ASTNode{K: kindVal, C: []MMCell{{N: "v", T: "n"}}}, nil
		}
		if elemIsNode(v.Type()) && v.Kind() == reflect.Pointer {
			return e.node(v) // 指向具體 AST 節點
		}
		return e.value(v.Elem())

	case reflect.Struct:
		return e.node(v)

	case reflect.Slice, reflect.Array:
		et := v.Type().Elem()
		if et.Kind() == reflect.Uint8 {
			return e.bytes(v), nil
		}
		n := &ASTNode{K: kindSlice}
		for i := 0; i < v.Len(); i++ {
			el, err := e.value(v.Index(i))
			if err != nil {
				return nil, err
			}
			n.Kids = append(n.Kids, *el)
		}
		return split(n), nil

	case reflect.Map:
		n := &ASTNode{K: kindMap}
		type kv struct {
			key, val *ASTNode
			sort     string
		}
		entries := make([]kv, 0, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			kd, err := e.value(iter.Key())
			if err != nil {
				return nil, err
			}
			vd, err := e.value(iter.Value())
			if err != nil {
				return nil, err
			}
			entries = append(entries, kv{kd, vd, canonKey(iter.Key())})
		}
		// map 迭代序不確定；依 key 規範序排出，保證 wire 逐位元組可重現。
		sort.Slice(entries, func(i, j int) bool { return entries[i].sort < entries[j].sort })
		for _, en := range entries {
			entry := &ASTNode{K: kindEntry, KidF: []string{"Key", "Value"}}
			entry.Kids = append(entry.Kids, *en.key, *en.val)
			n.Kids = append(n.Kids, *entry)
		}
		return split(n), nil

	case reflect.String:
		return split(&ASTNode{K: kindVal, C: []MMCell{{N: "v", T: "s", V: v.String()}}}), nil
	case reflect.Bool:
		// 無名位置（slice 元素/map 鍵值）無法用「缺席」表示 false，一律寫出字面量。
		return &ASTNode{K: kindVal, C: []MMCell{{N: "v", T: "b", V: strconv.FormatBool(v.Bool())}}}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return split(&ASTNode{K: kindVal, C: []MMCell{{N: "v", T: "i", V: strconv.FormatInt(v.Int(), 10)}}}), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return split(&ASTNode{K: kindVal, C: []MMCell{{N: "v", T: "i", V: strconv.FormatUint(v.Uint(), 10)}}}), nil
	case reflect.Float32, reflect.Float64:
		return split(&ASTNode{K: kindVal, C: []MMCell{{N: "v", T: "f", V: strconv.FormatFloat(v.Float(), 'x', -1, 64)}}}), nil
	}
	return nil, fmt.Errorf("mm: unsupported value kind %s", v.Kind())
}

// bytes 把 []byte／[]uint8／定長位元組陣列編成 MMBytes（base64，超標自動分段）。
// 這不是 JSON——只是一個 base64 字串cell。
func (e *mmEncoder) bytes(v reflect.Value) *ASTNode {
	buf := make([]byte, v.Len())
	for i := 0; i < v.Len(); i++ {
		buf[i] = byte(v.Index(i).Uint())
	}
	return split(&ASTNode{K: kindBytes, C: []MMCell{{N: "b", T: "s", V: base64.StdEncoding.EncodeToString(buf)}}})
}

// split 對一個容器節點的 cell 做超長分段（其子節點已在各自建立時處理過）。
func split(n *ASTNode) *ASTNode {
	n.C = splitLongCells(n.C)
	return n
}

// canonKey 給 map key 生成一個穩定排序字串（真實資料全是 string key）。
func canonKey(k reflect.Value) string {
	for k.Kind() == reflect.Interface || k.Kind() == reflect.Pointer {
		if k.IsNil() {
			return ""
		}
		k = k.Elem()
	}
	switch k.Kind() {
	case reflect.String:
		return k.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "i:" + strconv.FormatInt(k.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "u:" + strconv.FormatUint(k.Uint(), 10)
	default:
		return fmt.Sprintf("%v", k.Interface())
	}
}

// elemIsNode 判定切片/map 的元素型別是否為 AST 節點（介面／指標／結構體）。
// 節點元素走樹邊（run 或具體節點），非節點元素走容器節點展開。
func elemIsNode(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Struct:
		return true
	}
	return false
}

// splitLongCells 把超過 mmCellMax 的 cell 值切成多段，段名加 "#0" "#1" … 後綴。
// mm 對單一字串有 65535 位元組硬限制，來源裡的大字串字面量會撞上。
func splitLongCells(cells []MMCell) []MMCell {
	need := false
	for _, c := range cells {
		if len(c.V) > mmCellMax {
			need = true
			break
		}
	}
	if !need {
		return cells
	}
	out := make([]MMCell, 0, len(cells))
	for _, c := range cells {
		if len(c.V) <= mmCellMax {
			out = append(out, c)
			continue
		}
		for i, start := 0, 0; start < len(c.V); i++ {
			end := start + mmCellMax
			if end > len(c.V) {
				end = len(c.V)
			}
			out = append(out, MMCell{N: fmt.Sprintf("%s#%d", c.N, i), T: c.T, V: c.V[start:end]})
			start = end
		}
	}
	return out
}

// ── 解碼 ────────────────────────────────────────────────────────────────────

type mmDecoder struct {
	semCtx  *SemanticContext // 建出的唯一 SemanticContext（nodeSem 回填目標）
	pending []semPair        // 屬主 → 語義條目（全部節點建完後統一回填）
}

type semPair struct {
	owner Node
	entry *ASTSemEntry
}

// DecodeASTTree 把單棵樹還原成 *Program。版本不符一律硬錯（不靜默回退）。
func DecodeASTTree(t *ASTTree) (*Program, error) {
	if t == nil {
		return nil, fmt.Errorf("mm: nil ast tree")
	}
	if t.Ver != MMWireVersion {
		return nil, fmt.Errorf("mm: wire version mismatch: blob %q, this compiler %q", t.Ver, MMWireVersion)
	}
	if t.AstVer != ASTVersion {
		return nil, fmt.Errorf("mm: AST version mismatch: blob %q, this compiler %q", t.AstVer, ASTVersion)
	}
	if t.Root == nil {
		return nil, fmt.Errorf("mm: empty ast tree")
	}
	sanitize(t.Root)
	d := &mmDecoder{}
	v, err := d.build(t.Root)
	if err != nil {
		return nil, err
	}
	p, ok := v.Interface().(*Program)
	if !ok {
		return nil, fmt.Errorf("mm: root node is %T, want *parser.Program", v.Interface())
	}
	if len(d.pending) > 0 {
		if d.semCtx == nil {
			return nil, fmt.Errorf("mm: %d node_sem entries but no SemanticContext built", len(d.pending))
		}
		if d.semCtx.nodeSem == nil {
			d.semCtx.nodeSem = map[Node]*NodeSemantics{}
		}
		for _, pr := range d.pending {
			if pr.entry.Sem == nil {
				return nil, fmt.Errorf("mm: __sem__ wrapper for %T never filled", pr.owner)
			}
			d.semCtx.nodeSem[pr.owner] = pr.entry.Sem
		}
	}
	return p, nil
}

// build 把一個具體 AST 節點還原成 *T，純遞歸把子節點接回欄位。容器/標量由
// setValue 處理（依目標欄位型別解釋）。
func (d *mmDecoder) build(n *ASTNode) (reflect.Value, error) {
	typ, ok := mmKindTypes[n.K]
	if !ok {
		return reflect.Value{}, fmt.Errorf("mm: unknown node kind %q (registry has %d kinds)", n.K, len(mmKindTypes))
	}
	if len(n.KidF) != len(n.Kids) {
		return reflect.Value{}, fmt.Errorf("mm: %s kidf %d != kids %d", n.K, len(n.KidF), len(n.Kids))
	}
	pv := reflect.New(typ)
	sv := pv.Elem()

	if n.K == "SemanticContext" && d.semCtx == nil {
		d.semCtx = pv.Interface().(*SemanticContext)
	}

	// cell 可能因超長被切成 name#0 / name#1 …，先按原名合併。
	cells, order, kinds := mergeCells(n.C)
	for _, name := range order {
		f := sv.FieldByName(name)
		if !f.IsValid() {
			return reflect.Value{}, fmt.Errorf("mm: %s has no field %q", n.K, name)
		}
		if err := d.setCell(f, kinds[name], cells[name]); err != nil {
			return reflect.Value{}, fmt.Errorf("mm: %s.%s: %w", n.K, name, err)
		}
	}

	i := 0
	for i < len(n.Kids) {
		name := n.KidF[i]
		if name == semKidName {
			if err := d.attachSem(pv, &n.Kids[i]); err != nil {
				return reflect.Value{}, err
			}
			i++
			continue
		}
		f := sv.FieldByName(name)
		if !f.IsValid() {
			return reflect.Value{}, fmt.Errorf("mm: %s has no field %q", n.K, name)
		}
		// 節點切片欄位：同名連續 run → 列表；其餘欄位（含容器節點）→ 單子節點。
		if f.Kind() == reflect.Slice && elemIsNode(f.Type().Elem()) {
			j := i + 1
			for j < len(n.Kids) && n.KidF[j] == name {
				j++
			}
			sl := reflect.MakeSlice(f.Type(), j-i, j-i)
			for k := i; k < j; k++ {
				cv, err := d.build(&n.Kids[k])
				if err != nil {
					return reflect.Value{}, err
				}
				if err := assignNode(sl.Index(k-i), cv); err != nil {
					return reflect.Value{}, fmt.Errorf("mm: %s.%s[%d]: %w", n.K, name, k-i, err)
				}
			}
			f.Set(sl)
			i = j
			continue
		}
		if err := d.setValue(f, &n.Kids[i]); err != nil {
			return reflect.Value{}, fmt.Errorf("mm: %s.%s: %w", n.K, name, err)
		}
		i++
	}
	return pv, nil
}

// setValue 依目標型別 dst 把一個容器/標量/節點子樹寫回 dst（dst 必須可位址）。
//   - MMVal：標量（或 nil）
//   - MMBytes：[]byte／定長位元組陣列（base64 合併後解碼）
//   - MMSlice：切片／定長陣列（逐元素遞迴）
//   - MMMap：map（逐 MMEntry 建鍵值）
//   - 其他 K：具體 AST 節點 → build + assignNode
func (d *mmDecoder) setValue(dst reflect.Value, n *ASTNode) error {
	switch n.K {
	case kindVal:
		cells, order, kinds := mergeCells(n.C)
		if len(order) == 0 {
			return nil
		}
		name := order[0]
		if kinds[name] == "n" {
			return nil // nil：保持零值
		}
		return d.setCell(dst, kinds[name], cells[name])

	case kindBytes:
		cells, order, _ := mergeCells(n.C)
		if len(order) == 0 {
			return nil
		}
		raw, err := base64.StdEncoding.DecodeString(cells[order[0]])
		if err != nil {
			return fmt.Errorf("mm: base64: %w", err)
		}
		switch dst.Kind() {
		case reflect.Slice:
			b := reflect.MakeSlice(dst.Type(), len(raw), len(raw))
			reflect.Copy(b, reflect.ValueOf(raw))
			dst.Set(b)
		case reflect.Array:
			for i := 0; i < dst.Len() && i < len(raw); i++ {
				dst.Index(i).SetUint(uint64(raw[i]))
			}
		default:
			return fmt.Errorf("mm: MMBytes into %s", dst.Kind())
		}
		return nil

	case kindSlice:
		switch dst.Kind() {
		case reflect.Slice:
			sl := reflect.MakeSlice(dst.Type(), len(n.Kids), len(n.Kids))
			for i := range n.Kids {
				if err := d.setValue(sl.Index(i), &n.Kids[i]); err != nil {
					return fmt.Errorf("mm: slice[%d]: %w", i, err)
				}
			}
			dst.Set(sl)
		case reflect.Array:
			for i := range n.Kids {
				if i >= dst.Len() {
					return fmt.Errorf("mm: array overflow (%d > %d)", i, dst.Len())
				}
				if err := d.setValue(dst.Index(i), &n.Kids[i]); err != nil {
					return fmt.Errorf("mm: array[%d]: %w", i, err)
				}
			}
		default:
			return fmt.Errorf("mm: MMSlice into %s", dst.Kind())
		}
		return nil

	case kindMap:
		mk := reflect.MakeMapWithSize(dst.Type(), len(n.Kids))
		kt, vt := dst.Type().Key(), dst.Type().Elem()
		for i := range n.Kids {
			en := &n.Kids[i]
			if en.K != kindEntry || len(en.Kids) != 2 {
				return fmt.Errorf("mm: MMMap entry %d malformed (K=%q kids=%d)", i, en.K, len(en.Kids))
			}
			k := reflect.New(kt).Elem()
			if err := d.setValue(k, &en.Kids[0]); err != nil {
				return fmt.Errorf("mm: map key %d: %w", i, err)
			}
			v := reflect.New(vt).Elem()
			if err := d.setValue(v, &en.Kids[1]); err != nil {
				return fmt.Errorf("mm: map value %d: %w", i, err)
			}
			mk.SetMapIndex(k, v)
		}
		dst.Set(mk)
		return nil

	default:
		cv, err := d.build(n)
		if err != nil {
			return err
		}
		return assignNode(dst, cv)
	}
}

// attachSem 把 "__sem__" 子節點（ASTSemEntry 包裝）配對到屬主節點。Sem 直接由
// 純遞歸建全，故此刻 entry.Sem 已就緒，但仍統一在收尾時寫入 nodeSem（保持單一出口）。
func (d *mmDecoder) attachSem(owner reflect.Value, wrap *ASTNode) error {
	ev, err := d.build(wrap)
	if err != nil {
		return err
	}
	entry, ok := ev.Interface().(*ASTSemEntry)
	if !ok {
		return fmt.Errorf("mm: __sem__ wrapper is %T, want *parser.ASTSemEntry", ev.Interface())
	}
	node, ok := owner.Interface().(Node)
	if !ok {
		return fmt.Errorf("mm: node_sem owner %T does not implement Node", owner.Interface())
	}
	d.pending = append(d.pending, semPair{owner: node, entry: entry})
	return nil
}

// mergeCells 把（可能超長分段成 name#0/#1… 的）cell 依原名合併，回傳值、首次
// 出現序、以及每個原名對應的型別。
func mergeCells(cs []MMCell) (val map[string]string, order []string, kinds map[string]string) {
	val = map[string]string{}
	kinds = map[string]string{}
	for _, c := range cs {
		base := c.N
		if i := strings.LastIndex(base, "#"); i > 0 {
			base = base[:i]
		}
		if _, seen := val[base]; !seen {
			order = append(order, base)
			kinds[base] = c.T
		}
		val[base] += c.V
	}
	return
}

// sanitize 剔除 allow_empty 空容器解碼回來的零值退化項（實測：空 slice 會退化出
// 一個全零元素）。合法節點的 K/cell.N 永遠非空，可以直接丟。
func sanitize(n *ASTNode) {
	cs := n.C[:0]
	for _, c := range n.C {
		if c.N == "" && c.T == "" && c.V == "" {
			continue
		}
		cs = append(cs, c)
	}
	n.C = cs
	fs := n.KidF[:0]
	for _, f := range n.KidF {
		if f == "" {
			continue
		}
		fs = append(fs, f)
	}
	n.KidF = fs
	ks := n.Kids[:0]
	for i := range n.Kids {
		if n.Kids[i].K == "" {
			continue
		}
		sanitize(&n.Kids[i])
		ks = append(ks, n.Kids[i])
	}
	n.Kids = ks
}

func (d *mmDecoder) setCell(f reflect.Value, t, v string) error {
	switch t {
	case "s":
		if f.Kind() != reflect.String {
			return fmt.Errorf("cell type s on %s", f.Kind())
		}
		f.SetString(v)
	case "i":
		switch f.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			f.SetInt(n)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return err
			}
			f.SetUint(n)
		default:
			return fmt.Errorf("cell type i on %s", f.Kind())
		}
	case "f":
		if f.Kind() != reflect.Float32 && f.Kind() != reflect.Float64 {
			return fmt.Errorf("cell type f on %s", f.Kind())
		}
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		f.SetFloat(x)
	case "b":
		if f.Kind() != reflect.Bool {
			return fmt.Errorf("cell type b on %s", f.Kind())
		}
		f.SetBool(v == "true")
	default:
		return fmt.Errorf("unknown cell type %q", t)
	}
	return nil
}

// assignNode 把解出的節點指標 cv 指派給欄位 f（介面／指標／結構體三種形狀）。
func assignNode(f, cv reflect.Value) error {
	if !cv.IsValid() {
		return nil // nil 節點：欄位保持零值
	}
	switch f.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !cv.Type().AssignableTo(f.Type()) && !cv.Type().Implements(f.Type()) {
			return fmt.Errorf("cannot assign %s to %s", cv.Type(), f.Type())
		}
		f.Set(cv)
	case reflect.Struct:
		if cv.Kind() != reflect.Pointer {
			return fmt.Errorf("expected pointer node for struct field, got %s", cv.Kind())
		}
		f.Set(cv.Elem())
	default:
		return fmt.Errorf("cannot assign node to %s field", f.Kind())
	}
	return nil
}
