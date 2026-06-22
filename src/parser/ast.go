package parser

import (
	"fmt"

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
		}
	}
	return "[?]" + at.Elem.String()
}

type SliceType struct {
	Token lexer.Token // [
	Elem  Type
}

func (st *SliceType) typeNode()              {}
func (st *SliceType) Pos() lexer.Position    { return posFromToken(st.Token) }
func (st *SliceType) EndPos() lexer.Position { return st.Elem.EndPos() }
func (st *SliceType) String() string         { return "[]" + st.Elem.String() }

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
	case *NullableType:
		return "?" + typeString(n.Type)
	case *PointerType:
		return "ptr " + typeString(n.Type)
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
	Text string // content without // prefix
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

// a u8 = 8
type LetStatement struct {
	Token lexer.Token
	Name  *Identifier
	Type  Type
	Value Expression
	CommentedNode
}

func (ls *LetStatement) statementNode()      {}
func (ls *LetStatement) Pos() lexer.Position { return posFromToken(ls.Token) }
func (ls *LetStatement) EndPos() lexer.Position {
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
	Token      lexer.Token
	Expression Expression
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
	BetweenComments     []*CommentGroup // free-standing comment lines between statements
}

func (bs *BlockStatement) statementNode()         {}
func (bs *BlockStatement) Pos() lexer.Position    { return posFromToken(bs.Token) }
func (bs *BlockStatement) EndPos() lexer.Position { return bs.RBrace }

type Parameter struct {
	Token lexer.Token
	Name  string
	Type  Type
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
}

type FunctionDefinition struct {
	Token lexer.Token
	Name  string
	FuncSignature
	Body        *BlockStatement
	ColonSyntax bool // 是否為冒號語法 foo: (a int) { }
	CommentedNode
}

func (fd *FunctionDefinition) statementNode()         {}
func (fd *FunctionDefinition) Pos() lexer.Position    { return posFromToken(fd.Token) }
func (fd *FunctionDefinition) EndPos() lexer.Position { return fd.Body.EndPos() }

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
	return ce.Function.EndPos()
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

func (pt *PointerType) expressionNode()        {}
func (pt *PointerType) typeNode()              {}
func (pt *PointerType) Pos() lexer.Position    { return posFromToken(pt.Token) }
func (pt *PointerType) EndPos() lexer.Position { return pt.Type.EndPos() }
func (pt *PointerType) String() string         { return "ptr " + typeString(pt.Type) }

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

func (pe *PrefixExpression) expressionNode()        {}
func (pe *PrefixExpression) Pos() lexer.Position    { return posFromToken(pe.Token) }
func (pe *PrefixExpression) EndPos() lexer.Position { return pe.Right.EndPos() }

type InfixExpression struct {
	Token    lexer.Token
	Left     Expression
	Operator string
	Right    Expression
}

func (ie *InfixExpression) expressionNode()        {}
func (ie *InfixExpression) Pos() lexer.Position    { return posFromToken(ie.Token) }
func (ie *InfixExpression) EndPos() lexer.Position { return ie.Right.EndPos() }

// if
type IfExpression struct {
	Token       lexer.Token
	Condition   Expression
	Consequence *BlockStatement
	Alternative *BlockStatement
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

func (ce *ConditionalExpression) expressionNode()        {}
func (ce *ConditionalExpression) Pos() lexer.Position    { return posFromToken(ce.Token) }
func (ce *ConditionalExpression) EndPos() lexer.Position { return ce.Alternative.EndPos() }

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
	Token     lexer.Token
	Name      string
	Type      Type
	ArraySize int64 // >0 = 定長陣列 [N]type
	IsSlice   bool  // true = 切片 []type
	Value     Expression
}

type EnumValue struct {
	Token lexer.Token
	Name  string
	Value int64
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
	Token      lexer.Token
	Name       string
	Parameters []*Parameter // method parameter names and types
	IsVariadic bool         // method has variadic parameter (..t)
}

type InterfaceDefinition struct {
	Token   lexer.Token
	Name    string
	Methods []*InterfaceMethod
	CommentedNode
}

func (id *InterfaceDefinition) statementNode()         {}
func (id *InterfaceDefinition) Pos() lexer.Position    { return posFromToken(id.Token) }
func (id *InterfaceDefinition) EndPos() lexer.Position { return posFromToken(id.Token) }

type StructDefinition struct {
	Token      lexer.Token
	Name       string
	Implements []string // 實現的介面列表（空 = 無）
	Fields     []*StructField
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
