// md5-bench — Go MD5 基準測試（標準庫）
//
// 測量 100,000 次 MD5（"abc"，每次修改 input[2] 防止死代碼消除）
//
// 編譯：go build -ldflags="-s -w" -o md5-bench_go .
// 執行：./md5-bench_go

package main

import (
	"crypto/md5"
	"fmt"
)

func main() {
	input := []byte("abc")
	var result [16]byte

	for i := 0; i < 100000; i++ {
		input[2] = byte(i & 255)
		result = md5.Sum(input)
	}

	// 輸出以防止死代碼消除
	fmt.Println(result[0])
}
