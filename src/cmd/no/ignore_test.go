package main

import (
	"path/filepath"
	"testing"
)

// no test <dir> 必須從 <dir> 自己找 package.noml，不能退回上層；
// 否則 ignore 列表讀不到，*_skip.no 會在路徑命名檢查時才被誤報。
func TestPackageDirForInput(t *testing.T) {
	dir := filepath.Join("tmp", "ignore-probe")
	file := filepath.Join(dir, "tests", "good.no")
	if got := packageDirForInput(dir, true); got != dir {
		t.Errorf("dir input: got %q, want %q", got, dir)
	}
	if got := packageDirForInput(file, false); got != filepath.Dir(file) {
		t.Errorf("file input: got %q, want %q", got, filepath.Dir(file))
	}
}
