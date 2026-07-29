package plugins

import (
	"bufio"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

var defaultQuotes = []string{
	"Keep it simple, keep it moving.",
	"A little progress is still progress.",
	"The best tool is the one that gets the job done.",
	"There is no such thing as too much coffee. Probably.",
	"Ship small, learn quickly.",
	"The channel is calm. Suspiciously calm.",
}

type Banter struct {
	probability float64
	quotes      []string
}

func (p *Banter) Name() string       { return "banter" }
func (p *Banter) Commands() []string { return nil }
func (p *Banter) Help() string {
	return "occasionally responds when directly addressed or mentioned"
}
func (p *Banter) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.probability = c.Float("probability", 0.10)
	if p.probability < 0 {
		p.probability = 0
	}
	if p.probability > 1 {
		p.probability = 1
	}
	p.quotes = append([]string(nil), defaultQuotes...)
	if path := c.String("quotes_file", ""); path != "" {
		p.quotes = append(p.quotes, readQuotes(path)...)
	}
	if _, err := os.Stat("quotes"); err == nil {
		p.quotes = append(p.quotes, readQuotesDir("quotes")...)
	}
	if dir := c.String("fortune_dir", ""); dir != "" {
		p.quotes = append(p.quotes, readFortuneDir(dir)...)
	}
	return nil
}
func (p *Banter) Handle(b *bot.Bot, m bot.Message) bool {
	if m.Nick == "" || len(p.quotes) == 0 {
		return false
	}
	if _, _, isCommand := bot.IsCommand(m, b.Config.CommandPrefix); isCommand {
		return false
	}
	addressed := !m.IsChannel
	if m.IsChannel {
		addressed = strings.Contains(strings.ToLower(m.Text), strings.ToLower(b.Config.Identity.Nick))
	}
	if !addressed || rand.Float64() >= p.probability {
		return false
	}
	for _, part := range splitIRCText(p.quotes[rand.Intn(len(p.quotes))], 350) {
		b.Send(m.ReplyTarget(), part)
	}
	return true
}

// IRC lines are limited to 512 bytes including protocol overhead. Keep quote
// chunks comfortably below that limit so long fortune entries are not cut off
// by the server or relay. Multiple chunks are queued in order.
func splitIRCText(text string, maxBytes int) []string {
	if maxBytes < 1 {
		return nil
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var chunks []string
	current := ""
	flush := func() {
		if current != "" {
			chunks = append(chunks, current)
			current = ""
		}
	}
	for _, word := range words {
		if len([]byte(word)) > maxBytes {
			flush()
			var piece []rune
			for _, r := range word {
				candidate := string(append(piece, r))
				if len([]byte(candidate)) > maxBytes {
					chunks = append(chunks, string(piece))
					piece = piece[:0]
				}
				piece = append(piece, r)
			}
			if len(piece) > 0 {
				current = string(piece)
			}
			continue
		}
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if len([]byte(candidate)) > maxBytes {
			flush()
		}
		if current == "" {
			current = word
		} else {
			current += " " + word
		}
	}
	flush()
	return chunks
}

func readQuotes(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var quotes []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if quote := strings.TrimSpace(scanner.Text()); quote != "" {
			quotes = append(quotes, quote)
		}
	}
	return quotes
}

func readQuotesDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(names)

	var quotes []string
	for _, name := range names {
		quotes = append(quotes, readQuotes(name)...)
	}
	return quotes
}

func readFortuneDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".dat") || strings.HasSuffix(name, ".u8") {
			continue
		}
		names = append(names, filepath.Join(dir, name))
	}
	sort.Strings(names)

	var quotes []string
	for _, name := range names {
		quotes = append(quotes, readFortuneFile(name)...)
	}
	return quotes
}

func readFortuneFile(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var (
		quotes []string
		lines  []string
	)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "%" {
			if quote := strings.TrimSpace(strings.Join(lines, "\n")); quote != "" {
				quotes = append(quotes, quote)
			}
			lines = lines[:0]
			continue
		}
		lines = append(lines, line)
	}
	if quote := strings.TrimSpace(strings.Join(lines, "\n")); quote != "" {
		quotes = append(quotes, quote)
	}
	return quotes
}
