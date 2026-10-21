package plugins

import (
	"context"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
)

const (
	askHedgeDelay       = 650 * time.Millisecond
	askAssistPreference = 200 * time.Millisecond
)

type askLookupTask struct {
	delay  time.Duration
	lookup func(context.Context) (askSource, bool)
}

func (p *Ask) findSource(ctx context.Context, question string, cfg bot.PluginConfig) (askSource, bool) {
	if !cfg.Bool("search_assist_enabled", true) {
		return p.findSourceSequential(ctx, question, cfg)
	}
	// Give a healthy lightweight Search Assist request a head start. Its
	// existing normalized retries and final original-question recovery remain
	// available, but cannot hold independent answer providers behind a slow
	// request or an unavailable browser.
	tasks := []askLookupTask{{lookup: func(ctx context.Context) (askSource, bool) {
		return p.findSourceSequential(ctx, question, bot.PluginConfig{
			"search_assist_browser_enabled": false, "search_results_enabled": false,
			"duckduckgo_enabled": false, "wikidata_fallback": false,
		})
	}}}
	if cfg.Bool("search_assist_browser_enabled", true) {
		tasks = append(tasks, askLookupTask{delay: askHedgeDelay, lookup: func(ctx context.Context) (askSource, bool) {
			step, cancel := askBrowserStepContext(ctx)
			defer cancel()
			return askDuckDuckGoRenderedSearchAssist(step, question, cfg.String("browser_path", ""), cfg.Bool("search_results_enabled", true))
		}})
	}
	if cfg.Bool("duckduckgo_enabled", true) || cfg.Bool("wikidata_fallback", true) || cfg.Bool("search_results_enabled", true) {
		fallbackCfg := make(bot.PluginConfig, len(cfg)+2)
		for key, value := range cfg {
			fallbackCfg[key] = value
		}
		fallbackCfg["search_assist_enabled"] = false
		fallbackCfg["search_assist_browser_enabled"] = false
		tasks = append(tasks, askLookupTask{delay: askHedgeDelay, lookup: func(ctx context.Context) (askSource, bool) {
			return p.findSourceOnce(ctx, question, fallbackCfg)
		}})
	}
	return askRaceSources(ctx, tasks, askAssistPreference)
}

// askRaceSources returns the first usable answer, with a short preference
// window for Search Assist. It cancels and joins losing tasks before returning
// so lookups cannot outlive a request or accumulate orphaned browser workers.
func askRaceSources(parent context.Context, tasks []askLookupTask, preference time.Duration) (askSource, bool) {
	if parent.Err() != nil || len(tasks) == 0 {
		return askSource{}, false
	}
	ctx, cancel := context.WithCancel(parent)
	type outcome struct {
		source askSource
		found  bool
	}
	results := make(chan outcome, len(tasks))
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	for _, task := range tasks {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if !askWaitForRetry(ctx, task.delay) {
				results <- outcome{}
				return
			}
			source, found := runAskLookup(ctx, task.lookup)
			results <- outcome{source, found && source.Summary != ""}
		}()
	}
	var fallback askSource
	found := false
	var timer *time.Timer
	var preferenceExpired <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for remaining := len(tasks); remaining > 0; {
		select {
		case result := <-results:
			remaining--
			if result.found {
				if result.source.Provider == "search_assist" {
					return result.source, true
				}
				if !found {
					fallback, found = result.source, true
					timer = time.NewTimer(preference)
					preferenceExpired = timer.C
				}
			}
		case <-preferenceExpired:
			return fallback, found
		case <-ctx.Done():
			return fallback, found
		}
	}
	return fallback, found
}

// Provider workers run outside the bot's normal command-handler panic guard.
// Keep an upstream failure confined to its lane, without logging query data.
func runAskLookup(ctx context.Context, lookup func(context.Context) (askSource, bool)) (source askSource, found bool) {
	defer func() {
		if recover() != nil {
			source, found = askSource{}, false
		}
	}()
	return lookup(ctx)
}
