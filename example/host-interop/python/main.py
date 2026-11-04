#!/usr/bin/env python3
"""Call the Nolang shared library through ctypes (stdlib only)."""

import ctypes
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[1]
name = "libnolang.dylib" if sys.platform == "darwin" else "libnolang.so"
lib = ctypes.CDLL(str(root / "dist" / name))

lib.nolang_add.argtypes = [ctypes.c_int64, ctypes.c_int64]
lib.nolang_add.restype = ctypes.c_int64
lib.nolang_scale.argtypes = [ctypes.c_double, ctypes.c_double]
lib.nolang_scale.restype = ctypes.c_double
lib.nolang_sum3.argtypes = [ctypes.c_int64, ctypes.c_int64, ctypes.c_int64]
lib.nolang_sum3.restype = ctypes.c_int64

print(f"python {lib.nolang_add(20, 22)} "
      f"{lib.nolang_scale(2.5, 4.0):.1f} "
      f"{lib.nolang_sum3(10, 20, 12)}")
