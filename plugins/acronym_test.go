package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestAcronymLoadsCaseInsensitiveEntries(t *testing.T) {
	path := t.TempDir() + "/acronyms.txt"
	if err := os.WriteFile(path, []byte("# comment\nAPI|Application Programming Interface|technology\nAPI|Acute Pain Index|medical\nPCI DSS|Payment Card Industry Data Security Standard|security\nRCA|Root Cause Analysis\ninvalid line\n"), 0600); err != nil {
		t.Fatalf("write acronym file: %v", err)
	}
	plugin := &Acronym{}
	if err := plugin.Init(bot.PluginConfig{"data_file": path}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	entry, ok := plugin.entries["api"]
	if !ok || entry.Expansion != "Application Programming Interface" {
		t.Fatalf("loaded entries = %+v", plugin.entries)
	}
	if len(plugin.meanings["api"]) != 2 {
		t.Fatalf("API meanings = %+v", plugin.meanings["api"])
	}
	if contextEntry, ok := plugin.contextEntry("api", "medical"); !ok || contextEntry.Expansion != "Acute Pain Index" {
		t.Fatalf("medical API meaning = %+v, found=%v", contextEntry, ok)
	}
	if multiWord, ok := plugin.entries["pci dss"]; !ok || multiWord.Expansion == "" {
		t.Fatalf("multi-word acronym was not loaded: %+v", plugin.entries)
	}
	if _, ok := plugin.entries["invalid line"]; ok {
		t.Fatal("malformed acronym entry was loaded")
	}
	if plain := stripPluginIRC(formatAcronymEntry(entry)); !strings.Contains(plain, "API [technology] — Application Programming Interface") {
		t.Fatalf("formatted acronym = %q", plain)
	}
}

func TestAcronymFuzzySuggestion(t *testing.T) {
	plugin := &Acronym{entries: map[string]acronymEntry{
		"http":  {Name: "HTTP", Expansion: "Hypertext Transfer Protocol"},
		"https": {Name: "HTTPS", Expansion: "Hypertext Transfer Protocol Secure"},
	}}
	entry, ok := plugin.fuzzySuggestion("htpp")
	if !ok || entry.Name != "HTTP" {
		t.Fatalf("fuzzy suggestion = %+v, found=%v", entry, ok)
	}
	if _, ok := plugin.fuzzySuggestion("zzzzzz"); ok {
		t.Fatal("unexpected fuzzy suggestion for unrelated query")
	}
}

func TestBundledAcronymCatalogIsBroadAndValid(t *testing.T) {
	plugin := &Acronym{}
	if err := plugin.Init(bot.PluginConfig{"data_file": filepath.Join("..", "data", "acronyms.txt")}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if len(plugin.entries) < 350 {
		t.Fatalf("bundled acronym catalog has only %d entries", len(plugin.entries))
	}
	for _, key := range []string{"api", "ceo", "dna", "gpa", "http", "ircv3", "lol", "mri", "nato", "roi", "sre", "tls", "xss"} {
		if entry, ok := plugin.entries[key]; !ok || entry.Expansion == "" {
			t.Fatalf("bundled catalog missing usable %q entry", key)
		}
	}
	if len(plugin.meanings["api"]) < 2 {
		t.Fatal("bundled catalog should preserve multiple API meanings")
	}
	if entry, ok := plugin.contextEntry("api", "technology"); !ok || entry.Expansion != "Application Programming Interface" {
		t.Fatalf("bundled technology API meaning = %+v, found=%v", entry, ok)
	}
}
