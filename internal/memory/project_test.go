package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemoteKey(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/yetone/magpie.git":        "github.com/yetone/magpie",
		"https://GitHub.com/yetone/magpie/":           "github.com/yetone/magpie",
		"git@github.com:yetone/magpie.git":            "github.com/yetone/magpie",
		"ssh://git@github.com:22/yetone/magpie":       "github.com/yetone/magpie",
		"https://user:tok@gitlab.example.com/g/sub/r": "gitlab.example.com/g/sub/r",
		"file:///srv/repos/x.git":                     "",
		"/srv/repos/x.git":                            "",
		`C:\repos\x`:                                  "",
		"":                                            "",
	} {
		if got := RemoteKey(in); got != want {
			t.Errorf("RemoteKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func put(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitInit lays out at dotGit what git looks for in a repository's .git
// (provider.GitAt), as git init does: a HEAD, objects and refs.
func gitInit(t *testing.T, dotGit string) {
	t.Helper()
	put(t, filepath.Join(dotGit, "HEAD"), "ref: refs/heads/main\n")
	for _, d := range []string{"objects", "refs/heads"} {
		if err := os.MkdirAll(filepath.Join(dotGit, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// A clone, its worktree and a folder deep in either are one project.
func TestProjectOfIsTheRepository(t *testing.T) {
	d := t.TempDir()
	main := filepath.Join(d, "magpie")
	gitInit(t, filepath.Join(main, ".git"))
	put(t, filepath.Join(main, ".git", "config"), "[core]\n\tbare = false\n[remote \"upstream\"]\n\turl = git@github.com:other/fork.git\n[remote \"origin\"]\n\turl = https://github.com/yetone/magpie.git\n")
	wt := filepath.Join(d, "wt", "memory")
	put(t, filepath.Join(wt, ".git"), "gitdir: "+filepath.Join(main, ".git", "worktrees", "memory")+"\n")
	put(t, filepath.Join(main, ".git", "worktrees", "memory", "HEAD"), "ref: refs/heads/memory\n")
	put(t, filepath.Join(main, ".git", "worktrees", "memory", "commondir"), "../..\n")
	deep := filepath.Join(wt, "internal", "memory")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{main, deep} {
		p := ProjectOf(dir)
		if p == nil || p.Key != "github.com/yetone/magpie" || p.Name != "magpie" {
			t.Fatalf("ProjectOf(%s) = %+v", dir, p)
		}
	}
	if p := ProjectOf(deep); p.Roots[0] != wt {
		t.Fatalf("the worktree's root is %v", p.Roots)
	}

	// no remote: the main checkout's folder, from its worktree too
	put(t, filepath.Join(main, ".git", "config"), "[core]\n\tbare = false\n")
	if p := ProjectOf(deep); p.Key != "dir:"+filepath.ToSlash(main) {
		t.Fatalf("with no remote: %+v", p)
	}
	// outside a repository: the folder itself
	plain := filepath.Join(d, "notes")
	if p := ProjectOf(plain); p.Key != "dir:"+filepath.ToSlash(plain) || ScopeOf(p) != "project:dir:"+filepath.ToSlash(plain) {
		t.Fatalf("a plain folder: %+v", p)
	}
	if ProjectOf("") != nil || ProjectOf("relative/x") != nil || ScopeOf(nil) != "" {
		t.Fatal("no folder made a project")
	}
}

// A .git git doesn't take for a repository, an empty folder say, is
// passed over as git passes it over: a folder under one is a project of
// its own, or of the repository above it. An empty /tmp/.git made every
// temp folder the project "tmp".
func TestProjectOfPassesAnEmptyGit(t *testing.T) {
	d := t.TempDir()
	plain := filepath.Join(d, "empty", "notes")
	if err := os.MkdirAll(filepath.Join(d, "empty", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p := ProjectOf(plain); p.Key != "dir:"+filepath.ToSlash(plain) || p.Name != "notes" || p.Roots[0] != plain {
		t.Errorf("under an empty .git: %+v", p)
	}
	repo := filepath.Join(d, "magpie")
	gitInit(t, filepath.Join(repo, ".git"))
	put(t, filepath.Join(repo, ".git", "config"), "[remote \"origin\"]\n\turl = https://github.com/yetone/magpie.git\n")
	inner := filepath.Join(repo, "web", "src")
	if err := os.MkdirAll(filepath.Join(repo, "web", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if p := ProjectOf(inner); p.Key != "github.com/yetone/magpie" || p.Roots[0] != repo {
		t.Errorf("under an empty .git in a repository: %+v", p)
	}
	// a .git file that names no repository, in a repository: git stops
	// there, in none
	put(t, filepath.Join(repo, "garbage", ".git"), "not a gitfile\n")
	garbage := filepath.Join(repo, "garbage", "sub")
	if p := ProjectOf(garbage); p.Key != "dir:"+filepath.ToSlash(garbage) {
		t.Errorf("under a .git file naming nothing: %+v", p)
	}
}

// A .git on the way up that can't be read leaves the folder's project
// unknown, never the folder itself nor a repository above it.
func TestProjectOfUnreadIsUnknown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a folder isn't locked by its mode")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a folder whatever its mode")
	}
	repo := filepath.Join(t.TempDir(), "magpie")
	gitInit(t, filepath.Join(repo, ".git"))
	put(t, filepath.Join(repo, ".git", "config"), "[remote \"origin\"]\n\turl = https://github.com/yetone/magpie.git\n")
	put(t, filepath.Join(repo, "file", ".git"), "gitdir: "+filepath.Join(repo, ".git")+"\n")
	if err := os.MkdirAll(filepath.Join(repo, "folder", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"folder", "file"} {
		locked := filepath.Join(repo, name, ".git")
		fi, err := os.Stat(locked)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		p := ProjectOf(filepath.Join(repo, name, "sub"))
		if err := os.Chmod(locked, fi.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		if p != nil {
			t.Errorf("under a .git %s it can't read: %+v, want unknown", name, p)
		}
	}
}
