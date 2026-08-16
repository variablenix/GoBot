package plugins

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
)

// scopedCooldown is deliberately local to the plugin. Bot.AllowCommand gives
// every command a sender cooldown, while these cooldowns express the tighter
// channel/user scopes required by individual games.
type scopedCooldown struct {
	mu       sync.Mutex
	cooldown time.Duration
	last     map[string]time.Time
}

const maxScopedCooldownEntries = 10000

func (c *scopedCooldown) configure(seconds, fallback int) {
	if seconds < 1 {
		seconds = fallback
	}
	c.cooldown = time.Duration(seconds) * time.Second
	c.last = make(map[string]time.Time)
}

func (c *scopedCooldown) allow(key string) bool {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		c.last = make(map[string]time.Time)
	}
	if previous, ok := c.last[key]; ok && now.Sub(previous) < c.cooldown {
		return false
	}
	if len(c.last) >= maxScopedCooldownEntries {
		for existing, previous := range c.last {
			if existing != key && now.Sub(previous) >= c.cooldown {
				delete(c.last, existing)
			}
		}
	}
	if len(c.last) >= maxScopedCooldownEntries {
		for existing := range c.last {
			if existing != key {
				delete(c.last, existing)
				break
			}
		}
	}
	c.last[key] = now
	return true
}

func secureRandomInt(limit int64) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("random limit must be positive")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(limit))
	if err != nil {
		return 0, fmt.Errorf("secure random selection: %w", err)
	}
	return n.Int64(), nil
}

func maxNonNegative(value int) int {
	if value < 0 {
		return 0
	}
	if value > 1_000_000_000 {
		return 1_000_000_000
	}
	return value
}

func pluginIdentity(m bot.Message) string {
	if account := strings.TrimSpace(m.Account); account != "" && account != "*" {
		return "account:" + strings.ToLower(account)
	}
	return "nick:" + strings.ToLower(strings.TrimSpace(m.Nick))
}

func scopedKey(network, channel, identity string) string {
	parts := []string{network, channel, identity}
	for index, part := range parts {
		parts[index] = base64.RawURLEncoding.EncodeToString([]byte(strings.ToLower(part)))
	}
	return strings.Join(parts, "\x00")
}

const (
	maxWordleFileBytes = 2 << 20
	maxTriviaFileBytes = 1 << 20
)

func readBoundedRegularFile(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", path, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", path, maxBytes)
	}
	return data, nil
}
