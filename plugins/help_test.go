package plugins

import (
	"context"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestHelpLongResponseSplitsIntoBoundedPMParts(t *testing.T) {
	response := strings.Repeat("plugin, ", 100) + "details"
	parts := splitIRCText(response, helpChannelMaxBytes)
	if len(parts) < 2 {
		t.Fatalf("long help response produced %d part(s)", len(parts))
	}
	for _, part := range parts {
		if len([]byte(part)) > helpChannelMaxBytes {
			t.Fatalf("help part is %d bytes, want at most %d", len([]byte(part)), helpChannelMaxBytes)
		}
	}
}

func TestHelpShortResponseStaysOneMessage(t *testing.T) {
	parts := splitIRCText("plugins: ask, help, weather", helpChannelMaxBytes)
	if len(parts) != 1 {
		t.Fatalf("short help response produced %d parts", len(parts))
	}
}

func TestHelpLongChannelResponseUsesPMAndShortNotice(t *testing.T) {
	var sent []bot.Outgoing
	b := &bot.Bot{Queue: bot.NewQueue(1000, 100, func(message bot.Outgoing) {
		sent = append(sent, message)
	})}
	response := strings.Repeat("plugin, ", 100) + "details"
	sendHelpResponse(b, bot.Message{Nick: "Alice", Target: "#chat", IsChannel: true}, response)
	b.Queue.Drain(context.Background())

	pmCount, noticeCount := 0, 0
	for _, message := range sent {
		if message.Target == "Alice" {
			pmCount++
		}
		if message.Target == "#chat" && strings.Contains(message.Text, "Check your PM") {
			noticeCount++
		}
	}
	if pmCount < 2 || noticeCount != 1 {
		t.Fatalf("sent messages = %+v, want multiple PM parts and one channel notice", sent)
	}
}
