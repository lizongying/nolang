package main

import (
	"errors"
	"testing"
)

// TestResolvedDepVersion 驗證 no update / update-all 回寫的版本解析：
// AST 依賴鍵查 releases/latest 得到具體 tag；其餘鍵維持 "*"。
func TestResolvedDepVersion(t *testing.T) {
	var gotHost, gotOwner, gotRepo string
	fakeLatest := func(host, owner, repo string) (string, error) {
		gotHost, gotOwner, gotRepo = host, owner, repo
		return "v0.4.29", nil
	}

	cases := []struct {
		key     string
		want    string
		wantHit bool // 是否應呼叫 latest(host,owner,repo)
	}{
		// add 寫進 manifest 的 AST 依賴鍵形式：<tool>@<host>/<owner>/<repo>
		{"glossary@github.com/lizongying/nolang", "v0.4.29", true},
		{"nogit@github.com/lizongying/notools", "v0.4.29", true},
		// 本地路徑與 workspace / 普通原始碼依賴：沒有單一 release tag，維持 "*"
		{"/test", "*", false},
		{"github.com/lizongying/nolang/test2", "*", false},
		{"test2", "*", false},
	}

	for _, c := range cases {
		gotHost, gotOwner, gotRepo = "", "", ""
		got, err := resolvedDepVersion(c.key, fakeLatest)
		if err != nil {
			t.Fatalf("resolvedDepVersion(%q): unexpected error: %v", c.key, err)
		}
		if got != c.want {
			t.Errorf("resolvedDepVersion(%q) = %q, want %q", c.key, got, c.want)
		}
		hit := gotHost != "" || gotOwner != "" || gotRepo != ""
		if hit != c.wantHit {
			t.Errorf("resolvedDepVersion(%q): latest called=%v (host=%q owner=%q repo=%q), want called=%v",
				c.key, hit, gotHost, gotOwner, gotRepo, c.wantHit)
		}
		if c.wantHit && (gotHost != "github.com" || gotOwner != "lizongying") {
			t.Errorf("resolvedDepVersion(%q): parsed host/owner = %q/%q, want github.com/lizongying",
				c.key, gotHost, gotOwner)
		}
	}
}

// TestResolvedDepVersionRepo 驗證 repo 段取自 AST 鍵（glossary 落在 nolang，不是工具名）。
func TestResolvedDepVersionRepo(t *testing.T) {
	var gotRepo string
	got, err := resolvedDepVersion("glossary@github.com/lizongying/nolang",
		func(_, _, repo string) (string, error) {
			gotRepo = repo
			return "v0.4.30", nil
		})
	if err != nil || got != "v0.4.30" || gotRepo != "nolang" {
		t.Fatalf("got version=%q repo=%q err=%v; want v0.4.30 / nolang / nil", got, gotRepo, err)
	}
}

// TestResolvedDepVersionPropagatesError 驗證 latest 查詢失敗時不吞錯（update 不寫回）。
func TestResolvedDepVersionPropagatesError(t *testing.T) {
	wantErr := errors.New("rate limited")
	_, err := resolvedDepVersion("glossary@github.com/lizongying/nolang",
		func(_, _, _ string) (string, error) { return "", wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
