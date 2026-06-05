```shell
cd nolang/build && go build -o nolang-build *.go

cd nolang/build && ./nolang-build ../../nocode/test_simple.no  

cd nolang/build && ./nolang-build -v ../../nocode/test_simple.no  

cd nolang/build && go run *.go -v ../../nocode/test_simple.no  
```