package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
	viper.SetConfigFile("config.yaml")
	viper.AutomaticEnv()
	viper.SetEnvPrefix("BOT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	_ = viper.ReadInConfig()
	viper.BindEnv("identity.sasl_pass", "BOT_SASL_PASS")
	viper.BindEnv("plugins.news.api_key", "BOT_NEWS_API_KEY")
	viper.BindEnv("plugins.lastfm.api_key", "BOT_LASTFM_API_KEY")
	viper.BindEnv("plugins.github.token", "BOT_GITHUB_TOKEN")
	viper.BindEnv("plugins.urltitle.youtube_api_key", "BOT_YOUTUBE_API_KEY")
	viper.BindEnv("storage.db_path", "BOT_STORAGE_DB_PATH")
	viper.BindEnv("stats.listen_address", "BOT_STATS_LISTEN_ADDRESS")

	var cfg bot.Config
	if err := viper.Unmarshal(&cfg); err != nil {
		panic(err)
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

	networks := cfg.Networks
	if len(networks) == 0 && cfg.Server.Host != "" {
		networks = []bot.NetworkConfig{{Name: "default", Server: cfg.Server, Identity: cfg.Identity, Channels: cfg.Channels, PluginOverrides: cfg.PluginOverrides}}
	}
	if len(networks) == 0 {
		panic("no IRC networks configured")
	}
	for i := range networks {
		if user, ok := os.LookupEnv(fmt.Sprintf("BOT_NETWORKS_%d_IDENTITY_SASL_USER", i)); ok {
			networks[i].Identity.SASLUser = user
		}
		if pass, ok := os.LookupEnv(fmt.Sprintf("BOT_NETWORKS_%d_IDENTITY_SASL_PASS", i)); ok {
			networks[i].Identity.SASLPass = pass
		}
		// Keep the Compose-friendly legacy variables convenient for a
		// one-network deployment using the new networks list.
		if len(networks) == 1 && i == 0 {
			if user, ok := os.LookupEnv("BOT_SASL_USER"); ok {
				networks[i].Identity.SASLUser = user
			}
			if pass, ok := os.LookupEnv("BOT_SASL_PASS"); ok {
				networks[i].Identity.SASLPass = pass
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
		active := make([]bot.Plugin, 0)
		for _, p := range plugins.All() {
			if c, ok := cfg.Plugins[p.Name()]; ok && !c.Bool("enabled", true) {
				continue
			}
			if err := p.Init(cfg.Plugins[p.Name()], db); err != nil {
				log.Fatal("plugin init", zap.String("plugin", p.Name()), zap.String("network", network.Name), zap.Error(err))
			}
			active = append(active, p)
		}
		instance := bot.NewWithStats(networkCfg, db, active, log, stats)
		for _, plugin := range active {
			if starter, ok := plugin.(bot.Starter); ok {
				starter.Start(instance)
			}
		}
		instances = append(instances, instance)
		wg.Add(1)
		go func(networkName string, b *bot.Bot) {
			defer wg.Done()
			if err := b.Run(ctx); err != nil {
				log.Error("bot stopped", zap.String("network", networkName), zap.Error(err))
			}
		}(network.Name, instance)
	}
	wg.Wait()
	qctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, instance := range instances {
		instance.Queue.Drain(qctx)
	}
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
