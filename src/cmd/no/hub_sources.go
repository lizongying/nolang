package main

// Client-side nohub source list. A "source" is a nohub gh-pages site, e.g.
// https://lizongying.github.io/nohub. We read its root index.noml — the same
// file web/app.js loads for its first screen — and parse the `packages` array
// (one latest-version entry per package, with list fields only). No tarball,
// no per-package package.noml fetch, no generated data.js.
//
// Source list resolution order:
//
//	1. $NOHUB_SOURCES  (comma/newline separated base URLs)
//	2. ~/no/sources.noml (or $NOHUB_SOURCES_FILE)
//	3. default public site (hubDefaultSource)
//
// A source may also be a local directory containing a nohub checkout; we then
// read <dir>/index.noml. That is useful for offline dev and the wasip1 build.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pkg "github.com/lizongying/nolang/package"
)

const (
	hubSourcesEnv     = "NOHUB_SOURCES"
	hubSourcesFileEnv = "NOHUB_SOURCES_FILE"
)

// hubSource is one nohub instance. Ref is either a gh-pages base URL or a
// local directory containing an index.noml.
type hubSource struct {
	Name string
	Ref  string
}

// hubIndexSet is the merged view of all successfully loaded sources. Indexes
// stay separate (so `-source` can select one) while packages carry SourceName.
type hubIndexSet struct {
	Indexes  []*hubIndex
	Names    []string // loaded source names, in priority order
	Origin   string   // where the source list came from (diagnostics only)
	Warnings []string // per-source load failures
}

func (s *hubIndexSet) packages() []hubPackage {
	var out []hubPackage
	for _, idx := range s.Indexes {
		out = append(out, idx.Packages...)
	}
	return out
}

// subset returns the indexes matching name (case-insensitive). An empty name
// selects everything.
func (s *hubIndexSet) subset(name string) *hubIndexSet {
	if name == "" {
		return s
	}
	out := &hubIndexSet{Origin: s.Origin, Warnings: s.Warnings}
	for _, idx := range s.Indexes {
		if strings.EqualFold(idx.SourceName, name) || strings.EqualFold(idx.SourceRef, name) {
			out.Indexes = append(out.Indexes, idx)
			out.Names = append(out.Names, idx.SourceName)
		}
	}
	return out
}

// hubParseSourceSpec parses one "name = url" entry. A bare URL is allowed and
// is also used as the display name.
func hubParseSourceSpec(spec string) hubSource {
	spec = strings.TrimSpace(strings.Trim(spec, "'\""))
	if spec == "" {
		return hubSource{}
	}
	if i := strings.Index(spec, "="); i > 0 {
		name := strings.TrimSpace(spec[:i])
		ref := strings.TrimSpace(spec[i+1:])
		if name != "" && ref != "" && !strings.ContainsAny(name, "/:@\\") {
			return hubSource{Name: name, Ref: ref}
		}
	}
	return hubSource{Name: spec, Ref: spec}
}

// hubParseSourcesEnv parses $NOHUB_SOURCES: comma- and newline-separated base
// URLs or "name = url" specs.
func hubParseSourcesEnv(raw string) []hubSource {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	out := make([]hubSource, 0, len(parts))
	for _, part := range parts {
		s := hubParseSourceSpec(part)
		if s.Name != "" && s.Ref != "" {
			out = append(out, s)
		}
	}
	return out
}

// hubParseSourcesConfig parses ~/no/sources.noml. Canonical form is an object
// with a `sources` array of strings (order = priority):
//
//	{
//	  sources: [
//	    'https://lizongying.github.io/nohub',
//	    'corp = https://intranet.example/nohub',
//	  ]
//	}
//
// Each entry may also be an object (noml members are newline-separated):
//
//	{
//	  name: 'mirror'
//	  url: 'https://mirror.example/nohub'
//	}
func hubParseSourcesConfig(data []byte) ([]hubSource, error) {
	v, err := pkg.ParseNoml(data)
	if err != nil {
		return nil, fmt.Errorf("nohub sources: %w", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("nohub sources: expected an object with a `sources` array")
	}
	raw, ok := m["sources"]
	if !ok {
		return nil, fmt.Errorf("nohub sources: missing `sources` array")
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("nohub sources: `sources` must be an array")
	}
	out := make([]hubSource, 0, len(arr))
	for i, item := range arr {
		var src hubSource
		switch x := item.(type) {
		case string:
			src = hubParseSourceSpec(x)
		case map[string]any:
			name, _ := x["name"].(string)
			ref, _ := x["url"].(string)
			if ref == "" {
				ref, _ = x["repo"].(string)
			}
			src = hubSource{Name: name, Ref: ref}
		default:
			return nil, fmt.Errorf("nohub sources: entry %d has unsupported type %T", i, item)
		}
		if src.Ref == "" {
			return nil, fmt.Errorf("nohub sources: entry %d has no URL", i)
		}
		if src.Name == "" {
			src.Name = src.Ref
		}
		out = append(out, src)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("nohub sources: empty source list")
	}
	return out, nil
}

func hubSourcesConfigPath() string {
	if p := strings.TrimSpace(os.Getenv(hubSourcesFileEnv)); p != "" {
		return p
	}
	return filepath.Join(pkg.NoHomeDir(), "sources.noml")
}

// loadHubSources resolves the source list. The returned string describes the
// origin and is used in diagnostics only.
func loadHubSources() ([]hubSource, string, error) {
	if raw := strings.TrimSpace(os.Getenv(hubSourcesEnv)); raw != "" {
		sources := hubParseSourcesEnv(raw)
		if len(sources) == 0 {
			return nil, "", fmt.Errorf("nohub sources: $%s has no valid entries", hubSourcesEnv)
		}
		return sources, "$" + hubSourcesEnv, nil
	}
	path := hubSourcesConfigPath()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		sources, err := hubParseSourcesConfig(data)
		if err != nil {
			return nil, "", err
		}
		return sources, path, nil
	case os.IsNotExist(err):
		return []hubSource{{Name: hubDefaultSource, Ref: hubDefaultSource}}, "default", nil
	default:
		return nil, "", fmt.Errorf("nohub sources: read %s: %w", path, err)
	}
}

// hubLoadIndexData reads a source's root index.noml: from disk for a local
// directory, otherwise over HTTP from the gh-pages base URL. A local nohub
// checkout keeps the file at <dir>/web/index.noml; a gh-pages deployment puts
// it at the root. Both layouts are accepted.
func hubLoadIndexData(ref string) ([]byte, error) {
	if fi, err := os.Stat(ref); err == nil && fi.IsDir() {
		for _, rel := range []string{"index.noml", filepath.Join("web", "index.noml")} {
			if data, err := os.ReadFile(filepath.Join(ref, rel)); err == nil {
				return data, nil
			}
		}
		return nil, fmt.Errorf("nohub source %s: no index.noml found (looked in root and web/)", ref)
	}
	if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
		return nil, fmt.Errorf("nohub source %q: expected an http(s) URL or a local directory", ref)
	}
	return hubFetchIndex(ref)
}

func hubNomlString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func hubNomlStrings(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func hubNomlBool(m map[string]any, key string, def bool) bool {
	b, ok := m[key].(bool)
	if !ok {
		return def
	}
	return b
}

// hubBuildPackageFromIndex mirrors web/app.js recordFromIndexEntry: an
// index.noml entry already carries the latest repo version and list fields;
// the single `repo` field is split back into host/owner/repo.
func hubBuildPackageFromIndex(m map[string]any, sourceName string) hubPackage {
	name := hubNomlString(m, "name")
	repoKey := hubNomlString(m, "repo")
	version := hubNomlString(m, "version")
	tag := hubNomlString(m, "tag")
	if tag == "" {
		tag = version
	}

	parts := strings.Split(repoKey, "/")
	host, owner, repo := "", "", ""
	if len(parts) == 3 {
		host, owner, repo = parts[0], parts[1], parts[2]
	}

	repository := hubNomlString(m, "repository")
	docsURL := hubNomlString(m, "docs")
	if docsURL == "" {
		docsURL = hubNomlString(m, "homepage")
	}
	if docsURL == "" {
		docsURL = normalizeHubRepoURL(repository)
	}

	return hubPackage{
		Name:         name,
		Version:      version,
		Description:  hubNomlString(m, "description"),
		Keywords:     hubNomlStrings(m, "keywords"),
		Author:       hubNomlString(m, "author"),
		Email:        hubNomlString(m, "email"),
		Organization: hubNomlString(m, "organization"),
		Repository:   repository,
		RepoURL:      normalizeHubRepoURL(repository),
		Homepage:     hubNomlString(m, "homepage"),
		License:      hubNomlString(m, "license"),
		Category:     hubNomlString(m, "category"),
		Docs:         hubNomlString(m, "docs"),
		DocsURL:      docsURL,
		Readme:       hubNomlString(m, "readme"),
		Publish:      hubNomlBool(m, "publish", true),
		Host:         host,
		Owner:        owner,
		Repo:         repo,
		RepoVersion:  version,
		RepoKey:      repoKey,
		Tag:          tag,
		SourceName:   sourceName,
	}
}

// hubBuildIndex parses a root index.noml into the index. Sorting matches
// web/build.mjs buildManifest: name asc, then repo (host/owner/repo) asc.
func hubBuildIndex(data []byte, source hubSource) (*hubIndex, error) {
	v, err := pkg.ParseNoml(data)
	if err != nil {
		return nil, fmt.Errorf("source %s: parse index.noml: %w", source.Name, err)
	}
	root, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("source %s: index.noml: expected an object", source.Name)
	}
	raw, ok := root["packages"]
	if !ok {
		return nil, fmt.Errorf("source %s: index.noml: missing `packages` array", source.Name)
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("source %s: index.noml: `packages` must be an array", source.Name)
	}

	records := make([]hubPackage, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok || hubNomlString(m, "name") == "" {
			continue
		}
		records = append(records, hubBuildPackageFromIndex(m, source.Name))
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("source %s: index.noml has no packages", source.Name)
	}

	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.RepoKey < b.RepoKey
	})

	return &hubIndex{
		GeneratedAt: hubNomlString(root, "generated_at"),
		Source:      hubNomlString(root, "source"),
		Count:       len(records),
		Packages:    records,
		SourceName:  source.Name,
		SourceRef:   source.Ref,
	}, nil
}

// loadHubIndexes fetches each source independently. A failing source is
// recorded as a warning and does not block the others; only when every source
// fails does the command error out.
func loadHubIndexes() (*hubIndexSet, error) {
	sources, origin, err := loadHubSources()
	if err != nil {
		return nil, err
	}
	set := &hubIndexSet{Origin: origin}
	for _, src := range sources {
		data, err := hubLoadIndexData(src.Ref)
		if err != nil {
			set.Warnings = append(set.Warnings, fmt.Sprintf("source %s: %v", src.Name, err))
			continue
		}
		idx, err := hubBuildIndex(data, src)
		if err != nil {
			set.Warnings = append(set.Warnings, fmt.Sprintf("source %s: %v", src.Name, err))
			continue
		}
		set.Indexes = append(set.Indexes, idx)
		set.Names = append(set.Names, src.Name)
	}
	if len(set.Indexes) == 0 {
		return nil, fmt.Errorf("no nohub source could be loaded (from %s): %s", origin, strings.Join(set.Warnings, "; "))
	}
	return set, nil
}

// printHubWarnings reports skipped sources on stderr so a partial result is
// never silently mistaken for a complete one.
func printHubWarnings(set *hubIndexSet) {
	for _, w := range set.Warnings {
		fmt.Fprintf(os.Stderr, "no hub: warning: %s\n", w)
	}
}
