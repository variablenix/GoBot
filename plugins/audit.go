package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Audit struct{ cfg bot.PluginConfig }

type osvResponse struct {
	Vulns []osvVulnerability `json:"vulns"`
}
type osvVulnerability struct {
	ID               string                     `json:"id"`
	Aliases          []string                   `json:"aliases"`
	DatabaseSpecific map[string]json.RawMessage `json:"database_specific"`
	Severity         []osvSeverity              `json:"severity"`
	Affected         []osvAffected              `json:"affected"`
}
type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}
type osvAffected struct {
	Versions []string   `json:"versions"`
	Ranges   []osvRange `json:"ranges"`
}
type osvRange struct {
	Type   string     `json:"type"`
	Events []osvEvent `json:"events"`
}
type osvEvent struct {
	Introduced   string `json:"introduced"`
	Fixed        string `json:"fixed"`
	LastAffected string `json:"last_affected"`
}
type auditVulnerability struct {
	Label string
	Sev   string
	Fixed string
}

func (p *Audit) Name() string       { return "audit" }
func (p *Audit) Commands() []string { return []string{"audit", "vuln", "osv"} }
func (p *Audit) Help() string {
	return "!audit <go|npm|pip> <package> [version] — discover known OSV vulnerabilities; failed exact names get fuzzy suggestions (aliases: !vuln, !osv)"
}
func (p *Audit) Init(c bot.PluginConfig, _ *storage.DB) error { p.cfg = c; return nil }

func (p *Audit) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "audit" && cmd != "vuln" && cmd != "osv") {
		return false
	}
	parts := strings.Fields(arg)
	if len(parts) < 2 || len(parts) > 3 {
		b.Send(m.ReplyTarget(), "usage: !audit <go|npm|pip> <package> [version]")
		return true
	}
	ecosystem := normalizePackageEcosystem(parts[0])
	if ecosystem == "" || !validPackagePart(parts[1]) || (len(parts) == 3 && !validPackagePart(parts[2])) {
		b.Send(m.ReplyTarget(), "usage: !audit <go|npm|pip> <package> [version]")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), packageTimeout(p.cfg))
	defer cancel()
	version := ""
	if len(parts) == 3 {
		version = parts[2]
	}
	response, err := queryOSV(ctx, ecosystem, parts[1], version)
	if err != nil {
		b.Send(m.ReplyTarget(), "[audit] vulnerability lookup is temporarily unavailable")
		return true
	}
	maxShown := p.cfg.Int("max_vulns_shown", 3)
	if maxShown < 1 || maxShown > 10 {
		maxShown = 3
	}
	maxLength := packageMaxLength(p.cfg)
	if configured := p.cfg.Int("max_length", 400); configured >= 100 && configured <= 600 {
		maxLength = configured
	}
	if version != "" {
		if len(response.Vulns) == 0 {
			if _, metadataErr := lookupPackageMetadata(ctx, ecosystem, parts[1], version); metadataErr == errPackageNotFound {
				b.Send(m.ReplyTarget(), truncateRunes(formatAuditSuggestions(ctx, ecosystem, parts[1]), maxLength))
				return true
			}
		}
		b.Send(m.ReplyTarget(), truncateRunes(formatAuditExact(parts[1], version, response.Vulns, maxShown), maxLength))
		return true
	}
	latest, err := lookupPackageMetadata(ctx, ecosystem, parts[1], "")
	if err != nil {
		if err == errPackageNotFound {
			b.Send(m.ReplyTarget(), truncateRunes(formatAuditSuggestions(ctx, ecosystem, parts[1]), maxLength))
			return true
		}
		b.Send(m.ReplyTarget(), "[audit] latest package version could not be determined")
		return true
	}
	if len(response.Vulns) == 0 {
		b.Send(m.ReplyTarget(), truncateRunes(fmt.Sprintf("[audit] %s %s — no known vulnerabilities", cleanExternalText(parts[1]), cleanExternalText(latest.Version)), maxLength))
		return true
	}
	affected := make([]osvVulnerability, 0, len(response.Vulns))
	for _, vulnerability := range response.Vulns {
		if osvVersionAffected(vulnerability, latest.Version) {
			affected = append(affected, vulnerability)
		}
	}
	if len(affected) == 0 {
		b.Send(m.ReplyTarget(), truncateRunes(fmt.Sprintf("[audit] %s — %d vulns across all versions, latest %s is clean", cleanExternalText(parts[1]), len(response.Vulns), cleanExternalText(latest.Version)), maxLength))
		return true
	}
	b.Send(m.ReplyTarget(), truncateRunes(formatAuditLatest(parts[1], latest.Version, affected, maxShown), maxLength))
	return true
}

func formatAuditSuggestions(ctx context.Context, ecosystem, query string) string {
	query = cleanExternalText(query)
	candidates, err := searchPackageCandidates(ctx, ecosystem, query)
	if err != nil || len(candidates) == 0 {
		return fmt.Sprintf("[audit] %s not found; use the full package/module name", query)
	}
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		name := cleanExternalText(candidate.Name)
		if candidate.Version != "" {
			name += " " + cleanExternalText(candidate.Version)
		}
		items = append(items, name)
	}
	return fmt.Sprintf("[audit] no exact match for %s; possible packages: %s", query, strings.Join(items, "; "))
}

func queryOSV(ctx context.Context, ecosystem, name, version string) (osvResponse, error) {
	request := map[string]interface{}{"package": map[string]string{"name": name, "ecosystem": ecosystem}}
	if version != "" {
		request["version"] = version
	}
	body, err := json.Marshal(request)
	if err != nil {
		return osvResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.osv.dev/v1/query", bytes.NewReader(body))
	if err != nil {
		return osvResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; vulnerability audit)")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return osvResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return osvResponse{}, fmt.Errorf("OSV returned HTTP %d", res.StatusCode)
	}
	var response osvResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 8*1024*1024)).Decode(&response); err != nil {
		return osvResponse{}, err
	}
	return response, nil
}

func formatAuditExact(name, version string, vulnerabilities []osvVulnerability, maxShown int) string {
	name, version = cleanExternalText(name), cleanExternalText(version)
	if len(vulnerabilities) == 0 {
		return fmt.Sprintf("[audit] %s %s — no known vulnerabilities", name, version)
	}
	return fmt.Sprintf("[audit] %s %s — %s", name, version, formatVulnerabilityList(vulnerabilities, maxShown, true))
}

func formatAuditLatest(name, version string, vulnerabilities []osvVulnerability, maxShown int) string {
	return fmt.Sprintf("[audit] %s — latest %s affected: %s", cleanExternalText(name), cleanExternalText(version), formatVulnerabilityList(vulnerabilities, maxShown, false))
}

func formatVulnerabilityList(vulnerabilities []osvVulnerability, maxShown int, includeCount bool) string {
	if maxShown < 1 {
		maxShown = 1
	}
	items := make([]string, 0, len(vulnerabilities))
	fixed := make([]string, 0, len(vulnerabilities))
	for _, vulnerability := range vulnerabilities {
		detail := auditVulnerability{Label: vulnerabilityLabel(vulnerability), Sev: vulnerabilitySeverity(vulnerability), Fixed: vulnerabilityFixed(vulnerability)}
		if detail.Sev == "" {
			detail.Sev = "unknown"
		}
		items = append(items, fmt.Sprintf("%s (%s)", cleanExternalText(detail.Label), cleanExternalText(detail.Sev)))
		if detail.Fixed != "" {
			fixed = append(fixed, cleanExternalText(detail.Fixed))
		}
	}
	shown := items
	more := 0
	if len(shown) > maxShown {
		more = len(shown) - maxShown
		shown = shown[:maxShown]
	}
	result := strings.Join(shown, ", ")
	if includeCount {
		result = fmt.Sprintf("%d vulns: %s", len(vulnerabilities), result)
	}
	if more > 0 {
		result += fmt.Sprintf(" + %d more", more)
	}
	if len(fixed) > 0 {
		sort.Strings(fixed)
		result += " | fixed in " + fixed[0]
	}
	return result
}

func vulnerabilityLabel(v osvVulnerability) string {
	for _, alias := range v.Aliases {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(alias)), "CVE-") {
			return strings.ToUpper(strings.TrimSpace(alias))
		}
	}
	return strings.TrimSpace(v.ID)
}

func vulnerabilitySeverity(v osvVulnerability) string {
	if raw, ok := v.DatabaseSpecific["severity"]; ok {
		var severity string
		if json.Unmarshal(raw, &severity) == nil && severity != "" {
			return strings.ToUpper(severity)
		}
	}
	for _, severity := range v.Severity {
		value := strings.ToUpper(strings.TrimSpace(severity.Score))
		for _, name := range []string{"CRITICAL", "HIGH", "MEDIUM", "MODERATE", "LOW"} {
			if strings.Contains(value, name) {
				return name
			}
		}
		if score, err := strconv.ParseFloat(value, 64); err == nil {
			switch {
			case score >= 9:
				return "CRITICAL"
			case score >= 7:
				return "HIGH"
			case score >= 4:
				return "MEDIUM"
			default:
				return "LOW"
			}
		}
	}
	return ""
}

func vulnerabilityFixed(v osvVulnerability) string {
	for _, affected := range v.Affected {
		for _, versionRange := range affected.Ranges {
			for _, event := range versionRange.Events {
				if strings.TrimSpace(event.Fixed) != "" {
					return event.Fixed
				}
			}
		}
	}
	return ""
}

func osvVersionAffected(v osvVulnerability, version string) bool {
	version = strings.TrimSpace(version)
	for _, affected := range v.Affected {
		for _, exact := range affected.Versions {
			if strings.EqualFold(strings.TrimPrefix(exact, "v"), strings.TrimPrefix(version, "v")) {
				return true
			}
		}
		for _, versionRange := range affected.Ranges {
			if rangeAffectsVersion(versionRange, version) {
				return true
			}
		}
	}
	return false
}

func rangeAffectsVersion(versionRange osvRange, version string) bool {
	if len(versionRange.Events) == 0 {
		return false
	}
	current, currentOK := parseSimpleVersion(version)
	affected := false
	for _, event := range versionRange.Events {
		if event.Introduced != "" {
			introduced, ok := parseSimpleVersion(event.Introduced)
			if !ok || !currentOK {
				if strings.EqualFold(event.Introduced, version) {
					affected = true
				}
			} else if compareSimpleVersion(current, introduced) >= 0 {
				affected = true
			}
		}
		if event.Fixed != "" {
			fixed, ok := parseSimpleVersion(event.Fixed)
			if ok && currentOK {
				if compareSimpleVersion(current, fixed) >= 0 {
					affected = false
				}
			} else if strings.EqualFold(event.Fixed, version) {
				affected = false
			}
		}
		if event.LastAffected != "" {
			last, ok := parseSimpleVersion(event.LastAffected)
			if ok && currentOK {
				affected = compareSimpleVersion(current, last) <= 0
			}
		}
	}
	return affected
}

type simpleVersion struct {
	major, minor, patch int
	pre                 string
}

func parseSimpleVersion(value string) (simpleVersion, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "+", 2)[0]
	parts := strings.SplitN(value, "-", 2)
	numbers := strings.Split(parts[0], ".")
	if len(numbers) < 1 || len(numbers) > 3 {
		return simpleVersion{}, false
	}
	values := [3]int{}
	for i, number := range numbers {
		parsed, err := strconv.Atoi(number)
		if err != nil {
			return simpleVersion{}, false
		}
		values[i] = parsed
	}
	pre := ""
	if len(parts) == 2 {
		pre = parts[1]
	}
	return simpleVersion{values[0], values[1], values[2], pre}, true
}

func compareSimpleVersion(a, b simpleVersion) int {
	for _, pair := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if a.pre == b.pre {
		return 0
	}
	if a.pre == "" {
		return 1
	}
	if b.pre == "" {
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}
