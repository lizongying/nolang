package llvm

import (
	"fmt"
	"os"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// strCharAtIR 是 UTF-8 感知的码点索引运行时辅助函数 (@nolang.str_char_at)。
//
// 背景：nolang 的 str/txt 底层是 UTF-8 字节序列。取「第 i 个字符（码点）」必须
// 从缓冲区头部前向迭代，逐段跳过 UTF-8 序列，因此是 O(n) 操作。只有当编译器能
// 证明字符串全部是 ASCII（0-127）时，每个字节恰好等于一个码点，下标才能退化为
// 一次 GEP + load 的 O(1) 直接字节寻址。
//
// 本函数是那条 O(n) 路径：
//   - 先扫描，跳过前 idx 个码点，得到字节偏移 i
//   - 再在偏移 i 处解码一个 UTF-8 序列，返回码点值（i64）
//   - 越界时转发到 @nolang.bounds_check 统一报错退出
//
// 标记为 alwaysinline，使 opt -O3 能在调用点内联；当索引为常量时循环可被展开，
// 小字符串情形下往往能被完全优化掉。
const strCharAtIR = `define internal i64 @nolang.str_char_at(i8* %data, i64 %len, i64 %idx) alwaysinline {
entry:
  br label %scan
scan:
  %i = phi i64 [ 0, %entry ], [ %i.next, %adv ]
  %k = phi i64 [ 0, %entry ], [ %k.next, %adv ]
  %hit = icmp eq i64 %k, %idx
  br i1 %hit, label %decode, label %adv
adv:
  %ap = getelementptr i8, i8* %data, i64 %i
  %ab = load i8, i8* %ap
  %abu = zext i8 %ab to i64
  %is4 = icmp uge i64 %abu, 240
  %ge224 = icmp uge i64 %abu, 224
  %lt240 = icmp ult i64 %abu, 240
  %is3 = and i1 %ge224, %lt240
  %ge192 = icmp uge i64 %abu, 192
  %lt224 = icmp ult i64 %abu, 224
  %is2 = and i1 %ge192, %lt224
  %s0 = select i1 %is4, i64 4, i64 1
  %s1 = select i1 %is3, i64 3, i64 %s0
  %clen = select i1 %is2, i64 2, i64 %s1
  %i.next = add i64 %i, %clen
  %k.next = add i64 %k, 1
  br label %scan
decode:
  %oob = icmp sge i64 %i, %len
  br i1 %oob, label %oob.err, label %ok
oob.err:
  call void @nolang.bounds_check(i64 %i, i64 %len)
  unreachable
ok:
  %dp = getelementptr i8, i8* %data, i64 %i
  %b0 = load i8, i8* %dp
  %b0u = zext i8 %b0 to i64
  %c1 = icmp ult i64 %b0u, 128
  br i1 %c1, label %ret1, label %m2
ret1:
  ret i64 %b0u
m2:
  %c2 = icmp ult i64 %b0u, 224
  br i1 %c2, label %two, label %m3
two:
  %i1 = add i64 %i, 1
  %p1 = getelementptr i8, i8* %data, i64 %i1
  %b1 = load i8, i8* %p1
  %b1u = zext i8 %b1 to i64
  %t1 = and i64 %b0u, 31
  %t1s = shl i64 %t1, 6
  %t2 = and i64 %b1u, 63
  %r2 = or i64 %t1s, %t2
  ret i64 %r2
m3:
  %c3 = icmp ult i64 %b0u, 240
  br i1 %c3, label %three, label %four
three:
  %t3a = add i64 %i, 1
  %t3b = add i64 %i, 2
  %p3a = getelementptr i8, i8* %data, i64 %t3a
  %p3b = getelementptr i8, i8* %data, i64 %t3b
  %b3a = load i8, i8* %p3a
  %b3b = load i8, i8* %p3b
  %b3au = zext i8 %b3a to i64
  %b3bu = zext i8 %b3b to i64
  %u1 = and i64 %b0u, 15
  %u1s = shl i64 %u1, 12
  %u2 = and i64 %b3au, 63
  %u2s = shl i64 %u2, 6
  %u3 = and i64 %b3bu, 63
  %r3 = or i64 %u1s, %u2s
  %r3f = or i64 %r3, %u3
  ret i64 %r3f
four:
  %f1 = add i64 %i, 1
  %f2 = add i64 %i, 2
  %f3 = add i64 %i, 3
  %pf1 = getelementptr i8, i8* %data, i64 %f1
  %pf2 = getelementptr i8, i8* %data, i64 %f2
  %pf3 = getelementptr i8, i8* %data, i64 %f3
  %bf1 = load i8, i8* %pf1
  %bf2 = load i8, i8* %pf2
  %bf3 = load i8, i8* %pf3
  %bf1u = zext i8 %bf1 to i64
  %bf2u = zext i8 %bf2 to i64
  %bf3u = zext i8 %bf3 to i64
  %w1 = and i64 %b0u, 7
  %w1s = shl i64 %w1, 18
  %w2 = and i64 %bf1u, 63
  %w2s = shl i64 %w2, 12
  %w3 = and i64 %bf2u, 63
  %w3s = shl i64 %w3, 6
  %w4 = and i64 %bf3u, 63
  %r4 = or i64 %w1s, %w2s
  %r4b = or i64 %r4, %w3s
  %r4c = or i64 %r4b, %w4
  ret i64 %r4c
}

`

// isASCIIString 报告 s 的每个字节是否都落在 0-127。
// 这是「编译器证明字符串为纯 ASCII」的判据：一旦成立，字节数 == 码点数，
// 第 i 个字节就是第 i 个字符，下标可安全退化为 O(1) 直接定址。
func isASCIIString(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// collectAsciiVars 走訪整個程式，收集可被證明為純 ASCII 的字串變數名。
//
// 證明來源（任一成立即可）：
//  1. 顯式 `#{ascii}` 註解 — 程式員承諾該字串只含 ASCII，用於執行期輸入等
//     編譯器無法自動推斷的場合；
//  2. 字串字面量 — 字面量本身每個位元組都 < 0x80，編譯器可直接證明；
//  3. 由已證明的變數賦值傳播而來。
//
// 自動證明（2、3）是保守的：若某變數在程式中曾被「下標賦值」（s[i] = ...）
// 寫過，其位元組內容可能在執行期變成非 ASCII，自動證明即告失效；此時仍可用
// 顯式 `#{ascii}` 註解承諾（那是程式員的保證，不是推斷）。
//
// 未被證明的字串變數，其 s[i] 只能走 @nolang.str_char_at 的 O(n) 路徑。
func (g *Generator) collectAsciiVars(program *parser.Program) {
	if g.asciiVars == nil {
		g.asciiVars = make(map[string]bool)
	}
	if program == nil {
		return
	}
	// 先找出所有被下標賦值寫過的變數名，供 noteASCIILet 保守否決自動證明。
	g.mutatedVars = make(map[string]bool)
	g.scanMutatedVars(program.Statements)
	g.scanAsciiStmts(program.Statements, program.Sem)
}

// scanMutatedVars 收集所有出現於「下標賦值」左側的變數名（s[i] = ... / .[i] = ...）。
// 這些變數的位元組內容可能在執行期被寫成非 ASCII，故不能依賴字面量自動證明。
func (g *Generator) scanMutatedVars(stmts []parser.Statement) {
	for _, s := range stmts {
		switch st := s.(type) {
		case *parser.FunctionDefinition:
			if st.Body != nil {
				g.scanMutatedVars(st.Body.Statements)
			}
		case *parser.BlockStatement:
			g.scanMutatedVars(st.Statements)
		case *parser.ExpressionStatement:
			g.markMutatedInExpr(st.Expression)
		case *parser.LetStatement:
			g.markMutatedInExpr(st.Value)
		case *parser.ReturnStatement:
			g.markMutatedInExpr(st.ReturnValue)
		case *parser.ForStatement:
			if st.Init != nil {
				g.scanMutatedVars([]parser.Statement{st.Init})
			}
			if st.Body != nil {
				g.scanMutatedVars(st.Body.Statements)
			}
		case *parser.MultiAssignStatement:
			g.markMutatedInExpr(st.Value)
		case *parser.UnwrapAssignStatement:
			g.markMutatedInExpr(st.Value)
		}
	}
}

// markMutatedInExpr 遞迴尋找 AssignExpression，若其左側是 IndexExpression 則
// 記錄基底變數名為「可被位元組寫入」。
func (g *Generator) markMutatedInExpr(e parser.Expression) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *parser.AssignExpression:
		if idx, ok := x.Left.(*parser.IndexExpression); ok {
			g.markIndexBaseMutated(idx.Left)
		}
		g.markMutatedInExpr(x.Left)
		g.markMutatedInExpr(x.Value)
	case *parser.InfixExpression:
		g.markMutatedInExpr(x.Left)
		g.markMutatedInExpr(x.Right)
	case *parser.PrefixExpression:
		g.markMutatedInExpr(x.Right)
	case *parser.GroupedExpression:
		g.markMutatedInExpr(x.Expression)
	case *parser.IndexExpression:
		g.markMutatedInExpr(x.Left)
		g.markMutatedInExpr(x.Index)
	case *parser.CallExpression:
		g.markMutatedInExpr(x.Function)
		for _, a := range x.Arguments {
			g.markMutatedInExpr(a)
		}
	case *parser.IfExpression:
		if x.Consequence != nil {
			g.scanMutatedVars(x.Consequence.Statements)
		}
		if x.Alternative != nil {
			g.scanMutatedVars(x.Alternative.Statements)
		}
	}
}

// markIndexBaseMutated 記錄 index 表達式基底變數名（s[i] → s；.f[i] → 記為 self）。
func (g *Generator) markIndexBaseMutated(base parser.Expression) {
	switch b := base.(type) {
	case *parser.Identifier:
		g.mutatedVars[b.Value] = true
	case *parser.DotExpression:
		g.markIndexBaseMutated(b.Receiver)
	}
}

// hasASCIIAnnotation 报告节点是否带 #{ascii} 注解。
// 在 HIR codegen 模式下注解以 HIR id 为键（重建的 AST 节点不携带语义侧表），
// 因此统一走 g.annotationsFor，使两种模式行为一致。
func (g *Generator) hasASCIIAnnotation(n parser.Node) bool {
	if n == nil {
		return false
	}
	for _, e := range g.annotationsFor(n) {
		if e != nil && e.Key == "ascii" {
			return true
		}
	}
	return false
}

func (g *Generator) scanAsciiStmts(stmts []parser.Statement, sem *parser.SemanticContext) {
	for _, s := range stmts {
		switch st := s.(type) {
		case *parser.LetStatement:
			g.noteASCIILet(st, sem)
			g.scanAsciiExpr(st.Value, sem)
		case *parser.FunctionDefinition:
			if st.Body != nil {
				g.scanAsciiStmts(st.Body.Statements, sem)
			}
		case *parser.BlockStatement:
			g.scanAsciiStmts(st.Statements, sem)
		case *parser.ExpressionStatement:
			g.scanAsciiExpr(st.Expression, sem)
		case *parser.ReturnStatement:
			g.scanAsciiExpr(st.ReturnValue, sem)
		case *parser.ForStatement:
			// 迴圈內的 let（含解構綁定）也要納入證明，否則 std 內大量
			// `i <- [...): { x = ... }` 形式的綁定會被漏掉。
			if st.Init != nil {
				g.scanAsciiStmts([]parser.Statement{st.Init}, sem)
			}
			if st.Body != nil {
				g.scanAsciiStmts(st.Body.Statements, sem)
			}
		case *parser.MultiAssignStatement:
			g.scanAsciiExpr(st.Value, sem)
		case *parser.UnwrapAssignStatement:
			g.scanAsciiExpr(st.Value, sem)
		}
	}
}

// noteASCIILet 判定一個 let 綁定是否可證明為純 ASCII，是則記入 g.asciiVars。
func (g *Generator) noteASCIILet(st *parser.LetStatement, sem *parser.SemanticContext) {
	if st == nil || st.Name == nil {
		return
	}
	// 顯式 #{ascii} 註解是程式員的承諾，即使該變數後續被位元組寫入也照樣成立。
	proven := g.hasASCIIAnnotation(st)
	// 未註解時：若該變數曾作為下標賦值目標（s[i] = ...），其執行期內容可能
	// 已不是 ASCII，字面量/傳播的自動證明對它不成立——寧可退回 O(n) 也不能
	// 把碼點下標錯譯成位元組定址。
	if !proven && g.mutatedVars[st.Name.Value] {
		return
	}
	if !proven {
		switch v := st.Value.(type) {
		case *parser.StringLiteral:
			proven = isASCIIString(v.Value)
		case *parser.Identifier:
			proven = g.asciiVars[v.Value]
		case *parser.InfixExpression:
			// 字串拼接（-）：兩側都是已證明的 ASCII 時，結果仍是 ASCII。
			if v.Operator == "-" {
				l, lok := v.Left.(*parser.Identifier)
				r, rok := v.Right.(*parser.Identifier)
				if lok && rok {
					proven = g.asciiVars[l.Value] && g.asciiVars[r.Value]
				}
				if lr, ok := v.Left.(*parser.StringLiteral); ok {
					lok = isASCIIString(lr.Value)
				}
				if rr, ok := v.Right.(*parser.StringLiteral); ok {
					rok = isASCIIString(rr.Value)
				}
				if lok && rok {
					proven = true
				}
			}
		}
	}
	if os.Getenv("NOLANG_ASCII_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[ascii-let] name=%q proven=%v value=%T\n", st.Name.Value, proven, st.Value)
	}
	if proven {
		g.asciiVars[st.Name.Value] = true
	}
}

func (g *Generator) scanAsciiExpr(e parser.Expression, sem *parser.SemanticContext) {
	switch x := e.(type) {
	case *parser.IfExpression:
		if x.Consequence != nil {
			g.scanAsciiStmts(x.Consequence.Statements, sem)
		}
		if x.Alternative != nil {
			g.scanAsciiStmts(x.Alternative.Statements, sem)
		}
	case *parser.FunctionLiteral:
		if x.Body != nil {
			g.scanAsciiStmts(x.Body.Statements, sem)
		}
	case *parser.CallExpression:
		g.scanAsciiExpr(x.Function, sem)
		for _, a := range x.Arguments {
			g.scanAsciiExpr(a, sem)
		}
	}
}

// generateRawByteAt 实现 str.byte(i) / txt.byte(i) —— 原始位元組訪問逃生艙口。
//
// s[i] 的語義已改為「第 i 個字符（碼點）」（未證明 ASCII 時為 O(n) UTF-8 迭代），
// 但標準庫內部的 UTF-8 編解碼、memcmp、雜湊等實作物件必須按下标直接定址底層
// 位元組。此 accessor 顯式表達「我要的是第 i 個位元組」，永遠是 O(1)。
// 它在 codegen 層直接展開為 GEP+load，不進入 std 函式體。
func (g *Generator) generateRawByteAt(sb *strings.Builder, varName, llvmType string, idxExpr parser.Expression) string {
	idx := g.generateExprWithSB(sb, idxExpr)
	if llvmType == "%txt" {
		txtRef := llvmVarRef(varName)
		if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
			txtRef = llvmGlobalRef(varName)
		}
		dataGEP := g.tmpReg("byte.data.gep")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 0\n",
				g.indent(), dataGEP, txtRef))
		}
		dataPtr := g.tmpReg("byte.data")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = bitcast [255 x i8]* %s to i8*\n", g.indent(), dataPtr, dataGEP))
		}
		gep := g.tmpReg("byte.gep")
		val := g.tmpReg("byte.val")
		zext := g.tmpReg("byte.zext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), gep, dataPtr, idx))
			sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), val, gep))
			sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zext, val))
		}
		return zext
	}
	// %str-long
	strPtr := g.varAddr(varName)
	strLen := g.extractStrLen(sb, strPtr)
	g.emitBoundsCheck(sb, idx, strLen)
	dataPtr := g.extractStrDataPtr(sb, strPtr)
	gep := g.tmpReg("byte.gep")
	val := g.tmpReg("byte.val")
	zext := g.tmpReg("byte.zext")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), gep, dataPtr, idx))
		sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), val, gep))
		sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zext, val))
	}
	return zext
}

// emitStrCharAtCall 发出对 @nolang.str_char_at 的调用，返回存放码点的寄存器名。
// dataPtr 是底层字节缓冲区指针，lenReg 是缓冲区字节长度，idxReg 是码点下标。
func (g *Generator) emitStrCharAtCall(sb *strings.Builder, dataPtr, lenReg, idxReg string) string {
	res := g.tmpReg("str.charat")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.str_char_at(i8* %s, i64 %s, i64 %s)\n",
			g.indent(), res, dataPtr, lenReg, idxReg))
	}
	return res
}
