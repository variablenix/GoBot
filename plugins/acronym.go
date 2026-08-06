package plugins

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Acronym struct {
	entries   map[string]acronymEntry
	maxLength int
}

type acronymEntry struct {
	Name      string
	Expansion string
}

func (p *Acronym) Name() string       { return "acronym" }
func (p *Acronym) Commands() []string { return []string{"acronym", "acro"} }
func (p *Acronym) Help() string {
	return "!acronym ACRONYM — expand an operator-maintained ACRONYM|expansion entry"
}

func (p *Acronym) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", 320)
	if p.maxLength < 80 || p.maxLength > 500 {
		p.maxLength = 320
	}
	p.entries = make(map[string]acronymEntry)
	path := c.String("data_file", filepath.Join("data", "acronyms.txt"))
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open acronym file: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		name := cleanExternalText(strings.TrimSpace(parts[0]))
		expansion := cleanExternalText(strings.TrimSpace(parts[1]))
		if !validAcronym(name) || expansion == "" {
			continue
		}
		p.entries[strings.ToLower(name)] = acronymEntry{Name: name, Expansion: expansion}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read acronym file: %w", err)
	}
	return nil
}

func (p *Acronym) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (strings.ToLower(cmd) != "acronym" && strings.ToLower(cmd) != "acro") {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(arg))
	entry, exists := p.entries[key]
	if key == "" || !exists {
		if len(p.entries) == 0 {
			b.Send(m.ReplyTarget(), ircColor(ircYellow, "the acronym catalog is unavailable"))
		} else {
			b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !acronym ACRONYM (no matching entry found)"))
		}
		return true
	}
	message := formatAcronymEntry(entry)
	b.Send(m.ReplyTarget(), truncateIRCMessage(message, p.maxLength))
	return true
}

func formatAcronymEntry(entry acronymEntry) string {
	return fmt.Sprintf("%s — %s", ircColor(ircCyan, entry.Name), entry.Expansion)
}

func validAcronym(value string) bool {
	if value == "" || len([]rune(value)) > 32 || strings.ContainsAny(value, "|\r\n") {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == ' ' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
