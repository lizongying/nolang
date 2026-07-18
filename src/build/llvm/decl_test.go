package llvm

import (
	"runtime"
	"testing"
)

func TestStatLayout(t *testing.T) {
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
	}
}

func TestOpenWriteFlags(t *testing.T) {
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
	}
}
