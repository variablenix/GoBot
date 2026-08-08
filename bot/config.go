package bot

type Config struct {
	// Server, Identity, and Channels are retained for single-network config
	// compatibility. New installations should use Networks.
	Server   ServerConfig
	Identity IdentityConfig
	Channels []string
	Networks []NetworkConfig
	// PluginOverrides can disable individual plugins for a specific channel
	// on this network. A missing override leaves the global plugin setting in
	// effect.
	PluginOverrides map[string]map[string]bool `mapstructure:"plugin_overrides"`
	NetworkName     string
	CommandPrefix   string   `mapstructure:"command_prefix"`
	OwnerAccounts   []string `mapstructure:"owner_accounts"`
	RateLimit       struct {
		MessagesPerSecond             float64 `mapstructure:"messages_per_second"`
		Burst                         int
		CommandCooldownSeconds        int `mapstructure:"command_cooldown_seconds"`
		CommandWarningCooldownSeconds int `mapstructure:"command_warning_cooldown_seconds"`
		JoinWarmupSeconds             int `mapstructure:"join_warmup_seconds"`
	}
	Invites struct {
		Enabled         bool `mapstructure:"enabled"`
		CooldownSeconds int  `mapstructure:"cooldown_seconds"`
	}
	Plugins map[string]PluginConfig
	Stats   struct {
		Enabled       bool
		HTTPPort      int    `mapstructure:"http_port"`
		ListenAddress string `mapstructure:"listen_address"`
	}
	Storage struct {
		DBPath string `mapstructure:"db_path"`
	}
	Log struct{ Level, Format string }
}

type ServerConfig struct {
	Host       string
	Port       int
	TLS        bool `mapstructure:"tls"`
	VerifyCert bool `mapstructure:"verify_cert"`
	// ClientCert may point to a PEM containing the client certificate and
	// private key, as recommended by OuchNet for CertFP. ClientKey can be
	// supplied separately when the key is stored in its own file.
	ClientCert string `mapstructure:"client_cert"`
	ClientKey  string `mapstructure:"client_key"`
}

type IdentityConfig struct {
	Nick             string
	User             string
	Realname         string
	SASLUser         string `mapstructure:"sasl_user"`
	SASLPass         string `mapstructure:"sasl_pass"`
	SASLMechanism    string `mapstructure:"sasl_mechanism"`
	NickServFallback bool   `mapstructure:"nickserv_fallback"`
	NickServGhost    bool   `mapstructure:"nickserv_ghost"`
}

type NetworkConfig struct {
	Name            string
	Server          ServerConfig
	Identity        IdentityConfig
	Channels        []string
	PluginOverrides map[string]map[string]bool `mapstructure:"plugin_overrides"`
}
type PluginConfig map[string]interface{}

func (c PluginConfig) String(key, fallback string) string {
	if v, ok := c[key].(string); ok {
		return v
	}
	return fallback
}
func (c PluginConfig) Int(key string, fallback int) int {
	if v, ok := c[key].(int); ok {
		return v
	}
	if v, ok := c[key].(float64); ok {
		return int(v)
	}
	return fallback
}
func (c PluginConfig) Float(key string, fallback float64) float64 {
	if v, ok := c[key].(float64); ok {
		return v
	}
	if v, ok := c[key].(int); ok {
		return float64(v)
	}
	return fallback
}
func (c PluginConfig) Bool(key string, fallback bool) bool {
	if v, ok := c[key].(bool); ok {
		return v
	}
	return fallback
}
