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
	if err := os.WriteFile(path, []byte("# comment\nAPI|Application Programming Interface\nRCA|Root Cause Analysis\ninvalid line\n"), 0600); err != nil {
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
	if _, ok := plugin.entries["invalid line"]; ok {
		t.Fatal("malformed acronym entry was loaded")
	}
	if plain := stripPluginIRC(formatAcronymEntry(entry)); !strings.Contains(plain, "API — Application Programming Interface") {
		t.Fatalf("formatted acronym = %q", plain)
	}
}

func TestBundledAcronymCatalogIsBroadAndValid(t *testing.T) {
	plugin := &Acronym{}
	if err := plugin.Init(bot.PluginConfig{"data_file": filepath.Join("..", "data", "acronyms.txt")}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if len(plugin.entries) < 150 {
		t.Fatalf("bundled acronym catalog has only %d entries", len(plugin.entries))
	}
	for _, key := range []string{"api", "cvss", "dnssec", "ircv3", "mttr", "rbac", "sre", "tls", "xss"} {
		if entry, ok := plugin.entries[key]; !ok || entry.Expansion == "" {
			t.Fatalf("bundled catalog missing usable %q entry", key)
		}
	}
}
