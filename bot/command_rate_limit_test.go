package bot

import (
	"context"
	"testing"
)

func TestCommandBypassesCooldown(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "hit bypasses cooldown",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!hit"},
			want: true,
		},
		{
			name: "stand bypasses cooldown",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!stand"},
			want: true,
		},
		{
			name: "double bypasses cooldown",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!double"},
			want: true,
		},
		{
			name: "blackjack subcommand hit bypasses cooldown",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!21 hit"},
			want: true,
		},
		{
			name: "blackjack alias subcommand stand bypasses cooldown",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!bj stand"},
			want: true,
		},
		{
			name: "poll vote bypasses cooldown",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!poll vote 2"},
			want: true,
		},
		{
			name: "poll results bypasses cooldown",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!poll results"},
			want: true,
		},
		{
			name: "poll close bypasses cooldown",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!poll close"},
			want: true,
		},
		{
			name: "start blackjack still limited",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!21"},
			want: false,
		},
		{
			name: "poll create still limited",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!poll create lunch | tacos | pizza"},
			want: false,
		},
		{
			name: "help still limited",
			msg:  Message{Target: "#test", IsChannel: true, Text: "!help"},
			want: false,
		},
		{
			name: "non-command does not bypass",
			msg:  Message{Target: "#test", IsChannel: true, Text: "hello"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commandBypassesCooldown(tt.msg, "!")
			if got != tt.want {
				t.Fatalf("commandBypassesCooldown(%q) = %v, want %v", tt.msg.Text, got, tt.want)
			}
		})
	}
}

func TestAllowCommandInitializesState(t *testing.T) {
	b := &Bot{Queue: NewQueue(1000, 1, func(Outgoing) {})}
	t.Cleanup(func() { b.Queue.Drain(context.Background()) })
	m := Message{Nick: "alice", User: "u", Host: "host", Target: "#test", IsChannel: true, Text: "!help"}

	if !b.AllowCommand(m) {
		t.Fatal("first command should be allowed")
	}
	if b.lastCommands == nil || b.lastWarnings == nil {
		t.Fatal("rate-limit maps were not initialized")
	}
	if b.AllowCommand(m) {
		t.Fatal("second command inside the cooldown should be rejected")
	}
}
