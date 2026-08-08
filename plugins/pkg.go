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
)

type Pkg struct{ cfg bot.PluginConfig }

type packageMetadata struct {
	Ecosystem   string
	Name        string
	Version     string
	Description string
}

func (p *Pkg) Name() string       { return "pkg" }
func (p *Pkg) Commands() []string { return []string{"pkg", "package"} }
func (p *Pkg) Help() string {
	return "!pkg <go|npm|pip> <package> [version] — show package metadata (alias: !package)"
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
			b.Send(m.ReplyTarget(), fmt.Sprintf("[%s] %s not found", ecosystem, cleanExternalText(parts[1])))
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
		metadata.Version, _ = payload["version"].(string)
		metadata.Description, _ = payload["description"].(string)
	}
	if metadata.Version == "" {
		return packageMetadata{}, errPackageNotFound
	}
	return metadata, nil
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
