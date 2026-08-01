package plugins

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type eightBallResponse struct {
	Color string
	Text  string
}

type EightBall struct {
	responses []eightBallResponse
}

func (p *EightBall) Name() string {
	return "eightball"
}

func (p *EightBall) Commands() []string {
	return []string{"8ball", "8", "eightball"}
}

func (p *EightBall) Help() string {
	return "!8ball <question> — ask the magic 8-ball (aliases: !8, !eightball)"
}

func (p *EightBall) Init(c bot.PluginConfig, _ *storage.DB) error {
	path := c.String("responses_file", "quotes/eightball.txt")
	p.responses = loadEightBallResponses(path)
	if len(p.responses) == 0 {
		p.responses = defaultEightBallResponses()
	}
	return nil
}

func (p *EightBall) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "8ball" && cmd != "8" && cmd != "eightball") {
		return false
	}
	if strings.TrimSpace(arg) == "" {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !8ball <question>"))
		return true
	}
	if len(p.responses) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "the magic 8-ball is temporarily unavailable"))
		return true
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(p.responses))))
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "the magic 8-ball is temporarily unavailable"))
		return true
	}
	response := p.responses[index.Int64()]
	b.Send(m.ReplyTarget(), fmt.Sprintf("shakes the magic 8-ball... %s", ircColor(response.Color, response.Text)))
	return true
}

func loadEightBallResponses(path string) []eightBallResponse {
	lines := readQuotes(path)
	responses := make([]eightBallResponse, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		color, text := "", line
		if parts := strings.SplitN(line, "|", 2); len(parts) == 2 {
			color, text = strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1])
		}
		if text == "" {
			continue
		}
		color = eightBallIRCColor(color)
		responses = append(responses, eightBallResponse{Color: color, Text: truncateRunes(cleanExternalText(text), 220)})
	}
	return responses
}

func eightBallIRCColor(name string) string {
	switch name {
	case "green", ircGreen:
		return ircGreen
	case "yellow", ircYellow:
		return ircYellow
	case "red", ircRed:
		return ircRed
	default:
		return ""
	}
}

func defaultEightBallResponses() []eightBallResponse {
	return []eightBallResponse{
		{Color: ircGreen, Text: "Yes, definitely."},
		{Color: ircYellow, Text: "Ask again later."},
		{Color: ircRed, Text: "Very doubtful."},
	}
}
