package plugins

import (
	"fmt"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Status reports local process and connection state without making a network
// request. It is intentionally useful when external APIs are unavailable.
type Status struct{}

func (p *Status) Name() string       { return "status" }
func (p *Status) Commands() []string { return []string{"status", "uptime", "ping"} }
func (p *Status) Help() string {
	return "!status — show connection, uptime, and counters (aliases: !uptime, !ping)"
}
func (p *Status) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }

func (p *Status) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isStatusCommand(cmd) {
		return false
	}
	if b.Stats == nil {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "GoBot status is unavailable"))
		return true
	}
	snapshot := b.Stats.Snapshot()
	connected := "disconnected"
	if value, ok := snapshot["connected"].(bool); ok && value {
		connected = "connected"
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("GoBot: %s | uptime %v | network %s | received %v | sent %v | commands %v", connected, snapshot["uptime"], strings.TrimSpace(b.Config.NetworkName), snapshot["messages_received"], snapshot["messages_sent"], snapshot["commands_handled"]))
	return true
}

func isStatusCommand(command string) bool {
	switch strings.ToLower(command) {
	case "status", "uptime", "ping":
		return true
	default:
		return false
	}
}
