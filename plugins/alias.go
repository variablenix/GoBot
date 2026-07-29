package plugins

import (
	"sort"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Alias struct{}

func (p *Alias) Name() string                                 { return "alias" }
func (p *Alias) Commands() []string                           { return []string{"alias"} }
func (p *Alias) Help() string                                 { return "!alias [command] — list command aliases" }
func (p *Alias) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }

func (p *Alias) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "alias" {
		return false
	}
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg != "" {
		if aliases, plugin, ok := aliasesFor(b.Plugins, arg); ok {
			if len(aliases) == 0 {
				b.Send(m.ReplyTarget(), "!"+plugin+" has no aliases")
			} else {
				b.Send(m.ReplyTarget(), "!"+plugin+" aliases: "+formatAliasNames(aliases))
			}
			return true
		}
		b.Send(m.ReplyTarget(), "unknown command; use !alias to list aliases")
		return true
	}
	b.Send(m.ReplyTarget(), "aliases: "+formatAliasGroups(b.Plugins))
	return true
}

func aliasesFor(plugins []bot.Plugin, name string) ([]string, string, bool) {
	for _, plugin := range plugins {
		if strings.EqualFold(plugin.Name(), name) {
			return pluginAliases(plugin), plugin.Name(), true
		}
		for _, command := range plugin.Commands() {
			if strings.EqualFold(command, name) {
				return pluginAliases(plugin), plugin.Name(), true
			}
		}
	}
	return nil, "", false
}

func pluginAliases(plugin bot.Plugin) []string {
	aliases := make([]string, 0)
	for _, command := range plugin.Commands() {
		if !strings.EqualFold(command, plugin.Name()) {
			aliases = append(aliases, command)
		}
	}
	sort.Strings(aliases)
	return aliases
}

func formatAliasNames(aliases []string) string {
	formatted := make([]string, len(aliases))
	for i, alias := range aliases {
		formatted[i] = "!" + alias
	}
	return strings.Join(formatted, ", ")
}

func formatAliasGroups(plugins []bot.Plugin) string {
	groups := make([]string, 0)
	for _, plugin := range plugins {
		aliases := pluginAliases(plugin)
		if len(aliases) == 0 {
			continue
		}
		groups = append(groups, "!"+plugin.Name()+" <- "+formatAliasNames(aliases))
	}
	sort.Strings(groups)
	return strings.Join(groups, " | ")
}
