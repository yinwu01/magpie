package libc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A root with Alpine's name for musl's libc, or with musl's loader and no
// glibc loader, is a musl Linux, OpenWrt's layout among them (#5808); one
// with glibc's loader, musl's beside it or not, is not.
func TestMuslUnder(t *testing.T) {
	for _, c := range []struct {
		name  string
		files []string
		links [][2]string // name, target
		musl  bool
	}{
		{"glibc x86_64", []string{"lib64/ld-linux-x86-64.so.2"}, nil, false},
		{"glibc aarch64", []string{"lib/ld-linux-aarch64.so.1"}, nil, false},
		{"Alpine x86_64", []string{"lib/ld-musl-x86_64.so.1"}, [][2]string{{"lib/libc.musl-x86_64.so.1", "ld-musl-x86_64.so.1"}}, true},
		// gcompat puts glibc's loader name on an Alpine; it is musl still
		{"Alpine x86_64 with gcompat", []string{"lib/ld-musl-x86_64.so.1", "lib64/ld-linux-x86-64.so.2"}, [][2]string{{"lib/libc.musl-x86_64.so.1", "ld-musl-x86_64.so.1"}}, true},
		{"Alpine aarch64, its libc name alone", []string{"lib/libc.musl-aarch64.so.1"}, nil, true},
		// as the reporter's OpenWrt 25.12.5 on armv8 has it
		{"OpenWrt aarch64", []string{"lib/libc.so"}, [][2]string{{"lib/ld-musl-aarch64.so.1", "libc.so"}}, true},
		{"OpenWrt x86_64", []string{"lib/libc.so"}, [][2]string{{"lib/ld-musl-x86_64.so.1", "libc.so"}}, true},
		// Debian with its musl package, which can still start only glibc builds
		{"glibc x86_64 with musl's loader", []string{"lib64/ld-linux-x86-64.so.2", "usr/lib/x86_64-linux-musl/libc.so"}, [][2]string{{"lib/ld-musl-x86_64.so.1", "../usr/lib/x86_64-linux-musl/libc.so"}}, false},
		{"glibc aarch64 with musl's loader", []string{"lib/ld-linux-aarch64.so.1", "lib/libc.so"}, [][2]string{{"lib/ld-musl-aarch64.so.1", "libc.so"}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for _, f := range c.files {
				p := filepath.Join(root, f)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, nil, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, l := range c.links {
				p := filepath.Join(root, l[0])
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(l[1], p); err != nil {
					t.Skipf("no symlinks here: %v", err)
				}
			}
			if got := muslUnder(root); got != c.musl {
				t.Errorf("musl %v, want %v", got, c.musl)
			}
		})
	}
}

// A glibc loader that can't be looked at is not taken to be missing: an
// OpenWrt-like root whose /lib64 can't be searched stays glibc.
func TestMuslUnderUnreadableGlibcLoader(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root searches any folder")
	}
	root := t.TempDir()
	for _, d := range []string{"lib", "lib64"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "lib/libc.so"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("libc.so", filepath.Join(root, "lib/ld-musl-x86_64.so.1")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if !muslUnder(root) {
		t.Fatal("not musl before /lib64 was made unsearchable")
	}
	if err := os.Chmod(filepath.Join(root, "lib64"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "lib64"), 0o755) })
	if _, err := os.Stat(filepath.Join(root, "lib64/ld-linux-x86-64.so.2")); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Skipf("an unsearchable folder reads as %v here", err)
	}
	if muslUnder(root) {
		t.Fatal("musl with glibc's loader unknown")
	}
}

// Only a Linux is ever musl.
func TestMuslOnlyOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" && Musl() {
		t.Fatalf("Musl() on %s", runtime.GOOS)
	}
}
