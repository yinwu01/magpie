package catalog

import (
	"strings"
	"testing"
)

// datedCatalog is models.dev's rows for OpenAI's GPT-6.1 Sol and GPT-4o
// and two Qwen Max entries, as models.dev/api.json had them on 2026-11-10,
// trimmed to their limits, levels and prices. models.dev lists gpt-6.1-sol
// and no dated id of it; it lists gpt-4o-2024-05-13 apart from gpt-4o, with
// a reply limit and a price of its own, and Qiniu's qwen-max-2025-01-25
// with a window four times Alibaba's qwen-max. It lists gpt-4o-2024-08-06
// too, at gpt-4o's numbers; the fixture leaves it out, as a snapshot newer
// than a catalog's copy would be.
const datedCatalog = `{
  "openai": {"models": {
    "gpt-6.1-sol": {"id":"gpt-6.1-sol","name":"GPT-6.1 Sol","reasoning":true,"reasoning_options":[{"type":"effort","values":["low","medium","high","xhigh","max"]}],"release_date":"2026-09-29","limit":{"context":1050000,"input":922000,"output":128000},"cost":{"input":2,"output":10,"cache_read":0.1,"cache_write":2.5}},
    "gpt-4o": {"id":"gpt-4o","name":"GPT-4o","reasoning":false,"release_date":"2024-05-13","limit":{"context":128000,"output":16384},"cost":{"input":2.5,"output":10,"cache_read":1.25}},
    "gpt-4o-2024-05-13": {"id":"gpt-4o-2024-05-13","name":"GPT-4o (2024-05-13)","reasoning":false,"release_date":"2024-05-13","limit":{"context":128000,"output":4096},"cost":{"input":5,"output":15}}
  }},
  "alibaba": {"models": {
    "qwen-max": {"id":"qwen-max","name":"Qwen Max","reasoning":false,"release_date":"2024-04-03","limit":{"context":32768,"output":8192},"cost":{"input":1.6,"output":6.4}}
  }},
  "qiniu-ai": {"models": {
    "qwen-max-2025-01-25": {"id":"qwen-max-2025-01-25","name":"Qwen2.5-Max-2025-01-25","reasoning":false,"release_date":"2024-04-03","limit":{"context":128000,"output":4096},"cost":{"input":0.34782609,"output":1.39130435}}
  }}
}`

// #5643: a relay lists GPT-6.1 Sol under OpenAI's dated snapshot id,
// gpt-6.1-sol-2026-09-29, which models.dev doesn't list. It has the
// model's window, reply limit and levels. A snapshot models.dev lists
// keeps its own, and what only looks like a date reads as nothing.
func TestDashedSnapshotDateLimits(t *testing.T) {
	writeCatalog(t, datedCatalog)
	for _, c := range []struct {
		id          string
		window, out int
	}{
		// the issue's id, as a relay and a gateway write it: what
		// gpt-6.1-sol has (its input limit, 922000, is the window)
		{"gpt-6.1-sol", 922000, 128000},
		{"gpt-6.1-sol-2026-09-29", 922000, 128000},
		{"openai/gpt-6.1-sol-2026-09-29", 922000, 128000},
		{"GPT-6.1-Sol-2026-09-29", 922000, 128000},
		// a snapshot the catalog doesn't list: its model's
		{"gpt-4o-2024-08-06", 128000, 16384},
		// listed as given, or under another spelling: its own
		{"gpt-4o-2024-05-13", 128000, 4096},
		{"openai/GPT-4o-2024-05-13", 128000, 4096},
		{"qwen-max-2025-01-25", 128000, 4096},
		{"qwen-max", 32768, 8192},
		// no day, or not a date: nothing
		{"gpt-6.1-sol-2026-13-01", 0, 0},
		{"gpt-6.1-sol-2026-02-31", 0, 0},
		{"gpt-6.1-sol-2026-00-10", 0, 0},
		{"gpt-6.1-sol-1999-09-29", 0, 0},
		{"gpt-6.1-sol-2026-09", 0, 0},
		{"gpt-6.1-sol-09-2026", 0, 0},
		{"gpt-6.1-sol-09-2025", 0, 0},
		{"gpt-6.1-sol-29-09-2026", 0, 0},
		{"gpt-6.1-sol-2026-preview", 0, 0},
		{"gpt-6.1-sol-2026-9-29", 0, 0},
		// another model with a date: nothing, not the model it starts with
		{"gpt-6.1-sol-mini-2026-09-29", 0, 0},
		{"gpt-6.1-sol-2026-09-29-preview", 0, 0},
		{"model-x-2026-01-01", 0, 0},
		{"2026-09-29", 0, 0},
	} {
		if got := ContextOf(c.id); got != c.window {
			t.Errorf("ContextOf(%q) = %d, want %d", c.id, got, c.window)
		}
		if got := OutputOf(c.id); got != c.out {
			t.Errorf("OutputOf(%q) = %d, want %d", c.id, got, c.out)
		}
	}
	if got := strings.Join(EffortsOf("gpt-6.1-sol-2026-09-29"), ","); got != "low,medium,high,xhigh,max" {
		t.Errorf("EffortsOf = %q, want low,medium,high,xhigh,max", got)
	}
	if !Knows("gpt-6.1-sol-2026-09-29") || !Thinks("gpt-6.1-sol-2026-09-29") {
		t.Error("Knows/Thinks: the dated id is a model models.dev knows")
	}
	// the usage report sends only an id models.dev knows as written
	if KnowsAsWritten("gpt-6.1-sol-2026-09-29") {
		t.Error("KnowsAsWritten took the dated id for gpt-6.1-sol")
	}
	// the id itself is kept: only what is known of it is borrowed
	if got := NameOf("gpt-6.1-sol-2026-09-29"); got != "" {
		t.Errorf("NameOf = %q: the model's name stands for a dated id", got)
	}
}

// The date is dropped for the catalog's lookups alone. SameModel, which
// groups models across providers and names an auto group (provider's
// AutoGroupID), keeps it, and so does Spelling, which an auto group's id
// is matched by: a pinned snapshot stays a group of its own.
func TestDashedSnapshotStaysItsOwnModel(t *testing.T) {
	writeCatalog(t, datedCatalog)
	for _, id := range []string{"gpt-6.1-sol-2026-09-29", "gpt-4o-2024-08-06"} {
		base := id[:len(id)-len("-2026-09-29")]
		if SameModel(id) == SameModel(base) {
			t.Errorf("SameModel(%q) = SameModel(%q) = %q", id, base, SameModel(id))
		}
	}
	if got := Spelling("gpt-6.1-sol-2026-09-29"); got != "" {
		t.Errorf("Spelling = %q, want none: an auto group's id is matched by it", got)
	}
	if got := Spelling("GPT-4o-2024-05-13"); got != "gpt-4o-2024-05-13" {
		t.Errorf("Spelling(GPT-4o-2024-05-13) = %q", got)
	}
	// a provider's own levels and a vendor's live list match as before:
	// OpenAI's list carries both ids, and the dated one isn't given the
	// model's name or levels there
	if got, ok := ListedBy([]string{"openai"}, "gpt-6.1-sol-2026-09-29"); ok {
		t.Errorf("ListedBy(openai, gpt-6.1-sol-2026-09-29) = %v", got)
	}
	out := Decorate([]Model{{ID: "gpt-6.1-sol"}, {ID: "gpt-6.1-sol-2026-09-29"}}, Provider("openai"))
	if m := out[1]; m.Name != "" && m.Name != m.ID || len(m.Efforts) > 0 || m.Context != 0 || m.Price != nil {
		t.Errorf("Decorate filled the dated id from gpt-6.1-sol: %+v", m)
	}
	if m := out[0]; len(m.Efforts) == 0 {
		t.Errorf("Decorate: gpt-6.1-sol itself got no levels: %+v", m)
	}
}

// A price is never borrowed for a dated id: OpenAI priced
// gpt-4o-2024-05-13 at twice gpt-4o, so a snapshot models.dev doesn't
// price isn't given its model's price, while one it prices keeps its own.
func TestDashedSnapshotIsNotPriced(t *testing.T) {
	writeCatalog(t, datedCatalog)
	for _, id := range []string{"gpt-6.1-sol-2026-09-29", "gpt-4o-2024-08-06"} {
		if pr, ok := PricedBy([]string{"openai"}, id); ok {
			t.Errorf("PricedBy(openai, %q) = %+v", id, pr)
		}
	}
	if pr, ok := PricedBy([]string{"openai"}, "gpt-4o-2024-05-13"); !ok || pr.Input != 5 || pr.Output != 15 {
		t.Errorf("PricedBy(openai, gpt-4o-2024-05-13) = %+v %v, want 5/15", pr, ok)
	}
}

func TestUndated(t *testing.T) {
	for in, want := range map[string]string{
		"gpt-6-1-sol-2026-09-29": "gpt-6-1-sol",
		"gpt-4o-2024-02-29":      "gpt-4o",
		"gpt-4o-2023-02-29":      "",
		"gpt-4o-2024-08-6":       "",
		"gpt-4o-2024_08_06":      "",
		"gpt-4o-20240806":        "",
		"-2024-08-06":            "",
		"2024-08-06":             "",
	} {
		got, ok := undated(in)
		if got != want || ok != (want != "") {
			t.Errorf("undated(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
}
