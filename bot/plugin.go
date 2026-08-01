package bot

import "github.com/variablenix/GoBot/storage"

type Plugin interface {
	Name() string
	Commands() []string
	Help() string
	Init(PluginConfig, *storage.DB) error
	Handle(*Bot, Message) bool
}

type Starter interface {
	Start(*Bot)
}

// EventHandler lets a plugin observe non-message IRC events without making
// every command plugin inspect the raw event stream.
type EventHandler interface {
	HandleEvent(*Bot, Message) bool
}
