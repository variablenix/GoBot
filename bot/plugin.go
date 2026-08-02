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

// Stopper lets a plugin stop background work when it is disabled by a live
// configuration reload. Plugins without a Stopper remain loaded but must
// guard any background output with their live enablement state.
type Stopper interface {
	Stop(*Bot)
}

// EventHandler lets a plugin observe non-message IRC events without making
// every command plugin inspect the raw event stream.
type EventHandler interface {
	HandleEvent(*Bot, Message) bool
}

// Reloadable lets a long-running plugin apply configuration changes without
// rebuilding its state or dropping the IRC connection.
type Reloadable interface {
	Reload(PluginConfig) error
}
