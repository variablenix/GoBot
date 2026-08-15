package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/viper"
	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/plugins"
	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		panic(err)
	}

	networks := cfg.Networks
	if len(networks) == 0 && cfg.Server.Host != "" {
		networks = []bot.NetworkConfig{{Name: "default", Server: cfg.Server, Identity: cfg.Identity, Channels: cfg.Channels, PluginOverrides: cfg.PluginOverrides}}
	}
	if len(networks) == 0 {
		panic("no IRC networks configured")
	}
	for i := range networks {
		if cert, ok := os.LookupEnv(fmt.Sprintf("BOT_NETWORKS_%d_SERVER_CLIENT_CERT", i)); ok {
			networks[i].Server.ClientCert = cert
		}
		if key, ok := os.LookupEnv(fmt.Sprintf("BOT_NETWORKS_%d_SERVER_CLIENT_KEY", i)); ok {
			networks[i].Server.ClientKey = key
		}
		if mechanism, ok := os.LookupEnv(fmt.Sprintf("BOT_NETWORKS_%d_IDENTITY_SASL_MECHANISM", i)); ok {
			networks[i].Identity.SASLMechanism = mechanism
		}
		if user, ok := os.LookupEnv(fmt.Sprintf("BOT_NETWORKS_%d_IDENTITY_SASL_USER", i)); ok {
			networks[i].Identity.SASLUser = user
		}
		if pass, ok := os.LookupEnv(fmt.Sprintf("BOT_NETWORKS_%d_IDENTITY_SASL_PASS", i)); ok {
			networks[i].Identity.SASLPass = pass
		}
		if enroll, ok := os.LookupEnv(fmt.Sprintf("BOT_NETWORKS_%d_IDENTITY_CERTFP_ENROLL", i)); ok {
			value, parseErr := strconv.ParseBool(enroll)
			if parseErr != nil {
				panic(fmt.Sprintf("invalid BOT_NETWORKS_%d_IDENTITY_CERTFP_ENROLL: %v", i, parseErr))
			}
			networks[i].Identity.CertFPEnroll = value
		}
		if nickServ, ok := os.LookupEnv(fmt.Sprintf("BOT_NETWORKS_%d_IDENTITY_NICKSERV_NAME", i)); ok {
			networks[i].Identity.NickServName = nickServ
		}
		// Keep the Compose-friendly legacy variables convenient for a
		// one-network deployment using the new networks list.
		if len(networks) == 1 && i == 0 {
			if mechanism, ok := os.LookupEnv("BOT_SASL_MECHANISM"); ok {
				networks[i].Identity.SASLMechanism = mechanism
			}
			if user, ok := os.LookupEnv("BOT_SASL_USER"); ok {
				networks[i].Identity.SASLUser = user
			}
			if pass, ok := os.LookupEnv("BOT_SASL_PASS"); ok {
				networks[i].Identity.SASLPass = pass
			}
			if cert, ok := os.LookupEnv("BOT_TLS_CLIENT_CERT"); ok {
				networks[i].Server.ClientCert = cert
			}
			if key, ok := os.LookupEnv("BOT_TLS_CLIENT_KEY"); ok {
				networks[i].Server.ClientKey = key
			}
			if enroll, ok := os.LookupEnv("BOT_CERTFP_ENROLL"); ok {
				value, parseErr := strconv.ParseBool(enroll)
				if parseErr != nil {
					panic(fmt.Sprintf("invalid BOT_CERTFP_ENROLL: %v", parseErr))
				}
				networks[i].Identity.CertFPEnroll = value
			}
			if nickServ, ok := os.LookupEnv("BOT_NICKSERV_NAME"); ok {
				networks[i].Identity.NickServName = nickServ
			}
		}
		if networks[i].Identity.SASLPass != "" && networks[i].Identity.SASLUser == "" {
			networks[i].Identity.SASLUser = networks[i].Identity.Nick
		}
	}

	log := newLogger(cfg.Log.Format)
	defer log.Sync()
	db, err := storage.Open(cfg.Storage.DBPath)
	if err != nil {
		log.Fatal("database", zap.Error(err))
	}
	defer db.Close()

	stats := bot.NewStats(db)
	defer stats.Close()
	if cfg.Stats.Enabled {
		stats.Serve(cfg.Stats.ListenAddress, cfg.Stats.HTTPPort)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	instances := make([]*bot.Bot, 0, len(networks))
	var wg sync.WaitGroup
	for i, network := range networks {
		if network.Name == "" {
			network.Name = network.Server.Host
			if network.Name == "" {
				network.Name = fmt.Sprintf("network-%d", i+1)
			}
		}
		networkCfg := cfg
		networkCfg.NetworkName = network.Name
		networkCfg.Server = network.Server
		networkCfg.Identity = network.Identity
		networkCfg.Channels = network.Channels
		networkCfg.PluginOverrides = network.PluginOverrides
		allPlugins := plugins.All()
		instance := bot.NewWithStats(networkCfg, db, allPlugins, log, stats)
		for _, plugin := range allPlugins {
			enabled := true
			if c, ok := cfg.Plugins[plugin.Name()]; ok {
				enabled = c.Bool("enabled", true)
			}
			instance.SetPluginEnabled(plugin.Name(), enabled)
			if enabled {
				if err := plugin.Init(cfg.Plugins[plugin.Name()], db); err != nil {
					log.Fatal("plugin init", zap.String("plugin", plugin.Name()), zap.String("network", network.Name), zap.Error(err))
				}
				if starter, ok := plugin.(bot.Starter); ok {
					starter.Start(instance)
					instance.MarkPluginStarted(plugin.Name())
				}
			}
		}
		instances = append(instances, instance)
	}

	var reloadMu sync.Mutex
	reload := func(current *bot.Bot, msg bot.Message) {
		reloadMu.Lock()
		defer reloadMu.Unlock()
		updated, err := loadConfig()
		if err != nil {
			current.Send(msg.ReplyTarget(), "reload failed; configuration was not changed")
			log.Warn("configuration reload failed", zap.Error(err))
			return
		}
		count, err := current.ReloadPlugins(updated.Plugins)
		current.ReloadPluginOverrides(pluginOverridesForNetwork(updated, current.Config.NetworkName))
		if err != nil {
			current.Send(msg.ReplyTarget(), fmt.Sprintf("configuration reloaded with errors after %d plugin change(s); channel overrides applied; IRC connection unchanged", count))
			log.Warn("configuration reload partially failed", zap.Int("plugins", count), zap.Error(err))
			return
		}
		current.Send(msg.ReplyTarget(), fmt.Sprintf("configuration reloaded for %d plugin(s) and channel overrides; IRC connection unchanged", count))
		log.Info("configuration reloaded", zap.Int("plugins", count), zap.String("network", current.Config.NetworkName))
	}
	for _, instance := range instances {
		current := instance
		current.SetReloadHandler(func(msg bot.Message) { reload(current, msg) })
		wg.Add(1)
		go func(networkName string, b *bot.Bot) {
			defer wg.Done()
			if err := b.Run(ctx); err != nil {
				log.Error("bot stopped", zap.String("network", networkName), zap.Error(err))
			}
		}(current.Config.NetworkName, current)
	}
	wg.Wait()
	qctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, instance := range instances {
		instance.Queue.Drain(qctx)
	}
}

func pluginOverridesForNetwork(cfg bot.Config, networkName string) map[string]map[string]bool {
	if len(cfg.Networks) == 0 {
		return cfg.PluginOverrides
	}
	for i, network := range cfg.Networks {
		name := network.Name
		if name == "" {
			name = network.Server.Host
		}
		if name == "" {
			name = fmt.Sprintf("network-%d", i+1)
		}
		if strings.EqualFold(name, networkName) {
			return network.PluginOverrides
		}
	}
	return nil
}

func loadConfig() (bot.Config, error) {
	configureViper()
	if err := viper.ReadInConfig(); err != nil {
		return bot.Config{}, err
	}
	var cfg bot.Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return bot.Config{}, err
	}
	if cfg.Storage.DBPath == "" {
		cfg.Storage.DBPath = "bot.db"
	}
	if cfg.CommandPrefix == "" {
		cfg.CommandPrefix = "!"
	}
	if cfg.Stats.ListenAddress == "" {
		cfg.Stats.ListenAddress = "127.0.0.1"
	}
	return cfg, nil
}

func configureViper() {
	viper.Reset()
	viper.SetConfigFile("config.yaml")
	viper.AutomaticEnv()
	viper.SetEnvPrefix("BOT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	bind := func(key, env string) {
		if err := viper.BindEnv(key, env); err != nil {
			panic(err)
		}
	}
	bind("identity.sasl_pass", "BOT_SASL_PASS")
	bind("identity.sasl_mechanism", "BOT_SASL_MECHANISM")
	bind("identity.certfp_enroll", "BOT_CERTFP_ENROLL")
	bind("identity.nickserv_name", "BOT_NICKSERV_NAME")
	bind("server.client_cert", "BOT_TLS_CLIENT_CERT")
	bind("server.client_key", "BOT_TLS_CLIENT_KEY")
	bind("plugins.news.api_key", "BOT_NEWS_API_KEY")
	bind("plugins.lastfm.api_key", "BOT_LASTFM_API_KEY")
	bind("plugins.github.token", "BOT_GITHUB_TOKEN")
	bind("plugins.urltitle.youtube_api_key", "BOT_YOUTUBE_API_KEY")
	bind("plugins.youtube.api_key", "BOT_YOUTUBE_API_KEY")
	bind("storage.db_path", "BOT_STORAGE_DB_PATH")
	bind("stats.listen_address", "BOT_STATS_LISTEN_ADDRESS")
}

func newLogger(format string) *zap.Logger {
	cfg := zap.NewProductionConfig()
	if format != "json" {
		cfg.Encoding = "console"
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	l, _ := cfg.Build()
	return l
}
