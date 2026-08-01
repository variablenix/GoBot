package plugins

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Foods provides short, local food and drink suggestions. The lists live in
// data/foods so operators can add regional dishes without changing code.
// Every request produces one bounded IRC message.
type Foods struct {
	items     map[string][]string
	aliases   map[string]string
	maxLength int
}

var foodCategories = []string{
	"food", "beer", "coffee", "tea", "wine", "korean", "japanese", "sushi", "ramen",
	"chinese", "indian", "thai", "mexican", "italian", "mediterranean", "american",
	"pizza", "taco", "burger", "pasta", "dessert", "snack",
}

func (p *Foods) Name() string { return "foods" }

func (p *Foods) Commands() []string {
	commands := append([]string{"food", "foods"}, foodCategories[1:]...)
	return commands
}

func (p *Foods) Help() string {
	return "!food [category] [nickname] — suggest one food or drink; category aliases include !beer, !korean, !japanese, !sushi, and !ramen"
}

func (p *Foods) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", 240)
	if p.maxLength < 80 || p.maxLength > 400 {
		p.maxLength = 240
	}
	p.items = make(map[string][]string, len(foodCategories))
	dataDir := c.String("data_dir", "data/foods")
	for _, category := range foodCategories {
		p.items[category] = readFoodList(filepath.Join(dataDir, category+".txt"))
	}
	p.addFallbacks()
	p.aliases = map[string]string{
		"food": "food", "foods": "food", "beer": "beer", "coffee": "coffee", "java": "coffee",
		"tea": "tea", "wine": "wine", "korean": "korean", "kr": "korean", "japanese": "japanese",
		"japan": "japanese", "jp": "japanese", "sushi": "sushi", "ramen": "ramen", "chinese": "chinese",
		"indian": "indian", "thai": "thai", "mexican": "mexican", "italian": "italian",
		"mediterranean": "mediterranean", "american": "american", "pizza": "pizza", "taco": "taco",
		"burger": "burger", "pasta": "pasta", "dessert": "dessert", "snack": "snack",
	}
	return nil
}

func (p *Foods) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok {
		return false
	}
	category, exists := p.aliases[strings.ToLower(cmd)]
	if !exists {
		return false
	}

	arg = strings.TrimSpace(arg)
	target := ""
	if category == "food" && arg != "" {
		parts := strings.Fields(arg)
		if selected, ok := p.aliases[strings.ToLower(parts[0])]; ok && selected != "food" {
			category = selected
			arg = strings.TrimSpace(strings.TrimPrefix(arg, parts[0]))
		}
	}
	if arg != "" {
		target = truncateRunes(cleanExternalText(arg), 80)
	}

	if category == "food" {
		category = p.randomCategory()
	}
	choices := p.items[category]
	if len(choices) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "that food list is unavailable"))
		return true
	}
	item := choices[rand.Intn(len(choices))]
	label := strings.Title(category)
	result := fmt.Sprintf("%s pick: %s", label, item)
	if target != "" {
		result = fmt.Sprintf("%s pick for %s: %s", label, target, item)
	}
	b.Send(m.ReplyTarget(), truncateRunes(ircColor(ircCyan, result), p.maxLength))
	return true
}

func (p *Foods) randomCategory() string {
	available := make([]string, 0, len(p.items))
	for _, category := range foodCategories[1:] {
		if len(p.items[category]) > 0 {
			available = append(available, category)
		}
	}
	if len(available) == 0 {
		return "food"
	}
	return available[rand.Intn(len(available))]
}

func (p *Foods) addFallbacks() {
	fallbacks := map[string][]string{
		"food":          {"a warm bowl of ramen", "a crispy chicken sandwich", "fresh fruit", "a plate of nachos"},
		"beer":          {"a crisp pilsner", "a West Coast IPA", "a dry Irish stout", "a Belgian tripel"},
		"coffee":        {"an espresso", "a flat white", "a cold brew", "a cappuccino"},
		"tea":           {"earl grey", "matcha", "oolong tea", "masala chai"},
		"wine":          {"a dry Riesling", "a Pinot Noir", "a Malbec", "a sparkling rosé"},
		"korean":        {"bibimbap", "bulgogi", "tteokbokki", "kimchi jjigae"},
		"japanese":      {"okonomiyaki", "katsu curry", "yakitori", "onigiri"},
		"sushi":         {"salmon nigiri", "tuna maki", "unagi roll", "vegetable futomaki"},
		"ramen":         {"shoyu ramen", "miso ramen", "tonkotsu ramen", "tantanmen"},
		"chinese":       {"mapo tofu", "dan dan noodles", "char siu", "jiaozi"},
		"indian":        {"chana masala", "palak paneer", "biryani", "masala dosa"},
		"thai":          {"pad thai", "green curry", "tom yum", "khao soi"},
		"mexican":       {"tacos al pastor", "chiles rellenos", "pozole", "enchiladas"},
		"italian":       {"margherita pizza", "lasagna", "risotto", "gnocchi"},
		"mediterranean": {"falafel", "moussaka", "shakshuka", "spanakopita"},
		"american":      {"mac and cheese", "barbecue ribs", "clam chowder", "apple pie"},
		"pizza":         {"margherita pizza", "mushroom pizza", "Detroit-style pizza", "white pizza"},
		"taco":          {"fish taco", "carnitas taco", "barbacoa taco", "black bean taco"},
		"burger":        {"classic cheeseburger", "veggie burger", "smash burger", "mushroom Swiss burger"},
		"pasta":         {"spaghetti carbonara", "cacio e pepe", "pesto linguine", "puttanesca"},
		"dessert":       {"tiramisu", "mochi", "crème brûlée", "frozen yogurt"},
		"snack":         {"edamame", "popcorn", "trail mix", "rice crackers"},
	}
	for category, items := range fallbacks {
		if len(p.items[category]) == 0 {
			p.items[category] = items
		}
	}
}

func readFoodList(path string) []string {
	items := readQuotes(path)
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || strings.HasPrefix(item, "#") {
			continue
		}
		filtered = append(filtered, truncateRunes(cleanExternalText(item), 100))
	}
	return filtered
}
