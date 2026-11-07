package plugins

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Status reports local state without a target, or checks a public website when
// a host/URL is supplied. Website checks do not depend on the IRC network.
type Status struct {
	client  *http.Client
	timeout time.Duration
}

func (p *Status) Name() string       { return "status" }
func (p *Status) Commands() []string { return []string{"status", "uptime", "ping"} }
func (p *Status) Help() string {
	return "!status [public host|http(s) URL] — HTTP status and response time (ports 80/443); without a target: bot connection, uptime, counters (aliases: !uptime, !ping; HTTP, not ICMP)"
}
func (p *Status) Init(cfg bot.PluginConfig, _ *storage.DB) error {
	seconds := cfg.Int("timeout_seconds", 6)
	if seconds < 1 || seconds > 15 {
		seconds = 6
	}
	p.timeout = time.Duration(seconds) * time.Second
	p.client = newStatusHTTPClient(p.timeout)
	return nil
}

func (p *Status) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isStatusCommand(cmd) {
		return false
	}
	if strings.TrimSpace(arg) != "" {
		target, err := parseStatusTarget(arg)
		if err != nil {
			b.Send(m.ReplyTarget(), ircColor(ircYellow, "Use one public hostname or http(s) URL on port 80/443; private addresses and credentials are not allowed."))
			return true
		}
		select {
		case statusCheckSlots <- struct{}{}:
			defer func() { <-statusCheckSlots }()
		default:
			b.Send(m.ReplyTarget(), ircColor(ircYellow, "Website checks are busy; try again shortly."))
			return true
		}
		timeout := p.timeout
		if timeout <= 0 {
			timeout = 6 * time.Second
		}
		client := p.client
		if client == nil {
			client = newStatusHTTPClient(timeout)
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		result, err := checkStatusSite(ctx, client, target)
		b.Send(m.ReplyTarget(), formatStatusSite(target, result, err))
		return true
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
