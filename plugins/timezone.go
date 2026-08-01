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
	return "!time <IANA timezone or city> — show the current time, for example !time Seoul or !time America/Los_Angeles"
}
func (p *Timezone) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }
func (p *Timezone) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "time" && cmd != "tz") {
		return false
	}
	zone := strings.TrimSpace(arg)
	if zone == "" || len(zone) > 64 || strings.Contains(zone, "..") || strings.HasPrefix(zone, "/") {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !time <IANA timezone>, for example !time America/Los_Angeles"))
		return true
	}
	zone = resolveTimezoneAlias(zone)
	location, err := time.LoadLocation(zone)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "unknown timezone; use an IANA name like America/Los_Angeles"))
		return true
	}
	now := time.Now().In(location)
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s%s%s: %s", ircCyan, zone, ircReset, now.Format("2006-01-02 15:04:05 MST")))
	return true
}

var timezoneAliases = map[string]string{
	"utc": "UTC", "gmt": "UTC",
	"pst": "America/Los_Angeles", "est": "America/New_York", "cst": "America/Chicago", "mst": "America/Denver",
	"seoul": "Asia/Seoul", "tokyo": "Asia/Tokyo", "osaka": "Asia/Tokyo", "beijing": "Asia/Shanghai", "shanghai": "Asia/Shanghai",
	"hong kong": "Asia/Hong_Kong", "hong-kong": "Asia/Hong_Kong", "taipei": "Asia/Taipei", "singapore": "Asia/Singapore",
	"bangkok": "Asia/Bangkok", "jakarta": "Asia/Jakarta", "manila": "Asia/Manila", "kuala lumpur": "Asia/Kuala_Lumpur", "kuala-lumpur": "Asia/Kuala_Lumpur",
	"delhi": "Asia/Kolkata", "new delhi": "Asia/Kolkata", "mumbai": "Asia/Kolkata", "dubai": "Asia/Dubai", "jerusalem": "Asia/Jerusalem",
	"sydney": "Australia/Sydney", "melbourne": "Australia/Melbourne", "perth": "Australia/Perth", "auckland": "Pacific/Auckland",
	"london": "Europe/London", "paris": "Europe/Paris", "berlin": "Europe/Berlin", "rome": "Europe/Rome", "madrid": "Europe/Madrid",
	"amsterdam": "Europe/Amsterdam", "brussels": "Europe/Brussels", "vienna": "Europe/Vienna", "zurich": "Europe/Zurich", "moscow": "Europe/Moscow",
	"cairo": "Africa/Cairo", "johannesburg": "Africa/Johannesburg", "nairobi": "Africa/Nairobi",
	"new york": "America/New_York", "new-york": "America/New_York", "los angeles": "America/Los_Angeles", "los-angeles": "America/Los_Angeles",
	"san francisco": "America/Los_Angeles", "san-francisco": "America/Los_Angeles", "chicago": "America/Chicago", "denver": "America/Denver",
	"toronto": "America/Toronto", "vancouver": "America/Vancouver", "mexico city": "America/Mexico_City", "mexico-city": "America/Mexico_City",
	"sao paulo": "America/Sao_Paulo", "sao-paulo": "America/Sao_Paulo", "buenos aires": "America/Argentina/Buenos_Aires", "buenos-aires": "America/Argentina/Buenos_Aires",
}

func resolveTimezoneAlias(zone string) string {
	if alias, ok := timezoneAliases[strings.ToLower(strings.TrimSpace(zone))]; ok {
		return alias
	}
	return zone
}
