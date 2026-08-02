package plugins

import (
	"sort"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Help struct{ plugins []bot.Plugin }

func (p *Help) Name() string                                 { return "help" }
func (p *Help) Commands() []string                           { return []string{"help"} }
func (p *Help) Help() string                                 { return "!help [command] — list commands or show detailed help" }
func (p *Help) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }
func (p *Help) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "help" {
		return false
	}
	if strings.TrimSpace(arg) != "" {
		for _, x := range b.Plugins {
			if m.IsChannel && !b.PluginEnabledForChannel(x.Name(), m.Target) {
				continue
			}
			if strings.EqualFold(x.Name(), strings.TrimSpace(arg)) {
				b.Send(m.ReplyTarget(), ircColor(ircCyan, x.Help()))
				return true
			}
			for _, c := range x.Commands() {
				if c == strings.ToLower(strings.TrimSpace(arg)) {
					b.Send(m.ReplyTarget(), ircColor(ircCyan, x.Help()))
					return true
				}
			}
		}
	}
	var names []string
	for _, x := range b.Plugins {
		if m.IsChannel && !b.PluginEnabledForChannel(x.Name(), m.Target) {
			continue
		}
		names = append(names, x.Name())
	}
	sort.Strings(names)
	b.Send(m.ReplyTarget(), ircBold+"plugins:"+ircReset+" "+strings.Join(names, ", ")+" — use !help <plugin> for details")
	return true
}
