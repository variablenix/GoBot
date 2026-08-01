package plugins

import (
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestLinuxCommandsAndBounds(t *testing.T) {
	p := &Linux{}
	if got := p.Commands(); len(got) != 2 || got[0] != "linux" || got[1] != "kernel" {
		t.Fatalf("unexpected commands: %v", got)
	}
	if err := p.Init(bot.PluginConfig{"timeout_seconds": 60, "max_length": 20}, nil); err != nil {
		t.Fatal(err)
	}
	if got := linuxTimeout(p.cfg); got.String() != "8s" {
		t.Fatalf("timeout = %s, want 8s", got)
	}
}

func TestFormatKernelBanner(t *testing.T) {
	input := "The latest stable version of the Linux kernel is: 6.16.9\nThe latest longterm version of the Linux kernel is: 6.12.45\n"
	got := formatKernelBanner(input)
	want := "Linux kernel versions: stable 6.16.9, longterm 6.12.45"
	if got != want {
		t.Fatalf("formatKernelBanner() = %q, want %q", got, want)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Fatal("kernel output contains a line break")
	}
}
