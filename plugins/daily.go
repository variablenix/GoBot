package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const dailyClaimBucket = "daily_claims"

type dailyClaim struct {
	Date   string `json:"date"`
	Streak int    `json:"streak"`
}

// Daily awards one persistent bonus per user per UTC calendar day. Claims are
// unlimited across different users; the same account cannot claim twice in a
// day even from another channel or network.
type Daily struct {
	cfgMu   sync.RWMutex
	mu      sync.Mutex
	db      *storage.DB
	bonusXP int64
	claims  map[string]dailyClaim
}

var dailyNow = time.Now
var dailyClaimsMu sync.Mutex

func (p *Daily) Name() string       { return "daily" }
func (p *Daily) Commands() []string { return []string{"daily"} }
func (p *Daily) Help() string {
	return "!daily — claim one persistent daily bonus per UTC day"
}

func (p *Daily) Init(c bot.PluginConfig, db *storage.DB) error {
	bonusXP := int64(c.Int("bonus_xp", 25))
	if bonusXP < 1 {
		bonusXP = 1
	}
	if bonusXP > 1000000 {
		bonusXP = 1000000
	}
	p.cfgMu.Lock()
	p.bonusXP = bonusXP
	p.cfgMu.Unlock()
	p.mu.Lock()
	p.db = db
	if p.claims == nil {
		p.claims = make(map[string]dailyClaim)
	}
	p.mu.Unlock()
	return nil
}

func (p *Daily) Reload(c bot.PluginConfig) error {
	p.mu.Lock()
	db := p.db
	p.mu.Unlock()
	return p.Init(c, db)
}

func (p *Daily) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "daily" {
		return false
	}
	if !m.IsChannel || strings.TrimSpace(m.Nick) == "" {
		b.Send(m.ReplyTarget(), "!daily is available in channels only")
		return true
	}
	if !b.PluginEnabledForChannel("duckhunt", m.Target) {
		b.Send(m.ReplyTarget(), "daily bonuses are unavailable while Duck Hunt is disabled here")
		return true
	}
	duckhunt := findDuckHunt(b)
	if duckhunt == nil {
		b.Send(m.ReplyTarget(), "daily bonuses are unavailable")
		return true
	}

	identity := dailyIdentity(b, m)
	today := dailyNow().UTC().Format("2006-01-02")
	claim, alreadyClaimed, err := p.claim(identity, today)
	if err != nil {
		b.Send(m.ReplyTarget(), "daily bonus storage is temporarily unavailable")
		return true
	}
	if alreadyClaimed {
		b.Send(m.ReplyTarget(), "daily bonus already claimed today — come back tomorrow!")
		return true
	}

	p.cfgMu.RLock()
	bonusXP := p.bonusXP
	p.cfgMu.RUnlock()
	if _, err := duckhunt.AwardXP(b.Config.NetworkName, m.Target, m.Nick, bonusXP); err != nil {
		p.rollback(identity, claim)
		b.Send(m.ReplyTarget(), "daily bonus could not be saved; please try again later")
		return true
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s > Daily bonus claimed! +%d XP (%d-day streak!) Come back tomorrow!", m.Nick, bonusXP, claim.Streak))
	return true
}

func findDuckHunt(b *bot.Bot) *DuckHunt {
	for _, plugin := range b.Plugins {
		if duckhunt, ok := plugin.(*DuckHunt); ok && b.PluginEnabled(duckhunt.Name()) {
			return duckhunt
		}
	}
	return nil
}

func dailyIdentity(b *bot.Bot, m bot.Message) string {
	account := strings.TrimSpace(m.Account)
	if account != "" && account != "*" {
		return "account:" + strings.ToLower(account)
	}
	return "sender:" + strings.ToLower(strings.TrimSpace(b.Config.NetworkName)) + "\x00" + strings.ToLower(strings.TrimSpace(m.Nick))
}

func dailyClaimKey(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func (p *Daily) claim(identity, today string) (dailyClaim, bool, error) {
	key := dailyClaimKey(identity)
	dailyClaimsMu.Lock()
	defer dailyClaimsMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	previous, exists := p.claims[key]
	if p.db != nil {
		exists = false
		raw, err := p.db.Get(dailyClaimBucket, key)
		if err == nil {
			if storage.Decode(raw, &previous) != nil {
				return dailyClaim{}, false, errors.New("invalid daily claim record")
			}
			exists = true
		} else if !errors.Is(err, storage.ErrNotFound) {
			return dailyClaim{}, false, err
		}
	}
	if exists && previous.Date == today {
		return previous, true, nil
	}
	streak := 1
	if exists {
		yesterday := dailyNow().UTC().AddDate(0, 0, -1).Format("2006-01-02")
		if previous.Date == yesterday {
			streak = previous.Streak + 1
		}
	}
	claim := dailyClaim{Date: today, Streak: streak}
	if p.db != nil {
		if err := p.db.Set(dailyClaimBucket, key, claim); err != nil {
			return dailyClaim{}, false, err
		}
	}
	if p.claims == nil {
		p.claims = make(map[string]dailyClaim)
	}
	p.claims[key] = claim
	return claim, false, nil
}

func (p *Daily) rollback(identity string, claim dailyClaim) {
	key := dailyClaimKey(identity)
	dailyClaimsMu.Lock()
	defer dailyClaimsMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if current, ok := p.claims[key]; ok && current == claim {
		delete(p.claims, key)
	}
	if p.db != nil {
		_ = p.db.Delete(dailyClaimBucket, key)
	}
}
