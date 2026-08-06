package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const ipAPIURL = "http://ip-api.com/json/"

var (
	ipHTTPClient   = &http.Client{Timeout: 8 * time.Second}
	ipQueryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:%_-]{0,100}$`)
	ipRequestMu    sync.Mutex
	ipLastRequest  time.Time
)

type IPInfo struct {
	maxLength int
	timeout   time.Duration
}

type ipLookup struct {
	Status      string `json:"status"`
	Message     string `json:"message"`
	Query       string `json:"query"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	City        string `json:"city"`
	ISP         string `json:"isp"`
	Org         string `json:"org"`
	AS          string `json:"as"`
	ASName      string `json:"asname"`
	Reverse     string `json:"reverse"`
	Proxy       bool   `json:"proxy"`
	Hosting     bool   `json:"hosting"`
	Mobile      bool   `json:"mobile"`
}

func (p *IPInfo) Name() string       { return "ipinfo" }
func (p *IPInfo) Commands() []string { return []string{"ip", "asn"} }
func (p *IPInfo) Help() string {
	return "!ip <address|hostname> or !asn <address|hostname> — show ASN, organization, country, proxy/hosting flags, and rDNS (ip-api; no key required)"
}

func (p *IPInfo) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", 320)
	if p.maxLength < 160 || p.maxLength > 500 {
		p.maxLength = 320
	}
	timeoutSeconds := c.Int("timeout_seconds", 8)
	if timeoutSeconds < 3 || timeoutSeconds > 20 {
		timeoutSeconds = 8
	}
	p.timeout = time.Duration(timeoutSeconds) * time.Second
	return nil
}

func (p *IPInfo) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (strings.ToLower(cmd) != "ip" && strings.ToLower(cmd) != "asn") {
		return false
	}
	query := strings.TrimSpace(arg)
	if !validIPQuery(query) {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !ip <address|hostname> or !asn <address|hostname>"))
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	result, err := lookupIP(ctx, query)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "IP lookup is temporarily unavailable"))
		return true
	}
	b.Send(m.ReplyTarget(), truncateIRCMessage(formatIPResult(strings.ToLower(cmd), result), p.maxLength))
	return true
}

func validIPQuery(query string) bool {
	return query != "" && ipQueryPattern.MatchString(query) && !strings.ContainsAny(query, "\r\n/?#\\")
}

func lookupIP(ctx context.Context, query string) (ipLookup, error) {
	// The free ip-api endpoint allows 45 requests/minute. A small process-wide
	// spacing guard protects the service even when several users request data.
	ipRequestMu.Lock()
	if wait := time.Until(ipLastRequest.Add(1400 * time.Millisecond)); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ipLookup{}, ctx.Err()
		case <-timer.C:
		}
	}
	ipLastRequest = time.Now()
	ipRequestMu.Unlock()

	endpoint := ipAPIURL + url.PathEscape(query)
	params := url.Values{}
	params.Set("fields", "status,message,query,country,countryCode,city,isp,org,as,asname,reverse,proxy,hosting,mobile")
	endpoint += "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ipLookup{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; IP lookup)")
	res, err := ipHTTPClient.Do(req)
	if err != nil {
		return ipLookup{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ipLookup{}, fmt.Errorf("ip-api returned HTTP %d", res.StatusCode)
	}
	var result ipLookup
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result); err != nil {
		return ipLookup{}, err
	}
	if !strings.EqualFold(result.Status, "success") {
		return ipLookup{}, fmt.Errorf("ip-api lookup failed: %s", result.Message)
	}
	return result, nil
}

func formatIPResult(command string, result ipLookup) string {
	label := "[IP]"
	if command == "asn" {
		label = "[ASN]"
	}
	parts := []string{ircColor(ircGreen, label), ircColor(ircCyan, cleanExternalText(result.Query))}
	if result.AS != "" {
		parts = append(parts, cleanExternalText(result.AS))
	}
	org := firstNonEmpty(cleanExternalText(result.Org), cleanExternalText(result.ISP))
	if org != "" {
		parts = append(parts, "org: "+org)
	}
	country := firstNonEmpty(cleanExternalText(result.Country), cleanExternalText(result.CountryCode))
	if country != "" {
		parts = append(parts, "country: "+country)
	}
	flags := make([]string, 0, 3)
	if result.Hosting {
		flags = append(flags, "datacenter/hosting")
	}
	if result.Proxy {
		flags = append(flags, "proxy/VPN/Tor")
	}
	if result.Mobile {
		flags = append(flags, "mobile")
	}
	if len(flags) == 0 {
		flags = append(flags, "no hosting/proxy flag")
	}
	parts = append(parts, strings.Join(flags, ", "))
	if reverse := cleanExternalText(result.Reverse); reverse != "" {
		parts = append(parts, "rDNS: "+reverse)
	}
	return strings.Join(parts, " | ")
}
