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
		// read-dir（裸 readdir，同样含 "."/".."）+ 递归 + 无过滤
		"read_dir_recursive": `r1 = (p str) {
    dp = fs.open-dir(p)
    name, ok = fs.read-dir(dp)
    r1(p)
}`,
		// read-dir + 破坏性删除 + 无过滤
		"read_dir_destructive": `r2 = (p str) {
    dp = fs.open-dir(p)
    name, ok = fs.read-dir(dp)
    fs.rmdir(p)
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
		// read-dir 但非递归非删除（仅拼名字）：不致命
		"read_dir_norisk": `lb = (p str) (names str) {
    dp = fs.open-dir(p)
    name, ok = fs.read-dir(dp)
    names = names - name - '\n'
}`,
		// read-dir + 递归，但用字符串字面量跳过 "."/".."
		"read_dir_filtered_str": `gr = (p str) {
    dp = fs.open-dir(p)
    name, ok = fs.read-dir(dp)
    skip = name == '.' || name == '..'
    ! skip -> gr(p)
}`,
		// read-dir + 递归，但按字节值 46（'.'）跳过——main.add-directory-recursive 形态
		"read_dir_filtered_byte": `gb = (p str) {
    dp = fs.open-dir(p)
    name, ok = fs.read-dir(dp)
    name[0] == 46 -> gb(p)
}`,
	}
	for name, src := range cases {
		if fireListDir(t, src) {
			t.Errorf("%s: 期望不报，却命中", name)
		}
	}
}
