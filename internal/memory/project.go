package memory

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/yetone/magpie/internal/provider"
)

// ProjectOf is the project a folder belongs to: the git repository it is
// in, keyed by its origin remote (host/owner/repo), so every clone and
// worktree of one repository is one project; a repository with no remote
// by its main folder; a folder outside any repository by itself. nil for
// no folder, and for one with a .git on the way up that can't be read:
// whether it is in a repository, and which, is unknown.
func ProjectOf(dir string) *Project {
	dir = strings.TrimSpace(dir)
	if dir == "" || !filepath.IsAbs(dir) {
		return nil
	}
	dir = filepath.Clean(dir)
	root, common, err := gitRoot(dir)
	if err != nil {
		return nil
	}
	if root == "" {
		return &Project{Key: "dir:" + filepath.ToSlash(dir), Name: filepath.Base(dir), Roots: []string{dir}}
	}
	if u := originURL(filepath.Join(common, "config")); u != "" {
		if key := RemoteKey(u); key != "" {
			return &Project{Key: key, Name: lastPart(key), Roots: []string{root}}
		}
	}
	// a repository with no remote is its main checkout's folder: a
	// worktree's common git folder is <main>/.git
	main := root
	if filepath.Base(common) == ".git" {
		main = filepath.Dir(common)
	}
	return &Project{Key: "dir:" + filepath.ToSlash(main), Name: filepath.Base(main), Roots: []string{root}}
}

// gitRoot is the folder holding the .git git finds from dir, and the
// folder that holds its repository's config (provider.GitAt: a linked
// worktree's common one): "" when dir isn't in a repository. A .git git
// doesn't take, an empty folder say, is passed over as git passes it
// over; a .git file that names no repository ends the walk in none, as
// git stops there. err is a .git on the way that couldn't be read.
func gitRoot(dir string) (root, common string, err error) {
	for d := dir; ; {
		git, common, err := provider.GitAt(filepath.Join(d, ".git"))
		switch {
		case err != nil:
			return "", "", err
		case git == "no":
			return "", "", nil
		case git == "yes":
			return d, common, nil
		}
		up := filepath.Dir(d)
		if up == d {
			return "", "", nil
		}
		d = up
	}
}

// originURL is the url of [remote "origin"] in a git config file, else of
// the first remote it has.
func originURL(config string) string {
	f, err := os.Open(config)
	if err != nil {
		return ""
	}
	defer f.Close()
	section, first, origin := "", "", ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.Join(strings.Fields(strings.Trim(line, "[]")), " "))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !strings.HasPrefix(section, "remote ") || strings.ToLower(strings.TrimSpace(k)) != "url" {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if first == "" {
			first = v
		}
		if section == `remote "origin"` && origin == "" {
			origin = v
		}
	}
	if origin != "" {
		return origin
	}
	return first
}

// RemoteKey is a git remote's address as a project key, the same for each
// way of writing it: https://github.com/a/b.git, git@github.com:a/b and
// ssh://git@github.com/a/b all are github.com/a/b. "" for a local path.
func RemoteKey(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if i := strings.Index(u, "://"); i >= 0 {
		scheme := strings.ToLower(u[:i])
		if scheme == "file" {
			return ""
		}
		u = u[i+3:]
		if at := strings.LastIndex(strings.SplitN(u, "/", 2)[0], "@"); at >= 0 {
			u = u[at+1:]
		}
		host, path, _ := strings.Cut(u, "/")
		if h, _, ok := strings.Cut(host, ":"); ok {
			host = h // a port
		}
		u = host + "/" + path
	} else if host, path, ok := strings.Cut(u, ":"); ok && !strings.Contains(host, "/") && len(host) > 1 {
		// scp-like: [user@]host:path (a one-letter host is a Windows drive)
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		u = host + "/" + strings.TrimPrefix(path, "/")
	} else {
		return ""
	}
	u = strings.TrimSuffix(strings.TrimRight(u, "/"), ".git")
	host, path, _ := strings.Cut(u, "/")
	if host == "" || path == "" {
		return ""
	}
	return strings.ToLower(host) + "/" + path
}

func lastPart(key string) string {
	key = strings.TrimRight(key, "/")
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return key
}

// ScopeOf is the scope of a project's memories.
func ScopeOf(p *Project) string {
	if p == nil || p.Key == "" {
		return ""
	}
	return "project:" + p.Key
}
