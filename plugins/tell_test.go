package plugins

import (
	"testing"
	"time"
)

func TestTellConfirmation(t *testing.T) {
	got := formatTellConfirmation("Echo")
	if got != "I'll tell Echo the next time they speak." {
		t.Fatalf("got %q", got)
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
