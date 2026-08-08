package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Docker struct{ cfg bot.PluginConfig }

type dockerRepository struct {
	Name        string  `json:"name"`
	Namespace   string  `json:"namespace"`
	PullCount   float64 `json:"pull_count"`
	StarCount   float64 `json:"star_count"`
	Description string  `json:"description"`
}
type dockerTags struct {
	Results []struct {
		Name string `json:"name"`
	} `json:"results"`
}

func (p *Docker) Name() string       { return "docker" }
func (p *Docker) Commands() []string { return []string{"docker", "hub", "dockerhub"} }
func (p *Docker) Help() string {
	return "!docker <image|user/image> — show Docker Hub image metadata (aliases: !hub, !dockerhub)"
}
func (p *Docker) Init(c bot.PluginConfig, _ *storage.DB) error { p.cfg = c; return nil }

func (p *Docker) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "docker" && cmd != "hub" && cmd != "dockerhub") {
		return false
	}
	image := strings.TrimSpace(arg)
	parts := strings.Split(image, "/")
	if len(parts) == 0 || len(parts) > 2 || image == "" || strings.ContainsAny(image, " \r\n\t?#") {
		b.Send(m.ReplyTarget(), "usage: !docker <image|user/image>")
		return true
	}
	official := len(parts) == 1
	user, name := "library", parts[0]
	if !official {
		user, name = parts[0], parts[1]
	}
	if user == "" || name == "" || len([]rune(user)) > 128 || len([]rune(name)) > 128 {
		b.Send(m.ReplyTarget(), "usage: !docker <image|user/image>")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), packageTimeout(p.cfg))
	defer cancel()
	repository, err := lookupDockerRepository(ctx, user, name)
	if err != nil {
		if err == errDockerNotFound {
			b.Send(m.ReplyTarget(), fmt.Sprintf("[docker] %s not found", cleanExternalText(image)))
		} else {
			b.Send(m.ReplyTarget(), "[docker] image lookup is temporarily unavailable")
		}
		return true
	}
	latest := "unknown"
	if tags, tagErr := lookupDockerTags(ctx, user, name); tagErr == nil && len(tags.Results) > 0 {
		latest = tags.Results[0].Name
	}
	label := cleanExternalText(name)
	if !official {
		label = cleanExternalText(user) + "/" + label
	} else {
		label += " (official)"
	}
	link := "https://hub.docker.com/_/" + name
	if !official {
		link = "https://hub.docker.com/r/" + user + "/" + name
	}
	result := fmt.Sprintf("[docker] %s — latest: %s | pulls: %s | stars: %s", label, cleanExternalText(latest), humanCount(repository.PullCount), humanCount(repository.StarCount))
	if description := cleanExternalText(repository.Description); description != "" {
		result += " | " + description
	}
	result += " | " + link
	b.Send(m.ReplyTarget(), truncateRunes(result, packageMaxLength(p.cfg)))
	return true
}

var errDockerNotFound = fmt.Errorf("Docker image not found")

func dockerEndpoint(user, name string) string {
	return "https://hub.docker.com/v2/repositories/" + packagePath(user) + "/" + packagePath(name) + "/"
}

func lookupDockerRepository(ctx context.Context, user, name string) (dockerRepository, error) {
	var result dockerRepository
	if err := getDockerJSON(ctx, dockerEndpoint(user, name), &result); err != nil {
		return dockerRepository{}, err
	}
	return result, nil
}

func lookupDockerTags(ctx context.Context, user, name string) (dockerTags, error) {
	var result dockerTags
	endpoint := dockerEndpoint(user, name) + "tags/?page_size=1&ordering=last_updated"
	return result, getDockerJSON(ctx, endpoint, &result)
}

func getDockerJSON(ctx context.Context, endpoint string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; Docker Hub lookup)")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return errDockerNotFound
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Docker Hub returned HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 2*1024*1024)).Decode(target)
}

func humanCount(value float64) string {
	if value >= 1_000_000_000 {
		return trimHuman(value/1_000_000_000) + "B"
	}
	if value >= 1_000_000 {
		return trimHuman(value/1_000_000) + "M"
	}
	if value >= 1_000 {
		return trimHuman(value/1_000) + "k"
	}
	return fmt.Sprintf("%.0f", value)
}

func trimHuman(value float64) string {
	if value >= 100 {
		return fmt.Sprintf("%.0f", value)
	}
	if value >= 10 {
		return fmt.Sprintf("%.1f", value)
	}
	return fmt.Sprintf("%.1f", value)
}
