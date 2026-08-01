package plugins

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const linuxKernelBannerURL = "https://www.kernel.org/finger_banner"

// Linux provides a compact read-only lookup of current Linux kernel release
// lines. It mirrors CloudBot's kernel command without exposing arbitrary URL
// fetching or allowing an unbounded response into IRC.
type Linux struct{ cfg bot.PluginConfig }

func (p *Linux) Name() string       { return "linux" }
func (p *Linux) Commands() []string { return []string{"linux", "kernel"} }
func (p *Linux) Help() string {
	return "!linux or !kernel — show current Linux kernel versions from kernel.org"
}
func (p *Linux) Init(c bot.PluginConfig, _ *storage.DB) error { p.cfg = c; return nil }

func (p *Linux) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "linux" && cmd != "kernel") {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), linuxTimeout(p.cfg))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, linuxKernelBannerURL, nil)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Linux kernel information is temporarily unavailable"))
		return true
	}
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; Linux kernel lookup)")
	res, err := apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Linux kernel information is temporarily unavailable"))
		return true
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Linux kernel information could not be read"))
		return true
	}
	result := formatKernelBanner(string(body))
	if result == "" {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Linux kernel information could not be parsed"))
		return true
	}
	maxLength := p.cfg.Int("max_length", 260)
	if maxLength < 100 || maxLength > 400 {
		maxLength = 260
	}
	b.Send(m.ReplyTarget(), ircColor(ircCyan, truncateRunes(result, maxLength)))
	return true
}

func linuxTimeout(c bot.PluginConfig) time.Duration {
	seconds := c.Int("timeout_seconds", 8)
	if seconds < 1 || seconds > 30 {
		seconds = 8
	}
	return time.Duration(seconds) * time.Second
}

func formatKernelBanner(contents string) string {
	lines := strings.Split(contents, "\n")
	versions := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(cleanExternalText(raw))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		const prefix = "the latest "
		if strings.HasPrefix(lower, prefix) {
			if index := strings.Index(line, ":"); index >= 0 && index+1 < len(line) {
				kind := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[len(prefix):index]), "version of the Linux kernel is"))
				value := strings.TrimSpace(line[index+1:])
				if kind != "" && value != "" {
					versions = append(versions, kind+" "+value)
					continue
				}
			}
		}
		versions = append(versions, line)
	}
	if len(versions) == 0 {
		return ""
	}
	return fmt.Sprintf("Linux kernel versions: %s", strings.Join(versions, ", "))
}
