// Package libc tells which C library this Linux runs on, for the
// downloads that come in a glibc build and a musl one: Claude Code's and
// Bun's. A glibc build names glibc's loader (/lib64/ld-linux-x86-64.so.2)
// as its interpreter, which a musl Linux such as Alpine doesn't have, so
// the kernel can't start it and exec says "no such file or directory"
// (#1877).
package libc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// alpineLibcs are musl's libc under the name Alpine gives it, which
// Anthropic's installer looks for: a Linux that has one is musl.
var alpineLibcs = []string{"/lib/libc.musl-x86_64.so.1", "/lib/libc.musl-aarch64.so.1"}

// muslLoaders are musl's own loader, which every x86_64 or aarch64 musl
// Linux has. OpenWrt
// has only these (/lib/ld-musl-aarch64.so.1 -> libc.so, #5808). A glibc
// Linux has one too when its musl package is installed (Debian's musl
// puts /lib/ld-musl-x86_64.so.1 there), so they tell musl only where
// glibc's loader, the one a glibc build names, isn't there.
var muslLoaders = []string{"/lib/ld-musl-x86_64.so.1", "/lib/ld-musl-aarch64.so.1"}

// glibcLoaders are the loaders glibc's x86_64 and aarch64 builds name as
// their interpreter.
var glibcLoaders = []string{"/lib64/ld-linux-x86-64.so.2", "/lib/ld-linux-aarch64.so.1"}

// Musl is whether this is an x86_64 or aarch64 Linux whose C library is
// musl (Alpine, OpenWrt), the two that have musl builds to download. It
// is false on every other system.
func Musl() bool {
	return runtime.GOOS == "linux" && muslUnder("/")
}

// muslUnder is Musl for the system whose root is root. A glibc loader
// that can't be looked at for any reason but its absence counts as there:
// what can't be read is unknown, and unknown keeps the glibc build, as
// before musl's loader was looked for.
func muslUnder(root string) bool {
	if anyUnder(root, alpineLibcs, false) {
		return true
	}
	return anyUnder(root, muslLoaders, false) && !anyUnder(root, glibcLoaders, true)
}

// anyUnder is whether one of paths is under root; a path that can't be
// looked at for another reason than its absence counts as there when
// unknownIsThere.
func anyUnder(root string, paths []string, unknownIsThere bool) bool {
	for _, p := range paths {
		_, err := os.Stat(filepath.Join(root, p))
		if err == nil || unknownIsThere && !errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}
	return false
}
