// process.c — cross-platform implementation of @nolang.process_run
//
// This file is embedded into the nolang compiler and linked into every
// produced executable (see builder.go). It implements a complete,
// production-grade subprocess runner:
//
//   - spawn a child process (fork/exec on POSIX, CreateProcess on Windows)
//   - capture stdout; optionally merge stderr into the captured output
//   - feed an optional stdin buffer to the child
//   - optional working directory (chdir) and environment (full replacement)
//   - optional timeout with reliable child kill on expiry
//
// The caller (the nolang code generator) passes the argv/envp slices as raw
// byte buffers together with the element stride and the byte offset of the
// `data` pointer field inside each element. This avoids any assumption about
// the in-memory layout of nolang strings (which differs between build modes)
// and lets the C side iterate the slice generically.
//
// Returned output is allocated here with malloc() and handed back via
// out_data / out_len; the code generator wraps it into a nolang %str-long.
//
// status_out convention (mirrors common shell/timeout tools):
//   >= 0 : child exited normally, value is the exit code (0-255)
//      -1 : failed to start / exec the child
//      -2 : timed out (child was killed)

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#ifndef _WIN32
// Feature-test macros: enable POSIX.1-2008 functions (strndup, etc.) on macOS
// and GNU extensions on glibc/Linux.
#define _GNU_SOURCE
#ifdef __APPLE__
#define _DARWIN_C_SOURCE
#endif
#include <errno.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/wait.h>
#include <signal.h>
#include <time.h>
#include <sys/socket.h>
#else
#include <windows.h>
#include <winsock2.h>
#endif

// nolang_net_recv_nb: non-blocking recv used by the WebSocket poll loop
// (ws.recv-nb) so a connection handler can check for incoming "stop" messages
// without blocking the cooperative event loop.
//
// Return convention (maps cleanly onto nolang's i64 result):
//   > 0 : bytes received
//     0 : connection closed (peer sent FIN)
//    -1 : hard error
//    -2 : would block (no data available right now; EAGAIN / EWOULDBLOCK)
#ifdef _WIN32
long nolang_net_recv_nb(int fd, char* buf, long n) {
    u_long mode = 1;
    ioctlsocket((SOCKET)fd, FIONBIO, &mode);
    int r = recv((SOCKET)fd, buf, (int)n, 0);
    if (r == SOCKET_ERROR) {
        if (WSAGetLastError() == WSAEWOULDBLOCK) return -2;
        return -1;
    }
    return (long)r;
}
#else
long nolang_net_recv_nb(int fd, char* buf, long n) {
    long r = recv(fd, buf, n, MSG_DONTWAIT);
    if (r == -1 && (errno == EAGAIN || errno == EWOULDBLOCK)) return -2;
    return r;
}
#endif

// nolang_net_accept_nb: non-blocking accept used by the cooperative event loop
// (ws/server.accept-nb and net.accept-nb) so the main coroutine can poll for
// incoming connections without blocking the whole scheduler.
//
// The listen socket is put into non-blocking mode (idempotent) so this call
// returns immediately when no connection is pending. The accepted socket is
// likewise put into non-blocking mode so the connection handler can use
// net-recv-nb / ws.recv-nb right away.
//
// Return convention (maps cleanly onto nolang's i64 result):
//   >= 0 : client socket fd
//    -2  : would block (no connection pending right now; EAGAIN / EWOULDBLOCK)
//    -1  : hard error
#ifdef _WIN32
long nolang_net_accept_nb(int listen_fd) {
    u_long mode = 1;
    ioctlsocket((SOCKET)listen_fd, FIONBIO, &mode);
    SOCKET client = accept((SOCKET)listen_fd, NULL, NULL);
    if (client == INVALID_SOCKET) {
        if (WSAGetLastError() == WSAEWOULDBLOCK) return -2;
        return -1;
    }
    u_long cmode = 1;
    ioctlsocket(client, FIONBIO, &cmode);
    return (long)client;
}
#else
long nolang_net_accept_nb(int listen_fd) {
    int flags = fcntl(listen_fd, F_GETFL, 0);
    if (flags < 0) return -1;
    if (!(flags & O_NONBLOCK)) {
        if (fcntl(listen_fd, F_SETFL, flags | O_NONBLOCK) < 0) return -1;
    }
    int client = accept(listen_fd, NULL, NULL);
    if (client < 0) {
        if (errno == EAGAIN || errno == EWOULDBLOCK) return -2;
        return -1;
    }
    int cflags = fcntl(client, F_GETFL, 0);
    if (cflags >= 0) fcntl(client, F_SETFL, cflags | O_NONBLOCK);
    return (long)client;
}
#endif

// ---- small helpers -------------------------------------------------------

// Read an entire pipe/file descriptor into a malloc'd, NUL-terminated buffer.
// Returns the buffer (caller frees) and stores the number of bytes read
// (excluding the trailing NUL) into *out_len. On error returns NULL, *out_len=0.
#ifdef _WIN32
static char *read_all_handle(HANDLE h, int64_t *out_len) {
    size_t cap = 4096, len = 0;
    char *buf = (char *)malloc(cap);
    if (!buf) { *out_len = 0; return NULL; }
    for (;;) {
        DWORD got = 0;
        // Read in chunks; ReadFile returns FALSE when the pipe is closed.
        if (!ReadFile(h, buf + len, (DWORD)(cap - len), &got, NULL)) {
            break;
        }
        if (got == 0) break;
        len += got;
        if (len + 1 >= cap) {
            cap *= 2;
            char *nb = (char *)realloc(buf, cap);
            if (!nb) { free(buf); *out_len = 0; return NULL; }
            buf = nb;
        }
    }
    buf[len] = '\0';
    *out_len = (int64_t)len;
    return buf;
}
#else
static char *read_all_fd(int fd, int64_t *out_len) {
    size_t cap = 4096, len = 0;
    char *buf = (char *)malloc(cap);
    if (!buf) { *out_len = 0; return NULL; }
    for (;;) {
        ssize_t got = read(fd, buf + len, cap - len);
        if (got < 0) {
            if (errno == EINTR) continue;
            break;
        }
        if (got == 0) break;
        len += (size_t)got;
        if (len + 1 >= cap) {
            cap *= 2;
            char *nb = (char *)realloc(buf, cap);
            if (!nb) { free(buf); *out_len = 0; return NULL; }
            buf = nb;
        }
    }
    buf[len] = '\0';
    *out_len = (int64_t)len;
    return buf;
}
#endif

// Build a char** from a nolang string slice.
// base points at the first element; each element is `stride` bytes, with the
// string bytes at element + data_off (an i64 length at element + 0).
static char **build_argv(uint8_t *base, int64_t count, int64_t stride,
                          int64_t data_off) {
    if (count <= 0) {
        char **a = (char **)malloc(sizeof(char *));
        a[0] = NULL;
        return a;
    }
    char **a = (char **)malloc(sizeof(char *) * ((size_t)count + 1));
    for (int64_t i = 0; i < count; i++) {
        uint8_t *elem = base + i * stride;
        uint64_t slen = *(const uint64_t *)elem;            // length at offset 0
        const char *sdata = *(const char *const *)(elem + data_off); // ptr at data_off
        a[i] = strndup(sdata, (size_t)slen);
    }
    a[count] = NULL;
    return a;
}

static void free_argv(char **a) {
    if (!a) return;
    for (size_t i = 0; a[i]; i++) free(a[i]);
    free(a);
}

#ifdef _WIN32
// Build a Windows environment block ("K=V\0K=V\0\0") from a nolang string slice.
static char *build_env_block(uint8_t *base, int64_t count, int64_t stride,
                             int64_t data_off) {
    if (count <= 0) return NULL;
    // Worst case: every char becomes K=V\0 plus a trailing \0\0.
    size_t cap = 1;
    for (int64_t i = 0; i < count; i++) {
        uint8_t *elem = base + i * stride;
        uint64_t slen = *(const uint64_t *)elem;
        cap += (size_t)slen + 2;
    }
    char *blk = (char *)malloc(cap);
    if (!blk) return NULL;
    size_t pos = 0;
    for (int64_t i = 0; i < count; i++) {
        uint8_t *elem = base + i * stride;
        uint64_t slen = *(const uint64_t *)elem;
        const char *sdata = *(const char *const *)(elem + data_off);
        memcpy(blk + pos, sdata, (size_t)slen);
        pos += (size_t)slen;
        blk[pos++] = '\0';
    }
    blk[pos++] = '\0';
    return blk;
}

// Join argv[1..] into a command line with minimal quoting for CreateProcess.
static char *build_cmdline(uint8_t *base, int64_t count, int64_t stride,
                           int64_t data_off) {
    size_t cap = 256, pos = 0;
    char *cmd = (char *)malloc(cap);
    if (!cmd) return NULL;
    for (int64_t i = 1; i < count; i++) {
        uint8_t *elem = base + i * stride;
        uint64_t slen = *(const uint64_t *)elem;
        const char *sdata = *(const char *const *)(elem + data_off);
        int need_quote = 0;
        for (uint64_t k = 0; k < slen; k++) if (sdata[k] == ' ' || sdata[k] == '\t') need_quote = 1;
        if (pos + slen + 3 >= cap) { cap = pos + slen + 256; char *nc = (char *)realloc(cmd, cap); if (!nc) { free(cmd); return NULL; } cmd = nc; }
        if (i > 1) cmd[pos++] = ' ';
        if (need_quote) cmd[pos++] = '"';
        memcpy(cmd + pos, sdata, (size_t)slen);
        pos += (size_t)slen;
        if (need_quote) cmd[pos++] = '"';
    }
    cmd[pos] = '\0';
    return cmd;
}
#endif

// ---- public entry point --------------------------------------------------

void nolang_process_run(
    // argv slice (elements are nolang strings)
    uint8_t *argv_data, int64_t argv_len,
    // envp slice (elements are nolang strings); empty => inherit environment
    uint8_t *envp_data, int64_t envp_len,
    // element stride (bytes) and byte offset of the `data` pointer field
    int64_t stride, int64_t data_off,
    // working directory ("" => inherit); NULL-safe
    char *dir,
    // stdin payload (may be NULL); stdin_len bytes are written
    char *stdin_buf, int64_t stdin_len,
    // timeout in milliseconds (<=0 => wait forever)
    int64_t timeout_ms,
    // merge child stderr into captured stdout when non-zero
    int64_t merge_err,
    // outputs
    char **out_data, int64_t *out_len, int64_t *status_out) {
    *out_data = NULL;
    *out_len = 0;
    *status_out = -1;

    char **argv = build_argv(argv_data, argv_len, stride, data_off);
    if (argv_len > 0 && argv[0] == NULL) {
        // No program name — nothing to exec.
        free_argv(argv);
        *status_out = -1;
        return;
    }
    char **envp = NULL;
#ifdef _WIN32
    char *env_block = build_env_block(envp_data, envp_len, stride, data_off);
#else
    if (envp_len > 0) {
        envp = build_argv(envp_data, envp_len, stride, data_off);
    }
#endif

#ifdef _WIN32
    SECURITY_ATTRIBUTES sa;
    sa.nLength = sizeof(sa);
    sa.bInheritHandle = TRUE;
    sa.lpSecurityDescriptor = NULL;

    HANDLE out_r = NULL, out_w = NULL, err_r = NULL, err_w = NULL, in_r = NULL, in_w = NULL;
    if (!CreatePipe(&out_r, &out_w, &sa, 0)) { free_argv(argv); *status_out = -1; return; }
    if (!merge_err) {
        if (!CreatePipe(&err_r, &err_w, &sa, 0)) { CloseHandle(out_r); CloseHandle(out_w); free_argv(argv); *status_out = -1; return; }
    }
    if (stdin_len > 0) {
        if (!CreatePipe(&in_r, &in_w, &sa, 0)) {
            CloseHandle(out_r); CloseHandle(out_w);
            if (!merge_err) { CloseHandle(err_r); CloseHandle(err_w); }
            free_argv(argv); *status_out = -1; return;
        }
    }

    char *cmdline = build_cmdline(argv_data, argv_len, stride, data_off);
    STARTUPINFOA si;
    PROCESS_INFORMATION pi;
    memset(&si, 0, sizeof(si));
    memset(&pi, 0, sizeof(pi));
    si.cb = sizeof(si);
    si.hStdOutput = out_w;
    si.hStdError = merge_err ? out_w : err_w;
    si.hStdInput = in_r ? in_r : GetStdHandle(STD_INPUT_HANDLE);
    si.dwFlags = STARTF_USESTDHANDLES;

    const char *app = (argv_len > 0) ? argv[0] : NULL;
    int ok = CreateProcessA(app, cmdline, NULL, NULL, TRUE, 0,
                             env_block, (dir && dir[0]) ? dir : NULL, &si, &pi);
    free(cmdline);
    free_argv(argv);
    if (env_block) free(env_block);

    if (!ok) {
        CloseHandle(out_r); CloseHandle(out_w);
        if (!merge_err) { CloseHandle(err_r); CloseHandle(err_w); }
        if (in_r) { CloseHandle(in_r); CloseHandle(in_w); }
        *status_out = -1;
        return;
    }

    // Parent: close child-side handles.
    CloseHandle(out_w);
    if (!merge_err) CloseHandle(err_w);
    if (in_r) CloseHandle(in_r);

    if (stdin_len > 0 && in_w) {
        DWORD wr = 0;
        WriteFile(in_w, stdin_buf, (DWORD)stdin_len, &wr, NULL);
        CloseHandle(in_w);
    }

    DWORD wait_r = WaitForSingleObject(pi.hProcess,
                                       timeout_ms > 0 ? (DWORD)timeout_ms : INFINITE);
    if (wait_r == WAIT_TIMEOUT) {
        TerminateProcess(pi.hProcess, 1);
        // Reap.
        WaitForSingleObject(pi.hProcess, INFINITE);
        *status_out = -2;
    } else {
        DWORD code = 0;
        GetExitCodeProcess(pi.hProcess, &code);
        *status_out = (int64_t)code;
    }
    CloseHandle(pi.hProcess);
    CloseHandle(pi.hThread);

    // Capture output.
    char *out = read_all_handle(out_r, out_len);
    CloseHandle(out_r);
    if (!merge_err) { int64_t dr = 0; char *e = read_all_handle(err_r, &dr); if (e) free(e); CloseHandle(err_r); }
    *out_data = out ? out : (char *)malloc(1);
#else
    // ---- POSIX ----
    int out_pipe[2];
    if (pipe(out_pipe) != 0) { free_argv(argv); if (envp) free_argv(envp); *status_out = -1; return; }
    int in_pipe[2] = {-1, -1};
    if (stdin_len > 0 && pipe(in_pipe) != 0) {
        close(out_pipe[0]); close(out_pipe[1]);
        free_argv(argv); if (envp) free_argv(envp);
        *status_out = -1; return;
    }

    // Exec-error pipe: the child writes `errno` here if execvp() fails, and
    // the write end is CLOEXEC so a *successful* exec closes it and the parent
    // sees EOF. This lets the parent reliably distinguish "exec failed" (-1)
    // from "child ran and exited with code 127".
    int exec_pipe[2] = {-1, -1};
    if (pipe(exec_pipe) != 0) {
        close(out_pipe[0]); close(out_pipe[1]);
        if (in_pipe[0] >= 0) { close(in_pipe[0]); close(in_pipe[1]); }
        free_argv(argv); if (envp) free_argv(envp);
        *status_out = -1; return;
    }
    fcntl(exec_pipe[1], F_SETFD, FD_CLOEXEC);

    pid_t pid = fork();
    if (pid < 0) {
        close(out_pipe[0]); close(out_pipe[1]);
        if (in_pipe[0] >= 0) { close(in_pipe[0]); close(in_pipe[1]); }
        close(exec_pipe[0]); close(exec_pipe[1]);
        free_argv(argv); if (envp) free_argv(envp);
        *status_out = -1;
        return;
    }

    if (pid == 0) {
        // Child.
        close(exec_pipe[0]);
        dup2(out_pipe[1], 1);
        if (merge_err) dup2(out_pipe[1], 2);
        if (in_pipe[0] >= 0) dup2(in_pipe[0], 0);
        close(out_pipe[0]); close(out_pipe[1]);
        if (in_pipe[0] >= 0) { close(in_pipe[0]); close(in_pipe[1]); }
        if (dir && dir[0]) chdir(dir);
        if (envp) {
            for (int64_t i = 0; envp[i]; i++) {
                char *kv = envp[i];
                char *eq = strchr(kv, '=');
                if (eq) {
                    *eq = '\0';
                    setenv(kv, eq + 1, 1);
                    *eq = '=';
                }
            }
        }
        execvp(argv[0], argv);
        // execvp only returns on failure.
        int e = errno;
        ssize_t wr = write(exec_pipe[1], &e, sizeof(e));
        (void)wr;
        _exit(127);
    }

    // Parent.
    close(exec_pipe[1]);

    // Detect exec failure: a successful exec closes exec_pipe[1] (CLOEXEC) and
    // we get EOF here; a failed exec writes errno and we report -1.
    int exec_errno = 0;
    ssize_t er = read(exec_pipe[0], &exec_errno, sizeof(exec_errno));
    close(exec_pipe[0]);
    if (er == (ssize_t)sizeof(exec_errno) && exec_errno != 0) {
        int status = 0;
        waitpid(pid, &status, 0);
        close(out_pipe[0]);
        if (in_pipe[0] >= 0) { close(in_pipe[0]); close(in_pipe[1]); }
        free_argv(argv); if (envp) free_argv(envp);
        *status_out = -1;
        *out_data = (char *)malloc(1);
        *out_len = 0;
        return;
    }

    close(out_pipe[1]);
    if (in_pipe[0] >= 0) {
        close(in_pipe[0]);
        if (stdin_len > 0) {
            ssize_t w = 0;
            size_t off = 0;
            while (off < (size_t)stdin_len) {
                w = write(in_pipe[1], stdin_buf + off, (size_t)stdin_len - off);
                if (w < 0) { if (errno == EINTR) continue; break; }
                off += (size_t)w;
            }
        }
        close(in_pipe[1]);
    }

    // Wait with optional timeout (portable poll loop).
    int status = 0;
    if (timeout_ms > 0) {
        int64_t elapsed = 0;
        int done = 0;
        while (elapsed < timeout_ms) {
            int wpr = waitpid(pid, &status, WNOHANG);
            if (wpr == pid) { done = 1; break; }
            if (wpr < 0) { if (errno == EINTR) continue; break; }
            struct timespec ts;
            ts.tv_sec = 0;
            ts.tv_nsec = 10000000L; // 10 ms
            nanosleep(&ts, NULL);
            elapsed += 10;
        }
        if (!done) {
            kill(pid, SIGKILL);
            waitpid(pid, &status, 0);
            *status_out = -2;
        } else {
            *status_out = WIFEXITED(status) ? (int64_t)WEXITSTATUS(status)
                                            : (int64_t)(-WTERMSIG(status));
        }
    } else {
        if (waitpid(pid, &status, 0) == pid) {
            *status_out = WIFEXITED(status) ? (int64_t)WEXITSTATUS(status)
                                            : (int64_t)(-WTERMSIG(status));
        } else {
            *status_out = -1;
        }
    }

    // Capture stdout.
    char *out = read_all_fd(out_pipe[0], out_len);
    close(out_pipe[0]);
    *out_data = out ? out : (char *)malloc(1);

    free_argv(argv);
    if (envp) free_argv(envp);
#endif
}
