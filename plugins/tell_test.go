package plugins

import (
	"strings"
	"testing"
	"time"
)

func TestTellConfirmation(t *testing.T) {
	got := formatTellConfirmation("Echo")
	if got != "I'll tell Echo the next time they speak." {
		t.Fatalf("got %q", got)
	}
}

func TestTellRecognizesSelf(t *testing.T) {
	if !isSelfTellTarget("echo", "Echo") {
		t.Fatal("expected self target to be recognized case-insensitively")
	}
	if isSelfTellTarget("EchoBot", "Echo") {
		t.Fatal("did not expect partial nickname match")
	}
	for _, response := range selfTellResponses {
		if strings.TrimSpace(response) == "" {
			t.Fatal("self response must not be empty")
		}
	}
	if strings.TrimSpace(selfTellResponse()) == "" {
		t.Fatal("random self response must not be empty")
	}
}

func TestTellKeySeparatesNetworksAndNickCase(t *testing.T) {
	if got := tellKey("Network", "Echo"); got != "network\x00echo" {
		t.Fatalf("got key %q", got)
	}
	if tellKey("network-one", "Echo") == tellKey("network-two", "Echo") {
		t.Fatal("expected tell queues on different networks to be separate")
	}
}

func TestTellRecordStoresSenderMessageAndTime(t *testing.T) {
	at := time.Now()
	message := tellRecord{Nick: "Alice", Text: "hello", At: at}
	if message.Nick != "Alice" || message.Text != "hello" || !message.At.Equal(at) {
		t.Fatalf("unexpected tell record: %+v", message)
	}
}
