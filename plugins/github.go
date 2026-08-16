package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// GitHub provides compact, public GitHub lookups. It accepts repository
// names, GitHub URLs, issues, pull requests, releases, users, and searches.
// Every response is bounded to one IRC message.
type GitHub struct {
	cfg bot.PluginConfig
}

func (p *GitHub) Name() string       { return "github" }
func (p *GitHub) Commands() []string { return []string{"github", "gh"} }
func (p *GitHub) Help() string {
	return "!github <owner/repo|GitHub URL> — show a repository, issue, PR, or release; !github user <name>; !github search <query> (alias: !gh)"
}
func (p *GitHub) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.cfg = c
	return nil
}

func (p *GitHub) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "github" && cmd != "gh") {
		return false
	}
	arg = strings.TrimSpace(arg)
	if arg == "" {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !github <owner/repo|GitHub URL> | !github user <name> | !github search <query>"))
		return true
	}

	timeoutSeconds := p.cfg.Int("timeout_seconds", 8)
	if timeoutSeconds < 1 || timeoutSeconds > 30 {
		timeoutSeconds = 8
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	result, err := p.lookup(ctx, arg)
	if err != nil {
		message := "GitHub lookup is temporarily unavailable"
		if err == errInvalidGitHubTarget {
			message = "usage: !github <owner/repo|GitHub URL> | !github user <name> | !github search <query>"
		}
		b.Send(m.ReplyTarget(), ircColor(ircYellow, message))
		return true
	}

	maxLength := p.cfg.Int("max_length", 360)
	if maxLength < 120 || maxLength > 500 {
		maxLength = 360
	}
	b.Send(m.ReplyTarget(), "🐙 "+truncateRunes(result, maxLength))
	return true
}

var errInvalidGitHubTarget = fmt.Errorf("invalid GitHub target")

type githubTarget struct {
	owner  string
	repo   string
	kind   string
	number int
	tag    string
	sha    string
}

func (p *GitHub) lookup(ctx context.Context, arg string) (string, error) {
	parts := strings.Fields(arg)
	if len(parts) == 0 {
		return "", errInvalidGitHubTarget
	}
	switch strings.ToLower(parts[0]) {
	case "user", "u":
		if len(parts) != 2 || !validGitHubName(parts[1]) {
			return "", errInvalidGitHubTarget
		}
		return p.lookupUser(ctx, parts[1])
	case "search", "find":
		query := strings.TrimSpace(strings.TrimPrefix(arg, parts[0]))
		if query == "" || len([]rune(query)) > 120 {
			return "", errInvalidGitHubTarget
		}
		return p.searchRepositories(ctx, query)
	case "latest", "release":
		if len(parts) != 2 {
			return "", errInvalidGitHubTarget
		}
		target, err := parseGitHubTarget(parts[1])
		if err != nil {
			return "", err
		}
		return p.lookupRelease(ctx, target)
	}

	target, err := parseGitHubTarget(parts[0])
	if err != nil {
		return "", err
	}
	if target.kind == "repo" && len(parts) > 1 {
		switch strings.ToLower(parts[1]) {
		case "latest", "release", "releases":
			return p.lookupRelease(ctx, target)
		default:
			number, parseErr := strconv.Atoi(parts[1])
			if parseErr == nil && number > 0 {
				target.kind = "issue"
				target.number = number
			} else {
				return "", errInvalidGitHubTarget
			}
		}
	}
	switch target.kind {
	case "repo":
		return p.lookupRepository(ctx, target)
	case "issue", "pr":
		return p.lookupIssue(ctx, target)
	case "release":
		return p.lookupRelease(ctx, target)
	case "commit":
		return p.lookupCommit(ctx, target)
	default:
		return "", errInvalidGitHubTarget
	}
}

func parseGitHubTarget(raw string) (githubTarget, error) {
	raw = strings.TrimSpace(strings.Trim(raw, "<>()[]{}.,!?"))
	if raw == "" {
		return githubTarget{}, errInvalidGitHubTarget
	}
	if strings.Contains(raw, "#") && !strings.Contains(raw, "://") {
		base, number, ok := strings.Cut(raw, "#")
		if ok && base != "" {
			n, err := strconv.Atoi(number)
			if err == nil && n > 0 {
				raw = base
				parts := strings.Split(strings.Trim(raw, "/"), "/")
				if len(parts) == 2 {
					return githubTarget{owner: parts[0], repo: strings.TrimSuffix(parts[1], ".git"), kind: "issue", number: n}, nil
				}
			}
		}
	}

	path := raw
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || !isGitHubHost(u.Hostname()) {
			return githubTarget{}, errInvalidGitHubTarget
		}
		path = u.Path
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || !validGitHubName(parts[0]) || !validGitHubName(strings.TrimSuffix(parts[1], ".git")) {
		return githubTarget{}, errInvalidGitHubTarget
	}
	target := githubTarget{owner: parts[0], repo: strings.TrimSuffix(parts[1], ".git"), kind: "repo"}
	if len(parts) == 2 {
		return target, nil
	}
	switch strings.ToLower(parts[2]) {
	case "issues", "pull", "pulls":
		if len(parts) != 4 {
			return githubTarget{}, errInvalidGitHubTarget
		}
		number, err := strconv.Atoi(parts[3])
		if err != nil || number < 1 {
			return githubTarget{}, errInvalidGitHubTarget
		}
		target.kind = "issue"
		target.number = number
		if strings.ToLower(parts[2]) != "issues" {
			target.kind = "pr"
		}
	case "commit", "commits":
		if len(parts) != 4 || !validGitHubSHA(parts[3]) {
			return githubTarget{}, errInvalidGitHubTarget
		}
		target.kind, target.sha = "commit", parts[3]
	case "releases":
		if len(parts) == 4 && strings.EqualFold(parts[3], "latest") {
			target.kind = "release"
		} else if len(parts) == 5 && strings.EqualFold(parts[3], "tag") && parts[4] != "" {
			target.kind, target.tag = "release", parts[4]
		} else {
			return githubTarget{}, errInvalidGitHubTarget
		}
	default:
		return githubTarget{}, errInvalidGitHubTarget
	}
	return target, nil
}

func validGitHubName(value string) bool {
	if value == "" || len([]rune(value)) > 100 || strings.ContainsAny(value, "/?#%") {
		return false
	}
	return true
}

func validGitHubSHA(value string) bool {
	if len(value) < 7 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func isGitHubHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "github.com" || host == "www.github.com"
}

type githubRepository struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	HTMLURL     string `json:"html_url"`
	Language    string `json:"language"`
	Stars       int    `json:"stargazers_count"`
	Forks       int    `json:"forks_count"`
	OpenIssues  int    `json:"open_issues_count"`
	Archived    bool   `json:"archived"`
}

func (p *GitHub) lookupRepository(ctx context.Context, target githubTarget) (string, error) {
	var repo githubRepository
	if err := p.getJSON(ctx, "/repos/"+url.PathEscape(target.owner)+"/"+url.PathEscape(target.repo), &repo); err != nil {
		return "", err
	}
	result := cleanExternalText(repo.FullName)
	if result == "" {
		result = target.owner + "/" + target.repo
	}
	if description := cleanExternalText(repo.Description); description != "" {
		result += " — " + description
	}
	result += fmt.Sprintf(" | ★ %d | forks %d | open issues %d", repo.Stars, repo.Forks, repo.OpenIssues)
	if language := cleanExternalText(repo.Language); language != "" {
		result += " | " + language
	}
	if repo.Archived {
		result += " | archived"
	}
	return result + " — " + firstNonEmpty(repo.HTMLURL, "https://github.com/"+target.owner+"/"+target.repo), nil
}

type githubIssue struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	State    string `json:"state"`
	HTMLURL  string `json:"html_url"`
	Body     string `json:"body"`
	Comments int    `json:"comments"`
	User     struct {
		Login string `json:"login"`
	} `json:"user"`
	Pull map[string]any `json:"pull_request"`
}

func (p *GitHub) lookupIssue(ctx context.Context, target githubTarget) (string, error) {
	var issue githubIssue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(target.owner), url.PathEscape(target.repo), target.number)
	if err := p.getJSON(ctx, path, &issue); err != nil {
		return "", err
	}
	kind := "Issue"
	if target.kind == "pr" || issue.Pull != nil {
		kind = "PR"
	}
	state := cleanExternalText(issue.State)
	if state == "" {
		state = "unknown"
	}
	result := fmt.Sprintf("%s #%d (%s) — %s", kind, issue.Number, state, cleanExternalText(issue.Title))
	if issue.User.Login != "" {
		result += " | by " + cleanExternalText(issue.User.Login)
	}
	result += fmt.Sprintf(" | %d comments", issue.Comments)
	if summary := firstExternalLine(issue.Body); summary != "" {
		result += " | " + summary
	}
	return result + " — " + issue.HTMLURL, nil
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

func (p *GitHub) lookupRelease(ctx context.Context, target githubTarget) (string, error) {
	path := "/repos/" + url.PathEscape(target.owner) + "/" + url.PathEscape(target.repo) + "/releases/latest"
	if target.tag != "" {
		path = "/repos/" + url.PathEscape(target.owner) + "/" + url.PathEscape(target.repo) + "/releases/tags/" + url.PathEscape(target.tag)
	}
	var release githubRelease
	if err := p.getJSON(ctx, path, &release); err != nil {
		return "", err
	}
	label := firstNonEmpty(cleanExternalText(release.Name), cleanExternalText(release.TagName), "release")
	result := fmt.Sprintf("%s/%s release %s", target.owner, target.repo, label)
	if release.PublishedAt != "" {
		if published, err := time.Parse(time.RFC3339, release.PublishedAt); err == nil {
			result += " | " + published.UTC().Format("2006-01-02")
		}
	}
	if release.Prerelease {
		result += " | prerelease"
	}
	return result + " — " + release.HTMLURL, nil
}

type githubCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
		} `json:"author"`
	} `json:"commit"`
}

func (p *GitHub) lookupCommit(ctx context.Context, target githubTarget) (string, error) {
	var commit githubCommit
	path := "/repos/" + url.PathEscape(target.owner) + "/" + url.PathEscape(target.repo) + "/commits/" + url.PathEscape(target.sha)
	if err := p.getJSON(ctx, path, &commit); err != nil {
		return "", err
	}
	message := firstExternalLine(commit.Commit.Message)
	result := fmt.Sprintf("%s/%s commit %s", target.owner, target.repo, shortSHA(firstNonEmpty(commit.SHA, target.sha)))
	if message != "" {
		result += " — " + message
	}
	if author := cleanExternalText(commit.Commit.Author.Name); author != "" {
		result += " | by " + author
	}
	return result + " — " + commit.HTMLURL, nil
}

type githubUser struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	Bio       string `json:"bio"`
	HTMLURL   string `json:"html_url"`
	Repos     int    `json:"public_repos"`
	Followers int    `json:"followers"`
}

func (p *GitHub) lookupUser(ctx context.Context, name string) (string, error) {
	var user githubUser
	if err := p.getJSON(ctx, "/users/"+url.PathEscape(name), &user); err != nil {
		return "", err
	}
	result := "@" + cleanExternalText(user.Login)
	if user.Name != "" && !strings.EqualFold(user.Name, user.Login) {
		result += " (" + cleanExternalText(user.Name) + ")"
	}
	result += fmt.Sprintf(" | %d public repos | %d followers", user.Repos, user.Followers)
	if bio := cleanExternalText(user.Bio); bio != "" {
		result += " — " + bio
	}
	return result + " — " + user.HTMLURL, nil
}

type githubSearchResponse struct {
	Items []githubRepository `json:"items"`
}

func (p *GitHub) searchRepositories(ctx context.Context, query string) (string, error) {
	var data githubSearchResponse
	path := "/search/repositories?q=" + url.QueryEscape(query) + "&sort=stars&order=desc&per_page=3"
	if err := p.getJSON(ctx, path, &data); err != nil {
		return "", err
	}
	if len(data.Items) == 0 {
		return "GitHub search found no repositories", nil
	}
	results := make([]string, 0, len(data.Items))
	for _, repo := range data.Items {
		item := cleanExternalText(repo.FullName)
		if description := cleanExternalText(repo.Description); description != "" {
			item += " — " + description
		}
		item += fmt.Sprintf(" | ★ %d | %s", repo.Stars, repo.HTMLURL)
		results = append(results, item)
	}
	return "GitHub search: " + strings.Join(results, " ; "), nil
}

func (p *GitHub) getJSON(ctx context.Context, path string, destination any) error {
	base := "https://api.github.com"
	endpoint := strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; GitHub lookup)")
	if token := strings.TrimSpace(p.cfg.String("token", "")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("github API returned %s", res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(destination)
}

func firstExternalLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = cleanExternalText(line); line != "" {
			return truncateRunes(line, 120)
		}
	}
	return ""
}

func shortSHA(value string) string {
	if len(value) > 10 {
		return value[:10]
	}
	return value
}
