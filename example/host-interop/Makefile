# Seven host languages calling into one Nolang shared library.
# Usage: make verify   (from this directory)
#
# This is a documented workaround: `no build` only emits executables, so we
# take the LLVM IR it writes with -v and link it ourselves as a shared library.

SHELL := /bin/bash
export PATH := /opt/homebrew/bin:/opt/homebrew/opt/llvm/bin:$(PATH)

NO      ?= ../../bin/no
JAVA    ?= java
CUR     := $(CURDIR)
UNAME   := $(shell uname -s)

ifeq ($(UNAME),Darwin)
  LIB          := libnolang.dylib
  JNI_LIB      := libnolangjni.dylib
  CORE_INSTALL := -Wl,-install_name,@rpath/$(LIB)
  HOST_RPATH   := -Wl,-rpath,@loader_path
  LDLIBENV     := DYLD_LIBRARY_PATH=$(CUR)/dist
  JNI_OS_DIR   := darwin
else
  LIB          := libnolang.so
  JNI_LIB      := libnolangjni.so
  CORE_INSTALL :=
  HOST_RPATH   := -Wl,-rpath,'$$ORIGIN'
  LDLIBENV     := LD_LIBRARY_PATH=$(CUR)/dist
  JNI_OS_DIR   := linux
endif

# javac -h needs jni.h; fall back to deriving JAVA_HOME from javac on Linux.
JH := $(shell /usr/libexec/java_home 2>/dev/null || dirname $$(dirname $$(dirname "$$(command -v javac)")))
JNI_INCLUDES := -I"$(JH)/include" -I"$(JH)/include/$(JNI_OS_DIR)"

.PHONY: all probe lib jni c cpp rust go csharp verify clean

all: verify

# Step 1: .no -> raw .ll, then pin the internal ABI before any C is compiled.
probe:
	mkdir -p dist
	$(NO) build -v -o dist/libnolang main.no
	python3 abi_guard.py dist/libnolang.ll dist/abi_signatures.txt

# Step 2: .ll -> PIC object -> shared library, plus the stable C shim.
lib: probe
	clang -x ir -O0 -fPIC -c dist/libnolang.ll -o dist/nolang.o
	clang -O2 -fPIC -Ishim -c shim/nolang_shim.c -o dist/nolang_shim.o
	clang -shared -fPIC $(CORE_INSTALL) -o dist/$(LIB) dist/nolang.o dist/nolang_shim.o
	@if [ "$(UNAME)" = Darwin ]; then codesign -s - --force dist/$(LIB) >/dev/null 2>&1; fi
	@count=$$(nm -g dist/$(LIB) | grep -cE 'nolang_(add|scale|sum3)$$' || true); \
	 if [ "$$count" -ne 3 ]; then echo "FAIL: expected 3 exported symbols, got $$count"; exit 1; fi; \
	 echo "[lib] OK: 3 stable symbols exported from dist/$(LIB)"
	@if [ "$(UNAME)" = Linux ]; then \
	   if readelf -r dist/nolang.o | grep -qE 'R_X86_64_32S?'; then \
	     echo "FAIL: non-PIC relocations in dist/nolang.o"; exit 1; fi; \
	 fi

# JNI glue is a second shared library so the other six hosts need no JDK.
jni: lib
	mkdir -p dist/classes
	javac -h dist -d dist/classes java/NolangDemo.java
	clang -O2 -fPIC $(JNI_INCLUDES) -Ishim -Idist -c jni/nolang_jni.c -o dist/nolang_jni.o
	clang -shared -fPIC -Wl,-rpath,$(CUR)/dist -o dist/$(JNI_LIB) \
	      dist/nolang_jni.o -Ldist -lnolang
	@if [ "$(UNAME)" = Darwin ]; then codesign -s - --force dist/$(JNI_LIB) >/dev/null 2>&1; fi

c:
	clang -O2 -Ishim -o dist/c_demo c/main.c -Ldist -lnolang $(HOST_RPATH)

cpp:
	g++ -std=c++17 -O2 -Ishim -o dist/cpp_demo cpp/main.cpp -Ldist -lnolang $(HOST_RPATH)

rust:
	rustc -O -L native=$(CUR)/dist -C link-arg=-Wl,-rpath,$(CUR)/dist \
	      -o dist/rust_demo rust/main.rs

go:
	cd go && CGO_ENABLED=1 CC=clang go build -o ../dist/go_demo .

csharp:
	cd csharp && dotnet build -c Release -o ../dist/csharp_out -v q >/dev/null

# One command: build everything, run all seven hosts, diff pinned output.
verify:
	@$(MAKE) --no-print-directory jni
	@$(MAKE) --no-print-directory c cpp rust go csharp
	@: > dist/actual.txt
	@$(LDLIBENV) ./dist/c_demo   >> dist/actual.txt
	@$(LDLIBENV) ./dist/cpp_demo >> dist/actual.txt
	@$(LDLIBENV) ./dist/rust_demo >> dist/actual.txt
	@$(LDLIBENV) ./dist/go_demo  >> dist/actual.txt
	@$(LDLIBENV) $(JAVA) -Djava.library.path=dist -cp dist/classes NolangDemo >> dist/actual.txt
	@python3 python/main.py >> dist/actual.txt
	@NOLANG_LIB=$(CUR)/dist/$(LIB) dotnet dist/csharp_out/host-interop.dll >> dist/actual.txt
	@diff -u expected.txt dist/actual.txt
	@echo "ALL SEVEN HOST LANGUAGES PASSED"

clean:
	rm -rf dist
