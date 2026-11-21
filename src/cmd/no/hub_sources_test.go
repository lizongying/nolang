package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHubIndexURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://lizongying.github.io/nohub", "https://lizongying.github.io/nohub/index.noml"},
		{"https://lizongying.github.io/nohub/", "https://lizongying.github.io/nohub/index.noml"},
		{"https://corp.example/nohub/index.noml", "https://corp.example/nohub/index.noml"},
	}
	for _, c := range cases {
		if got := hubIndexURL(c.in); got != c.want {
			t.Errorf("hubIndexURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseSourcesConfig(t *testing.T) {
	data := []byte(`{
  sources: [
    'https://lizongying.github.io/nohub',
    'corp = https://intranet.example/nohub',
    {
      name: 'mirror'
      url: 'https://mirror.example/nohub'
    },
  ]
}`)
	sources, err := hubParseSourcesConfig(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sources) != 3 {
		t.Fatalf("sources=%d, want 3", len(sources))
	}
	if sources[0].Name != hubDefaultSource || sources[0].Ref != hubDefaultSource {
		t.Fatalf("source 0 = %+v", sources[0])
	}
	if sources[1].Name != "corp" || sources[1].Ref != "https://intranet.example/nohub" {
		t.Fatalf("source 1 = %+v", sources[1])
	}
	if sources[2].Name != "mirror" || sources[2].Ref != "https://mirror.example/nohub" {
		t.Fatalf("source 2 = %+v", sources[2])
	}
	if _, err := hubParseSourcesConfig([]byte(`{ sources: [] }`)); err == nil {
		t.Fatal("empty source list should be an error")
	}
}

func TestParseSourcesEnv(t *testing.T) {
	raw := "https://lizongying.github.io/nohub,\ncorp=https://intranet.example/nohub"
	sources := hubParseSourcesEnv(raw)
	if len(sources) != 2 {
		t.Fatalf("sources=%d, want 2: %+v", len(sources), sources)
	}
	if sources[0].Ref != hubDefaultSource {
		t.Fatalf("source 0 = %+v", sources[0])
	}
	if sources[1].Name != "corp" || sources[1].Ref != "https://intranet.example/nohub" {
		t.Fatalf("source 1 = %+v", sources[1])
	}
}

func TestLoadHubSourcesPrecedence(t *testing.T) {
	t.Setenv(hubSourcesEnv, "")
	missing := filepath.Join(t.TempDir(), "no-such-sources.noml")
	t.Setenv(hubSourcesFileEnv, missing)

	// No config file and no env: built-in public site.
	sources, origin, err := loadHubSources()
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if origin != "default" || len(sources) != 1 || sources[0].Ref != hubDefaultSource {
		t.Fatalf("default = %+v / %q", sources, origin)
	}

	// Config file is used when present.
	cfg := filepath.Join(t.TempDir(), "sources.noml")
	if err := os.WriteFile(cfg, []byte("{\n  sources: [\n    'https://corp.example/nohub',\n  ]\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(hubSourcesFileEnv, cfg)
	sources, origin, err = loadHubSources()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if origin != cfg || len(sources) != 1 || sources[0].Ref != "https://corp.example/nohub" {
		t.Fatalf("config = %+v / %q", sources, origin)
	}

	// $NOHUB_SOURCES wins over the config file.
	t.Setenv(hubSourcesEnv, "alpha=https://a.example/nohub, beta=https://b.example/nohub")
	sources, origin, err = loadHubSources()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if origin != "$"+hubSourcesEnv || len(sources) != 2 || sources[0].Name != "alpha" || sources[1].Name != "beta" {
		t.Fatalf("env = %+v / %q", sources, origin)
	}
}

func TestHubIndexSetSubset(t *testing.T) {
	set := &hubIndexSet{
		Indexes: []*hubIndex{{SourceName: "public", SourceRef: hubDefaultSource}, {SourceName: "corp", SourceRef: "https://corp.example/nohub"}},
		Names:   []string{"public", "corp"},
	}
	if got := len(set.subset("").Indexes); got != 2 {
		t.Fatalf("empty selection = %d, want 2", got)
	}
	sub := set.subset("CORP")
	if len(sub.Indexes) != 1 || sub.Indexes[0].SourceName != "corp" || len(sub.Names) != 1 {
		t.Fatalf("subset = %+v", sub)
	}
	// Selecting by full URL works too.
	if got := len(set.subset(hubDefaultSource).Indexes); got != 1 {
		t.Fatalf("subset by URL = %d, want 1", got)
	}
	if len(set.subset("missing").Indexes) != 0 {
		t.Fatal("unknown source should match nothing")
	}
}

func TestBuildIndexFromIndexNoml(t *testing.T) {
	data := []byte(`; generated
{
  generated_at: '2026-11-21T12:00:00Z'
  source: 'nohub packages/ 目录快照'
  packages: [
    {
      name: 'glossary'
      tag: 'v0.4.28'
      repo: 'github.com/lizongying/nolang'
      version: 'v0.4.28'
      description: 'Nolang knowledge base'
      keywords: ['nolang', 'mcp']
      category: ''
      license: 'Apache-2.0'
      author: 'lizongying'
      email: 'a@b.c'
      repository: 'https://github.com/lizongying/nolang'
      publish: true
    },
    {
      name: 'nogit'
      tag: 'v0.1.35'
      repo: 'github.com/lizongying/notools'
      version: 'v0.1.35'
      description: 'Git in Nolang'
      keywords: []
      repository: 'git@github.com:lizongying/notools.git'
      publish: false
    },
  ]
}`)
	idx, err := hubBuildIndex(data, hubSource{Name: "public", Ref: hubDefaultSource})
	if err != nil {
		t.Fatal(err)
	}
	if idx.Count != 2 || len(idx.Packages) != 2 {
		t.Fatalf("count=%d, packages=%d", idx.Count, len(idx.Packages))
	}
	// Sorted by name: glossary then nogit.
	g := idx.Packages[0]
	if g.Name != "glossary" || g.Version != "v0.4.28" || g.RepoVersion != "v0.4.28" {
		t.Fatalf("glossary = %+v", g)
	}
	if g.Host != "github.com" || g.Owner != "lizongying" || g.Repo != "nolang" || g.RepoKey != "github.com/lizongying/nolang" {
		t.Fatalf("repo split = %+v", g)
	}
	if g.Tag != "v0.4.28" || g.License != "Apache-2.0" || len(g.Keywords) != 2 {
		t.Fatalf("index fields = %+v", g)
	}
	if g.DocsURL != "https://github.com/lizongying/nolang" {
		t.Fatalf("docs fallback = %q", g.DocsURL)
	}
	n := idx.Packages[1]
	if n.Name != "nogit" || n.Publish {
		t.Fatalf("nogit publish should be false: %+v", n)
	}
	if n.DocsURL != "https://github.com/lizongying/notools" {
		t.Fatalf("git@ docs normalize = %q", n.DocsURL)
	}
}

func TestBuildIndexFromLocalDir(t *testing.T) {
	dir := t.TempDir()
	data := []byte("{\n  packages: [\n    {\n      name: 'local'\n      repo: 'github.com/o/r'\n      version: 'v1.0.0'\n    },\n  ]\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "index.noml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := hubLoadIndexData(dir)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := hubBuildIndex(raw, hubSource{Name: "local", Ref: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Packages) != 1 || idx.Packages[0].Name != "local" {
		t.Fatalf("local index = %+v", idx.Packages)
	}
	// publish defaults to true when omitted.
	if !idx.Packages[0].Publish {
		t.Fatal("publish should default to true")
	}
}

func TestBuildIndexRejectsOldSnapshotsFormat(t *testing.T) {
	data := []byte("{\n  snapshots: [\n    { name: 'x' },\n  ]\n}\n")
	if _, err := hubBuildIndex(data, hubSource{Name: "old"}); err == nil {
		t.Fatal("old snapshots format should be rejected")
	}
}

func TestGroupHubPackagesCrossSourceDedupe(t *testing.T) {
	pkgs := []hubPackage{
		{Name: "wid", RepoVersion: "v1.0.0", RepoKey: "github.com/o/wid", SourceName: "public", Description: "from public"},
		{Name: "wid", RepoVersion: "v1.0.0", RepoKey: "github.com/o/wid", SourceName: "corp", Description: "from corp"},
		{Name: "wid", RepoVersion: "v0.9.0", RepoKey: "github.com/o/wid", SourceName: "corp", Description: "old"},
	}
	groups := groupHubPackages(pkgs)
	if len(groups) != 1 {
		t.Fatalf("groups=%d, want 1", len(groups))
	}
	g := groups[0]
	if len(g.Versions) != 2 {
		t.Fatalf("versions=%d, want 2 (deduped v1.0.0 + v0.9.0)", len(g.Versions))
	}
	if g.Latest.SourceName != "public" || g.Latest.Description != "from public" {
		t.Fatalf("latest should come from first (highest-priority) source: %+v", g.Latest)
	}
}

func TestHubParseSourceFlag(t *testing.T) {
	cases := []struct {
		in         []string
		wantSource string
		wantRest   []string
		wantErr    bool
	}{
		{[]string{"-source", "corp", "glossary"}, "corp", []string{"glossary"}, false},
		{[]string{"--source=corp", "glossary"}, "corp", []string{"glossary"}, false},
		{[]string{"glossary"}, "", []string{"glossary"}, false},
		{[]string{"-bogus", "x"}, "", nil, true},
	}
	for _, c := range cases {
		source, rest, err := hubParseSourceFlag(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("hubParseSourceFlag(%v) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
		if source != c.wantSource || strings.Join(rest, ",") != strings.Join(c.wantRest, ",") {
			t.Errorf("hubParseSourceFlag(%v) = %q / %v, want %q / %v", c.in, source, rest, c.wantSource, c.wantRest)
		}
	}
}
