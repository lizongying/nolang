package fmt

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestDebugZstdAST(t *testing.T) {
	src, _ := os.ReadFile("../std/archive/zstd.no")
	l := lexer.New(string(src))
	p := parser.New(l)
	p.SkipUnwrapLowering = true
	prog := p.ParseProgram()
	for _, st := range prog.Statements {
		fd, ok := st.(*parser.FunctionDefinition)
		if !ok || !strings.Contains(fd.Name, "decode-block") {
			continue
		}
		for i, bs := range fd.Body.Statements {
			doc := ""
			if d, ok := bs.(interface{ GetDoc() *parser.CommentGroup }); ok && d.GetDoc() != nil {
				doc = d.GetDoc().List[0].Text
			}
			cmt := ""
			if c, ok := bs.(interface{ GetComment() *parser.CommentGroup }); ok && c.GetComment() != nil {
				cmt = c.GetComment().List[0].Text
			}
			t.Logf("[%2d] %T doc=%q cmt=%q", i, bs, doc, cmt)
		}
	}
}

func TestDebugZstdFmtTrace(t *testing.T) {
	// trace what formatStatement does for the annotation in decode-block
	src, _ := os.ReadFile("../std/archive/zstd.no")
	l := lexer.New(string(src))
	p := parser.New(l)
	prog := p.ParseProgram()
	for _, st := range prog.Statements {
		fd, ok := st.(*parser.FunctionDefinition)
		if !ok || !strings.Contains(fd.Name, "decode-block") {
			continue
		}
		for i, bs := range fd.Body.Statements {
			if as, ok := bs.(*parser.AnnotationStatement); ok {
				doc := as.GetDoc()
				fmt.Printf("TRACE annot[%d] entries=%d doc=%v\n", i, len(as.Entries), doc != nil)
				if doc != nil {
					fmt.Printf("TRACE   doc text=%q\n", doc.List[0].Text)
				}
			}
		}
	}
}
