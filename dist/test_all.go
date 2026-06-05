package main

import (
	"fmt"
)

func add(a int, b int) {
	var result = (a + b)
	fmt.Println("result:", result)
}
func add(a int, b int) (c int, d int) {
	result = (a + b)
	fmt.Println("result:", result)
}
func main() {
	var x = 1
	var y = 1
	var name = "hello"
	var flag = true
	var a int8 = 2
	var foo_bar = 42
	var hello_world = "Hello World"
	var sum = 0
	var i = 0
	for (i < 5) {
		sum = (sum + i)
		i = (i + 1)
	}	fmt.Println("sum:", sum)
	var arr = [3]int{1, 2, 3}
	var slice = []int{4, 5, 6}
	fmt.Println("array:", arr)
	fmt.Println("slice:", slice)
	var max = func() interface{} { if (sum > 10) { return sum }; return 10 }()
	fmt.Println("max:", max)
	add(10, 20)
	f(5, 0)
	f(a, b)
	for 	i = 0
;(i < 5);	(i ++ )
 {
		sum = (sum + i)
		i = (i + 1)
	}
}