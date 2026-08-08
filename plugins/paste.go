package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const pasteDefaultMaxInput = 4096

type Paste struct {
	baseURL        string
	token          string
	provider       string
	visibility     string
	maxInputLength int
}

func (p *Paste) Name() string       { return "paste" }
func (p *Paste) Commands() []string { return []string{"paste"} }
func (p *Paste) Help() string {
	return "!paste <text|url> — create an Opengist paste; URL content is fetched with a bounded timeout"
}

func (p *Paste) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.provider = strings.ToLower(strings.TrimSpace(c.String("provider", "opengist")))
	p.baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("BOT_PASTE_BASE_URL")), "/")
	if p.baseURL == "" {
		p.baseURL = strings.TrimRight(strings.TrimSpace(c.String("base_url", "")), "/")
	}
	p.token = strings.TrimSpace(os.Getenv("BOT_PASTE_TOKEN"))
	p.visibility = strings.ToLower(strings.TrimSpace(c.String("default_visibility", "unlisted")))
	if p.visibility != "public" && p.visibility != "unlisted" && p.visibility != "private" {
		p.visibility = "unlisted"
	}
	p.maxInputLength = c.Int("max_input_length", pasteDefaultMaxInput)
	if p.maxInputLength < 1 || p.maxInputLength > 64*1024 {
		p.maxInputLength = pasteDefaultMaxInput
	}
	return nil
}

func (p *Paste) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "paste" {
		return false
	}
	arg = strings.TrimSpace(arg)
	if arg == "" {
		b.Send(m.ReplyTarget(), "usage: !paste <text|url>")
		return true
	}
	if p.provider != "opengist" {
		b.Send(m.ReplyTarget(), "paste provider is not supported")
		return true
	}
	if p.baseURL == "" || p.token == "" {
		b.Send(m.ReplyTarget(), "paste is not configured: set BOT_PASTE_BASE_URL and BOT_PASTE_TOKEN")
		return true
	}

	input := arg
	truncated := false
	if parsed, err := url.ParseRequestURI(arg); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
		fetched, wasTruncated, err := fetchPasteURL(context.Background(), arg, p.maxInputLength)
		if err != nil {
			b.Send(m.ReplyTarget(), "could not fetch URL content for paste")
			return true
		}
		input, truncated = fetched, wasTruncated
	}
	if len([]rune(input)) > p.maxInputLength {
		input = truncateRunes(input, p.maxInputLength)
		truncated = true
	}
	result, err := p.createPaste(context.Background(), input)
	if err != nil {
		var providerErr pasteHTTPError
		if errors.As(err, &providerErr) {
			b.Send(m.ReplyTarget(), fmt.Sprintf("paste creation failed: Opengist HTTP %d", providerErr.status))
		} else {
			b.Send(m.ReplyTarget(), "paste creation failed")
		}
		return true
	}
	notice := ""
	if truncated {
		notice = " (input truncated)"
	}
	b.Send(m.ReplyTarget(), truncateRunes(cleanExternalText("[paste] "+result+notice), 400))
	return true
}

type pasteHTTPError struct{ status int }

func (e pasteHTTPError) Error() string { return fmt.Sprintf("Opengist returned HTTP %d", e.status) }

func fetchPasteURL(parent context.Context, rawURL string, maxLength int) (string, bool, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; paste plugin)")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", false, fmt.Errorf("URL returned HTTP %d", res.StatusCode)
	}
	limit := int64(maxLength*4 + 1)
	body, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return "", false, err
	}
	text := string(body)
	wasTruncated := len([]rune(text)) > maxLength
	if wasTruncated {
		text = truncateRunes(text, maxLength)
	}
	return text, wasTruncated, nil
}

func (p *Paste) createPaste(parent context.Context, content string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	body, err := json.Marshal(map[string]interface{}{
		"visibility": p.visibility,
		"files":      map[string]map[string]string{"paste.txt": {"content": content}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/gists", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", pasteHTTPError{status: res.StatusCode}
	}
	var response struct {
		URL     string `json:"url"`
		HTMLURL string `json:"html_url"`
		ID      string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 128*1024)).Decode(&response); err != nil {
		return "", err
	}
	result := strings.TrimSpace(response.URL)
	if result == "" {
		result = strings.TrimSpace(response.HTMLURL)
	}
	if result == "" && strings.TrimSpace(response.ID) != "" {
		result = p.baseURL + "/" + strings.TrimSpace(response.ID)
	}
	if result == "" {
		return "", fmt.Errorf("Opengist response did not include a URL")
	}
	return result, nil
}
