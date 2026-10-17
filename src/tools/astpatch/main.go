// astpatch: 基于 AST 的 Nolang 源码改写工具。
//
// 输入一个 .no 文件，按 JSON 补丁（patch）描述的修改操作对语法树做结构化改写，
// 再把改写后的 AST 重新格式化输出为 .no 文件。所有修改参数使用 JSON 表达，
// 以便描述任意复杂的嵌套结构（节点匹配模式、替换代码片段等）。
//
// 与正则/文本级工具（如 add_modprefix）不同，本工具先经 lexer+parser 得到真正的
// 语法树，只在 AST 节点上操作，最后交给项目 formatter（src/fmt）重新打印，因此
// 不会破坏语法、不会误改字符串/注释内容。
//
// 用法:
//
//	go run ./tools/astpatch -in <input.no> -patch <patch.json> [-out <output.no>]
//	go run ./tools/astpatch -in <input.no> -out <output.no> < patch.json   # patch 从 stdin 读取
//
// -out 省略时，结果写到 stdout。
//
// 补丁 JSON 顶层结构:
//
//	{ "ops": [ { "op": "...", ... }, ... ] }
//
// 支持的 op 见同目录 README.md。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	nofmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	in := flag.String("in", "", "输入 .no 文件路径（必填）")
	out := flag.String("out", "", "输出 .no 文件路径（省略则写到 stdout）")
	patchFile := flag.String("patch", "", "补丁 JSON 文件路径（省略则从 stdin 读取）")
	dump := flag.Bool("dump", false, "只解析并打印 AST，不改写（调试用）")
	flag.Parse()

	if *in == "" {
		fmt.Fprintln(os.Stderr, "错误: 必须提供 -in <input.no>")
		flag.Usage()
		os.Exit(2)
	}

	src, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取输入失败: %v\n", err)
		os.Exit(1)
	}

	program, srcStr, err := parseSource(*in, string(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析失败:\n%v\n", err)
		os.Exit(1)
	}

	if *dump {
		fmt.Println(parserDump(program))
		return
	}

	// 读取补丁
	var patchData []byte
	if *patchFile != "" {
		patchData, err = os.ReadFile(*patchFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取补丁失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(os.Stdin); err != nil {
			fmt.Fprintf(os.Stderr, "读取 stdin 补丁失败: %v\n", err)
			os.Exit(1)
		}
		patchData = buf.Bytes()
	}

	// 依次应用每个 op，再格式化输出
	result, _, err := applyPatch(program, srcStr, patchData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if *out == "" {
		os.Stdout.WriteString(result)
		return
	}
	if err := os.WriteFile(*out, []byte(result), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "写入输出失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "已写出: %s\n", *out)
}

// parseSourceShape 把源码解析为糖 AST（SkipLowering，S4 起的正式形態）。
// 舊的 surface 旗標（SkipUnwrapLowering + SkipSafeIndexLowering）路徑已隨 §4.3 刪除。
//
// 為何糖形態下不會「丟整棵 match」：parser/dump 有完整的 SurfaceMatch case
// （含逐臂的 matchArm，且它就是 ast_freeze 門禁的 oracle），formatter 也早已
// 支援糖形態（`no fmt` 本體就用它）。真正的盲區在 op 側——反射走訪器只看
// 「元素是否實作 Node」，而 SurfaceMatch.Arms 是 []MatchArm（值結構體、不實作
// Node）⇒ 已由 op.go 的 carriesNodes + walkSlice 的值結構體分支補上。
func parseSourceShape(name, code string) (*parser.Program, string, error) {
	l := lexer.New(code)
	p := parser.New(l)
	p.SkipLowering = true
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, "", fmt.Errorf("%s: %s", name, strings.Join(errs, "\n"))
	}
	return program, code, nil
}

// parseSource 是 parseSourceShape 的糖形態版（正式路徑）。
func parseSource(name, code string) (*parser.Program, string, error) {
	return parseSourceShape(name, code)
}

// parseCodeExprShape 把一段代码解析为单个表达式节点（用于替换表达式）。
func parseCodeExprShape(code string) (parser.Expression, error) {
	l := lexer.New(code)
	p := parser.New(l)
	p.SkipLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("替换代码解析失败: %s", strings.Join(errs, "; "))
	}
	if len(prog.Statements) != 1 {
		return nil, fmt.Errorf("替换代码必须是单个表达式，实得 %d 条语句", len(prog.Statements))
	}
	es, ok := prog.Statements[0].(*parser.ExpressionStatement)
	if !ok || es.Expression == nil {
		return nil, fmt.Errorf("替换代码不是表达式: %T", prog.Statements[0])
	}
	return es.Expression, nil
}

func parseCodeExpr(code string) (parser.Expression, error) {
	return parseCodeExprShape(code)
}

// parseCodeStmtsShape 把一段代码解析为若干语句节点（用于插入/替换语句，允许多行）。
func parseCodeStmtsShape(code string) ([]parser.Statement, error) {
	l := lexer.New(code)
	p := parser.New(l)
	p.SkipLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("语句代码解析失败: %s", strings.Join(errs, "; "))
	}
	if len(prog.Statements) == 0 {
		return nil, fmt.Errorf("语句代码为空")
	}
	return prog.Statements, nil
}

func parseCodeStmts(code string) ([]parser.Statement, error) {
	return parseCodeStmtsShape(code)
}

// applyPatch 依次应用补丁里的每个 op，再把改写后的 AST 交给 formatter 重新打印。
// 第二个回传值是累计的 op 计数（形狀門禁用它斷言兩形態reach 到同樣多的節點）。
// 抽出来是为了让测试能直接驱动（main 本身是 CLI，os.Exit 到处飞）。
func applyPatch(program *parser.Program, srcStr string, patchData []byte) (string, int, error) {
	spec, err := loadSpec(patchData)
	if err != nil {
		return "", 0, fmt.Errorf("补丁 JSON 无效: %v", err)
	}
	total := 0
	for i, raw := range spec.Ops {
		op, err := decodeOp(raw)
		if err != nil {
			return "", 0, fmt.Errorf("op[%d] 无效: %v", i, err)
		}
		n, err := applyOp(program, op)
		if err != nil {
			return "", 0, fmt.Errorf("op[%d] (%s) 应用失败: %v", i, op.Kind, err)
		}
		total += n
	}
	return nofmt.FormatProgram(program, srcStr), total, nil
}

// parserDump 输出调试用 AST 文本。
func parserDump(program *parser.Program) string {
	// 复用 parser/dump 的可视化输出。
	return dumpProgram(program)
}

// loadSpec 解析补丁 JSON 顶层。
type spec struct {
	Ops []json.RawMessage `json:"ops"`
}

func loadSpec(data []byte) (*spec, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return &spec{}, nil
	}
	var s spec
	if err := json.Unmarshal(trimmed, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
