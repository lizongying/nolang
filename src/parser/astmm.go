package parser

// MetaMessage 承載用的 AST ↔ 遞歸節點樹（ASTTree）編解碼。
//
// 歷史：mm ≤0.2.10 的反射編碼器遇到自引用型別（[]*Node / *Node 欄位）會在
// createExampleValue 裡無限递归爆栈（約束 C8，見 docs/design/nolang-mm-ast.md
// §3.1），所以當時只能編成「扁平索引節點表」。mm v0.2.11 為反射路徑補上
// refCtx 路徑棧迴圈檢測（internal/core/value_to_node.go），自引用型別可正常
// 展開——本檔隨即改成**真正的 AST 樹**：子節點是樹邊（Kids），不再用
// 1-based 索引引用。
//
// 仍存的約束（0.2.11/0.2.12 實測）：
//   - maxDepth：0.2.11 硬編碼 32（深樹撞牆）；0.2.12 放到 256。所以整棵 AST
//     仍切成「深度預算 ≤90、wire 估算預算 ≤50KB」的**根森林**：超預算的節點把
//     全部子節點「切割提升」（Cut=k）為森林中的新根，解碼端按 FIFO 槽位佇列拼回。
//     此時載入（C3 的 64KB）才是主導約束。
//   - C3 每容器 ≤65535 位元組：由森林切割 + mmast 分片共同保證。
//   - C2 零值拒绝：cell/欄位照舊「缺省即零值」；空容器帶 allow_empty
//    （解碼會退化出一個零值元素，sanitize 統一剔除）。
//   - C1 介面欄位：樹型別全是具體結構（[]ASTNode 值語義——0.2.11 的 binder
//     不支持 []*T 切片元素，實測報 "requires struct type, got ptr"）。
//
// 語義副表 nodeSem（未導出 map[Node]*NodeSemantics）不再有索引可引用：改為在
// 每個被登記節點的樹位置上內嵌一個 ASTSemEntry 包裝子節點，解碼端拼回屬主。

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// MMWireVersion 是節點樹格式的格式版本。AST 形狀（ast.go）或本檔案的編碼規則
// 任一變化都必須 bump，否則舊 .ast.zip 會解出結構不同的樹。
// 2026-10-15.2：mm v0.2.11 起改遞歸根森林（切割提升協議），不再是扁平索引表。
const MMWireVersion = "2026-10-15.2"

// mmCellMax 是單一 cell 值的字串上限。mm 對單一字串有 65535 位元組的硬
// 限制（實測：鍵名前缀+tag 還要佔額外位元組，60000 裸值編碼後到 65549 撞牆），
// 超過就必須切段（見 addCell 的 #N 後綴協議）。
const mmCellMax = 56000

// mmTreeDepthBudget 是單根森林每棵樹的節點深度預算。mm anyToJSONC 的
// maxDepth（0.2.12 = 256）按「每個容器/標量一層」計數，一棵 L 層節點樹到
// 最深 cell 字串約消耗 2L+3 層，L=90 時 ≈183，留足餘量。
const mmTreeDepthBudget = 90

// mmTreeLoadBudget 是單棵樹的 wire 估算上限。树形下每節點自帶 k/c/kidf/cut/kids
// 五個欄位容器，結構開銷遠大於字串內容本身（實測舊「字串長×1.35」模型低估
// ≈1.7 倍，單根冲到 65158B）；改用 ASTNodeWireEstimate 同口徑估算，留餘量低於
// mmast 的 60KB 分片上限，保證「提升根」本身不再超標。
const mmTreeLoadBudget = 50000

// 树形 wire 估算常量（實測校準：wire/est 比典型 1.31、小節點最差 3.13，
// 按最差比例留裕拉高；vec.no 13 鏈節點 ≈ 212B/節點的底銷）。
const (
	mmWireNodeBase = 170 // 節點物件头 + k/c/kidf/cut/kids 欄位框與 tag
	mmWireCellOver = 80  // 每個 cell（N/T/V 三字串帶鍵名前缀）
	mmWireKidOver  = 30  // kids 陣列元素框（kidf 條目另計 28）
)

// ASTNodeWireEstimate 估算一棵（提升）根樹的 mm wire 位元組數。編碼端 trim 與
// mmast 分片打包共用這個口徑；超估算即切割，留餘量低於 65535。
func ASTNodeWireEstimate(n *ASTNode) int64 {
	sum := int64(mmWireNodeBase + len(n.K))
	for _, c := range n.C {
		sum += int64(len(c.N) + len(c.T) + len(c.V) + mmWireCellOver)
	}
	for _, f := range n.KidF {
		sum += int64(len(f)) + 28
	}
	for i := range n.Kids {
		sum += ASTNodeWireEstimate(&n.Kids[i]) + mmWireKidOver
	}
	return sum
}

// ASTTree 是一整個模組 AST 的具體化表示：深度/載入受限的根森林。
// Roots[0] 是樹根（Program），其餘是編碼期切割提升出來的子樹根；
// 解碼端按拜訪順序 FIFO 消費。
type ASTTree struct {
	Ver    string
	AstVer string
	Nodes  int32      // 節點總數（校驗/日誌）
	Roots  []*ASTNode // 根森林（DFS 拜訪序）
}

// ASTNode 是樹上的一個具體節點：kind + 標量 cell + 樹邊子節點。
// KidF 與「Kids + Cut」一一对應（Kids 是留在本樹內的明示子節點，Cut 個尾巴
// 子節點被提升為森林新根）。
type ASTNode struct {
	K    string    `mm:"desc=節點型別名"`
	C    []MMCell  `mm:"allow_empty;desc=標量欄位"`
	KidF []string  `mm:"allow_empty;desc=子節點欄位名（明示+提升，DFS 序）"`
	Cut  int32     `mm:"allow_empty;desc=切割提升的子節點數"`
	Kids []ASTNode `mm:"allow_empty;desc=明示子節點"`

	// load 是本節點子樹的載入位元組數（編碼期自底向上累計，不上線）。
	load int64
}

// MMCell 是一個標量欄位。T 決定 V 的解釋方式：
//
//	s  字串      i  整數      f  浮點（十六進位精確格式）   b  布林（只存 true）
//	j  JSON（集合型欄位；[]byte 走 base64）
//
// 子節點不再是 cell——樹邊由 ASTNode.Kids/KidF/Cut 表達。
type MMCell struct {
	N string `mm:"desc=欄位名"`
	T string `mm:"desc=值類型"`
	V string `mm:"desc=值"`
}

// ASTSemEntry 是 nodeSem 語義條目的樹內包裝：挂在被登記節點的 Kids 下
// （KidF 欄位名 "__sem__"，不是任何 Go 欄位名），Sem 指向 NodeSemantics 子樹。
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
	// FuncSignature / CommentedNode / lexer.Token）。清單由
	// scripts 機械產生（見 TestMMKindRegistryCoversAllStructs 守衛），
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
	roots  []*ASTNode                 // 輸出根森林（FIFO：i 處彈出，切割子節點排隊到尾部）
	count  int32                      // 已建節點總數
	semOcc map[uintptr]*NodeSemantics // nodeSem 按底層節點指標索引（同一指標的每次出現都掛包裝）
	onPath map[uintptr]bool           // 路徑棧：真環（AST 正常不該有）直接報錯
}

// EncodeASTTree 把 *Program 具體化成根森林。srcSHA 可為空（由 manifest 承載）。
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
	// 切割提升：從根開始 BFS，超預算的節點把全部子節點排到森林尾部。
	e.roots = append(e.roots, root)
	for qi := 0; qi < len(e.roots); qi++ {
		e.trim(e.roots[qi], 1)
	}
	return &ASTTree{
		Ver:    MMWireVersion,
		AstVer: ASTVersion,
		Nodes:  e.count,
		Roots:  e.roots,
	}, nil
}

// trim 對一棵樹做深度/載入修剪：
//   - 深度超預算：全部子節點提升（樹邊不能再往下）；
//   - 載入超預算：保留能装進預算的前綴子節點（明示 Kids），尾部提升為新根
//     ——避免寬節點（上千條目語句塊）把每條都裂成獨立根的碎片化。
//
// 提升順序 = KidF 序（DFS 拜訪序），與解碼端槽位 FIFO 對齊。
//
// 預算口徑必須與 mmast 的 ASTNodeWireEstimate 完全一致，否則會漏算：提升一個
// 子節點只省下「它整棵子樹 + kids 陣列元素框」，而它在 KidF 裡的名字（name+28）
// 無論明示或提升都留在線上（解碼端要靠名字回填槽位）。舊寫法把 name+28 也當成
// 省下量，寬節點（上千條 Statements）的根就會嚴重低估、撞上 64KB 容器上限。
func (e *mmEncoder) trim(n *ASTNode, depth int) {
	if len(n.Kids) == 0 {
		return
	}
	// 提升 kid i 相对留在本樹內能省下的位元組（其 KidF 名字不計入，仍在线上）。
	kidSave := func(i int) int64 { return n.Kids[i].load + mmWireKidOver }
	// self = 全部子節點都提升後的節點線流量（base+cells+所有 kidf 名字）。
	self := n.load
	for i := range n.Kids {
		self -= kidSave(i)
	}
	if depth >= mmTreeDepthBudget {
		for i := range n.Kids {
			kid := n.Kids[i]
			e.roots = append(e.roots, &kid)
		}
		n.Cut = int32(len(n.Kids))
		n.Kids = nil
		n.load = self
		return
	}
	if n.load <= mmTreeLoadBudget {
		for i := range n.Kids {
			e.trim(&n.Kids[i], depth+1)
		}
		return
	}
	p := 0
	acc := self
	for p < len(n.Kids) && acc+kidSave(p) <= mmTreeLoadBudget {
		acc += kidSave(p)
		p++
	}
	for i := p; i < len(n.Kids); i++ {
		kid := n.Kids[i]
		e.roots = append(e.roots, &kid)
	}
	n.Cut = int32(len(n.Kids) - p)
	n.Kids = n.Kids[:p]
	n.load = acc
	// 保留的前綴仍需繼續自己的深度/載入修剪。
	for i := 0; i < p; i++ {
		e.trim(&n.Kids[i], depth+1)
	}
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
	n.load = int64(mmWireNodeBase + len(kind))
	e.count++

	addCell := func(c *MMCell) {
		n.C = append(n.C, *c)
		n.load += int64(len(c.N)+len(c.T)+len(c.V)) + mmWireCellOver
	}
	addKid := func(fieldName string, kid *ASTNode) {
		if kid == nil {
			return
		}
		n.KidF = append(n.KidF, fieldName)
		n.Kids = append(n.Kids, *kid)
		n.load += kid.load + int64(len(fieldName)) + 28 + mmWireKidOver
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
			wrap := &ASTNode{K: "ASTSemEntry", KidF: []string{"Sem"}, load: int64(len("ASTSemEntry") + len("Sem"))}
			sv, err := e.node(reflect.ValueOf(sem))
			if err != nil {
				return nil, fmt.Errorf("mm: %s node_sem: %w", kind, err)
			}
			if sv == nil {
				return nil, fmt.Errorf("mm: %s node_sem wrapped nil", kind)
			}
			wrap.Kids = append(wrap.Kids, *sv)
			wrap.load += sv.load + 28 + mmWireKidOver
			addKid("__sem__", wrap)
		}
	}

	n.C = splitLongCells(n.C)
	return n, nil
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

// field 把一個欄位值編進節點 n：標量走 addCell，節點型欄位走 addKid（樹邊）。
func (e *mmEncoder) field(name string, fv reflect.Value, n *ASTNode, addCell func(*MMCell), addKid func(string, *ASTNode)) error {
	switch fv.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Struct:
		kid, err := e.node(fv)
		if err != nil {
			return err
		}
		addKid(name, kid)
		return nil

	case reflect.Slice:
		if fv.Len() == 0 {
			return nil
		}
		et := fv.Type().Elem()
		if et.Kind() == reflect.Interface || et.Kind() == reflect.Pointer || et.Kind() == reflect.Struct {
			for i := 0; i < fv.Len(); i++ {
				kid, err := e.node(fv.Index(i))
				if err != nil {
					return err
				}
				addKid(name, kid) // 同名連續 run = 列表欄位，解碼端按 run 分組
			}
			return nil
		}
		bs, err := json.Marshal(fv.Interface())
		if err != nil {
			return err
		}
		addCell(&MMCell{N: name, T: "j", V: string(bs)})
		return nil

	case reflect.Map:
		if fv.Len() == 0 {
			return nil
		}
		bs, err := json.Marshal(fv.Interface())
		if err != nil {
			return err
		}
		addCell(&MMCell{N: name, T: "j", V: string(bs)})
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

// ── 解碼 ────────────────────────────────────────────────────────────────────

// semSlot 是等待提升根填補的子節點槽位（切割提升協議的解碼端 FIFO 佇列）。
type semSlot struct {
	owner reflect.Value // 父節點（*T）
	sv    reflect.Value // 父節點可位址欄位集（T）
	name  string        // KidF 中的欄位名（"__sem__" = 語義包裝）
}

type mmDecoder struct {
	t        *ASTTree
	pos      int              // 提升根流游標（Roots[0] 之後）
	slots    []semSlot        // 待填槽位佇列
	semCtx   *SemanticContext // 建出的唯一 SemanticContext（nodeSem 回填目標）
	pending  []semPair        // 屬主 → 語義條目（全部根建完後統一回填）
	deferred []deferredAssign // 值語義欄位：延後拷貝（源物件需先建全）
}

// deferredAssign 是「目標欄位是值語義結構體（內嵌欄位如 CommentedNode）」的槽位。
// assignNode 對結構體欄位是 `f.Set(cv.Elem())`——按值拷貝快照。若 cv 自己的
// 提升子節點（如 CommentedNode 的 Doc）在拷貝之後才回填，快照會丟掉它。所以
// 這類拷貝延後到全部根建完、再依「子先於父」（反序）套回。
type deferredAssign struct {
	s  semSlot
	cv reflect.Value
}

type semPair struct {
	owner Node
	// entry 可能尚未填塞 Sem（包裝或被包裝子樹被切割提升時）；
	// Sem 在全部槽位填完後才讀（見 DecodeASTTree 收尾迴圈）。
	entry *ASTSemEntry
}

// DecodeASTTree 把根森林還原成 *Program。版本不符一律硬錯（不靜默回退）。
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
	if len(t.Roots) == 0 {
		return nil, fmt.Errorf("mm: empty root forest")
	}
	for _, r := range t.Roots {
		sanitize(r)
	}
	d := &mmDecoder{t: t, pos: 1}
	v, err := d.build(t.Roots[0])
	if err != nil {
		return nil, err
	}
	// 消費所有槽位：每个槽位取下一根（build 期間可能追加新槽位——嚴格 FIFO，
	// 與編碼端 e.roots 的彈出序一致）。
	for len(d.slots) > 0 {
		s := d.slots[0]
		d.slots = d.slots[1:]
		if d.pos >= len(t.Roots) {
			return nil, fmt.Errorf("mm: cut slot %s.%s has no promoted root", s.owner.Type(), s.name)
		}
		rv := t.Roots[d.pos]
		d.pos++
		cv, err := d.build(rv)
		if err != nil {
			return nil, err
		}
		if s.name == "__sem__" {
			entry, ok := cv.Interface().(*ASTSemEntry)
			if !ok {
				return nil, fmt.Errorf("mm: promoted __sem__ slot got %T (want *parser.ASTSemEntry)", cv.Interface())
			}
			node, ok := s.owner.Interface().(Node)
			if !ok {
				return nil, fmt.Errorf("mm: node_sem owner %T does not implement Node", s.owner.Interface())
			}
			d.pending = append(d.pending, semPair{owner: node, entry: entry})
			continue
		}
		if s.isValueField() {
			// 值語義結構體欄位：延後拷貝，等 cv 的提升子節點全部回填再快照。
			d.deferred = append(d.deferred, deferredAssign{s, cv})
			continue
		}
		if err := s.assign(cv); err != nil {
			return nil, err
		}
	}
	if d.pos != len(t.Roots) {
		return nil, fmt.Errorf("mm: forest has %d unused roots after %d slots", len(t.Roots)-d.pos, d.pos)
	}
	// 值語義欄位的拷貝：反序套回（越晚建出的提升根越深，先補其父物件），
	// 確保「子樹補全」發生在「父按值快照」之前。
	for i := len(d.deferred) - 1; i >= 0; i-- {
		if err := d.deferred[i].s.assign(d.deferred[i].cv); err != nil {
			return nil, err
		}
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

// assign 把槽位對應的提升根建出的值接回父節點欄位（列表欄位增量 append）。
// "__sem__" 槽位由 DecodeASTTree 主循環先行攔截，不會走到這裡。
func (s *semSlot) assign(cv reflect.Value) error {
	f := s.sv.FieldByName(s.name)
	if !f.IsValid() {
		return fmt.Errorf("mm: %s has no field %q", s.owner.Type(), s.name)
	}
	if f.Kind() == reflect.Slice {
		grow := reflect.MakeSlice(f.Type(), f.Len()+1, f.Len()+1)
		reflect.Copy(grow, f)
		if err := assignNode(grow.Index(f.Len()), cv); err != nil {
			return err
		}
		f.Set(grow)
		return nil
	}
	return assignNode(f, cv)
}

// isValueField 判定槽位目標欄位是否為「按值拷貝」的結構體欄位（如內嵌的
// CommentedNode）。這類欄位 assignNode 會快照 cv.Elem()，若 cv 仍有未回填的
// 提升子節點就會丟資料——必須延後到 cv 建全再拷。指標/介面/切片欄位都是共享
// 底層物件（切片元素多為指標），就地 assign 即可，不需延後。
func (s *semSlot) isValueField() bool {
	f := s.sv.FieldByName(s.name)
	return f.IsValid() && f.Kind() == reflect.Struct
}

// build 把一個樹節點還原成具體型別的值（*T）。切割出去的子節點以槽位入隊。
func (d *mmDecoder) build(n *ASTNode) (reflect.Value, error) {
	typ, ok := mmKindTypes[n.K]
	if !ok {
		return reflect.Value{}, fmt.Errorf("mm: unknown node kind %q (registry has %d kinds)", n.K, len(mmKindTypes))
	}
	if len(n.KidF) != len(n.Kids)+int(n.Cut) {
		return reflect.Value{}, fmt.Errorf("mm: %s kidf %d != kids %d + cut %d", n.K, len(n.KidF), len(n.Kids), n.Cut)
	}
	pv := reflect.New(typ)
	sv := pv.Elem()

	if n.K == "SemanticContext" && d.semCtx == nil {
		d.semCtx = pv.Interface().(*SemanticContext)
	}

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
		f := sv.FieldByName(name)
		if !f.IsValid() {
			return reflect.Value{}, fmt.Errorf("mm: %s has no field %q", n.K, name)
		}
		if err := d.setCell(f, kinds[name], cells[name]); err != nil {
			return reflect.Value{}, fmt.Errorf("mm: %s.%s: %w", n.K, name, err)
		}
	}

	// 提升子節點：先入隊本節點尾巴槽位（順序 = KidF 尾巴順序），再遞歸明示
	// 子節點——與編碼端 trim()「先排尾巴、再遞歸前綴」的切割事件順序一致，
	// 槽位 FIFO 才能對上根流。
	for _, name := range n.KidF[len(n.Kids):] {
		d.slots = append(d.slots, semSlot{owner: pv, sv: sv, name: name})
	}

	// 明示子節點：同名連續 run → 列表欄位；"__sem__" → 語義包裝。
	i := 0
	for i < len(n.Kids) {
		name := n.KidF[i]
		j := i + 1
		if name != "__sem__" {
			// run 只在明示區（前綴）內分組；尾巴（提升根）由槽位逐個 append，
			// 同名字段跨界也不會漏序。
			for j < len(n.Kids) && n.KidF[j] == name {
				j++
			}
		}
		if name == "__sem__" {
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
		if f.Kind() == reflect.Slice {
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
		} else {
			if j-i > 1 {
				return reflect.Value{}, fmt.Errorf("mm: %s.%s is not a slice but has %d kids", n.K, name, j-i)
			}
			cv, err := d.build(&n.Kids[i])
			if err != nil {
				return reflect.Value{}, err
			}
			if err := assignNode(f, cv); err != nil {
				return reflect.Value{}, fmt.Errorf("mm: %s.%s: %w", n.K, name, err)
			}
		}
		i = j
	}
	return pv, nil
}

// attachSem 把 "__sem__" 明示子節點（ASTSemEntry 包裝）配對到屬主節點。
// Sem 可能由後續槽位填塞，所以只存指標、不在此刻讀取。
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

// sanitize 剔除 allow_empty 空容器解碼回來的零值退化項（實測：空 slice 會退化出
// 一個全零元素）。合法節點的 K/KidF/cell.N 永遠非空，可以直接丟。
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
	case "j":
		if !f.CanAddr() {
			return fmt.Errorf("unaddressable field for json")
		}
		return json.Unmarshal([]byte(v), f.Addr().Interface())
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
