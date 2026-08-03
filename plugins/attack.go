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
	name           string
	response       attackResponse
	templates      []string
	optionalTarget bool
}

var attackDefinitions = map[string]attackDefinition{
	"bite": {
		name: "bite", response: attackAction,
		templates: []string{"gives {target} a tiny cartoon bite.", "nibbles {target} with maximum theatricality."},
	},
	"bdsm": {
		name: "bdsm", response: attackAction,
		templates: []string{"offers {target} a clearly consensual, completely theatrical roleplay challenge.", "invites {target} to a consent-first battle of dramatic poses."},
	},
	"compliment": {
		name: "compliment", response: attackMessage,
		templates: []string{"{target}, your charisma stat is dangerously high.", "{target}, you are doing an excellent job being you."},
	},
	"clinton": {
		name: "clinton", response: attackAction,
		templates: []string{"gives {target} a famously presidential wave.", "offers {target} an overdramatic campaign-trail high five."},
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
	"lurve": {
		name: "lurve", response: attackMessage,
		templates: []string{"{target}, you are lurved with maximum wholesome theatricality.", "{target}, please accept this highly enthusiastic declaration of lurve."},
	},
	"nk": {
		name: "nk", response: attackMessage, optionalTarget: true,
		templates: []string{"broadcasts a completely fictional, wildly over-the-top propaganda slogan."},
	},
	"pokemon": {
		name: "pokemon", response: attackMessage,
		templates: []string{"tosses a cartoon Poké Ball near {target}; no actual catching occurs."},
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
	"strax": {
		name: "strax", response: attackMessage, optionalTarget: true,
		templates: []string{"recites a very serious report about the tactical importance of sandwiches."},
	},
	"trump": {
		name: "trump", response: attackAction,
		templates: []string{"hands {target} an extremely overconfident gold-star trophy.", "delivers {target} a spectacularly exaggerated thumbs-up."},
	},
	"westworld": {
		name: "westworld", response: attackMessage, optionalTarget: true,
		templates: []string{"recites a mysterious fictional-western android monologue about loops and dust."},
	},
}

var attackCommandOrder = []string{
	"attack", "bdsm", "bite", "clinton", "compliment", "fight", "flirt", "glomp", "highfive", "hug", "insult", "kill", "lart", "lurve", "nk", "pokemon", "present", "slap", "spank", "stab", "strax", "trump", "westworld",
}

var attackAliases = map[string]string{
	"attack":     "attack",
	"bdsm":       "bdsm",
	"bite":       "bite",
	"clinton":    "clinton",
	"compliment": "compliment",
	"challenge":  "fight",
	"dominate":   "bdsm",
	"end":        "kill",
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
	"luff":       "lurve",
	"luv":        "lurve",
	"lurve":      "lurve",
	"nk":         "nk",
	"pokemon":    "pokemon",
	"present":    "present",
	"gift":       "present",
	"sexup":      "flirt",
	"slap":       "slap",
	"spar":       "fight",
	"spank":      "spank",
	"stab":       "stab",
	"strax":      "strax",
	"trump":      "trump",
	"westworld":  "westworld",
	"jackmeoff":  "flirt",
}

// Attack provides short, playful target-based actions inspired by classic IRC
// fun bots. Templates are built in and user input is restricted to IRC-style
// nicknames so control characters cannot reach an outbound message.
type Attack struct{}

func (p *Attack) Name() string { return "attack" }

func (p *Attack) Commands() []string {
	commands := make([]string, len(attackCommandOrder))
	copy(commands, attackCommandOrder)
	commands = append(commands, "challenge", "dominate", "end", "fite", "gift", "high5", "hi5", "jackmeoff", "luff", "luv", "sexup", "spar")
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
	definition := attackDefinitions[style]
	if !definition.optionalTarget && !validAttackTarget(target) {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "attack targets must be a single valid nickname"))
		return true
	}
	if target != "" && !validAttackTarget(target) {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "attack targets must be a single valid nickname"))
		return true
	}
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
		if len(fields) < 1 || len(fields) > 2 {
			return "", "", false
		}
		style, ok := attackAliases[strings.ToLower(fields[0])]
		if !ok || style == "attack" {
			return "", "", false
		}
		if len(fields) == 1 {
			if !attackDefinitions[style].optionalTarget {
				return "", "", false
			}
			return style, "", true
		}
		return style, fields[1], true
	}
	style, ok := attackAliases[strings.ToLower(command)]
	if !ok || style == "attack" {
		return "", "", false
	}
	if len(fields) == 0 && attackDefinitions[style].optionalTarget {
		return style, "", true
	}
	if len(fields) != 1 {
		return "", "", false
	}
	return style, fields[0], true
}

func attackUsage(command string) string {
	if command == "attack" {
		return ircColor(ircYellow, "usage: !attack <style> [nick] (styles: bdsm, bite, clinton, compliment, fight, flirt, glomp, highfive, hug, insult, kill, lart, lurve, nk, pokemon, present, slap, spank, stab, strax, trump, westworld)")
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
