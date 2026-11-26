package mir

import (
	"fmt"
	"strconv"
	"strings"
)

func isRawVFSBuiltin(name string) bool {
	switch name {
	case "raw-open", "raw-close", "raw-seek", "raw-pread", "raw-pwrite",
		"raw-ftruncate", "raw-fstat-size", "raw-fsync", "raw-fdatasync",
		"raw-sync-dir", "raw-lock-range", "raw-unlock-range", "raw-capabilities":
		return true
	}
	return false
}

func rawVFSCapabilitiesForTarget() int64 {
	switch targetGOOS() {
	case "linux", "darwin":
		return 255
	case "windows":
		if targetGOARCH() == "amd64" || targetGOARCH() == "arm64" {
			return 239
		}
	}
	return 0
}

func (c *codegen) ensureRawVFSSupport() {
	if targetGOOS() == "windows" {
		// Windows raw helpers are part of the target-gated prelude, alongside
		// the Win32 declarations they use.
		return
	}
	key := "raw-unsupported-support"
	supported := targetGOOS() == "linux" || targetGOOS() == "darwin"
	if supported {
		key = "raw-posix-support"
	}
	if c.extraFuncs == nil {
		c.extraFuncs = map[string]bool{}
	}
	if c.extraFuncs[key] {
		return
	}
	c.extraFuncs[key] = true
	c.decl("declare i32* @" + c.errnoFnName() + "()")
	if supported {
		for _, d := range rawPOSIXSupportDecls() {
			c.decl(d)
		}
		c.extraFuncsBody.WriteString(rawPOSIXSupportIR())
		return
	}
	c.extraFuncsBody.WriteString(rawUnsupportedSupportIR())
}

func (c *codegen) rawPathArg(inst *Inst, index int, call func(string) error) error {
	v, err := c.argIndex(inst, index)
	if err != nil {
		return err
	}
	path := c.cstrOf(v)
	if path == "" {
		return fmt.Errorf("builtin %s: cannot marshal arg %d as path", inst.Sym, index)
	}
	// Preserve the helper's errno across the temporary path cleanup.
	c.decl("declare i32* @" + c.errnoFnName() + "()")
	errnoPtr := c.treg("raw.ep")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32* @%s()\n", errnoPtr, c.errnoFnName()))
	if err := call(path); err != nil {
		return err
	}
	// The helper runs before cleanup: nolang_free may itself touch errno, so
	// capture the helper's result only after the call and restore it afterwards.
	saved := c.treg("raw.err")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", saved, errnoPtr))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_free(i8* %s)\n", path))
	c.sb.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", saved, errnoPtr))
	return nil
}

func (c *codegen) rawBufferArg(inst *Inst, index int) (ptr, length string, err error) {
	v, err := c.argIndex(inst, index)
	if err != nil {
		return "", "", err
	}
	lt, val := c.loadVal(v)
	switch lt {
	case "%vec":
		length = c.vecLenI64(val)
	case "%str-long":
		length = c.hdrFieldI64("%str-long", val, 0)
	default:
		if strings.HasPrefix(lt, "[") && strings.HasSuffix(lt, "]") {
			parts := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(lt, "["), "]"))
			if len(parts) > 0 {
				if n, parseErr := strconv.ParseInt(parts[0], 10, 64); parseErr == nil {
					length = strconv.FormatInt(n, 10)
				}
			}
		}
	}
	if length == "" {
		return "", "", fmt.Errorf("builtin %s: cannot determine byte length of arg %d (%s)", inst.Sym, index, lt)
	}
	ptr = c.dataPtrOf(v)
	if ptr == "" {
		return "", "", fmt.Errorf("builtin %s: cannot take data pointer of arg %d", inst.Sym, index)
	}
	return ptr, length, nil
}

func (c *codegen) storeRawOption(inst *Inst, value, ok string, boolPayload bool) error {
	if _, exists := c.resultType(inst, 0); !exists {
		return nil
	}
	if boolPayload {
		wide := c.treg("raw.bool")
		c.sb.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", wide, ok))
		value = wide
	}
	return c.storeOptionFromPair(inst, value, ok)
}

func (c *codegen) emitBuiltinRawVFS(inst *Inst, name string) error {
	if name == "raw-capabilities" {
		return c.storeResult(inst, 0, strconv.FormatInt(rawVFSCapabilitiesForTarget(), 10), "i64")
	}
	c.ensureRawVFSSupport()

	switch name {
	case "raw-open":
		var result string
		err := c.rawPathArg(inst, 0, func(path string) error {
			flags, err := c.marshalScalar(inst, 1, "i64")
			if err != nil {
				return err
			}
			mode, err := c.marshalScalar(inst, 2, "i64")
			if err != nil {
				return err
			}
			result = c.treg("raw.open")
			c.sb.WriteString(fmt.Sprintf("  %s = call i64 @nolang.raw_open(i8* %s, i64 %s, i64 %s)\n", result, path, flags, mode))
			return nil
		})
		if err != nil {
			return err
		}
		ok := c.treg("raw.ok")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i64 %s, 0\n", ok, result))
		return c.storeRawOption(inst, result, ok, false)

	case "raw-close", "raw-ftruncate", "raw-fsync", "raw-fdatasync", "raw-unlock-range":
		args, err := c.rawScalarArgs(inst, name)
		if err != nil {
			return err
		}
		result := c.treg("raw.call")
		c.sb.WriteString(fmt.Sprintf("  %s = call i32 @nolang.%s(%s)\n", result, rawHelperName(name), args))
		ok := c.treg("raw.ok")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", ok, result))
		return c.storeRawOption(inst, "0", ok, true)

	case "raw-seek", "raw-fstat-size":
		args, err := c.rawScalarArgs(inst, name)
		if err != nil {
			return err
		}
		result := c.treg("raw.call")
		c.sb.WriteString(fmt.Sprintf("  %s = call i64 @nolang.%s(%s)\n", result, rawHelperName(name), args))
		ok := c.treg("raw.ok")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i64 %s, 0\n", ok, result))
		return c.storeRawOption(inst, result, ok, false)

	case "raw-pread", "raw-pwrite":
		fd, err := c.marshalScalar(inst, 0, "i64")
		if err != nil {
			return err
		}
		ptr, length, err := c.rawBufferArg(inst, 1)
		if err != nil {
			return err
		}
		count, err := c.marshalScalar(inst, 2, "i64")
		if err != nil {
			return err
		}
		offset, err := c.marshalScalar(inst, 3, "i64")
		if err != nil {
			return err
		}
		result := c.treg("raw.io")
		c.sb.WriteString(fmt.Sprintf("  %s = call i64 @nolang.%s(i64 %s, i8* %s, i64 %s, i64 %s, i64 %s)\n", result, rawHelperName(name), fd, ptr, count, offset, length))
		ok := c.treg("raw.ok")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i64 %s, 0\n", ok, result))
		return c.storeRawOption(inst, result, ok, false)

	case "raw-sync-dir":
		var result string
		err := c.rawPathArg(inst, 0, func(path string) error {
			result = c.treg("raw.syncdir")
			c.sb.WriteString(fmt.Sprintf("  %s = call i32 @nolang.raw_sync_dir(i8* %s)\n", result, path))
			return nil
		})
		if err != nil {
			return err
		}
		ok := c.treg("raw.ok")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", ok, result))
		return c.storeRawOption(inst, "0", ok, true)

	case "raw-lock-range":
		args, err := c.rawScalarArgs(inst, name)
		if err != nil {
			return err
		}
		result := c.treg("raw.lock")
		c.sb.WriteString(fmt.Sprintf("  %s = call i32 @nolang.raw_lock_range(%s)\n", result, args))
		ok := c.treg("raw.ok")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", ok, result))
		return c.storeRawOption(inst, "0", ok, true)
	}
	return fmt.Errorf("unsupported raw VFS builtin %s", name)
}

func rawHelperName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

func (c *codegen) rawScalarArgs(inst *Inst, name string) (string, error) {
	count := 1
	switch name {
	case "raw-seek":
		count = 3
	case "raw-ftruncate":
		count = 2
	case "raw-unlock-range":
		count = 3
	case "raw-lock-range":
		count = 5
	}
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		want := "i64"
		if name == "raw-lock-range" && i == 4 {
			want = "i1"
		}
		v, err := c.marshalScalar(inst, i, want)
		if err != nil {
			return "", err
		}
		args = append(args, want+" "+v)
	}
	return strings.Join(args, ", "), nil
}
