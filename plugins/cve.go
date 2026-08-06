package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const (
	nvdCVEURL           = "https://services.nvd.nist.gov/rest/json/cves/2.0"
	nvdVulnerabilityURL = "https://nvd.nist.gov/vuln/detail/"
	cveDefaultLimit     = 360
)

var (
	cveIDPattern   = regexp.MustCompile(`(?i)^CVE-[0-9]{4}-[0-9]{4,}$`)
	cveHTTPClient  = &http.Client{Timeout: 8 * time.Second}
	errCVENotFound = errors.New("CVE not found")
)

type CVE struct {
	maxLength int
	timeout   time.Duration
}

type cveResult struct {
	ID          string
	Score       float64
	Severity    string
	HasScore    bool
	Affected    []string
	Description string
}

func (p *CVE) Name() string       { return "cve" }
func (p *CVE) Commands() []string { return []string{"cve", "vuln", "vulnerability"} }
func (p *CVE) Help() string {
	return "!cve CVE-YYYY-NNNN — look up CVSS severity, affected software, and the NVD link (no API key required)"
}

func (p *CVE) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", cveDefaultLimit)
	if p.maxLength < 160 || p.maxLength > 500 {
		p.maxLength = cveDefaultLimit
	}
	timeoutSeconds := c.Int("timeout_seconds", 8)
	if timeoutSeconds < 3 || timeoutSeconds > 20 {
		timeoutSeconds = 8
	}
	p.timeout = time.Duration(timeoutSeconds) * time.Second
	return nil
}

func (p *CVE) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isCVECommand(cmd) {
		return false
	}
	cveID := strings.ToUpper(strings.TrimSpace(arg))
	if !cveIDPattern.MatchString(cveID) {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !cve CVE-YYYY-NNNN"))
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	result, err := lookupCVE(ctx, cveID)
	if err != nil {
		if errors.Is(err, errCVENotFound) {
			b.Send(m.ReplyTarget(), ircColor(ircYellow, "that CVE was not found in the NVD"))
			return true
		}
		b.Send(m.ReplyTarget(), ircColor(ircRed, "CVE lookup is temporarily unavailable"))
		return true
	}
	b.Send(m.ReplyTarget(), formatCVEResult(result, p.maxLength))
	return true
}

func isCVECommand(command string) bool {
	switch strings.ToLower(command) {
	case "cve", "vuln", "vulnerability":
		return true
	default:
		return false
	}
}

func lookupCVE(ctx context.Context, cveID string) (cveResult, error) {
	endpoint, err := url.Parse(nvdCVEURL)
	if err != nil {
		return cveResult{}, err
	}
	params := endpoint.Query()
	params.Set("cveId", cveID)
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return cveResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; CVE lookup)")
	res, err := cveHTTPClient.Do(req)
	if err != nil {
		return cveResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return cveResult{}, fmt.Errorf("NVD returned HTTP %d", res.StatusCode)
	}
	var payload nvdResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&payload); err != nil {
		return cveResult{}, err
	}
	if len(payload.Vulnerabilities) == 0 {
		return cveResult{}, errCVENotFound
	}
	return parseCVEResult(payload.Vulnerabilities[0].CVE), nil
}

type nvdResponse struct {
	Vulnerabilities []struct {
		CVE nvdCVE `json:"cve"`
	} `json:"vulnerabilities"`
}

type nvdCVE struct {
	ID           string `json:"id"`
	Descriptions []struct {
		Lang  string `json:"lang"`
		Value string `json:"value"`
	} `json:"descriptions"`
	Metrics        nvdMetrics         `json:"metrics"`
	Configurations []nvdConfiguration `json:"configurations"`
}

type nvdMetrics struct {
	CVSSMetricV40 []struct {
		CVSSData struct {
			BaseScore    float64 `json:"baseScore"`
			BaseSeverity string  `json:"baseSeverity"`
		} `json:"cvssData"`
	} `json:"cvssMetricV40"`
	CVSSMetricV31 []struct {
		CVSSData struct {
			BaseScore    float64 `json:"baseScore"`
			BaseSeverity string  `json:"baseSeverity"`
		} `json:"cvssData"`
	} `json:"cvssMetricV31"`
	CVSSMetricV30 []struct {
		CVSSData struct {
			BaseScore    float64 `json:"baseScore"`
			BaseSeverity string  `json:"baseSeverity"`
		} `json:"cvssData"`
	} `json:"cvssMetricV30"`
	CVSSMetricV2 []struct {
		CVSSData struct {
			BaseScore    float64 `json:"baseScore"`
			BaseSeverity string  `json:"baseSeverity"`
		} `json:"cvssData"`
	} `json:"cvssMetricV2"`
}

type nvdConfiguration struct {
	Nodes []struct {
		CPEMatch []struct {
			Criteria string `json:"criteria"`
		} `json:"cpeMatch"`
	} `json:"nodes"`
}

func parseCVEResult(cve nvdCVE) cveResult {
	result := cveResult{ID: strings.ToUpper(strings.TrimSpace(cve.ID))}
	for _, description := range cve.Descriptions {
		if strings.EqualFold(description.Lang, "en") {
			result.Description = cleanExternalText(description.Value)
			break
		}
	}
	result.Score, result.Severity, result.HasScore = cveScore(cve.Metrics)
	result.Affected = affectedProducts(cve.Configurations)
	return result
}

func cveScore(metrics nvdMetrics) (float64, string, bool) {
	if len(metrics.CVSSMetricV40) > 0 {
		data := metrics.CVSSMetricV40[0].CVSSData
		return data.BaseScore, strings.ToUpper(cleanExternalText(data.BaseSeverity)), data.BaseScore > 0
	}
	if len(metrics.CVSSMetricV31) > 0 {
		data := metrics.CVSSMetricV31[0].CVSSData
		return data.BaseScore, strings.ToUpper(cleanExternalText(data.BaseSeverity)), data.BaseScore > 0
	}
	if len(metrics.CVSSMetricV30) > 0 {
		data := metrics.CVSSMetricV30[0].CVSSData
		return data.BaseScore, strings.ToUpper(cleanExternalText(data.BaseSeverity)), data.BaseScore > 0
	}
	if len(metrics.CVSSMetricV2) > 0 {
		data := metrics.CVSSMetricV2[0].CVSSData
		severity := strings.ToUpper(cleanExternalText(data.BaseSeverity))
		if severity == "" {
			severity = cveV2Severity(data.BaseScore)
		}
		return data.BaseScore, severity, data.BaseScore > 0
	}
	return 0, "", false
}

func cveV2Severity(score float64) string {
	switch {
	case score >= 7:
		return "HIGH"
	case score >= 4:
		return "MEDIUM"
	case score > 0:
		return "LOW"
	default:
		return ""
	}
}

func affectedProducts(configurations []nvdConfiguration) []string {
	seen := make(map[string]struct{})
	products := make([]string, 0, 3)
	for _, configuration := range configurations {
		for _, node := range configuration.Nodes {
			for _, match := range node.CPEMatch {
				product := cpeProduct(match.Criteria)
				if product == "" {
					continue
				}
				if _, exists := seen[product]; exists {
					continue
				}
				seen[product] = struct{}{}
				products = append(products, product)
				if len(products) == 3 {
					return products
				}
			}
		}
	}
	return products
}

func cpeProduct(criteria string) string {
	parts := strings.Split(criteria, ":")
	if len(parts) < 6 || parts[0] != "cpe" || parts[1] != "2.3" {
		return ""
	}
	vendor := strings.ReplaceAll(parts[3], "_", " ")
	product := strings.ReplaceAll(parts[4], "_", " ")
	version := strings.ReplaceAll(parts[5], "_", " ")
	if vendor == "" || product == "" || vendor == "*" || product == "*" {
		return ""
	}
	label := vendor + "/" + product
	if version != "" && version != "*" && version != "-" {
		label += " " + version
	}
	return cleanExternalText(label)
}

func formatCVEResult(result cveResult, maxLength int) string {
	id := strings.ToUpper(cleanExternalText(result.ID))
	parts := []string{ircColor(ircRed, "[CVE]")}
	if cveIDPattern.MatchString(id) {
		parts = append(parts, ircColor(ircCyan, id))
	}
	if result.HasScore {
		parts = append(parts, ircColor(ircYellow, fmt.Sprintf("CVSS %.1f %s", result.Score, result.Severity)))
	}
	if len(result.Affected) > 0 {
		parts = append(parts, "affected: "+strings.Join(result.Affected, ", "))
	}
	if cveIDPattern.MatchString(id) {
		parts = append(parts, ircColor(ircCyan, nvdVulnerabilityURL+url.PathEscape(id)))
	}
	message := strings.Join(parts, " | ")
	if maxLength > 0 {
		message = truncateIRCMessage(message, maxLength)
	}
	return message
}
