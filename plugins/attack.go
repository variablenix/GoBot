package plugins

import (
	"fmt"
	"math/rand"
	"strings"
	"unicode"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type attackResponse int

const (
	attackAction attackResponse = iota
	attackMessage
)

type attackDefinition struct {
	name      string
	response  attackResponse
	templates []string
}

var attackDefinitions = map[string]attackDefinition{
	"bite": {
		name: "bite", response: attackAction,
		templates: []string{"gives {target} a tiny cartoon bite.", "nibbles {target} with maximum theatricality."},
	},
	"compliment": {
		name: "compliment", response: attackMessage,
		templates: []string{"{target}, your charisma stat is dangerously high.", "{target}, you are doing an excellent job being you."},
	},
	"fight": {
		name: "fight", response: attackAction,
		templates: []string{"challenges {target} to a duel of rock-paper-scissors.", "squares up against {target} with a pool noodle."},
	},
	"flirt": {
		name: "flirt", response: attackMessage,
		templates: []string{"{target}, are you always this charming, or is today special?", "{target}, your smile just caused a minor system outage."},
	},
	"glomp": {
		name: "glomp", response: attackAction,
		templates: []string{"glomps {target} with surprising enthusiasm.", "launches a soft, friendly glomp at {target}."},
	},
	"highfive": {
		name: "highfive", response: attackAction,
		templates: []string{"high-fives {target}.", "offers {target} an exceptionally crisp high five."},
	},
	"hug": {
		name: "hug", response: attackAction,
		templates: []string{"hugs {target} warmly.", "gives {target} a reassuring internet hug."},
	},
	"insult": {
		name: "insult", response: attackMessage,
		templates: []string{"{target}, your Wi-Fi password probably has a typo in it.", "{target}, I have seen loading bars with more forward momentum."},
	},
	"kill": {
		name: "kill", response: attackAction,
		templates: []string{"dramatically defeats {target} in a completely fictional cartoon duel.", "declares {target} defeated by the ancient art of exaggerated stage combat."},
	},
	"lart": {
		name: "lart", response: attackAction,
		templates: []string{"larts {target} with a giant foam mallet.", "delivers a highly theatrical lart to {target}."},
	},
	"present": {
		name: "present", response: attackAction,
		templates: []string{"gives {target} a suspiciously well-wrapped present.", "presents {target} with a gift containing exactly one surprise."},
	},
	"slap": {
		name: "slap", response: attackAction,
		templates: []string{"slaps {target} with a comically oversized foam hand.", "gives {target} a playful cartoon slap."},
	},
	"spank": {
		name: "spank", response: attackAction,
		templates: []string{"gives {target} a playful, completely theatrical pat.", "attempts a cartoon spank and immediately loses the prop."},
	},
	"stab": {
		name: "stab", response: attackAction,
		templates: []string{"stabs {target} with a harmless foam sword.", "attempts a dramatic foam-sword stab at {target}."},
	},
}

var attackCommandOrder = []string{
	"attack", "bite", "compliment", "fight", "flirt", "glomp", "highfive", "hug", "insult", "kill", "lart", "present", "slap", "spank", "stab",
}

var attackAliases = map[string]string{
	"attack":     "attack",
	"bite":       "bite",
	"compliment": "compliment",
	"fight":      "fight",
	"fite":       "fight",
	"flirt":      "flirt",
	"glomp":      "glomp",
	"high5":      "highfive",
	"highfive":   "highfive",
	"hi5":        "highfive",
	"hug":        "hug",
	"insult":     "insult",
	"kill":       "kill",
	"lart":       "lart",
	"present":    "present",
	"gift":       "present",
	"slap":       "slap",
	"spank":      "spank",
	"stab":       "stab",
}

// Attack provides short, playful target-based actions inspired by classic IRC
// fun bots. Templates are built in and user input is restricted to IRC-style
// nicknames so control characters cannot reach an outbound message.
type Attack struct{}

func (p *Attack) Name() string { return "attack" }

func (p *Attack) Commands() []string {
	commands := make([]string, len(attackCommandOrder))
	copy(commands, attackCommandOrder)
	commands = append(commands, "fite", "gift", "high5", "hi5")
	return commands
}

func (p *Attack) Help() string {
	return "!attack <style> <nick> — playful action or message; aliases include !slap, !hug, !flirt, !compliment, !gift, and !high5"
}

func (p *Attack) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }

func (p *Attack) Handle(b *bot.Bot, m bot.Message) bool {
	command, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok {
		return false
	}
	canonical, ok := attackAliases[strings.ToLower(command)]
	if !ok {
		return false
	}

	style, target, valid := parseAttackArguments(canonical, arg)
	if !valid {
		b.Send(m.ReplyTarget(), attackUsage(canonical))
		return true
	}
	if !validAttackTarget(target) {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "attack targets must be a single valid nickname"))
		return true
	}

	definition := attackDefinitions[style]
	actor := cleanExternalText(m.Nick)
	if actor == "" {
		actor = "someone"
	}
	if isAttackSelfTarget(target, b.Config.Identity.Nick) {
		target = actor
		actor = cleanExternalText(b.Config.Identity.Nick)
		if actor == "" {
			actor = "GoBot"
		}
	}

	template := definition.templates[rand.Intn(len(definition.templates))]
	text := renderAttackTemplate(template, actor, target)
	if definition.response == attackAction {
		b.Send(m.ReplyTarget(), formatAttackAction(text))
	} else {
		b.Send(m.ReplyTarget(), text)
	}
	return true
}

func parseAttackArguments(command, arg string) (string, string, bool) {
	fields := strings.Fields(arg)
	if command == "attack" {
		if len(fields) != 2 {
			return "", "", false
		}
		style, ok := attackAliases[strings.ToLower(fields[0])]
		if !ok || style == "attack" {
			return "", "", false
		}
		return style, fields[1], true
	}
	if len(fields) != 1 {
		return "", "", false
	}
	style, ok := attackAliases[command]
	if !ok || style == "attack" {
		return "", "", false
	}
	return style, fields[0], true
}

func attackUsage(command string) string {
	if command == "attack" {
		return ircColor(ircYellow, "usage: !attack <style> <nick> (styles: bite, compliment, fight, flirt, glomp, highfive, hug, insult, kill, lart, present, slap, spank, stab)")
	}
	return ircColor(ircYellow, fmt.Sprintf("usage: !%s <nick>", command))
}

func renderAttackTemplate(template, actor, target string) string {
	return cleanExternalText(strings.NewReplacer("{actor}", actor, "{target}", target).Replace(template))
}

func formatAttackAction(text string) string {
	return "\x01ACTION " + cleanExternalText(text) + "\x01"
}

func isAttackSelfTarget(target, botNick string) bool {
	return strings.EqualFold(target, "self") || (botNick != "" && strings.EqualFold(target, botNick))
}

func validAttackTarget(target string) bool {
	if target == "" || len([]rune(target)) > 30 {
		return false
	}
	for i, r := range target {
		if i == 0 {
			if !isAttackNickStart(r) {
				return false
			}
			continue
		}
		if !isAttackNickPart(r) {
			return false
		}
	}
	return true
}

func isAttackNickStart(r rune) bool {
	return unicode.IsLetter(r) || strings.ContainsRune("[]\\`^{}|_", r)
}

func isAttackNickPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-[]\\`^{}|_", r)
}
