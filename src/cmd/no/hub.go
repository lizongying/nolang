package main

// nohub registry client.
//
// The `no` CLI reads a nohub gh-pages site's root index.noml (e.g.
// https://lizongying.github.io/nohub/index.noml) — the same file web/app.js
// loads for its first screen. Each entry carries the package's latest repo
// version plus list fields (name, tag, repo, description, keywords, ...).
// Three read-only entry points:
//
//	no search [<query>...]   search packages by name / keyword / category
//	no info <pkg>            show a package's metadata
//	no docs <pkg>            print the documentation URL
//
// Bare `no info` keeps its original meaning (compiler / environment info).

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

// errHubNetworkUnavailable is returned by the wasip1 stub; the browser
// playground has no sockets and must use a local-directory source.
var errHubNetworkUnavailable = errors.New("nohub repo fetch not supported in WASM build")

// hubPackage is one package record from a root index.noml entry. Field names
// mirror web/app.js recordFromIndexEntry / web/build.mjs buildManifest.
type hubPackage struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Keywords     []string `json:"keywords"`
	Author       string   `json:"author"`
	Email        string   `json:"email"`
	Organization string   `json:"organization"`
	Repository   string   `json:"repository"`
	RepoURL      string   `json:"repoUrl"`
	Homepage     string   `json:"homepage"`
	License      string   `json:"license"`
	Category     string   `json:"category"`
	Docs         string   `json:"docs"`
	DocsURL      string   `json:"docsUrl"`
	Readme       string   `json:"readme"`
	Publish      bool     `json:"publish"`
	Host         string   `json:"host"`
	Owner        string   `json:"owner"`
	Repo         string   `json:"repo"`
	RepoVersion  string   `json:"repoVersion"`
	RepoKey      string   `json:"repoKey"`
	Tag          string   `json:"tag"`
	SnapshotPath string   `json:"snapshotPath"`
	LatestCommit string   `json:"latestCommit"`

	// SourceName is filled in by the client from the configured source list;
	// it is not part of the nohub index itself.
	SourceName string `json:"-"`
}

type hubIndex struct {
	GeneratedAt string       `json:"generatedAt"`
	Source      string       `json:"source"`
	Count       int          `json:"count"`
	Packages    []hubPackage `json:"packages"`

	SourceName string `json:"-"`
	SourceRef  string `json:"-"`
}

// hubGroup collapses the per-version snapshot records into one entry per
// name@repoKey, mirroring web/app.js. `latest` is the highest repo version.
type hubGroup struct {
	Name     string
	RepoKey  string
	Versions []hubPackage
	Latest   hubPackage
}

// compareHubVersions is a tolerant semantic-version comparison (v prefix and
// differing segment counts allowed), matching web/app.js compareVersions.
// Returns -1/0/1. Descending order uses compareHubVersions(b, a).
func compareHubVersions(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), "v"), ".")
	pb := strings.Split(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(b)), "v"), ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &x)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &y)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	// All numeric segments matched (a missing segment counts as 0), so 1.2 and
	// 1.2.0 compare equal. A non-numeric tail falls through to a stable string
	// tie-break so sorting stays deterministic.
	ta := strings.Trim(strings.Join(pa, "."), "0.")
	tb := strings.Trim(strings.Join(pb, "."), "0.")
	if ta == tb && isHubNumericTail(pa) && isHubNumericTail(pb) {
		return 0
	}
	return strings.Compare(a, b)
}

func isHubNumericTail(parts []string) bool {
	for _, p := range parts {
		if p == "" {
			continue
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// groupHubPackages builds the version-grouped view used by search/info/docs.
func groupHubPackages(pkgs []hubPackage) []*hubGroup {
	byKey := map[string]*hubGroup{}
	for _, p := range pkgs {
		key := p.Name
		if p.RepoKey == "" {
			key += "@" + p.Host + "/" + p.Owner + "/" + p.Repo
		} else {
			key += "@" + p.RepoKey
		}
		g := byKey[key]
		if g == nil {
			g = &hubGroup{Name: p.Name, RepoKey: strings.TrimPrefix(key, p.Name+"@")}
			byKey[key] = g
		}
		g.Versions = append(g.Versions, p)
	}
	out := make([]*hubGroup, 0, len(byKey))
	for _, g := range byKey {
		// The same version can appear in several sources. Packages arrive in
		// source-priority order, so the first occurrence wins (mirrors nohub's
		// "resolve by source priority" rule).
		seen := make(map[string]bool, len(g.Versions))
		unique := g.Versions[:0]
		for _, p := range g.Versions {
			if seen[p.RepoVersion] {
				continue
			}
			seen[p.RepoVersion] = true
			unique = append(unique, p)
		}
		g.Versions = unique
		sort.SliceStable(g.Versions, func(i, j int) bool {
			return compareHubVersions(g.Versions[j].RepoVersion, g.Versions[i].RepoVersion) < 0
		})
		g.Latest = g.Versions[0]
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if c := strings.Compare(out[i].Name, out[j].Name); c != 0 {
			return c < 0
		}
		return out[i].RepoKey < out[j].RepoKey
	})
	return out
}

// hubScoreToken mirrors web/app.js scoreToken: exact name > prefix > substring
// > keyword > free-text fields. Returns 0 when the token does not match at all.
func hubScoreToken(p hubPackage, tok string) int {
	name := strings.ToLower(p.Name)
	if name == tok {
		return 100
	}
	if strings.HasPrefix(name, tok) {
		return 60
	}
	if strings.Contains(name, tok) {
		return 40
	}
	for _, kw := range p.Keywords {
		if strings.Contains(strings.ToLower(kw), tok) {
			return 20
		}
	}
	hay := strings.ToLower(strings.Join([]string{
		p.Description, p.Category, p.Author, p.Email, p.License,
		p.Host, p.Owner, p.Repo, p.Repository,
	}, " "))
	if strings.Contains(hay, tok) {
		return 10
	}
	return 0
}

func hubSearch(groups []*hubGroup, query string, category string) []*hubGroup {
	tokens := strings.Fields(strings.ToLower(query))
	type scored struct {
		g     *hubGroup
		score int
	}
	var hits []scored
	for _, g := range groups {
		p := g.Latest
		if category != "" && !strings.EqualFold(p.Category, category) {
			continue
		}
		if len(tokens) == 0 {
			hits = append(hits, scored{g, 0})
			continue
		}
		total := 0
		ok := true
		for _, tok := range tokens {
			s := hubScoreToken(p, tok)
			if s == 0 {
				ok = false
				break
			}
			total += s
		}
		if ok {
			hits = append(hits, scored{g, total})
		}
	}
	if len(tokens) > 0 {
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score > hits[j].score
			}
			return hits[i].g.Name < hits[j].g.Name
		})
	}
	out := make([]*hubGroup, 0, len(hits))
	for _, h := range hits {
		if !h.g.Latest.Publish {
			continue
		}
		out = append(out, h.g)
	}
	return out
}

// resolveHubDocsURL implements the docs fallback chain: docs -> homepage ->
// repository, using the index's precomputed docsUrl when available.
func resolveHubDocsURL(p hubPackage) string {
	if p.DocsURL != "" {
		return p.DocsURL
	}
	if p.Docs != "" {
		return p.Docs
	}
	if p.Homepage != "" {
		return p.Homepage
	}
	return normalizeHubRepoURL(p.Repository)
}

// normalizeHubRepoURL turns git@/ssh/bare-domain clone URLs into clickable
// https addresses; mirrors web/build.mjs normalizeRepoUrl.
func normalizeHubRepoURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "git@") && strings.Contains(s, ":") {
		parts := strings.SplitN(strings.TrimPrefix(s, "git@"), ":", 2)
		return "https://" + parts[0] + "/" + strings.TrimSuffix(strings.TrimSuffix(parts[1], "/"), ".git")
	}
	if strings.HasPrefix(strings.ToLower(s), "ssh://") {
		rest := strings.TrimPrefix(strings.TrimPrefix(s, "ssh://"), "git@")
		if at := strings.Index(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		if slash := strings.Index(rest, "/"); slash >= 0 {
			host := strings.SplitN(rest[:slash], ":", 2)[0]
			path := rest[slash+1:]
			return "https://" + host + "/" + strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
		}
	}
	if !strings.Contains(s, "://") && strings.Contains(s, ".") && strings.Contains(s, "/") {
		return "https://" + s
	}
	return strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
}

// hubParseSourceFlag extracts -source/--source from the leading flags of any
// registry command. It must run before command-specific flags (-category).
func hubParseSourceFlag(args []string) (string, []string, error) {
	var source string
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch {
		case (args[0] == "-source" || args[0] == "--source") && len(args) > 1:
			source = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "-source="), strings.HasPrefix(args[0], "--source="):
			source = strings.SplitN(args[0], "=", 2)[1]
			args = args[1:]
		default:
			return "", nil, fmt.Errorf("unknown flag %q", args[0])
		}
	}
	return source, args, nil
}

// hubLoadForCommand loads all sources, warns about partial failures, then
// narrows to the user's -source selection (if any).
func hubLoadForCommand(sourceSel, cmd, usage string) *hubIndexSet {
	set, err := loadHubIndexes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "no %s: %v\n", cmd, err)
		os.Exit(1)
	}
	printHubWarnings(set)
	selected := set.subset(sourceSel)
	if sourceSel != "" && len(selected.Indexes) == 0 {
		fmt.Fprintf(os.Stderr, "no %s: unknown source %q (available: %s)\n%s\n",
			cmd, sourceSel, strings.Join(set.Names, ", "), usage)
		os.Exit(2)
	}
	return selected
}

// hubSearchCommand implements `no search [<query>...]`.
func hubSearchCommand(args []string) {
	const usage = "Usage: no search [-source <name>] [-category <cat>] [<query>...]"
	sourceSel, rest, err := hubParseSourceFlag(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no search: %v\n%s\n", err, usage)
		os.Exit(2)
	}
	category := ""
	queryArgs := rest
	for len(queryArgs) > 0 && strings.HasPrefix(queryArgs[0], "-") {
		switch {
		case queryArgs[0] == "-category" && len(queryArgs) > 1:
			category = queryArgs[1]
			queryArgs = queryArgs[2:]
		case strings.HasPrefix(queryArgs[0], "-category="):
			category = strings.TrimPrefix(queryArgs[0], "-category=")
			queryArgs = queryArgs[1:]
		default:
			fmt.Fprintf(os.Stderr, "no search: unknown flag %q\n%s\n", queryArgs[0], usage)
			os.Exit(2)
		}
	}
	set := hubLoadForCommand(sourceSel, "search", usage)
	hits := hubSearch(groupHubPackages(set.packages()), strings.Join(queryArgs, " "), category)
	if len(hits) == 0 {
		fmt.Println("No matching packages.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVERSION\tSOURCE\tCATEGORY\tREPOSITORY\tDESCRIPTION")
	for _, g := range hits {
		p := g.Latest
		repo := p.RepoKey
		if repo == "" {
			repo = p.Host + "/" + p.Owner + "/" + p.Repo
		}
		desc := p.Description
		if len(desc) > 72 {
			desc = desc[:69] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.RepoVersion, p.SourceName, p.Category, repo, desc)
	}
	tw.Flush()
	fmt.Printf("\n%d package(s) from %s\n", len(hits), strings.Join(set.Names, ", "))
}

// hubInfoCommand implements `no info <pkg>` (the registry form).
func hubInfoCommand(args []string) {
	const usage = "Usage: no info [-source <name>] <pkg>"
	sourceSel, rest, err := hubParseSourceFlag(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no info: %v\n%s\n", err, usage)
		os.Exit(2)
	}
	if len(rest) == 0 {
		fmt.Println(usage)
		os.Exit(2)
	}
	set := hubLoadForCommand(sourceSel, "info", usage)
	name := strings.ToLower(rest[0])
	var matches []*hubGroup
	for _, g := range groupHubPackages(set.packages()) {
		if strings.EqualFold(g.Name, name) {
			matches = append(matches, g)
		}
	}
	if len(matches) == 0 {
		fmt.Fprintf(os.Stderr, "no info: package %q not found (try `no search %s`)\n", args[0], args[0])
		os.Exit(1)
	}
	for gi, g := range matches {
		p := g.Latest
		if gi > 0 {
			fmt.Println()
		}
		fmt.Printf("name:        %s\n", p.Name)
		fmt.Printf("source:      %s\n", p.SourceName)
		if len(g.Versions) > 1 {
			versions := make([]string, 0, len(g.Versions))
			for _, v := range g.Versions {
				versions = append(versions, v.RepoVersion)
			}
			fmt.Printf("versions:    %s\n", strings.Join(versions, ", "))
		} else {
			fmt.Printf("version:     %s\n", p.RepoVersion)
		}
		fmt.Printf("description: %s\n", p.Description)
		if len(p.Keywords) > 0 {
			fmt.Printf("keywords:    %s\n", strings.Join(p.Keywords, ", "))
		}
		if p.Category != "" {
			fmt.Printf("category:    %s\n", p.Category)
		}
		if p.License != "" {
			fmt.Printf("license:     %s\n", p.License)
		}
		if p.Author != "" {
			fmt.Printf("author:      %s\n", p.Author)
		}
		repo := p.RepoKey
		if repo == "" {
			repo = p.Host + "/" + p.Owner + "/" + p.Repo
		}
		fmt.Printf("repository:  %s\n", normalizeHubRepoURL(p.Repository))
		if p.Homepage != "" {
			fmt.Printf("homepage:    %s\n", p.Homepage)
		}
		fmt.Printf("docs:        %s\n", resolveHubDocsURL(p))
		ref := p.Name + ":" + p.RepoVersion + "@" + repo
		fmt.Printf("install:     no install %s\n", ref)
		fmt.Printf("import:      no add %s\n", ref)
		if p.LatestCommit != "" {
			fmt.Printf("commit:      %s\n", p.LatestCommit)
		}
	}
}

// hubDocsCommand implements `no docs <pkg>`.
func hubDocsCommand(args []string) {
	const usage = "Usage: no docs [-source <name>] <pkg>"
	sourceSel, rest, err := hubParseSourceFlag(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no docs: %v\n%s\n", err, usage)
		os.Exit(2)
	}
	if len(rest) == 0 {
		fmt.Println(usage)
		os.Exit(2)
	}
	set := hubLoadForCommand(sourceSel, "docs", usage)
	name := strings.ToLower(rest[0])
	for _, g := range groupHubPackages(set.packages()) {
		if strings.EqualFold(g.Name, name) {
			url := resolveHubDocsURL(g.Latest)
			if url == "" {
				fmt.Fprintf(os.Stderr, "no docs: %s has no documentation URL\n", rest[0])
				os.Exit(1)
			}
			fmt.Println(url)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "no docs: package %q not found (try `no search %s`)\n", rest[0], rest[0])
	os.Exit(1)
}
