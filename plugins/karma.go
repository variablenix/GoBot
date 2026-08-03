package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Karma struct {
	db *storage.DB
	rx *regexp.Regexp
}

const karmaChannelsBucket = "karma_channels"

var karmaMu sync.Mutex

func (p *Karma) Name() string       { return "karma" }
func (p *Karma) Commands() []string { return []string{"karma"} }
func (p *Karma) Help() string       { return "!karma <thing> — show karma; thing++ or thing-- changes it" }
func (p *Karma) Init(_ bot.PluginConfig, d *storage.DB) error {
	p.db = d
	p.rx = regexp.MustCompile(`(?i)(^|[^a-z0-9_-])([a-z0-9_][a-z0-9_-]{0,30})(\+\+|--)`)
	return nil
}
func (p *Karma) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok {
		channel := ""
		if m.IsChannel {
			channel = m.Target
		}
		if updates := p.applyTextChanges(b.Config.NetworkName, channel, m.Text); len(updates) > 0 {
			b.Send(m.ReplyTarget(), formatKarmaUpdates(updates))
			return true
		}
		return false
	}
	if cmd != "karma" {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(arg))
	if key == "" {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !karma <thing>"))
		return true
	}
	channel := ""
	if m.IsChannel {
		channel = m.Target
	}
	channelValue, globalValue := p.readTotals(b.Config.NetworkName, channel, key)
	v := globalValue
	if channel != "" {
		v = channelValue
	}
	color := ircYellow
	if v > 0 {
		color = ircGreen
	} else if v < 0 {
		color = ircRed
	}
	message := fmt.Sprintf("%s has karma of %s%+d%s", key, color, v, ircReset)
	if channel != "" {
		message += fmt.Sprintf(" (🎯 %d in %s | 🌐 %d global)", channelValue, channel, globalValue)
	}
	b.Send(m.ReplyTarget(), message)
	return true
}

type karmaUpdate struct {
	key          string
	delta        int
	channel      string
	channelValue int
	globalValue  int
}

var karmaMilestones = []int{10, 25, 50, 100}

func (p *Karma) applyTextChanges(network, channel, text string) []karmaUpdate {
	if p.db == nil || p.rx == nil {
		return nil
	}
	updates := make([]karmaUpdate, 0)
	for _, match := range p.rx.FindAllStringSubmatchIndex(text, -1) {
		if len(match) < 8 {
			continue
		}
		// The trailing boundary is checked here instead of in the regular
		// expression so a separator can be reused by the next match.
		if match[1] < len(text) && isKarmaWordByte(text[match[1]]) {
			continue
		}
		key := strings.ToLower(text[match[4]:match[5]])
		delta := -1
		if text[match[6]:match[7]] == "++" {
			delta = 1
		}
		channelValue, globalValue, err := p.changeScoped(network, channel, key, delta)
		if err != nil {
			continue
		}
		updates = append(updates, karmaUpdate{key: key, delta: delta, channel: channel, channelValue: channelValue, globalValue: globalValue})
	}
	return updates
}

func formatKarmaUpdates(updates []karmaUpdate) string {
	if len(updates) == 0 {
		return ""
	}
	details := make([]string, 0, len(updates))
	positive, negative := true, true
	for _, update := range updates {
		if update.delta <= 0 {
			positive = false
		}
		if update.delta >= 0 {
			negative = false
		}
		details = append(details, formatKarmaUpdateDetail(update))
	}
	message := ""
	if positive {
		message = ircColor(ircGreen, "🆙 Karma boost! "+strings.Join(details, ", ")+" ✨ 🌟 💫")
	} else if negative {
		message = ircColor(ircRed, "Karma dip! "+strings.Join(details, ", ")+" 📉 🌀 💥 😬")
	} else {
		message = ircColor(ircCyan, "Karma update! "+strings.Join(details, ", ")+" ✨ 📊 🔄 🌟")
	}
	scopes := make([]string, 0)
	for _, update := range updates {
		if scope := formatKarmaScope(update); scope != "" {
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) > 0 {
		message += " " + strings.Join(scopes, " ")
	}

	milestones := make([]string, 0)
	for _, update := range updates {
		if milestone := crossedKarmaMilestone(update); milestone > 0 {
			milestones = append(milestones, fmt.Sprintf("%s has reached %+d karma! 🏆", update.key, milestone))
		}
	}
	if len(milestones) > 0 {
		message += " " + ircColor(ircTan, strings.Join(milestones, " "))
	}
	return message
}

func crossedKarmaMilestone(update karmaUpdate) int {
	if update.delta <= 0 || update.channel == "" || update.channelValue <= 0 {
		return 0
	}
	previous := update.channelValue - update.delta
	reached := 0
	for _, milestone := range karmaMilestones {
		if previous < milestone && update.channelValue >= milestone {
			reached = milestone
		}
	}
	return reached
}

func formatKarmaUpdateDetail(update karmaUpdate) string {
	if update.channel == "" {
		return fmt.Sprintf("%s %+d (global total %+d)", update.key, update.delta, update.globalValue)
	}
	action := "gained"
	amount := update.delta
	if amount < 0 {
		action = "lost"
		amount = -amount
	}
	return fmt.Sprintf("%s %s %d karma", update.key, action, amount)
}

func formatKarmaScope(update karmaUpdate) string {
	if update.channel == "" {
		return ""
	}
	return fmt.Sprintf("(🎯 %d in %s | 🌐 %d global)", update.channelValue, update.channel, update.globalValue)
}

func isKarmaWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_' || value == '-'
}

func (p *Karma) readTotals(network, channel, key string) (int, int) {
	if p.db == nil {
		return 0, 0
	}
	karmaMu.Lock()
	defer karmaMu.Unlock()
	global := readKarmaValue(p.db, "karma", key)
	if channel == "" {
		return global, global
	}
	return readKarmaValue(p.db, karmaChannelsBucket, karmaChannelKey(network, channel, key)), global
}

func (p *Karma) changeScoped(network, channel, key string, delta int) (int, int, error) {
	if p.db == nil {
		return 0, 0, fmt.Errorf("karma storage is unavailable")
	}
	karmaMu.Lock()
	defer karmaMu.Unlock()
	global, err := readKarmaValueStrict(p.db, "karma", key)
	if err != nil {
		return 0, 0, err
	}
	if channel == "" {
		global += delta
		if err := p.db.Set("karma", key, global); err != nil {
			return 0, 0, err
		}
		return global, global, nil
	}
	channelKey := karmaChannelKey(network, channel, key)
	channelValue, err := readKarmaValueStrict(p.db, karmaChannelsBucket, channelKey)
	if err != nil {
		return 0, 0, err
	}
	global += delta
	channelValue += delta
	if err := p.db.SetMany(
		storage.Entry{Bucket: "karma", Key: key, Value: global},
		storage.Entry{Bucket: karmaChannelsBucket, Key: channelKey, Value: channelValue},
	); err != nil {
		return 0, 0, err
	}
	return channelValue, global, nil
}

func (p *Karma) change(key string, delta int) (int, error) {
	_, global, err := p.changeScoped("", "", key, delta)
	return global, err
}

func readKarmaValue(db *storage.DB, bucket, key string) int {
	value, _ := readKarmaValueStrict(db, bucket, key)
	return value
}

func readKarmaValueStrict(db *storage.DB, bucket, key string) (int, error) {
	raw, err := db.Get(bucket, key)
	if errors.Is(err, storage.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	value := 0
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, err
	}
	return value, nil
}

func karmaChannelKey(network, channel, key string) string {
	return strings.ToLower(strings.TrimSpace(network)) + "\x00" + strings.ToLower(strings.TrimSpace(channel)) + "\x00" + key
}
