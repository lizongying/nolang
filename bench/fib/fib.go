// Fibonacci — Go 版本，與 Nolang 相同引用傳遞模式
// 編譯: go build -o test/bench/fib_go test/bench/fib.go

package main

import (
	"fmt"
)

func fib(n int64, o *int64) {
	var a, b, c int64 = 0, 1, 0
	var i int64 = 2
	for i <= n {
		c = a + b
		a = b
		b = c
		i++
	}
	*o = b
}

func main() {
	for iter := 0; iter < 10000000; iter++ {
		var result int64 = 0
		fib(40, &result)
		fmt.Println(result)
	}
}
