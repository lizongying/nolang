package main

import (
	"strings"
	"testing"
)

// hubTestPackages is the record set produced from package.noml manifests,
// mirroring what hubBuildIndex returns.
func hubTestPackages() []hubPackage {
	return []hubPackage{
		{
			Name: "glossary", Version: "v0.1.0", Description: "Nolang 词条化知识库",
			Keywords: []string{"nolang", "mcp", "knowledge-base"}, Category: "knowledge-base",
			Author: "lizongying", License: "Apache-2.0",
			Repository: "https://github.com/lizongying/nolang", Homepage: "",
			Docs: "", DocsURL: "https://github.com/lizongying/nolang/tree/main/glossary",
			Host: "github.com", Owner: "lizongying", Repo: "nolang",
			RepoVersion: "v0.4.28", RepoKey: "github.com/lizongying/nolang",
			Publish: true, LatestCommit: "abc123", SourceName: "public",
		},
		{
			Name: "glossary", Version: "v0.1.0", Description: "older snapshot",
			Keywords: []string{}, Category: "knowledge-base", Host: "github.com",
			Owner: "lizongying", Repo: "nolang", RepoVersion: "v0.4.20",
			RepoKey: "github.com/lizongying/nolang", Publish: true, SourceName: "public",
		},
		{
			Name: "nogit", Version: "v0.1.35", Description: "Unix cat/ls/grep tools",
			Keywords: []string{"cli", "unix"}, Category: "tools", Author: "lizongying",
			Repository: "git@github.com:lizongying/notools.git", Homepage: "https://example.org/nogit",
			Docs: "https://example.org/docs", Host: "github.com", Owner: "lizongying",
			Repo: "notools", RepoVersion: "v0.1.35", RepoKey: "github.com/lizongying/notools",
			Publish: true, SourceName: "public",
		},
		{
			Name: "hidden", Description: "unpublished", Keywords: []string{},
			Repository: "", Host: "github.com", Owner: "o", Repo: "r",
			RepoVersion: "v1.0.0", RepoKey: "github.com/o/r", Publish: false, SourceName: "public",
		},
	}
}

func TestGroupHubPackagesLatestVersion(t *testing.T) {
	groups := groupHubPackages(hubTestPackages())
	if len(groups) != 3 {
		t.Fatalf("groups=%d, want 3 (glossary + nogit + hidden)", len(groups))
	}
	var glossary *hubGroup
	for _, g := range groups {
		if g.Name == "glossary" {
			glossary = g
		}
	}
	if glossary == nil {
		t.Fatal("glossary group missing")
	}
	if len(glossary.Versions) != 2 {
		t.Fatalf("glossary versions=%d, want 2", len(glossary.Versions))
	}
	if glossary.Latest.RepoVersion != "v0.4.28" {
		t.Fatalf("latest = %q, want v0.4.28", glossary.Latest.RepoVersion)
	}
}

func TestHubSearch(t *testing.T) {
	groups := groupHubPackages(hubTestPackages())

	hits := hubSearch(groups, "mcp", "")
	if len(hits) != 1 || hits[0].Name != "glossary" {
		t.Fatalf("keyword search = %+v, want glossary", hits)
	}

	// Multi-token search is AND across tokens: "nolang" matches glossary's
	// keyword, "unix" matches nogit's keyword — no single package has both.
	hits = hubSearch(groups, "nolang unix", "")
	if len(hits) != 0 {
		t.Fatalf("multi-token AND search = %d hits, want 0", len(hits))
	}

	hits = hubSearch(groups, "", "tools")
	if len(hits) != 1 || hits[0].Name != "nogit" {
		t.Fatalf("category filter = %+v, want nogit", hits)
	}

	// Unpublished packages are filtered out.
	for _, g := range hubSearch(groups, "", "") {
		if g.Name == "hidden" {
			t.Fatal("unpublished package leaked into results")
		}
	}
}

func TestResolveHubDocsURL(t *testing.T) {
	byName := map[string]hubPackage{}
	for _, p := range hubTestPackages() {
		if old, ok := byName[p.Name]; !ok || compareHubVersions(p.RepoVersion, old.RepoVersion) > 0 {
			byName[p.Name] = p
		}
	}
	if got := resolveHubDocsURL(byName["glossary"]); !strings.Contains(got, "glossary") {
		t.Fatalf("glossary docs = %q", got)
	}
	if got := resolveHubDocsURL(byName["nogit"]); got != "https://example.org/docs" {
		t.Fatalf("nogit docs fallback = %q, want explicit docs", got)
	}
}

func TestNormalizeHubRepoURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"git@github.com:lizongying/notools.git", "https://github.com/lizongying/notools"},
		{"git@github.com:o/r", "https://github.com/o/r"},
		{"github.com/o/r", "https://github.com/o/r"},
		{"https://github.com/o/r.git", "https://github.com/o/r"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeHubRepoURL(c.in); got != c.want {
			t.Errorf("normalizeHubRepoURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCompareHubVersions(t *testing.T) {
	if compareHubVersions("v0.4.28", "v0.4.20") <= 0 {
		t.Error("v0.4.28 should be greater than v0.4.20")
	}
	if compareHubVersions("v0.4.3", "v0.4.28") >= 0 {
		t.Error("v0.4.3 should be less than v0.4.28")
	}
	if compareHubVersions("1.2", "1.2.0") != 0 {
		t.Error("1.2 and 1.2.0 should compare equal")
	}
}
