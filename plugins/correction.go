package plugins

import (
	"regexp"
	"strings"
	"sync"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// substitutionRX intentionally supports the common IRC correction form:
// s/old/new and s/old/new/g. Slashes inside either side may be escaped as \/.
var substitutionRX = regexp.MustCompile(`^s/((?:\\/|[^/])*)/((?:\\/|[^/])*)(?:/(g))?$`)

type correctionMessage struct {
	nick string
	text string
}

// ParseSubstitution parses an IRC correction and returns the old text, new
// text, and whether the replacement should be global.
func ParseSubstitution(text string) (old, replacement string, global bool, ok bool) {
	m := substitutionRX.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil || m[1] == "" {
		return "", "", false, false
	}
	return strings.ReplaceAll(m[1], `\/`, `/`), strings.ReplaceAll(m[2], `\/`, `/`), m[3] == "g", true
}

type Correction struct {
	mu      sync.Mutex
	byChan  map[string][]correctionMessage
	maxKeep int
}

func (p *Correction) Name() string       { return "correction" }
func (p *Correction) Commands() []string { return nil }
func (p *Correction) Help() string       { return "s/old/new — correct the most recent channel message" }
func (p *Correction) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.byChan = make(map[string][]correctionMessage)
	p.maxKeep = c.Int("history_size", 100)
	if p.maxKeep < 1 {
		p.maxKeep = 100
	}
	return nil
}

func (p *Correction) Handle(b *bot.Bot, m bot.Message) bool {
	if !m.IsChannel || m.Nick == "" {
		return false
	}
	old, replacement, global, ok := ParseSubstitution(m.Text)
	p.mu.Lock()
	history := p.byChan[m.Target]
	if ok {
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].nick != m.Nick || !strings.Contains(history[i].text, old) {
				continue
			}
			corrected := history[i].text
			if global {
				corrected = strings.ReplaceAll(corrected, old, replacement)
			} else {
				corrected = strings.Replace(corrected, old, replacement, 1)
			}
			p.mu.Unlock()
			b.Send(m.Target, corrected)
			return true
		}
	}
	history = append(history, correctionMessage{nick: m.Nick, text: m.Text})
	if len(history) > p.maxKeep {
		history = history[len(history)-p.maxKeep:]
	}
	p.byChan[m.Target] = history
	p.mu.Unlock()
	return false
}
