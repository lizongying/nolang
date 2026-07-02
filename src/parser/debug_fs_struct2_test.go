package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestDebugFsStructNo2(t *testing.T) {
	source := `main = () {
    tmp = '/tmp/nolang-fs-test.txt'
    payload = 'hello nolang file struct'
    remove(tmp)

    f-w ?file
    f-w = open(tmp, {
        perm: FilePerm.PERM_600,
        mode: FileMode.WRITE | FileMode.CREATE,
        excl: true
    })
    f-w: {
        err -> print('fail: open-write err')
        nil -> print('fail: open-write nil')
        ok -> print('ok: open write/create/excl')
    }

    f-e ?file
    f-e = open(tmp, {
        perm: FilePerm.PERM_600,
        mode: FileMode.WRITE | FileMode.CREATE,
        excl: true
    })
    f-e: {
        err -> print('fail: open-excl err')
        nil -> print('ok: excl blocks existing file')
        ok -> print('fail: excl did not block')
    }

    f-r ?file
    f-r = open(tmp, {
        perm: FilePerm.PERM_600,
        mode: FileMode.READ,
        excl: false
    })
    f-r: {
        err -> print('fail: open-read err')
        nil -> print('fail: open-read nil')
        ok -> {
            buf [256]byte
            n = it.read(buf, 256)
            if n == payload.len {
                print('ok: read length matches')
            } else {
                print('fail: read length mismatch')
            }
            it.close()
        }
    }

    remove(tmp)
}

main()
`
	l := lexer.New(source)
	p := New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, e := range p.Errors() {
			t.Logf("Parse error: %v", e)
		}
	}
	t.Logf("Program has %d statements", len(program.Statements))
	for i, stmt := range program.Statements {
		t.Logf("[%d] %T", i, stmt)
		dumpStmtForTest(t, stmt, 1)
	}
}
