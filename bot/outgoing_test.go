package bot

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOutgoingPluginTextIsBoundedWithoutLosingSuffix(t *testing.T) {
	for _, target := range []string{"#test", "#" + strings.Repeat("c", 180)} {
		text := strings.Repeat("界🎥", 250) + " https://example.com/result"
		parts := outgoingMessageParts(target, text)
		if len(parts) < 2 || strings.Join(parts, "") != text {
			t.Fatal("large output lost content or its trailing link")
		}
		for _, part := range parts {
			if !utf8.ValidString(part) || len("PRIVMSG "+target+" :"+part+"\r\n") > 512 {
				t.Fatalf("invalid UTF-8 or oversized IRC line: %d bytes", len(part))
			}
		}
	}
}

func TestOutgoingTextPreservesShortFormattingAndBlocksInjection(t *testing.T) {
	text := "\x0308GOLDEN DUCK\x0f 🎥"
	if parts := outgoingMessageParts("#test", text); len(parts) != 1 || parts[0] != text {
		t.Fatalf("short formatted output changed: %q", parts)
	}
	for _, target := range []string{"", "#test\r\nOPER injected", "#test other", "#a,#b", ":bad", strings.Repeat("c", 600)} {
		if parts := outgoingMessageParts(target, "hello"); len(parts) != 0 {
			t.Errorf("invalid target %q accepted", target)
		}
	}
	parts := outgoingMessageParts("#test", "hello\r\nQUIT :injected\x00\xff")
	if len(parts) != 1 || parts[0] != "hello  QUIT :injected" {
		t.Fatalf("unsafe characters survived: %q", parts)
	}
}

func TestOutgoingColorSequenceIsNotCut(t *testing.T) {
	limit := 512 - len("PRIVMSG #test :\r\n")
	text := strings.Repeat("x", limit-3) + "\x0308,04" + strings.Repeat("gold", 150)
	parts := outgoingMessageParts("#test", text)
	if !strings.HasPrefix(parts[1], "\x0308,04") {
		t.Fatalf("color sequence split: %q", parts[:2])
	}
	if len(outgoingMessageParts("#test", strings.Repeat("x", 100000))) != 10 {
		t.Fatal("output amplification is not bounded")
	}
}

func TestSendAppliesSafetyBeforeQueueing(t *testing.T) {
	sent := make(chan Outgoing, 10)
	b := &Bot{Queue: NewQueue(1, 1, func(message Outgoing) { sent <- message })}
	b.Send("#test", strings.Repeat("x", 700)+"\r\n")
	b.Queue.Drain(context.Background())
	if len(sent) != 2 {
		t.Fatalf("expected two safe messages, got %d", len(sent))
	}
	for len(sent) > 0 {
		message := <-sent
		if len(message.Text) > 497 || strings.ContainsAny(message.Text, "\r\n") {
			t.Fatal("Send queued an unsafe message")
		}
	}
}
