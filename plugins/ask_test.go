package plugins

import (
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
)

func TestAskCommandAliases(t *testing.T) {
	for _, command := range []string{"ask", "question", "q"} {
		if !isAskCommand(command) {
			t.Errorf("expected %q to be an ask command", command)
		}
	}
	if isAskCommand("asker") {
		t.Error("unexpectedly accepted invalid ask command")
	}
}

func TestAskResponseIsSingleLineAndBounded(t *testing.T) {
	answer := strings.Repeat("This is a useful answer. ", 40) + "\nignore this line break"
	got := formatAskResponse("Echo", answer, "https://en.wikipedia.org/wiki/Go_(programming_language)", 180, 120)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("response contains a line break: %q", got)
	}
	if len([]byte(got)) > 180 {
		t.Fatalf("response is %d bytes, want at most 180: %q", len([]byte(got)), got)
	}
	if !strings.Contains(got, "Read more: https://en.wikipedia.org/wiki/Go_(programming_language)") {
		t.Fatalf("response lost source link: %q", got)
	}
}

func TestAskResponseDropsInvalidSourceURL(t *testing.T) {
	got := formatAskResponse("Echo", "A short answer", "javascript:alert(1)", 360, 240)
	if strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe URL was included: %q", got)
	}
}

func TestAskProviderNoneDoesNotCallAI(t *testing.T) {
	p := &Ask{}
	if rewritten, ok := p.rewrite(t.Context(), "What is Go?", askSource{Title: "Go", Summary: "A programming language."}); ok || rewritten != "" {
		t.Fatalf("provider none unexpectedly rewrote answer: %q, %v", rewritten, ok)
	}
}

func TestAskEnvironmentOverridesNestedConfig(t *testing.T) {
	t.Setenv("BOT_ASK_PROVIDER", "openrouter")
	t.Setenv("BOT_ASK_AI_REWRITE", "true")

	p := &Ask{}
	if err := p.Init(bot.PluginConfig{
		"provider":   "none",
		"ai_rewrite": false,
		"max_length": 360,
	}, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	cfg := p.configSnapshot()
	if got := cfg.String("provider", "none"); got != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", got)
	}
	if !cfg.Bool("ai_rewrite", false) {
		t.Fatal("ai_rewrite remained disabled despite BOT_ASK_AI_REWRITE=true")
	}
}

func TestAskInvalidEnvironmentBooleanLeavesConfigUnchanged(t *testing.T) {
	t.Setenv("BOT_ASK_AI_REWRITE", "not-a-boolean")

	p := &Ask{}
	if err := p.Init(bot.PluginConfig{"ai_rewrite": true}, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if !p.configSnapshot().Bool("ai_rewrite", false) {
		t.Fatal("invalid environment boolean unexpectedly changed config")
	}
}

func TestAskProviderInfoUsesEnvironmentCredentials(t *testing.T) {
	t.Setenv("BOT_OPENROUTER_API_KEY", "secret")
	t.Setenv("BOT_OPENROUTER_MODEL", "nvidia/test:free")
	model, configured := askProviderInfo("openrouter", bot.PluginConfig{"provider": "openrouter"})
	if model != "nvidia/test:free" || !configured {
		t.Fatalf("provider info = (%q, %v), want configured test model", model, configured)
	}
}

func TestAskReloadUpdatesConfigAndPreservesCooldownState(t *testing.T) {
	t.Setenv("BOT_ASK_PROVIDER", "")
	t.Setenv("BOT_ASK_AI_REWRITE", "")
	p := &Ask{}
	if err := p.Init(bot.PluginConfig{"provider": "none", "ai_rewrite": false}, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.last["account:test"] = time.Now()
	if err := p.Reload(bot.PluginConfig{"provider": "openrouter", "ai_rewrite": true}); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	cfg := p.configSnapshot()
	if cfg.String("provider", "none") != "openrouter" || !cfg.Bool("ai_rewrite", false) {
		t.Fatalf("reload did not update config: %#v", cfg)
	}
	if _, ok := p.last["account:test"]; !ok {
		t.Fatal("reload discarded cooldown state")
	}
}

func TestAskSenderKeyUsesAccountWhenAvailable(t *testing.T) {
	key := askSenderKey(bot.Message{Nick: "Echo", Account: "UserAccount"})
	if key != "account:useraccount" {
		t.Fatalf("got %q", key)
	}
}

func TestUsableAskRewriteRejectsProviderMetaText(t *testing.T) {
	for _, answer := range []string{
		"The user asks: what is Linux?",
		"The source does not define that topic.",
		"According to the source, the answer is unclear.",
		"Not enough information in this source.",
	} {
		if usableAskRewrite(answer) {
			t.Errorf("usableAskRewrite(%q) = true, want false", answer)
		}
	}
	if !usableAskRewrite("Linux is a family of open-source operating systems.") {
		t.Fatal("usableAskRewrite rejected a direct factual answer")
	}
}
