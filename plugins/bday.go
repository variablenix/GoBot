package plugins

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

var birthdayPattern = regexp.MustCompile(`^(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$`)

type birthdayEntry struct {
	Network  string `json:"network"`
	Identity string `json:"identity"`
	Nick     string `json:"nick"`
	Birthday string `json:"birthday"`
}

type SeenRecord struct {
	Nick    string
	Channel string
}

type Bday struct {
	db           *storage.DB
	announce     bool
	announceHour int
	mu           sync.Mutex
	channels     map[string]struct{}
	announced    map[string]struct{}
	stop         chan struct{}
}

func (p *Bday) Name() string       { return "bday" }
func (p *Bday) Commands() []string { return []string{"bday", "birthday"} }
func (p *Bday) Help() string {
	return "!bday set <MM-DD>; !bday [nick|list|next|delete]; alias: !birthday"
}
func (p *Bday) Init(c bot.PluginConfig, db *storage.DB) error {
	p.db = db
	p.announce = c.Bool("announce", true)
	p.announceHour = c.Int("announce_hour", 9)
	if p.announceHour < 0 || p.announceHour > 23 {
		p.announceHour = 9
	}
	p.channels = make(map[string]struct{})
	p.announced = make(map[string]struct{})
	return nil
}

func (p *Bday) Start(b *bot.Bot) {
	p.mu.Lock()
	if p.stop != nil {
		p.mu.Unlock()
		return
	}
	for _, channel := range b.Config.Channels {
		if strings.TrimSpace(channel) != "" {
			p.channels[strings.ToLower(channel)] = struct{}{}
		}
	}
	stop := make(chan struct{})
	p.stop = stop
	p.mu.Unlock()
	p.announceIfDue(b, time.Now().UTC())
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				p.announceIfDue(b, now.UTC())
			case <-stop:
				return
			}
		}
	}()
}
func (p *Bday) Stop(_ *bot.Bot) {
	p.mu.Lock()
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
	p.mu.Unlock()
}
func (p *Bday) HandleEvent(_ *bot.Bot, m bot.Message) bool {
	if m.Command == "JOIN" && m.IsChannel {
		p.mu.Lock()
		p.channels[strings.ToLower(m.Target)] = struct{}{}
		p.mu.Unlock()
	}
	return false
}
func (p *Bday) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "bday" && cmd != "birthday") {
		return false
	}
	parts := strings.Fields(arg)
	action := "self"
	if len(parts) > 0 {
		action = strings.ToLower(parts[0])
	}
	switch action {
	case "set":
		if len(parts) != 2 || !validBirthday(parts[1]) {
			b.Send(m.ReplyTarget(), "usage: !bday set <MM-DD>")
			return true
		}
		if p.db == nil || p.save(b.Config.NetworkName, m, parts[1]) != nil {
			b.Send(m.ReplyTarget(), "birthday could not be saved")
		} else {
			b.Send(m.ReplyTarget(), "birthday saved")
		}
	case "delete":
		if p.db != nil {
			_ = p.db.Delete("bday", p.entryKey(b.Config.NetworkName, m))
		}
		b.Send(m.ReplyTarget(), "birthday deleted")
	case "list":
		b.Send(m.ReplyTarget(), p.listToday(b.Config.NetworkName, m.Target))
	case "next":
		b.Send(m.ReplyTarget(), p.next(b.Config.NetworkName))
	case "self":
		entry, found := p.findOwn(b.Config.NetworkName, m)
		if !found {
			b.Send(m.ReplyTarget(), "🎂 No birthday is saved for you")
		} else {
			b.Send(m.ReplyTarget(), fmt.Sprintf("🎂 Your birthday is %s", entry.Birthday))
		}
	default:
		entry, found := p.findNick(b.Config.NetworkName, parts[0])
		if !found {
			b.Send(m.ReplyTarget(), "no birthday is saved for that nick")
		} else {
			b.Send(m.ReplyTarget(), fmt.Sprintf("🎂 %s's birthday is %s", cleanExternalText(entry.Nick), entry.Birthday))
		}
	}
	return true
}

func validBirthday(value string) bool {
	if !birthdayPattern.MatchString(value) {
		return false
	}
	month := int(value[0]-'0')*10 + int(value[1]-'0')
	day := int(value[3]-'0')*10 + int(value[4]-'0')
	return time.Date(2024, time.Month(month), day, 0, 0, 0, 0, time.UTC).Month() == time.Month(month) && time.Date(2024, time.Month(month), day, 0, 0, 0, 0, time.UTC).Day() == day
}
func (p *Bday) entryKey(network string, m bot.Message) string {
	return scopedKey(network, "bday", pluginIdentity(m))
}
func (p *Bday) save(network string, m bot.Message, birthday string) error {
	return p.db.Set("bday", p.entryKey(network, m), birthdayEntry{Network: network, Identity: pluginIdentity(m), Nick: cleanExternalText(m.Nick), Birthday: birthday})
}
func (p *Bday) entries(network string) []birthdayEntry {
	if p.db == nil {
		return nil
	}
	keys, _ := p.db.List("bday")
	entries := make([]birthdayEntry, 0, len(keys))
	for _, key := range keys {
		if !strings.HasPrefix(key, scopedKey(network, "bday", "")) {
			continue
		}
		raw, err := p.db.Get("bday", key)
		if err != nil {
			continue
		}
		var entry birthdayEntry
		if storage.Decode(raw, &entry) == nil && validBirthday(entry.Birthday) {
			entries = append(entries, entry)
		}
	}
	return entries
}
func (p *Bday) findOwn(network string, m bot.Message) (birthdayEntry, bool) {
	if p.db == nil {
		return birthdayEntry{}, false
	}
	raw, err := p.db.Get("bday", p.entryKey(network, m))
	if err != nil {
		return birthdayEntry{}, false
	}
	var entry birthdayEntry
	return entry, storage.Decode(raw, &entry) == nil && validBirthday(entry.Birthday)
}
func (p *Bday) findNick(network, nick string) (birthdayEntry, bool) {
	for _, entry := range p.entries(network) {
		if strings.EqualFold(entry.Nick, strings.TrimSpace(nick)) {
			return entry, true
		}
	}
	return birthdayEntry{}, false
}
func (p *Bday) listToday(network, channel string) string {
	today := time.Now().UTC().Format("01-02")
	entries := p.entries(network)
	seen := make(map[string]bool)
	if p.db != nil {
		if keys, _ := p.db.List("seen"); len(keys) > 0 {
			for _, key := range keys {
				raw, err := p.db.Get("seen", key)
				if err != nil {
					continue
				}
				var record SeenRecord
				if json.Unmarshal(raw, &record) == nil && strings.EqualFold(record.Channel, channel) {
					seen[strings.ToLower(record.Nick)] = true
				}
			}
		}
	}
	filterSeen := len(seen) > 0
	nicks := make([]string, 0)
	for _, entry := range entries {
		if entry.Birthday == today && (!filterSeen || seen[strings.ToLower(entry.Nick)]) {
			nicks = append(nicks, cleanExternalText(entry.Nick))
		}
	}
	sort.Strings(nicks)
	if len(nicks) == 0 {
		return "no birthdays today"
	}
	return truncateRunes("🎂 Birthdays today: "+strings.Join(nicks, ", "), 400)
}
func (p *Bday) next(network string) string {
	today := time.Now().UTC()
	type upcoming struct {
		entry birthdayEntry
		days  int
	}
	var choices []upcoming
	for _, entry := range p.entries(network) {
		month := int(entry.Birthday[0]-'0')*10 + int(entry.Birthday[1]-'0')
		day := int(entry.Birthday[3]-'0')*10 + int(entry.Birthday[4]-'0')
		date := time.Date(today.Year(), time.Month(month), day, 0, 0, 0, 0, time.UTC)
		if date.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
			date = time.Date(today.Year()+1, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		}
		choices = append(choices, upcoming{entry: entry, days: int(date.Sub(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) / (24 * time.Hour))})
	}
	if len(choices) == 0 {
		return "no birthdays are saved"
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].days != choices[j].days {
			return choices[i].days < choices[j].days
		}
		return strings.ToLower(choices[i].entry.Nick) < strings.ToLower(choices[j].entry.Nick)
	})
	choice := choices[0]
	return fmt.Sprintf("🎂 Next birthday: %s on %s (%d days)", cleanExternalText(choice.entry.Nick), choice.entry.Birthday, choice.days)
}
func (p *Bday) announceIfDue(b *bot.Bot, now time.Time) {
	if !p.announce || now.Hour() < p.announceHour {
		return
	}
	today := now.Format("2006-01-02")
	p.mu.Lock()
	for key := range p.announced {
		if !strings.HasSuffix(key, "\x00"+today) {
			delete(p.announced, key)
		}
	}
	p.mu.Unlock()
	for _, entry := range p.entries(b.Config.NetworkName) {
		if entry.Birthday != now.Format("01-02") {
			continue
		}
		key := strings.ToLower(b.Config.NetworkName + "\x00" + entry.Nick + "\x00" + today)
		p.mu.Lock()
		if _, already := p.announced[key]; already {
			p.mu.Unlock()
			continue
		}
		p.announced[key] = struct{}{}
		channels := make([]string, 0, len(p.channels))
		for channel := range p.channels {
			channels = append(channels, channel)
		}
		p.mu.Unlock()
		for _, channel := range channels {
			b.Send(channel, fmt.Sprintf("🎂 Happy birthday, %s!", cleanExternalText(entry.Nick)))
		}
	}
}
