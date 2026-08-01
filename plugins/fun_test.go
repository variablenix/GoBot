package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFunLoadsAllCatalogs(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"yo_momma.txt":   "is so prepared, the backup plan has a backup plan.\n",
		"one_liners.txt": "A short joke.\n",
		"puns.txt":       "A punny line.\n",
		"wisdom.txt":     "Write it down before you need it.\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var plugin Fun
	if err := plugin.Init(map[string]interface{}{"data_dir": dir}, nil); err != nil {
		t.Fatal(err)
	}
	for _, category := range funCategories {
		if len(plugin.items[category]) != 1 {
			t.Fatalf("expected one %s entry, got %#v", category, plugin.items[category])
		}
	}
}

func TestFunUsesFallbacks(t *testing.T) {
	var plugin Fun
	if err := plugin.Init(map[string]interface{}{"data_dir": t.TempDir()}, nil); err != nil {
		t.Fatal(err)
	}
	for _, category := range funCategories {
		if len(plugin.items[category]) == 0 {
			t.Fatalf("expected fallback entries for %s", category)
		}
	}
}

func TestFunCommandCategories(t *testing.T) {
	tests := map[string]string{"yo": "yomomma", "oneliners": "oneliner", "puns": "pun", "wise": "wisdom"}
	for command, want := range tests {
		if got, ok := funCommandCategory(command); !ok || got != want {
			t.Fatalf("funCommandCategory(%q) = %q, %v; want %q, true", command, got, ok, want)
		}
	}
	if _, ok := funCommandCategory("unknown"); ok {
		t.Fatal("unknown command should not map to a fun category")
	}
}
