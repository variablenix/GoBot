package plugins

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Weapons serves a high-level, local reference catalog of firearm and weapons
// names. It deliberately provides identification only: no instructions,
// construction details, acquisition guidance, or tactical advice.
type Weapons struct {
	items      []weaponEntry
	byCategory map[string][]weaponEntry
	maxLength  int
}

type weaponEntry struct {
	category string
	name     string
}

var weaponAliases = map[string]string{
	"pistol": "pistol", "pistols": "pistol",
	"revolver": "revolver", "revolvers": "revolver",
	"handgun": "handgun", "handguns": "handgun",
	"rifle": "rifle", "rifles": "rifle",
	"carbine": "carbine", "carbines": "carbine",
	"shotgun": "shotgun", "shotguns": "shotgun",
	"smg": "smg", "submachine": "smg", "submachinegun": "smg", "submachine-gun": "smg",
	"machinegun": "machine-gun", "machine-gun": "machine-gun", "machineguns": "machine-gun",
	"sniper": "sniper", "sniperrifle": "sniper", "sniper-rifle": "sniper",
	"launcher": "launcher", "launchers": "launcher",
	"historical": "historical", "classic": "historical",
	"explosive": "explosive", "explosives": "explosive",
	"grenade": "grenade", "grenades": "grenade",
}

func (p *Weapons) Name() string { return "weapons" }

func (p *Weapons) Commands() []string {
	return []string{
		"firearm", "firearms", "gun", "guns", "weapon", "weapons", "arms",
		"pistol", "pistols", "handgun", "handguns", "rifle", "rifles",
		"carbine", "carbines", "shotgun", "shotguns", "smg", "submachinegun",
		"machinegun", "sniper", "sniperrifle", "launcher", "launchers",
		"grenade", "grenades", "explosive", "explosives",
	}
}

func (p *Weapons) Help() string {
	return "!firearm [category] — suggest a high-level firearm or weapons term; aliases include !guns, !weapons, !pistol, !rifle, !shotgun, !grenade"
}

func (p *Weapons) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", 240)
	if p.maxLength < 100 || p.maxLength > 400 {
		p.maxLength = 240
	}
	p.items = nil
	p.byCategory = make(map[string][]weaponEntry)
	path := c.String("data_file", filepath.Join("data", "weapons.txt"))
	for _, line := range readFoodList(path) {
		parts := strings.SplitN(line, "|", 2)
		category, name := "all", strings.TrimSpace(line)
		if len(parts) == 2 {
			category = normalizeWeaponCategory(parts[0])
			name = strings.TrimSpace(parts[1])
		}
		if name == "" {
			continue
		}
		entry := weaponEntry{category: category, name: name}
		p.items = append(p.items, entry)
		p.byCategory[category] = append(p.byCategory[category], entry)
		p.byCategory["all"] = append(p.byCategory["all"], entry)
	}
	if len(p.items) == 0 {
		p.items = weaponFallbacks
		p.byCategory["all"] = append([]weaponEntry(nil), weaponFallbacks...)
		for _, item := range weaponFallbacks {
			p.byCategory[item.category] = append(p.byCategory[item.category], item)
		}
	}
	return nil
}

func (p *Weapons) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok {
		return false
	}
	command := strings.ToLower(cmd)
	if !isWeaponCommand(command) {
		return false
	}

	category := normalizeWeaponCategory(command)
	if category == "" {
		category = "all"
	}
	arg = strings.TrimSpace(arg)
	target := ""
	if category == "all" && arg != "" {
		parts := strings.Fields(arg)
		if selected := normalizeWeaponCategory(parts[0]); selected != "" {
			category = selected
			arg = strings.TrimSpace(strings.TrimPrefix(arg, parts[0]))
		}
	}
	if arg != "" {
		target = truncateRunes(cleanExternalText(arg), 60)
	}

	choices := p.byCategory[category]
	if len(choices) == 0 {
		choices = p.byCategory["all"]
	}
	if len(choices) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "the weapons catalog is unavailable"))
		return true
	}
	item := choices[rand.Intn(len(choices))]
	result := fmt.Sprintf("Arsenal pick [%s]: %s", displayWeaponCategory(item.category), item.name)
	if target != "" {
		result = fmt.Sprintf("Arsenal pick for %s [%s]: %s", target, displayWeaponCategory(item.category), item.name)
	}
	b.Send(m.ReplyTarget(), truncateRunes(ircColor(ircCyan, result), p.maxLength))
	return true
}

func isWeaponCommand(command string) bool {
	if command == "firearm" || command == "firearms" || command == "gun" || command == "guns" || command == "weapon" || command == "weapons" || command == "arms" {
		return true
	}
	_, ok := weaponAliases[command]
	return ok
}

func normalizeWeaponCategory(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "firearm" || value == "firearms" || value == "gun" || value == "guns" || value == "weapon" || value == "weapons" || value == "arms" {
		return "all"
	}
	return weaponAliases[value]
}

func displayWeaponCategory(category string) string {
	if category == "all" || category == "" {
		return "General"
	}
	return cases.Title(language.English).String(strings.ReplaceAll(category, "-", " "))
}

var weaponFallbacks = []weaponEntry{
	{category: "pistol", name: ".22 rimfire pistol"},
	{category: "rifle", name: "bolt-action sporting rifle"},
	{category: "shotgun", name: "over-under shotgun"},
	{category: "revolver", name: "double-action revolver"},
	{category: "grenade", name: "training grenade — identification only"},
}
