package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortLoadsBidirectionalCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ports.txt")
	if err := os.WriteFile(path, []byte("22 SSH secure shell\n443 HTTPS secure web server\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Port{}
	if err := p.Init(map[string]interface{}{"data_file": path}, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.byPort[443]; got.Name != "HTTPS" || got.Description != "secure web server" {
		t.Fatalf("port lookup = %+v", got)
	}
	if got := p.byName["ssh"]; got.Port != 22 {
		t.Fatalf("service lookup = %+v", got)
	}
}
