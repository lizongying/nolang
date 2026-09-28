package checker

import "testing"

// fireListDir 返回源码中是否有被本 lint 命中的函数。
func fireListDir(t *testing.T, src string) bool {
	t.Helper()
	res := ValidateListDirUnfiltered(parseProg(t, src))
	return len(res) > 0
}

func TestListDirUnfiltered_Fires(t *testing.T) {
	cases := map[string]string{
		// list-dir + 递归 + 无过滤：remove-tree 事故原型
		"recursive_no_filter": `buggy = (p str) {
    kids = fs.list-dir(p)
    fs.remove(kids)
    buggy(p)
}`,
		// list-dir + 破坏性删除(rmdir) + 无递归：仍致命
		"destructive_no_filter": `d1 = (p str) {
    kids = fs.list-dir(p)
    fs.rmdir(p)
}`,
		// 裸 list-dir（std 内无前缀调用）+ 删除
		"bare_listdir_destructive": `d2 = (p str) {
    kids = list-dir(p)
    remove(p)
}`,
	}
	for name, src := range cases {
		if !fireListDir(t, src) {
			t.Errorf("%s: 期望命中，但未报", name)
		}
	}
}

func TestListDirUnfiltered_NoFalsePositive(t *testing.T) {
	cases := map[string]string{
		// 显式过滤 "."/".."，即便递归+删除也安全
		"filtered_recursive": `good = (p str) {
    kids = fs.list-dir(p)
    name = kids[0]
    skip = name == '.' || name == '..'
    ! skip -> good(p)
}`,
		// 用 dir-entries（已过滤），非 list-dir
		"uses_dir_entries": `ok2 = (p str) {
    kids = fs.dir-entries(p)
    ok2(p)
}`,
		// list-dir 但不递归也不删除：只读遍历
		"read_only": `ls1 = (p str) {
    kids = fs.list-dir(p)
    print(kids)
}`,
	}
	for name, src := range cases {
		if fireListDir(t, src) {
			t.Errorf("%s: 期望不报，却命中", name)
		}
	}
}
