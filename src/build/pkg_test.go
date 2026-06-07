package build

import (
	"testing"
)

func TestLoadPackageTestDir(t *testing.T) {
	// 確保 test/nolang.jsonc 能被正確解析
	pkg, err := LoadPackage("../../test")
	if err != nil {
		t.Fatalf("LoadPackage(test) error: %v", err)
	}
	if pkg == nil {
		t.Fatal("LoadPackage(test) returned nil")
	}
	if pkg.Name != "my-project" {
		t.Errorf("Name = %q, want %q", pkg.Name, "my-project")
	}
	if pkg.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", pkg.Version, "1.0.0")
	}
	if len(pkg.Keywords) != 1 || pkg.Keywords[0] != "nolang" {
		t.Errorf("Keywords = %v, want [nolang]", pkg.Keywords)
	}
	if pkg.Author != "lizongying" {
		t.Errorf("Author = %q, want %q", pkg.Author, "lizongying")
	}
	if pkg.Repository != "https://github.com/lizongying/nolang" {
		t.Errorf("Repository = %q, want %q", pkg.Repository, "https://github.com/lizongying/nolang")
	}
	if pkg.License != "MIT" {
		t.Errorf("License = %q, want %q", pkg.License, "MIT")
	}
	if pkg.DevDependencies == nil {
		t.Error("DevDependencies is nil")
	} else if pkg.DevDependencies["nolang"] != "^0.1.0" {
		t.Errorf("DevDependencies[nolang] = %q, want %q", pkg.DevDependencies["nolang"], "^0.1.0")
	}
	if len(pkg.Ignore) != 1 || pkg.Ignore[0] != "dist" {
		t.Errorf("Ignore = %v, want [dist]", pkg.Ignore)
	}
}
