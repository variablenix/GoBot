package plugins

import (
	"strings"
	"testing"

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

func TestAskSenderKeyUsesAccountWhenAvailable(t *testing.T) {
	key := askSenderKey(bot.Message{Nick: "Echo", Account: "UserAccount"})
	if key != "account:useraccount" {
		t.Fatalf("got %q", key)
	}
}
