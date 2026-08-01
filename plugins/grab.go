package plugins

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const grabBucket = "grabs"

type grabbedLine struct {
	Nick string    `json:"nick"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Grab remembers the latest channel message from each nickname in memory and
// persists only explicitly saved grabs. This keeps normal chat lightweight
// while allowing saved quotes to survive restarts.
type Grab struct {
	db               *storage.DB
	mu               sync.RWMutex
	latest           map[string]map[string]grabbedLine
	maxLength        int
	maxQuotesPerUser int
}

func (p *Grab) Name() string { return "grab" }

func (p *Grab) Commands() []string {
	return []string{"grab", "lastgrab", "lgrab", "grabrandom", "grabr", "grabsearch", "grabs"}
}

func (p *Grab) Help() string {
	return "!grab <nick> — save their latest message; !lgrab <nick> — repeat the last saved line; !grabr [nick] — show a random saved line; !grabs <text> — search saved lines"
}

func (p *Grab) Init(c bot.PluginConfig, db *storage.DB) error {
	p.db = db
	p.maxLength = c.Int("max_length", 320)
	if p.maxLength < 120 || p.maxLength > 500 {
		p.maxLength = 320
	}
	p.maxQuotesPerUser = c.Int("max_quotes_per_user", 20)
	if p.maxQuotesPerUser < 1 || p.maxQuotesPerUser > 100 {
		p.maxQuotesPerUser = 20
	}
	p.latest = make(map[string]map[string]grabbedLine)
	return nil
}

func (p *Grab) Handle(b *bot.Bot, m bot.Message) bool {
	if m.Command == "PRIVMSG" && m.IsChannel && strings.TrimSpace(m.Nick) != "" {
		p.remember(m)
	}

	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !m.IsChannel {
		return false
	}

	switch strings.ToLower(cmd) {
	case "grab":
		p.handleGrab(b, m, arg)
	case "lastgrab", "lgrab":
		p.handleLastGrab(b, m, arg)
	case "grabrandom", "grabr":
		p.handleRandomGrab(b, m, arg)
	case "grabsearch", "grabs":
		p.handleSearch(b, m, arg)
	default:
		return false
	}
	return true
}

func (p *Grab) remember(m bot.Message) {
	text := normalizeGrabText(m.Text)
	if text == "" {
		return
	}
	channel := strings.ToLower(strings.TrimSpace(m.Target))
	nick := strings.ToLower(strings.TrimSpace(m.Nick))
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.latest[channel] == nil {
		p.latest[channel] = make(map[string]grabbedLine)
	}
	p.latest[channel][nick] = grabbedLine{Nick: m.Nick, Text: text, At: m.Timestamp}
}

func (p *Grab) handleGrab(b *bot.Bot, m bot.Message, arg string) {
	target := strings.TrimSpace(arg)
	if len(strings.Fields(target)) != 1 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !grab <nick>"))
		return
	}
	if strings.EqualFold(target, m.Nick) {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "please grab someone else; I already heard that one"))
		return
	}

	channel := strings.ToLower(strings.TrimSpace(m.Target))
	nick := strings.ToLower(target)
	p.mu.RLock()
	line, found := p.latest[channel][nick]
	p.mu.RUnlock()
	if !found || line.Text == "" {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("I couldn't find a recent message from %s", target)))
		return
	}

	if p.db == nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "saved quotes are temporarily unavailable"))
		return
	}
	key := grabKey(b.Config.NetworkName, m.Target, nick)
	p.mu.Lock()
	quotes := p.load(key)
	for _, quote := range quotes {
		if quote.Text == line.Text {
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("I already saved that line from %s", line.Nick)))
			return
		}
	}
	quotes = append(quotes, line)
	if len(quotes) > p.maxQuotesPerUser {
		quotes = quotes[len(quotes)-p.maxQuotesPerUser:]
	}
	err := p.db.Set(grabBucket, key, quotes)
	p.mu.Unlock()
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "I couldn't save that line"))
		return
	}
	b.Send(m.ReplyTarget(), ircColor(ircGreen, fmt.Sprintf("saved %s's latest line", line.Nick)))
}

func (p *Grab) handleLastGrab(b *bot.Bot, m bot.Message, arg string) {
	target := strings.TrimSpace(arg)
	if len(strings.Fields(target)) != 1 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !lgrab <nick>"))
		return
	}
	quotes := p.quotesFor(b.Config.NetworkName, m.Target, target)
	if len(quotes) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s has never been grabbed here", target)))
		return
	}
	line := quotes[len(quotes)-1]
	b.Send(m.ReplyTarget(), ircColor(ircCyan, formatGrab(line.Nick, line.Text)))
}

func (p *Grab) handleRandomGrab(b *bot.Bot, m bot.Message, arg string) {
	arg = strings.TrimSpace(arg)
	if len(strings.Fields(arg)) > 1 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !grabr [nick]"))
		return
	}
	all := p.channelQuotes(b.Config.NetworkName, m.Target)
	if arg != "" {
		wanted := strings.ToLower(arg)
		filtered := all[:0]
		for _, line := range all {
			if strings.EqualFold(line.Nick, wanted) {
				filtered = append(filtered, line)
			}
		}
		all = filtered
	}
	if len(all) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "there are no saved grabs here"))
		return
	}
	line := all[rand.Intn(len(all))]
	b.Send(m.ReplyTarget(), ircColor(ircCyan, formatGrab(line.Nick, line.Text)))
}

func (p *Grab) handleSearch(b *bot.Bot, m bot.Message, arg string) {
	term := strings.TrimSpace(arg)
	if term == "" || len([]rune(term)) > 80 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !grabs <text>"))
		return
	}
	term = strings.ToLower(term)
	matches := make([]grabbedLine, 0, 3)
	for _, line := range p.channelQuotes(b.Config.NetworkName, m.Target) {
		if strings.Contains(strings.ToLower(line.Nick), term) || strings.Contains(strings.ToLower(line.Text), term) {
			matches = append(matches, line)
			if len(matches) == 3 {
				break
			}
		}
	}
	if len(matches) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("no saved grabs matched %q", strings.TrimSpace(arg))))
		return
	}
	parts := make([]string, len(matches))
	for i, line := range matches {
		parts[i] = formatGrab(line.Nick, line.Text)
	}
	result := strings.Join(parts, " | ")
	if len(matches) == 3 {
		result += " | showing up to 3"
	}
	b.Send(m.ReplyTarget(), ircColor(ircCyan, truncateRunes(result, p.maxLength)))
}

func (p *Grab) quotesFor(network, channel, nick string) []grabbedLine {
	if p.db == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.load(grabKey(network, channel, strings.ToLower(strings.TrimSpace(nick))))
}

func (p *Grab) channelQuotes(network, channel string) []grabbedLine {
	if p.db == nil {
		return nil
	}
	keys, err := p.db.List(grabBucket)
	if err != nil {
		return nil
	}
	prefix := grabChannelPrefix(network, channel)
	result := make([]grabbedLine, 0)
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		result = append(result, p.load(key)...)
	}
	return result
}

func (p *Grab) load(key string) []grabbedLine {
	if p.db == nil {
		return nil
	}
	raw, err := p.db.Get(grabBucket, key)
	if err != nil {
		return nil
	}
	var quotes []grabbedLine
	if storage.Decode(raw, &quotes) != nil {
		return nil
	}
	return quotes
}

func grabChannelPrefix(network, channel string) string {
	return strings.ToLower(strings.TrimSpace(network)) + "\x00" + strings.ToLower(strings.TrimSpace(channel)) + "\x00"
}

func grabKey(network, channel, nick string) string {
	return grabChannelPrefix(network, channel) + strings.ToLower(strings.TrimSpace(nick))
}

func normalizeGrabText(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "\x01ACTION ") && strings.HasSuffix(text, "\x01") {
		text = "* " + strings.TrimSuffix(strings.TrimPrefix(text, "\x01ACTION "), "\x01")
	}
	return truncateRunes(cleanExternalText(text), 360)
}

func formatGrab(nick, text string) string {
	nick = strings.TrimSpace(nick)
	if nick == "" {
		nick = "someone"
	}
	runes := []rune(nick)
	display := nick
	if len(runes) > 1 {
		display = string(runes[0]) + "\u200b" + string(runes[1:])
	}
	if strings.HasPrefix(text, "* ") {
		return "* " + display + " " + strings.TrimSpace(strings.TrimPrefix(text, "* "))
	}
	return fmt.Sprintf("<%s> %s", display, text)
}
