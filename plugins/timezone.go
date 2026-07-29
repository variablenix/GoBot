package plugins

import (
	"fmt"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Timezone struct{}

func (p *Timezone) Name() string       { return "time" }
func (p *Timezone) Commands() []string { return []string{"time", "tz"} }
func (p *Timezone) Help() string {
	return "!time <IANA timezone> — show the current time, for example !time America/Los_Angeles"
}
func (p *Timezone) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }
func (p *Timezone) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "time" && cmd != "tz") {
		return false
	}
	zone := strings.TrimSpace(arg)
	if zone == "" || len(zone) > 64 || strings.Contains(zone, "..") || strings.HasPrefix(zone, "/") {
		b.Send(m.ReplyTarget(), "usage: !time <IANA timezone>, for example !time America/Los_Angeles")
		return true
	}
	aliases := map[string]string{"pst": "America/Los_Angeles", "est": "America/New_York", "cst": "America/Chicago", "mst": "America/Denver", "utc": "UTC"}
	if alias, exists := aliases[strings.ToLower(zone)]; exists {
		zone = alias
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		b.Send(m.ReplyTarget(), "unknown timezone; use an IANA name like America/Los_Angeles")
		return true
	}
	now := time.Now().In(location)
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: %s", zone, now.Format("2006-01-02 15:04:05 MST")))
	return true
}
