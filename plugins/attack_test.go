package plugins

import (
	"strings"
	"testing"
)

func TestAttackCommandParsing(t *testing.T) {
	style, target, ok := parseAttackArguments("attack", "slap Alice")
	if !ok || style != "slap" || target != "Alice" {
		t.Fatalf("canonical attack parse = (%q, %q, %v)", style, target, ok)
	}
	style, target, ok = parseAttackArguments("gift", "Alice")
	if !ok || style != "present" || target != "Alice" {
		t.Fatalf("alias attack parse = (%q, %q, %v)", style, target, ok)
	}
	if _, _, ok := parseAttackArguments("attack", "Alice"); ok {
		t.Fatal("attack without a style unexpectedly parsed")
	}
	if _, _, ok := parseAttackArguments("attack", "unknown Alice"); ok {
		t.Fatal("unknown attack style unexpectedly parsed")
	}
}

func TestAttackTargetsRejectUnsafeText(t *testing.T) {
	for _, target := range []string{"", "Alice Smith", "Alice\x01", "Alice\n", strings.Repeat("A", 31), "@Alice"} {
		if validAttackTarget(target) {
			t.Errorf("validAttackTarget(%q) = true, want false", target)
		}
	}
	for _, target := range []string{"Alice", "ak[Relay]", "self", "bot_2"} {
		if !validAttackTarget(target) {
			t.Errorf("validAttackTarget(%q) = false, want true", target)
		}
	}
}

func TestAttackSelfTargetDetection(t *testing.T) {
	if !isAttackSelfTarget("self", "GoBot") || !isAttackSelfTarget("gObOt", "GoBot") {
		t.Fatal("expected self and bot nickname targets to be detected")
	}
	if isAttackSelfTarget("Alice", "GoBot") {
		t.Fatal("ordinary target was incorrectly treated as self")
	}
}

func TestAttackTemplateAndActionFormatting(t *testing.T) {
	text := renderAttackTemplate("slaps {target} while {actor} tries not to laugh.", "Echo", "Alice")
	if text != "slaps Alice while Echo tries not to laugh." {
		t.Fatalf("rendered attack = %q", text)
	}
	action := formatAttackAction(text)
	if action != "\x01ACTION slaps Alice while Echo tries not to laugh.\x01" {
		t.Fatalf("formatted action = %q", action)
	}
	if strings.ContainsAny(action, "\r\n") {
		t.Fatalf("action contains a line break: %q", action)
	}
}

func TestAttackDefinitionsHaveSafeTemplates(t *testing.T) {
	plugin := &Attack{}
	commands := plugin.Commands()
	if len(commands) < len(attackDefinitions) {
		t.Fatalf("commands = %d, definitions = %d", len(commands), len(attackDefinitions))
	}
	for name, definition := range attackDefinitions {
		if definition.name != name || len(definition.templates) == 0 {
			t.Fatalf("invalid definition %q: %+v", name, definition)
		}
		for _, template := range definition.templates {
			output := renderAttackTemplate(template, "Echo", "Alice")
			if output == "" || strings.ContainsAny(output, "\r\n\x00") {
				t.Fatalf("unsafe or empty template for %q: %q", name, output)
			}
		}
	}
}
