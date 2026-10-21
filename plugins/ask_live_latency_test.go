package plugins

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
)

// This opt-in check never connects to IRC or logs question text or answers.
func TestLiveAskLatency(t *testing.T) {
	if os.Getenv("GOBOT_LIVE_ASK") != "1" {
		t.Skip("opt-in live Ask latency and answer check")
	}
	queries := []string{"what is Debian?", "who created Linux?", "how much do dirt bikes cost vs GoKarts?"}
	if raw := os.Getenv("GOBOT_LIVE_ASK_QUERIES"); raw != "" {
		if json.Unmarshal([]byte(raw), &queries) != nil || len(queries) == 0 || len(queries) > 8 {
			t.Fatal("invalid live question list")
		}
	}
	for index, question := range queries {
		t.Run("query-"+strconv.Itoa(index+1), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			started := time.Now()
			source, found := (&Ask{}).findSource(ctx, question, bot.PluginConfig{})
			t.Logf("provider=%s found=%t elapsed_ms=%d", source.Provider, found, time.Since(started).Milliseconds())
			if !found || source.Summary == "" {
				t.Fatal("no sourced answer within the command deadline")
			}
		})
	}
}
