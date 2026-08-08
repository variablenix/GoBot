package plugins

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type portEntry struct {
	Port        int
	Name        string
	Description string
}

type Port struct {
	byPort map[int]portEntry
	byName map[string]portEntry
}

func (p *Port) Name() string       { return "port" }
func (p *Port) Commands() []string { return []string{"port", "ports"} }
func (p *Port) Help() string {
	return "!port <number|service> — look up a well-known TCP/UDP service (alias: !ports)"
}

func (p *Port) Init(c bot.PluginConfig, _ *storage.DB) error {
	path := c.String("data_file", "data/ports.txt")
	file, err := os.Open(path)
	if err != nil && !filepath.IsAbs(path) {
		file, err = os.Open(filepath.Join("..", path))
	}
	if err != nil {
		return fmt.Errorf("open ports data: %w", err)
	}
	defer file.Close()
	p.byPort = make(map[int]portEntry)
	p.byName = make(map[string]portEntry)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		port, err := strconv.Atoi(fields[0])
		if err != nil || port < 0 || port > 65535 {
			continue
		}
		entry := portEntry{Port: port, Name: fields[1], Description: strings.Join(fields[2:], " ")}
		if _, exists := p.byPort[port]; !exists {
			p.byPort[port] = entry
		}
		p.byName[strings.ToLower(entry.Name)] = entry
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read ports data: %w", err)
	}
	return nil
}

func (p *Port) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "port" && cmd != "ports") {
		return false
	}
	query := strings.TrimSpace(arg)
	if len(strings.Fields(query)) != 1 {
		b.Send(m.ReplyTarget(), "usage: !port <number|service>")
		return true
	}
	var entry portEntry
	var found bool
	if port, err := strconv.Atoi(query); err == nil {
		entry, found = p.byPort[port]
	} else {
		entry, found = p.byName[strings.ToLower(query)]
	}
	if !found {
		b.Send(m.ReplyTarget(), fmt.Sprintf("[port] %s not found", cleanExternalText(query)))
		return true
	}
	result := fmt.Sprintf("[port] %d — %s", entry.Port, cleanExternalText(entry.Name))
	if entry.Description != "" {
		result += " — " + cleanExternalText(entry.Description)
	}
	b.Send(m.ReplyTarget(), truncateRunes(result, 350))
	return true
}
