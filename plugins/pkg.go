package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
	"golang.org/x/net/html"
)

type Pkg struct{ cfg bot.PluginConfig }

type packageMetadata struct {
	Ecosystem   string
	Name        string
	Version     string
	Description string
}

type packageCandidate struct {
	Name        string
	Version     string
	Description string
	URL         string
}

func (p *Pkg) Name() string       { return "pkg" }
func (p *Pkg) Commands() []string { return []string{"pkg", "package"} }
func (p *Pkg) Help() string {
	return "!pkg <go|npm|pip> <package> [version] — show package metadata; failed exact names get fuzzy suggestions (alias: !package)"
}
func (p *Pkg) Init(c bot.PluginConfig, _ *storage.DB) error { p.cfg = c; return nil }

func (p *Pkg) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "pkg" && cmd != "package") {
		return false
	}
	parts := strings.Fields(arg)
	if len(parts) < 2 || len(parts) > 3 {
		b.Send(m.ReplyTarget(), "usage: !pkg <go|npm|pip> <package> [version]")
		return true
	}
	ecosystem := normalizePackageEcosystem(parts[0])
	if ecosystem == "" || !validPackagePart(parts[1]) || (len(parts) == 3 && !validPackagePart(parts[2])) {
		b.Send(m.ReplyTarget(), "usage: !pkg <go|npm|pip> <package> [version]")
		return true
	}
	timeout := packageTimeout(p.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	version := ""
	if len(parts) == 3 {
		version = parts[2]
	}
	metadata, err := lookupPackageMetadata(ctx, ecosystem, parts[1], version)
	if err != nil {
		if err == errPackageNotFound {
			b.Send(m.ReplyTarget(), truncateRunes(formatPackageSuggestions(ctx, ecosystem, parts[1]), packageMaxLength(p.cfg)))
		} else {
			b.Send(m.ReplyTarget(), fmt.Sprintf("[%s] package lookup is temporarily unavailable", ecosystem))
		}
		return true
	}
	maxLength := packageMaxLength(p.cfg)
	b.Send(m.ReplyTarget(), truncateRunes(formatPackageMetadata(metadata), maxLength))
	return true
}

var errPackageNotFound = fmt.Errorf("package not found")

func normalizePackageEcosystem(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "go", "golang":
		return "Go"
	case "npm", "node":
		return "npm"
	case "pip", "pypi", "python":
		return "PyPI"
	default:
		return ""
	}
}

func validPackagePart(value string) bool {
	return value != "" && len([]rune(value)) <= 240 && !strings.ContainsAny(value, "\r\n\t?#")
}

func packageTimeout(c bot.PluginConfig) time.Duration {
	seconds := c.Int("timeout_seconds", 8)
	if seconds < 1 || seconds > 30 {
		seconds = 8
	}
	return time.Duration(seconds) * time.Second
}

func packageMaxLength(c bot.PluginConfig) int {
	max := c.Int("max_length", 300)
	if max < 100 || max > 500 {
		max = 300
	}
	return max
}

func lookupPackageMetadata(ctx context.Context, ecosystem, name, version string) (packageMetadata, error) {
	if !validPackagePart(name) || (version != "" && !validPackagePart(version)) {
		return packageMetadata{}, errPackageNotFound
	}
	var endpoint string
	switch ecosystem {
	case "Go":
		path := packagePath(name)
		if version == "" {
			endpoint = "https://proxy.golang.org/" + path + "/@latest"
		} else {
			endpoint = "https://proxy.golang.org/" + path + "/@v/" + url.PathEscape(version) + ".info"
		}
	case "npm":
		path := packagePath(name)
		if version == "" {
			endpoint = "https://registry.npmjs.org/" + path + "/latest"
		} else {
			endpoint = "https://registry.npmjs.org/" + path + "/" + url.PathEscape(version)
		}
	case "PyPI":
		path := url.PathEscape(name)
		if version == "" {
			endpoint = "https://pypi.org/pypi/" + path + "/json"
		} else {
			endpoint = "https://pypi.org/pypi/" + path + "/" + url.PathEscape(version) + "/json"
		}
	default:
		return packageMetadata{}, errPackageNotFound
	}
	var payload map[string]interface{}
	if err := getPackageJSON(ctx, endpoint, &payload); err != nil {
		return packageMetadata{}, err
	}
	metadata := packageMetadata{Ecosystem: ecosystem, Name: name}
	if ecosystem == "PyPI" {
		info, _ := payload["info"].(map[string]interface{})
		metadata.Version, _ = info["version"].(string)
		metadata.Description, _ = info["summary"].(string)
	} else {
		metadata.Version = packagePayloadString(payload, "version", "Version")
		metadata.Description, _ = payload["description"].(string)
	}
	if metadata.Version == "" {
		return packageMetadata{}, errPackageNotFound
	}
	return metadata, nil
}

func packagePayloadString(payload map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatPackageSuggestions(ctx context.Context, ecosystem, query string) string {
	query = cleanExternalText(query)
	candidates, err := searchPackageCandidates(ctx, ecosystem, query)
	if err != nil || len(candidates) == 0 {
		return fmt.Sprintf("[%s] %s not found; use the full package/module name", ecosystem, query)
	}
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		name := cleanExternalText(candidate.Name)
		if candidate.Version != "" {
			name += " " + cleanExternalText(candidate.Version)
		}
		if candidate.URL != "" {
			name += " (" + cleanExternalText(candidate.URL) + ")"
		}
		items = append(items, name)
	}
	return fmt.Sprintf("[%s] no exact match for %s; possible matches: %s", ecosystem, query, strings.Join(items, "; "))
}

func searchPackageCandidates(ctx context.Context, ecosystem, query string) ([]packageCandidate, error) {
	if query == "" || len([]rune(query)) > 240 {
		return nil, errPackageNotFound
	}
	switch ecosystem {
	case "Go":
		return searchGoPackages(ctx, query)
	case "npm":
		return searchNPMPackages(ctx, query)
	case "PyPI":
		return searchPyPIPackages(ctx, query)
	default:
		return nil, errPackageNotFound
	}
}

func searchGoPackages(ctx context.Context, query string) ([]packageCandidate, error) {
	endpoint := "https://pkg.go.dev/search?m=package&limit=5&q=" + url.QueryEscape(query)
	body, err := getPackageResponse(ctx, endpoint, 2*1024*1024)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	candidates := make([]packageCandidate, 0, 5)
	walkHTML(doc, func(node *html.Node) {
		if len(candidates) >= 5 || node.Type != html.ElementNode || node.Data != "a" || !hasHTMLAttribute(node, "data-test-id", "snippet-title") {
			return
		}
		href := htmlAttribute(node, "href")
		if !strings.HasPrefix(href, "/") || strings.ContainsAny(href, "?#\r\n") {
			return
		}
		module := strings.TrimPrefix(href, "/")
		if module == "" {
			return
		}
		candidates = append(candidates, packageCandidate{Name: module, URL: "https://pkg.go.dev/" + module})
	})
	return candidates, nil
}

func searchNPMPackages(ctx context.Context, query string) ([]packageCandidate, error) {
	endpoint := "https://registry.npmjs.org/-/v1/search?text=" + url.QueryEscape(query) + "&size=5"
	body, err := getPackageResponse(ctx, endpoint, 2*1024*1024)
	if err != nil {
		return nil, err
	}
	var response struct {
		Objects []struct {
			Package struct {
				Name        string `json:"name"`
				Version     string `json:"version"`
				Description string `json:"description"`
				Links       struct {
					NPM string `json:"npm"`
				} `json:"links"`
			} `json:"package"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	candidates := make([]packageCandidate, 0, len(response.Objects))
	for _, object := range response.Objects {
		if object.Package.Name == "" {
			continue
		}
		link := object.Package.Links.NPM
		if link == "" {
			link = "https://www.npmjs.com/package/" + url.PathEscape(object.Package.Name)
		}
		candidates = append(candidates, packageCandidate{Name: object.Package.Name, Version: object.Package.Version, Description: object.Package.Description, URL: link})
	}
	return candidates, nil
}

func searchPyPIPackages(ctx context.Context, query string) ([]packageCandidate, error) {
	endpoint := "https://pypi.org/search/?q=" + url.QueryEscape(query)
	body, err := getPackageResponse(ctx, endpoint, 2*1024*1024)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	candidates := make([]packageCandidate, 0, 5)
	walkHTML(doc, func(node *html.Node) {
		if len(candidates) >= 5 || node.Type != html.ElementNode || node.Data != "a" || !hasHTMLClass(node, "package-snippet") {
			return
		}
		href := htmlAttribute(node, "href")
		prefix := "/project/"
		if !strings.HasPrefix(href, prefix) {
			return
		}
		name := strings.Trim(strings.TrimPrefix(href, prefix), "/")
		if name == "" || strings.ContainsAny(name, "?#\r\n") {
			return
		}
		candidates = append(candidates, packageCandidate{Name: name, URL: "https://pypi.org/project/" + name + "/"})
	})
	return candidates, nil
}

func getPackageResponse(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/html")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; package search)")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("package search returned HTTP %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, limit))
}

func walkHTML(node *html.Node, visit func(*html.Node)) {
	if node == nil {
		return
	}
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, visit)
	}
}

func htmlAttribute(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

func hasHTMLAttribute(node *html.Node, key, value string) bool {
	return htmlAttribute(node, key) == value
}

func hasHTMLClass(node *html.Node, class string) bool {
	for _, value := range strings.Fields(htmlAttribute(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func packagePath(value string) string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func getPackageJSON(ctx context.Context, endpoint string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; package lookup)")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return errPackageNotFound
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("package registry returned HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 2*1024*1024)).Decode(target)
}

func formatPackageMetadata(metadata packageMetadata) string {
	name := cleanExternalText(metadata.Name)
	version := cleanExternalText(metadata.Version)
	description := cleanExternalText(metadata.Description)
	detail := ""
	if description != "" {
		detail = " — " + description
	}
	switch metadata.Ecosystem {
	case "Go":
		return fmt.Sprintf("[Go] %s %s%s | https://pkg.go.dev/%s", name, version, detail, name)
	case "npm":
		return fmt.Sprintf("[npm] %s %s%s | https://npmjs.com/package/%s", name, version, detail, name)
	default:
		return fmt.Sprintf("[PyPI] %s %s%s | https://pypi.org/project/%s", name, version, detail, name)
	}
}
