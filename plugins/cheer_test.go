package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheerLoadsConfiguredLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cheers.txt")
	if err := os.WriteFile(path, []byte("\\o/ hooray!\n\n  Teamwork wins!  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Cheer{}
	if err := p.Init(map[string]interface{}{"cheers_file": path}, nil); err != nil {
		t.Fatal(err)
	}
	if len(p.cheers) != 2 || p.cheers[1] != "Teamwork wins!" {
		t.Fatalf("unexpected cheers: %#v", p.cheers)
	}
}

func TestCheerCommands(t *testing.T) {
	if got := (&Cheer{}).Commands(); len(got) != 2 {
		t.Fatalf("unexpected commands: %v", got)
	}
}
