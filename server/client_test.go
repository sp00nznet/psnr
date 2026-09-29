package main

import (
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCClient builds client/test_psnr.c and runs it against a live server, so
// the C client and the Go server can't drift apart without a failure. Skips
// when there's no C compiler (set CC to pick one).
func TestCClient(t *testing.T) {
	cc := ""
	for _, name := range []string{"cc", "gcc", "clang"} {
		if p, err := exec.LookPath(name); err == nil {
			cc = p
			break
		}
	}
	if cc == "" {
		t.Skip("no C compiler on PATH; client/test_psnr.c not run")
	}

	exe := filepath.Join(t.TempDir(), "test_psnr")
	args := []string{"-std=gnu11", "-Wall", "-o", exe, "../client/psnr.c", "../client/test_psnr.c"}
	if runtime.GOOS == "windows" {
		args = append(args, "-lws2_32")
	}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", cc, err, out)
	}

	addr := start(t)
	_, port, _ := net.SplitHostPort(addr)
	out, err := exec.Command(exe, "127.0.0.1", port).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "all passed") {
		t.Fatalf("test_psnr: %v\n%s", err, out)
	}
}
