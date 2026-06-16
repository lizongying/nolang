package parser

import (
	"fmt"

	"github.com/lizongying/nolang/lexer"
)

// Comment represents a single comment line.
type Comment struct {
	Token lexer.Token
	Text  string
}

// CommentGroup represents a sequence of consecutive comment lines.
type CommentGroup struct {
	List []*Comment
}

type Node interface {
	TokenLiteral() string
}

type Statement interface {
	Node
	statementNode()
	Print()
}

type Expression interface {
	Node
	expressionNode()
}

type Program struct {
	Statements []Statement
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	}
	return ""
}

func (p *Program) Print() {
	for _, stmt := range p.Statements {
		stmt.Print()
	}
}

// use path.fn 或 use path.fn alias
// path 範例: std/math, github.com/utils/math, /utils/math
type UseStatement struct {
	Token    lexer.Token
	Path     string // 模組路徑（無副檔名）
	Function string // 函數名
	Alias    string // 可選別名（空 = 不使用別名）
	Doc      *CommentGroup
	Comment  *CommentGroup
}

func (us *UseStatement) statementNode()       {}
func (us *UseStatement) TokenLiteral() string { return us.Token.Literal }
func (us *UseStatement) Print() {
	fmt.Printf("UseStatement{path: %s, function: %s, alias: %s}\n", us.Path, us.Function, us.Alias)
}

// a u8 = 8
type LetStatement struct {
	Token     lexer.Token
	Name      *Identifier
	Type      *Identifier
	Value     Expression
	ArraySize int64  // [N] 陣列大小，0 = 非陣列
	IsSlice   bool   // [] 切片標記
	ElemType  string // 元素型別（陣列/切片用）
	IsOption  bool   // ?type 標記
	Doc       *CommentGroup
	Comment   *CommentGroup
}

func (ls *LetStatement) statementNode()       {}
func (ls *LetStatement) TokenLiteral() string { return ls.Token.Literal }
func (ls *LetStatement) Print() {
	fmt.Printf("LetStatement{name: %s, type: %s, value: %s}\n", ls.Name.TokenLiteral(), ls.Type.Value, ls.Value.TokenLiteral())
}

type Identifier struct {
	Token lexer.Token
	Value string
}

func (i *Identifier) expressionNode()      {}
func (i *Identifier) TokenLiteral() string { return i.Token.Literal }
func (i *Identifier) Print() {
	fmt.Printf("Identifier{%s, %s}\n", i.Token.Literal, i.Value)
}

type ReturnStatement struct {
	Token       lexer.Token
	ReturnValue Expression
	Doc         *CommentGroup
	Comment     *CommentGroup
}

func (rs *ReturnStatement) statementNode()       {}
func (rs *ReturnStatement) TokenLiteral() string { return rs.Token.Literal }
func (rs *ReturnStatement) Print() {
	fmt.Printf("ReturnStatement{%s, %s}\n", rs.Token.Literal, rs.ReturnValue)
}

type ExpressionStatement struct {
	Token      lexer.Token
	Expression Expression
	Doc        *CommentGroup
	Comment    *CommentGroup
}

func (es *ExpressionStatement) statementNode()       {}
func (es *ExpressionStatement) TokenLiteral() string { return es.Token.Literal }
func (es *ExpressionStatement) Print() {
	fmt.Printf("ExpressionStatement{%s, %s}\n", es.Token.Literal, es.Expression)
}

type BlockStatement struct {
	Token      lexer.Token
	Statements []Statement
	Doc        *CommentGroup
	Comment    *CommentGroup
}

func (bs *BlockStatement) statementNode()       {}
func (bs *BlockStatement) TokenLiteral() string { return bs.Token.Literal }
func (bs *BlockStatement) Print() {
	for _, stmt := range bs.Statements {
		stmt.Print()
	}
}

type Parameter struct {
	Token lexer.Token
	Name  string
	Type  string
}

func (p *Parameter) expressionNode()      {}
func (p *Parameter) TokenLiteral() string { return p.Token.Literal }
func (p *Parameter) Print() {
	fmt.Printf("Parameter{%s, %s, %s}\n", p.Token.Literal, p.Name, p.Type)
}

type FunctionDefinition struct {
	Token         lexer.Token
	Name          string
	GenericParams []string // 泛型參數：<N, M, ...>
	Parameters    []*Parameter
	Results       []*Parameter
	Body          *BlockStatement
	IsVariadic    bool // 是否有 ...any 可變參數
	Doc           *CommentGroup
	Comment       *CommentGroup
}

func (fd *FunctionDefinition) statementNode()       {}
func (fd *FunctionDefinition) TokenLiteral() string { return fd.Token.Literal }
func (fd *FunctionDefinition) Print() {
	for _, stmt := range fd.Body.Statements {
		stmt.Print()
	}
}

type FunctionLiteral struct {
	Token      lexer.Token
	Parameters []*Parameter
	Body       *BlockStatement
}

func (fl *FunctionLiteral) expressionNode()      {}
func (fl *FunctionLiteral) TokenLiteral() string { return fl.Token.Literal }
func (fl *FunctionLiteral) Print() {
	fmt.Printf("FunctionLiteral{%s, %v}\n", fl.Token.Literal, fl.Parameters)
}

type CallExpression struct {
	Token       lexer.Token
	Function    Expression
	GenericArgs []Expression // 泛型引數：<type, ...>
	Arguments   []Expression
}

func (ce *CallExpression) expressionNode()      {}
func (ce *CallExpression) TokenLiteral() string { return ce.Token.Literal }
func (ce *CallExpression) Print() {
	fmt.Printf("CallExpression{%s, %s, %s}\n", ce.Token.Literal, ce.Function, ce.Arguments)
}

type DotExpression struct {
	Token    lexer.Token
	Receiver Expression
	Property string
}

func (de *DotExpression) expressionNode()      {}
func (de *DotExpression) TokenLiteral() string { return de.Token.Literal }
func (de *DotExpression) Print() {
	fmt.Printf("DotExpression{%s, %s, %s}\n", de.Token.Literal, de.Receiver, de.Property)
}

type NullableType struct {
	Token lexer.Token
	Type  Expression
}

func (nt *NullableType) expressionNode()      {}
func (nt *NullableType) TokenLiteral() string { return nt.Token.Literal }
func (nt *NullableType) Print() {
	fmt.Printf("NullableType{%s, %s}\n", nt.Token.Literal, nt.Type)
}

type PointerType struct {
	Token lexer.Token
	Type  Expression
}

func (pt *PointerType) expressionNode()      {}
func (pt *PointerType) TokenLiteral() string { return pt.Token.Literal }
func (pt *PointerType) Print() {
	fmt.Printf("PointerType{%s, %s}\n", pt.Token.Literal, pt.Type)
}

// GroupedExpression represents a parenthesized expression: (expr)
type GroupedExpression struct {
	Token      lexer.Token
	Expression Expression
}

func (ge *GroupedExpression) expressionNode()      {}
func (ge *GroupedExpression) TokenLiteral() string { return ge.Token.Literal }
func (ge *GroupedExpression) Print() {
	fmt.Printf("GroupedExpression{(%s)}\n", ge.Expression)
}

type IntegerLiteral struct {
	Token lexer.Token
	Value int64
}

func (il *IntegerLiteral) expressionNode()      {}
func (il *IntegerLiteral) TokenLiteral() string { return il.Token.Literal }
func (il *IntegerLiteral) Print() {
	fmt.Printf("IntegerLiteral{%s, %v}\n", il.Token.Literal, il.Value)
}

type ByteLiteral struct {
	Token lexer.Token
	Value int64
}

func (bl *ByteLiteral) expressionNode()      {}
func (bl *ByteLiteral) TokenLiteral() string { return bl.Token.Literal }
func (bl *ByteLiteral) Print() {
	fmt.Printf("ByteLiteral{%s, %v}\n", bl.Token.Literal, bl.Value)
}

type FloatLiteral struct {
	Token lexer.Token
	Value float64
}

func (fl *FloatLiteral) expressionNode()      {}
func (fl *FloatLiteral) TokenLiteral() string { return fl.Token.Literal }
func (fl *FloatLiteral) Print() {
	fmt.Printf("FloatLiteral{%s, %v}\n", fl.Token.Literal, fl.Value)
}

type StringLiteral struct {
	Token lexer.Token
	Value string
}

func (sl *StringLiteral) expressionNode()      {}
func (sl *StringLiteral) TokenLiteral() string { return sl.Token.Literal }

type CharLiteral struct {
	Token lexer.Token
	Value string // single Unicode character
}

func (cl *CharLiteral) expressionNode()      {}
func (cl *CharLiteral) TokenLiteral() string { return cl.Token.Literal }
func (cl *CharLiteral) Print() {
	fmt.Printf("CharLiteral{%s, %s}\n", cl.Token.Literal, cl.Value)
}
func (sl *StringLiteral) Print() {
	fmt.Printf("StringLiteral{%s, %s}\n", sl.Token.Literal, sl.Value)
}

type BooleanLiteral struct {
	Token lexer.Token
	Value bool
}

func (bl *BooleanLiteral) expressionNode()      {}
func (bl *BooleanLiteral) TokenLiteral() string { return bl.Token.Literal }
func (bl *BooleanLiteral) Print() {
	fmt.Printf("BooleanLiteral{%s, %v}\n", bl.Token.Literal, bl.Value)
}

type NilLiteral struct {
	Token lexer.Token
}

func (nl *NilLiteral) expressionNode()      {}
func (nl *NilLiteral) TokenLiteral() string { return nl.Token.Literal }
func (nl *NilLiteral) Print() {
	fmt.Printf("NilLiteral{%s}\n", nl.Token.Literal)
}

type PrefixExpression struct {
	Token    lexer.Token
	Operator string
	Right    Expression
}

func (pe *PrefixExpression) expressionNode()      {}
func (pe *PrefixExpression) TokenLiteral() string { return pe.Token.Literal }
func (pe *PrefixExpression) Print() {
	fmt.Printf("PrefixExpression{%s, %s, %s}\n", pe.Token.Literal, pe.Operator, pe.Right)
}

type InfixExpression struct {
	Token    lexer.Token
	Left     Expression
	Operator string
	Right    Expression
}

func (ie *InfixExpression) expressionNode()      {}
func (ie *InfixExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *InfixExpression) Print() {
	fmt.Printf("InfixExpression{%s, %v, %s, %v}\n", ie.Token.Literal, ie.Left, ie.Operator, ie.Right)
}

// if
type IfExpression struct {
	Token       lexer.Token
	Condition   Expression
	Consequence *BlockStatement
	Alternative *BlockStatement
}

func (ie *IfExpression) expressionNode()      {}
func (ie *IfExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *IfExpression) Print() {
	fmt.Printf("IfExpression{%s, %v, %v, %v}\n", ie.Token.Literal, ie.Condition, ie.Consequence, ie.Alternative)
}

// for i in [a..b], (a..b], [a..b), (a..b)
type RangeExpression struct {
	Token    lexer.Token // [ or (
	Start    Expression  // a
	End      Expression  // b
	LeftInc  bool        // [ = true, ( = false
	RightInc bool        // ] = true, ) = false
}

func (re *RangeExpression) expressionNode()      {}
func (re *RangeExpression) TokenLiteral() string { return re.Token.Literal }
func (re *RangeExpression) Print() {
	fmt.Printf("RangeExpression{%v, %v, %v, %v}\n", re.LeftInc, re.Start, re.End, re.RightInc)
}

// nums[..], nums[1..], nums[..3], nums[1..3], nums[1..3), nums(1..3)
type SliceExpression struct {
	Token lexer.Token      // [ or (
	Left  Expression       // 被切割的数组/切片
	Range *RangeExpression // 范围（nil = [..] 全切片）
}

func (se *SliceExpression) expressionNode()      {}
func (se *SliceExpression) TokenLiteral() string { return se.Token.Literal }
func (se *SliceExpression) Print() {
	fmt.Printf("SliceExpression{%s, %v}\n", se.Token.Literal, se.Left)
	if se.Range != nil {
		se.Range.Print()
	}
}

// arr[i], vec[i], str[i], map[key]
type IndexExpression struct {
	Token lexer.Token // [
	Left  Expression  // 被索引的對象
	Index Expression  // 索引值
}

func (ie *IndexExpression) expressionNode()      {}
func (ie *IndexExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *IndexExpression) Print() {
	fmt.Printf("IndexExpression{%s, %v, %v}\n", ie.Token.Literal, ie.Left, ie.Index)
}

// u.name = value 欄位賦值
type AssignExpression struct {
	Token lexer.Token
	Left  Expression // DotExpression
	Value Expression
}

func (ae *AssignExpression) expressionNode()      {}
func (ae *AssignExpression) TokenLiteral() string { return ae.Token.Literal }
func (ae *AssignExpression) Print() {
	fmt.Printf("AssignExpression{%s, %s}\n", ae.Left, ae.Value)
}

// 三元运算符
type ConditionalExpression struct {
	Token       lexer.Token
	Condition   Expression
	Consequence Expression
	Alternative Expression
}

func (ce *ConditionalExpression) expressionNode()      {}
func (ce *ConditionalExpression) TokenLiteral() string { return ce.Token.Literal }
func (ce *ConditionalExpression) Print() {
	fmt.Printf("ConditionalExpression{%s, %s, %s, %s}\n", ce.Token.Literal, ce.Condition, ce.Consequence, ce.Alternative)
}

type ForStatement struct {
	Token         lexer.Token
	Label         string // 循環名稱（空 = 未命名）
	Init          Statement
	Condition     Expression
	Update        Statement
	Body          *BlockStatement
	Variable      string           // range for 的變數名
	Range         *RangeExpression // [a..b], (a..b) 等
	RangeStr      string           // 字串遍歷: for i in 'hello'
	RangeIdent    string           // 陣列/切片遍歷: for i in a
	RangeSliceLit *SliceLiteral    // 匿名切片遍歷: for i in [1, 2, 3]
	CountExpr     Expression       // ! { } 或 N * { } 語法
	Doc           *CommentGroup
	Comment       *CommentGroup
}

func (fs *ForStatement) statementNode()       {}
func (fs *ForStatement) TokenLiteral() string { return fs.Token.Literal }
func (fs *ForStatement) Print() {
	fmt.Printf("ForStatement{%s, label=%s, %v, %v}\n", fs.Token.Literal, fs.Label, fs.Condition, fs.Body)
}

type BreakStatement struct {
	Token   lexer.Token
	Label   string // 跳轉目標循環名稱（空 = 跳出當前循環）
	Doc     *CommentGroup
	Comment *CommentGroup
}

func (bs *BreakStatement) statementNode()       {}
func (bs *BreakStatement) TokenLiteral() string { return bs.Token.Literal }
func (bs *BreakStatement) Print() {
	fmt.Printf("BreakStatement{%s, %s}\n", bs.Token.Literal, bs.Label)
}

type ContinueStatement struct {
	Token   lexer.Token
	Label   string // 跳轉目標循環名稱（空 = 繼續當前循環）
	Doc     *CommentGroup
	Comment *CommentGroup
}

func (cs *ContinueStatement) statementNode()       {}
func (cs *ContinueStatement) TokenLiteral() string { return cs.Token.Literal }
func (cs *ContinueStatement) Print() {
	fmt.Printf("ContinueStatement{%s, %s}\n", cs.Token.Literal, cs.Label)
}

type ArrayLiteral struct {
	Token    lexer.Token
	Size     Expression
	Elements []Expression
}

func (al *ArrayLiteral) expressionNode()      {}
func (al *ArrayLiteral) TokenLiteral() string { return al.Token.Literal }
func (al *ArrayLiteral) Print() {
	fmt.Printf("ArrayLiteral{%s, %s, %s}\n", al.Token.Literal, al.Size, al.Elements)
}

type SliceLiteral struct {
	Token    lexer.Token
	Elements []Expression
}

func (sl *SliceLiteral) expressionNode()      {}
func (sl *SliceLiteral) TokenLiteral() string { return sl.Token.Literal }
func (sl *SliceLiteral) Print() {
	fmt.Printf("SliceLiteral{%s, %s}\n", sl.Token.Literal, sl.Elements)
}

type StructField struct {
	Token     lexer.Token
	Name      string
	Type      string // 元素型別（對陣列/切片為元素型別）
	ArraySize int64  // >0 = 定長陣列 [N]type
	IsSlice   bool   // true = 切片 []type
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
	Doc    *CommentGroup
	Comment *CommentGroup
}

func (ed *EnumDefinition) statementNode()       {}
func (ed *EnumDefinition) TokenLiteral() string { return ed.Token.Literal }
func (ed *EnumDefinition) Print() {
	fmt.Printf("EnumDefinition{%s}", ed.Name)
}

// TaggedEnumVariant — 標籤列舉變體（名稱 + 型別）
type TaggedEnumVariant struct {
	Token lexer.Token
	Name  string
	Type  string
	Index int64
}

// TaggedEnumDefinition — 標籤列舉：option { val i64, nil bool, err str }
type TaggedEnumDefinition struct {
	Token    lexer.Token
	Name     string
	Variants []*TaggedEnumVariant
	Doc      *CommentGroup
	Comment  *CommentGroup
}

func (ted *TaggedEnumDefinition) statementNode()       {}
func (ted *TaggedEnumDefinition) TokenLiteral() string { return ted.Token.Literal }
func (ted *TaggedEnumDefinition) Print() {
	fmt.Printf("TaggedEnumDefinition{%s, %v}", ted.Name, ted.Variants)
}

type InterfaceMethod struct {
	Token lexer.Token
	Name  string
}

type InterfaceDefinition struct {
	Token   lexer.Token
	Name    string
	Methods []*InterfaceMethod
	Doc     *CommentGroup
	Comment *CommentGroup
}

func (id *InterfaceDefinition) statementNode()       {}
func (id *InterfaceDefinition) TokenLiteral() string { return id.Token.Literal }
func (id *InterfaceDefinition) Print() {
	fmt.Printf("InterfaceDefinition{%s, %s}", id.Token.Literal, id.Name)
}

type StructDefinition struct {
	Token      lexer.Token
	Name       string
	Implements []string // 實現的介面列表（空 = 無）
	Fields     []*StructField
	Doc        *CommentGroup
	Comment    *CommentGroup
}

func (sd *StructDefinition) statementNode()       {}
func (sd *StructDefinition) TokenLiteral() string { return sd.Token.Literal }
func (sd *StructDefinition) Print() {
	fmt.Printf("StructDefinition{%s, %s, %v}\n", sd.Token.Literal, sd.Name, sd.Fields)
}

type StructLiteral struct {
	Token  lexer.Token
	Type   string
	Fields []*StructField
}

func (sl *StructLiteral) expressionNode()      {}
func (sl *StructLiteral) TokenLiteral() string { return sl.Token.Literal }
func (sl *StructLiteral) Print() {
	fmt.Printf("StructLiteral{%s, %s, %v}\n", sl.Token.Literal, sl.Type, sl.Fields)
}
