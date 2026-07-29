package build

// 包管理子系統（mod.jsonc 解析、依賴下載/鎖定/解析、std 源碼路徑）已遷移至
// mod 套件，使 checker 等前端套件可以不依賴 build（transpiler 後端）。
// 以下別名保持 build 既有 API 相容，避免大面積改動呼叫方。

import "github.com/lizongying/nolang/mod"

type (
	Package         = mod.Package
	CompilerOptions = mod.CompilerOptions
	DependencyGraph = mod.DependencyGraph
	WorkspaceMap    = mod.WorkspaceMap
	LockFile        = mod.LockFile
)

var (
	LoadPackage        = mod.LoadPackage
	NewDependencyGraph = mod.NewDependencyGraph
	LoadLockFile       = mod.LoadLockFile
	PackageShortName   = mod.PackageShortName
	NoHomeDir          = mod.NoHomeDir
	GetStdSourceDir    = mod.GetStdSourceDir
	GetStdSourceFile   = mod.GetStdSourceFile
	GetSourceDir       = mod.GetSourceDir
	DownloadPackage    = mod.DownloadPackage
	StripJSONC         = mod.StripJSONC
)

// stripJSONC 舊私有名稱轉發（builder.go 等內部呼叫）。
func stripJSONC(raw []byte) []byte { return mod.StripJSONC(raw) }
