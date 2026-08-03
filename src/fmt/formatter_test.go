package fmt

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// cd ./src/fmt && go test -v . -run TestFormatBasic/space_one
func TestFormatBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "simple identifier",
			input:    "x",
			expected: "x",
		},
		{
			name:     "function definition",
			input:    "add: (a int,b int){a+b}",
			expected: "add: (a int, b int) {\n    a + b\n}",
		},
		{
			name:     "operator spacing",
			input:    "a+b",
			expected: "a + b",
		},
		{
			name:     "multiple operators",
			input:    "a+b*c",
			expected: "a + b * c",
		},
		{
			name:     "comparison operators",
			input:    "a==b",
			expected: "a == b",
		},
		{
			name:     "not equals",
			input:    "a!=b",
			expected: "a != b",
		},
		{
			name:     "comma spacing in function call",
			input:    "add(1,2)",
			expected: "add(1, 2)",
		},
		{
			name:     "nested function calls",
			input:    "add(1,add(2,3))",
			expected: "add(1, add(2, 3))",
		},
		{
			name:     "variable declaration",
			input:    "x=5",
			expected: "x = 5",
		},
		{
			name:     "typed variable declaration",
			input:    "a i8=2",
			expected: "a i8 = 2",
		},
		{
			name:     "if statement",
			input:    "if x>0{x=1}else{x=0}",
			expected: "if x > 0 {\n    x = 1\n} else {\n    x = 0\n}",
		},
		{
			name:     "for loop",
			input:    "{x=x+1}(x<10)",
			expected: "{\n    x = x + 1\n} (x < 10)",
		},
		{
			name:     "infinite for loop with break",
			input:    "{break}()",
			expected: "{\n    break\n} ()",
		},
		{
			// 新式 { } () 無限循環（由舊式 !! { } 遷移）
			name:     "bang_loop",
			input:    "!!{break}",
			expected: "{\n    break\n} ()",
		},
		{
			// 新式 { } * N 計數循環
			name:     "counted_loop",
			input:    "{print(1)}*5",
			expected: "{\n    print(1)\n} * 5",
		},
		{
			// 新式 i <- (a..b]: { } 範圍循環
			name:     "range_for_with_colon",
			input:    "i<-[0..10):{print(i)}",
			expected: "i <- [0..10): {\n    print(i)\n}",
		},
		{
			// range-for 單條語句 inline body（不用大括號）
			name:     "range_for_inline_body",
			input:    "k<-[0..n):buf[k]=data[k]",
			expected: "k <- [0..n): buf[k] = data[k]",
		},
		{
			// range-for 單條語句 inline body（賦值表達式）
			name:     "range_for_inline_body_assign",
			input:    "i <- [0..10): x = x + i",
			expected: "i <- [0..10): x = x + i",
		},
		{
			// 按位取反前綴運算子 ~
			name:     "bitwise_not_prefix",
			input:    "x = ~y",
			expected: "x = ~y",
		},
		{
			// ~ 用於表達式內
			name:     "bitwise_not_in_expr",
			input:    "f = c ^ (b | ~d)",
			expected: "f = c ^ (b | ~d)",
		},
		{
			// 新式 { } (cond) 條件循環（由舊式 for cond { } 遷移）
			name:     "for_cond_keyword",
			input:    "for i<5 {i=i+1}",
			expected: "{\n    i = i + 1\n} (i < 5)",
		},
		{
			// 新式 { cond -> body } if/else（包在函數內）
			name:     "if_else_new_syntax",
			input:    "foo=(){{\nx>0->a=1\n->a=0\n}}",
			expected: "foo = () {\n    {\n        x > 0 -> a = 1\n        -> a = 0\n    }\n}",
		},
		{
			// 新式 if/else 多分支（包在函數內）
			name:     "if_else_with_multiple_arms",
			input:    "foo=(){{\nx==1->a=1\nx==2->a=2\n->a=0\n}}",
			expected: "foo = () {\n    {\n        x == 1 -> a = 1\n        x == 2 -> a = 2\n        -> a = 0\n    }\n}",
		},
		{
			// 新式 if/else 多行 body（多語句 body → 用大括號包裹）
			name:     "if_else_multiline_body",
			input:    "foo=(){{\nx==1->\na=1\nb=2\n->\nc=0\n}}",
			expected: "foo = () {\n    {\n        x == 1 -> {\n            a = 1\n            b = 2\n        }\n        -> c = 0\n    }\n}",
		},
		{
			// 新式 if/else 或條件
			name:     "if_else_or_condition",
			input:    "foo=(){{\nx==2||x==3->a=1\n->a=0\n}}",
			expected: "foo = () {\n    {\n        x == 2 || x == 3 -> a = 1\n        -> a = 0\n    }\n}",
		},
		{
			// regression: standalone if-then (cond -> return / cond -> x = val)
			// inside a bare match arm block body that is itself classified as
			// blockMatch. Leaked CTX_MATCH_ARM context used to prevent the
			// standalone if-then from being recognized, producing `; ;` instead
			// of `->`.
			name: "standalone_if_then_in_nested_bare_match_arm_body",
			input: `foo = () {
    bpos = foo.index(marker)
    bpos < 0 -> return
    bstart = bpos + marker.len

    {
        bstart < foo.len -> {
            foo[bstart] == 34 -> {
                bstart = bstart + 1
                bend = foo.index-from('"', bstart)
                bend < 0 -> return
                boundary = foo.slice(bstart, bend)
            }
            -> {
                bend = foo.index-from(';', bstart)
                bend < 0 -> bend = foo.len
                boundary = foo.slice(bstart, bend)
                boundary = boundary.trim()
            }
        }
        -> return
    }
}`,
			expected: `foo = () {
    bpos = foo.index(marker)
    bpos < 0 -> return
    bstart = bpos + marker.len

    {
        bstart < foo.len -> {
            foo[bstart] == 34 -> {
                bstart = bstart + 1
                bend = foo.index-from('"', bstart)
                bend < 0 -> return
                boundary = foo.slice(bstart, bend)
            }
            -> {
                bend = foo.index-from(';', bstart)
                bend < 0 -> bend = foo.len
                boundary = foo.slice(bstart, bend)
                boundary = boundary.trim()
            }
        }
        -> return
    }
}`,
		},
		{
			// regression: `match` is a keyword but is used as a variable
			// name in std/fs.no.  Before the fix, the parser skipped the
			// `match` token (returning nil), so the formatter produced
			// ` -> body` (extra leading space from nil condition) on the
			// first pass but `-> body` on the second — breaking
			// idempotency.  The fix treats MATCH as an identifier in
			// expression/statement position.
			name: "match_keyword_as_variable",
			input: `f = () {
    a != b -> *
    match = true
    j <- [0..n): {
        .x[j] != y[j] -> {
            match = false
            *
        }
    }
    match -> return
}`,
			expected: `f = () {
    a != b -> *
    match = true
    j <- [0..n): {
        .x[j] != y[j] -> {
            match = false
            *
        }
    }
    match -> return
}`,
		},
		{
			name:     "c-style for loop with comma",
			input:    "for i=0,i<5,i++{i=i}",
			expected: "for i = 0, i < 5, i ++  {\n    i = i\n}",
		},
		{
			// regression: comment-only block body (-> { // comment }) was
			// being stripped to just -> because TrailingComments from the
			// parsed block were not transferred to the arm body.
			name: "bare_match_comment_only_block_body",
			input: `foo = () {
    {
        c == 46 -> {
            // 小數點 - 允許
        }
        c == 101 || c == 69 -> {
            // 科學記號 e/E - 允許
        }
        -> {
            val = err('invalid float')
            return
        }
    }
}`,
			expected: `foo = () {
    {
        c == 46 -> {
            // 小數點 - 允許
        }
        c == 101 || c == 69 -> {
            // 科學記號 e/E - 允許
        }
        -> {
            val = err('invalid float')
            return
        }
    }
}`,
		},
		{
			// regression: cond -> // comment\n return — the -> was followed
			// by a comment and then a newline. parseExpression would skip the
			// NEWLINE and consume `return` as the body expression, causing the
			// return statement to be silently lost.
			// The comment is on the same line as `diff != 0 ->`, so it is
			// correctly attached as an inline comment on the empty block body.
			name: "standalone_if_then_arrow_comment_then_newline",
			input: `foo = () {
    diff != 0 -> // comment
    return

    x = 1
}`,
			expected: `foo = () {
    diff != 0 -> {
    }  // comment
    return

    x = 1
}`,
		},
		{
			// regression: bare match wildcard arm with a doc comment on the
			// body statement. The formatter used to output `-> // comment\nstmt`
			// (inline form) instead of `-> { // comment\n stmt }` (braces form).
			name: "bare_match_arm_body_with_doc_comment",
			input: `foo = () {
    {
        n == 16 -> t4 = 1
        -> {
            // 部分區塊：設置對應位元
            t0 = t0 | (1 << (n * 8))
        }
    }
}`,
			expected: `foo = () {
    {
        n == 16 -> t4 = 1
        -> {

            // 部分區塊：設置對應位元
            t0 = t0 | (1 << (n * 8))
        }
    }
}`,
		},
		{
			// regression: chained -> (guard chain) a -> b -> c -> d -> e
			// was being parsed as if-then-else (only 3 elements) with the
			// rest becoming a separate wildcard statement, causing the
			// formatter to insert `;` between the 3rd and 4th elements.
			name:     "standalone_if_then_guard_chain",
			input:    "foo = () {\n    yes = false\n    .a == 0 -> .b == 0 -> .c == 0 -> .d == 0 -> yes = true\n}",
			expected: "foo = () {\n    yes = false\n    .a == 0 -> .b == 0 -> .c == 0 -> .d == 0 -> yes = true\n}",
		},
		{
			// regression: chained -> with 3 elements (shorter chain)
			name:     "standalone_if_then_short_chain",
			input:    "foo = () {\n    .a >= 224 -> .a <= 239 -> yes = true\n}",
			expected: "foo = () {\n    .a >= 224 -> .a <= 239 -> yes = true\n}",
		},
		{
			// regression: inline comment on bare match arm body was being
			// detached and moved outside the block.
			name: "bare_match_arm_inline_comment",
			input: `foo = () {
    {
        c == 43 -> out[out.len] = 32  // + comment
        -> out[out.len] = c
    }
}`,
			expected: `foo = () {
    {
        c == 43 -> out[out.len] = 32  // + comment
        -> out[out.len] = c
    }
}`,
		},
		{
			// regression: comment on the { line of a bare match arm block
			// body was being moved inside as a doc comment, changing the
			// code structure.
			name: "bare_match_arm_block_opening_comment",
			input: `foo = () {
    {
        cond -> { // comment
            x = 1
        }
        -> y = 0
    }
}`,
			expected: `foo = () {
    {
        cond -> {  // comment
            x = 1
        }
        -> y = 0
    }
}`,
		},
		{
			name:     "return statement",
			input:    "foo: (a int){return}",
			expected: "foo: (a int) {\n    return\n}",
		},
		{
			name:     "boolean literals",
			input:    "true",
			expected: "true",
		},
		{
			name:     "nil literal",
			input:    "nil",
			expected: "nil",
		},
		{
			name:     "string literal",
			input:    `'hello'`,
			expected: "'hello'",
		},
		{
			name:     "float literal",
			input:    "3.14",
			expected: "3.14",
		},
		{
			name:     "complex expression",
			input:    "a+b*c-d/e",
			expected: "a + b * c-d / e",
		},
		{
			name:     "logical and",
			input:    "a&&b",
			expected: "a && b",
		},
		{
			name:     "logical or",
			input:    "a||b",
			expected: "a || b",
		},
		{
			name:     "not operator",
			input:    "!a",
			expected: "! a",
		},
		{
			name: "method_definition",
			input: strings.TrimSpace(`
str.len: () (n    i64)      {
    n = .len
}
			`),
			expected: strings.TrimSpace(`
str.len: () (n i64) {
    n = .len
}
			`),
		},

		{
			name: "space_many",
			input: strings.TrimSpace(`
a   [3]=   [1,   2, 3]




for i <- a {
    print(i)
}
			`),
			expected: strings.TrimSpace(`
a [3] = [1, 2, 3]

for i <- a: {
    print(i)
}
			`),
		},

		{
			name: "space_one",
			input: strings.TrimSpace(`
a   [3]=   [1,   2, 3]

for i <- a {
    print(i)
}
			`),
			expected: strings.TrimSpace(`
a [3] = [1, 2, 3]

for i <- a: {
    print(i)
}
			`),
		},

		{
			name: "func1",
			input: strings.TrimSpace(`
max=(a t,b t)(r t){
    r = a > b ? a : b
}
			`),
			expected: strings.TrimSpace(`
max = (a t, b t) (r t) {
    r = a > b ? a : b
}
			`),
		},

		{
			name: "func2",
			input: strings.TrimSpace(`
max     =     (a t,       b t)       (r t)    {
    r = a > b ? a : b
}
			`),
			expected: strings.TrimSpace(`
max = (a t, b t) (r t) {
    r = a > b ? a : b
}
			`),
		},

		{
			name: "func3",
			input: strings.TrimSpace(`
get-env = (key str) (val str) {  // LLVM: call i8* @getenv
}
set-env = (k str, v str) {  // LLVM: call i32 @setenv
}
			`),
			expected: strings.TrimSpace(`
get-env = (key str) (val str) {  // LLVM: call i8* @getenv
}

set-env = (k str, v str) {  // LLVM: call i32 @setenv
}
			`),
		},

		{
			name: "func4",
			input: strings.TrimSpace(`
get-env = (key str) (val str) {  // LLVM: call i8* @getenv
}
// 註釋
// 註釋
set-env = (k str, v str) {  // LLVM: call i32 @setenv
}
			`),
			expected: strings.TrimSpace(`
get-env = (key str) (val str) {  // LLVM: call i8* @getenv
}

// 註釋
// 註釋
set-env = (k str, v str) {  // LLVM: call i32 @setenv
}
			`),
		},

		{
			name: "func5",
			input: strings.TrimSpace(`
tar-for-each = (data []byte, idx i64, name str, sz i64, typ str, data-out []byte) {
    idx = 0
    n = len(data)
    off = 0
    {
        empty = 1
        i = 0
        {
            if data[off + i] != 0 {
                empty = 0
                break
            }
            i = i + 1
        } (i < 512)
        if empty == 1 {
            return
        }
        name = ''
        i = 0
        {
            c = data[off + i]
            if c == 0 {
                break
            }
            name[i] = c
            i = i + 1
        } (i < 100)
        sz = 0
        i = 0
        {
            c = data[off + 124 + i]
            if c >= 48 && c <= 57 {
                sz = sz * 8 + c - 48
            }
            i = i + 1
        } (i < 12)
        c = data[off + 156]
        if c == 48 || c == 0 {
            typ = 'file'
        } elif c == 53 {
            typ = 'dir'
        } else {
            typ = 'unknown'
        }
    } (off + 512 <= n)
    if sz > 0 {
        i = 0
        {
            data-out[i] = data[off + 512 + i]
            i = i + 1
        } (i < sz)
    }
    blocks = sz + 511 / 512
    if blocks < 0 {
        blocks = 0
    }
    off = off + 512 + blocks * 512
    idx = idx + 1
}
			`),
			expected: strings.TrimSpace(`
tar-for-each = (data []byte, idx i64, name str, sz i64, typ str, data-out []byte) {
    idx = 0
    n = len(data)
    off = 0
    {
        empty = 1
        i = 0
        {
            if data[off + i] != 0 {
                empty = 0
                break
            }
            i = i + 1
        } (i < 512)
        if empty == 1 {
            return
        }
        name = ''
        i = 0
        {
            c = data[off + i]
            if c == 0 {
                break
            }
            name[i] = c
            i = i + 1
        } (i < 100)
        sz = 0
        i = 0
        {
            c = data[off + 124 + i]
            if c >= 48 && c <= 57 {
                sz = sz * 8 + c - 48
            }
            i = i + 1
        } (i < 12)
        c = data[off + 156]
        if c == 48 || c == 0 {
            typ = 'file'
        } elif c == 53 {
            typ = 'dir'
        } else {
            typ = 'unknown'
        }
    } (off + 512 <= n)
    if sz > 0 {
        i = 0
        {
            data-out[i] = data[off + 512 + i]
            i = i + 1
        } (i < sz)
    }
    blocks = sz + 511 / 512
    if blocks < 0 {
        blocks = 0
    }
    off = off + 512 + blocks * 512
    idx = idx + 1
}
			`),
		},

		{
			name: "func6",
			input: strings.TrimSpace(`// ─── 迭代器 ───────────────────────────────────

// tar-for-each: 遍歷所有條目
// 每次回呼傳入 (idx, name, sz, typ, data)
// 返回 0 繼續，非 0 停止
tar-for-each = (data []byte, idx i64, name str, sz i64, typ str, data-out []byte) {
    idx = 0
    n = len(data)
    off = 0
    {

        // 檢查結束
        empty = 1
        i = 0
        {
            if data[off + i] != 0 {
                empty = 0
                break
            }
            i = i + 1
        } (i < 512)
        if empty == 1 {
            return
        }

        // 讀取名稱
        name = ''
        i = 0
        {
            c = data[off + i]
            if c == 0 {
                break
            }
            name[i] = c
            i = i + 1
        } (i < 100)

        // 大小
        sz = 0
        i = 0
        {
            c = data[off + 124 + i]
            if c >= 48 && c <= 57 {
                sz = sz * 8 + (c - 48)
            }
            i = i + 1
        } (i < 12)

        // 類型
        c = data[off + 156]
        if c == 48 || c == 0 {
            typ = 'file'
        } elif c == 53 {
            typ = 'dir'
        } else {
            typ = 'unknown'
        }

        // 資料
        if sz > 0 {
            i = 0
            {
                data-out[i] = data[off + 512 + i]
                i = i + 1
            } (i < sz)
        }

        // 前進到下個條目
        blocks = (sz + 511) / 512
        if blocks < 0 {
            blocks = 0
        }
        off = off + 512 + blocks * 512
        idx = idx + 1
    } (off + 512 <= n)
}`),
			expected: strings.TrimSpace(`// ─── 迭代器 ───────────────────────────────────

// tar-for-each: 遍歷所有條目
// 每次回呼傳入 (idx, name, sz, typ, data)
// 返回 0 繼續，非 0 停止
tar-for-each = (data []byte, idx i64, name str, sz i64, typ str, data-out []byte) {
    idx = 0
    n = len(data)
    off = 0
    {

        // 檢查結束
        empty = 1
        i = 0
        {
            if data[off + i] != 0 {
                empty = 0
                break
            }
            i = i + 1
        } (i < 512)
        if empty == 1 {
            return
        }

        // 讀取名稱
        name = ''
        i = 0
        {
            c = data[off + i]
            if c == 0 {
                break
            }
            name[i] = c
            i = i + 1
        } (i < 100)

        // 大小
        sz = 0
        i = 0
        {
            c = data[off + 124 + i]
            if c >= 48 && c <= 57 {
                sz = sz * 8 + (c - 48)
            }
            i = i + 1
        } (i < 12)

        // 類型
        c = data[off + 156]
        if c == 48 || c == 0 {
            typ = 'file'
        } elif c == 53 {
            typ = 'dir'
        } else {
            typ = 'unknown'
        }

        // 資料
        if sz > 0 {
            i = 0
            {
                data-out[i] = data[off + 512 + i]
                i = i + 1
            } (i < sz)
        }

        // 前進到下個條目
        blocks = (sz + 511) / 512
        if blocks < 0 {
            blocks = 0
        }
        off = off + 512 + blocks * 512
        idx = idx + 1
    } (off + 512 <= n)
}`),
		},

		{
			name: "func7",
			input: strings.TrimSpace(`// aes-key-expand: 將 16-byte 金鑰展開為 176-byte 輪金鑰
// ek: 輸出輪金鑰字串（176 位元組）
aes-key-expand = (key str, ek str) {

    // 複製原始金鑰（前 16 位元組）
    i = 0
    {
        ek[i] = key[i]
        i = i + 1
    } (i < 16)

    // 產生 w[4..43]（共 44 個 32-bit 字 = 176 位元組）
    i = 4
    {

        // 讀取前一個字
        off = (i - 1) * 4
        w = (ek[off] << 24) | (ek[off + 1] << 16) | (ek[off + 2] << 8) | ek[off + 3]
        if i % 4 == 0 {
            rot-word(w, rw)
            sub-word(rw, sw)
            rcon-val(i / 4, rc)
            w = (ek[(i-4) * 4] << 24) | (ek[(i-4) * 4 + 1] << 16) | (ek[(i-4) * 4 + 2] << 8) | ek[(i-4) * 4 + 3]
            w = (w ^ sw ^ (rc << 24)) & 4294967295
        } else {
            w-prev4 = (ek[(i-4) * 4] << 24) | (ek[(i-4) * 4 + 1] << 16) | (ek[(i-4) * 4 + 2] << 8) | ek[(i-4) * 4 + 3]
            w = (w-prev4 ^ w) & 4294967295
        }
    } (i < 44)
    ek[i * 4] = (w >> 24) & 255
    ek[i * 4 + 1] = (w >> 16) & 255
    ek[i * 4 + 2] = (w >> 8) & 255
    ek[i * 4 + 3] = w & 255
    i = i + 1
}`),
			expected: strings.TrimSpace(`// aes-key-expand: 將 16-byte 金鑰展開為 176-byte 輪金鑰
// ek: 輸出輪金鑰字串（176 位元組）
aes-key-expand = (key str, ek str) {

    // 複製原始金鑰（前 16 位元組）
    i = 0
    {
        ek[i] = key[i]
        i = i + 1
    } (i < 16)

    // 產生 w[4..43]（共 44 個 32-bit 字 = 176 位元組）
    i = 4
    {

        // 讀取前一個字
        off = (i - 1) * 4
        w = (ek[off] << 24) | (ek[off + 1] << 16) | (ek[off + 2] << 8) | ek[off + 3]
        if i % 4 == 0 {
            rot-word(w, rw)
            sub-word(rw, sw)
            rcon-val(i / 4, rc)
            w = (ek[(i-4) * 4] << 24) | (ek[(i-4) * 4 + 1] << 16) | (ek[(i-4) * 4 + 2] << 8) | ek[(i-4) * 4 + 3]
            w = (w ^ sw ^ (rc << 24)) & 4294967295
        } else {
            w-prev4 = (ek[(i-4) * 4] << 24) | (ek[(i-4) * 4 + 1] << 16) | (ek[(i-4) * 4 + 2] << 8) | ek[(i-4) * 4 + 3]
            w = (w-prev4 ^ w) & 4294967295
        }
    } (i < 44)
    ek[i * 4] = (w >> 24) & 255
    ek[i * 4 + 1] = (w >> 16) & 255
    ek[i * 4 + 2] = (w >> 8) & 255
    ek[i * 4 + 3] = w & 255
    i = i + 1
}`),
		},

		{
			name: "comment1",
			input: strings.TrimSpace(`
// rsa — RSA 加解密（多精度整數模冪）
//
// 使用多精度整數（base 2^32，little-endian）進行 RSA 模冪運算：
//   result = base^exp mod modulus
//
// 不包含金鑰生成；呼叫者需自行提供 n、e、d。
// 支援 1024~4096-bit 金鑰（32~128 個 32-bit limbs）。
//
// 用法：
//   // base, exp, mod 為 []i64 切片
//   // result 為輸出切片（長度 ≥ mod 的長度）
//   rsa-modpow(base, base-n, exp, exp-n, mod, mod-n, result, result-n)

// ─── 大數比較 ─────────────────────────────────────

// bn-cmp: 比較兩個大數 a 和 b
// 返回 cmp: 1 = a > b, 0 = a == b, -1 = a < b
bn-cmp = (a []i64, an i64, b []i64, bn i64, cmp i64) {
    if an > bn {
        if a[i] > b[i] {
            cmp = 1
            return
        }
        if a[i] < b[i] {
            cmp = -1
            return
        }
        i = i - 1
    }
    cmp = 0
}
			`),
			expected: strings.TrimSpace(`
// rsa — RSA 加解密（多精度整數模冪）
//
// 使用多精度整數（base 2^32，little-endian）進行 RSA 模冪運算：
//   result = base^exp mod modulus
//
// 不包含金鑰生成；呼叫者需自行提供 n、e、d。
// 支援 1024~4096-bit 金鑰（32~128 個 32-bit limbs）。
//
// 用法：
//   // base, exp, mod 為 []i64 切片
//   // result 為輸出切片（長度 ≥ mod 的長度）
//   rsa-modpow(base, base-n, exp, exp-n, mod, mod-n, result, result-n)

// ─── 大數比較 ─────────────────────────────────────

// bn-cmp: 比較兩個大數 a 和 b
// 返回 cmp: 1 = a > b, 0 = a == b, -1 = a < b
bn-cmp = (a []i64, an i64, b []i64, bn i64, cmp i64) {
    if an > bn {
        if a[i] > b[i] {
            cmp = 1
            return
        }
        if a[i] < b[i] {
            cmp = -1
            return
        }
        i = i - 1
    }
    cmp = 0
}
			`),
		},

		{
			name: "comment2",
			input: strings.TrimSpace(`

// aes-128-dec: 解密一個 16-byte 區塊
// in: 輸入密文（16 位元組）
// n: 固定 16
// key: 16-byte 金鑰
// out: 輸出明文（16 位元組）
aes-128-dec= (in str, n i64, key str, out str) {
    // 展開金鑰
    ek = '(16+160 bytes)'
    aes-key-expand(key, ek)

    // 複製輸入到狀態
    i = 0
    {
        out[i] = in[i]
        i = i + 1
    } (i < 16)

    // 初始 AddRoundKey（輪 10）
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    } (round > 0)

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}
			`),
			expected: strings.TrimSpace(`// aes-128-dec: 解密一個 16-byte 區塊
// in: 輸入密文（16 位元組）
// n: 固定 16
// key: 16-byte 金鑰
// out: 輸出明文（16 位元組）
aes-128-dec = (in str, n i64, key str, out str) {

    // 展開金鑰
    ek = '(16+160 bytes)'
    aes-key-expand(key, ek)

    // 複製輸入到狀態
    i = 0
    {
        out[i] = in[i]
        i = i + 1
    } (i < 16)

    // 初始 AddRoundKey（輪 10）
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    } (round > 0)

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}`),
		},

		{
			name: "comment3",
			input: strings.TrimSpace(`// ─── 單區塊加密/解密 ──────────────────────────────

// aes-128-enc: 加密一個 16-byte 區塊
// in: 輸入明文（16 位元組）
// n: 固定 16
// key: 16-byte 金鑰
// out: 輸出密文（16 位元組）
aes-128-enc = (in str, n i64, key str, out str) {

    // 展開金鑰
    ek = '(16+160 bytes)'
    aes-key-expand(key, ek)

    // 複製輸入到狀態
    i = 0
    {
        out[i] = in[i]
        i = i + 1
    } (i < 16)

    // 初始 AddRoundKey（輪 0）
    // 輪金鑰 0：ek[0..15]
    add-round-key(out, ek)

    // 第 1-9 輪
    round = 1
    {
        sub-bytes(out, 16)
        shift-rows(out)
        mix-columns(out)

        // 輪金鑰 round：ek[round*16..round*16+15]
        rk-off = round * 16
        add-round-key(out, ek + rk-off)  // 需要 ek 子字串
        round = round + 1
    } (round < 10)

    // 第 10 輪（無 MixColumns）
    sub-bytes(out, 16)
    shift-rows(out)
    add-round-key(out, ek + 160)
}`),
			expected: strings.TrimSpace(`// ─── 單區塊加密/解密 ──────────────────────────────

// aes-128-enc: 加密一個 16-byte 區塊
// in: 輸入明文（16 位元組）
// n: 固定 16
// key: 16-byte 金鑰
// out: 輸出密文（16 位元組）
aes-128-enc = (in str, n i64, key str, out str) {

    // 展開金鑰
    ek = '(16+160 bytes)'
    aes-key-expand(key, ek)

    // 複製輸入到狀態
    i = 0
    {
        out[i] = in[i]
        i = i + 1
    } (i < 16)

    // 初始 AddRoundKey（輪 0）
    // 輪金鑰 0：ek[0..15]
    add-round-key(out, ek)

    // 第 1-9 輪
    round = 1
    {
        sub-bytes(out, 16)
        shift-rows(out)
        mix-columns(out)

        // 輪金鑰 round：ek[round*16..round*16+15]
        rk-off = round * 16
        add-round-key(out, ek + rk-off)  // 需要 ek 子字串
        round = round + 1
    } (round < 10)

    // 第 10 輪（無 MixColumns）
    sub-bytes(out, 16)
    shift-rows(out)
    add-round-key(out, ek + 160)
}`),
		},

		{
			name: "comment4",
			input: strings.TrimSpace(`
// sha1 — SHA-1 安全哈希算法（160-bit）
//
// 提供兩層公開 API 與一層低階 API：
//   sha1(data []byte) (hash [20]byte) — 完整雜湊，含填充與多區塊處理
//   sha1-hex(data []byte) (hex str) — 完整雜湊，返回 40 字元 hex 字串
//   sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — 處理單一 512-bit 區塊（低階）
//
// 用法：
//   h0 = 1732584193
//   h1 = 4023233417
//   h2 = 2562383102
//   h3 = 271733878
//   h4 = 3285377520
//   sha1-block(block, h0, h1, h2, h3, h4)

// sha1-block: 處理一個 512-bit 區塊
// s []u32: 16 個 32-bit 字 (big-endian)
// h0 u32, h1 u32, h2 u32, h3 u32, h4 u32: 輸入/輸出 160-bit 哈希狀態
sha1-block = (s str, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) {
    MASK = 4294967295

    // 初始狀態
    a = h0
    b = h1
    c = h2
    d = h3
    e = h4

    // ---- 第 0-19 輪 (K = 0x5A827999 = 1518500249) ----
    // f = (b & c) | (~b & d)

    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK
    temp = (temp + f + e + 1518500249 + s[0])  & MASK
    e = d
    d = c
    c = ((b << 30) | (b >> 2)) & MASK
    b = a
    a = temp



    // 第 16-19 輪 — 擴展訊息，rotl(w_{t-3} ^ w_{t-8} ^ w_{t-14} ^ w_{t-16}, 1)

    // w16 = rotl(w13 ^ w8 ^ w2 ^ w0, 1)
    w = s[13] ^ s[8] ^ s[2] ^ s[0]
    w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK
    temp = (temp + f + e + 1518500249 + w) & MASK
    e = d
    d = c
    c = ((b << 30) | (b >> 2)) & MASK
    b = a
    a = temp

    // w17 = rotl(w14 ^ w9 ^ w3 ^ w1, 1)
    w = s[14] ^ s[9] ^ s[3] ^ s[1]
    w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK
    temp = (temp + f + e + 1518500249 + w) & MASK
    e = d
    d = c
    c = ((b << 30) | (b >> 2)) & MASK
    b = a
    a = temp

    // w18 = rotl(w15 ^ w10 ^ w4 ^ w2, 1)
    w = s[15] ^ s[10] ^ s[4] ^ s[2]
    w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK
    temp = (temp + f + e + 1518500249 + w) & MASK
    e = d
    d = c
    c = ((b << 30) | (b >> 2)) & MASK
    b = a
    a = temp



    // 累加回初始哈希值
    h0 = (h0 + a) & MASK
    h1 = (h1 + b) & MASK
    h2 = (h2 + c) & MASK
    h3 = (h3 + d) & MASK
    h4 = (h4 + e) & MASK
}

			`),
			expected: strings.TrimSpace(`// sha1 — SHA-1 安全哈希算法（160-bit）
//
// 提供兩層公開 API 與一層低階 API：
//   sha1(data []byte) (hash [20]byte) — 完整雜湊，含填充與多區塊處理
//   sha1-hex(data []byte) (hex str) — 完整雜湊，返回 40 字元 hex 字串
//   sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — 處理單一 512-bit 區塊（低階）
//
// 用法：
//   h0 = 1732584193
//   h1 = 4023233417
//   h2 = 2562383102
//   h3 = 271733878
//   h4 = 3285377520
//   sha1-block(block, h0, h1, h2, h3, h4)

// sha1-block: 處理一個 512-bit 區塊
// s []u32: 16 個 32-bit 字 (big-endian)
// h0 u32, h1 u32, h2 u32, h3 u32, h4 u32: 輸入/輸出 160-bit 哈希狀態
sha1-block = (s str, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) {
    MASK = 4294967295

    // 初始狀態
    a = h0
    b = h1
    c = h2
    d = h3
    e = h4

    // ---- 第 0-19 輪 (K = 0x5A827999 = 1518500249) ----
    // f = (b & c) | (~b & d)

    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK
    temp = (temp + f + e + 1518500249 + s[0]) & MASK
    e = d
    d = c
    c = ((b << 30) | (b >> 2)) & MASK
    b = a
    a = temp

    // 第 16-19 輪 — 擴展訊息，rotl(w_{t-3} ^ w_{t-8} ^ w_{t-14} ^ w_{t-16}, 1)

    // w16 = rotl(w13 ^ w8 ^ w2 ^ w0, 1)
    w = s[13] ^ s[8] ^ s[2] ^ s[0]
    w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK
    temp = (temp + f + e + 1518500249 + w) & MASK
    e = d
    d = c
    c = ((b << 30) | (b >> 2)) & MASK
    b = a
    a = temp

    // w17 = rotl(w14 ^ w9 ^ w3 ^ w1, 1)
    w = s[14] ^ s[9] ^ s[3] ^ s[1]
    w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK
    temp = (temp + f + e + 1518500249 + w) & MASK
    e = d
    d = c
    c = ((b << 30) | (b >> 2)) & MASK
    b = a
    a = temp

    // w18 = rotl(w15 ^ w10 ^ w4 ^ w2, 1)
    w = s[15] ^ s[10] ^ s[4] ^ s[2]
    w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK
    temp = (temp + f + e + 1518500249 + w) & MASK
    e = d
    d = c
    c = ((b << 30) | (b >> 2)) & MASK
    b = a
    a = temp

    // 累加回初始哈希值
    h0 = (h0 + a) & MASK
    h1 = (h1 + b) & MASK
    h2 = (h2 + c) & MASK
    h3 = (h3 + d) & MASK
    h4 = (h4 + e) & MASK
}`),
		},

		{
			name: "comment5",
			input: strings.TrimSpace(`
// sha512 — SHA-512 安全哈希算法（512-bit）
//
// 提供兩層公開 API 與一層低階 API：
//   sha512(data []byte) (hash [64]byte) — 完整雜湊，含填充與多區塊處理
//   sha512-hex(data []byte) (hex str) — 完整雜湊，返回 128 字元 hex 字串
//   sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64) — 處理單一 1024-bit 區塊（低階）
//
// 用法：
//   h0 = 7640891576956012808
//   h1 = 13503953896175478587
//   h2 = 4354685564936845355
//   h3 = 11912009170470909681
//   h4 = 5840696475078001361
//   h5 = 11170449401992604703
//   h6 = 2270897969802886507
//   h7 = 6620516959819538809
//   sha512-block(block, h0, h1, h2, h3, h4, h5, h6, h7)

// sha512-block: 處理一個 1024-bit 區塊
// s []u64: 16 個 64-bit 字 (big-endian)
// h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64: 輸入/輸出 512-bit 哈希狀態
sha512-block=(s str, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64) {
    // 64-bit 全 1 遮罩（用於位元 NOT）
    MASK64 = -1

    // 初始狀態
    a = h0
    b = h1
    c = h2
    d = h3
    e = h4
    f = h5
    g = h6
    h = h7

    // 第 0 輪 (K0 = 0x428A2F98D728AE22)
    S1 = ((e >> 14) | (e << 50))
    S1 = S1 ^ ((e >> 18) | (e << 46)) ^ ((e >> 41) | (e << 23))
    Ch = (e & f) ^ ((MASK64 ^ e) & g)
    S0 = ((a >> 28) | (a << 36))
    S0 = S0 ^ ((a >> 34) | (a << 30)) ^ ((a >> 39) | (a << 25))
    Maj = (a & b) ^ (a & c) ^ (b & c)
    T1 = h + S1 + Ch + 4794697086780616226 + s[0]
    T2 = S0 + Maj
    h = g
    g = f
    f = e
    e = d + T1
    d = c
    c = b
    b = a
    a = T1 + T2
    // 第 1 輪 (K1 = 0x7137449123EF65CD)
    S1 = ((e >> 14) | (e << 50))
    S1 = S1 ^ ((e >> 18) | (e << 46)) ^ ((e >> 41) | (e << 23))
    Ch = (e & f) ^ ((MASK64 ^ e) & g)
    S0 = ((a >> 28) | (a << 36))
    S0 = S0 ^ ((a >> 34) | (a << 30)) ^ ((a >> 39) | (a << 25))
    Maj = (a & b) ^ (a & c) ^ (b & c)
    T1 = h + S1 + Ch + 8158064640168781261 + s[1]
    T2 = S0 + Maj
    h = g
    g = f
    f = e
    e = d + T1
    d = c
    c = b
    b = a
    a = T1 + T2

}
			`),
			expected: strings.TrimSpace(`// sha512 — SHA-512 安全哈希算法（512-bit）
//
// 提供兩層公開 API 與一層低階 API：
//   sha512(data []byte) (hash [64]byte) — 完整雜湊，含填充與多區塊處理
//   sha512-hex(data []byte) (hex str) — 完整雜湊，返回 128 字元 hex 字串
//   sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64) — 處理單一 1024-bit 區塊（低階）
//
// 用法：
//   h0 = 7640891576956012808
//   h1 = 13503953896175478587
//   h2 = 4354685564936845355
//   h3 = 11912009170470909681
//   h4 = 5840696475078001361
//   h5 = 11170449401992604703
//   h6 = 2270897969802886507
//   h7 = 6620516959819538809
//   sha512-block(block, h0, h1, h2, h3, h4, h5, h6, h7)

// sha512-block: 處理一個 1024-bit 區塊
// s []u64: 16 個 64-bit 字 (big-endian)
// h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64: 輸入/輸出 512-bit 哈希狀態
sha512-block = (s str, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64) {

    // 64-bit 全 1 遮罩（用於位元 NOT）
    MASK64 = -1

    // 初始狀態
    a = h0
    b = h1
    c = h2
    d = h3
    e = h4
    f = h5
    g = h6
    h = h7

    // 第 0 輪 (K0 = 0x428A2F98D728AE22)
    S1 = ((e >> 14) | (e << 50))
    S1 = S1 ^ ((e >> 18) | (e << 46)) ^ ((e >> 41) | (e << 23))
    Ch = (e & f) ^ ((MASK64 ^ e) & g)
    S0 = ((a >> 28) | (a << 36))
    S0 = S0 ^ ((a >> 34) | (a << 30)) ^ ((a >> 39) | (a << 25))
    Maj = (a & b) ^ (a & c) ^ (b & c)
    T1 = h + S1 + Ch + 4794697086780616226 + s[0]
    T2 = S0 + Maj
    h = g
    g = f
    f = e
    e = d + T1
    d = c
    c = b
    b = a
    a = T1 + T2

    // 第 1 輪 (K1 = 0x7137449123EF65CD)
    S1 = ((e >> 14) | (e << 50))
    S1 = S1 ^ ((e >> 18) | (e << 46)) ^ ((e >> 41) | (e << 23))
    Ch = (e & f) ^ ((MASK64 ^ e) & g)
    S0 = ((a >> 28) | (a << 36))
    S0 = S0 ^ ((a >> 34) | (a << 30)) ^ ((a >> 39) | (a << 25))
    Maj = (a & b) ^ (a & c) ^ (b & c)
    T1 = h + S1 + Ch + 8158064640168781261 + s[1]
    T2 = S0 + Maj
    h = g
    g = f
    f = e
    e = d + T1
    d = c
    c = b
    b = a
    a = T1 + T2
}`),
		},

		{
			name: "comment8",
			input: strings.TrimSpace(`

// x509-rsa-e: 提取 RSA 公鑰指數（通常為 65537）
// data: DER 編碼憑證, n: 總長度
// e: 輸出指數值（0 表示非 RSA 或解析失敗）
x509-rsa-e = (data str, n i64, e i64) {

    // spki-start = 跳過 SPKI 的標籤+長度，指向內容
    // SPKI 內容：
    //   SEQUENCE (AlgorithmIdentifier)
    //   BIT STRING (subjectPublicKey)
    // 跳過 AlgorithmIdentifier SEQUENCE
    der-skip(data, n, p, p)

    // 跳過 INTEGER（序號）
    der-skip(data, n, p, p)

    // 跳過 SEQUENCE（簽章演算法）
    der-skip(data, n, p, p)

    // 跳過 SEQUENCE（發行者）
    der-skip(data, n, p, p)

    // 跳過 SEQUENCE（有效期）
    der-skip(data, n, p, p)
}
			`),
			expected: strings.TrimSpace(`

// x509-rsa-e: 提取 RSA 公鑰指數（通常為 65537）
// data: DER 編碼憑證, n: 總長度
// e: 輸出指數值（0 表示非 RSA 或解析失敗）
x509-rsa-e = (data str, n i64, e i64) {

    // spki-start = 跳過 SPKI 的標籤+長度，指向內容
    // SPKI 內容：
    //   SEQUENCE (AlgorithmIdentifier)
    //   BIT STRING (subjectPublicKey)
    // 跳過 AlgorithmIdentifier SEQUENCE
    der-skip(data, n, p, p)

    // 跳過 INTEGER（序號）
    der-skip(data, n, p, p)

    // 跳過 SEQUENCE（簽章演算法）
    der-skip(data, n, p, p)

    // 跳過 SEQUENCE（發行者）
    der-skip(data, n, p, p)

    // 跳過 SEQUENCE（有效期）
    der-skip(data, n, p, p)
}
			`),
		},

		{
			name: "str1",
			input: strings.TrimSpace(`
INVSBOX = '\x52\x09\x6a\xd5\x30\x36\xa5\x38\xbf\x40\xa3\x9e\x81\xf3\xd7\xfb' +
          '\x7c\xe3\x39\x82\x9b\x2f\xff\x87\x34\x8e\x43\x44\xc4\xde\xe9\xcb' +
          '\x54\x7b\x94\x32\xa6\xc2\x23\x3d\xee\x4c\x95\x0b\x42\xfa\xc3\x4e' +
          '\x08\x2e\xa1\x66\x28\xd9\x24\xb2\x76\x5b\xa2\x49\x6d\x8b\xd1\x25' +
          '\x72\xf8\xf6\x64\x86\x68\x98\x16\xd4\xa4\x5c\xcc\x5d\x65\xb6\x92' +
          '\x6c\x70\x48\x50\xfd\xed\xb9\xda\x5e\x15\x46\x57\xa7\x8d\x9d\x84' +
          '\x90\xd8\xab\x00\x8c\xbc\xd3\x0a\xf7\xe4\x58\x05\xb8\xb3\x45\x06' +
          '\xd0\x2c\x1e\x8f\xca\x3f\x0f\x02\xc1\xaf\xbd\x03\x01\x13\x8a\x6b' +
          '\x3a\x91\x11\x41\x4f\x67\xdc\xea\x97\xf2\xcf\xce\xf0\xb4\xe6\x73' +
          '\x96\xac\x74\x22\xe7\xad\x35\x85\xe2\xf9\x37\xe8\x1c\x75\xdf\x6e' +
          '\x47\xf1\x1a\x71\x1d\x29\xc5\x89\x6f\xb7\x62\x0e\xaa\x18\xbe\x1b' +
          '\xfc\x56\x3e\x4b\xc6\xd2\x79\x20\x9a\xdb\xc0\xfe\x78\xcd\x5a\xf4' +
          '\x1f\xdd\xa8\x33\x88\x07\xc7\x31\xb1\x12\x10\x59\x27\x80\xec\x5f' +
          '\x60\x51\x7f\xa9\x19\xb5\x4a\x0d\x2d\xe5\x7a\x9f\x93\xc9\x9c\xef' +
          '\xa0\xe0\x3b\x4d\xae\x2a\xf5\xb0\xc8\xeb\xbb\x3c\x83\x53\x99\x61' +
          '\x17\x2b\x04\x7e\xba\x77\xd6\x26\xe1\x69\x14\x63\x55\x21\x0c\x7d'
			`),
			expected: strings.TrimSpace(`
INVSBOX = '\x52\x09\x6a\xd5\x30\x36\xa5\x38\xbf\x40\xa3\x9e\x81\xf3\xd7\xfb' +
          '\x7c\xe3\x39\x82\x9b\x2f\xff\x87\x34\x8e\x43\x44\xc4\xde\xe9\xcb' +
          '\x54\x7b\x94\x32\xa6\xc2\x23\x3d\xee\x4c\x95\x0b\x42\xfa\xc3\x4e' +
          '\x08\x2e\xa1\x66\x28\xd9\x24\xb2\x76\x5b\xa2\x49\x6d\x8b\xd1\x25' +
          '\x72\xf8\xf6\x64\x86\x68\x98\x16\xd4\xa4\x5c\xcc\x5d\x65\xb6\x92' +
          '\x6c\x70\x48\x50\xfd\xed\xb9\xda\x5e\x15\x46\x57\xa7\x8d\x9d\x84' +
          '\x90\xd8\xab\x00\x8c\xbc\xd3\x0a\xf7\xe4\x58\x05\xb8\xb3\x45\x06' +
          '\xd0\x2c\x1e\x8f\xca\x3f\x0f\x02\xc1\xaf\xbd\x03\x01\x13\x8a\x6b' +
          '\x3a\x91\x11\x41\x4f\x67\xdc\xea\x97\xf2\xcf\xce\xf0\xb4\xe6\x73' +
          '\x96\xac\x74\x22\xe7\xad\x35\x85\xe2\xf9\x37\xe8\x1c\x75\xdf\x6e' +
          '\x47\xf1\x1a\x71\x1d\x29\xc5\x89\x6f\xb7\x62\x0e\xaa\x18\xbe\x1b' +
          '\xfc\x56\x3e\x4b\xc6\xd2\x79\x20\x9a\xdb\xc0\xfe\x78\xcd\x5a\xf4' +
          '\x1f\xdd\xa8\x33\x88\x07\xc7\x31\xb1\x12\x10\x59\x27\x80\xec\x5f' +
          '\x60\x51\x7f\xa9\x19\xb5\x4a\x0d\x2d\xe5\x7a\x9f\x93\xc9\x9c\xef' +
          '\xa0\xe0\x3b\x4d\xae\x2a\xf5\xb0\xc8\xeb\xbb\x3c\x83\x53\x99\x61' +
          '\x17\x2b\x04\x7e\xba\x77\xd6\x26\xe1\x69\x14\x63\x55\x21\x0c\x7d'
			`),
		},
		{
			name:     "str2",
			input:    `x = '\n'`,
			expected: `x = '\n'`,
		},
		{
			name:     "str3",
			input:    `x = '\t'`,
			expected: `x = '\t'`,
		},
		{
			name:     "str4",
			input:    `x = '\\'`,
			expected: `x = '\\'`,
		},
		{
			name:     "str5",
			input:    `x = '\x41'`,
			expected: `x = '\x41'`,
		},
		{
			name:     "str6",
			input:    `x = 'hello\nworld'`,
			expected: `x = 'hello\nworld'`,
		},
		{
			name:     "str7",
			input:    `x = 'a\tb\nc\x41'`,
			expected: `x = 'a\tb\nc\x41'`,
		},
		{
			name:     "str8",
			input:    "x = 'line1\\nline2'",
			expected: "x = 'line1\\nline2'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			fmt.Printf("tt.input:\n%s\n", tt.input)
			fmt.Printf("\nresult:\n%s\n", result)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatIndentation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "nested blocks",
			input:    "outer: (){inner: (){x=1}}",
			expected: "outer: () {\n    inner: () {\n        x = 1\n    }\n}",
		},
		{
			name:     "deep nesting",
			input:    "if x>0{if x>0{if x>0{x=1}}}",
			expected: "if x > 0 {\n    if x > 0 {\n        if x > 0 {\n            x = 1\n        }\n    }\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// cd ./src/fmt && go test -v . -run TestFormatProgram
func TestFormatProgram(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "import then function adds blank line",
			input:    "# std/bigint\nmod = (a bigint, b bigint, r bigint) {\n    q, r = div-mod(a, b)\n}",
			expected: "# std/bigint\n\nmod = (a bigint, b bigint, r bigint) {\n    q, r = div-mod(a, b)\n}",
		},
		{
			name:     "multiple imports no blanks between",
			input:    "# std/hash/md5\n# std/hash/sha1\n# std/fmt\ntest-fn = () {\n    fmt.println('ok')\n}",
			expected: "# std/hash/md5\n# std/hash/sha1\n# std/fmt\n\ntest-fn = () {\n    fmt.println('ok')\n}",
		},
		{
			name:     "import blanks compressed",
			input:    "# std/hash/md5\n\n# std/hash/sha1\n# std/fmt\n\ntest-fn = () {\n    fmt.println('ok')\n}",
			expected: "# std/hash/md5\n# std/hash/sha1\n# std/fmt\n\ntest-fn = () {\n    fmt.println('ok')\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			t.Logf("input:\n%s", tt.input)
			t.Logf("result:\n%s", result)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// cd ./src/fmt && go test -v . -run TestFormatMultiAssign
func TestFormatMultiAssign(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "mod with multi-assign capture",
			input:    "mod = (a bigint, b bigint, r bigint) { q, r = div-mod(a, b) }",
			expected: "mod = (a bigint, b bigint, r bigint) {\n    q, r = div-mod(a, b)\n}",
		},
		{
			name:     "lcm with multi-assign and nested calls",
			input:    "lcm = (a bigint, b bigint, l bigint) { g = bigint{} gcd(a, b, g) q, r = div-mod(a, g) l = mul(q, b) }",
			expected: "lcm = (a bigint, b bigint, l bigint) {\n    g = bigint {}\n    gcd(a, b, g)\n    q, r = div-mod(a, g)\n    l = mul(q, b)\n}",
		},
		{
			// . method call as a bare statement after a let/assignment
			// — must NOT drop the leading '.' (regression: skipToStatementEnd
			// used to skip over the DOT, producing 'hash(key, idx)' instead).
			name:     "dot method call after let before for",
			input:    "foo = () {\n    idx = 0\n    .hash(key, idx)\n    {\n        print(x)\n    } (x < 10)\n}",
			expected: "foo = () {\n    idx = 0\n    .hash(key, idx)\n    {\n        print(x)\n    } (x < 10)\n}",
		},
		{
			// . method call whose return is used as a statement (no assignment)
			name:     "dot method call standalone",
			input:    "foo = () {\n    .hash(key, idx)\n}",
			expected: "foo = () {\n    .hash(key, idx)\n}",
		},
		{
			// . method call followed by for loop whose condition also uses '.'
			name:     "dot method call then dot for condition",
			input:    "linked-hash-map.put = (key i64, val i64) (is-new bool) {\n    idx = .hash(key)\n    {\n        print(idx)\n    } (.occ[idx] == 1)\n}",
			expected: "linked-hash-map.put = (key i64, val i64) (is-new bool) {\n    idx = .hash(key)\n    {\n        print(idx)\n    } (.occ[idx] == 1)\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			t.Logf("input:\n%s", tt.input)
			t.Logf("result:\n%s", result)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatFunction(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "function with no args",
			input:    "hello: (){return}",
			expected: "hello: () {\n    return\n}",
		},
		{
			name:     "function with multiple args",
			input:    "add: (a int,b int,c int){a+b+c}",
			expected: "add: (a int, b int, c int) {\n    a + b + c\n}",
		},
		{
			name:     "method with result parameter",
			input:    "str.len: () (n i64) {n = .len}",
			expected: "str.len: () (n i64) {\n    n = .len\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// cd ./src/fmt && go test -v . -run TestFormatComment/1
func TestFormatComment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "0",
			input: strings.TrimSpace(`

            `),
			expected: strings.TrimSpace(`
            
            `),
		},
		{
			name: "1",
			input: strings.TrimSpace(`// aes-128-dec: 解密一個 16-byte 區塊
// in: 輸入密文（16 位元組）
// n: 固定 16
// key: 16-byte 金鑰
// out: 輸出明文（16 位元組）
aes-128-dec = (in str, n i64, key str, out str) {

    // 展開金鑰
    ek = '(16+160 bytes)'
    aes-key-expand(key, ek)

    // 複製輸入到狀態
    i = 0
    {
        out[i] = in[i]
        i = i + 1
    } (i < 16)

    // 初始 AddRoundKey（輪 10）
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    } (round > 0)

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}`),
			expected: strings.TrimSpace(`// aes-128-dec: 解密一個 16-byte 區塊
// in: 輸入密文（16 位元組）
// n: 固定 16
// key: 16-byte 金鑰
// out: 輸出明文（16 位元組）
aes-128-dec = (in str, n i64, key str, out str) {

    // 展開金鑰
    ek = '(16+160 bytes)'
    aes-key-expand(key, ek)

    // 複製輸入到狀態
    i = 0
    {
        out[i] = in[i]
        i = i + 1
    } (i < 16)

    // 初始 AddRoundKey（輪 10）
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    } (round > 0)

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestFormatLabeledFor ensures that labeled loop syntax (`#N name ...`) is
// preserved by the formatter. Before the fix the formatter would mangle
// the source and emit the labels as bare numbers, completely destroying
// the program on save.
func TestFormatLabeledFor(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "labeled bare range-for",
			input: `#1 i <- [0..256): {
    x = 1
}`,
			expected: `#1 i <- [0..256): {
    x = 1
}`,
		},
		{
			name: "labeled infinite loop",
			input: `#1 {
    x = 1
} ()`,
			expected: `#1 {
    x = 1
} ()`,
		},
		{
			name: "labeled conditional",
			input: `#1 {
    val == 1
    x = 1
} (val)`,
			expected: `#1 {
    val == 1
    x = 1
} (val)`,
		},
		{
			name: "nested labeled for inside function",
			input: `f = () {
    #1 i <- [0..10): {
        x = 1
    }
}`,
			expected: `f = () {
    #1 i <- [0..10): {
        x = 1
    }
}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestFormatStarBreakContinue ensures that the `*` (break) and `**`
// (continue) shorthand forms are preserved by the formatter and that
// `break #1` / `continue #1` round-trip the numeric label correctly.
func TestFormatStarBreakContinue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "star shorthand round-trips",
			input: `f = () {
    *
}`,
			expected: `f = () {
    *
}`,
		},
		{
			name: "star star shorthand round-trips",
			input: `f = () {
    **
}`,
			expected: `f = () {
    **
}`,
		},
		{
			name: "star with numeric label round-trips",
			input: `f = () {
    * #1
}`,
			expected: `f = () {
    * #1
}`,
		},
		{
			name: "star star with numeric label round-trips",
			input: `f = () {
    ** #1
}`,
			expected: `f = () {
    ** #1
}`,
		},
		{
			name: "break with hash-prefixed label round-trips",
			input: `f = () {
    break #1
}`,
			expected: `f = () {
    break #1
}`,
		},
		{
			name: "continue with hash-prefixed label round-trips",
			input: `f = () {
    continue #1
}`,
			expected: `f = () {
    continue #1
}`,
		},
		{
			// Regression: `* #1` and `** #1` after `break #1` were
			// silently dropped because `skipToStatementEnd()` swallowed
			// them (MUL/STAR_STAR/LABEL missing from isStatementBoundary).
			name: "sequence of break + star + starstar round-trips",
			input: `f = () {
    break #1
    * #1
    ** #1
}`,
			expected: `f = () {
    break #1
    * #1
    ** #1
}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatUnionType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single-type alias round-trips",
			input:    "my-int = i64\n",
			expected: "my-int = i64",
		},
		{
			name:     "union type alias round-trips",
			input:    "int = i8 | i16 | i32 | i64\n",
			expected: "int = i8 | i16 | i32 | i64",
		},
		{
			name:     "chained union type alias round-trips",
			input:    "num = int | float\n",
			expected: "num = int | float",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatEqualsTypeAlias(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "equals union type",
			input:    "int = i8 | i16 | i32 | i64\n",
			expected: "int = i8 | i16 | i32 | i64",
		},
		{
			name:     "equals chained union",
			input:    "num = int | float\n",
			expected: "num = int | float",
		},
		{
			name:     "equals single type alias",
			input:    "bytes = []byte\n",
			expected: "bytes = []byte",
		},
		{
			name:     "equals simple alias",
			input:    "my-int = i64\n",
			expected: "my-int = i64",
		},
		{
			name:     "equals array type alias",
			input:    "buf = [16]u8\n",
			expected: "buf = [16]u8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestFormatTypeAliasBlankLinePreservation verifies that a blank line between
// doc comments and a type alias statement is preserved by the formatter.
// Before the fix, stmtTokenLine() was missing the *TypeAlias case (returned 0),
// so the blank-line-preservation check failed and the formatter collapsed the
// blank line away.
func TestFormatTypeAliasBlankLinePreservation(t *testing.T) {
	input := `// header comment
//
// header 2

int = i8 | i16
`
	expected := `// header comment
//
// header 2

int = i8 | i16`
	if got := Format(input); got != expected {
		t.Errorf("Format mismatch:\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

// TestFormatTypeAliasDocAttaches verifies that doc comments preceding a type
// alias are attached to the *TypeAlias node (not silently dropped) and then
// emitted by the formatter. Before the fix, setDoc() was missing the
// *TypeAlias case, causing the doc to vanish.
func TestFormatTypeAliasDocAttaches(t *testing.T) {
	input := `// describes int
int = i8 | i16
`
	expected := `// describes int
int = i8 | i16`
	if got := Format(input); got != expected {
		t.Errorf("Format mismatch:\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

// TestFormatMatchIdempotent verifies formatting test-match.no is idempotent.
func TestFormatMatchIdempotent(t *testing.T) {
	data, err := os.ReadFile("../../tests/test-match.no")
	if err != nil {
		t.Fatalf("read test-match.no: %v", err)
	}
	input := string(data)
	result := Format(input)
	// Double-format must be idempotent
	if result != Format(result) {
		t.Errorf("Format() is not idempotent\nfirst:\n%q\nsecond:\n%q", result, Format(result))
	}
}

func TestFormatMatch(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "deep nesting",
			input: `
test-match = () {
    x ?i64

    // 保存的時候會改變，不要變成if/else 修復它
    // 直接->是else
    x: {
        err -> log(it)
        nil -> log(it)
        -> log(it)
    }

    // 全部列舉
    x: {
        err -> log(it)
        nil -> log(it)
        ok -> log(it)
    }

    // 這裡-> 有else的意思
    x: {
        ok -> log(it)
        -> log(it)
    }

    // 這是if/else
    {
        a == 1 -> log('1')
        -> log('else')
    }
}
            `,
			expected: `test-match = () {
    x ?i64

    // 保存的時候會改變，不要變成if/else 修復它
    // 直接->是else
    x: {
        err -> log(it)
        nil -> log(it)
        -> log(it)
    }

    // 全部列舉
    x: {
        err -> log(it)
        nil -> log(it)
        ok -> log(it)
    }

    // 這裡-> 有else的意思
    x: {
        ok -> log(it)
        -> log(it)
    }

    // 這是if/else
    {
        a == 1 -> log('1')
        -> log('else')
    }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatCombinedOptionPatterns(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "nil || err with ok arm",
			input: `test = () {
    x ?i64
    x: {
        nil || err -> {
            cleanup()
            return
        }
        ok -> process(it)
    }
}`,
			expected: `test = () {
    x ?i64
    x: {
        nil || err -> {
            cleanup()
            return
        }
        ok -> process(it)
    }
}`,
		},
		{
			name: "nil || err inline body",
			input: `test = () {
    x ?i64
    x: {
        nil || err -> log('failed')
        ok -> process(it)
    }
}`,
			expected: `test = () {
    x ?i64
    x: {
        nil || err -> log('failed')
        ok -> process(it)
    }
}`,
		},
		{
			name: "err || nil order preserved",
			input: `test = () {
    x ?i64
    x: {
        err || nil -> log('failed')
        ok -> process(it)
    }
}`,
			expected: `test = () {
    x ?i64
    x: {
        err || nil -> log('failed')
        ok -> process(it)
    }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatMapType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "map type with literal and method call",
			input: `test = () {
    m [str]i64 = { 'a':0, 'b':1 }
    m.put('c', 2)
}`,
			expected: `test = () {
    m [str]i64 = { 'a': 0, 'b': 1 }
    m.put('c', 2)
}`,
		},
		{
			name: "map type empty literal",
			input: `test = () {
    m [str]i64 = { }
}`,
			expected: `test = () {
    m [str]i64 = { }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestFormatFile verifies that FormatFile enforces exactly one trailing
// newline at EOF: missing newlines are appended, multiple trailing newlines
// are collapsed to one. The result is also idempotent. Unparseable or
// empty/whitespace input is returned unchanged.
func TestFormatFile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "missing trailing newline is appended",
			input:    "add = (a i64, b i64) { a + b }",
			expected: "add = (a i64, b i64) {\n    a + b\n}\n",
		},
		{
			name:     "single trailing newline preserved",
			input:    "x = 1\n",
			expected: "x = 1\n",
		},
		{
			name:     "multiple trailing newlines collapsed to one",
			input:    "x = 1\n\n\n",
			expected: "x = 1\n",
		},
		{
			name:     "existing file with trailing newline stays stable",
			input:    "foo = () {\n    return\n}\n",
			expected: "foo = () {\n    return\n}\n",
		},
		{
			name:     "empty input returned unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace-only input returned unchanged",
			input:    "   \n  \n",
			expected: "   \n  \n",
		},
		{
			name:     "parse error returned unchanged",
			input:    "fn = (a i64 {",
			expected: "fn = (a i64 {",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatFile(tt.input)
			if got != tt.expected {
				t.Errorf("FormatFile(%q) = %q, want %q", tt.input, got, tt.expected)
			}
			// FormatFile must be idempotent for every input.
			if FormatFile(got) != got {
				t.Errorf("FormatFile not idempotent for %q: FormatFile(%q) = %q", tt.input, got, FormatFile(got))
			}
		})
	}
}

// TestFormatFileIdempotent confirms FormatFile(FormatFile(x)) == FormatFile(x)
// for a realistic multi-statement source with extra trailing blank lines.
func TestFormatFileIdempotent(t *testing.T) {
	input := "add = (a i64, b i64) {\n  a + b\n}\n\n\n"
	once := FormatFile(input)
	twice := FormatFile(once)
	if once != twice {
		t.Errorf("FormatFile not idempotent:\nfirst:  %q\nsecond: %q", once, twice)
	}
}

// TestFormatSemicolonComment verifies that ';' line comments are preserved as
// ';' (not normalized to '//') and that '//' comments stay '//'. ';' is a
// reserved comment marker in nolang.
func TestFormatSemicolonComment(t *testing.T) {
	// ';' line comments must be preserved as ';' (not normalized to '//').
	input := "; 行首註釋\nx = 1; 行尾註釋\ny = 2\n; 另一行註釋\n"
	got := Format(input)
	want := "; 行首註釋\nx = 1; 行尾註釋\ny = 2\n; 另一行註釋"
	if got != want {
		t.Errorf("Format semicolon comment:\n got:  %q\n want: %q", got, want)
	}
	// idempotent via FormatFile (adds exactly one trailing newline, preserves ';')
	file := FormatFile(input)
	if FormatFile(file) != file {
		t.Errorf("FormatFile not idempotent: %q", file)
	}
	if !strings.HasSuffix(file, "; 另一行註釋\n") {
		t.Errorf("trailing ';' comment not preserved: %q", file)
	}

	// '//' comments must remain '//' (formatter uses two-space indent before //).
	input2 := "// line comment\nx = 1 // trailing\n"
	want2 := "// line comment\nx = 1  // trailing"
	if got2 := Format(input2); got2 != want2 {
		t.Errorf("Format // comment:\n got:  %q\n want: %q", got2, want2)
	}
}

// TestFormatSemicolonBlockComment verifies the comment semantics:
//   - ";" and ";; xxx" are single-line comments (to end of line)
//   - ";;\n" triggers a multi-line (block) comment, terminated by a ";;" that
//     is followed by a newline/EOF. The formatter preserves delimiters verbatim
//     (idempotent).
func TestFormatSemicolonBlockComment(t *testing.T) {
	cases := []string{
		// multi-line block comment as doc
		";;\ndoc block\n;;\nx = 1\n",
		// multi-line block comment with multiple content lines
		";;\nfirst line\nsecond line\nthird line\n;;\nx = 1\n",
		// trailing (EOF) multi-line block comment
		"x = 1\n;;\ntrailing\nmulti-line block\n;;\n",
		// inline single-line ;; comment (no newline after ;; → single-line)
		"x = 1 ;; inline single-line\n",
		// standalone single-line ;; comment
		";; single line\nx = 1\n",
		// mixed: ;; multi-line block + ; line + // line
		";;\nblock\n;;\n; line comment\n// another line\nx = 1\n",
	}
	for _, in := range cases {
		file := FormatFile(in)
		if FormatFile(file) != file {
			t.Errorf("FormatFile not idempotent for %q:\n first:  %q\n second: %q", in, file, FormatFile(file))
		}
		if !strings.Contains(file, ";;") {
			t.Errorf("block comment markers lost for %q -> %q", in, file)
		}
		if strings.Contains(file, "//") && !strings.Contains(in, "//") {
			t.Errorf("unexpected // introduced for %q -> %q", in, file)
		}
	}
}

// TestFormatEnumPreservesBare verifies that a simple enum written without
// explicit `= <int>` values (e.g. `color { red, green, blue }`) is preserved
// verbatim by the formatter — the formatter must NOT inject sequential values
// like `red, green = 1, blue = 2`. Explicitly written values are still kept.
func TestFormatEnumPreservesBare(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bare enum preserved",
			in:   "color {\n    red,\n    green,\n    blue,\n}\n",
			want: "color {\n    red,\n    green,\n    blue,\n}\n",
		},
		{
			name: "explicit values kept",
			in:   "priority {\n    low = 1,\n    high = 5,\n    critical = 9,\n}\n",
			want: "priority {\n    low = 1,\n    high = 5,\n    critical = 9,\n}\n",
		},
		{
			name: "mixed: explicit first, rest bare",
			in:   "mode {\n    a = 3,\n    b,\n    c,\n}\n",
			want: "mode {\n    a = 3,\n    b,\n    c,\n}\n",
		},
	}
	for _, c := range cases {
		got := FormatFile(c.in)
		if got != c.want {
			t.Errorf("%s:\n got:  %q\n want: %q", c.name, got, c.want)
		}
		// Idempotency: re-formatting must not change anything.
		if FormatFile(got) != got {
			t.Errorf("%s: not idempotent: %q -> %q", c.name, got, FormatFile(got))
		}
	}
}

// TestFormatRegexLiteral verifies that /pattern/flags regex literals
// are preserved through formatting (idempotent round-trip).
func TestFormatRegexLiteral(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple regex assignment",
			input:    "re = /abc/",
			expected: "re = /abc/\n",
		},
		{
			name:     "regex with flags",
			input:    "re = /abc/gi",
			expected: "re = /abc/gi\n",
		},
		{
			name:     "regex with digit class",
			input:    "re = /\\d+/",
			expected: "re = /\\d+/\n",
		},
		{
			name:     "regex with escaped slash",
			input:    "re = /a\\/b/",
			expected: "re = /a\\/b/\n",
		},
		{
			name:     "regex in function call",
			input:    "result = match-text(/\\d+/, text)",
			expected: "result = match-text(/\\d+/, text)\n",
		},
		{
			name:     "regex with char class",
			input:    "re = /[a-z]+/",
			expected: "re = /[a-z]+/\n",
		},
	}
	for _, tt := range tests {
		got := FormatFile(tt.input)
		if got != tt.expected {
			t.Errorf("%s:\n got:  %q\n want: %q", tt.name, got, tt.expected)
		}
		// Idempotency: re-formatting must not change anything.
		if FormatFile(got) != got {
			t.Errorf("%s: not idempotent: %q -> %q", tt.name, got, FormatFile(got))
		}
	}
}

// TestFormatScalarSlicePerLine8 verifies that slice literals with >8 scalar
// literal elements (num/byte/char/bool etc.) are formatted with 8 elements per
// line, instead of 1-per-line. Compound elements (nested arrays, structs) keep
// the existing source-preserving behavior.
func TestFormatScalarSlicePerLine8(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "scalar slice literal 16 ints uses 8-per-line",
			input: "v []i64 = [\n    0,\n    1,\n    2,\n    3,\n    4,\n    5,\n    6,\n    7,\n    8,\n    9,\n    10,\n    11,\n    12,\n    13,\n    14,\n    15,\n]\n",
			expected: "v []i64 = [\n    0, 1, 2, 3, 4, 5, 6, 7,\n    8, 9, 10, 11, 12, 13, 14, 15,\n]\n",
		},
		{
			name:  "scalar slice literal 9 ints single source line becomes 8+1",
			input: "v []i64 = [0, 1, 2, 3, 4, 5, 6, 7, 8]\n",
			expected: "v []i64 = [\n    0, 1, 2, 3, 4, 5, 6, 7,\n    8,\n]\n",
		},
		{
			name: "scalar slice literal 20 bytes uses 8-per-line",
			input: "v []byte = [0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13]\n",
			expected: "v []byte = [\n    0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,\n    0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,\n    0x10, 0x11, 0x12, 0x13,\n]\n",
		},
		{
			name: "negative scalar literals also use 8-per-line",
			input: "v []i64 = [-1, -2, -3, -4, -5, -6, -7, -8, -9, -10]\n",
			expected: "v []i64 = [\n    -1, -2, -3, -4, -5, -6, -7, -8,\n    -9, -10,\n]\n",
		},
		{
			name: "scalar slice with <=8 elements stays single line",
			input: "v []i64 = [0, 1, 2, 3, 4, 5, 6, 7]\n",
			expected: "v []i64 = [0, 1, 2, 3, 4, 5, 6, 7]\n",
		},
		{
			name: "char scalar slice uses 8-per-line",
			input: "v []char = [\"a\", \"b\", \"c\", \"d\", \"e\", \"f\", \"g\", \"h\", \"i\", \"j\"]\n",
			expected: "v []char = [\n    \"a\", \"b\", \"c\", \"d\", \"e\", \"f\", \"g\", \"h\",\n    \"i\", \"j\",\n]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatFile(tt.input)
			if got != tt.expected {
				t.Errorf("Format mismatch:\ninput:\n%s\ngot:\n%s\nwant:\n%s", tt.input, got, tt.expected)
			}
			// Idempotency: re-formatting must not change anything.
			if FormatFile(got) != got {
				t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", got, FormatFile(got))
			}
		})
	}
}
