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
			name: "func7",
			input: strings.TrimSpace(`
// aes-key-expand: 將 16-byte 金鑰展開為 176-byte 輪金鑰
// ek: 輸出輪金鑰字串（176 位元組）
aes-key-expand = (key str, ek str) {
    // 複製原始金鑰（前 16 位元組）
    i = 0
    for i < 16 {
        ek[i] = key[i]
        i = i + 1
    }

    // 產生 w[4..43]（共 44 個 32-bit 字 = 176 位元組）
    i = 4
    for i < 44 {
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
    }
    ek[i * 4] = (w >> 24) & 255
    ek[i * 4 + 1] = (w >> 16) & 255
    ek[i * 4 + 2] = (w >> 8) & 255
    ek[i * 4 + 3] = w & 255
    i = i + 1
}
			`),
			expected: strings.TrimSpace(`
// aes-key-expand: 將 16-byte 金鑰展開為 176-byte 輪金鑰
// ek: 輸出輪金鑰字串（176 位元組）
aes-key-expand = (key str, ek str) {
    // 複製原始金鑰（前 16 位元組）
    i = 0
    for i < 16 {
        ek[i] = key[i]
        i = i + 1
    }

    // 產生 w[4..43]（共 44 個 32-bit 字 = 176 位元組）
    i = 4
    for i < 44 {
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
    }
    ek[i * 4] = (w >> 24) & 255
    ek[i * 4 + 1] = (w >> 16) & 255
    ek[i * 4 + 2] = (w >> 8) & 255
    ek[i * 4 + 3] = w & 255
    i = i + 1
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
    for i < 16 {
        out[i] = in[i]
        i = i + 1
    }

    // 初始 AddRoundKey（輪 10）
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    for round > 0 {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    }

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}
			`),
			expected: strings.TrimSpace(`

// aes-128-dec: 解密一個 16-byte 區塊
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
    for i < 16 {
        out[i] = in[i]
        i = i + 1
    }

    // 初始 AddRoundKey（輪 10）
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    for round > 0 {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    }

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}
			`),
		},

		{
			name: "comment3",
			input: strings.TrimSpace(`

// ─── 單區塊加密/解密 ──────────────────────────────

// aes-128-enc: 加密一個 16-byte 區塊
// in: 輸入明文（16 位元組）
// n: 固定 16
// key: 16-byte 金鑰
// out: 輸出密文（16 位元組）
aes-128-enc= (in str, n i64, key str, out str) {
    // 展開金鑰
    ek = '(16+160 bytes)'
    aes-key-expand(key, ek)

    // 複製輸入到狀態
    i = 0
    for i < 16 {
        out[i] = in[i]
        i = i + 1
    }

    // 初始 AddRoundKey（輪 0）
    // 輪金鑰 0：ek[0..15]
    add-round-key(out, ek)

    // 第 1-9 輪
    round = 1
    for round < 10 {
        sub-bytes(out, 16)
        shift-rows(out)
        mix-columns(out)
        // 輪金鑰 round：ek[round*16..round*16+15]
        rk-off = round * 16
        add-round-key(out, ek + rk-off)  // 需要 ek 子字串
        round = round + 1
    }

    // 第 10 輪（無 MixColumns）
    sub-bytes(out, 16)
    shift-rows(out)
    add-round-key(out, ek + 160)
}
			`),
			expected: strings.TrimSpace(`


// ─── 單區塊加密/解密 ──────────────────────────────

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
    for i < 16 {
        out[i] = in[i]
        i = i + 1
    }

    // 初始 AddRoundKey（輪 0）
    // 輪金鑰 0：ek[0..15]
    add-round-key(out, ek)

    // 第 1-9 輪
    round = 1
    for round < 10 {
        sub-bytes(out, 16)
        shift-rows(out)
        mix-columns(out)

        // 輪金鑰 round：ek[round*16..round*16+15]
        rk-off = round * 16
        add-round-key(out, ek + rk-off)  // 需要 ek 子字串
        round = round + 1
    }

    // 第 10 輪（無 MixColumns）
    sub-bytes(out, 16)
    shift-rows(out)
    add-round-key(out, ek + 160)
}
			`),
		},

		{
			name: "comment4",
			input: strings.TrimSpace(`
// sha1 — SHA-1 安全哈希算法（160-bit）
//
// 處理一個 512-bit（16 字）區塊。
// 多區塊訊息需由呼叫者自行填充和累加。
//
// 用法：
//   h0 = 1732584193; h1 = 4023233417; h2 = 2562383102; h3 = 271733878; h4 = 3285377520
//   sha1(block, 16, h0, h1, h2, h3, h4)

// sha1: 處理一個 512-bit 區塊
// s: 16 個 32-bit 字 (big-endian)
// n: 固定 16
// h0..h4: 輸入/輸出 160-bit 哈希狀態
sha1 = (s str, n i64, h0 i64, h1 i64, h2 i64, h3 i64, h4 i64) {
    MASK = 4294967295

    // 初始狀態
    a = h0; b = h1; c = h2; d = h3; e = h4

    // ---- 第 0-19 輪 (K = 0x5A827999 = 1518500249) ----
    // f = (b & c) | (~b & d)

    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK; temp = (temp + f + e + 1518500249 + s[0])  & MASK
    e = d; d = c; c = ((b << 30) | (b >> 2)) & MASK; b = a; a = temp



    // 第 16-19 輪 — 擴展訊息，rotl(w_{t-3} ^ w_{t-8} ^ w_{t-14} ^ w_{t-16}, 1)

    // w16 = rotl(w13 ^ w8 ^ w2 ^ w0, 1)
    w = s[13] ^ s[8] ^ s[2] ^ s[0]; w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK; temp = (temp + f + e + 1518500249 + w) & MASK
    e = d; d = c; c = ((b << 30) | (b >> 2)) & MASK; b = a; a = temp

    // w17 = rotl(w14 ^ w9 ^ w3 ^ w1, 1)
    w = s[14] ^ s[9] ^ s[3] ^ s[1]; w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK; temp = (temp + f + e + 1518500249 + w) & MASK
    e = d; d = c; c = ((b << 30) | (b >> 2)) & MASK; b = a; a = temp

    // w18 = rotl(w15 ^ w10 ^ w4 ^ w2, 1)
    w = s[15] ^ s[10] ^ s[4] ^ s[2]; w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK; temp = (temp + f + e + 1518500249 + w) & MASK
    e = d; d = c; c = ((b << 30) | (b >> 2)) & MASK; b = a; a = temp



    // 累加回初始哈希值
    h0 = (h0 + a) & MASK
    h1 = (h1 + b) & MASK
    h2 = (h2 + c) & MASK
    h3 = (h3 + d) & MASK
    h4 = (h4 + e) & MASK
}

			`),
			expected: strings.TrimSpace(`
// sha1 — SHA-1 安全哈希算法（160-bit）
//
// 處理一個 512-bit（16 字）區塊。
// 多區塊訊息需由呼叫者自行填充和累加。
//
// 用法：
//   h0 = 1732584193; h1 = 4023233417; h2 = 2562383102; h3 = 271733878; h4 = 3285377520
//   sha1(block, 16, h0, h1, h2, h3, h4)

// sha1: 處理一個 512-bit 區塊
// s: 16 個 32-bit 字 (big-endian)
// n: 固定 16
// h0..h4: 輸入/輸出 160-bit 哈希狀態
sha1 = (s str, n i64, h0 i64, h1 i64, h2 i64, h3 i64, h4 i64) {
    MASK = 4294967295

    // 初始狀態
    a = h0; b = h1; c = h2; d = h3; e = h4

    // ---- 第 0-19 輪 (K = 0x5A827999 = 1518500249) ----
    // f = (b & c) | (~b & d)

    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK; temp = (temp + f + e + 1518500249 + s[0]) & MASK
    e = d; d = c; c = ((b << 30) | (b >> 2)) & MASK; b = a; a = temp

    // 第 16-19 輪 — 擴展訊息，rotl(w_{t-3} ^ w_{t-8} ^ w_{t-14} ^ w_{t-16}, 1)

    // w16 = rotl(w13 ^ w8 ^ w2 ^ w0, 1)
    w = s[13] ^ s[8] ^ s[2] ^ s[0]; w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK; temp = (temp + f + e + 1518500249 + w) & MASK
    e = d; d = c; c = ((b << 30) | (b >> 2)) & MASK; b = a; a = temp

    // w17 = rotl(w14 ^ w9 ^ w3 ^ w1, 1)
    w = s[14] ^ s[9] ^ s[3] ^ s[1]; w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK; temp = (temp + f + e + 1518500249 + w) & MASK
    e = d; d = c; c = ((b << 30) | (b >> 2)) & MASK; b = a; a = temp

    // w18 = rotl(w15 ^ w10 ^ w4 ^ w2, 1)
    w = s[15] ^ s[10] ^ s[4] ^ s[2]; w = ((w << 1) | (w >> 31)) & MASK
    f = (b & c) | ((MASK ^ b) & d)
    temp = ((a << 5) | (a >> 27)) & MASK; temp = (temp + f + e + 1518500249 + w) & MASK
    e = d; d = c; c = ((b << 30) | (b >> 2)) & MASK; b = a; a = temp

    // 累加回初始哈希值
    h0 = (h0 + a) & MASK
    h1 = (h1 + b) & MASK
    h2 = (h2 + c) & MASK
    h3 = (h3 + d) & MASK
    h4 = (h4 + e) & MASK
}
`),
		},

		{
			name: "comment5",
			input: strings.TrimSpace(`
// sha512 — SHA-512 安全哈希算法（512-bit）
//
// 處理一個 1024-bit（16 個 64-bit 字）區塊。
// 多區塊訊息需由呼叫者自行填充和累加。
//
// 用法：
//   h0 = 7640891576956012808;  h1 = -4942790177534073029
//   h2 = 4354685564936845355;  h3 = -6534734903238641935
//   h4 = 5840696475078001361;  h5 = -7276294671716946913
//   h6 = 2270897969802886507;  h7 = 6620516959819538809
//   sha512(block, 16, h0, h1, h2, h3, h4, h5, h6, h7)

// sha512: 處理一個 1024-bit 區塊
// s: 16 個 64-bit 字 (big-endian)
// n: 固定 16
// h0..h7: 輸入/輸出 512-bit 哈希狀態
sha512=(s str, n i64, h0 i64, h1 i64, h2 i64, h3 i64, h4 i64, h5 i64, h6 i64, h7 i64) {
    // 64-bit 全 1 遮罩（用於位元 NOT）
    MASK64 = -1

    // 初始狀態
    a = h0; b = h1; c = h2; d = h3; e = h4; f = h5; g = h6; h = h7

    // 第 0 輪 (K0 = 0x428A2F98D728AE22)
    S1 = ((e >> 14) | (e << 50)); S1 = S1 ^ ((e >> 18) | (e << 46)) ^ ((e >> 41) | (e << 23))
    Ch = (e & f) ^ ((MASK64 ^ e) & g)
    S0 = ((a >> 28) | (a << 36)); S0 = S0 ^ ((a >> 34) | (a << 30)) ^ ((a >> 39) | (a << 25))
    Maj = (a & b) ^ (a & c) ^ (b & c)
    T1 = h + S1 + Ch + 4794697086780616226 + s[0]
    T2 = S0 + Maj
    h = g; g = f; f = e; e = d + T1
    d = c; c = b; b = a; a = T1 + T2
    // 第 1 輪 (K1 = 0x7137449123EF65CD)
    S1 = ((e >> 14) | (e << 50)); S1 = S1 ^ ((e >> 18) | (e << 46)) ^ ((e >> 41) | (e << 23))
    Ch = (e & f) ^ ((MASK64 ^ e) & g)
    S0 = ((a >> 28) | (a << 36)); S0 = S0 ^ ((a >> 34) | (a << 30)) ^ ((a >> 39) | (a << 25))
    Maj = (a & b) ^ (a & c) ^ (b & c)
    T1 = h + S1 + Ch + 8158064640168781261 + s[1]
    T2 = S0 + Maj
    h = g; g = f; f = e; e = d + T1
    d = c; c = b; b = a; a = T1 + T2

}
			`),
			expected: strings.TrimSpace(`
// sha512 — SHA-512 安全哈希算法（512-bit）
//
// 處理一個 1024-bit（16 個 64-bit 字）區塊。
// 多區塊訊息需由呼叫者自行填充和累加。
//
// 用法：
//   h0 = 7640891576956012808;  h1 = -4942790177534073029
//   h2 = 4354685564936845355;  h3 = -6534734903238641935
//   h4 = 5840696475078001361;  h5 = -7276294671716946913
//   h6 = 2270897969802886507;  h7 = 6620516959819538809
//   sha512(block, 16, h0, h1, h2, h3, h4, h5, h6, h7)

// sha512: 處理一個 1024-bit 區塊
// s: 16 個 64-bit 字 (big-endian)
// n: 固定 16
// h0..h7: 輸入/輸出 512-bit 哈希狀態
sha512 = (s str, n i64, h0 i64, h1 i64, h2 i64, h3 i64, h4 i64, h5 i64, h6 i64, h7 i64) {
    // 64-bit 全 1 遮罩（用於位元 NOT）
    MASK64 = -1

    // 初始狀態
    a = h0; b = h1; c = h2; d = h3; e = h4; f = h5; g = h6; h = h7

    // 第 0 輪 (K0 = 0x428A2F98D728AE22)
    S1 = ((e >> 14) | (e << 50)); S1 = S1 ^ ((e >> 18) | (e << 46)) ^ ((e >> 41) | (e << 23))
    Ch = (e & f) ^ ((MASK64 ^ e) & g)
    S0 = ((a >> 28) | (a << 36)); S0 = S0 ^ ((a >> 34) | (a << 30)) ^ ((a >> 39) | (a << 25))
    Maj = (a & b) ^ (a & c) ^ (b & c)
    T1 = h + S1 + Ch + 4794697086780616226 + s[0]
    T2 = S0 + Maj
    h = g; g = f; f = e; e = d + T1
    d = c; c = b; b = a; a = T1 + T2

    // 第 1 輪 (K1 = 0x7137449123EF65CD)
    S1 = ((e >> 14) | (e << 50)); S1 = S1 ^ ((e >> 18) | (e << 46)) ^ ((e >> 41) | (e << 23))
    Ch = (e & f) ^ ((MASK64 ^ e) & g)
    S0 = ((a >> 28) | (a << 36)); S0 = S0 ^ ((a >> 34) | (a << 30)) ^ ((a >> 39) | (a << 25))
    Maj = (a & b) ^ (a & c) ^ (b & c)
    T1 = h + S1 + Ch + 8158064640168781261 + s[1]
    T2 = S0 + Maj
    h = g; g = f; f = e; e = d + T1
    d = c; c = b; b = a; a = T1 + T2
}
`),
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
			input: strings.TrimSpace(`

// aes-128-dec: 解密一個 16-byte 區塊
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
    for i < 16 {
        out[i] = in[i]
        i = i + 1
    }

    // 初始 AddRoundKey（輪 10）
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    for round > 0 {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    }

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}
            `),
			expected: strings.TrimSpace(`

// aes-128-dec: 解密一個 16-byte 區塊
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
    for i < 16 {
        out[i] = in[i]
        i = i + 1
    }

    // 初始 AddRoundKey（輪 10）
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    for round > 0 {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    }

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}
            `),
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
