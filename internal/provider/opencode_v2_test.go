package provider

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// opencode225 is OpenCode 2.2.5's own chat request to Zen, as a recording
// proxy saw it on 2026-11-01 (#3491): its headers in order, its core tools
// and its environment block.
type opencode225 struct {
	Headers     [][2]string `json:"headers"`
	Environment string      `json:"environment"`
	Tools       []struct {
		Function struct {
			Name       string          `json:"name"`
			Parameters json.RawMessage `json:"parameters"`
			Strict     *bool           `json:"strict"`
		} `json:"function"`
	} `json:"tools"`
}

func loadOpenCode225(t *testing.T) opencode225 {
	t.Helper()
	b, err := os.ReadFile("testdata/opencode-2.2.5-chat.json")
	if err != nil {
		t.Fatal(err)
	}
	var c opencode225
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c opencode225) header(name string) string {
	for _, h := range c.Headers {
		if strings.EqualFold(h[0], name) {
			return h[1]
		}
	}
	return ""
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// #3491: Zen's free tier answers 429 "Free models work in OpenCode" to
// OpenCode 1.x's request shape. A request to OpenCode's gateway carries
// the headers OpenCode 2.2.5 sent, by name, with values of the same
// shape: its User-Agent, one trace a session and a new span a request,
// one session id in all four session headers, its project, and no 1.x
// x-opencode-request.
func TestOpenCodeClientIs225(t *testing.T) {
	c := loadOpenCode225(t)
	h := http.Header{"X-Opencode-Request": {"msg_old"}}
	// what the gateway and Sign put on beside OpenCodeClient
	h.Set("Authorization", "Bearer public")
	h.Set("Content-Type", "application/json")
	OpenCodeClient(h, "conversation-225", t.TempDir())

	transport := map[string]bool{"host": true, "content-length": true, "accept-encoding": true, "connection": true}
	var want, got []string
	for _, kv := range c.Headers {
		if !transport[strings.ToLower(kv[0])] {
			want = append(want, strings.ToLower(kv[0]))
		}
	}
	for k := range h {
		got = append(got, strings.ToLower(k))
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("headers\n got %v\nwant %v", got, want)
	}
	if ua := h.Get("User-Agent"); ua != c.header("User-Agent") || ua != OpenCodeUserAgent() {
		t.Errorf("User-Agent %q, OpenCode 2.2.5 sent %q", ua, c.header("User-Agent"))
	}
	for _, k := range []string{"Accept", "x-opencode-client"} {
		if h.Get(k) != c.header(k) {
			t.Errorf("%s %q, OpenCode sent %q", k, h.Get(k), c.header(k))
		}
	}
	id := h.Get("x-opencode-session")
	if !openCodeRealSession(id) {
		t.Errorf("session %q", id)
	}
	for _, k := range []string{"x-opencode-session-id", "x-session-affinity", "x-session-id"} {
		if h.Get(k) != id {
			t.Errorf("%s %q, session %q", k, h.Get(k), id)
		}
	}
	b3 := regexp.MustCompile(`^([0-9a-f]{32})-([0-9a-f]{16})-1-[0-9a-f]{16}$`)
	tp := regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-01$`)
	if !b3.MatchString(c.header("b3")) || !tp.MatchString(c.header("traceparent")) {
		t.Fatalf("the capture's trace headers aren't of the shape checked: %q %q", c.header("b3"), c.header("traceparent"))
	}
	b, p := b3.FindStringSubmatch(h.Get("b3")), tp.FindStringSubmatch(h.Get("traceparent"))
	if b == nil || p == nil || b[1] != p[1] || b[2] != p[2] {
		t.Fatalf("b3 %q traceparent %q", h.Get("b3"), h.Get("traceparent"))
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(h.Get("x-opencode-project")) || h.Get("x-opencode-project") == "global" {
		t.Errorf("project %q", h.Get("x-opencode-project"))
	}
	// the conversation's next request: the same session and trace, a new span
	h2 := http.Header{}
	OpenCodeClient(h2, "conversation-225", "")
	b2 := b3.FindStringSubmatch(h2.Get("b3"))
	if h2.Get("x-opencode-session") != id || b2 == nil || b2[1] != b[1] || b2[2] == b[2] {
		t.Errorf("next request: session %q b3 %q (first %q)", h2.Get("x-opencode-session"), h2.Get("b3"), h.Get("b3"))
	}
	// another conversation: another trace
	h3 := http.Header{}
	OpenCodeClient(h3, "conversation-other", "")
	if b3 := b3.FindStringSubmatch(h3.Get("b3")); b3 == nil || b3[1] == b[1] {
		t.Errorf("another conversation's b3 %q", h3.Get("b3"))
	}
}

// OpenCode's project id (packages/core/src/project.ts): sha1 of
// git-remote:host/path in a repository with a remote (origin, else the
// first), else of directory: and the repository's root or the folder.
func TestOpenCodeProjectIs225s(t *testing.T) {
	c := loadOpenCode225(t)
	if runtime.GOOS != "windows" {
		// the folder 2.2.5 ran in, not a repository: the id it sent
		if got := OpenCodeProject("/Users/yetone/runs/zencap/home/work"); got != c.header("x-opencode-project") {
			t.Errorf("project %q, OpenCode sent %q", got, c.header("x-opencode-project"))
		}
	}
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	repo := func(name, config string) string {
		d := filepath.Join(root, name)
		gitInit(t, filepath.Join(d, ".git"))
		os.MkdirAll(filepath.Join(d, "src", "deep"), 0o755)
		os.WriteFile(filepath.Join(d, ".git", "config"), []byte(config), 0o644)
		return d
	}
	plain := filepath.Join(root, "plain")
	os.MkdirAll(plain, 0o755)
	scp := repo("scp", "[core]\n\tbare = false\n[remote \"upstream\"]\n\turl = https://other.test/x/y.git\n[remote \"origin\"]\n\turl = git@GitHub.com:Foo/Bar.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n")
	first := repo("first", "[remote \"upstream\"]\n\turl = https://GitLab.com/a/b/\n")
	local := repo("local", "[core]\n\tbare = false\n")
	file := repo("file", "[remote \"origin\"]\n\turl = file:///srv/x.git\n")
	// a worktree: .git is a file naming its gitdir, whose commondir is the
	// main repository's
	wt := filepath.Join(root, "wt")
	os.MkdirAll(wt, 0o755)
	own := filepath.Join(scp, ".git", "worktrees", "wt")
	os.MkdirAll(own, 0o755)
	os.WriteFile(filepath.Join(own, "HEAD"), []byte("ref: refs/heads/wt\n"), 0o644)
	os.WriteFile(filepath.Join(own, "commondir"), []byte("../..\n"), 0o644)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+own+"\n"), 0o644)
	for dir, want := range map[string]string{
		plain:                               "directory:" + plain,
		filepath.Join(scp, "src", "deep"):   "git-remote:github.com/Foo/Bar",
		filepath.Join(first, "src"):         "git-remote:gitlab.com/a/b",
		filepath.Join(local, "src", "deep"): "directory:" + local,
		file:                                "directory:" + file,
		wt:                                  "git-remote:github.com/Foo/Bar",
	} {
		if got := OpenCodeProject(dir); got != sha1Hex(want) {
			t.Errorf("%s: %s, want sha1(%q)", dir, got, want)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	home, _ = filepath.EvalSymlinks(home)
	if got := OpenCodeProject(""); got != sha1Hex("directory:"+home) {
		t.Errorf("no folder: %s, want the home folder's", got)
	}
}

// The environment block is OpenCode 2.2.5's word for word, said for the
// folder given (#3491: Zen's free tier serves a request as OpenCode's only
// with one).
func TestOpenCodeEnvironmentIs225s(t *testing.T) {
	c := loadOpenCode225(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("TMP", tmp)
	realTmp, _ := filepath.EvalSymlinks(os.TempDir())
	platform := map[string]string{"windows": "win32"}[runtime.GOOS]
	if platform == "" {
		platform = runtime.GOOS
	}
	dir := filepath.Join(t.TempDir(), "work")
	os.MkdirAll(dir, 0o755)
	want := c.Environment
	want = strings.ReplaceAll(want, "/Users/yetone/runs/zencap/home/work", dir)
	want = strings.Replace(want, "Platform: darwin", "Platform: "+platform, 1)
	want = strings.Replace(want, "/private/tmp/opencode", filepath.Join(realTmp, "opencode"), 1)
	if got := OpenCodeEnvironment(dir); got != want {
		t.Fatalf("block\n%s\nwant\n%s", got, want)
	}
	if !OpenCodeHasEnvironment("You are OpenCode.\n\n"+c.Environment) || OpenCodeHasEnvironment("<ENV>\n  working directory: /x\n</ENV>") {
		t.Fatal("OpenCode's block is told by <env>, Working directory: and </env>")
	}
	// in a repository's subfolder: the root is the workspace
	repo := filepath.Join(t.TempDir(), "repo")
	sub := filepath.Join(repo, "pkg")
	gitInit(t, filepath.Join(repo, ".git"))
	os.MkdirAll(sub, 0o755)
	repo, _ = filepath.EvalSymlinks(repo)
	sub, _ = filepath.EvalSymlinks(sub)
	got := OpenCodeEnvironment(sub)
	if !strings.Contains(got, "  Working directory: "+sub+"\n  Workspace root folder: "+repo+"\n  Is directory a git repo: yes\n") {
		t.Fatalf("in a repository:\n%s", got)
	}
}

// gitInit lays out at dotGit what git looks for in a repository's .git
// (GitAt), as git init does: a HEAD, objects and refs.
func gitInit(t *testing.T, dotGit string) {
	t.Helper()
	for _, d := range []string{"objects", "refs/heads"} {
		if err := os.MkdirAll(filepath.Join(dotGit, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dotGit, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// OpenCode 2.2.5 finds a folder's repository with git rev-parse
// (Project.resolve, Git.repo.discover), so a .git git doesn't take is no
// repository to it: an empty one is passed over, to the repository above
// it if there is one, and the project and environment block are the
// folder's own. An empty /tmp/.git made every temp folder the repository
// at /tmp (#5239). Each kind of .git gitFixtures lays out is a repository
// to OpenCode when git finds one there.
func TestOpenCodeRepositoryIsWhatGitFinds(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	plain := filepath.Join(root, "plain", "work")
	os.MkdirAll(plain, 0o755)
	os.MkdirAll(filepath.Join(root, "plain", ".git"), 0o755)
	repo := filepath.Join(root, "repo")
	gitInit(t, filepath.Join(repo, ".git"))
	os.WriteFile(filepath.Join(repo, ".git", "config"), []byte("[remote \"origin\"]\n\turl = git@github.com:Foo/Bar.git\n"), 0o644)
	inner := filepath.Join(repo, "inner", "work")
	os.MkdirAll(inner, 0o755)
	os.MkdirAll(filepath.Join(repo, "inner", ".git"), 0o755)
	for _, c := range []struct{ dir, id, workspace, git string }{
		{plain, "directory:" + plain, plain, "no"},
		{inner, "git-remote:github.com/Foo/Bar", repo, "yes"},
	} {
		if got := OpenCodeProject(c.dir); got != sha1Hex(c.id) {
			t.Errorf("%s: project %s, want sha1(%q)", c.dir, got, c.id)
		}
		if got, want := OpenCodeEnvironment(c.dir), "  Workspace root folder: "+c.workspace+"\n  Is directory a git repo: "+c.git+"\n"; !strings.Contains(got, want) {
			t.Errorf("%s: environment\n%s\nwant it to say\n%s", c.dir, got, want)
		}
	}
	for _, c := range gitFixtures(t, filepath.Join(root, "kinds")) {
		if _, _, ok := openCodeRepository(c.dir); ok != (c.want == "yes") {
			t.Errorf("%s: a repository %v, git says %s", c.name, ok, c.want)
		}
	}
}

// A .git OpenCode's git can't read: git rev-parse passes a .git folder
// it can't open over and finds the repository above it, and fails at a
// .git file it can't read (git 2.34), where OpenCode names the folder by
// itself.
func TestOpenCodeRepositoryUnreadIsGits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a folder isn't locked by its mode")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a folder whatever its mode")
	}
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	gitInit(t, filepath.Join(repo, ".git"))
	os.WriteFile(filepath.Join(repo, ".git", "config"), []byte("[remote \"origin\"]\n\turl = git@github.com:Foo/Bar.git\n"), 0o644)
	os.MkdirAll(filepath.Join(repo, "folder", ".git"), 0o755)
	os.MkdirAll(filepath.Join(repo, "file"), 0o755)
	os.WriteFile(filepath.Join(repo, "file", ".git"), []byte("gitdir: "+filepath.Join(repo, ".git")+"\n"), 0o644)
	for _, c := range []struct{ name, id string }{
		{"folder", "git-remote:github.com/Foo/Bar"},
		{"file", "directory:" + filepath.Join(repo, "file", "sub")},
	} {
		dir := filepath.Join(repo, c.name, "sub")
		os.MkdirAll(dir, 0o755)
		locked := filepath.Join(repo, c.name, ".git")
		fi, err := os.Stat(locked)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		got := OpenCodeProject(dir)
		if err := os.Chmod(locked, fi.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		if got != sha1Hex(c.id) {
			t.Errorf("under a .git %s it can't read: %s, want sha1(%q)", c.name, got, c.id)
		}
	}
}

// The core tools a free request offers are OpenCode 2.2.5's, by name and
// parameters, strict false on Chat as its are.
func TestOpenCodeToolsAre225s(t *testing.T) {
	c := loadOpenCode225(t)
	var names []string
	for _, tl := range c.Tools {
		names = append(names, tl.Function.Name)
		var want, got any
		_ = json.Unmarshal(tl.Function.Parameters, &want)
		if err := json.Unmarshal([]byte(OpenCodeToolSchemas[tl.Function.Name]), &got); err != nil {
			t.Fatalf("%s: %v", tl.Function.Name, err)
		}
		w, _ := json.Marshal(want)
		g, _ := json.Marshal(got)
		if string(w) != string(g) {
			t.Errorf("%s:\n got %s\nwant %s", tl.Function.Name, g, w)
		}
		if tl.Function.Strict == nil || *tl.Function.Strict {
			t.Fatalf("the capture's %s isn't strict false", tl.Function.Name)
		}
	}
	if !slices.Equal(names, OpenCodeTools) {
		t.Fatalf("tools %v, OpenCode 2.2.5 sent %v", OpenCodeTools, names)
	}
	// the Test button's probe offers them so, with the block
	probe := zenFreeProbe(Chat, `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`)
	var m struct {
		Messages []struct{ Role, Content string }
		Tools    []struct {
			Function struct {
				Name   string
				Strict *bool
			}
		}
		Stream        bool
		StreamOptions map[string]bool `json:"stream_options"`
	}
	if err := json.Unmarshal([]byte(probe), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Tools) != 5 || m.Tools[0].Function.Strict == nil || *m.Tools[0].Function.Strict || !m.Stream || !m.StreamOptions["include_usage"] {
		t.Fatalf("probe %s", probe)
	}
	if len(m.Messages) != 2 || m.Messages[0].Role != "system" || !OpenCodeHasEnvironment(m.Messages[0].Content) {
		t.Fatalf("probe's prompt %s", probe)
	}
	for proto, key := range map[Protocol]string{Responses: "instructions", Anthropic: "system"} {
		var o map[string]any
		_ = json.Unmarshal([]byte(zenFreeProbe(proto, `{"model":"m"}`)), &o)
		if s, _ := o[key].(string); !OpenCodeHasEnvironment(s) {
			t.Errorf("%s probe: %s %v", proto, key, o[key])
		}
	}
}

// The version OpenCode's updater says is latest is sent, read at most
// every six hours, never one older than 2.2.5 or not a release's.
func TestOpenCodeVersionFromUpdater(t *testing.T) {
	var asked atomic.Int32
	answer := atomic.Value{}
	answer.Store(`{"version":"2.3.1"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Query().Get("current") == "" {
			w.WriteHeader(400)
			return
		}
		switch a := answer.Load().(string); a {
		case "500":
			w.WriteHeader(500)
		case "redirect":
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		default:
			w.Write([]byte(a))
		}
	}))
	defer srv.Close()
	for a, want := range map[string]string{
		`{"version":"2.3.1"}`:      "2.3.1",
		`{"version":"2.2.5"}`:      "2.2.5",
		`{"version":"2.10.0"}`:     "2.10.0",
		`{"version":"2.2.4"}`:      "",
		`{"version":"1.19.0"}`:     "",
		`{"version":"2.3.0-beta"}`: "",
		`{"version":"latest"}`:     "",
		`{"version":3}`:            "",
		`not json`:                 "",
		"500":                      "",
		"redirect":                 "",
	} {
		answer.Store(a)
		if got := openCodeAskVersion(srv.URL, OpenCodeVersion); got != want {
			t.Errorf("%s: %q, want %q", a, got, want)
		}
	}

	answer.Store(`{"version":"2.3.1"}`)
	asked.Store(0)
	openCodeVersion.Lock()
	savedV, savedAt := openCodeVersion.v, openCodeVersion.at
	openCodeVersion.v, openCodeVersion.at = "", time.Time{}
	openCodeVersion.Unlock()
	oldURL := openCodeVersionURL
	openCodeVersionURL = srv.URL
	t.Cleanup(func() {
		openCodeVersion.Lock()
		openCodeVersion.v, openCodeVersion.at, openCodeVersion.asking = savedV, savedAt, false
		openCodeVersion.Unlock()
		openCodeVersionURL = oldURL
	})
	if ua := OpenCodeUserAgent(); ua != "opencode/latest/2.2.5/cli" {
		t.Fatalf("before the updater answers: %q", ua)
	}
	deadline := time.Now().Add(10 * time.Second)
	for OpenCodeUserAgent() != "opencode/latest/2.3.1/cli" {
		if time.Now().After(deadline) {
			t.Fatalf("the updater's version never taken: %q", OpenCodeUserAgent())
		}
		time.Sleep(10 * time.Millisecond)
	}
	for range 5 {
		OpenCodeUserAgent()
	}
	if n := asked.Load(); n != 1 {
		t.Fatalf("updater asked %d times within six hours", n)
	}
	// six hours on, asked again; a read that fails keeps the last
	answer.Store("500")
	openCodeVersion.Lock()
	openCodeVersion.at = time.Now().Add(-openCodeVersionEvery - time.Minute)
	openCodeVersion.Unlock()
	OpenCodeUserAgent()
	for asked.Load() != 2 {
		if time.Now().After(deadline) {
			t.Fatal("not asked again after six hours")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		openCodeVersion.Lock()
		done := !openCodeVersion.asking
		openCodeVersion.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ua := OpenCodeUserAgent(); ua != "opencode/latest/2.3.1/cli" {
		t.Fatalf("after a failed read: %q", ua)
	}
}
