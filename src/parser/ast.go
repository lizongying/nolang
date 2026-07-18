package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// ---- Node interface ----

type Node interface {
	Pos() lexer.Position
	EndPos() lexer.Position
}

type Statement interface {
	Node
	statementNode()
}

type Expression interface {
	Node
	expressionNode()
}

// ---- Type interface ----

type Type interface {
	Node
	String() string
	typeNode()
}

type NamedType struct {
	Token lexer.Token
	Value string
}

func (nt *NamedType) typeNode()              {}
func (nt *NamedType) Pos() lexer.Position    { return posFromToken(nt.Token) }
func (nt *NamedType) EndPos() lexer.Position { return posFromToken(nt.Token) }
func (nt *NamedType) String() string         { return nt.Value }

type ArrayType struct {
	Token lexer.Token // [
	Size  Expression
	Elem  Type
}

func (at *ArrayType) typeNode()              {}
func (at *ArrayType) Pos() lexer.Position    { return posFromToken(at.Token) }
func (at *ArrayType) EndPos() lexer.Position { return at.Elem.EndPos() }
func (at *ArrayType) String() string {
	if at.Size != nil {
		switch s := at.Size.(type) {
		case *IntegerLiteral:
			return fmt.Sprintf("[%d]%s", s.Value, at.Elem.String())
		case *Identifier:
			return "[" + s.Value + "]" + at.Elem.String()
		default:
			return "[" + exprToString(s) + "]" + at.Elem.String()
		}
	}
	return "[?]" + at.Elem.String()
}

func exprToString(e Expression) string {
	switch ex := e.(type) {
	case *IntegerLiteral:
		return strconv.FormatInt(ex.Value, 10)
	case *Identifier:
		return ex.Value
	case *StringLiteral:
		if ex.Raw != "" {
			return ex.Raw
		}
		return "'" + ex.Value + "'"
	case *InfixExpression:
		// Constant fold integer expressions for display: 160+16 → 176
		if isInt, val := evalConstIntExpr(ex); isInt {
			return strconv.FormatInt(val, 10)
		}
		return exprToString(ex.Left) + " " + ex.Operator + " " + exprToString(ex.Right)
	default:
		return fmt.Sprintf("%v", e)
	}
}

// evalConstIntExpr evaluates a constant integer InfixExpression (e.g., 160+16).
// Returns (true, result) if both sides are integer literals with a supported operator.
func evalConstIntExpr(e *InfixExpression) (bool, int64) {
	leftVal, okLeft := e.Left.(*IntegerLiteral)
	rightVal, okRight := e.Right.(*IntegerLiteral)
	if !okLeft || !okRight {
		return false, 0
	}
	switch e.Operator {
	case "+":
		return true, leftVal.Value + rightVal.Value
	case "-":
		return true, leftVal.Value - rightVal.Value
	case "*":
		return true, leftVal.Value * rightVal.Value
	case "/":
		if rightVal.Value != 0 {
			return true, leftVal.Value / rightVal.Value
		}
	}
	return false, 0
}

type SliceType struct {
	Token lexer.Token // [
	Elem  Type
}

func (st *SliceType) typeNode()              {}
func (st *SliceType) Pos() lexer.Position    { return posFromToken(st.Token) }
func (st *SliceType) EndPos() lexer.Position { return st.Elem.EndPos() }
func (st *SliceType) String() string         { return "[]" + st.Elem.String() }

// MapType represents a map type: [K]V where K is the key type and V is the value type.
// Syntax: m [str]i64 declares a map with str keys and i64 values.
// Distinct from ArrayType [N]T where N is a size; here the bracket content is a type.
type MapType struct {
	Token lexer.Token // [
	Key   Type
	Value Type
}

func (mt *MapType) typeNode()              {}
func (mt *MapType) Pos() lexer.Position    { return posFromToken(mt.Token) }
func (mt *MapType) EndPos() lexer.Position { return mt.Value.EndPos() }
func (mt *MapType) String() string {
	return "[" + mt.Key.String() + "]" + mt.Value.String()
}

// LLVMName returns the LLVM struct name for this map type, e.g. "hashmap-str-i64".
// Used by codegen to derive %hashmap-K-V struct names; distinct from String()
// which returns the spec-mandated [K]V form (ambiguous with [N]T arrays).
func (mt *MapType) LLVMName() string {
	return "hashmap-" + mt.Key.String() + "-" + mt.Value.String()
}

// MapPair represents a single key:value pair in a map literal.
type MapPair struct {
	Token lexer.Token // the key's first token (or colon token)
	Key   Expression
	Value Expression
}

func (mp *MapPair) Pos() lexer.Position    { return mp.Key.Pos() }
func (mp *MapPair) EndPos() lexer.Position { return mp.Value.EndPos() }
func (mp *MapPair) String() string {
	return exprToString(mp.Key) + ":" + exprToString(mp.Value)
}

// FunctionType represents a function type: (params) (results)?
// Syntax: cb ()() or cb ()(i64) or cb (n i64)(r i64)
// Both Params and Results use the same Parameter struct as function definitions.
// Name-less entries (e.g. (i64) for a single i64 param without a name) are
// represented with an empty Name field.
// String() keeps the internal "fn(...)" marker prefix so codegen / transpiler
// can identify function-type variables via HasPrefix(t, "fn(").
type FunctionType struct {
	Token   lexer.Token // the opening '(' token
	Params  []*Parameter
	Results []*Parameter
}

func (ft *FunctionType) typeNode()           {}
func (ft *FunctionType) Pos() lexer.Position { return posFromToken(ft.Token) }
func (ft *FunctionType) EndPos() lexer.Position {
	if len(ft.Results) > 0 {
		return ft.Results[len(ft.Results)-1].EndPos()
	}
	if len(ft.Params) > 0 {
		return ft.Params[len(ft.Params)-1].EndPos()
	}
	return posFromToken(ft.Token)
}
func (ft *FunctionType) String() string {
	var sb strings.Builder
	sb.WriteString("fn(")
	for i, p := range ft.Params {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(p.Type.String())
	}
	sb.WriteString(")")
	if len(ft.Results) > 0 {
		sb.WriteString("(")
		for i, r := range ft.Results {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(r.Type.String())
		}
		sb.WriteString(")")
	}
	return sb.String()
}

// ---- Helper ----

func posFromToken(t lexer.Token) lexer.Position {
	return lexer.Position{Line: t.Line, Column: t.Column}
}

// Convenience Type variables for use in builtin definitions.
var (
	TypeByte = &NamedType{Value: "byte"}
	TypeBool = &NamedType{Value: "bool"}
	TypeChar = &NamedType{Value: "char"}
	TypeStr  = &NamedType{Value: "str"}
	TypeI8   = &NamedType{Value: "i8"}
	TypeI16  = &NamedType{Value: "i16"}
	TypeI32  = &NamedType{Value: "i32"}
	TypeI64  = &NamedType{Value: "i64"}
	TypeU8   = &NamedType{Value: "u8"}
	TypeU16  = &NamedType{Value: "u16"}
	TypeU32  = &NamedType{Value: "u32"}
	TypeU64  = &NamedType{Value: "u64"}
	TypeF32  = &NamedType{Value: "f32"}
	TypeF64  = &NamedType{Value: "f64"}
	TypeInt  = TypeI64
)

func typeString(n Node) string {
	switch n := n.(type) {
	case *Identifier:
		return n.Value
	case *NamedType:
		return n.Value
	case *ArrayType:
		return n.String()
	case *SliceType:
		return n.String()
	case *MapType:
		return n.String()
	case *NullableType:
		return "?" + typeString(n.Type)
	case *PointerType:
		return "*" + typeString(n.Type)
	case *FunctionType:
		return n.String()
	default:
		return fmt.Sprintf("%T", n)
	}
}

// ---- Comment model ----

type CommentKind int

const (
	NormalComment CommentKind = iota
	DocComment
)

// Comment represents a single comment line.
type Comment struct {
	Pos  lexer.Position
	End  lexer.Position
	Kind CommentKind
	Text string // content without marker prefix (e.g. without '//' or ';')
	// Marker is the comment start symbol: "//" or ";". Empty means "//" (default).
	Marker string
}

// CommentGroup represents a sequence of consecutive comment lines.
type CommentGroup struct {
	List  []*Comment
	Start lexer.Position // position of first comment
	End   lexer.Position // position of last comment
}

// CommentedNode is embedded in all statement types that can have comments.
type CommentedNode struct {
	Doc     *CommentGroup // standalone line(s) above the node
	Comment *CommentGroup // inline comment on the same line
}

func (cn *CommentedNode) GetDoc() *CommentGroup     { return cn.Doc }
func (cn *CommentedNode) GetComment() *CommentGroup { return cn.Comment }

// ---- Program ----

type Program struct {
	Statements       []Statement
	FreeComments     []*CommentGroup // standalone comments at file start, between stmts, EOF
	TrailingComments *CommentGroup
	Warnings         []string // parser warnings (e.g., dead code, deprecation)
}

func (p *Program) Pos() lexer.Position {
	if len(p.Statements) > 0 {
		return p.Statements[0].Pos()
	}
	return lexer.Position{}
}

func (p *Program) EndPos() lexer.Position {
	if len(p.Statements) > 0 {
		return p.Statements[len(p.Statements)-1].EndPos()
	}
	return lexer.Position{}
}

// ---- Statements ----

// use path.fn 或 use path.fn alias
type UseStatement struct {
	Token     lexer.Token
	Path      string // 模組路徑（無副檔名）
	Function  string // 函數名
	Alias     string // 可選別名（空 = 不使用別名）
	AsKeyword bool   // true if alias used 'as' keyword (e.g., "# path.fn as alias")
	CommentedNode
}

func (us *UseStatement) statementNode()         {}
func (us *UseStatement) Pos() lexer.Position    { return posFromToken(us.Token) }
func (us *UseStatement) EndPos() lexer.Position { return posFromToken(us.Token) }

// @ path.fn 或 @ path.fn alias
type ExportStatement struct {
	Token     lexer.Token
	Path      string // 模組路徑（無副檔名）
	Function  string // 函數/常量/枚舉名
	Alias     string // 可選別名（空 = 不使用別名）
	AsKeyword bool   // true if alias used 'as' keyword (e.g., "@ path.fn as alias")
	CommentedNode
}

func (es *ExportStatement) statementNode()         {}
func (es *ExportStatement) Pos() lexer.Position    { return posFromToken(es.Token) }
func (es *ExportStatement) EndPos() lexer.Position { return posFromToken(es.Token) }

// MultiAssignStatement represents multi-variable assignment:
//
//	v1, v2 = expr
//	fields[n], pos = expr
//
// The left-side targets are treated as new definitions if not already defined.
// Targets can be Identifiers (variable definition/assignment) or IndexExpressions
// (array element assignment).
type MultiAssignStatement struct {
	Token   lexer.Token  // the ASSIGN token
	Targets []Expression // left-side targets: Identifier or IndexExpression
	Value   Expression
	CommentedNode
}

func (mas *MultiAssignStatement) statementNode()         {}
func (mas *MultiAssignStatement) Pos() lexer.Position    { return posFromToken(mas.Token) }
func (mas *MultiAssignStatement) EndPos() lexer.Position { return mas.Value.EndPos() }

// a u8 = 8
type LetStatement struct {
	Token         lexer.Token
	Name          *Identifier
	Type          Type
	Value         Expression
	IsSynthetic   bool               // compiler-injected (e.g. `it = matched`), not from source
	SyntheticEnd  lexer.Position     // override EndPos for synthetic bindings
	GenericParams []string           // 泛型型別參數，來自 #{generic=[K,V]} 註解
	Annotations   []*AnnotationEntry // 來自前置 #{...} 註解的條目
	CommentedNode
}

func (ls *LetStatement) statementNode()      {}
func (ls *LetStatement) Pos() lexer.Position { return posFromToken(ls.Token) }
func (ls *LetStatement) EndPos() lexer.Position {
	if ls.IsSynthetic && (ls.SyntheticEnd.Line != 0 || ls.SyntheticEnd.Column != 0) {
		return ls.SyntheticEnd
	}
	if ls.Value != nil {
		return ls.Value.EndPos()
	}
	if ls.Type != nil {
		return ls.Type.EndPos()
	}
	return ls.Name.EndPos()
}

type Identifier struct {
	Token lexer.Token
	Value string
}

func (i *Identifier) expressionNode()        {}
func (i *Identifier) Pos() lexer.Position    { return posFromToken(i.Token) }
func (i *Identifier) EndPos() lexer.Position { return posFromToken(i.Token) }

type ReturnStatement struct {
	Token       lexer.Token
	ReturnValue Expression
	CommentedNode
}

func (rs *ReturnStatement) statementNode()      {}
func (rs *ReturnStatement) Pos() lexer.Position { return posFromToken(rs.Token) }
func (rs *ReturnStatement) EndPos() lexer.Position {
	if rs.ReturnValue != nil {
		return rs.ReturnValue.EndPos()
	}
	return posFromToken(rs.Token)
}

type ExpressionStatement struct {
	Token       lexer.Token
	Expression  Expression
	Annotations []*AnnotationEntry // 來自前置 #{...} 註解的條目（如 #{mac-arm64}, #{linux-amd64}）
	CommentedNode
}

func (es *ExpressionStatement) statementNode()      {}
func (es *ExpressionStatement) Pos() lexer.Position { return posFromToken(es.Token) }
func (es *ExpressionStatement) EndPos() lexer.Position {
	if es.Expression != nil {
		return es.Expression.EndPos()
	}
	return posFromToken(es.Token)
}

type BlockStatement struct {
	Token      lexer.Token    // the {
	RBrace     lexer.Position // position of }
	Statements []Statement
	CommentedNode
	TrailingComments    *CommentGroup   // standalone statements before }
	ClosingBraceComment *CommentGroup   // comment on the } line itself
	OpeningBraceComment *CommentGroup   // comment on the { line itself
	BetweenComments     []*CommentGroup // free-standing comment lines between statements
}

func (bs *BlockStatement) statementNode()         {}
func (bs *BlockStatement) Pos() lexer.Position    { return posFromToken(bs.Token) }
func (bs *BlockStatement) EndPos() lexer.Position { return bs.RBrace }

type Parameter struct {
	Token       lexer.Token
	Name        string
	Type        Type
	DefaultExpr Expression // 參數默認值表達式（如 `max-fields i64 = 1024` 中的 `1024`），nil 表示無默認值
}

func (p *Parameter) expressionNode()        {}
func (p *Parameter) Pos() lexer.Position    { return posFromToken(p.Token) }
func (p *Parameter) EndPos() lexer.Position { return posFromToken(p.Token) }

// FuncSignature captures the shared signature of FunctionDefinition and FunctionLiteral.
type FuncSignature struct {
	Parameters    []*Parameter
	Results       []*Parameter
	GenericParams []*Identifier // 泛型參數：<N, M, ...>
	IsVariadic    bool          // 是否有 ...any 可變參數
	VariadicUnion string        // 當 IsVariadic && 參數類型是 union alias 時記錄 union 名稱；codegen 會單態化
	GenericUnion  string        // 當函數的參數/結果型別是 union alias（非 variadic）時記錄 union 名稱；codegen 會單態化
}

type FunctionDefinition struct {
	Token lexer.Token
	Name  string
	FuncSignature
	Body        *BlockStatement
	ColonSyntax bool               // 是否為冒號語法 foo: (a int) { }
	Annotations []*AnnotationEntry // 來自前置 #{...} 註解的條目（如 #{mac-arm64}, #{linux-amd64}）
	CommentedNode
}

func (fd *FunctionDefinition) statementNode()         {}
func (fd *FunctionDefinition) Pos() lexer.Position    { return posFromToken(fd.Token) }
func (fd *FunctionDefinition) EndPos() lexer.Position { return fd.Body.EndPos() }

// ExternStatement — FFI 宣告：#{c} \n name = (params) (results)
// 僅為宣告，無函式主體；對應外部 C 函式。
// 支援 #{c}（新語法）和 #c（舊語法，向後相容）。
type ExternStatement struct {
	Token       lexer.Token
	Lang        string // FFI language: "c", "cpp", etc.
	Name        *Identifier
	Parameters  []*Parameter
	Results     []*Parameter
	Annotations []*AnnotationEntry // 來自 #{c, ...} 的額外註解條目
	CommentedNode
}

func (es *ExternStatement) statementNode()       {}
func (es *ExternStatement) TokenLiteral() string { return es.Token.Literal }
func (es *ExternStatement) Pos() lexer.Position  { return posFromToken(es.Token) }
func (es *ExternStatement) EndPos() lexer.Position {
	if len(es.Results) > 0 {
		return es.Results[len(es.Results)-1].EndPos()
	}
	if len(es.Parameters) > 0 {
		return es.Parameters[len(es.Parameters)-1].EndPos()
	}
	return es.Name.EndPos()
}
func (es *ExternStatement) String() string {
	var out strings.Builder
	if es.Lang != "" {
		out.WriteString("#{")
		out.WriteString(es.Lang)
		for _, a := range es.Annotations {
			if a.Key == es.Lang {
				continue
			}
			out.WriteString(", ")
			out.WriteString(a.String())
		}
		out.WriteString("}\n")
	}
	out.WriteString(es.Name.Value)
	out.WriteString(" = (")
	for i, p := range es.Parameters {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(p.Name)
		if p.Type != nil {
			out.WriteString(" ")
			out.WriteString(p.Type.String())
		}
	}
	out.WriteString(") (")
	for i, r := range es.Results {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(r.Name)
		if r.Type != nil {
			out.WriteString(" ")
			out.WriteString(r.Type.String())
		}
	}
	out.WriteString(")")
	return out.String()
}

// ---- Annotation System ----

// AnnotationValue 是註解值的介面，支援布爾、整數、字串、識別字、陣列、範圍等值類型。
type AnnotationValue interface {
	Node
	annotationValueNode()
	String() string
}

// AnnotationBoolValue — 獨立布爾鍵（例如 #{debug} 中的 debug）
type AnnotationBoolValue struct {
	Token lexer.Token
}

func (v *AnnotationBoolValue) annotationValueNode()   {}
func (v *AnnotationBoolValue) Pos() lexer.Position    { return posFromToken(v.Token) }
func (v *AnnotationBoolValue) EndPos() lexer.Position { return posFromToken(v.Token) }
func (v *AnnotationBoolValue) String() string         { return "true" }

// AnnotationIntValue — 整數值（例如 max=100）
type AnnotationIntValue struct {
	Token lexer.Token
	Value int64
}

func (v *AnnotationIntValue) annotationValueNode()   {}
func (v *AnnotationIntValue) Pos() lexer.Position    { return posFromToken(v.Token) }
func (v *AnnotationIntValue) EndPos() lexer.Position { return posFromToken(v.Token) }
func (v *AnnotationIntValue) String() string         { return strconv.FormatInt(v.Value, 10) }

// AnnotationStringValue — 字串值（例如 name='hello'）
type AnnotationStringValue struct {
	Token lexer.Token
	Value string
}

func (v *AnnotationStringValue) annotationValueNode()   {}
func (v *AnnotationStringValue) Pos() lexer.Position    { return posFromToken(v.Token) }
func (v *AnnotationStringValue) EndPos() lexer.Position { return posFromToken(v.Token) }
func (v *AnnotationStringValue) String() string         { return "'" + v.Value + "'" }

// AnnotationIdentValue — 識別字值（例如 mode=fast）
type AnnotationIdentValue struct {
	Token lexer.Token
	Value string
}

func (v *AnnotationIdentValue) annotationValueNode()   {}
func (v *AnnotationIdentValue) Pos() lexer.Position    { return posFromToken(v.Token) }
func (v *AnnotationIdentValue) EndPos() lexer.Position { return posFromToken(v.Token) }
func (v *AnnotationIdentValue) String() string         { return v.Value }

// AnnotationArrayValue — 陣列值（例如 derive=[Serialize, Deserialize]）
type AnnotationArrayValue struct {
	Token    lexer.Token
	Elements []AnnotationValue
}

func (v *AnnotationArrayValue) annotationValueNode()   {}
func (v *AnnotationArrayValue) Pos() lexer.Position    { return posFromToken(v.Token) }
func (v *AnnotationArrayValue) EndPos() lexer.Position { return posFromToken(v.Token) }
func (v *AnnotationArrayValue) String() string {
	var out strings.Builder
	out.WriteString("[")
	for i, el := range v.Elements {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(el.String())
	}
	out.WriteString("]")
	return out.String()
}

// AnnotationRangeValue — 範圍值（例如 range=[0..256)）
type AnnotationRangeValue struct {
	Token    lexer.Token
	Start    AnnotationValue
	End      AnnotationValue
	LeftInc  bool // [ = true, ( = false
	RightInc bool // ] = true, ) = false
}

func (v *AnnotationRangeValue) annotationValueNode()   {}
func (v *AnnotationRangeValue) Pos() lexer.Position    { return posFromToken(v.Token) }
func (v *AnnotationRangeValue) EndPos() lexer.Position { return posFromToken(v.Token) }
func (v *AnnotationRangeValue) String() string {
	var out strings.Builder
	if v.LeftInc {
		out.WriteString("[")
	} else {
		out.WriteString("(")
	}
	if v.Start != nil {
		out.WriteString(v.Start.String())
	}
	out.WriteString("..")
	if v.End != nil {
		out.WriteString(v.End.String())
	}
	if v.RightInc {
		out.WriteString("]")
	} else {
		out.WriteString(")")
	}
	return out.String()
}

// AnnotationEntry — 註解中的單個鍵值對或獨立布爾鍵。
type AnnotationEntry struct {
	Key   string          // 鍵名，如 "derive"、"range"、"max"、"debug"、"c"
	Value AnnotationValue // 值；nil 表示布爾獨立鍵
	Token lexer.Token     // 鍵名的 token
}

func (e *AnnotationEntry) Pos() lexer.Position { return posFromToken(e.Token) }
func (e *AnnotationEntry) EndPos() lexer.Position {
	if e.Value != nil {
		return e.Value.EndPos()
	}
	return posFromToken(e.Token)
}
func (e *AnnotationEntry) String() string {
	if e.Value != nil {
		return e.Key + "=" + e.Value.String()
	}
	return e.Key
}

// IsBool 報告此 entry 是否為獨立布爾鍵（無值）。
func (e *AnnotationEntry) IsBool() bool { return e.Value == nil }

// AnnotationStatement — #{...} 註解語句。
// 當註解包含 FFI 語言鍵（如 c、cpp、rust）且後續為函式宣告時，
// 解析器會將其轉換為 ExternStatement，此時 AnnotationStatement 不會出現在 AST 中。
// 其他註解（如 #{derive=[Serialize, Deserialize], debug}）作為獨立語句保留。
type AnnotationStatement struct {
	Token   lexer.Token
	Entries []*AnnotationEntry
	CommentedNode
}

func (as *AnnotationStatement) statementNode()         {}
func (as *AnnotationStatement) TokenLiteral() string   { return as.Token.Literal }
func (as *AnnotationStatement) Pos() lexer.Position    { return posFromToken(as.Token) }
func (as *AnnotationStatement) EndPos() lexer.Position { return posFromToken(as.Token) }
func (as *AnnotationStatement) String() string {
	var out strings.Builder
	out.WriteString("#{")
	for i, e := range as.Entries {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(e.String())
	}
	out.WriteString("}")
	return out.String()
}

// GetFFILang 檢查註解是否包含 FFI 語言鍵。
// 若存在，返回語言名稱（如 "c"、"cpp"）；否則返回空字串。
func (as *AnnotationStatement) GetFFILang() string {
	for _, e := range as.Entries {
		if e.IsBool() && isFFILang(e.Key) {
			return e.Key
		}
	}
	return ""
}

// isFFILang 報告給定字串是否為已知的 FFI 語言名稱。
func isFFILang(lang string) bool {
	switch lang {
	case "c", "cpp", "rust", "go", "zig", "objc", "asm":
		return true
	}
	return false
}

type FunctionLiteral struct {
	Token lexer.Token
	FuncSignature
	Body *BlockStatement
}

func (fl *FunctionLiteral) expressionNode()        {}
func (fl *FunctionLiteral) Pos() lexer.Position    { return posFromToken(fl.Token) }
func (fl *FunctionLiteral) EndPos() lexer.Position { return fl.Body.EndPos() }

type CallExpression struct {
	Token       lexer.Token
	Function    Expression
	GenericArgs []Expression // 泛型引數：<type, ...>
	Arguments   []Expression
}

func (ce *CallExpression) expressionNode()     {}
func (ce *CallExpression) Pos() lexer.Position { return posFromToken(ce.Token) }
func (ce *CallExpression) EndPos() lexer.Position {
	if len(ce.Arguments) > 0 {
		return ce.Arguments[len(ce.Arguments)-1].EndPos()
	}
	if len(ce.GenericArgs) > 0 {
		return ce.GenericArgs[len(ce.GenericArgs)-1].EndPos()
	}
	if ce.Function != nil {
		return ce.Function.EndPos()
	}
	return posFromToken(ce.Token)
}

type DotExpression struct {
	Token    lexer.Token
	Receiver Expression
	Property string
}

func (de *DotExpression) expressionNode()        {}
func (de *DotExpression) Pos() lexer.Position    { return posFromToken(de.Token) }
func (de *DotExpression) EndPos() lexer.Position { return posFromToken(de.Token) }

type NullableType struct {
	Token lexer.Token
	Type  Type // implements both Expression and Type
}

func (nt *NullableType) expressionNode()        {}
func (nt *NullableType) typeNode()              {}
func (nt *NullableType) Pos() lexer.Position    { return posFromToken(nt.Token) }
func (nt *NullableType) EndPos() lexer.Position { return nt.Type.EndPos() }
func (nt *NullableType) String() string         { return "?" + typeString(nt.Type) }

type PointerType struct {
	Token lexer.Token
	Type  Type // implements both Expression and Type
}

func (pt *PointerType) expressionNode()     {}
func (pt *PointerType) typeNode()           {}
func (pt *PointerType) Pos() lexer.Position { return posFromToken(pt.Token) }
func (pt *PointerType) EndPos() lexer.Position {
	if pt.Type != nil {
		return pt.Type.EndPos()
	}
	return posFromToken(pt.Token)
}
func (pt *PointerType) String() string { return "*" + typeString(pt.Type) }

// GroupedExpression represents a parenthesized expression: (expr)
type GroupedExpression struct {
	Token      lexer.Token
	Expression Expression
}

func (ge *GroupedExpression) expressionNode()        {}
func (ge *GroupedExpression) Pos() lexer.Position    { return posFromToken(ge.Token) }
func (ge *GroupedExpression) EndPos() lexer.Position { return posFromToken(ge.Token) }

type IntegerLiteral struct {
	Token lexer.Token
	Value int64
	Raw   string
}

func (il *IntegerLiteral) expressionNode()        {}
func (il *IntegerLiteral) Pos() lexer.Position    { return posFromToken(il.Token) }
func (il *IntegerLiteral) EndPos() lexer.Position { return posFromToken(il.Token) }

type ByteLiteral struct {
	Token lexer.Token
	Value int64
	Raw   string
}

func (bl *ByteLiteral) expressionNode()        {}
func (bl *ByteLiteral) Pos() lexer.Position    { return posFromToken(bl.Token) }
func (bl *ByteLiteral) EndPos() lexer.Position { return posFromToken(bl.Token) }

type FloatLiteral struct {
	Token lexer.Token
	Value float64
	Raw   string
}

func (fl *FloatLiteral) expressionNode()        {}
func (fl *FloatLiteral) Pos() lexer.Position    { return posFromToken(fl.Token) }
func (fl *FloatLiteral) EndPos() lexer.Position { return posFromToken(fl.Token) }

type StringLiteral struct {
	Token lexer.Token
	Value string
	Raw   string
}

func (sl *StringLiteral) expressionNode()        {}
func (sl *StringLiteral) Pos() lexer.Position    { return posFromToken(sl.Token) }
func (sl *StringLiteral) EndPos() lexer.Position { return posFromToken(sl.Token) }

type CharLiteral struct {
	Token lexer.Token
	Value string // single Unicode character
	Raw   string
}

func (cl *CharLiteral) expressionNode()        {}
func (cl *CharLiteral) Pos() lexer.Position    { return posFromToken(cl.Token) }
func (cl *CharLiteral) EndPos() lexer.Position { return posFromToken(cl.Token) }

// RegexLiteral represents a JS-style regex literal: /pattern/flags
// The opening delimiter is a single '/', disambiguated from division by
// context (see lexer.isRegexStart). The pattern ends at the next unescaped /,
// followed by optional ASCII flag letters (g, i, m, s, ...).
//
// Examples:
//
//	/abc/        → Pattern="abc", Flags=""
//	/\d+/        → Pattern="\\d+", Flags=""
//	/hello/gi    → Pattern="hello", Flags="gi"
type RegexLiteral struct {
	Token   lexer.Token
	Pattern string // regex pattern content (without delimiters)
	Flags   string // optional flags (e.g. "g", "i", "gi", "m", "s")
}

func (rl *RegexLiteral) expressionNode()        {}
func (rl *RegexLiteral) Pos() lexer.Position    { return posFromToken(rl.Token) }
func (rl *RegexLiteral) EndPos() lexer.Position { return posFromToken(rl.Token) }

type BooleanLiteral struct {
	Token lexer.Token
	Value bool
}

func (bl *BooleanLiteral) expressionNode()        {}
func (bl *BooleanLiteral) Pos() lexer.Position    { return posFromToken(bl.Token) }
func (bl *BooleanLiteral) EndPos() lexer.Position { return posFromToken(bl.Token) }

type NilLiteral struct {
	Token lexer.Token
}

func (nl *NilLiteral) expressionNode()        {}
func (nl *NilLiteral) Pos() lexer.Position    { return posFromToken(nl.Token) }
func (nl *NilLiteral) EndPos() lexer.Position { return posFromToken(nl.Token) }

type PrefixExpression struct {
	Token    lexer.Token
	Operator string
	Right    Expression
}

func (pe *PrefixExpression) expressionNode()     {}
func (pe *PrefixExpression) Pos() lexer.Position { return posFromToken(pe.Token) }
func (pe *PrefixExpression) EndPos() lexer.Position {
	if pe.Right == nil {
		return posFromToken(pe.Token)
	}
	return pe.Right.EndPos()
}

type InfixExpression struct {
	Token    lexer.Token
	Left     Expression
	Operator string
	Right    Expression
}

func (ie *InfixExpression) expressionNode()     {}
func (ie *InfixExpression) Pos() lexer.Position { return posFromToken(ie.Token) }
func (ie *InfixExpression) EndPos() lexer.Position {
	if ie.Right == nil {
		return posFromToken(ie.Token)
	}
	return ie.Right.EndPos()
}

// RunExpression: run <call-expression>
// Starts an async function call in a new thread, returns a task handle.
type RunExpression struct {
	Token lexer.Token
	Call  Expression // must be a *CallExpression
}

func (re *RunExpression) expressionNode()     {}
func (re *RunExpression) Pos() lexer.Position { return posFromToken(re.Token) }
func (re *RunExpression) EndPos() lexer.Position {
	if re.Call == nil {
		return posFromToken(re.Token)
	}
	return re.Call.EndPos()
}

// AwaitExpression: awy <expression>
// Waits for an async task to complete and returns the result.
type AwaitExpression struct {
	Token lexer.Token
	Right Expression // the task handle expression
}

func (ae *AwaitExpression) expressionNode()     {}
func (ae *AwaitExpression) Pos() lexer.Position { return posFromToken(ae.Token) }
func (ae *AwaitExpression) EndPos() lexer.Position {
	if ae.Right == nil {
		return posFromToken(ae.Token)
	}
	return ae.Right.EndPos()
}

// if
type IfExpression struct {
	Token       lexer.Token
	Condition   Expression
	Consequence *BlockStatement
	Alternative *BlockStatement
	// IsBareMatch 標記此 IfExpression 來自裸 match 表達式 `{ cond -> body }`，
	// 格式化器應輸出新式語法而非 if/else。
	IsBareMatch bool
	// MatchedExpr holds the matched expression in `x: { pattern -> body }` syntax.
	// When set, the formatter outputs `matched: { ... }` instead of if/else.
	MatchedExpr Expression
	// DotValBody marks that the wildcard alternative is an ok-> or .-> val branch
	// (not a catch-all else). The formatter outputs `ok -> body` instead of `-> body`.
	DotValBody *BlockStatement
	// IsStandalone marks a bare if-then expression written as `cond -> body`
	// without the enclosing `{ }` block. The formatter outputs `cond -> body`.
	IsStandalone bool
	// OpeningBraceComment holds comments on the same line as the opening `{`
	// of a bare match expression. The formatter outputs them inline after `{`.
	OpeningBraceComment *CommentGroup
}

func (ie *IfExpression) expressionNode()     {}
func (ie *IfExpression) Pos() lexer.Position { return posFromToken(ie.Token) }
func (ie *IfExpression) EndPos() lexer.Position {
	if ie.Alternative != nil {
		return ie.Alternative.EndPos()
	}
	return ie.Consequence.EndPos()
}

// for i in [a..b], (a..b], [a..b), (a..b)
type RangeExpression struct {
	Token    lexer.Token // [ or (
	Start    Expression  // a
	End      Expression  // b
	LeftInc  bool        // [ = true, ( = false
	RightInc bool        // ] = true, ) = false
}

func (re *RangeExpression) expressionNode()        {}
func (re *RangeExpression) Pos() lexer.Position    { return posFromToken(re.Token) }
func (re *RangeExpression) EndPos() lexer.Position { return posFromToken(re.Token) }

// nums[..], nums[1..], nums[..3], nums[1..3], nums[1..3), nums(1..3)
type SliceExpression struct {
	Token lexer.Token      // [ or (
	Left  Expression       // 被切割的数组/切片
	Range *RangeExpression // 範圍（nil = [..] 全切片）
}

func (se *SliceExpression) expressionNode()        {}
func (se *SliceExpression) Pos() lexer.Position    { return posFromToken(se.Token) }
func (se *SliceExpression) EndPos() lexer.Position { return posFromToken(se.Token) }

// arr[i], vec[i], str[i], map[key]
type IndexExpression struct {
	Token lexer.Token // [
	Left  Expression  // 被索引的對象
	Index Expression  // 索引值
}

func (ie *IndexExpression) expressionNode()        {}
func (ie *IndexExpression) Pos() lexer.Position    { return posFromToken(ie.Token) }
func (ie *IndexExpression) EndPos() lexer.Position { return posFromToken(ie.Token) }

// u.name = value 欄位賦值
type AssignExpression struct {
	Token lexer.Token
	Left  Expression // DotExpression
	Value Expression
}

func (ae *AssignExpression) expressionNode()        {}
func (ae *AssignExpression) Pos() lexer.Position    { return posFromToken(ae.Token) }
func (ae *AssignExpression) EndPos() lexer.Position { return ae.Value.EndPos() }

// 三元運算子
type ConditionalExpression struct {
	Token       lexer.Token
	Condition   Expression
	Consequence Expression
	Alternative Expression
}

func (ce *ConditionalExpression) expressionNode()     {}
func (ce *ConditionalExpression) Pos() lexer.Position { return posFromToken(ce.Token) }
func (ce *ConditionalExpression) EndPos() lexer.Position {
	if ce.Alternative != nil {
		return ce.Alternative.EndPos()
	}
	if ce.Consequence != nil {
		return ce.Consequence.EndPos()
	}
	return posFromToken(ce.Token)
}

// CastExpression represents a type cast: expr as Type
// All Nolang integers are stored as i64 in LLVM, so integer-to-integer
// casts are effectively no-ops at the IR level (see codegen in expr.go).
type CastExpression struct {
	Token lexer.Token // the `as` token
	Expr  Expression
	Type  Type
}

func (ce *CastExpression) expressionNode()     {}
func (ce *CastExpression) Pos() lexer.Position { return ce.Expr.Pos() }
func (ce *CastExpression) EndPos() lexer.Position {
	if ce.Type != nil {
		return ce.Type.EndPos()
	}
	return ce.Expr.EndPos()
}
func (ce *CastExpression) String() string {
	if ce.Type != nil {
		return exprToString(ce.Expr) + " as " + ce.Type.String()
	}
	return exprToString(ce.Expr) + " as ?"
}

// IterationExpr unifies the different kinds of for-range iteration.
type IterationExpr struct {
	Token     lexer.Token // the IN or ARROW token
	Variable  string
	Range     *RangeExpression // [a..b] etc.
	RangeStr  string           // string literal iteration
	RangeExpr Expression       // identifier or slice literal
}

func (ie *IterationExpr) expressionNode()        {}
func (ie *IterationExpr) Pos() lexer.Position    { return posFromToken(ie.Token) }
func (ie *IterationExpr) EndPos() lexer.Position { return posFromToken(ie.Token) }

type ForStatement struct {
	Token     lexer.Token
	Label     string // 循環名稱（空 = 未命名）
	Init      Statement
	Condition Expression
	Update    Statement
	Body      *BlockStatement
	IterRange *IterationExpr // unified iteration (range/str/ident/slice)
	CountExpr Expression     // ! { } 或 N * { } 語法
	CommentedNode
}

func (fs *ForStatement) statementNode()         {}
func (fs *ForStatement) Pos() lexer.Position    { return posFromToken(fs.Token) }
func (fs *ForStatement) EndPos() lexer.Position { return fs.Body.EndPos() }

type BreakStatement struct {
	Token lexer.Token
	Label string // 跳轉目標循環名稱（空 = 跳出當前循環）
	CommentedNode
}

func (bs *BreakStatement) statementNode()         {}
func (bs *BreakStatement) Pos() lexer.Position    { return posFromToken(bs.Token) }
func (bs *BreakStatement) EndPos() lexer.Position { return posFromToken(bs.Token) }

type ContinueStatement struct {
	Token lexer.Token
	Label string // 跳轉目標循環名稱（空 = 繼續當前循環）
	CommentedNode
}

func (cs *ContinueStatement) statementNode()         {}
func (cs *ContinueStatement) Pos() lexer.Position    { return posFromToken(cs.Token) }
func (cs *ContinueStatement) EndPos() lexer.Position { return posFromToken(cs.Token) }

type ArrayLiteral struct {
	Token    lexer.Token
	Size     Expression
	Elements []Expression
}

func (al *ArrayLiteral) expressionNode()     {}
func (al *ArrayLiteral) Pos() lexer.Position { return posFromToken(al.Token) }
func (al *ArrayLiteral) EndPos() lexer.Position {
	if len(al.Elements) > 0 {
		return al.Elements[len(al.Elements)-1].EndPos()
	}
	return posFromToken(al.Token)
}

// MapLiteral represents a map literal: { k1:v1, k2:v2, ... }
// Used in declarations like: m [str]i64 = { 'a':0, 'b':1 }
type MapLiteral struct {
	Token   lexer.Token // {
	Pairs   []MapPair
	MapType *MapType // optional: associated MapType for type inference
}

func (ml *MapLiteral) expressionNode()     {}
func (ml *MapLiteral) Pos() lexer.Position { return posFromToken(ml.Token) }
func (ml *MapLiteral) EndPos() lexer.Position {
	if len(ml.Pairs) > 0 {
		return ml.Pairs[len(ml.Pairs)-1].EndPos()
	}
	return posFromToken(ml.Token)
}
func (ml *MapLiteral) String() string {
	var sb strings.Builder
	sb.WriteString("{ ")
	for i, p := range ml.Pairs {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(p.String())
	}
	sb.WriteString(" }")
	return sb.String()
}
func (ml *MapLiteral) TokenLiteral() string { return "{" }

type SliceLiteral struct {
	Token    lexer.Token
	Elements []Expression
}

func (sl *SliceLiteral) expressionNode()     {}
func (sl *SliceLiteral) Pos() lexer.Position { return posFromToken(sl.Token) }
func (sl *SliceLiteral) EndPos() lexer.Position {
	if len(sl.Elements) > 0 {
		return sl.Elements[len(sl.Elements)-1].EndPos()
	}
	return posFromToken(sl.Token)
}

type StructField struct {
	Token       lexer.Token
	Name        string
	Type        Type
	ArraySize   int64 // >0 = 定長陣列 [N]type
	IsSlice     bool  // true = 切片 []type
	ReadOnly    bool  // true = read-only field modifier
	Sealed      bool  // true = sealed field modifier
	Value       Expression
	Annotations []*AnnotationEntry // 來自前置 #{...} 註解的條目
}

type EnumValue struct {
	Token lexer.Token
	Name  string
	Value int64
	// Explicit marks whether the value was written with `= <int>` in source.
	// When false (auto-assigned sequential value), the formatter must NOT emit
	// `= <int>` so the bare `red, green, blue` form is preserved. Codegen still
	// uses Value (the sequential index) regardless of Explicit.
	Explicit bool
}

type EnumDefinition struct {
	Token  lexer.Token
	Name   string
	Values []*EnumValue
	CommentedNode
}

func (ed *EnumDefinition) statementNode()         {}
func (ed *EnumDefinition) Pos() lexer.Position    { return posFromToken(ed.Token) }
func (ed *EnumDefinition) EndPos() lexer.Position { return posFromToken(ed.Token) }

// UnionType is a union of multiple Types, e.g. i8 | i16 | ... | u64
type UnionType struct {
	Token lexer.Token
	Types []Type
}

func (ut *UnionType) typeNode()           {}
func (ut *UnionType) Pos() lexer.Position { return posFromToken(ut.Token) }
func (ut *UnionType) EndPos() lexer.Position {
	if len(ut.Types) == 0 {
		return posFromToken(ut.Token)
	}
	return ut.Types[len(ut.Types)-1].EndPos()
}
func (ut *UnionType) String() string {
	parts := make([]string, len(ut.Types))
	for i, t := range ut.Types {
		parts[i] = t.String()
	}
	return strings.Join(parts, " | ")
}

// TypeAlias binds a name to a Type or UnionType.
// Syntax:  name type-expr        (alias, single concrete type)
//
//	name type1 | type2 | ...  (union of two or more types)
type TypeAlias struct {
	Token lexer.Token
	Name  string
	Type  Type       // set when RHS is a single type
	Union *UnionType // set when RHS is a union (mutually exclusive with Type)
	CommentedNode
}

func (ta *TypeAlias) statementNode()         {}
func (ta *TypeAlias) Pos() lexer.Position    { return posFromToken(ta.Token) }
func (ta *TypeAlias) EndPos() lexer.Position { return posFromToken(ta.Token) }

// IsUnion reports whether the alias binds to a union.
func (ta *TypeAlias) IsUnion() bool { return ta.Union != nil }

// TaggedEnumVariant — 標籤列舉變體（名稱 + 型別）
type TaggedEnumVariant struct {
	Token lexer.Token
	Name  string
	Type  Type
	Index int64
}

// TaggedEnumDefinition — 標籤列舉：option { val i64, nil bool, err str }
type TaggedEnumDefinition struct {
	Token    lexer.Token
	Name     string
	Variants []*TaggedEnumVariant
	CommentedNode
}

func (ted *TaggedEnumDefinition) statementNode()         {}
func (ted *TaggedEnumDefinition) Pos() lexer.Position    { return posFromToken(ted.Token) }
func (ted *TaggedEnumDefinition) EndPos() lexer.Position { return posFromToken(ted.Token) }

type InterfaceMethod struct {
	Token             lexer.Token
	Name              string       // method name (e.g. "gt") — dotted prefix lives in Receiver
	Parameters        []*Parameter // method parameter names and types
	IsVariadic        bool         // method has variadic parameter (..t)
	Results           []*Parameter // method result declarations: (res type)
	Receiver          string       // "" for normal methods, "t" for generic-receiver methods (e.g. t.gt)
	IsGenericReceiver bool         // true when Receiver is a type variable (e.g. the single letter "t")
}

type InterfaceDefinition struct {
	Token      lexer.Token
	Name       string
	Implements []string // 繼承的介面列表（空 = 無）
	Methods    []*InterfaceMethod
	CommentedNode
}

func (id *InterfaceDefinition) statementNode()         {}
func (id *InterfaceDefinition) Pos() lexer.Position    { return posFromToken(id.Token) }
func (id *InterfaceDefinition) EndPos() lexer.Position { return posFromToken(id.Token) }

type StructDefinition struct {
	Token         lexer.Token
	Name          string
	Implements    []string // 實現的介面列表（空 = 無）
	Fields        []*StructField
	GenericParams []string           // 泛型型別參數，來自 #{generic=[K,V]} 註解
	Annotations   []*AnnotationEntry // 來自前置 #{...} 註解的條目
	CommentedNode
}

func (sd *StructDefinition) statementNode()         {}
func (sd *StructDefinition) Pos() lexer.Position    { return posFromToken(sd.Token) }
func (sd *StructDefinition) EndPos() lexer.Position { return posFromToken(sd.Token) }

type StructLiteral struct {
	Token  lexer.Token
	Type   string
	Fields []*StructField
}

func (sl *StructLiteral) expressionNode()        {}
func (sl *StructLiteral) Pos() lexer.Position    { return posFromToken(sl.Token) }
func (sl *StructLiteral) EndPos() lexer.Position { return posFromToken(sl.Token) }
