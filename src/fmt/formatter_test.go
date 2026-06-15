package fmt

import (
	"fmt"
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
			input:    "for x<10{x=x+1}",
			expected: "for x < 10 {\n    x = x + 1\n}",
		},
		{
			name:     "infinite for loop with break",
			input:    "for{break}",
			expected: "for {\n    break\n}",
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

for i <- a {
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

for i <- a {
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
    for off + 512 <= n {
        empty = 1
        i = 0
        for i < 512 {
            if data[off + i] & 255 != 0 {
                empty = 0
                break
            }
            i = i + 1
        }
        if empty == 1 {
            return
        }
        name = ''
        i = 0
        for i < 100 {
            c = data[off + i] & 255
            if c == 0 {
                break
            }
            name[i] = c
            i = i + 1
        }
        sz = 0
        i = 0
        for i < 12 {
            c = data[off + 124 + i] & 255
            if c >= 48 && c <= 57 {
                sz = sz * 8 + c - 48
            }
            i = i + 1
        }
        c = data[off + 156] & 255
        if c == 48 || c == 0 {
            typ = 'file'
        } elif c == 53 {
            typ = 'dir'
        } else {
            typ = 'unknown'
        }
    }
    if sz > 0 {
        i = 0
        for i < sz {
            data-out[i] = data[off + 512 + i]
            i = i + 1
        }
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
    for off + 512 <= n {
        empty = 1
        i = 0
        for i < 512 {
            if data[off + i] & 255 != 0 {
                empty = 0
                break
            }
            i = i + 1
        }
        if empty == 1 {
            return
        }
        name = ''
        i = 0
        for i < 100 {
            c = data[off + i] & 255
            if c == 0 {
                break
            }
            name[i] = c
            i = i + 1
        }
        sz = 0
        i = 0
        for i < 12 {
            c = data[off + 124 + i] & 255
            if c >= 48 && c <= 57 {
                sz = sz * 8 + c - 48
            }
            i = i + 1
        }
        c = data[off + 156] & 255
        if c == 48 || c == 0 {
            typ = 'file'
        } elif c == 53 {
            typ = 'dir'
        } else {
            typ = 'unknown'
        }
    }
    if sz > 0 {
        i = 0
        for i < sz {
            data-out[i] = data[off + 512 + i]
            i = i + 1
        }
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
			input: strings.TrimSpace(`

// ─── 迭代器 ───────────────────────────────────

// tar-for-each: 遍歷所有條目
// 每次回呼傳入 (idx, name, sz, typ, data)
// 返回 0 繼續，非 0 停止
tar-for-each = (data []byte, idx i64, name str, sz i64, typ str, data-out []byte) {
    idx = 0
    n = len(data)
    off = 0
    for off + 512 <= n {
        // 檢查結束
        empty = 1
        i = 0
        for i < 512 {
            if data[off + i] & 255 != 0 {
                empty = 0
                break
            }
            i = i + 1
        }
        if empty == 1 { return }

        // 讀取名稱
        name = ''
        i = 0
        for i < 100 {
            c = data[off + i] & 255
            if c == 0 { break }
            name[i] = c
            i = i + 1
        }

        // 大小
        sz = 0
        i = 0
        for i < 12 {
            c = data[off + 124 + i] & 255
            if c >= 48 && c <= 57 {
                sz = sz * 8 + (c - 48)
            }
            i = i + 1
        }

        // 類型
        c = data[off + 156] & 255
        if c == 48 || c == 0 { typ = 'file' }
        elif c == 53 { typ = 'dir' }
        else { typ = 'unknown' }

        // 資料
        if sz > 0 {
            i = 0
            for i < sz {
                data-out[i] = data[off + 512 + i]
                i = i + 1
            }
        }

        // 前進到下個條目
        blocks = (sz + 511) / 512
        if blocks < 0 { blocks = 0 }
        off = off + 512 + blocks * 512
        idx = idx + 1
    }
}

			`),
			expected: strings.TrimSpace(`

// ─── 迭代器 ───────────────────────────────────

// tar-for-each: 遍歷所有條目
// 每次回呼傳入 (idx, name, sz, typ, data)
// 返回 0 繼續，非 0 停止
tar-for-each = (data []byte, idx i64, name str, sz i64, typ str, data-out []byte) {
    idx = 0
    n = len(data)
    off = 0
    for off + 512 <= n {
        // 檢查結束
        empty = 1
        i = 0
        for i < 512 {
            if data[off + i] & 255 != 0 {
                empty = 0
                break
            }
            i = i + 1
        }
        if empty == 1 {
            return
        }

        // 讀取名稱
        name = ''
        i = 0
        for i < 100 {
            c = data[off + i] & 255
            if c == 0 {
                break
            }
            name[i] = c
            i = i + 1
        }

        // 大小
        sz = 0
        i = 0
        for i < 12 {
            c = data[off + 124 + i] & 255
            if c >= 48 && c <= 57 {
                sz = sz * 8 + (c - 48)
            }
            i = i + 1
        }

        // 類型
        c = data[off + 156] & 255
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
            for i < sz {
                data-out[i] = data[off + 512 + i]
                i = i + 1
            }
        }

        // 前進到下個條目
        blocks = (sz + 511) / 512
        if blocks < 0 {
            blocks = 0
        }
        off = off + 512 + blocks * 512
        idx = idx + 1
    }
}

			`),
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
