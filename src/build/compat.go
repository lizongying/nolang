package build

// 包管理子系統（package.jsonc 解析、依賴下載/鎖定/解析、std 源碼路徑）已遷移至
// mod 套件，使 checker 等前端套件可以不依賴 build（transpiler 後端）。
// 以下別名保持 build 既有 API 相容，避免大面積改動呼叫方。

import "github.com/lizongying/nolang/package"

type (
	Package         = pkg.Package
	CompilerOptions = pkg.CompilerOptions
	DependencyGraph = pkg.DependencyGraph
	WorkspaceMap    = pkg.WorkspaceMap
	LockFile        = pkg.LockFile
)

var (
	LoadPackage             = pkg.LoadPackage
	NewDependencyGraph      = pkg.NewDependencyGraph
	LoadLockFile            = pkg.LoadLockFile
	PackageShortName        = pkg.PackageShortName
	NoHomeDir               = pkg.NoHomeDir
	GetStdSourceDir         = pkg.GetStdSourceDir
	GetStdSourceFile        = pkg.GetStdSourceFile
	GetSourceDir            = pkg.GetSourceDir
	DownloadPackage         = pkg.DownloadPackage
	StripJSONC              = pkg.StripJSONC
	FindWorkspaceRoot       = pkg.FindWorkspaceRoot
	FindPackageRoot         = pkg.FindPackageRoot
	ResolveToWorkspaceRoot  = pkg.ResolveToWorkspaceRoot
)

// stripJSONC 舊私有名稱轉發（builder.go 等內部呼叫）。
func stripJSONC(raw []byte) []byte { return pkg.StripJSONC(raw) }
