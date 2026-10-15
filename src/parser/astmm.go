package parser

// MetaMessage 承載用的 AST ↔ 節點表（NodeTable）編解碼。
//
// 為什麼不直接 `mm.EncodeFromValue(program)`：MetaMessage v0.2.9 的反射編碼器
// 明確拒絕 interface{}（AST 的 Expression / Type / Statement / Node 全是介面），
// 且拒絕任何零值（0、""、false、空 slice、nil 指針——bool 連 allow_empty 標籤都
// 不支援）。所以這裡先把 AST **具體化**成一張扁平節點表：
//
//   - 每個節點 = kind 名 + 一串 cell（欄位），cell 的值一律是**非空字串**；
//   - 零值欄位直接省略（解碼時取 Go 零值），天然繞開「not allow empty」；
//   - 子節點用 1-based 索引引用（"n" / "l"），索引是字串所以永遠非空；
//   - 集合型欄位（[]string、map[string]bool …）用 JSON 字串裝；
//   - 巢狀具體結構（lexer.Token、lexer.Position）也當成節點，不特判。
//
// 這樣整棵樹的深度恆定（≈6），又沒有任何零值，且形狀是自描述的。
// 分塊（每片編碼後 ≤ 65535 位元組）由 parser 之外的 mmast 套件負責。

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// MMWireVersion 是節點表自身的格式版本。AST 形狀（ast.go）或本檔案的編碼規則
// 任一變化都必須 bump，否則舊 .ast.zip 會解出結構不同的樹。
const MMWireVersion = "2026-10-15.1"

// mmCellMax 是單一 cell 值的字串上限。mm v0.2.9 對單一字串有 65535 位元組的硬
// 限制，超過就必須切段（見 addCell 的 #N 後綴協議）。
const mmCellMax = 60000

// NodeTable 是一整個模組 AST 的具體化表示：扁平節點表 + 根節點索引。
type NodeTable struct {
	Ver    string   `mm:"desc=節點表格式版本"`
	AstVer string   `mm:"desc=parser.ASTVersion"`
	Root   int32    `mm:"desc=根節點索引(1-based)"`
	Nodes  []NTNode `mm:"desc=節點表"`
}

// NTNode 是一個具體節點：kind + 欄位 cell 列表。
type NTNode struct {
	K string   `mm:"desc=節點型別名"`
	C []NTCell `mm:"desc=欄位"`
}

// NTCell 是一個欄位。T 決定 V 的解釋方式：
//
//	s  字串      i  整數      f  浮點（十六進位精確格式）   b  布林（只存 true）
//	n  子節點索引           l  子節點索引列表（逗號分隔）
//	j  JSON（集合型欄位）    by Base64（[]byte）
type NTCell struct {
	N string `mm:"desc=欄位名"`
	T string `mm:"desc=值類型"`
	V string `mm:"desc=值"`
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
	// FuncSignature / CommentedNode / lexer.Token）。清單由
	// scripts 機械產生（見 TestMMKindRegistryCoversAllStructs 守衛），
	// 新增結構體忘記註冊會讓解碼在語料測試上直接失敗。
	for _, v := range []any{
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
		MapLiteral{},
		MapPair{},
		MapType{},
		MultiAssignStatement{},
		NTCell{},
		NTNode{},
		NamedType{},
		NilLiteral{},
		NodeSemantics{},
		NodeTable{},
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
	nodes []NTNode
	memo  map[uintptr]int32 // 已編碼節點的指標 → 索引（AST 可能共享子節點）
}

// EncodeNodeTable 把 *Program 具體化成節點表。srcSHA 可為空。
func EncodeNodeTable(p *Program, srcSHA string) (*NodeTable, error) {
	if p == nil {
		return nil, fmt.Errorf("mm: nil program")
	}
	e := &mmEncoder{memo: map[uintptr]int32{}}
	root, err := e.node(reflect.ValueOf(p))
	if err != nil {
		return nil, err
	}
	return &NodeTable{
		Ver:    MMWireVersion,
		AstVer: ASTVersion,
		Root:   root,
		Nodes:  e.nodes,
	}, nil
}

// node 編碼一個節點值（介面／指標／結構體），返回 1-based 索引；nil 返回 0。
func (e *mmEncoder) node(v reflect.Value) (int32, error) {
	var id uintptr
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, nil
		}
		if v.Kind() == reflect.Pointer {
			id = v.Pointer()
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, fmt.Errorf("mm: expected struct node, got %s", v.Kind())
	}
	if id != 0 {
		if idx, ok := e.memo[id]; ok {
			return idx, nil
		}
	}

	idx := int32(len(e.nodes) + 1)
	e.nodes = append(e.nodes, NTNode{}) // 先佔位，子節點索引才能大於自己
	if id != 0 {
		e.memo[id] = idx
	}

	kind := v.Type().Name()
	if kind == "" {
		return 0, fmt.Errorf("mm: anonymous struct type at node %d", idx)
	}
	var cells []NTCell
	add := func(c *NTCell) { cells = append(cells, *c) }

	// 語義副表：nodeSem 以 Node 為鍵（未導出），只能在包內直接遍歷。
	if sc, ok := v.Addr().Interface().(*SemanticContext); ok && sc != nil {
		c, err := e.semCells(sc)
		if err != nil {
			return 0, err
		}
		cells = append(cells, c...)
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // 未導出欄位：gob 也同樣丟棄，保真度對齊 gob
		}
		c, err := e.field(f.Name, v.Field(i))
		if err != nil {
			return 0, fmt.Errorf("mm: %s.%s: %w", kind, f.Name, err)
		}
		if c != nil {
			add(c)
		}
	}

	e.nodes[idx-1] = NTNode{K: kind, C: splitLongCells(cells)}
	return idx, nil
}

// splitLongCells 把超過 mmCellMax 的 cell 值切成多段，段名加 "#0" "#1" … 後綴。
// mm v0.2.9 對單一字串有 65535 位元組硬限制，來源裡的大字串字面量會撞上。
func splitLongCells(cells []NTCell) []NTCell {
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
	out := make([]NTCell, 0, len(cells))
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
			out = append(out, NTCell{N: fmt.Sprintf("%s#%d", c.N, i), T: c.T, V: c.V[start:end]})
			start = end
		}
	}
	return out
}

// field 把一個欄位值編碼成 cell；零值欄位返回 nil（省略）。
func (e *mmEncoder) field(name string, fv reflect.Value) (*NTCell, error) {
	switch fv.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Struct:
		idx, err := e.node(fv)
		if err != nil {
			return nil, err
		}
		if idx == 0 {
			return nil, nil
		}
		return &NTCell{N: name, T: "n", V: strconv.Itoa(int(idx))}, nil

	case reflect.Slice:
		if fv.Len() == 0 {
			return nil, nil
		}
		et := fv.Type().Elem()
		if et.Kind() == reflect.Interface || et.Kind() == reflect.Pointer || et.Kind() == reflect.Struct {
			idx := make([]string, 0, fv.Len())
			for i := 0; i < fv.Len(); i++ {
				n, err := e.node(fv.Index(i))
				if err != nil {
					return nil, err
				}
				idx = append(idx, strconv.Itoa(int(n)))
			}
			return &NTCell{N: name, T: "l", V: strings.Join(idx, ",")}, nil
		}
		bs, err := json.Marshal(fv.Interface())
		if err != nil {
			return nil, err
		}
		return &NTCell{N: name, T: "j", V: string(bs)}, nil

	case reflect.Map:
		if fv.Len() == 0 {
			return nil, nil
		}
		bs, err := json.Marshal(fv.Interface())
		if err != nil {
			return nil, err
		}
		return &NTCell{N: name, T: "j", V: string(bs)}, nil

	case reflect.String:
		if fv.String() == "" {
			return nil, nil
		}
		return &NTCell{N: name, T: "s", V: fv.String()}, nil

	case reflect.Bool:
		if !fv.Bool() {
			return nil, nil // false 以「缺席」表示（mm 不接受空值）
		}
		return &NTCell{N: name, T: "b", V: "true"}, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Int() == 0 {
			return nil, nil
		}
		return &NTCell{N: name, T: "i", V: strconv.FormatInt(fv.Int(), 10)}, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if fv.Uint() == 0 {
			return nil, nil
		}
		return &NTCell{N: name, T: "i", V: strconv.FormatUint(fv.Uint(), 10)}, nil

	case reflect.Float32, reflect.Float64:
		if fv.Float() == 0 {
			return nil, nil
		}
		// 十六進位浮點：精確往返，不受格式化位數影響
		return &NTCell{N: name, T: "f", V: strconv.FormatFloat(fv.Float(), 'x', -1, 64)}, nil
	}
	return nil, fmt.Errorf("unsupported kind %s", fv.Kind())
}

// semCells 編碼語義副表的 nodeSem：[[節點索引, 語義節點索引], ...]（按索引排序，
// 保證編碼確定性）。其餘導出欄位是普通 map，由 field() 走 JSON。
func (e *mmEncoder) semCells(sc *SemanticContext) ([]NTCell, error) {
	if len(sc.nodeSem) == 0 {
		return nil, nil
	}
	pairs := make([][]int32, 0, len(sc.nodeSem))
	for n, ns := range sc.nodeSem {
		if ns == nil {
			continue
		}
		ni, err := e.node(reflect.ValueOf(n))
		if err != nil {
			return nil, err
		}
		si, err := e.node(reflect.ValueOf(ns))
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, []int32{ni, si})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	bs, err := json.Marshal(pairs)
	if err != nil {
		return nil, err
	}
	return []NTCell{{N: "node_sem", T: "j", V: string(bs)}}, nil
}

// ── 解碼 ────────────────────────────────────────────────────────────────────

type mmDecoder struct {
	nt    *NodeTable
	built map[int32]reflect.Value
}

// DecodeNodeTable 把節點表還原成 *Program。版本不符一律硬錯（不靜默回退）。
func DecodeNodeTable(nt *NodeTable) (*Program, error) {
	if nt == nil {
		return nil, fmt.Errorf("mm: nil node table")
	}
	if nt.Ver != MMWireVersion {
		return nil, fmt.Errorf("mm: wire version mismatch: blob %q, this compiler %q", nt.Ver, MMWireVersion)
	}
	if nt.AstVer != ASTVersion {
		return nil, fmt.Errorf("mm: AST version mismatch: blob %q, this compiler %q", nt.AstVer, ASTVersion)
	}
	d := &mmDecoder{nt: nt, built: map[int32]reflect.Value{}}
	if nt.Root <= 0 || int(nt.Root) > len(nt.Nodes) {
		return nil, fmt.Errorf("mm: bad root index %d (nodes=%d)", nt.Root, len(nt.Nodes))
	}
	v, err := d.build(nt.Root)
	if err != nil {
		return nil, err
	}
	p, ok := v.Interface().(*Program)
	if !ok {
		return nil, fmt.Errorf("mm: root node is %T, want *parser.Program", v.Interface())
	}
	return p, nil
}

func (d *mmDecoder) build(idx int32) (reflect.Value, error) {
	if idx <= 0 {
		return reflect.Value{}, nil
	}
	if v, ok := d.built[idx]; ok {
		return v, nil
	}
	if int(idx) > len(d.nt.Nodes) {
		return reflect.Value{}, fmt.Errorf("mm: node index %d out of range (%d nodes)", idx, len(d.nt.Nodes))
	}
	n := d.nt.Nodes[idx-1]
	typ, ok := mmKindTypes[n.K]
	if !ok {
		return reflect.Value{}, fmt.Errorf("mm: unknown node kind %q (registry has %d kinds)", n.K, len(mmKindTypes))
	}
	pv := reflect.New(typ)
	d.built[idx] = pv // 先佔位，防止環形引用無限遞迴
	sv := pv.Elem()

	// cell 可能因為超長被切成 name#0 / name#1 …，先按原名合併。
	cells := map[string]string{}
	order := []string{}
	kinds := map[string]string{}
	for _, c := range n.C {
		base := c.N
		if i := strings.LastIndex(base, "#"); i > 0 {
			base = base[:i]
		}
		if _, seen := cells[base]; !seen {
			order = append(order, base)
			kinds[base] = c.T
		}
		cells[base] += c.V
	}

	for _, name := range order {
		// node_sem 是編碼器合成的（map[Node]*NodeSemantics 沒有對應的導出欄位），
		// 由下方 SemanticContext 專用分支回填。
		if name == "node_sem" && n.K == "SemanticContext" {
			continue
		}
		f := sv.FieldByName(name)
		if !f.IsValid() {
			return reflect.Value{}, fmt.Errorf("mm: %s has no field %q", n.K, name)
		}
		if err := d.setCell(f, kinds[name], cells[name]); err != nil {
			return reflect.Value{}, fmt.Errorf("mm: %s.%s: %w", n.K, name, err)
		}
	}

	// 語義副表的 nodeSem 是未導出欄位，只能在包內直接回填。
	if n.K == "SemanticContext" {
		if jv, ok := cells["node_sem"]; ok {
			var pairs [][]int32
			if err := json.Unmarshal([]byte(jv), &pairs); err != nil {
				return reflect.Value{}, fmt.Errorf("mm: node_sem: %w", err)
			}
			sc := pv.Interface().(*SemanticContext)
			sc.nodeSem = make(map[Node]*NodeSemantics, len(pairs))
			for _, pr := range pairs {
				if len(pr) != 2 {
					return reflect.Value{}, fmt.Errorf("mm: node_sem entry %v is not a pair", pr)
				}
				nv, err := d.build(pr[0])
				if err != nil {
					return reflect.Value{}, err
				}
				svv, err := d.build(pr[1])
				if err != nil {
					return reflect.Value{}, err
				}
				node, ok := nv.Interface().(Node)
				if !ok {
					return reflect.Value{}, fmt.Errorf("mm: node_sem key %T does not implement Node", nv.Interface())
				}
				sem, ok := svv.Interface().(*NodeSemantics)
				if !ok {
					return reflect.Value{}, fmt.Errorf("mm: node_sem value %T", svv.Interface())
				}
				sc.nodeSem[node] = sem
			}
		}
	}
	return pv, nil
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
	case "j":
		if !f.CanAddr() {
			return fmt.Errorf("unaddressable field for json")
		}
		return json.Unmarshal([]byte(v), f.Addr().Interface())
	case "n":
		idx, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		cv, err := d.build(int32(idx))
		if err != nil {
			return err
		}
		return assignNode(f, cv)
	case "l":
		if v == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		sl := reflect.MakeSlice(f.Type(), len(parts), len(parts))
		for i, ps := range parts {
			idx, err := strconv.Atoi(ps)
			if err != nil {
				return err
			}
			cv, err := d.build(int32(idx))
			if err != nil {
				return err
			}
			if err := assignNode(sl.Index(i), cv); err != nil {
				return err
			}
		}
		f.Set(sl)
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
