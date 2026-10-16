package parser

// MetaMessage 承載用的 AST ↔ 節點表（ASTTree）編解碼。
//
// 形態：**一張扁平節點表 + 依索引的邊 + 名字字典**，節點形状對齊原始 AST。
//   - 每個 AST 節點 → ASTNode{K, F}：K 是 Go 結構型別名的 **u16 字典索引**（見
//     ASTTree.Names），F 是**依原始宣告順序**展開的具名字段清單（不把欄位拆成
//     「標量 C」+「子節點 Kids」兩組、丟失交錯順序）。
//   - 每個欄位 → ASTField：按 Kind 承載該欄位的值。節點型欄位以「子節點在本表中的
//     索引」引用（存進 Vals，字串化）——**沒有** MMSlice / MMMap / MMVal / MMBytes /
//     MMEntry 這些原 AST 中不存在的合成容器節點。非節點集合（[]string / []byte / map）
//     內聯：[]string 逐元素進 Vals；[]byte 與 map（自描述二進位 blob）以**原始位元組**
//     直接作為字串元素進 Vals——mm 的 string cell 實為長度前位元組流（實測含 0x00/
//     非 UTF-8 原樣透過），base64 是多餘的；只剩單條 65535 限，超標 chunk。
//   - 語義副表 nodeSem（未導出 map[Node]*NodeSemantics）以一個保留欄位（Kind=kSem）
//     引用本表中的 NodeSemantics 節點（Vals=[索引]），不再有 ASTSemEntry 包裝節點。
//
// 為何扁平：mm 的 encodeNodeObject/encodeNodeArray 在每一層 ancestor 都重新複製整棵
// 子樹的位元組（internal/core/encode.go:81-107），總開銷是 O(size × 物件嵌套深度)。
// 遞歸樹把這個深度掛在 AST 深度上（大模組可達數十~上百），再疊上每個 ASTField 的多個
// 空容器載體（[]ASTNode／[]ASTField 的「空 slice 仍會发出一個範例元素」——而且是完整的
// 巢狀物件範例）→ 尺寸/時間爆炸（tls 85527 節點 >120s）。扁平後物件嵌套深度變成**常數**
// （Tree→Table→ASTNode→F→ASTField→Vals），且載體全是便宜型別（[]string／string），
// 空範例元素只是 [0]/[""]，複製與尺寸都回到線性。
//
// 制約（mm ≥0.2.16）：
//   - 容器上限 4GB：整張節點表塞進單一 mm 文檔，無位元組切割。
//   - 字串仍 65535：單一標量/位元組/blob 超標切成同一 Vals 的連續元素，
//     解碼端串接（見 scalarFields / chunk / joinVals），**不使用 JSON、不使用 #N 後綴**。
//   - 空容器：nil → 缺席欄位；空的非 nil → **欄位在場但 Vals 為空**（apply 依目標型別
//     MakeSlice/MakeMap(0)）。mm 0.2.16 起 binder 已跳過 example 哨兵、is_null 綁 nil，
//     空 []string／[]ASTField 老實解回 len 0，故**不再需要 "|0" 標記、cleanVals、sanitize
//     這些對舊版 example-artefact 的绕行**。
//   - 仍禁止空字串「元素」與整數 0「元素」上 wire（child_allow_empty 未開），故節點
//     nil 元素用 -1 佔位、真實語料 never puts ""/0 into a scalar slice。

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// MMWireVersion 是節點表格式的格式版本。AST 形狀（ast.go）或本檔案的編碼規則
// 任一變化都必須 bump，否則舊 .ast.zip 會解出結構不同的樹。
// 2026-10-16.2：遞歸樹→**扁平節點表**（Root 索引 + Table），子節點以索引引用，
// map 內聯成 S blob——消除 mm 的 O(size×深度) 複製與空容器範例爆炸，同時維持
// 「按原始宣告順序的具名字段、無包裝節點、無 C/Kids 拆分」。
// 2026-10-16.3：依賴 mm 0.2.16（binder 跳過 example 哨兵、is_null 綁 nil）：
// 拆掉 "|0" 零容器後綴、cleanVals、sanitize、joinVals 的 "" 剔除——空的非 nil
// 容器改由「欄位在場且 Vals 為空」表達。舊 wire 不相容，必須 bump。
// 2026-10-16.4：拆 base64——mm string cell 即長度前位元組流（實測 0x00/非 UTF-8
// 原樣透過），[]byte 與 map blob 以原始位元組直接進 Vals；單條 65535 限照舊 chunk。
// 同時拆掉所有 mm tag 的 desc=——實測 desc 是**逐 object 重發**（每個 ASTField 多
// 數十位元組），27 萬欄位的 tree.mm raw 被描述文字淹沒。欄位語義說明改放 Go 註解。
// Kind 改 u8 形態碼（1-based）；K／N 改 u16 索引共用 ASTTree.Names 字典（1-based，
// 型別名/欄位名不再逐物件重發；deflate 32KB 窗口本就夠不着海量重複名，zip -40%）。
// （也試過 ASTField.B 原生 bytes 欄位：逐欄位收 example sentinel，zip 反增 20%，回退。）
// 舊 wire 不相容，必須 bump。
const MMWireVersion = "2026-10-16.4"

// mmCellMax 是單一欄位值字串的上限。mm 對單一字串仍有 65535 位元組硬限制，超過
// 就拆成同一 Vals 的多個 ≤mmCellMax 元素串接還原（容器已放到 4GB，唯獨字串仍受此限）。
const mmCellMax = 56000

// ASTField.Kind 取值：數字形態碼（u8，1-based：mm 拒發零值標量，0 永不上 wire）。
const (
	kStr   uint8 = 1  // string → Vals 文字
	kInt   uint8 = 2  // 任意整數（十進位文字於 Vals，依目標欄位型別解析）
	kFlt   uint8 = 3  // float（十六進位精確格式於 Vals）
	kBool  uint8 = 4  // bool（只發 true 於 Vals；缺席即 false）
	kStrs  uint8 = 5  // 非節點 slice／定長陣列：逐元素文字存進 Vals
	kBytes uint8 = 6  // []byte／[N]byte：原始位元組存進 Vals（超標切連續元素）
	kMap   uint8 = 7  // map：自描述二進位 blob 原始位元組存進 Vals（超標切元素）
	kNode  uint8 = 8  // 單一節點欄位（介面／指標／結構體）：Vals=[子索引]
	kNodes uint8 = 9  // 節點 slice／陣列欄位：Vals=依序子索引（-1=nil 元素）
	kSem   uint8 = 10 // 保留欄位：Vals=[NodeSemantics 節點索引]
)

// kindName 把形態碼還原成人類可讀名（僅供錯誤訊息/日誌）。
func kindName(k uint8) string {
	switch k {
	case kStr:
		return "s"
	case kInt:
		return "i"
	case kFlt:
		return "f"
	case kBool:
		return "b"
	case kStrs:
		return "strs"
	case kBytes:
		return "bytes"
	case kMap:
		return "map"
	case kNode:
		return "node"
	case kNodes:
		return "nodes"
	case kSem:
		return "sem"
	}
	return "?"
}

// nilChildIndex 是節點 slice 中 nil 元素的佔位索引（真索引恆 ≥0）。
const nilChildIndex = "-1"

// semFieldName 是語義欄位的保留名（不是任何 Go 欄位名，避免撞車）。
const semFieldName = "__sem__"

// ASTTree 是一整個模組 AST 的具體化表示：**扁平節點表 + 根索引 + 名字字典**。
// Names 把節點型別名與欄位名共用一張表，首見順序編號、**1-based**（索引 0 保留：
// mm 拒發零值標量）；ASTNode.K／ASTField.N 只存 u16 索引，型別名/欄位名不再
// 逐物件重發（tls：~95K 節點名 + ~274K 欄位名的文字全部收进字典一次）。
type ASTTree struct {
	Ver    string
	AstVer string
	Nodes  int32     // 節點總數（校驗/日誌）
	Root   int32     // Program 節點在 Table 中的索引（-1 = 空）
	Names  []string  // 名字字典：K/N 存 1-based 索引於 Names[idx-1]
	Table  []ASTNode // 扁平節點表：所有節點都在这里，彼此以索引相連
}

// ASTNode 是表上的一個節點：K 型別名字典索引 + F 依原始宣告順序的具名字段。
// 欄位順序即 wire 契約（見 ASTField 處說明：二進位 wire 不發鍵名、按位定位）。
type ASTNode struct {
	K uint16     `mm:"allow_empty"` // 型別名 → ASTTree.Names[K-1]（1-based，0 永不發出）
	F []ASTField `mm:"allow_empty"`
}

// ASTField 是一個具名字段，只有 3 個欄位：
//   - N：欄位名 → ASTTree.Names[N-1]（u16，1-based 字典索引）
//   - Kind：形態碼（u8，kStr..kSem；欄位存在即非零。空容器由「Kind 在場 + Vals 空」表達）
//   - Vals：唯一載體，語義依 Kind：
//     · s/i/f/b：標量文字（長文字切成連續元素，解碼串接；b 用 ["1"]）
//     · bytes：原始位元組串（切連續元素串接）
//     · map：自描述二進位 blob 原始位元組（切連續元素串接）
//     · strs：非節點 slice 逐元素
//     · node/sem：子節點索引（長度 1）
//     · nodes：依序子節點索引（-1＝nil 元素）
//
// ⚠ mm 的二進位 wire **不發字段鍵名**（實測：tag 短鍵 n/k/v 與預設名 kind/vals 的
// wire 逐字節相同；鍵名只影響 DecodeToJsonc 人類視圖），物件子項按
// **宣告順序定位**——故增删/重排下面三個欄位都屬 wire 結構變化，必須 bump MMWireVersion。
// （tag 留空即默認綁定；僅 allow_empty 為實質編碼規則。）
//
// 超標（>mmCellMax）的 s/bytes/map 直接拆成同一 Vals 的多個 ≤mmCellMax 元素——
// 陣列容器不受 65535 限（只有單一字串受），故**不再需要同名連續欄位 + mergeFields**。
type ASTField struct {
	N    uint16   // 欄位名 → ASTTree.Names[N-1]（1-based）
	Kind uint8    // 形態碼 kStr..kSem（見常量表；mm 零值拒發，故碼從 1 起）
	Vals []string `mm:"allow_empty"`
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
		MatchArm{},
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
	table  []ASTNode                  // 扁平節點表（附錄式追加）
	names  map[string]uint16          // 名字字典（K/N 共用）：首見順序編號，1-based
	semOcc map[uintptr]*NodeSemantics // nodeSem 按底層節點指標索引（同一指標的每次出現都掛語義欄位）
	onPath map[uintptr]bool           // 路徑棧：真環（AST 正常不該有）直接報錯
}

// intern 把名字登記進字典並回 1-based 索引（0 保留：mm 拒發零值標量）。
func (e *mmEncoder) intern(s string) (uint16, error) {
	if v, ok := e.names[s]; ok {
		return v, nil
	}
	if len(e.names)+1 > 65535 {
		return 0, fmt.Errorf("mm: name dictionary overflow (>65535 distinct names)")
	}
	v := uint16(len(e.names) + 1)
	e.names[s] = v
	return v, nil
}

// EncodeASTTree 把 *Program 展開成扁平節點表。srcSHA 可為空（由 manifest 承載）。
func EncodeASTTree(p *Program, srcSHA string) (*ASTTree, error) {
	if p == nil {
		return nil, fmt.Errorf("mm: nil program")
	}
	e := &mmEncoder{names: map[string]uint16{}, semOcc: map[uintptr]*NodeSemantics{}, onPath: map[uintptr]bool{}}
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
	if root < 0 {
		return nil, fmt.Errorf("mm: program root encoded to nil")
	}
	names := make([]string, len(e.names))
	for s, v := range e.names {
		names[v-1] = s
	}
	return &ASTTree{
		Ver:    MMWireVersion,
		AstVer: ASTVersion,
		Nodes:  int32(len(e.table)),
		Root:   root,
		Names:  names,
		Table:  e.table,
	}, nil
}

// add 把一個節點追加進表，返回其索引。
func (e *mmEncoder) add(n ASTNode) int32 {
	e.table = append(e.table, n)
	return int32(len(e.table) - 1)
}

// node 編碼一個節點值（介面／指標／結構體），返回它在表中的索引；nil 返回 -1。
// 先建欄位（遞歸把子節點追加進表），再把自身追加進表——索引無序约束，僅需一致。
func (e *mmEncoder) node(v reflect.Value) (int32, error) {
	var id uintptr
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return -1, nil
		}
		if v.Kind() == reflect.Pointer {
			id = v.Pointer()
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return -1, fmt.Errorf("mm: expected struct node, got %s", v.Kind())
	}
	if id != 0 {
		if e.onPath[id] {
			return -1, fmt.Errorf("mm: cyclic AST node %s (flat table cannot encode cycles)", v.Type().Name())
		}
		e.onPath[id] = true
		defer delete(e.onPath, id)
	}

	kind := v.Type().Name()
	if kind == "" {
		return -1, fmt.Errorf("mm: anonymous struct type in AST")
	}
	ki, err := e.intern(kind)
	if err != nil {
		return -1, err
	}
	n := ASTNode{K: ki}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // 未導出欄位：gob 也同樣丟棄，保真度對齊 gob
		}
		fields, err := e.field(f.Name, v.Field(i))
		if err != nil {
			return -1, fmt.Errorf("mm: %s.%s: %w", kind, f.Name, err)
		}
		n.F = append(n.F, fields...)
	}

	// 語義副表：以保留欄位（Kind=sem）引用表中的 NodeSemantics 節點，解碼端按
	// 「屬主節點 → Sem」配對拼回（不再有 ASTSemEntry 包裝節點）。
	if id != 0 {
		if sem, ok := e.semOcc[id]; ok {
			sv, err := e.node(reflect.ValueOf(sem))
			if err != nil {
				return -1, fmt.Errorf("mm: %s node_sem: %w", kind, err)
			}
			si, err := e.intern(semFieldName)
			if err != nil {
				return -1, err
			}
			if sv < 0 {
				return -1, fmt.Errorf("mm: %s node_sem wrapped nil", kind)
			}
			n.F = append(n.F, ASTField{N: si, Kind: kSem, Vals: []string{idxStr(sv)}})
		}
	}
	return e.add(n), nil
}

// field 把一個具名欄位編成若干 ASTField（超長標量/位元組/blob 會切成同名同 Kind 的
// 連續欄位；零值/空容器/nil 返回 nil 表示缺席）。
func (e *mmEncoder) field(name string, fv reflect.Value) ([]ASTField, error) {
	ni, err := e.intern(name)
	if err != nil {
		return nil, err
	}
	switch fv.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Struct:
		idx, err := e.node(fv)
		if err != nil {
			return nil, err
		}
		if idx < 0 {
			return nil, nil // nil 介面/指標：缺席
		}
		return []ASTField{{N: ni, Kind: kNode, Vals: []string{idxStr(idx)}}}, nil

	case reflect.Slice, reflect.Array:
		if fv.Len() == 0 {
			// nil 容器：缺席即 nil；空的非 nil 容器：欄位在場 + Vals 空，apply 依型別構造。
			if fv.Kind() == reflect.Slice && fv.IsNil() {
				return nil, nil
			}
			return []ASTField{{N: ni, Kind: containerKind(fv.Type().Elem())}}, nil
		}
		et := fv.Type().Elem()
		if elemIsNode(et) {
			// 節點切片／陣列：依序展索引進一個 nodes 欄位。
			vals := make([]string, 0, fv.Len())
			for i := 0; i < fv.Len(); i++ {
				idx, err := e.node(fv.Index(i))
				if err != nil {
					return nil, err
				}
				if idx < 0 {
					vals = append(vals, nilChildIndex) // nil 元素：-1 佔位保持對齊
					continue
				}
				vals = append(vals, idxStr(idx))
			}
			return []ASTField{{N: ni, Kind: kNodes, Vals: vals}}, nil
		}
		if et.Kind() == reflect.Uint8 {
			return bytesFields(ni, fv), nil
		}
		// 非節點 slice／定長陣列：逐元素文字存進 Vals。
		strs := make([]string, 0, fv.Len())
		for i := 0; i < fv.Len(); i++ {
			s, err := scalarText(fv.Index(i))
			if err != nil {
				return nil, err
			}
			strs = append(strs, s)
		}
		return []ASTField{{N: ni, Kind: kStrs, Vals: strs}}, nil

	case reflect.Map:
		if fv.Len() == 0 {
			if fv.IsNil() {
				return nil, nil
			}
			return []ASTField{{N: ni, Kind: kMap}}, nil
		}
		var buf bytes.Buffer
		if err := writeMap(&buf, fv); err != nil {
			return nil, fmt.Errorf("mm: %s map: %w", name, err)
		}
		return scalarFields(ni, kMap, string(buf.Bytes())), nil

	case reflect.String:
		if fv.String() == "" {
			return nil, nil
		}
		return scalarFields(ni, kStr, fv.String()), nil

	case reflect.Bool:
		if !fv.Bool() {
			return nil, nil // false 以缺席表示
		}
		return []ASTField{{N: ni, Kind: kBool, Vals: []string{"1"}}}, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Int() == 0 {
			return nil, nil
		}
		return []ASTField{{N: ni, Kind: kInt, Vals: []string{strconv.FormatInt(fv.Int(), 10)}}}, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if fv.Uint() == 0 {
			return nil, nil
		}
		return []ASTField{{N: ni, Kind: kInt, Vals: []string{strconv.FormatUint(fv.Uint(), 10)}}}, nil

	case reflect.Float32, reflect.Float64:
		if fv.Float() == 0 {
			return nil, nil
		}
		return []ASTField{{N: ni, Kind: kFlt, Vals: []string{strconv.FormatFloat(fv.Float(), 'x', -1, 64)}}}, nil
	}
	return nil, fmt.Errorf("unsupported kind %s", fv.Kind())
}

// bytesFields 把 []byte／[]uint8／定長位元組陣列的**原始位元組**放進 Vals（超標切元素）。
// mm 的 string cell 即長度前位元組流，實測含 0x00／非 UTF-8 原樣透過，base64 多餘。
func bytesFields(ni uint16, fv reflect.Value) []ASTField {
	raw := make([]byte, fv.Len())
	for i := 0; i < fv.Len(); i++ {
		raw[i] = byte(fv.Index(i).Uint())
	}
	return []ASTField{{N: ni, Kind: kBytes, Vals: chunk(string(raw))}}
}

// scalarFields 把可能超標的標量文字切成同一欄的連續 Vals 元素（解碼端串接）。
func scalarFields(ni uint16, kind uint8, s string) []ASTField {
	return []ASTField{{N: ni, Kind: kind, Vals: chunk(s)}}
}

// chunk 把一段文字按 mmCellMax 切成 ≤mmCellMax 的連續元素（s=="" → 空）。按位元組
// 切分對任意二進位無損（呼叫端含 raw bytes／blob，串接即還原），無 UTF-8 割裂風險。
func chunk(s string) []string {
	if s == "" {
		return nil
	}
	if len(s) <= mmCellMax {
		return []string{s}
	}
	var out []string
	for start := 0; start < len(s); {
		end := start + mmCellMax
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[start:end])
		start = end
	}
	return out
}

// joinVals 把切段的 s/bytes/map 載體串接回完整文字（剔除 mm 空陣例元素 ""）。
func joinVals(vals []string) string {
	var b strings.Builder
	for _, v := range vals {
		b.WriteString(v)
	}
	return b.String()
}

// scalarText 把一個非節點標量元素轉成文字（存進 Vals／map blob；解碼端依目標型別解析）。
func scalarText(v reflect.Value) (string, error) {
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "", nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'x', -1, 64), nil
	}
	return "", fmt.Errorf("mm: unsupported scalar element kind %s", v.Kind())
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
		return strconv.FormatInt(k.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(k.Uint(), 10)
	case reflect.Bool:
		return strconv.FormatBool(k.Bool())
	default:
		return fmt.Sprintf("%v", k.Interface())
	}
}

// elemIsNode 判定切片/map 的元素型別是否為 AST 節點（介面／指標／結構體）。
func elemIsNode(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Struct:
		return true
	}
	return false
}

// containerKind 依 slice／陣列的元素型別選出內聯欄位的形態碼（空的非 nil 容器標記用）。
func containerKind(et reflect.Type) uint8 {
	switch {
	case elemIsNode(et):
		return kNodes
	case et.Kind() == reflect.Uint8:
		return kBytes
	default:
		return kStrs
	}
}

func idxStr(i int32) string { return strconv.FormatInt(int64(i), 10) }

// ── map 二進位 blob（內聯進 S，非 JSON）─────────────────────────────────────
//
// 佈局（全部 len-prefixed；解碼端由目標 map 型別驅動，兩側对称）：
//   map   = 依 key 規範序的 [ entry ]*（頂層以 blob 終端界定；巢狀以 count 前置）
//   entry = str(key) value
//   value = 依靜態型別 kind：bool→1B；字串/數字→str；[]byte→bytes；
//           []scalar→count + str*；巢狀 map→count + entry*；nil→0xFF
// 只支援語料實際出現的 map 值型別（string/bool/[]string/[]byte/巢狀 map）；
// mm 本身禁止 map value 為 interface，故值全為具體型別。

func writeMap(buf *bytes.Buffer, mv reflect.Value) error {
	mt := mv.Type()
	if mt.Key().Kind() != reflect.String {
		return fmt.Errorf("map key must be string, got %s", mt.Key().Kind())
	}
	keys := mv.MapKeys()
	strs := make([]string, len(keys))
	for i, k := range keys {
		strs[i] = canonKey(k)
	}
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return strs[idx[a]] < strs[idx[b]] })
	et := mt.Elem()
	for _, i := range idx {
		putStr(buf, strs[i])
		if err := putValue(buf, mv.MapIndex(keys[i]), et); err != nil {
			return err
		}
	}
	return nil
}

func putValue(buf *bytes.Buffer, v reflect.Value, t reflect.Type) error {
	// 解引用介面／指標，nil 用 0xFF 佔位（語料 map 值多為具體型別，少見此路徑）。
	for t.Kind() == reflect.Interface || t.Kind() == reflect.Pointer {
		if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			if v.IsNil() {
				buf.WriteByte(0xFF)
				return nil
			}
			v = v.Elem()
			t = t.Elem()
			if t.Kind() == reflect.Interface {
				t = v.Type() // 介面值取其動態具體型別
			}
		} else {
			break
		}
	}
	switch t.Kind() {
	case reflect.Bool:
		if v.Bool() {
			buf.WriteByte(1)
		} else {
			buf.WriteByte(0)
		}
		return nil
	case reflect.String:
		putStr(buf, v.String())
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		s, err := scalarText(v)
		if err != nil {
			return err
		}
		putStr(buf, s)
		return nil
	case reflect.Slice, reflect.Array:
		et := t.Elem()
		if et.Kind() == reflect.Uint8 {
			raw := make([]byte, v.Len())
			for i := 0; i < v.Len(); i++ {
				raw[i] = byte(v.Index(i).Uint())
			}
			putBytes(buf, raw)
			return nil
		}
		putUvarint(buf, uint64(v.Len()))
		for i := 0; i < v.Len(); i++ {
			s, err := scalarText(v.Index(i))
			if err != nil {
				return err
			}
			putStr(buf, s)
		}
		return nil
	case reflect.Map:
		keys := v.MapKeys()
		strs := make([]string, len(keys))
		for i, k := range keys {
			strs[i] = canonKey(k)
		}
		order := make([]int, len(keys))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool { return strs[order[a]] < strs[order[b]] })
		putUvarint(buf, uint64(len(keys)))
		et := t.Elem()
		for _, i := range order {
			putStr(buf, strs[i])
			if err := putValue(buf, v.MapIndex(keys[i]), et); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("mm: unsupported map value kind %s", t.Kind())
}

func putUvarint(buf *bytes.Buffer, x uint64) { buf.Write(binary.AppendUvarint(nil, x)) }
func putStr(buf *bytes.Buffer, s string)     { putBytes(buf, []byte(s)) }
func putBytes(buf *bytes.Buffer, b []byte) {
	putUvarint(buf, uint64(len(b)))
	buf.Write(b)
}

type mapReader struct {
	b   []byte
	off int
}

func (r *mapReader) uvarint() (uint64, error) {
	x, n := binary.Uvarint(r.b[r.off:])
	if n <= 0 {
		return 0, fmt.Errorf("mm: map blob uvarint @%d", r.off)
	}
	r.off += n
	return x, nil
}

func (r *mapReader) bytes() ([]byte, error) {
	l, err := r.uvarint()
	if err != nil {
		return nil, err
	}
	if uint64(r.off)+l > uint64(len(r.b)) {
		return nil, fmt.Errorf("mm: map blob overflow")
	}
	b := r.b[r.off : r.off+int(l)]
	r.off += int(l)
	return b, nil
}

func (r *mapReader) str() (string, error) {
	b, err := r.bytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *mapReader) done() bool { return r.off >= len(r.b) }

// readMap 從 blob 還原一個 map（dst 型別驅動）。頂層讀到 blob 結束。
func readMap(r *mapReader, mt reflect.Type) (reflect.Value, error) {
	m := reflect.MakeMapWithSize(mt, 0)
	kt, et := mt.Key(), mt.Elem()
	for !r.done() {
		ks, err := r.str()
		if err != nil {
			return reflect.Value{}, err
		}
		k := reflect.New(kt).Elem()
		if err := setScalarText(k, kt, ks); err != nil {
			return reflect.Value{}, fmt.Errorf("map key %q: %w", ks, err)
		}
		v, err := readValue(r, et)
		if err != nil {
			return reflect.Value{}, err
		}
		m.SetMapIndex(k, v)
	}
	return m, nil
}

func readValue(r *mapReader, t reflect.Type) (reflect.Value, error) {
	// nil 佔位：保持該型別零值。
	if t.Kind() == reflect.Interface || t.Kind() == reflect.Pointer {
		if r.off < len(r.b) && r.b[r.off] == 0xFF {
			r.off++
			return reflect.Zero(t).Elem(), nil
		}
	}
	switch t.Kind() {
	case reflect.Bool:
		if r.off >= len(r.b) {
			return reflect.Value{}, fmt.Errorf("mm: map blob eof bool")
		}
		b := r.b[r.off]
		r.off++
		v := reflect.New(t).Elem()
		v.SetBool(b == 1)
		return v, nil
	case reflect.String:
		s, err := r.str()
		if err != nil {
			return reflect.Value{}, err
		}
		v := reflect.New(t).Elem()
		v.SetString(s)
		return v, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		s, err := r.str()
		if err != nil {
			return reflect.Value{}, err
		}
		v := reflect.New(t).Elem()
		if err := setScalarText(v, t, s); err != nil {
			return reflect.Value{}, err
		}
		return v, nil
	case reflect.Slice, reflect.Array:
		et := t.Elem()
		if et.Kind() == reflect.Uint8 {
			raw, err := r.bytes()
			if err != nil {
				return reflect.Value{}, err
			}
			v := reflect.MakeSlice(t, len(raw), len(raw))
			reflect.Copy(v, reflect.ValueOf(raw))
			return v, nil
		}
		n, err := r.uvarint()
		if err != nil {
			return reflect.Value{}, err
		}
		v := reflect.MakeSlice(t, int(n), int(n))
		for i := 0; i < int(n); i++ {
			s, err := r.str()
			if err != nil {
				return reflect.Value{}, err
			}
			if err := setScalarText(v.Index(i), et, s); err != nil {
				return reflect.Value{}, err
			}
		}
		return v, nil
	case reflect.Map:
		n, err := r.uvarint()
		if err != nil {
			return reflect.Value{}, err
		}
		m := reflect.MakeMapWithSize(t, int(n))
		kt, vet := t.Key(), t.Elem()
		for i := 0; i < int(n); i++ {
			ks, err := r.str()
			if err != nil {
				return reflect.Value{}, err
			}
			k := reflect.New(kt).Elem()
			if err := setScalarText(k, kt, ks); err != nil {
				return reflect.Value{}, err
			}
			v, err := readValue(r, vet)
			if err != nil {
				return reflect.Value{}, err
			}
			m.SetMapIndex(k, v)
		}
		return m, nil
	}
	return reflect.Value{}, fmt.Errorf("mm: unsupported map value kind %s", t.Kind())
}

// ── 解碼 ────────────────────────────────────────────────────────────────────

type mmDecoder struct {
	names   []string         // ASTTree.Names 字典（K/N 解析目標）
	table   []*ASTNode       // 節點表
	built   []reflect.Value  // 索引 → 已建節點指標（惰性 memo）
	done    []bool           // 對應 built 是否已填完
	semCtx  *SemanticContext // 建出的唯一 SemanticContext（nodeSem 回填目標）
	pending []semPair        // 屬主 → 語義條目（全部節點建完後統一回填）
}

// name 解析 1-based 字典索引（0 或越界＝wire 損壞）。
func (d *mmDecoder) name(i uint16) (string, error) {
	if i == 0 || int(i) > len(d.names) {
		return "", fmt.Errorf("mm: name index %d out of dictionary (%d)", i, len(d.names))
	}
	return d.names[i-1], nil
}

type semPair struct {
	owner Node
	sem   *NodeSemantics
}

// DecodeASTTree 把扁平節點表還原成 *Program。版本不符一律硬錯（不靜默回退）。
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
	if t.Root < 0 || int(t.Root) >= len(t.Table) {
		return nil, fmt.Errorf("mm: bad root index %d (table %d)", t.Root, len(t.Table))
	}
	d := &mmDecoder{
		names: t.Names,
		table: make([]*ASTNode, len(t.Table)),
		built: make([]reflect.Value, len(t.Table)),
		done:  make([]bool, len(t.Table)),
	}
	for i := range t.Table {
		n := t.Table[i]
		d.table[i] = &n
	}
	v, err := d.build(int(t.Root))
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
			d.semCtx.nodeSem[pr.owner] = pr.sem
		}
	}
	return p, nil
}

// build 惰性還原表索引 idx 处的節點（memo 於遞歸前寫入，可容忍異常環）。
func (d *mmDecoder) build(idx int) (reflect.Value, error) {
	if idx < 0 || idx >= len(d.table) {
		return reflect.Value{}, fmt.Errorf("mm: child index out of range %d", idx)
	}
	if d.done[idx] {
		return d.built[idx], nil
	}
	n := d.table[idx]
	kname, err := d.name(n.K)
	if err != nil {
		return reflect.Value{}, err
	}
	typ, ok := mmKindTypes[kname]
	if !ok {
		return reflect.Value{}, fmt.Errorf("mm: unknown node kind %q (registry has %d kinds)", kname, len(mmKindTypes))
	}
	pv := reflect.New(typ)
	d.built[idx] = pv
	d.done[idx] = true // 先標記，避免環回遞迴
	if kname == "SemanticContext" && d.semCtx == nil {
		d.semCtx = pv.Interface().(*SemanticContext)
	}
	sv := pv.Elem()

	for i := range n.F {
		lf := &n.F[i]
		if lf.Kind == kSem {
			if err := d.attachSem(pv, lf); err != nil {
				return reflect.Value{}, err
			}
			continue
		}
		fname, err := d.name(lf.N)
		if err != nil {
			return reflect.Value{}, err
		}
		f := sv.FieldByName(fname)
		if !f.IsValid() {
			return reflect.Value{}, fmt.Errorf("mm: %s has no field %q", kname, fname)
		}
		if err := d.apply(f, lf); err != nil {
			return reflect.Value{}, fmt.Errorf("mm: %s.%s: %w", kname, fname, err)
		}
	}
	return pv, nil
}

// apply 依目標欄位型別 dst 把一個欄位寫回（dst 必須可位址）。
func (d *mmDecoder) apply(dst reflect.Value, lf *ASTField) error {
	kind := lf.Kind
	// 空的非 nil 容器：Kind／欄位在場而 Vals 空，下方各型別驅動分支對空輸入
	// 天然產出 len 0 非 nil 容器（nil 已由「欄位缺席」表達）。
	switch kind {
	case kStr:
		if dst.Kind() != reflect.String {
			return fmt.Errorf("kind s on %s", dst.Kind())
		}
		dst.SetString(joinVals(lf.Vals))
		return nil
	case kBool:
		if dst.Kind() != reflect.Bool {
			return fmt.Errorf("kind b on %s", dst.Kind())
		}
		dst.SetBool(true) // 只在 true 時發出；缺席即 false
		return nil
	case kInt:
		return setInt(dst, joinVals(lf.Vals))
	case kFlt:
		if dst.Kind() != reflect.Float32 && dst.Kind() != reflect.Float64 {
			return fmt.Errorf("kind f on %s", dst.Kind())
		}
		x, err := strconv.ParseFloat(joinVals(lf.Vals), 64)
		if err != nil {
			return err
		}
		dst.SetFloat(x)
		return nil
	case kBytes:
		raw := []byte(joinVals(lf.Vals))
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
			return fmt.Errorf("mm: bytes into %s", dst.Kind())
		}
		return nil
	case kMap:
		r := &mapReader{b: []byte(joinVals(lf.Vals))}
		m, err := readMap(r, dst.Type())
		if err != nil {
			return err
		}
		dst.Set(m)
		return nil
	case kStrs:
		et := dst.Type().Elem()
		vals := lf.Vals
		switch dst.Kind() {
		case reflect.Slice:
			sl := reflect.MakeSlice(dst.Type(), len(vals), len(vals))
			for i, s := range vals {
				if err := setScalarText(sl.Index(i), et, s); err != nil {
					return fmt.Errorf("strs[%d]: %w", i, err)
				}
			}
			dst.Set(sl)
		case reflect.Array:
			for i, s := range vals {
				if i >= dst.Len() {
					return fmt.Errorf("mm: array overflow (%d > %d)", i, dst.Len())
				}
				if err := setScalarText(dst.Index(i), et, s); err != nil {
					return fmt.Errorf("strs[%d]: %w", i, err)
				}
			}
		default:
			return fmt.Errorf("mm: strs into %s", dst.Kind())
		}
		return nil
	case kNode:
		vals := lf.Vals
		if len(vals) != 1 {
			return fmt.Errorf("mm: node field has %d vals, want 1", len(vals))
		}
		idx, err := strconv.Atoi(vals[0])
		if err != nil {
			return fmt.Errorf("mm: node index %q: %w", vals[0], err)
		}
		cv, err := d.build(idx)
		if err != nil {
			return err
		}
		return assignNode(dst, cv)
	case kNodes:
		vals := lf.Vals
		switch dst.Kind() {
		case reflect.Slice:
			sl := reflect.MakeSlice(dst.Type(), len(vals), len(vals))
			for i, s := range vals {
				if err := d.setNodeElem(sl.Index(i), s); err != nil {
					return fmt.Errorf("nodes[%d]: %w", i, err)
				}
			}
			dst.Set(sl)
		case reflect.Array:
			for i, s := range vals {
				if i >= dst.Len() {
					return fmt.Errorf("mm: array overflow (%d > %d)", i, dst.Len())
				}
				if err := d.setNodeElem(dst.Index(i), s); err != nil {
					return fmt.Errorf("nodes[%d]: %w", i, err)
				}
			}
		default:
			return fmt.Errorf("mm: nodes into %s", dst.Kind())
		}
		return nil
	}
	return fmt.Errorf("mm: unknown field kind %d", lf.Kind)
}

// setNodeElem 還原一個節點 slice 的元素；-1（nilChildIndex）是 nil 元素佔位（保零值）。
func (d *mmDecoder) setNodeElem(dst reflect.Value, s string) error {
	if s == nilChildIndex {
		return nil
	}
	idx, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("node index %q: %w", s, err)
	}
	cv, err := d.build(idx)
	if err != nil {
		return err
	}
	return assignNode(dst, cv)
}

// attachSem 把 Kind=sem 保留欄位（Vals=[NodeSemantics 索引]）配對到屬主節點。
func (d *mmDecoder) attachSem(owner reflect.Value, lf *ASTField) error {
	vals := lf.Vals
	if len(vals) != 1 {
		return fmt.Errorf("mm: sem field has %d vals, want 1", len(vals))
	}
	idx, err := strconv.Atoi(vals[0])
	if err != nil {
		return fmt.Errorf("mm: sem index %q: %w", vals[0], err)
	}
	ev, err := d.build(idx)
	if err != nil {
		return err
	}
	sem, ok := ev.Interface().(*NodeSemantics)
	if !ok {
		return fmt.Errorf("mm: sem field is %T, want *parser.NodeSemantics", ev.Interface())
	}
	node, ok := owner.Interface().(Node)
	if !ok {
		return fmt.Errorf("mm: node_sem owner %T does not implement Node", owner.Interface())
	}
	d.pending = append(d.pending, semPair{owner: node, sem: sem})
	return nil
}

// setInt 依 dst 的整型寬度把十進位文字解析並寫入。
func setInt(dst reflect.Value, s string) error {
	switch dst.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		dst.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		dst.SetUint(n)
	default:
		return fmt.Errorf("kind i on %s", dst.Kind())
	}
	return nil
}

// setScalarText 依目標型別把一段文字解析回標量（用於 strs 元素與 map key）。
func setScalarText(dst reflect.Value, t reflect.Type, s string) error {
	switch t.Kind() {
	case reflect.String:
		dst.SetString(s)
		return nil
	case reflect.Bool:
		dst.SetBool(s == "true")
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return setInt(dst, s)
	case reflect.Float32, reflect.Float64:
		x, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		dst.SetFloat(x)
		return nil
	case reflect.Interface:
		// map key 是介面（少見）：原樣存字串。
		if dst.CanAddr() && dst.Kind() == reflect.Interface {
			dst.Set(reflect.ValueOf(s))
			return nil
		}
	}
	return fmt.Errorf("mm: cannot parse %q into %s", s, t)
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
