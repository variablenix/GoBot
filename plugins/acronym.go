package plugins

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/agnivade/levenshtein"
	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Acronym struct {
	entries   map[string]acronymEntry
	meanings  map[string][]acronymEntry
	maxLength int
}

type acronymEntry struct {
	Name      string
	Expansion string
	Context   string
}

func (p *Acronym) Name() string       { return "acronym" }
func (p *Acronym) Commands() []string { return []string{"acronym", "acro"} }
func (p *Acronym) Help() string {
	return "!acronym ACRONYM [context] — expand a local ACRONYM|expansion[|context] entry; misspellings get one suggestion"
}

func (p *Acronym) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", 320)
	if p.maxLength < 80 || p.maxLength > 500 {
		p.maxLength = 320
	}
	p.entries = make(map[string]acronymEntry)
	p.meanings = make(map[string][]acronymEntry)
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
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 {
			continue
		}
		name := cleanExternalText(strings.TrimSpace(parts[0]))
		expansion := cleanExternalText(strings.TrimSpace(parts[1]))
		context := ""
		if len(parts) == 3 {
			context = cleanExternalText(strings.TrimSpace(parts[2]))
		}
		if !validAcronym(name) || expansion == "" {
			continue
		}
		if context != "" && !validAcronymContext(context) {
			continue
		}
		key := strings.ToLower(name)
		entry := acronymEntry{Name: name, Expansion: expansion, Context: context}
		p.meanings[key] = append(p.meanings[key], entry)
		if _, exists := p.entries[key]; !exists {
			p.entries[key] = entry
		}
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
	raw := strings.TrimSpace(arg)
	fields := strings.Fields(raw)
	key := ""
	context := ""
	if raw != "" {
		key = strings.ToLower(raw)
		if _, exists := p.entries[key]; !exists && len(fields) > 0 {
			key = ""
			for split := len(fields) - 1; split >= 1; split-- {
				candidate := strings.ToLower(strings.Join(fields[:split], " "))
				if _, exists := p.entries[candidate]; exists {
					key = candidate
					context = strings.ToLower(strings.Join(fields[split:], " "))
					break
				}
			}
			if key == "" {
				key = strings.ToLower(fields[0])
				context = strings.ToLower(strings.Join(fields[1:], " "))
			}
		}
	}
	entry, exists := p.entries[key]
	if key == "" {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !acronym ACRONYM [context]"))
		return true
	}
	if !exists {
		if suggestion, ok := p.fuzzySuggestion(key); ok {
			message := fmt.Sprintf("%s no exact match for %s; did you mean %s?", ircColor(ircYellow, "[acronym]"), cleanExternalText(fields[0]), ircColor(ircCyan, suggestion.Name))
			b.Send(m.ReplyTarget(), truncateIRCMessage(message, p.maxLength))
			return true
		}
		if len(p.entries) == 0 {
			b.Send(m.ReplyTarget(), ircColor(ircYellow, "the acronym catalog is unavailable"))
		} else {
			b.Send(m.ReplyTarget(), ircColor(ircYellow, "no matching acronym found; try !acronym ACRONYM [context]"))
		}
		return true
	}
	if context != "" {
		entry, exists = p.contextEntry(key, context)
		if !exists {
			b.Send(m.ReplyTarget(), ircColor(ircYellow, "no matching acronym context; omit the context for the common meaning"))
			return true
		}
	}
	message := formatAcronymEntry(entry)
	b.Send(m.ReplyTarget(), truncateIRCMessage(message, p.maxLength))
	return true
}

func formatAcronymEntry(entry acronymEntry) string {
	if entry.Context != "" {
		return fmt.Sprintf("%s [%s] — %s", ircColor(ircCyan, entry.Name), cleanExternalText(entry.Context), entry.Expansion)
	}
	return fmt.Sprintf("%s — %s", ircColor(ircCyan, entry.Name), entry.Expansion)
}

func (p *Acronym) contextEntry(key, requested string) (acronymEntry, bool) {
	for _, entry := range p.meanings[key] {
		if strings.Contains(strings.ToLower(entry.Context), requested) {
			return entry, true
		}
	}
	return acronymEntry{}, false
}

func (p *Acronym) fuzzySuggestion(query string) (acronymEntry, bool) {
	query = normalizeAcronymKey(query)
	if query == "" {
		return acronymEntry{}, false
	}
	keys := make([]string, 0, len(p.entries))
	for key := range p.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	bestDistance := -1
	bestKey := ""
	for _, key := range keys {
		distance := levenshtein.ComputeDistance(query, normalizeAcronymKey(key))
		if distance > acronymFuzzyDistance(query, key) {
			continue
		}
		if bestDistance == -1 || distance < bestDistance {
			bestDistance = distance
			bestKey = key
		}
	}
	if bestKey == "" {
		return acronymEntry{}, false
	}
	return p.entries[bestKey], true
}

func acronymFuzzyDistance(query, candidate string) int {
	length := len([]rune(query))
	if candidateLength := len([]rune(candidate)); candidateLength > length {
		length = candidateLength
	}
	switch {
	case length <= 3:
		return 1
	case length <= 8:
		return 2
	default:
		return 3
	}
}

func normalizeAcronymKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
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

func validAcronymContext(value string) bool {
	return value != "" && len([]rune(value)) <= 48 && !strings.ContainsAny(value, "|\r\n") && !strings.ContainsFunc(value, unicode.IsControl)
}
