package memory

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What agents save reaches the digest when it is published, never at the
// save itself, so an agent's instructions don't change mid-session.
func TestDigestChangesOnlyWhenPublished(t *testing.T) {
	at := fresh(t)
	save(t, "Answer in Chinese", Preference, Global)
	if d, _ := Digest("codex"); d != "" {
		t.Fatalf("unpublished digest %q", d)
	}
	if changed, err := Publish(); err != nil || !changed {
		t.Fatalf("publish: %v, %v", changed, err)
	}
	d, _ := Digest("codex")
	if !strings.Contains(d, "Preferences:\n- Answer in Chinese") {
		t.Fatalf("digest %q", d)
	}
	save(t, "Keep commits small", Rule, Global)
	if d2, _ := Digest("codex"); d2 != d {
		t.Fatal("a save changed the digest")
	}
	*at = at.Add(time.Hour)
	if changed, _ := PublishDue(); changed {
		t.Fatal("published again within a day")
	}
	*at = at.Add(24 * time.Hour)
	if changed, _ := PublishDue(); !changed {
		t.Fatal("not published after a day")
	}
	if d, _ := Digest("codex"); !strings.Contains(d, "Rules:\n- Keep commits small") {
		t.Fatalf("digest after a day %q", d)
	}
	if changed, _ := Publish(); changed {
		t.Fatal("an unchanged publish said it changed")
	}
}

// A digest read after memory's own write has the write, even when the
// file kept its size and modification time, as two writes within one tick
// of a coarse file system clock leave it (#5999).
func TestDigestReadsMemorysOwnWriteWithinATick(t *testing.T) {
	fresh(t)
	m := save(t, "Answer in Chinese", Preference, Global)
	if _, err := Publish(); err != nil {
		t.Fatal(err)
	}
	if d, _ := Digest("codex"); !strings.Contains(d, "- Answer in Chinese") {
		t.Fatalf("digest %q", d)
	}
	before, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Edit(m.ID, Input{Text: str("Answer in English")}, false); err != nil {
		t.Fatal(err)
	}
	if changed, err := Publish(); err != nil || !changed {
		t.Fatalf("publish: %v, %v", changed, err)
	}
	sameTick(t, Path(), before)
	if d, _ := Digest("codex"); !strings.Contains(d, "- Answer in English") {
		t.Fatalf("digest after the edit was published %q", d)
	}
}

// Another magpie's write within the same tick is seen, even one that
// left the file's size as it was.
func TestDigestReadsAnotherMagpiesWriteWithinATick(t *testing.T) {
	fresh(t)
	save(t, "Answer in Chinese", Preference, Global)
	if _, err := Publish(); err != nil {
		t.Fatal(err)
	}
	if d, _ := Digest("codex"); !strings.Contains(d, "- Answer in Chinese") {
		t.Fatalf("digest %q", d)
	}
	before, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), bytes.ReplaceAll(b, []byte("Chinese"), []byte("English")), 0o600); err != nil {
		t.Fatal(err)
	}
	sameTick(t, Path(), before)
	if d, _ := Digest("codex"); !strings.Contains(d, "- Answer in English") {
		t.Fatalf("digest after another magpie's write %q", d)
	}
}

// The digest is of the memory.json at Path, not one read before from
// another path with the same size and modification time.
func TestDigestIsOfTheFileAtPath(t *testing.T) {
	fresh(t)
	save(t, "Answer in Chinese", Preference, Global)
	if _, err := Publish(); err != nil {
		t.Fatal(err)
	}
	a := Path()
	st, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(other, bytes.ReplaceAll(b, []byte("Chinese"), []byte("English")), 0o600); err != nil {
		t.Fatal(err)
	}
	sameTick(t, other, st)
	if d, _ := Digest("codex"); !strings.Contains(d, "- Answer in Chinese") {
		t.Fatalf("digest %q", d)
	}
	Path = func() string { return other }
	if d, _ := Digest("codex"); !strings.Contains(d, "- Answer in English") {
		t.Fatalf("digest of the other file %q", d)
	}
}

// sameTick gives the file at p the modification time of before, after
// checking it has before's size: what two writes of the same size within
// one tick of the file system's clock leave.
func sameTick(t *testing.T, p string, before os.FileInfo) {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != before.Size() {
		t.Fatalf("%s has %d bytes, %d before: the test needs the same size", p, st.Size(), before.Size())
	}
	if err := os.Chtimes(p, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
}

func TestDigestPartsAndBudget(t *testing.T) {
	at := fresh(t)
	old := globalBudget
	globalBudget = 120
	t.Cleanup(func() { globalBudget = old })

	save(t, strings.Repeat("a", 60), Fact, Global)
	*at = at.Add(time.Minute)
	save(t, strings.Repeat("b", 60), Fact, Global)
	pin := save(t, "pinned one", Episode, Global)
	tr := true
	if _, err := Edit(pin.ID, Input{Pinned: &tr}, true); err != nil {
		t.Fatal(err)
	}
	save(t, "only codex sees this", Rule, "agent:codex")
	p := &Project{Key: "github.com/yetone/magpie", Name: "magpie", Roots: []string{"/w/magpie"}}
	if _, _, err := Save(Input{Text: str("tests run on mote"), Kind: str(Rule), Scope: str(ScopeOf(p))}, FromAgent, nil, p); err != nil {
		t.Fatal(err)
	}
	gone := save(t, "archived one", Rule, Global)
	if _, err := Forget(gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(); err != nil {
		t.Fatal(err)
	}
	d, _ := Digest("codex")
	for _, want := range []string{"pinned one", strings.Repeat("b", 60), "Only for you:\n" + "Rules:\n- only codex sees this", "When working in /w/magpie:\nRules:\n- tests run on mote"} {
		if !strings.Contains(d, want) {
			t.Errorf("digest lacks %q:\n%s", want, d)
		}
	}
	// over budget: the older of the two long ones is left out, whole
	for _, not := range []string{strings.Repeat("a", 60), "archived one"} {
		if strings.Contains(d, not) {
			t.Errorf("digest has %q:\n%s", not, d)
		}
	}
	if other, _ := Digest("claude"); strings.Contains(other, "only codex") {
		t.Errorf("another agent got codex's own:\n%s", other)
	}
	for _, name := range []string{"magpie", "Magpie", "OpenAI", "Anthropic"} {
		if strings.Contains(strings.SplitN(d, "\n\n", 3)[1], name) {
			t.Errorf("the digest's header names %s", name)
		}
	}
}
