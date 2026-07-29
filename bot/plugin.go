package bot

import "github.com/variablenix/GoBot/storage"

type Plugin interface {
	Name() string
	Commands() []string
	Help() string
	Init(PluginConfig, *storage.DB) error
	Handle(*Bot, Message) bool
}
