// Fibonacci — Go 版本，純計算無 I/O（附編譯屏障）
// 編譯: go build -ldflags="-s -w" -o test/bench/fib_go_noprint test/bench/fib_noprint.go

package main

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
	var result int64 = 0
	// 全域變數防止編譯器優化
	for iter := 0; iter < 10000000; iter++ {
		if iter < 5000000 {
			fib(40, &result)
		} else {
			fib(41, &result)
		}
	}
	GlobalSink = result
}

var GlobalSink int64

