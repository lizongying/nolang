package llvm

import (
	"runtime"
	"testing"
)

func TestStatLayout(t *testing.T) {
	// 驗證當前平台值（既有行為，回歸測試重點）
	layout := statLayout()
	switch runtime.GOOS {
	case "darwin":
		if layout.Size != 144 {
			t.Errorf("darwin stat size = %d, want 144", layout.Size)
		}
		if layout.ModeOff != 4 {
			t.Errorf("darwin st_mode offset = %d, want 4", layout.ModeOff)
		}
		if layout.UidOff != 16 {
			t.Errorf("darwin st_uid offset = %d, want 16", layout.UidOff)
		}
		if layout.GidOff != 20 {
			t.Errorf("darwin st_gid offset = %d, want 20", layout.GidOff)
		}
		if layout.MtimeOff != 48 {
			t.Errorf("darwin st_mtime offset = %d, want 48", layout.MtimeOff)
		}
		if layout.SizeOff != 96 {
			t.Errorf("darwin st_size offset = %d, want 96", layout.SizeOff)
		}
	case "linux":
		if runtime.GOARCH == "arm64" {
			if layout.Size != 128 {
				t.Errorf("linux arm64 stat size = %d, want 128", layout.Size)
			}
			if layout.ModeOff != 16 {
				t.Errorf("linux arm64 st_mode offset = %d, want 16", layout.ModeOff)
			}
			if layout.UidOff != 24 {
				t.Errorf("linux arm64 st_uid offset = %d, want 24", layout.UidOff)
			}
			if layout.GidOff != 28 {
				t.Errorf("linux arm64 st_gid offset = %d, want 28", layout.GidOff)
			}
			if layout.MtimeOff != 88 {
				t.Errorf("linux arm64 st_mtime offset = %d, want 88", layout.MtimeOff)
			}
			if layout.SizeOff != 48 {
				t.Errorf("linux arm64 st_size offset = %d, want 48", layout.SizeOff)
			}
		} else {
			// amd64
			if layout.Size != 144 {
				t.Errorf("linux amd64 stat size = %d, want 144", layout.Size)
			}
			if layout.ModeOff != 24 {
				t.Errorf("linux amd64 st_mode offset = %d, want 24", layout.ModeOff)
			}
			if layout.UidOff != 28 {
				t.Errorf("linux amd64 st_uid offset = %d, want 28", layout.UidOff)
			}
			if layout.GidOff != 32 {
				t.Errorf("linux amd64 st_gid offset = %d, want 32", layout.GidOff)
			}
			if layout.MtimeOff != 88 {
				t.Errorf("linux amd64 st_mtime offset = %d, want 88", layout.MtimeOff)
			}
			if layout.SizeOff != 48 {
				t.Errorf("linux amd64 st_size offset = %d, want 48", layout.SizeOff)
			}
		}
	case "windows":
		if layout.Size != 64 {
			t.Errorf("windows stat size = %d, want 64", layout.Size)
		}
		if layout.ModeOff != 16 {
			t.Errorf("windows st_mode offset = %d, want 16", layout.ModeOff)
		}
		if layout.UidOff != 20 {
			t.Errorf("windows st_uid offset = %d, want 20", layout.UidOff)
		}
		if layout.GidOff != 22 {
			t.Errorf("windows st_gid offset = %d, want 22", layout.GidOff)
		}
		if layout.MtimeOff != 48 {
			t.Errorf("windows st_mtime offset = %d, want 48", layout.MtimeOff)
		}
		if layout.SizeOff != 32 {
			t.Errorf("windows st_size offset = %d, want 32", layout.SizeOff)
		}
	}
}

func TestStatLayoutForAllPlatforms(t *testing.T) {
	// 透過 statLayoutFor 驗證所有平台分支（不依賴當前 runtime）。
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   StatLayout
	}{
		{
			name:   "darwin arm64",
			goos:   "darwin",
			goarch: "arm64",
			want:   StatLayout{Size: 144, ModeOff: 4, UidOff: 16, GidOff: 20, MtimeOff: 48, SizeOff: 96},
		},
		{
			name:   "darwin amd64",
			goos:   "darwin",
			goarch: "amd64",
			want:   StatLayout{Size: 144, ModeOff: 4, UidOff: 16, GidOff: 20, MtimeOff: 48, SizeOff: 96},
		},
		{
			name:   "linux amd64",
			goos:   "linux",
			goarch: "amd64",
			want:   StatLayout{Size: 144, ModeOff: 24, UidOff: 28, GidOff: 32, MtimeOff: 88, SizeOff: 48},
		},
		{
			name:   "linux arm64",
			goos:   "linux",
			goarch: "arm64",
			want:   StatLayout{Size: 128, ModeOff: 16, UidOff: 24, GidOff: 28, MtimeOff: 88, SizeOff: 48},
		},
		{
			name:   "windows amd64",
			goos:   "windows",
			goarch: "amd64",
			want:   StatLayout{Size: 64, ModeOff: 16, UidOff: 20, GidOff: 22, MtimeOff: 48, SizeOff: 32},
		},
		{
			name:   "windows arm64",
			goos:   "windows",
			goarch: "arm64",
			want:   StatLayout{Size: 64, ModeOff: 16, UidOff: 20, GidOff: 22, MtimeOff: 48, SizeOff: 32},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statLayoutFor(tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("statLayoutFor(%q, %q) = %+v, want %+v", tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func TestOpenWriteFlags(t *testing.T) {
	// 驗證當前平台值（既有行為，回歸測試重點）
	flags := openWriteFlags()
	switch runtime.GOOS {
	case "darwin":
		if flags != 1537 {
			t.Errorf("darwin openWriteFlags = %d, want 1537", flags)
		}
	case "linux":
		if flags != 577 {
			t.Errorf("linux openWriteFlags = %d, want 577", flags)
		}
	case "windows":
		if flags != 769 {
			t.Errorf("windows openWriteFlags = %d, want 769", flags)
		}
	}
}

func TestOpenWriteFlagsForAllPlatforms(t *testing.T) {
	// 透過 openWriteFlagsFor 驗證所有平台分支（不依賴當前 runtime）。
	tests := []struct {
		goos string
		want int
	}{
		{"darwin", 1537},
		{"linux", 577},
		{"windows", 769},
		{"unknown", 1537}, // 回退到 macOS 值
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got := openWriteFlagsFor(tt.goos)
			if got != tt.want {
				t.Errorf("openWriteFlagsFor(%q) = %d, want %d", tt.goos, got, tt.want)
			}
		})
	}
}
